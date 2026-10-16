package zfs

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"slices"
	"strings"
	"time"
	"zfs-file-history/internal/logging"
)

// System is the Backend of the system: the zfs and zpool commands, /proc/mounts and sudo. It is the only part of the
// application that runs them (see TestOnlySystemRunsCommands).
type System struct{}

var _ Backend = System{}

// Replaceable in tests, so they never run zfs, zpool or sudo.
var (
	// runPoolCommand and runSudoCommand run a command and return its output.
	runPoolCommand = runCommand
	runSudoCommand = func(args ...string) (string, error) { return runCommand("sudo", args...) }
	// runDatasetCommand runs a zfs command of a MountPlan and returns its output.
	runDatasetCommand = runCommand
	// readZfsMounts returns the mounted ZFS datasets and snapshots.
	readZfsMounts = func() ([]Mount, error) {
		file, err := os.Open("/proc/mounts")
		if err != nil {
			return nil, err
		}
		defer file.Close()
		var mounts []Mount
		for name, mountpoint := range parseZfsMountPaths(file) {
			mounts = append(mounts, Mount{Name: name, Mountpoint: mountpoint})
		}
		return mounts, nil
	}
	// listSnapshotNames returns the names (behind the "@") of the snapshots of the dataset, the newest first.
	listSnapshotNames = func(dataset string) ([]string, error) {
		// by createtxg, not creation: snapshots created in the same second are still in order
		output, err := runZfs("list", "-H", "-o", "name", "-s", "createtxg", "-t", "snapshot", "-d", "1", dataset)
		if err != nil {
			return nil, err
		}
		var names []string
		for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
			if _, name, ok := strings.Cut(line, "@"); ok {
				names = append(names, name)
			}
		}
		slices.Reverse(names)
		return names, nil
	}
	// currentIdentity returns the user the application runs as.
	currentIdentity = func() (Identity, error) {
		current, err := user.Current()
		if err != nil {
			return Identity{}, err
		}
		result := Identity{Name: current.Username, Uid: current.Uid}
		groupIds, err := current.GroupIds()
		if err == nil {
			for _, groupId := range groupIds {
				if group, err := user.LookupGroupId(groupId); err == nil {
					result.Groups = append(result.Groups, group.Name)
				}
			}
		}
		return result, nil
	}
	// readDelegations returns the output of "zfs allow <dataset>", which lists the delegated permissions of the
	// dataset and its ancestors. Anyone can read them.
	readDelegations = func(dataset string) (string, error) {
		return runZfs("allow", dataset)
	}
	// streamCommand runs the command and passes each line of its output (without the line break, only valid during
	// the call) to onLine while it runs. Once onLine returns false, the output is not read further and the command is
	// stopped, which is no error. If it fails, the error contains the message the command printed, if any.
	streamCommand = func(ctx context.Context, onLine func(line []byte) bool, name string, args ...string) error {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		command := exec.CommandContext(ctx, name, args...)
		var stderr strings.Builder
		command.Stderr = &stderr
		output, err := command.StdoutPipe()
		if err != nil {
			return err
		}
		if err := command.Start(); err != nil {
			return err
		}
		reader := bufio.NewReaderSize(output, 1<<20)
		stopped := false
		var long []byte
		for ctx.Err() == nil {
			line, err := reader.ReadSlice('\n')
			if errors.Is(err, bufio.ErrBufferFull) {
				// a line longer than the buffer, e.g. the dumped arguments of an ioctl
				long = append(long, line...)
				continue
			}
			if long != nil {
				line, long = append(long, line...), nil
			}
			if len(line) > 0 && !onLine(bytes.TrimSuffix(line, []byte("\n"))) {
				stopped = true
				break
			}
			if err != nil {
				break
			}
		}
		if stopped || ctx.Err() != nil {
			// a command sudo runs does not get the kill of sudo: it stops once it cannot write anymore
			cancelled := ctx.Err()
			_ = output.Close()
			cancel()
			_ = command.Wait()
			if stopped {
				return nil
			}
			return cancelled
		}
		err = command.Wait()
		if err != nil && strings.TrimSpace(stderr.String()) != "" {
			return errors.New(strings.TrimSpace(stderr.String()))
		}
		return err
	}
	// listSudoRules lists the commands the user may run with sudo ("sudo -l"). Without a password (-n), which works
	// if any of them is allowed without one. Unlike running a command, this is not logged.
	listSudoRules = func() (string, error) { return runSudoCommand("-n", "-l") }
	// isRoot returns whether the application runs as root.
	isRoot = func() bool { return os.Geteuid() == 0 }
)

// runZfs runs the zfs command with the given arguments and returns its output.
// If it fails, the error contains the message zfs printed, if any.
func runZfs(args ...string) (string, error) {
	return runCommand("zfs", args...)
}

// runZpool runs the zpool command with the given arguments and returns its output.
func runZpool(args ...string) (string, error) {
	return runCommand("zpool", args...)
}

// runCommand runs the command with the given arguments and returns its output.
// If it fails, the error contains the message the command printed, if any.
func runCommand(name string, args ...string) (string, error) {
	output, err := exec.Command(name, args...).Output()
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) && len(strings.TrimSpace(string(exitError.Stderr))) > 0 {
			return "", errors.New(strings.TrimSpace(string(exitError.Stderr)))
		}
		return "", err
	}
	return string(output), nil
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

// ZfsPath returns the path of the zfs command. Sudo needs the full path.
func ZfsPath() string {
	path, err := exec.LookPath("zfs")
	if err != nil {
		return "/usr/bin/zfs"
	}
	return path
}

// isSudoDenied returns whether running a command with "sudo -n" failed because sudo did not allow it (e.g. no
// sudoers rule, so a password is required) or there is no sudo, not because of the command itself.
func isSudoDenied(err error) bool {
	return strings.HasPrefix(err.Error(), "sudo:") || errors.Is(err, exec.ErrNotFound)
}

func (System) Mounts() ([]Mount, error) {
	return readZfsMounts()
}

// datasetListProperties are the properties requested by ListDatasets, in the order parseDatasetList expects them.
// The mountpoint is last, so it may contain any character but a newline.
var datasetListProperties = []string{"name", "used", "available", "usedbysnapshots", "usedbydataset", "usedbychildren", "usedbyrefreservation", "origin",
	"canmount", "keystatus", "encryptionroot", "keylocation", "mountpoint"}

func (system System) ListDatasets() ([]*DatasetListEntry, error) {
	output, err := runZfs("list", "-H", "-p", "-t", "filesystem", "-o", strings.Join(datasetListProperties, ","))
	if err != nil {
		return nil, err
	}
	mountPaths := map[string]string{}
	if mounts, err := system.Mounts(); err == nil {
		for _, mount := range mounts {
			mountPaths[mount.Name] = mount.Mountpoint
		}
	}
	return parseDatasetList(output, mountPaths)
}

func (System) ListChildDatasets(parents []string) ([]*DatasetListEntry, error) {
	args := []string{"list", "-H", "-p", "-d", "1", "-t", "filesystem,volume", "-o", strings.Join(datasetListProperties, ","), "--"}
	output, err := runZfs(append(args, parents...)...)
	if err != nil {
		return nil, err
	}
	return parseDatasetList(output, nil)
}

func (System) ListSnapshotNames(dataset string) ([]string, error) {
	return listSnapshotNames(dataset)
}

func (System) ListProperties(dataset string) ([]*Property, error) {
	output, err := runZfs("get", "-H", "-o", "property,value,source", "all", dataset)
	if err != nil {
		return nil, err
	}
	return parseProperties(output)
}

func (System) SetProperty(dataset string, name string, value string) error {
	_, err := runZfs(SetPropertyCommand(dataset, name, value)[1:]...)
	return err
}

func (System) InheritProperty(dataset string, name string) error {
	_, err := runZfs(InheritPropertyCommand(dataset, name)[1:]...)
	return err
}

func (System) Delegations(dataset string) (*Delegations, error) {
	output, err := readDelegations(dataset)
	if err != nil {
		return nil, err
	}
	return parseDelegations(output, dataset), nil
}

func (System) ApplyDelegationChange(change DelegationChange) error {
	for _, command := range change.Commands() {
		if _, err := runZfs(command[1:]...); err != nil {
			return err
		}
	}
	return nil
}

func (System) CurrentIdentity() (Identity, error) {
	return currentIdentity()
}

// AllowedSudoersRules returns the ids of the rules the user may use (all as root): the ones whose commands sudo
// allows without a password, with the path their program has here.
func (System) AllowedSudoersRules() map[string]bool {
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
		allowed[rule.Id] = all || rule.isAllowedBy(commands)
	}
	return allowed
}

func (System) ListHolds(snapshotNames []string, recursive bool) ([]Hold, error) {
	args := []string{"holds", "-H", "-p"}
	if recursive {
		args = append(args, "-r")
	}
	output, err := runZfs(append(args, snapshotNames...)...)
	if err != nil {
		return nil, err
	}
	return parseHolds(output)
}

func (System) Hold(tag string, snapshotNames []string) error {
	_, err := runZfs(append([]string{"hold", tag}, snapshotNames...)...)
	return err
}

func (System) Release(tag string, snapshotNames []string) error {
	_, err := runZfs(append([]string{"release", tag}, snapshotNames...)...)
	return err
}

func (System) PreviewDestroySnapshots(dataset string, snapshots []string, recursive bool, dependantClones bool) (*DestroyPreview, error) {
	args := []string{"destroy", "-n", "-v", "-p"}
	if recursive {
		args = append(args, "-r")
	}
	if dependantClones {
		args = append(args, "-R")
	}
	args = append(args, dataset+"@"+strings.Join(snapshots, ","))

	output, err := runZfs(args...)
	if err != nil {
		return nil, err
	}
	return parseDestroyPreview(output, true)
}

func (System) RollbackSnapshot(dataset string, name string, destroyNewer bool) error {
	_, err := runZfs(rollbackArgs(dataset, name, destroyNewer)...)
	return err
}

// rollbackArgs returns the arguments of zfs for RollbackSnapshot. Never "-R", which would also destroy the clones of
// the newer snapshots.
func rollbackArgs(dataset string, name string, destroyNewer bool) []string {
	args := []string{"rollback"}
	if destroyNewer {
		args = append(args, "-r")
	}
	return append(args, dataset+"@"+name)
}

func (System) PreviewDestroyDataset(name string, recursive bool) (*DestroyPreview, error) {
	output, err := runZfs(destroyDatasetArgs(name, recursive, true)...)
	if err != nil {
		return nil, err
	}
	return parseDestroyPreview(output, false)
}

func (System) DestroyDataset(name string, recursive bool) error {
	_, err := runZfs(destroyDatasetArgs(name, recursive, false)...)
	return err
}

// destroyDatasetArgs returns the arguments of zfs for destroying the dataset, with dryRun only for listing what would
// be destroyed ("-nvp"). Never "-R", which would also destroy the clones of its snapshots, wherever they are.
func destroyDatasetArgs(name string, recursive bool, dryRun bool) []string {
	args := []string{"destroy"}
	if dryRun {
		args = append(args, "-n", "-v", "-p")
	}
	if recursive {
		args = append(args, "-r")
	}
	return append(args, name)
}

func (System) PromoteDataset(name string) error {
	_, err := runZfs("promote", name)
	return err
}

// poolListProperties are the properties requested by ListPools, in the order parsePoolList expects them.
var poolListProperties = []string{"name", "size", "allocated", "free", "fragmentation", "capacity", "dedupratio", "health"}

func (System) ListPools() ([]*PoolListEntry, error) {
	output, err := runZpool("list", "-H", "-p", "-o", strings.Join(poolListProperties, ","))
	if err != nil {
		return nil, err
	}
	return parsePoolList(output)
}

// poolStatusArgs are the arguments of "zpool status": exact numbers (-p) and the TRIM state of the devices (-t).
var poolStatusArgs = []string{"status", "-p", "-t"}

func (System) PoolStatus(name string) (*PoolStatus, error) {
	output, err := runZpool(append(poolStatusArgs, name)...)
	if err != nil {
		return nil, err
	}
	return parsePoolStatus(output)
}

func (System) PoolStatuses() ([]*PoolStatus, error) {
	output, err := runZpool(poolStatusArgs...)
	if err != nil {
		return nil, err
	}
	return parsePoolStatuses(output)
}

// PoolHistory reads the history of the pool, which needs root: unless the application runs as root, zpool runs with
// "sudo -n", which works with the sudoers rule of the setup command. Without root zpool fails anyway, so it is not
// tried first. Former versions of the rule only allow the history of all pools, which is read up to the end of the
// pool then.
func (System) PoolHistory(ctx context.Context, pool string, progress func(PoolHistoryProgress)) ([]PoolHistoryEntry, error) {
	read := func(name string, args ...string) ([]PoolHistoryEntry, error) {
		return readPoolHistory(pool, progress, func(onLine func(line []byte) bool) error {
			return streamCommand(ctx, onLine, name, args...)
		})
	}
	if isRoot() {
		return read(ZpoolPath(), poolHistoryArgs(pool)...)
	}
	entries, err := read("sudo", append([]string{"-n", ZpoolPath()}, poolHistoryArgs(pool)...)...)
	if err != nil && isSudoDenied(err) {
		entries, err = read("sudo", append([]string{"-n", ZpoolPath()}, historyArgs...)...)
		if err != nil && isSudoDenied(err) {
			return nil, fmt.Errorf("%w. %s", err, SetupHistoryHint)
		}
	}
	return entries, err
}

// DataErrorStatuses lists the damaged files, which needs root: if zpool does not list them, it is run again with
// "sudo -n", which works with the sudoers rule of the setup command.
func (System) DataErrorStatuses() ([]*PoolStatus, []string, error) {
	statuses, err := listDataErrorStatuses(false)
	if err != nil {
		return nil, nil, err
	}
	// root (also with sudo) gets the lists of all pools: a missing errors field has another reason then
	var unavailable []string
	if !isRoot() {
		unavailable = deniedPools(statuses)
	}
	if len(unavailable) > 0 {
		sudoStatuses, sudoErr := listDataErrorStatuses(true)
		if sudoErr == nil {
			statuses, unavailable = sudoStatuses, nil
		} else if !isSudoDenied(sudoErr) {
			return nil, nil, sudoErr
		}
	}
	return statuses, unavailable, nil
}

// listDataErrorStatuses returns the statuses of all pools with their damaged files, with "sudo -n" if asked to.
func listDataErrorStatuses(sudo bool) ([]*PoolStatus, error) {
	var output string
	var err error
	if sudo {
		output, err = runSudoCommand(append([]string{"-n", ZpoolPath()}, dataErrorsArgs...)...)
	} else {
		output, err = runPoolCommand("zpool", dataErrorsArgs...)
	}
	if err != nil {
		return nil, err
	}
	return parsePoolStatuses(output)
}

// PoolsWithoutHeadErrlog needs no root. Empty if it cannot be read (e.g. a version of ZFS without the feature).
func (System) PoolsWithoutHeadErrlog(pools []string) []string {
	if len(pools) == 0 {
		return nil
	}
	output, err := runPoolCommand("zpool", append([]string{"get", "-H", "-o", "name,value", headErrlogFeature}, pools...)...)
	if err != nil {
		return nil
	}
	var disabled []string
	for _, line := range strings.Split(output, "\n") {
		if name, value, ok := strings.Cut(line, "\t"); ok && strings.TrimSpace(value) == "disabled" {
			disabled = append(disabled, name)
		}
	}
	return disabled
}

// compatibilityDirs are the folders zpool reads the files of the property compatibility from, the first one that
// has a file wins (zpoolprops(7)). Replaceable in tests.
var compatibilityDirs = []string{"/etc/zfs/compatibility.d", "/usr/share/zfs/compatibility.d", "/usr/local/share/zfs/compatibility.d"}

// moduleFeaturesDir lists the features the loaded kernel module supports, a folder per feature with its short name
// in the file "uname" (Linux, OpenZFS 2.1+). Replaceable in tests.
var moduleFeaturesDir = "/sys/module/zfs/features.pool"

// PoolFeatures needs no root: "zpool upgrade -v" lists the features the installed ZFS supports, "zpool get" their
// states on the pool, sysfs the features of the loaded kernel module.
func (System) PoolFeatures(pool string) (*PoolFeatures, error) {
	descriptions, err := runPoolCommand("zpool", "upgrade", "-v")
	if err != nil {
		return nil, err
	}
	properties, err := runPoolCommand("zpool", "get", "-H", "-o", "property,value", "all", pool)
	if err != nil {
		return nil, err
	}
	states, unsupported, compatibility := parseFeatureStates(properties)
	features := &PoolFeatures{Pool: pool, Compatibility: compatibility}
	for _, feature := range parseFeatureDescriptions(descriptions) {
		// zpool lists the states of the features it knows itself, as "zpool upgrade -v" does
		if state, ok := states[feature.Name]; ok {
			feature.State = state
			features.Features = append(features.Features, feature)
		}
	}
	features.Features = append(features.Features, unsupported...)
	features.Compatible, features.CompatibilityErr = readCompatibilityFiles(compatibilityFileNames(compatibility))
	features.ModuleFeatures = readModuleFeatures()
	// only to tell the versions, older versions of ZFS have no "zfs version"
	if output, err := runPoolCommand("zfs", "version"); err == nil {
		features.ToolsVersion, features.ModuleVersion = parseZfsVersion(output)
	}
	return features, nil
}

// readModuleFeatures returns the short names of the features the loaded kernel module supports, nil if it does not
// list them (see moduleFeaturesDir).
func readModuleFeatures() map[string]bool {
	entries, err := os.ReadDir(moduleFeaturesDir)
	if err != nil || len(entries) == 0 {
		return nil
	}
	features := map[string]bool{}
	for _, entry := range entries {
		name, err := os.ReadFile(moduleFeaturesDir + "/" + entry.Name() + "/uname")
		if err != nil {
			// a list that cannot be read completely would mark supported features as unsupported
			return nil
		}
		features[strings.TrimSpace(string(name))] = true
	}
	return features
}

// readCompatibilityFiles returns the features all the compatibility files with the given names allow, nil without
// names.
func readCompatibilityFiles(names []string) (map[string]bool, error) {
	var sets []map[string]bool
	for _, name := range names {
		if strings.Contains(name, "/") || strings.HasPrefix(name, ".") {
			return nil, fmt.Errorf("invalid compatibility file name %q", name)
		}
		found := false
		for _, dir := range compatibilityDirs {
			content, err := os.ReadFile(dir + "/" + name)
			if err == nil {
				sets = append(sets, parseCompatibilityFile(string(content)))
				found = true
				break
			}
			if !os.IsNotExist(err) {
				return nil, err
			}
		}
		if !found {
			return nil, fmt.Errorf("the compatibility file %s is not in %s", name, strings.Join(compatibilityDirs, ", "))
		}
	}
	return intersectFeatures(sets), nil
}

// RunPoolAction runs the action as the current user. Pool operations cannot be delegated to users like dataset ones
// (zfs allow), so if zpool is denied, it is run again with "sudo -n", which works with the sudoers rule of
// "setup --pool-actions" (or as root).
func (System) RunPoolAction(action PoolAction, pool string) error {
	args := append(append([]string{}, action.Args...), pool)
	_, err := runPoolCommand("zpool", args...)
	if err == nil || !isPermissionDenied(err) {
		return err
	}

	_, sudoErr := runSudoCommand(append([]string{"-n", ZpoolPath()}, args...)...)
	if sudoErr == nil {
		return nil
	}
	if isSudoDenied(sudoErr) {
		// the original error says more
		return &PoolActionNeedsRootError{Action: action, Pool: pool, Cause: err}
	}
	// run by root, but zpool failed, e.g. "there is no active scrub"
	return sudoErr
}

// RunDatasetCommand runs zfs as the current user. Linux only lets root mount and unmount, so if zfs is denied, it is
// run again with "sudo -n", which works as root, or for a user sudo does not ask for a password.
func (System) RunDatasetCommand(args []string) error {
	_, err := runDatasetCommand("zfs", args...)
	if err == nil || !needsRoot(err) {
		return err
	}
	_, sudoErr := runSudoCommand(append([]string{"-n", ZfsPath()}, args...)...)
	if sudoErr == nil {
		return nil
	}
	if isSudoDenied(sudoErr) {
		// the original error says more
		return &DatasetCommandNeedsRootError{Commands: zfsCommandLines([][]string{args}), Cause: err}
	}
	// run by root, but zfs failed, e.g. "target is busy"
	return sudoErr
}

// WatchEvents follows "zpool events -v -f" with "sudo -n" (allowed by SudoersRuleEvents). Events from before the
// start are skipped.
func (System) WatchEvents(ctx context.Context, onDatasetsChanged func(), onPoolsChanged func(), onHistory func(PoolHistoryEntry)) error {
	cmd := exec.CommandContext(ctx, "sudo", "-n", ZpoolPath(), "events", "-v", "-f")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	parseZpoolEvents(stdout, time.Now(), onDatasetsChanged, onPoolsChanged, onHistory)
	return cmd.Wait()
}
