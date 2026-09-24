package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"fleet/internal/device"
)

func setupTestRouter(t *testing.T, fixedTime time.Time) (http.Handler, *device.Store) {
	t.Helper()
	store, err := device.NewStore("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	t.Cleanup(func() {
		store.Close()
	})

	h := NewHandler(store)
	if !fixedTime.IsZero() {
		h.now = func() time.Time { return fixedTime }
	}
	return NewRouter(h), store
}

func TestRegisterHandler(t *testing.T) {
	router, _ := setupTestRouter(t, time.Now())

	// 1. Success: 201 Created
	body := `{"id": "device-01", "name": "Lab Device 01"}`
	req := httptest.NewRequest(http.MethodPost, "/devices", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp DeviceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.ID != "device-01" || resp.Name != "Lab Device 01" || resp.Status != device.StatusOffline {
		t.Errorf("unexpected response: %+v", resp)
	}

	// 2. Duplicate: 409 Conflict
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/devices", bytes.NewBufferString(body))
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409 Conflict for duplicate ID, got %d", rec.Code)
	}

	// 3. Missing ID: 400 Bad Request
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/devices", bytes.NewBufferString(`{"name": "No ID"}`))
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for missing ID, got %d", rec.Code)
	}

	// 4. Missing Name: 400 Bad Request
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/devices", bytes.NewBufferString(`{"id": "dev-no-name"}`))
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for missing name, got %d", rec.Code)
	}

	// 5. Malformed JSON: 400 Bad Request
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/devices", bytes.NewBufferString(`{invalid-json}`))
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for malformed JSON, got %d", rec.Code)
	}
}

func TestHeartbeatHandler(t *testing.T) {
	fixedTime := time.Date(2026, 9, 21, 10, 30, 0, 0, time.UTC)
	router, store := setupTestRouter(t, fixedTime)

	// Register a device first
	if err := store.Register(&device.Device{ID: "device-01", Name: "Lab Device 01"}); err != nil {
		t.Fatalf("failed to register device: %v", err)
	}

	// 1. Success with metrics and explicit timestamp
	body := `{"timestamp": "2026-09-21T10:29:50Z", "status": "OK", "cpu_usage": 42.5, "signal_strength": -71.0}`
	req := httptest.NewRequest(http.MethodPost, "/devices/device-01/heartbeat", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp DeviceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Status != device.StatusOnline {
		t.Errorf("expected ONLINE status, got %s", resp.Status)
	}
	if resp.LastMetrics == nil || *resp.LastMetrics.CPUUsage != 42.5 || *resp.LastMetrics.SignalStrength != -71.0 {
		t.Errorf("unexpected metrics: %+v", resp.LastMetrics)
	}

	// 2. Success with omitted timestamp (server generates current time)
	bodyNoTime := `{"cpu_usage": 50.0}`
	req = httptest.NewRequest(http.MethodPost, "/devices/device-01/heartbeat", bytes.NewBufferString(bodyNoTime))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK for omitted timestamp, got %d", rec.Code)
	}

	// 3. Heartbeat on unknown device: 404 Not Found
	req = httptest.NewRequest(http.MethodPost, "/devices/unknown-99/heartbeat", bytes.NewBufferString(body))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 Not Found for unregistered device, got %d", rec.Code)
	}

	// 4. Malformed timestamp: 400 Bad Request
	req = httptest.NewRequest(http.MethodPost, "/devices/device-01/heartbeat", bytes.NewBufferString(`{"timestamp": "not-a-date"}`))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for invalid timestamp, got %d", rec.Code)
	}

	// 5. Far future timestamp (> 1 minute ahead): 400 Bad Request
	futureTime := fixedTime.Add(2 * time.Minute).Format(time.RFC3339)
	req = httptest.NewRequest(http.MethodPost, "/devices/device-01/heartbeat", bytes.NewBufferString(`{"timestamp": "`+futureTime+`"}`))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for future timestamp, got %d", rec.Code)
	}
}

func TestListAndFilterHandler(t *testing.T) {
	fixedTime := time.Date(2026, 9, 21, 10, 30, 0, 0, time.UTC)
	router, store := setupTestRouter(t, fixedTime)

	// Register 3 devices:
	// dev-01: heartbeat 10s ago -> ONLINE
	// dev-02: heartbeat 35s ago -> OFFLINE
	// dev-03: no heartbeat -> OFFLINE
	store.Register(&device.Device{ID: "dev-01", Name: "Online Sensor"})
	store.Register(&device.Device{ID: "dev-02", Name: "Offline Sensor"})
	store.Register(&device.Device{ID: "dev-03", Name: "Never Connected"})

	store.Heartbeat("dev-01", fixedTime.Add(-10*time.Second), nil)
	store.Heartbeat("dev-02", fixedTime.Add(-35*time.Second), nil)

	// 1. List all
	req := httptest.NewRequest(http.MethodGet, "/devices", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
	var all []DeviceListItem
	if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil {
		t.Fatalf("failed to decode list: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 devices, got %d", len(all))
	}

	// 2. Filter status=online
	req = httptest.NewRequest(http.MethodGet, "/devices?status=online", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
	var online []DeviceListItem
	json.Unmarshal(rec.Body.Bytes(), &online)
	if len(online) != 1 || online[0].ID != "dev-01" {
		t.Errorf("expected 1 online device (dev-01), got %+v", online)
	}

	// 3. Filter status=offline
	req = httptest.NewRequest(http.MethodGet, "/devices?status=offline", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
	var offline []DeviceListItem
	json.Unmarshal(rec.Body.Bytes(), &offline)
	if len(offline) != 2 {
		t.Errorf("expected 2 offline devices, got %d", len(offline))
	}
}

func TestGetOneHandler(t *testing.T) {
	fixedTime := time.Date(2026, 9, 21, 10, 30, 0, 0, time.UTC)
	router, store := setupTestRouter(t, fixedTime)

	store.Register(&device.Device{ID: "dev-one", Name: "Device One"})
	cpu := 30.0
	store.Heartbeat("dev-one", fixedTime.Add(-5*time.Second), &device.Metrics{CPUUsage: &cpu})

	// 1. Success
	req := httptest.NewRequest(http.MethodGet, "/devices/dev-one", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
	var dev DeviceResponse
	json.Unmarshal(rec.Body.Bytes(), &dev)
	if dev.ID != "dev-one" || dev.Status != device.StatusOnline || dev.LastMetrics == nil || *dev.LastMetrics.CPUUsage != 30.0 {
		t.Errorf("unexpected device detail: %+v", dev)
	}

	// 2. Unknown: 404
	req = httptest.NewRequest(http.MethodGet, "/devices/dev-unknown", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 Not Found, got %d", rec.Code)
	}
}

func TestSummaryHandler(t *testing.T) {
	fixedTime := time.Date(2026, 9, 21, 10, 30, 0, 0, time.UTC)
	router, store := setupTestRouter(t, fixedTime)

	// Empty store summary
	req := httptest.NewRequest(http.MethodGet, "/summary", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var sum SummaryResponse
	json.Unmarshal(rec.Body.Bytes(), &sum)
	if sum.Total != 0 || sum.Online != 0 || sum.Offline != 0 {
		t.Errorf("expected zero summary, got %+v", sum)
	}

	// Add devices
	store.Register(&device.Device{ID: "d1", Name: "D1"})
	store.Register(&device.Device{ID: "d2", Name: "D2"})
	store.Register(&device.Device{ID: "d3", Name: "D3"})

	store.Heartbeat("d1", fixedTime.Add(-5*time.Second), nil)  // online
	store.Heartbeat("d2", fixedTime.Add(-15*time.Second), nil) // online
	// d3 has no heartbeat -> offline

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	json.Unmarshal(rec.Body.Bytes(), &sum)

	if sum.Total != 3 || sum.Online != 2 || sum.Offline != 1 {
		t.Errorf("expected total=3, online=2, offline=1, got %+v", sum)
	}
}
