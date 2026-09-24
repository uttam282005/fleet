package api

import (
	"net/http"

	"fleet/web"
)

// NewRouter sets up the HTTP router and wraps it with recovery middleware.
func NewRouter(h *Handler) http.Handler {
	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("POST /devices", h.Register)
	mux.HandleFunc("POST /devices/{id}/heartbeat", h.Heartbeat)
	mux.HandleFunc("GET /devices", h.List)
	mux.HandleFunc("GET /devices/{id}", h.GetOne)
	mux.HandleFunc("GET /summary", h.Summary)

	// Web UI dashboard & self-hosted static assets
	fileServer := http.FileServer(http.FS(web.Content))
	mux.Handle("GET /{$}", fileServer)
	mux.Handle("GET /index.html", fileServer)
	mux.Handle("GET /fonts/", fileServer)

	return RecoverMiddleware(mux)
}
