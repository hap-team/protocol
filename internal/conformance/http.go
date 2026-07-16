package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
)

type httpHarness struct {
	handler scenarioHandler
	mu      sync.RWMutex
	results map[string]Result
}

func runHTTP(ctx context.Context, task map[string]any, handler scenarioHandler) (Result, error) {
	harness := &httpHarness{handler: handler, results: make(map[string]Result)}
	server := httptest.NewServer(harness.routes())
	defer server.Close()

	data, err := json.Marshal(task)
	if err != nil {
		return Result{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/tasks", bytes.NewReader(data))
	if err != nil {
		return Result{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := safeHTTPClient(server.URL).Do(request)
	if err != nil {
		return Result{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("HTTP task returned %s", response.Status)
	}
	return decodeResult(response.Body)
}

func (h *httpHarness) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /tasks", h.postTask)
	mux.HandleFunc("GET /runs/{run_id}", h.getRun)
	mux.HandleFunc("GET /runs/{run_id}/events", h.getEvents)
	mux.HandleFunc("POST /runs/{run_id}/cancel", h.cancelRun)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return http.MaxBytesHandler(mux, maxFrameSize)
}

func (h *httpHarness) postTask(w http.ResponseWriter, request *http.Request) {
	var task map[string]any
	if err := json.NewDecoder(request.Body).Decode(&task); err != nil {
		http.Error(w, "invalid task", http.StatusBadRequest)
		return
	}
	result, err := h.handler(task)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	h.mu.Lock()
	h.results[result.RunID] = result
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, result)
}

func (h *httpHarness) getRun(w http.ResponseWriter, request *http.Request) {
	h.mu.RLock()
	result, ok := h.results[request.PathValue("run_id")]
	h.mu.RUnlock()
	if !ok {
		http.NotFound(w, request)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *httpHarness) getEvents(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, []any{})
}

func (h *httpHarness) cancelRun(w http.ResponseWriter, request *http.Request) {
	writeJSON(w, http.StatusOK, CancelAcknowledgement{
		Type: "cancel_ack", RunID: request.PathValue("run_id"), Accepted: true, Terminal: false,
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func decodeResult(reader io.Reader) (Result, error) {
	var result Result
	if err := json.NewDecoder(io.LimitReader(reader, maxFrameSize+1)).Decode(&result); err != nil {
		return Result{}, err
	}
	return result, nil
}

func safeHTTPClient(base string) *http.Client {
	baseURL, _ := url.Parse(base)
	return &http.Client{
		CheckRedirect: func(request *http.Request, _ []*http.Request) error {
			if !strings.EqualFold(request.URL.Host, baseURL.Host) {
				return errors.New("cross-host redirect rejected")
			}
			return nil
		},
	}
}

func httptestServer(handler http.Handler) *httptest.Server {
	return httptest.NewServer(handler)
}

func postAndPoll(ctx context.Context, baseURL string, task map[string]any) (Result, error) {
	data, err := json.Marshal(task)
	if err != nil {
		return Result{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/tasks", bytes.NewReader(data))
	if err != nil {
		return Result{}, err
	}
	response, err := safeHTTPClient(baseURL).Do(request)
	if err != nil {
		return Result{}, err
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("task submit returned %s", response.Status)
	}

	runID := task["run_id"].(string)
	request, err = http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/runs/"+runID, nil)
	if err != nil {
		return Result{}, err
	}
	response, err = safeHTTPClient(baseURL).Do(request)
	if err != nil {
		return Result{}, err
	}
	defer response.Body.Close()
	return decodeResult(response.Body)
}

func verifyCrossHostRedirect(ctx context.Context) error {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, nil, target.URL, http.StatusFound)
	}))
	defer source.Close()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
	if err != nil {
		return err
	}
	response, err := safeHTTPClient(source.URL).Do(request)
	if response != nil {
		response.Body.Close()
	}
	return err
}
