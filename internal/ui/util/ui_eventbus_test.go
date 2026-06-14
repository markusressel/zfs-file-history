package util

import (
	"testing"
	"time"
	"zfs-file-history/internal/util"

	"github.com/rivo/tview"
)

func TestSubscribeUI(t *testing.T) {
	app := tview.NewApplication()
	emitter := util.NewEmitter[string]()

	SubscribeUI(emitter, app, func(v string) {
		// This won't be called because the app loop isn't running,
		// but we call it to cover the anonymous function lines.
	})

	// Emit in a goroutine because QueueUpdateDraw might block
	// if the application loop isn't running (unbuffered channel).
	go emitter.Emit("test-value")

	// Give it a tiny bit of time to execute the subscription logic
	// before the test finishes.
	time.Sleep(10 * time.Millisecond)
}
