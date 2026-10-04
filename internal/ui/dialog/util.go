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

// pagesAware is implemented by dialogs that need to know the pages they are shown on, e.g. to show follow-up dialogs.
type pagesAware interface {
	setPages(pages *tview.Pages)
}

// mouseCapturer is implemented by dialogs that handle mouse events themselves, e.g. dragging the boundary of
// resizable panes. ShowDialogOnPages owns the mouse capture of the dialog layout and calls this first.
type mouseCapturer interface {
	captureMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse)
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

// MakeFlexResizing registers a SetDrawFunc resize handler on columnWrapper
// to dynamically resize rowWrapper and dialogFrame based on terminal size and constraints.
func MakeFlexResizing(
	columnWrapper *tview.Flex,
	rowWrapper *tview.Flex,
	dialogFrame *tview.Flex,
	targetWidth, minWidth int,
	targetHeight, minHeight int,
) {
	columnWrapper.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		screenWidth, screenHeight := screen.Size()
		w := targetWidth
		if w > screenWidth-4 {
			w = screenWidth - 4
		}
		if w < minWidth {
			w = minWidth
		}
		h := targetHeight
		if h > screenHeight-2 {
			h = screenHeight - 2
		}
		if h < minHeight {
			h = minHeight
		}

		columnWrapper.ResizeItem(rowWrapper, w, 1)
		rowWrapper.ResizeItem(dialogFrame, h, 1)

		return x, y, width, height
	})
}

// createModal creates a [tview.Flex] layout for a modal dialog with the given title and content.
func createModal(title string, content tview.Primitive, constraints DialogSizeConstraints) *tview.Flex {
	layout, _ := createModalWithFrame(title, content, constraints)
	return layout
}

// clearInside makes a bordered dialog frame clear its inside before its children are drawn. tview's Flex does not
// clear its area, so gaps between the children (e.g. the padding of a table) would show the page behind the dialog.
// modalLayout is the layout of a dialog as a page: it consumes all mouse events but moves, also those that no
// primitive of the dialog consumes (e.g. on its border). tview hands mouse events on to the pages below until one
// consumes them, so they would reach the page behind the dialog otherwise (https://github.com/rivo/tview/issues/926).
// Moves are not consumed, so moving the mouse does not redraw the screen.
type modalLayout struct {
	*tview.Flex
}

func (layout modalLayout) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
	handler := layout.Flex.MouseHandler()
	return func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (bool, tview.Primitive) {
		consumed, capture := handler(action, event, setFocus)
		return consumed || action != tview.MouseMove, capture
	}
}

// isOnDialog returns whether the position is on the dialog (its frame), not on the empty space around it. The
// layouts of dialogs (see createModalWithFrame, createOverlayFrame) center the frame with nil items around it;
// frames have no nil items.
func isOnDialog(primitive tview.Primitive, x int, y int) bool {
	if flex, ok := primitive.(*tview.Flex); ok {
		isWrapper := false
		for i := 0; i < flex.GetItemCount(); i++ {
			if flex.GetItem(i) == nil {
				isWrapper = true
				break
			}
		}
		if isWrapper {
			for i := 0; i < flex.GetItemCount(); i++ {
				if item := flex.GetItem(i); item != nil && isOnDialog(item, x, y) {
					return true
				}
			}
			return false
		}
	}
	rectX, rectY, width, height := primitive.GetRect()
	return x >= rectX && x < rectX+width && y >= rectY && y < rectY+height
}

// The draw func runs after the border is drawn and before the children. Call it before other draw funcs are
// installed on the frame (e.g. uiutil.NewBorderFooter), which keep and call it.
func clearInside(frame *tview.Box) {
	frame.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		fillBackground(screen, x+1, y+1, width-2, height-2)
		// -1: tview computes the inner rect (border and padding) itself
		return -1, 0, 0, 0
	})
}

// fillBackground fills the given area with the background of the widgets.
func fillBackground(screen tcell.Screen, x, y, width, height int) {
	style := tcell.StyleDefault.Background(tview.Styles.PrimitiveBackgroundColor)
	for row := y; row < y+height; row++ {
		for column := x; column < x+width; column++ {
			screen.SetContent(column, row, ' ', nil, style)
		}
	}
}

// createModalWithFrame is createModal, and also returns the frame (with the border and title) around the content,
// e.g. to show the footer of a table in its bottom border, see table.RowSelectionTable.EmbedInFrame.
func createModalWithFrame(title string, content tview.Primitive, constraints DialogSizeConstraints) (*tview.Flex, *tview.Flex) {
	dialogFrame := tview.NewFlex()
	dialogFrame.SetBorder(true)
	uiutil.SetupDialogWindow(dialogFrame, title)
	clearInside(dialogFrame.Box)
	dialogFrame.AddItem(content, 0, 1, true)

	dialogContentColumnWrapper := tview.NewFlex()
	dialogContentColumnWrapper.AddItem(nil, 0, 1, false)

	dialogContentRowWrapper := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(dialogFrame, 0, 1, true).
		AddItem(nil, 0, 1, false)

	dialogContentColumnWrapper.
		AddItem(dialogContentRowWrapper, 0, 1, true).
		AddItem(nil, 0, 1, false)

	dialogContentColumnWrapper.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		screenWidth, screenHeight := screen.Size()
		w, h := CalculateDialogSize(constraints)

		// Center the modal on top of the view it is associated with (defined by x, y, width, height)
		dx := x + (width-w)/2
		dy := y + (height-h)/2

		// Ensure the dialog stays completely within available terminal screen bounds
		if dx < 0 {
			dx = 0
		}
		if dx+w > screenWidth {
			dx = screenWidth - w
		}
		if dy < 0 {
			dy = 0
		}
		if dy+h > screenHeight {
			dy = screenHeight - h
		}

		dialogContentColumnWrapper.ResizeItem(dialogContentRowWrapper, w, 1)
		dialogContentRowWrapper.ResizeItem(dialogFrame, h, 1)

		dialogContentColumnWrapper.SetRect(dx, dy, w, h)

		return dx, dy, w, h
	})

	return dialogContentColumnWrapper, dialogFrame
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
	onClosed func(),
) {
	layout := d.GetLayout()
	if aware, ok := d.(pagesAware); ok {
		aware.setPages(pages)
	}
	var previousFocus tview.Primitive
	if !layout.HasFocus() {
		previousFocus = application.GetFocus()
	}

	go func() {
		for {
			action := <-d.GetActionChannel()
			if action == DialogCloseActionId {
				closeFunc := func() {
					if layout.HasFocus() {
						if previousFocus != nil && isDescendantOf(pages, previousFocus) {
							application.SetFocus(previousFocus)
						} else {
							application.SetFocus(pages)
						}
					}

					pages.RemovePage(d.GetName())

					if onClosed != nil {
						onClosed()
					}
				}

				application.QueueUpdateDraw(closeFunc)
				return
			}
		}
	}()

	pages.AddPage(d.GetName(), modalLayout{layout}, true, true)
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
	capturer, hasMouseCapture := d.(mouseCapturer)
	layout.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		// first, so drags (e.g. of a split) continue when the mouse leaves the dialog
		if hasMouseCapture {
			if action, event = capturer.captureMouse(action, event); event == nil {
				return action, event
			}
		}
		// tview hands mouse events to all pages until one consumes them, and the empty space around the dialog does
		// not: without this, a click next to the dialog would select an entry of the page behind it
		// (https://github.com/rivo/tview/issues/926)
		if x, y := event.Position(); !isOnDialog(layout, x, y) {
			if action == tview.MouseMove {
				// no redraw for moves; the page behind ignores them while it is not in front
				return action, nil
			}
			return tview.MouseConsumed, nil
		}
		currentFocus := application.GetFocus()
		if currentFocus != nil && layout.HasFocus() {
			switch currentFocus.(type) {
			case *tview.Table, *tview.InputField, *tview.TextView, *tview.TextArea:
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
						case *tview.Table, *tview.InputField, *tview.TextView, *tview.TextArea:
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

	if onClosed != nil {
		onClosed()
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

func isDescendantOf(parent tview.Primitive, child tview.Primitive) bool {
	if parent == nil || child == nil {
		return false
	}
	if parent == child {
		return true
	}
	switch p := parent.(type) {
	case modalLayout:
		return isDescendantOf(p.Flex, child)
	case *tview.Flex:
		for i := 0; i < p.GetItemCount(); i++ {
			if isDescendantOf(p.GetItem(i), child) {
				return true
			}
		}
	case *tview.Pages:
		for _, name := range p.GetPageNames(false) {
			pagePrim := p.GetPage(name)
			if isDescendantOf(pagePrim, child) {
				return true
			}
		}
	}
	return false
}
