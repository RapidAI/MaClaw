# Workflow Start Premature Exit — Bugfix Design

## Overview

NeedsConfirm gate 在 `gui/im_message_handler.go` 的 no-tool 分支中，对 LLM 的非空、非 stall 输出一律 force-return。当工作流启动后 LLM 先输出一段短过渡开场白（如"好的，准备开工！"），gate 将其误判为阶段产出物并提前终止 agent loop，导致用户看不到需求文档。

修复方向：在 force-return 条件中新增 `isSubstantivePhaseDocument()` 检查，区分短过渡文本和实质性阶段文档。只有通过此检查的内容才触发 force-return。

## Glossary

- **Bug_Condition (C)**: NeedsConfirm gate 遇到短过渡文本（非空、非 stall、但不是实质性文档）时错误地 force-return
- **Property (P)**: 短过渡文本不触发 force-return，实质性文档正确触发 force-return
- **Preservation**: 实质性文档的 force-return 行为、非 NeedsConfirm 阶段的正常执行、空文本的 recover 路径、hard cap 等均不受影响
- **NeedsConfirm gate**: `gui/im_message_handler.go` no-tool 分支中的工作流阶段确认拦截逻辑（~line 4690）
- **`looksLikeNoToolStallReply()`**: 现有的 stall 检测函数，匹配"先想想"/"先分析"等关键词
- **`isSubstantivePhaseDocument()`**: 本次新增的函数，判断 LLM 输出是否构成实质性阶段文档
- **`trimmedForGate`**: LLM 输出经 `stripThinkingTags` + `TrimSpace` 后的文本，用于 gate 判断

## Bug Details

### Bug Condition

NeedsConfirm gate 在 no-tool 分支中，当 `trimmedForGate != ""` 且 `!looksLikeNoToolStallReply(msgContent)` 时立即 force-return。该条件不区分短过渡开场白和实质性阶段文档，导致 LLM 在 iteration 1 输出短文本后 agent loop 被提前终止。

**Formal Specification:**
```
FUNCTION isBugCondition(input)
  INPUT: input of type AgentLoopIteration
  OUTPUT: boolean

  trimmed := TrimSpace(StripThinkingTags(input.MsgContent))

  RETURN input.NeedsConfirmActive = true
     AND trimmed != ""
     AND NOT looksLikeNoToolStallReply(input.MsgContent)
     AND NOT isSubstantivePhaseDocument(trimmed)
END FUNCTION
```

**Where `isSubstantivePhaseDocument(text)`** returns true when ANY of the following hold:
1. `len([]rune(text)) >= 200` (sufficient length to be a document)
2. Text contains Markdown heading markers (`# `, `## `, `### `)
3. Text contains numbered list patterns (`1. `, `2. `, `1、`, `2、`)
4. Text contains multiple bullet points (`- ` appearing 3+ times on separate lines)

### Examples

- **"好的，准备开工！我将为您启动开发工作流..."** (42 chars, no structure markers) → `isBugCondition = true` → gate should NOT force-return (current behavior: force-returns )
- **"OK, let me start the workflow for you!"** (38 chars, no structure markers) → `isBugCondition = true` → gate should NOT force-return (current behavior: force-returns )
- **"# 需求文档\n\n## 1. 功能需求\n\n1. 用户可以...\n2. 系统应当..."** (200+ chars, has headings + numbered list) → `isBugCondition = false` → gate should force-return - **"让我先分析一下需求..."** (contains stall keyword "先分析") → `looksLikeNoToolStallReply = true` → gate condition not entered, continues loop 
## Expected Behavior

### Preservation Requirements

**Unchanged Behaviors:**
- 完整的需求/设计/任务文档（含 Markdown 标题、结构化列表，长度 >= 200 字符）在 NeedsConfirm 阶段仍触发 force-return 等待用户确认
- 非 NeedsConfirm 阶段（implementation, ToolFilterFull）的 agent loop 正常执行
- 空文本或纯 thinking tags 走 empty result recover 路径
- steering-based 编码工作流的 NeedsConfirm gate 对实质性内容仍 force-return
- 每个用户消息触发新 agent loop，gate 状态独立评估
- `maxConsecutiveNoTool` hard cap（连续 5 次无工具调用后第 6 次 force-return）不受影响
- 桌面平台 doc preview 面板在 force-return 时正确显示文档内容

**Scope:**
所有不涉及 NeedsConfirm gate force-return 条件判断的代码路径完全不受影响。具体包括：
- tool 分支的 NeedsConfirm gate（已有独立逻辑）
- 工具调用的过滤/拦截（coding tool gate）
- `SavePhaseOutput` / `HandleInput` 的阶段推进逻辑
- 对话历史保存路径

## Hypothesized Root Cause

Based on the bug description, the root cause is in the NeedsConfirm gate condition at `gui/im_message_handler.go` ~line 4716:

```go
if trimmedForGate != "" &&
    !looksLikeNoToolStallReply(msgContent) {
    // force-return
}
```

1. **条件过于宽松**: 只检查"非空"和"非 stall"，缺少"是否为实质性文档"的判断。任何非空、非 stall 的文本——包括短开场白——都会触发 force-return。

2. **`looksLikeNoToolStallReply` 覆盖不足**: 该函数只匹配特定的 stall 关键词（"先想想"/"先分析"等），不识别过渡性开场白（"好的，准备开工！"/"OK, starting now!"），因为开场白不是 stall reply——它是一种不同类型的非实质性输出。

3. **缺少文档结构检测**: 系统没有区分"LLM 正在热身的过渡文本"和"LLM 已完成的阶段文档"的机制。阶段文档通常具有明确的结构特征（Markdown 标题、编号列表、足够的长度），而过渡文本通常很短且无结构。

## Correctness Properties

Property 1: Bug Condition - Short Preamble Does Not Trigger Force-Return

_For any_ agent loop iteration where NeedsConfirm is active and the LLM output is non-empty, not a stall reply, and NOT a substantive phase document (length < 200 runes AND no markdown headings AND no numbered lists AND fewer than 3 bullet lines), the NeedsConfirm gate SHALL allow the agent loop to continue (no force-return).

**Validates: Requirements 2.1, 2.3**

Property 2: Preservation - Substantive Documents Still Force-Return

_For any_ agent loop iteration where NeedsConfirm is active and the LLM output IS a substantive phase document (length >= 200 runes OR contains markdown headings OR contains numbered list patterns OR contains 3+ bullet lines), the NeedsConfirm gate SHALL force-return the content for user confirmation, preserving the existing document confirmation workflow.

**Validates: Requirements 3.1, 3.4**

## Fix Implementation

### Changes Required

Assuming our root cause analysis is correct:

**File**: `gui/im_message_handler.go`

**Function**: New standalone function `isSubstantivePhaseDocument(text string) bool`

**Specific Changes**:

1. **Add `isSubstantivePhaseDocument` function** (~20 lines):
   - Accept the already-trimmed text (post `stripThinkingTags` + `TrimSpace`)
   - Return `true` if ANY of the following conditions hold:
     - `len([]rune(text)) >= 200` — sufficient length to be a document
     - Text contains Markdown heading markers: regex `(?m)^#{1,6}\s+\S` matches
     - Text contains numbered list patterns: regex `(?m)^(?:\d+[\.\、])\s+\S` matches
     - Text contains 3+ bullet list lines: count lines matching `(?m)^[-*]\s+\S` >= 3
   - Return `false` otherwise (short preamble / transitional text)

2. **Modify NeedsConfirm gate condition** (line ~4716):
   - Change from:
     ```go
     if trimmedForGate != "" &&
         !looksLikeNoToolStallReply(msgContent) {
     ```
   - To:
     ```go
     if trimmedForGate != "" &&
         !looksLikeNoToolStallReply(msgContent) &&
         isSubstantivePhaseDocument(trimmedForGate) {
     ```

3. **Add trace logging for skipped preambles**:
   - When `trimmedForGate != ""` and `!looksLikeNoToolStallReply` but `!isSubstantivePhaseDocument`, log at info level: `"NeedsConfirm gate: skipping non-substantive preamble (len=%d), allowing loop to continue"`
   - This aids debugging without changing control flow.

4. **Tool branch NeedsConfirm gate** (~line 5412):
   - Apply the same `isSubstantivePhaseDocument` check to the tool branch for consistency:
     ```go
     if trimmedAfterTools != "" && !looksLikeNoToolStallReply(msgContent) &&
         isSubstantivePhaseDocument(trimmedAfterTools) {
     ```

5. **Unit test file**: Add `isSubstantivePhaseDocument` tests to verify the classification boundary.

## Testing Strategy

### Validation Approach

The testing strategy follows a two-phase approach: first, surface counterexamples that demonstrate the bug on unfixed code, then verify the fix works correctly and preserves existing behavior.

### Exploratory Bug Condition Checking

**Goal**: Surface counterexamples that demonstrate the bug BEFORE implementing the fix. Confirm or refute the root cause analysis. If we refute, we will need to re-hypothesize.

**Test Plan**: Write tests that simulate NeedsConfirm gate evaluation with short preamble inputs. Run these tests on the UNFIXED code to observe that the gate incorrectly force-returns.

**Test Cases**:
1. **Chinese Preamble Test**: Input "好的，准备开工！我将为您启动开发工作流..." with NeedsConfirm=true → gate force-returns (will fail on unfixed code — gate should NOT force-return)
2. **English Preamble Test**: Input "OK, let me start working on this for you!" with NeedsConfirm=true → gate force-returns (will fail on unfixed code)
3. **Mixed Preamble Test**: Input "好的！Let me prepare the requirements document." with NeedsConfirm=true → gate force-returns (will fail on unfixed code)
4. **Edge Case - 199 chars**: Input of exactly 199 characters with no structure markers → gate force-returns (will fail on unfixed code)

**Expected Counterexamples**:
- All short preamble inputs (< 200 chars, no markdown structure) trigger force-return
- Root cause confirmed: the condition `trimmedForGate != ""` is too loose

### Fix Checking

**Goal**: Verify that for all inputs where the bug condition holds, the fixed function produces the expected behavior.

**Pseudocode:**
```
FOR ALL input WHERE isBugCondition(input) DO
  result := NeedsConfirmGate_fixed(input)
  ASSERT result.forceReturn = false
  ASSERT result.action = "continue"
END FOR
```

### Preservation Checking

**Goal**: Verify that for all inputs where the bug condition does NOT hold, the fixed function produces the same result as the original function.

**Pseudocode:**
```
FOR ALL input WHERE NOT isBugCondition(input) DO
  ASSERT NeedsConfirmGate_original(input) = NeedsConfirmGate_fixed(input)
END FOR
```

**Testing Approach**: Property-based testing is recommended for preservation checking because:
- It generates many test cases automatically across the input domain (varying lengths, markdown structures, languages)
- It catches edge cases that manual unit tests might miss (e.g., text that is exactly 200 chars, text with headings but very short)
- It provides strong guarantees that behavior is unchanged for all substantive document inputs

**Test Plan**: Observe behavior on UNFIXED code first for substantive document inputs, then write property-based tests capturing that behavior.

**Test Cases**:
1. **Substantive Document Preservation**: Generate random markdown documents (200+ chars with headings and lists) → verify gate still force-returns after fix
2. **Non-NeedsConfirm Preservation**: Generate random inputs with NeedsConfirm=false → verify gate is never entered
3. **Empty/Stall Preservation**: Generate empty strings and strings containing stall keywords → verify gate skips them as before

### Unit Tests

- Test `isSubstantivePhaseDocument` with short preambles (Chinese, English, mixed) → returns false
- Test `isSubstantivePhaseDocument` with markdown documents (headings, numbered lists, bullets) → returns true
- Test `isSubstantivePhaseDocument` with edge cases: exactly 200 chars, 199 chars, heading-only short text, numbered-list-only short text
- Test `isSubstantivePhaseDocument` with thinking tags stripped text
- Test gate integration: NeedsConfirm=true + short preamble → loop continues
- Test gate integration: NeedsConfirm=true + full document → force-return

### Property-Based Tests

- Generate random short strings (0-199 runes, no markdown markers) → `isSubstantivePhaseDocument` returns false, gate does not force-return
- Generate random markdown documents (200+ runes with headings/lists) → `isSubstantivePhaseDocument` returns true, gate force-returns
- Generate random strings of any length containing `# ` heading markers → `isSubstantivePhaseDocument` returns true regardless of length
- Generate mixed inputs and verify the gate's force-return decision matches `isSubstantivePhaseDocument` output

### Integration Tests

- End-to-end: simulate workflow start → LLM iteration 1 outputs preamble → verify loop continues → LLM iteration 2 outputs full document → verify force-return
- Verify doc preview panel receives event only when substantive document triggers force-return, not on preamble skip
- Verify `SavePhaseOutput` is called after substantive document force-return (not after preamble skip)
