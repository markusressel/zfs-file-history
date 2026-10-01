package dialog

import (
	"strings"
	"unicode/utf8"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TextInputDialog asks for a single line of text, e.g. a name.
// The input field behaves like a terminal input line (cursor movement, ctrl+a/e/u/k/w, ...).
type TextInputDialog struct {
	name          string
	layout        *tview.Flex
	inputField    *tview.InputField
	actionChannel chan DialogActionId
	onSubmit      func(text string)
}

// NewTextInputDialog creates a dialog that asks for a single line of text, prefilled with initialText.
// Enter submits the (trimmed) text if it is not empty and closes the dialog, Esc closes it without submitting.
// onSubmit is called on the UI thread, so slow work must be started in the background from there.
func NewTextInputDialog(
	application *tview.Application,
	name string,
	title string,
	description string,
	initialText string,
	onSubmit func(text string),
) *TextInputDialog {
	d := &TextInputDialog{
		name:          name,
		actionChannel: make(chan DialogActionId),
		onSubmit:      onSubmit,
	}
	d.createLayout(title, description, initialText)
	return d
}

func (d *TextInputDialog) createLayout(title string, description string, initialText string) {
	helpText := txwidgets.Span(theme.Colors.ShortcutMap.KeyCombo, "enter") +
		txwidgets.Span(theme.Colors.ShortcutMap.Name, ": confirm  ") +
		txwidgets.Span(theme.Colors.ShortcutMap.KeyCombo, "esc") +
		txwidgets.Span(theme.Colors.ShortcutMap.Name, ": cancel")

	// leave room for editing longer names than the initial text
	inputWidth := utf8.RuneCountInString(initialText) + 10
	constraints := DialogSizeConstraints{
		Title:             title,
		Description:       description,
		ExtraContentWidth: inputWidth,
		StaticHeight:      4, // spacer, input field, spacer, help text
	}
	dialogWidth, _ := CalculateDialogSize(constraints)
	textLineWidth := dialogWidth - 6
	if textLineWidth < 5 {
		textLineWidth = 5
	}

	descriptionView := tview.NewTextView().
		SetText(description).
		SetWrap(true).
		SetWordWrap(true)

	d.inputField = tview.NewInputField().
		SetText(initialText).
		SetFieldBackgroundColor(theme.Colors.Layout.Table.HeaderBackground).
		SetFieldTextColor(theme.Colors.Layout.Table.HeaderForeground)
	d.inputField.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			d.submit()
		case tcell.KeyEscape:
			d.Close()
		}
	})

	helpView := tview.NewTextView().
		SetDynamicColors(true).
		SetText(helpText)

	content := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(descriptionView, calculateWrappedHeight(description, textLineWidth), 0, false).
		AddItem(tview.NewBox(), 1, 0, false).
		AddItem(d.inputField, 1, 0, true).
		AddItem(tview.NewBox(), 1, 0, false).
		AddItem(helpView, 1, 0, false)

	d.layout = createModal(title, content, constraints)
}

// submit closes the dialog and passes the text on, unless it is empty.
func (d *TextInputDialog) submit() {
	text := strings.TrimSpace(d.inputField.GetText())
	if text == "" {
		return
	}
	d.Close()
	if d.onSubmit != nil {
		d.onSubmit(text)
	}
}

// GetText returns the current text of the input field.
func (d *TextInputDialog) GetText() string {
	return d.inputField.GetText()
}

func (d *TextInputDialog) GetName() string {
	return d.name
}

func (d *TextInputDialog) GetLayout() *tview.Flex {
	return d.layout
}

func (d *TextInputDialog) GetActionChannel() <-chan DialogActionId {
	return d.actionChannel
}

// Close closes the dialog (when shown via ShowDialogOnPages).
func (d *TextInputDialog) Close() {
	go func() {
		d.actionChannel <- DialogCloseActionId
	}()
}
