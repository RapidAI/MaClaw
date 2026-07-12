# Workflow Self-Confirm Bypass — Bugfix Design

## Overview

在 NeedsConfirm 阶段（如 PPT 工作流的 slide_scripting），LLM 在单次响应中完成"输出交付物 → 请求确认 → 自我确认 → 开始下一阶段"的完整链路，绕过了用户确认环节。NeedsConfirm gate 在 LLM 完成完整响应后才介入（iteration 边界），无法在确认请求和自我确认之间截断。

修复方向：在 NeedsConfirm gate 评估 `msgContent` 之前，新增 `containsSelfConfirmationPattern()` 检测函数和 `truncateAtConfirmationBoundary()` 截断函数。当检测到 LLM 在同一响应中既请求确认又自我确认时，截断响应至确认请求边界——只保留交付物和确认提示，丢弃自我确认和后续内容。截断后的文本仍需通过 `isSubstantivePhaseDocument()` 检查，确保 NeedsConfirm gate 正常 force-return。

此修复是通用的，适用于所有 19 个工作流模板的所有 NeedsConfirm 阶段，使用中英文双语模式匹配，不依赖模板特定逻辑。

## Glossary

- **Bug_Condition (C)**: NeedsConfirm 阶段的 LLM 响应中包含自我确认模式——LLM 既请求用户确认又自己回答了确认
- **Property (P)**: 检测到自我确认时，截断响应至确认请求边界，只返回交付物和确认提示给用户
- **Preservation**: 不含自我确认的正常 NeedsConfirm 响应、NeedsConfirm=false 阶段、首次执行跳过、补充信息处理等均不受影响
- **`containsSelfConfirmationPattern(text)`**: 本次新增的函数，检测 LLM 输出中是否存在"请求确认 + 自我回答"的模式
- **`truncateAtConfirmationBoundary(text)`**: 本次新增的函数，在确认请求位置截断文本，丢弃自我确认及后续内容
- **`isSubstantivePhaseDocument(text)`**: 现有函数，判断文本是否构成实质性阶段文档（≥200 rune 或含 Markdown 结构）
- **`trimmedForGate`**: LLM 输出经 `stripThinkingTags` + `TrimSpace` 后的文本，用于 gate 判断
- **NeedsConfirm gate**: `gui/im_message_handler.go` 中 no-tool 分支（~line 4763）和 tool 分支（~line 5610）的工作流阶段确认拦截逻辑
- **`SavePhaseOutput`**: `corelib/workflow/engine.go` 中存储阶段产出物的方法，在 gate force-return 后的 post-loop 路径中调用

## Bug Details

### Bug Condition

LLM 在 NeedsConfirm 阶段的单次响应中完成了完整的"交付物 → 确认请求 → 自我确认 → 下一阶段工作"链路。NeedsConfirm gate 在 iteration 结束后评估 `msgContent`，此时文本已包含自我确认内容。gate 将整个输出视为实质性文档并 force-return，但返回给用户的文本中已经包含了"好的，已确认！现在进入下一阶段..."等自我确认内容，以及可能的下一阶段工作产出。

`SavePhaseOutput` 随后存储了包含自我确认和下一阶段内容的完整文本，污染了当前阶段的产出物。

**Formal Specification:**
```
FUNCTION isBugCondition(input)
  INPUT: input of type AgentLoopIteration
  OUTPUT: boolean

  trimmed := TrimSpace(StripThinkingTags(input.MsgContent))

  needsConfirm := input.NeedsConfirmFromEngine OR input.NeedsConfirmFromSteering

  RETURN needsConfirm
     AND trimmed != ""
     AND NOT looksLikeNoToolStallReply(input.MsgContent)
     AND isSubstantivePhaseDocument(trimmed)
     AND containsSelfConfirmationPattern(trimmed)
END FUNCTION
```

```
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

### Examples

- **PPT slide_scripting 阶段**："...以上是全部20页的逐页脚本。\n\n请确认以上全部20页的逐页脚本，或提出修改意见。\n\n好的，逐页脚本已确认！现在进入最终阶段——PPT生成..." → `containsSelfConfirmationPattern = true` → 应截断至"请确认..."之后、"好的，逐页脚本已确认"之前
- **编码工作流 requirements 阶段**："# 需求文档\n\n## 功能需求\n...\n\n请确认以上需求，或提出修改意见。\n\n好的，需求已确认！现在开始技术设计..." → `containsSelfConfirmationPattern = true` → 应截断
- **英文工作流**："...Please confirm the above requirements, or suggest changes.\n\nConfirmed! Let me proceed to the design phase..." → `containsSelfConfirmationPattern = true` → 应截断
- **正常响应（无自我确认）**："# 需求文档\n\n## 功能需求\n...\n\n请确认以上需求，或提出修改意见。\n\n请输入：确认 或 修改意见" → `containsSelfConfirmationPattern = false` → 全文返回，不截断 - **无确认请求的文档**："# 技术设计文档\n\n## 架构设计\n..." → `findConfirmationRequest = -1` → `containsSelfConfirmationPattern = false` → 全文返回 
## Expected Behavior

### Preservation Requirements

**Unchanged Behaviors:**
- 不含自我确认模式的正常 NeedsConfirm 响应（交付物 + 确认提示）仍全文返回，等待用户确认
- NeedsConfirm=false 阶段（implementation, ppt_generation, test_execution）的 agent loop 正常执行，不触发自我确认检测
- 首次执行（`hasOutput=false`）时 NeedsConfirm gate 跳过（`workflow-double-confirm-fix` 行为保留）
- 用户提供补充信息时的修改请求处理（`workflow-continuation-needsconfirm-fix` 行为保留）
- 短前言（`isSubstantivePhaseDocument=false`）仍走 agent loop 继续路径（`workflow-start-premature-exit` 修复保留）
- `HandleInput` 的 confirm/skip/modify 逻辑不变
- `maxConsecutiveNoTool` hard cap（连续 5 次无工具调用后第 6 次 force-return）不受影响
- 桌面 doc preview 面板在 force-return 时正确显示文档内容（截断后的干净内容）
- steering-based 编码工作流的 NeedsConfirm gate 同样应用自我确认检测

**Scope:**
所有不涉及 NeedsConfirm gate force-return 路径中 `msgContent` 预处理的代码完全不受影响。具体包括：
- Coding Tool Gate 的工具拦截逻辑
- 工具调用的过滤/执行路径
- `SavePhaseOutput` / `HandleInput` 的阶段推进逻辑
- 对话历史保存路径
- 意图理解 / QuickFilter 分类
- `isSubstantivePhaseDocument` 函数本身不修改

## Hypothesized Root Cause

Based on the bug description and code analysis, the root cause is in the NeedsConfirm gate's text evaluation logic:

1. **Gate 不检测自我确认模式**: NeedsConfirm gate（no-tool 分支 ~line 4763，tool 分支 ~line 5610）在评估 `trimmedForGate` / `trimmedAfterTools` 时，只检查 `!= ""` + `!looksLikeNoToolStallReply` + `isSubstantivePhaseDocument`。当 LLM 在单次响应中自我确认时，整个文本仍然是非空、非 stall、且实质性的——gate 条件全部满足，force-return 整个包含自我确认的文本。

2. **Prompt 约束是软约束**: `BuildPhaseSystemPrompt` 中的 Section 6（"重要：等待用户确认"）指示 LLM "输出产出物后立即停止"、"绝对不要在同一次回复中既输出产出物又开始下一阶段的工作"。但这是 system prompt 中的文本指令，LLM 可以忽略。特别是在长文档生成场景中，LLM 的注意力可能已经偏离了 system prompt 中的约束。

3. **缺少 post-hoc 截断机制**: 系统在 gate 评估之前没有对 `msgContent` 做任何自我确认检测或截断。`stripThinkingTags` 只移除 `<think>` 标签，`looksLikeNoToolStallReply` 只检测 stall 关键词，`isSubstantivePhaseDocument` 只检测文档结构——没有任何函数检测"确认请求后跟自我回答"的模式。

4. **问题跨分支**: no-tool 分支和 tool 分支的 NeedsConfirm gate 都存在同样的问题。tool 分支中 LLM 可能在输出自我确认文本的同时调用下一阶段的工具（如 PPT 生成工具），`trimmedAfterTools` 同样包含自我确认内容。

## Correctness Properties

Property 1: Bug Condition - Self-Confirmed Responses Are Truncated

_For any_ NeedsConfirm phase response where the LLM output contains a self-confirmation pattern (a confirmation request followed by a self-answer or phase transition), the system SHALL detect the pattern and truncate the response at the confirmation request boundary, returning only the deliverable and confirmation prompt to the user. The truncated text SHALL still pass `isSubstantivePhaseDocument()` and trigger the NeedsConfirm gate's force-return.

**Validates: Requirements 2.1, 2.2, 2.6**

Property 2: Preservation - Non-Self-Confirmed Responses Unchanged

_For any_ NeedsConfirm phase response where the LLM output does NOT contain a self-confirmation pattern (normal case: deliverable + confirmation prompt only, or deliverable without any confirmation request), the system SHALL return the full response unchanged, preserving the existing NeedsConfirm gate behavior.

**Validates: Requirements 2.5, 3.1**

## Fix Implementation

### Changes Required

Assuming our root cause analysis is correct:

**File**: `gui/im_message_handler.go`

**New Functions**:

1. **`containsSelfConfirmationPattern(text string) bool`** (~40 lines):
   - Accept the already-trimmed text (post `stripThinkingTags` + `TrimSpace`)
   - Step 1: Find a confirmation request position using `findConfirmationRequestPos(text)`
     - Chinese patterns: `请确认`, `请输入：确认`, `请输入: 确认`, `请查看并确认`, `确认后我将`
     - English patterns: `please confirm`, `please review and confirm`, `confirm or suggest`
   - Step 2: If no confirmation request found, return `false`
   - Step 3: Extract text after the confirmation request (skip forward past the confirmation request line/paragraph)
   - Step 4: Check if the text after the request contains a self-answer:
     - Chinese self-answer: `已确认`, `确认完毕`, `确认！`, `好的，.*确认`, `收到确认`, `确认后.*现在`, `现在进入`, `开始生成`, `进入下一`, `进入最终`
     - English self-answer: `confirmed`, `proceeding to`, `moving on to`, `let me start`, `let me proceed`, `now entering`
   - Return `true` if self-answer found, `false` otherwise
   - Use compiled regexes at package level for performance

2. **`truncateAtConfirmationBoundary(text string) string`** (~30 lines):
   - Find the confirmation request position using the same `findConfirmationRequestPos`
   - Scan forward from the confirmation request to find the end of the confirmation prompt paragraph (next `\n\n` or end of line after the request)
   - Truncate at that boundary, preserving the confirmation request text itself
   - Trim trailing whitespace
   - If the truncated text is empty or too short (< 50 runes), return the original text unchanged (safety fallback)
   - Return the truncated text

3. **`findConfirmationRequestPos(text string) int`** (~20 lines):
   - Helper function used by both `containsSelfConfirmationPattern` and `truncateAtConfirmationBoundary`
   - Search for the LAST occurrence of a confirmation request pattern in the text (the confirmation request typically appears near the end of the deliverable, before the self-answer)
   - Return the byte position of the match start, or -1 if not found
   - Use a compiled regex combining Chinese and English patterns

**Modification 1**: No-tool branch NeedsConfirm gate (~line 4763)

4. **Add self-confirmation detection and truncation before gate evaluation**:
   - After computing `trimmedForGate` and before the `isSubstantivePhaseDocument` check
   - When `containsSelfConfirmationPattern(trimmedForGate)` returns true:
     - Call `truncateAtConfirmationBoundary(trimmedForGate)` to get the clean text
     - Replace `trimmedForGate` with the truncated text
     - Also update `msgContent` to the truncated version (so `SavePhaseOutput` receives clean content)
     - Log at info level: `"NeedsConfirm gate: detected self-confirmation pattern, truncated at confirmation boundary (originalLen=%d truncatedLen=%d)"`
   - The existing `isSubstantivePhaseDocument(trimmedForGate)` check then evaluates the truncated text
   - If the truncated text still passes `isSubstantivePhaseDocument`, gate force-returns the clean content
   - If the truncated text does NOT pass `isSubstantivePhaseDocument` (edge case: very short deliverable), fall through to let the loop continue

**Modification 2**: Tool branch NeedsConfirm gate (~line 5610)

5. **Apply the same self-confirmation detection and truncation**:
   - After computing `trimmedAfterTools` and before the `isSubstantivePhaseDocument` check
   - Same logic as the no-tool branch: detect → truncate → update `msgContent` → proceed with gate evaluation

**Modification 3**: Trace logging

6. **Add trace events for self-confirmation detection**:
   - When self-confirmation is detected and truncated, emit a trace event: `"gate.self_confirm_truncated"`
   - Include original length, truncated length, and the detected self-answer pattern in the trace

**No changes to**:
- `isSubstantivePhaseDocument()` — existing function unchanged
- `looksLikeNoToolStallReply()` — existing function unchanged
- `BuildPhaseSystemPrompt()` — prompt constraint kept as defense-in-depth
- `HasPhaseOutput()` / `IsPhaseNeedsConfirm()` — engine methods unchanged
- `HandleInput()` — confirm/skip/modify logic unchanged
- Any workflow template definitions
- `SavePhaseOutput()` — function unchanged, but now receives cleaner content

## Testing Strategy

### Validation Approach

The testing strategy follows a two-phase approach: first, surface counterexamples that demonstrate the bug on unfixed code, then verify the fix works correctly and preserves existing behavior.

### Exploratory Bug Condition Checking

**Goal**: Surface counterexamples that demonstrate the bug BEFORE implementing the fix. Confirm or refute the root cause analysis. If we refute, we will need to re-hypothesize.

**Test Plan**: Write tests that simulate NeedsConfirm gate evaluation with LLM responses containing self-confirmation patterns. Run these tests on the UNFIXED code to observe that the gate returns the full self-confirmed text without truncation.

**Test Cases**:
1. **PPT Slide Scripting Self-Confirm Test**: Simulate a response containing "请确认以上逐页脚本" followed by "好的，逐页脚本已确认！现在进入最终阶段" → gate returns full text including self-confirmation (will fail on unfixed code — should truncate)
2. **Coding Requirements Self-Confirm Test**: Simulate a response containing "请确认以上需求" followed by "好的，需求已确认！现在开始技术设计" → gate returns full text (will fail on unfixed code)
3. **English Self-Confirm Test**: Simulate a response containing "Please confirm the above" followed by "Confirmed! Let me proceed to design" → gate returns full text (will fail on unfixed code)
4. **Phase Transition Without Explicit Confirm Test**: Simulate a response containing "请确认" followed by "现在进入下一阶段" (no explicit "已确认" but has phase transition) → gate returns full text (will fail on unfixed code)

**Expected Counterexamples**:
- All self-confirmed responses are returned in full, including the self-answer and next-phase content
- Root cause confirmed: the gate has no self-confirmation detection mechanism

### Fix Checking

**Goal**: Verify that for all inputs where the bug condition holds, the fixed function produces the expected behavior.

**Pseudocode:**
```
FOR ALL input WHERE isBugCondition(input) DO
  result := processNeedsConfirmResponse_fixed(input)
  ASSERT result.text does NOT contain self-confirmation content
  ASSERT result.text ends at or near the confirmation request
  ASSERT isSubstantivePhaseDocument(result.text) = true
  ASSERT result.forceReturn = true
END FOR
```

### Preservation Checking

**Goal**: Verify that for all inputs where the bug condition does NOT hold, the fixed function produces the same result as the original function.

**Pseudocode:**
```
FOR ALL input WHERE NOT isBugCondition(input) DO
  ASSERT processNeedsConfirmResponse_original(input) = processNeedsConfirmResponse_fixed(input)
END FOR
```

**Testing Approach**: Property-based testing is recommended for preservation checking because:
- It generates many test cases automatically across the input domain (varying document lengths, languages, confirmation prompt styles)
- It catches edge cases that manual unit tests might miss (e.g., text containing "确认" as part of a requirement description, not as a confirmation request)
- It provides strong guarantees that behavior is unchanged for all non-self-confirmed inputs

**Test Plan**: Observe behavior on UNFIXED code first for normal NeedsConfirm responses (deliverable + confirmation prompt without self-answer), then write property-based tests capturing that behavior.

**Test Cases**:
1. **Normal Confirmation Prompt Preservation**: Generate random substantive documents ending with a confirmation prompt (no self-answer) → verify full text returned unchanged after fix
2. **No Confirmation Request Preservation**: Generate random substantive documents without any confirmation request → verify full text returned unchanged
3. **Non-NeedsConfirm Phase Preservation**: Generate random inputs with NeedsConfirm=false → verify gate never activates, self-confirmation detection never runs
4. **First Execution Preservation**: Generate random inputs with hasOutput=false → verify gate skips regardless of self-confirmation content

### Unit Tests

- Test `containsSelfConfirmationPattern` with Chinese self-confirm patterns → returns true
- Test `containsSelfConfirmationPattern` with English self-confirm patterns → returns true
- Test `containsSelfConfirmationPattern` with normal confirmation prompt (no self-answer) → returns false
- Test `containsSelfConfirmationPattern` with no confirmation request at all → returns false
- Test `containsSelfConfirmationPattern` with "确认" appearing in requirement text (not as confirmation request) → returns false
- Test `truncateAtConfirmationBoundary` with Chinese self-confirm → truncates correctly, preserves confirmation prompt
- Test `truncateAtConfirmationBoundary` with English self-confirm → truncates correctly
- Test `truncateAtConfirmationBoundary` with very short deliverable before confirmation → returns original (safety fallback)
- Test `truncateAtConfirmationBoundary` result passes `isSubstantivePhaseDocument` for typical documents
- Test `findConfirmationRequestPos` with various confirmation request patterns → returns correct position
- Test `findConfirmationRequestPos` with no confirmation request → returns -1
- Test gate integration: NeedsConfirm=true + self-confirmed response → truncated text returned
- Test gate integration: NeedsConfirm=true + normal response → full text returned unchanged
- Test gate integration: tool branch + self-confirmed response → truncated text returned

### Property-Based Tests

- Generate random substantive documents (200+ runes with headings/lists) + append random confirmation request + append random self-answer → `containsSelfConfirmationPattern` returns true AND `truncateAtConfirmationBoundary` produces text that does NOT contain the self-answer AND passes `isSubstantivePhaseDocument`
- Generate random substantive documents + append random confirmation request WITHOUT self-answer → `containsSelfConfirmationPattern` returns false AND full text returned unchanged
- Generate random substantive documents WITHOUT any confirmation request → `containsSelfConfirmationPattern` returns false AND full text returned unchanged
- Generate random strings containing "确认" in non-confirmation-request contexts (e.g., "用户确认功能需求", "确认按钮样式") → `containsSelfConfirmationPattern` returns false (no false positives)

### Integration Tests

- End-to-end: simulate NeedsConfirm phase → LLM outputs deliverable + self-confirmation → verify gate truncates and force-returns clean content → verify `SavePhaseOutput` stores only the deliverable + confirmation prompt
- End-to-end: simulate NeedsConfirm phase → LLM outputs deliverable + normal confirmation prompt → verify gate force-returns full content unchanged
- Verify doc preview panel receives truncated (clean) content when self-confirmation is detected
- Verify both no-tool and tool branches apply self-confirmation detection consistently
- Verify all 19 workflow templates' NeedsConfirm phases benefit from the fix (generic, no template-specific logic)
