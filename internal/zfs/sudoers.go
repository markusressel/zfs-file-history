package zfs

import (
	"fmt"
	"slices"
	"strings"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/util"
)

// SudoersChanged is emitted once the user was allowed to run more zpool commands with sudo (see SudoersRule), e.g.
// by the setup command run from the application, so what needs them is started again (see WatchZpoolEvents).
var SudoersChanged = util.NewEmitter[struct{}]()

// SudoersRule is a permission the setup command can grant: zpool commands the user may run with sudo without a
// password. Pool operations cannot be delegated like dataset ones (zfs allow), so this is the only way to allow
// them without starting the application as root.
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
	// allowed returns the commands the rule allows for one path of zpool
	allowed func(zpool string) []string
	// denied returns the commands the rule denies for one path of zpool, nil if none
	denied func(zpool string) []string
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

	// SudoersRules are all rules, in the order they are offered and written.
	SudoersRules = []*SudoersRule{SudoersRuleEvents, SudoersRuleDataErrors, SudoersRulePoolActions}
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

// Lines returns the sudoers lines that grant the rule to the user, for all paths zpool may be installed at.
func (rule *SudoersRule) Lines(user string) string {
	var allowed, denied []string
	for _, path := range zpoolPaths {
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

// matches returns whether the sudoers line was written for the rule (for any path of zpool, also by older versions).
func (rule *SudoersRule) matches(line string) bool {
	for _, path := range zpoolPaths {
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
			if rule.matches(trimmed) {
				file.Grant(user, rule)
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

// listSudoRules lists the commands the user may run with sudo ("sudo -l"). Without a password (-n), which works if
// any of them is allowed without one. Unlike running a command, this is not logged. Replaceable in tests.
var listSudoRules = func() (string, error) { return runSudoCommand("-n", "-l") }

// AllowedSudoersRules returns the ids of the rules the user may use (all as root): the ones whose commands sudo
// allows without a password, with the path zpool has here. It runs sudo, so it must not be called on the UI thread.
func AllowedSudoersRules() map[string]bool {
	allowed := map[string]bool{}
	if isRoot() {
		for _, rule := range SudoersRules {
			allowed[rule.Id] = true
		}
		return allowed
	}
	output, err := listSudoRules()
	if err != nil {
		// e.g. "a password is required" if nothing is allowed without one, or no sudo
		logging.Debug("Cannot list the sudo rules: %v", err)
		return allowed
	}
	commands, all := parseNoPasswordCommands(output)
	for _, rule := range SudoersRules {
		allowed[rule.Id] = all || !slices.ContainsFunc(rule.allowed(ZpoolPath()), func(command string) bool {
			return !commands[command]
		})
	}
	return allowed
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
