package ui

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"zfs-file-history/cmd/global"
	uiutil "zfs-file-history/internal/ui/util"
)

type ApplicationHeaderComponent struct {
	layout         *tview.Flex
	name           string
	version        string
	statusTextView *tview.TextView
}

func NewApplicationHeader() *ApplicationHeaderComponent {
	versionText := global.Version
	if versionText == "dev" {
		versionText = fmt.Sprintf("%s-(#%s)-%s", global.Version, global.Commit, global.Date)
	}

	applicationHeader := &ApplicationHeaderComponent{
		name:    "zfs-file-history",
		version: versionText,
	}

	applicationHeader.createLayout()
	applicationHeader.updateUi()

	return applicationHeader
}

func (applicationHeader *ApplicationHeaderComponent) createLayout() {
	layout := tview.NewFlex().SetDirection(tview.FlexColumn)
	// TODO: check colors
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

	statusTextView := tview.NewTextView()
	statusTextView.SetBorderPadding(0, 0, 1, 1)
	statusTextView.SetTextColor(tcell.ColorGray)
	statusTextView.SetTextAlign(tview.AlignLeft)

	helpText := "Press '?' for help"
	helpTextView := uiutil.CreateAttentionTextView(helpText)

	layout.AddItem(nameTextView, len(nameText), 0, false)
	layout.AddItem(versionTextView, len(versionText), 0, false)
	layout.AddItem(statusTextView, 0, 1, false)
	layout.AddItem(helpTextView, len(helpText)+4, 0, false)

	applicationHeader.statusTextView = statusTextView
	applicationHeader.layout = layout
}

func (applicationHeader *ApplicationHeaderComponent) updateUi() {
	// no changing data
}

func (applicationHeader *ApplicationHeaderComponent) SetStatus(text string) {
	applicationHeader.statusTextView.SetText(text)
}
