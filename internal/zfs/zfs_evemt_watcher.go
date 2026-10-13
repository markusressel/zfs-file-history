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

// poolsChangedThrottle is the most often PoolsChanged is emitted, see WatchZpoolEvents.
const poolsChangedThrottle = time.Second

// WatchZpoolEvents spawns `zpool events -v -f` and listens for changes in real-time: of the snapshots (and pools
// created, destroyed, imported or exported), which reload all ZFS data (see RefreshZfsData), and of the state of
// the pools, which only reload the pools (see PoolsChanged).
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
	var poolsTimer *time.Timer
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

	// Throttled instead of debounced: a failing device can report errors continuously, which would postpone a
	// debounced update forever.
	throttledPoolsUpdate := func() {
		updateMtx.Lock()
		defer updateMtx.Unlock()

		if poolsTimer != nil {
			// an update is pending already
			return
		}
		poolsTimer = time.AfterFunc(poolsChangedThrottle, func() {
			updateMtx.Lock()
			poolsTimer = nil
			updateMtx.Unlock()
			PoolsChanged.Emit(struct{}{})
		})
	}

	// Capture the exact time the watcher starts to filter out old backlog events
	startTime := time.Now()

	// Parse the stream and pass the startTime to filter the backlog
	parseZpoolEvents(stdout, startTime, debouncedUpdate, throttledPoolsUpdate)

	if err := cmd.Wait(); err != nil {
		return err
	}

	return nil
}

// poolEventClasses are the classes of the events about the state of pools and their devices (see zpool-events(8)),
// besides the ones with poolEventClassPrefixes.
var poolEventClasses = map[string]bool{
	"sysevent.fs.zfs.statechange":     true,
	"sysevent.fs.zfs.config_sync":     true,
	"sysevent.fs.zfs.pool_reguid":     true,
	"sysevent.fs.zfs.vdev_add":        true,
	"sysevent.fs.zfs.vdev_attach":     true,
	"sysevent.fs.zfs.vdev_autoexpand": true,
	"sysevent.fs.zfs.vdev_check":      true,
	"sysevent.fs.zfs.vdev_clear":      true,
	"sysevent.fs.zfs.vdev_online":     true,
	"sysevent.fs.zfs.vdev_remove":     true,
	"sysevent.fs.zfs.vdev_remove_aux": true,
	"sysevent.fs.zfs.vdev_remove_dev": true,
	"sysevent.fs.zfs.vdev_spare":      true,
}

// poolEventClassPrefixes are the prefixes of the classes of the events about the state of pools: scrubs, resilvers
// and trims starting, finishing and so on, the error reports (which count as read, write or checksum errors) and
// the resource events (e.g. a device that was removed).
var poolEventClassPrefixes = []string{
	"sysevent.fs.zfs.scrub_",
	"sysevent.fs.zfs.errorscrub_",
	"sysevent.fs.zfs.resilver_",
	"sysevent.fs.zfs.trim_",
	"ereport.fs.zfs.",
	"resource.fs.zfs.",
}

// poolListEventClasses are the classes of the events that change the pools, and with them the datasets.
var poolListEventClasses = map[string]bool{
	"sysevent.fs.zfs.pool_create":  true,
	"sysevent.fs.zfs.pool_destroy": true,
	"sysevent.fs.zfs.pool_import":  true,
	"sysevent.fs.zfs.pool_export":  true,
}

// isPoolEvent returns whether the event of the class is about the state of a pool or its devices.
func isPoolEvent(class string) bool {
	if poolEventClasses[class] {
		return true
	}
	for _, prefix := range poolEventClassPrefixes {
		if strings.HasPrefix(class, prefix) {
			return true
		}
	}
	return false
}

// parseZpoolEvents reads the output of `zpool events -v` from reader. For the events after startTime, it calls
// onDatasetsChanged for snapshots created or destroyed and pools created, destroyed, imported or exported, and
// onPoolsChanged for changes of the state of pools (see isPoolEvent).
func parseZpoolEvents(reader io.Reader, startTime time.Time, onDatasetsChanged func(), onPoolsChanged func()) {
	scanner := bufio.NewScanner(reader)
	// class is the class of the current event, from its first line, e.g. "sysevent.fs.zfs.history_event"
	class := ""
	isTargetAction := false
	var eventTime time.Time

	// Closure to handle the trigger logic cleanly (used for both empty lines and EOF)
	triggerIfValid := func() {
		if class == "" || !eventTime.After(startTime) {
			return
		}
		switch {
		case class == "sysevent.fs.zfs.history_event" && isTargetAction, poolListEventClasses[class]:
			onDatasetsChanged()
		case isPoolEvent(class):
			onPoolsChanged()
		}
	}

	for scanner.Scan() {
		rawLine := scanner.Text()
		line := strings.TrimSpace(rawLine)

		// Empty line marks the end of an event block
		if line == "" {
			triggerIfValid()

			// Reset block state for the next event
			class = ""
			isTargetAction = false
			eventTime = time.Time{}
			continue
		}

		// the first line of an event is not indented, e.g. "Jul  5 2026 00:55:17.359478925 sysevent.fs.zfs.history_event"
		if !strings.HasPrefix(rawLine, " ") && !strings.HasPrefix(rawLine, "\t") {
			fields := strings.Fields(line)
			class = fields[len(fields)-1]
			continue
		}

		if isTargetZfsAction(line) {
			isTargetAction = true
		} else if parsedTime, ok := parseEventTime(line); ok {
			eventTime = parsedTime
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
