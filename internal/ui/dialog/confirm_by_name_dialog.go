package dialog

import (
	"errors"
	"fmt"
	"unicode/utf8"
	"zfs-file-history/internal/ui/theme"

	"github.com/rivo/tview"
)

// NewConfirmByNameDialog asks to type expected (e.g. the full name of what is destroyed) to confirm an action that
// loses data, with a red border. The description may contain style tags, e.g. to highlight what is lost: escape everything else (see
// tview.Escape and txwidgets.Span). There is no option that confirms, so it cannot happen by accident (Enter, a click, a repeated key).
// onConfirm is called on the UI thread once the typed text matches exactly, after the dialog was closed; Esc closes it
// without.
func NewConfirmByNameDialog(
	application *tview.Application,
	name string,
	title string,
	description string,
	expected string,
	onConfirm func(),
) *TextInputDialog {
	d := &TextInputDialog{
		name:          name,
		actionChannel: make(chan DialogActionId),
		onSubmit:      func(string) { onConfirm() },
		validate: func(text string) error {
			if text != expected {
				return errors.New("the name does not match")
			}
			return nil
		},
	}
	description = fmt.Sprintf("%s\n\nType %s to confirm:", description, Highlight(expected))
	d.createLayout(title, description, true, "", utf8.RuneCountInString(expected)+10)
	d.frame.SetBorderColor(theme.Colors.Dialog.DangerBorder)
	return d
}
