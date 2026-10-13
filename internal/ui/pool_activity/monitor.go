// Package pool_activity shows the operations running on the pools (e.g. a scrub or a resilver) and their problems
// (e.g. data errors) on all pages, below the header.
package pool_activity

import (
	"context"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/state"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/rivo/tview"
)

// loadPoolStatuses, loadDataErrors and findObjects are replaceable in tests.
var (
	loadPoolStatuses = zfs.ListPoolStatuses
	loadDataErrors   = zfs.ListDataErrors
	findObjects      = zfs.FindObjects
)

// The settings of this package in state.Current.
const (
	stateNamespace       = "poolActivity"
	stateKeyTimeLeftMode = "timeLeftMode"
)

// objectSearchRedrawInterval is how often the progress of the object search is shown, see FindObjects.
const objectSearchRedrawInterval = 250 * time.Millisecond

// dataErrorsRefreshInterval is how long RefreshDataErrors waits after the last load of the damaged files: listing
// them runs sudo, which logs every call, and snapshots may be created every minute (e.g. by sanoid). Replaceable in
// tests.
var dataErrorsRefreshInterval = time.Minute

// dataErrorLoad is the result of a load of the damaged files: the index, and the version of the object locations
// (see Monitor.locationsVersion) it has.
type dataErrorLoad struct {
	index            *zfs.DataErrorIndex
	locationsVersion int
}

// activeRefreshInterval is how often the statuses are reloaded while something runs, to show its progress.
// idleRefreshInterval is how often they are reloaded otherwise, to notice operations started outside of the
// application even without the event watcher (see zfs.WatchZpoolEvents), which needs the sudoers rule. Replaceable
// in tests.
var (
	activeRefreshInterval = 5 * time.Second
	idleRefreshInterval   = time.Minute
)

// Monitor knows the operations running on all pools (see zfs.PoolStatus.Activities) and their problems (see
// zfs.PoolStatus.Alerts). It reloads the status of all pools periodically, and on Refresh. One for the whole
// application, shown by a Bar and an AlertBar on each page. Others can use
// the loaded statuses too (see OnStatusesLoaded), so they do not have to load them again while something runs.
type Monitor struct {
	application *tview.Application
	loader      *uiutil.DataLoader[[]*zfs.PoolStatus]
	// dataErrorLoader loads the damaged files, see DataErrors
	dataErrorLoader *uiutil.DataLoader[dataErrorLoad]

	// All fields below are only accessed on the UI thread.

	activities []zfs.PoolActivity
	// alerts are the problems of all pools, most severe first
	alerts []zfs.PoolAlert
	// listeners are called when the activities or alerts changed, see OnChanged
	listeners []func()
	// statusListeners are called with the statuses of every load, see OnStatusesLoaded
	statusListeners []func(statuses []*zfs.PoolStatus)
	// refreshTimer reloads the activities, see scheduleRefresh
	refreshTimer *time.Timer
	// dataErrors are the damaged files of all pools, nil while there are none (or they are not loaded yet)
	dataErrors *zfs.DataErrorIndex
	// dataErrorsKey are the data errors of the pools the damaged files were last loaded for, see dataErrorSummary
	dataErrorsKey string
	// dataErrorsOutdated reloads the damaged files after a load of the statuses, see RefreshDataErrors
	dataErrorsOutdated bool
	// dataErrorsLoaded is when the damaged files were last loaded, see dataErrorsRefreshInterval
	dataErrorsLoaded time.Time
	// locationsVersion counts the changes of objectLocations, so a load that started before one applies it, see
	// dataErrorLoad
	locationsVersion int
	// dataErrorListeners are called when the damaged files were loaded, see OnDataErrorsChanged
	dataErrorListeners []func()
	// objectLocations are where the damaged objects without a path were found in snapshots, see FindObjects. Kept
	// for the next loads of the damaged files.
	objectLocations map[zfs.ObjectKey][]zfs.ObjectLocation
	// objectSearch is the last search for the damaged objects, nil if there was none, see ObjectSearch
	objectSearch *ObjectSearch
	// objectSearchListeners are called when the search progressed or ended, see OnObjectSearchChanged
	objectSearchListeners []func()
	// timeLeftMode is what the bars show of the time left, see CycleTimeLeftMode
	timeLeftMode TimeLeftMode
}

// ObjectSearch is the state of the search for the damaged objects without a path in the snapshots, see
// Monitor.FindObjects.
type ObjectSearch struct {
	Progress zfs.ObjectSearchProgress
	Running  bool
	// Err is why the search failed, nil if it did not (or still runs)
	Err error
	// cancel stops the search
	cancel func()
}

// NewMonitor returns a monitor without activities, until the first Refresh.
func NewMonitor(application *tview.Application) *Monitor {
	monitor := &Monitor{application: application, timeLeftMode: TimeLeftBoth}
	if mode, ok := state.Get[TimeLeftMode](state.Current, stateNamespace, stateKeyTimeLeftMode); ok && slices.Contains(timeLeftModes, mode) {
		monitor.timeLeftMode = mode
	}
	monitor.loader = uiutil.NewDataLoader[[]*zfs.PoolStatus](application).
		OnLoad(monitor.setStatuses).
		OnError(func(err error) {
			// e.g. zpool is not installed: nothing runs that could be shown. The alerts stay: a failed load (e.g.
			// while a pool is suspended) must not look like all problems were solved.
			monitor.update(nil, monitor.alerts)
		})
	monitor.dataErrorLoader = uiutil.NewDataLoader[dataErrorLoad](application).
		OnLoad(func(load dataErrorLoad) {
			// e.g. a search found objects while loading: the load does not have them
			if load.index != nil && load.locationsVersion != monitor.locationsVersion {
				load.index = load.index.WithLocations(monitor.objectLocations)
			}
			monitor.setDataErrors(load.index)
		}).
		OnError(func(err error) {
			// the old ones stay, like the alerts
			logging.Warning("Cannot list the damaged files: %v", err)
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

// RefreshDataErrors reloads the statuses (see Refresh), and the damaged files of the pools with data errors (see
// DataErrors) even if the data errors did not change, e.g. after a snapshot was destroyed: at most every
// dataErrorsRefreshInterval, else with a later load of the statuses. Must be called on the UI thread.
func (monitor *Monitor) RefreshDataErrors() {
	monitor.dataErrorsOutdated = true
	monitor.Refresh()
}

// ReloadDataErrors is RefreshDataErrors without waiting for dataErrorsRefreshInterval, e.g. once the user was
// allowed to list the damaged files. Must be called on the UI thread.
func (monitor *Monitor) ReloadDataErrors() {
	monitor.dataErrorsLoaded = time.Time{}
	monitor.RefreshDataErrors()
}

// DataErrors returns the damaged files of all pools, nil if there are none (or they are not loaded yet). They are
// loaded whenever the data errors of the pools changed (see zfs.PoolStatus.DataErrors), and on RefreshDataErrors.
// Must be called on the UI thread.
func (monitor *Monitor) DataErrors() *zfs.DataErrorIndex {
	return monitor.dataErrors
}

// OnDataErrorsChanged adds a function that is called on the UI thread whenever the damaged files were loaded. Must
// be called on the UI thread.
func (monitor *Monitor) OnDataErrorsChanged(listener func()) {
	monitor.dataErrorListeners = append(monitor.dataErrorListeners, listener)
}

// TimeLeftMode returns what the bars show of the time left of the activities. Must be called on the UI thread.
func (monitor *Monitor) TimeLeftMode() TimeLeftMode {
	return monitor.timeLeftMode
}

// CycleTimeLeftMode switches what the bars show of the time left of the activities to the next mode (the time left,
// the ETA, both), and remembers it. The bars show it when they are drawn next. Must be called on the UI thread.
func (monitor *Monitor) CycleTimeLeftMode() {
	monitor.timeLeftMode = monitor.timeLeftMode.Next()
	state.Set(state.Current, stateNamespace, stateKeyTimeLeftMode, monitor.timeLeftMode)
}

// Alerts returns the problems of the pools, most severe first, empty if all are healthy. Must be called on the UI
// thread.
func (monitor *Monitor) Alerts() []zfs.PoolAlert {
	return monitor.alerts
}

// OnChanged adds a function that is called on the UI thread whenever the activities or the alerts changed.
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

// setStatuses shows the activities and alerts of the loaded statuses, and schedules the next reload. Runs on the UI
// thread.
func (monitor *Monitor) setStatuses(statuses []*zfs.PoolStatus) {
	var activities []zfs.PoolActivity
	var alerts []zfs.PoolAlert
	for _, status := range statuses {
		activities = append(activities, status.Activities()...)
		alerts = append(alerts, status.Alerts()...)
	}
	// the most severe of all pools first
	zfs.SortPoolAlerts(alerts)
	monitor.update(activities, alerts)
	for _, listener := range monitor.statusListeners {
		listener(statuses)
	}
	monitor.updateDataErrors(statuses)
}

// updateDataErrors loads the damaged files if the data errors of the pools changed since the last load (or they are
// outdated, see RefreshDataErrors). Listing them runs zpool with sudo, so not on every load of the statuses (e.g.
// every few seconds while a scrub runs). Runs on the UI thread.
func (monitor *Monitor) updateDataErrors(statuses []*zfs.PoolStatus) {
	key := dataErrorSummary(statuses)
	if key == monitor.dataErrorsKey {
		// outdated ones at most every dataErrorsRefreshInterval: the flag stays for a later load of the statuses
		if !monitor.dataErrorsOutdated || time.Since(monitor.dataErrorsLoaded) < dataErrorsRefreshInterval {
			return
		}
	}
	monitor.dataErrorsKey = key
	monitor.dataErrorsOutdated = false
	monitor.dataErrorsLoaded = time.Now()
	if key == "" {
		monitor.dataErrorLoader.LoadQuietly(func(ctx context.Context) (dataErrorLoad, error) { return dataErrorLoad{}, nil })
		return
	}
	load := loadDataErrors
	locations := maps.Clone(monitor.objectLocations)
	version := monitor.locationsVersion
	monitor.dataErrorLoader.LoadQuietly(func(ctx context.Context) (dataErrorLoad, error) {
		index, err := load()
		if err != nil || len(locations) == 0 {
			return dataErrorLoad{index: index, locationsVersion: version}, err
		}
		// e.g. not the ones in a snapshot that was destroyed
		return dataErrorLoad{index: index.WithLocations(zfs.ExistingLocations(locations)), locationsVersion: version}, nil
	})
}

// FindObjects looks for the damaged objects whose path ZFS does not know in the snapshots of their datasets, in the
// background (see zfs.FindObjects). The found ones are marked like the other damaged files. Does nothing while a
// search runs, or if there is nothing to look for. Must be called on the UI thread.
func (monitor *Monitor) FindObjects() {
	index := monitor.dataErrors
	if (monitor.objectSearch != nil && monitor.objectSearch.Running) || !index.HasUnresolvedObjects() {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	search := &ObjectSearch{Running: true, cancel: cancel}
	monitor.objectSearch = search
	monitor.notifyObjectSearch()

	find := findObjects
	go func() {
		defer cancel()
		// the progress is shown at most every objectSearchRedrawInterval, the last one with the result
		var mu sync.Mutex
		var latest zfs.ObjectSearchProgress
		lastShown := time.Time{}
		locations, err := find(ctx, index, func(progress zfs.ObjectSearchProgress) {
			mu.Lock()
			latest = progress
			show := time.Since(lastShown) >= objectSearchRedrawInterval
			if show {
				lastShown = time.Now()
			}
			mu.Unlock()
			if !show {
				return
			}
			// not while holding mu: it waits while the queue of the UI thread is full
			monitor.application.QueueUpdateDraw(func() {
				mu.Lock()
				defer mu.Unlock()
				if monitor.objectSearch == search && search.Running {
					search.Progress = latest
					monitor.notifyObjectSearch()
				}
			})
		})
		monitor.application.QueueUpdateDraw(func() {
			mu.Lock()
			search.Progress = latest
			mu.Unlock()
			search.Running = false
			search.Err = err
			if err == nil {
				monitor.addObjectLocations(locations)
			}
			if monitor.objectSearch == search {
				monitor.notifyObjectSearch()
			}
		})
	}()
}

// addObjectLocations remembers where the searched objects were found (also the ones that were not found), and marks
// them. Runs on the UI thread.
func (monitor *Monitor) addObjectLocations(locations map[zfs.ObjectKey][]zfs.ObjectLocation) {
	if monitor.objectLocations == nil {
		monitor.objectLocations = map[zfs.ObjectKey][]zfs.ObjectLocation{}
	}
	// the ones that were not searched (e.g. not mounted) are left out of the result, so they stay unknown
	for key, found := range locations {
		monitor.objectLocations[key] = found
	}
	monitor.locationsVersion++
	if monitor.dataErrors != nil {
		monitor.setDataErrors(monitor.dataErrors.WithLocations(monitor.objectLocations))
	}
}

// CancelObjectSearch stops the running search for the damaged objects, see FindObjects. Must be called on the UI
// thread.
func (monitor *Monitor) CancelObjectSearch() {
	if monitor.objectSearch != nil && monitor.objectSearch.Running {
		monitor.objectSearch.cancel()
	}
}

// ObjectSearch returns the state of the last search for the damaged objects, nil if there was none. Must be called
// on the UI thread.
func (monitor *Monitor) ObjectSearch() *ObjectSearch {
	return monitor.objectSearch
}

// OnObjectSearchChanged adds a function that is called on the UI thread whenever the search for the damaged objects
// progressed or ended, see FindObjects. Must be called on the UI thread.
func (monitor *Monitor) OnObjectSearchChanged(listener func()) {
	monitor.objectSearchListeners = append(monitor.objectSearchListeners, listener)
}

func (monitor *Monitor) notifyObjectSearch() {
	for _, listener := range monitor.objectSearchListeners {
		listener()
	}
}

// dataErrorSummary returns the data errors of the pools that have some (e.g. "rpool: 18 data errors"), one line
// each, "" if none has.
func dataErrorSummary(statuses []*zfs.PoolStatus) string {
	var lines []string
	for _, status := range statuses {
		if errors := status.DataErrors(); errors != "" {
			lines = append(lines, status.Name+": "+errors)
		}
	}
	return strings.Join(lines, "\n")
}

// setDataErrors shows the loaded damaged files. Runs on the UI thread.
func (monitor *Monitor) setDataErrors(index *zfs.DataErrorIndex) {
	if index != nil && index.Len() == 0 && len(index.Unavailable) == 0 {
		index = nil
	}
	if index == nil && monitor.dataErrors == nil {
		return
	}
	monitor.dataErrors = index
	for _, listener := range monitor.dataErrorListeners {
		listener()
	}
}

// update shows the activities and alerts, and schedules the next reload. Runs on the UI thread.
func (monitor *Monitor) update(activities []zfs.PoolActivity, alerts []zfs.PoolAlert) {
	changed := !slices.Equal(activities, monitor.activities) || !slices.Equal(alerts, monitor.alerts)
	monitor.activities = activities
	monitor.alerts = alerts
	if changed {
		for _, listener := range monitor.listeners {
			listener()
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
