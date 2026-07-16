package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
)

type a2aRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      string `json:"id"`
	Method  string `json:"method"`
	Params  struct {
		Message map[string]any `json:"message"`
	} `json:"params"`
}

type a2aResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      string `json:"id"`
	Result  Result `json:"result"`
}

func runA2A(ctx context.Context, task map[string]any, handler scenarioHandler) (Result, error) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var envelope a2aRequest
		if json.NewDecoder(http.MaxBytesReader(w, request.Body, maxFrameSize)).Decode(&envelope) != nil ||
			envelope.JSONRPC != "2.0" || envelope.Method != "message/send" {
			http.Error(w, "invalid A2A request", http.StatusBadRequest)
			return
		}
		result, err := handler(envelope.Params.Message)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, a2aResponse{JSONRPC: "2.0", ID: envelope.ID, Result: result})
	}))
	defer server.Close()

	runID, ok := task["run_id"].(string)
	if !ok {
		return Result{}, errors.New("task run_id is required")
	}
	envelope := a2aRequest{JSONRPC: "2.0", ID: runID, Method: "message/send"}
	envelope.Params.Message = task
	data, err := json.Marshal(envelope)
	if err != nil {
		return Result{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, bytes.NewReader(data))
	if err != nil {
		return Result{}, err
	}
	response, err := safeHTTPClient(server.URL).Do(request)
	if err != nil {
		return Result{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("A2A request returned %s", response.Status)
	}
	var reply a2aResponse
	if err := json.NewDecoder(response.Body).Decode(&reply); err != nil {
		return Result{}, err
	}
	if reply.JSONRPC != "2.0" || reply.ID != runID {
		return Result{}, errors.New("invalid A2A response correlation")
	}
	return reply.Result, nil
}
