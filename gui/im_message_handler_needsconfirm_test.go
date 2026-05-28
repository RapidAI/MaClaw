package main

import (
	"strings"
	"testing"
	"testing/quick"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// Bug Condition Exploration Test — Property 1: Short Preamble Does Not
// Trigger Force-Return
//
// Feature: workflow-start-premature-exit
// Property 1: Bug Condition — Short Preamble Does Not Trigger Force-Return
//
// **Validates: Requirements 1.1, 1.2, 2.1**
//
// CRITICAL: This test is EXPECTED TO FAIL on unfixed code.
// Failure confirms the bug exists because isSubstantivePhaseDocument does
// not exist yet and the NeedsConfirm gate force-returns on all non-empty,
// non-stall text.
//
// The test encodes the expected (correct) behavior: short preamble strings
// that lack document structure markers (markdown headings, numbered lists,
// 3+ bullet lines) and are shorter than 200 runes MUST NOT be classified
// as substantive phase documents.
// ---------------------------------------------------------------------------

// TestProperty1_BugCondition_ShortPreambleDoesNotTriggerForceReturn verifies
// that isSubstantivePhaseDocument returns false for concrete short preamble
// inputs that the NeedsConfirm gate currently (incorrectly) force-returns on.
func TestProperty1_BugCondition_ShortPreambleDoesNotTriggerForceReturn(t *testing.T) {
	// Concrete counterexamples: all are short preambles (non-empty, non-stall,
	// no document structure) that the current gate condition
	//   trimmedForGate != ""
	// evaluates to true, causing an incorrect force-return.
	cases := []struct {
		name  string
		input string
	}{
		{
			name:  "Chinese preamble (42 chars)",
			input: "好的，准备开工！我将为您启动开发工作流...",
		},
		{
			name:  "English preamble (42 chars)",
			input: "OK, let me start working on this for you!",
		},
		{
			name:  "Mixed preamble",
			input: "好的！Let me prepare the requirements document.",
		},
		{
			name:  "Exactly 199 runes with no structure markers",
			input: strings.Repeat("a", 199),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Pre-conditions: input is non-empty, short, and
			// shorter than 200 runes — i.e. the bug condition holds on
			// unfixed code.
			trimmed := strings.TrimSpace(tc.input)
			if trimmed == "" {
				t.Fatal("precondition failed: input must be non-empty")
			}
			if utf8.RuneCountInString(trimmed) >= 200 {
				t.Fatalf("precondition failed: input must be < 200 runes, got %d", utf8.RuneCountInString(trimmed))
			}

			// The expected behavior: isSubstantivePhaseDocument returns false
			// for short preambles lacking document structure.
			//
			// On UNFIXED code this call will fail to compile because
			// isSubstantivePhaseDocument does not exist yet — that
			// compilation failure IS the proof that the bug exists (there
			// is no mechanism to distinguish preambles from documents).
			result := isSubstantivePhaseDocument(trimmed)
			if result {
				t.Errorf("isSubstantivePhaseDocument(%q) = true, want false; short preamble should NOT be classified as substantive document", tc.input)
			}
		})
	}
}

// TestProperty1_BugCondition_PBT_ShortPreambleNeverSubstantive uses
// testing/quick to generate random short strings (0-199 runes) that contain
// no markdown heading markers, no numbered list patterns, and fewer than 3
// bullet lines. For each, asserts isSubstantivePhaseDocument returns false.
//
// **Validates: Requirements 1.1, 1.2, 2.1**
func TestProperty1_BugCondition_PBT_ShortPreambleNeverSubstantive(t *testing.T) {
	cfg := &quick.Config{MaxCount: 200}

	// Property: for any short string without document structure,
	// isSubstantivePhaseDocument must return false.
	err := quick.Check(func(seed int64) bool {
		input := generateShortPreamble(seed)
		return !isSubstantivePhaseDocument(input)
	}, cfg)
	if err != nil {
		t.Errorf("Property violated: short preamble classified as substantive: %v", err)
	}
}

// generateShortPreamble produces a random string of 0-199 runes that does
// NOT contain markdown heading markers (lines starting with # ), numbered
// list patterns (lines starting with digits followed by . or 、), and has
// fewer than 3 bullet lines (lines starting with - or *).
//
// This generates inputs that satisfy the bug condition: non-empty, non-stall,
// not a substantive document.
func generateShortPreamble(seed int64) string {
	// Use a simple deterministic approach based on seed.
	// Mix of ASCII and CJK characters to simulate realistic preambles.
	preambles := []string{
		"好的，准备开工！",
		"OK, starting now!",
		"让我开始吧。",
		"I'll begin working on this.",
		"好的！我来帮你处理这个任务。",
		"Sure, let me get started right away.",
		"没问题，马上开始！",
		"Alright, working on it now.",
		"好的，我现在开始为您处理。",
		"Let me prepare everything for you.",
		"收到，开始执行！",
		"Got it, starting the task.",
		"明白了，开始工作。",
		"Understanding the requirements, beginning now.",
		"好，让我来看看这个问题。",
		"OK, let me look into this.",
		"",
		"a",
		strings.Repeat("x", 50),
		strings.Repeat("好", 99),
	}

	idx := seed % int64(len(preambles))
	if idx < 0 {
		idx = -idx
	}
	return preambles[idx]
}

// ---------------------------------------------------------------------------
// Preservation Property Tests — Property 2: Substantive Documents Still
// Force-Return
//
// Feature: workflow-start-premature-exit
// Property 2: Preservation — Substantive Documents Still Force-Return
//
// **Validates: Requirements 3.1, 3.4, 3.6, 3.7**
//
// These tests verify that isSubstantivePhaseDocument returns true for
// inputs containing document structure markers (markdown headings, numbered
// lists, 3+ bullet lines, or 200+ runes). On UNFIXED code these tests will
// fail to compile because isSubstantivePhaseDocument does not exist yet.
//
// IMPORTANT: On UNFIXED code, the NeedsConfirm gate already correctly
// force-returns on substantive documents (because the condition
// `trimmedForGate != ""` is true for all non-empty text). The fix must
// preserve this behavior by having isSubstantivePhaseDocument return true
// for all substantive inputs.
// ---------------------------------------------------------------------------

// TestProperty2_Preservation_SubstantiveDocumentsForceReturn verifies that
// isSubstantivePhaseDocument returns true for concrete substantive document
// inputs that the NeedsConfirm gate should force-return on.
//
// **Validates: Requirements 3.1, 3.4, 3.6, 3.7**
func TestProperty2_Preservation_SubstantiveDocumentsForceReturn(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{
			name:  "Chinese requirements doc with headings and numbered list (200+ chars)",
			input: "# 需求文档\n\n## 1. 功能需求\n\n1. 用户可以通过点击按钮启动工作流，系统将自动生成需求文档并展示在预览面板中\n2. 系统应当在用户确认需求后自动进入技术设计阶段，生成技术设计文档\n3. 任务拆分阶段应将工作分解为可独立执行的小任务，每个任务包含描述、涉及文件、依赖关系",
		},
		{
			name:  "English doc with Architecture heading (200+ runes)",
			input: "## Architecture\n\nThe system uses a layered architecture with clear separation of concerns. The presentation layer handles UI rendering and user interactions. The business logic layer contains core algorithms and workflow management. The data access layer manages persistent storage and caching strategies for optimal performance.",
		},
		{
			name:  "Numbered list pattern",
			input: "1. First item in the list\n2. Second item in the list",
		},
		{
			name:  "Three or more bullet lines",
			input: "- item one for testing\n- item two for testing\n- item three for testing",
		},
		{
			name:  "Empty string (gate does NOT force-return)",
			input: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			trimmed := strings.TrimSpace(tc.input)

			// Empty string and stall replies: isSubstantivePhaseDocument
			// should return false (gate doesn't force-return on these).
			if trimmed == "" {
				result := isSubstantivePhaseDocument(trimmed)
				if result {
					t.Errorf("isSubstantivePhaseDocument(%q) = true, want false; empty text is not substantive", tc.input)
				}
				return
			}

			// Substantive document: isSubstantivePhaseDocument must return true.
			result := isSubstantivePhaseDocument(trimmed)
			if !result {
				t.Errorf("isSubstantivePhaseDocument(%q) = false, want true; substantive document should be classified as such", tc.input)
			}
		})
	}
}

// TestProperty2_PBT_LongDocumentsAlwaysSubstantive uses testing/quick to
// generate random markdown documents of 200+ runes with heading markers
// and verifies isSubstantivePhaseDocument returns true.
//
// **Validates: Requirements 3.1, 3.4**
func TestProperty2_PBT_LongDocumentsAlwaysSubstantive(t *testing.T) {
	cfg := &quick.Config{MaxCount: 150}

	err := quick.Check(func(seed int64) bool {
		input := generateLongDocument(seed)
		if utf8.RuneCountInString(input) < 200 {
			// Generator invariant violated — skip this input.
			return true
		}
		return isSubstantivePhaseDocument(input)
	}, cfg)
	if err != nil {
		t.Errorf("Property violated: 200+ rune document not classified as substantive: %v", err)
	}
}

// TestProperty2_PBT_HeadingMarkersAlwaysSubstantive generates random strings
// containing markdown heading markers (# / ## ) and verifies
// isSubstantivePhaseDocument returns true regardless of length.
//
// **Validates: Requirements 3.1, 3.4**
func TestProperty2_PBT_HeadingMarkersAlwaysSubstantive(t *testing.T) {
	cfg := &quick.Config{MaxCount: 150}

	err := quick.Check(func(seed int64) bool {
		input := generateStringWithHeading(seed)
		return isSubstantivePhaseDocument(input)
	}, cfg)
	if err != nil {
		t.Errorf("Property violated: string with heading marker not classified as substantive: %v", err)
	}
}

// TestProperty2_PBT_NumberedListAlwaysSubstantive generates random strings
// containing numbered list patterns (1. , 2. , 1、) and verifies
// isSubstantivePhaseDocument returns true.
//
// **Validates: Requirements 3.1, 3.4**
func TestProperty2_PBT_NumberedListAlwaysSubstantive(t *testing.T) {
	cfg := &quick.Config{MaxCount: 150}

	err := quick.Check(func(seed int64) bool {
		input := generateStringWithNumberedList(seed)
		return isSubstantivePhaseDocument(input)
	}, cfg)
	if err != nil {
		t.Errorf("Property violated: string with numbered list not classified as substantive: %v", err)
	}
}

// TestProperty2_PBT_ThreePlusBulletsAlwaysSubstantive generates random
// strings with 3+ bullet list lines and verifies isSubstantivePhaseDocument
// returns true.
//
// **Validates: Requirements 3.1, 3.4**
func TestProperty2_PBT_ThreePlusBulletsAlwaysSubstantive(t *testing.T) {
	cfg := &quick.Config{MaxCount: 150}

	err := quick.Check(func(seed int64) bool {
		input := generateStringWithBullets(seed)
		return isSubstantivePhaseDocument(input)
	}, cfg)
	if err != nil {
		t.Errorf("Property violated: string with 3+ bullets not classified as substantive: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Generators for preservation property tests
// ---------------------------------------------------------------------------

// generateLongDocument produces a random markdown document of 200+ runes
// with heading markers and/or numbered lists.
func generateLongDocument(seed int64) string {
	headings := []string{
		"# 需求文档",
		"## Architecture",
		"### Implementation Plan",
		"# Requirements",
		"## 功能需求",
		"# Design Document",
	}

	bodies := []string{
		"The system processes user input through multiple layers of validation and transformation before storing results in the database.",
		"用户可以通过界面操作触发工作流引擎执行预定义的阶段流程，每个阶段都有独立的质量门禁检查和确认机制。",
		"This component manages the lifecycle of agent loop iterations including tool calls, content generation, and gate evaluations.",
		"系统架构采用分层设计，表现层负责渲染和交互，业务层包含核心算法和流程管理，数据层管理持久化存储。",
	}

	hidx := seed % int64(len(headings))
	if hidx < 0 {
		hidx = -hidx
	}
	bidx := (seed / int64(len(headings))) % int64(len(bodies))
	if bidx < 0 {
		bidx = -bidx
	}

	doc := headings[hidx] + "\n\n" + bodies[bidx]

	// Ensure 200+ runes by padding if needed.
	for utf8.RuneCountInString(doc) < 200 {
		doc += "\n\nAdditional content to ensure sufficient document length for the property test. 额外内容确保文档长度满足测试要求。"
	}
	return doc
}

// generateStringWithHeading produces a random string containing a markdown
// heading marker at the start of a line. May be short or long.
func generateStringWithHeading(seed int64) string {
	templates := []string{
		"# Title",
		"## Section Header",
		"### Subsection",
		"#### Deep heading",
		"# 需求文档",
		"## 技术设计",
		"### 任务列表",
		"Some preamble text\n# Document Title",
		"Brief intro\n## Architecture Overview",
		"Hello\n### Details Here",
		"# A",
		"## B",
		"前言内容\n# 正文标题",
		"Intro paragraph.\n## Design Decisions",
		"Short.\n### Implementation Notes",
		"###### Smallest heading level",
		"Text before\n###### Deep heading content",
		"# 功能需求\n\n具体描述",
		"## Overview\n\nContent follows",
		"### Step One\n\nDo the thing.",
	}

	idx := seed % int64(len(templates))
	if idx < 0 {
		idx = -idx
	}
	return templates[idx]
}

// generateStringWithNumberedList produces a random string containing
// numbered list patterns (1. , 2. , 1、, etc.).
func generateStringWithNumberedList(seed int64) string {
	templates := []string{
		"1. First item\n2. Second item",
		"1. 用户可以操作\n2. 系统应当响应",
		"Steps:\n1. Initialize the system\n2. Run validation",
		"1、第一步操作\n2、第二步操作",
		"Requirements:\n1. Must be fast\n2. Must be reliable",
		"3. Third step continues\n4. Fourth step finishes",
		"Tasks:\n1. Create module\n2. Write tests\n3. Deploy",
		"1. Setup\n2. Configure\n3. Test\n4. Ship",
		"1. Single item is enough",
		"Begin.\n1. First action item here\n2. Second action",
		"Introduction text.\n1. Step one of the process\n2. Step two",
		"1、开始\n2、执行\n3、完成",
		"Outline:\n1. Design\n2. Implement\n3. Verify",
		"1. Read input\n2. Process data",
		"Plan:\n1. Research\n2. Prototype\n3. Build\n4. Test",
		"1. Alpha\n2. Beta",
		"1. 功能一\n2. 功能二\n3. 功能三",
		"Some context.\n1. Action A\n2. Action B",
		"1. Parse arguments\n2. Validate inputs\n3. Execute logic",
		"1、需求分析\n2、系统设计",
	}

	idx := seed % int64(len(templates))
	if idx < 0 {
		idx = -idx
	}
	return templates[idx]
}

// generateStringWithBullets produces a random string containing 3 or more
// bullet list lines (lines starting with - or *).
func generateStringWithBullets(seed int64) string {
	templates := []string{
		"- item one\n- item two\n- item three",
		"- first\n- second\n- third\n- fourth",
		"* alpha\n* beta\n* gamma",
		"Notes:\n- point A\n- point B\n- point C",
		"- 功能一\n- 功能二\n- 功能三",
		"* 需求一\n* 需求二\n* 需求三\n* 需求四",
		"Items:\n- apple\n- banana\n- cherry",
		"- Read\n- Write\n- Execute",
		"Checklist:\n- Design review\n- Code review\n- Test coverage",
		"- module_a\n- module_b\n- module_c\n- module_d\n- module_e",
		"Features:\n* Fast loading\n* Responsive UI\n* Offline support",
		"- 检查输入\n- 处理逻辑\n- 返回结果",
		"* Step 1 done\n* Step 2 done\n* Step 3 done",
		"Bugs:\n- Fix crash\n- Fix layout\n- Fix performance",
		"- TypeScript\n- Go\n- Python",
		"* 界面设计\n* 后端开发\n* 数据库优化",
		"Tasks:\n- Analyze\n- Implement\n- Verify",
		"- Low priority\n- Medium priority\n- High priority",
		"* one\n* two\n* three\n* four\n* five",
		"- a\n- b\n- c",
	}

	idx := seed % int64(len(templates))
	if idx < 0 {
		idx = -idx
	}
	return templates[idx]
}
