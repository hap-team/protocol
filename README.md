# HAP — Human Agent Protocol

> An open **draft** standard (v0.1) for describing, publishing, running, and interacting with AI agents. Created by HAP Team, open for community use. Apache-2.0.

HAP defines the **public contract** of an AI agent: what it is, what it can do, how to call it, what it needs, what it costs, and how it reports what happened — so humans, apps, and other agents can discover, run, and trust it without reading its source.

HAP is not MCP, A2A, or a safety-control spec:

| | Answers |
|---|---|
| **MCP** | How does an agent reach tools? |
| **A2A** | How do agents talk to each other? |
| **ACS** | What controls run at runtime? |
| **HAP** | How does an agent publish itself so it can be discovered, run, and trusted? |

## What's here

- [`docs/protocol.md`](docs/protocol.md) — the protocol specification (v0.1 draft)
- [`schemas/hap-manifest.schema.json`](schemas/hap-manifest.schema.json) — JSON Schema for `hap.yaml`
- [`examples/`](examples/) — reference agent manifests

## The contract in five layers

1. **Identity** — name, version, author, license, tags
2. **Mind** — model family, supported providers, endpoint
3. **Interfaces** — how input arrives: `task`, `chat`, `human_feedback`
4. **Capabilities** — what it exposes to other agents (`a2a`) and the instructions it accepts
5. **Trust** — guardrails, cost estimate, credentials, reporting

## Status

v0.1 draft. The spec will change. Issues and RFC discussions welcome.

## License

Apache-2.0.
