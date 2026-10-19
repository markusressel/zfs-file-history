package data

// FolderSummary is what a folder holds directly: its files and folders and the size of its files, not what is below
// its folders (that would mean reading the whole tree, which takes minutes for large ones; ZFS only knows the size of
// datasets).
type FolderSummary struct {
	// Files and Folders are the entries of the folder now; files are all entries that are no folders, e.g. links
	Files, Folders int
	// Size is the size of the files now
	Size int64
	// SnapshotSize is the size of the files of the folder in the selected snapshot, valid if InSnapshot
	SnapshotSize int64
	// InSnapshot is whether a snapshot is selected
	InSnapshot bool
	// Loading is whether the entries are being read (for longer than a moment): the values are of the last load, of
	// another folder or snapshot
	Loading bool
}
