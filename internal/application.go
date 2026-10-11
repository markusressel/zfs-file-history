package internal

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/profiling"
	"zfs-file-history/internal/state"
	"zfs-file-history/internal/ui"
	"zfs-file-history/internal/zfs"

	"github.com/oklog/run"
)

func RunApplication(start ui.Start) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var g run.Group
	addSignalHandlerActor(&g, cancel)
	profiling.AddActor(&g, ctx)
	ui.AddActor(&g, ctx, start)
	zfs.AddZpoolEventWatcherActor(&g, ctx)

	err := g.Run()
	if flushErr := state.Current.Flush(); flushErr != nil {
		logging.Error("Cannot save state: %v", flushErr)
	}
	if err != nil {
		logging.Error("%v", err)
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	} else {
		logging.Info("Done.")
		os.Exit(0)
	}
}

func addSignalHandlerActor(g *run.Group, cancel context.CancelFunc) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	g.Add(func() error {
		<-sig
		logging.Info("Received SIGTERM signal, exiting...")

		return nil
	}, func(err error) {
		signal.Stop(sig) // Stop notifying for signals
		close(sig)       // Then close the channel
		cancel()
	})
}
