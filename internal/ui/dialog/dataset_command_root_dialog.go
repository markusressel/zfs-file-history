package dialog

import (
	"fmt"
	"strings"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/localization"
	"zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/rivo/tview"
)

const (
	DatasetCommandRootDialogPage util.Page = "DatasetCommandRootDialog"

	DatasetCommandRootDialogRunOnceActionId DialogActionId = iota
	DatasetCommandRootDialogCopyActionId
	DatasetCommandRootDialogRetryActionId
)

// NewDatasetCommandRootDialog explains that a command on a dataset (e.g. mounting it) needs root, and offers to run
// the commands left once with sudo (asking for the password in the terminal), or to copy them. There is no sudoers
// rule for them, see zfs.DatasetCommandNeedsRootError. retry is called on the UI thread to start over once the user
// ran the commands; it may be nil.
func NewDatasetCommandRootDialog(application *tview.Application, needsRoot *zfs.DatasetCommandNeedsRootError, retry func()) *SelectionDialog {
	copied := strings.Join(sudoCommands(needsRoot.Commands), " && ")

	asyncWork := func(d *SelectionDialog, action DialogActionId) error {
		switch action {
		case DatasetCommandRootDialogRunOnceActionId:
			return grantPermissions(application, fmt.Sprintf("running zfs as root to %s.", needsRoot.Action), needsRoot.Commands)
		case DatasetCommandRootDialogCopyActionId:
			return copyToClipboard(copied)
		}
		return nil
	}

	onComplete := func(d *SelectionDialog, option *DialogOption, err error) {
		switch {
		case err != nil && option.Id == DatasetCommandRootDialogCopyActionId:
			d.ShowFollowUp(NewErrorDialog(application, "Copying Failed", fmt.Errorf("%w\n\nThe commands are:\n\n%s", err, copied)))
		case err != nil:
			// sudo printed why in the terminal, e.g. a wrong password or "target is busy"
			logging.Error("Running sudo failed: %v", err)
			// some of the commands may have run
			zfs.RefreshZfsData()
			d.ShowFollowUp(NewErrorDialog(application, "Running sudo Failed", err))
		case option.Id == DatasetCommandRootDialogCopyActionId:
			// stays open, so it can be retried once the commands were run
			d.SetOptionName(DatasetCommandRootDialogCopyActionId, "📋 Copied, run them in a terminal")
		case option.Id == DatasetCommandRootDialogRunOnceActionId:
			// mounts are not zfs events, the datasets are reloaded here
			zfs.RefreshZfsData()
			d.ShowFollowUp(NewSuccessDialog(application, "Done", needsRoot.Done+"."))
		case retry != nil:
			d.Chain(retry)
		}
	}

	options := []*DialogOption{
		{Id: DatasetCommandRootDialogRunOnceActionId, Name: "🔑 Run once with sudo (asks for your sudo password)"},
		{Id: DatasetCommandRootDialogCopyActionId, Name: "📋 Copy commands"},
	}
	if retry != nil {
		options = append(options, &DialogOption{Id: DatasetCommandRootDialogRetryActionId, Name: "🔁 Retry (after running the commands yourself)"})
	}
	options = append(options, &DialogOption{Id: DialogCloseActionId, Name: localization.LocalizationCommonClose})

	return NewSelectionDialog(
		application,
		string(DatasetCommandRootDialogPage),
		" 🔑 Root Required ",
		formatDatasetCommandNeedsRoot(needsRoot),
		options,
		asyncWork,
		onComplete,
	)
}

// formatDatasetCommandNeedsRoot explains why the commands need root, and lists them.
func formatDatasetCommandNeedsRoot(needsRoot *zfs.DatasetCommandNeedsRootError) string {
	var description strings.Builder
	fmt.Fprintf(&description, "To %s, zfs has to run as root: Linux only lets root mount and unmount file systems, "+
		"whatever was delegated with zfs allow.", needsRoot.Action)
	if needsRoot.Cause != nil {
		fmt.Fprintf(&description, "\n\nReason: %s", needsRoot.Cause.Error())
	}
	description.WriteString("\n\nThe setup cannot allow it without a password: a rule for zfs mount would let your user " +
		"mount any dataset, also over the directories of the system. It runs:\n")
	for _, command := range sudoCommands(needsRoot.Commands) {
		fmt.Fprintf(&description, "\n  %s", command)
	}
	return description.String()
}
