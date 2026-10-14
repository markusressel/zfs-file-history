package util

import (
	"strings"
	"zfs-file-history/internal/ui/clickable"
	"zfs-file-history/internal/ui/theme"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// minLeftFooterWidth is the minimum width (including padding) of the left part of a footer next to the right part.
// If less space is left, the right part is hidden.
const minLeftFooterWidth = 12

// BorderFooter draws texts into the bottom border of a box: one right-aligned (e.g. counts) and one left-aligned
// (e.g. the filter of a table). tview has no native support for this. It does not take up any space of the box content.
//
// The parts never overlap: the right part keeps its width and the left part gets the remaining space. If that leaves
// less than minLeftFooterWidth for the left part, the right part is hidden. Parts that do not fit are cut off with an
// ellipsis.
//
// The texts may contain tview style tags. Text without tags uses the footer color.
// All methods must be called on the UI thread, which is where the footer is drawn.
type BorderFooter struct {
	box      *tview.Box
	text     string
	leftFunc func(maxWidth int) string
	color    tcell.Color
	// parts are the parts of text, if set with SetParts
	parts []FooterPart
	// regions are the clickable parts, as last drawn
	regions clickable.Regions
}

// FooterPart is a part of the right-aligned footer text, see SetParts. One with OnClick is highlighted while below
// the mouse and runs OnClick (on the UI thread) when clicked.
type FooterPart struct {
	// Text may contain style tags
	Text    string
	OnClick func()
}

// NewBorderFooter installs a footer on the given box, which must have a border.
// Install it only once per box, and keep the returned footer to change its text.
// A draw func that the box already has is kept and called first.
func NewBorderFooter(box *tview.Box) *BorderFooter {
	footer := &BorderFooter{
		box:   box,
		color: theme.Colors.ShortcutMap.Name,
	}

	previousDrawFunc := box.GetDrawFunc()
	box.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		// tview uses the return value as the inner rect of the box.
		// An inner x of -1 makes tview compute its default inner rect (border and padding) on demand,
		// just like when a box has no draw func.
		innerX, innerY, innerWidth, innerHeight := -1, 0, 0, 0
		if previousDrawFunc != nil {
			innerX, innerY, innerWidth, innerHeight = previousDrawFunc(screen, x, y, width, height)
		}
		footer.draw(screen, x, y, width, height)
		return innerX, innerY, innerWidth, innerHeight
	})

	return footer
}

// SetText sets the right-aligned footer text. An empty text hides it.
func (f *BorderFooter) SetText(text string) {
	f.text = text
	f.parts = nil
}

// SetParts sets the right-aligned footer text from parts, some of which may be clicked (see HandleMouse). They are
// only clickable while the whole text fits.
func (f *BorderFooter) SetParts(parts ...FooterPart) {
	var text strings.Builder
	for _, part := range parts {
		text.WriteString(part.Text)
	}
	f.text = text.String()
	f.parts = parts
}

// HandleMouse highlights the clickable part below the mouse and runs it when clicked, see clickable.Regions. It
// only consumes events on the bottom border. enabled is whether the footer reacts to the mouse (e.g. not while a
// dialog is in front).
func (f *BorderFooter) HandleMouse(action tview.MouseAction, event *tcell.EventMouse, enabled bool) (consumed bool) {
	x, y := event.Position()
	boxX, boxY, width, height := f.box.GetRect()
	onFooter := y == boxY+height-1 && x >= boxX && x < boxX+width
	consumed, _ = f.regions.HandleMouse(action, event, enabled && onFooter)
	return consumed
}

// GetText returns the right-aligned footer text, including style tags.
func (f *BorderFooter) GetText() string {
	return f.text
}

// SetLeftText sets the left-aligned footer text. An empty text hides it.
func (f *BorderFooter) SetLeftText(text string) {
	if text == "" {
		f.leftFunc = nil
		return
	}
	f.leftFunc = func(int) string { return text }
}

// SetLeftFunc sets a function that returns the left-aligned footer text when the footer is drawn,
// for content that depends on the available width (e.g. an input line that scrolls to its cursor).
// maxWidth is the width available for the text (without padding). Longer texts are cut off.
// An empty text hides the left part. A nil func removes it.
func (f *BorderFooter) SetLeftFunc(leftFunc func(maxWidth int) string) {
	f.leftFunc = leftFunc
}

// SetColor sets the color of footer text without style tags.
func (f *BorderFooter) SetColor(color tcell.Color) {
	f.color = color
}

// draw prints the footer into the bottom border, leaving the corners intact.
func (f *BorderFooter) draw(screen tcell.Screen, x, y, width, height int) {
	f.regions.Reset()
	if width < 4 || height < 2 {
		return
	}
	defer f.regions.DrawHover(screen)
	bottomY := y + height - 1
	availableWidth := width - 2 // between the corners

	right := ""
	if f.text != "" {
		right = theme.CreateTitleText(f.text)
	}
	rightWidth := tview.TaggedStringWidth(right)

	left := ""
	if f.leftFunc != nil {
		left = f.leftFunc(availableWidth - 2)
	}
	if left == "" {
		f.drawPart(screen, right, x+1, bottomY, availableWidth, tview.AlignRight)
		f.addRegions(x+1, bottomY, availableWidth, rightWidth)
		return
	}

	leftWidth := availableWidth
	if right != "" {
		// keep at least one border character between the parts
		remainingWidth := availableWidth - rightWidth - 1
		if remainingWidth >= minLeftFooterWidth {
			leftWidth = remainingWidth
			f.drawPart(screen, right, x+1, bottomY, availableWidth, tview.AlignRight)
			f.addRegions(x+1, bottomY, availableWidth, rightWidth)
		}
		// otherwise the right part is hidden
	}

	left = f.leftFunc(leftWidth - 2)
	f.drawPart(screen, theme.CreateTitleText(left), x+1, bottomY, leftWidth, tview.AlignLeft)
}

// addRegions adds the clickable parts of the right text, drawn right-aligned within maxWidth from x. Nothing if it
// was cut off.
func (f *BorderFooter) addRegions(x int, y int, maxWidth int, rightWidth int) {
	if rightWidth > maxWidth {
		return
	}
	// behind the space of theme.CreateTitleText
	partX := x + maxWidth - rightWidth + 1
	for i, part := range f.parts {
		partWidth := tview.TaggedStringWidth(part.Text)
		if part.OnClick != nil {
			f.regions.Add(clickable.Region{
				X: partX, Y: y, Width: partWidth, Key: i,
				HoverBackground: theme.Colors.Layout.FooterHoverBackground,
				OnClick:         part.OnClick,
			})
		}
		partX += partWidth
	}
}

// drawPart prints text within maxWidth. If it does not fit, the beginning is shown, followed by an ellipsis.
func (f *BorderFooter) drawPart(screen tcell.Screen, text string, x, y, maxWidth int, align int) {
	if text == "" || maxWidth <= 0 {
		return
	}
	if tview.TaggedStringWidth(text) <= maxWidth {
		tview.Print(screen, text, x, y, maxWidth, align, f.color)
		return
	}
	tview.Print(screen, text, x, y, maxWidth-1, tview.AlignLeft, f.color)
	tview.Print(screen, string(tview.SemigraphicsHorizontalEllipsis), x+maxWidth-1, y, 1, tview.AlignLeft, f.color)
}
