package dialog

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"zfs-file-history/internal/configuration"
	"zfs-file-history/internal/logging"

	"github.com/rivo/tview"
)

type ExternalDiffViewerConfig struct {
	Path        string   `json:"path"`
	Args        []string `json:"args"`
	WrapInPager bool     `json:"wrapInPager"`
}

func (c ExternalDiffViewerConfig) IsAvailable() bool {
	_, err := exec.LookPath(c.Path)
	return err == nil
}

var (
	// Editors
	NVIM = ExternalDiffViewerConfig{
		Path: "nvim",
		Args: []string{"-d"},
	}
	VIMDIFF = ExternalDiffViewerConfig{
		Path: "vimdiff",
	}
	KAK = ExternalDiffViewerConfig{
		Path: "kak",
		Args: []string{"-d"},
	}

	// Pagers
	Delta = ExternalDiffViewerConfig{
		Path:        "delta",
		Args:        []string{"--line-numbers", "--hunk-header-style=omit"},
		WrapInPager: true,
	}

	Difft = ExternalDiffViewerConfig{
		Path: "difft",
	}

	GitDiff = ExternalDiffViewerConfig{
		Path:        "git",
		Args:        []string{"--paginate", "diff", "--no-index", "--color=always"},
		WrapInPager: true,
	}
	Deff = ExternalDiffViewerConfig{
		Path: "deff",
	}

	EditorOptions = []ExternalDiffViewerConfig{
		NVIM,
		KAK,
		Delta,
		Difft,
		Deff,
		//VIMDIFF,
		GitDiff,
	}
)

func determineExternalDiffViewer(binaryPath string) (editorConfig *ExternalDiffViewerConfig) {
	if binaryPath != "" {
		editorConfig = findEditorOption(binaryPath, EditorOptions)
		if editorConfig != nil {
			return editorConfig
		}

		logging.Warning("Configured external diff viewer '%s' not found on system. Checking other options.", binaryPath)
	}

	for _, editor := range EditorOptions {
		if !editor.IsAvailable() {
			continue
		}

		logging.Info("Using external diff viewer: %s", editor.Path)
		return &editor
	}

	logging.Warning("No configured external diff viewer found on system. Checking EDITOR environment variable for fallback option.")

	editorEnvValue := os.Getenv("EDITOR")
	if editorEnvValue == "" {
		logging.Error("EDITOR environment variable not set and no external editor path configured. Falling back to internal diff.")
	} else {
		editorConfig = findEditorOption(editorEnvValue, EditorOptions)
		if editorConfig == nil {
			logging.Error("EDITOR environment variable is set to '%s' but it is not a recognized editor. Falling back to internal diff.", editorEnvValue)
		} else {
			return editorConfig
		}
	}

	return nil
}

func findEditorOption(path string, options []ExternalDiffViewerConfig) *ExternalDiffViewerConfig {
	for _, option := range options {
		if strings.HasSuffix(path, option.Path) {
			return &option
		}
	}
	return nil
}

// externalDiffViewer returns the diff viewer configured (or the first one installed), or nil if the diff is shown
// inside the application (configured, or no viewer found).
func externalDiffViewer() *ExternalDiffViewerConfig {
	if configuration.CurrentConfig.Diff.Mode != configuration.DiffModeExternal {
		return nil
	}
	if conf := configuration.CurrentConfig.Diff.External; conf != nil {
		return &ExternalDiffViewerConfig{Path: conf.Path, Args: conf.Args, WrapInPager: conf.WrapInPager}
	}
	return determineExternalDiffViewer("")
}

// externalDiffCommand returns the command that shows the changes from oldPath to newPath in the viewer.
func externalDiffCommand(editorConf ExternalDiffViewerConfig, oldPath string, newPath string) *exec.Cmd {
	args := append(slices.Clone(editorConf.Args), oldPath, newPath)
	if editorConf.WrapInPager {
		// Piped into "less -R", which waits for the user. The viewer and the paths are arguments of the shell, not
		// part of its script: a quote in a file name would end the quoting and run the rest as a command.
		return exec.Command("sh", append([]string{"-c", `"$@" | less -R`, "sh", editorConf.Path}, args...)...)
	}
	return exec.Command(editorConf.Path, args...)
}

// runExternalDiffEditor shows the changes from oldPath to newPath in the viewer, in the terminal of the application,
// which is suspended until the viewer exits. It returns then. Must not be called on the UI thread: it waits for it.
func runExternalDiffEditor(
	application *tview.Application,
	editorConf ExternalDiffViewerConfig,
	oldPath string,
	newPath string,
) {
	cmd := externalDiffCommand(editorConf, oldPath, newPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	done := make(chan struct{})
	// suspended on the UI thread, so tview does not read stdin while the viewer runs
	application.QueueUpdate(func() {
		defer close(done)
		suspended := application.Suspend(func() {
			if err := cmd.Run(); err != nil {
				logging.Error("Error running external diff editor: %v", err)
			}
		})
		if !suspended {
			logging.Error("Failed to suspend tview application for external diff editor")
		}
	})
	<-done
}
