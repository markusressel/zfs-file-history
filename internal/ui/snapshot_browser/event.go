package snapshot_browser

type SnapshotBrowserEvent interface {
}

type SnapshotCreated struct {
	SnapshotName string
}

type SnapshotDestroyed struct {
	SnapshotName string
}
