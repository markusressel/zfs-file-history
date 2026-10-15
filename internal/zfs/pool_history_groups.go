package zfs

import (
	"slices"
	"strconv"
	"strings"
	"time"
)

// PoolHistoryGroupKind is what the records of a PoolHistoryGroup are, see GroupPoolHistory.
type PoolHistoryGroupKind int

const (
	// PoolHistorySingle is a record that belongs to nothing else
	PoolHistorySingle PoolHistoryGroupKind = iota
	// PoolHistoryCommand is a command with the events ZFS logged for it, e.g. each snapshot of "zfs snapshot -r"
	PoolHistoryCommand
	// PoolHistoryCommands are commands one user ran in a row, with their events, e.g. a run of a backup tool, or
	// events ZFS logged in a row that belong to no command
	PoolHistoryCommands
	// PoolHistoryScrub, PoolHistoryResilver and PoolHistoryScan are a scan of the pool from its start to its end, with
	// the commands that started, paused or cancelled it. PoolHistoryScan is one whose start is not logged, so it is
	// not known whether it was a scrub or a resilver.
	PoolHistoryScrub
	PoolHistoryResilver
	PoolHistoryScan
	// PoolHistoryTrim is a TRIM of the devices of the pool from its start to the end on the last device, with the
	// commands that started or cancelled it
	PoolHistoryTrim
)

// PoolHistoryOutcome is how the scan or TRIM of a PoolHistoryGroup ended.
type PoolHistoryOutcome int

const (
	// PoolHistoryRunning is a scan or TRIM whose end is not logged (yet)
	PoolHistoryRunning PoolHistoryOutcome = iota
	// PoolHistoryPaused is a scrub whose end is not logged, paused by the last command
	PoolHistoryPaused
	PoolHistoryDone
	PoolHistoryCancelled
	// PoolHistoryRestarted is a scan ZFS stopped to start another one, e.g. a scrub when a device came back and
	// needed a resilver
	PoolHistoryRestarted
	// PoolHistorySuspended is a TRIM suspended on a device, e.g. while the device was offline
	PoolHistorySuspended
	// PoolHistoryEndUnknown is a scan or TRIM followed by another one without its end being logged
	PoolHistoryEndUnknown
)

const (
	// historyClaimWindow is how long before a command ZFS may log the events of it: they are logged while the
	// command runs, the command when it ended.
	historyClaimWindow = 10 * time.Minute
	// historySendClaimWindow is the same for the holds "zfs send" puts on the snapshots it sends: they are logged when
	// the send starts, which may take hours.
	historySendClaimWindow = 24 * time.Hour
	// historyNoDatasetsWindow is how close an event that names no dataset (e.g. "open", "import" of "zpool import")
	// must be to a command that names none, to belong to it.
	historyNoDatasetsWindow = 5 * time.Second
	// historyLateCommandWindow is how long after the end of a scan or TRIM the command that ended it may be logged,
	// e.g. "zpool scrub -s" after "scan cancelled".
	historyLateCommandWindow = 10 * time.Second
	// historyBurstGap is the longest gap between the commands of one user that are grouped as a run of commands.
	historyBurstGap = time.Minute
)

// PoolHistoryGroup is records of the history of a pool that belong together, see GroupPoolHistory.
type PoolHistoryGroup struct {
	Kind PoolHistoryGroupKind
	// Entries are the records, in the order they were logged
	Entries []*PoolHistoryEntry
	// Outcome is how a scan or TRIM ended
	Outcome PoolHistoryOutcome
	// Errors is the number of errors the end of a scan logged
	Errors uint64
	// Devices are the devices a TRIM ran on
	Devices []string

	// first is the index of the first record in the history, to keep the groups in the order of the history
	first int
	// started is whether the start of a scan is logged ("scan setup")
	started bool
	// trimming are the devices of a TRIM that have not ended yet, cancelled and suspended whether one of them was
	trimming             map[string]bool
	cancelled, suspended bool
}

// Start returns when the first record of the group was logged.
func (group *PoolHistoryGroup) Start() time.Time {
	return group.Entries[0].Time
}

// End returns when the last record of the group was logged.
func (group *PoolHistoryGroup) End() time.Time {
	return group.Entries[len(group.Entries)-1].Time
}

// IsActivity returns whether the group is a scan or TRIM.
func (group *PoolHistoryGroup) IsActivity() bool {
	return group.Kind >= PoolHistoryScrub
}

// Commands returns the commands of the group, in the order they were logged.
func (group *PoolHistoryGroup) Commands() []*PoolHistoryEntry {
	var commands []*PoolHistoryEntry
	for _, entry := range group.Entries {
		if !entry.Internal {
			commands = append(commands, entry)
		}
	}
	return commands
}

// GroupPoolHistory groups the records of the history of one pool (oldest first, see PoolHistory) into what a person
// did, or what happened, oldest first. Each record is in exactly one group:
//   - a scrub, resilver or TRIM from its start to its end, with the commands that started, paused or cancelled it,
//   - a command with the events ZFS logged for it (events name the datasets of the command, or of its children, and
//     are logged before it),
//   - commands one user ran in a row (less than historyBurstGap apart), e.g. a backup tool that snapshots, sends and
//     destroys, or the hook of a package manager,
//   - a record that belongs to nothing else.
//
// Scans and TRIMs are recognized by the events ZFS logs for them, the others by datasets and time, so groups may
// overlap in time: a scrub runs while commands are run.
func GroupPoolHistory(entries []PoolHistoryEntry) []*PoolHistoryGroup {
	grouper := &historyGrouper{}
	for index := range entries {
		grouper.add(index, &entries[index])
	}
	grouper.finish()
	return grouper.mergeBursts()
}

// historyGrouper groups records, see GroupPoolHistory.
type historyGrouper struct {
	groups []*PoolHistoryGroup
	// scan and trim are the running scan and TRIM, lastScan and lastTrim the ones that ended last
	scan, lastScan *PoolHistoryGroup
	trim, lastTrim *PoolHistoryGroup
	// pending are the events not yet known to belong to a command, oldest first
	pending []pendingHistoryEvent
}

// pendingHistoryEvent is an event that may belong to a command logged after it.
type pendingHistoryEvent struct {
	index int
	entry *PoolHistoryEntry
}

func (grouper *historyGrouper) add(index int, entry *PoolHistoryEntry) {
	if grouper.addToActivity(index, entry) {
		return
	}
	grouper.expirePending(entry.Time)
	if entry.Internal {
		grouper.pending = append(grouper.pending, pendingHistoryEvent{index: index, entry: entry})
		return
	}
	group := &PoolHistoryGroup{Kind: PoolHistorySingle, first: index}
	kept := grouper.pending[:0]
	for _, event := range grouper.pending {
		if historyEventOfCommand(event.entry, entry) {
			group.Kind = PoolHistoryCommand
			group.first = min(group.first, event.index)
			group.Entries = append(group.Entries, event.entry)
		} else {
			kept = append(kept, event)
		}
	}
	clear(grouper.pending[len(kept):])
	grouper.pending = kept
	group.Entries = append(group.Entries, entry)
	grouper.groups = append(grouper.groups, group)
}

// expirePending makes the pending events that are too old to belong to a command logged at now records of their own.
func (grouper *historyGrouper) expirePending(now time.Time) {
	kept := grouper.pending[:0]
	for _, event := range grouper.pending {
		window := historyClaimWindow
		if isSendHold(event.entry) {
			window = historySendClaimWindow
		}
		if now.Sub(event.entry.Time) > window {
			grouper.groups = append(grouper.groups, &PoolHistoryGroup{Kind: PoolHistorySingle, first: event.index, Entries: []*PoolHistoryEntry{event.entry}})
		} else {
			kept = append(kept, event)
		}
	}
	clear(grouper.pending[len(kept):])
	grouper.pending = kept
}

// isSendHold returns whether the event is a hold "zfs send" put on a snapshot it sends, or released.
func isSendHold(entry *PoolHistoryEntry) bool {
	return (entry.Event == "hold" || entry.Event == "release") && strings.Contains(entry.Text, "tag=.send-")
}

// historyEventOfCommand returns whether the event was logged for the command: it names one of its datasets or one
// below it (e.g. a child of "zfs snapshot -r", a snapshot of "zfs destroy -r"), or neither names a dataset and they
// were logged at about the same time (e.g. "open" and "import" of "zpool import").
func historyEventOfCommand(event *PoolHistoryEntry, command *PoolHistoryEntry) bool {
	// the events of a command are logged while it runs, so one logged after it ended is not one of them
	if event.Time.Sub(command.Time) > historyNoDatasetsWindow || (command.Time.Sub(event.Time) > historyClaimWindow && !isSendHold(event)) {
		return false
	}
	if len(command.Datasets) == 0 {
		// the root dataset is not one of the datasets of a command, e.g. "zfs set compression=lz4 rpool"
		aboutPool := len(event.Datasets) == 0 || (len(event.Datasets) == 1 && event.Datasets[0] == event.Pool)
		return aboutPool && command.Time.Sub(event.Time) <= historyNoDatasetsWindow
	}
	for _, name := range event.Datasets {
		for _, commandName := range command.Datasets {
			if command.Event == "zfs rollback" {
				// it changes the dataset of the snapshot: destroys the newer snapshots, swaps in a clone
				commandName, _ = splitSnapshotName(commandName)
			}
			if datasetCovers(commandName, name) {
				return true
			}
		}
	}
	return false
}

// datasetCovers returns whether a command on the dataset (or snapshot, bookmark) given changes name: the same, a
// dataset below it, or a snapshot of one of those with the same name if a snapshot was given (e.g. "rpool@daily"
// covers "rpool/home@daily"). A range or list of snapshots ("rpool/home@a%c", "rpool/home@a,b") covers all snapshots
// of the dataset.
func datasetCovers(given string, name string) bool {
	givenBase, givenSnapshot := splitSnapshotName(given)
	base, snapshot := splitSnapshotName(name)
	if base != givenBase && !strings.HasPrefix(base, givenBase+"/") {
		return false
	}
	if givenSnapshot == "" || givenSnapshot == snapshot {
		return true
	}
	return snapshot != "" && strings.ContainsAny(givenSnapshot, "%,")
}

// splitSnapshotName returns the dataset of a snapshot or bookmark, and the rest with its "@" or "#".
func splitSnapshotName(name string) (dataset string, snapshot string) {
	if index := strings.IndexAny(name, "@#"); index >= 0 {
		return name[:index], name[index:]
	}
	return name, ""
}

// addToActivity adds the record to the scan or TRIM it belongs to, and returns whether it did.
func (grouper *historyGrouper) addToActivity(index int, entry *PoolHistoryEntry) bool {
	if entry.Internal {
		switch entry.Event {
		case "scan setup":
			kind := PoolHistoryScrub
			if historyValue(entry.Text, "func") == "2" {
				kind = PoolHistoryResilver
			}
			if grouper.scan == nil || grouper.scan.started {
				grouper.endScan(PoolHistoryEndUnknown)
				grouper.scan = grouper.open(index, kind)
			}
			grouper.scan.Kind, grouper.scan.started = kind, true
			grouper.scan.Entries = append(grouper.scan.Entries, entry)
			return true
		case "starting deferred resilver":
			// logged before the "scan setup" of the resilver
			grouper.endScan(PoolHistoryEndUnknown)
			grouper.scan = grouper.open(index, PoolHistoryResilver)
			grouper.scan.Entries = append(grouper.scan.Entries, entry)
			return true
		case "scan done", "scan cancelled", "scan aborted, restarting":
			if grouper.scan == nil {
				// its start was not logged, or is no longer in the history
				grouper.scan = grouper.open(index, PoolHistoryScan)
			}
			grouper.scan.Entries = append(grouper.scan.Entries, entry)
			grouper.scan.Errors, _ = strconv.ParseUint(historyValue(entry.Text, "errors"), 10, 64)
			outcome := map[string]PoolHistoryOutcome{"scan done": PoolHistoryDone, "scan cancelled": PoolHistoryCancelled,
				"scan aborted, restarting": PoolHistoryRestarted}[entry.Event]
			grouper.endScan(outcome)
			return true
		case "trim":
			grouper.addTrimEvent(index, entry)
			return true
		}
		return false
	}

	arguments := strings.Fields(entry.Text)
	switch entry.Event {
	case "zpool scrub", "zpool resilver":
		if group := grouper.recent(grouper.scan, grouper.lastScan, entry); group != nil {
			group.Entries = append(group.Entries, entry)
			return true
		}
		if slices.Contains(arguments, "-s") || slices.Contains(arguments, "-p") {
			return false
		}
		// older versions of ZFS log no "scan setup", the scan starts with the command
		kind := PoolHistoryScrub
		if entry.Event == "zpool resilver" {
			kind = PoolHistoryResilver
		}
		grouper.scan = grouper.open(index, kind)
		grouper.scan.Entries = append(grouper.scan.Entries, entry)
		return true
	case "zpool trim":
		if group := grouper.recent(grouper.trim, grouper.lastTrim, entry); group != nil {
			group.Entries = append(group.Entries, entry)
			return true
		}
	}
	return false
}

// recent returns the running group, or else the last one if it ended shortly before the command (which is logged
// when it ended, e.g. "zpool scrub -s" after "scan cancelled"), nil if neither.
func (grouper *historyGrouper) recent(running *PoolHistoryGroup, last *PoolHistoryGroup, command *PoolHistoryEntry) *PoolHistoryGroup {
	if running != nil {
		return running
	}
	if last != nil && command.Time.Sub(last.End()) <= historyLateCommandWindow {
		return last
	}
	return nil
}

// open adds a new group of the kind, which starts with the record at index.
func (grouper *historyGrouper) open(index int, kind PoolHistoryGroupKind) *PoolHistoryGroup {
	group := &PoolHistoryGroup{Kind: kind, first: index}
	grouper.groups = append(grouper.groups, group)
	return group
}

// endScan ends the running scan, if any.
func (grouper *historyGrouper) endScan(outcome PoolHistoryOutcome) {
	if grouper.scan == nil {
		return
	}
	grouper.scan.Outcome = outcome
	grouper.lastScan, grouper.scan = grouper.scan, nil
}

// addTrimEvent adds a "trim" event to the running TRIM, e.g. "vdev=/dev/sda1 activated": a TRIM runs on each device
// on its own, and ends when it ended on all of them.
func (grouper *historyGrouper) addTrimEvent(index int, entry *PoolHistoryEntry) {
	device := historyValue(entry.Text, "vdev")
	state := entry.Text[strings.LastIndexByte(entry.Text, ' ')+1:]
	if grouper.trim == nil {
		grouper.trim = grouper.open(index, PoolHistoryTrim)
		grouper.trim.trimming = map[string]bool{}
	}
	trim := grouper.trim
	trim.Entries = append(trim.Entries, entry)
	if device != "" && !slices.Contains(trim.Devices, device) {
		trim.Devices = append(trim.Devices, device)
	}
	switch state {
	case "activated", "resumed":
		trim.trimming[device] = true
		return
	case "canceled":
		trim.cancelled = true
	case "suspended":
		trim.suspended = true
	}
	delete(trim.trimming, device)
	if len(trim.trimming) > 0 {
		return
	}
	switch {
	case trim.cancelled:
		trim.Outcome = PoolHistoryCancelled
	case trim.suspended:
		trim.Outcome = PoolHistorySuspended
	default:
		trim.Outcome = PoolHistoryDone
	}
	grouper.lastTrim, grouper.trim = trim, nil
}

// historyValue returns the value of the key in the text of an event, e.g. "2" of "func" in "func=2 mintxg=1", "" if
// it has none.
func historyValue(text string, key string) string {
	for field := range strings.FieldsSeq(text) {
		if value, found := strings.CutPrefix(field, key+"="); found {
			return value
		}
	}
	return ""
}

// finish makes the events that belong to no command records of their own, and marks a scrub whose last command
// paused it.
func (grouper *historyGrouper) finish() {
	for _, event := range grouper.pending {
		grouper.groups = append(grouper.groups, &PoolHistoryGroup{Kind: PoolHistorySingle, first: event.index, Entries: []*PoolHistoryEntry{event.entry}})
	}
	grouper.pending = nil
	if scan := grouper.scan; scan != nil {
		commands := scan.Commands()
		if len(commands) > 0 && slices.Contains(strings.Fields(commands[len(commands)-1].Text), "-p") {
			scan.Outcome = PoolHistoryPaused
		}
	}
	slices.SortFunc(grouper.groups, func(a, b *PoolHistoryGroup) int { return a.first - b.first })
}

// mergeBursts merges the commands one user ran in a row into one group, see historyBurstGap. Events that belong to no
// command join the commands before them, or each other. Scans and TRIMs are left as they are, and do not interrupt
// a run.
func (grouper *historyGrouper) mergeBursts() []*PoolHistoryGroup {
	var result []*PoolHistoryGroup
	var burst *PoolHistoryGroup
	// burstIndex is where burst is in result, burstUser the user of its commands ("" while it has only events)
	var burstIndex int
	var burstUser string
	burstGroups := 0
	for _, group := range grouper.groups {
		if group.IsActivity() {
			result = append(result, group)
			continue
		}
		user := ""
		if commands := group.Commands(); len(commands) > 0 {
			user = commands[len(commands)-1].User
		}
		// events join the run before them, a command only a run of commands of the same user: an event no command
		// claimed says nothing about the command after it
		joins := burst != nil && group.Start().Sub(burst.End()) <= historyBurstGap && (user == "" || user == burstUser)
		if !joins {
			burst, burstIndex, burstUser, burstGroups = group, len(result), user, 1
			result = append(result, group)
			continue
		}
		if burstGroups == 1 {
			// the first group becomes a part of the run, in its place
			merged := &PoolHistoryGroup{Kind: PoolHistoryCommands, first: burst.first, Entries: slices.Clone(burst.Entries)}
			result[burstIndex] = merged
			burst = merged
		}
		burst.Entries = append(burst.Entries, group.Entries...)
		burstGroups++
	}
	return result
}
