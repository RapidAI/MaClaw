package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

// ===========================================================================
// Bug Condition Exploration Tests — Property 1b
//
// These tests encode the EXPECTED behavior after the fix. They are designed
// to FAIL on unfixed code, proving the bug exists.
//
// Property 1b: When HandleInput returns ready=true with intent.Category="none"
// (or empty), handleActiveUnderstanding() SHALL NOT call engine.StartWorkflow().
// Instead it SHALL return nil (fall through to normal agent loop).
//
// **Validates: Requirements 1.3, 1.4, 2.3, 2.4, 2.5**
// ===========================================================================

// mockLLMCallerGUI is a test double for workflow.LLMCaller in the gui package.
type mockLLMCallerGUI struct {
	Response string
	Err      error
}

func (m *mockLLMCallerGUI) DoSimpleLLMRequest(messages []interface{}, timeout time.Duration) (string, error) {
	if m.Err != nil {
		return "", m.Err
	}
	return m.Response, nil
}

// mockEngineCallbacksGUI is a no-op implementation of workflow.EngineCallbacks.
type mockEngineCallbacksGUI struct {
	// SentTexts captures messages sent via SendTextToUser for test assertions.
	SentTexts []string
}

func (m *mockEngineCallbacksGUI) SendTextToUser(userID, text string) error {
	m.SentTexts = append(m.SentTexts, text)
	return nil
}
func (m *mockEngineCallbacksGUI) EmitPhaseUpdate(userID string, state *workflow.WorkflowState) error { return nil }
func (m *mockEngineCallbacksGUI) EmitDocUpdate(userID, phaseID, content string) error                { return nil }
func (m *mockEngineCallbacksGUI) EmitGateResult(userID, phaseID string, result *workflow.QualityGateResult) error {
	return nil
}

// setupWorkflowTestHandler creates a minimal IMMessageHandler with a workflow
// engine configured to use the given LLM mock for intent understanding.
func setupWorkflowTestHandler(llm workflow.LLMCaller) (*IMMessageHandler, *mockEngineCallbacksGUI) {
	registry := workflow.NewWorkflowRegistry()
	cb := &mockEngineCallbacksGUI{}
	understanding := workflow.NewIntentUnderstandingManager(workflow.NullStore{}, llm, registry)
	engine := workflow.NewWorkflowEngine(registry, understanding, workflow.NullStore{}, cb)

	app := &App{}
	app.workflowEngine = engine

	handler := &IMMessageHandler{
		app: app,
	}
	return handler, cb
}

// TestBugCondition_CategoryNoneReadyTrue_ShouldNotCallStartWorkflow verifies
// that when HandleInput returns ready=true with category="none",
// handleActiveUnderstanding does NOT call StartWorkflow.
//
// EXPECTED TO FAIL on unfixed code: the current code unconditionally calls
// StartWorkflow, which fails with "未找到匹配的工作流模板: none".
// The expected behavior is to return nil (fall through to agent loop).
func TestBugCondition_CategoryNoneReadyTrue_ShouldNotCallStartWorkflow(t *testing.T) {
	// Phase 1: Start an understanding session with a coding intent
	llm := &mockLLMCallerGUI{
		Response: `{"intent":{"category":"coding","summary":"做一个系统","confidence":0.7,"ready":false},"reply":"我理解你想做一个系统","ready":false}`,
	}
	handler, _ := setupWorkflowTestHandler(llm)
	engine := handler.app.workflowEngine
	understanding := engine.GetUnderstanding()

	userID := "test-user-none"

	// Start an understanding session
	_, err := understanding.Start(userID, "看HF论文做摘要")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if !understanding.HasActiveSession(userID) {
		t.Fatal("expected active session after Start")
	}

	// Phase 2: LLM changes its mind — returns category="none" with ready=true
	// This simulates the bug scenario where the LLM realizes it's not a workflow
	// task mid-session and returns category="none" with ready=true.
	llm.Response = `{"intent":{"category":"none","summary":"内容处理任务","confidence":0.9,"ready":true},"reply":"好的，这是一个内容处理任务","ready":true}`

	// Call handleActiveUnderstanding — on unfixed code, this will call
	// StartWorkflow("none") which fails with "未找到匹配的工作流模板: none"
	resp := handler.handleActiveUnderstanding(engine, userID, "都要，开工")

	// Expected behavior (after fix): resp should be nil (fall through to agent loop)
	// Bug behavior (unfixed code): resp contains an error about "未找到匹配的工作流模板: none"
	if resp != nil {
		if resp.Error != "" && strings.Contains(resp.Error, "none") {
			t.Errorf("Bug condition confirmed: handleActiveUnderstanding called StartWorkflow "+
				"with category='none', got error: %s\n"+
				"Expected behavior: should return nil (fall through to agent loop) "+
				"when category='none' + ready=true", resp.Error)
		} else if resp.Text != "" && strings.Contains(resp.Text, "工作流已启动") {
			t.Errorf("Bug condition confirmed: handleActiveUnderstanding started a workflow "+
				"with category='none'. Response: %s\n"+
				"Expected behavior: should return nil (fall through to agent loop)", resp.Text)
		} else {
			t.Errorf("Bug condition: handleActiveUnderstanding returned non-nil response "+
				"for category='none' + ready=true.\n"+
				"Response: Text=%q, Error=%q\n"+
				"Expected behavior: should return nil (fall through to agent loop)",
				resp.Text, resp.Error)
		}
	}
}

// TestBugCondition_CategoryEmptyReadyTrue_ShouldNotCallStartWorkflow verifies
// that when HandleInput returns ready=true with category="" (empty string),
// handleActiveUnderstanding does NOT call StartWorkflow.
//
// EXPECTED TO FAIL on unfixed code: the current code unconditionally calls
// StartWorkflow, which fails with "未找到匹配的工作流模板: ".
// The expected behavior is to return nil (fall through to agent loop).
func TestBugCondition_CategoryEmptyReadyTrue_ShouldNotCallStartWorkflow(t *testing.T) {
	// Phase 1: Start an understanding session with a coding intent
	llm := &mockLLMCallerGUI{
		Response: `{"intent":{"category":"coding","summary":"做一个系统","confidence":0.7,"ready":false},"reply":"我理解你想做一个系统","ready":false}`,
	}
	handler, _ := setupWorkflowTestHandler(llm)
	engine := handler.app.workflowEngine
	understanding := engine.GetUnderstanding()

	userID := "test-user-empty"

	// Start an understanding session
	_, err := understanding.Start(userID, "帮我整理会议纪要")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if !understanding.HasActiveSession(userID) {
		t.Fatal("expected active session after Start")
	}

	// Phase 2: LLM returns empty category with ready=true
	// This simulates the edge case where the LLM omits the category field
	// but still signals ready=true.
	llm.Response = `{"intent":{"category":"","summary":"简单任务","confidence":0.5,"ready":true},"reply":"好的","ready":true}`

	// Call handleActiveUnderstanding — on unfixed code, this will call
	// StartWorkflow("") which fails with "未找到匹配的工作流模板: "
	resp := handler.handleActiveUnderstanding(engine, userID, "开工")

	// Expected behavior (after fix): resp should be nil (fall through to agent loop)
	// Bug behavior (unfixed code): resp contains an error about missing template
	if resp != nil {
		if resp.Error != "" {
			t.Errorf("Bug condition confirmed: handleActiveUnderstanding called StartWorkflow "+
				"with empty category, got error: %s\n"+
				"Expected behavior: should return nil (fall through to agent loop) "+
				"when category='' + ready=true", resp.Error)
		} else if resp.Text != "" && strings.Contains(resp.Text, "工作流已启动") {
			t.Errorf("Bug condition confirmed: handleActiveUnderstanding started a workflow "+
				"with empty category. Response: %s\n"+
				"Expected behavior: should return nil (fall through to agent loop)", resp.Text)
		} else {
			t.Errorf("Bug condition: handleActiveUnderstanding returned non-nil response "+
				"for category='' + ready=true.\n"+
				"Response: Text=%q, Error=%q\n"+
				"Expected behavior: should return nil (fall through to agent loop)",
				resp.Text, resp.Error)
		}
	}
}

// ===========================================================================
// Preservation Tests — Property 2a
//
// These tests MUST PASS on unfixed code. They confirm baseline behavior
// that must be preserved after the fix is applied.
//
// Property 2a: For all valid registered workflow types with ready=true,
// handleActiveUnderstanding() calls StartWorkflow and returns the workflow
// startup message containing "🚀 工作流已启动".
//
// **Validates: Requirements 3.1, 3.2, 3.7**
// ===========================================================================

// TestPreservation_ValidWorkflowCategories_StartWorkflow verifies that for
// each valid registered workflow type, when HandleInput returns ready=true
// with that category, handleActiveUnderstanding calls StartWorkflow and
// returns the "🚀 工作流已启动" message.
//
// MUST PASS on unfixed code — confirms baseline behavior to preserve.
func TestPreservation_ValidWorkflowCategories_StartWorkflow(t *testing.T) {
	// All valid registered workflow types that must continue to work.
	validCategories := []struct {
		category workflow.WorkflowType
		name     string
	}{
		{workflow.WorkflowCoding, "编程开发"},
		{workflow.WorkflowProductDesign, "产品设计"},
		{workflow.WorkflowInnovation, "创新制定"},
		{workflow.WorkflowBusinessPlan, "商业计划书"},
		{workflow.WorkflowTesting, "测试方案"},
		{workflow.WorkflowLiteratureReview, "论文综述整理"},
		{workflow.WorkflowResearchReport, "研报收集整理"},
		{workflow.WorkflowExperimentDesign, "实验方案设计"},
		{workflow.WorkflowGrantProposal, "基金申请书"},
		{workflow.WorkflowPaperWriting, "学术论文撰写"},
		{workflow.WorkflowProjectProposal, "项目立项"},
		{workflow.WorkflowEventPlanning, "活动策划"},
		{workflow.WorkflowCompetitiveAnalysis, "竞品分析"},
		{workflow.WorkflowPresentationDesign, "演示文稿设计"},
		{workflow.WorkflowBidResponse, "招投标文件生成"},
		{workflow.WorkflowContractReview, "合同审查"},
		{workflow.WorkflowDueDiligence, "尽职调查"},
		{workflow.WorkflowComplianceAudit, "合规审计"},
		{workflow.WorkflowPatentAnalysis, "专利分析"},
	}

	// Input-driven workflow types that still return IMAgentResponse directly
	// (they need to wait for user to upload a document before generating content).
	inputDrivenTypes := map[workflow.WorkflowType]bool{
		workflow.WorkflowBidResponse:     true,
		workflow.WorkflowContractReview:  true,
		workflow.WorkflowDueDiligence:    true,
		workflow.WorkflowComplianceAudit: true,
		workflow.WorkflowPatentAnalysis:  true,
	}

	for _, tc := range validCategories {
		tc := tc // capture range variable
		t.Run(string(tc.category), func(t *testing.T) {
			// Phase 1: Start an understanding session with a generic coding intent
			llm := &mockLLMCallerGUI{
				Response: `{"intent":{"category":"coding","summary":"做一个系统","confidence":0.7,"ready":false},"reply":"我理解你想做一个系统","ready":false}`,
			}
			handler, cb := setupWorkflowTestHandler(llm)
			engine := handler.app.workflowEngine
			understanding := engine.GetUnderstanding()

			userID := "test-preservation-" + string(tc.category)

			// Start an understanding session
			_, err := understanding.Start(userID, "帮我做一个项目")
			if err != nil {
				t.Fatalf("Start failed: %v", err)
			}
			if !understanding.HasActiveSession(userID) {
				t.Fatal("expected active session after Start")
			}

			// Phase 2: LLM returns the valid category with ready=true
			readyResponse := fmt.Sprintf(
				`{"intent":{"category":"%s","summary":"用户确认开始","confidence":0.9,"ready":true},"reply":"好的，开始工作","ready":true}`,
				string(tc.category),
			)
			llm.Response = readyResponse

			// Call handleActiveUnderstanding — should call StartWorkflow.
			resp := handler.handleActiveUnderstanding(engine, userID, "开工")

			if inputDrivenTypes[tc.category] {
				// Input-driven workflows: return IMAgentResponse with overview text
				// (user needs to upload a document before phase generation starts).
				if resp == nil {
					t.Fatalf("Preservation FAILED for input-driven category %q: "+
						"handleActiveUnderstanding returned nil.\n"+
						"Expected: non-nil response with workflow startup message",
						tc.category)
				}
				if !strings.Contains(resp.Text, "🚀 工作流已启动") {
					t.Errorf("Preservation FAILED for input-driven category %q: "+
						"response does not contain '🚀 工作流已启动'.\n"+
						"Response: Text=%q, Error=%q",
						tc.category, resp.Text, resp.Error)
				}
				if !strings.Contains(resp.Text, string(tc.category)) {
					t.Errorf("Preservation FAILED for input-driven category %q: "+
						"response does not mention the workflow type.\n"+
						"Response: Text=%q",
						tc.category, resp.Text)
				}
				if resp.Error != "" {
					t.Errorf("Preservation FAILED for input-driven category %q: "+
						"response contains error: %s",
						tc.category, resp.Error)
				}
			} else {
				// Non-input-driven workflows: return nil (fall through to agent loop)
				// and send overview via SendTextToUser callback.
				if resp != nil {
					t.Fatalf("Preservation FAILED for non-input-driven category %q: "+
						"handleActiveUnderstanding returned non-nil.\n"+
						"Expected: nil (fall through to agent loop) with overview sent via callback.\n"+
						"Response: Text=%q, Error=%q",
						tc.category, resp.Text, resp.Error)
				}
				// Verify overview was sent via callback
				found := false
				for _, sent := range cb.SentTexts {
					if strings.Contains(sent, "🚀 工作流已启动") && strings.Contains(sent, string(tc.category)) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Preservation FAILED for non-input-driven category %q: "+
						"overview message not sent via SendTextToUser callback.\n"+
						"SentTexts: %v",
						tc.category, cb.SentTexts)
				}
				// Verify agent loop markers were set
				if _, ok := handler.workflowAgentLoopMarker.Load(userID); !ok {
					t.Errorf("Preservation FAILED for non-input-driven category %q: "+
						"workflowAgentLoopMarker not set",
						tc.category)
				}
			}
		})
	}
}
