package config

import (
	"os"
	"time"
)

// Config holds runtime configuration options loaded from environment variables or defaults.
type Config struct {
	Addr         string        // FLEET_ADDR, default ":8080"
	DBPath       string        // FLEET_DB_PATH, default "fleet.db"
	OnlineWindow time.Duration // FLEET_ONLINE_WINDOW, default 30s
}

// Load reads configuration from the environment with sane fallback defaults.
func Load() Config {
	cfg := Config{
		Addr:         ":8080",
		DBPath:       "fleet.db",
		OnlineWindow: 30 * time.Second,
	}

	if v := os.Getenv("FLEET_ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := os.Getenv("FLEET_DB_PATH"); v != "" {
		cfg.DBPath = v
	}
	if v := os.Getenv("FLEET_ONLINE_WINDOW"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.OnlineWindow = d
		}
	}

	return cfg
}
