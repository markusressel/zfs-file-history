package txwidgets

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/stretchr/testify/assert"
)

func TestSpanAndColorTag(t *testing.T) {
	tag := ColorTag(tcell.ColorRed)
	assert.Equal(t, "[#ff0000]", tag)

	span := Span(tcell.ColorGreen, "value: %d", 42)
	assert.Equal(t, "[#008000]value: 42[-]", span)
}
