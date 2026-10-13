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

// ListPools returns the listing properties of all pools on the system.
// This executes "zpool list", so it must not be called on the UI thread.
func ListPools() ([]*PoolListEntry, error) {
	return backend.ListPools()
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
	// ErrorEntries are the damaged files and objects listed below Errors by "zpool status -v", as printed, e.g.
	// "/home/user/file", "tank/data@daily:/file" or "<metadata>:<0x1b>". Empty without -v.
	ErrorEntries []string
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
	// Note is what is printed behind the error counters, e.g. "(resilvering)" or "was /dev/sdb", without Trim
	Note string
	// Trim is the TRIM state printed by "zpool status -t", e.g. "(12% trimmed, started at Tue Oct 13 10:00:01
	// 2026)" or "(untrimmed)", "" if none is printed (e.g. for vdevs)
	Trim string
}

// IsGroup returns whether the device is the name of a group of devices, e.g. "logs".
func (device *PoolDevice) IsGroup() bool {
	return device.State == ""
}

// HasErrors returns whether there were read, write or checksum errors on the device.
func (device *PoolDevice) HasErrors() bool {
	return device.Read > 0 || device.Write > 0 || device.Checksum > 0
}

// trimPattern matches the TRIM state of a device, see PoolDevice.Trim.
var trimPattern = regexp.MustCompile(`\((?:[0-9]+% trimmed[^)]*|untrimmed|trim unsupported)\)`)

// trimRunningPattern matches the TRIM state of a device that is being trimmed, e.g. "(12% trimmed, started at
// ...)". Suspended or completed ones say "suspended at" or "completed at" instead.
var trimRunningPattern = regexp.MustCompile(`^\(([0-9]+)% trimmed, started at ([^)]+)\)`)

// now is replaceable in tests.
var now = time.Now

// TrimRemaining estimates the time left of the running TRIM, which zpool status does not: from how long each device
// has been trimmed and how much of it is done. The slowest device decides. Returns false if none is running, or no
// device made progress yet.
func (status *PoolStatus) TrimRemaining() (time.Duration, bool) {
	var remaining time.Duration
	known := false
	for _, device := range status.Devices {
		match := trimRunningPattern.FindStringSubmatch(device.Trim)
		if match == nil {
			continue
		}
		percent, _ := strconv.Atoi(match[1])
		started, err := time.ParseInLocation("Mon Jan _2 15:04:05 2006", match[2], time.Local)
		elapsed := now().Sub(started)
		if err != nil || percent <= 0 || percent >= 100 || elapsed <= 0 {
			continue
		}
		remaining = max(remaining, time.Duration(float64(elapsed)*float64(100-percent)/float64(percent)))
		known = true
	}
	return remaining, known
}

// TrimProgress returns how much of the running TRIM is done in percent (the average of the devices being trimmed),
// or -1 if none is running.
func (status *PoolStatus) TrimProgress() float64 {
	total, count := 0, 0
	for _, device := range status.Devices {
		if match := trimRunningPattern.FindStringSubmatch(device.Trim); match != nil {
			percent, _ := strconv.Atoi(match[1])
			total += percent
			count++
		}
	}
	if count == 0 {
		return -1
	}
	return float64(total) / float64(count)
}

// IsTrimming returns whether a TRIM is running on one of the devices.
func (status *PoolStatus) IsTrimming() bool {
	return status.TrimProgress() >= 0
}

// IsScrubPaused returns whether the last scrub was paused (and can be resumed).
func (status *PoolStatus) IsScrubPaused() bool {
	return strings.HasPrefix(status.Scan, "scrub paused")
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
	return backend.PoolStatus(name)
}

// poolStatusFieldPattern matches the first line of a field of "zpool status", e.g. " state: ONLINE" or
// "scan warning: skipped blocks ...". The names are right-aligned, continuation lines start with a tab instead.
var poolStatusFieldPattern = regexp.MustCompile(`^ *([a-z][a-z -]*): ?(.*)$`)

// scanProgressPattern matches the progress of a running scrub or resilver, e.g. "0B repaired, 21.30% done".
var scanProgressPattern = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)% done`)

// ListPoolStatuses returns the status of all pools on the system, see PoolStatus.
// This executes "zpool status", so it must not be called on the UI thread.
func ListPoolStatuses() ([]*PoolStatus, error) {
	return backend.PoolStatuses()
}

// noPoolsOutput is what "zpool status" prints if there are no pools.
const noPoolsOutput = "no pools available"

// parsePoolStatus parses the output of "zpool status -p -t <pool>", see PoolStatus. Only the first pool is returned.
func parsePoolStatus(output string) (*PoolStatus, error) {
	statuses, err := parsePoolStatuses(output)
	if err != nil {
		return nil, err
	}
	if len(statuses) == 0 {
		return nil, errors.New("unexpected output of zpool status: no pool found")
	}
	return statuses[0], nil
}

// parsePoolStatuses parses the output of "zpool status -p -t", see PoolStatus: one status per pool, in their order.
func parsePoolStatuses(output string) ([]*PoolStatus, error) {
	if strings.TrimSpace(output) == noPoolsOutput {
		return nil, nil
	}

	var statuses []*PoolStatus
	// status is the pool being read, nil before the first one
	var status *PoolStatus
	var fields map[string]*string
	// current is the value of the field being read, nil within the config
	var current *string
	inConfig := false
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}

		// the damaged files of "zpool status -v" are indented by 8 spaces, which no field name is (they are aligned
		// to 6 columns), and may look like a field (e.g. "tank:<0x0>")
		if status != nil && current == &status.Errors && isErrorEntry(line) {
			status.ErrorEntries = append(status.ErrorEntries, strings.TrimLeft(line, " \t"))
			continue
		}

		if !strings.HasPrefix(line, "\t") {
			match := poolStatusFieldPattern.FindStringSubmatch(line)
			if match == nil {
				return nil, fmt.Errorf("unexpected output of zpool status: %q", line)
			}
			name, value := match[1], strings.TrimSpace(match[2])
			if name == "pool" {
				status = &PoolStatus{ScanProgress: -1}
				statuses = append(statuses, status)
				fields = map[string]*string{
					"pool":   &status.Name,
					"state":  &status.State,
					"status": &status.Status,
					"action": &status.Action,
					"see":    &status.See,
					"scan":   &status.Scan,
					"errors": &status.Errors,
				}
			} else if status == nil {
				return nil, fmt.Errorf("unexpected output of zpool status: %q before the first pool", line)
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

	if len(statuses) == 0 {
		return nil, errors.New("unexpected output of zpool status: no pool found")
	}
	for _, status := range statuses {
		if strings.Contains(status.Scan, "in progress") {
			status.ScanProgress = parseProgress(status.Scan)
		}
	}
	return statuses, nil
}

// errorEntryIndent is how the damaged files are indented by "zpool status -v".
const errorEntryIndent = "        "

// isErrorEntry returns whether a line within the errors field of zpool status is a damaged file of "zpool status
// -v" (see PoolStatus.ErrorEntries), not a continuation of the summary or the next field.
func isErrorEntry(line string) bool {
	return strings.HasPrefix(line, errorEntryIndent)
}

// parseProgress returns the progress of a running operation in percent, e.g. 21.3 for "0B repaired, 21.30% done".
// Returns 0 if there is no progress yet (e.g. just started).
func parseProgress(text string) float64 {
	if match := scanProgressPattern.FindStringSubmatch(text); match != nil {
		progress, _ := strconv.ParseFloat(match[1], 64)
		return progress
	}
	return 0
}

// remainingPattern matches the estimated time left of a running operation, e.g. "31.77% done, 02:10:31 to go".
var remainingPattern = regexp.MustCompile(`, ([^,\n]+) to go`)

// PoolActivity is an operation running on a pool, e.g. a scrub, see PoolStatus.Activities.
type PoolActivity struct {
	Pool string
	// Kind is what runs: "scrub", "resilver" or "trim", or the name of the status field that reports it, e.g. "remove" (a
	// device is removed) or "expand" (a raidz is expanded)
	Kind string
	// Progress is how much is done in percent
	Progress float64
	// Remaining is the estimated time left, if RemainingKnown. zpool status prints it in several formats (see
	// parseRemaining), so it is up to the UI to show it in one.
	Remaining      time.Duration
	RemainingKnown bool
	// Processed and Total are how much is done of how much as printed by zpool status (e.g. "1.02T" of "3.21T"),
	// and Rate how fast it goes (e.g. "294M/s"). "" if not known (e.g. the rate of a paused scrub).
	Processed string
	Total     string
	Rate      string
}

// Activities returns the operations running on the pool: the running scrub or resilver, a running TRIM, and the
// other fields that report an operation in progress (e.g. the removal of a device). Empty if nothing runs.
func (status *PoolStatus) Activities() []PoolActivity {
	var activities []PoolActivity
	if status.IsScanning() {
		kind := status.ScanKind()
		if kind == "" {
			kind = "scan"
		}
		activity := PoolActivity{Pool: status.Name, Kind: kind, Progress: status.ScanProgress}
		activity.Remaining, activity.RemainingKnown = parseRemaining(status.Scan)
		activity.Processed, activity.Total, activity.Rate = parseAmounts(status.Scan)
		activities = append(activities, activity)
	}
	if progress := status.TrimProgress(); progress >= 0 {
		activity := PoolActivity{Pool: status.Name, Kind: "trim", Progress: progress}
		// zpool status does not estimate the time left of a TRIM
		activity.Remaining, activity.RemainingKnown = status.TrimRemaining()
		activities = append(activities, activity)
	}
	for _, other := range status.Other {
		if strings.Contains(other.Value, "in progress") {
			activity := PoolActivity{Pool: status.Name, Kind: other.Name, Progress: parseProgress(other.Value)}
			activity.Remaining, activity.RemainingKnown = parseRemaining(other.Value)
			activity.Processed, activity.Total, activity.Rate = parseAmounts(other.Value)
			activities = append(activities, activity)
		}
	}
	return activities
}

// amountPatterns match how much of a running operation is done and how fast it goes, as printed by zpool status
// (sizes formatted like "1.43T" or "512B", also with -p): the issued part of a scrub or resilver ("1.43T / 3.21T
// scanned at 412M/s, 1.02T / 3.21T issued at 294M/s"; scanning, not issuing, finds what to verify), the removal of a
// device ("2.10G copied out of 5.00G at 1.00M/s") and a raidz expansion ("1.23T / 4.56T copied at 123M/s"). The
// rate is missing e.g. while paused.
var amountPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(` + sizePattern + `) / (` + sizePattern + `) issued(?: at (` + sizePattern + `/s))?`),
	regexp.MustCompile(`(` + sizePattern + `) copied out of (` + sizePattern + `)(?: at (` + sizePattern + `/s))?`),
	regexp.MustCompile(`(` + sizePattern + `) / (` + sizePattern + `) copied(?: at (` + sizePattern + `/s))?`),
}

// scanRatePattern matches the rate of scanning a scrub or resilver, e.g. "scanned at 412M/s".
var scanRatePattern = regexp.MustCompile(`scanned at (` + sizePattern + `/s)`)

// sizePattern matches a size as formatted by zpool status, e.g. "1.43T", "254G" or "512B".
const sizePattern = `[0-9]+(?:\.[0-9]+)?[BKMGTPEZ]?`

// parseAmounts returns how much of a running operation is done of how much, and how fast it goes (see
// amountPatterns). Before a scrub issues anything, the rate is the one of scanning. "" for what is not known.
func parseAmounts(text string) (processed string, total string, rate string) {
	for _, pattern := range amountPatterns {
		if match := pattern.FindStringSubmatch(text); match != nil {
			processed, total, rate = match[1], match[2], match[3]
			break
		}
	}
	if rate == "" {
		if match := scanRatePattern.FindStringSubmatch(text); match != nil {
			rate = match[1]
		}
	}
	return processed, total, rate
}

// remainingFormats are the formats of the time left printed by zpool status: a scrub or resilver ("02:10:31", "1 days
// 07:53:32"), a raidz expansion ("0 days 07:53:32") and the removal of a device ("0h48m").
var remainingFormats = []*regexp.Regexp{
	regexp.MustCompile(`^(?:([0-9]+) days )?([0-9]+):([0-9]{2}):([0-9]{2})$`),
	regexp.MustCompile(`^()([0-9]+)h([0-9]+)m()$`),
}

// parseRemaining returns the estimated time left of a running operation (see remainingPattern), false if not known
// (e.g. "no estimated completion time", or a format not in remainingFormats).
func parseRemaining(text string) (time.Duration, bool) {
	match := remainingPattern.FindStringSubmatch(text)
	if match == nil {
		return 0, false
	}
	for _, format := range remainingFormats {
		if parts := format.FindStringSubmatch(strings.TrimSpace(match[1])); parts != nil {
			number := func(part string) time.Duration {
				value, _ := strconv.Atoi(part)
				return time.Duration(value)
			}
			return number(parts[1])*24*time.Hour + number(parts[2])*time.Hour + number(parts[3])*time.Minute +
				number(parts[4])*time.Second, true
		}
	}
	return 0, false
}

// parsePoolDevice parses a line of the config of "zpool status -p -t" (without the leading tab), e.g.
// "    sda  ONLINE  0  0  0  (resilvering)  (untrimmed)". Each level of the tree is indented by two spaces. It returns nil for
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
	note := strings.Join(fields[noteStart:], " ")
	if location := trimPattern.FindStringIndex(note); location != nil {
		device.Trim = note[location[0]:location[1]]
		note = note[:location[0]] + note[location[1]:]
	}
	device.Note = strings.Join(strings.Fields(note), " ")
	return device
}
