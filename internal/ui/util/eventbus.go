package util

type Listener[T any] func(T)

type Emitter[T any] struct {
	listeners []Listener[T]
}

func NewEmitter[T any]() *Emitter[T] {
	return &Emitter[T]{}
}

func (e *Emitter[T]) Subscribe(l Listener[T]) {
	e.listeners = append(e.listeners, l)
}

func (e *Emitter[T]) Emit(v T) {
	for _, l := range e.listeners {
		l(v)
	}
}
