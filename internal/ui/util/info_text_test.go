package util

import (
	"strings"
	"testing"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInfoText(t *testing.T) {
	var info InfoText
	info.Verdict(InfoPart{Text: "mounted"}, InfoPart{Text: "92% full", State: InfoWarning})
	info.Section("Space")
	info.Field("Data", InfoValue("1 GiB"))
	info.Field("Permissions", InfoValue("a, b,\nc"))

	lines := info.Lines()
	plain := make([]string, len(lines))
	for i, line := range lines {
		plain[i] = testutil.StripTags(line)
	}
	assert.Equal(t, []string{
		"● 92% full · mounted",
		"",
		"Space",
		"Data         1 GiB",
		"Permissions  a, b,",
		"             c",
	}, plain, "problems first, the labels aligned, further lines of a value below its first one")
	// the dot in the color of the worst part, the problem in its color
	assert.True(t, strings.HasPrefix(lines[0], txwidgets.ColorTag(theme.Colors.Info.Warning)+"●"))
	assert.Contains(t, lines[0], txwidgets.Span(theme.Colors.Info.Warning, "%s", "92% full"))
	// the labels dim
	assert.True(t, strings.HasPrefix(lines[3], txwidgets.Span(theme.Colors.Info.Label, "Data")))
}

func TestInfoBar(t *testing.T) {
	width := func(bar string) int { return tview.TaggedStringWidth(bar) }
	assert.Equal(t, 10, width(InfoBar(0, 10)))
	assert.Equal(t, 10, width(InfoBar(0.57, 10)))
	assert.Equal(t, 10, width(InfoBar(1, 10)))
	assert.Equal(t, 10, width(InfoBar(2, 10)), "at most full")
	assert.Contains(t, InfoBar(0.5, 10), strings.Repeat("█", 5))
	assert.Contains(t, InfoBar(0.01, 10), "▏", "something used shows")
	assert.NotContains(t, InfoBar(0, 10), "▏")
}

func TestInfoStackedBar(t *testing.T) {
	// as RGB, like the style tags of the bar
	empty, red, blue := theme.Colors.Info.BarEmpty.Hex(), tcell.ColorRed.Hex(), tcell.ColorBlue.Hex()
	// cells returns the blocks of the bar with the text and background color of each
	type cell struct {
		block      string
		foreground int32
		background int32
	}
	cells := func(bar string) []cell {
		screen := tcell.NewSimulationScreen("UTF-8")
		require.NoError(t, screen.Init())
		screen.SetSize(20, 1)
		tview.Print(screen, bar, 0, 0, 20, tview.AlignLeft, tcell.ColorWhite)
		var result []cell
		for x := range tview.TaggedStringWidth(bar) {
			text, style, _ := screen.Get(x, 0)
			foreground, background, _ := style.Decompose()
			result = append(result, cell{text, foreground.Hex(), background.Hex()})
		}
		return result
	}

	bar := cells(InfoStackedBar([]BarSegment{{Fraction: 0.2, Color: tcell.ColorRed}, {Fraction: 0.35, Color: tcell.ColorBlue}}, 4))
	assert.Equal(t, []cell{
		// 0.2 of 32 eighths: ends after 6 of the first cell
		{"▊", red, blue},
		{"█", blue, blue},
		// 0.55: ends after 2 of the third cell
		{"▎", blue, empty},
		{" ", empty, empty},
	}, bar)

	bar = cells(InfoStackedBar([]BarSegment{{Fraction: 0.001, Color: tcell.ColorRed}, {Fraction: 0, Color: tcell.ColorGreen}, {Fraction: 1, Color: tcell.ColorBlue}}, 2))
	assert.Equal(t, []cell{{"▏", red, blue}, {"█", blue, blue}}, bar, "a tiny segment shows, the rest at most fills the bar")
}
