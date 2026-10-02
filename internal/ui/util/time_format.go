package util

import (
	"sync/atomic"
	"time"
	"zfs-file-history/internal/state"
	"zfs-file-history/internal/ui/theme"

	"github.com/dustin/go-humanize"
	"github.com/rivo/tview"
)

// toggleRelativeTimes is the key of the setting in state.Current.
const toggleRelativeTimes = "ui.relativeTimes"

const (
	// relativeTimeRefreshInterval is how often relative times are updated ("1 minute ago" becomes "2 minutes ago").
	relativeTimeRefreshInterval = time.Minute
	// relativeTimeLiveInterval is how often relative times are updated while one of them counts seconds
	// ("30 seconds ago", see relativeTimeLiveAge).
	relativeTimeLiveInterval = time.Second
	// relativeTimeLiveAge is the age up to which humanize.Time counts seconds.
	relativeTimeLiveAge = time.Minute
)

var (
	// relativeTimes is whether times in tables are shown relative to now, e.g. "3 minutes ago".
	relativeTimes atomic.Bool
	// timeFormatGeneration changes whenever formatted times change (the setting, or the time passing while they are
	// relative), so tables know to render their cells again, see TimeFormatGeneration.
	timeFormatGeneration atomic.Uint64
	// newestFormattedTime is the newest time (unix nanoseconds) formatted relatively since the generation changed,
	// i.e. the newest one shown. While it counts seconds, relative times are updated every second.
	newestFormattedTime atomic.Int64
)

// InitTimeFormat loads the setting from state.Current. Called when the UI is created.
func InitTimeFormat() {
	relativeTimes.Store(state.Current.Toggle(toggleRelativeTimes, false))
	refreshRelativeTimes()
}

// IsRelativeTimes returns whether times are shown relative to now.
func IsRelativeTimes() bool {
	return relativeTimes.Load()
}

// ToggleRelativeTimes switches between relative ("3 minutes ago") and absolute times, and remembers the setting.
// Tables show the new format the next time they are drawn.
func ToggleRelativeTimes() {
	enabled := !relativeTimes.Load()
	relativeTimes.Store(enabled)
	state.Current.SetToggle(toggleRelativeTimes, enabled)
	refreshRelativeTimes()
}

// FormatTime formats a time for tables: relative to now (e.g. "3 minutes ago", see humanize.Time) or absolute,
// depending on the setting.
func FormatTime(t time.Time) string {
	if relativeTimes.Load() {
		recordFormattedTime(t)
		return humanize.Time(t)
	}
	return t.Format(theme.Style.Format.DateTime)
}

// TimeFormatGeneration changes whenever FormatTime may return something else for the same time. Tables compare it
// when they are drawn and render their cells again if it changed (see table.RowSelectionTable).
func TimeFormatGeneration() uint64 {
	return timeFormatGeneration.Load()
}

// recordFormattedTime remembers t if it is the newest time formatted since the generation changed.
func recordFormattedTime(t time.Time) {
	nanos := t.UnixNano()
	for {
		newest := newestFormattedTime.Load()
		if nanos <= newest || newestFormattedTime.CompareAndSwap(newest, nanos) {
			return
		}
	}
}

// relativeTimesOutdated returns whether relative times shown since lastRefresh may read differently at now:
// every second while one of them counts seconds ("30 seconds ago"), otherwise every minute. So times tick live
// right after a change, without redrawing every second when nothing is that recent.
func relativeTimesOutdated(now time.Time, lastRefresh time.Time) bool {
	if now.Sub(lastRefresh) >= relativeTimeRefreshInterval {
		return true
	}
	newest := newestFormattedTime.Load()
	return newest != 0 && now.Sub(time.Unix(0, newest)) < relativeTimeLiveAge+relativeTimeLiveInterval
}

// refreshRelativeTimes makes tables render their times again. The tables record the times they show again while
// rendering, see recordFormattedTime.
func refreshRelativeTimes() {
	newestFormattedTime.Store(0)
	timeFormatGeneration.Add(1)
}

// StartRelativeTimeRefresh keeps relative times current: while they are shown, it changes the generation and
// redraws the application when they read differently (see relativeTimesOutdated). Returns a function that stops it.
func StartRelativeTimeRefresh(application *tview.Application) (stop func()) {
	ticker := time.NewTicker(relativeTimeLiveInterval)
	done := make(chan struct{})
	go func() {
		lastRefresh := time.Now()
		for {
			select {
			case <-done:
				return
			case now := <-ticker.C:
				if relativeTimes.Load() && relativeTimesOutdated(now, lastRefresh) {
					lastRefresh = now
					refreshRelativeTimes()
					application.QueueUpdateDraw(func() {})
				}
			}
		}
	}()
	return func() {
		ticker.Stop()
		close(done)
	}
}
