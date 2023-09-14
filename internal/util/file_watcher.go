package util

import (
	"github.com/fsnotify/fsnotify"
	"os"
	"path/filepath"
	"zfs-file-history/internal/logging"
)

type FileWatcher struct {
	RootPath string
	stop     chan bool
	watcher  *fsnotify.Watcher
}

func NewFileWatcher(path string) *FileWatcher {
	return &FileWatcher{
		RootPath: path,
		stop:     make(chan bool),
	}
}

func (fileWatcher *FileWatcher) Watch(action func(s string)) error {
	return fileWatcher.watchDir(fileWatcher.RootPath, action)
}

func (fileWatcher *FileWatcher) WatchRecursive(action func(s string)) {
	fileWatcher.watchDirRecursive(fileWatcher.RootPath, action)
}

func (fileWatcher *FileWatcher) Stop() {
	go func() {
		fileWatcher.stop <- true
	}()
}

// watches all files and folders in the given path recursively
func (fileWatcher *FileWatcher) watchDir(path string, action func(s string)) error {
	// creates a new file watcher
	fileWatcher.watcher, _ = fsnotify.NewWatcher()

	go func() {
		for {
			select {
			case <-fileWatcher.stop:
				err := fileWatcher.watcher.Close()
				if err != nil {
					logging.Error(err.Error())
				}
				break
			// watch for events
			case event := <-fileWatcher.watcher.Events:
				if event.Name != "" {
					action(event.Name)
				} else {
					break
				}
			// watch for errors
			case err := <-fileWatcher.watcher.Errors:
				if err != nil {
					logging.Error(err.Error())
				}
			}
		}
	}()

	return fileWatcher.watcher.Add(path)
}

// watches all files and folders in the given path recursively
func (fileWatcher *FileWatcher) watchDirRecursive(path string, action func(s string)) {
	// creates a new file watcher
	fileWatcher.watcher, _ = fsnotify.NewWatcher()

	go func() {
		for {
			select {
			// watch for events
			case event := <-fileWatcher.watcher.Events:
				action(event.Name)
			// watch for errors
			case err := <-fileWatcher.watcher.Errors:
				logging.Error(err.Error())
			case <-fileWatcher.stop:
				err := fileWatcher.watcher.Close()
				if err != nil {
					logging.Error(err.Error())
				}
				break
			}
		}
	}()

	// starting at the root of the project, walk each file/directory searching for directories
	if err := filepath.Walk(path, fileWatcher.addFolderWatch); err != nil {
		logging.Error(err.Error())
	}
}

// adds a path to the watcher
func (fileWatcher *FileWatcher) addFolderWatch(path string, fi os.FileInfo, err error) error {
	// since fsnotify can watch all the files in a directory, watchers only need
	// to be added to each nested directory
	if err != nil {
		return err
	}

	if fi.Mode().IsDir() {
		return fileWatcher.watcher.Add(path)
	}

	return nil
}
