package device

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	// Open an in-memory SQLite database unique to this test
	store, err := NewStore("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to create in-memory store: %v", err)
	}
	t.Cleanup(func() {
		store.Close()
	})
	return store
}

func TestStoreRegisterAndGet(t *testing.T) {
	store := newTestStore(t)

	dev := &Device{
		ID:   "device-01",
		Name: "Sensor Alpha",
	}

	// 1. Register succeeds
	if err := store.Register(dev); err != nil {
		t.Fatalf("unexpected error registering device: %v", err)
	}

	// 2. Duplicate registration returns ErrAlreadyExists
	if err := store.Register(dev); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got: %v", err)
	}

	// 3. Get existing device
	got, err := store.Get("device-01")
	if err != nil {
		t.Fatalf("unexpected error getting device: %v", err)
	}
	if got.ID != dev.ID || got.Name != dev.Name {
		t.Errorf("got device %+v, want %+v", got, dev)
	}
	if !got.LastHeartbeat.IsZero() {
		t.Errorf("expected zero LastHeartbeat before first heartbeat, got %v", got.LastHeartbeat)
	}
	if got.LastMetrics != nil {
		t.Errorf("expected nil LastMetrics before first heartbeat, got %+v", got.LastMetrics)
	}

	// 4. Get unknown device returns ErrNotFound
	_, err = store.Get("unknown-device")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing device, got: %v", err)
	}
}

func TestStoreHeartbeat(t *testing.T) {
	store := newTestStore(t)

	// Heartbeat on unregistered device returns ErrNotFound
	ts := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	cpu := 45.5
	sig := -65.0
	metrics := &Metrics{CPUUsage: &cpu, SignalStrength: &sig}

	err := store.Heartbeat("nonexistent", ts, metrics)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}

	// Register device
	if err := store.Register(&Device{ID: "device-02", Name: "Sensor Beta"}); err != nil {
		t.Fatalf("failed to register device: %v", err)
	}

	// Heartbeat on registered device succeeds
	if err := store.Heartbeat("device-02", ts, metrics); err != nil {
		t.Fatalf("unexpected error updating heartbeat: %v", err)
	}

	// Verify updated fields
	updated, err := store.Get("device-02")
	if err != nil {
		t.Fatalf("failed to get updated device: %v", err)
	}
	if !updated.LastHeartbeat.Equal(ts) {
		t.Errorf("got LastHeartbeat %v, want %v", updated.LastHeartbeat, ts)
	}
	if updated.LastMetrics == nil {
		t.Fatalf("expected LastMetrics to be populated")
	}
	if updated.LastMetrics.CPUUsage == nil || *updated.LastMetrics.CPUUsage != cpu {
		t.Errorf("got CPUUsage %v, want %v", updated.LastMetrics.CPUUsage, cpu)
	}
	if updated.LastMetrics.SignalStrength == nil || *updated.LastMetrics.SignalStrength != sig {
		t.Errorf("got SignalStrength %v, want %v", updated.LastMetrics.SignalStrength, sig)
	}
}

func TestStoreList(t *testing.T) {
	store := newTestStore(t)

	// Empty list
	list, err := store.List()
	if err != nil {
		t.Fatalf("unexpected error listing empty store: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 devices, got %d", len(list))
	}

	// Insert multiple devices
	devices := []*Device{
		{ID: "dev-03", Name: "Device Three"},
		{ID: "dev-01", Name: "Device One"},
		{ID: "dev-02", Name: "Device Two"},
	}
	for _, d := range devices {
		if err := store.Register(d); err != nil {
			t.Fatalf("failed to register %s: %v", d.ID, err)
		}
	}

	list, err = store.List()
	if err != nil {
		t.Fatalf("unexpected error listing devices: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 devices, got %d", len(list))
	}

	// Check sorted order (ORDER BY id ASC)
	if list[0].ID != "dev-01" || list[1].ID != "dev-02" || list[2].ID != "dev-03" {
		t.Errorf("devices not in expected sorted order: %v, %v, %v", list[0].ID, list[1].ID, list[2].ID)
	}
}

func TestStoreConcurrency(t *testing.T) {
	store := newTestStore(t)

	// Register 5 devices
	for i := 1; i <= 5; i++ {
		id := fmt.Sprintf("dev-conc-%02d", i)
		if err := store.Register(&Device{ID: id, Name: id}); err != nil {
			t.Fatalf("failed to register device %s: %v", id, err)
		}
	}

	const goroutines = 20
	const iterations = 15

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(gid int) {
			defer wg.Done()
			devID := fmt.Sprintf("dev-conc-%02d", (gid%5)+1)
			for i := 0; i < iterations; i++ {
				cpu := float64(gid*10 + i)
				sig := float64(-50 - i)
				now := time.Now().UTC()

				err := store.Heartbeat(devID, now, &Metrics{
					CPUUsage:       &cpu,
					SignalStrength: &sig,
				})
				if err != nil {
					t.Errorf("goroutine %d: heartbeat error: %v", gid, err)
					return
				}

				// Concurrent read
				_, err = store.Get(devID)
				if err != nil {
					t.Errorf("goroutine %d: get error: %v", gid, err)
					return
				}
			}
		}(g)
	}

	wg.Wait()
}
