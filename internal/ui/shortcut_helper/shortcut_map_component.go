package shortcut_helper

import (
	"cmp"
	"math"
	"os"
	"slices"
	"strings"
	"zfs-file-history/internal/ui/theme"

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
	// SearchOnly commands are only listed in the command menu while something is searched, e.g. one per column to
	// sort by: listed always, they would push the commands of the shown view out of sight. An entry with a Query
	// leads to them.
	SearchOnly bool
	// Query makes the entry a shortcut to a search of the command menu (e.g. "sort" for the SearchOnly commands to
	// sort by a column): choosing it types the query instead of running something. It is only listed while nothing
	// is searched. Needs no Run.
	Query string
	// On is set for an entry that switches something on and off (e.g. hiding the unmounted datasets): whether it is
	// on. The command menu shows it as a switch with its state, named SwitchName (e.g. "Hide unmounted datasets")
	// instead of Name, which tells what its key does now (e.g. "Show unmounted"). See AsSwitch.
	On func() bool
	// SwitchName and SwitchDescription replace Name and Description in the command menu, see On
	SwitchName        string
	SwitchDescription string
}

// AsSwitch returns the entry as a switch of the command menu, with its name and description there, and whether it
// is on, see ShortcutEntry.On.
func (entry ShortcutEntry) AsSwitch(name string, description string, on func() bool) ShortcutEntry {
	entry.SwitchName, entry.SwitchDescription, entry.On = name, description, on
	return entry
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

// Commands returns the entries that can be run (or lead to a search, see ShortcutEntry.Query), i.e. the ones of the
// command menu.
func Commands(entries []ShortcutEntry) []ShortcutEntry {
	var commands []ShortcutEntry
	for _, entry := range entries {
		if entry.Run != nil || entry.Query != "" {
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

// formatEntries returns the plain text of the entries, as shown in one line.
func formatEntries(entries []ShortcutEntry) string {
	var text strings.Builder
	for i, item := range layoutEntries(entries, math.MaxInt) {
		if i > 0 {
			text.WriteString(strings.Repeat(" ", entryGap))
		}
		for _, segment := range item.segments {
			text.WriteString(segment.text)
		}
	}
	return text.String()
}

type ShortcutMapComponent struct {
	application *tview.Application

	layout          *tview.Flex
	bar             *shortcutBar
	onHeightChanged func(height int)
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

	bar := newShortcutBar(sm.application)
	// resizes the map when its lines wrap differently, e.g. after the terminal was resized
	bar.beforeDraw = func(width int, height int) {
		lines := sm.CalculateHeightForWidth(width + 2)
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
	}

	layout.AddItem(bar, 0, 1, false)

	sm.bar = bar
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

// SetEnabledFunc sets a function that returns whether the entries can be clicked, e.g. not while a dialog is
// shown above the map.
func (sm *ShortcutMapComponent) SetEnabledFunc(enabled func() bool) *ShortcutMapComponent {
	sm.bar.enabled = enabled
	return sm
}

func (sm *ShortcutMapComponent) SetEntries(entries []ShortcutEntry) {
	sm.ShortCutEntries = entries
	if sm.isHidden() {
		sm.bar.setEntries(nil)
		if sm.onHeightChanged != nil {
			sm.onHeightChanged(0)
		}
		return
	}
	sm.bar.setEntries(entries)
	if sm.onHeightChanged != nil {
		lines := sm.CalculateHeightFromTerminal()
		sm.onHeightChanged(lines)
	}
}

func (sm *ShortcutMapComponent) Clear() {
	sm.bar.setEntries(nil)
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

// linesForWidth returns the number of lines the entries need in availableWidth, at least 1.
func linesForWidth(entries []ShortcutEntry, availableWidth int) int {
	return lineCount(layoutEntries(entries, availableWidth))
}
