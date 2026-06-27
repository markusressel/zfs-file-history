package dialog

import (
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

func TestCreateModal(t *testing.T) {
	content := tview.NewBox()
	dialog := createModal("Test Modal", content, DialogSizeConstraints{
		Title:        "Test Modal",
		StaticHeight: 15,
	})
	assert.NotNil(t, dialog)
}

func TestCreateModal_Clamping(t *testing.T) {
	content := tview.NewBox()
	dialog := createModal("Test Modal", content, DialogSizeConstraints{
		Title:             "Test Modal",
		ExtraContentWidth: 60,
		StaticHeight:      10,
	})

	screen := tcell.NewSimulationScreen("UTF-8")
	err := screen.Init()
	assert.NoError(t, err)
	defer screen.Fini()

	screen.SetSize(80, 24)

	// Set rect simulating it's mounted on an offset sub-view at x=60, width=20
	dialog.SetRect(60, 5, 20, 10)

	dialog.Draw(screen)

	x, _, w, _ := dialog.GetRect()

	// Expected dialog width: maxContentWidth (60) + 6 = 66
	// Centering relative to offset sub-view: 60 + (20-66)/2 = 37.
	// Since 37 + 66 = 103 > 80 (screenWidth), it must clamp to screenWidth - w = 14!
	assert.Equal(t, 14, x)
	assert.Equal(t, 66, w)
}

func TestShowDialogOnPages(t *testing.T) {
	app := tview.NewApplication()
	pages := tview.NewPages()
	options := []*DialogOption{
		{Id: DialogCloseActionId, Name: "Cancel"},
	}
	d := NewSelectionDialog(app, "test-dialog", "Title", "Desc", options, nil, nil)

	onClosedCalled := false
	onClosed := func() {
		onClosedCalled = true
	}

	ShowDialogOnPages(app, pages, d, onClosed)

	assert.True(t, pages.HasPage("test-dialog"))

	d.Close()

	time.Sleep(50 * time.Millisecond)
	assert.True(t, onClosedCalled)
}
