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

// relativeTimeRefreshInterval is how often relative times are updated ("1 minute ago" becomes "2 minutes ago").
const relativeTimeRefreshInterval = time.Minute

var (
	// relativeTimes is whether times in tables are shown relative to now, e.g. "3 minutes ago".
	relativeTimes atomic.Bool
	// timeFormatGeneration changes whenever formatted times change (the setting, or the time passing while they are
	// relative), so tables know to render their cells again, see TimeFormatGeneration.
	timeFormatGeneration atomic.Uint64
)

// InitTimeFormat loads the setting from state.Current. Called when the UI is created.
func InitTimeFormat() {
	relativeTimes.Store(state.Current.Toggle(toggleRelativeTimes, false))
	timeFormatGeneration.Add(1)
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
	timeFormatGeneration.Add(1)
}

// FormatTime formats a time for tables: relative to now (e.g. "3 minutes ago", see humanize.Time) or absolute,
// depending on the setting.
func FormatTime(t time.Time) string {
	if relativeTimes.Load() {
		return humanize.Time(t)
	}
	return t.Format(theme.Style.Format.DateTime)
}

// TimeFormatGeneration changes whenever FormatTime may return something else for the same time. Tables compare it
// when they are drawn and render their cells again if it changed (see table.RowSelectionTable).
func TimeFormatGeneration() uint64 {
	return timeFormatGeneration.Load()
}

// StartRelativeTimeRefresh keeps relative times current: while they are shown, it changes the generation and
// redraws the application every minute. Returns a function that stops it.
func StartRelativeTimeRefresh(application *tview.Application) (stop func()) {
	ticker := time.NewTicker(relativeTimeRefreshInterval)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if relativeTimes.Load() {
					timeFormatGeneration.Add(1)
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
