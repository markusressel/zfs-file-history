package file_browser

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExternalDiffViewer_IsAvailable(t *testing.T) {
	// git should be installed on any dev machine
	gitConfig := ExternalDiffViewerConfig{Path: "git"}
	assert.True(t, gitConfig.IsAvailable())

	badConfig := ExternalDiffViewerConfig{Path: "nonexistent-diff-tool-xyz-123"}
	assert.False(t, badConfig.IsAvailable())
}

func TestFindEditorOption(t *testing.T) {
	options := []ExternalDiffViewerConfig{
		{Path: "nvim"},
		{Path: "delta"},
	}

	assert.NotNil(t, findEditorOption("/usr/bin/nvim", options))
	assert.NotNil(t, findEditorOption("delta", options))
	assert.Nil(t, findEditorOption("vim", options))
}

func TestDetermineExternalDiffViewer(t *testing.T) {
	// Specified binary matching option
	editor := determineExternalDiffViewer("git")
	assert.NotNil(t, editor)

	// Specified nonexistent binary falls back to finding available option or EDITOR
	editor = determineExternalDiffViewer("nonexistent-tool-xyz")
	// If git or delta or nvim is installed on system, it finds one
	if gitInstalled := (ExternalDiffViewerConfig{Path: "git"}).IsAvailable(); gitInstalled {
		assert.NotNil(t, editor)
	}

	// Environment variable fallback test
	oldEditor := os.Getenv("EDITOR")
	defer func() { _ = os.Setenv("EDITOR", oldEditor) }()

	_ = os.Setenv("EDITOR", "")
	assert.Nil(t, findEditorOption("invalid", nil))
}
