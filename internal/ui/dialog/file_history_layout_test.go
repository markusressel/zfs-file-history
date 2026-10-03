package dialog

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCountDiffLines(t *testing.T) {
	added, removed := countDiffLines("@@ -1,2 +1,3 @@\n one\n-two\n+zwei\n+drei\n")
	assert.Equal(t, 2, added)
	assert.Equal(t, 1, removed)
}

func TestFormatDiffLineCounts(t *testing.T) {
	colors := theme.Colors.FileBrowser.Table.State
	text := formatDiffLineCounts(1, 0)
	assert.Contains(t, text, txwidgets.Span(colors.Added, "+1 line"))
	assert.Contains(t, text, txwidgets.Span(theme.Colors.ShortcutMap.Name, "−0 lines"), "zero is not highlighted")
}

func TestFileHistoryOverlay_Layout(t *testing.T) {
	if !DiffBinExists() {
		t.Skip("needs diff")
	}
	ds := newFakeDataset(t)
	ds.addSnapshot("d1", day(1), map[string]string{"notes.txt": "one\n"})
	ds.addSnapshot("d2", day(2), map[string]string{"notes.txt": "one\ntwo\n"})
	ds.writeFile(ds.root, "notes.txt", "one\ntwo\nthree\n", fileTime.Add(time.Hour))
	original := findSnapshotsOfPath
	t.Cleanup(func() { findSnapshotsOfPath = original })
	findSnapshotsOfPath = func(string, []*data.SnapshotBrowserEntry) ([]*zfs.Snapshot, error) { return ds.snapshots, nil }

	app := tview.NewApplication()
	screen := tcell.NewSimulationScreen("UTF-8")
	app.SetScreen(screen)
	screen.SetSize(140, 30)
	pages := tview.NewPages().AddPage("background", tview.NewBox(), true, true)
	app.SetRoot(pages, true)
	go func() { _ = app.Run() }()
	t.Cleanup(app.Stop)

	path := filepath.Join(ds.root, "notes.txt")
	file := &data.FileBrowserEntry{Name: "notes.txt", Type: data.File, RealFile: &data.RealFile{Name: "notes.txt", Path: path}}
	var overlay *FileHistoryOverlay
	testutil.OnUiThread(t, app, func() {
		overlay = NewFileHistoryOverlay(app, file, nil)
		ShowDialogOnPages(app, pages, overlay, nil)
	})

	screenLines := func() []string {
		var lines []string
		testutil.OnUiThread(t, app, func() {
			app.ForceDraw()
			cells, width, height := screen.GetContents()
			for y := 0; y < height; y++ {
				var line strings.Builder
				for x := 0; x < width; x++ {
					if runes := cells[y*width+x].Runes; len(runes) > 0 {
						line.WriteRune(runes[0])
					}
				}
				lines = append(lines, line.String())
			}
		})
		return lines
	}
	row := func(lines []string, text string) int {
		for i, line := range lines {
			if strings.Contains(line, text) {
				return i
			}
		}
		return -1
	}

	// the newest version (d2) compared to the working copy (from the working copy to the snapshot): the working
	// copy has one more line, so it is a removed line
	var lines []string
	require.Eventually(t, func() bool {
		lines = screenLines()
		return row(lines, "+0 lines · −1 line") >= 0
	}, 3*time.Second, 10*time.Millisecond, "diff with line counts")

	assert.GreaterOrEqual(t, row(lines, "Size"), 0, "size sparkline")
	// the shortcuts of the focused list (shortcut names use non-breaking spaces)
	assert.GreaterOrEqual(t, row(lines, "[T]:\u00a0Time\u00a0format"), 0, "time format shortcut")
	assert.GreaterOrEqual(t, row(lines, "[F2]:\u00a0Columns"), 0, "columns shortcut")
	_, highlighted := sparklineRow(t, app, screen, "Size")
	assert.True(t, highlighted, "the selected version is highlighted in the sparkline")
	assert.Less(t, row(lines, "Presence"), row(lines, "Changes (Working Copy"), "the metadata is above the diff")
	assert.Equal(t, row(lines, "Snapshots"), row(lines, "Changes (Working Copy"), "both start on the same line")
	assert.Equal(t, -1, row(lines, "Metadata Comparison"), "no box in a box anymore")
	assert.Contains(t, lines[row(lines, "Presence")], "│ ", "divider left of the metadata")

	var sizes []int64
	testutil.OnUiThread(t, app, func() { sizes = overlay.sizes })
	assert.Equal(t, []int64{4, 8}, sizes, "d1 and d2, oldest first")

	// the boundary between the versions and the changes can be dragged
	var boundary, leftWidth int
	testutil.OnUiThread(t, app, func() {
		boundary, _, _, _ = overlay.rightLayoutContainer.GetRect()
		_, _, leftWidth, _ = overlay.tableContainer.GetLayout().GetRect()
	})
	screen.InjectMouse(boundary, 20, tcell.Button1, tcell.ModNone)
	time.Sleep(50 * time.Millisecond)
	screen.InjectMouse(boundary+15, 20, tcell.Button1, tcell.ModNone)
	time.Sleep(50 * time.Millisecond)
	screen.InjectMouse(boundary+15, 20, tcell.ButtonNone, tcell.ModNone)
	require.Eventually(t, func() bool {
		width := 0
		testutil.OnUiThread(t, app, func() { _, _, width, _ = overlay.tableContainer.GetLayout().GetRect() })
		return width >= leftWidth+10
	}, 3*time.Second, 10*time.Millisecond, "versions wider")
}

// Opened from the snapshot browser: the version of the file that was current in the snapshot is selected. The
// history only lists the snapshots in which the file changed, so d2 shows the version of d1.
func TestFileHistoryOverlay_SelectSnapshot(t *testing.T) {
	for snapshot, expected := range map[string]string{"d1": "d1", "d2": "d1", "d3": "d3"} {
		t.Run(snapshot, func(t *testing.T) {
			ds := newFakeDataset(t)
			ds.addSnapshot("d1", day(1), map[string]string{"notes.txt": "one\n"})
			ds.addSnapshot("d2", day(2), map[string]string{"notes.txt": "one\n"})
			ds.addSnapshot("d3", day(3), map[string]string{"notes.txt": "one\ntwo\n"})
			ds.writeFile(ds.root, "notes.txt", "one\ntwo\n", fileTime)
			original := findSnapshotsOfPath
			t.Cleanup(func() { findSnapshotsOfPath = original })
			findSnapshotsOfPath = func(string, []*data.SnapshotBrowserEntry) ([]*zfs.Snapshot, error) { return ds.snapshots, nil }

			app := tview.NewApplication()
			app.SetScreen(tcell.NewSimulationScreen("UTF-8"))
			pages := tview.NewPages().AddPage("background", tview.NewBox(), true, true)
			app.SetRoot(pages, true)
			go func() { _ = app.Run() }()
			t.Cleanup(app.Stop)

			path := filepath.Join(ds.root, "notes.txt")
			file := &data.FileBrowserEntry{Name: "notes.txt", Type: data.File, RealFile: &data.RealFile{Name: "notes.txt", Path: path}}
			var overlay *FileHistoryOverlay
			testutil.OnUiThread(t, app, func() {
				overlay = NewFileHistoryOverlay(app, file, nil).SelectSnapshot(ds.snapshot(snapshot))
				ShowDialogOnPages(app, pages, overlay, nil)
			})

			var versions []string
			var selected, current string
			require.Eventually(t, func() bool {
				testutil.OnUiThread(t, app, func() {
					versions = nil
					for _, entry := range overlay.historyEntries {
						versions = append(versions, entry.Snapshot.Name)
					}
					if entry := overlay.tableContainer.GetSelectedEntry(); entry != nil {
						selected = entry.Snapshot.Name
					}
					if overlay.currentSelection != nil {
						current = overlay.currentSelection.Snapshot.Name
					}
				})
				return len(versions) > 0
			}, 3*time.Second, 10*time.Millisecond, "history loaded")
			assert.Equal(t, []string{"d3", "d1"}, versions, "d2 did not change the file")
			assert.Equal(t, expected, selected)
			assert.Equal(t, expected, current)
		})
	}
}
