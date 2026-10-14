package zfs

import (
	"fmt"
	"path"
	"slices"
	"strings"
)

// PoolAlertSeverity is how bad a problem of a pool is, see PoolAlert.
type PoolAlertSeverity int

const (
	// PoolAlertWarning is a problem that does not lose data yet, e.g. a degraded pool or checksum errors that could
	// be repaired
	PoolAlertWarning PoolAlertSeverity = iota
	// PoolAlertError is a problem that loses data or makes the pool (or a device) unusable, e.g. permanent data
	// errors or a faulted device
	PoolAlertError
)

// Kinds of alerts, see PoolAlert.Kind.
const (
	// PoolAlertState is a pool that is not ONLINE
	PoolAlertState = "state"
	// PoolAlertDataErrors are permanent errors in files or metadata ("errors:" of zpool status)
	PoolAlertDataErrors = "data errors"
	// PoolAlertDeviceErrors are read, write or checksum errors of devices
	PoolAlertDeviceErrors = "device errors"
	// PoolAlertDeviceState are devices that cannot be used, e.g. FAULTED ones
	PoolAlertDeviceState = "device state"
	// PoolAlertSpareState are spares that cannot be used. Only a warning: no data is at risk.
	PoolAlertSpareState = "spare state"
	// PoolAlertTemperature are disks at or above their critical temperature. Not from zpool status, see
	// pool_activity.Monitor.
	PoolAlertTemperature = "temperature"
	// PoolAlertSmart are disks whose SMART values tell of a failure or of wear. Not from zpool status, see
	// pool_activity.Monitor.
	PoolAlertSmart = "smart"
)

// sparesGroup is the name of the group of the spares in the config of zpool status.
const sparesGroup = "spares"

// noDataErrors is what zpool status prints as "errors:" of a pool without data errors.
const noDataErrors = "No known data errors"

// dataErrorsListHint is what zpool status prints behind the number of data errors without -v.
const dataErrorsListHint = ", use '-v' for a list"

// unavailableDeviceStates are the states of devices that cannot be used. OFFLINE is left out: a device is taken
// offline on purpose, and the pool reports itself as degraded anyway.
var unavailableDeviceStates = []string{PoolStateFaulted, PoolStateUnavail, PoolStateRemoved}

// PoolAlert is a problem of a pool that needs attention, see PoolStatus.Alerts.
type PoolAlert struct {
	Pool     string
	Severity PoolAlertSeverity
	// Kind is what the alert is about, e.g. PoolAlertDataErrors. A pool has at most one alert of each kind.
	Kind string
	// Message describes the problem in one line, e.g. "18 data errors in files or metadata"
	Message string
}

// Alerts returns the problems of the pool, most severe first: a state other than ONLINE, permanent data errors,
// devices that cannot be used, devices with read, write or checksum errors, and spares that cannot be used. Empty if
// the pool is healthy. A pool whose features are not all enabled is healthy.
func (status *PoolStatus) Alerts() []PoolAlert {
	var alerts []PoolAlert
	if alert, ok := status.stateAlert(); ok {
		alerts = append(alerts, alert)
	}
	if errors := status.DataErrors(); errors != "" {
		alerts = append(alerts, PoolAlert{Pool: status.Name, Severity: PoolAlertError, Kind: PoolAlertDataErrors,
			Message: errors + " in files or metadata"})
	}

	var unavailable, unavailableSpares, withErrors []string
	// group is the group of devices being read, e.g. "logs", "" for the devices of the pool itself
	group := ""
	for _, device := range status.Devices {
		if device.Depth == 0 {
			group = ""
			if device.IsGroup() {
				group = device.Name
			}
		}
		// the line of the pool itself repeats its state, and the sum of the errors of its devices
		if device.Depth == 0 || device.IsGroup() {
			continue
		}
		if slices.Contains(unavailableDeviceStates, device.State) {
			description := DeviceDisplayName(device.Name) + " " + device.State
			if device.Note != "" {
				description += " (" + device.Note + ")"
			}
			if group == sparesGroup {
				unavailableSpares = append(unavailableSpares, description)
			} else {
				unavailable = append(unavailable, description)
			}
		}
		if device.HasErrors() {
			withErrors = append(withErrors, DeviceDisplayName(device.Name)+" "+formatDeviceErrors(device))
		}
	}
	if len(unavailable) > 0 {
		alerts = append(alerts, PoolAlert{Pool: status.Name, Severity: PoolAlertError, Kind: PoolAlertDeviceState,
			Message: "Devices not available: " + strings.Join(unavailable, "; ")})
	}
	if len(withErrors) > 0 {
		alerts = append(alerts, PoolAlert{Pool: status.Name, Severity: PoolAlertWarning, Kind: PoolAlertDeviceErrors,
			Message: "Device errors: " + strings.Join(withErrors, "; ")})
	}
	if len(unavailableSpares) > 0 {
		alerts = append(alerts, PoolAlert{Pool: status.Name, Severity: PoolAlertWarning, Kind: PoolAlertSpareState,
			Message: "Spares not available: " + strings.Join(unavailableSpares, "; ")})
	}
	SortPoolAlerts(alerts)
	return alerts
}

// SortPoolAlerts sorts the alerts by severity, the most severe first. Alerts of the same severity keep their order,
// e.g. the order of the pools.
func SortPoolAlerts(alerts []PoolAlert) {
	slices.SortStableFunc(alerts, func(a, b PoolAlert) int { return int(b.Severity) - int(a.Severity) })
}

// stateAlert returns the alert of a pool that is not ONLINE, with the first sentence of its status (e.g. "Pool is
// DEGRADED: One or more devices could not be used because the label is missing or invalid."). DEGRADED is a
// warning: the pool still works, but with less redundancy.
func (status *PoolStatus) stateAlert() (PoolAlert, bool) {
	if status.State == "" || status.State == PoolStateOnline {
		return PoolAlert{}, false
	}
	severity := PoolAlertError
	if status.State == PoolStateDegraded {
		severity = PoolAlertWarning
	}
	message := "Pool is " + status.State
	if sentence := firstSentence(status.Status); sentence != "" {
		message += ": " + sentence
	}
	return PoolAlert{Pool: status.Name, Severity: severity, Kind: PoolAlertState, Message: message}, true
}

// DataErrors returns the summary of the permanent data errors of the pool, e.g. "18 data errors", or "" if there
// are none.
func (status *PoolStatus) DataErrors() string {
	firstLine, _, _ := strings.Cut(status.Errors, "\n")
	firstLine = strings.TrimSpace(firstLine)
	if firstLine == "" || firstLine == noDataErrors {
		return ""
	}
	return strings.TrimSuffix(firstLine, dataErrorsListHint)
}

// firstSentence returns the first sentence of a text that zpool status wraps into several lines, in one line.
func firstSentence(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if end := strings.Index(text, ". "); end >= 0 {
		return text[:end+1]
	}
	return text
}

// DeviceDisplayName returns the name of a device without the directory of its path, e.g.
// "nvme-SSD_1TB_SERIAL0001-part3" for "/dev/disk/by-id/nvme-SSD_1TB_SERIAL0001-part3".
func DeviceDisplayName(name string) string {
	if strings.HasPrefix(name, "/") {
		return path.Base(name)
	}
	return name
}

// formatDeviceErrors returns the errors of a device that are not 0, e.g. "12 read, 340 write".
func formatDeviceErrors(device *PoolDevice) string {
	var parts []string
	counters := []struct {
		name  string
		count uint64
	}{{"read", device.Read}, {"write", device.Write}, {"checksum", device.Checksum}}
	for _, counter := range counters {
		if counter.count > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counter.count, counter.name))
		}
	}
	return strings.Join(parts, ", ")
}
