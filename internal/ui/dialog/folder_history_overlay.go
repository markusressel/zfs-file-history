package dialog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/state"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	FolderHistoryOverlayPage uiutil.Page = "FolderHistoryOverlay"

	// folderHistorySparklineLines: items, size, and the marker of the selected snapshot
	folderHistorySparklineLines = 3
	// folderHistoryHeaderLines is the height above both tables: the mode and the sparklines on the left, the details
	// of the selected change on the right, so the tables line up
	folderHistoryHeaderLines = 1 + folderHistorySparklineLines
)

// Replaceable in tests, so tests never access ZFS.
var findSnapshotsOfPath = snapshotsOfPath

var (
	timelineColumnSnapshot = &table.Column{Id: 0, Key: "snapshot", Title: "Snapshot", Alignment: tview.AlignLeft}
	timelineColumnChanges  = &table.Column{Id: 1, Key: "changes", Title: "Changes", Alignment: tview.AlignLeft}
	timelineColumnItems    = &table.Column{Id: 2, Key: "items", Title: "Items", Alignment: tview.AlignRight}
	timelineColumnDate     = &table.Column{Id: 3, Key: "creation", Title: "Creation Date", Alignment: tview.AlignLeft}
	timelineColumns        = []*table.Column{timelineColumnSnapshot, timelineColumnChanges, timelineColumnItems, timelineColumnDate}

	changeColumnKind     = &table.Column{Id: 0, Key: "kind", Title: "±", Alignment: tview.AlignCenter}
	changeColumnName     = &table.Column{Id: 1, Key: "name", Title: "Name", Alignment: tview.AlignLeft}
	changeColumnSize     = &table.Column{Id: 2, Key: "size", Title: "Size", Alignment: tview.AlignRight}
	changeColumnModified = &table.Column{Id: 3, Key: "modified", Title: "Modified", Alignment: tview.AlignLeft}
	changeColumns        = []*table.Column{changeColumnKind, changeColumnName, changeColumnSize, changeColumnModified}
)

// FolderHistoryOverlay shows how the content of a folder changed across the snapshots of its dataset: a timeline of
// the snapshots in which its direct entries changed, sparklines of its size and number of items, and the changed
// entries of the selected snapshot (compared to the previous snapshot, or to the working copy). Entries and the
// whole folder can be restored from the selected snapshot, and the history of an entry can be opened.
type FolderHistoryOverlay struct {
	application   *tview.Application
	folderPath    string
	cachedEntries []*data.SnapshotBrowserEntry
	layout        *tview.Flex
	actionChannel chan DialogActionId
	cancelScan    context.CancelFunc

	// UI widgets, only accessed on the UI thread
	pages       *tview.Pages
	loadingView *uiutil.LoadingView
	modeView    *tview.TextView
	sparklines  *tview.Box
	timeline    *table.RowSelectionTable[folderVersion]
	changes     *table.RowSelectionTable[folderChange]
	details     *tview.TextView
	shortcuts   *shortcut_helper.ShortcutMapComponent
	// split separates the timeline and the changes, its boundary can be dragged with the mouse
	split *uiutil.ResizableSplit

	// state, only accessed on the UI thread
	history  *folderHistory
	selected *folderVersion
	mode     diffMode
	// initialSnapshot is the snapshot whose version is selected once the history is loaded (nil: the newest),
	// see SelectSnapshot
	initialSnapshot *zfs.Snapshot
}

// NewFolderHistoryOverlay creates the overlay for the folder at folderPath and starts scanning its history in the
// background. cachedEntries are the snapshots known already (e.g. of the snapshot browser), used if they belong to
// the dataset of the folder. Must be called on the UI thread.
func NewFolderHistoryOverlay(application *tview.Application, folderPath string, cachedEntries []*data.SnapshotBrowserEntry) *FolderHistoryOverlay {
	o := &FolderHistoryOverlay{
		application:   application,
		folderPath:    folderPath,
		cachedEntries: cachedEntries,
		actionChannel: make(chan DialogActionId, 1),
		mode:          loadDiffMode(toggleFolderHistoryCompareNow, diffModePredecessor, diffModeWorkingCopy),
	}
	o.createLayout()
	o.loadingView.Start()
	o.scan()
	return o
}

func (o *FolderHistoryOverlay) GetName() string {
	return string(FolderHistoryOverlayPage)
}

func (o *FolderHistoryOverlay) GetLayout() *tview.Flex {
	return o.layout
}

func (o *FolderHistoryOverlay) GetActionChannel() <-chan DialogActionId {
	return o.actionChannel
}

func (o *FolderHistoryOverlay) Close() {
	if o.cancelScan != nil {
		o.cancelScan()
	}
	o.loadingView.Stop()
	select {
	case o.actionChannel <- DialogCloseActionId:
	default:
	}
}

// captureMouse lets the boundary between the timeline and the changes be dragged (see ShowDialogOnPages).
func (o *FolderHistoryOverlay) captureMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	return o.split.MouseCapture(action, event)
}

// isMainPageInFront returns whether the history is shown, not the loading view or a dialog above it.
func (o *FolderHistoryOverlay) isMainPageInFront() bool {
	front, _ := o.pages.GetFrontPage()
	return front == string(HistoryMainPage)
}

func (o *FolderHistoryOverlay) folderName() string {
	return filepath.Base(o.folderPath)
}

func (o *FolderHistoryOverlay) createLayout() {
	o.modeView = tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	o.sparklines = tview.NewBox()
	o.sparklines.SetDrawFunc(o.drawSparklines)

	o.timeline = table.NewTableContainer[folderVersion](o.application, o.toTimelineCells, sortTimeline)
	o.timeline.SetTitle(" Timeline ")
	o.timeline.SetColumnSpec(timelineColumns, timelineColumnDate, false)
	o.timeline.SetActiveColumns(timelineColumns)
	o.timeline.BindColumnLayout(state.Current, stateKeyFolderHistoryTimeline, timelineColumns)
	o.timeline.SetSelectionChangedCallback(func(version *folderVersion) {
		o.selected = version
		o.updateChanges()
	})
	o.timeline.SetInputCapture(o.captureTimelineInput)

	o.changes = table.NewTableContainer[folderChange](o.application, o.toChangeCells, sortChanges)
	o.changes.SetColumnSpec(changeColumns, changeColumnName, false)
	o.changes.SetActiveColumns(changeColumns)
	o.changes.BindColumnLayout(state.Current, stateKeyFolderHistoryChanges, changeColumns)
	o.changes.SetFilterFunc(func(change *folderChange, filterText string) bool {
		return table.MatchesGlob(change.Name, filterText)
	})
	o.changes.SetFilterChangedCallback(o.updateChangesFooter)
	o.changes.SetSelectionChangedCallback(func(*folderChange) { o.updateDetails() })
	o.changes.SetInputCapture(o.captureChangesInput)

	o.details = tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetWordWrap(true)
	o.details.SetDrawFunc(drawLeftDivider)
	// hidden with ? (see shortcut_helper.ToggleShortcuts)
	o.shortcuts = shortcut_helper.NewShortcutMap(o.application).SetCollapsible()

	left := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(o.modeView, 1, 0, false).
		AddItem(o.sparklines, folderHistorySparklineLines, 0, false).
		AddItem(o.timeline.GetLayout(), 0, 1, true)
	right := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(o.details, folderHistoryHeaderLines, 0, false).
		AddItem(o.changes.GetLayout(), 0, 1, false)
	o.split = uiutil.NewResizableSplit(o.application, left, right, 2, 3).
		SetEnabledFunc(o.isMainPageInFront)
	split := o.split
	content := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(split, 0, 1, true).
		AddItem(o.shortcuts.GetLayout(), 1, 0, false)
	content.SetBorderPadding(0, 0, 1, 1)
	// the shortcuts wrap on narrow terminals
	o.shortcuts.SetOnHeightChanged(func(height int) { content.ResizeItem(o.shortcuts.GetLayout(), height, 0) })

	o.loadingView = uiutil.NewLoadingView(o.application, "", "Reading the folder in all snapshots...")
	o.loadingView.SetBorder(false)
	o.pages = tview.NewPages().
		AddPage(string(HistoryMainPage), content, true, false).
		AddPage(string(HistoryLoadingPage), o.loadingView, true, true)

	// Esc is handled by the lists, not here: this layout also contains the dialogs opened from the overlay
	o.layout = createOverlayFrame(fmt.Sprintf(" 📂 History of '%s/' ", o.folderName()), o.pages)
	o.updateModeView()
	o.updateShortcuts()
}

// scan reads the folder in all snapshots in the background and shows the history. Must be called on the UI thread.
func (o *FolderHistoryOverlay) scan() {
	ctx, cancel := context.WithCancel(context.Background())
	o.cancelScan = cancel
	folderPath, cachedEntries := o.folderPath, o.cachedEntries
	// captured here: the background work must not read shared variables (replaced in tests)
	findSnapshots := findSnapshotsOfPath
	go func() {
		snapshots, err := findSnapshots(folderPath, cachedEntries)
		var history *folderHistory
		if err == nil {
			history, err = scanFolderHistory(ctx, folderPath, snapshots)
		}
		if ctx.Err() != nil {
			return
		}
		o.application.QueueUpdateDraw(func() {
			o.loadingView.Stop()
			o.pages.HidePage(string(HistoryLoadingPage))
			o.pages.ShowPage(string(HistoryMainPage))
			if err != nil {
				logging.Error("Failed to scan the history of %s: %v", folderPath, err)
				o.details.SetText(tview.Escape("Failed to read the history: " + err.Error()))
				return
			}
			o.setHistory(history)
			o.application.SetFocus(o.timeline.GetLayout())
		})
	}()
}

// setHistory shows a scanned history. Runs on the UI thread.
func (o *FolderHistoryOverlay) setHistory(history *folderHistory) {
	o.history = history
	o.timeline.SetData(history.Changed)
	o.timeline.SetFooter(fmt.Sprintf("%d of %d snapshots", len(history.Changed), len(history.All)))
	if len(history.Changed) == 0 {
		o.details.SetText("The folder did not change in any snapshot (or is not in any).")
		o.updateChanges()
		return
	}
	if o.initialSnapshot != nil {
		o.timeline.Select(versionAt(history.Changed, folderVersionCreation, o.initialSnapshot.Properties.CreationDate))
	} else {
		o.timeline.SelectFirstIfExists()
	}
	o.selected = o.timeline.GetSelectedEntry()
	o.updateChanges()
}

// SelectSnapshot selects the version of the folder that was current in the snapshot once the history is loaded,
// instead of the newest one. Must be called before the overlay is shown, on the UI thread.
func (o *FolderHistoryOverlay) SelectSnapshot(snapshot *zfs.Snapshot) *FolderHistoryOverlay {
	o.initialSnapshot = snapshot
	return o
}

func folderVersionCreation(version *folderVersion) time.Time {
	return version.Snapshot.Properties.CreationDate
}

func sortTimeline(versions []*folderVersion, column *table.Column, inverted bool) []*folderVersion {
	sort.SliceStable(versions, func(i, j int) bool {
		a, b := versions[i].Snapshot.Properties.CreationDate, versions[j].Snapshot.Properties.CreationDate
		if inverted {
			return a.Before(b)
		}
		return a.After(b)
	})
	return versions
}

func (o *FolderHistoryOverlay) toTimelineCells(row int, columns []*table.Column, version *folderVersion) []*tview.TableCell {
	var cells []*tview.TableCell
	for _, column := range columns {
		var text string
		switch column {
		case timelineColumnSnapshot:
			text = tview.Escape(version.Snapshot.Name)
		case timelineColumnChanges:
			text = formatVersionChanges(version)
		case timelineColumnItems:
			text = "—"
			if version.Listing.Exists {
				text = fmt.Sprintf("%d", len(version.Listing.Entries))
			}
		case timelineColumnDate:
			text = uiutil.FormatTime(version.Snapshot.Properties.CreationDate)
		}
		cells = append(cells, tview.NewTableCell(text).SetAlign(column.Alignment))
	}
	return cells
}

// formatVersionChanges summarizes the changes of a version, e.g. "+3 −1 ~2", or what happened to the folder itself.
func formatVersionChanges(version *folderVersion) string {
	colors := theme.Colors.FileBrowser.Table.State
	switch version.State {
	case folderVersionInitial:
		return txwidgets.Span(colors.Equal, "initial (%d)", len(version.Changes))
	case folderVersionCreated:
		return txwidgets.Span(colors.Added, "created +%d", len(version.Changes))
	case folderVersionDeleted:
		return txwidgets.Span(colors.Deleted, "folder deleted")
	}
	added, deleted, modified := countChanges(version.Changes)
	var parts []string
	if added > 0 {
		parts = append(parts, txwidgets.Span(colors.Added, "+%d", added))
	}
	if deleted > 0 {
		parts = append(parts, txwidgets.Span(colors.Deleted, "−%d", deleted))
	}
	if modified > 0 {
		parts = append(parts, txwidgets.Span(colors.Modified, "~%d", modified))
	}
	return strings.Join(parts, " ")
}

// drawSparklines draws the number of items and the size of the folder across all snapshots, and marks the selected
// snapshot. Runs while drawing, on the UI thread.
func (o *FolderHistoryOverlay) drawSparklines(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
	fillBackground(screen, x, y, width, height)
	if o.history == nil || len(o.history.All) == 0 || height < folderHistorySparklineLines {
		return x, y, width, height
	}
	items := make([]int64, len(o.history.All))
	sizes := make([]int64, len(o.history.All))
	for i, version := range o.history.All {
		items[i], sizes[i] = -1, -1
		if version.Listing.Exists {
			items[i] = int64(len(version.Listing.Entries))
			sizes[i] = version.Listing.totalSize()
		}
	}

	const labelWidth = 7
	var itemsValue, sizeValue string
	if o.selected != nil && o.selected.Listing.Exists {
		itemsValue = fmt.Sprintf("%d", len(o.selected.Listing.Entries))
		sizeValue = strings.TrimSpace(uiutil.HumanizedBytes(uint64(max(o.selected.Listing.totalSize(), 0))))
	}
	valueWidth := max(len(itemsValue), len(sizeValue), 8) + 1
	chartWidth := width - labelWidth - valueWidth
	if chartWidth < 4 {
		return x, y, width, height
	}

	label := theme.Colors.ShortcutMap.Name
	bars := theme.Colors.Layout.Table.Accent
	drawLine := func(row int, name string, values []int64, value string) int {
		tview.Print(screen, name, x, y+row, labelWidth, tview.AlignLeft, label)
		line, column := sparkline(values, chartWidth)
		tview.Print(screen, string(line), x+labelWidth, y+row, chartWidth, tview.AlignLeft, bars)
		tview.Print(screen, value, x+labelWidth+len(line)+1, y+row, valueWidth, tview.AlignLeft, label)
		if o.selected == nil {
			return -1
		}
		return column(o.selected.Index)
	}
	drawLine(0, "Items", items, itemsValue)
	markerColumn := drawLine(1, "Size", sizes, sizeValue)
	if markerColumn >= 0 {
		tview.Print(screen, "▲", x+labelWidth+markerColumn, y+2, 1, tview.AlignLeft, theme.Colors.Layout.Table.Accent)
	}
	return x, y, width, height
}

// currentChanges returns the changed entries of the selected snapshot: compared to the previous snapshot, or the
// changes since the snapshot (until the working copy).
func (o *FolderHistoryOverlay) currentChanges() []*folderChange {
	if o.selected == nil || o.history == nil {
		return nil
	}
	if o.mode == diffModeWorkingCopy {
		return compareFolderListings(o.selected.Listing, o.history.WorkingCopy)
	}
	return o.selected.Changes
}

// updateChanges shows the changes of the selected snapshot. Runs on the UI thread.
func (o *FolderHistoryOverlay) updateChanges() {
	o.changes.SetData(o.currentChanges())
	if o.selected == nil {
		o.changes.SetTitle(" Changes ")
	} else if o.mode == diffModeWorkingCopy {
		o.changes.SetTitle(fmt.Sprintf(" Changes since %s ", o.selected.Snapshot.Name))
	} else {
		o.changes.SetTitle(fmt.Sprintf(" Changes in %s ", o.selected.Snapshot.Name))
	}
	o.changes.SelectFirstIfExists()
	o.updateChangesFooter()
	o.updateDetails()
}

func (o *FolderHistoryOverlay) updateChangesFooter() {
	changes := o.changes.GetAllEntries()
	if o.changes.IsFilterActive() {
		o.changes.SetFooter(fmt.Sprintf("%d of %d changes", len(o.changes.GetEntries()), len(changes)))
		return
	}
	added, deleted, modified := countChanges(changes)
	o.changes.SetFooter(formatChangeCounts(added, deleted, modified))
}

// formatChangeCounts returns e.g. "1 added · 0 deleted · 2 modified", counts above zero in the color of their kind.
func formatChangeCounts(added int, deleted int, modified int) string {
	colors := theme.Colors.FileBrowser.Table.State
	part := func(count int, label string, color tcell.Color) string {
		if count == 0 {
			color = theme.Colors.ShortcutMap.Name
		}
		return txwidgets.Span(color, "%d %s", count, label)
	}
	separator := txwidgets.Span(theme.Colors.ShortcutMap.Name, " · ")
	return part(added, "added", colors.Added) + separator + part(deleted, "deleted", colors.Deleted) + separator +
		part(modified, "modified", colors.Modified)
}

func sortChanges(changes []*folderChange, column *table.Column, inverted bool) []*folderChange {
	sort.SliceStable(changes, func(i, j int) bool {
		a, b := changes[i], changes[j]
		result := 0
		switch column {
		case changeColumnKind:
			result = int(a.Kind) - int(b.Kind)
		case changeColumnSize:
			result = int(changeSize(a) - changeSize(b))
		case changeColumnModified:
			result = changeModTime(a).Compare(changeModTime(b))
		}
		if result == 0 {
			result = strings.Compare(a.Name, b.Name)
		}
		if inverted {
			result = -result
		}
		return result < 0
	})
	return changes
}

// newestEntry returns the newer state of the changed entry (or the older one, if it was deleted).
func newestEntry(change *folderChange) *folderEntry {
	if change.After != nil {
		return change.After
	}
	return change.Before
}

func changeSize(change *folderChange) int64 {
	return newestEntry(change).Size
}

func changeModTime(change *folderChange) time.Time {
	return newestEntry(change).ModTime
}

// changeKindLabel describes how an entry changed, depending on the mode.
func (o *FolderHistoryOverlay) changeKindLabel(kind folderChangeKind) (symbol string, label string, color tcell.Color) {
	colors := theme.Colors.FileBrowser.Table.State
	switch kind {
	case folderChangeAdded:
		symbol, label, color = "+", "added", colors.Added
	case folderChangeDeleted:
		symbol, label, color = "−", "deleted", colors.Deleted
	default:
		symbol, label, color = "~", "modified", colors.Modified
	}
	if o.mode == diffModeWorkingCopy {
		label += " since"
	}
	return symbol, label, color
}

func (o *FolderHistoryOverlay) toChangeCells(row int, columns []*table.Column, change *folderChange) []*tview.TableCell {
	symbol, _, color := o.changeKindLabel(change.Kind)
	var cells []*tview.TableCell
	for _, column := range columns {
		var text string
		cellColor := theme.Colors.Properties.Value
		switch column {
		case changeColumnKind:
			text, cellColor = symbol, color
		case changeColumnName:
			text = tview.Escape(displayName(newestEntry(change)))
			cellColor = color
		case changeColumnSize:
			text = formatChangeSize(change)
		case changeColumnModified:
			text = uiutil.FormatTime(newestEntry(change).ModTime)
		}
		cells = append(cells, tview.NewTableCell(text).SetTextColor(cellColor).SetAlign(column.Alignment))
	}
	return cells
}

// displayName shows directories with a trailing slash and links with an arrow.
func displayName(entry *folderEntry) string {
	switch entry.Type {
	case folderEntryDirectory:
		return entry.Name + "/"
	case folderEntryLink:
		return entry.Name + " →"
	}
	return entry.Name
}

// formatChangeSize shows the size of files, e.g. "1.2 KiB → 3.4 KiB" if it changed.
func formatChangeSize(change *folderChange) string {
	format := func(entry *folderEntry) string {
		if entry.Type != folderEntryFile {
			return ""
		}
		return strings.TrimSpace(uiutil.HumanizedBytes(uint64(max(entry.Size, 0))))
	}
	if change.Kind == folderChangeModified && change.Before.Type == folderEntryFile && change.After.Type == folderEntryFile &&
		change.Before.Size != change.After.Size {
		return format(change.Before) + " → " + format(change.After)
	}
	return format(newestEntry(change))
}

// snapshotSide returns the entry as it is in the selected snapshot, nil if it is not in the snapshot.
func (o *FolderHistoryOverlay) snapshotSide(change *folderChange) *folderEntry {
	if o.mode == diffModeWorkingCopy {
		// compared from the snapshot to the working copy
		return change.Before
	}
	return change.After
}

// updateDetails describes the selected change and what can be done with it.
func (o *FolderHistoryOverlay) updateDetails() {
	change := o.changes.GetSelectedEntry()
	if o.selected == nil {
		// keeps e.g. the message that nothing changed
		return
	}
	if change == nil {
		o.details.SetText(txwidgets.Span(theme.Colors.Properties.Hint, "Enter in the timeline: restore the whole folder from %s", o.selected.Snapshot.Name))
		return
	}
	_, label, color := o.changeKindLabel(change.Kind)
	hint := theme.Colors.Properties.Hint
	var actions []string
	if o.snapshotSide(change) != nil {
		actions = append(actions, "Enter: restore it from "+o.selected.Snapshot.Name)
	} else {
		actions = append(actions, "not in "+o.selected.Snapshot.Name+", nothing to restore")
	}
	if entry := newestEntry(change); entry.Type == folderEntryFile || entry.Type == folderEntryDirectory {
		actions = append(actions, "h: its history")
	}
	o.details.SetText(txwidgets.Span(color, "%s %s", displayName(newestEntry(change)), label) + "\n" +
		txwidgets.Span(hint, "%s", describeChange(change)) + "\n" +
		txwidgets.Span(hint, "%s", strings.Join(actions, " · ")))
}

// drawLeftDivider draws a vertical line on the left of a box (the details), separating it from the mode and the
// sparklines. It is in the same column as the left border of the changes below, so it continues it. Returns the
// inner rect of the box: right of the line, with a space.
func drawLeftDivider(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
	style := tcell.StyleDefault.Foreground(theme.Colors.Layout.Border).Background(tview.Styles.PrimitiveBackgroundColor)
	for row := y; row < y+height; row++ {
		screen.SetContent(x, row, tview.Borders.Vertical, nil, style)
	}
	return x + 2, y, max(width-2, 0), height
}

// describeChange tells the size and modification time of the entry, before → after if it was modified.
func describeChange(change *folderChange) string {
	describe := func(entry *folderEntry) string {
		modified := entry.ModTime.Format(theme.Style.Format.DateTime)
		if entry.Type != folderEntryFile {
			return "modified " + modified
		}
		return strings.TrimSpace(uiutil.HumanizedBytes(uint64(max(entry.Size, 0)))) + ", modified " + modified
	}
	switch change.Kind {
	case folderChangeAdded:
		return describe(change.After)
	case folderChangeDeleted:
		return "was " + describe(change.Before)
	}
	return describe(change.Before) + " → " + describe(change.After)
}

func (o *FolderHistoryOverlay) updateModeView() {
	mode := "vs. previous snapshot"
	if o.mode == diffModeWorkingCopy {
		mode = "since snapshot (vs. now)"
	}
	o.modeView.SetText(txwidgets.Span(theme.Colors.ShortcutMap.KeyCombo, " Mode: ") + txwidgets.Span(theme.Colors.ShortcutMap.Name, "%s", mode))
}

func (o *FolderHistoryOverlay) toggleMode() {
	if o.mode == diffModePredecessor {
		o.mode = diffModeWorkingCopy
	} else {
		o.mode = diffModePredecessor
	}
	saveDiffMode(toggleFolderHistoryCompareNow, o.mode, diffModeWorkingCopy)
	o.updateModeView()
	o.updateChanges()
}

func (o *FolderHistoryOverlay) updateShortcuts() {
	entries := []shortcut_helper.ShortcutEntry{
		{KeyCombo: []string{shortcut_helper.KeyTab}, Name: "Switch list", Group: shortcut_helper.GroupNavigation},
		{KeyCombo: []string{"d"}, Name: "Toggle mode", Group: shortcut_helper.GroupView},
	}
	if o.changes.HasFocus() {
		entries = append(entries,
			shortcut_helper.ShortcutEntry{KeyCombo: []string{shortcut_helper.KeyEnter}, Name: "Restore entry"},
			shortcut_helper.ShortcutEntry{KeyCombo: []string{"h"}, Name: "History of entry"},
			uiutil.TableComponentShortcutFilter,
		)
	} else {
		entries = append(entries, shortcut_helper.ShortcutEntry{KeyCombo: []string{shortcut_helper.KeyEnter}, Name: "Restore folder"})
	}
	entries = append(entries,
		uiutil.TableComponentShortcutColumns,
		shortcut_helper.ShortcutTimeFormat,
		shortcut_helper.ShortcutHide,
		shortcut_helper.ShortcutEntry{KeyCombo: []string{shortcut_helper.KeyEsc}, Name: "Close", Group: shortcut_helper.GroupGlobal},
	)
	o.shortcuts.SetEntries(entries)
}

func (o *FolderHistoryOverlay) captureTimelineInput(event *tcell.EventKey) *tcell.EventKey {
	switch {
	case event.Key() == tcell.KeyEscape:
		o.Close()
		return nil
	case event.Key() == tcell.KeyF2:
		openColumnDialog(o.application, o.pages, "Configure Timeline Columns", timelineColumns, o.timeline)
		return nil
	case event.Key() == tcell.KeyTab || event.Key() == tcell.KeyBacktab:
		o.application.SetFocus(o.changes.GetLayout())
		o.updateShortcuts()
		return nil
	case event.Key() == tcell.KeyRune && event.Rune() == 'd':
		o.toggleMode()
		return nil
	case event.Key() == tcell.KeyEnter && o.timeline.GetSelectedEntry() != nil:
		o.restoreFolder()
		return nil
	}
	return event
}

// captureChangesInput handles the keys of the changes, after the table handled its filter (e.g. Esc ends typing a
// filter or clears it first).
func (o *FolderHistoryOverlay) captureChangesInput(event *tcell.EventKey) *tcell.EventKey {
	switch {
	case event.Key() == tcell.KeyEscape:
		o.Close()
		return nil
	case event.Key() == tcell.KeyF2:
		openColumnDialog(o.application, o.pages, "Configure Change Columns", changeColumns, o.changes)
		return nil
	case event.Key() == tcell.KeyTab || event.Key() == tcell.KeyBacktab:
		o.application.SetFocus(o.timeline.GetLayout())
		o.updateShortcuts()
		return nil
	case event.Key() == tcell.KeyRune && event.Rune() == 'd':
		o.toggleMode()
		return nil
	case event.Key() == tcell.KeyRune && event.Rune() == 'h':
		o.openEntryHistory()
		return nil
	case event.Key() == tcell.KeyEnter && o.changes.GetSelectedEntry() != nil:
		o.restoreEntry()
		return nil
	}
	return event
}

// restoreFolder restores the whole folder from the selected snapshot (the dialog offers "directory only" and
// "recursively"). Must be called on the UI thread.
func (o *FolderHistoryOverlay) restoreFolder() {
	if o.selected == nil || !o.selected.Listing.Exists {
		return
	}
	o.restore(o.folderName(), o.folderPath, data.Directory)
}

// restoreEntry restores the selected changed entry from the selected snapshot, if it is in the snapshot.
// Must be called on the UI thread.
func (o *FolderHistoryOverlay) restoreEntry() {
	change := o.changes.GetSelectedEntry()
	if change == nil || o.selected == nil {
		return
	}
	entry := o.snapshotSide(change)
	if entry == nil {
		// restoring would delete it, which this overlay does not do
		return
	}
	o.restore(entry.Name, filepath.Join(o.folderPath, entry.Name), toEntryType(entry.Type))
}

func toEntryType(entryType folderEntryType) data.FileBrowserEntryType {
	switch entryType {
	case folderEntryDirectory:
		return data.Directory
	case folderEntryLink:
		return data.Link
	}
	return data.File
}

// restore asks how to restore the path from the selected snapshot and restores it, then reads the working copy
// again. Must be called on the UI thread.
func (o *FolderHistoryOverlay) restore(name string, realPath string, entryType data.FileBrowserEntryType) {
	snapshot := o.selected.Snapshot
	snapshotPath := snapshot.GetSnapshotPath(realPath)
	snapshotStat, err := os.Lstat(snapshotPath)
	if err != nil {
		ShowDialogOnPages(o.application, o.pages, NewErrorDialog(o.application, "Restore Failed", err), nil)
		return
	}
	restoreEntry := &data.FileBrowserEntry{
		Name: name,
		Type: entryType,
		SnapshotFiles: []*data.SnapshotFile{{
			Path:         snapshotPath,
			OriginalPath: realPath,
			Stat:         snapshotStat,
			Snapshot:     snapshot,
		}},
	}
	if realStat, err := os.Lstat(realPath); err == nil {
		restoreEntry.RealFile = &data.RealFile{Name: name, Path: realPath, Stat: realStat}
	}

	onComplete := func(d *SelectionDialog, option *DialogOption, err error) {
		if option.Id != RestoreFileDialogRestoreFileActionId && option.Id != RestoreFileDialogRestoreRecursiveActionId {
			d.Close()
			return
		}
		recursive := option.Id == RestoreFileDialogRestoreRecursiveActionId
		d.Chain(func() {
			progress := NewRestoreFileProgressDialog(o.application, restoreEntry, recursive)
			ShowDialogOnPages(o.application, o.pages, progress, o.reloadWorkingCopy)
		})
	}
	ShowDialogOnPages(o.application, o.pages, NewRestoreFileDialog(o.application, restoreEntry, nil, onComplete), nil)
}

// reloadWorkingCopy reads the folder in the working copy again (e.g. after a restore) in the background, and
// updates the changes. Must be called on the UI thread.
func (o *FolderHistoryOverlay) reloadWorkingCopy() {
	folderPath := o.folderPath
	go func() {
		listing := readFolderListing(folderPath)
		o.application.QueueUpdateDraw(func() {
			if o.history == nil {
				return
			}
			o.history.WorkingCopy = listing
			o.updateChanges()
		})
	}()
}

// openEntryHistory opens the history of the selected changed entry: the file history for files, a folder history
// for folders. Must be called on the UI thread.
func (o *FolderHistoryOverlay) openEntryHistory() {
	change := o.changes.GetSelectedEntry()
	if change == nil {
		return
	}
	entry := newestEntry(change)
	path := filepath.Join(o.folderPath, entry.Name)
	switch entry.Type {
	case folderEntryDirectory:
		ShowDialogOnPages(o.application, o.pages, NewFolderHistoryOverlay(o.application, path, o.cachedEntries), nil)
	case folderEntryFile:
		file := &data.FileBrowserEntry{Name: entry.Name, Type: data.File}
		if stat, err := os.Lstat(path); err == nil {
			file.RealFile = &data.RealFile{Name: entry.Name, Path: path, Stat: stat}
		} else {
			// deleted by now: the file history only needs the original path
			file.SnapshotFiles = []*data.SnapshotFile{{OriginalPath: path, Path: o.selected.Snapshot.GetSnapshotPath(path), Snapshot: o.selected.Snapshot}}
		}
		ShowDialogOnPages(o.application, o.pages, NewFileHistoryOverlay(o.application, file, o.cachedEntries), nil)
	}
}
