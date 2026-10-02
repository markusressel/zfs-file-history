package dialog

import (
	"fmt"
	"os"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/state"
	"zfs-file-history/internal/ui/table"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/rivo/tview"
	"golang.org/x/term"
)

// createOverlayFrame puts content into a frame that fills nearly the whole screen, for the history overlays.
// The frame clears its inside (see clearInside) and follows terminal resizes.
func createOverlayFrame(title string, content tview.Primitive) *tview.Flex {
	termWidth, termHeight, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || termWidth <= 0 || termHeight <= 0 {
		termWidth = 100
		termHeight = 30
	}
	width := max(termWidth-4, 80)
	height := max(termHeight-2, 15)

	dialogFrame := tview.NewFlex()
	dialogFrame.SetBorder(true)
	uiutil.SetupDialogWindow(dialogFrame, title)
	clearInside(dialogFrame.Box)
	dialogFrame.AddItem(content, 0, 1, true)

	dialogContentColumnWrapper := tview.NewFlex()
	dialogContentColumnWrapper.AddItem(nil, 0, 1, false)

	dialogContentRowWrapper := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(dialogFrame, height, 1, true).
		AddItem(nil, 0, 1, false)

	dialogContentColumnWrapper.
		AddItem(dialogContentRowWrapper, width, 1, true).
		AddItem(nil, 0, 1, false)

	MakeFlexResizing(dialogContentColumnWrapper, dialogContentRowWrapper, dialogFrame, 99999, 80, 99999, 15)
	return dialogContentColumnWrapper
}

// snapshotsOfPath returns the snapshots of the dataset that contains path. The snapshots of cachedEntries (e.g.
// the ones the snapshot browser shows) are used if they belong to that dataset, otherwise they are loaded.
// Accesses ZFS, so it must not run on the UI thread.
func snapshotsOfPath(path string, cachedEntries []*data.SnapshotBrowserEntry) ([]*zfs.Snapshot, error) {
	dataset, err := zfs.FindHostDataset(path)
	if err != nil {
		return nil, fmt.Errorf("failed to find host dataset: %w", err)
	}
	if len(cachedEntries) > 0 && cachedEntries[0].Snapshot != nil && cachedEntries[0].Snapshot.ParentDataset != nil &&
		cachedEntries[0].Snapshot.ParentDataset.Path == dataset.Path {
		var snapshots []*zfs.Snapshot
		for _, entry := range cachedEntries {
			if entry != nil && entry.Snapshot != nil {
				snapshots = append(snapshots, entry.Snapshot)
			}
		}
		return snapshots, nil
	}
	snapshots, err := dataset.GetSnapshots()
	if err != nil {
		return nil, fmt.Errorf("failed to get snapshots for dataset %s: %w", dataset.Path, err)
	}
	return snapshots, nil
}

// Keys of the settings the dialogs remember in state.Current: the column layouts of their tables, and their modes.
const (
	stateKeyFileHistoryTable      = "fileHistory"
	stateKeyFolderHistoryTimeline = "folderHistory.timeline"
	stateKeyFolderHistoryChanges  = "folderHistory.changes"
	stateKeyPropertiesTable       = "properties"

	// toggleFileHistoryComparePrevious: compare versions to the previous version instead of the working copy
	toggleFileHistoryComparePrevious = "fileHistory.comparePrevious"
	// toggleFolderHistoryCompareNow: show the changes since a snapshot instead of the changes in it
	toggleFolderHistoryCompareNow = "folderHistory.compareNow"
)

// loadDiffMode returns the remembered mode: alternative if the toggle with the given key is on, else standard.
func loadDiffMode(key string, standard diffMode, alternative diffMode) diffMode {
	if state.Current.Toggle(key, false) {
		return alternative
	}
	return standard
}

// saveDiffMode remembers whether the alternative mode is used.
func saveDiffMode(key string, mode diffMode, alternative diffMode) {
	state.Current.SetToggle(key, mode == alternative)
}

// openColumnDialog lets the user select and order the columns of a table (F2), shown on the given pages.
// Must be called on the UI thread.
func openColumnDialog[T table.RowSelectionTableEntry](application *tview.Application, pages *tview.Pages, title string,
	allColumns []*table.Column, tableContainer *table.RowSelectionTable[T]) {
	if pages == nil {
		return
	}
	ShowDialogOnPages(application, pages, NewTableColumnSelectionDialog(application, title, allColumns, tableContainer), nil)
}
