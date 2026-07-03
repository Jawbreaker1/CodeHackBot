#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

base="${GUARDRAIL_BASE_SHA:-${1:-}}"
if [[ -z "$base" || "$base" =~ ^0+$ ]] || ! git cat-file -e "$base^{commit}" 2>/dev/null; then
  if git rev-parse --verify origin/main >/dev/null 2>&1; then
    base="$(git merge-base origin/main HEAD)"
  elif git rev-parse --verify HEAD~1 >/dev/null 2>&1; then
    base="HEAD~1"
  else
    base=""
  fi
fi

diff_file="$(mktemp)"
trap 'rm -f "$diff_file"' EXIT

if [[ -n "$base" ]]; then
  git diff --unified=0 --no-ext-diff "$base" HEAD -- cmd internal >>"$diff_file"
fi
git diff --unified=0 --no-ext-diff -- cmd internal >>"$diff_file"

if [[ ! -s "$diff_file" ]]; then
  exit 0
fi

violations=0
current_file=""

while IFS= read -r line; do
  case "$line" in
    "+++ b/"*)
      current_file="${line#+++ b/}"
      ;;
    "+"*)
      [[ "$line" == "+++"* ]] && continue
      [[ "$current_file" =~ ^(cmd|internal)/.*\.go$ ]] || continue
      [[ "$current_file" == *_test.go ]] && continue

      added="${line#+}"

      if [[ "$added" == *"regexp.MustCompile("* ]]; then
        printf 'architecture guardrail: new regexp in production code requires explicit architecture review: %s\n  %s\n' "$current_file" "$added" >&2
        violations=$((violations + 1))
      fi

      if [[ "$added" == *"secret.zip"* || "$added" == *"192.168.50.1"* || "$added" == *"rockyou"* ]]; then
        printf 'architecture guardrail: canonical scenario literal added to production code: %s\n  %s\n' "$current_file" "$added" >&2
        violations=$((violations + 1))
      fi

      if [[ "$added" == *"strings.Contains("* ]] && [[ "$added" =~ (OutputSummary|OutputEvidence|Stdout|Stderr|stdout|stderr|combined) ]]; then
        printf 'architecture guardrail: new output-string contains check in production code requires explicit architecture review: %s\n  %s\n' "$current_file" "$added" >&2
        violations=$((violations + 1))
      fi
      ;;
  esac
done <"$diff_file"

if ((violations > 0)); then
  cat >&2 <<'EOF'

Architecture guardrail failed.
If this change is genuinely generic, document the architecture rationale and adjust the guardrail deliberately.
Do not bypass this by hiding parsing behind helper names.
EOF
  exit 1
fi
