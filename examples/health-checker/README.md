# Health Checker -- HAP Example Agent

A reference HAP agent that monitors URL health. Demonstrates the full HAP event protocol with all mandatory and task events.

## What it does

- Reads `URL_TO_CHECK` from environment
- Checks the URL every 30 seconds via HTTP GET
- Emits JSONL events to stdout (HAP protocol)
- Handles SIGTERM gracefully

## Events emitted

| Event | When | Fields |
|-------|------|--------|
| `agent.started` | On boot | - |
| `agent.ready` | After initialization | - |
| `agent.heartbeat` | Every 10s | `status`, `uptime` |
| `task.started` | Before each check | `task_id`, `detail` |
| `task.completed` | Check succeeded | `task_id`, `detail` |
| `task.failed` | Check failed | `task_id`, `detail` |
| `agent.stopped` | On SIGTERM | - |

## Run with HAP

```bash
# From this directory:
hap dev
```

## Run standalone

```bash
URL_TO_CHECK=https://example.com go run .
```

## Build container

```bash
hap build
# Or directly:
docker build -t health-checker:0.1.0 .
```
