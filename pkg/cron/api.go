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

func (h *CronAPIHandler) handleListJobs(w http.ResponseWriter, _ *http.Request) {
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

func (h *CronAPIHandler) handleStats(w http.ResponseWriter, _ *http.Request) {
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
		"totalJobs":      len(jobs),
		"enabledJobs":    enabledJobs,
		"runs24h":        histStats.Runs24h,
		"success24h":     histStats.Success24h,
		"errors24h":      histStats.Errors24h,
		"successRate24h": histStats.SuccessRate,
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
