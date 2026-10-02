package shortcut_helper

// Key names for ShortcutEntry.KeyCombo, so all shortcuts look the same:
//   - characters as typed, so case matters: "h" is the plain key, "H" is shift+h
//   - named keys capitalized: KeyEnter, KeyEsc, ... and F-keys like "F2"
//   - modifiers lowercase with "+": Ctrl("f") is "ctrl+f", Shift(KeyTab) is "shift+⭾"
//
// TestShortcutStyle checks the key names in the code.
const (
	KeyEnter  = "Enter"
	KeyEsc    = "Esc"
	KeySpace  = "Space"
	KeyDelete = "Delete"
	KeyPgUp   = "PgUp"
	KeyPgDn   = "PgDn"
	KeyTab    = "⭾"
)

// Alt returns the name of the key with alt, e.g. "alt+f".
func Alt(key string) string {
	return "alt+" + key
}

// Ctrl returns the name of the key with ctrl, e.g. "ctrl+f".
func Ctrl(key string) string {
	return "ctrl+" + key
}

// Shift returns the name of the key with shift, e.g. "shift+⭾". For characters, use the shifted character instead,
// e.g. "H" instead of shift+h.
func Shift(key string) string {
	return "shift+" + key
}

var (
	// ShortcutTimeFormat switches times in tables between absolute and relative (see uiutil.ToggleRelativeTimes).
	ShortcutTimeFormat = ShortcutEntry{KeyCombo: []string{"T"}, Name: "Time format", Group: GroupView}
	// ShortcutHide hides the shortcuts (see ToggleShortcuts).
	ShortcutHide = ShortcutEntry{KeyCombo: []string{"?"}, Name: "Hide shortcuts", Group: GroupGlobal}
)
