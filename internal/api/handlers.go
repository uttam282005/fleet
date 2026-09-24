package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"fleet/internal/device"
)

// Handler handles HTTP requests for the fleet monitoring API.
type Handler struct {
	store *device.Store
	now   func() time.Time
}

// NewHandler creates a new Handler with default system clock.
func NewHandler(store *device.Store) *Handler {
	return &Handler{
		store: store,
		now:   time.Now,
	}
}

// RegisterRequest represents the JSON body to register a device.
type RegisterRequest struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// HeartbeatRequest represents the JSON body sent by a device on heartbeat.
type HeartbeatRequest struct {
	Timestamp      *string  `json:"timestamp,omitempty"`
	Status         *string  `json:"status,omitempty"` // Informational device self-report
	CPUUsage       *float64 `json:"cpu_usage,omitempty"`
	SignalStrength *float64 `json:"signal_strength,omitempty"`
}

// DeviceResponse represents the public JSON representation of a device.
type DeviceResponse struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Status        device.Status   `json:"status"`
	LastHeartbeat *string         `json:"last_heartbeat,omitempty"`
	LastMetrics   *device.Metrics `json:"last_metrics,omitempty"`
}

func (h *Handler) toResponse(d *device.Device, now time.Time) DeviceResponse {
	resp := DeviceResponse{
		ID:     d.ID,
		Name:   d.Name,
		Status: d.Status(now),
	}
	if !d.LastHeartbeat.IsZero() {
		hb := d.LastHeartbeat.UTC().Format(time.RFC3339)
		resp.LastHeartbeat = &hb
	}
	resp.LastMetrics = d.LastMetrics
	return resp
}

// Register handles POST /devices to register a new device.
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.ID = strings.TrimSpace(req.ID)
	req.Name = strings.TrimSpace(req.Name)
	if req.ID == "" || req.Name == "" {
		WriteError(w, http.StatusBadRequest, "id and name are required")
		return
	}

	d := &device.Device{
		ID:   req.ID,
		Name: req.Name,
	}

	if err := h.store.Register(d); err != nil {
		if errors.Is(err, device.ErrAlreadyExists) {
			WriteError(w, http.StatusConflict, "device already exists")
			return
		}
		WriteError(w, http.StatusInternalServerError, "failed to register device")
		return
	}

	WriteJSON(w, http.StatusCreated, h.toResponse(d, h.now()))
}

// Heartbeat handles POST /devices/{id}/heartbeat to record telemetry.
func (h *Handler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		WriteError(w, http.StatusBadRequest, "device id is required")
		return
	}

	var req HeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	now := h.now()
	ts := now.UTC()
	if req.Timestamp != nil && strings.TrimSpace(*req.Timestamp) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*req.Timestamp))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid timestamp format: must be RFC3339")
			return
		}
		// Guard against timestamps absurdly in the future (> 1 minute)
		if parsed.After(now.Add(1 * time.Minute)) {
			WriteError(w, http.StatusBadRequest, "timestamp cannot be in the future")
			return
		}
		ts = parsed.UTC()
	}

	var metrics *device.Metrics
	if req.CPUUsage != nil || req.SignalStrength != nil {
		metrics = &device.Metrics{
			CPUUsage:       req.CPUUsage,
			SignalStrength: req.SignalStrength,
		}
	}

	if err := h.store.Heartbeat(id, ts, metrics); err != nil {
		if errors.Is(err, device.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "device not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "failed to record heartbeat")
		return
	}

	updated, err := h.store.Get(id)
	if err != nil {
		if errors.Is(err, device.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "device not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "failed to fetch updated device")
		return
	}

	WriteJSON(w, http.StatusOK, h.toResponse(updated, now))
}
