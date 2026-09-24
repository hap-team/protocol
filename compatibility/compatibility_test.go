package compatibility

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"github.com/hap-team/receipts/go/receipts"
	"testing"
	"time"
)

var descriptor = []byte(`hap: "0.2"
agent: {name: sample-agent, version: 1.0.0}
interfaces:
  - {id: api, type: http, endpoint: "https://example.invalid"}
`)

func TestCompatibilityRequiresTrustedMatchingDescriptorAndInterfaceReceipts(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	artifact := receipts.Digest([]byte("build-one"))
	keys := map[string]Verifier{"fixture": {PublicKey: pub, ProducerID: ProducerID}}
	makeEvidence := func(check, iface, status string, at time.Time) Evidence {
		context := Context{Schema: ContextSchema, AgentName: "sample-agent", AgentVersion: "1.0.0", ProtocolVersion: "0.2", DefinitionDigest: receipts.Digest(descriptor), ArtifactDigest: artifact, Check: check, InterfaceID: iface, Suite: SuiteID, ScenarioDigest: receipts.Digest([]byte("scenario"))}
		ev, err := Issue(context, status, "run-1", at, "fixture", priv)
		if err != nil {
			t.Fatal(err)
		}
		return ev
	}
	now := time.Now().UTC().Add(-time.Minute)
	descriptorCheck := makeEvidence("descriptor", "", "succeeded", now)
	interfaceCheck := makeEvidence("interface", "api", "succeeded", now)
	valid := []Evidence{descriptorCheck, interfaceCheck}
	for _, tc := range []struct {
		name     string
		raw      []byte
		artifact string
		evidence []Evidence
		keys     map[string]Verifier
		want     string
	}{
		{"valid", descriptor, artifact, valid, keys, "compatible"},
		{"missing interface", descriptor, artifact, valid[:1], keys, "unverified"},
		{"untrusted", descriptor, artifact, valid, nil, "unverified"},
		{"different build", descriptor, receipts.Digest([]byte("build-two")), valid, keys, "unverified"},
		{"changed definition", append(append([]byte{}, descriptor...), []byte("# changed\n")...), artifact, valid, keys, "unverified"},
		{"new failed interface", descriptor, artifact, append(append([]Evidence{}, valid...), makeEvidence("interface", "api", "failed", now.Add(time.Second))), keys, "not_compatible"},
		{"cancelled check", descriptor, artifact, []Evidence{descriptorCheck, makeEvidence("interface", "api", "cancelled", now)}, keys, "unverified"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Verify(tc.raw, tc.artifact, tc.evidence, tc.keys)
			if got.State != tc.want {
				t.Fatalf("state %s, reason %s", got.State, got.Reason)
			}
		})
	}
	tampered := interfaceCheck
	var ctx map[string]any
	_ = json.Unmarshal(tampered.Context, &ctx)
	ctx["artifactDigest"] = receipts.Digest([]byte("other"))
	tampered.Context, _ = json.Marshal(ctx)
	if Verify(descriptor, artifact, []Evidence{descriptorCheck, tampered}, keys).State != "unverified" {
		t.Fatal("tampered context accepted")
	}
	var envelope map[string]any
	_ = json.Unmarshal(interfaceCheck.Receipt, &envelope)
	envelope["payloadDigest"] = artifact
	tampered = interfaceCheck
	tampered.Receipt, _ = json.Marshal(envelope)
	if Verify(descriptor, artifact, []Evidence{descriptorCheck, tampered}, keys).State != "unverified" {
		t.Fatal("tampered receipt accepted")
	}
}

func TestEvidenceRoundTripPreservesExactSignedContextBytes(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	context := Context{Schema: ContextSchema, AgentName: "sample-agent", AgentVersion: "1.0.0", ProtocolVersion: "0.2", DefinitionDigest: receipts.Digest(descriptor), ArtifactDigest: receipts.Digest([]byte("build")), Check: "descriptor", Suite: SuiteID, ScenarioDigest: receipts.Digest([]byte("fixture"))}
	item, err := Issue(context, "succeeded", "check", time.Now().Add(-time.Minute), "fixture", priv)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]Verifier{"fixture": {PublicKey: pub, ProducerID: ProducerID}}
	verified, err := receipts.Verify(item.Receipt, receipts.TerminalV1, func(string) (ed25519.PublicKey, error) { return pub, nil })
	if err != nil {
		t.Fatal(err)
	}
	item.Context, _ = json.MarshalIndent(context, "", "  ")
	payload := verified.Receipt
	payload.Subject.ConfigurationDigest = receipts.Digest(item.Context)
	raw, err := receipts.Generate(payload)
	if err != nil {
		t.Fatal(err)
	}
	item.Receipt, err = receipts.Sign(raw, "fixture", priv)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(item)
	var decoded Evidence
	if json.Unmarshal(encoded, &decoded) != nil || string(decoded.Context) != string(item.Context) {
		t.Fatal("JSON persistence changed exact context bytes")
	}
	if got := Verify(descriptor, context.ArtifactDigest, []Evidence{decoded}, keys); len(got.Checks) != 1 || got.Checks[0].Status != "succeeded" {
		t.Fatalf("roundtrip lost signature binding: %#v", got)
	}
}

func TestDeploymentRequiresPackagedEvidenceForExactPlatformAndInterface(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	keys := map[string]Verifier{"fixture": {PublicKey: pub, ProducerID: ProducerID}}
	digest := receipts.Digest([]byte("image"))
	at := time.Now().Add(-time.Minute).UTC()
	c := Context{Schema: PackagedContextSchema, AgentName: "sample-agent", AgentVersion: "1.0.0", ProtocolVersion: "0.2", DefinitionDigest: receipts.Digest(descriptor), ArtifactDigest: digest, Suite: PackagedSuiteID, ScenarioDigest: digest, Platform: "linux/amd64", ManifestDigest: digest, RuntimeConfigDigest: digest, VerifierArtifactDigest: digest, VerificationReceiptDigest: digest, FixturesDigest: digest, ExecutionDigest: digest}
	issue := func(check, iface string) Evidence {
		t.Helper()
		v := c
		v.Check = check
		v.InterfaceID = iface
		e, err := Issue(v, "succeeded", "test-run", at, "fixture", key)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	good := []Evidence{issue("descriptor", ""), issue("interface", "api")}
	for _, tc := range []struct {
		name, artifact, platform, iface string
		evidence                        []Evidence
		trusted                         map[string]Verifier
		pass                            bool
	}{
		{"exact", digest, "linux/amd64", "api", good, keys, true},
		{"other image", receipts.Digest([]byte("other")), "linux/amd64", "api", good, keys, false},
		{"other platform", digest, "linux/arm64", "api", good, keys, false},
		{"other interface", digest, "linux/amd64", "other", good, keys, false},
		{"descriptor only", digest, "linux/amd64", "api", good[:1], keys, false},
		{"unknown signer", digest, "linux/amd64", "api", good, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := VerifyDeployment(descriptor, tc.artifact, tc.platform, tc.iface, tc.evidence, tc.trusted)
			if (result.State == "compatible") != tc.pass {
				t.Fatal(result)
			}
		})
	}
	c.Schema = ContextSchema
	c.Suite = SuiteID
	c.Platform = ""
	c.ManifestDigest = ""
	c.RuntimeConfigDigest = ""
	c.VerifierArtifactDigest = ""
	c.VerificationReceiptDigest = ""
	c.FixturesDigest = ""
	c.ExecutionDigest = ""
	source := []Evidence{issue("descriptor", ""), issue("interface", "api")}
	if Verify(descriptor, digest, source, keys).State != "compatible" {
		t.Fatal("legacy verification broken")
	}
	if VerifyDeployment(descriptor, digest, "linux/amd64", "api", source, keys).State == "compatible" {
		t.Fatal("source tests admitted deployment")
	}
	tampered := append([]Evidence{}, good...)
	tampered[1].Context = append(append([]byte{}, good[1].Context...), byte(' '))
	if VerifyDeployment(descriptor, digest, "linux/amd64", "api", tampered, keys).State == "compatible" {
		t.Fatal("tampered context admitted")
	}
}
