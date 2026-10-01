package zfs

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	gopath "path"
	"strconv"
	"strings"
	"time"
	"zfs-file-history/internal/util"

	golibzfs "github.com/kraudcloud/go-libzfs"
	gozfs "github.com/mistifyio/go-zfs/v4"
)

const (
	propType            = "type"
	propMountpoint      = "mountpoint"
	propMounted         = "mounted"
	propCanmount        = "canmount"
	propReadonly        = "readonly"
	propVolsize         = "volsize"
	propAvailable       = "available"
	propUsed            = "used"
	propCompression     = "compression"
	propCompressratio   = "compressratio"
	propSnapdir         = "snapdir"
	propCasesensitivity = "casesensitivity"
	propEncryption      = "encryption"
	propKeystatus       = "keystatus"
	propOrigin          = "origin"
	propSnapshotLimit   = "snapshot_limit"
	propSnapshotCount   = "snapshot_count"
)

type Dataset struct {
	Path          string
	HiddenZfsPath string

	rawGozfsData    *gozfs.Dataset
	rawGolibzfsData *golibzfs.Dataset
}

func findDatasetNameByMountpoint(mountpoint string) (string, error) {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[2] == "zfs" {
			if gopath.Clean(fields[1]) == gopath.Clean(mountpoint) {
				return fields[0], nil
			}
		}
	}
	return "", fmt.Errorf("no zfs mount found for mountpoint: %s", mountpoint)
}

func NewDataset(path string, hiddenZfsPath string) (*Dataset, error) {
	path = gopath.Clean(path)
	dataset := &Dataset{
		Path:          path,
		HiddenZfsPath: hiddenZfsPath,
	}

	// Try to find the dataset metadata in the local cache
	cacheMtx.RLock()
	ds, cached := datasetCache[path]
	cacheMtx.RUnlock()

	if cached {
		dataset.rawGolibzfsData = ds
	} else {
		// If not cached, resolve it directly by looking up its mountpoint
		datasetName, err := findDatasetNameByMountpoint(path)
		if err == nil {
			libds, err := golibzfs.DatasetOpen(datasetName)
			if err == nil {
				cacheMtx.Lock()
				datasetCache[path] = &libds
				cacheMtx.Unlock()
				dataset.rawGolibzfsData = &libds
			}
		}
	}

	return dataset, nil
}

func (dataset *Dataset) lazyLoadGozfsData() error {
	if dataset.rawGozfsData != nil {
		return nil
	}

	if dataset.rawGolibzfsData != nil {
		nameProp, err := dataset.rawGolibzfsData.GetProperty(golibzfs.DatasetPropName)
		if err == nil {
			gozfsDs, err := gozfs.GetDataset(nameProp.Value)
			if err == nil {
				dataset.rawGozfsData = gozfsDs
				return nil
			}
		}
	}

	// Fallback: if we haven't found metadata yet, try mistify/go-zfs directly.
	gozfsList, err := gozfs.Filesystems("")
	if err == nil {
		for _, fs := range gozfsList {
			if gopath.Clean(fs.Mountpoint) == dataset.Path {
				dataset.rawGozfsData = fs
				return nil
			}
		}
	}

	return errors.New("could not load gozfs metadata")
}

// OpenDatasetByName opens the dataset with the given name, which does not need to be mounted.
// mountPath is the path the dataset is currently mounted at, or "" if it is not mounted.
// This calls into libzfs, so it must not be called on the UI thread.
func OpenDatasetByName(name string, mountPath string) (*Dataset, error) {
	if name == "" {
		return nil, errors.New("cannot open dataset with empty name")
	}

	cacheMtx.RLock()
	handle, cached := datasetByNameCache[name]
	cacheMtx.RUnlock()

	if !cached {
		// only opens this dataset, unlike golibzfs.DatasetOpen, which recursively opens all children
		opened, err := golibzfs.DatasetOpenSingle(name)
		if err != nil {
			return nil, err
		}

		cacheMtx.Lock()
		if existing, exists := datasetByNameCache[name]; exists {
			// opened concurrently by someone else, our handle was never shared
			opened.Close()
			handle = existing
		} else {
			datasetByNameCache[name] = &opened
			handle = &opened
		}
		cacheMtx.Unlock()
	}

	dataset := &Dataset{
		Path:            mountPath,
		rawGolibzfsData: handle,
	}
	if mountPath != "" {
		dataset.Path = gopath.Clean(mountPath)
		dataset.HiddenZfsPath = gopath.Join(dataset.Path, ".zfs")
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
	_ = dataset.lazyLoadGozfsData()
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
	return dataset.getPropertyString(golibzfs.DatasetPropType, propType)
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
	return dataset.getPropertyString(golibzfs.DatasetPropMountpoint, propMountpoint)
}

func (dataset *Dataset) GetMounted() string {
	return dataset.getPropertyString(golibzfs.DatasetPropMounted, propMounted)
}

func (dataset *Dataset) GetCanMount() string {
	return dataset.getPropertyString(golibzfs.DatasetPropCanmount, propCanmount)
}

func (dataset *Dataset) GetReadonly() string {
	return dataset.getPropertyString(golibzfs.DatasetPropReadonly, propReadonly)
}

func (dataset *Dataset) GetVolSize() uint64 {
	return dataset.getPropertyUint64(golibzfs.DatasetPropVolsize, propVolsize)
}

func (dataset *Dataset) GetAvailable() uint64 {
	return dataset.getPropertyUint64(golibzfs.DatasetPropAvailable, propAvailable)
}

func (dataset *Dataset) GetUsed() uint64 {
	return dataset.getPropertyUint64(golibzfs.DatasetPropUsed, propUsed)
}

func (dataset *Dataset) GetCompression() string {
	return dataset.getPropertyString(golibzfs.DatasetPropCompression, propCompression)
}

func (dataset *Dataset) GetCompressRatio() string {
	return dataset.getPropertyString(golibzfs.DatasetPropCompressratio, propCompressratio)
}

func (dataset *Dataset) GetSnapdir() string {
	return dataset.getPropertyString(golibzfs.DatasetPropSnapdir, propSnapdir)
}

func (dataset *Dataset) GetCaseSensitivity() string {
	// golibzfs.DatasetPropCasesensitivity might not exist, use a string if needed
	return dataset.getPropertyString(golibzfs.Prop(115), propCasesensitivity)
}

func (dataset *Dataset) IsEncrypted() bool {
	encryption := dataset.getPropertyString(golibzfs.DatasetPropEncryption, propEncryption)
	return encryption != "" && encryption != "off"
}

func (dataset *Dataset) GetEncryption() string {
	return dataset.getPropertyString(golibzfs.DatasetPropEncryption, propEncryption)
}

func (dataset *Dataset) GetKeyStatus() string {
	return dataset.getPropertyString(golibzfs.DatasetPropKeyStatus, propKeystatus)
}

func (dataset *Dataset) GetOrigin() string {
	return dataset.getPropertyString(golibzfs.DatasetPropOrigin, propOrigin)
}

func (dataset *Dataset) GetSnapshotLimit() int {
	return dataset.getPropertyInt(golibzfs.DatasetPropSnapshotLimit, propSnapshotLimit, 0)
}

func (dataset *Dataset) GetSnapshotCount() int {
	return dataset.getPropertyInt(golibzfs.DatasetPropSnapshotCount, propSnapshotCount, 0)
}

func (dataset *Dataset) CreateSnapshot(name string) error {
	_ = dataset.lazyLoadGozfsData()
	if dataset.rawGozfsData != nil {
		_, err := dataset.rawGozfsData.Snapshot(name, false)
		return err
	}
	return errors.New("cannot create snapshot: no dataset metadata available")
}

func (dataset *Dataset) DestroySnapshot(name string, recursive bool, dependantClones bool) error {
	_ = dataset.lazyLoadGozfsData()
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

	names := make([]string, 0, len(snapshotDirs))
	for _, file := range snapshotDirs {
		_, name := gopath.Split(file)
		names = append(names, name)
	}

	var snapshotHandles map[string]*golibzfs.Dataset
	if dataset.rawGolibzfsData != nil {
		snapshotHandles = findSnapshotHandles(dataset.rawGolibzfsData, names)
	}

	for i, file := range snapshotDirs {
		name := names[i]
		// NewSnapshot reads properties via GetProperty, which locks golibzfs.Global.Mtx itself,
		// so this must happen outside of findSnapshotHandles
		result = append(result, NewSnapshot(name, file, dataset, snapshotHandles[name]))
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

// DatasetListEntry is a snapshot of the listing properties of a filesystem dataset.
// All values are fetched up front, so reading them never spawns a zfs process.
type DatasetListEntry struct {
	Name      string
	Used      uint64
	Available uint64
	// Mountpoint is the value of the "mountpoint" property (e.g. "/data", "legacy" or "none").
	Mountpoint string
	// MountPath is the path the dataset is currently mounted at, or "" if it is not mounted.
	MountPath string
}

func (entry DatasetListEntry) TableRowId() string {
	return entry.Name
}

// ListAllDatasets returns the listing properties of all filesystem datasets on the system.
// This executes "zfs list" and reads /proc/mounts, so it must not be called on the UI thread.
func ListAllDatasets() ([]*DatasetListEntry, error) {
	fsList, err := gozfs.Filesystems("")
	if err != nil {
		return nil, err
	}

	mountPaths := map[string]string{}
	mounts, err := os.Open("/proc/mounts")
	if err == nil {
		mountPaths = parseZfsMountPaths(mounts)
		_ = mounts.Close()
	}

	entries := make([]*DatasetListEntry, 0, len(fsList))
	for _, fs := range fsList {
		entries = append(entries, &DatasetListEntry{
			Name:       fs.Name,
			Used:       fs.Used,
			Available:  fs.Avail,
			Mountpoint: fs.Mountpoint,
			MountPath:  mountPaths[fs.Name],
		})
	}

	return entries, nil
}

// parseZfsMountPaths maps the names of mounted zfs datasets to their mount path,
// given content in the format of /proc/mounts. If a dataset is mounted multiple times,
// the first mount wins.
func parseZfsMountPaths(reader io.Reader) map[string]string {
	result := map[string]string{}
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 || fields[2] != "zfs" {
			continue
		}
		name := unescapeMountField(fields[0])
		if _, exists := result[name]; exists {
			continue
		}
		result[name] = gopath.Clean(unescapeMountField(fields[1]))
	}
	return result
}

// unescapeMountField decodes the octal escapes (e.g. "\040" for a space) used in /proc/mounts.
func unescapeMountField(field string) string {
	if !strings.Contains(field, `\`) {
		return field
	}
	var builder strings.Builder
	for i := 0; i < len(field); i++ {
		if field[i] == '\\' && i+4 <= len(field) {
			if value, err := strconv.ParseUint(field[i+1:i+4], 8, 8); err == nil {
				builder.WriteByte(byte(value))
				i += 3
				continue
			}
		}
		builder.WriteByte(field[i])
	}
	return builder.String()
}
