package zfs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDelegations(t *testing.T) {
	delegations := parseDelegations(testDelegations, "pool/data/child")

	assert.Equal(t, "pool/data/child", delegations.Dataset)
	assert.Equal(t, []PermissionSet{
		{Dataset: "pool/data", Name: "@backup", Permissions: []string{"hold", "@nested"}},
		{Dataset: "pool/data", Name: "@nested", Permissions: []string{"send"}},
		{Dataset: "pool/data", Name: "@unused", Permissions: []string{"rollback"}},
	}, delegations.Sets)
	assert.Equal(t, []Delegation{
		{Dataset: "pool/data/child", Scope: ScopeLocal, WhoType: WhoUser, Who: "alice", Permissions: []string{"clone"}},
		{Dataset: "pool/data/child", Scope: ScopeLocal, WhoType: WhoUser, Who: "bob", Permissions: []string{"release"}},
		{Dataset: "pool/data", Scope: ScopeCreateTime, WhoType: WhoCreator, Permissions: []string{"destroy"}},
		{Dataset: "pool/data", Scope: ScopeLocal, WhoType: WhoUser, Who: "alice", Permissions: []string{"rename"}},
		{Dataset: "pool/data", Scope: ScopeDescendent, WhoType: WhoGroup, Who: "staff", Permissions: []string{"mount"}},
		{Dataset: "pool/data", Scope: ScopeLocalAndDescendent, WhoType: WhoUser, Who: "alice", Permissions: []string{"@backup", "snapshot"}},
		{Dataset: "pool/data", Scope: ScopeLocalAndDescendent, WhoType: WhoUser, Who: "1001", Permissions: []string{"diff"}},
		{Dataset: "pool/data", Scope: ScopeLocalAndDescendent, WhoType: WhoEveryone, Permissions: []string{"userprop"}},
	}, delegations.Entries)
}

func TestDelegations_Grants(t *testing.T) {
	grants := parseDelegations(testDelegations, "pool/data/child").grants(alice)

	backup := Delegation{Dataset: "pool/data", Scope: ScopeLocalAndDescendent, WhoType: WhoUser, Who: "alice", Permissions: []string{"@backup", "snapshot"}}
	assert.Equal(t, []Grant{{Delegation: backup, Set: "@backup"}}, grants[PermissionHold])
	assert.Equal(t, []Grant{{Delegation: backup, Set: "@backup"}}, grants["send"], "nested sets name the outermost set")
	assert.Equal(t, []Grant{{Delegation: backup}}, grants[PermissionSnapshot])
	assert.Equal(t, []Grant{{Delegation: Delegation{Dataset: "pool/data", Scope: ScopeDescendent, WhoType: WhoGroup, Who: "staff", Permissions: []string{"mount"}}}}, grants[PermissionMount])
	assert.NotContains(t, grants, PermissionDestroy, "create time permissions are not granted")
	assert.NotContains(t, grants, PermissionRelease, "granted to bob")
}

func TestLoadDatasetPermissions(t *testing.T) {
	read := stubPermissions(t, alice, testDelegations)

	permissions, err := LoadDatasetPermissions("pool/data/child")

	require.NoError(t, err)
	assert.Equal(t, []string{"pool/data/child"}, *read)
	assert.Equal(t, "alice", permissions.User)
	assert.False(t, permissions.IsRoot)
	assert.True(t, permissions.Has(PermissionClone))
	assert.False(t, permissions.Has(PermissionDestroy))
	assert.Len(t, permissions.Delegations.Entries, 8)
}

func TestDatasetPermissions_RootHasAll(t *testing.T) {
	stubPermissions(t, identity{name: "root", uid: "0"}, "")

	permissions, err := LoadDatasetPermissions("pool/data")

	require.NoError(t, err)
	assert.True(t, permissions.IsRoot)
	for _, info := range KnownPermissions {
		assert.True(t, permissions.Has(info.Permission), info.Permission)
	}
}

func TestKnownPermissions_UniqueAbbreviations(t *testing.T) {
	seen := map[rune]bool{}
	for _, info := range KnownPermissions {
		assert.False(t, seen[info.Abbreviation], "duplicate abbreviation %c", info.Abbreviation)
		seen[info.Abbreviation] = true
		assert.NotEmpty(t, info.Description)
	}
}

func TestDatasetPermissions_KnownGranted(t *testing.T) {
	permissions := &DatasetPermissions{Grants: map[Permission][]Grant{PermissionHold: {{}}, PermissionSnapshot: {{}}, "send": {{}}}}
	assert.Equal(t, []Permission{PermissionSnapshot, PermissionHold}, permissions.KnownGranted())
	assert.Len(t, (&DatasetPermissions{IsRoot: true}).KnownGranted(), len(KnownPermissions))
	assert.Empty(t, (&DatasetPermissions{}).KnownGranted())
}
