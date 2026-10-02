package zfs

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	gopath "path"
	"strconv"
	"strings"
	"sync"
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

	// rawGozfsData is loaded lazily and may be accessed from different goroutines (e.g. a snapshot action in the
	// background while the UI uses the same dataset), so it must only be accessed with gozfsMutex held,
	// see gozfsData and loadedGozfsData.
	rawGozfsData    *gozfs.Dataset
	gozfsMutex      sync.Mutex
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

// gozfsData returns the go-zfs data of this dataset, loading it first if necessary (which may spawn zfs processes).
// It returns nil if it cannot be loaded. Safe for concurrent use.
func (dataset *Dataset) gozfsData() *gozfs.Dataset {
	dataset.gozfsMutex.Lock()
	defer dataset.gozfsMutex.Unlock()
	_ = dataset.lazyLoadGozfsData()
	return dataset.rawGozfsData
}

// loadedGozfsData returns the go-zfs data of this dataset if it was loaded already, without loading it.
// Safe for concurrent use.
func (dataset *Dataset) loadedGozfsData() *gozfs.Dataset {
	dataset.gozfsMutex.Lock()
	defer dataset.gozfsMutex.Unlock()
	return dataset.rawGozfsData
}

// lazyLoadGozfsData loads the go-zfs data, if not loaded already. The caller must hold gozfsMutex.
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
	if gozfsDataset := dataset.gozfsData(); gozfsDataset != nil {
		val, err := gozfsDataset.GetProperty(goProp)
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
	if gozfsDataset := dataset.gozfsData(); gozfsDataset != nil {
		_, err := gozfsDataset.Snapshot(name, false)
		return err
	}
	return errors.New("cannot create snapshot: no dataset metadata available")
}

func (dataset *Dataset) DestroySnapshot(name string, recursive bool, dependantClones bool) error {
	if gozfsDataset := dataset.gozfsData(); gozfsDataset != nil {
		fullName := fmt.Sprintf("%s@%s", gozfsDataset.Name, name)
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
	if gozfsDataset := dataset.loadedGozfsData(); gozfsDataset != nil {
		return gozfsDataset.Name
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
	// UsedBySnapshots is the space that would be freed by destroying all snapshots of the dataset ("usedbysnapshots").
	UsedBySnapshots uint64
	// UsedByDataset is the space used by the dataset itself ("usedbydataset").
	UsedByDataset uint64
	// UsedByChildren is the space used by the descendants of the dataset ("usedbychildren").
	UsedByChildren uint64
	// UsedByRefreservation is the space reserved for the dataset by its refreservation, beyond what it actually
	// uses ("usedbyrefreservation"). Together with the other UsedBy* values, it adds up to Used.
	UsedByRefreservation uint64
	// Mountpoint is the value of the "mountpoint" property (e.g. "/data", "legacy" or "none").
	Mountpoint string
	// MountPath is the path the dataset is currently mounted at, or "" if it is not mounted.
	MountPath string
}

func (entry DatasetListEntry) TableRowId() string {
	return entry.Name
}

// datasetListProperties are the properties requested by ListAllDatasets, in the order parseDatasetList expects them.
// The mountpoint is last, so it may contain any character but a newline.
var datasetListProperties = []string{"name", "used", "available", "usedbysnapshots", "usedbydataset", "usedbychildren", "usedbyrefreservation", "mountpoint"}

// ListAllDatasets returns the listing properties of all filesystem datasets on the system.
// This executes "zfs list" and reads /proc/mounts, so it must not be called on the UI thread.
func ListAllDatasets() ([]*DatasetListEntry, error) {
	output, err := exec.Command("zfs", "list", "-H", "-p", "-t", "filesystem", "-o", strings.Join(datasetListProperties, ",")).Output()
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) && len(strings.TrimSpace(string(exitError.Stderr))) > 0 {
			return nil, errors.New(strings.TrimSpace(string(exitError.Stderr)))
		}
		return nil, err
	}

	mountPaths := map[string]string{}
	mounts, err := os.Open("/proc/mounts")
	if err == nil {
		mountPaths = parseZfsMountPaths(mounts)
		_ = mounts.Close()
	}

	return parseDatasetList(string(output), mountPaths)
}

// parseDatasetList parses the output of "zfs list -Hp -o <datasetListProperties>". mountPaths maps the names of
// mounted datasets to their mount path, see parseZfsMountPaths.
func parseDatasetList(output string, mountPaths map[string]string) ([]*DatasetListEntry, error) {
	var entries []*DatasetListEntry
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", len(datasetListProperties))
		if len(fields) != len(datasetListProperties) {
			return nil, fmt.Errorf("unexpected output of zfs list: %q", line)
		}

		entry := &DatasetListEntry{
			Name:       fields[0],
			Mountpoint: fields[7],
			MountPath:  mountPaths[fields[0]],
		}
		sizes := []*uint64{&entry.Used, &entry.Available, &entry.UsedBySnapshots, &entry.UsedByDataset, &entry.UsedByChildren, &entry.UsedByRefreservation}
		for i, size := range sizes {
			value, err := parseSizeProperty(fields[i+1])
			if err != nil {
				return nil, fmt.Errorf("cannot parse %s of %s: %w", datasetListProperties[i+1], entry.Name, err)
			}
			*size = value
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// parseSizeProperty parses a size in bytes, as printed by "zfs list -p". "-" (not applicable) is 0.
func parseSizeProperty(value string) (uint64, error) {
	if value == "-" {
		return 0, nil
	}
	return strconv.ParseUint(value, 10, 64)
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
