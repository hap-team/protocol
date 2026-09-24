package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hap-team/protocol/compatibility"
	"github.com/hap-team/receipts/go/receipts"
)

func TestPackagedEvidenceBindsRunnerDerivedArtifactAndAllChecks(t *testing.T) {
	root := t.TempDir()
	descriptor := []byte("hap: '0.2'\nagent: {name: sample-agent, version: 1.0.0}\ninterfaces: [{id: api, type: http, endpoint: https://example.invalid}]\n")
	result := packagedResult{Schema: "oci-verifier-result/v1", Status: "passed", Suite: compatibility.PackagedSuiteID, InterfaceID: "api", ScenarioDigest: receipts.Digest([]byte("scenario")), Checks: map[string]string{}}
	for _, check := range packagedChecks {
		result.Checks[check] = "passed"
	}
	data, _ := json.Marshal(result)
	digest := receipts.Digest([]byte("image"))
	verifier := receipts.Digest([]byte("verifier"))
	record := packagedVerification{ExecutionDigest: digest, Schema: "runner-oci-verification/v1", Status: "passed", Subject: packagedImage{URI: "registry.invalid/agent@" + digest, Digest: digest, ManifestDigest: digest, ConfigDigest: digest, Platform: "linux/amd64"}, Verifier: packagedImage{URI: "registry.invalid/checker@" + verifier, Digest: verifier, ManifestDigest: verifier, ConfigDigest: verifier, Platform: "linux/amd64"}, DefinitionDigest: receipts.Digest(descriptor), ResultDigest: receipts.Digest(data), FixturesDigest: receipts.Digest([]byte("fixtures"))}
	save := func() {
		raw, _ := json.Marshal(record)
		_ = os.WriteFile(filepath.Join(root, "verification.json"), raw, 0600)
		_ = os.WriteFile(filepath.Join(root, "definition"), descriptor, 0600)
		_ = os.WriteFile(filepath.Join(root, "result.json"), data, 0600)
	}
	save()
	context, shipped, err := packagedContext(root)
	if err != nil || context.ArtifactDigest != digest || context.Platform != "linux/amd64" || string(shipped) != string(descriptor) {
		t.Fatal(context, err)
	}
	policy := filepath.Join(root, "policy.json")
	_ = os.WriteFile(policy, []byte(`{"schema":"hap-packaged-release-policy/v1","verifierArtifacts":[],"scenarios":[]}`), 0600)
	if authorizedPackagedContext(policy, context) {
		t.Fatal("unapproved verifier signed")
	}
	approved := map[string]any{"schema": "hap-packaged-release-policy/v1", "verifierArtifacts": []string{verifier}, "scenarios": []any{map[string]string{"agentName": "sample-agent", "interfaceId": "api", "digest": context.ScenarioDigest, "executionDigest": context.ExecutionDigest}}}
	raw, _ := json.Marshal(approved)
	_ = os.WriteFile(policy, raw, 0600)
	if !authorizedPackagedContext(policy, context) {
		t.Fatal("approved verifier rejected")
	}
	context.ScenarioDigest = "sha256:" + strings.Repeat("0", 64)
	if authorizedPackagedContext(policy, context) {
		t.Fatal("different scenario signed")
	}
	for _, name := range []string{"definition", "result.json"} {
		save()
		p := filepath.Join(root, name)
		raw, _ := os.ReadFile(p)
		_ = os.WriteFile(p, append(raw, ' '), 0600)
		if _, _, err := packagedContext(root); err == nil {
			t.Fatal("tampered artifact accepted", name)
		}
	}
	save()
	record.Subject.URI = "registry.invalid/agent:latest"
	save()
	if _, _, err := packagedContext(root); err == nil {
		t.Fatal("mutable subject accepted")
	}
	record.Subject.URI = "registry.invalid/agent@" + digest
	delete(result.Checks, "deadline")
	data, _ = json.Marshal(result)
	record.ResultDigest = receipts.Digest(data)
	save()
	if _, _, err := packagedContext(root); err == nil {
		t.Fatal("incomplete suite signed")
	}
}
