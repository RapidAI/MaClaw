# Implementation Plan

- [x] 1. Write bug condition exploration test
  - **Property 1: Bug Condition** - Short Preamble Does Not Trigger Force-Return
  - **CRITICAL**: This test MUST FAIL on unfixed code - failure confirms the bug exists
  - **DO NOT attempt to fix the test or the code when it fails**
  - **NOTE**: This test encodes the expected behavior - it will validate the fix when it passes after implementation
  - **GOAL**: Surface counterexamples that demonstrate the NeedsConfirm gate incorrectly force-returns on short preamble text
  - **Scoped PBT Approach**: Use `testing/quick` to generate random short strings (0-199 runes, no markdown heading markers, no numbered list patterns, fewer than 3 bullet lines). For each generated input, assert that `isSubstantivePhaseDocument(input)` returns `false`. Since the function does not exist yet on unfixed code, scope to concrete failing cases:
    - `"好的，准备开工！我将为您启动开发工作流..."` (42 chars, no structure markers) — gate should NOT force-return
    - `"OK, let me start working on this for you!"` (38 chars, no structure markers) — gate should NOT force-return
    - `"好的！Let me prepare the requirements document."` (mixed, no structure markers) — gate should NOT force-return
    - A string of exactly 199 runes with no structure markers — gate should NOT force-return
  - Test that on UNFIXED code, the NeedsConfirm gate condition `trimmedForGate != "" && !looksLikeNoToolStallReply(msgContent)` evaluates to `true` for all these inputs (confirming force-return happens when it shouldn't)
  - Run test on UNFIXED code
  - **EXPECTED OUTCOME**: Test FAILS (this is correct - it proves the bug exists because `isSubstantivePhaseDocument` doesn't exist yet and the gate force-returns on all non-empty, non-stall text)
  - Document counterexamples found: all short preamble inputs trigger force-return due to the overly loose `trimmedForGate != ""` condition
  - Mark task complete when test is written, run, and failure is documented
  - _Requirements: 1.1, 1.2, 2.1_

- [x] 2. Write preservation property tests (BEFORE implementing fix)
  - **Property 2: Preservation** - Substantive Documents Still Force-Return
  - **IMPORTANT**: Follow observation-first methodology
  - Observe behavior on UNFIXED code for substantive document inputs (cases where `isBugCondition` returns false):
    - Observe: `"# 需求文档\n\n## 1. 功能需求\n\n1. 用户可以...\n2. 系统应当..."` (200+ chars, has headings + numbered list) — gate force-returns ✅
    - Observe: A 200+ rune string with `## Architecture` heading — gate force-returns ✅
    - Observe: A string with `1. First item\n2. Second item` numbered list — gate force-returns ✅
    - Observe: A string with 3+ bullet lines (`- item1\n- item2\n- item3`) — gate force-returns ✅
    - Observe: Empty string or stall reply (`"让我先分析一下需求..."`) — gate does NOT force-return ✅
  - Write property-based tests using `testing/quick` (100+ iterations):
    - Generate random markdown documents (200+ runes with headings/lists) → verify `isSubstantivePhaseDocument` returns `true`
    - Generate random strings containing `# ` / `## ` heading markers → verify returns `true` regardless of length
    - Generate random strings containing numbered list patterns (`1. `, `2. `, `1、`) → verify returns `true`
    - Generate random strings with 3+ bullet list lines → verify returns `true`
    - Verify gate force-return decision matches `isSubstantivePhaseDocument` output for all substantive inputs
  - Since `isSubstantivePhaseDocument` doesn't exist on unfixed code, write the tests targeting the function signature from the design doc; verify existing gate behavior for substantive documents (they already force-return correctly)
  - Run tests on UNFIXED code
  - **EXPECTED OUTCOME**: Tests PASS for the existing correct behavior (substantive documents already trigger force-return on unfixed code)
  - Mark task complete when tests are written, run, and passing on unfixed code
  - _Requirements: 3.1, 3.4, 3.6, 3.7_

- [x] 3. Fix for NeedsConfirm gate premature force-return on short preamble text

  - [x] 3.1 Add `isSubstantivePhaseDocument` function to `gui/im_message_handler.go`
    - Add new standalone function `isSubstantivePhaseDocument(text string) bool` (~20 lines)
    - Accept already-trimmed text (post `stripThinkingTags` + `TrimSpace`)
    - Return `true` if ANY of the following conditions hold:
      - `len([]rune(text)) >= 200` — sufficient length to be a document
      - Text contains Markdown heading markers: regex `(?m)^#{1,6}\s+\S` matches
      - Text contains numbered list patterns: regex `(?m)^(?:\d+[\.\、])\s+\S` matches
      - Text contains 3+ bullet list lines: count lines matching `(?m)^[-*]\s+\S` >= 3
    - Return `false` otherwise (short preamble / transitional text)
    - _Bug_Condition: isBugCondition(input) where NeedsConfirmActive=true AND trimmed != "" AND NOT looksLikeNoToolStallReply AND NOT isSubstantivePhaseDocument_
    - _Expected_Behavior: isSubstantivePhaseDocument correctly classifies short preambles as false and substantive documents as true_
    - _Preservation: All existing gate behavior for substantive documents, empty text, stall replies unchanged_
    - _Requirements: 2.1, 2.2, 2.3_

  - [x] 3.2 Modify NeedsConfirm gate condition in no-tool branch (~line 4716)
    - Change from: `if trimmedForGate != "" && !looksLikeNoToolStallReply(msgContent) {`
    - Change to: `if trimmedForGate != "" && !looksLikeNoToolStallReply(msgContent) && isSubstantivePhaseDocument(trimmedForGate) {`
    - Add trace logging when preamble is skipped: `"NeedsConfirm gate: skipping non-substantive preamble (len=%d), allowing loop to continue"`
    - _Bug_Condition: This is the primary fix location — the overly loose condition that caused premature force-return_
    - _Expected_Behavior: Short preambles no longer trigger force-return; substantive documents still do_
    - _Preservation: All other code paths in the no-tool branch (empty text recovery, stall detection, hard cap) remain unchanged_
    - _Requirements: 1.1, 1.2, 2.1, 2.2, 2.3, 3.3, 3.5, 3.6_

  - [x] 3.3 Modify NeedsConfirm gate condition in tool branch (~line 5412)
    - Apply the same `isSubstantivePhaseDocument` check to the tool branch for consistency
    - Change to: `if trimmedAfterTools != "" && !looksLikeNoToolStallReply(msgContent) && isSubstantivePhaseDocument(trimmedAfterTools) {`
    - _Bug_Condition: Tool branch has the same overly loose condition_
    - _Expected_Behavior: Consistent behavior across both branches_
    - _Preservation: Tool execution, coding tool gate, tool filtering all unchanged_
    - _Requirements: 2.2, 3.2, 3.4_

  - [x] 3.4 Verify bug condition exploration test now passes
    - **Property 1: Expected Behavior** - Short Preamble Does Not Trigger Force-Return
    - **IMPORTANT**: Re-run the SAME test from task 1 - do NOT write a new test
    - The test from task 1 encodes the expected behavior via `isSubstantivePhaseDocument`
    - When this test passes, it confirms short preambles are correctly classified and no longer trigger force-return
    - Run bug condition exploration test from step 1
    - **EXPECTED OUTCOME**: Test PASSES (confirms bug is fixed — `isSubstantivePhaseDocument` now exists and returns `false` for short preambles)
    - _Requirements: 2.1, 2.3_

  - [x] 3.5 Verify preservation tests still pass
    - **Property 2: Preservation** - Substantive Documents Still Force-Return
    - **IMPORTANT**: Re-run the SAME tests from task 2 - do NOT write new tests
    - Run preservation property tests from step 2
    - **EXPECTED OUTCOME**: Tests PASS (confirms no regressions — substantive documents still force-return correctly)
    - Confirm all tests still pass after fix (no regressions)
    - _Requirements: 3.1, 3.4_

- [x] 4. Checkpoint - Ensure all tests pass
  - Run `go test ./gui/ -run "TestProperty|TestIsSubstantive"` to verify all property and unit tests pass
  - Run `go build ./gui/` to verify compilation
  - Ensure all tests pass, ask the user if questions arise
