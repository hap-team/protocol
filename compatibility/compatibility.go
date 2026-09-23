// Package compatibility binds HAP definition and interface checks to signed
// terminal receipts. Verification never invokes an agent or fetches a URL.
package compatibility

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"sort"
	"time"

	"github.com/hap-team/protocol/definition"
	"github.com/hap-team/receipts/go/receipts"
)

const ContextSchema = "dev.hap.agent-conformance-context/v1"
const SuiteID = "hap-0.2-published-agent/v1"
const ProducerID = "hap-conformance"

var digestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

type Context struct {
	Schema           string `json:"schema"`
	AgentName        string `json:"agentName"`
	AgentVersion     string `json:"agentVersion"`
	ProtocolVersion  string `json:"protocolVersion"`
	DefinitionDigest string `json:"definitionDigest"`
	ArtifactDigest   string `json:"artifactDigest"`
	Check            string `json:"check"`
	InterfaceID      string `json:"interfaceId,omitempty"`
	Suite            string `json:"suite"`
	ScenarioDigest   string `json:"scenarioDigest"`
}
type Evidence struct {
	// Base64 preserves the exact signed context bytes across JSON persistence.
	Context []byte          `json:"context"`
	Receipt json.RawMessage `json:"receipt"`
}
type Verifier struct {
	PublicKey  ed25519.PublicKey
	ProducerID string
}
type Check struct {
	Check         string   `json:"check"`
	InterfaceID   string   `json:"interfaceId,omitempty"`
	Status        string   `json:"status"`
	ReceiptDigest string   `json:"receiptDigest"`
	KeyIDs        []string `json:"keyIds"`
	Producer      string   `json:"producer"`
	IssuedAt      string   `json:"issuedAt"`
}
type Result struct {
	State            string             `json:"state"`
	Reason           string             `json:"reason"`
	ProtocolVersion  string             `json:"protocolVersion,omitempty"`
	DefinitionDigest string             `json:"definitionDigest,omitempty"`
	ArtifactDigest   string             `json:"artifactDigest,omitempty"`
	Definition       *definition.Public `json:"definition,omitempty"`
	Checks           []Check            `json:"checks"`
}

// Issue is used by the conformance checker after it has actually run a check.
// Private keys are supplied by the caller's approved signer, never by agents.
func Issue(context Context, status, runID string, at time.Time, keyID string, key ed25519.PrivateKey) (Evidence, error) {
	if !validContext(context) {
		return Evidence{}, errors.New("invalid conformance context")
	}
	raw, err := json.Marshal(context)
	if err != nil {
		return Evidence{}, err
	}
	outcome := "incomplete"
	if status == "succeeded" {
		outcome = "passed"
	}
	if status == "failed" {
		outcome = "failed"
	}
	payload, err := receipts.Generate(receipts.Receipt{
		Schema: receipts.ReceiptSchema, Interface: receipts.TerminalV1,
		Producer: receipts.Producer{ID: ProducerID, Version: "1.0.0"},
		Run:      receipts.Run{ID: runID, Action: "hap.conformance." + context.Check},
		Subject:  receipts.Subject{Kind: "hap-agent-definition", Digest: context.DefinitionDigest, ConfigurationDigest: receipts.Digest(raw)},
		Terminal: receipts.Terminal{Status: status, Outcome: outcome},
		Effect:   receipts.Effect{Class: "none", MutationStarted: "no"}, IssuedAt: at.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return Evidence{}, err
	}
	envelope, err := receipts.Sign(payload, keyID, key)
	return Evidence{Context: raw, Receipt: envelope}, err
}

func Verify(raw []byte, artifact string, evidence []Evidence, keys map[string]Verifier) Result {
	result := Result{State: "unverified", Reason: "evidence_missing", ArtifactDigest: artifact, Checks: []Check{}}
	if len(raw) == 0 {
		result.Reason = "definition_missing"
		return result
	}
	result.DefinitionDigest = receipts.Digest(raw)
	public, err := definition.Parse(raw)
	if err != nil {
		result.State = "not_compatible"
		result.Reason = "invalid_definition"
		return result
	}
	result.ProtocolVersion = public.ProtocolVersion
	result.Definition = &public
	if !digestPattern.MatchString(artifact) {
		result.Reason = "build_identity_missing"
		return result
	}
	if len(evidence) > 256 {
		result.Reason = "evidence_limit"
		return result
	}
	latest := map[string]Check{}
	for _, item := range evidence {
		var context Context
		if len(item.Context) > 16384 || strictJSON(item.Context, &context) != nil || !validContext(context) {
			result.Reason = "invalid_evidence"
			continue
		}
		if context.DefinitionDigest != result.DefinitionDigest || context.ArtifactDigest != artifact || context.AgentName != public.Name || context.AgentVersion != public.Version || context.ProtocolVersion != public.ProtocolVersion {
			result.Reason = "subject_mismatch"
			continue
		}
		if context.Check == "interface" {
			found := false
			for _, iface := range public.Interfaces {
				if iface.ID == context.InterfaceID {
					found = true
				}
			}
			if !found {
				result.Reason = "interface_mismatch"
				continue
			}
		}
		verified, err := receipts.Verify(item.Receipt, receipts.TerminalV1, func(id string) (ed25519.PublicKey, error) {
			key, ok := keys[id]
			if !ok {
				return nil, errors.New("untrusted verifier")
			}
			return key.PublicKey, nil
		})
		if err != nil {
			result.Reason = "receipt_unverified"
			continue
		}
		r := verified.Receipt
		trusted := true
		for _, id := range verified.KeyIDs {
			if keys[id].ProducerID != r.Producer.ID {
				trusted = false
			}
		}
		if !trusted || r.Producer.ID != ProducerID || r.Run.Action != "hap.conformance."+context.Check || r.Subject.Kind != "hap-agent-definition" || r.Subject.Digest != context.DefinitionDigest || r.Subject.ConfigurationDigest != receipts.Digest(item.Context) || r.Effect.Class != "none" || r.Effect.MutationStarted != "no" {
			result.Reason = "receipt_mismatch"
			continue
		}
		issued, err := time.Parse(time.RFC3339Nano, r.IssuedAt)
		if err != nil || issued.After(time.Now().Add(5*time.Minute)) {
			result.Reason = "invalid_check_time"
			continue
		}
		if (r.Terminal.Status == "succeeded" && r.Terminal.Outcome != "passed") || (r.Terminal.Status == "failed" && r.Terminal.Outcome != "failed") {
			result.Reason = "invalid_check_result"
			continue
		}
		check := Check{Check: context.Check, InterfaceID: context.InterfaceID, Status: r.Terminal.Status, ReceiptDigest: verified.PayloadDigest, KeyIDs: verified.KeyIDs, Producer: r.Producer.ID, IssuedAt: r.IssuedAt}
		id := context.Check + ":" + context.InterfaceID
		old, ok := latest[id]
		oldTime, _ := time.Parse(time.RFC3339Nano, old.IssuedAt)
		// Equal-time conflicting checks fail closed instead of depending on input order.
		if ok && oldTime.Equal(issued) && old.Status != check.Status {
			check.Status = "unknown"
		}
		if !ok || !issued.Before(oldTime) {
			latest[id] = check
		}
	}
	descriptorPassed, interfacePassed, failed := false, false, false
	for _, check := range latest {
		result.Checks = append(result.Checks, check)
		if check.Check == "descriptor" {
			descriptorPassed = check.Status == "succeeded"
		}
		if check.Check == "interface" && check.Status == "succeeded" {
			interfacePassed = true
		}
		if check.Status == "failed" {
			failed = true
		}
	}
	sort.Slice(result.Checks, func(i, j int) bool {
		return result.Checks[i].Check+result.Checks[i].InterfaceID < result.Checks[j].Check+result.Checks[j].InterfaceID
	})
	if descriptorPassed && interfacePassed {
		result.State = "compatible"
		result.Reason = "verified_conformance"
	} else if failed {
		result.State = "not_compatible"
		result.Reason = "conformance_failed"
	}
	return result
}

func validContext(c Context) bool {
	return c.Schema == ContextSchema && c.Suite == SuiteID && c.ProtocolVersion == "0.2" && c.AgentName != "" && c.AgentVersion != "" && digestPattern.MatchString(c.DefinitionDigest) && digestPattern.MatchString(c.ArtifactDigest) && digestPattern.MatchString(c.ScenarioDigest) && ((c.Check == "descriptor" && c.InterfaceID == "") || (c.Check == "interface" && c.InterfaceID != ""))
}
func strictJSON(raw []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}
