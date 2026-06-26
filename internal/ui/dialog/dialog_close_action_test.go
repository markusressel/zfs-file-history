package dialog

import (
	"errors"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

// setupTestApp creates a headless tview application for testing QueueUpdateDraw
func setupTestApp() (*tview.Application, func()) {
	app := tview.NewApplication()
	simScreen := tcell.NewSimulationScreen("")
	simScreen.Init()
	app.SetScreen(simScreen)

	go app.Run()

	return app, func() {
		app.Stop()
	}
}

func TestSelectionDialog_SelectCloseOption_EmitsCloseAction(t *testing.T) {
	app := tview.NewApplication() // We don't need the event loop just for closing

	d := NewSelectionDialog(
		app,
		"TestDialog",
		"Title",
		"Description",
		[]*DialogOption{{Id: DialogCloseActionId, Name: "Close"}},
		nil,
		nil,
	)

	d.selectAction(&DialogOption{Id: DialogCloseActionId, Name: "Close"})

	assertDialogActionEmitted(t, d.actionChannel, DialogCloseActionId)
}

func TestSelectionDialog_SelectOtherOption_ExecutesHandlerAndOnComplete(t *testing.T) {
	app, cleanup := setupTestApp()
	defer cleanup()

	handlerCalled := make(chan bool, 1)
	onCompleteCalled := make(chan bool, 1)

	testOption := &DialogOption{Id: DialogActionId(42), Name: "Action"}

	handler := func(dialog *SelectionDialog, action DialogActionId) error {
		assert.Equal(t, DialogActionId(42), action)
		handlerCalled <- true
		return nil // Success
	}

	onComplete := func(dialog *SelectionDialog, option *DialogOption, err error) {
		assert.Equal(t, testOption, option)
		assert.NoError(t, err)
		onCompleteCalled <- true
	}

	d := NewSelectionDialog(
		app,
		"TestDialog",
		"Title",
		"Description",
		[]*DialogOption{testOption},
		handler,
		onComplete,
	)

	d.selectAction(testOption)

	// Verify the background handler was triggered
	select {
	case <-handlerCalled:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatalf("expected handler to be called")
	}

	// Verify the UI update queue executed the onComplete callback
	select {
	case <-onCompleteCalled:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatalf("expected onComplete to be called")
	}
}

func TestSelectionDialog_SelectOtherOption_PropagatesErrorToOnComplete(t *testing.T) {
	app, cleanup := setupTestApp()
	defer cleanup()

	handlerCalled := make(chan bool, 1)
	onCompleteCalled := make(chan bool, 1)
	expectedErr := errors.New("simulated background task failure")

	testOption := &DialogOption{Id: DialogActionId(99), Name: "Failing Action"}

	handler := func(dialog *SelectionDialog, action DialogActionId) error {
		handlerCalled <- true
		return expectedErr // Inject failure
	}

	onComplete := func(dialog *SelectionDialog, option *DialogOption, err error) {
		assert.ErrorIs(t, err, expectedErr, "expected the injected error to propagate to onComplete")
		onCompleteCalled <- true
	}

	d := NewSelectionDialog(
		app,
		"TestDialog",
		"Title",
		"Description",
		[]*DialogOption{testOption},
		handler,
		onComplete,
	)

	d.selectAction(testOption)

	<-handlerCalled

	// Verify the error made it back to the UI thread
	select {
	case <-onCompleteCalled:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatalf("expected onComplete to be called with error")
	}
}

func assertDialogActionEmitted(t *testing.T, ch <-chan DialogActionId, expected DialogActionId) {
	t.Helper()
	select {
	case action := <-ch:
		assert.Equal(t, expected, action)
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("expected action %v to be emitted", expected)
	}
}
