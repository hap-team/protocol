# Receipt-bound HAP compatibility

`definition.Parse` validates the embedded HAP 0.2 schema and returns only public
identifiers. `compatibility.Verify` performs offline verification with configured
Ed25519 public keys. It requires a descriptor pass and at least one pass for a
declared interface, following the HAP published-agent rule.

The Receipts envelope and terminal payload are unchanged. Subject digest binds
exact definition bytes; configuration digest binds exact conformance-context
bytes. The context includes identity/version, HAP version, definition and build
digests, suite, check kind, interface and scenario digest. Evidence encodes
context bytes in base64, preserving them across JSON serialization. Trust is
supplied by the consumer, never by the agent.

The checker supports `--evidence-output`, `--artifact-digest`, `--signer-key-id`
and `--signer-key-env` on descriptor/interface checks. It signs only after the
check executes. `hap-conformance job --job <source-owned-job.json>` supports a
declared fixture verification command, including authentication, replay and
agent-specific result-schema tests. The job format is
`dev.hap.conformance-job/v1` with `definition`, `interface`, `workingDirectory`
and an argv array `command`, all owned by the agent repository. Relative paths
resolve from the job file. The signer is not forwarded to the fixture process.

The artifact digest must identify the exact build under test. A trusted CI or
Runner verification job supplies it; an agent's own success claim does not.
Registration never invokes the checker. Package publication, production key
provisioning and deployment are outside these libraries. Tests use ephemeral
keys and real local fixture commands.

Runner local commands use the `--artifact-digest-file-env`,
`--signer-key-id-file-env`, and `--signer-key-file-env` variants to resolve
approved file handles inside the checker. Those handles are not passed to the
fixture command or included in public evidence.
