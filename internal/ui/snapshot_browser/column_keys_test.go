package snapshot_browser

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The keys identify the columns in the saved table layout, see table.RowSelectionTable.BindColumnLayout.
func TestColumnKeysAreUnique(t *testing.T) {
	keys := map[string]bool{}
	for _, column := range tableColumns {
		assert.NotEmpty(t, column.Key, column.Title)
		assert.False(t, keys[column.Key], "duplicate key %q", column.Key)
		keys[column.Key] = true
	}
}
