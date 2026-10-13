// Package pool_activity shows the operations running on the pools (e.g. a scrub or a resilver) on all pages, below
// the header.
package pool_activity

import (
	"context"
	"slices"
	"time"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/rivo/tview"
)

// loadPoolStatuses is replaceable in tests.
var loadPoolStatuses = zfs.ListPoolStatuses

// activeRefreshInterval is how often the statuses are reloaded while something runs, to show its progress.
// idleRefreshInterval is how often they are reloaded otherwise, to notice operations started outside of the
// application even without the event watcher (see zfs.WatchZpoolEvents), which needs the sudoers rule. Replaceable
// in tests.
var (
	activeRefreshInterval = 5 * time.Second
	idleRefreshInterval   = time.Minute
)

// Monitor knows the operations running on all pools, see zfs.PoolStatus.Activities. It reloads the status of all
// pools periodically, and on Refresh. One for the whole application, shown by a Bar on each page. Others can use
// the loaded statuses too (see OnStatusesLoaded), so they do not have to load them again while something runs.
type Monitor struct {
	application *tview.Application
	loader      *uiutil.DataLoader[[]*zfs.PoolStatus]

	// All fields below are only accessed on the UI thread.

	activities []zfs.PoolActivity
	// listeners are called when the activities changed, see OnChanged
	listeners []func()
	// statusListeners are called with the statuses of every load, see OnStatusesLoaded
	statusListeners []func(statuses []*zfs.PoolStatus)
	// refreshTimer reloads the activities, see scheduleRefresh
	refreshTimer *time.Timer
}

// NewMonitor returns a monitor without activities, until the first Refresh.
func NewMonitor(application *tview.Application) *Monitor {
	monitor := &Monitor{application: application}
	monitor.loader = uiutil.NewDataLoader[[]*zfs.PoolStatus](application).
		OnLoad(monitor.setStatuses).
		OnError(func(err error) {
			// e.g. zpool is not installed: there is nothing to show
			monitor.setStatuses(nil)
		})
	return monitor
}

// Refresh reloads the activities in the background, showing the old ones while loading.
// Must be called on the UI thread.
func (monitor *Monitor) Refresh() {
	monitor.stopRefresh()
	monitor.loader.LoadQuietly(func(ctx context.Context) ([]*zfs.PoolStatus, error) {
		return loadPoolStatuses()
	})
}

// Activities returns the operations running on the pools, empty if nothing runs. Must be called on the UI thread.
func (monitor *Monitor) Activities() []zfs.PoolActivity {
	return monitor.activities
}

// OnChanged adds a function that is called on the UI thread whenever the activities changed.
// Must be called on the UI thread.
func (monitor *Monitor) OnChanged(listener func()) {
	monitor.listeners = append(monitor.listeners, listener)
}

// OnStatusesLoaded adds a function that is called on the UI thread with the statuses of all pools whenever they
// were loaded (every few seconds while something runs, see activeRefreshInterval), e.g. to show the progress of a
// scrub elsewhere. Not called if they could not be loaded. Must be called on the UI thread.
func (monitor *Monitor) OnStatusesLoaded(listener func(statuses []*zfs.PoolStatus)) {
	monitor.statusListeners = append(monitor.statusListeners, listener)
}

// setStatuses shows the activities of the loaded statuses (nil: none, e.g. they could not be loaded), and schedules
// the next reload. Runs on the UI thread.
func (monitor *Monitor) setStatuses(statuses []*zfs.PoolStatus) {
	var activities []zfs.PoolActivity
	for _, status := range statuses {
		activities = append(activities, status.Activities()...)
	}
	changed := !slices.Equal(activities, monitor.activities)
	monitor.activities = activities
	if changed {
		for _, listener := range monitor.listeners {
			listener()
		}
	}
	if statuses != nil {
		for _, listener := range monitor.statusListeners {
			listener(statuses)
		}
	}
	monitor.scheduleRefresh()
}

// scheduleRefresh reloads the activities after activeRefreshInterval while something runs, or else after
// idleRefreshInterval. Runs on the UI thread.
func (monitor *Monitor) scheduleRefresh() {
	monitor.stopRefresh()
	interval := idleRefreshInterval
	if len(monitor.activities) > 0 {
		interval = activeRefreshInterval
	}
	var timer *time.Timer
	timer = time.AfterFunc(interval, func() {
		monitor.application.QueueUpdateDraw(func() {
			// not if it was stopped, or replaced, in the meantime
			if monitor.refreshTimer == timer {
				monitor.refreshTimer = nil
				monitor.Refresh()
			}
		})
	})
	monitor.refreshTimer = timer
}

func (monitor *Monitor) stopRefresh() {
	if monitor.refreshTimer != nil {
		monitor.refreshTimer.Stop()
		monitor.refreshTimer = nil
	}
}
