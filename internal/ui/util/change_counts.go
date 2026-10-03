package util

import (
	"strings"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
)

// FormatChangeCounts returns numbers of changed entries in short, e.g. "+3 −1 ~2" for 3 added, 1 deleted and 2
// modified ones, each in the color of its diff state. Zeros are left out, "" if nothing changed.
func FormatChangeCounts(added int, deleted int, modified int) string {
	colors := theme.Colors.FileBrowser.Table.State
	var parts []string
	if added > 0 {
		parts = append(parts, txwidgets.Span(colors.Added, "+%d", added))
	}
	if deleted > 0 {
		parts = append(parts, txwidgets.Span(colors.Deleted, "−%d", deleted))
	}
	if modified > 0 {
		parts = append(parts, txwidgets.Span(colors.Modified, "~%d", modified))
	}
	return strings.Join(parts, " ")
}
