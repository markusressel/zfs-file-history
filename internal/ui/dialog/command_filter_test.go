package dialog

import (
	"testing"
	"zfs-file-history/internal/ui/shortcut_helper"

	"github.com/stretchr/testify/assert"
)

func matchNames(matches []commandMatch) []string {
	var names []string
	for _, match := range matches {
		names = append(names, match.command.Name)
	}
	return names
}

func TestFilterCommands(t *testing.T) {
	commands := []shortcut_helper.ShortcutEntry{
		{Name: "Restore file"},
		{Name: "Restore directory recursively"},
		{Name: "Show diff"},
		{Name: "Folder history", KeyCombo: []string{"h"}},
		{Name: "Refresh", KeyCombo: []string{"F5"}},
	}

	assert.Equal(t, matchNames(filterCommands(commands, "")), []string{
		"Restore file", "Restore directory recursively", "Show diff", "Folder history", "Refresh",
	}, "all, in their order")

	assert.Equal(t, []string{"Restore directory recursively"}, matchNames(filterCommands(commands, "res rec")),
		"each term has to match")
	assert.Equal(t, []string{"Restore file", "Restore directory recursively"}, matchNames(filterCommands(commands, "RESTORE")),
		"the case is ignored")
	assert.Empty(t, filterCommands(commands, "xyz"))

	// a word start is better than a part of a word, which is better than runes in order
	assert.Equal(t, []string{"Refresh", "Restore file"}, matchNames(filterCommands(commands, "ref")))
	assert.Equal(t, []string{"Restore file", "Restore directory recursively", "Refresh"}, matchNames(filterCommands(commands, "res")))
	assert.Equal(t, []string{"Restore file", "Restore directory recursively"}, matchNames(filterCommands(commands, "tore")))
	assert.Equal(t, []string{"Folder history", "Show diff"}, matchNames(filterCommands(commands, "hi")))
	assert.Equal(t, []string{"Restore file", "Restore directory recursively", "Refresh", "Folder history"},
		matchNames(filterCommands(commands, "r")), "equally good matches keep their order")

	// keys match as well
	assert.Equal(t, []string{"Refresh"}, matchNames(filterCommands(commands, "f5")))

	// and descriptions, weaker than names
	described := []shortcut_helper.ShortcutEntry{
		{Name: "Columns", Description: "Choose, order and reset the shown columns"},
		{Name: "Reset"},
	}
	assert.Equal(t, []string{"Reset", "Columns"}, matchNames(filterCommands(described, "reset")))
}

func TestMatchTerm(t *testing.T) {
	name := []rune("restore file")

	score, positions := matchTerm([]rune("fil"), name)
	assert.Equal(t, scoreWordStart, score)
	assert.Equal(t, []int{8, 9, 10}, positions)

	score, positions = matchTerm([]rune("tore"), name)
	assert.Equal(t, scoreSubstring, score)
	assert.Equal(t, []int{3, 4, 5, 6}, positions)

	score, positions = matchTerm([]rune("rsf"), name)
	assert.Equal(t, scoreSubsequence, score)
	assert.Equal(t, []int{0, 2, 8}, positions)

	score, _ = matchTerm([]rune("fr"), name)
	assert.Equal(t, scoreNoMatch, score, "the runes have to be in order")
}

func TestWithoutIcon(t *testing.T) {
	assert.Equal(t, "Create Snapshot", withoutIcon("📸 Create Snapshot"))
	assert.Equal(t, "Restore file", withoutIcon("♻️ Restore file"))
	assert.Equal(t, "Restore directory only", withoutIcon("📁 Restore directory only"))
	assert.Equal(t, "Close", withoutIcon("Close"))
	assert.Equal(t, "Show diff", withoutIcon("Show diff"))
}
