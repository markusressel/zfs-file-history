package dialog

import (
	"fmt"
	"time"
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

	optionTable *tview.Table
	isRunning   bool

	// Handlers for exclusive async execution
	handler    func(d *SelectionDialog, action DialogActionId) error
	onComplete func(d *SelectionDialog, option *DialogOption, err error)
}

// handler - The background execution logic to be executed when the user selects an option.
// onComplete - The callback to be executed on the UI thread after the background execution completes.
func NewSelectionDialog(
	application *tview.Application,
	name string,
	title string,
	description string,
	options []*DialogOption,
	handler func(dialog *SelectionDialog, action DialogActionId) error,
	onComplete func(d *SelectionDialog, option *DialogOption, err error),
) *SelectionDialog {
	d := &SelectionDialog{
		application:   application,
		name:          name,
		title:         title,
		description:   description,
		options:       options,
		actionChannel: make(chan DialogActionId),
		handler:       handler,
		onComplete:    onComplete,
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
		StaticHeight:      1 + actualTableRows,
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

	d.optionTable = createOptionTable(d.application, d.options, d.selectAction)

	dialogContent := tview.NewFlex().SetDirection(tview.FlexRow)
	dialogContent.AddItem(textDescriptionView, descHeight, 0, false)
	dialogContent.AddItem(tview.NewBox(), 1, 0, false)
	dialogContent.AddItem(d.optionTable, actualTableRows, 0, true)

	dialog := createModal(d.title, dialogContent, dialogWidth, dialogHeight)
	dialog.SetInputCapture(createOptionDialogInputCapture(d.optionTable, d.options, d.selectAction, d.Close))
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
	if option.Id == DialogCloseActionId {
		d.Close()
		return
	}

	if d.handler != nil {
		d.ShowLoading(option)

		go func() {
			err := d.handler(d, option.Id)

			d.application.QueueUpdateDraw(func() {
				d.StopLoading()
				if d.onComplete != nil {
					d.onComplete(d, option, err)
				}
			})
		}()
	}
}

func (d *SelectionDialog) ShowLoading(option *DialogOption) {
	d.isRunning = true
	d.optionTable.SetSelectable(false, false) // Lock input

	var targetRow, targetCol int
	var originalText string
	found := false

	// Safely find the specific table cell by matching the exact memory reference
	for r := 0; r < d.optionTable.GetRowCount(); r++ {
		cell := d.optionTable.GetCell(r, 1) // 1 is the name column
		if cell != nil && cell.GetReference() == option {
			targetRow = r
			targetCol = 1
			originalText = cell.Text
			found = true
			break
		}
	}

	if !found {
		return
	}

	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		frameIdx := 0

		for {
			<-ticker.C
			if !d.isRunning {
				// Restore original text when loading finishes
				d.application.QueueUpdateDraw(func() {
					cell := d.optionTable.GetCell(targetRow, targetCol)
					if cell != nil {
						cell.SetText(originalText)
					}
				})
				break
			}

			// Update the cell with the spinning frame
			d.application.QueueUpdateDraw(func() {
				cell := d.optionTable.GetCell(targetRow, targetCol)
				if cell != nil {
					cell.SetText(fmt.Sprintf("%s %s", originalText, frames[frameIdx]))
				}
			})

			frameIdx = (frameIdx + 1) % len(frames)
		}
	}()
}

func (d *SelectionDialog) StopLoading() {
	d.isRunning = false
	d.optionTable.SetSelectable(true, false) // Unlock input
}
