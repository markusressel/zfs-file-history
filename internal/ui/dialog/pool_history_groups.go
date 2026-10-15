package dialog

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	"zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// poolHistoryGroupCell returns the cell of the row of a group in the column: when it started, what it was, the
// dataset of all its records, who ran its commands, how it ended (or what its commands did). A collapsed group
// says how many records it hides, like a collapsed dataset of the tree.
func poolHistoryGroupCell(group *zfs.PoolHistoryGroup, column int, expanded bool) *tview.TableCell {
	switch column {
	case 0:
		return tview.NewTableCell(group.Start().Format(poolHistoryTimeLayout)).SetTextColor(theme.Colors.Info.Label)
	case 1:
		text := tview.Escape(poolHistoryGroupTitle(group))
		if !expanded {
			text += txwidgets.Span(theme.Colors.Layout.Table.TreeCollapsedIndicator, " ▸ +%d", len(group.Entries))
		}
		return tview.NewTableCell(text).SetTextColor(theme.Colors.Info.Value)
	case 2:
		return tview.NewTableCell(tview.Escape(poolHistoryCommonDataset(group))).SetTextColor(theme.Colors.Layout.Table.Accent).SetMaxWidth(50)
	case 3:
		return tview.NewTableCell(tview.Escape(strings.Join(poolHistoryGroupUsers(group), ", "))).SetTextColor(theme.Colors.Info.Label)
	default:
		summary, color := poolHistoryGroupSummary(group)
		return tview.NewTableCell(tview.Escape(summary)).SetTextColor(color).SetExpansion(1)
	}
}

// poolHistoryGroupTitle returns what the group is: the kind of a scan or TRIM, the command of a command with its
// events, or the commands of a run by how often each was run, e.g. "zfs snapshot, send, set ×2, destroy" (events if
// it has no commands).
func poolHistoryGroupTitle(group *zfs.PoolHistoryGroup) string {
	switch group.Kind {
	case zfs.PoolHistoryScrub:
		return "Scrub"
	case zfs.PoolHistoryResilver:
		return "Resilver"
	case zfs.PoolHistoryScan:
		return "Scrub or resilver"
	case zfs.PoolHistoryTrim:
		return "TRIM"
	}
	entries := group.Commands()
	if len(entries) == 0 {
		entries = group.Entries
	}
	var events []string
	counts := map[string]int{}
	for _, entry := range entries {
		if counts[entry.Event] == 0 {
			events = append(events, entry.Event)
		}
		counts[entry.Event]++
	}
	var parts []string
	program := ""
	for _, event := range events {
		// "zfs snapshot, send" instead of "zfs snapshot, zfs send"
		name := event
		if before, after, found := strings.Cut(event, " "); found && before == program {
			name = after
		} else if found {
			program = before
		}
		if counts[event] > 1 {
			name += fmt.Sprintf(" ×%d", counts[event])
		}
		parts = append(parts, name)
	}
	return strings.Join(parts, ", ")
}

// poolHistoryGroupSummary returns how a scan or TRIM ended (in the color of how well), or what the commands of the
// group did: the arguments of a command and how many events ZFS logged for it, how many commands and events a run
// has and how long it took.
func poolHistoryGroupSummary(group *zfs.PoolHistoryGroup) (string, tcell.Color) {
	duration := formatHistoryDuration(group.End().Sub(group.Start()))
	commands := group.Commands()
	events := len(group.Entries) - len(commands)
	switch {
	case group.IsActivity():
		return poolHistoryActivitySummary(group, duration)
	case group.Kind == zfs.PoolHistoryCommand:
		return fmt.Sprintf("%s · %d %s", poolHistoryDetails(commands[0]), events, util.Plural(events, "event", "events")),
			theme.Colors.Info.Value
	case len(commands) == 0:
		return fmt.Sprintf("%d events in %s", events, duration), theme.Colors.Info.Muted
	case events == 0:
		return fmt.Sprintf("%d commands in %s", len(commands), duration), theme.Colors.Info.Value
	}
	return fmt.Sprintf("%d %s, %d %s in %s", len(commands), util.Plural(len(commands), "command", "commands"), events,
		util.Plural(events, "event", "events"), duration), theme.Colors.Info.Value
}

// poolHistoryActivitySummary returns how a scan or TRIM ended, after how long, with how many errors (of a scan) on
// how many devices (of a TRIM), in the color of how well.
func poolHistoryActivitySummary(group *zfs.PoolHistoryGroup, duration string) (string, tcell.Color) {
	var text string
	color := theme.Colors.Info.Value
	switch group.Outcome {
	case zfs.PoolHistoryDone:
		text, color = "done in "+duration, theme.Colors.Info.Ok
		if group.Kind != zfs.PoolHistoryTrim && group.Errors > 0 {
			text, color = fmt.Sprintf("%s, %d %s", text, group.Errors, util.Plural(int(group.Errors), "error", "errors")), theme.Colors.Info.Error
		} else if group.Kind != zfs.PoolHistoryTrim {
			text += ", no errors"
		}
	case zfs.PoolHistoryCancelled:
		text = "cancelled after " + duration
	case zfs.PoolHistoryRestarted:
		text, color = "restarted after "+duration, theme.Colors.Info.Warning
	case zfs.PoolHistorySuspended:
		text, color = "suspended after "+duration, theme.Colors.Info.Warning
	case zfs.PoolHistoryPaused:
		text, color = "paused", theme.Colors.Info.Warning
	case zfs.PoolHistoryRunning:
		text = "running"
	default:
		text, color = "end not logged", theme.Colors.Info.Muted
	}
	if devices := len(group.Devices); devices > 0 {
		text += fmt.Sprintf(", %d %s", devices, util.Plural(devices, "device", "devices"))
	}
	return text, color
}

// poolHistoryGroupDetails returns the group in full, for below the list: when it started and ended, what it was, how
// it ended, who ran its commands, on which devices a TRIM ran.
func poolHistoryGroupDetails(group *zfs.PoolHistoryGroup) string {
	end := group.End().Format(poolHistoryTimeLayout)
	if sameDay(group.Start(), group.End()) {
		end = group.End().Format(time.TimeOnly)
	}
	label := fmt.Sprintf("%s – %s, %d records", group.Start().Format(poolHistoryTimeLayout), end, len(group.Entries))
	if users := poolHistoryGroupUsers(group); len(users) > 0 {
		label += ", " + strings.Join(users, ", ")
	}
	text := poolHistoryGroupTitle(group)
	if dataset := poolHistoryCommonDataset(group); dataset != "" {
		text += " of " + dataset
	}
	summary, _ := poolHistoryGroupSummary(group)
	text += ", " + summary
	if len(group.Devices) > 0 {
		text += " (" + strings.Join(group.Devices, ", ") + ")"
	}
	return txwidgets.Span(theme.Colors.Info.Label, "%s: ", label) + txwidgets.Span(theme.Colors.Info.Value, "%s", text)
}

// sameDay returns whether both times are on the same day.
func sameDay(a time.Time, b time.Time) bool {
	aYear, aMonth, aDay := a.Date()
	bYear, bMonth, bDay := b.Date()
	return aYear == bYear && aMonth == bMonth && aDay == bDay
}

// poolHistoryGroupUsers returns the users who ran the commands of the group, in the order they first did.
func poolHistoryGroupUsers(group *zfs.PoolHistoryGroup) []string {
	var users []string
	for _, command := range group.Commands() {
		if command.User != "" && !slices.Contains(users, command.User) {
			users = append(users, command.User)
		}
	}
	return users
}

// poolHistoryCommonDataset returns the dataset all records of the group are about: the name they all share, else the
// dataset all of them are in (e.g. "rpool/docker" of "rpool/docker/a@s" and "rpool/docker/b"), "" if none.
func poolHistoryCommonDataset(group *zfs.PoolHistoryGroup) string {
	common, found := "", false
	for _, entry := range group.Entries {
		for _, name := range entry.Datasets {
			switch {
			case !found:
				common, found = name, true
			case name != common:
				common = commonParentDataset(common, name)
			}
		}
	}
	return common
}

// commonParentDataset returns the dataset both names are in or are, without a snapshot or bookmark, "" if none.
func commonParentDataset(a string, b string) string {
	a, _, _ = strings.Cut(strings.ReplaceAll(a, "#", "@"), "@")
	b, _, _ = strings.Cut(strings.ReplaceAll(b, "#", "@"), "@")
	aParts, bParts := strings.Split(a, "/"), strings.Split(b, "/")
	shared := 0
	for shared < len(aParts) && shared < len(bParts) && aParts[shared] == bParts[shared] {
		shared++
	}
	return strings.Join(aParts[:shared], "/")
}

// formatHistoryDuration formats how long a group took in its two largest units, e.g. "42s", "7m 24s", "2h 10m",
// "3d 07h".
func formatHistoryDuration(duration time.Duration) string {
	seconds := int64(max(duration, 0) / time.Second)
	days, hours, minutes := seconds/86400, seconds%86400/3600, seconds%3600/60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %02dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %02dm", hours, minutes)
	case minutes > 0:
		return fmt.Sprintf("%dm %02ds", minutes, seconds%60)
	}
	return fmt.Sprintf("%ds", seconds)
}
