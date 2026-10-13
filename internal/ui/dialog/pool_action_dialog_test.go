package dialog

import (
	"testing"
	"zfs-file-history/internal/zfs"

	"github.com/stretchr/testify/assert"
)

func TestBuildPoolDialogOptions(t *testing.T) {
	trimming := []*zfs.PoolDevice{{Name: "nvme0n1", State: zfs.PoolStateOnline, Trim: "(12% trimmed, started at Tue Oct 13 10:00:01 2026)"}}
	tests := map[string]struct {
		status   *zfs.PoolStatus
		expected []DialogActionId
	}{
		"not known": {nil, []DialogActionId{
			PoolDialogScrubActionId, PoolDialogTrimActionId, PoolDialogClearActionId, DialogCloseActionId}},
		"idle": {&zfs.PoolStatus{Scan: "scrub repaired 0B in 00:06:46 with 0 errors on Thu Oct  1 12:50:53 2026", ScanProgress: -1}, []DialogActionId{
			PoolDialogScrubActionId, PoolDialogTrimActionId, PoolDialogClearActionId, DialogCloseActionId}},
		"scrubbing": {&zfs.PoolStatus{Scan: "scrub in progress since Tue Oct 13 10:00:01 2026", ScanProgress: 10}, []DialogActionId{
			PoolDialogPauseScrubActionId, PoolDialogCancelScrubActionId, PoolDialogTrimActionId, PoolDialogClearActionId, DialogCloseActionId}},
		"paused": {&zfs.PoolStatus{Scan: "scrub paused since Tue Oct 13 10:00:01 2026", ScanProgress: -1}, []DialogActionId{
			PoolDialogScrubActionId, PoolDialogCancelScrubActionId, PoolDialogTrimActionId, PoolDialogClearActionId, DialogCloseActionId}},
		// zpool refuses to scrub while resilvering
		"resilvering": {&zfs.PoolStatus{Scan: "resilver in progress since Tue Oct 13 09:12:44 2026", ScanProgress: 30}, []DialogActionId{
			PoolDialogTrimActionId, PoolDialogClearActionId, DialogCloseActionId}},
		"trimming": {&zfs.PoolStatus{ScanProgress: -1, Devices: trimming}, []DialogActionId{
			PoolDialogScrubActionId, PoolDialogCancelTrimActionId, PoolDialogClearActionId, DialogCloseActionId}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, test.expected, optionIds(buildPoolDialogOptions(test.status)))
		})
	}
}

func TestBuildPoolDialogOptions_ResumeScrub(t *testing.T) {
	options := buildPoolDialogOptions(&zfs.PoolStatus{Scan: "scrub paused since Tue Oct 13 10:00:01 2026", ScanProgress: -1})
	assert.Equal(t, "▶️ Resume scrub", options[0].Name)
}

func TestPoolActionOf(t *testing.T) {
	expected := map[DialogActionId]zfs.PoolAction{
		PoolDialogScrubActionId:       zfs.PoolActionScrub,
		PoolDialogPauseScrubActionId:  zfs.PoolActionPauseScrub,
		PoolDialogCancelScrubActionId: zfs.PoolActionCancelScrub,
		PoolDialogTrimActionId:        zfs.PoolActionTrim,
		PoolDialogCancelTrimActionId:  zfs.PoolActionCancelTrim,
		PoolDialogClearActionId:       zfs.PoolActionClear,
	}
	for id, action := range expected {
		actual, ok := PoolActionOf(id)
		assert.True(t, ok)
		assert.Equal(t, action, actual)
	}
	_, ok := PoolActionOf(DialogCloseActionId)
	assert.False(t, ok)
}
