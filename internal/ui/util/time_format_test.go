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

// Relative times are updated every second while one of them counts seconds, otherwise every minute.
func TestRelativeTimesOutdated(t *testing.T) {
	store := state.Load(filepath.Join(t.TempDir(), "state.json"))
	state.Current = store
	t.Cleanup(func() {
		_ = store.Flush()
		state.Current = nil
		relativeTimes.Store(false)
		newestFormattedTime.Store(0)
	})
	InitTimeFormat()
	ToggleRelativeTimes()
	now := time.Now()
	justNow := now.Add(-time.Second)

	assert.False(t, relativeTimesOutdated(now, justNow), "nothing shown")
	assert.True(t, relativeTimesOutdated(now, now.Add(-time.Minute)), "every minute")

	FormatTime(now.Add(-time.Hour))
	assert.False(t, relativeTimesOutdated(now, justNow), "only old times shown")

	FormatTime(now.Add(-30 * time.Second))
	assert.True(t, relativeTimesOutdated(now, justNow), "counts seconds")
	assert.True(t, relativeTimesOutdated(now.Add(30*time.Second), justNow), `becomes "1 minute ago"`)
	assert.False(t, relativeTimesOutdated(now.Add(32*time.Second), justNow), "counts minutes")

	// the tables record their times again after a refresh
	generation := TimeFormatGeneration()
	refreshRelativeTimes()
	assert.NotEqual(t, generation, TimeFormatGeneration())
	assert.False(t, relativeTimesOutdated(now, justNow))

	// absolute times are not recorded
	ToggleRelativeTimes()
	FormatTime(now)
	assert.False(t, relativeTimesOutdated(now, justNow))
}
