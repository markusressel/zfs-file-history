package scrollbar

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBarUnits(t *testing.T) {
	tests := []struct {
		name                        string
		units, max, position, width int
		expectedStart, expectedEnd  int
	}{
		{name: "all shown", units: 6, max: 5, position: 0, width: 5, expectedStart: 0, expectedEnd: 6},
		{name: "nothing to show", units: 6, max: 0, position: 0, width: 0, expectedStart: 0, expectedEnd: 6},
		{name: "top", units: 6, max: 20, position: 0, width: 5, expectedStart: 0, expectedEnd: 2},
		// Regression: the bar ended in half a cell at the end, but in a whole one at the top.
		{name: "end", units: 6, max: 20, position: 15, width: 5, expectedStart: 4, expectedEnd: 6},
		{name: "middle", units: 6, max: 20, position: 7, width: 5, expectedStart: 2, expectedEnd: 4},
		{name: "past the end", units: 6, max: 20, position: 100, width: 5, expectedStart: 4, expectedEnd: 6},
		{name: "at least a cell", units: 10, max: 1000, position: 0, width: 5, expectedStart: 0, expectedEnd: 2},
		{name: "half of it shown", units: 10, max: 20, position: 10, width: 10, expectedStart: 5, expectedEnd: 10},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			start, end := barUnits(test.units, 0, test.max, test.position, test.width)
			assert.Equal(t, test.expectedStart, start)
			assert.Equal(t, test.expectedEnd, end)
		})
	}
}
