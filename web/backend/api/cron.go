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

// registerCronRoutes binds the cron API reverse-proxy to the ServeMux.
func (h *Handler) registerCronRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/cron/", h.handleCronProxy)
}

// handleCronProxy forwards /api/cron/* requests to the gateway's /cron/* endpoints,
// injecting the Bearer token from the PID file for authentication.
func (h *Handler) handleCronProxy(w http.ResponseWriter, r *http.Request) {
	gateway.mu.Lock()
	pidData := gateway.pidData
	gateway.mu.Unlock()

	if pidData == nil {
		w.Header().Set("Content-Type", "application/json")
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

	targetPath := strings.TrimPrefix(r.URL.Path, "/api")
	targetURL := fmt.Sprintf("http://%s%s", net.JoinHostPort(host, strconv.Itoa(port)), targetPath)
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, r.Body)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"error":"failed to create proxy request","code":500}`, http.StatusInternalServerError)
		return
	}
	proxyReq.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	proxyReq.Header.Set("Authorization", "Bearer "+pidData.Token)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(proxyReq)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
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
