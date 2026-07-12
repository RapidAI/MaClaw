#!/usr/bin/env bash
# Adaptive prompt & shared agent loop critical-path regression suite.
# Run from repo root:  bash scripts/test-adaptive-shared-loop.sh
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

failed=0

run_pkg() {
  local pkg="$1"
  local run="$2"
  local timeout="${3:-120s}"
  echo ""
  echo "==> go test ${pkg} -run ${run}"
  if go test "${pkg}" -count=1 -timeout "${timeout}" -run "${run}"; then
    echo "OK   ${pkg}"
  else
    echo "FAIL ${pkg}"
    failed=1
  fi
}

doctor_run='TestSharedLoop|TestAdaptivePrompt|TestRunIncludesAdaptive'
agent_run='TestRecordLight|TestPromptProfile|TestResolvePromptProfile|TestShouldAB|TestFormatPrompt|TestAdaptivePromptHeartbeat|TestWritePromptProfile|TestMergePrompt|TestLoadPrompt'
cli_run='TestSharedLoop'
gui_run='TestPreviewSharedLoop|TestSharedLoopCanary|TestGetSharedAgentLoop|TestExportAdaptive|TestRunDoctor_Always|TestRunDoctor_Includes'
tui_run='TestFormatCanary|TestFirstNonFlagArg|TestSlashCanary'

run_pkg ./corelib/doctor/ "$doctor_run" 60s
run_pkg ./corelib/agent/ "$agent_run" 120s
run_pkg ./maclaw-cli/ "$cli_run" 90s
run_pkg ./gui/ "$gui_run" 120s
run_pkg ./tui/ "$tui_run" 90s

echo ""
if [[ "$failed" -ne 0 ]]; then
  echo "Adaptive/shared-loop regression FAILED."
  exit 1
fi
echo "Adaptive/shared-loop regression PASSED."
exit 0
