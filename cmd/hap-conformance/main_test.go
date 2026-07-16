package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunInterfaceExecutesDeclaredProcessCommand(t *testing.T) {
	root := t.TempDir()
	agentDirectory := filepath.Join(root, "testagent")
	if err := os.MkdirAll(agentDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	agentPath := filepath.Join(agentDirectory, "wrong-agent.sh")
	agent := `#!/bin/sh
read handshake
printf '%s\n' '{"type":"handshake_ack","protocol_version":"0.2","agent":{"name":"wrong-agent","version":"1.0.0"},"max_concurrent_runs":1}'
read task
printf '%s\n' '{"type":"result","task_id":"issue-42","run_id":"run-success","status":"failed","outcome":"needs_human","output":{},"error":{"code":"wrong","message":"wrong result","retryable":false}}'
`
	if err := os.WriteFile(agentPath, []byte(agent), 0o755); err != nil {
		t.Fatal(err)
	}
	descriptorPath := filepath.Join(agentDirectory, "hap.yaml")
	descriptor := "hap: \"0.2\"\nagent:\n  name: wrong-agent\n  version: 1.0.0\ninterfaces:\n  - id: local\n    type: process\n    protocol: jsonl\n    command: [\"" + agentPath + "\"]\n"
	if err := os.WriteFile(descriptorPath, []byte(descriptor), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"interface", "--descriptor", descriptorPath, "--interface", "local"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run returned %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunInterfaceRejectsMismatchedResultCorrelation(t *testing.T) {
	directory := t.TempDir()
	agentPath := filepath.Join(directory, "wrong-correlation.sh")
	agent := `#!/bin/sh
read handshake
printf '%s\n' '{"type":"handshake_ack","protocol_version":"0.2"}'
read task
printf '%s\n' '{"type":"result","task_id":"issue-42","run_id":"wrong-run","status":"succeeded","outcome":"completed","output":{"message":"hello"}}'
`
	if err := os.WriteFile(agentPath, []byte(agent), 0o755); err != nil {
		t.Fatal(err)
	}
	descriptorPath := filepath.Join(directory, "hap.yaml")
	descriptor := "hap: \"0.2\"\nagent:\n  name: wrong-correlation\n  version: 1.0.0\ninterfaces:\n  - id: local\n    type: process\n    protocol: jsonl\n    command: [\"./wrong-correlation.sh\"]\n"
	if err := os.WriteFile(descriptorPath, []byte(descriptor), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"interface", "--descriptor", descriptorPath, "--interface", "local"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run returned %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

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
	descriptorPath := buildTestAgentDescriptor(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"interface",
		"--descriptor", descriptorPath,
		"--interface", "local",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run returned %d; stderr: %s", code, stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "PASS local (process)") {
		t.Fatalf("stdout %q does not contain interface result", got)
	}
}

func buildTestAgentDescriptor(t *testing.T) string {
	t.Helper()

	directory := t.TempDir()
	binaryPath := filepath.Join(directory, "hap-conformance-testagent")
	build := exec.Command("go", "build", "-o", binaryPath, "../../conformance/testagent")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build test agent: %v\n%s", err, output)
	}

	descriptorPath := filepath.Join(directory, "hap.yaml")
	descriptor := "hap: \"0.2\"\nagent:\n  name: conformance-agent\n  version: 0.2.0\ninterfaces:\n  - id: local\n    type: process\n    protocol: jsonl\n    command: [\"./hap-conformance-testagent\", \"process\"]\n"
	if err := os.WriteFile(descriptorPath, []byte(descriptor), 0o644); err != nil {
		t.Fatal(err)
	}
	return descriptorPath
}
