# 工作流表单自动预填——从记忆/知识库收集默认值

## 核心原则

1. **只填有出处的信息，不猜测**
2. 来源标记：每个预填值必须附带 `source`（`memory` / `web:URL` / `context`）
3. 记忆来源 → 直接显示，用户可改
4. 网络来源 → 带确认标记（），需用户确认后才视为有效
5. 对话上下文来源 → 直接显示（用户刚说的话，可信度高）
6. 无出处 → 留空，不填

## 数据结构

### PrefilledValue

```go
// PrefilledValue represents a pre-filled form field value with provenance tracking.
type PrefilledValue struct {
    Value       interface{} `json:"value"`                  // 预填的值
    Source      string      `json:"source"`                 // "memory" | "web" | "context"
    SourceDetail string     `json:"source_detail,omitempty"` // 记忆条目摘要 / URL / 对话引用
    Confidence  float64     `json:"confidence"`             // 0-1，前端可据此决定是否高亮提示
    NeedsConfirm bool       `json:"needs_confirm"`          // true=需要用户确认（web来源）
}
```

### HandleResult 扩展

```go
type HandleResult struct {
    Action        Action
    PhasePrompt   string
    Reply         string
    // 新增：表单预填数据
    PrefilledData map[string]*PrefilledValue `json:"prefilled_data,omitempty"`
}
```

## 实现分层

### Layer 1: corelib/workflow/v2（状态机层）— 不改

状态机不负责预填逻辑。`ActionShowForm` 返回时不携带预填数据。
状态机是纯状态转换，不依赖 memory/web 等外部模块。

### Layer 2: GUI 消费层（`gui/workflow_form_prefill.go`，新文件）

在状态机返回 `ActionShowForm` 后、发送事件到前端之前，GUI 层执行预填逻辑。

```go
// PrefillFormFromSources 从多个数据源收集表单默认值。
// 严格规则：只填有明确出处的信息，不使用 LLM 推断/生成。
func (h *IMMessageHandler) PrefillFormFromSources(
    ctx context.Context,
    schema *v2.PhaseInputSchema,
    userID string,
    userMessage string,
) map[string]*PrefilledValue
```

### Layer 3: 前端渲染（`WorkflowFormPanel.tsx`）

- 预填值直接作为 `defaultValue` 渲染到表单字段
- `source == "memory"` / `source == "context"` → 正常显示，浅蓝底色标注"自动填充"
- `source == "web"` → 橙色底色 + 图标 + tooltip 显示来源 URL
- `NeedsConfirm == true` → 字段旁显示"确认"复选框，未确认前提交时警告
- 用户修改预填值后，`source` 变为空（用户输入优先级最高）

## 预填数据源（按优先级）

### Source 1: 对话上下文（`context`）

从用户发送的消息 + 最近对话历史中提取。

**提取方式**：对每个 InputSchema 字段，用字段的 `Name` + `Label` + `Placeholder` 作为关键词，在用户消息中做模式匹配。

示例：
- 用户说 "帮我写杰青申请书，我是XX大学的张三"
- 字段 `name`(姓名) → 匹配到 "张三" → `{Value: "张三", Source: "context", Confidence: 0.9}`
- 字段 `institution`(单位) → 匹配到 "XX大学" → `{Value: "XX大学", Source: "context", Confidence: 0.9}`

**约束**：只做精确提取（正则/NER），不让 LLM 推断。提取失败 → 不填。

### Source 2: 长期记忆（`memory`）

对每个未被 Source 1 填充的字段，用字段语义构造 query 调用 `RecallDynamic`。

**查询构造**：`"{Label} {Placeholder中的关键词}"`
- 字段 `institution`(现工作单位) → query: "工作单位 大学"
- 字段 `research_field`(研究领域) → query: "研究领域 研究方向"

**匹配规则**：
- 召回结果中 entry.Content 包含明确的事实性陈述 → 提取值
- entry.Category 为 `user_fact` / `project_knowledge` → 高可信度
- entry.Category 为 `conversation_summary` → 中可信度（可能过时）
- 提取使用规则匹配（正则），不使用 LLM 生成

**约束**：如果召回结果中没有明确匹配字段语义的值 → 不填。宁可漏填不可错填。

### Source 3: 历史表单数据（`memory` 的 `task_artifact` 类别）

如果用户之前做过同类型或相关类型的工作流（如之前写过优青，现在写杰青），从历史 FormData 中复制共同字段。

**查询方式**：`RecallDynamic(query: "{workflowType} 表单数据", category: "task_artifact")`

**复制规则**：
- 字段名完全相同（如 `name`, `institution`, `discipline_code`） → 直接复制
- 字段名不同但语义等价（需要预定义映射表） → 复制
- 不确定的 → 不复制

### Source 4: Web 搜索（`web`）— 仅特定字段

**适用场景**：字段类型为"查证性信息"（如 H指数、论文数、获奖情况），且记忆中无数据。

**触发条件**：
1. 字段有 `Name` 匹配 `h_index` / `total_citations` / `total_papers` / `awards` 等预定义的"可查证字段"列表
2. Source 1-3 都未填充该字段
3. 有足够的上下文（至少知道人名 + 单位）来构造搜索 query

**执行方式**：
- 构造 query: `"{name} {institution} {field_specific_keyword}"`
- 调用 `web_search` → 提取结构化数据
- 标记 `NeedsConfirm: true` + `SourceDetail: URL`

**约束**：
- 搜索失败或结果不确定 → 不填
- 不搜索主观信息（如"研究方向"、"核心贡献"）
- 每次表单最多触发 2 次 web 搜索（避免延迟过长）

## 不预填的字段类型

以下字段类型永远不自动预填：
- `textarea` + `Required: true` 的核心创作字段（如 `core_question`, `key_contribution`, `hypothesis`）
- 文件路径字段（`material_path`, `contract_path` 等）
- 密码字段（`ssh_password`）
- 明确是"本次任务特有"的字段（如 `project_title`、`paper_topic`）

规则：通过字段 Name 的黑名单控制，而非白名单。黑名单：
```go
var noPrefillFieldNames = map[string]bool{
    "material_path": true, "material_text": true,
    "contract_path": true, "contract_text": true,
    "bid_doc_path": true, "bid_doc_text": true,
    "ssh_password": true,
    "project_title": true, "paper_topic": true,
    "core_question": true, "key_contribution": true,
    "hypothesis": true, "paper_title": true, "paper_url": true,
    "work_dir": true,
}
```

## 实现计划

### Phase 1: 数据结构 + 对话上下文提取（纯本地，零延迟）

1. `corelib/workflow/v2/prefill_types.go`：定义 `PrefilledValue` 结构体
2. `gui/workflow_form_prefill.go`：`PrefillFormFromSources()` 骨架 + Source 1（对话上下文提取）
3. `gui/workflow_form_prefill_context.go`：从用户消息 + 最近对话历史提取字段值（NER/正则）
4. `HandleResult.PrefilledData` 字段添加
5. GUI 消费 `ActionShowForm` 时调用 `PrefillFormFromSources()`，结果附到前端事件
6. 前端表单渲染使用预填值

**预期效果**：用户说"我是张三，XX大学教授，帮我写杰青" → 表单中姓名、单位、职称自动填好

### Phase 2: 记忆召回（异步，~50ms）

1. `gui/workflow_form_prefill_memory.go`：对未填充字段，构造 query 调用 `RecallDynamic`
2. 结果提取规则（正则匹配，不用 LLM）
3. 历史 FormData 复制逻辑

**预期效果**：用户之前做过优青，现在做杰青 → 姓名、单位、学科代码、H指数等自动从历史表单复制

### Phase 3: Web 搜索（可选，异步，需用户确认）

1. `gui/workflow_form_prefill_web.go`：对可查证字段触发 web 搜索
2. 结构化数据提取
3. 前端确认 UI

**预期效果**：已知用户名+单位 → 搜索 Google Scholar 填充 H指数和引用数 → 橙色标注需确认

### Phase 4: 优化 + 持久化 DONE

1. 用户确认的 web 数据自动沉淀到记忆（下次不需要再搜索）
2. 表单提交后，稳定的用户事实字段（姓名/单位/学科代码等）自动沉淀为 `user_fact` 记忆
3. 下次同类型工作流触发时，Phase 2 的 RecallDynamic 自动召回这些沉淀的事实
4. 预填缓存（同一用户短时间内多次触发同类型工作流 → 缓存结果）— 由记忆系统的去重机制天然覆盖

## 前端交互设计

```
┌──────────────────────────────────────────────┐
│  杰青申请人基本信息                              │
├──────────────────────────────────────────────┤
│  姓名 [张三___________] 来自记忆            │
│  性别 [男 ▼___________]                       │
│  出生日期 [1982年3月_____] 来自记忆         │
│  现工作单位 [XX大学 计算机学院] 来自对话      │
│  职称 [教授___________] 来自记忆            │
│  H指数 [42____________] 来自网络 [确认]     │
│  研究领域 [自然语言处理__] 来自记忆          │
│  拟解决的科学问题 [____________]  ← 不预填     │
│                                              │
│           [提交]  [跳过表单]                   │
└──────────────────────────────────────────────┘
```

图标说明：
- 蓝色点：自动填充（来自记忆/对话），用户可直接修改
- 橙色警告：来自网络搜索，需要点击"确认"表示信息准确
- 无标记：用户手动填写的字段

## 性能约束

- Source 1（上下文提取）：< 5ms（纯字符串操作）
- Source 2（记忆召回）：< 100ms（本地 BM25 + embedding）
- Source 3（Web 搜索）：< 3s（最多 2 次搜索），异步执行不阻塞表单渲染
- 表单先用 Source 1+2 的结果渲染，Source 3 结果异步补充（字段动态更新）

## 安全约束

- 预填数据不写入 FormData（只有用户提交后才写入）
- 前端明确区分"预填值"和"用户确认值"——只有后者参与 SubmitForm
- Web 搜索结果中不包含敏感信息（不搜索密码、API Key 等）
- 用户关闭预填功能时（设置项），跳过全部预填逻辑
