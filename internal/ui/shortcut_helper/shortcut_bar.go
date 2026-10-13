package shortcut_helper

import (
	"strings"
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

// barTarget is what a click on the map runs: an entry, or one of its keys.
type barTarget struct {
	// item is the index in the layout, -1 for none
	item int
	// key is the index of the clicked key, -1 for the entry as a whole (its first key, or its Run)
	key int
}

var noTarget = barTarget{item: -1, key: -1}

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

	// hovered is the entry (or key) below the mouse
	hovered barTarget
}

func newShortcutBar(application *tview.Application) *shortcutBar {
	bar := &shortcutBar{
		Box:     tview.NewBox().SetBorderPadding(0, 0, 1, 1),
		enabled: func() bool { return true },
		pressKey: func(event *tcell.EventKey) {
			// not on the UI thread: QueueEvent blocks while the event queue is full
			go application.QueueEvent(event)
		},
		hovered: noTarget,
	}
	return bar
}

// setEntries sets the shown entries (nil for none).
func (bar *shortcutBar) setEntries(entries []ShortcutEntry) {
	bar.entries = entries
	// the positions changed
	bar.hovered = noTarget
}

// layout places the entries in the current width of the bar.
func (bar *shortcutBar) layout() []barItem {
	_, _, width, _ := bar.GetInnerRect()
	return layoutEntries(bar.entries, width)
}

func (bar *shortcutBar) Draw(screen tcell.Screen) {
	bar.DrawForSubclass(screen, bar)
	x, y, width, height := bar.GetInnerRect()
	if bar.beforeDraw != nil {
		bar.beforeDraw(width, height)
	}
	for index, item := range bar.layout() {
		if item.line >= height {
			break
		}
		for _, segment := range item.segments {
			segmentX := x + item.x + segment.x
			if segmentX >= x+width {
				break
			}
			tview.Print(screen, tview.Escape(segment.text), segmentX, y+item.line, x+width-segmentX, tview.AlignLeft, segment.color)
			if bar.isHighlighted(index, segment) {
				for column := segmentX; column < min(segmentX+segment.width, x+width); column++ {
					str, style, _ := screen.Get(column, y+item.line)
					screen.Put(column, y+item.line, str, style.Background(theme.Colors.ShortcutMap.HoverBackground))
				}
			}
		}
	}
}

// isHighlighted returns whether the segment of the item is below the mouse: the hovered key, or all of the hovered
// entry.
func (bar *shortcutBar) isHighlighted(item int, segment barSegment) bool {
	if bar.hovered.item != item {
		return false
	}
	return bar.hovered.key < 0 || bar.hovered.key == segment.key
}

// targetAt returns what a click at the position would run.
func (bar *shortcutBar) targetAt(x int, y int) barTarget {
	rectX, rectY, width, _ := bar.GetInnerRect()
	if x < rectX || x >= rectX+width {
		return noTarget
	}
	x, y = x-rectX, y-rectY
	for index, item := range bar.layout() {
		if item.line != y || x < item.x || x >= item.x+item.width || item.entry == nil {
			continue
		}
		if !isClickable(item.entry) {
			return noTarget
		}
		for _, segment := range item.segments {
			// a key of several: only that one, if it is a key that can be pressed
			if segment.key >= 0 && len(item.entry.KeyCombo) > 1 && x >= item.x+segment.x && x < item.x+segment.x+segment.width {
				if _, ok := keyEvent(item.entry.KeyCombo[segment.key]); ok {
					return barTarget{item: index, key: segment.key}
				}
			}
		}
		return barTarget{item: index, key: -1}
	}
	return noTarget
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

// click presses the key of the target, or runs its entry if it has no key that can be pressed.
func (bar *shortcutBar) click(target barTarget) {
	items := bar.layout()
	if target.item < 0 || target.item >= len(items) || items[target.item].entry == nil {
		return
	}
	entry := items[target.item].entry
	key := max(target.key, 0)
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

// MouseHandler highlights the entry below the mouse and runs a clicked one. The bar never takes the focus, so the
// pressed key goes to the focused component, the one whose entries are shown.
func (bar *shortcutBar) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
	return bar.WrapMouseHandler(func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
		x, y := event.Position()
		if !bar.enabled() || !bar.InRect(x, y) {
			// e.g. moved away: tview only sends the events of other positions while the bar captures the mouse
			changed := bar.hovered != noTarget
			bar.hovered = noTarget
			return changed, nil
		}
		target := bar.targetAt(x, y)
		changed := target != bar.hovered
		bar.hovered = target
		if action == tview.MouseLeftClick {
			bar.click(target)
		}
		// captures the mouse while on the bar, to know when it leaves it
		return changed || action == tview.MouseLeftDown || action == tview.MouseLeftClick, bar
	})
}
