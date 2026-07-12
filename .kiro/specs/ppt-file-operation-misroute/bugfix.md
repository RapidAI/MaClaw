# Bugfix: PPT 文件操作被误路由到 PPT 设计工作流

## 问题描述

用户在 AI 助手中输入"打开桌面上任何一个ppt文件并截图"，系统触发了 PPT 设计工作流（presentation_design），而不是直接执行文件操作。

## 根因分析

### 触发链路

1. `QuickFilter.Classify()` → `FilterNeedsUnderstanding`（非闲聊，路由到 LLM）
2. `IntentUnderstandingManager.Start()` → LLM 正确返回 `category="none"`（rejected）
3. `handleNeedsUnderstanding()` → `result.Rejected=true` → 调用 `tryKeywordWorkflowFallback(strongOnly=true)`
4. `MatchTemplateByStrongKeyword("打开桌面上任何一个ppt文件并截图")` → 找到 "PPT"（大写缩写）→ 匹配 `presentation_design`
5. **关键词 fallback 覆盖了 LLM 的正确拒绝，工作流被错误启动**

### 核心矛盾

`tryKeywordWorkflowFallback` 在 LLM **明确拒绝**时仍然尝试用关键词匹配覆盖 LLM 的判断。关键词匹配没有语义理解能力，无法区分：
- "打开PPT文件" → 文件操作（不是工作流）
- "设计一个PPT" → 创建任务（是工作流）

用关键词规则去修补（如加"打开"/"文件"等排除词）本质上是堆规则，不可扩展。每次遇到新的误匹配场景就得加词。

## 修复方案

### 核心原则：信任 LLM 的判断

LLM 有完整的语义理解能力，能区分"打开PPT文件"和"设计一个PPT"。当 LLM 明确返回 `category="none"` 时，应该信任它的判断，不用关键词去覆盖。

关键词 fallback 只应在 LLM 调用**失败**（超时/网络错误）时使用，作为降级方案。

### 修改 1: `gui/im_message_handler_workflow.go` — 移除 LLM 拒绝后的关键词覆盖

`handleNeedsUnderstanding()` 中：
- **修改前**：`result.Rejected` → 调用 `tryKeywordWorkflowFallback(strongOnly=true)` 尝试用关键词覆盖
- **修改后**：`result.Rejected` → 直接 `return nil`（信任 LLM，fall through 到正常 agent loop）
- LLM 调用**失败**时仍然使用关键词 fallback（降级方案不变）

### 修改 2: `corelib/workflow/intent_understanding.go` — 增强 LLM 系统 prompt

给 LLM 更好的判断依据，而不是在 LLM 之后加关键词规则：
- "不需要工作流"列表新增"文件操作"类别
- "易混淆示例"新增 5 个 PPT 文件操作示例 → `category="none"`
- 新增 PPT 判断口诀："打开/查看/转换/截图 PPT" → none；"设计/制作/生成 PPT" → presentation_design

### 修改 3: `corelib/workflow/registry.go` — 更新 MatchTemplateByStrongKeyword 注释

明确该函数只用于 LLM 调用失败的降级场景，不用于覆盖 LLM 的明确拒绝。

## 设计对比

| 方案 | 做法 | 问题 |
|------|------|------|
| 关键词排除 | 在 MatchTemplateByStrongKeyword 中加"打开"/"文件"等排除词 | 堆规则，不可扩展，每次新场景都要加词 |
| 信任 LLM | LLM 拒绝时不做关键词覆盖 | 语义理解，自动覆盖所有场景 |

## 验收标准

- "打开桌面上任何一个ppt文件并截图" → LLM 返回 none → 不触发工作流 → 直接执行
- "帮我设计一个产品介绍PPT" → LLM 返回 presentation_design → 正常触发工作流
- LLM 调用超时时 → 关键词 fallback 仍然工作（降级方案不受影响）
- 所有 workflow 包测试通过
- 所有 GUI coding gate 测试通过
