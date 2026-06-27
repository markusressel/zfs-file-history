package zfs

import (
	"context"
	"strings"
	"sync"
	"zfs-file-history/internal/util"

	golibzfs "github.com/kraudcloud/go-libzfs"
)

const (
	SnapshotTimeFormat = "2006-01-02-150405"
)

var (
	DatasetsLoaded = util.NewEmitter[struct{}]()
	datasetCache   = make(map[string]*golibzfs.Dataset)
	cacheMtx       sync.RWMutex
)

func RefreshZfsData() {
	cacheMtx.Lock()
	datasetCache = make(map[string]*golibzfs.Dataset)
	cacheMtx.Unlock()

	DatasetsLoaded.Emit(struct{}{})
}

func IsDatasetsLoaded() bool {
	return true
}

func WaitForDatasets(ctx context.Context) error {
	return nil
}

func findSnapshot(snapshots []golibzfs.Dataset, name string) *golibzfs.Dataset {
	for i := range snapshots {
		nameProperty := snapshots[i].Properties[golibzfs.DatasetPropName]
		parts := strings.Split(nameProperty.Value, "@")
		if len(parts) < 2 {
			continue
		}
		currentName := parts[1]
		if currentName == name {
			return &snapshots[i]
		}
	}
	return nil
}
