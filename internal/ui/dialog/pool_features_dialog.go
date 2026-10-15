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
	PoolFeaturesDialogPage       util.Page = "PoolFeaturesDialog"
	EnablePoolFeaturesDialogPage util.Page = "EnablePoolFeaturesDialog"
)

// PoolFeaturesDialog lists the features of a pool, to choose the ones to enable: the ones that cannot be enabled
// (enabled already, not allowed by the property compatibility of the pool) are grayed out with the reason, and
// cannot be checked. They can be selected, so the keys reach all of them (the table scrolls with the selection), and
// the line below shows the description of the selected one. It enables nothing itself, see NewPoolFeaturesDialog.
type PoolFeaturesDialog struct {
	application   *tview.Application
	layout        *tview.Flex
	actionChannel chan DialogActionId
	features      []zfs.PoolFeature
	// checked are the features chosen, by their row. Only accessed on the UI thread.
	checked map[int]bool
	table   *tview.Table
	// detailView shows the selected feature, or why Enter did nothing
	detailView *tview.TextView
	onEnable   func(features []string)
}

// NewPoolFeaturesDialog shows the features of the pool (see zfs.ListPoolFeatures). Space or a click checks a
// feature, Enter closes the dialog and calls onEnable on the UI thread with the names of the checked ones, which
// confirms them (see NewEnablePoolFeaturesDialog): enabling cannot be undone.
func NewPoolFeaturesDialog(application *tview.Application, features *zfs.PoolFeatures, onEnable func(features []string)) *PoolFeaturesDialog {
	d := &PoolFeaturesDialog{
		application:   application,
		actionChannel: make(chan DialogActionId),
		features:      features.Features,
		checked:       map[int]bool{},
		onEnable:      onEnable,
	}
	d.createLayout(features)
	return d
}

func (d *PoolFeaturesDialog) createLayout(features *zfs.PoolFeatures) {
	title := " 🧩 Pool Features "
	description := formatPoolFeatures(features)
	plainDescription := withoutTags(description)

	d.table = util.SetSelectionStyle(tview.NewTable())
	d.table.SetSelectable(true, false)
	util.IgnoreClicksBelowRows(d.table)
	nameWidth := 0
	for _, feature := range d.features {
		nameWidth = max(nameWidth, len(feature.Name))
	}
	for row, feature := range d.features {
		available := feature.Unavailable == ""
		info := feature.Description
		color := theme.Colors.Info.Value
		if !available {
			info = feature.Unavailable
			color = theme.Colors.Dialog.Unavailable
		}
		cells := []*tview.TableCell{
			tview.NewTableCell(d.mark(row)),
			tview.NewTableCell(tview.Escape(feature.Name)).SetTextColor(color),
			tview.NewTableCell(tview.Escape(info)).SetTextColor(color).SetExpansion(1),
		}
		for column, cell := range cells {
			util.KeepColorWhenSelected(cell)
			cell.SetClickedFunc(func() bool {
				d.toggle(row)
				return false
			})
			d.table.SetCell(row, column, cell)
		}
	}
	d.table.Select(0, 0)
	d.table.SetSelectionChangedFunc(func(row, column int) { d.showDetail(row) })

	descriptionView := tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetWordWrap(true).SetText(description)
	d.detailView = tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetWordWrap(true)
	helpView := tview.NewTextView().SetDynamicColors(true).SetText(formatHelp(
		shortcut_helper.KeySpace, "check",
		shortcut_helper.KeyEnter, "enable the checked ones",
		shortcut_helper.KeyEsc, "close",
	))
	d.showDetail(0)

	constraints := DialogSizeConstraints{
		Title:       title,
		Description: plainDescription,
		// as wide as dialogs get, for the descriptions of the features
		ExtraContentWidth: 2 + nameWidth + 1 + 60,
		// spacer, the features, spacer, detail, help
		StaticHeight: 1 + len(d.features) + 1 + 3 + 1,
	}
	dialogWidth, _ := CalculateDialogSize(constraints)
	textLineWidth := max(dialogWidth-6, 5)

	// A click on the text does not focus it: the keys would no longer reach the table.
	content := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(unfocusableTextView{descriptionView}, calculateWrappedHeight(plainDescription, textLineWidth), 0, false).
		AddItem(unfocusableTextView{tview.NewTextView()}, 1, 0, false).
		AddItem(d.table, 0, 1, true).
		AddItem(unfocusableTextView{tview.NewTextView()}, 1, 0, false).
		AddItem(unfocusableTextView{d.detailView}, 3, 0, false).
		AddItem(unfocusableTextView{helpView}, 1, 0, false)
	// like the other dialogs; CalculateDialogSize leaves room for it
	content.SetBorderPadding(0, 0, 1, 1)

	var frame *tview.Flex
	d.layout, frame = createModalWithFrame(title, content, constraints)
	// what is enabled cannot be disabled again
	frame.SetBorderColor(theme.Colors.Dialog.DangerBorder)
	d.layout.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch {
		case event.Key() == tcell.KeyEscape:
			d.Close()
		case event.Key() == tcell.KeyRune && event.Rune() == ' ':
			row, _ := d.table.GetSelection()
			d.toggle(row)
		case event.Key() == tcell.KeyEnter:
			d.submit()
		default:
			return event
		}
		return nil
	})
}

// formatPoolFeatures tells what enabling a feature means, and what limits the features of the pool.
func formatPoolFeatures(features *zfs.PoolFeatures) string {
	var text strings.Builder
	fmt.Fprintf(&text, "Enable single features of the pool %s. This %s. Once a feature is in use, software "+
		"without it (an older ZFS, another operating system, a rescue system, a boot loader) may no longer import the "+
		"pool, or only read-only. ZFS also enables the features it depends on. See %s.",
		Highlight(features.Pool), txwidgets.Span(theme.Colors.Dialog.Danger, "%s", "cannot be undone"),
		Link("zpool-features(7)", zpoolFeaturesUrl))
	if features.ModuleLacksFeatures() {
		text.WriteString("\n\n" + formatModuleLacksFeatures(features))
	}
	switch {
	case features.CompatibilityErr != nil:
		fmt.Fprintf(&text, "\n\nThe pool is limited to the features of compatibility=%s, which cannot be read: %s",
			tview.Escape(features.Compatibility), tview.Escape(features.CompatibilityErr.Error()))
	case features.Compatible != nil:
		fmt.Fprintf(&text, "\n\nThe pool is limited to the features of compatibility=%s.", Highlight(features.Compatibility))
	}
	return text.String()
}

// featureUrl returns the address of the section of the feature (its short name) in zpool-features(7). The page of
// the development version has all features: a page of each version exists only from 2.1 on, and the one of a new
// version only once it is published.
func featureUrl(name string) string {
	return zpoolFeaturesUrl + "#" + name
}

// formatModuleLacksFeatures tells that the loaded kernel module does not support all features of the zfs tools, with
// their versions if known.
func formatModuleLacksFeatures(features *zfs.PoolFeatures) string {
	if features.ToolsVersion == "" || features.ModuleVersion == "" {
		return "The loaded kernel module does not support all features of the zfs tools."
	}
	return fmt.Sprintf("The loaded kernel module (%s) does not support all features of the zfs tools (%s).",
		Highlight(features.ModuleVersion), Highlight(features.ToolsVersion))
}

// formatHelp returns the keys and what they do, like the help of TextInputDialog: key, text, key, text, ...
func formatHelp(keysAndTexts ...string) string {
	var help strings.Builder
	for i := 0; i+1 < len(keysAndTexts); i += 2 {
		if i > 0 {
			help.WriteString("  ")
		}
		help.WriteString(txwidgets.Span(theme.Colors.ShortcutMap.KeyCombo, "%s", keysAndTexts[i]))
		help.WriteString(txwidgets.Span(theme.Colors.ShortcutMap.Name, ": %s", keysAndTexts[i+1]))
	}
	return help.String()
}

// mark returns the mark of the feature of the row: checked or not, or in the color of unavailable features whether
// it is enabled. Runs on the UI thread.
func (d *PoolFeaturesDialog) mark(row int) string {
	feature := d.features[row]
	if feature.Unavailable == "" {
		return switchMark(d.checked[row])
	}
	mark := switchOff
	if feature.State != zfs.PoolFeatureDisabled {
		mark = switchOn
	}
	return txwidgets.Span(theme.Colors.Dialog.Unavailable, "%s", mark)
}

// toggle checks or unchecks the feature of the row, unless it cannot be enabled. Runs on the UI thread.
func (d *PoolFeaturesDialog) toggle(row int) {
	if row < 0 || row >= len(d.features) || d.features[row].Unavailable != "" {
		return
	}
	d.checked[row] = !d.checked[row]
	d.table.GetCell(row, 0).SetText(d.mark(row))
	d.showDetail(row)
}

// showDetail shows the full description of the feature of the row, which the table may cut off, and why it cannot
// be enabled. Runs on the UI thread.
func (d *PoolFeaturesDialog) showDetail(row int) {
	if row < 0 || row >= len(d.features) {
		d.detailView.SetText("The pool has no features.")
		return
	}
	feature := d.features[row]
	compatible := "not read-only compatible"
	if feature.ReadOnlyCompatible {
		compatible = "read-only compatible"
	}
	// the name links to its section of zpool-features(7)
	name := txwidgets.ColorTag(theme.Colors.Dialog.Highlight) + Link(feature.Name, featureUrl(feature.Name)) + "[-]"
	detail := fmt.Sprintf("%s: %s (%s)", name, tview.Escape(feature.Description), compatible)
	if feature.Unsupported {
		// zpool knows nothing about it but its GUID and whether it is in use
		detail = fmt.Sprintf("%s: a feature the installed ZFS does not know, e.g. enabled by a newer version.", Highlight(feature.Name))
	}
	if feature.Unavailable != "" {
		detail += txwidgets.Span(theme.Colors.Dialog.Unavailable, " Cannot be enabled: %s.", feature.Unavailable)
	}
	d.detailView.SetText(detail)
}

// Checked returns the names of the checked features, in the order listed. Must be called on the UI thread.
func (d *PoolFeaturesDialog) Checked() []string {
	var names []string
	for row, feature := range d.features {
		if d.checked[row] {
			names = append(names, feature.Name)
		}
	}
	return names
}

// submit closes the dialog and passes the checked features on, unless none is checked. Runs on the UI thread.
func (d *PoolFeaturesDialog) submit() {
	checked := d.Checked()
	if len(checked) == 0 {
		d.detailView.SetText(txwidgets.Span(theme.Colors.Dialog.Error, "%s", "Check the features to enable with Space or a click."))
		return
	}
	d.Close()
	if d.onEnable != nil {
		d.onEnable(checked)
	}
}

func (d *PoolFeaturesDialog) GetName() string {
	return string(PoolFeaturesDialogPage)
}

func (d *PoolFeaturesDialog) GetLayout() *tview.Flex {
	return d.layout
}

func (d *PoolFeaturesDialog) GetActionChannel() <-chan DialogActionId {
	return d.actionChannel
}

func (d *PoolFeaturesDialog) Close() {
	go func() {
		d.actionChannel <- DialogCloseActionId
	}()
}

// NewEnablePoolFeaturesDialog lists the features to enable (see NewPoolFeaturesDialog), and calls onConfirm on the UI
// thread once the name of the pool was typed: it cannot be undone (see NewConfirmByNameDialog).
func NewEnablePoolFeaturesDialog(application *tview.Application, pool string, features []string, onConfirm func()) *TextInputDialog {
	return NewConfirmByNameDialog(
		application,
		string(EnablePoolFeaturesDialogPage),
		" 🧩 Enable Features ",
		formatEnablePoolFeatures(pool, features),
		pool,
		onConfirm,
	)
}

// formatEnablePoolFeatures tells which features are enabled, and that it cannot be undone.
func formatEnablePoolFeatures(pool string, features []string) string {
	var names []string
	for _, feature := range features {
		names = append(names, Highlight(feature))
	}
	return fmt.Sprintf("Enable the features %s of the pool %s (zpool set feature@…=enabled)?\n\nThis %s: features "+
		"cannot be disabled again. Software that does not support them may no longer be able to import the pool.",
		strings.Join(names, ", "), Highlight(pool), txwidgets.Span(theme.Colors.Dialog.Danger, "%s", "cannot be undone"))
}
