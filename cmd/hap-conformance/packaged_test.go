package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPackagedHTTPExercisesLifecycleAndShippedSchema(t *testing.T) {
	for _, fault := range []string{"", "handshake", "schema", "event-order", "cancellation", "deadline", "duplicate"} {
		t.Run(fault, func(t *testing.T) {
			type run struct {
				task   map[string]any
				result map[string]any
				events []any
			}
			runs := map[string]*run{}
			var mu sync.Mutex
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				send := func(v any) { _ = json.NewEncoder(w).Encode(v) }
				if r.URL.Path == "/handshake" {
					name := "sample-agent"
					if fault == "handshake" {
						name = "wrong-agent"
					}
					send(map[string]any{"type": "handshake_ack", "protocol_version": "0.2", "agent": map[string]string{"name": name, "version": "1.0.0"}, "max_concurrent_runs": 1})
					return
				}
				if r.URL.Path == "/tasks" {
					var task map[string]any
					_ = json.NewDecoder(r.Body).Decode(&task)
					id := task["run_id"].(string)
					if old := runs[id]; old != nil {
						if fault == "duplicate" {
							old.result["outcome"] = "changed"
						}
						send(old.result)
						return
					}
					output := any(map[string]string{"value": "ok"})
					if fault == "schema" {
						output = map[string]string{"value": "wrong"}
					}
					value := &run{task: task, result: map[string]any{"type": "result", "task_id": task["task_id"], "run_id": id, "status": "succeeded", "outcome": "completed", "output": output}}
					sequence := 1
					if fault == "event-order" {
						sequence = 2
					}
					value.events = []any{map[string]any{"type": "event", "run_id": id, "sequence": sequence, "time": time.Now().UTC().Format(time.RFC3339Nano), "name": "task.completed", "data": map[string]any{}}}
					if strings.Contains(id, "cancellation") {
						value.result["status"] = "running"
					}
					if strings.Contains(id, "deadline") && fault != "deadline" {
						value.result["status"] = "cancelled"
					}
					runs[id] = value
					w.WriteHeader(202)
					send(map[string]string{"run_id": id})
					return
				}
				parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
				if len(parts) < 2 || runs[parts[1]] == nil {
					w.WriteHeader(404)
					send(map[string]string{"error": "missing"})
					return
				}
				run := runs[parts[1]]
				if len(parts) == 3 && parts[2] == "events" {
					send(run.events)
					return
				}
				if len(parts) == 3 && parts[2] == "cancel" {
					run.result["status"] = "cancelled"
					send(map[string]any{"type": "cancel_ack", "run_id": parts[1], "accepted": fault != "cancellation", "terminal": false})
					return
				}
				if run.result["status"] == "running" {
					w.WriteHeader(202)
					send(map[string]string{"state": "running"})
					return
				}
				send(run.result)
			}))
			defer server.Close()
			root := t.TempDir()
			if os.WriteFile(filepath.Join(root, "result.json"), []byte(`{"type":"object","required":["value"],"properties":{"value":{"const":"ok"}},"additionalProperties":false}`), 0600) != nil {
				t.Fatal("schema")
			}
			descriptor := []byte("hap: \"0.2\"\nagent: {name: sample-agent, version: 1.0.0}\ncontracts: {result: {schema: result.json}}\ninterfaces:\n  - {id: api, type: http, endpoint: https://example.invalid}\n")
			scenario, _ := json.Marshal(packagedScenario{Schema: "hap-packaged-scenario/v1", InterfaceID: "api", Endpoint: server.URL, ResultTarget: "output", Task: map[string]any{"type": "task", "protocol_version": "0.2", "task_id": "test", "run_id": "test", "operation": "fixture", "input": map[string]any{}, "context": nil}})
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			result, err := verifyPackaged(ctx, descriptor, root, scenario)
			if fault == "" {
				if err != nil || result.Status != "passed" || len(result.Checks) != len(packagedChecks) {
					t.Fatal(result, err)
				}
			} else if err == nil || result.Status == "passed" {
				t.Fatal("fault passed", fault)
			}
		})
	}
}
func TestPackagedSchemasCannotReadOutsideArtifact(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.json")
	_ = os.WriteFile(outside, []byte(`{}`), 0600)
	_ = os.Symlink(outside, filepath.Join(root, "linked.json"))
	for _, name := range []string{"../secret.json", "linked.json", outside, "https://example.invalid/schema"} {
		if _, err := packagedSchema(root, name); err == nil {
			t.Fatal("unsafe schema admitted", name)
		}
	}
	_ = os.WriteFile(filepath.Join(root, "remote.json"), []byte(`{"$ref":"https://example.invalid/schema"}`), 0600)
	if _, err := packagedSchema(root, "remote.json"); err == nil {
		t.Fatal("external schema admitted")
	}
}
