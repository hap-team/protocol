package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

type task struct {
	Type            string `json:"type"`
	ProtocolVersion string `json:"protocol_version"`
	TaskID          string `json:"task_id"`
	RunID           string `json:"run_id"`
	Operation       string `json:"operation"`
	Input           struct {
		URL string `json:"url"`
	} `json:"input"`
}

type result struct {
	Type    string         `json:"type"`
	TaskID  string         `json:"task_id"`
	RunID   string         `json:"run_id"`
	Status  string         `json:"status"`
	Outcome string         `json:"outcome"`
	Output  map[string]any `json:"output"`
	Error   map[string]any `json:"error,omitempty"`
	Usage   map[string]any `json:"usage,omitempty"`
}

type server struct {
	client  *http.Client
	mu      sync.RWMutex
	results map[string]result
}

func main() {
	address := os.Getenv("HAP_LISTEN_ADDR")
	if address == "" {
		address = "127.0.0.1:8080"
	}
	log.Printf("health-checker listening on %s", address)
	log.Fatal(http.ListenAndServe(address, routes(&http.Client{Timeout: 10 * time.Second})))
}

func routes(client *http.Client) http.Handler {
	s := &server{client: client, results: make(map[string]result)}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /tasks", s.postTask)
	mux.HandleFunc("GET /runs/{run_id}", s.getRun)
	mux.HandleFunc("GET /runs/{run_id}/events", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, []any{})
	})
	mux.HandleFunc("POST /runs/{run_id}/cancel", func(w http.ResponseWriter, request *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"type": "cancel_ack", "run_id": request.PathValue("run_id"),
			"accepted": false, "terminal": false,
		})
	})
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return http.MaxBytesHandler(mux, 4<<20)
}

func (s *server) postTask(w http.ResponseWriter, request *http.Request) {
	var incoming task
	if err := json.NewDecoder(request.Body).Decode(&incoming); err != nil {
		http.Error(w, "invalid task", http.StatusBadRequest)
		return
	}
	if incoming.Type != "task" || incoming.ProtocolVersion != "0.2" ||
		incoming.Operation != "check-url" || incoming.TaskID == "" ||
		incoming.RunID == "" || incoming.Input.URL == "" {
		writeJSON(w, http.StatusOK, failure(incoming, "invalid_input", "a check-url task with input.url is required"))
		return
	}

	started := time.Now()
	response, err := s.client.Get(incoming.Input.URL)
	if err != nil {
		completed := failure(incoming, "dependency_unavailable", err.Error())
		s.store(completed)
		writeJSON(w, http.StatusOK, completed)
		return
	}
	defer response.Body.Close()

	outcome := "unhealthy"
	if response.StatusCode >= 200 && response.StatusCode < 400 {
		outcome = "healthy"
	}
	completed := result{
		Type: "result", TaskID: incoming.TaskID, RunID: incoming.RunID,
		Status: "succeeded", Outcome: outcome,
		Output: map[string]any{
			"url": incoming.Input.URL, "status_code": response.StatusCode,
			"duration_ms": time.Since(started).Milliseconds(),
		},
		Usage: map[string]any{
			"measurements": []any{map[string]any{
				"name": "http.requests", "quantity": 1, "unit": "request",
			}},
		},
	}
	s.store(completed)
	writeJSON(w, http.StatusOK, completed)
}

func (s *server) getRun(w http.ResponseWriter, request *http.Request) {
	s.mu.RLock()
	completed, ok := s.results[request.PathValue("run_id")]
	s.mu.RUnlock()
	if !ok {
		http.NotFound(w, request)
		return
	}
	writeJSON(w, http.StatusOK, completed)
}

func (s *server) store(completed result) {
	s.mu.Lock()
	s.results[completed.RunID] = completed
	s.mu.Unlock()
}

func failure(incoming task, code, message string) result {
	return result{
		Type: "result", TaskID: incoming.TaskID, RunID: incoming.RunID,
		Status: "failed", Outcome: code,
		Output: map[string]any{},
		Error: map[string]any{
			"code": code, "message": message, "retryable": code == "dependency_unavailable",
		},
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}
