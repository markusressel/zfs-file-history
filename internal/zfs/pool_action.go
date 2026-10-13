package zfs

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// PoolAction is an operation on a pool that can be started from the application, see RunPoolAction. Operations
// that lose data or make the pool unavailable (destroy, export, upgrade, "clear -F", ...) are deliberately not
// offered.
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

	// PoolActions are all actions, e.g. for the sudoers rule of the setup command, see SudoersRulePoolActions.
	PoolActions = []PoolAction{PoolActionScrub, PoolActionPauseScrub, PoolActionCancelScrub, PoolActionTrim, PoolActionCancelTrim, PoolActionClear}
)

// SetupPoolActionsHint tells how to allow the pool actions without starting the application as root.
const SetupPoolActionsHint = "To allow it without a password, run: sudo zfs-file-history setup --pool-actions"

// PoolActionNeedsRootError is returned by RunPoolAction if zpool denied the action, and sudo did not allow running
// it either (e.g. a password is required). The pool actions need root, ZFS cannot delegate them to users like it
// does for datasets (zfs allow).
type PoolActionNeedsRootError struct {
	Action PoolAction
	Pool   string
	// Cause is the error of the denied zpool command
	Cause error
}

func (e *PoolActionNeedsRootError) Error() string {
	return fmt.Sprintf("cannot %s on %s, it needs root: %s\n\n%s", e.Action.Name, e.Pool, e.Cause, SetupPoolActionsHint)
}

func (e *PoolActionNeedsRootError) Unwrap() error {
	return e.Cause
}

// Command returns the zpool command of the action (with the full path, as needed by sudo), e.g.
// ["/usr/sbin/zpool", "scrub", "tank"].
func (e *PoolActionNeedsRootError) Command() []string {
	return append(append([]string{ZpoolPath()}, e.Action.Args...), e.Pool)
}

// runPoolCommand and runSudoCommand run a command and return its output. Replaceable in tests.
var (
	runPoolCommand = runCommand
	runSudoCommand = func(args ...string) (string, error) { return runCommand("sudo", args...) }
)

// RunPoolAction runs the action on the pool with the given name. Pool operations cannot be delegated to users like
// dataset ones (zfs allow), so if zpool is denied, it is run again with "sudo -n", which works with the sudoers rule
// of "setup --pool-actions" (or as root). This spawns processes, so it must not be called on the UI thread.
func RunPoolAction(action PoolAction, pool string) error {
	if pool == "" || strings.HasPrefix(pool, "-") {
		return fmt.Errorf("cannot %s: invalid pool name %q", action.Name, pool)
	}
	args := append(append([]string{}, action.Args...), pool)
	_, err := runPoolCommand("zpool", args...)
	if err == nil || !isPermissionDenied(err) {
		return err
	}

	_, sudoErr := runSudoCommand(append([]string{"-n", ZpoolPath()}, args...)...)
	if sudoErr == nil {
		return nil
	}
	if strings.HasPrefix(sudoErr.Error(), "sudo:") || errors.Is(sudoErr, exec.ErrNotFound) {
		// not allowed (e.g. no sudoers rule, so a password is required) or no sudo: the original error says more
		return &PoolActionNeedsRootError{Action: action, Pool: pool, Cause: err}
	}
	// run by root, but zpool failed, e.g. "there is no active scrub"
	return sudoErr
}

// ZpoolPath returns the path of the zpool command. Sudo needs the full path, which is also the one in the sudoers
// rules, see SudoersRule.
func ZpoolPath() string {
	path, err := exec.LookPath("zpool")
	if err != nil {
		return "/usr/bin/zpool"
	}
	return path
}

// zpoolPaths are the paths zpool is installed at by the distributions, used in the sudoers rules.
var zpoolPaths = []string{"/usr/sbin/zpool", "/sbin/zpool", "/usr/bin/zpool"}
