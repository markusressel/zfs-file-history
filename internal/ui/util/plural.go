package util

// Plural returns singular if count is 1, otherwise plural, e.g. for "1 entry" / "42 entries".
func Plural(count int, singular string, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}
