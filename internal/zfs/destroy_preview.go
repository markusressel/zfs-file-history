package zfs

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// DestroyPreview is the result of a dry run of destroying snapshots ("zfs destroy -nvp").
type DestroyPreview struct {
	// Destroyed are the full names of everything that would be destroyed, including snapshots of child datasets
	// and dependent clones of recursive destroys.
	Destroyed []string
	// Reclaim is the space in bytes that would be freed.
	Reclaim uint64
}

// PreviewDestroySnapshots does a dry run of destroying the given snapshots, with the same flags as
// Snapshot.Destroy(recursive, dependantClones). All snapshots must belong to the same dataset, so the space that
// would be freed by destroying them together is reported (which can be more than the sum of the single snapshots).
// This spawns zfs processes, so it must not be called on the UI thread.
func PreviewDestroySnapshots(snapshots []*Snapshot, recursive bool, dependantClones bool) (*DestroyPreview, error) {
	dataset, names, err := destroyTarget(snapshots)
	if err != nil {
		return nil, err
	}
	// the dry run succeeds without permissions, so the destroy would only fail after the confirmation
	if err := checkPermissions(destroyAction(len(snapshots)), PermissionGap{
		Dataset:     dataset,
		Permissions: destroyPermissions,
	}); err != nil {
		return nil, err
	}
	return backend.PreviewDestroySnapshots(dataset, names, recursive, dependantClones)
}

// destroyPermissions are the permissions needed to destroy snapshots.
var destroyPermissions = []Permission{PermissionDestroy, PermissionMount}

func destroyAction(count int) string {
	if count == 1 {
		return "destroy the snapshot"
	}
	return "destroy the snapshots"
}

// destroyTarget returns the dataset of the given snapshots and their names (behind the "@"), e.g. "pool/data" and
// ["a", "b"]. They must all belong to the same dataset.
func destroyTarget(snapshots []*Snapshot) (dataset string, names []string, err error) {
	if len(snapshots) == 0 {
		return "", nil, errors.New("no snapshots to destroy")
	}

	datasetName := ""
	names = make([]string, 0, len(snapshots))
	for _, snapshot := range snapshots {
		snapshotDataset, snapshotName, found := strings.Cut(snapshot.FullName, "@")
		if !found || snapshotDataset == "" || snapshotName == "" {
			return "", nil, fmt.Errorf("invalid snapshot name: %s", snapshot.FullName)
		}
		if datasetName == "" {
			datasetName = snapshotDataset
		} else if datasetName != snapshotDataset {
			return "", nil, fmt.Errorf("snapshots of different datasets: %s and %s", datasetName, snapshotDataset)
		}
		names = append(names, snapshotName)
	}
	return datasetName, names, nil
}

// parseDestroyPreview parses the output of "zfs destroy -nvp", e.g.:
//
//	destroy	pool/data@a
//	destroy	pool/data@b
//	reclaim	447066112
//
// zfs only prints the reclaimed space for snapshots, so with requireReclaim false (destroying datasets) it may be
// missing, Reclaim is 0 then.
func parseDestroyPreview(output string, requireReclaim bool) (*DestroyPreview, error) {
	preview := &DestroyPreview{}
	foundReclaim := false
	for _, line := range strings.Split(output, "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "\t")
		if !found {
			continue
		}
		switch key {
		case "destroy":
			preview.Destroyed = append(preview.Destroyed, value)
		case "reclaim":
			reclaim, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("cannot parse reclaimed space %q: %w", value, err)
			}
			preview.Reclaim = reclaim
			foundReclaim = true
		}
	}
	if len(preview.Destroyed) == 0 || (requireReclaim && !foundReclaim) {
		return nil, fmt.Errorf("unexpected output of zfs destroy: %q", strings.TrimSpace(output))
	}
	return preview, nil
}
