package zfs

import (
	"io"
	"os"
	path2 "path"
	"strings"
	"syscall"
	"time"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/util"
)

type Snapshot struct {
	Name          string
	Path          string
	ParentDataset *Dataset
	Date          *time.Time
}

func (s *Snapshot) Equal(e Snapshot) bool {
	return s.Name == e.Name && s.Path == e.Path
}

func NewSnapshot(name string, path string, parentDataset *Dataset, date *time.Time) *Snapshot {
	snapshot := &Snapshot{
		Name:          name,
		Path:          path,
		ParentDataset: parentDataset,
		Date:          date,
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

func (s *Snapshot) RestoreDirRecursive(srcPath string) error {
	stat, err := os.Stat(srcPath)
	if err != nil {
		return err
	}
	dstPath := s.GetRealPath(srcPath)
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
		stat, err = os.Stat(file)
		if err != nil {
			return err
		}
		if stat.IsDir() {
			err = s.RestoreDirRecursive(s.GetRealPath(file))
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
	defer destFile.Close()

	err = syncFiles(dstPath, destFile, stat)
	if err != nil {
		return err
	}

	return err
}

func (s *Snapshot) RestoreFile(srcPath string) error {
	srcFile, err := os.Open(srcPath)

	stat, err := os.Stat(srcPath)
	if err != nil {
		return err
	}

	defer srcFile.Close()

	dstPath := s.GetRealPath(srcPath)
	destFile, err := os.Create(dstPath) // creates if file doesn't exist
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, srcFile) // check first var for number of bytes copied
	if err != nil {
		return err
	}

	err = syncFiles(dstPath, destFile, stat)
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

	realStat, err := os.Stat(realPath)
	if err != nil {
		return false
	}

	snapStat, err := os.Stat(snapshotPath)
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

func syncFiles(dstPath string, destFile *os.File, stat os.FileInfo) error {
	err := destFile.Sync()
	if err != nil {
		return err
	}

	err = os.Chmod(dstPath, stat.Mode())
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
	}

	return nil
}
