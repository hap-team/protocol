package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunValidDescriptor(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"--schema", filepath.Join("..", "..", "schemas", "0.2", "hap-agent.schema.json"),
		"--file", filepath.Join("..", "..", "conformance", "descriptors", "valid", "minimal.yaml"),
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run returned %d; stderr: %s", code, stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "HAP 0.2 echo-agent@1.0.0") {
		t.Fatalf("stdout %q does not contain descriptor identity", got)
	}
}

func TestRunInterfaceDescriptor(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"interface",
		"--descriptor", filepath.Join("..", "..", "conformance", "testagent", "hap.yaml"),
		"--interface", "local",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run returned %d; stderr: %s", code, stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "PASS local (process)") {
		t.Fatalf("stdout %q does not contain interface result", got)
	}
}
