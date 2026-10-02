package dialog

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type textInputDialogTest struct {
	t      *testing.T
	app    *tview.Application
	screen tcell.SimulationScreen
	pages  *tview.Pages
	dialog *TextInputDialog

	mu        sync.Mutex
	submitted []string
}

func newTextInputDialogTest(t *testing.T, initialText string) *textInputDialogTest {
	dt := &textInputDialogTest{t: t}
	dt.app = tview.NewApplication()
	dt.screen = tcell.NewSimulationScreen("UTF-8")
	dt.app.SetScreen(dt.screen)
	dt.pages = tview.NewPages().AddPage("main", tview.NewBox(), true, true)
	dt.app.SetRoot(dt.pages, true)
	go func() { _ = dt.app.Run() }()
	t.Cleanup(dt.app.Stop)

	dt.dialog = NewTextInputDialog(dt.app, "TextInputTest", " Title ", "Name of the thing:", initialText, func(text string) {
		dt.mu.Lock()
		defer dt.mu.Unlock()
		dt.submitted = append(dt.submitted, text)
	})
	onUiThread(t, dt.app, func() { ShowDialogOnPages(dt.app, dt.pages, dt.dialog, nil) })
	return dt
}

func (dt *textInputDialogTest) press(key tcell.Key, r rune) {
	dt.screen.InjectKey(key, r, tcell.ModNone)
}

func (dt *textInputDialogTest) typeText(text string) {
	for _, r := range text {
		dt.press(tcell.KeyRune, r)
	}
}

func (dt *textInputDialogTest) text() (text string) {
	onUiThread(dt.t, dt.app, func() { text = dt.dialog.GetText() })
	return text
}

func (dt *textInputDialogTest) isShown() (shown bool) {
	onUiThread(dt.t, dt.app, func() { shown = dt.pages.HasPage("TextInputTest") })
	return shown
}

func (dt *textInputDialogTest) getSubmitted() []string {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	return append([]string{}, dt.submitted...)
}

func TestTextInputDialog_EditAndSubmit(t *testing.T) {
	dt := newTextInputDialogTest(t, "pool/data-daily")
	assert.Equal(t, "pool/data-daily", dt.text())

	// edit like a terminal input line: delete the last word, append text
	dt.press(tcell.KeyCtrlW, 0)
	dt.typeText("clone  ")
	require.Eventually(t, func() bool { return dt.text() == "pool/data-clone  " }, 2*time.Second, 10*time.Millisecond)

	dt.press(tcell.KeyEnter, 0)
	require.Eventually(t, func() bool { return len(dt.getSubmitted()) == 1 }, 2*time.Second, 10*time.Millisecond)
	// trimmed
	assert.Equal(t, []string{"pool/data-clone"}, dt.getSubmitted())
	require.Eventually(t, func() bool { return !dt.isShown() }, 2*time.Second, 10*time.Millisecond)
}

func TestTextInputDialog_EmptyTextIsNotSubmitted(t *testing.T) {
	dt := newTextInputDialogTest(t, "x")
	dt.press(tcell.KeyCtrlU, 0)
	dt.typeText("   ")
	require.Eventually(t, func() bool { return dt.text() == "   " }, 2*time.Second, 10*time.Millisecond)

	dt.press(tcell.KeyEnter, 0)
	time.Sleep(100 * time.Millisecond)
	assert.Empty(t, dt.getSubmitted())
	assert.True(t, dt.isShown(), "the dialog stays open")
}

func TestTextInputDialog_EscCancels(t *testing.T) {
	dt := newTextInputDialogTest(t, "pool/data-daily")
	dt.press(tcell.KeyEscape, 0)

	require.Eventually(t, func() bool { return !dt.isShown() }, 2*time.Second, 10*time.Millisecond)
	assert.Empty(t, dt.getSubmitted())
}

func TestTextInputDialog_Validator(t *testing.T) {
	dt := newTextInputDialogTest(t, "")
	onUiThread(t, dt.app, func() {
		dt.dialog.SetValidator(func(text string) error {
			if !strings.Contains(text, ":") {
				return errors.New("needs a colon")
			}
			return nil
		})
	})
	errorText := func() string {
		text := ""
		onUiThread(t, dt.app, func() { text = dt.dialog.errorView.GetText(true) })
		return text
	}

	dt.typeText("note")
	dt.press(tcell.KeyEnter, 0)
	require.Eventually(t, func() bool { return errorText() == "needs a colon" }, 2*time.Second, 10*time.Millisecond)
	assert.Empty(t, dt.getSubmitted())
	assert.True(t, dt.isShown(), "the dialog stays open to correct the text")

	// editing clears the error
	dt.press(tcell.KeyCtrlU, 0)
	require.Eventually(t, func() bool { return errorText() == "" }, 2*time.Second, 10*time.Millisecond)

	dt.typeText("org:note")
	dt.press(tcell.KeyEnter, 0)
	require.Eventually(t, func() bool { return len(dt.getSubmitted()) == 1 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, []string{"org:note"}, dt.getSubmitted())
}
