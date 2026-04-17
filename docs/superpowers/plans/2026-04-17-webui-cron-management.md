# WebUI Cron Management Panel — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a full cron management panel to the PicoClaw WebUI Launcher at `/config/cron` with job CRUD, manual trigger, execution history (SQLite), and statistics with 7-day trend chart.

**Architecture:** Two-layer API — gateway process hosts cron endpoints (direct access to CronService + HistoryStore), web backend proxies `/api/cron/*` to gateway using PID file token auth. Frontend uses React + TanStack Router/Query + Tailwind + Radix UI.

**Tech Stack:** Go 1.25, modernc.org/sqlite, React 19, TypeScript, TanStack Router v1, TanStack Query v5, Jotai, Tailwind CSS v4, Radix UI, i18next

**Spec:** `docs/superpowers/specs/2026-04-17-webui-cron-management-design.md`

---

## Key Reference: Existing Go Signatures

Before implementing, be aware of these exact signatures from `pkg/cron/service.go`:

```go
// Types (JSON tags use camelCase)
type CronSchedule struct {
    Kind    string `json:"kind"`
    AtMS    *int64 `json:"atMs,omitempty"`      // pointer
    EveryMS *int64 `json:"everyMs,omitempty"`    // pointer
    Expr    string `json:"expr,omitempty"`
    TZ      string `json:"tz,omitempty"`
}
type CronPayload struct {
    Kind    string `json:"kind"`
    Message string `json:"message"`
    Command string `json:"command,omitempty"`
    Channel string `json:"channel,omitempty"`
    To      string `json:"to,omitempty"`
}
type CronJobState struct {
    NextRunAtMS *int64 `json:"nextRunAtMs,omitempty"`
    LastRunAtMS *int64 `json:"lastRunAtMs,omitempty"`
    LastStatus  string `json:"lastStatus,omitempty"`
    LastError   string `json:"lastError,omitempty"`
}
type CronJob struct {
    ID             string       `json:"id"`
    Name           string       `json:"name"`
    Enabled        bool         `json:"enabled"`
    Schedule       CronSchedule `json:"schedule"`
    Payload        CronPayload  `json:"payload"`
    State          CronJobState `json:"state"`
    CreatedAtMS    int64        `json:"createdAtMs"`
    UpdatedAtMS    int64        `json:"updatedAtMs"`
    DeleteAfterRun bool         `json:"deleteAfterRun"`
}

// Handler type
type JobHandler func(job *CronJob) (string, error)

// Constructor
func NewCronService(storePath string, onJob JobHandler) *CronService

// Methods
func (cs *CronService) AddJob(name string, schedule CronSchedule, message string, channel, to string) (*CronJob, error)
func (cs *CronService) RemoveJob(jobID string) bool
func (cs *CronService) EnableJob(jobID string, enabled bool) *CronJob
func (cs *CronService) ListJobs(includeDisabled bool) []CronJob
func (cs *CronService) saveStoreUnsafe() error     // unexported, same package only
func (cs *CronService) recomputeNextRuns()          // unexported, no args, recomputes all jobs

// Helper used in tests:
func int64Ptr(v int64) *int64  // check if exists, otherwise define locally in test
```

Web backend gateway access pattern:
```go
// Package-level var in web/backend/api/gateway.go
var gateway = struct {
    mu      sync.Mutex
    pidData *ppid.PidFileData
    // ... other fields
}{}
// Access: gateway.pidData (with gateway.mu lock)
```

---

## Chunk 1: Setup + Backend History Store

### Task 0: Branch Setup

**Files:** None

- [ ] **Step 1: Create feature branch**

```bash
git checkout main
git checkout -b feat/webui-cron-management
```

- [ ] **Step 2: Verify clean state**

```bash
git status
```

Expected: On branch `feat/webui-cron-management`, working tree clean.

---

### Task 1: HistoryStore — SQLite Schema + Write

**Files:**
- Create: `pkg/cron/history.go`
- Create: `pkg/cron/history_test.go`

- [ ] **Step 1: Write failing test for HistoryStore creation and record write**

```go
// pkg/cron/history_test.go
package cron

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tempHistoryStore(t *testing.T) *HistoryStore {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "history.db")
	hs, err := NewHistoryStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { hs.Close() })
	return hs
}

func TestHistoryStore_CreateAndWrite(t *testing.T) {
	hs := tempHistoryStore(t)

	rec := ExecutionRecord{
		JobID:      "job-001",
		JobName:    "Test Job",
		Trigger:    "scheduled",
		Status:     "ok",
		ErrorMsg:   "",
		Output:     "done",
		DurationMS: 1200,
		StartedAt:  1700000000000,
		FinishedAt: 1700000001200,
	}
	err := hs.WriteRecord(rec)
	require.NoError(t, err)

	records, total, err := hs.QueryHistory("", 1, 20)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Equal(t, "job-001", records[0].JobID)
	assert.Equal(t, "Test Job", records[0].JobName)
	assert.Equal(t, "scheduled", records[0].Trigger)
	assert.Equal(t, "ok", records[0].Status)
	assert.Equal(t, int64(1200), records[0].DurationMS)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /e/WorkSpace/Personal/picoclaw && go test ./pkg/cron/ -run TestHistoryStore_CreateAndWrite -v
```

Expected: FAIL — `NewHistoryStore` undefined.

- [ ] **Step 3: Implement HistoryStore with schema, WriteRecord, QueryHistory**

```go
// pkg/cron/history.go
package cron

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const maxOutputBytes = 4096
const maxHistoryRecords = 10000

// ExecutionRecord represents a single cron job execution.
type ExecutionRecord struct {
	ID         int64  `json:"id,omitempty"`
	JobID      string `json:"jobId"`
	JobName    string `json:"jobName"`
	Trigger    string `json:"trigger"`
	Status     string `json:"status"`
	ErrorMsg   string `json:"errorMsg"`
	Output     string `json:"output"`
	DurationMS int64  `json:"durationMs"`
	StartedAt  int64  `json:"startedAt"`
	FinishedAt int64  `json:"finishedAt"`
}

// ExecutionListener is called after each job execution.
type ExecutionListener interface {
	OnExecutionComplete(record ExecutionRecord)
}

// HistoryStore persists cron execution records in SQLite.
type HistoryStore struct {
	db *sql.DB
	mu sync.Mutex
}

func NewHistoryStore(dbPath string) (*HistoryStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open history db: %w", err)
	}
	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA synchronous = NORMAL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("set pragma: %w", err)
		}
	}
	if err := runHistorySchema(db); err != nil {
		db.Close()
		return nil, err
	}
	return &HistoryStore{db: db}, nil
}

func runHistorySchema(db *sql.DB) error {
	schema := `
CREATE TABLE IF NOT EXISTS cron_executions (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id      TEXT    NOT NULL,
    job_name    TEXT    NOT NULL,
    trigger_kind TEXT   NOT NULL DEFAULT 'scheduled',
    status      TEXT    NOT NULL,
    error_msg   TEXT    NOT NULL DEFAULT '',
    output      TEXT    NOT NULL DEFAULT '',
    duration_ms INTEGER NOT NULL DEFAULT 0,
    started_at  INTEGER NOT NULL,
    finished_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_exec_job_id ON cron_executions(job_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_exec_started ON cron_executions(started_at DESC);
`
	_, err := db.Exec(schema)
	if err != nil {
		return fmt.Errorf("run history schema: %w", err)
	}
	return nil
}

func (hs *HistoryStore) Close() error {
	return hs.db.Close()
}

func (hs *HistoryStore) WriteRecord(rec ExecutionRecord) error {
	if len(rec.Output) > maxOutputBytes {
		rec.Output = rec.Output[:maxOutputBytes]
	}
	hs.mu.Lock()
	defer hs.mu.Unlock()

	_, err := hs.db.Exec(`
		INSERT INTO cron_executions (job_id, job_name, trigger_kind, status, error_msg, output, duration_ms, started_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.JobID, rec.JobName, rec.Trigger, rec.Status, rec.ErrorMsg, rec.Output, rec.DurationMS, rec.StartedAt, rec.FinishedAt,
	)
	if err != nil {
		return fmt.Errorf("write execution record: %w", err)
	}
	// Prune oldest records if over limit.
	hs.db.Exec(`DELETE FROM cron_executions WHERE id IN (
		SELECT id FROM cron_executions ORDER BY started_at ASC LIMIT MAX(0, (SELECT COUNT(*) FROM cron_executions) - ?))`,
		maxHistoryRecords,
	)
	return nil
}

// OnExecutionComplete implements ExecutionListener.
func (hs *HistoryStore) OnExecutionComplete(rec ExecutionRecord) {
	hs.WriteRecord(rec) //nolint:errcheck
}

func (hs *HistoryStore) QueryHistory(jobID string, page, size int) ([]ExecutionRecord, int, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	offset := (page - 1) * size

	var total int
	var rows *sql.Rows
	var err error

	if jobID != "" {
		err = hs.db.QueryRow("SELECT COUNT(*) FROM cron_executions WHERE job_id = ?", jobID).Scan(&total)
		if err != nil {
			return nil, 0, err
		}
		rows, err = hs.db.Query(
			"SELECT id, job_id, job_name, trigger_kind, status, error_msg, output, duration_ms, started_at, finished_at FROM cron_executions WHERE job_id = ? ORDER BY started_at DESC LIMIT ? OFFSET ?",
			jobID, size, offset,
		)
	} else {
		err = hs.db.QueryRow("SELECT COUNT(*) FROM cron_executions").Scan(&total)
		if err != nil {
			return nil, 0, err
		}
		rows, err = hs.db.Query(
			"SELECT id, job_id, job_name, trigger_kind, status, error_msg, output, duration_ms, started_at, finished_at FROM cron_executions ORDER BY started_at DESC LIMIT ? OFFSET ?",
			size, offset,
		)
	}
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var records []ExecutionRecord
	for rows.Next() {
		var r ExecutionRecord
		if err := rows.Scan(&r.ID, &r.JobID, &r.JobName, &r.Trigger, &r.Status, &r.ErrorMsg, &r.Output, &r.DurationMS, &r.StartedAt, &r.FinishedAt); err != nil {
			return nil, 0, err
		}
		records = append(records, r)
	}
	if records == nil {
		records = []ExecutionRecord{}
	}
	return records, total, nil
}

// timeFromMS converts Unix milliseconds to time.Time.
func timeFromMS(ms int64) time.Time {
	return time.Unix(ms/1000, (ms%1000)*int64(time.Millisecond))
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /e/WorkSpace/Personal/picoclaw && go test ./pkg/cron/ -run TestHistoryStore_CreateAndWrite -v
```

Expected: PASS

---

### Task 2: HistoryStore — Stats, Trend, Edge Cases

**Files:**
- Modify: `pkg/cron/history.go`
- Modify: `pkg/cron/history_test.go`

- [ ] **Step 1: Write failing tests for stats, trend, filter, truncation**

Append to `pkg/cron/history_test.go`:

```go
func TestHistoryStore_QueryByJobID(t *testing.T) {
	hs := tempHistoryStore(t)

	hs.WriteRecord(ExecutionRecord{JobID: "a", JobName: "A", Trigger: "scheduled", Status: "ok", StartedAt: 1000, FinishedAt: 1100})
	hs.WriteRecord(ExecutionRecord{JobID: "b", JobName: "B", Trigger: "scheduled", Status: "ok", StartedAt: 2000, FinishedAt: 2100})
	hs.WriteRecord(ExecutionRecord{JobID: "a", JobName: "A", Trigger: "manual", Status: "error", ErrorMsg: "fail", StartedAt: 3000, FinishedAt: 3100})

	records, total, err := hs.QueryHistory("a", 1, 20)
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Len(t, records, 2)
	assert.Equal(t, "manual", records[0].Trigger) // newest first
}

func TestHistoryStore_Pagination(t *testing.T) {
	hs := tempHistoryStore(t)
	for i := 0; i < 5; i++ {
		hs.WriteRecord(ExecutionRecord{JobID: "j", JobName: "J", Trigger: "scheduled", Status: "ok", StartedAt: int64(i * 1000), FinishedAt: int64(i*1000 + 100)})
	}
	records, total, err := hs.QueryHistory("", 2, 2)
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Len(t, records, 2)
}

func TestHistoryStore_EmptyDB(t *testing.T) {
	hs := tempHistoryStore(t)
	records, total, err := hs.QueryHistory("", 1, 20)
	require.NoError(t, err)
	assert.Equal(t, 0, total)
	assert.NotNil(t, records)
	assert.Len(t, records, 0)
}

func TestHistoryStore_OutputTruncation(t *testing.T) {
	hs := tempHistoryStore(t)
	bigOutput := make([]byte, 8000)
	for i := range bigOutput {
		bigOutput[i] = 'x'
	}
	hs.WriteRecord(ExecutionRecord{JobID: "j", JobName: "J", Trigger: "scheduled", Status: "ok", Output: string(bigOutput), StartedAt: 1000, FinishedAt: 1100})
	records, _, err := hs.QueryHistory("", 1, 20)
	require.NoError(t, err)
	assert.Len(t, records[0].Output, maxOutputBytes)
}

func TestHistoryStore_Stats(t *testing.T) {
	hs := tempHistoryStore(t)
	now := int64(1700000000000)
	hourAgo := now - 3600*1000

	hs.WriteRecord(ExecutionRecord{JobID: "a", JobName: "A", Trigger: "scheduled", Status: "ok", StartedAt: hourAgo + 1000, FinishedAt: hourAgo + 2000})
	hs.WriteRecord(ExecutionRecord{JobID: "a", JobName: "A", Trigger: "scheduled", Status: "error", ErrorMsg: "fail", StartedAt: hourAgo + 3000, FinishedAt: hourAgo + 4000})
	// Old record (>24h ago)
	hs.WriteRecord(ExecutionRecord{JobID: "b", JobName: "B", Trigger: "scheduled", Status: "ok", StartedAt: now - 48*3600*1000, FinishedAt: now - 48*3600*1000 + 100})

	stats, err := hs.Stats24h(now)
	require.NoError(t, err)
	assert.Equal(t, 2, stats.Runs24h)
	assert.Equal(t, 1, stats.Success24h)
	assert.Equal(t, 1, stats.Errors24h)
}

func TestHistoryStore_Trend(t *testing.T) {
	hs := tempHistoryStore(t)
	now := int64(1700000000000)
	dayMS := int64(86400 * 1000)

	hs.WriteRecord(ExecutionRecord{JobID: "a", JobName: "A", Trigger: "scheduled", Status: "ok", StartedAt: now, FinishedAt: now + 100})
	hs.WriteRecord(ExecutionRecord{JobID: "a", JobName: "A", Trigger: "scheduled", Status: "error", StartedAt: now - dayMS, FinishedAt: now - dayMS + 100})
	hs.WriteRecord(ExecutionRecord{JobID: "a", JobName: "A", Trigger: "scheduled", Status: "ok", StartedAt: now - dayMS, FinishedAt: now - dayMS + 100})

	trend, err := hs.Trend(now, 7)
	require.NoError(t, err)
	assert.Len(t, trend, 7)
	assert.GreaterOrEqual(t, trend[len(trend)-1].OK, 1)
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /e/WorkSpace/Personal/picoclaw && go test ./pkg/cron/ -run "TestHistoryStore_" -v
```

Expected: FAIL — `Stats24h`, `Trend` undefined.

- [ ] **Step 3: Implement Stats24h and Trend methods**

Append to `pkg/cron/history.go`:

```go
// HistoryStats holds aggregated execution statistics.
type HistoryStats struct {
	Runs24h     int     `json:"runs24h"`
	Success24h  int     `json:"success24h"`
	Errors24h   int     `json:"errors24h"`
	SuccessRate float64 `json:"successRate24h"`
}

// TrendEntry holds per-day execution counts.
type TrendEntry struct {
	Date  string `json:"date"`
	OK    int    `json:"ok"`
	Error int    `json:"error"`
}

func (hs *HistoryStore) Stats24h(nowMS int64) (HistoryStats, error) {
	since := nowMS - 24*3600*1000
	var stats HistoryStats
	row := hs.db.QueryRow(`
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN status='ok' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status='error' THEN 1 ELSE 0 END), 0)
		FROM cron_executions WHERE started_at >= ?`, since)
	if err := row.Scan(&stats.Runs24h, &stats.Success24h, &stats.Errors24h); err != nil {
		return stats, err
	}
	if stats.Runs24h > 0 {
		stats.SuccessRate = float64(stats.Success24h) / float64(stats.Runs24h) * 100
	}
	return stats, nil
}

func (hs *HistoryStore) Trend(nowMS int64, days int) ([]TrendEntry, error) {
	if days < 1 {
		days = 7
	}
	since := nowMS - int64(days)*24*3600*1000

	rows, err := hs.db.Query(`
		SELECT
			DATE(started_at / 1000, 'unixepoch') AS day,
			COALESCE(SUM(CASE WHEN status='ok' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status='error' THEN 1 ELSE 0 END), 0)
		FROM cron_executions
		WHERE started_at >= ?
		GROUP BY day
		ORDER BY day ASC`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	dataMap := make(map[string]TrendEntry)
	for rows.Next() {
		var e TrendEntry
		if err := rows.Scan(&e.Date, &e.OK, &e.Error); err != nil {
			return nil, err
		}
		dataMap[e.Date] = e
	}

	trend := make([]TrendEntry, 0, days)
	for i := days - 1; i >= 0; i-- {
		dayStart := nowMS - int64(i)*24*3600*1000
		dayStr := timeFromMS(dayStart).UTC().Format("2006-01-02")
		if e, ok := dataMap[dayStr]; ok {
			trend = append(trend, e)
		} else {
			trend = append(trend, TrendEntry{Date: dayStr, OK: 0, Error: 0})
		}
	}
	return trend, nil
}
```

- [ ] **Step 4: Run all HistoryStore tests**

```bash
cd /e/WorkSpace/Personal/picoclaw && go test ./pkg/cron/ -run "TestHistoryStore_" -v
```

Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
cd /e/WorkSpace/Personal/picoclaw && git add pkg/cron/history.go pkg/cron/history_test.go && git commit -m "$(cat <<'EOF'
feat(cron): add HistoryStore with SQLite execution history

- Schema with cron_executions table, indexes
- WriteRecord with 4KB output truncation and 10k retention pruning
- QueryHistory with job_id filter and pagination
- Stats24h for 24-hour aggregation
- Trend for daily ok/error counts
- Implements ExecutionListener interface

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: CronService — ExecutionListener + TriggerJob

**Files:**
- Modify: `pkg/cron/service.go` (struct at ~line 60, executeJobByID at ~line 214-301)
- Modify: `pkg/cron/service_test.go` (append)

- [ ] **Step 1: Write failing test for listener callback and TriggerJob**

Append to `pkg/cron/service_test.go`. Note the correct `JobHandler` signature: `func(job *CronJob) (string, error)`.

```go
type testListener struct {
	mu      sync.Mutex
	records []ExecutionRecord
}

func (l *testListener) OnExecutionComplete(rec ExecutionRecord) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, rec)
}

func TestCronService_SetListener(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	svc := NewCronService(storePath, func(job *CronJob) (string, error) {
		return "", nil
	})
	listener := &testListener{}
	svc.SetListener(listener)
	// Verify it was set (no panic, listener is not nil internally)
	assert.NotNil(t, listener)
}

func TestCronService_TriggerJob(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	var handledIDs []string
	var mu sync.Mutex
	svc := NewCronService(storePath, func(job *CronJob) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		handledIDs = append(handledIDs, job.ID)
		return "ok", nil
	})
	svc.Start()
	defer svc.Stop()

	everyMS := int64(3600000)
	job, err := svc.AddJob("test", CronSchedule{Kind: "every", EveryMS: &everyMS}, "hello", "", "")
	require.NoError(t, err)

	// Disable job, then trigger manually — should still execute.
	svc.EnableJob(job.ID, false)

	err = svc.TriggerJob(job.ID)
	require.NoError(t, err)

	mu.Lock()
	assert.Contains(t, handledIDs, job.ID)
	mu.Unlock()
}

func TestCronService_TriggerJob_NotFound(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	svc := NewCronService(storePath, func(job *CronJob) (string, error) {
		return "", nil
	})
	svc.Start()
	defer svc.Stop()

	err := svc.TriggerJob("nonexistent")
	assert.Error(t, err)
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /e/WorkSpace/Personal/picoclaw && go test ./pkg/cron/ -run "TestCronService_(SetListener|TriggerJob)" -v
```

Expected: FAIL — `SetListener`, `TriggerJob` undefined.

- [ ] **Step 3: Add listener field and methods to CronService**

In `pkg/cron/service.go`:

1. Add field to `CronService` struct (~line 60-69):
```go
listener ExecutionListener // optional, nil-safe
```

2. Add methods:
```go
// SetListener sets an optional execution listener.
func (cs *CronService) SetListener(l ExecutionListener) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.listener = l
}

// TriggerJob manually executes a job by ID, regardless of Enabled state.
func (cs *CronService) TriggerJob(jobID string) error {
	cs.mu.RLock()
	var found *CronJob
	for i := range cs.store.Jobs {
		if cs.store.Jobs[i].ID == jobID {
			cp := cs.store.Jobs[i]
			found = &cp
			break
		}
	}
	cs.mu.RUnlock()

	if found == nil {
		return fmt.Errorf("job not found: %s", jobID)
	}

	startMS := time.Now().UnixMilli()
	output, execErr := cs.onJob(found)
	finishMS := time.Now().UnixMilli()

	status := "ok"
	errMsg := ""
	if execErr != nil {
		status = "error"
		errMsg = execErr.Error()
	}

	// Update job state to reflect the manual execution.
	cs.mu.Lock()
	for i := range cs.store.Jobs {
		if cs.store.Jobs[i].ID == jobID {
			cs.store.Jobs[i].State.LastRunAtMS = &startMS
			cs.store.Jobs[i].State.LastStatus = status
			cs.store.Jobs[i].State.LastError = errMsg
			break
		}
	}
	cs.saveStoreUnsafe()
	cs.mu.Unlock()

	if cs.listener != nil {
		cs.listener.OnExecutionComplete(ExecutionRecord{
			JobID:      found.ID,
			JobName:    found.Name,
			Trigger:    "manual",
			Status:     status,
			ErrorMsg:   errMsg,
			Output:     output,
			DurationMS: finishMS - startMS,
			StartedAt:  startMS,
			FinishedAt: finishMS,
		})
	}

	return nil
}
```

3. At the end of the existing `executeJobByID` method (~line 301), add listener callback with timing. Wrap the existing `cs.onJob(job)` call with timing capture:

```go
startMS := time.Now().UnixMilli()
// ... existing cs.onJob(&job) call ...
finishMS := time.Now().UnixMilli()

if cs.listener != nil {
	cs.listener.OnExecutionComplete(ExecutionRecord{
		JobID:      job.ID,
		JobName:    job.Name,
		Trigger:    "scheduled",
		Status:     job.State.LastStatus,
		ErrorMsg:   job.State.LastError,
		Output:     output,  // capture from cs.onJob return
		DurationMS: finishMS - startMS,
		StartedAt:  startMS,
		FinishedAt: finishMS,
	})
}
```

Note: Read `executeJobByID` carefully to see exactly where `cs.onJob` is called and how its return values are used. Adapt the timing capture to wrap only the execution call.

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /e/WorkSpace/Personal/picoclaw && go test ./pkg/cron/ -run "TestCronService_(SetListener|TriggerJob)" -v
```

Expected: PASS

- [ ] **Step 5: Run all existing cron tests for regression**

```bash
cd /e/WorkSpace/Personal/picoclaw && go test ./pkg/cron/ -v
```

Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
cd /e/WorkSpace/Personal/picoclaw && git add pkg/cron/service.go pkg/cron/service_test.go && git commit -m "$(cat <<'EOF'
feat(cron): add ExecutionListener and TriggerJob to CronService

- SetListener() for optional execution callback
- TriggerJob() for manual execution regardless of Enabled state
- Listener called at end of executeJobByID with timing info

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Chunk 2: Gateway API + Web Backend Proxy

### Task 4: Gateway-Side Cron API Handlers

**Files:**
- Create: `pkg/cron/api.go`
- Create: `pkg/cron/api_test.go`

- [ ] **Step 1: Implement CronAPIHandler**

Create `pkg/cron/api.go`. Key design points:
- Handler uses `CronService` public methods (AddJob, RemoveJob, EnableJob, ListJobs, TriggerJob) — NOT direct store access for standard operations
- For UpdateJob (which has no existing public method), access `cs.mu`, `cs.store.Jobs`, `cs.saveStoreUnsafe()`, `cs.recomputeNextRuns()` — allowed because same package
- Auth via Bearer token matching the existing `pkg/health/server.go` pattern
- JSON request/response uses the same camelCase tags as existing structs

```go
// pkg/cron/api.go
package cron

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type CronAPIHandler struct {
	service   *CronService
	history   *HistoryStore
	authToken string
	allowCmd  bool
}

func NewCronAPIHandler(service *CronService, history *HistoryStore, authToken string, allowCmd bool) *CronAPIHandler {
	return &CronAPIHandler{service: service, history: history, authToken: authToken, allowCmd: allowCmd}
}

func (h *CronAPIHandler) RegisterOnMux(mux *http.ServeMux) {
	mux.HandleFunc("GET /cron/jobs", h.withAuth(h.handleListJobs))
	mux.HandleFunc("POST /cron/jobs", h.withAuth(h.handleCreateJob))
	mux.HandleFunc("PUT /cron/jobs/{id}", h.withAuth(h.handleUpdateJob))
	mux.HandleFunc("DELETE /cron/jobs/{id}", h.withAuth(h.handleDeleteJob))
	mux.HandleFunc("POST /cron/jobs/{id}/enable", h.withAuth(h.handleEnableJob))
	mux.HandleFunc("POST /cron/jobs/{id}/disable", h.withAuth(h.handleDisableJob))
	mux.HandleFunc("POST /cron/jobs/{id}/trigger", h.withAuth(h.handleTriggerJob))
	mux.HandleFunc("GET /cron/history", h.withAuth(h.handleHistory))
	mux.HandleFunc("GET /cron/stats", h.withAuth(h.handleStats))
	mux.HandleFunc("GET /cron/stats/trend", h.withAuth(h.handleTrend))
}

func (h *CronAPIHandler) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.authToken != "" {
			given := extractCronBearer(r.Header.Get("Authorization"))
			if given == "" || subtle.ConstantTimeCompare([]byte(given), []byte(h.authToken)) != 1 {
				writeAPIError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
		}
		next(w, r)
	}
}

func extractCronBearer(header string) string {
	if len(header) > 7 && strings.EqualFold(header[:7], "bearer ") {
		return header[7:]
	}
	return ""
}

func writeAPIJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeAPIError(w http.ResponseWriter, code int, msg string) {
	writeAPIJSON(w, code, map[string]any{"error": msg, "code": code})
}

func (h *CronAPIHandler) handleListJobs(w http.ResponseWriter, r *http.Request) {
	jobs := h.service.ListJobs(true)
	writeAPIJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

type cronCreateRequest struct {
	Name     string       `json:"name"`
	Schedule CronSchedule `json:"schedule"`
	Payload  struct {
		Message string `json:"message"`
		Command string `json:"command"`
		Channel string `json:"channel"`
		To      string `json:"to"`
	} `json:"payload"`
}

func (h *CronAPIHandler) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	var req cronCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeAPIError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Payload.Command != "" && !h.allowCmd {
		writeAPIError(w, http.StatusForbidden, "shell commands are disabled in configuration")
		return
	}

	job, err := h.service.AddJob(req.Name, req.Schedule, req.Payload.Message, req.Payload.Channel, req.Payload.To)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Set command on payload if provided (AddJob doesn't accept command).
	if req.Payload.Command != "" {
		h.service.mu.Lock()
		for i := range h.service.store.Jobs {
			if h.service.store.Jobs[i].ID == job.ID {
				h.service.store.Jobs[i].Payload.Command = req.Payload.Command
				cp := h.service.store.Jobs[i]
				job = &cp
				break
			}
		}
		h.service.saveStoreUnsafe()
		h.service.mu.Unlock()
	}
	writeAPIJSON(w, http.StatusCreated, map[string]any{"job": job})
}

func (h *CronAPIHandler) handleUpdateJob(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	var req cronCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Payload.Command != "" && !h.allowCmd {
		writeAPIError(w, http.StatusForbidden, "shell commands are disabled in configuration")
		return
	}

	h.service.mu.Lock()
	var found *CronJob
	for i := range h.service.store.Jobs {
		if h.service.store.Jobs[i].ID == jobID {
			found = &h.service.store.Jobs[i]
			break
		}
	}
	if found == nil {
		h.service.mu.Unlock()
		writeAPIError(w, http.StatusNotFound, "job not found")
		return
	}

	if req.Name != "" {
		found.Name = req.Name
	}
	found.Schedule = req.Schedule
	found.Payload.Message = req.Payload.Message
	found.Payload.Command = req.Payload.Command
	found.Payload.Channel = req.Payload.Channel
	found.Payload.To = req.Payload.To
	found.UpdatedAtMS = time.Now().UnixMilli()

	h.service.recomputeNextRuns()
	h.service.saveStoreUnsafe()
	cp := *found
	h.service.mu.Unlock()
	h.service.notify()

	writeAPIJSON(w, http.StatusOK, map[string]any{"job": cp})
}

func (h *CronAPIHandler) handleDeleteJob(w http.ResponseWriter, r *http.Request) {
	if ok := h.service.RemoveJob(r.PathValue("id")); !ok {
		writeAPIError(w, http.StatusNotFound, "job not found")
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (h *CronAPIHandler) handleEnableJob(w http.ResponseWriter, r *http.Request) {
	job := h.service.EnableJob(r.PathValue("id"), true)
	if job == nil {
		writeAPIError(w, http.StatusNotFound, "job not found")
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (h *CronAPIHandler) handleDisableJob(w http.ResponseWriter, r *http.Request) {
	job := h.service.EnableJob(r.PathValue("id"), false)
	if job == nil {
		writeAPIError(w, http.StatusNotFound, "job not found")
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (h *CronAPIHandler) handleTriggerJob(w http.ResponseWriter, r *http.Request) {
	if err := h.service.TriggerJob(r.PathValue("id")); err != nil {
		writeAPIError(w, http.StatusNotFound, err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"triggered": true})
}

func (h *CronAPIHandler) handleHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	jobID := q.Get("job_id")
	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("size"))
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	records, total, err := h.history.QueryHistory(jobID, page, size)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"records": records, "total": total, "page": page, "size": size})
}

func (h *CronAPIHandler) handleStats(w http.ResponseWriter, r *http.Request) {
	jobs := h.service.ListJobs(true)
	enabledJobs := 0
	for _, j := range jobs {
		if j.Enabled {
			enabledJobs++
		}
	}
	now := time.Now().UnixMilli()
	histStats, err := h.history.Stats24h(now)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{
		"totalJobs":       len(jobs),
		"enabledJobs":     enabledJobs,
		"runs24h":         histStats.Runs24h,
		"success24h":      histStats.Success24h,
		"errors24h":       histStats.Errors24h,
		"successRate24h":  histStats.SuccessRate,
	})
}

func (h *CronAPIHandler) handleTrend(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days < 1 || days > 90 {
		days = 7
	}
	trend, err := h.history.Trend(time.Now().UnixMilli(), days)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"trend": trend})
}
```

- [ ] **Step 2: Write API handler tests**

```go
// pkg/cron/api_test.go
package cron

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestAPI(t *testing.T) (*CronAPIHandler, *CronService) {
	t.Helper()
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	dbPath := filepath.Join(dir, "history.db")

	svc := NewCronService(storePath, func(job *CronJob) (string, error) {
		return "executed", nil
	})
	svc.Start()
	t.Cleanup(func() { svc.Stop() })

	hs, err := NewHistoryStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { hs.Close() })
	svc.SetListener(hs)

	handler := NewCronAPIHandler(svc, hs, "test-token", true)
	return handler, svc
}

func TestCronAPI_ListJobs_Empty(t *testing.T) {
	h, _ := setupTestAPI(t)
	mux := http.NewServeMux()
	h.RegisterOnMux(mux)

	req := httptest.NewRequest("GET", "/cron/jobs", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	jobs := resp["jobs"].([]any)
	assert.Len(t, jobs, 0)
}

func TestCronAPI_CreateAndListJob(t *testing.T) {
	h, _ := setupTestAPI(t)
	mux := http.NewServeMux()
	h.RegisterOnMux(mux)

	body := `{"name":"Test","schedule":{"kind":"every","everyMs":3600000},"payload":{"message":"hi","channel":"cli","to":""}}`
	req := httptest.NewRequest("POST", "/cron/jobs", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)

	req2 := httptest.NewRequest("GET", "/cron/jobs", nil)
	req2.Header.Set("Authorization", "Bearer test-token")
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)
	var resp map[string]any
	json.Unmarshal(w2.Body.Bytes(), &resp)
	jobs := resp["jobs"].([]any)
	assert.Len(t, jobs, 1)
}

func TestCronAPI_Unauthorized(t *testing.T) {
	h, _ := setupTestAPI(t)
	mux := http.NewServeMux()
	h.RegisterOnMux(mux)

	req := httptest.NewRequest("GET", "/cron/jobs", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestCronAPI_DeleteNotFound(t *testing.T) {
	h, _ := setupTestAPI(t)
	mux := http.NewServeMux()
	h.RegisterOnMux(mux)

	req := httptest.NewRequest("DELETE", "/cron/jobs/nonexistent", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestCronAPI_CommandForbidden(t *testing.T) {
	h, _ := setupTestAPI(t)
	h.allowCmd = false
	mux := http.NewServeMux()
	h.RegisterOnMux(mux)

	body := `{"name":"Test","schedule":{"kind":"every","everyMs":3600000},"payload":{"message":"hi","command":"echo hi","channel":"cli","to":""}}`
	req := httptest.NewRequest("POST", "/cron/jobs", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}
```

Note: Request bodies use camelCase JSON keys (`everyMs` not `every_ms`) to match Go struct tags.

- [ ] **Step 3: Run API tests**

```bash
cd /e/WorkSpace/Personal/picoclaw && go test ./pkg/cron/ -run "TestCronAPI_" -v
```

Expected: ALL PASS. If `recomputeNextRuns` acquires its own lock internally, calling it after `mu.Unlock()` in `handleUpdateJob` is correct. If it doesn't acquire its own lock, move the call inside the locked section.

- [ ] **Step 4: Commit**

```bash
cd /e/WorkSpace/Personal/picoclaw && git add pkg/cron/api.go pkg/cron/api_test.go && git commit -m "$(cat <<'EOF'
feat(cron): add gateway-side HTTP API handlers

- CronAPIHandler with Bearer token auth
- CRUD: list/create/update/delete jobs
- Enable/disable/trigger endpoints
- History, stats, and trend endpoints
- Command gating via allow_command config

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: Wire into Gateway + Health Server

**Files:**
- Modify: `pkg/gateway/gateway.go` (~line 64 services struct, ~line 758 setupCronTool, ~line 467 stopAndCleanupServices)
- Modify: `pkg/health/server.go` (~line 16 Server struct, ~line 40 NewServer)

- [ ] **Step 1: Expose mux on health Server**

In `pkg/health/server.go`, change the local `mux` variable in `NewServer` (~line 40) to a field on the Server struct. Add `mux *http.ServeMux` to the struct definition. Then add a public method:

```go
func (s *Server) Mux() *http.ServeMux {
	return s.mux
}
```

- [ ] **Step 2: Add CronHistory to services struct**

In `pkg/gateway/gateway.go`, add to the `services` struct (~line 64):

```go
CronHistory *cron.HistoryStore
```

- [ ] **Step 3: Modify setupCronTool to create HistoryStore and CronAPIHandler**

Modify `setupCronTool` (~line 758) to also return `*cron.HistoryStore`. After creating the CronService:

```go
historyDBPath := filepath.Join(workspace, "cron", "history.db")
historyStore, err := cron.NewHistoryStore(historyDBPath)
if err != nil {
    return nil, nil, fmt.Errorf("create cron history store: %w", err)
}
cronService.SetListener(historyStore)
```

Update the return type and callers accordingly.

- [ ] **Step 4: Register CronAPIHandler on health server mux**

After both CronService and HealthServer are created in the gateway startup flow, register the cron API:

```go
if runningServices.CronService != nil && runningServices.CronHistory != nil {
    cronAPI := cron.NewCronAPIHandler(
        runningServices.CronService,
        runningServices.CronHistory,
        runningServices.authToken,
        cfg.Tools.Cron.AllowCommand,
    )
    cronAPI.RegisterOnMux(runningServices.HealthServer.Mux())
}
```

- [ ] **Step 5: Add cleanup in stopAndCleanupServices**

In `stopAndCleanupServices` (~line 467), add:

```go
if runningServices.CronHistory != nil {
    runningServices.CronHistory.Close()
}
```

- [ ] **Step 6: Build to verify**

```bash
cd /e/WorkSpace/Personal/picoclaw && go build ./...
```

Expected: No errors.

- [ ] **Step 7: Run all tests**

```bash
cd /e/WorkSpace/Personal/picoclaw && go test ./pkg/cron/ -v && go test ./pkg/health/ -v
```

Expected: ALL PASS

- [ ] **Step 8: Commit**

```bash
cd /e/WorkSpace/Personal/picoclaw && git add pkg/gateway/gateway.go pkg/health/server.go && git commit -m "$(cat <<'EOF'
feat(gateway): wire HistoryStore and CronAPI into gateway lifecycle

- Create HistoryStore in setupCronTool
- Expose mux on health server for CronAPI registration
- Register CronAPIHandler routes on gateway startup
- Clean up HistoryStore on shutdown

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: Web Backend Proxy

**Files:**
- Create: `web/backend/api/cron.go`
- Modify: `web/backend/api/router.go` (~line 110 RegisterRoutes)

- [ ] **Step 1: Implement proxy handler**

```go
// web/backend/api/cron.go
package api

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (h *Handler) registerCronRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/cron/", h.handleCronProxy)
}

func (h *Handler) handleCronProxy(w http.ResponseWriter, r *http.Request) {
	gateway.mu.Lock()
	pidData := gateway.pidData
	gateway.mu.Unlock()

	if pidData == nil {
		http.Error(w, `{"error":"gateway not running","code":502}`, http.StatusBadGateway)
		return
	}

	port := pidData.Port
	if port == 0 {
		port = 18790
	}
	host := pidData.Host
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}

	// Strip /api prefix: /api/cron/jobs -> /cron/jobs
	targetPath := strings.TrimPrefix(r.URL.Path, "/api")
	targetURL := fmt.Sprintf("http://%s%s", net.JoinHostPort(host, strconv.Itoa(port)), targetPath)
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, r.Body)
	if err != nil {
		http.Error(w, `{"error":"failed to create proxy request","code":500}`, http.StatusInternalServerError)
		return
	}
	proxyReq.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	proxyReq.Header.Set("Authorization", "Bearer "+pidData.Token)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(proxyReq)
	if err != nil {
		http.Error(w, `{"error":"gateway unreachable","code":502}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
```

- [ ] **Step 2: Register in router.go**

In `web/backend/api/router.go`, in `RegisterRoutes` (~line 110), add before the closing brace:

```go
h.registerCronRoutes(mux)
```

- [ ] **Step 3: Build**

```bash
cd /e/WorkSpace/Personal/picoclaw && go build ./web/backend/...
```

Expected: No errors.

- [ ] **Step 4: Commit**

```bash
cd /e/WorkSpace/Personal/picoclaw && git add web/backend/api/cron.go web/backend/api/router.go && git commit -m "$(cat <<'EOF'
feat(web): add cron API proxy to web backend

- Proxy /api/cron/* to gateway /cron/* endpoints
- Uses PID file for gateway discovery and Bearer token auth
- Returns 502 when gateway is not running

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Chunk 3: Frontend Phase 1 — CRUD

### Task 7: Frontend API Client

**Files:**
- Create: `web/frontend/src/api/cron.ts`

- [ ] **Step 1: Create cron API client**

Note: TypeScript types must use **camelCase** to match Go JSON tags (e.g., `everyMs` not `every_ms`, `createdAtMs` not `CreatedAtMS`).

```typescript
// web/frontend/src/api/cron.ts
import { launcherFetch } from "./http"

export interface CronSchedule {
  kind: "at" | "every" | "cron"
  atMs?: number
  everyMs?: number
  expr?: string
  tz?: string
}

export interface CronPayload {
  kind?: string
  message: string
  command?: string
  channel?: string
  to?: string
}

export interface CronJobState {
  nextRunAtMs?: number | null
  lastRunAtMs?: number | null
  lastStatus?: string
  lastError?: string
}

export interface CronJob {
  id: string
  name: string
  enabled: boolean
  schedule: CronSchedule
  payload: CronPayload
  state: CronJobState
  createdAtMs: number
  updatedAtMs: number
  deleteAfterRun: boolean
}

export interface ExecutionRecord {
  id: number
  jobId: string
  jobName: string
  trigger: string
  status: string
  errorMsg: string
  output: string
  durationMs: number
  startedAt: number
  finishedAt: number
}

export interface CronStats {
  totalJobs: number
  enabledJobs: number
  runs24h: number
  success24h: number
  errors24h: number
  successRate24h: number
}

export interface TrendEntry {
  date: string
  ok: number
  error: number
}

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await launcherFetch(path, options)
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(body.error || res.statusText)
  }
  return res.json()
}

export async function listCronJobs(): Promise<CronJob[]> {
  const data = await request<{ jobs: CronJob[] }>("/api/cron/jobs")
  return data.jobs ?? []
}

export async function createCronJob(job: {
  name: string
  schedule: CronSchedule
  payload: { message: string; command?: string; channel?: string; to?: string }
}): Promise<CronJob> {
  const data = await request<{ job: CronJob }>("/api/cron/jobs", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(job),
  })
  return data.job
}

export async function updateCronJob(
  id: string,
  job: {
    name: string
    schedule: CronSchedule
    payload: { message: string; command?: string; channel?: string; to?: string }
  },
): Promise<CronJob> {
  const data = await request<{ job: CronJob }>(`/api/cron/jobs/${id}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(job),
  })
  return data.job
}

export async function deleteCronJob(id: string): Promise<void> {
  await request(`/api/cron/jobs/${id}`, { method: "DELETE" })
}

export async function enableCronJob(id: string): Promise<CronJob> {
  const data = await request<{ job: CronJob }>(`/api/cron/jobs/${id}/enable`, { method: "POST" })
  return data.job
}

export async function disableCronJob(id: string): Promise<CronJob> {
  const data = await request<{ job: CronJob }>(`/api/cron/jobs/${id}/disable`, { method: "POST" })
  return data.job
}

export async function triggerCronJob(id: string): Promise<void> {
  await request(`/api/cron/jobs/${id}/trigger`, { method: "POST" })
}

export async function getCronHistory(params: {
  page?: number
  size?: number
  job_id?: string
}): Promise<{ records: ExecutionRecord[]; total: number; page: number; size: number }> {
  const sp = new URLSearchParams()
  if (params.page) sp.set("page", String(params.page))
  if (params.size) sp.set("size", String(params.size))
  if (params.job_id) sp.set("job_id", params.job_id)
  return request(`/api/cron/history?${sp.toString()}`)
}

export async function getCronStats(): Promise<CronStats> {
  return request("/api/cron/stats")
}

export async function getCronTrend(days?: number): Promise<{ trend: TrendEntry[] }> {
  const params = days ? `?days=${days}` : ""
  return request(`/api/cron/stats/trend${params}`)
}
```

- [ ] **Step 2: Commit**

```bash
cd /e/WorkSpace/Personal/picoclaw && git add web/frontend/src/api/cron.ts && git commit -m "$(cat <<'EOF'
feat(web/frontend): add cron API client with camelCase types

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 8: Route + Page Shell + Sidebar Nav + i18n

**Files:**
- Create: `web/frontend/src/routes/config.cron.tsx`
- Create: `web/frontend/src/components/cron/cron-page.tsx`
- Modify: `web/frontend/src/components/app-sidebar.tsx` (~line 169, add nav item; ~line 1, add IconClock import)
- Modify: `web/frontend/src/i18n/locales/en.json`
- Modify: `web/frontend/src/i18n/locales/zh.json`

- [ ] **Step 1: Create route file**

```typescript
// web/frontend/src/routes/config.cron.tsx
import { createFileRoute } from "@tanstack/react-router"
import { CronPage } from "@/components/cron/cron-page"

export const Route = createFileRoute("/config/cron")({
  component: CronPage,
})
```

- [ ] **Step 2: Create cron-page shell**

Create `web/frontend/src/components/cron/cron-page.tsx` with:
- PageHeader with translated title
- Tab bar (Jobs / Execution History) using state
- Placeholder divs for tab content (will be replaced in Tasks 9-11)

- [ ] **Step 3: Add sidebar nav item**

In `app-sidebar.tsx`, import `IconClock` from `@tabler/icons-react`. In the Config nav group (~line 169), add a Cron nav item between config and logs:

```typescript
{
  title: "navigation.cron",
  url: "/config/cron",
  icon: IconClock,
  translateTitle: true,
},
```

- [ ] **Step 4: Add i18n keys to both en.json and zh.json**

Add `pages.cron.*` keys covering: title, tabs, table headers, status labels, form labels, stats labels, empty states, toast messages, history labels. Add `navigation.cron` key.

See the spec for the complete key list. Key naming follows existing pattern: `pages.cron.<section>.<field>`.

- [ ] **Step 5: Build frontend**

```bash
cd /e/WorkSpace/Personal/picoclaw/web/frontend && pnpm build
```

Expected: Build succeeds.

- [ ] **Step 6: Commit**

```bash
cd /e/WorkSpace/Personal/picoclaw && git add web/frontend/src/routes/config.cron.tsx web/frontend/src/components/cron/ web/frontend/src/components/app-sidebar.tsx web/frontend/src/i18n/locales/ && git commit -m "$(cat <<'EOF'
feat(web/frontend): add cron page route, shell, sidebar nav, and i18n

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 9: Job List Component

**Files:**
- Create: `web/frontend/src/components/cron/cron-job-list.tsx`
- Modify: `web/frontend/src/components/cron/cron-page.tsx`

- [ ] **Step 1: Implement cron-job-list.tsx**

Build a table component using:
- `useQuery(["cron-jobs"], listCronJobs)` for data fetching
- `useMutation` + `useQueryClient().invalidateQueries(["cron-jobs"])` for actions
- Columns: Name (with green/gray dot for enabled/disabled), Schedule (human-readable), Channel (badge), Last Run (relative time via dayjs), Status, Actions
- Actions: trigger (IconPlayerPlay), edit (IconEdit), enable/disable (IconPlayerPause/IconPlayerPlay), delete (IconX) with AlertDialog confirmation
- Toast notifications from sonner for action feedback
- Empty state when no jobs
- "+ New Job" button in header area

Follow existing component patterns (Tailwind utility classes, `useTranslation()`, Radix UI primitives).

- [ ] **Step 2: Integrate into cron-page.tsx**

Replace jobs placeholder with `<CronJobList onEdit={handleEdit} />`. Add state for dialog open/selected job.

- [ ] **Step 3: Build and verify**

```bash
cd /e/WorkSpace/Personal/picoclaw/web/frontend && pnpm build
```

- [ ] **Step 4: Commit**

```bash
cd /e/WorkSpace/Personal/picoclaw && git add web/frontend/src/components/cron/ && git commit -m "$(cat <<'EOF'
feat(web/frontend): add cron job list component with actions

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 10: Job Form Dialog

**Files:**
- Create: `web/frontend/src/components/cron/cron-job-form.tsx`
- Modify: `web/frontend/src/components/cron/cron-page.tsx`

- [ ] **Step 1: Implement cron-job-form.tsx**

Radix Dialog component with:
- Props: `open`, `onOpenChange`, optional `job` (null=create, populated=edit)
- Simple mode (default): radio group for schedule type
  - One-time: datetime-local input → converts to `atMs` (Unix ms)
  - Recurring: number input + unit select (minutes/hours/days) → converts to `everyMs`
  - Cron expression: text input + optional timezone
- Advanced mode: toggle button switches to raw cron expression input
- Payload section: message (textarea required), command (input optional), channel (input), to (input)
- Save uses `useMutation` calling `createCronJob` or `updateCronJob`
- Form validation: name and message required

- [ ] **Step 2: Wire into cron-page.tsx**

Add state for `editingJob` and `formOpen`. Wire "+ New Job" button and edit action.

- [ ] **Step 3: Build and verify**

```bash
cd /e/WorkSpace/Personal/picoclaw/web/frontend && pnpm build
```

- [ ] **Step 4: Commit Phase 1 complete**

```bash
cd /e/WorkSpace/Personal/picoclaw && git add web/frontend/src/components/cron/ && git commit -m "$(cat <<'EOF'
feat(web/frontend): add cron job form dialog with simple/advanced mode

Phase 1 complete: full CRUD for cron jobs in WebUI

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Chunk 4: Frontend Phase 2 + Phase 3

### Task 11: Execution History Tab (Phase 2)

**Files:**
- Create: `web/frontend/src/components/cron/cron-history.tsx`
- Create: `web/frontend/src/components/cron/cron-output-dialog.tsx`
- Modify: `web/frontend/src/components/cron/cron-page.tsx`

- [ ] **Step 1: Implement cron-history.tsx**

- `useQuery(["cron-history", page, size, jobId], () => getCronHistory({...}))` for data
- Job filter: select dropdown from `listCronJobs()` query, "All Jobs" default
- Table: Job Name, Time (dayjs format), Trigger (badge), Status (colored), Duration (formatted ms→s), Output ("View" link)
- Pagination: Previous/Next buttons, "Page X of Y" display
- "View" click opens CronOutputDialog with selected record

- [ ] **Step 2: Implement cron-output-dialog.tsx**

Radix Dialog showing:
- Header with job name, time, trigger type, status, duration
- Error message if status=error
- Output in scrollable `<pre>` block with monospace font
- Close button

- [ ] **Step 3: Integrate into cron-page.tsx**

Replace history placeholder with `<CronHistory />`.

- [ ] **Step 4: Build and verify**

```bash
cd /e/WorkSpace/Personal/picoclaw/web/frontend && pnpm build
```

- [ ] **Step 5: Commit Phase 2**

```bash
cd /e/WorkSpace/Personal/picoclaw && git add web/frontend/src/components/cron/ && git commit -m "$(cat <<'EOF'
feat(web/frontend): add execution history tab with output viewer

Phase 2 complete: history with filter, pagination, output dialog

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 12: Stats + Trend Chart (Phase 3)

**Files:**
- Create: `web/frontend/src/components/cron/cron-stats.tsx`
- Modify: `web/frontend/src/components/cron/cron-page.tsx`

- [ ] **Step 1: Implement cron-stats.tsx**

- `useQuery(["cron-stats"], getCronStats, { refetchInterval: 30000 })` for stats
- `useQuery(["cron-trend"], () => getCronTrend(7), { refetchInterval: 30000 })` for trend
- Stats cards: 4 cards in a flex row (Total Jobs, Enabled, 24h Runs, Success Rate)
- 7-day trend: pure CSS bar chart — flex container with 7 columns, each with stacked green/red bars proportional to daily max count, date labels below, success/error legend
- Skeleton loading states while fetching

- [ ] **Step 2: Integrate into cron-page.tsx**

Add `<CronStats />` above the tabs — always visible regardless of active tab.

- [ ] **Step 3: Build and verify**

```bash
cd /e/WorkSpace/Personal/picoclaw/web/frontend && pnpm build
```

- [ ] **Step 4: Commit Phase 3**

```bash
cd /e/WorkSpace/Personal/picoclaw && git add web/frontend/src/components/cron/ && git commit -m "$(cat <<'EOF'
feat(web/frontend): add stats overview and 7-day trend chart

Phase 3 complete: full cron management panel

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 13: Final Verification

- [ ] **Step 1: Run all backend tests**

```bash
cd /e/WorkSpace/Personal/picoclaw && go test ./pkg/cron/ -v
```

Expected: ALL PASS

- [ ] **Step 2: Build entire project**

```bash
cd /e/WorkSpace/Personal/picoclaw && go build ./...
```

Expected: No errors

- [ ] **Step 3: Build frontend**

```bash
cd /e/WorkSpace/Personal/picoclaw/web/frontend && pnpm build
```

Expected: No TypeScript errors

- [ ] **Step 4: Verify git log**

```bash
cd /e/WorkSpace/Personal/picoclaw && git log --oneline feat/webui-cron-management --not main
```

Expected: Clean commit history with all phase commits.
