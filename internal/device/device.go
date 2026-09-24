package device

import "time"

// Status represents the operational state of a device.
type Status string

const (
	StatusOnline  Status = "ONLINE"
	StatusOffline Status = "OFFLINE"
)

// DefaultOnlineWindow is the default time window within which a device
// is considered ONLINE if a heartbeat was received.
const DefaultOnlineWindow = 30 * time.Second

// Metrics holds the latest telemetry metrics reported by a device.
type Metrics struct {
	CPUUsage       *float64 `json:"cpu_usage,omitempty"`
	SignalStrength *float64 `json:"signal_strength,omitempty"`
}

// Device represents a registered IoT device in the fleet.
type Device struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	LastHeartbeat time.Time `json:"last_heartbeat,omitempty"`
	LastMetrics   *Metrics  `json:"last_metrics,omitempty"`
}

// Status computes the device's status at read time relative to the provided timestamp
// using DefaultOnlineWindow.
func (d Device) Status(now time.Time) Status {
	return d.StatusWithWindow(now, DefaultOnlineWindow)
}

// StatusWithWindow computes the device's status relative to now using a specified window.
func (d Device) StatusWithWindow(now time.Time, window time.Duration) Status {
	if d.LastHeartbeat.IsZero() {
		return StatusOffline
	}
	if now.Sub(d.LastHeartbeat) <= window {
		return StatusOnline
	}
	return StatusOffline
}
