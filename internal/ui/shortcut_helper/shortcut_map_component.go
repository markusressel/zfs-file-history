package shortcut_helper

import (
	"cmp"
	"os"
	"slices"
	"strings"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"golang.org/x/term"
)

// ShortcutEntry is a shortcut shown in a shortcut map and, if it can be run, a command of the command menu (see
// dialog.CommandMenu), with its keys as a hint.
type ShortcutEntry struct {
	// KeyCombo are the keys of the shortcut, empty for commands that are only run from the command menu
	KeyCombo []string
	Name     string
	// Description tells what the command does, shown behind its name in the command menu (not in the shortcut map)
	Description string
	// Group decides the color of the keys and where the entry is shown, see ShortcutGroup
	Group ShortcutGroup
	// Run runs the command, nil if it can only be run with its keys (e.g. moving the selection)
	Run func()
	// MenuOnly entries are not shown in the shortcut map, only in the command menu
	MenuOnly bool
}

// WithRun returns the entry with Run set to run, so it is a command of the command menu.
func (entry ShortcutEntry) WithRun(run func()) ShortcutEntry {
	entry.Run = run
	return entry
}

// WithDescription returns the entry with the description, see ShortcutEntry.Description.
func (entry ShortcutEntry) WithDescription(description string) ShortcutEntry {
	entry.Description = description
	return entry
}

// OnlyInMenu returns the entry with MenuOnly set, so it is only shown in the command menu.
func (entry ShortcutEntry) OnlyInMenu() ShortcutEntry {
	entry.MenuOnly = true
	return entry
}

// Commands returns the entries that can be run, i.e. the ones of the command menu.
func Commands(entries []ShortcutEntry) []ShortcutEntry {
	var commands []ShortcutEntry
	for _, entry := range entries {
		if entry.Run != nil {
			commands = append(commands, entry)
		}
	}
	return commands
}

// ShortcutGroup groups the entries of a shortcut map, so the one looked for is found quickly: the groups are
// shown in this order, separated by a line, and the keys of each group have their own color
// (theme.Colors.ShortcutMap). Within a group, the entries keep their order.
type ShortcutGroup int

const (
	// GroupAction is for actions on the selection or the component (the default), e.g. restore or history
	GroupAction ShortcutGroup = iota
	// GroupView is for keys that change what is shown, e.g. columns, filter, sorting or modes
	GroupView
	// GroupNavigation is for keys that move the selection or the focus
	GroupNavigation
	// GroupGlobal is for keys that work everywhere, e.g. switching pages or quitting
	GroupGlobal
)

// KeyColor returns the color of the keys of the group.
func (group ShortcutGroup) KeyColor() tcell.Color {
	switch group {
	case GroupView:
		return theme.Colors.ShortcutMap.ViewKeyCombo
	case GroupNavigation:
		return theme.Colors.ShortcutMap.NavigationKeyCombo
	case GroupGlobal:
		return theme.Colors.ShortcutMap.GlobalKeyCombo
	default:
		return theme.Colors.ShortcutMap.KeyCombo
	}
}

// groupSeparator is shown between groups. Surrounded by spaces, so lines may wrap around it.
const groupSeparator = "│"

// sortedByGroup returns the entries shown in a shortcut map (not MenuOnly) ordered by group, keeping their order
// within a group.
func sortedByGroup(entries []ShortcutEntry) []ShortcutEntry {
	sorted := slices.DeleteFunc(slices.Clone(entries), func(entry ShortcutEntry) bool { return entry.MenuOnly })
	slices.SortStableFunc(sorted, func(a, b ShortcutEntry) int { return cmp.Compare(a.Group, b.Group) })
	return sorted
}

// formatEntries returns the text of the entries, styled or (for calculating its size) plain.
func formatEntries(entries []ShortcutEntry, styled bool) string {
	var text strings.Builder
	sorted := sortedByGroup(entries)
	for i, entry := range sorted {
		if i > 0 {
			text.WriteString("  ")
			if sorted[i-1].Group != entry.Group {
				if styled {
					text.WriteString(txwidgets.Span(theme.Colors.ShortcutMap.Separator, "%s", groupSeparator))
				} else {
					text.WriteString(groupSeparator)
				}
				text.WriteString("  ")
			}
		}
		// alternative keys joined with a non-breaking vertical line, so an entry is never wrapped
		keys := "[" + strings.Join(entry.KeyCombo, "\u01c0") + "]"
		name := strings.ReplaceAll(entry.Name, " ", "\u00a0")
		if styled {
			keys = txwidgets.Span(entry.Group.KeyColor(), "%s", keys)
			name = txwidgets.Span(theme.Colors.ShortcutMap.Name, "%s", name)
		}
		text.WriteString(keys + ":\u00a0" + name)
	}
	return text.String()
}

type ShortcutMapComponent struct {
	application *tview.Application

	layout                  *tview.Flex
	shortcutEntriesTextView *tview.TextView
	onHeightChanged         func(height int)
	// collapsible maps are hidden with ToggleShortcuts, see SetCollapsible
	collapsible bool
	// alternativeEntries returns the entries that may be shown instead of the current ones, see
	// SetAlternativeEntries
	alternativeEntries func() [][]ShortcutEntry

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

// SetAlternativeEntries sets the entries that may be shown instead of the current ones, e.g. those of the other
// components of a page: the map is as high as the highest of them needs, so its height does not change (and the
// layout above it does not move) when the entries are switched. Called on each draw.
func (sm *ShortcutMapComponent) SetAlternativeEntries(alternatives func() [][]ShortcutEntry) {
	sm.alternativeEntries = alternatives
}

func (sm *ShortcutMapComponent) SetEntries(entries []ShortcutEntry) {
	sm.ShortCutEntries = entries
	if sm.isHidden() {
		sm.shortcutEntriesTextView.SetText("")
		if sm.onHeightChanged != nil {
			sm.onHeightChanged(0)
		}
		return
	}
	sm.shortcutEntriesTextView.SetText(formatEntries(entries, true))
	if sm.onHeightChanged != nil {
		lines := sm.CalculateHeightFromTerminal()
		sm.onHeightChanged(lines)
	}
}

func (sm *ShortcutMapComponent) Clear() {
	sm.shortcutEntriesTextView.SetText("")
	if sm.onHeightChanged != nil {
		height := 1
		if sm.isHidden() {
			height = 0
		}
		sm.onHeightChanged(height)
	}
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
	if sm.isHidden() {
		return 0
	}
	availableWidth := width - 2 // padding
	if availableWidth <= 0 {
		availableWidth = 80
	}

	lines := linesForWidth(sm.ShortCutEntries, availableWidth)
	if sm.alternativeEntries != nil {
		for _, entries := range sm.alternativeEntries() {
			lines = max(lines, linesForWidth(entries, availableWidth))
		}
	}
	return lines
}

// linesForWidth returns the number of lines the entries need when wrapped at availableWidth, at least 1.
func linesForWidth(entries []ShortcutEntry, availableWidth int) int {
	visibleText := formatEntries(entries, false)
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
