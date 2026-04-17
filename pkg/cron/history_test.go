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
