// Package disk reads the health of the disks of the pools from sysfs: their temperature (hwmon) and whether they
// are busy (their I/O statistics). Nothing here needs root or runs a process.
//
// Reading the temperature of a hard disk may reset its spin down timer (see the kernel documentation of
// drivetemp), so hard disks are only read while they are busy anyway (see Sampler) or when asked to. Reading the
// I/O statistics never touches the disk.
package disk

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// devDir and sysDir are the roots of the device files and of sysfs, replaceable in tests.
var (
	devDir = "/dev"
	sysDir = "/sys"
)

// searchDirs are the folders below devDir in which the names of the devices of "zpool status" are looked up, in the
// order zpool searches them on import (see zpool-import(8)), then devDir itself.
var searchDirs = []string{
	"disk/by-vdev",
	"mapper",
	"disk/by-partlabel",
	"disk/by-partuuid",
	"disk/by-label",
	"disk/by-uuid",
	"disk/by-id",
	"disk/by-path",
	"",
}

// maxStackDepth limits how many layers of device mapper or md devices are followed down to the disks, e.g. LVM on
// top of LUKS.
const maxStackDepth = 4

// Resolve returns the kernel names of the disks (e.g. "sda", "nvme0n1") a device of "zpool status" is stored on: the
// disk of a partition, the disks below an encrypted or md device. Empty if it is not a block device (e.g. a file, or
// a missing device shown by its guid).
func Resolve(name string) []string {
	path := ""
	if filepath.IsAbs(name) {
		path = name
	} else {
		for _, dir := range searchDirs {
			candidate := filepath.Join(devDir, dir, name)
			if _, err := os.Stat(candidate); err == nil {
				path = candidate
				break
			}
		}
	}
	if path == "" {
		return nil
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil
	}
	return disksOf(filepath.Base(real), 0)
}

// disksOf returns the disks below the block device with the kernel name: itself if it is a disk, the disk of a
// partition, the disks of the devices below a device mapper or md device.
func disksOf(name string, depth int) []string {
	dir := filepath.Join(sysDir, "class/block", name)
	if depth > maxStackDepth || !exists(dir) {
		return nil
	}
	if exists(filepath.Join(dir, "partition")) {
		// the folder of a partition is within the one of its disk
		real, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return nil
		}
		return disksOf(filepath.Base(filepath.Dir(real)), depth+1)
	}
	slaves, _ := os.ReadDir(filepath.Join(dir, "slaves"))
	if len(slaves) == 0 {
		return []string{name}
	}
	var disks []string
	for _, slave := range slaves {
		for _, disk := range disksOf(slave.Name(), depth+1) {
			if !slices.Contains(disks, disk) {
				disks = append(disks, disk)
			}
		}
	}
	return disks
}

// IsRotational returns whether the disk spins (a hard disk), so it may spin down. Unknown ones are assumed to.
func IsRotational(disk string) bool {
	value, err := os.ReadFile(filepath.Join(sysDir, "class/block", disk, "queue/rotational"))
	return err != nil || strings.TrimSpace(string(value)) != "0"
}

// Activity returns the I/O statistics of the disk, which change whenever it reads or writes, "" if unknown. See
// the kernel documentation of /sys/block/<disk>/stat.
func Activity(disk string) string {
	value, err := os.ReadFile(filepath.Join(sysDir, "class/block", disk, "stat"))
	if err != nil {
		return ""
	}
	return strings.Join(strings.Fields(string(value)), " ")
}

// IsBusy returns whether there are requests of the disk in flight, by its Activity.
func IsBusy(activity string) bool {
	fields := strings.Fields(activity)
	// the 9th field is the number of requests in flight
	return len(fields) > 8 && fields[8] != "0"
}

// Temperature is the temperature of a disk, in degrees Celsius.
type Temperature struct {
	Celsius float64
	// Warning is the temperature above which the disk is too hot, 0 if unknown (e.g. "temp1_max")
	Warning float64
	// Critical is the temperature at which the disk may be damaged, 0 if unknown (e.g. "temp1_crit")
	Critical float64
}

// plausible returns whether a value of hwmon is a temperature a disk can have, in degrees Celsius: some report
// placeholders, e.g. -273 or 65261 for "none".
func plausible(celsius float64) bool {
	return celsius > -40 && celsius < 150
}

// sensors returns the hwmon folders of the disk: SATA disks (drivetemp) have device/hwmon/hwmonN, NVMe drives
// device/hwmonN (of their controller).
func sensors(disk string) []string {
	device := filepath.Join(sysDir, "class/block", disk, "device")
	found, _ := filepath.Glob(filepath.Join(device, "hwmon", "hwmon*"))
	more, _ := filepath.Glob(filepath.Join(device, "hwmon[0-9]*"))
	return append(found, more...)
}

// HasSensor returns whether the disk has a temperature sensor, without reading it (which does not touch the disk).
func HasSensor(disk string) bool {
	for _, sensor := range sensors(disk) {
		if exists(filepath.Join(sensor, "temp1_input")) {
			return true
		}
	}
	return false
}

// ReadTemperature reads the temperature of the disk from its hwmon sensor: NVMe drives have one, SATA disks with
// the kernel module drivetemp. Returns false if it has none. This may reset the spin down timer of hard disks, see
// the package documentation.
func ReadTemperature(disk string) (Temperature, bool) {
	for _, sensor := range sensors(disk) {
		// temp1 is the main sensor, e.g. "Composite" of NVMe drives
		celsius, ok := readMilliCelsius(filepath.Join(sensor, "temp1_input"))
		if !ok {
			continue
		}
		temperature := Temperature{Celsius: celsius}
		if warning, ok := readMilliCelsius(filepath.Join(sensor, "temp1_max")); ok && warning > 0 {
			temperature.Warning = warning
		}
		if critical, ok := readMilliCelsius(filepath.Join(sensor, "temp1_crit")); ok && critical > 0 {
			temperature.Critical = critical
		}
		return temperature, true
	}
	return Temperature{}, false
}

// readMilliCelsius reads a temperature file of hwmon, in thousandths of a degree, as degrees.
func readMilliCelsius(path string) (float64, bool) {
	value, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	milli, err := strconv.ParseInt(strings.TrimSpace(string(value)), 10, 64)
	if err != nil {
		return 0, false
	}
	celsius := float64(milli) / 1000
	return celsius, plausible(celsius)
}

// NeedsDrivetemp returns whether the disk could report its temperature with the kernel module drivetemp, which is
// not loaded: a SCSI or SATA disk (NVMe drives report it without).
func NeedsDrivetemp(disk string) bool {
	return !exists(filepath.Join(sysDir, "module/drivetemp")) &&
		exists(filepath.Join(sysDir, "class/block", disk, "device/scsi_disk"))
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
