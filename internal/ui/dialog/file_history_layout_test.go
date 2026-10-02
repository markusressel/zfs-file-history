package dialog

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
	"zfs-file-history/internal/data"
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
	onUiThread(t, app, func() {
		overlay = NewFileHistoryOverlay(app, file, nil)
		ShowDialogOnPages(app, pages, overlay, nil)
	})

	screenLines := func() []string {
		var lines []string
		onUiThread(t, app, func() {
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
	assert.GreaterOrEqual(t, row(lines, "▲"), 0, "selected version")
	assert.Less(t, row(lines, "Presence"), row(lines, "Changes (Working Copy"), "the metadata is above the diff")
	assert.Equal(t, row(lines, "Snapshots"), row(lines, "Changes (Working Copy"), "both start on the same line")
	assert.Equal(t, -1, row(lines, "Metadata Comparison"), "no box in a box anymore")
	assert.Contains(t, lines[row(lines, "Presence")], "│ ", "divider left of the metadata")

	var sizes []int64
	onUiThread(t, app, func() { sizes = overlay.sizes })
	assert.Equal(t, []int64{4, 8}, sizes, "d1 and d2, oldest first")
}
