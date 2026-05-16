# Bugfix Requirements Document

## Introduction

When a user starts a workflow (e.g., presentation_design) by saying "开工", the system enters the first NeedsConfirm phase (e.g., `audience_goal`) and the LLM outputs a substantive plan overview/preamble (e.g., "收到，马上为您启动PPT制作工作流！我们将按以下步骤进行：1. 受众目标定义...让我们开始吧！"). The NeedsConfirm gate in the agent loop treats this preamble as the actual phase deliverable and force-returns it, terminating the agent loop prematurely. The user must then send a second message (e.g., "开始") to actually get the phase document generated. This is a double-confirmation bug that affects all 19 workflow templates with NeedsConfirm=true phases.

The root cause is that the NeedsConfirm gate (`needsConfirmFromEngine`) does not distinguish between **first execution** (no phase output exists yet — the LLM is still generating the document) and **post-output confirmation** (a document already exists — the gate should stop the loop so the user can confirm). The gate fires on `IsPhaseNeedsConfirm(userID)` which returns `true` regardless of whether the phase already has output. When the LLM's first-iteration preamble is substantive (≥200 runes or contains numbered lists/headings), `isSubstantivePhaseDocument()` returns true, and the gate force-returns it as if it were the completed deliverable.

The user explicitly requires a **generic/structural fix** (not keyword-based): "需要用通用的办法，不要用关键字触发".

## Bug Analysis

### Current Behavior (Defect)

1.1 WHEN the engine returns `RunAgentLoop=true` for a NeedsConfirm phase that has NO prior output (`hasOutput=false`, first execution) AND the LLM's first iteration outputs a substantive preamble/plan overview (≥200 runes or containing numbered lists/headings), THEN the NeedsConfirm gate (`needsConfirmFromEngine=true`) force-returns this preamble as the phase deliverable, terminating the agent loop before the actual phase document is generated

1.2 WHEN the NeedsConfirm gate force-returns the preamble AND `SavePhaseOutput` captures this preamble as the phase output, THEN the workflow engine records the preamble (not the actual deliverable) as the phase's output, and the user sees a plan overview instead of the expected phase document (e.g., 受众与目标文档)

1.3 WHEN the user sends a second message (e.g., "开始") after seeing the preamble, THEN the engine sees `hasOutput=true` (the preamble) and returns `PendingConfirm=true`, triggering LLM intent classification which classifies "开始" as "confirm" and advances to the next phase — OR the engine falls through to the default case and runs the agent loop again, finally generating the actual document — resulting in inconsistent behavior depending on the exact message

1.4 WHEN the same first-execution scenario occurs on ANY of the 19 workflow templates with NeedsConfirm=true phases (not just presentation_design), THEN the same double-confirmation bug occurs because `IsPhaseNeedsConfirm()` is template-agnostic and does not check `hasOutput` state

1.5 WHEN `needsConfirmFromEngine` is computed in the agent loop, THEN it calls `IsPhaseNeedsConfirm(userID)` which only checks `tmpl.Phases[ws.PhaseIndex].NeedsConfirm` — it does NOT check whether `ws.PhaseOutputs[ws.CurrentPhase]` exists, making the gate unable to distinguish first execution from post-output confirmation

### Expected Behavior (Correct)

2.1 WHEN the engine returns `RunAgentLoop=true` for a NeedsConfirm phase that has NO prior output (`hasOutput=false`, first execution), THEN the NeedsConfirm gate in the agent loop SHALL NOT force-return on the LLM's output — it SHALL let the agent loop continue until the LLM completes generating the full phase document

2.2 WHEN the engine returns `RunAgentLoop=true` for a NeedsConfirm phase that HAS prior output (`hasOutput=true`, post-output modification), THEN the NeedsConfirm gate SHALL continue to force-return after the LLM produces substantive output, preserving the existing confirmation workflow

2.3 WHEN the agent loop completes naturally (or via other termination conditions like hard cap) during first execution of a NeedsConfirm phase, THEN `SavePhaseOutput` SHALL capture the full phase document (not a preamble), and the response SHALL be returned to the user for confirmation

2.4 WHEN the fix is applied, THEN it SHALL work generically across all 19 workflow templates without any template-specific or keyword-based logic — the gate's behavior SHALL be driven by the engine's `hasOutput` state for the current phase

2.5 WHEN the engine exposes the `hasOutput` state to the GUI layer (e.g., via a new `HasPhaseOutput(userID)` method or a field on `WorkflowResponse`), THEN the GUI layer SHALL use this state to conditionally apply the NeedsConfirm gate — only activating the gate when `hasOutput=true`

### Unchanged Behavior (Regression Prevention)

3.1 WHEN a NeedsConfirm phase already HAS output and the LLM produces substantive text (during modification), THEN the NeedsConfirm gate SHALL CONTINUE TO force-return the response for user confirmation — the existing post-output confirmation workflow is unchanged

3.2 WHEN a phase has `NeedsConfirm=false` (e.g., implementation, ppt_generation), THEN `IsPhaseNeedsConfirm` SHALL CONTINUE TO return false and the NeedsConfirm gate SHALL NOT activate — execution phases are unaffected

3.3 WHEN the LLM outputs a short non-substantive preamble (< 200 runes, no markdown structure) during first execution, THEN `isSubstantivePhaseDocument()` SHALL CONTINUE TO return false and the agent loop SHALL continue — the existing short-preamble bypass from the `workflow-start-premature-exit` fix is preserved

3.4 WHEN `needsConfirmFromSteering` is computed for a pure steering-driven coding workflow (no WorkflowEngine active), THEN the steering-based NeedsConfirm gate SHALL CONTINUE TO use `gateConfig.active && iteration > 0` as before — the fix only affects the engine-based path

3.5 WHEN the user sends confirmWords/skipWords during a NeedsConfirm phase with existing output, THEN `HandleInput` SHALL CONTINUE TO advance/skip the phase as before — the engine's confirm/skip logic is unchanged

3.6 WHEN the doc preview panel receives a `doc_update` event after the NeedsConfirm gate force-returns (post-output path), THEN the panel SHALL CONTINUE TO display the document correctly — desktop doc preview behavior is unchanged

3.7 WHEN the `maxConsecutiveNoTool` hard cap (5 consecutive no-tool iterations) is reached during first execution, THEN the agent loop SHALL CONTINUE TO force-return the response — the hard cap safety mechanism is unaffected by the NeedsConfirm gate bypass

3.8 WHEN `SavePhaseOutput` is called after the agent loop completes during first execution, THEN it SHALL CONTINUE TO store the full content, run quality gate checks, and emit doc preview events — the post-loop capture path is unchanged

---

## Bug Condition (Formal)

```pascal
FUNCTION isBugCondition(X)
  INPUT: X of type AgentLoopIteration
  OUTPUT: boolean

  // Returns true when:
  // 1. The NeedsConfirm gate is active (engine-based: IsPhaseNeedsConfirm=true)
  // 2. The current phase has NO prior output (first execution)
  // 3. The LLM output is substantive (isSubstantivePhaseDocument=true)
  // 4. The LLM output is not a stall reply
  //
  // Under these conditions, the gate incorrectly force-returns the preamble
  // as the phase deliverable, terminating the agent loop prematurely.

  trimmed := TrimSpace(StripThinkingTags(X.MsgContent))

  RETURN X.NeedsConfirmFromEngine = true
     AND NOT hasPhaseOutput(X.UserID, currentPhaseID(X.UserID))
     AND trimmed != ""
     AND NOT looksLikeNoToolStallReply(X.MsgContent)
     AND isSubstantivePhaseDocument(trimmed)
END FUNCTION
```

```pascal
// Property: Fix Checking — First execution does not trigger NeedsConfirm gate
FOR ALL X WHERE isBugCondition(X) DO
  result := NeedsConfirmGate_fixed(X)
  ASSERT result.forceReturn = false
  ASSERT result.action = "continue"  // agent loop continues to generate full document
END FOR
```

```pascal
// Property: Preservation Checking — Post-output confirmation unchanged
FOR ALL X WHERE NOT isBugCondition(X) DO
  ASSERT NeedsConfirmGate_original(X) = NeedsConfirmGate_fixed(X)
END FOR
```

```pascal
// Property: Preservation — Post-output substantive text still force-returns
FOR ALL X WHERE X.NeedsConfirmFromEngine = true
              AND hasPhaseOutput(X.UserID, currentPhaseID(X.UserID))
              AND isSubstantivePhaseDocument(TrimSpace(StripThinkingTags(X.MsgContent)))
              AND NOT looksLikeNoToolStallReply(X.MsgContent) DO
  result := NeedsConfirmGate_fixed(X)
  ASSERT result.forceReturn = true  // existing behavior preserved
END FOR
```
