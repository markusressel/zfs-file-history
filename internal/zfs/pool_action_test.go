package zfs

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePoolCommands records the commands run by RunPoolAction, failing with the given errors.
type fakePoolCommands struct {
	commands []string
	zpoolErr error
	sudoErr  error
}

func setupFakePoolCommands(t *testing.T, zpoolErr error, sudoErr error) *fakePoolCommands {
	fake := &fakePoolCommands{zpoolErr: zpoolErr, sudoErr: sudoErr}
	originalPool, originalSudo := runPoolCommand, runSudoCommand
	runPoolCommand = func(name string, args ...string) (string, error) {
		fake.commands = append(fake.commands, name+" "+strings.Join(args, " "))
		return "", fake.zpoolErr
	}
	runSudoCommand = func(args ...string) (string, error) {
		fake.commands = append(fake.commands, "sudo "+strings.Join(args, " "))
		return "", fake.sudoErr
	}
	t.Cleanup(func() { runPoolCommand, runSudoCommand = originalPool, originalSudo })
	return fake
}

func TestRunPoolAction(t *testing.T) {
	fake := setupFakePoolCommands(t, nil, nil)

	for _, action := range PoolActions {
		require.NoError(t, RunPoolAction(action, "tank"))
	}

	assert.Equal(t, []string{
		"zpool scrub tank",
		"zpool scrub -p tank",
		"zpool scrub -s tank",
		"zpool trim tank",
		"zpool trim -c tank",
		"zpool clear tank",
	}, fake.commands)
}

func TestRunPoolAction_Fails(t *testing.T) {
	fake := setupFakePoolCommands(t, errors.New("cannot cancel scrubbing tank: there is no active scrub"), nil)

	err := RunPoolAction(PoolActionCancelScrub, "tank")

	// not because of permissions: not run again
	assert.EqualError(t, err, "cannot cancel scrubbing tank: there is no active scrub")
	assert.Equal(t, []string{"zpool scrub -s tank"}, fake.commands)
}

func TestRunPoolAction_Sudo(t *testing.T) {
	fake := setupFakePoolCommands(t, errors.New("cannot scrub tank: permission denied"), nil)

	require.NoError(t, RunPoolAction(PoolActionScrub, "tank"))

	assert.Equal(t, []string{"zpool scrub tank", "sudo -n " + ZpoolPath() + " scrub tank"}, fake.commands)
}

func TestRunPoolAction_SudoNotAllowed(t *testing.T) {
	setupFakePoolCommands(t, errors.New("cannot scrub tank: permission denied"), errors.New("sudo: a password is required"))

	err := RunPoolAction(PoolActionScrub, "tank")

	var needsRoot *PoolActionNeedsRootError
	require.ErrorAs(t, err, &needsRoot)
	assert.Equal(t, PoolActionScrub, needsRoot.Action)
	assert.Equal(t, "tank", needsRoot.Pool)
	assert.Equal(t, []string{ZpoolPath(), "scrub", "tank"}, needsRoot.Command())
	assert.Contains(t, err.Error(), "cannot start a scrub on tank, it needs root: cannot scrub tank: permission denied")
	assert.Contains(t, err.Error(), SetupPoolActionsHint)
	assert.EqualError(t, errors.Unwrap(err), "cannot scrub tank: permission denied")
}

func TestRunPoolAction_NoSudo(t *testing.T) {
	setupFakePoolCommands(t, errors.New("cannot scrub tank: permission denied"), &exec.Error{Name: "sudo", Err: exec.ErrNotFound})

	var needsRoot *PoolActionNeedsRootError
	assert.ErrorAs(t, RunPoolAction(PoolActionScrub, "tank"), &needsRoot)
}

func TestRunPoolAction_SudoFails(t *testing.T) {
	setupFakePoolCommands(t, errors.New("cannot scrub tank: permission denied"), errors.New("cannot scrub tank: currently resilvering"))

	// run as root, zpool says why
	assert.EqualError(t, RunPoolAction(PoolActionScrub, "tank"), "cannot scrub tank: currently resilvering")
}

func TestRunPoolAction_InvalidName(t *testing.T) {
	fake := setupFakePoolCommands(t, nil, nil)

	assert.Error(t, RunPoolAction(PoolActionClear, ""))
	assert.Error(t, RunPoolAction(PoolActionClear, "-F"))
	assert.Empty(t, fake.commands)
}
