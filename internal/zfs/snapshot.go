package zfs

import (
	golibzfs "github.com/kraudcloud/go-libzfs"
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
)

type Snapshot struct {
	Name          string
	Path          string
	ParentDataset *Dataset
	Date          *time.Time

	internalSnapshot *golibzfs.Dataset
}

func (s *Snapshot) Equal(e Snapshot) bool {
	return s.Name == e.Name && s.Path == e.Path
}

func NewSnapshot(name string, path string, parentDataset *Dataset, date *time.Time, s *golibzfs.Dataset) *Snapshot {
	snapshot := &Snapshot{
		Name:             name,
		Path:             path,
		ParentDataset:    parentDataset,
		Date:             date,
		internalSnapshot: s,
	}

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

func (s *Snapshot) CheckIfFileHasChanged(path string) bool {
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

	return realStat.IsDir() != snapStat.IsDir() ||
		realStat.Mode() != snapStat.Mode() ||
		realStat.ModTime() != snapStat.ModTime() ||
		realStat.Size() != snapStat.Size() ||
		realStat.Name() != snapStat.Name()
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

func (s *Snapshot) DetermineDiffState(path string) diff_state.DiffState {
	containsFile, err := s.ContainsFile(path)
	if err != nil {
		logging.Error(err.Error())
		return diff_state.Unknown
	}
	if containsFile {
		if s.CheckIfFileHasChanged(path) {
			return diff_state.Modified
		} else {
			return diff_state.Equal
		}
	} else {
		return diff_state.Added
	}
}

func (s *Snapshot) Destroy() error {
	ds := s.ParentDataset
	return ds.DestroySnapshot(s.Name, false)
}

func (s *Snapshot) DestroyRecursive() error {
	ds := s.ParentDataset
	return ds.DestroySnapshot(s.Name, true)
}

func (s *Snapshot) GetUsed() uint64 {
	used, err := strconv.ParseUint(s.internalSnapshot.Properties[golibzfs.DatasetPropUsed].Value, 10, 64)
	if err != nil {
		logging.Error("Could not parse used property: %s", err.Error())
		return 0
	}
	return used
}

func (s *Snapshot) GetReferenced() uint64 {
	referenced, err := strconv.ParseUint(s.internalSnapshot.Properties[golibzfs.DatasetPropReferenced].Value, 10, 64)
	if err != nil {
		logging.Error("Could not parse referenced property: %s", err.Error())
		return 0
	}
	return referenced
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
