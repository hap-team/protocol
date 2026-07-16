package conformance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nhooyr.io/websocket"
)

func TestRunProcessAgentResolvesBareExecutableFromPath(t *testing.T) {
	directory := t.TempDir()
	scriptPath := filepath.Join(directory, "agent.sh")
	script := `#!/bin/sh
read handshake
printf '%s\n' '{"type":"handshake_ack","protocol_version":"0.2"}'
read task
printf '%s\n' '{"type":"result","task_id":"task-actual","run_id":"run-actual","status":"failed","outcome":"needs_human","output":{},"error":{"code":"remote_failure","message":"remote agent failed","retryable":false}}'
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := RunProcessAgent(context.Background(), []string{"sh", scriptPath}, directory, actualScenario())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" || result.Error["code"] != "remote_failure" {
		t.Fatalf("result = %#v, want remote failed result", result)
	}
}

func TestRunHTTPAgentReturnsRemoteResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/tasks" {
			http.NotFound(w, request)
			return
		}
		writeJSON(w, http.StatusOK, failedRemoteResult())
	}))
	defer server.Close()

	result, err := RunHTTPAgent(context.Background(), server.URL, actualScenario())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" || result.Error["code"] != "remote_failure" {
		t.Fatalf("result = %#v, want remote failed result", result)
	}
}

func TestRunWebSocketAgentReturnsRemoteResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		connection, err := websocket.Accept(w, request, nil)
		if err != nil {
			return
		}
		defer connection.CloseNow()

		if _, _, err := connection.Read(request.Context()); err != nil {
			return
		}
		acknowledgement, _ := json.Marshal(map[string]any{
			"type":             "handshake_ack",
			"protocol_version": "0.2",
		})
		if err := connection.Write(request.Context(), websocket.MessageText, acknowledgement); err != nil {
			return
		}
		if _, _, err := connection.Read(request.Context()); err != nil {
			return
		}
		result, _ := json.Marshal(failedRemoteResult())
		_ = connection.Write(request.Context(), websocket.MessageText, result)
	}))
	defer server.Close()

	endpoint := "ws" + strings.TrimPrefix(server.URL, "http")
	result, err := RunWebSocketAgent(context.Background(), endpoint, actualScenario())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" || result.Error["code"] != "remote_failure" {
		t.Fatalf("result = %#v, want remote failed result", result)
	}
}

func TestRunA2AAgentReturnsRemoteResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var envelope struct {
			JSONRPC string `json:"jsonrpc"`
			ID      string `json:"id"`
			Method  string `json:"method"`
		}
		if err := json.NewDecoder(request.Body).Decode(&envelope); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if envelope.JSONRPC != "2.0" || envelope.ID != "run-actual" || envelope.Method != "message/send" {
			http.Error(w, "invalid A2A request", http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"jsonrpc": "2.0",
			"id":      envelope.ID,
			"result":  failedRemoteResult(),
		})
	}))
	defer server.Close()

	result, err := RunA2AAgent(context.Background(), server.URL, actualScenario())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" || result.Error["code"] != "remote_failure" {
		t.Fatalf("result = %#v, want remote failed result", result)
	}
}

func TestRunWebSocketAgentRejectsWrongProtocolVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		connection, err := websocket.Accept(w, request, nil)
		if err != nil {
			return
		}
		defer connection.CloseNow()

		if _, _, err := connection.Read(request.Context()); err != nil {
			return
		}
		acknowledgement, _ := json.Marshal(map[string]any{
			"type":             "handshake_ack",
			"protocol_version": "0.1",
		})
		_ = connection.Write(request.Context(), websocket.MessageText, acknowledgement)
	}))
	defer server.Close()

	endpoint := "ws" + strings.TrimPrefix(server.URL, "http")
	_, err := RunWebSocketAgent(context.Background(), endpoint, Scenario{})
	if err == nil || !strings.Contains(err.Error(), "handshake") {
		t.Fatalf("RunWebSocketAgent error = %v, want handshake error", err)
	}
}

func actualScenario() Scenario {
	return Scenario{Task: map[string]any{
		"type":             "task",
		"protocol_version": "0.2",
		"task_id":          "task-actual",
		"run_id":           "run-actual",
		"operation":        "test",
		"input":            map[string]any{},
	}}
}

func failedRemoteResult() Result {
	return Result{
		Type:    "result",
		TaskID:  "task-actual",
		RunID:   "run-actual",
		Status:  "failed",
		Outcome: "needs_human",
		Output:  map[string]any{},
		Error: map[string]any{
			"code":      "remote_failure",
			"message":   "remote agent failed",
			"retryable": false,
		},
	}
}
