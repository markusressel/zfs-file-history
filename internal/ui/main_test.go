package ui

import (
	"fmt"
	"os"
	"slices"
	"sync"
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

// testSystem is the zfs.Backend of the tests of this package: a system without ZFS (see zfs.NoSystem), unless a test
// gives it pools (see setPools). Set once in TestMain: the loads of earlier tests may still run in the background.
var testSystem = &fakeSystem{}

// fakeSystem is a system without ZFS, or with the pools of a test. Safe for concurrent use.
type fakeSystem struct {
	zfs.NoSystem
	mutex sync.Mutex
	// pools are the statuses of the pools, nil: no ZFS. Guarded by mutex.
	pools []*zfs.PoolStatus
}

// setPools gives the system the pools until the end of the test.
func (system *fakeSystem) setPools(t *testing.T, pools ...*zfs.PoolStatus) {
	system.mutex.Lock()
	defer system.mutex.Unlock()
	system.pools = pools
	t.Cleanup(func() {
		system.mutex.Lock()
		defer system.mutex.Unlock()
		system.pools = nil
	})
}

func (system *fakeSystem) ListPools() ([]*zfs.PoolListEntry, error) {
	system.mutex.Lock()
	defer system.mutex.Unlock()
	if system.pools == nil {
		return nil, zfs.ErrNoSystem
	}
	var entries []*zfs.PoolListEntry
	for _, pool := range system.pools {
		entries = append(entries, &zfs.PoolListEntry{Name: pool.Name, Health: pool.State})
	}
	return entries, nil
}

func (system *fakeSystem) PoolStatuses() ([]*zfs.PoolStatus, error) {
	system.mutex.Lock()
	defer system.mutex.Unlock()
	if system.pools == nil {
		return nil, zfs.ErrNoSystem
	}
	return slices.Clone(system.pools), nil
}

func (system *fakeSystem) PoolStatus(name string) (*zfs.PoolStatus, error) {
	statuses, err := system.PoolStatuses()
	if err != nil {
		return nil, err
	}
	for _, status := range statuses {
		if status.Name == name {
			return status, nil
		}
	}
	return nil, fmt.Errorf("cannot open '%s': no such pool", name)
}

func TestMain(m *testing.M) {
	zfs.SetBackend(testSystem)
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
