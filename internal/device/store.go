package device

import (
	"database/sql"
	_ "embed"
	"errors"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

var (
	// ErrNotFound is returned when a requested device is not found.
	ErrNotFound = errors.New("device not found")
	// ErrAlreadyExists is returned when attempting to register a device with an existing ID.
	ErrAlreadyExists = errors.New("device already exists")
)

// Store provides persistence for devices using an embedded SQLite database.
type Store struct {
	db *sql.DB
}

// NewStore initializes a new SQLite-backed Store and executes the schema migration.
func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	// SQLite only supports one writer at a time. Capping open connections to 1
	// avoids "database is locked" errors under concurrent writes.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, err
	}

	return &Store{db: db}, nil
}

// Close closes the underlying SQLite database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// Register inserts a new device into the store.
func (s *Store) Register(d *Device) error {
	_, err := s.db.Exec(
		`INSERT INTO devices (id, name) VALUES (?, ?)`,
		d.ID, d.Name,
	)
	if err != nil {
		if isUniqueConstraintErr(err) {
			return ErrAlreadyExists
		}
		return err
	}
	return nil
}

// Heartbeat updates a device's last heartbeat timestamp and optional metrics.
func (s *Store) Heartbeat(id string, ts time.Time, m *Metrics) error {
	var cpu, signal any
	if m != nil {
		if m.CPUUsage != nil {
			cpu = *m.CPUUsage
		}
		if m.SignalStrength != nil {
			signal = *m.SignalStrength
		}
	}

	res, err := s.db.Exec(
		`UPDATE devices SET last_heartbeat = ?, cpu_usage = ?, signal_strength = ? WHERE id = ?`,
		ts.UTC().Format(time.RFC3339Nano), cpu, signal, id,
	)
	if err != nil {
		return err
	}

	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Get retrieves a device by ID from the database.
func (s *Store) Get(id string) (*Device, error) {
	var (
		d           Device
		lastHBStr   sql.NullString
		cpu, signal sql.NullFloat64
	)

	row := s.db.QueryRow(
		`SELECT id, name, last_heartbeat, cpu_usage, signal_strength FROM devices WHERE id = ?`,
		id,
	)
	err := row.Scan(&d.ID, &d.Name, &lastHBStr, &cpu, &signal)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if lastHBStr.Valid && lastHBStr.String != "" {
		if t, err := parseTimestamp(lastHBStr.String); err == nil {
			d.LastHeartbeat = t
		}
	}

	if cpu.Valid || signal.Valid {
		d.LastMetrics = &Metrics{}
		if cpu.Valid {
			d.LastMetrics.CPUUsage = &cpu.Float64
		}
		if signal.Valid {
			d.LastMetrics.SignalStrength = &signal.Float64
		}
	}

	return &d, nil
}

// List returns all devices ordered by ID.
func (s *Store) List() ([]*Device, error) {
	rows, err := s.db.Query(`SELECT id, name, last_heartbeat, cpu_usage, signal_strength FROM devices ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []*Device
	for rows.Next() {
		var (
			d           Device
			lastHBStr   sql.NullString
			cpu, signal sql.NullFloat64
		)

		if err := rows.Scan(&d.ID, &d.Name, &lastHBStr, &cpu, &signal); err != nil {
			return nil, err
		}

		if lastHBStr.Valid && lastHBStr.String != "" {
			if t, err := parseTimestamp(lastHBStr.String); err == nil {
				d.LastHeartbeat = t
			}
		}

		if cpu.Valid || signal.Valid {
			d.LastMetrics = &Metrics{}
			if cpu.Valid {
				d.LastMetrics.CPUUsage = &cpu.Float64
			}
			if signal.Valid {
				d.LastMetrics.SignalStrength = &signal.Float64
			}
		}

		devices = append(devices, &d)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return devices, nil
}

func parseTimestamp(val string) (time.Time, error) {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, val); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, errors.New("unrecognized timestamp format")
}

func isUniqueConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") || strings.Contains(msg, "PRIMARY KEY")
}
