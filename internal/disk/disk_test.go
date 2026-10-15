package disk

import (
	"os"
	"path/filepath"
	"testing"
	"time"
	"zfs-file-history/internal/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSystem is a /dev and /sys in a temporary folder, replacing devDir and sysDir.
type fakeSystem struct {
	t   *testing.T
	dev string
	sys string
}

func newFakeSystem(t *testing.T) *fakeSystem {
	root := t.TempDir()
	system := &fakeSystem{t: t, dev: filepath.Join(root, "dev"), sys: filepath.Join(root, "sys")}
	testutil.Replace(t, &devDir, system.dev)
	testutil.Replace(t, &sysDir, system.sys)
	return system
}

func (system *fakeSystem) write(path string, content string) {
	require.NoError(system.t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(system.t, os.WriteFile(path, []byte(content), 0o644))
}

func (system *fakeSystem) link(target string, path string) {
	require.NoError(system.t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(system.t, os.Symlink(target, path))
}

// addDisk adds a disk with partitions (e.g. "sda1"), like the kernel: its folder below devices, linked from
// class/block, the ones of the partitions within it, and its device file, also linked from disk/by-id.
func (system *fakeSystem) addDisk(name string, rotational bool, partitions ...string) {
	dir := filepath.Join(system.sys, "devices/fake", name)
	rotationalValue := "0\n"
	if rotational {
		rotationalValue = "1\n"
	}
	system.write(filepath.Join(dir, "queue/rotational"), rotationalValue)
	system.write(filepath.Join(dir, "stat"), "1 0 8 0 0 0 0 0 0 0 0\n")
	require.NoError(system.t, os.MkdirAll(filepath.Join(dir, "device"), 0o755))
	system.link(dir, filepath.Join(system.sys, "class/block", name))
	system.write(filepath.Join(system.dev, name), "")
	system.link("../../"+name, filepath.Join(system.dev, "disk/by-id", "ata-"+name))
	for _, partition := range partitions {
		system.write(filepath.Join(dir, partition, "partition"), "1\n")
		system.link(filepath.Join(dir, partition), filepath.Join(system.sys, "class/block", partition))
		system.write(filepath.Join(system.dev, partition), "")
		system.link("../../"+partition, filepath.Join(system.dev, "disk/by-id", "ata-"+name+"-"+partition))
	}
}

// addSensor adds a temperature sensor (millidegrees) to the disk, like drivetemp (SATA) or NVMe do.
func (system *fakeSystem) addSensor(disk string, nvme bool, milliCelsius string, maxMilliCelsius string) {
	dir := filepath.Join(system.sys, "class/block", disk, "device/hwmon/hwmon3")
	if nvme {
		dir = filepath.Join(system.sys, "class/block", disk, "device/hwmon7")
	}
	system.write(filepath.Join(dir, "temp1_input"), milliCelsius+"\n")
	if maxMilliCelsius != "" {
		system.write(filepath.Join(dir, "temp1_max"), maxMilliCelsius+"\n")
	}
}

// setActivity changes the I/O statistics of the disk, as if it read something.
func (system *fakeSystem) setActivity(disk string, stat string) {
	system.write(filepath.Join(system.sys, "class/block", disk, "stat"), stat+"\n")
}

func TestResolve(t *testing.T) {
	system := newFakeSystem(t)
	system.addDisk("sda", true, "sda1")
	system.addDisk("sdb", true)
	system.addDisk("sdc", true)
	// an encrypted device on sdb, a device mapper device on top of it and sdc
	system.write(filepath.Join(system.sys, "devices/virtual/dm-0/slaves/sdb"), "")
	system.link(filepath.Join(system.sys, "devices/virtual/dm-0"), filepath.Join(system.sys, "class/block/dm-0"))
	system.write(filepath.Join(system.sys, "devices/virtual/dm-1/slaves/dm-0"), "")
	system.write(filepath.Join(system.sys, "devices/virtual/dm-1/slaves/sdc"), "")
	system.link(filepath.Join(system.sys, "devices/virtual/dm-1"), filepath.Join(system.sys, "class/block/dm-1"))
	system.write(filepath.Join(system.dev, "dm-1"), "")
	system.link("../dm-1", filepath.Join(system.dev, "mapper/crypt-tank"))
	// a file vdev
	system.write(filepath.Join(system.dev, "..", "tank.img"), "")

	tests := []struct {
		name     string
		expected []string
	}{
		{"sda", []string{"sda"}},
		{"sda1", []string{"sda"}},
		{"ata-sda-sda1", []string{"sda"}},
		{"ata-sdb", []string{"sdb"}},
		{filepath.Join(system.dev, "sda1"), []string{"sda"}},
		{"crypt-tank", []string{"sdb", "sdc"}},
		{filepath.Join(system.dev, "..", "tank.img"), nil},
		// a missing device, shown by its guid
		{"1234567890", nil},
		{"mirror-0", nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.ElementsMatch(t, test.expected, Resolve(test.name))
		})
	}
}

func TestReadTemperature(t *testing.T) {
	system := newFakeSystem(t)
	system.addDisk("sda", true)
	system.addDisk("nvme0n1", false)
	system.addDisk("sdb", true)
	system.addSensor("sda", false, "31000", "60000")
	system.addSensor("nvme0n1", true, "49850", "")
	// placeholders of a sensor without a value
	system.addSensor("sdb", false, "-273150", "65261850")

	temperature, ok := ReadTemperature("sda")
	assert.True(t, ok)
	assert.Equal(t, Temperature{Celsius: 31, Warning: 60}, temperature)

	temperature, ok = ReadTemperature("nvme0n1")
	assert.True(t, ok)
	assert.InDelta(t, 49.85, temperature.Celsius, 0.001)
	assert.Zero(t, temperature.Warning)

	_, ok = ReadTemperature("sdb")
	assert.False(t, ok)
	_, ok = ReadTemperature("sdx")
	assert.False(t, ok)
}

func TestNeedsDrivetemp(t *testing.T) {
	system := newFakeSystem(t)
	system.addDisk("sda", true)
	system.addDisk("nvme0n1", false)
	require.NoError(t, os.MkdirAll(filepath.Join(system.sys, "class/block/sda/device/scsi_disk"), 0o755))

	assert.True(t, NeedsDrivetemp("sda"))
	assert.False(t, NeedsDrivetemp("nvme0n1"))

	require.NoError(t, os.MkdirAll(filepath.Join(system.sys, "module/drivetemp"), 0o755))
	assert.False(t, NeedsDrivetemp("sda"))
}

func TestIsBusy(t *testing.T) {
	assert.False(t, IsBusy("88 0 16952 361 0 0 0 0 0 259 361"))
	assert.True(t, IsBusy("88 0 16952 361 0 0 0 0 2 259 361"))
	assert.False(t, IsBusy(""))
}

// newTestSampler returns a sampler whose time is set by the returned function.
func newTestSampler() (*Sampler, func(time.Time)) {
	sampler := NewSampler()
	current := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	sampler.now = func() time.Time { return current }
	return sampler, func(t time.Time) { current = t }
}

func TestSampler_ReadsDrivesThatDoNotSpin(t *testing.T) {
	system := newFakeSystem(t)
	system.addDisk("nvme0n1", false, "nvme0n1p1")
	system.addSensor("nvme0n1", true, "45000", "80000")
	sampler, setNow := newTestSampler()
	start := sampler.now()

	reading := sampler.Sample([]string{"nvme0n1p1"}, nil)["nvme0n1p1"]
	assert.Equal(t, Reading{IsDisk: true, Known: true, ReadAt: start, Temperature: Temperature{Celsius: 45, Warning: 80}}, reading)

	// not read again before TemperatureInterval
	system.addSensor("nvme0n1", true, "50000", "80000")
	setNow(start.Add(TemperatureInterval / 2))
	assert.Equal(t, 45.0, sampler.Sample([]string{"nvme0n1p1"}, nil)["nvme0n1p1"].Temperature.Celsius)
	setNow(start.Add(TemperatureInterval))
	assert.Equal(t, 50.0, sampler.Sample([]string{"nvme0n1p1"}, nil)["nvme0n1p1"].Temperature.Celsius)
}

func TestSampler_ReadsHardDisksOnlyWhileBusy(t *testing.T) {
	system := newFakeSystem(t)
	system.addDisk("sda", true, "sda1")
	system.addSensor("sda", false, "35000", "60000")
	sampler, setNow := newTestSampler()
	start := sampler.now()
	devices := []string{"sda1"}

	// nothing to compare the I/O statistics with yet
	assert.Equal(t, Reading{IsDisk: true, Idle: true}, sampler.Sample(devices, nil)["sda1"])

	// no I/O in the meantime: it may sleep
	setNow(start.Add(10 * time.Second))
	assert.Equal(t, Reading{IsDisk: true, Idle: true}, sampler.Sample(devices, nil)["sda1"])

	// I/O in the meantime, e.g. a scrub
	system.setActivity("sda", "2 0 16 0 0 0 0 0 0 1 1")
	setNow(start.Add(20 * time.Second))
	reading := sampler.Sample(devices, nil)["sda1"]
	assert.Equal(t, Reading{IsDisk: true, Known: true, ReadAt: start.Add(20 * time.Second), Temperature: Temperature{Celsius: 35, Warning: 60}}, reading)

	// idle again: the last temperature is kept
	setNow(start.Add(30 * time.Second))
	reading = sampler.Sample(devices, nil)["sda1"]
	assert.True(t, reading.Idle)
	assert.True(t, reading.Known)
	assert.Equal(t, 35.0, reading.Temperature.Celsius)

	// statistics that changed long ago do not count, the disk may sleep by now
	system.setActivity("sda", "3 0 24 0 0 0 0 0 0 1 1")
	setNow(start.Add(30*time.Second + activityWindow + time.Second))
	assert.True(t, sampler.Sample(devices, nil)["sda1"].Idle)

	// requests in flight
	system.setActivity("sda", "3 0 24 0 0 0 0 0 1 1 1")
	system.addSensor("sda", false, "40000", "60000")
	setNow(start.Add(20*time.Second + 2*TemperatureInterval))
	reading = sampler.Sample(devices, nil)["sda1"]
	assert.False(t, reading.Idle)
	assert.Equal(t, 40.0, reading.Temperature.Celsius)
}

func TestSampler_Forced(t *testing.T) {
	system := newFakeSystem(t)
	system.addDisk("sda", true)
	system.addDisk("sdb", true)
	system.addSensor("sda", false, "35000", "")
	system.addSensor("sdb", false, "36000", "")
	sampler, _ := newTestSampler()

	readings := sampler.Sample([]string{"sda", "sdb"}, []string{"sda"})
	assert.True(t, readings["sda"].Known)
	assert.False(t, readings["sda"].Idle)
	assert.Equal(t, Reading{IsDisk: true, Idle: true}, readings["sdb"])

	// forced again right away: read again
	system.addSensor("sda", false, "37000", "")
	assert.Equal(t, 37.0, sampler.Sample([]string{"sda"}, []string{"sda"})["sda"].Temperature.Celsius)
}

func TestSampler_WithoutSensor(t *testing.T) {
	system := newFakeSystem(t)
	system.addDisk("sda", true)
	require.NoError(t, os.MkdirAll(filepath.Join(system.sys, "class/block/sda/device/scsi_disk"), 0o755))
	sampler, _ := newTestSampler()

	readings := sampler.Sample([]string{"sda", "tank.img"}, nil)
	assert.Equal(t, Reading{IsDisk: true, NoSensor: true, NeedsDrivetemp: true}, readings["sda"])
	assert.Equal(t, Reading{}, readings["tank.img"])
}

// TestSampler_HottestDisk checks a device stored on several disks (e.g. an encrypted one on an md device).
func TestSampler_HottestDisk(t *testing.T) {
	system := newFakeSystem(t)
	system.addDisk("nvme0n1", false)
	system.addDisk("nvme1n1", false)
	system.addSensor("nvme0n1", true, "45000", "")
	system.addSensor("nvme1n1", true, "52000", "")
	system.write(filepath.Join(system.sys, "devices/virtual/md0/slaves/nvme0n1"), "")
	system.write(filepath.Join(system.sys, "devices/virtual/md0/slaves/nvme1n1"), "")
	system.link(filepath.Join(system.sys, "devices/virtual/md0"), filepath.Join(system.sys, "class/block/md0"))
	system.write(filepath.Join(system.dev, "md0"), "")
	sampler, _ := newTestSampler()

	assert.Equal(t, 52.0, sampler.Sample([]string{"md0"}, nil)["md0"].Temperature.Celsius)
}

func TestReading_IsCritical(t *testing.T) {
	assert.True(t, Reading{IsDisk: true, Known: true, Temperature: Temperature{Celsius: 85, Critical: 85}}.IsCritical())
	assert.True(t, Reading{IsDisk: true, Known: true, Temperature: Temperature{Celsius: 86, Critical: 85}}.IsCritical())
	assert.False(t, Reading{IsDisk: true, Known: true, Temperature: Temperature{Celsius: 84, Critical: 85}}.IsCritical())
	assert.False(t, Reading{IsDisk: true, Known: true, Temperature: Temperature{Celsius: 86}}.IsCritical(), "critical unknown")
	assert.False(t, Reading{IsDisk: true, Known: true, Idle: true, Temperature: Temperature{Celsius: 86, Critical: 85}}.IsCritical(), "outdated")
	assert.False(t, Reading{IsDisk: true, Idle: true}.IsCritical())
}
