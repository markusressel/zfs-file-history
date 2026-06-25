package dialog

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
	"zfs-file-history/internal/ui/localization"
	uiutil "zfs-file-history/internal/ui/util"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"golang.org/x/term"
)

type DialogActionId int

const (
	DialogCloseActionId DialogActionId = iota
)

type DialogSeverity int

const (
	DialogSeverityNeutral DialogSeverity = iota
	DialogSeveritySafe
	DialogSeverityWarning
	DialogSeverityDanger
)

type Dialog interface {
	GetName() string
	GetLayout() *tview.Flex
	GetActionChannel() <-chan DialogActionId
}

type DialogOption struct {
	Id       DialogActionId
	Name     string
	Severity DialogSeverity
}

// buildConfirmDialogOptions creates a standard [confirm, cancel] option list.
func buildConfirmDialogOptions(
	confirmActionId DialogActionId,
	confirmLabel string,
	includeConfirm bool,
	severity DialogSeverity,
) []*DialogOption {
	options := []*DialogOption{{
		Id:   DialogCloseActionId,
		Name: localization.LocalizationCommonCancel,
	}}
	if includeConfirm {
		options = slices.Insert(options, 0, &DialogOption{
			Id:       confirmActionId,
			Name:     confirmLabel,
			Severity: severity,
		})
	}
	return options
}

// createModal creates a [tview.Flex] layout for a modal dialog with the given title and content.
func createModal(title string, content tview.Primitive, width int, height int) *tview.Flex {
	dialogFrame := tview.NewFlex()
	dialogFrame.SetBorder(true)
	uiutil.SetupDialogWindow(dialogFrame, title)
	dialogFrame.AddItem(content, 0, 1, true)

	dialogContentColumnWrapper := tview.NewFlex()
	dialogContentColumnWrapper.AddItem(nil, 0, 1, false)

	dialogContentRowWrapper := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(dialogFrame, height, 1, true).
		AddItem(nil, 0, 1, false)

	dialogContentColumnWrapper.
		AddItem(dialogContentRowWrapper, width, 1, true).
		AddItem(nil, 0, 1, false)

	return dialogContentColumnWrapper
}

func createOptionTable(application *tview.Application, options []*DialogOption, onSelect func(option *DialogOption)) *tview.Table {
	optionTable := tview.NewTable()
	optionTable.SetSelectable(true, false)
	optionTable.Select(0, 0)

	tableRow := 0
	hasMultipleOptions := len(options) > 1
	nonCloseIndex := 0

	for _, option := range options {
		var textColor tcell.Color
		switch option.Severity {
		case DialogSeverityNeutral, DialogSeveritySafe:
			textColor = tcell.ColorWhite
		case DialogSeverityWarning:
			textColor = tcell.ColorYellow
		case DialogSeverityDanger:
			textColor = tcell.ColorRed
		}

		if option.Id == DialogCloseActionId && hasMultipleOptions {
			optionTable.SetCell(tableRow, 0, tview.NewTableCell("").SetSelectable(false))
			optionTable.SetCell(tableRow, 1, tview.NewTableCell("").SetSelectable(false))
			tableRow++
		}

		prefixText := ""
		if option.Id == DialogCloseActionId {
			prefixText = "Esc."
		} else {
			nonCloseIndex++
			prefixText = fmt.Sprintf("%d.", nonCloseIndex)
		}

		prefixCell := tview.NewTableCell(prefixText).
			SetTextColor(tcell.ColorGray).
			SetAlign(tview.AlignRight)
		prefixCell.SetSelectedStyle(tcell.StyleDefault.
			Foreground(tcell.ColorGray).
			Background(tview.Styles.PrimitiveBackgroundColor))

		nameCell := tview.NewTableCell(option.Name).
			SetTextColor(textColor).
			SetAlign(tview.AlignLeft).
			SetExpansion(1)
		nameCell.SetReference(option)

		optionTable.SetCell(tableRow, 0, prefixCell)
		optionTable.SetCell(tableRow, 1, nameCell)
		tableRow++
	}

	optionTable.SetSelectedFunc(func(row, column int) {
		cell := optionTable.GetCell(row, 1)
		if cell != nil && cell.GetReference() != nil {
			if option, ok := cell.GetReference().(*DialogOption); ok {
				onSelect(option)
			}
		}
	})

	return optionTable
}

func createOptionDialogInputCapture(
	optionTable *tview.Table,
	options []*DialogOption,
	onSelect func(option *DialogOption),
	onClose func(),
) func(event *tcell.EventKey) *tcell.EventKey {
	return func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			onClose()
			return nil
		}
		if event.Key() == tcell.KeyEnter {
			row, _ := optionTable.GetSelection()
			cell := optionTable.GetCell(row, 1)
			if cell != nil && cell.GetReference() != nil {
				if option, ok := cell.GetReference().(*DialogOption); ok {
					onSelect(option)
				}
			}
			return nil
		}
		if event.Key() == tcell.KeyRune {
			r := event.Rune()
			if r >= '1' && r <= '9' {
				targetIndex := int(r - '0')
				optionCounter := 0
				rowCount := optionTable.GetRowCount()
				for row := 0; row < rowCount; row++ {
					cell := optionTable.GetCell(row, 1)
					if cell != nil && cell.GetReference() != nil {
						if opt, ok := cell.GetReference().(*DialogOption); ok {
							if opt.Id != DialogCloseActionId {
								optionCounter++
								if optionCounter == targetIndex {
									optionTable.Select(row, 0)
									return nil
								}
							}
						}
					}
				}
				return nil
			}
		}
		return event
	}
}

func emitDialogActions(actionChannel chan DialogActionId, actionIds ...DialogActionId) {
	go func() {
		for _, action := range actionIds {
			actionChannel <- action
		}
	}()
}

// ShowDialogOnPages mounts and focuses a dialog to the provided pages component.
// The dialog will be removed from the pages when it emits a close action.
// onUpdate - Called when the dialog emits a close action.
func ShowDialogOnPages(
	application *tview.Application,
	pages *tview.Pages,
	d Dialog,
	onUpdate func(),
) {
	layout := d.GetLayout()
	var previousFocus tview.Primitive
	if !layout.HasFocus() {
		previousFocus = application.GetFocus()
	}

	go func() {
		for {
			action := <-d.GetActionChannel()
			if action == DialogCloseActionId {
				application.QueueUpdateDraw(func() {
					pages.RemovePage(d.GetName())

					if layout.HasFocus() && previousFocus != nil {
						application.SetFocus(previousFocus)
					}

					if onUpdate != nil {
						onUpdate()
					}
				})
				return
			}
		}
	}()

	pages.AddPage(d.GetName(), layout, true, true)
	if !layout.HasFocus() {
		application.SetFocus(layout)
	}

	var lastValidFocus tview.Primitive = layout
	if layout.HasFocus() {
		if f := application.GetFocus(); f != nil {
			lastValidFocus = f
		}
	}

	// Ensure that clicking outside the focusable elements doesn't lose focus
	layout.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		currentFocus := application.GetFocus()
		if currentFocus != nil && layout.HasFocus() {
			switch currentFocus.(type) {
			case *tview.Table, *tview.InputField:
				lastValidFocus = currentFocus
			}
		}

		if action == tview.MouseLeftDown {
			go func() {
				time.Sleep(10 * time.Millisecond)
				application.QueueUpdateDraw(func() {
					newFocus := application.GetFocus()
					isLeaf := false
					if newFocus != nil {
						switch newFocus.(type) {
						case *tview.Table, *tview.InputField:
							isLeaf = true
						}
					}
					if !isLeaf || !layout.HasFocus() {
						application.SetFocus(lastValidFocus)
					}
				})
			}()
		}
		return action, event
	})

	if onUpdate != nil {
		onUpdate()
	}
}

type DialogSizeConstraints struct {
	Title             string
	Description       string
	ExtraContentWidth int
	StaticHeight      int
}

func CalculateDialogSize(constraints DialogSizeConstraints) (width int, height int) {
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

	maxContentWidth := utf8.RuneCountInString(constraints.Title)
	if constraints.Description != "" {
		if l := utf8.RuneCountInString(constraints.Description); l > maxContentWidth {
			maxContentWidth = l
		}
	}
	if constraints.ExtraContentWidth > maxContentWidth {
		maxContentWidth = constraints.ExtraContentWidth
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

	descHeight := 0
	if constraints.Description != "" {
		descHeight = calculateWrappedHeight(constraints.Description, textLineWidth)
	}

	dialogHeight := 2 + descHeight + constraints.StaticHeight
	maxHeight := termHeight - 2
	if maxHeight < 5 {
		maxHeight = 5
	}
	if dialogHeight > maxHeight {
		dialogHeight = maxHeight
	}

	return dialogWidth, dialogHeight
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

func ensureDialogCloseIsLast(options []*DialogOption) []*DialogOption {
	closeIndex := slices.IndexFunc(options, func(option *DialogOption) bool {
		return option != nil && option.Id == DialogCloseActionId
	})
	if closeIndex < 0 || closeIndex == len(options)-1 {
		return options
	}

	closeOption := options[closeIndex]
	result := slices.Delete(options, closeIndex, closeIndex+1)
	result = append(result, closeOption)
	return result
}
