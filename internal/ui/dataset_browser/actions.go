package dataset_browser

import (
	"fmt"
	"strings"
	"time"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/dialog"
	"zfs-file-history/internal/ui/status_message"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"
)

// Replaceable in tests, so tests never change anything.
var (
	createSnapshot        = zfs.CreateSnapshot
	previewPromote        = zfs.PreviewPromote
	promoteClone          = zfs.PromoteClone
	previewDestroyDataset = zfs.PreviewDestroyDataset
	destroyDataset        = zfs.DestroyDataset
	planMountAction       = zfs.PlanMountAction
	runMountPlan          = zfs.RunMountPlan
	runInTerminal         = uiutil.RunInTerminal
	runWithSudoInTerminal = uiutil.RunWithSudoInTerminal
)

// mountActions are the mount actions of the options of the dataset action dialog.
var mountActions = map[dialog.DialogActionId]zfs.MountAction{
	dialog.DatasetDialogMountActionId:     zfs.MountActionMount,
	dialog.DatasetDialogUnmountActionId:   zfs.MountActionUnmount,
	dialog.DatasetDialogLoadKeyActionId:   zfs.MountActionLoadKey,
	dialog.DatasetDialogUnloadKeyActionId: zfs.MountActionUnloadKey,
}

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
	datasetBrowser.ShowDialog(datasetBrowser.newActionDialog(entry), nil)
}

// newActionDialog creates the dialog with the actions for the dataset, see openActionDialog. Promoting a clone,
// destroying a dataset, unmounting it and unloading its key ask for confirmation first, with what changes. Mounting
// and loading a key run right away, in the terminal if zfs asks for the passphrase. The dry run of destroying a
// dataset runs in the dialog of its confirmation (see previewAndConfirmDestroy), not in this one: a spinner next to
// "Destroy dataset" looked like it was destroyed without a confirmation.
func (datasetBrowser *DatasetBrowserComponent) newActionDialog(entry *zfs.DatasetListEntry) *dialog.SelectionDialog {
	name := entry.Name
	var createdName string
	var promotePreview *zfs.PromotePreview
	var mountPlan *zfs.MountPlan

	asyncWork := func(d *dialog.SelectionDialog, action dialog.DialogActionId) error {
		var err error
		switch action {
		case dialog.DatasetDialogCreateSnapshotActionId, dialog.DatasetDialogCreateSnapshotRecursivelyActionId:
			createdName = fmt.Sprintf("zfh-%s", time.Now().Format(zfs.SnapshotTimeFormat))
			err = createSnapshot(name, createdName, action == dialog.DatasetDialogCreateSnapshotRecursivelyActionId)
		case dialog.DatasetDialogPromoteActionId:
			promotePreview, err = previewPromote(name)
		case dialog.DatasetDialogMountActionId, dialog.DatasetDialogLoadKeyActionId:
			mountPlan, err = planMountAction(mountActions[action], name)
			if err == nil && !mountPlan.Prompts {
				err = runMountPlan(mountPlan)
			}
		case dialog.DatasetDialogUnmountActionId, dialog.DatasetDialogUnloadKeyActionId:
			mountPlan, err = planMountAction(mountActions[action], name)
		}
		return err
	}

	onComplete := func(d *dialog.SelectionDialog, option *dialog.DialogOption, err error) {
		d.Close()
		if err != nil {
			logging.Error("Action on %s failed: %s", name, err.Error())
			datasetBrowser.ShowDialog(dialog.NewErrorDialogWithRetry(datasetBrowser.application, "Operation Failed", err, d.RetryFunc(option)), nil)
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
		case dialog.DatasetDialogDestroyActionId:
			// nothing is destroyed yet, the confirmations do that
			datasetBrowser.previewAndConfirmDestroy(entry)
		case dialog.DatasetDialogMountActionId, dialog.DatasetDialogLoadKeyActionId:
			if mountPlan.Prompts {
				datasetBrowser.runInTerminal(entry, mountPlan)
			} else {
				datasetBrowser.mounted(mountPlan)
			}
		case dialog.DatasetDialogUnmountActionId, dialog.DatasetDialogUnloadKeyActionId:
			// nothing was unmounted yet, the confirmation does that
			datasetBrowser.showUnmountConfirmation(entry, mountPlan)
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
			datasetBrowser.ShowDialog(dialog.NewErrorDialogWithRetry(datasetBrowser.application, "Promote Failed", err, d.RetryFunc(option)), nil)
			return
		}
		datasetBrowser.Events.Emit(DatasetBrowserStatusEvent{
			Message: status_message.NewSuccessStatusMessage(fmt.Sprintf("Promoted %s", preview.Clone)),
		})
		zfs.RefreshZfsData()
	}
	datasetBrowser.ShowDialog(dialog.NewPromoteCloneDialog(datasetBrowser.application, formatPromoteDescription(preview), asyncWork, onComplete), nil)
}

// destroyedParts returns the datasets and the snapshots of a dry run of destroying a dataset, each in the order of the
// dry run, so the confirmation lists the datasets first: hundreds of snapshots would hide them.
func destroyedParts(preview *zfs.DestroyPreview) (datasets []string, snapshots []string) {
	for _, name := range preview.Destroyed {
		if strings.Contains(name, "@") {
			snapshots = append(snapshots, name)
		} else {
			datasets = append(datasets, name)
		}
	}
	return datasets, snapshots
}

// formatDestroyedCount returns e.g. "2 child datasets and 3 snapshots", leaving out what there is none of. children
// is the number of datasets besides the destroyed one.
func formatDestroyedCount(children int, snapshots int) string {
	var parts []string
	if children > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", children, uiutil.Plural(children, "child dataset", "child datasets")))
	}
	if snapshots > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", snapshots, uiutil.Plural(snapshots, "snapshot", "snapshots")))
	}
	return strings.Join(parts, " and ")
}

// formatDestroyDescription describes what destroying the dataset destroys, based on its dry run, with style tags.
func formatDestroyDescription(entry *zfs.DatasetListEntry, preview *zfs.DestroyPreview) string {
	danger := func(text string) string { return txwidgets.Span(theme.Colors.Dialog.Danger, "%s", text) }
	var description strings.Builder
	if entry.IsClone() {
		fmt.Fprintf(&description, "Destroy the clone %s of %s", dialog.Highlight(entry.Name), dialog.Highlight(entry.Origin))
	} else {
		fmt.Fprintf(&description, "Destroy the dataset %s", dialog.Highlight(entry.Name))
	}
	fmt.Fprintf(&description, " with all its files. This frees %s and %s.", dialog.Highlight(uiutil.HumanizedBytes(preview.Reclaim)),
		danger("cannot be undone"))
	if entry.MountPath != "" {
		fmt.Fprintf(&description, "\n\nIt is unmounted from %s first. A dataset in use cannot be unmounted: the destroy "+
			"stops there, after destroying what came before it.", dialog.Highlight(entry.MountPath))
	}
	datasets, snapshots := destroyedParts(preview)
	fmt.Fprintf(&description, "\n\nDatasets that will be destroyed (%d):%s", len(datasets), formatNameList(datasets))
	if len(snapshots) > 0 {
		fmt.Fprintf(&description, "\n\nSnapshots that will be destroyed (%d):%s", len(snapshots), formatNameList(snapshots))
	}
	return description.String()
}

// previewAndConfirmDestroy does the dry run of destroying the dataset in the background and then asks for
// confirmation. Meanwhile, a dialog says that it is checked, and that nothing is destroyed yet; it can be cancelled.
// Must be called on the UI thread.
func (datasetBrowser *DatasetBrowserComponent) previewAndConfirmDestroy(entry *zfs.DatasetListEntry) {
	dialog.RunPreview(&datasetBrowser.Host, dialog.Preview[*zfs.DestroyPreview]{
		Title: " 💥 Destroy Dataset ",
		Message: fmt.Sprintf("Checking what destroying %s would destroy: a dry run, and its permissions, holds and "+
			"mounts. Nothing is destroyed yet.", entry.Name),
		ErrorTitle: "Cannot Destroy", Subject: entry.Name,
		Check:   func() (*zfs.DestroyPreview, error) { return previewDestroyDataset(entry.Name) },
		Confirm: func(preview *zfs.DestroyPreview) { datasetBrowser.showDestroyConfirmation(entry, preview) },
		Retry:   func() { datasetBrowser.previewAndConfirmDestroy(entry) },
	})
}

// showDestroyConfirmation asks for confirmation to destroy the dataset as the dry run says. If it has snapshots or
// child datasets, they must be included explicitly (a checkbox) first. In any case its full name must be typed.
// Must be called on the UI thread.
func (datasetBrowser *DatasetBrowserComponent) showDestroyConfirmation(entry *zfs.DatasetListEntry, preview *zfs.DestroyPreview) {
	description := formatDestroyDescription(entry, preview)
	datasets, snapshots := destroyedParts(preview)
	dependents := formatDestroyedCount(len(datasets)-1, len(snapshots))
	if dependents == "" {
		datasetBrowser.showDestroyByName(entry, false, preview, description)
		return
	}

	recursive := "Also destroy its " + dependents
	onComplete := func(d *dialog.SelectionDialog, option *dialog.DialogOption, err error) {
		d.Close()
		if option.Id != dialog.DestroyDatasetDialogContinueActionId {
			return
		}
		if !d.Checked()[0] {
			// shows this dialog again, so the box can be checked
			retry := func() { datasetBrowser.showDestroyConfirmation(entry, preview) }
			datasetBrowser.ShowDialog(dialog.NewErrorDialogWithRetry(datasetBrowser.application, "Nothing Destroyed",
				fmt.Errorf("'%s' has %s, which are destroyed with it: check '%s' to destroy them as well", entry.Name,
					dependents, recursive), retry), nil)
			return
		}
		datasetBrowser.showDestroyByName(entry, true, preview, description)
	}
	datasetBrowser.ShowDialog(dialog.NewDestroyDatasetDialog(datasetBrowser.application, description, recursive, nil, onComplete), nil)
}

// showDestroyByName destroys the dataset as the dry run says once its full name is typed. Must be called on the UI
// thread.
func (datasetBrowser *DatasetBrowserComponent) showDestroyByName(entry *zfs.DatasetListEntry, recursive bool, preview *zfs.DestroyPreview, description string) {
	datasetBrowser.ShowDialog(dialog.NewConfirmByNameDialog(
		datasetBrowser.application,
		"DestroyDatasetByNameDialog",
		" 💥 Destroy Dataset ",
		description,
		entry.Name,
		func() { datasetBrowser.destroyInBackground(entry, recursive, preview) },
	), nil)
}

// destroyInBackground destroys the dataset as confirmed in the background and shows the result. Must be called on
// the UI thread.
func (datasetBrowser *DatasetBrowserComponent) destroyInBackground(entry *zfs.DatasetListEntry, recursive bool, preview *zfs.DestroyPreview) {
	name := entry.Name
	go func() {
		err := destroyDataset(name, recursive, preview)
		datasetBrowser.application.QueueUpdateDraw(func() {
			if err != nil {
				logging.Error("Failed to destroy %s: %s", name, err.Error())
				// a retry asks for confirmation again, with a new dry run
				retry := func() { datasetBrowser.previewAndConfirmDestroy(entry) }
				datasetBrowser.ShowDialog(dialog.NewErrorDialogWithRetry(datasetBrowser.application, "Destroy Failed", err, retry), nil)
			} else {
				message := fmt.Sprintf("Destroyed '%s' (%d %s), freed %s.", name, len(preview.Destroyed),
					uiutil.Plural(len(preview.Destroyed), "dataset or snapshot", "datasets and snapshots"), uiutil.HumanizedBytes(preview.Reclaim))
				datasetBrowser.ShowDialog(dialog.NewSuccessDialog(datasetBrowser.application, "Dataset Destroyed", message), nil)
			}
			// some of it may be destroyed, also after an error
			zfs.RefreshZfsData()
		})
	}()
}

// mounted tells that the plan was done, and reloads the datasets: mounts are no zfs events. Must be called on the UI
// thread.
func (datasetBrowser *DatasetBrowserComponent) mounted(plan *zfs.MountPlan) {
	datasetBrowser.Events.Emit(DatasetBrowserStatusEvent{
		Message: status_message.NewSuccessStatusMessage(plan.Done()),
	})
	zfs.RefreshZfsData()
}

// runInTerminal runs the plan in the terminal, where zfs asks for the passphrase of the key: the application never
// sees it. With sudo if it needs root, which asks for the password first. Must be called on the UI thread.
func (datasetBrowser *DatasetBrowserComponent) runInTerminal(entry *zfs.DatasetListEntry, plan *zfs.MountPlan) {
	explanation := fmt.Sprintf("loading the key of %s: zfs asks for its passphrase.", plan.EncryptionRoot)
	if plan.Action == zfs.MountActionMount {
		explanation = fmt.Sprintf("loading the key of %s and mounting %s: zfs asks for its passphrase.", plan.EncryptionRoot, plan.Dataset)
	}
	run := runInTerminal
	if plan.NeedsRoot {
		explanation += " It runs as root, sudo asks for your password first."
		run = runWithSudoInTerminal
	}
	go func() {
		// blocks until the UI is back
		err := run(datasetBrowser.application, explanation, plan.CommandLines())
		datasetBrowser.application.QueueUpdateDraw(func() {
			if err != nil {
				logging.Error("Failed to %s: %s", plan.Name(), err.Error())
				// zfs printed why in the terminal, e.g. a wrong passphrase
				retry := func() { datasetBrowser.startMountAction(entry, plan.Action) }
				datasetBrowser.ShowDialog(dialog.NewErrorDialogWithRetry(datasetBrowser.application, "Failed", err, retry), nil)
				return
			}
			datasetBrowser.mounted(plan)
		})
	}()
}

// startMountAction plans the action in the background, and then does it like the action dialog does: asking first
// before unmounting, in the terminal if zfs asks for a passphrase. Must be called on the UI thread.
func (datasetBrowser *DatasetBrowserComponent) startMountAction(entry *zfs.DatasetListEntry, action zfs.MountAction) {
	go func() {
		plan, err := planMountAction(action, entry.Name)
		datasetBrowser.application.QueueUpdateDraw(func() {
			switch {
			case err != nil:
				logging.Error("Cannot change the mounts of %s: %s", entry.Name, err.Error())
				retry := func() { datasetBrowser.startMountAction(entry, action) }
				datasetBrowser.ShowDialog(dialog.NewErrorDialogWithRetry(datasetBrowser.application, "Failed", err, retry), nil)
			case plan.Prompts:
				datasetBrowser.runInTerminal(entry, plan)
			case action == zfs.MountActionUnmount || action == zfs.MountActionUnloadKey:
				datasetBrowser.showUnmountConfirmation(entry, plan)
			default:
				datasetBrowser.runMountPlanInBackground(entry, plan)
			}
		})
	}()
}

// formatUnmountDescription describes what unmounting the dataset (or unloading its key) does, with style tags.
func formatUnmountDescription(plan *zfs.MountPlan) string {
	var description strings.Builder
	if plan.Action == zfs.MountActionUnloadKey {
		fmt.Fprintf(&description, "Unload the key of %s. The datasets encrypted with it are unmounted first, and their "+
			"files cannot be read until the key is loaded again.", dialog.Highlight(plan.EncryptionRoot))
	} else {
		fmt.Fprintf(&description, "Unmount %s from %s.", dialog.Highlight(plan.Dataset), dialog.Highlight(plan.MountPath))
	}
	if len(plan.Unmounted) == 0 {
		return description.String()
	}
	description.WriteString(" Nothing is lost, but programs that use their files fail. A dataset in use is not " +
		"unmounted: it stops there, after unmounting what came before it.")
	unmounted := make([]string, 0, len(plan.Unmounted))
	for _, mount := range plan.Unmounted {
		unmounted = append(unmounted, fmt.Sprintf("%s (%s)", mount.Name, mount.Mountpoint))
	}
	fmt.Fprintf(&description, "\n\nDatasets that will be unmounted, in this order (%d):%s", len(unmounted), formatNameList(unmounted))
	return description.String()
}

// showUnmountConfirmation asks for confirmation to unmount the datasets of the plan (and unload the key), and does it
// on confirmation. Must be called on the UI thread.
func (datasetBrowser *DatasetBrowserComponent) showUnmountConfirmation(entry *zfs.DatasetListEntry, plan *zfs.MountPlan) {
	title, confirm := " ⏏️ Unmount ", "Unmount"
	if plan.Action == zfs.MountActionUnloadKey {
		title, confirm = " 🔒 Unload Key ", "Unload key"
	}
	onComplete := func(d *dialog.SelectionDialog, option *dialog.DialogOption, err error) {
		d.Close()
		if option.Id == dialog.UnmountDialogUnmountActionId {
			datasetBrowser.runMountPlanInBackground(entry, plan)
		}
	}
	datasetBrowser.ShowDialog(dialog.NewUnmountDialog(datasetBrowser.application, title, formatUnmountDescription(plan), confirm, nil, onComplete), nil)
}

// runMountPlanInBackground does what the plan says in the background and shows the result. Must be called on the UI
// thread.
func (datasetBrowser *DatasetBrowserComponent) runMountPlanInBackground(entry *zfs.DatasetListEntry, plan *zfs.MountPlan) {
	go func() {
		err := runMountPlan(plan)
		datasetBrowser.application.QueueUpdateDraw(func() {
			if err == nil {
				datasetBrowser.mounted(plan)
				return
			}
			logging.Error("Failed to %s: %s", plan.Name(), err.Error())
			// a retry plans again, and asks again before unmounting
			retry := func() { datasetBrowser.startMountAction(entry, plan.Action) }
			datasetBrowser.ShowDialog(dialog.NewErrorDialogWithRetry(datasetBrowser.application, "Failed", err, retry), nil)
			// some of it may be unmounted, also after an error
			zfs.RefreshZfsData()
		})
	}()
}
