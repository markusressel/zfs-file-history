package util

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/rivo/tview"
)

// DataLoader handles asynchronous data loading with sequence tracking to avoid race conditions.
type DataLoader[T any] struct {
	app     *tview.Application
	seq     atomic.Uint64
	onLoad  func(T)
	onError func(error)
	onStart func()

	mu     sync.Mutex
	cancel context.CancelFunc
}

func NewDataLoader[T any](app *tview.Application) *DataLoader[T] {
	return &DataLoader[T]{app: app}
}

func (l *DataLoader[T]) OnLoad(f func(T)) *DataLoader[T]      { l.onLoad = f; return l }
func (l *DataLoader[T]) OnError(f func(error)) *DataLoader[T] { l.onError = f; return l }
func (l *DataLoader[T]) OnStart(f func()) *DataLoader[T]      { l.onStart = f; return l }

func (l *DataLoader[T]) Load(f func(ctx context.Context) (T, error)) {
	l.load(f, true)
}

func (l *DataLoader[T]) LoadQuietly(f func(ctx context.Context) (T, error)) {
	l.load(f, false)
}

func (l *DataLoader[T]) load(f func(ctx context.Context) (T, error), showLoading bool) {
	seq := l.seq.Add(1)

	l.mu.Lock()
	if l.cancel != nil {
		l.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	l.cancel = cancel
	l.mu.Unlock()

	if showLoading && l.onStart != nil {
		l.onStart()
	}
	go func() {
		data, err := f(ctx)
		l.app.QueueUpdateDraw(func() {
			if l.seq.Load() != seq {
				return
			}
			if err != nil {
				if l.onError != nil {
					l.onError(err)
				}
			} else {
				if l.onLoad != nil {
					l.onLoad(data)
				}
			}
		})
	}()
}

const (
	LoadingContainerContentPage = "content"
	LoadingContainerLoadingPage = "loading"
)
