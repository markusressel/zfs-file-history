package dialog

import (
	"fmt"

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
	)
}

// NewErrorDialog creates a generic dialog for failed operations.
func NewErrorDialog(application *tview.Application, title string, err error) *SelectionDialog {
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
	)
}
