package guiapp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
)

func TestCodingQualityGateSettingDefaultsOffAndCanBeEnabled(t *testing.T) {
	if !codingQualityGateEnabled(nil) {
		t.Fatal("agents without an app keep the historical gate so unit audits stay covered")
	}

	app := &App{testHomeDir: t.TempDir()}
	if _, err := app.LoadConfig(); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	handler := &IMMessageHandler{app: app}
	if codingQualityGateEnabled(handler) {
		t.Fatal("programming settings must leave the quality gate off until the user opts in")
	}

	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{handler: handler}}
	cb.trackRemoteFileRead("/repo/main.cpp")
	cb.trackRemoteFileChanged("/repo/main.cpp", false)
	cb.trackRemoteFileRead("/repo/main.cpp")
	cb.trackRemoteCommand(`cd build && ctest --output-on-failure 2>&1 | tail -20`, "/repo", "100% tests passed", true)
	result := cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status: "success", Summary: "done", ToolCalls: 4,
	})
	if result.Status != "success" || result.Error != "" || result.VerifiedNoChange || result.QualityStatus != codingSubAgentQualityNotNeeded || result.QualitySummary != "" || result.VerificationSummary != "" {
		t.Fatalf("disabled quality gate must keep a successful turn without an audit banner, got %#v", result)
	}
	failed := cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status: "failed", Error: "model stopped", Summary: "partial", ToolCalls: 2,
	})
	if failed.Status != "failed" || failed.Error != "model stopped" || failed.QualityStatus != codingSubAgentQualityNotNeeded || failed.QualitySummary != "" || failed.VerificationSummary != "" {
		t.Fatalf("disabled quality gate must leave a real loop failure unchanged, got %#v", failed)
	}

	if _, err := app.PatchConfigFields(map[string]interface{}{"coding_quality_gate_enabled": true}); err != nil {
		t.Fatalf("PatchConfigFields: %v", err)
	}
	if !codingQualityGateEnabled(handler) {
		t.Fatal("turning the setting on must enable the quality gate")
	}
	result = cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status: "success", Summary: "done", ToolCalls: 4,
	})
	if result.Status != "failed" || result.Error == "" {
		t.Fatalf("enabled quality gate must still reject an unauditable verifier, got %#v", result)
	}
}

func TestRelaxCodingPromptDropsQualityGateName(t *testing.T) {
	out := relaxCodingPromptForDisabledQualityGate("## Quality audit gates\n- keep\n不要调用 git_diff 来「凑」质量门禁。\n不要为「通过质量门禁」去改代码或造测试。\n不要把同一份完整报告再交一遍，除非门禁明确要求重交。\n")
	if strings.Contains(out, "Quality audit gates") || strings.Contains(out, "质量门禁") || strings.Contains(out, "门禁") || !strings.Contains(out, "Host quality gate is off for this turn.") || !strings.Contains(out, "不要把同一份完整报告再交一遍。") {
		t.Fatalf("relaxed prompt still names the quality gate: %s", out)
	}
}

func TestCodingQualityGateOffPromptDropsAuditThreat(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		if app.enterpriseClient != nil {
			_ = app.enterpriseClient.Close()
		}
	})
	if _, err := app.LoadConfig(); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	handler := &IMMessageHandler{app: app}
	local := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{
			handler:                  handler,
			projectPath:              t.TempDir(),
			correlatedLocalExecution: true,
		},
		task: &TaskItem{Title: "update parser error handling"},
	}
	off := local.BuildSystemPrompt("update parser error handling", true)
	if strings.Contains(off, "Enforced hard gates") || strings.Contains(off, "failure-suppressing") || strings.Contains(off, "Quality audit gates") || strings.Contains(off, "质量门禁") || strings.Contains(off, "门禁") || strings.Contains(off, "report_localization before editing") || !strings.Contains(off, "does not block edits") || !strings.Contains(off, "Host quality gate is off for this turn.") {
		t.Fatalf("disabled quality gate prompt must stop threatening an audit failure")
	}
	userMsg := local.buildTaskUserMessage()
	if strings.Contains(userMsg, "Do not present pre-edit verification as final verification") || strings.Contains(userMsg, "the gate says") || strings.Contains(userMsg, "before editing existing code") {
		t.Fatalf("disabled quality gate user message must not demand a separate final verifier")
	}

	remote := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{
		handler:                   handler,
		projectDir:                "/repo",
		workDir:                   "/repo",
		correlatedRemoteExecution: true,
	}}
	remoteOff := remote.BuildSystemPrompt("update parser error handling", true)
	if strings.Contains(remoteOff, "质量门禁") || strings.Contains(remoteOff, "门禁") || strings.Contains(remoteOff, "质量门误判") || strings.Contains(remoteOff, "必须在本步骤验证") || strings.Contains(remoteOff, "Finish the requested change") || strings.Contains(remoteOff, "禁止修改") || !strings.Contains(remoteOff, "缺少这份报告不会阻止修改") || !strings.Contains(remoteOff, "Host quality gate is off for this turn.") {
		t.Fatalf("disabled remote prompt must stop requiring the quality gate")
	}

	if _, err := app.PatchConfigFields(map[string]interface{}{"coding_quality_gate_enabled": true}); err != nil {
		t.Fatalf("PatchConfigFields: %v", err)
	}
	localOn := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{
			handler:                  handler,
			projectPath:              t.TempDir(),
			correlatedLocalExecution: true,
		},
		task: &TaskItem{Title: "update parser error handling"},
	}
	on := localOn.BuildSystemPrompt("update parser error handling", true)
	if !strings.Contains(on, "Enforced hard gates") || !strings.Contains(on, "report_localization before editing") || strings.Contains(on, "Host quality gate is off for this turn.") {
		t.Fatalf("enabled quality gate must keep the audit instructions")
	}
	if !strings.Contains(localOn.buildTaskUserMessage(), "Do not present pre-edit verification as final verification") || !strings.Contains(localOn.buildTaskUserMessage(), "before editing existing code") {
		t.Fatalf("enabled quality gate must keep the final verification instruction")
	}
}

func TestDisabledQualityGateAcceptsUnchangedWorkspace(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if _, err := app.LoadConfig(); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	handler := &IMMessageHandler{app: app}
	hostDigest := codingRuntimeDigest(codingQualityGateDisabledNoChangeSource)

	unchanged := (&remoteCodingCallbacks{agent: &RemoteCodingSubAgent{handler: handler}}).applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status: "success", Summary: "already implemented", ToolCalls: 2,
	})
	if unchanged.Status != "success" || !unchanged.VerifiedNoChange || unchanged.QualityStatus != codingSubAgentQualityNotNeeded {
		t.Fatalf("disabled quality gate must accept a successful turn that did not edit files, got %#v", unchanged)
	}
	remoteOut := (&guiRemoteCodingRuntimeAdapter{run: func() *RemoteCodingSubAgentResult { return unchanged }}).Execute(context.Background(), codingruntime.ExecutionRequest{})
	if !ledgerNoChangeAccepted(remoteOut) || remoteOut.NoWorkspaceChangeEvidenceDigest != hostDigest || remoteOut.NoWorkspaceChangeEvidenceDigest == codingRuntimeDigest(unchanged.Summary) {
		t.Fatalf("unchanged remote success must carry the host no-change assertion, got %#v", remoteOut)
	}
	store := codingruntime.NewMemoryStore()
	_, attempt, err := runGUIRemoteCodingTaskWithLedger(
		context.Background(), store, "owner", "workflow", "phase", "remote-target", "/srv/repo", "fix",
		codingruntime.WorkspaceProberFunc(func(context.Context, codingruntime.Task, codingruntime.Attempt) (*codingruntime.WorkspaceProbe, error) {
			return &codingruntime.WorkspaceProbe{ProjectRef: "/srv/repo", Head: "abc", HostKey: "remote-target", WorkDir: "/srv/repo"}, nil
		}),
		func() *RemoteCodingSubAgentResult { return unchanged },
	)
	if err != nil || attempt == nil || attempt.Status != codingruntime.TaskCompleted || attempt.ErrorCode != "" {
		t.Fatalf("an unchanged successful turn must complete the execution ledger, attempt=%#v err=%v", attempt, err)
	}

	passed := (&remoteCodingCallbacks{agent: &RemoteCodingSubAgent{handler: handler}}).applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status: "passed", Summary: "already implemented", ToolCalls: 1,
	})
	if passed.Status != "passed" || !passed.VerifiedNoChange {
		t.Fatalf("a passed turn with no edits must be accepted, got %#v", passed)
	}
	withError := (&remoteCodingCallbacks{agent: &RemoteCodingSubAgent{handler: handler}}).applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status: "success", Error: "model stopped", Summary: "partial", ToolCalls: 1,
	})
	if withError.VerifiedNoChange {
		t.Fatalf("a success that still carries an error must not bypass the workspace gate, got %#v", withError)
	}

	edited := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{handler: handler}}
	edited.trackRemoteFileChanged("/repo/main.cpp", false)
	changed := edited.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{Status: "success", Summary: "edited", ToolCalls: 2})
	if changed.VerifiedNoChange {
		t.Fatal("a turn that edited files must not be recorded as a verified no-change")
	}
	changedOut := (&guiRemoteCodingRuntimeAdapter{run: func() *RemoteCodingSubAgentResult { return changed }}).Execute(context.Background(), codingruntime.ExecutionRequest{})
	if changedOut.Status != codingruntime.TaskCompleted || changedOut.NoWorkspaceChangeEvidenceDigest != "" {
		t.Fatalf("file-changing success must complete without a no-change digest, got %#v", changedOut)
	}

	failed := (&remoteCodingCallbacks{agent: &RemoteCodingSubAgent{handler: handler}}).applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status: "failed", Error: "model stopped", ToolCalls: 1,
	})
	if failed.Status != "failed" || failed.VerifiedNoChange {
		t.Fatalf("a failed turn must stay failed without no-change acceptance, got %#v", failed)
	}

	localOut := (&guiCodingRuntimeAdapter{run: func() *CodingSubAgentResult {
		return &CodingSubAgentResult{Status: TaskExecPassed, Summary: "already implemented", qualityGateDisabled: true}
	}}).Execute(context.Background(), codingruntime.ExecutionRequest{})
	if !ledgerNoChangeAccepted(localOut) || localOut.NoWorkspaceChangeEvidenceDigest != hostDigest {
		t.Fatalf("local success with the quality gate off must carry the host no-change assertion, got %#v", localOut)
	}

	withoutHostFlag := (&guiCodingRuntimeAdapter{run: func() *CodingSubAgentResult {
		return &CodingSubAgentResult{Status: TaskExecPassed, Summary: "already implemented", QualityStatus: codingSubAgentQualityNotNeeded}
	}}).Execute(context.Background(), codingruntime.ExecutionRequest{})
	if withoutHostFlag.NoWorkspaceChangeEvidenceDigest != "" {
		t.Fatalf("an unchanged turn without the host flag must not bypass the workspace gate, got %#v", withoutHostFlag)
	}

	root := initRecoveryGitFixture(t)
	options := defaultGUICodingRuntimeOptions(&TaskItem{RequestKind: codingRequestImplementation, Files: []string{"kept.txt"}}, root)
	localResult, localAttempt, localErr := runGUICodingTaskWithLedgerWithOptions(
		context.Background(), codingruntime.NewMemoryStore(), "gui:test", "workflow", "phase", root, "confirm existing behavior", nil, nil, options,
		func() *CodingSubAgentResult {
			return &CodingSubAgentResult{Status: TaskExecPassed, Summary: "already implemented", qualityGateDisabled: true}
		},
	)
	if localErr != nil || localResult == nil || localAttempt == nil || localResult.Status != TaskExecPassed || localAttempt.Status != codingruntime.TaskCompleted || localAttempt.ErrorCode != "" {
		t.Fatalf("local success with the quality gate off must complete an unchanged workspace, result=%#v attempt=%#v err=%v", localResult, localAttempt, localErr)
	}
}

func ledgerNoChangeAccepted(result codingruntime.ExecutionResult) bool {
	digest := strings.TrimSpace(result.NoWorkspaceChangeEvidenceDigest)
	if result.Status != codingruntime.TaskCompleted || digest == "" {
		return false
	}
	for _, evidence := range result.Evidence {
		if strings.TrimSpace(evidence.Type) == "verified_no_change" && strings.TrimSpace(evidence.Digest) == digest {
			return true
		}
	}
	return false
}

func TestDisabledQualityGateOperationalChecklistOmitsGateName(t *testing.T) {
	off := false
	cb := &codingSubAgentCallbacks{
		qualityGateEnabled: &off,
		subagent:           &CodingSubAgent{correlatedLocalExecution: true},
		task:               &TaskItem{Title: "run the demo", RequestKind: codingRequestOperational},
	}
	msg := cb.buildTaskUserMessage()
	if strings.Contains(msg, "质量门") {
		t.Fatalf("operational checklist must not name the quality gate while it is off: %s", msg)
	}
	on := true
	cb.qualityGateEnabled = &on
	if !strings.Contains(cb.buildTaskUserMessage(), "通过质量门禁") {
		t.Fatal("enabled quality gate keeps the operational checklist wording")
	}
}

func TestDisabledQualityGateDoesNotBlockBugFixEdit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bug.go")
	if err := os.WriteFile(path, []byte("package bug\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	off := false
	local := &codingSubAgentCallbacks{
		qualityGateEnabled: &off,
		subagent:           &CodingSubAgent{projectPath: dir},
		task:               &TaskItem{Title: "fix crash bug", Description: "panic on save"},
	}
	if got := local.requireLocalizationBeforeExistingBugEdit(path, false); got != "" {
		t.Fatalf("disabled quality gate must allow the edit, got %q", got)
	}
	report := local.executeReportLocalization(map[string]interface{}{})
	if report.Outcome != codingToolOutcomeSuccess || !strings.Contains(report.Text, "Do not retry") {
		t.Fatalf("disabled quality gate must skip localization instead of failing the tool, got %#v", report)
	}
	remote := &remoteCodingCallbacks{
		qualityGateEnabled: &off,
		agent:              &RemoteCodingSubAgent{},
		task:               "fix crash bug",
		taskContext:        "panic on save",
	}
	if got := remote.requireRemoteLocalizationBeforeBugEdit(map[string]interface{}{"path": "bug.go"}, true); got != "" {
		t.Fatalf("disabled quality gate must allow the remote edit, got %q", got)
	}
	if skipped := remote.executeRemoteReportLocalization(map[string]interface{}{}); !strings.Contains(skipped, "Do not retry") || remoteCodingToolResultLooksFailed(skipped) {
		t.Fatalf("disabled quality gate must skip the remote localization report, got %q", skipped)
	}
	on := true
	local.qualityGateEnabled = &on
	if got := local.requireLocalizationBeforeExistingBugEdit(path, false); !strings.Contains(got, "report_localization") {
		t.Fatalf("enabled quality gate must still require localization, got %q", got)
	}
	rejected := local.executeReportLocalization(map[string]interface{}{})
	if rejected.Outcome == codingToolOutcomeSuccess {
		t.Fatalf("enabled quality gate must still reject an empty localization report, got %#v", rejected)
	}
}
