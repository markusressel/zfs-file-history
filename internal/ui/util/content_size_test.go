package util

import (
	"testing"

	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

func TestTextSize(t *testing.T) {
	width, height := TextSize("")
	assert.Equal(t, [2]int{0, 0}, [2]int{width, height})

	// the style tags are not counted, nor the trailing line break
	width, height = TextSize("[red]abc[-]\nabcdef\n")
	assert.Equal(t, [2]int{6, 2}, [2]int{width, height})

	// wide characters count twice, empty lines count
	width, height = TextSize("日本\n\nx")
	assert.Equal(t, [2]int{4, 3}, [2]int{width, height})
}

func TestTextViewContentSize(t *testing.T) {
	textView := tview.NewTextView().SetDynamicColors(true)
	width, height := TextViewContentSize(textView)
	assert.Equal(t, [2]int{0, 0}, [2]int{width, height}, "no text")

	textView.SetText("[green]ONLINE[-]\nab\n")
	width, height = TextViewContentSize(textView)
	assert.Equal(t, [2]int{6 + 2, 2 + 2}, [2]int{width, height}, "with the border")
}

func TestFitContent(t *testing.T) {
	assert.Equal(t, 0, FitContent(100, 0, 60), "no content")
	assert.Equal(t, 20, FitContent(100, 20, 60))
	assert.Equal(t, 60, FitContent(100, 80, 60), "at most the share")
}
