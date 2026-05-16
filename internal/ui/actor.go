package ui

import (
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/zfs"

	"github.com/oklog/run"
	"github.com/pterm/pterm"
)

// AddActor wires ZFS preload and UI lifecycle into the application run group.
func AddActor(g *run.Group, path string) {
	g.Add(func() error {
		logging.Info("Loading ZFS data...")
		pterm.Info.Printfln("Loading ZFS data...")
		zfs.RefreshZfsData()
		pterm.Info.Printfln("Launching UI...")
		logging.Info("Launching UI...")
		return CreateUi(path, true).Run()
	}, func(err error) {
		if err != nil {
			logging.Warning("Error stopping UI: %s", err.Error())
			pterm.Warning.Printfln("Error stopping UI: %s", err.Error())
		} else {
			logging.Debug("UI stopped.")
			pterm.Debug.Printfln("Received SIGTERM signal, exiting...")
		}
	})
}
