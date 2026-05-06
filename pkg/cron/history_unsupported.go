//go:build mipsle || netbsd || (freebsd && arm)

package cron

// noopHistoryStore discards all writes and returns empty results. It is used
// on platforms where modernc.org/sqlite (and its modernc.org/libc dependency)
// currently has no stable build path: linux/mipsle, netbsd, freebsd/arm.
//
// Cron jobs continue to run normally on these platforms; only the execution
// history view (and the dashboards built on top of it) is unavailable.
type noopHistoryStore struct{}

// NewHistoryStore returns a no-op HistoryStore on platforms that lack
// modernc.org/sqlite support. dbPath is accepted for API parity but ignored.
func NewHistoryStore(_ string) (HistoryStore, error) {
	return &noopHistoryStore{}, nil
}

func (noopHistoryStore) Close() error { return nil }

func (noopHistoryStore) WriteRecord(_ ExecutionRecord) error { return nil }

func (noopHistoryStore) OnExecutionComplete(_ ExecutionRecord) {}

func (noopHistoryStore) QueryHistory(_ string, _, _ int) ([]ExecutionRecord, int, error) {
	return []ExecutionRecord{}, 0, nil
}

func (noopHistoryStore) Stats24h(_ int64) (HistoryStats, error) {
	return HistoryStats{}, nil
}

func (noopHistoryStore) Trend(_ int64, _ int) ([]TrendEntry, error) {
	return []TrendEntry{}, nil
}
