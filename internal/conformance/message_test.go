package conformance

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMessageFixtures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		valid bool
	}{
		{name: "valid/handshake.json", valid: true},
		{name: "valid/handshake-ack.json", valid: true},
		{name: "valid/task.json", valid: true},
		{name: "valid/event.json", valid: true},
		{name: "valid/result-unknown-cost.json", valid: true},
		{name: "valid/result-zero-cost.json", valid: true},
		{name: "valid/result-measured-cost.json", valid: true},
		{name: "valid/result-failed.json", valid: true},
		{name: "valid/cancel.json", valid: true},
		{name: "valid/cancel-ack.json", valid: true},
		{name: "invalid/event-zero-sequence.json"},
		{name: "invalid/negative-usage.json"},
		{name: "invalid/numeric-cost.json"},
		{name: "invalid/cost-without-currency.json"},
		{name: "invalid/failed-result-without-error.json"},
		{name: "invalid/unknown-result-status.json"},
		{name: "invalid/task-board-field.json"},
		{name: "invalid/unknown-core-field.json"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := os.ReadFile(filepath.Join("..", "..", "conformance", "messages", tt.name))
			if err != nil {
				t.Fatal(err)
			}
			violations, err := ValidateMessage(data)
			if err != nil {
				t.Fatalf("ValidateMessage returned infrastructure error: %v", err)
			}
			if tt.valid && len(violations) != 0 {
				t.Fatalf("valid message has violations: %v", violations)
			}
			if !tt.valid && len(violations) == 0 {
				t.Fatal("invalid message unexpectedly passed")
			}
		})
	}
}

func TestCancellationAcknowledgementRunID(t *testing.T) {
	request, err := os.ReadFile(filepath.Join("..", "..", "conformance", "messages", "valid", "cancel.json"))
	if err != nil {
		t.Fatal(err)
	}
	acknowledgement, err := os.ReadFile(filepath.Join("..", "..", "conformance", "messages", "invalid", "cancel-ack-mismatched-run.json"))
	if err != nil {
		t.Fatal(err)
	}

	violations, err := ValidateCancellationAcknowledgement(request, acknowledgement)
	if err != nil {
		t.Fatalf("ValidateCancellationAcknowledgement returned infrastructure error: %v", err)
	}
	if len(violations) == 0 {
		t.Fatal("acknowledgement with a different run_id unexpectedly passed")
	}
}
