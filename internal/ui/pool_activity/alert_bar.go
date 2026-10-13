package pool_activity

import (
	"unicode/utf8"
	"zfs-file-history/internal/ui/clickable"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// severityTitles are the titles of the severities of alerts, see zfs.PoolAlertSeverity.
var severityTitles = map[zfs.PoolAlertSeverity]string{
	zfs.PoolAlertError:   "Error",
	zfs.PoolAlertWarning: "Warning",
}

// AlertBar shows the problems of the pools (see Monitor.Alerts), one line each, most severe first: how severe, which
// pool, and what is wrong, e.g. " Error  rpool  18 data errors in files or metadata". The columns are aligned. It
// has no height while all pools are healthy, so it comes up by itself when something goes wrong (see
// SetOnHeightChanged). A click on a line runs the function set with SetOnClick, the line below the mouse is
// highlighted.
type AlertBar struct {
	*linePanel
	// All fields below are only accessed on the UI thread.
	monitor *Monitor
	// onClick is called with the clicked alert, nil: the lines cannot be clicked
	onClick func(alert zfs.PoolAlert)
}

// alertRegion is the key of the line of an alert (see clickable.Region): a pool has at most one alert of a kind.
type alertRegion struct {
	pool, kind string
}

// NewAlertBar returns a bar showing the alerts of monitor. Must be called on the UI thread.
func NewAlertBar(monitor *Monitor) *AlertBar {
	bar := &AlertBar{monitor: monitor}
	bar.linePanel = newLinePanel(func() int { return len(monitor.Alerts()) })
	uiutil.SetupWindow(bar.Box, "Alerts")
	bar.SetBackgroundColor(theme.Colors.Alert.Background)
	monitor.OnChanged(bar.update)
	return bar
}

// SetOnClick sets the function called with the clicked alert. Must be called on the UI thread.
func (bar *AlertBar) SetOnClick(onClick func(alert zfs.PoolAlert)) {
	bar.onClick = onClick
}

// Draw draws one line per alert, see formatAlerts. The border has the color of the most severe alert.
func (bar *AlertBar) Draw(screen tcell.Screen) {
	alerts := bar.monitor.Alerts()
	if len(alerts) > 0 {
		bar.SetBorderColor(severityColor(alerts[0].Severity))
	}
	bar.DrawForSubclass(screen, bar)
	bar.regions.Reset()
	x, y, width, height := bar.GetInnerRect()
	for i, line := range formatAlerts(alerts, width) {
		if i >= height {
			break
		}
		tview.Print(screen, line, x, y+i, width, tview.AlignLeft, theme.Colors.Alert.Message)
		if bar.onClick != nil {
			alert := alerts[i]
			bar.regions.Add(clickable.Region{X: x, Y: y + i, Width: width, Key: alertRegion{alert.Pool, alert.Kind},
				HoverBackground: theme.Colors.Alert.HoverBackground, OnClick: func() { bar.onClick(alert) }})
		}
	}
	bar.regions.DrawHover(screen)
}

// formatAlerts returns the lines of the alerts for the given width: the severity and the pool aligned in columns,
// then the message, e.g. " Error    rpool  18 data errors in files or metadata". A message that does not fit ends
// with "…", so it does not look complete (e.g. a list of devices).
func formatAlerts(alerts []zfs.PoolAlert, width int) []string {
	severityWidth, poolWidth := 0, 0
	for _, alert := range alerts {
		severityWidth = max(severityWidth, utf8.RuneCountInString(severityTitles[alert.Severity]))
		poolWidth = max(poolWidth, utf8.RuneCountInString(alert.Pool))
	}
	messageWidth := width - (1 + severityWidth + 2 + poolWidth + 2)
	lines := make([]string, 0, len(alerts))
	for _, alert := range alerts {
		lines = append(lines, " "+txwidgets.Span(severityColor(alert.Severity), "%-*s", severityWidth, severityTitles[alert.Severity])+
			"  "+txwidgets.Span(theme.Colors.Alert.Pool, "%-*s", poolWidth, alert.Pool)+
			"  "+txwidgets.Span(theme.Colors.Alert.Message, "%s", truncate(alert.Message, messageWidth)))
	}
	return lines
}

// truncate returns the text cut to the given width, ending with "…" if it was cut.
func truncate(text string, width int) string {
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	if width <= 0 {
		return ""
	}
	return string(runes[:width-1]) + "…"
}

// severityColor returns the color of a severity, see theme.AlertColors.
func severityColor(severity zfs.PoolAlertSeverity) tcell.Color {
	if severity == zfs.PoolAlertError {
		return theme.Colors.Alert.Error
	}
	return theme.Colors.Alert.Warning
}
