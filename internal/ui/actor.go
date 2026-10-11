package ui

import (
	"context"
	"zfs-file-history/internal/logging"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/oklog/run"
	"github.com/pterm/pterm"
)

// AddActor wires ZFS preload and UI lifecycle into the application run group.
func AddActor(g *run.Group, ctx context.Context, start Start) {
	g.Add(func() error {
		logging.Info("Initializing ZFS data...")
		zfs.RefreshZfsData()
		logging.Info("Launching UI...")

		application := CreateUi(start, true)
		stopTimeRefresh := uiutil.StartRelativeTimeRefresh(application)
		defer stopTimeRefresh()
		return application.Run()
	}, func(err error) {
		if err != nil {
			logging.Warning("Error stopping UI: %s", err.Error())
			pterm.Warning.Printfln("Error stopping UI: %s", err.Error())
		} else {
			logging.Debug("UI stopped.")
		}
	})
}
