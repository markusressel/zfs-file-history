package zfs

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testMounts are the mounts of the damaged files in testdata/zpool-status-data-errors-verbose.txt: the home dataset
// and one of its snapshots (mounted while accessed), rpool/data is not mounted.
var testMounts = []Mount{
	{Name: "rpool/ROOT/ubuntu", Mountpoint: "/"},
	{Name: "rpool/USERDATA/home", Mountpoint: "/home"},
	{Name: "rpool/USERDATA/home@daily", Mountpoint: "/home/.zfs/snapshot/daily"},
}

func TestParsePoolStatus_ErrorEntries(t *testing.T) {
	statuses, err := parsePoolStatuses(readTestData(t, "zpool-status-data-errors-verbose.txt"))
	require.NoError(t, err)

	require.Len(t, statuses, 2, "the entries do not end the pool")
	assert.Equal(t, "Permanent errors have been detected in the following files:", statuses[0].Errors)
	assert.Equal(t, []string{
		"/home/user/Pictures/IMG_0001.jpg",
		"rpool/USERDATA/home@autosnap_2026-10-01:/user/Pictures/IMG_0001.jpg",
		"rpool/USERDATA/home@autosnap_2026-10-01:<0x1c2a>",
		"rpool/data:/archive/file name with spaces.bin",
		"/home/.zfs/snapshot/daily/user/old.txt",
		"<metadata>:<0x1b>",
		"<0x15>:<0x1b>",
	}, statuses[0].ErrorEntries)
	assert.Empty(t, statuses[0].Other, "no entry is taken for a field")
	assert.Equal(t, "tank", statuses[1].Name)
	assert.Empty(t, statuses[1].ErrorEntries)
}

func TestParseDataError(t *testing.T) {
	tests := []struct {
		entry    string
		expected DataError
	}{
		{"/home/user/Pictures/IMG_0001.jpg", DataError{Dataset: "rpool/USERDATA/home", Path: "/user/Pictures/IMG_0001.jpg",
			LivePath: "/home/user/Pictures/IMG_0001.jpg"}},
		{"/etc/passwd", DataError{Dataset: "rpool/ROOT/ubuntu", Path: "/etc/passwd", LivePath: "/etc/passwd"}},
		// the live path is the one the file has in the mounted dataset
		{"rpool/USERDATA/home@autosnap_2026-10-01:/user/Pictures/IMG_0001.jpg", DataError{Dataset: "rpool/USERDATA/home",
			Snapshot: "autosnap_2026-10-01", Path: "/user/Pictures/IMG_0001.jpg", LivePath: "/home/user/Pictures/IMG_0001.jpg"}},
		{"/home/.zfs/snapshot/daily/user/old.txt", DataError{Dataset: "rpool/USERDATA/home", Snapshot: "daily",
			Path: "/user/old.txt", LivePath: "/home/user/old.txt"}},
		// not mounted
		{"rpool/data:/archive/file name with spaces.bin", DataError{Dataset: "rpool/data", Path: "/archive/file name with spaces.bin"}},
		// not resolved
		{"rpool/USERDATA/home@autosnap_2026-10-01:<0x1c2a>", DataError{Dataset: "rpool/USERDATA/home", Snapshot: "autosnap_2026-10-01", Object: 0x1c2a}},
		{"rpool/USERDATA/home:<0xa0f02>", DataError{Dataset: "rpool/USERDATA/home", Object: 0xa0f02}},
		{"rpool/data:<0x0>", DataError{Dataset: "rpool/data"}},
		// zpool prints the files of a dataset mounted at / with two slashes
		{"//usr/share/doc", DataError{Dataset: "rpool/ROOT/ubuntu", Path: "/usr/share/doc", LivePath: "/usr/share/doc"}},
		{"<metadata>:<0x1b>", DataError{}},
		{"<0x15>:<0x1b>", DataError{}},
		{"garbage", DataError{}},
	}
	for _, test := range tests {
		t.Run(test.entry, func(t *testing.T) {
			test.expected.Pool = "rpool"
			test.expected.Entry = test.entry
			assert.Equal(t, test.expected, parseDataError("rpool", test.entry, testMounts))
		})
	}
}

func TestParseDataError_NoMount(t *testing.T) {
	// e.g. /proc/mounts changed in the meantime: still a path in the file system
	assert.Equal(t, DataError{Pool: "rpool", Entry: "/x/file", LivePath: "/x/file"}, parseDataError("rpool", "/x/file", nil))
}

func TestDataErrorIndex(t *testing.T) {
	var dataErrors []DataError
	for _, entry := range []string{
		"/home/user/Pictures/IMG_0001.jpg",
		"rpool/USERDATA/home@autosnap_2026-10-01:/user/Pictures/IMG_0001.jpg",
		"rpool/USERDATA/home@autosnap_2026-10-01:<0x1c2a>",
		"/home/.zfs/snapshot/daily/user/old.txt",
		"<metadata>:<0x1b>",
	} {
		dataErrors = append(dataErrors, parseDataError("rpool", entry, testMounts))
	}
	index := NewDataErrorIndex(dataErrors, []string{"tank"})

	assert.Equal(t, 5, index.Len())
	assert.Equal(t, []string{"tank"}, index.Unavailable)
	assert.Len(t, index.At("/home/user/Pictures/IMG_0001.jpg"), 2, "in the file system and in a snapshot")
	assert.Len(t, index.At("/home/user/Pictures/IMG_0001.jpg/"), 2, "cleaned")
	assert.True(t, index.IsDamaged("/home/user/Pictures/IMG_0001.jpg", ""))
	assert.True(t, index.IsDamaged("/home/user/Pictures/IMG_0001.jpg", "rpool/USERDATA/home@autosnap_2026-10-01"))
	assert.False(t, index.IsDamaged("/home/user/Pictures/IMG_0001.jpg", "rpool/USERDATA/home@daily"))
	assert.False(t, index.IsDamaged("/home/user/old.txt", ""), "only in the snapshot")
	assert.True(t, index.IsDamaged("/home/user/old.txt", "rpool/USERDATA/home@daily"))

	// each file once, also if it is damaged in a snapshot as well
	assert.Equal(t, 2, index.DamagedBelow("/home/user"))
	assert.Equal(t, 1, index.DamagedBelow("/home/user/Pictures"))
	assert.Equal(t, 2, index.DamagedBelow("/"))
	assert.Equal(t, 0, index.DamagedBelow("/home/user/Pictures/IMG_0001.jpg"))
	assert.Equal(t, 0, index.DamagedBelow("/home/other"))

	assert.Equal(t, 2, index.DamagedInSnapshot("rpool/USERDATA/home@autosnap_2026-10-01"), "also the one without a path")
	assert.Equal(t, 1, index.DamagedInSnapshot("rpool/USERDATA/home@daily"))
	assert.Equal(t, 0, index.DamagedInSnapshot("rpool/USERDATA/home@other"))
}

func TestDataErrorIndex_Nil(t *testing.T) {
	var index *DataErrorIndex
	assert.Equal(t, 0, index.Len())
	assert.Empty(t, index.At("/x"))
	assert.False(t, index.IsDamaged("/x", ""))
	assert.Equal(t, 0, index.DamagedBelow("/"))
	assert.Equal(t, 0, index.DamagedInSnapshot("a@b"))
}

// fakeDataErrorCommands replaces zpool and sudo with the given outputs (an error if empty and failed), and records
// the commands.
type fakeDataErrorCommands struct {
	commands []string
}

func setupFakeDataErrors(t *testing.T, zpoolOutput string, sudoOutput string, sudoErr error) *fakeDataErrorCommands {
	fake := &fakeDataErrorCommands{}
	originalPool, originalSudo, originalMounts := runPoolCommand, runSudoCommand, readZfsMounts
	runPoolCommand = func(name string, args ...string) (string, error) {
		fake.commands = append(fake.commands, name+" "+strings.Join(args, " "))
		if args[0] == "get" {
			return "rpool\tdisabled\n", nil
		}
		return zpoolOutput, nil
	}
	runSudoCommand = func(args ...string) (string, error) {
		fake.commands = append(fake.commands, "sudo "+strings.Join(args, " "))
		return sudoOutput, sudoErr
	}
	readZfsMounts = func() ([]Mount, error) { return testMounts, nil }
	originalRoot := isRoot
	isRoot = func() bool { return false }
	t.Cleanup(func() {
		runPoolCommand, runSudoCommand, readZfsMounts, isRoot = originalPool, originalSudo, originalMounts, originalRoot
	})
	return fake
}

func TestListDataErrors_AsRoot(t *testing.T) {
	fake := setupFakeDataErrors(t, readTestData(t, "zpool-status-data-errors-verbose.txt"), "", errors.New("not called"))

	index, err := ListDataErrors()

	require.NoError(t, err)
	// the feature is only looked up for the pools with data errors
	assert.Equal(t, []string{"zpool status -p -v", "zpool get -H -o name,value feature@head_errlog rpool"}, fake.commands)
	assert.Equal(t, 7, index.Len())
	assert.Empty(t, index.Unavailable)
	assert.Equal(t, []string{"rpool"}, index.WithoutHeadErrlog)
	assert.Equal(t, "rpool", index.Errors[0].Pool)
}

func TestListDataErrors_WithSudo(t *testing.T) {
	fake := setupFakeDataErrors(t, readTestData(t, "zpool-status-data-errors-denied.txt"),
		readTestData(t, "zpool-status-data-errors-verbose.txt"), nil)

	index, err := ListDataErrors()

	require.NoError(t, err)
	assert.Equal(t, []string{"zpool status -p -v", "sudo -n " + ZpoolPath() + " status -p -v",
		"zpool get -H -o name,value feature@head_errlog rpool"}, fake.commands)
	assert.Equal(t, 7, index.Len())
	assert.Empty(t, index.Unavailable)
}

func TestListDataErrors_NeedsRoot(t *testing.T) {
	setupFakeDataErrors(t, readTestData(t, "zpool-status-data-errors-denied.txt"), "", errors.New("sudo: a password is required"))

	index, err := ListDataErrors()

	// the pools whose list could not be read
	require.NoError(t, err)
	assert.Equal(t, 0, index.Len())
	assert.Equal(t, []string{"rpool"}, index.Unavailable)
}

func TestListDataErrors_SudoFails(t *testing.T) {
	setupFakeDataErrors(t, readTestData(t, "zpool-status-data-errors-denied.txt"), "", errors.New("internal error"))

	_, err := ListDataErrors()

	assert.EqualError(t, err, "internal error")
}

func TestPoolsWithoutHeadErrlog(t *testing.T) {
	original := runPoolCommand
	t.Cleanup(func() { runPoolCommand = original })
	runPoolCommand = func(name string, args ...string) (string, error) {
		return "rpool\tdisabled\ntank\tactive\nbackup\tenabled\n", nil
	}
	assert.Equal(t, []string{"rpool"}, System{}.PoolsWithoutHeadErrlog([]string{"rpool", "tank", "backup"}))
	assert.Nil(t, System{}.PoolsWithoutHeadErrlog(nil))

	// e.g. a version of ZFS without the feature
	runPoolCommand = func(name string, args ...string) (string, error) { return "", errors.New("bad property list") }
	assert.Nil(t, System{}.PoolsWithoutHeadErrlog([]string{"rpool"}))

	assert.Equal(t, "sudo zpool set feature@head_errlog=enabled rpool", EnableHeadErrlogCommand("rpool"))
}

func TestDataErrorIndex_WithLocations(t *testing.T) {
	gone := DataError{Pool: "rpool", Entry: "rpool/home:<0x10>", Dataset: "rpool/home", Object: 0x10}
	found := DataError{Pool: "rpool", Entry: "rpool/home:<0x20>", Dataset: "rpool/home", Object: 0x20}
	inSnapshot := DataError{Pool: "rpool", Entry: "rpool/home@b:<0x20>", Dataset: "rpool/home", Snapshot: "b", Object: 0x20}
	notSearched := DataError{Pool: "rpool", Entry: "rpool/home:<0x30>", Dataset: "rpool/home", Object: 0x30}
	index := NewDataErrorIndex([]DataError{gone, found, inSnapshot, notSearched}, nil)
	index.WithoutHeadErrlog = []string{"rpool"}

	located := index.WithLocations(map[ObjectKey][]ObjectLocation{
		{"rpool/home", 0x10}: nil,
		{"rpool/home", 0x20}: {
			{Snapshot: "a", Path: "/user/x", LivePath: "/home/user/x"},
			{Snapshot: "b", Path: "/user/x", LivePath: "/home/user/x"},
		},
	})

	require.Equal(t, 4, located.Len(), "the errors as listed by ZFS")
	assert.True(t, located.Errors[0].Searched)
	assert.Empty(t, located.Errors[0].Locations, "in no snapshot")
	assert.Len(t, located.Errors[1].Locations, 2)
	assert.Len(t, located.Errors[2].Locations, 1, "only the one in its snapshot")
	assert.False(t, located.Errors[3].Searched)
	assert.Equal(t, []string{"rpool"}, located.WithoutHeadErrlog)
	assert.Empty(t, index.Errors[1].Locations, "the index itself is not changed")

	// the found copies are marked like the files listed by ZFS
	assert.True(t, located.IsDamaged("/home/user/x", "rpool/home@a"))
	assert.True(t, located.IsDamaged("/home/user/x", "rpool/home@b"))
	assert.False(t, located.IsDamaged("/home/user/x", ""))
	assert.Equal(t, 1, located.DamagedBelow("/home/user"))
	assert.Equal(t, 1, located.DamagedInSnapshot("rpool/home@a"))
	// listed by ZFS in b already, not counted twice
	assert.Equal(t, 2, located.DamagedInSnapshot("rpool/home@b"))

	assert.Nil(t, (*DataErrorIndex)(nil).WithLocations(nil))
}

func TestListDataErrors_RootWithoutErrorsField(t *testing.T) {
	// e.g. a pool whose status has no errors field for another reason
	fake := setupFakeDataErrors(t, readTestData(t, "zpool-status-data-errors-denied.txt"), "", errors.New("not called"))
	isRoot = func() bool { return true }

	index, err := ListDataErrors()

	// Regression: listed as needing root, and sudo was run on every load
	require.NoError(t, err)
	assert.Empty(t, index.Unavailable)
	assert.NotContains(t, strings.Join(fake.commands, "\n"), "sudo")
}
