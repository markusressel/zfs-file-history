package zfs

import (
	"strconv"
	"strings"
	"sync"
	"time"
	"zfs-file-history/internal/logging"

	golibzfs "github.com/kraudcloud/go-libzfs"
)

// The libzfs handles of System, opened once and shared until Reload.
var (
	// datasetHandles are opened with their children, which includes the snapshots, see SnapshotProperties.
	datasetHandles = make(map[string]*golibzfs.Dataset)
	// singleDatasetHandles are opened without their children, which is much faster for datasets with many
	// descendants, see DatasetProperties.
	singleDatasetHandles = make(map[string]*golibzfs.Dataset)
	handlesMtx           sync.RWMutex
)

// datasetPropertyIds are the libzfs ids of datasetProperties.
var datasetPropertyIds = map[string]golibzfs.Prop{
	propType:            golibzfs.DatasetPropType,
	propCreation:        golibzfs.DatasetPropCreation,
	propMountpoint:      golibzfs.DatasetPropMountpoint,
	propMounted:         golibzfs.DatasetPropMounted,
	propCanmount:        golibzfs.DatasetPropCanmount,
	propReadonly:        golibzfs.DatasetPropReadonly,
	propVolsize:         golibzfs.DatasetPropVolsize,
	propAvailable:       golibzfs.DatasetPropAvailable,
	propUsed:            golibzfs.DatasetPropUsed,
	propCompression:     golibzfs.DatasetPropCompression,
	propCompressratio:   golibzfs.DatasetPropCompressratio,
	propSnapdir:         golibzfs.DatasetPropSnapdir,
	propCasesensitivity: golibzfs.DatasetPropCase,
	propEncryption:      golibzfs.DatasetPropEncryption,
	propKeystatus:       golibzfs.DatasetPropKeyStatus,
	propOrigin:          golibzfs.DatasetPropOrigin,
	propSnapshotLimit:   golibzfs.DatasetPropSnapshotLimit,
	propSnapshotCount:   golibzfs.DatasetPropSnapshotCount,
}

// Reload closes nothing (the handles may still be in use), but drops them, so they are opened again with the
// current state.
func (System) Reload() {
	handlesMtx.Lock()
	datasetHandles = make(map[string]*golibzfs.Dataset)
	singleDatasetHandles = make(map[string]*golibzfs.Dataset)
	handlesMtx.Unlock()
}

// datasetHandle returns the libzfs handle of the dataset, opened (and cached) with its children if asked to. Without
// children, a handle opened with them is used if there is one.
func datasetHandle(name string, withChildren bool) (*golibzfs.Dataset, error) {
	handlesMtx.RLock()
	handle, cached := datasetHandles[name]
	if !cached && !withChildren {
		handle, cached = singleDatasetHandles[name]
	}
	handlesMtx.RUnlock()
	if cached {
		return handle, nil
	}

	var opened golibzfs.Dataset
	var err error
	if withChildren {
		opened, err = golibzfs.DatasetOpen(name)
	} else {
		// only opens this dataset, unlike golibzfs.DatasetOpen, which recursively opens all children
		opened, err = golibzfs.DatasetOpenSingle(name)
	}
	if err != nil {
		return nil, err
	}

	handles := singleDatasetHandles
	if withChildren {
		handles = datasetHandles
	}
	handlesMtx.Lock()
	defer handlesMtx.Unlock()
	if existing, exists := handles[name]; exists {
		// opened concurrently by someone else, our handle was never shared
		opened.Close()
		return existing, nil
	}
	handles[name] = &opened
	return &opened, nil
}

// DatasetProperties reads the properties from libzfs, or with "zfs get" if the dataset cannot be opened.
func (System) DatasetProperties(name string) (map[string]string, error) {
	handle, err := datasetHandle(name, false)
	if err != nil {
		return datasetPropertiesOfZfsGet(name)
	}
	result := make(map[string]string, len(datasetPropertyIds))
	for property, id := range datasetPropertyIds {
		if value, err := handle.GetProperty(id); err == nil {
			result[property] = value.Value
		}
	}
	return result, nil
}

// datasetPropertiesOfZfsGet reads the datasetProperties of the dataset with "zfs get", with exact values like
// libzfs returns them.
func datasetPropertiesOfZfsGet(name string) (map[string]string, error) {
	output, err := runZfs("get", "-H", "-p", "-o", "property,value", strings.Join(datasetProperties, ","), name)
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		if property, value, ok := strings.Cut(line, "\t"); ok && value != "-" {
			result[property] = value
		}
	}
	return result, nil
}

// SnapshotProperties reads the properties from the libzfs handles of the snapshots, loaded with the dataset. The
// ones without a handle (e.g. created since) are read with "zfs get".
func (System) SnapshotProperties(dataset string, names []string) map[string]SnapshotProperties {
	result := make(map[string]SnapshotProperties, len(names))
	var handles map[string]*golibzfs.Dataset
	if handle, err := datasetHandle(dataset, true); err == nil {
		handles = findSnapshotHandles(handle, names)
	}
	var missing []string
	for _, name := range names {
		// GetProperty locks golibzfs.Global.Mtx itself, so this must happen outside of findSnapshotHandles
		if handle, found := handles[name]; found {
			result[name] = snapshotPropertiesOfHandle(dataset+"@"+name, handle)
		} else {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		for name, properties := range snapshotPropertiesOfZfsGet(dataset, missing) {
			result[name] = properties
		}
	}
	return result
}

func snapshotPropertiesOfHandle(fullName string, handle *golibzfs.Dataset) SnapshotProperties {
	value := func(id golibzfs.Prop, name string) string {
		property, err := handle.GetProperty(id)
		if err != nil {
			logging.Error("Could not get %s property for %s: %s", name, fullName, err.Error())
			return ""
		}
		return property.Value
	}
	return SnapshotProperties{
		CreationDate:     parseUnixTime(value(golibzfs.DatasetPropCreation, "creation")),
		Used:             parseUint(value(golibzfs.DatasetPropUsed, "used")),
		Referenced:       parseUint(value(golibzfs.DatasetPropReferenced, "referenced")),
		Written:          parseUint(value(golibzfs.DatasetPropWritten, "written")),
		CompressionRatio: parseRatio(value(golibzfs.DatasetPropRefratio, "refratio")),
		Clones:           parseUint(value(golibzfs.DatasetPropNumclones, "clones")),
		Holds:            parseUint(value(golibzfs.DatasetPropUserrefs, "userrefs")),
	}
}

// snapshotPropertiesOfZfsGet reads the properties of the snapshots (names behind the "@") of the dataset with one
// "zfs get". Snapshots it cannot read are not in the result.
func snapshotPropertiesOfZfsGet(dataset string, names []string) map[string]SnapshotProperties {
	args := []string{"get", "-H", "-p", "-o", "name,property,value", "creation,used,referenced,written,refcompressratio,clones,userrefs"}
	for _, name := range names {
		args = append(args, dataset+"@"+name)
	}
	output, err := runZfs(args...)
	if err != nil {
		logging.Error("Could not get the properties of the snapshots of %s: %s", dataset, err.Error())
		return nil
	}
	return parseSnapshotProperties(output)
}

// parseSnapshotProperties parses the output of "zfs get -H -p -o name,property,value" of snapshots, by the names of
// the snapshots behind the "@", e.g.:
//
//	pool/data@daily	used	46747648
func parseSnapshotProperties(output string) map[string]SnapshotProperties {
	result := map[string]SnapshotProperties{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			continue
		}
		_, name, _ := strings.Cut(fields[0], "@")
		properties := result[name]
		value := fields[2]
		switch fields[1] {
		case "creation":
			properties.CreationDate = parseUnixTime(value)
		case "used":
			properties.Used = parseUint(value)
		case "referenced":
			properties.Referenced = parseUint(value)
		case "written":
			properties.Written = parseUint(value)
		case "refcompressratio":
			properties.CompressionRatio = parseRatio(value)
		case "clones":
			if value != "" && value != "-" {
				properties.Clones = uint64(strings.Count(value, ",") + 1)
			}
		case "userrefs":
			properties.Holds = parseUint(value)
		}
		result[name] = properties
	}
	return result
}

func parseUnixTime(value string) time.Time {
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(seconds, 0)
}

func parseUint(value string) uint64 {
	result, _ := strconv.ParseUint(value, 10, 64)
	return result
}

// parseRatio parses a compression ratio, e.g. "1.89" or "1.89x".
func parseRatio(value string) float64 {
	result, _ := strconv.ParseFloat(strings.TrimSuffix(value, "x"), 64)
	return result
}

// findSnapshotHandles returns the libzfs handles of the snapshots with the given names (if found),
// from the snapshots loaded with the given dataset handle.
//
// libzfs handles are cached and shared between goroutines. golibzfs.Dataset.GetProperty writes the
// Properties map of a handle while holding golibzfs.Global.Mtx, but Snapshots() and findSnapshot read
// these maps without locking. Concurrent map reads and writes crash the Go runtime, so these reads
// must hold golibzfs.Global.Mtx as well. Nothing in here may call a golibzfs function that locks
// golibzfs.Global.Mtx itself (like GetProperty), as that would deadlock.
func findSnapshotHandles(dataset *golibzfs.Dataset, names []string) map[string]*golibzfs.Dataset {
	golibzfs.Global.Mtx.Lock()
	defer golibzfs.Global.Mtx.Unlock()

	result := make(map[string]*golibzfs.Dataset, len(names))
	snapshots, _ := dataset.Snapshots()
	if len(snapshots) == 0 {
		return result
	}
	for _, name := range names {
		if snapshot := findSnapshot(snapshots, name); snapshot != nil {
			result[name] = snapshot
		}
	}
	return result
}

// findSnapshot returns the snapshot with the given name.
// The caller must hold golibzfs.Global.Mtx, see findSnapshotHandles.
func findSnapshot(snapshots []golibzfs.Dataset, name string) *golibzfs.Dataset {
	for i := range snapshots {
		nameProperty := snapshots[i].Properties[golibzfs.DatasetPropName]
		parts := strings.Split(nameProperty.Value, "@")
		if len(parts) < 2 {
			continue
		}
		currentName := parts[1]
		if currentName == name {
			return &snapshots[i]
		}
	}
	return nil
}

func (System) CreateSnapshot(dataset string, name string) error {
	_, err := runZfs("snapshot", dataset+"@"+name)
	return err
}

func (System) DestroySnapshot(dataset string, name string, recursive bool, dependantClones bool) error {
	args := []string{"destroy"}
	if recursive {
		args = append(args, "-r")
	}
	if dependantClones {
		args = append(args, "-R")
	}
	_, err := runZfs(append(args, dataset+"@"+name)...)
	return err
}

func (System) CloneSnapshot(fullName string, target string) error {
	_, err := runZfs("clone", "-p", fullName, target)
	return err
}
