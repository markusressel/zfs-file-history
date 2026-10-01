package zfs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDestroyPreview(t *testing.T) {
	preview, err := parseDestroyPreview("destroy\trpool/home@2026-10-01-220000\nreclaim\t131600384\n")
	require.NoError(t, err)
	assert.Equal(t, []string{"rpool/home@2026-10-01-220000"}, preview.Destroyed)
	assert.Equal(t, uint64(131600384), preview.Reclaim)

	// several snapshots, e.g. recursive or combined
	preview, err = parseDestroyPreview("destroy\trpool/home@a\ndestroy\trpool/home/child@a\ndestroy\trpool/clone\nreclaim\t447066112\n")
	require.NoError(t, err)
	assert.Equal(t, []string{"rpool/home@a", "rpool/home/child@a", "rpool/clone"}, preview.Destroyed)
	assert.Equal(t, uint64(447066112), preview.Reclaim)

	// nothing to destroy, or unexpected output
	_, err = parseDestroyPreview("")
	assert.Error(t, err)
	_, err = parseDestroyPreview("destroy\trpool/home@a\n")
	assert.Error(t, err, "reclaim is missing")
	_, err = parseDestroyPreview("destroy\trpool/home@a\nreclaim\tlots\n")
	assert.Error(t, err)
}

func TestDestroyTarget(t *testing.T) {
	a := &Snapshot{Name: "a", FullName: "pool/data@a"}
	b := &Snapshot{Name: "b", FullName: "pool/data@b"}
	other := &Snapshot{Name: "c", FullName: "pool/other@c"}

	target, err := destroyTarget([]*Snapshot{a})
	require.NoError(t, err)
	assert.Equal(t, "pool/data@a", target)

	// several snapshots of the same dataset are destroyed together
	target, err = destroyTarget([]*Snapshot{a, b})
	require.NoError(t, err)
	assert.Equal(t, "pool/data@a,b", target)

	_, err = destroyTarget([]*Snapshot{a, other})
	assert.Error(t, err)
	_, err = destroyTarget(nil)
	assert.Error(t, err)
	_, err = destroyTarget([]*Snapshot{{FullName: "pool/data"}})
	assert.Error(t, err)
}
