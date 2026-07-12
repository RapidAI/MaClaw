#!/usr/bin/env bash
# Skill self-evolution critical-path regression suite.
# Run from repo root:  bash scripts/test-skill-evolution.sh
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

core_run='TestApply|TestVersioner_|TestEvolutionAudit|TestCollectMaintenance|TestNormalizeManageSkillActionEvolution|TestOpsCritical|TestCollectHighValue|TestFileBacked|TestIsHighValue|TestBuildHighValue|TestBuildMaintenanceExperience|TestIngestHighValue|TestKindFromEventName|TestBuildSkillMaintenance|TestExecuteSkillMaintenancePlanSkipsFileBacked'
gui_run='TestSetNLSkillStatus|TestGetSkillEvolutionStatus|TestListSkillEvolution|TestPatchConfigFieldsSkillEvolution|TestBatch|TestBuildExperienceLearningSnapshotIncludesSkillMaintenance'
tui_run='TestManageSkillHandler_AllCanonical|TestManageSkillHandler_Evolution|TestManageSkillHandler_SetEvolution'

run_pkg ./corelib/skill/ "$core_run" 90s
run_pkg ./gui/ "$gui_run" 120s
run_pkg ./tui/ "$tui_run" 90s

echo ""
if [[ "$failed" -ne 0 ]]; then
  echo "Skill evolution regression FAILED."
  exit 1
fi
echo "Skill evolution regression PASSED."
exit 0
