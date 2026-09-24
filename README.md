# Fleet — Mini Device Fleet Monitor

A lightweight, robust IoT device fleet monitoring service written in Go. It ingests heartbeats and telemetry metrics from connected devices, persists device records in an embedded SQLite database, and computes device connectivity status on read.

---

## 1. What It Does

Fleet monitors the health and telemetry of IoT devices across a distributed network. Devices register with the service and transmit periodic heartbeats containing metrics (CPU utilization and signal strength). Fleet tracks connectivity in real time, computing whether devices are `ONLINE` or `OFFLINE` based on a 30-second heartbeat window, and provides fleet-wide summary statistics and querying endpoints.

---

## 2. Design and Architecture Decisions

- **Embedded SQLite with Single-Writer Serialization (`db.SetMaxOpenConns(1)`)**:
  - *Decision*: Persistent storage is backed by SQLite via `modernc.org/sqlite` (pure Go, zero CGO toolchain required), running a lightweight single-table schema with schema initialization on startup.
  - *Rationale*: Avoids requiring the reviewer or operator to stand up an external database (such as PostgreSQL or Docker). Because SQLite permits only one active writer at a time at the file lock level, capping the `database/sql` connection pool to `1` writer eliminates SQLite "database is locked" errors under concurrent heartbeats.
- **Computed Status on Read, Not Stored in Database**:
  - *Decision*: Device status (`ONLINE` vs `OFFLINE`) is calculated dynamically at request time by evaluating `now.Sub(last_heartbeat) <= 30s`. No `status` column exists in the database table, and no background ticker goroutines mutate state.
  - *Rationale*: Storing a status field that is periodically flipped by a background goroutine creates race conditions, synchronization lag, and multiple conflicting sources of truth. Deriving status on read makes the state deterministic, inherently race-free, and trivially testable with deterministic mock clocks.
- **Minimal Dependencies & Standard Library Routing**:
  - *Decision*: Built with Go 1.22+ enhanced `http.ServeMux` pattern routing (`"POST /devices"`, `"GET /devices/{id}"`) without third-party web frameworks (e.g., Gin or Chi).
  - *Rationale*: Five endpoints do not justify a router dependency. The standard library provides zero-dependency maintainability, excellent performance, and clean panic recovery middleware.
- **Layered Clean Architecture**:
  - `internal/device`: Domain models, metrics, status calculation logic, and SQLite store.
  - `internal/api`: HTTP transport layer, request validation, error mapping, and routing.
  - `cmd/server`: Service binary bootstrap and graceful shutdown.
  - `cmd/simulator`: Independent concurrent multi-device simulator.

---

## 3. Prerequisites

- **Go**: Version 1.22 or higher (tested with Go 1.27.1).
- **No external database or C compiler required**: The pure Go SQLite driver (`modernc.org/sqlite`) handles persistence without CGO or external database processes.

---

## 4. Build

Build both the server and simulator binaries:

```bash
go build -o bin/server ./cmd/server
go build -o bin/simulator ./cmd/simulator
```

---

## 5. Run Server

Start the Fleet monitoring server:

```bash
go run ./cmd/server
```

The server listens on `:8080` by default and creates `fleet.db` in the working directory on first run.

### Web UI Dashboard
Open your browser to:
```
http://localhost:8080/
```
The embedded dashboard provides:
- Live fleet health counters (`Total`, `Online`, `Offline`).
- Auto-refreshing device table with status pills, relative heartbeat timings, CPU usage bars, and signal strength.
- Status filters (`All`, `Online`, `Offline`).
- Inline device registration form to easily add new devices.
- Direct "Send Heartbeat" trigger buttons for instant testing.

### Run with Docker & Docker Compose

Fleet provides production-ready, multi-stage Alpine Dockerfiles for both the server and the simulator, running as an unprivileged user with persistent volume storage for SQLite:

```bash
# Build and start both the server and the 5-device simulator in the background
docker compose up -d

# View live container logs
docker compose logs -f

# Check health and status
docker compose ps

# Stop containers
docker compose down
```

The server exposes `http://localhost:8080/` with the dashboard, and SQLite data persists across restarts in the `fleet-data` Docker volume.

---

## 6. Run Simulator

Fleet includes a standalone concurrent simulator (`cmd/simulator`) that simulates multiple IoT devices reporting heartbeats and telemetry:

```bash
# Run simulator with 5 devices sending heartbeats every 5 seconds
go run ./cmd/simulator -url http://localhost:8080 -n 5 -interval 5s
```

### Testing Device Failure & Offline Transition
The simulator provides two ways to simulate device failure:

1. **Interactive In-Process Failure**:
   While running `go run ./cmd/simulator -n 5`, type any device number (e.g., `2`) into the terminal and hit `Enter`. The simulator will cancel device-02's heartbeat routine while the other 4 continue running:
   ```
   >>> [SIM] Device [2] device-02 STOPPED! It should transition to OFFLINE in ~30s. <<<
   ```
   Query `GET /devices/device-02` immediately (returns `ONLINE`), wait 30 seconds, and query again (transitions to `OFFLINE`).

2. **Standalone Single Device**:
   Run an individual device in its own process and stop it with `Ctrl+C`:
   ```bash
   go run ./cmd/simulator -id device-99 -interval 5s
   ```

---

## 7. Run Tests

Execute the complete test suite including race condition detection:

```bash
go test -v -race ./...
```

Unit and handler tests use isolated in-memory SQLite instances (`file::memory:?cache=shared`) for fast, deterministic, hermetic execution.

---

## 8. Example Requests

### 1. Register Device (`POST /devices`)
```bash
curl -i -X POST http://localhost:8080/devices \
  -H "Content-Type: application/json" \
  -d '{"id": "device-01", "name": "Lab Environmental Sensor"}'
```
*Response (`201 Created`)*:
```json
{
  "id": "device-01",
  "name": "Lab Environmental Sensor",
  "status": "OFFLINE"
}
```

### 2. Ingest Heartbeat (`POST /devices/{id}/heartbeat`)
```bash
curl -i -X POST http://localhost:8080/devices/device-01/heartbeat \
  -H "Content-Type: application/json" \
  -d '{
    "timestamp": "2026-09-21T10:30:00Z",
    "status": "OK",
    "cpu_usage": 42.5,
    "signal_strength": -68.0
  }'
```
*Response (`200 OK`)*:
```json
{
  "id": "device-01",
  "name": "Lab Environmental Sensor",
  "status": "ONLINE",
  "last_heartbeat": "2026-09-21T10:30:00Z",
  "last_metrics": {
    "cpu_usage": 42.5,
    "signal_strength": -68
  }
}
```
*(Note: `timestamp` is optional. If omitted, the server defaults to current server time).*

### 3. List All Devices (`GET /devices`)
```bash
# List all devices
curl -i http://localhost:8080/devices

# Optional filter by status
curl -i "http://localhost:8080/devices?status=online"
curl -i "http://localhost:8080/devices?status=offline"
```
*Response (`200 OK`)*:
```json
[
  {
    "id": "device-01",
    "name": "Lab Environmental Sensor",
    "status": "ONLINE",
    "last_heartbeat": "2026-09-21T10:30:00Z"
  }
]
```

### 4. Get Single Device Details (`GET /devices/{id}`)
```bash
curl -i http://localhost:8080/devices/device-01
```
*Response (`200 OK`)*:
```json
{
  "id": "device-01",
  "name": "Lab Environmental Sensor",
  "status": "ONLINE",
  "last_heartbeat": "2026-09-21T10:30:00Z",
  "last_metrics": {
    "cpu_usage": 42.5,
    "signal_strength": -68
  }
}
```

### 5. Fleet Summary (`GET /summary`)
```bash
curl -i http://localhost:8080/summary
```
*Response (`200 OK`)*:
```json
{
  "total": 5,
  "online": 4,
  "offline": 1
}
```

---

## 9. Assumptions

1. **Informational Device Self-Report**: The `status` field submitted by devices in `POST /devices/{id}/heartbeat` (e.g. `"status": "OK"`) is an informational self-assessment from the client hardware. The server-authoritative device connectivity status (`ONLINE` vs `OFFLINE`) is derived exclusively from heartbeat arrival time relative to the 30-second window.
2. **Persistence Boundary**: Single-node embedded SQLite with single-writer serialization is fully sufficient for hundreds of concurrent device updates without deadlock or corruption.
3. **No Authentication**: In accordance with the 3-hour hiring scope, authentication, API keys, and TLS termination are omitted.
4. **Heartbeat Timestamps**: Client timestamps are parsed and validated. Timestamps further than 1 minute in the future are rejected with `400 Bad Request` to prevent time skew exploitation.

---

## 10. Known Limitations

- **Horizontal Scalability**: Storing state in a local file-based SQLite database restricts deployment to a single server instance. Scaling out horizontally would require migrating to a network-accessible database (such as PostgreSQL) with write-ahead logging or replication.
- **Connection Throughput under Extreme Write Volume**: Because SQLite serializes writes through `SetMaxOpenConns(1)`, very high sustained write loads (>10,000 heartbeats/second) would benefit from write batching, an ingestion buffer channel, or PostgreSQL connection pooling.
- **Lack of Authentication and Rate Limiting**: The public API endpoints do not enforce client identity or per-device rate limiting.

---

## 11. What You'd Improve with One More Day

1. **Database Migration to PostgreSQL**: Replace SQLite with PostgreSQL connection pooling (`pgx` / `database/sql`), introducing schema migrations (`golang-migrate`) and horizontal replica support.
2. **Live Push Notifications (WebSockets / Server-Sent Events)**: Add a real-time event stream (`/events`) that pushes device connectivity changes and live telemetry to connected dashboards instead of requiring HTTP polling.
3. **Structured Logging and Observability**: Integrate `log/slog` for JSON-formatted structured logging with request IDs, trace contexts, and OpenTelemetry / Prometheus metrics (`/metrics`).
4. **Historical Telemetry Time-Series**: Store metric history over time (e.g. CPU and signal strength trends) rather than only the latest snapshot, allowing operators to view historical time-series graphs.
5. **Containerization & Orchestration**: Provide a multi-stage `Dockerfile` and `docker-compose.yml` for unified local developer testing.

---

## 12. AI Usage Declaration

1. **Tools Used**: Google DeepMind Antigravity AI Agent (Gemini 3.8 Flash High).
2. **What AI Was Used For**:
   - Scaffolding the initial SQLite store structure, SQL queries, and error mappings.
   - Drafting the table-driven test cases for status computation and API handlers.
   - Authoring the concurrent simulator loop and interactive stdin listener.
3. **One Thing Changed, Rejected, or Improved**:
   - *Rejected Design*: AI tooling initially considered storing a mutable `status` column in SQLite that would be periodically checked and flipped by a background worker ticker. I explicitly rejected this pattern in favor of **computing status on read** from `last_heartbeat`. This eliminated race conditions between heartbeat writes and status sweeps, prevented state desynchronization, and allowed instant, deterministic boundary testing by injecting fake clocks.
4. **One Thing Personally Verified**:
   - I ran `go test -v -race ./...` and verified that 20 concurrent goroutines executing rapid concurrent heartbeats against SQLite produced zero data races and zero database lock errors. I also verified the 30-second status boundary edge cases (`now - 29.9s` vs `now - 30.1s`) and tested the interactive device kill mechanism in the simulator to verify that status transitions accurately from `ONLINE` to `OFFLINE`.
