package shortcut_helper

import (
	"fmt"
	"os"
	"strings"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"golang.org/x/term"
)

type ShortcutEntry struct {
	KeyCombo []string
	Name     string
}

type ShortcutMapComponent struct {
	application *tview.Application

	layout                  *tview.Flex
	shortcutEntriesTextView *tview.TextView
	onHeightChanged         func(height int)

	ShortCutEntries []ShortcutEntry
}

func NewShortcutMap(application *tview.Application) *ShortcutMapComponent {
	shortcutMap := &ShortcutMapComponent{
		application: application,
	}

	shortcutMap.createLayout()

	return shortcutMap
}

func (sm *ShortcutMapComponent) createLayout() {
	layout := tview.NewFlex().SetDirection(tview.FlexColumn)

	shortcutEntriesTextView := tview.NewTextView().
		SetDynamicColors(true)
	shortcutEntriesTextView.SetBorderPadding(0, 0, 1, 1)
	shortcutEntriesTextView.SetTextAlign(tview.AlignLeft)

	// Set draw func to monitor and dynamically resize height on line wraps
	shortcutEntriesTextView.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		lines := sm.CalculateHeightForWidth(width)
		if height != lines {
			if sm.onHeightChanged != nil {
				go func() {
					sm.application.QueueUpdateDraw(func() {
						if sm.onHeightChanged != nil {
							sm.onHeightChanged(lines)
						}
					})
				}()
			}
		}
		return x, y, width, height
	})

	layout.AddItem(shortcutEntriesTextView, 0, 1, false)

	sm.shortcutEntriesTextView = shortcutEntriesTextView
	sm.layout = layout
}

func (sm *ShortcutMapComponent) SetOnHeightChanged(f func(height int)) {
	sm.onHeightChanged = f
}

func (sm *ShortcutMapComponent) SetEntries(entries []ShortcutEntry) {
	sm.ShortCutEntries = entries
	var statusText string
	for _, entry := range entries {
		// comma separated list joined with non-breaking vertical line
		shortCutsText := strings.Join(entry.KeyCombo, "\u01c0")
		shortcuts := txwidgets.Span(theme.Colors.ShortcutMap.KeyCombo, "[%s]", shortCutsText)
		nameText := strings.ReplaceAll(entry.Name, " ", "\u00a0")
		name := txwidgets.Span(theme.Colors.ShortcutMap.Name, "%s", nameText)
		statusText += fmt.Sprintf("%s:\u00a0%s  ", shortcuts, name)
	}
	sm.shortcutEntriesTextView.SetText(statusText)
	if sm.onHeightChanged != nil {
		lines := sm.CalculateHeightFromTerminal()
		sm.onHeightChanged(lines)
	}
	sm.application.ForceDraw()
}

func (sm *ShortcutMapComponent) Clear() {
	sm.shortcutEntriesTextView.SetText("")
	if sm.onHeightChanged != nil {
		sm.onHeightChanged(1)
	}
	sm.application.ForceDraw()
}

func (sm *ShortcutMapComponent) GetLayout() *tview.Flex {
	return sm.layout
}

func (sm *ShortcutMapComponent) CalculateHeightFromTerminal() int {
	_, _, width, _ := sm.layout.GetRect()
	if width <= 0 {
		var err error
		width, _, err = term.GetSize(int(os.Stdout.Fd()))
		if err != nil || width <= 0 {
			width = 80
		}
	}
	return sm.CalculateHeightForWidth(width)
}

func (sm *ShortcutMapComponent) CalculateHeightForWidth(width int) int {
	availableWidth := width - 2 // padding
	if availableWidth <= 0 {
		availableWidth = 80
	}

	var visibleText string
	for _, entry := range sm.ShortCutEntries {
		shortCutsText := strings.Join(entry.KeyCombo, "\u01c0")
		nameText := strings.ReplaceAll(entry.Name, " ", "\u00a0")
		visibleText += fmt.Sprintf("[%s]:\u00a0%s  ", shortCutsText, nameText)
	}

	visibleText = strings.TrimSpace(visibleText)
	if len(visibleText) == 0 {
		return 1
	}

	runes := []rune(visibleText)
	lines := 1
	currentLineLength := 0

	i := 0
	for i < len(runes) {
		if runes[i] == ' ' {
			currentLineLength++
			i++
		} else {
			wordStart := i
			for i < len(runes) && runes[i] != ' ' {
				i++
			}
			wordWidth := i - wordStart

			if currentLineLength+wordWidth > availableWidth {
				lines++
				currentLineLength = wordWidth
			} else {
				currentLineLength += wordWidth
			}
		}
	}

	return lines
}
