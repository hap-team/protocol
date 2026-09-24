package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/hap-team/protocol/compatibility"
	"github.com/hap-team/protocol/definition"
	"github.com/hap-team/receipts/go/receipts"
)

type packagedImage struct {
	URI            string `json:"uri"`
	Digest         string `json:"digest"`
	ManifestDigest string `json:"manifest_digest"`
	ConfigDigest   string `json:"config_digest"`
	Platform       string `json:"platform"`
}
type packagedVerification struct {
	ExecutionDigest  string        `json:"execution_digest"`
	Schema           string        `json:"schema"`
	Status           string        `json:"status"`
	Subject          packagedImage `json:"subject"`
	Verifier         packagedImage `json:"verifier"`
	FixturesDigest   string        `json:"fixtures_digest"`
	DefinitionDigest string        `json:"definition_digest"`
	ResultDigest     string        `json:"result_digest"`
}
type packagedResult struct {
	Schema         string            `json:"schema"`
	Status         string            `json:"status"`
	Suite          string            `json:"suite"`
	InterfaceID    string            `json:"interfaceId"`
	ScenarioDigest string            `json:"scenarioDigest"`
	Checks         map[string]string `json:"checks"`
}

var packagedChecks = []string{"descriptor", "handshake", "result_schema", "ordered_events", "cancellation", "deadline", "duplicate_task"}
var canonicalDigest = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

func readPackagedFile(root, name string, limit int64) ([]byte, error) {
	p := filepath.Join(root, name)
	info, err := os.Lstat(p)
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("packaged evidence unavailable")
	}
	return os.ReadFile(p)
}
func packagedContext(root string) (compatibility.Context, []byte, error) {
	var context compatibility.Context
	raw, err := readPackagedFile(root, "verification.json", 1<<20)
	if err != nil {
		return context, nil, err
	}
	var record packagedVerification
	if json.Unmarshal(raw, &record) != nil || record.Schema != "runner-oci-verification/v1" || record.Status != "passed" {
		return context, nil, errors.New("packaged verification required")
	}
	descriptor, err := readPackagedFile(root, "definition", definition.MaxBytes)
	if err != nil {
		return context, nil, err
	}
	resultRaw, err := readPackagedFile(root, "result.json", 1<<20)
	if err != nil {
		return context, nil, err
	}
	if receipts.Digest(descriptor) != record.DefinitionDigest || receipts.Digest(resultRaw) != record.ResultDigest {
		return context, nil, errors.New("packaged verification digest mismatch")
	}
	for _, image := range []packagedImage{record.Subject, record.Verifier} {
		if !canonicalDigest.MatchString(image.Digest) || !canonicalDigest.MatchString(image.ManifestDigest) || !canonicalDigest.MatchString(image.ConfigDigest) || !regexp.MustCompile(`^linux/[a-z0-9]+$`).MatchString(image.Platform) || !regexp.MustCompile(`^[^@\s]+@`+regexp.QuoteMeta(image.Digest)+`$`).MatchString(image.URI) {
			return context, nil, errors.New("packaged image identity invalid")
		}
	}
	if record.Subject.Platform != record.Verifier.Platform || !canonicalDigest.MatchString(record.FixturesDigest) || !canonicalDigest.MatchString(record.ExecutionDigest) {
		return context, nil, errors.New("packaged platform or fixtures invalid")
	}
	var result packagedResult
	if json.Unmarshal(resultRaw, &result) != nil || result.Schema != "oci-verifier-result/v1" || result.Status != "passed" || result.Suite != compatibility.PackagedSuiteID || !canonicalDigest.MatchString(result.ScenarioDigest) {
		return context, nil, errors.New("packaged conformance suite required")
	}
	for _, check := range packagedChecks {
		if result.Checks[check] != "passed" {
			return context, nil, errors.New("packaged conformance is incomplete")
		}
	}
	public, err := definition.Parse(descriptor)
	if err != nil {
		return context, nil, errors.New("shipped definition invalid")
	}
	found := false
	for _, iface := range public.Interfaces {
		if iface.ID == result.InterfaceID && iface.Type == "http" {
			found = true
		}
	}
	if !found {
		return context, nil, errors.New("deployed HTTP interface is not declared")
	}
	context = compatibility.Context{Schema: compatibility.PackagedContextSchema, AgentName: public.Name, AgentVersion: public.Version, ProtocolVersion: public.ProtocolVersion, DefinitionDigest: record.DefinitionDigest, ArtifactDigest: record.Subject.Digest, Platform: record.Subject.Platform, ManifestDigest: record.Subject.ManifestDigest, RuntimeConfigDigest: record.Subject.ConfigDigest, VerifierArtifactDigest: record.Verifier.Digest, FixturesDigest: record.FixturesDigest, ExecutionDigest: record.ExecutionDigest, VerificationReceiptDigest: receipts.Digest(raw), Suite: compatibility.PackagedSuiteID, ScenarioDigest: result.ScenarioDigest, Check: "interface", InterfaceID: result.InterfaceID}
	return context, descriptor, nil
}

type packagedReleasePolicy struct {
	Schema            string   `json:"schema"`
	VerifierArtifacts []string `json:"verifierArtifacts"`
	Scenarios         []struct {
		AgentName       string `json:"agentName"`
		InterfaceID     string `json:"interfaceId"`
		Digest          string `json:"digest"`
		ExecutionDigest string `json:"executionDigest"`
	} `json:"scenarios"`
}

func authorizedPackagedContext(path string, c compatibility.Context) bool {
	raw, err := readPackagedFile(filepath.Dir(path), filepath.Base(path), 1<<20)
	if err != nil {
		return false
	}
	var policy packagedReleasePolicy
	if json.Unmarshal(raw, &policy) != nil || policy.Schema != "hap-packaged-release-policy/v1" {
		return false
	}
	verifier := false
	for _, digest := range policy.VerifierArtifacts {
		if digest == c.VerifierArtifactDigest && canonicalDigest.MatchString(digest) {
			verifier = true
		}
	}
	if !verifier {
		return false
	}
	for _, scenario := range policy.Scenarios {
		if scenario.AgentName == c.AgentName && scenario.InterfaceID == c.InterfaceID && scenario.Digest == c.ScenarioDigest && scenario.ExecutionDigest == c.ExecutionDigest {
			return true
		}
	}
	return false
}

func runSignPackaged(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("hap-conformance sign-packaged", flag.ContinueOnError)
	flags.SetOutput(stderr)
	input := flags.String("verification", "", "Runner verification artifact directory")
	policy := flags.String("policy", "", "approved verifier and scenario policy")
	output := flags.String("output", "", "detached evidence directory")
	keyIDHandle := flags.String("signer-key-id-file-env", "", "approved key ID file handle")
	keyHandle := flags.String("signer-key-file-env", "", "approved signing key file handle")
	if flags.Parse(args) != nil || *input == "" || *output == "" || *keyIDHandle == "" || *keyHandle == "" || *policy == "" {
		fmt.Fprintln(stderr, "verification, output and signer file handles are required")
		return 2
	}
	c, descriptor, err := packagedContext(*input)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if !authorizedPackagedContext(*policy, c) {
		fmt.Fprintln(stderr, "verifier or scenario is not approved")
		return 1
	}
	id, err := resolveFileHandle("", *keyIDHandle)
	if err != nil {
		fmt.Fprintln(stderr, "signer unavailable")
		return 2
	}
	encoded, err := resolveFileHandle("", *keyHandle)
	if err != nil {
		fmt.Fprintln(stderr, "signer unavailable")
		return 2
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) != ed25519.PrivateKeySize {
		fmt.Fprintln(stderr, "signer unavailable")
		return 2
	}
	at := time.Now().UTC()
	interfaceID := c.InterfaceID
	c.Check = "descriptor"
	c.InterfaceID = ""
	descriptorEvidence, err := compatibility.Issue(c, "succeeded", "packaged-descriptor-"+at.Format("20060102T150405.000000000"), at, id, ed25519.PrivateKey(key))
	if err != nil {
		fmt.Fprintln(stderr, "cannot sign packaged definition")
		return 1
	}
	c.Check = "interface"
	c.InterfaceID = interfaceID
	interfaceEvidence, err := compatibility.Issue(c, "succeeded", "packaged-interface-"+at.Format("20060102T150405.000000000"), at, id, ed25519.PrivateKey(key))
	if err != nil {
		fmt.Fprintln(stderr, "cannot sign packaged interface")
		return 1
	}
	bundle, err := json.Marshal([]compatibility.Evidence{descriptorEvidence, interfaceEvidence})
	if err != nil || os.MkdirAll(*output, 0o700) != nil {
		fmt.Fprintln(stderr, "cannot save detached proof")
		return 2
	}
	for name, raw := range map[string][]byte{"definition": descriptor, "evidence.json": bundle} {
		if os.WriteFile(filepath.Join(*output, name), raw, 0o644) != nil {
			fmt.Fprintln(stderr, "cannot save detached proof")
			return 2
		}
	}
	fmt.Fprintln(stdout, "packaged conformance signed for", c.ArtifactDigest)
	return 0
}
func runDeploymentGate(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("hap-conformance gate-deployment", flag.ContinueOnError)
	flags.SetOutput(stderr)
	handlesEnv := flags.String("input-handles-env", "RUNNER_INPUTS_FILE", "Runner typed input file handle")
	iface := flags.String("interface", "", "deployed interface ID")
	keysPath := flags.String("trusted-keys", "", "public trusted verifier configuration")
	if flags.Parse(args) != nil || *iface == "" || *keysPath == "" {
		return 2
	}
	handle := os.Getenv(*handlesEnv)
	raw, err := readPackagedFile(filepath.Dir(handle), filepath.Base(handle), 1<<20)
	if err != nil || len(raw) > 1<<20 {
		fmt.Fprintln(stderr, "typed release inputs unavailable")
		return 2
	}
	var inputs map[string]struct {
		Kind     string `json:"kind"`
		Path     string `json:"path"`
		URI      string `json:"uri"`
		Digest   string `json:"digest"`
		Platform string `json:"platform"`
	}
	if json.Unmarshal(raw, &inputs) != nil {
		return 2
	}
	image, artifact := inputs["image"], inputs["artifact"]
	if image.Kind != "oci-image" || artifact.Kind != "artifact" || !canonicalDigest.MatchString(image.Digest) || !strings.HasSuffix(image.URI, "@"+image.Digest) || strings.Count(image.URI, "@") != 1 || strings.ContainsAny(image.URI, " \r\n\t") || !canonicalDigest.MatchString(artifact.Digest) {
		fmt.Fprintln(stderr, "typed immutable release inputs required")
		return 1
	}
	descriptor, err := readPackagedFile(artifact.Path, "definition", definition.MaxBytes)
	if err != nil {
		return 1
	}
	bundle, err := readPackagedFile(artifact.Path, "evidence.json", 4<<20)
	if err != nil {
		return 1
	}
	var evidence []compatibility.Evidence
	if json.Unmarshal(bundle, &evidence) != nil || len(evidence) > 256 {
		return 1
	}
	keysRaw, err := readPackagedFile(filepath.Dir(*keysPath), filepath.Base(*keysPath), 64<<10)
	if err != nil || len(keysRaw) > 64<<10 {
		return 2
	}
	var config []struct {
		KeyID      string `json:"keyId"`
		PublicKey  string `json:"publicKey"`
		ProducerID string `json:"producerId"`
	}
	if json.Unmarshal(keysRaw, &config) != nil {
		return 2
	}
	keys := map[string]compatibility.Verifier{}
	for _, k := range config {
		raw, err := base64.StdEncoding.DecodeString(k.PublicKey)
		if err != nil || len(raw) != ed25519.PublicKeySize {
			return 2
		}
		keys[k.KeyID] = compatibility.Verifier{PublicKey: raw, ProducerID: k.ProducerID}
	}
	result := compatibility.VerifyDeployment(descriptor, image.Digest, image.Platform, *iface, evidence, keys)
	if result.State != "compatible" {
		fmt.Fprintln(stderr, "deployment denied:", result.Reason)
		return 1
	}
	fmt.Fprintln(stdout, "deployment proof verified for", image.Digest)
	return 0
}
