package dialog

import (
	"errors"
	"fmt"
	"zfs-file-history/internal/zfs"

	"github.com/rivo/tview"
)

// NewSuccessDialog creates a generic dialog for successful operations.
func NewSuccessDialog(application *tview.Application, title string, message string) *SelectionDialog {
	return NewSelectionDialog(
		application,
		"SuccessDialog",
		fmt.Sprintf(" ✅ %s ", title),
		message,
		[]*DialogOption{
			{
				Id:   DialogCloseActionId,
				Name: "OK",
			},
		},
		nil,
		nil,
	)
}

// NewErrorDialog creates a generic dialog for failed operations.
// If ZFS permissions are missing, it explains which ones and offers to grant them, see NewMissingPermissionsDialog.
func NewErrorDialog(application *tview.Application, title string, err error) *SelectionDialog {
	return NewErrorDialogWithRetry(application, title, err, nil)
}

// NewErrorDialogWithRetry is NewErrorDialog for operations that can be repeated: if ZFS permissions are missing,
// retry is called (on the UI thread) once they were granted. retry may be nil.
func NewErrorDialogWithRetry(application *tview.Application, title string, err error, retry func()) *SelectionDialog {
	var missing *zfs.MissingPermissionsError
	if errors.As(err, &missing) {
		return NewMissingPermissionsDialog(application, missing, retry)
	}
	return NewSelectionDialog(
		application,
		"ErrorDialog",
		fmt.Sprintf(" ❌ %s ", title),
		err.Error(),
		[]*DialogOption{
			{
				Id:       DialogCloseActionId,
				Name:     "Close",
				Severity: DialogSeverityDanger, // Assuming this makes it red based on your theme
			},
		},
		nil,
		nil,
	)
}
