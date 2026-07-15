# Runs automated approval E2E checklist suites (see docs/approval-e2e-verification-zh.md).
# Usage (Windows PowerShell 5.1 or PowerShell 7+):
#   powershell -File scripts/run-approval-e2e-checks.ps1
#   pwsh -File scripts/run-approval-e2e-checks.ps1
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

function Step {
    param(
        [Parameter(Mandatory = $true)][string]$Name,
        [Parameter(Mandatory = $true)][scriptblock]$ScriptBlock
    )
    Write-Host ""
    Write-Host "=== $Name ===" -ForegroundColor Cyan
    & $ScriptBlock
    if ($LASTEXITCODE -ne 0) {
        throw "FAILED: $Name (exit $LASTEXITCODE)"
    }
    Write-Host "OK $Name" -ForegroundColor Green
}

Step -Name "Hub workflow (executor / escalation / checklist)" -ScriptBlock {
    go test ./hub/internal/workflow/ -count=1 -timeout 180s -run "TestChecklist_|TestEscalation_|TestHandleTimeout|TestStartInstance_"
}

Step -Name "Hub httpapi (multi-machine push / classify)" -ScriptBlock {
    go test ./hub/internal/httpapi/ -count=1 -timeout 120s -run "TestHubWorkflowParticipantNotifier|TestClassifyWorkflowStatusEvent"
}

Step -Name "GUI (attention / directory / reconcile)" -ScriptBlock {
    go test ./gui/ -count=1 -timeout 180s -run "TestApplyHubWorkflowStatusAttention|TestMaclawAppApprovalInstanceFromHubDirectoryItem|TestReconcile"
}

Step -Name "Workflow editor empty-state" -ScriptBlock {
    node hub/web/approval_workflow/workflow-editor.test.js
}

Step -Name "Workflow editor i18n" -ScriptBlock {
    node hub/web/approval_workflow/i18n.test.js
}

Write-Host ""
Write-Host "All automated approval E2E checks passed." -ForegroundColor Green
Write-Host "Manual remaining: dual-desktop WS (#1), Admin empty roles UX (#4), Hub jitter (#5)." -ForegroundColor Yellow
Write-Host "See docs/approval-e2e-verification-zh.md"
