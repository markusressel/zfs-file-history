package dialog

import (
	"fmt"
	"os"
	"strings"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/localization"
	"zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/rivo/tview"
)

const (
	PoolActionRootDialogPage util.Page = "PoolActionRootDialog"

	PoolActionRootDialogRunOnceActionId DialogActionId = iota
	PoolActionRootDialogAllowActionId
	PoolActionRootDialogCopyActionId
	PoolActionRootDialogRetryActionId
)

// executablePath is replaceable in tests.
var executablePath = os.Executable

// NewPoolActionRootDialog explains that a pool action needs root (ZFS cannot delegate pool operations like it does
// for datasets), and offers to run it once with sudo, or to allow the pool actions for the user ("setup
// --pool-actions") and retry, or to copy that command. Both ask for the sudo password in the terminal.
// retry is called on the UI thread to repeat the action once it is allowed. retry may be nil, the user is then
// asked to try again. Actions the setup does not allow (e.g. enabling single features, see
// zfs.PoolActionNeedsRootError.AllowedBySetup) can only be run once, or copied to run them in a terminal.
func NewPoolActionRootDialog(application *tview.Application, needsRoot *zfs.PoolActionNeedsRootError, retry func()) *SelectionDialog {
	setup := setupPoolActionsCommand()
	allowable := needsRoot.AllowedBySetup()
	// what "Copy command" copies: the setup, or the commands themselves if the setup does not allow them
	copied := "sudo " + strings.Join(setup, " ")
	if !allowable {
		var commands []string
		for _, command := range needsRoot.Commands() {
			commands = append(commands, "sudo "+strings.Join(command, " "))
		}
		copied = strings.Join(commands, " && ")
	}

	asyncWork := func(d *SelectionDialog, action DialogActionId) error {
		switch action {
		case PoolActionRootDialogRunOnceActionId:
			explanation := fmt.Sprintf("running zpool as root to %s on %s.", needsRoot.Action.Name, needsRoot.Pool)
			return grantPermissions(application, explanation, needsRoot.Commands())
		case PoolActionRootDialogAllowActionId:
			explanation := "allowing your user to scrub, TRIM, upgrade and clear the errors of pools without a password " +
				"(sudoers rules, see \"zfs-file-history setup --help\")."
			return grantPermissions(application, explanation, [][]string{setup})
		case PoolActionRootDialogCopyActionId:
			return copyToClipboard(copied)
		}
		return nil
	}

	onComplete := func(d *SelectionDialog, option *DialogOption, err error) {
		switch {
		case err != nil && option.Id == PoolActionRootDialogCopyActionId:
			d.ShowFollowUp(NewErrorDialog(application, "Copying Failed", fmt.Errorf("%w\n\nThe command is:\n\n%s", err, copied)))
		case err != nil:
			// sudo printed why in the terminal, e.g. a wrong password or "there is no active scrub"
			logging.Error("Running sudo failed: %v", err)
			d.ShowFollowUp(NewErrorDialog(application, "Running sudo Failed", err))
		case option.Id == PoolActionRootDialogCopyActionId:
			// stays open, so the action can be retried once the command was run
			d.SetOptionName(PoolActionRootDialogCopyActionId, "📋 Copied, run it in a terminal")
		case option.Id == PoolActionRootDialogRunOnceActionId:
			// the event watcher may not run (see "setup"), so the pools are reloaded here
			zfs.PoolsChanged.Emit(struct{}{})
			d.ShowFollowUp(NewSuccessDialog(application, "Done", fmt.Sprintf("%s of %s.", needsRoot.Action.Done, needsRoot.Pool)))
		case retry != nil:
			d.Chain(retry)
		default:
			d.ShowFollowUp(NewSuccessDialog(application, "Pool Actions Allowed",
				fmt.Sprintf("You may now %s on %s. Please try again.", needsRoot.Action.Name, needsRoot.Pool)))
		}
	}

	allowName := "🔓 Always allow (asks for your sudo password)"
	if retry != nil {
		allowName = "🔓 Always allow and retry (asks for your sudo password)"
	}
	options := []*DialogOption{
		{Id: PoolActionRootDialogRunOnceActionId, Name: "🔑 Run once with sudo (asks for your sudo password)"},
	}
	if allowable {
		options = append(options, &DialogOption{Id: PoolActionRootDialogAllowActionId, Name: allowName})
	}
	options = append(options, &DialogOption{Id: PoolActionRootDialogCopyActionId, Name: "📋 Copy command"})
	if retry != nil {
		options = append(options, &DialogOption{Id: PoolActionRootDialogRetryActionId, Name: "🔁 Retry (after running the command yourself)"})
	}
	options = append(options, &DialogOption{Id: DialogCloseActionId, Name: localization.LocalizationCommonClose})

	return NewSelectionDialog(
		application,
		string(PoolActionRootDialogPage),
		" 🔑 Root Required ",
		formatPoolActionNeedsRoot(needsRoot, setup, allowable),
		options,
		asyncWork,
		onComplete,
	)
}

// formatPoolActionNeedsRoot explains why the action needs root, and how to allow it.
// Without allowable, the setup does not allow the action, so it is not mentioned.
func formatPoolActionNeedsRoot(needsRoot *zfs.PoolActionNeedsRootError, setup []string, allowable bool) string {
	var description strings.Builder
	fmt.Fprintf(&description, "To %s on %s, zpool has to run as root: ZFS cannot delegate pool operations to users "+
		"like it does for datasets (zfs allow).", needsRoot.Action.Name, needsRoot.Pool)
	if needsRoot.Cause != nil {
		fmt.Fprintf(&description, "\n\nReason: %s", needsRoot.Cause.Error())
	}
	if !allowable {
		description.WriteString("\n\nIt cannot be allowed without a password like the other pool actions: it cannot " +
			"be undone, so sudo asks every time.")
		return description.String()
	}
	description.WriteString("\n\nYour user can be allowed to scrub, TRIM, upgrade and clear the errors of pools without a " +
		"password (nothing else, e.g. no destroying) with:\n")
	fmt.Fprintf(&description, "\n  sudo %s", strings.Join(setup, " "))
	return description.String()
}

// setupPoolActionsCommand returns the command that allows the pool actions, see setupCommand.
func setupPoolActionsCommand() []string {
	return setupCommand([]*zfs.SudoersRule{zfs.SudoersRulePoolActions})
}
