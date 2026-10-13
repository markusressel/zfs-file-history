package pool_activity

import (
	"fmt"
	"strings"
	"unicode/utf8"
	"zfs-file-history/internal/ui/clickable"
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
// " Resilver  tank  [ 31.8%  1.02T / 3.21T  294M/s        ]   ⧗    2h 10m ". The columns are aligned, so all bars
// start and end at the same column. It has no height while nothing runs, see SetOnHeightChanged. A click on the label
// or the bar runs the function set with SetOnClick, a click on the time left shows the ETA instead (see
// Monitor.CycleTimeLeftMode). Only the part below the mouse is highlighted.
type Bar struct {
	*linePanel
	// All fields below are only accessed on the UI thread.
	monitor *Monitor
	// onClick is called with the clicked activity, nil: the label and the bar cannot be clicked
	onClick func(activity zfs.PoolActivity)
}

// NewBar returns a bar showing the activities of monitor. Must be called on the UI thread.
func NewBar(monitor *Monitor) *Bar {
	bar := &Bar{monitor: monitor}
	bar.linePanel = newLinePanel(func() int { return len(monitor.Activities()) })
	// a panel of its own, which looks different from the views of the page: it is about all pools, not the page
	uiutil.SetupWindow(bar.Box, "Activity")
	bar.SetBorderColor(theme.Colors.Activity.Border)
	bar.SetBackgroundColor(theme.Colors.Activity.Background)
	monitor.OnChanged(bar.update)
	return bar
}

// SetOnClick sets the function called with the clicked activity. Must be called on the UI thread.
func (bar *Bar) SetOnClick(onClick func(activity zfs.PoolActivity)) {
	bar.onClick = onClick
}

// The parts of the line of an activity that do different things when clicked, see activityRegion.
const (
	// regionPool is the label and the progress bar: a click shows the pool
	regionPool = iota
	// regionTimeLeft is the time left behind the bar: a click shows the ETA instead
	regionTimeLeft
)

// activityRegion is the key of a part of the line of an activity (see clickable.Region), by what runs where, so the
// hover stays while its progress changes.
type activityRegion struct {
	pool, kind string
	part       int
}

// Draw draws one line per activity, see layoutLines, and adds the parts that can be clicked to the regions.
func (bar *Bar) Draw(screen tcell.Screen) {
	bar.DrawForSubclass(screen, bar)
	bar.regions.Reset()
	x, y, width, height := bar.GetInnerRect()
	activities := bar.monitor.Activities()
	colors := theme.Colors.Activity
	for i, line := range layoutLines(activities, width, bar.monitor.TimeLeftMode()) {
		if i >= height {
			break
		}
		activity := activities[i]
		tview.Print(screen, line.label, x, y+i, width, tview.AlignLeft, tcell.ColorWhite)
		drawProgressBar(screen, x+line.barStart, y+i, line.barWidth, activity.Progress, line.barText)
		if bar.onClick != nil {
			// the bar has colors of its own: only the label is highlighted
			key := activityRegion{activity.Pool, activity.Kind, regionPool}
			onClick := func() { bar.onClick(activity) }
			// cut to the panel: the labels can be wider than a narrow one, the hover would be painted over the border
			labelWidth := min(line.barStart, width)
			bar.regions.Add(clickable.Region{X: x, Y: y + i, Width: labelWidth, Key: key, HoverBackground: colors.HoverBackground, OnClick: onClick})
			bar.regions.Add(clickable.Region{X: x + labelWidth, Y: y + i, Width: min(line.barWidth, width-labelWidth), Key: key,
				HoverBackground: tcell.ColorDefault, OnClick: onClick})
		}
		column := x + line.barStart + line.barWidth + timeLeftGap
		for j, field := range line.timeLeft {
			if j > 0 {
				column++
			}
			_, fieldWidth := tview.Print(screen, backgroundTag(field.background)+" "+tview.Escape(field.text)+" ",
				column, y+i, x+width-column, tview.AlignLeft, colors.Remaining)
			bar.regions.Add(clickable.Region{X: column, Y: y + i, Width: fieldWidth, Key: activityRegion{activity.Pool, activity.Kind, regionTimeLeft},
				HoverBackground: field.hoverBackground, OnClick: bar.monitor.CycleTimeLeftMode})
			column += fieldWidth
		}
	}
	bar.regions.DrawHover(screen)
}

// activityLine is the layout of the line of an activity, see layoutLines.
type activityLine struct {
	// label is what runs on which pool, see formatLabels
	label string
	// barStart and barWidth are the columns of the progress bar, barText is written into it
	barStart int
	barWidth int
	barText  string
	// timeLeft are the fields of the time left behind the bar (see timeLeftFields), nil if it is written into the bar
	timeLeft []timeLeftField
}

// timeLeftGap is the space between a bar and the time left behind it.
const timeLeftGap = 2

// timeLeftWidth returns the width of the time left behind a bar: the fields with a space of padding on each side,
// one column between them, and the gap in front, see Bar.Draw.
func timeLeftWidth(fields []timeLeftField) int {
	width := timeLeftGap + max(len(fields)-1, 0)
	for _, field := range fields {
		width += utf8.RuneCountInString(field.text) + 2
	}
	return width
}

// layoutLines lays out the lines of the activities for the given width: the labels (see formatLabels), the progress
// bars, as wide as fits, and the time left behind them. All bars have the same width, so they end at the same
// column, with space for the time left behind them (and one column of padding, like in front of the labels). The
// time left (as chosen by mode) always has the same width (see timeLeftFields), also while it is not known yet (e.g.
// a TRIM, estimated after a while), so the bars do not change their width while an activity runs. If the bars would
// get narrower than minBarWidthWithRemaining that way, the time left is written into the bars instead.
func layoutLines(activities []zfs.PoolActivity, width int, mode TimeLeftMode) []activityLine {
	labels, labelWidth := formatLabels(activities)
	remainingWidth := timeLeftWidth(timeLeftFields(zfs.PoolActivity{}, mode))
	barWidth := width - labelWidth - remainingWidth - 1
	besideBar := barWidth >= minBarWidthWithRemaining
	if !besideBar {
		barWidth = width - labelWidth - 1
	}

	lines := make([]activityLine, 0, len(activities))
	for i, activity := range activities {
		line := activityLine{label: labels[i], barStart: labelWidth, barWidth: barWidth}
		if besideBar {
			line.timeLeft = timeLeftFields(activity, mode)
			activity.RemainingKnown = false
		}
		line.barText = barText(activity, barWidth, mode)
		lines = append(lines, line)
	}
	return lines
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
