package zfs

import (
	"errors"
	"fmt"
	"os/user"
	"slices"
	"strings"

	gozfs "github.com/mistifyio/go-zfs/v4"
)

// Permission is a ZFS permission that can be delegated to users with "zfs allow".
type Permission string

const (
	PermissionClone    Permission = "clone"
	PermissionCreate   Permission = "create"
	PermissionDestroy  Permission = "destroy"
	PermissionDiff     Permission = "diff"
	PermissionHold     Permission = "hold"
	PermissionMount    Permission = "mount"
	PermissionRelease  Permission = "release"
	PermissionRollback Permission = "rollback"
	PermissionSnapshot Permission = "snapshot"
)

// PermissionGap is a set of permissions on a dataset that an operation needs.
type PermissionGap struct {
	Dataset     string
	Permissions []Permission
}

// MissingPermissionsError is returned when the current user lacks ZFS permissions for an operation.
type MissingPermissionsError struct {
	// Action describes the operation, e.g. "destroy snapshots"
	Action string
	// User is the name of the current user, who needs the permissions
	User string
	// Missing are the permissions the user lacks. If they cannot be determined (e.g. because ZFS denied an
	// operation that the delegated permissions should allow), these are all permissions the operation needs.
	Missing []PermissionGap
	// Cause is the error of the denied zfs command, or nil if the permissions were checked before running it.
	Cause error
}

func (e *MissingPermissionsError) Error() string {
	var gaps []string
	for _, gap := range e.Missing {
		gaps = append(gaps, fmt.Sprintf("%s on %s", joinPermissions(gap.Permissions), gap.Dataset))
	}
	message := fmt.Sprintf("cannot %s: user %s is missing the ZFS permissions %s", e.Action, e.User, strings.Join(gaps, ", "))
	if e.Cause != nil {
		message += fmt.Sprintf(" (%s)", e.Cause.Error())
	}
	return message
}

// Verify checks whether the current user has the missing permissions now, e.g. after granting them.
// Returns nil if so, or a MissingPermissionsError with the permissions that are still missing.
// This spawns zfs processes, so it must not be called on the UI thread.
func (e *MissingPermissionsError) Verify() error {
	who, err := currentIdentity()
	if err != nil {
		return fmt.Errorf("cannot check the permissions: %w", err)
	}
	if who.isRoot() {
		return nil
	}
	stillMissing, err := findMissingPermissions(who, e.Missing)
	if err != nil {
		return fmt.Errorf("cannot check the permissions: %w", err)
	}
	if len(stillMissing) == 0 {
		return nil
	}
	return &MissingPermissionsError{
		Action:  e.Action,
		User:    who.name,
		Missing: stillMissing,
		Cause:   errors.New("the permissions are still missing"),
	}
}

func (e *MissingPermissionsError) Unwrap() error {
	return e.Cause
}

// GrantCommands returns the commands that delegate the missing permissions to the user, without "sudo".
func (e *MissingPermissionsError) GrantCommands() [][]string {
	var commands [][]string
	for _, gap := range e.Missing {
		commands = append(commands, []string{"zfs", "allow", "-u", e.User, joinPermissions(gap.Permissions), gap.Dataset})
	}
	return commands
}

func joinPermissions(permissions []Permission) string {
	names := make([]string, 0, len(permissions))
	for _, permission := range permissions {
		names = append(names, string(permission))
	}
	return strings.Join(names, ",")
}

// identity is the user the permissions are checked for.
type identity struct {
	name   string
	uid    string
	groups []string
}

func (i identity) isRoot() bool {
	return i.uid == "0"
}

// Replaceable in tests.
var (
	currentIdentity = func() (identity, error) {
		current, err := user.Current()
		if err != nil {
			return identity{}, err
		}
		result := identity{name: current.Username, uid: current.Uid}
		groupIds, err := current.GroupIds()
		if err == nil {
			for _, groupId := range groupIds {
				if group, err := user.LookupGroupId(groupId); err == nil {
					result.groups = append(result.groups, group.Name)
				}
			}
		}
		return result, nil
	}
	// readDelegations returns the output of "zfs allow <dataset>", which lists the delegated permissions of the
	// dataset and its ancestors. Anyone can read them.
	readDelegations = func(dataset string) (string, error) {
		return runZfs("allow", dataset)
	}
)

// checkPermissions returns a MissingPermissionsError if the current user lacks one of the required permissions.
// Used before operations that would otherwise appear to work, e.g. the dry run of a destroy succeeds without
// permissions. If the permissions cannot be determined, nil is returned, so the operation itself reports the error.
// This spawns zfs processes, so it must not be called on the UI thread.
func checkPermissions(action string, required ...PermissionGap) error {
	who, err := currentIdentity()
	if err != nil || who.isRoot() {
		return nil
	}
	missing, err := findMissingPermissions(who, required)
	if err != nil || len(missing) == 0 {
		return nil
	}
	return &MissingPermissionsError{Action: action, User: who.name, Missing: missing}
}

// explainPermissionError turns err into a MissingPermissionsError if ZFS denied the operation because of missing
// permissions, listing the required permissions the current user lacks. Other errors are returned unchanged.
// This spawns zfs processes, so it must not be called on the UI thread.
func explainPermissionError(err error, action string, required ...PermissionGap) error {
	if err == nil || !isPermissionDenied(err) {
		return err
	}
	who, identityErr := currentIdentity()
	if identityErr != nil {
		return err
	}
	missing, checkErr := findMissingPermissions(who, required)
	if checkErr != nil || len(missing) == 0 {
		// e.g. delegation is disabled on the pool, or an unknown requirement: name everything that is needed
		missing = required
	}
	return &MissingPermissionsError{Action: action, User: who.name, Missing: missing, Cause: err}
}

// isPermissionDenied returns whether a zfs command failed because of missing permissions.
func isPermissionDenied(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "permission denied") || strings.Contains(message, "operation not permitted")
}

// findMissingPermissions returns the required permissions the user does not have.
func findMissingPermissions(who identity, required []PermissionGap) ([]PermissionGap, error) {
	var missing []PermissionGap
	for _, gap := range required {
		output, err := readDelegations(gap.Dataset)
		if err != nil {
			return nil, err
		}
		granted := effectivePermissions(output, gap.Dataset, who)
		var lacking []Permission
		for _, permission := range gap.Permissions {
			if !granted[permission] {
				lacking = append(lacking, permission)
			}
		}
		if len(lacking) > 0 {
			missing = append(missing, PermissionGap{Dataset: gap.Dataset, Permissions: lacking})
		}
	}
	return missing, nil
}

// effectivePermissions returns the permissions the user has on dataset, given the output of "zfs allow <dataset>".
func effectivePermissions(output string, dataset string, who identity) map[Permission]bool {
	result := map[Permission]bool{}
	for permission := range parseDelegations(output, dataset).grants(who) {
		result[permission] = true
	}
	return result
}

// datasetOfSnapshot returns the dataset name of a full snapshot name, e.g. "pool/data" for "pool/data@daily".
func datasetOfSnapshot(fullName string) string {
	dataset, _, _ := strings.Cut(fullName, "@")
	return dataset
}

// snapshotPermissionGaps returns the given permissions on the datasets of the given snapshots.
func snapshotPermissionGaps(snapshotNames []string, permissions ...Permission) []PermissionGap {
	var gaps []PermissionGap
	for _, name := range snapshotNames {
		dataset := datasetOfSnapshot(name)
		if !slices.ContainsFunc(gaps, func(gap PermissionGap) bool { return gap.Dataset == dataset }) {
			gaps = append(gaps, PermissionGap{Dataset: dataset, Permissions: permissions})
		}
	}
	return gaps
}

// cleanZfsError reduces errors of go-zfs to the message zfs printed. go-zfs includes the exit status and the full
// command line, e.g. `exit status 1: "/usr/bin/zfs snapshot pool@x" => cannot create snapshots : permission denied`.
func cleanZfsError(err error) error {
	var zfsError *gozfs.Error
	if errors.As(err, &zfsError) && strings.TrimSpace(zfsError.Stderr) != "" {
		return errors.New(strings.TrimSpace(zfsError.Stderr))
	}
	return err
}
