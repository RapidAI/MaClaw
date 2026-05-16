# Implementation Plan

- [ ] 1. Write bug condition exploration test
  - **Property 1: Bug Condition** - Supplementary Info Misclassified as "other" During NeedsConfirm
  - **CRITICAL**: This test MUST FAIL on unfixed code — failure confirms the bug exists
  - **DO NOT attempt to fix the test or the code when it fails**
  - **NOTE**: This test encodes the expected behavior — it will validate the fix when it passes after implementation
  - **GOAL**: Surface counterexamples that demonstrate the bug exists in `handlePendingConfirm`
  - **Scoped PBT Approach**: Scope the property to concrete failing cases — supplementary/additive user messages during NeedsConfirm phases with existing output
  - **Bug Condition from design**:
    ```
    isBugCondition(input) =
      hasActiveWorkflow(input.userID)
      AND currentPhase(input.userID).NeedsConfirm = true
      AND hasPhaseOutput(input.userID, currentPhase(input.userID).ID)
      AND NOT matchesConfirmWords(input.text)
      AND NOT matchesSkipWords(input.text)
      AND handlePendingConfirmClassify(input.text) = "other"
    ```
  - Test file: `gui/im_message_handler_workflow_pendingconfirm_test.go`
  - Create a mock LLM classify that simulates the current (unfixed) behavior: returns "other" for additive/supplementary messages
  - Generate test inputs from concrete bug examples:
    - "需要音效，需要偷东西的中间目标" (additive requirements)
    - "还需要一个排行榜功能" (feature addition)
    - "用 C++ 和 cmake" (constraint addition)
    - "数据库用 PostgreSQL" (design phase supplement)
  - For each input satisfying the bug condition, assert the **expected** behavior:
    - `handlePendingConfirm` returns nil (falls through to agent loop)
    - `workflowAgentLoopMarker` is set to true for the user
    - `stashedPhasePrompt` contains the user's supplementary text as modification context
  - Also test LLM classify failure path: mock LLM returning error → assert fallback treats as "modify" (not "confirm")
  - Run test on UNFIXED code
  - **EXPECTED OUTCOME**: Test FAILS — on unfixed code, "other" classification causes `workflowAgentLoopMarker` to NOT be set, and LLM failure falls back to "confirm" (advancing with stale output)
  - Document counterexamples found:
    - "需要音效" → LLM returns "other" → `workflowAgentLoopMarker` not set → stall
    - LLM timeout → fallback to "confirm" → `advanceAndRespond` with stale v1 output → supplementary info lost
  - Mark task complete when test is written, run, and failure is documented
  - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 2.1, 2.2, 2.3, 2.4_

- [ ] 2. Write preservation property tests (BEFORE implementing fix)
  - **Property 2: Preservation** - Confirm/Skip Words, Unrelated Queries, and Phase-Agnostic Behavior
  - **IMPORTANT**: Follow observation-first methodology
  - Test file: `gui/im_message_handler_workflow_pendingconfirm_test.go` (same file, separate test functions)
  - **Observation phase** — run on UNFIXED code and record actual outputs:
    - Observe: confirmWords ("确认", "OK", "没问题") are intercepted by `HandleInput` before reaching `handlePendingConfirm` → `advancePhase` called
    - Observe: skipWords ("跳过", "skip") during CanSkip phases are intercepted by `HandleInput` → `advancePhase` called
    - Observe: explicit modify indicators ("把技术栈改成 React") → LLM classify returns "modify" → `workflowAgentLoopMarker` set, `stashedPhasePrompt` contains modify prompt
    - Observe: unrelated short queries ("查询天气", "今天几号", "几点了") → LLM classify returns "other" → returns nil without markers (passthrough to normal agent loop)
    - Observe: same behavior across multiple phase types (requirements, design, task_breakdown)
  - **Property-based tests capturing observed behavior**:
    - **Confirm words preservation**: For all messages matching `confirmWords`, `HandleInput` returns advance response — `handlePendingConfirm` is never reached
    - **Skip words preservation**: For all messages matching `skipWords` during CanSkip phases, `HandleInput` returns skip response
    - **Explicit modify preservation**: For all messages matching `modifyIndicators`, the modify path is triggered with `workflowAgentLoopMarker` set
    - **Unrelated query passthrough**: For genuinely unrelated short queries (< 10 chars, no domain keywords), `handlePendingConfirm` returns nil without setting `workflowAgentLoopMarker`
    - **Phase-agnostic behavior**: For any NeedsConfirm phase (requirements, design, task_breakdown, audience_goal, etc.), the classification behavior is identical
  - Verify all tests PASS on UNFIXED code
  - **EXPECTED OUTCOME**: Tests PASS — these capture existing correct behavior that must be preserved
  - Mark task complete when tests are written, run, and passing on unfixed code
  - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8, 3.9_

- [ ] 3. Fix for supplementary info misclassification during NeedsConfirm phases

  - [ ] 3.1 Enhance LLM classify system prompt in `handlePendingConfirm`
    - File: `gui/im_message_handler_workflow.go`, function `handlePendingConfirm`
    - Update the system prompt's "modify" category definition from "user wants to change or update the document" to "user wants to change, update, add to, or supplement the document with new information, requirements, or constraints"
    - Add explicit examples to the system prompt:
      - "Adding new requirements, specifying constraints, providing additional details, supplementing with new features = modify"
      - "Asking about weather, asking the time, asking unrelated questions = other"
    - This addresses Root Cause #1 from design: LLM prompt too narrow on "modify"
    - _Bug_Condition: isBugCondition(input) where handlePendingConfirmClassify(input.text) = "other" for additive info_
    - _Expected_Behavior: LLM classify returns "modify" for supplementary/additive messages_
    - _Preservation: Explicit modify indicators ("把技术栈改成 React") still classified as "modify"; unrelated queries still classified as "other"_
    - _Requirements: 2.1, 2.2, 2.3, 2.4_

  - [ ] 3.2 Change "other" fallback to treat as "modify" during PendingConfirm
    - File: `gui/im_message_handler_workflow.go`, function `handlePendingConfirm`
    - In the `default` case (currently returns nil without markers):
      - Set `workflowAgentLoopMarker.Store(userID, true)`
      - Build modify prompt with phase context + user's text
      - Store in `stashedPhasePrompt.Store(userID, modifyPrompt)`
      - Return nil (fall through to agent loop with markers set)
    - Rationale from design: `HandleInput` already filters confirmWords and skipWords before `PendingConfirm`. If the message reached `handlePendingConfirm`, it's neither confirm nor skip. Treating ambiguous messages as modifications is safer than losing them.
    - Add info-level logging: `"[workflow-confirm] 'other' intent during PendingConfirm, treating as modify: user=%s text=%q"`
    - This addresses Root Cause #2 from design: "other" path returns nil without safety net
    - _Bug_Condition: handlePendingConfirmClassify(input.text) = "other" during PendingConfirm_
    - _Expected_Behavior: "other" treated as "modify" — workflowAgentLoopMarker set, stashedPhasePrompt contains user text_
    - _Preservation: Confirm words and skip words are intercepted before reaching this code path_
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 3.4_

  - [ ] 3.3 Change LLM classify failure fallback from "confirm" to "modify"
    - File: `gui/im_message_handler_workflow.go`, function `handlePendingConfirm`
    - In the `if err != nil` block (currently calls `advanceAndRespond`):
      - Replace `return h.advanceAndRespond(engine, userID)` with modify fallback logic
      - Build modify prompt with phase context + user's text
      - Store in `stashedPhasePrompt` and set `workflowAgentLoopMarker`
      - Return nil (fall through to agent loop)
    - Update log message: `"[workflow-confirm] LLM classify failed, falling back to modify (safer than advancing with stale output): %v"`
    - Rationale from design: Losing user input (advancing with stale output) is worse than re-generating the document. If the user actually wanted to confirm, they can confirm again.
    - This addresses Root Cause #4 from design: LLM classify failure fallback is destructive
    - _Bug_Condition: LLM classify call fails (timeout, network error) during PendingConfirm_
    - _Expected_Behavior: Fallback to modify — agent loop runs with user's text as modification context_
    - _Preservation: Successful LLM classify results (confirm/modify) are unaffected_
    - _Requirements: 1.6, 2.1_

  - [ ] 3.4 Verify bug condition exploration test now passes
    - **Property 1: Expected Behavior** - Supplementary Info Treated as Modify
    - **IMPORTANT**: Re-run the SAME test from task 1 — do NOT write a new test
    - The test from task 1 encodes the expected behavior (workflowAgentLoopMarker set, stashedPhasePrompt contains user text)
    - After the fix in 3.1-3.3:
      - Supplementary messages → LLM classify returns "modify" (prompt enhancement) OR "other" → fallback treats as "modify" (safety net)
      - LLM failure → fallback to modify (not confirm)
    - Run bug condition exploration test from step 1
    - **EXPECTED OUTCOME**: Test PASSES (confirms bug is fixed and expected behavior is satisfied)
    - _Requirements: 2.1, 2.2, 2.3, 2.4_

  - [ ] 3.5 Verify preservation tests still pass
    - **Property 2: Preservation** - Confirm/Skip/Unrelated/Phase-Agnostic Behavior Unchanged
    - **IMPORTANT**: Re-run the SAME tests from task 2 — do NOT write new tests
    - Run preservation property tests from step 2
    - **EXPECTED OUTCOME**: Tests PASS (confirms no regressions)
    - Confirm all preservation tests still pass after fix:
      - confirmWords still advance phases
      - skipWords still skip phases
      - explicit modify indicators still trigger modify path
      - unrelated queries still pass through to normal agent loop
      - behavior is identical across all NeedsConfirm phases
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.9_

- [ ] 4. Checkpoint - Ensure all tests pass
  - Run the full test suite for the `gui` package: `go test ./gui/... -run TestPendingConfirm -v`
  - Ensure both Property 1 (bug condition → expected behavior) and Property 2 (preservation) tests pass
  - Ensure no existing tests in `gui/` are broken by the changes
  - Run `go vet ./gui/...` to check for compilation issues
  - If any test fails, investigate and fix before marking complete
  - Ask the user if questions arise
