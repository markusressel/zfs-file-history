package util

import (
	"testing"
	"zfs-file-history/internal/ui/theme"

	"github.com/gdamore/tcell/v2"
	"github.com/stretchr/testify/assert"
)

func TestMagnitudeScale(t *testing.T) {
	// 1 KiB to 1 GiB, and a zero that is not part of the scale
	scale := NewMagnitudeScale([]uint64{0, 1 << 10, 1 << 20, 1 << 30})

	assert.Equal(t, 0.0, scale.Position(1<<10), "the smallest")
	assert.InDelta(t, 0.5, scale.Position(1<<20), 1e-9, "logarithmic: 1 MiB is in the middle between 1 KiB and 1 GiB")
	assert.Equal(t, 1.0, scale.Position(1<<30), "the largest")
	assert.Equal(t, 0.0, scale.Position(0))
	assert.Equal(t, 0.0, scale.Position(1), "below the smallest")
	assert.Equal(t, 1.0, scale.Position(1<<40), "above the largest")

	// nothing stands out
	assert.Equal(t, 0.0, NewMagnitudeScale([]uint64{5, 5, 0}).Position(5))
	assert.Equal(t, 0.0, NewMagnitudeScale(nil).Position(5))

	stops := theme.Colors.Magnitude
	assert.Equal(t, stops[0].Color, scale.MagnitudeColor(1<<10, tcell.ColorWhite))
	assert.Equal(t, stops[len(stops)-1].Color, scale.MagnitudeColor(1<<30, tcell.ColorWhite))
}
