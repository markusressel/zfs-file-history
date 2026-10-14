package disk

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSmartCounters(t *testing.T) {
	tests := []struct {
		fixture  string
		expected SmartCounters
	}{
		// a hard disk: 241 counts blocks of 512 bytes, 192 the heads parked by power losses
		{"smartctl-ata-hot.json", SmartCounters{BadSectors: 0, Uncorrectable: 0, CrcErrors: 0, WearPercent: -1,
			BytesWritten: 85772543731 * 512, PowerCycles: 21, UnsafeShutdowns: 8, LoadCycles: 48284}},
		// 5, 197 and 198 together
		{"smartctl-ata.json", SmartCounters{BadSectors: 8, Uncorrectable: -1, CrcErrors: 3, WearPercent: -1,
			BytesWritten: -1, PowerCycles: -1, UnsafeShutdowns: -1, LoadCycles: -1}},
		{"smartctl-ata-failing.json", SmartCounters{BadSectors: 3912 + 16, Uncorrectable: 211, CrcErrors: -1, WearPercent: -1,
			BytesWritten: -1, PowerCycles: -1, UnsafeShutdowns: -1, LoadCycles: -1}},
		// data units of 512 000 bytes
		{"smartctl-nvme-healthy.json", SmartCounters{BadSectors: 0, Uncorrectable: -1, CrcErrors: -1, WearPercent: 4,
			BytesWritten: 198543345 * 512_000, PowerCycles: 684, UnsafeShutdowns: 98, LoadCycles: -1}},
		{"smartctl-scsi.json", SmartCounters{BadSectors: 12, Uncorrectable: 3, CrcErrors: -1, WearPercent: -1,
			BytesWritten: -1, PowerCycles: -1, UnsafeShutdowns: -1, LoadCycles: -1}},
	}
	for _, test := range tests {
		t.Run(test.fixture, func(t *testing.T) {
			report, _, err := parseSmartctl("sda", readFixture(t, test.fixture), smartTime)
			require.NoError(t, err)
			assert.Equal(t, test.expected, report.Counters)
		})
	}
}

func TestSmartCounters_AtaSsd(t *testing.T) {
	output := []byte(`{"smartctl": {"exit_status": 0}, "smart_status": {"passed": true}, "ata_smart_attributes": {"table": [
		{"id": 177, "name": "Wear_Leveling_Count", "value": 93, "raw": {"value": 120}},
		{"id": 174, "name": "Unexpect_Power_Loss_Ct", "value": 100, "raw": {"value": 5}},
		{"id": 241, "name": "Host_Writes_32MiB", "value": 100, "raw": {"value": 1000}}
	]}}`)
	report, _, err := parseSmartctl("sda", output, smartTime)
	require.NoError(t, err)
	assert.Equal(t, int64(7), report.Counters.WearPercent, "the normalized value is the endurance left")
	assert.Equal(t, int64(5), report.Counters.UnsafeShutdowns)
	assert.Equal(t, int64(1000*32<<20), report.Counters.BytesWritten)
}

func TestBytesWritten(t *testing.T) {
	tests := []struct {
		name      string
		blockSize int64
		expected  int64
		ok        bool
	}{
		{"Total_LBAs_Written", 4096, 10 * 4096, true},
		{"Total_LBAs_Written", 0, 10 * 512, true},
		{"Host_Writes_GiB", 512, 10 << 30, true},
		// a unit that is not known: nothing rather than something wrong
		{"Unknown_Attribute", 512, 0, false},
	}
	for _, test := range tests {
		written, ok := bytesWritten(test.name, 10, test.blockSize)
		assert.Equal(t, test.ok, ok, test.name)
		assert.Equal(t, test.expected, written, test.name)
	}
	written, _ := bytesWritten("Host_Writes_GiB", 1<<62, 0)
	assert.Equal(t, int64(1<<63-1), written, "capped")
}
