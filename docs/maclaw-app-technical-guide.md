# MaClaw App 技术文档

## 概述

MaClaw App 是基于 **AG UI 协议**和 **Skill 运行时**构建的应用程序框架。它将各种能力（文档处理、数据管理、审批流程、自动化任务）封装为可安装、可运行、可发布的应用单元，统一在 App Studio 面板中管理。

本文档覆盖 MaClaw App 的四种类型、核心运行机制、数据模型，以及底层 DataSrv 的 Dataset/Fields/Business Actions 概念。

---

## 一、MaClaw App 四种类型

```
┌────────────────────────────────────────────────────────────────┐
│                    MaClaw App 类型体系                          │
├────────────────┬────────────────┬──────────────┬──────────────┤
│   tool_app     │ enterprise_    │ enterprise_  │ automation_  │
│   工具应用     │ approval_app   │ normal_app   │ app          │
│                │ 企业审批应用    │ 企业普通应用  │ 自动化应用    │
├────────────────┼────────────────┼──────────────┼──────────────┤
│ PDF转Word      │ 报销申请       │ 客户建档     │ 网页采集      │
│ 文档脱敏       │ 合同审查       │ 采购入库     │ 数据同步      │
│ 表格分析       │ 请假审批       │ 库存盘点     │              │
├────────────────┼────────────────┼──────────────┼──────────────┤
│ launchMode:    │ launchMode:    │ launchMode:  │ launchMode:  │
│ fixed_skill_ui │ agent_dynamic  │ agent_dynamic│ automation_  │
│                │ _ui            │ _ui          │ console      │
├────────────────┼────────────────┼──────────────┼──────────────┤
│ installUnit:   │ installUnit:   │ installUnit: │ installUnit: │
│ skill          │ enterprise_    │ enterprise_  │ skill        │
│                │ app_pack       │ app_pack     │              │
├────────────────┼────────────────┼──────────────┼──────────────┤
│ 数据层:        │ 数据层:        │ 数据层:      │ 数据层:      │
│ 无 (文件I/O)   │ DataSrv +      │ DataSrv      │ 无/自定义    │
│                │ Hub Workflow   │              │              │
└────────────────┴────────────────┴──────────────┴──────────────┘
```

---

## 二、tool_app（工具应用）

### 概念

工具应用是最简单的 MaClaw App 类型。它将一个已安装的 Skill 包装为"上传文件 → 处理 → 输出结果"的固定 UI 模式。适用于文档转换、数据提取、内容生成等单次执行任务。

### 运行模式：`fixed_skill_ui`

```
┌──────────────────────────────────────────────────────────┐
│                   tool_app 运行界面                        │
│                                                          │
│  ┌─────────────────┐    ┌─────────────────────────────┐  │
│  │   输入区域       │    │   输出区域                    │  │
│  │                 │    │                             │  │
│  │ [上传文件]      │    │  📄 生成的文档               │  │
│  │ [参数表单]      │    │  📊 分析结果                 │  │
│  │ [输出格式选择]  │    │  📁 下载链接                 │  │
│  │                 │    │                             │  │
│  │ [▶ 运行]       │    │  📋 运行历史                 │  │
│  └─────────────────┘    └─────────────────────────────┘  │
└──────────────────────────────────────────────────────────┘
```

### 执行链路

```
用户点击"运行"
  → 前端收集输入：文件路径 + 参数字段 + 输出格式
  → 调用 RunNLSkillAsync(skillID, {
      _maclaw_app: true,
      app_id: "pdf-word",
      input_mode: "file",
      output_mode: "docx",
      file_path: "C:/Users/.../staged/input.pdf",
      fields: { language: "zh", ... },
      prompt: "将 PDF 转换为 Word 文档...",
    })
  → Skill Runner 执行（bash/craft_tool 步骤）
  → 产出 artifacts（输出文件）
  → 前端轮询 GetNLSkillRunStatus(runID) 获取进度和结果
  → 显示输出文件下载链接
```

### Skill 侧声明

一个 Skill 通过在目录中放置 `maclaw.apps.json` 声明自己是 tool_app：

```json
{
  "x_maclaw_apps": "v1",
  "apps": [
    {
      "id": "pdf-word",
      "skill_id": "pdf-word-converter",
      "name": "PDF 转 Word",
      "description": "上传 PDF，保留版式输出 Word 文档。",
      "category": "文档处理",
      "kind": "tool_app",
      "icon": "pdf",
      "input_mode": "file",
      "output_modes": ["docx", "pdf"],
      "fields": [
        { "name": "language", "label": "文档语言", "type": "select",
          "options": [{"label":"中文","value":"zh"}, {"label":"English","value":"en"}] },
        { "name": "keep_layout", "label": "保留版式", "type": "boolean", "default": true }
      ]
    }
  ]
}
```

### SkillAppManifestEntry 结构

```go
type SkillAppManifestEntry struct {
    ID                string   // 应用唯一标识
    SkillID           string   // 绑定的 Skill 名称
    Name              string   // 显示名称
    Description       string   // 功能描述
    Category          string   // 分类（文档处理/数据分析/...）
    Kind              string   // "tool_app"
    Icon              string   // 图标标识
    InputMode         string   // "file" | "form" | "mixed"
    MultipleFiles     bool     // 是否支持多文件
    OutputModes       []string // 输出格式列表 ["docx", "pdf", "xlsx"]
    Fields            []Field  // 参数表单字段定义
    AppDefinitionFile string   // 声明文件名
}
```

### 输入模式

| InputMode | UI 表现 | 适用场景 |
|-----------|---------|---------|
| `file` | 上传文件区 + 输出格式选择 | PDF转换、文件翻译 |
| `form` | 纯参数表单 | 密码生成、文本处理 |
| `mixed` | 上传文件 + 参数表单 | 合同审查（上传合同 + 选审查重点） |

### 工具应用典型案例

| App ID | 名称 | Skill 实现方式 |
|--------|------|--------------|
| pdf-word | PDF 转 Word | Python pypdf + python-docx |
| doc-redact | 文档脱敏 | NER 识别 + 替换 |
| sheet-analysis | 表格分析 | Python pandas 分析 + 图表生成 |
| contract-review | 合同审查 | LLM 分析条款风险 |

---

## 三、enterprise_approval_app（企业审批应用）

### 概念

审批型应用将 DataSrv 的数据管理能力与 Hub 的审批工作流引擎结合。用户提交数据后自动触发审批流程，审批人在 AG UI 面板或 IM 通道中处理。

### 运行模式：`agent_dynamic_ui`

```
┌──────────────────────────────────────────────────────────┐
│              enterprise_approval_app 运行界面              │
│                                                          │
│  ┌──────────────────────────────────────────────────┐    │
│  │ [我的申请] [待我审批] [已处理] [需关注] [全部]     │    │
│  └──────────────────────────────────────────────────┘    │
│                                                          │
│  ┌──────────────┐    ┌────────────────────────────────┐  │
│  │ 审批列表      │    │ 审批详情                        │  │
│  │              │    │                                │  │
│  │ EXP-001 ●待审│    │ 标题：差旅报销                  │  │
│  │ EXP-002 ✓通过│    │ 申请人：张三                    │  │
│  │ EXP-003 ✗拒绝│    │ 金额：¥2,580                   │  │
│  │              │    │ 当前节点：部门主管审批           │  │
│  │              │    │                                │  │
│  │              │    │ [✓ 通过]  [✗ 拒绝]  [↑ 转签]  │  │
│  └──────────────┘    └────────────────────────────────┘  │
└──────────────────────────────────────────────────────────┘
```

### 执行链路

```
1. 用户填写审批表单（通过 AG UI form 或 App 界面）

2. 调用 RunNLSkillAsync(workflowSkillID, {
     _maclaw_app: true,
     app_id: "expense",
     business_entity: "expense_report",
     business_action: "submit",
     ...表单数据
   })

3. Workflow Skill 执行 → 创建审批实例 (ApprovalInstance)

4. Hub WorkflowExecutor 按图流转：
   trigger → form(填写) → approval(审批) → action(执行) → terminal

5. 审批人收到通知 → 在 AG UI / IM 中做出决策 (approve/reject)

6. 结果回写：
   → SyncMaclawAppApprovalInstanceToDataSrv
   → DataSrv 更新 RecordApproval 状态
   → 前端刷新审批列表
```

### Manifest 结构

```typescript
manifest: {
    schema: 'maclaw.app.v1',
    installUnit: 'enterprise_app_pack',
    entryKind: 'enterprise_approval_app',
    launchMode: 'agent_dynamic_ui',
    datasrv: {
        domain: 'finance',
        preferredAction: 'finance.expense_submit',
        preferredView: 'finance.expense_review',
        preferredReport: 'finance.expense_by_status',
        preferredDashboard: 'finance.overview',
    },
    mis: {
        approvalBindings: [{
            event: 'expense.submitted',
            workflowSkillId: 'expense-approval-workflow',
            objectRole: 'expense_report',
        }]
    },
    dependencies: {
        skills: [
            { id: 'expense-approval-workflow', kind: 'workflow_skill', required: true, source: 'hub' }
        ]
    }
}
```

### 审批工作流图结构

Hub 的 WorkflowExecutor 使用有向图描述流程：

```go
type WorkflowGraph struct {
    Nodes []WorkflowNode  // 节点列表
    Edges []WorkflowEdge  // 边列表（有向）
}

// 节点类型
NodeTrigger         // 触发节点（入口）
NodeForm            // 表单节点（收集信息）
NodeApproval        // 审批节点（等待决策）
NodeConditionBranch // 条件分支（按规则路由）
NodeAction          // 动作节点（执行操作）
NodeNotification    // 通知节点（发送消息）
NodeSubProcess      // 子流程
NodeTypeTerminal    // 终止节点

// 审批模式
ModeSingle          // 单人审批
ModeCountersign     // 会签（所有人通过）
ModeAnyNofM         // N/M 通过
ModeSequential      // 顺序审批（逐个）
```

---

## 四、enterprise_normal_app（企业普通应用）

### 概念

企业普通应用绑定到 DataSrv 的 Business Actions/Views/Reports，但没有审批工作流。适用于主数据管理（客户、供应商、物料等）和业务操作（录入、查询、导出）。

### 运行模式：`agent_dynamic_ui`

执行时直接调用 `ExecuteMaclawAppBusinessOperation`，通过 DataSrv HTTP API 执行 Business Action 或查询 View/Report。

```
用户在 App 界面操作
  → ExecuteMaclawAppBusinessOperation({
      appID: "customer-profile",
      preferredAction: "sales.customer_upsert",  // 或
      preferredView: "sales.customer_directory",  // 或
      preferredReport: "sales.customer_activity",
      data: { customer_no: "C-001", name: "Acme", ... }
    })
  → Go 后端调用 DataSrv HTTP API
  → 返回执行结果（含 result_payload, outputs, artifacts）
```

---

## 五、automation_app（自动化应用）

### 概念

自动化应用用于定时执行、持续监控的任务。运行模式为 `automation_console`，提供启动/停止/状态监控的操作面板。

当前状态：**UI 框架已有**（运行模式选择、状态显示），**实际调度引擎待实现**。

---

## 六、AG UI 协议（Agent UI Lifecycle）

### 概念

AG UI 是 MaClaw App 生态的通信协议——后端 Agent loop 通过 `emitAgentView` 向前端推送结构化 UI（表单、审批面板、进度条、结果浏览器），前端通过 `AgentViewSubmit` 回传用户输入。

### 核心交互模式

```
后端 (Go)                              前端 (React)
    │                                      │
    │──── emitAgentView(form) ────────────▶│  AgentTaskPanel 渲染表单
    │                                      │
    │◀─── AgentViewSubmit(data) ───────────│  用户填写提交
    │                                      │
    │──── emitAgentView(result) ──────────▶│  显示执行结果
    │                                      │
    │──── emitAgentViewLifecycle ─────────▶│  更新状态 (submit/complete/error)
    │                                      │
```

### AgentView 类型

| View ID 前缀 | 用途 | 触发场景 |
|--------------|------|---------|
| `skill:run:{id}` | Skill 运行参数表单 | 工具应用启动 |
| `skill:status` | Skill 运行状态 | 运行中状态更新 |
| `tool:run:{id}` | 注册工具参数表单 | Agent 调用注册工具 |
| `tool:approval` | 工具安全审批确认 | 高风险操作 |
| `workflow:form:{phase}` | 工作流阶段表单 | V2 工作流收集输入 |
| `intent:{id}` | 业务意图表单 | MIS 数据录入 |
| `commit:{id}` | 业务数据确认 | DryRun 后确认提交 |

### 表单字段类型

```typescript
type AgentViewField = {
    name: string;          // 字段标识
    label: string;         // 显示名称
    type: string;          // text | number | select | multiselect | boolean |
                           // date | datetime | file | directory | textarea | hidden
    required?: boolean;    // 是否必填
    description?: string;  // 帮助文本
    placeholder?: string;  // 占位提示
    options?: { label: string; value: string }[];  // select/multiselect 选项
    value?: any;           // 默认值
    min?: number;          // 数值最小值
    max?: number;          // 数值最大值
    pattern?: string;      // 正则校验
};
```

---

## 七、DataSrv 核心概念

以下三个概念是 `enterprise_approval_app` 和 `enterprise_normal_app` 的数据基础。`tool_app` 不依赖 DataSrv。

### 7.1 Dataset（数据集）

数据容器，相当于数据库"表"。命名格式：`{domain}.{name}`

内置 20+ 企业模板覆盖：sales / finance / hr / procurement / inventory / legal / assets / company。

首次执行 Business Action 时，如果目标 Dataset 不存在，系统从模板自动创建（含字段定义和示例数据）。

### 7.2 Fields（字段定义）

Dataset 中每条 Record 的数据结构。支持：
- **类型**：string / number / boolean / date / datetime / record_ref / json
- **约束**：required / unique / indexed / sensitive
- **配置**：枚举值（enum）、外键引用（ref_dataset_id）

示例（销售订单字段）：
```
sales.orders:
├── order_no     string  [必填, 唯一, 索引]
├── customer     string  [必填, 索引]
├── customer_ref record_ref [索引, 引用→sales.customers]
├── amount       number  [必填, 索引]
├── stage        string  [索引, 枚举: draft/confirmed/won/lost/cancelled/fulfilled]
├── payment_status string [索引, 枚举: unpaid/partial/paid/refunded/overdue]
└── order_date   date    [索引]
```

### 7.3 Business Actions（业务操作）

语义化的数据写入封装。两种操作类型：

| Operation | 含义 | 场景 |
|-----------|------|------|
| `upsert_record` | 全量创建/更新 | 录入新数据 |
| `merge_record` | 增量合并（只改指定字段） | 状态流转、审批结果 |

内置 30+ 标准操作。执行时自动：校验必填字段 → 检查业务规则 → 写入/更新 Record → 记录事件日志 → 返回结果。

支持 DryRun（预演）：只校验不执行，用于表单提交前的实时验证。

---

## 八、App 来源与生命周期

### 三种来源

| 来源 | source 值 | 说明 |
|------|-----------|------|
| 前端内置占位 | `builtin` / `datasrv` / `skill` / `market` | `initialApps` 硬编码的展示位 |
| Skill 声明发现 | `skill` | 已安装 Skill 目录中的 `maclaw.apps.json` |
| Hub 安装 | `market` / `enterprise_hub` | 通过能力市场下载安装的 App 包 |

### 生命周期

```
1. 发现/创建
   ├── 前端 initialApps 预置（占位，缺依赖时不可运行）
   ├── ListSkillAppManifests() 扫描已安装 Skill 目录
   └── DownloadMaclawAppPackageFromHub() 从能力市场下载

2. 安装
   PlanMaclawAppInstall → 解析依赖（workflow_skill / app_skill）
   InstallMaclawAppDependencies → 安装缺失 Skill
   RecordMaclawAppInstall → 写入 app_install_records.json + DataSrv 注册

3. 运行
   ├── tool_app: RunNLSkillAsync(skillID, params) → Skill Runner 执行
   ├── enterprise_*: ExecuteMaclawAppBusinessOperation → DataSrv API
   └── approval: RunNLSkillAsync(workflowSkillID) → Hub Workflow 流转

4. 发布
   SubmitMaclawAppPackage → SHA256 签名 → SyncToHub → Review → Publish
```

### App 包结构（`maclaw.apps.json`）

```json
{
  "x_maclaw_apps": "v1",
  "apps": [
    {
      "id": "expense-approval",
      "skill_id": "expense-approval-app",
      "name": "报销申请",
      "kind": "enterprise_approval_app",
      "category": "OA",
      "icon": "receipt",
      "input_mode": "form",
      "output_modes": ["pdf"],
      "fields": [
        { "name": "amount", "label": "报销金额", "type": "number", "required": true },
        { "name": "category", "label": "费用类别", "type": "select",
          "options": [{"label":"交通","value":"traffic"}, {"label":"餐饮","value":"food"}] }
      ]
    }
  ]
}
```

---

## 九、实现状态总结

| 模块 | tool_app | enterprise_approval_app | enterprise_normal_app | automation_app |
|------|----------|------------------------|-----------------------|----------------|
| UI 框架 | ✅ 完整 | ✅ 完整 | ✅ 完整 | ✅ 基本 |
| 运行引擎 | ✅ Skill Runner | ✅ Hub Workflow | ✅ DataSrv API | ❌ 待实现 |
| 数据层 | N/A | ✅ DataSrv + RecordApproval | ✅ DataSrv | N/A |
| 内置 Skill | ❌ 需安装 | ❌ 需开发 workflow_skill | N/A | ❌ 需开发 |
| 安装/发布 | ✅ 完整 | ✅ 完整 | ✅ 完整 | ✅ 完整 |
| 治理/审计 | ✅ | ✅ | ✅ | — |

### 让各类 App 真正可用

**tool_app**（最简单）：
1. 写一个 Skill（bash/Python 脚本实现文件处理）
2. 在 Skill 目录放 `maclaw.apps.json` 声明为 tool_app
3. 安装 Skill → App Studio 自动发现 → 可运行

**enterprise_normal_app**：
1. 部署 DataSrv，配置 API Key
2. 在 MaClaw 设置启用 MIS Data（填入 endpoint + token）
3. 首次操作时 DataSrv 自动从模板创建 Dataset + Fields

**enterprise_approval_app**：
1. 以上两步 +
2. 开发 workflow_skill（定义审批图节点和分支规则）
3. 发布到 Hub 或本地安装
4. 通过 `RecordMaclawAppInstall` 绑定 App → Workflow Skill

---

## 附录 A：App 类型快速对比

| 维度 | tool_app | enterprise_approval_app | enterprise_normal_app | automation_app |
|------|----------|------------------------|-----------------------|----------------|
| 用途 | 文件/文档处理 | 需审批的业务流程 | CRUD 数据管理 | 定时/持续任务 |
| 输入 | 文件 + 参数 | 业务表单 | 业务表单 | 配置参数 |
| 输出 | 文件 (docx/pdf/xlsx) | 审批结果 + 状态 | 数据记录 | 运行状态 |
| 后端 | Skill Runner | Skill + Hub Workflow + DataSrv | DataSrv | Skill + 调度器 |
| 状态 | 无状态（每次独立） | 有状态（审批流转） | 有状态（数据持久化） | 有状态（运行/停止） |

## 附录 B：核心 Wails Binding 列表

| Binding | 用途 |
|---------|------|
| `ListSkillAppManifests()` | 从已安装 Skill 发现 tool_app |
| `RunNLSkillAsync(skillID, params)` | 异步执行 Skill（tool_app 和 approval_app） |
| `GetNLSkillRunStatus(runID)` | 查询 Skill 运行状态和产出物 |
| `ListMaclawAppInstalls(limit)` | 列出已安装的 App |
| `PlanMaclawAppInstall(packageJSON)` | 解析 App 包依赖 |
| `InstallMaclawAppDependencies(plan)` | 安装缺失依赖 |
| `RecordMaclawAppInstall(...)` | 注册 App（本地 + DataSrv） |
| `ExecuteMaclawAppBusinessOperation(input)` | 执行企业业务操作 |
| `ListMaclawAppApprovalInstances(appID)` | 查询审批实例列表 |
| `RecordMaclawAppApprovalInstance(instance)` | 创建/更新审批实例 |
| `SyncMaclawAppApprovalInstanceToDataSrv(input)` | 同步审批结果到 DataSrv |
| `DownloadMaclawAppPackageFromHub(capID)` | 从能力市场下载 App 包 |
| `SubmitMaclawAppPackage(manifest)` | 提交 App 包到能力市场 |
