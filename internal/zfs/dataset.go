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

func (dataset *Dataset) getPropertyString(libProp golibzfs.Prop, goProp string) string {
	if dataset.rawGolibzfsData != nil {
		prop, err := dataset.rawGolibzfsData.GetProperty(libProp)
		if err == nil {
			return prop.Value
		}
	}
	if dataset.rawGozfsData != nil {
		val, err := dataset.rawGozfsData.GetProperty(goProp)
		if err == nil {
			return val
		}
	}
	return ""
}

func (dataset *Dataset) getPropertyInt(libProp golibzfs.Prop, goProp string, defaultValue int) int {
	if dataset.rawGolibzfsData != nil {
		prop, err := dataset.rawGolibzfsData.GetProperty(libProp)
		if err == nil {
			val, err := strconv.Atoi(prop.Value)
			if err == nil {
				return val
			}
		}
	}
	if dataset.rawGozfsData != nil {
		prop, err := dataset.rawGozfsData.GetProperty(goProp)
		if err == nil {
			val, err := strconv.Atoi(prop)
			if err == nil {
				return val
			}
		}
	}
	return defaultValue
}

func (dataset *Dataset) getPropertyUint64(libProp golibzfs.Prop, goValue uint64) uint64 {
	if dataset.rawGolibzfsData != nil {
		prop, err := dataset.rawGolibzfsData.GetProperty(libProp)
		if err == nil {
			number, err := strconv.ParseUint(prop.Value, 10, 64)
			if err == nil {
				return number
			}
		}
	}
	if dataset.rawGozfsData != nil {
		return goValue
	}
	return 0
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
	return dataset.getPropertyString(golibzfs.DatasetPropType, "type")
}

func (dataset *Dataset) GetMountPoint() string {
	return dataset.getPropertyString(golibzfs.DatasetPropMountpoint, "mountpoint")
}

func (dataset *Dataset) GetVolSize() uint64 {
	var goValue uint64
	if dataset.rawGozfsData != nil {
		goValue = dataset.rawGozfsData.Volsize
	}
	return dataset.getPropertyUint64(golibzfs.DatasetPropVolsize, goValue)
}

func (dataset *Dataset) GetAvailable() uint64 {
	var goValue uint64
	if dataset.rawGozfsData != nil {
		goValue = dataset.rawGozfsData.Avail
	}
	return dataset.getPropertyUint64(golibzfs.DatasetPropAvailable, goValue)
}

func (dataset *Dataset) GetUsed() uint64 {
	var goValue uint64
	if dataset.rawGozfsData != nil {
		goValue = dataset.rawGozfsData.Used
	}
	return dataset.getPropertyUint64(golibzfs.DatasetPropUsed, goValue)
}

func (dataset *Dataset) GetCompression() string {
	return dataset.getPropertyString(golibzfs.DatasetPropCompression, "compression")
}

func (dataset *Dataset) GetCompressRatio() string {
	return dataset.getPropertyString(golibzfs.DatasetPropCompressratio, "compressratio")
}

func (dataset *Dataset) GetOrigin() string {
	return dataset.getPropertyString(golibzfs.DatasetPropOrigin, "origin")
}

func (dataset *Dataset) GetSnapshotCount() int {
	return dataset.getPropertyInt(golibzfs.DatasetPropSnapshotCount, "snapshot_count", 0)
}

// GetSnapshotLimit returns the maximum number of snapshots that can be created
func (dataset *Dataset) GetSnapshotLimit() int {
	return dataset.getPropertyInt(golibzfs.DatasetPropSnapshotLimit, "snapshot_limit", -1)
}

func (dataset *Dataset) GetMounted() string {
	return dataset.getPropertyString(golibzfs.DatasetPropMounted, "mounted")
}

// on or off
func (dataset *Dataset) GetReadonly() string {
	return dataset.getPropertyString(golibzfs.DatasetPropReadonly, "readonly")
}

func (dataset *Dataset) GetSnapdir() string {
	return dataset.getPropertyString(golibzfs.DatasetPropSnapdir, "snapdir")
}

func (dataset *Dataset) GetCaseSensitivity() string {
	return dataset.getPropertyString(golibzfs.DatasetPropCase, "case")
}

func (dataset *Dataset) GetQuota() string {
	return dataset.getPropertyString(golibzfs.DatasetPropQuota, "quota")
}

func (dataset *Dataset) IsEncrypted() bool {
	return dataset.getPropertyString(golibzfs.DatasetPropEncryption, "encryption") != "off"
}

func (dataset *Dataset) GetEncryption() string {
	return dataset.getPropertyString(golibzfs.DatasetPropEncryption, "encryption")
}

func (dataset *Dataset) GetKeyStatus() string {
	return dataset.getPropertyString(golibzfs.DatasetPropKeyStatus, "key_status")
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
