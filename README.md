# Human Agent Protocol

Human Agent Protocol (HAP) 0.2 is an open contract for describing and invoking
software agents without prescribing how they are implemented.

A HAP agent publishes:

- identity and semantic version;
- one or more process/JSONL, HTTP, WebSocket, or A2A interfaces;
- optional task and result contracts;
- optional informational capabilities;
- logical credential requirements without secret values; and
- observable lifecycle and cancellation behavior.

HAP does not require an LLM, model provider, SDK, container, orchestrator,
LiteLLM, prompt format, scheduler, board, or workflow engine. An agent may be
deterministic, human-backed, LLM-backed, locally executed, or remotely hosted.

## Repository contents

- [Protocol specification](docs/protocol.md)
- [Agent descriptor schema](schemas/0.2/hap-agent.schema.json)
- [Canonical message schema](schemas/0.2/message.schema.json)
- [Conformance fixtures](conformance/)
- [Migration notes](docs/migrating-from-0.1.md)
- [Release policy](docs/release-policy.md)

Validate a descriptor offline:

```bash
go run ./cmd/hap-conformance \
  --schema schemas/0.2/hap-agent.schema.json \
  --file examples/minimal/hap.yaml
```

Test a declared interface:

```bash
go run ./cmd/hap-conformance interface \
  --descriptor conformance/testagent/hap.yaml \
  --interface local
```

## Related concepts

- A HAP Agent is independently described and published using this protocol.
- A HAP Team references agents and gives them team-specific responsibilities.
- A HAP Flow defines movement between agents, runbooks, boards, APIs, states,
  and approval gates.
- A HAP Orchestrator loads Team and Flow configuration, resolves plugins, and
  executes the flow.

Those related formats are separate specifications. HAP Agent conformance does
not depend on them.

Apache-2.0 licensed.
