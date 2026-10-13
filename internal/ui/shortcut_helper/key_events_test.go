package shortcut_helper

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/stretchr/testify/assert"
)

func TestKeyEvent(t *testing.T) {
	tests := []struct {
		name      string
		key       tcell.Key
		rune      rune
		modifiers tcell.ModMask
	}{
		{"h", tcell.KeyRune, 'h', tcell.ModNone},
		{"?", tcell.KeyRune, '?', tcell.ModNone},
		{KeySpace, tcell.KeyRune, ' ', tcell.ModNone},
		{KeyEnter, tcell.KeyEnter, 0, tcell.ModNone},
		{KeyEsc, tcell.KeyEsc, 0, tcell.ModNone},
		{KeyTab, tcell.KeyTab, 0, tcell.ModNone},
		{KeyPgDn, tcell.KeyPgDn, 0, tcell.ModNone},
		{"↑", tcell.KeyUp, 0, tcell.ModNone},
		{"F2", tcell.KeyF2, 0, tcell.ModNone},
		{"F12", tcell.KeyF12, 0, tcell.ModNone},
		// like terminals report them
		{Ctrl("q"), tcell.KeyCtrlQ, 'q', tcell.ModCtrl},
		{Shift(KeyTab), tcell.KeyBacktab, 0, tcell.ModShift},
		{Shift("↓"), tcell.KeyDown, 0, tcell.ModShift},
		{Alt("←"), tcell.KeyLeft, 0, tcell.ModAlt},
		// the plus key, not a modifier
		{"+", tcell.KeyRune, '+', tcell.ModNone},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event, ok := keyEvent(test.name)
			if !assert.True(t, ok) {
				return
			}
			assert.Equal(t, test.key, event.Key())
			if test.rune != 0 {
				assert.Equal(t, test.rune, event.Rune())
			}
			assert.Equal(t, test.modifiers, event.Modifiers())
		})
	}

	for _, invalid := range []string{"", "F13", "Return", "ctrl+"} {
		_, ok := keyEvent(invalid)
		assert.False(t, ok, invalid)
	}
}
