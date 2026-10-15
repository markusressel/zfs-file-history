package dialog

import (
	"errors"
	"sync"
	"testing"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	liveError     = zfs.DataError{Pool: "rpool", Entry: "/home/b", Dataset: "rpool/home", Path: "/b", LivePath: "/home/b"}
	snapshotError = zfs.DataError{Pool: "rpool", Entry: "rpool/home@daily:/a", Dataset: "rpool/home", Snapshot: "daily", Path: "/a", LivePath: "/home/a"}
	unmounted     = zfs.DataError{Pool: "rpool", Entry: "rpool/data:/c", Dataset: "rpool/data", Path: "/c"}
	metadata      = zfs.DataError{Pool: "rpool", Entry: "<metadata>:<0x1b>"}
	located       = zfs.DataError{Pool: "rpool", Entry: "rpool/home:<0x20>", Dataset: "rpool/home", Object: 0x20, Searched: true,
		Locations: []zfs.ObjectLocation{{Snapshot: "new", Path: "/x", LivePath: "/home/x"}, {Snapshot: "old", Path: "/x", LivePath: "/home/x"}}}
	gone      = zfs.DataError{Pool: "rpool", Entry: "rpool/home:<0x10>", Dataset: "rpool/home", Object: 0x10, Searched: true}
	otherLive = zfs.DataError{Pool: "backup", Entry: "/backup/a", Dataset: "backup", Path: "/a", LivePath: "/backup/a"}
)

func TestSortedDataErrors(t *testing.T) {
	index := zfs.NewDataErrorIndex([]zfs.DataError{metadata, snapshotError, unmounted, liveError, otherLive}, nil)

	// the files in the file system first, then the ones in snapshots (or not mounted), then the unknown ones
	assert.Equal(t, []zfs.DataError{otherLive, liveError, unmounted, snapshotError, metadata}, sortedDataErrors(index))
	assert.Nil(t, sortedDataErrors(nil))
}

func TestFormatDataErrorLocation(t *testing.T) {
	tests := []struct {
		dataError zfs.DataError
		text      string
		color     tcell.Color
	}{
		{liveError, "file system", theme.Colors.Alert.Error},
		{snapshotError, "@daily", theme.Colors.Alert.Warning},
		{unmounted, "not mounted", theme.Colors.Alert.Unknown},
		{metadata, "unknown", theme.Colors.Alert.Unknown},
		{located, "@new +1", theme.Colors.Alert.Warning},
		{gone, "no snapshot", theme.Colors.Alert.Unknown},
	}
	for _, test := range tests {
		text, color := formatDataErrorLocation(test.dataError)
		assert.Equal(t, test.text, text, test.dataError.Entry)
		assert.Equal(t, test.color, color, test.dataError.Entry)
	}
}

func TestFormatDataErrorFile(t *testing.T) {
	assert.Equal(t, "/home/b", formatDataErrorFile(liveError))
	assert.Equal(t, "/c", formatDataErrorFile(unmounted))
	assert.Equal(t, "/home/x  (rpool/home:<0x20>, by inode)", formatDataErrorFile(located))
	assert.Equal(t, "rpool/home:<0x10>", formatDataErrorFile(gone))
	assert.Equal(t, "<metadata>:<0x1b>", formatDataErrorFile(metadata))
}

func TestFormatObjectSearch(t *testing.T) {
	progress := zfs.ObjectSearchProgress{Snapshot: "daily", Done: 2, Total: 31, Entries: 120000, Found: 2, Wanted: 13}
	assert.Equal(t, "Searching @daily (3 of 31 snapshots, 120000 entries): found 2 of 13 objects", formatObjectSearch(progress, true, nil))
	assert.Equal(t, "Searching the snapshots…", formatObjectSearch(zfs.ObjectSearchProgress{}, true, nil))
	progress.Snapshot, progress.Done = "", 31
	assert.Equal(t, "Searched 31 snapshots: found 2 of 13 objects. The others are probably deleted everywhere (or in "+
		"folders you cannot read): then their blocks are freed, and the errors go away with the next scrubs.",
		formatObjectSearch(progress, false, nil))
	assert.Equal(t, "The search failed: boom", formatObjectSearch(progress, false, errors.New("boom")))
}

func TestWrappedHeight(t *testing.T) {
	assert.Equal(t, 1, wrappedHeight("short", 10))
	assert.Equal(t, 2, wrappedHeight("one two three", 8))
	assert.Equal(t, 3, wrappedHeight("a\n\nb", 10), "empty lines count")
}

func TestFormatDataErrorsHelp(t *testing.T) {
	assert.Equal(t, dataErrorsHelp, formatDataErrorsHelp(nil, false))
	help := formatDataErrorsHelp(zfs.NewDataErrorIndex(nil, []string{"rpool", "tank"}), false)
	assert.Contains(t, help, "The damaged files of rpool, tank can only be listed by root.")
	assert.Contains(t, help, zfs.SetupDataErrorsHint)
	help = formatDataErrorsHelp(zfs.NewDataErrorIndex(nil, []string{"rpool"}), true)
	assert.Contains(t, help, "Press s to allow your user to list them.")

	index := zfs.NewDataErrorIndex(nil, nil)
	index.WithoutHeadErrlog = []string{"rpool"}
	help = formatDataErrorsHelp(index, false)
	assert.Contains(t, help, "rpool reports an error only for the dataset or snapshot whose read failed")
	assert.Contains(t, help, "cannot be undone")
	assert.Contains(t, help, "sudo zpool set feature@head_errlog=enabled rpool")
}

func TestDataErrorsDialog_FindAndUpdate(t *testing.T) {
	app, _ := testutil.NewApp()
	finds := 0
	index := zfs.NewDataErrorIndex([]zfs.DataError{liveError, gone, metadata}, nil)
	closes := 0
	dialog := NewDataErrorsDialog(app, index, nil, func() { finds++ }, func() { closes++ })
	dialog.table.Select(2, 0)

	dialog.layout.GetInputCapture()(tcell.NewEventKey(tcell.KeyRune, 'f', tcell.ModNone))
	assert.Equal(t, 1, finds)

	// found: the row shows where, the selection stays
	dialog.SetIndex(zfs.NewDataErrorIndex([]zfs.DataError{liveError, located, metadata}, nil))
	row, _ := dialog.table.GetSelection()
	assert.Equal(t, 2, row)
	assert.Equal(t, "@new +1", dialog.table.GetCell(2, 1).Text)
	dialog.SetObjectSearch(zfs.ObjectSearchProgress{Total: 2, Done: 2, Found: 1, Wanted: 1}, false, nil)
	assert.Equal(t, "Searched 2 snapshots: found 1 of 1 object.", dialog.searchView.GetText(true))

	// e.g. stops the search
	dialog.layout.GetInputCapture()(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	assert.Equal(t, 1, closes)

	// without a search, no key
	dialog = NewDataErrorsDialog(app, index, nil, nil, nil)
	event := tcell.NewEventKey(tcell.KeyRune, 'f', tcell.ModNone)
	assert.Equal(t, event, dialog.layout.GetInputCapture()(event))
}

func TestDataErrorsDialog_Setup(t *testing.T) {
	app, _ := testutil.NewApp()
	setups := 0
	key := tcell.NewEventKey(tcell.KeyRune, 's', tcell.ModNone)

	// the list is complete: nothing to allow
	dialog := NewDataErrorsDialog(app, zfs.NewDataErrorIndex([]zfs.DataError{liveError}, nil), nil, nil, nil)
	dialog.SetOnSetup(func() { setups++ })
	assert.Equal(t, key, dialog.layout.GetInputCapture()(key))
	assert.NotContains(t, dialog.hintView.GetText(true), "s: allow")

	// needs root
	dialog.SetIndex(zfs.NewDataErrorIndex(nil, []string{"rpool"}))
	assert.Contains(t, dialog.hintView.GetText(true), "s: allow listing them")
	assert.Nil(t, dialog.layout.GetInputCapture()(key))
	assert.Equal(t, 1, setups)
}

func TestDataErrorsDialog_ShowsFile(t *testing.T) {
	app, screen := testutil.NewApp()
	screen.SetSize(140, 40)
	pages := tview.NewPages().AddPage("background", tview.NewBox(), true, true)
	testutil.StartApp(t, app, pages)

	var mu sync.Mutex
	var shown []string
	index := zfs.NewDataErrorIndex([]zfs.DataError{metadata, liveError}, nil)
	var dialog *DataErrorsDialog
	testutil.OnUiThread(t, app, func() {
		dialog = NewDataErrorsDialog(app, index, func(dataError zfs.DataError) {
			mu.Lock()
			defer mu.Unlock()
			shown = append(shown, dataError.LivePath)
		}, nil, nil)
		ShowDialogOnPages(app, pages, dialog, nil)
	})

	testutil.OnUiThread(t, app, func() {
		assert.Equal(t, 3, dialog.table.GetRowCount(), "a header and one row per error")
		assert.Equal(t, "/home/b", dialog.table.GetCell(1, 2).Text)
		assert.Equal(t, "<metadata>:<0x1b>", testutil.StripTags(dialog.table.GetCell(2, 2).Text))
		// an object without a path cannot be shown
		dialog.showRow(2)
	})
	testutil.OnUiThread(t, app, func() { dialog.showRow(1) })
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(shown) == 1
	}, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, []string{"/home/b"}, shown)
	require.Eventually(t, func() bool {
		closed := false
		testutil.OnUiThread(t, app, func() { closed = !pages.HasPage(string(DataErrorsDialogPage)) })
		return closed
	}, 2*time.Second, 10*time.Millisecond)
}

func TestFileHistoryOverlay_MarksDamagedVersions(t *testing.T) {
	app, _ := testutil.NewApp()
	file := &data.FileBrowserEntry{Name: "a", RealFile: &data.RealFile{Name: "a", Path: "/home/a"}, Type: data.File}
	overlay := NewFileHistoryOverlay(app, file, nil)
	daily := &data.SnapshotBrowserEntry{Snapshot: &zfs.Snapshot{Name: "daily", FullName: "rpool/home@daily"}}
	weekly := &data.SnapshotBrowserEntry{Snapshot: &zfs.Snapshot{Name: "weekly", FullName: "rpool/home@weekly"}}

	overlay.SetDataErrors(zfs.NewDataErrorIndex([]zfs.DataError{snapshotError}, nil))

	cells := overlay.createTableCells(0, []*table.Column{historyColumnName}, daily)
	assert.Equal(t, "daily  ⚠ damaged", cells[0].Text)
	foreground, _, _ := cells[0].Style.Decompose()
	assert.Equal(t, theme.Colors.Alert.Error, foreground)
	cells = overlay.createTableCells(0, []*table.Column{historyColumnName}, weekly)
	assert.Equal(t, "weekly", cells[0].Text)
}
