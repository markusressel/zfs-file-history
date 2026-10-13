package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/zfs"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// sudoersFilePath is the file the setup command installs the sudoers rules to.
const sudoersFilePath = "/etc/sudoers.d/zfs-file-history"

// setupFlags are the flags of the setup command, one per rule (see zfs.SudoersRules), by rule id.
var setupFlags = map[string]*bool{}

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Allow your user to run the zpool commands zfs-file-history needs root for (sudoers rules)",
	Long: `Some features of zfs-file-history need zpool commands that only root may run:
ZFS cannot delegate them to users like it does for datasets (zfs allow).

This command allows the current user to run ONLY these specific commands with
sudo, without a password, with rules in /etc/sudoers.d/zfs-file-history:

` + setupRulesHelp() + `
Without flags, it asks for each rule that is not allowed yet. With flags, it adds
the given rules without asking. The rules allowed before are kept, so it can be
run again to allow more. The rules are checked with visudo before installing.

zfs-file-history also offers this in the application ("Set up permissions" in
the command menu), and asks for your sudo password in the terminal.

You MUST run this command with sudo:
  sudo zfs-file-history setup [--events] [--data-errors] [--pool-actions]`,
	Run: func(cmd *cobra.Command, args []string) {
		// When run with sudo, $USER becomes "root".
		// We need $SUDO_USER to get the name of the actual person running the command.
		targetUser := os.Getenv("SUDO_USER")
		if targetUser == "" {
			logging.Fatal("This command must be run with sudo: sudo zfs-file-history setup")
			return
		}

		content, err := os.ReadFile(sudoersFilePath)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			logging.Fatal("Failed to read the sudoers file: %v", err)
			return
		}
		file := zfs.ParseSudoersFile(string(content))

		rules := flaggedRules()
		if len(rules) == 0 {
			if term.IsTerminal(int(os.Stdin.Fd())) {
				rules = askForRules(os.Stdin, os.Stdout, file, targetUser)
			} else {
				// e.g. a script: the rules that only read, as before there were flags
				for _, rule := range zfs.SudoersRules {
					if rule.ReadOnly {
						rules = append(rules, rule)
					}
				}
			}
		}

		added := addRules(file, targetUser, rules)
		if len(added) == 0 {
			fmt.Printf("Nothing to change for user %s.\n", targetUser)
			return
		}
		if err := installSudoersFile(sudoersFilePath, file.String()); err != nil {
			logging.Fatal("Failed to install the sudoers file: %v", err)
			return
		}

		logging.Info("Allowed %s for user %s in %s", strings.Join(added, ", "), targetUser, sudoersFilePath)
		fmt.Printf("Allowed %s for user %s in %s\n", strings.Join(added, ", "), targetUser, sudoersFilePath)
	},
}

// setupRulesHelp describes the rules for the help of the setup command, with their flags.
func setupRulesHelp() string {
	var help strings.Builder
	for _, rule := range zfs.SudoersRules {
		fmt.Fprintf(&help, "  --%s: %s\n%s\n", rule.Id, rule.Name, indent(wrap(rule.Description, 72), "    "))
	}
	return help.String()
}

// flaggedRules returns the rules whose flags are set.
func flaggedRules() []*zfs.SudoersRule {
	var rules []*zfs.SudoersRule
	for _, rule := range zfs.SudoersRules {
		if *setupFlags[rule.Id] {
			rules = append(rules, rule)
		}
	}
	return rules
}

// askForRules asks the user for each rule that the file does not grant yet whether to allow it. The ones that only
// read are allowed by default (Enter), the others not.
func askForRules(in io.Reader, out io.Writer, file *zfs.SudoersFile, user string) []*zfs.SudoersRule {
	reader := bufio.NewReader(in)
	var rules []*zfs.SudoersRule
	for _, rule := range zfs.SudoersRules {
		if file.Has(user, rule) {
			_, _ = fmt.Fprintf(out, "%s: already allowed\n", rule.Name)
			continue
		}
		choices := "[y/N]"
		if rule.ReadOnly {
			choices = "[Y/n]"
		}
		_, _ = fmt.Fprintf(out, "\n%s\n%s\nAllow for %s? %s ", rule.Name, indent(wrap(rule.Description, 76), "  "), user, choices)
		answer, err := reader.ReadString('\n')
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer == "y" || answer == "yes" || (answer == "" && rule.ReadOnly && err == nil) {
			rules = append(rules, rule)
		}
		if err != nil {
			// e.g. the input was closed: nothing more is asked
			_, _ = fmt.Fprintln(out)
			break
		}
	}
	_, _ = fmt.Fprintln(out)
	return rules
}

// addRules grants the rules to the user in the file, and returns the names of the ones that were not granted yet.
func addRules(file *zfs.SudoersFile, user string, rules []*zfs.SudoersRule) []string {
	var added []string
	for _, rule := range rules {
		if !file.Has(user, rule) {
			file.Grant(user, rule)
			added = append(added, rule.Name)
		}
	}
	return added
}

// wrap breaks the text into lines of at most width characters, at spaces.
func wrap(text string, width int) string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		if line != "" && len(line)+1+len(word) > width {
			lines = append(lines, line)
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += word
	}
	return strings.Join(append(lines, line), "\n")
}

// indent puts prefix in front of every line of the text.
func indent(text string, prefix string) string {
	return prefix + strings.ReplaceAll(text, "\n", "\n"+prefix)
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
	for _, rule := range zfs.SudoersRules {
		setupFlags[rule.Id] = setupCmd.Flags().Bool(rule.Id, false, "Allow: "+rule.Name)
	}
	rootCmd.AddCommand(setupCmd)
}
