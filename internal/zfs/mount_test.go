package zfs

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
	"zfs-file-history/internal/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mountBackend is a Backend for the tests of the mount actions. Methods it does not implement panic (the embedded
// Backend is nil), so a test never reaches the system.
type mountBackend struct {
	Backend
	// identity is the current user, root if empty
	identity Identity
	// loadKeyGranted are the datasets the user was delegated "load-key" on
	loadKeyGranted []string
	datasets       []*DatasetListEntry
	// commands records the zfs commands run, e.g. "mount tank/data"
	commands []string
	// fail are the errors of the commands, by command
	fail map[string]error
	// onCommand is called after each command, e.g. to change the datasets in between
	onCommand func()
}

func (b *mountBackend) CurrentIdentity() (Identity, error) {
	if b.identity.Name == "" {
		return Identity{Name: "root", Uid: "0"}, nil
	}
	return b.identity, nil
}

func (b *mountBackend) Delegations(dataset string) (*Delegations, error) {
	delegations := &Delegations{Dataset: dataset}
	for _, granted := range b.loadKeyGranted {
		if granted == dataset {
			delegations.Entries = append(delegations.Entries, Delegation{Dataset: dataset, Scope: ScopeLocalAndDescendent,
				WhoType: WhoUser, Who: b.identity.Name, Permissions: []string{string(PermissionLoadKey)}})
		}
	}
	return delegations, nil
}

func (b *mountBackend) ListDatasets() ([]*DatasetListEntry, error) {
	return b.datasets, nil
}

func (b *mountBackend) RunDatasetCommand(args []string) error {
	command := strings.Join(args, " ")
	b.commands = append(b.commands, command)
	if b.onCommand != nil {
		b.onCommand()
	}
	return b.fail[command]
}

var testUser = Identity{Name: "alice", Uid: "1000"}

// newMountBackend returns a backend with a pool of mounted datasets, an encryption root with its key loaded
// (tank/secret, mounted with a child, and a child mounted elsewhere) and one with it not loaded (tank/locked), and
// sets it as the backend of the test.
func newMountBackend(t *testing.T) *mountBackend {
	fake := &mountBackend{datasets: []*DatasetListEntry{
		{Name: "tank", CanMount: "on", Mountpoint: "/tank", MountPath: "/tank"},
		{Name: "tank/data", CanMount: "on", Mountpoint: "/data", MountPath: "/data"},
		{Name: "tank/data/photos", CanMount: "on", Mountpoint: "/data/photos", MountPath: "/data/photos"},
		{Name: "tank/data/photos/2024", CanMount: "on", Mountpoint: "/data/photos/2024", MountPath: "/data/photos/2024"},
		// not below /data, although its path starts with it
		{Name: "tank/data2", CanMount: "on", Mountpoint: "/data2", MountPath: "/data2"},
		// another pool mounted on a dataset of tank
		{Name: "backup/media", CanMount: "on", Mountpoint: "/data/media", MountPath: "/data/media"},
		{Name: "tank/off", CanMount: "off", Mountpoint: "/off"},
		{Name: "tank/none", CanMount: "on", Mountpoint: "none"},
		{Name: "tank/legacy", CanMount: "on", Mountpoint: "legacy"},
		{Name: "tank/later", CanMount: "noauto", Mountpoint: "/later"},
		{Name: "tank/secret", CanMount: "on", Mountpoint: "/secret", MountPath: "/secret",
			KeyStatus: "available", EncryptionRoot: "tank/secret", KeyLocation: "prompt"},
		{Name: "tank/secret/notes", CanMount: "on", Mountpoint: "/secret/notes", MountPath: "/secret/notes",
			KeyStatus: "available", EncryptionRoot: "tank/secret", KeyLocation: "none"},
		{Name: "tank/secret/elsewhere", CanMount: "on", Mountpoint: "/elsewhere", MountPath: "/elsewhere",
			KeyStatus: "available", EncryptionRoot: "tank/secret", KeyLocation: "none"},
		{Name: "tank/locked", CanMount: "on", Mountpoint: "/locked",
			KeyStatus: "unavailable", EncryptionRoot: "tank/locked", KeyLocation: "prompt"},
		{Name: "tank/locked/child", CanMount: "on", Mountpoint: "/locked/child",
			KeyStatus: "unavailable", EncryptionRoot: "tank/locked", KeyLocation: "none"},
		{Name: "tank/keyfile", CanMount: "on", Mountpoint: "/keyfile",
			KeyStatus: "unavailable", EncryptionRoot: "tank/keyfile", KeyLocation: "file:///etc/zfs/keyfile"},
		{Name: "rpool/ROOT", CanMount: "on", Mountpoint: "/", MountPath: "/"},
		{Name: "rpool/var", CanMount: "on", Mountpoint: "/var", MountPath: "/var"},
		{Name: "rpool/var/cache", CanMount: "on", Mountpoint: "/var/cache", MountPath: "/var/cache"},
	}}
	testutil.Replace(t, &backend, Backend(fake))
	return fake
}

func TestPlanMountAction_Mount(t *testing.T) {
	newMountBackend(t)

	plan, err := PlanMountAction(MountActionMount, "tank/later")

	require.NoError(t, err)
	assert.Equal(t, &MountPlan{Action: MountActionMount, Dataset: "tank/later", MountPath: "/later",
		Commands: [][]string{{"mount", "tank/later"}}}, plan)
	assert.Equal(t, "mount tank/later", plan.Name())
	assert.Equal(t, "Mounted tank/later at /later", plan.Done())
	assert.Equal(t, [][]string{{ZfsPath(), "mount", "tank/later"}}, plan.CommandLines())
}

func TestPlanMountAction_MountLoadsTheKey(t *testing.T) {
	tests := []struct {
		name     string
		identity Identity
		dataset  string
		prompts  bool
		root     bool
	}{
		{name: "prompt as user", identity: testUser, dataset: "tank/locked", prompts: true, root: true},
		{name: "prompt as root", dataset: "tank/locked", prompts: true},
		{name: "child of the encryption root", identity: testUser, dataset: "tank/locked/child", prompts: true, root: true},
		// RunMountPlan finds out by itself whether it needs root
		{name: "key file", identity: testUser, dataset: "tank/keyfile"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fake := newMountBackend(t)
			fake.identity = test.identity

			plan, err := PlanMountAction(MountActionMount, test.dataset)

			require.NoError(t, err)
			assert.Equal(t, [][]string{{"mount", "-l", test.dataset}}, plan.Commands)
			assert.Equal(t, strings.Join(strings.Split(test.dataset, "/")[:2], "/"), plan.EncryptionRoot)
			assert.Equal(t, test.prompts, plan.Prompts)
			assert.Equal(t, test.root, plan.NeedsRoot)
		})
	}
}

func TestPlanMountAction_Refused(t *testing.T) {
	tests := []struct {
		action  MountAction
		dataset string
		err     string
	}{
		{MountActionMount, "tank/data", "cannot mount tank/data: it is already mounted at /data"},
		{MountActionMount, "tank/off", "cannot mount tank/off: its property canmount is off"},
		{MountActionMount, "tank/none", "cannot mount tank/none: its mountpoint is none"},
		{MountActionMount, "tank/legacy", "cannot mount tank/legacy: its mountpoint is legacy, it is mounted with mount(8) or /etc/fstab"},
		{MountActionMount, "tank/missing", "cannot mount tank/missing: it does not exist or is not a filesystem"},
		{MountActionMount, "-a", `invalid dataset name: "-a"`},
		{MountActionMount, "tank/data@snap", `invalid dataset name: "tank/data@snap"`},
		{MountActionUnmount, "tank/later", "cannot unmount tank/later: it is not mounted"},
		{MountActionUnmount, "rpool/ROOT", "cannot unmount rpool/ROOT: the running system is mounted from datasets that would be unmounted:\n" +
			"  rpool/var (/var)\n  rpool/ROOT (/)"},
		{MountActionUnmount, "rpool/var", "cannot unmount rpool/var: the running system is mounted from datasets that would be unmounted:\n" +
			"  rpool/var (/var)"},
		{MountActionLoadKey, "tank/data", "cannot load the key of tank/data: it is not encrypted"},
		{MountActionLoadKey, "tank/secret/notes", "cannot load the key of tank/secret: it is already loaded"},
		{MountActionUnloadKey, "tank/data", "cannot unload the key of tank/data: it is not encrypted"},
		{MountActionUnloadKey, "tank/locked/child", "cannot unload the key of tank/locked: it is not loaded"},
	}
	for _, test := range tests {
		t.Run(test.err, func(t *testing.T) {
			newMountBackend(t)

			_, err := PlanMountAction(test.action, test.dataset)

			assert.EqualError(t, err, test.err)
		})
	}
}

func TestPlanMountAction_Unmount(t *testing.T) {
	newMountBackend(t)

	plan, err := PlanMountAction(MountActionUnmount, "tank/data")

	require.NoError(t, err)
	// everything mounted below it first, the deepest first, also of other pools; not tank/data2
	assert.Equal(t, []Mount{
		{Name: "tank/data/photos/2024", Mountpoint: "/data/photos/2024"},
		{Name: "tank/data/photos", Mountpoint: "/data/photos"},
		{Name: "backup/media", Mountpoint: "/data/media"},
		{Name: "tank/data", Mountpoint: "/data"},
	}, plan.Unmounted)
	assert.Equal(t, [][]string{
		{"unmount", "tank/data/photos/2024"},
		{"unmount", "tank/data/photos"},
		{"unmount", "backup/media"},
		{"unmount", "tank/data"},
	}, plan.Commands)
	assert.Equal(t, "/data", plan.MountPath)
	assert.False(t, plan.Prompts)
	assert.Equal(t, "Unmounted tank/data", plan.Done())
}

func TestPlanMountAction_LoadKey(t *testing.T) {
	tests := []struct {
		name     string
		identity Identity
		granted  []string
		dataset  string
		prompts  bool
		root     bool
	}{
		{name: "as root", dataset: "tank/locked", prompts: true},
		{name: "as user", identity: testUser, dataset: "tank/locked", prompts: true, root: true},
		{name: "delegated", identity: testUser, granted: []string{"tank/locked"}, dataset: "tank/locked/child", prompts: true},
		{name: "key file", identity: testUser, dataset: "tank/keyfile"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fake := newMountBackend(t)
			fake.identity = test.identity
			fake.loadKeyGranted = test.granted

			plan, err := PlanMountAction(MountActionLoadKey, test.dataset)

			require.NoError(t, err)
			root := strings.Join(strings.Split(test.dataset, "/")[:2], "/")
			// the key of the encryption root, also for its children
			assert.Equal(t, [][]string{{"load-key", root}}, plan.Commands)
			assert.Equal(t, root, plan.EncryptionRoot)
			assert.Empty(t, plan.Unmounted)
			assert.Equal(t, test.prompts, plan.Prompts)
			assert.Equal(t, test.root, plan.NeedsRoot)
			assert.Equal(t, "load the key of "+root, plan.Name())
		})
	}
}

func TestPlanMountAction_UnloadKey(t *testing.T) {
	newMountBackend(t)

	plan, err := PlanMountAction(MountActionUnloadKey, "tank/secret/notes")

	require.NoError(t, err)
	// all datasets encrypted with the key, also the one mounted elsewhere
	assert.Equal(t, []Mount{
		{Name: "tank/secret/notes", Mountpoint: "/secret/notes"},
		{Name: "tank/secret", Mountpoint: "/secret"},
		{Name: "tank/secret/elsewhere", Mountpoint: "/elsewhere"},
	}, plan.Unmounted)
	assert.Equal(t, [][]string{
		{"unmount", "tank/secret/notes"},
		{"unmount", "tank/secret"},
		{"unmount", "tank/secret/elsewhere"},
		{"unload-key", "tank/secret"},
	}, plan.Commands)
	assert.Equal(t, "tank/secret", plan.EncryptionRoot)
	assert.False(t, plan.Prompts)
	assert.Equal(t, "Unloaded the key of tank/secret", plan.Done())
}

func TestRunMountPlan(t *testing.T) {
	fake := newMountBackend(t)
	plan, err := PlanMountAction(MountActionUnloadKey, "tank/secret")
	require.NoError(t, err)

	require.NoError(t, RunMountPlan(plan))

	assert.Equal(t, []string{"unmount tank/secret/notes", "unmount tank/secret", "unmount tank/secret/elsewhere",
		"unload-key tank/secret"}, fake.commands)
}

func TestRunMountPlan_StopsAtTheFirstError(t *testing.T) {
	fake := newMountBackend(t)
	plan, err := PlanMountAction(MountActionUnmount, "tank/data")
	require.NoError(t, err)
	fake.fail = map[string]error{"unmount backup/media": errors.New("cannot unmount '/data/media': pool or dataset is busy")}

	err = RunMountPlan(plan)

	assert.EqualError(t, err, "cannot unmount '/data/media': pool or dataset is busy")
	assert.Equal(t, []string{"unmount tank/data/photos/2024", "unmount tank/data/photos", "unmount backup/media"}, fake.commands)
}

func TestRunMountPlan_NeedsRoot(t *testing.T) {
	fake := newMountBackend(t)
	plan, err := PlanMountAction(MountActionUnmount, "tank/data")
	require.NoError(t, err)
	cause := errors.New("cannot unmount '/data/photos': permission denied")
	fake.fail = map[string]error{"unmount tank/data/photos": &DatasetCommandNeedsRootError{Cause: cause}}

	err = RunMountPlan(plan)

	needsRoot, ok := errors.AsType[*DatasetCommandNeedsRootError](err)
	require.True(t, ok, err)
	// sudo runs the commands left, the denied one included
	assert.Equal(t, [][]string{
		{ZfsPath(), "unmount", "tank/data/photos"},
		{ZfsPath(), "unmount", "backup/media"},
		{ZfsPath(), "unmount", "tank/data"},
	}, needsRoot.Commands)
	assert.Equal(t, "unmount tank/data", needsRoot.Action)
	assert.Equal(t, "Unmounted tank/data", needsRoot.Done)
	assert.EqualError(t, err, "cannot unmount tank/data, it needs root: cannot unmount '/data/photos': permission denied")
	assert.ErrorIs(t, err, cause)
}

func TestRunMountPlan_ChangedSinceConfirmation(t *testing.T) {
	fake := newMountBackend(t)
	plan, err := PlanMountAction(MountActionUnmount, "tank/data")
	require.NoError(t, err)
	// mounted in between
	fake.datasets = append(fake.datasets, &DatasetListEntry{Name: "tank/data/new", MountPath: "/data/new"})

	err = RunMountPlan(plan)

	assert.ErrorIs(t, err, ErrMountsChangedSinceConfirmation)
	assert.Empty(t, fake.commands)
}

func TestRunMountPlan_Refused(t *testing.T) {
	fake := newMountBackend(t)
	plan, err := PlanMountAction(MountActionLoadKey, "tank/locked")
	require.NoError(t, err)

	assert.EqualError(t, RunMountPlan(plan), "cannot load the key of tank/locked here: zfs asks for the passphrase, so it runs in the terminal")
	assert.EqualError(t, RunMountPlan(nil), "cannot change the mounts: not confirmed")
	assert.Empty(t, fake.commands)
}

// fakeDatasetCommands records the commands run by System.RunDatasetCommand, failing with the given errors.
func fakeDatasetCommands(t *testing.T, zfsErr error, sudoErr error) *[]string {
	var commands []string
	testutil.Replace(t, &runDatasetCommand, func(name string, args ...string) (string, error) {
		commands = append(commands, name+" "+strings.Join(args, " "))
		return "", zfsErr
	})
	testutil.Replace(t, &runSudoCommand, func(args ...string) (string, error) {
		commands = append(commands, "sudo "+strings.Join(args, " "))
		return "", sudoErr
	})
	return &commands
}

func TestSystem_RunDatasetCommand(t *testing.T) {
	commands := fakeDatasetCommands(t, nil, nil)

	require.NoError(t, System{}.RunDatasetCommand([]string{"mount", "tank/data"}))

	assert.Equal(t, []string{"zfs mount tank/data"}, *commands)
}

func TestSystem_RunDatasetCommand_Fails(t *testing.T) {
	commands := fakeDatasetCommands(t, errors.New("cannot unmount '/data': pool or dataset is busy"), nil)

	err := System{}.RunDatasetCommand([]string{"unmount", "tank/data"})

	// not because of permissions: not run again
	assert.EqualError(t, err, "cannot unmount '/data': pool or dataset is busy")
	assert.Equal(t, []string{"zfs unmount tank/data"}, *commands)
}

func TestSystem_RunDatasetCommand_Sudo(t *testing.T) {
	for _, denied := range []string{"cannot mount 'tank/data': Insufficient privileges", "cannot unmount '/data': permission denied"} {
		t.Run(denied, func(t *testing.T) {
			commands := fakeDatasetCommands(t, errors.New(denied), nil)

			require.NoError(t, System{}.RunDatasetCommand([]string{"mount", "tank/data"}))

			assert.Equal(t, []string{"zfs mount tank/data", "sudo -n " + ZfsPath() + " mount tank/data"}, *commands)
		})
	}
}

func TestSystem_RunDatasetCommand_SudoNotAllowed(t *testing.T) {
	for _, sudoErr := range []error{errors.New("sudo: a password is required"), &exec.Error{Name: "sudo", Err: exec.ErrNotFound}} {
		t.Run(sudoErr.Error(), func(t *testing.T) {
			fakeDatasetCommands(t, errors.New("cannot mount 'tank/data': Insufficient privileges"), sudoErr)

			err := System{}.RunDatasetCommand([]string{"mount", "tank/data"})

			needsRoot, ok := errors.AsType[*DatasetCommandNeedsRootError](err)
			require.True(t, ok, err)
			assert.Equal(t, [][]string{{ZfsPath(), "mount", "tank/data"}}, needsRoot.Commands)
			assert.EqualError(t, needsRoot.Cause, "cannot mount 'tank/data': Insufficient privileges")
		})
	}
}

func TestSystem_RunDatasetCommand_SudoFails(t *testing.T) {
	fakeDatasetCommands(t, errors.New("cannot mount 'tank/data': Insufficient privileges"),
		errors.New("cannot mount 'tank/data': mountpoint or dataset is busy"))

	err := System{}.RunDatasetCommand([]string{"mount", "tank/data"})

	// run as root, but zfs failed: that is the error
	assert.EqualError(t, err, "cannot mount 'tank/data': mountpoint or dataset is busy")
}
