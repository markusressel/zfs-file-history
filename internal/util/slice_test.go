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

func TestMin(t *testing.T) {
	assert.Equal(t, 1.0, Min([]float64{1, 2, 3}))
	assert.Equal(t, 0.0, Min([]float64{}))
	assert.Equal(t, 5.0, Min([]float64{5}))
}

func TestMax(t *testing.T) {
	assert.Equal(t, 3.0, Max([]float64{1, 2, 3}))
	assert.Equal(t, 0.0, Max([]float64{}))
	assert.Equal(t, 5.0, Max([]float64{5}))
}

func TestSortedKeys(t *testing.T) {
	input := map[int]string{
		3: "c",
		1: "a",
		2: "b",
	}
	expected := []int{1, 2, 3}
	assert.Equal(t, expected, SortedKeys(input))
}

func TestUniqueSlice(t *testing.T) {
	s1 := []int{1, 2, 3}
	s2 := []int{2, 3, 4}
	result := UniqueSlice(s1, s2)
	assert.ElementsMatch(t, []int{1, 2, 3, 4}, result)
}

func TestMergeUniqueFileLists(t *testing.T) {
	s1 := []string{"a", "b"}
	s2 := []string{"b", "c"}
	result := MergeUniqueFileLists(s1, s2)
	assert.ElementsMatch(t, []string{"a", "b", "c"}, result)
}
