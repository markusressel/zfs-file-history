package zfs

import (
	path2 "path"
	"strings"
)

type Snapshot struct {
	Name          string
	Path          string
	ParentDataset *Dataset
}

// GetPath returns the corresponding snapshot path of a file on the dataset
func (s *Snapshot) GetPath(file string) string {
	fileWithoutBasePath := strings.Replace(file, s.ParentDataset.Path, "", 1)
	snapshotPath := path2.Join(s.ParentDataset.GetSnapshotsDir(), s.Name, fileWithoutBasePath)
	return snapshotPath
}
