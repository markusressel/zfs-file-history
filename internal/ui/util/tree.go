package util

// The lines of the tree views (the datasets, the devices of a pool, the groups of the history of a pool), in front of
// the name of an entry: its branch, and for each level above it a line if a sibling of that level follows, else a
// space.
const (
	TreeBranch     = "├─ "
	TreeLastBranch = "└─ "
	TreeLine       = "│  "
	TreeSpace      = "   "
)
