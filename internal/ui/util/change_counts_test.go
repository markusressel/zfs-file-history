package util

import (
	"testing"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"

	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

func TestFormatChangeCounts(t *testing.T) {
	plain := func(text string) string {
		return tview.NewTextView().SetDynamicColors(true).SetText(text).GetText(true)
	}
	assert.Equal(t, "+3 −1 ~2", plain(FormatChangeCounts(3, 1, 2)))
	assert.Equal(t, "+3 ~2", plain(FormatChangeCounts(3, 0, 2)), "zeros are left out")
	assert.Equal(t, "", FormatChangeCounts(0, 0, 0))
	assert.Contains(t, FormatChangeCounts(0, 1, 0), txwidgets.ColorTag(theme.Colors.FileBrowser.Table.State.Deleted))
}
