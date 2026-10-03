package path_overview

import (
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/ui/theme"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fileInfo is a file or folder as returned by os.Lstat.
type fileInfo struct {
	size    int64
	modTime time.Time
	dir     bool
}

func (f fileInfo) Name() string       { return "x" }
func (f fileInfo) Size() int64        { return f.size }
func (f fileInfo) ModTime() time.Time { return f.modTime }
func (f fileInfo) IsDir() bool        { return f.dir }
func (f fileInfo) Sys() any           { return nil }
func (f fileInfo) Mode() fs.FileMode {
	if f.dir {
		return fs.ModeDir | 0o755
	}
	return 0o644
}

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
	return fileInfo{size: size, modTime: time.Date(2026, 9, modified, 0, 0, 0, 0, time.UTC)}
}

func folder(modified int) os.FileInfo {
	return fileInfo{size: 4096, modTime: time.Date(2026, 9, modified, 0, 0, 0, 0, time.UTC), dir: true}
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
		result = append(result, stripTags(part.text()))
	}
	return result
}

func stripTags(text string) string {
	view := tview.NewTextView().SetDynamicColors(true).SetText(text)
	return view.GetText(true)
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
		line := stripTags(fitParts(parts, test.width))
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
	assert.Contains(t, lines[3], "Selected  report.pdf · …")

	overview.SetVersions("pool/home", "/pool/home", folderPath,
		versions(folder(1), folder(1), folder(2), folder(2), folder(3)),
		entry, versions(nil, file(10, 1), file(20, 2), file(20, 2), file(40, 3)))
	lines, screen := render(t, overview, 120)
	for _, line := range lines {
		t.Log(line)
	}
	assert.Contains(t, lines[0], "Overview")
	// less important parts are left out to fit (here "12 unchanged" and the time of the last change)
	assert.Contains(t, lines[1], "Folder    pool/home/markus/docs · vs s3: +2 −1 ~4 · entries added/removed in 2 of 5 snapshots")
	// the graphs have their own lines, one dot column per snapshot: s1..s5
	assert.Contains(t, lines[2], "  changes ⣀⣇⡇")
	assert.Contains(t, lines[3], "Selected  report.pdf · 3 versions · last changed ")
	assert.Contains(t, lines[4], "  size    ⢀⣤⡇")

	// the selected snapshot (s3, in the second cell) is highlighted in both graphs
	graphX := 2 + labelWidth
	for _, y := range []int{3, 5} {
		for i := 0; i < 3; i++ {
			_, _, style, _ := screen.GetContent(graphX+i, y-1)
			_, background, _ := style.Decompose()
			assert.Equal(t, i == 1, background == theme.Colors.Sparkline.SelectedBackground, "row %d, cell %d", y-1, i)
		}
	}

	// narrow: nothing is cut off, the least important parts are left out
	lines, _ = render(t, overview, 50)
	for _, line := range lines {
		t.Log(line)
	}
	assert.Contains(t, lines[1], "Folder    …/home/markus/docs · vs s3: +2 −1 ~4 │")
	assert.Contains(t, lines[3], "Selected  report.pdf · 3 versions")

	// moving to another folder: loading again
	overview.SetFolder("/pool/home/markus/other")
	overview.SetEntry(nil)
	overview.SetSelectedSnapshot(nil)
	lines, _ = render(t, overview, 120)
	assert.Contains(t, lines[1], "/pool/home/markus/other · select a snapshot to compare")
	assert.Contains(t, lines[2], "…")
	assert.Contains(t, lines[3], "select a file or folder to see its history")
}
