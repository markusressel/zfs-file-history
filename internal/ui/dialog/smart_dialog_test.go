package dialog

import (
	"errors"
	"strings"
	"testing"
	"time"
	"zfs-file-history/internal/disk"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/util"

	"github.com/stretchr/testify/assert"
)

var smartReadAt = time.Date(2026, 10, 14, 12, 0, 0, 0, time.Local)

func TestFormatSmartDevices(t *testing.T) {
	devices := []SmartDevice{
		{Name: "tank.img"},
		{Name: "/dev/disk/by-id/ata-DISK_1-part1", Reports: []disk.SmartReport{{
			Disk: "sda", ReadAt: smartReadAt, CheckedAt: smartReadAt, Model: "WDC WD40EFRX", Serial: "WD-1", Firmware: "82.00A82", Capacity: 4000787030016, Protocol: "ATA",
			HealthKnown: true, Passed: true, PowerOnHours: 8760,
			Problems: []disk.SmartProblem{{Severity: disk.SmartWarning, Short: "8 realloc", Text: "8 reallocated sectors"}},
			Attributes: []disk.SmartAttribute{
				{Id: 5, Name: "Reallocated_Sector_Ct", Value: "8", Normalized: 199, Worst: 199, Threshold: 140, Important: true},
				{Id: 9, Name: "Power_On_Hours", Value: "8760", Normalized: 52, Worst: 52, Threshold: 0},
			},
		}}},
		{Name: "sdb", Reports: []disk.SmartReport{{Disk: "sdb", Standby: true, CheckedAt: smartReadAt}}},
		{Name: "sdc", Reports: []disk.SmartReport{{Disk: "sdc", Err: disk.ErrSmartNeedsRoot}}},
		{Name: "sdd", Reports: []disk.SmartReport{{Disk: "sdd", Err: errors.New("smartctl failed: no")}}},
	}
	text, lines := formatSmartDevices(devices)
	plain := testutil.StripTags(text)
	assert.Equal(t, map[string]int{"/dev/disk/by-id/ata-DISK_1-part1": 0, "sdb": 9, "sdc": 12, "sdd": 15}, lines)
	assert.Equal(t, []string{
		"ata-DISK_1-part1 → sda  WDC WD40EFRX  WD-1  4.0 TB  firmware 82.00A82  ATA",
		"  Health:   PASSED",
		"  Power on: 8760 h (1.0 years)",
		"  Read:     " + util.FormatTime(smartReadAt),
		"  Problems: 8 reallocated sectors",
		"   ID  ATTRIBUTE               NORM WORST THRESH  RAW",
		"    5  Reallocated_Sector_Ct    199   199    140  8",
		"    9  Power_On_Hours            52    52      0  8760",
	}, strings.Split(plain, "\n")[:8])
	assert.Contains(t, plain, "sdb\n  State:    asleep, not woken up to read it")
	assert.Contains(t, plain, "sdc\n  Error:    only root may read them: allow \"Disk health\"")
	assert.Contains(t, plain, "sdd\n  Error:    smartctl failed: no")
	assert.NotContains(t, plain, "tank.img", "not stored on a disk")

	text, _ = formatSmartDevices(nil)
	assert.Equal(t, "No device of the pool is stored on a disk.\n", testutil.StripTags(text))
}

func TestFormatSmartDevices_Nvme(t *testing.T) {
	text, _ := formatSmartDevices([]SmartDevice{{Name: "nvme0n1", Reports: []disk.SmartReport{{
		Disk: "nvme0n1", ReadAt: smartReadAt, HealthKnown: true, Passed: false, PowerOnHours: -1,
		Attributes: []disk.SmartAttribute{{Name: "Percentage used", Value: "93%", Normalized: -1, Worst: -1, Threshold: -1, Important: true}},
	}}}})
	plain := testutil.StripTags(text)
	assert.Contains(t, plain, "  Health:   FAILED")
	assert.NotContains(t, plain, "Power on")
	assert.Contains(t, plain, "  ATTRIBUTE        VALUE\n  Percentage used  93%")
}
