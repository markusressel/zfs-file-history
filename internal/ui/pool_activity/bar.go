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

// maxBarWidth is the widest the progress bars get, so they do not span wide terminals; the rest of the line stays
// empty.
const maxBarWidth = 60

// minBarWidthWithRemaining is the narrowest a progress bar gets before the time left is written into it instead
// of next to it, see layoutLines.
const minBarWidthWithRemaining = 20

// noLine is the hovered line while the mouse is not on the bar.
const noLine = -1

// kindTitles are the titles of the kinds of activities that are not just capitalized, see kindTitle.
var kindTitles = map[string]string{
	"trim":   "TRIM",
	"remove": "Device removal",
	"expand": "RAIDZ expansion",
}

// Bar shows the operations running on the pools (see Monitor), one line each: what runs on which pool, a progress
// bar with the progress written into it, and the time left, e.g.
// " Resilver  tank  [ 31.8%  1.02T / 3.21T  294M/s        ]  02:10:31 to go". The columns are aligned, so all bars
// start and end at the same column. It has no height while nothing runs, see SetOnHeightChanged. A click on a line
// runs the function set with SetOnClick.
type Bar struct {
	*tview.Box
	monitor *Monitor

	// All fields below are only accessed on the UI thread.

	height int
	// onHeightChanged is called with the number of lines when it changed
	onHeightChanged func(height int)
	// onClick is called with the clicked activity, nil: lines cannot be clicked
	onClick func(activity zfs.PoolActivity)
	// enabled returns whether the lines can be clicked, e.g. not while a dialog is in front
	enabled func() bool
	// hovered is the index of the line below the mouse, noLine if none
	hovered int
}

// NewBar returns a bar showing the activities of monitor. Must be called on the UI thread.
func NewBar(monitor *Monitor) *Bar {
	bar := &Bar{
		Box:     tview.NewBox(),
		monitor: monitor,
		enabled: func() bool { return true },
		hovered: noLine,
	}
	// a panel of its own, which looks different from the views of the page: it is about all pools, not the page
	bar.SetBorder(true)
	uiutil.SetupWindow(bar.Box, "Activity")
	bar.SetBorderColor(theme.Colors.Activity.Border)
	monitor.OnChanged(bar.update)
	return bar
}

// SetOnHeightChanged sets the function called with the height the bar needs (the number of activities and the
// border, 0 while nothing runs) whenever it changed, e.g. to resize it in its layout. Must be called on the UI thread.
func (bar *Bar) SetOnHeightChanged(onHeightChanged func(height int)) *Bar {
	bar.onHeightChanged = onHeightChanged
	return bar
}

// SetOnClick sets the function called with the clicked activity. Must be called on the UI thread.
func (bar *Bar) SetOnClick(onClick func(activity zfs.PoolActivity)) *Bar {
	bar.onClick = onClick
	return bar
}

// SetEnabledFunc sets a function that returns whether the lines can be clicked, e.g. not while a dialog is in front
// (tview hands mouse events to all pages, see rivo/tview#926). Must be called on the UI thread.
func (bar *Bar) SetEnabledFunc(enabled func() bool) *Bar {
	bar.enabled = enabled
	return bar
}

// Height returns the number of lines the bar needs, including the border (0 while nothing runs). Must be called on
// the UI thread.
func (bar *Bar) Height() int {
	return bar.height
}

// update reports the new height after the activities changed. Runs on the UI thread.
func (bar *Bar) update() {
	lines := len(bar.monitor.Activities())
	height := 0
	if lines > 0 {
		// the border
		height = lines + 2
	}
	if height == bar.height {
		return
	}
	bar.height = height
	if bar.hovered >= lines {
		bar.hovered = noLine
	}
	if bar.onHeightChanged != nil {
		bar.onHeightChanged(height)
	}
}

// Draw draws one line per activity, see layoutLines.
func (bar *Bar) Draw(screen tcell.Screen) {
	bar.DrawForSubclass(screen, bar)
	x, y, width, height := bar.GetInnerRect()
	// only the inside: the border lines are in the middle of their cells, a background behind them would reach
	// beyond them
	fillBackground(screen, x, y, width, height, theme.Colors.Activity.Background)
	activities := bar.monitor.Activities()
	lines := layoutLines(activities, width)
	for i, line := range lines {
		if i >= height {
			break
		}
		if i == bar.hovered && bar.onClick != nil {
			style := tcell.StyleDefault.Background(theme.Colors.Activity.HoverBackground)
			for column := x; column < x+width; column++ {
				screen.SetContent(column, y+i, ' ', nil, style)
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
	// remaining is the time left behind the bar (e.g. "  02:10:31 to go"), "" if it is written into the bar or not
	// known
	remaining string
}

// layoutLines lays out the lines of the activities for the given width: the labels (see formatLabels), the progress
// bars, as wide as fits (up to maxBarWidth), and the time left behind them. All bars have the same width, so they
// end at the same column, with space for the longest time left behind them (and one column of padding, like in
// front of the labels). If the bars would get narrower than minBarWidthWithRemaining that way, the time left is
// written into the bars instead.
func layoutLines(activities []zfs.PoolActivity, width int) []activityLine {
	labels, labelWidth := formatLabels(activities)
	remainingWidth := 0
	for _, activity := range activities {
		remainingWidth = max(remainingWidth, utf8.RuneCountInString(formatRemaining(activity.Remaining)))
	}
	barWidth := width - labelWidth - remainingWidth - 1
	besideBar := remainingWidth > 0 && barWidth >= minBarWidthWithRemaining
	if !besideBar {
		barWidth = width - labelWidth - 1
	}
	barWidth = min(barWidth, maxBarWidth)

	lines := make([]activityLine, 0, len(activities))
	for i, activity := range activities {
		line := activityLine{label: labels[i], barStart: labelWidth, barWidth: barWidth}
		if besideBar {
			if remaining := formatRemaining(activity.Remaining); remaining != "" {
				line.remaining = txwidgets.Span(theme.Colors.Activity.Remaining, "%s", remaining)
			}
			activity.Remaining = ""
		}
		line.barText = barText(activity, barWidth)
		lines = append(lines, line)
	}
	return lines
}

// formatRemaining returns the time left behind a progress bar, e.g. "  02:10:31 to go", "" if not known.
func formatRemaining(remaining string) string {
	if remaining == "" {
		return ""
	}
	return "  " + remaining + " to go"
}

// fillBackground fills the rectangle with the background color.
func fillBackground(screen tcell.Screen, x int, y int, width int, height int, color tcell.Color) {
	style := tcell.StyleDefault.Background(color)
	for row := y; row < y+height; row++ {
		for column := x; column < x+width; column++ {
			screen.SetContent(column, row, ' ', nil, style)
		}
	}
}

// drawProgressBar draws a bar of the given width at x, y, filled by percent, with text written into its middle. Each
// cell has the text color that is readable on its background, so the text stays readable where the filled part
// ends.
func drawProgressBar(screen tcell.Screen, x int, y int, width int, percent float64, text string) {
	if width <= 0 {
		return
	}
	filled := filledCells(percent, width)
	colors := theme.Colors.Activity
	runes := []rune(text)
	start := max((width-len(runes))/2, 0)
	for column := 0; column < width; column++ {
		style := tcell.StyleDefault.Background(colors.BarTrack).Foreground(colors.BarTrackText)
		if column < filled {
			style = tcell.StyleDefault.Background(colors.BarFilled).Foreground(colors.BarFilledText)
		}
		character := ' '
		if index := column - start; index >= 0 && index < len(runes) {
			character = runes[index]
		}
		screen.SetContent(x+column, y, character, nil, style)
	}
}

// filledCells returns how many of the cells of a bar of the given width are filled by percent.
func filledCells(percent float64, width int) int {
	percent = min(max(percent, 0), 100)
	return int(percent / 100 * float64(width))
}

// MouseHandler highlights the line below the mouse and reports a clicked one, see SetOnClick. The bar never takes
// the focus.
func (bar *Bar) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
	return bar.WrapMouseHandler(func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
		x, y := event.Position()
		if bar.onClick == nil || !bar.enabled() || !bar.InRect(x, y) {
			// e.g. moved away: tview only sends the events of other positions while the bar captures the mouse
			changed := bar.hovered != noLine
			bar.hovered = noLine
			return changed, nil
		}
		_, top, _, _ := bar.GetInnerRect()
		line := y - top
		activities := bar.monitor.Activities()
		if line < 0 || line >= len(activities) {
			line = noLine
		}
		changed := line != bar.hovered
		bar.hovered = line
		if action == tview.MouseLeftClick && line != noLine {
			bar.onClick(activities[line])
		}
		// captures the mouse while on the bar, to know when it leaves it
		return changed || action == tview.MouseLeftDown || action == tview.MouseLeftClick, bar
	})
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
// width: the percentage, how much is done, how fast it goes and the time left, e.g. " 31.8%  1.02T / 3.21T  294M/s
// 02:10:31 to go". The numbers are padded to the widest they get (sizes are printed with at most 3 digits by zpool,
// e.g. "1.02T" or "999G"), so the text does not move while they change. If it does not fit (with a space on each
// side), the least important parts are left out: first the amount, then the rate, then the time left.
func barText(activity zfs.PoolActivity, width int) string {
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
	if activity.Remaining != "" {
		remaining = activity.Remaining + " to go"
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
