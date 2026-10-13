package zfs

import (
	"context"
	"encoding/binary"
	"os"
	"path"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
)

// ObjectKey identifies a damaged object whose path ZFS does not know (see DataError.Object): the dataset and the
// object number, which is the inode number of a file.
type ObjectKey struct {
	Dataset string
	Object  uint64
}

// ObjectLocation is where a damaged object was found by its inode number in a snapshot, see FindObjects.
type ObjectLocation struct {
	// Snapshot is the name of the snapshot (behind the "@")
	Snapshot string
	// Path is the path of the file within the dataset, starting with "/"
	Path string
	// LivePath is the path the file had in the mounted dataset
	LivePath string
	// File is the path of the file in the snapshot directory, e.g. "/home/.zfs/snapshot/daily/user/file"
	File string
}

// ObjectSearchProgress is how far FindObjects got.
type ObjectSearchProgress struct {
	// Snapshot is the name of the snapshot being searched, Done the number of snapshots searched of Total
	Snapshot string
	Done     int
	Total    int
	// Entries is the number of files and folders looked at in Snapshot
	Entries int64
	// Found is the number of objects found so far, of Wanted
	Found  int
	Wanted int
}

// objectSearchWorkers is the number of folders read at the same time while searching a snapshot.
const objectSearchWorkers = 8

// FindObjects looks for the damaged objects whose path ZFS does not know (e.g. "tank/data:<0x1c2a>", a file that
// was deleted since) in the snapshots of their datasets, by their inode number: on ZFS, it is the object number.
// The snapshots are searched from the newest to the oldest. Once an object was found, the older snapshots are only
// checked for a file with the same inode at the same path, so a snapshot is only read completely while objects are
// missing. Only mounted datasets can be searched, and only the folders the user can read.
//
// The result has an entry for every object that was searched (nil: found in no snapshot), none for the ones that
// could not be (e.g. their dataset is not mounted). An object number can be reused after its file was deleted, so a
// file found this way is the damaged one only probably. progress is called from the goroutine of the search. It
// reads whole snapshots, so it must not be called on the UI thread.
func FindObjects(ctx context.Context, index *DataErrorIndex, progress func(ObjectSearchProgress)) (map[ObjectKey][]ObjectLocation, error) {
	mounts, err := backend.Mounts()
	if err != nil {
		return nil, err
	}
	// the objects to find in each dataset, and the snapshots they were reported in (nil: in the dataset itself, so
	// all snapshots are searched)
	wanted := map[string]map[uint64]map[string]bool{}
	for _, dataError := range index.objectErrors() {
		if wanted[dataError.Dataset] == nil {
			wanted[dataError.Dataset] = map[uint64]map[string]bool{}
		}
		snapshots, known := wanted[dataError.Dataset][dataError.Object]
		switch {
		case dataError.Snapshot == "":
			snapshots = nil
		case !known || snapshots != nil:
			if snapshots == nil {
				snapshots = map[string]bool{}
			}
			snapshots[dataError.Snapshot] = true
		}
		wanted[dataError.Dataset][dataError.Object] = snapshots
	}

	result := map[ObjectKey][]ObjectLocation{}
	state := ObjectSearchProgress{}
	for _, objects := range wanted {
		state.Wanted += len(objects)
	}
	datasets := make([]string, 0, len(wanted))
	for dataset := range wanted {
		datasets = append(datasets, dataset)
	}
	slices.Sort(datasets)
	for _, dataset := range datasets {
		mountpoint, ok := mountpointOf(dataset, mounts)
		if !ok {
			// not mounted: not searched, so not in the result
			continue
		}
		snapshots, err := backend.ListSnapshotNames(dataset)
		if err != nil {
			return nil, err
		}
		state.Total += len(snapshots)
		// the path at which each object was found first
		found := map[uint64]string{}
		for _, snapshot := range snapshots {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			root := path.Join(mountpoint, ".zfs", "snapshot", snapshot)
			state.Snapshot, state.Entries = snapshot, 0
			progress(state)

			add := func(object uint64, filePath string) {
				result[ObjectKey{dataset, object}] = append(result[ObjectKey{dataset, object}], ObjectLocation{
					Snapshot: snapshot, Path: filePath, LivePath: path.Join(mountpoint, filePath), File: path.Join(root, filePath),
				})
			}
			// the ones found already: the same file at the same path
			missing := map[uint64]bool{}
			for object, only := range wanted[dataset] {
				if only != nil && !only[snapshot] {
					continue
				}
				if filePath, ok := found[object]; ok {
					if inodeOf(path.Join(root, filePath)) == object {
						add(object, filePath)
					}
					continue
				}
				missing[object] = true
			}
			if len(missing) > 0 {
				err := searchInodes(ctx, root, missing, func(object uint64, filePath string) {
					if _, ok := found[object]; !ok {
						found[object] = filePath
						state.Found++
					}
					add(object, filePath)
				}, func(entries int64) {
					state.Entries = entries
					progress(state)
				})
				if err != nil {
					return nil, err
				}
			}
			state.Done++
		}
		// searched, but not found
		for object := range wanted[dataset] {
			if _, ok := result[ObjectKey{dataset, object}]; !ok {
				result[ObjectKey{dataset, object}] = nil
			}
		}
	}
	state.Snapshot = ""
	progress(state)
	return result, nil
}

// objectErrors returns the errors of objects whose path ZFS does not know, but their dataset and object number.
func (index *DataErrorIndex) objectErrors() []DataError {
	if index == nil {
		return nil
	}
	var objects []DataError
	for _, dataError := range index.Errors {
		if dataError.Path == "" && dataError.Dataset != "" && dataError.Object != 0 {
			objects = append(objects, dataError)
		}
	}
	return objects
}

// HasUnresolvedObjects returns whether there are damaged objects that FindObjects could look for.
func (index *DataErrorIndex) HasUnresolvedObjects() bool {
	return len(index.objectErrors()) > 0
}

// mountpointOf returns where the dataset is mounted.
func mountpointOf(dataset string, mounts []Mount) (string, bool) {
	for _, mount := range mounts {
		if mount.Name == dataset {
			return mount.Mountpoint, true
		}
	}
	return "", false
}

// inodeOf returns the inode number of the file, 0 if it does not exist.
func inodeOf(filePath string) uint64 {
	var stat syscall.Stat_t
	if err := syscall.Lstat(filePath, &stat); err != nil {
		return 0
	}
	return stat.Ino
}

// searchInodes reads all folders below root (with several workers), and calls onFound with each entry whose inode
// is one of the wanted ones, and its path relative to root (starting with "/"). The inodes are taken from the
// directory entries (getdents), so no file has to be stat'ed. onProgress is called with the number of entries read
// every few thousand entries. Both are called from one goroutine at a time.
func searchInodes(ctx context.Context, root string, wanted map[uint64]bool, onFound func(inode uint64, filePath string), onProgress func(entries int64)) error {
	var mu sync.Mutex
	var entries atomic.Int64
	var pending sync.WaitGroup
	folders := make(chan string, 4096)
	// queue adds a folder to read; folders are never dropped, a full channel is handed to a goroutine
	queue := func(folder string) {
		pending.Add(1)
		select {
		case folders <- folder:
		default:
			go func() { folders <- folder }()
		}
	}

	var rootStat syscall.Stat_t
	if err := syscall.Stat(root, &rootStat); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var workers sync.WaitGroup
	for range objectSearchWorkers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			buffer := make([]byte, 64*1024)
			for folder := range folders {
				if ctx.Err() == nil {
					readFolder(root, folder, rootStat.Dev, buffer, func(name string, inode uint64, isDir bool) {
						filePath := folder + "/" + name
						if wanted[inode] {
							mu.Lock()
							onFound(inode, filePath)
							mu.Unlock()
						}
						if isDir {
							queue(filePath)
						}
						if count := entries.Add(1); count%20000 == 0 {
							mu.Lock()
							onProgress(count)
							mu.Unlock()
						}
					})
				}
				pending.Done()
			}
		}()
	}
	queue("")
	pending.Wait()
	close(folders)
	workers.Wait()
	onProgress(entries.Load())
	return ctx.Err()
}

// readFolder calls onEntry with each entry of the folder below root (without "." and ".."), its inode number and
// whether it is a folder. Folders that cannot be read (e.g. no permission), and the ones of other file systems than
// device (e.g. a child dataset mounted inside), are skipped: their inode numbers are the objects of another dataset.
func readFolder(root string, folder string, device uint64, buffer []byte, onEntry func(name string, inode uint64, isDir bool)) {
	file, err := os.Open(root + folder)
	if err != nil {
		return
	}
	defer file.Close()
	fd := int(file.Fd())
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil || stat.Dev != device {
		return
	}
	for {
		n, err := syscall.Getdents(fd, buffer)
		if err != nil || n <= 0 {
			return
		}
		parseDirents(buffer[:n], func(name string, inode uint64, direntType byte) {
			if name == "." || name == ".." {
				return
			}
			onEntry(name, inode, direntType == syscall.DT_DIR)
		})
	}
}

// parseDirents parses the entries returned by getdents64 (struct linux_dirent64: inode, offset, record length, type,
// name terminated by 0).
func parseDirents(buffer []byte, onEntry func(name string, inode uint64, direntType byte)) {
	for offset := 0; offset+19 <= len(buffer); {
		inode := binary.NativeEndian.Uint64(buffer[offset:])
		recordLength := int(binary.NativeEndian.Uint16(buffer[offset+16:]))
		if recordLength == 0 || offset+recordLength > len(buffer) {
			return
		}
		direntType := buffer[offset+18]
		name := buffer[offset+19 : offset+recordLength]
		if end := slices.Index(name, 0); end >= 0 {
			name = name[:end]
		}
		onEntry(string(name), inode, direntType)
		offset += recordLength
	}
}
