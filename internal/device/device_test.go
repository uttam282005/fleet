package device

import (
	"testing"
	"time"
)

func TestDeviceStatus(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		last time.Time
		want Status
	}{
		{
			name: "never received heartbeat",
			last: time.Time{},
			want: StatusOffline,
		},
		{
			name: "just received heartbeat (1s ago)",
			last: now.Add(-1 * time.Second),
			want: StatusOnline,
		},
		{
			name: "received heartbeat 10s ago",
			last: now.Add(-10 * time.Second),
			want: StatusOnline,
		},
		{
			name: "just inside 30s boundary (29.9s ago)",
			last: now.Add(-29900 * time.Millisecond),
			want: StatusOnline,
		},
		{
			name: "exact 30.0s boundary",
			last: now.Add(-30 * time.Second),
			want: StatusOnline,
		},
		{
			name: "just past 30s boundary (30.1s ago)",
			last: now.Add(-30100 * time.Millisecond),
			want: StatusOffline,
		},
		{
			name: "received heartbeat 31s ago",
			last: now.Add(-31 * time.Second),
			want: StatusOffline,
		},
		{
			name: "received heartbeat 5 minutes ago",
			last: now.Add(-5 * time.Minute),
			want: StatusOffline,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := Device{
				ID:            "dev-test",
				Name:          "Test Device",
				LastHeartbeat: tt.last,
			}

			if got := d.Status(now); got != tt.want {
				t.Errorf("d.Status() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDeviceStatusWithCustomWindow(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	customWindow := 5 * time.Second

	d := Device{
		ID:            "dev-custom",
		Name:          "Custom Window Device",
		LastHeartbeat: now.Add(-4 * time.Second),
	}

	if got := d.StatusWithWindow(now, customWindow); got != StatusOnline {
		t.Errorf("got %v, want %v for heartbeat 4s ago with 5s window", got, StatusOnline)
	}

	d.LastHeartbeat = now.Add(-6 * time.Second)
	if got := d.StatusWithWindow(now, customWindow); got != StatusOffline {
		t.Errorf("got %v, want %v for heartbeat 6s ago with 5s window", got, StatusOffline)
	}
}
