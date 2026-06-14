package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsBlank(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "empty string",
			input:    "",
			expected: true,
		},
		{
			name:     "only spaces",
			input:    "   ",
			expected: true,
		},
		{
			name:     "tabs and newlines",
			input:    "\t\n ",
			expected: true,
		},
		{
			name:     "non-blank string",
			input:    "hello",
			expected: false,
		},
		{
			name:     "string with spaces",
			input:    "  hello  ",
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsBlank(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}
