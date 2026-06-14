package util

import (
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
}

func NewDataLoader[T any](app *tview.Application) *DataLoader[T] {
	return &DataLoader[T]{app: app}
}

func (l *DataLoader[T]) OnLoad(f func(T)) *DataLoader[T]      { l.onLoad = f; return l }
func (l *DataLoader[T]) OnError(f func(error)) *DataLoader[T] { l.onError = f; return l }
func (l *DataLoader[T]) OnStart(f func()) *DataLoader[T]      { l.onStart = f; return l }

func (l *DataLoader[T]) Load(f func() (T, error)) {
	l.load(f, true)
}

func (l *DataLoader[T]) LoadQuietly(f func() (T, error)) {
	l.load(f, false)
}

func (l *DataLoader[T]) load(f func() (T, error), showLoading bool) {
	seq := l.seq.Add(1)
	if showLoading && l.onStart != nil {
		l.onStart()
	}
	go func() {
		data, err := f()
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

// LoadingContainer is a tview.Pages wrapper that handles switching between a loading view and content.
type LoadingContainer struct {
	*tview.Pages
	loadingView *tview.TextView
	content     tview.Primitive
	isLoading   bool
}

func NewLoadingContainer(app *tview.Application, content tview.Primitive, title string, message string) *LoadingContainer {
	loadingView := NewLoadingView(app, title, message)
	pages := tview.NewPages().
		AddPage(LoadingContainerContentPage, content, true, true).
		AddPage(LoadingContainerLoadingPage, loadingView, true, false)

	return &LoadingContainer{
		Pages:       pages,
		loadingView: loadingView,
		content:     content,
	}
}

func (c *LoadingContainer) SetIsLoading(isLoading bool) {
	if c.isLoading == isLoading {
		return
	}
	c.isLoading = isLoading
	if isLoading {
		c.SwitchToPage(LoadingContainerLoadingPage)
	} else {
		c.SwitchToPage(LoadingContainerContentPage)
	}
}

func (c *LoadingContainer) GetFrontPage() (string, tview.Primitive) {
	return c.Pages.GetFrontPage()
}
