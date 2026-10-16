package zfs

import (
	"errors"
	"slices"
	"testing"
	"zfs-file-history/internal/testutil"

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
	// mounts are the paths the datasets are mounted at, by name
	mounts map[string]string
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

func (b *actionBackend) ListDatasets() ([]*DatasetListEntry, error) {
	var datasets []*DatasetListEntry
	for name := range b.origins {
		datasets = append(datasets, &DatasetListEntry{Name: name, MountPath: b.mounts[name]})
	}
	return datasets, nil
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
		panic("the preview of a dataset is always recursive")
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
	testutil.Replace[Backend](t, &backend, b)
	return b
}

// cloneBackend has the clone "pool/clone" of "pool/data@b", with a snapshot and a child dataset, and the dataset
// "pool/home", which is not a clone, with a child dataset. "pool/data" cannot be destroyed without "-R": its snapshots
// have clones.
func cloneBackend() *actionBackend {
	return &actionBackend{
		origins: map[string]string{"pool": "-", "pool/data": "-", "pool/clone": "pool/data@b", "pool/plain-clone": "pool/data@a",
			"pool/clone/child": "-", "pool/home": "-", "pool/home/user": "-"},
		mounts: map[string]string{"pool/clone": "/clone", "pool/clone/child": "/clone/child", "pool/home": "/home",
			"pool/home/user": "/home/user"},
		snapshots: map[string][]string{
			"pool/data":  {"c", "b", "a"},
			"pool/clone": {"x"},
		},
		destroyPreviews: map[string]*DestroyPreview{
			"pool/clone": {Destroyed: []string{"pool/clone/child", "pool/clone@x", "pool/clone"}, Reclaim: 300},
			// like zfs prints it: no reclaimed space for datasets
			"pool/plain-clone": {Destroyed: []string{"pool/plain-clone"}},
			"pool/home":        {Destroyed: []string{"pool/home/user", "pool/home"}},
		},
	}
}

func TestPreviewDestroyDataset(t *testing.T) {
	b := useActionBackend(t, cloneBackend())

	preview, err := PreviewDestroyDataset("pool/clone")

	require.NoError(t, err)
	assert.Equal(t, []string{"pool/clone/child", "pool/clone@x", "pool/clone"}, preview.Destroyed)
	assert.Empty(t, b.calls)
}

func TestPreviewDestroyDataset_Refuses(t *testing.T) {
	tests := map[string]struct {
		name   string
		change func(b *actionBackend)
		err    string
	}{
		"unknown dataset": {name: "pool/missing", err: "does not exist"},
		"root of a pool":  {name: "pool", err: "root dataset of a pool"},
		// the dry run fails, "-R" is never used
		"snapshots with clones": {name: "pool/data", err: "use '-R'"},
		// Regression: only clones could be destroyed, so nothing protected the datasets of the running system.
		"mounted at /": {name: "pool/home", err: "pool/home (/)",
			change: func(b *actionBackend) { b.mounts["pool/home"] = "/" }},
		"child mounted at a system path": {name: "pool/home", err: "pool/home/user (/var)",
			change: func(b *actionBackend) { b.mounts["pool/home/user"] = "/var" }},
		"child mounted below /boot": {name: "pool/home", err: "pool/home/user (/boot/efi)",
			change: func(b *actionBackend) { b.mounts["pool/home/user"] = "/boot/efi" }},
		"snapshot": {name: "pool/clone@x", err: "invalid dataset name"},
		"option":   {name: "-r", err: "invalid dataset name"},
		"empty":    {name: "", err: "invalid dataset name"},
		"dependent clones": {name: "pool/clone", err: "use '-R'",
			change: func(b *actionBackend) { delete(b.destroyPreviews, "pool/clone") }},
		"held snapshot": {name: "pool/clone", err: "pool/clone@x (keep)",
			change: func(b *actionBackend) { b.holds = []Hold{{Snapshot: "pool/clone@x", Tag: "keep"}} }},
		"missing permissions": {name: "pool/clone", err: "destroy",
			change: func(b *actionBackend) { b.identity = Identity{Name: "user", Uid: "1000"} }},
		// nothing outside of the dataset is ever destroyed, whatever the dry run lists
		"outside of the dataset": {name: "pool/clone", err: "'pool/clone2', which",
			change: func(b *actionBackend) {
				b.destroyPreviews["pool/clone"] = &DestroyPreview{Destroyed: []string{"pool/clone", "pool/clone2"}}
			}},
		"dataset not listed": {name: "pool/clone", err: "does not list",
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

			_, err := PreviewDestroyDataset(test.name)

			require.Error(t, err)
			assert.Contains(t, err.Error(), test.err)
			assert.Empty(t, b.calls)
		})
	}
}

func TestDestroyDataset(t *testing.T) {
	b := useActionBackend(t, cloneBackend())
	preview, err := PreviewDestroyDataset("pool/plain-clone")
	require.NoError(t, err)

	assert.Equal(t, uint64(4096), preview.Reclaim, "the space used by the clone")
	err = DestroyDataset("pool/plain-clone", false, preview)

	require.NoError(t, err)
	assert.Equal(t, []string{"destroy pool/plain-clone"}, b.calls)
}

// Datasets that are not clones are destroyed the same way, with their child datasets.
func TestDestroyDataset_NotAClone(t *testing.T) {
	b := useActionBackend(t, cloneBackend())
	preview, err := PreviewDestroyDataset("pool/home")
	require.NoError(t, err)
	assert.Equal(t, []string{"pool/home/user", "pool/home"}, preview.Destroyed)

	err = DestroyDataset("pool/home", true, preview)

	require.NoError(t, err)
	assert.Equal(t, []string{"destroy pool/home -r"}, b.calls)
}

func TestDestroyDataset_Recursive(t *testing.T) {
	b := useActionBackend(t, cloneBackend())
	preview, err := PreviewDestroyDataset("pool/clone")
	require.NoError(t, err)

	err = DestroyDataset("pool/clone", true, preview)

	require.NoError(t, err)
	assert.Equal(t, []string{"destroy pool/clone -r"}, b.calls)
}

func TestDestroyDataset_RefusesDescendantsWithoutRecursive(t *testing.T) {
	b := useActionBackend(t, cloneBackend())
	preview, err := PreviewDestroyDataset("pool/clone")
	require.NoError(t, err)

	err = DestroyDataset("pool/clone", false, preview)

	require.Error(t, err)
	assert.Empty(t, b.calls)
}

func TestDestroyDataset_RefusesChanges(t *testing.T) {
	b := useActionBackend(t, cloneBackend())
	preview, err := PreviewDestroyDataset("pool/clone")
	require.NoError(t, err)
	// e.g. a snapshot created after the confirmation
	b.destroyPreviews["pool/clone"] = &DestroyPreview{Destroyed: append(slices.Clone(preview.Destroyed), "pool/clone@new")}

	err = DestroyDataset("pool/clone", true, preview)

	assert.ErrorIs(t, err, ErrChangedSinceConfirmation)
	assert.Empty(t, b.calls)
}

func TestDestroyDataset_ChecksAgain(t *testing.T) {
	b := useActionBackend(t, cloneBackend())
	preview, err := PreviewDestroyDataset("pool/clone")
	require.NoError(t, err)
	b.holds = []Hold{{Snapshot: "pool/clone@x", Tag: "keep"}}

	err = DestroyDataset("pool/clone", true, preview)

	require.Error(t, err)
	assert.Empty(t, b.calls)
}

func TestDestroyDataset_NotConfirmed(t *testing.T) {
	b := useActionBackend(t, cloneBackend())

	err := DestroyDataset("pool/clone", true, nil)

	require.Error(t, err)
	assert.Empty(t, b.calls)
}

func TestDestroyDataset_ExplainsDeniedPermissions(t *testing.T) {
	b := cloneBackend()
	b.failChanges = errors.New("permission denied")
	useActionBackend(t, b)
	preview, err := PreviewDestroyDataset("pool/plain-clone")
	require.NoError(t, err)

	err = DestroyDataset("pool/plain-clone", false, preview)

	var missing *MissingPermissionsError
	require.ErrorAs(t, err, &missing)
	assert.Equal(t, "destroy the dataset", missing.Action)
}

func TestIsSystemPath(t *testing.T) {
	tests := map[string]bool{
		"/":           true,
		"/usr":        true,
		"/var/":       true,
		"/boot/efi":   true,
		"/home":       false,
		"/var/lib":    false,
		"/srvdata":    false,
		"/data/usr":   false,
		"/bootstrap":  false,
		"/mnt/backup": false,
	}
	for path, expected := range tests {
		assert.Equal(t, expected, isSystemPath(path), path)
	}
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
