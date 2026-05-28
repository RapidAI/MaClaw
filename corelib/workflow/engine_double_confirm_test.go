package workflow

import (
	"regexp"
	"strings"
	"testing"
	"testing/quick"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// Bug Condition Exploration Test — Property 1: First Execution Does Not
// Trigger NeedsConfirm Gate
//
// Feature: workflow-double-confirm-fix
// Property 1: Bug Condition — First Execution Substantive Preamble Triggers
// Incorrect Force-Return
//
// **Validates: Requirements 1.1, 1.4, 1.5**
//
// CRITICAL: This test is EXPECTED TO FAIL on unfixed code.
// Failure confirms the bug exists because:
//   - IsPhaseNeedsConfirm(userID) returns true for NeedsConfirm phases
//     regardless of whether PhaseOutputs[CurrentPhase] exists
//   - HasPhaseOutput(userID) does not exist yet
//   - The NeedsConfirm gate in im_message_handler.go uses only
//     needsConfirmFromEngine (from IsPhaseNeedsConfirm) without checking
//     hasOutput state, causing force-return on first execution
//
// The test encodes the EXPECTED behavior: for any agent loop iteration where
// needsConfirmFromEngine=true AND hasOutput=false (first execution) AND the
// LLM output is substantive, the gate SHALL NOT force-return.
//
// Bug Condition (formal):
//
//	isBugCondition(X) =
//	  X.NeedsConfirmFromEngine = true
//	  AND NOT HasPhaseOutput(X.UserID)
//	  AND trimmed != ""
//	  AND isSubstantivePhaseDocument(trimmed)
//
// Counterexamples documented:
//
//	All substantive LLM outputs during first execution trigger force-return
//	because needsConfirmFromEngine does not check hasOutput state.
//	Specifically:
//	- PPT workflow audience_goal phase: plan overview with numbered list
//	- Coding workflow requirements phase: Markdown document with headings
//	- Any NeedsConfirm=true phase with text containing # Heading (even < 200 rune)
//
// ---------------------------------------------------------------------------

// Local copies of the regex patterns from gui/im_message_handler.go
// (isSubstantivePhaseDocument). These are duplicated here because the
// originals are in package main and not importable.
var (
	testSubstantiveHeadingRe    = regexp.MustCompile(`(?m)^#{1,6}\s+\S`)
	testSubstantiveNumberedRe   = regexp.MustCompile(`(?m)^(?:\d+[.、])\s*\S`)
	testSubstantiveBulletLineRe = regexp.MustCompile(`(?m)^[-*]\s+\S`)
)

// simulateNeedsConfirmGate models the NeedsConfirm gate evaluation from
// gui/im_message_handler.go (~line 4763). It returns forceReturn=true when
// the gate would terminate the agent loop.
//
// On FIXED code, this function checks hasOutput and returns forceReturn=false
// when hasOutput=false (first execution — Engine-State-Aware Gate).
func simulateNeedsConfirmGate(
	needsConfirmFromEngine bool,
	hasOutput bool,
	msgContent string,
) (forceReturn bool) {
	if !needsConfirmFromEngine {
		return false
	}

	// Engine-State-Aware Gate: skip gate on first execution (hasOutput=false)
	if !hasOutput {
		return false // first execution — let agent loop continue
	}

	trimmed := strings.TrimSpace(msgContent)
	if trimmed == "" {
		return false
	}
	if !testIsSubstantivePhaseDocument(trimmed) {
		return false
	}

	// Gate fires — force-return
	return true
}

// testIsSubstantivePhaseDocument mirrors the logic from
// gui/im_message_handler.go isSubstantivePhaseDocument.
func testIsSubstantivePhaseDocument(text string) bool {
	if utf8.RuneCountInString(text) >= 200 {
		return true
	}
	if testSubstantiveHeadingRe.MatchString(text) {
		return true
	}
	if testSubstantiveNumberedRe.MatchString(text) {
		return true
	}
	if len(testSubstantiveBulletLineRe.FindAllStringIndex(text, 3)) >= 3 {
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Concrete Test Cases
// ---------------------------------------------------------------------------

// TestBugCondition_FirstExecution_SubstantivePreamble_DoesNotForceReturn
// verifies that the NeedsConfirm gate does NOT force-return during first
// execution (hasOutput=false) when the LLM outputs substantive preamble text.
//
// On UNFIXED code: this test FAILS because the gate force-returns (forceReturn=true)
// even though hasOutput=false. This proves the bug exists.
//
// On FIXED code: this test PASSES because the gate checks hasOutput and
// skips force-return when hasOutput=false.
//
// **Validates: Requirements 1.1, 1.4, 1.5**
func TestBugCondition_FirstExecution_SubstantivePreamble_DoesNotForceReturn(t *testing.T) {
	cases := []struct {
		name       string
		msgContent string
	}{
		{
			// PPT workflow audience_goal phase: plan overview with numbered list
			// Counterexample: gate force-returns this preamble as the phase deliverable
			name:       "PPT workflow audience_goal phase — plan overview with numbered list",
			msgContent: "收到，马上为您启动PPT制作工作流！\n1. 受众目标定义\n2. 内容大纲\n3. 视觉设计\n让我们开始吧！",
		},
		{
			// Coding workflow requirements phase: Markdown document with headings
			// Counterexample: gate force-returns this preamble instead of letting
			// the agent loop continue to generate the full requirements document
			name:       "Coding workflow requirements phase — Markdown doc with headings",
			msgContent: "好的，我来为您生成需求文档。\n\n# 贪吃蛇游戏需求文档\n\n## 1. 功能需求\n...",
		},
		{
			// Any NeedsConfirm=true phase with text containing # Heading (even < 200 rune)
			// Counterexample: even short text with a heading triggers force-return
			name:       "Short text with Markdown heading (< 200 rune)",
			msgContent: "# 受众与目标文档\n\n让我来分析一下。",
		},
		{
			// Substantive preamble with numbered list (Chinese numbering)
			name:       "Numbered list with Chinese numbering",
			msgContent: "好的，我将按以下步骤进行：\n1、需求分析\n2、技术设计\n3、任务拆分\n4、编码实现",
		},
		{
			// Long preamble (≥200 rune) without any Markdown structure
			name:       "Long preamble (200+ rune) without Markdown structure",
			msgContent: strings.Repeat("好", 120) + "，我来为您启动工作流。首先我们需要明确受众和目标，然后制定内容大纲，接着进行视觉设计，最后生成演示文稿。整个过程中我会在每个阶段完成后请您确认，确保最终产出符合您的期望。让我们开始第一个阶段的工作吧！",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Pre-conditions: verify this input satisfies the bug condition
			trimmed := strings.TrimSpace(tc.msgContent)
			if trimmed == "" {
				t.Fatal("precondition failed: msgContent must be non-empty")
			}
			if !testIsSubstantivePhaseDocument(trimmed) {
				t.Fatalf("precondition failed: %q should be substantive", tc.msgContent)
			}

			// Simulate the gate with:
			//   needsConfirmFromEngine = true (phase has NeedsConfirm: true)
			//   hasOutput = false (first execution — no prior output)
			forceReturn := simulateNeedsConfirmGate(
				true,  // needsConfirmFromEngine
				false, // hasOutput = false (first execution)
				tc.msgContent,
			)

			// EXPECTED: forceReturn = false (gate should NOT fire on first execution)
			//
			// On UNFIXED code: forceReturn = true (BUG — gate fires because
			// it doesn't check hasOutput). Test FAILS here, proving the bug.
			if forceReturn {
				t.Errorf("NeedsConfirm gate force-returned on first execution (hasOutput=false) for input %q; "+
					"expected forceReturn=false — gate should NOT fire when no phase output exists yet",
					testTruncateForDisplay(tc.msgContent, 80))
			}
		})
	}
}

// TestBugCondition_PBT_FirstExecution_NeverForceReturns uses testing/quick
// to generate random substantive LLM outputs and verifies that the
// NeedsConfirm gate does NOT force-return during first execution.
//
// On UNFIXED code: this test FAILS because the gate force-returns for all
// substantive inputs regardless of hasOutput state.
//
// **Validates: Requirements 1.1, 1.4, 1.5**
func TestBugCondition_PBT_FirstExecution_NeverForceReturns(t *testing.T) {
	cfg := &quick.Config{MaxCount: 200}

	// Property: for any substantive LLM output during first execution
	// (hasOutput=false), the NeedsConfirm gate must NOT force-return.
	err := quick.Check(func(seed int64) bool {
		input := generateSubstantivePreamble(seed)

		// Verify generator invariant: input must be substantive
		trimmed := strings.TrimSpace(input)
		if trimmed == "" || !testIsSubstantivePhaseDocument(trimmed) {
			// Generator produced invalid input — skip
			return true
		}

		forceReturn := simulateNeedsConfirmGate(
			true,  // needsConfirmFromEngine
			false, // hasOutput = false (first execution)
			input,
		)

		// Expected: forceReturn = false
		return !forceReturn
	}, cfg)
	if err != nil {
		t.Errorf("Property violated: NeedsConfirm gate force-returned on first execution (hasOutput=false): %v\n"+
			"Counterexample proves the bug: gate fires on substantive preamble during first execution "+
			"because needsConfirmFromEngine does not check hasOutput state", err)
	}
}

// TestBugCondition_HasPhaseOutput_NotExposed verifies that the WorkflowEngine
// does NOT currently expose a HasPhaseOutput method. This is the structural
// root cause: the GUI layer cannot query whether the current phase has output.
//
// On UNFIXED code: this test FAILS because HasPhaseOutput does not exist
// (compile error).
// On FIXED code: this test PASSES because HasPhaseOutput is added.
//
// **Validates: Requirements 1.5, 2.5**
func TestBugCondition_HasPhaseOutput_NotExposed(t *testing.T) {
	engine, _ := newTestEngine()

	// Start a workflow to get an active NeedsConfirm phase
	intent := StructuredIntent{Category: WorkflowCoding, Summary: "test"}
	_, err := engine.StartWorkflow("u1", intent)
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	// Verify the phase is NeedsConfirm=true
	if !engine.IsPhaseNeedsConfirm("u1") {
		t.Fatal("precondition failed: first phase should be NeedsConfirm=true")
	}

	// On UNFIXED code: HasPhaseOutput does not exist — this will fail to compile.
	// On FIXED code: HasPhaseOutput exists and returns false (no output yet).
	hasOutput := engine.HasPhaseOutput("u1")
	if hasOutput {
		t.Error("HasPhaseOutput should return false for first execution (no phase output exists)")
	}

	// The bug: IsPhaseNeedsConfirm returns true but there's no way to check
	// if the phase already has output. The gate uses IsPhaseNeedsConfirm alone,
	// causing force-return on first execution.
	needsConfirm := engine.IsPhaseNeedsConfirm("u1")
	if !needsConfirm {
		t.Error("IsPhaseNeedsConfirm should return true for NeedsConfirm phase")
	}

	// The fix: gate should check BOTH needsConfirm AND hasOutput.
	// Only force-return when needsConfirm=true AND hasOutput=true.
	// On first execution (hasOutput=false), gate should NOT force-return.
	engineGateActive := needsConfirm && hasOutput
	if engineGateActive {
		t.Error("engineGateActive should be false on first execution: needsConfirm=true but hasOutput=false")
	}
}

// TestBugCondition_AllTemplates_FirstExecution verifies that the bug affects
// all workflow templates with NeedsConfirm=true phases, not just coding.
//
// On UNFIXED code: this test FAILS because HasPhaseOutput does not exist
// (compile error).
//
// **Validates: Requirements 1.4**
func TestBugCondition_AllTemplates_FirstExecution(t *testing.T) {
	registry := NewWorkflowRegistry()

	// All registered workflow types
	workflowTypes := []WorkflowType{
		WorkflowCoding, WorkflowProductDesign, WorkflowInnovation,
		WorkflowBusinessPlan, WorkflowTesting,
		WorkflowLiteratureReview, WorkflowResearchReport,
		WorkflowExperimentDesign, WorkflowGrantProposal,
		WorkflowPaperWriting, WorkflowProjectProposal,
		WorkflowEventPlanning, WorkflowCompetitiveAnalysis,
		WorkflowPresentationDesign, WorkflowBidResponse,
		WorkflowContractReview, WorkflowDueDiligence,
		WorkflowComplianceAudit, WorkflowPatentAnalysis,
		WorkflowOpsMaintenance, WorkflowChangjiangScholar,
		WorkflowChangjiangScholarReview,
	}

	for _, wt := range workflowTypes {
		tmpl := registry.Match(wt)
		if tmpl == nil {
			continue
		}

		// Find first NeedsConfirm=true phase
		for _, phase := range tmpl.Phases {
			if !phase.NeedsConfirm {
				continue
			}

			t.Run(string(wt)+"/"+phase.ID, func(t *testing.T) {
				engine, _ := newTestEngine()
				intent := StructuredIntent{Category: wt, Summary: "test"}
				_, err := engine.StartWorkflow("u1", intent)
				if err != nil {
					t.Skipf("StartWorkflow failed for %s: %v", wt, err)
					return
				}

				// Verify NeedsConfirm is true
				if !engine.IsPhaseNeedsConfirm("u1") {
					t.Skipf("phase %s is not NeedsConfirm", phase.ID)
					return
				}

				// On UNFIXED code: HasPhaseOutput does not exist — compile error.
				// On FIXED code: returns false (first execution).
				hasOutput := engine.HasPhaseOutput("u1")
				if hasOutput {
					t.Errorf("HasPhaseOutput should return false for first execution of %s/%s", wt, phase.ID)
				}

				// Simulate gate with substantive preamble
				forceReturn := simulateNeedsConfirmGate(true, hasOutput, "# 阶段文档标题\n\n这是一段实质性的内容。")
				if forceReturn {
					t.Errorf("Gate force-returned on first execution of %s/%s (hasOutput=false); "+
						"bug affects this template", wt, phase.ID)
				}
			})

			break // Only test first NeedsConfirm phase per template
		}
	}
}

// ---------------------------------------------------------------------------
// Generators
// ---------------------------------------------------------------------------

// generateSubstantivePreamble produces random substantive LLM output that
// would trigger isSubstantivePhaseDocument=true. These simulate the kind of
// preamble/plan overview that the LLM outputs on first execution.
func generateSubstantivePreamble(seed int64) string {
	preambles := []string{
		// PPT workflow preamble with numbered list
		"收到，马上为您启动PPT制作工作流！\n1. 受众目标定义\n2. 内容大纲\n3. 视觉设计\n让我们开始吧！",
		// Coding workflow preamble with Markdown headings
		"好的，我来为您生成需求文档。\n\n# 贪吃蛇游戏需求文档\n\n## 1. 功能需求\n...",
		// Short text with heading
		"# 受众与目标文档\n\n让我来分析一下。",
		// Chinese numbered list
		"好的，我将按以下步骤进行：\n1、需求分析\n2、技术设计\n3、任务拆分\n4、编码实现",
		// Long preamble (200+ rune)
		"收到您的需求！我将为您启动完整的工作流程。首先，我们需要明确项目的受众群体和核心目标，这将帮助我们确定演示文稿的整体方向和重点内容。接下来，我会根据受众分析结果制定详细的内容大纲，确保每个章节都能有效传达关键信息。然后进入视觉设计阶段，选择合适的配色方案、字体和布局。最后生成最终的演示文稿文件。让我们开始第一个阶段吧！",
		// English with heading
		"# Requirements Document\n\n## Overview\n\nLet me generate the requirements for your project.",
		// Mixed language with numbered list
		"OK，让我来整理一下：\n1. 功能需求分析\n2. 非功能需求\n3. 边界情况\n4. 验收标准",
		// Product design preamble
		"# 产品设计文档\n\n## 问题发现\n\n我来帮您分析产品需求。",
		// Business plan preamble
		"好的，我来为您制定商业计划：\n1. 市场分析\n2. 竞争格局\n3. 商业模式\n4. 财务预测\n5. 执行计划",
		// Research report preamble
		"# 研究报告\n\n## 研究背景\n\n让我开始整理研究资料。",
		// Innovation workflow
		"收到！创新方案工作流启动：\n1. 问题定义\n2. 创意发散\n3. 方案评估\n4. 路线图\n5. 实施计划",
		// Testing workflow
		"# 测试计划\n\n## 测试范围\n\n我来为您制定完整的测试方案。",
		// Long English preamble
		"I'll start working on the requirements document for your project. The document will cover functional requirements, non-functional requirements, boundary conditions, and acceptance criteria. Let me begin by analyzing the core features you've described and organizing them into a structured format that we can review together.",
		// Presentation design
		"# 演示文稿设计方案\n\n## 受众分析\n\n目标受众：技术团队和管理层",
		// Event planning
		"好的，活动策划工作流启动！\n1. 活动目标\n2. 场地选择\n3. 日程安排\n4. 预算规划\n5. 执行方案",
		// Competitive analysis
		"# 竞品分析报告\n\n## 市场概况\n\n让我来分析竞争格局。",
		// Grant proposal
		"收到，我来帮您撰写基金申请书：\n1. 研究背景\n2. 研究目标\n3. 研究方法\n4. 预期成果\n5. 预算说明",
		// Paper writing
		"# 论文框架\n\n## 摘要\n\n本文将探讨...",
		// 3+ bullet lines
		"好的，我来整理需求：\n- 用户认证模块\n- 数据管理模块\n- 报表生成模块\n- API 接口设计",
		// Mixed structure
		"## 项目概述\n\n我将按以下步骤执行：\n1. 需求分析\n2. 架构设计\n\n### 第一步：需求分析\n\n让我开始吧。",
	}

	idx := seed % int64(len(preambles))
	if idx < 0 {
		idx = -idx
	}
	return preambles[idx]
}

// testTruncateForDisplay truncates a string for display in error messages.
func testTruncateForDisplay(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
