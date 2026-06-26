package file_browser

import (
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/ui/status_message"

	"github.com/rivo/tview"
)

type Event interface {
	isFileBrowserEvent()
}

type PathChangedEvent struct {
	NewPath string
}

func (pathChangedEvent PathChangedEvent) isFileBrowserEvent() {}

type CreateSnapshotEvent struct {
	SnapshotName string
}

func (CreateSnapshotEvent) isFileBrowserEvent() {}

type RequestFocusEvent struct {
	Layout tview.Primitive
}

func (RequestFocusEvent) isFileBrowserEvent() {}

type FileBrowserStatusEvent struct {
	Message *status_message.StatusMessage
}

func (FileBrowserStatusEvent) isFileBrowserEvent() {}

type SelectedTableEntryChangedEvent struct {
	FileEntry *data.FileBrowserEntry
}

func (SelectedTableEntryChangedEvent) isFileBrowserEvent() {}

type RequestFileHistoryEvent struct {
	FileEntry *data.FileBrowserEntry
}

func (RequestFileHistoryEvent) isFileBrowserEvent() {}
