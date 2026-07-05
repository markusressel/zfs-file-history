package cmd

import (
	"fmt"
	"os"
	"zfs-file-history/internal/logging"

	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Install the sudoers rule required for real-time ZFS event monitoring",
	Long: `In order to watch for snapshot creations and destructions in real-time,
zfs-file-history needs permission to run 'zpool events' without a password.

This command creates a minimal, secure sudoers rule in /etc/sudoers.d/zfs-file-history
that grants the current user access ONLY to that specific command.

You MUST run this command with sudo:
  sudo zfs-file-history setup`,
	Run: func(cmd *cobra.Command, args []string) {
		// When run with sudo, $USER becomes "root".
		// We need $SUDO_USER to get the name of the actual person running the command.
		targetUser := os.Getenv("SUDO_USER")
		if targetUser == "" {
			logging.Fatal("This command must be run with sudo: sudo zfs-file-history setup")
			return
		}

		rule := fmt.Sprintf("%s ALL=(root) NOPASSWD: /usr/sbin/zpool events -v -f, /sbin/zpool events -v -f, /usr/bin/zpool events -v -f\n", targetUser)
		filePath := "/etc/sudoers.d/zfs-file-history"

		// Sudoers files MUST strictly have 0440 permissions, or sudo will break on the system.
		err := os.WriteFile(filePath, []byte(rule), 0440)
		if err != nil {
			logging.Fatal("Failed to write sudoers file: %v", err)
			return
		}

		logging.Info("Successfully installed sudoers rule for user %s at %s", targetUser, filePath)
		pterm.Print("Successfully installed sudoers rule for user %s at %s\n", targetUser, filePath)
	},
}

func init() {
	rootCmd.AddCommand(setupCmd)
}
