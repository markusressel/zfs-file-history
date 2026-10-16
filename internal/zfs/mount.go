package zfs

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// MountAction changes whether the files of a dataset can be accessed: mounting and unmounting it, loading and
// unloading its encryption key. None of them loses data, but programs that use the files of a dataset fail once it
// is unmounted, so the application asks before unmounting (also before unloading a key, which unmounts the datasets
// encrypted with it). Like the actions that lose data, they work in two steps: PlanMountAction says what would be
// done, RunMountPlan does it once confirmed, and refuses if the plan changed meanwhile. Nothing is ever forced
// ("-f"): a dataset in use stays mounted, and zfs says so.
type MountAction int

const (
	// MountActionMount mounts the dataset at its mountpoint ("zfs mount"), loading its key first if needed.
	MountActionMount MountAction = iota
	// MountActionUnmount unmounts the dataset and everything mounted below it ("zfs unmount").
	MountActionUnmount
	// MountActionLoadKey loads the key of the encryption root of the dataset ("zfs load-key"), without mounting.
	MountActionLoadKey
	// MountActionUnloadKey unmounts all datasets encrypted with the key of the encryption root of the dataset, and
	// unloads the key ("zfs unload-key"), so their files cannot be read until it is loaded again.
	MountActionUnloadKey
)

// ErrMountsChangedSinceConfirmation is returned by RunMountPlan if what would be done differs from the confirmed plan,
// e.g. because another dataset was mounted in between.
var ErrMountsChangedSinceConfirmation = errors.New("the mounts changed since it was confirmed, nothing was changed: review it again")

// MountPlan is what a MountAction would do, see PlanMountAction.
type MountPlan struct {
	Action MountAction
	// Dataset is the name of the dataset the action was chosen for.
	Dataset string
	// MountPath is the path the dataset is mounted at: its "mountpoint" property for MountActionMount.
	MountPath string
	// EncryptionRoot is the dataset whose key is loaded or unloaded (also by mounting a dataset whose key is not
	// loaded), "" if no key is.
	EncryptionRoot string
	// Unmounted are the datasets that are unmounted, the deepest mount path first: for MountActionUnmount the
	// dataset and everything mounted below it (it cannot be unmounted before them), for MountActionUnloadKey also
	// the datasets encrypted with the key.
	Unmounted []Mount
	// Commands are the arguments of the zfs commands that do it, in the order they run.
	Commands [][]string
	// Prompts tells that zfs asks for the passphrase of the key ("keylocation=prompt"), so the commands have to run
	// in the terminal, not with RunMountPlan. The application never sees the passphrase.
	Prompts bool
	// NeedsRoot tells that the commands have to run as root (with sudo): Linux only lets root mount, and loading a
	// key needs the permission "load-key" otherwise. Only set if Prompts is: sudo has to ask for the password before
	// zfs asks for the passphrase. RunMountPlan finds out by itself.
	NeedsRoot bool
}

// Name returns what the plan does, e.g. "mount tank/data", used in errors and explanations.
func (plan *MountPlan) Name() string {
	switch plan.Action {
	case MountActionUnmount:
		return "unmount " + plan.Dataset
	case MountActionLoadKey:
		return "load the key of " + plan.EncryptionRoot
	case MountActionUnloadKey:
		return "unload the key of " + plan.EncryptionRoot
	}
	return "mount " + plan.Dataset
}

// Done returns that the plan was done, e.g. "Mounted tank/data at /data".
func (plan *MountPlan) Done() string {
	switch plan.Action {
	case MountActionUnmount:
		return fmt.Sprintf("Unmounted %s", plan.Dataset)
	case MountActionLoadKey:
		return fmt.Sprintf("Loaded the key of %s", plan.EncryptionRoot)
	case MountActionUnloadKey:
		return fmt.Sprintf("Unloaded the key of %s", plan.EncryptionRoot)
	}
	return fmt.Sprintf("Mounted %s at %s", plan.Dataset, plan.MountPath)
}

// CommandLines returns the commands with the full path of zfs, as sudo needs it, e.g.
// [["/usr/bin/zfs", "mount", "tank/data"]].
func (plan *MountPlan) CommandLines() [][]string {
	return zfsCommandLines(plan.Commands)
}

// zfsCommandLines returns the arguments of zfs commands with the full path of zfs in front.
func zfsCommandLines(commands [][]string) [][]string {
	lines := make([][]string, 0, len(commands))
	for _, args := range commands {
		lines = append(lines, append([]string{ZfsPath()}, args...))
	}
	return lines
}

// PlanMountAction returns what the action would do to the filesystem dataset with the given name, without changing
// anything. It refuses what zfs would refuse (e.g. mounting a dataset whose mountpoint is "legacy"), and unmounting
// where the running system is mounted (protectedMountPaths).
// This spawns zfs processes, so it must not be called on the UI thread.
func PlanMountAction(action MountAction, dataset string) (*MountPlan, error) {
	if err := validateDatasetName(dataset); err != nil {
		return nil, err
	}
	datasets, err := backend.ListDatasets()
	if err != nil {
		return nil, err
	}
	index := slices.IndexFunc(datasets, func(entry *DatasetListEntry) bool { return entry.Name == dataset })
	plan := &MountPlan{Action: action, Dataset: dataset}
	if index < 0 {
		return nil, fmt.Errorf("cannot %s: it does not exist or is not a filesystem", plan.Name())
	}
	entry := datasets[index]
	switch action {
	case MountActionMount:
		err = planMount(plan, entry, datasets)
	case MountActionUnmount:
		err = planUnmount(plan, entry, datasets)
	case MountActionLoadKey:
		err = planLoadKey(plan, entry, datasets)
	case MountActionUnloadKey:
		err = planUnloadKey(plan, entry, datasets)
	default:
		err = fmt.Errorf("unknown mount action %d", action)
	}
	if err != nil {
		return nil, err
	}
	return plan, nil
}

// planMount fills the plan of mounting the dataset entry, loading its key first if it is not loaded ("-l").
func planMount(plan *MountPlan, entry *DatasetListEntry, datasets []*DatasetListEntry) error {
	switch {
	case entry.MountPath != "":
		return fmt.Errorf("cannot %s: it is already mounted at %s", plan.Name(), entry.MountPath)
	case entry.CanMount == "off":
		return fmt.Errorf("cannot %s: its property canmount is off", plan.Name())
	case entry.Mountpoint == "none":
		return fmt.Errorf("cannot %s: its mountpoint is none", plan.Name())
	case entry.Mountpoint == "legacy":
		return fmt.Errorf("cannot %s: its mountpoint is legacy, it is mounted with mount(8) or /etc/fstab", plan.Name())
	}
	plan.MountPath = entry.Mountpoint
	plan.Commands = [][]string{{"mount", entry.Name}}
	if entry.KeyStatus != "unavailable" {
		return nil
	}
	plan.EncryptionRoot = entry.EncryptionRoot
	plan.Commands = [][]string{{"mount", "-l", entry.Name}}
	plan.Prompts = keyPrompts(entry.EncryptionRoot, datasets)
	if plan.Prompts {
		// Linux only lets root mount, whatever was delegated
		plan.NeedsRoot = !isRootUser()
	}
	return nil
}

// planUnmount fills the plan of unmounting the dataset entry, with everything mounted below it.
func planUnmount(plan *MountPlan, entry *DatasetListEntry, datasets []*DatasetListEntry) error {
	if entry.MountPath == "" {
		return fmt.Errorf("cannot %s: it is not mounted", plan.Name())
	}
	plan.MountPath = entry.MountPath
	plan.Unmounted = mountedWithin(datasets, []string{entry.MountPath})
	if err := refuseSystemUnmounts(plan); err != nil {
		return err
	}
	for _, mount := range plan.Unmounted {
		plan.Commands = append(plan.Commands, []string{"unmount", mount.Name})
	}
	return nil
}

// planLoadKey fills the plan of loading the key of the encryption root of the dataset entry.
func planLoadKey(plan *MountPlan, entry *DatasetListEntry, datasets []*DatasetListEntry) error {
	plan.EncryptionRoot = entry.EncryptionRoot
	switch {
	case entry.EncryptionRoot == "":
		return fmt.Errorf("cannot load the key of %s: it is not encrypted", entry.Name)
	case entry.KeyStatus == "available":
		return fmt.Errorf("cannot %s: it is already loaded", plan.Name())
	}
	plan.Commands = [][]string{{"load-key", entry.EncryptionRoot}}
	plan.Prompts = keyPrompts(entry.EncryptionRoot, datasets)
	if plan.Prompts {
		plan.NeedsRoot = !mayLoadKey(entry.EncryptionRoot)
	}
	return nil
}

// planUnloadKey fills the plan of unloading the key of the encryption root of the dataset entry, after unmounting
// the datasets encrypted with it (and everything mounted below them).
func planUnloadKey(plan *MountPlan, entry *DatasetListEntry, datasets []*DatasetListEntry) error {
	plan.EncryptionRoot = entry.EncryptionRoot
	switch {
	case entry.EncryptionRoot == "":
		return fmt.Errorf("cannot unload the key of %s: it is not encrypted", entry.Name)
	case entry.KeyStatus != "available":
		return fmt.Errorf("cannot %s: it is not loaded", plan.Name())
	}
	var paths []string
	for _, dataset := range datasets {
		if dataset.EncryptionRoot == entry.EncryptionRoot && dataset.MountPath != "" {
			paths = append(paths, dataset.MountPath)
		}
	}
	plan.Unmounted = mountedWithin(datasets, paths)
	if err := refuseSystemUnmounts(plan); err != nil {
		return err
	}
	for _, mount := range plan.Unmounted {
		plan.Commands = append(plan.Commands, []string{"unmount", mount.Name})
	}
	plan.Commands = append(plan.Commands, []string{"unload-key", entry.EncryptionRoot})
	return nil
}

// keyPrompts returns whether zfs asks for the passphrase of the key of the encryption root (its keylocation is
// "prompt"). If the encryption root is not listed, it assumes so: the terminal works for both.
func keyPrompts(encryptionRoot string, datasets []*DatasetListEntry) bool {
	index := slices.IndexFunc(datasets, func(entry *DatasetListEntry) bool { return entry.Name == encryptionRoot })
	return index < 0 || datasets[index].KeyLocation == "prompt"
}

// isRootUser returns whether the current user is root. If that cannot be found out, it assumes not.
func isRootUser() bool {
	who, err := backend.CurrentIdentity()
	return err == nil && who.IsRoot()
}

// mayLoadKey returns whether the current user may load the key of the encryption root without root: root, or the
// permission "load-key" was delegated. If that cannot be found out, it assumes not, sudo works for both.
func mayLoadKey(encryptionRoot string) bool {
	who, err := backend.CurrentIdentity()
	if err != nil {
		return false
	}
	if who.IsRoot() {
		return true
	}
	missing, err := findMissingPermissions(who, []PermissionGap{{Dataset: encryptionRoot, Permissions: []Permission{PermissionLoadKey}}})
	return err == nil && len(missing) == 0
}

// mountedWithin returns the mounted datasets whose mount path is one of the paths or below one, the deepest first, so
// each is unmounted before what it is mounted on.
func mountedWithin(datasets []*DatasetListEntry, paths []string) []Mount {
	var mounts []Mount
	for _, dataset := range datasets {
		if dataset.MountPath == "" {
			continue
		}
		if slices.ContainsFunc(paths, func(path string) bool { return isPathWithin(dataset.MountPath, path) }) {
			mounts = append(mounts, Mount{Name: dataset.Name, Mountpoint: dataset.MountPath})
		}
	}
	slices.SortFunc(mounts, func(a, b Mount) int {
		return cmp.Or(cmp.Compare(strings.Count(b.Mountpoint, "/"), strings.Count(a.Mountpoint, "/")),
			cmp.Compare(b.Mountpoint, a.Mountpoint), cmp.Compare(a.Name, b.Name))
	})
	return mounts
}

// isPathWithin returns whether path is base or below it.
func isPathWithin(path string, base string) bool {
	return path == base || base == "/" || strings.HasPrefix(path, strings.TrimSuffix(base, "/")+"/")
}

// refuseSystemUnmounts returns an error if the plan would unmount a path of the running system (protectedMountPaths).
func refuseSystemUnmounts(plan *MountPlan) error {
	var system []string
	for _, mount := range plan.Unmounted {
		if isSystemPath(mount.Mountpoint) {
			system = append(system, fmt.Sprintf("%s (%s)", mount.Name, mount.Mountpoint))
		}
	}
	if len(system) > 0 {
		return fmt.Errorf("cannot %s: the running system is mounted from datasets that would be unmounted:%s", plan.Name(), formatNames(system))
	}
	return nil
}

// RunMountPlan does what the confirmed plan of PlanMountAction says. Returns ErrMountsChangedSinceConfirmation if
// the plan changed since, and a DatasetCommandNeedsRootError with the commands left if zfs and "sudo -n" denied one
// (e.g. mounting needs root on Linux). Plans that prompt for a passphrase are refused: they run in the terminal.
// This spawns zfs processes, so it must not be called on the UI thread.
func RunMountPlan(confirmed *MountPlan) error {
	if confirmed == nil {
		return errors.New("cannot change the mounts: not confirmed")
	}
	if confirmed.Prompts {
		return fmt.Errorf("cannot %s here: zfs asks for the passphrase, so it runs in the terminal", confirmed.Name())
	}
	plan, err := PlanMountAction(confirmed.Action, confirmed.Dataset)
	if err != nil {
		return err
	}
	if plan.Prompts || !slices.EqualFunc(plan.Commands, confirmed.Commands, slices.Equal) {
		return ErrMountsChangedSinceConfirmation
	}
	for i, args := range plan.Commands {
		err := backend.RunDatasetCommand(args)
		if needsRoot, ok := errors.AsType[*DatasetCommandNeedsRootError](err); ok {
			// the ones before ran, sudo runs the rest
			return &DatasetCommandNeedsRootError{Action: plan.Name(), Done: plan.Done(),
				Commands: zfsCommandLines(plan.Commands[i:]), Cause: needsRoot.Cause}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// DatasetCommandNeedsRootError is returned if zfs denied a command on a dataset, and sudo did not allow running it
// either (e.g. a password is required). Linux only lets root mount and unmount, whatever was delegated with
// "zfs allow". The setup command has no sudoers rule for them: a rule for "zfs mount" would let the user mount any
// dataset, also over the directories of the system.
type DatasetCommandNeedsRootError struct {
	// Action is what was to be done, e.g. "mount tank/data".
	Action string
	// Done tells that it was done, e.g. "Mounted tank/data at /data".
	Done string
	// Commands are the commands left to run, with the full path of zfs, as sudo needs it.
	Commands [][]string
	// Cause is the error of the denied zfs command.
	Cause error
}

func (e *DatasetCommandNeedsRootError) Error() string {
	return fmt.Sprintf("cannot %s, it needs root: %s", e.Action, e.Cause)
}

func (e *DatasetCommandNeedsRootError) Unwrap() error {
	return e.Cause
}

// needsRoot returns whether a zfs command failed because only root may run it (e.g. "Insufficient privileges" of zfs
// mount, which Linux only allows root).
func needsRoot(err error) bool {
	return isPermissionDenied(err) || strings.Contains(strings.ToLower(err.Error()), "insufficient privileges")
}
