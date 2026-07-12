# Bugfix Requirements Document

## Introduction

在 PPT 生成（presentation_design）工作流中，当 LLM 完成逐页脚本（slide_scripting）阶段的文档生成后，在同一个 LLM 响应中：先输出确认请求（"请确认以上全部20页的逐页脚本，或提出修改意见"），紧接着自己回答（"好的，逐页脚本已确认！现在进入最终阶段"），然后直接开始 PPT 生成阶段的工作。LLM 在一次输出中既提出了确认请求又自我确认，完全跳过了用户输入环节。

此 bug 的核心问题是 NeedsConfirm gate 只能在 LLM 完成一次完整响应后才介入（iteration 边界），无法在 LLM 响应的中途（确认请求和自我确认之间）截断。当 LLM 在单次响应中完成"输出文档 → 请求确认 → 自我确认 → 开始下一阶段"的完整链路时，gate 来不及介入。

用户要求"从原理上解决此类未确认就继续的工作流上的问题"——需要一个通用的、结构性的修复，适用于所有 19 个工作流模板的所有 NeedsConfirm 阶段，而不是针对 PPT 工作流的特殊处理。

## Bug Analysis

### Current Behavior (Defect)

1.1 WHEN the LLM generates a response for a NeedsConfirm phase AND the response contains both the phase deliverable AND a self-confirmation pattern (e.g., "已确认"、"确认完毕"、"进入下一阶段") AND the start of next-phase work, all within a single `msgContent`, THEN the NeedsConfirm gate sees the entire output as one substantive document and force-returns it — but the returned text already contains the self-confirmation and next-phase content, effectively bypassing user confirmation

1.2 WHEN the phase prompt (`BuildPhaseSystemPrompt`) instructs the LLM to "output the deliverable and then stop, asking for confirmation" (Section 6: "重要：等待用户确认"), THEN the LLM sometimes ignores this instruction and continues generating beyond the confirmation request within the same streaming response, because the instruction is a soft constraint in the system prompt with no hard enforcement mechanism

1.3 WHEN the NeedsConfirm gate in the no-tool branch evaluates `msgContent` after the LLM finishes its full response for an iteration, THEN it has no opportunity to intervene between the confirmation request portion and the self-confirmation portion of the text — the gate operates at iteration granularity, not at token/sentence granularity

1.4 WHEN the same self-confirm-and-continue pattern occurs in the tool branch (LLM outputs text containing self-confirmation AND makes tool calls for the next phase in the same iteration), THEN the tool branch NeedsConfirm gate similarly cannot intervene mid-response — it evaluates `trimmedAfterTools` which already contains the self-confirmed text

1.5 WHEN the LLM self-confirms and the response is force-returned to the user, THEN `SavePhaseOutput` captures the entire output including the self-confirmation text and next-phase content as the current phase's output, polluting the phase output with content that belongs to the next phase

1.6 WHEN this bug occurs on any of the 19 workflow templates with NeedsConfirm=true phases (not just presentation_design's slide_scripting), THEN the same self-confirm bypass can happen because the root cause is in the generic NeedsConfirm gate logic and the soft prompt constraint, not in any template-specific code

### Expected Behavior (Correct)

2.1 WHEN the LLM generates a response for a NeedsConfirm phase AND the response contains a self-confirmation pattern after the deliverable content, THEN the system SHALL detect the self-confirmation pattern and truncate the response at the confirmation request boundary — returning only the deliverable and the confirmation prompt to the user, discarding the self-confirmation and any subsequent content

2.2 WHEN the system detects a self-confirmation pattern in a NeedsConfirm phase response, THEN it SHALL strip the self-confirmed portion from `msgContent` before force-returning, ensuring `SavePhaseOutput` captures only the legitimate phase deliverable (without self-confirmation text or next-phase content)

2.3 WHEN the phase prompt instructs the LLM to stop and wait for confirmation, THEN the system SHALL have a hard enforcement mechanism (beyond the soft prompt constraint) that detects and prevents self-confirmation — this mechanism SHALL operate on the LLM's complete response text before the NeedsConfirm gate evaluates it

2.4 WHEN the self-confirmation detection and truncation is applied, THEN it SHALL work generically across all 19 workflow templates and all NeedsConfirm phases — the detection SHALL use language-agnostic patterns (both Chinese and English self-confirmation phrases) without template-specific or phase-specific logic

2.5 WHEN the LLM's response for a NeedsConfirm phase does NOT contain a self-confirmation pattern (normal case: deliverable + confirmation prompt only), THEN the system SHALL return the full response unchanged — the truncation mechanism SHALL only activate when self-confirmation is detected

2.6 WHEN the self-confirmation detection truncates the response, THEN the truncated response SHALL still be a valid substantive document that passes `isSubstantivePhaseDocument()` — ensuring the NeedsConfirm gate still force-returns it for user confirmation

### Unchanged Behavior (Regression Prevention)

3.1 WHEN a NeedsConfirm phase response contains only the deliverable and a confirmation prompt (no self-confirmation), THEN the system SHALL CONTINUE TO force-return the full response for user confirmation — the normal NeedsConfirm gate flow is unchanged

3.2 WHEN a NeedsConfirm=false phase (e.g., implementation, ppt_generation) generates a response, THEN the system SHALL CONTINUE TO let the agent loop proceed without NeedsConfirm gate intervention — execution phases are unaffected

3.3 WHEN the NeedsConfirm gate evaluates a response during first execution (hasOutput=false), THEN the system SHALL CONTINUE TO skip the gate and let the agent loop continue — the `workflow-double-confirm-fix` behavior is preserved

3.4 WHEN the user provides supplementary information during a NeedsConfirm phase, THEN the system SHALL CONTINUE TO treat it as a modification request — the `workflow-continuation-needsconfirm-fix` behavior is preserved

3.5 WHEN the LLM outputs a short non-substantive preamble during any phase, THEN `isSubstantivePhaseDocument()` SHALL CONTINUE TO return false and the agent loop SHALL continue — the `workflow-start-premature-exit` fix is preserved

3.6 WHEN the user sends confirmWords/skipWords during a NeedsConfirm phase with existing output, THEN `HandleInput` SHALL CONTINUE TO advance/skip the phase as before — the engine's confirm/skip logic is unchanged

3.7 WHEN `SavePhaseOutput` is called after the NeedsConfirm gate force-returns, THEN it SHALL CONTINUE TO store the content, run quality gate checks, and emit doc preview events — the post-loop capture path is unchanged, but now receives cleaner content (without self-confirmation pollution)

3.8 WHEN the steering-based NeedsConfirm gate (needsConfirmFromSteering) fires for pure steering-driven coding workflows, THEN the self-confirmation detection SHALL also apply — the fix is not limited to engine-based workflows

3.9 WHEN the tool branch NeedsConfirm gate evaluates `trimmedAfterTools`, THEN the self-confirmation detection and truncation SHALL also apply — both no-tool and tool branches are covered

---

## Bug Condition (Formal)

```pascal
FUNCTION isBugCondition(X)
  INPUT: X of type AgentLoopIteration
  OUTPUT: boolean

  // Returns true when:
  // 1. The NeedsConfirm gate is active (engine-based or steering-based)
  // 2. The LLM's response is substantive (isSubstantivePhaseDocument=true)
  // 3. The LLM's response contains a self-confirmation pattern after the
  //    deliverable content — i.e., the LLM both asks for confirmation
  //    AND answers its own confirmation request in the same response
  //
  // Self-confirmation patterns include:
  //   Chinese: "已确认"、"确认完毕"、"确认后"紧跟"现在"/"开始"/"进入"
  //   English: "confirmed", "proceeding to", "moving on to"
  //   Structural: confirmation request followed by "好的"/"OK" + next-phase action

  trimmed := TrimSpace(StripThinkingTags(X.MsgContent))

  needsConfirm := X.NeedsConfirmFromEngine OR X.NeedsConfirmFromSteering

  RETURN needsConfirm
     AND trimmed != ""
     AND NOT looksLikeNoToolStallReply(X.MsgContent)
     AND isSubstantivePhaseDocument(trimmed)
     AND containsSelfConfirmationPattern(trimmed)
END FUNCTION
```

```pascal
FUNCTION containsSelfConfirmationPattern(text)
  INPUT: text of type string (trimmed, thinking tags stripped)
  OUTPUT: boolean

  // Detects when the LLM both requests confirmation AND self-answers it.
  // The pattern is: deliverable content → confirmation request → self-answer → next-phase action
  //
  // Key indicators of self-confirmation:
  // 1. Text contains a confirmation request (请确认/请输入：确认/please confirm)
  //    followed by a self-answer (已确认/好的.*确认/confirmed)
  // 2. Text contains a phase transition after the confirmation request
  //    (进入下一阶段/进入最终阶段/开始生成/let me start/moving on)

  confirmRequestPos := findConfirmationRequest(text)
  IF confirmRequestPos < 0 THEN
    RETURN false  // No confirmation request found — not a self-confirm scenario
  END IF

  textAfterRequest := text[confirmRequestPos:]
  RETURN containsSelfAnswer(textAfterRequest)
     OR containsPhaseTransition(textAfterRequest)
END FUNCTION
```

```pascal
// Property: Fix Checking — Self-confirmed responses are truncated
FOR ALL X WHERE isBugCondition(X) DO
  result := processNeedsConfirmResponse_fixed(X)
  ASSERT result.text does NOT contain self-confirmation content
  ASSERT result.text ends at or near the confirmation request
  ASSERT isSubstantivePhaseDocument(result.text) = true  // still valid for gate
  ASSERT result.forceReturn = true  // gate still fires
END FOR
```

```pascal
// Property: Preservation Checking — Non-self-confirmed responses unchanged
FOR ALL X WHERE NOT isBugCondition(X) DO
  ASSERT processNeedsConfirmResponse_original(X) = processNeedsConfirmResponse_fixed(X)
END FOR
```

```pascal
// Property: Preservation — Normal NeedsConfirm gate behavior unchanged
FOR ALL X WHERE X.NeedsConfirm = true
              AND isSubstantivePhaseDocument(TrimSpace(StripThinkingTags(X.MsgContent)))
              AND NOT containsSelfConfirmationPattern(TrimSpace(StripThinkingTags(X.MsgContent))) DO
  result := processNeedsConfirmResponse_fixed(X)
  ASSERT result.text = StripThinkingTags(X.MsgContent)  // full text returned unchanged
  ASSERT result.forceReturn = true
END FOR
```
