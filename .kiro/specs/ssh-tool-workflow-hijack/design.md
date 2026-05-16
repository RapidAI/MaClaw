# SSH Tool Workflow Hijack Bugfix Design

## Overview

When a user has an active workflow whose current phase has no output yet (`hasOutput=false`), any user message — including completely unrelated requests like SSH server operations — gets unconditionally hijacked into the workflow agent loop. The `HandleInput` section 4 (default branch) returns `RunAgentLoop=true` with `DefaultInput=false`, causing `handleActiveWorkflow` to set `workflowAgentLoopMarker=true`. The subsequent agent loop runs in workflow mode where `applyWorkflowToolFilter` strips all conditional tools (SSH, browser, web_search, etc.), leaving the LLM unable to execute the user's actual request.

The fix is minimal: set `DefaultInput=true` in the section 4 response. The existing `if !resp.DefaultInput` guard in `handleActiveWorkflow` already prevents the marker from being set when `DefaultInput=true`, so the message falls through to the normal agent loop with the full tool list.

## Glossary

- **Bug_Condition (C)**: User sends any message while an active workflow phase has `hasOutput=false` AND the message does not match confirm/skip words AND the phase is not in `NeedsConfirm+hasOutput` state — i.e., the message reaches `HandleInput` section 4 (default branch)
- **Property (P)**: The section 4 response must have `DefaultInput=true`, so `handleActiveWorkflow` does NOT set `workflowAgentLoopMarker`, and the message falls through to the normal agent loop with the full tool list
- **Preservation**: All other `HandleInput` branches (confirm words → `advancePhase`, skip words → `advancePhase`/reject, `PendingConfirm` → LLM classification, input-waiting → reminder) must remain unchanged
- **HandleInput**: `WorkflowEngine.HandleInput()` in `corelib/workflow/engine.go` — the state machine that processes user input within an active workflow
- **handleActiveWorkflow**: `IMMessageHandler.handleActiveWorkflow()` in `gui/im_message_handler_workflow.go` — the GUI layer that consumes `WorkflowResponse` and sets agent loop markers
- **workflowAgentLoopMarker**: A `sync.Map` flag that, when set, causes the agent loop to run in workflow mode with `PhasePrompt` injection and `doc_only` tool filtering
- **applyWorkflowToolFilter**: Function in `gui/im_message_handler.go` that strips conditional tools (SSH, browser, etc.) when the workflow phase has `ToolFilterPolicy=doc_only`

## Bug Details

### Bug Condition

The bug manifests when a user has an active workflow whose current phase has no output yet, and the user sends any message that does not match confirm/skip words and is not in the `PendingConfirm` state. The `HandleInput` function falls through to section 4 (default branch) which unconditionally returns `RunAgentLoop=true` with `DefaultInput=false` (Go zero value), regardless of whether the user's message is related to the workflow.

**Formal Specification:**
```
FUNCTION isBugCondition(input)
  INPUT: input of type {userID: string, text: string, workflowState: WorkflowState}
  OUTPUT: boolean

  phase := currentPhase(input.workflowState)
  trimmed := trimSpace(input.text)
  _, hasOutput := input.workflowState.PhaseOutputs[input.workflowState.CurrentPhase]

  // Section 0: input-waiting gate
  IF input.workflowState.IsWaitingForInput AND NOT isSubstantialInput(trimmed) THEN
    RETURN false  // handled by section 0, not section 4
  END IF

  // Section 1: skip words
  IF containsAny(trimmed, skipWords) THEN
    RETURN false  // handled by section 1
  END IF

  // Section 2: confirm words with output
  IF phase.NeedsConfirm AND containsAny(trimmed, confirmWords) AND hasOutput THEN
    RETURN false  // handled by section 2 (advancePhase)
  END IF

  // Section 3: PendingConfirm (NeedsConfirm + hasOutput, any text)
  IF phase.NeedsConfirm AND hasOutput THEN
    RETURN false  // handled by section 3 (PendingConfirm)
  END IF

  // Section 4: default branch — THIS IS WHERE THE BUG IS
  RETURN true
END FUNCTION
```

### Examples

- User has a coding workflow at `requirements` phase with no output. User sends "check server 2's status" → falls through to section 4 → `RunAgentLoop=true, DefaultInput=false` → `workflowAgentLoopMarker` set → agent loop runs with `doc_only` filtering → SSH tool stripped → LLM cannot execute the request
- User has a PPT workflow at `content_outline` phase with no output. User sends "查看驱网服务器资源状态" → same path → SSH tool stripped → LLM spins trying to generate a "content outline" document
- User has a coding workflow at `requirements` phase with no output. User sends "开工" (legitimate workflow trigger) → falls through to section 4 → same bug path, but in this case the workflow agent loop is actually desired. However, with the fix (`DefaultInput=true`), the message falls through to the normal agent loop which still has the `PhasePrompt` in conversation history and can generate the document
- User sends "确认" during a phase with output → handled by section 2/3, NOT section 4 → no bug

## Expected Behavior

### Preservation Requirements

**Unchanged Behaviors:**
- Confirm words (e.g., "确认", "OK") during a `NeedsConfirm` phase with existing output must continue to advance the phase via `advancePhase` (section 2)
- Skip words during a skippable phase must continue to skip the phase (section 1)
- `handlePendingConfirm` LLM classification (confirm/modify/other) must continue to work for `NeedsConfirm` phases with output (section 3)
- Input-waiting workflows must continue to gate on `isSubstantialInput` (section 0)
- Attachment bypass (short text + image) must continue to skip workflow interception entirely (pre-HandleInput check)
- `applyWorkflowToolFilter` with `SkipNeedsConfirmGate=true` must continue to skip `doc_only` filtering

**Scope:**
The fix only changes section 4 of `HandleInput` — the default branch that is reached when no other section matches. All other sections (0, 1, 2, 3) and all downstream consumers of `WorkflowResponse` fields other than `DefaultInput` are completely unaffected.

## Hypothesized Root Cause

Based on the bug description and code analysis, the root cause is clear and singular:

1. **Missing `DefaultInput=true` in section 4**: `HandleInput` section 4 constructs a `WorkflowResponse` with `RunAgentLoop=true`, `PhasePrompt`, and `ToolFilter`, but does not set `DefaultInput=true`. Since Go zero-initializes booleans to `false`, `DefaultInput` defaults to `false`. The `handleActiveWorkflow` function checks `if !resp.DefaultInput` (line 86 of `im_message_handler_workflow.go`) and only sets `workflowAgentLoopMarker` when `DefaultInput` is false. The infrastructure for the fix already exists — only the signal is missing.

2. **No intent classification for no-output phases**: The `PendingConfirm` path (section 3) has an LLM intent classifier that can detect "other" messages, but it only activates when `phase.NeedsConfirm=true AND hasOutput=true`. When either condition is false, there is no mechanism to distinguish workflow-related messages from unrelated ones. Setting `DefaultInput=true` sidesteps this by letting ALL section 4 messages fall through to the normal agent loop, which has the full tool list and can handle both workflow and non-workflow requests.

## Correctness Properties

Property 1: Bug Condition - Default Branch Sets DefaultInput

_For any_ input that reaches `HandleInput` section 4 (default branch) — meaning the workflow is active, the input does not match confirm/skip words, and the phase is not in `NeedsConfirm+hasOutput` state — the returned `WorkflowResponse` SHALL have `DefaultInput=true`, preventing `handleActiveWorkflow` from setting `workflowAgentLoopMarker`.

**Validates: Requirements 2.1, 2.2, 2.3**

Property 2: Preservation - Non-Default Branches Unchanged

_For any_ input that is handled by sections 0, 1, 2, or 3 of `HandleInput` (input-waiting, skip words, confirm words with output, PendingConfirm), the behavior SHALL be identical to the original function — `advancePhase`, skip rejection, reminder text, or `PendingConfirm=true` respectively — preserving all existing workflow state transitions.

**Validates: Requirements 3.1, 3.2, 3.3, 3.5, 3.6, 3.7, 3.8**

## Fix Implementation

### Changes Required

Assuming our root cause analysis is correct:

**File**: `corelib/workflow/engine.go`

**Function**: `HandleInput` — section 4 (default branch, approximately line 234-242)

**Specific Changes**:
1. **Add `DefaultInput: true`** to the `WorkflowResponse` struct literal returned in section 4:
   ```go
   return &WorkflowResponse{
       Text:         "",
       PhasePrompt:  phasePrompt,
       ToolFilter:   phase.ToolPolicy,
       RunAgentLoop: true,
       DefaultInput: true,  // ← ADD THIS LINE
   }, nil
   ```

That's it. One line. The downstream logic in `handleActiveWorkflow` already has the correct guard:
```go
if !resp.DefaultInput {
    // ... set workflowAgentLoopMarker
}
```

When `DefaultInput=true`, this guard prevents the marker from being set, and `handleActiveWorkflow` returns `nil`, causing the message to fall through to the normal agent loop with the full tool list.

**No changes needed in**:
- `gui/im_message_handler_workflow.go` — the `if !resp.DefaultInput` check already works correctly
- `gui/im_message_handler.go` — `applyWorkflowToolFilter` is already gated by `workflowAgentLoopMarker` and `SkipNeedsConfirmGate`
- Any other file

## Testing Strategy

### Validation Approach

The testing strategy follows a two-phase approach: first, surface counterexamples that demonstrate the bug on unfixed code, then verify the fix works correctly and preserves existing behavior.

### Exploratory Bug Condition Checking

**Goal**: Surface counterexamples that demonstrate the bug BEFORE implementing the fix. Confirm that `HandleInput` section 4 returns `DefaultInput=false` for all inputs that reach it.

**Test Plan**: Call `HandleInput` with various inputs during a no-output phase and assert that `DefaultInput` is `false` (demonstrating the bug). Run these tests on the UNFIXED code to observe failures.

**Test Cases**:
1. **Unrelated SSH request**: Active workflow at `requirements` phase with no output, user sends "check server status" → `DefaultInput` should be `false` on unfixed code (will fail after fix)
2. **Legitimate workflow trigger**: Active workflow at `requirements` phase with no output, user sends "开工" → `DefaultInput` should be `false` on unfixed code (will fail after fix)
3. **Random text**: Active workflow at `content_outline` phase with no output, user sends "hello world" → `DefaultInput` should be `false` on unfixed code (will fail after fix)

**Expected Counterexamples**:
- All inputs reaching section 4 return `DefaultInput=false`, causing `workflowAgentLoopMarker` to be set unconditionally
- The marker causes `doc_only` tool filtering, stripping SSH and other conditional tools

### Fix Checking

**Goal**: Verify that for all inputs where the bug condition holds, the fixed function produces the expected behavior.

**Pseudocode:**
```
FOR ALL input WHERE isBugCondition(input) DO
  result := HandleInput_fixed(input.userID, input.text)
  ASSERT result.DefaultInput == true
  ASSERT result.RunAgentLoop == true
  ASSERT result.PhasePrompt != ""
END FOR
```

### Preservation Checking

**Goal**: Verify that for all inputs where the bug condition does NOT hold, the fixed function produces the same result as the original function.

**Pseudocode:**
```
FOR ALL input WHERE NOT isBugCondition(input) DO
  ASSERT HandleInput_original(input) == HandleInput_fixed(input)
END FOR
```

**Testing Approach**: Property-based testing is recommended for preservation checking because:
- It generates many test cases automatically across the input domain (various confirm words, skip words, phase configurations)
- It catches edge cases that manual unit tests might miss (e.g., confirm words in non-NeedsConfirm phases)
- It provides strong guarantees that behavior is unchanged for all non-buggy inputs

**Test Plan**: Observe behavior on UNFIXED code first for confirm/skip/PendingConfirm paths, then write property-based tests capturing that behavior.

**Test Cases**:
1. **Confirm word preservation**: Generate random confirm words during `NeedsConfirm+hasOutput` phases → verify `advancePhase` is called (same as original)
2. **Skip word preservation**: Generate random skip words during skippable phases → verify phase is skipped (same as original)
3. **PendingConfirm preservation**: Generate random text during `NeedsConfirm+hasOutput` phases → verify `PendingConfirm=true` is returned (same as original)
4. **Input-waiting preservation**: Generate random short text during input-waiting phases → verify reminder is returned (same as original)

### Unit Tests

- Test `HandleInput` section 4 returns `DefaultInput=true` for various inputs during no-output phases
- Test `HandleInput` section 4 returns `DefaultInput=true` for various workflow types (coding, PPT, etc.)
- Test `handleActiveWorkflow` does NOT set `workflowAgentLoopMarker` when `DefaultInput=true`
- Test confirm/skip/PendingConfirm paths are unaffected by the change

### Property-Based Tests

- Generate random workflow states (various types, phases, output states) and random user inputs → verify section 4 always returns `DefaultInput=true`
- Generate random inputs for non-section-4 paths → verify behavior is identical to original
- Generate random combinations of `NeedsConfirm`, `hasOutput`, confirm words → verify correct branch selection

### Integration Tests

- End-to-end test: active workflow + unrelated SSH request → message reaches normal agent loop with full tool list
- End-to-end test: active workflow + legitimate "开工" → message reaches normal agent loop (still works, just without workflow-specific prompt injection)
- End-to-end test: active workflow + confirm word during NeedsConfirm phase → phase advances correctly
