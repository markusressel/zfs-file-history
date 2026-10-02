package zfs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseHolds(t *testing.T) {
	output := "pool/data@daily\tzfs-file-history\t1727863440\n" +
		"pool/data/child@daily\tsyncoid_host\t1727863441\n"

	holds, err := parseHolds(output)

	require.NoError(t, err)
	assert.Equal(t, []Hold{
		{Snapshot: "pool/data@daily", Tag: "zfs-file-history", Created: time.Unix(1727863440, 0)},
		{Snapshot: "pool/data/child@daily", Tag: "syncoid_host", Created: time.Unix(1727863441, 0)},
	}, holds)
}

func TestParseHolds_Empty(t *testing.T) {
	holds, err := parseHolds("")
	require.NoError(t, err)
	assert.Empty(t, holds)
}

func TestParseHolds_Invalid(t *testing.T) {
	tests := map[string]string{
		"missing fields":    "pool/data@daily\tzfs-file-history",
		"invalid timestamp": "pool/data@daily\tzfs-file-history\tWed Oct  2 12:04 2024",
	}
	for name, output := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := parseHolds(output)
			assert.Error(t, err)
		})
	}
}

func TestHoldFunctions_NoSnapshots(t *testing.T) {
	// nothing to do, so no zfs process is spawned
	holds, err := ListHolds(nil, true)
	assert.NoError(t, err)
	assert.Empty(t, holds)
	assert.NoError(t, HoldSnapshots(nil))
	assert.NoError(t, ReleaseSnapshots(nil))
}
