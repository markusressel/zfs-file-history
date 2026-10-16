package dialog

import (
	"strings"
	"testing"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

func TestBuildPoolDialogOptions(t *testing.T) {
	trimming := []*zfs.PoolDevice{{Name: "nvme0n1", State: zfs.PoolStateOnline, Trim: "(12% trimmed, started at Tue Oct 13 10:00:01 2026)"}}
	tests := map[string]struct {
		status   *zfs.PoolStatus
		expected []DialogActionId
	}{
		"not known": {nil, []DialogActionId{
			PoolDialogScrubActionId, PoolDialogTrimActionId, PoolDialogClearActionId, PoolDialogFeaturesActionId, PoolDialogDestroyActionId, DialogCloseActionId}},
		"idle": {&zfs.PoolStatus{Scan: "scrub repaired 0B in 00:06:46 with 0 errors on Thu Oct  1 12:50:53 2026", ScanProgress: -1}, []DialogActionId{
			PoolDialogScrubActionId, PoolDialogTrimActionId, PoolDialogClearActionId, PoolDialogFeaturesActionId, PoolDialogDestroyActionId, DialogCloseActionId}},
		"scrubbing": {&zfs.PoolStatus{Scan: "scrub in progress since Tue Oct 13 10:00:01 2026", ScanProgress: 10}, []DialogActionId{
			PoolDialogPauseScrubActionId, PoolDialogCancelScrubActionId, PoolDialogTrimActionId, PoolDialogClearActionId, PoolDialogFeaturesActionId, PoolDialogDestroyActionId, DialogCloseActionId}},
		"paused": {&zfs.PoolStatus{Scan: "scrub paused since Tue Oct 13 10:00:01 2026", ScanProgress: -1}, []DialogActionId{
			PoolDialogScrubActionId, PoolDialogCancelScrubActionId, PoolDialogTrimActionId, PoolDialogClearActionId, PoolDialogFeaturesActionId, PoolDialogDestroyActionId, DialogCloseActionId}},
		// zpool refuses to scrub while resilvering
		"resilvering": {&zfs.PoolStatus{Scan: "resilver in progress since Tue Oct 13 09:12:44 2026", ScanProgress: 30}, []DialogActionId{
			PoolDialogTrimActionId, PoolDialogClearActionId, PoolDialogFeaturesActionId, PoolDialogDestroyActionId, DialogCloseActionId}},
		"trimming": {&zfs.PoolStatus{ScanProgress: -1, Devices: trimming}, []DialogActionId{
			PoolDialogScrubActionId, PoolDialogCancelTrimActionId, PoolDialogClearActionId, PoolDialogFeaturesActionId, PoolDialogDestroyActionId, DialogCloseActionId}},
		// zpool status suggests it
		"upgradable": {&zfs.PoolStatus{ScanProgress: -1, Action: "Enable all features using 'zpool upgrade'."}, []DialogActionId{
			PoolDialogScrubActionId, PoolDialogTrimActionId, PoolDialogClearActionId, PoolDialogUpgradeActionId, PoolDialogFeaturesActionId, PoolDialogDestroyActionId, DialogCloseActionId}},
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

// Upgrading cannot be undone: it is shown in red, and not run by the dialog (see TestPoolActionOf).
func TestBuildPoolDialogOptions_Upgrade(t *testing.T) {
	options := buildPoolDialogOptions(&zfs.PoolStatus{ScanProgress: -1, Action: "Enable all features using 'zpool upgrade'."})
	upgrade := options[len(options)-4]
	assert.Equal(t, PoolDialogUpgradeActionId, upgrade.Id)
	assert.Equal(t, DialogSeverityDanger, upgrade.Severity)
	assert.Contains(t, upgrade.Description, "cannot be undone")
}

// Enabling features cannot be undone: it is shown in red, and not run by the dialog (see TestPoolActionOf).
func TestBuildPoolDialogOptions_Features(t *testing.T) {
	options := buildPoolDialogOptions(nil)
	features := options[len(options)-3]
	assert.Equal(t, PoolDialogFeaturesActionId, features.Id)
	assert.Equal(t, DialogSeverityDanger, features.Severity)
	assert.Contains(t, features.Description, "cannot be undone")
}

// Destroying a pool loses all its data: it is shown in red, and not run by the dialog (see TestPoolActionOf).
func TestBuildPoolDialogOptions_Destroy(t *testing.T) {
	options := buildPoolDialogOptions(nil)
	destroy := options[len(options)-2]
	assert.Equal(t, PoolDialogDestroyActionId, destroy.Id)
	assert.Equal(t, DialogSeverityDanger, destroy.Severity)
	assert.Contains(t, destroy.Description, "typing its name")
}

func TestFormatPoolUpgrade(t *testing.T) {
	text := testutil.StripTags(formatPoolUpgrade("tank"))
	assert.Contains(t, text, "the pool tank (zpool upgrade)")
	assert.Contains(t, text, "This cannot be undone")
	assert.Contains(t, text, "may no longer be accessible by software that does not support them")
	assert.Contains(t, text, "may no longer boot")
	assert.Contains(t, text, "See zpool-features(7) for details.")
	assert.Contains(t, formatPoolUpgrade("tank"), "[::u:"+zpoolFeaturesUrl+"]zpool-features(7)[::U:-]")
}

// The link is shown as its text, underlined.
func TestLink(t *testing.T) {
	view := tview.NewTextView().SetDynamicColors(true).SetText("see " + Link("a[b]", "https://example.com/x") + " here")
	screen := testutil.DrawToScreen(t, view, 30, 1)
	assert.Equal(t, "see a[b] here", strings.TrimSpace(testutil.ScreenLines(screen)[0]))
	_, style, _ := screen.Get(4, 0)
	assert.NotEqual(t, tcell.UnderlineStyleNone, style.GetUnderlineStyle())
	_, style, _ = screen.Get(10, 0)
	assert.Equal(t, tcell.UnderlineStyleNone, style.GetUnderlineStyle(), "only the link")
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
	_, ok = PoolActionOf(PoolDialogUpgradeActionId)
	assert.False(t, ok, "confirmed by name first")
	_, ok = PoolActionOf(PoolDialogFeaturesActionId)
	assert.False(t, ok, "chosen and confirmed by name first")
	_, ok = PoolActionOf(PoolDialogDestroyActionId)
	assert.False(t, ok, "previewed and confirmed by name first")
}
