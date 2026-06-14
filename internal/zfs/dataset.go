package zfs

import (
	"errors"
	"fmt"
	"os"
	gopath "path"
	"strconv"
	"time"
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
	path = gopath.Clean(path)
	dataset := &Dataset{
		Path:          path,
		HiddenZfsPath: hiddenZfsPath,
	}

	// Try to find the dataset metadata in the golibzfs cache
	ds := findDataset(allDatasets, path)
	if ds != nil {
		dataset.rawGolibzfsData = ds

		// Use the name found in golibzfs to fetch full metadata from gozfs
		nameProp, err := ds.GetProperty(golibzfs.DatasetPropName)
		if err == nil {
			gozfsDs, err := gozfs.GetDataset(nameProp.Value)
			if err == nil {
				dataset.rawGozfsData = gozfsDs
			}
		}
	}

	// Fallback: if we haven't found metadata yet, try mistify/go-zfs directly.
	if dataset.rawGozfsData == nil {
		gozfsList, err := gozfs.Filesystems("")
		if err == nil {
			for _, fs := range gozfsList {
				if gopath.Clean(fs.Mountpoint) == path {
					dataset.rawGozfsData = fs
					break
				}
			}
		}
	}

	return dataset, nil
}

// FindHostDataset returns the root path of the dataset containing this path
func FindHostDataset(path string) (*Dataset, error) {
	if path == "" {
		return nil, errors.New("cannot find host dataset for empty path")
	}

	var currentPath = gopath.Clean(path)
	for {
		pathToTest := gopath.Join(currentPath, ".zfs")
		stat, err := os.Lstat(pathToTest)
		if err == nil && stat.IsDir() {
			return NewDataset(currentPath, pathToTest)
		}

		if os.IsPermission(err) {
			return nil, err
		}

		// Navigate up
		old := currentPath
		currentPath = gopath.Dir(currentPath)
		if old == currentPath {
			return nil, fmt.Errorf("could not find dataset for path: %s", path)
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
	str := dataset.getPropertyString(libProp, goProp)
	if str == "" {
		return defaultValue
	}
	val, err := strconv.Atoi(str)
	if err != nil {
		return defaultValue
	}
	return val
}

func (dataset *Dataset) getPropertyUint64(libProp golibzfs.Prop, goProp string) uint64 {
	str := dataset.getPropertyString(libProp, goProp)
	if str == "" {
		return 0
	}
	val, err := strconv.ParseUint(str, 10, 64)
	if err != nil {
		return 0
	}
	return val
}

func (dataset *Dataset) GetType() string {
	return dataset.getPropertyString(golibzfs.DatasetPropType, "type")
}

func (dataset *Dataset) GetCreationString() time.Time {
	if dataset.rawGolibzfsData != nil {
		prop, err := dataset.rawGolibzfsData.GetProperty(golibzfs.DatasetPropCreation)
		if err == nil {
			i, err := strconv.ParseInt(prop.Value, 10, 64)
			if err == nil {
				return time.Unix(i, 0)
			}
		}
	}
	return time.Time{}
}

func (dataset *Dataset) GetMountPoint() string {
	return dataset.getPropertyString(golibzfs.DatasetPropMountpoint, "mountpoint")
}

func (dataset *Dataset) GetMounted() string {
	return dataset.getPropertyString(golibzfs.DatasetPropMounted, "mounted")
}

func (dataset *Dataset) GetReadonly() string {
	return dataset.getPropertyString(golibzfs.DatasetPropReadonly, "readonly")
}

func (dataset *Dataset) GetVolSize() uint64 {
	return dataset.getPropertyUint64(golibzfs.DatasetPropVolsize, "volsize")
}

func (dataset *Dataset) GetAvailable() uint64 {
	return dataset.getPropertyUint64(golibzfs.DatasetPropAvailable, "available")
}

func (dataset *Dataset) GetUsed() uint64 {
	return dataset.getPropertyUint64(golibzfs.DatasetPropUsed, "used")
}

func (dataset *Dataset) GetCompression() string {
	return dataset.getPropertyString(golibzfs.DatasetPropCompression, "compression")
}

func (dataset *Dataset) GetCompressRatio() string {
	return dataset.getPropertyString(golibzfs.DatasetPropCompressratio, "compressratio")
}

func (dataset *Dataset) GetSnapdir() string {
	return dataset.getPropertyString(golibzfs.DatasetPropSnapdir, "snapdir")
}

func (dataset *Dataset) GetCaseSensitivity() string {
	// golibzfs.DatasetPropCasesensitivity might not exist, use a string if needed
	return dataset.getPropertyString(golibzfs.Prop(115), "casesensitivity")
}

func (dataset *Dataset) IsEncrypted() bool {
	encryption := dataset.getPropertyString(golibzfs.DatasetPropEncryption, "encryption")
	return encryption != "" && encryption != "off"
}

func (dataset *Dataset) GetEncryption() string {
	return dataset.getPropertyString(golibzfs.DatasetPropEncryption, "encryption")
}

func (dataset *Dataset) GetKeyStatus() string {
	return dataset.getPropertyString(golibzfs.DatasetPropKeyStatus, "keystatus")
}

func (dataset *Dataset) GetOrigin() string {
	return dataset.getPropertyString(golibzfs.DatasetPropOrigin, "origin")
}

func (dataset *Dataset) GetSnapshotLimit() int {
	return dataset.getPropertyInt(golibzfs.DatasetPropSnapshotLimit, "snapshot_limit", 0)
}

func (dataset *Dataset) GetSnapshotCount() int {
	return dataset.getPropertyInt(golibzfs.DatasetPropSnapshotCount, "snapshot_count", 0)
}

func (dataset *Dataset) CreateSnapshot(name string) error {
	if dataset.rawGozfsData != nil {
		_, err := dataset.rawGozfsData.Snapshot(name, false)
		return err
	}
	return errors.New("cannot create snapshot: no dataset metadata available")
}

func (dataset *Dataset) DestroySnapshot(name string, recursive bool, dependantClones bool) error {
	if dataset.rawGozfsData != nil {
		fullName := fmt.Sprintf("%s@%s", dataset.rawGozfsData.Name, name)
		snapshots, err := gozfs.Snapshots(fullName)
		if err != nil {
			return err
		}
		if len(snapshots) == 0 {
			return errors.New("snapshot not found")
		}
		flags := gozfs.DestroyDefault
		if recursive {
			flags = gozfs.DestroyRecursive
		}
		if dependantClones {
			flags = flags | gozfs.DestroyRecursiveClones
		}
		return snapshots[0].Destroy(flags)
	}
	return errors.New("cannot destroy snapshot: no dataset metadata available")
}

func (dataset *Dataset) GetSnapshotsDir() string {
	return gopath.Join(dataset.HiddenZfsPath, "snapshot")
}

// GetSnapshots returns all snapshots for this dataset
// Note: depending on the amount of snapshots, this can be a slow operation.
func (dataset *Dataset) GetSnapshots() ([]*Snapshot, error) {
	var result []*Snapshot

	snapshotDirs, err := util.ListFilesIn(dataset.GetSnapshotsDir())
	if err != nil {
		return []*Snapshot{}, err
	}

	var rawSnapshots []golibzfs.Dataset
	if dataset.rawGolibzfsData != nil {
		rawSnapshots, _ = dataset.rawGolibzfsData.Snapshots()
	}

	for _, file := range snapshotDirs {
		_, name := gopath.Split(file)

		var s *golibzfs.Dataset
		if len(rawSnapshots) > 0 {
			s = findSnapshot(rawSnapshots, name)
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
		if err == nil {
			return nameProperty.Value
		}
	}
	return ""
}
