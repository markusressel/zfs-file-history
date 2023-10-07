package ui

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"time"
	"zfs-file-history/cmd/global"
	"zfs-file-history/internal/ui/status_message"
	"zfs-file-history/internal/ui/theme"
	uiutil "zfs-file-history/internal/ui/util"
)

type ApplicationHeaderComponent struct {
	application    *tview.Application
	layout         *tview.Flex
	name           string
	version        string
	statusTextView *tview.TextView
	lastStatus     *status_message.StatusMessage
}

func NewApplicationHeader(application *tview.Application) *ApplicationHeaderComponent {
	versionText := fmt.Sprintf("%s-(#%s)-%s", global.Version, global.Commit, global.Date)

	applicationHeader := &ApplicationHeaderComponent{
		application: application,
		name:        "zfs-file-history",
		version:     versionText,
	}

	applicationHeader.createLayout()
	applicationHeader.updateUi()

	return applicationHeader
}

func (applicationHeader *ApplicationHeaderComponent) createLayout() {
	layout := tview.NewFlex().SetDirection(tview.FlexColumn)

	nameTextView := tview.NewTextView()
	nameTextView.SetTextColor(theme.Colors.Header.Name)
	nameTextView.SetBackgroundColor(theme.Colors.Header.NameBackground)
	nameText := fmt.Sprintf(" %s ", applicationHeader.name)
	nameTextView.SetText(nameText)
	nameTextView.SetTextAlign(tview.AlignCenter)

	versionTextView := tview.NewTextView()
	versionTextView.SetTextColor(theme.Colors.Header.Version)
	versionTextView.SetBackgroundColor(theme.Colors.Header.VersionBackground)
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

func (applicationHeader *ApplicationHeaderComponent) SetStatus(status *status_message.StatusMessage) {
	applicationHeader.statusTextView.SetText(status.Message).SetTextColor(status.Color)
	applicationHeader.application.ForceDraw()
	if status.Duration > 0 {
		go func() {
			time.Sleep(status.Duration)
			if applicationHeader.lastStatus != status {
				return
			}
			applicationHeader.ResetStatus()
		}()
	}
	applicationHeader.lastStatus = status
}

func (applicationHeader *ApplicationHeaderComponent) ResetStatus() {
	applicationHeader.statusTextView.SetText("").SetTextColor(tcell.ColorWhite)
	applicationHeader.application.ForceDraw()
}
