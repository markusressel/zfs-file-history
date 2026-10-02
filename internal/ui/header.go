package ui

import (
	"fmt"
	"strings"
	"time"
	"zfs-file-history/cmd/global"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/status_message"
	"zfs-file-history/internal/ui/theme"
	uiutil "zfs-file-history/internal/ui/util"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type ApplicationHeaderComponent struct {
	application    *tview.Application
	layout         *tview.Flex
	name           string
	version        string
	statusTextView *tview.TextView
	lastStatus     *status_message.StatusMessage
	// shortcutHint tells how to show the shortcuts while they are hidden, see UpdateShortcutHint
	shortcutHint *tview.TextView
	// pageIndicator shows the page and its position, e.g. "FILES 1/2", see SetPage
	pageIndicator *tview.TextView
}

func NewApplicationHeader(application *tview.Application) *ApplicationHeaderComponent {
	versionText := fmt.Sprintf("%s-(#%s)-%s", global.Version, global.Commit, global.Date)

	applicationHeader := &ApplicationHeaderComponent{
		application: application,
		name:        "zfs-file-history",
		version:     versionText,
	}

	applicationHeader.createLayout()

	return applicationHeader
}

func (applicationHeader *ApplicationHeaderComponent) createLayout() {
	layout := tview.NewFlex().SetDirection(tview.FlexColumn)

	nameTextView := tview.NewTextView()
	nameTextView.SetTextStyle(tcell.StyleDefault.Bold(true))
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

	shortcutHint := uiutil.CreateAttentionTextView(shortcutHintText)

	pageIndicator := tview.NewTextView().
		SetTextStyle(tcell.StyleDefault.Bold(true)).
		SetTextColor(theme.Colors.Header.PageIndicator).
		SetTextAlign(tview.AlignCenter)
	pageIndicator.SetBackgroundColor(theme.Colors.Header.PageIndicatorBackground)

	layout.AddItem(nameTextView, len(nameText), 0, false)
	layout.AddItem(versionTextView, len(versionText), 0, false)
	layout.AddItem(statusTextView, 0, 1, false)
	// hidden until SetPage
	layout.AddItem(pageIndicator, 0, 0, false)
	// sized by UpdateShortcutHint
	layout.AddItem(shortcutHint, 0, 0, false)

	applicationHeader.pageIndicator = pageIndicator
	applicationHeader.shortcutHint = shortcutHint

	applicationHeader.statusTextView = statusTextView
	applicationHeader.layout = layout
	applicationHeader.UpdateShortcutHint()
}

// shortcutHintText is shown while the shortcuts are hidden.
const shortcutHintText = "? shortcuts"

// UpdateShortcutHint shows how to show the shortcuts while they are hidden (see shortcut_helper.ToggleShortcuts),
// and nothing while they are shown. Must be called on the UI thread.
func (applicationHeader *ApplicationHeaderComponent) UpdateShortcutHint() {
	width := 0
	if shortcut_helper.ShortcutsHidden() {
		width = len(shortcutHintText) + 4
	}
	applicationHeader.layout.ResizeItem(applicationHeader.shortcutHint, width, 0)
}

// SetPage shows the title of the page the header belongs to and its position among the pages, e.g. "FILES 1/2".
// The title is padded to titleWidth (the longest title of all pages), so the indicator has the same width on all
// pages and the position is right-aligned in it.
func (applicationHeader *ApplicationHeaderComponent) SetPage(title string, titleWidth int, number int, count int) {
	text := formatPageIndicator(title, titleWidth, number, count)
	applicationHeader.pageIndicator.SetText(text)
	// one space on each side
	applicationHeader.layout.ResizeItem(applicationHeader.pageIndicator, len(text)+2, 0)
}

// formatPageIndicator returns e.g. "FILES    1/2" for a titleWidth of 8.
func formatPageIndicator(title string, titleWidth int, number int, count int) string {
	return fmt.Sprintf("%-*s %d/%d", titleWidth, strings.ToUpper(title), number, count)
}

// SetStatus shows a status message. A message with a duration is cleared after it, unless another message was shown
// in the meantime. Must be called on the UI thread.
func (applicationHeader *ApplicationHeaderComponent) SetStatus(status *status_message.StatusMessage) {
	applicationHeader.statusTextView.SetText(status.Message).SetTextColor(status.Color)
	applicationHeader.lastStatus = status
	if status.Duration > 0 {
		// the timer runs on its own goroutine, so it hands the clearing over to the UI thread
		time.AfterFunc(status.Duration, func() {
			applicationHeader.application.QueueUpdateDraw(func() {
				if applicationHeader.lastStatus == status {
					applicationHeader.ClearStatus()
				}
			})
		})
	}
}

// ClearStatus removes the status message. Must be called on the UI thread.
func (applicationHeader *ApplicationHeaderComponent) ClearStatus() {
	applicationHeader.statusTextView.SetText("").SetTextColor(tcell.ColorWhite)
	applicationHeader.lastStatus = nil
}
