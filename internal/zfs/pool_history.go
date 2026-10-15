package zfs

import (
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// historyArgs are the arguments of "zpool history" that list the history of all pools, with the events ZFS logs
// itself (-i) and who did what on which host (-l). Exactly these are allowed by the sudoers rule of the setup
// command, see SudoersRuleHistory.
var historyArgs = []string{"history", "-il"}

// SetupHistoryHint tells how to allow reading the history of the pools without starting the application as root.
const SetupHistoryHint = "To see the history of the pools, run: sudo zfs-file-history setup --history"

// historyTimeLayout is the time of a record, in local time (strftime "%F.%T").
const historyTimeLayout = "2006-01-02.15:04:05"

// PoolHistoryEntry is a record of the history of a pool, see PoolHistory: a command that changed the pool (as typed,
// e.g. "zfs snapshot -r tank@daily"), or an event ZFS logged itself (Internal), e.g. each snapshot a recursive
// command created, a destroyed snapshot, or the end of a scrub.
type PoolHistoryEntry struct {
	Pool string
	Time time.Time
	// Internal is set for the events ZFS logged itself, not commands
	Internal bool
	// Event is the program and subcommand of a command (e.g. "zfs snapshot"), or the kind of an internal event (e.g.
	// "snapshot", "destroy", "scan done")
	Event string
	// Datasets are the datasets (or snapshots, bookmarks) the record is about: of an internal event the one it names
	// (and the origin of a clone), of a command the ones in its arguments, but not the name of the pool alone (it is
	// also the name of the pool's root dataset, the argument of zpool commands)
	Datasets []string
	// Text is the whole command line of a command, or what an internal event says besides its kind and dataset (e.g.
	// "compression=15", "errors=0")
	Text string
	// Txg is the transaction group of an internal event, 0 for commands
	Txg uint64
	// User is the name of the user who ran a command (or the uid, if the name is not known), "" for internal events
	User string
	// Host is the host the record was written on
	Host string
}

// Dataset returns the first dataset the record is about, "" if none.
func (entry *PoolHistoryEntry) Dataset() string {
	if len(entry.Datasets) == 0 {
		return ""
	}
	return entry.Datasets[0]
}

// IsAbout returns whether the record is about the dataset, one of its snapshots or bookmarks, or a dataset below it.
func (entry *PoolHistoryEntry) IsAbout(dataset string) bool {
	return slices.ContainsFunc(entry.Datasets, func(name string) bool {
		if !strings.HasPrefix(name, dataset) {
			return false
		}
		rest := name[len(dataset):]
		return rest == "" || strings.ContainsRune("/@#", rune(rest[0]))
	})
}

// PoolHistory returns the history of all pools ("zpool history -il"), oldest first per pool. ZFS keeps it in a ring
// buffer on the pool: the oldest records are dropped once it is full, except the creation of the pool. Only root
// may read it, so it runs with "sudo -n" (see SudoersRuleHistory) unless the application runs as root. Runs
// zpool, so it must not be called on the UI thread.
func PoolHistory() ([]PoolHistoryEntry, error) {
	return backend.PoolHistory()
}

// historyMultiWordEvents are the internal events whose kind is more than one word. The others are the first word.
var historyMultiWordEvents = []string{"scan aborted, restarting", "starting deferred resilver", "scan setup", "scan done",
	"scan cancelled", "clone swap", "permission update"}

// parsePoolHistory parses the output of "zpool history -il". Records of ioctls are left out: they repeat the
// command logged next to them, and their arguments are dumped as they are, including binary data (e.g. the key of
// "zfs load-key"). Lines that are not the start of a record belong to the record before. A pool may have hundreds of
// thousands of records, so the lines are parsed without regular expressions.
func parsePoolHistory(output string) []PoolHistoryEntry {
	var entries []PoolHistoryEntry
	pool := ""
	for line := range strings.SplitSeq(output, "\n") {
		if name, found := strings.CutPrefix(line, "History for '"); found {
			pool = strings.TrimSuffix(name, "':")
			continue
		}
		// e.g. "2026-10-15.10:14:22 zpool scrub rpool", ioctls with their duration: "... (771ms) ioctl scrub"
		if pool == "" || len(line) <= len(historyTimeLayout) || line[len(historyTimeLayout)] != ' ' || line[0] < '0' || line[0] > '9' {
			continue
		}
		recorded, err := time.ParseInLocation(historyTimeLayout, line[:len(historyTimeLayout)], time.Local)
		if err != nil {
			continue
		}
		rest := line[len(historyTimeLayout)+1:]
		if strings.HasPrefix(rest, "(") {
			if _, after, found := strings.Cut(rest, "ms) "); found {
				rest = after
			}
		}
		if strings.HasPrefix(rest, "ioctl ") || strings.HasPrefix(rest, "unrecognized record") {
			continue
		}
		entry := PoolHistoryEntry{Pool: pool, Time: recorded}
		rest = cutWho(&entry, rest)
		rest = printable(rest)
		if txg, found := strings.CutPrefix(rest, "[txg:"); found {
			number, text, _ := strings.Cut(txg, "] ")
			entry.Internal = true
			entry.Txg, _ = strconv.ParseUint(number, 10, 64)
			parseInternalEvent(&entry, strings.TrimSpace(text))
		} else {
			entry.Text = strings.TrimSpace(rest)
			parseCommand(&entry)
		}
		entries = append(entries, entry)
	}
	return entries
}

// cutWho sets the user and host from what -l adds at the end of a record, e.g. " [user 0 (root) on host:linux]" of a
// command, " [on host]" of an internal event, and returns the record without it.
func cutWho(entry *PoolHistoryEntry, record string) string {
	if !strings.HasSuffix(record, "]") {
		return record
	}
	start := strings.LastIndex(record, " [")
	if start < 0 {
		return record
	}
	who := record[start+2 : len(record)-1]
	if !strings.HasPrefix(who, "user ") && !strings.HasPrefix(who, "on ") && who != "" {
		return record
	}
	if user, found := strings.CutPrefix(who, "user "); found {
		uid, rest, _ := strings.Cut(user, " ")
		entry.User = uid
		if name, after, found := strings.Cut(strings.TrimPrefix(rest, "("), ") "); found && strings.HasPrefix(rest, "(") {
			entry.User, rest = name, after
		}
		who = rest
	}
	if host, found := strings.CutPrefix(who, "on "); found {
		// without the zone, e.g. "linux"
		entry.Host, _, _ = strings.Cut(host, ":")
	}
	return record[:start]
}

// parseInternalEvent sets the kind, datasets and text of an internal event, e.g. "snapshot rpool/data@daily (1548)"
// or "clone rpool/b (2337) origin=rpool/a@s (3385)" or "scan done errors=0".
func parseInternalEvent(entry *PoolHistoryEntry, text string) {
	if names, first, last := internalDatasets(text); len(names) > 0 && first > 0 {
		entry.Event, entry.Datasets, entry.Text = strings.TrimSpace(text[:first]), names, strings.TrimSpace(text[last:])
		return
	}
	for _, event := range historyMultiWordEvents {
		if rest, found := strings.CutPrefix(text, event); found && (rest == "" || rest[0] == ' ') {
			entry.Event, entry.Text = event, strings.TrimSpace(rest)
			return
		}
	}
	entry.Event, entry.Text, _ = strings.Cut(text, " ")
}

// internalDatasets returns the datasets an internal event names: each is followed by its object id in parentheses,
// e.g. "rpool/data@daily (1548)", or "origin=rpool/a@s (3385)" of a clone. first is where the first starts, last
// where the id of the first ends.
func internalDatasets(text string) (names []string, first int, last int) {
	offset := 0
	for {
		index := strings.Index(text[offset:], " (")
		if index < 0 {
			return names, first, last
		}
		index += offset
		closing := strings.IndexByte(text[index+2:], ')')
		if closing <= 0 || strings.ContainsFunc(text[index+2:index+2+closing], func(r rune) bool { return r < '0' || r > '9' }) {
			offset = index + 2
			continue
		}
		start := strings.LastIndexAny(text[:index], " =") + 1
		if start < index {
			if len(names) == 0 {
				first, last = start, index+3+closing
			}
			names = append(names, text[start:index])
		}
		offset = index + 3 + closing
	}
}

// parseCommand sets the event (the program and subcommand, e.g. "zfs snapshot") and the datasets of a command: the
// arguments that are names in the pool, e.g. "rpool/data@daily", but not the pool itself.
func parseCommand(entry *PoolHistoryEntry) {
	fields := strings.Fields(entry.Text)
	entry.Event = strings.Join(fields[:min(len(fields), 2)], " ")
	for _, field := range fields[min(len(fields), 2):] {
		// e.g. "origin=rpool/a@s" of a property
		_, value, isProperty := strings.Cut(field, "=")
		if isProperty {
			field = value
		}
		if len(field) > len(entry.Pool) && strings.HasPrefix(field, entry.Pool) && strings.ContainsRune("/@#", rune(field[len(entry.Pool)])) {
			entry.Datasets = append(entry.Datasets, field)
		}
	}
}

// printable replaces what cannot be shown (invalid UTF-8, control characters) with "?", e.g. a property value set to
// binary data.
func printable(text string) string {
	if utf8.ValidString(text) && !strings.ContainsFunc(text, unicode.IsControl) {
		return text
	}
	return strings.Map(func(r rune) rune {
		if r == utf8.RuneError || unicode.IsControl(r) {
			return '?'
		}
		return r
	}, strings.ToValidUTF8(text, "?"))
}
