package disk

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SmartctlArgs are the arguments of smartctl in front of the device: JSON output (-j), the model, serial number and
// so on (-i), the health (-H) and the attributes (-A), skipping disks that sleep (-n standby: smartctl does not wake them up, it exits instead). "--"
// ends the options, so the sudoers rule (see zfs.SudoersRuleSmart) allows no others: smartctl takes only one device,
// further arguments (e.g. "-t long") are refused.
var SmartctlArgs = []string{"-j", "-i", "-H", "-A", "-n", "standby", "--"}

// SmartctlPaths are the paths smartctl is installed at by the distributions, used in the sudoers rules.
var SmartctlPaths = []string{"/usr/sbin/smartctl", "/usr/bin/smartctl", "/sbin/smartctl"}

// isRotational returns whether the disk spins, see IsRotational. Replaceable in tests.
var isRotational = IsRotational

// lookPathSmartctl returns the path of smartctl, "" if it is not installed. Replaceable in tests.
var lookPathSmartctl = sync.OnceValue(func() string {
	path, err := exec.LookPath("smartctl")
	if err != nil {
		return ""
	}
	return path
})

// SmartctlPath returns the path of smartctl. Sudo needs the full path, which is also the one in the sudoers rules.
func SmartctlPath() string {
	if path := lookPathSmartctl(); path != "" {
		return path
	}
	return SmartctlPaths[0]
}

// SmartctlInstalled returns whether smartctl is installed. Replaceable in tests.
var SmartctlInstalled = func() bool {
	return lookPathSmartctl() != ""
}

var (
	// ErrSmartNeedsRoot is the error of a disk whose SMART values could not be read because only root may, and sudo
	// is not allowed to run smartctl without a password (see zfs.SudoersRuleSmart).
	ErrSmartNeedsRoot = errors.New("reading SMART values needs root: allow it with \"zfs-file-history setup --smart\"")
	// ErrSmartctlMissing is the error of all disks if smartctl is not installed.
	ErrSmartctlMissing = errors.New("smartctl is not installed (smartmontools)")
)

// Replaceable in tests, so they never run smartctl or sudo.
var (
	// runSmartctl runs smartctl for the device (e.g. "/dev/sda"), with "sudo -n" unless the application runs as
	// root. It returns what smartctl printed (JSON, also if it exits with an error, its exit status is a bit mask),
	// and the error output of sudo.
	runSmartctl = func(device string) (stdout []byte, stderr string, err error) {
		args := append(append([]string{}, SmartctlArgs...), device)
		command := exec.Command("sudo", append([]string{"-n", SmartctlPath()}, args...)...)
		if os.Geteuid() == 0 {
			command = exec.Command(SmartctlPath(), args...)
		}
		var out, errOut bytes.Buffer
		command.Stdout, command.Stderr = &out, &errOut
		err = command.Run()
		return out.Bytes(), strings.TrimSpace(errOut.String()), err
	}
)

// SmartSeverity is how bad a SMART problem is, see SmartProblem.
type SmartSeverity int

const (
	// SmartWarning is a sign of wear or of a coming failure, e.g. reallocated sectors
	SmartWarning SmartSeverity = iota + 1
	// SmartError is a disk that fails, e.g. its health check failed
	SmartError
)

// SmartProblem is a problem found in the SMART values of a disk.
type SmartProblem struct {
	Severity SmartSeverity
	// Short is a few characters for a column, e.g. "8 realloc"
	Short string
	// Text describes it, e.g. "8 reallocated sectors"
	Text string
}

// SmartAttribute is a SMART value of a disk, e.g. an ATA attribute or a field of the health log of NVMe drives.
type SmartAttribute struct {
	// Id is the id of an ATA attribute, 0 for others
	Id   int
	Name string
	// Value is the raw value, as smartctl prints it
	Value string
	// Raw is the raw value as a number (e.g. for sorting), -1 if it has none
	Raw int64
	// Normalized, Worst and Threshold are the normalized values of an ATA attribute (the lower, the worse; the
	// attribute fails at or below Threshold), -1 for others
	Normalized, Worst, Threshold int
	// Failed is "now" if the attribute is at or below its threshold, "past" if it was, "" otherwise
	Failed string
	// Important is whether the attribute tells of failures, see importantAtaAttributes
	Important bool
	// Used is how far the attribute went from its value of a new disk to the one it fails at: 0 new, 1 failing (or
	// all of the rated endurance used), valid if UsedKnown. Known for ATA attributes that can fail (a threshold
	// above 0, see ataUsed), and the endurance and spare of NVMe disks.
	Used      float64
	UsedKnown bool
}

// ataUsed returns how far an ATA attribute went toward its threshold (see SmartAttribute.Used), false if it cannot
// fail (threshold 0). smartctl does not tell the value of a new disk: vendors start at 100, 200 or 253, so it is the
// smallest of them that is at least the normalized and the worst value. A disk that started below (e.g. at 100 of
// 200) shows as more used than it is.
func ataUsed(normalized int, worst int, threshold int) (float64, bool) {
	if threshold <= 0 || normalized < 0 {
		return 0, false
	}
	start := 253
	for _, candidate := range []int{100, 200} {
		if max(normalized, worst) <= candidate {
			start = candidate
			break
		}
	}
	if threshold >= start {
		return 0, false
	}
	return min(max(float64(start-normalized)/float64(start-threshold), 0), 1), true
}

// SmartReport is what is known about the SMART values of a disk, see SmartReader.
type SmartReport struct {
	// Disk is the kernel name of the disk, e.g. "sda"
	Disk string
	// CheckedAt is when smartctl ran last, ReadAt when it read the values (zero if never)
	CheckedAt time.Time
	ReadAt    time.Time
	// Standby is whether the disk slept when smartctl ran last, so it was not read (the values are the ones of
	// ReadAt, if any)
	Standby bool
	// Err is why the values could not be read, e.g. ErrSmartNeedsRoot. nil if they were (or the disk slept).
	Err error

	Model, Serial, Firmware string
	// Capacity is the size of the disk in bytes, 0 if unknown
	Capacity int64
	// Protocol is "ATA", "NVMe" or "SCSI"
	Protocol string
	// HealthKnown is whether the disk reported the result of its health check, Passed that result
	HealthKnown, Passed bool
	// PowerOnHours is how long the disk has run, -1 if unknown
	PowerOnHours int64
	// Rotational is whether the disk spins (a hard disk, see IsRotational), known also if the values were not read
	Rotational bool
	// Counters are the values that mean the same for all kinds of disks, to compare them
	Counters   SmartCounters
	Attributes []SmartAttribute
	// Problems are what is wrong, the most severe first, see problems
	Problems []SmartProblem
}

// HasValues returns whether the values of the disk were read (now or before it went to sleep).
func (report SmartReport) HasValues() bool {
	return !report.ReadAt.IsZero()
}

// Worst returns the most severe problem, false if there is none.
func (report SmartReport) Worst() (SmartProblem, bool) {
	if len(report.Problems) == 0 {
		return SmartProblem{}, false
	}
	return report.Problems[0], true
}

// WorstProblem returns the most severe problem of the reports (e.g. of the disks of a device), false if there is
// none.
func WorstProblem(reports []SmartReport) (SmartProblem, bool) {
	var worst SmartProblem
	found := false
	for _, report := range reports {
		if problem, ok := report.Worst(); ok && (!found || problem.Severity > worst.Severity) {
			worst, found = problem, true
		}
	}
	return worst, found
}

// importantAtaAttribute is an ATA attribute whose raw value is a count of failures: anything but 0 is a problem.
type importantAtaAttribute struct {
	// short and text name the count, e.g. "realloc" and "reallocated sectors"
	short string
	text  string
}

// importantAtaAttributes are the ATA attributes that tell of failures, by id (the ones Backblaze found to predict
// them, without the ones that grow without failures, e.g. command timeouts and CRC errors of the cable).
var importantAtaAttributes = map[int]importantAtaAttribute{
	5:   {"realloc", "reallocated sectors"},
	10:  {"spin retry", "spin retries"},
	184: {"end-to-end", "end-to-end errors"},
	187: {"uncorr", "reported uncorrectable errors"},
	197: {"pending", "pending sectors"},
	198: {"offline unc", "offline uncorrectable sectors"},
}

// temperatureAttributes are the ids of the ATA attributes of the temperature: at or below their threshold, the disk
// is too hot, which is shown by its temperature as well (see Sampler).
var temperatureAttributes = []int{190, 194}

// failingAttributeProblem returns the problem of an ATA attribute at or below its threshold. Only one that tells of
// a coming failure (prefailure, like smartctl's exit status bit 4) is an error; one of old age a warning, e.g. a
// temperature above the limit of the disk (e.g. Airflow_Temperature_Cel of Seagate disks during a scrub).
func failingAttributeProblem(id int, name string, prefailure bool) SmartProblem {
	switch {
	case prefailure:
		return SmartProblem{Severity: SmartError, Short: "failing", Text: name + " is at or below its threshold: the disk may fail soon"}
	case slices.Contains(temperatureAttributes, id):
		return SmartProblem{Severity: SmartWarning, Short: "too hot", Text: name + " is past its threshold: the disk is too hot"}
	default:
		return SmartProblem{Severity: SmartWarning, Short: "worn", Text: name + " is at or below its threshold (old age)"}
	}
}

// nvmeCriticalWarnings are the bits of the critical warning of NVMe drives.
var nvmeCriticalWarnings = []string{
	"available spare below threshold",
	"temperature out of range",
	"reliability degraded",
	"read-only",
	"volatile memory backup failed",
	"persistent memory region read-only",
}

// WornWarningPercent is the wear of an NVMe drive ("percentage used") from which it is a problem.
const WornWarningPercent = 90

// smartctlOutput is the part of the JSON output of smartctl that is used. Counters that may exceed 64 bits (NVMe)
// are numbers, see jsonCount.
type smartctlOutput struct {
	Smartctl struct {
		Messages []struct {
			String   string `json:"string"`
			Severity string `json:"severity"`
		} `json:"messages"`
		ExitStatus int `json:"exit_status"`
	} `json:"smartctl"`
	Device struct {
		Protocol string `json:"protocol"`
	} `json:"device"`
	// PowerMode is the power mode of ATA disks checked by -n, see powerModeName
	PowerMode    json.RawMessage `json:"power_mode"`
	ModelName    string          `json:"model_name"`
	ScsiModel    string          `json:"scsi_model_name"`
	SerialNumber string          `json:"serial_number"`
	Firmware     string          `json:"firmware_version"`
	ScsiRevision string          `json:"scsi_revision"`
	// LogicalBlockSize is the size of the blocks ATA attribute 241 (Total_LBAs_Written) counts
	LogicalBlockSize int64 `json:"logical_block_size"`
	UserCapacity     *struct {
		Bytes json.Number `json:"bytes"`
	} `json:"user_capacity"`
	SmartStatus *struct {
		Passed bool `json:"passed"`
	} `json:"smart_status"`
	PowerOnTime *struct {
		Hours int64 `json:"hours"`
	} `json:"power_on_time"`
	AtaSmartAttributes *struct {
		Table []struct {
			Id         int    `json:"id"`
			Name       string `json:"name"`
			Value      int    `json:"value"`
			Worst      int    `json:"worst"`
			Thresh     int    `json:"thresh"`
			WhenFailed string `json:"when_failed"`
			Flags      struct {
				Prefailure bool `json:"prefailure"`
			} `json:"flags"`
			Raw struct {
				Value  json.Number `json:"value"`
				String string      `json:"string"`
			} `json:"raw"`
		} `json:"table"`
	} `json:"ata_smart_attributes"`
	NvmeLog *struct {
		CriticalWarning         int         `json:"critical_warning"`
		AvailableSpare          int         `json:"available_spare"`
		AvailableSpareThreshold int         `json:"available_spare_threshold"`
		PercentageUsed          int         `json:"percentage_used"`
		MediaErrors             json.Number `json:"media_errors"`
		NumErrLogEntries        json.Number `json:"num_err_log_entries"`
		UnsafeShutdowns         json.Number `json:"unsafe_shutdowns"`
		PowerOnHours            json.Number `json:"power_on_hours"`
		DataUnitsWritten        json.Number `json:"data_units_written"`
		PowerCycles             json.Number `json:"power_cycles"`
	} `json:"nvme_smart_health_information_log"`
	ScsiGrownDefectList *json.Number `json:"scsi_grown_defect_list"`
	ScsiErrorCounterLog map[string]struct {
		TotalUncorrectedErrors json.Number `json:"total_uncorrected_errors"`
	} `json:"scsi_error_counter_log"`
}

// jsonCount returns a counter of the output, 0 if it is missing, the largest int64 if it is larger.
func jsonCount(number json.Number) int64 {
	if number == "" {
		return 0
	}
	if value, err := strconv.ParseInt(string(number), 10, 64); err == nil {
		return value
	}
	if value, err := strconv.ParseFloat(string(number), 64); err == nil && value > 0 {
		return 1<<63 - 1
	}
	return 0
}

// powerModeName returns the name of the power mode of the output, e.g. "STANDBY", "SLEEP" or "ACTIVE or IDLE", ""
// if there is none. smartctl writes an object ({"ata_value": 0, "name": "STANDBY"}); a plain string is accepted too.
func powerModeName(raw json.RawMessage) string {
	var mode struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &mode) == nil {
		return mode.Name
	}
	var name string
	if json.Unmarshal(raw, &name) == nil {
		return name
	}
	return ""
}

// parseSmartctl parses the output of smartctl (see SmartctlArgs) into a report of the disk read at the time.
// standby is set if the disk slept, so nothing was read. The error tells why nothing was read, e.g. the device
// could not be opened.
func parseSmartctl(disk string, output []byte, at time.Time) (report SmartReport, standby bool, err error) {
	var parsed smartctlOutput
	if err := json.Unmarshal(output, &parsed); err != nil {
		return SmartReport{}, false, fmt.Errorf("unexpected output of smartctl: %w", err)
	}
	var messages []string
	for _, message := range parsed.Smartctl.Messages {
		text := strings.TrimSpace(message.String)
		// "Device is in STANDBY mode, exit(2)", also SLEEP
		if strings.HasPrefix(text, "Device is in ") && strings.Contains(text, " mode") {
			return SmartReport{}, true, nil
		}
		if message.Severity == "error" {
			messages = append(messages, text)
		}
	}
	// smartctl skipped it (see the -n option in ataprint.cpp), also if the message is worded differently
	if mode := powerModeName(parsed.PowerMode); parsed.SmartStatus == nil && (strings.HasPrefix(mode, "STANDBY") || mode == "SLEEP") {
		return SmartReport{}, true, nil
	}
	// bit 1: the device could not be opened, or did not answer
	if parsed.Smartctl.ExitStatus&3 != 0 && parsed.SmartStatus == nil && parsed.AtaSmartAttributes == nil && parsed.NvmeLog == nil {
		if len(messages) == 0 {
			messages = append(messages, fmt.Sprintf("smartctl failed (exit status %d)", parsed.Smartctl.ExitStatus))
		}
		return SmartReport{}, false, errors.New(strings.Join(messages, "; "))
	}

	report = SmartReport{
		Disk:         disk,
		CheckedAt:    at,
		ReadAt:       at,
		Model:        parsed.ModelName,
		Serial:       parsed.SerialNumber,
		Protocol:     parsed.Device.Protocol,
		PowerOnHours: -1,
	}
	if report.Model == "" {
		report.Model = parsed.ScsiModel
	}
	report.Firmware = parsed.Firmware
	if report.Firmware == "" {
		report.Firmware = parsed.ScsiRevision
	}
	if parsed.UserCapacity != nil {
		report.Capacity = jsonCount(parsed.UserCapacity.Bytes)
	}
	if parsed.SmartStatus != nil {
		report.HealthKnown, report.Passed = true, parsed.SmartStatus.Passed
	}
	if parsed.PowerOnTime != nil {
		report.PowerOnHours = parsed.PowerOnTime.Hours
	}

	var problems []SmartProblem
	if report.HealthKnown && !report.Passed {
		problems = append(problems, SmartProblem{Severity: SmartError, Short: "FAILED", Text: "the health check of the disk failed"})
	}
	if parsed.AtaSmartAttributes != nil {
		for _, attribute := range parsed.AtaSmartAttributes.Table {
			important, isImportant := importantAtaAttributes[attribute.Id]
			used, usedKnown := ataUsed(attribute.Value, attribute.Worst, attribute.Thresh)
			report.Attributes = append(report.Attributes, SmartAttribute{
				Id: attribute.Id, Name: attribute.Name, Value: attribute.Raw.String, Raw: jsonCount(attribute.Raw.Value),
				Normalized: attribute.Value, Worst: attribute.Worst, Threshold: attribute.Thresh,
				Failed: attribute.WhenFailed, Important: isImportant, Used: used, UsedKnown: usedKnown,
			})
			if attribute.WhenFailed == "now" {
				problems = append(problems, failingAttributeProblem(attribute.Id, attribute.Name, attribute.Flags.Prefailure))
			}
			if count := jsonCount(attribute.Raw.Value); isImportant && count > 0 {
				problems = append(problems, SmartProblem{Severity: SmartWarning,
					Short: fmt.Sprintf("%d %s", count, important.short), Text: fmt.Sprintf("%d %s", count, important.text)})
			}
		}
	}
	if log := parsed.NvmeLog; log != nil {
		mediaErrors := jsonCount(log.MediaErrors)
		// the spare goes down from 100% to its threshold, where the disk warns
		spareUsed, spareKnown := 0.0, log.AvailableSpareThreshold > 0 && log.AvailableSpareThreshold < 100
		if spareKnown {
			spareUsed = min(max(float64(100-log.AvailableSpare)/float64(100-log.AvailableSpareThreshold), 0), 1)
		}
		report.Attributes = append(report.Attributes,
			SmartAttribute{Name: "Critical warning", Value: fmt.Sprintf("0x%02x", log.CriticalWarning), Raw: int64(log.CriticalWarning), Normalized: -1, Worst: -1, Threshold: -1, Important: true},
			SmartAttribute{Name: "Available spare", Value: fmt.Sprintf("%d%% (threshold %d%%)", log.AvailableSpare, log.AvailableSpareThreshold), Raw: int64(log.AvailableSpare), Normalized: -1, Worst: -1, Threshold: -1, Important: true, Used: spareUsed, UsedKnown: spareKnown},
			SmartAttribute{Name: "Percentage used", Value: fmt.Sprintf("%d%%", log.PercentageUsed), Raw: int64(log.PercentageUsed), Normalized: -1, Worst: -1, Threshold: -1, Important: true, Used: min(float64(log.PercentageUsed)/100, 1), UsedKnown: true},
			SmartAttribute{Name: "Media errors", Value: strconv.FormatInt(mediaErrors, 10), Raw: mediaErrors, Normalized: -1, Worst: -1, Threshold: -1, Important: true},
			SmartAttribute{Name: "Error log entries", Value: strconv.FormatInt(jsonCount(log.NumErrLogEntries), 10), Raw: jsonCount(log.NumErrLogEntries), Normalized: -1, Worst: -1, Threshold: -1},
			SmartAttribute{Name: "Unsafe shutdowns", Value: strconv.FormatInt(jsonCount(log.UnsafeShutdowns), 10), Raw: jsonCount(log.UnsafeShutdowns), Normalized: -1, Worst: -1, Threshold: -1},
			SmartAttribute{Name: "Data units written", Value: strconv.FormatInt(jsonCount(log.DataUnitsWritten), 10), Raw: jsonCount(log.DataUnitsWritten), Normalized: -1, Worst: -1, Threshold: -1},
			SmartAttribute{Name: "Power cycles", Value: strconv.FormatInt(jsonCount(log.PowerCycles), 10), Raw: jsonCount(log.PowerCycles), Normalized: -1, Worst: -1, Threshold: -1},
		)
		if report.PowerOnHours < 0 && log.PowerOnHours != "" {
			report.PowerOnHours = jsonCount(log.PowerOnHours)
		}
		if log.CriticalWarning != 0 {
			var warnings []string
			for bit, warning := range nvmeCriticalWarnings {
				if log.CriticalWarning&(1<<bit) != 0 {
					warnings = append(warnings, warning)
				}
			}
			problems = append(problems, SmartProblem{Severity: SmartError, Short: "critical",
				Text: "critical warning: " + strings.Join(warnings, ", ")})
		}
		if mediaErrors > 0 {
			problems = append(problems, SmartProblem{Severity: SmartWarning,
				Short: fmt.Sprintf("%d media err", mediaErrors), Text: fmt.Sprintf("%d media errors", mediaErrors)})
		}
		if log.PercentageUsed >= WornWarningPercent {
			problems = append(problems, SmartProblem{Severity: SmartWarning,
				Short: fmt.Sprintf("%d%% worn", log.PercentageUsed), Text: fmt.Sprintf("%d%% of the rated endurance used", log.PercentageUsed)})
		}
	}
	if parsed.ScsiGrownDefectList != nil {
		defects := jsonCount(*parsed.ScsiGrownDefectList)
		report.Attributes = append(report.Attributes, SmartAttribute{Name: "Grown defects", Value: strconv.FormatInt(defects, 10), Raw: defects,
			Normalized: -1, Worst: -1, Threshold: -1, Important: true})
		if defects > 0 {
			problems = append(problems, SmartProblem{Severity: SmartWarning,
				Short: fmt.Sprintf("%d defects", defects), Text: fmt.Sprintf("%d grown defects", defects)})
		}
	}
	var uncorrected int64
	for _, counter := range parsed.ScsiErrorCounterLog {
		uncorrected += jsonCount(counter.TotalUncorrectedErrors)
	}
	if uncorrected > 0 {
		problems = append(problems, SmartProblem{Severity: SmartWarning,
			Short: fmt.Sprintf("%d uncorr", uncorrected), Text: fmt.Sprintf("%d uncorrected errors", uncorrected)})
	}

	// the most severe first, else in the order found
	slices.SortStableFunc(problems, func(a, b SmartProblem) int { return int(b.Severity) - int(a.Severity) })
	report.Problems = problems
	report.Counters = countersOf(&parsed)
	return report, false, nil
}

// SmartSource reads the SMART values of the devices of pools, see SmartReader.
type SmartSource interface {
	// ReadDevices returns the reports of the disks of each device (its name in "zpool status"), reading the ones
	// that were not read within maxAge. Devices that are not stored on disks have none.
	ReadDevices(devices []string, maxAge time.Duration) map[string][]SmartReport
	// Reset forgets all reports, e.g. once the user may run smartctl with sudo.
	Reset()
}

// SmartReader reads the SMART values of disks with smartctl, which needs root: with "sudo -n", allowed by the
// sudoers rule of "setup --smart". sudo logs every call, so the reports are kept: a disk is only read again once its
// report is older than asked for. Disks that sleep are not woken up (see SmartctlArgs), their last report is kept.
//
// It is safe for concurrent use, its methods may block (they run smartctl), so they must not be called on the UI
// thread.
type SmartReader struct {
	// now is replaceable in tests
	now func() time.Time

	// mu is held while reading, so a disk is not read twice at the same time
	mu      sync.Mutex
	reports map[string]SmartReport
}

var _ SmartSource = (*SmartReader)(nil)

func NewSmartReader() *SmartReader {
	return &SmartReader{now: time.Now, reports: map[string]SmartReport{}}
}

// ReadDevices see SmartSource.
func (reader *SmartReader) ReadDevices(devices []string, maxAge time.Duration) map[string][]SmartReport {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	result := make(map[string][]SmartReport, len(devices))
	for _, device := range devices {
		var reports []SmartReport
		for _, disk := range Resolve(device) {
			reports = append(reports, reader.read(disk, maxAge))
		}
		result[device] = reports
	}
	return result
}

// Reset see SmartSource.
func (reader *SmartReader) Reset() {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	reader.reports = map[string]SmartReport{}
}

// read returns the report of the disk, reading it if the last one is older than maxAge. Holds mu.
func (reader *SmartReader) read(disk string, maxAge time.Duration) SmartReport {
	now := reader.now()
	last, ok := reader.reports[disk]
	if ok && now.Sub(last.CheckedAt) < maxAge {
		return last
	}

	report := SmartReport{Disk: disk, CheckedAt: now, PowerOnHours: -1}
	if !SmartctlInstalled() {
		report.Err = ErrSmartctlMissing
		reader.reports[disk] = report
		return report
	}
	stdout, stderr, err := runSmartctl(filepath.Join("/dev", disk))
	switch {
	case len(bytes.TrimSpace(stdout)) == 0 && strings.HasPrefix(stderr, "sudo:"):
		// e.g. "sudo: a password is required"
		report.Err = ErrSmartNeedsRoot
	case len(bytes.TrimSpace(stdout)) == 0:
		if stderr == "" && err != nil {
			stderr = err.Error()
		}
		report.Err = fmt.Errorf("smartctl failed: %s", stderr)
	default:
		parsed, standby, parseErr := parseSmartctl(disk, stdout, now)
		switch {
		case parseErr != nil:
			report.Err = parseErr
		case standby:
			// the values from before it went to sleep, if any
			if last.HasValues() {
				report = last
			}
			report.CheckedAt, report.Standby, report.Err = now, true, nil
		default:
			report = parsed
		}
	}
	report.Rotational = isRotational(disk)
	reader.reports[disk] = report
	return report
}
