package zfs

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseZfsMountPaths(t *testing.T) {
	mounts := strings.Join([]string{
		"rpool/ROOT/arch / zfs rw,relatime,xattr,posixacl 0 0",
		"proc /proc proc rw,nosuid,nodev,noexec,relatime 0 0",
		"rpool/data /mnt/my\\040data zfs rw,relatime 0 0",
		"rpool/legacy /srv/legacy/ zfs rw,relatime 0 0",
		"rpool/data /second/mount zfs rw,relatime 0 0",
		"/dev/sda1 /boot vfat rw 0 0",
		"",
		"broken-line",
	}, "\n")

	result := parseZfsMountPaths(strings.NewReader(mounts))

	assert.Equal(t, map[string]string{
		"rpool/ROOT/arch": "/",
		"rpool/data":      "/mnt/my data",
		"rpool/legacy":    "/srv/legacy",
	}, result)
}

func TestUnescapeMountField(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/plain/path", "/plain/path"},
		{"/with\\040space", "/with space"},
		{"/tab\\011and\\134backslash", "/tab\tand\\backslash"},
		{"/trailing\\04", "/trailing\\04"},
		{"/invalid\\999", "/invalid\\999"},
		{"\\", "\\"},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			assert.Equal(t, test.expected, unescapeMountField(test.input))
		})
	}
}

func TestDatasetListEntry_TableRowId(t *testing.T) {
	// the mountpoint is not unique (e.g. "legacy" or "none"), the name is
	a := DatasetListEntry{Name: "pool/a", Mountpoint: "legacy"}
	b := DatasetListEntry{Name: "pool/b", Mountpoint: "legacy"}
	assert.NotEqual(t, a.TableRowId(), b.TableRowId())
	assert.Equal(t, "pool/a", a.TableRowId())
}

func TestOpenDatasetByName_EmptyName(t *testing.T) {
	_, err := OpenDatasetByName("", "")
	assert.Error(t, err)
}

func TestParseDatasetList(t *testing.T) {
	output := strings.Join([]string{
		"rpool\t3000\t1000\t0\t98304\t2000\t0\t/",
		"rpool/data\t2500\t1000\t500\t1500\t0\t500\t/mnt/my data",
		"rpool/legacy\t100\t1000\t-\t100\t0\t-\tlegacy",
		"",
	}, "\n")
	mountPaths := map[string]string{"rpool": "/", "rpool/data": "/mnt/my data"}

	result, err := parseDatasetList(output, mountPaths)

	require.NoError(t, err)
	assert.Equal(t, []*DatasetListEntry{
		{Name: "rpool", Used: 3000, Available: 1000, UsedBySnapshots: 0, UsedByDataset: 98304, UsedByChildren: 2000, Mountpoint: "/", MountPath: "/"},
		{Name: "rpool/data", Used: 2500, Available: 1000, UsedBySnapshots: 500, UsedByDataset: 1500, UsedByChildren: 0, UsedByRefreservation: 500, Mountpoint: "/mnt/my data", MountPath: "/mnt/my data"},
		// "-" (not applicable) is 0
		{Name: "rpool/legacy", Used: 100, Available: 1000, UsedBySnapshots: 0, UsedByDataset: 100, UsedByChildren: 0, Mountpoint: "legacy"},
	}, result)
}

func TestParseDatasetList_Empty(t *testing.T) {
	result, err := parseDatasetList("", nil)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestParseDatasetList_Invalid(t *testing.T) {
	tests := map[string]string{
		"missing fields": "rpool\t3000\t1000",
		"invalid size":   "rpool\t3000\t1000\tabc\t0\t0\t0\t/",
	}
	for name, output := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := parseDatasetList(output, nil)
			assert.Error(t, err)
		})
	}
}
