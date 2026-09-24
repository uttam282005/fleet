package api

import (
	"net/http"
)

// NewRouter sets up the HTTP router and wraps it with recovery middleware.
func NewRouter(h *Handler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /devices", h.Register)
	mux.HandleFunc("POST /devices/{id}/heartbeat", h.Heartbeat)

	return RecoverMiddleware(mux)
}
