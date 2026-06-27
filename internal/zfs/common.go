package zfs

import (
	"context"
	"strings"
	"sync"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/util"

	golibzfs "github.com/kraudcloud/go-libzfs"
)

const (
	SnapshotTimeFormat = "2006-01-02-150405"
)

var (
	allDatasets      = []golibzfs.Dataset{}
	datasetsMtx      sync.RWMutex
	DatasetsLoaded   = util.NewEmitter[struct{}]()
	isDatasetsLoaded bool
	isLoading        bool
	loadingMtx       sync.Mutex

	waiters    []chan struct{}
	waitersMtx sync.Mutex
)

func RefreshZfsData() {
	loadingMtx.Lock()
	if isLoading {
		loadingMtx.Unlock()
		return
	}
	isLoading = true
	isDatasetsLoaded = false

	datasetsMtx.Lock()
	allDatasets = []golibzfs.Dataset{}
	datasetsMtx.Unlock()

	loadingMtx.Unlock()

	go func() {
		defer func() {
			loadingMtx.Lock()
			isLoading = false
			loadingMtx.Unlock()
		}()
		loadDatasets()
	}()
}

func IsDatasetsLoaded() bool {
	datasetsMtx.RLock()
	defer datasetsMtx.RUnlock()
	return isDatasetsLoaded
}

func WaitForDatasets(ctx context.Context) error {
	datasetsMtx.RLock()
	loaded := isDatasetsLoaded
	datasetsMtx.RUnlock()
	if loaded {
		return nil
	}

	ch := make(chan struct{})
	waitersMtx.Lock()
	waiters = append(waiters, ch)
	waitersMtx.Unlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ch:
		return nil
	}
}

func loadDatasets() {
	datasets, err := golibzfs.DatasetOpenAll()
	datasetsMtx.Lock()
	if err != nil {
		logging.Error("Could not load ZFS datasets: %s", err.Error())
	} else {
		allDatasets = datasets
	}
	isDatasetsLoaded = true
	datasetsMtx.Unlock()

	waitersMtx.Lock()
	for _, ch := range waiters {
		close(ch)
	}
	waiters = []chan struct{}{}
	waitersMtx.Unlock()

	DatasetsLoaded.Emit(struct{}{})
}

func findDataset(datasets []golibzfs.Dataset, path string) *golibzfs.Dataset {
	datasetsMtx.RLock()
	defer datasetsMtx.RUnlock()
	return findDatasetRecursive(datasets, path)
}

func findDatasetRecursive(datasets []golibzfs.Dataset, path string) *golibzfs.Dataset {
	for _, dataset := range datasets {
		mountPoint := dataset.Properties[golibzfs.DatasetPropMountpoint]
		if mountPoint.Value == path {
			return &dataset
		}
		dataset := findDatasetRecursive(dataset.Children, path)
		if dataset != nil {
			return dataset
		}
	}
	return nil
}

func findSnapshot(snapshots []golibzfs.Dataset, name string) *golibzfs.Dataset {
	for _, snapshot := range snapshots {
		nameProperty := snapshot.Properties[golibzfs.DatasetPropName]
		parts := strings.Split(nameProperty.Value, "@")
		if len(parts) < 2 {
			continue
		}
		currentName := parts[1]
		if currentName == name {
			return &snapshot
		}
	}
	return nil
}
