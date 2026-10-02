package zfs

import (
	"slices"
	"strings"
)

// DelegationScope defines which datasets a delegation applies to, relative to the dataset it is set on.
type DelegationScope string

const (
	// ScopeLocal applies to the dataset itself ("zfs allow -l").
	ScopeLocal DelegationScope = "local"
	// ScopeDescendent applies to the descendants of the dataset ("zfs allow -d").
	ScopeDescendent DelegationScope = "descendent"
	// ScopeLocalAndDescendent applies to the dataset and its descendants (default of "zfs allow").
	ScopeLocalAndDescendent DelegationScope = "local+descendent"
	// ScopeCreateTime is granted to the creator of a new descendant ("zfs allow -c").
	ScopeCreateTime DelegationScope = "create time"
)

// sectionScopes maps the section titles of "zfs allow" to scopes.
var sectionScopes = map[string]DelegationScope{
	"Local permissions":            ScopeLocal,
	"Descendent permissions":       ScopeDescendent,
	"Local+Descendent permissions": ScopeLocalAndDescendent,
	"Create time permissions":      ScopeCreateTime,
}

// WhoType is the kind of principal a delegation is granted to.
type WhoType string

const (
	WhoUser     WhoType = "user"
	WhoGroup    WhoType = "group"
	WhoEveryone WhoType = "everyone"
	// WhoCreator is the creator of a new descendant, see ScopeCreateTime.
	WhoCreator WhoType = "creator"
)

// Delegation is a set of permissions delegated to a principal on a dataset ("zfs allow").
type Delegation struct {
	// Dataset is the dataset the delegation is set on.
	Dataset string
	Scope   DelegationScope
	WhoType WhoType
	// Who is the name (or id) of the user or group, empty for everyone and the creator.
	Who string
	// Permissions are permission names (e.g. "snapshot") and permission sets (e.g. "@backup").
	Permissions []string
}

// PermissionSet is a named set of permissions ("zfs allow -s @name"), usable on its dataset and the descendants.
type PermissionSet struct {
	Dataset     string
	Name        string
	Permissions []string
}

// Delegations are the delegated permissions of a dataset and its ancestors, as listed by "zfs allow <dataset>".
type Delegations struct {
	// Dataset is the dataset the delegations were listed for.
	Dataset string
	// Entries are ordered like "zfs allow" lists them: the dataset first, then its ancestors.
	Entries []Delegation
	Sets    []PermissionSet
}

// Grant is a delegation that grants a permission to a user, possibly through a permission set.
type Grant struct {
	Delegation Delegation
	// Set is the permission set the permission is part of, empty if the delegation names the permission directly.
	Set string
}

// parseDelegations parses the output of "zfs allow <dataset>":
//
//	---- Permissions on pool/data ----------------------------------------
//	Permission sets:
//		@backup hold,send
//	Create time permissions:
//		destroy
//	Local permissions:
//		user alice snapshot
//	Descendent permissions:
//		group staff mount
//	Local+Descendent permissions:
//		user alice @backup,destroy
//		everyone mount
//
// The output contains a block for the dataset and for each ancestor with delegations. Blocks of other datasets
// (which "zfs allow" never lists) are ignored.
func parseDelegations(output string, dataset string) *Delegations {
	result := &Delegations{Dataset: dataset}
	blockDataset := ""
	section := ""
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			continue
		case strings.HasPrefix(trimmed, "---- Permissions on "):
			blockDataset = strings.Fields(strings.TrimPrefix(trimmed, "---- Permissions on "))[0]
			section = ""
			continue
		case strings.HasSuffix(trimmed, ":") && !strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, " "):
			section = strings.TrimSuffix(trimmed, ":")
			continue
		}
		if blockDataset != dataset && !strings.HasPrefix(dataset, blockDataset+"/") {
			continue
		}

		fields := strings.Fields(trimmed)
		if section == "Permission sets" {
			if len(fields) == 2 && strings.HasPrefix(fields[0], "@") {
				result.Sets = append(result.Sets, PermissionSet{Dataset: blockDataset, Name: fields[0], Permissions: strings.Split(fields[1], ",")})
			}
			continue
		}
		scope, ok := sectionScopes[section]
		if !ok {
			continue
		}

		delegation := Delegation{Dataset: blockDataset, Scope: scope}
		switch {
		case scope == ScopeCreateTime && len(fields) == 1:
			delegation.WhoType = WhoCreator
			delegation.Permissions = strings.Split(fields[0], ",")
		case len(fields) == 2 && fields[0] == "everyone":
			delegation.WhoType = WhoEveryone
			delegation.Permissions = strings.Split(fields[1], ",")
		case len(fields) == 3 && (fields[0] == "user" || fields[0] == "group"):
			delegation.WhoType = WhoType(fields[0])
			delegation.Who = fields[1]
			delegation.Permissions = strings.Split(fields[2], ",")
		default:
			continue
		}
		result.Entries = append(result.Entries, delegation)
	}
	return result
}

// appliesTo returns whether the delegation applies to the user on the dataset the delegations were listed for.
func (d *Delegations) appliesTo(delegation Delegation, who identity) bool {
	isSelf := delegation.Dataset == d.Dataset
	switch delegation.Scope {
	case ScopeLocal:
		if !isSelf {
			return false
		}
	case ScopeDescendent:
		if isSelf {
			return false
		}
	case ScopeLocalAndDescendent:
	default:
		return false
	}

	switch delegation.WhoType {
	case WhoEveryone:
		return true
	case WhoUser:
		return delegation.Who == who.name || delegation.Who == who.uid
	case WhoGroup:
		return slices.Contains(who.groups, delegation.Who)
	}
	return false
}

// grants returns the permissions the user has on the dataset, with the delegations granting them.
func (d *Delegations) grants(who identity) map[Permission][]Grant {
	result := map[Permission][]Grant{}
	var add func(names []string, grant Grant, depth int)
	add = func(names []string, grant Grant, depth int) {
		for _, name := range names {
			if strings.HasPrefix(name, "@") {
				// sets may contain sets, the depth guards against cycles
				if depth >= 10 {
					continue
				}
				viaSet := grant
				if viaSet.Set == "" {
					viaSet.Set = name
				}
				for _, set := range d.Sets {
					if set.Name == name {
						add(set.Permissions, viaSet, depth+1)
					}
				}
				continue
			}
			permission := Permission(name)
			if !slices.ContainsFunc(result[permission], func(g Grant) bool {
				return g.Delegation.Dataset == grant.Delegation.Dataset && g.Delegation.Grantee() == grant.Delegation.Grantee() && g.Delegation.Scope == grant.Delegation.Scope
			}) {
				result[permission] = append(result[permission], grant)
			}
		}
	}
	for _, delegation := range d.Entries {
		if d.appliesTo(delegation, who) {
			add(delegation.Permissions, Grant{Delegation: delegation}, 0)
		}
	}
	return result
}

// DatasetPermissions are the delegations of a dataset and what the current user may do on it.
type DatasetPermissions struct {
	Dataset string
	// User is the name of the current user.
	User string
	// IsRoot is set for root, who has all permissions without delegations.
	IsRoot      bool
	Delegations *Delegations
	// Grants maps the permissions the user has to the delegations granting them (empty for root).
	Grants map[Permission][]Grant
}

// Has returns whether the user has the permission on the dataset.
func (p *DatasetPermissions) Has(permission Permission) bool {
	return p.IsRoot || len(p.Grants[permission]) > 0
}

// KnownGranted returns the known permissions (see KnownPermissions) the user has, in display order.
func (p *DatasetPermissions) KnownGranted() []Permission {
	var result []Permission
	for _, info := range KnownPermissions {
		if p.Has(info.Permission) {
			result = append(result, info.Permission)
		}
	}
	return result
}

// IsCurrentUserRoot returns whether the application runs as root, who has all permissions without delegations.
func IsCurrentUserRoot() bool {
	who, err := currentIdentity()
	return err == nil && who.isRoot()
}

// LoadDatasetPermissions reads the delegations of the dataset ("zfs allow") and determines what the current user may
// do on it. This spawns a zfs process, so it must not be called on the UI thread.
func LoadDatasetPermissions(dataset string) (*DatasetPermissions, error) {
	who, err := currentIdentity()
	if err != nil {
		return nil, err
	}
	output, err := readDelegations(dataset)
	if err != nil {
		return nil, err
	}
	delegations := parseDelegations(output, dataset)
	return &DatasetPermissions{
		Dataset:     dataset,
		User:        who.name,
		IsRoot:      who.isRoot(),
		Delegations: delegations,
		Grants:      delegations.grants(who),
	}, nil
}

// PermissionInfo describes a permission for display.
type PermissionInfo struct {
	Permission Permission
	// Abbreviation is the letter shown in compact permission masks.
	Abbreviation rune
	Description  string
}

// KnownPermissions are the permissions relevant for zfs-file-history, in display order.
var KnownPermissions = []PermissionInfo{
	{PermissionSnapshot, 's', "Create snapshots (also needs mount)"},
	{PermissionDestroy, 'd', "Destroy snapshots and datasets (also needs mount)"},
	{PermissionMount, 'm', "Mount and unmount datasets, needed by most other permissions"},
	{PermissionHold, 'h', "Hold snapshots, to protect them from being destroyed"},
	{PermissionRelease, 'r', "Release holds, which might destroy a snapshot marked for deferred destruction"},
	{PermissionClone, 'c', "Clone snapshots (also needs create and mount on the target's parent)"},
	{PermissionCreate, 'n', "Create new descendant datasets (also needs mount)"},
	{PermissionRollback, 'b', "Roll back to a snapshot (also needs mount)"},
	{PermissionDiff, 'f', "Show the changed files of a snapshot (zfs diff)"},
}
