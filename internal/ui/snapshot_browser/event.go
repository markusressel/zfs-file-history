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

// PathVersionsLoaded is emitted on the UI thread once the selected entry was compared with all snapshots: the
// versions of the folder that is shown and of the selected entry in each snapshot, in no particular order.
type PathVersionsLoaded struct {
	// DatasetName and DatasetPath (its mountpoint) are the dataset of the snapshots, "" if unknown
	DatasetName string
	DatasetPath string
	FolderPath  string
	Folder      []data.PathVersion
	// Entry is the selected file or folder, nil if there is none (e.g. on the header row of the file browser)
	Entry         *data.FileBrowserEntry
	EntryVersions []data.PathVersion
}

func (e PathVersionsLoaded) isSnapshotBrowserEvent() {}
