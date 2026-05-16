package internal

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/profiling"
	"zfs-file-history/internal/ui"

	"github.com/oklog/run"
	"github.com/pterm/pterm"
)

func RunApplication(path string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var g run.Group
	profiling.AddActor(&g, ctx)
	ui.AddActor(&g, path)
	addSignalHandlerActor(&g, cancel)

	if err := g.Run(); err != nil {
		logging.Error("%v", err)
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	} else {
		logging.Info("Done.")
		pterm.Info.Printfln("Done.")
		os.Exit(0)
	}
}

func addSignalHandlerActor(g *run.Group, cancel context.CancelFunc) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	g.Add(func() error {
		<-sig
		logging.Info("Received SIGTERM signal, exiting...")
		pterm.Info.Printfln("Received SIGTERM signal, exiting...")

		return nil
	}, func(err error) {
		defer close(sig)
		cancel()
	})
}
