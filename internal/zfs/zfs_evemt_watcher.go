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
		logging.Error("zpool events listener exited: %s", err.Error())
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

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Empty line marks the end of an event block
		if line == "" {
			if inHistoryEvent && isTargetAction {
				// Only trigger if the event happened AFTER the watcher started
				if eventTime.After(startTime) {
					onUpdate()
				}
			}

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
			// 1. Check if the action is relevant
			if strings.HasPrefix(line, "history_str =") || strings.HasPrefix(line, "history_internal_name =") {
				if strings.Contains(line, "\"snapshot\"") || strings.Contains(line, "\"destroy\"") ||
					strings.Contains(line, "\"zfs snapshot ") || strings.Contains(line, "\"zfs destroy ") {
					isTargetAction = true
				}
			}

			// 2. Extract and parse the event timestamp
			if strings.HasPrefix(line, "time = ") {
				parts := strings.Fields(line)
				if len(parts) >= 3 {
					secHex := strings.TrimPrefix(parts[2], "0x")
					sec, err := strconv.ParseInt(secHex, 16, 64)
					if err == nil {
						nsec := int64(0)
						if len(parts) >= 4 {
							nsecHex := strings.TrimPrefix(parts[3], "0x")
							nsec, _ = strconv.ParseInt(nsecHex, 16, 64)
						}
						eventTime = time.Unix(sec, nsec)
					}
				}
			}
		}
	} // End of scanner loop

	if inHistoryEvent && isTargetAction {
		if eventTime.After(startTime) {
			onUpdate()
		}
	}
}

func AddZpoolEventWatcherActor(g *run.Group, ctx context.Context) {
	g.Add(func() error {
		logging.Info("Starting ZFS event watcher...")
		return WatchZpoolEvents(ctx)
	}, func(err error) {
		logging.Debug("Stopping ZFS event watcher...")
	})
}
