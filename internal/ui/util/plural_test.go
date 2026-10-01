package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPlural(t *testing.T) {
	assert.Equal(t, "entries", Plural(0, "entry", "entries"))
	assert.Equal(t, "entry", Plural(1, "entry", "entries"))
	assert.Equal(t, "entries", Plural(2, "entry", "entries"))
}
