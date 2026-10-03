package util

import (
	"testing"
	"zfs-file-history/internal/ui/theme"

	"github.com/gdamore/tcell/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBrailleBar(t *testing.T) {
	assert.Equal(t, rune(0), brailleBar(0, 0))
	assert.Equal(t, '⡀', brailleBlank|brailleBar(1, 0), "the bottom dot")
	assert.Equal(t, '⡇', brailleBlank|brailleBar(4, 0), "the whole left column")
	assert.Equal(t, '⢸', brailleBlank|brailleBar(4, 1), "the whole right column")
	assert.Zero(t, brailleBar(4, 0)&brailleBar(4, 1), "the columns do not overlap")
}

func TestSparkline(t *testing.T) {
	// two values per cell: zero (one dot) and the maximum (four dots), then a gap and a value in between
	cells, cell := Sparkline([]int64{0, 9, -1, 4}, 10)
	assert.Equal(t, "⣸⢠", string(cells), "⡀+⢸, then nothing+⢠ (4 of 9: two dots)")
	assert.Equal(t, 0, cell(1))
	assert.Equal(t, 1, cell(3))

	// activity: one dot unchanged, four dots changed
	cells, _ = Sparkline([]int64{0, 1, 0, 0}, 10)
	assert.Equal(t, "⣸⣀", string(cells))

	// more values than dot columns: the maximum of each column (8 1 | 1 8)
	cells, cell = Sparkline([]int64{1, 8, 1, 1, 1, 1, 1, 8}, 2)
	assert.Equal(t, "⣇⣸", string(cells))
	assert.Equal(t, 0, cell(1))
	assert.Equal(t, 1, cell(7))

	cells, _ = Sparkline(nil, 10)
	assert.Empty(t, cells)
}

func TestDrawSparkline(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, screen.Init())
	screen.SetSize(20, 1)

	// five values: three cells, the third value (index 2) is in the second cell
	drawn := DrawSparkline(screen, 2, 0, 10, []int64{1, 2, 3, 4, -1}, 2)
	assert.Equal(t, 3, drawn)
	for i := 0; i < drawn; i++ {
		r, _, style, _ := screen.GetContent(2+i, 0)
		assert.True(t, r >= 0x2800 && r <= 0x28ff, "braille in cell %d", i)
		foreground, background, _ := style.Decompose()
		if i == 1 {
			assert.Equal(t, theme.Colors.Sparkline.Selected, foreground)
			assert.Equal(t, theme.Colors.Sparkline.SelectedBackground, background)
		} else {
			assert.Equal(t, theme.Colors.Sparkline.Graph, foreground)
		}
	}

	// limited to the width, no selection
	assert.Equal(t, 2, DrawSparkline(screen, 0, 0, 2, []int64{1, 2, 3, 4, 5, 6, 7, 8}, -1))
	assert.Zero(t, DrawSparkline(screen, 0, 0, 10, nil, 0))
}
