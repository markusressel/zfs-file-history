package path_overview

import (
	"path/filepath"
	"strings"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
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
}

// NewPathOverview creates the overview. diffCounts returns the states of the entries of the folder compared to
// the selected snapshot; it is called while drawing.
func NewPathOverview(diffCounts func() diff_state.Counts) *PathOverviewComponent {
	overview := &PathOverviewComponent{diffCounts: diffCounts}
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
	view.overview.draw(screen, x, y, width, height)
	if height > dividerRow {
		view.drawDivider(screen, y+dividerRow)
	}
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

func (overview *PathOverviewComponent) draw(screen tcell.Screen, x int, y int, width int, height int) {
	if width <= labelWidth || height <= 0 {
		return
	}
	textWidth := width - labelWidth
	lines := []struct {
		label string
		draw  func(y int)
	}{
		{"Folder", func(y int) { printFitted(screen, overview.folderParts(), x+labelWidth, y, textWidth) }},
		{"", func(y int) { overview.drawDistanceGraph(screen, x, y, width) }},
		// the divider, see overviewView.drawDivider
		{"", func(int) {}},
		{"Selected", func(y int) { printFitted(screen, overview.entryParts(), x+labelWidth, y, textWidth) }},
		{"", func(y int) {
			if overview.entry != nil {
				overview.drawGraph(screen, x, y, width, overview.currentEntryHistory(), overview.entry.Type == data.Directory)
			}
		}},
	}
	for i, line := range lines[:min(len(lines), height)] {
		if line.label != "" {
			tview.Print(screen, line.label, x, y+i, labelWidth, tview.AlignLeft, theme.Colors.ShortcutMap.Name)
		}
		line.draw(y + i)
	}
}

// folderParts returns the text about the folder: where it is, how it compares to the selected snapshot and in how
// many snapshots it changed.
func (overview *PathOverviewComponent) folderParts() []textPart {
	dim := theme.Colors.ShortcutMap.Name
	parts := []textPart{overview.folderNamePart()}
	if overview.selectedSnapshot == nil {
		parts = append(parts, textPart{styled: txwidgets.Span(dim, "select a snapshot to compare"), priority: 1})
	} else {
		counts := overview.diffCounts()
		parts = append(parts, textPart{styled: txwidgets.Span(dim, "vs %s: ", overview.selectedSnapshot.Name) + formatCounts(counts), priority: 1})
		if counts.Unknown > 0 {
			parts = append(parts, textPart{styled: txwidgets.Span(dim, "%d pending", counts.Unknown), priority: 2})
		}
		parts = append(parts, textPart{styled: txwidgets.Span(dim, "%d unchanged", counts.Equal), priority: 5})
	}
	return append(parts, overview.distanceParts()...)
}

// distanceParts describes since when the folder is the same as now.
func (overview *PathOverviewComponent) distanceParts() []textPart {
	history, distances := overview.currentFolderHistory(), overview.currentDistances()
	if history == nil || distances == nil {
		return nil
	}
	dim := func(format string, args ...any) []textPart {
		return []textPart{{styled: txwidgets.Span(theme.Colors.ShortcutMap.Name, format, args...), priority: 3}}
	}
	since, always := sameAsNowSince(history, distanceValues(history, distances))
	switch {
	case always:
		return dim("same as now in all snapshots")
	case since != nil:
		return dim("same as now since %s", since.Name)
	case history.present > 0:
		return dim("differs from now in the newest snapshot")
	}
	return nil
}

// drawDistanceGraph draws how many entries of the folder differ from now in each snapshot.
func (overview *PathOverviewComponent) drawDistanceGraph(screen tcell.Screen, x int, y int, width int) {
	dim := theme.Colors.ShortcutMap.Name
	history, distances := overview.currentFolderHistory(), overview.currentDistances()
	if history == nil || distances == nil {
		tview.Print(screen, "…", x+labelWidth, y, width-labelWidth, tview.AlignLeft, dim)
		return
	}
	if len(history.versions) == 0 {
		return
	}
	tview.Print(screen, "  differs", x, y, labelWidth, tview.AlignLeft, dim)
	// the timeline runs from the oldest snapshot towards now, which the marker behind it makes explicit. The graph
	// comes first: the marker is left out if the graph needs the space (two snapshots per cell)
	values := distanceValues(history, distances)
	drawn := uiutil.DrawSparkline(screen, x+labelWidth, y, width-labelWidth, values, history.indexOf(overview.selectedSnapshot))
	if remaining := width - labelWidth - drawn; remaining >= textWidth(nowMarker) {
		tview.Print(screen, nowMarker, x+labelWidth+drawn, y, remaining, tview.AlignLeft, dim)
	}
}

// nowMarker is shown behind the distance graph: its timeline ends now.
const nowMarker = " → now"

// entryParts returns the text about the selected entry: its name and its history.
func (overview *PathOverviewComponent) entryParts() []textPart {
	dim := theme.Colors.ShortcutMap.Name
	if overview.entry == nil {
		return []textPart{{styled: txwidgets.Span(dim, "select a file or folder to see its history")}}
	}
	parts := []textPart{{plain: overview.entry.Name, color: theme.Colors.Layout.Table.Accent}}
	history := overview.currentEntryHistory()
	if history == nil {
		return append(parts, textPart{styled: txwidgets.Span(dim, "…"), priority: 1})
	}
	return append(parts, historyParts(history, overview.entry.Type == data.Directory)...)
}

// folderNamePart returns the folder as the dataset name (dimmed) and the path within it, e.g. "rpool/home" and
// "markus/docs". Shortened from the left if there is not enough space.
func (overview *PathOverviewComponent) folderNamePart() textPart {
	accent := theme.Colors.Layout.Table.Accent
	part := textPart{plain: overview.folderPath, color: accent}
	if overview.datasetName == "" || overview.loadedFolderPath != overview.folderPath {
		return part
	}
	relative, err := filepath.Rel(overview.datasetPath, overview.folderPath)
	if err != nil || strings.HasPrefix(relative, "..") {
		return part
	}
	if relative == "." {
		part.plain = overview.datasetName
		return part
	}
	part.plain = overview.datasetName + "/" + relative
	part.styled = txwidgets.Span(theme.Colors.ShortcutMap.Name, "%s/", overview.datasetName) + txwidgets.Span(accent, "%s", relative)
	return part
}

// formatCounts returns e.g. "+2 −1 ~4" (see uiutil.FormatChangeCounts), "=" (dimmed) without differences.
func formatCounts(counts diff_state.Counts) string {
	if text := uiutil.FormatChangeCounts(counts.Added, counts.Deleted, counts.Modified); text != "" {
		return text
	}
	return txwidgets.Span(theme.Colors.ShortcutMap.Name, "=")
}

// historyParts describes a history, e.g. "5 versions", "last changed 3 days ago", "in 40 of 48 snapshots".
func historyParts(history *pathHistory, isFolder bool) []textPart {
	dim := func(priority int, format string, args ...any) textPart {
		return textPart{styled: txwidgets.Span(theme.Colors.ShortcutMap.Name, format, args...), priority: priority}
	}
	total := len(history.versions)
	snapshots := uiutil.Plural(total, "snapshot", "snapshots")
	switch {
	case total == 0:
		return []textPart{dim(1, "no snapshots")}
	case history.present == 0:
		return []textPart{dim(1, "not in any of the %d %s", total, snapshots)}
	case isFolder && history.changes == 0:
		return []textPart{dim(3, "no entries added/removed in %d %s", total, snapshots)}
	case isFolder:
		return []textPart{
			dim(3, "entries added/removed in %d of %d %s", history.changes, total, snapshots),
			dim(4, "last %s", uiutil.FormatTime(history.lastChange.Properties.CreationDate)),
		}
	}
	parts := []textPart{dim(1, "%d %s", history.distinct, uiutil.Plural(history.distinct, "version", "versions"))}
	if history.lastChange != nil {
		parts = append(parts, dim(2, "last changed %s", uiutil.FormatTime(history.lastChange.Properties.CreationDate)))
	}
	return append(parts, dim(3, "in %d of %d %s", history.present, total, snapshots))
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

// drawGraph draws the sparkline of a history behind a legend in the label column: the sizes of a file, or the
// snapshots in which a folder changed. The snapshot selected in the snapshot browser is highlighted.
func (overview *PathOverviewComponent) drawGraph(screen tcell.Screen, x int, y int, width int, history *pathHistory, isFolder bool) {
	dim := theme.Colors.ShortcutMap.Name
	legend, values := "  size", []int64(nil)
	if isFolder {
		legend = "  changes"
	}
	if history == nil {
		tview.Print(screen, "…", x+labelWidth, y, width-labelWidth, tview.AlignLeft, dim)
		return
	}
	if isFolder {
		values = history.activity()
	} else {
		values = history.sizes()
	}
	if len(values) == 0 {
		return
	}
	tview.Print(screen, legend, x, y, labelWidth, tview.AlignLeft, dim)
	uiutil.DrawSparkline(screen, x+labelWidth, y, width-labelWidth, values, history.indexOf(overview.selectedSnapshot))
}
