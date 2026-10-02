package zfs

import (
	"errors"
	"fmt"
	"strings"
)

// Grantee is who permissions are delegated to: a user, a group, or everyone.
type Grantee struct {
	Type WhoType
	// Name is the name (or id) of the user or group, empty for everyone.
	Name string
}

// String describes the grantee like "zfs allow" does, e.g. "user alice", "group staff" or "everyone".
func (g Grantee) String() string {
	if g.Name == "" {
		return string(g.Type)
	}
	return string(g.Type) + " " + g.Name
}

// Grantee returns who the delegation is granted to.
func (d Delegation) Grantee() Grantee {
	return Grantee{Type: d.WhoType, Name: d.Who}
}

// DelegatedOn returns the permissions delegated to the grantee on the dataset itself (not inherited from ancestors),
// for the given scope. For ScopeLocalAndDescendent, a permission must be delegated both locally and to the
// descendants. Only permissions named directly are returned, not the contents of permission sets.
func (d *Delegations) DelegatedOn(dataset string, grantee Grantee, scope DelegationScope) map[Permission]bool {
	local := map[Permission]bool{}
	descendent := map[Permission]bool{}
	for _, entry := range d.Entries {
		if entry.Dataset != dataset || entry.Grantee() != grantee {
			continue
		}
		for _, name := range entry.Permissions {
			if strings.HasPrefix(name, "@") {
				continue
			}
			permission := Permission(name)
			switch entry.Scope {
			case ScopeLocal:
				local[permission] = true
			case ScopeDescendent:
				descendent[permission] = true
			case ScopeLocalAndDescendent:
				local[permission] = true
				descendent[permission] = true
			}
		}
	}

	switch scope {
	case ScopeLocal:
		return local
	case ScopeDescendent:
		return descendent
	}
	result := map[Permission]bool{}
	for permission := range local {
		if descendent[permission] {
			result[permission] = true
		}
	}
	return result
}

// DelegationChange delegates (allow) and revokes (unallow) permissions of a grantee on a dataset.
type DelegationChange struct {
	Dataset string
	Grantee Grantee
	Scope   DelegationScope
	Allow   []Permission
	Unallow []Permission
}

// IsEmpty returns whether the change does nothing.
func (c DelegationChange) IsEmpty() bool {
	return len(c.Allow) == 0 && len(c.Unallow) == 0
}

// Commands returns the zfs commands that apply the change (without "sudo"), e.g.
// "zfs allow -u alice hold,release pool/data" and "zfs unallow -l -g staff destroy pool/data".
func (c DelegationChange) Commands() [][]string {
	var commands [][]string
	if len(c.Allow) > 0 {
		commands = append(commands, c.command("allow", c.Allow))
	}
	if len(c.Unallow) > 0 {
		commands = append(commands, c.command("unallow", c.Unallow))
	}
	return commands
}

func (c DelegationChange) command(subcommand string, permissions []Permission) []string {
	command := []string{"zfs", subcommand}
	switch c.Scope {
	case ScopeLocal:
		command = append(command, "-l")
	case ScopeDescendent:
		command = append(command, "-d")
	}
	switch c.Grantee.Type {
	case WhoUser:
		command = append(command, "-u", c.Grantee.Name)
	case WhoGroup:
		command = append(command, "-g", c.Grantee.Name)
	case WhoEveryone:
		command = append(command, "-e")
	}
	return append(command, joinPermissions(permissions), c.Dataset)
}

// ErrPermissionDenied is returned by ApplyDelegationChange if the current user may not change the delegations
// (which needs root, or the "allow" permission and the delegated permissions themselves).
var ErrPermissionDenied = errors.New("permission denied")

// ApplyDelegationChange runs the commands of the change as the current user. If ZFS denies them, the returned error
// wraps ErrPermissionDenied, so the commands can be run with sudo instead.
// This spawns zfs processes, so it must not be called on the UI thread.
func ApplyDelegationChange(change DelegationChange) error {
	if err := validateGrantee(change.Grantee); err != nil {
		return err
	}
	for _, command := range change.Commands() {
		if _, err := runZfs(command[1:]...); err != nil {
			if isPermissionDenied(err) {
				return fmt.Errorf("%w: %s", ErrPermissionDenied, err.Error())
			}
			return err
		}
	}
	return nil
}

// validateGrantee rejects grantees that would make the zfs commands ambiguous.
func validateGrantee(grantee Grantee) error {
	switch grantee.Type {
	case WhoUser, WhoGroup:
		if grantee.Name == "" {
			return fmt.Errorf("the %s name must not be empty", grantee.Type)
		}
		for _, char := range grantee.Name {
			if char == ',' || char == ' ' || char == '\t' || char == '\n' {
				return fmt.Errorf("invalid %s name: %q", grantee.Type, grantee.Name)
			}
		}
	case WhoEveryone:
	default:
		return fmt.Errorf("permissions cannot be delegated to %s", grantee.Type)
	}
	return nil
}
