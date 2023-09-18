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
	Path            string
	HiddenZfsPath   string
	rawGozfsData    *gozfs.Dataset
	rawGolibzfsData *golibzfs.Dataset
}

func NewDataset(path string, hiddenZfsPath string) (*Dataset, error) {
	dataset := &Dataset{
		Path:          path,
		HiddenZfsPath: hiddenZfsPath,
	}

	ds := findDataset(allDatasets, path)
	if dataset != nil {
		dataset.rawGolibzfsData = ds
	}

	datasets, err := gozfs.Filesystems(path)
	if err != nil {
		return dataset, err
	} else {
		if len(datasets) > 0 {
			dataset.rawGozfsData = datasets[0]
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
			logging.Warning("Could not find snapshot %s on dataset %s", name, dataset.GetName())
		}

		result = append(result, NewSnapshot(name, file, dataset, &creationDate))
	}

	return result, nil
}

func (dataset *Dataset) GetName() string {
	if dataset.rawGozfsData != nil {
		return dataset.rawGozfsData.Name
	}
	if dataset.rawGolibzfsData != nil {
		nameProperty, err := dataset.rawGolibzfsData.GetProperty(golibzfs.DatasetPropName)
		if err != nil {
			logging.Error(err.Error())
		} else {
			return nameProperty.Value
		}
	}
	return dataset.Path
}

func (dataset *Dataset) GetType() string {
	if dataset.rawGozfsData != nil {
		return dataset.rawGozfsData.Type
	}
	if dataset.rawGolibzfsData != nil {
		prop, err := dataset.rawGolibzfsData.GetProperty(golibzfs.DatasetPropType)
		if err == nil {
			return prop.Value
		}
	}
	return ""
}

func (dataset *Dataset) GetMountPoint() string {
	if dataset.rawGozfsData != nil {
		return dataset.rawGozfsData.Mountpoint
	}
	if dataset.rawGolibzfsData != nil {
		prop, err := dataset.rawGolibzfsData.GetProperty(golibzfs.DatasetPropMountpoint)
		if err == nil {
			return prop.Value
		}
	}
	return ""
}

func (dataset *Dataset) GetVolSize() uint64 {
	if dataset.rawGozfsData != nil {
		return dataset.rawGozfsData.Volsize
	}
	if dataset.rawGolibzfsData != nil {
		prop, err := dataset.rawGolibzfsData.GetProperty(golibzfs.DatasetPropVolsize)
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
	if dataset.rawGozfsData != nil {
		return dataset.rawGozfsData.Avail
	}
	if dataset.rawGolibzfsData != nil {
		prop, err := dataset.rawGolibzfsData.GetProperty(golibzfs.DatasetPropAvailable)
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
	if dataset.rawGozfsData != nil {
		return dataset.rawGozfsData.Used
	}
	if dataset.rawGolibzfsData != nil {
		prop, err := dataset.rawGolibzfsData.GetProperty(golibzfs.DatasetPropUsed)
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
	if dataset.rawGozfsData != nil {
		return dataset.rawGozfsData.Compression
	}
	if dataset.rawGolibzfsData != nil {
		prop, err := dataset.rawGolibzfsData.GetProperty(golibzfs.DatasetPropCompression)
		if err == nil {
			return prop.Value
		}
	}
	return ""
}

func (dataset *Dataset) GetOrigin() string {
	if dataset.rawGozfsData != nil {
		return dataset.rawGozfsData.Origin
	}
	if dataset.rawGolibzfsData != nil {
		prop, err := dataset.rawGolibzfsData.GetProperty(golibzfs.DatasetPropOrigin)
		if err == nil {
			return prop.Value
		}
	}
	return ""
}
