package workflow

import (
	"strings"
	"testing"
)

// ===========================================================================
// Bug Condition Exploration Tests — Property 1a
//
// These tests encode the EXPECTED behavior after the fix. They are designed
// to FAIL on unfixed code, proving the bug exists.
//
// Property 1a: buildSystemPrompt() output SHALL contain a dedicated
// "内容处理任务 vs 工作流任务" section that teaches the LLM the semantic
// distinction between one-shot content processing and multi-phase workflow
// creation, including confusable examples.
//
// **Validates: Requirements 1.2, 2.1, 2.2**
// ===========================================================================

// TestBugCondition_SystemPromptMissingContentProcessingSection verifies that
// buildSystemPrompt() contains a dedicated "内容处理任务 vs 工作流任务" section.
//
// EXPECTED TO FAIL on unfixed code: the section does not exist yet.
// This failure confirms the bug — the LLM prompt lacks explicit guidance
// to distinguish content processing tasks from workflow tasks.
func TestBugCondition_SystemPromptMissingContentProcessingSection(t *testing.T) {
	registry := NewWorkflowRegistry()
	llm := &MockLLMCaller{Response: `{"reply":"ok","ready":false}`}
	mgr := NewIntentUnderstandingManager(NullStore{}, llm, registry)

	prompt := mgr.buildSystemPrompt()

	// Assert: prompt contains the content processing vs workflow section
	if !strings.Contains(prompt, "内容处理任务 vs 工作流任务") {
		t.Errorf("Bug condition confirmed: buildSystemPrompt() does NOT contain "+
			"'内容处理任务 vs 工作流任务' section.\n"+
			"This section is needed to teach the LLM the semantic distinction "+
			"between one-shot content processing and multi-phase workflow creation.\n"+
			"Prompt length: %d chars", len(prompt))
	}
}

// TestBugCondition_SystemPromptMissingConfusableContentExamples verifies that
// buildSystemPrompt() contains confusable examples that specifically address
// the content processing vs workflow ambiguity (e.g., "看HF论文做摘要" → none).
//
// EXPECTED TO FAIL on unfixed code: these examples do not exist yet.
// This failure confirms the bug — the LLM has no guidance for distinguishing
// "reading/summarizing existing papers" from "writing a new literature review".
func TestBugCondition_SystemPromptMissingConfusableContentExamples(t *testing.T) {
	registry := NewWorkflowRegistry()
	llm := &MockLLMCaller{Response: `{"reply":"ok","ready":false}`}
	mgr := NewIntentUnderstandingManager(NullStore{}, llm, registry)

	prompt := mgr.buildSystemPrompt()

	// These confusable examples should exist in the prompt to prevent
	// content processing tasks from being misclassified as workflows.
	confusableExamples := []struct {
		text     string
		category string
	}{
		{"看HF论文做摘要", "none"},
		{"把这份报告翻译成英文", "none"},
		{"整理这些会议纪要", "none"},
		{"解读这篇论文的核心观点", "none"},
	}

	for _, ex := range confusableExamples {
		if !strings.Contains(prompt, ex.text) {
			t.Errorf("Bug condition confirmed: buildSystemPrompt() does NOT contain "+
				"confusable example %q → category=%q.\n"+
				"Without this example, the LLM may misclassify content processing "+
				"tasks as workflow tasks.", ex.text, ex.category)
		}
	}
}

// ===========================================================================
// Preservation Tests — Property 2b
//
// These tests MUST PASS on unfixed code. They confirm baseline behavior
// that must be preserved after the fix is applied.
//
// Property 2b: buildSystemPrompt() output SHALL contain all existing section
// headers that are present in the current (unfixed) code. This ensures the
// fix does not accidentally remove or rename any existing prompt sections.
//
// **Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8**
// ===========================================================================

// TestPreservation_ExistingPromptSectionsPresent verifies that buildSystemPrompt()
// contains all existing section headers. These sections are critical for the LLM's
// intent classification behavior and must remain unchanged after the fix.
//
// MUST PASS on unfixed code — confirms baseline behavior to preserve.
func TestPreservation_ExistingPromptSectionsPresent(t *testing.T) {
	registry := NewWorkflowRegistry()
	llm := &MockLLMCaller{Response: `{"reply":"ok","ready":false}`}
	mgr := NewIntentUnderstandingManager(NullStore{}, llm, registry)

	prompt := mgr.buildSystemPrompt()

	// All existing section headers that must be preserved.
	requiredSections := []string{
		"核心判断：是否需要工作流",
		"可用的工作流类型",
		"你的职责",
		"输出格式",
		"category 判断规则",
		"易混淆示例",
		"ready 判断规则",
	}

	for _, section := range requiredSections {
		if !strings.Contains(prompt, section) {
			t.Errorf("Preservation FAILED: buildSystemPrompt() does NOT contain "+
				"required section %q.\n"+
				"This section is critical for LLM intent classification and must "+
				"be preserved after the fix.\nPrompt length: %d chars",
				section, len(prompt))
		}
	}
}

// TestPreservation_AllRegisteredTemplatesDescribed verifies that buildSystemPrompt()
// includes descriptions for all registered workflow templates. This ensures the LLM
// has visibility into all available workflow types for classification.
//
// MUST PASS on unfixed code — confirms baseline behavior to preserve.
func TestPreservation_AllRegisteredTemplatesDescribed(t *testing.T) {
	registry := NewWorkflowRegistry()
	llm := &MockLLMCaller{Response: `{"reply":"ok","ready":false}`}
	mgr := NewIntentUnderstandingManager(NullStore{}, llm, registry)

	prompt := mgr.buildSystemPrompt()

	// Every registered template type should appear in the prompt
	// (via AllDescriptions() which is included in the "可用的工作流类型" section).
	registeredTypes := []WorkflowType{
		WorkflowCoding,
		WorkflowProductDesign,
		WorkflowInnovation,
		WorkflowBusinessPlan,
		WorkflowTesting,
		WorkflowLiteratureReview,
		WorkflowResearchReport,
		WorkflowExperimentDesign,
		WorkflowGrantProposal,
		WorkflowPaperWriting,
		WorkflowProjectProposal,
		WorkflowEventPlanning,
		WorkflowCompetitiveAnalysis,
		WorkflowPresentationDesign,
		WorkflowBidResponse,
		WorkflowContractReview,
		WorkflowDueDiligence,
		WorkflowComplianceAudit,
		WorkflowPatentAnalysis,
		WorkflowOpsMaintenance,
		WorkflowChangjiangScholar,
		WorkflowChangjiangScholarReview,
	}

	for _, wt := range registeredTypes {
		if !strings.Contains(prompt, string(wt)) {
			t.Errorf("Preservation FAILED: buildSystemPrompt() does NOT mention "+
				"registered template type %q.\n"+
				"All registered templates must be described in the system prompt "+
				"so the LLM can classify intent correctly.", string(wt))
		}
	}
}

func TestPreservation_OpsMaintenancePromptGuidancePresent(t *testing.T) {
	registry := NewWorkflowRegistry()
	llm := &MockLLMCaller{Response: `{"reply":"ok","ready":false}`}
	mgr := NewIntentUnderstandingManager(NullStore{}, llm, registry)

	prompt := mgr.buildSystemPrompt()
	required := []string{
		`category="ops_maintenance"`,
		"服务器运维",
		"只读采集",
		"风险门禁",
		"受控执行",
	}
	for _, want := range required {
		if !strings.Contains(prompt, want) {
			t.Fatalf("intent prompt should contain ops maintenance guidance %q", want)
		}
	}
}
