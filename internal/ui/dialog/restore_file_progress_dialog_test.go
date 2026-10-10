package dialog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

func TestRestoreFileProgressDialog(t *testing.T) {
	app := tview.NewApplication()
	file := &data.FileBrowserEntry{
		Name: "test.txt",
		SnapshotFiles: []*data.SnapshotFile{
			{
				Path: "/pool/ds1/.zfs/snapshot/snap1/test.txt",
				Snapshot: &zfs.Snapshot{
					Name: "snap1",
					ParentDataset: &zfs.Dataset{
						Path:          "/pool/ds1",
						HiddenZfsPath: "/pool/ds1/.zfs",
					},
				},
			},
		},
	}

	d := NewRestoreFileProgressDialog(app, file, false)

	assert.Equal(t, string(RestoreFileProgress), d.GetName())
	assert.NotNil(t, d.GetLayout())
	assert.NotNil(t, d.GetActionChannel())

	// Wait briefly to allow background goroutine and handleError to execute
	time.Sleep(100 * time.Millisecond)

	d.Close()
}

func TestRestoreFileProgressDialog_AbsentFile(t *testing.T) {
	app := tview.NewApplication()
	tempFile := filepath.Join(t.TempDir(), "to_delete.txt")
	err := os.WriteFile(tempFile, []byte("hello"), 0644)
	assert.NoError(t, err)

	file := &data.FileBrowserEntry{
		Name: "to_delete.txt",
		SnapshotFiles: []*data.SnapshotFile{
			{
				Path:         "", // empty path means absent in snapshot
				OriginalPath: tempFile,
				Snapshot: &zfs.Snapshot{
					Name: "snap1",
					ParentDataset: &zfs.Dataset{
						Path:          "/pool/ds1",
						HiddenZfsPath: "/pool/ds1/.zfs",
					},
				},
			},
		},
	}

	d := NewRestoreFileProgressDialog(app, file, false)
	assert.Equal(t, string(RestoreFileProgress), d.GetName())

	time.Sleep(100 * time.Millisecond)

	d.Close()

	// Assert the working copy was deleted
	assert.NoFileExists(t, tempFile)
}

// Regression: the dialog kept the height of the description, so only the first line of a longer error was shown.
func TestRestoreFileProgressDialog_ShowsTheWholeError(t *testing.T) {
	app, screen, pages, _ := startDialogTestApp(t)
	longPath := "/pool/ds1/.zfs/snapshot/snap1/" + strings.Repeat("a-long-folder-name/", 4) + "test.txt"
	file := &data.FileBrowserEntry{
		Name: "test.txt",
		SnapshotFiles: []*data.SnapshotFile{{
			Path: longPath,
			Snapshot: &zfs.Snapshot{
				Name:          "snap1",
				ParentDataset: &zfs.Dataset{Path: "/pool/ds1", HiddenZfsPath: "/pool/ds1/.zfs"},
			},
		}},
	}

	testutil.OnUiThread(t, app, func() {
		ShowDialogOnPages(app, pages, NewRestoreFileProgressDialog(app, file, false), nil)
	})
	// the error is shown in lines of the dialog, so its end is shown as well
	assert.Eventually(t, func() bool {
		text := dialogScreenText(t, app, screen)
		return strings.Contains(text, "Failed!") && strings.Contains(text, "test.txt: no such file or") &&
			strings.Contains(text, "directory")
	}, 3*time.Second, 20*time.Millisecond, "the end of the error is shown")
}

// startDialogTestApp runs an app with a focused list on its pages, to show dialogs on.
func startDialogTestApp(t *testing.T) (*tview.Application, tcell.SimulationScreen, *tview.Pages, *tview.List) {
	app := tview.NewApplication()
	screen := tcell.NewSimulationScreen("UTF-8")
	app.SetScreen(screen)
	screen.SetSize(80, 24)
	list := tview.NewList().AddItem("entry", "", 0, nil)
	pages := tview.NewPages().AddPage("main", list, true, true)
	app.SetRoot(pages, true).SetFocus(list)
	go func() { _ = app.Run() }()
	t.Cleanup(app.Stop)
	return app, screen, pages, list
}

// dialogScreenText returns the text on the screen, one line per row.
func dialogScreenText(t *testing.T, app *tview.Application, screen tcell.SimulationScreen) string {
	var text strings.Builder
	// on the UI thread: the cells are the ones of the screen, not a copy
	testutil.OnUiThread(t, app, func() {
		screen.Show()
		cells, width, height := screen.GetContents()
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
