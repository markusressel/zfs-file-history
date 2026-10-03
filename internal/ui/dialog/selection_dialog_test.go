package dialog

import (
	"sync"
	"testing"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSelectionDialog(t *testing.T) {
	app := tview.NewApplication()
	options := []*DialogOption{
		{Id: DialogActionId(1), Name: "Option 1"},
		{Id: DialogCloseActionId, Name: "Cancel"},
	}
	d := NewSelectionDialog(app, "test-dialog", "Title", "Description Text", options, nil, nil)

	assert.Equal(t, "test-dialog", d.GetName())
	assert.NotNil(t, d.GetLayout())
	assert.NotNil(t, d.GetActionChannel())
}

func TestSelectionDialog_NumericJumpShortcuts(t *testing.T) {
	app := tview.NewApplication()
	options := []*DialogOption{
		{Id: DialogActionId(1), Name: "Option A"},
		{Id: DialogActionId(2), Name: "Option B"},
		{Id: DialogCloseActionId, Name: "Close"},
	}

	onSelect := func(opt *DialogOption) {}
	onClose := func() {}

	optionTable := createOptionTable(app, options, onSelect)
	capture := createOptionDialogInputCapture(optionTable, options, onSelect, onClose)

	// Initially, row 0 is selected
	row, _ := optionTable.GetSelection()
	assert.Equal(t, 0, row)

	// Press '2' to jump to Option B (row 1 in the table, since row 2 is spacer, row 3 is Close)
	evt2 := tcell.NewEventKey(tcell.KeyRune, '2', tcell.ModNone)
	res := capture(evt2)
	assert.Nil(t, res) // Consumed

	row, _ = optionTable.GetSelection()
	assert.Equal(t, 1, row) // Row 1 is Option B

	// Press '1' to jump to Option A
	evt1 := tcell.NewEventKey(tcell.KeyRune, '1', tcell.ModNone)
	res = capture(evt1)
	assert.Nil(t, res) // Consumed

	row, _ = optionTable.GetSelection()
	assert.Equal(t, 0, row)

	// Press '3' (out of range, since we only have 2 numbered options)
	evt3 := tcell.NewEventKey(tcell.KeyRune, '3', tcell.ModNone)
	res = capture(evt3)
	assert.Nil(t, res) // Consumed, but should not change selection

	row, _ = optionTable.GetSelection()
	assert.Equal(t, 0, row) // Remains 0
}

func TestNewDeleteFileDialog(t *testing.T) {
	app := tview.NewApplication()
	file := &data.FileBrowserEntry{
		Name:     "to_delete.txt",
		RealFile: &data.RealFile{Name: "to_delete.txt"},
	}

	d := NewDeleteFileDialog(app, file, nil, nil)
	assert.Equal(t, "DeleteFileDialog", d.GetName())
}

func TestNewDestroySnapshotsDialog(t *testing.T) {
	app := tview.NewApplication()
	d := NewDestroySnapshotsDialog(app, "This frees 1 GiB and cannot be undone.", nil, nil)
	assert.Equal(t, "DestroySnapshotsDialog", d.GetName())
}

func TestNewFileActionDialog(t *testing.T) {
	app := tview.NewApplication()
	file := &data.FileBrowserEntry{
		Name:      "test.txt",
		RealFile:  &data.RealFile{Name: "test.txt", Path: "/pool/ds1/test.txt"},
		Type:      data.File,
		DiffState: diff_state.Modified,
		SnapshotFiles: []*data.SnapshotFile{
			{
				Path: "/pool/ds1/.zfs/snapshot/snap-file/test.txt",
				Snapshot: &zfs.Snapshot{
					Name: "snap-file",
					ParentDataset: &zfs.Dataset{
						Path:          "/pool/ds1",
						HiddenZfsPath: "/pool/ds1/.zfs",
					},
				},
			},
		},
	}

	d := NewFileActionDialog(app, file, nil, nil)
	assert.Equal(t, "ActionDialog", d.GetName())
}

func TestNewSnapshotActionDialog(t *testing.T) {
	app := tview.NewApplication()
	snapshot := &data.SnapshotBrowserEntry{
		Snapshot: &zfs.Snapshot{
			Name: "snapshot-2",
			ParentDataset: &zfs.Dataset{
				Path:          "/pool/ds1",
				HiddenZfsPath: "/pool/ds1/.zfs",
			},
		},
	}

	d := NewSnapshotActionDialog(app, snapshot, "", nil, nil)
	assert.Equal(t, "SnapshotActionDialog", d.GetName())
	for _, option := range d.options {
		assert.NotEqual(t, SnapshotDialogShowHistoryActionId, option.Id, "no history without a file or folder")
	}

	// with a file or folder: its history at the snapshot, as the first action
	d = NewSnapshotActionDialog(app, snapshot, "notes.txt", nil, nil)
	assert.Equal(t, SnapshotDialogShowHistoryActionId, d.options[0].Id)
	assert.Contains(t, d.options[0].Name, "'notes.txt'")
}

func TestNewMultiSnapshotActionDialog(t *testing.T) {
	app := tview.NewApplication()
	snapshots := []*data.SnapshotBrowserEntry{
		{
			Snapshot: &zfs.Snapshot{
				Name: "snapshot-3",
				ParentDataset: &zfs.Dataset{
					Path:          "/pool/ds1",
					HiddenZfsPath: "/pool/ds1/.zfs",
				},
			},
		},
		{
			Snapshot: &zfs.Snapshot{
				Name: "snapshot-4",
				ParentDataset: &zfs.Dataset{
					Path:          "/pool/ds1",
					HiddenZfsPath: "/pool/ds1/.zfs",
				},
			},
		},
	}

	d := NewMultiSnapshotActionDialog(app, snapshots, nil, nil)
	assert.Equal(t, "MultiSnapshotActionDialog", d.GetName())
}

func TestNewRestoreFileDialog(t *testing.T) {
	app := tview.NewApplication()
	file := &data.FileBrowserEntry{
		Name: "test.txt",
		Type: data.File,
	}

	d := NewRestoreFileDialog(app, file, nil, nil)
	assert.Equal(t, "RestoreFileDialog", d.GetName())

	opts := buildRestoreDialogOptions(file)
	assert.Len(t, opts, 2)
	assert.Equal(t, RestoreFileDialogRestoreFileActionId, opts[0].Id)
	assert.Equal(t, DialogCloseActionId, opts[1].Id)

	dir := &data.FileBrowserEntry{
		Name: "test-dir",
		Type: data.Directory,
	}
	optsDir := buildRestoreDialogOptions(dir)
	assert.Len(t, optsDir, 3)
	assert.Equal(t, RestoreFileDialogRestoreRecursiveActionId, optsDir[0].Id)
	assert.Equal(t, RestoreFileDialogRestoreFileActionId, optsDir[1].Id)
	assert.Equal(t, DialogCloseActionId, optsDir[2].Id)
}

func TestSelectionDialog_StopLoadingRestoresOptionText(t *testing.T) {
	app, stop := setupTestApp()
	defer stop()

	option := &DialogOption{Id: 1, Name: "Run"}
	d := NewSelectionDialog(app, "TestDialog", "Title", "Description",
		[]*DialogOption{option, {Id: DialogCloseActionId, Name: "Close"}}, nil, nil)

	optionCellText := func() string {
		text := ""
		onUiThread(t, app, func() {
			for row := 0; row < d.optionTable.GetRowCount(); row++ {
				if cell := d.optionTable.GetCell(row, 1); cell != nil && cell.GetReference() == option {
					text = cell.Text
				}
			}
		})
		return text
	}
	originalText := optionCellText()
	assert.NotEmpty(t, originalText)

	onUiThread(t, app, func() { d.ShowLoading(option) })
	// the spinner appends a frame to the option text
	assert.Eventually(t, func() bool { return optionCellText() != originalText }, 2*time.Second, 10*time.Millisecond)

	onUiThread(t, app, func() { d.StopLoading() })
	assert.Equal(t, originalText, optionCellText())

	// no late spinner frame overwrites the restored text
	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, originalText, optionCellText())

	// loading can be started again, e.g. for another action
	onUiThread(t, app, func() { d.ShowLoading(option) })
	assert.Eventually(t, func() bool { return optionCellText() != originalText }, 2*time.Second, 10*time.Millisecond)
	onUiThread(t, app, func() { d.StopLoading() })
	assert.Equal(t, originalText, optionCellText())
}

func TestSelectionDialog_RetryFunc(t *testing.T) {
	app := tview.NewApplication()
	screen := tcell.NewSimulationScreen("UTF-8")
	app.SetScreen(screen)
	pages := tview.NewPages()
	app.SetRoot(pages, true)
	go func() { _ = app.Run() }()
	t.Cleanup(app.Stop)

	var mu sync.Mutex
	var runs []DialogActionId
	options := []*DialogOption{{Id: 7, Name: "Hold"}, {Id: DialogCloseActionId, Name: "Close"}}
	d := NewSelectionDialog(app, "Actions", "Actions", "", options,
		func(d *SelectionDialog, action DialogActionId) error {
			mu.Lock()
			defer mu.Unlock()
			runs = append(runs, action)
			return nil
		},
		func(d *SelectionDialog, option *DialogOption, err error) { d.Close() },
	)
	runCount := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(runs)
	}
	isShown := func() bool {
		shown := false
		onUiThread(t, app, func() { shown = pages.HasPage("Actions") })
		return shown
	}

	onUiThread(t, app, func() { ShowDialogOnPages(app, pages, d, nil) })
	screen.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	require.Eventually(t, func() bool { return runCount() == 1 && !isShown() }, 2*time.Second, 10*time.Millisecond)

	// the dialog is shown again and runs the option again, then closes like the first time
	onUiThread(t, app, func() { d.RetryFunc(options[0])() })
	require.Eventually(t, func() bool { return runCount() == 2 && !isShown() }, 2*time.Second, 10*time.Millisecond)
	mu.Lock()
	assert.Equal(t, []DialogActionId{7, 7}, runs)
	mu.Unlock()
}

func TestSelectionDialog_SetOptionName(t *testing.T) {
	options := []*DialogOption{{Id: 1, Name: "Copy"}, {Id: 2, Name: "Other"}, {Id: DialogCloseActionId, Name: "Close"}}
	d := NewSelectionDialog(tview.NewApplication(), "Actions", "Actions", "", options, nil, nil)

	d.SetOptionName(1, "Copied")

	assert.Equal(t, "Copied", d.optionTable.GetCell(0, 1).Text)
	assert.Equal(t, "Copied", options[0].Name)
	assert.Equal(t, "Other", d.optionTable.GetCell(1, 1).Text)
}
