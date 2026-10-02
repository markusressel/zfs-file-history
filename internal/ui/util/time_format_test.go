package util

import (
	"path/filepath"
	"testing"
	"time"
	"zfs-file-history/internal/state"
	"zfs-file-history/internal/ui/theme"

	"github.com/stretchr/testify/assert"
)

func TestTimeFormat(t *testing.T) {
	store := state.Load(filepath.Join(t.TempDir(), "state.json"))
	state.Current = store
	t.Cleanup(func() {
		_ = store.Flush()
		state.Current = nil
		relativeTimes.Store(false)
	})
	then := time.Now().Add(-3 * time.Minute)

	InitTimeFormat()
	assert.False(t, IsRelativeTimes(), "absolute by default")
	assert.Equal(t, then.Format(theme.Style.Format.DateTime), FormatTime(then))

	generation := TimeFormatGeneration()
	ToggleRelativeTimes()
	assert.True(t, IsRelativeTimes())
	assert.Equal(t, "3 minutes ago", FormatTime(then))
	assert.NotEqual(t, generation, TimeFormatGeneration(), "tables render again")
	assert.True(t, store.Toggle(toggleRelativeTimes, false), "remembered")

	// loaded on the next start
	relativeTimes.Store(false)
	InitTimeFormat()
	assert.True(t, IsRelativeTimes())
}
