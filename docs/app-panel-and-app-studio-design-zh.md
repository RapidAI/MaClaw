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

来源第一版拆成 `DataSrv`、`Skill`、`市场`、`本地`、`内置`。应用程序工作室创建的应用和从已有应用复制出的变体都标记为 `本地`；复制变体仍保留原 DataSrv / Skill 绑定，只改变面板入口定义。

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
[搜索应用... (x)] [+]
[全部应用 v]
```

说明：

- 搜索框用于搜索应用名、描述、分类、类型、来源、来源能力、业务流程关键词、DataSrv 绑定能力、Skill id、输入模式、输出格式和结构化字段。
- 搜索框有输入时显示清空按钮；按 `Esc` 也清空搜索。搜索状态下标题显示 `搜索结果`，不单独展示常用应用区。顶部提供 `重置筛选`，可一次清空搜索和分类。
- `+` 按钮进入应用程序工作室。
- 分类使用下拉列表，节省空间；选项显示数量，例如 `文档处理 (3)`、`最近使用 (2)`。有搜索词时，分类数量按当前搜索结果重算，0 匹配分类置灰禁用；如果当前分类被搜索条件过滤到 0，自动回到 `全部应用`，避免空列表困住用户。筛选控件下方显示轻量摘要，例如 `搜索“脱敏” · 1 个匹配`、`文档处理 · 2 个应用`，让用户知道当前列表是被什么条件收窄的。
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
- 左上角状态点表达运行/依赖状态，右上角 pin 点表达常用固定；两个点都不能改变单元格尺寸。
- 图标按钮支持键盘访问：`Tab` 进入网格后，方向键按 3 列布局移动焦点，`Home` / `End` 跳到首尾。
- 图标按钮必须有明确 `aria-label`，包含应用名称、应用类型、来源和当前状态；状态变化时同步更新。

Tooltip 显示：

- 应用名
- 描述
- 来源：企业应用包 / 技能包 / 本地创建
- 状态：可用 / 需配置 / 停用 / 运行中
- 最近使用时间

当前第一版 tooltip 已包含应用名、描述、类型/来源、状态和最近使用时间。状态已经接入基础运行态：打开右侧 tab 后显示 `运行中`；DataSrv 应用按能力发现状态显示 `读取中`、`未启用`、`不可用` 或 `可用`。同样状态会通过图标左上角小圆点可视化。后续接入权限和依赖配置后可继续细分。

### 置顶应用

常用应用可 pin 在最上方两排。

规则：

- 最多 6 个置顶应用。
- 每排 3 个，共 2 排。
- 置顶应用使用 `pin_order` 排序。
- 超过 6 个时提示用户先取消一个置顶；应用管理页在满 6 个时禁用其他应用的 `置顶` 按钮，并用 tooltip 说明原因。
- 从市场安装、DataSrv/Skill 发现添加、恢复隐藏应用时，如果新应用声明 `pinned=true` 但当前常用已满 6 个，前端自动降级为不置顶，保持两排上限不被 manifest 绕过。
- 搜索状态下不显示单独置顶区，只显示搜索结果。
- 分类筛选状态下，置顶区只显示匹配当前筛选的置顶应用；已经出现在置顶区的应用不在下方主网格重复显示。
- 置顶区存在时，下方主网格标题使用 `其他应用`，表示已排除上方常用应用。
- `最近使用` 是时间排序视图，不显示单独置顶区，避免打散最近使用顺序。

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
      "sort_order": 80,
      "recent_used_at": "1718520000000-000001"
    }
  }
}
```

当前 GUI 实现使用 `maclaw:apps-panel:v1` localStorage 保存面板布局：

- `orderedIds`：应用显示顺序。
- `pinnedIds`：常用应用，最多 6 个。
- `hiddenIds`：被隐藏的内置应用。
- `editedApps` / `customApps`：本地编辑和本地创建应用。
- `recentUsedAtById`：最近使用时间戳。点击打开应用、在运行页执行应用都会刷新；分类下拉选择 `最近使用` 时只显示有时间戳的应用，并按时间倒序排列。

读取旧版本缓存时要做轻量迁移：`customApps` 中 `id` 以 `local-app-` 开头但来源仍是旧值 `market` 的应用，统一迁移为 `local`；缺失或未知来源的自定义应用也按 `local` 处理，避免旧缓存导致应用管理和 tooltip 崩溃。

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

工作室 tab 使用标准 tab 语义：

- 外层 `role="tablist"`。
- 三个入口按钮使用 `role="tab"`，通过 `aria-selected` 表达当前页。
- 内容区使用 `role="tabpanel"`，通过 `aria-labelledby` 绑定当前 tab。
- 支持 `ArrowLeft` / `ArrowRight` / `Home` / `End` 键盘切换。

也可以在首页提供自然语言输入：

```text
描述你想要的应用...
例如：做一个合同审查应用，上传合同后输出风险报告和修订版 Word
```

创建页下方的三张类型卡片不是纯说明区，而是快捷 preset：

- `应用程序`：切到 `enterprise_app`，默认分类 `OA`，默认图标 `sheet`，Manifest 使用 `agent_dynamic_ui`。
- `工具应用`：切到 `tool_app`，默认分类 `文档处理`，默认图标 `shield`，默认文件上传和 `docx/pdf` 输出，Manifest 使用 `fixed_skill_ui`。
- `自动化应用`：切到 `automation_app`，默认分类 `自动化`，默认图标 `sync`，Manifest 使用 `automation_console`。
- 直接修改类型下拉只改变类型和默认颜色，不覆盖用户已经填写的分类、图标、输入输出；点击卡片才应用完整 preset。

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

当前实现约定：

- Skill 扩展文件：`docs/schemas/maclaw.apps.schema.json`
- 单应用安装 Manifest：`docs/schemas/maclaw.app.schema.json`
- 应用包安装 Manifest：`docs/schemas/maclaw.app.pack.schema.json`
- 示例文件：`docs/examples/maclaw.apps.json`、`docs/examples/maclaw.app.json`、`docs/examples/maclaw.app.pack.json`
- Skill 扩展使用字段 `x_maclaw_apps: "v1"`。
- 单应用/应用包安装 Manifest 使用字段 `privateMarker: "x_maclaw_apps"`。

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
2. 校验 `x_maclaw_apps == "v1"`
3. 校验 app source 指向当前 skill
4. 校验图标 key、类型、输入字段、输出格式
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

当前 GUI 第一版实现的可选图标：

- `receipt`：报销/费用
- `wallet`：付款/财务
- `invoice`：发票/票据
- `warehouse`：仓储/入库
- `inventory`：库存/盘点
- `customer`：客户/建档
- `users`：人员/组织
- `contract`：合同/法务
- `pdf`：PDF/转换
- `shield`：审查/脱敏
- `sheet`：表格/数据
- `chart`：报表/分析
- `dashboard`：看板/指标
- `database`：数据库/处理
- `eraser`：清洗/脱敏
- `truck`：物流/配送
- `calendar`：日程/考勤
- `web`：网页/采集
- `sync`：同步/自动化
- `bot`：Agent/自动化

应用程序工作室中的图标选择器显示图标 SVG，并在 tooltip / aria-label 中显示“语义名称 + icon key”，例如 `合同/法务 (contract)`，方便用户选择，也方便 manifest 调试。

图标颜色使用固定企业色板，不开放任意颜色输入。创建应用和编辑应用都可设置 `panel.accent`，应用面板、运行 tab、市场安装预览都按该颜色渲染图标。当前色板：

- `#2f5f98`：蓝色
- `#657a42`：绿色
- `#7c3f58`：紫红
- `#b45309`：琥珀
- `#28705f`：青绿
- `#4b6572`：灰蓝
- `#8a5a44`：棕色
- `#5b5ea6`：靛蓝
- `#6b7280`：中性灰

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

Tab 交互规则：

- tablist 使用 `role="tablist"`，每个应用 tab 使用 `role="tab"`。
- 当前 tab 设置 `aria-selected="true"`，右侧内容区使用 `role="tabpanel"`，并通过 `aria-labelledby` 指回当前 tab。
- 关闭按钮是独立按钮，不嵌套在 tab 按钮内，避免键盘和读屏焦点混乱。
- `ArrowLeft` / `ArrowRight` 在已打开应用间切换，`Home` / `End` 跳到首尾 tab。
- 点击已打开应用图标时激活已有 tab，不重复打开。

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
- `最近使用` 分类筛选：打开应用或执行应用后写入最近使用时间，重启后仍可恢复。
- 本地应用注册表。
- 应用程序工作室打开右侧 tab。
- 应用管理：显示/隐藏、置顶、排序、改图标、改分类。
- 应用管理：支持搜索和分类筛选，方便应用较多时定位；筛选同时作用于已安装列表和已隐藏列表，只影响可见项，不改变真实排序。筛选状态下显示当前条件摘要和匹配数量，并禁用上移/下移/移到顶部/移到底部，提示用户清空筛选后再排序，提供 `重置筛选` 按钮一次清空搜索和分类。
- 应用管理：排序操作支持 `上移`、`下移`、`移到顶部`、`移到底部`，用于快速安排应用面板图标显示位置。
- 应用管理：支持 `复制应用`，从已有应用克隆出一个本地副本，名称追加 `副本`，来源标为 `本地`，默认不置顶，保留原 DataSrv / Skill 绑定，方便基于相似能力创建变体。连续复制同一应用时自动编号为 `副本 2`、`副本 3`，避免同名混淆。复制后自动打开副本编辑表单，用户可立即改名、改分类、改图标和颜色。
- 应用管理：内置应用使用 `隐藏`，进入 `已隐藏应用` 列表并可恢复；用户创建、复制或市场安装的本地应用使用 `移除`，从面板删除，不进入内置恢复列表。移除/隐藏应用时同步清理该应用在 `maclaw:apps-run-history:v1` 中的本地运行历史，避免后续重装同 id 时看到旧记录。

可用本地 mock app 验证 UI。

### Phase 2：Skill App

范围：

- 支持 skill 目录 `maclaw.apps.json`。
- 安装 skill 后扫描应用定义。
- 应用面板显示工具应用。
- 点击打开工具应用 tab。
- 支持文件上传、参数表单、运行 skill、展示进度与输出。
- 支持应用工作室创建 Skill App。
- 应用工作室的创建页和管理页都支持编辑 `tool_app` 结构化字段：
  - 字段属性：`name`、`label`、`type(text/select/boolean)`、`required`、`default`、`options`。
  - `inputMode=form/mixed` 时显示字段编辑器；`file` 模式只显示文件输入。
  - `multiple_files=true` 时文件输入允许一次选择多个文件；默认仍是单文件。
  - 保存应用时必须保留并导出 `binding.skill.fields`。
- 工具应用运行 tab 按 manifest 渲染字段表单：
  - `text/select` 的 `required=true` 必须填值后才能执行。
  - `boolean` 始终有明确 true/false 值，不阻塞执行。
  - 文件输入模式下没有文件时给出内联校验提示。
- 工具应用执行桥接现有 Skill Runner：
  - 点击执行时调用 `RunNLSkillAsync(skillID, runArgs)`。
  - `runArgs` 包含 `_maclaw_app`、`app_id`、`app_name`、`input_mode`、`output_mode`、`params`、`fields`、`file`、`files`、`file_name`、`prompt`。
  - 文件输入先调用 `StageSkillAppInputFile(fileName, mimeType, lastModified, contentBase64)` 写入 MaClaw 临时目录，再启动 skill。
  - `StageSkillAppInputFile` 返回 `file` 交接对象：`name`、`size`、`type`、`last_modified`、`staged_path`、`transfer=staged_file`。
  - 启动 skill 时同时提供扁平路径别名：`file_path`、`input_file_path`、`local_file_path`、`uploaded_file_path`，方便 skill.yaml 使用 `{{file_path}}` 直接读取首个文件。
  - 多文件运行额外提供 `files` 和 `file_paths`；旧 skill 仍可用首个文件的 `file/file_path`。
  - 当前 staging 上限为 25MB；超过上限前端阻止执行并提示用户。后续大文件应改为流式 artifact/file store。
  - Skill Runner 在 run 进入终态后清理 `~/.maclaw/temp/app-inputs` 下对应 staged 输入文件；越界路径拒绝删除，并写入 run warning。
  - 如果 skill run 未能启动（例如找不到 skill、策略拒绝、precheck 失败），`RunNLSkillAsync` 会 best-effort 清理本次 staged 输入文件。
  - 如果 `RunNLSkillAsync` 返回空 run id，也按启动失败处理：运行页显示失败，写入本地运行历史，不进入无法轮询和无法取消的 running 状态。
  - 每次 stage 新输入文件前会清扫 24 小时前残留的 `app-inputs/input-*` 目录，用于覆盖崩溃或异常退出后的残留。
  - 小文本文件可附带 `file_text` 作为轻量预览；正式大文件/二进制文件应继续走 artifact/file store 管线。
  - 返回 run id 后进入 `running` 状态，并轮询 `GetNLSkillRunStatus(runID)`。
  - `success/completed/done` 映射为完成，`failed/error/timeout` 映射为失败，`cancelled/canceled` 映射为取消。
  - 运行中显示当前 step、session progress 或 run id；完成后显示输出摘要。
  - 运行页必须展示执行证据区：最多显示最近 6 个 `steps`，展示 step 名称、状态、耗时和短输出/错误。
  - 如 `SkillRunStatus.summary.artifact_path`、`artifact_status`、`expected_artifact` 或 `needs_artifact_verification` 存在，运行页展示产物状态；有真实路径时显示路径，并提供 `打开` 与 `定位` 操作，分别调用 `OpenFileOrShowInFolder(path)` 与 `ShowItemInFolder(path)`。后续可替换为 artifact 链接。
  - 运行中允许调用 `CancelNLSkillRun(runID)` 取消。
  - 每个应用保留最近 8 条本地运行历史，存储 key 为 `maclaw:apps-run-history:v1`。
  - 历史项记录 run id、终态、输出格式、输入摘要、消息摘要、产物路径和时间，展示在运行页结果区下方；有产物路径时同样提供 `打开` 与 `定位`。
  - 用户可在单个应用运行页清空该应用历史，不影响其他应用。
- `maclaw.apps.json`、`maclaw.app.v1`、`maclaw.app.pack.v1` 三种格式都支持同一套字段定义。
- 市场安装页必须在安装前做轻量结构校验：
  - `maclaw.app.v1` 必须包含 `schema=maclaw.app.v1`、`privateMarker=x_maclaw_apps`、`app.id`、`app.name`。
  - `maclaw.app.pack.v1` 必须包含 `privateMarker=x_maclaw_apps` 和非空 `apps`，并逐个校验包内 `maclaw.app.v1`。
  - `maclaw.apps.json` 必须包含 `x_maclaw_apps=v1` 和非空 `apps`，每个 app 至少有合法 `id` 和 `name`。
  - 应用名称可以是中文；manifest `app.id` / `apps[].id` 必须保持 ASCII 标识符，匹配 `^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`。本地创建中文应用时用 `local-app-<time>-app` 兜底，避免导出后无法安装。
  - `app.kind` 与 `launchMode` 必须匹配：`enterprise_app -> agent_dynamic_ui`，`tool_app -> fixed_skill_ui`，`automation_app -> automation_console`。显式写错时阻止安装。
  - `enterprise_app` 必须带 `binding.datasrv.domain`，代表 DataSrv/MIS 动态界面能力；`tool_app` 必须带 `binding.skill.id`，且 `installUnit` 必须是 `skill`。这样企业动态应用和 Skill UI 化应用在市场包里不会混用。
  - Skill App 的 `output_modes` / `binding.skill.outputModes` 只能使用 `docx`、`xlsx`、`pdf`、`json`、`txt`；字段类型只能是 `text`、`select`、`boolean`。非法值必须阻止安装并显示具体路径，不能静默降级。
  - 错误提示要指向具体路径，例如 `maclaw.app.pack.v1 apps[0] privateMarker must be x_maclaw_apps`。
  - 安装预览做重复识别时，`foo` 与 `market-foo` 视为同一个安装身份。这样市场包不能绕过前缀再安装一份已内置或已安装的同名应用。
  - 安装预览工具条同时显示 `已选/总数`、`可安装 N` 和 `将跳过 N`，让用户在全选、全不选、重复包场景下直接知道实际安装结果。
  - 安装预览中不可安装项必须显示跳过原因，例如 `已安装`、`重复应用`、`未选择`；行 hover 和 checkbox aria-label 也要包含相同原因，方便键盘和读屏用户理解。
- 后端扫描 `maclaw.apps.json` 时要先规范化字段：
  - 空 `name` 字段丢弃。
  - 未知 `type` 降级为 `text`。
  - `select.options` 去空、去重，且默认值自动并入候选项。
  - `boolean.default` 统一为布尔值，忽略 `options`。
  - 空 `label` 自动使用 `name`，保证运行界面总有可见标签。

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
