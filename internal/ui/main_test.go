package ui

import (
	"os"
	"testing"
	"time"
	"zfs-file-history/internal/disk"
	"zfs-file-history/internal/zfs"
)

// noDisks is a disk.TemperatureSampler and disk.SmartSource for devices that are not stored on disks.
type noDisks struct{}

func (noDisks) Sample(devices []string, _ []string) map[string]disk.Reading {
	readings := map[string]disk.Reading{}
	for _, device := range devices {
		readings[device] = disk.Reading{}
	}
	return readings
}

func (noDisks) ReadDevices(devices []string, _ time.Duration) map[string][]disk.SmartReport {
	return map[string][]disk.SmartReport{}
}

func (noDisks) Reset() {}

func TestMain(m *testing.M) {
	// all allowed: tests never run sudo, and the permissions are not offered at the start
	allowedSudoersRules = func() map[string]bool {
		allowed := map[string]bool{}
		for _, rule := range zfs.SudoersRules {
			allowed[rule.Id] = true
		}
		return allowed
	}
	// tests never read the disks of the system
	newTemperatureSampler = func() disk.TemperatureSampler { return noDisks{} }
	newSmartSource = func() disk.SmartSource { return noDisks{} }
	os.Exit(m.Run())
}
