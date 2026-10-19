package path_overview

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/folder_listing"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/theme"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	dataset   = &zfs.Dataset{Path: "/pool/home"}
	snapshots = []*zfs.Snapshot{snapshot("s1", 1), snapshot("s2", 2), snapshot("s3", 3), snapshot("s4", 4), snapshot("s5", 5)}
)

func snapshot(name string, day int) *zfs.Snapshot {
	return &zfs.Snapshot{Name: name, ParentDataset: dataset, Properties: zfs.SnapshotProperties{
		CreationDate: time.Date(2026, 10, day, 0, 0, 0, 0, time.UTC),
	}}
}

func file(size int64, modified int) os.FileInfo {
	return testutil.File(size, time.Date(2026, 9, modified, 0, 0, 0, 0, time.UTC))
}

func folder(modified int) os.FileInfo {
	return testutil.Folder(5, time.Date(2026, 9, modified, 0, 0, 0, 0, time.UTC))
}

// versions returns the versions in s1..s5 (nil: not in the snapshot), in reverse order: summarize sorts them.
func versions(infos ...os.FileInfo) []data.PathVersion {
	var result []data.PathVersion
	for i := len(infos) - 1; i >= 0; i-- {
		result = append(result, data.PathVersion{Snapshot: snapshots[i], Info: infos[i]})
	}
	return result
}

func TestSummarize(t *testing.T) {
	tests := []struct {
		name       string
		versions   []data.PathVersion
		present    int
		changes    int
		distinct   int
		first      string
		lastChange string
		changed    []bool
	}{
		{"unchanged", versions(file(1, 1), file(1, 1), file(1, 1), file(1, 1), file(1, 1)), 5, 0, 1, "s1", "", []bool{false, false, false, false, false}},
		{"modified twice", versions(file(1, 1), file(1, 1), file(2, 2), file(2, 2), file(3, 3)), 5, 2, 3, "s1", "s5", []bool{false, false, true, false, true}},
		{"same size, newer", versions(file(1, 1), file(1, 2), file(1, 2), file(1, 2), file(1, 2)), 5, 1, 2, "s1", "s2", []bool{false, true, false, false, false}},
		{"created later", versions(nil, nil, file(1, 1), file(1, 1), file(1, 1)), 3, 1, 1, "s3", "s3", []bool{false, false, true, false, false}},
		{"deleted and recreated", versions(file(1, 1), nil, file(1, 1), file(1, 1), nil), 3, 3, 2, "s1", "s5", []bool{false, true, true, false, true}},
		{"never", versions(nil, nil, nil, nil, nil), 0, 0, 0, "", "", []bool{false, false, false, false, false}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			history := summarize(test.versions)
			assert.Equal(t, "s1", history.versions[0].Snapshot.Name, "oldest first")
			assert.Equal(t, test.present, history.present)
			assert.Equal(t, test.changes, history.changes)
			assert.Equal(t, test.distinct, history.distinct)
			assert.Equal(t, test.changed, history.changed)
			name := func(snapshot *zfs.Snapshot) string {
				if snapshot == nil {
					return ""
				}
				return snapshot.Name
			}
			assert.Equal(t, test.first, name(history.first))
			assert.Equal(t, test.lastChange, name(history.lastChange))
		})
	}
}

func TestSparklineValues(t *testing.T) {
	history := summarize(versions(nil, file(10, 1), file(20, 2), file(20, 2), nil))
	assert.Equal(t, []int64{-1, 10, 20, 20, -1}, history.sizes())

	history = summarize(versions(nil, folder(1), folder(1), folder(2), folder(2)))
	assert.Equal(t, []int64{-1, 1, 0, 1, 0}, history.activity(), "1 where entries were added or removed")
}

// render draws the overview on a simulation screen and returns its lines and the screen.
func render(t *testing.T, overview *PathOverviewComponent, width int) ([]string, tcell.SimulationScreen) {
	screen := testutil.DrawToScreen(t, overview.GetLayout(), width, Height)
	lines := testutil.ScreenLines(screen)
	for y, line := range lines {
		lines[y] = strings.TrimRight(line, " ")
	}
	return lines, screen
}

// newDrawnOverview returns an overview of a folder with an added, a deleted and 4 modified entries compared to s3,
// 12 files and 3 folders, with report.pdf selected, its versions and distances loaded.
func newDrawnOverview(t *testing.T) (*PathOverviewComponent, *data.FileBrowserEntry, *data.FolderSummary) {
	uiutil.InitTimeFormat()
	counts := diff_state.Counts{Added: 2, Deleted: 1, Modified: 4, Equal: 12}
	overview := NewPathOverview(func() diff_state.Counts { return counts })
	summary := &data.FolderSummary{Files: 12, Folders: 3, Size: 3 << 30, SnapshotSize: 3<<30 - 120<<20, InSnapshot: true}
	overview.SetFolderSummaryFunc(func() data.FolderSummary { return *summary })
	folderPath := "/pool/home/markus/docs"
	entry := &data.FileBrowserEntry{Name: "report.pdf", Type: data.File, DiffState: diff_state.Modified,
		RealFile: &data.RealFile{Name: "report.pdf", Path: folderPath + "/report.pdf", Stat: file(42<<20, 3)}}
	overview.SetFolder(folderPath)
	overview.SetEntry(entry)
	overview.SetSelectedSnapshot(snapshots[2])
	return overview, entry, summary
}

// loadVersions loads the versions of the folder and of report.pdf; 3 entries differ from now in s1, 2 in s2, 1 in
// s3, none in s4 and s5.
func loadVersions(overview *PathOverviewComponent, entry *data.FileBrowserEntry) {
	overview.SetVersions("pool/home", "/pool/home", overview.folderPath,
		versions(folder(1), folder(1), folder(2), folder(2), folder(3)),
		entry, versions(nil, file(10, 1), file(20, 2), file(20, 2), file(40, 3)))
	// computed in the background (see TestPathOverview_SetFolderChanges)
	overview.distances = &folderDistances{folderPath: overview.folderPath, byName: map[string]int{"s1": 3, "s2": 2, "s3": 1, "s4": 0, "s5": 0}}
}

// The overview has a section for the folder and one for the selected entry, each with the same three columns: what
// it is now, how it compares to the selected snapshot and its history, with a graph below.
func TestPathOverview_Draw(t *testing.T) {
	overview, entry, _ := newDrawnOverview(t)

	// while the versions load
	lines, _ := render(t, overview, 120)
	for _, line := range lines {
		t.Log(line)
	}
	assert.Regexp(t, `^│ Folder    3\.0 GiB  12 files  3 folders +│ vs s3  \+2 −1 ~4  \+120 MiB │ … │$`, lines[1])
	assert.Contains(t, lines[2], "          …")
	assert.Regexp(t, `^│ Selected  report\.pdf  42 MiB +│ vs s3  modified           │ … │$`, lines[4])

	loadVersions(overview, entry)
	lines, screen := render(t, overview, 120)
	for _, line := range lines {
		t.Log(line)
	}
	assert.Contains(t, lines[0], "Overview")
	// the first column fills the rest: the others are at the right edge
	assert.Regexp(t, `^│ Folder    3\.0 GiB  12 files  3 folders +│ vs s3  \+2 −1 ~4  \+120 MiB │ unchanged since s4 +│$`, lines[1])
	assert.Contains(t, lines[2], "  vs now  ")
	assert.Contains(t, lines[2], " → now │ in pool/home")
	assert.Regexp(t, `^│ Selected  report\.pdf  42 MiB +│ vs s3  modified           │ 3 versions in 4 of 5 snapshots +│$`, lines[4])
	assert.Contains(t, lines[5], "  size    ")
	assert.Contains(t, lines[5], " → now │ last changed ")
	// a divider between the folder and the selected entry, joined to the border
	assert.Equal(t, "├"+strings.Repeat("─", 118)+"┤", lines[3])

	// Regression: only one cell of the selected snapshot (s3) was highlighted, not all of its cells in the stretched
	// graphs
	graphX := 2 + labelWidth
	for _, y := range []int{2, 5} {
		var highlighted []int
		for column := graphX; column < 120; column++ {
			_, style, _ := screen.Get(column, y)
			if _, background, _ := style.Decompose(); background == theme.Colors.Sparkline.SelectedBackground {
				highlighted = append(highlighted, column-graphX)
			}
		}
		require.NotEmpty(t, highlighted, "row %d", y)
		width := len(highlighted)
		assert.Greater(t, width, 1, "row %d: all cells of s3", y)
		assert.Equal(t, highlighted[len(highlighted)-1]-highlighted[0]+1, width, "row %d: one run", y)
		assert.InDelta(t, 2*width, highlighted[0], float64(width), "row %d: the third of five", y)
	}

	// moving to another folder: loading again
	overview.SetFolder("/pool/home/markus/other")
	overview.SetEntry(nil)
	overview.SetSelectedSnapshot(nil)
	lines, _ = render(t, overview, 120)
	assert.Contains(t, lines[1], "no snapshot selected")
	assert.Contains(t, lines[4], "select a file or folder to see its history")
}

// The comparison and the history keep their place while the selection moves: their widths only grow while the folder
// is shown, and start again in another folder.
func TestPathOverview_ColumnsDoNotMove(t *testing.T) {
	overview, entry, _ := newDrawnOverview(t)
	loadVersions(overview, entry)
	historyColumn := func() int {
		lines, _ := render(t, overview, 120)
		return strings.Index(lines[1], "│ unchanged since")
	}
	before := historyColumn()

	// a file with a short history and a long name
	other := &data.FileBrowserEntry{Name: strings.Repeat("long-name-", 5) + ".txt", Type: data.File, DiffState: diff_state.Equal}
	overview.SetEntry(other)
	overview.SetVersions("pool/home", "/pool/home", overview.folderPath,
		versions(folder(1), folder(1), folder(2), folder(2), folder(3)), other, versions(nil, nil, nil, nil, file(1, 1)))
	assert.Equal(t, before, historyColumn(), "the history column of the longest history stays")

	overview.SetFolder("/pool/home/markus/other")
	overview.SetVersions("pool/home", "/pool/home", overview.folderPath, versions(folder(1), folder(1), folder(1), folder(1), folder(1)), nil, nil)
	overview.distances = &folderDistances{folderPath: overview.folderPath, byName: map[string]int{"s1": 0, "s2": 0, "s3": 0, "s4": 0, "s5": 0}}
	overview.SetEntry(nil)
	lines, _ := render(t, overview, 120)
	assert.Regexp(t, `│ unchanged in all 5 snapshots │$`, lines[1], "as wide as the texts of the other folder")
}

// Regression: the comparison kept the width of a longer text shown before (e.g. "3 pending" while the states were
// compared), while the name of the selected file next to it was shortened: "demo-pools-large.g…".
func TestPathOverview_NameTakesTheWidthGrownBefore(t *testing.T) {
	overview, _, summary := newDrawnOverview(t)
	long := &zfs.Snapshot{Name: "2026-10-18-190000", ParentDataset: dataset, Properties: zfs.SnapshotProperties{CreationDate: time.Date(2026, 10, 18, 19, 0, 0, 0, time.UTC)}}
	overview.SetSelectedSnapshot(long)
	entry := &data.FileBrowserEntry{Name: "demo-pools-large.gif", Type: data.File, DiffState: diff_state.Equal,
		RealFile: &data.RealFile{Name: "demo-pools-large.gif", Path: overview.folderPath + "/demo-pools-large.gif", Stat: file(12<<20, 3)}}
	overview.SetEntry(entry)
	overview.SetVersions("pool/home", "/pool/home", overview.folderPath, versions(folder(1), folder(1), folder(1), folder(1), folder(1)),
		entry, versions(nil, file(1, 1), file(1, 1), file(1, 1), file(1, 1)))
	overview.distances = &folderDistances{folderPath: overview.folderPath, byName: map[string]int{"s1": 0, "s2": 0, "s3": 0, "s4": 0, "s5": 0}}
	// the comparison of the folder is wide: with the change of the size
	render(t, overview, 115)
	summary.SnapshotSize = summary.Size

	lines, _ := render(t, overview, 115)
	assert.Contains(t, lines[4], "Selected  demo-pools-large.gif  12 MiB │ vs 2026-10-18-190000  same")
	assert.Contains(t, lines[4], "│ 1 version in 4 of 5 snapshots")
}

// Regression: with nothing selected, "select a file or folder to see its history" was shortened to the first column.
func TestPathOverview_NothingSelectedTakesTheLine(t *testing.T) {
	overview, entry, _ := newDrawnOverview(t)
	loadVersions(overview, entry)
	overview.SetEntry(nil)
	lines, _ := render(t, overview, 90)
	assert.Regexp(t, `^│ Selected  select a file or folder to see its history +│$`, lines[4], "no columns")
}

// A click on a snapshot in a graph selects it, the snapshot below the mouse is highlighted in both graphs.
func TestPathOverview_ClickSelectsSnapshot(t *testing.T) {
	overview, entry, _ := newDrawnOverview(t)
	loadVersions(overview, entry)
	var selected []string
	overview.SetOnSelectSnapshot(func(snapshot *zfs.Snapshot) { selected = append(selected, snapshot.Name) })
	_, screen := render(t, overview, 120)
	mouse := func(action tview.MouseAction, x int, y int) {
		overview.GetLayout().MouseHandler()(action, tcell.NewEventMouse(x, y, tcell.Button1, tcell.ModNone), func(tview.Primitive) {})
	}

	// the first cells of the graph of the folder are s1
	graphX := 2 + labelWidth
	mouse(tview.MouseMove, graphX, 2)
	screen = testutil.DrawToScreen(t, overview.GetLayout(), 120, Height)
	hovered := func(y int) bool {
		_, style, _ := screen.Get(graphX, y)
		_, background, _ := style.Decompose()
		return background == theme.Colors.Sparkline.HoverBackground
	}
	assert.True(t, hovered(2), "in the graph of the folder")
	mouse(tview.MouseLeftClick, graphX, 2)
	assert.Equal(t, []string{"s1"}, selected)

	// the last cells of the graph of report.pdf are s5
	lines := testutil.ScreenLines(screen)
	end := strings.Index(lines[5], " → now")
	require.Positive(t, end)
	mouse(tview.MouseLeftClick, len([]rune(lines[5][:end]))-1, 5)
	assert.Equal(t, []string{"s1", "s5"}, selected)

	// not while a dialog is in front
	overview.SetEnabledFunc(func() bool { return false })
	mouse(tview.MouseLeftClick, graphX, 2)
	assert.Equal(t, []string{"s1", "s5"}, selected)
}

// The snapshot in the history of the folder ("unchanged since s4") selects it when clicked, the name of the dataset
// shows it on the datasets page.
func TestPathOverview_HistoryLinks(t *testing.T) {
	overview, entry, _ := newDrawnOverview(t)
	loadVersions(overview, entry)
	var clicked []string
	overview.SetOnSelectSnapshot(func(snapshot *zfs.Snapshot) { clicked = append(clicked, "snapshot "+snapshot.Name) })
	overview.SetOnShowDataset(func(name string) { clicked = append(clicked, "dataset "+name) })
	_, screen := render(t, overview, 120)
	click := func(text string, offset int) {
		x, y, found := testutil.FindInScreen(screen, text)
		require.True(t, found, text)
		overview.GetLayout().MouseHandler()(tview.MouseLeftClick, tcell.NewEventMouse(x+offset, y, tcell.Button1, tcell.ModNone), func(tview.Primitive) {})
	}

	click("unchanged since s4", len("unchanged since "))
	click("in pool/home", len("in "))
	assert.Equal(t, []string{"snapshot s4", "dataset pool/home"}, clicked)
	// not the words in front of them
	click("unchanged since s4", 0)
	click("in pool/home", 0)
	assert.Len(t, clicked, 2)

	// hovered, the snapshot is highlighted in the graphs as well
	x, y, _ := testutil.FindInScreen(screen, "unchanged since s4")
	overview.GetLayout().MouseHandler()(tview.MouseMove, tcell.NewEventMouse(x+len("unchanged since "), y, tcell.ButtonNone, tcell.ModNone), func(tview.Primitive) {})
	screen = testutil.DrawToScreen(t, overview.GetLayout(), 120, Height)
	hovered := 0
	for column := 2 + labelWidth; column < 120; column++ {
		_, style, _ := screen.Get(column, 2)
		if _, background, _ := style.Decompose(); background == theme.Colors.Sparkline.HoverBackground {
			hovered++
		}
	}
	assert.Positive(t, hovered, "s4 in the graph of the folder")
}

// Narrow: the columns on the right are left out first, the first one is shortened.
func TestPathOverview_Narrow(t *testing.T) {
	overview, entry, _ := newDrawnOverview(t)
	loadVersions(overview, entry)

	lines, _ := render(t, overview, 80)
	for _, line := range lines {
		t.Log(line)
	}
	assert.Regexp(t, `^│ Folder    3\.0 GiB  12 files  3 folders +│ vs s3  \+2 −1 ~4  \+120 MiB │$`, lines[1])
	assert.NotContains(t, lines[1], "unchanged")
	assert.Contains(t, lines[2], " → now")

	lines, _ = render(t, overview, 40)
	for _, line := range lines {
		t.Log(line)
	}
	assert.Contains(t, lines[1], "Folder    3.0 GiB  12 files  3 fold…")
	assert.Contains(t, lines[4], "Selected  report.pdf  42 MiB")
}

// The folder section tells what the folder holds directly, and how much the size of its files changed since the
// selected snapshot.
func TestPathOverview_FolderSummary(t *testing.T) {
	overview, _, summary := newDrawnOverview(t)

	summary.SnapshotSize = summary.Size + 2048
	lines, _ := render(t, overview, 140)
	assert.Contains(t, lines[1], "vs s3  +2 −1 ~4  −2.0 KiB")

	summary.SnapshotSize = summary.Size
	lines, _ = render(t, overview, 140)
	assert.Contains(t, lines[1], "vs s3  +2 −1 ~4 ", "no change of the size")
	assert.NotContains(t, lines[1], "KiB")

	*summary = data.FolderSummary{Folders: 1}
	lines, _ = render(t, overview, 140)
	assert.Contains(t, lines[1], "Folder    no files  1 folder")

	// while the folder is read: a spinner in place of the sizes of the last load, and no change of the size
	*summary = data.FolderSummary{Files: 12, Size: 3 << 30, SnapshotSize: 1 << 30, InSnapshot: true, Loading: true}
	lines, screen := render(t, overview, 140)
	assert.Regexp(t, `Folder    [`+strings.Join(uiutil.SpinnerFrames, "")+`] reading the folder +│ vs s3  \+2 −1 ~4 +│`, lines[1])
	_, style, _ := screen.Get(2+labelWidth, 1)
	foreground, _, _ := style.Decompose()
	assert.Equal(t, theme.Colors.Layout.Loading.Spinner, foreground)
}

// Since when the folder is the same as now, from how many entries differ from now in each snapshot.
func TestSameAsNowText(t *testing.T) {
	history := summarize(versions(folder(1), folder(1), folder(1), folder(1), folder(1)))
	text := func(byName map[string]int) string {
		text, _ := (&PathOverviewComponent{}).sameAsNowText(history, &folderDistances{byName: byName})
		return testutil.StripTags(text)
	}
	assert.Equal(t, "unchanged since s4", text(map[string]int{"s1": 3, "s2": 2, "s3": 1, "s4": 0, "s5": 0}))
	assert.Equal(t, "unchanged in all 5 snapshots", text(map[string]int{"s1": 0, "s2": 0, "s3": 0, "s4": 0, "s5": 0}))
	assert.Equal(t, "changed since s5", text(map[string]int{"s1": 0, "s2": 0, "s3": 0, "s4": 0, "s5": 1}))
	assert.Equal(t, "created after s5", text(map[string]int{"s1": 0, "s2": 0}), "not in the newest snapshot")
	loading, link := (&PathOverviewComponent{}).sameAsNowText(history, nil)
	assert.Equal(t, "…", testutil.StripTags(loading))
	assert.Nil(t, link)
}

func TestHistoryTexts(t *testing.T) {
	uiutil.InitTimeFormat()
	lastChange := uiutil.FormatTime(snapshots[4].Properties.CreationDate)
	texts := func(history *pathHistory, isFolder bool) []string {
		first, second := historyTexts(history, isFolder)
		return []string{testutil.StripTags(first), testutil.StripTags(second)}
	}
	assert.Equal(t, []string{"3 versions in 5 of 5 snapshots", "last changed " + lastChange},
		texts(summarize(versions(file(1, 1), file(1, 1), file(2, 2), file(2, 2), file(3, 3))), false))
	assert.Equal(t, []string{"1 version in 5 of 5 snapshots", ""},
		texts(summarize(versions(file(1, 1), file(1, 1), file(1, 1), file(1, 1), file(1, 1))), false))
	assert.Equal(t, []string{"not in any of the 5 snapshots", ""}, texts(summarize(versions(nil, nil, nil, nil, nil)), false))
	assert.Equal(t, []string{"1 change in 5 snapshots", "last changed " + lastChange},
		texts(summarize(versions(folder(1), folder(1), folder(1), folder(1), folder(2))), true))
	assert.Equal(t, []string{"no changes in 5 snapshots", ""},
		texts(summarize(versions(folder(1), folder(1), folder(1), folder(1), folder(1))), true))
	assert.Equal(t, []string{"no snapshots", ""}, texts(summarize(nil), false))
}

func TestStretch(t *testing.T) {
	values, selected := stretch([]int64{1, 2, 3}, 1, 6)
	assert.Equal(t, []int64{1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3}, values, "whole cells: two values per cell")
	assert.Equal(t, 4, selected, "the first of its cells")

	// Regression: each value was repeated as often as all of them fit, rounded down, so cells at the end stayed
	// empty: 5 values in 51 cells took 50.
	values, _ = stretch([]int64{1, 2, 3, 4, 5}, -1, 51)
	assert.Len(t, values, 102, "all cells")
	values, selected = stretch([]int64{1, 2, 3}, 2, 7)
	assert.Equal(t, []int64{1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3, 3, 3}, values)
	assert.Equal(t, 8, selected)

	values, selected = stretch([]int64{1, 2, 3}, 1, 2)
	assert.Equal(t, []int64{1, 2, 3, 3}, values, "half cells: more values than cells")
	assert.Equal(t, 1, selected)
	values, _ = stretch(make([]int64, 60), -1, 40)
	assert.Len(t, values, 80, "all cells, not 30")

	values, selected = stretch([]int64{1, 2, 3, 4}, -1, 2)
	assert.Equal(t, []int64{1, 2, 3, 4}, values, "no room to stretch")
	assert.Equal(t, -1, selected)
}

func TestSameAsNowSince(t *testing.T) {
	history := summarize(versions(folder(1), folder(1), folder(1), folder(1), folder(1)))
	name := func(snapshot *zfs.Snapshot) string {
		if snapshot == nil {
			return ""
		}
		return snapshot.Name
	}
	for _, test := range []struct {
		values []int64
		since  string
		always bool
	}{
		{[]int64{3, 2, 1, 0, 0}, "s4", false},
		{[]int64{0, 0, 0, 0, 0}, "s1", true},
		{[]int64{0, 0, 0, 0, 1}, "", false},
		// changed back: only the last run of zeros counts
		{[]int64{0, 1, 0, 2, 0}, "s5", false},
		// the folder is not in the newest snapshot
		{[]int64{0, 0, 0, 0, -1}, "", false},
	} {
		since, always := sameAsNowSince(history, test.values)
		assert.Equal(t, test.since, name(since), "%v", test.values)
		assert.Equal(t, test.always, always, "%v", test.values)
	}
}

// The distances come from the folder changes of the snapshot browser: the entries that differ from now.
func TestPathOverview_SetFolderChanges(t *testing.T) {
	folderPath := "/pool/home/markus/docs"
	overview := NewPathOverview(func() diff_state.Counts { return diff_state.Counts{} })
	overview.SetFolder(folderPath)
	overview.SetFolderChanges(folderPath, map[string]folder_listing.SnapshotChanges{
		"s1": {Exists: false, Initial: true},
		"s2": {Exists: true, VsNow: folder_listing.Counts{Added: 1, Modified: 2}, VsPrevious: folder_listing.Counts{Added: 3}},
		"s3": {Exists: true},
	})
	require.NotNil(t, overview.currentDistances())
	assert.Equal(t, map[string]int{"s2": 3, "s3": 0}, overview.currentDistances().byName, "s1 does not contain the folder")

	// of another folder: not shown
	overview.SetFolder("/pool/home/markus/other")
	assert.Nil(t, overview.currentDistances())
}

// The distance graph ends with "→ now", unless the graph needs the space: then the graph comes first.
func TestPathOverview_NowMarker(t *testing.T) {
	var many []data.PathVersion
	distances := map[string]int{}
	for i := 0; i < 40; i++ {
		snapshot := &zfs.Snapshot{Name: fmt.Sprintf("s%02d", i), ParentDataset: dataset,
			Properties: zfs.SnapshotProperties{CreationDate: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i)}}
		many = append(many, data.PathVersion{Snapshot: snapshot, Info: folder(1)})
		distances[snapshot.Name] = 40 - i
	}
	folderPath := "/pool/home/docs"
	overview := NewPathOverview(func() diff_state.Counts { return diff_state.Counts{} })
	overview.SetFolder(folderPath)
	overview.SetVersions("pool/home", "/pool/home", folderPath, many, nil, nil)
	overview.distances = &folderDistances{folderPath: folderPath, byName: distances}

	// 40 snapshots: 20 cells, and room for the marker
	lines, _ := render(t, overview, 60)
	assert.Contains(t, lines[2], " → now")
	// only room for 16 cells after the label: the graph uses all of it (merging snapshots), no marker
	lines, _ = render(t, overview, 2+labelWidth+16+2)
	assert.NotContains(t, lines[2], "→ now")
	assert.Contains(t, lines[2], "vs now")
}

// Regression: a click into the overview focused it, which Tab skips: there is nothing to do in it.
func TestPathOverview_ClickDoesNotFocus(t *testing.T) {
	overview := NewPathOverview(func() diff_state.Counts { return diff_state.Counts{} })
	overview.GetLayout().SetRect(0, 0, 40, 8)
	focused := false
	overview.GetLayout().MouseHandler()(tview.MouseLeftDown, tcell.NewEventMouse(5, 3, tcell.Button1, tcell.ModNone),
		func(tview.Primitive) { focused = true })
	assert.False(t, focused)
}
