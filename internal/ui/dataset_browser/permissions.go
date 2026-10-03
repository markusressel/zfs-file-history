package dataset_browser

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/dialog"
	"zfs-file-history/internal/zfs"
)

// Replaceable in tests, so tests never spawn zfs processes.
var (
	loadDatasetPermissions = zfs.LoadDatasetPermissions
	isCurrentUserRoot      = zfs.IsCurrentUserRoot
	listDatasetProperties  = zfs.ListProperties
)

const (
	// permissionWorkers is the number of datasets whose permissions are read at the same time.
	// Each read is a "zfs allow" process (≈15 ms, mostly in the kernel), so more workers barely help.
	permissionWorkers = 4
	// permissionRedrawInterval throttles redraws while permissions are loading (see the tview rules in AGENTS.md).
	permissionRedrawInterval = 50 * time.Millisecond
)

// permissionState is the loaded permissions of the current user on a dataset.
type permissionState struct {
	permissions *zfs.DatasetPermissions
	err         error
}

// permissionLoader loads the permissions of the displayed datasets in the background.
// All fields are only accessed on the UI thread.
type permissionLoader struct {
	// loaded are the permissions by dataset name. They are kept on reloads, so the column does not flicker.
	loaded map[string]permissionState
	// stale are the names of datasets whose permissions are outdated (e.g. after a reload) and read again
	stale  map[string]bool
	cancel context.CancelFunc
}

// formatPermissionMask returns the compact form of the permissions, one letter per zfs.KnownPermissions
// (like "ls -l"), e.g. "sdmh-----". Returns "" if the permissions are not loaded yet, and "?" if they cannot be read.
func formatPermissionMask(state permissionState, ok bool) string {
	if !ok {
		return ""
	}
	if state.err != nil || state.permissions == nil {
		return "?"
	}
	var mask strings.Builder
	for _, info := range zfs.KnownPermissions {
		if state.permissions.Has(info.Permission) {
			mask.WriteRune(info.Abbreviation)
		} else {
			mask.WriteRune('-')
		}
	}
	return mask.String()
}

// isPermissionColumnActive returns whether the permissions column is displayed. Without it, nothing is loaded.
func (datasetBrowser *DatasetBrowserComponent) isPermissionColumnActive() bool {
	return slices.Contains(datasetBrowser.tableContainer.GetColumnSpec(), columnPermissions)
}

// invalidatePermissions marks all loaded permissions as outdated, they are read again when displayed.
// Must be called on the UI thread.
func (datasetBrowser *DatasetBrowserComponent) invalidatePermissions() {
	for name := range datasetBrowser.permissions.loaded {
		datasetBrowser.permissions.stale[name] = true
	}
}

// ReloadPermissions reads the permissions of the displayed datasets again, e.g. after they were changed. The old
// ones are shown until then. Must be called on the UI thread.
func (datasetBrowser *DatasetBrowserComponent) ReloadPermissions() {
	datasetBrowser.invalidatePermissions()
	datasetBrowser.loadPermissions()
}

// loadPermissions reads the permissions of the displayed datasets that are not loaded yet (or outdated), in the
// background. Results are applied progressively, with throttled redraws. A new call cancels the previous one.
// Must be called on the UI thread.
func (datasetBrowser *DatasetBrowserComponent) loadPermissions() {
	loader := &datasetBrowser.permissions
	if loader.cancel != nil {
		loader.cancel()
		loader.cancel = nil
	}
	if !datasetBrowser.isPermissionColumnActive() {
		return
	}

	var names []string
	for _, entry := range datasetBrowser.tableContainer.GetEntries() {
		if _, ok := loader.loaded[entry.Name]; !ok || loader.stale[entry.Name] {
			names = append(names, entry.Name)
		}
	}
	if len(names) == 0 {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	loader.cancel = cancel
	go datasetBrowser.readPermissions(ctx, names)
}

// readPermissions runs in the background and hands the results over to the UI thread in batches.
func (datasetBrowser *DatasetBrowserComponent) readPermissions(ctx context.Context, names []string) {
	results := make(chan struct {
		name  string
		state permissionState
	})

	if isCurrentUserRoot() {
		// root has all permissions, no need to read the delegations
		go func() {
			defer close(results)
			for _, name := range names {
				select {
				case results <- struct {
					name  string
					state permissionState
				}{name, permissionState{permissions: &zfs.DatasetPermissions{Dataset: name, User: "root", IsRoot: true}}}:
				case <-ctx.Done():
					return
				}
			}
		}()
	} else {
		queue := make(chan string)
		var workers sync.WaitGroup
		for i := 0; i < permissionWorkers; i++ {
			workers.Add(1)
			go func() {
				defer workers.Done()
				for name := range queue {
					permissions, err := loadDatasetPermissions(name)
					if err != nil {
						logging.Error("Could not read the permissions of %s: %v", name, err)
					}
					select {
					case results <- struct {
						name  string
						state permissionState
					}{name, permissionState{permissions, err}}:
					case <-ctx.Done():
						return
					}
				}
			}()
		}
		go func() {
			defer close(queue)
			for _, name := range names {
				select {
				case queue <- name:
				case <-ctx.Done():
					return
				}
			}
		}()
		go func() {
			workers.Wait()
			close(results)
		}()
	}

	batch := map[string]permissionState{}
	lastDraw := time.Time{}
	apply := func(draw bool) {
		applied := batch
		batch = map[string]permissionState{}
		update := func() {
			if ctx.Err() != nil {
				return
			}
			datasetBrowser.applyPermissions(applied)
		}
		if draw {
			datasetBrowser.application.QueueUpdateDraw(update)
		} else {
			datasetBrowser.application.QueueUpdate(update)
		}
	}
	for result := range results {
		batch[result.name] = result.state
		if time.Since(lastDraw) >= permissionRedrawInterval {
			apply(true)
			lastDraw = time.Now()
		}
	}
	if len(batch) > 0 && ctx.Err() == nil {
		apply(true)
	}
}

// applyPermissions stores loaded permissions and updates their rows. Runs on the UI thread.
func (datasetBrowser *DatasetBrowserComponent) applyPermissions(states map[string]permissionState) {
	for name, state := range states {
		datasetBrowser.permissions.loaded[name] = state
		delete(datasetBrowser.permissions.stale, name)
	}
	for _, entry := range datasetBrowser.tableContainer.GetEntries() {
		if _, ok := states[entry.Name]; ok {
			datasetBrowser.tableContainer.UpdateEntry(entry)
		}
	}
}

// openPermissionsDialog reads the permissions of the dataset in the background and shows them.
// Must be called on the UI thread.
func (datasetBrowser *DatasetBrowserComponent) openPermissionsDialog(entry *zfs.DatasetListEntry) {
	if entry == nil {
		return
	}
	name := entry.Name
	go func() {
		permissions, err := loadDatasetPermissions(name)
		datasetBrowser.application.QueueUpdateDraw(func() {
			if err != nil {
				logging.Error("Could not read the permissions of %s: %v", name, err)
				datasetBrowser.showDialog(dialog.NewErrorDialog(datasetBrowser.application, "Cannot Read Permissions",
					fmt.Errorf("cannot read the permissions of %s: %w", name, err)))
				return
			}
			// the dialog read them anyway, so the column shows the same
			datasetBrowser.applyPermissions(map[string]permissionState{name: {permissions: permissions}})
			// changed delegations can affect the descendants as well, and the dataset info shows them too
			onChanged := func() { zfs.RefreshZfsData() }
			datasetBrowser.showDialog(dialog.NewDatasetPermissionsDialog(datasetBrowser.application, permissions, onChanged))
		})
	}()
}

// openPropertiesDialog reads the properties of the dataset in the background and shows them.
// Must be called on the UI thread.
func (datasetBrowser *DatasetBrowserComponent) openPropertiesDialog(entry *zfs.DatasetListEntry) {
	if entry == nil {
		return
	}
	name := entry.Name
	go func() {
		properties, err := listDatasetProperties(name)
		isRoot := isCurrentUserRoot()
		datasetBrowser.application.QueueUpdateDraw(func() {
			if err != nil {
				logging.Error("Could not read the properties of %s: %v", name, err)
				datasetBrowser.showDialog(dialog.NewErrorDialog(datasetBrowser.application, "Cannot Read Properties",
					fmt.Errorf("cannot read the properties of %s: %w", name, err)))
				return
			}
			// e.g. a new mountpoint changes the dataset list, the dataset info shows properties as well
			onChanged := func() { zfs.RefreshZfsData() }
			datasetBrowser.showDialog(dialog.NewDatasetPropertiesDialog(datasetBrowser.application, name, properties, isRoot, onChanged))
		})
	}()
}

func (datasetBrowser *DatasetBrowserComponent) showDialog(d dialog.Dialog) {
	dialog.ShowDialogOnPages(datasetBrowser.application, datasetBrowser.layout, d, nil)
}
