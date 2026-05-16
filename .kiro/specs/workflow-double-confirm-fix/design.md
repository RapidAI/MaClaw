# Workflow Double-Confirm Fix — Bugfix Design

## Overview

NeedsConfirm gate 在 `gui/im_message_handler.go` 的 no-tool 分支和 tool 分支中，对 NeedsConfirm=true 阶段的 LLM 输出一律 force-return，不区分**首次执行**（尚无阶段产出物）和**产出后确认**（已有阶段产出物）。当工作流启动后 LLM 首轮输出一段实质性的计划概述/前言（≥200 rune 或含编号列表/标题），`isSubstantivePhaseDocument()` 返回 true，gate 将其误判为阶段交付物并提前终止 agent loop。用户必须发送第二条消息才能真正生成阶段文档——这是一个影响全部 19 个工作流模板的双重确认 bug。

修复方向：**Engine-State-Aware Gate**（方案 C）。在 `corelib/workflow/engine.go` 新增 `HasPhaseOutput(userID)` 方法，查询当前阶段是否已有产出物。GUI 层的 NeedsConfirm gate 在 `needsConfirmFromEngine=true` 时额外检查 `HasPhaseOutput`——仅当 `hasOutput=true`（产出后确认/修改场景）时才 force-return；`hasOutput=false`（首次执行）时跳过 gate，让 agent loop 继续生成完整文档。

此修复是**通用/结构性**的，不依赖关键词或模板特定逻辑，完全由引擎的 `PhaseOutputs` 状态驱动。

## Glossary

- **Bug_Condition (C)**: NeedsConfirm gate 在首次执行（`hasOutput=false`）时遇到实质性 LLM 输出，错误地 force-return
- **Property (P)**: 首次执行时 gate 不 force-return，让 agent loop 继续生成完整阶段文档
- **Preservation**: 产出后确认/修改场景的 force-return 行为、NeedsConfirm=false 阶段的正常执行、steering-based 路径、hard cap 等均不受影响
- **`HasPhaseOutput(userID)`**: 本次新增的 `WorkflowEngine` 导出方法，返回当前阶段是否已有产出物
- **`IsPhaseNeedsConfirm(userID)`**: 现有方法，检查当前阶段模板的 `NeedsConfirm` 属性
- **`isSubstantivePhaseDocument(text)`**: 现有函数，判断 LLM 输出是否构成实质性阶段文档（≥200 rune 或含 Markdown 结构）
- **`needsConfirmFromEngine`**: agent loop 中从 `IsPhaseNeedsConfirm` 计算的布尔值
- **`needsConfirmFromSteering`**: agent loop 中从 steering gate 配置计算的布尔值
- **`PhaseOutputs`**: `WorkflowState` 中的 `map[string]string`，key 为 phase ID，value 为阶段产出物内容

## Bug Details

### Bug Condition

NeedsConfirm gate 在 no-tool 分支（~line 4763）和 tool 分支（~line 5412）中，当 `needsConfirmFromEngine=true` 时立即检查 `isSubstantivePhaseDocument(trimmedForGate)` 并 force-return。该条件不区分首次执行和产出后确认——`IsPhaseNeedsConfirm(userID)` 只检查模板的 `NeedsConfirm` 属性，不检查 `ws.PhaseOutputs[ws.CurrentPhase]` 是否存在。

当 LLM 在首次执行的第一轮迭代中输出一段实质性的计划概述（如"收到，马上为您启动PPT制作工作流！我们将按以下步骤进行：1. 受众目标定义...2. 内容大纲...3. 视觉设计..."），`isSubstantivePhaseDocument` 返回 true（含编号列表且可能 ≥200 rune），gate force-return 将此前言当作阶段交付物返回给用户。

**Formal Specification:**
```
FUNCTION isBugCondition(input)
  INPUT: input of type AgentLoopIteration
  OUTPUT: boolean

  trimmed := TrimSpace(StripThinkingTags(input.MsgContent))

  RETURN input.NeedsConfirmFromEngine = true
     AND NOT HasPhaseOutput(input.UserID)
     AND trimmed != ""
     AND NOT looksLikeNoToolStallReply(input.MsgContent)
     AND isSubstantivePhaseDocument(trimmed)
END FUNCTION
```

### Examples

- **PPT 工作流首次执行**："收到，马上为您启动PPT制作工作流！我们将按以下步骤进行：\n1. 受众目标定义\n2. 内容大纲\n3. 视觉设计\n让我们开始吧！"（含编号列表，≥200 rune）→ `isBugCondition=true` → gate 应 NOT force-return（当前行为：force-return ❌）
- **编码工作流首次执行**："好的，我来为您生成需求文档。\n\n# 贪吃蛇游戏需求文档\n\n## 1. 功能需求\n..."（含 Markdown 标题，≥200 rune）→ `isBugCondition=true` → gate 应 NOT force-return（当前行为：force-return ❌）
- **产出后修改场景**：用户确认需求后说"把技术栈改成 React"，LLM 输出修改后的完整文档（hasOutput=true）→ `isBugCondition=false` → gate 应 force-return ✅
- **NeedsConfirm=false 阶段**：implementation 阶段 LLM 输出代码 → `needsConfirmFromEngine=false` → gate 不激活 ✅

## Expected Behavior

### Preservation Requirements

**Unchanged Behaviors:**
- 已有产出物的 NeedsConfirm 阶段（修改/确认场景），LLM 产出实质性文本后仍 force-return 等待用户确认
- NeedsConfirm=false 阶段（implementation, ppt_generation, test_execution）的 agent loop 正常执行
- 短前言（< 200 rune，无 Markdown 结构）仍走 `isSubstantivePhaseDocument=false` 路径，agent loop 继续（`workflow-start-premature-exit` 修复保留）
- steering-based 编码工作流（无 WorkflowEngine 活跃工作流）的 NeedsConfirm gate 使用 `gateConfig.active && iteration > 0` 不变
- `HandleInput` 的 confirm/skip/modify 逻辑不变
- `maxConsecutiveNoTool` hard cap（5 次连续无工具后 force-return）不受影响
- 桌面 doc preview 面板在产出后 force-return 时正确显示文档
- `SavePhaseOutput` 在 agent loop 自然完成后正确存储完整内容

**Scope:**
所有不涉及 `needsConfirmFromEngine` 条件判断的代码路径完全不受影响。具体包括：
- `needsConfirmFromSteering` 路径（纯 steering 驱动，无 WorkflowEngine）
- tool 过滤/拦截（coding tool gate）
- `SavePhaseOutput` / `HandleInput` 的阶段推进逻辑
- 对话历史保存路径
- 意图理解 / QuickFilter 分类

## Hypothesized Root Cause

Based on the bug description and code analysis, the root cause is in the NeedsConfirm gate condition:

1. **`IsPhaseNeedsConfirm` 不感知产出物状态**: 该方法（`engine.go` ~line 406）只检查 `tmpl.Phases[ws.PhaseIndex].NeedsConfirm`，不检查 `ws.PhaseOutputs[ws.CurrentPhase]` 是否存在。对于 NeedsConfirm=true 的阶段，无论是首次执行还是产出后确认，都返回 true。

2. **GUI 层无法区分首次执行和产出后确认**: `im_message_handler.go` 中 `needsConfirmFromEngine` 直接使用 `IsPhaseNeedsConfirm` 的返回值，没有额外查询引擎的产出物状态。gate 条件 `needsConfirmFromEngine && isSubstantivePhaseDocument(trimmedForGate)` 在首次执行时也会匹配。

3. **引擎已有 `hasOutput` 概念但未暴露给 GUI**: `HandleInput` 内部已使用 `_, hasOutput := ws.PhaseOutputs[ws.CurrentPhase]` 来区分首次执行和产出后确认（line 202），但这个状态没有通过公开方法暴露给 GUI 层的 agent loop。

4. **修复点明确且最小化**: 只需在引擎层新增一个 `HasPhaseOutput` 查询方法（~5 行），GUI 层在两处 gate 条件中增加 `hasOutput` 检查（每处 ~4 行）。不需要修改 `IsPhaseNeedsConfirm`、`HandleInput`、模板定义或任何关键词列表。

## Correctness Properties

Property 1: Bug Condition - First Execution Does Not Trigger NeedsConfirm Gate

_For any_ agent loop iteration where `needsConfirmFromEngine=true` AND `HasPhaseOutput(userID)=false` (first execution) AND the LLM output is substantive (`isSubstantivePhaseDocument=true`) AND not a stall reply, the NeedsConfirm gate SHALL NOT force-return — it SHALL allow the agent loop to continue generating the full phase document.

**Validates: Requirements 2.1, 2.4, 2.5**

Property 2: Preservation - Post-Output Confirmation Still Force-Returns

_For any_ agent loop iteration where `needsConfirmFromEngine=true` AND `HasPhaseOutput(userID)=true` (post-output) AND the LLM output is substantive AND not a stall reply, the NeedsConfirm gate SHALL force-return the response for user confirmation, preserving the existing post-output confirmation workflow.

**Validates: Requirements 3.1, 3.6**

Property 3: Preservation - NeedsConfirm=false Phases Unaffected

_For any_ agent loop iteration where `IsPhaseNeedsConfirm(userID)=false` (e.g., implementation phase), the NeedsConfirm gate SHALL NOT activate regardless of `HasPhaseOutput` state, preserving execution phase behavior.

**Validates: Requirements 3.2**

Property 4: HasPhaseOutput Correctness

_For any_ active workflow state, `HasPhaseOutput(userID)` SHALL return `true` if and only if `ws.PhaseOutputs[ws.CurrentPhase]` exists and is non-empty, and SHALL return `false` otherwise (including when no active workflow exists).

**Validates: Requirements 2.5, 1.5**

## Fix Implementation

### Changes Required

Assuming our root cause analysis is correct:

**File**: `corelib/workflow/engine.go`

**New Method**: `HasPhaseOutput(userID string) bool`

**Specific Changes**:

1. **Add `HasPhaseOutput` exported method** (~8 lines):
   ```go
   // HasPhaseOutput returns true if the user's active workflow has
   // output stored for the current phase. The agent loop uses this
   // to distinguish first execution (no output yet — let the loop
   // continue) from post-output confirmation (output exists — gate
   // should force-return).
   func (e *WorkflowEngine) HasPhaseOutput(userID string) bool {
       e.mu.RLock()
       defer e.mu.RUnlock()
       ws := e.workflows[userID]
       if ws == nil || ws.Status != WorkflowActive {
           return false
       }
       output, ok := ws.PhaseOutputs[ws.CurrentPhase]
       return ok && output != ""
   }
   ```

**File**: `gui/im_message_handler.go`

**Location 1**: No-tool branch NeedsConfirm gate (~line 4763)

2. **Add `hasOutput` check to engine-based NeedsConfirm gate**:
   - When `needsConfirmFromEngine=true`, additionally check `workflowEngine.HasPhaseOutput(userID)`
   - Only apply the gate (force-return) when `hasOutput=true`
   - When `hasOutput=false`, skip the engine-based gate entirely (let agent loop continue)
   - Log at info level when skipping: `"NeedsConfirm gate: first execution (hasOutput=false), allowing loop to continue"`

   Change from:
   ```go
   if needsConfirmFromEngine || needsConfirmFromSteering {
   ```
   To logic equivalent of:
   ```go
   engineGateActive := needsConfirmFromEngine
   if engineGateActive && h.app != nil && h.app.workflowEngine != nil {
       if !h.app.workflowEngine.HasPhaseOutput(userID) {
           engineGateActive = false
           log.Printf("[agent-loop] NeedsConfirm gate: first execution (hasOutput=false), allowing loop to continue (iter=%d)", iteration)
       }
   }
   if engineGateActive || needsConfirmFromSteering {
   ```

**Location 2**: Tool branch NeedsConfirm gate (~line 5412)

3. **Apply the same `hasOutput` check to the tool branch**:
   - In the `needsConfirmToolBranch` computation, when the engine path is taken (`IsPhaseNeedsConfirm=true`), additionally check `HasPhaseOutput(userID)`
   - Only set `needsConfirmToolBranch=true` when `hasOutput=true`

4. **No changes to**:
   - `isSubstantivePhaseDocument()` — existing function unchanged
   - `IsPhaseNeedsConfirm()` — existing method unchanged
   - `HandleInput()` — existing confirm/skip/modify logic unchanged
   - Any workflow template definitions
   - Any keyword lists or steering rules
   - `needsConfirmFromSteering` computation — steering path unchanged

## Testing Strategy

### Validation Approach

The testing strategy follows a two-phase approach: first, surface counterexamples that demonstrate the bug on unfixed code, then verify the fix works correctly and preserves existing behavior.

### Exploratory Bug Condition Checking

**Goal**: Surface counterexamples that demonstrate the bug BEFORE implementing the fix. Confirm or refute the root cause analysis. If we refute, we will need to re-hypothesize.

**Test Plan**: Write tests that simulate the NeedsConfirm gate evaluation with `needsConfirmFromEngine=true`, `hasOutput=false`, and substantive LLM output. Run these tests on the UNFIXED code to observe that the gate incorrectly force-returns.

**Test Cases**:
1. **PPT Workflow Preamble Test**: Simulate first execution of `audience_goal` phase with substantive plan overview (numbered list, ≥200 rune) → gate force-returns (will fail on unfixed code — gate should NOT force-return)
2. **Coding Workflow Preamble Test**: Simulate first execution of `requirements` phase with substantive Markdown document → gate force-returns (will fail on unfixed code)
3. **Any Template First Execution Test**: Simulate first execution of any NeedsConfirm=true phase across multiple templates → gate force-returns (will fail on unfixed code)
4. **Edge Case - Substantive But Short**: Simulate first execution with text containing `# Heading` but < 200 rune → gate force-returns (will fail on unfixed code)

**Expected Counterexamples**:
- All substantive LLM outputs during first execution trigger force-return
- Root cause confirmed: `needsConfirmFromEngine` does not check `hasOutput` state

### Fix Checking

**Goal**: Verify that for all inputs where the bug condition holds, the fixed function produces the expected behavior.

**Pseudocode:**
```
FOR ALL input WHERE isBugCondition(input) DO
  result := NeedsConfirmGate_fixed(input)
  ASSERT result.forceReturn = false
  ASSERT result.action = "continue"  // agent loop continues
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
- It generates many test cases automatically across the input domain (varying hasOutput states, phase types, text lengths, template types)
- It catches edge cases that manual unit tests might miss (e.g., empty PhaseOutputs map vs missing key vs empty string value)
- It provides strong guarantees that behavior is unchanged for all post-output confirmation inputs

**Test Plan**: Observe behavior on UNFIXED code first for post-output confirmation inputs, then write property-based tests capturing that behavior.

**Test Cases**:
1. **Post-Output Substantive Text Preservation**: Generate random substantive documents with `hasOutput=true` → verify gate still force-returns after fix
2. **NeedsConfirm=false Preservation**: Generate random inputs with `IsPhaseNeedsConfirm=false` → verify gate never activates
3. **Steering Path Preservation**: Generate random inputs with `needsConfirmFromSteering=true` (no engine workflow) → verify gate behavior unchanged
4. **Short Preamble Preservation**: Generate random short texts (< 200 rune, no structure) with any `hasOutput` state → verify `isSubstantivePhaseDocument=false` path unchanged

### Unit Tests

- Test `HasPhaseOutput` with no active workflow → returns false
- Test `HasPhaseOutput` with active workflow, no output for current phase → returns false
- Test `HasPhaseOutput` with active workflow, empty string output → returns false
- Test `HasPhaseOutput` with active workflow, non-empty output → returns true
- Test `HasPhaseOutput` with output for different phase (not current) → returns false
- Test gate integration: `needsConfirmFromEngine=true` + `hasOutput=false` + substantive text → loop continues
- Test gate integration: `needsConfirmFromEngine=true` + `hasOutput=true` + substantive text → force-return
- Test gate integration: `needsConfirmFromEngine=false` → gate not activated regardless of hasOutput

### Property-Based Tests

- Generate random workflow states (varying phase indices, output maps, template types) → `HasPhaseOutput` returns true iff `PhaseOutputs[CurrentPhase]` is non-empty
- Generate random gate inputs with `hasOutput=false` and substantive text → gate never force-returns on engine path
- Generate random gate inputs with `hasOutput=true` and substantive text → gate always force-returns on engine path
- Generate random gate inputs across all 19 template types → verify generic behavior (no template-specific logic)

### Integration Tests

- End-to-end: simulate workflow start → LLM iteration 1 outputs substantive preamble → verify loop continues (hasOutput=false) → LLM completes full document → SavePhaseOutput stores content → user sends "开始" → LLM outputs modified doc → verify force-return (hasOutput=true)
- Verify doc preview panel receives event only on post-output force-return, not during first execution bypass
- Verify all 19 workflow templates exhibit the same behavior (generic fix)
