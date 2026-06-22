package dialog

import (
	"unicode/utf8"

	"github.com/rivo/tview"
)

type SelectionDialog struct {
	application   *tview.Application
	name          string
	title         string
	description   string
	options       []*DialogOption
	layout        *tview.Flex
	actionChannel chan DialogActionId
}

func NewSelectionDialog(
	application *tview.Application,
	name string,
	title string,
	description string,
	options []*DialogOption,
) *SelectionDialog {
	d := &SelectionDialog{
		application:   application,
		name:          name,
		title:         title,
		description:   description,
		options:       options,
		actionChannel: make(chan DialogActionId),
	}
	d.createLayout()
	return d
}

func (d *SelectionDialog) createLayout() {
	maxOptWidth := 0
	for _, opt := range d.options {
		prefixLen := 2 // E.g. "1."
		if opt.Id == DialogCloseActionId {
			prefixLen = 4 // E.g. "Esc."
		}
		optWidth := prefixLen + 1 + utf8.RuneCountInString(opt.Name)
		if optWidth > maxOptWidth {
			maxOptWidth = optWidth
		}
	}

	actualTableRows := len(d.options)
	if len(d.options) > 1 {
		hasClose := false
		for _, opt := range d.options {
			if opt.Id == DialogCloseActionId {
				hasClose = true
				break
			}
		}
		if hasClose {
			actualTableRows++
		}
	}

	dialogWidth, dialogHeight := CalculateDialogSize(DialogSizeConstraints{
		Title:             d.title,
		Description:       d.description,
		ExtraContentWidth: maxOptWidth,
		StaticHeight:      1 + actualTableRows, // 1 line for the spacer box
	})

	textLineWidth := dialogWidth - 6
	if textLineWidth < 5 {
		textLineWidth = 5
	}
	descHeight := calculateWrappedHeight(d.description, textLineWidth)

	textDescriptionView := tview.NewTextView().
		SetText(d.description).
		SetWrap(true).
		SetWordWrap(true)

	optionTable := createOptionTable(d.application, d.options, d.selectAction)

	dialogContent := tview.NewFlex().SetDirection(tview.FlexRow)
	dialogContent.AddItem(textDescriptionView, descHeight, 0, false)
	dialogContent.AddItem(tview.NewBox(), 1, 0, false) // 1 line spacer/padding
	dialogContent.AddItem(optionTable, actualTableRows, 0, true)

	dialog := createModal(d.title, dialogContent, dialogWidth, dialogHeight)
	dialog.SetInputCapture(createOptionDialogInputCapture(optionTable, d.options, d.selectAction, d.Close))
	d.layout = dialog
}

func (d *SelectionDialog) GetName() string {
	return d.name
}

func (d *SelectionDialog) GetLayout() *tview.Flex {
	return d.layout
}

func (d *SelectionDialog) GetActionChannel() <-chan DialogActionId {
	return d.actionChannel
}

func (d *SelectionDialog) Close() {
	emitDialogActions(d.actionChannel, DialogCloseActionId)
}

func (d *SelectionDialog) selectAction(option *DialogOption) {
	emitDialogActions(d.actionChannel, DialogCloseActionId, option.Id)
}
