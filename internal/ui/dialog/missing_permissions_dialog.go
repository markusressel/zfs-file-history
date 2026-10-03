package dialog

import (
	"errors"
	"fmt"
	"strings"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/localization"
	"zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/rivo/tview"
)

const (
	MissingPermissionsDialogPage util.Page = "MissingPermissionsDialog"

	MissingPermissionsDialogGrantActionId DialogActionId = iota
	MissingPermissionsDialogCopyActionId
	MissingPermissionsDialogRetryActionId
)

// grantPermissions, copyToClipboard and verifyPermissions are replaceable in tests, so tests never run sudo or zfs.
var (
	grantPermissions  = util.RunWithSudoInTerminal
	copyToClipboard   = util.CopyToClipboard
	verifyPermissions = (*zfs.MissingPermissionsError).Verify
)

// NewMissingPermissionsDialog explains which ZFS permissions the user lacks for an operation and offers to delegate
// them ("zfs allow"), either by running the command with sudo or by copying it.
// Once the permissions are granted (checked with zfs.MissingPermissionsError.Verify), retry is called on the UI
// thread to repeat the operation. retry may be nil, the user is then asked to try again.
func NewMissingPermissionsDialog(application *tview.Application, missing *zfs.MissingPermissionsError, retry func()) *SelectionDialog {
	commands := sudoCommands(missing.GrantCommands())

	asyncWork := func(d *SelectionDialog, action DialogActionId) error {
		switch action {
		case MissingPermissionsDialogGrantActionId:
			explanation := fmt.Sprintf("delegating the ZFS permissions needed to %s to user %s.", missing.Action, missing.User)
			if err := grantPermissions(application, explanation, missing.GrantCommands()); err != nil {
				return &grantError{err}
			}
			// sudo succeeded, but only retry if the permissions are in effect now
			return verifyPermissions(missing)
		case MissingPermissionsDialogCopyActionId:
			return copyToClipboard(strings.Join(commands, "\n"))
		case MissingPermissionsDialogRetryActionId:
			return verifyPermissions(missing)
		}
		return nil
	}

	onComplete := func(d *SelectionDialog, option *DialogOption, err error) {
		if err == nil && (option.Id == MissingPermissionsDialogGrantActionId || option.Id == MissingPermissionsDialogRetryActionId) {
			// verified to be in effect: the displayed permissions are outdated (e.g. the Perms column)
			zfs.PermissionsChanged.Emit(struct{}{})
		}
		var stillMissing *zfs.MissingPermissionsError
		var failedGrant *grantError
		switch {
		case errors.As(err, &failedGrant):
			logging.Error("Granting ZFS permissions failed: %v", failedGrant.err)
			d.ShowFollowUp(NewErrorDialog(application, "Granting Permissions Failed", failedGrant.err))
		case errors.As(err, &stillMissing):
			d.ShowFollowUp(NewMissingPermissionsDialog(application, stillMissing, retry))
		case err != nil && option.Id == MissingPermissionsDialogCopyActionId:
			d.ShowFollowUp(NewErrorDialog(application, "Copying Failed", fmt.Errorf("%w\n\nThe command is:\n\n%s", err, strings.Join(commands, "\n"))))
		case err != nil:
			d.ShowFollowUp(NewErrorDialog(application, "Checking Permissions Failed", err))
		case option.Id == MissingPermissionsDialogCopyActionId:
			// stays open, so the operation can be retried once the command was run
			d.SetOptionName(MissingPermissionsDialogCopyActionId, "📋 Copied, run it in a terminal")
		case retry != nil:
			d.Chain(retry)
		default:
			d.ShowFollowUp(NewSuccessDialog(application, "Permissions Granted",
				fmt.Sprintf("User %s may now %s. Please try again.", missing.User, missing.Action)))
		}
	}

	grantName := "🔑 Grant now (asks for your sudo password)"
	if retry != nil {
		grantName = "🔑 Grant and retry (asks for your sudo password)"
	}
	options := []*DialogOption{
		{Id: MissingPermissionsDialogGrantActionId, Name: grantName},
		{Id: MissingPermissionsDialogCopyActionId, Name: "📋 Copy command"},
	}
	if retry != nil {
		options = append(options, &DialogOption{Id: MissingPermissionsDialogRetryActionId, Name: "🔁 Retry (after running the command yourself)"})
	}
	options = append(options, &DialogOption{Id: DialogCloseActionId, Name: localization.LocalizationCommonClose})

	return NewSelectionDialog(
		application,
		string(MissingPermissionsDialogPage),
		" 🔑 Missing ZFS Permissions ",
		formatMissingPermissions(missing, commands),
		options,
		asyncWork,
		onComplete,
	)
}

// grantError marks a failure of running the grant commands, as opposed to permissions still missing afterwards.
type grantError struct {
	err error
}

func (e *grantError) Error() string { return e.err.Error() }

// formatMissingPermissions describes the missing permissions and how to grant them.
func formatMissingPermissions(missing *zfs.MissingPermissionsError, commands []string) string {
	var description strings.Builder
	fmt.Fprintf(&description, "To %s, user %s needs these ZFS permissions:\n", missing.Action, missing.User)
	for _, gap := range missing.Missing {
		names := make([]string, 0, len(gap.Permissions))
		for _, permission := range gap.Permissions {
			names = append(names, string(permission))
		}
		fmt.Fprintf(&description, "\n  %s on %s", strings.Join(names, ", "), gap.Dataset)
	}
	if missing.Cause != nil {
		fmt.Fprintf(&description, "\n\nReason: %s", missing.Cause.Error())
	}
	description.WriteString("\n\nThey can be delegated by root with:\n")
	for _, command := range commands {
		fmt.Fprintf(&description, "\n  %s", command)
	}
	return description.String()
}

func sudoCommands(commands [][]string) []string {
	result := make([]string, 0, len(commands))
	for _, command := range commands {
		result = append(result, "sudo "+strings.Join(command, " "))
	}
	return result
}
