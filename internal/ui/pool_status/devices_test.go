package pool_status

import (
	"strings"
	"testing"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

// resilveringDevices are the devices of a degraded pool with a resilver running, with logs, a cache and spares.
func resilveringDevices() []*zfs.PoolDevice {
	return []*zfs.PoolDevice{
		{Name: "tank", Depth: 0, State: zfs.PoolStateDegraded},
		{Name: "raidz2-0", Depth: 1, State: zfs.PoolStateDegraded},
		{Name: "sda", Depth: 2, State: zfs.PoolStateOnline},
		{Name: "replacing-1", Depth: 2, State: zfs.PoolStateDegraded},
		{Name: "sdc", Depth: 3, State: zfs.PoolStateFaulted, Read: 12, Write: 340, Note: "too many errors"},
		{Name: "sde", Depth: 3, State: zfs.PoolStateOnline, Note: "(resilvering)"},
		{Name: "sdb", Depth: 2, State: zfs.PoolStateOnline, Checksum: 2},
		{Name: "logs", Depth: 0},
		{Name: "nvme0n1p1", Depth: 1, State: zfs.PoolStateOnline},
		{Name: "spares", Depth: 0},
		{Name: "sdf", Depth: 1, State: zfs.PoolStateAvail},
		{Name: "sdg", Depth: 1, State: zfs.PoolStateInUse, Note: "currently in use"},
	}
}

func TestDevicePrefixes(t *testing.T) {
	assert.Equal(t, []string{
		"",
		"└─ ",
		"   ├─ ",
		"   ├─ ",
		"   │  ├─ ",
		"   │  └─ ",
		"   └─ ",
		"",
		"└─ ",
		"",
		"├─ ",
		"└─ ",
	}, devicePrefixes(resilveringDevices()))

	assert.Empty(t, devicePrefixes(nil))
}

func TestDevicePrefixes_SkippedLevel(t *testing.T) {
	// does not happen, but must not crash
	devices := []*zfs.PoolDevice{{Name: "tank"}, {Name: "deep", Depth: 2}, {Name: "sda", Depth: 1}}
	assert.Equal(t, []string{"", "   └─ ", "└─ "}, devicePrefixes(devices))
}

func TestIsLastSibling(t *testing.T) {
	devices := resilveringDevices()
	assert.False(t, isLastSibling(devices, 0), "the groups follow the pool")
	assert.True(t, isLastSibling(devices, 1))
	assert.False(t, isLastSibling(devices, 3), "sdb follows the children of replacing-1")
	assert.True(t, isLastSibling(devices, 6))
	assert.True(t, isLastSibling(devices, 11))
}

func TestFormatDevices(t *testing.T) {
	text := testutil.StripTags(formatDevices(resilveringDevices()))

	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	assert.Equal(t, []string{
		" NAME               STATE      READ WRITE CKSUM",
		" tank               DEGRADED      0     0     0",
		" └─ raidz2-0        DEGRADED      0     0     0",
		"    ├─ sda          ONLINE        0     0     0",
		"    ├─ replacing-1  DEGRADED      0     0     0",
		"    │  ├─ sdc       FAULTED      12   340     0  too many errors",
		"    │  └─ sde       ONLINE        0     0     0  (resilvering)",
		"    └─ sdb          ONLINE        0     0     2",
		" logs",
		" └─ nvme0n1p1       ONLINE        0     0     0",
		" spares",
		" ├─ sdf             AVAIL",
		" └─ sdg             INUSE                        currently in use",
	}, lines)
}

func TestFormatDevices_WideCounters(t *testing.T) {
	devices := []*zfs.PoolDevice{{Name: "tank", State: zfs.PoolStateOnline, Checksum: 1234567}}
	lines := strings.Split(testutil.StripTags(formatDevices(devices)), "\n")
	assert.Equal(t, " NAME  STATE      READ   WRITE   CKSUM", lines[0])
	assert.Equal(t, " tank  ONLINE        0       0 1234567", lines[1])
}

func TestDevicesTitle(t *testing.T) {
	assert.Equal(t, "Devices", devicesTitle(nil))
	assert.Equal(t, "Devices · 2 with errors", devicesTitle(resilveringDevices()))
}

func TestCounterAndNoteColors(t *testing.T) {
	assert.Equal(t, theme.Colors.Pool.NoErrors, counterColor(0))
	assert.Equal(t, theme.Colors.Pool.Errors, counterColor(1))

	assert.Equal(t, theme.Colors.Pool.Degraded, noteColor(&zfs.PoolDevice{State: zfs.PoolStateOnline, Note: "(resilvering)"}))
	assert.Equal(t, theme.Colors.Pool.Faulted, noteColor(&zfs.PoolDevice{State: zfs.PoolStateFaulted, Note: "too many errors"}))
	assert.Equal(t, tcell.ColorWhite, noteColor(&zfs.PoolDevice{State: "OTHER", Note: "?"}))
}

func TestPoolDevices_SetStatus(t *testing.T) {
	app := tview.NewApplication()
	devices := NewPoolDevices(app)

	devices.SetStatus(&zfs.PoolStatus{Name: "tank", Devices: resilveringDevices()})
	assert.Contains(t, devices.GetLayout().GetTitle(), "Devices · 2 with errors")
	assert.Contains(t, devices.GetLayout().GetText(true), "replacing-1")

	// the scroll position is kept while refreshed
	devices.GetLayout().ScrollTo(3, 0)
	devices.SetStatus(&zfs.PoolStatus{Name: "tank", Devices: resilveringDevices()})
	row, _ := devices.GetLayout().GetScrollOffset()
	assert.Equal(t, 3, row)

	devices.SetStatus(nil)
	assert.Equal(t, " Devices ", devices.GetLayout().GetTitle())
	assert.Empty(t, devices.GetLayout().GetText(true))
}

func TestPoolDevices_Focus(t *testing.T) {
	app := tview.NewApplication()
	devices := NewPoolDevices(app)
	assert.False(t, devices.HasFocus())
	devices.Focus()
	assert.True(t, devices.HasFocus())
}
