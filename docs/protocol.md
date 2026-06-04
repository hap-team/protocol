# HAP Protocol Specification

Version: 0.1 (draft)

---

## 1. Overview

The Human Agent Protocol (HAP) is an open **draft** standard for packaging, distributing, and running autonomous AI agents. It defines:

- A **manifest format** (`hap.yaml`) that declares an agent's identity, capabilities, dependencies, and operational parameters.
- An **event protocol** (JSONL over stdout) that provides a uniform interface for observing agent lifecycle and task execution.
- A **credentials model** for declaring and injecting secrets and configuration.
- A **container packaging convention** for producing portable OCI images.

HAP defines interfaces, not implementations. Any language or runtime can produce a conforming agent. The protocol makes no assumptions about whether an agent uses an LLM, rule-based logic, or any other approach -- only that it speaks the event protocol and ships with a valid manifest.

---

## 2. Manifest Format (hap.yaml)

Every HAP agent must include a `hap.yaml` file at the root of its project directory. The manifest is validated against the JSON Schema at `schemas/hap-manifest.schema.json`.

Two fields are required at the top level: `version` and `agent`. All other sections are optional.

### 2.1 version (required)

The manifest specification version. Currently `"0.1"`.

```yaml
version: "0.1"
```

### 2.2 agent (required)

Agent identity metadata. The `name` and `version` fields are required.

| Field         | Type     | Required | Description                                      |
|---------------|----------|----------|--------------------------------------------------|
| `name`        | string   | Yes      | Agent name. Must match `^[a-z0-9][a-z0-9-]*$`.  |
| `version`     | string   | Yes      | Agent version (semver recommended).              |
| `description` | string   | No       | Short description of what the agent does.        |
| `author`      | string   | No       | Author or organization.                          |
| `license`     | string   | No       | License identifier (e.g. "MIT", "commercial").   |
| `tags`        | string[] | No       | Searchable tags for discovery.                   |

```yaml
agent:
  name: "jira-project-manager"
  version: "1.2.0"
  description: "Manages Jira projects -- triages issues, assigns work, writes sprint reports"
  author: "acme-agents"
  license: "commercial"
  tags: ["project-management", "jira", "agile"]
```

### 2.3 mind

LLM configuration. Declares which models the agent supports and how to reach them.

| Field       | Type           | Description                              |
|-------------|----------------|------------------------------------------|
| `default`   | string         | Default model name.                      |
| `supported` | string[]       | Supported model patterns (glob allowed). |
| `endpoint`  | string or null | Custom LLM endpoint URL, or null.        |

```yaml
mind:
  default: "claude-sonnet-4-6"
  supported: ["claude-*", "gpt-4o", "bedrock/*", "ollama/*"]
  endpoint: null
```

Agents that do not use an LLM omit this section entirely.

### 2.4 credentials

Declares credentials the agent requires or can optionally use. Each credential is identified by name and injected as an environment variable at runtime.

| Field      | Type         | Description                                      |
|------------|--------------|--------------------------------------------------|
| `required` | Credential[] | Credentials that must be present for the agent.  |
| `optional` | Credential[] | Credentials the agent can use if available.      |

Each Credential object has:

| Field         | Type   | Required | Description                        |
|---------------|--------|----------|------------------------------------|
| `name`        | string | Yes      | Environment variable name.         |
| `description` | string | No       | Human-readable purpose.            |

```yaml
credentials:
  required:
    - name: JIRA_API_TOKEN
      description: "Jira API token with read/write access"
    - name: LLM_API_KEY
      description: "API key for the LLM provider"
  optional:
    - name: SLACK_WEBHOOK_URL
      description: "For sending sprint summaries to Slack"
```

### 2.5 instructions

Configures how runtime instructions (prompts, rules, style guides) are loaded into the agent.

| Field           | Type     | Description                                            |
|-----------------|----------|--------------------------------------------------------|
| `path`          | string   | Path to the instructions directory inside the container.|
| `format`        | string   | File format: `"markdown"` or `"text"`.                 |
| `hot_reload`    | boolean  | Whether instructions can be reloaded without restart.  |
| `supported`     | string[] | Instruction categories the agent accepts.              |
| `not_supported` | string[] | Instruction categories the agent does not accept.      |

```yaml
instructions:
  path: "/agent/instructions/"
  format: "markdown"
  hot_reload: true
  supported:
    - "tone_and_style"
    - "domain_rules"
    - "forbidden_actions"
  not_supported:
    - "custom_tool_definitions"
```

### 2.6 activation

Defines how the agent is triggered. See Section 5 for the full description of each mode.

| Field      | Type   | Description                                   |
|------------|--------|-----------------------------------------------|
| `mode`     | string | One of: `manual`, `continuous`, `scheduled`, `event`, `task`. |
| `schedule` | string | Cron expression. Required when mode is `scheduled`. |

```yaml
activation:
  mode: "continuous"
```

### 2.7 interfaces

Communication interfaces the agent supports for receiving input.

**task** -- structured task input:

| Field    | Type   | Description                      |
|----------|--------|----------------------------------|
| `format` | string | Input format (e.g. `"json"`).    |
| `schema` | string | Path to a JSON Schema for input, resolved inside the agent's own image (not a file in this repo). |

**chat** -- conversational input:

| Field      | Type   | Description                          |
|------------|--------|--------------------------------------|
| `protocol` | string | Protocol type (e.g. `"websocket"`).  |

**human_feedback** -- human-in-the-loop feedback:

| Field   | Type    | Description                              |
|---------|---------|------------------------------------------|
| `async` | boolean | Whether feedback is delivered asynchronously. |

```yaml
interfaces:
  task:
    format: "json"
    schema: "schemas/task.json"
  chat:
    protocol: "websocket"
  human_feedback:
    async: true
```

### 2.8 a2a

Agent-to-agent communication configuration.

| Field          | Type         | Description                                    |
|----------------|--------------|------------------------------------------------|
| `enabled`      | boolean      | Whether agent-to-agent communication is on.    |
| `capabilities` | Capability[] | Operations this agent exposes to other agents. |
| `accepts`      | string[]     | Message types the agent can receive.           |

Each Capability has:

| Field         | Type   | Description                               |
|---------------|--------|-------------------------------------------|
| `name`        | string | Capability identifier.                    |
| `input`       | string | Expected input format.                    |
| `description` | string | What the capability does.                 |

```yaml
a2a:
  enabled: true
  capabilities:
    - name: "triage-issue"
      input: "json"
      description: "Send me a Jira issue, I'll classify and route it"
  accepts: ["task", "query", "event"]
```

### 2.9 reporting

Observability and reporting output configuration.

| Field      | Type      | Description                              |
|------------|-----------|------------------------------------------|
| `default`  | string    | Default reporting target (e.g. `"stdout"`). |
| `adapters` | Adapter[] | Additional reporting adapters.           |

Each Adapter has:

| Field        | Type   | Description                                     |
|--------------|--------|-------------------------------------------------|
| `name`       | string | Adapter name (e.g. `"datadog"`).                |
| `config_env` | string | Environment variable holding adapter config.    |

```yaml
reporting:
  default: "stdout"
  adapters:
    - name: "datadog"
      config_env: "DD_API_KEY"
```

### 2.10 conduct

Safety guardrails and failure handling policy.

| Field              | Type     | Description                                       |
|--------------------|----------|---------------------------------------------------|
| `guardrails`       | string[] | Human-readable descriptions of safety boundaries. |
| `failure_protocol` | string   | Behavior on failure (e.g. `"report_and_pause"`).  |

```yaml
conduct:
  guardrails:
    - "Will not delete Jira projects"
    - "Will not modify user permissions"
    - "Max 100 API calls per minute"
  failure_protocol: "report_and_pause"
```

### 2.11 debug

Debug mode configuration.

| Field       | Type    | Description                                  |
|-------------|---------|----------------------------------------------|
| `enabled`   | boolean | Whether debug mode is available.             |
| `interface` | string  | Debug interface type (e.g. `"http"`).        |
| `auth`      | boolean | Whether the debug interface requires auth.   |

### 2.12 cost_estimate

Cost metadata for transparency.

| Field                    | Type     | Description                               |
|--------------------------|----------|-------------------------------------------|
| `llm_cost_per_task`      | string   | Estimated LLM cost per task.              |
| `typical_monthly_llm`    | string   | Typical monthly LLM spend.               |
| `required_subscriptions` | string[] | External subscriptions the agent needs.   |

### 2.13 registry

Container registry configuration used when publishing the agent's image. Optional; consumed by packaging/publishing tooling (see Section 7.2).

| Field       | Type   | Description                                  |
|-------------|--------|----------------------------------------------|
| `url`       | string | Registry URL (e.g. `ghcr.io`, `docker.io`).  |
| `namespace` | string | Registry namespace or organization.          |

```yaml
registry:
  url: "ghcr.io"
  namespace: "hap-team"
```

---

## 3. Event Protocol

### 3.1 Format

Agents emit events as JSONL (newline-delimited JSON) to **stdout**. Each line is one independently parseable JSON object. Debug logs and free-form output go to **stderr**.

This separation is a hard requirement: stdout must contain only valid JSONL event lines. Any non-JSON output on stdout is a protocol violation.

### 3.2 Required Fields

Every event object must contain these two fields:

| Field   | Type   | Description                                      |
|---------|--------|--------------------------------------------------|
| `event` | string | The event type identifier.                       |
| `ts`    | string | ISO 8601 / RFC 3339 timestamp in UTC.            |

Additional fields depend on the event type and are described below.

### 3.3 Event Types

The protocol defines nine event types in two categories.

**Agent lifecycle events:**

| Type               | Description                                   |
|--------------------|-----------------------------------------------|
| `agent.started`    | Agent process has started.                    |
| `agent.ready`      | Agent is initialized and ready to work.       |
| `agent.heartbeat`  | Periodic health signal.                       |
| `agent.stopped`    | Agent is shutting down gracefully.            |
| `agent.error`      | Agent encountered a non-task-specific error.  |

**Task lifecycle events:**

| Type              | Description                                    |
|-------------------|------------------------------------------------|
| `task.started`    | A discrete unit of work has begun.             |
| `task.progress`   | Reports progress on an in-flight task.         |
| `task.completed`  | A task finished successfully.                  |
| `task.failed`     | A task finished with an error.                 |

### 3.4 Event Examples

**agent.started** -- emitted once at process start:

```json
{"event":"agent.started","ts":"2026-03-06T09:00:01Z"}
```

**agent.ready** -- emitted once when the agent is fully initialized:

```json
{"event":"agent.ready","ts":"2026-03-06T09:00:01Z"}
```

**agent.heartbeat** -- emitted periodically to signal liveness:

```json
{"event":"agent.heartbeat","ts":"2026-03-06T09:00:12Z","status":"healthy","uptime":11}
```

Optional fields: `status` (string), `uptime` (integer, seconds since start).

**agent.stopped** -- emitted before graceful exit (e.g. on SIGTERM):

```json
{"event":"agent.stopped","ts":"2026-03-06T09:05:00Z"}
```

**agent.error** -- emitted on non-task errors:

```json
{"event":"agent.error","ts":"2026-03-06T09:03:00Z","detail":"database connection lost"}
```

Optional fields: `detail` (string).

**task.started** -- emitted when a unit of work begins:

```json
{"event":"task.started","ts":"2026-03-06T09:00:03Z","task_id":"t-001","detail":"Checking https://example.com"}
```

Optional fields: `task_id` (string), `detail` (string).

**task.completed** -- emitted when a task finishes successfully:

```json
{"event":"task.completed","ts":"2026-03-06T09:00:03Z","task_id":"t-001","detail":"status=200 time=142ms"}
```

Optional fields: `task_id` (string), `detail` (string).

**task.progress** -- emitted to report progress on an in-flight task:

```json
{"event":"task.progress","ts":"2026-03-06T09:00:03Z","task_id":"t-001","message":"Processing items","progress":0.75}
```

Optional fields: `task_id` (string), `message` (string), `progress` (number, 0-1).

**task.failed** -- emitted when a task finishes with an error:

```json
{"event":"task.failed","ts":"2026-03-06T09:00:04Z","task_id":"t-001","detail":"connection refused"}
```

Optional fields: `task_id` (string), `detail` (string).

### 3.5 Lifecycle Diagram

Agent lifecycle:

```
agent.started --> agent.ready --> [agent.heartbeat]* --> agent.stopped
                                         |
                                    agent.error (may occur at any point)
```

Task lifecycle (may occur zero or more times while the agent is ready):

```
task.started --> [task.progress]* --> task.completed
                                 \-> task.failed
```

### 3.6 Conventions

- Agents SHOULD emit `agent.started` immediately on process start.
- Agents SHOULD emit `agent.ready` once initialization is complete.
- Agents in `continuous` mode SHOULD emit `agent.heartbeat` at a regular interval (recommended: 10-30 seconds).
- Agents MUST emit `agent.stopped` before graceful exit. The agent should handle SIGTERM and use it as the signal to shut down.
- Task events SHOULD include a `task_id` to correlate `task.started` with its `task.completed` or `task.failed`.
- Additional fields beyond those documented here are permitted. Consumers MUST ignore fields they do not recognize.

---

## 4. Credentials Model

Credentials are declared in the `credentials` section of `hap.yaml` and injected into the agent process as environment variables.

### 4.1 Declaration

Credentials are split into two lists:

- **required**: The agent cannot function without these. The runtime MUST refuse to start the agent if any required credential is missing.
- **optional**: The agent can function without these but may offer reduced functionality.

Each credential's `name` field corresponds exactly to the environment variable name the agent reads at runtime.

### 4.2 Injection

During development (`hap dev`), credentials are loaded from a `.env` file in the project directory. In production, credentials are injected as environment variables by the container runtime, orchestrator, or secret manager.

The agent binary reads credentials via standard environment variable access (e.g. `os.Getenv` in Go, `process.env` in Node.js). HAP does not define a secrets API -- environment variables are the universal interface.

### 4.3 Security

- `.env` files MUST NOT be committed to version control. The scaffolded `.gitignore` excludes `.env` by default.
- Container images MUST NOT embed credentials. Credentials are always injected at runtime.

---

## 5. Activation Modes

The `activation.mode` field declares how the agent is triggered. The runtime uses this to determine process lifecycle.

| Mode         | Description                                                                |
|--------------|----------------------------------------------------------------------------|
| `manual`     | Started explicitly by a human operator. Runs once and exits.               |
| `continuous` | Starts and runs indefinitely. Emits heartbeats. Stopped via SIGTERM.       |
| `scheduled`  | Triggered on a cron schedule. The `activation.schedule` field is required.  |
| `event`      | Triggered by an external event (webhook, message queue, etc.).             |
| `task`       | Triggered by a task submission. Processes the task and exits.              |

### 5.1 Scheduled Mode

When `mode` is `"scheduled"`, the `schedule` field must contain a valid cron expression:

```yaml
activation:
  mode: "scheduled"
  schedule: "0 */6 * * *"  # every 6 hours
```

### 5.2 Continuous Mode

Continuous agents run as long-lived processes. They MUST emit periodic `agent.heartbeat` events. The runtime uses missing heartbeats to detect unhealthy agents.

### 5.3 Task Mode

Task-mode agents receive input (via stdin, HTTP, or another mechanism defined by `interfaces`), process it, emit task events, and exit. They are not expected to emit heartbeats.

---

## 6. Container Packaging

HAP agents are distributed as OCI-compliant container images.

### 6.1 Image Layout

A conforming HAP image must satisfy these requirements:

- The `hap.yaml` manifest MUST be present at `/hap.yaml` inside the image.
- The agent binary (or interpreter entrypoint) MUST be the container's `ENTRYPOINT`.
- The image SHOULD be as small as practical. Multi-stage builds are recommended.

### 6.2 Reference Dockerfile

The following Dockerfile illustrates the packaging convention for a Go agent:

```dockerfile
FROM golang:1.22-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /agent .

FROM alpine:3.19
COPY --from=build /agent /agent
COPY hap.yaml /hap.yaml
ENTRYPOINT ["/agent"]
```

### 6.3 Image Tagging

Images are tagged as `<agent-name>:<agent-version>`, derived from the `agent.name` and `agent.version` fields in `hap.yaml`:

```
health-checker:0.1.0
jira-project-manager:1.2.0
```

### 6.4 Runtime Contract

When a container is started:

1. Required credentials are injected as environment variables.
2. The entrypoint process starts and emits `agent.started` on stdout.
3. Stdout is captured as the JSONL event stream.
4. Stderr is captured as debug log output.
5. To stop the agent, the runtime sends SIGTERM. The agent emits `agent.stopped` and exits.

---

## 7. Versioning

### 7.1 Manifest Version

The top-level `version` field identifies the manifest specification version. This document defines version `"0.1"` (draft). Future revisions will increment this field. Runtimes SHOULD reject manifests with unrecognized version values.

### 7.2 Agent Version

The `agent.version` field tracks the agent's own release version. Semantic versioning (MAJOR.MINOR.PATCH) is recommended but not enforced by the schema. This version is used for image tagging, registry publishing, and upgrade decisions.

### 7.3 Compatibility

- Consumers of `hap.yaml` MUST ignore unknown top-level sections to allow forward compatibility.
- Event consumers MUST ignore unrecognized fields in JSONL events.
- New event types may be added in future protocol versions. Consumers SHOULD treat unrecognized event types as informational rather than errors.
