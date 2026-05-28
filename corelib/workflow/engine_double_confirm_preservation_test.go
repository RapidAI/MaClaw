package workflow

import (
	"strings"
	"testing"
	"testing/quick"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// Preservation Property Tests — Property 2: Post-Output Confirmation and
// Non-NeedsConfirm Phases Unchanged
//
// Feature: workflow-double-confirm-fix
//
// These tests capture CURRENT behaviors on UNFIXED code that MUST be
// preserved after the fix is applied. They all PASS on unfixed code.
//
// **Validates: Requirements 3.1, 3.2, 3.3, 3.4**
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Simulation functions
// ---------------------------------------------------------------------------

// NOTE: simulateNeedsConfirmGate (defined in engine_double_confirm_test.go)
// is reused here for preservation tests. The function models the NeedsConfirm
// gate for the engine-based path, including the hasOutput check: the gate
// only fires when hasOutput=true (post-output confirmation). When
// hasOutput=false (first execution), the gate does NOT fire.

// simulateSteeringNeedsConfirmGate models the steering-based NeedsConfirm
// gate from gui/im_message_handler.go (~line 4745). This path is used when
// there is NO WorkflowEngine active workflow (pure steering-driven coding
// workflow). The gate activates when gateConfig.active=true AND iteration > 0.
//
// Parameters:
//   - gateActive: whether the coding tool gate is active (intentCoding + no skip signal)
//   - iteration: the current agent loop iteration (0-based)
//   - hasEngineWorkflow: whether WorkflowEngine has an active workflow for this user
//   - msgContent: the LLM output text
//
// Returns forceReturn=true when the steering gate would terminate the agent loop.
func simulateSteeringNeedsConfirmGate(
	gateActive bool,
	iteration int,
	hasEngineWorkflow bool,
	msgContent string,
) (forceReturn bool) {
	// Compute needsConfirmFromSteering per the current code logic
	needsConfirmFromSteering := false
	if gateActive && iteration > 0 {
		if hasEngineWorkflow {
			// Engine owns the workflow — this path delegates to
			// IsPhaseNeedsConfirm, which is tested separately.
			// For this simulation, we don't activate steering gate
			// when engine workflow exists.
			needsConfirmFromSteering = false
		} else {
			// No engine workflow — pure steering-driven flow.
			needsConfirmFromSteering = true
		}
	}

	if !needsConfirmFromSteering {
		return false
	}

	trimmed := strings.TrimSpace(msgContent)
	if trimmed == "" {
		return false
	}
	if !testIsSubstantivePhaseDocument(trimmed) {
		return false
	}

	return true
}

// ---------------------------------------------------------------------------
// Sub-property 2a: Post-Output Confirmation Force-Returns
//
// For all inputs where needsConfirmFromEngine=true AND HasPhaseOutput=true
// AND isSubstantivePhaseDocument=true
// → gate force-returns
//
// This tests the post-output confirmation path which MUST be preserved.
//
// **Validates: Requirements 3.1, 3.6**
// ---------------------------------------------------------------------------

func TestPreservation2a_PostOutput_SubstantiveText_ForceReturns(t *testing.T) {
	cases := []struct {
		name       string
		msgContent string
	}{
		{
			name:       "Markdown heading document",
			msgContent: "# 受众与目标文档\n\n## 目标受众\n\n技术团队和管理层。",
		},
		{
			name:       "Numbered list document",
			msgContent: "好的，修改后的需求如下：\n1. 用户认证模块\n2. 数据管理模块\n3. 报表生成模块",
		},
		{
			name:       "Long document (200+ rune)",
			msgContent: "修改后的完整需求文档。首先我们需要明确受众和目标，然后制定内容大纲，接着进行视觉设计，最后生成演示文稿。整个过程中我会在每个阶段完成后请您确认，确保最终产出符合您的期望。这是一段足够长的文本，用于测试超过两百个字符的情况。我们需要确保这段文本的长度超过两百个Unicode字符，这样才能触发isSubstantivePhaseDocument的长度检查条件。为了达到这个目标，我们继续添加更多的内容描述，包括功能需求、非功能需求、边界情况和验收标准等各个方面的详细说明。",
		},
		{
			name:       "English heading document",
			msgContent: "# Updated Requirements Document\n\n## Overview\n\nHere are the revised requirements.",
		},
		{
			name:       "Bullet list document (3+ bullets)",
			msgContent: "修改后的方案：\n- 用户认证模块\n- 数据管理模块\n- 报表生成模块\n- API 接口设计",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Pre-conditions
			trimmed := strings.TrimSpace(tc.msgContent)
			if !testIsSubstantivePhaseDocument(trimmed) {
				t.Fatalf("precondition failed: %q should be substantive", tc.msgContent)
			}

			// Simulate gate with hasOutput=true (post-output confirmation)
			forceReturn := simulateNeedsConfirmGate(
				true, // needsConfirmFromEngine
				true, // hasOutput = true (post-output)
				tc.msgContent,
			)

			// EXPECTED: forceReturn = true (gate fires for post-output confirmation)
			if !forceReturn {
				t.Errorf("NeedsConfirm gate did NOT force-return for post-output confirmation (hasOutput=true) "+
					"with substantive input %q; expected forceReturn=true",
					testTruncateForDisplay(tc.msgContent, 80))
			}
		})
	}
}

// TestPreservation2a_PBT_PostOutput_AlwaysForceReturns uses testing/quick to
// verify that for ALL substantive inputs with hasOutput=true, the gate
// force-returns.
//
// **Validates: Requirements 3.1**
func TestPreservation2a_PBT_PostOutput_AlwaysForceReturns(t *testing.T) {
	cfg := &quick.Config{MaxCount: 200}

	err := quick.Check(func(seed int64) bool {
		input := generateSubstantivePreamble(seed)

		trimmed := strings.TrimSpace(input)
		if trimmed == "" || !testIsSubstantivePhaseDocument(trimmed) {
			return true // skip invalid generator output
		}

		forceReturn := simulateNeedsConfirmGate(
			true, // needsConfirmFromEngine
			true, // hasOutput = true (post-output)
			input,
		)

		// Expected: forceReturn = true (post-output confirmation preserved)
		return forceReturn
	}, cfg)
	if err != nil {
		t.Errorf("Preservation property 2a violated: gate did NOT force-return for post-output "+
			"confirmation with substantive text: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Sub-property 2b: NeedsConfirm=false Phases Unaffected
//
// For all inputs where IsPhaseNeedsConfirm=false → gate does NOT activate
// regardless of hasOutput state.
//
// **Validates: Requirements 3.2**
// ---------------------------------------------------------------------------

func TestPreservation2b_NeedsConfirmFalse_GateNeverActivates(t *testing.T) {
	cases := []struct {
		name       string
		hasOutput  bool
		msgContent string
	}{
		{
			name:       "hasOutput=false, substantive text",
			hasOutput:  false,
			msgContent: "# Implementation Plan\n\n## Step 1\n\nLet me start coding the solution.",
		},
		{
			name:       "hasOutput=true, substantive text",
			hasOutput:  true,
			msgContent: "# Updated Implementation\n\n## Changes\n\nHere are the code changes.",
		},
		{
			name:       "hasOutput=false, long text",
			hasOutput:  false,
			msgContent: strings.Repeat("代码实现中", 50) + "。这是一段很长的实现说明文本。",
		},
		{
			name:       "hasOutput=true, numbered list",
			hasOutput:  true,
			msgContent: "实现步骤：\n1. 创建文件\n2. 编写代码\n3. 运行测试",
		},
		{
			name:       "hasOutput=false, short non-substantive text",
			hasOutput:  false,
			msgContent: "开始编码。",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			forceReturn := simulateNeedsConfirmGate(
				false, // needsConfirmFromEngine = false (NeedsConfirm=false phase)
				tc.hasOutput,
				tc.msgContent,
			)

			if forceReturn {
				t.Errorf("NeedsConfirm gate activated for NeedsConfirm=false phase "+
					"(hasOutput=%v, input=%q); expected gate to NOT activate",
					tc.hasOutput, testTruncateForDisplay(tc.msgContent, 80))
			}
		})
	}
}

// TestPreservation2b_PBT_NeedsConfirmFalse_NeverForceReturns uses testing/quick
// to verify that for ALL inputs with needsConfirmFromEngine=false, the gate
// never activates regardless of hasOutput state or text content.
//
// **Validates: Requirements 3.2**
func TestPreservation2b_PBT_NeedsConfirmFalse_NeverForceReturns(t *testing.T) {
	cfg := &quick.Config{MaxCount: 300}

	err := quick.Check(func(seed int64, hasOutput bool) bool {
		// Generate a mix of substantive and non-substantive texts
		var input string
		if seed%2 == 0 {
			input = generateSubstantivePreamble(seed)
		} else {
			input = generateShortNonSubstantiveText(seed)
		}

		forceReturn := simulateNeedsConfirmGate(
			false, // needsConfirmFromEngine = false
			hasOutput,
			input,
		)

		// Expected: forceReturn = false (gate never activates for NeedsConfirm=false)
		return !forceReturn
	}, cfg)
	if err != nil {
		t.Errorf("Preservation property 2b violated: gate activated for NeedsConfirm=false phase: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Sub-property 2c: Steering Path Unchanged
//
// For all inputs where needsConfirmFromSteering=true (no engine workflow)
// → steering gate behavior unchanged.
//
// **Validates: Requirements 3.4**
// ---------------------------------------------------------------------------

func TestPreservation2c_SteeringPath_NoEngineWorkflow_GateActivates(t *testing.T) {
	cases := []struct {
		name       string
		iteration  int
		msgContent string
		expectGate bool
	}{
		{
			name:       "iteration=0, substantive text — gate inactive",
			iteration:  0,
			msgContent: "# 需求文档\n\n## 功能需求\n\n这是需求文档。",
			expectGate: false, // iteration=0 → gate not active
		},
		{
			name:       "iteration=1, substantive text — gate fires",
			iteration:  1,
			msgContent: "# 需求文档\n\n## 功能需求\n\n这是需求文档。",
			expectGate: true,
		},
		{
			name:       "iteration=2, long substantive text — gate fires",
			iteration:  2,
			msgContent: strings.Repeat("需求", 120) + "。这是一段很长的需求文档。",
			expectGate: true,
		},
		{
			name:       "iteration=1, short non-substantive text — gate does not fire",
			iteration:  1,
			msgContent: "好的，开始。",
			expectGate: false, // not substantive
		},
		{
			name:       "iteration=1, short non-substantive text variant — gate does not fire",
			iteration:  1,
			msgContent: "让我先想想这个问题。",
			expectGate: false, // not substantive
		},
		{
			name:       "iteration=1, empty text — gate does not fire",
			iteration:  1,
			msgContent: "",
			expectGate: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			forceReturn := simulateSteeringNeedsConfirmGate(
				true, // gateActive (coding intent detected)
				tc.iteration,
				false, // hasEngineWorkflow = false (pure steering)
				tc.msgContent,
			)

			if forceReturn != tc.expectGate {
				t.Errorf("Steering gate: expected forceReturn=%v, got %v "+
					"(iteration=%d, input=%q)",
					tc.expectGate, forceReturn, tc.iteration,
					testTruncateForDisplay(tc.msgContent, 80))
			}
		})
	}
}

// TestPreservation2c_SteeringPath_WithEngineWorkflow_SteeringGateInactive
// verifies that when an engine workflow IS active, the steering gate does
// NOT activate (engine takes over NeedsConfirm responsibility).
func TestPreservation2c_SteeringPath_WithEngineWorkflow_SteeringGateInactive(t *testing.T) {
	substantiveText := "# 需求文档\n\n## 功能需求\n\n这是需求文档。"

	for _, iteration := range []int{0, 1, 2, 5} {
		forceReturn := simulateSteeringNeedsConfirmGate(
			true, // gateActive
			iteration,
			true, // hasEngineWorkflow = true (engine owns workflow)
			substantiveText,
		)

		if forceReturn {
			t.Errorf("Steering gate activated when engine workflow is active "+
				"(iteration=%d); expected steering gate to be inactive", iteration)
		}
	}
}

// TestPreservation2c_PBT_SteeringPath_SubstantiveText_ForceReturns uses
// testing/quick to verify that for ALL substantive inputs with
// gateActive=true, iteration>0, and no engine workflow, the steering gate
// force-returns.
//
// **Validates: Requirements 3.4**
func TestPreservation2c_PBT_SteeringPath_SubstantiveText_ForceReturns(t *testing.T) {
	cfg := &quick.Config{MaxCount: 200}

	err := quick.Check(func(seed int64) bool {
		input := generateSubstantivePreamble(seed)

		trimmed := strings.TrimSpace(input)
		if trimmed == "" || !testIsSubstantivePhaseDocument(trimmed) {
			return true // skip invalid generator output
		}

		// iteration > 0 (use seed to vary iteration 1-10)
		iteration := int(abs64(seed)%10) + 1

		forceReturn := simulateSteeringNeedsConfirmGate(
			true, // gateActive
			iteration,
			false, // no engine workflow
			input,
		)

		// Expected: forceReturn = true (steering gate fires)
		return forceReturn
	}, cfg)
	if err != nil {
		t.Errorf("Preservation property 2c violated: steering gate did NOT force-return "+
			"for substantive text with gateActive=true, iteration>0, no engine workflow: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Sub-property 2d: Short Non-Substantive Text Continues Loop
//
// For all inputs with short non-substantive text (< 200 rune, no structure)
// → isSubstantivePhaseDocument=false, loop continues regardless of hasOutput.
//
// **Validates: Requirements 3.3**
// ---------------------------------------------------------------------------

func TestPreservation2d_ShortNonSubstantive_LoopContinues(t *testing.T) {
	cases := []struct {
		name       string
		hasOutput  bool
		msgContent string
	}{
		{
			name:       "hasOutput=false, short Chinese text",
			hasOutput:  false,
			msgContent: "好的，我来处理。",
		},
		{
			name:       "hasOutput=true, short Chinese text",
			hasOutput:  true,
			msgContent: "收到，开始工作。",
		},
		{
			name:       "hasOutput=false, short English text",
			hasOutput:  false,
			msgContent: "OK, let me start working on this.",
		},
		{
			name:       "hasOutput=true, short English text",
			hasOutput:  true,
			msgContent: "Got it, starting now.",
		},
		{
			name:       "hasOutput=false, empty text",
			hasOutput:  false,
			msgContent: "",
		},
		{
			name:       "hasOutput=true, whitespace only",
			hasOutput:  true,
			msgContent: "   \n\t  ",
		},
		{
			name:       "hasOutput=false, single word",
			hasOutput:  false,
			msgContent: "好",
		},
		{
			name:       "hasOutput=true, short text without structure",
			hasOutput:  true,
			msgContent: "我来为您处理这个需求，请稍等。",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Verify precondition: text is NOT substantive
			trimmed := strings.TrimSpace(tc.msgContent)
			if trimmed != "" && testIsSubstantivePhaseDocument(trimmed) {
				t.Fatalf("precondition failed: %q should NOT be substantive", tc.msgContent)
			}

			// Test with needsConfirmFromEngine=true — even with gate active,
			// non-substantive text should NOT trigger force-return
			forceReturn := simulateNeedsConfirmGate(
				true, // needsConfirmFromEngine
				tc.hasOutput,
				tc.msgContent,
			)

			if forceReturn {
				t.Errorf("Gate force-returned for short non-substantive text "+
					"(hasOutput=%v, input=%q); expected loop to continue",
					tc.hasOutput, testTruncateForDisplay(tc.msgContent, 80))
			}
		})
	}
}

// TestPreservation2d_PBT_ShortNonSubstantive_NeverForceReturns uses
// testing/quick to verify that for ALL short non-substantive texts, the gate
// never force-returns regardless of hasOutput or needsConfirmFromEngine state.
//
// **Validates: Requirements 3.3**
func TestPreservation2d_PBT_ShortNonSubstantive_NeverForceReturns(t *testing.T) {
	cfg := &quick.Config{MaxCount: 300}

	err := quick.Check(func(seed int64, hasOutput, needsConfirm bool) bool {
		input := generateShortNonSubstantiveText(seed)

		// Verify generator invariant: input must NOT be substantive
		trimmed := strings.TrimSpace(input)
		if trimmed != "" && testIsSubstantivePhaseDocument(trimmed) {
			// Generator produced substantive input — skip
			return true
		}

		forceReturn := simulateNeedsConfirmGate(
			needsConfirm,
			hasOutput,
			input,
		)

		// Expected: forceReturn = false (non-substantive text never triggers gate)
		return !forceReturn
	}, cfg)
	if err != nil {
		t.Errorf("Preservation property 2d violated: gate force-returned for short "+
			"non-substantive text: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Cross-cutting PBT: Engine-based gate with varying workflow states
// ---------------------------------------------------------------------------

// TestPreservation_PBT_CrossCutting_VaryingWorkflowStates generates random
// combinations of (needsConfirm, hasOutput, text) and verifies that the gate
// behavior is consistent with the simulation function.
//
// **Validates: Requirements 3.1, 3.2, 3.3**
func TestPreservation_PBT_CrossCutting_VaryingWorkflowStates(t *testing.T) {
	cfg := &quick.Config{MaxCount: 500}

	err := quick.Check(func(seed int64, needsConfirm, hasOutput bool) bool {
		// Generate random text — mix of substantive and non-substantive
		var input string
		switch abs64(seed) % 4 {
		case 0:
			input = generateSubstantivePreamble(seed)
		case 1:
			input = generateShortNonSubstantiveText(seed)
		case 2:
			input = "" // empty
		case 3:
			// short non-substantive text
			shortTexts := []string{
				"让我先想想", "let me think about this", "先分析一下",
			}
			input = shortTexts[abs64(seed)%int64(len(shortTexts))]
		}

		forceReturn := simulateNeedsConfirmGate(
			needsConfirm,
			hasOutput,
			input,
		)

		// Verify consistency:
		// 1. needsConfirm=false → never force-return
		if !needsConfirm && forceReturn {
			return false
		}

		// 2. empty/whitespace text → never force-return
		trimmed := strings.TrimSpace(input)
		if trimmed == "" && forceReturn {
			return false
		}

		// 3. non-substantive text → never force-return
		if trimmed != "" && !testIsSubstantivePhaseDocument(trimmed) && forceReturn {
			return false
		}

		// 4. needsConfirm=true + substantive + hasOutput=true → force-return
		//    (on FIXED code, this only holds for hasOutput=true; hasOutput=false skips the gate)
		if needsConfirm && hasOutput && trimmed != "" &&
			testIsSubstantivePhaseDocument(trimmed) && !forceReturn {
			return false
		}

		// 5. needsConfirm=true + substantive + hasOutput=false → NO force-return
		//    (on FIXED code, first execution skips the gate)
		if needsConfirm && !hasOutput && trimmed != "" &&
			testIsSubstantivePhaseDocument(trimmed) && forceReturn {
			return false
		}

		return true
	}, cfg)
	if err != nil {
		t.Errorf("Cross-cutting preservation property violated: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Generators
// ---------------------------------------------------------------------------

// generateShortNonSubstantiveText produces random short texts that do NOT
// qualify as substantive phase documents (< 200 rune, no Markdown structure,
// no numbered lists, fewer than 3 bullet lines).
func generateShortNonSubstantiveText(seed int64) string {
	texts := []string{
		"好的，我来处理。",
		"收到，开始工作。",
		"OK, let me start.",
		"Got it.",
		"了解，马上开始。",
		"没问题。",
		"Sure, working on it.",
		"好的。",
		"明白了。",
		"收到。",
		"开始吧。",
		"Let me work on this.",
		"I'll get started.",
		"好的，请稍等。",
		"正在处理中。",
		"Working on it now.",
		"理解了，开始执行。",
		"OK.",
		"",
		"  ",
		"嗯。",
		"Yes.",
		"Alright.",
		"好，开始。",
		"我来看看。",
		"Let me check.",
		"处理中。",
		"稍等。",
		"On it.",
		"Roger that.",
	}

	idx := abs64(seed) % int64(len(texts))
	base := texts[idx]

	// Occasionally add some random padding but keep under 200 rune and
	// without any Markdown structure
	if seed%5 == 0 && base != "" {
		padding := []string{
			"这个任务我来处理。",
			"I will handle this task.",
			"请放心，我会认真完成。",
			"No worries, I got this.",
		}
		padIdx := abs64(seed/5) % int64(len(padding))
		candidate := base + " " + padding[padIdx]
		// Ensure still under 200 rune and not substantive
		if utf8.RuneCountInString(candidate) < 200 {
			trimmed := strings.TrimSpace(candidate)
			if !testIsSubstantivePhaseDocument(trimmed) {
				return candidate
			}
		}
	}

	return base
}

// abs64 returns the absolute value of an int64.
func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
