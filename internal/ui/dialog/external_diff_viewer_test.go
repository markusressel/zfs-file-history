package dialog

import (
	"os"
	"sync/atomic"
	"testing"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

func TestExternalDiffViewer_IsAvailable(t *testing.T) {
	// git should be installed on any dev machine
	gitConfig := ExternalDiffViewerConfig{Path: "git"}
	assert.True(t, gitConfig.IsAvailable())

	badConfig := ExternalDiffViewerConfig{Path: "nonexistent-diff-tool-xyz-123"}
	assert.False(t, badConfig.IsAvailable())
}

func TestFindEditorOption(t *testing.T) {
	options := []ExternalDiffViewerConfig{
		{Path: "nvim"},
		{Path: "delta"},
	}

	assert.NotNil(t, findEditorOption("/usr/bin/nvim", options))
	assert.NotNil(t, findEditorOption("delta", options))
	assert.Nil(t, findEditorOption("vim", options))
}

func TestDetermineExternalDiffViewer(t *testing.T) {
	// Specified binary matching option
	editor := determineExternalDiffViewer("git")
	assert.NotNil(t, editor)

	// Specified nonexistent binary falls back to finding available option or EDITOR
	editor = determineExternalDiffViewer("nonexistent-tool-xyz")
	// If git or delta or nvim is installed on system, it finds one
	if gitInstalled := (ExternalDiffViewerConfig{Path: "git"}).IsAvailable(); gitInstalled {
		assert.NotNil(t, editor)
	}

	// Environment variable fallback test
	oldEditor := os.Getenv("EDITOR")
	defer func() { _ = os.Setenv("EDITOR", oldEditor) }()

	_ = os.Setenv("EDITOR", "")
	assert.Nil(t, findEditorOption("invalid", nil))
}

// Regression: the paths were put into the script of the shell in single quotes for viewers piped into less, so a
// file name with a quote ended the quoting and the rest of it ran as a command.
func TestExternalDiffCommand_PathsAreNotPartOfTheScript(t *testing.T) {
	oldPath := "/pool/.zfs/snapshot/s1/it's; touch pwned"
	newPath := "/pool/it's; touch pwned"

	cmd := externalDiffCommand(Delta, oldPath, newPath)

	assert.Equal(t, []string{"sh", "-c", `"$@" | less -R`, "sh", "delta", "--line-numbers", "--hunk-header-style=omit", oldPath, newPath}, cmd.Args)
}

func TestExternalDiffCommand_WithoutPager(t *testing.T) {
	cmd := externalDiffCommand(NVIM, "/old", "/new")

	assert.Equal(t, []string{"nvim", "-d", "/old", "/new"}, cmd.Args)
}

func TestDiffPaths(t *testing.T) {
	dataset := &zfs.Dataset{Path: "/pool/ds", HiddenZfsPath: "/pool/ds/.zfs"}
	snapshot := func(name string) *zfs.Snapshot {
		return &zfs.Snapshot{Name: name, Path: "/pool/ds/.zfs/snapshot/" + name, ParentDataset: dataset}
	}
	entry := &data.SnapshotBrowserEntry{Snapshot: snapshot("new")}
	filePath := "/pool/ds/file.txt"

	tests := []struct {
		name         string
		mode         diffMode
		prevSnapshot *zfs.Snapshot
		wantOld      string
	}{
		{"working copy", diffModeWorkingCopy, nil, filePath},
		{"previous version", diffModePredecessor, snapshot("old"), "/pool/ds/.zfs/snapshot/old/file.txt"},
		{"added in this version", diffModePredecessor, nil, DevNull},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldPath, newPath := diffPaths(filePath, entry, tt.mode, tt.prevSnapshot)
			assert.Equal(t, tt.wantOld, oldPath)
			assert.Equal(t, "/pool/ds/.zfs/snapshot/new/file.txt", newPath)
		})
	}
}

// Regression: the screen was resumed after the viewer exited but not drawn, so it stayed black until the next key.
func TestRunExternalDiffEditor_DrawsWhenTheViewerExits(t *testing.T) {
	app, screen := testutil.NewApp()
	var draws atomic.Int32
	app.SetAfterDrawFunc(func(tcell.Screen) { draws.Add(1) })
	testutil.StartApp(t, app, tview.NewBox())
	testutil.WaitForEvents(t, app, screen)
	var before int32
	// after the draw of the key of WaitForEvents, which runs before the next update
	testutil.OnUiThread(t, app, func() { before = draws.Load() })

	runExternalDiffEditor(app, ExternalDiffViewerConfig{Path: "true"}, "/old", "/new")

	assert.Eventually(t, func() bool { return draws.Load() > before }, 5*time.Second, 10*time.Millisecond)
}
