package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/hap-team/protocol/compatibility"
	"github.com/hap-team/receipts/go/receipts"
)

func TestExecutedFixtureProducesVerifiableEvidence(t *testing.T) {
	descriptorPath := buildTestAgentDescriptor(t)
	raw, err := os.ReadFile(descriptorPath)
	if err != nil {
		t.Fatal(err)
	}
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	t.Setenv("HAP_FIXTURE_SIGNER", base64.StdEncoding.EncodeToString(key))
	artifact := receipts.Digest([]byte("fixture-build"))
	output := filepath.Join(t.TempDir(), "evidence.json")
	var stdout, stderr bytes.Buffer
	code := run([]string{"interface", "--descriptor", descriptorPath, "--interface", "local", "--artifact-digest", artifact, "--signer-key-id", "fixture", "--signer-key-env", "HAP_FIXTURE_SIGNER", "--evidence-output", output}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("checker failed: %s", stderr.String())
	}
	bundle, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var evidence []compatibility.Evidence
	if err := json.Unmarshal(bundle, &evidence); err != nil {
		t.Fatal(err)
	}
	result := compatibility.Verify(raw, artifact, evidence, map[string]compatibility.Verifier{"fixture": {PublicKey: pub, ProducerID: compatibility.ProducerID}})
	if result.State != "compatible" {
		t.Fatalf("result %#v", result)
	}
	if bytes.Contains(bundle, []byte(base64.StdEncoding.EncodeToString(key))) {
		t.Fatal("private signer leaked")
	}
}

func TestSourceOwnedJobSignsActualPassAndFailureWithoutSharingSigner(t *testing.T) {
	descriptorPath := buildTestAgentDescriptor(t)
	descriptor, _ := os.ReadFile(descriptorPath)
	directory := filepath.Dir(descriptorPath)
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	t.Setenv("HAP_FIXTURE_SIGNER", base64.StdEncoding.EncodeToString(key))
	artifact := receipts.Digest([]byte("fixture-artifact"))
	keys := map[string]compatibility.Verifier{"fixture": {PublicKey: pub, ProducerID: compatibility.ProducerID}}
	for _, failing := range []bool{false, true} {
		script := "#!/bin/sh\nset -eu\ntest -z \"${HAP_FIXTURE_SIGNER:-}\"\n"
		if failing {
			script += "exit 1\n"
		} else {
			script += "./hap-conformance-testagent process <<'EOF'\n{\"type\":\"handshake\",\"supported_versions\":[\"0.2\"],\"client\":{\"name\":\"fixture\",\"version\":\"1.0.0\"}}\nEOF\n"
		}
		if err := os.WriteFile(filepath.Join(directory, "check.sh"), []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
		job, _ := json.Marshal(conformanceJob{Schema: "dev.hap.conformance-job/v1", Definition: "hap.yaml", Interface: "local", WorkingDirectory: ".", Command: []string{"sh", "check.sh"}})
		jobPath := filepath.Join(directory, "job.json")
		if err := os.WriteFile(jobPath, job, 0644); err != nil {
			t.Fatal(err)
		}
		output := filepath.Join(directory, "evidence.json")
		var stdout, stderr bytes.Buffer
		code := run([]string{"job", "--job", jobPath, "--artifact-digest", artifact, "--evidence-output", output, "--signer-key-env", "HAP_FIXTURE_SIGNER", "--signer-key-id", "fixture"}, &stdout, &stderr)
		wantCode, want := 0, "compatible"
		if failing {
			wantCode, want = 1, "not_compatible"
		}
		if code != wantCode {
			t.Fatalf("job code %d: %s", code, stderr.String())
		}
		raw, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		var evidence []compatibility.Evidence
		if json.Unmarshal(raw, &evidence) != nil {
			t.Fatal("invalid bundle")
		}
		if got := compatibility.Verify(descriptor, artifact, evidence, keys); got.State != want {
			t.Fatalf("got %#v want %s", got, want)
		}
	}
}

func TestRunnerFileHandlesKeepSignerOutOfArgumentsAndEvidence(t *testing.T) {
	directory := t.TempDir()
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	artifact := receipts.Digest([]byte("checked-build"))
	for name, value := range map[string]string{"TEST_SIGNER_FILE": base64.StdEncoding.EncodeToString(key), "TEST_KEY_ID_FILE": "fixture", "TEST_ARTIFACT_FILE": artifact} {
		file := filepath.Join(directory, name)
		if err := os.WriteFile(file, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		t.Setenv(name, file)
	}
	descriptorPath := filepath.Join("..", "..", "conformance", "descriptors", "valid", "minimal.yaml")
	descriptor, err := os.ReadFile(descriptorPath)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(directory, "bundle.json")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--schema", filepath.Join("..", "..", "schemas", "0.2", "hap-agent.schema.json"), "--file", descriptorPath, "--evidence-output", output, "--artifact-digest-file-env", "TEST_ARTIFACT_FILE", "--signer-key-id-file-env", "TEST_KEY_ID_FILE", "--signer-key-file-env", "TEST_SIGNER_FILE"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("file handles failed: %s", stderr.String())
	}
	bundle, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var evidence []compatibility.Evidence
	if json.Unmarshal(bundle, &evidence) != nil {
		t.Fatal("invalid bundle")
	}
	result := compatibility.Verify(descriptor, artifact, evidence, map[string]compatibility.Verifier{"fixture": {PublicKey: pub, ProducerID: compatibility.ProducerID}})
	if result.State != "unverified" || len(result.Checks) != 1 || result.Checks[0].Status != "succeeded" {
		t.Fatalf("descriptor-only evidence %#v", result)
	}
	if bytes.Contains(bundle, []byte(directory)) || bytes.Contains(bundle, []byte(base64.StdEncoding.EncodeToString(key))) {
		t.Fatal("signer handle or value leaked")
	}
}
