package zfs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDataset_GetSnapshotsDir(t *testing.T) {
	dataset := &Dataset{
		Path:          "/pool/ds1",
		HiddenZfsPath: "/pool/ds1/.zfs",
	}

	assert.Equal(t, "/pool/ds1/.zfs/snapshot", dataset.GetSnapshotsDir())
}
