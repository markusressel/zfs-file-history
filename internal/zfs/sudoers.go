package zfs

import (
	"fmt"
	"slices"
	"strings"
	"zfs-file-history/internal/disk"
	"zfs-file-history/internal/util"
)

// SudoersChanged is emitted once the user was allowed to run more zpool commands with sudo (see SudoersRule), e.g.
// by the setup command run from the application, so what needs them is started again (see WatchZpoolEvents).
var SudoersChanged = util.NewEmitter[struct{}]()

// SudoersRule is a permission the setup command can grant: commands (of zpool, or smartctl) the user may run with
// sudo without a password. Pool operations cannot be delegated like dataset ones (zfs allow), so this is the only
// way to allow them without starting the application as root.
type SudoersRule struct {
	// Id is the flag of the setup command that grants the rule, without the leading "--"
	Id string
	// Name is the short name of the rule, e.g. for lists
	Name string
	// Summary tells in a few words what the rule is good for
	Summary string
	// Commands names the commands the rule allows, without the paths
	Commands string
	// Description tells what the rule is good for and what it allows
	Description string
	// ReadOnly is set for rules whose commands only read: they are offered as selected
	ReadOnly bool
	// allowed returns the commands the rule allows for one path of its program (zpool, see program)
	allowed func(path string) []string
	// denied returns the commands the rule denies for one path of its program, nil if none
	denied func(path string) []string
	// program is the program of the commands, nil: zpool
	program *sudoersProgram
	// former returns the commands former versions of the rule allowed for one path of its program, nil if none: their
	// lines are dropped from the sudoers file, so the setup command can grant the rule again (see ParseSudoersFile)
	former func(path string) []string
	// fallback returns the commands of a former version of the rule the application still works with (e.g. slower),
	// for one path of its program, nil if none: the rule counts as allowed with them (see AllowedSudoersRules), and
	// the setup command replaces their lines by the ones of the rule (see ParseSudoersFile)
	fallback func(path string) []string
}

// sudoersProgram is a program the commands of a SudoersRule run.
type sudoersProgram struct {
	// paths are the paths it is installed at by the distributions, all of them are allowed
	paths []string
	// path returns the path it has here
	path func() string
	// installed returns whether it is installed, nil: always
	installed func() bool
}

// zpoolProgram is the program of the rules without one.
var zpoolProgram = &sudoersProgram{paths: zpoolPaths, path: ZpoolPath}

// smartctlProgram runs the commands of SudoersRuleSmart.
var smartctlProgram = &sudoersProgram{paths: disk.SmartctlPaths, path: disk.SmartctlPath, installed: func() bool { return disk.SmartctlInstalled() }}

// programOf returns the program of the commands of the rule.
func (rule *SudoersRule) programOf() *sudoersProgram {
	if rule.program == nil {
		return zpoolProgram
	}
	return rule.program
}

// IsAvailable returns whether the program of the rule is installed: rules of others are not offered.
func (rule *SudoersRule) IsAvailable() bool {
	installed := rule.programOf().installed
	return installed == nil || installed()
}

var (
	// SudoersRuleEvents allows to watch the zpool events, see WatchZpoolEvents.
	SudoersRuleEvents = &SudoersRule{
		Id:       "events",
		Name:     "Live updates",
		Summary:  "snapshots and pool changes show up right away",
		Commands: "zpool events -v -f",
		Description: "Show new and destroyed snapshots and changes of the pools (e.g. a failing disk or a finished " +
			"scrub) right away, instead of only after F5. Allows: zpool events -v -f",
		ReadOnly: true,
		allowed:  func(zpool string) []string { return []string{zpool + " events -v -f"} },
	}
	// SudoersRuleDataErrors allows to list the damaged files, see ListDataErrors. The arguments are given exactly,
	// so no others (e.g. "status -c", which runs scripts) are allowed.
	SudoersRuleDataErrors = &SudoersRule{
		Id:       "data-errors",
		Name:     "Damaged files",
		Summary:  "list the files that data errors damaged",
		Commands: "zpool " + strings.Join(dataErrorsArgs, " "),
		Description: "Show which files are damaged if ZFS finds data errors (only root may list them). " +
			"Allows: zpool " + strings.Join(dataErrorsArgs, " "),
		ReadOnly: true,
		allowed:  func(zpool string) []string { return []string{zpool + " " + strings.Join(dataErrorsArgs, " ")} },
	}
	// SudoersRuleHistory allows to read the history of a pool, see PoolHistory. The pool name has to start with a
	// letter, so no options can be given in its place (in sudoers, * also matches spaces, so more pool names may
	// follow, which only read more). Former versions allowed only the history of all pools, which takes much longer
	// to read with several pools; it still works.
	SudoersRuleHistory = &SudoersRule{
		Id:       "history",
		Name:     "Pool history",
		Summary:  "what was done to the pools and datasets, when and by whom",
		Commands: "zpool " + strings.Join(historyArgs, " ") + " <pool>",
		Description: "Show the history of the pools and datasets (only root may read it): the commands that " +
			"changed them, e.g. created or destroyed snapshots, with their time and user, and what ZFS did itself, " +
			"e.g. scrubs. Allows: zpool " + strings.Join(historyArgs, " ") + " <pool>",
		ReadOnly: true,
		allowed: func(zpool string) []string {
			return []string{zpool + " " + strings.Join(historyArgs, " ") + " [a-zA-Z]*"}
		},
		fallback: func(zpool string) []string { return []string{zpool + " " + strings.Join(historyArgs, " ")} },
	}
	// SudoersRulePoolActions allows to run the pool actions, see PoolActions. The pool name has to start with a
	// letter, so no options can be given in its place. Further arguments are harmless for scrub and trim, but would
	// allow "clear -F" (which discards transactions), so clear is denied with more than one argument (in sudoers, *
	// also matches spaces, and the last matching rule wins).
	SudoersRulePoolActions = &SudoersRule{
		Id:       "pool-actions",
		Name:     "Pool actions",
		Summary:  "scrub, TRIM and clear the errors of pools",
		Commands: "zpool scrub, trim and clear",
		Description: "Start, pause and cancel scrubs, start and cancel TRIMs, and clear the errors of pools. " +
			"Nothing else, in particular no destroying, exporting or rewinding of pools. " +
			"Allows: zpool scrub, trim and clear",
		allowed: func(zpool string) []string {
			commands := make([]string, 0, len(PoolActions))
			for _, action := range PoolActions {
				commands = append(commands, fmt.Sprintf("%s %s [a-zA-Z]*", zpool, strings.Join(action.Args, " ")))
			}
			return commands
		},
		denied: func(zpool string) []string {
			return []string{fmt.Sprintf("%s %s * *", zpool, strings.Join(PoolActionClear.Args, " "))}
		},
	}

	// SudoersRuleSmart allows to read the SMART values of the disks with smartctl (see disk.SmartReader). Any device
	// below /dev may be given, nothing else: "--" ends the options (see disk.SmartctlArgs), and smartctl refuses
	// more than one device, so no other options (e.g. self-tests, or switching SMART off) can be added behind it.
	SudoersRuleSmart = &SudoersRule{
		Id:       "smart",
		Name:     "Disk health",
		Summary:  "SMART values of the disks of the pools",
		Commands: "smartctl " + strings.Join(disk.SmartctlArgs, " ") + " /dev/*",
		Description: "Show the SMART values of the disks of the pools (only root may read them), and warn if a disk " +
			"fails or wears out. Sleeping disks are not woken up. Allows: smartctl " +
			strings.Join(disk.SmartctlArgs, " ") + " /dev/*",
		ReadOnly: true,
		allowed: func(smartctl string) []string {
			return []string{smartctl + " " + strings.Join(disk.SmartctlArgs, " ") + " /dev/*"}
		},
		program: smartctlProgram,
		// without -i, before the model and serial number were shown
		former: func(smartctl string) []string { return []string{smartctl + " -j -H -A -n standby -- /dev/*"} },
	}

	// SudoersRules are all rules, in the order they are offered and written.
	SudoersRules = []*SudoersRule{SudoersRuleEvents, SudoersRuleDataErrors, SudoersRuleHistory, SudoersRuleSmart, SudoersRulePoolActions}
)

// SudoersRuleById returns the rule with the given id, nil if there is none.
func SudoersRuleById(id string) *SudoersRule {
	for _, rule := range SudoersRules {
		if rule.Id == id {
			return rule
		}
	}
	return nil
}

// Lines returns the sudoers lines that grant the rule to the user, for all paths its program may be installed at.
func (rule *SudoersRule) Lines(user string) string {
	var allowed, denied []string
	for _, path := range rule.programOf().paths {
		allowed = append(allowed, rule.allowed(path)...)
		if rule.denied != nil {
			for _, command := range rule.denied(path) {
				denied = append(denied, "!"+command)
			}
		}
	}
	lines := fmt.Sprintf("%s ALL=(root) NOPASSWD: %s\n", user, strings.Join(allowed, ", "))
	if len(denied) > 0 {
		lines += fmt.Sprintf("%s ALL=(root) %s\n", user, strings.Join(denied, ", "))
	}
	return lines
}

// matches returns whether the sudoers line was written for the rule (for any path of its program, also by older
// versions).
func (rule *SudoersRule) matches(line string) bool {
	for _, path := range rule.programOf().paths {
		commands := rule.allowed(path)
		if rule.denied != nil {
			commands = append(commands, rule.denied(path)...)
		}
		for _, command := range commands {
			if containsCommand(line, command) {
				return true
			}
		}
	}
	return false
}

// matchesFormer returns whether the sudoers line was written for a former version of the rule, see
// SudoersRule.former.
func (rule *SudoersRule) matchesFormer(line string) bool {
	if rule.former == nil {
		return false
	}
	for _, path := range rule.programOf().paths {
		for _, command := range rule.former(path) {
			if containsCommand(line, command) {
				return true
			}
		}
	}
	return false
}

// matchesFallback returns whether the sudoers line was written for the fallback of the rule, see
// SudoersRule.fallback.
func (rule *SudoersRule) matchesFallback(line string) bool {
	if rule.fallback == nil {
		return false
	}
	for _, path := range rule.programOf().paths {
		for _, command := range rule.fallback(path) {
			if containsCommand(line, command) {
				return true
			}
		}
	}
	return false
}

// containsCommand returns whether the command is one of the commands of the sudoers line, which are separated by
// commas and may be negated.
func containsCommand(line string, command string) bool {
	_, commands, found := strings.Cut(line, ")")
	if !found {
		return false
	}
	for _, candidate := range strings.Split(commands, ",") {
		candidate, _, _ = splitSudoersTags(candidate)
		if strings.TrimPrefix(candidate, "!") == command {
			return true
		}
	}
	return false
}

// splitSudoersTags removes the tags (e.g. "NOPASSWD:") in front of a sudoers command. hasPasswordTag is set if one
// of them is NOPASSWD or PASSWD, noPassword to whether the last of these allows the command without a password.
func splitSudoersTags(command string) (rest string, hasPasswordTag bool, noPassword bool) {
	rest = strings.TrimSpace(command)
	for {
		tag, after, found := strings.Cut(rest, ":")
		if !found || tag == "" || strings.ContainsFunc(tag, func(r rune) bool { return (r < 'A' || r > 'Z') && r != '_' }) {
			return rest, hasPasswordTag, noPassword
		}
		if tag == "NOPASSWD" || tag == "PASSWD" {
			hasPasswordTag, noPassword = true, tag == "NOPASSWD"
		}
		rest = strings.TrimSpace(after)
	}
}

// sudoersFileHeader is the first line of the sudoers file written by the setup command.
const sudoersFileHeader = "# installed by zfs-file-history setup, see \"zfs-file-history setup --help\"\n"

// SudoersFile is the content of the sudoers file of the setup command: the rules granted to each user. The setup
// command adds rules to it, so running it again (e.g. for another rule) keeps the ones granted before.
type SudoersFile struct {
	// users are the users with rules, in the order they were granted
	users []string
	// granted are the ids of the rules granted to each user
	granted map[string]map[string]bool
	// other are the lines that do not belong to a rule (e.g. added by hand), kept as they are
	other []string
}

// ParseSudoersFile reads the content of the sudoers file of the setup command, also of older versions (which wrote
// the zpool events and damaged files into one line).
func ParseSudoersFile(content string) *SudoersFile {
	file := &SudoersFile{granted: map[string]map[string]bool{}}
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		user, _, _ := strings.Cut(trimmed, " ")
		matched := false
		for _, rule := range SudoersRules {
			if rule.matches(trimmed) || rule.matchesFallback(trimmed) {
				// the lines of a fallback are written as the ones of the rule
				file.Grant(user, rule)
				matched = true
			} else if rule.matchesFormer(trimmed) {
				// dropped: it no longer allows what the application runs
				matched = true
			}
		}
		if !matched {
			file.other = append(file.other, line)
		}
	}
	return file
}

// Has returns whether the rule is granted to the user.
func (file *SudoersFile) Has(user string, rule *SudoersRule) bool {
	return file.granted[user][rule.Id]
}

// Grant adds the rule for the user.
func (file *SudoersFile) Grant(user string, rule *SudoersRule) {
	if file.granted[user] == nil {
		file.granted[user] = map[string]bool{}
		file.users = append(file.users, user)
	}
	file.granted[user][rule.Id] = true
}

// String returns the content of the file: the rules of each user, with the current paths of zpool and arguments.
func (file *SudoersFile) String() string {
	var content strings.Builder
	content.WriteString(sudoersFileHeader)
	for _, user := range file.users {
		for _, rule := range SudoersRules {
			if file.Has(user, rule) {
				fmt.Fprintf(&content, "# %s\n%s", rule.Name, rule.Lines(user))
			}
		}
	}
	for _, line := range file.other {
		content.WriteString(line + "\n")
	}
	return content.String()
}

// AllowedCommands returns the commands the rule allows, with the path its program has here.
func (rule *SudoersRule) AllowedCommands() []string {
	return rule.allowed(rule.programOf().path())
}

// isAllowedBy returns whether the commands (see parseNoPasswordCommands) allow the rule: all of its commands, or all
// of the ones of its fallback.
func (rule *SudoersRule) isAllowedBy(commands map[string]bool) bool {
	all := func(required []string) bool {
		return !slices.ContainsFunc(required, func(command string) bool { return !commands[command] })
	}
	path := rule.programOf().path()
	return all(rule.allowed(path)) || (rule.fallback != nil && all(rule.fallback(path)))
}

// AllowedSudoersRules returns the ids of the rules the user may use (all as root). It runs sudo, so it must not be
// called on the UI thread.
func AllowedSudoersRules() map[string]bool {
	return backend.AllowedSudoersRules()
}

// parseNoPasswordCommands returns the commands that "sudo -l" says the user may run as root without a password, and
// whether all are (NOPASSWD: ALL). Commands of an entry are separated by commas, long entries are continued on
// lines with a deeper indentation.
func parseNoPasswordCommands(output string) (commands map[string]bool, all bool) {
	commands = map[string]bool{}
	var entries []string
	inCommands := false
	for _, line := range strings.Split(output, "\n") {
		switch {
		case strings.Contains(line, "may run the following commands"):
			inCommands = true
		case !inCommands || strings.TrimSpace(line) == "":
		case strings.HasPrefix(strings.TrimSpace(line), "("):
			entries = append(entries, strings.TrimSpace(line))
		case len(entries) > 0 && strings.HasPrefix(line, " "):
			entries[len(entries)-1] += " " + strings.TrimSpace(line)
		default:
			// e.g. the commands of another section
			inCommands = false
		}
	}
	for _, entry := range entries {
		runAs, rest, found := strings.Cut(strings.TrimPrefix(entry, "("), ")")
		if !found {
			continue
		}
		users, _, _ := strings.Cut(runAs, ":")
		if !slices.ContainsFunc(strings.Split(users, ","), func(user string) bool {
			user = strings.TrimSpace(user)
			return user == "root" || user == "ALL"
		}) {
			continue
		}
		// a tag applies to the following commands of the entry, until another one
		noPassword := false
		for _, command := range strings.Split(rest, ",") {
			command, hasPasswordTag, tagNoPassword := splitSudoersTags(command)
			if hasPasswordTag {
				noPassword = tagNoPassword
			}
			if !noPassword || strings.HasPrefix(command, "!") {
				continue
			}
			if command == "ALL" {
				all = true
			}
			commands[command] = true
		}
	}
	return commands, all
}
