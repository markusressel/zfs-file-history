package shortcut_helper

import (
	"strings"
	"zfs-file-history/internal/ui/clickable"
	"zfs-file-history/internal/ui/theme"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// entryGap is the space between two entries (and around the separator between groups).
const entryGap = 2

// barItem is an entry, or the separator between two groups, placed in a shortcut map (see layoutEntries).
type barItem struct {
	// line and x are the position of the item within the map, relative to its inner area
	line, x, width int
	// entry is the shown entry, nil for a separator
	entry    *ShortcutEntry
	segments []barSegment
}

// barSegment is a part of an item drawn in one color: a key, or the text around the keys.
type barSegment struct {
	text  string
	color tcell.Color
	// x is the position within the item
	x, width int
	// key is the index of the key in ShortcutEntry.KeyCombo, -1 for the brackets, separators and the name
	key int
}

// barTarget is the key of what a click on the map runs (see clickable.Region): an entry, or one of its keys.
type barTarget struct {
	// item is the index in the layout
	item int
	// key is the index of the clicked key, -1 for the entry as a whole (its first key, or its Run)
	key int
}

// newBarItem returns the item of the entry: "[keys]: name", with the alternative keys joined by a vertical line.
// Spaces in the name are non-breaking, like in the plain text of formatEntries.
func newBarItem(entry *ShortcutEntry) barItem {
	item := barItem{entry: entry}
	add := func(text string, color tcell.Color, key int) {
		width := tview.TaggedStringWidth(tview.Escape(text))
		item.segments = append(item.segments, barSegment{text: text, color: color, x: item.width, width: width, key: key})
		item.width += width
	}
	keyColor := entry.Group.KeyColor()
	add("[", keyColor, -1)
	for i, key := range entry.KeyCombo {
		if i > 0 {
			add("ǀ", keyColor, -1)
		}
		add(key, keyColor, i)
	}
	add("]", keyColor, -1)
	add(": "+strings.ReplaceAll(entry.Name, " ", " "), theme.Colors.ShortcutMap.Name, -1)
	return item
}

func newSeparatorItem() barItem {
	width := tview.TaggedStringWidth(groupSeparator)
	return barItem{width: width, segments: []barSegment{
		{text: groupSeparator, color: theme.Colors.ShortcutMap.Separator, width: width, key: -1},
	}}
}

// layoutEntries places the shown entries (see sortedByGroup) in lines of the given width: an entry is never
// wrapped, it moves to the next line as a whole. An entry wider than a line is cut off.
func layoutEntries(entries []ShortcutEntry, width int) []barItem {
	sorted := sortedByGroup(entries)
	var items []barItem
	line, x := 0, 0
	place := func(item barItem) {
		if x > 0 {
			x += entryGap
			if x+item.width > width {
				line++
				x = 0
			}
		}
		item.line, item.x = line, x
		x += item.width
		items = append(items, item)
	}
	for i := range sorted {
		if i > 0 && sorted[i-1].Group != sorted[i].Group {
			place(newSeparatorItem())
		}
		place(newBarItem(&sorted[i]))
	}
	return items
}

// lineCount returns the number of lines of the items, at least 1.
func lineCount(items []barItem) int {
	if len(items) == 0 {
		return 1
	}
	return items[len(items)-1].line + 1
}

// shortcutBar draws the entries of a shortcut map. Its entries can be clicked, which presses their key (see
// keyEvent): the first one, or the clicked one of an entry with alternative keys (e.g. [1ǀ2ǀ3]). The entry (or
// key) below the mouse is highlighted.
type shortcutBar struct {
	*tview.Box
	entries []ShortcutEntry
	// enabled returns whether the entries react to the mouse, e.g. not while a dialog is shown above the map
	enabled func() bool
	// pressKey sends the key event of a clicked entry to the application, replaceable in tests
	pressKey func(event *tcell.EventKey)
	// beforeDraw is called with the inner size before drawing, see ShortcutMapComponent.createLayout
	beforeDraw func(width int, height int)

	// regions are the entries and keys that can be clicked, added by Draw
	regions clickable.Regions
}

func newShortcutBar(application *tview.Application) *shortcutBar {
	bar := &shortcutBar{
		Box:     tview.NewBox().SetBorderPadding(0, 0, 1, 1),
		enabled: func() bool { return true },
		pressKey: func(event *tcell.EventKey) {
			// not on the UI thread: QueueEvent blocks while the event queue is full
			go application.QueueEvent(event)
		},
	}
	return bar
}

// setEntries sets the shown entries (nil for none).
func (bar *shortcutBar) setEntries(entries []ShortcutEntry) {
	bar.entries = entries
	// the positions changed
	bar.regions.ClearHover()
}

// layout places the entries in the current width of the bar.
func (bar *shortcutBar) layout() []barItem {
	_, _, width, _ := bar.GetInnerRect()
	return layoutEntries(bar.entries, width)
}

func (bar *shortcutBar) Draw(screen tcell.Screen) {
	bar.DrawForSubclass(screen, bar)
	bar.regions.Reset()
	x, y, width, height := bar.GetInnerRect()
	if bar.beforeDraw != nil {
		bar.beforeDraw(width, height)
	}
	hover := theme.Colors.ShortcutMap.HoverBackground
	for index, item := range bar.layout() {
		if item.line >= height {
			break
		}
		itemX := x + item.x
		for _, segment := range item.segments {
			segmentX := itemX + segment.x
			if segmentX >= x+width {
				break
			}
			tview.Print(screen, tview.Escape(segment.text), segmentX, y+item.line, x+width-segmentX, tview.AlignLeft, segment.color)
		}
		if item.entry == nil || !isClickable(item.entry) {
			continue
		}
		entry := item.entry
		bar.regions.Add(clickable.Region{X: itemX, Y: y + item.line, Width: min(item.width, x+width-itemX),
			Key: barTarget{item: index, key: -1}, HoverBackground: hover, OnClick: func() { bar.click(entry, -1) }})
		// one of several keys, if it can be pressed: only that one, added last, so it wins over the entry
		for _, segment := range item.segments {
			if segment.key < 0 || len(entry.KeyCombo) < 2 {
				continue
			}
			if _, ok := keyEvent(entry.KeyCombo[segment.key]); ok {
				key, segmentX := segment.key, itemX+segment.x
				bar.regions.Add(clickable.Region{X: segmentX, Y: y + item.line, Width: min(segment.width, x+width-segmentX),
					Key: barTarget{item: index, key: key}, HoverBackground: hover, OnClick: func() { bar.click(entry, key) }})
			}
		}
	}
	bar.regions.DrawHover(screen)
}

// isClickable returns whether clicking the entry does something: pressing its first key, or running it.
func isClickable(entry *ShortcutEntry) bool {
	if len(entry.KeyCombo) > 0 {
		if _, ok := keyEvent(entry.KeyCombo[0]); ok {
			return true
		}
	}
	return entry.Run != nil
}

// click presses the key of the entry with the index (-1: its first key), or runs the entry if it has no key that can
// be pressed.
func (bar *shortcutBar) click(entry *ShortcutEntry, key int) {
	key = max(key, 0)
	if key < len(entry.KeyCombo) {
		if event, ok := keyEvent(entry.KeyCombo[key]); ok {
			bar.pressKey(event)
			return
		}
	}
	if entry.Run != nil {
		entry.Run()
	}
}

// MouseHandler highlights the entry below the mouse and runs a clicked one, see regions. The bar never takes the
// focus, so the pressed key goes to the focused component, the one whose entries are shown.
func (bar *shortcutBar) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
	return bar.WrapMouseHandler(func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
		consumed, captures := bar.regions.HandleMouse(action, event, bar.enabled() && bar.InRect(event.Position()))
		if captures {
			return consumed, bar
		}
		return consumed, nil
	})
}
