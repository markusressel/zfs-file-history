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
	} else {
		if len(datasets) > 0 {
			dataset.ZfsData = datasets[0]
		}
	}

	return dataset, nil
}

// FindHostDataset returns the root path of the dataset containing this path
func FindHostDataset(path string) (*Dataset, error) {
	var dataset *string = nil

	var currentPath = path
	for dataset == nil {
		for {
			stat, err := os.Stat(currentPath)
			if err != nil || !stat.IsDir() {
				currentPath = path2.Dir(currentPath)
				continue
			} else {
				break
			}
		}

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

		// TODO: figure out date somehow
		result = append(result, NewSnapshot(name, file, dataset, nil))
	}

	return result, nil
}
