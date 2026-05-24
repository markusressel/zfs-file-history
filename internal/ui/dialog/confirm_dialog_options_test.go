package dialog

import (
	"testing"
	"zfs-file-history/internal/ui/localization"

	"github.com/stretchr/testify/assert"
)

func TestBuildConfirmDialogOptions_WithConfirmAction(t *testing.T) {
	t.Parallel()

	options := buildConfirmDialogOptions(DeleteFileDialogDeleteFileActionId, localization.LocalizationCommonDelete, true, DialogSeverityDanger)

	assert.Equal(t,
		[]DialogActionId{DeleteFileDialogDeleteFileActionId, DialogCloseActionId},
		optionIds(options),
	)
	assert.Equal(t, localization.LocalizationCommonDelete, options[0].Name)
	assert.Equal(t, localization.LocalizationCommonCancel, options[1].Name)
}

func TestBuildConfirmDialogOptions_WithoutConfirmAction(t *testing.T) {
	t.Parallel()

	options := buildConfirmDialogOptions(DeleteFileDialogDeleteFileActionId, localization.LocalizationCommonDelete, false, DialogSeverityDanger)

	assert.Equal(t, []DialogActionId{DialogCloseActionId}, optionIds(options))
	assert.Equal(t, "Cancel", options[0].Name)
}
