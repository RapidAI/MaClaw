# 消息响应延迟优化 Phase 2

## 来源

日志分析 `~/.maclaw/logs/maclaw.log`（2026-05-13），用户输入"北京天气"后 34 秒才看到第一个 token。

## 精确时间线

| 时间 | 阶段 | 耗时 | 根因 |
|------|------|------|------|
| 08:26:47 | 消息接收 + UIC 短消息跳过 | 0ms | ✅ 已优化 |
| 08:26:47-08:26:49 | UIC fusion（handleNeedsUnderstanding 路径） | 2s | tree channel 超时 1.5s |
| 08:26:49-08:26:51 | Task-Context LLM（deepseek-reasoner） | 2s | reasoning 模型做简单分类 |
| 08:26:51-08:27:06 | buildIMEntrySystemPrompt（frozen_snapshot 生成） | 15s | 被后台 LLM 调用阻塞 |
| 08:27:06-08:27:20 | proactive_recall | 14s | embedding API 或 rate limit 阻塞 |
| 08:27:20-08:27:21 | knowledge_auto_recall + agent loop 启动 | 1s | ✅ 正常 |
| 08:27:21-08:27:24 | 第一次 LLM 调用（主 agent loop） | 3s | deepseek-reasoner 正常延迟 |

## 已实施的四个优化方案

### 方案 1: 短消息快速路径——跳过所有工作流拦截 ✅

**根因**：`shouldBypassWorkflowForIntent` 对短消息（<10 runes）跳过了 UIC fusion，但 `handleNeedsUnderstanding` 中又独立调用了一次完整的 UIC fusion（1.58s）。两次调用不共享短消息判断。

**修复**：`handleWorkflowInterception` 在调用 `filter.Classify` 之前，对短消息（<10 runes）直接 return nil。仍保留对活跃工作流/理解会话的响应（"确认"、"继续"等短消息可能是工作流回复）。

**修改文件**：`gui/im_message_handler_workflow.go`

**预期收益**：-2s

### 方案 2: 分类任务使用轻量模型 ✅

**根因**：Task-Context LLM 通过 `LLMClassify` → `getMaclawLLMConfig()` 使用主模型（deepseek-reasoner）。Reasoning 模型对"continue/new"二分类任务输出完整推理链，浪费 2s。

**修复**：
- `LLMClassifyRequest` 新增 `PreferLightweight bool` 字段
- `LLMClassify` 当 `PreferLightweight=true` 时，调用 `getLightweightLLMConfig()`
- `getLightweightLLMConfig()` 将 reasoning 模型映射到同 provider 的 chat 模型：
  - `deepseek-reasoner` / `deepseek-r1` → `deepseek-chat`
  - `o1-*` / `o3-*` → `gpt-4o-mini`
  - `qwen3` → `qwen-turbo`
- Task-Context adapter 设置 `PreferLightweight: true`

**修改文件**：`gui/llm_lightweight.go`, `gui/im_app_accessors.go`, `gui/im_task_context_adapter.go`

**预期收益**：-1.5s

### 方案 3: 详细计时日志定位 proactive_recall 瓶颈 ✅

**根因**：proactive_recall 到 agent loop 之间有 29s 的 gap，但缺少子步骤计时日志无法精确定位。

**修复**：
- `executePreparedIMEntry` 添加每个步骤的计时日志（gates、history_load、system_prompt、loop_ctx）
- `appendGUIEpilogue` 添加 memory section 和 knowledge recall 的分别计时
- `appendProactiveRecall` 添加 RecallDynamic 调用的精确耗时

**修改文件**：`gui/im_entry_execution.go`, `gui/im_system_prompt_gui_sections.go`, `gui/im_system_prompt.go`

**预期收益**：下次运行后可精确定位 29s gap 的根因，指导进一步优化

### 方案 4: 工具名进度显示 + 后台隔离基础 ✅

**修复 A — 工具名进度显示**：
- `userFacingToolProgressText` 重写：每个工具有专属进度文本（含 emoji + 工具名）
- `isVisibleAIAssistantProgressText` 更新：允许 emoji 前缀的工具进度通过（移除 "执行工具" 的 block）
- 前端 `AIAssistantPanel.tsx`：`activeProcessingText` 优先使用最新的工具进度事件文本
- 前端 `useAIAssistant.ts`：清理 `HIDDEN_PROGRESS_PATTERNS`，不再隐藏工具进度消息

**效果**：用户看到 "🚀 正在执行 Skill...（可继续输入）" 而非 "正在执行工具...（可继续输入）"

**修改文件**：`gui/im_tool_progress.go`, `gui/app_wails_bindings.go`, `gui/frontend/src/components/ai/AIAssistantPanel.tsx`, `gui/frontend/src/components/ai/useAIAssistant.ts`

## 预期总体效果

| 场景 | 优化前 | 优化后 |
|------|--------|--------|
| 短消息（"北京天气"）first token | 34s | ~15-20s（方案1省2s + 方案2省1.5s + 日志定位后续优化） |
| 工具执行期间用户感知 | "正在执行工具..." | "🚀 正在执行 Skill..." / "🔍 正在搜索网络..." |

## 后续优化方向（基于方案 3 的日志数据）

1. 如果 `RecallDynamic` 本身 <100ms 但 `appendGUIEpilogue` 总耗时 >10s → 问题在 embedding API 调用或后台 goroutine 阻塞
2. 如果 `system_prompt` 构建 >5s → 需要将 proactive_recall 改为异步（设 2s 超时，超时后跳过）
3. 如果 `history_load` >1s → 需要优化 ConversationMemory 的磁盘读取
