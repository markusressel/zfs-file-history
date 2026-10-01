package dataset_browser

import (
	"strings"
	"zfs-file-history/internal/zfs"
)

// treeRow is how a dataset is displayed in the tree view.
type treeRow struct {
	// prefix are the tree lines in front of the name, e.g. "│  ├─ "
	prefix string
	// name is the name relative to the parent in the tree, or the full name for roots
	name string
}

const (
	treeBranch     = "├─ "
	treeLastBranch = "└─ "
	treeLine       = "│  "
	treeSpace      = "   "
)

// buildDatasetTree arranges the given (sorted) datasets as a tree: each dataset follows its parent, siblings keep
// their order. The parent of a dataset is its nearest ancestor among the given datasets, so the tree stays correct
// if datasets are hidden (e.g. unmounted ones or by a filter). Datasets without such an ancestor are roots.
// It returns the datasets in tree order, and how to display each of them.
func buildDatasetTree(sorted []*zfs.DatasetListEntry) ([]*zfs.DatasetListEntry, map[*zfs.DatasetListEntry]treeRow) {
	byName := make(map[string]*zfs.DatasetListEntry, len(sorted))
	for _, entry := range sorted {
		byName[entry.Name] = entry
	}

	var roots []*zfs.DatasetListEntry
	parents := make(map[*zfs.DatasetListEntry]*zfs.DatasetListEntry, len(sorted))
	children := make(map[*zfs.DatasetListEntry][]*zfs.DatasetListEntry)
	for _, entry := range sorted {
		parent := nearestAncestor(entry.Name, byName)
		if parent == nil {
			roots = append(roots, entry)
		} else {
			parents[entry] = parent
			children[parent] = append(children[parent], entry)
		}
	}

	result := make([]*zfs.DatasetListEntry, 0, len(sorted))
	rows := make(map[*zfs.DatasetListEntry]treeRow, len(sorted))

	var visit func(entry *zfs.DatasetListEntry, indent string, isLast bool, isRoot bool)
	visit = func(entry *zfs.DatasetListEntry, indent string, isLast bool, isRoot bool) {
		row := treeRow{name: entry.Name}
		childIndent := ""
		if !isRoot {
			row.name = strings.TrimPrefix(entry.Name, parents[entry].Name+"/")
			if isLast {
				row.prefix = indent + treeLastBranch
				childIndent = indent + treeSpace
			} else {
				row.prefix = indent + treeBranch
				childIndent = indent + treeLine
			}
		}
		rows[entry] = row
		result = append(result, entry)

		entryChildren := children[entry]
		for i, child := range entryChildren {
			visit(child, childIndent, i == len(entryChildren)-1, false)
		}
	}
	for i, root := range roots {
		visit(root, "", i == len(roots)-1, true)
	}

	return result, rows
}

// nearestAncestor returns the closest ancestor of the dataset with the given name, e.g. "pool/a" for
// "pool/a/b/c" if "pool/a/b" does not exist, or nil if there is none.
func nearestAncestor(name string, byName map[string]*zfs.DatasetListEntry) *zfs.DatasetListEntry {
	for {
		index := strings.LastIndex(name, "/")
		if index < 0 {
			return nil
		}
		name = name[:index]
		if ancestor, ok := byName[name]; ok {
			return ancestor
		}
	}
}
