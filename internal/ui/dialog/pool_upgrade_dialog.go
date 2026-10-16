package dialog

import (
	"fmt"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	"zfs-file-history/internal/ui/util"

	"github.com/rivo/tview"
)

const PoolUpgradeDialogPage util.Page = "PoolUpgradeDialog"

// zpoolFeaturesUrl is the manual page of the features of pools (zpool-features(7)) on the website of OpenZFS.
const zpoolFeaturesUrl = "https://openzfs.github.io/openzfs-docs/man/master/7/zpool-features.7.html"

// NewPoolUpgradeDialog explains that upgrading the pool (zfs.PoolActionUpgrade) cannot be undone, and calls
// onConfirm on the UI thread once the name of the pool was typed (see NewConfirmByNameDialog).
func NewPoolUpgradeDialog(application *tview.Application, pool string, onConfirm func()) *TextInputDialog {
	return NewConfirmByNameDialog(
		application,
		string(PoolUpgradeDialogPage),
		" ⬆️ Upgrade Pool ",
		formatPoolUpgrade(pool),
		pool,
		onConfirm,
	)
}

// formatPoolUpgrade tells what upgrading the pool does, and what it may break for good.
func formatPoolUpgrade(pool string) string {
	return fmt.Sprintf("Enable all features the installed ZFS supports on the pool %s (zpool upgrade). The data of "+
		"the pool stays as it is.\n\nThis %s: features cannot be disabled again. The pool may no longer be accessible "+
		"by software that does not support them, e.g. an older version of ZFS, another operating system or a rescue "+
		"system. If a boot loader reads the pool (e.g. GRUB with /boot on it), the system may no longer boot. See "+
		"%s for details.",
		Highlight(pool), txwidgets.Span(theme.Colors.Dialog.Danger, "%s", "cannot be undone"),
		Link("zpool-features(7)", zpoolFeaturesUrl))
}
