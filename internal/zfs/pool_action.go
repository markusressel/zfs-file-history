package zfs

import (
	"fmt"
	"slices"
	"strings"
)

// PoolAction is an operation on a pool that can be started from the application, see RunPoolAction. Operations
// that lose data or make the pool unavailable (destroy, export, "clear -F", ...) are deliberately not offered.
// Upgrading cannot be undone, the application only runs it once the name of the pool was typed.
type PoolAction struct {
	// Name is what is done, e.g. "start a scrub", used in error messages
	Name string
	// Done tells that it was done, e.g. "Started a scrub"
	Done string
	// Args are the arguments of zpool in front of the name of the pool, e.g. ["scrub", "-s"]
	Args []string
}

var (
	// PoolActionScrub starts a scrub, or resumes a paused one.
	PoolActionScrub = PoolAction{Name: "start a scrub", Done: "Started a scrub", Args: []string{"scrub"}}
	// PoolActionPauseScrub pauses the running scrub. Starting a scrub again resumes it.
	PoolActionPauseScrub = PoolAction{Name: "pause the scrub", Done: "Paused the scrub", Args: []string{"scrub", "-p"}}
	// PoolActionCancelScrub stops the running or paused scrub. It cannot be resumed.
	PoolActionCancelScrub = PoolAction{Name: "cancel the scrub", Done: "Canceled the scrub", Args: []string{"scrub", "-s"}}
	// PoolActionTrim starts a TRIM of all devices that support it.
	PoolActionTrim = PoolAction{Name: "start a TRIM", Done: "Started a TRIM", Args: []string{"trim"}}
	// PoolActionCancelTrim stops the running TRIM.
	PoolActionCancelTrim = PoolAction{Name: "cancel the TRIM", Done: "Canceled the TRIM", Args: []string{"trim", "-c"}}
	// PoolActionClear resets the error counters of the devices, and lets ZFS retry devices that failed.
	PoolActionClear = PoolAction{Name: "clear the errors", Done: "Cleared the errors", Args: []string{"clear"}}
	// PoolActionUpgrade enables all features the installed ZFS supports (see PoolStatus.CanUpgrade). It cannot be
	// undone: software that does not support the features (e.g. an older ZFS, or a boot loader) may no longer be
	// able to access the pool.
	PoolActionUpgrade = PoolAction{Name: "upgrade the pool", Done: "Enabled all features", Args: []string{"upgrade"}}

	// PoolActions are all actions, e.g. for the sudoers rule of the setup command, see SudoersRulePoolActions.
	PoolActions = []PoolAction{PoolActionScrub, PoolActionPauseScrub, PoolActionCancelScrub, PoolActionTrim, PoolActionCancelTrim, PoolActionClear, PoolActionUpgrade}
)

// SetupPoolActionsHint tells how to allow the pool actions without starting the application as root.
const SetupPoolActionsHint = "To allow it without a password, run: sudo zfs-file-history setup --pool-actions"

// PoolActionNeedsRootError is returned by RunPoolAction if zpool denied the action, and sudo did not allow running
// it either (e.g. a password is required). The pool actions need root, ZFS cannot delegate them to users like it
// does for datasets (zfs allow).
type PoolActionNeedsRootError struct {
	// Action is the denied action. If Then has more, its Name and Done describe all of them.
	Action PoolAction
	// Then are the actions that were to run after Action, which need root as well, e.g. the other features to
	// enable (see EnablePoolFeatures)
	Then []PoolAction
	Pool string
	// Cause is the error of the denied zpool command
	Cause error
}

func (e *PoolActionNeedsRootError) Error() string {
	if !e.AllowedBySetup() {
		return fmt.Sprintf("cannot %s on %s, it needs root: %s", e.Action.Name, e.Pool, e.Cause)
	}
	return fmt.Sprintf("cannot %s on %s, it needs root: %s\n\n%s", e.Action.Name, e.Pool, e.Cause, SetupPoolActionsHint)
}

func (e *PoolActionNeedsRootError) Unwrap() error {
	return e.Cause
}

// Commands returns the zpool commands of the actions (with the full path, as needed by sudo), e.g.
// [["/usr/sbin/zpool", "scrub", "tank"]].
func (e *PoolActionNeedsRootError) Commands() [][]string {
	var commands [][]string
	for _, action := range append([]PoolAction{e.Action}, e.Then...) {
		commands = append(commands, append(append([]string{ZpoolPath()}, action.Args...), e.Pool))
	}
	return commands
}

// AllowedBySetup returns whether the sudoers rule of "setup --pool-actions" allows all the actions. It does not
// allow enabling single features (PoolActionEnableFeature): the rule would need a wildcard for the name of the
// feature, and sudo wildcards also match spaces, so it would allow setting any property.
func (e *PoolActionNeedsRootError) AllowedBySetup() bool {
	for _, action := range append([]PoolAction{e.Action}, e.Then...) {
		if !slices.ContainsFunc(PoolActions, func(allowed PoolAction) bool { return slices.Equal(allowed.Args, action.Args) }) {
			return false
		}
	}
	return true
}

// RunPoolAction runs the action on the pool with the given name. Pool operations cannot be delegated to users like
// dataset ones (zfs allow), so if zpool is denied, it is run again with "sudo -n", which works with the sudoers rule
// of "setup --pool-actions" (or as root). This spawns processes, so it must not be called on the UI thread.
func RunPoolAction(action PoolAction, pool string) error {
	if pool == "" || strings.HasPrefix(pool, "-") {
		return fmt.Errorf("cannot %s: invalid pool name %q", action.Name, pool)
	}
	return backend.RunPoolAction(action, pool)
}

// zpoolPaths are the paths zpool is installed at by the distributions, used in the sudoers rules.
var zpoolPaths = []string{"/usr/sbin/zpool", "/sbin/zpool", "/usr/bin/zpool"}
