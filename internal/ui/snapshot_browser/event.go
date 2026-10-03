package snapshot_browser

import (
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/ui/status_message"
)

type Event interface {
	isSnapshotBrowserEvent()
}

type SelectedSnapshotChanged struct {
	Snapshot *data.SnapshotBrowserEntry
}

func (e SelectedSnapshotChanged) isSnapshotBrowserEvent() {}

type StatusMessageEvent struct {
	Message *status_message.StatusMessage
}

func (e StatusMessageEvent) isSnapshotBrowserEvent() {}

// RequestHistoryEvent asks for the history of a file or folder, at the version that was current in a snapshot.
type RequestHistoryEvent struct {
	Entry    *data.FileBrowserEntry
	Snapshot *data.SnapshotBrowserEntry
}

func (e RequestHistoryEvent) isSnapshotBrowserEvent() {}
