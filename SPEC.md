# Fleet — Mini Device Fleet Monitor

Spec for a 3-hour hiring project. Target stack: Go, since that's your strongest language and this is exactly the kind of small HTTP-service-with-concurrency problem Go is built for. Skip anything fancy — the brief explicitly penalizes unnecessary complexity.

---

## 1. Scope Decision (read this first)

You have 3 hours, and persistent storage (SQLite) is now a requirement, not optional — budget for it explicitly:

- 20 min: project skeleton, module setup, SQLite schema + connection wiring
- 60 min: core API (register, heartbeat, list, get, summary) against the SQLite store
- 20 min: status computation + timeout logic
- 25 min: simulator script
- 30 min: tests (store tests now need a temp SQLite file/`:memory:` DB, budget accordingly)
- 15 min: README + AI usage section
- 10 min: buffer (this always gets eaten — leave it in the plan rather than pretending it won't)

**Cut list if you're behind:** Docker, structured logging, filtering, Swagger docs, a UI. SQLite via `mattn/go-sqlite3` or `modernc.org/sqlite` stays in scope even under time pressure since it's now required — but keep the schema to one table and skip migrations tooling (a single `CREATE TABLE IF NOT EXISTS` at startup is enough). Don't reach for Postgres despite it being your normal stack — SQLite means zero external service for the reviewer to stand up, which matters more here than matching your usual stack. Optimize for "finishes early, is correct, is well tested" over "impressive."

### Git workflow

The brief explicitly grades commit history ("prefer meaningful commits that show the progression of your work") and lists Git usage as a separate evaluation criterion — don't treat this as an afterthought you clean up at the end. Commit at natural checkpoints as you hit them, not in one squashed dump before submission:

1. `chore: project skeleton, module setup`
2. `feat: SQLite schema + store`
3. `feat: register + heartbeat endpoints`
4. `feat: list/get/summary endpoints + computed status`
5. `feat: device simulator`
6. `test: store and handler tests`
7. `docs: README`
8. any follow-on commits for optional enhancements, each separate

A reviewer skimming `git log` should be able to reconstruct your build order from the messages alone without opening a diff.

---

## 2. Architecture

Single Go binary, layered but not over-abstracted:

```
fleet/
├── README.md
├── go.mod
├── cmd/
│   ├── server/
│   │   └── main.go          # wires everything, starts HTTP server
│   └── simulator/
│       └── main.go          # standalone simulator binary
├── internal/
│   ├── device/
│   │   ├── device.go        # Device struct, Status type, status computation
│   │   ├── store.go         # SQLite-backed store
│   │   ├── schema.sql       # CREATE TABLE statements
│   │   └── store_test.go
│   └── api/
│       ├── handlers.go      # HTTP handlers
│       ├── handlers_test.go
│       └── router.go        # route registration
├── simulator/
│   └── config.go            # (optional) device count/interval config
├── fleet.db                 # SQLite file, created at runtime — gitignore this
└── Makefile                 # optional: make run, make test, make sim
```

No repository interface abstraction, no DI framework, no separate "service layer" — one package for domain logic (`device`), one for transport (`api`). This is a 3-hour project; three layers of indirection will read as padding, not skill. The store's public methods (`Register`, `Heartbeat`, `Get`, `List`) keep the same signatures they'd have with an in-memory map, so the API layer above them doesn't know or care that persistence is involved.

### Why SQLite, not Postgres

- Requirement is "persistent storage," not "a specific RDBMS" — SQLite is a single file, needs no server process, and a reviewer's `git clone && go run` still works with zero setup.
- Postgres would mean either a `docker-compose.yml` dependency or asking the reviewer to have a local instance running — friction that works against you in a timed evaluation, even though Postgres is your normal stack.
- `mattn/go-sqlite3` (cgo, mature, most examples online) or `modernc.org/sqlite` (pure Go, no cgo, slightly newer) both work; pick `modernc.org/sqlite` if you want to avoid needing a C toolchain available during `go build` — that's one less thing that can fail on the reviewer's machine.

---

## 3. Data Model

```go
package device

import "time"

type Status string

const (
    StatusOnline  Status = "ONLINE"
    StatusOffline Status = "OFFLINE"
)

const OnlineWindow = 30 * time.Second

type Device struct {
    ID            string    `json:"id"`
    Name          string    `json:"name"`
    LastHeartbeat time.Time `json:"last_heartbeat,omitempty"`
    LastMetrics   *Metrics  `json:"last_metrics,omitempty"`
}

type Metrics struct {
    CPUUsage        *float64 `json:"cpu_usage,omitempty"`
    SignalStrength  *float64 `json:"signal_strength,omitempty"`
}

// Status is computed on read, not stored — this is the key design decision.
func (d Device) Status(now time.Time) Status {
    if d.LastHeartbeat.IsZero() {
        return StatusOffline
    }
    if now.Sub(d.LastHeartbeat) <= OnlineWindow {
        return StatusOnline
    }
    return StatusOffline
}
```

**Key decision: compute status on read, don't store it.** A background goroutine that flips a `Status` field every N seconds is a race condition waiting to happen and adds a moving part you don't need. `time.Now().Sub(lastHeartbeat) <= 30s` evaluated at request time is simpler, always correct, and trivially testable by injecting a fake clock.

---

## 4. Store (SQLite-backed, concurrency-safe)

### Schema

One table is enough — don't normalize metrics into a separate table for this scope:

```sql
CREATE TABLE IF NOT EXISTS devices (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    last_heartbeat  TIMESTAMP,        -- NULL until first heartbeat
    cpu_usage       REAL,
    signal_strength REAL
);
```

Run this once at startup (`db.Exec(schemaSQL)`), not as a separate migration tool — `golang-migrate`/`goose` are overkill for one table on a hiring project.

### Store

```go
package device

import (
    "database/sql"
    "errors"
    "time"

    _ "modernc.org/sqlite" // pure-Go driver, no cgo
)

var (
    ErrNotFound      = errors.New("device not found")
    ErrAlreadyExists = errors.New("device already exists")
)

type Store struct {
    db *sql.DB
}

func NewStore(dbPath string) (*Store, error) {
    db, err := sql.Open("sqlite", dbPath)
    if err != nil {
        return nil, err
    }
    // SQLite only supports one writer at a time — cap open connections
    // so the standard library doesn't hand out concurrent writers that
    // then serialize (or error) at the driver level anyway.
    db.SetMaxOpenConns(1)
    if _, err := db.Exec(schemaSQL); err != nil {
        return nil, err
    }
    return &Store{db: db}, nil
}

func (s *Store) Register(d *Device) error {
    _, err := s.db.Exec(
        `INSERT INTO devices (id, name) VALUES (?, ?)`,
        d.ID, d.Name,
    )
    if err != nil && isUniqueConstraintErr(err) {
        return ErrAlreadyExists
    }
    return err
}

func (s *Store) Heartbeat(id string, ts time.Time, m *Metrics) error {
    res, err := s.db.Exec(
        `UPDATE devices SET last_heartbeat = ?, cpu_usage = ?, signal_strength = ? WHERE id = ?`,
        ts, nullableFloat(m, "cpu"), nullableFloat(m, "signal"), id,
    )
    if err != nil {
        return err
    }
    n, _ := res.RowsAffected()
    if n == 0 {
        return ErrNotFound
    }
    return nil
}

func (s *Store) Get(id string) (*Device, error) {
    var d Device
    var lastHB sql.NullTime
    row := s.db.QueryRow(`SELECT id, name, last_heartbeat, cpu_usage, signal_strength FROM devices WHERE id = ?`, id)
    // scan into d + lastHB, map NULL -> zero time.Time, return ErrNotFound on sql.ErrNoRows
    return &d, nil
}

func (s *Store) List() ([]*Device, error) {
    rows, err := s.db.Query(`SELECT id, name, last_heartbeat, cpu_usage, signal_strength FROM devices`)
    // scan all rows into []*Device
    defer rows.Close()
    return nil, err
}
```

Key points worth stating explicitly in the README's design section:

- **`db.SetMaxOpenConns(1)`** — SQLite serializes writers at the file level regardless of what Go's connection pool thinks it's doing; capping to 1 avoids `database is locked` errors under concurrent heartbeats instead of discovering them under load. This is the SQLite-specific version of the same "know your read/write concurrency model" point the in-memory design made with `RWMutex`.
- Status is still **computed on read** from `last_heartbeat`, not stored as a column — same reasoning as before (section 3), it just now applies to a DB row instead of a map entry. Don't add a `status` column; it'd be a second source of truth that can drift.
- Sentinel errors (`ErrNotFound`, `ErrAlreadyExists`) still wrap driver-specific errors (`RowsAffected() == 0`, unique constraint violation) so the API layer doesn't know or care that SQLite is underneath.
- For tests, open the store against `":memory:"` instead of a file path — same schema, instant, no cleanup, and each test gets isolation by opening a fresh in-memory DB.

---

## 5. API Specification

All bodies JSON. All timestamps RFC3339 (`time.RFC3339`).

### `POST /devices` — Register a device

Request:
```json
{"id": "device-01", "name": "Lab Device 01"}
```

Responses:
- `201 Created` — body: the created device
- `400 Bad Request` — missing `id` or `name`
- `409 Conflict` — device ID already registered

### `POST /devices/{id}/heartbeat` — Receive heartbeat

Request:
```json
{"timestamp": "2026-09-21T10:30:00Z", "status": "OK", "cpu_usage": 42, "signal_strength": -71}
```

Notes:
- `timestamp` optional — if omitted, use `time.Now().UTC()` server-side. Simplifies the simulator.
- `status` field from the request body is informational only (device self-report); **don't confuse it with the computed ONLINE/OFFLINE status** — that's always server-derived from timing. Document this distinction explicitly in the README since it's a plausible source of confusion for a reviewer skimming your code.

Responses:
- `200 OK` — body: updated device
- `404 Not Found` — unregistered device ID
- `400 Bad Request` — malformed timestamp

### `GET /devices` — List all devices

Response:
```json
[
  {"id": "device-01", "name": "Lab Device 01", "status": "ONLINE", "last_heartbeat": "2026-09-21T10:30:00Z"}
]
```

Optional enhancement (cheap, do it if time allows): `?status=online|offline` query param filter.

### `GET /devices/{id}` — Device detail

Response: same shape as a list entry, plus `last_metrics` if present.
- `404 Not Found` if unregistered.

### `GET /summary` — Fleet summary

Response:
```json
{"total": 10, "online": 8, "offline": 2}
```

Computed by iterating the store snapshot and applying `Status(time.Now())` — no separate counters to keep in sync (that would be a second source of truth and a bug magnet).

---

## 6. HTTP Layer

Use the standard library (`net/http` + Go 1.22's `http.ServeMux` pattern routing) unless you're materially faster in `chi` or `gin` — for 5 routes, a router library buys you nothing and is one more thing to explain if asked "why did you pick this."

```go
mux := http.NewServeMux()
mux.HandleFunc("POST /devices", h.Register)
mux.HandleFunc("POST /devices/{id}/heartbeat", h.Heartbeat)
mux.HandleFunc("GET /devices", h.List)
mux.HandleFunc("GET /devices/{id}", h.GetOne)
mux.HandleFunc("GET /summary", h.Summary)
```

Handler responsibilities: decode JSON → validate → call store → map errors to status codes → encode JSON. Keep handlers thin; all logic lives in `device` package so it's unit-testable without spinning up HTTP.

---

## 7. Device Simulator

Separate binary, `cmd/simulator/main.go`. Requirements from the brief: ≥5 devices, heartbeat every 5s, must be able to kill one and watch it go OFFLINE.

```go
func main() {
    baseURL := flag.String("url", "http://localhost:8080", "server URL")
    n := flag.Int("n", 5, "number of devices")
    interval := flag.Duration("interval", 5*time.Second, "heartbeat interval")
    flag.Parse()

    var wg sync.WaitGroup
    ctx, cancel := context.WithCancel(context.Background())
    // handle SIGINT/SIGTERM -> cancel()

    for i := 1; i <= *n; i++ {
        id := fmt.Sprintf("device-%02d", i)
        registerDevice(*baseURL, id)
        wg.Add(1)
        go runDevice(ctx, &wg, *baseURL, id, *interval)
    }
    <-ctx.Done()
    wg.Wait()
}
```

To let a reviewer stop *one* device without killing the whole simulator (as the brief asks), either:
- Run the simulator as `go run ./cmd/simulator -n 5`, then separately document: "to simulate one device going offline, run each device as its own process with `-n 1 -id device-03`, and Ctrl+C that one," **or**
- Simpler: make the simulator print `[1] device-01  [2] device-02 ...` and read stdin for a device number to stop, killing that one goroutine while others continue.

Pick whichever you can implement in under 15 minutes — the first option is less code and just as valid; document it clearly in the README rather than over-engineering the simulator's UX.

---

## 8. Testing

Minimum bar from the brief — hit all four:

1. **Registration** — register succeeds; duplicate ID returns conflict; missing fields return 400.
2. **Heartbeat handling** — heartbeat on registered device updates `LastHeartbeat`; heartbeat on unknown device returns 404.
3. **Device status** — table-driven test on `Device.Status(now)` with injected timestamps: `now - 10s` → ONLINE, `now - 31s` → OFFLINE, zero-value `LastHeartbeat` → OFFLINE.
4. **30-second boundary** — explicit test at `now - 29.9s` and `now - 30.1s` to nail the edge, not just "roughly online/offline."

```go
func TestDeviceStatus(t *testing.T) {
    now := time.Now()
    tests := []struct {
        name string
        last time.Time
        want Status
    }{
        {"just heartbeat", now.Add(-1 * time.Second), StatusOnline},
        {"at boundary", now.Add(-30 * time.Second), StatusOnline},
        {"just past boundary", now.Add(-31 * time.Second), StatusOffline},
        {"never heartbeat", time.Time{}, StatusOffline},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            d := Device{LastHeartbeat: tt.last}
            if got := d.Status(now); got != tt.want {
                t.Errorf("got %v, want %v", got, tt.want)
            }
        })
    }
}
```

Do NOT test the 30-second timeout with a real `time.Sleep(31 * time.Second)` — that's a slow, flaky test and a red flag in review. Always inject `now` as a parameter (as above) so timing logic is deterministic and instant to test. This is worth calling out explicitly in your README's "design" section — it's a real signal of engineering judgment.

Concurrency test (optional but cheap, good signal): spin up N goroutines hammering `Heartbeat` on the same device ID concurrently with `go test -race`, confirm no panics/data races.

---

## 9. Error Handling

- Malformed JSON → `400` with `{"error": "..."}` body, not a stack trace.
- Unknown device ID on heartbeat/get → `404`.
- Duplicate registration → `409`.
- All handlers wrapped so a panic doesn't crash the whole server (recover middleware) — small addition, good practice, ~10 lines.

---

## 10. README Checklist

Per the brief's 11 required sections — don't skip any, they're explicitly graded:

1. What it does — 2-3 sentences.
2. Design/architecture — the SQLite + computed-status decisions above, stated as decisions with reasons, not just described.
3. Prerequisites — Go version (note: no separate DB server needed, SQLite file is created automatically).
4. Build — `go build ./...`
5. Run — `go run ./cmd/server`
6. Run simulator — exact command + flags.
7. Run tests — `go test ./... -race`
8. Example requests — `curl` examples for all 5 endpoints.
9. Assumptions — e.g., "heartbeat `status` field is informational and doesn't drive ONLINE/OFFLINE," "no auth," "single-writer SQLite is fine at this scale."
10. Known limitations — single SQLite file means no horizontal scaling without moving to a networked DB; no auth/rate limiting; no connection pooling beyond the capped writer.
11. What you'd improve with one more day — move from SQLite to Postgres (genuinely your normal stack, and the natural next step once you need multiple app instances), structured logging, graceful shutdown, Docker Compose for server+simulator, WebSocket/SSE push instead of polling for a live dashboard.

### AI Usage section (required, keep it honest and specific)

The brief asks for four specific things — write the section to hit all four explicitly, not just the "changed something" bullet:

1. **Which AI tools** — name them (e.g., Claude, Copilot).
2. **What you used them for** — be concrete: "scaffolding the SQLite store," "drafting table-driven status tests," not "coding help."
3. **One thing you changed, rejected, or improved** — e.g., "Claude's first draft of the store used a stored `Status` field updated by a background ticker; I rejected that in favor of computing status on read, because it removes a synchronization bug class entirely."
4. **One thing you personally verified before submitting** — e.g., "ran `go test -race` myself and confirmed no data races under concurrent heartbeats" or "manually checked the 30s boundary with `curl` against a running server rather than trusting the unit test alone."

Skipping #4 is the most common way this section reads as thin — it's the one bullet that actually demonstrates you understand and stand behind the code, not just that you used a tool.

---

## 11. Optional Enhancements — Implementation Notes

You already get persistent storage, concurrency-safety, and basic input validation "for free" from sections 4 and 9 above — the store is SQLite-backed with a capped writer connection, and handlers reject malformed/missing fields. The rest below are additions on top. Treat this list as a priority-ordered menu, not a checklist to clear top to bottom: pick based on time remaining after core + tests + README are solid. Rough cost estimates assume you're comfortable in Go.

### Input validation (5–10 min, do this first if you do anything)
Already partially covered by handler-level checks, but make it explicit and centralized rather than scattered `if` statements:

```go
func (r *RegisterRequest) Validate() error {
    if strings.TrimSpace(r.ID) == "" {
        return errors.New("id is required")
    }
    if strings.TrimSpace(r.Name) == "" {
        return errors.New("name is required")
    }
    return nil
}
```
Also validate heartbeat timestamps aren't absurdly in the future (e.g., reject `> 1 minute` ahead of server time) — cheap, and shows you thought about bad input beyond "field is present."

### Concurrency-safe implementation (already designed in, verify it)
The SQLite store in section 4 already gives you this via `db.SetMaxOpenConns(1)`, which serializes writes at the connection-pool level to match SQLite's single-writer model. What's left is *proving* it: run
```
go test ./... -race
```
and add the concurrent-heartbeat test mentioned in section 8 if you haven't. Don't add anything beyond the capped connection pool (no channels/actors/application-level locking on top) — the driver and `database/sql` are already handling this; duplicating it would be complexity for its own sake.

### Configuration through environment variables (10–15 min)
Small `internal/config` package, no library needed for this scope:

```go
type Config struct {
    Addr          string        // FLEET_ADDR, default ":8080"
    OnlineWindow  time.Duration // FLEET_ONLINE_WINDOW, default 30s
}

func Load() Config {
    cfg := Config{Addr: ":8080", OnlineWindow: 30 * time.Second}
    if v := os.Getenv("FLEET_ADDR"); v != "" {
        cfg.Addr = v
    }
    if v := os.Getenv("FLEET_ONLINE_WINDOW"); v != "" {
        if d, err := time.ParseDuration(v); err == nil {
            cfg.OnlineWindow = d
        }
    }
    return cfg
}
```
Making the 30s window configurable is a nice touch — lets a reviewer test the timeout without waiting 30 real seconds (`FLEET_ONLINE_WINDOW=3s`). Worth doing even in isolation from the rest of this section.

### Graceful shutdown (10 min)
Standard Go pattern, cheap and a genuine good-practice signal:

```go
srv := &http.Server{Addr: cfg.Addr, Handler: mux}

go func() {
    if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
        log.Fatalf("server error: %v", err)
    }
}()

stop := make(chan os.Signal, 1)
signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
<-stop

ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
srv.Shutdown(ctx)
```
Since there's no persistent connection state or in-flight writes that need draining beyond what `Shutdown` already handles, this is mostly about not dropping in-flight HTTP requests on Ctrl+C. Low effort, put it in if you have 10 spare minutes.

### Filtering devices by status (5 min)
Extend `GET /devices` handler:
```go
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
    statusFilter := r.URL.Query().Get("status") // "online" | "offline" | ""
    devices := h.store.List()
    now := time.Now()
    var result []DeviceResponse
    for _, d := range devices {
        s := d.Status(now)
        if statusFilter != "" && !strings.EqualFold(string(s), statusFilter) {
            continue
        }
        result = append(result, toResponse(d, s))
    }
    json.NewEncoder(w).Encode(result)
}
```
Cheapest enhancement on this whole list relative to how visible it is in a demo. Do this before Docker or a UI.

### Docker support (15–20 min)
Multi-stage build, single binary, no external deps to worry about:

```dockerfile
FROM golang:1.23-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o fleet ./cmd/server

FROM alpine:3.20
COPY --from=build /app/fleet /fleet
EXPOSE 8080
ENTRYPOINT ["/fleet"]
```
Add a second stage or separate `Dockerfile.simulator` only if you have time to spare — don't let Docker for the simulator block finishing the server's Dockerfile. A `docker-compose.yml` running both is a nice-to-have, not required by the brief; skip it if pressed for time.

### Simple UI (20–30 min — lowest priority)
A single static HTML file served at `/` that polls `GET /devices` and `GET /summary` every few seconds and renders a table — plain JS, no framework, no build step:
```go
mux.Handle("GET /", http.FileServer(http.Dir("./web")))
```
This is the enhancement with the worst effort-to-signal ratio for this specific evaluation — the brief says explicitly they're grading correctness/structure/tests/docs, not visual polish. Only do this if everything else, including the "what I'd improve" README section, is already done.

## 12. What NOT to add

The brief explicitly warns against padding. Skip unless you finish with 45+ minutes to spare:

- Swagger/OpenAPI generation
- A frontend/UI
- Auth
- Postgres (SQLite already covers the persistence requirement — don't upgrade the DB engine just because it's more familiar)
- Kubernetes/Docker Compose multi-service setups
- A generic "repository pattern" abstraction with multiple storage backends — one SQLite implementation is enough

Every one of these is a plausible "optional enhancement" per the brief, but each also risks eating time you need for tests and README — which are explicitly graded and easy to half-finish under time pressure. Correctness + clarity beats breadth here.
