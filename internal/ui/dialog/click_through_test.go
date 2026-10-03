package dialog

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"zfs-file-history/internal/testutil"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression: tview hands mouse events to all pages until one consumes them (https://github.com/rivo/tview/issues/926),
// and the empty space around a dialog did not: a click next to it selected an entry of the page behind it.
func TestShowDialogOnPages_MouseEventsOutsideTheDialogDoNotReachThePageBehind(t *testing.T) {
	app := tview.NewApplication()
	screen := tcell.NewSimulationScreen("UTF-8")
	app.SetScreen(screen)
	screen.SetSize(100, 40)
	app.EnableMouse(true)
	background := tview.NewTable().SetSelectable(true, false)
	for i := 0; i < 100; i++ {
		background.SetCell(i, 0, tview.NewTableCell(fmt.Sprintf("row %d", i)))
	}
	pages := tview.NewPages().AddPage("background", background, true, true)
	app.SetRoot(pages, true)
	go func() { _ = app.Run() }()
	t.Cleanup(app.Stop)

	chosen := make(chan DialogActionId, 1)
	var dialog *SelectionDialog
	testutil.OnUiThread(t, app, func() {
		app.SetFocus(background)
		background.Select(0, 0)
		options := []*DialogOption{{Id: 1, Name: "First"}, {Id: DialogCloseActionId, Name: "Close"}}
		dialog = NewSelectionDialog(app, "ClickDialog", "Click", "a dialog", options, nil,
			func(d *SelectionDialog, option *DialogOption, err error) { chosen <- option.Id })
		ShowDialogOnPages(app, pages, dialog, nil)
	})
	state := func() (row int, offset int) {
		testutil.OnUiThread(t, app, func() {
			app.ForceDraw()
			row, _ = background.GetSelection()
			offset, _ = background.GetOffset()
		})
		return row, offset
	}
	click := func(x, y int) {
		screen.InjectMouse(x, y, tcell.Button1, tcell.ModNone)
		time.Sleep(30 * time.Millisecond)
		screen.InjectMouse(x, y, tcell.ButtonNone, tcell.ModNone)
		time.Sleep(100 * time.Millisecond)
	}
	state()

	// row 2 of the table behind, at the top left, outside the centered dialog
	click(2, 2)
	row, _ := state()
	assert.Equal(t, 0, row, "the selection of the page behind is unchanged")

	// the mouse wheel outside the dialog does not scroll the page behind either
	for i := 0; i < 5; i++ {
		screen.InjectMouse(2, 2, tcell.WheelDown, tcell.ModNone)
		// like a terminal, which reports no buttons after the wheel
		screen.InjectMouse(2, 2, tcell.ButtonNone, tcell.ModNone)
	}
	time.Sleep(100 * time.Millisecond)
	_, offset := state()
	assert.Equal(t, 0, offset)

	// nor does a click on its border, which no primitive of the dialog consumes
	var frameX, frameY, frameHeight int
	testutil.OnUiThread(t, app, func() { frameX, frameY, _, frameHeight = dialogFrame(dialog.GetLayout()).GetRect() })
	require.Positive(t, frameHeight)
	time.Sleep(tview.DoubleClickInterval)
	click(frameX, frameY+frameHeight/2)
	row, _ = state()
	assert.Equal(t, 0, row, "the selection of the page behind is unchanged")

	// clicks on the dialog still work: they select its options
	var closeX, closeY int
	testutil.OnUiThread(t, app, func() {
		cells, width, height := screen.GetContents()
		for y := 0; y < height && closeY == 0; y++ {
			var line strings.Builder
			for x := 0; x < width; x++ {
				line.WriteString(string(cells[y*width+x].Runes))
			}
			// the option, not the title of the dialog
			if index := strings.Index(line.String(), "Close"); index >= 0 {
				closeX, closeY = len([]rune(line.String()[:index])), y
			}
		}
	})
	require.NotZero(t, closeY, "the close option is shown")
	// otherwise, tview takes it as a double click with the click above (even at another position)
	time.Sleep(tview.DoubleClickInterval)
	click(closeX, closeY)
	var selected string
	testutil.OnUiThread(t, app, func() {
		row, _ := dialog.optionTable.GetSelection()
		selected = dialog.optionTable.GetCell(row, 1).Text
	})
	assert.Contains(t, selected, "Close", "the clicked option is selected")
	select {
	case <-chosen:
		t.Fatal("a click selects an option, Enter chooses it")
	default:
	}
}

// dialogFrame returns the frame of a dialog in its layout, see isOnDialog.
func dialogFrame(primitive tview.Primitive) tview.Primitive {
	flex, ok := primitive.(*tview.Flex)
	if !ok {
		return primitive
	}
	isWrapper := false
	for i := 0; i < flex.GetItemCount(); i++ {
		isWrapper = isWrapper || flex.GetItem(i) == nil
	}
	if !isWrapper {
		return primitive
	}
	for i := 0; i < flex.GetItemCount(); i++ {
		if item := flex.GetItem(i); item != nil {
			return dialogFrame(item)
		}
	}
	return nil
}
