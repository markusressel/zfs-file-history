package ui

import (
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/status_message"
	uiutil "zfs-file-history/internal/ui/util"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// basePage is what the switchable pages have in common: the header with the status messages, the shortcuts of the
// focused component at the bottom, the focus cycle (⭾), and panes whose boundaries can be dragged.
type basePage struct {
	application *tview.Application
	// name is the name of the page in pages
	name   uiutil.Page
	pages  *tview.Pages
	header *ApplicationHeaderComponent
	// messages are the messages of the whole application, shown in the header of each page
	messages *status_message.Center
	// shortcutMap shows the shortcuts of the focused component, see updateShortcutMap
	shortcutMap *shortcut_helper.ShortcutMapComponent
	// focusableComponents returns the components that can be focused, in focus cycle order; the first one is the
	// main component of the page
	focusableComponents func() []FocusableUiComponent
	// pageShortcuts returns the shortcuts of the page itself (nil: none), shown behind the ones of the focused
	// component and before the global ones
	pageShortcuts func() []shortcut_helper.ShortcutEntry
}

func (page *basePage) SetPages(pages *tview.Pages) {
	page.pages = pages
}

// isInFront returns whether the page is shown, not another page or a dialog above it.
func (page *basePage) isInFront() bool {
	if page.pages == nil {
		return true
	}
	front, _ := page.pages.GetFrontPage()
	return front == string(page.name)
}

func (page *basePage) showStatusMessage(status *status_message.StatusMessage) {
	page.messages.Show(status)
}

// updateShortcutMap shows the shortcuts of the component, the page and the global ones.
func (page *basePage) updateShortcutMap(component FocusableUiComponent) {
	page.shortcutMap.SetEntries(page.shortcutsOf(component))
}

// shortcutsOf returns the shortcuts shown while the component has the focus: its own, the page's and the global ones.
func (page *basePage) shortcutsOf(component FocusableUiComponent) []shortcut_helper.ShortcutEntry {
	var shortcuts []shortcut_helper.ShortcutEntry
	if provider, ok := component.(shortcut_helper.ShortcutMapProvider); ok {
		shortcuts = provider.GetShortcutMap()
	}
	if page.pageShortcuts != nil {
		shortcuts = append(shortcuts, page.pageShortcuts()...)
	}
	return append(shortcuts, globalShortcuts()...)
}

// reserveShortcutMapHeight makes the shortcut map as high as the shortcuts of any focusable component need, so
// cycling the focus does not resize the components of the page.
func (page *basePage) reserveShortcutMapHeight() {
	page.shortcutMap.SetAlternativeEntries(func() [][]shortcut_helper.ShortcutEntry {
		var alternatives [][]shortcut_helper.ShortcutEntry
		for _, component := range page.focusableComponents() {
			alternatives = append(alternatives, page.shortcutsOf(component))
		}
		return alternatives
	})
}

// refreshShortcutMap shows the shortcuts of the focused component (or the main one, if none has focus),
// e.g. after the page was switched to.
func (page *basePage) refreshShortcutMap() {
	components := page.focusableComponents()
	for _, component := range components {
		if component.HasFocus() {
			page.updateShortcutMap(component)
			return
		}
	}
	page.updateShortcutMap(components[0])
}

func (page *basePage) CycleFocus(reversed bool) {
	components := page.focusableComponents()

	currentIndex := -1
	for i, component := range components {
		if component.HasFocus() {
			currentIndex = i
			break
		}
	}

	var nextIndex int
	if currentIndex == -1 {
		nextIndex = 0
		logging.Warning("Unexpected focus state")
	} else if reversed {
		nextIndex = (currentIndex - 1 + len(components)) % len(components)
	} else {
		nextIndex = (currentIndex + 1) % len(components)
	}

	nextFocusedComponent := components[nextIndex]
	nextFocusedComponent.Focus()
	page.updateShortcutMap(nextFocusedComponent)
}

// handleFocusKeys cycles the focus with ⭾ / shift+⭾ (ctrl+⭾ is left to others). Returns whether it was handled.
func (page *basePage) handleFocusKeys(event *tcell.EventKey) bool {
	key := event.Key()
	if (key != tcell.KeyTab && key != tcell.KeyBacktab) || event.Modifiers()&tcell.ModCtrl != 0 {
		return false
	}
	page.CycleFocus(key == tcell.KeyBacktab || event.Modifiers()&tcell.ModShift != 0)
	return true
}

// newSplit returns a split of two panes side by side (or one above the other), whose boundary can be dragged
// while the page is in front. Its mouse handling has to be registered with captureSplitDrags.
func (page *basePage) newSplit(first tview.Primitive, second tview.Primitive, firstProportion int, secondProportion int, vertical bool) *uiutil.ResizableSplit {
	var split *uiutil.ResizableSplit
	if vertical {
		split = uiutil.NewVerticalResizableSplit(page.application, first, second, firstProportion, secondProportion)
	} else {
		split = uiutil.NewResizableSplit(page.application, first, second, firstProportion, secondProportion)
	}
	return split.SetEnabledFunc(page.isInFront)
}

// captureSplitDrags lets the boundaries of the splits be dragged. Registered on the top-level layout of the page,
// so a drag is not lost when the mouse leaves the panes (see the tview rules in AGENTS.md).
func captureSplitDrags(layout *tview.Flex, splits ...*uiutil.ResizableSplit) {
	layout.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		for _, split := range splits {
			action, event = split.MouseCapture(action, event)
			if event == nil {
				return action, nil
			}
		}
		return action, event
	})
}
