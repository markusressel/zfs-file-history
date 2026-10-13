package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/zfs"

	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
)

// sudoersFilePath is the file the setup command installs the sudoers rules to.
const sudoersFilePath = "/etc/sudoers.d/zfs-file-history"

// setupPoolActions is the --pool-actions flag of the setup command.
var setupPoolActions bool

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Install the sudoers rules for real-time ZFS event monitoring and damaged files (and optionally pool actions)",
	Long: `In order to watch for snapshot creations and destructions in real-time,
zfs-file-history needs permission to run 'zpool events' without a password.
To show which files are damaged (data errors), it needs to run 'zpool status -v',
which only lists them for root.

This command creates a minimal, secure sudoers rule in /etc/sudoers.d/zfs-file-history
that grants the current user access ONLY to these specific commands.

With --pool-actions, the user may also start, pause and cancel scrubs, start and
cancel TRIMs and clear the errors of pools (zpool scrub, trim and clear), which need
root. Nothing else, in particular no destroying, exporting or rewinding of pools.

You MUST run this command with sudo:
  sudo zfs-file-history setup [--pool-actions]`,
	Run: func(cmd *cobra.Command, args []string) {
		// When run with sudo, $USER becomes "root".
		// We need $SUDO_USER to get the name of the actual person running the command.
		targetUser := os.Getenv("SUDO_USER")
		if targetUser == "" {
			logging.Fatal("This command must be run with sudo: sudo zfs-file-history setup")
			return
		}

		err := installSudoersFile(sudoersFilePath, sudoersRules(targetUser, setupPoolActions))
		if err != nil {
			logging.Fatal("Failed to install the sudoers file: %v", err)
			return
		}

		logging.Info("Successfully installed sudoers rule for user %s at %s", targetUser, sudoersFilePath)
		pterm.Printf("Successfully installed sudoers rule for user %s at %s\n", targetUser, sudoersFilePath)
	},
}

// sudoersRules returns the content of the sudoers file for the user: the zpool events, and with poolActions the
// pool actions as well (see zfs.PoolActions).
func sudoersRules(user string, poolActions bool) string {
	rules := "# installed by zfs-file-history setup\n" + zfs.SudoersEventsRule(user)
	if poolActions {
		rules += zfs.SudoersPoolActionRules(user)
	}
	return rules
}

// installSudoersFile writes the rules to path, after visudo checked them: a broken file in /etc/sudoers.d breaks
// sudo for the whole system. The file is written next to path first, with a name sudo ignores (it contains a dot).
func installSudoersFile(path string, rules string) error {
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()

	_, err = temp.WriteString(rules)
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	// sudoers files MUST strictly have 0440 permissions, or sudo will break on the system
	if err := os.Chmod(tempPath, 0440); err != nil {
		return err
	}
	if output, err := exec.Command("visudo", "-c", "-q", "-f", tempPath).CombinedOutput(); err != nil {
		return fmt.Errorf("the rules are not valid: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return os.Rename(tempPath, path)
}

func init() {
	setupCmd.Flags().BoolVar(&setupPoolActions, "pool-actions", false,
		"Also allow scrubbing, trimming and clearing the errors of pools (zpool scrub, trim and clear)")
	rootCmd.AddCommand(setupCmd)
}
