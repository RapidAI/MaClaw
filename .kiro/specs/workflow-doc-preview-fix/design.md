# Workflow Doc Preview Fix — Bugfix Design

## Overview

MaClaw 的编程工作流有两条执行路径：工作流引擎路径和 steering 驱动路径。三个 UI 功能（全屏建议横幅、右侧文档预览面板、聊天区可点击文件链接）的触发逻辑全部绑定在工作流引擎路径上。当编程任务走 steering 驱动路径时，这些 UI 功能完全不工作。

修复策略：在 agent loop 中添加轻量级检测机制，通过模式匹配识别 steering 驱动的编程工作流，直接调用 `GUIWorkflowAdapter` 的事件发射方法（绕过 `WorkflowEngine` 的状态管理），使前端 UI 在两条路径下都能正常工作。

## Glossary

- **Bug_Condition (C)**: 编程任务通过 steering 驱动路径处理时，前端 UI 功能（全屏建议横幅、文档预览、文件链接）不触发
- **Property (P)**: steering 驱动路径下，系统应发射与工作流引擎路径相同的前端事件（`workflow:suggest_maximize`、`workflow:doc_update`），使 UI 正常工作
- **Preservation**: 工作流引擎路径的所有现有行为不变；非编程任务不触发工作流 UI；后台消息不触发文档预览
- **GUIWorkflowAdapter**: `gui/workflow_adapter.go` 中的适配器，负责通过 Wails `runtime.EventsEmit` 向前端发射工作流事件
- **Steering 驱动路径**: LLM 在正常 `runAgentLoop` 中按 `coding-workflow.md` 规则执行三阶段流程（需求文档→技术设计→任务拆分）
- **工作流引擎路径**: 通过 `handleWorkflowInterception` → `IntentUnderstandingManager` → `WorkflowEngine.StartWorkflow` 启动的完整工作流管理路径
- **SteeringWorkflowDetector**: 新增的检测器，在 agent loop 中识别 steering 驱动的编程工作流并发射前端事件

## Bug Details

### Bug Condition

当编程任务通过 steering 驱动路径处理时，`WorkflowEngine` 没有为该用户启动 workflow，导致三个 UI 功能的触发条件全部不满足：
1. `EmitSuggestMaximize` 仅在 `handleActiveUnderstanding` → `StartWorkflow` 后调用
2. `SavePhaseOutput` 检查 `e.workflows[userID]` 为 nil，返回空字符串，`EmitDocUpdate` 不触发
3. 聊天区的文件链接依赖 `LocalFilePath`/`LocalFilePaths`，但 `write_file` 工具结果不自动填充这些字段

**Formal Specification:**
```
FUNCTION isBugCondition(input)
  INPUT: input of type {userMessage: string, executionPath: string, toolCalls: []ToolCall}
  OUTPUT: boolean

  isCodingTask := matchesCodingKeywords(input.userMessage)
  isSteeringPath := input.executionPath == "steering_driven"
                    AND NOT workflowEngineHasActiveWorkflow(input.userID)
  hasWorkflowDocOutput := ANY tc IN input.toolCalls WHERE
    (tc.name == "write_file" AND isWorkflowDocPattern(tc.args.path))
    OR (tc.name == "generate_pdf" AND isWorkflowDocContent(tc.args.content))

  RETURN isCodingTask AND isSteeringPath AND hasWorkflowDocOutput
END FUNCTION
```

### Examples

- 用户发送"开发一个贪吃蛇游戏"，`handleWorkflowInterception` 返回 nil（未拦截），LLM 按 steering 规则生成需求文档 → 前端不显示全屏建议横幅，不显示右侧文档预览
- LLM 调用 `write_file(path="需求文档_贪吃蛇.md", content="# 需求文档\n...")` → `doc_update` 事件不发射，右侧面板空白
- LLM 调用 `generate_pdf` 生成需求文档 PDF → 文件通过 `pendingFiles` 机制发送，但 `LocalFilePaths` 中只有 PDF 路径，Markdown 源文件不在其中
- 用户发送"翻译这个文件"（非编程任务）→ 不应触发任何工作流 UI（这是正确行为，不是 bug）

## Expected Behavior

### Preservation Requirements

**Unchanged Behaviors:**
- 工作流引擎路径（`handleWorkflowInterception` 拦截成功）的所有功能继续正常工作：全屏建议横幅、右侧文档预览、阶段切换、质量门禁
- 非编程任务消息（翻译、整理、闲聊等）不触发全屏建议横幅和文档预览面板
- `SavePhaseOutput` 和 `EmitGateResult` 在工作流引擎路径中的行为不变
- 前端 `userClosedRef` 机制继续生效——用户手动关闭文档预览后，新的 `doc_update` 事件不会重新打开面板
- 后台消息（`msg.IsBackground == true`）不触发文档预览更新

**Scope:**
所有不涉及 steering 驱动编程工作流的输入应完全不受此修复影响。这包括：
- 工作流引擎路径处理的编程任务
- 非编程任务（翻译、资料收集、内容整理等）
- 后台任务消息
- 闲聊和简短对话

## Hypothesized Root Cause

Based on the bug description, the most likely issues are:

1. **事件发射路径耦合**: `EmitSuggestMaximize` 仅在 `handleActiveUnderstanding` → `StartWorkflow` 路径中被调用，没有独立于 `WorkflowEngine` 状态的发射入口。当 steering 路径处理编程任务时，没有任何代码路径会调用此方法。

2. **SavePhaseOutput 的 nil 守卫**: `SavePhaseOutput` 在 `corelib/workflow/engine.go` 中检查 `e.workflows[userID]`，steering 路径下该 map 为空，直接返回空字符串。`EmitDocUpdate` 的调用依赖 `SavePhaseOutput` 返回非空 phaseID，形成了死锁依赖。

3. **缺少 steering 路径的工作流检测**: agent loop 中没有任何机制检测 LLM 是否正在按 steering 规则执行编程工作流。工具调用（`write_file`、`generate_pdf`）的结果被处理后直接返回，没有检查是否属于工作流文档。

4. **文件链接缺失**: `write_file` 工具的结果是纯文本确认消息，不像 `generate_pdf`/`send_file` 那样返回 `[file_base64|...]` 格式的数据，因此不会被 `pendingFiles` 机制捕获并填充到 `LocalFilePaths`。

## Correctness Properties

Property 1: Bug Condition — Steering 驱动路径的工作流事件发射

_For any_ coding task message that bypasses workflow engine interception (isBugCondition returns true) and the LLM generates workflow phase documents via tool calls, the fixed system SHALL emit `workflow:suggest_maximize` event once when the coding workflow is first detected, and SHALL emit `workflow:doc_update` event with the correct phase ID and document content for each phase document generated.

**Validates: Requirements 2.1, 2.2**

Property 2: Preservation — 非编程任务和工作流引擎路径不受影响

_For any_ input where the bug condition does NOT hold (non-coding tasks, workflow engine path tasks, background messages), the fixed code SHALL produce exactly the same behavior as the original code, preserving all existing functionality: no spurious `workflow:suggest_maximize` events for non-coding tasks, no spurious `workflow:doc_update` events, and all workflow engine path features (phase updates, quality gates, doc updates) continue working unchanged.

**Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5**

## Fix Implementation

### Changes Required

Assuming our root cause analysis is correct:

**File**: `gui/im_message_handler.go`

**New Type**: `SteeringWorkflowDetector`

**Specific Changes**:

1. **新增 SteeringWorkflowDetector 结构体**: 轻量级检测器，跟踪 steering 驱动路径下的编程工作流状态
   - `detected bool` — 是否已检测到编程工作流
   - `suggestMaximizeEmitted bool` — 是否已发射 suggest_maximize 事件
   - `phaseDocuments map[string]string` — 已检测到的阶段文档（phaseID → content）
   - `userID string` — 当前用户 ID

2. **编程任务检测**: 在 `runAgentLoop` 的首次迭代前，检查用户消息是否匹配编程任务关键词（复用 steering 规则中的关键词列表：开发、编写、实现、创建、修改代码、重构、修 bug、设计架构、添加功能、新增功能）。仅当 `workflowEngine` 没有为该用户启动 active workflow 时才激活检测器。

3. **suggest_maximize 事件发射**: 当检测器激活且 LLM 首次产生工具调用（表明开始执行工作流）时，通过 `GUIWorkflowAdapter.EmitSuggestMaximize` 发射事件。仅发射一次（`suggestMaximizeEmitted` 守卫）。

4. **工具调用拦截 — 文档阶段检测**: 在 agent loop 的工具执行后，检查 `write_file` 和 `generate_pdf` 的调用参数：
   - 文件名匹配模式：`需求文档`/`需求分析`/`requirements` → phaseID = `"requirements"`
   - 文件名匹配模式：`技术设计`/`设计文档`/`design`/`架构设计` → phaseID = `"design"`
   - 文件名匹配模式：`任务拆分`/`任务列表`/`tasks`/`task_breakdown` → phaseID = `"tasks"`
   - 检测到匹配时，读取文件内容（对于 `write_file`，从工具参数中提取 content；对于 `generate_pdf`，从 markdown_content 参数提取），通过 `GUIWorkflowAdapter.EmitDocUpdate` 发射事件。

5. **直接事件发射方法**: 在 `GUIWorkflowAdapter` 上新增 `EmitDocUpdateDirect(userID, phaseID, content string)` 方法（或直接复用现有 `EmitDocUpdate`），绕过 `WorkflowEngine` 的状态检查，直接发射 Wails 事件。由于现有 `EmitDocUpdate` 已经是直接发射（不检查 engine 状态），可以直接复用。

6. **!msg.IsBackground 守卫**: 所有检测逻辑仅在 `!msg.IsBackground` 时激活，保持与现有行为一致。

**File**: `gui/workflow_adapter.go`

无需修改。现有的 `EmitDocUpdate` 和 `EmitSuggestMaximize` 方法已经是直接发射事件，不依赖 `WorkflowEngine` 状态。只需在 agent loop 中直接获取 adapter 实例并调用即可。

**File**: `gui/frontend/src/components/ai/useWorkflowState.ts`

无需修改。现有的事件监听器已经能处理来自任何来源的 `workflow:doc_update` 和 `workflow:suggest_maximize` 事件。`userClosedRef` 机制也会自动生效。

**File**: `gui/frontend/src/components/ai/AIAssistantPanel.tsx`

无需修改。现有的 `WorkflowDocPreview` 组件和 split-pane 布局已经由 `useWorkflowState` 驱动，只要事件正确发射，UI 就会正常渲染。

## Testing Strategy

### Validation Approach

The testing strategy follows a two-phase approach: first, surface counterexamples that demonstrate the bug on unfixed code, then verify the fix works correctly and preserves existing behavior.

### Exploratory Bug Condition Checking

**Goal**: Surface counterexamples that demonstrate the bug BEFORE implementing the fix. Confirm or refute the root cause analysis. If we refute, we will need to re-hypothesize.

**Test Plan**: Write tests that simulate coding task messages going through the steering-driven agent loop path, verify that `EmitSuggestMaximize` and `EmitDocUpdate` are NOT called (confirming the bug exists). Run these tests on the UNFIXED code to observe failures and understand the root cause.

**Test Cases**:
1. **Coding Task Without Engine Workflow**: Simulate a message "开发一个贪吃蛇游戏" where `handleWorkflowInterception` returns nil, verify `EmitSuggestMaximize` is never called (will fail on unfixed code — confirms bug)
2. **Write File With Workflow Doc Pattern**: Simulate `write_file(path="需求文档_贪吃蛇.md")` tool call in agent loop, verify `EmitDocUpdate` is never called (will fail on unfixed code — confirms bug)
3. **Generate PDF With Workflow Content**: Simulate `generate_pdf` tool call with requirements document content, verify `EmitDocUpdate` is never called (will fail on unfixed code — confirms bug)
4. **Non-Coding Task Baseline**: Simulate "翻译这个文件" message, verify no workflow events are emitted (should pass on unfixed code — baseline)

**Expected Counterexamples**:
- `EmitSuggestMaximize` is never called when coding tasks go through steering path
- `EmitDocUpdate` is never called because `SavePhaseOutput` returns "" (no active workflow)
- Possible causes: event emission tightly coupled to `WorkflowEngine` state, no detection mechanism in agent loop

### Fix Checking

**Goal**: Verify that for all inputs where the bug condition holds, the fixed function produces the expected behavior.

**Pseudocode:**
```
FOR ALL input WHERE isBugCondition(input) DO
  result := runAgentLoop_fixed(input)
  ASSERT suggestMaximizeEmitted(input.userID) == true
  ASSERT docUpdateEmitted(input.userID, expectedPhaseID) == true
  ASSERT docUpdateContent matches expectedContent
END FOR
```

### Preservation Checking

**Goal**: Verify that for all inputs where the bug condition does NOT hold, the fixed function produces the same result as the original function.

**Pseudocode:**
```
FOR ALL input WHERE NOT isBugCondition(input) DO
  ASSERT runAgentLoop_original(input).events = runAgentLoop_fixed(input).events
END FOR
```

**Testing Approach**: Property-based testing is recommended for preservation checking because:
- It generates many test cases automatically across the input domain (random non-coding messages, random background flags)
- It catches edge cases that manual unit tests might miss (e.g., messages containing coding keywords but classified as non-coding)
- It provides strong guarantees that behavior is unchanged for all non-buggy inputs

**Test Plan**: Observe behavior on UNFIXED code first for non-coding tasks and workflow engine path tasks, then write property-based tests capturing that behavior.

**Test Cases**:
1. **Workflow Engine Path Preservation**: Verify that when `handleWorkflowInterception` returns a response, all existing events (`phase_update`, `doc_update`, `suggest_maximize`, `gate_result`) continue to be emitted correctly
2. **Non-Coding Task Preservation**: Verify that non-coding task messages (translations, summaries, file operations) do not trigger any workflow events after the fix
3. **Background Message Preservation**: Verify that `msg.IsBackground == true` messages never trigger workflow events
4. **User Close Preference Preservation**: Verify that `userClosedRef` mechanism in `useWorkflowState.ts` continues to prevent auto-opening of doc preview after user manually closes it

### Unit Tests

- Test `SteeringWorkflowDetector.isCodingTask()` with various coding and non-coding messages
- Test phase ID detection from file names (`需求文档_xxx.md` → `requirements`, `设计文档_xxx.md` → `design`, `任务列表_xxx.md` → `tasks`)
- Test that `suggestMaximizeEmitted` flag prevents duplicate emissions
- Test that detector is not activated when `workflowEngine` has an active workflow for the user

### Property-Based Tests

- Generate random user messages and verify: coding task messages trigger detector activation, non-coding messages do not
- Generate random file names and verify: workflow doc patterns are correctly classified to phase IDs, non-workflow files are ignored
- Generate random combinations of `IsBackground` flag and message content, verify: background messages never trigger events regardless of content

### Integration Tests

- End-to-end test: coding task message → steering path → write_file with requirements doc → verify `suggest_maximize` and `doc_update` events emitted
- End-to-end test: same coding task through workflow engine path → verify all existing events still emitted
- End-to-end test: non-coding task → verify zero workflow events emitted
