package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"strings"
	"time"

	"github.com/hap-team/protocol/compatibility"
	"github.com/hap-team/protocol/definition"
	"github.com/hap-team/receipts/go/receipts"
)

type evidenceOptions struct {
	output, artifact, keyID, keyEnv           *string
	artifactFileEnv, keyIDFileEnv, keyFileEnv *string
}

func evidenceFlags(flags *flag.FlagSet) evidenceOptions {
	return evidenceOptions{
		flags.String("evidence-output", "", "optional signed conformance bundle output"),
		flags.String("artifact-digest", "", "sha256 digest of the exact checked build artifact"),
		flags.String("signer-key-id", "", "explicit verifier key ID"),
		flags.String("signer-key-env", "", "approved environment handle containing a base64 Ed25519 private key"),
		flags.String("artifact-digest-file-env", "", "Runner environment handle naming a file containing the checked build digest"),
		flags.String("signer-key-id-file-env", "", "Runner environment handle naming a file containing the verifier key ID"),
		flags.String("signer-key-file-env", "", "Runner environment handle naming a file containing the base64 signer key"),
	}
}

// Called only after descriptor validation and actual interface execution. No
// registration path uses this signer or executes conformance checks.
func (options evidenceOptions) write(raw, scenario []byte, iface, status string) error {
	if *options.output == "" {
		return nil
	}
	public, err := definition.Parse(raw)
	if err != nil {
		return errors.New("cannot attest an invalid descriptor")
	}
	artifact, err := resolveFileHandle(*options.artifact, *options.artifactFileEnv)
	if err != nil {
		return err
	}
	keyID, err := resolveFileHandle(*options.keyID, *options.keyIDFileEnv)
	if err != nil {
		return err
	}
	keyValue, err := resolveFileHandle(os.Getenv(*options.keyEnv), *options.keyFileEnv)
	if err != nil {
		return err
	}
	key, err := base64.StdEncoding.DecodeString(keyValue)
	if err != nil || len(key) != ed25519.PrivateKeySize || keyID == "" {
		return errors.New("an explicit conformance signer is required")
	}
	at := time.Now().UTC()
	c := compatibility.Context{Schema: compatibility.ContextSchema, AgentName: public.Name, AgentVersion: public.Version, ProtocolVersion: public.ProtocolVersion, DefinitionDigest: receipts.Digest(raw), ArtifactDigest: artifact, Check: "descriptor", Suite: compatibility.SuiteID, ScenarioDigest: receipts.Digest([]byte("hap-0.2-descriptor-schema"))}
	descriptor, err := compatibility.Issue(c, "succeeded", "descriptor-"+at.Format("20060102T150405.000000000"), at, keyID, ed25519.PrivateKey(key))
	if err != nil {
		return err
	}
	evidence := []compatibility.Evidence{descriptor}
	if iface != "" {
		c.Check = "interface"
		c.InterfaceID = iface
		c.ScenarioDigest = receipts.Digest(scenario)
		check, err := compatibility.Issue(c, status, "interface-"+at.Format("20060102T150405.000000000"), at, keyID, ed25519.PrivateKey(key))
		if err != nil {
			return err
		}
		evidence = append(evidence, check)
	}
	bytes, err := json.Marshal(evidence)
	if err != nil {
		return err
	}
	// Output is public evidence. The private signer is never written.
	return os.WriteFile(*options.output, append(bytes, '\n'), 0644)
}

// Runner local/command passes approved file handles, never credential values.
// Resolving them is confined to the signer; no handle or value enters the job.
func resolveFileHandle(value, name string) (string, error) {
	if name == "" {
		return strings.TrimSpace(value), nil
	}
	file, err := os.Open(os.Getenv(name))
	if err != nil {
		return "", errors.New("conformance credential handle unavailable")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(raw) > 4096 {
		return "", errors.New("conformance credential handle invalid")
	}
	return strings.TrimSpace(string(raw)), nil
}
