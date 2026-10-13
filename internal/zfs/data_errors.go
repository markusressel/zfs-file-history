package zfs

import (
	"errors"
	"os"
	"os/exec"
	"path"
	"slices"
	"strconv"
	"strings"
)

// dataErrorsArgs are the arguments of "zpool status" that list the damaged files of all pools. Exactly these are
// allowed by the sudoers rule of the setup command, see SudoersRuleDataErrors.
var dataErrorsArgs = []string{"status", "-p", "-v"}

// dataErrorsDenied is what "zpool status -v" prints instead of the damaged files of a pool if the list needs root.
// It goes to stderr, and the errors field of the pool is left out of the status.
const dataErrorsDenied = "List of errors unavailable"

// SetupDataErrorsHint tells how to allow listing the damaged files without starting the application as root.
const SetupDataErrorsHint = "To see which files are damaged, run: sudo zfs-file-history setup --data-errors"

// DataError is a damaged file or object listed by "zpool status -v", see ListDataErrors. ZFS lists an object once
// per dataset or snapshot whose read failed; with the head_errlog feature (OpenZFS 2.2+) also every snapshot and
// clone that shares the damaged block.
type DataError struct {
	Pool string
	// Entry is the line as printed by zpool status, e.g. "tank/data@daily:/docs/file"
	Entry string
	// Dataset is the name of the dataset of the object (also of a snapshot), "" if ZFS could not resolve it
	Dataset string
	// Snapshot is the name of the snapshot (behind the "@") the object is in, "" if it is not in a snapshot
	Snapshot string
	// Path is the path of the file within the dataset (starting with "/"), "" if ZFS could not resolve it (e.g. a
	// deleted file, or metadata)
	Path string
	// LivePath is the path the file has (or had, if it is in a snapshot) in the mounted dataset, "" if the dataset
	// is not mounted, or Path is not known
	LivePath string
	// Object is the object number (the inode number of a file) if ZFS does not know the path, e.g. 0x1c2a for
	// "tank/data:<0x1c2a>", 0 otherwise
	Object uint64
	// Locations are where the object was found by its inode number in snapshots (see FindObjects), if ZFS does not
	// know its path
	Locations []ObjectLocation
	// Searched is whether the object was looked for in the snapshots (see FindObjects). Searched without Locations:
	// the file is in no snapshot anymore, so its blocks are freed, and the error goes away with the next scrubs.
	Searched bool
}

// SnapshotFullName returns the full name of the snapshot the object is in (e.g. "tank/data@daily"), "" if it is
// not in a snapshot.
func (e DataError) SnapshotFullName() string {
	if e.Snapshot == "" {
		return ""
	}
	return e.Dataset + "@" + e.Snapshot
}

// IsResolved returns whether ZFS knows the path of the damaged object, i.e. it is a file or folder.
func (e DataError) IsResolved() bool {
	return e.Path != ""
}

// DataErrorIndex are the damaged files of all pools, indexed to look them up while browsing. A nil index has no
// damaged files. Not changed after it was created, so it can be shared between threads.
type DataErrorIndex struct {
	// Errors are all damaged objects, in the order of zpool status
	Errors []DataError
	// Unavailable are the pools with data errors whose list could not be read: it needs root (see
	// SetupDataErrorsHint)
	Unavailable []string
	// WithoutHeadErrlog are the pools with data errors whose feature head_errlog is disabled: an error is only
	// reported for the dataset or snapshot whose read failed, not for every snapshot that shares the damaged block
	WithoutHeadErrlog []string

	// byLivePath are the errors of each LivePath
	byLivePath map[string][]DataError
	// below is the number of damaged files (live or in a snapshot) below each folder, by live path
	below map[string]int
	// bySnapshot is the number of damaged objects in each snapshot, by its full name
	bySnapshot map[string]int
}

// NewDataErrorIndex returns the index of the errors. unavailable are the pools whose list could not be read.
func NewDataErrorIndex(dataErrors []DataError, unavailable []string) *DataErrorIndex {
	index := &DataErrorIndex{
		Errors:      dataErrors,
		Unavailable: unavailable,
		byLivePath:  map[string][]DataError{},
		below:       map[string]int{},
		bySnapshot:  map[string]int{},
	}
	for _, dataError := range dataErrors {
		if name := dataError.SnapshotFullName(); name != "" {
			index.bySnapshot[name]++
		}
		index.add(dataError)
		// the copies of an object found by its inode number count like the files listed by ZFS
		for _, location := range dataError.Locations {
			located := dataError
			located.Snapshot, located.Path, located.LivePath, located.Locations = location.Snapshot, location.Path, location.LivePath, nil
			if dataError.Snapshot == "" {
				index.bySnapshot[located.SnapshotFullName()]++
			}
			index.add(located)
		}
	}
	return index
}

// add adds the error to the lookups by live path. The folders above count each damaged file once, also if it is
// damaged in several snapshots.
func (index *DataErrorIndex) add(dataError DataError) {
	if dataError.LivePath == "" {
		return
	}
	if len(index.byLivePath[dataError.LivePath]) == 0 {
		for dir := path.Dir(dataError.LivePath); ; dir = path.Dir(dir) {
			index.below[dir]++
			if dir == "/" || dir == "." {
				break
			}
		}
	}
	index.byLivePath[dataError.LivePath] = append(index.byLivePath[dataError.LivePath], dataError)
}

// WithLocations returns the index with the objects whose path ZFS does not know found in snapshots by FindObjects
// (an empty list: looked for, but not found). Objects ZFS reported in a snapshot only keep the locations in it.
func (index *DataErrorIndex) WithLocations(locations map[ObjectKey][]ObjectLocation) *DataErrorIndex {
	if index == nil {
		return nil
	}
	dataErrors := slices.Clone(index.Errors)
	for i := range dataErrors {
		dataError := &dataErrors[i]
		found, ok := locations[ObjectKey{dataError.Dataset, dataError.Object}]
		if !ok || dataError.Path != "" || dataError.Object == 0 {
			continue
		}
		dataError.Searched = true
		dataError.Locations = nil
		for _, location := range found {
			if dataError.Snapshot == "" || location.Snapshot == dataError.Snapshot {
				dataError.Locations = append(dataError.Locations, location)
			}
		}
	}
	result := NewDataErrorIndex(dataErrors, index.Unavailable)
	result.WithoutHeadErrlog = index.WithoutHeadErrlog
	return result
}

// ExistingLocations returns the locations whose files still exist with the same inode, e.g. not after their
// snapshot was destroyed. This reads the file system, so it must not be called on the UI thread.
func ExistingLocations(locations map[ObjectKey][]ObjectLocation) map[ObjectKey][]ObjectLocation {
	result := make(map[ObjectKey][]ObjectLocation, len(locations))
	for key, found := range locations {
		var existing []ObjectLocation
		for _, location := range found {
			if inodeOf(location.File) == key.Object {
				existing = append(existing, location)
			}
		}
		result[key] = existing
	}
	return result
}

// Len returns the number of damaged objects.
func (index *DataErrorIndex) Len() int {
	if index == nil {
		return 0
	}
	return len(index.Errors)
}

// At returns the errors of the file at the given live path: in the file itself and in its versions in snapshots.
func (index *DataErrorIndex) At(livePath string) []DataError {
	if index == nil {
		return nil
	}
	return index.byLivePath[path.Clean(livePath)]
}

// IsDamaged returns whether the file at the given live path is damaged in the given snapshot (its full name, e.g.
// "tank/data@daily"), or in the file system with "".
func (index *DataErrorIndex) IsDamaged(livePath string, snapshotFullName string) bool {
	for _, dataError := range index.At(livePath) {
		if dataError.SnapshotFullName() == snapshotFullName {
			return true
		}
	}
	return false
}

// DamagedBelow returns the number of damaged files (in the file system or in snapshots) below the folder with the
// given live path.
func (index *DataErrorIndex) DamagedBelow(dir string) int {
	if index == nil {
		return 0
	}
	return index.below[path.Clean(dir)]
}

// DamagedInSnapshot returns the number of damaged objects in the snapshot with the given full name.
func (index *DataErrorIndex) DamagedInSnapshot(fullName string) int {
	if index == nil {
		return 0
	}
	return index.bySnapshot[fullName]
}

// zfsMount is a mounted ZFS dataset or snapshot, see readZfsMounts.
type zfsMount struct {
	// Name is the name of the dataset, or the full name of the snapshot (snapshots are mounted below .zfs/snapshot
	// when they are accessed)
	Name       string
	Mountpoint string
}

// readZfsMounts returns the mounted ZFS datasets and snapshots. Replaceable in tests.
var readZfsMounts = func() ([]zfsMount, error) {
	file, err := os.Open("/proc/mounts")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var mounts []zfsMount
	for name, mountpoint := range parseZfsMountPaths(file) {
		mounts = append(mounts, zfsMount{Name: name, Mountpoint: mountpoint})
	}
	return mounts, nil
}

// parseDataError parses a damaged object listed by "zpool status -v" of the pool (see PoolStatus.ErrorEntries),
// using the mounts to find its dataset and live path. The forms are:
//   - "/home/user/file": a file in a mounted dataset (or in a snapshot mounted below .zfs/snapshot)
//   - "tank/data@daily:/file": a file in a snapshot, "tank/data:/file" in a dataset that is not mounted
//   - "tank/data:<0x1b>": an object whose path is not known, e.g. a deleted file
//   - "<metadata>:<0x1b>", "<0x15>:<0x1b>": metadata, or an object in a dataset that is gone
func parseDataError(pool string, entry string, mounts []zfsMount) DataError {
	dataError := DataError{Pool: pool, Entry: entry}
	switch {
	case strings.HasPrefix(entry, "<"):
		return dataError
	case strings.HasPrefix(entry, "/"):
		mount, ok := mountOf(entry, mounts)
		if !ok {
			// not known how, but it is a path in the file system
			dataError.LivePath = path.Clean(entry)
			return dataError
		}
		dataError.Dataset, dataError.Snapshot, _ = strings.Cut(mount.Name, "@")
		dataError.Path = path.Join("/", strings.TrimPrefix(entry, mount.Mountpoint))
	default:
		end := indexOfAny(entry, ":/", ":<")
		if end < 0 {
			return dataError
		}
		dataError.Dataset, dataError.Snapshot, _ = strings.Cut(entry[:end], "@")
		rest := entry[end+1:]
		if strings.HasPrefix(rest, "/") {
			dataError.Path = path.Clean(rest)
		} else if object, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimPrefix(rest, "<0x"), ">"), 16, 64); err == nil {
			dataError.Object = object
		}
	}
	if dataError.Path != "" {
		for _, mount := range mounts {
			if mount.Name == dataError.Dataset {
				dataError.LivePath = path.Join(mount.Mountpoint, dataError.Path)
				break
			}
		}
	}
	return dataError
}

// mountOf returns the mount the path is in: the one with the longest mountpoint that contains it.
func mountOf(filePath string, mounts []zfsMount) (zfsMount, bool) {
	var found zfsMount
	ok := false
	for _, mount := range mounts {
		if isBelowOrSame(filePath, mount.Mountpoint) && (!ok || len(mount.Mountpoint) > len(found.Mountpoint)) {
			found, ok = mount, true
		}
	}
	return found, ok
}

// isBelowOrSame returns whether the path is dir or below it.
func isBelowOrSame(filePath string, dir string) bool {
	if dir == "/" {
		return strings.HasPrefix(filePath, "/")
	}
	return filePath == dir || strings.HasPrefix(filePath, dir+"/")
}

// indexOfAny returns the first index of one of the substrings in text, -1 if none is in it.
func indexOfAny(text string, substrings ...string) int {
	first := -1
	for _, substring := range substrings {
		if index := strings.Index(text, substring); index >= 0 && (first < 0 || index < first) {
			first = index
		}
	}
	return first
}

// ListDataErrors returns the damaged files of all pools ("zpool status -v"). Listing them needs root: if zpool does
// not list them, it is run again with "sudo -n", which works with the sudoers rule of the setup command. The pools
// whose list still cannot be read are DataErrorIndex.Unavailable. This spawns processes, so it must not be called
// on the UI thread.
func ListDataErrors() (*DataErrorIndex, error) {
	statuses, err := listDataErrorStatuses(false)
	if err != nil {
		return nil, err
	}
	// root (also with sudo) gets the lists of all pools: a missing errors field has another reason then
	var unavailable []string
	if !isRoot() {
		unavailable = deniedPools(statuses)
	}
	if len(unavailable) > 0 {
		sudoStatuses, sudoErr := listDataErrorStatuses(true)
		if sudoErr == nil {
			statuses, unavailable = sudoStatuses, nil
		} else if !strings.HasPrefix(sudoErr.Error(), "sudo:") && !errors.Is(sudoErr, exec.ErrNotFound) {
			return nil, sudoErr
		}
	}
	mounts, err := readZfsMounts()
	if err != nil {
		return nil, err
	}
	var dataErrors []DataError
	var withErrors []string
	for _, status := range statuses {
		for _, entry := range status.ErrorEntries {
			dataErrors = append(dataErrors, parseDataError(status.Name, entry, mounts))
		}
		if len(status.ErrorEntries) > 0 || status.Errors == "" {
			withErrors = append(withErrors, status.Name)
		}
	}
	index := NewDataErrorIndex(dataErrors, unavailable)
	index.WithoutHeadErrlog = poolsWithoutHeadErrlog(withErrors)
	return index, nil
}

// headErrlogFeature is the pool feature that reports a data error for every snapshot and clone sharing the damaged
// block (OpenZFS 2.2+), see DataErrorIndex.WithoutHeadErrlog.
const headErrlogFeature = "feature@head_errlog"

// EnableHeadErrlogCommand returns the command that enables the feature head_errlog on the pool. It cannot be undone:
// afterwards, the pool can only be imported by OpenZFS 2.2 or newer.
func EnableHeadErrlogCommand(pool string) string {
	return "sudo zpool set " + headErrlogFeature + "=enabled " + pool
}

// poolsWithoutHeadErrlog returns the pools of the given ones whose feature head_errlog is disabled. It needs no
// root. Empty if it cannot be read (e.g. a version of ZFS without the feature).
func poolsWithoutHeadErrlog(pools []string) []string {
	if len(pools) == 0 {
		return nil
	}
	output, err := runPoolCommand("zpool", append([]string{"get", "-H", "-o", "name,value", headErrlogFeature}, pools...)...)
	if err != nil {
		return nil
	}
	var disabled []string
	for _, line := range strings.Split(output, "\n") {
		if name, value, ok := strings.Cut(line, "\t"); ok && strings.TrimSpace(value) == "disabled" {
			disabled = append(disabled, name)
		}
	}
	return disabled
}

// listDataErrorStatuses returns the statuses of all pools with their damaged files, with "sudo -n" if asked to.
func listDataErrorStatuses(sudo bool) ([]*PoolStatus, error) {
	var output string
	var err error
	if sudo {
		output, err = runSudoCommand(append([]string{"-n", ZpoolPath()}, dataErrorsArgs...)...)
	} else {
		output, err = runPoolCommand("zpool", dataErrorsArgs...)
	}
	if err != nil {
		return nil, err
	}
	return parsePoolStatuses(output)
}

// isRoot returns whether the application runs as root. Replaceable in tests.
var isRoot = func() bool { return os.Geteuid() == 0 }

// deniedPools returns the pools whose damaged files were not listed because it needs root: their errors field is
// missing (see dataErrorsDenied). Only for the output of zpool run by another user than root.
func deniedPools(statuses []*PoolStatus) []string {
	var pools []string
	for _, status := range statuses {
		if status.Errors == "" || strings.HasPrefix(status.Errors, dataErrorsDenied) {
			pools = append(pools, status.Name)
		}
	}
	return pools
}
