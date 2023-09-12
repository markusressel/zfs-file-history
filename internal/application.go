package internal

import (
	"zfs-file-history/internal/ui"
)

func RunApplication(path string) {

	// TODO: allow restoration of this file
	//    - use latest version by default
	//    - allow the user to select a specific snapshot as a source
	// TODO: allow selection of multiple files/directories
	// TODO: restore the file(s)

	if err := ui.CreateUi(path, true).Run(); err != nil {
		panic(err)
	}
}
