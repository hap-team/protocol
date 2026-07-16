#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

forbidden='(^|[[:space:]])(mind|activation|cost_estimate|registry|reporting|conduct|debug):|^version:[[:space:]]*"0\.1"'
if rg -n "$forbidden" README.md docs examples --glob '*.md' --glob '*.yaml'; then
  echo "active documentation or examples contain removed HAP 0.1 descriptor fields" >&2
  exit 1
fi

interface_types=("")
for descriptor in examples/*/hap.yaml; do
  go run ./cmd/hap-conformance \
    --schema schemas/0.2/hap-agent.schema.json \
    --file "$descriptor" >/dev/null
  while IFS= read -r interface_type; do
    interface_types+=("$interface_type")
  done < <(awk '$1 == "type:" { print $2 }' "$descriptor")
done

for required in process http websocket a2a; do
  if ! printf '%s\n' "${interface_types[@]}" | rg -qx "$required"; then
    echo "examples do not include interface type: $required" >&2
    exit 1
  fi
done

if [[ -f SHA256SUMS ]]; then
  sha256sum --check SHA256SUMS
fi

go test ./...

first_archive="$(SOURCE_DATE_EPOCH=1784188800 scripts/build-release.sh 0.2.0-rc.1)"
first_checksum="$(sha256sum "$first_archive")"
second_archive="$(SOURCE_DATE_EPOCH=1784188800 scripts/build-release.sh 0.2.0-rc.1)"
second_checksum="$(sha256sum "$second_archive")"
if [[ "$first_checksum" != "$second_checksum" ]]; then
  echo "release archive is not reproducible" >&2
  exit 1
fi

tar -tzf "$first_archive" | rg -q 'hap-protocol-0.2.0-rc.1/schemas/0.2/message.schema.json'
tar -tzf "$first_archive" | rg -q 'hap-protocol-0.2.0-rc.1/conformance/scenarios/success.json'
