package dataset_browser

import (
	"strings"
	"unicode/utf8"
	"zfs-file-history/internal/zfs"
)

// treeRow is how a dataset is displayed in the tree view.
type treeRow struct {
	// prefix are the tree lines in front of the name, e.g. "│  ├─ ", with the toggle of a dataset with children in
	// place of its branch, e.g. "│  ├▾ " (see treeExpanded, treeCollapsed)
	prefix string
	// toggle is the cell of the toggle in prefix, which expands or collapses the dataset when clicked; -1 if it has
	// no children
	toggle int
	// name is the name relative to the parent in the tree, or the full name for roots
	name string
	// parent is the parent in the tree, nil for roots
	parent *zfs.DatasetListEntry
	// children are the children in the tree (also if collapsed)
	children []*zfs.DatasetListEntry
	// collapsed is true if the dataset has children, which are hidden
	collapsed bool
	// hiddenCount is the number of datasets hidden in a collapsed dataset (all descendants)
	hiddenCount int
}

const (
	treeBranch     = "├─ "
	treeLastBranch = "└─ "
	treeLine       = "│  "
	treeSpace      = "   "
	// treeExpanded and treeCollapsed replace the line of the branch of a dataset with children ("├▾ "), so the names
	// stay where they are, and stand in front of roots ("▾ tank"). Roots without children get as many spaces, so the
	// names of all roots line up, if any root has children.
	treeExpanded  = "▾"
	treeCollapsed = "▸"
	treeRootSpace = "  "
)

// buildDatasetTree arranges the given (sorted) datasets as a tree: each dataset follows its parent, siblings keep
// their order. The parent of a dataset is its nearest ancestor among the given datasets, so the tree stays correct
// if datasets are hidden (e.g. unmounted ones or by a filter). Datasets without such an ancestor are roots.
// The descendants of datasets whose name is in collapsed are left out.
// It returns the datasets to display in tree order, and how to display each of them.
func buildDatasetTree(sorted []*zfs.DatasetListEntry, collapsed map[string]bool) ([]*zfs.DatasetListEntry, map[*zfs.DatasetListEntry]treeRow) {
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

	var countDescendants func(entry *zfs.DatasetListEntry) int
	countDescendants = func(entry *zfs.DatasetListEntry) int {
		count := 0
		for _, child := range children[entry] {
			count += 1 + countDescendants(child)
		}
		return count
	}

	rootsHaveChildren := false
	for _, root := range roots {
		rootsHaveChildren = rootsHaveChildren || len(children[root]) > 0
	}

	var visit func(entry *zfs.DatasetListEntry, indent string, isLast bool, isRoot bool)
	visit = func(entry *zfs.DatasetListEntry, indent string, isLast bool, isRoot bool) {
		entryChildren := children[entry]
		row := treeRow{
			name:      entry.Name,
			parent:    parents[entry],
			children:  entryChildren,
			collapsed: collapsed[entry.Name] && len(entryChildren) > 0,
			toggle:    -1,
		}
		toggle := treeExpanded
		if row.collapsed {
			toggle = treeCollapsed
		}
		childIndent := ""
		switch {
		case isRoot && len(entryChildren) > 0:
			row.prefix, row.toggle = toggle+" ", 0
		case isRoot && rootsHaveChildren:
			row.prefix = treeRootSpace
		case !isRoot:
			row.name = strings.TrimPrefix(entry.Name, parents[entry].Name+"/")
			branch := treeBranch
			childIndent = indent + treeLine
			if isLast {
				branch = treeLastBranch
				childIndent = indent + treeSpace
			}
			if len(entryChildren) > 0 {
				// "├─ " becomes "├▾ "
				branch = branch[:len(branch)-len("─ ")] + toggle + " "
				row.toggle = utf8.RuneCountInString(indent) + 1
			}
			row.prefix = indent + branch
		}
		if row.collapsed {
			row.hiddenCount = countDescendants(entry)
		}
		rows[entry] = row
		result = append(result, entry)

		if row.collapsed {
			return
		}
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
