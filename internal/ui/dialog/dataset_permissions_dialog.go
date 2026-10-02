package dialog

import (
	"fmt"
	"strings"
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
	datasetPermissionsDetailLines = 3
	// datasetPermissionsHeaderRows are the rows of the permission table above the permissions
	datasetPermissionsHeaderRows = 1
)

// DatasetPermissionsDialog shows which ZFS permissions the current user has on a dataset, who grants them, and the
// delegations ("zfs allow") of the dataset and its parents.
type DatasetPermissionsDialog struct {
	application   *tview.Application
	permissions   *zfs.DatasetPermissions
	layout        *tview.Flex
	actionChannel chan DialogActionId

	table       *tview.Table
	details     *tview.TextView
	delegations *tview.TextView
}

func NewDatasetPermissionsDialog(application *tview.Application, permissions *zfs.DatasetPermissions) *DatasetPermissionsDialog {
	d := &DatasetPermissionsDialog{
		application:   application,
		permissions:   permissions,
		actionChannel: make(chan DialogActionId),
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

func (d *DatasetPermissionsDialog) Close() {
	emitDialogActions(d.actionChannel, DialogCloseActionId)
}

func (d *DatasetPermissionsDialog) createLayout() {
	summary := tview.NewTextView().SetText(d.summary())

	d.table = tview.NewTable().SetSelectable(true, false)
	d.table.SetSelectedStyle(tcell.StyleDefault.
		Foreground(theme.Colors.Layout.Table.SelectedForeground).
		Background(theme.Colors.Layout.Table.SelectedBackground))
	rows := d.fillTable()

	delegationsText, delegationLines := d.formatDelegations()
	d.delegations = tview.NewTextView().SetDynamicColors(true).SetText(delegationsText)
	d.table.SetSelectionChangedFunc(func(row, column int) { d.updateDetails(row) })

	d.details = tview.NewTextView().SetWrap(true).SetWordWrap(true)
	d.table.Select(datasetPermissionsHeaderRows, 0)
	d.updateDetails(datasetPermissionsHeaderRows)

	shortcuts := shortcut_helper.NewShortcutMap(d.application)
	shortcuts.SetEntries([]shortcut_helper.ShortcutEntry{
		{KeyCombo: []string{"↑", "↓"}, Name: "Select"},
		{KeyCombo: []string{"PgUp", "PgDn"}, Name: "Scroll delegations"},
		{KeyCombo: []string{"Esc"}, Name: "Close"},
	})

	content := tview.NewFlex().SetDirection(tview.FlexRow)
	content.AddItem(summary, 1, 0, false)
	content.AddItem(tview.NewBox(), 1, 0, false)
	content.AddItem(d.table, rows, 0, true)
	content.AddItem(tview.NewBox(), 1, 0, false)
	content.AddItem(d.details, datasetPermissionsDetailLines, 0, false)
	content.AddItem(tview.NewBox(), 1, 0, false)
	// shrinks (and scrolls) on small terminals
	content.AddItem(d.delegations, 0, 1, false)
	content.AddItem(shortcuts.GetLayout(), 1, 0, false)

	title := fmt.Sprintf(" 🔑 Permissions on %s ", d.permissions.Dataset)
	d.layout = createModal(title, content, DialogSizeConstraints{
		Title:             title,
		ExtraContentWidth: 76,
		StaticHeight:      1 + 1 + rows + 1 + datasetPermissionsDetailLines + 1 + delegationLines + 1,
	})
	d.layout.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			d.Close()
			return nil
		case tcell.KeyPgUp, tcell.KeyPgDn:
			// the table has the focus, the delegations shrink (and need scrolling) on small terminals
			d.scrollDelegations(event.Key() == tcell.KeyPgDn)
			return nil
		}
		return event
	})
}

func (d *DatasetPermissionsDialog) summary() string {
	if d.permissions.IsRoot {
		return "You are root and have all permissions, regardless of delegations."
	}
	return fmt.Sprintf("What user %s may do on this dataset:", d.permissions.User)
}

// fillTable shows the known permissions, one (selectable) row each. Returns the number of rows.
func (d *DatasetPermissionsDialog) fillTable() int {
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
		d.table.SetCell(row, 0, tview.NewTableCell(string(info.Permission)).SetReference(info))
		d.table.SetCell(row, 1, tview.NewTableCell(string(info.Abbreviation)).SetTextColor(color))
		d.table.SetCell(row, 2, tview.NewTableCell(mark).SetTextColor(color).SetAlign(tview.AlignCenter))
		d.table.SetCell(row, 3, tview.NewTableCell(tview.Escape(d.grantedBy(info.Permission))).SetExpansion(1))
		row++
	}
	return row
}

// formatDelegations lists the delegations of the dataset and its parents, which the permissions result from.
// Returns the text and its number of lines.
func (d *DatasetPermissionsDialog) formatDelegations() (string, int) {
	lines := []string{txwidgets.Span(theme.Colors.Layout.Table.HeaderForeground, "Delegations")}
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
		lines = append(lines, tview.Escape(fmt.Sprintf("  %s: %s (%s)", entry.Principal(), strings.Join(entry.Permissions, ","), entry.Scope)))
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
	text := grant.Delegation.Principal()
	if grant.Set != "" {
		text += " via " + grant.Set
	}
	if grant.Delegation.Dataset == dataset {
		return text + " (this dataset)"
	}
	return text + " (from " + grant.Delegation.Dataset + ")"
}

// updateDetails describes the permission in the given row and all delegations granting it.
func (d *DatasetPermissionsDialog) updateDetails(row int) {
	info, ok := d.table.GetCell(row, 0).GetReference().(zfs.PermissionInfo)
	if !ok {
		d.details.SetText("")
		return
	}
	var details strings.Builder
	fmt.Fprintf(&details, "%s: %s", info.Permission, info.Description)
	for _, grant := range d.permissions.Grants[info.Permission] {
		fmt.Fprintf(&details, "\nGranted to %s, %s", formatGrant(grant, d.permissions.Dataset), grant.Delegation.Scope)
	}
	d.details.SetText(tview.Escape(details.String()))
	d.details.ScrollToBeginning()
}
