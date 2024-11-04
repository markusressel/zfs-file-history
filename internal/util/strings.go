package util

import "strings"

func IsBlank(s string) bool {
	return len(s) == 0 || strings.TrimSpace(s) == ""
}
