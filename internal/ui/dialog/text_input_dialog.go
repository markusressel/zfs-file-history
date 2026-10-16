package dialog

import (
	"strings"
	"unicode/utf8"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	"zfs-file-history/internal/ui/util"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TextInputDialog asks for a single line of text, e.g. a name.
// The input field behaves like a terminal input line (cursor movement, ctrl+a/e/u/k/w, ...).
type TextInputDialog struct {
	name          string
	layout        *tview.Flex
	inputField    *tview.InputField
	errorView     *tview.TextView
	actionChannel chan DialogActionId
	onSubmit      func(text string)
	// validate checks the text before it is submitted, see SetValidator. May be nil.
	validate func(text string) error
	// frame is the bordered window of the dialog
	frame *tview.Flex
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
	// leave room for editing longer names than the initial text
	d.createLayout(title, description, false, initialText, utf8.RuneCountInString(initialText)+10)
	return d
}

// createLayout creates the layout. With tagged, the description contains style tags (anything else escaped).
func (d *TextInputDialog) createLayout(title string, description string, tagged bool, initialText string, inputWidth int) {
	// the size is calculated from the text as shown
	plainDescription := description
	if tagged {
		plainDescription = withoutTags(description)
	}
	helpText := txwidgets.Span(theme.Colors.ShortcutMap.KeyCombo, "%s", shortcut_helper.KeyEnter) +
		txwidgets.Span(theme.Colors.ShortcutMap.Name, ": confirm  ") +
		txwidgets.Span(theme.Colors.ShortcutMap.KeyCombo, "%s", shortcut_helper.KeyEsc) +
		txwidgets.Span(theme.Colors.ShortcutMap.Name, ": cancel")

	constraints := DialogSizeConstraints{
		Title:             title,
		Description:       plainDescription,
		ExtraContentWidth: inputWidth,
		StaticHeight:      4, // spacer, input field, spacer, help text
	}
	dialogWidth, _ := CalculateDialogSize(constraints)
	textLineWidth := dialogWidth - 6
	if textLineWidth < 5 {
		textLineWidth = 5
	}

	descriptionView := tview.NewTextView().
		SetDynamicColors(tagged).
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

	// the line between the input field and the help shows validation errors, see SetValidator
	d.errorView = tview.NewTextView().SetTextColor(theme.Colors.Dialog.Error)
	d.inputField.SetChangedFunc(func(string) { d.errorView.SetText("") })

	// A click on the text does not focus it: the keys (typing, Esc) would no longer reach the input field.
	content := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(unfocusableTextView{descriptionView}, calculateWrappedHeight(plainDescription, textLineWidth), 0, false).
		AddItem(unfocusableTextView{tview.NewTextView()}, 1, 0, false).
		AddItem(d.inputField, 1, 0, true).
		AddItem(unfocusableTextView{d.errorView}, 1, 0, false).
		AddItem(unfocusableTextView{helpView}, 1, 0, false)
	// like the other dialogs; CalculateDialogSize leaves room for it
	content.SetBorderPadding(0, 0, 1, 1)

	d.layout, d.frame = createModalWithFrame(title, content, constraints)
}

// SetValidator sets a check for the (trimmed) text: if it returns an error, the error is shown in the dialog and the
// text is not submitted, so it can be corrected. The error disappears when the text is edited.
func (d *TextInputDialog) SetValidator(validate func(text string) error) *TextInputDialog {
	d.validate = validate
	return d
}

// submit closes the dialog and passes the text on, unless it is empty or invalid.
func (d *TextInputDialog) submit() {
	text := strings.TrimSpace(d.inputField.GetText())
	if text == "" {
		return
	}
	if d.validate != nil {
		if err := d.validate(text); err != nil {
			d.errorView.SetText(err.Error())
			return
		}
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

// unfocusableTextView is a text view a click does not focus (see util.WithoutFocus), e.g. the description of a dialog
// whose input field has to keep the focus.
type unfocusableTextView struct {
	*tview.TextView
}

func (view unfocusableTextView) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
	return util.WithoutFocus(view.TextView.MouseHandler())
}
