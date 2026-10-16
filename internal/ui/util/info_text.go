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
	// isVerdict is set on the verdict line, whose further lines are indented below its first part, see WrappedLines
	isVerdict bool
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
	info.lines = append(info.lines, infoLine{
		text:      txwidgets.Span(worst.Color(), "●") + " " + strings.Join(texts, txwidgets.Span(theme.Colors.Info.Muted, " · ")),
		isVerdict: true,
	})
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

// infoLabelGap separates the labels from the values.
const infoLabelGap = "  "

// LineCount returns the number of lines added so far, e.g. the line of the next field.
func (info *InfoText) LineCount() int {
	return len(info.lines)
}

// ValueColumn returns the column at which the values of the fields start (the labels are padded to the longest one,
// see Lines), e.g. to find a value on the screen.
func (info *InfoText) ValueColumn() int {
	return info.labelWidth() + len(infoLabelGap)
}

// labelWidth returns the width of the longest label, to which all are padded.
func (info *InfoText) labelWidth() int {
	width := 0
	for _, line := range info.lines {
		if line.isField {
			width = max(width, tview.TaggedStringWidth(line.label))
		}
	}
	return width
}

// String returns the text, the labels padded to the longest one.
func (info *InfoText) String() string {
	return strings.Join(info.Lines(), "\n")
}

// Lines returns the lines of the text, see String.
func (info *InfoText) Lines() []string {
	labelWidth := info.labelWidth()
	colors := theme.Colors.Info
	lines := make([]string, 0, len(info.lines))
	for _, line := range info.lines {
		switch {
		case line.isSection:
			lines = append(lines, "[::b]"+txwidgets.Span(colors.Section, "%s", line.section)+"[::-]")
		case line.isField:
			lines = append(lines, fieldPrefix(line.label, labelWidth)+line.value+"[-]")
		default:
			lines = append(lines, line.text)
		}
	}
	return lines
}

// Width returns the width of the widest line, see Lines.
func (info *InfoText) Width() int {
	width := 0
	for _, line := range info.Lines() {
		width = max(width, tview.TaggedStringWidth(line))
	}
	return width
}

// verdictIndent is the width of the dot of the verdict and the space after it, see Verdict.
const verdictIndent = 2

// WrappedLines returns the lines of the text like Lines, but wraps the lines wider than width (e.g. a long error on a
// narrow pane) instead of letting the pane cut them off. A value is wrapped next to its label, its further lines
// aligned below its first one, so the labels stay readable as a column; where that leaves it less than half of the
// width, it is below its label. The verdict is wrapped below its first part. A width of 0 or less does not wrap.
func (info *InfoText) WrappedLines(width int) []string {
	lines, _ := info.Wrap(width)
	return lines
}

// InfoPosition is where the value of a line of an InfoText is after wrapping (see Wrap): the wrapped line its value
// starts on, and the column.
type InfoPosition struct {
	Line, Column int
}

// Wrap returns the lines of WrappedLines, and for each line added to the text where its value starts (for lines that
// are not fields, where the line starts), e.g. to find a value that can be clicked.
func (info *InfoText) Wrap(width int) ([]string, []InfoPosition) {
	lines := info.Lines()
	labelWidth := info.labelWidth()
	valueColumn := info.ValueColumn()
	wrapped := make([]string, 0, len(lines))
	positions := make([]InfoPosition, len(lines))
	for i, line := range lines {
		field := info.lines[i]
		switch {
		case width <= 0 || tview.TaggedStringWidth(line) <= width:
			if field.isField {
				positions[i] = InfoPosition{Line: len(wrapped), Column: valueColumn}
			} else {
				positions[i] = InfoPosition{Line: len(wrapped)}
			}
			wrapped = append(wrapped, line)
		case field.isField && width-valueColumn >= width/2:
			positions[i] = InfoPosition{Line: len(wrapped), Column: valueColumn}
			parts := wrapLine(field.value, width-valueColumn, 0)
			wrapped = append(wrapped, fieldPrefix(field.label, labelWidth)+parts[0])
			padding := strings.Repeat(" ", valueColumn)
			for _, part := range parts[1:] {
				wrapped = append(wrapped, padding+part)
			}
			wrapped[len(wrapped)-1] += "[-]"
		case field.isField:
			// a value squeezed into a few columns is harder to read than one below its label
			if field.label != "" {
				wrapped = append(wrapped, txwidgets.Span(theme.Colors.Info.Label, "%s", field.label))
			}
			positions[i] = InfoPosition{Line: len(wrapped)}
			wrapped = append(wrapped, wrapLine(txwidgets.ColorTag(theme.Colors.Info.Value)+field.value+"[-]", width, 0)...)
		case field.isVerdict:
			positions[i] = InfoPosition{Line: len(wrapped)}
			wrapped = append(wrapped, wrapLine(line, width, verdictIndent)...)
		default:
			positions[i] = InfoPosition{Line: len(wrapped)}
			wrapped = append(wrapped, wrapLine(line, width, 0)...)
		}
	}
	return wrapped, positions
}

// wrapLine wraps the line (with style tags) at width, the further lines indented by indent columns. They take the
// style the line had where it was split: tview keeps it across line breaks.
func wrapLine(line string, width int, indent int) []string {
	if tview.TaggedStringWidth(line) <= width {
		return []string{line}
	}
	first := tview.WordWrap(line, width)[0]
	rest := strings.TrimLeft(line[len(first):], " ")
	lines := []string{strings.TrimRight(first, " ")}
	padding := strings.Repeat(" ", indent)
	for _, part := range tview.WordWrap(rest, width-indent) {
		lines = append(lines, padding+strings.TrimRight(part, " "))
	}
	return lines
}

// fieldPrefix returns the label of a field padded to labelWidth, and the gap to its value, which starts in the color
// of values (closed by "[-]" after it).
func fieldPrefix(label string, labelWidth int) string {
	padding := strings.Repeat(" ", labelWidth-tview.TaggedStringWidth(label))
	return txwidgets.Span(theme.Colors.Info.Label, "%s", label) + padding + infoLabelGap + txwidgets.ColorTag(theme.Colors.Info.Value)
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

// partialBarBlocks are the characters of a cell of a bar filled from the left by 1 to 7 eighths, see InfoStackedBar.
var partialBarBlocks = []rune("▏▎▍▌▋▊▉")

// InfoBar returns a bar of the width filled to the fraction (0 to 1), in eighths of a cell, colored by how full it
// is (see theme.Colors.Info.BarFilled).
func InfoBar(fraction float64, width int) string {
	fraction = min(max(fraction, 0), 1)
	colors := theme.Colors.Info
	return InfoStackedBar([]BarSegment{{Fraction: fraction, Color: GradientColor(colors.BarFilled, fraction, colors.Ok)}}, width)
}

// BarSegment is a part of a bar, see InfoStackedBar.
type BarSegment struct {
	// Fraction is how much of the bar it fills (0 to 1)
	Fraction float64
	Color    tcell.Color
}

// InfoStackedBar returns a bar of the width with the segments one after the other from the left, in eighths of a
// cell, the rest empty (theme.Colors.Info.BarEmpty). A segment that is not empty fills at least an eighth, so it
// shows. A cell shows two colors at most: a segment that starts and ends within a cell another one ends in is lost.
func InfoStackedBar(segments []BarSegment, width int) string {
	total := width * 8
	// ends are where the segments end, in eighths from the left
	ends := make([]int, len(segments))
	sum, previous := 0.0, 0
	for i, segment := range segments {
		sum += max(segment.Fraction, 0)
		end := min(int(sum*float64(total)+0.5), total)
		if segment.Fraction > 0 {
			// something is used, so something is shown
			end = max(end, min(previous+1, total))
		}
		end = max(end, previous)
		ends[i], previous = end, end
	}
	empty := theme.Colors.Info.BarEmpty
	// colorAt returns the color of the eighth
	colorAt := func(eighth int) tcell.Color {
		for i, end := range ends {
			if eighth < end {
				return segments[i].Color
			}
		}
		return empty
	}
	var bar strings.Builder
	lastTag := ""
	write := func(tag string, block rune) {
		if tag != lastTag {
			bar.WriteString(tag)
			lastTag = tag
		}
		bar.WriteRune(block)
	}
	for cell := range width {
		left := colorAt(cell * 8)
		// the first segment that ends within the cell splits it
		split := 0
		for _, end := range ends {
			if end > cell*8 && end < cell*8+8 {
				split = end - cell*8
				break
			}
		}
		switch {
		case (split == 0 || colorAt(cell*8+split) == left) && left == empty:
			// a space, so copied text has no blocks where nothing is
			write(txwidgets.ColorsTag(empty, empty), ' ')
			continue
		case split == 0 || colorAt(cell*8+split) == left:
			write(txwidgets.ColorsTag(left, left), '█')
			continue
		}
		write(txwidgets.ColorsTag(left, colorAt(cell*8+split)), partialBarBlocks[split-1])
	}
	bar.WriteString("[-:-]")
	return bar.String()
}
