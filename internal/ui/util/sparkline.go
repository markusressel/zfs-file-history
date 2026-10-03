package util

import (
	"zfs-file-history/internal/ui/theme"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// brailleBlank is the empty braille pattern; the dots of a cell are added to it.
const brailleBlank = rune(0x2800)

// brailleDot are the bits of the dots of a braille cell, by row (top first) and column (left, right),
// like the bar graphs of fan2go-tui.
var brailleDot = [4][2]rune{
	{0x01, 0x08},
	{0x02, 0x10},
	{0x04, 0x20},
	{0x40, 0x80},
}

// brailleLevels is the number of dots of a braille column, the height of a full bar.
const brailleLevels = 4

// brailleBar returns the bits of a bar of level dots (0-4), filled from the bottom, in the given column (0, 1).
func brailleBar(level int, column int) rune {
	var bits rune
	for i := 0; i < min(level, brailleLevels); i++ {
		bits |= brailleDot[brailleLevels-1-i][column]
	}
	return bits
}

// Sparkline renders values as a bar chart of braille characters, at most width cells wide: every cell has two
// columns of dots, so it shows two values, each with up to four dots. Negative values are gaps (no dots), all other
// values have at least one dot, so a zero is distinguishable from a gap. If there are more values than dot columns,
// a column shows the maximum of the values it covers. cell returns the cell of the value with the given index.
func Sparkline(values []int64, width int) (cells []rune, cell func(index int) int) {
	if width <= 0 || len(values) == 0 {
		return nil, func(int) int { return 0 }
	}
	columns := min(len(values), width*2)
	column := func(index int) int {
		return min(index*columns/len(values), columns-1)
	}
	cell = func(index int) int {
		return column(index) / 2
	}

	buckets := make([]int64, columns)
	for i := range buckets {
		buckets[i] = -1
	}
	var maxValue int64
	for index, value := range values {
		bucket := column(index)
		buckets[bucket] = max(buckets[bucket], value)
		maxValue = max(maxValue, value)
	}

	cells = make([]rune, (columns+1)/2)
	for i := range cells {
		cells[i] = brailleBlank
	}
	for i, value := range buckets {
		if value < 0 {
			continue
		}
		level := 1
		if maxValue > 0 {
			level = 1 + int(value*(brailleLevels-1)/maxValue)
		}
		cells[i/2] |= brailleBar(level, i%2)
	}
	return cells, cell
}

// DrawSparkline draws the sparkline of values (see Sparkline) at x, y, at most width cells wide, and highlights the
// cell of the value with index selected (-1: none) with a background, so it is visible where the sparkline has no
// dots as well. Returns the width that was drawn.
func DrawSparkline(screen tcell.Screen, x int, y int, width int, values []int64, selected int) int {
	cells, cellOf := Sparkline(values, width)
	selectedCell := -1
	if selected >= 0 && selected < len(values) {
		selectedCell = cellOf(selected)
	}
	colors := theme.Colors.Sparkline
	style := tcell.StyleDefault.Background(tview.Styles.PrimitiveBackgroundColor).Foreground(colors.Graph)
	for i, r := range cells {
		cellStyle := style
		if i == selectedCell {
			cellStyle = cellStyle.Foreground(colors.Selected).Background(colors.SelectedBackground)
		}
		screen.SetContent(x+i, y, r, nil, cellStyle)
	}
	return len(cells)
}
