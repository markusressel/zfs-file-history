package pool_activity

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
)

// TimeLeftMode is what is shown of the time left of the activities, see Monitor.CycleTimeLeftMode.
type TimeLeftMode string

const (
	// TimeLeftRemaining shows how long an activity still runs, e.g. "⧗    2h 10m"
	TimeLeftRemaining TimeLeftMode = "remaining"
	// TimeLeftEta shows when it ends, e.g. "⚑     14:32" (today) or "⚑ Wed 14:32"
	TimeLeftEta TimeLeftMode = "eta"
	// TimeLeftBoth shows both, e.g. "⧗    2h 10m  ⚑     14:32"
	TimeLeftBoth TimeLeftMode = "both"
)

// timeLeftModes are the modes in the order they are cycled through.
var timeLeftModes = []TimeLeftMode{TimeLeftRemaining, TimeLeftEta, TimeLeftBoth}

// Next returns the mode after this one, see timeLeftModes.
func (mode TimeLeftMode) Next() TimeLeftMode {
	// an unknown mode (-1) goes to the first one
	return timeLeftModes[(slices.Index(timeLeftModes, mode)+1)%len(timeLeftModes)]
}

// Name returns what the mode shows, for the command menu, e.g. "the ETA".
func (mode TimeLeftMode) Name() string {
	switch mode {
	case TimeLeftEta:
		return "the ETA"
	case TimeLeftBoth:
		return "the time left and the ETA"
	}
	return "the time left"
}

// Placeholders stand for a time left that is not known yet, as wide as formatDuration and formatEta, so the bars do
// not change their width once it is.
const (
	remainingPlaceholder = " --h --m"
	etaPlaceholder       = "--- --:--"
)

// now is replaceable in tests.
var now = time.Now

// Symbols in front of the time left and the ETA, one column wide each (unlike emoji, which take two in most
// terminals).
const (
	remainingSymbol = "⧗"
	etaSymbol       = "⚑"
)

// timeLeftField is a part of what is shown of the time left, see timeLeftFields.
type timeLeftField struct {
	text string
	// background is the one of the field, hoverBackground the one while it is below the mouse
	background      tcell.Color
	hoverBackground tcell.Color
}

// timeLeftFields returns what the mode shows of the time left of the activity: the time left (e.g. "⧗    2h 10m"),
// the ETA (e.g. "⚑     14:32") or both, with placeholders if it is not known. They have the same width for all
// activities, and the time left and the ETA have the same width, so the bars keep their width when the mode changes
// between them.
func timeLeftFields(activity zfs.PoolActivity, mode TimeLeftMode) []timeLeftField {
	remaining, eta := remainingPlaceholder, etaPlaceholder
	if activity.RemainingKnown {
		current := now()
		remaining = formatDuration(activity.Remaining)
		eta = formatEta(current.Add(activity.Remaining), current)
	}
	colors := theme.Colors.Activity
	remainingField := timeLeftField{fmt.Sprintf("%s %*s", remainingSymbol, len(etaPlaceholder), remaining),
		colors.RemainingBackground, colors.RemainingHoverBackground}
	etaField := timeLeftField{etaSymbol + " " + eta, colors.EtaBackground, colors.EtaHoverBackground}
	switch mode {
	case TimeLeftEta:
		return []timeLeftField{etaField}
	case TimeLeftBoth:
		return []timeLeftField{remainingField, etaField}
	}
	return []timeLeftField{remainingField}
}

// formatTimeLeft returns the text of the fields of the time left (see timeLeftFields), e.g. to write it into a bar.
func formatTimeLeft(activity zfs.PoolActivity, mode TimeLeftMode) string {
	var texts []string
	for _, field := range timeLeftFields(activity, mode) {
		texts = append(texts, field.text)
	}
	return strings.Join(texts, "  ")
}

// formatDuration formats a time left in its two largest units, in 8 columns whatever format zpool status printed it
// in, so the bars do not change their width when it changes: "  0m 42s", " 42m 05s", "  2h 10m", "  1d 07h",
// "123d 07h". Longer than 999 days is shown as 999 days.
func formatDuration(remaining time.Duration) string {
	seconds := int64(max(remaining, 0) / time.Second)
	days, hours, minutes := seconds/86400, seconds%86400/3600, seconds%3600/60
	switch {
	case days > 999:
		return "999d 23h"
	case days > 0:
		return fmt.Sprintf("%3dd %02dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%3dh %02dm", hours, minutes)
	}
	return fmt.Sprintf("%3dm %02ds", minutes, seconds%60)
}

// formatEta formats when an activity ends in 9 columns: the time for today ("    14:32", without the weekday, which
// would suggest another day), the weekday and the time within the next 6 days ("Wed 14:32"), the date after that
// ("   Oct 25"), when the weekday would be ambiguous and the time does not matter much. Right-aligned, so the times
// of all lines are in the same columns.
func formatEta(eta time.Time, now time.Time) string {
	year, month, day := now.Date()
	etaYear, etaMonth, etaDay := eta.Date()
	switch {
	case etaYear == year && etaMonth == month && etaDay == day:
		return fmt.Sprintf("%9s", eta.Format("15:04"))
	case eta.Before(time.Date(year, month, day+7, 0, 0, 0, 0, now.Location())):
		return eta.Format("Mon 15:04")
	}
	return fmt.Sprintf("%9s", eta.Format("Jan 2"))
}
