package util

import (
	"math"
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
	cells, _, cell = sparkline(values, width)
	return cells, cell
}

// sparkline renders values like Sparkline, and also returns the height of each cell: the higher of its values
// relative to the highest value (0 to 1), -1 for cells without values.
func sparkline(values []int64, width int) (cells []rune, heights []float64, cell func(index int) int) {
	if width <= 0 || len(values) == 0 {
		return nil, nil, func(int) int { return 0 }
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
	heights = make([]float64, len(cells))
	for i := range cells {
		cells[i] = brailleBlank
		heights[i] = -1
	}
	for i, value := range buckets {
		if value < 0 {
			continue
		}
		level, height := 1, 0.0
		if maxValue > 0 {
			level = 1 + int(value*(brailleLevels-1)/maxValue)
			height = float64(value) / float64(maxValue)
		}
		cells[i/2] |= brailleBar(level, i%2)
		heights[i/2] = max(heights[i/2], height)
	}
	return cells, heights, cell
}

// DrawSparkline draws the sparkline of values (see Sparkline) at x, y, at most width cells wide, colored by height
// with theme.Colors.Sparkline.Gradient (a cell has the color of the higher of its values). The cell of the value with
// index selected (-1: none) is highlighted with a background, so it is visible where the sparkline has no dots as
// well. Returns the width that was drawn.
func DrawSparkline(screen tcell.Screen, x int, y int, width int, values []int64, selected int) int {
	cells, heights, cellOf := sparkline(values, width)
	selectedCell := -1
	if selected >= 0 && selected < len(values) {
		selectedCell = cellOf(selected)
	}
	colors := theme.Colors.Sparkline
	style := tcell.StyleDefault.Background(tview.Styles.PrimitiveBackgroundColor)
	for i, r := range cells {
		cellStyle := style.Foreground(gradientColor(colors.Gradient, heights[i], colors.Graph))
		if i == selectedCell {
			cellStyle = cellStyle.Foreground(colors.Selected).Background(colors.SelectedBackground)
		}
		screen.SetContent(x+i, y, r, nil, cellStyle)
	}
	return len(cells)
}

// gradientColor returns the color of the gradient at position (0 to 1), mixing the stops around it, like the
// graphs of fan2go-tui. fallback without stops.
func gradientColor(stops []theme.GradientStop, position float64, fallback tcell.Color) tcell.Color {
	if len(stops) == 0 {
		return fallback
	}
	if position <= stops[0].Position {
		return stops[0].Color
	}
	for i := 1; i < len(stops); i++ {
		previous, next := stops[i-1], stops[i]
		if position <= next.Position {
			if next.Position <= previous.Position {
				return next.Color
			}
			return mixColors(previous.Color, next.Color, (position-previous.Position)/(next.Position-previous.Position))
		}
	}
	return stops[len(stops)-1].Color
}

// mixColors returns the color between a (t = 0) and b (t = 1).
func mixColors(a tcell.Color, b tcell.Color, t float64) tcell.Color {
	t = min(max(t, 0), 1)
	ar, ag, ab := a.RGB()
	br, bg, bb := b.RGB()
	mix := func(from int32, to int32) int32 {
		return int32(math.Round(float64(from) + float64(to-from)*t))
	}
	return tcell.NewRGBColor(mix(ar, br), mix(ag, bg), mix(ab, bb))
}
