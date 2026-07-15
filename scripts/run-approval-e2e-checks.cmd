@echo off
REM Approval E2E automated checks (docs/approval-e2e-verification-zh.md)
REM Usage: scripts\run-approval-e2e-checks.cmd
setlocal
cd /d "%~dp0.."
if errorlevel 1 exit /b 1

echo === Hub workflow (executor / escalation / checklist) ===
go test ./hub/internal/workflow/ -count=1 -timeout 180s -run "TestChecklist_|TestEscalation_|TestHandleTimeout|TestStartInstance_"
if errorlevel 1 exit /b 1

echo.
echo === Hub httpapi (multi-machine push / classify) ===
go test ./hub/internal/httpapi/ -count=1 -timeout 120s -run "TestHubWorkflowParticipantNotifier|TestClassifyWorkflowStatusEvent"
if errorlevel 1 exit /b 1

echo.
echo === GUI (attention / directory / reconcile) ===
go test ./gui/ -count=1 -timeout 180s -run "TestApplyHubWorkflowStatusAttention|TestMaclawAppApprovalInstanceFromHubDirectoryItem|TestReconcile"
if errorlevel 1 exit /b 1

echo.
echo === Workflow editor empty-state ===
node hub/web/approval_workflow/workflow-editor.test.js
if errorlevel 1 exit /b 1

echo.
echo === Workflow editor i18n ===
node hub/web/approval_workflow/i18n.test.js
if errorlevel 1 exit /b 1

echo.
echo All automated approval E2E checks passed.
echo Manual remaining: dual-desktop WS (#1), Admin empty roles UX (#4), Hub jitter (#5).
echo See docs/approval-e2e-verification-zh.md
exit /b 0
