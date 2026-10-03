package zfs

import (
	"errors"
	"fmt"
	"io"
	"os"
	path2 "path"
	"strconv"
	"strings"
	"syscall"
	"time"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/util"

	golibzfs "github.com/kraudcloud/go-libzfs"
	gozfs "github.com/mistifyio/go-zfs/v4"
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

	rawGozfsData    *gozfs.Dataset
	rawGolibzfsData *golibzfs.Dataset
	Properties      SnapshotProperties
}

func (s *Snapshot) Equal(e Snapshot) bool {
	return s.Name == e.Name && s.Path == e.Path
}

func NewSnapshot(name string, path string, parentDataset *Dataset, s *golibzfs.Dataset) *Snapshot {
	fullName := fmt.Sprintf("%s@%s", parentDataset.GetName(), name)
	snapshot := &Snapshot{
		Name:            name,
		FullName:        fullName,
		Path:            path,
		ParentDataset:   parentDataset,
		rawGolibzfsData: s,
	}

	if s != nil {
		snapshot.FetchDetails()
		return snapshot
	}

	rawGoufsData, err := gozfs.Snapshots(fullName)
	if err != nil {
		logging.Error("NewSnapshot: gozfs snapshot failed: %s", err.Error())
		return snapshot
	} else if len(rawGoufsData) > 0 {
		snapshot.rawGozfsData = rawGoufsData[0]
	}

	snapshot.FetchDetails()

	return snapshot
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

func (s *Snapshot) RestoreRecursive(srcPath string) error {
	stat, err := os.Lstat(srcPath)
	if err != nil {
		return err
	}
	dstPath := s.GetRealPath(srcPath)
	if stat.IsDir() {
		err = s.RestoreDir(dstPath, stat)
		if err != nil {
			return err
		}

		files, err := util.ListFilesIn(srcPath)
		if err != nil {
			logging.Fatal("Cannot list path: %s", err.Error())
			return err
		}
		for _, file := range files {
			stat, err = os.Lstat(file)
			if err != nil {
				return err
			}
			if stat.IsDir() {
				err = s.RestoreRecursive(file)
				if err != nil {
					return err
				}
			} else {
				err = s.RestoreFile(file)
				if err != nil {
					return err
				}
			}
		}
	} else {
		err = s.RestoreFile(srcPath)
		if err != nil {
			return err
		}
	}

	// TODO: we have to sync file properties from bottom to top, to avoid
	//  affecting the modtime of folders due to changes of files within them

	return err
}

func (s *Snapshot) Restore(srcPath string) error {
	stat, err := os.Lstat(srcPath)
	if err != nil {
		return err
	}
	dstPath := s.GetRealPath(srcPath)
	if stat.IsDir() {
		err = s.RestoreDir(dstPath, stat)
		if err != nil {
			return err
		}
	} else {
		err = s.RestoreFile(srcPath)
		if err != nil {
			return err
		}
	}
	return err
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

func (s *Snapshot) RestoreFile(srcPath string) error {
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return err
	}

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

	destFile, err := os.Create(dstPath) // creates if file doesn't exist
	if err != nil {
		return err
	}

	_, err = io.Copy(destFile, srcFile) // check first var for number of bytes copied
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
	err = srcFile.Close()
	if err != nil {
		return err
	}

	err = syncFileProperties(dstPath, stat)
	if err != nil {
		return err
	}

	return err
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
	snapshots, err := gozfs.Snapshots(s.FullName)
	if err != nil {
		return cleanZfsError(err)
	}
	if len(snapshots) == 0 {
		return fmt.Errorf("snapshot not found: %s", s.FullName)
	}
	_, err = snapshots[0].Clone(targetName, nil)
	targetParent := path2.Dir(targetName)
	return explainPermissionError(cleanZfsError(err), "clone the snapshot",
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

func (s *Snapshot) GetCreationDate() time.Time {
	if s.rawGolibzfsData != nil {
		prop, err := s.rawGolibzfsData.GetProperty(golibzfs.DatasetPropCreation)
		if err == nil {
			timestamp, err := strconv.ParseInt(prop.Value, 10, 64)
			if err == nil {
				return time.Unix(timestamp, 0)
			}
		}
	}

	if s.rawGozfsData == nil {
		return time.Time{}
	}
	propValue, err := s.rawGozfsData.GetProperty("creation")
	if err != nil {
		logging.Error("Could not get creation property for %s: %s", s.FullName, err.Error())
		return time.Time{}
	}

	timestamp, err := strconv.ParseInt(propValue, 10, 64)
	if err != nil {
		logging.Error("Could not parse creation property for %s: %s", s.FullName, err.Error())
		return time.Time{}
	}

	return time.Unix(timestamp, 0)
}

func (s *Snapshot) GetUsed() uint64 {
	if s.rawGolibzfsData == nil {
		return 0
	}
	prop, err := s.rawGolibzfsData.GetProperty(golibzfs.DatasetPropUsed)
	if err != nil {
		logging.Error("Could not get used property for %s: %s", s.FullName, err.Error())
		return 0
	}
	used, err := strconv.ParseUint(prop.Value, 10, 64)
	if err != nil {
		logging.Error("Could not parse used property for %s: %s", s.FullName, err.Error())
		return 0
	}
	return used
}

func (s *Snapshot) GetReferenced() uint64 {
	if s.rawGolibzfsData == nil {
		return 0
	}
	prop, err := s.rawGolibzfsData.GetProperty(golibzfs.DatasetPropReferenced)
	if err != nil {
		logging.Error("Could not get referenced property for %s: %s", s.FullName, err.Error())
		return 0
	}
	referenced, err := strconv.ParseUint(prop.Value, 10, 64)
	if err != nil {
		logging.Error("Could not parse referenced property for %s: %s", s.FullName, err.Error())
		return 0
	}
	return referenced
}

// GetWritten returns the space written to the dataset between the previous snapshot and this one.
func (s *Snapshot) GetWritten() uint64 {
	if s.rawGolibzfsData == nil {
		return 0
	}
	prop, err := s.rawGolibzfsData.GetProperty(golibzfs.DatasetPropWritten)
	if err != nil {
		logging.Error("Could not get written property for %s: %s", s.FullName, err.Error())
		return 0
	}
	written, err := strconv.ParseUint(prop.Value, 10, 64)
	if err != nil {
		logging.Error("Could not parse written property for %s: %s", s.FullName, err.Error())
		return 0
	}
	return written
}

func (s *Snapshot) GetRatio() float64 {
	if s.rawGolibzfsData == nil {
		return 0
	}
	prop, err := s.rawGolibzfsData.GetProperty(golibzfs.DatasetPropRefratio)
	if err != nil {
		logging.Error("Could not get ratio property for %s: %s", s.FullName, err.Error())
		return 0
	}
	val, err := strconv.ParseFloat(prop.Value, 64)
	if err != nil {
		logging.Error("Could not parse ratio property for %s: %s", s.FullName, err.Error())
		return 0
	}
	return val
}

func (s *Snapshot) GetClones() uint64 {
	if s.rawGolibzfsData == nil {
		return 0
	}
	prop, err := s.rawGolibzfsData.GetProperty(golibzfs.DatasetPropNumclones)
	if err != nil {
		logging.Error("Could not get clones property for %s: %s", s.FullName, err.Error())
		return 0
	}
	val, err := strconv.ParseUint(prop.Value, 10, 64)
	if err != nil {
		logging.Error("Could not parse clones property for %s: %s", s.FullName, err.Error())
		return 0
	}
	return val
}

// GetHolds returns the number of holds of the snapshot (see ListHolds for their tags).
func (s *Snapshot) GetHolds() uint64 {
	if s.rawGolibzfsData == nil {
		return 0
	}
	prop, err := s.rawGolibzfsData.GetProperty(golibzfs.DatasetPropUserrefs)
	if err != nil {
		logging.Error("Could not get userrefs property for %s: %s", s.FullName, err.Error())
		return 0
	}
	val, err := strconv.ParseUint(prop.Value, 10, 64)
	if err != nil {
		logging.Error("Could not parse userrefs property for %s: %s", s.FullName, err.Error())
		return 0
	}
	return val
}

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

func (s *Snapshot) FetchDetails() {
	s.Properties = SnapshotProperties{
		CreationDate:     s.GetCreationDate(),
		Used:             s.GetUsed(),
		Referenced:       s.GetReferenced(),
		Written:          s.GetWritten(),
		CompressionRatio: s.GetRatio(),
		Clones:           s.GetClones(),
		Holds:            s.GetHolds(),
	}
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
