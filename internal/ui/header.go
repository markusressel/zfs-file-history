package ui

import (
	"fmt"
	"strings"
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
	// messages are the messages of the application: the current one is shown in statusTextView
	messages *status_message.Center
	// unreadBadge shows the number of warnings and errors not yet seen in the message history, e.g. "⚠ 2"
	unreadBadge *tview.TextView
	// shortcutHint tells how to show the shortcuts while they are hidden, see UpdateShortcutHint
	shortcutHint *tview.TextView
	// pageIndicator shows the page and its position, e.g. "FILES 1/2", see SetPage
	pageIndicator *tview.TextView
}

func NewApplicationHeader(application *tview.Application, messages *status_message.Center) *ApplicationHeaderComponent {
	versionText := fmt.Sprintf("%s-(#%s)-%s", global.Version, global.Commit, global.Date)

	applicationHeader := &ApplicationHeaderComponent{
		application: application,
		name:        "zfs-file-history",
		version:     versionText,
		messages:    messages,
	}

	applicationHeader.createLayout()
	messages.OnChanged(applicationHeader.updateMessages)
	applicationHeader.updateMessages()

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

	unreadBadge := tview.NewTextView().
		SetTextStyle(tcell.StyleDefault.Bold(true)).
		SetTextAlign(tview.AlignCenter)

	shortcutHint := uiutil.CreateAttentionTextView(shortcutHintText)

	pageIndicator := tview.NewTextView().
		SetTextStyle(tcell.StyleDefault.Bold(true)).
		SetTextColor(theme.Colors.Header.PageIndicator).
		SetTextAlign(tview.AlignCenter)
	pageIndicator.SetBackgroundColor(theme.Colors.Header.PageIndicatorBackground)

	layout.AddItem(nameTextView, len(nameText), 0, false)
	layout.AddItem(versionTextView, len(versionText), 0, false)
	layout.AddItem(statusTextView, 0, 1, false)
	// sized by updateMessages
	layout.AddItem(unreadBadge, 0, 0, false)
	// hidden until SetPage
	layout.AddItem(pageIndicator, 0, 0, false)
	// sized by UpdateShortcutHint
	layout.AddItem(shortcutHint, 0, 0, false)

	applicationHeader.pageIndicator = pageIndicator
	applicationHeader.shortcutHint = shortcutHint

	applicationHeader.statusTextView = statusTextView
	applicationHeader.unreadBadge = unreadBadge
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

// updateMessages shows the current message and the number of unread warnings and errors. Must be called on the UI
// thread.
func (applicationHeader *ApplicationHeaderComponent) updateMessages() {
	if current := applicationHeader.messages.Current(); current != nil {
		applicationHeader.statusTextView.SetText(current.Message).SetTextColor(current.Color())
	} else {
		applicationHeader.statusTextView.SetText("")
	}

	text := ""
	count, highest := applicationHeader.messages.UnreadCount()
	if count > 0 {
		text = formatUnreadBadge(count)
		applicationHeader.unreadBadge.SetTextColor(highest.Color())
	}
	applicationHeader.unreadBadge.SetText(text)
	// one space on each side
	width := 0
	if text != "" {
		width = tview.TaggedStringWidth(text) + 2
	}
	applicationHeader.layout.ResizeItem(applicationHeader.unreadBadge, width, 0)
}

// formatUnreadBadge returns e.g. "⚠ 2" for two unread warnings or errors. Its color tells whether there are errors.
func formatUnreadBadge(count int) string {
	return fmt.Sprintf("%s %d", status_message.LevelWarning.Icon(), count)
}
