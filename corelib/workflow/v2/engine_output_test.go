package v2

import (
	"strings"
	"testing"
)

func setupGaokaoEngineWithMachine(t *testing.T) (*WorkflowEngine, *StateMachine) {
	t.Helper()

	registry := NewWorkflowRegistry()
	registry.MustRegister(&TemplateSpec{
		Type: WorkflowGaokaoApplication,
		Name: "gaokao application",
		Phases: []PhaseSpec{
			{ID: GaokaoPhaseProfile, Name: "profile", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
			{ID: GaokaoPhaseDataSearch, Name: "data search", NeedsConfirm: true, ToolPolicy: ToolPolicyFull},
			{ID: GaokaoPhaseCandidateRanking, Name: "ranking", NeedsConfirm: true, ToolPolicy: ToolPolicyFull},
			{ID: GaokaoPhaseFinalPlan, Name: "final plan", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
		},
	})

	machine := setupTestMachine()
	engine := NewWorkflowEngine(registry, nil, nil, nil)
	engine.SetMachine(machine)
	if _, err := engine.StartWorkflowWithOptions("user1", StructuredIntent{
		Category: WorkflowGaokaoApplication,
		Summary:  "高考志愿",
	}, WorkflowStartOptions{ProjectPath: "d:\\project"}); err != nil {
		t.Fatalf("StartWorkflowWithOptions failed: %v", err)
	}
	setGaokaoEngineAndMachinePhase(t, engine, machine, GaokaoPhaseFinalPlan, 3)
	return engine, machine
}

func setGaokaoEngineAndMachinePhase(t *testing.T, engine *WorkflowEngine, machine *StateMachine, phaseID string, phaseIndex int) {
	t.Helper()

	engine.mu.Lock()
	ws := engine.workflows["user1"]
	if ws == nil {
		engine.mu.Unlock()
		t.Fatal("engine workflow missing")
	}
	ws.CurrentPhase = phaseID
	ws.PhaseIndex = phaseIndex
	ws.PendingReviewPhaseID = ""
	if ws.PhaseOutputs == nil {
		ws.PhaseOutputs = make(map[string]string)
	}
	engine.mu.Unlock()

	state := machine.GetActive("user1")
	if state == nil {
		t.Fatal("machine workflow missing")
	}
	state.CurrentPhase = phaseIndex
	for i := range state.Phases {
		state.Phases[i].Status = PhaseCompleted
		state.Phases[i].Output = "completed"
	}
	state.Phases[phaseIndex].Status = PhaseRunning
	state.Phases[phaseIndex].Output = ""
	if err := machine.store.Save(state); err != nil {
		t.Fatalf("machine Save failed: %v", err)
	}
}

func TestSavePhaseOutputAndMaybeAdvanceRejectsIncompleteGaokaoFinalPlan(t *testing.T) {
	engine, machine := setupGaokaoEngineWithMachine(t)

	phaseID, resp, err := engine.SavePhaseOutputAndMaybeAdvance("user1", "让我读取 evidence_doc.txt，然后运行 check_db2.py 看看数据库结构。")
	if err == nil {
		t.Fatal("expected incomplete final plan to be rejected")
	}
	if !strings.Contains(err.Error(), "gaokao final plan output is incomplete") {
		t.Fatalf("error = %v", err)
	}
	if phaseID != GaokaoPhaseFinalPlan {
		t.Fatalf("phaseID = %q, want %q", phaseID, GaokaoPhaseFinalPlan)
	}
	if resp != nil {
		t.Fatalf("response = %#v, want nil", resp)
	}

	ws := engine.GetActiveWorkflow("user1")
	if ws == nil {
		t.Fatal("engine workflow should remain active")
	}
	if got := ws.PhaseOutputs[GaokaoPhaseFinalPlan]; got != "" {
		t.Fatalf("engine output should not be saved: %q", got)
	}
	if ws.PendingReviewPhaseID != "" {
		t.Fatalf("PendingReviewPhaseID = %q, want empty", ws.PendingReviewPhaseID)
	}

	state := machine.GetActive("user1")
	if state == nil {
		t.Fatal("machine workflow should remain active")
	}
	if got := state.ActivePhase().Status; got != PhaseRunning {
		t.Fatalf("machine phase status = %q, want running", got)
	}
	if got := state.ActivePhase().Output; got != "" {
		t.Fatalf("machine output should not be saved: %q", got)
	}
}

func TestSavePhaseOutputAndMaybeAdvanceRejectsEngineMachinePhaseMismatch(t *testing.T) {
	engine, machine := setupGaokaoEngineWithMachine(t)
	setGaokaoEngineAndMachinePhase(t, engine, machine, GaokaoPhaseCandidateRanking, 2)
	engine.mu.Lock()
	engine.workflows["user1"].CurrentPhase = GaokaoPhaseFinalPlan
	engine.workflows["user1"].PhaseIndex = 3
	engine.mu.Unlock()

	phaseID, resp, err := engine.SavePhaseOutputAndMaybeAdvance("user1", "让我读取 evidence_doc.txt，然后运行 check_db2.py 看看数据库结构。")
	if err == nil {
		t.Fatal("expected phase mismatch to be rejected")
	}
	if !strings.Contains(err.Error(), "workflow state mismatch") {
		t.Fatalf("error = %v", err)
	}
	if phaseID != GaokaoPhaseFinalPlan {
		t.Fatalf("phaseID = %q, want %q", phaseID, GaokaoPhaseFinalPlan)
	}
	if resp != nil {
		t.Fatalf("response = %#v, want nil", resp)
	}

	ws := engine.GetActiveWorkflow("user1")
	if got := ws.PhaseOutputs[GaokaoPhaseFinalPlan]; got != "" {
		t.Fatalf("engine final output should not be saved: %q", got)
	}
	state := machine.GetActive("user1")
	if state == nil {
		t.Fatal("machine workflow should remain active")
	}
	if got := state.ActivePhase().ID; got != GaokaoPhaseCandidateRanking {
		t.Fatalf("machine active phase = %q, want %q", got, GaokaoPhaseCandidateRanking)
	}
	if got := state.ActivePhase().Output; got != "" {
		t.Fatalf("machine mismatched phase output should not be saved: %q", got)
	}
}

func TestSavePhaseOutputAndMaybeAdvanceAcceptsCompleteGaokaoFinalPlan(t *testing.T) {
	engine, machine := setupGaokaoEngineWithMachine(t)
	output := completeGaokaoFinalPlanOutputForTest()

	phaseID, resp, err := engine.SavePhaseOutputAndMaybeAdvance("user1", output)
	if err != nil {
		t.Fatalf("SavePhaseOutputAndMaybeAdvance failed: %v", err)
	}
	if phaseID != GaokaoPhaseFinalPlan {
		t.Fatalf("phaseID = %q, want %q", phaseID, GaokaoPhaseFinalPlan)
	}
	if resp == nil || !resp.PendingConfirm {
		t.Fatalf("response = %#v, want pending confirm", resp)
	}

	ws := engine.GetActiveWorkflow("user1")
	if got := ws.PhaseOutputs[GaokaoPhaseFinalPlan]; got != output {
		t.Fatalf("engine output = %q, want complete output", got)
	}
	if ws.PendingReviewPhaseID != GaokaoPhaseFinalPlan {
		t.Fatalf("PendingReviewPhaseID = %q, want %q", ws.PendingReviewPhaseID, GaokaoPhaseFinalPlan)
	}

	state := machine.GetActive("user1")
	if got := state.ActivePhase().Status; got != PhaseWaitingConfirm {
		t.Fatalf("machine phase status = %q, want waiting_confirm", got)
	}
	if got := state.ActivePhase().Output; got != output {
		t.Fatalf("machine output = %q, want complete output", got)
	}
}

func TestApplyReviewIntentConfirmCompletesGaokaoFinalPlanInEngineAndMachine(t *testing.T) {
	engine, machine := setupGaokaoEngineWithMachine(t)
	output := completeGaokaoFinalPlanOutputForTest()
	if _, _, err := engine.SavePhaseOutputAndMaybeAdvance("user1", output); err != nil {
		t.Fatalf("SavePhaseOutputAndMaybeAdvance failed: %v", err)
	}

	resp, err := engine.ApplyReviewIntent("user1", ReviewIntentConfirm, "")
	if err != nil {
		t.Fatalf("ApplyReviewIntent failed: %v", err)
	}
	if resp == nil || !resp.Complete {
		t.Fatalf("response = %#v, want complete", resp)
	}
	if ws := engine.GetActiveWorkflow("user1"); ws != nil {
		t.Fatalf("engine workflow should not remain active: %#v", ws)
	}
	if machine.GetActive("user1") != nil {
		t.Fatal("machine workflow should not remain active")
	}
}

func TestApplyReviewIntentSupplementReopensGaokaoFinalPlanForRevision(t *testing.T) {
	engine, machine := setupGaokaoEngineWithMachine(t)
	output := completeGaokaoFinalPlanOutputForTest()
	if _, _, err := engine.SavePhaseOutputAndMaybeAdvance("user1", output); err != nil {
		t.Fatalf("SavePhaseOutputAndMaybeAdvance failed: %v", err)
	}

	resp, err := engine.ApplyReviewIntent("user1", ReviewIntentSupplement, "补充宁波诺丁汉和西交利物浦")
	if err != nil {
		t.Fatalf("ApplyReviewIntent failed: %v", err)
	}
	if resp == nil || !resp.RunAgentLoop {
		t.Fatalf("response = %#v, want rerun", resp)
	}
	ws := engine.GetActiveWorkflow("user1")
	if ws == nil {
		t.Fatal("engine workflow should remain active")
	}
	if ws.PendingReviewPhaseID != "" {
		t.Fatalf("PendingReviewPhaseID = %q, want empty", ws.PendingReviewPhaseID)
	}
	if !ws.PendingReviewRevisionRequested {
		t.Fatal("PendingReviewRevisionRequested should be true")
	}
	if got := ws.PhaseOutputs[GaokaoPhaseFinalPlan]; got != "" {
		t.Fatalf("engine output should be cleared for revision: %q", got)
	}
	state := machine.GetActive("user1")
	if state == nil {
		t.Fatal("machine workflow should remain active")
	}
	if got := state.ActivePhase().Status; got != PhaseRunning {
		t.Fatalf("machine phase status = %q, want running", got)
	}
	if got := state.ActivePhase().Output; got != "" {
		t.Fatalf("machine output should be cleared for revision: %q", got)
	}

	phaseID, saveResp, err := engine.SavePhaseOutputAndMaybeAdvance("user1", output)
	if err != nil {
		t.Fatalf("SavePhaseOutputAndMaybeAdvance after revision failed: %v", err)
	}
	if phaseID != GaokaoPhaseFinalPlan || saveResp == nil || !saveResp.PendingConfirm {
		t.Fatalf("phaseID=%q response=%#v, want final pending confirm", phaseID, saveResp)
	}
}

func completeGaokaoFinalPlanOutputForTest() string {
	return `# 填报参考资料与建议

## 总排清单
| 学校 | 专业 | 办学地点 | 类型 | 往年最低位次 | 推荐理由 | 数据来源 |
| 北京工业大学 | 电子信息类 | 北京 | 普通 | 12000 | 位次匹配 | 北京教育考试院 https://example.edu/admission |

## 冲
| 学校 | 专业 | 办学地点 | 类型 | 最低位次 | 推荐理由 | 依据来源 |
| A大学 | 电子信息 | 北京 | 普通 | 11000 | 专业较好 | 官方招生网 https://example.edu/admission |

## 稳
| 学校 | 专业 | 办学地点 | 类型 | 最低位次 | 推荐理由 | 依据来源 |
| B大学 | 电子信息 | 北京 | 中外合办 | 12500 | 位次合理 | 官方招生网 https://example.edu/admission |

## 保
| 学校 | 专业 | 办学地点 | 类型 | 最低位次 | 推荐理由 | 依据来源 |
| C大学 | 电子信息 | 北京 | 普通 | 15000 | 兜底 | 官方招生网 https://example.edu/admission |`
}
