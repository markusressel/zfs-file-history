package pool_status

import (
	"testing"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/stretchr/testify/assert"
)

func TestStateColor(t *testing.T) {
	tests := map[string]tcell.Color{
		zfs.PoolStateOnline:    theme.Colors.Pool.Online,
		zfs.PoolStateAvail:     theme.Colors.Pool.Online,
		zfs.PoolStateDegraded:  theme.Colors.Pool.Degraded,
		zfs.PoolStateInUse:     theme.Colors.Pool.Degraded,
		zfs.PoolStateFaulted:   theme.Colors.Pool.Faulted,
		zfs.PoolStateUnavail:   theme.Colors.Pool.Faulted,
		zfs.PoolStateSuspended: theme.Colors.Pool.Faulted,
		zfs.PoolStateOffline:   theme.Colors.Pool.Offline,
		zfs.PoolStateRemoved:   theme.Colors.Pool.Offline,
		"SOMETHING":            tcell.ColorWhite,
		"":                     tcell.ColorWhite,
	}
	for state, expected := range tests {
		t.Run(state, func(t *testing.T) {
			assert.Equal(t, expected, StateColor(state, tcell.ColorWhite))
		})
	}
}
