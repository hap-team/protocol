package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTaskEndpointReturnsHAPResult(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	request := httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewBufferString(
		`{"type":"task","protocol_version":"0.2","task_id":"check-1","run_id":"run-1","operation":"check-url","input":{"url":"`+target.URL+`"},"context":{}}`,
	))
	response := httptest.NewRecorder()
	routes(http.DefaultClient).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	for _, expected := range []string{
		`"type":"result"`, `"task_id":"check-1"`, `"run_id":"run-1"`,
		`"status":"succeeded"`, `"outcome":"healthy"`,
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("body %q does not contain %q", response.Body.String(), expected)
		}
	}
}
