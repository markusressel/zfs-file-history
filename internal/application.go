package internal

import (
	"context"
	"fmt"
	"github.com/oklog/run"
	"net/http"
	"net/http/pprof"
	"os"
	"zfs-file-history/internal/configuration"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui"
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
					profilingConfig := configuration.CurrentConfig.Profiling
					address := fmt.Sprintf("%s:%d", profilingConfig.Host, profilingConfig.Port)
					err := http.ListenAndServe(address, mux)
					logging.Error("Error running profiling webserver: %v", err)
				}()

				<-ctx.Done()
				logging.Info("Stopping profiling webserver...")
				return nil
			}, func(err error) {
				if err != nil {
					logging.Warning("Error stopping parca webserver: " + err.Error())
				} else {
					logging.Debug("Webservers stopped.")
				}
			})
		}
	}
	{
		g.Add(func() error {
			return ui.CreateUi(path, true).Run()
		}, func(err error) {
			if err != nil {
				logging.Warning("Error stopping parca webserver: " + err.Error())
			} else {
				logging.Debug("Webservers stopped.")
			}
		})
	}

	if err := g.Run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	} else {
		logging.Info("Done.")
		os.Exit(0)
	}
}
