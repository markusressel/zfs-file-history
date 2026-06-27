package util

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFileWatcher(t *testing.T) {
	tempDir := t.TempDir()

	fw := NewFileWatcher(tempDir)
	assert.Equal(t, tempDir, fw.RootPath)

	eventChan := make(chan string, 10)
	action := func(path string) {
		eventChan <- path
	}

	err := fw.Watch(action)
	assert.NoError(t, err)

	// Write a new file
	testFile := filepath.Join(tempDir, "test.txt")
	err = os.WriteFile(testFile, []byte("hello"), 0644)
	assert.NoError(t, err)

	// Wait for event or timeout
	select {
	case eventPath := <-eventChan:
		assert.NotEmpty(t, eventPath)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for file watcher event")
	}

	fw.Stop()
	time.Sleep(100 * time.Millisecond)
}

func TestFileWatcherRecursive(t *testing.T) {
	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "subdir")
	err := os.Mkdir(subDir, 0755)
	assert.NoError(t, err)

	fw := NewFileWatcher(tempDir)
	eventChan := make(chan string, 10)
	action := func(path string) {
		eventChan <- path
	}

	fw.WatchRecursive(action)

	// Write a new file in subdir
	testFile := filepath.Join(subDir, "test.txt")
	err = os.WriteFile(testFile, []byte("hello"), 0644)
	assert.NoError(t, err)

	// Wait for event or timeout
	select {
	case eventPath := <-eventChan:
		assert.Contains(t, eventPath, "subdir")
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for recursive file watcher event")
	}

	fw.Stop()
	time.Sleep(100 * time.Millisecond)
}
