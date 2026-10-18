package zfs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	path2 "path"
	"strings"
	"syscall"
	"time"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/util"
)

const (
	OS_READ        = 04
	OS_WRITE       = 02
	OS_EX          = 01
	OS_USER_SHIFT  = 6
	OS_GROUP_SHIFT = 3
	OS_OTH_SHIFT   = 0

	OS_USER_R   = OS_READ << OS_USER_SHIFT
	OS_USER_W   = OS_WRITE << OS_USER_SHIFT
	OS_USER_X   = OS_EX << OS_USER_SHIFT
	OS_USER_RW  = OS_USER_R | OS_USER_W
	OS_USER_RWX = OS_USER_RW | OS_USER_X

	OS_GROUP_R   = OS_READ << OS_GROUP_SHIFT
	OS_GROUP_W   = OS_WRITE << OS_GROUP_SHIFT
	OS_GROUP_X   = OS_EX << OS_GROUP_SHIFT
	OS_GROUP_RW  = OS_GROUP_R | OS_GROUP_W
	OS_GROUP_RWX = OS_GROUP_RW | OS_GROUP_X

	OS_OTH_R   = OS_READ << OS_OTH_SHIFT
	OS_OTH_W   = OS_WRITE << OS_OTH_SHIFT
	OS_OTH_X   = OS_EX << OS_OTH_SHIFT
	OS_OTH_RW  = OS_OTH_R | OS_OTH_W
	OS_OTH_RWX = OS_OTH_RW | OS_OTH_X

	OS_ALL_R   = OS_USER_R | OS_GROUP_R | OS_OTH_R
	OS_ALL_W   = OS_USER_W | OS_GROUP_W | OS_OTH_W
	OS_ALL_X   = OS_USER_X | OS_GROUP_X | OS_OTH_X
	OS_ALL_RW  = OS_ALL_R | OS_ALL_W
	OS_ALL_RWX = OS_ALL_RW | OS_GROUP_X
)

type Snapshot struct {
	Name          string
	FullName      string
	Path          string
	ParentDataset *Dataset
	// Properties are read with the snapshots, see Dataset.GetSnapshots
	Properties SnapshotProperties
}

func (s *Snapshot) Equal(e Snapshot) bool {
	return s.Name == e.Name && s.Path == e.Path
}

// GetSnapshotPath returns the corresponding snapshot path of a file on the dataset
func (s *Snapshot) GetSnapshotPath(path string) string {
	fileWithoutBasePath := strings.Replace(path, s.ParentDataset.Path, "", 1)
	snapshotPath := path2.Join(s.ParentDataset.GetSnapshotsDir(), s.Name, fileWithoutBasePath)
	return snapshotPath
}

// GetRealPath returns the corresponding "real" path of a file on the dataset
func (s *Snapshot) GetRealPath(path string) string {
	fileWithoutBasePath := strings.Replace(path, s.Path, "", 1)
	realPath := path2.Join(s.ParentDataset.Path, fileWithoutBasePath)
	return realPath
}

// RestoreProgress is called by RestoreRecursive after each entry it restored, with the number of entries restored so
// far.
type RestoreProgress func(done int)

// CountRestoreEntries returns how many entries RestoreRecursive restores for srcPath: the entry itself and everything
// below it, for its progress. Stops with the error of ctx when it is cancelled.
// This reads the snapshot, so it must not be called on the UI thread.
func CountRestoreEntries(ctx context.Context, srcPath string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	stat, err := os.Lstat(srcPath)
	if err != nil {
		return 0, err
	}
	count := 1
	if !stat.IsDir() {
		return count, nil
	}
	files, err := util.ListFilesIn(srcPath)
	if err != nil {
		return 0, err
	}
	for _, file := range files {
		below, err := CountRestoreEntries(ctx, file)
		if err != nil {
			return 0, err
		}
		count += below
	}
	return count, nil
}

// RestoreRecursive restores srcPath (a path in the snapshot) with everything below it, and calls progress (if not
// nil) after each entry. The times and mode of a folder are set after its entries were restored: restoring them
// changes its modification time, and a folder without write permission could not take them. Stops between entries
// (and while copying a file, which then stays as it was) with the error of ctx when it is cancelled: the entries
// restored until then stay restored.
// This copies files, so it must not be called on the UI thread.
func (s *Snapshot) RestoreRecursive(ctx context.Context, srcPath string, progress RestoreProgress) error {
	done := 0
	return s.restoreRecursive(ctx, srcPath, func() {
		done++
		if progress != nil {
			progress(done)
		}
	})
}

// restoreRecursive restores srcPath with everything below it, see RestoreRecursive, and calls restored after each
// entry.
func (s *Snapshot) restoreRecursive(ctx context.Context, srcPath string, restored func()) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	stat, err := os.Lstat(srcPath)
	if err != nil {
		return err
	}
	if !stat.IsDir() {
		if err := s.RestoreFile(ctx, srcPath); err != nil {
			return err
		}
		restored()
		return nil
	}

	dstPath := s.GetRealPath(srcPath)
	// writable by the owner until its entries are restored, its own mode is set after them
	if err := os.MkdirAll(dstPath, stat.Mode().Perm()|OS_USER_RWX); err != nil {
		return err
	}
	files, err := util.ListFilesIn(srcPath)
	if err != nil {
		return err
	}
	for _, file := range files {
		if err := s.restoreRecursive(ctx, file, restored); err != nil {
			return err
		}
	}
	if err := syncFileProperties(dstPath, stat); err != nil {
		return err
	}
	restored()
	return nil
}

// Restore restores srcPath (a path in the snapshot) without the entries below it: a file with its content, a folder
// with its mode, owner and times. See RestoreFile for cancelling.
// This copies files, so it must not be called on the UI thread.
func (s *Snapshot) Restore(ctx context.Context, srcPath string) error {
	stat, err := os.Lstat(srcPath)
	if err != nil {
		return err
	}
	if stat.IsDir() {
		return s.RestoreDir(s.GetRealPath(srcPath), stat)
	}
	return s.RestoreFile(ctx, srcPath)
}

func (s *Snapshot) RestoreDir(dstPath string, stat os.FileInfo) error {
	err := os.MkdirAll(dstPath, stat.Mode())
	if err != nil {
		return err
	}

	destFile, err := os.Open(dstPath) // creates if file doesn't exist
	if err != nil {
		return err
	}

	err = destFile.Sync()
	if err != nil {
		return err
	}

	err = destFile.Close()
	if err != nil {
		return err
	}

	err = syncFileProperties(dstPath, stat)
	if err != nil {
		return err
	}

	return err
}

// RestoreFile replaces the real file with its version in the snapshot (srcPath), with the mode, owner and times of
// that version. The content is written to a temporary file next to the real one first, which then replaces it: so a
// failed restore leaves the real file as it was, and files that cannot be written to while they are in use can be
// restored as well, e.g. the executable of a running program (open fails with "text file busy"). Stops copying with
// the error of ctx when it is cancelled, the real file stays as it was.
func (s *Snapshot) RestoreFile(ctx context.Context, srcPath string) (err error) {
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	stat, err := os.Lstat(srcPath)
	if err != nil {
		return err
	}

	dstPath := s.GetRealPath(srcPath)

	// ensure parent directories exist
	parentDir := path2.Dir(dstPath)

	// use permissions of source (snapshot) but add "x" if it is a directory
	fileMode := stat.Mode() | OS_USER_X
	err = os.MkdirAll(parentDir, fileMode)
	if err != nil {
		return err
	}

	// in the same folder, so it can be renamed to the real file (a rename does not work across file systems)
	tempFile, err := os.CreateTemp(parentDir, "."+path2.Base(dstPath)+".zfh-restore-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tempFile.Close()
			_ = os.Remove(tempFile.Name())
		}
	}()

	_, err = io.Copy(tempFile, contextReader{ctx: ctx, reader: srcFile})
	if err != nil {
		return err
	}

	err = tempFile.Sync()
	if err != nil {
		return err
	}

	err = tempFile.Close()
	if err != nil {
		return err
	}

	err = syncFileProperties(tempFile.Name(), stat)
	if err != nil {
		return err
	}

	return os.Rename(tempFile.Name(), dstPath)
}

// contextReader reads from reader until ctx is cancelled, then fails with the error of ctx: a copy stops within a
// read of the buffer of io.Copy.
type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func (s *Snapshot) IsRealFileDifferent(path string) bool {
	realPath := path
	snapshotPath := s.GetSnapshotPath(path)

	if s.IsSnapshotPath(path) {
		snapshotPath = path
		realPath = s.GetRealPath(path)
	}

	realStat, err := os.Lstat(realPath)
	if err != nil {
		return false
	}

	snapStat, err := os.Lstat(snapshotPath)
	if err != nil {
		return false
	}

	return isFileInfoDifferent(realStat, snapStat)
}

// isFileInfoDifferent returns whether two versions of a file differ, judged by their metadata.
func isFileInfoDifferent(a os.FileInfo, b os.FileInfo) bool {
	return a.IsDir() != b.IsDir() ||
		a.Mode() != b.Mode() ||
		!a.ModTime().Equal(b.ModTime()) ||
		a.Size() != b.Size() ||
		a.Name() != b.Name()
}

func (s *Snapshot) IsSnapshotPath(path string) bool {
	return strings.HasPrefix(path, s.Path)
}

func (s *Snapshot) ContainsFile(entry string) (bool, error) {
	realPath := s.GetSnapshotPath(entry)
	_, err := os.Lstat(realPath)
	if os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

// DetermineDiffState Determine the diff state between a real file and its snapshot counterpart
func (s *Snapshot) DetermineDiffState(path string) diff_state.DiffState {
	state, _ := s.DiffStateAndInfo(path)
	return state
}

// DiffStateAndInfo determines the diff state like DetermineDiffState, and also returns the file info of the path in
// the snapshot (nil if the snapshot does not contain it). Both come from a single Lstat of each side.
func (s *Snapshot) DiffStateAndInfo(path string) (diff_state.DiffState, os.FileInfo) {
	snapshotInfo, err := os.Lstat(s.GetSnapshotPath(path))
	if os.IsNotExist(err) {
		snapshotInfo = nil
	} else if err != nil {
		logging.Error("Could not determine if snapshot contains file %s: %s", path, err.Error())
		return diff_state.Unknown, nil
	}
	realInfo, err := os.Lstat(path)
	if err != nil {
		realInfo = nil
	}

	switch {
	case snapshotInfo != nil && realInfo != nil:
		if isFileInfoDifferent(realInfo, snapshotInfo) {
			return diff_state.Modified, snapshotInfo
		}
		return diff_state.Equal, snapshotInfo
	case snapshotInfo != nil:
		return diff_state.Deleted, snapshotInfo
	case realInfo != nil:
		return diff_state.Added, nil
	}
	return diff_state.Equal, nil
}

// DetermineDiffStateBetween Determine the diff state between this snapshot and another snapshot of a file
func (s *Snapshot) DetermineDiffStateBetween(path string, prev *Snapshot) diff_state.DiffState {
	sContains, err := s.ContainsFile(path)
	if err != nil {
		logging.Error("Could not determine if snapshot %s contains file %s: %s", s.Name, path, err.Error())
		return diff_state.Unknown
	}

	if prev == nil {
		if sContains {
			return diff_state.Added
		}
		return diff_state.Equal
	}

	prevContains, err := prev.ContainsFile(path)
	if err != nil {
		logging.Error("Could not determine if snapshot %s contains file %s: %s", prev.Name, path, err.Error())
		return diff_state.Unknown
	}

	if sContains && prevContains {
		sPath := s.GetSnapshotPath(path)
		prevPath := prev.GetSnapshotPath(path)

		sStat, err := os.Lstat(sPath)
		if err != nil {
			return diff_state.Unknown
		}
		prevStat, err := os.Lstat(prevPath)
		if err != nil {
			return diff_state.Unknown
		}

		if sStat.IsDir() != prevStat.IsDir() ||
			sStat.Mode() != prevStat.Mode() ||
			sStat.ModTime() != prevStat.ModTime() ||
			sStat.Size() != prevStat.Size() {
			return diff_state.Modified
		}
		return diff_state.Equal
	} else if sContains {
		return diff_state.Added
	} else if prevContains {
		return diff_state.Deleted
	}

	return diff_state.Equal
}

// Clone creates a new dataset with the given name from this snapshot ("zfs clone").
// The target must be in the same pool. The clone is created even if it cannot be mounted (e.g. without root),
// in which case an error is returned nonetheless.
// This spawns zfs processes, so it must not be called on the UI thread.
func (s *Snapshot) Clone(targetName string) error {
	if strings.TrimSpace(targetName) == "" {
		return errors.New("the name of the clone must not be empty")
	}
	if strings.HasPrefix(targetName, "-") {
		// zfs would take it for an option
		return fmt.Errorf("invalid name of the clone: %s", targetName)
	}
	err := backend.CloneSnapshot(s.FullName, targetName)
	targetParent := path2.Dir(targetName)
	return explainPermissionError(err, "clone the snapshot",
		PermissionGap{Dataset: datasetOfSnapshot(s.FullName), Permissions: []Permission{PermissionClone}},
		PermissionGap{Dataset: targetParent, Permissions: []Permission{PermissionCreate, PermissionMount}},
	)
}

// SuggestCloneName returns a name for a clone of this snapshot, next to its parent dataset,
// e.g. "pool/data-daily-2026" for "pool/data@daily-2026".
func (s *Snapshot) SuggestCloneName() string {
	datasetName, snapshotName, found := strings.Cut(s.FullName, "@")
	if !found {
		return ""
	}
	return fmt.Sprintf("%s-%s", datasetName, snapshotName)
}

func (s *Snapshot) Destroy(recursive bool, dependantClones bool) error {
	ds := s.ParentDataset
	return ds.DestroySnapshot(s.Name, recursive, dependantClones)
}

// SnapshotProperties are the properties of a snapshot shown by the application, see Backend.SnapshotProperties.
type SnapshotProperties struct {
	CreationDate time.Time
	Used         uint64
	Referenced   uint64
	// Written is the space written to the dataset between the previous snapshot and this one ("written"):
	// 0 for a snapshot in which nothing changed. Deleting data does not count, see Used for that.
	Written          uint64
	CompressionRatio float64
	Clones           uint64
	// Holds is the number of holds ("userrefs"). A held snapshot cannot be destroyed.
	Holds uint64
}

func syncFileProperties(dstPath string, stat os.FileInfo) error {
	err := os.Chmod(dstPath, stat.Mode())
	if err != nil {
		return err
	}

	if stat, ok := stat.Sys().(*syscall.Stat_t); ok {
		err = os.Chown(dstPath, int(stat.Uid), int(stat.Gid))
		if err != nil {
			return err
		}
		aTime := time.Unix(stat.Atim.Unix())
		mTime := time.Unix(stat.Mtim.Unix())
		err = os.Chtimes(dstPath, aTime, mTime)
		if err != nil {
			return err
		}
	} else {
		logging.Error("Could not sync timestamps")
	}

	return nil
}
