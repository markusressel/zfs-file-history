package dialog

import (
	"fmt"
	"strings"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	"zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	DatasetPermissionsDialogPage util.Page = "DatasetPermissionsDialog"

	// datasetPermissionsDetailLines is the height of the details of the selected permission
	datasetPermissionsDetailLines = 4
	// datasetPermissionsHeaderRows are the rows of the permission table above the permissions
	datasetPermissionsHeaderRows = 1
	// datasetPermissionsShortcutLines is the height of the shortcut map
	// (see TestDatasetPermissionsDialog_ShortcutsFit)
	datasetPermissionsShortcutLines = 2
	// datasetPermissionsContentWidth is the width of the dialog's content
	datasetPermissionsContentWidth = 76

	ApplyDelegationsDialogApplyActionId DialogActionId = iota
)

// Replaceable in tests, so tests never run zfs or sudo.
var (
	loadDatasetPermissions = zfs.LoadDatasetPermissions
	applyDelegationChange  = zfs.ApplyDelegationChange
)

// DatasetPermissionsDialog shows which ZFS permissions the current user has on a dataset, who grants them, and the
// delegations ("zfs allow") of the dataset and its parents. Permissions can be granted to and revoked from the
// current user, for the dataset and its children ("zfs allow -u <user> <permission> <dataset>"). Other grantees and
// scopes are left to "zfs allow" in a shell: zfs-file-history is mostly used by single users.
type DatasetPermissionsDialog struct {
	application   *tview.Application
	permissions   *zfs.DatasetPermissions
	layout        *tview.Flex
	actionChannel chan DialogActionId
	// pages the dialog is shown on, set by ShowDialogOnPages. Used for the dialogs opened from this one.
	pages *tview.Pages
	// onChanged is called on the UI thread after the delegations were changed, may be nil
	onChanged func()

	// pending are the toggled permissions, with their new state (granted or revoked)
	pending map[zfs.Permission]bool

	summary     *tview.TextView
	table       *tview.Table
	details     *tview.TextView
	delegations *tview.TextView
}

// NewDatasetPermissionsDialog creates the dialog. onChanged is called on the UI thread after the delegations were
// changed in the dialog, it may be nil.
func NewDatasetPermissionsDialog(application *tview.Application, permissions *zfs.DatasetPermissions, onChanged func()) *DatasetPermissionsDialog {
	d := &DatasetPermissionsDialog{
		application:   application,
		permissions:   permissions,
		actionChannel: make(chan DialogActionId),
		onChanged:     onChanged,
		pending:       map[zfs.Permission]bool{},
	}
	d.createLayout()
	return d
}

func (d *DatasetPermissionsDialog) GetName() string {
	return string(DatasetPermissionsDialogPage)
}

func (d *DatasetPermissionsDialog) GetLayout() *tview.Flex {
	return d.layout
}

func (d *DatasetPermissionsDialog) GetActionChannel() <-chan DialogActionId {
	return d.actionChannel
}

func (d *DatasetPermissionsDialog) setPages(pages *tview.Pages) {
	d.pages = pages
}

func (d *DatasetPermissionsDialog) Close() {
	emitDialogActions(d.actionChannel, DialogCloseActionId)
}

func (d *DatasetPermissionsDialog) createLayout() {
	d.summary = tview.NewTextView().SetDynamicColors(true)

	d.table = tview.NewTable().SetSelectable(true, false)
	d.table.SetSelectedStyle(tcell.StyleDefault.
		Foreground(theme.Colors.Layout.Table.SelectedForeground).
		Background(theme.Colors.Layout.Table.SelectedBackground))
	d.table.SetSelectionChangedFunc(func(row, column int) { d.updateDetails(row) })
	d.details = tview.NewTextView().SetWrap(true).SetWordWrap(true)
	d.delegations = tview.NewTextView().SetDynamicColors(true)

	d.refresh()
	rows := d.table.GetRowCount()
	_, delegationLines := d.formatDelegations()
	d.table.Select(datasetPermissionsHeaderRows, 0)
	d.updateDetails(datasetPermissionsHeaderRows)

	shortcuts := shortcut_helper.NewShortcutMap(d.application)
	shortcuts.SetEntries(datasetPermissionsShortcuts(d.permissions.IsRoot))

	content := tview.NewFlex().SetDirection(tview.FlexRow)
	content.AddItem(d.summary, 1, 0, false)
	content.AddItem(tview.NewBox(), 1, 0, false)
	content.AddItem(d.table, rows, 0, true)
	content.AddItem(tview.NewBox(), 1, 0, false)
	content.AddItem(d.details, datasetPermissionsDetailLines, 0, false)
	content.AddItem(tview.NewBox(), 1, 0, false)
	// shrinks (and scrolls) on small terminals
	content.AddItem(d.delegations, 0, 1, false)
	content.AddItem(shortcuts.GetLayout(), datasetPermissionsShortcutLines, 0, false)

	title := fmt.Sprintf(" 🔑 Permissions on %s ", d.permissions.Dataset)
	d.layout = createModal(title, content, DialogSizeConstraints{
		Title:             title,
		ExtraContentWidth: datasetPermissionsContentWidth,
		StaticHeight:      1 + 1 + rows + 1 + datasetPermissionsDetailLines + 1 + delegationLines + datasetPermissionsShortcutLines,
	})
	d.layout.SetInputCapture(d.captureInput)
}

func datasetPermissionsShortcuts(isRoot bool) []shortcut_helper.ShortcutEntry {
	var shortcuts []shortcut_helper.ShortcutEntry
	if !isRoot {
		shortcuts = append(shortcuts,
			shortcut_helper.ShortcutEntry{KeyCombo: []string{shortcut_helper.KeySpace}, Name: "Grant/revoke"},
			shortcut_helper.ShortcutEntry{KeyCombo: []string{"a"}, Name: "Apply"},
		)
	}
	return append(shortcuts,
		shortcut_helper.ShortcutEntry{KeyCombo: []string{shortcut_helper.KeyPgUp, shortcut_helper.KeyPgDn}, Name: "Scroll delegations"},
		shortcut_helper.ShortcutEntry{KeyCombo: []string{shortcut_helper.KeyEsc}, Name: "Discard/close"},
	)
}

func (d *DatasetPermissionsDialog) captureInput(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyEscape:
		if len(d.pending) > 0 {
			// first discard the changes, so they are not lost by accident
			clear(d.pending)
			d.refresh()
			return nil
		}
		d.Close()
		return nil
	case tcell.KeyPgUp, tcell.KeyPgDn:
		// the table has the focus, the delegations shrink (and need scrolling) on small terminals
		d.scrollDelegations(event.Key() == tcell.KeyPgDn)
		return nil
	case tcell.KeyRune:
		switch event.Rune() {
		case ' ':
			d.toggleSelected()
			return nil
		case 'a':
			d.confirmChange()
			return nil
		}
	}
	return event
}

// setPermissions shows newly loaded permissions, e.g. after changing the delegations. Pending changes are discarded.
func (d *DatasetPermissionsDialog) setPermissions(permissions *zfs.DatasetPermissions) {
	d.permissions = permissions
	clear(d.pending)
	d.refresh()
}

// refresh updates all content from the permissions and the pending changes.
func (d *DatasetPermissionsDialog) refresh() {
	d.summary.SetText(d.summaryText())
	d.fillTable()
	text, _ := d.formatDelegations()
	d.delegations.SetText(text)
	row, _ := d.table.GetSelection()
	d.updateDetails(row)
}

func (d *DatasetPermissionsDialog) summaryText() string {
	if d.permissions.IsRoot {
		return "You are root and have all permissions, no delegations needed."
	}
	text := tview.Escape(fmt.Sprintf("What user %s may do on this dataset and its children:", d.permissions.User))
	if count := len(d.pending); count > 0 {
		text += txwidgets.Span(theme.Colors.Permissions.Pending, " (%d %s)", count, util.Plural(count, "change", "changes"))
	}
	return text
}

// fillTable shows the known permissions, one (selectable) row each.
func (d *DatasetPermissionsDialog) fillTable() {
	for column, text := range []string{"Permission", "", "You", "Granted by"} {
		d.table.SetCell(0, column, tview.NewTableCell(text).
			SetSelectable(false).
			SetTextColor(theme.Colors.Layout.Table.HeaderForeground).
			SetAttributes(tcell.AttrBold))
	}
	row := datasetPermissionsHeaderRows
	for _, info := range zfs.KnownPermissions {
		has := d.permissions.Has(info.Permission)
		mark, color := "✗", theme.Colors.Permissions.Missing
		if has {
			mark, color = "✓", theme.Colors.Permissions.Granted
		}
		if granted, changed := d.pending[info.Permission]; changed {
			// the state after applying
			mark, color = "→✗", theme.Colors.Permissions.Pending
			if granted {
				mark = "→✓"
			}
		}
		d.table.SetCell(row, 0, tview.NewTableCell(string(info.Permission)).SetReference(info))
		d.table.SetCell(row, 1, tview.NewTableCell(string(info.Abbreviation)).SetTextColor(color))
		d.table.SetCell(row, 2, tview.NewTableCell(mark).SetTextColor(color).SetAlign(tview.AlignCenter))
		d.table.SetCell(row, 3, tview.NewTableCell(tview.Escape(d.grantedBy(info.Permission))).SetExpansion(1))
		row++
	}
}

// me is the current user, as the grantee of the delegations changed in this dialog.
func (d *DatasetPermissionsDialog) me() zfs.Grantee {
	return zfs.Grantee{Type: zfs.WhoUser, Name: d.permissions.User}
}

// revocable returns whether the permission is delegated to the current user on this dataset itself, so revoking it
// here ("zfs unallow") has an effect. Permissions from parents, groups, everyone or permission sets are not.
func (d *DatasetPermissionsDialog) revocable(permission zfs.Permission) bool {
	return d.permissions.Delegations.DelegatedOn(d.permissions.Dataset, d.me(), zfs.ScopeLocal)[permission]
}

// otherGrants returns the delegations that grant the permission to the current user, apart from the one on this
// dataset (which revoking removes).
func (d *DatasetPermissionsDialog) otherGrants(permission zfs.Permission) []zfs.Grant {
	var result []zfs.Grant
	for _, grant := range d.permissions.Grants[permission] {
		direct := grant.Set == "" && grant.Delegation.Dataset == d.permissions.Dataset && grant.Delegation.Grantee() == d.me()
		if !direct {
			result = append(result, grant)
		}
	}
	return result
}

// toggleSelected grants the selected permission if the user does not have it, or revokes it if it is delegated to
// the user on this dataset. Toggling it again removes the pending change. Does nothing for root, and for permissions
// that are only granted elsewhere (the details tell where).
func (d *DatasetPermissionsDialog) toggleSelected() {
	row, _ := d.table.GetSelection()
	info, ok := d.table.GetCell(row, 0).GetReference().(zfs.PermissionInfo)
	if !ok || d.permissions.IsRoot {
		return
	}
	_, changed := d.pending[info.Permission]
	switch {
	case changed:
		delete(d.pending, info.Permission)
	case !d.permissions.Has(info.Permission):
		d.pending[info.Permission] = true
	case d.revocable(info.Permission):
		d.pending[info.Permission] = false
	default:
		return
	}
	d.refresh()
}

// pendingChange returns the pending changes as a change of the delegations.
func (d *DatasetPermissionsDialog) pendingChange() zfs.DelegationChange {
	change := zfs.DelegationChange{Dataset: d.permissions.Dataset, Grantee: d.me(), Scope: zfs.ScopeLocalAndDescendent}
	for _, info := range zfs.KnownPermissions {
		desired, changed := d.pending[info.Permission]
		switch {
		case changed && desired:
			change.Allow = append(change.Allow, info.Permission)
		case changed:
			change.Unallow = append(change.Unallow, info.Permission)
		}
	}
	return change
}

// confirmChange shows the commands of the pending changes and runs them on confirmation: as the current user, or if
// ZFS denies that, with sudo in the terminal. Afterwards, the permissions are loaded again.
func (d *DatasetPermissionsDialog) confirmChange() {
	change := d.pendingChange()
	if change.IsEmpty() || d.pages == nil || d.permissions.IsRoot {
		return
	}

	var reloaded *zfs.DatasetPermissions
	asyncWork := func(confirmation *SelectionDialog, action DialogActionId) error {
		if action != ApplyDelegationsDialogApplyActionId {
			return nil
		}
		explanation := fmt.Sprintf("changing the ZFS permissions of %s on %s.", change.Grantee, change.Dataset)
		err := runZfsChange(d.application, d.permissions.IsRoot, explanation, change.Commands(), func() error {
			return applyDelegationChange(change)
		})
		if err != nil {
			return err
		}
		reloaded, err = loadDatasetPermissions(change.Dataset)
		return err
	}
	onComplete := func(confirmation *SelectionDialog, option *DialogOption, err error) {
		if err != nil {
			logging.Error("Changing the permissions of %s on %s failed: %v", change.Grantee, change.Dataset, err)
			confirmation.ShowFollowUp(NewErrorDialog(d.application, "Changing Permissions Failed", err))
			return
		}
		confirmation.Close()
		d.setPermissions(reloaded)
		if d.onChanged != nil {
			d.onChanged()
		}
	}

	confirmation := NewSelectionDialog(d.application, "ApplyDelegationsDialog", " 🔑 Change Permissions ",
		d.describeChange(change),
		buildConfirmDialogOptions(ApplyDelegationsDialogApplyActionId, "Apply", true, DialogSeverityWarning),
		asyncWork, onComplete)
	ShowDialogOnPages(d.application, d.pages, confirmation, nil)
}

// describeChange lists the commands of the change and how they are run.
func (d *DatasetPermissionsDialog) describeChange(change zfs.DelegationChange) string {
	var description strings.Builder
	if len(change.Allow) > 0 {
		fmt.Fprintf(&description, "Grant %s to %s on %s and its children.\n", joinPermissionNames(change.Allow), change.Grantee, change.Dataset)
	}
	if len(change.Unallow) > 0 {
		fmt.Fprintf(&description, "Revoke %s from %s on %s and its children.\n", joinPermissionNames(change.Unallow), change.Grantee, change.Dataset)
	}
	for _, command := range change.Commands() {
		fmt.Fprintf(&description, "\n  %s", strings.Join(command, " "))
	}
	if !d.permissions.IsRoot {
		description.WriteString("\n\nIf ZFS does not allow you to change them, the commands are run with sudo (asks for your password).")
	}
	return description.String()
}

func joinPermissionNames(permissions []zfs.Permission) string {
	names := make([]string, 0, len(permissions))
	for _, permission := range permissions {
		names = append(names, string(permission))
	}
	return strings.Join(names, ", ")
}

// formatDelegations lists the delegations of the dataset and its parents, which the permissions result from.
// Returns the text and its number of lines.
func (d *DatasetPermissionsDialog) formatDelegations() (string, int) {
	lines := []string{txwidgets.Span(theme.Colors.Layout.Table.HeaderForeground, "All delegations")}
	delegations := d.permissions.Delegations
	if len(delegations.Entries) == 0 && len(delegations.Sets) == 0 {
		lines = append(lines, "none on this dataset or its parents")
	}
	dataset := ""
	for _, entry := range delegations.Entries {
		if entry.Dataset != dataset {
			dataset = entry.Dataset
			lines = append(lines, tview.Escape(d.datasetLabel(dataset)+":"))
		}
		lines = append(lines, tview.Escape(fmt.Sprintf("  %s: %s (%s)", entry.Grantee(), strings.Join(entry.Permissions, ","), entry.Scope)))
	}
	for _, set := range delegations.Sets {
		lines = append(lines, tview.Escape(fmt.Sprintf("set %s: %s (defined %s)", set.Name, strings.Join(set.Permissions, ","), d.datasetLabel(set.Dataset))))
	}
	return strings.Join(lines, "\n"), len(lines)
}

// scrollDelegations scrolls the delegations by their visible height.
func (d *DatasetPermissionsDialog) scrollDelegations(down bool) {
	_, _, _, height := d.delegations.GetInnerRect()
	row, _ := d.delegations.GetScrollOffset()
	// -1 until the text view was drawn
	row = max(row, 0)
	step := max(height-1, 1)
	if down {
		lines := strings.Count(d.delegations.GetText(false), "\n") + 1
		row = min(row+step, max(lines-height, 0))
	} else {
		row = max(row-step, 0)
	}
	d.delegations.ScrollTo(row, 0)
}

// datasetLabel names a dataset relative to the dialog's dataset.
func (d *DatasetPermissionsDialog) datasetLabel(dataset string) string {
	if dataset == d.permissions.Dataset {
		return "on this dataset"
	}
	return "on " + dataset
}

// grantedBy summarizes the delegations granting the permission, e.g. "user alice (this dataset) +1".
func (d *DatasetPermissionsDialog) grantedBy(permission zfs.Permission) string {
	if d.permissions.IsRoot {
		return "root"
	}
	grants := d.permissions.Grants[permission]
	if len(grants) == 0 {
		return ""
	}
	text := formatGrant(grants[0], d.permissions.Dataset)
	if len(grants) > 1 {
		text += fmt.Sprintf(" +%d", len(grants)-1)
	}
	return text
}

func formatGrant(grant zfs.Grant, dataset string) string {
	text := grant.Delegation.Grantee().String()
	if grant.Set != "" {
		text += " via " + grant.Set
	}
	if grant.Delegation.Dataset == dataset {
		return text + " (this dataset)"
	}
	return text + " (from " + grant.Delegation.Dataset + ")"
}

// updateDetails describes the permission in the given row, what Space does with it, and all delegations granting it.
func (d *DatasetPermissionsDialog) updateDetails(row int) {
	info, ok := d.table.GetCell(row, 0).GetReference().(zfs.PermissionInfo)
	if !ok {
		d.details.SetText("")
		return
	}
	var details strings.Builder
	fmt.Fprintf(&details, "%s: %s", info.Permission, info.Description)
	if hint := d.editHint(info.Permission); hint != "" {
		fmt.Fprintf(&details, "\n%s", hint)
	}
	for _, grant := range d.permissions.Grants[info.Permission] {
		fmt.Fprintf(&details, "\nGranted to %s, %s", formatGrant(grant, d.permissions.Dataset), grant.Delegation.Scope)
	}
	d.details.SetText(tview.Escape(details.String()))
	d.details.ScrollToBeginning()
}

// editHint tells what Space does with the permission, or why it cannot be revoked here.
func (d *DatasetPermissionsDialog) editHint(permission zfs.Permission) string {
	if d.permissions.IsRoot {
		return ""
	}
	if granted, changed := d.pending[permission]; changed {
		action := "revoked"
		if granted {
			action = "granted"
		}
		hint := fmt.Sprintf("Will be %s when applying (a), Space to undo.", action)
		if others := d.otherGrants(permission); !granted && len(others) > 0 {
			hint += fmt.Sprintf(" Still granted by %s afterwards.", formatGrant(others[0], d.permissions.Dataset))
		}
		return hint
	}
	switch {
	case !d.permissions.Has(permission):
		return "Space: grant it to you on this dataset and its children."
	case d.revocable(permission):
		return "Space: revoke it."
	default:
		return "Cannot be revoked here, change the delegation it is granted by."
	}
}
