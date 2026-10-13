package pool_status

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// the lines of the tree of devices, like the ones of the tree view of the dataset browser
const (
	treeBranch     = "├─ "
	treeLastBranch = "└─ "
	treeLine       = "│  "
	treeSpace      = "   "
)

// PoolDevicesComponent shows the devices of a pool as a tree, like the config of "zpool status": their state and
// their read, write and checksum errors.
type PoolDevicesComponent struct {
	application *tview.Application
	textView    *tview.TextView
}

func NewPoolDevices(application *tview.Application) *PoolDevicesComponent {
	poolDevices := &PoolDevicesComponent{
		application: application,
	}
	poolDevices.textView = tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(false).
		SetScrollable(true)
	poolDevices.textView.SetBorder(true)
	uiutil.SetupWindow(poolDevices.textView, "Devices")
	return poolDevices
}

// SetStatus shows the devices of the status, or none if it is nil. Must be called on the UI thread.
func (poolDevices *PoolDevicesComponent) SetStatus(status *zfs.PoolStatus) {
	if status == nil {
		uiutil.SetupWindow(poolDevices.textView, "Devices")
		poolDevices.textView.SetText("")
		return
	}
	uiutil.SetupWindow(poolDevices.textView, devicesTitle(status.Devices))
	// keep the scroll position while the status is refreshed
	row, column := poolDevices.textView.GetScrollOffset()
	poolDevices.textView.SetText(formatDevices(status.Devices))
	poolDevices.textView.ScrollTo(row, column)
}

// devicesTitle returns the title of the devices, with the number of devices with errors, e.g. "Devices · 2 with
// errors".
func devicesTitle(devices []*zfs.PoolDevice) string {
	withErrors := 0
	for _, device := range devices {
		if device.HasErrors() {
			withErrors++
		}
	}
	if withErrors == 0 {
		return "Devices"
	}
	return fmt.Sprintf("Devices · %d with errors", withErrors)
}

// formatDevices formats the devices as a table: the tree of their names, their state, their errors and their note.
func formatDevices(devices []*zfs.PoolDevice) string {
	prefixes := devicePrefixes(devices)

	nameWidth := len("NAME")
	stateWidth := len("STATE")
	counterWidth := len("CKSUM")
	for i, device := range devices {
		nameWidth = max(nameWidth, utf8.RuneCountInString(prefixes[i])+len(device.Name))
		stateWidth = max(stateWidth, len(device.State))
		for _, counter := range []uint64{device.Read, device.Write, device.Checksum} {
			counterWidth = max(counterWidth, len(strconv.FormatUint(counter, 10)))
		}
	}

	var out strings.Builder
	header := fmt.Sprintf(" %-*s  %-*s  %*s %*s %*s", nameWidth, "NAME", stateWidth, "STATE",
		counterWidth, "READ", counterWidth, "WRITE", counterWidth, "CKSUM")
	out.WriteString("[::b]" + txwidgets.Span(theme.Colors.Layout.Table.HeaderForeground, "%s", header) + "[::-]\n")

	for i, device := range devices {
		out.WriteString(" " + txwidgets.Span(theme.Colors.Layout.Table.TreeLines, "%s", prefixes[i]))
		if device.IsGroup() {
			out.WriteString(txwidgets.Span(theme.Colors.Pool.Group, "%s", device.Name) + "\n")
			continue
		}

		padding := nameWidth - utf8.RuneCountInString(prefixes[i]) - len(device.Name)
		out.WriteString(txwidgets.Span(tcell.ColorWhite, "%s", device.Name) + strings.Repeat(" ", padding) + "  ")

		// like "zpool status": spares have no error counters, their note is aligned with the others
		counters := ""
		if !isSpare(device) || device.HasErrors() {
			for _, counter := range []uint64{device.Read, device.Write, device.Checksum} {
				counters += " " + txwidgets.Span(counterColor(counter), "%*d", counterWidth, counter)
			}
		} else if device.Note != "" {
			counters = strings.Repeat(" ", 3*(counterWidth+1))
		}
		note := ""
		if device.Note != "" {
			note = "  " + txwidgets.Span(noteColor(device), "%s", device.Note)
		}

		state := device.State
		if counters != "" {
			state = fmt.Sprintf("%-*s ", stateWidth, state)
		}
		out.WriteString(txwidgets.Span(StateColor(device.State, tcell.ColorWhite), "%s", state) + counters + note + "\n")
	}
	return out.String()
}

// isSpare returns whether the device is a hot spare, by its state.
func isSpare(device *zfs.PoolDevice) bool {
	return device.State == zfs.PoolStateAvail || device.State == zfs.PoolStateInUse
}

// counterColor returns the color of an error counter: whether there were errors.
func counterColor(counter uint64) tcell.Color {
	if counter > 0 {
		return theme.Colors.Pool.Errors
	}
	return theme.Colors.Pool.NoErrors
}

// noteColor returns the color of the note of a device, e.g. "(resilvering)": the color of its state, unless it is
// online, so the notes of healthy devices do not look like a problem.
func noteColor(device *zfs.PoolDevice) tcell.Color {
	if device.State == zfs.PoolStateOnline {
		return theme.Colors.Pool.Degraded
	}
	return StateColor(device.State, tcell.ColorWhite)
}

// devicePrefixes returns the tree lines in front of the name of each device, e.g. "│  ├─ ". The pool and the
// groups (depth 0) are the roots.
func devicePrefixes(devices []*zfs.PoolDevice) []string {
	prefixes := make([]string, len(devices))
	// hasNextSibling[depth] is whether the last device seen on that depth has a sibling below it
	var hasNextSibling []bool
	for i, device := range devices {
		for len(hasNextSibling) < device.Depth {
			hasNextSibling = append(hasNextSibling, false)
		}
		hasNextSibling = append(hasNextSibling[:device.Depth], !isLastSibling(devices, i))
		if device.Depth == 0 {
			continue
		}

		var prefix strings.Builder
		for depth := 1; depth < device.Depth; depth++ {
			if hasNextSibling[depth] {
				prefix.WriteString(treeLine)
			} else {
				prefix.WriteString(treeSpace)
			}
		}
		if hasNextSibling[device.Depth] {
			prefix.WriteString(treeBranch)
		} else {
			prefix.WriteString(treeLastBranch)
		}
		prefixes[i] = prefix.String()
	}
	return prefixes
}

// isLastSibling returns whether no sibling follows the device at index: the next device that is not a descendant
// is on a higher level (or there is none).
func isLastSibling(devices []*zfs.PoolDevice, index int) bool {
	depth := devices[index].Depth
	for _, device := range devices[index+1:] {
		if device.Depth <= depth {
			return device.Depth < depth
		}
	}
	return true
}

func (poolDevices *PoolDevicesComponent) HasFocus() bool {
	return poolDevices.textView.HasFocus()
}

func (poolDevices *PoolDevicesComponent) Focus() {
	poolDevices.application.SetFocus(poolDevices.textView)
}

func (poolDevices *PoolDevicesComponent) GetLayout() *tview.TextView {
	return poolDevices.textView
}
