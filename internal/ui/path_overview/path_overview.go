package path_overview

import (
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/ui/clickable"
	"zfs-file-history/internal/ui/theme"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	// Height is the height of the overview, including its border: a text line and a graph line for the folder and
	// for the selected entry (so the graphs are never next to each other), separated by a divider.
	Height = 7
	// dividerRow is the row of the divider between the folder and the selected entry, within the border
	dividerRow = 2
	// labelWidth is the width of the labels in front of the lines ("Folder", "Selected")
	labelWidth = 10
)

// PathOverviewComponent shows the folder of the file browser and its selected entry across the snapshots:
// how the folder compares to the selected snapshot, how much it differs from now in each snapshot, and how the
// selected entry changed. The selected snapshot is highlighted in the sparklines. Everything comes from the
// snapshot browser (see snapshot_browser.PathVersionsLoaded and FolderChangesLoaded) and the file browser, the
// overview reads nothing itself. All methods run on the UI thread.
type PathOverviewComponent struct {
	view *overviewView
	// diffCounts returns the states of the entries of the folder compared to the selected snapshot
	diffCounts func() diff_state.Counts
	// folderSummary returns the files and folders of the folder and their size, nil: not shown, see
	// SetFolderSummaryFunc
	folderSummary func() data.FolderSummary

	folderPath       string
	entry            *data.FileBrowserEntry
	selectedSnapshot *zfs.Snapshot

	// the last loaded versions, see SetVersions
	datasetName       string
	datasetPath       string
	loadedFolderPath  string
	folderHistory     *pathHistory
	loadedEntryPath   string
	entryHistory      *pathHistory
	hasLoadedVersions bool

	// how the folder differs from now in each snapshot, see SetFolderChanges
	distances *folderDistances

	// grownWidths are the widths of the columns vs and history while the folder grownFolder is shown: they only grow,
	// so the columns do not move while the selection moves (see columnWidths)
	grownWidths [3]int
	grownFolder string

	// regions are the snapshots in the graphs, which can be clicked (see SetOnSelectSnapshot)
	regions clickable.Regions
	// onSelectSnapshot selects a snapshot in the snapshot browser, nil: the snapshots cannot be clicked
	onSelectSnapshot func(snapshot *zfs.Snapshot)
	// onShowDataset shows a dataset on the datasets page, nil: the name of the dataset cannot be clicked
	onShowDataset func(name string)
	// enabled returns whether the graphs can be clicked, e.g. not while a dialog is in front
	enabled func() bool
}

// NewPathOverview creates the overview. diffCounts returns the states of the entries of the folder compared to
// the selected snapshot; it is called while drawing.
func NewPathOverview(diffCounts func() diff_state.Counts) *PathOverviewComponent {
	overview := &PathOverviewComponent{diffCounts: diffCounts, enabled: func() bool { return true }}
	overview.view = &overviewView{Box: tview.NewBox(), overview: overview}
	overview.view.SetBorder(true)
	overview.view.SetBorderPadding(0, 0, 1, 1)
	uiutil.SetupWindow(overview.view.Box, "Overview")
	return overview
}

// overviewView draws the overview inside its border.
type overviewView struct {
	*tview.Box
	overview *PathOverviewComponent
}

func (view *overviewView) Draw(screen tcell.Screen) {
	// the border and the background; the inner rect is only known after this
	view.DrawForSubclass(screen, view)
	x, y, width, height := view.GetInnerRect()
	view.overview.regions.Reset()
	view.overview.draw(screen, x, y, width, height)
	if height > dividerRow {
		view.drawDivider(screen, y+dividerRow)
	}
	view.overview.regions.DrawHover(screen)
}

// MouseHandler selects the snapshot clicked in a graph, and does not focus the overview on a click: there are no keys
// in it, like Tab skips it.
func (view *overviewView) MouseHandler() uiutil.MouseHandler {
	return view.overview.regions.WrapMouseHandler(view, view.overview.enabled, uiutil.WithoutFocus(view.Box.MouseHandler()))
}

// drawDivider draws a horizontal line across the overview at row y, joined to its border ("├───┤").
func (view *overviewView) drawDivider(screen tcell.Screen, y int) {
	x, _, width, _ := view.GetRect()
	if width < 2 {
		return
	}
	style := tcell.StyleDefault.Background(tview.Styles.PrimitiveBackgroundColor).Foreground(theme.Colors.Layout.Border)
	screen.SetContent(x, y, tview.BoxDrawingsLightVerticalAndRight, nil, style)
	for column := x + 1; column < x+width-1; column++ {
		screen.SetContent(column, y, tview.BoxDrawingsLightHorizontal, nil, style)
	}
	screen.SetContent(x+width-1, y, tview.BoxDrawingsLightVerticalAndLeft, nil, style)
}

func (overview *PathOverviewComponent) GetLayout() tview.Primitive {
	return overview.view
}

// SetFolderSummaryFunc sets the function that returns the files and folders of the folder and the size of its files,
// now and in the selected snapshot; it is called while drawing.
func (overview *PathOverviewComponent) SetFolderSummaryFunc(folderSummary func() data.FolderSummary) {
	overview.folderSummary = folderSummary
}

// SetOnShowDataset sets the function that shows a dataset on the datasets page, called when the name of the dataset
// of the folder is clicked.
func (overview *PathOverviewComponent) SetOnShowDataset(onShowDataset func(name string)) {
	overview.onShowDataset = onShowDataset
}

// SetOnSelectSnapshot sets the function that selects a snapshot in the snapshot browser, called when it is clicked in
// a graph or in the history of the folder.
func (overview *PathOverviewComponent) SetOnSelectSnapshot(onSelectSnapshot func(snapshot *zfs.Snapshot)) {
	overview.onSelectSnapshot = onSelectSnapshot
}

// SetEnabledFunc sets a function that returns whether the graphs can be clicked, e.g. not while a dialog is in front
// (tview hands mouse events to all pages, see rivo/tview#926).
func (overview *PathOverviewComponent) SetEnabledFunc(enabled func() bool) {
	overview.enabled = enabled
}

// SetFolder sets the folder that is shown in the file browser.
func (overview *PathOverviewComponent) SetFolder(path string) {
	overview.folderPath = path
}

// SetEntry sets the selected entry of the file browser (nil: none, e.g. the header row).
func (overview *PathOverviewComponent) SetEntry(entry *data.FileBrowserEntry) {
	overview.entry = entry
}

// SetSelectedSnapshot sets the snapshot selected in the snapshot browser (nil: none).
func (overview *PathOverviewComponent) SetSelectedSnapshot(snapshot *zfs.Snapshot) {
	overview.selectedSnapshot = snapshot
}

// SetVersions sets the versions of a folder and of an entry in it (nil: none) in all snapshots of the dataset with
// the given name and mountpoint.
func (overview *PathOverviewComponent) SetVersions(datasetName string, datasetPath string, folderPath string, folder []data.PathVersion, entry *data.FileBrowserEntry, entryVersions []data.PathVersion) {
	overview.datasetName = datasetName
	overview.datasetPath = datasetPath
	overview.loadedFolderPath = folderPath
	overview.folderHistory = summarize(folder)
	overview.loadedEntryPath = ""
	overview.entryHistory = nil
	if entry != nil {
		overview.loadedEntryPath = entry.GetRealPath()
		overview.entryHistory = summarize(entryVersions)
	}
	overview.hasLoadedVersions = true
}

// currentFolderHistory returns the history of the folder that is shown, nil while it is loading.
func (overview *PathOverviewComponent) currentFolderHistory() *pathHistory {
	if !overview.hasLoadedVersions || overview.loadedFolderPath != overview.folderPath {
		return nil
	}
	return overview.folderHistory
}

// currentEntryHistory returns the history of the selected entry, nil while it is loading.
func (overview *PathOverviewComponent) currentEntryHistory() *pathHistory {
	if overview.entry == nil || overview.loadedEntryPath != overview.entry.GetRealPath() {
		return nil
	}
	return overview.entryHistory
}
