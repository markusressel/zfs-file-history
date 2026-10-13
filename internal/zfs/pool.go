package zfs

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Health states of pools and their devices, as printed by "zpool list" and "zpool status".
const (
	PoolStateOnline    = "ONLINE"
	PoolStateDegraded  = "DEGRADED"
	PoolStateFaulted   = "FAULTED"
	PoolStateOffline   = "OFFLINE"
	PoolStateRemoved   = "REMOVED"
	PoolStateUnavail   = "UNAVAIL"
	PoolStateSuspended = "SUSPENDED"
	// PoolStateAvail and PoolStateInUse are the states of spares
	PoolStateAvail = "AVAIL"
	PoolStateInUse = "INUSE"
)

// PoolListEntry is a snapshot of the listing properties of a pool.
// All values are fetched up front, so reading them never spawns a zpool process.
type PoolListEntry struct {
	Name      string
	Size      uint64
	Allocated uint64
	Free      uint64
	// Fragmentation is the fragmentation of the free space in percent, or -1 if it is not known ("-")
	Fragmentation int
	// Capacity is the allocated space in percent of the size
	Capacity int
	// DedupRatio is the deduplication ratio, e.g. "1.00"
	DedupRatio string
	Health     string
}

func (entry PoolListEntry) TableRowId() string {
	return entry.Name
}

// poolListProperties are the properties requested by ListPools, in the order parsePoolList expects them.
var poolListProperties = []string{"name", "size", "allocated", "free", "fragmentation", "capacity", "dedupratio", "health"}

// ListPools returns the listing properties of all pools on the system.
// This executes "zpool list", so it must not be called on the UI thread.
func ListPools() ([]*PoolListEntry, error) {
	output, err := runZpool("list", "-H", "-p", "-o", strings.Join(poolListProperties, ","))
	if err != nil {
		return nil, err
	}
	return parsePoolList(output)
}

// parsePoolList parses the output of "zpool list -Hp -o <poolListProperties>".
func parsePoolList(output string) ([]*PoolListEntry, error) {
	var entries []*PoolListEntry
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != len(poolListProperties) {
			return nil, fmt.Errorf("unexpected output of zpool list: %q", line)
		}

		entry := &PoolListEntry{
			Name:       fields[0],
			DedupRatio: fields[6],
			Health:     fields[7],
		}
		sizes := []*uint64{&entry.Size, &entry.Allocated, &entry.Free}
		for i, size := range sizes {
			value, err := parseSizeProperty(fields[i+1])
			if err != nil {
				return nil, fmt.Errorf("cannot parse %s of %s: %w", poolListProperties[i+1], entry.Name, err)
			}
			*size = value
		}
		percentages := []*int{&entry.Fragmentation, &entry.Capacity}
		for i, percentage := range percentages {
			value, err := parsePercentProperty(fields[i+4])
			if err != nil {
				return nil, fmt.Errorf("cannot parse %s of %s: %w", poolListProperties[i+4], entry.Name, err)
			}
			*percentage = value
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// parsePercentProperty parses a percentage, as printed by "zpool list -p" (e.g. "39"). "-" (not known) is -1.
func parsePercentProperty(value string) (int, error) {
	if value == "-" {
		return -1, nil
	}
	return strconv.Atoi(strings.TrimSuffix(value, "%"))
}

// PoolStatus is the status of a pool, as shown by "zpool status".
type PoolStatus struct {
	Name  string
	State string
	// Status explains why the pool is not healthy (or which features are not enabled), "" if there is nothing to say
	Status string
	// Action tells what to do about Status
	Action string
	// See links to more information about Status
	See string
	// Scan describes the last or running scrub or resilver, one line per fact
	Scan string
	// ScanProgress is how much of a running scrub or resilver is done in percent, or -1 if none is running
	ScanProgress float64
	// Errors is the summary of the data errors, e.g. "No known data errors"
	Errors string
	// Other are the fields not known otherwise (e.g. "remove" or "checkpoint"), in their order
	Other []PoolStatusField
	// Devices are the devices of the pool, in the order of the config (parents before their children)
	Devices []*PoolDevice
}

// PoolStatusField is a field of the status of a pool, e.g. "remove: Removal of vdev 1 copied ...".
type PoolStatusField struct {
	Name  string
	Value string
}

// PoolDevice is a device of a pool: the pool itself, a vdev (e.g. "mirror-0"), a disk, or the name of a group of
// devices with a special purpose (e.g. "logs", "cache" or "spares"), which has no State.
type PoolDevice struct {
	Name string
	// Depth is the depth in the tree of devices: 0 for the pool and the groups, 1 for their children, and so on
	Depth int
	// State is the health state, e.g. "ONLINE", or "" for groups
	State    string
	Read     uint64
	Write    uint64
	Checksum uint64
	// Note is what is printed behind the error counters, e.g. "(resilvering)" or "was /dev/sdb"
	Note string
}

// IsGroup returns whether the device is the name of a group of devices, e.g. "logs".
func (device *PoolDevice) IsGroup() bool {
	return device.State == ""
}

// HasErrors returns whether there were read, write or checksum errors on the device.
func (device *PoolDevice) HasErrors() bool {
	return device.Read > 0 || device.Write > 0 || device.Checksum > 0
}

// IsScanning returns whether a scrub or resilver is running.
func (status *PoolStatus) IsScanning() bool {
	return status.ScanProgress >= 0
}

// ScanKind returns what the last or running scan is: "scrub" or "resilver", or "" if there was none.
func (status *PoolStatus) ScanKind() string {
	switch {
	case strings.HasPrefix(status.Scan, "scrub"):
		return "scrub"
	case strings.HasPrefix(status.Scan, "resilver"):
		return "resilver"
	}
	return ""
}

// scanFinishedPattern matches the end of a finished scrub or resilver, e.g. "... with 0 errors on Thu Oct  1
// 12:50:53 2026". Canceled or paused ones are not finished.
var scanFinishedPattern = regexp.MustCompile(` with [0-9]+ errors on (.+)$`)

// ScanFinished returns when the last scrub or resilver finished, in the local time zone. Returns false if it did not
// finish (e.g. it is running or was canceled), or there was none.
func (status *PoolStatus) ScanFinished() (time.Time, bool) {
	firstLine, _, _ := strings.Cut(status.Scan, "\n")
	match := scanFinishedPattern.FindStringSubmatch(firstLine)
	if match == nil {
		return time.Time{}, false
	}
	finished, err := time.ParseInLocation("Mon Jan _2 15:04:05 2006", match[1], time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return finished, true
}

// GetPoolStatus returns the status of the pool with the given name.
// This executes "zpool status", so it must not be called on the UI thread.
func GetPoolStatus(name string) (*PoolStatus, error) {
	if name == "" {
		return nil, errors.New("cannot get the status of a pool with an empty name")
	}
	output, err := runZpool("status", "-p", name)
	if err != nil {
		return nil, err
	}
	return parsePoolStatus(output)
}

func runZpool(args ...string) (string, error) {
	return runCommand("zpool", args...)
}

// poolStatusFieldPattern matches the first line of a field of "zpool status", e.g. " state: ONLINE" or
// "scan warning: skipped blocks ...". The names are right-aligned, continuation lines start with a tab instead.
var poolStatusFieldPattern = regexp.MustCompile(`^ *([a-z][a-z -]*): ?(.*)$`)

// scanProgressPattern matches the progress of a running scrub or resilver, e.g. "0B repaired, 21.30% done".
var scanProgressPattern = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)% done`)

// parsePoolStatus parses the output of "zpool status -p <pool>", see PoolStatus. Only the first pool is parsed.
func parsePoolStatus(output string) (*PoolStatus, error) {
	status := &PoolStatus{ScanProgress: -1}
	fields := map[string]*string{
		"pool":   &status.Name,
		"state":  &status.State,
		"status": &status.Status,
		"action": &status.Action,
		"see":    &status.See,
		"scan":   &status.Scan,
		"errors": &status.Errors,
	}

	// current is the value of the field being read, nil within the config
	var current *string
	inConfig := false
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}

		if !strings.HasPrefix(line, "\t") {
			match := poolStatusFieldPattern.FindStringSubmatch(line)
			if match == nil {
				return nil, fmt.Errorf("unexpected output of zpool status: %q", line)
			}
			name, value := match[1], strings.TrimSpace(match[2])
			if name == "pool" && status.Name != "" {
				// the next pool
				break
			}
			inConfig = name == "config"
			current = nil
			if inConfig {
				continue
			}
			if field, ok := fields[name]; ok {
				current = field
			} else {
				status.Other = append(status.Other, PoolStatusField{Name: name})
				current = &status.Other[len(status.Other)-1].Value
			}
			*current = value
			continue
		}

		if inConfig {
			device := parsePoolDevice(strings.TrimPrefix(line, "\t"))
			if device != nil {
				status.Devices = append(status.Devices, device)
			}
			continue
		}

		// a continuation line of the current field
		if current == nil {
			return nil, fmt.Errorf("unexpected output of zpool status: %q", line)
		}
		*current = strings.TrimSpace(*current + "\n" + strings.TrimSpace(line))
	}

	if status.Name == "" {
		return nil, errors.New("unexpected output of zpool status: no pool found")
	}
	if strings.Contains(status.Scan, "in progress") {
		if match := scanProgressPattern.FindStringSubmatch(status.Scan); match != nil {
			status.ScanProgress, _ = strconv.ParseFloat(match[1], 64)
		} else {
			// e.g. just started: no progress yet
			status.ScanProgress = 0
		}
	}
	return status, nil
}

// parsePoolDevice parses a line of the config of "zpool status -p" (without the leading tab), e.g.
// "    sda  ONLINE  0  0  0  (resilvering)". Each level of the tree is indented by two spaces. It returns nil for
// the header line ("NAME STATE READ WRITE CKSUM").
func parsePoolDevice(line string) *PoolDevice {
	trimmed := strings.TrimLeft(line, " ")
	depth := (len(line) - len(trimmed)) / 2
	fields := strings.Fields(trimmed)
	if len(fields) == 0 || (depth == 0 && len(fields) >= 2 && fields[0] == "NAME" && fields[1] == "STATE") {
		return nil
	}

	device := &PoolDevice{Name: fields[0], Depth: depth}
	if len(fields) == 1 {
		return device
	}
	device.State = fields[1]

	// spares have no error counters, e.g. "sdc  INUSE  currently in use"
	noteStart := 2
	if len(fields) >= 5 {
		counters := []*uint64{&device.Read, &device.Write, &device.Checksum}
		parsed := true
		for i, counter := range counters {
			value, err := strconv.ParseUint(fields[i+2], 10, 64)
			if err != nil {
				parsed = false
				break
			}
			*counter = value
		}
		if parsed {
			noteStart = 5
		} else {
			device.Read, device.Write, device.Checksum = 0, 0, 0
		}
	}
	device.Note = strings.Join(fields[noteStart:], " ")
	return device
}
