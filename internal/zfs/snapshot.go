package zfs

import (
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
