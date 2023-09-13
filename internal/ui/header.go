package ui

import (
	"fmt"
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
	versionText := global.Version
	if versionText == "dev" {
		versionText = fmt.Sprintf("%s-(#%s)-%s", global.Version, global.Commit, global.Date)
	}

	applicationHeader := &ApplicationHeader{
		name:    "zfs-file-history",
		version: versionText,
	}

	applicationHeader.createLayout()
	applicationHeader.updateUi()

	return applicationHeader
}

func (applicationHeader *ApplicationHeader) createLayout() {
	layout := tview.NewFlex().SetDirection(tview.FlexColumn)
	layout.SetBackgroundColor(tcell.ColorRed)
	layout.SetTitleColor(tcell.ColorRed)
	layout.SetBorderColor(tcell.ColorGreen)

	nameTextView := tview.NewTextView()
	nameTextView.SetTextColor(tcell.ColorWhite)
	nameTextView.SetBackgroundColor(tcell.ColorDodgerBlue)
	nameText := fmt.Sprintf(" %s ", applicationHeader.name)
	nameTextView.SetText(nameText)
	nameTextView.SetTextAlign(tview.AlignCenter)

	versionTextView := tview.NewTextView()
	versionTextView.SetBackgroundColor(tcell.ColorGreenYellow)
	versionTextView.SetTextColor(tcell.ColorBlack)
	versionText := fmt.Sprintf("  %s  ", applicationHeader.version)
	versionTextView.SetText(versionText)
	versionTextView.SetTextAlign(tview.AlignCenter)

	helpTextView := tview.NewTextView()
	helpTextView.SetText("  Press '?' for help  ")
	helpTextView.SetTextColor(tcell.ColorYellow)
	helpTextView.SetTextAlign(tview.AlignRight)

	layout.AddItem(nameTextView, len(nameText), 0, false)
	layout.AddItem(versionTextView, len(versionText), 0, false)
	layout.AddItem(helpTextView, 0, 1, false)

	applicationHeader.layout = layout
}

func (applicationHeader *ApplicationHeader) updateUi() {
	// no changing data
}
