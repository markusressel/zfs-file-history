package zfs

import (
	"errors"
	"testing"

	gozfs "github.com/mistifyio/go-zfs/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testDelegations = `---- Permissions on pool/data/child --------------------------------
Local permissions:
	user alice clone
	user bob release
---- Permissions on pool/data ----------------------------------------
Permission sets:
	@backup hold,@nested
	@nested send
	@unused rollback
Create time permissions:
	destroy
Local permissions:
	user alice rename
Descendent permissions:
	group staff mount
Local+Descendent permissions:
	user alice @backup,snapshot
	user 1001 diff
	everyone userprop
`

var alice = identity{name: "alice", uid: "1001", groups: []string{"alice", "staff"}}

func TestEffectivePermissions(t *testing.T) {
	tests := []struct {
		name     string
		dataset  string
		who      identity
		expected []Permission
	}{
		{
			name:    "descendant dataset",
			dataset: "pool/data/child",
			who:     alice,
			// clone (local on child), mount (descendent of pool/data, via group), hold+send (nested sets),
			// snapshot, diff (by uid), userprop (everyone) - but not rename (local on pool/data only)
			expected: []Permission{"clone", "mount", "hold", "send", "snapshot", "diff", "userprop"},
		},
		{
			name:    "dataset itself",
			dataset: "pool/data",
			who:     alice,
			// rename is local, mount is descendent only, the create time permission destroy does not count,
			// and permissions of other datasets (child) do not apply
			expected: []Permission{"rename", "hold", "send", "snapshot", "diff", "userprop"},
		},
		{
			name:     "other user",
			dataset:  "pool/data/child",
			who:      identity{name: "bob", uid: "1002"},
			expected: []Permission{"release", "userprop"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := effectivePermissions(testDelegations, test.dataset, test.who)
			var granted []Permission
			for permission := range result {
				granted = append(granted, permission)
			}
			assert.ElementsMatch(t, test.expected, granted)
		})
	}
}

func TestEffectivePermissions_NoDelegations(t *testing.T) {
	assert.Empty(t, effectivePermissions("", "pool/data", alice))
}

func TestEffectivePermissions_OnlyAncestorsApply(t *testing.T) {
	output := "---- Permissions on pool/data2 ----\nLocal+Descendent permissions:\n\tuser alice hold\n"
	assert.Empty(t, effectivePermissions(output, "pool/data", alice), "pool/data2 is not an ancestor of pool/data")
}

func TestEffectivePermissions_SetCycle(t *testing.T) {
	output := "---- Permissions on pool ----\nPermission sets:\n\t@a hold,@b\n\t@b @a\nLocal+Descendent permissions:\n\tuser alice @a\n"
	assert.Equal(t, map[Permission]bool{"hold": true}, effectivePermissions(output, "pool/data", alice))
}

// stubPermissions replaces the current user and the delegations read from zfs.
func stubPermissions(t *testing.T, who identity, delegations string) *[]string {
	originalIdentity, originalRead := currentIdentity, readDelegations
	t.Cleanup(func() { currentIdentity, readDelegations = originalIdentity, originalRead })
	var read []string
	currentIdentity = func() (identity, error) { return who, nil }
	readDelegations = func(dataset string) (string, error) {
		read = append(read, dataset)
		return delegations, nil
	}
	return &read
}

func TestCheckPermissions(t *testing.T) {
	stubPermissions(t, alice, testDelegations)

	assert.NoError(t, checkPermissions("hold", PermissionGap{"pool/data/child", []Permission{PermissionHold, PermissionClone}}))

	err := checkPermissions("destroy the snapshot", PermissionGap{"pool/data/child", destroyPermissions})
	var missing *MissingPermissionsError
	require.ErrorAs(t, err, &missing)
	assert.Equal(t, &MissingPermissionsError{
		Action:  "destroy the snapshot",
		User:    "alice",
		Missing: []PermissionGap{{"pool/data/child", []Permission{PermissionDestroy}}},
	}, missing)
	assert.Equal(t, "cannot destroy the snapshot: user alice is missing the ZFS permissions destroy on pool/data/child", err.Error())
	assert.Equal(t, [][]string{{"zfs", "allow", "-u", "alice", "destroy", "pool/data/child"}}, missing.GrantCommands())
}

func TestCheckPermissions_RootIsNotChecked(t *testing.T) {
	read := stubPermissions(t, identity{name: "root", uid: "0"}, "")
	assert.NoError(t, checkPermissions("destroy the snapshot", PermissionGap{"pool/data", destroyPermissions}))
	assert.Empty(t, *read)
}

func TestCheckPermissions_UnknownDelegationsDoNotBlock(t *testing.T) {
	stubPermissions(t, alice, "")
	readDelegations = func(dataset string) (string, error) { return "", errors.New("zfs not found") }
	assert.NoError(t, checkPermissions("destroy the snapshot", PermissionGap{"pool/data", destroyPermissions}))
}

func TestExplainPermissionError(t *testing.T) {
	stubPermissions(t, alice, testDelegations)
	denied := errors.New("cannot release hold from snapshot 'pool/data/child@a': permission denied")
	required := PermissionGap{"pool/data/child", []Permission{PermissionRelease, PermissionHold}}

	err := explainPermissionError(denied, "release holds", required)

	var missing *MissingPermissionsError
	require.ErrorAs(t, err, &missing)
	assert.Equal(t, []PermissionGap{{"pool/data/child", []Permission{PermissionRelease}}}, missing.Missing)
	assert.ErrorIs(t, err, denied)
	assert.Contains(t, err.Error(), "(cannot release hold from snapshot 'pool/data/child@a': permission denied)")
}

func TestExplainPermissionError_AllRequiredIfNothingSeemsMissing(t *testing.T) {
	// e.g. delegation is disabled on the pool: the delegated permissions have no effect
	stubPermissions(t, alice, testDelegations)
	required := PermissionGap{"pool/data/child", []Permission{PermissionHold}}

	err := explainPermissionError(errors.New("permission denied"), "hold snapshots", required)

	var missing *MissingPermissionsError
	require.ErrorAs(t, err, &missing)
	assert.Equal(t, []PermissionGap{required}, missing.Missing)
}

func TestExplainPermissionError_OtherErrors(t *testing.T) {
	read := stubPermissions(t, alice, testDelegations)
	other := errors.New("dataset is busy")

	assert.Same(t, other, explainPermissionError(other, "destroy", PermissionGap{"pool/data", destroyPermissions}))
	assert.NoError(t, explainPermissionError(nil, "destroy", PermissionGap{"pool/data", destroyPermissions}))
	assert.Empty(t, *read, "delegations are only read for permission errors")
}

func TestSnapshotPermissionGaps(t *testing.T) {
	gaps := snapshotPermissionGaps([]string{"pool/a@1", "pool/a@2", "pool/b@1"}, PermissionHold)
	assert.Equal(t, []PermissionGap{
		{"pool/a", []Permission{PermissionHold}},
		{"pool/b", []Permission{PermissionHold}},
	}, gaps)
}

func TestCleanZfsError(t *testing.T) {
	zfsError := &gozfs.Error{
		Err:    errors.New("exit status 1"),
		Debug:  "/usr/bin/zfs snapshot pool@x",
		Stderr: "cannot create snapshots : permission denied\n",
	}
	assert.EqualError(t, cleanZfsError(zfsError), "cannot create snapshots : permission denied")

	other := errors.New("other")
	assert.Same(t, other, cleanZfsError(other))
	assert.NoError(t, cleanZfsError(nil))
}

func TestPreviewDestroySnapshots_ChecksPermissionsFirst(t *testing.T) {
	stubPermissions(t, alice, testDelegations)

	_, err := PreviewDestroySnapshots([]*Snapshot{{FullName: "pool/data@a"}, {FullName: "pool/data@b"}}, false, false)

	var missing *MissingPermissionsError
	require.ErrorAs(t, err, &missing, "the dry run is not started (it would succeed without permissions)")
	assert.Equal(t, "destroy the snapshots", missing.Action)
	assert.Equal(t, []PermissionGap{{"pool/data", []Permission{PermissionDestroy, PermissionMount}}}, missing.Missing)
}

func TestMissingPermissionsError_Verify(t *testing.T) {
	missing := &MissingPermissionsError{
		Action: "release holds",
		User:   "alice",
		Missing: []PermissionGap{
			{"pool/data/child", []Permission{PermissionHold, PermissionRelease}},
		},
	}

	// alice has hold, but not release
	stubPermissions(t, alice, testDelegations)
	err := missing.Verify()
	var stillMissing *MissingPermissionsError
	require.ErrorAs(t, err, &stillMissing)
	assert.Equal(t, []PermissionGap{{"pool/data/child", []Permission{PermissionRelease}}}, stillMissing.Missing)
	assert.EqualError(t, stillMissing.Cause, "the permissions are still missing")

	// after granting release
	stubPermissions(t, alice, testDelegations+"\tuser alice release\n")
	assert.NoError(t, missing.Verify())

	// root has all permissions
	stubPermissions(t, identity{name: "root", uid: "0"}, "")
	assert.NoError(t, missing.Verify())
}

func TestMissingPermissionsError_VerifyCannotReadDelegations(t *testing.T) {
	stubPermissions(t, alice, "")
	readDelegations = func(dataset string) (string, error) { return "", errors.New("zfs not found") }
	missing := &MissingPermissionsError{Missing: []PermissionGap{{"pool/data", []Permission{PermissionHold}}}}

	err := missing.Verify()

	assert.EqualError(t, err, "cannot check the permissions: zfs not found")
	var stillMissing *MissingPermissionsError
	assert.False(t, errors.As(err, &stillMissing), "unknown is not the same as missing")
}
