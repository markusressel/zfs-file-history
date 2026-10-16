package disk

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
	"zfs-file-history/internal/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readFixture returns a file of testdata: outputs of "smartctl -j -H -A -n standby -- <device>" (see SmartctlArgs).
// smartctl-permission-denied.json (without root), smartctl-nvme-healthy.json (without -i) and smartctl-ata-hot.json
// (serial number and WWN replaced) are from real systems (smartctl 7.5),
// the others follow the format of smartctl 7.5 with made up values and serial numbers (smartctl-standby.json the
// output of the -n option in ataprint.cpp).
func readFixture(t *testing.T, name string) []byte {
	content, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return content
}

var smartTime = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

func TestParseSmartctl_Ata(t *testing.T) {
	report, standby, err := parseSmartctl("sda", readFixture(t, "smartctl-ata.json"), smartTime)
	require.NoError(t, err)
	assert.False(t, standby)
	assert.Equal(t, "sda", report.Disk)
	assert.Equal(t, smartTime, report.ReadAt)
	assert.Equal(t, "WDC WD40EFRX-68N32N0", report.Model)
	assert.Equal(t, "WD-SERIAL0001", report.Serial)
	assert.Equal(t, "82.00A82", report.Firmware)
	assert.Equal(t, int64(4000787030016), report.Capacity)
	assert.Equal(t, "ATA", report.Protocol)
	assert.True(t, report.HealthKnown)
	assert.True(t, report.Passed)
	assert.Equal(t, int64(35123), report.PowerOnHours)
	// CRC errors (199) are not a problem: they count errors of the cable
	assert.Equal(t, []SmartProblem{{Severity: SmartWarning, Short: "8 realloc", Text: "8 reallocated sectors"}}, report.Problems)

	require.Len(t, report.Attributes, 7)
	assert.Equal(t, SmartAttribute{Id: 5, Name: "Reallocated_Sector_Ct", Value: "8", Raw: 8, Normalized: 199, Worst: 199, Threshold: 140, Important: true,
		// from 200 down to 140
		Used: 1.0 / 60, UsedKnown: true}, report.Attributes[1])
	assert.Equal(t, "33 (Min/Max 17/47)", report.Attributes[3].Value)
	assert.False(t, report.Attributes[3].Important)
}

func TestParseSmartctl_AtaFailing(t *testing.T) {
	report, _, err := parseSmartctl("sdb", readFixture(t, "smartctl-ata-failing.json"), smartTime)
	require.NoError(t, err)
	assert.False(t, report.Passed)
	assert.Equal(t, []SmartProblem{
		{Severity: SmartError, Short: "FAILED", Text: "the health check of the disk failed"},
		{Severity: SmartError, Short: "failing", Text: "Reallocated_Sector_Ct is at or below its threshold: the disk may fail soon"},
		{Severity: SmartWarning, Short: "3912 realloc", Text: "3912 reallocated sectors"},
		{Severity: SmartWarning, Short: "211 uncorr", Text: "211 reported uncorrectable errors"},
		{Severity: SmartWarning, Short: "16 pending", Text: "16 pending sectors"},
	}, report.Problems)
	assert.Equal(t, "now", report.Attributes[0].Failed)
}

// Regression: a Seagate IronWolf Pro too hot during a scrub was shown as failing (Error), its health check passed.
func TestParseSmartctl_AtaTooHot(t *testing.T) {
	report, _, err := parseSmartctl("sdd", readFixture(t, "smartctl-ata-hot.json"), smartTime)
	require.NoError(t, err)
	assert.Equal(t, "ST14000NE0008-2RX103", report.Model)
	assert.Equal(t, "EN02", report.Firmware)
	assert.Equal(t, int64(14000519643136), report.Capacity)
	assert.True(t, report.Passed)
	assert.Equal(t, []SmartProblem{{Severity: SmartWarning, Short: "too hot",
		Text: "Airflow_Temperature_Cel is past its threshold: the disk is too hot"}}, report.Problems)
}

func TestFailingAttributeProblem(t *testing.T) {
	assert.Equal(t, SmartError, failingAttributeProblem(5, "Reallocated_Sector_Ct", true).Severity)
	assert.Equal(t, "too hot", failingAttributeProblem(194, "Temperature_Celsius", false).Short)
	assert.Equal(t, SmartProblem{Severity: SmartWarning, Short: "worn", Text: "Load_Cycle_Count is at or below its threshold (old age)"},
		failingAttributeProblem(193, "Load_Cycle_Count", false))
}

func TestParseSmartctl_Nvme(t *testing.T) {
	report, _, err := parseSmartctl("nvme0n1", readFixture(t, "smartctl-nvme.json"), smartTime)
	require.NoError(t, err)
	assert.Equal(t, "NVMe", report.Protocol)
	assert.True(t, report.Passed)
	assert.Equal(t, int64(24321), report.PowerOnHours)
	assert.Equal(t, []SmartProblem{
		{Severity: SmartWarning, Short: "2 media err", Text: "2 media errors"},
		{Severity: SmartWarning, Short: "93% worn", Text: "93% of the rated endurance used"},
	}, report.Problems)

	values := map[string]string{}
	for _, attribute := range report.Attributes {
		values[attribute.Name] = attribute.Value
	}
	assert.Equal(t, "0x00", values["Critical warning"])
	assert.Equal(t, "100% (threshold 10%)", values["Available spare"])
	used := usedOf(report)
	assert.Equal(t, map[string]float64{"Available spare": 0, "Percentage used": 0.93}, used, "only the ones with a limit")
	// a counter of 128 bits
	assert.Equal(t, "9223372036854775807", values["Error log entries"])
}

func TestParseSmartctl_NvmeHealthy(t *testing.T) {
	report, standby, err := parseSmartctl("nvme0n1", readFixture(t, "smartctl-nvme-healthy.json"), smartTime)
	require.NoError(t, err)
	assert.False(t, standby)
	assert.Equal(t, "NVMe", report.Protocol)
	assert.True(t, report.HealthKnown)
	assert.True(t, report.Passed)
	assert.Equal(t, int64(19773), report.PowerOnHours)
	// error log entries are not a problem: they count all errors the drive logged, e.g. of unsupported commands
	assert.Empty(t, report.Problems)
	assert.Empty(t, report.Model, "only with -i")

	values := map[string]string{}
	for _, attribute := range report.Attributes {
		values[attribute.Name] = attribute.Value
	}
	assert.Equal(t, map[string]string{
		"Critical warning":   "0x00",
		"Available spare":    "100% (threshold 10%)",
		"Percentage used":    "4%",
		"Media errors":       "0",
		"Error log entries":  "1876",
		"Unsafe shutdowns":   "98",
		"Data units written": "198543345",
		"Power cycles":       "684",
	}, values)
}

// usedOf returns how far the attributes of the report went toward failing, the ones it is known of.
func usedOf(report SmartReport) map[string]float64 {
	used := map[string]float64{}
	for _, attribute := range report.Attributes {
		if attribute.UsedKnown {
			used[attribute.Name] = attribute.Used
		}
	}
	return used
}

func TestAtaUsed(t *testing.T) {
	tests := []struct {
		name                         string
		normalized, worst, threshold int
		used                         float64
		known                        bool
	}{
		{"new, from 100", 100, 100, 10, 0, true},
		{"half way, from 100", 55, 55, 10, 0.5, true},
		{"from 200", 170, 170, 140, 0.5, true},
		{"the worst value tells the start", 90, 150, 50, 110.0 / 150, true},
		{"from 253", 253, 253, 21, 0, true},
		{"at the threshold", 36, 36, 36, 1, true},
		{"below the threshold", 30, 30, 36, 1, true},
		{"cannot fail", 100, 100, 0, 0, false},
		{"no normalized value", -1, -1, -1, 0, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			used, known := ataUsed(test.normalized, test.worst, test.threshold)
			assert.Equal(t, test.known, known)
			assert.InDelta(t, test.used, used, 1e-9)
		})
	}
}

func TestParseSmartctl_NvmeCritical(t *testing.T) {
	report, _, err := parseSmartctl("nvme1n1", readFixture(t, "smartctl-nvme-critical.json"), smartTime)
	require.NoError(t, err)
	assert.Equal(t, int64(100), report.PowerOnHours, "from the health log")
	assert.Equal(t, []SmartProblem{
		{Severity: SmartError, Short: "FAILED", Text: "the health check of the disk failed"},
		{Severity: SmartError, Short: "critical", Text: "critical warning: available spare below threshold, reliability degraded"},
		{Severity: SmartWarning, Short: "100% worn", Text: "100% of the rated endurance used"},
	}, report.Problems)
	// the spare is below its threshold: all used
	assert.Equal(t, map[string]float64{"Available spare": 1, "Percentage used": 1}, usedOf(report))
}

func TestParseSmartctl_Scsi(t *testing.T) {
	report, _, err := parseSmartctl("sdc", readFixture(t, "smartctl-scsi.json"), smartTime)
	require.NoError(t, err)
	assert.Equal(t, "ST8000NM0075", report.Model)
	assert.Equal(t, []SmartProblem{
		{Severity: SmartWarning, Short: "12 defects", Text: "12 grown defects"},
		{Severity: SmartWarning, Short: "3 uncorr", Text: "3 uncorrected errors"},
	}, report.Problems)
}

func TestParseSmartctl_NotRead(t *testing.T) {
	_, standby, err := parseSmartctl("sda", readFixture(t, "smartctl-standby.json"), smartTime)
	require.NoError(t, err)
	assert.True(t, standby)

	// by the power mode alone
	_, standby, err = parseSmartctl("sda", []byte(`{"smartctl": {"exit_status": 2}, "power_mode": {"ata_value": 0, "name": "SLEEP"}}`), smartTime)
	require.NoError(t, err)
	assert.True(t, standby)

	_, standby, err = parseSmartctl("nvme0n1", readFixture(t, "smartctl-permission-denied.json"), smartTime)
	assert.False(t, standby)
	assert.EqualError(t, err, "Smartctl open device: /dev/nvme0n1 failed: Permission denied")

	_, _, err = parseSmartctl("sda", []byte("not json"), smartTime)
	assert.ErrorContains(t, err, "unexpected output of smartctl")
}

// fakeSmartctl replaces runSmartctl with outputs by device, and records the devices it ran for.
type fakeSmartctl struct {
	mu      sync.Mutex
	outputs map[string][]byte
	stderr  string
	runs    []string
}

func newFakeSmartctl(t *testing.T) *fakeSmartctl {
	fake := &fakeSmartctl{outputs: map[string][]byte{}}
	testutil.Replace(t, &runSmartctl, fake.run)
	testutil.Replace(t, &SmartctlInstalled, func() bool { return true })
	return fake
}

func (fake *fakeSmartctl) run(device string) ([]byte, string, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.runs = append(fake.runs, device)
	if output, ok := fake.outputs[device]; ok {
		return output, "", nil
	}
	return nil, fake.stderr, errors.New("exit status 1")
}

func (fake *fakeSmartctl) set(device string, output []byte) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.outputs[device] = output
}

func (fake *fakeSmartctl) getRuns() []string {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return append([]string{}, fake.runs...)
}

// newTestSmartReader returns a reader whose time is set by the returned function.
func newTestSmartReader() (*SmartReader, func(time.Time)) {
	reader := NewSmartReader()
	current := smartTime
	reader.now = func() time.Time { return current }
	return reader, func(t time.Time) { current = t }
}

func TestSmartReader_ReadDevices(t *testing.T) {
	system := newFakeSystem(t)
	system.addDisk("sda", true, "sda1")
	system.addDisk("nvme0n1", false)
	fake := newFakeSmartctl(t)
	fake.set("/dev/sda", readFixture(t, "smartctl-ata.json"))
	fake.set("/dev/nvme0n1", readFixture(t, "smartctl-nvme.json"))
	reader, setNow := newTestSmartReader()

	reports := reader.ReadDevices([]string{"sda1", "nvme0n1", "tank.img"}, time.Hour)
	require.Len(t, reports["sda1"], 1)
	assert.Equal(t, "sda", reports["sda1"][0].Disk)
	assert.Equal(t, "8 realloc", reports["sda1"][0].Problems[0].Short)
	assert.Equal(t, "Samsung SSD 970 EVO Plus 1TB", reports["nvme0n1"][0].Model)
	assert.True(t, reports["sda1"][0].Rotational, "from sysfs")
	assert.False(t, reports["nvme0n1"][0].Rotational)
	assert.Empty(t, reports["tank.img"], "not stored on a disk")
	assert.Equal(t, []string{"/dev/sda", "/dev/nvme0n1"}, fake.getRuns())

	// kept: sudo logs every call
	setNow(smartTime.Add(59 * time.Minute))
	reader.ReadDevices([]string{"sda1", "nvme0n1"}, time.Hour)
	assert.Len(t, fake.getRuns(), 2)
	// read again once older than asked for
	reader.ReadDevices([]string{"sda1"}, 10*time.Minute)
	assert.Equal(t, []string{"/dev/sda", "/dev/nvme0n1", "/dev/sda"}, fake.getRuns())
	// or asked to
	reader.ReadDevices([]string{"nvme0n1"}, 0)
	assert.Len(t, fake.getRuns(), 4)
}

func TestSmartReader_Standby(t *testing.T) {
	system := newFakeSystem(t)
	system.addDisk("sda", true)
	fake := newFakeSmartctl(t)
	reader, setNow := newTestSmartReader()

	// asleep before it was ever read
	fake.set("/dev/sda", readFixture(t, "smartctl-standby.json"))
	report := reader.ReadDevices([]string{"sda"}, 0)["sda"][0]
	assert.True(t, report.Standby)
	assert.False(t, report.HasValues())
	assert.NoError(t, report.Err)

	fake.set("/dev/sda", readFixture(t, "smartctl-ata.json"))
	setNow(smartTime.Add(time.Hour))
	report = reader.ReadDevices([]string{"sda"}, 0)["sda"][0]
	assert.False(t, report.Standby)
	assert.Equal(t, smartTime.Add(time.Hour), report.ReadAt)

	// asleep again: the values from before are kept
	fake.set("/dev/sda", readFixture(t, "smartctl-standby.json"))
	setNow(smartTime.Add(2 * time.Hour))
	report = reader.ReadDevices([]string{"sda"}, 0)["sda"][0]
	assert.True(t, report.Standby)
	assert.Equal(t, smartTime.Add(time.Hour), report.ReadAt)
	assert.Equal(t, smartTime.Add(2*time.Hour), report.CheckedAt)
	assert.Equal(t, "8 realloc", report.Problems[0].Short)
}

func TestSmartReader_Errors(t *testing.T) {
	system := newFakeSystem(t)
	system.addDisk("sda", true)
	fake := newFakeSmartctl(t)
	reader, _ := newTestSmartReader()

	fake.stderr = "sudo: a password is required"
	assert.ErrorIs(t, reader.ReadDevices([]string{"sda"}, 0)["sda"][0].Err, ErrSmartNeedsRoot)

	fake.stderr = "sudo: smartctl: command not found"
	assert.ErrorIs(t, reader.ReadDevices([]string{"sda"}, 0)["sda"][0].Err, ErrSmartNeedsRoot)

	fake.set("/dev/sda", readFixture(t, "smartctl-permission-denied.json"))
	assert.EqualError(t, reader.ReadDevices([]string{"sda"}, 0)["sda"][0].Err, "Smartctl open device: /dev/nvme0n1 failed: Permission denied")

	// e.g. once allowed: read again, even if asked for old ones
	fake.set("/dev/sda", readFixture(t, "smartctl-ata.json"))
	assert.Error(t, reader.ReadDevices([]string{"sda"}, time.Hour)["sda"][0].Err)
	reader.Reset()
	assert.NoError(t, reader.ReadDevices([]string{"sda"}, time.Hour)["sda"][0].Err)

	testutil.Replace(t, &SmartctlInstalled, func() bool { return false })
	assert.ErrorIs(t, reader.ReadDevices([]string{"sda"}, 0)["sda"][0].Err, ErrSmartctlMissing)
}
