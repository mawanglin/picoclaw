package cron

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

// HistoryStore persists cron job execution history.
//
// The default implementation uses SQLite (via modernc.org/sqlite). On
// platforms where modernc.org/libc has no stable build path
// (linux/mipsle, netbsd, freebsd/arm), NewHistoryStore returns a no-op
// store that discards writes and returns empty results, so cron jobs
// themselves still run — only the history view is unavailable.
type HistoryStore interface {
	ExecutionListener
	Close() error
	WriteRecord(rec ExecutionRecord) error
	QueryHistory(jobID string, page, size int) ([]ExecutionRecord, int, error)
	Stats24h(nowMS int64) (HistoryStats, error)
	Trend(nowMS int64, days int) ([]TrendEntry, error)
}
