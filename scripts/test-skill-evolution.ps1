# Skill self-evolution critical-path regression suite.
# Run from repo root:  ./scripts/test-skill-evolution.ps1
# Exit code 0 only when all packages pass.

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
Set-Location $Root

$failed = @()

function Invoke-GoTestPackage {
    param(
        [Parameter(Mandatory = $true)][string]$Package,
        [Parameter(Mandatory = $true)][string]$Run,
        [int]$TimeoutSec = 120
    )
    Write-Host ""
    Write-Host "==> go test $Package -run `"$Run`"" -ForegroundColor Cyan
    & go test $Package -count=1 -timeout "${TimeoutSec}s" -run $Run
    if ($LASTEXITCODE -ne 0) {
        $script:failed += $Package
        Write-Host "FAIL $Package" -ForegroundColor Red
    } else {
        Write-Host "OK   $Package" -ForegroundColor Green
    }
}

$coreRun = "TestApply|TestVersioner_|TestEvolutionAudit|TestCollectMaintenance|TestNormalizeManageSkillActionEvolution|TestOpsCritical|TestCollectHighValue|TestFileBacked|TestIsHighValue|TestBuildHighValue|TestBuildMaintenanceExperience|TestIngestHighValue|TestKindFromEventName|TestBuildSkillMaintenance|TestExecuteSkillMaintenancePlanSkipsFileBacked"
$guiRun = "TestSetNLSkillStatus|TestGetSkillEvolutionStatus|TestListSkillEvolution|TestPatchConfigFieldsSkillEvolution|TestBatch|TestBuildExperienceLearningSnapshotIncludesSkillMaintenance"
$tuiRun = "TestManageSkillHandler_AllCanonical|TestManageSkillHandler_Evolution|TestManageSkillHandler_SetEvolution"

Invoke-GoTestPackage -Package "./corelib/skill/" -Run $coreRun -TimeoutSec 90
Invoke-GoTestPackage -Package "./gui/" -Run $guiRun -TimeoutSec 120
Invoke-GoTestPackage -Package "./tui/" -Run $tuiRun -TimeoutSec 90

Write-Host ""
if ($failed.Count -gt 0) {
    Write-Host "Skill evolution regression FAILED: $($failed -join ', ')" -ForegroundColor Red
    exit 1
}
Write-Host "Skill evolution regression PASSED." -ForegroundColor Green
exit 0
