package path_overview

import (
	"fmt"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/ui/clickable"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// section is the folder or the selected entry in the overview: two lines behind a label. The first line has three
// columns, each about one thing and compared with one thing, which line up between both sections, so a value is
// found at the same place every time: what it is now, how it compares to the selected snapshot, and its history
// across all snapshots. The second line has a graph below the first two columns, and more of the history.
type section struct {
	label string
	// now, vs and history are the columns of the first line, with style tags ("" for none)
	now, vs, history string
	// spans is whether now takes the whole line, without columns (e.g. "select a file"), and does not count for
	// their widths
	spans bool
	// history2 is the second line of the history column
	history2 string
	// historyLink and history2Link are the parts of history and history2 that can be clicked, nil: none
	historyLink, history2Link *textLink
	// legend is shown in the label column of the second line, in front of the graph
	legend string
	// graph returns the values of the graph and their snapshots (oldest first) and the index of the selected snapshot
	// (-1: none), nil: no graph. loading is true while they are read: "…" is shown instead.
	graph   func() (values []int64, snapshots []*zfs.Snapshot, selected int)
	loading bool
}

const (
	// columnSeparator is drawn between the columns of the first line
	columnSeparator = " │ "
	// minNowWidth is the width down to which the column "now" is shortened (e.g. a long name) before the other
	// columns are left out
	minNowWidth = 16
	// spinnerFrameDuration is how long each frame of the spinner is shown while the folder is read; the file browser
	// redraws as often meanwhile.
	spinnerFrameDuration = 100 * time.Millisecond
)

// nowMarker is shown behind the graphs: their timeline ends now.
const nowMarker = " → now"

func (overview *PathOverviewComponent) draw(screen tcell.Screen, x int, y int, width int, height int) {
	if width <= labelWidth || height <= 0 {
		return
	}
	sections := []section{overview.folderSection(), overview.entrySection()}
	if overview.grownFolder != overview.folderPath {
		overview.grownFolder, overview.grownWidths = overview.folderPath, [3]int{}
	}
	widths := columnWidths(sections, width-labelWidth, overview.grownWidths)
	overview.grownWidths = [3]int{0, max(overview.grownWidths[1], widths[1]), max(overview.grownWidths[2], widths[2])}
	for i, section := range sections {
		// each section takes two lines, the divider is between them (see overviewView.drawDivider)
		row := y + i*(dividerRow+1)
		if row >= y+height {
			return
		}
		overview.drawSection(screen, section, widths, x, row, width, row+1 < y+height)
	}
}

// columnWidths returns the widths of the columns now, vs and history for the space behind the labels, 0 for the ones
// that are left out. The comparison and the history are as wide as their widest line in both sections, and at least
// as wide as in grown (what they were before, so they do not move while the selection moves). The column "now" fills
// the rest, so the others are at the right edge. Where its text (e.g. a long name) does not fit, the others give back
// what they grew to first, and then it is shortened. Only if less than minNowWidth is left for it, the history is
// left out, then the comparison.
func columnWidths(sections []section, space int, grown [3]int) [3]int {
	var widths [3]int
	for _, section := range sections {
		if section.spans {
			continue
		}
		widths[0] = max(widths[0], taggedWidth(section.now))
		widths[1] = max(widths[1], taggedWidth(section.vs))
		widths[2] = max(widths[2], taggedWidth(section.history), taggedWidth(section.history2))
	}
	natural := widths
	for i := 1; i < len(widths); i++ {
		if widths[i] > 0 {
			widths[i] = max(widths[i], grown[i])
		}
	}
	separator := textWidth(columnSeparator)
	others := func() int {
		sum := 0
		for _, width := range widths[1:] {
			if width > 0 {
				sum += separator + width
			}
		}
		return sum
	}
	// the width they grew to is given back first where "now" does not fit (the comparison next to it first)
	for i := 1; i < len(widths); i++ {
		if over := natural[0] - (space - others()); over > 0 {
			widths[i] -= min(over, widths[i]-natural[i])
		}
	}
	for i := len(widths) - 1; i > 0 && space-others() < minNowWidth; i-- {
		widths[i] = 0
	}
	widths[0] = max(space-others(), 0)
	return widths
}

// drawSection draws a section at row y: the label and the columns, and below (if withSecondLine) the legend, the
// graph and the second line of the history.
func (overview *PathOverviewComponent) drawSection(screen tcell.Screen, section section, widths [3]int, x int, y int, width int, withSecondLine bool) {
	tview.Print(screen, section.label, x, y, labelWidth, tview.AlignLeft, theme.Colors.ShortcutMap.Name)
	if section.spans {
		printShortened(screen, section.now, x+labelWidth, y, width-labelWidth)
		return
	}
	columnX := x + labelWidth
	separator := textWidth(columnSeparator)
	historyX := -1
	for i, text := range []string{section.now, section.vs, section.history} {
		if widths[i] == 0 {
			continue
		}
		if i > 0 {
			tview.Print(screen, columnSeparator, columnX, y, separator, tview.AlignLeft, theme.Colors.Layout.Border)
			columnX += separator
		}
		if i == 2 {
			historyX = columnX
		}
		printShortened(screen, text, columnX, y, widths[i])
		columnX += widths[i]
	}
	if historyX >= 0 {
		overview.addLink(section.historyLink, historyX, y, widths[2])
	}
	if !withSecondLine {
		return
	}
	graphEnd := x + width
	if historyX >= 0 {
		tview.Print(screen, columnSeparator, historyX-separator, y+1, separator, tview.AlignLeft, theme.Colors.Layout.Border)
		printShortened(screen, section.history2, historyX, y+1, widths[2])
		overview.addLink(section.history2Link, historyX, y+1, widths[2])
		graphEnd = historyX - separator
	}
	switch {
	case section.loading:
		tview.Print(screen, "…", x+labelWidth, y+1, graphEnd-x-labelWidth, tview.AlignLeft, theme.Colors.Info.Muted)
	case section.graph != nil:
		values, snapshots, selected := section.graph()
		if len(values) == 0 {
			return
		}
		tview.Print(screen, section.legend, x, y+1, labelWidth, tview.AlignLeft, theme.Colors.ShortcutMap.Name)
		overview.drawTimeline(screen, x+labelWidth, y+1, graphEnd-x-labelWidth, values, snapshots, selected)
	}
}

// drawTimeline draws the sparkline of values stretched over the width (see stretch), followed by nowMarker. The graph
// comes first: the marker is left out if the graph needs the space (two snapshots per cell). The cells of the
// selected snapshot are highlighted, and the cells of each snapshot can be clicked to select it (see
// SetOnSelectSnapshot).
func (overview *PathOverviewComponent) drawTimeline(screen tcell.Screen, x int, y int, width int, values []int64, snapshots []*zfs.Snapshot, selected int) {
	markerWidth := textWidth(nowMarker)
	graphWidth := width
	// two values per cell
	if (len(values)+1)/2+markerWidth <= width {
		graphWidth = width - markerWidth
	}
	stretched, _ := stretch(values, -1, graphWidth)
	drawn := uiutil.DrawSparkline(screen, x, y, graphWidth, stretched, -1)
	spans := cellSpans(len(values), drawn)
	colors := theme.Colors.Sparkline
	for i, span := range spans {
		if i == selected {
			for cell := span.first; cell < span.first+span.width; cell++ {
				text, style, _ := screen.Get(x+cell, y)
				screen.Put(x+cell, y, text, style.Foreground(colors.Selected).Background(colors.SelectedBackground))
			}
		}
		if i < len(snapshots) && overview.onSelectSnapshot != nil {
			snapshot := snapshots[i]
			overview.regions.Add(clickable.Region{X: x + span.first, Y: y, Width: span.width, Key: snapshot.Name,
				HoverBackground: colors.HoverBackground, OnClick: func() { overview.onSelectSnapshot(snapshot) }})
		}
	}
	if remaining := width - drawn; remaining >= markerWidth {
		tview.Print(screen, nowMarker, x+drawn, y, remaining, tview.AlignLeft, theme.Colors.ShortcutMap.Name)
	}
}

// textLink is a part of a text in a column that can be clicked, e.g. the name of a snapshot.
type textLink struct {
	// offset and width are where the part is in the text, in cells
	offset, width int
	// key identifies what it stands for, see clickable.Region
	key     any
	onClick func()
}

// linkTo returns a link to the part of text, which is behind prefix (both as shown, without style tags), nil if
// onClick is nil.
func linkTo(prefix string, part string, key any, onClick func()) *textLink {
	if onClick == nil {
		return nil
	}
	return &textLink{offset: textWidth(prefix), width: textWidth(part), key: key, onClick: onClick}
}

// addLink adds the link (nil: none) of the text drawn at x, y in a column of width cells, as far as it is shown.
func (overview *PathOverviewComponent) addLink(link *textLink, x int, y int, width int) {
	if link == nil || link.offset >= width {
		return
	}
	overview.regions.Add(clickable.Region{X: x + link.offset, Y: y, Width: min(link.width, width-link.offset), Key: link.key,
		HoverBackground: theme.Colors.Pool.HoverBackground, OnClick: link.onClick})
}

// cellSpan are the cells of the sparkline that show a value: from first, width cells.
type cellSpan struct {
	first, width int
}

// cellSpans returns the cells of each of count values in a sparkline of cells cells drawn from the values stretched
// by stretch: whole cells each if they are fewer than the cells, else the cells their halves are in (a cell may show
// two values), or the cell they are merged into (see uiutil.DrawSparkline).
func cellSpans(count int, cells int) []cellSpan {
	spans := make([]cellSpan, count)
	if count == 0 || cells <= 0 {
		return spans
	}
	for i := range spans {
		switch {
		case count < cells:
			first := i * cells / count
			spans[i] = cellSpan{first: first, width: (i+1)*cells/count - first}
		case count < cells*2:
			start, end := i*cells*2/count, (i+1)*cells*2/count
			spans[i] = cellSpan{first: start / 2, width: (end+1)/2 - start/2}
		default:
			spans[i] = cellSpan{first: min(i*cells*2/count, cells*2-1) / 2, width: 1}
		}
	}
	return spans
}

// stretch spreads the values over cells cells of the sparkline (two values per cell), so they fill them exactly.
// Fewer values than cells take whole cells each: value i the cells from i*cells/len(values) to
// (i+1)*cells/len(values), so some take one cell more than others. Up to twice as many take the halves of cells the
// same way. More are returned as they are: the sparkline merges them. Returns the index of the selected value in the
// first half cell of its run.
func stretch(values []int64, selected int, cells int) ([]int64, int) {
	unit, units := 2, cells
	switch {
	case len(values) == 0 || len(values) >= cells*2:
		return values, selected
	case len(values) >= cells:
		// half cells
		unit, units = 1, cells*2
	}
	stretched := make([]int64, 0, cells*2)
	stretchedSelected := -1
	for i, value := range values {
		if i == selected {
			stretchedSelected = len(stretched)
		}
		for range ((i+1)*units/len(values) - i*units/len(values)) * unit {
			stretched = append(stretched, value)
		}
	}
	return stretched, stretchedSelected
}

// folderSection describes the folder: what it holds directly, how it compares to the selected snapshot, since when it
// is the same as now and its dataset, with how much each snapshot differs from now below. Not its path: the file
// browser above shows it in its title.
func (overview *PathOverviewComponent) folderSection() section {
	var summary *data.FolderSummary
	if overview.folderSummary != nil {
		folderSummary := overview.folderSummary()
		summary = &folderSummary
	}
	history, distances := overview.currentFolderHistory(), overview.currentDistances()
	result := section{
		label:   "Folder",
		now:     formatSummary(summary),
		vs:      overview.folderComparison(summary),
		legend:  "  vs now",
		loading: history == nil || distances == nil,
	}
	result.history, result.historyLink = overview.sameAsNowText(history, distances)
	result.history2, result.history2Link = overview.datasetText()
	if !result.loading {
		result.graph = func() ([]int64, []*zfs.Snapshot, int) {
			return distanceValues(history, distances), history.snapshots(), history.indexOf(overview.selectedSnapshot)
		}
	}
	return result
}

// entrySection describes the selected entry: its name and size, how it compares to the selected snapshot and its
// history, with its sizes (a file) or the snapshots in which it changed (a folder) below.
func (overview *PathOverviewComponent) entrySection() section {
	entry := overview.entry
	if entry == nil {
		return section{label: "Selected", now: uiutil.InfoMuted("select a file or folder to see its history"), spans: true}
	}
	isFolder := entry.Type == data.Directory
	history := overview.currentEntryHistory()
	result := section{label: "Selected", now: formatEntry(entry), vs: overview.entryComparison(entry), loading: history == nil}
	result.legend = "  size"
	if isFolder {
		result.legend = "  changes"
	}
	if history == nil {
		result.history = uiutil.InfoMuted("…")
		return result
	}
	result.history, result.history2 = historyTexts(history, isFolder)
	result.graph = func() ([]int64, []*zfs.Snapshot, int) {
		if isFolder {
			return history.activity(), history.snapshots(), history.indexOf(overview.selectedSnapshot)
		}
		return history.sizes(), history.snapshots(), history.indexOf(overview.selectedSnapshot)
	}
	return result
}

// formatSummary returns what the folder holds directly, e.g. "3.0 GiB  12 files  3 folders", a spinner while it is
// read, "" if unknown.
func formatSummary(summary *data.FolderSummary) string {
	switch {
	case summary == nil:
		return ""
	case summary.Loading:
		frame := uiutil.SpinnerFrames[int(time.Now().UnixMilli()/spinnerFrameDuration.Milliseconds())%len(uiutil.SpinnerFrames)]
		return txwidgets.Span(theme.Colors.Layout.Loading.Spinner, "%s", frame) + uiutil.InfoMuted(" reading the folder")
	}
	text := uiutil.InfoMuted("no files")
	if summary.Files > 0 {
		text = uiutil.InfoValue(uiutil.HumanizedBytes(uint64(summary.Size))) + "  " + count(summary.Files, "file", "files")
	}
	if summary.Folders > 0 {
		text += "  " + count(summary.Folders, "folder", "folders")
	}
	return text
}

// count returns e.g. "12 files": the number bright, the noun dim.
func count(n int, singular string, plural string) string {
	return uiutil.InfoValue(fmt.Sprint(n)) + uiutil.InfoMuted(" "+uiutil.Plural(n, singular, plural))
}

// folderComparison returns how the folder differs from the selected snapshot, e.g. "vs s3  +2 −1 ~4  +120 MiB".
func (overview *PathOverviewComponent) folderComparison(summary *data.FolderSummary) string {
	if overview.selectedSnapshot == nil {
		return uiutil.InfoMuted("no snapshot selected")
	}
	counts := overview.diffCounts()
	// not the entries still compared: the file browser shows them, and the column would stay as wide
	text := overview.vsLabel() + formatCounts(counts)
	if summary != nil && summary.InSnapshot && !summary.Loading && summary.Size != summary.SnapshotSize {
		text += "  " + uiutil.InfoMuted(formatSizeChange(summary.Size-summary.SnapshotSize))
	}
	return text
}

// entryComparison returns how the selected entry differs from the selected snapshot, e.g. "vs s3  modified".
func (overview *PathOverviewComponent) entryComparison(entry *data.FileBrowserEntry) string {
	if overview.selectedSnapshot == nil {
		return ""
	}
	colors := theme.Colors.FileBrowser.Table.State
	state := uiutil.InfoMuted("…")
	switch {
	case entry.IsLoading:
	case entry.DiffState == diff_state.Added:
		state = txwidgets.Span(colors.Added, "added")
	case entry.DiffState == diff_state.Deleted:
		state = txwidgets.Span(colors.Deleted, "deleted")
	case entry.DiffState == diff_state.Modified:
		state = txwidgets.Span(colors.Modified, "modified")
	case entry.DiffState == diff_state.Equal:
		state = uiutil.InfoMuted("same")
	}
	return overview.vsLabel() + state
}

// vsLabel returns the label of the comparison with the selected snapshot, e.g. "vs s3  ".
func (overview *PathOverviewComponent) vsLabel() string {
	return txwidgets.Span(theme.Colors.Info.Label, "vs ") + uiutil.InfoValue(overview.selectedSnapshot.Name) + "  "
}

// formatEntry returns the name of the entry and, for a file, its size now.
func formatEntry(entry *data.FileBrowserEntry) string {
	text := txwidgets.Span(theme.Colors.Layout.Table.Accent, "%s", entry.Name)
	if entry.Type != data.Directory && entry.HasReal() && entry.RealFile.Stat != nil {
		text += "  " + uiutil.InfoValue(uiutil.HumanizedBytes(uint64(entry.RealFile.Stat.Size())))
	}
	return text
}

// datasetText returns the dataset of the folder, e.g. "in rpool/home", whose snapshots are shown (a child dataset has
// others), "" while it is loading; and the link of its name, which shows it on the datasets page.
func (overview *PathOverviewComponent) datasetText() (string, *textLink) {
	if overview.datasetName == "" || overview.loadedFolderPath != overview.folderPath {
		return "", nil
	}
	name := overview.datasetName
	var onClick func()
	if overview.onShowDataset != nil {
		onClick = func() { overview.onShowDataset(name) }
	}
	return uiutil.InfoMuted("in ") + uiutil.InfoValue(name), linkTo("in ", name, "dataset:"+name, onClick)
}

// sameAsNowText returns since when the folder is the same as now, e.g. "unchanged since s4", "changed since s5" (the
// newest snapshot), "…" while it is loading; and the link of the snapshot named, which selects it.
func (overview *PathOverviewComponent) sameAsNowText(history *pathHistory, distances *folderDistances) (string, *textLink) {
	if history == nil || distances == nil {
		return uiutil.InfoMuted("…"), nil
	}
	total := len(history.versions)
	if total == 0 {
		return uiutil.InfoMuted("no snapshots"), nil
	}
	values := distanceValues(history, distances)
	since, always := sameAsNowSince(history, values)
	prefix, snapshot := "changed since ", history.versions[total-1].Snapshot
	switch {
	case always:
		return uiutil.InfoMuted("unchanged in all ") + count(total, "snapshot", "snapshots"), nil
	case since != nil:
		prefix, snapshot = "unchanged since ", since
	case values[total-1] < 0:
		prefix = "created after "
	}
	var onClick func()
	if overview.onSelectSnapshot != nil {
		onClick = func() { overview.onSelectSnapshot(snapshot) }
	}
	// the same key as in the graphs: hovered, the snapshot is highlighted there as well
	return uiutil.InfoMuted(prefix) + uiutil.InfoValue(snapshot.Name), linkTo(prefix, snapshot.Name, snapshot.Name, onClick)
}

// historyTexts describes the history of the selected entry in two lines, e.g. "3 versions in 4 of 5 snapshots" and
// "last changed 3 days ago".
func historyTexts(history *pathHistory, isFolder bool) (string, string) {
	total := len(history.versions)
	value := func(n int) string { return uiutil.InfoValue(fmt.Sprint(n)) }
	switch {
	case total == 0:
		return uiutil.InfoMuted("no snapshots"), ""
	case history.present == 0:
		return uiutil.InfoMuted("not in any of the ") + count(total, "snapshot", "snapshots"), ""
	case isFolder && history.changes == 0:
		return uiutil.InfoMuted("no changes in ") + count(total, "snapshot", "snapshots"), ""
	}
	last := ""
	if history.lastChange != nil {
		last = uiutil.InfoMuted("last changed ") + uiutil.InfoValue(uiutil.FormatTime(history.lastChange.Properties.CreationDate))
	}
	if isFolder {
		return count(history.changes, "change", "changes") + uiutil.InfoMuted(" in ") + count(total, "snapshot", "snapshots"), last
	}
	return count(history.distinct, "version", "versions") + uiutil.InfoMuted(" in ") + value(history.present) +
		uiutil.InfoMuted(" of ") + count(total, "snapshot", "snapshots"), last
}

// formatSizeChange returns how much the size changed, e.g. "+120 MiB", "−3.0 KiB".
func formatSizeChange(change int64) string {
	if change < 0 {
		return "−" + uiutil.HumanizedBytes(uint64(-change))
	}
	return "+" + uiutil.HumanizedBytes(uint64(change))
}

// formatCounts returns e.g. "+2 −1 ~4" (see uiutil.FormatChangeCounts), "same" (dimmed) without differences.
func formatCounts(counts diff_state.Counts) string {
	if text := uiutil.FormatChangeCounts(counts.Added, counts.Deleted, counts.Modified); text != "" {
		return text
	}
	return uiutil.InfoMuted("same")
}

// printShortened prints text with style tags at x, y, shortened with "…" if it is wider than width.
func printShortened(screen tcell.Screen, text string, x int, y int, width int) {
	if taggedWidth(text) > width && width > 0 {
		tview.Print(screen, text, x, y, width-1, tview.AlignLeft, theme.Colors.Info.Value)
		tview.Print(screen, "…", x+width-1, y, 1, tview.AlignLeft, theme.Colors.Info.Muted)
		return
	}
	tview.Print(screen, text, x, y, width, tview.AlignLeft, theme.Colors.Info.Value)
}

// taggedWidth returns the width of text with style tags in cells.
func taggedWidth(text string) int {
	return tview.TaggedStringWidth(text)
}

// textWidth returns the width of plain text in cells.
func textWidth(text string) int {
	return tview.TaggedStringWidth(tview.Escape(text))
}
