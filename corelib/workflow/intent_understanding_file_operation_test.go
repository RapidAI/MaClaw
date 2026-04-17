package workflow

import (
	"strings"
	"testing"
)

// ===========================================================================
// Bug: PPT file operation misroute
//
// User says "打开桌面上任何一个ppt文件并截图" (open any PPT file on desktop
// and take a screenshot). The system incorrectly triggers the presentation_design
// workflow because:
//   1. LLM correctly returns category="none" (rejected)
//   2. handleNeedsUnderstanding called tryKeywordWorkflowFallback(strongOnly=true)
//   3. MatchTemplateByStrongKeyword found "PPT" → matched presentation_design
//   4. Keyword fallback overrode the LLM's correct rejection
//
// Fix: When the LLM explicitly rejects a message (category="none"), trust
// the LLM's judgment. Keyword fallback is only used when the LLM call FAILS
// (timeout, network error), not when it succeeds and says "not a workflow".
// ===========================================================================

// TestLLMRejection_FileOperationNotOverridden verifies that when the LLM
// correctly rejects a file-operation message, the Start() function returns
// Rejected=true and no understanding session is created.
//
// The fix is in handleNeedsUnderstanding (GUI side) which no longer calls
// tryKeywordWorkflowFallback when result.Rejected=true. This test verifies
// the LLM-level behavior: the LLM returns category="none" for file operations.
func TestLLMRejection_FileOperationNotOverridden(t *testing.T) {
	// Mock LLM that correctly returns category="none" for file operations
	llm := &MockLLMCaller{
		Response: `{"intent":{"category":"none","summary":"文件操作"},"reply":"","ready":false}`,
	}
	registry := NewWorkflowRegistry()
	mgr := NewIntentUnderstandingManager(NullStore{}, llm, registry)

	result, err := mgr.Start("user1", "打开桌面上任何一个ppt文件并截图")
	if err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}
	if !result.Rejected {
		t.Errorf("Start() returned Rejected=false, want true. "+
			"LLM returned category='none' but Start() did not reject. "+
			"File operation messages should be rejected by the LLM.")
	}

	// Verify no session was created
	if mgr.HasActiveSession("user1") {
		t.Errorf("HasActiveSession() = true after rejection, want false. "+
			"No session should be created when LLM rejects the message.")
	}
}

// TestLLMRejection_CreationTaskAccepted verifies that when the LLM correctly
// identifies a PPT creation task, the Start() function creates a session.
func TestLLMRejection_CreationTaskAccepted(t *testing.T) {
	// Mock LLM that correctly returns category="presentation_design"
	llm := &MockLLMCaller{
		Response: `{"intent":{"category":"presentation_design","summary":"设计PPT","confidence":0.9},"reply":"好的，我来帮你设计PPT。确定了就告诉我\"开工\"","ready":false}`,
	}
	registry := NewWorkflowRegistry()
	mgr := NewIntentUnderstandingManager(NullStore{}, llm, registry)

	result, err := mgr.Start("user1", "帮我设计一个产品介绍PPT")
	if err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}
	if result.Rejected {
		t.Errorf("Start() returned Rejected=true, want false. "+
			"PPT creation tasks should be accepted by the LLM.")
	}
	if result.Reply == "" {
		t.Errorf("Start() returned empty Reply, want non-empty. "+
			"LLM should provide a reply for accepted workflow tasks.")
	}

	// Verify session was created
	if !mgr.HasActiveSession("user1") {
		t.Errorf("HasActiveSession() = false after acceptance, want true. "+
			"A session should be created when LLM accepts the message.")
	}
}

// TestSystemPrompt_FileOperationGuidance verifies that the intent understanding
// system prompt contains explicit guidance for file operations and PPT
// disambiguation, giving the LLM the information it needs to make correct
// classification decisions.
func TestSystemPrompt_FileOperationGuidance(t *testing.T) {
	registry := NewWorkflowRegistry()
	llm := &MockLLMCaller{Response: `{"reply":"ok","ready":false}`}
	mgr := NewIntentUnderstandingManager(NullStore{}, llm, registry)

	prompt := mgr.buildSystemPrompt()

	// The prompt should contain file operation guidance
	checks := []struct {
		name    string
		content string
	}{
		{
			"file_operation_category",
			"文件操作",
		},
		{
			"ppt_file_open_example",
			"打开桌面上的PPT文件并截图",
		},
		{
			"ppt_convert_example",
			"把PPT转换成PDF",
		},
		{
			"ppt_disambiguation",
			"PPT 特别注意",
		},
	}

	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(prompt, tc.content) {
				t.Errorf("buildSystemPrompt() does NOT contain %q. "+
					"The LLM needs this guidance to correctly distinguish "+
					"file operations from creation tasks.", tc.content)
			}
		})
	}
}

// TestKeywordFallback_StillWorksForLLMFailure verifies that MatchTemplateByStrongKeyword
// still matches "PPT" as a strong keyword. This function is used as a degraded
// fallback when the LLM call fails (timeout/network error). The fix ensures
// handleNeedsUnderstanding no longer CALLS this function when the LLM explicitly
// rejects — the keyword matcher itself is unchanged.
func TestKeywordFallback_StillWorksForLLMFailure(t *testing.T) {
	registry := NewWorkflowRegistry()

	// "PPT" is a strong keyword — keyword matcher should match it regardless
	// of surrounding context. The semantic understanding is the LLM's job,
	// not the keyword matcher's.
	wt, matched := registry.MatchTemplateByStrongKeyword("打开桌面上任何一个ppt文件并截图")
	if !matched {
		t.Fatalf("MatchTemplateByStrongKeyword should match 'PPT' in file operation text — "+
			"the keyword matcher has no semantic understanding, it just matches keywords. "+
			"The protection against false positives is in handleNeedsUnderstanding, not here.")
	}
	if wt != WorkflowPresentationDesign {
		t.Errorf("MatchTemplateByStrongKeyword matched %q, want %q",
			wt, WorkflowPresentationDesign)
	}

	// Verify it also matches creation tasks (preservation)
	wt2, matched2 := registry.MatchTemplateByStrongKeyword("帮我设计一个产品介绍PPT")
	if !matched2 {
		t.Fatalf("MatchTemplateByStrongKeyword should match PPT creation task")
	}
	if wt2 != WorkflowPresentationDesign {
		t.Errorf("MatchTemplateByStrongKeyword matched %q for creation task, want %q",
			wt2, WorkflowPresentationDesign)
	}
}
