package util

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/rivo/tview"
)

type DebouncedLoader struct {
	application        *tview.Application
	onShowSpinner      func()
	cancelContext      context.CancelFunc
	sequenceCounter    atomic.Uint64
	timer              *time.Timer
	showLoadingSpinner bool
}

func NewDebouncedLoader(application *tview.Application, onShowSpinner func()) *DebouncedLoader {
	return &DebouncedLoader{
		application:   application,
		onShowSpinner: onShowSpinner,
	}
}

func (l *DebouncedLoader) Start() (context.Context, uint64) {
	l.Cancel()
	ctx, cancel := context.WithCancel(context.Background())
	l.cancelContext = cancel
	seq := l.sequenceCounter.Add(1)
	l.showLoadingSpinner = false

	l.timer = time.AfterFunc(100*time.Millisecond, func() {
		l.application.QueueUpdateDraw(func() {
			if seq != l.sequenceCounter.Load() {
				return
			}
			l.showLoadingSpinner = true
			l.onShowSpinner()
		})
	})
	return ctx, seq
}

func (l *DebouncedLoader) Stop(seq uint64) {
	l.application.QueueUpdateDraw(func() {
		if seq != l.sequenceCounter.Load() {
			return
		}
		if l.timer != nil {
			l.timer.Stop()
			l.timer = nil
		}
	})
}

func (l *DebouncedLoader) Cancel() {
	if l.cancelContext != nil {
		l.cancelContext()
		l.cancelContext = nil
	}
	if l.timer != nil {
		l.timer.Stop()
		l.timer = nil
	}
}

func (l *DebouncedLoader) IsCurrentSequence(seq uint64) bool {
	return seq == l.sequenceCounter.Load()
}

func (l *DebouncedLoader) ShowLoadingSpinner() bool {
	return l.showLoadingSpinner
}
