package internal

import (
	"zfs-file-history/internal/ui"
)

func RunApplication(path string) {
	if err := ui.CreateUi(path, true).Run(); err != nil {
		panic(err)
	}
}
