package dataset_browser

import (
	"fmt"
	"strings"
	"time"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/dialog"
	"zfs-file-history/internal/ui/status_message"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"
)

// Replaceable in tests, so tests never change anything.
var (
	createSnapshot      = zfs.CreateSnapshot
	previewPromote      = zfs.PreviewPromote
	promoteClone        = zfs.PromoteClone
	previewDestroyClone = zfs.PreviewDestroyClone
	destroyClone        = zfs.DestroyClone
)

// maxListedNames is the maximum number of snapshots or datasets listed in the confirmations.
const maxListedNames = 10

// OpenInFileBrowserEvent asks to show the files of a dataset, at the path it is mounted at.
type OpenInFileBrowserEvent struct {
	Path string
}

// openActionDialog shows the actions for the dataset. Must be called on the UI thread.
func (datasetBrowser *DatasetBrowserComponent) openActionDialog(entry *zfs.DatasetListEntry) {
	if entry == nil {
		return
	}
	datasetBrowser.showDialog(datasetBrowser.newActionDialog(entry))
}

// newActionDialog creates the dialog with the actions for the dataset, see openActionDialog. Promoting and destroying
// a clone ask for confirmation first, with what changes.
func (datasetBrowser *DatasetBrowserComponent) newActionDialog(entry *zfs.DatasetListEntry) *dialog.SelectionDialog {
	name := entry.Name
	var createdName string
	var promotePreview *zfs.PromotePreview
	var destroyPreview *zfs.DestroyPreview

	asyncWork := func(d *dialog.SelectionDialog, action dialog.DialogActionId) error {
		var err error
		switch action {
		case dialog.DatasetDialogCreateSnapshotActionId, dialog.DatasetDialogCreateSnapshotRecursivelyActionId:
			createdName = fmt.Sprintf("zfh-%s", time.Now().Format(zfs.SnapshotTimeFormat))
			err = createSnapshot(name, createdName, action == dialog.DatasetDialogCreateSnapshotRecursivelyActionId)
		case dialog.DatasetDialogPromoteActionId:
			promotePreview, err = previewPromote(name)
		case dialog.DatasetDialogDestroyCloneActionId:
			destroyPreview, err = previewDestroyClone(name)
		}
		return err
	}

	onComplete := func(d *dialog.SelectionDialog, option *dialog.DialogOption, err error) {
		d.Close()
		if err != nil {
			logging.Error("Action on %s failed: %s", name, err.Error())
			datasetBrowser.showDialog(dialog.NewErrorDialogWithRetry(datasetBrowser.application, "Operation Failed", err, d.RetryFunc(option)))
			if option.Id == dialog.DatasetDialogCreateSnapshotRecursivelyActionId {
				// some of the snapshots may exist
				zfs.RefreshZfsData()
			}
			return
		}
		switch option.Id {
		case dialog.DatasetDialogOpenInFileBrowserActionId:
			datasetBrowser.Events.Emit(OpenInFileBrowserEvent{Path: entry.MountPath})
		case dialog.DatasetDialogCreateSnapshotActionId, dialog.DatasetDialogCreateSnapshotRecursivelyActionId:
			datasetBrowser.Events.Emit(DatasetBrowserStatusEvent{
				Message: status_message.NewSuccessStatusMessage(fmt.Sprintf("Created snapshot %s@%s", name, createdName)),
			})
			zfs.RefreshZfsData()
		case dialog.DatasetDialogPermissionsActionId:
			datasetBrowser.openPermissionsDialog(entry)
		case dialog.DatasetDialogPropertiesActionId:
			datasetBrowser.openPropertiesDialog(entry)
		case dialog.DatasetDialogPromoteActionId:
			// nothing was promoted yet, the confirmation does that
			datasetBrowser.showPromoteConfirmation(promotePreview)
		case dialog.DatasetDialogDestroyCloneActionId:
			// nothing was destroyed yet, the confirmations do that
			datasetBrowser.showDestroyCloneConfirmation(name, destroyPreview)
		}
	}

	return dialog.NewDatasetActionDialog(datasetBrowser.application, entry, asyncWork, onComplete)
}

// formatNameList returns the names as indented lines, highlighted, at most maxListedNames.
func formatNameList(names []string) string {
	var list strings.Builder
	for i, name := range names {
		if i == maxListedNames {
			fmt.Fprintf(&list, "\n  … and %d more", len(names)-maxListedNames)
			break
		}
		fmt.Fprintf(&list, "\n  %s", dialog.Highlight(name))
	}
	return list.String()
}

// formatPromoteDescription describes what promoting the clone changes, with style tags.
func formatPromoteDescription(preview *zfs.PromotePreview) string {
	originDataset, _, _ := strings.Cut(preview.Origin, "@")
	clone, origin := dialog.Highlight(preview.Clone), dialog.Highlight(preview.Origin)
	var description strings.Builder
	fmt.Fprintf(&description, "Promote %s, so it no longer depends on %s. Nothing is lost: ", clone, origin)
	fmt.Fprintf(&description, "%s becomes a clone of %s instead, and can be destroyed afterwards.", dialog.Highlight(originDataset), clone)
	if len(preview.Moved) > 0 {
		fmt.Fprintf(&description, "\n\nThese snapshots move to %s (%d):%s", clone, len(preview.Moved), formatNameList(preview.Moved))
	}
	return description.String()
}

// showPromoteConfirmation shows what promoting the clone changes, and promotes it on confirmation. Must be called on
// the UI thread.
func (datasetBrowser *DatasetBrowserComponent) showPromoteConfirmation(preview *zfs.PromotePreview) {
	asyncWork := func(d *dialog.SelectionDialog, action dialog.DialogActionId) error {
		if action != dialog.PromoteCloneDialogPromoteActionId {
			return nil
		}
		return promoteClone(preview.Clone)
	}
	onComplete := func(d *dialog.SelectionDialog, option *dialog.DialogOption, err error) {
		d.Close()
		if option.Id != dialog.PromoteCloneDialogPromoteActionId {
			return
		}
		if err != nil {
			logging.Error("Failed to promote %s: %s", preview.Clone, err.Error())
			datasetBrowser.showDialog(dialog.NewErrorDialogWithRetry(datasetBrowser.application, "Promote Failed", err, d.RetryFunc(option)))
			return
		}
		datasetBrowser.Events.Emit(DatasetBrowserStatusEvent{
			Message: status_message.NewSuccessStatusMessage(fmt.Sprintf("Promoted %s", preview.Clone)),
		})
		zfs.RefreshZfsData()
	}
	datasetBrowser.showDialog(dialog.NewPromoteCloneDialog(datasetBrowser.application, formatPromoteDescription(preview), asyncWork, onComplete))
}

// formatDestroyCloneDescription describes what destroying the clone destroys, based on its dry run, with style tags.
func formatDestroyCloneDescription(name string, preview *zfs.DestroyPreview) string {
	var description strings.Builder
	fmt.Fprintf(&description, "Destroy the clone %s. This frees %s and cannot be undone.\n\n", dialog.Highlight(name),
		dialog.Highlight(uiutil.HumanizedBytes(preview.Reclaim)))
	fmt.Fprintf(&description, "Will be destroyed (%d):%s", len(preview.Destroyed), formatNameList(preview.Destroyed))
	return description.String()
}

// previewAndConfirmDestroyClone does the dry run of destroying the clone in the background and then asks for
// confirmation. Must be called on the UI thread.
func (datasetBrowser *DatasetBrowserComponent) previewAndConfirmDestroyClone(name string) {
	go func() {
		preview, err := previewDestroyClone(name)
		datasetBrowser.application.QueueUpdateDraw(func() {
			if err != nil {
				logging.Error("Cannot destroy %s: %s", name, err.Error())
				retry := func() { datasetBrowser.previewAndConfirmDestroyClone(name) }
				datasetBrowser.showDialog(dialog.NewErrorDialogWithRetry(datasetBrowser.application, "Cannot Destroy", err, retry))
				return
			}
			datasetBrowser.showDestroyCloneConfirmation(name, preview)
		})
	}()
}

// showDestroyCloneConfirmation asks for confirmation to destroy the clone as the dry run says. If it has snapshots or
// child datasets, they must be included explicitly (a checkbox) first. In any case its name must be typed.
// Must be called on the UI thread.
func (datasetBrowser *DatasetBrowserComponent) showDestroyCloneConfirmation(name string, preview *zfs.DestroyPreview) {
	description := formatDestroyCloneDescription(name, preview)
	dependents := len(preview.Destroyed) - 1
	if dependents == 0 {
		datasetBrowser.showDestroyCloneByName(name, false, preview, description)
		return
	}

	recursive := fmt.Sprintf("Also destroy its %d %s", dependents, uiutil.Plural(dependents, "snapshot or child dataset", "snapshots and child datasets"))
	onComplete := func(d *dialog.SelectionDialog, option *dialog.DialogOption, err error) {
		d.Close()
		if option.Id != dialog.DestroyCloneDialogContinueActionId {
			return
		}
		if !d.Checked()[0] {
			// shows this dialog again, so the box can be checked
			retry := func() { datasetBrowser.showDestroyCloneConfirmation(name, preview) }
			datasetBrowser.showDialog(dialog.NewErrorDialogWithRetry(datasetBrowser.application, "Nothing Destroyed",
				fmt.Errorf("'%s' has %d %s, which are destroyed with it: check '%s' to destroy them as well", name, dependents,
					uiutil.Plural(dependents, "snapshot or child dataset", "snapshots and child datasets"), recursive), retry))
			return
		}
		datasetBrowser.showDestroyCloneByName(name, true, preview, description)
	}
	datasetBrowser.showDialog(dialog.NewDestroyCloneDialog(datasetBrowser.application, description, recursive, nil, onComplete))
}

// showDestroyCloneByName destroys the clone as the dry run says once its name is typed. Must be called on the UI
// thread.
func (datasetBrowser *DatasetBrowserComponent) showDestroyCloneByName(name string, recursive bool, preview *zfs.DestroyPreview, description string) {
	datasetBrowser.showDialog(dialog.NewConfirmByNameDialog(
		datasetBrowser.application,
		"DestroyCloneByNameDialog",
		" 💥 Destroy Clone ",
		description,
		name,
		func() { datasetBrowser.destroyCloneInBackground(name, recursive, preview) },
	))
}

// destroyCloneInBackground destroys the clone as confirmed in the background and shows the result. Must be called on
// the UI thread.
func (datasetBrowser *DatasetBrowserComponent) destroyCloneInBackground(name string, recursive bool, preview *zfs.DestroyPreview) {
	go func() {
		err := destroyClone(name, recursive, preview)
		datasetBrowser.application.QueueUpdateDraw(func() {
			if err != nil {
				logging.Error("Failed to destroy %s: %s", name, err.Error())
				// a retry asks for confirmation again, with a new dry run
				retry := func() { datasetBrowser.previewAndConfirmDestroyClone(name) }
				datasetBrowser.showDialog(dialog.NewErrorDialogWithRetry(datasetBrowser.application, "Destroy Failed", err, retry))
			} else {
				message := fmt.Sprintf("Destroyed '%s' (%d %s), freed %s.", name, len(preview.Destroyed),
					uiutil.Plural(len(preview.Destroyed), "dataset or snapshot", "datasets and snapshots"), uiutil.HumanizedBytes(preview.Reclaim))
				datasetBrowser.showDialog(dialog.NewSuccessDialog(datasetBrowser.application, "Clone Destroyed", message))
			}
			// some of it may be destroyed, also after an error
			zfs.RefreshZfsData()
		})
	}()
}
