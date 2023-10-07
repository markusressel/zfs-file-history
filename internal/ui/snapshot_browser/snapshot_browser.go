package snapshot_browser

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"golang.org/x/exp/slices"
	"sort"
	"strings"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/dialog"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/util"
	"zfs-file-history/internal/zfs"
)

type SelectionInfo[T any] struct {
	Index int
	Entry *T
}

type SnapshotBrowserComponent struct {
	application *tview.Application

	layout *tview.Pages

	tableContainer *table.RowSelectionTable[data.SnapshotBrowserEntry]

	path             string
	hostDataset      *zfs.Dataset
	currentFileEntry *data.FileBrowserEntry

	selectedSnapshotMap map[string]*SelectionInfo[data.SnapshotBrowserEntry]

	selectedSnapshotChangedCallback func(snapshot *data.SnapshotBrowserEntry)
}

var (
	columnName = &table.Column{
		Id:        0,
		Title:     "Name",
		Alignment: tview.AlignLeft,
	}
	columnDate = &table.Column{
		Id:        1,
		Title:     "Date",
		Alignment: tview.AlignLeft,
	}
	columnDiff = &table.Column{
		Id:        2,
		Title:     "Diff",
		Alignment: tview.AlignCenter,
	}
	tableColumns = []*table.Column{
		columnName, columnDate, columnDiff,
	}
)

func NewSnapshotBrowser(application *tview.Application) *SnapshotBrowserComponent {
	snapshotBrowser := &SnapshotBrowserComponent{
		application:                     application,
		selectedSnapshotMap:             map[string]*SelectionInfo[data.SnapshotBrowserEntry]{},
		selectedSnapshotChangedCallback: func(snapshot *data.SnapshotBrowserEntry) {},
	}

	snapshotBrowser.layout = snapshotBrowser.createLayout()

	return snapshotBrowser
}

func (snapshotBrowser *SnapshotBrowserComponent) createLayout() *tview.Pages {
	layout := tview.NewPages()

	toTableCellsFunction := func(row int, columns []*table.Column, entry *data.SnapshotBrowserEntry) (cells []*tview.TableCell) {
		result := []*tview.TableCell{}
		for _, column := range columns {
			cellText := "N/A"
			cellAlign := tview.AlignLeft
			cellColor := tcell.ColorWhite
			if column == columnDate {
				cellText = entry.Snapshot.Date.Format("2006-01-02 15:04:05")
			} else if column == columnName {
				cellText = entry.Snapshot.Name
			} else if column == columnDiff {
				cellAlign = tview.AlignCenter
				switch entry.DiffState {
				case diff_state.Equal:
					cellText = "="
					cellColor = theme.Colors.SnapshotBrowser.Table.State.Equal
				case diff_state.Deleted:
					cellText = "+"
					cellColor = theme.Colors.SnapshotBrowser.Table.State.Deleted
				case diff_state.Added:
					cellText = "-"
					cellColor = theme.Colors.SnapshotBrowser.Table.State.Added
				case diff_state.Modified:
					cellText = "≠"
					cellColor = theme.Colors.SnapshotBrowser.Table.State.Modified
				case diff_state.Unknown:
					cellText = "N/A"
					cellColor = theme.Colors.SnapshotBrowser.Table.State.Unknown
				}
			}
			cell := tview.NewTableCell(cellText).
				SetTextColor(cellColor).SetAlign(cellAlign)
			result = append(result, cell)
		}
		return result
	}

	tableEntrySortFunction := func(entries []*data.SnapshotBrowserEntry, columnToSortBy *table.Column, inverted bool) []*data.SnapshotBrowserEntry {
		sort.SliceStable(entries, func(i, j int) bool {
			a := entries[i]
			b := entries[j]

			result := 0
			if columnToSortBy == columnName {
				result = strings.Compare(strings.ToLower(a.Snapshot.Name), strings.ToLower(b.Snapshot.Name))
			} else if columnToSortBy == columnDate {
				result = a.Snapshot.Date.Compare(*b.Snapshot.Date)
			} else if columnToSortBy == columnDiff {
				result = int(b.DiffState - a.DiffState)
			}
			if inverted {
				result *= -1
			}

			if result <= 0 {
				return true
			} else {
				return false
			}
		})
		return entries
	}

	snapshotBrowser.tableContainer = table.NewTableContainer[data.SnapshotBrowserEntry](
		snapshotBrowser.application,
		toTableCellsFunction,
		tableEntrySortFunction,
	)

	snapshotBrowser.tableContainer.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		if snapshotBrowser.GetSelection() != nil {
			if key == tcell.KeyEnter {
				snapshotBrowser.openActionDialog(snapshotBrowser.GetSelection())
				return nil
			}
		}
		return event
	})

	snapshotBrowser.tableContainer.SetColumnSpec(tableColumns, columnDate, true)
	snapshotBrowser.tableContainer.SetSelectionChangedCallback(func(entry *data.SnapshotBrowserEntry) {
		snapshotBrowser.rememberSelectionForDataset(entry)
		snapshotBrowser.selectedSnapshotChangedCallback(entry)
		snapshotBrowser.updateTableContents()
	})

	layout.AddPage("snapshot-browser", snapshotBrowser.tableContainer.GetLayout(), true, true)
	return layout
}

func (snapshotBrowser *SnapshotBrowserComponent) GetLayout() tview.Primitive {
	return snapshotBrowser.layout
}

func (snapshotBrowser *SnapshotBrowserComponent) SetPath(path string, force bool) {
	if !force && snapshotBrowser.path == path {
		return
	}
	snapshotBrowser.path = path

	snapshotBrowser.updateTableContents()
}

func (snapshotBrowser *SnapshotBrowserComponent) Refresh(force bool) {
	zfs.RefreshZfsData()
	snapshotBrowser.SetPath(snapshotBrowser.path, force)
}

func (snapshotBrowser *SnapshotBrowserComponent) SetFileEntry(fileEntry *data.FileBrowserEntry) {
	if fileEntry != nil &&
		snapshotBrowser.currentFileEntry != nil &&
		snapshotBrowser.currentFileEntry.GetRealPath() == fileEntry.GetRealPath() {
		return
	}
	snapshotBrowser.currentFileEntry = fileEntry
	snapshotBrowser.updateTableContents()
}

func (snapshotBrowser *SnapshotBrowserComponent) Focus() {
	snapshotBrowser.application.SetFocus(snapshotBrowser.GetLayout())
}

func (snapshotBrowser *SnapshotBrowserComponent) HasFocus() bool {
	return snapshotBrowser.layout.HasFocus()
}

func (snapshotBrowser *SnapshotBrowserComponent) updateTableContents() {
	newEntries := snapshotBrowser.computeTableEntries()
	snapshotBrowser.tableContainer.SetData(newEntries)

	title := "Snapshots"
	if snapshotBrowser.GetSelection() != nil {
		currentSelectionIndex := slices.Index(snapshotBrowser.GetEntries(), snapshotBrowser.GetSelection()) + 1
		totalEntriesCount := len(snapshotBrowser.GetEntries())
		title = fmt.Sprintf("Snapshot: %s (%d/%d)", snapshotBrowser.GetSelection().Snapshot.Name, currentSelectionIndex, totalEntriesCount)
	}
	snapshotBrowser.tableContainer.SetTitle(title)
	snapshotBrowser.restoreSelectionForDataset()
}

func (snapshotBrowser *SnapshotBrowserComponent) computeTableEntries() []*data.SnapshotBrowserEntry {
	result := []*data.SnapshotBrowserEntry{}
	ds, err := zfs.FindHostDataset(snapshotBrowser.path)
	if err != nil {
		logging.Error(err.Error())
		return result
	}
	snapshotBrowser.hostDataset = ds

	if snapshotBrowser.hostDataset == nil {
		return result
	}

	snapshots, err := snapshotBrowser.hostDataset.GetSnapshots()
	if err != nil {
		logging.Error(err.Error())
		return result
	}

	for _, snapshot := range snapshots {
		diffState := diff_state.Unknown
		if snapshotBrowser.currentFileEntry != nil {
			filePath := snapshotBrowser.currentFileEntry.GetRealPath()
			diffState = snapshot.DetermineDiffState(filePath)
		}
		result = append(result, &data.SnapshotBrowserEntry{
			Snapshot:  snapshot,
			DiffState: diffState,
		})
	}

	// TODO: for testing
	//for i := 0; i < 5; i++ {
	//	result = append(result, result...)
	//}

	return result
}

func (snapshotBrowser *SnapshotBrowserComponent) clear() {
	snapshotBrowser.path = ""
	snapshotBrowser.updateTableContents()
}

func (snapshotBrowser *SnapshotBrowserComponent) rememberSelectionForDataset(selection *data.SnapshotBrowserEntry) {
	if snapshotBrowser.hostDataset == nil {
		return
	}
	snapshotBrowser.selectedSnapshotMap[snapshotBrowser.hostDataset.Path] = &SelectionInfo[data.SnapshotBrowserEntry]{
		Index: slices.Index(snapshotBrowser.GetEntries(), selection),
		Entry: selection,
	}
}

func (snapshotBrowser *SnapshotBrowserComponent) getRememberedSelectionInfo(path string) *SelectionInfo[data.SnapshotBrowserEntry] {
	selectionInfo, ok := snapshotBrowser.selectedSnapshotMap[path]
	if !ok {
		return nil
	} else {
		return selectionInfo
	}
}

func (snapshotBrowser *SnapshotBrowserComponent) restoreSelectionForDataset() {
	var entryToSelect *data.SnapshotBrowserEntry
	if snapshotBrowser.hostDataset == nil {
		snapshotBrowser.selectSnapshot(entryToSelect)
		return
	}

	entries := snapshotBrowser.GetEntries()
	rememberedSelectionInfo := snapshotBrowser.getRememberedSelectionInfo(snapshotBrowser.hostDataset.Path)
	if rememberedSelectionInfo == nil {
		if len(entries) > 0 {
			entryToSelect = entries[0]
		}
	} else {
		var index int
		if rememberedSelectionInfo.Entry == nil {
			snapshotBrowser.selectHeader()
			return
		} else {
			index = slices.IndexFunc(entries, func(entry *data.SnapshotBrowserEntry) bool {
				return entry.Snapshot.Name == rememberedSelectionInfo.Entry.Snapshot.Name
			})
		}
		if index < 0 {
			closestIndex := util.Coerce(rememberedSelectionInfo.Index, 0, len(entries)-1)
			entryToSelect = entries[closestIndex]
		} else {
			entryToSelect = entries[index]
		}
	}
	snapshotBrowser.selectSnapshot(entryToSelect)
}

func (snapshotBrowser *SnapshotBrowserComponent) selectSnapshot(snapshot *data.SnapshotBrowserEntry) {
	snapshotBrowser.selectedSnapshotChangedCallback(snapshot)
	if snapshotBrowser.GetSelection() == snapshot || snapshotBrowser.GetSelection() != nil && snapshot != nil && snapshotBrowser.GetSelection().Snapshot.Path == snapshot.Snapshot.Path {
		return
	}
	snapshotBrowser.tableContainer.Select(snapshot)
}

func (snapshotBrowser *SnapshotBrowserComponent) GetSelection() *data.SnapshotBrowserEntry {
	return snapshotBrowser.tableContainer.GetSelectedEntry()
}

func (snapshotBrowser *SnapshotBrowserComponent) SetSelectedSnapshotChangedCallback(f func(snapshot *data.SnapshotBrowserEntry)) {
	snapshotBrowser.selectedSnapshotChangedCallback = f
}

func (snapshotBrowser *SnapshotBrowserComponent) GetEntries() []*data.SnapshotBrowserEntry {
	return snapshotBrowser.tableContainer.GetEntries()
}

func (snapshotBrowser *SnapshotBrowserComponent) selectHeader() {
	snapshotBrowser.tableContainer.SelectHeader()
}

func (snapshotBrowser *SnapshotBrowserComponent) selectFirstIfExists() {
	snapshotBrowser.tableContainer.SelectFirstIfExists()
}

func (snapshotBrowser *SnapshotBrowserComponent) openActionDialog(selection *data.SnapshotBrowserEntry) {
	if snapshotBrowser.GetSelection() == nil {
		return
	}
	actionDialogLayout := dialog.NewSnapshotActionDialog(snapshotBrowser.application, selection)
	actionHandler := func(action dialog.DialogActionId) bool {
		switch action {
		case dialog.SnapshotDialogCreateSnapshotActionId:
			err := snapshotBrowser.createSnapshot(selection)
			if err != nil {
				logging.Error(err.Error())
			}
			return true
		case dialog.SnapshotDialogDestroySnapshotActionId:
			err := snapshotBrowser.destroySnapshot(selection, false)
			if err != nil {
				logging.Error(err.Error())
			}
			return true
		case dialog.SnapshotDialogDestroySnapshotRecursivelyActionId:
			err := snapshotBrowser.destroySnapshot(selection, true)
			if err != nil {
				logging.Error(err.Error())
			}
			return true
		}
		return false
	}
	snapshotBrowser.showDialog(actionDialogLayout, actionHandler)
}

func (snapshotBrowser *SnapshotBrowserComponent) showDialog(d dialog.Dialog, actionHandler func(action dialog.DialogActionId) bool) {
	layout := d.GetLayout()
	go func() {
		for {
			action := <-d.GetActionChannel()
			if actionHandler(action) {
				return
			}
			if action == dialog.DialogCloseActionId {
				snapshotBrowser.layout.RemovePage(d.GetName())
			}
		}
	}()
	snapshotBrowser.layout.AddPage(d.GetName(), layout, true, true)
}

func (snapshotBrowser *SnapshotBrowserComponent) createSnapshot(entry *data.SnapshotBrowserEntry) error {
	name := fmt.Sprintf("zfh-%s", time.Now().Format(time.DateTime))
	err := entry.Snapshot.ParentDataset.CreateSnapshot(name)
	if err != nil {
		return err
	}
	snapshotBrowser.Refresh(true)
	return nil
}

func (snapshotBrowser *SnapshotBrowserComponent) destroySnapshot(entry *data.SnapshotBrowserEntry, recursive bool) (err error) {
	snapshot := entry.Snapshot
	if recursive {
		err = snapshot.DestroyRecursive()
	} else {
		err = snapshot.Destroy()
	}
	if err != nil {
		return err
	}
	snapshotBrowser.Refresh(true)
	return nil
}
