package dialog

import (
	"fmt"
	"zfs-file-history/internal/ui/localization"
	"zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/rivo/tview"
)

const (
	PoolActionDialogPage util.Page = "PoolActionDialog"

	PoolDialogScrubActionId DialogActionId = iota
	PoolDialogPauseScrubActionId
	PoolDialogCancelScrubActionId
	PoolDialogTrimActionId
	PoolDialogCancelTrimActionId
	PoolDialogClearActionId
	PoolDialogUpgradeActionId
	PoolDialogFeaturesActionId
)

// NewPoolActionDialog creates the dialog with the actions for a pool. status is the status of the pool (nil if
// not known), it decides which actions make sense, e.g. pausing a scrub only while one runs. Destroying or exporting
// the pool is deliberately not offered, see zfs.PoolAction. Upgrading it and enabling features are not run by the
// dialog (see PoolActionOf), but confirmed with NewPoolUpgradeDialog or chosen with NewPoolFeaturesDialog first.
func NewPoolActionDialog(
	application *tview.Application,
	pool string,
	status *zfs.PoolStatus,
	asyncWork func(d *SelectionDialog, action DialogActionId) error,
	onComplete func(d *SelectionDialog, option *DialogOption, err error),
) *SelectionDialog {
	return newTaggedSelectionDialog(
		application,
		string(PoolActionDialogPage),
		localization.LocalizationSelectActionDialogTitle,
		fmt.Sprintf("What do you want to do with pool %s?", Highlight(pool)),
		nil,
		buildPoolDialogOptions(status),
		asyncWork,
		onComplete,
	)
}

// buildPoolDialogOptions returns the options of the pool action dialog for the status of the pool (nil: not
// known), see NewPoolActionDialog.
func buildPoolDialogOptions(status *zfs.PoolStatus) []*DialogOption {
	scrubbing, resilvering, paused, trimming := false, false, false, false
	if status != nil {
		scrubbing = status.IsScanning() && status.ScanKind() == "scrub"
		resilvering = status.IsScanning() && status.ScanKind() == "resilver"
		paused = status.IsScrubPaused()
		trimming = status.IsTrimming()
	}

	var dialogOptions []*DialogOption
	switch {
	case scrubbing:
		dialogOptions = append(dialogOptions, &DialogOption{
			Id:          PoolDialogPauseScrubActionId,
			Name:        "⏸️ Pause scrub",
			Description: "Stop the scrub for now, starting it again resumes it (zpool scrub -p)",
		})
	case paused:
		dialogOptions = append(dialogOptions, &DialogOption{
			Id:          PoolDialogScrubActionId,
			Name:        "▶️ Resume scrub",
			Description: "Continue the paused scrub (zpool scrub)",
		})
	case !resilvering:
		// zpool refuses to scrub while resilvering, which checks all data anyway
		dialogOptions = append(dialogOptions, &DialogOption{
			Id:          PoolDialogScrubActionId,
			Name:        "🔍 Start scrub",
			Description: "Read and verify all data of the pool, and repair what can be repaired (zpool scrub)",
		})
	}
	if scrubbing || paused {
		dialogOptions = append(dialogOptions, &DialogOption{
			Id:          PoolDialogCancelScrubActionId,
			Name:        "⏹️ Cancel scrub",
			Description: "Stop the scrub, the next one starts from the beginning (zpool scrub -s)",
			Severity:    DialogSeverityWarning,
		})
	}
	if trimming {
		dialogOptions = append(dialogOptions, &DialogOption{
			Id:          PoolDialogCancelTrimActionId,
			Name:        "⏹️ Cancel TRIM",
			Description: "Stop the running TRIM (zpool trim -c)",
		})
	} else {
		dialogOptions = append(dialogOptions, &DialogOption{
			Id:          PoolDialogTrimActionId,
			Name:        "✂️ Start TRIM",
			Description: "Tell the devices which blocks are unused, e.g. SSDs (zpool trim)",
		})
	}
	dialogOptions = append(dialogOptions,
		&DialogOption{
			Id:          PoolDialogClearActionId,
			Name:        "🩹 Clear errors",
			Description: "Reset the error counters of the devices, and retry the ones that failed (zpool clear)",
		},
	)
	if status != nil && status.CanUpgrade() {
		dialogOptions = append(dialogOptions, &DialogOption{
			Id:          PoolDialogUpgradeActionId,
			Name:        "⬆️ Upgrade pool",
			Description: "Enable all features ZFS supports, cannot be undone (zpool upgrade)",
			Severity:    DialogSeverityDanger,
		})
	}
	dialogOptions = append(dialogOptions, &DialogOption{
		Id:          PoolDialogFeaturesActionId,
		Name:        "🧩 Enable features",
		Description: "Choose features of the pool to enable, cannot be undone (zpool set feature@…=enabled)",
		Severity:    DialogSeverityDanger,
	})
	dialogOptions = append(dialogOptions, &DialogOption{
		Id:   DialogCloseActionId,
		Name: localization.LocalizationCommonClose,
	})

	return dialogOptions
}

// PoolActionOf returns the zfs action of an option of the pool action dialog that runs right away, false for others
// (e.g. close, or upgrade, which is confirmed first, or the features, which are listed first).
func PoolActionOf(id DialogActionId) (zfs.PoolAction, bool) {
	actions := map[DialogActionId]zfs.PoolAction{
		PoolDialogScrubActionId:       zfs.PoolActionScrub,
		PoolDialogPauseScrubActionId:  zfs.PoolActionPauseScrub,
		PoolDialogCancelScrubActionId: zfs.PoolActionCancelScrub,
		PoolDialogTrimActionId:        zfs.PoolActionTrim,
		PoolDialogCancelTrimActionId:  zfs.PoolActionCancelTrim,
		PoolDialogClearActionId:       zfs.PoolActionClear,
	}
	action, ok := actions[id]
	return action, ok
}
