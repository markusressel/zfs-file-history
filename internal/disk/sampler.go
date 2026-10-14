package disk

import (
	"slices"
	"sync"
	"time"
)

const (
	// TemperatureInterval is how often the temperature of a device is read at most, unless asked to.
	TemperatureInterval = 30 * time.Second
	// activityWindow is how old the last I/O statistics of a disk may be to tell from them whether it is busy: older
	// ones may have changed long ago, the disk may sleep by now. Longer than the interval in which the pools are
	// reloaded while nothing runs (a minute), so the alerts notice busy hard disks too. A read resets the spin down
	// timer of some disks, so it is delayed by at most this long after the last I/O.
	activityWindow = 90 * time.Second
)

// Reading is what is known about the temperature of a device of a pool, see Sampler.
type Reading struct {
	// IsDisk is whether the device is stored on disks, see Resolve. All other fields are unset without.
	IsDisk bool
	// Temperature is the last one read, valid if Known. Of the hottest disk, if the device is stored on several.
	Temperature Temperature
	Known       bool
	// ReadAt is when Temperature was read
	ReadAt time.Time
	// Idle is whether the device is a hard disk that was not busy, so its temperature was not read (again) since
	// ReadAt, so it can spin down. Read it with Sampler.Sample(force).
	Idle bool
	// NoSensor is whether none of the disks has a temperature sensor
	NoSensor bool
	// NeedsDrivetemp is whether NoSensor because the kernel module drivetemp is not loaded, see NeedsDrivetemp
	NeedsDrivetemp bool
}

// TemperatureSampler reads the temperatures of the devices of pools, see Sampler.
type TemperatureSampler interface {
	Sample(devices []string, forced []string) map[string]Reading
}

// IsCritical returns whether the disk is at or above the temperature at which it may be damaged, as read now (not
// the outdated one of an idle hard disk).
func (reading Reading) IsCritical() bool {
	return reading.Known && !reading.Idle && reading.Temperature.Critical > 0 &&
		reading.Temperature.Celsius >= reading.Temperature.Critical
}

// Sampler reads the temperatures of the devices of pools, without waking up hard disks: it compares the I/O
// statistics of a hard disk with the ones of the last call, and only reads its temperature if they changed (it is
// busy anyway, e.g. by a scrub). Drives that do not spin (SSDs, NVMe) are always read.
//
// It is safe for concurrent use, its methods may block (they read sysfs), so they must not be called on the UI
// thread.
type Sampler struct {
	// now is replaceable in tests
	now func() time.Time

	mu sync.Mutex
	// activities are the last I/O statistics of each disk, by its kernel name
	activities map[string]activitySample
	// readings are the last readings of each device, by its name in "zpool status"
	readings map[string]Reading
}

type activitySample struct {
	activity string
	at       time.Time
}

func NewSampler() *Sampler {
	return &Sampler{
		now:        time.Now,
		activities: map[string]activitySample{},
		readings:   map[string]Reading{},
	}
}

// Sample returns the readings of the devices (their names in "zpool status"), reading the temperatures that are
// older than TemperatureInterval, of hard disks only if they are busy. forced devices are read in any case, e.g. when
// the user asks for them.
func (sampler *Sampler) Sample(devices []string, forced []string) map[string]Reading {
	sampler.mu.Lock()
	defer sampler.mu.Unlock()

	now := sampler.now()
	result := make(map[string]Reading, len(devices))
	for _, device := range devices {
		reading := sampler.sample(device, slices.Contains(forced, device), now)
		sampler.readings[device] = reading
		result[device] = reading
	}
	return result
}

// sample returns the new reading of the device. Holds mu.
func (sampler *Sampler) sample(device string, forced bool, now time.Time) Reading {
	disks := Resolve(device)
	if len(disks) == 0 {
		return Reading{}
	}

	last := sampler.readings[device]
	last.IsDisk = true
	busy, rotational, hasSensor, needsDrivetemp := false, false, false, false
	for _, disk := range disks {
		activity := Activity(disk)
		previous, ok := sampler.activities[disk]
		if activity != "" {
			if ok && now.Sub(previous.at) <= activityWindow && previous.activity != activity || IsBusy(activity) {
				busy = true
			}
			sampler.activities[disk] = activitySample{activity: activity, at: now}
		}
		rotational = rotational || IsRotational(disk)
		hasSensor = hasSensor || HasSensor(disk)
		needsDrivetemp = needsDrivetemp || NeedsDrivetemp(disk)
	}

	if !hasSensor {
		// known without touching the disks
		return Reading{IsDisk: true, NoSensor: true, NeedsDrivetemp: needsDrivetemp}
	}
	if rotational && !busy && !forced {
		last.Idle = true
		return last
	}
	last.Idle = false
	if !forced && last.Known && now.Sub(last.ReadAt) < TemperatureInterval {
		return last
	}

	reading := Reading{IsDisk: true, ReadAt: now, NoSensor: true}
	for _, disk := range disks {
		temperature, ok := ReadTemperature(disk)
		if ok && (!reading.Known || temperature.Celsius > reading.Temperature.Celsius) {
			reading.Temperature, reading.Known, reading.NoSensor = temperature, true, false
		}
	}
	return reading
}
