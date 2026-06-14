package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCreateAttentionText(t *testing.T) {
	assert.Equal(t, "  Hello  ", CreateAttentionText("Hello"))
	assert.Equal(t, "    ", CreateAttentionText(""))
}
