package pool_status

import (
	"context"
	"strings"
	"time"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// loadPoolStatus is replaceable in tests.
var loadPoolStatus = zfs.GetPoolStatus

// scanRefreshInterval is how often the status is reloaded while a scrub or resilver is running, to show its
// progress. Replaceable in tests.
var scanRefreshInterval = 5 * time.Second

// progressBarWidth is the width of the progress bar of a running scrub or resilver, without the percentage.
const progressBarWidth = 30

type Event interface{}

// StatusLoadedEvent is emitted on the UI thread whenever the displayed status changed: Status is nil if there is
// none (no pool selected, or it could not be loaded).
type StatusLoadedEvent struct {
	Status *zfs.PoolStatus
}

// PoolStatusComponent shows the status of a pool, like the top of "zpool status": its state, what is wrong with it
// and what to do about it, the last or running scrub or resilver, and the data errors. The devices are shown by
// PoolDevicesComponent, see StatusLoadedEvent.
type PoolStatusComponent struct {
	Events *util.Emitter[Event]

	application *tview.Application
	textView    *tview.TextView
	container   *uiutil.LoadingContainer
	loader      *uiutil.DataLoader[*zfs.PoolStatus]

	// All fields below are only accessed on the UI thread.

	// name is the name of the requested pool, "" if none
	name string
	// refreshTimer reloads the status while a scrub or resilver is running, see scheduleScanRefresh
	refreshTimer *time.Timer
}

func NewPoolStatus(application *tview.Application) *PoolStatusComponent {
	poolStatus := &PoolStatusComponent{
		Events:      util.NewEmitter[Event](),
		application: application,
	}

	poolStatus.textView = tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(false).
		SetScrollable(true)
	poolStatus.textView.SetBorder(true)
	uiutil.SetupWindow(poolStatus.textView, "Pool")

	poolStatus.container = uiutil.NewLoadingContainer(application, poolStatus.textView, "Pool", "Loading pool status...")

	poolStatus.loader = uiutil.NewDataLoader[*zfs.PoolStatus](application).
		OnStart(func() {
			poolStatus.container.SetIsLoading(true)
		}).
		OnLoad(func(status *zfs.PoolStatus) {
			poolStatus.container.SetIsLoading(false)
			poolStatus.show(status, nil)
		}).
		OnError(func(err error) {
			poolStatus.container.SetIsLoading(false)
			poolStatus.show(nil, err)
		})

	return poolStatus
}

// SetPool shows the status of the pool with the given name, or nothing if it is "".
// Must be called on the UI thread.
func (poolStatus *PoolStatusComponent) SetPool(name string) {
	if name == poolStatus.name {
		return
	}
	poolStatus.name = name
	poolStatus.load(false)
}

// GetPool returns the name of the requested pool, "" if none.
func (poolStatus *PoolStatusComponent) GetPool() string {
	return poolStatus.name
}

// Refresh reloads the status of the requested pool, showing the old one while loading.
// Must be called on the UI thread.
func (poolStatus *PoolStatusComponent) Refresh() {
	poolStatus.load(true)
}

// load starts loading the status of the requested pool in the background. Must be called on the UI thread.
func (poolStatus *PoolStatusComponent) load(quietly bool) {
	poolStatus.stopScanRefresh()
	name := poolStatus.name
	if name == "" {
		// e.g. no pools: a load still running must not show its result
		poolStatus.loader.LoadQuietly(func(ctx context.Context) (*zfs.PoolStatus, error) { return nil, nil })
		return
	}

	loadFunc := func(ctx context.Context) (*zfs.PoolStatus, error) {
		return loadPoolStatus(name)
	}
	if quietly {
		poolStatus.loader.LoadQuietly(loadFunc)
	} else {
		poolStatus.loader.Load(loadFunc)
	}
}

// show displays the status (nil: none), or the error it could not be loaded with. Runs on the UI thread.
func (poolStatus *PoolStatusComponent) show(status *zfs.PoolStatus, err error) {
	switch {
	case err != nil:
		uiutil.SetupWindow(poolStatus.textView, "Pool: "+poolStatus.name)
		poolStatus.textView.SetText(txwidgets.Span(theme.Colors.Dialog.Error, " Could not load the status: %s", err.Error()))
	case status == nil:
		uiutil.SetupWindow(poolStatus.textView, "Pool")
		poolStatus.textView.SetText("")
	default:
		uiutil.SetupWindow(poolStatus.textView, "Pool: "+status.Name)
		poolStatus.textView.SetText(formatStatus(status))
		if status.IsScanning() {
			poolStatus.scheduleScanRefresh()
		}
	}
	poolStatus.Events.Emit(StatusLoadedEvent{Status: status})
}

// scheduleScanRefresh reloads the status after scanRefreshInterval, to show the progress of the running scrub or
// resilver. Runs on the UI thread.
func (poolStatus *PoolStatusComponent) scheduleScanRefresh() {
	poolStatus.stopScanRefresh()
	var timer *time.Timer
	timer = time.AfterFunc(scanRefreshInterval, func() {
		poolStatus.application.QueueUpdateDraw(func() {
			// not if it was stopped, or replaced, in the meantime
			if poolStatus.refreshTimer == timer {
				poolStatus.refreshTimer = nil
				poolStatus.load(true)
			}
		})
	})
	poolStatus.refreshTimer = timer
}

func (poolStatus *PoolStatusComponent) stopScanRefresh() {
	if poolStatus.refreshTimer != nil {
		poolStatus.refreshTimer.Stop()
		poolStatus.refreshTimer = nil
	}
}

// formatStatus formats the fields of the status that are set, like "zpool status" does: the name of each field in
// front of its first line, its other lines aligned with the first one.
func formatStatus(status *zfs.PoolStatus) string {
	type field struct {
		name  string
		value string
		color tcell.Color
	}
	fields := []field{
		{"State", status.State, StateColor(status.State, tcell.ColorWhite)},
		{"Status", status.Status, theme.Colors.Pool.Degraded},
		{"Action", status.Action, tcell.ColorWhite},
		{"See", status.See, tcell.ColorLightBlue},
		{"Scan", status.Scan, tcell.ColorWhite},
	}
	for _, other := range status.Other {
		fields = append(fields, field{capitalize(other.Name), other.Value, tcell.ColorWhite})
	}
	fields = append(fields, field{"Errors", status.Errors, errorsColor(status.Errors)})

	nameWidth := 0
	for _, f := range fields {
		nameWidth = max(nameWidth, len(f.name))
	}
	indent := strings.Repeat(" ", nameWidth+4)

	var out strings.Builder
	for _, f := range fields {
		if f.value == "" {
			continue
		}
		for i, line := range strings.Split(f.value, "\n") {
			if i == 0 {
				out.WriteString(txwidgets.Span(theme.Colors.Layout.Table.Accent, " %*s:", nameWidth, f.name) + "  ")
			} else {
				out.WriteString(indent)
			}
			out.WriteString(txwidgets.Span(f.color, "%s", line) + "\n")
		}
		if f.name == "Scan" && status.IsScanning() {
			out.WriteString(indent + uiutil.FormatProgressBar(status.ScanProgress, progressBarWidth) + "\n")
		}
	}
	return out.String()
}

// errorsColor returns the color of the summary of the data errors: all good, or not.
func errorsColor(errors string) tcell.Color {
	if errors == "No known data errors" {
		return theme.Colors.Pool.Online
	}
	return theme.Colors.Pool.Faulted
}

// capitalize returns the text with its first letter in upper case, e.g. "Remove" for "remove".
func capitalize(text string) string {
	if text == "" {
		return text
	}
	return strings.ToUpper(text[:1]) + text[1:]
}

func (poolStatus *PoolStatusComponent) HasFocus() bool {
	return poolStatus.container.HasFocus()
}

func (poolStatus *PoolStatusComponent) Focus() {
	poolStatus.application.SetFocus(poolStatus.container)
}

func (poolStatus *PoolStatusComponent) GetLayout() *uiutil.LoadingContainer {
	return poolStatus.container
}

// IsLoading returns whether the status is being loaded (its result, or error, is not shown yet).
func (poolStatus *PoolStatusComponent) IsLoading() bool {
	return poolStatus.loader.IsLoading()
}

// ContentSize returns the size needed to show all of the text, including the border. Must be called on the UI
// thread.
func (poolStatus *PoolStatusComponent) ContentSize() (width int, height int) {
	return uiutil.TextViewContentSize(poolStatus.textView)
}
