package snapshot_browser

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/dialog"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"
)

// listHolds, holdSnapshots and releaseSnapshots are replaceable in tests, so tests never change holds.
var (
	listHolds        = zfs.ListHolds
	holdSnapshots    = zfs.HoldSnapshots
	releaseSnapshots = zfs.ReleaseSnapshots
)

// maxListedHolds is the maximum number of snapshots listed in hold messages.
const maxListedHolds = 10

// holdResult is the outcome of holding or releasing snapshots.
type holdResult struct {
	// changed are the full names of the snapshots that were held or released
	changed []string
	// skipped are the full names of the snapshots that were already held (hold) or not held with zfs.HoldTag (release)
	skipped []string
	// otherHolds are the holds with other tags of the changed snapshots, which still prevent destroying them
	otherHolds []zfs.Hold
}

func snapshotFullNames(entries []*data.SnapshotBrowserEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Snapshot.FullName)
	}
	return names
}

// holdOrRelease holds (hold = true) or releases the given snapshots with zfs.HoldTag. Snapshots that are already
// held (or not held) with zfs.HoldTag are skipped. Holds with other tags are never released.
// Runs in the background.
func holdOrRelease(names []string, hold bool) (*holdResult, error) {
	holds, err := listHolds(names, false)
	if err != nil {
		return nil, err
	}

	result := &holdResult{}
	for _, name := range names {
		heldByUs := slices.ContainsFunc(holds, func(h zfs.Hold) bool { return h.Snapshot == name && h.Tag == zfs.HoldTag })
		if heldByUs == hold {
			result.skipped = append(result.skipped, name)
		} else {
			result.changed = append(result.changed, name)
		}
	}
	for _, h := range holds {
		if h.Tag != zfs.HoldTag && slices.Contains(result.changed, h.Snapshot) {
			result.otherHolds = append(result.otherHolds, h)
		}
	}

	if hold {
		err = holdSnapshots(result.changed)
	} else {
		if len(result.changed) == 0 {
			return nil, notHeldByUsError(names, holds)
		}
		err = releaseSnapshots(result.changed)
	}
	if err != nil {
		return nil, err
	}
	return result, nil
}

// notHeldByUsError explains why nothing was released.
func notHeldByUsError(names []string, holds []zfs.Hold) error {
	var message strings.Builder
	fmt.Fprintf(&message, "Not held by %s: %s", zfs.HoldTag, formatNameList(names))
	if len(holds) > 0 {
		fmt.Fprintf(&message, "\n\nHolds of other tags are not released, as other tools (e.g. replication) rely on them:%s",
			formatHoldList(holds))
	}
	return errors.New(message.String())
}

// formatHoldMessage describes the result of holding (hold = true) or releasing snapshots.
func formatHoldMessage(result *holdResult, hold bool) string {
	var message strings.Builder
	count := len(result.changed)
	noun := uiutil.Plural(count, "snapshot", "snapshots")
	if hold {
		if count > 0 {
			fmt.Fprintf(&message, "Held %d %s with tag '%s', destroying %s fails until the hold is released:%s",
				count, noun, zfs.HoldTag, uiutil.Plural(count, "it", "them"), formatNameList(result.changed))
		}
		if len(result.skipped) > 0 {
			if message.Len() > 0 {
				message.WriteString("\n\n")
			}
			fmt.Fprintf(&message, "Already held:%s", formatNameList(result.skipped))
		}
		return message.String()
	}

	fmt.Fprintf(&message, "Released the hold '%s' of %d %s:%s", zfs.HoldTag, count, noun, formatNameList(result.changed))
	if len(result.skipped) > 0 {
		fmt.Fprintf(&message, "\n\nNot held by %s:%s", zfs.HoldTag, formatNameList(result.skipped))
	}
	if len(result.otherHolds) > 0 {
		fmt.Fprintf(&message, "\n\nStill held by other tags:%s", formatHoldList(result.otherHolds))
	}
	return message.String()
}

// formatNameList returns the names as indented lines, limited to maxListedHolds.
func formatNameList(names []string) string {
	var list strings.Builder
	for i, name := range names {
		if i == maxListedHolds {
			fmt.Fprintf(&list, "\n  … and %d more", len(names)-maxListedHolds)
			break
		}
		fmt.Fprintf(&list, "\n  %s", name)
	}
	return list.String()
}

// formatHoldList returns the holds as indented lines ("snapshot (tag)"), limited to maxListedHolds.
func formatHoldList(holds []zfs.Hold) string {
	names := make([]string, 0, len(holds))
	for _, h := range holds {
		names = append(names, fmt.Sprintf("%s (%s)", h.Snapshot, h.Tag))
	}
	return formatNameList(names)
}

// heldSnapshotsError is returned instead of a destroy preview if snapshots that would be destroyed are held.
// "zfs destroy -n" does not check holds, so the destroy itself would fail with "dataset is busy".
func heldSnapshotsError(holds []zfs.Hold) error {
	return fmt.Errorf("cannot destroy held snapshots, release their holds first:%s", formatHoldList(holds))
}

// showHoldResult shows the result of holding or releasing snapshots and reloads them. retry repeats the action, e.g.
// after missing permissions were granted. Must be called on the UI thread.
func (snapshotBrowser *SnapshotBrowserComponent) showHoldResult(result *holdResult, hold bool, err error, retry func()) {
	title := "Snapshots Released"
	if hold {
		title = "Snapshots Held"
	}
	if err != nil {
		logging.Error("Failed to change holds: %s", err.Error())
		failedTitle := "Release Failed"
		if hold {
			failedTitle = "Hold Failed"
		}
		snapshotBrowser.showDialog(dialog.NewErrorDialogWithRetry(snapshotBrowser.application, failedTitle, err, retry), nil)
	} else {
		snapshotBrowser.showDialog(dialog.NewSuccessDialog(snapshotBrowser.application, title, formatHoldMessage(result, hold)), nil)
		snapshotBrowser.ClearMultiSelection()
	}
	// reload the snapshot properties (number of holds), the cached libzfs handles are outdated
	zfs.RefreshZfsData()
}
