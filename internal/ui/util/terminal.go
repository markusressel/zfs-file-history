package util

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/rivo/tview"
)

// RunWithSudoInTerminal suspends the UI and runs the given commands with sudo in the terminal, so sudo can ask for
// the password. explanation is printed before the commands. If a command fails, the user can read its output before
// the UI returns. Blocks until the UI is back.
// Must be called from a background goroutine, never on the UI thread (it waits for the UI thread).
func RunWithSudoInTerminal(application *tview.Application, explanation string, commands [][]string) error {
	sudoCommands := make([][]string, 0, len(commands))
	for _, command := range commands {
		sudoCommands = append(sudoCommands, append([]string{"sudo"}, command...))
	}
	return RunInTerminal(application, explanation, sudoCommands)
}

// RunInTerminal suspends the UI and runs the given commands (each a program and its arguments) in the terminal, so
// they can ask for input, e.g. zfs for the passphrase of a key. explanation is printed before the commands. If a
// command fails, the user can read its output before the UI returns. Blocks until the UI is back.
// Must be called from a background goroutine, never on the UI thread (it waits for the UI thread).
func RunInTerminal(application *tview.Application, explanation string, commands [][]string) error {
	var runErr error
	done := make(chan struct{})
	application.QueueUpdate(func() {
		suspended := application.Suspend(func() {
			runErr = runCommands(explanation, commands)
		})
		if !suspended {
			runErr = errors.New("cannot suspend the user interface to run commands in the terminal")
		}
		close(done)
	})
	<-done
	return runErr
}

// runCommands runs while the UI is suspended, so it may use the terminal.
func runCommands(explanation string, commands [][]string) error {
	fmt.Printf("\nzfs-file-history: %s\n\n", explanation)
	for _, command := range commands {
		fmt.Printf("$ %s\n", strings.Join(command, " "))
		cmd := exec.Command(command[0], command[1:]...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Printf("\nFailed: %v\nPress Enter to return to zfs-file-history.", err)
			_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
			return fmt.Errorf("%s: %w", strings.Join(command, " "), err)
		}
	}
	return nil
}
