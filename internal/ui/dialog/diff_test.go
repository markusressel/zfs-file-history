package dialog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsBinaryFile(t *testing.T) {
	tempDir := t.TempDir()

	// Text file
	textFile := filepath.Join(tempDir, "text.txt")
	err := os.WriteFile(textFile, []byte("Hello world, this is a plain text file."), 0644)
	assert.NoError(t, err)
	assert.False(t, IsBinaryFile(textFile))

	// Binary file (contains null byte)
	binFile := filepath.Join(tempDir, "binary.bin")
	err = os.WriteFile(binFile, []byte{0x48, 0x65, 0x6c, 0x6c, 0x6f, 0x00, 0x57, 0x6f, 0x72, 0x6c, 0x64}, 0644)
	assert.NoError(t, err)
	assert.True(t, IsBinaryFile(binFile))

	// Non-existent file
	assert.False(t, IsBinaryFile(filepath.Join(tempDir, "does-not-exist")))
	assert.False(t, IsBinaryFile(DevNull))
}

func TestFormatDiffText(t *testing.T) {
	diffInput := `--- old.txt
+++ new.txt
@@ -1,3 +1,3 @@
-Hello
+World
 Unchanged`

	// Test without filtering headers
	outNoFilter := FormatDiffText(diffInput, false)
	assert.Contains(t, outNoFilter, "[red]-Hello[white]")
	assert.Contains(t, outNoFilter, "[green]+World[white]")
	assert.Contains(t, outNoFilter, "--- old.txt")
	assert.Contains(t, outNoFilter, "+++ new.txt")

	// Test with filtering headers
	outWithFilter := FormatDiffText(diffInput, true)
	assert.Contains(t, outWithFilter, "[red]-Hello[white]")
	assert.Contains(t, outWithFilter, "[green]+World[white]")
	assert.NotContains(t, outWithFilter, "--- old.txt")
	assert.NotContains(t, outWithFilter, "+++ new.txt")
}
