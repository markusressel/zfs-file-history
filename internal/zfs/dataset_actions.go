package zfs

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// The actions on datasets that lose data (destroying a clone, rolling back) work in two steps: a preview lists what
// would be lost, without changing anything, and the action takes the confirmed preview. It checks everything again
// and refuses if the result differs from what was confirmed (ErrChangedSinceConfirmation), so it never destroys more
// than the user saw. Neither ever uses "-R", which would also destroy clones, wherever they are.

// ErrChangedSinceConfirmation is returned by the actions that lose data if what they would destroy differs from the
// confirmed preview, e.g. because a snapshot was created in between.
var ErrChangedSinceConfirmation = errors.New("what would be destroyed changed since it was confirmed, nothing was changed: review it again")

// maxListedNames is the maximum number of names listed in errors.
const maxListedNames = 10

// IsClone returns whether origin, the value of the "origin" property of a dataset, says that it is a clone.
func IsClone(origin string) bool {
	return origin != "" && origin != "-"
}

// validateDatasetName returns an error if name is not the name of a dataset (e.g. empty, a snapshot, or something
// zfs would take for an option).
func validateDatasetName(name string) error {
	if name == "" || strings.HasPrefix(name, "-") || strings.ContainsAny(name, "@#") {
		return fmt.Errorf("invalid dataset name: %q", name)
	}
	return nil
}

// CreateSnapshot creates the snapshot (name behind the "@") of the dataset, with recursive also the ones with the same
// name of all descendants ("zfs snapshot -r"), all at once. This spawns a zfs process, so it must not be called on
// the UI thread.
func CreateSnapshot(dataset string, name string, recursive bool) error {
	if err := validateDatasetName(dataset); err != nil {
		return err
	}
	if name == "" || strings.HasPrefix(name, "-") || strings.ContainsAny(name, "@#/") {
		return fmt.Errorf("invalid snapshot name: %q", name)
	}
	return explainPermissionError(backend.CreateSnapshot(dataset, name, recursive), "create a snapshot",
		PermissionGap{Dataset: dataset, Permissions: []Permission{PermissionSnapshot, PermissionMount}})
}

// cloneOrigin returns the origin of the dataset (the full name of the snapshot it was cloned from), or an error if it
// is not a clone. It reads the property again, the listed datasets may be outdated.
func cloneOrigin(name string, action string) (string, error) {
	properties, err := cloneProperties(name, action)
	if err != nil {
		return "", err
	}
	return properties[propOrigin], nil
}

// cloneProperties returns the datasetProperties of the dataset, or an error if it is not a clone, see cloneOrigin.
func cloneProperties(name string, action string) (map[string]string, error) {
	if err := validateDatasetName(name); err != nil {
		return nil, err
	}
	properties, err := backend.DatasetProperties(name)
	if err != nil {
		return nil, err
	}
	if !IsClone(properties[propOrigin]) {
		return nil, fmt.Errorf("cannot %s: '%s' is not a clone", action, name)
	}
	return properties, nil
}

// PreviewDestroyClone does a dry run of destroying the clone with the given name, together with its snapshots and
// descendants ("zfs destroy -nvp -r"): DestroyPreview.Destroyed lists all of them, including the clone. It refuses
// datasets that are not clones, missing permissions and held snapshots, so the destroy does not fail halfway. The dry
// run fails if snapshots of the clone have clones themselves, those are never destroyed with it.
// This spawns zfs processes, so it must not be called on the UI thread.
func PreviewDestroyClone(name string) (*DestroyPreview, error) {
	const action = "destroy the clone"
	properties, err := cloneProperties(name, action)
	if err != nil {
		return nil, err
	}
	// the dry run succeeds without permissions, so the destroy would only fail after the confirmation
	if err := checkPermissions(action, PermissionGap{Dataset: name, Permissions: destroyPermissions}); err != nil {
		return nil, err
	}
	preview, err := backend.PreviewDestroyDataset(name, true)
	if err != nil {
		return nil, fmt.Errorf("cannot %s: %w", action, err)
	}
	// zfs prints the reclaimed space only for snapshots: the clone frees what it uses, with its snapshots and
	// descendants
	if preview.Reclaim == 0 {
		preview.Reclaim, _ = strconv.ParseUint(properties[propUsed], 10, 64)
	}
	if !slices.Contains(preview.Destroyed, name) {
		return nil, fmt.Errorf("cannot %s: the dry run does not list '%s'", action, name)
	}
	var snapshots []string
	for _, destroyed := range preview.Destroyed {
		if !isWithinDataset(destroyed, name) {
			// zfs never lists anything else for "-r", but nothing outside the clone is destroyed, whatever happens
			return nil, fmt.Errorf("cannot %s: the dry run lists '%s', which is not part of it", action, destroyed)
		}
		if strings.Contains(destroyed, "@") {
			snapshots = append(snapshots, destroyed)
		}
	}
	// the dry run does not check holds, the destroy would fail with "dataset is busy"
	if err := refuseHeld(snapshots, action); err != nil {
		return nil, err
	}
	return preview, nil
}

// isWithinDataset returns whether name is the dataset, one of its descendants, or a snapshot or bookmark of either.
func isWithinDataset(name string, dataset string) bool {
	if name == dataset {
		return true
	}
	rest, found := strings.CutPrefix(name, dataset)
	return found && rest != "" && strings.ContainsRune("/@#", rune(rest[0]))
}

// DestroyClone destroys the clone with the given name exactly as confirmed, a preview of PreviewDestroyClone. Without
// recursive it refuses a clone with snapshots or descendants, with it they are destroyed as well ("zfs destroy -r").
// Returns ErrChangedSinceConfirmation if the clone changed since the preview.
// This spawns zfs processes, so it must not be called on the UI thread.
func DestroyClone(name string, recursive bool, confirmed *DestroyPreview) error {
	const action = "destroy the clone"
	if confirmed == nil {
		return fmt.Errorf("cannot %s: not confirmed", action)
	}
	preview, err := PreviewDestroyClone(name)
	if err != nil {
		return err
	}
	if !sameNames(preview.Destroyed, confirmed.Destroyed) {
		return ErrChangedSinceConfirmation
	}
	if len(preview.Destroyed) > 1 && !recursive {
		return fmt.Errorf("cannot %s: it has snapshots or child datasets, which would be destroyed as well", action)
	}
	return explainPermissionError(backend.DestroyDataset(name, recursive), action,
		PermissionGap{Dataset: name, Permissions: destroyPermissions})
}

// RollbackPreview is what rolling back to a snapshot would lose, see PreviewRollback.
type RollbackPreview struct {
	// Snapshot is the full name of the snapshot to roll back to, e.g. "pool/data@daily".
	Snapshot string
	// Destroyed are the full names of the newer snapshots of the dataset, which the rollback destroys, the newest
	// first. The changes of the dataset since the snapshot are lost as well.
	Destroyed []string
}

// PreviewRollback returns what rolling back to the snapshot with the given full name would destroy, without changing
// anything. It refuses missing permissions, and newer snapshots that are held or have clones: the rollback would
// fail, and their clones are never destroyed with them.
// This spawns zfs processes, so it must not be called on the UI thread.
func PreviewRollback(snapshotFullName string) (*RollbackPreview, error) {
	const action = "roll back"
	dataset, name, found := strings.Cut(snapshotFullName, "@")
	if !found || name == "" {
		return nil, fmt.Errorf("invalid snapshot name: %q", snapshotFullName)
	}
	if err := validateDatasetName(dataset); err != nil {
		return nil, err
	}
	names, err := backend.ListSnapshotNames(dataset)
	if err != nil {
		return nil, err
	}
	index := slices.Index(names, name)
	if index < 0 {
		return nil, fmt.Errorf("cannot %s: snapshot '%s' does not exist", action, snapshotFullName)
	}
	newer := names[:index]
	if err := checkPermissions(action, rollbackPermissions(dataset, len(newer) > 0)); err != nil {
		return nil, err
	}

	preview := &RollbackPreview{Snapshot: snapshotFullName}
	if len(newer) == 0 {
		return preview, nil
	}
	properties := backend.SnapshotProperties(dataset, newer)
	var cloned []string
	for _, newerName := range newer {
		fullName := dataset + "@" + newerName
		snapshotProperties, ok := properties[newerName]
		if !ok {
			return nil, fmt.Errorf("cannot %s: cannot read snapshot '%s'", action, fullName)
		}
		if snapshotProperties.Clones > 0 {
			cloned = append(cloned, fullName)
		}
		preview.Destroyed = append(preview.Destroyed, fullName)
	}
	if len(cloned) > 0 {
		return nil, fmt.Errorf("cannot %s: newer snapshots have clones, destroy or promote them first:%s", action, formatNames(cloned))
	}
	if err := refuseHeld(preview.Destroyed, action); err != nil {
		return nil, err
	}
	return preview, nil
}

// rollbackPermissions are the permissions needed to roll back the dataset, with destroyNewer also destroying newer
// snapshots.
func rollbackPermissions(dataset string, destroyNewer bool) PermissionGap {
	permissions := []Permission{PermissionRollback, PermissionMount}
	if destroyNewer {
		permissions = append(permissions, PermissionDestroy)
	}
	return PermissionGap{Dataset: dataset, Permissions: permissions}
}

// Rollback rolls the dataset back to the snapshot exactly as confirmed, a preview of PreviewRollback: the newer
// snapshots it lists are destroyed ("zfs rollback -r"). Returns ErrChangedSinceConfirmation if the snapshots changed
// since the preview.
// This spawns zfs processes, so it must not be called on the UI thread.
func Rollback(confirmed *RollbackPreview) error {
	if confirmed == nil {
		return errors.New("cannot roll back: not confirmed")
	}
	preview, err := PreviewRollback(confirmed.Snapshot)
	if err != nil {
		return err
	}
	if !sameNames(preview.Destroyed, confirmed.Destroyed) {
		return ErrChangedSinceConfirmation
	}
	dataset, name, _ := strings.Cut(preview.Snapshot, "@")
	destroyNewer := len(preview.Destroyed) > 0
	return explainPermissionError(backend.RollbackSnapshot(dataset, name, destroyNewer), "roll back",
		rollbackPermissions(dataset, destroyNewer))
}

// PromotePreview is what promoting a clone changes, see PreviewPromote.
type PromotePreview struct {
	// Clone is the name of the clone.
	Clone string
	// Origin is the full name of the snapshot the clone was created from.
	Origin string
	// Moved are the full names of the snapshots that move from the dataset of the origin to the clone: the origin and
	// the older ones, the newest first. The dataset of the origin becomes a clone of the origin afterwards.
	Moved []string
}

// PreviewPromote returns what promoting the clone with the given name would change, without changing anything. It
// refuses datasets that are not clones and missing permissions.
// This spawns zfs processes, so it must not be called on the UI thread.
func PreviewPromote(name string) (*PromotePreview, error) {
	const action = "promote the clone"
	origin, err := cloneOrigin(name, action)
	if err != nil {
		return nil, err
	}
	originDataset, originName, _ := strings.Cut(origin, "@")
	if err := checkPermissions(action, promotePermissions(name, originDataset)...); err != nil {
		return nil, err
	}
	names, err := backend.ListSnapshotNames(originDataset)
	if err != nil {
		return nil, err
	}
	preview := &PromotePreview{Clone: name, Origin: origin}
	if index := slices.Index(names, originName); index >= 0 {
		for _, moved := range names[index:] {
			preview.Moved = append(preview.Moved, originDataset+"@"+moved)
		}
	}
	return preview, nil
}

// promotePermissions are the permissions needed to promote the clone, whose origin belongs to originDataset.
func promotePermissions(clone string, originDataset string) []PermissionGap {
	permissions := []Permission{PermissionPromote, PermissionMount}
	return []PermissionGap{{Dataset: clone, Permissions: permissions}, {Dataset: originDataset, Permissions: permissions}}
}

// PromoteClone promotes the clone with the given name ("zfs promote"), so it no longer depends on its origin, which
// can then be destroyed. Nothing is lost: the snapshots up to the origin move to the clone, and the dataset of the
// origin becomes a clone of it. This spawns zfs processes, so it must not be called on the UI thread.
func PromoteClone(name string) error {
	const action = "promote the clone"
	origin, err := cloneOrigin(name, action)
	if err != nil {
		return err
	}
	return explainPermissionError(backend.PromoteDataset(name), action,
		promotePermissions(name, datasetOfSnapshot(origin))...)
}

// refuseHeld returns an error listing the holds if any of the snapshots (full names) are held, so they cannot be
// destroyed.
func refuseHeld(snapshots []string, action string) error {
	if len(snapshots) == 0 {
		return nil
	}
	holds, err := backend.ListHolds(snapshots, false)
	if err != nil {
		return err
	}
	if len(holds) == 0 {
		return nil
	}
	held := make([]string, 0, len(holds))
	for _, hold := range holds {
		held = append(held, fmt.Sprintf("%s (%s)", hold.Snapshot, hold.Tag))
	}
	return fmt.Errorf("cannot %s: snapshots that would be destroyed are held, release their holds first:%s", action, formatNames(held))
}

// formatNames returns the names as indented lines, at most maxListedNames.
func formatNames(names []string) string {
	var result strings.Builder
	for i, name := range names {
		if i == maxListedNames {
			fmt.Fprintf(&result, "\n  … and %d more", len(names)-maxListedNames)
			break
		}
		fmt.Fprintf(&result, "\n  %s", name)
	}
	return result.String()
}

// sameNames returns whether a and b contain the same names, in any order.
func sameNames(a []string, b []string) bool {
	return slices.Equal(slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b)))
}
