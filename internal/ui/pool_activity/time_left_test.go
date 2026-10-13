package pool_activity

import (
	"testing"
	"time"
	"unicode/utf8"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/zfs"

	"github.com/stretchr/testify/assert"
)

// setNow sets the time formatTimeLeft takes as now, until the end of the test.
func setNow(t *testing.T, current time.Time) {
	original := now
	t.Cleanup(func() { now = original })
	now = func() time.Time { return current }
}

func TestTimeLeftMode_Next(t *testing.T) {
	assert.Equal(t, TimeLeftEta, TimeLeftRemaining.Next())
	assert.Equal(t, TimeLeftBoth, TimeLeftEta.Next())
	assert.Equal(t, TimeLeftRemaining, TimeLeftBoth.Next())
	assert.Equal(t, TimeLeftRemaining, TimeLeftMode("unknown").Next())
}

func TestFormatTimeLeft(t *testing.T) {
	// a Tuesday
	setNow(t, time.Date(2026, 10, 13, 12, 22, 0, 0, time.Local))
	known := zfs.PoolActivity{Remaining: 2*time.Hour + 10*time.Minute + 31*time.Second, RemainingKnown: true}

	tests := []struct {
		mode    TimeLeftMode
		known   string
		unknown string
	}{
		{TimeLeftRemaining, "  2h 10m to go", " --h --m to go"},
		{TimeLeftEta, "done Tue 14:32", "done --- --:--"},
		{TimeLeftBoth, "  2h 10m to go · Tue 14:32", " --h --m to go · --- --:--"},
	}
	for _, test := range tests {
		assert.Equal(t, test.known, formatTimeLeft(known, test.mode), test.mode)
		assert.Equal(t, test.unknown, formatTimeLeft(zfs.PoolActivity{}, test.mode), test.mode)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		remaining time.Duration
		expected  string
	}{
		{0, "  0m 00s"},
		{-time.Second, "  0m 00s"},
		{42 * time.Second, "  0m 42s"},
		{42*time.Minute + 5*time.Second + 600*time.Millisecond, " 42m 05s"},
		{2*time.Hour + 10*time.Minute + 31*time.Second, "  2h 10m"},
		{24*time.Hour - time.Second, " 23h 59m"},
		{31*time.Hour + 53*time.Minute + 32*time.Second, "  1d 07h"},
		{12*24*time.Hour + 7*time.Hour + 59*time.Minute, " 12d 07h"},
		{123*24*time.Hour + 23*time.Hour, "123d 23h"},
		{5000 * 24 * time.Hour, "999d 23h"},
	}
	for _, test := range tests {
		formatted := formatDuration(test.remaining)
		assert.Equal(t, test.expected, formatted, test.remaining.String())
		assert.Len(t, formatted, len(remainingPlaceholder), "as wide as the placeholder")
	}
}

func TestFormatEta(t *testing.T) {
	// a Tuesday
	current := time.Date(2026, 10, 13, 12, 22, 0, 0, time.Local)
	tests := []struct {
		eta      time.Time
		expected string
	}{
		{current.Add(2*time.Hour + 10*time.Minute), "Tue 14:32"},
		{time.Date(2026, 10, 14, 3, 10, 0, 0, time.Local), "Wed 03:10"},
		// the last day before the weekday of today comes again
		{time.Date(2026, 10, 19, 23, 59, 0, 0, time.Local), "Mon 23:59"},
		{time.Date(2026, 10, 20, 0, 0, 0, 0, time.Local), "   Oct 20"},
		{time.Date(2026, 11, 5, 18, 0, 0, 0, time.Local), "    Nov 5"},
	}
	for _, test := range tests {
		formatted := formatEta(test.eta, current)
		assert.Equal(t, test.expected, formatted, test.eta.String())
		assert.Equal(t, utf8.RuneCountInString(etaPlaceholder), utf8.RuneCountInString(formatted), "as wide as the placeholder")
	}
}

func TestLayoutLines_Modes(t *testing.T) {
	setNow(t, time.Date(2026, 10, 13, 12, 22, 0, 0, time.Local))
	activities := []zfs.PoolActivity{
		{Pool: "tank", Kind: "scrub", Progress: 30, Remaining: 2*time.Hour + 10*time.Minute, RemainingKnown: true},
		{Pool: "backup", Kind: "trim", Progress: 3},
	}

	for _, mode := range timeLeftModes {
		lines := layoutLines(activities, 120, mode)
		width := utf8.RuneCountInString(formatRemaining(zfs.PoolActivity{}, mode))
		for _, line := range lines {
			assert.Equal(t, width, utf8.RuneCountInString(testutil.StripTags(line.remaining)), "aligned in %s", mode)
			assert.Equal(t, 120-line.barStart-width-1, line.barWidth, mode)
		}
	}
	lines := layoutLines(activities, 120, TimeLeftEta)
	assert.Equal(t, "  done Tue 14:32", testutil.StripTags(lines[0].remaining))
	assert.Equal(t, "  done --- --:--", testutil.StripTags(lines[1].remaining))
}
