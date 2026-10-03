package dataset_browser

import (
	"sort"
	"strings"
	"testing"
	"time"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/theme"
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
	ordered, rows := buildDatasetTree(sorted, nil)
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
		testutil.OnUiThread(t, app, func() {
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
		testutil.OnUiThread(t, app, func() {
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

	testutil.OnUiThread(t, app, func() {
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
	testutil.OnUiThread(t, app, func() { browser.tableContainer.SelectHeader() })
	pressKey(tcell.KeyEnter, 0)
	waitForOrder("flat, descending", []string{"zpool", "rpool/var/log", "rpool/var", "rpool/home", "rpool"})
	pressKey(tcell.KeyRune, 't')
	waitForOrder("tree, descending", []string{"zpool", "rpool", "rpool/var", "rpool/var/log", "rpool/home"})

	// 't' typed into a filter does not toggle
	pressKey(tcell.KeyCtrlF, 0)
	pressKey(tcell.KeyRune, 't')
	require.Eventually(t, func() bool {
		filter := ""
		testutil.OnUiThread(t, app, func() { filter = browser.tableContainer.GetFilterText() })
		return filter == "t"
	}, 2*time.Second, 10*time.Millisecond)
	_, _, tree, _ = state()
	assert.True(t, tree)
}

func TestBuildDatasetTree_Collapsed(t *testing.T) {
	sorted := datasets("rpool", "rpool/arch", "rpool/arch/DATA", "rpool/arch/DATA/home", "rpool/arch/ROOT", "rpool/data")

	ordered, rows := buildDatasetTree(sorted, map[string]bool{"rpool/arch": true, "rpool/data": true})

	// the descendants of collapsed datasets are left out
	assert.Equal(t, []string{"rpool", "rpool/arch", "rpool/data"}, names(ordered))
	arch := sorted[1]
	assert.True(t, rows[arch].collapsed)
	assert.Equal(t, 3, rows[arch].hiddenCount, "all descendants are counted")
	assert.Len(t, rows[arch].children, 2, "children are known, also if collapsed")
	assert.Equal(t, "└─ data", rows[sorted[5]].prefix+rows[sorted[5]].name)
	// a dataset without children cannot be collapsed
	assert.False(t, rows[sorted[5]].collapsed)
	// parents
	assert.Nil(t, rows[sorted[0]].parent)
	assert.Same(t, sorted[0], rows[arch].parent)
}

func TestDatasetBrowser_CollapseAndExpand(t *testing.T) {
	setListDatasets(t, func() ([]*zfs.DatasetListEntry, error) {
		return datasets("rpool", "rpool/home", "rpool/var", "rpool/var/lib", "rpool/var/log"), nil
	})

	app, browser, screen := newBrowserApp(t)
	pressKey := func(key tcell.Key) {
		screen.InjectKey(key, 0, tcell.ModNone)
	}
	typeRune := func(r rune) {
		screen.InjectKey(tcell.KeyRune, r, tcell.ModNone)
	}
	state := func() (visible []string, selected string, footer string) {
		testutil.OnUiThread(t, app, func() {
			visible = names(browser.tableContainer.GetEntries())
			if entry := browser.tableContainer.GetSelectedEntry(); entry != nil {
				selected = entry.Name
			}
			footer = browser.tableContainer.GetFooter()
		})
		return
	}
	waitFor := func(message string, condition func(visible []string, selected string) bool) {
		require.Eventually(t, func() bool {
			visible, selected, _ := state()
			return condition(visible, selected)
		}, 2*time.Second, 10*time.Millisecond, message)
	}
	selectedIs := func(name string) func([]string, string) bool {
		return func(_ []string, selected string) bool { return selected == name }
	}
	screenContains := func(text string) bool {
		var screenText strings.Builder
		testutil.OnUiThread(t, app, func() {
			cells, width, height := screen.GetContents()
			for y := 0; y < height; y++ {
				for x := 0; x < width; x++ {
					if runes := cells[y*width+x].Runes; len(runes) > 0 {
						screenText.WriteRune(runes[0])
					}
				}
				screenText.WriteRune('\n')
			}
		})
		return strings.Contains(screenText.String(), text)
	}

	testutil.OnUiThread(t, app, func() {
		browser.SetPath("/rpool/var", false)
		browser.Refresh(false)
	})
	waitFor("loaded", func(visible []string, selected string) bool { return len(visible) == 5 && selected == "rpool/var" })
	_, _, footer := state()

	// - collapses an expanded dataset: its children are hidden, the count is shown, the footer stays the same
	typeRune('-')
	waitFor("collapsed", func(visible []string, selected string) bool {
		return assert.ObjectsAreEqual([]string{"rpool", "rpool/home", "rpool/var"}, visible) && selected == "rpool/var"
	})
	require.Eventually(t, func() bool { return screenContains("var ▸ +2") }, 2*time.Second, 10*time.Millisecond)
	_, _, collapsedFooter := state()
	assert.Equal(t, footer, collapsedFooter)

	// - on a collapsed dataset selects its parent
	typeRune('-')
	waitFor("parent selected", selectedIs("rpool"))

	// on rows that are not selected, the indicator is drawn in its own (readable) color,
	// not in the dark color of the tree lines (selected rows use the selection colors)
	require.Eventually(t, func() bool {
		var foreground tcell.Color
		testutil.OnUiThread(t, app, func() {
			cells, width, height := screen.GetContents()
			for i := 0; i < width*height; i++ {
				if runes := cells[i].Runes; len(runes) > 0 && runes[0] == '▸' {
					foreground, _, _ = cells[i].Style.Decompose()
				}
			}
		})
		return foreground == theme.Colors.Layout.Table.TreeCollapsedIndicator
	}, 2*time.Second, 10*time.Millisecond)

	// + on an expanded dataset selects its first child
	typeRune('+')
	waitFor("first child selected", selectedIs("rpool/home"))

	// + on a leaf does nothing
	typeRune('+')
	time.Sleep(50 * time.Millisecond)
	_, selected, _ := state()
	assert.Equal(t, "rpool/home", selected)

	// the collapsed state is kept when switching to the flat list and back, and on reload
	screen.InjectKey(tcell.KeyRune, 't', tcell.ModNone)
	waitFor("flat list shows everything", func(visible []string, _ string) bool { return len(visible) == 5 })
	screen.InjectKey(tcell.KeyRune, 't', tcell.ModNone)
	waitFor("tree is collapsed again", func(visible []string, _ string) bool { return len(visible) == 3 })
	testutil.OnUiThread(t, app, func() { browser.Refresh(false) })
	time.Sleep(100 * time.Millisecond)
	waitFor("still collapsed after reload", func(visible []string, _ string) bool { return len(visible) == 3 })

	// + on a collapsed dataset expands it, then + selects its first child, - on a leaf selects the parent
	pressKey(tcell.KeyDown)
	waitFor("var selected", selectedIs("rpool/var"))
	typeRune('+')
	waitFor("expanded", func(visible []string, selected string) bool { return len(visible) == 5 && selected == "rpool/var" })
	typeRune('+')
	waitFor("first child selected", selectedIs("rpool/var/lib"))
	typeRune('-')
	waitFor("parent selected", selectedIs("rpool/var"))
	assert.False(t, screenContains("▸"))

	// ← and → on data rows are left to the table (horizontal scrolling), they don't change the tree
	pressKey(tcell.KeyLeft)
	pressKey(tcell.KeyRight)
	time.Sleep(50 * time.Millisecond)
	visible, selected, _ := state()
	assert.Len(t, visible, 5)
	assert.Equal(t, "rpool/var", selected)

	// * collapses all (the selection moves to the top-most ancestor), * again expands all
	pressKey(tcell.KeyDown)
	waitFor("lib selected", selectedIs("rpool/var/lib"))
	typeRune('*')
	waitFor("all collapsed", func(visible []string, selected string) bool {
		return assert.ObjectsAreEqual([]string{"rpool"}, visible) && selected == "rpool"
	})
	typeRune('*')
	waitFor("all expanded", func(visible []string, _ string) bool { return len(visible) == 5 })

	// on the header row, ← and → still change the sort column
	testutil.OnUiThread(t, app, func() { browser.tableContainer.SelectHeader() })
	pressKey(tcell.KeyRight)
	require.Eventually(t, func() bool { return screenContains("Used ↑") }, 2*time.Second, 10*time.Millisecond)
}
