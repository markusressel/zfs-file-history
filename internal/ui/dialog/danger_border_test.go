package dialog

import (
	"testing"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/theme"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

// The confirmations of actions that lose data have a red border, other dialogs do not.
func TestDangerBorder(t *testing.T) {
	app, _ := testutil.NewApp()
	borderOf := func(frame *tview.Flex) tcell.Color { return frame.GetBorderColor() }

	assert.Equal(t, theme.Colors.Dialog.DangerBorder, borderOf(NewConfirmByNameDialog(app, "n", "t", "d", "pool/x", func() {}).frame))
	assert.Equal(t, theme.Colors.Dialog.DangerBorder, borderOf(NewDestroySnapshotsDialog(app, "d", nil, nil).frame))
	assert.Equal(t, theme.Colors.Dialog.DangerBorder, borderOf(NewDestroyDatasetDialog(app, "d", "also", nil, nil).frame))
	assert.Equal(t, theme.Colors.Dialog.DangerBorder, borderOf(NewDestroyPoolDialog(app, "d", "lost", nil, nil).frame))

	assert.Equal(t, theme.Colors.Dialog.Border, borderOf(NewTextInputDialog(app, "n", "t", "d", "x", nil).frame))
	assert.Equal(t, theme.Colors.Dialog.Border, borderOf(NewPromoteCloneDialog(app, "d", nil, nil).frame))
	assert.Equal(t, theme.Colors.Dialog.Border, borderOf(NewSuccessDialog(app, "t", "m").frame))
}
