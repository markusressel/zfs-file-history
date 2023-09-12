package zfs

import (
	"os"
	path2 "path"
	"strings"
	"time"
)

type Snapshot struct {
	Name          string
	Path          string
	ParentDataset *Dataset
	Date          *time.Time
}

// GetSnapshotPath returns the corresponding snapshot path of a file on the dataset
func (s *Snapshot) GetSnapshotPath(file string) string {
	fileWithoutBasePath := strings.Replace(file, s.ParentDataset.Path, "", 1)
	snapshotPath := path2.Join(s.ParentDataset.GetSnapshotsDir(), s.Name, fileWithoutBasePath)
	return snapshotPath
}

type SnapshotFile struct {
	Path         string
	OriginalPath string
	Stat         os.FileInfo
	Snapshot     *Snapshot
}
