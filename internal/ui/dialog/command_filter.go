package dialog

import (
	"slices"
	"strings"
	"unicode"
	"zfs-file-history/internal/ui/shortcut_helper"
)

// commandMatch is a command that matches the query of the command menu, see filterCommands.
type commandMatch struct {
	command shortcut_helper.ShortcutEntry
	// positions are the indices of the runes of the name that match the query, to highlight them
	positions []int
	score     int
}

// How well a term of the query matches a name, see matchTerm.
const (
	scoreNoMatch = iota
	// scoreSubsequence: the runes of the term appear in the name in order, e.g. "rsf" in "Restore file"
	scoreSubsequence
	// scoreSubstring: the term is part of the name, e.g. "tore" in "Restore file"
	scoreSubstring
	// scoreWordStart: a word of the name starts with the term, e.g. "fil" in "Restore file", or the term is a key of
	// the command, e.g. "h" for history
	scoreWordStart
)

// filterCommands returns the commands that match the query, the best matches first and otherwise in their order.
// The query is split into terms at spaces, each of them has to match (ignoring the case), so "res rec" finds
// "Restore directory recursively".
func filterCommands(commands []shortcut_helper.ShortcutEntry, query string) []commandMatch {
	terms := strings.Fields(strings.ToLower(query))
	var matches []commandMatch
	for _, command := range commands {
		match := commandMatch{command: command}
		name := []rune(strings.ToLower(command.Name))
		matched := true
		for _, term := range terms {
			score, positions := matchTerm([]rune(term), name)
			if slices.ContainsFunc(command.KeyCombo, func(key string) bool { return strings.EqualFold(key, term) }) {
				score = max(score, scoreWordStart)
			}
			if score == scoreNoMatch {
				matched = false
				break
			}
			match.score += score
			match.positions = append(match.positions, positions...)
		}
		if matched {
			matches = append(matches, match)
		}
	}
	slices.SortStableFunc(matches, func(a, b commandMatch) int { return b.score - a.score })
	return matches
}

// matchTerm returns how well the term matches the name (see scoreWordStart, ...) and the positions of the matching
// runes in the name.
func matchTerm(term []rune, name []rune) (score int, positions []int) {
	if len(term) == 0 {
		return scoreWordStart, nil
	}
	substringAt := -1
	for start := 0; start+len(term) <= len(name); start++ {
		if !slices.Equal(name[start:start+len(term)], term) {
			continue
		}
		if start == 0 || !isWordRune(name[start-1]) {
			return scoreWordStart, span(start, len(term))
		}
		if substringAt < 0 {
			substringAt = start
		}
	}
	if substringAt >= 0 {
		return scoreSubstring, span(substringAt, len(term))
	}

	next := 0
	for i, r := range name {
		if next < len(term) && r == term[next] {
			positions = append(positions, i)
			next++
		}
	}
	if next < len(term) {
		return scoreNoMatch, nil
	}
	return scoreSubsequence, positions
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// span returns the indices start, start+1, ... of length runes.
func span(start int, length int) []int {
	indices := make([]int, length)
	for i := range indices {
		indices[i] = start + i
	}
	return indices
}
