#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
test -z "$(gofmt -l cmd internal tools)" || {
  echo 'Go formatting differs; run gofmt -w cmd internal tools' >&2
  exit 1
}
python3 ../tools/sync_cli_skill.py --check
python3 tools/generate.py --check
go test -race ./...
go vet ./...
go build -o bin/remote-agent-session ./cmd/remote-agent-session
