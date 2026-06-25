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
