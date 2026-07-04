package zfs

import (
	"bufio"
	"context"
	"io"
	"os/exec"
	"strings"
	"zfs-file-history/internal/logging"

	"github.com/oklog/run"
)

// WatchZpoolEvents spawns `zpool events -v -f` and listens for snapshot changes in real-time.
// This should be run as an oklog/run actor.
func WatchZpoolEvents(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "zpool", "events", "-v", "-f")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	// Parse the stream and trigger RefreshZfsData when an event matches
	parseZpoolEvents(stdout, func() {
		RefreshZfsData()
	})

	if err := cmd.Wait(); err != nil {
		logging.Error("zpool events listener exited: %s", err.Error())
	}

	return nil
}

// parseZpoolEvents reads from an io.Reader and triggers onUpdate for snapshot events.
// Extracted for testability.
func parseZpoolEvents(reader io.Reader, onUpdate func()) {
	scanner := bufio.NewScanner(reader)
	inHistoryEvent := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Empty line marks the end of an event block
		if line == "" {
			inHistoryEvent = false
			continue
		}

		// Check if a new history event block is starting
		if strings.Contains(line, "sysevent.fs.zfs.history_event") {
			inHistoryEvent = true
			continue
		}

		// If we are parsing a history event, look for snapshot creations or destructions
		if inHistoryEvent {
			// ZFS logs explicit user commands in history_str, and automated/internal actions in history_internal_name
			if strings.HasPrefix(line, "history_str =") || strings.HasPrefix(line, "history_internal_name =") {
				if strings.Contains(line, "\"snapshot\"") || strings.Contains(line, "\"destroy\"") ||
					strings.Contains(line, "\"zfs snapshot ") || strings.Contains(line, "\"zfs destroy ") {

					onUpdate()

					// Reset state to avoid triggering multiple times for the same event block
					inHistoryEvent = false
				}
			}
		}
	}
}

func AddZpoolEventWatcherActor(g *run.Group, ctx context.Context) {
	g.Add(func() error {
		logging.Info("Starting ZFS event watcher...")
		// Assuming WatchZpoolEvents is now a blocking function returning an error
		return WatchZpoolEvents(ctx)
	}, func(err error) {
		// We don't need to do much here, cancelling the context
		// will naturally kill the exec.CommandContext inside it.
		logging.Debug("Stopping ZFS event watcher...")
	})
}
