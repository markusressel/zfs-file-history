package dialog

import (
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/localization"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/util"

	"github.com/rivo/tview"
)

const PreviewDialogPage util.Page = "PreviewDialog"

// PreviewDialog shows that what an action that loses data would do is being checked (e.g. the dry run of destroying a
// dataset), in place of its confirmation: with its title and red border, a spinner, and what is checked. It is shown
// as soon as the action is chosen, so the action dialog does not show a spinner, which looked like the action itself
// was running without a confirmation. Once the check is done, the confirmation replaces it (see Done); Cancel (Esc)
// closes it, and the result of the check is dropped.
type PreviewDialog struct {
	application   *tview.Application
	layout        *tview.Flex
	loading       *util.LoadingView
	actionChannel chan DialogActionId
	// closed is set once the dialog was closed, by Cancel or Done. Only accessed on the UI thread.
	closed bool
}

// NewPreviewDialog creates a PreviewDialog with the title of the confirmation that follows, and message saying what
// is checked, e.g. "Dry run of destroying tank/data: listing what would be destroyed. Nothing is destroyed yet.".
// The spinner runs until the dialog is closed. Must be called on the UI thread.
func NewPreviewDialog(application *tview.Application, title string, message string) *PreviewDialog {
	d := &PreviewDialog{
		application:   application,
		actionChannel: make(chan DialogActionId),
	}
	d.loading = util.NewLoadingView(application, "", message)
	d.loading.SetBorder(false)

	options := []*DialogOption{{Id: DialogCloseActionId, Name: localization.LocalizationCommonCancel}}
	optionTable := createOptionTable(application, options, func(*DialogOption) { d.Close() })

	content := tview.NewFlex().SetDirection(tview.FlexRow)
	content.SetBorderPadding(0, 0, 1, 1)
	// the spinner, a line, the message and a line around it, see LoadingView
	content.AddItem(d.loading, 0, 1, false)
	content.AddItem(optionTable, len(options), 0, true)

	layout, frame := createModalWithFrame(title, content, DialogSizeConstraints{
		Title:       title,
		Description: message,
		// the spinner and the lines around the message, and the option
		StaticHeight: 4 + len(options),
	})
	frame.SetBorderColor(theme.Colors.Dialog.DangerBorder)
	layout.SetInputCapture(createOptionDialogInputCapture(optionTable, options, func(*DialogOption) { d.Close() }, d.Close))
	d.layout = layout
	d.loading.Start()
	return d
}

func (d *PreviewDialog) GetName() string {
	return string(PreviewDialogPage)
}

func (d *PreviewDialog) GetLayout() *tview.Flex {
	return d.layout
}

func (d *PreviewDialog) GetActionChannel() <-chan DialogActionId {
	return d.actionChannel
}

// Close closes the dialog and stops the spinner, once. Must be called on the UI thread.
func (d *PreviewDialog) Close() {
	if d.closed {
		return
	}
	d.closed = true
	d.loading.Stop()
	// the listener of ShowDialogOnPages may not be ready yet
	go func() { d.actionChannel <- DialogCloseActionId }()
}

// Done closes the dialog once the check is done, so the confirmation (or why it failed) can be shown. Returns false if
// it was cancelled before, then nothing is to be shown. Must be called on the UI thread.
func (d *PreviewDialog) Done() bool {
	if d.closed {
		return false
	}
	d.Close()
	return true
}

// Preview is a check that runs before the confirmation of an action that loses data, see RunPreview. P is its result,
// e.g. what would be destroyed.
type Preview[P any] struct {
	// Title and Message are the ones of the PreviewDialog, see NewPreviewDialog
	Title, Message string
	// ErrorTitle is the title of the error dialog if the check fails, e.g. "Cannot Destroy"; Subject is what is
	// checked, for the log
	ErrorTitle, Subject string
	// Check runs in the background
	Check func() (P, error)
	// Confirm shows the confirmation of the result of Check, on the UI thread
	Confirm func(result P)
	// Retry starts over after an error, e.g. RunPreview with the same preview again
	Retry func()
}

// RunPreview shows a PreviewDialog on the host, runs the check in the background, and then shows the confirmation, or
// the error with a retry. Nothing follows if the dialog was cancelled meanwhile. Must be called on the UI thread.
func RunPreview[P any](host *Host, preview Preview[P]) {
	application := host.application
	checking := NewPreviewDialog(application, preview.Title, preview.Message)
	host.ShowDialog(checking, nil)
	go func() {
		result, err := preview.Check()
		application.QueueUpdateDraw(func() {
			if !checking.Done() {
				return // cancelled
			}
			if err != nil {
				logging.Error("%s %s: %s", preview.ErrorTitle, preview.Subject, err.Error())
				host.ShowDialog(NewErrorDialogWithRetry(application, preview.ErrorTitle, err, preview.Retry), nil)
				return
			}
			preview.Confirm(result)
		})
	}()
}
