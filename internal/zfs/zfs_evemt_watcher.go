package zfs

import (
	"bufio"
	"context"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
	"zfs-file-history/internal/logging"

	"github.com/oklog/run"
)

// WatchZpoolEvents spawns `zpool events -v -f` and listens for snapshot changes in real-time.
func WatchZpoolEvents(ctx context.Context) error {
	zpoolPath, err := exec.LookPath("zpool")
	if err != nil {
		zpoolPath = "/usr/bin/zpool"
	}

	cmd := exec.CommandContext(ctx, "sudo", "-n", zpoolPath, "events", "-v", "-f")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	var updateTimer *time.Timer
	var updateMtx sync.Mutex

	debouncedUpdate := func() {
		updateMtx.Lock()
		defer updateMtx.Unlock()

		if updateTimer != nil {
			updateTimer.Stop()
		}

		updateTimer = time.AfterFunc(500*time.Millisecond, func() {
			logging.Info("Applying debounced UI update for ZFS events...")
			RefreshZfsData()
		})
	}

	// Capture the exact time the watcher starts to filter out old backlog events
	startTime := time.Now()

	// Parse the stream and pass the startTime to filter the backlog
	parseZpoolEvents(stdout, startTime, debouncedUpdate)

	if err := cmd.Wait(); err != nil {
		return err
	}

	return nil
}

// parseZpoolEvents reads from an io.Reader and triggers onUpdate for snapshot events.
func parseZpoolEvents(reader io.Reader, startTime time.Time, onUpdate func()) {
	scanner := bufio.NewScanner(reader)
	inHistoryEvent := false
	isTargetAction := false
	var eventTime time.Time

	// Closure to handle the trigger logic cleanly (used for both empty lines and EOF)
	triggerIfValid := func() {
		if inHistoryEvent && isTargetAction && eventTime.After(startTime) {
			onUpdate()
		}
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Empty line marks the end of an event block
		if line == "" {
			triggerIfValid()

			// Reset block state for the next event
			inHistoryEvent = false
			isTargetAction = false
			eventTime = time.Time{}
			continue
		}

		if strings.Contains(line, "sysevent.fs.zfs.history_event") {
			inHistoryEvent = true
			continue
		}

		if inHistoryEvent {
			if isTargetZfsAction(line) {
				isTargetAction = true
			} else if parsedTime, ok := parseEventTime(line); ok {
				eventTime = parsedTime
			}
		}
	}

	// Catch the final event if the file didn't end with an empty line
	triggerIfValid()
}

// isTargetZfsAction checks if the line indicates a snapshot creation or destruction
func isTargetZfsAction(line string) bool {
	if !strings.HasPrefix(line, "history_str =") && !strings.HasPrefix(line, "history_internal_name =") {
		return false
	}

	return strings.Contains(line, "\"snapshot\"") || strings.Contains(line, "\"destroy\"") ||
		strings.Contains(line, "\"zfs snapshot ") || strings.Contains(line, "\"zfs destroy ")
}

// parseEventTime extracts and parses the hex timestamp from a ZFS event time line
func parseEventTime(line string) (time.Time, bool) {
	if !strings.HasPrefix(line, "time = ") {
		return time.Time{}, false
	}

	parts := strings.Fields(line)
	if len(parts) < 3 {
		return time.Time{}, false
	}

	secHex := strings.TrimPrefix(parts[2], "0x")
	sec, err := strconv.ParseInt(secHex, 16, 64)
	if err != nil {
		return time.Time{}, false
	}

	nsec := int64(0)
	if len(parts) >= 4 {
		nsecHex := strings.TrimPrefix(parts[3], "0x")
		nsec, _ = strconv.ParseInt(nsecHex, 16, 64)
	}

	return time.Unix(sec, nsec), true
}

func AddZpoolEventWatcherActor(g *run.Group, ctx context.Context) {
	g.Add(func() error {
		logging.Info("Starting ZFS event watcher...")

		err := WatchZpoolEvents(ctx)

		// If it crashed but the application ISN'T shutting down (ctx is not canceled)
		if err != nil && ctx.Err() == nil {
			logging.Warning("⚠️ Real-time updates disabled. Missing permissions to watch ZFS events.")
			logging.Warning("Run 'sudo zfs-file-history setup' to enable real-time UI refreshes.")
		}

		// CRITICAL: Block until the application shuts down!
		// If we return here, oklog/run will forcefully kill the UI and exit the app.
		<-ctx.Done()

		return nil
	}, func(err error) {
		logging.Debug("Stopping ZFS event watcher...")
	})
}
