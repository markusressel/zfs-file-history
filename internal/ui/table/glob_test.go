package table

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMatchesGlob(t *testing.T) {
	tests := []struct {
		value    string
		pattern  string
		expected bool
	}{
		// without wildcards: anywhere in the value
		{"zfs-auto-snap_daily-2026-10-01", "daily", true},
		{"zfs-auto-snap_daily-2026-10-01", "weekly", false},
		{"anything", "", true},
		// case-insensitive
		{"zfs-auto-snap_Daily-2026", "DAILY", true},
		{"zfs-auto-snap_Daily-2026", "*daily*", true},
		// wildcards match the whole value
		{"daily-2026-10-01", "daily*", true},
		{"zfs-auto-snap_daily-2026", "daily*", false},
		{"zfs-auto-snap_daily-2026", "*daily*", true},
		{"2026-10-01-220000", "*-22????", true},
		{"2026-10-01-221500", "*-22????", true},
		{"2026-10-01-120000", "*-22????", false},
		{"daily-1", "daily-[0-9]", true},
		{"daily-x", "daily-[0-9]", false},
		// '*' also matches across '/'
		{"rpool/arch/home", "rpool*home", true},
		{"rpool/arch/home", "rpool/*/home", true},
		{"rpool/arch/home", "rpool/home", false},
		// incomplete patterns (while typing) match as plain text
		{"daily-[0-", "[0-", true},
		{"daily-1", "[0-", false},
	}
	for _, test := range tests {
		t.Run(test.value+" "+test.pattern, func(t *testing.T) {
			assert.Equal(t, test.expected, MatchesGlob(test.value, test.pattern))
		})
	}
}
