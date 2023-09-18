package zfs

import (
	"errors"
	"fmt"
	golibzfs "github.com/bicomsystems/go-libzfs"
	gozfs "github.com/mistifyio/go-zfs"
	"os"
	path2 "path"
	"strconv"
	"time"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/util"
)

type Dataset struct {
	Path          string
	HiddenZfsPath string
	zfsData       *gozfs.Dataset
	rawDataset    *golibzfs.Dataset
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

	var currentPath = path
	for {
		pathToTest := path2.Join(currentPath, ".zfs")
		stat, err := os.Lstat(pathToTest)
		if os.IsNotExist(err) || !stat.IsDir() {
			old := currentPath
			currentPath = path2.Dir(currentPath)
			if old == currentPath {
				return nil, errors.New(fmt.Sprintf("Could not find dataset for path: %s", path))
			} else {
				continue
			}
		} else if os.IsPermission(err) {
			return nil, err
		} else if err != nil {
			return nil, err
		} else {
			return NewDataset(currentPath, pathToTest)
		}
	}
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
	if dataset.rawDataset != nil {
		prop, err := dataset.rawDataset.GetProperty(golibzfs.DatasetPropType)
		if err == nil {
			return prop.Value
		}
	}
	return ""
}

func (dataset *Dataset) GetMountPoint() string {
	if dataset.zfsData != nil {
		return dataset.zfsData.Mountpoint
	}
	if dataset.rawDataset != nil {
		prop, err := dataset.rawDataset.GetProperty(golibzfs.DatasetPropMountpoint)
		if err == nil {
			return prop.Value
		}
	}
	return ""
}

func (dataset *Dataset) GetVolSize() uint64 {
	if dataset.zfsData != nil {
		return dataset.zfsData.Volsize
	}
	if dataset.rawDataset != nil {
		prop, err := dataset.rawDataset.GetProperty(golibzfs.DatasetPropVolsize)
		if err == nil {
			number, err := strconv.ParseUint(prop.Value, 10, 64)
			if err == nil {
				return number
			}
		}
	}
	return 0
}

func (dataset *Dataset) GetAvailable() uint64 {
	if dataset.zfsData != nil {
		return dataset.zfsData.Avail
	}
	if dataset.rawDataset != nil {
		prop, err := dataset.rawDataset.GetProperty(golibzfs.DatasetPropAvailable)
		if err == nil {
			number, err := strconv.ParseUint(prop.Value, 10, 64)
			if err == nil {
				return number
			}
		}
	}
	return 0
}

func (dataset *Dataset) GetUsed() uint64 {
	if dataset.zfsData != nil {
		return dataset.zfsData.Used
	}
	if dataset.rawDataset != nil {
		prop, err := dataset.rawDataset.GetProperty(golibzfs.DatasetPropUsed)
		if err == nil {
			number, err := strconv.ParseUint(prop.Value, 10, 64)
			if err == nil {
				return number
			}
		}
	}
	return 0
}

func (dataset *Dataset) GetCompression() string {
	if dataset.zfsData != nil {
		return dataset.zfsData.Compression
	}
	if dataset.rawDataset != nil {
		prop, err := dataset.rawDataset.GetProperty(golibzfs.DatasetPropCompression)
		if err == nil {
			return prop.Value
		}
	}
	return ""
}

func (dataset *Dataset) GetOrigin() string {
	if dataset.zfsData != nil {
		return dataset.zfsData.Origin
	}
	if dataset.rawDataset != nil {
		prop, err := dataset.rawDataset.GetProperty(golibzfs.DatasetPropOrigin)
		if err == nil {
			return prop.Value
		}
	}
	return ""
}
