package dialog

import (
	"slices"
	"zfs-file-history/internal/ui/localization"
	uiutil "zfs-file-history/internal/ui/util"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
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

	for row, option := range options {
		var textColor tcell.Color
		switch option.Severity {
		case DialogSeverityNeutral:
			textColor = tcell.ColorWhite
		case DialogSeveritySafe:
			textColor = tcell.ColorWhite
		case DialogSeverityWarning:
			textColor = tcell.ColorYellow
		case DialogSeverityDanger:
			textColor = tcell.ColorRed
		}
		optionTable.SetCell(row, 0,
			tview.NewTableCell(option.Name).
				SetTextColor(textColor).
				SetAlign(tview.AlignLeft).
				SetExpansion(1),
		)
	}

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
			onSelect(options[row])
			return nil
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

func ShowDialogOnPages(
	application *tview.Application,
	pages *tview.Pages,
	d Dialog,
	actionHandler func(action DialogActionId) bool,
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
			if actionHandler(action) {
				return
			}
			if action == DialogCloseActionId {
				application.QueueUpdateDraw(func() {
					pages.RemovePage(d.GetName())
					if previousFocus != nil {
						application.SetFocus(previousFocus)
					}
					if onUpdate != nil {
						onUpdate()
					}
				})
			}
		}
	}()
	// Opening dialogs is usually triggered from input handlers on the UI goroutine.
	// Calling QueueUpdateDraw there can deadlock, so update directly.
	pages.AddPage(d.GetName(), layout, true, true)
	if !layout.HasFocus() {
		application.SetFocus(layout)
	}
	if onUpdate != nil {
		onUpdate()
	}
}
