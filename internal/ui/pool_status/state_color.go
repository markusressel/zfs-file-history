package pool_status

import (
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
)

// StateColor returns the color of a health state of a pool or device, e.g. "DEGRADED", see theme.PoolColors.
// Unknown states use fallback.
func StateColor(state string, fallback tcell.Color) tcell.Color {
	switch state {
	case zfs.PoolStateOnline, zfs.PoolStateAvail:
		return theme.Colors.Pool.Online
	case zfs.PoolStateDegraded, zfs.PoolStateInUse:
		return theme.Colors.Pool.Degraded
	case zfs.PoolStateFaulted, zfs.PoolStateUnavail, zfs.PoolStateSuspended:
		return theme.Colors.Pool.Faulted
	case zfs.PoolStateOffline, zfs.PoolStateRemoved:
		return theme.Colors.Pool.Offline
	}
	return fallback
}
