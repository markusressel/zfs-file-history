package util

import (
	"testing"
	"zfs-file-history/internal/ui/theme"

	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

func TestSetupWindow(t *testing.T) {
	box := tview.NewBox()
	box.SetBorder(true)
	SetupWindow(box, "My Window Title")

	assert.Equal(t, theme.CreateTitleText("My Window Title"), box.GetTitle())
	assert.Equal(t, theme.Colors.Layout.Border, box.GetBorderColor())
}

func TestSetupDialogWindow(t *testing.T) {
	box := tview.NewBox()
	box.SetBorder(true)
	SetupDialogWindow(box, "My Dialog Title")

	assert.Equal(t, theme.CreateTitleText("My Dialog Title"), box.GetTitle())
	assert.Equal(t, theme.Colors.Dialog.Border, box.GetBorderColor())
}
