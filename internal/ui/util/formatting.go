package util

import (
	"fmt"
	"strings"

	"github.com/dustin/go-humanize"
)

func StableLengthHumanizedBytes(u uint64) string {
	text := humanize.IBytes(u)
	if strings.HasSuffix(text, " B") {
		withoutSuffix := strings.TrimSuffix(text, " B")
		text = fmt.Sprintf("%s   B", withoutSuffix)
	}
	if len(text) < 10 {
		text = fmt.Sprintf("%s%s", strings.Repeat(" ", 10-len(text)), text)
	}
	return text
}
