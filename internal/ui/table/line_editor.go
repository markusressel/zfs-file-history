package table

import (
	"unicode"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// lineEditor is a single line text input with a cursor, which behaves like a terminal input line.
// It is used for typing the filter of a RowSelectionTable, which is shown in the window title.
type lineEditor struct {
	text   []rune
	cursor int
}

// Text returns the current text.
func (e *lineEditor) Text() string {
	return string(e.text)
}

// Reset sets the text and moves the cursor to its end.
func (e *lineEditor) Reset(text string) {
	e.text = []rune(text)
	e.cursor = len(e.text)
}

// HandleKey applies an editing key. It returns false for keys that are not editing keys.
func (e *lineEditor) HandleKey(event *tcell.EventKey) bool {
	wordJump := event.Modifiers()&(tcell.ModCtrl|tcell.ModAlt) != 0

	switch event.Key() {
	case tcell.KeyRune:
		e.text = append(e.text[:e.cursor], append([]rune{event.Rune()}, e.text[e.cursor:]...)...)
		e.cursor++
	case tcell.KeyLeft:
		if wordJump {
			e.cursor = e.previousWordStart()
		} else if e.cursor > 0 {
			e.cursor--
		}
	case tcell.KeyRight:
		if wordJump {
			e.cursor = e.nextWordEnd()
		} else if e.cursor < len(e.text) {
			e.cursor++
		}
	case tcell.KeyHome, tcell.KeyCtrlA:
		e.cursor = 0
	case tcell.KeyEnd, tcell.KeyCtrlE:
		e.cursor = len(e.text)
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if wordJump {
			// ctrl+Backspace (or alt+Backspace): the word before the cursor
			e.deletePreviousWord()
		} else if e.cursor > 0 {
			e.text = append(e.text[:e.cursor-1], e.text[e.cursor:]...)
			e.cursor--
		}
	case tcell.KeyDelete:
		if wordJump {
			// ctrl+Delete (or alt+Delete): the word after the cursor
			e.text = append(e.text[:e.cursor], e.text[e.nextWordEnd():]...)
		} else if e.cursor < len(e.text) {
			e.text = append(e.text[:e.cursor], e.text[e.cursor+1:]...)
		}
	case tcell.KeyCtrlW:
		e.deletePreviousWord()
	case tcell.KeyCtrlU:
		e.text = e.text[e.cursor:]
		e.cursor = 0
	case tcell.KeyCtrlK:
		e.text = e.text[:e.cursor]
	default:
		return false
	}
	return true
}

// deletePreviousWord deletes the word before the cursor, including the separators between it and the cursor.
func (e *lineEditor) deletePreviousWord() {
	start := e.previousWordStart()
	e.text = append(e.text[:start], e.text[e.cursor:]...)
	e.cursor = start
}

// previousWordStart returns the start of the word before the cursor (skipping separators in between).
func (e *lineEditor) previousWordStart() int {
	position := e.cursor
	for position > 0 && !isWordRune(e.text[position-1]) {
		position--
	}
	for position > 0 && isWordRune(e.text[position-1]) {
		position--
	}
	return position
}

// nextWordEnd returns the end of the word after the cursor (skipping separators in between).
func (e *lineEditor) nextWordEnd() int {
	position := e.cursor
	for position < len(e.text) && !isWordRune(e.text[position]) {
		position++
	}
	for position < len(e.text) && isWordRune(e.text[position]) {
		position++
	}
	return position
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// Render returns the text with tview style tags that show the cursor like a terminal does:
// the character at the cursor in reverse video, or a reversed space at the end of the text.
func (e *lineEditor) Render() string {
	before := tview.Escape(string(e.text[:e.cursor]))
	if e.cursor >= len(e.text) {
		return before + "[::r] [::-]"
	}
	atCursor := tview.Escape(string(e.text[e.cursor]))
	after := tview.Escape(string(e.text[e.cursor+1:]))
	return before + "[::r]" + atCursor + "[::-]" + after
}

// RenderWindow renders like Render, within maxWidth cells. If the text (plus the cursor at its end) does not fit,
// only a part around the cursor is shown, like a terminal input line scrolls. Cut off sides show an ellipsis,
// unless the cursor is there.
func (e *lineEditor) RenderWindow(maxWidth int) string {
	cellCount := len(e.text) + 1 // the cursor can be after the last character
	if cellCount <= maxWidth {
		return e.Render()
	}
	if maxWidth <= 0 {
		return ""
	}

	start := 0
	if e.cursor >= maxWidth {
		start = e.cursor - maxWidth + 1
	}
	end := start + maxWidth

	cells := make([]rune, 0, maxWidth)
	for i := start; i < end; i++ {
		if i < len(e.text) {
			cells = append(cells, e.text[i])
		} else {
			cells = append(cells, ' ')
		}
	}
	cursor := e.cursor - start
	if start > 0 && cursor != 0 {
		cells[0] = tview.SemigraphicsHorizontalEllipsis
	}
	if end < cellCount && cursor != len(cells)-1 {
		cells[len(cells)-1] = tview.SemigraphicsHorizontalEllipsis
	}

	return tview.Escape(string(cells[:cursor])) + "[::r]" + tview.Escape(string(cells[cursor])) + "[::-]" + tview.Escape(string(cells[cursor+1:]))
}
