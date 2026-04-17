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

type ExecutionListener interface {
	OnExecutionComplete(record ExecutionRecord)
}

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
	hs.db.Exec(`DELETE FROM cron_executions WHERE id IN (
		SELECT id FROM cron_executions ORDER BY started_at ASC LIMIT MAX(0, (SELECT COUNT(*) FROM cron_executions) - ?))`,
		maxHistoryRecords,
	)
	return nil
}

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

func timeFromMS(ms int64) time.Time {
	return time.Unix(ms/1000, (ms%1000)*int64(time.Millisecond))
}

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
