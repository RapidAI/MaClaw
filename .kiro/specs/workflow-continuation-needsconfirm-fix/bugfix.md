# Bugfix Requirements Document

## Introduction

When MaClaw is in the coding workflow's requirements confirmation phase and the user provides supplementary information (e.g., "需要音效，需要偷东西的中间目标"), the LLM correctly generates an updated requirements document (v2), but the workflow stalls. The updated document is returned to the user, but the system does not properly wait for user confirmation and does not proceed to the next phase. The root cause is a gap in how `HandleInput` classifies supplementary user input during a `NeedsConfirm` phase: the input doesn't match `confirmWords`, `skipWords`, or `modifyIndicators`, so it falls to the default case with `DefaultInput=true`. This causes `handleActiveWorkflow` to skip setting the `workflowAgentLoopMarker`, which in turn breaks phase prompt injection, `SavePhaseOutput` capture, and doc preview updates — effectively stalling the workflow.

## Bug Analysis

### Current Behavior (Defect)

1.1 WHEN the user provides supplementary information during a NeedsConfirm phase (text that doesn't match confirmWords, skipWords, or modifyIndicators) AND the phase already has output, THEN `HandleInput` returns `DefaultInput=true` and `handleActiveWorkflow` does NOT set `workflowAgentLoopMarker`, causing the agent loop to run as a normal (non-workflow) loop without phase prompt injection

1.2 WHEN `workflowAgentLoopMarker` is not set AND the agent loop completes with LLM-generated text output, THEN `SavePhaseOutput` is NOT called (guarded by `workflowAgentLoop` flag), so the workflow engine does not capture the updated document (v2) as the current phase's output

1.3 WHEN `workflowAgentLoopMarker` is not set AND the agent loop completes on a desktop platform, THEN the doc preview panel is NOT updated with the new document content (the post-loop doc capture block at line ~3307 is skipped)

1.4 WHEN the GateIntentClassifier classifies the supplementary info as `continuation` (conf=0.90), THEN `gateConfig.active` is set to `false`, causing `needsConfirmFromSteering` to be `false` in the NeedsConfirm gate — the steering-based fallback path also cannot fire, so BOTH NeedsConfirm paths (engine via missing marker, steering via inactive gate) are broken simultaneously

1.5 WHEN the phase prompt is not injected into the system prompt (because `workflowAgentLoop=false`), THEN the LLM generates the updated document without proper phase guidance, potentially producing lower-quality or incorrectly structured output

1.6 WHEN the user sends another message after the stall (e.g., "确认"), THEN `HandleInput` sees the phase still has output (v1, since v2 was never saved) and processes the confirmation against the stale v1 output, or the supplementary info is effectively lost

1.7 WHEN the same bug condition occurs during any NeedsConfirm phase (not just requirements — also design confirmation, task breakdown confirmation, etc.), THEN the same stall behavior occurs because the `DefaultInput` logic in `engine.go` is phase-agnostic: `isDefault = hasOutput` applies to all phases equally

### Expected Behavior (Correct)

2.1 WHEN the user provides supplementary information during a NeedsConfirm phase that already has output, THEN the system SHALL treat it as a modification request — injecting the phase prompt with the user's supplementary text as modification context, setting `workflowAgentLoopMarker`, and running the agent loop on behalf of the workflow

2.2 WHEN the agent loop completes after processing supplementary info during a NeedsConfirm phase, THEN the system SHALL call `SavePhaseOutput` to capture the updated document (v2) as the current phase's output in the workflow engine

2.3 WHEN the agent loop completes after processing supplementary info on a desktop platform, THEN the system SHALL update the doc preview panel with the new document content via `EmitDocUpdate`

2.4 WHEN the NeedsConfirm gate fires after the LLM generates the updated document, THEN the system SHALL return the response to the user and wait for explicit confirmation before advancing to the next phase

2.5 WHEN the user subsequently confirms (e.g., "确认", "OK") after seeing the updated document, THEN the system SHALL advance to the next workflow phase using the captured v2 output

### Unchanged Behavior (Regression Prevention)

3.1 WHEN the user sends a message that matches `confirmWords` during a NeedsConfirm phase with existing output, THEN the system SHALL CONTINUE TO advance to the next phase via `advancePhase`

3.2 WHEN the user sends a message that matches `skipWords` during a CanSkip phase, THEN the system SHALL CONTINUE TO skip the phase via `advancePhase`

3.3 WHEN the user sends a message that matches `modifyIndicators` during any phase, THEN the system SHALL CONTINUE TO inject the modify prompt and run the agent loop with `workflowAgentLoopMarker` set

3.4 WHEN the user sends an unrelated message during an active workflow (e.g., "查询天气") that doesn't match any workflow keywords AND the phase already has output AND the GateIntentClassifier does NOT classify it as `continuation`, THEN the system SHALL CONTINUE TO treat it as `DefaultInput=true` and let the normal agent loop handle it without workflow markers — preserving the ability to handle non-workflow queries during an active workflow

3.5 WHEN the user sends the initial execution request (e.g., "开工") for a phase that has NO output yet, THEN the system SHALL CONTINUE TO run the agent loop with `workflowAgentLoopMarker` set and phase prompt injected (this is the existing non-DefaultInput path, since `isDefault = hasOutput` and `hasOutput=false`)

3.6 WHEN the GateIntentClassifier classifies a message as `new_project` with high confidence, THEN the Coding Tool Gate SHALL CONTINUE TO activate and enforce the three-phase flow

3.7 WHEN the GateIntentClassifier classifies a message as `bug_fix`, THEN the system SHALL CONTINUE TO bypass the three-phase flow and execute directly

3.8 WHEN the NeedsConfirm gate fires in the no-tool branch for engine-based workflows, THEN the system SHALL CONTINUE TO emit doc preview updates via both Path 1 (steering detector) and Path 2 (engine current phase)

3.9 WHEN the same supplementary-info-as-modification behavior fires during any NeedsConfirm phase (design, task breakdown, etc.), THEN the system SHALL behave identically to the requirements phase — the fix must be phase-agnostic

---

## Bug Condition (Formal)

```pascal
FUNCTION isBugCondition(X)
  INPUT: X of type UserMessage
  OUTPUT: boolean
  
  // Returns true when:
  // 1. There is an active workflow for the user
  // 2. The current phase has NeedsConfirm=true
  // 3. The current phase already has output (previous document generated)
  // 4. The user's text does NOT match confirmWords, skipWords, or modifyIndicators
  // 5. The GateIntentClassifier classifies the message as `continuation`
  //    (i.e., the user is continuing/supplementing the current task,
  //     not asking something unrelated like "查询天气")
  //
  // Note: Condition 5 is the key discriminator between supplementary info
  // (should trigger modification) and unrelated queries (should pass through
  // as DefaultInput=true). The existing LLM-based classifier already provides
  // this signal — the bug is that it's not being used at the HandleInput level.
  
  RETURN hasActiveWorkflow(X.userID)
     AND currentPhase(X.userID).NeedsConfirm = true
     AND hasPhaseOutput(X.userID, currentPhase(X.userID).ID)
     AND NOT matchesConfirmWords(X.text)
     AND NOT matchesSkipWords(X.text)
     AND NOT matchesModifyIndicators(X.text)
     AND intentClassification(X.text) IN {continuation, new_project}
END FUNCTION
```

```pascal
// Property: Fix Checking — Supplementary info triggers workflow-aware agent loop
FOR ALL X WHERE isBugCondition(X) DO
  response ← HandleInput'(X)
  ASSERT response.RunAgentLoop = true
     AND response.DefaultInput = false  // treated as modify, not default
     AND response.PhasePrompt != ""     // phase prompt with user's supplementary text
  
  // After agent loop completes:
  ASSERT workflowAgentLoopMarker is set
     AND SavePhaseOutput is called with updated content
     AND NeedsConfirm gate fires (response returned for user confirmation)
END FOR
```

```pascal
// Property: Preservation Checking — Non-buggy inputs behave identically
FOR ALL X WHERE NOT isBugCondition(X) DO
  ASSERT HandleInput(X) = HandleInput'(X)
END FOR
```

```pascal
// Property: Unrelated Query Passthrough — Weather queries etc. still work
FOR ALL X WHERE hasActiveWorkflow(X.userID)
              AND hasPhaseOutput(X.userID, currentPhase(X.userID).ID)
              AND intentClassification(X.text) NOT IN {continuation, new_project}
              AND NOT matchesConfirmWords(X.text)
              AND NOT matchesSkipWords(X.text)
              AND NOT matchesModifyIndicators(X.text) DO
  response ← HandleInput'(X)
  ASSERT response.DefaultInput = true  // still treated as unrelated
END FOR
```
