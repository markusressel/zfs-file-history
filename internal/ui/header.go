package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"zfs-file-history/cmd/global"
)

type ApplicationHeader struct {
	layout  *tview.Flex
	name    string
	version string
}

func NewApplicationHeader() *ApplicationHeader {
	applicationHeader := &ApplicationHeader{
		name:    "zfs-file-history",
		version: global.Version,
	}

	applicationHeader.Layout()
	applicationHeader.updateUi()

	return applicationHeader
}

func (applicationHeader *ApplicationHeader) Layout() {
	layout := tview.NewFlex().SetDirection(tview.FlexColumn)
	layout.SetBackgroundColor(tcell.ColorRed)
	layout.SetTitleColor(tcell.ColorRed)
	layout.SetBorderColor(tcell.ColorGreen)

	nameText := tview.NewTextView()
	nameText.SetTextColor(tcell.ColorWhite)
	nameText.SetBackgroundColor(tcell.ColorDodgerBlue)
	nameText.SetText(applicationHeader.name)
	nameText.SetTextAlign(tview.AlignCenter)

	versionText := tview.NewTextView()
	versionText.SetBackgroundColor(tcell.ColorGreenYellow)
	versionText.SetTextColor(tcell.ColorBlack)
	versionText.SetText(applicationHeader.version)
	versionText.SetTextAlign(tview.AlignCenter)

	helpText := tview.NewTextView()
	helpText.SetText("  Press `?` for help  ")
	helpText.SetTextColor(tcell.ColorWhite)
	helpText.SetTextAlign(tview.AlignRight)

	layout.AddItem(nameText, 20, 0, false)
	layout.AddItem(versionText, 10, 0, false)
	layout.AddItem(helpText, 0, 1, false)

	applicationHeader.layout = layout
}

func (applicationHeader *ApplicationHeader) updateUi() {
	// no changing data
}
