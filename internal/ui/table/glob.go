package table

import (
	"path"
	"strings"
)

// globSeparatorReplacement replaces '/' before matching, because path.Match does not match '*' across '/',
// which would be surprising for e.g. dataset names like "rpool/home".
const globSeparatorReplacement = "\x00"

// MatchesGlob reports whether value matches the glob pattern (see path.Match), case-insensitive.
// A pattern without wildcards ('*', '?' or '[') matches anywhere in the value, as if surrounded by '*'.
// A pattern that is not (yet) a valid glob, e.g. "[0-" while typing, is matched as plain text instead.
// It is meant to be used in filter funcs of RowSelectionTable (see SetFilterFunc).
func MatchesGlob(value string, pattern string) bool {
	value = strings.ToLower(value)
	pattern = strings.ToLower(pattern)

	if !strings.ContainsAny(pattern, "*?[") {
		return strings.Contains(value, pattern)
	}

	matched, err := path.Match(
		strings.ReplaceAll(pattern, "/", globSeparatorReplacement),
		strings.ReplaceAll(value, "/", globSeparatorReplacement),
	)
	if err != nil {
		return strings.Contains(value, pattern)
	}
	return matched
}
