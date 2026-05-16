# Workflow Repeat Content Loss Bugfix Design

## Overview

Agent loop 多轮迭代时，`IMAgentResponse.Text` 只包含最后一轮的 `msgContent`，前端 `resolveFinalRoundContent()` 的 `endsWith` 单一策略在最后一轮文本非精确后缀时失败，导致完整的流式累积内容被短文本覆盖。

修复方案分两层：
1. **后端**：在 `IMAgentResponse` 中新增 `ResponseSource` 字段，标识 `text` 的来源路径
2. **前端**：`resolveFinalRoundContent()` 从单一 `endsWith` 升级为三层判断策略（来源标记 → 长度比较 → endsWith 兜底）

## Glossary

- **Bug_Condition (C)**: `streamedContent` 非空且长度显著大于 `finalText`，但 `!streamedContent.endsWith(finalText)`，且 `response_source` 为 `agent_loop` 或未设置——此时当前代码错误地用 `finalText` 替换 `streamedContent`
- **Property (P)**: 当 C 成立时，`resolveFinalRoundContent` 应返回 `streamedContent`（保留完整的多轮累积输出）
- **Preservation**: 特殊路径（`ask_user`/`cancel`/`file_delivery`/`screenshot`）、非流式响应、`endsWith` 匹配场景的行为不变
- **`resolveFinalRoundContent`**: `gui/frontend/src/components/ai/useAIAssistant.ts` 中的函数，决定 assistant 消息最终显示内容
- **`IMAgentResponse`**: `gui/im_message_handler.go` 中的结构体，后端 agent loop 返回给前端的响应
- **`ResponseSource`**: 新增字段，标识 `IMAgentResponse.Text` 的来源路径

## Bug Details

### Bug Condition

当 agent loop 多轮迭代后，最后一轮的 `response.text` 与前端累积的 `streamedContent` 不是后缀关系时，`endsWith` 检查失败，代码走到 `if (finalText) return finalText` 分支，用短文本替换了完整输出。

**Formal Specification:**
```
FUNCTION isBugCondition(input)
  INPUT: input of type { streamedContent: string, finalText: string, responseSource: string | undefined }
  OUTPUT: boolean

  RETURN streamedContent IS NOT EMPTY
         AND finalText IS NOT EMPTY
         AND streamedContent.length >= finalText.length * 2
         AND NOT streamedContent.endsWith(finalText)
         AND (responseSource IS UNDEFINED OR responseSource == 'agent_loop')
END FUNCTION
```

### Examples

- **需求文档被覆盖**：`streamedContent` = 3000 字的需求文档 + 确认提示 A，`finalText` = 200 字的确认提示 B（措辞不同），`endsWith` 失败 → 当前代码返回 200 字的确认提示 B，3000 字文档消失
- **工具调用后重复输出**：LLM 输出设计文档后调用 `generate_pdf`，后续迭代输出"已生成 PDF"，`finalText` = "已生成 PDF"，不是 `streamedContent` 的后缀 → 设计文档被替换
- **尾部空白差异**：`streamedContent` 尾部有 `\n\n`，`finalText` 尾部只有 `\n`，`endsWith` 失败 → 内容丢失
- **正常场景（不触发 bug）**：`streamedContent` = "你好，我是 maclaw"，`finalText` = "你好，我是 maclaw"，`endsWith` 成功 → 保留 `streamedContent`

## Expected Behavior

### Preservation Requirements

**Unchanged Behaviors:**
- `ask_user` 工具的结构化提问必须继续使用 `response.text`（文本与流式输出语义无关）
- `cancel` 操作的取消消息必须继续使用 `response.text`
- `file_delivery`/`screenshot` 路径的文件/截图消息必须继续使用 `response.text`
- 非流式响应（`streamedContent` 为空）必须继续使用 `response.text`
- `endsWith` 匹配成功时（改进记录 #19 的原始场景）必须继续保留 `streamedContent`
- `response_source` 缺失时降级到长度比较 + `endsWith`，不崩溃

**Scope:**
所有 `response_source` 为特殊处理路径的输入，以及 `streamedContent` 为空的输入，完全不受此修复影响。

## Hypothesized Root Cause

Based on the bug description, the most likely issues are:

1. **前端判断策略单一**：`resolveFinalRoundContent()` 仅依赖 `endsWith` 一种策略判断是否保留 `streamedContent`。当最后一轮输出与累积内容不是精确后缀关系时（措辞变化、格式差异、重复输出），策略失败，走到 `if (finalText) return finalText` 分支
   - 代码位置：`gui/frontend/src/components/ai/useAIAssistant.ts` 第 957-993 行

2. **后端缺少来源标记**：`IMAgentResponse` 没有字段区分 `Text` 是来自普通 agent loop 最后一轮还是特殊处理路径（ask_user/cancel/file_delivery/screenshot）。前端无法精确判断 `finalText` 的语义
   - 代码位置：`gui/im_message_handler.go` 第 65-120 行（`IMAgentResponse` 结构体）

3. **Agent loop 多轮迭代的 Text 只保留最后一轮**：`IMAgentResponse.Text` 赋值为 `stripThinkingTags(msgContent)`，其中 `msgContent` 是最后一轮 `choice.Message.Content`，不包含前面轮次的输出
   - 代码位置：`gui/im_message_handler.go` 第 4391、4410、4495、4510 行的各个 `&IMAgentResponse{Text: ...}` 构造

## Correctness Properties

Property 1: Bug Condition - Agent Loop Content Preserved When Streamed Content Is Significantly Longer

_For any_ input where `streamedContent` is non-empty, `finalText` is non-empty, `streamedContent.length >= finalText.length * 2`, `!streamedContent.endsWith(finalText)`, and `response_source` is `'agent_loop'` or undefined, the fixed `resolveFinalRoundContent` function SHALL return `streamedContent`, preserving the complete multi-round accumulated output.

**Validates: Requirements 2.2, 2.3, 2.5**

Property 2: Preservation - Special Source Paths Always Use Response Text

_For any_ input where `response_source` is one of `'ask_user'`, `'cancel'`, `'file_delivery'`, `'screenshot'`, the fixed `resolveFinalRoundContent` function SHALL return `finalText` (i.e., `response.text`), regardless of `streamedContent` length, preserving all existing special-path behavior.

**Validates: Requirements 3.1, 3.2, 3.3**

Property 3: Preservation - Non-Streaming Responses Use Response Text

_For any_ input where `streamedContent` is empty and `finalText` is non-empty, the fixed `resolveFinalRoundContent` function SHALL return `finalText`, preserving existing non-streaming behavior.

**Validates: Requirements 3.4, 3.6**

Property 4: Preservation - EndsWith Fallback Still Works

_For any_ input where `streamedContent` is non-empty, `finalText` is non-empty, `streamedContent.length > finalText.length`, and `streamedContent.endsWith(finalText)`, the fixed `resolveFinalRoundContent` function SHALL return `streamedContent`, preserving the original improvement #19 behavior.

**Validates: Requirements 3.5**

Property 5: Preservation - Missing ResponseSource Degrades Gracefully

_For any_ input where `response_source` is undefined or empty, the fixed `resolveFinalRoundContent` function SHALL produce the same result as applying the length-comparison + endsWith degraded strategy, without throwing errors or behaving abnormally.

**Validates: Requirements 3.7**

## Fix Implementation

### Changes Required

Assuming our root cause analysis is correct:

**File**: `gui/im_message_handler.go`

**Struct**: `IMAgentResponse`

**Specific Changes**:
1. **新增 `ResponseSource` 字段**：在 `IMAgentResponse` 结构体中添加 `ResponseSource string \`json:"response_source,omitempty"\``
2. **Agent loop 正常结束路径**：4 处 `&IMAgentResponse{Text: stripThinkingTags(msgContent)}` 构造处（NeedsConfirm gate、hard cap、normal finalize、capability gap），设置 `ResponseSource: "agent_loop"`
3. **ask_user 路径**：`ParseAskUserResult` 成功后构造的 `resp`，设置 `ResponseSource: "ask_user"`
4. **cancel 路径**：所有 `cancelMsg` 返回处（约 5 处），设置 `ResponseSource: "cancel"`
5. **screenshot 路径**：`screenshotAlreadySent` 和 `saveScreenshotToFile` 返回处，设置 `ResponseSource: "screenshot"`
6. **file delivery 路径**：`pendingFiles` 处理后的返回处，设置 `ResponseSource: "file_delivery"`
7. **empty fallback 路径**：最大轮次 `(已达到最大推理轮次...)` 返回处，设置 `ResponseSource: "empty_fallback"`
8. **其他特殊路径**：`Deferred` 响应（编程会话运行中）、对话重置、短闲聊等，设置对应的 `ResponseSource`

**File**: `gui/frontend/src/components/ai/useAIAssistant.ts`

**Function**: `resolveFinalRoundContent`

**Specific Changes**:
1. **提取 `response_source`**：从 `response` 对象中读取 `response_source` 字段
2. **第一层：特殊来源检查**：若 `response_source` 为 `ask_user`/`cancel`/`file_delivery`/`screenshot`，直接返回 `finalText`（无论 `streamedContent` 长度）
3. **第二层：长度比较**：若 `streamedContent` 非空且 `streamedContent.length >= finalText.length * 2`，且 `response_source` 为 `agent_loop` 或未设置，返回 `streamedContent`
4. **第三层：endsWith 兜底**：保留现有 `endsWith` 逻辑作为最后一层判断
5. **后续 fallback 不变**：`hasVisibleTerminalPayload`、`isFailedTerminalTraceStatus`、空内容兜底逻辑保持不变

## Testing Strategy

### Validation Approach

The testing strategy follows a two-phase approach: first, surface counterexamples that demonstrate the bug on unfixed code, then verify the fix works correctly and preserves existing behavior.

### Exploratory Bug Condition Checking

**Goal**: Surface counterexamples that demonstrate the bug BEFORE implementing the fix. Confirm or refute the root cause analysis. If we refute, we will need to re-hypothesize.

**Test Plan**: Write tests that call `resolveFinalRoundContent` with inputs matching the bug condition (long streamedContent, short non-suffix finalText, no response_source). Run these tests on the UNFIXED code to observe that `finalText` is incorrectly returned.

**Test Cases**:
1. **Long Document + Short Non-Suffix Confirmation**: `streamedContent` = 3000 chars document, `finalText` = 200 chars different confirmation text (will fail on unfixed code — returns finalText instead of streamedContent)
2. **Repeated Output with Wording Variation**: `streamedContent` ends with "请确认需求文档", `finalText` = "请查看并确认上述需求" (will fail on unfixed code)
3. **Trailing Whitespace Difference**: `streamedContent` ends with `\n\n`, `finalText` is same text but ends with `\n` (will fail on unfixed code)

**Expected Counterexamples**:
- `resolveFinalRoundContent` returns `finalText` (short text) instead of `streamedContent` (long document)
- Root cause confirmed: `endsWith` fails → falls through to `if (finalText) return finalText`

### Fix Checking

**Goal**: Verify that for all inputs where the bug condition holds, the fixed function produces the expected behavior.

**Pseudocode:**
```
FOR ALL input WHERE isBugCondition(input) DO
  result := resolveFinalRoundContent_fixed(message, response)
  ASSERT result == streamedContent
END FOR
```

### Preservation Checking

**Goal**: Verify that for all inputs where the bug condition does NOT hold, the fixed function produces the same result as the original function.

**Pseudocode:**
```
FOR ALL input WHERE NOT isBugCondition(input) DO
  ASSERT resolveFinalRoundContent_original(input) == resolveFinalRoundContent_fixed(input)
END FOR
```

**Testing Approach**: Property-based testing is recommended for preservation checking because:
- It generates many test cases automatically across the input domain
- It catches edge cases that manual unit tests might miss (e.g., boundary lengths, empty strings, special characters)
- It provides strong guarantees that behavior is unchanged for all non-buggy inputs

**Test Plan**: Observe behavior on UNFIXED code first for special-path responses, non-streaming responses, and endsWith-matching responses, then write property-based tests capturing that behavior.

**Test Cases**:
1. **Special Source Preservation**: Verify that `ask_user`/`cancel`/`file_delivery`/`screenshot` responses always use `response.text` regardless of streamedContent
2. **Non-Streaming Preservation**: Verify that empty `streamedContent` + non-empty `finalText` always returns `finalText`
3. **EndsWith Preservation**: Verify that when `streamedContent.endsWith(finalText)` and `streamedContent` is longer, `streamedContent` is returned
4. **Missing ResponseSource Preservation**: Verify that undefined `response_source` degrades to length + endsWith strategy without errors

### Unit Tests

- Test `resolveFinalRoundContent` with each `response_source` value and various streamedContent/finalText combinations
- Test `IMAgentResponse.ResponseSource` is correctly set at each agent loop return path (Go unit tests)
- Test backward compatibility when `response_source` is missing from response JSON

### Property-Based Tests

- Generate random `streamedContent`/`finalText` pairs with `response_source = 'agent_loop'` and verify bug condition inputs always preserve `streamedContent`
- Generate random special-source responses and verify `finalText` is always returned
- Generate random inputs without `response_source` and verify degraded strategy matches expected behavior
- Generate random endsWith-matching pairs and verify `streamedContent` is preserved

### Integration Tests

- Test full send → stream → finalize flow with multi-round agent loop producing long document + short final text
- Test ask_user interruption during streaming preserves ask_user text
- Test cancel during streaming preserves cancel message
- Test file delivery after streaming preserves file path message
