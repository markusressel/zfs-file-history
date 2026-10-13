package util

import (
	"strings"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"

	"github.com/gdamore/tcell/v2"
)

// FormatProgressBar returns a bar of the given width, filled by percent, followed by the percentage, e.g.
// "██████░░░░ 60.0%".
func FormatProgressBar(percent float64, width int) string {
	percent = min(max(percent, 0), 100)
	width = max(width, 0)
	filled := int(percent / 100 * float64(width))
	return txwidgets.Span(theme.Colors.Sparkline.Graph, "%s", strings.Repeat("█", filled)) +
		txwidgets.Span(theme.Colors.Layout.Table.TreeLines, "%s", strings.Repeat("░", width-filled)) +
		txwidgets.Span(tcell.ColorWhite, " %.1f%%", percent)
}
