package cron

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestSaveStore_FilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission bits are not enforced on Windows")
	}

	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "cron", "jobs.json")

	cs := NewCronService(storePath, nil)

	_, err := cs.AddJob("test", CronSchedule{Kind: "every", EveryMS: int64Ptr(60000)}, "hello", "cli", "direct")
	if err != nil {
		t.Fatalf("AddJob failed: %v", err)
	}

	info, err := os.Stat(storePath)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0o600 {
		t.Errorf("cron store has permission %04o, want 0600", perm)
	}
}

func int64Ptr(v int64) *int64 {
	return &v
}

func setupService(handler JobHandler) (*CronService, string) {
	tmpFile := fmt.Sprintf("test_cron_%d.json", time.Now().UnixNano())
	cs := NewCronService(tmpFile, handler)
	return cs, tmpFile
}

func TestCronService_CRUD(t *testing.T) {
	cs, path := setupService(nil)
	defer os.Remove(path)

	// Test AddJob
	at := time.Now().Add(time.Hour).UnixMilli()
	job, err := cs.AddJob("Task1", CronSchedule{Kind: "at", AtMS: &at}, "msg", "ch", "to")
	if err != nil || job.ID == "" {
		t.Fatalf("AddJob failed: %v", err)
	}

	// Test ListJobs
	if len(cs.ListJobs(true)) != 1 {
		t.Error("ListJobs should return 1 job")
	}

	// Test UpdateJob
	job.Name = "UpdatedName"
	err = cs.UpdateJob(job)
	if err != nil || cs.store.Jobs[0].Name != "UpdatedName" {
		t.Error("UpdateJob failed")
	}

	// Test EnableJob
	cs.EnableJob(job.ID, false)
	if cs.store.Jobs[0].Enabled != false || cs.store.Jobs[0].State.NextRunAtMS != nil {
		t.Error("EnableJob(false) failed to clear state")
	}

	// Test RemoveJob
	removed := cs.RemoveJob(job.ID)
	if !removed || len(cs.store.Jobs) != 0 {
		t.Error("RemoveJob failed")
	}
}

// 2. Test Cron Expression Calculation Logic
func TestCronService_ComputeNextRun(t *testing.T) {
	cs, path := setupService(nil)
	defer os.Remove(path)

	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC).UnixMilli()

	tests := []struct {
		name     string
		schedule CronSchedule
		wantNil  bool
	}{
		{"Valid Cron", CronSchedule{Kind: "cron", Expr: "0 * * * *"}, false},
		{"Invalid Cron", CronSchedule{Kind: "cron", Expr: "invalid"}, true},
		{"Every MS", CronSchedule{Kind: "every", EveryMS: int64Ptr(5000)}, false},
		{"At Future", CronSchedule{Kind: "at", AtMS: int64Ptr(now + 1000)}, false},
		{"At Past", CronSchedule{Kind: "at", AtMS: int64Ptr(now - 1000)}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cs.computeNextRun(&tt.schedule, now)
			if (got == nil) != tt.wantNil {
				t.Errorf("%s: got %v, wantNil %v", tt.name, got, tt.wantNil)
			}
		})
	}
}

// 3. Test Execution Flow
func TestCronService_ExecutionFlow(t *testing.T) {
	var mu sync.Mutex
	executedJobs := make(map[string]bool)

	handler := func(job *CronJob) (string, error) {
		mu.Lock()
		executedJobs[job.ID] = true
		mu.Unlock()
		return "ok", nil
	}

	cs, path := setupService(handler)
	defer os.Remove(path)

	// Start the service
	if err := cs.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer cs.Stop()

	// Add a job then runs 100ms from now
	target := time.Now().Add(100 * time.Millisecond).UnixMilli()
	job, _ := cs.AddJob("FastJob", CronSchedule{Kind: "at", AtMS: &target}, "", "", "")

	// Check for job execution with a timeout
	success := false
	for range 20 {
		mu.Lock()
		if executedJobs[job.ID] {
			success = true
			mu.Unlock()
			break
		}
		mu.Unlock()
		time.Sleep(100 * time.Millisecond)
	}

	if !success {
		t.Error("Job was not executed in time")
	}

	// check that the job is removed after execution (DeleteAfterRun = true)
	status := cs.Status()
	if status["jobs"].(int) != 0 {
		t.Errorf("Job should be deleted after run, got count: %v", status["jobs"])
	}
}

func TestCronService_PersistenceIntegrity(t *testing.T) {
	tmpFile := "persist_test.json"
	defer os.Remove(tmpFile)

	// write a job and persist
	cs1 := NewCronService(tmpFile, nil)
	at := int64(2000000000000)
	cs1.AddJob("PersistMe", CronSchedule{Kind: "at", AtMS: &at}, "payload", "ch1", "")

	// check file exists
	if _, err := os.Stat(tmpFile); os.IsNotExist(err) {
		t.Fatal("Store file was not created")
	}

	// reload and check data integrity
	cs2 := NewCronService(tmpFile, nil)
	if err := cs2.Load(); err != nil {
		t.Fatalf("Failed to load store: %v", err)
	}

	jobs := cs2.ListJobs(true)
	if len(jobs) != 1 || jobs[0].Name != "PersistMe" {
		t.Errorf("Data corruption after reload. Got: %+v", jobs)
	}

	// test loading invalid JSON
	os.WriteFile(tmpFile, []byte("{invalid json}"), 0o644)
	cs3 := NewCronService(tmpFile, nil)
	err := cs3.loadStore()
	if err == nil {
		t.Error("Should return error when loading invalid JSON")
	}
}

func TestCronService_ConcurrentAccess(t *testing.T) {
	cs, path := setupService(nil)
	defer os.Remove(path)

	cs.Start()
	defer cs.Stop()

	var wg sync.WaitGroup
	workers := 10
	iterations := 50

	wg.Add(workers * 2)

	// add jobs concurrently
	for i := range workers {
		go func(id int) {
			defer wg.Done()
			for j := range iterations {
				at := time.Now().Add(time.Hour).UnixMilli()
				cs.AddJob(fmt.Sprintf("Job-%d-%d", id, j), CronSchedule{Kind: "at", AtMS: &at}, "", "", "")
				time.Sleep(100 * time.Microsecond)
			}
		}(i)
	}

	// read and update jobs concurrently
	for range workers {
		go func() {
			defer wg.Done()
			for j := range iterations {
				jobs := cs.ListJobs(true)
				if len(jobs) > 0 {
					cs.EnableJob(jobs[0].ID, j%2 == 0)
				}
				time.Sleep(100 * time.Microsecond)
			}
		}()
	}

	wg.Wait()
}

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
	if listener == nil {
		t.Fatal("listener should not be nil")
	}
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
	if err != nil {
		t.Fatalf("AddJob failed: %v", err)
	}

	// Disable job, then trigger manually — should still execute.
	svc.EnableJob(job.ID, false)

	err = svc.TriggerJob(job.ID)
	if err != nil {
		t.Fatalf("TriggerJob failed: %v", err)
	}

	mu.Lock()
	found := false
	for _, id := range handledIDs {
		if id == job.ID {
			found = true
			break
		}
	}
	mu.Unlock()

	if !found {
		t.Error("TriggerJob did not execute the job handler")
	}
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
	if err == nil {
		t.Error("TriggerJob should return error for nonexistent job")
	}
}

func TestCronService_ListenerCalledOnTrigger(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	svc := NewCronService(storePath, func(job *CronJob) (string, error) {
		return "output-data", nil
	})
	listener := &testListener{}
	svc.SetListener(listener)
	svc.Start()
	defer svc.Stop()

	everyMS := int64(3600000)
	job, err := svc.AddJob("listener-test", CronSchedule{Kind: "every", EveryMS: &everyMS}, "hello", "", "")
	if err != nil {
		t.Fatalf("AddJob failed: %v", err)
	}

	err = svc.TriggerJob(job.ID)
	if err != nil {
		t.Fatalf("TriggerJob failed: %v", err)
	}

	listener.mu.Lock()
	defer listener.mu.Unlock()
	if len(listener.records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(listener.records))
	}
	rec := listener.records[0]
	if rec.Trigger != "manual" {
		t.Errorf("expected trigger 'manual', got '%s'", rec.Trigger)
	}
	if rec.Status != "ok" {
		t.Errorf("expected status 'ok', got '%s'", rec.Status)
	}
	if rec.Output != "output-data" {
		t.Errorf("expected output 'output-data', got '%s'", rec.Output)
	}
}
