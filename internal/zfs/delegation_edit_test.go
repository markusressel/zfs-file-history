package zfs

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const editDelegations = `---- Permissions on pool/data ----------------------------------------
Permission sets:
	@backup hold,send
Create time permissions:
	destroy
Local permissions:
	user alice snapshot,mount
	group staff diff
Descendent permissions:
	user alice destroy,mount
Local+Descendent permissions:
	user alice hold,@backup
	everyone userprop
---- Permissions on pool --------------------------------------------
Local+Descendent permissions:
	user alice release
	user bob clone
`

func TestGrantee(t *testing.T) {
	assert.Equal(t, "user alice", Grantee{WhoUser, "alice"}.String())
	assert.Equal(t, "group staff", Grantee{WhoGroup, "staff"}.String())
	assert.Equal(t, "everyone", Grantee{Type: WhoEveryone}.String())
	assert.Equal(t, Grantee{WhoUser, "alice"}, Delegation{WhoType: WhoUser, Who: "alice"}.Grantee())
}

func TestDelegations_DelegatedOn(t *testing.T) {
	delegations := parseDelegations(editDelegations, "pool/data")
	alice := Grantee{WhoUser, "alice"}

	assert.Equal(t, map[Permission]bool{"snapshot": true, "mount": true, "hold": true},
		delegations.DelegatedOn("pool/data", alice, ScopeLocal), "sets are not expanded")
	assert.Equal(t, map[Permission]bool{"destroy": true, "mount": true, "hold": true},
		delegations.DelegatedOn("pool/data", alice, ScopeDescendent))
	assert.Equal(t, map[Permission]bool{"mount": true, "hold": true},
		delegations.DelegatedOn("pool/data", alice, ScopeLocalAndDescendent), "both local and descendent")
	assert.Empty(t, delegations.DelegatedOn("pool/data", Grantee{WhoUser, "bob"}, ScopeLocalAndDescendent),
		"inherited from pool, not delegated on pool/data")
	assert.Equal(t, map[Permission]bool{"release": true},
		delegations.DelegatedOn("pool", alice, ScopeLocalAndDescendent))
}

func TestDelegationChange_Commands(t *testing.T) {
	change := DelegationChange{
		Dataset: "pool/data",
		Grantee: Grantee{WhoUser, "alice"},
		Scope:   ScopeLocalAndDescendent,
		Allow:   []Permission{PermissionHold, PermissionRelease},
		Unallow: []Permission{PermissionDestroy},
	}
	assert.Equal(t, [][]string{
		{"zfs", "allow", "-u", "alice", "hold,release", "pool/data"},
		{"zfs", "unallow", "-u", "alice", "destroy", "pool/data"},
	}, change.Commands())

	change.Scope = ScopeLocal
	change.Grantee = Grantee{WhoGroup, "staff"}
	change.Allow = nil
	assert.Equal(t, [][]string{{"zfs", "unallow", "-l", "-g", "staff", "destroy", "pool/data"}}, change.Commands())

	change.Scope = ScopeDescendent
	change.Grantee = Grantee{Type: WhoEveryone}
	change.Allow, change.Unallow = []Permission{PermissionMount}, nil
	assert.Equal(t, [][]string{{"zfs", "allow", "-d", "-e", "mount", "pool/data"}}, change.Commands())

	assert.True(t, DelegationChange{Dataset: "pool/data"}.IsEmpty())
	assert.Empty(t, DelegationChange{Dataset: "pool/data"}.Commands())
}

func TestApplyDelegationChange_InvalidGrantee(t *testing.T) {
	tests := []Grantee{
		{WhoUser, ""},
		{WhoGroup, "a,b"},
		{WhoUser, "two words"},
		{Type: WhoCreator},
	}
	for _, grantee := range tests {
		t.Run(grantee.String(), func(t *testing.T) {
			err := ApplyDelegationChange(DelegationChange{Dataset: "pool/data", Grantee: grantee, Allow: []Permission{PermissionHold}})
			require.Error(t, err)
			assert.False(t, errors.Is(err, ErrPermissionDenied))
		})
	}
}
