CREATE TABLE IF NOT EXISTS devices (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    last_heartbeat  TIMESTAMP,
    cpu_usage       REAL,
    signal_strength REAL
);
