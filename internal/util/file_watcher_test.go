package util

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// Regression: after the first event, the action ran every second (reloading the directory) until the next one.
func TestFileWatcherCallsTheActionOncePerChange(t *testing.T) {
	previousInterval := watchInterval
	watchInterval = 20 * time.Millisecond
	t.Cleanup(func() { watchInterval = previousInterval })

	tempDir := t.TempDir()
	fw := NewFileWatcher(tempDir)
	eventChan := make(chan string, 100)
	require.NoError(t, fw.Watch(func(path string) { eventChan <- path }))
	t.Cleanup(fw.Stop)

	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "test.txt"), []byte("hello"), 0644))
	select {
	case <-eventChan:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for file watcher event")
	}

	// many intervals without a change: no further action
	time.Sleep(10 * watchInterval)
	assert.Empty(t, eventChan)

	// the next change is handled again
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "test.txt"), []byte("changed"), 0644))
	select {
	case <-eventChan:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the second file watcher event")
	}
}
