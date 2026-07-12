# MaClaw 应用面板与应用程序工作室设计

> Related supplement: app panel operations area and global approval instance managemenredesign: [app-panel-approval-ops-redesign-zh.md](app-panel-approval-ops-redesign-zh.md).


## 目标

MaClaw 需要在 GUI 中间面板新增一�?`应用` 面板，用图标化方式承载可复用的软件入口。这个面板不是传统工具列表，也不是简�?Skill 快捷方式，而是�?MaClaw 已有的结构化数据服务、AgentView、Skill、Workflow、MCP 与企业能力市场组合成一套动态软件系统�?
目标体验�?
- 用户在中�?`应用` 面板中搜索、筛选、点击应用图标�?- 点击后在右侧ab 打开固定的软件运行界面；右侧不是设计器，不默认展�?manifest、绑定能力、安装单元等定义信息�?- 软件界面可以是企业内部系统式交互，也可以是上传文件后自动处理的工具式交互�?- 后台仍由 Agent、Skill、DataSrv、MCP、Workflow 驱动，但前台尽量呈现传统软件操作方式�?- 应用可以从企业能力市场安装，也可以在应用程序工作室中创建、编辑、排序、隐藏、发布�?
## 命名

用户可见命名�?
- 中间面板：`应用`
- 创建与管理入口：`应用程序工作室`
- 市场：`企业能力市场`
- 应用大类�?- `企业应用`
- `工具应用`
- `自动化应用`
- 市场安装单元�?- `企业应用包`
- `技能包`
- `MCP 服务`

内部类型建议�?
```json
{
"app.type": "enterprise_app |ool_app | automation_app",
"capability_type": "enterprise_app_pack | skill | mcp"
}
```

兼容说明�?
- 现有 MIS / DataSrv 能力在实现层仍可称为 `mis_app` �?`datasrv_business_action`�?- UI 层不展示 `MIS`，统一使用 `企业应用`�?- `企业应用包` 是安装单元；`企业应用` 是应用面板中的图标入口�?
## 核心概念

## 端到端闭环总览

应用系统要闭环，不应只解决“面板里出现一个图标”。完整链路是�?
```text
需�?现有能力
-> 应用草案
-> 本地测试
-> 打包上传
-> 市场审核
-> 企业发布/推荐/下发
-> 用户安装
-> 应用面板展示
-> 右侧ab 运行
-> 产物/事务/审计
-> 评分、反馈、升级、下�?```

这个链路中有三类对象�?
- `App Definition`：应用入口和运行契约，决定图标、分类、界面、绑定能力和权限边界�?- `Capability Package`：市场安装单元，可能�?Skill、企业应用包、MCP 或组合包�?- `Installed App`：用户或企业策略安装后的本地实例，保存显示顺序、置顶、隐藏、最近使用、运行历史和本地覆盖项�?
不要�?DataSrv capability、Skill、MCP 直接等同于应用。它们是能力；应用是面向用户的固定入口、界面和使用习惯�?
### 角色

- 普通用户：搜索、安装、使用、置顶、隐藏、反馈应用�?- 应用创建者：在应用程序工作室中创建草案、测试、上传�?- 企业管理员：审核、发布、推荐、下发、撤回应用�?- 市场审核员：对外部上传包做安全、版权、可移植、运行证据、UI 契约审核�?- Agent/runtime：生成草案、补齐字段、执�?dry-run、运�?skill、输出审计证据�?
### 状态机

应用定义状态：

```text
draft
-> local_tested
-> submied
-> review_failed
-> approved
-> published
-> deprecated
-> revoked
```

安装实例状态：

```text
available
-> needs_config
-> disabled
-> running
-> failed
-> update_available
-> removed
```

状态含义：

- `draft`：只在创建者本机或草稿库可见�?- `local_tested`：已通过至少一次本地试运行�?DataSrv dry-run�?- `submied`：已上传企业能力市场，等待审核�?- `review_failed`：审核未通过，保留错误、证据和修复建议�?- `approved`：审核通过但未公开�?- `published`：市场可发现，可被安装、推荐或下发�?- `deprecated`：不推荐新装，已安装用户可继续使用�?- `revoked`：撤回，默认从面板隐藏或禁用，保留审计�?- `needs_config`：能力已安装但缺�?token、DataSrv 连接、MCP secre或管理员授权�?
### 闭环原则

- 创建必须落到可校�?manifest，不只是一段聊天总结�?- 上传必须带运行证据和权限声明，不只传 zip�?- 审核必须产出结构化结论，可被市场和客户端展示�?- 安装必须可选择应用，不要求整个包的所�?app 都显示�?- 使用必须有产物、事务或审计记录，方便复盘�?- 升级必须保护用户本地排序、置顶、图标、分类覆盖�?- 下架必须有客户端行为：隐藏、禁用、保留历史、提示原因�?
### 应用

应用是用户可以点击打开的软件入口�?
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
- 输出形�?- 权限与安全规�?- 排序和置顶配�?
来源第一版拆�?`DataSrv`、`Skill`、`市场`、`本地`、`内置`。应用程序工作室创建的应用和从已有应用复制出的变体都标记�?`本地`；复制变体仍保留�?DataSrv / Skill 绑定，只改变面板入口定义�?
### 企业应用

企业应用面向 OA、ERP、财务、进销存、CRM、HR、审批、报表、台账等企业内部系统场景�?
它通常绑定 DataSrv 的能力：

- dataset
-emplate
- business_action
- business_view
- report
- dashboard
- business_rule
- quality_check

企业应用使用方式�?
```text
应用图标 -> 右侧ab -> 表单/列表/详情/审批/报表 -> dry-run -> 人工确认 -> commit
```

### 工具应用

工具应用是复�?Skill �?UI 化。它更像传统桌面小工具或在线处理器�?
典型流程�?
```text
应用图标 -> 上传文件/填写少量参数 -> 开始处�?-> Agent/Skill 执行 -> 结果预览/下载
```

例如�?
- 合同审查
- PDF �?Word
- 文档脱敏
- 表格分析
- 数据清洗
- OCR 识别
- 会议纪要生成

### 自动化应�?
自动化应用面向周期性、批量、监控、同步、采集类任务�?
例如�?
- 定时网页采集
- 数据同步
- 异常监控
- 批量文件处理
- 周报自动生成

第一版可先定义类型和 UI 位置，具体能力后续逐步实现�?
## 应用面板设计

### 设计原则

应用面板是高频入口，不是宣传页。视觉应安静、密度适中、可扫读�?
原则�?
- 第一屏直接是可用应用，不放欢迎页和大幅说明�?- 顶部只保留搜索、分类、应用程序工作室入口；工作室入口必须是可见的图标+文字按钮，不能只做一个不明显�?`+`�?- 图标统一风格、统一尺寸，只显示图标和文字；状态放�?tooltip / aria-label，不在图标格角上额外放点�?- 状态文案稳定：`可用`、`运行中`、`需配置`、`不可用`、`已停用`、`有更新`�?- 操作路径短：搜索 -> 点击 -> 右侧ab 打开；管�?-> 工作�?-> 保存�?- 未点击应用前，右侧显示轻量空态，只提示用户选择左侧应用，不自动打开第一个应用�?- 不用弹窗承载主要流程；创建、运行、审核都在右�?tab 或工作区中完成�?- 空状态要给下一步：没有应用时显�?`从市场添加` �?`创建应用`；筛选为空时显示 `重置筛选`�?- 错误要能修：`DataSrv 未启用` 旁边�?`去设置`；`缺少 Skill` �?`重新安装`；`缺少 secret` �?`配置`�?
推荐视觉�?
- 背景使用现有产品 surface，不新增大面积强色�?- 主操作使用现�?primary buon；次要操作用 quiet/secondary�?- 列表、表单、预览之间用轻边框和间距区分，不使用嵌套卡片堆叠�?- 运行证据和输出路径使用等宽字体和浅色证据面板，体现可审计；manifest、安装单元、私有标识等定义信息只在应用程序工作室和审核/发布页展示，不进入普通运行页�?- 红色只用于失败、拒绝、危险权限、删�?撤回�?
### 布局

应用面板位于 GUI 中间面板，与数字员工等入口并列�?
顶部结构�?
```text
应用
[搜索应用... (x)] [+]
[全部应用 v]
```

说明�?
- 搜索框用于搜索应用名、描述、分类、类型、来源、来源能力、业务流程关键词、DataSrv 绑定能力、Skill id、输入模式、输出格式和结构化字段�?- 搜索框有输入时显示清空按钮；�?`Esc` 也清空搜索。搜索状态下标题显示 `搜索结果`，不单独展示常用应用区。顶部提�?`重置筛选`，可一次清空搜索和分类�?- `+` 按钮进入应用程序工作室�?- 分类使用下拉列表，节省空间；选项显示数量，例�?`文档处理 (3)`、`最近使�?(2)`。有搜索词时，分类数量按当前搜索结果重算�? 匹配分类置灰禁用；如果当前分类被搜索条件过滤�?0，自动回�?`全部应用`，避免空列表困住用户。筛选控件下方显示轻量摘要，例如 `搜索“脱敏�?· 1 个匹配`、`文档处理 · 2 个应用`，让用户知道当前列表是被什么条件收窄的�?- 应用列表用图标网格，每行 4 个�?
### 分类下拉

使用一个下拉同时表达大类和细分类：

```text
全部应用
常用应用
最近使�?
企业应用
全部企业应用
财务
进销�?CRM
OA
HR
采购
库存
销�?
工具应用
全部工具应用
文档工具
数据工具
图像工具
转换工具

自动�?全部自动�?定时任务
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

应用列表固定�?4 列图标网格，只保留图标和文字�?
```text
[图标] [图标] [图标] [图标]
名称名称名称名称

[图标] [图标] [图标] [图标]
名称名称名称名称
```

建议尺寸�?
- 单元格：�?52 px 高，宽度�?4 列网格平均分�?- 图标：约 21 x 21 px
- 名称最�?2 行，超出省略
- 单元格尺寸固定，hover、active、状态变化、右键菜单不应造成布局跳动
- 不显示左上角状态点和右上角 pin 点；运行/依赖/常用状态通过ooltip、aria-label、分区和右键菜单表达�?- 图标按钮支持键盘访问：`Tab` 进入网格后，方向键按 4 列布局移动焦点，`Home` / `End` 跳到首尾�?- 图标按钮支持右键菜单：非常用应用显示 `设为常用`，常用应用显�?`移出常用`；键盘用户可�?`Shift+F10` �?`ContextMenu` 键打开同一菜单�?- 图标按钮必须有明�?`aria-label`，包含应用名称、应用类型、来源和当前状态；状态变化时同步更新�?
Tooltip 显示�?
- 应用�?- 描述
- 来源：企业应用包 / 技能包 / 本地创建
- 状态：可用 / 需配置 / 停用 / 运行�?- 最近使用时�?
当前第一�?tooltip 已包含应用名、描述、类�?来源、状态和最近使用时间。状态已经接入基础运行态：打开右侧ab 后显�?`运行中`；DataSrv 应用按能力发现状态显�?`读取中`、`未启用`、`不可用` �?`可用`。后续接入权限和依赖配置后可继续细分�?
### 置顶应用

常用应用�?pin 在最上方两排�?
规则�?
- 最�?8 个置顶应用�?- 图标网格每排 4 个；常用区最多显�?8 个，视觉上不超过两排�?- 置顶应用使用 `pin_order` 排序�?- 超过 8 个时提示用户先取消一个置顶；应用管理页在�?8 个时禁用其他应用�?`置顶` 按钮，并�?tooltip 说明原因；应用面板右键菜单中�?`设为常用` 同样禁用�?- 从市场安装、DataSrv/Skill 发现添加、恢复隐藏应用时，如果新应用声明 `pinned=true` 但当前常用已�?8 个，前端自动降级为不置顶，保持两排上限不�?manifes绕过�?- 搜索状态下不显示单独置顶区，只显示搜索结果�?- 分类筛选状态下，置顶区只显示匹配当前筛选的置顶应用；已经出现在置顶区的应用不在下方主网格重复显示�?- 置顶区存在时，下方主网格标题使用 `其他应用`，表示已排除上方常用应用�?- `最近使用` 是时间排序视图，不显示单独置顶区，避免打散最近使用顺序�?
示例�?
```text
置顶
[报销申请] [合同审查] [采购入库]
[PDF转Word] [表格分析] [库存盘点]

全部应用
[客户建档] [报销审批] [文档脱敏]
```

本地配置�?
```json
{
"installed_apps": {
"doc.contract_review": {
"visible":rue,
"enabled":rue,
"pinned":rue,
"pin_order": 20,
"sort_order": 80,
"recent_used_at": "1718520000000-000001"
}
}
}
```

当前 GUI 实现使用 `maclaw:apps-panel:v1` localStorage 保存面板布局�?
- `orderedIds`：应用显示顺序�?- `pinnedIds`：常用应用，最�?8 个�?- `hiddenIds`：被隐藏的内置应用�?- `editedApps` / `customApps`：本地编辑和本地创建应用�?- `recentUsedAtById`：最近使用时间戳。点击打开应用、在运行页执行应用都会刷新；分类下拉选择 `最近使用` 时只显示有时间戳的应用，并按时间倒序排列�?
读取旧版本缓存时要做轻量迁移：`customApps` �?`id` �?`local-app-` 开头但来源仍是旧�?`market` 的应用，统一迁移�?`local`；缺失或未知来源的自定义应用也按 `local` 处理，避免旧缓存导致应用管理�?tooltip 崩溃�?
## 应用程序工作�?
应用程序工作室通过应用面板顶部 `+` 进入，在右侧打开ab�?
工作室不是弹窗，应该是可长期停留和编辑的工作区。它是创建、配置、审核、发布应用的地方；普通应用运行界面不承担设计器职责�?
工作室目标不是让用户填一堆开发者字段，而是把“想要一个应用”收敛成可运行、可审核、可上传的定义。界面要像企业软件配置台，不像代码编辑器�?
首页入口�?
```text
应用程序工作室（应用面板顶部“工作室”按钮，图标+文字�?
[创建应用]
[应用管理]
[从市场添加]
[审核/发布]
```

工作�?tab 使用标准ab 语义�?
- 外层 `role="tablist"`�?- 入口按钮使用 `role="tab"`，通过 `aria-selected` 表达当前页�?- 内容区使�?`role="tabpanel"`，通过 `aria-labelledby` 绑定当前ab�?- 支持 `ArrowLeft` / `ArrowRight` / `Home` / `End` 键盘切换�?
也可以在首页提供自然语言输入�?
```text
描述你想要的应用...
例如：做一个合同审查应用，上传合同后输出风险报告和修订�?Word
```

### 工作室信息架�?
建议四个页签�?
- `创建应用`：从对话、DataSrv、Skill、MCP、Manifes创建草案�?- `应用管理`：管理已安装和本地应用在面板中的展示�?- `从市场添加`：默认显示可添加应用列表，用户直接点 `添加`；高级导入折叠在 `导入应用包` 中，用于管理员粘贴已审核的应用包 JSON�?- `审核/发布`：本地草案上传、预检、运行证据、审核状态、发布记录�?
第一版前端已经实现四个页签：`创建应用`、`应用管理`、`从市场添加`、`审核/发布`。当�?`审核/发布` 先做本地发布检查：只扫描本地应用，展示基础信息、Manifes结构、绑定能力、运行证据四类检查，并提�?`maclaw.app.pack.v1` 提交包预览和复制。运行证据优先读�?`maclaw:apps-run-history:v1` 中成功运行记录，没有真实运行记录时仅把最近使用作为弱证据。导出的单应用和应用�?manifes都带 `app.version` �?`governance`；`version` 是应用定义的本地整数版本，从 `1` 开始，应用管理中每次保存编辑递增，用于审核、发布、升级和回滚判断。`governance` 包含 `status`、`riskLevel`、`requiredScopes` �?`testEvidence`，方便后续企业市场审核页直接读取。准备好的本地应用可先进入本�?`submied` 状态，提交记录保存�?`maclaw:apps-publish-submissions:v1`，并写入 manifes�?`governance.submission`；企业市场后续可回写 `review_failed`、`approved`、`published`、`deprecated`、`revoked` 等状态，应用面板按状态展示并在提交包中保真导出。用户可撤回本地提交，状态恢复为 `local_tested` �?`draft`；审核退回状态支持修正后重新提交。当前这只是本地审核状态流，不代表已经上传到企�?Hub。后续接真实企业市场接口后，同一页继续承载上传、审核状态和发布记录�?
### 创建入口

创建应用有四条常用入口：

```text
自然语言描述
-> Agen生成草案
-> 用户确认字段/界面/权限
-> 测试
-> 发布或上�?
DataSrv 能力
-> 选择 domain/action/view/report/dashboard
-> 生成企业应用
-> dry-run
-> 发布

Skill/MCP/Workflow
-> 读取参数契约
-> 生成工具应用或自动化应用
-> 试运�?-> 发布

Manifes导入
-> 结构校验
-> 预览应用
-> 安装或保存为草案
```

创建页推荐采用左窄右宽：

```text
左侧：创建方式、类�?preset、基础信息
右侧：实时应用预览、manifest、测试结�?底部：保存草�?/ 测试 / 提交审核
```

交互细节�?
- 表单字段少而清楚：名称、类型、分类、图标、描述、绑定能力�?- 高级字段折叠：权限、运行参数、输出、审计、市场信息�?- Manifes预览默认只读，提供复制；高级用户可切到“源码编辑”，但保存前必须回到结构化校验�?- 每次保存草案生成 `draft_id` �?`version`，测试和审核都绑定这个版本�?- 创建完成不自动发布到企业市场；先进入本地草案或本地应用�?
创建页下方的三张类型卡片不是纯说明区，而是快捷 preset�?
- `应用程序`：切�?`enterprise_app`，默认分�?`OA`，默认图�?`sheet`，Manifes使用 `agent_dynamic_ui`�?- `工具应用`：切�?`tool_app`，默认分�?`文档处理`，默认图�?`shield`，默认文件上传和 `docx/pdf` 输出，Manifes使用 `fixed_skill_ui`�?- `自动化应用`：切�?`automation_app`，默认分�?`自动化`，默认图�?`sync`，Manifes使用 `automation_console`�?- 直接修改类型下拉只改变类型和默认颜色，不覆盖用户已经填写的分类、图标、输入输出；点击卡片才应用完�?preset�?
### 创建企业应用

企业应用创建有两种方式�?
#### 对话创建

适合用户没有现成 DataSrv 模型时使用�?
流程�?
```text
用户描述业务
-> Agen追问字段、规则、状态、权限、流�?-> Agen生成应用草案
-> 用户查看字段表、流程、界面预览、权�?-> 测试 dry-run
-> 发布到应用面�?```

Agen需要总结�?
- 业务对象
- 字段
- 必填�?- 唯一性规�?- 状态流
- 权限
- 业务动作
- 列表视图
- 详情视图
- 审批/确认规则
- 报表或统计入�?
对话创建输出应先成为草案，不直接写入生产 DataSrv�?
#### �?DataSrv 能力创建

适合已有 `MaClawDataSrv` 能力时使用�?
流程�?
```text
选择 DataSrv
-> 选择 domain
-> 选择 BusinessAction / BusinessView / Repor/ Dashboard
-> 自动生成应用定义
-> 用户调整名称、图标、分类、字段顺序、确认规�?-> 测试 dry-run
-> 发布
```

DataSrv 可用能力�?
- `GET /api/v1/data/capabilities`
- `POST /api/v1/data/intent/resolve`
- `GET /api/v1/data/business-actions`
- `POST /api/v1/data/business-actions/{id}/execute`
- `GET /api/v1/data/business-views`
- `POST /api/v1/data/reports/{id}/run`
- `POST /api/v1/data/dashboards/{id}/run`

### 创建工具应用

工具应用�?Skill / Workflow / MCP 包装成固定界面�?
流程�?
```text
选择 Skill / Workflow / MCP
-> 自动读取参数契约
-> 设计上传区和字段
-> 设计输出
-> 试运�?-> 发布
```

工作室配置页�?
- 基本信息：名称、图标、分类、描�?- 输入：文件上传、字段、默认值、高级设�?- 执行：绑�?skill、operation、运行方式、失败修复次�?- 输出：摘要、文件、预览、打开方式
- 安全：权限、是否允许写文件、是否需要确�?- 测试发布：样例输入、运行记录、发布按�?
### 应用管理

应用管理负责应用面板显示，而不是运行应用�?
功能�?
- 添加到应用面�?- 从应用面板移�?- 启用 / 停用
- 置顶 / 取消置顶
- 调整置顶顺序
- 调整普通显示顺�?- 修改图标
- 修改名称
- 修改分类
- 查看来源
- 删除自定义应�?- 卸载来源�?
文案要区分：

- `隐藏`：不在应用面板显示，但仍安装�?- `删除自定义应用`：删除本地创建的 app 定义�?- `卸载来源包`：卸载企业应用包或技能包，可能影响多个应用�?- `停用`：保留入口但不可启动�?
管理界面�?
```text
应用管理

筛选：[全部应用 v]搜索：[搜索应用...]

置顶应用（最�?8 个）
[[menu]] [图标] 报销申请置顶
[[menu]] [图标] 合同审查置顶

可见应用
[[menu]] [图标] PDF �?Word文档工具已显�?[[menu]] [图标] 表格分析数据工具已显�?
未显示应�?[+] [图标] 文档脱敏文档工具添加
```

排序实现�?
- 置顶区使�?`pin_order`
- 普通区使用 `sort_order`
- 应用面板每行 4 个，排序本质是一维顺�?- 工作室可提供 4 列布局预览，拖拽后保存

## 应用定义

统一 schema�?
```json
{
"schema": "maclaw.apps/v1",
"apps": []
}
```

单个应用定义�?
```json
{
"id": "doc.contract_review",
"type": "tool_app",
"name": "合同审查",
"description": "上传合同文件，输出风险报告和修订版文档�?,
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
"description": "填写并提交费用报销，提交前进行结构化校验和 dry-run�?,
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
"detail":rue,
"review":rue
},
"transaction": {
"draft":rue,
"dry_run":rue,
"commit_requires_confirm":rue,
"resume":rue
},
"permissions": {
"allowed_actions": ["finance.expense_submit"],
"allowed_views": ["finance.expense_review"],
"allowed_datasets": ["finance.expenses"]
}
}
```

运行流程�?
```text
打开应用
-> 拉取 BusinessAction
-> 根据 InputFields 生成表单
-> 保存草稿
-> dry-run
-> 展示校验和预览结�?-> 用户确认 commit
-> 展示结果和审计信�?```

### 工具应用定义

```json
{
"id": "doc.contract_review",
"type": "tool_app",
"name": "合同审查",
"description": "上传合同，生成风险报告和修订�?Word�?,
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
"required":rue
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
"label": "生成修订�?,
"type": "boolean",
"default":rue
}
]
},
"run": {
"mode": "agent",
"auto_start": false,
"max_repair_aempts": 1
},
"output": {
"summary":rue,
"files":rue,
"preview":rue
}
}
```

运行流程�?
```text
打开应用
-> 上传文件
-> 填少量参�?-> 点击开�?-> 展示步骤进度
-> 输出摘要、预览、文�?```

## 私有扩展标识

### 推荐标识

统一使用�?
```text
x_maclaw_apps
```

市场预览字段�?
```json
{
"x_maclaw_has_apps":rue,
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

完整定义放在安装包内�?
```text
maclaw.apps.json
```

当前实现约定�?
- Skill 扩展文件：`docs/schemas/maclaw.apps.schema.json`
- 单应用安�?Manifest：`docs/schemas/maclaw.app.schema.json`
- 应用包安�?Manifest：`docs/schemas/maclaw.app.pack.schema.json`
- 示例文件：`docs/examples/maclaw.apps.json`、`docs/examples/maclaw.app.json`、`docs/examples/maclaw.app.pack.json`
- Skill 扩展使用字段 `x_maclaw_apps: "v1"`�?- 单应�?应用包安�?Manifes使用字段 `privateMarker: "x_maclaw_apps"`�?
理由�?
- 不破坏现�?Skill manifest�?- 不要求所�?Skill 都升级结构�?- 企业能力市场可以先展示预览�?- 安装后再读取完整应用定义�?
### Skill 包结�?
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
4. 校验图标 key、类型、输入字段、输出格�?5. 写入本地应用注册�?6. 应用面板显示图标

### 企业应用�?
企业应用包来自企业能力市场，安装后写�?DataSrv 或绑定已�?DataSrv 能力�?
市场卡片�?
```json
{
"capability_type": "enterprise_app_pack",
"capability_id": "inventory-suite",
"display_name": "进销存套�?,
"description": "采购、销售、库存的企业应用包�?,
"metadata_json": {
"x_maclaw_has_apps":rue,
"x_maclaw_app_count": 4,
"x_maclaw_apps_preview": [
{
"id": "inventory.stock_in.app",
"type": "enterprise_app",
"name": "采购入库",
"icon": "warehouse",
"category": "进销�?
}
]
}
}
```

安装时：

```text
安装进销存套�?
将添加以下应用到应用面板�?[x] 采购入库
[x] 销售出�?[x] 库存盘点
[ ] 库存预警
```

## 图标系统

应用图标使用预置 SVG 图标 key。第一版不要求用户上传自定义图标�?
若项目已�?lucide 图标库，优先使用 lucide，保持统一风格�?
应用定义只保存字符串�?
```json
{
"icon": "receipt"
}
```

当前 GUI 第一版实现的可选图标：

- `receipt`：报销/费用
- `wallet`：付�?财务
- `invoice`：发�?票据
- `warehouse`：仓�?入库
- `inventory`：库�?盘点
- `customer`：客�?建档
- `users`：人�?组织
- `contract`：合�?法务
- `pdf`：PDF/转换
- `shield`：审�?脱敏
- `sheet`：表�?数据
- `chart`：报�?分析
- `dashboard`：看�?指标
- `database`：数据库/处理
- `eraser`：清�?脱敏
- `truck`：物�?配�?- `calendar`：日�?考勤
- `web`：网�?采集
- `sync`：同�?自动�?- `bot`：Agent/自动�?
应用程序工作室中的图标选择器显示图�?SVG，并�?tooltip / aria-label 中显示“语义名�?+ icon key”，例如 `合同/法务 (contract)`，方便用户选择，也方便 manifes调试�?
图标颜色使用固定企业色板，不开放任意颜色输入。创建应用和编辑应用都可设置 `panel.accent`，应用面板、运�?tab、市场安装预览都按该颜色渲染图标。当前色板：

- `#2f5f98`：蓝�?- `#657a42`：绿�?- `#7c3f58`：紫�?- `#b45309`：琥珀
- `#28705f`：青�?- `#4b6572`：灰�?- `#8a5a44`：棕�?- `#5b5ea6`：靛�?- `#6b7280`：中性灰

前端维护注册表：

```ts
consAPP_ICON_REGISTRY = {
receipt: Receipt,
wallet: Wallet,
invoice: FileText,
package: Package,
fileCheck: FileCheck,
bot: Bot
}
```

预置图标建议�?
企业应用�?
- `receipt`：报销/费用
- `wallet`：付�?财务
- `invoice`：发�?- `calculator`：核�?- `chart`：报�?- `dashboard`：仪表盘
- `users`：客�?人员
- `user-plus`：建�?入职
- `briefcase`：商�?项目
- `shopping-cart`：采�?- `package`：库�?- `warehouse`：仓�?- `truck`：物�?- `clipboard-check`：审�?- `calendar-check`：考勤/日程
- `building`：组�?部门
- `file-lock`：合�?法务

工具应用�?
- `file-text`：文档处�?- `file-check`：文档审�?- `file-output`：文件转�?- `scan-text`：OCR
- `table`：表�?- `chart-line`：数据分�?- `database`：数据处�?- `eraser`：脱�?清洗
- `sparkles`：AI 生成
- `languages`：翻�?- `image`：图片处�?
自动化：

- `bot`：自动化 Agent
- `clock`：定时任�?- `refresh-cw`：同�?- `globe`：网页采�?- `bell`：监�?提醒
- `workflow`：流�?- `zap`：快速执�?
默认规则�?
- `enterprise_app` 默认 `app-window`
- `tool_app` 默认 `file-text`
- `automation_app` 默认 `bot`
- 财务默认 `receipt`
- 进销存默�?`package`
- 文档工具默认 `file-text`
- 数据工具默认 `table`
- 审批默认 `clipboard-check`

## 右侧 Tab 运行界面

点击应用图标后在右侧创建应用ab�?
右侧ab 是应用运行界面，不是设计器。首屏只承载用户完成任务所需的信息，固定分成�?
```text
应用说明
输入�?执行状态区
执行按钮�?输出�?运行历史
```

运行界面要求�?
- 应用说明只显示名称、描述、类型、分类、常用状态等用户可理解信息�?- 输入区按应用类型渲染文件上传、参数表单、业务对象、操作、备注等控件�?- 执行状态区显示等待、运行中、完成、失败、取消和最�?6 条步骤证据�?- 执行按钮区包�?`重置`、`取消执行`、`执行`，位置固定在输入和输出之间�?- 输出区只放结果：文件产物显示路径�?`打开`、`定位` 操作；无文件时显示文本结果；未执行时显示轻量空态�?- 运行历史放在输出区之后，最多展示最�?8 次，避免挤占本次任务�?- 不显�?`应用定义`、`privateMarker`、`installUnit`、`launchMode`、`binding` 等开�?审核字段�?
Tab 交互规则�?
-ablis使用 `role="tablist"`，每个应�?tab 使用 `role="tab"`�?- 当前ab 设置 `aria-selected="true"`，右侧内容区使用 `role="tabpanel"`，并通过 `aria-labelledby` 指回当前ab�?- 关闭按钮是独立按钮，不嵌套在ab 按钮内，避免键盘和读屏焦点混乱�?- `ArrowLeft` / `ArrowRight` 在已打开应用间切换，`Home` / `End` 跳到首尾ab�?- 点击已打开应用图标时激活已�?tab，不重复打开�?
Tab 标题�?
```text
报销申请
合同审查
采购入库
```

若同一应用多开�?
```text
报销申请 · 草稿 2
合同审查 · report.pdf
```

### 企业应用 Tab

推荐布局�?
```text
顶部：标题、状态、保存时间、操作菜�?主体：表�?/ 列表 / 详情 / 审批 / 报表
底部：保存草稿、校验、提交、取�?侧栏：Agen辅助，可折叠
```

Agen辅助不应抢主界面。默认折叠或窄栏展示�?
- 自动补全
- 解释校验错误
- 从附件提取字�?- 对比历史记录
- 总结提交影响

### 工具应用 Tab

推荐布局�?
```text
顶部：应用标题、状�?中间：上传区、参数区
底部：开始处�?运行中：步骤进度
完成后：摘要、预览、文件列�?```

失败时：

- 展示可读错误
- 提供重试
- 若允许自动修复，展示修复尝试次数
- 保留输入文件和参�?
## 本地应用注册�?
建议�?AppConfig 中新增应用相关配置，或独立存储到 `apps.json`�?
推荐独立存储，避免主配置膨胀�?
```json
{
"schema": "maclaw.installed_apps/v1",
"items": {
"doc.contract_review": {
"app_id": "doc.contract_review",
"enabled":rue,
"visible":rue,
"pinned":rue,
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

原则�?
- 原始 app 定义来自来源包�?- 本地注册表保存用户个性化展示配置�?- 来源包升级后，本地覆盖项继续生效�?- 来源缺失时应用显�?`需重新安装` 或自动隐藏�?
## 与现有能力的关系

### AgentView

现有 AgentView 可作为应用运行界面的底座�?
- 表单字段渲染
- submi/ dismiss
- schemaVersion
- skill 参数补齐
- MIS dry-run / commireview
- workflow form

需要扩展：

- appab owner
- app session lifecycle
-ool_app 文件上传输入
- app-level progress
- app 输出文件索引

### AppView：面向复杂应用的 AG UI 扩展

现有 AgentView 已能覆盖表单、审批、进度、结果、产物。复杂企业应用还需要“可长期停留的软件界面”，不能每次都像一次性弹出的任务卡。因此建议在 AgentView 之上增加 AppView 约定�?
AppView 不允许模型生成任意前端代码，仍然只渲染受控组件。区别是 AppView 有导航、数据源、动作栏和会话状态�?
```ts
type AppView = {
schema: "maclaw.appview.v1";
appId: string;
sessionId: string;
itle: string;
layout: "workspace" | "record" | "report" | "tool";
regions: {
header?: AppHeader;
nav?: AppNavItem[];
main: AgentView | AgentView[];
side?: AgentView | AgentView[];
footer?: AppActionBar;
};
dataSources?: AppDataSource[];
actions?: AppAction[];
audit?: AppAuditRef;
};
```

核心组件�?
- `app_shell`：标题、状态、保存时间、主操作�?- `record_form`：业务对象表单，支持草稿、校验、dry-run�?- `list_view`：DataSrv BusinessView 列表，支持筛选、排序、分页、行操作�?- `detail_view`：记录详情、变更历史、关联产物�?- `approval_panel`：提交影响、风险、确�?拒绝�?- `report_view`：报表参数、图�?表格结果、导出�?- `dashboard_view`：多指标、多卡片、多列表，数据来�?DataSrv dashboard�?- `artifact_viewer`：文档、表格、PDF、JSON、文本产物预览�?- `activity_timeline`：Agen步骤、用户提交、审批、系统校验�?- `assistant_sidecar`：可折叠 Agen辅助栏，解释错误、从附件补字段、生成建议�?
AppView 生命周期�?
```text
open
-> hydrate
-> draft_changed
-> validate
-> dry_run
-> approval_required
-> commit
-> result
-> close/archive
```

与现�?AgentView 事件兼容�?
- 简单表单仍�?`agent-view:lifecycle`�?- AppView 通过同一通道�?`type=app_view`，或新增 `app-view:lifecycle`�?- 提交仍走 `SubmitAgentView` 的兼容壳，后续可增加 `SubmitAppViewAction(sessionID, actionID, payload)`�?- 每个 AppView 必须�?`appId`、`sessionId`、`schemaVersion`、`viewRevision`，避免用户提交过期界面�?
复杂应用的最小可用体验：

```text
打开报销申请
-> AppView workspace
-> 左侧/主区 record_form
-> 右侧 assistant_sidecar 可折�?-> 点击校验
-> dry-run 结果进入 approval_panel
-> 用户确认
-> commit
-> resul+ activity_timeline
```

为什么需�?AppView�?
- 企业应用常有列表、详情、表单、审批、报表多页面，不是一张动态表单�?- 用户会在同一应用里停留、筛选、编辑、保存草稿、返回继续�?- 需要稳定导航、状态条、操作栏，符合传统软件使用习惯�?- Agen仍在后台驱动，但界面像固定软件，降低学习成本�?
实现分层�?
```text
Appab
-> AppSession
-> AppView shell
-> AgentView components
-> DataSrv/Skill/MCP adapters
```

前端可以先把 `enterprise_app` 的运行页接到现有 AgentTaskPanel 渲染器；�?DataSrv 返回 list/report/dashboard 时，逐步增加 AppView shell 和组件，不需要推翻现�?AG UI�?
### DataSrv

DataSrv 是企业应用的主要后端�?
第一版优先包装：

- `BusinessAction -> 表单应用`
- `BusinessView -> 列表应用`
- `Repor-> 报表应用`
- `Dashboard -> 仪表盘应用`

### Skill

Skill 保持 Skill。工具应用是 Skill 暴露�?UI 入口�?
安装 Skill 不等于所�?App 都必须显示。安装弹窗和应用管理允许选择�?
### 企业能力市场

市场仍以能力为安装单元：

- 企业应用�?- 技能包
- MCP 服务

市场卡片通过 `metadata_json.x_maclaw_*` 展示是否包含应用�?
## 上传、审核与发布闭环

应用可以本地使用，但要进入企业能力市场，必须走上传与审核。上传对象仍是能力包，不是孤立图标�?
### 上传对象

| 应用类型 | 上传�?| 必备内容 | 必备证据 |
|---|---|---|---|
| 企业应用 | `enterprise_app_pack` | `maclaw.app.pack.v1`、DataSrv 绑定、权�?scope、业务对�?schema | DataSrv dry-run、字段校验、commi影响说明 |
| 工具应用 | `skill` | `skill.yaml/skill.md`、`maclaw.apps.json`、脚�?引用文件 | 至少一次成功运行或明确免运行理由、产物验�?|
| 自动化应�?| `workflow` �?`enterprise_app_pack` | 触发器、运行计划、权限、输出位�?| dry-run、取�?回滚策略、失败告�?|
| MCP 应用 | `mcp` �?app pack | MCP 配置模板、secre声明、health check | 连接测试、工�?schema 校验 |

### 客户端上传桥�?
前端发布页已预留 Wails 桥接：如果运行时存在 `window.go.main.App.SubmitMaclawAppPackage`，点�?`提交审核` 会优先把单应�?`maclaw.app.pack.v1` 作为 JSON 字符串传给后端；如果方法不存在或调用失败，则保存为前端本�?`channel: "local"` 的待同步提交，用户仍能复制提交包，不会丢失草案。当前后端已实现同名方法，并先把提交写入 `~/.maclaw/data/app_market_submissions.json` durable queue，返�?`channel: "local"`；发布页的本机提交队列会在运行时存在 `SyncMaclawAppPackageSubmissionToHub` 时显�?`同步�?Hub`，把本地提交 POST 到企�?Hub �?`POST /api/capabilities/maclaw-apps/submit`。Hub 接收后会把每�?MaClaw App 作为 `capability_type: skill`、`product_kind: maclaw_app_skill`、`status/version_status: pending_review` 的待审核能力写入租户能力库，并在 metadata 中保�?app id/name/kind、package sha、工作区布局、结果契约、审批工作流契约、测试证据和依赖验证摘要。GUI 同步成功后把本地 `local-review-*` 替换�?Hub 返回�?submission/version key，状态改�?`channel: "hub"`、`status: "pending_review"`，队列和发布卡片随后从同一�?durable queue 刷新�?
建议后端方法签名�?
```go
func (a *App) SubmitMaclawAppPackage(packageJSON string) (map[string]any, error)
func (a *App) ListMaclawAppPackageSubmissions(limiint) ([]maclawAppSubmissionSummary, error)
func (a *App) GetMaclawAppPackageSubmission(submissionID string) (*maclawAppSubmissionRecord, error)
func (a *App) WithdrawMaclawAppPackageSubmission(submissionID string) (bool, error)
func (a *App) SyncMaclawAppPackageSubmissionToHub(submissionID string) (map[string]any, error)
func (a *App) UpdateMaclawAppPackageSubmissionStatus(submissionID string, update maclawAppSubmissionStatusUpdate) (bool, error)
```

`ListMaclawAppPackageSubmissions` 只返回提交摘要：`submission_id`、`submied_at`、`status`、`channel`、`app_ids`、`app_names`、`package_sha256`、`package_bytes`、`reviewed_at`、`published_at`、`reviewer`、`risk_level`、`approved_scopes`、`review_issues`、`submission_evidence`、`event_count`、`last_event_at`、`message`。完�?`maclaw.app.pack.v1` 仍只保存�?durable queue 中，避免列表界面加载大包或暴露过�?manifes内容；`submission_evidence` 是按 app id �?package governance 派生的轻量证据摘要，包含版本快照、依赖、工作区布局、结果契约、审批工作流契约、测试证据和依赖验证，供队列列表直接展示审计状态。`package_sha256` 使用规范�?JSON 后的 sha256，可用于同步重试、人工审核和客户端显示“是否同一包”；`package_bytes` 用于快速发现异常大包。`risk_level` 表示市场审核后的风险等级，`approved_scopes` 表示市场最终批准的权限范围，客户端可据此发现权限升级和发布范围变化。`review_issues` 是结构化修复项，包含 `path`、`severity`、`message`、`suggestion`，用于把审核退回精确定位到 manifes字段或运行证据。`event_count` �?`last_event_at` 用于快速判断该提交是否经历过市场回写或人工处理。后台同�?worker、审计诊断或人工复核需要完整包时，使用 `GetMaclawAppPackageSubmission(submissionID)` 按精确提交编号读取单条记录；该方法返回完�?package payload 和完�?`events` 历史，但不用于应用面板常规刷新�?
前端 `审核/发布` 页会探测该列表方法；存在时显�?`本机提交队列`，队列读取期间先显示轻量加载态，读取成功后列出最近提交摘要，并提供手�?`刷新` �?`最后刷新` 时间，用于同�?worker 或市场回调刚写入状态后的即时查看。旧运行时没有该方法时隐藏此区域，继续使用前端本地提交状态和提交包复制能力。队列行支持通过 `GetMaclawAppPackageSubmission` 内联展开提交详情，直接查看包内应用、审核人、风险等级、审核问题和最近审计事件；也支持复制完整提交包或完整审计记录（包含 package、events、review_issues、approved_scopes 等），方便离线重传、人工审核或同步 worker 诊断。如果运行时没有 detail API，则只显示摘要，详情和复制按钮禁用。运行时存在 `SyncMaclawAppPackageSubmissionToHub` 且队列项仍是 `channel: "local"` 时，队列行显�?`同步�?Hub`，使用当�?`remote_hub_url` �?`remote_viewer_token` 直接上传到企�?Hub；失败时保留本地队列并提示可重试。撤�?`channel: "local"` 的本机待同步提交时，前端会调�?`WithdrawMaclawAppPackageSubmission` 同步删除 durable queue 记录；`channel: "hub"` 的市场提交不允许只从本地队列删除，必须通过企业市场撤回，避免丢失审核审计�?
`UpdateMaclawAppPackageSubmissionStatus` 供后续上�?worker 或企业市场回调使用。允许状态为 `submied`、`pending_review`、`review_failed`、`approved`、`published`、`deprecated`、`revoked`，允�?channel �?`local` �?`hub`。当本地提交成功同步到企业市场后，worker 或手动同步可以把本地 `local-review-*` 替换成市场返回的 `submission_id`，并写入审核消息、审核人、审核时间、发布时间、风险等级、批准权限和结构化审核问题；前端队列摘要会直接显示新状态，发布卡片会显示最多前三条审核问题�?`severity / path / message / suggestion`，超出时显示剩余数量，并把完整字段回灌到应用 manifes�?`governance.submission`。如果发布卡片存在审核问题，卡片提供 `去修复`，切�?`应用管理` 并自动展开对应应用编辑区；管理页会清空旧筛选，避免目标应用被筛选条件隐藏。修复保存后不会删除旧审核记录，而是在本地提交记录上写入 `modifiedAt` 和当�?`version`，发布卡片显�?`本地已修改，需重新提交`，重新启�?`提交审核`；提交包 manifes�?`governance.submission.modifiedAt` �?`governance.submission.version` 同步带出，方便市场判断这是基于旧审核意见修改后的新版本。点击重新提交时，前端使用当前应用定义生成干净提交包，不带�?`modifiedAt` 和旧 `reviewIssues`；新提交成功后用新的 submission 覆盖本地记录，清理旧修改标记、旧审核问题和旧批准权限，状态回到待审核，并保留本次提交版本�?
前端刷新 `本机提交队列` 时，会按 `app_ids` 把队列摘要回灌到 `maclaw:apps-publish-submissions:v1`。因�?worker �?durable queue 更新�?`published` �?`review_failed` 后，发布卡片、提交包 manifes和队列摘要会保持同一状态，不需要用户重新提交或手工复制状态�?
返回字段�?
```json
{
"submission_id": "market-review-123",
"submied_at": "2026-06-17T01:00:00Z",
"status": "submied",
"channel": "local",
"app_ids": ["contract-review"],
"app_names": ["合同审查"],
"package_sha256": "6b1f...",
"package_bytes": 4096,
"reviewed_at": "2026-06-17T01:10:00Z",
"published_at": "2026-06-17T01:20:00Z",
"reviewer": "market-reviewer",
"risk_level": "high",
"approved_scopes": ["finance.expense_submit", "finance.audit"],
"event_count": 2,
"last_event_at": "2026-06-17T01:20:00Z",
"review_issues": [
{
"path": "apps[0].app.governance.testEvidence",
"severity": "error",
"message": "缺少运行证据",
"suggestion": "先运行一次应用并重新提交"
}
],
"message": "queued"
}
```

客户端会接受 `submied`、`review_failed`、`approved`、`published`、`deprecated`、`revoked`。返回的提交状态会写入 `governance.submission.channel = "hub"`；本地兜底状态写�?`channel = "local"`�?
### 上传前预检

上传前必须在本地显示一张“发布检查”面板：

```text
发布检�?�?Manifes结构有效
�?图标、分类、名称完�?�?权限声明完整
�?包文件可移植
�?安全扫描通过
�?至少一次测试运行成�?! 缺少市场截图，可�?
[查看详情] [重新检查] [提交审核]
```

预检项：

- Manifest：schema、id、kind、launchMode、binding、panel、governance�?- 可移植性：Skill 复用 `PrepareSkillForUpload`，阻止绝对路径、缺失文件和机器特定引用�?- 安全：复用现�?skill security scan；高风险包进入人工确认或企业审核�?- 运行证据：Skill App 记录 run id、输入摘要、输�?artifact；企业应用记�?dry-run request/response 摘要�?- 权限：声�?DataSrv scope、文件读写、网络、MCP secrets、外�?API、付费调用�?- UI 契约：固定工具应用必须能用当�?`fixed_skill_ui` 渲染；复杂企业应用必须能生成受控 AG UI/AppView�?- 隐私：上传包不得包含用户输入文件、运行缓存、token、`.git`、`node_modules`、`__pycache__` 等运行产物�?
### 审核队列

企业市场审核页建议分三栏�?
```text
左：提交列表
中：包摘要、应用预览、权限、风险、运行证�?右：审核结论、修改建议、发布范�?```

审核员看到：

- 包来源、上传者、版本、fingerprint、签�?校验和�?- 包内应用列表：名称、类型、分类、图标、描述、launchMode�?- 权限矩阵：DataSrv scope、Skill/MCP 权限、文�?网络/外部服务�?- 运行证据：测试输入摘要、步骤、产物路�?哈希、dry-run 结果�?- 安全扫描：严重级别、风险因素、是否需要用户确认�?- UI 预览：应用面板图标预览、运�?tab 首屏、AG UI 表单/列表/审批预览�?- 升级影响：新�?删除 app、权限变化、schema 变化、兼容性说明�?
审核动作�?
- `通过并发布`：进�?published，可被搜�?安装�?- `通过但暂不发布`：进�?approved，等待管理员决定范围�?- `要求修改`：进�?review_failed，返回结构化问题�?- `拒绝`：进�?rejected/revoked，记录原因，不允许同版本重复提交�?
审核结论必须结构化：

```json
{
"review": {
"status": "approved | changes_requested | rejected",
"riskLevel": "low | medium | high | critical",
"requiredFixes": [
{ "path": "apps[0].binding.skill", "message": "缺少运行证据" }
],
"approvedScopes": ["finance.expense_submit"],
"notes": "只允许财务部门安�?
}
}
```

### 发布与分�?
发布后管理员决定分发策略�?
- `可搜索`：用户在市场中主动安装�?- `推荐`：显示在推荐区，用户可安�?忽略�?- `下发`：进�?managed deployment，客户端 best-effor自动安装�?- `隐藏应用`：包已安装，但某�?app 不默认显示�?- `灰度发布`：按部门、角色、设备、版本范围发布�?
安装时必须显�?app 级预览，用户或管理员可以选择�?
```text
将添�?4 个应�?[x] 报销申请企业应用财务
[x] 报销审批企业应用财务
[ ] 费用看板企业应用报表
[x] 发票识别工具应用文档处理
```

安装成功后：

- 写入本地 installed app registry�?- 保留来源包、版本、origin、review id�?- 触发 `app:registry_updated`�?- 应用面板刷新�?- 若缺少配置，显示 `需配置`，不隐藏错误�?
### 升级、回滚、下�?
升级要同时处理“来源定义”和“本地展示配置”：

- 来源定义更新：新字段、新 app、新权限、新输出�?- 本地覆盖保留：用户改过的名称、图标、分类、置顶、排序、隐藏�?- 权限升级：新增高风险权限时必须重新确认或重新审核�?- 兼容升级：`schemaVersion` 不兼容时显示“需要迁移”�?- 回滚：管理员可回退到已批准版本；客户端恢复来源定义，不清除用户本地布局�?- 下架：`deprecated` 只停止新装；`revoked` 禁用运行，保留历史和审计，面板显示原因或自动隐藏�?
第一版市场导入已支持基础升级判断：粘�?manifes时，安装预览会按应用 identity 比较本地 `app.version` 和导入版本。同一应用更高版本显示 `将升�?v�?-> v新`，可勾选执行；同版本或低版本仍显示 `已安装` 并跳过。执行升级时保留本地应用 id、排序、置顶、分类、图标、颜色和最近使用记录，只替换来源定义、名称、描述、binding、manifes和版本。这样升级不会破坏用户使用习惯，也不会丢失运行历史和打开ab 的身份线索。升级预览还会比较旧版和新版 `requiredScopes`，如果新�?scope，行内显�?`权限变化`；命�?`finance/payment/admin/audit/approve/delete/write/upsert/commit` 等关键词时额外标�?`高风险`。第一版客户端已经把高风险升级改为二次确认：首次点�?`安装` 只显示风险提示并把按钮切换为 `确认安装`，第二次点击才真正写入升级；后续企业版可把同一规则升级为管理员确认或重新审核�?
### 反馈与治�?
使用后形成闭环：

- 用户可在应用运行ab 反馈“好�?有问�?结果错误”�?- 工具应用保存 run outcome、失败类别、产物验证�?- 企业应用保存事务审计、dry-run/commi结果、审批链�?- 市场聚合安装量、运行成功率、失败率、最近风险�?- 低成功率或高风险应用自动进入复审队列�?
## 建议实现接口

第一版可先在 GUI 后端提供 Wails 方法，前端不直接读写文件�?
应用发现�?
```go
ListApps(filter AppListFilter) ([]AppSummary, error)
GetAppDefinition(appID string) (*AppDefinition, error)
```

应用面板配置�?
```go
UpdateAppVisibility(appID string, visible bool) error
PinApp(appID string, pin bool) error
ReorderApps(items []AppOrderUpdate) error
UpdateAppDisplay(appID string, patch AppDisplayPatch) error
```

应用运行�?
```go
OpenAppSession(appID string, inpuOpenAppSessionInput) (*AppSession, error)
SubmitAppSession(sessionID string, data map[string]any) (*AppRunResult, error)
CancelAppSession(sessionID string) error
GetAppSession(sessionID string) (*AppSession, error)
```

应用工作室：

```go
CreateAppDraft(inpuCreateAppDraftInput) (*AppDraft, error)
UpdateAppDraft(draftID string, patch AppDraftPatch) (*AppDraft, error)
TestAppDraft(draftID string, inpumap[string]any) (*AppRunResult, error)
PublishAppDraft(draftID string) (*AppSummary, error)
DeleteCustomApp(appID string) error
```

市场和安装：

```go
ListMarketplaceAppPacks(query string) ([]CapabilityWithApps, error)
InstallCapabilityApps(capabilityRef string, appIDs []string) error
ScanSkillApps(skillName string) ([]AppDefinition, error)
```

建议事件�?
```text
app:registry_updated
app:session_created
app:session_progress
app:session_completed
app:session_failed
app:session_output_ready
```

## 数据�?
### 应用面板加载

```text
前端进入应用面板
-> ListApps
-> 合并来源定义 + 本地注册�?-> �?pinned/pin_order/sort_order 排序
-> 应用搜索和分类筛�?-> 渲染 4 列图标网�?```

### Skill App 安装后出现图�?
```text
安装 Skill
-> 扫描 skillDir/maclaw.apps.json
-> 校验 app 定义
-> 写入 app definition cache
-> 写入 installed_apps 默认可见�?-> 发出 app:registry_updated
-> 应用面板刷新
```

### 企业应用打开

```text
点击企业应用
-> OpenAppSession
-> 根据 source.kind 拉取 DataSrv BusinessAction/View/Report/Dashboard
-> 构�?AgentView �?AppView
-> 右侧创建ab
-> 用户提交
-> dry-run
-> commireview
-> commit
-> 记录 session、事务、审�?```

### 工具应用打开

```text
点击工具应用
-> OpenAppSession
-> 渲染上传区和参数
-> 用户开始处�?-> 上传文件进入临时 artifact
-> run_skill / workflow / MCP 调用
-> app:session_progress 推送步�?-> 输出文件进入 artifact/outpustore
-> 前端展示预览和下载入�?```

## 校验规则

应用定义必须校验�?
- `id` 非空，且全局唯一�?- `type` 属于 `enterprise_app |ool_app | automation_app`�?- `name` 非空�?- `icon` 必须存在于预置图标注册表，缺失时使用默认图标�?- `source.kind` 必须与应用类型匹配�?- `tool_app` 的文件输入必须声�?`accept` �?`required`�?- `enterprise_app` 绑定 DataSrv action 时必须能解析�?`BusinessAction`�?- `enterprise_app` �?commi操作时默�?`commit_requires_confirm=true`�?- 市场安装�?app 不能越权声明 DataSrv 权限，只能请求已授权 scope�?- 自定�?app 删除只允许删除本地定义，不得误删来源包�?
## 安全与权�?
企业应用�?
- 写入类动作必须先 dry-run�?- commi默认需要人工确认�?- 敏感字段遵循 DataSrv API key policy�?- 权限不足时应用可显示，但状态为 `需授权` �?`不可用`�?- 事务记录必须包含 action_id、dataset_id、dry_run 结果、commi结果、操作者�?
工具应用�?
- 文件上传进入受控临时目录�?- Skill 写文件时复用现有安全策略和审批机制�?- 输出文件必须明确来源 app/session�?- 自动修复次数有限，默�?1 次�?- 涉及外网、敏感文件、批量写入时沿用现有风险评估�?
应用市场�?
- `x_maclaw_apps` 只作为声明，不等于自动信任�?- 安装时必须校验来源包签名/来源策略�?- 企业策略禁止非企业市场安装时，带 app �?skill 也必须被拦截�?- 管理员可允许安装包但隐藏部分 app�?
## 实施阶段

### Phase 1：应用面板与本地应用注册�?
范围�?
- 新增 `应用` 面板入口�?- 顶部搜索、创建按钮、分类下拉�?- 4 列图标网格�?-ooltip�?- pin 前两排，最�?8 个�?- `最近使用` 分类筛选：打开应用或执行应用后写入最近使用时间，重启后仍可恢复�?- 本地应用注册表�?- 应用程序工作室打开右侧ab�?- 应用管理：显�?隐藏、置顶、排序、改图标、改分类�?- 应用管理：支持搜索和分类筛选，方便应用较多时定位；筛选同时作用于已安装列表和已隐藏列表，只影响可见项，不改变真实排序。筛选状态下显示当前条件摘要和匹配数量，并禁用上�?下移/移到顶部/移到底部，提示用户清空筛选后再排序，提供 `重置筛选` 按钮一次清空搜索和分类�?- 应用管理：排序操作支�?`上移`、`下移`、`移到顶部`、`移到底部`，用于快速安排应用面板图标显示位置�?- 应用管理：支�?`复制应用`，从已有应用克隆出一个本地副本，名称追加 `副本`，来源标�?`本地`，默认不置顶，保留原 DataSrv / Skill 绑定，方便基于相似能力创建变体。连续复制同一应用时自动编号为 `副本 2`、`副本 3`，避免同名混淆。复制后自动打开副本编辑表单，用户可立即改名、改分类、改图标和颜色�?- 应用管理：内置应用使�?`隐藏`，进�?`已隐藏应用` 列表并可恢复；用户创建、复制或市场安装的本地应用使�?`移除`，从面板删除，不进入内置恢复列表。移�?隐藏应用时同步清理该应用�?`maclaw:apps-run-history:v1` 中的本地运行历史；移除本地应用时还清�?`maclaw:apps-publish-submissions:v1` 中的本地审核提交状态，避免后续重建�?id 时看到旧记录�?
可用本地 mock app 验证 UI�?
### Phase 2：Skill App

范围�?
- 支持 skill 目录 `maclaw.apps.json`�?- 安装 skill 后扫描应用定义�?- 应用面板显示工具应用�?- 点击打开工具应用ab�?- 支持文件上传、参数表单、运�?skill、展示进度与输出�?- 支持应用工作室创�?Skill App�?- 应用工作室的创建页和管理页都支持编辑 `tool_app` 结构化字段：
- 字段属性：`name`、`label`、`type(text/select/boolean)`、`required`、`default`、`options`�?- `inputMode=form/mixed` 时显示字段编辑器；`file` 模式只显示文件输入�?- `multiple_files=true` 时文件输入允许一次选择多个文件；默认仍是单文件�?- 保存应用时必须保留并导出 `binding.skill.fields`�?- 工具应用运行ab �?manifes渲染字段表单�?- `text/select` �?`required=true` 必须填值后才能执行�?- `boolean` 始终有明�?true/false 值，不阻塞执行�?- 文件输入模式下没有文件时给出内联校验提示�?- 工具应用执行桥接现有 Skill Runner�?- 点击执行时调�?`RunNLSkillAsync(skillID, runArgs)`�?- `runArgs` 包含 `_maclaw_app`、`app_id`、`app_name`、`input_mode`、`output_mode`、`params`、`fields`、`file`、`files`、`file_name`、`prompt`�?- 文件输入先调�?`StageSkillAppInputFile(fileName, mimeType, lastModified, contentBase64)` 写入 MaClaw 临时目录，再启动 skill�?- `StageSkillAppInputFile` 返回 `file` 交接对象：`name`、`size`、`type`、`last_modified`、`staged_path`、`transfer=staged_file`�?- 启动 skill 时同时提供扁平路径别名：`file_path`、`input_file_path`、`local_file_path`、`uploaded_file_path`，方�?skill.yaml 使用 `{{file_path}}` 直接读取首个文件�?- 多文件运行额外提�?`files` �?`file_paths`；旧 skill 仍可用首个文件的 `file/file_path`�?- 当前 staging 上限�?25MB；超过上限前端阻止执行并提示用户。后续大文件应改为流�?artifact/file store�?- Skill Runner �?run 进入终态后清理 `~/.maclaw/temp/app-inputs` 下对�?staged 输入文件；越界路径拒绝删除，并写�?run warning�?- 如果 skill run 未能启动（例如找不到 skill、策略拒绝、precheck 失败），`RunNLSkillAsync` �?best-effor清理本次 staged 输入文件�?- 如果 `RunNLSkillAsync` 返回�?run id，也按启动失败处理：运行页显示失败，写入本地运行历史，不进入无法轮询和无法取消的 running 状态�?- 每次 stage 新输入文件前会清�?24 小时前残留的 `app-inputs/input-*` 目录，用于覆盖崩溃或异常退出后的残留�?- 小文本文件可附带 `file_text` 作为轻量预览；正式大文件/二进制文件应继续�?artifact/file store 管线�?- 返回 run id 后进�?`running` 状态，并轮�?`GetNLSkillRunStatus(runID)`�?- `success/completed/done` 映射为完成，`failed/error/timeout` 映射为失败，`cancelled/canceled` 映射为取消�?- 运行中显示当�?step、session progress �?run id；完成后状态区只显示完成态，不重复展示输出正文�?- 运行页必须展示执行状态区：最多显示最�?6 �?`steps`，展�?step 名称、状态、耗时和短输出/错误�?- �?`SkillRunStatus.summary.artifact_path`、`artifact_status`、`expected_artifact` �?`needs_artifact_verification` 存在，输出区展示产物状态；有真实路径时显示路径，并提供 `打开` �?`定位` 操作，分别调�?`OpenFileOrShowInFolder(path)` �?`ShowItemInFolder(path)`。后续可替换�?artifac链接�?- 运行中允许调�?`CancelNLSkillRun(runID)` 取消�?- 每个应用保留最�?8 条本地运行历史，存储 key �?`maclaw:apps-run-history:v1`�?- 历史项记�?run id、终态、输出格式、输入摘要、消息摘要、产物路径和时间，展示在运行页结果区下方；有产物路径时同样提�?`打开` �?`定位`�?- 用户可在单个应用运行页清空该应用历史，不影响其他应用�?- `maclaw.apps.json`、`maclaw.app.v1`、`maclaw.app.pack.v1` 三种格式都支持同一套字段定义�?- 市场安装页必须在安装前做轻量结构校验�?- `maclaw.app.v1` 必须包含 `schema=maclaw.app.v1`、`privateMarker=x_maclaw_apps`、`app.id`、`app.name`�?- `maclaw.app.pack.v1` 必须包含 `privateMarker=x_maclaw_apps` 和非�?`apps`，并逐个校验包内 `maclaw.app.v1`�?- `maclaw.apps.json` 必须包含 `x_maclaw_apps=v1` 和非�?`apps`，每�?app 至少有合�?`id` �?`name`�?- 应用名称可以是中文；manifes`app.id` / `apps[].id` 必须保持 ASCII 标识符，匹配 `^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`。本地创建中文应用时�?`local-app-<time>-app` 兜底，避免导出后无法安装�?- `app.kind` �?`launchMode` 必须匹配：`enterprise_app -> agent_dynamic_ui`，`tool_app -> fixed_skill_ui`，`automation_app -> automation_console`。显式写错时阻止安装�?- `enterprise_app` 必须�?`binding.datasrv.domain`，代�?DataSrv/MIS 动态界面能力；`tool_app` 必须�?`binding.skill.id`，且 `installUnit` 必须�?`skill`。这样企业动态应用和 Skill UI 化应用在市场包里不会混用�?- Skill App �?`output_modes` / `binding.skill.outputModes` 只能使用 `docx`、`xlsx`、`pdf`、`json`、`txt`；字段类型只能是 `text`、`select`、`boolean`。非法值必须阻止安装并显示具体路径，不能静默降级�?- 错误提示要指向具体路径，例如 `maclaw.app.pack.v1 apps[0] privateMarker musbe x_maclaw_apps`�?- 安装预览做重复识别时，`foo` �?`market-foo` 视为同一个安装身份。这样市场包不能绕过前缀再安装一份已内置或已安装的同名应用�?- 安装预览工具条同时显�?`已�?总数`、`可安�?N`、`可升�?N` �?`将跳�?N`，让用户在全选、全不选、重复包、升级包场景下直接知道实际安装结果�?- 选中的升级项如果新增高风险权限，首次点击 `安装` 只进入确认态，不执行安装；用户再次点击 `确认安装` 后才升级，防止高风险 scope 通过普通市场包静默扩权�?- 安装完成后结果面板必须显示逐项明细：哪些应用已安装、哪些已升级、哪些被跳过以及跳过原因。摘要数字不把升级混进安装数量；发生升级时按 `已安�?/ 已升�?/ 已跳过` 分开统计，普通安装仍保持 `已安�?/ 已跳过` 的简洁摘要。明细回答“是哪几个、为什么”，便于用户核对市场包行为�?- 安装预览中不可安装项必须显示跳过原因，例�?`已安装`、`重复应用`、`未选择`；行 hover �?checkbox aria-label 也要包含相同原因，方便键盘和读屏用户理解�?- 后端扫描 `maclaw.apps.json` 时要先规范化字段�?- �?`name` 字段丢弃�?- 未知 `type` 降级�?`text`�?- `select.options` 去空、去重，且默认值自动并入候选项�?- `boolean.default` 统一为布尔值，忽略 `options`�?- �?`label` 自动使用 `name`，保证运行界面总有可见标签�?
优先样例�?
- 合同审查
- PDF �?Word
- 文档脱敏

### Phase 3：Enterprise App / DataSrv App

范围�?
- �?DataSrv capabilities 创建企业应用�?- BusinessAction 表单渲染�?- dry-run �?commireview�?- BusinessView 列表页�?- Repor/ Dashboard 结果页�?- 应用工作室对话式创建草案�?
优先样例�?
- 报销申请
- 客户建档
- 采购入库

### Phase 4：企业能力市场联�?
范围�?
- 市场支持 `enterprise_app_pack`�?- `metadata_json.x_maclaw_apps_preview` 展示应用预览�?- 安装弹窗选择要添加的应用�?- 管理员可统一部署 / 推荐 / 隐藏应用�?- inventory 上报已安装应用�?
### Phase 5：自动化应用

范围�?
- 定时任务 / 监控 / 同步类应用�?- �?scheduler、workflow、MCP 结合�?- 支持运行历史、暂停、恢复、提醒�?
## 关键边界

- 应用面板只负责找应用和启动应用，不承载复杂管理�?- 应用程序工作室负责创建、添加、隐藏、排序、删除�?- Skill 仍是能力包，App �?UI 入口�?- 企业应用包是市场安装单元，企业应用是应用面板入口�?- DataSrv capability 是内部资源，不直接暴露为用户命名�?- 使用阶段尽量软件化，不把所有操作变成聊天�?- 创建阶段可以对话式，�?Agen总结�?app 定义�?
## 开放问�?
- 企业应用包是否需要独立包格式，还是先通过市场 metadata + DataSrv bootstrap API 实现�?- 自定义应用定义是否允许导出为技能包或企业应用包�?- 应用运行历史应归属现�?session store，还是新�?app session store�?- 文件上传结果应复�?artifac体系，还是为ool_app 建独�?outpustore�?- 移动端是否显示完整应用面板，还是仅显示常用和最近�?