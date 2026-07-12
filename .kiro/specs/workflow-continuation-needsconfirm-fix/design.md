# Workflow Continuation NeedsConfirm Fix — Bugfix Design

## Overview

When a user provides supplementary information during a NeedsConfirm phase that already has output (e.g., "需要音效，需要偷东西的中间目标" during requirements confirmation), the workflow stalls. The `HandleInput` method correctly returns `PendingConfirm=true`, delegating to `handlePendingConfirm` for LLM-based intent classification. However, the lightweight LLM classify may return "other" for supplementary info that doesn't look like an explicit modification request (e.g., additive requirements vs. "把技术栈改成 React"). When classified as "other", the message falls through to the normal agent loop **without** `workflowAgentLoopMarker` set, breaking phase prompt injection, `SavePhaseOutput` capture, and doc preview updates. Additionally, the `GateIntentClassifier` classifies supplementary info as `continuation` (conf≈0.90), setting `gateConfig.active=false`, which disables the steering-based NeedsConfirm fallback path — both engine and steering NeedsConfirm paths are broken simultaneously.

The fix has two parts:
1. **LLM classify prompt enhancement**: Update the `handlePendingConfirm` system prompt to explicitly recognize supplementary/additive information as "modify" intent, not "other"
2. **Fallback safety net**: When `handlePendingConfirm` returns nil (LLM said "other") but the workflow engine's `PendingConfirm` was true, ensure the agent loop still runs with workflow markers set — treating unclassified responses during NeedsConfirm as modification requests rather than passthrough

## Glossary

- **Bug_Condition (C)**: User sends supplementary info during a NeedsConfirm phase with existing output, and the LLM classify in `handlePendingConfirm` returns "other" instead of "modify", causing the message to fall through without workflow markers
- **Property (P)**: Supplementary info during NeedsConfirm phases is treated as a modification request — `workflowAgentLoopMarker` is set, phase prompt is injected, `SavePhaseOutput` captures the updated document
- **Preservation**: Confirm words still advance phases, skip words still skip, explicit modify indicators still trigger modify, unrelated queries (weather, etc.) during active workflows still pass through as normal agent loop messages
- **`handlePendingConfirm`**: The function in `gui/im_message_handler_workflow.go` that makes a lightweight LLM call to classify user intent as confirm/modify/other during NeedsConfirm phases
- **`PendingConfirm`**: Field in `WorkflowResponse` set to true when the engine detects a NeedsConfirm phase with existing output, delegating intent classification to the caller
- **`workflowAgentLoopMarker`**: A `sync.Map` flag in `IMMessageHandler` that, when set, causes the agent loop to inject phase prompts, call `SavePhaseOutput`, and update doc preview
- **`GateIntentClassifier`**: The semantic classifier that categorizes user messages into intents (new_project, bug_fix, continuation, etc.) for the Coding Tool Gate
- **`gateConfig.active`**: Boolean in `codingToolGateConfig` that controls whether the Coding Tool Gate intercepts coding tools; set to false for `continuation` intent
- **`needsConfirmFromSteering`**: Boolean computed in the agent loop from `gateConfig.active && iteration > 0`, used as a fallback NeedsConfirm gate when no WorkflowEngine workflow is active

## Bug Details

### Bug Condition

The bug manifests when a user provides supplementary information during a NeedsConfirm phase that already has output. The `HandleInput` method returns `PendingConfirm=true`, and `handlePendingConfirm` makes a lightweight LLM call to classify the intent. The LLM classify prompt defines three categories: "confirm", "modify", and "other". Supplementary/additive information (e.g., "需要音效，需要偷东西的中间目标") doesn't match the LLM's mental model of "modify" (which implies changing existing content), so it gets classified as "other". The "other" path returns nil, falling through to the normal agent loop without setting `workflowAgentLoopMarker`.

Simultaneously, the `GateIntentClassifier` classifies the supplementary info as `continuation` (conf≈0.90), which maps to `gateConfig.active=false` in `mapGateIntentToConfig`. This disables `needsConfirmFromSteering` in the agent loop's NeedsConfirm gate. With both the engine path (missing marker due to "other" classification) and the steering path (inactive gate due to `continuation` intent) broken, the agent loop runs as a normal non-workflow loop.

**Formal Specification:**
```
FUNCTION isBugCondition(input)
  INPUT: input of type UserMessage
  OUTPUT: boolean

  RETURN hasActiveWorkflow(input.userID)
     AND currentPhase(input.userID).NeedsConfirm = true
     AND hasPhaseOutput(input.userID, currentPhase(input.userID).ID)
     AND NOT matchesConfirmWords(input.text)
     AND NOT matchesSkipWords(input.text)
     AND handlePendingConfirmClassify(input.text) = "other"
END FUNCTION
```

### Examples

- **Additive requirements**: User says "需要音效，需要偷东西的中间目标" during requirements confirmation → LLM classify returns "other" (additive info, not explicit change request) → falls through without markers → **stall** (expected: treated as modify, updated doc generated and saved)
- **Feature additions**: User says "还需要一个排行榜功能" during requirements confirmation → LLM classify returns "other" → **stall** (expected: treated as modify)
- **Constraint additions**: User says "用 C++ 和 cmake" during requirements confirmation → LLM classify returns "other" → **stall** (expected: treated as modify)
- **Explicit modification**: User says "把技术栈改成 React" → LLM classify returns "modify" → correctly handled - **Confirmation**: User says "确认" / "OK" → `HandleInput` matches `confirmWords` → `advancePhase` → correctly handled - **Unrelated query**: User says "查询天气" → LLM classify returns "other" → falls through to normal agent loop → correctly handled (but this is the same path as the bug — the fix must distinguish supplementary info from truly unrelated queries)
- **LLM classify failure**: LLM call times out → fallback to "confirm" → `advanceAndRespond` with stale v1 output → **supplementary info lost** (expected: treated as modify or at minimum not advance with stale output)

## Expected Behavior

### Preservation Requirements

**Unchanged Behaviors:**
- Messages matching `confirmWords` ("确认", "OK", "没问题", etc.) during NeedsConfirm phases with output must continue to advance via `advancePhase` — these are intercepted by `HandleInput` before reaching `PendingConfirm`
- Messages matching `skipWords` during CanSkip phases must continue to skip via `advancePhase`
- Messages matching `modifyIndicators` during any phase must continue to trigger the modify path (also intercepted before `PendingConfirm` in the current code, but the LLM classify "modify" path must remain functional)
- Unrelated queries during active workflows (e.g., "查询天气") that the LLM correctly classifies as "other" must continue to fall through to the normal agent loop
- Initial execution requests (e.g., "开工") for phases with no output must continue to run the agent loop with `workflowAgentLoopMarker` set
- `GateIntentClassifier` classification of `new_project` must continue to activate the Coding Tool Gate
- `GateIntentClassifier` classification of `bug_fix` must continue to bypass the three-phase flow
- NeedsConfirm gate in the no-tool branch for engine-based workflows must continue to emit doc preview updates
- The fix must be phase-agnostic — identical behavior across requirements, design, task breakdown, and all other NeedsConfirm phases

**Scope:**
All inputs that do NOT involve the `handlePendingConfirm` "other" classification path should be completely unaffected by this fix. This includes:
- Messages intercepted by `HandleInput` before `PendingConfirm` (confirmWords, skipWords)
- Messages during phases without output (first execution path)
- Messages during NeedsConfirm=false phases (implementation, etc.)
- Messages when no active workflow exists

## Hypothesized Root Cause

Based on the bug description and code analysis, the most likely issues are:

1. **LLM classify prompt too narrow on "modify"**: The `handlePendingConfirm` system prompt defines "modify" as "user wants to **change or update** the document". Supplementary/additive information ("需要音效") doesn't feel like "changing" existing content — it's adding new requirements. The LLM interprets this as "other" because the user isn't explicitly requesting a change to what's already written. The prompt needs to explicitly include additive/supplementary information as a "modify" category.

2. **"other" path returns nil without safety net**: When the LLM returns "other", `handlePendingConfirm` returns nil, which causes `handleActiveWorkflow` to also return nil. The caller then proceeds to the normal agent loop. But `handleActiveWorkflow` only sets `workflowAgentLoopMarker` when `!resp.DefaultInput` — and since `PendingConfirm` responses don't reach that code path (they're handled by `handlePendingConfirm` which returns early), the marker is never set for the "other" case.

3. **GateIntentClassifier disables steering fallback**: The `GateIntentClassifier` classifies supplementary info as `continuation`, which maps to `gateConfig.active=false`. This disables `needsConfirmFromSteering` in the agent loop. Even if the agent loop ran with workflow awareness, the NeedsConfirm gate wouldn't fire because the steering path is inactive. This is a secondary failure — the primary fix should ensure the engine path works correctly, but the steering path's inability to serve as a fallback makes the bug more severe.

4. **LLM classify failure fallback is destructive**: When the LLM call fails (timeout, network error), `handlePendingConfirm` falls back to treating the message as "confirm" and calls `advanceAndRespond`. This advances the phase with the stale v1 output, effectively losing the user's supplementary information. A safer fallback would be to treat failures as "modify" (run the agent loop with the user's text as modification context).

## Correctness Properties

Property 1: Bug Condition - Supplementary Info Treated as Modify

_For any_ user message during a NeedsConfirm phase with existing output, where the message does not match confirmWords or skipWords, and the message contains supplementary/additive information relevant to the current phase document, the fixed `handlePendingConfirm` SHALL classify the intent as "modify" and set `workflowAgentLoopMarker`, inject the phase prompt with the user's text as modification context, and run the agent loop on behalf of the workflow.

**Validates: Requirements 2.1, 2.2, 2.3, 2.4**

Property 2: Preservation - Unrelated Queries Still Pass Through

_For any_ user message during a NeedsConfirm phase with existing output, where the message is genuinely unrelated to the workflow (e.g., "查询天气", "今天几号"), the fixed `handlePendingConfirm` SHALL classify the intent as "other" and let the message fall through to the normal agent loop without workflow markers, preserving the ability to handle non-workflow queries during active workflows.

**Validates: Requirements 3.4**

Property 3: Preservation - Confirm and Skip Words Unchanged

_For any_ user message that matches `confirmWords` or `skipWords`, the `HandleInput` method SHALL continue to intercept the message before reaching `handlePendingConfirm`, preserving the existing confirm/skip behavior without any change.

**Validates: Requirements 3.1, 3.2, 3.3**

Property 4: Preservation - Phase-Agnostic Behavior

_For any_ NeedsConfirm phase (requirements, design, task_breakdown, audience_goal, etc.) across all 19 workflow templates, the fix SHALL produce identical behavior — supplementary info is treated as modify, unrelated queries pass through, confirmations advance.

**Validates: Requirements 3.9**

## Fix Implementation

### Changes Required

Assuming our root cause analysis is correct:

**File**: `gui/im_message_handler_workflow.go`

**Function**: `handlePendingConfirm`

**Specific Changes**:

1. **Enhance LLM classify system prompt** to explicitly recognize supplementary/additive information as "modify":
   - Current prompt defines "modify" as "user wants to change or update the document"
   - Updated prompt should define "modify" as "user wants to change, update, add to, or supplement the document with new information or requirements"
   - Add explicit examples: "Adding new requirements, specifying constraints, providing additional details = modify"
   - Add explicit counter-examples: "Asking about weather, asking unrelated questions = other"

2. **Change "other" fallback behavior** when `PendingConfirm` was true:
   - Current: returns nil → falls through to normal agent loop without markers
   - Fixed: treat "other" during `PendingConfirm` as "modify" (conservative approach — better to re-generate with supplementary info than to lose it)
   - Rationale: The `HandleInput` method already filters out confirmWords and skipWords before reaching `PendingConfirm`. If the message reached `handlePendingConfirm`, it's neither a confirmation nor a skip. The only remaining possibilities are: (a) modification/supplementary info, or (b) truly unrelated query. Since the engine already determined this is a `PendingConfirm` scenario (NeedsConfirm phase with output), treating ambiguous messages as modifications is safer than losing them.
   - **Exception**: If the LLM explicitly returns "other" with high confidence AND the message is very short (< 10 chars) AND doesn't contain any domain-relevant keywords, still pass through. This preserves the ability to handle truly unrelated short queries like "几点了".

3. **Change LLM classify failure fallback** from "confirm" to "modify":
   - Current: LLM failure → fallback to confirm → `advanceAndRespond` with stale output
   - Fixed: LLM failure → fallback to modify → run agent loop with user's text as modification context
   - Rationale: Losing user input (advancing with stale output) is worse than re-generating the document with the user's text. If the user actually wanted to confirm, they can confirm again after seeing the (potentially unchanged) document.

4. **Add logging** for the "other → modify" fallback to aid debugging:
   - Log at info level: `"[workflow-confirm] 'other' intent during PendingConfirm, treating as modify: user=%s text=%q"`

**File**: `gui/im_message_handler_workflow.go`

**Function**: `handleActiveWorkflow`

**Specific Changes**:

5. **No changes needed** to `handleActiveWorkflow` itself — the fix is entirely within `handlePendingConfirm`. When `handlePendingConfirm` correctly returns nil after setting `workflowAgentLoopMarker` and `stashedPhasePrompt` (the "modify" path), `handleActiveWorkflow` returns nil, and the agent loop picks up the stashed prompt and marker.

**File**: `gui/coding_tool_gate.go`

**No changes needed** — the `GateIntentClassifier`'s `continuation` classification and `gateConfig.active=false` behavior is correct for its purpose (the Coding Tool Gate should not activate for continuation messages). The fix addresses the bug at the `handlePendingConfirm` level, before the agent loop's NeedsConfirm gate is evaluated.

## Testing Strategy

### Validation Approach

The testing strategy follows a two-phase approach: first, surface counterexamples that demonstrate the bug on unfixed code, then verify the fix works correctly and preserves existing behavior.

### Exploratory Bug Condition Checking

**Goal**: Surface counterexamples that demonstrate the bug BEFORE implementing the fix. Confirm or refute the root cause analysis. If we refute, we will need to re-hypothesize.

**Test Plan**: Write tests that simulate `handlePendingConfirm` receiving supplementary/additive user messages and assert that the LLM classify returns "modify". Run these tests on the UNFIXED code to observe that the LLM returns "other" instead.

**Test Cases**:
1. **Additive Requirements Test**: Simulate "需要音效，需要偷东西的中间目标" during requirements confirmation → LLM classify returns "other" (will fail on unfixed code — should return "modify")
2. **Feature Addition Test**: Simulate "还需要一个排行榜功能" during requirements confirmation → LLM classify returns "other" (will fail on unfixed code)
3. **Constraint Addition Test**: Simulate "用 C++ 和 cmake" during requirements confirmation → LLM classify returns "other" (will fail on unfixed code)
4. **Design Phase Supplement Test**: Simulate "数据库用 PostgreSQL" during design confirmation → LLM classify returns "other" (will fail on unfixed code)

**Expected Counterexamples**:
- LLM classify returns "other" for all supplementary/additive messages
- Root cause confirmed: LLM prompt's "modify" definition is too narrow, excluding additive information

### Fix Checking

**Goal**: Verify that for all inputs where the bug condition holds, the fixed function produces the expected behavior.

**Pseudocode:**
```
FOR ALL input WHERE isBugCondition(input) DO
  result := handlePendingConfirm_fixed(engine, input.userID, input.text)
  // result is nil (falls through to agent loop) with markers set
  ASSERT result = nil
  ASSERT workflowAgentLoopMarker.Load(input.userID) = true
  ASSERT stashedPhasePrompt.Load(input.userID) contains input.text
END FOR
```

### Preservation Checking

**Goal**: Verify that for all inputs where the bug condition does NOT hold, the fixed function produces the same result as the original function.

**Pseudocode:**
```
FOR ALL input WHERE NOT isBugCondition(input) DO
  ASSERT handlePendingConfirm_original(input) = handlePendingConfirm_fixed(input)
END FOR
```

**Testing Approach**: Property-based testing is recommended for preservation checking because:
- It generates many test cases automatically across the input domain (varying message types, phase types, output states)
- It catches edge cases that manual unit tests might miss (e.g., messages that are borderline between supplementary info and unrelated queries)
- It provides strong guarantees that behavior is unchanged for all non-buggy inputs

**Test Plan**: Observe behavior on UNFIXED code first for confirmation messages, skip messages, and truly unrelated queries, then write property-based tests capturing that behavior.

**Test Cases**:
1. **Confirm Words Preservation**: Verify that "确认", "OK", "没问题" etc. are intercepted by `HandleInput` before reaching `handlePendingConfirm` — behavior unchanged
2. **Skip Words Preservation**: Verify that "跳过", "skip" etc. are intercepted by `HandleInput` — behavior unchanged
3. **Explicit Modify Preservation**: Verify that "把技术栈改成 React" is classified as "modify" by the LLM — behavior unchanged (already works)
4. **Unrelated Query Preservation**: Verify that "查询天气", "今天几号" are classified as "other" and pass through — the "other → modify" fallback should NOT trigger for genuinely unrelated short queries

### Unit Tests

- Test `handlePendingConfirm` with additive requirements text → returns nil with markers set
- Test `handlePendingConfirm` with explicit modify text → returns nil with markers set (existing behavior preserved)
- Test `handlePendingConfirm` with confirm text → returns advance response (but note: confirmWords are intercepted earlier by `HandleInput`)
- Test `handlePendingConfirm` with LLM classify failure → falls back to modify (not confirm)
- Test `handlePendingConfirm` with truly unrelated short query → passes through without markers
- Test that `workflowAgentLoopMarker` is set after supplementary info classification
- Test that `stashedPhasePrompt` contains the user's supplementary text
- Test phase-agnostic behavior: same result for requirements, design, task_breakdown phases

### Property-Based Tests

- Generate random supplementary info messages (additive requirements, constraint specifications, feature additions) → verify `handlePendingConfirm` sets workflow markers
- Generate random unrelated queries (weather, time, greetings) → verify `handlePendingConfirm` does NOT set workflow markers for genuinely unrelated short messages
- Generate random confirm/skip messages → verify `HandleInput` intercepts before `handlePendingConfirm` is reached
- Test across all 19 workflow templates with NeedsConfirm=true phases → verify identical behavior

### Integration Tests

- End-to-end: simulate workflow start → LLM generates requirements doc → user sends "需要音效" → verify doc is updated (v2) and saved → user sends "确认" → verify phase advances with v2 output
- End-to-end: simulate workflow start → LLM generates design doc → user sends "数据库用 PostgreSQL" → verify design doc is updated and saved
- Verify doc preview panel updates after supplementary info processing on desktop platform
- Verify NeedsConfirm gate fires after updated document generation, returning response for user confirmation
