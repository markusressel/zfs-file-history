package util

import (
	"github.com/fsnotify/fsnotify"
	"os"
	"path/filepath"
	"sync"
	"time"
	"zfs-file-history/internal/logging"
)

type FileWatcher struct {
	RootPath   string
	stop       chan bool
	actionLock sync.Mutex
	watcher    *fsnotify.Watcher
	newEvent   *fsnotify.Event
}

func NewFileWatcher(path string) *FileWatcher {
	return &FileWatcher{
		RootPath:   path,
		stop:       make(chan bool),
		actionLock: sync.Mutex{},
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
	err := fileWatcher.watcher.Add(path)
	if err != nil {
		return err
	}

	t := time.NewTicker(1 * time.Second)

	go func() {
		for {
			select {
			case <-t.C:
				if fileWatcher.newEvent == nil {
					continue
				}

				fileWatcher.actionLock.Lock()
				event := fileWatcher.newEvent
				action(event.Name)
				fileWatcher.actionLock.Unlock()
			// watch for events
			case event, ok := <-fileWatcher.watcher.Events:
				if !ok {
					return
				}
				fileWatcher.newEvent = &event
			// watch for errors
			case err := <-fileWatcher.watcher.Errors:
				if err != nil {
					logging.Error(err.Error())
				}
			case <-fileWatcher.stop:
				err := fileWatcher.watcher.Close()
				if err != nil {
					logging.Error(err.Error())
				}
				return
			}
		}
	}()
	return nil
}

// watches all files and folders in the given path recursively
func (fileWatcher *FileWatcher) watchDirRecursive(path string, action func(s string)) {
	// creates a new file watcher
	fileWatcher.watcher, _ = fsnotify.NewWatcher()

	// starting at the root of the project, walk each file/directory searching for directories
	if err := filepath.Walk(path, fileWatcher.addFolderWatch); err != nil {
		logging.Error(err.Error())
		return
	}

	go func() {
		for {
			select {
			// watch for events
			case event, ok := <-fileWatcher.watcher.Events:
				if !ok {
					return
				}
				fileWatcher.actionLock.Lock()
				action(event.Name)
				fileWatcher.actionLock.Unlock()
			// watch for errors
			case err := <-fileWatcher.watcher.Errors:
				logging.Error(err.Error())
			case <-fileWatcher.stop:
				err := fileWatcher.watcher.Close()
				if err != nil {
					logging.Error(err.Error())
				}
				return
			}
		}
	}()
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
