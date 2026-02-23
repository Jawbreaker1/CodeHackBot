package orchestrator

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	workerRuntimeGuardrailBinRelPath = ".birdhackbot/runtime-bin"
	workerNmapGuardrailHostTimeout   = "10s"
	workerNmapGuardrailMaxRetries    = "1"
	workerNmapGuardrailMaxRate       = "1500"
)

var lookPath = exec.LookPath

func applyWorkerRuntimeGuardrails(baseEnv []string, workDir string) ([]string, []string, error) {
	env := append([]string{}, baseEnv...)
	realNmap, err := lookPath("nmap")
	if err != nil || strings.TrimSpace(realNmap) == "" {
		return env, nil, nil
	}
	if strings.TrimSpace(workDir) == "" {
		workDir = "."
	}
	binDir := filepath.Join(workDir, workerRuntimeGuardrailBinRelPath)
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("prepare runtime guardrail bin dir: %w", err)
	}
	wrapperPath := filepath.Join(binDir, "nmap")
	script := renderNmapGuardrailWrapper(realNmap)
	if err := os.WriteFile(wrapperPath, []byte(script), 0o755); err != nil {
		return nil, nil, fmt.Errorf("write runtime nmap guardrail wrapper: %w", err)
	}

	pathValue := prependPathEntry(envValue(env, "PATH"), binDir)
	env = withEnvValue(env, "PATH", pathValue)
	notes := []string{
		fmt.Sprintf("runtime guardrail active: nmap wrapper enforces -n, --host-timeout %s, --max-retries %s, --max-rate %s", workerNmapGuardrailHostTimeout, workerNmapGuardrailMaxRetries, workerNmapGuardrailMaxRate),
	}
	return env, notes, nil
}

func renderNmapGuardrailWrapper(realNmap string) string {
	return fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail

real_nmap=%q
input_args=("$@")
args=()
has_sn=0
has_n=0
has_host_timeout=0
has_max_retries=0
has_max_rate=0

for ((i=0; i<${#input_args[@]}; i++)); do
  arg="${input_args[$i]}"
  case "$arg" in
    -R|--system-dns)
      continue
      ;;
    --host-timeout)
      has_host_timeout=1
      args+=("$arg")
      if ((i+1 < ${#input_args[@]})); then
        i=$((i+1))
        args+=("${input_args[$i]}")
      fi
      continue
      ;;
    --host-timeout=*)
      has_host_timeout=1
      args+=("$arg")
      continue
      ;;
    --max-retries)
      has_max_retries=1
      args+=("$arg")
      if ((i+1 < ${#input_args[@]})); then
        i=$((i+1))
        args+=("${input_args[$i]}")
      fi
      continue
      ;;
    --max-retries=*)
      has_max_retries=1
      args+=("$arg")
      continue
      ;;
    --max-rate)
      has_max_rate=1
      args+=("$arg")
      if ((i+1 < ${#input_args[@]})); then
        i=$((i+1))
        args+=("${input_args[$i]}")
      fi
      continue
      ;;
    --max-rate=*)
      has_max_rate=1
      args+=("$arg")
      continue
      ;;
  esac

  case "$arg" in
    -sn)
      has_sn=1
      ;;
    -n)
      has_n=1
      ;;
  esac
  args+=("$arg")
done

if ((has_sn == 1)); then
  for i in "${!args[@]}"; do
    if [[ "${args[$i]}" =~ ^127\.[0-9]+\.[0-9]+\.[0-9]+/([0-9]{1,2})$ ]]; then
      mask="${BASH_REMATCH[1]}"
      if ((mask < 24)); then
        args[$i]="127.0.0.1"
      fi
    fi
    if [[ "${args[$i]}" =~ ^::1/([0-9]{1,3})$ ]]; then
      mask="${BASH_REMATCH[1]}"
      if ((mask < 120)); then
        args[$i]="::1"
      fi
    fi
  done
fi

prefix=()
if ((has_n == 0)); then
  prefix+=("-n")
fi
if ((has_host_timeout == 0)); then
  prefix+=("--host-timeout" "%s")
fi
if ((has_max_retries == 0)); then
  prefix+=("--max-retries" "%s")
fi
if ((has_max_rate == 0)); then
  prefix+=("--max-rate" "%s")
fi

exec "$real_nmap" "${prefix[@]}" "${args[@]}"
`, realNmap, workerNmapGuardrailHostTimeout, workerNmapGuardrailMaxRetries, workerNmapGuardrailMaxRate)
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			return strings.TrimPrefix(item, prefix)
		}
	}
	return ""
}

func withEnvValue(env []string, key, value string) []string {
	key = strings.TrimSpace(key)
	if key == "" {
		return append([]string{}, env...)
	}
	out := make([]string, 0, len(env)+1)
	prefix := key + "="
	set := false
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			if !set {
				out = append(out, prefix+value)
				set = true
			}
			continue
		}
		out = append(out, item)
	}
	if !set {
		out = append(out, prefix+value)
	}
	return out
}

func prependPathEntry(pathValue, entry string) string {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return pathValue
	}
	if strings.TrimSpace(pathValue) == "" {
		return entry
	}
	parts := strings.Split(pathValue, string(os.PathListSeparator))
	for _, part := range parts {
		if part == entry {
			return pathValue
		}
	}
	return entry + string(os.PathListSeparator) + pathValue
}

