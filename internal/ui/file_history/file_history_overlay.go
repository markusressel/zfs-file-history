package file_history

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/state"
	"zfs-file-history/internal/ui/dialog"
	"zfs-file-history/internal/ui/scrollbar"
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
	// fileHistorySparklineLines: the size and spacers (the selected version is highlighted in the sparkline)
	fileHistorySparklineLines = 3
	// fileHistoryHeaderLines is the height above both the versions and the diff: the mode and the sparkline on the
	// left, the metadata comparison (presence, size, mode, modification time) on the right
	fileHistoryHeaderLines = 1 + fileHistorySparklineLines
)

const (
	FileHistoryOverlayPage uiutil.Page = "FileHistoryOverlay"
	HistoryMainPage        uiutil.Page = "history-main"
	HistoryLoadingPage     uiutil.Page = "history-loading"
	DevNull                            = "/dev/null"
)

type diffMode int

const (
	diffModePredecessor diffMode = iota
	diffModeWorkingCopy
)

type FileHistoryOverlay struct {
	application    *tview.Application
	file           *data.FileBrowserEntry
	cachedEntries  []*data.SnapshotBrowserEntry
	historyEntries []*data.SnapshotBrowserEntry
	layout         *tview.Flex
	actionChannel  chan dialog.DialogActionId

	// UI widgets
	pages          *tview.Pages
	tableContainer *table.RowSelectionTable[data.SnapshotBrowserEntry]
	modeView       *tview.TextView
	sparklines     *tview.Box
	metadataView   *tview.TextView
	// diffView shows the changes, or the text, of the selected version
	diffView *scrollbar.TextView
	// diffFooter shows the number of added and removed lines in the bottom border of the diff
	diffFooter   *uiutil.BorderFooter
	shortcutHelp *shortcut_helper.ShortcutMapComponent
	// split separates the versions and the changes, its boundary can be dragged with the mouse
	split *uiutil.ResizableSplit
	// sizes are the sizes of the file in the versions of historyEntries, oldest first (-1: absent), for the
	// sparkline. Only accessed on the UI thread.
	sizes []int64

	currentSelection *data.SnapshotBrowserEntry
	// initialSnapshot is the snapshot whose version is selected once the history is loaded (nil: the newest),
	// see SelectSnapshot
	initialSnapshot      *zfs.Snapshot
	currentDiffMode      diffMode
	diffLoader           *uiutil.DebouncedLoader
	currentRawDiff       string
	copyShortcutLabel    string
	rightLayoutContainer *uiutil.LoadingContainer
	loadingView          *uiutil.LoadingView
	// dataErrors are the damaged files, to mark the damaged versions (nil: none), see SetDataErrors. Only accessed on
	// the UI thread.
	dataErrors *zfs.DataErrorIndex
	// externalViewer shows the changes of the selected version in the terminal (nil: none configured or installed),
	// see openInExternalViewer
	externalViewer *ExternalDiffViewerConfig
}

// ShowDialogOnPages finds this by a type assertion, which would fail silently if the method did not match.
var _ dialog.MouseCapturer = (*FileHistoryOverlay)(nil)

// damagedVersionMarker marks the versions of the file that are damaged, behind the name of their snapshot.
const damagedVersionMarker = "⚠ damaged"

// SetDataErrors marks the versions of the file that ZFS reported as damaged (nil: none), so an intact one is
// restored. Must be called on the UI thread.
func (o *FileHistoryOverlay) SetDataErrors(index *zfs.DataErrorIndex) {
	o.dataErrors = index
	o.tableContainer.RenderCells()
}

var (
	historyColumnName = &table.Column{
		Id:        0,
		Key:       "snapshot",
		Title:     "Snapshot",
		Alignment: tview.AlignLeft,
	}
	historyColumnDiff = &table.Column{
		Id:        1,
		Key:       "change",
		Title:     "Change",
		Alignment: tview.AlignCenter,
	}
	historyColumnDate = &table.Column{
		Id:        2,
		Key:       "creation",
		Title:     "Creation Date",
		Alignment: tview.AlignLeft,
	}
	historyColumns = []*table.Column{
		historyColumnName, historyColumnDiff, historyColumnDate,
	}
)

func NewFileHistoryOverlay(
	application *tview.Application,
	file *data.FileBrowserEntry,
	cachedEntries []*data.SnapshotBrowserEntry,
) *FileHistoryOverlay {
	overlay := &FileHistoryOverlay{
		application:     application,
		file:            file,
		cachedEntries:   cachedEntries,
		actionChannel:   make(chan dialog.DialogActionId, 1),
		currentDiffMode: loadDiffMode(toggleFileHistoryComparePrevious, diffModeWorkingCopy, diffModePredecessor),
		historyEntries:  []*data.SnapshotBrowserEntry{},
		externalViewer:  externalDiffViewer(),
	}

	overlay.tableContainer = overlay.createHistoryTable()
	overlay.tableContainer.SetTitle(" Snapshots ")

	overlay.modeView = tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(false)
	overlay.updateModeView()

	overlay.sparklines = tview.NewBox()
	overlay.sparklines.SetDrawFunc(overlay.drawSparkline)

	// the metadata comparison above the diff, next to the mode and the sparkline (like in the folder history)
	overlay.metadataView = tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(false)
	overlay.metadataView.SetDrawFunc(drawLeftDivider)

	// the text of a version is often longer than the view
	overlay.diffView = scrollbar.NewTextView(application)
	overlay.diffView.SetDynamicColors(true).
		SetRegions(true).
		SetChangedFunc(func() {
			application.Draw()
		})
	overlay.diffView.SetBorder(true)
	overlay.diffView.SetBorderPadding(0, 0, 1, 1)
	uiutil.SetupWindow(overlay.diffView, " Changes ")
	overlay.diffFooter = uiutil.NewBorderFooter(overlay.diffView.Box)

	// hidden with ? (see shortcut_helper.ToggleShortcuts)
	overlay.shortcutHelp = shortcut_helper.NewShortcutMap(application).SetCollapsible().SetEnabledFunc(overlay.isMainPageInFront)
	overlay.updateShortcuts()

	overlay.layout = overlay.createLayout()
	overlay.setupInputCaptures()

	overlay.diffLoader = uiutil.NewDebouncedLoader(application, func() {
		overlay.rightLayoutContainer.SetIsLoading(true)
		overlay.rightLayoutContainer.SetMessage("Calculating diff...")
	})

	// Start initial loading animation
	overlay.loadingView.Start()

	// Load host dataset and scan snapshots in background
	overlay.scanHistoryAsync()

	return overlay
}

// CaptureMouse lets the boundary between the versions and the changes be dragged (see dialog.ShowDialogOnPages).
func (o *FileHistoryOverlay) CaptureMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	return o.split.MouseCapture(action, event)
}

// isMainPageInFront returns whether the history is shown, not the loading view or a dialog above it.
func (o *FileHistoryOverlay) isMainPageInFront() bool {
	front, _ := o.pages.GetFrontPage()
	return front == string(HistoryMainPage)
}

func (o *FileHistoryOverlay) GetName() string {
	return string(FileHistoryOverlayPage)
}

func (o *FileHistoryOverlay) GetLayout() *tview.Flex {
	return o.layout
}

func (o *FileHistoryOverlay) GetActionChannel() <-chan dialog.DialogActionId {
	return o.actionChannel
}

func (o *FileHistoryOverlay) Close() {
	o.diffLoader.Cancel()
	o.actionChannel <- dialog.DialogCloseActionId
}

func (o *FileHistoryOverlay) createHistoryTable() *table.RowSelectionTable[data.SnapshotBrowserEntry] {
	t := table.NewTableContainer[data.SnapshotBrowserEntry](
		o.application,
		o.createTableCells,
		func(entries []*data.SnapshotBrowserEntry, columnToSortBy *table.Column, inverted bool) []*data.SnapshotBrowserEntry {
			sort.SliceStable(entries, func(i, j int) bool {
				a := entries[i].Snapshot.Properties.CreationDate
				b := entries[j].Snapshot.Properties.CreationDate
				if inverted {
					return a.Before(b)
				}
				return a.After(b)
			})
			return entries
		},
	)
	t.SetColumnSpec(historyColumns, historyColumnDate, false)
	t.SetActiveColumns(historyColumns)
	t.BindColumnLayout(state.Current, stateKeyFileHistoryTable, historyColumns)
	t.SetSelectionChangedCallback(func(entry *data.SnapshotBrowserEntry) {
		o.currentSelection = entry
		o.updateDiff()
	})
	return t
}

// SelectSnapshot selects the version of the file that was current in the snapshot once the history is loaded,
// instead of the newest one. Must be called before the overlay is shown, on the UI thread.
func (o *FileHistoryOverlay) SelectSnapshot(snapshot *zfs.Snapshot) *FileHistoryOverlay {
	o.initialSnapshot = snapshot
	return o
}

func snapshotEntryCreation(entry *data.SnapshotBrowserEntry) time.Time {
	return entry.Snapshot.Properties.CreationDate
}

func (o *FileHistoryOverlay) createTableCells(row int, columns []*table.Column, entry *data.SnapshotBrowserEntry) []*tview.TableCell {
	result := []*tview.TableCell{}
	for _, col := range columns {
		text := ""
		color := tcell.ColorWhite
		align := tview.AlignLeft

		switch col {
		case historyColumnName:
			text = entry.Snapshot.Name
			if o.dataErrors.IsDamaged(o.file.GetRealPath(), entry.Snapshot.FullName) {
				text += "  " + damagedVersionMarker
				color = theme.Colors.Alert.Error
			}
		case historyColumnDiff:
			align = tview.AlignCenter
			if o.currentDiffMode == diffModeWorkingCopy {
				switch entry.WorkingCopyDiffState {
				case diff_state.Deleted:
					text = "Present"
					color = theme.Colors.FileBrowser.Table.State.Added
				case diff_state.Added:
					text = "Absent"
					color = theme.Colors.FileBrowser.Table.State.Deleted
				case diff_state.Modified:
					text = "Modified"
					color = theme.Colors.FileBrowser.Table.State.Modified
				default:
					text = "Identical"
					color = theme.Colors.FileBrowser.Table.State.Equal
				}
			} else {
				isOldest := len(o.historyEntries) > 0 && o.historyEntries[len(o.historyEntries)-1] == entry
				switch entry.DiffState {
				case diff_state.Added:
					if isOldest {
						text = "Initial"
					} else {
						text = "Added"
					}
					color = theme.Colors.FileBrowser.Table.State.Added
				case diff_state.Deleted:
					text = "Deleted"
					color = theme.Colors.FileBrowser.Table.State.Deleted
				case diff_state.Modified:
					text = "Modified"
					color = theme.Colors.FileBrowser.Table.State.Modified
				default:
					text = "Equal"
					color = theme.Colors.FileBrowser.Table.State.Equal
				}
			}
		case historyColumnDate:
			text = uiutil.FormatTime(entry.Snapshot.Properties.CreationDate)
		}

		cell := tview.NewTableCell(text).
			SetTextColor(color).
			SetAlign(align)

		cell.SetSelectedStyle(theme.SelectedStyle(color))
		result = append(result, cell)
	}
	return result
}

func (o *FileHistoryOverlay) createLayout() *tview.Flex {
	title := fmt.Sprintf(" 📜 History of '%s' ", o.file.Name)

	// the same layout as the folder history: the mode and a sparkline above the versions, the metadata comparison
	// above the diff, so both start on the same line
	leftLayout := tview.NewFlex().SetDirection(tview.FlexRow)
	leftLayout.AddItem(o.modeView, 1, 0, false)
	leftLayout.AddItem(o.sparklines, fileHistorySparklineLines, 0, false)
	leftLayout.AddItem(o.tableContainer.GetLayout(), 0, 1, true)

	o.rightLayoutContainer = uiutil.NewLoadingContainer(o.application, o.diffView, " Changes ", "Loading...")

	rightLayout := tview.NewFlex().SetDirection(tview.FlexRow)
	rightLayout.AddItem(o.metadataView, fileHistoryHeaderLines, 0, false)
	rightLayout.AddItem(o.rightLayoutContainer, 0, 1, false)

	o.split = uiutil.NewResizableSplit(o.application, leftLayout, rightLayout, 1, 2).
		SetEnabledFunc(o.isMainPageInFront)
	splitLayout := o.split

	overlayContent := tview.NewFlex().SetDirection(tview.FlexRow)
	overlayContent.AddItem(splitLayout, 0, 1, true)
	overlayContent.AddItem(o.shortcutHelp.GetLayout(), 1, 0, false)
	overlayContent.SetBorderPadding(0, 0, 1, 1)
	// the shortcuts wrap on narrow terminals
	o.shortcutHelp.SetOnHeightChanged(func(height int) {
		overlayContent.ResizeItem(o.shortcutHelp.GetLayout(), height, 0)
	})

	o.loadingView = uiutil.NewLoadingView(o.application, "", "Finding dataset snapshots...")
	o.loadingView.SetBorder(false)

	o.pages = tview.NewPages().
		AddPage(string(HistoryMainPage), overlayContent, true, false).
		AddPage(string(HistoryLoadingPage), o.loadingView, true, true)

	return dialog.NewOverlayFrame(title, o.pages)
}

func (o *FileHistoryOverlay) setupInputCaptures() {
	o.tableContainer.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		runeChar := event.Rune()

		if key == tcell.KeyEscape {
			o.Close()
			return nil
		}

		if key == tcell.KeyF2 {
			dialog.ShowColumnDialogOnPages(o.application, o.pages, "Configure History Columns", historyColumns, o.tableContainer)
			return nil
		}

		if key == tcell.KeyTab {
			o.application.SetFocus(o.diffView)
			o.updateShortcuts()
			return nil
		}

		if runeChar == 'd' || runeChar == 'D' {
			o.toggleDiffMode()
			return nil
		}

		if runeChar == 'c' || runeChar == 'C' {
			o.copyDiffToClipboard()
			return nil
		}

		if runeChar == 'e' && o.externalViewer != nil {
			o.openInExternalViewer()
			return nil
		}

		if key == tcell.KeyEnter {
			o.restoreSelectedVersion()
			return nil
		}

		return event
	})

	o.diffView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		runeChar := event.Rune()

		if key == tcell.KeyEscape {
			o.Close()
			return nil
		}

		if key == tcell.KeyTab || key == tcell.KeyBacktab {
			o.application.SetFocus(o.tableContainer.GetLayout())
			o.updateShortcuts()
			return nil
		}

		if runeChar == 'd' || runeChar == 'D' {
			o.toggleDiffMode()
			return nil
		}

		if runeChar == 'c' || runeChar == 'C' {
			o.copyDiffToClipboard()
			return nil
		}

		if runeChar == 'e' && o.externalViewer != nil {
			o.openInExternalViewer()
			return nil
		}

		return event
	})
}

func (o *FileHistoryOverlay) updateShortcuts() {
	var entries []shortcut_helper.ShortcutEntry
	copyLabel := o.copyShortcutLabel
	if copyLabel == "" {
		copyLabel = "Copy diff"
	}

	if o.tableContainer.HasFocus() {
		entries = []shortcut_helper.ShortcutEntry{
			{KeyCombo: []string{shortcut_helper.KeyTab}, Name: "Focus Diff", Group: shortcut_helper.GroupNavigation},
			{KeyCombo: []string{"d"}, Name: "Toggle Diff Mode", Group: shortcut_helper.GroupView},
			{KeyCombo: []string{"c"}, Name: copyLabel},
			o.externalViewerShortcut(),
			{KeyCombo: []string{shortcut_helper.KeyEnter}, Name: "Restore version"},
			uiutil.TableComponentShortcutColumns,
			shortcut_helper.ShortcutTimeFormat,
			shortcut_helper.ShortcutHide,
			{KeyCombo: []string{shortcut_helper.KeyEsc}, Name: "Close history", Group: shortcut_helper.GroupGlobal},
		}
	} else {
		entries = []shortcut_helper.ShortcutEntry{
			{KeyCombo: []string{shortcut_helper.KeyTab, shortcut_helper.Shift(shortcut_helper.KeyTab)}, Name: "Focus List", Group: shortcut_helper.GroupNavigation},
			{KeyCombo: []string{"d"}, Name: "Toggle Diff Mode", Group: shortcut_helper.GroupView},
			{KeyCombo: []string{"c"}, Name: copyLabel},
			o.externalViewerShortcut(),
			shortcut_helper.ShortcutTimeFormat,
			shortcut_helper.ShortcutHide,
			{KeyCombo: []string{shortcut_helper.KeyEsc}, Name: "Close history", Group: shortcut_helper.GroupGlobal},
		}
	}
	// without the entries that are not offered (e.g. no external diff viewer)
	entries = slices.DeleteFunc(entries, func(entry shortcut_helper.ShortcutEntry) bool { return len(entry.KeyCombo) == 0 })
	o.shortcutHelp.SetEntries(entries)
}

func (o *FileHistoryOverlay) updateModeView() {
	mode := "vs. now"
	if o.currentDiffMode == diffModePredecessor {
		mode = "vs. previous version"
	}
	o.modeView.SetText(txwidgets.Span(theme.Colors.ShortcutMap.KeyCombo, " Mode: ") + txwidgets.Span(theme.Colors.ShortcutMap.Name, "%s", mode))
}

func (o *FileHistoryOverlay) toggleDiffMode() {
	if o.currentDiffMode == diffModePredecessor {
		o.currentDiffMode = diffModeWorkingCopy
	} else {
		o.currentDiffMode = diffModePredecessor
	}
	saveDiffMode(toggleFileHistoryComparePrevious, o.currentDiffMode, diffModePredecessor)
	o.updateModeView()
	o.updateShortcuts()
	o.updateDiff()
	o.tableContainer.SetData(o.historyEntries)
}

func (o *FileHistoryOverlay) renderDiffTextSync(text string) {
	o.currentRawDiff = text
	o.diffView.Clear()
	o.diffView.SetText(text)
}

func (o *FileHistoryOverlay) scanHistoryAsync() {
	filePath := o.file.GetRealPath()
	// created here, not in the background, see historyScanner.findSnapshots
	scanner := newHistoryScanner(filePath, o.cachedEntries)

	go func() {
		history, err := scanner.scan(func(msg string) {
			o.application.QueueUpdate(func() {
				o.loadingView.SetMessage(msg)
			})
		})

		if err != nil {
			logging.Error("Failed to scan history: %s", err.Error())
			o.application.QueueUpdateDraw(func() {
				o.loadingView.Stop()
				o.pages.HidePage(string(HistoryLoadingPage))
				o.pages.ShowPage(string(HistoryMainPage))
				o.rightLayoutContainer.SetIsLoading(false)
				o.renderDiffTextSync(err.Error())
			})
			return
		}

		// oldest first, like the sparkline shows them
		chronological := slices.Clone(history)
		slices.Reverse(chronological)
		sizes := scanner.sizesOf(chronological)

		o.application.QueueUpdateDraw(func() {
			o.historyEntries = history
			o.sizes = sizes
			o.tableContainer.SetData(history)
			if len(history) > 0 {
				selected := history[0]
				if o.initialSnapshot != nil {
					selected = versionAt(history, snapshotEntryCreation, o.initialSnapshot.Properties.CreationDate)
				}
				o.tableContainer.Select(selected)
				o.currentSelection = selected
				o.updateDiff()
			} else {
				o.loadingView.Stop()
				o.pages.HidePage(string(HistoryLoadingPage))
				o.pages.ShowPage(string(HistoryMainPage))
				o.rightLayoutContainer.SetIsLoading(false)
				o.renderDiffTextSync("No snapshot changes found for this file.")
				o.application.SetFocus(o.tableContainer.GetLayout())
				o.updateShortcuts()
			}
		})
	}()
}

// drawSparkline draws the size of the file in its versions, oldest first, and marks the selected one.
// Runs while drawing, on the UI thread.
func (o *FileHistoryOverlay) drawSparkline(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
	dialog.FillBackground(screen, x, y, width, height)
	if len(o.sizes) == 0 || height < 1 {
		return x, y, width, height
	}
	selected := -1
	if index := slices.Index(o.historyEntries, o.currentSelection); index >= 0 {
		// historyEntries are newest first
		selected = len(o.historyEntries) - 1 - index
	}
	value := ""
	if selected >= 0 && selected < len(o.sizes) && o.sizes[selected] >= 0 {
		value = strings.TrimSpace(uiutil.HumanizedBytes(uint64(o.sizes[selected])))
	}

	const labelWidth = 7
	valueWidth := max(len(value), 8) + 1
	chartWidth := width - labelWidth - valueWidth
	if chartWidth < 4 {
		return x, y, width, height
	}
	label := theme.Colors.ShortcutMap.Name
	tview.Print(screen, "Size", x, y, labelWidth, tview.AlignLeft, label)
	graphWidth := uiutil.DrawSparkline(screen, x+labelWidth, y, chartWidth, o.sizes, selected)
	tview.Print(screen, value, x+labelWidth+graphWidth+1, y, valueWidth, tview.AlignLeft, label)
	return x, y, width, height
}

// countDiffLines returns the number of added and removed lines of a unified diff (without its file headers).
func countDiffLines(diff string) (added int, removed int) {
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+"):
			added++
		case strings.HasPrefix(line, "-"):
			removed++
		}
	}
	return added, removed
}

// formatDiffLineCounts returns e.g. "+12 lines · −3 lines", counts above zero in the color of added/removed lines.
func formatDiffLineCounts(added int, removed int) string {
	colors := theme.Colors.FileBrowser.Table.State
	part := func(text string, count int, color tcell.Color) string {
		if count == 0 {
			color = theme.Colors.ShortcutMap.Name
		}
		return txwidgets.Span(color, "%s", text)
	}
	return part(fmt.Sprintf("+%d %s", added, uiutil.Plural(added, "line", "lines")), added, colors.Added) +
		txwidgets.Span(theme.Colors.ShortcutMap.Name, " · ") +
		part(fmt.Sprintf("−%d %s", removed, uiutil.Plural(removed, "line", "lines")), removed, colors.Deleted)
}

func presenceStr(exists bool) string {
	if exists {
		return "Exists"
	}
	return "Missing"
}

func formatCompareField(oldVal, newVal string, changed bool, isPresence bool) string {
	if !changed {
		return fmt.Sprintf("[gray]%s  ->  %s[white]", oldVal, newVal)
	}
	if isPresence {
		formatEx := func(val string) string {
			if val == "Exists" {
				return txwidgets.ColorTag(theme.Colors.Info.Ok) + "Exists[white]"
			}
			return txwidgets.ColorTag(theme.Colors.Info.Error) + "Missing[white]"
		}
		return fmt.Sprintf("%s  ->  %s", oldVal, formatEx(newVal))
	}
	return fmt.Sprintf("%s  ->  %s%s[white]", oldVal, txwidgets.ColorTag(theme.Colors.Info.Warning), newVal)
}

func (o *FileHistoryOverlay) getMetadataComparisonText(oldPath, newPath string) string {
	var sb strings.Builder

	oldStat, oldErr := os.Lstat(oldPath)
	if oldPath == DevNull {
		oldErr = os.ErrNotExist
	}
	newStat, newErr := os.Lstat(newPath)
	if newPath == DevNull {
		newErr = os.ErrNotExist
	}

	formatSize := func(s os.FileInfo, err error) string {
		if err != nil {
			return "N/A"
		}
		if s.IsDir() {
			return "Directory"
		}
		return fmt.Sprintf("%d B", s.Size())
	}

	formatMode := func(s os.FileInfo, err error) string {
		if err != nil {
			return "N/A"
		}
		return s.Mode().String()
	}

	formatTime := func(s os.FileInfo, err error) string {
		if err != nil {
			return "N/A"
		}
		return s.ModTime().Format("2006-01-02 15:04:05")
	}

	oldExists := oldErr == nil && oldPath != DevNull
	newExists := newErr == nil && newPath != DevNull

	keyColorTag := txwidgets.ColorTag(theme.Colors.Layout.Table.Accent)
	maxKeyLen := 10

	writeMetaRow := func(name string, oldVal, newVal string, changed bool, isPresence bool) {
		valStr := formatCompareField(oldVal, newVal, changed, isPresence)
		sb.WriteString(fmt.Sprintf(" %s%*s:[-]  %s\n",
			keyColorTag,
			maxKeyLen,
			name,
			valStr,
		))
	}

	writeMetaRow("Presence", presenceStr(oldExists), presenceStr(newExists), oldExists != newExists, true)
	if oldExists || newExists {
		writeMetaRow("Size", formatSize(oldStat, oldErr), formatSize(newStat, newErr), formatSize(oldStat, oldErr) != formatSize(newStat, newErr), false)
		writeMetaRow("Mode", formatMode(oldStat, oldErr), formatMode(newStat, newErr), formatMode(oldStat, oldErr) != formatMode(newStat, newErr), false)
		writeMetaRow("Mod Time", formatTime(oldStat, oldErr), formatTime(newStat, newErr), formatTime(oldStat, oldErr) != formatTime(newStat, newErr), false)
	}

	return sb.String()
}

func (o *FileHistoryOverlay) updateDiff() {
	entry := o.currentSelection
	if entry == nil {
		o.renderDiffTextSync("No version selected.")
		return
	}

	ctx, seq := o.diffLoader.Start()

	filePath := o.file.GetRealPath()
	diffMode := o.currentDiffMode
	prevSnapshot := o.predecessorOf(entry)

	go func() {
		defer o.diffLoader.Stop(seq)

		if ctx.Err() != nil {
			return
		}

		oldPath, newPath := diffPaths(filePath, entry, diffMode, prevSnapshot)
		var title string
		if diffMode == diffModeWorkingCopy {
			title = fmt.Sprintf(" Changes (Working Copy -> Selected: %s) ", entry.Snapshot.Name)
		} else {
			prevName := DevNull
			if prevSnapshot != nil {
				prevName = prevSnapshot.Name
			}
			title = fmt.Sprintf(" Changes (%s -> Selected: %s) ", prevName, entry.Snapshot.Name)
		}

		isBinary := (newPath != DevNull && IsBinaryFile(newPath)) || (oldPath != DevNull && IsBinaryFile(oldPath))
		diffText := computeHistoryDiffText(oldPath, newPath, diffMode, prevSnapshot, isBinary)
		metaText := o.getMetadataComparisonText(oldPath, newPath)

		rawDiff := filterDiffHeaders(diffText)
		coloredDiff := FormatDiffText(rawDiff, false)

		o.application.QueueUpdateDraw(func() {
			if !o.diffLoader.IsCurrentSequence(seq) {
				return
			}
			o.currentRawDiff = rawDiff
			o.metadataView.Clear()
			o.metadataView.SetText(metaText)

			uiutil.SetupWindow(o.diffView, title)
			o.diffView.Clear()
			if isBinary {
				o.diffView.SetText(tview.Escape(diffText))
				o.diffFooter.SetText("")
			} else {
				o.diffView.SetText(coloredDiff)
				o.diffView.ScrollToBeginning()
				o.diffFooter.SetText(formatDiffLineCounts(countDiffLines(rawDiff)))
			}
			o.rightLayoutContainer.SetIsLoading(false)

			frontPage, _ := o.pages.GetFrontPage()
			if frontPage == string(HistoryLoadingPage) {
				o.loadingView.Stop()
				o.pages.HidePage(string(HistoryLoadingPage))
				o.pages.ShowPage(string(HistoryMainPage))
				o.application.SetFocus(o.tableContainer.GetLayout())
				o.updateShortcuts()
			}
		})
	}()
}

func computeHistoryDiffText(oldPath, newPath string, diffMode diffMode, prevSnapshot *zfs.Snapshot, isBinary bool) string {
	if isBinary {
		return "Binary files differ, content preview not available."
	}

	// Resolve missing/deleted files to DevNull for comparison
	if oldPath != DevNull {
		stat, err := os.Lstat(oldPath)
		if os.IsNotExist(err) {
			oldPath = DevNull
		} else if err == nil && stat.IsDir() {
			return "Directory content comparison not available."
		}
	}
	if newPath != DevNull {
		stat, err := os.Lstat(newPath)
		if os.IsNotExist(err) {
			newPath = DevNull
		} else if err == nil && stat.IsDir() {
			return "Directory content comparison not available."
		}
	}

	// If both are missing, there's no diff content to show
	if oldPath == DevNull && newPath == DevNull {
		return ""
	}

	if diffMode == diffModeWorkingCopy {
		output, err := RunDiff(oldPath, newPath)
		if err != nil {
			return "Error calculating diff: " + err.Error()
		}
		return output
	}

	// diffModePredecessor
	if prevSnapshot == nil {
		// Since prevSnapshot is nil, oldPath is DevNull.
		// If newPath is also DevNull (e.g. not found), return empty
		if newPath == DevNull {
			return ""
		}
		data, err := os.ReadFile(newPath)
		if err != nil {
			return "Error reading file content: " + err.Error()
		}
		content := string(data)
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			lines[i] = "+" + line
		}
		return strings.Join(lines, "\n")
	}

	output, err := RunDiff(oldPath, newPath)
	if err != nil {
		return "Error calculating diff: " + err.Error()
	}
	return output
}

func filterDiffHeaders(diffText string) string {
	diffTextLines := strings.Split(diffText, "\n")
	var filteredLines []string
	for _, line := range diffTextLines {
		if len(line) >= 4 && (strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++")) && (line[3] == ' ' || line[3] == '\t') {
			continue
		}
		filteredLines = append(filteredLines, line)
	}
	return strings.Join(filteredLines, "\n")
}

func (o *FileHistoryOverlay) restoreSelectedVersion() {
	entry := o.currentSelection
	if entry == nil {
		return
	}

	var stat os.FileInfo
	var snapshotPath string

	if entry.DiffState == diff_state.Deleted {
		// File is deleted/absent in the selected snapshot.
		snapshotPath = ""
		stat = nil
	} else {
		snapshotPath = entry.Snapshot.GetSnapshotPath(o.file.GetRealPath())
		var err error
		stat, err = os.Lstat(snapshotPath)
		if err != nil {
			logging.Error("Could not stat snapshot file %s: %s", snapshotPath, err.Error())
			errDialog := dialog.NewErrorDialog(o.application, "Restore Failed", err)
			dialog.ShowDialogOnPages(o.application, o.pages, errDialog, nil)
			return
		}
	}

	snapFile := &data.SnapshotFile{
		Path:         snapshotPath,
		OriginalPath: o.file.GetRealPath(),
		Stat:         stat,
		Snapshot:     entry.Snapshot,
	}

	restoreEntry := &data.FileBrowserEntry{
		Name:          o.file.Name,
		RealFile:      o.file.RealFile,
		SnapshotFiles: []*data.SnapshotFile{snapFile},
		Type:          o.file.Type,
		DiffState:     entry.DiffState,
	}

	onComplete := func(d *dialog.SelectionDialog, option *dialog.DialogOption, err error) {
		// Only trigger chain logic if we are doing a restore
		if option.Id == dialog.RestoreFileDialogRestoreFileActionId || option.Id == dialog.RestoreFileDialogRestoreRecursiveActionId {

			// Use Chain() instead of Close() + QueueUpdateDraw()
			d.Chain(func() {
				progressDialog := dialog.NewRestoreFileProgressDialog(o.application, restoreEntry, false)
				dialog.ShowDialogOnPages(o.application, o.pages, progressDialog, func() {
					o.updateDiff()
				})
			})
		} else {
			d.Close()
		}
	}

	restoreDialog := dialog.NewRestoreFileDialog(o.application, restoreEntry, nil, onComplete)
	dialog.ShowDialogOnPages(o.application, o.pages, restoreDialog, nil)
}

// predecessorOf returns the snapshot of the version before entry in the diff mode diffModePredecessor (nil: none, or
// another mode). Must be called on the UI thread.
func (o *FileHistoryOverlay) predecessorOf(entry *data.SnapshotBrowserEntry) *zfs.Snapshot {
	if o.currentDiffMode != diffModePredecessor {
		return nil
	}
	index := slices.Index(o.historyEntries, entry)
	if index >= 0 && index < len(o.historyEntries)-1 {
		return o.historyEntries[index+1].Snapshot
	}
	return nil
}

// diffPaths returns the files whose changes are shown for the version of the file at filePath in entry: from the
// working copy, or from the version of prevSnapshot (DevNull if nil: the file was added).
func diffPaths(filePath string, entry *data.SnapshotBrowserEntry, mode diffMode, prevSnapshot *zfs.Snapshot) (oldPath string, newPath string) {
	newPath = entry.Snapshot.GetSnapshotPath(filePath)
	switch {
	case mode == diffModeWorkingCopy:
		oldPath = filePath
	case prevSnapshot != nil:
		oldPath = prevSnapshot.GetSnapshotPath(filePath)
	default:
		oldPath = DevNull
	}
	return oldPath, newPath
}

// externalViewerShortcut is the shortcut entry of openInExternalViewer, without keys if there is no viewer (left out,
// see updateShortcuts).
func (o *FileHistoryOverlay) externalViewerShortcut() shortcut_helper.ShortcutEntry {
	if o.externalViewer == nil {
		return shortcut_helper.ShortcutEntry{}
	}
	return shortcut_helper.ShortcutEntry{KeyCombo: []string{"e"}, Name: "Open in " + filepath.Base(o.externalViewer.Path)}
}

// openInExternalViewer shows the changes of the selected version, as the diff shows them, in the external diff
// viewer, e.g. to scroll through them with its search. Must be called on the UI thread.
func (o *FileHistoryOverlay) openInExternalViewer() {
	entry := o.currentSelection
	if entry == nil || o.externalViewer == nil {
		return
	}
	oldPath, newPath := diffPaths(o.file.GetRealPath(), entry, o.currentDiffMode, o.predecessorOf(entry))
	viewer := *o.externalViewer
	// it waits for the UI thread, which suspends the application while the viewer runs
	go runExternalDiffEditor(o.application, viewer, oldPath, newPath)
}

func (o *FileHistoryOverlay) copyDiffToClipboard() {
	if o.currentRawDiff == "" {
		return
	}
	err := uiutil.CopyToClipboard(o.currentRawDiff)
	if err != nil {
		errDialog := dialog.NewErrorDialog(o.application, "Copy Failed", err)
		dialog.ShowDialogOnPages(o.application, o.pages, errDialog, nil)
	} else {
		o.copyShortcutLabel = "Copied!"
		o.updateShortcuts()
		time.AfterFunc(2*time.Second, func() {
			o.application.QueueUpdateDraw(func() {
				o.copyShortcutLabel = ""
				o.updateShortcuts()
			})
		})
	}
}
