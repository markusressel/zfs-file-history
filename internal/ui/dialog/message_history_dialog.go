package dialog

import (
	"fmt"
	"strings"
	"zfs-file-history/internal/ui/status_message"
	"zfs-file-history/internal/ui/txwidgets"
	"zfs-file-history/internal/ui/util"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	MessageHistoryDialogPage util.Page = "MessageHistoryDialog"
)

// MessageHistoryDialog lists the messages shown in the status bar, the newest first, with their full text.
type MessageHistoryDialog struct {
	layout        *tview.Flex
	actionChannel chan DialogActionId
}

// NewMessageHistoryDialog shows messages, the newest first (see status_message.Center.History).
func NewMessageHistoryDialog(messages []*status_message.StatusMessage) *MessageHistoryDialog {
	dialog := &MessageHistoryDialog{
		actionChannel: make(chan DialogActionId),
	}
	dialog.createLayout(messages)
	return dialog
}

func (d *MessageHistoryDialog) createLayout(messages []*status_message.StatusMessage) {
	dialogTitle := " 📜 Messages "

	messagesView := tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(true).
		SetWordWrap(true)
	messagesView.SetText(formatMessageHistory(messages))

	closeTextView := util.CreateAttentionTextView("Press 'esc' to close")

	dialogContent := tview.NewFlex().SetDirection(tview.FlexRow)
	dialogContent.AddItem(messagesView, 0, 1, true)
	dialogContent.AddItem(closeTextView, 1, 0, false)
	dialogContent.SetBorderPadding(0, 0, 1, 1)

	dialog := createModal(dialogTitle, dialogContent, DialogSizeConstraints{
		Title:             dialogTitle,
		ExtraContentWidth: 90,
		StaticHeight:      20,
	})
	dialog.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape || (event.Key() == tcell.KeyRune && (event.Rune() == 'q' || event.Rune() == 'm')) {
			d.Close()
			return nil
		}
		return event
	})
	d.layout = dialog
}

// formatMessageHistory returns one line per message, e.g. "12:03:15 ✖ open /home/x: permission denied", colored by
// its level.
func formatMessageHistory(messages []*status_message.StatusMessage) string {
	if len(messages) == 0 {
		return "No messages yet."
	}
	var text strings.Builder
	for i, message := range messages {
		if i > 0 {
			text.WriteString("\n")
		}
		fmt.Fprintf(&text, "[gray]%s[-] %s%s %s[-]",
			util.FormatTime(message.Time),
			txwidgets.ColorTag(message.Color()),
			message.Level.Icon(),
			tview.Escape(message.Message),
		)
	}
	return text.String()
}

func (d *MessageHistoryDialog) GetName() string {
	return string(MessageHistoryDialogPage)
}

func (d *MessageHistoryDialog) GetLayout() *tview.Flex {
	return d.layout
}

func (d *MessageHistoryDialog) GetActionChannel() <-chan DialogActionId {
	return d.actionChannel
}

func (d *MessageHistoryDialog) Close() {
	go func() {
		d.actionChannel <- DialogCloseActionId
	}()
}
