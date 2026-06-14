package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEmitter_SubscribeAndEmit(t *testing.T) {
	emitter := NewEmitter[string]()
	var receivedValue string
	calls := 0

	emitter.Subscribe(func(v string) {
		receivedValue = v
		calls++
	})

	emitter.Emit("hello")

	assert.Equal(t, "hello", receivedValue)
	assert.Equal(t, 1, calls)
}

func TestEmitter_MultipleSubscribers(t *testing.T) {
	emitter := NewEmitter[int]()
	callsA := 0
	callsB := 0

	emitter.Subscribe(func(v int) {
		callsA++
	})
	emitter.Subscribe(func(v int) {
		callsB++
	})

	emitter.Emit(42)

	assert.Equal(t, 1, callsA)
	assert.Equal(t, 1, callsB)
}

func TestEmitter_NoSubscribers(t *testing.T) {
	emitter := NewEmitter[int]()
	// Should not panic
	assert.NotPanics(t, func() {
		emitter.Emit(42)
	})
}
