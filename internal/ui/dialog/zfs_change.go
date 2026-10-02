package dialog

import (
	"errors"
	"zfs-file-history/internal/zfs"

	"github.com/rivo/tview"
)

// runZfsChange runs a change of ZFS settings (e.g. "zfs set", "zfs allow") as the current user with direct. If ZFS
// denies it (zfs.ErrPermissionDenied) and the user is not root, the given commands are run with sudo in the
// suspended terminal instead, which asks for the password.
// It runs in the background and must not be called on the UI thread.
func runZfsChange(application *tview.Application, isRoot bool, explanation string, commands [][]string, direct func() error) error {
	err := direct()
	if errors.Is(err, zfs.ErrPermissionDenied) && !isRoot {
		return grantPermissions(application, explanation, commands)
	}
	return err
}
