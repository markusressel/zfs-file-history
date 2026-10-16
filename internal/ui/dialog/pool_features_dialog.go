package dialog

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"zfs-file-history/internal/state"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	"zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// stateKeyPoolFeaturesTable is where the column layout of the features is saved in state.Current
const stateKeyPoolFeaturesTable = "poolFeatures"

const (
	PoolFeaturesDialogPage       util.Page = "PoolFeaturesDialog"
	EnablePoolFeaturesDialogPage util.Page = "EnablePoolFeaturesDialog"

	// poolFeaturesDetailLines is the height of the details of the selected feature
	poolFeaturesDetailLines = 3
	// poolFeaturesShortcutLines is the height of the shortcut map (see TestPoolFeaturesDialog_ShortcutsFit)
	poolFeaturesShortcutLines = 2
	// poolFeaturesContentWidth is the width of the content, as wide as dialogs get, for the descriptions
	poolFeaturesContentWidth = 76

	// groupingFeatureAvailability groups the features by whether they can be enabled, and why not
	groupingFeatureAvailability = "availability"
)

var (
	featureColumnEnable      = &table.Column{Id: 0, Key: "enable", Title: "Enable", Alignment: tview.AlignCenter}
	featureColumnName        = &table.Column{Id: 1, Key: "name", Title: "Feature", Alignment: tview.AlignLeft}
	featureColumnState       = &table.Column{Id: 2, Key: "state", Title: "State", Alignment: tview.AlignLeft}
	featureColumnReadOnly    = &table.Column{Id: 3, Key: "readonly", Title: "Read-only compatible", Alignment: tview.AlignLeft}
	featureColumnReason      = &table.Column{Id: 4, Key: "reason", Title: "Why not", Alignment: tview.AlignLeft}
	featureColumnDescription = &table.Column{Id: 5, Key: "description", Title: "Description", Alignment: tview.AlignLeft}
	featureColumns           = []*table.Column{featureColumnEnable, featureColumnName, featureColumnState, featureColumnReadOnly,
		featureColumnReason, featureColumnDescription}
	// featureDefaultColumns are shown at first: the reason is the title of the group (see featureGroup)
	featureDefaultColumns = []*table.Column{featureColumnEnable, featureColumnName, featureColumnDescription}
)

// PoolFeaturesDialog lists the features of a pool, to choose the ones to enable, in a table like the other lists
// (sorted by any column, columns chosen with F2, a filter, grouped with b). The ones that cannot be enabled (enabled
// already, not allowed by the property compatibility of the pool, ...) are grayed out, grouped by the reason, and
// cannot be checked. They can be selected, so the line below shows the description of each. It enables nothing
// itself, see NewPoolFeaturesDialog.
type PoolFeaturesDialog struct {
	application   *tview.Application
	layout        *tview.Flex
	actionChannel chan DialogActionId
	// pages the dialog is shown on, set by ShowDialogOnPages. Used for the column dialog.
	pages    *tview.Pages
	features []*zfs.PoolFeature
	// checked are the names of the features chosen. Only accessed on the UI thread.
	checked map[string]bool
	table   *table.RowSelectionTable[zfs.PoolFeature]
	// detailView shows the selected feature, or why Enter did nothing
	detailView *tview.TextView
	onEnable   func(features []string)
}

// NewPoolFeaturesDialog shows the features of the pool (see zfs.ListPoolFeatures). Space or a click on the
// highlighted one checks a feature, Enter closes the dialog and calls onEnable on the UI thread with the names of the
// checked ones, which confirms them (see NewEnablePoolFeaturesDialog): enabling cannot be undone.
func NewPoolFeaturesDialog(application *tview.Application, features *zfs.PoolFeatures, onEnable func(features []string)) *PoolFeaturesDialog {
	d := &PoolFeaturesDialog{
		application:   application,
		actionChannel: make(chan DialogActionId),
		checked:       map[string]bool{},
		onEnable:      onEnable,
	}
	for i := range features.Features {
		d.features = append(d.features, &features.Features[i])
	}
	d.createLayout(features)
	return d
}

func (d *PoolFeaturesDialog) createLayout(features *zfs.PoolFeatures) {
	title := " 🧩 Pool Features "
	description := formatPoolFeatures(features)
	plainDescription := withoutTags(description)

	d.table = table.NewTableContainer[zfs.PoolFeature](d.application, d.toCells, d.sortFeatures)
	d.table.SetColumnSpec(featureColumns, featureColumnName, false)
	d.table.SetActiveColumns(featureDefaultColumns)
	d.table.SetGroupings([]table.Grouping[zfs.PoolFeature]{
		{Key: groupingFeatureAvailability, Name: "availability", GroupOf: featureGroup},
	})
	d.table.SetGrouping(groupingFeatureAvailability)
	d.table.BindColumnLayout(state.Current, stateKeyPoolFeaturesTable, featureColumns)
	d.table.SetFilterFunc(func(feature *zfs.PoolFeature, filterText string) bool {
		return table.MatchesGlob(feature.Name, filterText) || table.MatchesGlob(feature.Description, filterText)
	})
	d.table.SetFilterChangedCallback(d.updateFooter)
	d.table.SetSelectionChangedCallback(d.showDetail)
	d.table.SetInputCapture(d.captureTableInput)
	// a click checks only the highlighted feature, another one is selected first
	d.table.SetClickOnSelectedFunc(d.toggle)
	d.table.SetPlaceholder("The pool has no features.", theme.Colors.Dialog.Unavailable)

	descriptionView := tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetWordWrap(true).SetText(description)
	d.detailView = tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetWordWrap(true)
	shortcuts := shortcut_helper.NewShortcutMap(d.application)
	shortcuts.SetEntries(poolFeaturesShortcuts())

	constraints := DialogSizeConstraints{
		Title:             title,
		Description:       plainDescription,
		ExtraContentWidth: poolFeaturesContentWidth,
		// spacer, the header, the features and the titles of their groups, detail, shortcuts; clamped to the
		// terminal height
		StaticHeight: 1 + 1 + len(d.features) + featureGroupCount(d.features) + poolFeaturesDetailLines + poolFeaturesShortcutLines,
	}
	dialogWidth, _ := CalculateDialogSize(constraints)
	textLineWidth := max(dialogWidth-6, 5)

	// A click on the text does not focus it: the keys would no longer reach the table.
	content := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(unfocusableTextView{descriptionView}, calculateWrappedHeight(plainDescription, textLineWidth), 0, false).
		AddItem(unfocusableTextView{tview.NewTextView()}, 1, 0, false).
		AddItem(d.table.GetLayout(), 0, 1, true).
		AddItem(unfocusableTextView{d.detailView}, poolFeaturesDetailLines, 0, false).
		AddItem(shortcuts.GetLayout(), poolFeaturesShortcutLines, 0, false)
	// like the other dialogs; CalculateDialogSize leaves room for it
	content.SetBorderPadding(0, 0, 1, 1)

	var frame *tview.Flex
	d.layout, frame = createModalWithFrame(title, content, constraints)
	// what is enabled cannot be disabled again
	frame.SetBorderColor(theme.Colors.Dialog.DangerBorder)
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

	d.table.SetData(d.features)
	d.table.SelectFirstIfExists()
	d.updateFooter()
	d.showDetail(d.table.GetSelectedEntry())
}

func poolFeaturesShortcuts() []shortcut_helper.ShortcutEntry {
	return []shortcut_helper.ShortcutEntry{
		{KeyCombo: []string{shortcut_helper.KeySpace}, Name: "Check"},
		{KeyCombo: []string{shortcut_helper.KeyEnter}, Name: "Enable the checked"},
		util.TableComponentShortcutFilter,
		util.TableComponentShortcutColumns,
		util.TableComponentShortcutGroup,
		{KeyCombo: []string{shortcut_helper.KeyEsc}, Name: "Close", Group: shortcut_helper.GroupGlobal},
	}
}

// captureTableInput handles the keys on the table, after the filter input (see table.RowSelectionTable): on the
// header row, Enter changes the sort direction.
func (d *PoolFeaturesDialog) captureTableInput(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyF2 {
		ShowColumnDialogOnPages(d.application, d.pages, "Configure Feature Columns", featureColumns, d.table)
		return nil
	}
	feature := d.table.GetSelectedEntry()
	if feature == nil {
		return event
	}
	switch {
	case event.Key() == tcell.KeyRune && event.Rune() == ' ':
		d.toggle(feature)
		return nil
	case event.Key() == tcell.KeyEnter:
		d.submit()
		return nil
	}
	return event
}

// featureGroup returns the group of the feature: whether it can be enabled, or why not.
func featureGroup(feature *zfs.PoolFeature) table.Group {
	if feature.Unavailable == "" {
		return table.Group{Title: "Can be enabled"}
	}
	order := 1
	switch {
	case feature.Unsupported:
		order = 4
	case feature.State == zfs.PoolFeatureActive:
		order = 3
	case feature.State == zfs.PoolFeatureEnabled:
		order = 2
	}
	return table.Group{Title: upperFirst(feature.Unavailable), Order: order}
}

// featureGroupCount returns the number of groups of the features, for the height of the dialog.
func featureGroupCount(features []*zfs.PoolFeature) int {
	groups := map[table.Group]bool{}
	for _, feature := range features {
		groups[featureGroup(feature)] = true
	}
	return len(groups)
}

// upperFirst returns the text with its first letter in upper case, e.g. a reason as the title of a group.
func upperFirst(text string) string {
	if text == "" {
		return text
	}
	return strings.ToUpper(text[:1]) + text[1:]
}

// toCells shows a feature: the ones that cannot be enabled grayed out. Runs on the UI thread.
func (d *PoolFeaturesDialog) toCells(row int, columns []*table.Column, feature *zfs.PoolFeature) []*tview.TableCell {
	color := theme.Colors.Info.Value
	if feature.Unavailable != "" {
		color = theme.Colors.Dialog.Unavailable
	}
	var cells []*tview.TableCell
	for _, column := range columns {
		var text string
		switch column {
		case featureColumnEnable:
			// the mark has its own color, see mark
			cells = append(cells, tview.NewTableCell(d.mark(feature)).SetAlign(tview.AlignCenter))
			continue
		case featureColumnName:
			text = feature.Name
		case featureColumnState:
			text = string(feature.State)
		case featureColumnReadOnly:
			text = formatReadOnlyCompatible(feature)
		case featureColumnReason:
			text = feature.Unavailable
		case featureColumnDescription:
			text = featureDescription(feature)
		}
		cells = append(cells, tview.NewTableCell(tview.Escape(text)).SetTextColor(color))
	}
	return cells
}

// featureDescription returns the description of the feature, or what is known about one the installed ZFS does not
// know.
func featureDescription(feature *zfs.PoolFeature) string {
	if feature.Unsupported {
		return "unknown to the installed ZFS"
	}
	return feature.Description
}

// formatReadOnlyCompatible returns whether software without the feature can still import the pool read-only: empty
// if not known (a feature the installed ZFS does not know, which is not in use).
func formatReadOnlyCompatible(feature *zfs.PoolFeature) string {
	switch {
	case feature.ReadOnlyCompatible:
		return "yes"
	case feature.Unsupported:
		return ""
	}
	return "no"
}

// sortFeatures sorts the features by the column, then by their name. Enable sorts the checked ones first, then the
// ones that can be enabled.
func (d *PoolFeaturesDialog) sortFeatures(features []*zfs.PoolFeature, column *table.Column, inverted bool) []*zfs.PoolFeature {
	slices.SortStableFunc(features, func(a, b *zfs.PoolFeature) int {
		result := 0
		switch column {
		case featureColumnEnable:
			result = cmp.Compare(d.checkRank(a), d.checkRank(b))
		case featureColumnState:
			result = strings.Compare(string(a.State), string(b.State))
		case featureColumnReadOnly:
			result = strings.Compare(formatReadOnlyCompatible(a), formatReadOnlyCompatible(b))
		case featureColumnReason:
			result = strings.Compare(a.Unavailable, b.Unavailable)
		case featureColumnDescription:
			result = strings.Compare(featureDescription(a), featureDescription(b))
		}
		if result == 0 {
			result = strings.Compare(a.Name, b.Name)
		}
		if inverted {
			result = -result
		}
		return result
	})
	return features
}

// checkRank orders the features by the column Enable: checked, can be enabled, enabled, cannot be enabled.
func (d *PoolFeaturesDialog) checkRank(feature *zfs.PoolFeature) int {
	switch {
	case d.checked[feature.Name]:
		return 0
	case feature.Unavailable == "":
		return 1
	case feature.State != zfs.PoolFeatureDisabled:
		return 2
	}
	return 3
}

// updateFooter shows the number of features in the border, and how many are checked.
func (d *PoolFeaturesDialog) updateFooter() {
	all := len(d.table.GetAllEntries())
	footer := fmt.Sprintf("%d features", all)
	if d.table.IsFilterActive() {
		footer = fmt.Sprintf("%d of %d features", len(d.table.GetEntries()), all)
	}
	if checked := len(d.Checked()); checked > 0 {
		footer += fmt.Sprintf(", %d checked", checked)
	}
	d.table.SetFooter(footer)
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

// mark returns the mark of the feature: checked or not, or in the color of unavailable features whether it is
// enabled. Runs on the UI thread.
func (d *PoolFeaturesDialog) mark(feature *zfs.PoolFeature) string {
	if feature.Unavailable == "" {
		return switchMark(d.checked[feature.Name])
	}
	mark := switchOff
	if feature.State != zfs.PoolFeatureDisabled {
		mark = switchOn
	}
	return txwidgets.Span(theme.Colors.Dialog.Unavailable, "%s", mark)
}

// toggle checks or unchecks the feature, unless it cannot be enabled. Runs on the UI thread.
func (d *PoolFeaturesDialog) toggle(feature *zfs.PoolFeature) {
	if feature == nil || feature.Unavailable != "" {
		return
	}
	d.checked[feature.Name] = !d.checked[feature.Name]
	d.table.UpdateEntry(feature)
	d.updateFooter()
	d.showDetail(feature)
}

// showDetail shows the full description of the feature, which the table may cut off, and why it cannot be enabled.
// Runs on the UI thread.
func (d *PoolFeaturesDialog) showDetail(feature *zfs.PoolFeature) {
	if feature == nil {
		d.detailView.SetText("")
		return
	}
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

// Checked returns the names of the checked features, in the order zpool lists them. Must be called on the UI thread.
func (d *PoolFeaturesDialog) Checked() []string {
	var names []string
	for _, feature := range d.features {
		if d.checked[feature.Name] {
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

func (d *PoolFeaturesDialog) SetPages(pages *tview.Pages) {
	d.pages = pages
}

func (d *PoolFeaturesDialog) Close() {
	emitDialogActions(d.actionChannel, DialogCloseActionId)
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
