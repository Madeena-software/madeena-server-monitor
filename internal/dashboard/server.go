// Package dashboard provides the HTTP server for the live web monitoring dashboard.
// It exposes three endpoints:
//
//	GET /                    – serves the embedded web/index.html SPA
//	GET /api/metrics/live    – Server-Sent Events stream with live sensor data
//	GET /api/metrics/history – JSON snapshot of the rolling metric history
package dashboard

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"time"

	"github.com/Madeena-software/madeena-server-monitor/internal/assets"
	"github.com/Madeena-software/madeena-server-monitor/internal/checker"
)

// Server is the live-dashboard HTTP server.
type Server struct {
	store    *checker.MetricsStore
	interval time.Duration
}

// New creates a new dashboard Server.
// store is the shared MetricsStore updated by the monitor loop.
// interval is how often SSE events are pushed to clients.
func New(store *checker.MetricsStore, interval time.Duration) *Server {
	return &Server{store: store, interval: interval}
}

// ListenAndServe registers routes and starts the HTTP server on the given addr
// (e.g., ":8080"). It blocks until the server fails.
func (s *Server) ListenAndServe(addr string) error {
	mux := http.NewServeMux()

	// Static files – serve from the embedded FS, stripping the "web/" prefix.
	webFS, err := fs.Sub(assets.WebFS, "web")
	if err != nil {
		return fmt.Errorf("dashboard: unable to sub embed FS: %w", err)
	}
	mux.Handle("/", http.FileServer(http.FS(webFS)))

	// REST – history data for chart initialisation
	mux.HandleFunc("/api/metrics/history", s.handleHistory)

	// SSE – live push of current sensor values
	mux.HandleFunc("/api/metrics/live", s.handleLive)

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 0, // disabled for SSE streams (long-lived connections)
		IdleTimeout:  120 * time.Second,
	}

	log.Printf("INFO: dashboard listening on %s", addr)
	return srv.ListenAndServe()
}

// handleHistory returns the rolling metric history as a JSON object.
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	resp := s.store.BuildHistoryResponse()
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("ERROR: dashboard history encode: %v", err)
	}
}

// handleLive streams SSE events with the current live sensor snapshot at the
// configured interval. Each event is a JSON-encoded LiveResponse.
func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
	// Verify the client accepts SSE
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ctx := r.Context()
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// Send an initial event immediately so the browser doesn't wait
	s.pushEvent(w, flusher)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.pushEvent(w, flusher)
		}
	}
}

// pushEvent serialises the current LiveResponse and writes a single SSE event.
func (s *Server) pushEvent(w http.ResponseWriter, flusher http.Flusher) {
	resp := s.store.BuildLiveResponse()
	data, err := json.Marshal(resp)
	if err != nil {
		log.Printf("ERROR: dashboard SSE marshal: %v", err)
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}
