# HAP 0.2 health-checker example

This deterministic reference agent exposes the standard HAP HTTP resources:

- `POST /tasks`
- `GET /runs/{run_id}`
- `GET /runs/{run_id}/events`
- `POST /runs/{run_id}/cancel`
- `GET /health`

It accepts the `check-url` operation with `input.url`, then returns one
terminal Result with `healthy` or `unhealthy` outcome. Usage records one
measured HTTP request. Cost is omitted because it is unavailable.

Run locally:

```bash
HAP_LISTEN_ADDR=127.0.0.1:8080 go run .
```

Validate the descriptor from the protocol repository root:

```bash
go run ./cmd/hap-conformance \
  --schema schemas/0.2/hap-agent.schema.json \
  --file examples/health-checker/hap.yaml
```

Container packaging remains an optional CLI concern:

```bash
docker build -t health-checker:0.2.0 .
```
