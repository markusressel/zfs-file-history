package ui

import (
	"testing"
	"zfs-file-history/internal/testutil"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

func TestPageTabs_Click(t *testing.T) {
	var selected []int
	tabs := newPageTabs([]string{"Files", "Datasets", "Pools"}, 1, func(index int) { selected = append(selected, index) })
	enabled := true
	tabs.enabled = func() bool { return enabled }
	screen := testutil.NewScreen(t, 40, 1)
	tabs.SetRect(0, 0, tabs.width(), 1)
	tabs.Draw(screen)
	// " 1  2 DATASETS  3 "
	click := func(x int) {
		event := tcell.NewEventMouse(x, 0, tcell.ButtonNone, tcell.ModNone)
		tabs.MouseHandler()(tview.MouseLeftClick, event, func(tview.Primitive) {})
	}

	click(1)
	click(9)
	click(16)
	assert.Equal(t, []int{0, 2}, selected, "the shown page is not clicked")

	// e.g. a dialog in front
	enabled = false
	click(1)
	assert.Equal(t, []int{0, 2}, selected)
}
