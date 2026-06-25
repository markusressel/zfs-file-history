package dialog

import (
	"testing"
	"time"

	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

func TestCreateModal(t *testing.T) {
	content := tview.NewBox()
	dialog := createModal("Test Modal", content, 50, 15)
	assert.NotNil(t, dialog)
}

func TestShowDialogOnPages(t *testing.T) {
	app := tview.NewApplication()
	pages := tview.NewPages()
	options := []*DialogOption{
		{Id: DialogCloseActionId, Name: "Cancel"},
	}
	d := NewSelectionDialog(app, "test-dialog", "Title", "Desc", options, nil, nil)

	handler := func() {}

	ShowDialogOnPages(app, pages, d, handler)

	assert.True(t, pages.HasPage("test-dialog"))

	d.Close()

	time.Sleep(50 * time.Millisecond)
	assert.True(t, actionHandled)
}
