package file_browser

import (
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/ui/status_message"
	"zfs-file-history/internal/zfs"

	"github.com/rivo/tview"
)

type Event interface {
	isFileBrowserEvent()
}

type PathChangedEvent struct {
	NewPath string
}

func (pathChangedEvent PathChangedEvent) isFileBrowserEvent() {}

// SnapshotCreatedEvent is emitted (on the UI thread) after a snapshot was created via the file browser.
type SnapshotCreatedEvent struct {
	SnapshotName string
}

func (SnapshotCreatedEvent) isFileBrowserEvent() {}

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
	// Snapshot is the version selected first (nil: the newest)
	Snapshot *zfs.Snapshot
}

func (RequestFileHistoryEvent) isFileBrowserEvent() {}

// EntriesLoadedEvent is emitted when the entries of the folder at Path were (re)loaded, e.g. after a change on disk.
type EntriesLoadedEvent struct {
	Path string
}

func (EntriesLoadedEvent) isFileBrowserEvent() {}
