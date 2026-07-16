package conformance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDescriptorFixtures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		path          string
		wantViolation string
	}{
		{name: "minimal descriptor", path: "valid/minimal.yaml"},
		{name: "full descriptor with relative contracts", path: "valid/full.yaml"},
		{name: "namespaced extensions", path: "valid/extensions.yaml"},
		{name: "removed mind", path: "invalid/removed-mind.yaml", wantViolation: "mind"},
		{name: "unknown core field", path: "invalid/unknown-core-field.yaml", wantViolation: "owner"},
		{name: "duplicate interface ID", path: "invalid/duplicate-interface-id.yaml", wantViolation: "interfaces"},
		{name: "invalid agent semantic version", path: "invalid/bad-agent-version.yaml", wantViolation: "agent.version"},
		{name: "embedded credential value", path: "invalid/credential-value.yaml", wantViolation: "credentials"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := os.ReadFile(filepath.Join("..", "..", "conformance", "descriptors", tt.path))
			if err != nil {
				t.Fatal(err)
			}

			violations, err := ValidateDescriptor(data)
			if err != nil {
				t.Fatalf("ValidateDescriptor returned infrastructure error: %v", err)
			}

			if tt.wantViolation == "" {
				if len(violations) != 0 {
					t.Fatalf("valid descriptor has violations: %v", violations)
				}
				return
			}

			for _, violation := range violations {
				if strings.Contains(violation.Path, tt.wantViolation) {
					return
				}
			}
			t.Fatalf("expected violation containing path %q, got %v", tt.wantViolation, violations)
		})
	}
}
