package pool_activity

import (
	"fmt"
	"strings"
	"unicode/utf8"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// minBarWidthWithRemaining is the narrowest a progress bar gets before the time left is written into it instead
// of next to it, see layoutLines.
const minBarWidthWithRemaining = 20

// kindTitles are the titles of the kinds of activities that are not just capitalized, see kindTitle.
var kindTitles = map[string]string{
	"trim":   "TRIM",
	"remove": "Device removal",
	"expand": "RAIDZ expansion",
}

// Bar shows the operations running on the pools (see Monitor), one line each: what runs on which pool, a progress
// bar with the progress written into it, and the time left, e.g.
// " Resilver  tank  [ 31.8%  1.02T / 3.21T  294M/s        ]  ⧗    2h 10m". The columns are aligned, so all bars
// start and end at the same column. It has no height while nothing runs, see SetOnHeightChanged. A click on the time
// left shows the ETA instead (see Monitor.CycleTimeLeftMode), a click on the rest of a line runs the function set with
// SetOnClick. Only the part below the mouse is highlighted, see regionAt.
type Bar struct {
	*linePanel
	// monitor is only accessed on the UI thread
	monitor *Monitor
}

// NewBar returns a bar showing the activities of monitor. Must be called on the UI thread.
func NewBar(monitor *Monitor) *Bar {
	bar := &Bar{monitor: monitor}
	bar.linePanel = newLinePanel(func() int { return len(monitor.Activities()) })
	bar.linePanel.regionAt = bar.regionAt
	// a panel of its own, which looks different from the views of the page: it is about all pools, not the page
	uiutil.SetupWindow(bar.Box, "Activity")
	bar.SetBorderColor(theme.Colors.Activity.Border)
	bar.SetBackgroundColor(theme.Colors.Activity.Background)
	monitor.OnChanged(bar.update)
	return bar
}

// The parts of a line, which do different things when clicked, see regionAt.
const (
	// regionPool is the label and the progress bar: a click shows the pool
	regionPool = iota
	// regionTimeLeft is the time left behind the bar: a click shows the ETA instead (see Monitor.CycleTimeLeftMode)
	regionTimeLeft
)

// SetOnClick sets the function called with the clicked activity. Must be called on the UI thread.
func (bar *Bar) SetOnClick(onClick func(activity zfs.PoolActivity)) {
	bar.onClickLine = func(line int, column int) {
		activities := bar.monitor.Activities()
		if line >= len(activities) {
			return
		}
		if bar.regionAt(line, column) == regionTimeLeft {
			bar.monitor.CycleTimeLeftMode()
			return
		}
		onClick(activities[line])
	}
}

// regionAt returns the part of the line of an activity at the column: the time left if it is behind the bar, else
// the pool (the time left in the bar is part of it).
func (bar *Bar) regionAt(line int, column int) int {
	activities := bar.monitor.Activities()
	if line >= len(activities) {
		return regionPool
	}
	_, _, width, _ := bar.GetInnerRect()
	layout := layoutLines(activities, width, bar.monitor.TimeLeftMode())[line]
	if layout.remaining != "" && column >= layout.barStart+layout.barWidth {
		return regionTimeLeft
	}
	return regionPool
}

// Draw draws one line per activity, see layoutLines.
func (bar *Bar) Draw(screen tcell.Screen) {
	bar.DrawForSubclass(screen, bar)
	x, y, width, height := bar.GetInnerRect()
	activities := bar.monitor.Activities()
	mode := bar.monitor.TimeLeftMode()
	lines := layoutLines(activities, width, mode)
	for i, line := range lines {
		if i >= height {
			break
		}
		// only the hovered part is highlighted: the label (for the bar, which has colors of its own), or the fields
		// of the time left
		if i == bar.hovered && bar.onClickLine != nil {
			if bar.hoveredRegion == regionTimeLeft && line.remaining != "" {
				line.remaining = formatRemaining(activities[i], mode, true)
			} else {
				style := tcell.StyleDefault.Background(theme.Colors.Activity.HoverBackground)
				for column := 0; column < line.barStart; column++ {
					screen.SetContent(x+column, y+i, ' ', nil, style)
				}
			}
		}
		tview.Print(screen, line.label, x, y+i, width, tview.AlignLeft, tcell.ColorWhite)
		drawProgressBar(screen, x+line.barStart, y+i, line.barWidth, activities[i].Progress, line.barText)
		if line.remaining != "" {
			tview.Print(screen, line.remaining, x+line.barStart+line.barWidth, y+i, width-line.barStart-line.barWidth, tview.AlignLeft, tcell.ColorWhite)
		}
	}
}

// activityLine is the layout of the line of an activity, see layoutLines.
type activityLine struct {
	// label is what runs on which pool, see formatLabels
	label string
	// barStart and barWidth are the columns of the progress bar, barText is written into it
	barStart int
	barWidth int
	barText  string
	// remaining is the time left behind the bar (see formatRemaining), "" if it is written into the bar
	remaining string
}

// layoutLines lays out the lines of the activities for the given width: the labels (see formatLabels), the progress
// bars, as wide as fits, and the time left behind them. All bars have the same width, so they end at the same
// column, with space for the time left behind them (and one column of padding, like in front of the labels). The
// time left (as chosen by mode) always has the same width (see formatTimeLeft), also while it is not known yet (e.g. a
// TRIM, estimated after a while), so the bars do not change their width while an activity runs. If the bars would
// get narrower than minBarWidthWithRemaining that way, the time left is written into the bars instead.
func layoutLines(activities []zfs.PoolActivity, width int, mode TimeLeftMode) []activityLine {
	labels, labelWidth := formatLabels(activities)
	_, remainingWidth := formatRemainingWidth(zfs.PoolActivity{}, mode, false)
	barWidth := width - labelWidth - remainingWidth - 1
	besideBar := barWidth >= minBarWidthWithRemaining
	if !besideBar {
		barWidth = width - labelWidth - 1
	}

	lines := make([]activityLine, 0, len(activities))
	for i, activity := range activities {
		line := activityLine{label: labels[i], barStart: labelWidth, barWidth: barWidth}
		if besideBar {
			line.remaining = formatRemaining(activity, mode, false)
			activity.RemainingKnown = false
		}
		line.barText = barText(activity, barWidth, mode)
		lines = append(lines, line)
	}
	return lines
}

// formatRemaining returns the time left behind a progress bar (see timeLeftFields), each field on its background
// (the lighter one while hovered) with a space of padding on each side, e.g. "   ⧗    2h 10m ".
func formatRemaining(activity zfs.PoolActivity, mode TimeLeftMode, hovered bool) string {
	text, _ := formatRemainingWidth(activity, mode, hovered)
	return text
}

// formatRemainingWidth is formatRemaining, and also returns the width of the text (without the style tags).
func formatRemainingWidth(activity zfs.PoolActivity, mode TimeLeftMode, hovered bool) (text string, width int) {
	text, width = "  ", 2
	for i, field := range timeLeftFields(activity, mode, hovered) {
		if i > 0 {
			text, width = text+" ", width+1
		}
		text += txwidgets.ColorTag(theme.Colors.Activity.Remaining) + backgroundTag(field.background) + " " +
			tview.Escape(field.text) + " [-:-]"
		width += utf8.RuneCountInString(field.text) + 2
	}
	return text, width
}

// backgroundTag returns the style tag of tview that sets the background color.
func backgroundTag(color tcell.Color) string {
	r, g, b := color.RGB()
	return fmt.Sprintf("[:#%02x%02x%02x]", uint8(r), uint8(g), uint8(b))
}

// partialBlocks are the characters of a cell filled from the left by 1 to 7 eighths, see drawProgressBar.
var partialBlocks = []rune("▏▎▍▌▋▊▉")

// drawProgressBar draws a bar of the given width at x, y, filled by percent, with text written into its middle. The
// filled part has the colors of the gradient theme.Colors.Activity.BarFilled at its columns, and ends with a partly
// filled cell (in eighths), so it grows smoothly. Each cell has the text color that is readable on its background,
// so the text stays readable along the gradient and where the filled part ends.
func drawProgressBar(screen tcell.Screen, x int, y int, width int, percent float64, text string) {
	if width <= 0 {
		return
	}
	colors := theme.Colors.Activity
	eighths := filledEighths(percent, width)
	runes := []rune(text)
	start := max((width-len(runes))/2, 0)
	for column := 0; column < width; column++ {
		filled := uiutil.GradientColor(colors.BarFilled, float64(column)/float64(max(width-1, 1)), colors.BarTrack)
		background, partial := colors.BarTrack, 0
		switch cellEighths := eighths - 8*column; {
		case cellEighths >= 8:
			background = filled
		case cellEighths > 0:
			partial = cellEighths
		}
		character := ' '
		style := tcell.StyleDefault.Background(background).Foreground(textColor(background))
		if index := column - start; index >= 0 && index < len(runes) {
			character = runes[index]
			if partial >= 4 {
				// the text needs one background: the one of the larger part
				style = tcell.StyleDefault.Background(filled).Foreground(textColor(filled))
			}
		} else if partial > 0 {
			character = partialBlocks[partial-1]
			style = style.Foreground(filled)
		}
		screen.SetContent(x+column, y, character, nil, style)
	}
}

// filledEighths returns how many eighths of the cells of a bar of the given width are filled by percent.
func filledEighths(percent float64, width int) int {
	percent = min(max(percent, 0), 100)
	return int(percent / 100 * float64(8*width))
}

// textColor returns the color of text that is readable on the background: dark on light ones, light on dark ones.
func textColor(background tcell.Color) tcell.Color {
	colors := theme.Colors.Activity
	r, g, b := background.RGB()
	if r < 0 {
		return colors.BarLightText
	}
	// the perceived brightness (Rec. 709 weights)
	if 0.2126*float64(r)+0.7152*float64(g)+0.0722*float64(b) > 0.4*255 {
		return colors.BarDarkText
	}
	return colors.BarLightText
}

// formatLabels returns the label in front of the progress bar of each activity: what runs on which pool, aligned
// in columns, e.g. " Resilver  tank  ". width is the width of the labels.
func formatLabels(activities []zfs.PoolActivity) (labels []string, width int) {
	kindWidth, poolWidth := 0, 0
	for _, activity := range activities {
		kindWidth = max(kindWidth, utf8.RuneCountInString(kindTitle(activity.Kind)))
		poolWidth = max(poolWidth, utf8.RuneCountInString(activity.Pool))
	}
	labels = make([]string, 0, len(activities))
	for _, activity := range activities {
		labels = append(labels, " "+txwidgets.Span(theme.Colors.Activity.Kind, "%-*s", kindWidth, kindTitle(activity.Kind))+
			"  "+txwidgets.Span(theme.Colors.Activity.Pool, "%-*s", poolWidth, activity.Pool)+"  ")
	}
	return labels, 1 + kindWidth + 2 + poolWidth + 2
}

// barText returns the text written into the middle of the progress bar of the activity, for a bar of the given
// width: the percentage, how much is done, how fast it goes and the time left, e.g.
// " 31.8%  1.02T / 3.21T  294M/s  ⧗    2h 10m". The numbers are padded to the widest they get (sizes are printed with
// at most 3 digits by zpool, e.g. "1.02T" or "999G"), so the text does not move while they change. If it does not fit (with a space on each
// side), the least important parts are left out: first the amount, then the rate, then the time left.
func barText(activity zfs.PoolActivity, width int, mode TimeLeftMode) string {
	percent := fmt.Sprintf("%5.1f%%", min(max(activity.Progress, 0), 100))
	amount := ""
	if activity.Processed != "" && activity.Total != "" {
		amount = fmt.Sprintf("%5s / %5s", activity.Processed, activity.Total)
	}
	rate := ""
	if activity.Rate != "" {
		rate = fmt.Sprintf("%7s", activity.Rate)
	}
	remaining := ""
	if activity.RemainingKnown {
		remaining = formatTimeLeft(activity, mode)
	}
	// the parts in order of importance: the last one is left out first
	candidates := [][]string{
		{percent, amount, rate, remaining},
		{percent, rate, remaining},
		{percent, remaining},
		{percent},
	}
	for _, parts := range candidates {
		text := joinNonEmpty(parts, "  ")
		if utf8.RuneCountInString(text)+2 <= width {
			return text
		}
	}
	return strings.TrimSpace(percent)
}

// joinNonEmpty joins the parts that are not empty with the separator.
func joinNonEmpty(parts []string, separator string) string {
	var nonEmpty []string
	for _, part := range parts {
		if part != "" {
			nonEmpty = append(nonEmpty, part)
		}
	}
	return strings.Join(nonEmpty, separator)
}

// kindTitle returns the title of a kind of activity, e.g. "Scrub" for "scrub", see zfs.PoolActivity.Kind.
func kindTitle(kind string) string {
	if title, ok := kindTitles[kind]; ok {
		return title
	}
	if kind == "" {
		return kind
	}
	return strings.ToUpper(kind[:1]) + kind[1:]
}
