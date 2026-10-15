package dialog

import (
	"errors"
	"sync"
	"testing"
	"time"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testPoolFeatures are features as zfs.ListPoolFeatures sorts them: the ones that can be enabled first.
func testPoolFeatures() *zfs.PoolFeatures {
	return &zfs.PoolFeatures{
		Pool:          "tank",
		Compatibility: "grub2",
		Compatible:    map[string]bool{"bookmarks": true, "large_blocks": true},
		Features: []zfs.PoolFeature{
			{Name: "bookmarks", Description: "\"zfs bookmark\" command", ReadOnlyCompatible: true, State: zfs.PoolFeatureDisabled},
			{Name: "large_blocks", Description: "Support for blocks larger than 128KB.", State: zfs.PoolFeatureDisabled},
			{Name: "zstd_compress", Description: "zstd compression algorithm support.", State: zfs.PoolFeatureDisabled, Unavailable: "not allowed by compatibility=grub2"},
			{Name: "async_destroy", Description: "Destroy filesystems asynchronously.", State: zfs.PoolFeatureEnabled, Unavailable: "already enabled"},
		},
	}
}

type poolFeaturesDialogTest struct {
	t      *testing.T
	app    *tview.Application
	screen tcell.SimulationScreen
	pages  *tview.Pages
	dialog *PoolFeaturesDialog

	mu      sync.Mutex
	enabled [][]string
}

func newPoolFeaturesDialogTest(t *testing.T, features *zfs.PoolFeatures) *poolFeaturesDialogTest {
	dt := &poolFeaturesDialogTest{t: t}
	dt.app, dt.screen = testutil.NewApp(testutil.WithSize(100, 30))
	dt.pages = tview.NewPages().AddPage("main", tview.NewBox(), true, true)
	testutil.StartApp(t, dt.app, dt.pages)
	dt.dialog = NewPoolFeaturesDialog(dt.app, features, func(features []string) {
		assert.True(t, testutil.IsOnUiThread(dt.app))
		dt.mu.Lock()
		defer dt.mu.Unlock()
		dt.enabled = append(dt.enabled, features)
	})
	testutil.OnUiThread(t, dt.app, func() { ShowDialogOnPages(dt.app, dt.pages, dt.dialog, nil) })
	testutil.WaitForText(t, dt.app, dt.screen, "Pool Features", 2*time.Second)
	return dt
}

func (dt *poolFeaturesDialogTest) press(key tcell.Key, r rune) {
	testutil.Press(dt.t, dt.app, dt.screen, key, r)
}

func (dt *poolFeaturesDialogTest) checked() (names []string) {
	testutil.OnUiThread(dt.t, dt.app, func() { names = dt.dialog.Checked() })
	return names
}

func (dt *poolFeaturesDialogTest) getEnabled() [][]string {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	return append([][]string{}, dt.enabled...)
}

func TestPoolFeaturesDialog_ChooseAndEnable(t *testing.T) {
	dt := newPoolFeaturesDialogTest(t, testPoolFeatures())

	text := testutil.ScreenText(t, dt.app, dt.screen)
	assert.Contains(t, text, "Support for blocks larger than 128KB.")
	assert.Contains(t, text, "not allowed by compatibility=grub2", "the reason instead of the description")
	assert.Contains(t, text, "already enabled")
	assert.Contains(t, text, "bookmarks: \"zfs bookmark\" command (read-only compatible)", "the details of the selected one")

	dt.press(tcell.KeyRune, ' ')
	assert.Equal(t, []string{"bookmarks"}, dt.checked())
	dt.press(tcell.KeyDown, 0)
	dt.press(tcell.KeyRune, ' ')
	assert.Equal(t, []string{"bookmarks", "large_blocks"}, dt.checked())
	dt.press(tcell.KeyRune, ' ')
	assert.Equal(t, []string{"bookmarks"}, dt.checked(), "unchecked again")

	dt.press(tcell.KeyEnter, 0)
	require.Eventually(t, func() bool { return len(dt.getEnabled()) == 1 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, [][]string{{"bookmarks"}}, dt.getEnabled())
	testutil.WaitForNoPage(t, dt.app, dt.pages, string(PoolFeaturesDialogPage), 2*time.Second)
}

// The features that cannot be enabled cannot be checked, neither with the keys nor with the mouse. They can be
// selected, which shows why.
func TestPoolFeaturesDialog_UnavailableCannotBeChecked(t *testing.T) {
	dt := newPoolFeaturesDialogTest(t, testPoolFeatures())

	dt.press(tcell.KeyDown, 0)
	dt.press(tcell.KeyRune, ' ')
	assert.Equal(t, []string{"large_blocks"}, dt.checked())
	dt.press(tcell.KeyDown, 0)
	dt.press(tcell.KeyRune, ' ')
	assert.Equal(t, []string{"large_blocks"}, dt.checked())
	testutil.WaitForText(t, dt.app, dt.screen, "Cannot be enabled: not allowed by compatibility=grub2.", 2*time.Second)

	testutil.ClickText(t, dt.app, dt.screen, "zstd_compress")
	testutil.ClickText(t, dt.app, dt.screen, "async_destroy")
	assert.Equal(t, []string{"large_blocks"}, dt.checked())

	testutil.ClickText(t, dt.app, dt.screen, "bookmarks")
	assert.Equal(t, []string{"bookmarks", "large_blocks"}, dt.checked(), "a click checks one that can be enabled")
}

func TestPoolFeaturesDialog_UnavailableAreGrayedOut(t *testing.T) {
	dt := newPoolFeaturesDialogTest(t, testPoolFeatures())
	testutil.ScreenText(t, dt.app, dt.screen)

	x, y, found := testutil.FindInScreen(dt.screen, "zstd_compress")
	require.True(t, found)
	_, style, _ := dt.screen.Get(x, y)
	foreground, _, _ := style.Decompose()
	assert.Equal(t, theme.Colors.Dialog.Unavailable, foreground)
	x, y, found = testutil.FindInScreen(dt.screen, "large_blocks")
	require.True(t, found)
	_, style, _ = dt.screen.Get(x, y)
	foreground, _, _ = style.Decompose()
	assert.NotEqual(t, theme.Colors.Dialog.Unavailable, foreground)
}

func TestPoolFeaturesDialog_EnterWithoutChecked(t *testing.T) {
	dt := newPoolFeaturesDialogTest(t, testPoolFeatures())

	dt.press(tcell.KeyEnter, 0)

	testutil.WaitForText(t, dt.app, dt.screen, "Check the features to enable", 2*time.Second)
	assert.True(t, testutil.HasPage(t, dt.app, dt.pages, string(PoolFeaturesDialogPage)))
	assert.Empty(t, dt.getEnabled())
}

func TestPoolFeaturesDialog_Esc(t *testing.T) {
	dt := newPoolFeaturesDialogTest(t, testPoolFeatures())
	dt.press(tcell.KeyRune, ' ')

	dt.press(tcell.KeyEscape, 0)

	testutil.WaitForNoPage(t, dt.app, dt.pages, string(PoolFeaturesDialogPage), 2*time.Second)
	assert.Empty(t, dt.getEnabled())
}

func TestPoolFeaturesDialog_NothingToEnable(t *testing.T) {
	features := testPoolFeatures()
	features.Features = features.Features[2:]
	dt := newPoolFeaturesDialogTest(t, features)

	testutil.WaitForText(t, dt.app, dt.screen, "Cannot be enabled: not allowed by compatibility=grub2.", 2*time.Second)
	dt.press(tcell.KeyRune, ' ')
	dt.press(tcell.KeyDown, 0)
	dt.press(tcell.KeyRune, ' ')
	assert.Empty(t, dt.checked())
}

func TestFormatPoolFeatures(t *testing.T) {
	features := testPoolFeatures()
	text := testutil.StripTags(formatPoolFeatures(features))
	assert.Contains(t, text, "of the pool tank. This cannot be undone")
	assert.Contains(t, text, "See zpool-features(7).")
	assert.Contains(t, text, "limited to the features of compatibility=grub2")

	features.Compatible = nil
	features.CompatibilityErr = errors.New("the compatibility file grub2 is not in /etc")
	assert.Contains(t, testutil.StripTags(formatPoolFeatures(features)), "compatibility=grub2, which cannot be read: the compatibility file grub2 is not in /etc")

	features.CompatibilityErr = nil
	features.Compatibility = "off"
	assert.NotContains(t, testutil.StripTags(formatPoolFeatures(features)), "compatibility")
}

func TestFormatPoolFeatures_ModuleLacksFeatures(t *testing.T) {
	features := testPoolFeatures()
	features.ModuleFeatures = map[string]bool{"bookmarks": true}
	features.ToolsVersion, features.ModuleVersion = "zfs-2.4.4-1", "zfs-kmod-2.3.0-1"
	assert.Contains(t, testutil.StripTags(formatPoolFeatures(features)),
		"The loaded kernel module (zfs-kmod-2.3.0-1) does not support all features of the zfs tools (zfs-2.4.4-1).")

	features.ToolsVersion, features.ModuleVersion = "", ""
	assert.Contains(t, testutil.StripTags(formatPoolFeatures(features)), "The loaded kernel module does not support all features of the zfs tools.")

	features.ModuleFeatures = nil
	assert.NotContains(t, formatPoolFeatures(features), "kernel module", "not known")
}

// A feature the installed ZFS does not know is shown with its GUID, as enabled, and why it cannot be enabled.
func TestPoolFeaturesDialog_Unsupported(t *testing.T) {
	features := testPoolFeatures()
	features.Features = append(features.Features, zfs.PoolFeature{Name: "org.example:new", State: zfs.PoolFeatureUnsupportedReadOnly,
		Unsupported: true, ReadOnlyCompatible: true, Unavailable: "not supported by the installed ZFS, in use: the pool can only be imported read-only"})
	dt := newPoolFeaturesDialogTest(t, features)

	for range len(features.Features) - 1 {
		dt.press(tcell.KeyDown, 0)
	}
	dt.press(tcell.KeyRune, ' ')

	testutil.WaitForText(t, dt.app, dt.screen, "org.example:new: a feature the installed ZFS does not know", 2*time.Second)
	assert.Contains(t, testutil.ScreenText(t, dt.app, dt.screen), "not supported by the installed ZFS, in use")
	assert.Empty(t, dt.checked())
	testutil.OnUiThread(t, dt.app, func() {
		assert.Contains(t, dt.dialog.table.GetCell(len(features.Features)-1, 0).Text, switchOn, "enabled")
	})
}

func TestFormatEnablePoolFeatures(t *testing.T) {
	text := testutil.StripTags(formatEnablePoolFeatures("tank", []string{"bookmarks", "large_blocks"}))
	assert.Contains(t, text, "Enable the features bookmarks, large_blocks of the pool tank")
	assert.Contains(t, text, "This cannot be undone")
}
