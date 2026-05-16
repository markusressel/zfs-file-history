package profiling

import (
	"net/http/httptest"
	"testing"
)

func TestCreatePprofMuxRegistersExpectedRoutes(t *testing.T) {
	t.Parallel()

	mux := createPprofMux()
	routes := []string{
		"/debug/pprof/",
		"/debug/pprof/cmdline",
		"/debug/pprof/profile",
		"/debug/pprof/symbol",
		"/debug/pprof/trace",
	}

	for _, route := range routes {
		route := route
		t.Run(route, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest("GET", route, nil)
			_, pattern := mux.Handler(req)
			if pattern == "" {
				t.Fatalf("route %s is not registered", route)
			}
		})
	}
}
