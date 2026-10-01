package ui

import (
	"testing"
	"zfs-file-history/internal/ui/util"

	"github.com/stretchr/testify/assert"
)

func TestAdjacentPage(t *testing.T) {
	two := []util.Page{Main, Dataset}
	assert.Equal(t, Dataset, adjacentPage(two, Main, false))
	assert.Equal(t, Main, adjacentPage(two, Dataset, false))
	assert.Equal(t, Dataset, adjacentPage(two, Main, true))
	assert.Equal(t, Main, adjacentPage(two, Dataset, true))

	three := []util.Page{"a", "b", "c"}
	assert.Equal(t, util.Page("b"), adjacentPage(three, "a", false))
	assert.Equal(t, util.Page("a"), adjacentPage(three, "c", false))
	assert.Equal(t, util.Page("c"), adjacentPage(three, "a", true))
	assert.Equal(t, util.Page("b"), adjacentPage(three, "c", true))

	// unknown pages fall back to the first page
	assert.Equal(t, Main, adjacentPage(two, HelpDialog, false))
}
