@echo off
REM Approval E2E automated checks (docs/approval-e2e-verification-zh.md)
REM Usage: scripts\run-approval-e2e-checks.cmd
setlocal
cd /d "%~dp0.."
if errorlevel 1 exit /b 1

echo === Hub workflow (executor / escalation / checklist / reconcile) ===
go test ./hub/internal/workflow/ -count=1 -timeout 180s -run "TestChecklist_|TestEscalation_|TestHandleTimeout|TestOnEscalation|TestReconcileEscalations|TestShouldDefer|TestPersistEscalation|TestEnqueueEscalation|TestStartInstance_"
if errorlevel 1 exit /b 1

echo.
echo === Hub httpapi (multi-machine push / classify / escalation extras) ===
go test ./hub/internal/httpapi/ -count=1 -timeout 120s -run "TestHubWorkflowParticipantNotifier|TestClassifyWorkflowStatusEvent"
if errorlevel 1 exit /b 1

echo.
echo === GUI (attention / directory / reconcile / escalation merge) ===
go test ./gui/ -count=1 -timeout 180s -run "TestApplyHubWorkflowStatusAttention|TestMaclawAppApprovalInstanceFromHubDirectoryItem|TestReconcile|TestMergeMaclawAppApprovalEscalation"
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
echo Next (release day, ~15 min): docs\approval-release-day-checklist-zh.md
echo Full manual matrix: docs\approval-e2e-verification-zh.md
echo   Required: #1 dual-desktop WS; #4 empty roles OR #5 Hub jitter
echo   Optional: #7 any-N, #8 Hub restart, #9 peer-aware timeout
exit /b 0
