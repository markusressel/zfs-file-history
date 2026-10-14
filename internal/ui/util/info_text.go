package util

import (
	"strings"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// InfoState is the state of what an info pane describes, or of one of its parts, see InfoText.
type InfoState int

const (
	// InfoNone is a part that tells something, but is neither good nor bad (e.g. read-only)
	InfoNone InfoState = iota
	// InfoUnknown is a part that could not be found out, e.g. SMART values that need root: the dot is gray unless
	// something else is wrong
	InfoUnknown
	InfoOk
	InfoWarning
	InfoError
)

// Color returns the color of the state, the one of values for InfoNone.
func (state InfoState) Color() tcell.Color {
	switch state {
	case InfoOk:
		return theme.Colors.Info.Ok
	case InfoWarning:
		return theme.Colors.Info.Warning
	case InfoError:
		return theme.Colors.Info.Error
	case InfoUnknown:
		return theme.Colors.Info.Muted
	default:
		return theme.Colors.Info.Value
	}
}

// InfoPart is a part of the verdict of an info pane, e.g. "92% full" (a warning), see InfoText.Verdict.
type InfoPart struct {
	Text  string
	State InfoState
}

// InfoText builds the text of a pane that describes one thing (the dataset info, the details of a disk). It answers
// the question whether all is well first: a verdict line, colored by the worst of its parts, then the values in
// sections. Labels are dim and values bright, so the eye lands on the values; color only tells states, never the kind
// of a value. The labels of all sections are aligned, the text is not indented, so it is short and narrow.
type InfoText struct {
	lines []infoLine
}

// infoLine is a line of an InfoText: text as is (e.g. the verdict), a section heading, or a value with its label.
type infoLine struct {
	text      string
	section   string
	label     string
	value     string
	isSection bool
	isField   bool
}

// Verdict adds the line that says how the thing is: a dot in the color of the worst part (green if nothing is wrong
// or unknown), then the parts, the problems first. Texts are escaped.
func (info *InfoText) Verdict(parts ...InfoPart) {
	worst := InfoOk
	unknown := false
	for _, part := range parts {
		worst = max(worst, part.State)
		unknown = unknown || part.State == InfoUnknown
	}
	if worst == InfoOk && unknown {
		worst = InfoUnknown
	}
	ordered := make([]InfoPart, 0, len(parts))
	for state := InfoError; state >= InfoNone; state-- {
		for _, part := range parts {
			if part.State == state {
				ordered = append(ordered, part)
			}
		}
	}
	var texts []string
	for _, part := range ordered {
		color := theme.Colors.Info.Value
		if part.State == InfoWarning || part.State == InfoError || part.State == InfoUnknown {
			color = part.State.Color()
		}
		texts = append(texts, txwidgets.Span(color, "%s", part.Text))
	}
	info.Line(txwidgets.Span(worst.Color(), "●") + " " + strings.Join(texts, txwidgets.Span(theme.Colors.Info.Muted, " · ")))
}

// Line adds a line as is, which may contain style tags.
func (info *InfoText) Line(text string) {
	info.lines = append(info.lines, infoLine{text: text})
}

// Section adds the heading of the values below it, after an empty line (unless it is the first line, e.g. of a column,
// see SideBySide).
func (info *InfoText) Section(title string) {
	if len(info.lines) > 0 {
		info.lines = append(info.lines, infoLine{text: ""})
	}
	info.lines = append(info.lines, infoLine{section: title, isSection: true})
}

// SideBySide adds the lines of the columns next to each other, gap columns apart, e.g. two sections on a wide pane.
func (info *InfoText) SideBySide(gap int, columns ...*InfoText) {
	lines := make([][]string, len(columns))
	widths := make([]int, len(columns))
	height := 0
	for i, column := range columns {
		lines[i] = column.Lines()
		height = max(height, len(lines[i]))
		for _, line := range lines[i] {
			widths[i] = max(widths[i], tview.TaggedStringWidth(line))
		}
	}
	for row := range height {
		var text strings.Builder
		for i := range columns {
			line := ""
			if row < len(lines[i]) {
				line = lines[i][row]
			}
			text.WriteString(line)
			if i < len(columns)-1 {
				text.WriteString(strings.Repeat(" ", widths[i]+gap-tview.TaggedStringWidth(line)))
			}
		}
		info.Line(strings.TrimRight(text.String(), " "))
	}
}

// Field adds a value with its label. The value may contain style tags (white if it has none, see InfoValue) and
// line breaks, its further lines are aligned below the first one.
func (info *InfoText) Field(label string, value string) {
	for i, line := range strings.Split(value, "\n") {
		if i > 0 {
			label = ""
		}
		info.lines = append(info.lines, infoLine{label: label, value: line, isField: true})
	}
}

// String returns the text, the labels padded to the longest one.
func (info *InfoText) String() string {
	return strings.Join(info.Lines(), "\n")
}

// Lines returns the lines of the text, see String.
func (info *InfoText) Lines() []string {
	labelWidth := 0
	for _, line := range info.lines {
		if line.isField {
			labelWidth = max(labelWidth, tview.TaggedStringWidth(line.label))
		}
	}
	colors := theme.Colors.Info
	lines := make([]string, 0, len(info.lines))
	for _, line := range info.lines {
		switch {
		case line.isSection:
			lines = append(lines, "[::b]"+txwidgets.Span(colors.Section, "%s", line.section)+"[::-]")
		case line.isField:
			padding := strings.Repeat(" ", labelWidth-tview.TaggedStringWidth(line.label))
			lines = append(lines, txwidgets.Span(colors.Label, "%s", line.label)+padding+"  "+txwidgets.ColorTag(colors.Value)+line.value+"[-]")
		default:
			lines = append(lines, line.text)
		}
	}
	return lines
}

// InfoValue returns the text in the color of values (escaped).
func InfoValue(text string) string {
	return txwidgets.Span(theme.Colors.Info.Value, "%s", text)
}

// InfoMuted returns the text in the color of values that say little, e.g. a unit or an exact value behind a rounded
// one (escaped).
func InfoMuted(text string) string {
	return txwidgets.Span(theme.Colors.Info.Muted, "%s", text)
}

// partialBarBlocks are the characters of a cell of a bar filled from the left by 1 to 7 eighths, see InfoBar.
var partialBarBlocks = []rune("▏▎▍▌▋▊▉")

// InfoBar returns a bar of the width filled to the fraction (0 to 1), in eighths of a cell, colored by how full it
// is (see theme.Colors.Info.BarFilled).
func InfoBar(fraction float64, width int) string {
	fraction = min(max(fraction, 0), 1)
	colors := theme.Colors.Info
	filled := GradientColor(colors.BarFilled, fraction, colors.Ok)
	eighths := int(fraction*float64(width*8) + 0.5)
	if fraction > 0 && eighths == 0 {
		// something is used, so something is shown
		eighths = 1
	}
	var bar strings.Builder
	fullCells, partial := eighths/8, eighths%8
	if fullCells > 0 {
		bar.WriteString(txwidgets.ColorTag(filled) + strings.Repeat("█", fullCells))
	}
	empty := width - fullCells
	if partial > 0 {
		bar.WriteString(txwidgets.ColorsTag(filled, colors.BarEmpty) + string(partialBarBlocks[partial-1]))
		empty--
	}
	if empty > 0 {
		bar.WriteString(txwidgets.ColorsTag(colors.BarEmpty, colors.BarEmpty) + strings.Repeat(" ", empty))
	}
	bar.WriteString("[-:-]")
	return bar.String()
}
