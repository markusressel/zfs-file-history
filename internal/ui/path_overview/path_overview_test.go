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
	return testutil.Folder(time.Date(2026, 9, modified, 0, 0, 0, 0, time.UTC))
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

// plainText returns the parts as shown, without style tags.
func plainText(parts []textPart) []string {
	var result []string
	for _, part := range parts {
		result = append(result, testutil.StripTags(part.text()))
	}
	return result
}

func TestHistoryParts(t *testing.T) {
	uiutil.InitTimeFormat()
	lastChange := uiutil.FormatTime(snapshots[4].Properties.CreationDate)

	assert.Equal(t, []string{"3 versions", "last changed " + lastChange, "in 5 of 5 snapshots"},
		plainText(historyParts(summarize(versions(file(1, 1), file(1, 1), file(2, 2), file(2, 2), file(3, 3))), false)))
	assert.Equal(t, []string{"1 version", "in 5 of 5 snapshots"},
		plainText(historyParts(summarize(versions(file(1, 1), file(1, 1), file(1, 1), file(1, 1), file(1, 1))), false)))
	assert.Equal(t, []string{"not in any of the 5 snapshots"}, plainText(historyParts(summarize(versions(nil, nil, nil, nil, nil)), false)))
	assert.Equal(t, []string{"entries added/removed in 1 of 5 snapshots", "last " + lastChange},
		plainText(historyParts(summarize(versions(folder(1), folder(1), folder(1), folder(1), folder(2))), true)))
	assert.Equal(t, []string{"no entries added/removed in 5 snapshots"},
		plainText(historyParts(summarize(versions(folder(1), folder(1), folder(1), folder(1), folder(1))), true)))
	assert.Equal(t, []string{"no snapshots"}, plainText(historyParts(summarize(nil), false)))
}

// Lines that are too long leave out their least important parts first, then shorten the path from the left.
func TestFitParts(t *testing.T) {
	parts := []textPart{
		{plain: "pool/home/markus/docs", color: tcell.ColorWhite},
		{styled: "vs s3: +2 −1 ~4", priority: 1},
		{styled: "12 unchanged", priority: 5},
		{styled: "in 4 of 5 snapshots", priority: 3},
	}
	full := "pool/home/markus/docs · vs s3: +2 −1 ~4 · 12 unchanged · in 4 of 5 snapshots"
	for _, test := range []struct {
		width    int
		expected string
	}{
		{100, full},
		{textWidth(full), full},
		{70, "pool/home/markus/docs · vs s3: +2 −1 ~4 · in 4 of 5 snapshots"},
		{45, "pool/home/markus/docs · vs s3: +2 −1 ~4"},
		// the path is shortened before the comparison is left out
		{35, "…home/markus/docs · vs s3: +2 −1 ~4"},
		{30, "…markus/docs · vs s3: +2 −1 ~4"},
		// not below minShortenedWidth: then the comparison is left out, and the whole path may fit again
		{25, "pool/home/markus/docs"},
		{15, "…me/markus/docs"},
		{5, "…docs"},
	} {
		line := testutil.StripTags(fitParts(parts, test.width))
		assert.Equal(t, test.expected, line, "width %d", test.width)
		assert.LessOrEqual(t, textWidth(line), test.width, "width %d", test.width)
	}
}

// render draws the overview on a simulation screen and returns its lines and the screen.
func render(t *testing.T, overview *PathOverviewComponent, width int) ([]string, tcell.SimulationScreen) {
	screen := tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, screen.Init())
	screen.SetSize(width, Height)
	overview.GetLayout().SetRect(0, 0, width, Height)
	overview.GetLayout().Draw(screen)
	screen.Show()
	cells, _, _ := screen.GetContents()
	var lines []string
	for y := 0; y < Height; y++ {
		var line strings.Builder
		for x := 0; x < width; x++ {
			if runes := cells[y*width+x].Runes; len(runes) > 0 {
				line.WriteRune(runes[0])
			}
		}
		lines = append(lines, strings.TrimRight(line.String(), " "))
	}
	return lines, screen
}

func TestPathOverview_Draw(t *testing.T) {
	uiutil.InitTimeFormat()
	counts := diff_state.Counts{Added: 2, Deleted: 1, Modified: 4, Equal: 12}
	overview := NewPathOverview(func() diff_state.Counts { return counts })
	folderPath := "/pool/home/markus/docs"
	entry := &data.FileBrowserEntry{Name: "report.pdf", Type: data.File, RealFile: &data.RealFile{Name: "report.pdf", Path: folderPath + "/report.pdf"}}
	overview.SetFolder(folderPath)
	overview.SetEntry(entry)
	overview.SetSelectedSnapshot(snapshots[2])

	// while the versions load
	lines, _ := render(t, overview, 120)
	assert.Contains(t, lines[1], "Folder    /pool/home/markus/docs · vs s3: +2 −1 ~4 · 12 unchanged")
	assert.Contains(t, lines[2], "…")
	assert.Contains(t, lines[4], "Selected  report.pdf · …")

	overview.SetVersions("pool/home", "/pool/home", folderPath,
		versions(folder(1), folder(1), folder(2), folder(2), folder(3)),
		entry, versions(nil, file(10, 1), file(20, 2), file(20, 2), file(40, 3)))
	// the distances are computed in the background (see TestPathOverview_Distances)
	lines, _ = render(t, overview, 120)
	assert.Contains(t, lines[2], "…")
	// 3 entries differ from now in s1, 2 in s2, 1 in s3, none in s4 and s5
	overview.distances = &folderDistances{folderPath: folderPath, byName: map[string]int{"s1": 3, "s2": 2, "s3": 1, "s4": 0, "s5": 0}}

	lines, screen := render(t, overview, 120)
	for _, line := range lines {
		t.Log(line)
	}
	assert.Contains(t, lines[0], "Overview")
	assert.Contains(t, lines[1], "Folder    pool/home/markus/docs · vs s3: +2 −1 ~4 · 12 unchanged · same as now since s4")
	// the graphs have their own lines, one dot column per snapshot: s1..s5
	assert.Contains(t, lines[2], "  differs ⣷⣄⡀ → now")
	assert.Contains(t, lines[4], "Selected  report.pdf · 3 versions · last changed ")
	assert.Contains(t, lines[5], "  size    ⢀⣤⡇")

	// the selected snapshot (s3, in the second cell) is highlighted in both graphs
	graphX := 2 + labelWidth
	for _, y := range []int{2, 5} {
		for i := 0; i < 3; i++ {
			_, style, _ := screen.Get(graphX+i, y)
			_, background, _ := style.Decompose()
			assert.Equal(t, i == 1, background == theme.Colors.Sparkline.SelectedBackground, "row %d, cell %d", y, i)
		}
	}
	// a divider between the folder and the selected entry, joined to the border
	assert.Equal(t, "├"+strings.Repeat("─", 118)+"┤", lines[3])

	// narrow: nothing is cut off, the least important parts are left out
	lines, _ = render(t, overview, 50)
	for _, line := range lines {
		t.Log(line)
	}
	assert.Contains(t, lines[1], "Folder    …/home/markus/docs · vs s3: +2 −1 ~4 │")
	assert.Contains(t, lines[4], "Selected  report.pdf · 3 versions")

	// moving to another folder: loading again
	overview.SetFolder("/pool/home/markus/other")
	overview.SetEntry(nil)
	overview.SetSelectedSnapshot(nil)
	lines, _ = render(t, overview, 120)
	assert.Contains(t, lines[1], "/pool/home/markus/other · select a snapshot to compare")
	assert.Contains(t, lines[2], "…")
	assert.Contains(t, lines[4], "select a file or folder to see its history")
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
	assert.NotContains(t, lines[2], "now")
	assert.Contains(t, lines[2], "differs")
}
