package dialog

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCalculateWrappedHeight(t *testing.T) {
	tests := []struct {
		name         string
		text         string
		maxLineWidth int
		expected     int
	}{
		{
			name:         "empty text",
			text:         "",
			maxLineWidth: 10,
			expected:     1,
		},
		{
			name:         "single line fitting fully",
			text:         "hello",
			maxLineWidth: 10,
			expected:     1,
		},
		{
			name:         "single line wrapping once",
			text:         "hello world", // 11 runes
			maxLineWidth: 10,
			expected:     2,
		},
		{
			name:         "single line wrapping twice",
			text:         "hello world wide web", // 20 runes
			maxLineWidth: 8,
			expected:     3,
		},
		{
			name:         "multiple lines with empty lines",
			text:         "line1\n\nline2",
			maxLineWidth: 10,
			expected:     3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			height := calculateWrappedHeight(tt.text, tt.maxLineWidth)
			assert.Equal(t, tt.expected, height)
		})
	}
}

func TestCalculateDialogSize(t *testing.T) {
	tests := []struct {
		name        string
		constraints DialogSizeConstraints
		minWidth    int
		maxWidth    int
	}{
		{
			name: "short title and description",
			constraints: DialogSizeConstraints{
				Title:             "Short Title",
				Description:       "Short Description",
				ExtraContentWidth: 10,
				StaticHeight:      3,
			},
			minWidth: 40,
			maxWidth: 80,
		},
		{
			name: "long description wrapping",
			constraints: DialogSizeConstraints{
				Title:             "Short Title",
				Description:       "This is a very long description that should wrap multiple times on a typical screen layout size constraint.",
				ExtraContentWidth: 20,
				StaticHeight:      5,
			},
			minWidth: 40,
			maxWidth: 80,
		},
		{
			name: "large extra content width",
			constraints: DialogSizeConstraints{
				Title:             "Title",
				Description:       "",
				ExtraContentWidth: 90,
				StaticHeight:      4,
			},
			minWidth: 40,
			maxWidth: 80,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, h := CalculateDialogSize(tt.constraints)
			assert.GreaterOrEqual(t, w, tt.minWidth)
			assert.GreaterOrEqual(t, h, 2+tt.constraints.StaticHeight)
		})
	}
}
