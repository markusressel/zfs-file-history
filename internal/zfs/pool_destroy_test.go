package zfs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreviewDestroyPool(t *testing.T) {
	b := useActionBackend(t, cloneBackend())

	preview, err := PreviewDestroyPool("pool")

	require.NoError(t, err)
	assert.Equal(t, "pool", preview.Pool)
	assert.Equal(t, []string{"pool", "pool/clone", "pool/clone/child", "pool/data", "pool/home", "pool/home/user",
		"pool/plain-clone"}, preview.Datasets, "all datasets, the root dataset first")
	assert.Equal(t, uint64(8192), preview.Allocated)
	assert.Empty(t, b.calls)
}

func TestPreviewDestroyPool_Refuses(t *testing.T) {
	tests := map[string]struct {
		pool   string
		change func(b *actionBackend)
		err    string
	}{
		"unknown pool": {pool: "other", err: "does not exist"},
		"dataset":      {pool: "pool/data", err: "invalid pool name"},
		"option":       {pool: "-f", err: "invalid pool name"},
		"empty":        {pool: "", err: "invalid pool name"},
		// the running system would be destroyed, or zpool would fail to unmount it
		"root file system": {pool: "pool", err: "pool/home/user (/)",
			change: func(b *actionBackend) { b.mounts["pool/home/user"] = "/" }},
		"boot": {pool: "pool", err: "pool (/boot)",
			change: func(b *actionBackend) { b.mounts["pool"] = "/boot" }},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			b := cloneBackend()
			if test.change != nil {
				test.change(b)
			}
			useActionBackend(t, b)

			_, err := PreviewDestroyPool(test.pool)

			require.Error(t, err)
			assert.Contains(t, err.Error(), test.err)
			assert.Empty(t, b.calls)
		})
	}
}

func TestDestroyPool(t *testing.T) {
	b := useActionBackend(t, cloneBackend())
	preview, err := PreviewDestroyPool("pool")
	require.NoError(t, err)

	err = DestroyPool(preview)

	require.NoError(t, err)
	assert.Equal(t, []string{"destroy pool"}, b.calls, "never with -f")
}

func TestDestroyPool_RefusesChanges(t *testing.T) {
	b := useActionBackend(t, cloneBackend())
	preview, err := PreviewDestroyPool("pool")
	require.NoError(t, err)
	// e.g. a dataset created after the confirmation
	b.origins["pool/new"] = "-"

	err = DestroyPool(preview)

	assert.ErrorIs(t, err, ErrChangedSinceConfirmation)
	assert.Empty(t, b.calls)
}

func TestDestroyPool_ChecksAgain(t *testing.T) {
	b := useActionBackend(t, cloneBackend())
	preview, err := PreviewDestroyPool("pool")
	require.NoError(t, err)
	b.mounts["pool/data"] = "/"

	err = DestroyPool(preview)

	require.Error(t, err)
	assert.Empty(t, b.calls)
}

func TestDestroyPool_NotConfirmed(t *testing.T) {
	b := useActionBackend(t, cloneBackend())

	require.Error(t, DestroyPool(nil))
	assert.Empty(t, b.calls)
}

// The sudoers rule of the setup never allows destroying a pool: the root dialog only offers to run it once.
func TestPoolActionDestroy_NotAllowedBySetup(t *testing.T) {
	assert.NotContains(t, PoolActions, PoolActionDestroy)
	needsRoot := &PoolActionNeedsRootError{Action: PoolActionDestroy, Pool: "pool"}
	assert.False(t, needsRoot.AllowedBySetup())
	assert.Equal(t, [][]string{{ZpoolPath(), "destroy", "pool"}}, needsRoot.Commands())
}
