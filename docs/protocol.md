# HAP Agent Protocol 0.2

Status: release candidate specification.

## Scope

HAP 0.2 defines an agent descriptor and transport-neutral invocation
semantics. It answers:

- What agent is this?
- Which invocation interfaces does it expose?
- Which task and result contracts does it publish?
- Which logical credentials does it require?
- What lifecycle and cancellation behavior can callers observe?
- Which measured usage and actual cost did a run report?

HAP does not define teams, workflows, runbooks, board state machines,
scheduling, model selection, prompts, packaging, deployment, or marketplace
policy.

## Conformance principles

1. Core objects are strict. Unknown unnamespaced fields are invalid.
2. Extensions use keys matching `x-[a-z0-9][a-z0-9.-]*`.
3. The public invocation term is `interface`.
4. Task, Event, Result, cancellation, usage, and trace semantics are the same
   across every interface.
5. Cost is measured or unavailable. HAP does not carry estimates.
6. Credentials are logical requirements. Descriptors never contain values.

## Agent descriptor

A descriptor is YAML or JSON matching
`schemas/0.2/hap-agent.schema.json`.

```yaml
hap: "0.2"
agent:
  name: researcher
  version: 1.0.0
contracts:
  task:
    schema: schemas/research-task.json
  result:
    schema: schemas/research-result.json
capabilities:
  - id: cited-research
    description: Produces findings with cited sources.
credentials:
  - id: search-api
    required: true
    methods: [bearer, workload_identity]
    description: Credential for the configured search provider.
lifecycle:
  health: supported
  cancellation: best_effort
  graceful_shutdown: supported
  max_concurrent_runs: 4
interfaces:
  - id: local
    type: process
    protocol: jsonl
    command: ["./researcher", "serve"]
```

Required fields are `hap`, `agent.name`, `agent.version`, and at least one
interface. Agent versions use Semantic Versioning. Relative contract schema
references resolve from the descriptor location. Ordinary offline validation
does not fetch arbitrary remote schemas.

Capabilities are informational publisher assertions. They are not permission
grants, routing rules, or substitutes for task and result validation.

Credential methods standardized in 0.2 are bearer, basic, OAuth2 client
credentials, mTLS, and workload identity. Deployment configuration maps
credential IDs to a secret provider.

Lifecycle metadata may declare a positive startup timeout, health support,
cancellation as supported, best effort, or unsupported, graceful shutdown
support, and positive maximum concurrent runs. Scheduling and retries belong
to the caller or a Flow.

## Interfaces

Interface order is preference order unless project configuration pins a
descriptor-local interface ID.

### Process/JSONL

```yaml
- id: local
  type: process
  protocol: jsonl
  command: ["./agent", "serve"]
```

The command starts without a shell. Standard input and output contain one
compact JSON message per line. Standard error is diagnostic output. The caller
and agent perform a version handshake before task delivery.

### HTTP

```yaml
- id: api
  type: http
  endpoint: https://agents.example.com/researcher
  events: both
```

HTTP resources are `POST /tasks`, `GET /runs/{run_id}`,
`GET /runs/{run_id}/events`, `POST /runs/{run_id}/cancel`, and `GET /health`.
Event delivery is polling, streaming, or both; polling is the default.

### WebSocket

```yaml
- id: stream
  type: websocket
  endpoint: wss://agents.example.com/researcher
```

WebSocket uses the same handshake and framed messages as process/JSONL.
Reconnect uses the run ID and last observed event sequence. If resumption is
not possible, the agent returns an explicit non-resumable error.

### A2A 1.0

```yaml
- id: a2a
  type: a2a
  endpoint: https://agents.example.com/a2a
  version: "1.0"
```

The adapter maps HAP messages and semantics to A2A 1.0. The HAP descriptor
remains authoritative for HAP identity and contracts. A native A2A agent
without a HAP descriptor can still be used by an orchestrator plugin, but is
not thereby HAP-conforming.

## Canonical messages

Every framed message has a `type` discriminator and validates against
`schemas/0.2/message.schema.json`.

### Task

A Task requires protocol version, caller-generated task ID, globally unique
run ID within the caller domain, operation, input, and context. An optional
deadline is RFC 3339. Board- or vendor-specific identifiers belong in context
or namespaced extensions, not core fields.

### Orchestration rules and skills

Team responsibilities, rules, and skill references are orchestration context,
not HAP 0.2 descriptor fields. An orchestrator adapter may map resolved rule
text and requested skills into Task context or agent-specific task input when
the declared task contract supports them.

HAP 0.2 does not define a universal executable skill package. Codex, Claude,
AgentCore, deterministic agents, and custom frameworks may use different skill
formats. The invoking adapter must validate support before invocation, and a
required unsupported skill must fail before work is claimed. An `x-*`
descriptor extension may advertise integration hints, but it does not change
HAP conformance or authorize automatic workflow routing.

### Event

An Event requires run ID, positive sequence, RFC 3339 time, name, and data.
Sequence numbers strictly increase per run. Events are non-terminal.

Standard event names are accepted, started, progress, output, usage, warning,
and cancellation requested. Namespaced event names are allowed.

### Result

Exactly one terminal Result may be produced per run. Status is succeeded,
failed, or cancelled. Outcome is an agent-defined, contract-documented value
that a Flow may map to an edge. A failed Result includes a structured error
with code, readable message, and retryability.

Transport loss before a Result means unknown outcome. It is not success or
failure.

### Cancellation

A cancellation request identifies a run and may include a reason.
Acknowledgement repeats the same run ID and reports whether the request was
accepted and whether the run was already terminal. Acknowledgement does not
claim an unsupported remote process stopped.

Cancellation is idempotent. Supported cancellation eventually produces a
terminal Result. Best-effort cancellation may report that work could not be
stopped. Unsupported cancellation allows the caller to stop waiting while the
remote outcome remains unknown.

## Usage and actual cost

Usage is optional. When present, measurements have a non-negative quantity and
explicit unit. Provider and model are optional informational dimensions.

```json
{
  "measurements": [
    {
      "name": "llm.input_tokens",
      "quantity": 1250,
      "unit": "token",
      "provider": "openai",
      "model": "gpt-example"
    }
  ],
  "cost": {
    "amount": "0.01234",
    "currency": "USD",
    "source": "gateway"
  }
}
```

Cost amount is a non-negative decimal string, never a JSON number. Omitted cost
means unavailable. Zero means measured zero. The final Result is authoritative
for cumulative usage when present. LiteLLM may supply measured usage inside an
implementation, but HAP neither requires nor configures it.

## Trace context

Task and Result may carry W3C `traceparent` and `tracestate`. Agents should
propagate valid context to downstream work. No tracing backend or SDK is
required.

## Security and limits

- Descriptors, URLs, messages, events, errors, and logs must not contain raw
  credentials.
- Process stdout contains protocol frames only.
- Conformance transports limit an individual frame or body to 4 MiB.
- HTTP redirects to a different host are rejected by default.
- Implementations validate task and result contracts at trust boundaries.

## Conformance levels

- Descriptor conformance: the descriptor validates against the released
  schema.
- Interface conformance: a declared interface preserves canonical message
  semantics and required transport behavior.
- Published-agent conformance: a released descriptor has at least one passing
  declared interface.

OCI images are an optional CLI packaging format, not a protocol conformance
layer.
