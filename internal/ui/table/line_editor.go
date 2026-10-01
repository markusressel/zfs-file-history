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
		if e.cursor > 0 {
			e.text = append(e.text[:e.cursor-1], e.text[e.cursor:]...)
			e.cursor--
		}
	case tcell.KeyDelete:
		if e.cursor < len(e.text) {
			e.text = append(e.text[:e.cursor], e.text[e.cursor+1:]...)
		}
	case tcell.KeyCtrlW:
		start := e.previousWordStart()
		e.text = append(e.text[:start], e.text[e.cursor:]...)
		e.cursor = start
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
