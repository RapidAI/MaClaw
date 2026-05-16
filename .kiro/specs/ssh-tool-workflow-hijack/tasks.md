# Implementation Plan

- [x] 1. Write bug condition exploration test
  - **Property 1: Bug Condition** - Section 4 Default Branch Returns DefaultInput=false
  - **CRITICAL**: This test MUST FAIL on unfixed code — failure confirms the bug exists
  - **DO NOT attempt to fix the test or the code when it fails**
  - **NOTE**: This test encodes the expected behavior — it will validate the fix when it passes after implementation
  - **GOAL**: Surface counterexamples that demonstrate HandleInput section 4 returns DefaultInput=false, causing workflowAgentLoopMarker to be set unconditionally
  - **Scoped PBT Approach**: Generate inputs that reach HandleInput section 4 (default branch):
    - Active workflow with `hasOutput=false` for current phase
    - Input text does NOT match confirm words (e.g., "确认", "OK")
    - Input text does NOT match skip words (e.g., "跳过")
    - Phase is NOT in `NeedsConfirm=true AND hasOutput=true` state (not PendingConfirm)
    - Input is NOT caught by input-waiting gate (`IsWaitingForInput=false`)
  - Test that `HandleInput` returns `DefaultInput == true` for all such inputs (expected behavior from design)
  - Concrete test cases from Bug Condition in design:
    - Coding workflow at `requirements` phase with no output, user sends "check server status" → assert `DefaultInput == true`
    - Coding workflow at `requirements` phase with no output, user sends "开工" → assert `DefaultInput == true`
    - PPT workflow at `content_outline` phase with no output, user sends "hello world" → assert `DefaultInput == true`
    - Any workflow type at first phase with no output, random non-confirm/non-skip text → assert `DefaultInput == true`
  - Also verify `RunAgentLoop == true` and `PhasePrompt != ""` for all section 4 inputs
  - Run test on UNFIXED code
  - **EXPECTED OUTCOME**: Test FAILS (section 4 currently returns `DefaultInput=false` — Go zero value — this proves the bug exists)
  - Document counterexamples: all inputs reaching section 4 return `DefaultInput=false`, which causes `handleActiveWorkflow` to set `workflowAgentLoopMarker=true` unconditionally
  - Mark task complete when test is written, run, and failure is documented
  - _Requirements: 1.1, 1.2, 2.2_

- [x] 2. Write preservation property tests (BEFORE implementing fix)
  - **Property 2: Preservation** - Sections 0-3 Behavior Unchanged
  - **IMPORTANT**: Follow observation-first methodology
  - **Observe on UNFIXED code** the following non-bug-condition behaviors:
    - Section 0 (input-waiting): `IsWaitingForInput=true` + non-substantial input → returns reminder text with `RunAgentLoop=false` ✅
    - Section 1 (skip words): skip word during `CanSkip=true` phase → `advancePhase` called, `Advance=true` ✅
    - Section 1 (skip words): skip word during `CanSkip=false` phase → rejection text with `RunAgentLoop=false` ✅
    - Section 2 (confirm words): confirm word during `NeedsConfirm=true` + `hasOutput=true` → `advancePhase` called, `Advance=true` ✅
    - Section 3 (PendingConfirm): any text during `NeedsConfirm=true` + `hasOutput=true` (non-confirm) → `PendingConfirm=true` ✅
  - Write property-based tests capturing these observed behaviors:
    - **Sub-property 2a**: For all confirm words during `NeedsConfirm=true` + `hasOutput=true` phases → `Advance=true` (section 2 preservation, from design Preservation Requirements)
    - **Sub-property 2b**: For all skip words during `CanSkip=true` phases → `Advance=true` (section 1 preservation)
    - **Sub-property 2c**: For all skip words during `CanSkip=false` phases → `RunAgentLoop=false` + rejection text (section 1 preservation)
    - **Sub-property 2d**: For all non-confirm text during `NeedsConfirm=true` + `hasOutput=true` phases → `PendingConfirm=true` (section 3 preservation)
    - **Sub-property 2e**: For all non-substantial input during `IsWaitingForInput=true` → `RunAgentLoop=false` + reminder text (section 0 preservation)
  - Generate random inputs: various confirm words ("确认", "OK", "好的"), skip words ("跳过"), random non-matching text, various workflow types and phase configurations
  - Verify all tests PASS on UNFIXED code
  - **EXPECTED OUTCOME**: Tests PASS (confirms baseline behavior to preserve)
  - Mark task complete when tests are written, run, and passing on unfixed code
  - _Requirements: 3.1, 3.2, 3.3, 3.5, 3.8_

- [x] 3. Implement the fix — Add DefaultInput: true to HandleInput section 4

  - [x] 3.1 Add `DefaultInput: true` to the section 4 WorkflowResponse in `corelib/workflow/engine.go`
    - In `HandleInput`, locate section 4 (default branch, approximately line 234-242)
    - Add `DefaultInput: true` to the `WorkflowResponse` struct literal:
      ```go
      return &WorkflowResponse{
          Text:         "",
          PhasePrompt:  phasePrompt,
          ToolFilter:   phase.ToolPolicy,
          RunAgentLoop: true,
          DefaultInput: true,  // ← ADD THIS LINE
      }, nil
      ```
    - This is the ONLY code change required — one line addition
    - _Bug_Condition: isBugCondition(input) where input reaches section 4 default branch AND DefaultInput is Go zero value (false)_
    - _Expected_Behavior: DefaultInput=true signals to handleActiveWorkflow that this is a default fallthrough, so workflowAgentLoopMarker is NOT set_
    - _Preservation: Sections 0, 1, 2, 3 are completely unaffected — they return before reaching section 4_
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 3.1, 3.2, 3.3, 3.5, 3.8_

  - [x] 3.2 Verify bug condition exploration test now passes
    - **Property 1: Expected Behavior** - Section 4 Default Branch Returns DefaultInput=true
    - **IMPORTANT**: Re-run the SAME test from task 1 — do NOT write a new test
    - The test from task 1 asserts `DefaultInput == true` for all section 4 inputs
    - When this test passes, it confirms: section 4 now returns `DefaultInput=true`, so `handleActiveWorkflow` does NOT set `workflowAgentLoopMarker`, and the message falls through to the normal agent loop with the full tool list
    - Run bug condition exploration test from step 1
    - **EXPECTED OUTCOME**: Test PASSES (confirms bug is fixed)
    - _Requirements: 2.1, 2.2, 2.3_

  - [x] 3.3 Verify preservation tests still pass
    - **Property 2: Preservation** - Sections 0-3 Behavior Unchanged
    - **IMPORTANT**: Re-run the SAME tests from task 2 — do NOT write new tests
    - Run preservation property tests from step 2
    - **EXPECTED OUTCOME**: Tests PASS (confirms no regressions)
    - Confirm all preservation sub-properties still hold:
      - 2a: Confirm words with output still advance phase
      - 2b: Skip words on CanSkip phases still advance
      - 2c: Skip words on non-CanSkip phases still reject
      - 2d: Non-confirm text on NeedsConfirm+hasOutput phases still return PendingConfirm
      - 2e: Non-substantial input on input-waiting phases still return reminder

- [x] 4. Checkpoint — Ensure all tests pass
  - Run full test suite: `go test ./corelib/workflow/...`
  - Ensure Property 1 (bug condition → DefaultInput=true) passes
  - Ensure Property 2 (sections 0-3 preservation) passes
  - Ensure all existing engine tests pass (TestEngine_HandleInputNoActiveWorkflow, TestEngine_CompleteWorkflowLifecycle, etc.)
  - Ensure all existing property tests pass (Property 7, 9, 10, 11, 15, 16, 17)
  - Ask the user if questions arise
