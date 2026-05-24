package file_browser

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"zfs-file-history/internal/logging"

	"github.com/rivo/tview"
)

type ExternalDiffViewerConfig struct {
	Path        string
	Args        []string
	WrapInPager bool
}

func (c ExternalDiffViewerConfig) computeRunArgs() []string {
	args := make([]string, len(c.Args))
	copy(args, c.Args)
	return args
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

func determineExternalDiffViewer(editorPath string) (editorConfig *ExternalDiffViewerConfig) {
	if editorPath != "" {
		editorConfig = findEditorOption(editorPath, EditorOptions)
		if editorConfig != nil {
			return editorConfig
		}

		logging.Warning("Configured external editor '%s' is not recognized. Falling back to internal diff.", editorPath)
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
		editorConfig = findEditorOption(editorPath, EditorOptions)
		if editorConfig == nil {
			logging.Error("EDITOR environment variable is set to '%s' but it is not a recognized editor. Falling back to internal diff.", editorEnvValue)
		} else {
			return editorConfig
		}
	}

	return nil
}

func runExternalDiffEditor(application *tview.Application, editorConf ExternalDiffViewerConfig, snapshotFilePath string, realFilePath string) {
	var args []string
	args = append(args, editorConf.Args...)
	args = append(args, snapshotFilePath)
	args = append(args, realFilePath)

	var cmd *exec.Cmd
	if editorConf.WrapInPager {
		// pipe the output into "less -R" to make it blocking
		editorCommandArgsString := strings.Join(editorConf.Args, " ")
		editorCommand := fmt.Sprintf("%s %s '%s' '%s' | less -R", editorConf.Path, editorCommandArgsString, snapshotFilePath, realFilePath)
		cmd = exec.Command("sh", "-c", editorCommand)
	} else {
		cmd = exec.Command(editorConf.Path, args...)
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	application.Suspend(func() {
		runErr := cmd.Run()
		if runErr != nil {
			logging.Error("Error running external diff editor: %v", runErr)
		}
	})
	// After suspend, ensure the tview application redraws itself
	application.Sync()
	application.Draw()
}
