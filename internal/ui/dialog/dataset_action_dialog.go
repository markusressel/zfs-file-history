package dialog

import (
	"fmt"
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
	DatasetDialogDestroyCloneActionId
)

// NewDatasetActionDialog creates the dialog with the actions for a dataset. Promoting and destroying are only offered
// for clones: other datasets are never destroyed by the application.
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
	if dataset.IsClone() {
		options = append(options,
			&DialogOption{
				Id:          DatasetDialogPromoteActionId,
				Name:        "🔀 Promote clone",
				Description: fmt.Sprintf("Make the clone independent of '%s', after asking (zfs promote)", dataset.Origin),
				Severity:    DialogSeverityWarning,
			},
			&DialogOption{
				Id:          DatasetDialogDestroyCloneActionId,
				Name:        "💥 Destroy clone",
				Description: "Destroy the clone, after a dry run and typing its name (zfs destroy)",
				Severity:    DialogSeverityDanger,
			},
		)
	}
	return append(options, &DialogOption{Id: DialogCloseActionId, Name: localization.LocalizationCommonClose})
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
	DestroyCloneDialogPage util.Page = "DestroyCloneDialog"

	DestroyCloneDialogContinueActionId DialogActionId = iota
)

// NewDestroyCloneDialog is the first confirmation of destroying a clone that has snapshots or child datasets: they
// are only destroyed with it if the checklist item recursive (e.g. "Also destroy its 3 snapshots and child datasets")
// is checked, see SelectionDialog.Checked. The description should list what would be destroyed, it may contain
// style tags (see Highlight). asyncWork receives
// DestroyCloneDialogContinueActionId on confirmation; the name of the clone is typed afterwards, to destroy it.
func NewDestroyCloneDialog(
	application *tview.Application,
	description string,
	recursive string,
	asyncWork func(d *SelectionDialog, action DialogActionId) error,
	onComplete func(d *SelectionDialog, option *DialogOption, err error),
) *SelectionDialog {
	return newTaggedSelectionDialog(
		application,
		string(DestroyCloneDialogPage),
		" 💥 Destroy Clone ",
		description,
		[]*ChecklistItem{{Name: recursive}},
		buildConfirmDialogOptions(DestroyCloneDialogContinueActionId, "Continue", true, DialogSeverityDanger),
		asyncWork,
		onComplete,
	).SetDanger()
}
