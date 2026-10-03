package util

import (
	"math"
	"zfs-file-history/internal/ui/theme"

	"github.com/gdamore/tcell/v2"
)

// MagnitudeScale maps sizes to their position between the smallest and the largest non-zero size of a list (0 to 1),
// logarithmically: sizes are very uneven (one snapshot may hold GiBs, most only KiBs), so every order of magnitude
// gets its own step instead of everything but the largest looking tiny.
type MagnitudeScale struct {
	logMin float64
	logMax float64
}

// NewMagnitudeScale returns the scale of the given sizes. Zeros are not part of it.
func NewMagnitudeScale(sizes []uint64) MagnitudeScale {
	scale := MagnitudeScale{logMin: math.Inf(1), logMax: math.Inf(-1)}
	for _, size := range sizes {
		if size == 0 {
			continue
		}
		value := math.Log(float64(size))
		scale.logMin = min(scale.logMin, value)
		scale.logMax = max(scale.logMax, value)
	}
	return scale
}

// Position returns the position of size on the scale, from 0 (the smallest, or less) to 1 (the largest, or more).
// 0 if all sizes are the same, as nothing stands out then.
func (scale MagnitudeScale) Position(size uint64) float64 {
	if size == 0 || !(scale.logMax > scale.logMin) {
		return 0
	}
	position := (math.Log(float64(size)) - scale.logMin) / (scale.logMax - scale.logMin)
	return min(max(position, 0), 1)
}

// SizeColor returns the color of a size in a size column: dimmed for 0, otherwise by how big it is on the scale.
func (scale MagnitudeScale) SizeColor(size uint64, fallback tcell.Color) tcell.Color {
	if size == 0 {
		return theme.Colors.Layout.Table.ZeroSize
	}
	return scale.MagnitudeColor(size, fallback)
}

// MagnitudeColor returns the color of a size on the scale, see theme.Colors.Magnitude.
func (scale MagnitudeScale) MagnitudeColor(size uint64, fallback tcell.Color) tcell.Color {
	return GradientColor(theme.Colors.Magnitude, scale.Position(size), fallback)
}

// GradientColor returns the color of the gradient at position (0 to 1), mixing the stops around it, like the
// graphs of fan2go-tui. fallback without stops.
func GradientColor(stops []theme.GradientStop, position float64, fallback tcell.Color) tcell.Color {
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
