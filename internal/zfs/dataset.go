package zfs

import (
	"errors"
	"fmt"
	"os"
	path2 "path"
	"strconv"
	"time"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/util"

	golibzfs "github.com/kraudcloud/go-libzfs"
	gozfs "github.com/mistifyio/go-zfs/v4"
)

type Dataset struct {
	Path          string
	HiddenZfsPath string

	rawGozfsData    *gozfs.Dataset
	rawGolibzfsData *golibzfs.Dataset
}

func NewDataset(path string, hiddenZfsPath string) (*Dataset, error) {
	dataset := &Dataset{
		Path:          path,
		HiddenZfsPath: hiddenZfsPath,
	}

	ds := findDataset(allDatasets, path)
	dataset.rawGolibzfsData = ds

	datasets, err := gozfs.Filesystems(path)
	if err != nil {
		return dataset, err
	} else {
		if len(datasets) > 0 {
			dataset.rawGozfsData = datasets[0]
		}
		if len(datasets) > 1 {
			// TODO: is this a real case?
			//  Can we automatically select the "correct" one or should the user
			//  decide which one to use?
			logging.Warning("Found multiple datasets for path %s", path)
		}
	}

	return dataset, nil
}

// FindHostDataset returns the root path of the dataset containing this path
func FindHostDataset(path string) (*Dataset, error) {
	if path == "" {
		return nil, errors.New("cannot find host dataset for empty path")
	}

	var currentPath = path
	for {
		pathToTest := path2.Join(currentPath, ".zfs")
		stat, err := os.Lstat(pathToTest)
		if os.IsNotExist(err) || !stat.IsDir() {
			old := currentPath
			currentPath = path2.Dir(currentPath)
			if old == currentPath {
				return nil, fmt.Errorf("could not find dataset for path: %s", path)
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

// GetSnapshots returns all snapshots for this dataset
// Note: depending on the amount of snapshots, this can be a slow operation.
func (dataset *Dataset) GetSnapshots() ([]*Snapshot, error) {
	var result []*Snapshot

	snapshotDirs, err := util.ListFilesIn(dataset.GetSnapshotsDir())
	if err != nil {
		return []*Snapshot{}, err
	}
	for _, file := range snapshotDirs {
		_, name := path2.Split(file)

		s := findSnapshot(AllSnapshots[dataset.GetName()], name)
		if s != nil {
		} else {
			logging.Warning("Could not find snapshot %s on dataset %s", name, dataset.GetName())
		}

		result = append(result, NewSnapshot(name, file, dataset, s))
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
			logging.Error("Could not get name property for dataset %s: %s", dataset.Path, err.Error())
		} else {
			return nameProperty.Value
		}
	}
	return dataset.Path
}

func (dataset *Dataset) GetCreationString() time.Time {
	if dataset.rawGolibzfsData != nil {
		prop, err := dataset.rawGolibzfsData.GetProperty(golibzfs.DatasetPropCreation)
		if err == nil {
			rawValue := prop.Value
			valueInt, err := strconv.ParseInt(rawValue, 10, 64)
			if err != nil {
				logging.Error("Could not parse creation time for dataset %s: %s", dataset.Path, err.Error())
				return time.Time{}
			}
			// convert integer to *time.Time
			return time.Unix(valueInt, 0)
		}
	}
	return time.Time{}
}

func (dataset *Dataset) GetType() string {
	if dataset.rawGolibzfsData != nil {
		prop, err := dataset.rawGolibzfsData.GetProperty(golibzfs.DatasetPropType)
		if err == nil {
			return prop.Value
		}
	}
	if dataset.rawGozfsData != nil {
		return dataset.rawGozfsData.Type
	}
	return ""
}

func (dataset *Dataset) GetMountPoint() string {
	if dataset.rawGolibzfsData != nil {
		prop, err := dataset.rawGolibzfsData.GetProperty(golibzfs.DatasetPropMountpoint)
		if err == nil {
			return prop.Value
		}
	}
	if dataset.rawGozfsData != nil {
		return dataset.rawGozfsData.Mountpoint
	}
	return ""
}

func (dataset *Dataset) GetVolSize() uint64 {
	if dataset.rawGolibzfsData != nil {
		prop, err := dataset.rawGolibzfsData.GetProperty(golibzfs.DatasetPropVolsize)
		if err == nil {
			number, err := strconv.ParseUint(prop.Value, 10, 64)
			if err == nil {
				return number
			}
		}
	}
	if dataset.rawGozfsData != nil {
		return dataset.rawGozfsData.Volsize
	}
	return 0
}

func (dataset *Dataset) GetAvailable() uint64 {
	if dataset.rawGolibzfsData != nil {
		prop, err := dataset.rawGolibzfsData.GetProperty(golibzfs.DatasetPropAvailable)
		if err == nil {
			number, err := strconv.ParseUint(prop.Value, 10, 64)
			if err == nil {
				return number
			}
		}
	}
	if dataset.rawGozfsData != nil {
		return dataset.rawGozfsData.Avail
	}
	return 0
}

func (dataset *Dataset) GetUsed() uint64 {
	if dataset.rawGolibzfsData != nil {
		prop, err := dataset.rawGolibzfsData.GetProperty(golibzfs.DatasetPropUsed)
		if err == nil {
			number, err := strconv.ParseUint(prop.Value, 10, 64)
			if err == nil {
				return number
			}
		}
	}
	if dataset.rawGozfsData != nil {
		return dataset.rawGozfsData.Used
	}
	return 0
}

func (dataset *Dataset) GetCompression() string {
	if dataset.rawGolibzfsData != nil {
		prop, err := dataset.rawGolibzfsData.GetProperty(golibzfs.DatasetPropCompression)
		if err == nil {
			return prop.Value
		}
	}
	if dataset.rawGozfsData != nil {
		return dataset.rawGozfsData.Compression
	}
	return ""
}

func (dataset *Dataset) GetCompressRatio() string {
	if dataset.rawGolibzfsData != nil {
		prop, err := dataset.rawGolibzfsData.GetProperty(golibzfs.DatasetPropCompressratio)
		if err == nil {
			return prop.Value
		}
	}
	return ""
}

func (dataset *Dataset) GetOrigin() string {
	if dataset.rawGolibzfsData != nil {
		prop, err := dataset.rawGolibzfsData.GetProperty(golibzfs.DatasetPropOrigin)
		if err == nil {
			return prop.Value
		}
	}
	if dataset.rawGozfsData != nil {
		return dataset.rawGozfsData.Origin
	}
	return ""
}

func (dataset *Dataset) GetSnapshotCount() int {
	if dataset.rawGolibzfsData != nil {
		prop, err := dataset.rawGolibzfsData.GetProperty(golibzfs.DatasetPropSnapshotCount)
		if err == nil {
			val, err := strconv.Atoi(prop.Value)
			if err == nil {
				return val
			}
		}
	}
	if dataset.rawGozfsData != nil {
		prop, err := dataset.rawGozfsData.GetProperty("snapshot_count")
		if err == nil {
			val, err := strconv.Atoi(prop)
			if err == nil {
				return val
			}
		}
	}
	return 0
}

// GetSnapshotLimit returns the maximum number of snapshots that can be created
func (dataset *Dataset) GetSnapshotLimit() int {
	if dataset.rawGolibzfsData != nil {
		prop, err := dataset.rawGolibzfsData.GetProperty(golibzfs.DatasetPropSnapshotLimit)
		if err == nil {
			val, err := strconv.Atoi(prop.Value)
			if err == nil {
				return val
			}
		}
	}
	if dataset.rawGozfsData != nil {
		prop, err := dataset.rawGozfsData.GetProperty("snapshot_limit")
		if err == nil {
			val, err := strconv.Atoi(prop)
			if err == nil {
				return val
			}
		}
	}
	return -1
}

func (dataset *Dataset) GetMounted() string {
	if dataset.rawGolibzfsData != nil {
		prop, err := dataset.rawGolibzfsData.GetProperty(golibzfs.DatasetPropMounted)
		if err == nil {
			return prop.Value
		}
	}
	if dataset.rawGozfsData != nil {
		val, err := dataset.rawGozfsData.GetProperty("mounted")
		if err == nil {
			return val
		}
	}
	return ""
}

// on or off
func (dataset *Dataset) GetReadonly() string {
	if dataset.rawGolibzfsData != nil {
		prop, err := dataset.rawGolibzfsData.GetProperty(golibzfs.DatasetPropReadonly)
		if err == nil {
			return prop.Value
		}
	}
	if dataset.rawGozfsData != nil {
		val, err := dataset.rawGozfsData.GetProperty("readonly")
		if err == nil {
			return val
		}
	}
	return ""
}

func (dataset *Dataset) GetSnapdir() string {
	if dataset.rawGolibzfsData != nil {
		prop, err := dataset.rawGolibzfsData.GetProperty(golibzfs.DatasetPropSnapdir)
		if err == nil {
			return prop.Value
		}
	}
	if dataset.rawGozfsData != nil {
		val, err := dataset.rawGozfsData.GetProperty("snapdir")
		if err == nil {
			return val
		}
	}
	return ""
}

func (dataset *Dataset) GetCaseSensitivity() string {
	if dataset.rawGolibzfsData != nil {
		prop, err := dataset.rawGolibzfsData.GetProperty(golibzfs.DatasetPropCase)
		if err == nil {
			return prop.Value
		}
	}
	if dataset.rawGozfsData != nil {
		val, err := dataset.rawGozfsData.GetProperty("case")
		if err == nil {
			return val
		}
	}
	return ""
}

func (dataset *Dataset) GetQuota() string {
	if dataset.rawGolibzfsData != nil {
		prop, err := dataset.rawGolibzfsData.GetProperty(golibzfs.DatasetPropQuota)
		if err == nil {
			return prop.Value
		}
	}
	if dataset.rawGozfsData != nil {
		val, err := dataset.rawGozfsData.GetProperty("quota")
		if err == nil {
			return val
		}
	}
	return ""
}

func (dataset *Dataset) IsEncrypted() bool {
	if dataset.rawGolibzfsData != nil {
		prop, err := dataset.rawGolibzfsData.GetProperty(golibzfs.DatasetPropEncryption)
		if err == nil {
			return prop.Value != "off"
		}
	}
	if dataset.rawGozfsData != nil {
		val, err := dataset.rawGozfsData.GetProperty("encryption")
		if err == nil {
			return val != "off"
		}
	}
	return false
}

func (dataset *Dataset) GetEncryption() string {
	if dataset.rawGolibzfsData != nil {
		prop, err := dataset.rawGolibzfsData.GetProperty(golibzfs.DatasetPropEncryption)
		if err == nil {
			return prop.Value
		}
	}
	if dataset.rawGozfsData != nil {
		val, err := dataset.rawGozfsData.GetProperty("encryption")
		if err == nil {
			return val
		}
	}
	return ""
}

func (dataset *Dataset) GetKeyStatus() string {
	if dataset.rawGolibzfsData != nil {
		prop, err := dataset.rawGolibzfsData.GetProperty(golibzfs.DatasetPropKeyStatus)
		if err == nil {
			return prop.Value
		}
	}
	if dataset.rawGozfsData != nil {
		val, err := dataset.rawGozfsData.GetProperty("key_status")
		if err == nil {
			return val
		}
	}
	return ""
}

func (dataset *Dataset) CreateSnapshot(name string) error {
	if dataset.rawGolibzfsData != nil {
		_, err := golibzfs.DatasetCreate(dataset.GetMountPoint()+"@"+name, golibzfs.DatasetTypeSnapshot, nil)
		if err != nil {
			return err
		}
		return nil
	}
	if dataset.rawGozfsData != nil {
		_, err := dataset.rawGozfsData.Snapshot(name, false)
		if err != nil {
			return err
		}
		return nil
	}
	return errors.New("missing ZFS data, could not create snapshot")
}

func (dataset *Dataset) DestroySnapshot(name string, recursive bool, dependantClones bool) (err error) {
	if dataset.rawGozfsData != nil {
		flags := gozfs.DestroyDefault
		if recursive {
			flags = gozfs.DestroyRecursive
		}
		if dependantClones {
			flags = flags | gozfs.DestroyRecursiveClones
		}
		err = dataset.rawGozfsData.Destroy(flags)
		if err != nil {
			return err
		}
		return nil
	}
	if dataset.rawGolibzfsData != nil {
		snapshot := findSnapshot(AllSnapshots[dataset.GetName()], name)
		if snapshot == nil {
			return errors.New("could not find snapshot")
		} else {
			if recursive {
				err = snapshot.DestroyRecursive()
			} else {
				err = snapshot.Destroy(false)
			}
			if err != nil {
				return err
			}
		}
		return nil
	}
	return errors.New("missing ZFS data, could not destroy snapshot")
}
