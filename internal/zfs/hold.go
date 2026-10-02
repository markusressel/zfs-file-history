package zfs

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// HoldTag is the tag of the holds created by zfs-file-history. Holds with other tags (e.g. set by replication
// tools) are never released by the application.
const HoldTag = "zfs-file-history"

// Hold is a user reference ("zfs hold") on a snapshot, which prevents it from being destroyed.
type Hold struct {
	// Snapshot is the full name of the held snapshot, e.g. "pool/data@daily".
	Snapshot string
	Tag      string
	Created  time.Time
}

// ListHolds returns the holds of the snapshots with the given full names. With recursive, the holds of the snapshots
// with the same name of all descendant datasets are listed as well (like "zfs destroy -r" would destroy them).
// This spawns a zfs process, so it must not be called on the UI thread.
func ListHolds(snapshotNames []string, recursive bool) ([]Hold, error) {
	if len(snapshotNames) == 0 {
		return nil, nil
	}
	args := []string{"holds", "-H", "-p"}
	if recursive {
		args = append(args, "-r")
	}
	output, err := runZfs(append(args, snapshotNames...)...)
	if err != nil {
		return nil, err
	}
	return parseHolds(output)
}

// HoldSnapshots adds a hold with HoldTag to the snapshots with the given full names.
// Fails if one of them is already held with HoldTag. This spawns a zfs process, so it must not be called on the UI
// thread.
func HoldSnapshots(snapshotNames []string) error {
	if len(snapshotNames) == 0 {
		return nil
	}
	_, err := runZfs(append([]string{"hold", HoldTag}, snapshotNames...)...)
	return err
}

// ReleaseSnapshots removes the hold with HoldTag from the snapshots with the given full names.
// Fails if one of them is not held with HoldTag. This spawns a zfs process, so it must not be called on the UI
// thread.
func ReleaseSnapshots(snapshotNames []string) error {
	if len(snapshotNames) == 0 {
		return nil
	}
	_, err := runZfs(append([]string{"release", HoldTag}, snapshotNames...)...)
	return err
}

// parseHolds parses the output of "zfs holds -H -p", e.g.:
//
//	pool/data@daily	zfs-file-history	1727863440
func parseHolds(output string) ([]Hold, error) {
	var holds []Hold
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			return nil, fmt.Errorf("unexpected output of zfs holds: %q", line)
		}
		timestamp, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot parse the creation time of hold %q of %s: %w", fields[1], fields[0], err)
		}
		holds = append(holds, Hold{Snapshot: fields[0], Tag: fields[1], Created: time.Unix(timestamp, 0)})
	}
	return holds, nil
}

// runZfs runs the zfs command with the given arguments and returns its output.
// If it fails, the error contains the message zfs printed, if any.
func runZfs(args ...string) (string, error) {
	output, err := exec.Command("zfs", args...).Output()
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) && len(strings.TrimSpace(string(exitError.Stderr))) > 0 {
			return "", errors.New(strings.TrimSpace(string(exitError.Stderr)))
		}
		return "", err
	}
	return string(output), nil
}
