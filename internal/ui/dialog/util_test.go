package dialog

import (
	"sync/atomic"
	"testing"
	"time"
	"zfs-file-history/internal/testutil"

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
	screen := tcell.NewSimulationScreen("UTF-8")
	app.SetScreen(screen)
	pages := tview.NewPages().AddPage("main", tview.NewBox(), true, true)
	// set the root before the event loop starts, like CreateUi does
	app.SetRoot(pages, true)
	go func() { _ = app.Run() }()
	defer app.Stop()

	options := []*DialogOption{
		{Id: DialogCloseActionId, Name: "Cancel"},
	}
	d := NewSelectionDialog(app, "test-dialog", "Title", "Desc", options, nil, nil)

	var onClosedCalls atomic.Int32
	onClosed := func() {
		onClosedCalls.Add(1)
	}

	testutil.OnUiThread(t, app, func() {
		ShowDialogOnPages(app, pages, d, onClosed)
		assert.True(t, pages.HasPage("test-dialog"))
		assert.True(t, d.GetLayout().HasFocus())
	})
	// ShowDialogOnPages currently also calls onClosed when mounting the dialog
	callsAfterOpen := onClosedCalls.Load()

	d.Close()

	// closing removes the dialog on the UI thread and calls onClosed
	assert.Eventually(t, func() bool {
		removed := false
		testutil.OnUiThread(t, app, func() { removed = !pages.HasPage("test-dialog") })
		return removed
	}, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, callsAfterOpen+1, onClosedCalls.Load())
	testutil.OnUiThread(t, app, func() {
		assert.False(t, d.GetLayout().HasFocus())
	})
}

// a dialog shown from another one returns the focus to where the other one was shown from, also when the other one
// is closed first, e.g. the result of an action shown while the dialog of the action is closing
func TestShowDialogOnPages_DialogShownFromDialog_RestoresOriginalFocus(t *testing.T) {
	app := tview.NewApplication()
	screen := tcell.NewSimulationScreen("UTF-8")
	app.SetScreen(screen)
	first := tview.NewTable()
	second := tview.NewTable()
	main := tview.NewFlex().AddItem(first, 0, 1, true).AddItem(second, 0, 1, false)
	pages := tview.NewPages().AddPage("main", main, true, true)
	app.SetRoot(pages, true)
	go func() { _ = app.Run() }()
	defer app.Stop()

	options := []*DialogOption{
		{Id: DialogCloseActionId, Name: "OK"},
	}
	action := NewSelectionDialog(app, "action-dialog", "Action", "Desc", options, nil, nil)
	result := NewSelectionDialog(app, "result-dialog", "Result", "Desc", options, nil, nil)

	testutil.OnUiThread(t, app, func() {
		app.SetFocus(second)
		ShowDialogOnPages(app, pages, action, nil)
		ShowDialogOnPages(app, pages, result, nil)
	})

	action.Close()
	assert.Eventually(t, func() bool {
		removed := false
		testutil.OnUiThread(t, app, func() { removed = !pages.HasPage("action-dialog") })
		return removed
	}, 2*time.Second, 10*time.Millisecond)

	result.Close()
	assert.Eventually(t, func() bool {
		removed := false
		testutil.OnUiThread(t, app, func() { removed = !pages.HasPage("result-dialog") })
		return removed
	}, 2*time.Second, 10*time.Millisecond)

	testutil.OnUiThread(t, app, func() {
		assert.Same(t, second, app.GetFocus())
	})
}

// components that embed a Flex or Pages, e.g. util.ResizableSplit and util.LoadingContainer
type embeddingFlex struct{ *tview.Flex }
type embeddingPages struct{ *tview.Pages }

func TestIsDescendantOf_SearchesComponentsEmbeddingContainers(t *testing.T) {
	table := tview.NewTable()
	loadingContainer := embeddingPages{tview.NewPages().AddPage("content", table, true, false)}
	split := embeddingFlex{tview.NewFlex().AddItem(tview.NewBox(), 0, 1, false).AddItem(loadingContainer, 0, 1, false)}
	pages := tview.NewPages().AddPage("main", tview.NewFlex().AddItem(split, 0, 1, true), true, true)

	assert.True(t, isDescendantOf(pages, table))
	assert.False(t, isDescendantOf(pages, tview.NewTable()))
}
