package path_overview

import (
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// partSeparator is shown between the parts of a line.
const partSeparator = " · "

// textPart is a part of a line that may be left out if the line is too narrow.
type textPart struct {
	// styled is the text with style tags; if empty, plain is shown in color
	styled string
	// plain is the text of a part that is shortened from the left instead of left out (e.g. a path), in color
	plain string
	color tcell.Color
	// priority 0 is always shown; parts with a higher priority are left out first
	priority int
}

func (part textPart) text() string {
	if part.styled != "" {
		return part.styled
	}
	return txwidgets.Span(part.color, "%s", part.plain)
}

func (part textPart) width() int {
	return tview.TaggedStringWidth(part.text())
}

// minShortenedWidth is the width down to which a path is shortened before important parts are left out.
const minShortenedWidth = 12

// fitParts joins the parts to a line of at most width cells. Until it fits: the parts with priority 2 and higher
// are left out (highest first), the parts with priority 0 are shortened from the left ("…markus/docs") down to
// minShortenedWidth, the parts with priority 1 are left out, and finally shortened further.
func fitParts(parts []textPart, width int) string {
	separatorWidth := textWidth(partSeparator)
	shown := append([]textPart(nil), parts...)
	excess := func() int {
		total := 0
		for i, part := range shown {
			if i > 0 {
				total += separatorWidth
			}
			total += part.width()
		}
		return total - width
	}
	// dropLeastImportant leaves out the part with the highest priority of at least minPriority
	dropLeastImportant := func(minPriority int) bool {
		dropped := -1
		for i, part := range shown {
			if part.priority >= minPriority && (dropped < 0 || part.priority >= shown[dropped].priority) {
				dropped = i
			}
		}
		if dropped < 0 {
			return false
		}
		shown = append(shown[:dropped], shown[dropped+1:]...)
		return true
	}
	// shorten shortens the parts with priority 0, not below minWidth
	shorten := func(minWidth int) {
		for i, part := range shown {
			if over := excess(); over > 0 && part.priority == 0 && part.plain != "" && part.width() > minWidth {
				shown[i] = textPart{plain: shortenLeft(part.plain, max(part.width()-over, minWidth)), color: part.color}
			}
		}
	}

	for excess() > 0 && dropLeastImportant(2) {
	}
	unshortened := append([]textPart(nil), shown...)
	shorten(minShortenedWidth)
	if excess() > 0 {
		// leaving out the important parts instead: from the whole path again, which may fit now
		shown = unshortened
		for excess() > 0 && dropLeastImportant(1) {
		}
		shorten(1)
	}

	line := ""
	for i, part := range shown {
		if i > 0 {
			line += txwidgets.Span(theme.Colors.ShortcutMap.Name, "%s", partSeparator)
		}
		line += part.text()
	}
	return line
}

// shortenLeft shortens text to at most width cells by replacing its beginning with an ellipsis.
func shortenLeft(text string, width int) string {
	if textWidth(text) <= width {
		return text
	}
	if width < 1 {
		return ""
	}
	runes := []rune(text)
	for start := range runes {
		if rest := string(runes[start:]); textWidth(rest) <= width-1 {
			return "…" + rest
		}
	}
	return "…"
}

// printFitted prints the parts in a line of at most width cells, see fitParts.
func printFitted(screen tcell.Screen, parts []textPart, x int, y int, width int) {
	tview.Print(screen, fitParts(parts, width), x, y, width, tview.AlignLeft, theme.Colors.ShortcutMap.Name)
}

// textWidth returns the width of plain text in cells.
func textWidth(text string) int {
	return tview.TaggedStringWidth(tview.Escape(text))
}
