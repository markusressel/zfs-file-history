package dataset_info

import (
	"slices"
	"strings"
	"zfs-file-history/internal/zfs"
)

// datasetOffset returns the space before the dataset in the bar of its pool, false if a parent of it is not listed.
// The bar shows the datasets of the pool in the order of their tree: the own space of each one (data, snapshots,
// refreservation) before its children, siblings sorted by name. So every dataset keeps its place, and a child lies
// within its parent: moving between them only moves the highlight. Datasets that are not listed (volumes, see
// zfs.ListAllDatasets) come after the listed children of their parent.
func datasetOffset(entries []*zfs.DatasetListEntry, name string) (uint64, bool) {
	byName := make(map[string]*zfs.DatasetListEntry, len(entries))
	children := map[string][]*zfs.DatasetListEntry{}
	for _, entry := range entries {
		byName[entry.Name] = entry
		if parent, isChild := parentOf(entry.Name); isChild {
			children[parent] = append(children[parent], entry)
		}
	}
	// the pool, then each parent down to the dataset
	var path []string
	for i, char := range name {
		if char == '/' {
			path = append(path, name[:i])
		}
	}
	path = append(path, name)

	offset := uint64(0)
	for i, ancestor := range path[:len(path)-1] {
		entry := byName[ancestor]
		if entry == nil {
			return 0, false
		}
		offset += entry.Used - min(entry.UsedByChildren, entry.Used)
		next := path[i+1]
		_, nextListed := byName[next]
		for _, sibling := range children[ancestor] {
			if !nextListed || sibling.Name < next {
				offset += sibling.Used
			}
		}
	}
	return offset, true
}

// parentOf returns the parent of the dataset, false for a pool.
func parentOf(name string) (string, bool) {
	index := strings.LastIndex(name, "/")
	if index < 0 {
		return "", false
	}
	return name[:index], true
}

// poolDatasets returns the datasets of the pool from the listing of all datasets.
func poolDatasets(entries []*zfs.DatasetListEntry, pool string) []*zfs.DatasetListEntry {
	return slices.DeleteFunc(slices.Clone(entries), func(entry *zfs.DatasetListEntry) bool {
		return entry.Name != pool && !strings.HasPrefix(entry.Name, pool+"/")
	})
}
