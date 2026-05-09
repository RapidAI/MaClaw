# Agent 动态 UI 与结构化输入运行时设计

状态：draft  
日期：2026-05-07  
范围：AG-UI/事件协议、Skill/Tool/Business Object 非侵入式适配、右侧 Task Panel、结构化输入校验与业务数据落库

## 1. 背景

传统企业 MIS 系统依赖固定菜单、固定页面和固定表单。用户必须知道去哪个系统、打开哪个页面、填写哪些字段。Agent 让用户可以用自然语言、图片、语音、文件表达意图，但如果最终目标是企业业务流程，系统仍然需要把这些输入转换成可校验、可审批、可存储的结构化数据。

因此需要一套动态 UI 机制：agent 负责理解意图和抽取候选数据，runtime 负责选择 schema、生成受控界面、校验参数、要求用户确认，并把最终结构化数据写入业务数据层或触发工作流。

这套机制不要求修改标准 skill，也不允许 LLM 直接生成任意前端代码。所有界面都由受控 schema 和组件白名单渲染。

## 2. 目标

1. 在复杂 skill、tool、MCP 调用前自动生成参数录入界面，减少无效调用。
2. 在报销、请假、采购、合同、工单等业务场景中，自动生成业务对象草稿编辑器。
3. 支持从自然语言、附件、图片、已有数据中抽取候选字段，并交给用户补全确认。
4. 支持高风险动作确认、长任务进度、结果浏览、异常修复等动态交互。
5. 最终输出结构化 JSON，经 schema、权限、策略、工作流校验后再写库或调用外部系统。

非目标：

- 不让模型直接执行任意 JSX/HTML/脚本。
- 不要求标准 skill 修改自身格式。
- 不让 LLM 绕过校验、权限、审批或审计。
- 第一阶段不替代完整 ERP/CRM/HRM，而是优先替代表单入口、数据采集和流程发起体验。

## 3. 总体架构

```text
User Input
  - text / voice / image / file / existing data
        ↓
Conversation Layer
        ↓
Agent Reasoning Layer
  - intent detection
  - field extraction
  - candidate draft
        ↓
Interaction Runtime
  - trigger policy
  - view lifecycle
  - pause/resume action
        ↓
Adapter Registry
  - skill adapter
  - tool/MCP adapter
  - business object adapter
        ↓
Dynamic UI Renderer
  - form / wizard / table editor / approval / progress
        ↓
Validation & Policy Engine
        ↓
Business Object Store / Workflow / External MIS
```

核心原则：

- agent 可以请求 UI，但是否打开、如何打断用户，由 runtime trigger policy 决定。
- adapter 是非侵入式 sidecar，不修改标准 skill。
- UI 只渲染注册组件和 schema，不执行模型生成代码。
- 任何写库、提交审批、发送消息、删除/覆盖数据的动作都必须经过校验与确认。

## 4. 关键概念

### 4.1 Agent View

Agent View 是右侧 Task Panel 的统一描述。

```ts
type AgentView =
  | { type: "form"; title: string; fields: FieldSchema[] }
  | { type: "wizard"; title: string; steps: WizardStep[] }
  | { type: "approval"; title: string; action: ActionSummary }
  | { type: "progress"; title: string; steps: StepState[] }
  | { type: "table_editor"; title: string; columns: ColumnSchema[]; rows: unknown[] }
  | { type: "resource_picker"; title: string; resourceType: string; options: ResourceOption[] }
  | { type: "field_mapper"; title: string; sourceFields: string[]; targetFields: FieldSchema[] }
  | { type: "rule_builder"; title: string; rules: RuleSchema[] }
  | { type: "artifact"; title: string; artifact: ArtifactRef }
  | { type: "result_browser"; title: string; results: ResultItem[] };
```

### 4.2 Skill Adapter

Skill Adapter 描述标准 skill 如何被结构化调用。标准 skill 不修改，adapter 可由平台自动推断、用户确认、项目覆盖。

```json
{
  "type": "skill_adapter",
  "target": "imagegen",
  "source": "sidecar",
  "fields": [
    { "name": "prompt", "label": "Prompt", "type": "textarea", "required": true },
    { "name": "size", "label": "Size", "type": "select", "options": ["1024x1024", "1536x1024"] }
  ],
  "mapping": {
    "type": "template",
    "value": "Use imagegen with prompt={{prompt}}, size={{size}}"
  }
}
```

### 4.3 Tool / MCP Adapter

Tool/MCP Adapter 优先从已有 JSON Schema、OpenAPI、函数签名、MCP tool schema 中生成。runtime 在 tool 参数缺失或校验失败时自动拦截，生成参数表单，用户补齐后再继续执行。

### 4.4 Business Object Adapter

Business Object Adapter 是替代传统 MIS 表单入口的核心。它定义业务对象的数据结构、UI、策略、流程和存储映射。

```json
{
  "object": "expense_claim",
  "title": "Expense Claim",
  "fields": [
    { "name": "applicant", "type": "user_ref", "required": true },
    { "name": "expense_date", "type": "date", "required": true },
    { "name": "cost_center", "type": "department_ref", "required": true },
    { "name": "items", "type": "array", "itemType": "expense_item", "required": true }
  ],
  "policies": ["meal_limit_policy", "duplicate_invoice_policy"],
  "workflow": "expense_approval_flow",
  "storage": { "table": "expense_claims" }
}
```

## 5. 右侧 Task Panel 呈现

推荐布局是左侧 Chat / Agent Trace，右侧 Task Panel / Workbench。

```text
┌─────────────────────────┬──────────────────────────────────┐
│ Chat / Agent Trace      │ Task Panel                        │
│                         │                                  │
│ 用户输入                 │ 参数表单                          │
│ agent 解释               │ 业务对象草稿                      │
│ tool 调用摘要             │ 审批确认                          │
│ 错误与建议                │ 进度 / 结果 / 预览                 │
└─────────────────────────┴──────────────────────────────────┘
```

左侧负责自然语言、解释、事件摘要；右侧负责结构化操作。右侧不是只读 preview，而是可交互任务面板。

第一阶段组件白名单：

- `form`
- `approval`
- `progress`
- `result_browser`
- `artifact`

第二阶段扩展：

- `wizard`
- `table_editor`
- `resource_picker`
- `field_mapper`
- `rule_builder`
- `batch_editor`
- `diff_merge`

## 6. 触发机制

触发来源分三类：

1. Agent 主动触发：缺少参数、需要选择路径、建议打开业务对象草稿。
2. Runtime 自动触发：tool schema 校验失败、高风险动作、业务对象识别成功、产生 artifact。
3. 用户手动触发：用户说“用表单”“打开右侧面板”“让我确认参数”。

```ts
type ViewTrigger = {
  source: "agent" | "runtime" | "user";
  reason:
    | "missing_required_inputs"
    | "complex_parameters"
    | "business_object_detected"
    | "approval_required"
    | "long_running_task"
    | "artifact_available"
    | "multiple_results"
    | "validation_failed"
    | "manual_request";
  priority: "low" | "medium" | "high";
  view: AgentView;
};
```

建议策略：

- `high`：自动打开右侧面板，例如缺必填字段、高风险确认、提交审批。
- `medium`：右侧更新，同时聊天里显示按钮，例如 artifact、多个结果。
- `low`：只更新 Activity/Artifacts，不打断用户。

### 6.1 业务对象识别必须走意图识别

业务对象识别不能用关键词直接决定。关键词只能作为低权重 signal 或召回 hint，最终应由 Business Intent Recognition 输出候选列表、置信度和决策建议。

```text
User input / file / context
        ↓
business intent recognition
        ↓
candidate ranking
        ↓
confidence decision
        ↓
draft object / chooser / clarification
```

推荐决策：

- `confidence >= 0.8`：自动创建 draft，并打开 Task Panel。
- `0.5 <= confidence < 0.8`：打开候选选择器，让用户选择业务对象。
- `confidence < 0.5`：继续聊天澄清。

识别输入包括：

- 用户自然语言。
- 附件类型和 OCR/解析结果。
- 当前上下文、用户角色、部门、历史任务。
- business object / business action / dataset schema 的描述、字段、正反例和 semantic signals。

示例：用户说“昨天去杭州见客户，高铁 174，午餐 86，凭证在附件里”，即使没有说“报销”，runtime 也应通过“出行 + 餐费 + 金额 + 附件凭证”的语义信号，将 `finance.expense_submit` 排在候选前列。

## 7. 数据流

### 7.1 通用流

```text
User message / file
  ↓
Intent detection
  ↓
Schema matching
  ↓
Candidate extraction
  ↓
Draft object creation
  ↓
Task Panel render
  ↓
User correction / completion
  ↓
Validation / policy / permission
  ↓
Submit
  ↓
Storage / workflow / tool execution
```

### 7.2 报销示例

用户输入：

```text
我昨天去杭州见客户，高铁 174，午餐 86，发票在附件里。
```

agent 生成候选草稿：

```json
{
  "object": "expense_claim",
  "status": "draft",
  "source": "agent_extracted",
  "confidence": 0.86,
  "data": {
    "expense_date": "2026-05-06",
    "trip_from": "上海",
    "trip_to": "杭州",
    "items": [
      { "category": "transport", "amount": 174, "currency": "CNY" },
      { "category": "meal", "amount": 86, "currency": "CNY" }
    ],
    "total": 260
  },
  "missingFields": ["customer", "cost_center", "approval_manager"]
}
```

右侧 Task Panel 展示：

- 基础信息
- 明细表格
- 发票附件预览
- 缺失字段
- 政策校验结果
- 提交审批按钮

## 8. Adapter 自动生成

标准 skill 下载后通常没有 UI 描述，因此需要 UI Profiler。

Adapter 来源优先级：

```text
1. Project adapter
2. User customized adapter
3. Official/community adapter
4. Generated cached adapter
5. Fresh LLM inference
6. Chat fallback
```

自动推断来源：

- `SKILL.md`
- README
- plugin manifest
- MCP/tool schema
- JSON Schema
- OpenAPI
- TypeScript/Python 签名
- CLI help
- 历史调用样本

推断结果必须带置信度：

```json
{
  "target": "some_skill",
  "source": "llm_inferred",
  "confidence": 0.78,
  "needsReview": true,
  "fields": [
    { "name": "input_file", "type": "file", "required": true },
    { "name": "output_format", "type": "select", "options": ["pdf", "docx", "html"] }
  ]
}
```

低置信度时只展示建议表单，用户确认后才保存为 sidecar adapter。

## 9. 校验、安全与审计

校验层级：

```text
UI validation
  ↓
schema validation
  ↓
business policy validation
  ↓
permission validation
  ↓
workflow validation
  ↓
storage transaction validation
```

LLM 可以做：

- 意图识别
- 字段抽取
- 候选草稿生成
- UI 类型建议
- 错误解释和修复建议

LLM 不可以做：

- 直接写核心业务库
- 绕过 schema、权限、策略、审批
- 直接执行任意前端代码
- 私自执行高风险工具

必须 human-in-the-loop 的动作：

- 写入业务系统
- 提交审批
- 发邮件/消息
- 付款/退款
- 删除/覆盖数据
- 修改权限
- 部署/发布
- 调用外部付费接口

每次结构化提交记录审计日志：

- 用户原始输入
- agent 抽取结果
- 用户修改内容
- 最终提交数据
- 校验结果
- 审批结果
- 工具调用记录
- 操作者和时间戳

## 10. MVP 路线

### Phase 1：右侧 Task Panel 骨架

- 增加 AgentView 类型。
- 在 AI Assistant 右侧预览区域渲染 Task Panel。
- 支持 form、approval、progress、result 四类静态视图。
- 先使用 state 注入，不绑定后端。

### Phase 2：Skill / Tool Adapter

- 增加 adapter registry。
- 支持 sidecar adapter。
- 支持 JSON Schema 到 form schema。
- 支持从 `SKILL.md` 推断候选 adapter。

### Phase 3：Business Object Runtime

- 增加 draft object。
- 增加 business object schema。
- 接入结构化数据底座。
- 支持报销/请假/采购的第一批对象。

### Phase 4：企业级输入组件

- table editor
- resource picker
- field mapper
- rule builder
- batch editor
- artifact preview

### Phase 5：治理与生态

- adapter marketplace
- schema versioning
- audit log
- permission model
- community/project adapter override

## 11. 核心结论

这套系统的本质不是“给 agent 加一个表单”，而是建立一个从自然语言到结构化业务对象的动态交互运行时。

```text
自然语言入口
+
动态结构化 UI
+
业务对象模型
+
规则校验
+
工作流
+
审计存储
```

agent 负责理解，runtime 负责约束，用户负责确认，系统负责存储和流程。这是 agent 取代传统企业 MIS 表单入口的关键路径。
