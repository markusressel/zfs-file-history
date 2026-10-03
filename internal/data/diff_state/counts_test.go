package diff_state

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCounts(t *testing.T) {
	var counts Counts
	for _, state := range []DiffState{Added, Added, Deleted, Modified, Equal, Equal, Equal, Unknown} {
		counts.Add(state)
	}
	assert.Equal(t, Counts{Added: 2, Deleted: 1, Modified: 1, Equal: 3, Unknown: 1}, counts)
}
