package zfs

import (
	"errors"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// actionBackend is a Backend for the tests of the dataset actions. Methods it does not implement panic (the embedded
// Backend is nil), so a test never reaches the system.
type actionBackend struct {
	Backend
	// identity is the current user, root if empty
	identity Identity
	// origins are the "origin" properties of the datasets, "-" if missing
	origins map[string]string
	// snapshots are the snapshot names of the datasets, the newest first
	snapshots map[string][]string
	// clones are the numbers of clones of the snapshots, by full name
	clones map[string]uint64
	holds  []Hold
	// destroyPreviews are the results of the dry runs, by dataset; a dataset without one fails like "-R" was needed
	destroyPreviews map[string]*DestroyPreview
	// calls records the calls that change something, e.g. "destroy pool/clone -r"
	calls []string
	// failChanges is returned by the calls that change something
	failChanges error
}

func (b *actionBackend) CurrentIdentity() (Identity, error) {
	if b.identity.Name == "" {
		return Identity{Name: "root", Uid: "0"}, nil
	}
	return b.identity, nil
}

func (b *actionBackend) Delegations(dataset string) (*Delegations, error) {
	return &Delegations{Dataset: dataset}, nil
}

func (b *actionBackend) DatasetProperties(name string) (map[string]string, error) {
	origin, ok := b.origins[name]
	if !ok {
		return nil, errors.New("dataset does not exist")
	}
	return map[string]string{propOrigin: origin, propUsed: "4096"}, nil
}

func (b *actionBackend) ListSnapshotNames(dataset string) ([]string, error) {
	return b.snapshots[dataset], nil
}

func (b *actionBackend) SnapshotProperties(dataset string, names []string) map[string]SnapshotProperties {
	result := map[string]SnapshotProperties{}
	for _, name := range names {
		result[name] = SnapshotProperties{Clones: b.clones[dataset+"@"+name]}
	}
	return result
}

func (b *actionBackend) ListHolds(snapshotNames []string, recursive bool) ([]Hold, error) {
	var result []Hold
	for _, hold := range b.holds {
		if slices.Contains(snapshotNames, hold.Snapshot) {
			result = append(result, hold)
		}
	}
	return result, nil
}

func (b *actionBackend) PreviewDestroyDataset(name string, recursive bool) (*DestroyPreview, error) {
	if !recursive {
		panic("the preview of a clone is always recursive")
	}
	preview, ok := b.destroyPreviews[name]
	if !ok {
		return nil, errors.New("filesystem has dependent clones\nuse '-R' to destroy the following datasets")
	}
	return preview, nil
}

func (b *actionBackend) record(call string) error {
	b.calls = append(b.calls, call)
	return b.failChanges
}

func (b *actionBackend) DestroyDataset(name string, recursive bool) error {
	if recursive {
		return b.record("destroy " + name + " -r")
	}
	return b.record("destroy " + name)
}

func (b *actionBackend) RollbackSnapshot(dataset string, name string, destroyNewer bool) error {
	if destroyNewer {
		return b.record("rollback " + dataset + "@" + name + " -r")
	}
	return b.record("rollback " + dataset + "@" + name)
}

func (b *actionBackend) PromoteDataset(name string) error {
	return b.record("promote " + name)
}

func (b *actionBackend) CreateSnapshot(dataset string, name string, recursive bool) error {
	if recursive {
		return b.record("snapshot " + dataset + "@" + name + " -r")
	}
	return b.record("snapshot " + dataset + "@" + name)
}

// useActionBackend replaces the backend with b for the test.
func useActionBackend(t *testing.T, b *actionBackend) *actionBackend {
	original := backend
	t.Cleanup(func() { SetBackend(original) })
	SetBackend(b)
	return b
}

// cloneBackend has the clone "pool/clone" of "pool/data@b", with a snapshot and a child dataset.
func cloneBackend() *actionBackend {
	return &actionBackend{
		origins: map[string]string{"pool/data": "-", "pool/clone": "pool/data@b", "pool/plain-clone": "pool/data@a"},
		snapshots: map[string][]string{
			"pool/data":  {"c", "b", "a"},
			"pool/clone": {"x"},
		},
		destroyPreviews: map[string]*DestroyPreview{
			"pool/clone": {Destroyed: []string{"pool/clone/child", "pool/clone@x", "pool/clone"}, Reclaim: 300},
			// like zfs prints it: no reclaimed space for datasets
			"pool/plain-clone": {Destroyed: []string{"pool/plain-clone"}},
		},
	}
}

func TestPreviewDestroyClone(t *testing.T) {
	b := useActionBackend(t, cloneBackend())

	preview, err := PreviewDestroyClone("pool/clone")

	require.NoError(t, err)
	assert.Equal(t, []string{"pool/clone/child", "pool/clone@x", "pool/clone"}, preview.Destroyed)
	assert.Empty(t, b.calls)
}

func TestPreviewDestroyClone_Refuses(t *testing.T) {
	tests := map[string]struct {
		name   string
		change func(b *actionBackend)
		err    string
	}{
		"not a clone":     {name: "pool/data", err: "'pool/data' is not a clone"},
		"no origin":       {name: "pool/other", change: func(b *actionBackend) { b.origins["pool/other"] = "" }, err: "is not a clone"},
		"unknown dataset": {name: "pool/missing", err: "does not exist"},
		"snapshot":        {name: "pool/clone@x", err: "invalid dataset name"},
		"option":          {name: "-r", err: "invalid dataset name"},
		"empty":           {name: "", err: "invalid dataset name"},
		"dependent clones": {name: "pool/clone", err: "use '-R'",
			change: func(b *actionBackend) { delete(b.destroyPreviews, "pool/clone") }},
		"held snapshot": {name: "pool/clone", err: "pool/clone@x (keep)",
			change: func(b *actionBackend) { b.holds = []Hold{{Snapshot: "pool/clone@x", Tag: "keep"}} }},
		"missing permissions": {name: "pool/clone", err: "destroy",
			change: func(b *actionBackend) { b.identity = Identity{Name: "user", Uid: "1000"} }},
		// nothing outside of the clone is ever destroyed, whatever the dry run lists
		"outside of the clone": {name: "pool/clone", err: "'pool/clone2', which",
			change: func(b *actionBackend) {
				b.destroyPreviews["pool/clone"] = &DestroyPreview{Destroyed: []string{"pool/clone", "pool/clone2"}}
			}},
		"clone not listed": {name: "pool/clone", err: "does not list",
			change: func(b *actionBackend) {
				b.destroyPreviews["pool/clone"] = &DestroyPreview{Destroyed: []string{"pool/clone@x"}}
			}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			b := cloneBackend()
			if test.change != nil {
				test.change(b)
			}
			useActionBackend(t, b)

			_, err := PreviewDestroyClone(test.name)

			require.Error(t, err)
			assert.Contains(t, err.Error(), test.err)
			assert.Empty(t, b.calls)
		})
	}
}

func TestDestroyClone(t *testing.T) {
	b := useActionBackend(t, cloneBackend())
	preview, err := PreviewDestroyClone("pool/plain-clone")
	require.NoError(t, err)

	assert.Equal(t, uint64(4096), preview.Reclaim, "the space used by the clone")
	err = DestroyClone("pool/plain-clone", false, preview)

	require.NoError(t, err)
	assert.Equal(t, []string{"destroy pool/plain-clone"}, b.calls)
}

func TestDestroyClone_Recursive(t *testing.T) {
	b := useActionBackend(t, cloneBackend())
	preview, err := PreviewDestroyClone("pool/clone")
	require.NoError(t, err)

	err = DestroyClone("pool/clone", true, preview)

	require.NoError(t, err)
	assert.Equal(t, []string{"destroy pool/clone -r"}, b.calls)
}

func TestDestroyClone_RefusesDescendantsWithoutRecursive(t *testing.T) {
	b := useActionBackend(t, cloneBackend())
	preview, err := PreviewDestroyClone("pool/clone")
	require.NoError(t, err)

	err = DestroyClone("pool/clone", false, preview)

	require.Error(t, err)
	assert.Empty(t, b.calls)
}

func TestDestroyClone_RefusesChanges(t *testing.T) {
	b := useActionBackend(t, cloneBackend())
	preview, err := PreviewDestroyClone("pool/clone")
	require.NoError(t, err)
	// e.g. a snapshot created after the confirmation
	b.destroyPreviews["pool/clone"] = &DestroyPreview{Destroyed: append(slices.Clone(preview.Destroyed), "pool/clone@new")}

	err = DestroyClone("pool/clone", true, preview)

	assert.ErrorIs(t, err, ErrChangedSinceConfirmation)
	assert.Empty(t, b.calls)
}

func TestDestroyClone_ChecksAgain(t *testing.T) {
	b := useActionBackend(t, cloneBackend())
	preview, err := PreviewDestroyClone("pool/clone")
	require.NoError(t, err)
	b.holds = []Hold{{Snapshot: "pool/clone@x", Tag: "keep"}}

	err = DestroyClone("pool/clone", true, preview)

	require.Error(t, err)
	assert.Empty(t, b.calls)
}

func TestDestroyClone_NotConfirmed(t *testing.T) {
	b := useActionBackend(t, cloneBackend())

	err := DestroyClone("pool/clone", true, nil)

	require.Error(t, err)
	assert.Empty(t, b.calls)
}

func TestDestroyClone_ExplainsDeniedPermissions(t *testing.T) {
	b := cloneBackend()
	b.failChanges = errors.New("permission denied")
	useActionBackend(t, b)
	preview, err := PreviewDestroyClone("pool/plain-clone")
	require.NoError(t, err)

	err = DestroyClone("pool/plain-clone", false, preview)

	var missing *MissingPermissionsError
	require.ErrorAs(t, err, &missing)
	assert.Equal(t, "destroy the clone", missing.Action)
}

func TestIsWithinDataset(t *testing.T) {
	tests := map[string]bool{
		"pool/clone":         true,
		"pool/clone/child":   true,
		"pool/clone@x":       true,
		"pool/clone/child@x": true,
		"pool/clone#mark":    true,
		"pool/clone2":        false,
		"pool/clone-x":       false,
		"pool":               false,
		"pool/data@x":        false,
	}
	for name, expected := range tests {
		assert.Equal(t, expected, isWithinDataset(name, "pool/clone"), name)
	}
}

func TestPreviewRollback(t *testing.T) {
	tests := map[string]struct {
		snapshot  string
		destroyed []string
	}{
		"latest": {snapshot: "pool/data@c"},
		"older":  {snapshot: "pool/data@a", destroyed: []string{"pool/data@c", "pool/data@b"}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			b := useActionBackend(t, cloneBackend())

			preview, err := PreviewRollback(test.snapshot)

			require.NoError(t, err)
			assert.Equal(t, test.snapshot, preview.Snapshot)
			assert.Equal(t, test.destroyed, preview.Destroyed)
			assert.Empty(t, b.calls)
		})
	}
}

func TestPreviewRollback_Refuses(t *testing.T) {
	tests := map[string]struct {
		snapshot string
		change   func(b *actionBackend)
		err      string
	}{
		"missing snapshot": {snapshot: "pool/data@gone", err: "does not exist"},
		"no snapshot":      {snapshot: "pool/data", err: "invalid snapshot name"},
		"option":           {snapshot: "-r@a", err: "invalid dataset name"},
		"newer clone": {snapshot: "pool/data@a", err: "pool/data@b",
			change: func(b *actionBackend) { b.clones = map[string]uint64{"pool/data@b": 1} }},
		"newer held": {snapshot: "pool/data@a", err: "pool/data@c (keep)",
			change: func(b *actionBackend) { b.holds = []Hold{{Snapshot: "pool/data@c", Tag: "keep"}} }},
		"missing permissions": {snapshot: "pool/data@a", err: "roll back",
			change: func(b *actionBackend) { b.identity = Identity{Name: "user", Uid: "1000"} }},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			b := cloneBackend()
			if test.change != nil {
				test.change(b)
			}
			useActionBackend(t, b)

			_, err := PreviewRollback(test.snapshot)

			require.Error(t, err)
			assert.Contains(t, err.Error(), test.err)
			assert.Empty(t, b.calls)
		})
	}
}

func TestRollback(t *testing.T) {
	tests := map[string]struct {
		snapshot string
		call     string
	}{
		// without -r, so a snapshot created in between makes it fail instead of being destroyed
		"latest": {snapshot: "pool/data@c", call: "rollback pool/data@c"},
		"older":  {snapshot: "pool/data@a", call: "rollback pool/data@a -r"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			b := useActionBackend(t, cloneBackend())
			preview, err := PreviewRollback(test.snapshot)
			require.NoError(t, err)

			err = Rollback(preview)

			require.NoError(t, err)
			assert.Equal(t, []string{test.call}, b.calls)
		})
	}
}

func TestRollback_RefusesChanges(t *testing.T) {
	b := useActionBackend(t, cloneBackend())
	preview, err := PreviewRollback("pool/data@a")
	require.NoError(t, err)
	b.snapshots["pool/data"] = []string{"d", "c", "b", "a"}

	err = Rollback(preview)

	assert.ErrorIs(t, err, ErrChangedSinceConfirmation)
	assert.Empty(t, b.calls)
}

func TestRollback_NotConfirmed(t *testing.T) {
	b := useActionBackend(t, cloneBackend())

	err := Rollback(nil)

	require.Error(t, err)
	assert.Empty(t, b.calls)
}

func TestPreviewPromote(t *testing.T) {
	b := useActionBackend(t, cloneBackend())

	preview, err := PreviewPromote("pool/clone")

	require.NoError(t, err)
	assert.Equal(t, &PromotePreview{
		Clone:  "pool/clone",
		Origin: "pool/data@b",
		Moved:  []string{"pool/data@b", "pool/data@a"},
	}, preview)
	assert.Empty(t, b.calls)
}

func TestPreviewPromote_RefusesNonClones(t *testing.T) {
	b := useActionBackend(t, cloneBackend())

	_, err := PreviewPromote("pool/data")

	require.Error(t, err)
	assert.Empty(t, b.calls)
}

func TestPromoteClone(t *testing.T) {
	b := useActionBackend(t, cloneBackend())

	require.NoError(t, PromoteClone("pool/clone"))
	require.Error(t, PromoteClone("pool/data"))

	assert.Equal(t, []string{"promote pool/clone"}, b.calls)
}

func TestCreateSnapshot(t *testing.T) {
	b := useActionBackend(t, cloneBackend())

	require.NoError(t, CreateSnapshot("pool/data", "now", false))
	require.NoError(t, CreateSnapshot("pool/data", "now", true))
	require.Error(t, CreateSnapshot("pool/data", "-r", false))
	require.Error(t, CreateSnapshot("pool/data", "a@b", false))
	require.Error(t, CreateSnapshot("-r", "now", false))

	assert.Equal(t, []string{"snapshot pool/data@now", "snapshot pool/data@now -r"}, b.calls)
}

func TestSystemArgs(t *testing.T) {
	// the dry run must never destroy anything, and -R (which destroys clones elsewhere) is never used
	assert.Equal(t, []string{"destroy", "-n", "-v", "-p", "-r", "pool/clone"}, destroyDatasetArgs("pool/clone", true, true))
	assert.Equal(t, []string{"destroy", "-n", "-v", "-p", "pool/clone"}, destroyDatasetArgs("pool/clone", false, true))
	assert.Equal(t, []string{"destroy", "-r", "pool/clone"}, destroyDatasetArgs("pool/clone", true, false))
	assert.Equal(t, []string{"destroy", "pool/clone"}, destroyDatasetArgs("pool/clone", false, false))
	assert.Equal(t, []string{"rollback", "-r", "pool/data@a"}, rollbackArgs("pool/data", "a", true))
	assert.Equal(t, []string{"rollback", "pool/data@a"}, rollbackArgs("pool/data", "a", false))
}
