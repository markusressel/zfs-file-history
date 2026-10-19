package util

import (
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// keyRepeatGap is the longest time between two presses of a key that counts as holding it down: terminals do not send
// releases, a held key repeats (typically every 30 to 50 ms after a first delay), while pressing it again takes
// longer.
const keyRepeatGap = 150 * time.Millisecond

// FirstRowStop makes PgUp and Home in a table with a header row (row 0, fixed and selectable, e.g. to sort) stop at
// the first row below it (firstRow) before they select the header: tview moves a page up and stops at the header, and
// Home selects it, so the first row was skipped. While the key is held down, it stays on the first row; a new press
// selects the header. Only accessed on the UI thread.
type FirstRowStop struct {
	// lastKey and lastPress are the last key handled and when it was pressed
	lastKey   tcell.Key
	lastPress time.Time
	// now returns the current time, replaceable in tests
	now func() time.Time
}

// SetClock sets the function that returns the current time, for tests of other packages.
func (stop *FirstRowStop) SetClock(now func() time.Time) {
	stop.now = now
}

// HandleKey handles PgUp and Home in the input capture of the table, with the function that selects firstRow (e.g.
// scrolled to the top, with the row of a group above it). Returns whether it handled the key; if not, it goes on to
// tview. Must be called on the UI thread.
func (stop *FirstRowStop) HandleKey(table *tview.Table, event *tcell.EventKey, firstRow int, selectFirstRow func()) bool {
	key := event.Key()
	if (key != tcell.KeyPgUp && key != tcell.KeyHome) || event.Modifiers() != tcell.ModNone {
		stop.lastKey = tcell.KeyNUL
		return false
	}
	now := time.Now()
	if stop.now != nil {
		now = stop.now()
	}
	held := stop.lastKey == key && now.Sub(stop.lastPress) <= keyRepeatGap
	stop.lastKey, stop.lastPress = key, now

	row, _ := table.GetSelection()
	switch {
	case row < firstRow:
		// the header already
		return false
	case row == firstRow:
		// held down: stays until the key is pressed again
		return held
	case key == tcell.KeyHome:
		selectFirstRow()
		return true
	}
	// as tview pages: by the rows shown below the fixed header row
	_, _, _, height := table.GetInnerRect()
	if row-max(height-1, 0) >= firstRow {
		return false
	}
	selectFirstRow()
	return true
}
