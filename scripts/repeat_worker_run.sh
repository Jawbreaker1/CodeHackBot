#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/repeat_worker_run.sh --goal "..." --base-url URL --model MODEL [options]

Options:
  --goal TEXT           Worker goal to run.
  --base-url URL        LM Studio / OpenAI-compatible base URL.
  --model ID            Model id.
  --token-file PATH     Local bridge client token file (optional).
  --runs N              Number of repeated runs. Default: 3
  --max-steps N         Worker max steps per run. Default: 6
  --timeout SEC         Per-run timeout in seconds. Default: 120
  --workdir DIR         Directory to run the worker from. Default: repo root
  --allow-all           Pass --allow-all to birdhackbot.
  --inspect-context     Pass --inspect-context to birdhackbot.
  --binary PATH         Worker binary path. Default: ./birdhackbot
  --output-dir DIR      Batch output directory. Default: sessions/repeat/<timestamp>
EOF
}

goal=""
base_url=""
model=""
token_file=""
runs=3
max_steps=6
timeout_sec=120
workdir=""
allow_all=0
inspect_context=0
binary="./birdhackbot"
output_dir=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --goal) goal="${2:-}"; shift 2 ;;
    --base-url) base_url="${2:-}"; shift 2 ;;
    --model) model="${2:-}"; shift 2 ;;
    --token-file) token_file="${2:-}"; shift 2 ;;
    --runs) runs="${2:-}"; shift 2 ;;
    --max-steps) max_steps="${2:-}"; shift 2 ;;
    --timeout) timeout_sec="${2:-}"; shift 2 ;;
    --workdir) workdir="${2:-}"; shift 2 ;;
    --allow-all) allow_all=1; shift ;;
    --inspect-context) inspect_context=1; shift ;;
    --binary) binary="${2:-}"; shift 2 ;;
    --output-dir) output_dir="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

if [[ -z "$goal" || -z "$base_url" || -z "$model" ]]; then
  usage >&2
  exit 2
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ -z "$workdir" ]]; then
  workdir="$repo_root"
fi

if [[ -z "$output_dir" ]]; then
  output_dir="$repo_root/sessions/repeat/$(date -u +%Y%m%d-%H%M%S)"
fi
mkdir -p "$output_dir"
output_dir="$(cd "$output_dir" && pwd)"

summary_file="$output_dir/summary.tsv"
printf "run\trc\tstatus\tsummary\n" > "$summary_file"

for i in $(seq 1 "$runs"); do
  run_id="$(printf 'run-%02d' "$i")"
  run_dir="$output_dir/$run_id"
  mkdir -p "$run_dir"

  cmd=(timeout "$timeout_sec" "$binary" --goal "$goal" --llm-base-url "$base_url" --llm-model "$model" --max-steps "$max_steps" --session-dir "$run_dir")
  if [[ -n "$token_file" ]]; then
    cmd+=(--llm-token-file "$token_file")
  fi
  if [[ "$allow_all" -eq 1 ]]; then
    cmd+=(--allow-all)
  fi
  if [[ "$inspect_context" -eq 1 ]]; then
    cmd+=(--inspect-context)
  fi

  (
    cd "$workdir"
    set +e
    "${cmd[@]}" >"$run_dir/stdout.txt" 2>"$run_dir/stderr.txt"
    rc=$?
    set -e
    echo "$rc" > "$run_dir/rc.txt"
  )

  rc="$(cat "$run_dir/rc.txt")"
  status="unknown"
  summary=""

  if [[ -f "$run_dir/session.json" ]]; then
    status="$(sed -n 's/.*"status": "\(.*\)".*/\1/p' "$run_dir/session.json" | head -n 1)"
    summary="$(sed -n 's/.*"summary": "\(.*\)".*/\1/p' "$run_dir/session.json" | head -n 1)"
  fi

  printf "%s\t%s\t%s\t%s\n" "$run_id" "$rc" "${status:-unknown}" "${summary:-}" >> "$summary_file"
  printf "[%s] rc=%s status=%s\n" "$run_id" "$rc" "${status:-unknown}"
done

echo
echo "batch output: $output_dir"
echo "summary: $summary_file"
