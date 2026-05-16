package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContainsString_Valid(t *testing.T) {
	// GIVEN
	list := []string{
		"one",
		"two",
		"three",
	}

	// WHEN
	result := ContainsString(list, "two")

	// THEN
	assert.True(t, result)
}

func TestContainsString_Invalid(t *testing.T) {
	// GIVEN
	list := []string{
		"one",
		"two",
		"three",
	}

	// WHEN
	result := ContainsString(list, "zero")

	// THEN
	assert.False(t, result)
}
