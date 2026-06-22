package dialog

import "github.com/rivo/tview"

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
	width int,
	height int,
) *SelectionDialog {
	d := &SelectionDialog{
		application:   application,
		name:          name,
		title:         title,
		description:   description,
		options:       options,
		actionChannel: make(chan DialogActionId),
	}
	d.createLayout(width, height)
	return d
}

func (d *SelectionDialog) createLayout(width int, height int) {
	textDescriptionView := tview.NewTextView().SetText(d.description)
	optionTable := createOptionTable(d.application, d.options, d.selectAction)

	dialogContent := tview.NewFlex().SetDirection(tview.FlexRow)
	dialogContent.AddItem(textDescriptionView, 0, 1, false)
	dialogContent.AddItem(optionTable, 0, 1, true)

	dialog := createModal(d.title, dialogContent, width, height)
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
