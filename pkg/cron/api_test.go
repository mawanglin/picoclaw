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
