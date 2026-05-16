package dialog

import (
	"testing"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/data/diff_state"

	"github.com/stretchr/testify/assert"
)

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

	options := buildFileDialogOptions(entry, true)

	assert.Equal(t,
		[]DialogActionId{
			FileDialogCreateSnapshotDialogActionId,
			FileDialogShowDiffActionId,
			FileDialogRestoreFileActionId,
			FileDialogDeleteDialogActionId,
			DialogCloseActionId,
		},
		optionIds(options),
	)
}

func TestBuildFileDialogOptions_FileWithoutDiffBinary(t *testing.T) {
	entry := &data.FileBrowserEntry{
		Name:      "example.txt",
		RealFile:  &data.RealFile{Name: "example.txt"},
		Type:      data.File,
		DiffState: diff_state.Modified,
		SnapshotFiles: []*data.SnapshotFile{
			{},
		},
	}

	options := buildFileDialogOptions(entry, false)

	assert.Equal(t,
		[]DialogActionId{
			FileDialogCreateSnapshotDialogActionId,
			FileDialogDeleteDialogActionId,
			FileDialogRestoreFileActionId,
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

	options := buildFileDialogOptions(entry, true)

	assert.Equal(t,
		[]DialogActionId{
			FileDialogCreateSnapshotDialogActionId,
			FileDialogRestoreRecursiveDialogActionId,
			FileDialogRestoreFileActionId,
			FileDialogDeleteDialogActionId,
			DialogCloseActionId,
		},
		optionIds(options),
	)
}

func TestBuildFileDialogOptions_OnlyRealFile(t *testing.T) {
	entry := &data.FileBrowserEntry{
		Name:     "real-only.txt",
		RealFile: &data.RealFile{Name: "real-only.txt"},
		Type:     data.File,
	}

	options := buildFileDialogOptions(entry, true)

	assert.Equal(t,
		[]DialogActionId{
			FileDialogCreateSnapshotDialogActionId,
			FileDialogDeleteDialogActionId,
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
