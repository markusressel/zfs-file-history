package dialog

import (
	"fmt"
	"strings"
	"zfs-file-history/internal/ui/localization"
	"zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/rivo/tview"
)

const (
	DatasetActionDialogPage util.Page = "DatasetActionDialog"

	DatasetDialogOpenInFileBrowserActionId DialogActionId = iota
	DatasetDialogCreateSnapshotActionId
	DatasetDialogCreateSnapshotRecursivelyActionId
	DatasetDialogPermissionsActionId
	DatasetDialogPropertiesActionId
	DatasetDialogPromoteActionId
	DatasetDialogDestroyActionId
	DatasetDialogMountActionId
	DatasetDialogUnmountActionId
	DatasetDialogLoadKeyActionId
	DatasetDialogUnloadKeyActionId
)

// NewDatasetActionDialog creates the dialog with the actions for a dataset. Promoting is only offered for clones,
// destroying for all datasets but the root dataset of a pool (which is only destroyed with the pool), mounting and
// unmounting, loading and unloading the key depending on the state of the dataset.
func NewDatasetActionDialog(
	application *tview.Application,
	dataset *zfs.DatasetListEntry,
	asyncWork func(d *SelectionDialog, action DialogActionId) error,
	onComplete func(d *SelectionDialog, option *DialogOption, err error),
) *SelectionDialog {
	return newTaggedSelectionDialog(
		application,
		string(DatasetActionDialogPage),
		localization.LocalizationSelectActionDialogTitle,
		fmt.Sprintf("What do you want to do with dataset %s?", Highlight(dataset.Name)),
		nil,
		buildDatasetDialogOptions(dataset),
		asyncWork,
		onComplete,
	)
}

// buildDatasetDialogOptions returns the options of the dataset action dialog, see NewDatasetActionDialog.
func buildDatasetDialogOptions(dataset *zfs.DatasetListEntry) []*DialogOption {
	var options []*DialogOption
	if dataset.MountPath != "" {
		options = append(options, &DialogOption{
			Id:          DatasetDialogOpenInFileBrowserActionId,
			Name:        "📂 Open in file browser",
			Description: fmt.Sprintf("Show the files of the dataset (%s)", dataset.MountPath),
		})
	}
	options = append(options,
		&DialogOption{
			Id:          DatasetDialogCreateSnapshotActionId,
			Name:        "📸 Create snapshot",
			Description: "Create a snapshot of the dataset (zfs snapshot)",
		},
		&DialogOption{
			Id:          DatasetDialogCreateSnapshotRecursivelyActionId,
			Name:        "📸 Create snapshot (recursive)",
			Description: "Create a snapshot of the dataset and all its child datasets at once (zfs snapshot -r)",
		},
		&DialogOption{
			Id:          DatasetDialogPermissionsActionId,
			Name:        "🔑 Permissions",
			Description: "Show and change the delegated ZFS permissions (zfs allow)",
		},
		&DialogOption{
			Id:          DatasetDialogPropertiesActionId,
			Name:        "⚙️ Properties",
			Description: "Show and change the ZFS properties (zfs get/set)",
		},
	)
	options = append(options, buildMountDialogOptions(dataset)...)
	if dataset.IsClone() {
		options = append(options, &DialogOption{
			Id:          DatasetDialogPromoteActionId,
			Name:        "🔀 Promote clone",
			Description: fmt.Sprintf("Make the clone independent of '%s', after asking (zfs promote)", dataset.Origin),
			Severity:    DialogSeverityWarning,
		})
	}
	if strings.Contains(dataset.Name, "/") {
		options = append(options, &DialogOption{
			Id:          DatasetDialogDestroyActionId,
			Name:        "💥 Destroy dataset",
			Description: "Destroy the dataset with its files, after a dry run and typing its name (zfs destroy)",
			Severity:    DialogSeverityDanger,
		})
	}
	return append(options, &DialogOption{Id: DialogCloseActionId, Name: localization.LocalizationCommonClose})
}

// buildMountDialogOptions returns the options of the dataset action dialog that mount or unmount the dataset, or load
// or unload its key: only those that zfs would not refuse right away (see zfs.PlanMountAction).
func buildMountDialogOptions(dataset *zfs.DatasetListEntry) []*DialogOption {
	var options []*DialogOption
	locked := dataset.KeyStatus == "unavailable"
	switch {
	case dataset.MountPath != "":
		options = append(options, &DialogOption{
			Id:          DatasetDialogUnmountActionId,
			Name:        "⏏️ Unmount",
			Description: fmt.Sprintf("Unmount the dataset from %s with everything mounted below it, after asking (zfs unmount)", dataset.MountPath),
			Severity:    DialogSeverityWarning,
		})
	case dataset.CanMount != "off" && dataset.Mountpoint != "none" && dataset.Mountpoint != "legacy" && locked:
		options = append(options, &DialogOption{
			Id:          DatasetDialogMountActionId,
			Name:        "🔓 Load key and mount",
			Description: fmt.Sprintf("Load the key of %s and mount the dataset at %s (zfs mount -l)", dataset.EncryptionRoot, dataset.Mountpoint),
		})
	case dataset.CanMount != "off" && dataset.Mountpoint != "none" && dataset.Mountpoint != "legacy":
		options = append(options, &DialogOption{
			Id:          DatasetDialogMountActionId,
			Name:        "📁 Mount",
			Description: fmt.Sprintf("Mount the dataset at %s (zfs mount)", dataset.Mountpoint),
		})
	}
	switch {
	case dataset.EncryptionRoot == "":
	case locked:
		options = append(options, &DialogOption{
			Id:          DatasetDialogLoadKeyActionId,
			Name:        "🔑 Load key",
			Description: fmt.Sprintf("Load the key of %s, without mounting (zfs load-key)", dataset.EncryptionRoot),
		})
	case dataset.KeyStatus == "available":
		options = append(options, &DialogOption{
			Id:   DatasetDialogUnloadKeyActionId,
			Name: "🔒 Unload key",
			Description: fmt.Sprintf("Unmount the datasets encrypted with the key of %s and unload it, after asking (zfs unload-key)",
				dataset.EncryptionRoot),
			Severity: DialogSeverityWarning,
		})
	}
	return options
}

const (
	UnmountDialogPage util.Page = "UnmountDialog"

	UnmountDialogUnmountActionId DialogActionId = iota
)

// NewUnmountDialog asks for confirmation before unmounting datasets (also to unload a key). Nothing is lost, but
// programs that use their files fail. The description should list what is unmounted, it may contain style tags (see
// Highlight). confirmLabel names the action, e.g. "Unmount". asyncWork receives UnmountDialogUnmountActionId on
// confirmation.
func NewUnmountDialog(
	application *tview.Application,
	title string,
	description string,
	confirmLabel string,
	asyncWork func(d *SelectionDialog, action DialogActionId) error,
	onComplete func(d *SelectionDialog, option *DialogOption, err error),
) *SelectionDialog {
	return newTaggedSelectionDialog(
		application,
		string(UnmountDialogPage),
		title,
		description,
		nil,
		buildConfirmDialogOptions(UnmountDialogUnmountActionId, confirmLabel, true, DialogSeverityWarning),
		asyncWork,
		onComplete,
	)
}

const (
	PromoteCloneDialogPage util.Page = "PromoteCloneDialog"

	PromoteCloneDialogPromoteActionId DialogActionId = iota
)

// NewPromoteCloneDialog asks for confirmation before promoting a clone. The description should explain what changes,
// it may contain style tags (see Highlight).
// asyncWork receives PromoteCloneDialogPromoteActionId on confirmation.
func NewPromoteCloneDialog(
	application *tview.Application,
	description string,
	asyncWork func(d *SelectionDialog, action DialogActionId) error,
	onComplete func(d *SelectionDialog, option *DialogOption, err error),
) *SelectionDialog {
	return newTaggedSelectionDialog(
		application,
		string(PromoteCloneDialogPage),
		" 🔀 Promote Clone ",
		description,
		nil,
		buildConfirmDialogOptions(PromoteCloneDialogPromoteActionId, "Promote", true, DialogSeverityWarning),
		asyncWork,
		onComplete,
	)
}

const (
	DestroyDatasetDialogPage util.Page = "DestroyDatasetDialog"

	DestroyDatasetDialogContinueActionId DialogActionId = iota
)

// NewDestroyDatasetDialog is the first confirmation of destroying a dataset that has snapshots or child datasets:
// they are only destroyed with it if the checklist item recursive (e.g. "Also destroy its 2 child datasets and 3
// snapshots") is checked, see SelectionDialog.Checked. The description should list what would be destroyed, it may
// contain style tags (see Highlight). asyncWork receives DestroyDatasetDialogContinueActionId on confirmation; the
// name of the dataset is typed afterwards, to destroy it.
func NewDestroyDatasetDialog(
	application *tview.Application,
	description string,
	recursive string,
	asyncWork func(d *SelectionDialog, action DialogActionId) error,
	onComplete func(d *SelectionDialog, option *DialogOption, err error),
) *SelectionDialog {
	return newTaggedSelectionDialog(
		application,
		string(DestroyDatasetDialogPage),
		" 💥 Destroy Dataset ",
		description,
		[]*ChecklistItem{{Name: recursive}},
		buildConfirmDialogOptions(DestroyDatasetDialogContinueActionId, "Continue", true, DialogSeverityDanger),
		asyncWork,
		onComplete,
	).SetDanger()
}
