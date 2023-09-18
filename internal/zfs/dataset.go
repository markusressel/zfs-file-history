package zfs

import (
	"errors"
	golibzfs "github.com/bicomsystems/go-libzfs"
	gozfs "github.com/mistifyio/go-zfs"
	"os"
	path2 "path"
	"strconv"
	"strings"
	"time"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/util"
)

var (
	allDatasets  = []golibzfs.Dataset{}
	AllSnapshots = map[string][]golibzfs.Dataset{}
)

func init() {
	RefreshZfsData()
}

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

type Dataset struct {
	Path          string
	HiddenZfsPath string
	zfsData       *gozfs.Dataset
	rawDataset    *golibzfs.Dataset
}

type ZfsData struct {
	Name          string
	Origin        string
	Used          uint64
	Avail         uint64
	Mountpoint    string
	Compression   string
	Type          string
	Written       uint64
	Volsize       uint64
	Usedbydataset uint64
	Logicalused   uint64
	Quota         uint64
}

func NewDataset(path string, hiddenZfsPath string) (*Dataset, error) {
	dataset := &Dataset{
		Path:          path,
		HiddenZfsPath: hiddenZfsPath,
	}

	ds := findDataset(allDatasets, path)
	if dataset != nil {
		dataset.rawDataset = ds
		return dataset, nil
	}
	datasets, err := gozfs.Filesystems(path)
	if err != nil {
		return dataset, err
	} else {
		if len(datasets) > 0 {
			dataset.zfsData = datasets[0]
		}
	}

	return dataset, nil
}

// FindHostDataset returns the root path of the dataset containing this path
func FindHostDataset(path string) (*Dataset, error) {
	if path == "" {
		return nil, errors.New("Cannot find host dataset for empty path")
	}
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

		var creationDate time.Time
		s := findSnapshot(AllSnapshots[dataset.GetName()], name)
		if s != nil {
			creationDateProperty := s.Properties[golibzfs.DatasetPropCreation]
			creationDateTimestamp, err := strconv.ParseInt(creationDateProperty.Value, 10, 64)
			if err != nil {
				logging.Error(err.Error())
			} else {
				creationDate = time.Unix(creationDateTimestamp, 0)
			}
		} else {
			logging.Warning("Could not find snapshot %s on dataset %s", name, dataset.Path)
		}

		result = append(result, NewSnapshot(name, file, dataset, &creationDate))
	}

	return result, nil
}

func (dataset *Dataset) GetName() string {
	if dataset.rawDataset != nil {
		nameProperty, err := dataset.rawDataset.GetProperty(golibzfs.DatasetPropName)
		if err != nil {
			logging.Error(err.Error())
		} else {
			return nameProperty.Value
		}
	}
	if dataset.zfsData != nil {
		return dataset.zfsData.Name
	}
	return dataset.Path
}

func (dataset *Dataset) GetType() string {
	if dataset.zfsData != nil {
		return dataset.zfsData.Type
	}
	return ""
}

func (dataset *Dataset) GetMountPoint() string {
	if dataset.zfsData != nil {
		return dataset.zfsData.Mountpoint
	}
	return ""
}

func (dataset *Dataset) GetVolSize() uint64 {
	if dataset.zfsData != nil {
		return dataset.zfsData.Volsize
	}
	return 0
}

func (dataset *Dataset) GetAvailable() uint64 {
	if dataset.zfsData != nil {
		return dataset.zfsData.Avail
	}
	return 0
}

func (dataset *Dataset) GetUsed() uint64 {
	if dataset.zfsData != nil {
		return dataset.zfsData.Used
	}
	return 0
}

func (dataset *Dataset) GetCompression() string {
	if dataset.zfsData != nil {
		return dataset.zfsData.Compression
	}
	return ""
}

func (dataset *Dataset) GetOrigin() string {
	if dataset.zfsData != nil {
		return dataset.zfsData.Origin
	}
	return ""
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
