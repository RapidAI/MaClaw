# Adaptive prompt & shared agent loop critical-path regression suite.
# Run from repo root:  ./scripts/test-adaptive-shared-loop.ps1
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

# core: canary FNV, adaptive doctor checks, prompt stats / A/B / deny / export
$doctorRun = "TestSharedLoop|TestAdaptivePrompt|TestRunIncludesAdaptive"
$agentRun = "TestRecordLight|TestPromptProfile|TestResolvePromptProfile|TestShouldAB|TestFormatPrompt|TestAdaptivePromptHeartbeat|TestWritePromptProfile|TestMergePrompt|TestLoadPrompt"
$cliRun = "TestSharedLoop"
$guiRun = "TestPreviewSharedLoop|TestSharedLoopCanary|TestGetSharedAgentLoop|TestExportAdaptive|TestRunDoctor_Always|TestRunDoctor_Includes"
$tuiRun = "TestFormatCanary|TestFirstNonFlagArg|TestSlashCanary"

Invoke-GoTestPackage -Package "./corelib/doctor/" -Run $doctorRun -TimeoutSec 60
Invoke-GoTestPackage -Package "./corelib/agent/" -Run $agentRun -TimeoutSec 120
Invoke-GoTestPackage -Package "./maclaw-cli/" -Run $cliRun -TimeoutSec 90
Invoke-GoTestPackage -Package "./gui/" -Run $guiRun -TimeoutSec 120
Invoke-GoTestPackage -Package "./tui/" -Run $tuiRun -TimeoutSec 90

Write-Host ""
if ($failed.Count -gt 0) {
    Write-Host "Adaptive/shared-loop regression FAILED: $($failed -join ', ')" -ForegroundColor Red
    exit 1
}
Write-Host "Adaptive/shared-loop regression PASSED." -ForegroundColor Green
exit 0
