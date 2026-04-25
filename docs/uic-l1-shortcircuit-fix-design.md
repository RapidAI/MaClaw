# UIC L1 短路修复设计：统一分类管道，消除双分类器架构

## 问题

### 触发场景

用户说"基于给出的文档，生成iWorker系统的主打宣传ppt"，系统没有进入 `presentation_design` 工作流，直接用 python-pptx 生成 PPT。

### 日志证据

```
2026/04/25 05:54:07 [WorkflowInterception] UIC pre-check rejected workflow for user desktop-user:
intent=non_coding conf=0.92 layer=1 threshold=0.70
— fast-rejecting before IUM LLM call,
text="基于给出的文档，生成iWorker系统的主打宣传ppt..."
```

### 根因链路

1. `handleNeedsUnderstanding` → UIC 预检 → `uic.Classify()`
2. L1 `classifyByKeywords`：`"宣传ppt"` 匹配 `LabelNonCoding` Strong（优先级 3）> `"ppt"` 匹配 `LabelOffice`（优先级 10）
3. L1 `confident=true`（conf=0.92）→ **`Classify()` 直接返回，L2+L3 fusion 被跳过**
4. 预检：`!IsWorkflowCandidate("non_coding")` → reject → IUM 被跳过

---

## 机制性分析

### 表层问题：L1 短路 fusion

`Classify()` 中 `if l1Confident { return }` 让关键词层拥有一票否决 fusion 管道的权力。Phase 2 实现的 L2+L3 并行 fusion 在 L1 Strong 命中时完全不起作用。

### 深层问题：双分类器架构

但仅仅去掉 L1 短路是不够的。即使 fusion 正确返回 `office`，预检放行到 IUM，IUM 还要再做一次 LLM 调用（10-30s）来判断工作流类型。这是因为系统中存在**两个独立的分类器做重叠的事情**：

| 分类器 | 分类维度 | 延迟 | 决策权 |
|--------|---------|------|--------|
| UIC | 12 个 IntentLabel（ssh/browser/coding/non_coding/office/...） | L1 <1ms, fusion 2-8s | 工具激活 + 工作流预检门控 |
| IUM | 19 个 WorkflowType + "none" | 10-30s | 工作流启动 |

UIC 的 12 个 label 和 IUM 的 19+1 个 category 是两套不同的分类体系，但它们的判断对象是同一条用户消息。UIC 预检的存在本身就是 workaround——因为 IUM 太慢，所以加了一个快速的 UIC 在前面做门控。但 UIC 的分类粒度和 IUM 的分类粒度不匹配，导致 UIC 的 `non_coding` 把 IUM 的 `presentation_design` 杀掉了。

**把 `"宣传ppt"` 从 `LabelNonCoding` 移到 `LabelOffice` 是 workaround。**
**给 L1 加层级门槛（只有 L2+ 才能 reject）是 workaround。**
**L1+L2 一致性快速返回是 workaround。**

这些都是在修补双分类器架构的裂缝，而不是消除裂缝。

---

## 机制性修复：统一分类管道

### 核心思想

**消除 IUM 作为独立分类器的角色。将工作流类型判断合并到 UIC 的 L3 tree 通道中，一次 LLM 调用同时完成工具意图分类和工作流类型判断。**

当前：
```
用户消息
  → UIC.Classify()（L1/L2/L3 fusion）→ IntentLabel（12 个）
  → UIC 预检：IsWorkflowCandidate? → reject / pass
  → IUM.Start()（独立 LLM 调用）→ WorkflowType（19+1 个）
  → 工作流启动
```

修复后：
```
用户消息
  → UIC.Classify()（L1/L2/L3 fusion）→ ClassificationResult
      ├── Primary: IntentLabel（12 个，用于工具激活）
      ├── WorkflowType: string（19+1 个，用于工作流启动）
      └── WorkflowReady: bool（是否可以直接启动）
  → handleNeedsUnderstanding：
      if WorkflowType != "" && WorkflowType != "none":
          直接启动工作流（跳过 IUM LLM 调用）
      else:
          正常 agent loop
```

### 为什么可行

1. **UIC L3 tree 和 IUM 做的是同一件事**：都是让 LLM 读用户消息，判断意图类别。UIC L3 判断 12 个 IntentLabel，IUM 判断 19+1 个 WorkflowType。两次 LLM 调用可以合并为一次。

2. **L3 tree prompt 已经按 domain 分组**：当前 tree prompt 有 "内容处理 (Content)" domain，包含 `non_coding`、`search`、`document_delivery`、`office`。只需在 `office` 的 tree_text 中加入工作流类型细分（`presentation_design`），LLM 就能在同一次推理中完成两级分类。

3. **IUM 的 system prompt 中的判断规则可以合并到 L3 tree prompt**：IUM 的"内容处理 vs 工作流"判断口诀、易混淆示例、PPT 特别注意等规则，都可以作为 tree_text 的一部分。

### L3 Intent Tree 升级

当前 tree_text 结构（按 domain 分组）：
```
── 内容处理 (Content) ──
  non_coding: 翻译/总结/整理/搜索等非编码任务
  search: 搜索信息
  document_delivery: 打开/发送/导出文件
  office: 创建新的 PPT/Excel/Word 文档
```

升级后：
```
── 内容处理 (Content) ──
  non_coding: 翻译/总结/整理/搜索等一步完成的内容处理任务
  search: 搜索信息
  document_delivery: 打开/发送/导出文件
  office: 创建新的 PPT/Excel/Word 等办公文档
    → 如果是 PPT/演示文稿创建，workflow_type="presentation_design"
    → 如果是其他办公文档创建，workflow_type=""（不需要工作流）

── 编码开发 (Coding) ──
  coding: 从零创建软件/应用/游戏/工具
    → workflow_type="coding"
  bug_fix: 修复已有代码的 bug
  maintenance: 重构/优化/升级已有代码

── 文档创作 (Document) ──  ← 新增 domain
  workflow_doc: 需要多阶段迭代的结构化文档创作
    → 根据具体类型设置 workflow_type:
      PRD/产品需求 → "product_design"
      商业计划 → "business_plan"
      竞品分析 → "competitive_analysis"
      文献综述 → "literature_review"
      研究报告 → "research_report"
      ... (19 个工作流模板)
```

### L3 输出格式升级

当前 L3 输出：
```json
{"label": "office", "score": 0.85}
```

升级后：
```json
{"label": "office", "score": 0.85, "workflow_type": "presentation_design"}
```

`workflow_type` 是可选字段：
- 非工作流意图（non_coding/search/ssh/browser 等）：不输出或为空
- 工作流意图（coding/office/workflow_doc）：输出具体的 WorkflowType

### ClassificationResult 扩展

```go
type ClassificationResult struct {
    Primary          IntentLabel
    Confidence       float64
    Secondary        []IntentLabel
    ToolNames        []string
    Layer            int
    Reason           string
    CreationOriented bool

    // 新增：工作流类型判断（来自 L3 tree 推理）
    WorkflowType     string  // "coding", "presentation_design", "", etc.
    WorkflowReady    bool    // LLM 判断用户是否已准备好开始
}
```

### handleNeedsUnderstanding 改造

```go
func (h *IMMessageHandler) handleNeedsUnderstanding(engine, userID, text string) *IMAgentResponse {
    // UIC 分类（L1/L2/L3 fusion，一次完成工具意图 + 工作流类型判断）
    uicResult := uic.Classify(intent.MessageContext{Text: text, UserID: userID})

    // 情况 1：UIC 明确判断为非工作流意图
    if uicResult.WorkflowType == "" || uicResult.WorkflowType == "none" {
        // 不需要工作流 → 正常 agent loop
        return nil
    }

    // 情况 2：UIC 判断为工作流意图 → 直接启动工作流
    // 不再需要 IUM 的独立 LLM 调用
    wfType := workflow.WorkflowType(uicResult.WorkflowType)
    tmpl := engine.GetRegistry().Match(wfType)
    if tmpl == nil {
        // 未知的工作流类型 → 正常 agent loop
        return nil
    }

    // 启动工作流（复用 IUM 的 session 创建逻辑，但不做 LLM 调用）
    return h.startWorkflowDirect(engine, userID, text, wfType, uicResult)
}
```

### L1 不再短路

`Classify()` 中移除 `if l1Confident { return }` 短路。L1 始终作为 prior 信号进入 fusion。

但这里有一个关键区别：**不是给 L1 加权重注入 fusion 公式**（那是 workaround），而是**让 L1 的结果作为 L3 tree prompt 的上下文**。

```go
func (u *UnifiedIntentClassifier) Classify(msg MessageContext) ClassificationResult {
    l1Result, _ := classifyByKeywords(u.registry, u.affinity, msg)
    // 不再检查 l1Confident，不再短路

    if canEmb || canTree {
        // L1 结果作为 hint 传给 fusion（L3 tree prompt 可以参考）
        fusionResult := u.classifyWithFusion(msg.Text, l1Result)
        ...
    }
    ...
}
```

L3 tree prompt 中可以加一行：`"系统初步判断为 {l1Result.Primary}，请验证或纠正。"` 这不是给 L1 投票权，而是给 L3 一个参考信号，让 L3 在推理时可以考虑（也可以忽略）。

### IUM 的角色变化

IUM 不再做意图分类。它的角色变为**多轮澄清管理器**：

- 当 UIC 判断为工作流但 `WorkflowReady=false`（信息不足）时，IUM 负责多轮对话澄清需求
- 当 UIC 判断为工作流且 `WorkflowReady=true` 时，直接启动工作流，跳过 IUM

IUM 的 `Start()` 方法不再做分类 LLM 调用，只做 session 创建和多轮管理。分类决策完全由 UIC 负责。

---

## 对 "宣传ppt" 场景的预期效果

1. L1：`non_coding`（Strong "宣传ppt"）— 不短路，进入 fusion
2. L2 embedding：`office`（与 "帮我制作一个PPT演示文稿" 高相似度）
3. L3 tree：domain="内容处理" → intent=`office` → `workflow_type="presentation_design"`
4. Fusion：L2+L3 一致 → `office`，`WorkflowType="presentation_design"`
5. `handleNeedsUnderstanding`：`WorkflowType != ""` → 直接启动 `presentation_design` 工作流
6. **不需要 IUM 的 10-30s LLM 调用** → 总延迟从 10-30s 降到 2-8s

---

## 修改文件清单

### Phase 1: L1 不再短路 + L3 tree 输出工作流类型

#### `corelib/intent/classifier.go`
- `Classify()`：移除 `if l1Confident { return }` 短路
- 所有路径都走 fusion（L2+L3 至少有一个可用时）
- L2+L3 都不可用时回退到 L1（降级模式）

#### `corelib/intent/tree.go`
- `BuildIntentTreeText()`：在 office/coding 等 label 的 tree_text 中加入 workflow_type 细分
- 新增 "文档创作 (Document)" domain，覆盖 19 个工作流模板
- `ParseTreeResponse()`：解析新增的 `workflow_type` 字段
- `TreeCandidate` 新增 `WorkflowType string` 字段

#### `corelib/intent/types.go`
- `ClassificationResult` 新增 `WorkflowType string` 和 `WorkflowReady bool` 字段

#### `corelib/intent/definitions.go`
- `IntentDefinition` 新增 `WorkflowTypes []string` 字段：声明该 label 可能触发的工作流类型列表
- `LabelOffice` 的 `WorkflowTypes`: `["presentation_design"]`
- `LabelCoding` 的 `WorkflowTypes`: `["coding"]`
- 新增 `LabelWorkflowDoc` 定义，`WorkflowTypes` 包含其余 17 个模板类型

#### `corelib/intent/fusion.go`
- `fusionToClassification()`：从 L3 tree 结果中提取 `WorkflowType`，写入 `ClassificationResult`

### Phase 2: handleNeedsUnderstanding 消费统一结果

#### `gui/im_message_handler_workflow.go`
- `handleNeedsUnderstanding()`：
  - 移除 UIC 预检逻辑（不再需要——UIC 的结果已经包含工作流类型判断）
  - 直接使用 `uicResult.WorkflowType` 决定是否启动工作流
  - `WorkflowType != "" && WorkflowType != "none"` → 启动工作流
  - 否则 → `return nil`（正常 agent loop）

#### `corelib/workflow/intent_understanding.go`
- `Start()`：移除分类 LLM 调用，只做 session 创建
- 新增 `StartWithType(userID, text, workflowType)` 方法：接受已确定的工作流类型
- `buildSystemPrompt()`：简化为多轮澄清专用 prompt（移除分类规则）

### Phase 3: 清理

#### `corelib/intent/layer1.go`
- `classifyByKeywords` 保留（仍提供 L1 信号），但 `confident` 返回值不再被消费
- 可选：移除 `confident` 返回值，简化接口

#### `corelib/intent/keyword_registry.go`
- `"宣传ppt"` 等关键词不需要移动——L1 的分类结果不再有短路权力，错误的 label 会被 L2+L3 纠正

---

## 不变量

1. **单一分类管道**：UIC 是唯一的分类器，一次调用同时产出 IntentLabel + WorkflowType
2. **L1 永远不短路**：L1 只提供 prior 信号，最终决策由 L2+L3 fusion 做
3. **IUM 不做分类**：IUM 只负责多轮澄清，分类决策完全由 UIC 负责
4. **工作流类型来自 L3 tree 推理**：不来自关键词匹配，不来自独立的 LLM 调用
5. **所有现有 adapter 接口不变**：`ToTaskIntent`、`ToGateIntent` 继续工作
6. **降级模式**：L2+L3 都不可用时回退到 L1 + IUM 旧路径（向后兼容）

---

## 预期收益

| 指标 | 当前 | 修复后 |
|------|------|--------|
| 工作流启动延迟 | UIC(L1 <1ms) + IUM(10-30s) = 10-30s | UIC fusion(2-8s) = 2-8s |
| LLM 调用次数 | 2 次（UIC L3 + IUM） | 1 次（UIC L3，含工作流类型） |
| "宣传ppt" 分类 | non_coding（L1 短路，错误） | office + presentation_design（fusion，正确） |
| 分类器数量 | 2 个（UIC + IUM），独立决策 | 1 个（UIC），统一决策 |
| 关键词错误影响 | L1 错误 → 一票否决 → 工作流被杀 | L1 错误 → 被 L2+L3 纠正 → 无影响 |
