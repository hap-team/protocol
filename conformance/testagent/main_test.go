package main

import "testing"

func TestHandleTaskEcho(t *testing.T) {
	result := handleTask(map[string]any{
		"type": "task", "task_id": "task-1", "run_id": "run-1",
		"operation": "echo", "input": map[string]any{"message": "hello"},
	})
	if result["status"] != "succeeded" || result["outcome"] != "completed" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result["task_id"] != "task-1" || result["run_id"] != "run-1" {
		t.Fatalf("result identity changed: %#v", result)
	}
}
