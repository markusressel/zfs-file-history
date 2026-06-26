package dialog

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/ui/theme"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"golang.org/x/term"
)

const FileHistoryOverlayPage uiutil.Page = "FileHistoryOverlay"

type diffMode int

const (
	diffModePredecessor diffMode = iota
	diffModeWorkingCopy
)

type FileHistoryOverlay struct {
	application    *tview.Application
	file           *data.FileBrowserEntry
	historyEntries []*data.SnapshotBrowserEntry
	layout         *tview.Flex
	actionChannel  chan DialogActionId

	// UI widgets
	pages          *tview.Pages
	tableContainer *table.RowSelectionTable[data.SnapshotBrowserEntry]
	diffView       *tview.TextView
	shortcutHelp   *shortcut_helper.ShortcutMapComponent

	currentSelection *data.SnapshotBrowserEntry
	currentDiffMode  diffMode
	diffLoader       *uiutil.DebouncedLoader
}

var (
	historyColumnName = &table.Column{
		Id:        0,
		Title:     "Snapshot",
		Alignment: tview.AlignLeft,
	}
	historyColumnDiff = &table.Column{
		Id:        1,
		Title:     "Change",
		Alignment: tview.AlignCenter,
	}
	historyColumnDate = &table.Column{
		Id:        2,
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
) *FileHistoryOverlay {
	overlay := &FileHistoryOverlay{
		application:     application,
		file:            file,
		actionChannel:   make(chan DialogActionId, 1),
		currentDiffMode: diffModePredecessor,
		historyEntries:  []*data.SnapshotBrowserEntry{},
	}

	overlay.tableContainer = overlay.createHistoryTable()
	overlay.diffView = tview.NewTextView().
		SetDynamicColors(true).
		SetRegions(true).
		SetChangedFunc(func() {
			application.Draw()
		})
	overlay.diffView.SetBorder(true).SetTitle(" Changes ")

	overlay.diffLoader = uiutil.NewDebouncedLoader(application, func() {
		overlay.renderDiffTextSync("Calculating diff...")
	})

	overlay.shortcutHelp = shortcut_helper.NewShortcutMap(application)
	overlay.updateShortcuts()

	overlay.layout = overlay.createLayout()
	overlay.setupInputCaptures()

	// Load host dataset and scan snapshots in background
	overlay.scanHistoryAsync()

	return overlay
}

func (o *FileHistoryOverlay) GetName() string {
	return string(FileHistoryOverlayPage)
}

func (o *FileHistoryOverlay) GetLayout() *tview.Flex {
	return o.layout
}

func (o *FileHistoryOverlay) GetActionChannel() <-chan DialogActionId {
	return o.actionChannel
}

func (o *FileHistoryOverlay) Close() {
	o.diffLoader.Cancel()
	o.actionChannel <- DialogCloseActionId
}

func (o *FileHistoryOverlay) createHistoryTable() *table.RowSelectionTable[data.SnapshotBrowserEntry] {
	t := table.NewTableContainer[data.SnapshotBrowserEntry](
		o.application,
		o.createTableCells,
		func(entries []*data.SnapshotBrowserEntry, columnToSortBy *table.Column, inverted bool) []*data.SnapshotBrowserEntry {
			sort.SliceStable(entries, func(i, j int) bool {
				a := entries[i].Snapshot.GetCreationDate()
				b := entries[j].Snapshot.GetCreationDate()
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
	t.SetSelectionChangedCallback(func(entry *data.SnapshotBrowserEntry) {
		o.currentSelection = entry
		o.updateDiff()
	})
	return t
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
		case historyColumnDiff:
			align = tview.AlignCenter
			switch entry.DiffState {
			case diff_state.Added:
				text = "Added"
				color = theme.Colors.SnapshotBrowser.Table.State.LocalOnly
			case diff_state.Deleted:
				text = "Deleted"
				color = theme.Colors.SnapshotBrowser.Table.State.SnapshotOnly
			case diff_state.Modified:
				text = "Modified"
				color = theme.Colors.SnapshotBrowser.Table.State.Modified
			default:
				text = "Unknown"
				color = tcell.ColorGray
			}
		case historyColumnDate:
			text = entry.Snapshot.Properties.CreationDate.Format(theme.Style.Format.DateTime)
		}

		cell := tview.NewTableCell(text).
			SetTextColor(color).
			SetAlign(align)

		statusColor := o.determineStatusColor(entry)
		cell.SetSelectedStyle(
			tcell.StyleDefault.
				Foreground(theme.Colors.Layout.Table.SelectedForeground).
				Background(statusColor),
		)
		result = append(result, cell)
	}
	return result
}

func (o *FileHistoryOverlay) determineStatusColor(entry *data.SnapshotBrowserEntry) tcell.Color {
	switch entry.DiffState {
	case diff_state.Equal:
		return theme.Colors.SnapshotBrowser.Table.State.Equal
	case diff_state.Deleted:
		return theme.Colors.SnapshotBrowser.Table.State.SnapshotOnly
	case diff_state.Added:
		return theme.Colors.SnapshotBrowser.Table.State.LocalOnly
	case diff_state.Modified:
		return theme.Colors.SnapshotBrowser.Table.State.Modified
	default:
		return theme.Colors.SnapshotBrowser.Table.State.Unknown
	}
}

func (o *FileHistoryOverlay) createLayout() *tview.Flex {
	termWidth, termHeight, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || termWidth <= 0 || termHeight <= 0 {
		termWidth = 100
		termHeight = 30
	}
	width := termWidth - 4
	if width < 80 {
		width = 80
	}
	height := termHeight - 2
	if height < 15 {
		height = 15
	}

	title := fmt.Sprintf(" 📜 History of '%s' ", o.file.Name)

	splitLayout := tview.NewFlex().SetDirection(tview.FlexColumn)
	splitLayout.AddItem(o.tableContainer.GetLayout(), 0, 1, true)
	splitLayout.AddItem(o.diffView, 0, 2, false)

	overlayContent := tview.NewFlex().SetDirection(tview.FlexRow)
	overlayContent.AddItem(splitLayout, 0, 1, true)
	overlayContent.AddItem(o.shortcutHelp.GetLayout(), 1, 0, false)
	overlayContent.SetBorderPadding(0, 0, 1, 1)

	o.pages = tview.NewPages().
		AddPage("history-main", overlayContent, true, true)

	dialogFrame := tview.NewFlex()
	dialogFrame.SetBorder(true)
	uiutil.SetupDialogWindow(dialogFrame, title)
	dialogFrame.AddItem(o.pages, 0, 1, true)

	dialogContentColumnWrapper := tview.NewFlex()
	dialogContentColumnWrapper.AddItem(nil, 0, 1, false)

	dialogContentRowWrapper := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(dialogFrame, height, 1, true).
		AddItem(nil, 0, 1, false)

	dialogContentColumnWrapper.
		AddItem(dialogContentRowWrapper, width, 1, true).
		AddItem(nil, 0, 1, false)

	return dialogContentColumnWrapper
}

func (o *FileHistoryOverlay) setupInputCaptures() {
	o.tableContainer.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		runeChar := event.Rune()

		if key == tcell.KeyEscape {
			o.Close()
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

		return event
	})
}

func (o *FileHistoryOverlay) updateShortcuts() {
	var entries []shortcut_helper.ShortcutEntry
	if o.tableContainer.HasFocus() {
		entries = []shortcut_helper.ShortcutEntry{
			{KeyCombo: []string{"⭾"}, Name: "Focus Diff"},
			{KeyCombo: []string{"d"}, Name: fmt.Sprintf("Diff mode: %s", o.getDiffModeName())},
			{KeyCombo: []string{"Enter"}, Name: "Restore version"},
			{KeyCombo: []string{"Esc"}, Name: "Close history"},
		}
	} else {
		entries = []shortcut_helper.ShortcutEntry{
			{KeyCombo: []string{"⭾", "shift+⭾"}, Name: "Focus List"},
			{KeyCombo: []string{"d"}, Name: fmt.Sprintf("Diff mode: %s", o.getDiffModeName())},
			{KeyCombo: []string{"Esc"}, Name: "Close history"},
		}
	}
	o.shortcutHelp.SetEntries(entries)
}

func (o *FileHistoryOverlay) getDiffModeName() string {
	if o.currentDiffMode == diffModePredecessor {
		return "vs Predecessor"
	}
	return "vs Working Copy"
}

func (o *FileHistoryOverlay) toggleDiffMode() {
	if o.currentDiffMode == diffModePredecessor {
		o.currentDiffMode = diffModeWorkingCopy
	} else {
		o.currentDiffMode = diffModePredecessor
	}
	o.updateShortcuts()
	o.updateDiff()
}

func (o *FileHistoryOverlay) renderDiffTextSync(text string) {
	o.diffView.Clear()
	o.diffView.SetText(text)
}

func (o *FileHistoryOverlay) scanHistoryAsync() {
	o.renderDiffTextSync("Finding dataset snapshots...")

	filePath := o.file.RealFile.Path

	go func() {
		ds, err := zfs.FindHostDataset(filePath)
		if err != nil {
			logging.Error("Failed to find host dataset for %s: %s", filePath, err.Error())
			o.application.QueueUpdate(func() {
				o.renderDiffTextSync(fmt.Sprintf("Failed to load dataset: %s", err.Error()))
			})
			return
		}

		snapshots, err := ds.GetSnapshots()
		if err != nil {
			logging.Error("Failed to get snapshots for dataset %s: %s", ds.Path, err.Error())
			o.application.QueueUpdate(func() {
				o.renderDiffTextSync(fmt.Sprintf("Failed to load snapshots: %s", err.Error()))
			})
			return
		}

		o.application.QueueUpdate(func() {
			o.renderDiffTextSync("Scanning snapshot history for changes...")
		})

		slices.SortFunc(snapshots, func(a, b *zfs.Snapshot) int {
			return a.GetCreationDate().Compare(b.GetCreationDate())
		})

		var history []*data.SnapshotBrowserEntry
		var prev *zfs.Snapshot = nil

		for _, snap := range snapshots {
			state := snap.DetermineDiffStateBetween(filePath, prev)
			if state != diff_state.Equal && state != diff_state.Unknown {
				history = append(history, &data.SnapshotBrowserEntry{
					Snapshot:  snap,
					DiffState: state,
					IsLoading: false,
				})
				prev = snap
			} else if state == diff_state.Equal {
				prev = snap
			}
		}

		slices.Reverse(history)

		o.application.QueueUpdate(func() {
			o.historyEntries = history
			o.tableContainer.SetData(history)
			if len(history) > 0 {
				o.tableContainer.SelectFirstIfExists()
				o.currentSelection = history[0]
				o.updateDiff()
			} else {
				o.renderDiffTextSync("No snapshot changes found for this file.")
			}
		})
	}()
}

func (o *FileHistoryOverlay) updateDiff() {
	entry := o.currentSelection
	if entry == nil {
		o.renderDiffTextSync("No version selected.")
		return
	}

	ctx, seq := o.diffLoader.Start()

	filePath := o.file.RealFile.Path
	diffMode := o.currentDiffMode

	var prevSnapshot *zfs.Snapshot = nil
	if diffMode == diffModePredecessor {
		index := slices.Index(o.historyEntries, entry)
		if index >= 0 && index < len(o.historyEntries)-1 {
			prevSnapshot = o.historyEntries[index+1].Snapshot
		}
	}

	o.renderDiffTextSync("Loading diff...")

	go func() {
		defer o.diffLoader.Stop(seq)

		if ctx.Err() != nil {
			return
		}

		var diffText string
		if diffMode == diffModeWorkingCopy {
			realFilePath := filePath
			snapshotFilePath := entry.Snapshot.GetSnapshotPath(filePath)

			_, err := os.Lstat(realFilePath)
			if os.IsNotExist(err) {
				diffText = "Working copy file does not exist (deleted)."
			} else {
				output, err := exec.Command(
					DiffBinPath,
					"-U", "3",
					snapshotFilePath,
					realFilePath,
				).Output()
				diffText = string(output)
				if err != nil && err.Error() != "exit status 1" {
					diffText = "Error calculating diff: " + err.Error()
				}
			}
		} else {
			snapshotFilePath := entry.Snapshot.GetSnapshotPath(filePath)
			var prevPath string
			if prevSnapshot != nil {
				prevPath = prevSnapshot.GetSnapshotPath(filePath)
			} else {
				prevPath = "/dev/null"
			}

			output, err := exec.Command(
				DiffBinPath,
				"-U", "3",
				prevPath,
				snapshotFilePath,
			).Output()
			diffText = string(output)
			if err != nil && err.Error() != "exit status 1" {
				diffText = "Error calculating diff: " + err.Error()
			}
		}

		diffTextLines := strings.Split(diffText, "\n")
		for i := 0; i < len(diffTextLines); i++ {
			line := diffTextLines[i]
			if strings.HasPrefix(line, "+") {
				diffTextLines[i] = `[green]` + line + `[white]`
			} else if strings.HasPrefix(line, "-") {
				diffTextLines[i] = `[red]` + line + `[white]`
			}
		}
		diffText = strings.Join(diffTextLines, "\n")

		o.application.QueueUpdate(func() {
			if !o.diffLoader.IsCurrentSequence(seq) {
				return
			}
			o.diffView.Clear()
			o.diffView.SetText(diffText)
			o.diffView.ScrollToBeginning()
		})
	}()
}

func (o *FileHistoryOverlay) restoreSelectedVersion() {
	entry := o.currentSelection
	if entry == nil {
		return
	}

	snapshotPath := entry.Snapshot.GetSnapshotPath(o.file.RealFile.Path)
	stat, err := os.Lstat(snapshotPath)
	if err != nil {
		logging.Error("Could not stat snapshot file %s: %s", snapshotPath, err.Error())
		errDialog := NewErrorDialog(o.application, "Restore Failed", err)
		ShowDialogOnPages(o.application, o.pages, errDialog, nil)
		return
	}

	snapFile := &data.SnapshotFile{
		Path:         snapshotPath,
		OriginalPath: o.file.RealFile.Path,
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

	onComplete := func(d *SelectionDialog, option *DialogOption, err error) {
		d.Close()

		if option.Id == RestoreFileDialogRestoreFileActionId {
			o.application.QueueUpdateDraw(func() {
				progressDialog := NewRestoreFileProgressDialog(o.application, restoreEntry, false)
				ShowDialogOnPages(o.application, o.pages, progressDialog, func() {
					o.updateDiff()
				})
			})
		}
	}

	restoreDialog := NewRestoreFileDialog(o.application, restoreEntry, nil, onComplete)
	ShowDialogOnPages(o.application, o.pages, restoreDialog, nil)
}
