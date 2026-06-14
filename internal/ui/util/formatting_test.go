package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStableLengthHumanizedBytes(t *testing.T) {
	tests := []struct {
		input    uint64
		expected string
	}{
		{0, "     0   B"},
		{1024, "   1.0 KiB"},
		{1024 * 1024, "   1.0 MiB"},
		{1024 * 1024 * 1024, "   1.0 GiB"},
		{500, "   500   B"},
		{1000, "  1000   B"}, // humanize.IBytes uses power of 2, so 1000 is still B
		{1023, "  1023   B"},
		{9999, "   9.8 KiB"},
		{100 * 1024 * 1024 * 1024 * 1024, "   100 TiB"},
	}

	for _, tt := range tests {
		t.Run(string(tt.expected), func(t *testing.T) {
			assert.Equal(t, tt.expected, StableLengthHumanizedBytes(tt.input))
		})
	}
}
