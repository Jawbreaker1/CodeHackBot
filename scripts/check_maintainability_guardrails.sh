#!/usr/bin/env bash
set -euo pipefail

ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"

fail=0

check_max_lines() {
  local file="$1"
  local max="$2"
  if [[ ! -f "$file" ]]; then
    echo "guardrail: missing file $file"
    fail=1
    return
  fi
  local lines
  lines=$(wc -l < "$file")
  if (( lines > max )); then
    echo "guardrail: $file is $lines lines (max $max)"
    fail=1
  else
    echo "guardrail: $file is $lines lines (max $max)"
  fi
}

# Sprint 32 targeted file caps.
check_max_lines "internal/assist/assistant.go" 260
check_max_lines "internal/cli/assist_state.go" 120
check_max_lines "internal/orchestrator/worker_runtime_assist.go" 140
check_max_lines "cmd/birdhackbot-orchestrator/tui_render.go" 360

# Global cap for non-test Go files (maintainability safety net).
while IFS= read -r file; do
  lines=$(wc -l < "$file")
  if (( lines > 1000 )); then
    echo "guardrail: $file is $lines lines (global max 1000 for non-test Go files)"
    fail=1
  fi
done < <(find cmd internal -type f -name '*.go' ! -name '*_test.go' | sort)

# Enforce test updates for refactors when a comparable base ref is available.
base_ref=""
if git rev-parse --verify origin/main >/dev/null 2>&1; then
  base_ref="$(git merge-base origin/main HEAD)"
fi
if [[ -z "$base_ref" ]]; then
  echo "guardrail: origin/main not available; skipping changed-test coupling check"
else
  while IFS= read -r file; do
    [[ -z "$file" ]] && continue
    case "$file" in
      internal/assist/*|internal/cli/*|internal/orchestrator/*|cmd/birdhackbot-orchestrator/*)
        pkg_dir="$(dirname "$file")"
        if ! git diff --name-only "$base_ref...HEAD" -- "$pkg_dir/*_test.go" | grep -q .; then
          echo "guardrail: refactor in $pkg_dir requires a test update in the same package"
          fail=1
        fi
        ;;
    esac
  done < <(git diff --name-only "$base_ref...HEAD" -- '*.go' | grep -Ev '_test\.go$' || true)
fi

if (( fail != 0 )); then
  echo "guardrail: maintainability checks failed"
  exit 1
fi

echo "guardrail: maintainability checks passed"
