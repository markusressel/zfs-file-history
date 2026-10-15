package zfs

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
	"zfs-file-history/internal/logging"

	"github.com/oklog/run"
)

// poolsChangedThrottle is the most often PoolsChanged is emitted, see WatchZpoolEvents.
const poolsChangedThrottle = time.Second

// WatchZpoolEvents listens for changes in real-time (see Backend.WatchEvents, `zpool events -v -f` on the system):
// of the snapshots (and pools created, destroyed, imported or exported), which reload all ZFS data (see
// RefreshZfsData), and of the state of the pools, which only reload the pools (see PoolsChanged).
func WatchZpoolEvents(ctx context.Context) error {
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

	return backend.WatchEvents(ctx, debouncedUpdate, throttledPoolsUpdate, PoolHistoryRecorded.Emit)
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

// historyEventClass is the class of the events of the records ZFS logs in the history of a pool, see historyEvent.
const historyEventClass = "sysevent.fs.zfs.history_event"

// parseZpoolEvents reads the output of `zpool events -v` from reader. For the events after startTime, it calls
// onDatasetsChanged for snapshots created or destroyed and pools created, destroyed, imported or exported,
// onPoolsChanged for changes of the state of pools (see isPoolEvent), and onHistory with the records ZFS logged in
// the history of a pool (see historyEvent).
func parseZpoolEvents(reader io.Reader, startTime time.Time, onDatasetsChanged func(), onPoolsChanged func(), onHistory func(PoolHistoryEntry)) {
	scanner := bufio.NewScanner(reader)
	// class is the class of the current event, from its first line, e.g. "sysevent.fs.zfs.history_event"
	class := ""
	isTargetAction := false
	var eventTime time.Time
	// history are the values of the current event of the class historyEventClass
	history := historyEvent{}

	// Closure to handle the trigger logic cleanly (used for both empty lines and EOF)
	triggerIfValid := func() {
		if class == "" || !eventTime.After(startTime) {
			return
		}
		if class == historyEventClass {
			if entry, ok := history.entry(); ok && onHistory != nil {
				onHistory(entry)
			}
		}
		switch {
		case class == historyEventClass && isTargetAction, poolListEventClasses[class]:
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
			clear(history)
			continue
		}

		// the first line of an event is not indented, e.g. "Jul  5 2026 00:55:17.359478925 sysevent.fs.zfs.history_event"
		if !strings.HasPrefix(rawLine, " ") && !strings.HasPrefix(rawLine, "\t") {
			fields := strings.Fields(line)
			class = fields[len(fields)-1]
			continue
		}

		if class == historyEventClass {
			if name, value, found := strings.Cut(line, " = "); found {
				history[name] = value
			}
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

// historyEvent are the values of an event of a record ZFS logged in the history of a pool, by their name, as printed
// (e.g. `"snapshot"`, `0x15cb403`). The events have the values of the record (see zpool-events(8)), but only of
// the ones ZFS logs itself: the commands are not reported.
type historyEvent map[string]string

// entry returns the record of the history the event reports, as "zpool history -il" lists it (see
// poolHistoryParser), false if it is not one ZFS logged itself.
func (event historyEvent) entry() (PoolHistoryEntry, bool) {
	name, ok := event.text("history_internal_name")
	pool, hasPool := event.text("pool")
	recorded, hasTime := event.number("history_time")
	txg, hasTxg := event.number("history_txg")
	if !ok || !hasPool || !hasTime || !hasTxg {
		return PoolHistoryEntry{}, false
	}
	// as zpool prints it, e.g. "snapshot rpool/data@daily (1548) " or "scan done errors=0"
	text := name
	if dataset, ok := event.text("history_dsname"); ok {
		id, _ := event.number("history_dsid")
		text += fmt.Sprintf(" %s (%d)", dataset, id)
	}
	if internal, ok := event.text("history_internal_str"); ok {
		text += " " + internal
	}
	entry := PoolHistoryEntry{Pool: pool, Time: time.Unix(int64(recorded), 0), Internal: true, Txg: txg}
	entry.Host, _ = event.text("history_hostname")
	parseInternalEvent(&entry, strings.TrimSpace(printable(text)))
	return entry, true
}

// text returns the string value with the name, without its quotes.
func (event historyEvent) text(name string) (string, bool) {
	value, ok := event[name]
	if !ok || len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return "", false
	}
	return value[1 : len(value)-1], true
}

// number returns the number value with the name, e.g. 0x15cb403.
func (event historyEvent) number(name string) (uint64, bool) {
	number, err := strconv.ParseUint(event[name], 0, 64)
	return number, err == nil
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

// AddZpoolEventWatcherActor runs WatchZpoolEvents until the application shuts down. If the user may not watch the
// events (see SudoersRuleEvents), it is started again once that was allowed (see SudoersChanged).
func AddZpoolEventWatcherActor(g *run.Group, ctx context.Context) {
	// subscribed here, before the UI runs, so a change while the UI starts is not missed
	sudoersChanged := make(chan struct{}, 1)
	SudoersChanged.Subscribe(func(struct{}) {
		select {
		case sudoersChanged <- struct{}{}:
		default:
		}
	})
	g.Add(func() error {
		for {
			logging.Info("Starting ZFS event watcher...")
			err := WatchZpoolEvents(ctx)
			if ctx.Err() != nil {
				return nil
			}
			if err != nil {
				logging.Warning("⚠️ Real-time updates disabled. Missing permissions to watch ZFS events: %v", err)
				logging.Warning("Run 'sudo zfs-file-history setup --events' to enable real-time UI refreshes.")
			}

			// Block until the application shuts down, or the user may watch the events now. If we return here,
			// oklog/run stops the UI and exits the app.
			select {
			case <-ctx.Done():
				return nil
			case <-sudoersChanged:
			}
		}
	}, func(err error) {
		logging.Debug("Stopping ZFS event watcher...")
	})
}
