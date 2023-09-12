package zfs

import (
	gozfs "github.com/mistifyio/go-zfs"
	"os"
	path2 "path"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/util"
)

type Dataset struct {
	Path          string
	HiddenZfsPath string
	ZfsData       *gozfs.Dataset
}

func NewDataset(path string, hiddenZfsPath string) (*Dataset, error) {
	dataset := &Dataset{
		Path:          path,
		HiddenZfsPath: hiddenZfsPath,
	}

	datasets, err := gozfs.Filesystems(path)
	if err != nil {
		return dataset, err
	}
	for _, d := range datasets {
		if d.Mountpoint == path {
			dataset.ZfsData = d
			break
		}
	}

	return dataset, nil
}

// FindHostDataset returns the root path of the dataset containing this path
func FindHostDataset(path string) (*Dataset, error) {
	var dataset *string = nil

	var currentPath = path
	for dataset == nil {
		pathToTest := path2.Join(currentPath, ".zfs")
		_, err := os.Stat(pathToTest)
		if os.IsNotExist(err) {
			logging.Debug(".zfs not found in %s, continuing...", currentPath)
			dir := path2.Dir(currentPath)
			currentPath = dir
			continue

		} else if os.IsPermission(err) {
			return nil, err
		} else if err != nil {
			return nil, err
		} else {
			return NewDataset(currentPath, pathToTest)
		}
	}

	logging.Fatal("Could not find host dataset for path %s", path)
	panic(nil)
}

func (dataset *Dataset) GetSnapshotsDir() string {
	return path2.Join(dataset.HiddenZfsPath, "snapshot")
}

func (dataset *Dataset) GetSnapshots() ([]*Snapshot, error) {
	var result []*Snapshot

	snapshotDirs, err := util.ListFilesIn(dataset.GetSnapshotsDir())
	if err != nil {
		return []*Snapshot{}, err
	}
	for _, file := range snapshotDirs {
		_, name := path2.Split(file)

		result = append(result, &Snapshot{
			Name:          name,
			Path:          file,
			ParentDataset: dataset,
		})
	}

	return result, nil
}
