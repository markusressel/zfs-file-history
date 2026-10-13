package pool_activity

import (
	"fmt"
	"slices"
	"time"
	"zfs-file-history/internal/zfs"
)

// TimeLeftMode is what is shown of the time left of the activities, see Monitor.CycleTimeLeftMode.
type TimeLeftMode string

const (
	// TimeLeftRemaining shows how long an activity still runs, e.g. "  2h 10m to go"
	TimeLeftRemaining TimeLeftMode = "remaining"
	// TimeLeftEta shows when it ends, e.g. "done Tue 14:32"
	TimeLeftEta TimeLeftMode = "eta"
	// TimeLeftBoth shows both, e.g. "  2h 10m to go · Tue 14:32"
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

// formatTimeLeft returns what the mode shows of the time left of the activity, e.g. "  2h 10m to go" or
// "done Tue 14:32", with placeholders if it is not known. It has the same width for all activities.
func formatTimeLeft(activity zfs.PoolActivity, mode TimeLeftMode) string {
	remaining, eta := remainingPlaceholder, etaPlaceholder
	if activity.RemainingKnown {
		current := now()
		remaining = formatDuration(activity.Remaining)
		eta = formatEta(current.Add(activity.Remaining), current)
	}
	switch mode {
	case TimeLeftEta:
		return "done " + eta
	case TimeLeftBoth:
		return remaining + " to go · " + eta
	}
	return remaining + " to go"
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

// formatEta formats when an activity ends in 9 columns: the weekday and the time within the next 6 days ("Tue 14:32",
// the weekday of today for today), the date after that ("   Oct 25"), when the weekday would be ambiguous and the time
// does not matter much.
func formatEta(eta time.Time, now time.Time) string {
	year, month, day := now.Date()
	if eta.Before(time.Date(year, month, day+7, 0, 0, 0, 0, now.Location())) {
		return eta.Format("Mon 15:04")
	}
	return fmt.Sprintf("%9s", eta.Format("Jan 2"))
}
