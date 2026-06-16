# MaClaw 应用面板与应用程序工作室设计

## 目标

MaClaw 需要在 GUI 中间面板新增一个 `应用` 面板，用图标化方式承载可复用的软件入口。这个面板不是传统工具列表，也不是简单 Skill 快捷方式，而是把 MaClaw 已有的结构化数据服务、AgentView、Skill、Workflow、MCP 与企业能力市场组合成一套动态软件系统。

目标体验：

- 用户在中间 `应用` 面板中搜索、筛选、点击应用图标。
- 点击后在右侧 tab 打开固定的软件界面。
- 软件界面可以是企业内部系统式交互，也可以是上传文件后自动处理的工具式交互。
- 后台仍由 Agent、Skill、DataSrv、MCP、Workflow 驱动，但前台尽量呈现传统软件操作方式。
- 应用可以从企业能力市场安装，也可以在应用程序工作室中创建、编辑、排序、隐藏、发布。

## 命名

用户可见命名：

- 中间面板：`应用`
- 创建与管理入口：`应用程序工作室`
- 市场：`企业能力市场`
- 应用大类：
  - `企业应用`
  - `工具应用`
  - `自动化应用`
- 市场安装单元：
  - `企业应用包`
  - `技能包`
  - `MCP 服务`

内部类型建议：

```json
{
  "app.type": "enterprise_app | tool_app | automation_app",
  "capability_type": "enterprise_app_pack | skill | mcp"
}
```

兼容说明：

- 现有 MIS / DataSrv 能力在实现层仍可称为 `mis_app` 或 `datasrv_business_action`。
- UI 层不展示 `MIS`，统一使用 `企业应用`。
- `企业应用包` 是安装单元；`企业应用` 是应用面板中的图标入口。

## 核心概念

### 应用

应用是用户可以点击打开的软件入口。

一个应用包含：

- 图标
- 名称
- 描述
- 类型
- 分类
- 标签
- 来源
- 输入界面
- 执行逻辑
- 输出形态
- 权限与安全规则
- 排序和置顶配置

### 企业应用

企业应用面向 OA、ERP、财务、进销存、CRM、HR、审批、报表、台账等企业内部系统场景。

它通常绑定 DataSrv 的能力：

- dataset
- template
- business_action
- business_view
- report
- dashboard
- business_rule
- quality_check

企业应用使用方式：

```text
应用图标 -> 右侧 tab -> 表单/列表/详情/审批/报表 -> dry-run -> 人工确认 -> commit
```

### 工具应用

工具应用是复杂 Skill 的 UI 化。它更像传统桌面小工具或在线处理器。

典型流程：

```text
应用图标 -> 上传文件/填写少量参数 -> 开始处理 -> Agent/Skill 执行 -> 结果预览/下载
```

例如：

- 合同审查
- PDF 转 Word
- 文档脱敏
- 表格分析
- 数据清洗
- OCR 识别
- 会议纪要生成

### 自动化应用

自动化应用面向周期性、批量、监控、同步、采集类任务。

例如：

- 定时网页采集
- 数据同步
- 异常监控
- 批量文件处理
- 周报自动生成

第一版可先定义类型和 UI 位置，具体能力后续逐步实现。

## 应用面板设计

### 布局

应用面板位于 GUI 中间面板，与数字员工等入口并列。

顶部结构：

```text
应用
[搜索应用...] [+]
[全部应用 v]
```

说明：

- 搜索框用于搜索应用名、描述、标签、来源能力、业务流程关键词。
- `+` 按钮进入应用程序工作室。
- 分类使用下拉列表，节省空间。
- 应用列表用图标网格，每行 3 个。

### 分类下拉

使用一个下拉同时表达大类和细分类：

```text
全部应用
常用应用
最近使用

企业应用
  全部企业应用
  财务
  进销存
  CRM
  OA
  HR
  采购
  库存
  销售

工具应用
  全部工具应用
  文档工具
  数据工具
  图像工具
  转换工具

自动化
  全部自动化
  定时任务
  网页采集
  数据同步
  监控提醒
```

应用定义中区分：

```json
{
  "type": "tool_app",
  "category": "文档工具",
  "tags": ["合同", "审查", "风险"]
}
```

### 图标网格

应用列表固定为 3 列图标网格：

```text
[图标] [图标] [图标]
名称   名称   名称

[图标] [图标] [图标]
名称   名称   名称
```

建议尺寸：

- 单元格：72 x 76 px
- 图标容器：40 x 40 px
- 名称最多 2 行，超出省略
- 单元格尺寸固定，hover / loading / badge 不应造成布局跳动

Tooltip 显示：

- 应用名
- 描述
- 来源：企业应用包 / 技能包 / 本地创建
- 状态：可用 / 需配置 / 停用 / 运行中
- 最近使用时间

### 置顶应用

常用应用可 pin 在最上方两排。

规则：

- 最多 6 个置顶应用。
- 每排 3 个，共 2 排。
- 置顶应用使用 `pin_order` 排序。
- 超过 6 个时提示用户先取消一个置顶。
- 搜索状态下不显示单独置顶区，只显示搜索结果。
- 分类筛选状态下，置顶区只显示匹配当前筛选的置顶应用。

示例：

```text
置顶
[报销申请] [合同审查] [采购入库]
[PDF转Word] [表格分析] [库存盘点]

全部应用
[客户建档] [报销审批] [文档脱敏]
```

本地配置：

```json
{
  "installed_apps": {
    "doc.contract_review": {
      "visible": true,
      "enabled": true,
      "pinned": true,
      "pin_order": 20,
      "sort_order": 80
    }
  }
}
```

## 应用程序工作室

应用程序工作室通过应用面板顶部 `+` 进入，在右侧打开 tab。

工作室不是弹窗，应该是可长期停留和编辑的工作区。

首页入口：

```text
应用程序工作室

[创建应用]
[应用管理]
[从市场添加]
```

也可以在首页提供自然语言输入：

```text
描述你想要的应用...
例如：做一个合同审查应用，上传合同后输出风险报告和修订版 Word
```

### 创建企业应用

企业应用创建有两种方式。

#### 对话创建

适合用户没有现成 DataSrv 模型时使用。

流程：

```text
用户描述业务
-> Agent 追问字段、规则、状态、权限、流程
-> Agent 生成应用草案
-> 用户查看字段表、流程、界面预览、权限
-> 测试 dry-run
-> 发布到应用面板
```

Agent 需要总结：

- 业务对象
- 字段
- 必填项
- 唯一性规则
- 状态流
- 权限
- 业务动作
- 列表视图
- 详情视图
- 审批/确认规则
- 报表或统计入口

对话创建输出应先成为草案，不直接写入生产 DataSrv。

#### 从 DataSrv 能力创建

适合已有 `MaClawDataSrv` 能力时使用。

流程：

```text
选择 DataSrv
-> 选择 domain
-> 选择 BusinessAction / BusinessView / Report / Dashboard
-> 自动生成应用定义
-> 用户调整名称、图标、分类、字段顺序、确认规则
-> 测试 dry-run
-> 发布
```

DataSrv 可用能力：

- `GET /api/v1/data/capabilities`
- `POST /api/v1/data/intent/resolve`
- `GET /api/v1/data/business-actions`
- `POST /api/v1/data/business-actions/{id}/execute`
- `GET /api/v1/data/business-views`
- `POST /api/v1/data/reports/{id}/run`
- `POST /api/v1/data/dashboards/{id}/run`

### 创建工具应用

工具应用把 Skill / Workflow / MCP 包装成固定界面。

流程：

```text
选择 Skill / Workflow / MCP
-> 自动读取参数契约
-> 设计上传区和字段
-> 设计输出
-> 试运行
-> 发布
```

工作室配置页：

- 基本信息：名称、图标、分类、描述
- 输入：文件上传、字段、默认值、高级设置
- 执行：绑定 skill、operation、运行方式、失败修复次数
- 输出：摘要、文件、预览、打开方式
- 安全：权限、是否允许写文件、是否需要确认
- 测试发布：样例输入、运行记录、发布按钮

### 应用管理

应用管理负责应用面板显示，而不是运行应用。

功能：

- 添加到应用面板
- 从应用面板移除
- 启用 / 停用
- 置顶 / 取消置顶
- 调整置顶顺序
- 调整普通显示顺序
- 修改图标
- 修改名称
- 修改分类
- 查看来源
- 删除自定义应用
- 卸载来源包

文案要区分：

- `隐藏`：不在应用面板显示，但仍安装。
- `删除自定义应用`：删除本地创建的 app 定义。
- `卸载来源包`：卸载企业应用包或技能包，可能影响多个应用。
- `停用`：保留入口但不可启动。

管理界面：

```text
应用管理

筛选：[全部应用 v]  搜索：[搜索应用...]

置顶应用（最多 6 个）
[☰] [图标] 报销申请      置顶
[☰] [图标] 合同审查      置顶

可见应用
[☰] [图标] PDF 转 Word   文档工具   已显示
[☰] [图标] 表格分析      数据工具   已显示

未显示应用
[+] [图标] 文档脱敏      文档工具   添加
```

排序实现：

- 置顶区使用 `pin_order`
- 普通区使用 `sort_order`
- 应用面板每行 3 个，排序本质是一维顺序
- 工作室可提供 3 列布局预览，拖拽后保存

## 应用定义

统一 schema：

```json
{
  "schema": "maclaw.apps/v1",
  "apps": []
}
```

单个应用定义：

```json
{
  "id": "doc.contract_review",
  "type": "tool_app",
  "name": "合同审查",
  "description": "上传合同文件，输出风险报告和修订版文档。",
  "icon": "file-check",
  "category": "文档工具",
  "tags": ["合同", "审查", "风险"],
  "source": {
    "kind": "skill",
    "skill_name": "document-suite",
    "operation": "contract_review"
  },
  "input": {
    "files": [],
    "fields": []
  },
  "run": {},
  "output": {},
  "security": {},
  "ui": {}
}
```

### 企业应用定义

```json
{
  "id": "finance.expense_submit.app",
  "type": "enterprise_app",
  "name": "报销申请",
  "description": "填写并提交费用报销，提交前进行结构化校验和 dry-run。",
  "icon": "receipt",
  "category": "财务",
  "tags": ["报销", "费用", "审批"],
  "source": {
    "kind": "datasrv_business_action",
    "service": "maclaw-data-srv",
    "action_id": "finance.expense_submit"
  },
  "views": {
    "entry": "form",
    "list": {
      "kind": "datasrv_business_view",
      "view_id": "finance.expense_review"
    },
    "detail": true,
    "review": true
  },
  "transaction": {
    "draft": true,
    "dry_run": true,
    "commit_requires_confirm": true,
    "resume": true
  },
  "permissions": {
    "allowed_actions": ["finance.expense_submit"],
    "allowed_views": ["finance.expense_review"],
    "allowed_datasets": ["finance.expenses"]
  }
}
```

运行流程：

```text
打开应用
-> 拉取 BusinessAction
-> 根据 InputFields 生成表单
-> 保存草稿
-> dry-run
-> 展示校验和预览结果
-> 用户确认 commit
-> 展示结果和审计信息
```

### 工具应用定义

```json
{
  "id": "doc.contract_review",
  "type": "tool_app",
  "name": "合同审查",
  "description": "上传合同，生成风险报告和修订版 Word。",
  "icon": "file-check",
  "category": "文档工具",
  "tags": ["合同", "审查", "Word"],
  "source": {
    "kind": "skill",
    "skill_name": "document-suite",
    "operation": "contract_review"
  },
  "input": {
    "files": [
      {
        "name": "contract",
        "label": "合同文件",
        "accept": ".docx,.pdf",
        "multiple": false,
        "required": true
      }
    ],
    "fields": [
      {
        "name": "focus",
        "label": "审查重点",
        "type": "select",
        "options": ["通用", "付款", "违约", "知识产权"],
        "default": "通用"
      },
      {
        "name": "make_revision",
        "label": "生成修订版",
        "type": "boolean",
        "default": true
      }
    ]
  },
  "run": {
    "mode": "agent",
    "auto_start": false,
    "max_repair_attempts": 1
  },
  "output": {
    "summary": true,
    "files": true,
    "preview": true
  }
}
```

运行流程：

```text
打开应用
-> 上传文件
-> 填少量参数
-> 点击开始
-> 展示步骤进度
-> 输出摘要、预览、文件
```

## 私有扩展标识

### 推荐标识

统一使用：

```text
x_maclaw_apps
```

市场预览字段：

```json
{
  "x_maclaw_has_apps": true,
  "x_maclaw_app_count": 3,
  "x_maclaw_app_types": ["tool_app"],
  "x_maclaw_apps_preview": [
    {
      "id": "doc.contract_review",
      "name": "合同审查",
      "type": "tool_app",
      "category": "文档工具",
      "icon": "file-check"
    }
  ]
}
```

完整定义放在安装包内：

```text
maclaw.apps.json
```

理由：

- 不破坏现有 Skill manifest。
- 不要求所有 Skill 都升级结构。
- 企业能力市场可以先展示预览。
- 安装后再读取完整应用定义。

### Skill 包结构

```text
document-suite/
  SKILL.md
  maclaw.apps.json
  scripts/
  references/
```

安装 Skill 后：

1. 读取 `maclaw.apps.json`
2. 校验 `schema == maclaw.apps/v1`
3. 校验 app source 指向当前 skill
4. 校验图标 key、类型、输入字段
5. 写入本地应用注册表
6. 应用面板显示图标

### 企业应用包

企业应用包来自企业能力市场，安装后写入 DataSrv 或绑定已有 DataSrv 能力。

市场卡片：

```json
{
  "capability_type": "enterprise_app_pack",
  "capability_id": "inventory-suite",
  "display_name": "进销存套件",
  "description": "采购、销售、库存的企业应用包。",
  "metadata_json": {
    "x_maclaw_has_apps": true,
    "x_maclaw_app_count": 4,
    "x_maclaw_apps_preview": [
      {
        "id": "inventory.stock_in.app",
        "type": "enterprise_app",
        "name": "采购入库",
        "icon": "warehouse",
        "category": "进销存"
      }
    ]
  }
}
```

安装时：

```text
安装进销存套件

将添加以下应用到应用面板：
[x] 采购入库
[x] 销售出库
[x] 库存盘点
[ ] 库存预警
```

## 图标系统

应用图标使用预置 SVG 图标 key。第一版不要求用户上传自定义图标。

若项目已有 lucide 图标库，优先使用 lucide，保持统一风格。

应用定义只保存字符串：

```json
{
  "icon": "receipt"
}
```

前端维护注册表：

```ts
const APP_ICON_REGISTRY = {
  receipt: Receipt,
  wallet: Wallet,
  invoice: FileText,
  package: Package,
  fileCheck: FileCheck,
  bot: Bot
}
```

预置图标建议：

企业应用：

- `receipt`：报销/费用
- `wallet`：付款/财务
- `invoice`：发票
- `calculator`：核算
- `chart`：报表
- `dashboard`：仪表盘
- `users`：客户/人员
- `user-plus`：建档/入职
- `briefcase`：商机/项目
- `shopping-cart`：采购
- `package`：库存
- `warehouse`：仓储
- `truck`：物流
- `clipboard-check`：审批
- `calendar-check`：考勤/日程
- `building`：组织/部门
- `file-lock`：合同/法务

工具应用：

- `file-text`：文档处理
- `file-check`：文档审查
- `file-output`：文件转换
- `scan-text`：OCR
- `table`：表格
- `chart-line`：数据分析
- `database`：数据处理
- `eraser`：脱敏/清洗
- `sparkles`：AI 生成
- `languages`：翻译
- `image`：图片处理

自动化：

- `bot`：自动化 Agent
- `clock`：定时任务
- `refresh-cw`：同步
- `globe`：网页采集
- `bell`：监控/提醒
- `workflow`：流程
- `zap`：快速执行

默认规则：

- `enterprise_app` 默认 `app-window`
- `tool_app` 默认 `file-text`
- `automation_app` 默认 `bot`
- 财务默认 `receipt`
- 进销存默认 `package`
- 文档工具默认 `file-text`
- 数据工具默认 `table`
- 审批默认 `clipboard-check`

## 右侧 Tab 运行界面

点击应用图标后在右侧创建应用 tab。

Tab 标题：

```text
报销申请
合同审查
采购入库
```

若同一应用多开：

```text
报销申请 · 草稿 2
合同审查 · report.pdf
```

### 企业应用 Tab

推荐布局：

```text
顶部：标题、状态、保存时间、操作菜单
主体：表单 / 列表 / 详情 / 审批 / 报表
底部：保存草稿、校验、提交、取消
侧栏：Agent 辅助，可折叠
```

Agent 辅助不应抢主界面。默认折叠或窄栏展示：

- 自动补全
- 解释校验错误
- 从附件提取字段
- 对比历史记录
- 总结提交影响

### 工具应用 Tab

推荐布局：

```text
顶部：应用标题、状态
中间：上传区、参数区
底部：开始处理
运行中：步骤进度
完成后：摘要、预览、文件列表
```

失败时：

- 展示可读错误
- 提供重试
- 若允许自动修复，展示修复尝试次数
- 保留输入文件和参数

## 本地应用注册表

建议在 AppConfig 中新增应用相关配置，或独立存储到 `apps.json`。

推荐独立存储，避免主配置膨胀：

```json
{
  "schema": "maclaw.installed_apps/v1",
  "items": {
    "doc.contract_review": {
      "app_id": "doc.contract_review",
      "enabled": true,
      "visible": true,
      "pinned": true,
      "pin_order": 10,
      "sort_order": 40,
      "display_name_override": "",
      "icon_override": "",
      "category_override": "",
      "source": {
        "kind": "skill",
        "ref": "document-suite",
        "version": "1.0.0",
        "installed_from": "enterprise_hub"
      },
      "last_used_at": "",
      "use_count": 0
    }
  }
}
```

原则：

- 原始 app 定义来自来源包。
- 本地注册表保存用户个性化展示配置。
- 来源包升级后，本地覆盖项继续生效。
- 来源缺失时应用显示 `需重新安装` 或自动隐藏。

## 与现有能力的关系

### AgentView

现有 AgentView 可作为应用运行界面的底座：

- 表单字段渲染
- submit / dismiss
- schemaVersion
- skill 参数补齐
- MIS dry-run / commit review
- workflow form

需要扩展：

- app tab owner
- app session lifecycle
- tool_app 文件上传输入
- app-level progress
- app 输出文件索引

### DataSrv

DataSrv 是企业应用的主要后端。

第一版优先包装：

- `BusinessAction -> 表单应用`
- `BusinessView -> 列表应用`
- `Report -> 报表应用`
- `Dashboard -> 仪表盘应用`

### Skill

Skill 保持 Skill。工具应用是 Skill 暴露的 UI 入口。

安装 Skill 不等于所有 App 都必须显示。安装弹窗和应用管理允许选择。

### 企业能力市场

市场仍以能力为安装单元：

- 企业应用包
- 技能包
- MCP 服务

市场卡片通过 `metadata_json.x_maclaw_*` 展示是否包含应用。

## 建议实现接口

第一版可先在 GUI 后端提供 Wails 方法，前端不直接读写文件。

应用发现：

```go
ListApps(filter AppListFilter) ([]AppSummary, error)
GetAppDefinition(appID string) (*AppDefinition, error)
```

应用面板配置：

```go
UpdateAppVisibility(appID string, visible bool) error
PinApp(appID string, pin bool) error
ReorderApps(items []AppOrderUpdate) error
UpdateAppDisplay(appID string, patch AppDisplayPatch) error
```

应用运行：

```go
OpenAppSession(appID string, input OpenAppSessionInput) (*AppSession, error)
SubmitAppSession(sessionID string, data map[string]any) (*AppRunResult, error)
CancelAppSession(sessionID string) error
GetAppSession(sessionID string) (*AppSession, error)
```

应用工作室：

```go
CreateAppDraft(input CreateAppDraftInput) (*AppDraft, error)
UpdateAppDraft(draftID string, patch AppDraftPatch) (*AppDraft, error)
TestAppDraft(draftID string, input map[string]any) (*AppRunResult, error)
PublishAppDraft(draftID string) (*AppSummary, error)
DeleteCustomApp(appID string) error
```

市场和安装：

```go
ListMarketplaceAppPacks(query string) ([]CapabilityWithApps, error)
InstallCapabilityApps(capabilityRef string, appIDs []string) error
ScanSkillApps(skillName string) ([]AppDefinition, error)
```

建议事件：

```text
app:registry_updated
app:session_created
app:session_progress
app:session_completed
app:session_failed
app:session_output_ready
```

## 数据流

### 应用面板加载

```text
前端进入应用面板
-> ListApps
-> 合并来源定义 + 本地注册表
-> 按 pinned/pin_order/sort_order 排序
-> 应用搜索和分类筛选
-> 渲染 3 列图标网格
```

### Skill App 安装后出现图标

```text
安装 Skill
-> 扫描 skillDir/maclaw.apps.json
-> 校验 app 定义
-> 写入 app definition cache
-> 写入 installed_apps 默认可见项
-> 发出 app:registry_updated
-> 应用面板刷新
```

### 企业应用打开

```text
点击企业应用
-> OpenAppSession
-> 根据 source.kind 拉取 DataSrv BusinessAction/View/Report/Dashboard
-> 构造 AgentView 或 AppView
-> 右侧创建 tab
-> 用户提交
-> dry-run
-> commit review
-> commit
-> 记录 session、事务、审计
```

### 工具应用打开

```text
点击工具应用
-> OpenAppSession
-> 渲染上传区和参数
-> 用户开始处理
-> 上传文件进入临时 artifact
-> run_skill / workflow / MCP 调用
-> app:session_progress 推送步骤
-> 输出文件进入 artifact/output store
-> 前端展示预览和下载入口
```

## 校验规则

应用定义必须校验：

- `id` 非空，且全局唯一。
- `type` 属于 `enterprise_app | tool_app | automation_app`。
- `name` 非空。
- `icon` 必须存在于预置图标注册表，缺失时使用默认图标。
- `source.kind` 必须与应用类型匹配。
- `tool_app` 的文件输入必须声明 `accept` 和 `required`。
- `enterprise_app` 绑定 DataSrv action 时必须能解析到 `BusinessAction`。
- `enterprise_app` 含 commit 操作时默认 `commit_requires_confirm=true`。
- 市场安装的 app 不能越权声明 DataSrv 权限，只能请求已授权 scope。
- 自定义 app 删除只允许删除本地定义，不得误删来源包。

## 安全与权限

企业应用：

- 写入类动作必须先 dry-run。
- commit 默认需要人工确认。
- 敏感字段遵循 DataSrv API key policy。
- 权限不足时应用可显示，但状态为 `需授权` 或 `不可用`。
- 事务记录必须包含 action_id、dataset_id、dry_run 结果、commit 结果、操作者。

工具应用：

- 文件上传进入受控临时目录。
- Skill 写文件时复用现有安全策略和审批机制。
- 输出文件必须明确来源 app/session。
- 自动修复次数有限，默认 1 次。
- 涉及外网、敏感文件、批量写入时沿用现有风险评估。

应用市场：

- `x_maclaw_apps` 只作为声明，不等于自动信任。
- 安装时必须校验来源包签名/来源策略。
- 企业策略禁止非企业市场安装时，带 app 的 skill 也必须被拦截。
- 管理员可允许安装包但隐藏部分 app。

## 实施阶段

### Phase 1：应用面板与本地应用注册表

范围：

- 新增 `应用` 面板入口。
- 顶部搜索、创建按钮、分类下拉。
- 3 列图标网格。
- tooltip。
- pin 前两排，最多 6 个。
- 本地应用注册表。
- 应用程序工作室打开右侧 tab。
- 应用管理：显示/隐藏、置顶、排序、改图标、改分类。

可用本地 mock app 验证 UI。

### Phase 2：Skill App

范围：

- 支持 skill 目录 `maclaw.apps.json`。
- 安装 skill 后扫描应用定义。
- 应用面板显示工具应用。
- 点击打开工具应用 tab。
- 支持文件上传、参数表单、运行 skill、展示进度与输出。
- 支持应用工作室创建 Skill App。

优先样例：

- 合同审查
- PDF 转 Word
- 文档脱敏

### Phase 3：Enterprise App / DataSrv App

范围：

- 从 DataSrv capabilities 创建企业应用。
- BusinessAction 表单渲染。
- dry-run 和 commit review。
- BusinessView 列表页。
- Report / Dashboard 结果页。
- 应用工作室对话式创建草案。

优先样例：

- 报销申请
- 客户建档
- 采购入库

### Phase 4：企业能力市场联动

范围：

- 市场支持 `enterprise_app_pack`。
- `metadata_json.x_maclaw_apps_preview` 展示应用预览。
- 安装弹窗选择要添加的应用。
- 管理员可统一部署 / 推荐 / 隐藏应用。
- inventory 上报已安装应用。

### Phase 5：自动化应用

范围：

- 定时任务 / 监控 / 同步类应用。
- 与 scheduler、workflow、MCP 结合。
- 支持运行历史、暂停、恢复、提醒。

## 关键边界

- 应用面板只负责找应用和启动应用，不承载复杂管理。
- 应用程序工作室负责创建、添加、隐藏、排序、删除。
- Skill 仍是能力包，App 是 UI 入口。
- 企业应用包是市场安装单元，企业应用是应用面板入口。
- DataSrv capability 是内部资源，不直接暴露为用户命名。
- 使用阶段尽量软件化，不把所有操作变成聊天。
- 创建阶段可以对话式，由 Agent 总结成 app 定义。

## 开放问题

- 企业应用包是否需要独立包格式，还是先通过市场 metadata + DataSrv bootstrap API 实现。
- 自定义应用定义是否允许导出为技能包或企业应用包。
- 应用运行历史应归属现有 session store，还是新建 app session store。
- 文件上传结果应复用 artifact 体系，还是为 tool_app 建独立 output store。
- 移动端是否显示完整应用面板，还是仅显示常用和最近。
