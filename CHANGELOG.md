# Changelog

## 0.2.0-rc.2

- Run interface conformance against the agent command or endpoint declared in
  `hap.yaml`.
- Add runtime endpoint and scenario overrides for local conformance testing.
- Require HAP 0.2 handshake acknowledgement and terminal-result correlation.
- Support both descriptor-relative and `PATH`-resolved process commands.

## 0.2.0-rc.1

- Replace the 0.1 proof-of-concept manifest with a strict HAP agent descriptor.
- Add process/JSONL, HTTP, WebSocket, and HAP-declared A2A 1.0 interfaces.
- Define canonical handshake, Task, Event, Result, and cancellation messages.
- Add measured-or-unavailable usage, actual cost, and W3C trace context.
- Add strict extension, credential, capability, and lifecycle semantics.
- Add descriptor, message, and cross-interface conformance fixtures.

This release intentionally provides no 0.1 compatibility layer.
