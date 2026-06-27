package util

import (
	"zfs-file-history/internal/util"

	"github.com/rivo/tview"
)

func SubscribeUI[T any](emitter *util.Emitter[T], app *tview.Application, callback func(v T)) {
	emitter.Subscribe(func(v T) {
		go func() {
			app.QueueUpdateDraw(func() {
				callback(v)
			})
		}()
	})
}
