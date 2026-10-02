package shortcut_helper

import (
	"weak"
	"zfs-file-history/internal/state"
)

// toggleHideShortcuts is the key of the setting in state.Current.
const toggleHideShortcuts = "ui.hideShortcuts"

var (
	// shortcutsHidden is whether collapsible shortcut maps are hidden, to make room in small terminals.
	// Only accessed on the UI thread.
	shortcutsHidden bool
	// collapsibleMaps are the shortcut maps that are hidden with the setting. Weak, so the maps of closed overlays
	// are not kept alive. Only accessed on the UI thread.
	collapsibleMaps []weak.Pointer[ShortcutMapComponent]
)

// InitShortcutVisibility loads the setting from state.Current. Called when the UI is created.
func InitShortcutVisibility() {
	shortcutsHidden = state.Current.Toggle(toggleHideShortcuts, false)
}

// ShortcutsHidden returns whether collapsible shortcut maps are hidden.
func ShortcutsHidden() bool {
	return shortcutsHidden
}

// ToggleShortcuts hides or shows all collapsible shortcut maps, and remembers the setting.
// Must be called on the UI thread.
func ToggleShortcuts() {
	shortcutsHidden = !shortcutsHidden
	state.Current.SetToggle(toggleHideShortcuts, shortcutsHidden)

	alive := collapsibleMaps[:0]
	for _, pointer := range collapsibleMaps {
		if shortcutMap := pointer.Value(); shortcutMap != nil {
			shortcutMap.applyVisibility()
			alive = append(alive, pointer)
		}
	}
	collapsibleMaps = alive
}

// SetCollapsible makes the map follow the setting of ToggleShortcuts: hidden, it has no text and a height of 0
// (reported to the callback of SetOnHeightChanged, so its container can resize it).
// Only for maps of pages and overlays: dialogs are sized including their shortcuts. Must be called on the UI thread.
func (sm *ShortcutMapComponent) SetCollapsible() *ShortcutMapComponent {
	if !sm.collapsible {
		sm.collapsible = true
		collapsibleMaps = append(collapsibleMaps, weak.Make(sm))
	}
	return sm
}

// isHidden returns whether the map is hidden by the setting.
func (sm *ShortcutMapComponent) isHidden() bool {
	return sm.collapsible && shortcutsHidden
}

// applyVisibility shows the entries again or hides them, after the setting changed.
func (sm *ShortcutMapComponent) applyVisibility() {
	sm.SetEntries(sm.ShortCutEntries)
}
