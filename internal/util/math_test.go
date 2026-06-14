package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRatio(t *testing.T) {
	// GIVEN
	a := 0.0
	b := 100.0
	c := 50.0

	expected := 0.5

	// WHEN
	result := Ratio(c, a, b)

	// THEN
	assert.Equal(t, expected, result)
}

func TestCoerce(t *testing.T) {
	assert.Equal(t, 5, Coerce(5, 0, 10))
	assert.Equal(t, 0, Coerce(-1, 0, 10))
	assert.Equal(t, 10, Coerce(11, 0, 10))
}

func TestAvg(t *testing.T) {
	assert.Equal(t, 5.0, Avg([]float64{0, 5, 10}))
	assert.Equal(t, 0.0, Avg([]float64{0}))
}

func TestHexString(t *testing.T) {
	assert.Equal(t, "A", HexString("a"))
	assert.Equal(t, "A", HexString("0a"))
	assert.Equal(t, "FF", HexString("ff"))
	assert.Equal(t, "invalid", HexString("invalid"))
}

func TestFindClosest(t *testing.T) {
	// GIVEN
	options := []int{
		10, 20, 30, 40, 50, 60, 70, 80, 90,
	}

	// WHEN
	closest := FindClosest(5, options)
	// THEN
	assert.Equal(t, 10, closest)

	// WHEN
	closest = FindClosest(54, options)
	// THEN
	assert.Equal(t, 50, closest)

	// WHEN
	closest = FindClosest(55, options)

	// THEN
	assert.Equal(t, 60, closest)

	// WHEN
	closest = FindClosest(100, options)
	// THEN
	assert.Equal(t, 90, closest)

}
