package api

import (
	"embed"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/antikirra/beanstalkd-ui/internal/model"
	"github.com/antikirra/beanstalkd-ui/internal/store"
)

//go:embed admin_tmpl/*.html
var tmplFS embed.FS

//go:embed admin_static/*
var staticFS embed.FS

// NewServer creates a fully configured HTTP handler with all routes and middleware.
func NewServer(log *slog.Logger, st *store.Store, samples model.SampleJobs, authPassword string) (http.Handler, *Handlers, error) {
	tmplDir, err := fs.Sub(tmplFS, "admin_tmpl")
	if err != nil {
		return nil, nil, err
	}

	h, err := NewHandlers(log, st, tmplDir, samples)
	if err != nil {
		return nil, nil, err
	}

	mux := http.NewServeMux()
	h.registerRoutes(mux)

	// Middleware chain: recovery → security headers → no-cache → auth (if enabled) → mux.
	var handler http.Handler = mux

	if authPassword != "" {
		throttle := newAuthThrottle()
		handler = authMiddleware(handler, "beanstalkd", authPassword, throttle, log)
	}

	handler = noCache(handler)
	handler = securityHeaders(handler)
	handler = recoveryMiddleware(handler, log)

	return handler, h, nil
}

func (h *Handlers) registerRoutes(mux *http.ServeMux) {
	// Static assets.
	staticDir, _ := fs.Sub(staticFS, "admin_static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticDir))))

	// Pages.
	mux.HandleFunc("GET /{$}", h.handleServers)
	mux.HandleFunc("GET /index", h.handleServersReload)
	mux.HandleFunc("POST /serversAdd", h.handleServerAdd)
	mux.HandleFunc("POST /serversRemove", h.handleServerRemove)
	mux.HandleFunc("/server", h.handleServer)
	mux.HandleFunc("/tube", h.handleTube)
	mux.HandleFunc("/sample", h.handleSamples)
	mux.HandleFunc("/statistics", h.handleStatistics)
	mux.HandleFunc("GET /settings", h.handleSettings)
}
