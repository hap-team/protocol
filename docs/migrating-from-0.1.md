# Migrating from HAP 0.1

HAP 0.2 intentionally replaces the unused 0.1 proof of concept. There is no
compatibility parser, conversion mode, or dual-schema period.

Key changes:

- Replace the old top-level manifest version with `hap: "0.2"`.
- Define at least one entry in the `interfaces` array.
- Use Task, Event, Result, cancellation, usage, and trace envelopes.
- Move model choice, prompts, instructions, schedules, packaging, reporting,
  runtime conduct, debug configuration, and registry configuration into agent
  implementation or deployment configuration.
- Replace embedded secret names and values with logical credential IDs and
  declarative methods.
- Remove estimated or human-entered cost metadata. Report only measured
  runtime usage and actual cost when available.
- Model A2A as a declared interface rather than a separate top-level feature.

Current descriptors must validate against
`schemas/0.2/hap-agent.schema.json`. Historical 0.1 artifacts remain available
through repository history.
