# WebUI Cron Management Panel — Design Spec

## Overview

Add a full cron/scheduled-task management panel to the PicoClaw WebUI Launcher, accessible at `/config/cron`. The panel provides CRUD operations for cron jobs, manual trigger execution, execution history with SQLite persistence, and a statistics overview with a 7-day trend chart.

## Context

PicoClaw has a fully functional cron engine (`pkg/cron/service.go`) supporting three schedule types: one-time (`at`), recurring (`every`), and cron expressions (`cron`). Currently, cron jobs can only be managed via CLI commands or Agent Tool — there is no web-based management interface. This design adds the missing WebUI layer without modifying the existing cron engine's core behavior.

### Process architecture

The gateway runs as a **separate OS process** spawned by the web backend (`web/backend/api/gateway.go`). The two communicate via:

- **PID file** (`picoclaw.pid.json`) — port, host, auth token discovery
- **Gateway internal HTTP API** (`pkg/health/server.go`) — health/ready/reload endpoints on port 18790
- **WebSocket proxy** — frontend WebSocket tunneled through web backend to gateway

The `CronService` lives in the **gateway process**. The web backend has no direct access to it. All cron operations must go through the gateway's internal HTTP API.

## Architecture

```
WebUI Frontend (/config/cron)
  ├── StatsBar, Jobs Tab, History Tab, Dialogs
  │
  │ fetch / TanStack Query
  ▼
Web Backend (/api/cron/*)                    [launcher process]
  └── CronProxyHandler
       │ HTTP proxy (Bearer token from PID file)
       ▼
Gateway Internal API (/cron/*)               [gateway process]
  └── CronAPIHandler
       ├── CronService (existing)
       └── HistoryStore (NEW, SQLite)
            │
            ▼
Storage
  ├── {workspace}/cron/jobs.json    (existing, unchanged)
  └── {workspace}/cron/history.db   (NEW, SQLite WAL)
```

### Key decisions

1. **Job storage unchanged** — job definitions remain in `jobs.json` for CLI/Agent Tool compatibility.
2. **Execution history in separate SQLite DB** — `{workspace}/cron/history.db`, independent from `seahorse.db` and `launcher-auth.db`, following the project's pattern of per-module storage.
3. **Two-layer API** — gateway owns the cron endpoints (direct access to CronService + HistoryStore); web backend proxies requests using the PID file token, matching the existing health-probe pattern.
4. **Listener pattern** — `CronService` gains an optional `ExecutionListener` callback; the HistoryStore implements it within the same gateway process.
5. **Retention policy** — history capped at 10,000 records; oldest auto-pruned on write.

## Data Model

### SQLite Schema (`history.db`)

```sql
CREATE TABLE cron_executions (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id      TEXT    NOT NULL,
    job_name    TEXT    NOT NULL,
    trigger     TEXT    NOT NULL DEFAULT 'scheduled',  -- 'scheduled' | 'manual'
    status      TEXT    NOT NULL,                       -- 'ok' | 'error'
    error_msg   TEXT    NOT NULL DEFAULT '',
    output      TEXT    NOT NULL DEFAULT '',            -- truncated to 4KB
    duration_ms INTEGER NOT NULL DEFAULT 0,
    started_at  INTEGER NOT NULL,                       -- Unix ms
    finished_at INTEGER NOT NULL                        -- Unix ms
);

CREATE INDEX idx_exec_job_id ON cron_executions(job_id, started_at DESC);
CREATE INDEX idx_exec_started ON cron_executions(started_at DESC);
```

- `job_name` stored redundantly so history remains readable after job deletion.
- `trigger` distinguishes scheduled vs. manual executions.
- `output` capped at 4KB per record to prevent unbounded growth.
- **Retention:** after each write, if total rows exceed 10,000, delete oldest rows to maintain the cap.

### Existing CronJob model (unchanged)

```
CronJob {
  ID, Name, Enabled, DeleteAfterRun,
  Schedule { Kind, Expr, TZ, EveryMS, AtMS },
  Payload  { Kind, Message, Command, Channel, To },
  State    { NextRunAtMS, LastRunAtMS, LastStatus, LastError },
  CreatedAtMS, UpdatedAtMS
}
```

## REST API

### Frontend-facing endpoints (web backend, proxied)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/cron/jobs` | List all jobs (with State) |
| POST | `/api/cron/jobs` | Create a job |
| PUT | `/api/cron/jobs/:id` | Update a job (name, schedule, payload) |
| DELETE | `/api/cron/jobs/:id` | Delete a job |
| POST | `/api/cron/jobs/:id/enable` | Enable a job |
| POST | `/api/cron/jobs/:id/disable` | Disable a job |
| POST | `/api/cron/jobs/:id/trigger` | Manually trigger execution |
| GET | `/api/cron/history` | Execution history (paginated: `?page=1&size=20&job_id=xxx`) |
| GET | `/api/cron/stats` | Stats overview |
| GET | `/api/cron/stats/trend` | 7-day trend (default; optional `?days=N`) |

### Gateway internal endpoints (same paths without `/api` prefix)

The gateway registers `/cron/*` on its internal HTTP mux (`pkg/health/server.go`), protected by Bearer token auth (same as `/reload`).

### Request/response examples

**POST /api/cron/jobs**

```json
{
  "name": "Daily status check",
  "schedule": {
    "kind": "cron",
    "expr": "0 9 * * *",
    "tz": "Asia/Shanghai"
  },
  "payload": {
    "message": "Check system status and report",
    "command": "",
    "channel": "telegram",
    "to": "chat_123"
  }
}
```

Notes:
- `payload.kind` is always `"agent_turn"`, set server-side. Not accepted in request.
- `command` requires `tools.cron.allow_command` to be enabled; rejected with 403 otherwise.
- Only the fields relevant to the chosen `schedule.kind` are required (`expr`+`tz` for cron, `every_ms` for every, `at_ms` for at). Others are ignored.
- `DeleteAfterRun` is auto-set to `true` for `kind: "at"` jobs, server-side.

**PUT /api/cron/jobs/:id**

```json
{
  "name": "Updated name",
  "schedule": { "kind": "every", "every_ms": 7200000 },
  "payload": { "message": "New message", "channel": "discord", "to": "" }
}
```

Notes:
- Replaces name, schedule, and payload entirely. State fields are not accepted.
- `NextRunAtMS` is recomputed server-side after update.

**GET /api/cron/stats**

```json
{
  "total_jobs": 8,
  "enabled_jobs": 5,
  "runs_24h": 23,
  "success_24h": 22,
  "errors_24h": 1,
  "success_rate_24h": 95.65
}
```

Notes:
- `total_jobs` and `enabled_jobs` come from `CronService.ListJobs()`.
- `runs_24h`, `success_24h`, `errors_24h` come from `HistoryStore`.
- Both sources are in the same gateway process — no cross-process aggregation needed.

**GET /api/cron/stats/trend?days=7**

```json
{
  "trend": [
    { "date": "2026-04-11", "ok": 12, "error": 1 },
    { "date": "2026-04-12", "ok": 15, "error": 0 }
  ]
}
```

**Error response format**

All error responses use a consistent envelope:

```json
{
  "error": "job not found",
  "code": 404
}
```

### Manual trigger behavior

`POST /api/cron/jobs/:id/trigger` executes the job regardless of its `Enabled` state. This allows debugging disabled jobs. The execution is recorded with `trigger: "manual"`.

## Backend Changes

### New files

| File | Responsibility |
|------|----------------|
| `pkg/cron/history.go` | HistoryStore: SQLite init (WAL mode, busy_timeout=5000), write record, query by job, paginate, stats, trend, retention pruning |
| `pkg/cron/history_test.go` | Unit tests for HistoryStore |
| `pkg/cron/api.go` | CronAPIHandler: gateway-side HTTP handlers for `/cron/*` routes, registered on health server mux |
| `pkg/cron/api_test.go` | Gateway API handler tests |
| `web/backend/api/cron.go` | CronProxyHandler: proxies `/api/cron/*` to gateway internal API using PID file token |

### Modified files

| File | Change |
|------|--------|
| `pkg/cron/service.go` | Add `ExecutionListener` interface, `SetListener()`, `TriggerJob()` method; call listener at end of `executeJob()`. Extend `AddJob` signature to accept optional `command` field. |
| `pkg/health/server.go` | Accept optional `CronAPIHandler` and register `/cron/*` routes on mux |
| `pkg/gateway/gateway.go` | In `setupCronTool()`: create HistoryStore, set as listener on CronService, pass CronAPIHandler to health server |
| `web/backend/api/router.go` | Register `/api/cron/*` proxy route group |

### ExecutionListener interface

```go
// pkg/cron/history.go
type ExecutionRecord struct {
    JobID      string
    JobName    string
    Trigger    string // "scheduled" | "manual"
    Status     string // "ok" | "error"
    ErrorMsg   string
    Output     string // truncated to 4KB
    DurationMS int64
    StartedAt  int64  // Unix ms
    FinishedAt int64  // Unix ms
}

type ExecutionListener interface {
    OnExecutionComplete(record ExecutionRecord)
}
```

### CronService additions

- `SetListener(l ExecutionListener)` — sets the optional listener (nil-safe).
- `TriggerJob(jobID string) error` — finds job by ID, executes immediately via existing `executeJob` logic, with `trigger="manual"`. Runs regardless of `Enabled` state.
- At the end of `executeJob()`: `if s.listener != nil { s.listener.OnExecutionComplete(record) }`.

### HistoryStore lifecycle (gateway process)

1. Created in `pkg/gateway/gateway.go` during `setupCronTool()`.
2. Registered as `ExecutionListener` on `CronService` via `SetListener()`.
3. Passed to `CronAPIHandler` for query endpoints.
4. `Close()` called in `stopAndCleanupServices()`.

### Web backend proxy pattern

`CronProxyHandler` in `web/backend/api/cron.go`:
- Discovers gateway host/port from PID file (same as `getGatewayHealth()`).
- Forwards all `/api/cron/*` requests to `http://{gateway}/cron/*` with Bearer token from PID file.
- Returns gateway response as-is to frontend.
- Returns 502 if gateway is unreachable.

## Frontend Changes

### New files

```
web/frontend/src/
├── api/cron.ts                      # API client (TanStack Query hooks)
├── components/cron/
│   ├── cron-page.tsx                # Main page: Stats + Tabs container
│   ├── cron-stats.tsx               # Stats cards + 7-day trend mini chart
│   ├── cron-job-list.tsx            # Jobs Tab: table + action buttons
│   ├── cron-job-form.tsx            # Create/Edit Dialog (simple + advanced mode toggle)
│   ├── cron-history.tsx             # History Tab: table + filter + pagination
│   └── cron-output-dialog.tsx       # Execution output detail Dialog
└── routes/config.cron.tsx           # /config/cron route file
```

### Modified files

| File | Change |
|------|--------|
| `i18n/locales/en.json` | Add `pages.cron.*` translation keys |
| `i18n/locales/zh.json` | Add `pages.cron.*` translation keys |
| `components/app-sidebar.tsx` | Add "Cron" nav item under Config section |

### UI layout

- **Route:** `/config/cron`
- **Layout:** Stats bar (always visible) + two tabs ("Jobs" / "Execution History")
- **Stats bar:** Total Jobs, Enabled, 24h Runs, Success Rate, 7-Day Trend mini bar chart
- **Jobs Tab:** Table with columns: Name, Schedule, Channel, Last Run, Status, Actions (trigger/edit/enable-disable/delete)
- **History Tab:** Table with columns: Job, Time, Trigger, Status, Duration, Output (View link opens dialog); job filter dropdown; pagination
- **Job Form Dialog:** Simple mode (radio: one-time/recurring/cron expression, friendly inputs) with toggle to Advanced mode (raw cron expression input + human-readable preview)
- **Delete confirmation:** AlertDialog
- **Empty states:** Friendly messages when no jobs exist or no history records

### i18n

Both `en.json` and `zh.json` get `pages.cron.*` keys covering all labels, buttons, table headers, status text, empty states, form validation messages, and error messages.

## Security

- **Gateway internal API:** Protected by Bearer token auth (same mechanism as `/reload`).
- **Web backend proxy:** Protected by existing launcher cookie/token auth (`launcherFetch`).
- **Shell commands:** Scheduling a job with `command` requires `tools.cron.allow_command` config to be `true`; otherwise the gateway returns 403.
- No additional auth layer needed.

## Testing

### Backend

| Layer | File | Coverage |
|-------|------|----------|
| Storage | `pkg/cron/history_test.go` | Schema creation, record write/read, job_id filter, pagination, stats, trend, concurrent writes, output truncation (>4KB), empty DB edge case, retention pruning at 10,000 |
| Gateway API | `pkg/cron/api_test.go` | All handler endpoints: CRUD, enable/disable, trigger, history query, stats, trend, validation errors (400), not found (404), command without allow_command (403) |
| Service | `pkg/cron/service_test.go` (append) | ExecutionListener callback fires, TriggerJob manual execution (enabled and disabled jobs) |
| Proxy | `web/backend/api/cron_test.go` | Proxy forwards correctly, 502 when gateway down |

### Frontend

No frontend tests (project has no existing test framework configured). Consistent with current codebase.

## Delivery Phases

### Phase 1: Backend API + Frontend CRUD

**Backend:**
- `pkg/cron/history.go` + `history_test.go` — HistoryStore
- `pkg/cron/api.go` + `api_test.go` — Gateway-side HTTP handlers
- `pkg/cron/service.go` — ExecutionListener, SetListener, TriggerJob
- `pkg/health/server.go` — Register cron routes
- `pkg/gateway/gateway.go` — Initialize HistoryStore + wire up
- `web/backend/api/cron.go` — Proxy handler
- `web/backend/api/router.go` — Register proxy routes

**Frontend:**
- `api/cron.ts`, `routes/config.cron.tsx`
- `components/cron/cron-page.tsx`, `cron-job-list.tsx`, `cron-job-form.tsx`
- i18n keys, sidebar nav item

**Deliverable:** Job list, create/edit/delete/enable/disable/manual trigger in WebUI

### Phase 2: Execution History

**Frontend:**
- `components/cron/cron-history.tsx`, `cron-output-dialog.tsx`

**Deliverable:** History Tab with filter, pagination, output viewer

### Phase 3: Stats + Trend

**Frontend:**
- `components/cron/cron-stats.tsx`

**Deliverable:** Complete panel with stats cards and 7-day trend chart

### Git strategy

- Branch: `feat/webui-cron-management` (from `main`)
- One commit per phase
- User merges to main
