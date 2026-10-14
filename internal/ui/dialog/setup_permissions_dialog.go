package dialog

import (
	"errors"
	"fmt"
	"strings"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/rivo/tview"
)

const (
	SetupPermissionsDialogPage util.Page = "SetupPermissionsDialog"

	SetupPermissionsDialogAllowActionId DialogActionId = iota
	SetupPermissionsDialogAllowAllActionId
	SetupPermissionsDialogCopyActionId
	SetupPermissionsDialogNeverAskActionId
)

// errNothingSelected is returned when allowing with no rule selected: the dialog stays open.
var errNothingSelected = errors.New("nothing selected")

// NewSetupPermissionsDialog offers to allow the commands that need root (see zfs.SudoersRules): the user
// checks the rules in a checklist (or allows all), which are then allowed by running the setup command with sudo in
// the terminal (or copies that command). allowed are the ids of the rules allowed already (see
// zfs.AllowedSudoersRules), they are not offered, nor the ones whose program is not installed. The rules that only read are checked. If onNeverAsk is not nil
// (the dialog was shown without being asked for), it is offered not to show the dialog again, and onNeverAsk is
// called on the UI thread if chosen.
func NewSetupPermissionsDialog(application *tview.Application, allowed map[string]bool, onNeverAsk func()) *SelectionDialog {
	var offered []*zfs.SudoersRule
	var checklist []*ChecklistItem
	for _, rule := range zfs.SudoersRules {
		if !allowed[rule.Id] && rule.IsAvailable() {
			offered = append(offered, rule)
			checklist = append(checklist, &ChecklistItem{Name: rule.Name + ": " + rule.Summary, Checked: rule.ReadOnly})
		}
	}

	// granted are the rules of the running action. Written by asyncWork, read by onComplete, which runs after it.
	var granted []*zfs.SudoersRule
	asyncWork := func(d *SelectionDialog, action DialogActionId) error {
		granted = nil
		for i, checked := range d.Checked() {
			if checked || action == SetupPermissionsDialogAllowAllActionId {
				granted = append(granted, offered[i])
			}
		}
		switch action {
		case SetupPermissionsDialogAllowActionId, SetupPermissionsDialogAllowAllActionId:
			if len(granted) == 0 {
				return errNothingSelected
			}
			explanation := "allowing your user to run these commands with sudo without a password " +
				"(sudoers rules, see \"zfs-file-history setup --help\"): " + ruleNames(granted) + "."
			if err := grantPermissions(application, explanation, [][]string{setupCommand(granted)}); err != nil {
				return err
			}
			// e.g. the event watcher starts again
			zfs.SudoersChanged.Emit(struct{}{})
		case SetupPermissionsDialogCopyActionId:
			if len(granted) == 0 {
				return errNothingSelected
			}
			return copyToClipboard("sudo " + strings.Join(setupCommand(granted), " "))
		}
		return nil
	}

	onComplete := func(d *SelectionDialog, option *DialogOption, err error) {
		switch {
		case errors.Is(err, errNothingSelected):
			// stays open, to check something
		case err != nil && option.Id == SetupPermissionsDialogCopyActionId:
			d.ShowFollowUp(NewErrorDialog(application, "Copying Failed",
				fmt.Errorf("%w\n\nThe command is:\n\nsudo %s", err, strings.Join(setupCommand(granted), " "))))
		case err != nil:
			// sudo printed why in the terminal, e.g. a wrong password
			logging.Error("Running sudo failed: %v", err)
			d.ShowFollowUp(NewErrorDialog(application, "Running sudo Failed", err))
		case option.Id == SetupPermissionsDialogCopyActionId:
			d.SetOptionName(SetupPermissionsDialogCopyActionId, "📋 Copied, run it in a terminal")
		case option.Id == SetupPermissionsDialogNeverAskActionId:
			onNeverAsk()
			d.Close()
		case option.Id == SetupPermissionsDialogAllowActionId || option.Id == SetupPermissionsDialogAllowAllActionId:
			d.ShowFollowUp(NewSuccessDialog(application, "Permissions Allowed", "Allowed: "+ruleNames(granted)+"."))
		}
	}

	var options []*DialogOption
	closeName := "Close"
	if len(offered) > 0 {
		options = append(options,
			&DialogOption{Id: SetupPermissionsDialogAllowActionId, Name: "🔓 Allow checked (asks for your sudo password)"},
			&DialogOption{Id: SetupPermissionsDialogAllowAllActionId, Name: "🔓 Allow all (asks for your sudo password)"},
			&DialogOption{Id: SetupPermissionsDialogCopyActionId, Name: "📋 Copy command for the checked ones"},
		)
		if onNeverAsk != nil {
			options = append(options, &DialogOption{Id: SetupPermissionsDialogNeverAskActionId, Name: "🔕 Don't ask again"})
			closeName = "Not now"
		}
	}
	options = append(options, &DialogOption{Id: DialogCloseActionId, Name: closeName})

	return NewSelectionDialogWithChecklist(
		application,
		string(SetupPermissionsDialogPage),
		" 🔓 Set Up Permissions ",
		formatSetupPermissions(offered),
		checklist,
		options,
		asyncWork,
		onComplete,
	)
}

// formatSetupPermissions explains why the rules need root, and what each of them is good for.
func formatSetupPermissions(offered []*zfs.SudoersRule) string {
	if len(offered) == 0 {
		return "Your user may already run all commands zfs-file-history needs root for."
	}
	commands := make([]string, 0, len(offered))
	for _, rule := range offered {
		commands = append(commands, rule.Commands)
	}
	return "Some features need commands that only root may run. Your user can be allowed to run exactly these " +
		"with sudo, without a password (rules in /etc/sudoers.d/zfs-file-history): " + strings.Join(commands, "; ") +
		".\n\nCheck them with ↑/↓ and Space. Allowing runs \"zfs-file-history setup\" with sudo, which asks for your " +
		"password. Also in the command menu (:) later."
}

// ruleNames returns the names of the rules, for messages.
func ruleNames(rules []*zfs.SudoersRule) string {
	names := make([]string, 0, len(rules))
	for _, rule := range rules {
		names = append(names, rule.Name)
	}
	return strings.Join(names, ", ")
}

// setupCommand returns the command that allows the rules, run by root: the setup command of this executable, see
// cmd/setup.go.
func setupCommand(rules []*zfs.SudoersRule) []string {
	executable, err := executablePath()
	if err != nil {
		executable = "zfs-file-history"
	}
	command := []string{executable, "setup"}
	for _, rule := range rules {
		command = append(command, "--"+rule.Id)
	}
	return command
}
