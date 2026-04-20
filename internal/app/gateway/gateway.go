// Package gateway provides the HTTP gateway for the whiteagent runtime.
// It serves /healthz and /readyz endpoints and allows channel plugins to register routes.
package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/pkg/runtime"
)

// Gateway is the HTTP server that exposes health endpoints and channel plugin routes.
type Gateway struct {
	server *http.Server
	mux    *http.ServeMux
}

// pluginStatus is the JSON response element for health endpoints.
type pluginStatus struct {
	ID    string `json:"id"`
	State string `json:"state"`
}

// readyzResponse is the JSON response body for /readyz.
type readyzResponse struct {
	Status  string         `json:"status"`
	Plugins []pluginStatus `json:"plugins"`
}

// NewGateway creates an HTTP gateway with /healthz and /readyz endpoints.
// The plugins slice is used by /readyz to report plugin states.
// The stateFunc returns the current runtime lifecycle state for readiness checks.
func NewGateway(addr string, plugins []port.Plugin, stateFunc func() runtime.State) *Gateway {
	mux := http.NewServeMux()

	// Liveness probe: always returns 200 if the process is alive.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"alive"}`))
	})

	// Readiness probe: returns 200 when Ready, 503 otherwise.
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		state := stateFunc()
		statuses := make([]pluginStatus, 0, len(plugins))
		for _, p := range plugins {
			statuses = append(statuses, pluginStatus{
				ID:    p.ID(),
				State: string(p.Status()),
			})
		}

		resp := readyzResponse{
			Status:  state.String(),
			Plugins: statuses,
		}

		w.Header().Set("Content-Type", "application/json")
		if state != runtime.StateReady {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	gw := &Gateway{
		mux: mux,
		server: &http.Server{
			Addr:    addr,
			Handler: loggingMiddleware(mux),
		},
	}
	return gw
}

// RegisterChannelRoutes lets each channel plugin register its HTTP routes on the gateway mux.
func (g *Gateway) RegisterChannelRoutes(channels []port.ChannelEntry) {
	for _, entry := range channels {
		entry.Plugin.RegisterRoutes(g.mux)
	}
}

// Start begins listening in a background goroutine. Returns nil immediately.
func (g *Gateway) Start() error {
	slog.Info("gateway starting", "addr", g.server.Addr)
	go func() {
		if err := g.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("gateway listen error", "err", err)
		}
	}()
	return nil
}

// Stop gracefully shuts down the HTTP server.
func (g *Gateway) Stop(ctx context.Context) error {
	return g.server.Shutdown(ctx)
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// loggingMiddleware logs every HTTP request at INFO level with method, path,
// status code, and duration.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)
		slog.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.statusCode,
			"duration", fmt.Sprintf("%dms", time.Since(start).Milliseconds()),
		)
	})
}
