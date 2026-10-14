package zfs

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	gopath "path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/util"
)

const (
	propType            = "type"
	propCreation        = "creation"
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
	propUsedByDataset   = "usedbydataset"
	propUsedBySnapshots = "usedbysnapshots"
	propUsedByChildren  = "usedbychildren"
	propLogicalUsed     = "logicalused"
)

// datasetProperties are the properties read by the getters of Dataset, see Backend.DatasetProperties.
var datasetProperties = []string{propType, propCreation, propMountpoint, propMounted, propCanmount, propReadonly,
	propVolsize, propAvailable, propUsed, propCompression, propCompressratio, propSnapdir, propCasesensitivity,
	propEncryption, propKeystatus, propOrigin, propSnapshotLimit, propSnapshotCount, propUsedByDataset,
	propUsedBySnapshots, propUsedByChildren, propLogicalUsed}

type Dataset struct {
	Path          string
	HiddenZfsPath string

	// mutex guards the fields below, which are loaded lazily and may be accessed from different goroutines (e.g. a
	// snapshot action in the background while the UI uses the same dataset)
	mutex sync.Mutex
	// name is the name of the dataset, once known, see GetName
	name         string
	nameResolved bool
	// properties are the values of datasetProperties, once loaded, see property
	properties map[string]string
}

func findDatasetNameByMountpoint(mountpoint string) (string, error) {
	mounts, err := backend.Mounts()
	if err != nil {
		return "", err
	}
	for _, mount := range mounts {
		if gopath.Clean(mount.Mountpoint) == gopath.Clean(mountpoint) {
			return mount.Name, nil
		}
	}
	return "", fmt.Errorf("no zfs mount found for mountpoint: %s", mountpoint)
}

// NewDataset returns the dataset mounted at path. Its name and properties are loaded once needed.
func NewDataset(path string, hiddenZfsPath string) (*Dataset, error) {
	return &Dataset{
		Path:          gopath.Clean(path),
		HiddenZfsPath: hiddenZfsPath,
	}, nil
}

// OpenDatasetByName opens the dataset with the given name, which does not need to be mounted.
// mountPath is the path the dataset is currently mounted at, or "" if it is not mounted.
// This reads the properties of the dataset, so it must not be called on the UI thread.
func OpenDatasetByName(name string, mountPath string) (*Dataset, error) {
	if name == "" {
		return nil, errors.New("cannot open dataset with empty name")
	}
	properties, err := backend.DatasetProperties(name)
	if err != nil {
		return nil, err
	}
	dataset := &Dataset{
		Path:         mountPath,
		name:         name,
		nameResolved: true,
		properties:   properties,
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

// GetName returns the name of the dataset, "" if it is not known (e.g. not mounted where it was found). It may read
// the mounts, so it must not be called on the UI thread.
func (dataset *Dataset) GetName() string {
	dataset.mutex.Lock()
	defer dataset.mutex.Unlock()
	if !dataset.nameResolved {
		dataset.nameResolved = true
		name, err := findDatasetNameByMountpoint(dataset.Path)
		if err != nil {
			logging.Debug("Cannot find the name of the dataset at %s: %v", dataset.Path, err)
		}
		dataset.name = name
	}
	return dataset.name
}

// property returns the value of one of datasetProperties, "" if it is not known. All of them are loaded with the
// first one, so this must not be called on the UI thread.
func (dataset *Dataset) property(name string) string {
	datasetName := dataset.GetName()
	dataset.mutex.Lock()
	defer dataset.mutex.Unlock()
	if dataset.properties == nil {
		dataset.properties = map[string]string{}
		if datasetName != "" {
			properties, err := backend.DatasetProperties(datasetName)
			if err != nil {
				logging.Error("Could not get the properties of %s: %s", datasetName, err.Error())
			} else {
				dataset.properties = properties
			}
		}
	}
	return dataset.properties[name]
}

func (dataset *Dataset) getPropertyInt(name string, defaultValue int) int {
	val, err := strconv.Atoi(dataset.property(name))
	if err != nil {
		return defaultValue
	}
	return val
}

func (dataset *Dataset) getPropertyUint64(name string) uint64 {
	val, err := strconv.ParseUint(dataset.property(name), 10, 64)
	if err != nil {
		return 0
	}
	return val
}

func (dataset *Dataset) GetType() string {
	return dataset.property(propType)
}

func (dataset *Dataset) GetCreationString() time.Time {
	return parseUnixTime(dataset.property(propCreation))
}

func (dataset *Dataset) GetMountPoint() string {
	return dataset.property(propMountpoint)
}

func (dataset *Dataset) GetMounted() string {
	return dataset.property(propMounted)
}

func (dataset *Dataset) GetCanMount() string {
	return dataset.property(propCanmount)
}

func (dataset *Dataset) GetReadonly() string {
	return dataset.property(propReadonly)
}

func (dataset *Dataset) GetVolSize() uint64 {
	return dataset.getPropertyUint64(propVolsize)
}

func (dataset *Dataset) GetAvailable() uint64 {
	return dataset.getPropertyUint64(propAvailable)
}

func (dataset *Dataset) GetUsed() uint64 {
	return dataset.getPropertyUint64(propUsed)
}

// GetUsedByDataset, GetUsedBySnapshots and GetUsedByChildren return the parts of the space used by the dataset
// (GetUsed): by its own data, by its snapshots (what destroying all of them frees) and by its child datasets.
func (dataset *Dataset) GetUsedByDataset() uint64 {
	return dataset.getPropertyUint64(propUsedByDataset)
}

func (dataset *Dataset) GetUsedBySnapshots() uint64 {
	return dataset.getPropertyUint64(propUsedBySnapshots)
}

func (dataset *Dataset) GetUsedByChildren() uint64 {
	return dataset.getPropertyUint64(propUsedByChildren)
}

// GetLogicalUsed returns the space the dataset would use without compression, see GetUsed.
func (dataset *Dataset) GetLogicalUsed() uint64 {
	return dataset.getPropertyUint64(propLogicalUsed)
}

func (dataset *Dataset) GetCompression() string {
	return dataset.property(propCompression)
}

func (dataset *Dataset) GetCompressRatio() string {
	return dataset.property(propCompressratio)
}

func (dataset *Dataset) GetSnapdir() string {
	return dataset.property(propSnapdir)
}

func (dataset *Dataset) GetCaseSensitivity() string {
	return dataset.property(propCasesensitivity)
}

func (dataset *Dataset) IsEncrypted() bool {
	encryption := dataset.property(propEncryption)
	return encryption != "" && encryption != "off"
}

func (dataset *Dataset) GetEncryption() string {
	return dataset.property(propEncryption)
}

func (dataset *Dataset) GetKeyStatus() string {
	return dataset.property(propKeystatus)
}

func (dataset *Dataset) GetOrigin() string {
	return dataset.property(propOrigin)
}

func (dataset *Dataset) GetSnapshotLimit() int {
	return dataset.getPropertyInt(propSnapshotLimit, 0)
}

func (dataset *Dataset) GetSnapshotCount() int {
	return dataset.getPropertyInt(propSnapshotCount, 0)
}

// CreateSnapshot creates a snapshot of the dataset with the given name (behind the "@").
// This spawns a zfs process, so it must not be called on the UI thread.
func (dataset *Dataset) CreateSnapshot(name string) error {
	datasetName := dataset.GetName()
	if datasetName == "" {
		return errors.New("cannot create snapshot: no dataset metadata available")
	}
	return CreateSnapshot(datasetName, name, false)
}

// DestroySnapshot destroys the snapshot of the dataset with the given name (behind the "@"), see
// Backend.DestroySnapshot. This spawns a zfs process, so it must not be called on the UI thread.
func (dataset *Dataset) DestroySnapshot(name string, recursive bool, dependantClones bool) error {
	datasetName := dataset.GetName()
	if datasetName == "" {
		return errors.New("cannot destroy snapshot: no dataset metadata available")
	}
	return explainPermissionError(backend.DestroySnapshot(datasetName, name, recursive, dependantClones),
		"destroy the snapshot", PermissionGap{Dataset: datasetName, Permissions: destroyPermissions})
}

func (dataset *Dataset) GetSnapshotsDir() string {
	return gopath.Join(dataset.HiddenZfsPath, "snapshot")
}

// GetSnapshots returns all snapshots for this dataset
// Note: depending on the amount of snapshots, this can be a slow operation.
func (dataset *Dataset) GetSnapshots() ([]*Snapshot, error) {
	snapshotDirs, err := util.ListFilesIn(dataset.GetSnapshotsDir())
	if err != nil {
		return []*Snapshot{}, err
	}

	names := make([]string, 0, len(snapshotDirs))
	for _, file := range snapshotDirs {
		_, name := gopath.Split(file)
		names = append(names, name)
	}

	var properties map[string]SnapshotProperties
	datasetName := dataset.GetName()
	if datasetName != "" {
		properties = backend.SnapshotProperties(datasetName, names)
	}

	result := make([]*Snapshot, 0, len(snapshotDirs))
	for i, file := range snapshotDirs {
		result = append(result, &Snapshot{
			Name:          names[i],
			FullName:      fmt.Sprintf("%s@%s", datasetName, names[i]),
			Path:          file,
			ParentDataset: dataset,
			Properties:    properties[names[i]],
		})
	}
	return result, nil
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
	// Origin is the full name of the snapshot the dataset was cloned from, "" if it is not a clone.
	Origin string
}

// IsClone returns whether the dataset is a clone of a snapshot.
func (entry DatasetListEntry) IsClone() bool {
	return IsClone(entry.Origin)
}

func (entry DatasetListEntry) TableRowId() string {
	return entry.Name
}

// ListAllDatasets returns the listing properties of all filesystem datasets on the system.
// This executes "zfs list" and reads /proc/mounts, so it must not be called on the UI thread.
func ListAllDatasets() ([]*DatasetListEntry, error) {
	return backend.ListDatasets()
}

// ListDatasetsAlong returns the parents of the dataset (up to the pool) and their direct children, each once: the
// dataset, its siblings and those of its parents. Only what is needed to place the dataset within its pool, which
// is much faster than listing all datasets on a pool with many of them.
// This executes "zfs list", so it must not be called on the UI thread.
func ListDatasetsAlong(name string) ([]*DatasetListEntry, error) {
	var parents []string
	for i, char := range name {
		if char == '/' {
			parents = append(parents, name[:i])
		}
	}
	if len(parents) == 0 {
		return nil, nil
	}
	entries, err := backend.ListChildDatasets(parents)
	if err != nil {
		return nil, err
	}
	// a parent is listed as itself and as a child of its parent
	seen := map[string]bool{}
	return slices.DeleteFunc(entries, func(entry *DatasetListEntry) bool {
		duplicate := seen[entry.Name]
		seen[entry.Name] = true
		return duplicate
	}), nil
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
			Mountpoint: fields[8],
			MountPath:  mountPaths[fields[0]],
		}
		if IsClone(fields[7]) {
			entry.Origin = fields[7]
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
