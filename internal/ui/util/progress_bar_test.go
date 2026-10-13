package util

import (
	"testing"
	"zfs-file-history/internal/testutil"

	"github.com/stretchr/testify/assert"
)

func TestFormatProgressBar(t *testing.T) {
	assert.Equal(t, "░░░░░░░░░░ 0.0%", testutil.StripTags(FormatProgressBar(0, 10)))
	assert.Equal(t, "█████░░░░░ 50.0%", testutil.StripTags(FormatProgressBar(50, 10)))
	assert.Equal(t, "█████████░ 99.9%", testutil.StripTags(FormatProgressBar(99.9, 10)))
	assert.Equal(t, "██████████ 100.0%", testutil.StripTags(FormatProgressBar(100, 10)))
	// clamped
	assert.Equal(t, "░░░░░░░░░░ 0.0%", testutil.StripTags(FormatProgressBar(-5, 10)))
	assert.Equal(t, "██████████ 100.0%", testutil.StripTags(FormatProgressBar(150, 10)))
	assert.Equal(t, " 50.0%", testutil.StripTags(FormatProgressBar(50, -3)))
}
