package zfs

import (
	golibzfs "github.com/kraudcloud/go-libzfs"
	"strings"
	"zfs-file-history/internal/logging"
)

const (
	SnapshotTimeFormat = "2006-01-02-150405"
)

var (
	allDatasets  = []golibzfs.Dataset{}
	AllSnapshots = map[string][]golibzfs.Dataset{}
)

func RefreshZfsData() {
	loadDatasets()
	loadSnapshots(allDatasets)
}

func loadDatasets() {
	datasets, err := golibzfs.DatasetOpenAll()
	if err != nil {
		logging.Error(err.Error())
	} else {
		allDatasets = datasets
	}
}

func loadSnapshots(datasets []golibzfs.Dataset) {
	for _, dataset := range datasets {
		nameProperty := dataset.Properties[golibzfs.DatasetPropName]
		//mountPointProperty := dataset.Properties[golibzfs.DatasetPropMountpoint]
		snapshots, err := dataset.Snapshots()
		if err != nil {
			logging.Error(err.Error())
		} else {
			AllSnapshots[nameProperty.Value] = snapshots
		}
		loadSnapshots(dataset.Children)
	}
}

func findDataset(datasets []golibzfs.Dataset, path string) *golibzfs.Dataset {
	for _, dataset := range datasets {
		mountPoint := dataset.Properties[golibzfs.DatasetPropMountpoint]
		if mountPoint.Value == path {
			return &dataset
		}
		dataset := findDataset(dataset.Children, path)
		if dataset != nil {
			return dataset
		}
	}
	return nil
}

func findSnapshot(snapshots []golibzfs.Dataset, name string) *golibzfs.Dataset {
	for _, snapshot := range snapshots {
		nameProperty := snapshot.Properties[golibzfs.DatasetPropName]
		currentName := strings.Split(nameProperty.Value, "@")[1]
		if currentName == name {
			return &snapshot
		}
		snapshot := findSnapshot(snapshot.Children, name)
		if snapshot != nil {
			return snapshot
		}
	}
	return nil
}
