package dialog

import (
	"github.com/rivo/tview"
	"zfs-file-history/internal/ui/theme"
	uiutil "zfs-file-history/internal/ui/util"
)

type DialogAction int

const (
	ActionClose DialogAction = iota
)

type Dialog interface {
	GetName() string
	GetLayout() *tview.Flex
	GetActionChannel() chan DialogAction
}

type DialogOption struct {
	Name string
}

func createModal(title string, content tview.Primitive, width int, height int) *tview.Flex {
	dialogFrame := tview.NewFlex()
	dialogFrame.SetBorder(true)
	uiutil.SetupWindowTitle(dialogFrame, title)
	dialogFrame.SetBorderColor(theme.GetDialogBorderColor())
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
