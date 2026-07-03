#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

export GOCACHE="${GOCACHE:-$ROOT/.cache/go-build}"
mkdir -p "$GOCACHE"

run() {
  printf '+'
  printf ' %q' "$@"
  printf '\n'
  "$@"
}

check_gofmt() {
  mapfile -t go_files < <(git ls-files '*.go' ':(exclude)legacy/**')
  if ((${#go_files[@]} == 0)); then
    return
  fi

  local unformatted
  unformatted="$(gofmt -l "${go_files[@]}")"
  if [[ -n "$unformatted" ]]; then
    printf 'gofmt required for:\n%s\n' "$unformatted" >&2
    printf 'Run: gofmt -w <files>\n' >&2
    exit 1
  fi
}

check_no_tracked_runtime_artifacts() {
  local tracked
  tracked="$(git ls-files 'sessions/**' 'reports/**' 'artifacts/**' 'evidence/**' 'logs/**')"
  if [[ -n "$tracked" ]]; then
    printf 'runtime artifacts must not be tracked:\n%s\n' "$tracked" >&2
    exit 1
  fi
}

check_gofmt
check_no_tracked_runtime_artifacts

run ./scripts/architecture_guardrails.sh
run go vet ./...
run go test ./...
run go build -buildvcs=false -o /tmp/birdhackbot-ci ./cmd/birdhackbot
run go build -buildvcs=false -o /tmp/birdhackbot-orchestrator-ci ./cmd/birdhackbot-orchestrator
