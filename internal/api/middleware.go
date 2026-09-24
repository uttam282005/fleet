package api

import (
	"encoding/json"
	"log"
	"net/http"
)

// ErrorResponse represents a standardized JSON error message.
type ErrorResponse struct {
	Error string `json:"error"`
}

// WriteJSON sends a JSON response with the specified status code.
func WriteJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		if err := json.NewEncoder(w).Encode(data); err != nil {
			log.Printf("error encoding JSON response: %v", err)
		}
	}
}

// WriteError sends a JSON error response with the specified status code.
func WriteError(w http.ResponseWriter, status int, message string) {
	WriteJSON(w, status, ErrorResponse{Error: message})
}

// RecoverMiddleware recovers from panics in HTTP handlers, logging the incident
// and returning a 500 Internal Server Error JSON response.
func RecoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("PANIC recovered in HTTP handler: %v", rec)
				WriteError(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
