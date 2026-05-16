package profiling

import (
	"context"
	"fmt"
	"net/http"
	"net/http/pprof"
	"zfs-file-history/internal/configuration"
	"zfs-file-history/internal/logging"

	"github.com/oklog/run"
	"github.com/pterm/pterm"
)

// AddActor wires the optional pprof HTTP server into the application run group.
func AddActor(g *run.Group, ctx context.Context) {
	if !configuration.CurrentConfig.Profiling.Enabled {
		return
	}

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
			logging.Warning("Error stopping parca webserver: %s", err.Error())
			pterm.Warning.Printfln("Error stopping parca webserver: %s", err.Error())
		} else {
			logging.Debug("Webservers stopped.")
			pterm.Debug.Printfln("parca webserver stopped.")
		}
	})
}
