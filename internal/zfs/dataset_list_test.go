package zfs

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
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
