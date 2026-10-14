package util

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

func TestWithoutFocus(t *testing.T) {
	textView := tview.NewTextView()
	textView.SetRect(0, 0, 10, 5)
	focused := false
	consumed, _ := WithoutFocus(textView.MouseHandler())(tview.MouseLeftDown,
		tcell.NewEventMouse(1, 1, tcell.Button1, tcell.ModNone), func(tview.Primitive) { focused = true })
	assert.True(t, consumed, "handled as before, e.g. not passed on to the panes behind")
	assert.False(t, focused)
}
