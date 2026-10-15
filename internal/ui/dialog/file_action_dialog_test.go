package dialog

import (
	"testing"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/data/diff_state"

	"github.com/stretchr/testify/assert"
)

// The history comes first: it is what the menu is opened for most of the time, and it shows the changes too (there
// is no "Show diff" anymore).
func TestBuildFileDialogOptions_FileWithSnapshotAndDiff(t *testing.T) {
	entry := &data.FileBrowserEntry{
		Name:      "example.txt",
		RealFile:  &data.RealFile{Name: "example.txt"},
		Type:      data.File,
		DiffState: diff_state.Modified,
		SnapshotFiles: []*data.SnapshotFile{
			{},
		},
	}

	options := buildFileDialogOptions(entry)

	assert.Equal(t,
		[]DialogActionId{
			FileDialogShowHistoryActionId,
			FileDialogRestoreFileActionId,
			FileDialogCreateSnapshotDialogActionId,
			FileDialogDeleteDialogActionId,
			DialogCloseActionId,
		},
		optionIds(options),
	)
}

func TestBuildFileDialogOptions_DirectoryWithSnapshot(t *testing.T) {
	entry := &data.FileBrowserEntry{
		Name:     "example",
		RealFile: &data.RealFile{Name: "example"},
		Type:     data.Directory,
		SnapshotFiles: []*data.SnapshotFile{
			{},
		},
	}

	options := buildFileDialogOptions(entry)

	assert.Equal(t,
		[]DialogActionId{
			FileDialogShowHistoryActionId,
			FileDialogRestoreRecursiveDialogActionId,
			FileDialogRestoreFileActionId,
			FileDialogCreateSnapshotDialogActionId,
			FileDialogDeleteDialogActionId,
			DialogCloseActionId,
		},
		optionIds(options),
	)
}

func TestBuildFileDialogOptions_UnchangedDirectoryHasHistory(t *testing.T) {
	entry := &data.FileBrowserEntry{
		Name:      "example",
		RealFile:  &data.RealFile{Name: "example"},
		Type:      data.Directory,
		DiffState: diff_state.Equal,
	}

	options := buildFileDialogOptions(entry)

	assert.Equal(t,
		[]DialogActionId{
			FileDialogShowHistoryActionId,
			FileDialogCreateSnapshotDialogActionId,
			FileDialogDeleteDialogActionId,
			DialogCloseActionId,
		},
		optionIds(options),
	)
	assert.Equal(t, "📂 Browse folder history", options[0].Name)
}

func TestBuildFileDialogOptions_OnlyRealFile(t *testing.T) {
	entry := &data.FileBrowserEntry{
		Name:      "real-only.txt",
		RealFile:  &data.RealFile{Name: "real-only.txt"},
		Type:      data.File,
		DiffState: diff_state.Unknown,
	}

	options := buildFileDialogOptions(entry)

	assert.Equal(t,
		[]DialogActionId{
			FileDialogCreateSnapshotDialogActionId,
			FileDialogDeleteDialogActionId,
			DialogCloseActionId,
		},
		optionIds(options),
	)
}

func TestBuildFileDialogOptions_DeletedFile_AlwaysHasCloseLast(t *testing.T) {
	entry := &data.FileBrowserEntry{
		Name: "deleted-in-live.txt",
		Type: data.File,
		SnapshotFiles: []*data.SnapshotFile{
			{},
		},
		DiffState: diff_state.Deleted,
	}

	options := buildFileDialogOptions(entry)

	assert.Equal(t,
		[]DialogActionId{
			FileDialogShowHistoryActionId,
			FileDialogRestoreFileActionId,
			FileDialogCreateSnapshotDialogActionId,
			DialogCloseActionId,
		},
		optionIds(options),
	)
}

func optionIds(options []*DialogOption) []DialogActionId {
	result := make([]DialogActionId, 0, len(options))
	for _, option := range options {
		result = append(result, option.Id)
	}
	return result
}
