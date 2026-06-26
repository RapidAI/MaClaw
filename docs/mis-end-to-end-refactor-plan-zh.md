# MaClaw MIS 端到端重构方案

日期：2026-06-18  
状态：重构决策稿  
范围：MaClawDataSrv、MIS Tools、Skill Generator、MaClaw App、AG UI、App Studio

## 结论

彻底重构 MIS 业务逻辑。

不再以 `dataset / field / action / token` 作为产品主入口。它们保留为底层实现概念。
本轮按重新开发处理，不以兼容现有 `binding.datasrv`、`mis_data(action=...)`、`/api/v1/data/approvals` 等旧入口为设计前提。
旧实现只作为领域经验和可复用代码片段参考，不作为新架构的兼容约束。

界面风格继续沿用现有 MaClaw / App Studio 的产品气质：

```text
安静、清晰、偏工作台
应用面板、向导、表单、表格、审批卡片延续现有交互习惯
不做营销式首页
不做重装饰视觉
不打断用户已有认知
```

也就是说：

```text
后端和业务模型可以重做
前端视觉语言和操作路径要连续
```

第一版前端不是重新发明一套 UI，而是在现有风格下替换企业应用内核：

```text
应用入口仍是 App 面板
创建入口仍是 App Studio 向导
业务录入仍是 AG UI 表单/表格/选择器
审批仍以审批中心、单据审批卡片、通知待办为主要入口
```

新的主入口是：

```text
业务对象
业务应用
业务 Skill
AG UI
统一 MIS Tool
```

目标链路：

```text
用户提出业务需求
  -> 系统识别业务对象
  -> DataSrv 创建/复用数据模型
  -> Skill Generator 生成业务 Skill
  -> MaClaw App 包装成应用入口
  -> AG UI 提供输入输出
  -> MIS Tool 统一访问 DataSrv
  -> DataSrv 做权限、审批、审计
```

## MaClaw App 本质和产品形态

MaClaw App 的本质不是独立业务运行时，而是：

```text
Skill / Workflow Skill 的 UI 化封装
能力市场里的分发单位
用户可点击、可配置、可测试、可发布的应用入口
```

Skill 提供能力，MaClaw App 提供传统软件式可视化操作界面。
App 不应该退化成聊天入口，也不应该只是一个表单。

核心公式：

```text
MaClaw App = Skill + 动态 UI 布局 + 运行契约 + 测试证据 + 市场分发信息
```

所有 MaClaw App 的界面都应动态生成。
应用设计时，系统根据 Skill 输入输出、工作流节点、结果类型自动生成界面初稿。
用户在 App Studio 里可视化调整位置、字段、列、按钮、导航和输出区域。
设计结果保存到应用信息文件，再经本地测试后上传 Hub 能力市场。

应用信息文件至少保存：

```text
app metadata
  名称、图标、分类、版本、来源

binding
  绑定 Skill 或 Workflow Skill

workspace layout
  传统软件式工作界面布局

runtime contract
  输入、输出、结果类型、权限需求

governance
  风险等级、测试证据、审核状态、发布状态
```

### App 三种产品形态

第一版 MaClaw App 分三类：

```text
企业审批型应用 enterprise_approval_app
  连接企业后台数据
  带审批/流转
  需要审批实例数据管理
  例：报销审批、采购申请、合同审批、付款审批

企业普通应用 enterprise_normal_app
  连接企业后台数据
  不带审批实例
  用于一次性数据录入、查询、业务功能调用
  例：客户建档、库存查询、发票录入、库存盘点提交

工具型应用 tool_app
  不连接企业后台数据
  只做独立功能处理
  例：PDF 翻译、Excel 清洗、文档生成、图片处理
```

抽象关系：

```text
企业审批型应用 = 审批工作流 Skill 的 UI 化封装
企业普通应用 = 业务操作 Skill 的 UI 化封装
工具型应用 = 工具 Skill 的 UI 化封装
```

审批实例数据管理只属于企业审批型应用，不属于所有企业应用。

### 企业审批型应用

企业审批型应用的本质是：

```text
企业审批型应用
= 数据录入
+ 审批工作流 Skill 运行
+ 审批实例数据管理
+ 审批实例视图
+ 结果反馈
```

#### 1. 数据录入

发起节点 UI 收集业务数据、附件、业务对象引用和基础校验结果。
它生成发起 payload，不直接承担审批流转。

```text
发起表单
附件上传
业务对象选择器
字段校验
提交按钮
```

#### 2. 审批工作流 Skill 运行

审批工作流 Skill 负责流程定义和节点编排。
节点可以是人工审批、VE 审批、自动判断、MIS 操作、通知、结果处理。

```text
start_form
approval
condition
mis_operation
notification
result
```

#### 3. 审批实例数据管理

每次提交都会创建审批实例。
审批实例保存一次运行的事实，不是流程定义，也不是 App manifest。

至少包含：

```text
实例头
节点状态
审批任务
审批决策
意见/评论
附件引用
事件日志
当前节点
当前处理人
最终结果
```

#### 4. 审批实例视图

审批型应用内部必须能看实例状态，不只负责发起。

应用内固定视图：

```text
我的申请
待我审批
已处理
需关注
全部（有权限才显示）
```

DataSrv 恢复出的审批实例进入 GUI 时，必须按当前用户恢复视图归属：

```text
显式 lane / approval_lane 优先
attention 状态进入需关注
submitted_by / created_by / applicant 命中当前用户 -> 我的申请
pending 且 assigned_to / current_assignee 命中当前用户 -> 待我审批
approved / rejected 且 reviewed_by 命中当前用户 -> 已处理
```

这样 `all` 视图不会把远端 RecordApproval 全部粗略归为待审批，也能支撑“我的申请”和“我审批的”两个入口。

列表至少显示：

```text
单号/标题
发起人
发起时间
当前节点
当前处理人
状态
最近意见
更新时间
```

详情至少显示：

```text
录入数据
附件
当前节点状态
审批轨迹
评论/意见
操作按钮
最终结果
```

审批中心是跨应用聚合入口，不替代每个审批型应用自己的实例视图。

#### 5. 结果反馈

结果反馈可返回审批结果、业务状态、文件、文本、通知或后续动作。
审批型应用必须让发起人和审批人都能看到当前状态和最终结果。

### 企业普通应用

企业普通应用是一次性业务操作型软件。
它连接企业后台数据，但不创建审批实例。

典型能力：

```text
数据录入
数据查询
状态更新
业务功能调用
外部系统同步
结果展示
```

企业普通应用的运行链路：

```text
用户输入
  -> 业务 Skill 运行
  -> MIS 操作
  -> 返回业务记录/状态/表格/文件/文本
```

它需要 MIS 能力和业务对象映射，但不需要审批实例数据管理。

### 工具型应用

工具型应用不连接企业后台数据。
它只做独立能力处理，通常输入文件或参数，输出 artifact 或文本。

典型能力：

```text
PDF 翻译
文档生成
Excel 清洗
图片处理
合同摘要
```

工具型应用不应该出现企业后台、审批、DataSrv、审批实例等概念。

## 动态 UI 和传统软件式工作台

MaClaw App 的界面要像传统业务软件/工具软件，而不是只有表单。
表单只是组件之一，不是 App 的整体形态。

动态 UI 生成目标是工作台布局：

```text
应用窗口
工具栏
左侧导航或页签
主工作区
列表/表格
详情区
操作按钮
状态栏
输出/附件/历史面板
```

workspace layout 支持的组件至少包括：

```text
toolbar
sidebar
tabs
table
detail_panel
form_panel
timeline
approval_actions
file_queue
preview_panel
output_panel
status_bar
dashboard
split_view
```

### 企业审批型应用界面

例如报销审批 App：

```text
顶部工具栏
  发起报销 / 刷新 / 导出 / 筛选

左侧或页签视图
  我的申请 / 待我审批 / 已处理 / 需关注 / 全部

中间列表
  单号 / 申请人 / 金额 / 当前节点 / 状态 / 更新时间

右侧详情
  基本信息 / 报销明细 / 附件 / 审批轨迹 / 意见

操作区
  通过 / 拒绝 / 转交 / 加签 / 仅关注
```

### 企业普通应用界面

例如客户管理 App：

```text
顶部工具栏
  新建客户 / 导入 / 刷新 / 导出

左侧导航
  客户列表 / 最近跟进 / 待完善 / 标签分组

主区表格
  客户名 / 联系人 / 阶段 / 负责人 / 最近跟进

右侧详情
  基本资料 / 联系记录 / 附件 / 操作历史

操作区
  保存 / 新增跟进 / 生成报告
```

### 工具型应用界面

例如 PDF 翻译 App：

```text
顶部工具栏
  添加文件 / 开始翻译 / 取消 / 打开输出目录

左侧文件队列
  文件列表 / 状态 / 页数 / 进度

主区预览
  原文/译文对照 / 当前页 / 处理日志

右侧设置
  目标语言 / 输出格式 / 术语表 / 保留版式

底部输出
  生成文件 / 文本摘要 / 错误提示
```

## App 输出结果类型

App 输出不能只理解成文件或文本。
企业应用还需要状态、记录、任务、回执和业务化错误。

标准输出类型：

```text
approval_result
  审批结果：approved / rejected / attention / cancelled / timeout
  attention 表示需关注、仅查看，不等同通过或拒绝

business_status
  业务对象状态变化

business_record
  新建、更新或查询到的企业数据

artifact
  文件或文档：PDF、DOCX、XLSX、CSV、图片、压缩包

text
  直接展示的文本内容：摘要、翻译、说明、意见

table
  结构化结果集：库存列表、客户列表、交易明细

dashboard
  图表或仪表盘

notification
  通知、待办、抄送、提醒

external_receipt
  外部系统动作回执：邮件已发送、ERP 已同步、付款指令已提交

requires_input
  需补充信息：缺附件、缺字段、需选择对象

error
  业务化错误：额度不足、权限不够、状态不允许
```

各类 App 的常见输出：

```text
企业审批型应用
  approval_result
  business_status
  business_record
  notification
  artifact
  requires_input
  error

企业普通应用
  business_record
  business_status
  table
  dashboard
  artifact
  text
  external_receipt
  requires_input
  error

工具型应用
  artifact
  text
  table
  dashboard
  requires_input
  error
```

## MaClaw App 超级 Skill 和依赖模型

MaClaw App 本质上是一个带特殊 App 数据的超级 Skill。
它不是普通 Skill 的外部快捷方式，而是一个可分发、可安装、可运行的 Skill 产品包。

```text
MaClaw App Skill
  = 普通 Skill 能力
  + app metadata
  + workspace layout
  + runtime contract
  + governance metadata
  + dependency declaration
  + market distribution metadata
```

它自己可以承担入口、编排和 UI，也可以依赖其它 Skill 执行实际动作。
这些依赖必须显式声明、安装时检查，并能从当前 Hub 或能力市场下载安装。

App Studio 保存到 Skill 包时，工具型 App 可以继续使用 `binding.skill` 和 `maclaw.apps.json` 的轻量多入口格式；企业审批型/企业普通型 App 必须作为超级 Skill 的完整 `maclaw.app.json` 保存，使用 `binding.appSkill` 指向当前超级 Skill，并原样保留动态 UI layout、MIS/DataSrv 绑定、审批 workflow 绑定、dependencies.skills、workflow/result contract、governance/testEvidence。不能把企业 App 降级成只含 input/output 字段的工具型 manifest。

### 依赖类型

MaClaw App 可声明多类依赖：

```text
runtime skill
  实际动作 Skill，例如 PDF 翻译、发票识别、客户建档、报销单写入

workflow skill
  审批工作流 Skill，例如 expense-approval-workflow

ui component skill
  可复用 UI/AG UI 组件能力，例如字段映射器、文件预览器

connector skill
  外部系统连接能力，例如 ERP 同步、邮件发送、企业微信通知

policy skill
  权限、风控、校验能力，例如额度检查、合同风险检查
```

### 应用信息文件中的依赖声明

应用信息文件必须能表达依赖 Skill：

```json
{
  "schema": "maclaw.app.v1",
  "app": {
    "id": "expense-approval",
    "name": "报销审批",
    "kind": "enterprise_approval_app",
    "binding": {
      "appSkill": {
        "id": "expense-approval-app",
        "version": "1.0.0"
      }
    },
    "dependencies": {
      "skills": [
        {
          "id": "expense-approval-workflow",
          "version": ">=1.0.0 <2.0.0",
          "kind": "workflow_skill",
          "required": true,
          "source": "hub"
        },
        {
          "id": "invoice-ocr",
          "version": ">=0.3.0",
          "kind": "runtime_skill",
          "required": false,
          "source": "market"
        }
      ]
    }
  }
}
```

关键字段：

```text
id
  Skill 标识

version
  版本约束，不只固定版本

kind
  依赖用途：runtime_skill / workflow_skill / ui_component_skill / connector_skill / policy_skill

required
  是否必须安装；可选依赖缺失时应用仍可安装，但相关能力降级

source
  优先来源：local / hub / market / builtin

capabilities
  可选，声明需要该 Skill 提供哪些能力
```

### 安装时依赖检查

安装 MaClaw App 时必须执行依赖检查：

```text
1. 读取 app manifest
2. 解析 dependencies.skills
3. 检查本地已安装 Skill
4. 检查版本是否满足约束
5. 缺失或版本不满足时，从本 Hub 查询
6. Hub 没有时按策略查询能力市场
7. 展示安装计划
8. 用户确认
9. 安装/升级依赖 Skill
10. 安装 App Skill
11. 写安装记录和审计
12. 运行依赖健康检查
```

当前落地接口：

```text
PlanMaclawAppInstall(packageJSON)
  后端权威解析 maclaw.app.v1 / maclaw.app.pack.v1，汇总 appSkill 和 dependencies.skills，返回已安装、缺失、可选缺失、阻断信息。

InstallMaclawAppDependencies(packageJSON)
  安装缺失的必需 Skill 依赖；hub/skillhub 走 SkillHub，market/skillmarket 走 SkillMarket，enterprise_hub 走企业 Hub；local/builtin 等不能自动安装的来源保持 blocked。

RecordMaclawAppInstall(packageJSON, source)
  写入本机 app_install_records.json，保存 App、安装来源、包指纹、依赖快照、必需依赖是否仍缺失。重复安装同一 App 时按 app_id upsert，保留最新记录。

ListMaclawAppInstalls(limit)
  读取本机安装记录，用于后续管理页、审计页和故障诊断。

前端市场导入流程
  粘贴应用包后先预览依赖；点击安装时如有必需依赖缺失，先调用 InstallMaclawAppDependencies，再安装 MaClaw App 面板入口，并后台写安装记录。
  对标准 maclaw.app.v1 / maclaw.app.pack.v1 包，安装按钮不能只信任前端预览缓存；即使依赖预览仍在 loading，也必须针对用户当前选中的 App 子包重新调用后端 PlanMaclawAppInstall / InstallMaclawAppDependencies，确认治理、workflow contract 和必需 Skill 依赖通过后，才允许写入本地面板。

运行前健康检查
  已安装的 market/local/skill/datasrv App 如显式声明 appSkill 或 dependencies.skills，执行前先调用 PlanMaclawAppInstall 检查必需依赖；缺失时阻止运行并展示依赖错误。DataSrv 能力发现恢复出的企业应用如果带有 workflow_skill_ids、workflow_skill_versions、approval_binding_versions 或 dependencies，也必须纳入同一运行前健康检查。
```

安装计划必须可视化展示：

```text
将安装 App：报销审批
将安装依赖：expense-approval-workflow 1.2.0
将升级依赖：invoice-ocr 0.2.1 -> 0.3.4
可选依赖缺失：erp-sync，安装后可启用财务同步
风险权限变化：action:expense.submit、file:invoice.read
```

### 依赖失败处理

依赖处理规则：

```text
必需依赖缺失
  阻止安装或阻止启用

必需依赖版本不满足
  提示升级，升级失败则阻止安装

可选依赖缺失
  允许安装，但 UI 中标记相关功能不可用

依赖权限超出 App 已声明权限
  阻止安装，要求重新审核

依赖来源不可信
  进入人工确认或企业审核
```

### 运行时依赖检查

App 启动前仍需快速检查依赖状态：

```text
依赖是否存在
版本是否仍满足
Skill 是否启用
权限是否仍有效
Hub/本地运行器是否可用
```

如果依赖失效，应用不应该静默失败。
应显示业务化状态：

```text
报销审批暂不可用：审批工作流 Skill 未安装或已停用。
[安装依赖] [查看详情]
```

### 能力市场分发要求

Hub 能力市场发布 MaClaw App 时，必须带依赖清单和测试证据：

```text
app skill 包
依赖 skill 清单
依赖版本约束
依赖权限摘要
workspace layout
runtime contract
测试运行证据
依赖安装验证结果
```

市场审核需要检查：

```text
依赖是否存在
依赖是否可信
版本约束是否合理
权限是否过宽
是否能完整安装
是否能在干净环境中跑通测试
```

## App Studio 和能力市场闭环

用户的一切设计操作都必须可视化。
App Studio 不以 JSON 编辑器为主入口。

设计流程：

```text
1. 选择应用类型
   企业审批型 / 企业普通 / 工具型

2. 选择或生成 Skill
   审批型：workflow skill
   企业普通：business skill
   工具型：tool skill

3. 自动生成 UI 初稿
   根据 Skill 输入输出、工作流节点、字段、结果类型生成

4. 用户可视化调节界面
   字段顺序
   分组
   显隐
   表格列
   默认筛选
   详情区模块
   按钮位置
   输出展示方式

5. 保存应用信息文件
   maclaw.app.json / maclaw.apps.json

6. 本地测试
   工具型至少跑通一次 Skill
   企业普通至少跑通一次业务操作
   企业普通应用直连 DataSrv 的测试证据必须结构化保存 mode、target、result_status/business_status、原始 response，以及可展示的 outputs（table / business_record / dashboard / text 等），不能只保存裸 response；发布门禁和能力市场安装证据要能据此判断业务操作确实完成。
   企业审批型至少发起一条测试实例并验证实例视图
   企业审批型测试证据必须保存 approvalInstance / approval_instance：instanceId、status，以及 approvalInstanceViewVerified 或 approvalViews，证明 App Studio 已打开我的申请/待我审批/已处理/需关注之一并能看到该实例。
   审批 workflow Skill 运行完成后，run history 中的 approvalInstance 必须来自最终审批实例（approved/rejected/attention、currentNode、resultPayload、outputs/artifacts 已回填并完成 DataSrv 同步尝试后的实例），不能只保存发起时的 pending 快照；否则后续发布、安装证据和复测都会误判。
   App Studio 或后端写回 Skill 定义文件的 run evidence 时，只能刷新 runId、verifiedAt、definitionHash、artifactPresent/artifactName 等新鲜度字段；已有的 approvalInstance、resultPayload、outputs、artifacts、dependencyVerification、resultCoverage 等企业证据必须合并保留，不能被工具型的简化写回接口覆盖为空。
   如果 DataSrv 同步返回 approval_id / record_approval_id、record_id 或 dataset_id，前端必须合并回最终 approvalInstance，并在 run history / 发布证据中保留 approvalID，确保审批实例管理、审计和二次安装恢复能定位远端 RecordApproval。
   approvalInstance 子证据本身必须保存 datasetID、blueprintID、objectRole、approvalEvent、approvalWorkflowID、workflowSkillId/version、businessStatus、resultStatus、detailURL、resultPayload、outputs、artifacts 等字段；不能只依赖 run history 顶层 resultPayload/outputs/artifacts，否则 Hub/DataSrv 恢复后无法独立判断审批实例证据是否完整。
   App Studio 前端发布检查和 GUI 后端 SubmitMaclawAppPackage 必须把企业审批型应用的 approvalInstance 完整性作为能力市场门禁；缺 currentNode、workflowSkillId、businessStatus/resultStatus，或 approvalInstance 自身没有 resultPayload / outputs / artifacts 结果包时，即使顶层 testEvidence 有 resultPayload，也必须在前端禁用提交并在后端返回 review issue，阻止空壳审批证据进入 Hub。
   从 Hub 或 DataSrv 安装记录恢复审批证据时，如果 approval_instance 只有 approval_id / record_approval_id 而没有 instanceId / workflow_instance_id，也必须用远端审批 ID 作为实例定位兜底，不能丢弃整段 approvalInstance 证据。
   App Studio 发布检查必须将“审批实例证据”作为企业审批型应用的独立门禁；缺少 instanceId、status 或实例视图校验证据时，提交审核按钮不可用。提交包的 governance.testEvidence 必须携带同一份 approvalInstance 证据。
   App Studio、GUI 后端和 Hub 审核都必须把运行证据绑定到当前 App 定义：governance.testEvidence 必须携带 definitionHash / definitionFingerprint，且该值必须等于当前 app manifest、动态 UI、binding、version 与运行契约计算出的定义指纹。用户编辑应用名称、版本、binding、UI 布局、workflow/result contract 或依赖后，旧的 importedRunEvidence / run history 只能作为历史审计，不能再满足发布门禁，必须重新测试后才能上传能力市场。
   从 DataSrv app_installations 恢复已安装应用时，也必须把 metadata.test_evidence.approvalInstance / approval_instance 恢复成 importedRunEvidence.approvalInstance，避免安装后再发布时丢失审批测试证据。
   从 DataSrv app_installations 恢复已安装应用时，metadata.workspace_layout 必须是 App Studio 保存的完整动态布局契约；除 entry/template/density/navigation/list columns 外，还要保留 primaryRegion / outputRegion / regions。regions 是用户调整位置后的 canonical 布局，不允许在 DataSrv 往返、Hub 安装或二次发布时退化成只有模板名的摘要。
   DataSrv 服务端 normalize app_installations.metadata.workspace_layout 时，必须保留完整 workspace_layout.regions，并暴露 workspace_layout_primary_region、workspace_layout_output_region、workspace_layout_region_count、workspace_layout_region_ids 等轻量摘要；审计日志可以只写这些摘要字段，但能力发现接口必须返回完整 workspace_layout，供 GUI 恢复传统软件式工作台。
   DataSrv OpenAPI 的 app_installations.metadata schema 必须显式声明 workspace_layout_primary_region、workspace_layout_output_region、workspace_layout_region_count、workspace_layout_region_ids，确保 GUI、Hub 和自动化测试把动态布局位置当成稳定契约，而不是未文档化的临时 metadata。
   DataSrv app_installations 恢复 test_evidence 时必须保留完整 outputs / artifacts / result_payload；只有没有 outputs 时才允许用 output_count 生成摘要，避免企业普通应用的 table/dashboard/business_record 证据在恢复后退化成不可验证的计数。
   MaClaw App 安装注册到 DataSrv app_installations 时，也必须在 metadata.governance.test_evidence 和顶层 metadata.test_evidence 同时保留完整 outputs、artifacts、resultPayload / result_payload、approvalInstance / approval_instance（含 approvalID / approval_id、recordID / record_id 等远端定位字段）与 dependencyVerification；DataSrv 是安装后恢复、审计、二次发布的证据源，不能只写摘要字段。
   GUI 后端 RecordMaclawAppInstall 生成 install_evidence、写本地安装审计、注册 DataSrv app_installations 时，必须同一份保留 approvalInstance 内部 resultPayload、outputs、artifacts、currentNode、workflowSkillId、businessStatus/resultStatus；安装证据、DataSrv metadata、本地审计三处任一处退化，都会破坏后续复测、运行恢复和再次发布。
   DataSrv 服务端 normalize app_installations.metadata.test_evidence 时，也必须把 outputs、artifacts、approval_instance 原样保留在 canonical metadata.test_evidence 中；审计日志可以只写轻量摘要字段（如 test_evidence_approval_id、test_evidence_record_id、test_evidence_approval_status），但能力发现接口必须返回完整证据。
   DataSrv OpenAPI 的 app_installations.metadata schema 必须显式声明 test_evidence_outputs、test_evidence_artifacts、test_evidence_result_payload、test_evidence_approval_instance 及审批/依赖/结果覆盖摘要字段，确保 GUI、Hub 和自动化测试把这些字段当成稳定契约，而不是未文档化的临时 metadata。
   从 Hub/能力市场安装 MaClaw App 时，前端必须把后端 install_record.install_evidence 中的 test_evidence、dependency_verification、version_snapshot、workflow_contract 回灌到本地 AppEntry；企业审批型应用尤其要保留 importedRunEvidence.approvalInstance 及其内部 resultPayload、outputs、artifacts、workflowSkillId、businessStatus/resultStatus 等字段，避免安装后运行、复测或再次发布时丢失审批实例证据。
   DataSrv 恢复出的应用在 GUI 内部可以使用 datasrv-installed-* 包装 ID 避免与本地/市场应用冲突，但 app.manifest.datasrv.appID 必须保存真实 DataSrv app_id；提交给 PlanMaclawAppInstall、发布包、依赖验证、workflow/governance issue 归属判断时必须使用真实 app_id，不能把包装 ID 写入 MaClaw App 契约。

7. 上传 Hub 能力市场
   上传 Skill + App manifest + UI layout + 测试证据
   Hub 接收 MaClaw App 包时，必须把原始 maclaw.app.v1 entry 作为 capability version manifest 完整保存；app.ui.layouts、workspace layout regions、workflowContract、resultContract、testEvidence.outputs、testEvidence.artifacts、testEvidence.approvalInstance 都属于安装包契约，不能只进入搜索摘要。
   Hub 保存 metadata.maclaw_app_test_evidence / metadata.test_evidence 时，也必须保留 approvalInstance 内部的 resultPayload、outputs、artifacts、currentNode、workflowSkillId、businessStatus/resultStatus 等完整字段；搜索摘要可以另算，但不能覆盖或裁剪安装包证据。
   Hub capability metadata 可以用于市场列表、审核和搜索，但它不能替代安装包本体。metadata 至少要能从 governance.workspaceLayout 或 app.ui.entry/layouts[entry] 恢复 workspace_layout，并暴露 primary/output region 与 region ids/count；审核页可以看摘要，下载/安装接口必须返回完整 package。
   Hub 下载 `/api/capabilities/maclaw-apps/{id}/package` 时，只能给已 approved/published 的能力返回安装包；返回包要在原始 App entry 上追加 hub submission/review 信息，同时保留原始动态 UI、workflow contract、测试证据和依赖声明。
   Hub / DataSrv 回灌的 testEvidence 可用于安装审计和运行回放，但用于再次发布前必须重新计算当前 App 定义指纹并与 evidence.definitionHash 比对；不匹配或缺失时，GUI 发布按钮和后端 SubmitMaclawAppPackage 都必须报错，提示“当前应用定义已变更，需要重新测试”。
```

能力市场分发的不是单一脚本，而是完整 App 能力包：

```text
Skill / Workflow Skill
App manifest
workspace layout
runtime contract
test evidence
governance metadata
```

## 新架构分层

### 0. MaClaw App 分类边界

MaClaw App 是应用入口里的统一展示单位，本质上都是超级 Skill。

后端真正干活的都是 Skill。App 只是给 Skill 加上：

```text
固定入口
图标/名称/分类
默认参数
AG UI 输入输出
权限声明
发布和治理信息
```

区别不在“是不是 Skill”，而在 Skill 绑定哪些能力。

应用入口至少包含：

```text
企业审批型应用 enterprise_approval_app
  面向报销、合同、付款、采购等需要审批流的企业 MIS 场景
  本质是超级 Skill + 动态 AG UI + MIS tool + DataSrv + 审批 workflow-skill
  必须创建和展示审批实例，并反馈审批结果/文档/内容

企业普通应用 enterprise_normal_app
  面向 CRM、进销存、财务、ERP、库存等一次性企业业务操作
  本质是超级 Skill + 动态 AG UI + MIS tool + DataSrv
  不创建审批实例

工具应用 tool_app
  面向文件处理、文档生成、PDF、图片、数据清洗、爬取、分析等工具场景
  本质也是超级 Skill，但只绑定普通 skill 能力
  不依赖 DataSrv
  不关联 MIS 审批工作流

自动化应用 automation_app
  面向定时任务、同步任务、监控任务、批处理
  本质也是超级 Skill 的自动化包装
  可选择调用 MIS tool，但不是默认 MIS 应用
```

所以审批实例只属于企业审批型应用：

```text
企业审批型应用必须有关联审批 workflow-skill
企业普通应用不出现审批实例管理
工具应用不应该出现审批配置
普通 Skill 不应该因为 App 体系而被迫理解 MIS
```

manifest 边界：

```json
{
  "kind": "enterprise_approval_app",
  "binding": {
    "appSkill": {"id": "mis-expense-claim", "source": "hub"},
    "mis": {
      "appId": "mis.expense",
      "requiredRoles": ["expense_report"],
      "requiredScopes": ["action:expense.submit"],
      "approvalBindings": [
        {"event": "expense.submitted", "workflowSkillId": "expense_approval", "objectRole": "expense_report"}
      ]
    }
  },
  "dependencies": {
    "skills": [
      {"id": "expense_approval", "kind": "workflow_skill", "required": true, "source": "hub"}
    ]
  }
}
```

工具应用：

```json
{
  "kind": "tool_app",
  "binding": {
    "skill": {"id": "pdf-summarizer"}
  }
}
```

自动化应用：

```json
{
  "kind": "automation_app",
  "binding": {
    "automation": {"id": "nightly-sync"}
  }
}
```

设计原则：

- `binding.mis` 只出现在企业应用。
- `approvalBindings` 只出现在企业应用的 `binding.mis` 下。
- 工具应用不依赖 DataSrv token，除非它显式声明需要 MIS 能力。
- App Studio 根据 app kind 展示不同配置面板。

### 1. DataSrv：语义数据底座

DataSrv 不只是表存储。

它要维护：

- `BusinessObjectCatalog`：客户、订单、报销单、库存、付款等业务对象。
- `RoleBinding`：`expense_report -> expense.reports`。
- `RelationshipCatalog`：对象间关系。
- `FieldAliasCatalog`：业务字段和真实字段映射。
- `Blueprint`：CRM、进销存、报销、财务、ERP 模板。
- `AppInstallation`：某个 tenant 安装了哪些 MIS 应用。
- `ChangePlan`：所有结构变更先预览。
- `ApprovalBinding`：业务事件到独立审批工作流的绑定。
- `AuditLog`：所有读写、审批、付款留痕。

### 2. MIS Tools：统一访问层

Agent 和 Skill 都只能通过 MIS Tool 操作企业数据。

第一批工具：

```text
mis.app.list_templates
mis.app.preview
mis.app.create
mis.app.get
mis.app.check_access

mis.object.list
mis.object.resolve
mis.object.describe

mis.data.query
mis.data.get_record
mis.data.upsert_record
mis.data.execute_action

mis.change.plan
mis.change.apply

mis.graph.get
mis.import.preview
mis.import.apply
```

工具入参使用业务语义：

```text
app_id
object_role
action_role
field_alias
actor
data
```

不让 Skill 传真实 dataset。

### 3. Skill Generator：业务 Skill 生成器

Skill 是业务执行单元。

Skill Generator 输入：

- 用户业务需求。
- 已安装 MIS 应用。
- DataSrv 蓝图。
- 业务对象目录。
- 可用 view/action。
- 用户确认的映射。

输出：

- `SKILL.md`
- skill manifest
- AG UI schema
- workflow steps
- MIS tool call plan
- tests
- MaClaw App Entry

### 4. MaClaw App：超级 Skill

所有 MaClaw App 本质都是：

```text
可放到应用入口的超级 Skill
```

分类区别：

```text
enterprise_approval_app = Skill + AG UI + binding.mis + 审批 workflow-skill + 审批实例管理
enterprise_normal_app = Skill + AG UI + binding.mis + 一次性企业业务操作
tool_app = Skill + AG UI + 文件/表单/artifact 输入输出
automation_app = Skill + 触发器/计划/运行记录
```

所有 App 都不直接干活，后端工作由 skill 执行。

企业审批型应用和企业普通应用都不直接访问 DataSrv，而是通过 Skill 调 MIS tool。

App 绑定：

```json
{
  "binding": {
    "appSkill": {"id": "mis-expense-claim", "source": "hub"},
    "mis": {
      "appId": "mis.expense",
      "requiredRoles": ["employee", "expense_report", "approval", "payment"],
      "requiredScopes": ["action:expense.submit", "view:expense.my_reports"]
    }
  }
}
```

### 5. AG UI：企业应用交互层

第一版支持：

- `form`
- `table`
- `resource_picker`
- `approval`
- `result_browser`
- `field_mapper`

后续支持：

- `relationship_graph`
- `dashboard`
- `timeline`

这主要服务 `enterprise_approval_app` 和 `enterprise_normal_app`。

`tool_app` 可以继续使用更轻的 Skill 表单/文件输入输出，不需要完整企业应用布局。

### 6. App Studio：按应用类型分流

App Studio 不再以 manifest JSON 编辑为核心。

它先让用户选择 App 类型：

```text
企业应用
工具应用
自动化应用
```

企业应用流程：

```text
描述业务需求
  -> 选择模板或已有 MIS 应用
  -> 确认业务对象
  -> 确认缺失字段/关系
  -> 生成 Skill Plan
  -> 预览 AG UI
  -> dry-run 测试
  -> 发布到应用入口
```

工具应用流程：

```text
选择/创建普通 Skill
  -> 配置输入模式：文件 / 表单 / 混合
  -> 配置输出：文本 / 文件 / artifact
  -> 预览简单 AG UI
  -> 测试运行
  -> 发布到应用入口
```

自动化应用流程：

```text
选择触发器
  -> 选择任务 skill/tool
  -> 设置计划/条件
  -> 测试一次
  -> 启用
```

只有企业应用面板出现：

- MIS 应用模板。
- 业务对象。
- `binding.mis`。
- 审批工作流绑定。
- DataSrv 权限检查。

## 核心数据模型

### BusinessObject

```json
{
  "role": "expense_report",
  "title": "报销单",
  "aliases": ["报销", "报销申请", "费用报销"],
  "dataset": "expense.reports",
  "display_field": "report_no",
  "search_fields": ["report_no", "applicant_name", "description"],
  "fields": [
    {"key": "applicant_ref", "title": "报销人", "type": "record_ref", "target_role": "employee"},
    {"key": "amount", "title": "金额", "type": "money"},
    {"key": "status", "title": "状态", "type": "enum"}
  ]
}
```

### AppInstallation

```json
{
  "app_id": "mis.expense",
  "blueprint_id": "mis.expense",
  "kind": "enterprise_approval_app",
  "role_bindings": {
    "employee": "company.users",
    "department": "company.departments",
    "expense_report": "expense.reports",
    "expense_item": "expense.items",
    "approval": "workflow.approvals",
    "payment": "finance.payments"
  },
  "metadata": {
    "app_skill_id": "mis-expense-claim",
    "workflow_skill_ids": ["approval-expense"],
    "dependencies": [
      {"id": "mis-expense-claim", "kind": "runtime_skill", "required": true, "source": "hub", "health": "ready"},
      {"id": "approval-expense", "kind": "workflow_skill", "required": true, "source": "hub", "health": "ready"}
    ],
    "dependency_count": 2,
    "has_missing_required_dependency": false,
    "has_blocking_dependency": false,
    "dependency_verification": {
      "schema": "maclaw.app.install_plan.v1",
      "verified_at": "2026-06-18T10:00:00Z",
      "dependency_count": 2,
      "has_missing_required": false,
      "has_blocking_dependency": false,
      "dependencies": [
        {"id": "mis-expense-claim", "kind": "runtime_skill", "installed": true, "health": "ready"},
        {"id": "approval-expense", "kind": "workflow_skill", "installed": true, "health": "ready"}
      ]
    }
  }
}
```

### ApprovalBinding

```json
{
  "event": "expense.submitted",
  "record_role": "expense_report",
  "workflow_id": "expense_approval",
  "workflow_version": "1.0.0",
  "input_mapping": {
    "applicant": "expense_report.applicant_ref",
    "amount": "expense_report.amount",
    "department": "expense_report.department_ref"
  },
  "on_approved": {"set_status": "pending_finance_payment"},
  "on_rejected": {"set_status": "rejected"}
}
```

## 权限模型

全局 MIS 设置保存 DataSrv URL/token。

每次调用还要传 actor。

```text
token = 调用凭证
actor = 当前业务操作人
scope = 可调用范围
approval_workflow = 独立审批工作流是否通过
```

DataSrv 校验顺序：

```text
1. token 是否有效
2. scope 是否允许
3. actor 是否存在
4. object/action 是否存在
5. 业务规则是否通过
6. 若动作需要审批，是否已有关联审批工作流通过结果
7. dry-run 或正式执行
8. 写审计
```

## API 规划

新 API 用 `/api/v1/mis/*`。

```http
GET  /api/v1/mis/apps/templates
POST /api/v1/mis/apps/preview
POST /api/v1/mis/apps
GET  /api/v1/mis/apps/{appId}
POST /api/v1/mis/apps/{appId}/access/check

GET  /api/v1/mis/apps/{appId}/objects
POST /api/v1/mis/apps/{appId}/objects/resolve
GET  /api/v1/mis/apps/{appId}/objects/{role}

POST /api/v1/mis/apps/{appId}/data/query
POST /api/v1/mis/apps/{appId}/data/upsert
POST /api/v1/mis/apps/{appId}/actions/{actionRole}/execute

POST /api/v1/mis/apps/{appId}/changes/plan
POST /api/v1/mis/apps/{appId}/changes/{planId}/apply

GET  /api/v1/mis/apps/{appId}/graph
```

## 重构阶段

### Phase 1：DataSrv 语义层

- 加 `BusinessObjectCatalog`。
- 加 `RoleBinding`。
- 加 `AppInstallation`。
- 加 object resolve API。
- 做 `mis.expense` 和 `mis.inventory` 两个蓝图。

验收：

```text
用户选择“报销”
系统能预览会创建/复用哪些业务对象
安装后能通过 object_role 找到真实 dataset
```

### Phase 2：MIS Tool 层

- 增加 `mis.*` tool registry。
- agent 和 skill 共用。
- tool 内部读取全局 MIS 设置。
- 所有 tool 支持 actor。

验收：

```text
agent 和 skill 都能用 mis.data.query(object_role=expense_report)
不需要知道真实表名
```

### Phase 3：Skill Generator

- 从模板生成 Skill Plan。
- 从自然语言生成 Skill Plan。
- 生成 AG UI schema。
- 生成 tests。
- 先支持报销申请、库存盘点。

验收：

```text
输入“做一个报销申请 App”
输出可运行 skill + AG UI + app entry
```

### Phase 4：MaClaw App 超级 Skill

- `enterprise_approval_app` 绑定 `appSkill + mis + workflow_skill + approvalBindings`。
- `enterprise_normal_app` 绑定 `appSkill + mis`，不创建审批实例。
- App 入口启动 skill。
- 启动前做 `mis.app.check_access`。
- App 不保存 DataSrv token。

验收：

```text
应用入口点击“报销申请”
打开 AG UI
提交后 skill 调 MIS tool 写 DataSrv
```

### Phase 5：AG UI / App Studio

- App Studio 增加应用生成向导。
- 支持业务对象选择。
- 支持 AG UI 预览。
- 支持发布到应用入口。

验收：

```text
非技术用户能通过向导创建“客户跟进”App 草稿
```

### Phase 6：审批闭环

- 标准化 `company.users`、部门、主管关系。
- 复用现有 Workflow V2 作为业务流程编排引擎。
- 扩展 DataSrv `RecordApproval` 为 MIS 单据审批事实记录。
- 报销审批和付款跑通。

验收：

```text
主管能审批下属报销
非主管不能审批
超额度不能审批
所有动作有审计
```

## 现有审批/工作流 Review 结论

当前代码里已经有两类审批相关能力，重构时不能重复造轮子。

更正后的结论：

```text
审批流程设计
  复用现有独立审批工作流和工作流设计器

审批流程执行
  复用现有工作流/VE 审批能力

业务数据落账
  由 DataSrv 负责单据、状态、审批结果、审计、timeline

MaClaw App / Skill
  只发起审批、展示待办和结果，不自己实现审批流
```

### 1. GUI Workflow V2

现有 Workflow V2 是状态机式流程引擎。

已有能力：

- workflow template。
- phase。
- `NeedsConfirm` 确认门。
- `ToolPolicy` 工具限制。
- structured form / AG UI 表单收集。
- review intent：confirm / supplement / skip / cancel。
- workflow progress event。
- AgentView / AG UI 集成。

适合承担：

```text
业务流程编排
多步骤流转
人机交互
AG UI 表单/确认/结果
Skill 执行过程控制
```

不适合直接承担：

```text
企业单据事实存储
财务/库存/报销主数据
跨应用业务对象目录
最终审计账本
```

### 2. 独立审批工作流和工作流设计器

现有系统已经有独立审批工作流能力，不应该在 MIS 重构里重做。

已发现能力：

- 工作流设计器入口：`VirtualEmployeeSettingsPanel` 可打开 Hub visual workflow designer。
- 审批节点支持 VE approver。
- `veApprovalCapabilityCheck.ts` 会在设计器里校验 VE 是否具备审批能力。
- `ValidateVEApproverAssignment` 后端绑定用于防止把无审批能力的 VE 分配为审批人。
- `WorkflowDirectoryPanel` / `InstanceConfirmationPanel` 已有工作流目录和实例确认 UI。
- A2A/Hub 消息里已有 approval workflow request / decision 结构。

这说明审批工作流应该是独立产品能力：

```text
流程设计：工作流设计器
审批节点：人工 / VE / fallback approver
执行实例：工作流运行时
决策消息：approve / reject / timeout / fallback
```

MIS 要做的是接入：

```text
报销单提交
  -> 发起审批工作流实例
  -> 传入业务上下文和 DataSrv record ref
  -> 审批工作流决定通过/驳回
  -> DataSrv 根据结果更新单据状态并写审计
```

不是在 DataSrv 里画审批流。

### 2.1 审批定义、审批实例、业务记录三层

审批 workflow-skill 是流程定义，不是审批实例。

必须分三层：

```text
Approval Workflow Skill
  审批流程定义
  由工作流设计器创建
  描述节点、条件、审批人、VE、超时、fallback

Approval Instance
  一次具体审批运行
  由某个企业 App/Skill 触发
  保存节点轨迹、审批人、决策、时间、评论、附件、超时、fallback

Business Record / DataSrv Approval Link
  业务单据和审批实例的关联
  保存 record_role、record_id、workflow_skill_id、approval_instance_id、最终结果、业务状态、审计
```

工具类 App 不需要这套保存。

只有企业 App 触发需要审批的业务动作时，才创建审批实例。

### 2.2 审批实例保存在哪里

完整审批实例应由审批工作流系统保存。

它包含：

```text
instance_id
workflow_skill_id
workflow_version
trigger_event
started_by
started_at
status
current_node
nodes[]
decisions[]
comments[]
attachments[]
timeouts[]
fallbacks[]
completed_at
final_decision
```

因为工作流节点不固定，不能用固定列保存“第一审批人、第二审批人、第三审批人”。

审批实例存储要用三类结构：

```text
WorkflowInstance
  实例头：instance_id、workflow_skill_id、版本、状态、发起人、当前节点

WorkflowNodeInstance[]
  可变节点实例：每个节点一条记录，数量不限

WorkflowEventLog[]
  事件日志：节点进入、分派、审批、驳回、转交、加签、超时、fallback、完成
```

推荐结构：

```json
{
  "instance_id": "appr_inst_123",
  "workflow_skill_id": "approval-expense",
  "workflow_version": "1.0.0",
  "definition_snapshot": {
    "nodes": [],
    "edges": []
  },
  "status": "running",
  "current_node_ids": ["manager_approval"],
  "business_ref": {
    "app_id": "mis.expense",
    "object_role": "expense_report",
    "record_id": "EXP-001"
  },
  "nodes": [
    {
      "node_instance_id": "node_inst_001",
      "node_id": "manager_approval",
      "title": "主管审批",
      "type": "approval",
      "status": "pending",
      "assignees": [{"type": "user", "id": "u_lijingli"}],
      "entered_at": "2026-06-18T09:10:00Z"
    }
  ],
  "events": [
    {
      "event_id": "evt_001",
      "type": "node_entered",
      "node_instance_id": "node_inst_001",
      "at": "2026-06-18T09:10:00Z"
    }
  ]
}
```

关键点：

- `definition_snapshot` 保存发起时的流程定义快照，防止流程后来被改导致旧实例解释不清。
- `nodes[]` 数量不限，支持任意多节点。
- `current_node_ids[]` 支持并行审批。
- `events[]` 是完整审计轨迹。
- 节点动态加签/转交/fallback 也只是新增 node/event，不改固定 schema。

DataSrv 不复制完整节点图和轨迹。

DataSrv 只保存业务关联索引和结果摘要：

```json
{
  "app_id": "mis.expense",
  "object_role": "expense_report",
  "record_id": "EXP-20260618-001",
  "workflow_skill_id": "approval-expense",
  "workflow_version": "1.0.0",
  "approval_instance_id": "appr_inst_123",
  "trigger_event": "expense.submitted",
  "status": "approved",
  "final_decision": "approved",
  "started_by": "u_zhangsan",
  "completed_by": "u_lijingli",
  "started_at": "2026-06-18T09:00:00Z",
  "completed_at": "2026-06-18T11:00:00Z",
  "from_status": "submitted",
  "to_status": "pending_finance_payment"
}
```

### 2.3 为什么 DataSrv 还要保存关联

因为企业 MIS 查询通常从业务单据出发：

```text
查看这张报销单审批到哪了？
谁审批的？
审批结果是什么？
为什么现在是 pending_finance_payment？
审计链路在哪里？
```

DataSrv 要能回答：

```text
这张单据关联 approval_instance_id=appr_inst_123
最终结果 approved
业务状态从 submitted 变成 pending_finance_payment
完整审批轨迹请查看审批实例
```

所以 DataSrv 保存的是索引和落账事实，不是流程引擎本体。

### 2.4 推荐数据模型：ApprovalLink

建议新增 `ApprovalLink` 或扩展 `RecordApproval` 成语义化关联记录。

```json
{
  "id": "approval_link_001",
  "tenant_id": "default",
  "app_id": "mis.expense",
  "object_role": "expense_report",
  "dataset_id": "expense.reports",
  "record_id": "EXP-20260618-001",
  "trigger_event": "expense.submitted",
  "workflow_skill_id": "approval-expense",
  "workflow_version": "1.0.0",
  "approval_instance_id": "appr_inst_123",
  "status": "pending",
  "final_decision": "",
  "started_by": "u_zhangsan",
  "started_at": "2026-06-18T09:00:00Z",
  "completed_at": "",
  "result_payload": {},
  "audit_id": "audit_001"
}
```

审批完成后更新：

```json
{
  "status": "approved",
  "final_decision": "approved",
  "completed_at": "2026-06-18T11:00:00Z",
  "result_payload": {
    "approved_by": "u_lijingli",
    "comment": "同意，金额合理"
  }
}
```

### 2.5 运行链路

```text
1. 用户在企业 App 提交业务动作
2. Skill 写业务单据到 DataSrv
3. Skill 发出业务事件 expense.submitted
4. MIS tool 根据 approvalBindings 找 workflow_skill_id
5. MIS tool 调审批工作流系统创建 Approval Instance
6. 审批系统返回 approval_instance_id
7. DataSrv 保存 ApprovalLink/RecordApproval 关联
8. 审批系统运行审批节点
9. 审批完成后回调或由 MIS tool sync_result
10. DataSrv 更新 ApprovalLink 状态
11. DataSrv 按 onApproved/onRejected 更新业务单据状态
12. 写 AuditLog / Timeline
```

### 2.6 查询链路

企业 App 查询审批状态：

```text
mis.approval.get(app_id, object_role, record_id)
```

返回：

```json
{
  "record_id": "EXP-20260618-001",
  "approval_instance_id": "appr_inst_123",
  "status": "pending",
  "current_node": "主管审批",
  "assignee": "李经理",
  "started_at": "2026-06-18T09:00:00Z",
  "detail_url": "approval://instances/appr_inst_123"
}
```

如果用户要看完整轨迹，打开审批实例详情，由审批系统/工作流 UI 展示。

### 2.7 审批查看入口

带审批的企业 App 必须解决“从哪里看审批”。

建议有五个入口。

#### 入口 0：应用面板固定入口

需要有面板入口。

应用入口里建议内置一个固定企业 App：

```text
审批中心
```

它显示在企业应用分类里，类似：

```text
企业应用
  报销申请
  采购申请
  合同审批
  审批中心
```

审批中心不是设计器，是运行入口。

普通用户从这里处理：

```text
待我审批
我发起的
我已处理
抄送我的
超时/异常
```

实施/管理员从这里查看：

```text
全部审批
异常实例
超时实例
按流程/业务类型筛选
```

工作流设计器入口不要放在这里第一层。

设计器属于管理/配置：

```text
审批中心 -> 管理 -> 打开工作流设计器
```

或：

```text
Settings / VE Approval / Workflow Designer
```

#### 入口 A：企业 App 内查看

普通用户最常用。

例如“报销申请” App 内有：

```text
我的报销
待我处理
全部报销（管理员/财务）
```

列表显示：

```text
单号
金额
当前状态
当前审批节点
当前处理人
提交时间
审批结果
```

点开单据详情：

```text
报销内容
明细
附件
审批进度
审批评论
操作按钮
```

企业 App 通过：

```text
mis.approval.get
mis.approval.list_by_record
mis.approval.my_pending
```

获取审批摘要。

#### 入口 B：统一审批中心

给主管、财务、管理员。

应用入口里需要有一个企业 App：

```text
审批中心
```

它本质也是超级 Skill。

功能：

```text
待我审批
我发起的
我已审批
抄送我的
超时/异常
按业务类型筛选：报销、采购、付款、合同
```

它不替代审批工作流系统，而是统一聚合入口。

底层查询：

```text
审批系统实例待办
DataSrv ApprovalLink
DataSrv 业务单据摘要
```

#### 入口 C：单据详情里的审批轨迹

任何业务单据详情页都要有审批卡片：

```text
审批状态：主管审批中
当前处理人：李经理
提交时间：2026-06-18 09:00
最近意见：同意/驳回原因
[查看完整流程]
```

“查看完整流程”打开审批实例详情：

```text
approval://instances/appr_inst_123
```

完整节点轨迹由审批工作流 UI 展示。

#### 入口 D：通知和待办

审批节点变化要进入通知/待办。

事件：

```text
approval.instance.started
approval.task.assigned
approval.task.approved
approval.task.rejected
approval.instance.completed
approval.instance.timeout
```

通知示例：

```text
你有一条报销审批待处理：张三 1280 元。
```

点通知打开：

```text
审批中心 -> 对应审批任务
```

或：

```text
报销申请 App -> 单据详情
```

### 2.8 审批查看数据边界

查看分两类。

DataSrv 提供业务摘要：

```json
{
  "record_id": "EXP-001",
  "record_title": "张三差旅报销",
  "amount": 1280,
  "business_status": "pending_manager_approval",
  "approval_status": "pending",
  "current_node": "主管审批",
  "current_assignee": "李经理",
  "approval_instance_id": "appr_inst_123"
}
```

审批系统提供完整流程轨迹：

```json
{
  "approval_instance_id": "appr_inst_123",
  "nodes": [
    {"name": "提交", "actor": "张三", "time": "..."},
    {"name": "主管审批", "actor": "李经理", "decision": "approved", "comment": "同意"}
  ]
}
```

企业 App 默认展示 DataSrv 摘要。

用户点“完整流程”才进入审批系统详情。

### 3. DataSrv RecordApproval

DataSrv 现在已有 `RecordApproval`：

```text
CreateRecordApproval
ListRecordApprovals
GetRecordApproval
ReviewRecordApproval
MIS Inbox
Timeline
AuditLog
```

当前模型：

```json
{
  "dataset_id": "expense.reports",
  "record_id": "EXP-001",
  "status": "pending",
  "kind": "finance",
  "assigned_to": "finance_manager",
  "decision": "approve",
  "reason": "...",
  "created_by": "...",
  "reviewed_by": "..."
}
```

适合承担：

```text
单据审批记录
审批状态事实
审批审计
待办 inbox
record timeline
```

当前不足：

- 主要按 `dataset_id/record_id` 工作，不懂业务 `object_role`。
- 不应承担完整审批流程设计。
- 不应替代独立审批工作流实例。
- 需要能记录外部审批工作流实例和节点结果。

### 4. 正确分工

不要再造第三套审批，也不要把审批流硬编码进 DataSrv。

采用：

```text
独立审批工作流 / 工作流设计器
  负责“审批流程怎么设计、怎么流转、谁审批、VE 如何参与”

DataSrv RecordApproval
  负责记录“哪张业务单据关联了哪个审批实例，审批结果是什么”

MIS Tool
  负责把 skill/app 调用转成“发起审批工作流 + 更新 DataSrv 单据”

AG UI
  负责显示待办、表单、审批确认、结果

DataSrv
  负责业务对象、单据状态、数据校验、审计、timeline
```

### 5. 报销审批目标链路

```text
用户打开“报销申请” MaClaw App
  -> 启动 mis-expense-claim skill
  -> AG UI 填报销单
  -> mis.data.upsert_record(object_role=expense_report, status=draft)
  -> 用户提交
  -> mis.approval.start(workflow_id=expense_approval)
  -> 审批工作流实例创建
  -> DataSrv 记录 RecordApproval(workflow_instance_id, status=pending)
  -> 主管/VE 在审批工作流中处理
  -> 审批工作流返回 decision=approved/rejected
  -> mis.approval.sync_result 写回 DataSrv
  -> 更新 RecordApproval approved/rejected
  -> 更新报销单 status=pending_finance_payment 或 rejected
  -> 财务付款可进入另一个付款审批工作流或付款确认 App
  -> 更新报销单 status=paid
  -> 全程写 AuditLog / Timeline
```

### 6. API/Tool 调整

新增 MIS tool，对接独立审批工作流，不直接让 Skill 操作底层 dataset approval：

```text
mis.approval.start
mis.approval.list
mis.approval.get
mis.approval.sync_result
mis.approval.my_inbox
```

入参使用业务语义：

```json
{
  "app_id": "mis.expense",
  "object_role": "expense_report",
  "record_id": "EXP-001",
  "approval_workflow_id": "expense_approval",
  "actor": {"user_id": "u_manager"}
}
```

MIS tool 内部解析：

```text
object_role -> dataset_id
approval_workflow_id -> Hub/Workflow Designer 定义
record_id -> DataSrv 单据引用
actor -> 当前提交人
workflow result -> DataSrv RecordApproval + record status
```

### 7. DataSrv 需要扩展，不是推倒

保留并扩展 `RecordApproval`：

```text
新增：
  app_id
  object_role
  approval_workflow_id
  workflow_instance_id
  workflow_node_id
  workflow_decision_id
  submitted_by
  current_assignee
  current_assignee_type
  from_status
  to_status
```

保留：

```text
dataset_id
record_id
status
kind
assigned_to
created_by
reviewed_by
audit
timeline
inbox
```

这样能兼容 DataSrv 已有审计/inbox/timeline，又能关联独立审批工作流实例。

### 8. 工作流设计器需要暴露给 MIS 模板绑定

DataSrv 蓝图和 Skill Generator 不生成审批流程图本身，而是引用已有审批工作流模板。

例如报销模板：

```json
{
  "blueprint_id": "mis.expense",
  "approval_bindings": [
    {
      "event": "expense.submitted",
      "workflow_id": "expense_approval",
      "record_role": "expense_report",
      "on_approved": {"set_status": "pending_finance_payment"},
      "on_rejected": {"set_status": "rejected"}
    },
    {
      "event": "expense.payment_requested",
      "workflow_id": "expense_payment_approval",
      "record_role": "expense_report",
      "on_approved": {"set_status": "paid"}
    }
  ]
}
```

App Studio / Skill Generator 需要能做：

```text
选择已有审批工作流
检查审批工作流输入参数是否匹配 MIS 对象
把 record ref、申请人、金额、部门等上下文传给工作流
保存 approval_bindings
```

工作流设计器仍负责：

```text
审批节点
审批人/VE
条件分支
超时
fallback approver
加签/转交
```

### 9. 设计原则

- 不新造孤立审批引擎。
- 独立审批工作流和设计器管审批流程。
- DataSrv 管业务单据事实和审计。
- AG UI 管交互。
- Skill 管业务动作编排。
- MIS tool 是唯一桥。
- DataSrv 只引用审批工作流实例和决策结果，不复制审批图。

## 第一条样板：报销申请

覆盖：

- 员工
- 部门
- 报销单
- 报销明细
- 附件
- 审批
- 付款

流程：

```text
员工提交
  -> 主管审批
  -> 财务付款
  -> 员工查询记录
```

## 第二条样板：库存盘点

覆盖：

- 商品
- 仓库
- 库存
- 库存流水

流程：

```text
选择仓库
  -> 查询库存
  -> 录入盘点数
  -> dry-run 差异
  -> 确认
  -> 写库存流水
```

## 不做的事

第一版不做：

- 复杂低代码平台。
- 任意拖拽生成全系统。
- 跨 tenant 数据共享。
- 完整财务总账。
- 复杂 BPMN 工作流。
- App 内独立 token 管理。

## 重构护栏

这些是重构时不能省的基础约束。

### 1. 租户和组织模型先定

审批、权限和数据隔离都依赖组织模型。

第一版至少要标准化：

```text
tenant
organization
department
user
position
manager
approval_role
```

推荐最小关系：

```text
tenant -> organization -> department -> user
user -> manager
user -> approval_role
user -> approval_limit
```

否则无法判断：

- 谁能看哪些数据。
- 谁是谁的主管。
- 谁能审批哪类单据。
- 金额超过多少需要更高级审批。

### 2. 必须有内置种子数据

端到端样板不能空跑。

报销样板需要：

```text
用户：
  张三：员工
  李经理：主管
  财务王五：财务

部门：
  销售部
  财务部

关系：
  张三.manager = 李经理
  李经理.approval_limit = 5000
  财务王五.approval_role = finance_payable
```

库存样板需要：

```text
商品：
  A 商品
  B 商品

仓库：
  主仓

库存：
  A 商品 主仓 100
  B 商品 主仓 50
```

验收必须用这些种子数据跑通。

### 3. App/Skill 必须有测试协议

生成的 Skill 不能只保存代码或 prompt。

每个 Skill 必须带：

```text
sample_input
expected_tool_calls
expected_output
required_roles
required_scopes
risk_level
```

示例：

```json
{
  "sample_input": {
    "applicant": "张三",
    "amount": 1280,
    "description": "客户拜访差旅"
  },
  "expected_tool_calls": [
    {"tool": "mis.app.check_access"},
    {"tool": "mis.data.upsert_record", "object_role": "expense_report"},
    {"tool": "mis.data.execute_action", "action_role": "submit_expense"}
  ],
  "expected_output": {
    "status": "pending_manager_approval"
  }
}
```

没有测试协议的 Skill 不能发布成 MaClaw App。

### 4. 所有核心对象必须版本化

必须版本化：

```text
blueprint_version
component_version
business_object_version
object_schema_version
skill_version
app_entry_version
approval_policy_version
approval_workflow_version
```

版本用途：

- 模板升级。
- 字段新增。
- Skill 兼容旧对象。
- App 回滚。
- 审批工作流绑定和版本变更可追溯。

安装记录要保存当时版本：

```json
{
  "app_id": "mis.expense",
  "blueprint_id": "mis.expense",
  "blueprint_version": "1.0.0",
  "installed_at": "2026-06-18T10:00:00Z",
  "role_bindings": {
    "expense_report": "expense.reports"
  }
}
```

### 5. 错误必须业务化解释

MIS tool 和 DataSrv 不能只返回技术错误。

不合格：

```text
403 forbidden
validation failed
policy denied
```

合格：

```text
李经理不能审批该报销，因为金额 8000 超过审批额度 5000。
```

错误结构：

```json
{
  "code": "approval_limit_exceeded",
  "message": "李经理不能审批该报销，因为金额 8000 超过审批额度 5000。",
  "actor": "李经理",
  "target": "报销单 EXP-20260618-001",
  "required": "approval_limit >= 8000",
  "actual": "approval_limit = 5000",
  "next_actions": [
    {"label": "提交给上级主管", "action": "route_to_next_approver"},
    {"label": "退回申请人修改金额", "action": "reject_for_revision"}
  ]
}
```

AG UI 应展示 `message` 和可选 `next_actions`。

### 6. 安全边界必须明确

明确禁止：

```text
Skill 保存 DataSrv token
MaClaw App 保存 DataSrv token
前端绕过 MIS tool 直接写 DataSrv
AI 直接 apply 高风险 schema 变更
低置信度对象映射自动提交
审批 action 只靠 token scope 放行
```

必须执行：

```text
DataSrv URL/token 只在全局 MIS 设置保存
Skill/App 只声明 requiredRoles/requiredScopes
所有写入经过 MIS tool
所有结构变更经过 ChangePlan
所有审批经过独立审批工作流
所有关键动作写 AuditLog
```

## 最终目标

用户只需要懂业务：

```text
我要报销
我要查库存
我要建客户
我要审批付款
```

系统负责：

```text
找业务对象
找真实数据
生成 Skill
生成 AG UI
校验权限
执行审批
写审计
发布 App
```

## 企业 App + 审批工作流完整流程

这一节固定作为后续产品和研发对齐用的主流程。

### 1. 先有 DataSrv 语义底座

DataSrv 提供企业数据和业务对象映射：

```text
业务对象：
  员工
  报销单
  采购单
  合同
  库存

role_bindings：
  employee -> company.users
  expense_report -> expense.reports
  purchase_order -> procurement.purchase_orders

审批关联：
  业务事件 -> approval workflow-skill
```

DataSrv 不负责画审批流程，但负责业务对象、单据、状态、审计和审批实例关联。

### 2. 先有审批 workflow-skill 定义

工作流设计器创建审批 workflow-skill：

```text
approval-expense
  主管审批
  财务复核
  超时 fallback
  VE 可审批/不可审批
```

它是流程定义，不是审批实例。

保存内容包括：

```text
节点
条件
审批人/审批角色
VE 审批能力
超时规则
fallback approver
输出决策：approved / rejected / cancelled
```

### 3. 创建企业 App

App Studio 创建企业 App：

```text
App：报销申请
Skill：mis-expense-claim
MIS 应用：mis.expense
审批绑定：
  expense.submitted -> approval-expense
```

用户/管理员确认：

```text
提交报销时使用哪个审批 workflow-skill？
需要传给审批流哪些字段？
审批通过/驳回后业务状态怎么变？
```

保存成：

```json
{
  "kind": "enterprise_approval_app",
  "binding": {
    "appSkill": {"id": "mis-expense-claim", "source": "hub"},
    "mis": {
      "appId": "mis.expense",
      "approvalBindings": [
        {
          "event": "expense.submitted",
          "workflowSkillId": "approval-expense",
          "recordRole": "expense_report",
          "inputMapping": {
            "applicant": "expense_report.applicant_ref",
            "department": "expense_report.department_ref",
            "amount": "expense_report.amount",
            "reason": "expense_report.description",
            "attachments": "expense_report.attachments"
          },
          "onApproved": {"setStatus": "pending_finance_payment"},
          "onRejected": {"setStatus": "rejected"}
        }
      ]
    }
  },
  "dependencies": {
    "skills": [
      {"id": "approval-expense", "kind": "workflow_skill", "required": true, "source": "hub"}
    ]
  }
}
```

### 4. 用户运行企业审批型应用

用户从应用入口打开：

```text
企业审批型应用 -> 报销申请
```

AG UI 展示业务表单：

```text
报销人
金额
事由
报销明细
附件
```

用户不需要知道：

```text
dataset
field key
workflow instance
approval link
```

### 5. Skill 写业务数据

Skill 调 MIS tool：

```json
{
  "tool": "mis.data.upsert_record",
  "args": {
    "app_id": "mis.expense",
    "object_role": "expense_report",
    "data": {
      "报销人": "张三",
      "金额": 1280,
      "事由": "客户拜访差旅"
    }
  }
}
```

MIS tool 解析：

```text
expense_report -> expense.reports
报销人 -> applicant_ref
张三 -> company.users record id
```

DataSrv 保存报销单：

```text
status = draft / submitted
```

### 6. Skill 发起审批

用户点击提交。

Skill 发出业务事件：

```text
expense.submitted
```

MIS tool 查审批绑定：

```text
expense.submitted -> approval-expense
```

然后创建审批实例：

```text
approval_instance_id = appr_inst_123
```

审批实例由审批工作流系统保存。

### 7. DataSrv 保存审批关联

DataSrv 保存 `ApprovalLink`：

```json
{
  "app_id": "mis.expense",
  "object_role": "expense_report",
  "record_id": "EXP-001",
  "workflow_skill_id": "approval-expense",
  "approval_instance_id": "appr_inst_123",
  "trigger_event": "expense.submitted",
  "status": "pending"
}
```

同时业务单据状态变为：

```text
pending_manager_approval
```

### 8. 审批系统跑流程

审批 workflow-skill 实例运行：

```text
主管审批
  -> 财务复核
  -> 完成
```

审批系统保存完整轨迹：

```text
节点
审批人
意见
时间
附件
超时
fallback
最终决策
```

节点数量不固定，所以审批系统按实例头、节点实例列表、事件日志保存：

```text
WorkflowInstance
WorkflowNodeInstance[]
WorkflowEventLog[]
```

DataSrv 不复制这些细节，只保存关联和结果摘要。

### 9. 结果同步回 DataSrv

审批通过：

```text
final_decision = approved
```

MIS tool 或 webhook 同步结果：

```text
ApprovalLink.status = approved
expense_report.status = pending_finance_payment
AuditLog 写入
Timeline 写入
```

审批驳回：

```text
ApprovalLink.status = rejected
expense_report.status = rejected
AuditLog 写入
Timeline 写入
```

### 10. 用户查看

常用入口：

```text
报销申请 App -> 我的报销 -> 单据详情
审批中心 -> 待我审批 / 我发起的 / 我已处理
通知/待办 -> 打开对应审批任务
```

企业 App 内显示摘要：

```text
当前节点
当前处理人
审批状态
最近意见
提交时间
完成时间
```

完整轨迹跳转到审批实例详情：

```text
approval://instances/appr_inst_123
```

### 11. 总结

```text
企业 App
  填业务数据
  发起审批
  查看业务状态

审批 workflow-skill
  定义审批流程
  运行审批实例
  保存完整审批轨迹

DataSrv
  保存业务数据
  保存审批关联
  更新业务状态
  写审计和 timeline

审批中心
  统一查看和处理审批
```
