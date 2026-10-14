package disk_browser

import (
	"context"
	"errors"
	"slices"
	"time"
	"zfs-file-history/internal/disk"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	uiutil "zfs-file-history/internal/ui/util"

	"github.com/rivo/tview"
)

const (
	// sampleInterval is how often the temperatures are sampled while the disks are shown: whether hard disks are busy
	// is told by comparing their I/O statistics between two samples (see disk.Sampler)
	sampleInterval = 10 * time.Second
	// smartInterval is how old the SMART values may be while the disks are shown: smartctl runs with sudo, which
	// logs every call. The activity monitor reads them every hour (for the alerts), the reports are shared.
	smartInterval = 10 * time.Minute
)

// HealthReader reads the temperatures and SMART values of the devices of pools for a component that shows them (the
// devices pane of the pools page, the disks page), only while it is drawn: the component calls ReadIfDue in its Draw,
// so nothing is read while another page is shown. The temperatures are sampled every sampleInterval, idle hard disks
// only when asked to (ReadTemperatures), so they can spin down (see disk.Sampler); the SMART values if older than
// smartInterval, or when asked to (ReadSmart).
type HealthReader struct {
	application *tview.Application
	sampler     disk.TemperatureSampler
	smart       disk.SmartSource
	loader      *uiutil.DataLoader[map[string]disk.Reading]
	smartLoader *uiutil.DataLoader[map[string][]disk.SmartReport]

	// All fields below are only accessed on the UI thread.

	// readings are the last readings of the devices, by their name
	readings map[string]disk.Reading
	// sampledDevices are the devices of the last sample, sampledAt when it was started
	sampledDevices []string
	sampledAt      time.Time
	// sampleTimer draws again after sampleInterval, which samples again if the devices are still shown
	sampleTimer *time.Timer
	// smartReports are the SMART reports of the disks of the devices, by the name of the device; nil until read
	smartReports map[string][]disk.SmartReport
	// smartDevices are the devices SMART was last read for, smartReadAt when
	smartDevices []string
	smartReadAt  time.Time
}

// NewHealthReader returns a reader of the temperatures (with the sampler) and SMART values (with smart, nil: none) of
// devices, calling onChange on the UI thread when new ones were read. Must be called on the UI thread.
func NewHealthReader(application *tview.Application, sampler disk.TemperatureSampler, smart disk.SmartSource, onChange func()) *HealthReader {
	reader := &HealthReader{application: application, sampler: sampler, smart: smart}
	reader.loader = uiutil.NewDataLoader[map[string]disk.Reading](application).
		OnLoad(func(readings map[string]disk.Reading) {
			reader.readings = readings
			onChange()
			reader.scheduleSample()
		})
	reader.smartLoader = uiutil.NewDataLoader[map[string][]disk.SmartReport](application).
		OnLoad(func(reports map[string][]disk.SmartReport) {
			reader.smartReports = reports
			onChange()
		})
	return reader
}

// Readings returns the last readings of the temperatures of the devices, by their name. Must be called on the UI
// thread.
func (reader *HealthReader) Readings() map[string]disk.Reading {
	return reader.readings
}

// SmartReports returns the SMART reports of the disks of the devices, by the name of the device; nil until read.
// Must be called on the UI thread.
func (reader *HealthReader) SmartReports() map[string][]disk.SmartReport {
	return reader.smartReports
}

// ReadIfDue samples the devices if they changed or the last sample is older than sampleInterval, and reads their
// SMART values if they changed or the last read is older than smartInterval. Called while they are drawn. Must be
// called on the UI thread.
func (reader *HealthReader) ReadIfDue(devices []string) {
	// a timer may fire a little early
	if reader.sampler != nil && !reader.loader.IsLoading() &&
		(!slices.Equal(devices, reader.sampledDevices) || time.Since(reader.sampledAt) >= sampleInterval-time.Second) {
		reader.ReadTemperatures(devices, nil)
	}
	if reader.smart != nil && !reader.smartLoader.IsLoading() &&
		(reader.smartReports == nil || !slices.Equal(devices, reader.smartDevices) || time.Since(reader.smartReadAt) >= smartInterval) {
		reader.readSmart(devices, smartInterval)
	}
}

// ReadTemperatures samples the devices in the background, the forced ones also if they are idle hard disks. Must be
// called on the UI thread.
func (reader *HealthReader) ReadTemperatures(devices []string, forced []string) {
	if reader.sampler == nil {
		return
	}
	reader.sampledDevices = devices
	reader.sampledAt = time.Now()
	sampler := reader.sampler
	reader.loader.LoadQuietly(func(ctx context.Context) (map[string]disk.Reading, error) {
		return sampler.Sample(devices, forced), nil
	})
}

// ReadSmart reads the SMART values of the devices again, also if they were read recently. Disks that sleep are not
// woken up. Must be called on the UI thread.
func (reader *HealthReader) ReadSmart(devices []string) {
	reader.readSmart(devices, 0)
}

// readSmart reads the SMART values of the devices in the background, the ones older than maxAge. Runs on the UI
// thread.
func (reader *HealthReader) readSmart(devices []string, maxAge time.Duration) {
	if reader.smart == nil {
		return
	}
	reader.smartDevices = devices
	reader.smartReadAt = time.Now()
	smart := reader.smart
	reader.smartLoader.LoadQuietly(func(ctx context.Context) (map[string][]disk.SmartReport, error) {
		return smart.ReadDevices(devices, maxAge), nil
	})
}

// scheduleSample draws again after sampleInterval, which samples the devices again if they are still shown (see
// ReadIfDue). Runs on the UI thread.
func (reader *HealthReader) scheduleSample() {
	if reader.sampleTimer != nil {
		reader.sampleTimer.Stop()
	}
	application := reader.application
	reader.sampleTimer = time.AfterFunc(sampleInterval, func() {
		application.QueueUpdateDraw(func() {})
	})
}

// FooterHints returns the hints about the values of the devices that were not read, for the footer of the
// component: the temperatures of idle hard disks (a click runs readTemperatures) and of disks without the kernel
// module drivetemp, and SMART values that need root (a click runs onSetup, nil: not offered). Must be called on the
// UI thread.
func (reader *HealthReader) FooterHints(devices []string, readTemperatures func(), onSetup func()) []uiutil.FooterPart {
	idle, needsDrivetemp, smartNeedsRoot := false, false, false
	for _, name := range devices {
		reading := reader.readings[name]
		idle = idle || reading.Idle && !reading.Known
		needsDrivetemp = needsDrivetemp || reading.NeedsDrivetemp
		for _, report := range reader.smartReports[name] {
			smartNeedsRoot = smartNeedsRoot || errors.Is(report.Err, disk.ErrSmartNeedsRoot)
		}
	}
	var parts []uiutil.FooterPart
	if needsDrivetemp {
		parts = append(parts, uiutil.FooterPart{Text: txwidgets.Span(theme.Colors.Pool.Hint, "SATA temperatures need: modprobe drivetemp")})
	}
	if idle {
		parts = append(parts, uiutil.FooterPart{
			Text:    txwidgets.Span(theme.Colors.Pool.ReadTemperature, "%s idle, not read: t", ReadTemperatureIcon),
			OnClick: readTemperatures,
		})
	}
	if smartNeedsRoot && onSetup != nil {
		parts = append(parts, uiutil.FooterPart{Text: txwidgets.Span(theme.Colors.Pool.ReadTemperature, "SMART needs root: allow"), OnClick: onSetup})
	}
	return parts
}
