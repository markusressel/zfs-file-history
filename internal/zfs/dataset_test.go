package zfs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDataset_GetSnapshotsDir(t *testing.T) {
	dataset := &Dataset{
		Path:          "/pool/ds1",
		HiddenZfsPath: "/pool/ds1/.zfs",
	}

	assert.Equal(t, "/pool/ds1/.zfs/snapshot", dataset.GetSnapshotsDir())
}

func TestDataset_GettersFallback(t *testing.T) {
	dataset := &Dataset{
		Path:          "/pool/ds1",
		HiddenZfsPath: "/pool/ds1/.zfs",
	}

	assert.Equal(t, "", dataset.GetType())
	assert.Equal(t, "", dataset.GetMountPoint())
	assert.Equal(t, "", dataset.GetMounted())
	assert.Equal(t, "", dataset.GetCanMount())
	assert.Equal(t, "", dataset.GetReadonly())
	assert.Equal(t, uint64(0), dataset.GetVolSize())
	assert.Equal(t, uint64(0), dataset.GetAvailable())
	assert.Equal(t, uint64(0), dataset.GetUsed())
}

func TestFindHostDataset(t *testing.T) {
	// Empty path
	_, err := FindHostDataset("")
	assert.Error(t, err)

	// Temp dir with simulated .zfs folder
	tempDir := t.TempDir()
	zfsDir := filepath.Join(tempDir, ".zfs")
	require.NoError(t, os.Mkdir(zfsDir, 0755))

	// Subfolder inside dataset
	subDir := filepath.Join(tempDir, "a", "b", "c")
	require.NoError(t, os.MkdirAll(subDir, 0755))

	ds, err := FindHostDataset(subDir)
	require.NoError(t, err)
	assert.Equal(t, tempDir, ds.Path)
	assert.Equal(t, zfsDir, ds.HiddenZfsPath)
}

func TestRefreshZfsDataAndStatus(t *testing.T) {
	assert.True(t, IsDatasetsLoaded())
	RefreshZfsData()
}
