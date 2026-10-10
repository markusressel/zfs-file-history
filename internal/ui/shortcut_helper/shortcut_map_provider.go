package shortcut_helper

type ShortcutMapProvider interface {
	GetShortcutMap() []ShortcutEntry
}

// CommandProvider provides commands only shown in the command menu, in addition to the runnable entries of its
// shortcut map (see ShortcutEntry.Run). They are only requested when the menu is opened, so they may be costly to
// create, e.g. from the options of an action dialog.
type CommandProvider interface {
	GetCommands() []ShortcutEntry
}
