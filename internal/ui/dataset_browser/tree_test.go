package dataset_browser

import (
	"sort"
	"strings"
	"testing"
	"time"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func datasets(names ...string) []*zfs.DatasetListEntry {
	var result []*zfs.DatasetListEntry
	for _, name := range names {
		result = append(result, &zfs.DatasetListEntry{Name: name, MountPath: "/" + name})
	}
	return result
}

// renderTree returns the tree as text lines, like the name column shows it.
func renderTree(sorted []*zfs.DatasetListEntry) []string {
	ordered, rows := buildDatasetTree(sorted)
	var lines []string
	for _, entry := range ordered {
		lines = append(lines, rows[entry].prefix+rows[entry].name)
	}
	return lines
}

func TestBuildDatasetTree(t *testing.T) {
	// already sorted, e.g. by name
	sorted := datasets(
		"bpool",
		"bpool/BOOT",
		"rpool",
		"rpool/arch",
		"rpool/arch/DATA",
		"rpool/arch/DATA/home",
		"rpool/arch/DATA/var",
		"rpool/arch/ROOT",
		"rpool/data",
	)

	assert.Equal(t, []string{
		"bpool",
		"└─ BOOT",
		"rpool",
		"├─ arch",
		"│  ├─ DATA",
		"│  │  ├─ home",
		"│  │  └─ var",
		"│  └─ ROOT",
		"└─ data",
	}, renderTree(sorted))
}

func TestBuildDatasetTree_KeepsTheSiblingOrder(t *testing.T) {
	// sorted by size, descending: siblings keep that order, parents still come before their children
	sorted := datasets("rpool/small", "rpool", "rpool/big/child", "rpool/big")
	assert.Equal(t, []string{
		"rpool",
		"├─ small",
		"└─ big",
		"   └─ child",
	}, renderTree(sorted))
}

func TestBuildDatasetTree_HiddenParents(t *testing.T) {
	// e.g. unmounted parents are hidden: datasets hang below their nearest displayed ancestor, with a relative name,
	// or are roots with their full name
	sorted := datasets(
		"rpool/arch/DATA/default/home",
		"rpool/arch/DATA/default/var/lib/docker",
		"rpool/arch/DATA/default/var/lib/docker/layer1",
		"rpool/arch/DATA/default/var/log",
		"rpool/arch/ROOT/default",
	)
	assert.Equal(t, []string{
		"rpool/arch/DATA/default/home",
		"rpool/arch/DATA/default/var/lib/docker",
		"└─ layer1",
		"rpool/arch/DATA/default/var/log",
		"rpool/arch/ROOT/default",
	}, renderTree(sorted))

	sorted = datasets("rpool", "rpool/arch/DATA/default/home", "rpool/data")
	assert.Equal(t, []string{
		"rpool",
		"├─ arch/DATA/default/home",
		"└─ data",
	}, renderTree(sorted))
}

func TestBuildDatasetTree_SimilarNames(t *testing.T) {
	// "rpool/data2" is not a child of "rpool/data"
	sorted := datasets("rpool/data", "rpool/data2", "rpool/data/x")
	assert.Equal(t, []string{
		"rpool/data",
		"└─ x",
		"rpool/data2",
	}, renderTree(sorted))
}

func TestCompareDatasetPaths(t *testing.T) {
	sorted := []string{"rpool-x", "rpool/test/b", "Rpool/Test", "rpool", "rpool/test-2", "a", "rpool/test/a"}
	sort.SliceStable(sorted, func(i, j int) bool { return compareDatasetPaths(sorted[i], sorted[j]) < 0 })
	// parents directly before their children, case-insensitive, '-' does not split a parent from its children
	assert.Equal(t, []string{"a", "rpool", "Rpool/Test", "rpool/test/a", "rpool/test/b", "rpool/test-2", "rpool-x"}, sorted)

	assert.Equal(t, 0, compareDatasetPaths("rpool/Home", "RPOOL/home"))
	assert.Equal(t, -1, compareDatasetPaths("/var", "/var/lib"))
	assert.Equal(t, -1, compareDatasetPaths("/var/lib", "/var-x"))
}

func TestDatasetBrowser_TreeViewIsTheDefaultAndCanBeToggled(t *testing.T) {
	setListDatasets(t, func() ([]*zfs.DatasetListEntry, error) {
		return datasets("rpool", "rpool/home", "rpool/var", "rpool/var/log", "zpool"), nil
	})

	app, browser, screen := newBrowserApp(t)
	pressKey := func(key tcell.Key, r rune) {
		screen.InjectKey(key, r, tcell.ModNone)
	}
	state := func() (visible []string, selected string, tree bool, shortcut string) {
		onUiThread(t, app, func() {
			visible = names(browser.tableContainer.GetEntries())
			if entry := browser.tableContainer.GetSelectedEntry(); entry != nil {
				selected = entry.Name
			}
			tree = browser.IsTreeView()
			for _, entry := range browser.GetShortcutMap() {
				if len(entry.KeyCombo) == 1 && entry.KeyCombo[0] == "t" {
					shortcut = entry.Name
				}
			}
		})
		return
	}
	waitForOrder := func(message string, expected []string) {
		require.Eventually(t, func() bool {
			visible, _, _, _ := state()
			return assert.ObjectsAreEqual(expected, visible)
		}, 2*time.Second, 10*time.Millisecond, message)
	}
	screenText := func() string {
		var text strings.Builder
		onUiThread(t, app, func() {
			cells, width, height := screen.GetContents()
			for y := 0; y < height; y++ {
				for x := 0; x < width; x++ {
					if runes := cells[y*width+x].Runes; len(runes) > 0 {
						text.WriteRune(runes[0])
					}
				}
				text.WriteRune('\n')
			}
		})
		return text.String()
	}

	onUiThread(t, app, func() {
		browser.SetPath("/rpool/var/log", false)
		browser.Refresh(false)
	})

	// tree view by default, sorted by name ascending: parents before their children
	waitForOrder("tree, ascending", []string{"rpool", "rpool/home", "rpool/var", "rpool/var/log", "zpool"})
	_, selected, tree, shortcut := state()
	assert.True(t, tree)
	assert.Equal(t, "Flat list", shortcut)
	assert.Equal(t, "rpool/var/log", selected)
	require.Eventually(t, func() bool {
		text := screenText()
		return strings.Contains(text, "├─ home") && strings.Contains(text, "└─ var") && strings.Contains(text, "   └─ log")
	}, 2*time.Second, 10*time.Millisecond, "the name column shows the tree")

	// flat list: same order when sorted by name ascending, but full names
	pressKey(tcell.KeyRune, 't')
	require.Eventually(t, func() bool {
		_, _, tree, _ := state()
		return !tree
	}, 2*time.Second, 10*time.Millisecond)
	waitForOrder("flat, ascending", []string{"rpool", "rpool/home", "rpool/var", "rpool/var/log", "zpool"})
	_, selected, _, shortcut = state()
	assert.Equal(t, "rpool/var/log", selected, "the selection is kept")
	assert.Equal(t, "Tree view", shortcut)
	require.Eventually(t, func() bool {
		text := screenText()
		return strings.Contains(text, "rpool/var/log") && !strings.Contains(text, "└─")
	}, 2*time.Second, 10*time.Millisecond, "the name column shows full names")

	// descending: the flat list reverses the order, the tree keeps parents before their children
	onUiThread(t, app, func() { browser.tableContainer.SelectHeader() })
	pressKey(tcell.KeyEnter, 0)
	waitForOrder("flat, descending", []string{"zpool", "rpool/var/log", "rpool/var", "rpool/home", "rpool"})
	pressKey(tcell.KeyRune, 't')
	waitForOrder("tree, descending", []string{"zpool", "rpool", "rpool/var", "rpool/var/log", "rpool/home"})

	// 't' typed into a filter does not toggle
	pressKey(tcell.KeyCtrlF, 0)
	pressKey(tcell.KeyRune, 't')
	require.Eventually(t, func() bool {
		filter := ""
		onUiThread(t, app, func() { filter = browser.tableContainer.GetFilterText() })
		return filter == "t"
	}, 2*time.Second, 10*time.Millisecond)
	_, _, tree, _ = state()
	assert.True(t, tree)
}
