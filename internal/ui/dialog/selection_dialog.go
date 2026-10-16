package dialog

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"zfs-file-history/internal/ui/scrollbar"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/util"

	"github.com/rivo/tview"
)

type SelectionDialog struct {
	application *tview.Application
	name        string
	title       string
	description string
	// tagged is whether description contains style tags, see newTaggedSelectionDialog
	tagged        bool
	options       []*DialogOption
	layout        *tview.Flex
	actionChannel chan DialogActionId

	optionTable *tview.Table
	// checklist are the items above the options, nil if there is no checklist, see NewSelectionDialogWithChecklist
	checklist      []*ChecklistItem
	checklistTable *tview.Table
	// checked is whether each item of the checklist was checked when the running action was selected, see Checked
	checked []bool
	// stopLoading stops the loading spinner of the running action, nil if no action is running.
	// Only accessed on the UI thread.
	stopLoading func()

	// Handlers for exclusive async execution
	handler    func(d *SelectionDialog, action DialogActionId) error
	onComplete func(d *SelectionDialog, option *DialogOption, err error)

	// pages the dialog is shown on, set by ShowDialogOnPages. Only accessed on the UI thread.
	pages *tview.Pages
	// frame is the bordered window of the dialog, see SetDanger
	frame *tview.Flex
}

func (d *SelectionDialog) setPages(pages *tview.Pages) {
	d.pages = pages
}

// RetryFunc returns a function that shows this dialog again and runs option again, e.g. after the user granted
// missing permissions. The returned function must be called on the UI thread, after this dialog was closed.
func (d *SelectionDialog) RetryFunc(option *DialogOption) func() {
	return func() {
		if d.pages == nil {
			return
		}
		ShowDialogOnPages(d.application, d.pages, d, nil)
		d.selectAction(option)
	}
}

// SetOptionName changes the displayed name of the option with the given id. Must be called on the UI thread.
func (d *SelectionDialog) SetOptionName(id DialogActionId, name string) {
	for row := 0; row < d.optionTable.GetRowCount(); row++ {
		cell := d.optionTable.GetCell(row, 1)
		if option, ok := cell.GetReference().(*DialogOption); ok && option.Id == id {
			option.Name = name
			cell.SetText(name)
		}
	}
}

// ShowFollowUp closes this dialog and shows next on the same pages, e.g. the result of an action.
// Must be called on the UI thread.
func (d *SelectionDialog) ShowFollowUp(next Dialog) {
	pages := d.pages
	if pages == nil {
		d.Close()
		return
	}
	d.Chain(func() { ShowDialogOnPages(d.application, pages, next, nil) })
}

// NewSelectionDialog
// handler - The background execution logic to be executed when the user selects an option.
// onComplete - The callback to be executed on the UI thread after the background execution completes.
func NewSelectionDialog(
	application *tview.Application,
	name string,
	title string,
	description string,
	options []*DialogOption,
	handler func(dialog *SelectionDialog, action DialogActionId) error,
	onComplete func(d *SelectionDialog, option *DialogOption, err error),
) *SelectionDialog {
	return NewSelectionDialogWithChecklist(application, name, title, description, nil, options, handler, onComplete)
}

// NewSelectionDialogWithChecklist is NewSelectionDialog with a checklist between the description and the options,
// e.g. to choose what an option applies to: the handler gets the checked items with Checked.
func NewSelectionDialogWithChecklist(
	application *tview.Application,
	name string,
	title string,
	description string,
	checklist []*ChecklistItem,
	options []*DialogOption,
	handler func(dialog *SelectionDialog, action DialogActionId) error,
	onComplete func(d *SelectionDialog, option *DialogOption, err error),
) *SelectionDialog {
	return newSelectionDialog(application, name, title, description, false, checklist, options, handler, onComplete)
}

// newTaggedSelectionDialog is NewSelectionDialogWithChecklist with a description that contains style tags, e.g. names
// made to stand out with Highlight. Everything else in it must be escaped.
func newTaggedSelectionDialog(
	application *tview.Application,
	name string,
	title string,
	description string,
	checklist []*ChecklistItem,
	options []*DialogOption,
	handler func(dialog *SelectionDialog, action DialogActionId) error,
	onComplete func(d *SelectionDialog, option *DialogOption, err error),
) *SelectionDialog {
	return newSelectionDialog(application, name, title, description, true, checklist, options, handler, onComplete)
}

func newSelectionDialog(
	application *tview.Application,
	name string,
	title string,
	description string,
	tagged bool,
	checklist []*ChecklistItem,
	options []*DialogOption,
	handler func(dialog *SelectionDialog, action DialogActionId) error,
	onComplete func(d *SelectionDialog, option *DialogOption, err error),
) *SelectionDialog {
	d := &SelectionDialog{
		tagged:        tagged,
		checklist:     checklist,
		application:   application,
		name:          name,
		title:         title,
		description:   description,
		options:       options,
		actionChannel: make(chan DialogActionId),
		handler:       handler,
		onComplete:    onComplete,
	}
	d.createLayout()
	return d
}

func (d *SelectionDialog) createLayout() {
	// the prefixes ("1.", "Esc.") are a column of their own, as wide as the widest one; names are measured in
	// cells, an emoji takes two
	prefixWidth := 0
	for i, opt := range d.options {
		prefix := fmt.Sprintf("%d.", i+1)
		if opt.Id == DialogCloseActionId {
			prefix = "Esc."
		}
		prefixWidth = max(prefixWidth, len(prefix))
	}
	maxOptWidth := 0
	for _, opt := range d.options {
		maxOptWidth = max(maxOptWidth, prefixWidth+1+tview.TaggedStringWidth(opt.Name))
	}

	maxOptWidth = max(maxOptWidth, checklistWidth(d.checklist))

	actualTableRows := len(d.options)
	if len(d.options) > 1 {
		hasClose := false
		for _, opt := range d.options {
			if opt.Id == DialogCloseActionId {
				hasClose = true
				break
			}
		}
		if hasClose {
			actualTableRows++
		}
	}

	// the size is calculated from the text as shown
	plainDescription := d.description
	if d.tagged {
		plainDescription = withoutTags(d.description)
	}
	constraints := DialogSizeConstraints{
		Title:             d.title,
		Description:       plainDescription,
		ExtraContentWidth: maxOptWidth,
		StaticHeight:      1 + actualTableRows,
	}
	if len(d.checklist) > 0 {
		constraints.StaticHeight += len(d.checklist) + 1
	}

	// a scrollbar shows that more follows: the description gets what is left of a dialog that does not fit (e.g. a
	// long list of what would be destroyed on a small terminal), so the checklist and the options are always shown
	textDescriptionView := scrollbar.NewTextView(nil)
	textDescriptionView.SetDynamicColors(d.tagged).
		SetText(d.description).
		SetWrap(true).
		SetWordWrap(true)

	d.optionTable = createOptionTable(d.application, d.options, d.selectAction)

	dialogContent := tview.NewFlex().SetDirection(tview.FlexRow)
	// like the other dialogs; CalculateDialogSize leaves room for it
	dialogContent.SetBorderPadding(0, 0, 1, 1)
	dialogContent.AddItem(textDescriptionView, 0, 1, false)
	dialogContent.AddItem(tview.NewBox(), 1, 0, false)
	if len(d.checklist) > 0 {
		d.checklistTable = d.createChecklistTable()
		dialogContent.AddItem(d.checklistTable, len(d.checklist), 0, false)
		dialogContent.AddItem(tview.NewBox(), 1, 0, false)
	}
	dialogContent.AddItem(d.optionTable, actualTableRows, 0, true)

	dialog, frame := createModalWithFrame(d.title, dialogContent, constraints)
	d.frame = frame
	inputCapture := createOptionDialogInputCapture(d.optionTable, d.options, d.selectAction, d.Close)
	if d.checklistTable != nil {
		inputCapture = d.checklistInputCapture(inputCapture)
	}
	dialog.SetInputCapture(inputCapture)
	d.layout = dialog
}

// SetDanger shows the dialog with a red border: it confirms an action that loses data. Must be called on the UI
// thread.
func (d *SelectionDialog) SetDanger() *SelectionDialog {
	d.frame.SetBorderColor(theme.Colors.Dialog.DangerBorder)
	return d
}

func (d *SelectionDialog) GetName() string {
	return d.name
}

func (d *SelectionDialog) GetLayout() *tview.Flex {
	return d.layout
}

func (d *SelectionDialog) GetActionChannel() <-chan DialogActionId {
	return d.actionChannel
}

func (d *SelectionDialog) Close() {
	// Bypass emitDialogActions' goroutine to guarantee the close signal
	// is processed synchronously if the listener is ready.
	select {
	case d.actionChannel <- DialogCloseActionId:
	default:
		go func() { d.actionChannel <- DialogCloseActionId }()
	}
}

// Chain safely orchestrates closing the current dialog and opening a new one.
// It prevents the "Dead Focus Pointer" bug by guaranteeing the current dialog
// fully unmounts and restores its focus before the next dialog captures its fallback.
func (d *SelectionDialog) Chain(mountNext func()) {
	d.Close()
	go func() {
		// A microscopic pause gives the background listener time to process the close event
		time.Sleep(10 * time.Millisecond)
		d.application.QueueUpdateDraw(mountNext)
	}()
}

func (d *SelectionDialog) selectAction(option *DialogOption) {
	if option.Id == DialogCloseActionId {
		d.Close()
		return
	}

	// read by the handler in the background, see Checked
	d.checked = make([]bool, len(d.checklist))
	for i, item := range d.checklist {
		d.checked[i] = item.Checked
	}

	// 1. Always trigger the loading state and background routine
	d.ShowLoading(option)

	go func() {
		var err error

		// 2. Safely execute the handler only if it was provided
		if d.handler != nil {
			err = d.handler(d, option.Id)
		}

		// 3. Queue the completion logic back to the main UI thread
		d.application.QueueUpdateDraw(func() {
			d.StopLoading()
			if d.onComplete != nil {
				d.onComplete(d, option, err)
			}
		})
	}()
}

// ShowLoading locks the options and shows a spinner next to the given option.
// Must be called on the UI thread.
func (d *SelectionDialog) ShowLoading(option *DialogOption) {
	d.StopLoading()                           // stop the spinner of a previous action, if any
	d.optionTable.SetSelectable(false, false) // Lock input

	var targetRow, targetCol int
	var originalText string
	found := false

	// Safely find the specific table cell by matching the exact memory reference
	for r := 0; r < d.optionTable.GetRowCount(); r++ {
		cell := d.optionTable.GetCell(r, 1) // 1 is the name column
		if cell != nil && cell.GetReference() == option {
			targetRow = r
			targetCol = 1
			originalText = cell.Text
			found = true
			break
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	d.stopLoading = func() {
		cancel()
		if !found {
			return
		}
		// Restore the original text (on the UI thread, like the spinner updates)
		cell := d.optionTable.GetCell(targetRow, targetCol)
		if cell != nil {
			cell.SetText(originalText)
		}
	}

	if !found {
		return
	}

	frames := util.SpinnerFrames

	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		frameIdx := 0

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}

			frame := frames[frameIdx]
			d.application.QueueUpdateDraw(func() {
				// checked on the UI thread, so a frame never overwrites the text restored by StopLoading
				if ctx.Err() != nil {
					return
				}
				cell := d.optionTable.GetCell(targetRow, targetCol)
				if cell != nil {
					cell.SetText(fmt.Sprintf("%s %s", originalText, frame))
				}
			})

			frameIdx = (frameIdx + 1) % len(frames)
		}
	}()
}

// StopLoading stops the spinner, restores the option text and unlocks the options.
// Must be called on the UI thread.
func (d *SelectionDialog) StopLoading() {
	if d.stopLoading != nil {
		d.stopLoading()
		d.stopLoading = nil
	}
	d.optionTable.SetSelectable(true, false) // Unlock input
}

// OptionCommands returns a command for each option of the dialog (but close and skipped ones) for the command menu
// (see CommandMenu): it shows the dialog with show and selects the option, as if it was chosen in the dialog. Skip
// the options that are commands of their own already, e.g. because they have a shortcut.
func (d *SelectionDialog) OptionCommands(show func(d Dialog), skip ...DialogActionId) []shortcut_helper.ShortcutEntry {
	var commands []shortcut_helper.ShortcutEntry
	for _, option := range d.options {
		if option.Id == DialogCloseActionId || slices.Contains(skip, option.Id) {
			continue
		}
		commands = append(commands, shortcut_helper.ShortcutEntry{
			Name:        withoutIcon(option.Name),
			Description: option.Description,
			Run: func() {
				show(d)
				d.selectAction(option)
			},
			MenuOnly: true,
		})
	}
	return commands
}

// withoutIcon returns the name of an option without its leading icon, e.g. "Create Snapshot" for "📸 Create Snapshot".
func withoutIcon(name string) string {
	first, rest, found := strings.Cut(name, " ")
	if !found || strings.ContainsFunc(first, isWordRune) {
		return name
	}
	return strings.TrimSpace(rest)
}
