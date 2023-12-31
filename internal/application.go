package internal

import (
	"context"
	"fmt"
	"github.com/oklog/run"
	"github.com/pterm/pterm"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"zfs-file-history/internal/configuration"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui"
	"zfs-file-history/internal/zfs"
)

func RunApplication(path string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var g run.Group
	{
		if configuration.CurrentConfig.Profiling.Enabled {
			g.Add(func() error {
				mux := http.NewServeMux()
				mux.HandleFunc("/debug/pprof/", pprof.Index)
				mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
				mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
				mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
				mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

				go func() {
					logging.Info("Starting profiling webserver...")
					pterm.Info.Printfln("Starting profiling webserver...")

					profilingConfig := configuration.CurrentConfig.Profiling
					address := fmt.Sprintf("%s:%d", profilingConfig.Host, profilingConfig.Port)
					err := http.ListenAndServe(address, mux)
					logging.Error("Error running profiling webserver: %v", err)
					pterm.Error.Printfln("Error running profiling webserver: %v", err)
				}()

				<-ctx.Done()
				logging.Info("Stopping profiling webserver...")
				return nil
			}, func(err error) {
				if err != nil {
					logging.Warning("Error stopping parca webserver: " + err.Error())
					pterm.Warning.Printfln("Error stopping parca webserver: " + err.Error())
				} else {
					logging.Debug("Webservers stopped.")
					pterm.Debug.Printfln("parca webserver stopped.")
				}
			})
		}
	}
	{
		g.Add(func() error {
			logging.Info("Loading ZFS data...")
			pterm.Info.Printfln("Loading ZFS data...")
			zfs.RefreshZfsData()
			pterm.Info.Printfln("Launching UI...")
			logging.Info("Launching UI...")
			return ui.CreateUi(path, true).Run()
		}, func(err error) {
			if err != nil {
				logging.Warning("Error stopping UI: " + err.Error())
				pterm.Warning.Printfln("Error stopping UI: " + err.Error())
			} else {
				logging.Debug("UI stopped.")
				pterm.Debug.Printfln("Received SIGTERM signal, exiting...")
			}
		})
	}
	{
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
