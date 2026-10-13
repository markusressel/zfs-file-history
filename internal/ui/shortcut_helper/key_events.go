package shortcut_helper

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
)

// keysByName are the keys of the key names that are not characters.
var keysByName = map[string]tcell.Key{
	KeyEnter:  tcell.KeyEnter,
	KeyEsc:    tcell.KeyEsc,
	KeyDelete: tcell.KeyDelete,
	KeyPgUp:   tcell.KeyPgUp,
	KeyPgDn:   tcell.KeyPgDn,
	KeyTab:    tcell.KeyTab,
	"↑":       tcell.KeyUp,
	"↓":       tcell.KeyDown,
	"←":       tcell.KeyLeft,
	"→":       tcell.KeyRight,
}

// keyEvent returns the event of pressing the key with the given name (see the key names, e.g. "ctrl+f" or "F2"),
// like a terminal reports it. false if the name is not a key.
func keyEvent(name string) (*tcell.EventKey, bool) {
	modifiers := tcell.ModNone
	for _, prefix := range []struct {
		name     string
		modifier tcell.ModMask
	}{{"ctrl+", tcell.ModCtrl}, {"alt+", tcell.ModAlt}, {"shift+", tcell.ModShift}} {
		if rest, found := strings.CutPrefix(name, prefix.name); found && rest != "" {
			modifiers |= prefix.modifier
			name = rest
		}
	}

	if key, ok := keysByName[name]; ok {
		if key == tcell.KeyTab && modifiers&tcell.ModShift != 0 {
			key = tcell.KeyBacktab
		}
		return tcell.NewEventKey(key, 0, modifiers), true
	}
	if name == KeySpace {
		return tcell.NewEventKey(tcell.KeyRune, ' ', modifiers), true
	}
	if number, found := strings.CutPrefix(name, "F"); found {
		if n, err := strconv.Atoi(number); err == nil && n >= 1 && n <= 12 {
			return tcell.NewEventKey(tcell.KeyF1+tcell.Key(n-1), 0, modifiers), true
		}
	}
	if utf8.RuneCountInString(name) == 1 {
		r, _ := utf8.DecodeRuneInString(name)
		return tcell.NewEventKey(tcell.KeyRune, r, modifiers), true
	}
	return nil, false
}
