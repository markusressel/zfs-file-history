package dialog

import (
	"os"
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

func TestSparkline(t *testing.T) {
	line, column := sparkline([]int64{0, 1, 2, 3, 4, 5, 6, 7}, 20)
	assert.Equal(t, "▁▂▃▄▅▆▇█", string(line))
	assert.Equal(t, 3, column(3))

	// gaps for missing values, an all-zero line
	line, _ = sparkline([]int64{-1, 0, 0}, 10)
	assert.Equal(t, " ▁▁", string(line))

	// more values than columns: the maximum of each column
	line, column = sparkline([]int64{1, 8, 1, 1, 1, 1, 1, 8}, 4)
	assert.Equal(t, "█▁▁█", string(line))
	assert.Equal(t, 0, column(1))
	assert.Equal(t, 3, column(7))

	line, _ = sparkline(nil, 10)
	assert.Empty(t, line)
}

// folderHistoryTest shows the folder history of "docs" in a fake dataset, in a running application.
type folderHistoryTest struct {
	t       *testing.T
	ds      *fakeDataset
	app     *tview.Application
	screen  tcell.SimulationScreen
	pages   *tview.Pages
	overlay *FolderHistoryOverlay
}

// newFolderHistoryTest creates the snapshots:
//
//	d1: a.txt, b.txt          (folder created)
//	d2: a.txt, b.txt          (unchanged)
//	d3: b.txt (bigger), c/    (a.txt deleted, b.txt modified, c/ added)
//
// and a working copy with only c/: b.txt was deleted since d3.
// configure is applied to the overlay before it is shown, e.g. to select a snapshot.
func newFolderHistoryTest(t *testing.T, configure ...func(ds *fakeDataset, overlay *FolderHistoryOverlay)) *folderHistoryTest {
	ds := newFakeDataset(t)
	ds.addSnapshot("d1", day(1), map[string]string{"docs/a.txt": "a", "docs/b.txt": "b"})
	ds.addSnapshot("d2", day(2), map[string]string{"docs/a.txt": "a", "docs/b.txt": "b"})
	ds.addSnapshot("d3", day(3), map[string]string{"docs/b.txt": "bbbb", "docs/c/x": "x"})
	for _, name := range []string{"d1", "d2", "d3"} {
		// the folder itself has the same modification time in all snapshots
		require.NoError(t, os.Chtimes(filepath.Join(ds.root, ".zfs", "snapshot", name, "docs"), fileTime, fileTime))
	}
	ds.writeFile(ds.root, "docs/c/x", "x", fileTime)
	for _, name := range []string{"d3"} {
		require.NoError(t, os.Chtimes(filepath.Join(ds.root, ".zfs", "snapshot", name, "docs", "c"), fileTime, fileTime))
	}
	require.NoError(t, os.Chtimes(filepath.Join(ds.root, "docs", "c"), fileTime, fileTime))

	original := findSnapshotsOfPath
	t.Cleanup(func() { findSnapshotsOfPath = original })
	findSnapshotsOfPath = func(path string, cachedEntries []*data.SnapshotBrowserEntry) ([]*zfs.Snapshot, error) {
		return ds.snapshots, nil
	}

	ft := &folderHistoryTest{t: t, ds: ds}
	ft.app = tview.NewApplication()
	ft.screen = tcell.NewSimulationScreen("UTF-8")
	ft.app.SetScreen(ft.screen)
	ft.screen.SetSize(140, 40)
	ft.pages = tview.NewPages().AddPage("background", tview.NewBox(), true, true)
	ft.app.SetRoot(ft.pages, true)
	go func() { _ = ft.app.Run() }()
	t.Cleanup(ft.app.Stop)

	onUiThread(t, ft.app, func() {
		ft.overlay = NewFolderHistoryOverlay(ft.app, filepath.Join(ds.root, "docs"), nil)
		for _, f := range configure {
			f(ds, ft.overlay)
		}
		ShowDialogOnPages(ft.app, ft.pages, ft.overlay, nil)
	})
	ft.waitFor("history loaded", func() bool {
		loaded := false
		onUiThread(t, ft.app, func() { loaded = ft.overlay.history != nil })
		return loaded
	})
	return ft
}

func (ft *folderHistoryTest) waitFor(message string, condition func() bool) {
	require.Eventually(ft.t, condition, 3*time.Second, 10*time.Millisecond, message)
}

func (ft *folderHistoryTest) press(key tcell.Key, r rune) {
	ft.screen.InjectKey(key, r, tcell.ModNone)
}

func (ft *folderHistoryTest) screenText() string {
	var text strings.Builder
	onUiThread(ft.t, ft.app, func() {
		ft.app.ForceDraw()
		cells, width, height := ft.screen.GetContents()
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				if runes := cells[y*width+x].Runes; len(runes) > 0 {
					text.WriteRune(runes[0])
				}
			}
			text.WriteRune('\n')
		}
	})
	return text.String()
}

// changes returns the displayed changes as "<kind> <name>".
func (ft *folderHistoryTest) changes() []string {
	var result []string
	onUiThread(ft.t, ft.app, func() {
		for _, change := range ft.overlay.changes.GetEntries() {
			symbol, _, _ := ft.overlay.changeKindLabel(change.Kind)
			result = append(result, symbol+" "+displayName(newestEntry(change)))
		}
	})
	return result
}

func (ft *folderHistoryTest) timeline() []string {
	var result []string
	onUiThread(ft.t, ft.app, func() {
		for _, version := range ft.overlay.timeline.GetEntries() {
			result = append(result, version.Snapshot.Name)
		}
	})
	return result
}

func (ft *folderHistoryTest) hasPage(name string) bool {
	shown := false
	onUiThread(ft.t, ft.app, func() { shown = ft.overlay.pages.HasPage(name) })
	return shown
}

func (ft *folderHistoryTest) selectChange(name string) {
	onUiThread(ft.t, ft.app, func() {
		ft.app.SetFocus(ft.overlay.changes.GetLayout())
		for _, change := range ft.overlay.changes.GetEntries() {
			if change.Name == name {
				ft.overlay.changes.Select(change)
			}
		}
	})
}

func TestFolderHistoryOverlay_Timeline(t *testing.T) {
	ft := newFolderHistoryTest(t)

	assert.Equal(t, []string{"d3", "d1"}, ft.timeline(), "newest first, d2 did not change anything")
	// the newest snapshot is selected: its changes compared to d2
	assert.ElementsMatch(t, []string{"− a.txt", "+ c/", "~ b.txt"}, ft.changes())

	ft.waitFor("timeline, sparklines and footer", func() bool {
		text := ft.screenText()
		return strings.Contains(text, "+1 −1 ~1") && strings.Contains(text, "initial (2)") &&
			strings.Contains(text, "Items") && strings.Contains(text, "▲") &&
			strings.Contains(text, "1 added · 1 deleted · 1 modified") && strings.Contains(text, "2 of 3 snapshots")
	})
	// b.txt grew from "b" to "bbbb"
	assert.Contains(t, ft.screenText(), "1 B → 4 B")
}

// Opened from the snapshot browser: the version that was current in the snapshot is selected. d2 did not change
// anything, so it shows the version of d1.
func TestFolderHistoryOverlay_SelectSnapshot(t *testing.T) {
	for snapshot, expected := range map[string]string{"d1": "d1", "d2": "d1", "d3": "d3"} {
		t.Run(snapshot, func(t *testing.T) {
			ft := newFolderHistoryTest(t, func(ds *fakeDataset, overlay *FolderHistoryOverlay) {
				overlay.SelectSnapshot(ds.snapshot(snapshot))
			})
			var selected string
			onUiThread(t, ft.app, func() {
				selected = ft.overlay.timeline.GetSelectedEntry().Snapshot.Name
				assert.Same(t, ft.overlay.timeline.GetSelectedEntry(), ft.overlay.selected)
			})
			assert.Equal(t, expected, selected)
		})
	}
}

func TestFolderHistoryOverlay_ModeSinceSnapshot(t *testing.T) {
	ft := newFolderHistoryTest(t)

	ft.press(tcell.KeyRune, 'd')
	// since d3 until now: b.txt was deleted, c/ is unchanged
	ft.waitFor("changes since d3", func() bool {
		return assert.ObjectsAreEqual([]string{"− b.txt"}, ft.changes())
	})
	ft.waitFor("mode shown", func() bool { return strings.Contains(ft.screenText(), "since snapshot (vs. now)") })
}

func TestFolderHistoryOverlay_RestoreDeletedFile(t *testing.T) {
	ft := newFolderHistoryTest(t)
	ft.press(tcell.KeyRune, 'd')
	ft.waitFor("changes since d3", func() bool { return assert.ObjectsAreEqual([]string{"− b.txt"}, ft.changes()) })
	ft.selectChange("b.txt")

	ft.press(tcell.KeyEnter, 0)
	ft.waitFor("restore dialog", func() bool { return ft.hasPage(string(RestoreFileDialogPage)) })
	// 1: restore file
	ft.press(tcell.KeyRune, '1')
	ft.press(tcell.KeyEnter, 0)

	restored := filepath.Join(ft.ds.root, "docs", "b.txt")
	ft.waitFor("restored", func() bool {
		content, err := os.ReadFile(restored)
		return err == nil && string(content) == "bbbb"
	})
	// close the progress dialog
	ft.waitFor("progress dialog", func() bool { return ft.hasPage(string(RestoreFileProgress)) })
	ft.press(tcell.KeyEscape, 0)
	// the working copy is read again: b.txt is not "deleted since" anymore
	ft.waitFor("changes updated", func() bool {
		for _, change := range ft.changes() {
			if change == "− b.txt" {
				return false
			}
		}
		return true
	})
}

func TestFolderHistoryOverlay_NoRestoreOfEntriesNotInTheSnapshot(t *testing.T) {
	ft := newFolderHistoryTest(t)
	// a.txt was deleted in d3: restoring it "from d3" would delete it, which the overlay does not offer
	ft.selectChange("a.txt")
	ft.waitFor("hint", func() bool { return strings.Contains(ft.screenText(), "not in d3, nothing to restore") })

	ft.press(tcell.KeyEnter, 0)
	time.Sleep(100 * time.Millisecond)
	assert.False(t, ft.hasPage(string(RestoreFileDialogPage)))
}

func TestFolderHistoryOverlay_RestoreFolderAndEsc(t *testing.T) {
	ft := newFolderHistoryTest(t)

	// Enter on the timeline: restore the whole folder (directory only / recursively)
	ft.press(tcell.KeyEnter, 0)
	ft.waitFor("restore dialog", func() bool { return ft.hasPage(string(RestoreFileDialogPage)) })
	ft.waitFor("directory options", func() bool { return strings.Contains(ft.screenText(), "Restore directory recursively") })

	// Esc closes the restore dialog only
	ft.press(tcell.KeyEscape, 0)
	ft.waitFor("restore dialog closed", func() bool { return !ft.hasPage(string(RestoreFileDialogPage)) })
	onUiThread(t, ft.app, func() { assert.True(t, ft.pages.HasPage(string(FolderHistoryOverlayPage))) })

	ft.press(tcell.KeyEscape, 0)
	ft.waitFor("overlay closed", func() bool {
		shown := true
		onUiThread(t, ft.app, func() { shown = ft.pages.HasPage(string(FolderHistoryOverlayPage)) })
		return !shown
	})
}

func TestFolderHistoryOverlay_HistoryOfEntry(t *testing.T) {
	ft := newFolderHistoryTest(t)

	// a folder opens its own folder history
	ft.selectChange("c")
	ft.press(tcell.KeyRune, 'h')
	ft.waitFor("nested folder history", func() bool { return ft.hasPage(string(FolderHistoryOverlayPage)) })
	ft.waitFor("nested title", func() bool { return strings.Contains(ft.screenText(), "History of 'c/'") })
}

func TestFormatChangeCounts(t *testing.T) {
	colors := theme.Colors.FileBrowser.Table.State
	text := formatChangeCounts(2, 0, 1)

	assert.Contains(t, text, txwidgets.Span(colors.Added, "2 added"))
	assert.Contains(t, text, txwidgets.Span(theme.Colors.ShortcutMap.Name, "0 deleted"), "zero is not highlighted")
	assert.Contains(t, text, txwidgets.Span(colors.Modified, "1 modified"))
}

func TestDescribeChange(t *testing.T) {
	before := &folderEntry{Name: "b.txt", Type: folderEntryFile, Size: 1, ModTime: fileTime}
	after := &folderEntry{Name: "b.txt", Type: folderEntryFile, Size: 4096, ModTime: fileTime.Add(time.Hour)}
	dir := &folderEntry{Name: "c", Type: folderEntryDirectory, ModTime: fileTime}
	modified := fileTime.Format(theme.Style.Format.DateTime)
	later := fileTime.Add(time.Hour).Format(theme.Style.Format.DateTime)

	assert.Equal(t, "1 B, modified "+modified+" → 4.0 KiB, modified "+later,
		describeChange(&folderChange{Kind: folderChangeModified, Before: before, After: after}))
	assert.Equal(t, "was 1 B, modified "+modified, describeChange(&folderChange{Kind: folderChangeDeleted, Before: before}))
	assert.Equal(t, "modified "+modified, describeChange(&folderChange{Kind: folderChangeAdded, After: dir}), "no size for folders")
}

func TestFolderHistoryOverlay_DetailsAboveChanges(t *testing.T) {
	ft := newFolderHistoryTest(t)
	ft.selectChange("b.txt")

	ft.waitFor("details", func() bool { return strings.Contains(ft.screenText(), "b.txt modified") })
	lines := strings.Split(ft.screenText(), "\n")
	row := func(text string) int {
		for i, line := range lines {
			if strings.Contains(line, text) {
				return i
			}
		}
		return -1
	}
	assert.Less(t, row("b.txt modified"), row("Changes in d3"), "the details are above the changes")
	assert.Equal(t, row("Timeline"), row("Changes in d3"), "both tables start on the same line")
}

func TestFolderHistoryOverlay_DividerBetweenSparklinesAndDetails(t *testing.T) {
	ft := newFolderHistoryTest(t)
	ft.selectChange("b.txt")
	ft.waitFor("details", func() bool { return strings.Contains(ft.screenText(), "b.txt modified") })

	var detailsX, innerX, detailsHeight int
	var column []rune
	onUiThread(t, ft.app, func() {
		ft.app.ForceDraw()
		x, y, _, height := ft.overlay.details.GetRect()
		detailsX, detailsHeight = x, height
		innerX, _, _, _ = ft.overlay.details.GetInnerRect()
		for row := y; row < y+height; row++ {
			character, _, _, _ := ft.screen.GetContent(x, row)
			column = append(column, character)
		}
	})
	require.Equal(t, folderHistoryHeaderLines, detailsHeight)
	for row, character := range column {
		assert.Equal(t, tview.Borders.Vertical, character, "row %d of the details", row)
	}
	assert.Equal(t, detailsX+2, innerX, "the text starts right of the line, with a space")
}

func TestFolderHistoryOverlay_DragBoundary(t *testing.T) {
	ft := newFolderHistoryTest(t)
	var boundary, leftWidth int
	onUiThread(t, ft.app, func() {
		boundary, _, _, _ = ft.overlay.changes.GetLayout().GetRect()
		_, _, leftWidth, _ = ft.overlay.timeline.GetLayout().GetRect()
	})
	ft.waitFor("drawn", func() bool { return boundary > 0 })

	ft.screen.InjectMouse(boundary, 20, tcell.Button1, tcell.ModNone)
	time.Sleep(50 * time.Millisecond)
	ft.screen.InjectMouse(boundary+15, 20, tcell.Button1, tcell.ModNone)
	time.Sleep(50 * time.Millisecond)
	ft.screen.InjectMouse(boundary+15, 20, tcell.ButtonNone, tcell.ModNone)

	ft.waitFor("timeline wider", func() bool {
		width := 0
		onUiThread(t, ft.app, func() { _, _, width, _ = ft.overlay.timeline.GetLayout().GetRect() })
		return width >= leftWidth+10
	})
}

func TestFolderHistoryOverlay_NoDragWhileADialogIsShown(t *testing.T) {
	ft := newFolderHistoryTest(t)
	ft.press(tcell.KeyF2, 0)
	ft.waitFor("column dialog", func() bool { return ft.hasPage(string(ColumnSelectionDialogPage)) })
	var enabled bool
	onUiThread(t, ft.app, func() { enabled = ft.overlay.isMainPageInFront() })
	assert.False(t, enabled)
}
