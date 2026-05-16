package profiling

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/pprof"
	"time"
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

	profilingConfig := configuration.CurrentConfig.Profiling
	address := fmt.Sprintf("%s:%d", profilingConfig.Host, profilingConfig.Port)
	server := &http.Server{Addr: address, Handler: createPprofMux()}

	g.Add(func() error {
		logging.Info("Starting profiling webserver...")
		pterm.Info.Printfln("Starting profiling webserver...")

		go shutdownOnContextDone(ctx, server)

		err := server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logging.Error("Error running profiling webserver: %v", err)
			pterm.Error.Printfln("Error running profiling webserver: %v", err)
			return err
		}

		logging.Info("Stopping profiling webserver...")
		return nil
	}, func(err error) {
		if err != nil {
			logging.Warning("Error stopping profiling webserver: %s", err.Error())
			pterm.Warning.Printfln("Error stopping profiling webserver: %s", err.Error())
		} else {
			logging.Debug("Profiling webserver stopped.")
			pterm.Debug.Printfln("Profiling webserver stopped.")
		}
	})
}

func createPprofMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return mux
}

func shutdownOnContextDone(ctx context.Context, server *http.Server) {
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
		logging.Warning("Error shutting down profiling webserver: %s", err.Error())
		pterm.Warning.Printfln("Error shutting down profiling webserver: %s", err.Error())
	}
}
