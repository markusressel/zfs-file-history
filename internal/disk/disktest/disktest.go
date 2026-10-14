// Package disktest has fakes of the readers of the temperatures and SMART values of disks, for the tests of the
// components that show them, so they never read sysfs or run smartctl.
package disktest

import (
	"maps"
	"slices"
	"sync"
	"time"
	"zfs-file-history/internal/disk"
)

// Sampler is a disk.TemperatureSampler that returns the readings set, and records the forced devices of each sample.
// A forced device that is idle is read (45 °C), like disk.Sampler reads idle hard disks only when forced. It is safe
// for concurrent use.
type Sampler struct {
	mu       sync.Mutex
	readings map[string]disk.Reading
	forced   [][]string
}

var _ disk.TemperatureSampler = (*Sampler)(nil)

// NewSampler returns a sampler of the readings, by device (nil: none).
func NewSampler(readings map[string]disk.Reading) *Sampler {
	return &Sampler{readings: maps.Clone(readings)}
}

func (sampler *Sampler) Sample(devices []string, forced []string) map[string]disk.Reading {
	sampler.mu.Lock()
	defer sampler.mu.Unlock()
	sampler.forced = append(sampler.forced, forced)
	result := map[string]disk.Reading{}
	for _, device := range devices {
		reading := sampler.readings[device]
		if slices.Contains(forced, device) && reading.Idle {
			reading = disk.Reading{IsDisk: true, Known: true, Temperature: disk.Temperature{Celsius: 45}}
			sampler.readings[device] = reading
		}
		result[device] = reading
	}
	return result
}

// Set replaces the readings, e.g. while the test runs.
func (sampler *Sampler) Set(readings map[string]disk.Reading) {
	sampler.mu.Lock()
	defer sampler.mu.Unlock()
	sampler.readings = maps.Clone(readings)
}

// Forced returns the forced devices of each sample so far, nil for a sample that forced none.
func (sampler *Sampler) Forced() [][]string {
	sampler.mu.Lock()
	defer sampler.mu.Unlock()
	return slices.Clone(sampler.forced)
}

// Smart is a disk.SmartSource that returns the reports set, by device, and records the reads and resets. It is safe
// for concurrent use.
type Smart struct {
	mu      sync.Mutex
	reports map[string][]disk.SmartReport
	maxAges []time.Duration
	resets  int
}

var _ disk.SmartSource = (*Smart)(nil)

// NewSmart returns a source of the reports, by device (nil: none).
func NewSmart(reports map[string][]disk.SmartReport) *Smart {
	smart := &Smart{reports: maps.Clone(reports)}
	if smart.reports == nil {
		smart.reports = map[string][]disk.SmartReport{}
	}
	return smart
}

func (smart *Smart) ReadDevices(devices []string, maxAge time.Duration) map[string][]disk.SmartReport {
	smart.mu.Lock()
	defer smart.mu.Unlock()
	smart.maxAges = append(smart.maxAges, maxAge)
	result := map[string][]disk.SmartReport{}
	for _, device := range devices {
		result[device] = smart.reports[device]
	}
	return result
}

func (smart *Smart) Reset() {
	smart.mu.Lock()
	defer smart.mu.Unlock()
	smart.resets++
}

// SetReports sets the reports of the device, e.g. while the test runs.
func (smart *Smart) SetReports(device string, reports []disk.SmartReport) {
	smart.mu.Lock()
	defer smart.mu.Unlock()
	smart.reports[device] = reports
}

// MaxAges returns the maximum age asked for in each read so far.
func (smart *Smart) MaxAges() []time.Duration {
	smart.mu.Lock()
	defer smart.mu.Unlock()
	return slices.Clone(smart.maxAges)
}

// Resets returns how often Reset was called.
func (smart *Smart) Resets() int {
	smart.mu.Lock()
	defer smart.mu.Unlock()
	return smart.resets
}
