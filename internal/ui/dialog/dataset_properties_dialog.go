package dialog

import (
	"cmp"
	"fmt"
	"sort"
	"strings"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	"zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	DatasetPropertiesDialogPage util.Page = "DatasetPropertiesDialog"

	// datasetPropertiesDetailLines is the height of the details of the selected property
	datasetPropertiesDetailLines = 2
	// datasetPropertiesShortcutLines is the height of the shortcut map (see TestDatasetPropertiesDialog_ShortcutsFit)
	datasetPropertiesShortcutLines = 2

	InheritPropertyDialogInheritActionId DialogActionId = iota
)

// Replaceable in tests, so tests never run zfs or sudo.
var (
	listProperties  = zfs.ListProperties
	setProperty     = zfs.SetProperty
	inheritProperty = zfs.InheritProperty
)

var (
	propertyColumnName   = &table.Column{Id: 0, Key: "name", Title: "Property", Alignment: tview.AlignLeft}
	propertyColumnValue  = &table.Column{Id: 1, Key: "value", Title: "Value", Alignment: tview.AlignLeft}
	propertyColumnSource = &table.Column{Id: 2, Key: "source", Title: "Source", Alignment: tview.AlignLeft}
	propertyColumns      = []*table.Column{propertyColumnName, propertyColumnValue, propertyColumnSource}
)

// DatasetPropertiesDialog shows all ZFS properties of a dataset ("zfs get all"), and lets the user change them
// ("zfs set"), add user properties ("module:property"), and reset local values to the inherited or default ones
// ("zfs inherit", which removes user properties).
type DatasetPropertiesDialog struct {
	application   *tview.Application
	dataset       string
	isRoot        bool
	layout        *tview.Flex
	actionChannel chan DialogActionId
	// pages the dialog is shown on, set by ShowDialogOnPages. Used for the dialogs opened from this one.
	pages *tview.Pages
	// onChanged is called on the UI thread after a property was changed, may be nil
	onChanged func()

	table   *table.RowSelectionTable[zfs.Property]
	details *tview.TextView
	// selectAfterReload is the name of the property to select after the next reload (e.g. an added one)
	selectAfterReload string
}

// NewDatasetPropertiesDialog creates the dialog for the given properties of the dataset. isRoot is whether the
// application runs as root (changes are then never run with sudo). onChanged is called on the UI thread after a
// property was changed, it may be nil.
func NewDatasetPropertiesDialog(application *tview.Application, dataset string, properties []*zfs.Property, isRoot bool, onChanged func()) *DatasetPropertiesDialog {
	d := &DatasetPropertiesDialog{
		application:   application,
		dataset:       dataset,
		isRoot:        isRoot,
		actionChannel: make(chan DialogActionId),
		onChanged:     onChanged,
	}
	d.createLayout(properties)
	return d
}

func (d *DatasetPropertiesDialog) GetName() string {
	return string(DatasetPropertiesDialogPage)
}

func (d *DatasetPropertiesDialog) GetLayout() *tview.Flex {
	return d.layout
}

func (d *DatasetPropertiesDialog) GetActionChannel() <-chan DialogActionId {
	return d.actionChannel
}

func (d *DatasetPropertiesDialog) setPages(pages *tview.Pages) {
	d.pages = pages
}

func (d *DatasetPropertiesDialog) Close() {
	emitDialogActions(d.actionChannel, DialogCloseActionId)
}

func (d *DatasetPropertiesDialog) createLayout(properties []*zfs.Property) {
	d.table = table.NewTableContainer[zfs.Property](d.application, toPropertyCells, sortProperties)
	d.table.SetColumnSpec(propertyColumns, propertyColumnName, false)
	d.table.SetActiveColumns(propertyColumns)
	d.table.SetFilterFunc(func(property *zfs.Property, filterText string) bool {
		return table.MatchesGlob(property.Name, filterText) || table.MatchesGlob(property.Value, filterText)
	})
	d.table.SetFilterChangedCallback(d.updateFooter)
	d.table.SetSelectionChangedCallback(func(*zfs.Property) { d.updateDetails() })
	d.table.SetInputCapture(d.captureTableInput)

	d.details = tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetWordWrap(true)

	shortcuts := shortcut_helper.NewShortcutMap(d.application)
	shortcuts.SetEntries(datasetPropertiesShortcuts())

	content := tview.NewFlex().SetDirection(tview.FlexRow)
	content.AddItem(d.table.GetLayout(), 0, 1, true)
	content.AddItem(d.details, datasetPropertiesDetailLines, 0, false)
	content.AddItem(shortcuts.GetLayout(), datasetPropertiesShortcutLines, 0, false)

	title := fmt.Sprintf(" ⚙ Properties of %s ", d.dataset)
	var frame *tview.Flex
	d.layout, frame = createModalWithFrame(title, content, DialogSizeConstraints{
		Title:             title,
		ExtraContentWidth: 76,
		// the table header, all properties, details and shortcuts; clamped to the terminal height
		StaticHeight: 1 + len(properties) + datasetPropertiesDetailLines + datasetPropertiesShortcutLines,
	})
	// one border around everything, with the counts and the filter of the table in it
	d.table.EmbedInFrame(frame.Box)
	d.layout.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// while typing a filter, Esc ends that (in the table)
		if event.Key() == tcell.KeyEscape && !d.table.IsEditingFilter() {
			d.Close()
			return nil
		}
		return event
	})

	d.setProperties(properties)
	d.table.SelectFirstIfExists()
	d.updateDetails()
}

func datasetPropertiesShortcuts() []shortcut_helper.ShortcutEntry {
	return []shortcut_helper.ShortcutEntry{
		{KeyCombo: []string{"Enter"}, Name: "Edit"},
		{KeyCombo: []string{"a"}, Name: "Add"},
		{KeyCombo: []string{"i"}, Name: "Inherit"},
		util.TableComponentShortcutFilter,
		{KeyCombo: []string{"←", "→"}, Name: "Scroll"},
		{KeyCombo: []string{"Esc"}, Name: "Close"},
	}
}

// captureTableInput handles the keys on the table, after the filter input (see table.RowSelectionTable).
func (d *DatasetPropertiesDialog) captureTableInput(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyRune && event.Rune() == 'a' {
		// also on the header row
		d.addProperty()
		return nil
	}
	property := d.table.GetSelectedEntry()
	if property == nil {
		// e.g. Enter on the header row changes the sort direction
		return event
	}
	switch {
	case event.Key() == tcell.KeyEnter:
		d.editProperty(property)
		return nil
	case event.Key() == tcell.KeyRune && event.Rune() == 'i':
		d.inheritProperty(property)
		return nil
	}
	return event
}

// setProperties shows the given properties, keeping the selected one. Must be called on the UI thread.
func (d *DatasetPropertiesDialog) setProperties(properties []*zfs.Property) {
	selectedName := ""
	if selected := d.table.GetSelectedEntry(); selected != nil {
		selectedName = selected.Name
	}
	if d.selectAfterReload != "" {
		selectedName = d.selectAfterReload
		d.selectAfterReload = ""
	}
	d.table.SetData(properties)
	for _, property := range d.table.GetEntries() {
		if property.Name == selectedName {
			d.table.Select(property)
		}
	}
	d.updateFooter()
	d.updateDetails()
}

func (d *DatasetPropertiesDialog) updateFooter() {
	all := len(d.table.GetAllEntries())
	if d.table.IsFilterActive() {
		d.table.SetFooter(fmt.Sprintf("%d of %d properties", len(d.table.GetEntries()), all))
	} else {
		d.table.SetFooter(fmt.Sprintf("%d properties", all))
	}
}

// updateDetails shows the full value of the selected property, and what can be done with it.
func (d *DatasetPropertiesDialog) updateDetails() {
	property := d.table.GetSelectedEntry()
	if property == nil {
		d.details.SetText("")
		return
	}
	var hint string
	switch {
	case !property.IsEditable():
		hint = "read-only (a statistic, or fixed when the dataset was created)"
	case property.IsLocal() && property.IsUserProperty():
		hint = "Enter: change, i: remove"
	case property.IsLocal():
		hint = "Enter: change, i: reset to the inherited/default value"
	default:
		hint = "Enter: change"
	}
	d.details.SetText(tview.Escape(fmt.Sprintf("%s = %s (%s)", property.Name, property.Value, property.Source)) +
		"\n" + txwidgets.Span(theme.Colors.Properties.Hint, "%s", hint))
}

// toPropertyCells shows a property. Read-only properties are dimmed, local values highlighted.
func toPropertyCells(row int, columns []*table.Column, property *zfs.Property) []*tview.TableCell {
	color := theme.Colors.Properties.Value
	switch {
	case !property.IsEditable():
		color = theme.Colors.Properties.ReadOnly
	case property.IsLocal():
		color = theme.Colors.Properties.Local
	}
	var cells []*tview.TableCell
	for _, column := range columns {
		var text string
		switch column {
		case propertyColumnName:
			text = property.Name
		case propertyColumnValue:
			text = property.Value
		case propertyColumnSource:
			text = property.Source
		}
		cells = append(cells, tview.NewTableCell(tview.Escape(text)).SetTextColor(color))
	}
	return cells
}

func sortProperties(properties []*zfs.Property, column *table.Column, inverted bool) []*zfs.Property {
	sort.SliceStable(properties, func(i, j int) bool {
		a, b := properties[i], properties[j]
		result := 0
		switch column {
		case propertyColumnValue:
			result = strings.Compare(a.Value, b.Value)
		case propertyColumnSource:
			result = strings.Compare(a.Source, b.Source)
		}
		if result == 0 {
			result = cmp.Compare(a.Name, b.Name)
		}
		if inverted {
			result = -result
		}
		return result < 0
	})
	return properties
}

// editProperty asks for the new value and sets it. Must be called on the UI thread.
func (d *DatasetPropertiesDialog) editProperty(property *zfs.Property) {
	if !property.IsEditable() || d.pages == nil {
		return
	}
	name := property.Name
	description := fmt.Sprintf("New value of %s (currently %s, %s). Runs: zfs set %s=<value> %s",
		name, property.Value, property.Source, name, d.dataset)
	input := NewTextInputDialog(d.application, "EditPropertyDialog", fmt.Sprintf(" Edit %s ", name), description, property.Value,
		func(value string) {
			if value == property.Value {
				return
			}
			d.change(fmt.Sprintf("setting %s=%s on %s.", name, value, d.dataset),
				[][]string{zfs.SetPropertyCommand(d.dataset, name, value)},
				func() error { return setProperty(d.dataset, name, value) })
		})
	ShowDialogOnPages(d.application, d.pages, input, nil)
}

// findProperty returns the property with the given name, or nil.
func (d *DatasetPropertiesDialog) findProperty(name string) *zfs.Property {
	for _, property := range d.table.GetAllEntries() {
		if property.Name == name {
			return property
		}
	}
	return nil
}

// addProperty asks for the name of a new user property ("module:property") and then for its value. Native
// properties always exist, so only user properties can be added. Must be called on the UI thread.
func (d *DatasetPropertiesDialog) addProperty() {
	if d.pages == nil {
		return
	}
	nameInput := NewTextInputDialog(d.application, "AddPropertyNameDialog", " Add User Property ",
		"Name of the new user property, in the form module:property, e.g. org.example:note. "+
			"User properties are free-form metadata, also used by tools (e.g. com.sun:auto-snapshot).",
		"", d.askForNewPropertyValue)
	nameInput.SetValidator(func(name string) error {
		if existing := d.findProperty(name); existing != nil && !existing.IsUserProperty() {
			return fmt.Errorf("%s is a native property, change it with Enter in the list", name)
		}
		return zfs.ValidateUserPropertyName(name)
	})
	ShowDialogOnPages(d.application, d.pages, nameInput, nil)
}

// askForNewPropertyValue asks for the value of the new user property and sets it. If the property exists already,
// it is edited instead. Runs on the UI thread.
func (d *DatasetPropertiesDialog) askForNewPropertyValue(name string) {
	if existing := d.findProperty(name); existing != nil {
		d.editProperty(existing)
		return
	}
	description := fmt.Sprintf("Value of the new user property %s. Runs: zfs set %s=<value> %s", name, name, d.dataset)
	valueInput := NewTextInputDialog(d.application, "AddPropertyValueDialog", fmt.Sprintf(" Add %s ", name), description, "",
		func(value string) {
			d.selectAfterReload = name
			d.change(fmt.Sprintf("setting %s=%s on %s.", name, value, d.dataset),
				[][]string{zfs.SetPropertyCommand(d.dataset, name, value)},
				func() error { return setProperty(d.dataset, name, value) })
		})
	ShowDialogOnPages(d.application, d.pages, valueInput, nil)
}

// inheritProperty asks for confirmation and resets the local value of the property. Must be called on the UI thread.
func (d *DatasetPropertiesDialog) inheritProperty(property *zfs.Property) {
	if !property.IsLocal() || d.pages == nil {
		return
	}
	name := property.Name
	command := zfs.InheritPropertyCommand(d.dataset, name)
	onComplete := func(confirmation *SelectionDialog, option *DialogOption, err error) {
		confirmation.Close()
		if option.Id == InheritPropertyDialogInheritActionId {
			d.change(fmt.Sprintf("resetting %s on %s.", name, d.dataset), [][]string{command},
				func() error { return inheritProperty(d.dataset, name) })
		}
	}
	description := fmt.Sprintf("Remove the local value of %s (%s), so the value inherited from the parent dataset "+
		"(or the default) applies?\n\n  %s", name, property.Value, strings.Join(command, " "))
	if property.IsUserProperty() {
		description = fmt.Sprintf("Remove the user property %s (%s)? If a parent dataset sets it, its value applies "+
			"instead.\n\n  %s", name, property.Value, strings.Join(command, " "))
	}
	confirmation := NewSelectionDialog(d.application, "InheritPropertyDialog", " Reset Property ", description,
		buildConfirmDialogOptions(InheritPropertyDialogInheritActionId, "Reset", true, DialogSeverityWarning),
		nil, onComplete)
	ShowDialogOnPages(d.application, d.pages, confirmation, nil)
}

// change runs a change in the background (with sudo, if ZFS denies it to the user), then reloads the properties.
// Must be called on the UI thread.
func (d *DatasetPropertiesDialog) change(explanation string, commands [][]string, direct func() error) {
	d.details.SetText(txwidgets.Span(theme.Colors.Properties.Hint, "Applying: %s", strings.Join(commands[0], " ")))
	go func() {
		err := runZfsChange(d.application, d.isRoot, explanation, commands, direct)
		var properties []*zfs.Property
		if err == nil {
			properties, err = listProperties(d.dataset)
		}
		d.application.QueueUpdateDraw(func() {
			if err != nil {
				logging.Error("Changing a property of %s failed: %v", d.dataset, err)
				d.selectAfterReload = ""
				d.updateDetails()
				ShowDialogOnPages(d.application, d.pages, NewErrorDialog(d.application, "Changing Property Failed", err), nil)
				return
			}
			d.setProperties(properties)
			if d.onChanged != nil {
				d.onChanged()
			}
		})
	}()
}
