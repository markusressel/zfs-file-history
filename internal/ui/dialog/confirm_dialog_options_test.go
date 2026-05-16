package dialog

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildConfirmDialogOptions_WithConfirmAction(t *testing.T) {
	t.Parallel()

	options := buildConfirmDialogOptions(DeleteFileDialogDeleteFileActionId, "Delete", true)

	assert.Equal(t,
		[]DialogActionId{DeleteFileDialogDeleteFileActionId, DialogCloseActionId},
		optionIds(options),
	)
	assert.Equal(t, "Delete", options[0].Name)
	assert.Equal(t, "Cancel", options[1].Name)
}

func TestBuildConfirmDialogOptions_WithoutConfirmAction(t *testing.T) {
	t.Parallel()

	options := buildConfirmDialogOptions(DeleteFileDialogDeleteFileActionId, "Delete", false)

	assert.Equal(t, []DialogActionId{DialogCloseActionId}, optionIds(options))
	assert.Equal(t, "Cancel", options[0].Name)
}
