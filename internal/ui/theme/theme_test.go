package theme

import (
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/stretchr/testify/assert"
)

func TestHeaderBackgroundIsDistinct(t *testing.T) {
	table := Colors.Layout.Table
	assert.NotEqual(t, table.HeaderBackground, Colors.FileBrowser.Table.State.Equal, "unchanged file color")
	assert.NotEqual(t, table.HeaderBackground, Colors.SnapshotBrowser.Table.State.Equal, "unchanged snapshot color")
	assert.NotEqual(t, table.HeaderBackground, table.MultiSelectionBackground)
	assert.NotEqual(t, table.HeaderBackground, table.SelectedBackground)
	assert.NotEqual(t, table.HeaderBackground, tcell.ColorBlack)
}
