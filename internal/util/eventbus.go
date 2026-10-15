package util

import "sync"

type Listener[T any] func(T)

// Emitter calls its listeners with each value emitted. Safe for concurrent use: the UI subscribes to emitters of
// internal/zfs while their watchers emit from other goroutines.
type Emitter[T any] struct {
	mutex sync.Mutex
	// listeners are guarded by mutex
	listeners []Listener[T]
}

func NewEmitter[T any]() *Emitter[T] {
	return &Emitter[T]{}
}

func (e *Emitter[T]) Subscribe(l Listener[T]) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.listeners = append(e.listeners, l)
}

// Emit calls the listeners with v, outside the lock, so a listener may subscribe (it is called from the next value
// on).
func (e *Emitter[T]) Emit(v T) {
	e.mutex.Lock()
	listeners := e.listeners
	e.mutex.Unlock()
	for _, l := range listeners {
		l(v)
	}
}
