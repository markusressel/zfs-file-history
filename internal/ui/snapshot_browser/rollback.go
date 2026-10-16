package snapshot_browser

import (
	"fmt"
	"strings"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/dialog"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"
)

// previewRollback and rollback are replaceable in tests, so tests never roll anything back.
var (
	previewRollback = zfs.PreviewRollback
	rollback        = zfs.Rollback
)

// previewAndConfirmRollback lists in the background what rolling back to the snapshot (full name) loses, and then
// asks for confirmation. Meanwhile, a dialog says that it is checked, and that nothing is changed yet; it can be
// cancelled. Must be called on the UI thread.
func (snapshotBrowser *SnapshotBrowserComponent) previewAndConfirmRollback(snapshot string) {
	dialog.RunPreview(snapshotBrowser.application, snapshotBrowser.showDialogWithoutCallback, dialog.Preview[*zfs.RollbackPreview]{
		Title: " ⏪ Roll Back ",
		Message: fmt.Sprintf("Checking what rolling back to %s would lose: the newer snapshots, their clones and holds, "+
			"and the permissions. Nothing is changed yet.", snapshot),
		ErrorTitle: "Cannot Roll Back", Subject: snapshot,
		Check:   func() (*zfs.RollbackPreview, error) { return previewRollback(snapshot) },
		Confirm: snapshotBrowser.showRollbackConfirmation,
		Retry:   func() { snapshotBrowser.previewAndConfirmRollback(snapshot) },
	})
}

// formatRollbackDescription describes what rolling back loses, based on the preview, with style tags: what is lost
// stands out. It suggests cloning, which keeps everything.
func formatRollbackDescription(preview *zfs.RollbackPreview) string {
	dataset, name, _ := strings.Cut(preview.Snapshot, "@")
	danger := func(text string) string { return txwidgets.Span(theme.Colors.Dialog.Danger, "%s", text) }
	var description strings.Builder
	fmt.Fprintf(&description, "Roll back %s to snapshot %s.\n\n", dialog.Highlight(dataset), dialog.Highlight(name))
	fmt.Fprintf(&description, "All changes to the dataset since the snapshot %s, this cannot be undone. "+
		"Programs that have files of the dataset open may see them change.", danger("are lost"))
	if len(preview.Destroyed) > 0 {
		fmt.Fprintf(&description, "\n\nThe newer snapshots (and bookmarks) %s as well (%d):", danger("are destroyed"), len(preview.Destroyed))
		for i, destroyed := range preview.Destroyed {
			if i == maxListedDestroyed {
				fmt.Fprintf(&description, "\n  … and %d more", len(preview.Destroyed)-maxListedDestroyed)
				break
			}
			fmt.Fprintf(&description, "\n  %s", dialog.Highlight(destroyed))
		}
	}
	description.WriteString("\n\nTo keep everything, clone the snapshot instead (Clone in the actions of the snapshot): " +
		"its state becomes a new dataset, next to this one. Promoting the clone makes it the main one, also without " +
		"losing anything.")
	return description.String()
}

// showRollbackConfirmation shows what rolling back loses, and rolls back exactly as previewed once the full name of
// the snapshot is typed. Must be called on the UI thread.
func (snapshotBrowser *SnapshotBrowserComponent) showRollbackConfirmation(preview *zfs.RollbackPreview) {
	confirmation := dialog.NewConfirmByNameDialog(
		snapshotBrowser.application,
		"RollbackDialog",
		" ⏪ Roll Back ",
		formatRollbackDescription(preview),
		preview.Snapshot,
		func() { snapshotBrowser.rollbackInBackground(preview) },
	)
	snapshotBrowser.showDialog(confirmation, nil)
}

// rollbackInBackground rolls back as confirmed in the background and shows the result. Must be called on the UI
// thread.
func (snapshotBrowser *SnapshotBrowserComponent) rollbackInBackground(preview *zfs.RollbackPreview) {
	go func() {
		err := rollback(preview)
		snapshotBrowser.application.QueueUpdateDraw(func() {
			if err != nil {
				logging.Error("Failed to roll back to %s: %s", preview.Snapshot, err.Error())
				// a retry asks for confirmation again, with a new preview
				retry := func() { snapshotBrowser.previewAndConfirmRollback(preview.Snapshot) }
				snapshotBrowser.showDialog(dialog.NewErrorDialogWithRetry(snapshotBrowser.application, "Rollback Failed", err, retry), nil)
			} else {
				message := fmt.Sprintf("Rolled back to '%s'.", preview.Snapshot)
				if count := len(preview.Destroyed); count > 0 {
					message += fmt.Sprintf(" Destroyed %d newer %s.", count, uiutil.Plural(count, "snapshot", "snapshots"))
				}
				snapshotBrowser.showDialog(dialog.NewSuccessDialog(snapshotBrowser.application, "Rolled Back", message), nil)
				snapshotBrowser.emit(RolledBackEvent{Snapshot: preview.Snapshot})
			}
			// newer snapshots may be gone, also after an error
			zfs.RefreshZfsData()
		})
	}()
}
