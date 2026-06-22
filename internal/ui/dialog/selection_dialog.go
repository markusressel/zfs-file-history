package dialog

import (
	"os"
	"strings"
	"unicode/utf8"

	"github.com/rivo/tview"
	"golang.org/x/term"
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
	termWidth, termHeight, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || termWidth <= 0 || termHeight <= 0 {
		termWidth = 80
		termHeight = 24
	}

	minWidth := 40
	maxWidth := 80
	if maxWidth > termWidth-4 {
		maxWidth = termWidth - 4
	}
	if minWidth > maxWidth {
		minWidth = maxWidth
	}
	if minWidth < 10 {
		minWidth = 10
	}

	maxContentWidth := utf8.RuneCountInString(d.title)
	if l := utf8.RuneCountInString(d.description); l > maxContentWidth {
		maxContentWidth = l
	}
	for _, opt := range d.options {
		if l := utf8.RuneCountInString(opt.Name); l > maxContentWidth {
			maxContentWidth = l
		}
	}

	dialogWidth := maxContentWidth + 6
	if dialogWidth < minWidth {
		dialogWidth = minWidth
	}
	if dialogWidth > maxWidth {
		dialogWidth = maxWidth
	}

	textLineWidth := dialogWidth - 6
	if textLineWidth < 5 {
		textLineWidth = 5
	}
	descHeight := calculateWrappedHeight(d.description, textLineWidth)

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

	dialogHeight := 2 + descHeight + 1 + actualTableRows
	maxHeight := termHeight - 2
	if maxHeight < 5 {
		maxHeight = 5
	}
	if dialogHeight > maxHeight {
		dialogHeight = maxHeight
	}

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

func calculateWrappedHeight(text string, maxLineWidth int) int {
	lines := strings.Split(text, "\n")
	height := 0
	for _, line := range lines {
		runes := utf8.RuneCountInString(line)
		if runes == 0 {
			height += 1
			continue
		}
		height += (runes + maxLineWidth - 1) / maxLineWidth
	}
	return height
}
