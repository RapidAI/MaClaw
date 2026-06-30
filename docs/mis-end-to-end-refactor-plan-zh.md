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
已保存的企业超级 Skill 再被 App Studio/Skill discovery 读回时，也必须返回完整 `maclaw.app.json` 定义；GUI 恢复成本地 AppEntry 时保持 `enterprise_approval_app` / `enterprise_normal_app` 类型、真实 app id、动态 layout、appSkill/dependencies、approval bindings 和 importedRunEvidence。不能在发现阶段把企业 App 过滤掉，也不能恢复成 `tool_app`。

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
  安装缺失的必需 Skill 依赖；local/hub/skillhub 走 SkillHub（local 表示设计态本地 Skill，进入 App 包安装态后按当前 Hub 解析），market/skillmarket 走 SkillMarket，enterprise_hub 走企业 Hub；builtin/未知来源等不能自动安装的来源保持 blocked。

RecordMaclawAppInstall(packageJSON, source)
  写入本机 app_install_records.json，保存 App、安装来源、包指纹、依赖快照、必需依赖是否仍缺失。重复安装同一 App 时按 app_id upsert，保留最新记录。

ListMaclawAppInstalls(limit)
  读取本机安装记录，用于后续管理页、审计页和故障诊断。

前端市场导入流程
  粘贴应用包后先预览依赖；点击安装时如有必需依赖缺失，先调用 InstallMaclawAppDependencies，再安装 MaClaw App 面板入口，并后台写安装记录。
  对标准 maclaw.app.v1 / maclaw.app.pack.v1 包，安装按钮不能只信任前端预览缓存；即使依赖预览仍在 loading，也必须针对用户当前选中的 App 子包重新调用后端 PlanMaclawAppInstall / InstallMaclawAppDependencies，确认治理、workflow contract 和必需 Skill 依赖通过后，才允许写入本地面板。
  企业审批型/企业普通型 App 不能先写入本地面板再异步补安装审计；必须在 RecordMaclawAppInstall 成功写入本地安装记录、安装证据和 DataSrv 注册结果后，才允许进入本地 AppEntry。否则会出现“看似安装成功但审批实例/证据/依赖快照无法恢复”的断链。工具型 App 可以继续保留安装审计失败不阻断的宽容策略。

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
   App Studio、GUI 后端和 Hub 审核都必须把运行证据绑定到当前 App 定义：governance.testEvidence 必须携带 definitionHash / definitionFingerprint，且该值必须等于当前 app manifest、动态 UI layout、binding.datasrv/mis/skill/appSkill、dependencies.skills、workflow/result/test protocol、version 等字段共同计算出的定义指纹。用户编辑应用名称、版本、binding、UI 布局、workflow/result contract、test protocol 或依赖后，旧的 importedRunEvidence / run history 只能作为历史审计，不能再满足发布门禁，必须重新测试后才能上传能力市场。
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

## 当前全链路完整性审计（推进中）

从 MaClaw DataSrv 到 MaClaw App 的制作、测试、上传、安装和运行角度看，主干链路已经形成，但还没有完全闭环。

### 已落地的主干能力

```text
App Studio / GUI
  动态 UI layout、resultContract、workflow mapping、testProtocol、testEvidence 进入 MaClaw App 定义
  definitionHash 覆盖 appSkill、dependencies、动态 UI、workflow/result contract、test protocol、binding
  旧运行证据在定义变化后会失效，必须重新测试才能发布

GUI 后端
  PlanMaclawAppInstall / InstallMaclawAppDependencies / RecordMaclawAppInstall 串起安装前依赖检查
  Hub 安装时会重新规划选中的 App 子包，不能只信任前端预览
  安装记录保存 package 指纹、依赖快照、版本快照、workflow contract、workspace layout、result contract、test evidence
  企业 App 安装成功后再写本地入口，并尝试注册 DataSrv app_installations

DataSrv 回流
  MaClaw App 安装注册 metadata 保留 workspace_layout、workflow_mapping、workflow_contract、result_contract、test_evidence、dependency_verification、version_snapshot
  app_skill_id / app_skill_version / app_skill_source 可回流为 GUI 里的 appSkill 快照
  GUI 从 DataSrv app_installations 恢复候选 App 时，能恢复 resultContract 和 workflow mapping

Hub 能力市场
  MaClaw App 提交后保留原始 manifest 作为安装包本体
  metadata 额外暴露 appSkill、Skill 依赖、workflow skill IDs、workflow mapping、result/workflow contract、workspace layout 摘要
  package 下载仍以完整 App entry 为准，metadata 只做市场搜索、审核和列表摘要
```

### 仍不完整的缺口

```text
1. DataSrv 服务端契约还要继续硬化
   app_installations.metadata 的 OpenAPI / normalize / 审计摘要需要把 workspace_layout、test_evidence、result_contract、workflow_mapping、app_skill_source 等字段显式列为稳定契约。

2. 依赖自动安装还要做真实 Hub/SkillMarket 联动验收
   当前安装规划和阻断逻辑已经存在，但仍要用真实缺失 Skill 场景验证：
   enterprise_hub -> 企业 Hub
   hub/local -> 当前 Hub/SkillHub
   market/skillmarket -> SkillMarket
   builtin/unknown -> blocked

3. 审批型应用运行闭环还要端到端验收
   需要用一个样例报销/合同 App 跑通：
   发起数据提交 -> workflow-skill 创建审批实例 -> DataSrv RecordApproval -> 我的申请/待我审批/已处理/需关注 -> 最终 resultPayload/outputs/artifacts 回写。

4. App Studio 可视化设计还要补完整保存/复测/上传体验
   UI 动态生成和 layout 保存契约已经进入 manifest，但还要把用户拖拽调整、传统软件式工具栏/列表/详情/输出面板的编辑体验进一步产品化。

5. GUI 包当前有外部编译阻塞
   当前工作树里 `gui/workflow_v2_integration.go` 缺 `TemplateRegistry`，`gui/app_maclaw_llm_test.go` 仍引用已迁移的 Volcengine TokenPlan 常量。它们不是 MaClaw App 链路本轮改动引入，但会阻塞 `go test ./gui` 的包级验证，必须在全量回归前清理。
```

### 下一步推进计划

```text
P0
  修复 GUI 包外部编译阻塞，恢复 `go test ./gui` 可运行。
  给 DataSrv app_installations metadata schema/normalize/openapi 补齐 appSkill、layout、workflow、result、test evidence 字段。

P1
  做一个企业审批型样例 App 端到端测试：安装依赖、发起审批、实例视图、结果回写、Hub 上传、Hub 下载重装。
  做一个企业普通 App 端到端测试：DataSrv business action/query/report 执行、结构化 outputs、测试证据、二次发布。

P2
  强化 App Studio 的动态 UI 编辑体验：传统软件式工作台、可视化布局调整、区域锁定、运行前依赖健康提示。
  强化市场页安装体验：依赖安装进度、失败原因、版本冲突、可选依赖降级说明。
```

### 推进记录：审批型应用结果反馈闭环（2026-06-27）

本轮把审批型应用的结果反馈从“状态可回写”推进到“完整结果包可回写”：

```text
DataSrv RecordApproval.review
  接收并持久化 result_payload / outputs / artifacts
  审批完成后同步到业务记录：
    approval_result_payload
    approval_outputs
    approval_artifacts
    approval_result_summary
    approval_primary_artifact
    approval_output_count
    approval_artifact_count

审批实例视图
  handled / my_requests / pending_my_approval / attention 等 lane 继续以 RecordApproval 为实例数据源
  GUI 审批工作台和审批管理页已有结果包展示区，可显示 payload、文本/结构化 outputs、文件 artifacts
```

已补充 DataSrv HTTP 级样例验证：

```text
创建企业审批型 expense approval
  -> pending_my_approval / attention / my_requests lane 查询
  -> review approve 并提交 resultPayload、outputs、artifacts
  -> handled lane 保留完整结果包
  -> 业务记录保留完整结果包与摘要字段
```

这使“数据录入 + 审批工作流 Skill 运行 + 审批实例数据管理 + 结果反馈”中的结果反馈环节更接近可验收闭环。下一步继续补齐真实 App 运行样例：从 GUI 发起一个企业审批型 App，经过 workflow-skill 产出审批实例，再回到 DataSrv/GUI lane 与 Hub 安装包证据。

### 推进记录：企业普通应用运行证据上传闭环（2026-06-27）

本轮补强了企业普通应用（enterprise_normal_app）的“运行后再上传”证据链：

```text
GUI 企业普通应用运行
  -> ExecuteMaclawAppBusinessOperation 调用 DataSrv business action/view/report/dashboard
  -> 前端生成 resultPayload + outputs 运行证据
  -> 本地运行历史保存当前 definitionHash
  -> 审核/发布时写入 governance.testEvidence
  -> 上传到 Hub/能力市场的 maclaw.app.pack.v1 包携带结构化结果证据
```

新增前端测试断言普通企业 App 发布包里的 testEvidence 必须包含：

```text
resultPayload.business_status
resultPayload.business_record
outputs[].kind / title / text / status / data
outputCount
resultCoverage
```

这补上了“企业普通应用：DataSrv business action/query/report 执行、结构化 outputs、测试证据、二次发布”里的测试证据上传断点。下一步继续把同类证据向真实 Hub 安装包下载重装、DataSrv app_installations 回流做端到端串联。

### 推进记录：企业普通应用 DataSrv 安装记录回流（2026-06-27）

本轮补强了企业普通应用从 DataSrv app_installations 回到 GUI App 面板的证据链：

```text
DataSrv capabilities.app_installations
  enterprise_normal_app
  metadata.result_contract
  metadata.test_evidence.test_protocol
  metadata.test_evidence.result_payload
  metadata.test_evidence.outputs

GUI App Studio
  发现已安装应用
  Add to panel
  还原 manifest.datasrv / manifest.appSkill / manifest.resultContract / manifest.testProtocol
  还原 importedRunEvidence.resultPayload / outputs / definitionHash
```

新增前端测试覆盖普通企业 App 的回流场景，避免只有审批型应用保留安装证据、普通企业应用丢失结构化运行结果。下一步继续把 Hub 下载重装后的 app_installations 注册和 GUI 回流放进同一个端到端验收链。

### 推进记录：Hub 市场元数据测试证据摘要（2026-06-27）

本轮补强了 Hub/能力市场侧的 MaClaw App 测试证据摘要字段。提交 maclaw.app.pack.v1 后，完整 test_evidence 仍保留在 metadata 中，同时额外派生 DataSrv app_installations 已使用的稳定摘要键：

```text
test_evidence_run_id
test_evidence_test_protocol_fingerprint
test_evidence_primary_result
test_evidence_output_count
test_evidence_artifact_count
test_evidence_result_payload
test_evidence_outputs
test_evidence_artifacts
test_evidence_result_coverage
test_evidence_result_coverage_ok
test_evidence_result_coverage_primary
test_evidence_covered_types
test_evidence_missing_types
test_evidence_approval_instance_id / approval_id / record_id / approval_status
test_evidence_approval_view_verified
```

这样市场列表、审核、安装包注册到 DataSrv、再由 GUI 从 DataSrv 回流时，可以使用同一套摘要字段，不必每一层都重新解析深层 governance.testEvidence。下一步继续把 Hub 下载重装后的 RecordMaclawAppInstall/DataSrv registration 与 GUI 回流放进同一条验收测试。

### 推进记录：GUI 安装注册到 DataSrv 的测试证据摘要对齐（2026-06-27）

本轮补齐了 GUI 侧 `RecordMaclawAppInstall -> DataSrv app_installations` 注册 payload 的证据摘要。安装 MaClaw App 时，完整 `metadata.test_evidence` 仍保留，同时派生与 Hub 市场、DataSrv app_installations 归一化一致的稳定字段：

```text
test_evidence_run_id
test_evidence_verified_at
test_evidence_definition_fingerprint
test_evidence_test_protocol_fingerprint
test_evidence_artifact_present / artifact_count / artifact_name
test_evidence_output_count
test_evidence_result_payload
test_evidence_outputs
test_evidence_artifacts
test_evidence_result_coverage
test_evidence_result_coverage_ok / primary
test_evidence_covered_types / missing_types
test_evidence_approval_instance
test_evidence_approval_instance_id / approval_id / record_id / approval_status
test_evidence_approval_view_verified
```

这使真实 Hub 安装包、GUI 本地安装审计、DataSrv 已安装应用记录、GUI App 面板回流之间的证据索引口径保持一致。新增 GUI 后端测试验证审批型 App 安装注册时，DataSrv 收到的 metadata 同时包含嵌套证据包和这些可查询摘要，覆盖 resultPayload、outputs、artifacts、resultCoverage、approvalInstance 等核心结果反馈字段。

### 推进记录：Hub 下载安装到 DataSrv 注册验收链（2026-06-27）

本轮把 “从能力市场/Enterprise Hub 下载 MaClaw App 并安装” 的后端测试推进到 DataSrv 注册环节。测试现在覆盖：

```text
GUI InstallMaclawAppPackageFromHub
  -> Enterprise Hub 下载 maclaw.app.pack.v1
  -> 选择/安装包进入 RecordMaclawAppInstall
  -> 读取 MIS Data 配置
  -> PUT /api/v1/data/app-installations/{app_id}
  -> DataSrv 接收 enterprise_normal_app 的 role_bindings + metadata
  -> metadata 携带 test_evidence_* 结果证据摘要
```

新增断言确认 Hub 安装来源为 `enterprise_hub`，DataSrv registration 为 synced，普通企业应用的 DataSrv role binding 被写入，同时测试证据摘要包含 `run_id`、`verified_at`、`test_protocol_fingerprint`、`primary_result`、`result_payload`、`outputs`、`result_coverage` 等字段。这样 “Hub 分发 -> 本地安装 -> DataSrv 已安装应用记录” 不再只停留在本地 install audit，而是有了可回流到 GUI App 面板的数据基础。

### 推进记录：DataSrv 已安装应用回流的文件结果摘要兼容（2026-06-27）

本轮补齐 GUI App 面板从 DataSrv `app_installations` 回流时的文件结果恢复规则。此前 `importedRunEvidence.artifacts` 主要依赖深层 `metadata.test_evidence.artifacts`；现在同时支持 DataSrv/Hub 稳定摘要字段：

```text
metadata.test_evidence_artifacts
metadata.test_evidence_artifact_count
metadata.test_evidence_artifact_present
metadata.test_evidence_artifact_name
```

这样即使 DataSrv 将文件结果作为顶层摘要字段归一化，GUI 仍能在 “Add to panel” 后恢复 `importedRunEvidence.artifacts`、`artifactName`，后续二次发布和运行证据展示不会丢失文档/附件类输出。前端测试已调整为审批型 installed app 不在深层 test_evidence 放 artifacts，而从顶层 `test_evidence_artifacts` 恢复，覆盖真实 Hub/DataSrv 摘要回流形态。

### 推进记录：DataSrv 摘要-only 安装证据规范化（2026-06-27）

本轮补齐 DataSrv `app_installations` 的归一化能力：当安装记录只携带顶层 `test_evidence_*` 摘要字段、没有深层 `metadata.test_evidence` 对象时，DataSrv 会自动合成标准 `metadata.test_evidence`：

```text
test_evidence.run_id / verified_at / definition_fingerprint
test_evidence.test_protocol_fingerprint
test_evidence.result_payload
test_evidence.outputs
test_evidence.artifacts
test_evidence.result_coverage
test_evidence.approval_instance
test_evidence.dependency_verification
```

同时继续保留顶层摘要字段，供列表过滤、能力市场审核、GUI App 面板回流直接使用。新增 DataSrv 测试覆盖 summary-only 输入，验证 `UpsertAppInstallation` 和 `/api/v1/data/capabilities` 输出都能暴露合成后的深层证据与原有摘要。这让 Hub/GUI/DataSrv 任一环节即使只保存摘要字段，也不会破坏 MaClaw App 的运行证据闭环。

### 推进记录：Hub 多 App 包选择安装的定义指纹门禁（2026-06-27）

本轮补齐 Hub 多 App 包中“只安装选中的子 App”测试夹具的治理证据：即使是被筛选后的单个 App 子包，也必须携带当前 `definitionHash` / `definitionFingerprint`。安装链路仍然按当前选中子包重新运行：

```text
DownloadMaclawAppPackageFromHub
  -> maclawAppPackageForSelectedAppIDs
  -> InstallMaclawAppDependencies
  -> RecordMaclawAppInstall
  -> governance.testEvidence.definitionHash 校验
```

这避免多 App 包选择安装绕过“运行证据必须绑定当前 App 定义”的发布/安装门禁。对应 GUI 后端测试已恢复通过，覆盖 Hub 下载、子包过滤、依赖规划、安装审计写入。

### 推进记录：GUI 包级回归审计与 MaClaw App 相关失败修复（2026-06-27）

本轮重新运行 `go test ./gui -count=1 -vet=off` 做包级回归审计。当前全包仍未完全通过，但 MaClaw App 主链路中的失败已修复：

```text
已修复：
  TestInstallSelectedMaclawAppPackageFromHubFiltersPackageApps
  原因：Hub 多 App 测试包缺少当前 definitionHash，被治理门禁正确拒绝。
  处理：测试夹具通过 maclawAppPackageWithCurrentDefinitionHashes 注入当前定义指纹。

仍待清理的外围失败：
  AI assistant oversized tool args 测试受 OpenAI OAuth 401 影响
  Skill create-session guard 测试受语义分类器/LLM 可用性影响
  Hub Skill bundled file fixture base64 解码失败
  SubAgentConcurrency 配置上限断言与当前实现不一致
  VE group conversation 测试路由期望与实际 discoverable 请求不一致
  recent task name whitespace 归一化断言不一致
```

MaClaw App 相关的安装/规划定向回归已通过：

```bash
go test ./gui -count=1 -vet=off -run "InstallSelectedMaclawAppPackageFromHubFiltersPackageApps|InstallMaclawAppPackageFromHub|RecordMaclawAppInstall|PlanMaclawAppInstall"
```

这说明当前 MaClaw App 的 Hub 下载、子包过滤、依赖规划、安装记录与 definitionHash 门禁路径可继续作为后续端到端验收基础；全 GUI 包级绿色仍需要继续清理上述外围测试阻塞。

### 推进记录：App Studio 企业审批型应用写回完整超级 Skill 定义（2026-06-27）
本轮补齐 App Studio 管理面板的保存链路：此前已发现企业审批型/企业普通型 App 能从 `maclaw.app.json` 读回为动态应用，但管理面板编辑保存时只允许 `tool_app` 调用 `SaveMaclawAppDefinitionForSkill`，导致“可视化调整布局 -> 保存 -> 测试 -> 上传能力市场”的企业应用链路存在断点。

调整后，来自 Skill 的以下应用类型都会在保存时写回 Skill 定义文件：

```text
tool_app
enterprise_approval_app
enterprise_normal_app
```

写回门禁仍要求：

```text
source == skill
manifest.skill.id 非空
tool_app: manifest.skill.appDefinitionFile 为 maclaw.app.json 或 maclaw.apps.json
enterprise_approval_app / enterprise_normal_app: manifest.skill.appDefinitionFile 必须为 maclaw.app.json
```

其中企业应用前端保存门禁和后端规范化逻辑均保证只能保存为完整 `maclaw.app.json`，避免企业审批型/普通型应用被降级成轻量 `maclaw.apps.json`。

新增前端回归测试覆盖一个企业审批型 Skill App 的管理面板保存，验证写回 payload 仍保留：

```text
schema/privateMarker/installUnit
enterprise_approval_app kind
agent_dynamic_ui launchMode
DataSrv domain/objectRole
appSkill
workflow_skill dependency
MIS approvalBindings
ui.layouts.approval_workspace.regions
resultContract.approvalDecisions
testProtocol expectedOutput/requiredRoles/fingerprint
```

这让“应用程序设计时自动生成界面 -> 用户可视化调整位置 -> 保存到应用信息文件 -> 测试 -> 上传 Hub 能力市场”的企业审批型应用制作链路更接近完整闭环。

### 推进记录：后端禁止企业应用降级写入轻量 maclaw.apps.json（2026-06-27）
本轮继续收紧 GUI 后端 `SaveMaclawAppDefinitionForSkill` 的企业应用保存规则。前端企业 App 为了兼容发现/面板状态，会同时携带 `binding.skill` 与 `binding.appSkill`；此前后端规范化阶段只检查企业 App 的 `binding.appSkill.appDefinitionFile`，但最终文件选择会同时读取 `binding.skill.appDefinitionFile`。这意味着异常/旧版 payload 可能通过 `binding.skill.appDefinitionFile = maclaw.apps.json` 绕过规范化检查，最终把企业审批型应用写入轻量 `maclaw.apps.json`。

修复后，企业审批型/企业普通型应用的定义文件判定会同时检查：

```text
binding.appSkill.appDefinitionFile
binding.appSkill.app_definition_file
binding.skill.appDefinitionFile
binding.skill.app_definition_file
```

只要任一有效声明指向 `maclaw.apps.json`，就拒绝保存，并返回：

```text
enterprise MaClaw App definitions must be saved as maclaw.app.json
```

新增后端回归测试覆盖“企业审批型 App 通过 `binding.skill.appDefinitionFile` 声明 `maclaw.apps.json`”的情况，确认保存失败且不会生成 `maclaw.apps.json`。这把前端保存门禁和后端规范化门禁对齐，保证企业审批型应用始终作为完整超级 Skill 定义保存，动态 UI、审批流依赖、DataSrv 绑定、结果契约和测试证据不会被轻量清单格式截断。

### 推进记录：App Studio 创建态支持企业审批型应用直接保存为超级 Skill（2026-06-27）
本轮补齐 App Studio “新建应用”入口的企业应用制作链路。此前管理面板已经能把企业审批型/企业普通型 Skill App 写回 `maclaw.app.json`，但创建态 `保存到 Skill / 上传到 SkillMarket` 仍只对 `tool_app` 开放，企业应用只能先创建成本地面板 App，再进入管理态间接写回，制作流程不完整。

调整后，创建态会按应用类型选择定义写回目标：

```text
tool_app:
  使用 Existing Skill 作为目标 Skill

enterprise_approval_app / enterprise_normal_app:
  使用 App Skill 作为目标超级 Skill
  workflow_skill 作为依赖写入 dependencies.skills
```

保存前会在企业应用 manifest 中补齐 Skill 定义定位信息：

```text
binding.skill.id = App Skill
binding.skill.appDefinitionFile = maclaw.app.json
binding.appSkill.id = App Skill
```

这样创建态保存后的面板应用既是 `source = skill`，又保留后续管理面板再次编辑写回所需的 `manifest.skill.appDefinitionFile`。新增前端回归测试覆盖“新建企业审批型应用 -> 默认选择 App Skill/workflow Skill -> 保存到 Skill”，验证写回 payload 包含：

```text
maclaw.app.v1 / x_maclaw_apps
enterprise_approval_app
agent_dynamic_ui
binding.skill.appDefinitionFile = maclaw.app.json
binding.appSkill
dependencies.skills[workflow_skill]
mis.approvalBindings
ui.layouts.approval_workspace
resultContract.primary = approval_result
testProtocol.fingerprint
```

至此，App Studio 的企业审批型应用制作路径从“自动生成/手工设计动态界面”到“保存为完整超级 Skill 定义”不再需要绕到管理态完成。

### 推进记录：依赖证据合并改为显式配置优先（2026-06-27）
本轮修复 App Studio / 发布治理中的依赖证据合并问题。企业应用会同时携带：

```text
binding.appSkill          # 超级 Skill / App Skill
binding.skill             # 用于面板写回 maclaw.app.json 的定义定位
dependencies.skills       # 显式依赖，如 workflow_skill、connector_skill
mis.approvalBindings      # 可推断 workflow_skill
```

此前前端 `appSkillDependencies` 直接按数组顺序用 Map 去重，后出现的推断依赖会覆盖前面的显式依赖。实际影响包括：

```text
App Skill 显式 source=local 可能被 binding.skill 推断成 hub
workflow_skill 显式 source=market/local 可能被 approvalBindings 推断成 hub
显式 version/source 在治理导出和安装规划证据中不稳定
```

现在依赖合并改为“先出现的显式元数据优先，后续推断项只补空字段”，并合并 capabilities。这样 App Skill、workflow Skill 的版本、来源、required 标记会稳定进入：

```text
安装预览 buildInstallPlan
发布治理 app.governance.dependencies.skills
复制应用包 / 上传包
安装审计与依赖检查的前端证据
```

新增前端回归测试构造企业审批型 App：

```text
appSkill: expense-super-skill@1.3.0 source=local
binding.skill: expense-super-skill appDefinitionFile=maclaw.app.json
dependencies.skills: expense-approval-flow@2.1.0 source=market
mis.approvalBindings: workflowSkillId=expense-approval-flow
```

导出应用包后断言 governance dependencies 仍保留 `expense-super-skill · local` 和 `expense-approval-flow · market`，避免安装/发布环节错误地从 Hub 查找本地或市场来源的依赖。

### 推进记录：DataSrv 安装记录支持从审批实例运行证据过滤 workflow（2026-06-27）
本轮补齐 DataSrv `app_installations` 的审批实例查询能力。此前安装记录列表已经支持按 `workflow_skill_id` / `workflow_node` 过滤，但主要依赖顶层摘要字段和 `workflow_mapping`。实际企业审批型 App 的运行结果经常只体现在测试证据中：

```text
metadata.test_evidence.approval_instance.workflowSkillId
metadata.test_evidence.approval_instance.currentNode
metadata.test_evidence_approval_instance.workflowSkillId
metadata.test_evidence_approval_instance.currentNode
```

如果没有同步写入 `workflow_mapping`，DataSrv 无法通过“当前审批流 / 当前节点”查回对应 App 安装记录，会影响 GUI 中“我的申请 / 我审批的 / 当前节点状态 / 审批结果反馈”的后续数据回流。

调整后，DataSrv store 的安装记录 metadata filter 会额外识别审批实例运行证据中的：

```text
workflowSkillId / workflowSkillID / workflow_skill_id
approvalWorkflowID / approvalWorkflowId / approval_workflow_id
currentNode / current_node / node / workflowNode / workflow_node
```

新增回归测试覆盖企业审批型安装记录只携带 `test_evidence.approvalInstance` 的情况，验证：

```text
ListAppInstallations(WorkflowSkillID="expense-approval-flow") 可以命中
ListAppInstallations(WorkflowNode="expense.result_feedback") 可以命中
ListAppInstallations(WorkflowSkillID="other-flow") 不会误命中
```

这让 DataSrv 的审批实例数据管理更贴近实际运行链路：即使安装审计记录只保存运行证据包，也能按审批 workflow 和当前节点进行回流查询。

### 推进记录：GUI 审批实例详情保留多当前节点回流（2026-06-27）
本轮补齐 GUI 审批实例视图对 DataSrv / 后端回流的 `current_node_ids` 支持。后端审批实例记录已经允许返回：

```text
current_node
current_node_ids
workflow_node_ids
```

前端 `backendApprovalInstanceToView` 虽然会解析节点数组并选出首个节点作为当前节点，但没有把完整 `currentNodeIDs` 保存到视图模型，导致审批实例详情、全局审批管理、搜索文本只能看到单个节点，无法呈现多节点/并行节点状态。

修复后，`ApprovalInstanceView` 会保留完整 `currentNodeIDs`，详情头、列表行和时间线继续通过 `approvalCurrentNodeText` 展示：

```text
manager_approval / finance_review
```

新增前端回归测试覆盖全局“审批状态”入口：后端只返回 `current_node_ids`、不返回 `current_node` 时，GUI 仍能在审批详情中显示完整节点路径。这和 DataSrv 新增的 workflow/node 过滤能力拼起来，形成“按当前节点查回 -> GUI 展示当前节点状态”的闭环。

### 推进记录：GUI 后端审批实例回流兼容 request 别名字段（2026-06-27）
本轮补齐 Wails 后端从 DataSrv `/api/v1/data/approvals` 读取审批实例时的字段兼容。DataSrv 标准响应包含顶层 `app_id`、`workflow_skill_id`、`workflow_node_id(s)`，但企业审批工作流运行证据、旧版同步记录或外部连接器可能把这些信息放在 `request` 载荷里：

```text
request.maclaw_app_id / request.appID / request.app_id
request.blueprintID / request.blueprint_id
request.workflowSkillId / request.workflowSkillID / request.workflow_skill_id
request.workflowVersion / request.workflow_version
request.approvalWorkflowID / request.approval_workflow_id
request.current_node_ids / request.workflow_node_ids
request.currentNode / request.current_node / request.workflowNode / request.workflow_node
```

调整后，`maclawAppApprovalInstanceFromRecordApproval` 会从 request 别名中补齐：

```text
AppID / BlueprintID
ApprovalWorkflowID / WorkflowSkillID / WorkflowVersion
CurrentNode / CurrentNodeIDs
```

并扩展字符串列表解析，支持 request 中单字符串或数组形式的节点字段。新增 GUI 后端测试覆盖 DataSrv 审批记录只通过 request 携带 App、workflow 和 current nodes 的情况，验证 `ListMaclawAppApprovalInstancesAll("pending_my_approval")` 仍能输出完整审批实例视图。

这让“DataSrv 审批实例数据管理 -> Wails 后端转换 -> GUI 全局审批状态/单应用审批工作台”在面对不同来源的审批记录时更稳，不会因为字段落在 request 包里就丢失 App 归属或当前节点状态。

### 推进记录：DataSrv 审批实例查询支持当前节点语义别名（2026-06-27）
本轮补齐 DataSrv HTTP 查询层对审批“当前节点”命名的兼容。原 `/api/v1/data/approvals`、`/api/v1/data/inbox`、`/api/v1/data/inbox/summary` 已支持 `workflow_node_id`，SQLite 层也能同时匹配主节点和 `workflow_node_ids_json` 中的并行节点。但 App manifest、测试证据、安装记录和 GUI 文案里更常出现：

```text
current_node_id
current_node
workflow_node
```

如果调用方使用这些语义字段，HTTP 层此前不会映射到 `WorkflowNodeID`，导致“按当前节点查看我的申请/我审批的/需关注实例”必须记住底层字段名。

调整后，DataSrv 三个审批实例入口统一使用同一套别名解析：

```text
workflow_node_id -> current_node_id -> current_node -> workflow_node
```

并同步更新 OpenAPI，使能力市场、App Studio、GUI 后端和外部连接器都能从接口描述里看到这些别名。新增回归测试覆盖 `/api/v1/data/approvals?current_node_id=...`、`current_node=...`、`workflow_node=...` 对并行审批节点的命中，确保企业审批型 App 的“当前节点状态”查询不再依赖单一底层字段名。

### 推进记录：安装计划明确区分 App 超级 Skill 依赖（2026-06-27）
本轮继续梳理“MaClaw App 是带特殊数据的超级 Skill”在安装链路里的表达。后端安装计划此前已经会从 `binding.appSkill` / `binding.app_skill` 提取 App 自身的 Skill，并和 `dependencies.skills`、审批工作流 Skill 一起做依赖检查/安装；但计划中的 `kind` 使用的是 `runtime_skill`，容易和普通工具/运行动作 Skill 混淆。

调整后，安装计划和安装证据中的依赖类型更明确：

```text
binding.skill        -> runtime_skill
binding.appSkill     -> app_skill
binding.app_skill    -> app_skill
dependencies.skills(kind=workflow_skill) -> workflow_skill
```

这让安装、审计、DataSrv 注册 metadata、能力市场治理都能区分三类依赖：

```text
app_skill：App 本身的超级 Skill，包含 maclaw.app.json、动态 UI、workflow/result/test contract
workflow_skill：审批工作流 Skill，实际驱动审批节点和结果回写
runtime_skill：普通工具/动作 Skill，支撑一次性调用或业务操作
```

新增/更新回归测试覆盖：

```text
PlanMaclawAppInstall 会把 enterprise app 的 binding.appSkill 标记为 app_skill
binding.app_skill 的 source/version 保持不丢失
RecordMaclawAppInstall 写入 DataSrv metadata 时保留 app_skill 依赖类型
```

这一步不改变安装行为本身，但把安装计划的语义层补齐，为后续“安装 MaClaw App 时从本 Hub/市场安装超级 Skill + workflow Skill + runtime Skill，并分别做版本/来源治理”打基础。

### 推进记录：前端发布包和回流视图保留 app_skill 语义（2026-06-27）
上一轮后端安装计划已把 `binding.appSkill` / `binding.app_skill` 标记为 `app_skill`。本轮继续补齐 GUI 前端侧，避免 App Studio 在导出、发布、安装记录回显时又把超级 Skill 降级为普通 `runtime_skill`。

调整后，前端依赖证据的语义保持一致：

```text
manifest.appSkill -> governance.dependencies.skills(kind=app_skill)
DataSrv metadata.app_skill_id -> version_snapshot.app_skill(kind=app_skill)
install_record.app_versions[app].app_skill.kind -> 详情页显示 app_skill
```

对应影响：

```text
复制/上传应用包：超级 Skill 以 app_skill 写入 governance.dependencies
安装市场 App：版本快照 App Skill 行显示 app_skill
DataSrv 安装记录回流：从 app_skill_id/app_skill_version 恢复版本快照时默认 kind=app_skill
```

新增/更新前端回归测试覆盖：

```text
导出企业审批型 App 包时，expense-super-skill 为 app_skill，workflow 仍为 workflow_skill
单个市场 App 安装后的版本快照显示 app_skill
安装记录列表和应用详情页从 install_record / DataSrv 回流的 app_skill 语义保持不变
```

这样从 App Studio 设计、发布包治理、Hub/市场安装、DataSrv 注册和 GUI 回流视图，App 本体 Skill 都统一叫 `app_skill`；普通动作 Skill 才叫 `runtime_skill`，审批流仍叫 `workflow_skill`。

### 推进记录：GUI 包级回归阻塞清理进展（2026-06-27）
本轮重新审计 `go test ./gui -count=1 -vet=off` 的当前失败状态。此前文档里提到的 `TemplateRegistry` 编译缺口和 Volcengine TokenPlan 常量迁移问题已经不是当前阻塞：GUI 包可以编译并进入运行测试阶段。当前全包失败主要集中在外围测试和外部状态依赖上，其中两个不依赖外部网络、可以直接修复的阻塞已清理：

```text
TestSaveConfigSanitizesSubAgentConcurrency
TestNormalizeRecentTaskName
```

处理结果：

```text
SubAgentConcurrency 测试改为真正的越界输入 Max+1，继续验证 SaveConfig 会 clamp 到 MaxSubAgentConcurrency。
normalizeRecentTaskName 改为把所有空白折叠为单空格，并按 120 rune 截断，符合最近任务标题的侧边栏展示契约。
```

定向验证已通过：

```text
go test ./gui -count=1 -vet=off -run "TestSaveConfigSanitizesSubAgentConcurrency|TestNormalizeRecentTaskName"
```

当前全量 GUI 包级剩余阻塞仍包括：

```text
TestSendAIAssistantMessage_RejectsOversizedToolArguments_OpenAI / Anthropic
  真实 OpenAI OAuth 401 抢先返回，测试没有隔离 LLM 调用。

TestSkillCreateSessionGuard_DoesNotGuess*WithoutSemantic
  语义分类器/LLM 退化路径仍会使用 embedding/tree fallback 做判断，测试期望“无语义分类器时只返回 ambiguous”。

TestInstallManagedHubSkillAllowsHighRiskWithAuditOnly
TestInstallHubSkillSucceedsWhenHubExtractsFileBackedSkillDir
  Hub Skill 安装测试 fixture / 文件包路径仍需单独清理。

TestInitiateGroupConversationErrorsOnMissingSessionID
  VE group conversation 测试服务器未预期 /api/ve/discoverable 探测请求。
```

这一步不是 MaClaw App 主链路功能本身，但它减少了全流程交付前的包级回归噪音：全包回归已从“编译阻塞”推进到“少数外围运行测试阻塞”，MaClaw App 定向链路仍保持可验证。

### 推进记录：Hub 文件型 Skill 安装夹具对齐 base64 契约（2026-06-27）

本轮继续从“安装 MaClaw App 时自动检查并安装依赖 Skill”这条链路向下收敛。`SkillHubClient.extractBundledSkillFiles` 的生产契约是：Hub 下载包中的 `files` 字段按路径映射到 base64 编码后的文件内容，客户端只负责安全路径校验、base64 解码、写入本地 Skill 目录，再进入后续安全扫描和注册流程。

此前两个 Hub 安装回归测试的 fixture 把文件内容做了双层 JSON 字符串转义，实际返回值变成了带引号字符的 `"base64内容"`，导致解包器在第 0 字节遇到非法 base64 字符。修复后，测试夹具只保留一层 JSON 字符串编码，和真实 Hub 下载协议一致：

```text
files["skill.yaml"] = base64.StdEncoding.EncodeToString(...)
files["skill.md"]   = base64.StdEncoding.EncodeToString(...)
```

这次不放宽生产解包逻辑，因为 MaClaw App / Skill 市场分发需要一个明确、可审计的包格式；客户端不应在安装企业能力或审批工作流依赖时猜测内容格式。

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestInstallManagedHubSkillAllowsHighRiskWithAuditOnly|TestInstallHubSkillSucceedsWhenHubExtractsFileBackedSkillDir"
go test ./gui -count=1 -vet=off -run "PlanMaclawAppInstall|InstallMaclawAppDependencies|RecordMaclawAppInstallRegistersEnterpriseApprovalAppEvidence|ListMaclawAppApprovalInstances"
go test ./structureddata -count=1 -run "HTTPServerRequiresBearerTokenAndHandlesRecords|Approval|AppInstallation|Capabilities"
```

对应全流程意义：

```text
Hub / 能力市场下载文件型 Skill -> base64 解包 -> 本地 Skill 目录落盘 -> 安全扫描/审计 -> 注册为可安装依赖
MaClaw App 安装计划 -> app_skill / workflow_skill / runtime_skill 依赖识别 -> 依赖检查与安装 -> DataSrv 安装证据回写
```

当前仍未宣称全链路完整。下一步继续清理 GUI 包剩余阻塞，优先处理和运行期可信性相关的测试隔离问题：超大 tool arguments 应在真实 LLM 网络调用前被拒绝；无语义分类器时的 Skill 会话创建 guard 应避免 fallback 过度猜测；VE group conversation 测试需要显式处理 `/api/ve/discoverable` 探测请求。

### 推进记录：运行期工具参数上限前置到执行入口（2026-06-27）

本轮继续清理 GUI 包级阻塞中和运行期可信性直接相关的一项：模型返回的 tool arguments 过大时，不能让工具执行器、Skill、审批工作流节点或文件/网络类工具收到超大输入。

原有底层 OpenAI/Responses 部分流式聚合器已经有 `guiMaxToolArgumentsBytes` / `maxToolArgumentsBytes` 检查，但 GUI agent loop 对已经解析出的 Anthropic `tool_use`、非流式 OpenAI 兼容返回、以及其他能进入工具执行入口的调用，仍可能先尝试执行工具，再在失败记录里截断参数。这对 MaClaw App 来说不够稳：企业审批型应用运行 workflow skill 时，任何节点上的 MIS 操作 Skill 都应该在执行前完成参数边界检查。

调整后，`executeAgentLoopToolCalls` 在调用具体工具 handler 前统一检查：

```text
len([]byte(tool_call.function.arguments)) > guiMaxToolArgumentsBytes
  -> 直接返回 IMAgentResponse.Error
  -> 不调用工具 handler
  -> loop 状态标记 failed
```

测试隔离也同步修正：

```text
MaClaw LLM provider 测试配置不再用普通 SaveConfig 覆盖后端拥有字段
改为走 SaveMaclawLLMProviders，与真实 GUI 设置面板一致
OpenAI 用例使用兼容 JSON 返回覆盖非流式解析后的执行前拦截
Anthropic 用例使用 tool_use stream 覆盖已解析 tool call 的执行前拦截
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestSendAIAssistantMessage_RejectsOversizedToolArguments"
go test ./gui -count=1 -vet=off -run "TestInstallManagedHubSkillAllowsHighRiskWithAuditOnly|TestInstallHubSkillSucceedsWhenHubExtractsFileBackedSkillDir|TestSendAIAssistantMessage_RejectsOversizedToolArguments"
go test ./gui -count=1 -vet=off -run "PlanMaclawAppInstall|InstallMaclawAppDependencies|RecordMaclawAppInstallRegistersEnterpriseApprovalAppEvidence|ListMaclawAppApprovalInstances"
go test ./structureddata -count=1 -run "HTTPServerRequiresBearerTokenAndHandlesRecords|Approval|AppInstallation|Capabilities"
```

对应全流程意义：

```text
MaClaw App 运行 -> workflow/app/runtime skill 触发工具 -> agent loop 执行入口 -> 参数大小边界检查 -> 合法才进入实际 MIS 操作/文件/网络/Skill handler
```

这一步把“审批工作流 Skill 运行”和“工具型/企业普通应用的一次性功能调用”的运行期防护都前移了一层，减少了企业应用运行时由模型输出异常带来的资源和安全风险。

### 推进记录：清理剩余 GUI 定向阻塞并验证会话路由 guard（2026-06-27）

本轮继续按“从 DataSrv 到 MaClaw App 设计、安装、测试、上传、运行”的全链路视角清理外围阻塞。重点不是新增业务表面，而是让企业应用运行入口和协作会话入口的测试证据更干净。

首先复核了 Skill 创建外部 coding session 的 guard。当前源码中 `classifyTaskIntentWithoutSemantic` 在语义分类器缺席时已经返回 `intentUnknown`，不会再靠关键词或 fallback 猜测 SSH、非编码演示或编码任务。定向测试已证明无语义分类器时会走保守 ambiguous/unknown 分支：

```text
go test ./gui -count=1 -vet=off -run "TestSkillCreateSessionGuard_DoesNotGuess|TestSkillCreateSessionGuard"
```

随后清理 VE group conversation 的测试隔离问题。`InitiateGroupConversation` 的真实流程会先调用 `/api/ve/discoverable` 解析可邀请对象，再创建 A2A consultation。`TestInitiateGroupConversationErrorsOnMissingSessionID` 只 mock 了 `/api/a2a/consultations`，导致测试在 discoverable 探测处提前失败，无法验证原本目标“consultation 返回缺失 session id 时应报错”。本轮补齐测试服务器的 discoverable 响应，使测试进入原始 missing-session-id 分支。

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestInitiateGroupConversationErrorsOnMissingSessionID|TestInitiateGroupConversationReturnsInviteFailure|TestInitiateGroupConversationDeduplicatesInvitees"
go test ./gui -count=1 -vet=off -run "TestSkillCreateSessionGuard_DoesNotGuess|TestInitiateGroupConversationErrorsOnMissingSessionID|TestSendAIAssistantMessage_RejectsOversizedToolArguments|TestInstallManagedHubSkillAllowsHighRiskWithAuditOnly|TestInstallHubSkillSucceedsWhenHubExtractsFileBackedSkillDir"
go test ./gui -count=1 -vet=off -run "PlanMaclawAppInstall|InstallMaclawAppDependencies|RecordMaclawAppInstallRegistersEnterpriseApprovalAppEvidence|ListMaclawAppApprovalInstances"
go test ./structureddata -count=1 -run "HTTPServerRequiresBearerTokenAndHandlesRecords|Approval|AppInstallation|Capabilities"
```

对应全流程意义：

```text
企业应用 / MaClaw App 运行入口 -> 不在语义分类缺席时误开外部 coding session
VE 群组协作入口 -> discoverable 探测、consultation 创建、邀请失败、缺失 session id 分支都有稳定测试证据
```

至此，上一轮列出的几个可直接清理的 GUI 包级阻塞中，Hub 文件型 Skill 安装、超大工具参数执行前拒绝、Skill 会话 guard、VE group conversation discoverable 夹具均已定向通过。后续仍需继续扩大验证范围，尤其是 App Studio 动态界面制作/保存/测试/上传/安装/运行的前端交互闭环和全包 GUI 回归。

### 推进记录：群聊执行器上下文隔离、确认门测试隔离与 DataSrv 验证修正（2026-06-27）
本轮继续从“MaClaw App 作为超级 Skill，运行时会依赖其它 Skill、审批工作流和 DataSrv 操作”的角度清理全链路外围阻塞。重点是让 GUI 后端测试不再被真实用户配置、全局语义分类器或后台群聊执行器拖入不可控路径。

处理内容：

```text
1. VE group executor 运行入口显式创建 LoopContext
   - Platform/UserID/Lang 写入 LoopContext，prepareIMLoopContext 对已有上下文也补齐这些字段。
   - initialAgentLoopPhase 对 platform=ve_group_executor 跳过 skill preference，避免群聊上下文中的“文档/pdf/report”等词误触发 Skill/Hub 搜索。
   - group executor session cancel 会传播到 LoopContext.Cancel，handler 返回时也主动 cancel，减少异步测试和真实会话取消泄漏。

2. Wails 事件发送在无生命周期 ctx 时降级为日志
   - emitAIAssistantResponse 在 App nil 或 ctx nil 时直接跳过 EventsEmit。
   - 这避免后端异步失败路径在无 Wails runtime 的单元测试里二次 panic/报错。

3. 执行确认门测试隔离 LLM provider 和全局 UIC
   - 确认门用例使用 SaveMaclawLLMProviders 写入 httptest provider，而不是只依赖 SaveConfig 中的后端拥有字段。
   - 需要“无语义分类器”前提的测试显式 setUnifiedClassifierForIM(nil) 并在 Cleanup 中清理，避免全包运行时被其它 App 初始化留下的 UIC 影响。

4. DataSrv 模块验证命令修正
   - DataSrv 是独立 Go module，结构化数据测试需要在 datasrv 目录下运行 go test ./structureddata。
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestSendMessageForTabRoutesRecentTaskToWorkingDir|TestSendAIAssistantMessageRoutesRecentTaskToWorkingDir"
go test ./gui -count=1 -vet=off -run "TestSkillCreateSessionGuard_DoesNotGuess|TestSkillCreateSessionGuard_BlocksAmbiguousIntent|TestSkillExecutorExecute_BlocksCreateSessionWhenSemanticUnavailable"
go test ./gui -count=1 -vet=off -run "TestHandleIMMessageWithProgressAndStream_ReturnsConfirmationBeforeExecution|TestHandleIMMessageWithProgressAndStream_PresentationTaskSkipsCodingConfirmation|TestHandleIMMessageWithProgressAndStream_ScreenshotTaskUsesLLMAndSkipsConfirmation"
go test ./gui -count=1 -vet=off -run "TestInitiateGroupConversationErrorsOnMissingSessionID|TestCodingSubAgentFinalGitDiffRejectsEmptyDiffAfterTrackedModification|TestClassifyTaskIntent_WithoutUIC_ReturnsUnknown"
go test ./gui -count=1 -vet=off -run "PlanMaclawAppInstall|InstallMaclawAppDependencies|RecordMaclawAppInstallRegistersEnterpriseApprovalAppEvidence|ListMaclawAppApprovalInstances"
cd datasrv && go test ./structureddata -count=1 -run "HTTPServerRequiresBearerTokenAndHandlesRecords|Approval|AppInstallation|Capabilities"
git diff --check -- gui/app_wails_bindings.go gui/app_ve_test.go gui/app_ai_assistant_delivery_runtime_test.go gui/app_nl_skills_market_test.go gui/im_agent_loop_tool_exec.go gui/im_loop_control.go gui/im_agent_loop_phase.go gui/ve_group_dispatcher.go gui/im_confirmation_gate_test.go gui/app_nl_skills_guard_test.go docs/mis-end-to-end-refactor-plan-zh.md
```

当前仍不能宣称全 GUI 包级完成。最新 `go test ./gui -count=1 -vet=off -timeout 150s` 已不再卡在最初的 Hub file-backed Skill、oversized tool args、VE discoverable 或确认门 httptest provider 问题上，但全包仍暴露其它历史/外围测试失败与全局状态污染，例如：

```text
coding subagent 动态工具失败是否应计入 rejected result 的断言不一致
workflow doc-only / planning phase 工具过滤策略与测试期望不一致
confirmation typed approval 语义分类上下文在 full run 下仍受全局分类器/配置影响
in-flight marker / agent loop trace 类测试存在多处旧期望与当前执行策略不一致
```

这些失败暂不作为 MaClaw App 主链路完成证明。下一步继续优先处理两类工作：

```text
1. 继续压低全包 GUI 回归噪音：优先修复全局状态隔离和与运行时可信性相关的失败。
2. 回到 App Studio / 动态 UI / Hub 上传下载重装 / DataSrv app_installations 回流的端到端样例，把企业审批型应用和企业普通应用各做成可验收闭环。
```

### 推进记录：企业 App 保存到超级 Skill 定义时补齐绑定契约（2026-06-27）

本轮继续回到 App Studio / MaClaw App 制作链路。企业审批型应用现在按“超级 Skill”保存时，需要同时满足两个视角：

```text
appSkill
  表示 MaClaw App 自身依附的超级 Skill 身份，用于能力市场、依赖检查、安装记录和版本快照。

skill
  表示运行期/兼容读取所需的 App 定义绑定，包含 appDefinitionFile、inputMode、outputModes、fields 等动态 UI 与运行入口信息。
```

此前企业审批型/企业普通型 App 的草稿 manifest 只写 `appSkill`，保存到 `maclaw.app.json` 后缺少 `binding.skill`，导致 App Studio 保存到 Skill 的契约测试无法证明“这个 App 定义可以被旧读取点、运行入口和市场包校验共同识别”。本轮在前端序列化层补齐兼容绑定：

```text
App Studio 企业 App 草稿 -> 保存到 Skill
  -> manifest.appSkill 保留超级 Skill 身份
  -> manifest.skill 自动派生为同一 Skill ID
  -> appDefinitionFile 固定为 maclaw.app.json
  -> inputMode 默认为 form，outputModes 从结果契约继承

appToManifest
  -> 若 manifest.skill 缺失但 manifest.appSkill 存在，发布/安装包输出中也补齐 binding.skill
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "saves a newly created enterprise approval app into its app skill definition"
npm.cmd test -- AppsPage.test.tsx -t "approval"
npm.cmd run build
```

### 推进记录：发布治理尊重显式缺失的结果覆盖类型（2026-06-28）

本轮继续从“App Studio 测试证据 -> 发布治理 -> 能力市场审核”的角度收紧 result contract 覆盖门禁。此前后端发布治理已经会检查 `resultCoverage`，但当 coverage 声明 `ok=true` 且覆盖了 primary result 时，即使 `missingTypes` 里还有声明结果类型，也会放行。这样会让 App Studio 明确发现的输出缺口在提交市场时被弱化。

已落地调整：

```text
SubmitMaclawAppPackage / governance review
  -> 读取 governance.testEvidence.resultCoverage
  -> 若 missingTypes / missing_types 非空，直接产生 error review issue
  -> 即使 ok=true 且 coveredTypes 包含 primary，也不能绕过显式 missingTypes
  -> issue message 列出缺失结果类型
```

对应全链路意义：

```text
App Studio 测试
  -> result coverage 明确指出缺少 business_record / artifact / notification 等类型
  -> 发布提交保留该问题
  -> Hub/能力市场审核不会接收“primary 通过但其它声明输出缺失”的 App 包
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestSubmitMaclawAppPackageFlags(ResultCoverageMismatch|ExplicitResultCoverageMissingTypes)"
```

### 推进记录：App Studio 发布预检尊重 result coverage 缺失类型（2026-06-28）

上一轮后端发布治理已经收紧 `resultCoverage.missingTypes`：只要 App Studio 测试证据明确记录缺失的结果类型，就不能因为 primary result 已覆盖而放行。本轮继续把同一规则前移到 GUI 发布面板，避免用户在前端看到“Ready to submit”，提交后才被后端 review issue 打回。

已落地调整：

```text
AppRunHistoryEntry
  -> 新增 resultCoverage
  -> DataSrv importedRunEvidence 恢复 result_coverage
  -> 本地 App Studio 测试历史可保存显式 resultCoverage

appRunEvidenceContractCoverage
  -> 优先读取 evidence.resultCoverage.coveredTypes / covered_types
  -> 优先读取 evidence.resultCoverage.missingTypes / missing_types
  -> missingTypes 非空时直接判定 publish check 失败
  -> appGovernanceForManifest 提交包继续写入同一 resultCoverage
```

对应全链路意义：

```text
App Studio 测试发现缺少声明输出类型
  -> 发布面板立即显示 Needs work
  -> Submit for review 按钮禁用
  -> 后端 SubmitMaclawAppPackage 也有同一门禁兜底
  -> 前后端发布治理口径一致
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "requires run evidence to cover the declared primary result contract"
npm.cmd run build
```

### 推进记录：运行态展示安装证据中的依赖验证和测试证据（2026-06-27）
本轮继续把安装证据从“安装结果页/发布复核页”推进到真正的 App 运行态。此前用户安装 MaClaw App 后，安装结果面板能看到依赖验证和测试证据，发布复核也已能读取 `AppEntry.installEvidence`；但用户关闭 App Studio、回到应用运行界面后，如果实时依赖检查尚未返回、返回空计划或不可用，运行态无法继续展示安装时的依赖验证与测试证据。

已落地调整：

```text
运行态 AppPreview
  -> 从 AppEntry.installEvidence.dependency_verification 生成 fallback install plan
  -> 若缺少 dependency_verification，则从 AppEntry.installEvidence.dependencies 生成 fallback install plan
  -> 实时 Plan 有依赖/问题时优先显示实时 Plan
  -> 实时 Plan 为空时保留安装证据中的依赖验证
  -> 在运行状态区复用 DependencyVerificationPanel
  -> 同时展示 InstallRecordEvidenceSnapshot
```

对应全链路意义：

```text
市场/Hub 安装
  -> RecordMaclawAppInstall 生成 install_evidence
  -> AppEntry.installEvidence 持久化
  -> 用户打开应用运行界面
  -> 直接看到依赖验证、安装测试证据、布局/结果契约摘要
  -> 运行前诊断不再依赖刚好可用的实时 Plan
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "keeps dependency verification visible after single market app install"
npm.cmd test -- AppsPage.test.tsx -t "includes enterprise visual UI metadata in market submission packages"
npm.cmd run build
```

对应全链路意义：

```text
App Studio 可视化制作 -> 保存 maclaw.app.json 到超级 Skill -> 测试当前定义 -> 上传 SkillMarket/Hub -> 安装时依赖检查和运行时读取都能识别同一个 App Skill 绑定
```

### 推进记录：审批/输入门控优先于工具策略，补齐运行期安全回归（2026-06-27）

本轮继续从“企业审批型应用 = 数据录入 + 审批工作流 Skill 运行 + 审批实例数据管理 + 结果反馈”的运行期角度推进。核心结论是：当当前节点还在等待表单输入、等待确认或等待审批时，工具暴露与工具执行都必须先被节点状态拦住，不能因为后续 `doc_only`、`planning` 或默认路由策略又把 `read_file`、`bash` 等工具补回来。

已落地调整：

```text
1. WorkflowEngine.IsPhaseExecutionBlocked 不再固定返回 false
   - 当前 phase 存在待审 PendingReviewPhaseID 且就是 CurrentPhase 时阻断执行。
   - 当前 phase 定义了 InputSchema，且 PhaseFormSubmitted / PhaseFormSkipped 都未发生时阻断执行。

2. GUI agent loop 工具暴露把“阶段阻断”放在工具策略之前
   - workflowToolFilterOwnerPolicyAndDecision 先检查 IsPhaseExecutionBlocked。
   - applyWorkflowFilter=true 且 ToolFilterNone 时明确清空工具列表。
   - 这把 ToolFilterNone 的两种语义区分开：未应用策略时不处理；已应用策略时代表当前阶段无可用工具。

3. doc_only / planning 阶段不允许具体执行型工具
   - 文档阶段和规划阶段允许检查/阅读类能力，但阻止 bash、write_file、edit_file、edit_lines、task、delegate_task 等会改变系统状态的工具。
   - loop command cycle 也同步禁止 doc-only 阶段通过 bash 绕过。

4. MCP 与 inline payload 安全边界收紧
   - call_mcp_tool.arguments 不是完整 JSON object 时，错误信息明确要求传入完整对象。
   - 超大 inline bash payload 在 handler 前被拒绝，不再自动放行；提示改为先写入或上传脚本。
   - MCP required-argument 预检查失败不再记为 dynamic tool execution result，因为该 MCP tool 实际没有被执行。
   - manage_skill 等真实动态工具参数失败仍保留 rejected audit 记录。
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestDocOnlyWorkflowPhaseBlocksImplementationTools|TestPlanningWorkflowPhaseAllowsInspectionButBlocksImplementationTools|TestLoopCommandCycleHonorsWorkflowPolicy|TestPrepareAgentLoopToolsUsesRuntimePolicyOwner|TestPrepareAgentLoopToolsBlockedPhaseWithNoPolicyExposesNoTools"
go test ./gui -count=1 -vet=off -run "TestPreCheckToolArgsForAgentLoopRejectsInvalidMCPArgumentsShape|TestPreCheckAgentLoopInlinePayloadLimitGuidesChunking|TestExecuteAgentLoopToolCallRejectsOversizedInlinePayloadBeforeHandler|TestRejectedDynamicToolArgumentFailureIsTrackedForAudit|TestCodingSubAgentRejectsMissingMCPRequiredArguments"
git diff --check -- corelib/workflow/v2/engine.go gui/im_agent_loop_tools.go gui/im_tool_execution.go gui/im_loop_command_callbacks.go gui/coding_subagent.go gui/coding_subagent_mcp.go
```

对应全链路意义：

```text
企业审批型 MaClaw App 发起节点 -> 动态 UI 提交数据 -> 未提交前不暴露执行工具
审批工作流 Skill 当前节点 -> 待确认/待审批 -> 不允许通过工具路由绕过节点状态
workflow/app/runtime skill 调用 MCP 或本地工具 -> 参数形态、payload 大小、动态工具审计在执行前完成
```

### 推进记录：App Studio 动态布局支持区域显示/隐藏并驱动运行界面（2026-06-27）

本轮继续补“所有 MaClaw App 的界面动态生成，应用程序设计时自动生成，用户调节位置，在应用信息文件中保存界面布局信息”这一条。此前 App Studio 已能选择布局模板、密度、主操作区、输出区，并能逐个调整 region placement；本轮补齐 region 可见性：

```text
App Studio / UI layout
  -> 每个 region 有显示开关
  -> 关闭后从布局预览里移除
  -> placement select 禁用，避免隐藏区域继续误调位置
  -> manifest.ui.layouts[entry].regions 写入 visible:false

App runtime panel
  -> 读取 runtimeWorkspaceLayoutForApp(app)
  -> input / instance_list / record_list / output 等区域按 visible 状态渲染
  -> 隐藏 output region 时，输出面板和运行历史不再显示
  -> 发布校验仍会检查必需 region role，避免无效企业应用进入市场
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "workspace region visibility"
npm.cmd test -- AppsPage.test.tsx -t "App Studio|layout|enterprise normal|approval"
npm.cmd run build
```

对应全链路意义：

```text
App Studio 自动生成动态 UI -> 用户可视化调整区域位置和可见性 -> 布局写入 maclaw.app.json / app package -> 运行界面按 manifest 布局渲染 -> 测试后上传 Hub 能携带同一布局契约
```

### 推进记录：布局可见性进入发布队列、安装记录与 DataSrv 回流（2026-06-27）

上一轮补了 App Studio 和运行面板对 `regions[].visible=false` 的支持。本轮把这件事继续推过分发/安装链路，避免用户在 Studio 里隐藏的区域只在本机草稿有效，上传、重装或 DataSrv 回流后又丢失。

已补齐验证点：

```text
GUI 提交队列
  SubmitMaclawAppPackage -> normalized package -> queued package
  保留 ui.layouts[entry].regions[].visible=false

DataSrv app_installations
  UpsertAppInstallation -> normalize workspace_layout -> SQLite persistence
  保留 workspace_layout.regions[].visible=false

前端 App Studio / runtime
  用户隐藏 region -> manifest 写入 visible:false
  runtime panel 按 visible 状态隐藏 output/history 等区块
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestSubmitMaclawAppPackagePersistsNormalizedWorkspaceLayout"
go test ./structureddata -count=1 -run "TestUpsertAppInstallationNormalizesGovernanceResultContract"
go test ./gui -count=1 -vet=off -run "SubmitMaclawAppPackage|PlanMaclawAppInstallNormalizesWorkspaceLayout|RecordMaclawAppInstall|InstallSelectedMaclawAppPackageFromHub|InstallMaclawAppPackageFromHub|ListMaclawAppInstalls"
go test ./structureddata -count=1 -run "AppInstallation|Approval|Capabilities|HTTPServerRequiresBearerTokenAndHandlesRecords"
npm.cmd test -- AppsPage.test.tsx -t "App Studio|layout|enterprise normal|approval|install records|app_installations"
```

对应全链路意义：

```text
App Studio 可视化布局 -> maclaw.app.json / app package -> 本地提交队列 -> Hub 安装包 -> RecordMaclawAppInstall -> DataSrv app_installations -> GUI 应用面板恢复
```

布局字段现在不只是前端状态，而是进入了 MaClaw App 分发和安装审计的持久契约。

### 推进记录：审核通过队列安装补齐 DataSrv 注册可视化证据（2026-06-27）

本轮继续从“本地制作 / 测试 / 上传 Hub / 审核通过 / 安装回本机 / DataSrv 注册 / 运行入口恢复”的全链路检查发布面板。后端安装返回的 `install_record.datasrv_registration` 已经存在，但发布队列里按单个 App 拆出的安装证据没有把父级 DataSrv 注册结果带下来，导致用户从“审核通过的队列项”直接安装后，只能看到依赖、版本、布局和测试证据，看不到企业普通应用或企业审批型应用是否已经完成 DataSrv 应用安装登记。

已落地调整：

```text
Hub 审核通过队列项
  -> Install approved app
  -> InstallMaclawAppPackageFromHub
  -> install_record.datasrv_registration
  -> installEvidenceRecordForApp 继承父级 DataSrv 注册结果
  -> InstallRecordEvidenceSnapshot 显示 DataSrv bindings registered / failed / skipped
```

现在发布队列中同一个审核通过安装结果会同时展示：

```text
Dependency verification
Version snapshot
Workspace layout
Result contract
Test evidence
DataSrv registration summary
```

对应全链路意义：

```text
App Studio 设计企业普通/审批型应用
  -> 测试证据与定义 hash 绑定
  -> 上传/同步到 Hub 审核
  -> 审核通过后从队列安装
  -> 自动安装依赖 Skill
  -> 记录安装版本与安装证据
  -> DataSrv app_installations 注册结果回显到发布队列
  -> 用户能确认“已安装 + 已登记 + 可运行”
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "installs an approved Hub app directly from the publish queue"
```

### 推进记录：审批实例管理的 lane/status 语义对齐（2026-06-27）

本轮继续检查“企业审批型应用 = 数据录入 + 审批工作流 Skill 运行 + 审批实例数据管理 + 结果反馈”中的审批实例管理环节。前端运行工作台和全局审批实例管理器已经提供“我的申请 / 待我审批 / 已处理 / 需关注 / 全部”、当前节点、审批结果、结果包和决策按钮；但后端本地/远端合并后的 lane 过滤仍主要依赖 `instance.Lane == lane`。这会导致旧数据或 DataSrv 回流数据出现以下不一致：

```text
status = approved / rejected
lane   = pending_my_approval  // 旧数据或外部回流未迁移

前端语义：应显示在“已处理”
后端旧语义：仍按 lane 字段过滤，可能从“已处理”漏掉
```

已落地调整：

```text
ListMaclawAppApprovalInstances / ListMaclawAppApprovalInstancesAll
  -> 本地审批实例过滤改为 maclawAppApprovalInstanceMatchesLane
  -> DataSrv 远端合并后的二次过滤也使用同一语义

maclawAppApprovalInstanceMatchesLane
  -> handled: approved / rejected / cancelled / timeout 或 lane=handled
  -> attention: status=attention 或 lane=attention
  -> pending_my_approval: 只有 status=pending 且 lane=pending_my_approval
  -> my_requests: 保持 lane=my_requests
```

对应全链路意义：

```text
审批工作流 Skill 运行/回写
  -> 本地审批实例 registry
  -> DataSrv RecordApproval 回流
  -> GUI 单 App 审批工作台
  -> GUI 全局审批实例管理器
  -> 我的申请 / 我审批的 / 已处理 / 需关注 分类一致
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestMaclawAppApprovalInstancesPersistAndFilter|TestListMaclawAppApprovalInstancesAll|TestListMaclawAppApprovalInstancesMergesDataSrvWithLocal"
```

### 推进记录：DataSrv 待审批 lane 支持 current_assignee（2026-06-27）

本轮继续检查 DataSrv 到 GUI 的审批实例回流。MaClaw App 审批型应用在运行时已经把“当前节点状态”拆成：

```text
current_node / workflow_node_ids
current_assignee
current_assignee_type
from_status / to_status
```

但 DataSrv 的 `/api/v1/data/approvals?lane=pending_my_approval` 旧查询只使用 `assigned_to = 当前用户`。如果审批工作流 Skill 或 MaClaw App 运行时只写入 `current_assignee`，或者当前节点负责人从个人切换到队列/节点上下文，GUI 从 DataSrv 拉取“待我审批”时可能漏掉该审批实例。

已落地调整：

```text
DataSrv SQLiteStore.ListRecordApprovals
  lane=pending_my_approval
  旧: status=pending AND assigned_to=current_user
  新: status=pending AND (assigned_to=current_user OR current_assignee=current_user)
```

对应全链路意义：

```text
审批型 App 发起
  -> 审批工作流 Skill 运行
  -> DataSrv RecordApproval 写入 current_assignee/current_node
  -> GUI 查询 pending_my_approval
  -> 我的审批列表能看到当前节点负责人为自己的审批实例
```

已通过定向验证：

```text
go test ./structureddata -count=1 -run "TestHTTPServerRecordApprovalsCarryMaClawAppSemantics"
```

### 推进记录：DataSrv app_installations 支持并行审批节点检索（2026-06-27）

本轮继续推进“安装登记 -> DataSrv 能力发现 -> GUI 恢复企业审批型 App”的链路。此前 DataSrv `ListAppInstallations` 已支持按 `workflow_skill_id` 和 `workflow_node` 从安装 metadata / test evidence 中筛选 App，但 `workflow_node` 只匹配单值字段：

```text
currentNode
current_node
workflowNode
workflow_node
```

审批工作流实际可能存在并行节点或多当前节点，GUI 与审批实例回流已使用：

```text
currentNodeIDs / current_node_ids
workflowNodeIDs / workflow_node_ids
workflowNodes / workflow_nodes
```

如果安装证据里只在数组字段中保留并行节点，DataSrv 的 app_installations 查询就无法按该节点找回对应 App，影响“按当前节点定位关联企业审批应用/安装记录”的全链路诊断。

已落地调整：

```text
appInstallationApprovalInstanceHasWorkflowNode
  -> 保留单值节点匹配
  -> 增加数组节点匹配:
     currentNodeIDs
     current_node_ids
     workflowNodeIDs
     workflow_node_ids
     workflowNodes
     workflow_nodes
```

对应全链路意义：

```text
App Studio / 测试证据
  -> approvalInstance.currentNodeIDs
  -> RecordMaclawAppInstall
  -> DataSrv app_installations metadata
  -> ListAppInstallations(workflow_node=并行节点)
  -> GUI / DataSrv 能力发现可恢复对应审批型 App
```

已通过定向验证：

```text
go test ./structureddata -count=1 -run "TestListAppInstallationsFiltersByApprovalInstanceEvidence"
```

### 推进记录：GUI 恢复 DataSrv installed App 时保留并行审批节点证据（2026-06-27）

本轮继续把上一段 DataSrv app_installations 的并行节点检索补到 GUI 恢复链路。DataSrv 安装记录和测试证据中可能保存：

```text
approval_instance.current_node
approval_instance.current_node_ids
approval_instance.workflow_node_ids
```

此前 GUI 从 DataSrv `app_installations` 恢复 installed MaClaw App 时，`normalizeAppRunApprovalInstanceEvidence` 只保留单个 `currentNode`，会丢失并行节点数组。这样虽然 App 可以恢复，但后续测试证据、发布校验详情、运行历史诊断都无法看到完整当前节点状态。

已落地调整：

```text
AppRunApprovalInstanceEvidence
  -> 增加 currentNodeIDs

normalizeAppRunApprovalInstanceEvidence
  -> 解析 currentNodeIDs/current_node_ids/workflowNodeIDs/workflow_node_ids/workflowNodes/workflow_nodes
  -> 如果缺少 currentNode，则使用数组第一个节点作为 currentNode
  -> 如果只有 currentNode，也回填 currentNodeIDs=[currentNode]

appRunApprovalInstanceEvidenceFromBackend
  -> 从 BackendApprovalInstance 继承 normalizeApprovalCurrentNodeIDs(instance)
```

对应全链路意义：

```text
审批工作流 Skill 产生并行节点
  -> DataSrv RecordApproval / app_installations 保存 current_node_ids
  -> GUI 从 DataSrv 能力发现恢复 installed App
  -> importedRunEvidence.approvalInstance.currentNodeIDs 保留完整节点状态
  -> 发布校验、运行历史和审批实例诊断共享同一证据
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "turns DataSrv installed MaClaw apps into addable app candidates with layout metadata"
```

### 推进记录：DataSrv OpenAPI 明确审批 workflow 节点映射合同（2026-06-28）

上一轮已经让 GUI 后端安装证据、前端安装审计恢复和 DataSrv capabilities 回流都保留 `workflow_mapping`。本轮继续把同一合同补到 DataSrv OpenAPI：此前 `workflow_mapping` 只是一个泛 `object`，外部 Hub、企业系统、生成客户端和审计工具只能猜测内部字段，容易丢失 submit/approval/result/attention 节点和状态映射。

已落地调整：

```text
DataSrv OpenAPI app_installations.metadata.workflow_mapping
  -> schema = maclaw.app.workflow.v1
  -> submitNode
  -> approvalNode
  -> resultNode
  -> attentionNode
  -> statusMapping.pending / approved / rejected / attention / requiresInput
```

对应全链路意义：

```text
App Studio 设计审批 workflow 节点
  -> GUI 安装/提交证据保留 workflow_mapping
  -> DataSrv 注册和 capabilities 回流保留 workflow_mapping
  -> OpenAPI 对外明确节点和状态字段
  -> Hub/企业系统/生成客户端可稳定读写当前节点状态、我的申请、我审批的、需关注和结果反馈
```

已通过定向验证：

```text
go test ./structureddata -count=1 -timeout 60s -run "TestAppInstallationOpenAPISchemaDocumentsFullTestEvidence"
```

### 推进记录：DataSrv OpenAPI 明确审批 workflow Skill 合同（2026-06-28）

本轮继续从企业审批型应用的核心链路检查公开合同。审批型应用不仅要保存 workflow 节点映射，还要保存“这个 App 依赖哪个审批 workflow Skill、适用哪个业务对象、需要哪些输入、会返回哪些审批输出和状态”的 workflow contract。前端和 GUI 后端已经使用 `workflow_contract` 做安装门禁、运行路由和审批实例视图，但 DataSrv OpenAPI 之前只声明为普通 object。

已落地调整：

```text
DataSrv OpenAPI app_installations.metadata.workflow_contract
  -> schema = maclaw.app.workflow_contract.v1
  -> workflowSkillId / workflowVersion
  -> objectRole
  -> requiredInputs
  -> decisionOutputs
  -> statusMapping.pending / approved / rejected / attention / requiresInput
  -> workflow_contract_required_inputs / workflow_contract_decision_outputs 作为外部系统和老数据回流兜底摘要字段
  -> workflow_contract_status_mapping 作为审批状态映射的回流兜底摘要字段
```

对应全链路意义：

```text
企业审批型应用
  -> App Studio 设计审批 workflow Skill 合同
  -> 安装时校验 workflow Skill 依赖与合同
  -> DataSrv 保存并对外暴露同一合同
  -> GUI 我的申请 / 我审批的 / 当前节点状态 / 结果反馈 使用同一份 workflow contract
```

已通过定向验证：

```text
go test ./structureddata -count=1 -timeout 60s -run "TestAppInstallationOpenAPISchemaDocumentsFullTestEvidence"
```

### 推进记录：审批 workflow contract 状态映射支持扁平摘要回流（2026-06-28）

本轮继续补齐 DataSrv/Hub/外部企业系统回流已安装审批型应用时的兼容合同。此前 `workflow_contract_required_inputs` 与 `workflow_contract_decision_outputs` 已作为扁平摘要字段被 GUI 前端读取，但状态映射只有完整 `workflow_contract.statusMapping` 时才能恢复；如果外部系统只保存摘要字段，审批型应用会退回默认状态，影响“我的申请 / 我审批的 / 需关注 / 结果反馈”的状态解释。

已落地调整：

```text
DataSrv normalizeAppInstallationWorkflowContractMetadata
  -> 从 workflow_contract.statusMapping / status_mapping 归一化 requiresInput
  -> 写入 workflow_contract_status_mapping 摘要
  -> 审计 metadata 记录 workflow_contract_status_mapping

DataSrv OpenAPI
  -> app_installations.metadata.workflow_contract_status_mapping 使用同一 statusMapping schema

GUI DataSrv capabilities 回流
  -> metadata.workflow_contract_status_mapping / workflowContractStatusMapping 作为 workflowContract.statusMapping fallback
  -> installEvidence.workflow_contract 与运行 workflow skill payload 保留恢复后的状态映射
```

对应全链路意义：

```text
外部系统或 Hub 只保存 workflow contract 摘要
  -> DataSrv capabilities 回流
  -> GUI 恢复完整审批 workflow contract
  -> 审批实例视图和 workflow skill 运行使用一致状态映射
```

已通过定向验证：

```text
go test ./structureddata -count=1 -timeout 60s -run "Test(UpsertAppInstallationNormalizesApprovalWorkflowContract|AppInstallationOpenAPISchemaDocumentsFullTestEvidence)"
npm.cmd test -- AppsPage.test.tsx -t "turns DataSrv installed MaClaw apps into addable app candidates with layout metadata"
```

### 推进记录：DataSrv 回流安装证据恢复结果覆盖率（2026-06-28）

本轮继续补齐“测试证据证明输出结果符合 result contract”的回流链路。DataSrv 已经归一化 `test_evidence.result_coverage`，并暴露 `test_evidence_result_coverage_ok / primary / covered_types / missing_types` 摘要字段；但 GUI 从 DataSrv capabilities 恢复已安装 App 时，`installEvidence.test_evidence` 没有从这些扁平字段恢复 `result_coverage`。这会让 App Studio 再发布、复测或治理回放时只能看到 result payload / outputs，而看不到测试运行覆盖了哪些声明结果类型。

已落地调整：

```text
GUI DataSrv capabilities 回流
  -> test_evidence.resultCoverage / result_coverage 原样保留
  -> test_evidence_result_coverage_ok 恢复为 result_coverage.ok
  -> test_evidence_result_coverage_primary 恢复为 result_coverage.primary
  -> test_evidence_covered_types 恢复为 result_coverage.covered_types
  -> test_evidence_missing_types 恢复为 result_coverage.missing_types
  -> installEvidence.test_evidence.result_coverage 可被发布治理/审计回放复用
```

对应全链路意义：

```text
App Studio 本地测试
  -> 生成 result coverage
  -> DataSrv / Hub 保存完整或扁平测试证据
  -> GUI 回流已安装 App
  -> 再发布、复测、治理检查知道输出结果是否覆盖 result contract
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "turns DataSrv installed MaClaw apps into addable app candidates with layout metadata"
npm.cmd run build
```

### 推进记录：DataSrv OpenAPI 明确应用输出结果合同（2026-06-28）

本轮继续从“应用输出结果有哪些、如何进入安装证据和发布治理”的角度补公开合同。DataSrv 已经保存 `result_contract` 并派生 `result_contract_primary / result_contract_types / result_contract_delivery`，GUI 前端也依赖该合同来渲染文本、文档、业务记录、审批结果和通知类输出。但 OpenAPI 之前只把 `result_contract` 写成泛对象，外部 Hub、企业系统和生成客户端无法稳定知道该对象内部结构。

已落地调整：

```text
DataSrv OpenAPI app_installations.metadata.result_contract
  -> schema = maclaw.app.result.v1
  -> primary
  -> types
  -> output_modes
  -> approval_decisions
  -> delivery.inlineContent / artifacts / businessRecord / notifications
  -> result_contract_delivery 作为兼容摘要字段继续文档化
```

对应全链路意义：

```text
App Studio 设计输出合同
  -> 运行历史和测试证据按 result contract 校验覆盖
  -> 发布治理判断 primary result 是否被覆盖
  -> DataSrv 注册和 capabilities 回流保留输出合同
  -> GUI / Hub / 外部企业系统可按合同识别审批结果、文档、文本内容、业务记录和通知输出
```

已通过定向验证：

```text
go test ./structureddata -count=1 -timeout 60s -run "TestAppInstallationOpenAPISchemaDocumentsFullTestEvidence"
```

### 推进记录：DataSrv OpenAPI 明确完整测试协议证据合同（2026-06-28）

前面已经让 DataSrv 归一化和 GUI 前端回流支持 `test_evidence_test_protocol`，可以从 capabilities 中恢复 App Studio 的完整测试协议。但 OpenAPI 只文档化了 `test_evidence_test_protocol_fingerprint`，没有公开 `sample_input / expected_output / roles / scopes / risk` 等复测所需字段，外部 Hub、企业系统和生成客户端仍可能只保存摘要。

已落地调整：

```text
DataSrv OpenAPI app_installations.metadata.test_evidence_test_protocol
  -> schema = maclaw.app.test_protocol.v1
  -> fingerprint
  -> sample_input
  -> expected_output
  -> required_roles
  -> required_scopes
  -> risk_level
```

对应全链路意义：

```text
App Studio 设计测试协议
  -> 本地运行证据绑定完整 test protocol
  -> DataSrv 注册和 capabilities 回流保留完整 test protocol
  -> OpenAPI 对外明确复测协议字段
  -> Hub 审核、企业审计、重新发布、自动复测不再只能依赖 fingerprint 摘要
```

已通过定向验证：

```text
go test ./structureddata -count=1 -timeout 60s -run "TestAppInstallationOpenAPISchemaDocumentsFullTestEvidence"
```

### 推进记录：安装证据自动固化依赖验证摘要（2026-06-27）
本轮继续补齐 MaClaw App 作为“超级 Skill”的安装证据链。此前 app 级 `install_evidence` 已保存依赖列表和阻断状态，但标准 `dependency_verification` 只在 manifest/governance 主动提供时才会出现；如果用户在 App Studio 设计后直接安装或从 Hub 安装一个没有预置验证块的 App，GUI/DataSrv 后续恢复时会缺少统一的依赖验证摘要。

已落地调整：

```text
maclawAppInstallEvidenceByApp
  -> dependency_verification 改为调用 maclawAppDependencyVerificationMetadataForEntry
  -> 优先保留 governance.dependency_verification
  -> 未预置时，根据安装计划中的 per-app dependencies 自动生成：
     schema=maclaw.app.install_plan.v1
     app_count=1
     dependency_count
     has_missing_required
     has_blocking_dependency
     dependencies
```

对应全链路意义：

```text
PlanMaclawAppInstall 依赖检查
  -> RecordMaclawAppInstall
  -> install_evidence[app_id].dependency_verification
  -> Hub 队列安装 / 本地安装 / DataSrv app_installations 回填
  -> GUI installed App 恢复与运行历史诊断可读取同一份依赖验证证据
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestMaclawAppInstallEvidenceGeneratesDependencyVerification|TestRecordMaclawAppInstallPersistsNewestInstallAudit"
```

### 推进记录：本地安装记录持久化依赖验证证据（2026-06-27）
本轮继续收口 MaClaw App 安装证据链的一致性。上一轮已让 app 级 `install_evidence` 在缺少 manifest 预置验证块时自动生成 `dependency_verification`；但本地 install registry 的 `maclawAppInstallRecord` 仍只保存 `dependencies`、`has_missing_required`、`has_blocking_dependency`，没有保存标准验证摘要。这样会造成：Hub/DataSrv/安装结果能看到标准依赖验证证据，而本地安装历史只能看到散落字段。

已落地调整：

```text
maclawAppInstallRecord
  -> 新增 dependency_verification

RecordMaclawAppInstall
  -> 写入每个 App 的 dependency_verification 快照
  -> 复用 maclawAppDependencyVerificationMetadataForEntry

ListMaclawAppInstalls
  -> 返回记录时克隆 dependency_verification，避免调用侧修改 registry 快照
```

对应全链路意义：

```text
安装计划依赖检查
  -> 本地 install registry
  -> ListMaclawAppInstalls
  -> GUI 安装历史 / 依赖复检 / 修复入口
  -> 与 Hub install_record、DataSrv metadata、install_evidence 使用同一种依赖验证证据结构
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestMaclawAppInstallEvidenceGeneratesDependencyVerification|TestRecordMaclawAppInstallPersistsNewestInstallAudit"
```

### 推进记录：安装历史界面展示依赖验证证据（2026-06-27）
本轮继续把后端新增的 `dependency_verification` 快照接到 GUI 可见层。此前安装历史只展示包 SHA、依赖数量、依赖列表、版本快照、布局/结果/测试证据和 DataSrv 注册状态；即使本地 install registry 已保存标准依赖验证证据，用户也无法在安装历史证据区直接看到这份标准化摘要。

已落地调整：

```text
InstallRecordEvidenceSnapshot / installRecordEvidenceItems
  -> 读取 record.dependency_verification
  -> 优先使用 dependency_verification.dependencies 计算阻断项
  -> 缺失时回退 record.dependencies / has_missing_required / has_blocking_dependency
  -> 在证据快照中展示：
     Dependency verification
     Skill dependencies: N · Blocking deps: M
```

对应全链路意义：

```text
RecordMaclawAppInstall 写入 dependency_verification
  -> ListMaclawAppInstalls 返回本地安装历史
  -> GUI 安装历史证据快照展示依赖验证摘要
  -> 用户可以从同一处看到 App 包、依赖状态、测试证据、DataSrv 注册状态
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "shows recent app install records in the market pane"
```

### 推进记录：DataSrv app-installations 支持依赖健康过滤（2026-06-27）
本轮继续补齐企业 Hub / DataSrv / GUI 的安装治理链路。此前 `app_installations` 已保存 `dependency_verification`、`dependency_count`、`has_missing_required_dependency`、`has_blocking_dependency` 等元数据，但 `/api/v1/data/app-installations` 只能按 app、kind、source、status、workflow skill/node 过滤，无法直接列出“安装后依赖阻断”或“缺少必需依赖”的 App。

已落地调整：

```text
QueryAppInstallationsInput
  -> 新增 HasBlockingDependency
  -> 新增 HasMissingRequiredDependency

GET /api/v1/data/app-installations
  -> has_blocking_dependency=true|false
  -> has_missing_required_dependency=true|false
  -> 兼容别名 has_missing_required=true|false

SQLite ListAppInstallations
  -> metadata filter 支持：
     has_blocking_dependency
     test_evidence_dependency_blocking
     dependency_verification.has_blocking_dependency / hasBlockingDependency
     test_evidence.dependency_verification.has_blocking_dependency / hasBlockingDependency
     has_missing_required_dependency
     has_missing_required
     test_evidence_dependency_missing_required
     dependency_verification.has_missing_required / hasMissingRequired
     test_evidence.dependency_verification.has_missing_required / hasMissingRequired

OpenAPI
  -> 公开 has_blocking_dependency
  -> 公开 has_missing_required_dependency
```

对应全链路意义：

```text
MaClaw App 安装 / Hub 安装 / DataSrv 注册
  -> dependency_verification 标准证据入库
  -> DataSrv 可直接查询依赖阻断或缺失必需依赖的 App
  -> 企业运维 / GUI 能力发现 / 安装修复入口可按依赖健康状态定位问题 App
```

已通过定向验证：

```text
go test ./structureddata -count=1 -run "TestListAppInstallationsFiltersByDependencyHealth|TestListAppInstallationsFiltersByApprovalInstanceEvidence|TestHTTPServerAppInstallations"
go test ./structureddata -count=1 -run "TestHTTPServerOpenAPIIncludesExpectedQueryParameters|TestAppInstallationOpenAPISchemaDocumentsFullTestEvidence"
```

### 推进记录：GUI MIS Data Tool 暴露 app-installations 依赖健康查询（2026-06-27）
本轮继续把上一段 DataSrv `app-installations` 依赖健康过滤接到 GUI/agent 工具链。此前 DataSrv 已支持 `has_blocking_dependency`、`has_missing_required_dependency` 查询参数，但 GUI 的 `executeMISDataTool` 没有 `list_app_installations` action，agent/工具调用侧无法直接列出安装后依赖阻断或缺少必需依赖的 MaClaw App。

已落地调整：

```text
MIS Data Tool
  -> 新增 action: list_app_installations
  -> 支持过滤参数：
     app_id / appId
     blueprint_id / blueprintId
     kind
     source
     status
     workflow_skill_id / workflowSkillId
     workflow_node / workflowNode / current_node / currentNode
     has_blocking_dependency / hasBlockingDependency
     has_missing_required_dependency / hasMissingRequiredDependency
     has_missing_required / hasMissingRequired
     before / before_id / beforeId
     limit
  -> 布尔过滤支持 true 和 false，避免只能查询阻断项、不能查询健康项
```

对应全链路意义：

```text
DataSrv app-installations 依赖健康过滤
  -> GUI MIS Data Tool list_app_installations
  -> agent / App Studio / 运维辅助可以按依赖阻断状态定位已安装 App
  -> 安装修复入口与企业治理诊断可以复用同一查询契约
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestExecuteMISDataToolListAppInstallationsPassesDependencyFilters"
```

### 推进记录：DataSrv 与 MIS Tool 支持审批结果/参与人/输出类型过滤（2026-06-27）
本轮继续从“企业审批型应用 = 数据录入 + 审批工作流 skill 运行 + 审批实例数据管理 + 结果反馈”的闭环检查缺口。此前 `app_installations` 已能保存 workflow contract、approval instance evidence、result contract、test evidence outputs/artifacts，但列表查询主要只能按 App、workflow skill/node、依赖健康筛选；GUI/App Studio/agent 要做“我的申请”“我审批的”“已通过/已拒绝/需关注”“返回文档/文本内容”等视图时，仍需要拉全量 metadata 后自行扫描。

已落地调整：

```text
QueryAppInstallationsInput
  -> 新增 ApprovalStatus
  -> 新增 ApprovalDecision
  -> 新增 ApplicantID
  -> 新增 ApproverID
  -> 新增 ResultType

GET /api/v1/data/app-installations
  -> approval_status / approval_result_status
  -> approval_decision / decision
  -> applicant_id / submitted_by / created_by
  -> approver_id / assigned_to / current_assignee
  -> result_type / output_type

SQLite ListAppInstallations metadata filter
  -> 支持从顶层 metadata、test_evidence、approval_instance、result_payload 中匹配审批状态/审批决定
  -> 支持 applicant/requester/submitted_by 与 approver/current_assignee/assigned_to
  -> 支持从 result_contract、result_coverage、outputs、artifacts 中匹配 approval_result、document、inline_content、table、business_status 等输出类型

OpenAPI
  -> 公开 approval_status
  -> 公开 approval_decision
  -> 公开 applicant_id
  -> 公开 approver_id
  -> 公开 result_type

MIS Data Tool list_app_installations
  -> 同步支持上述过滤参数及 camelCase/常用别名
```

对应全链路意义：

```text
审批型 MaClaw App 运行/测试/安装记录
  -> DataSrv 保存 approval instance + result package evidence
  -> app-installations 可按审批状态、参与人、输出类型直接查询
  -> GUI 应用面板可构建“我的申请 / 我审批的 / 审批结果 / 文档或内容输出”视图
  -> App Studio / agent 测试与运维诊断可复用同一查询契约，不再依赖前端扫 metadata
```

已通过定向验证：

```text
go test ./structureddata -count=1 -run "TestListAppInstallationsFiltersByApprovalResultMetadata|TestListAppInstallationsFiltersByDependencyHealth|TestHTTPServerOpenAPIIncludesExpectedQueryParameters"
go test ./gui -count=1 -vet=off -run "TestExecuteMISDataToolListAppInstallationsPassesDependencyFilters"
```

### 推进记录：GUI 审批实例管理接入 DataSrv app-installations 审批概览（2026-06-27）
本轮继续把上一段 DataSrv 查询能力接到 MaClaw App 面板。此前 `/api/v1/data/app-installations` 已支持按审批状态、审批决定、申请人、审批人、输出类型过滤，但 GUI 的“审批实例管理”仍主要依赖本地审批实例列表；用户无法在同一个工作台上直接看到 DataSrv 侧已经登记的审批型应用结果概览。

已落地调整：

```text
AppsPage ApprovalManager
  -> 新增 DataSrv approval overview 汇总栏
  -> 读取 MIS DataSrv 配置与 token
  -> 并行查询：
     kind=enterprise_approval_app
     applicant_id=<current user>
     approver_id=<current user>
     approval_status=approved / rejected / attention
     result_type=document / inline_content
  -> 显示每类匹配数量与最近应用名称
  -> DataSrv 未启用、加载中、读取失败、无匹配结果均有对应状态

界面风格
  -> 保持企业工作台密度
  -> 使用现有审批管理的边框、字号、状态色和列表语义
  -> 不改变本地审批实例列表和审批详情操作流
```

对应全链路意义：

```text
DataSrv app-installations 审批过滤契约
  -> GUI 审批实例管理直接消费
  -> 我的申请 / 我审批的 / 已通过 / 已拒绝 / 需关注 / 文档输出 / 文本输出
  -> 审批型 App 的实例数据管理与结果反馈开始在同一个应用工作台露出
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "shows DataSrv approval app summary with approval result filters"
npm.cmd test -- AppsPage.test.tsx -t "opens global approval management from the operation section"
完整 npm.cmd run build 当轮曾被不相关并行改动阻塞，后续已重新通过：
src/components/ai/AIAssistantPanel.tsx(2049,66): Property 'textSecondary' does not exist on type 'Theme'.
```

### 推进记录：GUI DataSrv 审批明细携带实例/记录标识并驱动真实实例过滤（2026-06-27）
本轮继续把 DataSrv app-installations 审批概览从“看得到明细”推进到“能定位具体实例”。上一段已经可以点入分类并查看应用明细，但明细行主要依靠 `app_id` 或应用名去过滤审批实例列表；当同一个 App 下有多个审批实例、多个业务记录或多个工作流实例时，这仍然不够精确。

已落地调整：

```text
DataSrv approval summary item
  -> 新增 approvalID
  -> 新增 workflowInstanceID
  -> 新增 recordID

DataSrv metadata 解析
  -> 从 test_evidence.approval_instance / approval_instance 中读取：
     approval_id / record_approval_id
     workflow_instance_id
     record_id / business_record_id
  -> 从 result_payload 与顶层 metadata 中读取同名兼容字段

GUI 点击行为
  -> 点击 DataSrv 明细行时优先按 approvalID 过滤
  -> 其次按 workflowInstanceID / recordID
  -> 最后才回退到 appID / app 名称

GUI 明细展示
  -> 在 DataSrv 明细行中显示 approvalID / workflowInstanceID / recordID
  -> 保持企业工作台密集列表风格，不新增跳转式页面
```

对应全链路意义：

```text
DataSrv app-installations 审批结果证据
  -> GUI DataSrv 审批概览明细
  -> approval_id / workflow_instance_id / record_id
  -> 合并后的审批实例列表搜索命中具体实例
  -> 后续可继续升级为“打开 DataSrv approval detail / business record detail”的直达操作
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "shows DataSrv approval app summary with approval result filters"
npm.cmd test -- AppsPage.test.tsx -t "opens global approval management from the operation section"
npm.cmd run build
```

### 推进记录：GUI 安装证据恢复审批结果包（2026-06-27）

本轮继续补齐“Hub/能力市场安装 -> App 面板恢复 -> 运行态/审批态证据可用”的断点。此前单 App 市场安装记录已经能从顶层 install record fallback 恢复 `workspace_layout`、`result_contract`、`workflow_contract`、`test_evidence` 和依赖校验，但审批型应用的完整结果包可能只存在于 `test_evidence.approval_instance` 内部；如果顶层 `result_payload / outputs / artifacts` 缺失，App 面板的运行证据和审核证据检查容易看不到结果内容。

已落地调整：

```text
DataSrv / Hub install evidence -> importedRunEvidence
  -> 合成 test_evidence 时保留 metadata.test_evidence_approval_instance
  -> 若 test_evidence 顶层缺少 result_payload / outputs / artifacts
     从 approval_instance.result_payload / outputs / artifacts 提升为 run evidence
  -> approval_instance 本身继续保留 approval_id / workflow_instance_id / current_node
     workflow_skill_id / result_status / approval_instance_view_verified 等字段

市场单 App 安装 fallback
  -> 顶层 install record 不含 install_evidence map 时仍可恢复单 App 证据
  -> 依赖证据继续按当前 app_id 过滤，不把同包其它 App 的依赖混入当前 App
  -> 嵌套审批结果包恢复到 localStorage 中的 importedRunEvidence
```

对应全链路意义：

```text
能力市场下载安装 MaClaw App
  -> 安装时完成 skill 依赖检查和安装证据记录
  -> App 面板从安装记录恢复动态 UI / 结果契约 / 工作流契约
  -> 审批型 App 从审批实例证据恢复 resultPayload / outputs / artifacts
  -> 用户进入传统软件式 App 工作台时可以直接看到审批结果、业务状态、业务记录和文件产物
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "restores single app install evidence from top-level market install records"
npm.cmd test -- AppsPage.test.tsx -t "imports enterprise approval app capabilities from DataSrv app-installations|keeps dependency verification visible after single market app install|restores single app install evidence from top-level market install records"
npm.cmd run build
```

### 推进记录：审批 workflow 失败/取消也生成可审计结果包（2026-06-27）

本轮继续补齐“审批型应用 = 数据录入 + 审批 workflow Skill 运行 + 审批实例数据管理 + 结果反馈”的运行时闭环。此前 workflow 正常完成时会从 Skill 输出恢复 `resultPayload / outputs / artifacts`，但当 workflow 失败或取消且没有结构化输出时，前端只会把审批实例状态标为 `attention`，`result_payload` 可能为空。对企业审批型应用来说，“需关注（仅查看）”也是结果反馈，仍需要能进入审批实例、DataSrv 同步和审计证据。

已落地调整：

```text
approvalWorkflowResultFromSkillRunStatus
  -> 无论 workflow 正常完成、失败还是取消，都生成最小结果包
  -> 默认补齐 approval_result / business_status / result_status / text / workflow_lifecycle
  -> Skill 输出中的结构化 result_payload 仍优先保留，不被默认字段覆盖

审批实例运行闭环
  -> 本地 RecordMaclawAppApprovalInstance 的 attention 实例带 result_payload
  -> SyncMaclawAppApprovalInstanceToDataSrv 入参带同一份 result_payload
  -> 后续 DataSrv app_installations / 审批工作台 / 发布证据检查不再出现“有 attention 状态但无结果包”的空洞
```

对应全链路意义：

```text
审批 workflow Skill 运行异常或取消
  -> GUI 将实例归入需关注 lane
  -> 审批实例保存最小可审计结果包
  -> DataSrv 同步可记录失败/取消原因和生命周期
  -> 我的申请 / 我审批的 / 需关注 / 结果反馈仍可看到明确结果
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "marks approval workflow failures as attention results"
npm.cmd test -- AppsPage.test.tsx -t "records and completes an approval instance when running an approval app|marks approval workflow failures as attention results|keeps approval result package when a pending item is manually approved"
npm.cmd run build
```

### 推进记录：DataSrv 安装注册摘要提升嵌套审批结果包（2026-06-27）

本轮继续把上一段运行态结果包推进到安装注册和 DataSrv 回流层。前端已经能从 `test_evidence.approval_instance.resultPayload / outputs / artifacts` 恢复运行证据，但 GUI 后端 `RecordMaclawAppInstall` 在生成 DataSrv `app_installations.metadata` 摘要时，仍只读取顶层 `testEvidence.resultPayload / outputs / artifacts`。如果 App Studio 或 Hub 包只把完整结果包保存在 `approvalInstance` 内部，DataSrv 顶层 `test_evidence_result_payload`、`test_evidence_outputs`、`test_evidence_artifacts` 和计数字段会缺失，影响 `result_type` 查询、外部集成和能力回流摘要。

已落地调整：

```text
applyMaclawAppDataSrvTestEvidenceMetadata
  -> 先识别 testEvidence.approvalInstance / approval_instance / approval
  -> 顶层 resultPayload 缺失时，从 approvalInstance.resultPayload / result_payload 提升
  -> 顶层 outputs 缺失时，从 approvalInstance.outputs 提升
  -> 顶层 artifacts 缺失时，从 approvalInstance.artifacts 提升
  -> output_count / artifact_count 同步使用提升后的结果包
  -> 显式顶层 testEvidence.resultPayload / outputs / artifacts 仍优先，不被嵌套字段覆盖
```

对应全链路意义：

```text
App Studio 测试运行生成审批实例证据
  -> Hub/能力市场安装包保存完整 approvalInstance
  -> GUI 安装注册到 DataSrv app_installations
  -> DataSrv metadata 同时具备完整 approvalInstance 和顶层结果摘要
  -> GUI / agent / 外部系统可按结果类型、文件、文本、审批结果稳定查询和恢复
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "Test(ApplyMaclawAppDataSrvTestEvidenceMetadataPromotesNestedApprovalResultPackage|RecordMaclawAppInstallRegistersApprovalAppWithDataSrv|RecordMaclawAppInstallRegistersEnterpriseApprovalAppEvidence)"
```

### 推进记录：DataSrv 直接归一化嵌套审批结果包（2026-06-27）

本轮继续把“嵌套审批结果包提升为标准结果摘要”的能力从 GUI 后端安装注册入口下沉到 DataSrv 自身。这样即使外部 Hub、企业集成方或其它客户端直接调用 DataSrv `UpsertAppInstallation`，只提交 `test_evidence.approval_instance.result_payload / outputs / artifacts`，DataSrv 也能生成标准的顶层测试证据摘要，而不是依赖 GUI 预处理。

已落地调整：

```text
DataSrv normalizeAppInstallationTestEvidenceMetadata
  -> 先识别 test_evidence.approvalInstance / approval_instance / approval
  -> 顶层 result_payload 缺失时，从 approval_instance.result_payload 提升
  -> 顶层 outputs 缺失时，从 approval_instance.outputs 提升
  -> 顶层 artifacts 缺失时，从 approval_instance.artifacts 提升
  -> output_count / artifact_count 自动补齐
  -> 显式顶层 test_evidence.result_payload / outputs / artifacts 仍优先
```

对应全链路意义：

```text
Hub / 外部企业系统 / GUI 后端任一路径注册 MaClaw App
  -> DataSrv app_installations.metadata 归一化到同一证据形态
  -> result_type=document / inline_content / approval_result 等查询能命中嵌套结果包
  -> capabilities 回流、GUI 应用面板恢复、agent 诊断、企业审计读取同一份标准摘要
```

已通过定向验证：

```text
go test ./structureddata -count=1 -timeout 60s -run "Test(UpsertAppInstallationPromotesNestedApprovalInstanceResultPackage|ListAppInstallationsFiltersByApprovalResultMetadata|UpsertAppInstallationPreservesFullEnterpriseApprovalTestEvidence)"
```

### 推进记录：OpenAPI 明确嵌套审批结果包提升合同（2026-06-27）

本轮继续把 DataSrv 直接归一化能力补进公开合同。此前实现已经能把 `approval_instance.result_payload / outputs / artifacts` 提升到标准 `test_evidence_result_payload / test_evidence_outputs / test_evidence_artifacts`，但 OpenAPI 字段描述只说明这些是测试输出结果，没有明确“可由 approval_instance 内部结果包提升而来”。对 Hub、企业集成方、生成客户端和审计工具来说，字段来源不明确会导致不同入口写入形态不一致。

已落地调整：

```text
DataSrv OpenAPI app_installations.metadata
  -> test_evidence_result_payload 描述明确可从 approval_instance.result_payload 提升
  -> test_evidence_outputs 描述明确可从 approval_instance.outputs 提升
  -> test_evidence_artifacts 描述明确可从 approval_instance.artifacts 提升
  -> schema 测试检查 description 必须包含 promoted from approval_instance
```

对应全链路意义：

```text
App Studio / Hub / 企业系统 / GUI 后端 任一路径提交测试证据
  -> 可选择顶层 test_evidence 结果包或嵌套 approval_instance 结果包
  -> DataSrv 归一化后对外暴露同一套标准摘要字段
  -> 外部客户端按 OpenAPI 生成后能稳定读取审批结果、文档、文本内容和文件产物
```

已通过定向验证：

```text
go test ./structureddata -count=1 -timeout 60s -run "Test(AppInstallationOpenAPISchemaDocumentsFullTestEvidence|UpsertAppInstallationPromotesNestedApprovalInstanceResultPackage|ListAppInstallationsFiltersByApprovalResultMetadata)"
```

### 推进记录：HTTP app-installations 验证嵌套审批结果包回流与过滤（2026-06-27）

本轮把上一段 DataSrv 服务层和 OpenAPI 合同继续推进到真实 HTTP 入口。新增 HTTP 回归场景直接向 `/api/v1/data/app-installations` 提交一个仅在 `test_evidence.approval_instance` 内部携带结果包的审批型 App，验证 DataSrv 返回体和后续列表查询都能使用提升后的标准摘要。

已落地验证：

```text
HTTP POST /api/v1/data/app-installations
  -> 输入只有 approval_instance.result_payload / outputs / artifacts
  -> 返回 metadata.test_evidence.result_payload 已提升
  -> 返回 test_evidence_result_payload / output_count / artifact_count 摘要已补齐

HTTP GET /api/v1/data/app-installations?result_type=attention_document
  -> 能按 approval_instance.outputs.kind 命中该 App
  -> 证明外部客户端不需要知道结果包原始嵌套位置
```

对应全链路意义：

```text
Hub 下载重装 / 企业系统直接注册 / GUI 后端注册
  -> HTTP 层接受不同证据形态
  -> DataSrv 归一化并返回统一 metadata
  -> GUI App 面板、agent、Hub 审核、企业审批列表可按 result_type 直接筛选
```

已通过定向验证：

```text
go test ./structureddata -count=1 -timeout 60s -run "TestHTTPServerRequiresBearerTokenAndHandlesRecords"
```

### 推进记录：GUI DataSrv 回流保留审批实例内部结果包（2026-06-28）

上一轮已经在 DataSrv HTTP 层验证 `/api/v1/data/capabilities` 会返回完整 `test_evidence`。本轮继续把同一要求推进到 GUI 前端恢复链路：从 DataSrv capabilities 加入本地 App 面板后，不能只保留 run history 顶层 `resultPayload / outputs / artifacts`，还必须保留 `approvalInstance` 内部自己的结果包。

已落地验证：

```text
DataSrv capabilities app_installations[].metadata.test_evidence.approval_instance
  -> approval_instance.result_payload 保留到 AppEntry.installEvidence.test_evidence.approval_instance.result_payload
  -> approval_instance.outputs 保留到 AppEntry.installEvidence.test_evidence.approval_instance.outputs
  -> approval_instance.artifacts 保留到 AppEntry.installEvidence.test_evidence.approval_instance.artifacts
  -> 同时恢复到 importedRunEvidence.approvalInstance.resultPayload / outputs / artifacts
```

对应全链路意义：

```text
DataSrv 已安装审批型 App
  -> GUI App Studio 可添加候选
  -> 本地 AppEntry 同时拥有 installEvidence 和 importedRunEvidence
  -> 审批实例证据可独立判断业务状态、结果内容和文件产物
  -> Hub/DataSrv 恢复后再次发布不会因为只剩顶层运行结果而误判审批实例证据不完整
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "turns DataSrv installed MaClaw apps into addable app candidates with layout metadata"
```

### 推进记录：DataSrv 查询兼容历史嵌套-only 审批结果包（2026-06-27）

本轮继续从“现有已安装数据能否回流”的角度补齐兼容性。前面已经保证新写入的 `app_installations.metadata` 会把 `approval_instance.result_payload / outputs / artifacts` 提升为标准摘要；但历史记录或外部旧客户端可能已经只保存了 `test_evidence_approval_instance`，没有顶层 `test_evidence_outputs`、`test_evidence_artifacts`、`test_evidence_result_payload`。如果查询侧只看新摘要，这些旧记录仍无法按 `result_type` 或 `approval_decision` 找到。

已落地调整：

```text
DataSrv app_installations 查询侧
  -> appInstallationResultPayloads 额外扫描 approval_instance.result_payload / resultPayload
  -> result_type 过滤额外扫描 approval_instance.outputs / artifacts
  -> 兼容 metadata.test_evidence_approval_instance 和 test_evidence.approval_instance 两种旧形态
```

对应全链路意义：

```text
升级前已经存在的 MaClaw App 安装证据
  -> 不需要重新注册或迁移也能被 DataSrv 查询命中
  -> result_type=document / artifact / 自定义输出类型可回流
  -> approval_decision=attention 等查询可从嵌套 result_payload 中识别
  -> GUI 审批工作台、Hub 审核和 agent 诊断不会只适用于新安装数据
```

已通过定向验证：

```text
go test ./structureddata -count=1 -timeout 60s -run "Test(ListAppInstallationsFiltersLegacyNestedApprovalResultPackage|UpsertAppInstallationPromotesNestedApprovalInstanceResultPackage|ListAppInstallationsFiltersByApprovalResultMetadata)"
go test ./structureddata -count=1 -timeout 60s -run "TestHTTPServerRequiresBearerTokenAndHandlesRecords"
```

### 推进记录：DataSrv 安装元数据 OpenAPI 补齐结构化依赖验证合同（2026-06-27）
本轮继续从“安装、测试、上传、运行、审批实例管理”全链路核对，发现前面已经把 `dependencyVerification` / `dependency_verification` 的后端归一化、筛选、运行证据和发布门禁打通，但 DataSrv OpenAPI 只文档化了扁平摘要字段与旧的 `dependencies` 快照，缺少新的结构化 `dependency_verification` 对象。这会让 GUI、Hub、App Studio 或外部企业集成方无法稳定获知依赖验证证据的标准形态。

已落地调整：

```text
DataSrv OpenAPI app installation metadata
  -> 新增 dependency_verification 对象 schema
  -> 文档化 verified_at / dependency_count / has_missing_required / has_blocking_dependency
  -> 文档化 has_governance_review_issue / governance_review_issue_count
  -> 文档化 has_workflow_contract_issue / workflow_contract_issue_count
  -> dependency_verification.dependencies 复用依赖条目 schema
  -> 依赖条目明确包含 id / version / kind / source / required / installed / health / action / app_ids / installed_status / message

DataSrv schema 测试
  -> 扩展 TestAppInstallationOpenAPISchemaDocumentsFullTestEvidence
  -> 检查 dependency_verification 顶层对象、内部字段和依赖明细字段
```

对应全链路意义：

```text
MacLaw App 安装依赖检查
  -> DataSrv 归一化保存 dependency_verification
  -> OpenAPI 明确标准字段
  -> GUI/App Studio/Hub 可按合同读取依赖证据
  -> 安装修复、运行前检查、发布门禁、企业审计不再依赖未文档化 metadata 约定
```

已通过定向验证：
```text
go test ./structureddata -count=1 -timeout 60s -run "Test(UpsertAppInstallationNormalizesTopLevelDependencyVerification|AppInstallationOpenAPISchemaDocumentsFullTestEvidence)"
```

### 推进记录：前端安装证据回灌兼容单 App 顶层证据（2026-06-27）
本轮继续检查“Hub/能力市场安装 -> RecordMaclawAppInstall -> 前端写入本地 AppEntry -> 运行/发布复核复用安装证据”的链路。此前前端恢复安装证据时主要读取：

```text
install_record.install_evidence[app_id]
```

这适合多 App 包按子 App 拆分证据的场景，但如果 Hub/DataSrv/后端返回的是单 App 安装记录，证据直接位于顶层：

```text
install_record.dependency_verification
install_record.test_evidence
install_record.workspace_layout
install_record.result_contract
install_record.dependencies
```

前端会展示安装成功，却没有把这些顶层证据合成为 `AppEntry.installEvidence`，导致后续运行前依赖诊断、发布复核、冷启动恢复和 App Studio 管理页看不到同一份安装证据。

已落地调整：

```text
installEvidenceRecordForApp
  -> 优先读取 install_evidence[app_id]
  -> 缺少 per-app evidence 时回退单 App 顶层安装记录
  -> 按当前 App canonical/market/datasrv-installed identity 过滤 dependencies
  -> dependency_verification.dependencies 同步过滤到当前 App 范围
  -> 保留 workspace_layout / result_contract / workflow_contract / test_evidence / dependency_verification

App Studio 市场安装结果
  -> 单 App 顶层安装证据也能显示测试证据和依赖验证摘要
  -> 未安装子 App 的依赖不会污染当前 App 的本地 installEvidence
```

对应全链路意义：

```text
Hub/能力市场安装
  -> 后端返回单 App 顶层安装证据
  -> 前端写入 AppEntry.installEvidence
  -> 运行态依赖检查、发布复核、DataSrv 回流、冷启动恢复使用同构证据
```

已通过定向验证：
```text
npm.cmd test -- AppsPage.test.tsx -t "keeps dependency verification visible after single market app install|restores single app install evidence from top-level market install records"
```

### 推进记录：DataSrv 保留完整企业审批测试证据并支持实例级查询（2026-06-27）
本轮继续补齐“企业审批型应用 = 数据录入 + 审批 workflow Skill 运行 + 审批实例数据管理 + 结果反馈”的 DataSrv 证据层。前面已经要求 `test_evidence` 不能退化成摘要计数；本轮用服务层测试把这个合同锁住：完整嵌套的审批实例证据进入 `app_installations.metadata` 后，必须仍能通过 capabilities 和查询接口被恢复与定位。

已验证并加固的合同：

```text
metadata.governance.testEvidence
  -> resultPayload 原样进入 metadata.test_evidence.result_payload
  -> outputs 原样进入 metadata.test_evidence.outputs
  -> artifacts 原样进入 metadata.test_evidence.artifacts
  -> approvalInstance 原样进入 metadata.test_evidence.approval_instance
  -> approvalInstance.resultPayload / outputs / artifacts 不被裁剪
  -> 同时派生轻量摘要：
     test_evidence_output_count
     test_evidence_artifact_count
     test_evidence_approval_instance_id
     test_evidence_approval_id
     test_evidence_record_id
     test_evidence_approval_status
```

同时确认 DataSrv 查询能用完整证据定位已安装 App：

```text
workflow_skill_id
workflow_node
approval_status
approval_decision
dataset_id
object_role
record_id
approval_id
workflow_instance_id
result_type
```

对应全链路意义：

```text
审批 workflow Skill 运行完成
  -> GUI / Hub / DataSrv 注册完整 test_evidence
  -> DataSrv capabilities 回读完整审批实例与结果包
  -> GUI 我的申请 / 待我审批 / 已处理 / 需关注 / 结果反馈可按实例字段查询
  -> 后续二次发布、审计和诊断不再只能依赖计数摘要
```

已通过定向验证：
```text
go test ./structureddata -count=1 -timeout 60s -run "TestUpsertAppInstallationPreservesFullEnterpriseApprovalTestEvidence"
```

### 推进记录：App Studio 运行证据写回改为企业证据深合并（2026-06-27）
本轮继续检查“App Studio 测试运行 -> 写回 Skill/App 定义文件 -> 上传 Hub 能力市场”的证据链。此前后端 `RecordMaclawAppRunEvidenceForSkill` 已能在只刷新 runId、verifiedAt、definitionHash、artifactName 等新鲜度字段时保留旧的企业证据；但底层合并是浅合并，一旦未来前端或其它调用方传入部分 `approvalInstance`、部分 `dependencyVerification`，或者空的 `outputs/artifacts`，就可能把已经完整的审批实例结果包覆盖成不完整证据。

已落地调整：

```text
mergeMaclawAppRunEvidence
  -> 新鲜度字段继续覆盖更新
  -> resultPayload 深合并，保留旧 approval_result 等结果字段
  -> approvalInstance 深合并，保留 instanceId / approvalID / workflowSkillId 等身份字段
  -> approvalInstance.resultPayload 深合并
  -> dependencyVerification 深合并，保留 dependencies 明细
  -> outputs / artifacts 只有新值为非空数组时才替换
  -> 空 outputs / artifacts 不再清空已有结果包
```

对应全链路意义：

```text
App Studio 测试运行
  -> 后端写回 maclaw.app.json / maclaw.apps.json
  -> 当前运行的新鲜度更新
  -> 已验证的审批实例、结果 payload、输出块、文件、依赖验证明细继续保留
  -> 发布门禁和 Hub 安装包不会因为一次简化写回退化为空壳证据
```

已通过定向验证：
```text
go test ./gui -count=1 -vet=off -run "Test(RecordMaclawAppRunEvidenceForSkill(PreservesEnterpriseEvidence|WritesGovernance|UpdatesMaclawAppsManifest)|MergeMaclawAppRunEvidenceDeepPreservesEnterpriseEvidence)"
```

### 推进记录：DataSrv app-installations 公开审批实例级过滤别名（2026-06-27）
本轮继续补齐“完整审批证据能被外部链路使用”的接口层。DataSrv 服务层和 HTTP handler 已经支持用审批实例、业务记录、结果类型、定义指纹和依赖健康来过滤已安装 MaClaw App；但 OpenAPI 只文档化了主参数，缺少许多 handler 实际支持的别名。这样 GUI MIS Tool、agent、Hub 或企业集成方按 OpenAPI 生成客户端时，会看不到 `definition_hash`、`record_approval_id`、`approval_instance_id`、`output_type` 等常用字段。

已落地调整：

```text
/api/v1/data/app-installations OpenAPI query params
  -> approval_result_status 作为 approval_status 别名
  -> decision 作为 approval_decision 别名
  -> submitted_by / created_by 作为 applicant_id 别名
  -> assigned_to / current_assignee 作为 approver_id 别名
  -> record_approval_id 作为 approval_id 别名
  -> approval_instance_id / instance_id 作为 workflow_instance_id 别名
  -> dataset 作为 dataset_id 别名
  -> object 作为 object_role 别名
  -> business_record_id 作为 record_id 别名
  -> output_type 作为 result_type 别名
  -> definition_hash / app_definition_hash / app_definition_fingerprint 作为 definition_fingerprint 别名
  -> has_missing_required 作为 has_missing_required_dependency 别名
```

同时新增 HTTP 功能断言，确认这些别名不是只出现在文档里，而是能实际查询到同一条审批型 App 安装记录。

对应全链路意义：

```text
审批 workflow / DataSrv RecordApproval / App Studio 测试证据
  -> 写入 app_installations.metadata
  -> 外部客户端可按实例 ID、远端 approval ID、业务记录 ID、结果类型、定义 hash 查询
  -> GUI、agent、Hub 审核和企业集成共享同一套查询合同
```

已通过定向验证：
```text
go test ./structureddata -count=1 -timeout 60s -run "TestHTTPServerRequiresBearerTokenAndHandlesRecords"
```

### 推进记录：GUI MIS Tool 对齐 app-installations 实例级过滤别名（2026-06-27）
本轮继续把上一段 DataSrv HTTP/OpenAPI 的实例级过滤合同推进到 GUI/agent 运行诊断工具。`list_app_installations` 已经支持多数 App 安装证据过滤字段，但和 DataSrv 新公开的别名仍有小缺口：agent 用 `created_by`、`current_assignee`、`dataset`、`object` 等自然字段发起查询时，工具没有统一归一化到 DataSrv 的 canonical query 参数。

已落地调整：

```text
GUI MIS Tool list_app_installations
  -> created_by / createdBy 映射到 applicant_id
  -> current_assignee / currentAssignee 映射到 approver_id
  -> dataset 映射到 dataset_id
  -> object 映射到 object_role
  -> 继续兼容 approval_result_status / decision / record_approval_id / approval_instance_id / business_record_id / output_type / app_definition_hash / has_missing_required
  -> datasrv-installed-* 本地包装 App ID 继续规范化为真实 DataSrv app_id
```

对应全链路意义：

```text
GUI / agent / MIS Tool
  -> 用审批实例、远端审批 ID、业务记录 ID、对象角色、定义 hash 查询 DataSrv app_installations
  -> 找到对应 MaClaw App 安装证据、依赖健康、测试证据和审批结果包
  -> 运行诊断、发布复核、审批工作台跳转使用同一套查询口径
```

已通过定向验证：
```text
go test ./gui -count=1 -vet=off -run "TestExecuteMISDataTool(ListAppInstallationsPassesDependencyFilters|GetAppInstallation)"
```

### 推进记录：DataSrv 安装证据依赖状态按当前 App 收敛（2026-06-27）

本轮继续检查“安装 MaClaw App -> 写本地安装审计 -> 注册 DataSrv app_installations -> GUI 从 DataSrv 恢复候选 App -> 运行/发布复用安装证据”的链路。此前市场安装、运行态修复、发布门禁和后端提交审核已经按 selected app / current app 过滤依赖状态，但 DataSrv 安装注册和恢复仍需要明确同一规则：任何包级依赖阻断都不能污染当前 App 的安装证据。

已落地调整：

```text
GUI 后端 DataSrv 注册测试
  -> 新增多 App 包场景：
     selected-app 依赖 ready
     other-app 依赖 blocked
  -> maclawAppDataSrvInstallationPayloads 生成 selected-app metadata 时：
     dependency_verification.dependencies 只含 selected-skill
     dependency_count = 1
     has_missing_required = false
     has_blocking_dependency = false
  -> 同时验证 market-selected-app 这类包装 App ID 能匹配回 canonical selected-app

GUI 前端 DataSrv 恢复
  -> dataSrvInstalledInstallEvidence 恢复 installEvidence 时优先使用结构化 dependency_verification 的布尔状态
  -> 仅当 dependency_verification 缺失时才回退 metadata.has_missing_required_dependency / has_blocking_dependency
  -> 旧 DataSrv metadata 中残留的包级 blocking=true 不再覆盖当前 App 已验证 ready 的结构化证据

回归覆盖
  -> DataSrv installed MaClaw App 候选导入测试加入“顶层 blocking=true、dependency_verification=false”的旧数据场景
  -> 导入后的 installEvidence.has_blocking_dependency / has_missing_required 仍为 false
```

对应全链路意义：

```text
Hub/市场多 App 包
  -> 安装 selected app
  -> 后端写本地安装记录和 DataSrv app_installations
  -> GUI 后续从 DataSrv capabilities 恢复
  -> 运行前依赖检查、发布复核、治理证据继续按当前 App 判断
  -> 不再因为同包其它 App 的缺失依赖误阻断当前 App
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestMaclawAppDataSrvInstallationPayloadsScopeDependenciesPerApp"
npm.cmd test -- AppsPage.test.tsx -t "turns DataSrv installed MaClaw apps into addable app candidates with layout metadata"
```

### 推进记录：多 App 包共享 Skill 依赖按语义拆分（2026-06-27）

本轮继续从“能力市场包安装 -> 依赖检查 -> DataSrv/GUI 恢复 -> 运行/发布复核”的全链路检查依赖归属。发现后端安装计划此前按 Skill ID 合并依赖；当同一个 Skill 在不同 App 中语义不同，例如：

```text
App A: shared-skill required=true
App B: shared-skill required=false

或：

App A: workflow-skill version=1.0.0
App B: workflow-skill version=2.0.0
```

如果只按 ID 合并，就会把 A 的 required / version 覆盖到 B，导致用户只安装或运行 B 时被 A 的必需依赖误阻断，或丢失版本约束。这和 MaClaw App 作为“超级 Skill + 依赖 Skill”的能力市场分发模型不一致。

已落地调整：

```text
GUI 后端 PlanMaclawAppInstall
  -> 依赖合并键从 id 改为：
     id + version + kind + source + required
  -> 只有同一语义的依赖才合并 app_ids
  -> 同一 Skill 在不同 App 中 required/optional 不再互相污染
  -> 同一 Skill 在不同 App 中版本不同也不再丢失约束

依赖证据过滤
  -> hasBlockingMaclawAppRequiredDependencyForApp 继续按 app_id 过滤
  -> optional-app 不会被 required-app 的 shared-skill 阻断

GUI 前端 identity alias
  -> appInstallIdentityKeys 增加 datasrv-installed-* 双向别名
  -> DataSrv 恢复应用、本地包装 ID、canonical app_id 的依赖证据过滤口径保持一致
```

对应全链路意义：

```text
maclaw.app.pack.v1 多应用包
  -> 用户选择其中一个 App
  -> 后端安装计划保留该 App 自己的 required/version/source 语义
  -> 市场安装、DataSrv 注册、运行前健康检查、发布门禁都按当前 App 判断
  -> 多 App 包共享 Skill 不再造成跨 App 误阻断
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestPlanMaclawAppInstallScopesSharedDependencyRequirementPerApp"
npm.cmd test -- AppsPage.test.tsx -t "turns DataSrv installed MaClaw apps into addable app candidates with layout metadata"
```

### 推进记录：DataSrv 依赖健康过滤优先使用结构化验证证据（2026-06-27）

本轮继续把 GUI 与 DataSrv 的安装证据口径对齐。前面 GUI 从 `app_installations.metadata` 恢复安装证据时，已经改为优先读取结构化 `dependency_verification`，再回退旧的扁平 metadata 字段；但 DataSrv 服务端 `ListAppInstallations` 的依赖健康过滤仍先读取 `has_blocking_dependency` / `has_missing_required_dependency` 顶层字段。

这会造成一个旧数据兼容问题：

```text
metadata.has_blocking_dependency = true          # 旧包级字段，可能来自多 App 包汇总
metadata.has_missing_required_dependency = true  # 旧包级字段，可能来自多 App 包汇总
metadata.dependency_verification.has_blocking_dependency = false
metadata.dependency_verification.has_missing_required = false
```

如果 DataSrv 按顶层字段优先判断，`/api/v1/data/app-installations?has_blocking_dependency=false` 会漏掉当前 App 已验证 ready 的安装记录；GUI、MIS Tool 和运维 agent 查询“可运行 App”时会被旧包级字段误导。

已落地调整：

```text
DataSrv ListAppInstallations
  -> HasBlockingDependency 过滤优先读取：
     dependency_verification.has_blocking_dependency
     dependency_verification.hasBlockingDependency
     test_evidence.dependency_verification.*
     test_evidence_dependency_blocking
     顶层 has_blocking_dependency

  -> HasMissingRequiredDependency 过滤优先读取：
     dependency_verification.has_missing_required
     dependency_verification.hasMissingRequired
     test_evidence.dependency_verification.*
     test_evidence_dependency_missing_required
     顶层 has_missing_required_dependency / has_missing_required

DataSrv 回归测试
  -> 新增 legacy_ready 安装记录：
     顶层字段为 blocking=true
     结构化 dependency_verification 为 blocking=false
  -> 查询 has_blocking_dependency=true 只返回真正 blocked App
  -> 查询 has_blocking_dependency=false 返回 ready 与 legacy_ready
```

对应全链路意义：

```text
GUI 安装 / Hub 安装 / DataSrv 注册
  -> 结构化 dependency_verification 成为权威 App 级证据
  -> 旧扁平字段只作为兼容兜底
  -> MIS Tool、App Studio、agent 通过 DataSrv 查询依赖健康时，不再被多 App 包旧汇总状态误阻断
```

已通过定向验证：

```text
go test ./structureddata -count=1 -timeout 60s -run "TestListAppInstallationsFiltersByDependencyHealth"
```

### 推进记录：MIS Tool 查询 DataSrv 安装证据兼容本地包装 App ID（2026-06-27）

本轮继续检查 “GUI 工作台 / agent / MIS Tool -> DataSrv app_installations -> 安装证据诊断” 这条链路。DataSrv 安装应用进入 GUI 面板后，本地 `AppEntry.id` 会使用 `datasrv-installed-{app_id}` 避免和本地/市场 App 冲突；但 DataSrv 服务端保存和查询的真实 `app_id` 仍是 canonical ID。

如果 agent 或工具调用直接拿 GUI 面板里的本地 ID 调用：

```text
list_app_installations(app_id="datasrv-installed-expense.approval")
get_app_installation(app_id="datasrv-installed-expense.approval")
```

此前请求会原样发给 DataSrv，导致 `/api/v1/data/app-installations/{id}` 查不到真实安装记录。这样会影响运行态诊断、发布前证据查询、依赖健康定位和审批实例证据追踪。

已落地调整：

```text
GUI MIS Data Tool
  -> list_app_installations 的 app_id/appId 参数进入 query 前解包 datasrv-installed-* 前缀
  -> get_app_installation 的 app_id/appId/id 进入 path 前解包 datasrv-installed-* 前缀
  -> market-* 不在这里强行解包，避免误改真实市场 app_id

回归测试
  -> list_app_installations 输入 datasrv-installed-expense.blocked
     请求 query app_id=expense.blocked
  -> get_app_installation 输入 datasrv-installed-expense.approval
     请求 path /api/v1/data/app-installations/expense.approval
```

对应全链路意义：

```text
DataSrv capabilities 恢复 App
  -> GUI 面板使用 datasrv-installed-* 本地 ID
  -> agent / MIS Tool 用同一个本地 ID 查询安装证据
  -> 工具层自动转成 DataSrv canonical app_id
  -> 依赖健康、测试证据、审批实例、结果文件/内容证据可以被稳定定位
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestExecuteMISDataTool(ListAppInstallationsPassesDependencyFilters|GetAppInstallation)"
```

### 推进记录：Skill 包保存保留 App Studio 完整动态布局元数据（2026-06-27）

本轮从 App Studio 制作链路继续检查“用户可视化调整界面 -> 保存到应用信息文件 -> 测试/上传/安装恢复”的契约。前端已经能在 `manifest.ui.layouts` 中保存布局区域，但后端 `SaveMaclawAppDefinitionForSkill` 是最终写入 Skill 包 `maclaw.app.json` 的入口；如果这里只保留 entry/template/density 等摘要，用户在 App Studio 中调整的传统软件式工作台布局会在进入能力市场前丢失。

已补强回归覆盖：

```text
企业审批型 App 保存到 skill 包
  -> binding.ui.layouts.approval_workspace.regions 原样保留
  -> primaryRegion / outputRegion 原样保留
  -> navigation: my_requests / pending_my_approval / attention 原样保留
  -> list.columns: title / applicant / current_node / status 原样保留
  -> studio: generated / lastEditedBy 原样保留
  -> region 级 locked / width 等用户调整元数据原样保留
```

对应全链路意义：

```text
App Studio 可视化设计
  -> 用户调整区域位置、导航、列表列、区域属性
  -> SaveMaclawAppDefinitionForSkill 写入 maclaw.app.json
  -> Hub 上传/安装包下载/DataSrv 安装注册继续拿到完整 UI 契约
  -> MaClaw App 界面保持“传统软件式动态工作台”，不退化成只有表单字段的 manifest
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestSaveMaclawAppDefinitionForSkillWritesEnterpriseAppFile"
```

### 推进记录：DataSrv 顶层 dependencyVerification 规范化并保留依赖明细（2026-06-27）

本轮继续检查 DataSrv `app_installations.metadata` 作为安装后证据源的稳定性。GUI 安装注册和 Hub/市场回灌可能把依赖验证写在顶层：

```text
metadata.dependencyVerification / metadata.dependency_verification
```

此前 DataSrv 会原样保存该对象，但不会规范化 camelCase/snake_case，也不会同步生成 `test_evidence_dependency_*` 摘要。这样会让 capabilities 回流、MIS Tool 查询和运维诊断出现口径不一致：GUI 可以看到原始对象，但 DataSrv 的摘要字段、过滤字段和审计/诊断显示不一定完整。

已落地调整：

```text
DataSrv metadata normalize
  -> 顶层 dependencyVerification 规范化为 dependency_verification
  -> schema 固定为 maclaw.app.install_plan.v1
  -> verifiedAt -> verified_at
  -> dependencyCount -> dependency_count
  -> hasMissingRequired -> has_missing_required
  -> hasBlockingDependency -> has_blocking_dependency
  -> 保留 dependencies 明细数组
  -> 同步生成 test_evidence_dependency_verified_at / count / blocking / missing_required 等摘要
  -> capabilities 返回同一份规范化后的 dependency_verification
```

对应全链路意义：

```text
MaClaw App 安装注册 DataSrv
  -> dependencyVerification 进入 app_installations.metadata
  -> DataSrv 规范化为稳定契约
  -> GUI/DataSrv capabilities/MIS Tool/agent 诊断拿到同一份依赖健康证据
  -> 后续依赖修复、发布复核、安装审计可以看到具体缺失或 ready 的 Skill 明细
```

已通过定向验证：

```text
go test ./structureddata -count=1 -timeout 60s -run "TestUpsertAppInstallationNormalizesTopLevelDependencyVerification"
```

### 推进记录：市场包安装依赖验证按选中 App 收敛（2026-06-27）

本轮继续检查“能力市场安装 -> 依赖检查 -> 安装记录 -> 本地 AppEntry”链路。此前粘贴 `maclaw.app.pack.v1` 包时，安装动作已经会按用户当前选中的 App 子包重新调用 `PlanMaclawAppInstall` / `InstallMaclawAppDependencies`，安装审计也只记录实际安装的 App；但依赖验证面板仍会读取后端计划的包级 `has_missing_required` / `has_blocking_dependency` 汇总标记。

这会造成一个误导场景：

```text
包内有 A、B 两个 App
  -> A 的依赖缺失
  -> B 的依赖 ready
用户取消选择 A，只安装 B
  -> 安装动作可以继续
  -> 但依赖验证面板仍显示整个包阻塞
```

已落地调整：

```text
DependencyVerificationPanel
  -> 如果传入 selectedAppIDs，只按选中 App 过滤 dependencies、workflow issues、governance issues
  -> 包级 has_missing_required / has_blocking_dependency 只在没有选择范围时作为整体计划状态使用

市场高级导入
  -> 预览、安装结果和单 App 安装反馈继续传入 selectedAppIDs
  -> 未选中 App 的缺失依赖不再污染已选 App 的验证状态
```

对应全链路意义：

```text
能力市场大包
  -> 用户选择部分 App 安装
  -> 依赖检查和 UI 反馈都绑定到实际安装范围
  -> RecordMaclawAppInstall 只记录实际安装子包
  -> 后续安装证据、DataSrv 注册、运行诊断不会混入未安装 App 的依赖状态
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "installs dependencies and records audit only for selected apps in a pasted pack|scopes pasted pack dependency verification to selected apps"
```

### 推进记录：运行态依赖修复结果进入运行证据（2026-06-27）

本轮继续检查“安装完成后 -> 运行前依赖检查 -> 缺失依赖修复 -> Skill 实际运行 -> run history / 发布证据”的链路。运行界面已经提供“安装依赖并运行”入口，能够在依赖缺失时调用 `InstallMaclawAppDependencies` 后继续执行 App；但修复成功后前端会清空 `runtimeDependencyPlan` 并直接跳过二次依赖检查运行。

这会造成一个证据断点：

```text
运行前发现依赖缺失
  -> 用户点击安装依赖并运行
  -> 后端返回已修复的 install plan
  -> App 成功运行
  -> run history 可能没有记录本次修复后的 dependencyVerification
```

已落地调整：

```text
AppPreview runtime
  -> 新增当前运行级 activeRunDependencyPlanRef
  -> 运行开始时记录本次运行所依据的依赖计划
  -> PlanMaclawAppInstall 成功后写入 ref
  -> InstallMaclawAppDependencies 修复成功后保留修复后的 plan，并标记 runtime dependency ready
  -> Skill run 完成写 run history 时，优先使用当前运行级 plan 生成 dependencyVerification

appRunDependencyVerificationEvidence
  -> dependencyCount / dependencies / hasBlockingDependency 按当前 App app_id 过滤
  -> 不再把同一个包里其它 App 的阻塞依赖写入当前 App 的运行证据
```

对应全链路意义：

```text
安装后的 MaClaw App
  -> 运行前发现依赖问题
  -> 用户在传统软件式运行界面直接修复
  -> 修复结果成为本次运行证据
  -> 发布复核、Hub 审核、DataSrv 回流可以看到运行当时依赖已经 ready
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "installs missing runtime dependencies and continues the installed app run"
```

### 推进记录：发布门禁和治理依赖证据按当前 App 过滤（2026-06-27）

本轮继续把依赖验证的“按实际 App 范围归属”推进到上传 Hub / 能力市场发布环节。此前市场安装预览、安装结果、运行态修复已经按 selected app / current app 过滤依赖状态，但发布复核仍有两个包级口径残留：

```text
PublishPane submitApp
  -> PlanMaclawAppInstall 返回的 has_blocking_dependency 是计划级汇总
  -> 即使阻断依赖属于其它 App，也可能阻止当前 App 提交

appGovernanceForManifest.dependencyVerification
  -> 直接写入 dependencyPlan.dependencies 和 has_blocking_dependency
  -> 当前 App 的治理证据可能混入其它 App 的阻断依赖
```

已落地调整：

```text
发布门禁
  -> 使用 runtimeInstallPlanBlocked(dependencyPlan, app)
  -> 依赖缺失、治理 review issue、workflow contract issue 都按当前 canonical app id 判断

治理依赖验证证据
  -> 复用 appRunDependencyVerificationEvidence
  -> dependencies / dependencyCount / hasBlockingDependency / appCount 均按当前 App id 过滤
  -> 不再把同一计划中其它 App 的 blocked skill 写入当前 App 的治理证据
```

对应全链路意义：

```text
App Studio 测试完成
  -> 发布前重新做后端依赖计划
  -> 当前 App 通过即可提交
  -> 提交包 governance.dependencyVerification 只描述当前 App
  -> Hub 审核、DataSrv 安装注册、GUI 回流不会继承未发布 App 的依赖阻断状态
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "includes enterprise visual UI metadata in market submission packages"
```

### 推进记录：后端提交审核按当前 App 过滤 dependencyVerification（2026-06-27）

本轮继续把上一段“发布门禁和治理依赖证据按当前 App 过滤”推进到 GUI 后端 `SubmitMaclawAppPackage`。前端已经保证提交包中的 `governance.dependencyVerification` 只描述当前 App，但后端仍可能收到其它客户端或旧版本前端提交的混合证据：

```text
dependencyVerification.hasBlockingDependency = true
dependencies:
  - 当前 App 的依赖：ready
  - 其它 App 的依赖：blocked
governanceReviewIssues:
  - apps[1] 的 review issue
```

此前后端只要看到 `hasBlockingDependency / hasMissingRequired / hasGovernanceReviewIssue` 为 true，就会给当前 App 生成 review issue，没有继续判断这些阻断是否属于当前 `apps[i]`。这会让 Hub/本地队列审核重新引入“包级状态污染单 App”的问题。

已落地调整：

```text
maclawAppDependencyVerificationReviewIssue
  -> hasBlockingDependency / hasMissingRequired 不再直接作为当前 App 阻断
  -> 有 dependencies 明细时，按 dependency.app_ids / appIDs 过滤当前 entry.ID
  -> 只有当前 App 匹配的依赖 blocked，才生成 dependencyVerification error
  -> 没有 dependencies 明细但布尔标记阻断时，继续按保守策略拒绝

maclawAppDependencyVerificationIssueFromEvidence
  -> workflowContractIssues / governanceReviewIssues 按当前 appPath 过滤
  -> apps[其它索引] 的 issue 不再污染当前 App

app id 匹配
  -> 兼容原始 id、market-* 包装 id、datasrv-installed-* 包装 id
```

对应全链路意义：

```text
App Studio / 旧客户端 / 自动化工具提交 MaClaw App 包
  -> GUI 后端重新做提交审核
  -> 当前 App 的依赖和治理状态按 app_id 归属判断
  -> 本地提交队列、Hub 上传前审核、后续安装记录不再继承未提交 App 的 blocked 状态
```

新增后端回归测试：

```text
TestSubmitMaclawAppPackageScopesDependencyVerificationToCurrentApp
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestSubmitMaclawAppPackageScopesDependencyVerificationToCurrentApp"
```

### 推进记录：后端安装/提交证据依赖过滤兼容包装 App ID（2026-06-27）

本轮继续检查后端 `submission_evidence`、安装记录 evidence、DataSrv metadata 里按 App 拆分依赖状态的底层 helper。前端和运行态已经区分：

```text
本地面板 id: market-* / datasrv-installed-*
MaClaw App 契约 id: canonical app id
DataSrv app_id: 真实业务 App id
```

此前后端 per-app helper 仍使用精确字符串匹配：

```text
cloneMaclawAppPlanDependenciesForApp
hasMissingMaclawAppRequiredDependencyForApp
hasBlockingMaclawAppRequiredDependencyForApp
```

如果安装计划中的依赖使用 `market-xxx` 或 `datasrv-installed-xxx`，而安装证据/提交证据按 canonical `xxx` 拆分，就可能漏掉当前 App 的依赖；反过来，也可能让后续运行诊断、DataSrv 回流、提交队列摘要口径不一致。

已落地调整：

```text
maclawAppPlanDependencyMatchesAppID
  -> 统一通过 maclawAppIDsMatch 判断依赖 app_ids 是否属于当前 app
  -> 兼容 canonical id、market-*、datasrv-installed-* 三种身份

影响范围
  -> install_evidence.dependencies
  -> install_evidence.has_missing_required / has_blocking_dependency
  -> dependency_verification.dependencies
  -> DataSrv app_installations metadata dependency summary
  -> submission_evidence per-app dependency snapshot
```

对应全链路意义：

```text
Hub / DataSrv / GUI 任一路径产生的 App ID 包装形式
  -> 后端安装记录和提交证据都能归并到同一个 MaClaw App
  -> 审批型应用和普通企业应用的依赖诊断不会因本地包装 ID 丢失
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "Test(MaclawAppPlanDependencyMatchesWrappedAppIDs|SubmitMaclawAppPackageScopesDependencyVerificationToCurrentApp|SubmitMaclawAppPackageAcceptsCompleteGovernanceEvidence)"
```

### 推进记录：DataSrv 安装审批应用统一审批实例 canonical app id（2026-06-27）
本轮继续从“maclaw data srv 到 maclaw app 的全链路”检查审批运行态，发现 DataSrv 安装进 GUI 面板的应用存在双重身份：

```text
面板本地 id: datasrv-installed-mis.expense
DataSrv / manifest canonical id: mis.expense
```

此前运行态依赖计划已经使用 canonical app id，但审批实例查询、记录、审批决策更新、同步 DataSrv payload 仍有部分路径使用本地面板 id。这样会导致审批型应用的“我的申请 / 我审批的 / 当前节点 / 已处理 / 需关注”在 DataSrv 安装应用上查不到真实实例，或者把同一个审批实例分裂成两套 app_id。

已落地调整：

```text
AppPreview 审批实例查询
  -> ListMaclawAppApprovalInstances(canonicalAppManifestID(app), lane, 50)

审批 workflow skill 运行 payload
  -> app_id 使用 manifest.datasrv.appID / canonical id

审批实例初始记录
  -> BackendApprovalInstance.app_id 使用 canonical id

workflow 完成 / 失败 / 取消后的审批实例回写
  -> app_id 使用 canonical id，纠正旧上下文中的本地 id

审批决策更新
  -> app_id 使用 canonical id

DataSrv sync payload
  -> 外层 app_id 与内层 instance.app_id 都使用 canonical id
```

对应全链路意义：

```text
DataSrv app-installations / manifest app.id
  -> GUI DataSrv installed app candidate
  -> AppPreview 审批实例查询
  -> 发起审批 workflow skill
  -> 本地审批实例缓存
  -> DataSrv RecordApproval / business record sync
  -> 我的申请 / 我审批的 / 当前节点 / 结果反馈
```

这一步把 DataSrv 安装审批应用的运行态身份统一起来，后续继续推进时可以在此基础上补“发起节点动态 UI -> 审批 workflow skill 输入契约 -> 审批实例结果/文件回填”的更深链路。

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "turns DataSrv installed MaClaw apps into addable app candidates with layout metadata"
npm.cmd run build
```

### 推进记录：审批 workflow 嵌套结果包回填审批实例（2026-06-27）
本轮继续补“审批 workflow skill 运行 -> 审批实例数据管理 -> 结果反馈”的后半段。此前 GUI 已能从 skill run 的顶层 summary、outputs、artifacts 中读取审批结果，但企业审批 workflow 更自然的输出往往是结构化结果包，例如：

```json
{
  "approval_result": "approved",
  "approval_instance": {
    "record_id": "exp-1",
    "current_node": "expense.result_feedback",
    "business_status": "pending_payment"
  },
  "outputs": [
    { "kind": "notification", "title": "Nested notice" }
  ],
  "artifacts": [
    { "id": "artifact-nested", "name": "nested-result.zip" }
  ]
}
```

此前这类 JSON 包如果放在 output text / summary snippet / result_payload 中，解析层只看第一层对象，容易漏掉：

```text
approval_instance.current_node
approval_instance.record_id
嵌套 outputs
嵌套 artifacts
record_ref / business_payload 中的业务引用
```

已落地调整：

```text
expandSkillRunApprovalObjects(...)
  -> 展开 result_payload / approval_instance / approvalInstance / approval
  -> 展开 record_ref / recordRef
  -> 展开 business_payload / businessPayload

approvalWorkflowResultPayloadFromObjects(...)
  -> 对展开后的对象做结果 payload 合并

approvalWorkflowOutputsFromStatus(...)
  -> 合并 skill run output blocks 与 JSON 结果包中的 outputs

approvalWorkflowArtifactsFromStatus(...)
  -> 合并 status artifacts / output block artifact / JSON 结果包中的 artifacts

approvalWorkflowResultFromSkillRunStatus(...)
  -> current_node / record_id / business_status / result_status 可来自嵌套 approval_instance 或 record_ref
```

对应全链路意义：

```text
workflow skill 结构化输出
  -> GUI 解析 approval_instance / outputs / artifacts
  -> BackendApprovalInstance.result_payload / outputs / artifacts / current_node / record_id
  -> DataSrv RecordApproval sync payload
  -> 审批实例详情、全局审批管理、运行历史 evidence
  -> 用户看到状态、内容、文件结果
```

这一步让“应用输出结果 = 审批结果 + 文档/文件 + 显示内容 + 业务状态/记录/通知”的反馈面更完整，workflow skill 可以按结构化契约返回结果，而不必把所有字段拍平。

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "records and completes an approval instance when running an approval app"
npm.cmd test -- AppsPage.test.tsx -t "turns DataSrv installed MaClaw apps into addable app candidates with layout metadata"
npm.cmd run build
```

### 推进记录：App Studio 编辑后失效旧安装/测试证据（2026-06-27）
本轮继续检查“应用程序设计时自动生成界面，用户调节位置，在应用信息文件中保存界面布局信息；测试后上传 Hub 能力市场”的证据链。发现一个编辑态风险：

```text
用户在 App Studio 管理页编辑应用布局、依赖、workflow 或结果契约
  -> manifest 已更新，version 已递增
  -> 但 AppEntry 上的 importedRunEvidence / versionSnapshot / installEvidence / workflowContract 旧快照仍可能保留
  -> 发布审查、运行态依赖面板或 workflow contract 摘要可能继续看到旧安装/测试证据
```

同时，workflow contract 读取此前优先使用 `app.workflowContract` 安装快照，再回退到 manifest 推导。这样 DataSrv/Hub 安装应用被用户二次编辑后，旧 workflow contract 可能覆盖新 manifest。

已落地调整：

```text
workflowContractForApp(app)
  -> 改为优先 appWorkflowContractForManifest(app)
  -> 只有 manifest 无法推导时才回退 app.workflowContract

ManageAppsPane.saveEdit(...)
  -> 保存编辑时继续写入新 manifest 与递增 version
  -> 同时清理 importedRunEvidence
  -> 清理 versionSnapshot
  -> 清理 installEvidence
  -> 清理 workflowContract
```

对应全链路意义：

```text
App Studio 可视化编辑布局/依赖/契约
  -> manifest.definitionHash 改变
  -> 旧测试证据、旧安装依赖检查、旧 workflow contract 快照自动失效
  -> 用户需要重新测试当前定义
  -> 发布/上传 Hub 时使用当前 manifest 与当前证据
  -> 安装 maclaw app 时依赖检查不会被旧 evidence 误导
```

这一步让“设计 -> 保存 -> 测试 -> 上传 -> 安装 -> 运行”的证据边界更清楚：只要用户改变 App 定义，旧安装/测试快照就不再被当作当前版本的证明。

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "invalidates installed evidence snapshots when visual layout edits change the app definition"
npm.cmd test -- AppsPage.test.tsx -t "edits app studio layout visually and persists it|invalidates installed evidence snapshots when visual layout edits change the app definition|turns DataSrv installed MaClaw apps into addable app candidates with layout metadata"
npm.cmd run build
```

### 推进记录：审批 workflow skill 发起输入补齐契约载荷（2026-06-27）
本轮继续沿着“发起节点 UI -> 审批 workflow skill 运行 -> 审批实例数据管理”的链路推进。上一轮已经把 DataSrv 安装审批应用的 app_id 统一为 canonical id；继续检查后发现，DataSrv 安装记录中的 workflow contract 可以声明：

```text
required_inputs:
  - record_ref
  - applicant
  - business_payload
```

但 GUI 发起审批 workflow skill 时，主要传递的是扁平字段，例如 app_id、business_entity、business_action、dataset_id、object_role 等。这样旧 skill 能跑，但新设计里的企业审批型应用缺少稳定的 workflow 输入契约，workflow skill 需要自己从散字段中猜 record、申请人、业务载荷。

已落地调整：

```text
approvalWorkflowContractInputPayload(app, ...)
  -> 生成 record_ref
     app_id / app_name / instance_id / record_id / dataset_id / object_role / blueprint_id / title
  -> 生成 applicant
     id / name / display_name / type
  -> 生成 business_payload
     app_id / app_name / approval_instance_id / record_ref / applicant
     business_entity / business_action / business_note
     dataset_id / object_role / blueprint_id / datasrv_domain / preferred_action / preferred_view / submitted_at
  -> 透传 workflow_contract
  -> 透传 workflow_required_inputs

RunNLSkillAsync(workflowSkillID, payload)
  -> 保留原有扁平字段，兼容已有 workflow skill
  -> 同时新增 record_ref / applicant / business_payload / workflow_contract

初始 BackendApprovalInstance
  -> 显式写入 record_id=fallbackID
  -> DataSrv sync 可以直接使用 record_id，而不是只依赖 instance_id 兜底
```

对应全链路意义：

```text
App Studio / DataSrv manifest workflow_contract
  -> GUI 发起节点动态 UI 采集业务输入
  -> workflow skill 获得稳定契约输入
  -> workflow skill 决策输出 approval_result / business_status / artifact / notification
  -> 审批实例记录与 DataSrv RecordApproval 关联
  -> 结果反馈可按 record_ref 和 approval_instance_id 回填
```

这一步让“企业审批型应用 = 数据录入 + 审批 workflow skill 运行 + 审批实例数据管理 + 结果反馈”的中间启动载荷更接近最终设计，不再只是把 GUI 表单字段松散地丢给 skill。

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "turns DataSrv installed MaClaw apps into addable app candidates with layout metadata"
npm.cmd run build
```

### 推进记录：App Studio 动态布局与安装证据支持冷启动恢复（2026-06-27）
本轮继续从“完整全流程”角度检查 MaClaw App 安装后的长期可用性。此前安装结果页、运行态、发布复核已经可以读取 `AppEntry.installEvidence`，但本地应用面板从 `localStorage` 恢复时，安装证据、版本快照、审批工作流合同主要依赖对象展开被动保留，缺少显式恢复契约和冷启动回归测试。对于“所有 MaClaw App 界面动态生成、用户调整位置后保存到应用信息文件”的目标来说，冷启动恢复是必须成立的基础链路。

已落地调整：

```text
normalizeStoredAppEntry
  -> 显式归一化 kind，避免重复推导
  -> 显式恢复 versionSnapshot
  -> 显式恢复 installEvidence
  -> 显式恢复 workflowContract

冷启动回归测试
  -> localStorage 中预置带 manifest.ui 的 MaClaw App
  -> manifest.ui 保存 App Studio 调整后的 template / density / regions
  -> installEvidence 保存 dependency_verification / workspace_layout / test_evidence
  -> 页面重新挂载后打开 App
  -> 运行态仍显示依赖验证、布局证据、测试证据
```

对应全链路意义：

```text
App Studio 设计动态界面
  -> 用户调整位置/区域/密度
  -> 保存到 app manifest.ui
  -> 测试并生成 installEvidence
  -> 上传 Hub 能力市场
  -> 安装后写入本地应用面板
  -> 关闭/重启 GUI
  -> 从本地状态恢复 manifest.ui + installEvidence
  -> 运行态/发布复核/依赖诊断继续使用同一份证据
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "restores dynamic layout and install evidence from stored app entries after cold start|keeps dependency verification visible after single market app install"
```

### 推进记录：DataSrv app-installations 支持按 App 定义指纹过滤证据（2026-06-27）
本轮继续补齐 App Studio 测试、上传、安装、运行诊断之间的证据定位能力。此前 MaClaw App 发布侧已经要求测试证据的 `definitionHash` 必须匹配当前应用定义，DataSrv 也会保存 `test_evidence_definition_fingerprint`；但 `/api/v1/data/app-installations` 还不能直接按该定义指纹过滤，GUI MIS Data Tool 也无法把当前定义 hash 传给 DataSrv 精确查证据。

已落地调整：

```text
QueryAppInstallationsInput
  -> 新增 DefinitionFingerprint

GET /api/v1/data/app-installations
  -> 支持 definition_fingerprint
  -> 支持 definition_hash
  -> 支持 app_definition_hash
  -> 支持 app_definition_fingerprint

SQLite ListAppInstallations metadata filter
  -> 匹配 test_evidence_definition_fingerprint
  -> 匹配 test_evidence.definitionHash / definition_hash / definition_fingerprint
  -> 匹配 governance.test_evidence 等嵌套证据路径

OpenAPI
  -> 公开 definition_fingerprint 查询参数

GUI MIS Data Tool
  -> list_app_installations 支持 definitionHash / definition_hash / definitionFingerprint / appDefinitionHash 等别名
  -> agent 可直接按当前 app 定义 hash 查安装证据
```

对应全链路意义：

```text
App Studio 保存当前定义
  -> 计算 appDefinitionFingerprint / definitionHash
  -> 运行测试并生成测试证据
  -> 上传/安装后 DataSrv 保存 test_evidence_definition_fingerprint
  -> 运行态或诊断代理用当前 definitionHash 查询 app-installations
  -> 精确确认安装证据、测试证据、依赖验证是否属于当前定义
  -> 避免旧版本测试证据被误当成当前 App 的有效证据
```

已通过定向验证：

```text
go test ./structureddata -count=1 -timeout 60s -run "TestUpsertAppInstallationSynthesizesTestEvidenceFromSummaryMetadata|TestHTTPServerAppInstallationsOverrideObjectRoleBindings|TestHTTPServerOpenAPIIncludesExpectedQueryParameters"
```

补充复验：

```text
go test ./gui -count=1 -vet=off -run "TestExecuteMISDataToolListAppInstallationsPassesDependencyFilters|TestExecuteMISDataToolGetAppInstallation"
```

### 推进记录：DataSrv 已安装 App 候选恢复完整安装证据（2026-06-27）
本轮继续把 DataSrv 到 MaClaw App GUI 的证据链补齐。此前 DataSrv `/api/v1/data/capabilities` 返回的 `app_installations` 已经能恢复为 App Studio 中可添加的 App 候选，并能恢复 `importedRunEvidence`、`versionSnapshot`、`workflowContract`、动态布局和测试协议；但它没有同步合成为 `AppEntry.installEvidence`。这会导致用户从 DataSrv capabilities 发现并添加已安装 App 后，运行态、发布复核和依赖诊断缺少与市场安装路径同构的完整安装证据快照。

已落地调整：

```text
DataSrv installed app candidate
  -> 从 metadata.version_snapshot / app_skill / workflow_skill_versions 恢复 versionSnapshot
  -> 从 metadata.workspace_layout 或 workspace_layout_* summary 恢复 workspace_layout
  -> 从 metadata.result_contract 恢复 result_contract
  -> 从 metadata.workflow_contract 恢复 workflow_contract
  -> 从 metadata.test_evidence 或 test_evidence_* summary 恢复 test_evidence
  -> 从 metadata.dependency_verification 或 test evidence dependency summary 恢复 dependency_verification
  -> 合成为 AppEntry.installEvidence

AppEntry 本地保存
  -> DataSrv 已安装 App 加到面板后，customApps 中保留 installEvidence
  -> 后续冷启动恢复、运行态依赖面板、发布复核可复用同一份证据结构
```

对应全链路意义：

```text
DataSrv 注册 app-installations
  -> capabilities 返回已安装 App
  -> App Studio 展示为可添加候选
  -> 用户加到本地应用面板
  -> AppEntry 同时带 manifest + importedRunEvidence + installEvidence
  -> 市场安装路径与 DataSrv 发现路径的证据结构统一
  -> 运行/诊断/发布不再区分“从市场安装”还是“从 DataSrv 发现”
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "restores DataSrv installed enterprise normal app run evidence into app candidates|turns DataSrv installed MaClaw apps into addable app candidates with layout metadata"
npm.cmd run build
```

### 推进记录：GUI MIS Data Tool 支持读取单个 App 安装证据（2026-06-27）
本轮继续补齐 MaClaw App 作为“带特殊数据的超级 Skill”在运行态和诊断态的基础查询能力。此前 `list_app_installations` 已经能够按审批实例、业务记录、数据集、对象角色、依赖状态、测试证据等维度筛选安装记录；但 App Studio、运行态代理和诊断面板在拿到具体 `app_id` 后，还缺少一个稳定入口直接读取该 App 的完整安装证据。

已落地调整：

```text
GUI MIS Data Tool
  -> 新增 action: get_app_installation
  -> 参数兼容 app_id / appId / id
  -> 调用 DataSrv:
     GET /api/v1/data/app-installations/{appId}
  -> 返回 app 安装记录完整 JSON

可读取的信息
  -> app manifest / definition hash
  -> role bindings / dataset / object role
  -> dependency check / missing skill / blocking dependency
  -> test evidence / publish evidence
  -> approval_instance / result_payload / outputs
```

对应全链路意义：

```text
App Studio 测试/上传
  -> list_app_installations 找到候选记录
  -> get_app_installation 读取当前 app 的完整证据
  -> 校验 definitionHash 与测试证据是否一致
  -> 校验依赖 Skill 是否已安装、是否阻塞
  -> 校验审批实例、业务记录、输出结果是否闭环

运行态/诊断
  -> 用户打开某个 MaClaw App
  -> 代理或 GUI 直接读取该 app 安装证据
  -> 决定是否展示审批实例入口、结果入口、依赖修复入口、记录详情入口
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestExecuteMISDataToolListAppInstallationsPassesDependencyFilters|TestExecuteMISDataToolGetAppInstallation"
```

### 推进记录：前端安装后保留 App 级安装证据（2026-06-27）
本轮继续补齐“安装 MaClaw App 后可恢复、可诊断、可继续运行”的前端状态链。此前安装结果面板已经能够展示 `install_evidence` 中的版本快照、依赖验证、布局证据、结果契约和测试证据，但真正写回本地应用面板的 `AppEntry` 只保留了 `versionSnapshot`、`workflowContract` 和 `importedRunEvidence`，没有保留完整 `installEvidence`。这会导致用户关闭市场安装结果后，后续运行面板、发布复核、诊断入口无法从应用对象本身拿到完整安装证据。

已落地调整：

```text
AppEntry
  -> 新增 installEvidence 字段

市场安装成功
  -> installedAppWithInstallEvidence 从 install_record.install_evidence[app_id] 提取 app 级证据
  -> 写回 AppEntry.installEvidence
  -> 同步保留 versionSnapshot / workflowContract / importedRunEvidence

同版本或重复安装补证据
  -> 当本地已有 AppEntry 且版本不升级时
  -> 若已有应用缺少 installEvidence，则用本次安装证据补齐
```

对应全链路意义：

```text
Hub/市场安装 -> RecordMaclawAppInstall -> install_evidence
  -> 前端安装结果展示
  -> AppEntry.installEvidence 持久化到本地应用面板
  -> 后续运行、发布复核、依赖诊断、DataSrv 注册状态展示可以继续使用同一份证据
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "keeps dependency verification visible after single market app install"
```

### 推进记录：发布复核优先使用安装证据中的依赖健康状态（2026-06-27）
本轮继续把 `AppEntry.installEvidence` 从“安装结果展示”推进到“运行/发布复核可复用的权威证据”。此前发布检查中的依赖摘要主要从 manifest 的 Skill 声明推导，只能说明“依赖了哪些 Skill”，无法显示安装后依赖是否已安装、是否 ready、是否 failed/disabled。对于 MaClaw App 作为超级 Skill 的全链路来说，发布复核必须看到安装证据里的真实依赖健康状态。

已落地调整：

```text
AppEntry.installEvidence
  -> 读取 dependency_verification.dependencies
  -> 回退读取 installEvidence.dependencies
  -> 生成 AppDependencyEvidence

发布检查 / 发布包治理信息
  -> appDependencyEvidence 优先使用安装证据
  -> 依赖摘要追加 health/action 状态
     例如: skill@1.0.0 (runtime_skill, ready)
  -> appGovernanceForManifest 在没有实时 Plan override 时
     从 installEvidence.dependency_verification 生成标准 dependencyVerification
```

对应全链路意义：

```text
安装 MaClaw App
  -> 保存 app 级 installEvidence
  -> 发布复核读取真实依赖状态
  -> 复制提交包 / 本地回退包也保留标准 dependencyVerification
  -> Hub 审核与后续重装不再只依赖 manifest 声明
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "includes enterprise visual UI metadata in market submission packages"
npm.cmd test -- AppsPage.test.tsx -t "keeps dependency verification visible after single market app install"
npm.cmd run build
```

### 推进记录：DataSrv app-installations 支持审批/记录精确标识过滤（2026-06-27）
本轮继续把 GUI 的“打开远端审批和业务记录”能力反向补齐到 DataSrv 查询契约。此前 GUI 已能从 app-installations evidence 中提取 `approval_id`、`workflow_instance_id`、`dataset_id`、`record_id` 并构造远端详情 URL，但 DataSrv `/api/v1/data/app-installations` 与 GUI MIS Data Tool 尚不能直接按这些标识过滤；agent 或运维诊断仍需要先拉宽列表再自行扫描 metadata。

已落地调整：

```text
QueryAppInstallationsInput
  -> 新增 ApprovalID
  -> 新增 WorkflowInstanceID
  -> 新增 DatasetID
  -> 新增 RecordID

GET /api/v1/data/app-installations
  -> approval_id / record_approval_id
  -> workflow_instance_id / approval_instance_id / instance_id
  -> dataset_id / dataset
  -> record_id / business_record_id

SQLite ListAppInstallations metadata filter
  -> 从顶层 metadata 匹配上述标识
  -> 从 test_evidence / approval_instance 匹配上述标识
  -> 从 result_payload 匹配上述标识
  -> 支持 snake_case、camelCase、ID/Id 常见变体

OpenAPI
  -> 公开 approval_id
  -> 公开 workflow_instance_id
  -> 公开 dataset_id
  -> 公开 record_id

MIS Data Tool list_app_installations
  -> 同步支持上述过滤参数和常用别名
```

对应全链路意义：

```text
MaClaw App 审批型应用运行/测试/安装证据
  -> DataSrv app-installations 保存 approval/workflow/record 标识
  -> DataSrv 可直接按具体审批或业务记录查询相关 App 安装证据
  -> GUI / agent / 运维诊断复用同一过滤契约
  -> “概览 -> 明细 -> 具体实例/记录 -> 安装证据”链路更完整
```

已通过定向验证：

```text
go test ./structureddata -count=1 -run "TestListAppInstallationsFiltersByApprovalResultMetadata|TestListAppInstallationsFiltersByApprovalInstanceEvidence"
go test ./structureddata -count=1 -run "TestHTTPServerAppInstallationsOverrideObjectRoleBindings|TestHTTPServerOpenAPIIncludesExpectedQueryParameters"
go test ./gui -count=1 -vet=off -run "TestExecuteMISDataToolListAppInstallationsPassesDependencyFilters"
```

### 推进记录：DataSrv app-installations 的 dataset/objectRole 过滤接入 role bindings（2026-06-27）
本轮继续补齐上一段精确标识过滤的业务对象绑定侧。此前 `/api/v1/data/app-installations?dataset_id=...` 已能从 metadata、test evidence、approval instance、result payload 中匹配 dataset，但很多企业普通应用或刚安装的审批型应用只在 `app_role_bindings` 表中保存 DataSrv 业务对象绑定；这类 App 如果还没有运行/测试 evidence，会被 dataset 查询漏掉。同时 GUI 明细已经显示 `objectRole`，但 DataSrv app-installations 还没有公开 `object_role` 过滤。

已落地调整：

```text
QueryAppInstallationsInput
  -> 新增 ObjectRole

GET /api/v1/data/app-installations
  -> object_role / object

SQLite ListAppInstallations
  -> dataset_id 过滤支持 metadata/evidence 命中
  -> dataset_id 过滤同时支持 app_role_bindings.dataset_id 命中
  -> object_role 过滤支持 metadata/evidence 命中
  -> object_role 过滤同时支持 app_role_bindings.object_role 命中
  -> 涉及 role binding 过滤时，先读取 app_installations rows，关闭 rows 后再加载 role bindings
     避免在 SQLite rows 未关闭时嵌套查询 app_role_bindings 导致连接等待

OpenAPI / MIS Data Tool
  -> app-installations 公开 object_role 参数
  -> list_app_installations 支持 object_role / objectRole
```

对应全链路意义：

```text
MaClaw App 安装
  -> 保存 app_role_bindings
  -> 未运行或未形成 approval evidence 时也能按 dataset/objectRole 找到 App
  -> 企业普通应用、审批型应用、运维诊断可以统一从业务对象反查相关 App
  -> “业务对象 -> 已安装 App -> 运行/审批/结果证据”的方向补齐
```

已通过定向验证：

```text
go test ./structureddata -count=1 -timeout 60s -run "TestListAppInstallationsFiltersByRoleBinding|TestListAppInstallationsFiltersByApprovalResultMetadata|TestListAppInstallationsFiltersByApprovalInstanceEvidence"
go test ./structureddata -count=1 -timeout 60s -run "TestHTTPServerAppInstallationsOverrideObjectRoleBindings|TestHTTPServerOpenAPIIncludesExpectedQueryParameters"
go test ./gui -count=1 -vet=off -run "TestExecuteMISDataToolListAppInstallationsPassesDependencyFilters"
```

### 推进记录：GUI DataSrv 审批明细支持打开远端审批和业务记录（2026-06-27）
本轮继续把上一段“用 approval_id / workflow_instance_id / record_id 命中具体实例”推进为可执行入口。此前 DataSrv 审批概览明细已经能筛选真实审批实例，但用户仍需要复制标识后手工去 DataSrv 查询审批详情或业务记录；全链路上缺少从 MaClaw App 工作台到 DataSrv 审批/记录详情的直接跳转。

已落地调整：

```text
DataSrv approval summary item
  -> 新增 datasetID
  -> 新增 objectRole
  -> 新增 detailURL

DataSrv metadata 解析
  -> 从 role_bindings / approval_instance / result_payload / 顶层 metadata 中读取 dataset_id、object_role
  -> 兼容 detail_url / detailURL

GUI 明细行
  -> 从单个整行按钮改为：主筛选按钮 + 行内动作区
  -> 主按钮仍用于按 approvalID / workflowInstanceID / recordID 筛选真实实例
  -> “打开审批”构造：
     detail_url 优先
     /api/v1/data/approvals/{approval_id}
     或 /api/v1/data/approvals?workflow_instance_id=...&record_id=...&app_id=...
  -> “打开记录”构造：
     /api/v1/data/datasets/{dataset_id}/records/{record_id}
  -> 无足够标识时动作按钮禁用
```

对应全链路意义：

```text
MaClaw App 审批实例工作台
  -> DataSrv app-installations 审批概览
  -> 精确筛选 GUI 合并审批实例
  -> 打开 DataSrv approval detail / business record detail
  -> 审批结果反馈与业务记录追踪开始形成可操作闭环
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "shows DataSrv approval app summary with approval result filters"
```

### 推进记录：GUI DataSrv 审批概览支持点入明细（2026-06-27）
本轮继续把“审批型应用的实例数据管理”从摘要推进到可查看明细。上一段已经在审批实例管理中显示 DataSrv app-installations 的审批概览，但每个分类只显示数量和最近应用名；用户仍无法在该工作台里确认这些数量对应哪些应用、当前节点是什么、输出类型是什么。

已落地调整：

```text
DataSrv approval overview
  -> 每个分类从纯摘要扩展为可点击 bucket
  -> 点击后在同一审批管理区域展开明细
  -> 明细包含：
     app_id / app 名称
     当前节点 / workflow result node
     审批状态或审批决定
     result_contract / result_coverage / outputs 推导出的输出类型
     更新时间或测试证据 verified_at

DataSrv metadata 解析
  -> 从 test_evidence.approval_instance 读取 status / decision / current_node
  -> 从 result_payload 读取 decision / approval_result
  -> 从 result_contract_types、test_evidence_covered_types、primary_result、outputs.kind 推导结果类型
```

对应全链路意义：

```text
DataSrv app-installations 审批过滤契约
  -> GUI 审批概览分类
  -> 点击分类查看匹配应用明细
  -> 审批型 App 的“我的申请 / 我审批的 / 审批结果 / 输出结果”不再停留在数量层
  -> 后续可继续把这些明细连接到具体 DataSrv approval instance / business record
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "shows DataSrv approval app summary with approval result filters"
npm.cmd test -- AppsPage.test.tsx -t "opens global approval management from the operation section"
npm.cmd run build
```
### 推进记录：GUI DataSrv 回流提升旧式嵌套审批结果包（2026-06-27）
本轮补齐了“DataSrv 已安装应用回流 -> GUI 可安装应用候选 -> 本地 AppEntry 运行证据”的兼容口径。旧数据或第三方实现可能只把审批结果包放在 `test_evidence.approval_instance.result_payload / outputs / artifacts` 下，而不是顶层 `test_evidence.result_payload / outputs / artifacts`；GUI 现在会在读取 DataSrv capabilities 和 app_installations 时把这些嵌套结果包提升为标准安装证据。

落地调整：

```text
DataSrv installed app recovery
  -> 顶层 test_evidence 仍然优先
  -> 缺少顶层结果包时，从 approval_instance.result_payload / outputs / artifacts 提升
  -> AppEntry.installEvidence.test_evidence 恢复完整结果包
  -> AppEntry.importedRunEvidence 恢复同一份运行结果包
  -> 冷启动、市场安装记录、DataSrv 已安装候选三条入口保持一致
```

对应全链路意义：

```text
旧式 DataSrv / 历史安装记录
  -> GUI 发现已安装 MaClaw App
  -> 仍能显示审批结果、输出类型、附件/文档摘要
  -> 审批型 App 的“结果反馈”和“实例数据管理”不因证据层级差异丢失
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "turns DataSrv installed MaClaw apps into addable app candidates with layout metadata"
npm.cmd test -- AppsPage.test.tsx -t "turns DataSrv installed MaClaw apps into addable app candidates with layout metadata|restores single app install evidence from top-level market install records|restores dynamic layout and install evidence from stored app entries after cold start"
npm.cmd run build
```
### 推进记录：企业普通应用运行返回标准结果包（2026-06-27）
本轮继续补齐“企业普通应用”的运行结果合同。企业普通应用不走审批实例管理，但它仍然是 MaClaw App 的一种运行形态，DataSrv 业务动作、视图、报表、仪表盘调用后的返回不能只停留在原始 `response`，还需要给前端、测试证据、后续发布/回流链路提供统一的结果包。

已落地调整：

```text
ExecuteMaclawAppBusinessOperation
  -> 保留 synced / mode / target / response 等旧字段
  -> 新增 result_payload，合并 DataSrv 返回与 app_id / dataset_id / object_role / business_action / result_status
  -> 新增 outputs，优先使用 DataSrv outputs，否则按操作类型生成默认输出项
  -> 新增 artifacts，优先使用 DataSrv artifacts，否则返回空列表
  -> 新增 primary_result
       business_action + record/record_id -> business_record
       business_view -> records
       business_report -> report
       business_dashboard -> dashboard
  -> 新增 business_status，优先 DataSrv business_status，否则沿用 result_status
```

对应全链路意义：

```text
企业普通应用
  -> DataSrv 业务动作/查询/报表/仪表盘
  -> 标准 result_payload / outputs / artifacts / primary_result
  -> 可被 GUI 运行面板、测试证据、发布治理、安装回流复用
  -> 与审批型应用的结果反馈合同对齐，但不引入审批实例管理
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestExecuteMaclawAppBusinessOperationRunsPreferred(Action|Report|Dashboard)|TestExecuteMaclawAppBusinessOperationQueriesPreferredView"
```

### 推进记录：Hub store 汇总修复与 GUI remote 测试隔离（2026-06-28）
本轮继续从“DataSrv -> Hub -> MaClaw App -> GUI 运行”的验收角度收口，重点补齐上一轮遗留的 Hub store 回归，并定位 GUI 全包超时中暴露出的 remote tool 测试串味问题。

已落地调整：

```text
Hub store session duration
  -> SummarizeUserDurations 不再只依赖 machine_heartbeat_log
  -> 同时读取 sessions 表中的历史/legacy 区间
  -> running/host_online session 统计到传入 now
  -> exited session 优先使用 ended_at，legacy 空 updated_at 可回退
  -> 同一 user_id 的 session/heartbeat 区间合并后再汇总，避免多机器重叠重复计时
  -> user_id 继续批量解析为 users.email，direct@example.com 这类 user_id 仍可作为 email fallback

GUI remote tool 测试隔离
  -> corelib/remote 新增 ResolveToolPathInDir(tool, toolsRoot)
  -> ToolManager.GetToolStatus 优先使用 app.GetDataDir()/tools，而不是进程级 os.UserHomeDir()
  -> tool status cache key 加入 toolsDir，避免不同 testHomeDir/profile 之间复用陈旧工具路径
  -> InvalidateToolStatusCache 删除同一 tool 的所有目录变体
  -> remote session 相关测试补充 App shutdown cleanup，释放 memory.db 与后台循环
  -> remote session 测试断言从“完整 AI assistant ready”收紧为“remote hub client 已初始化”，避免把 startup warmup 契约误放到 remote launch 单测里
```

本轮已通过验证：

```text
go test ./hub/internal/store/sqlite -count=1 -vet=off -run "TestSessionRepositorySummarizeUserDurations" -timeout 60s
  -> ok github.com/RapidAI/CodeClaw/hub/internal/store/sqlite

go test ./hub/internal/store/... -count=1 -vet=off -timeout 120s
  -> ok github.com/RapidAI/CodeClaw/hub/internal/store/sqlite

go test ./gui -count=1 -vet=off -run "TestBugCondition_NonDesktopSourceBlockedByRemoteEnabled|TestGetRemoteToolReadinessDelegatesToDiagnosticCheck|TestGetRemoteToolLaunchProbeDelegatesToDiagnosticCheck|TestStartRemoteSessionSupportsCodex|TestStartRemoteHandoffSessionInitializesAIAssistantWhenCreatingHubClient|TestStartRemoteSessionSupportsOpencode|TestStartRemoteSessionSupportsIFlow|TestStartRemoteSessionSupportsKilo" -timeout 120s
  -> ok github.com/RapidAI/CodeClaw/gui

go test ./gui -count=1 -vet=off -run "MaclawApp|ApprovalPending|RecordMaclawAppInstall" -timeout 90s
  -> ok github.com/RapidAI/CodeClaw/gui

go test ./hub/internal/httpapi -count=1 -vet=off -run "MaclawApp|AppInstall|CapabilityMaclaw|Approval" -timeout 90s
  -> ok github.com/RapidAI/CodeClaw/hub/internal/httpapi

cd datasrv
go test ./... -count=1 -vet=off
  -> ok github.com/RapidAI/CodeClaw/datasrv/cmd/maclaw-data-srv
  -> ok github.com/RapidAI/CodeClaw/datasrv/structureddata
```

当前仍需继续：

```text
GUI package-level full regression
  -> 上一轮 go test ./gui -timeout 300s 仍超时
  -> 已确认其中一类失败来自 remote tool 路径缓存串味，本轮已修并通过定向 remote 回归
  -> 后续需要重新跑 GUI 全包，若仍超时，再按 JSON 中最后 run/fail 测试继续拆分

前端 AppsPage 全量
  -> 之前 AppsPage 目标链路已过，但历史全量仍有旧文案/边界测试失败
  -> 不阻塞本轮 DataSrv/Hub/App 后端主链路，但发布前仍需继续按失败簇治理
```

### 推进记录：收口 AppsPage 全量前端回归（2026-06-28）

本轮继续从 App Studio 制作、管理、市场安装、升级、运行证据这条前端主链路收口。上一段记录中 `AppsPage.test.tsx` 仍有 49 个失败；本轮把剩余失败推进到全量通过。

已落地调整：

```text
App Studio 管理面板
  -> 编辑弹窗的名称、分类、描述字段补稳定 data-testid
  -> 测试限定在编辑弹窗内操作字段，避免命中页面上其它同名输入
  -> 验证内置 App 元数据编辑后能写入 manifest，并在冷启动后恢复
  -> 复制、删除、清空历史、结构化字段编辑链路恢复通过

能力市场 / 安装预览
  -> 市场列表统计断言对齐当前 UI：可添加 4 · 可升级 0
  -> 安装预览统计断言对齐当前 UI：可安装 N · 可升级 N · 将跳过 N
  -> 升级包测试限定到导入包区域点击“安装 / 确认安装”
  -> 高风险新增权限升级进入二次确认态，确认后完成升级并保留版本变更明细
  -> 安装结果中“未选择 / 已升级 v1 -> v2”等旧乱码或旧标点断言已修正

前端回归噪声
  -> 清理剩余旧中文乱码断言，例如“模式: JSON”
  -> apps-page-vitest.json 更新为最新全量结果
```

已通过验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "edits built-in app metadata from app studio management and persists it|duplicates an app from app studio management and keeps the source skill binding|deletes duplicated local apps and clears their run history|edits tool app structured fields from app studio management"

npm.cmd test -- AppsPage.test.tsx -t "shows market apps as a list and adds one to the panel|upgrades installed market apps when a higher manifest version is pasted|previews manifest apps before installing from market|installs only selected apps from the market preview"

npm.cmd test -- AppsPage.test.tsx --reporter=json --outputFile=apps-page-vitest.json
  -> 194 total
  -> 194 passed
  -> 0 failed
```

当前意义：

```text
App Studio 动态界面制作 / 管理
  -> 内置 App 和本地复制 App 可编辑、保存、恢复

MaClaw App 市场安装
  -> 市场列表、粘贴 manifest 预览、选择安装、重复跳过、升级确认链路可验证

超级 Skill 依赖与证据链
  -> 安装预览仍展示依赖验证、版本快照、测试证据和风险提示
  -> 发布/安装/冷启动相关前端主链路拥有一份全量通过的 AppsPage 回归基线
```

仍未宣称整套 MaClaw App 全链路完成。下一步继续扩大到 GUI Go 包、DataSrv、Hub 安装/上传/审批 workflow 设计与安装的端到端验证，尤其是：

```text
go test ./gui -count=1 -vet=off -timeout 150s
DataSrv app-installations / approvals / capabilities HTTP 回归
Hub app package submit / review / install / dependency gate 回归
真实审批型应用：发起 -> workflow Skill -> 审批实例 -> DataSrv 同步 -> 我的申请/待我审批/已处理/需关注 -> 结果反馈
```

### 推进记录：Hub 提交版本保留完整 MaClaw App 包体（2026-06-28）

本轮继续检查“App Studio 测试上传 -> Hub 审核 -> 能力市场下载/安装 -> 冷启动再发布”的包体保真链路。Hub 下载端已经从 capability version 的 `ManifestJSON` 还原 `maclaw.app.v1` entry，并在下载时补入 review/submission metadata；但提交端原有测试主要验证 metadata 摘要，没有直接证明 version manifest 保存的是完整 App entry。

已落地验证：

```text
CapabilityMaclawAppSubmitHandler
  -> 解析 maclaw.app.pack.v1
  -> 对每个 app 写入 capability version ManifestJSON
  -> ManifestJSON 保留完整 maclaw.app.v1 entry

提交后 version manifest 覆盖：
  -> app.ui.layouts.*.regions 动态布局位置
  -> binding.dependencies.skills Skill 依赖声明
  -> binding.workflow 节点映射
  -> governance.resultContract
  -> governance.testEvidence.outputs / artifacts
  -> governance.testEvidence.approvalInstance
  -> approvalInstance.resultPayload / workflowSkillId / currentNode
```

对应全链路意义：

```text
App Studio 上传完整 MaClaw App
  -> Hub 不把企业 App 降级成 metadata-only 能力摘要
  -> 审核通过后下载包仍能恢复传统软件式动态 UI、依赖、审批 workflow、测试证据和结果包
  -> GUI 安装/冷启动/二次发布可继续使用同一份完整 app manifest
```

已通过定向验证：

```text
go test ./hub/internal/httpapi -count=1 -run "TestCapabilityMaclawAppSubmitCreatesPendingReviewCapability|TestCapabilityMaclawAppPackageDownloadReturnsApprovedPack"
```

### 推进记录：GUI 从 Hub 安装时保留动态 UI layout 并注册到 DataSrv（2026-06-28）

本轮继续顺着 Hub 下载包进入 GUI 安装链路检查。`InstallMaclawAppPackageFromHub` 已经会下载完整 `maclaw.app.pack.v1`，执行依赖安装计划和治理门禁，然后用同一份包体写入本地安装记录并注册到 DataSrv。此前测试覆盖了 Hub 下载、安装审计、DataSrv 注册和测试证据摘要，但安装样例缺少动态 UI layout，不能证明用户在 App Studio 调节过的界面布局会从 Hub 安装进入 DataSrv 安装元数据。

已落地验证：

```text
Hub package app.ui.layouts
  -> normal_workspace
  -> classic_split / compact
  -> primaryRegion=left
  -> outputRegion=right
  -> regions: input_form / record_grid / result_panel

governance.workspaceLayout
  -> 作为发布/安装治理证据
  -> regionCount=3
  -> roles 覆盖 enterprise_normal_app 必需的 input / record_list / output

GUI InstallMaclawAppPackageFromHub
  -> 下载完整 Hub package
  -> 通过治理门禁
  -> RecordMaclawAppInstall
  -> registerMaclawAppInstallationsToDataSrv
  -> DataSrv metadata.workspace_layout 保留 entry、template、primary/output region、region_count、regions
```

对应全链路意义：

```text
Hub 能力市场安装企业普通应用
  -> GUI 不只安装 Skill 依赖和运行证据
  -> 也把传统软件式动态界面布局写入 DataSrv app_installations metadata
  -> DataSrv capabilities 回流后，App Studio / 应用面板可按用户设计的区域位置重建工作台
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestInstallMaclawAppPackageFromHubDownloadsAndRecordsInstall|TestInstallSelectedMaclawAppPackageFromHubFiltersPackageApps"
```

### 推进记录：前端 Hub 安装恢复标准 app.ui 动态布局（2026-06-28）

本轮继续检查 Hub 安装结果进入 GUI 应用面板后的状态恢复。后端已经能从标准 `maclaw.app.v1` 的 `app.ui.layouts` 提取动态 UI layout，并注册到 DataSrv；但前端 `manifestToAppEntry` 读取市场包时只看旧式 `binding.ui`，如果 Hub 下载包使用标准顶层 `app.ui`，应用面板会退回默认布局，导致“用户在 App Studio 调节的位置”在前端本地 AppEntry 中丢失。

已落地调整：

```text
manifestToAppEntry
  -> ui 来源改为 app.ui || app.binding.ui
  -> 标准 maclaw.app.v1 顶层 app.ui 优先
  -> 旧包 binding.ui 继续兼容

Hub 市场安装测试
  -> Hub package.app.ui.layouts.approval_workspace 带 regions
  -> install_evidence.workspace_layout 带 regions
  -> 安装后 localStorage customApps[].manifest.ui 保留完整 layout
  -> 安装后 customApps[].installEvidence.workspace_layout 保留完整 install evidence layout
```

对应全链路意义：

```text
App Studio 设计标准 app.ui
  -> 上传 Hub
  -> Hub 下载/安装
  -> GUI 前端应用面板恢复 AppEntry.manifest.ui
  -> GUI 后端/DataSrv 安装证据恢复 workspace_layout
  -> 本地应用运行、二次发布、冷启动恢复都使用同一份动态界面布局
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "installs approved Hub MaClaw Apps from market search results"
```
### 推进记录：企业普通应用实际运行结果进入发布证据包（2026-06-28）
上一轮已经确认 DataSrv 和 GUI 后端会透传企业普通应用标准结果包。本轮继续补齐 App Studio 发布链路中的实际运行场景：不能只靠测试 helper 或安装回流证据证明发布包完整，用户在 GUI 内点击“执行”后产生的真实运行历史，也必须能直接满足“提交审核 / 上传能力市场”的治理证据要求。

已落地调整：

```text
GUI recordRunHistory
  -> 所有成功运行默认补写当前 App definitionHash
  -> 所有成功运行默认补写当前 testProtocolFingerprint
  -> 如果本次运行前执行过 PlanMaclawAppInstall，则把运行时依赖验证证据写入 dependencyVerification
  -> 企业普通应用 DataSrv 执行路径不再产生缺 definitionHash / dependencyVerification 的半证据

企业普通应用实际运行
  -> ExecuteMaclawAppBusinessOperation 返回 primary_result / result_payload / outputs / artifacts
  -> businessOperationRunEvidence 写入运行历史
  -> recordRunHistory 自动生成 resultCoverage
  -> latestAppRunEvidence 识别当前 definitionHash 的真实运行证据
  -> appGovernanceForManifest 将 resultPayload / outputs / artifacts / resultCoverage / dependencyVerification 写入 governance.testEvidence
```

对应全链路意义：

```text
企业普通应用
  -> 用户在传统软件式业务工作台点击执行
  -> DataSrv 返回标准结果包
  -> GUI 运行历史保留结果、附件、输出块、定义指纹和依赖验证
  -> App Studio 提交审核时直接消费同一份运行证据
  -> Hub / 能力市场获得可审计的 resultPayload / outputs / artifacts / resultCoverage
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "publishes enterprise normal app evidence from an actual DataSrv business run"
npm.cmd test -- AppsPage.test.tsx -t "renders enterprise normal apps as business workspaces without approval instances|requires run evidence to cover the declared primary result contract|requires dependency verification to cover declared app Skill dependencies before publishing"
npm.cmd run build
```
### 推进记录：工具型应用实际运行证据进入发布证据包（2026-06-28）
上一轮补齐了企业普通应用的实际运行证据闭环。本轮继续对齐工具型应用：工具型 App 不连接企业后台数据，但它仍是 MaClaw App 超级 Skill，需要把真实运行产生的文本内容、文档产物、输出块、结果覆盖和依赖验证一起进入 App Studio 发布包，不能只证明“有一个 artifact”。

已落地调整：

```text
工具型 App 实际运行
  -> RunNLSkillAsync / GetNLSkillRunStatus 返回 outputs / summary.output_blocks / artifacts
  -> 运行历史保存 resultPayload / outputs / artifacts
  -> recordRunHistory 自动补写 definitionHash / testProtocolFingerprint / dependencyVerification
  -> appRunDependencyVerificationEvidence 将运行前依赖验证归属到当前 canonical App ID
  -> appGovernanceForManifest 将工具型运行证据写入 governance.testEvidence
```

测试覆盖已经明确验证：

```text
工具型 App 真实执行
  -> resultPayload.text 保留主要文档输出摘要
  -> outputs 保留 document/content 输出块
  -> artifacts 保留 docx/pdf 文件产物、mimeType、sizeBytes
  -> resultCoverage 覆盖 artifact / document / content
  -> dependencyVerification 覆盖声明的 app_skill / runtime skill 依赖
  -> 提交审核包携带同一份 governance.testEvidence
```

对应全链路意义：

```text
工具型应用
  -> 用户在传统软件式工具工作台执行
  -> Skill 运行产出文本、文档、文件产物
  -> GUI 运行历史保留完整结果包和依赖验证
  -> App Studio 提交审核时复用同一份真实运行证据
  -> Hub / 能力市场安装包具备可审计的工具输出合同
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "uses the enterprise market bridge when submitting app packages"
npm.cmd test -- AppsPage.test.tsx -t "uses the enterprise market bridge when submitting app packages|requires dependency verification to cover declared app Skill dependencies before publishing|publishes enterprise normal app evidence from an actual DataSrv business run"
npm.cmd run build
```
### 推进记录：审批型应用实际 workflow 运行证据进入发布包（2026-06-28）
上一轮补齐了工具型应用的真实运行证据发布链路。本轮继续把企业审批型应用的前端真实运行链路收口：审批型应用不能只靠 seed 的 approvalInstance 或安装回流证据通过发布门禁，用户在审批工作台点击执行、workflow Skill 完成、DataSrv 同步回填后的最终审批实例，也必须能直接进入 App Studio 提交审核包。

已落地验证链路：

```text
本地企业审批型 App
  -> 声明 app_skill 超级 Skill 依赖
  -> 声明 workflow_skill 审批工作流依赖
  -> 声明 datasrv / approvalBindings / workflow node mapping / resultContract
  -> 运行前 PlanMaclawAppInstall 生成依赖验证
  -> RunNLSkillAsync 启动审批 workflow skill
  -> RecordMaclawAppApprovalInstance 先记录 pending 实例
  -> SyncMaclawAppApprovalInstanceToDataSrv 同步 pending RecordApproval
  -> GetNLSkillRunStatus 返回 approved 结果包
  -> finalizeApprovalRunFromStatus 生成最终 approvalInstance
  -> SyncMaclawAppApprovalInstanceToDataSrv 同步最终 RecordApproval
  -> 运行历史保存 approvalInstance.resultPayload / outputs / artifacts
  -> App Studio 发布包携带 governance.testEvidence.approvalInstance
```

测试覆盖已经明确验证：

```text
approvalInstance
  -> approvalID 使用最终 DataSrv approval id
  -> workflowSkillId / workflowVersion / approvalEvent 保留
  -> currentNode / businessStatus / resultStatus 保留
  -> recordID / datasetID / objectRole 保留
  -> resultPayload 包含 approval_result / business_status / business_record
  -> outputs 保留业务记录与通知输出
  -> artifacts 保留审批 PDF / 结果包文件
  -> approvalInstanceViewVerified = true

governance
  -> dependencyVerification 覆盖 app_skill 和 workflow_skill
  -> testEvidence.resultPayload 与 approvalInstance.resultPayload 对齐
  -> resultCoverage 覆盖 approval_result / business_status / business_record / document
```

对应全链路意义：

```text
企业审批型应用
  -> 数据录入
  -> 审批 workflow skill 运行
  -> 审批实例数据管理
  -> DataSrv RecordApproval 同步
  -> 结果反馈
  -> App Studio 提交审核
  -> Hub / 能力市场获得可审计的审批实例证据
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "publishes approval app evidence from an actual workflow run"
npm.cmd test -- AppsPage.test.tsx -t "records and completes an approval instance when running an approval app|publishes approval app evidence from an actual workflow run|requires approval instance evidence before publishing approval apps|blocks approval app publish submission when workflow contract verification fails"
npm.cmd run build
go test ./gui -count=1 -vet=off -run "TestRecordMaclawAppInstallRegistersApprovalAppWithDataSrv"
```
### 推进记录：Hub 重装审批型应用的安装证据可冷启动再发布（2026-06-28）
上一轮已经验证企业审批型应用的真实 workflow 运行证据可以进入发布包。本轮继续补齐“Hub 下载重装 / DataSrv 安装回灌 / 冷启动再发布”这一段：安装后的审批型 App 可能没有本地 run history，但安装记录里的 `installEvidence.test_evidence` 已经是经过市场审核和安装审计保存的证据。只要这份证据绑定当前 App 定义指纹，就应该能在 App Studio 冷启动后用于再次提交审核。

已落地验证链路：

```text
Hub / DataSrv 安装记录
  -> installEvidence.dependency_verification
  -> installEvidence.test_evidence
  -> test_evidence.approval_instance
  -> AppEntry.installEvidence
  -> latestAppRunEvidence fallback
  -> appGovernanceForManifest
  -> governance.testEvidence
  -> Submit for review
```

测试覆盖已经明确验证：

```text
冷启动审批型 App
  -> 本地 run history 为空
  -> importedRunEvidence 可为空
  -> installEvidence.test_evidence.definition_fingerprint 匹配当前 App 定义
  -> 发布门禁读取安装证据作为运行证据
  -> approvalInstance 保留 instanceId / approvalID / workflowSkillId / workflowVersion / approvalEvent
  -> resultPayload / outputs / artifacts 保留
  -> resultCoverage 保留 approval_result 覆盖
  -> dependencyVerification 覆盖 app_skill 和 workflow_skill
```

同时修正测试数据口径：

```text
工具型 App 若声明 appSkill
  -> 发布门禁按 app_skill 超级 Skill 依赖检查
  -> 冷启动安装证据中的 dependencyVerification 也必须使用 app_skill
  -> 避免 runtime_skill 旧测试数据误绕过当前超级 Skill 依赖模型
```

对应全链路意义：

```text
能力市场安装 / Hub 重装
  -> 安装记录保存审批实例测试证据
  -> App Studio 冷启动恢复 App
  -> 用户无需立即重新运行也能看到安装时测试证据
  -> 若 App 定义未变化，可复用该证据再次提交审核
  -> 若 App 定义变化，既有 definitionHash 门禁仍会要求重新测试
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "republishes cold-start approval apps from Hub install evidence"
npm.cmd test -- AppsPage.test.tsx -t "installs approved Hub MaClaw Apps from market search results|republishes cold-start approval apps from Hub install evidence|uses install evidence test records when republishing cold-start app packages|restores single app install evidence from top-level market install records"
npm.cmd run build
```

### 推进记录：DataSrv 审批 request OpenAPI 合同补齐运行上下文字段（2026-06-28）
上一轮 GUI 后端已经在 `SyncMaclawAppApprovalInstanceToDataSrv` 的 `create_record_approval` 请求中保留完整审批运行上下文，包括当前节点、审批决策、状态流转、业务状态、结果状态、结构化结果包、输出块和文件产物。本轮把同一组字段补进 DataSrv OpenAPI，避免外部集成方、GUI 回流代码或后续 SDK 只看到旧的 request 摘要合同。

已落地调整：

```text
recordApprovalRequestOpenAPISchema
  -> 新增 workflow_node_id / workflowNodeId
  -> 新增 workflowNodeIds，和既有 workflowNodeIDs 并存
  -> 新增 workflow_decision_id / workflowDecisionId
  -> 新增 from_status / fromStatus
  -> 新增 to_status / toStatus
  -> 新增 business_status / businessStatus
  -> 新增 result_status / resultStatus
  -> 新增 result_payload / resultPayload
  -> 新增 outputs
  -> 新增 artifacts

HTTP OpenAPI 测试
  -> create approval request body 检查 request 内部运行上下文字段
  -> approvals list response 检查 request 内部运行上下文字段
  -> approval detail response 继续检查审批字段合同
```

对应全链路意义：

```text
审批型 App 发起/审批/结果反馈
  -> GUI 后端把完整 approval runtime context 写入 DataSrv request
  -> DataSrv OpenAPI 对外声明同一组字段
  -> GUI 审批工作台、外部系统、SDK、Hub 安装证据复核可按合同读取节点、状态和结果包
  -> 我的申请 / 我审批的 / 结果反馈不再依赖未声明的隐式字段
```

已通过定向验证：

```text
go test ./structureddata -count=1 -timeout 60s -run "TestHTTPServerRequiresBearerTokenAndHandlesRecords"
```

### 推进记录：安装计划合并重复 Skill 依赖时保留 install_ref（2026-06-28）
本轮继续补齐“安装 MaClaw App 时自动检查并安装相关 Skill 依赖”的精确引用链。此前 `maclawAppDependenciesForEntry` 会在单个 App 内部按 Skill `id` 去重：如果 `binding.appSkill` 先声明了应用 Skill id，而 `dependencies.skills` 后续为同一个 Skill 补充了 `source=enterprise_hub` 和 `capability_id/install_ref`，去重会提前返回，导致安装计划只剩 skill id，丢失 Hub 能力市场的精确安装引用。

已落地调整：

```text
maclawAppDependenciesForEntry
  -> 重复 Skill id 合并时继续保留 required/version/kind
  -> 新增合并 source，后续显式 source 可覆盖默认 hub
  -> 新增合并 install_ref/capability_id，避免 binding 与 dependencies 汇合时丢失精确安装目标

InstallMaclawAppDependencies 回归测试
  -> binding.appSkill 只有 id
  -> dependencies.skills 为同一个 id 声明 enterprise_hub + capability_id
  -> install plan 最终 dependency.Source = enterprise_hub
  -> install plan 最终 dependency.InstallRef = capability_id
  -> 实际安装调用使用 enterprise_hub + capability_id
```

对应全链路意义：

```text
App Studio 可视化设计
  -> appSkill / workflowSkill 在界面绑定中出现
  -> dependencies.skills 保存 Hub/能力市场 install_ref
  -> PlanMaclawAppInstall 不丢 install_ref
  -> InstallMaclawAppDependencies 按精确 Hub capability 下载依赖 Skill
  -> RecordMaclawAppInstall / DataSrv / Hub 回流继续保留同一份依赖证据
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "Test(InstallMaclawAppDependenciesPreservesInstallRefFromDuplicateDependency|InstallMaclawAppDependenciesInstallsHubBackedSources|MaclawAppInstallSkillSourceNormalizesHubAndMarket)"
```

### 推进记录：GUI 审批工作台恢复 request-only 运行结果包（2026-06-28）
本轮继续收口“审批型应用 = 数据录入 + 审批 workflow Skill 运行 + 审批实例数据管理 + 结果反馈”的运行态回流。此前 DataSrv 审批列表如果把 `result_payload / outputs / artifacts / business_status / workflow_node_id` 等字段放在 approval 顶层，GUI 后端可以恢复；但前面已经把同一组运行上下文写入并声明在 `request` 内部，外部系统或兼容 DataSrv 响应有可能只返回 request 内嵌字段。旧转换逻辑会导致审批工作台丢失结果反馈、当前节点和状态流转。

已落地调整：

```text
maclawAppApprovalInstanceFromRecordApproval
  -> resultPayload 缺失时读取 request.result_payload / request.resultPayload
  -> outputs 缺失时读取 request.outputs / request.approval_outputs / request.approvalOutputs
  -> artifacts 缺失时读取 request.artifacts / request.approval_artifacts / request.approvalArtifacts
  -> CurrentNode / CurrentNodeIDs 读取 request.workflow_node_id / request.workflowNodeId / request.workflowNodeIds
  -> BusinessStatus / ResultStatus / FromStatus / ToStatus / WorkflowDecisionID 读取 request snake/camel 别名

结构恢复
  -> request 内嵌 outputs 转为 GUI maclawAppApprovalOutput
  -> request 内嵌 artifacts 转为 GUI maclawAppApprovalArtifact
```

对应全链路意义：

```text
审批 workflow Skill 运行
  -> GUI/DataSrv 写入审批 request 运行上下文
  -> DataSrv / 外部系统可返回顶层字段或 request-only 字段
  -> GUI 审批工作台仍能显示我的申请、待我审批、当前节点、状态流转、结果文本、输出块和文件产物
  -> 审批结果反馈不依赖某一种响应展开形态
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestListMaclawAppApprovalInstances(LoadsRequestOnlyRuntimeResult|AllLoadsDataSrvLane|MapsDataSrvAttentionStatus|AllInfersDataSrvLanesForCurrentUser)"
```

### 推进记录：App Studio 发布队列包身份字段对齐 package_sha/package_sha256（2026-06-28）
本轮继续补齐“App Studio 制作、测试、上传 Hub、安装回流”的包身份链路。此前安装结果已经同时返回 `package_sha` 和 `package_sha256`，但发布提交 `SubmitMaclawAppPackage` 的即时返回和 `ListMaclawAppPackageSubmissions` summary 只暴露 `package_sha256`。这会造成上传/安装两个环节的包身份字段不对称：Hub、GUI 发布历史或外部集成若按 `package_sha` 追踪包，发布队列阶段会拿不到同一字段。

已落地调整：

```text
SubmitMaclawAppPackage 返回体
  -> 新增 package_sha
  -> 保留 package_sha256
  -> 两者都指向同一 package fingerprint

maclawAppSubmissionSummary
  -> JSON 同时暴露 package_sha 与 package_sha256
  -> ListMaclawAppPackageSubmissions summary 可用两种字段追踪同一提交包

AppsPage 发布队列解析
  -> packageSHA 兼容 package_sha / package_sha256 / packageSHA
```

对应全链路意义：

```text
App Studio 保存/测试/提交发布包
  -> 本地提交队列记录 package_sha/package_sha256
  -> Hub 审核、同步、重新安装、DataSrv 回流都可用同一包哈希关联
  -> 安装结果与发布结果的包身份合同保持一致
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestSubmitMaclawAppPackageQueuesLocalSubmission"
npm.cmd test -- AppsPage.test.tsx -t "shows local app package submission queue summaries"
```

### 推进记录：Hub 同步与安装返回包身份字段对齐 package_sha/package_sha256（2026-06-28）
上一轮已经让 App Studio 本地发布队列同时暴露 `package_sha` 和 `package_sha256`。本轮继续把同一合同推进到 Hub 同步和 Hub 安装回流：`SyncMaclawAppPackageSubmissionToHub` 与 `InstallSelectedMaclawAppPackageFromHub` 过去只返回 `package_sha256`，而安装记录、DataSrv 回流和部分前端证据对象已经使用 `package_sha` 作为通用别名。字段不对称会让“上传到 Hub 后再下载安装”的包身份追踪中途断开。

已落地调整：

```text
SyncMaclawAppPackageSubmissionToHub
  -> 返回 package_sha
  -> 保留 package_sha256
  -> 优先使用 Hub package_sha256，缺失时回退本地提交包哈希

InstallSelectedMaclawAppPackageFromHub / InstallMaclawAppPackageFromHub
  -> 返回 package_sha
  -> 保留 package_sha256
  -> 两者指向本次实际安装包 fingerprint

AppsPage 安装证据写回
  -> installEvidence.package_sha 可从 package_sha256 回填
  -> installEvidence.package_sha256 可从 package_sha 回填
```

对应全链路意义：

```text
App Studio 发布包
  -> Sync 到 Hub
  -> Hub 审核/发布
  -> 从 Hub 下载安装
  -> RecordMaclawAppInstall / DataSrv app_installations / GUI AppEntry.installEvidence
  -> 全链路都可用 package_sha 或 package_sha256 追踪同一个包
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "Test(SyncMaclawAppPackageSubmissionToHubUpdatesLocalQueue|InstallMaclawAppPackageFromHubDownloadsAndRecordsInstall)"
npm.cmd test -- AppsPage.test.tsx -t "restores single app install evidence from top-level market install records"
```

### 推进记录：审批型 App 安装到运行实例回流端到端回归（2026-06-28）
本轮开始把此前分散的安装证据、审批实例同步和审批工作台回流串成一条更接近真实使用路径的回归。过去已有单点测试分别覆盖 `RecordMaclawAppInstall`、DataSrv `app_installations` 注册、`SyncMaclawAppApprovalInstanceToDataSrv`、`ListMaclawAppApprovalInstances`；但缺少一条证明“同一个企业审批型 App 安装后，后续运行产生的审批实例还能沿着同一份 workflow/data/result 合同进入 DataSrv 并回流到审批工作台”的链路。

已落地验证链：

```text
RecordMaclawAppInstall(enterprise_approval_app)
  -> 保存本地安装审计
  -> 注册 DataSrv app_installations
  -> metadata.install_evidence 保留 workspace_layout / workflow_mapping / workflow_contract / dependency_verification / test_evidence

SyncMaclawAppApprovalInstanceToDataSrv(runtime instance)
  -> 使用已安装 App 的 app_id / blueprint / object_role / workflowSkillId / workflowNodeId
  -> 先 PATCH business record，写入 approval_* 与 workflow_* 语义字段
  -> POST /records/{recordId}/approvals 创建审批实例
  -> request 内保留 resultPayload / outputs / artifacts

ListMaclawAppApprovalInstances(pending_my_approval)
  -> 从 DataSrv /approvals 回流同一实例
  -> 恢复 app identity、dataset/object role、workflow 状态、业务状态、结果文本、输出块和文件产物
```

对应全链路意义：

```text
企业审批型应用
  -> 数据录入/发起审批
  -> 审批 workflow Skill 运行
  -> 审批实例进入 DataSrv 管理
  -> 我的申请 / 待我审批 等审批工作台视图可回流
  -> 结果反馈包不丢失
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestRecordMaclawAppInstallRegistersApprovalAppWithDataSrv"
```

### 推进记录：App Studio 可视化保存审批 workflow install_ref（2026-06-28）

上一轮后端已经让 `PlanMaclawAppInstall` / `InstallMaclawAppDependencies` 支持 `install_ref`、`capability_id`、`hub_skill_id`、`raw_url`、`repo_url` 等精确安装引用，并把引用传给 `InstallMixedSkill(source, id, install_ref)`。本轮继续补齐 App Studio 设计态，避免用户在 Studio 中制作或编辑企业审批型 App 时只能看到 workflow Skill ID，无法把能力市场/企业 Hub/GitHub 的精确安装目标保存进应用信息文件。

已落地调整：

```text
App Studio 创建企业审批型 App
  -> 能力与依赖区域新增 Install ref 输入
  -> 保存到 Skill / 创建本地 App 时写入 dependencies.skills[].install_ref
  -> manifest 预览同步展示同一安装引用

App Studio 管理页编辑企业审批型 App
  -> 打开编辑弹窗时从 install_ref / installRef / capability_id / hub_skill_id / raw_url / repo_url 回填
  -> 保存时写回 dependencies.skills[].install_ref
  -> 用户清空 Install ref 时不再保留旧别名

依赖来源类型
  -> 前端 AppSkillDependency source 支持 local / hub / market / skillmarket / enterprise_hub / github / builtin
  -> Hub / SkillMarket / GitHub 搜索或手工填写的安装引用能进入同一份 MaClaw App manifest
```

对应全链路意义：

```text
App Studio 可视化设计审批型应用
  -> manifest 保存 workflow_skill id + source + install_ref
  -> 保存为超级 Skill 的 maclaw.app.json
  -> 上传能力市场时携带精确依赖声明
  -> 安装 MaClaw App 时后端按 install_ref 下载/安装依赖 Skill
  -> 安装审计和 DataSrv app_installations 能保留同一依赖证据
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "saves a newly created enterprise approval app into its app skill definition|edits approval workflow node mappings visually and persists them"
npm.cmd run build
```

### 推进记录：App Studio 发布预检按 install_ref 精确匹配依赖证据（2026-06-28）

上一轮 App Studio 已经能在创建和编辑企业审批型 App 时把 workflow Skill 的 `install_ref` 写入 `dependencies.skills[]`。本轮继续收口发布治理：仅靠 Skill id 判断依赖验证是否覆盖声明依赖是不够的，同名 Skill 可能来自企业 Hub、SkillMarket、GitHub 或不同 capability id。发布预检必须确认“声明的那个安装引用”已经被验证，而不是只验证了一个同名 Skill。

已落地调整：

```text
BackendAppInstallDependency
  -> 前端类型新增 install_ref / installRef
  -> parseBackendAppInstallDependencies 读取 install_ref / installRef / InstallRef

appSkillDependencies / governance dependencies
  -> 同名依赖合并时保留显式 dependencies.skills[].install_ref
  -> appGovernanceForManifest 导出的 dependencies.skills 保留 install_ref

App Studio Review / publish
  -> 依赖验证匹配从单纯 id 匹配升级为 id + kind + source + install_ref
  -> 声明依赖带 install_ref 时，验证证据必须带相同 install_ref
  -> 同 id 但 install_ref 不一致会显示 Needs work，并阻止 Submit for review
```

对应全链路意义：

```text
App Studio 设计依赖
  -> manifest 声明 workflow/app/runtime Skill 的 source + install_ref
  -> 测试或安装生成 dependencyVerification
  -> 发布预检确认验证证据覆盖同一个 install_ref
  -> 能力市场收到的治理依赖和依赖验证证据不再因同名 Skill 混淆
  -> Hub 下载重装时可按精确引用复现依赖安装
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "requires dependency verification to cover declared app Skill dependencies before publishing"
npm.cmd run build
```

### 推进记录：安装依赖明细展示 install_ref 审计证据（2026-06-28）

上一轮发布预检已经按 `install_ref` 精确匹配依赖验证证据。本轮继续补齐安装后的可见性：安装包预览、安装结果、最近安装记录、运行态依赖诊断共用同一套依赖明细组件，如果只显示 Skill id/version/source，用户仍然无法确认当前安装审计对应哪个 Hub capability、SkillMarket 条目或 GitHub URL。

已落地调整：

```text
InstallRecordDependencies
  -> 依赖 meta 增加 ref:<install_ref>
  -> title / hover 摘要也包含 ref:<install_ref>

backendDependencySummary
  -> installed/missing/unavailable 摘要保留 install_ref

市场包安装预览与安装结果
  -> dependencyVerification.dependencies[] 中的 install_ref 直接展示
  -> 安装后 result panel 能看到同一 ref
```

对应全链路意义：

```text
Hub / SkillMarket / GitHub 依赖安装
  -> 后端安装计划返回 install_ref
  -> 前端安装预览显示 install_ref
  -> 安装结果和最近安装记录保留 install_ref
  -> 运行态依赖诊断和发布复核可人工核对同一精确依赖
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "shows dependency verification in market package preview and install results"
npm.cmd run build
```

### 推进记录：DataSrv 依赖验证合同保留 install_ref（2026-06-28）

上一轮 GUI 已经能在安装预览、安装结果、最近安装记录和运行态依赖诊断里展示 `install_ref`。本轮继续把这个字段沉到 DataSrv 合同层，避免它只是前端/GUI 后端内部字段。企业 Hub capability id、SkillMarket install ref、GitHub raw URL 或 repo URL 都必须能随安装审计进入 DataSrv，并从 capabilities 回流给 App Studio。

已落地调整：

```text
DataSrv app_installations.metadata.dependency_verification.dependencies[]
  -> 归一化 installRef / InstallRef / capability_id / hub_skill_id / skill_id / raw_url / repo_url
  -> canonical 字段统一保存为 install_ref
  -> 删除 installRef 等 camelCase/别名字段，避免回流时字段漂移

DataSrv capabilities 回流
  -> dependency_verification.dependencies[].install_ref 原样返回
  -> GUI 可继续用于依赖诊断、发布预检和安装审计展示

OpenAPI
  -> app_installations.metadata.dependencies[] item schema 文档化 install_ref
  -> app_installations.metadata.dependency_verification.dependencies[] item schema 同步文档化 install_ref
```

对应全链路意义：

```text
App Studio / GUI 安装 MaClaw App
  -> 后端安装计划记录精确依赖 install_ref
  -> RecordMaclawAppInstall 注册到 DataSrv
  -> DataSrv 归一化为稳定 install_ref 合同
  -> capabilities / app-installations 回流
  -> App Studio 发布治理、运行态依赖诊断、Hub 重装可以使用同一精确依赖引用
```

已通过定向验证：

```text
go test ./structureddata -count=1 -timeout 60s -run "TestUpsertAppInstallationNormalizesTopLevelDependencyVerification|TestAppInstallationOpenAPISchemaDocumentsFullTestEvidence"
```

### 推进记录：GUI DataSrv App 回流保留依赖 install_ref（2026-06-28）

上一轮 DataSrv 已经能把依赖验证合同中的 `install_ref` 规范化保存并通过 capabilities 回流。本轮继续补齐 GUI 侧回流入口：从 DataSrv `app_installations` 生成可添加 App 候选时，显式依赖与从 `workflow_skill_ids` 推断出的审批工作流依赖会合并；此前归一化步骤只保留 id/version/kind/source/capabilities，导致 `metadata.dependencies[].install_ref` 在进入 App Studio 候选 manifest 时丢失。

已落地调整：

```text
normalizeAppDependencies
  -> 使用 appSkillDependencyInstallRef 统一识别 install_ref / installRef / capability_id / hub_skill_id / skill_id / raw_url / repo_url
  -> manifest.dependencies.skills[] 输出 canonical install_ref

mergeDataSrvInstalledDependencies
  -> 合并 DataSrv 显式依赖与 workflow_skill_ids 推断依赖时保留 install_ref
  -> 显式 DataSrv dependency_verification / metadata.dependencies 与推断 workflow dependency 指向同一 Skill id 时不会丢失精确安装引用
```

对应全链路意义：

```text
DataSrv app_installations.metadata.dependencies[]
  -> GUI capabilities 回流
  -> App Studio addable candidate manifest.dependencies.skills[].install_ref
  -> installEvidence.dependencies[].install_ref
  -> importedRunEvidence.dependencyVerification.dependencies[].install_ref
  -> 后续编辑、发布预检、Hub 重装继续按精确依赖引用推进
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "turns DataSrv installed MaClaw apps into addable app candidates with layout metadata"
npm.cmd run build
```

### 推进记录：RecordMaclawAppInstall 返回体补齐 package_sha256（2026-06-28）

前面已经让 DataSrv 冷启动安装证据和 GUI 最近安装记录兼容 `package_sha256`。继续复核 Hub 下载重装链路时发现，`maclawAppInstallRecord` 本地审计结构和 `ListMaclawAppInstalls` 输出使用 `package_sha256`，`InstallSelectedMaclawAppPackageFromHub` 外层安装结果也使用 `package_sha256`，但 `RecordMaclawAppInstall` 的即时返回体只有旧式 `package_sha`。这会让“刚安装完成的结果面板”和“刷新后的最近安装记录”字段名不完全一致。

已落地调整：

```text
RecordMaclawAppInstall 返回
  -> package_sha 保持兼容
  -> package_sha256 同步返回同一包指纹

Hub 安装结果
  -> install_record.package_sha256 可直接用于安装反馈、依赖复检 key、DataSrv 注册审计对照
```

对应全链路意义：

```text
粘贴包安装 / Hub 下载重装
  -> RecordMaclawAppInstall
  -> 即时安装结果 package_sha256
  -> ListMaclawAppInstalls package_sha256
  -> DataSrv app_installations.metadata.package_sha256
  -> GUI 回流 installEvidence.package_sha256
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "Test(RecordMaclawAppInstallPersistsNewestInstallAudit|InstallMaclawAppPackageFromHubDownloadsAndRecordsInstall|InstallSelectedMaclawAppPackageFromHubFiltersPackageApps)"
```

### 推进记录：DataSrv 注册与 GUI 回流保留 nested install_evidence（2026-06-28）

前面已经把安装证据拆成可查询摘要字段，并能从 DataSrv 散字段合成 `installEvidence`。本轮继续收紧 Hub 下载重装、DataSrv 注册、GUI 冷启动回流之间的证据一致性：只靠散字段可以恢复运行态，但缺少一份“安装时 per-app evidence 原包”，后续审计、重装对照和外部系统同步时容易出现字段口径漂移。

已落地调整：

```text
GUI 后端 RecordMaclawAppInstall -> DataSrv app_installations.metadata
  -> 新增 install_evidence
  -> 保存 per-app version_snapshot / dependencies / workspace_layout / workflow_mapping / workflow_contract / result_contract / test_evidence / dependency_verification

GUI 前端 DataSrv 回流
  -> dataSrvInstalledInstallEvidence 优先读取 metadata.install_evidence
  -> 散字段继续作为旧记录兼容兜底
  -> nested install_evidence.apps 可直接恢复到 AppEntry.installEvidence.apps

DataSrv OpenAPI
  -> app_installations.metadata.install_evidence 文档化为稳定字段
```

对应全链路意义：

```text
Hub 下载重装 / 粘贴包安装
  -> RecordMaclawAppInstall 生成 per-app install_evidence
  -> 注册 DataSrv app_installations.metadata.install_evidence
  -> DataSrv capabilities 回流
  -> GUI App 面板恢复同一份 installEvidence
  -> 后续复测、依赖复检、发布治理和审计回放共享同一证据原包
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestRecordMaclawAppInstallRegistersApprovalAppWithDataSrv"
npm.cmd test -- AppsPage.test.tsx -t "turns DataSrv installed MaClaw apps into addable app candidates with layout metadata"
npm.cmd run build
go test ./structureddata -count=1 -timeout 60s -run "TestAppInstallationOpenAPISchemaDocumentsFullTestEvidence"
```

### 推进记录：DataSrv 查询过滤纳入 nested install_evidence（2026-06-28）

上一轮已经把 `metadata.install_evidence` 作为 per-app 安装证据原包写入 DataSrv，并让 GUI 回流优先读取它。本轮继续补齐 DataSrv 自身查询能力：如果外部系统或 Hub 安装记录只保留 nested `install_evidence`，而没有展开成顶层 `test_evidence / workflow_mapping / result_contract / dependency_verification`，DataSrv 的 app-installations 查询也必须能按审批实例、结果类型、工作流节点、定义指纹和依赖健康过滤。

已落地调整：

```text
DataSrv app_installations metadata filter
  -> appInstallationTestEvidenceMaps 读取 install_evidence.test_evidence / installEvidence.testEvidence
  -> workflow_skill_id 过滤读取 nested approvalInstance 与 install_evidence.dependencies
  -> workflow_node 过滤读取 install_evidence.workflow_mapping 与 nested approvalInstance
  -> result_type 过滤读取 install_evidence.result_contract
  -> has_blocking_dependency / has_missing_required_dependency 过滤读取 install_evidence.dependency_verification
```

对应全链路意义：

```text
Hub / GUI 后端安装注册
  -> DataSrv metadata.install_evidence 保存证据原包
  -> /api/v1/data/app-installations 可按 nested 证据查询
  -> capabilities / GUI 回流不仅能显示证据，也能按审批、结果、依赖健康筛选已安装 App
```

已通过定向验证：

```text
go test ./structureddata -count=1 -timeout 60s -run "TestListAppInstallationsFilters(NestedInstallEvidence|ByDependencyHealth|ByApprovalInstanceEvidence)"
```

### 推进记录：审批发起 request 保留完整运行上下文（2026-06-28）

前面已经让 DataSrv 和 GUI 都能从审批实例、业务记录和 nested install evidence 中恢复当前节点、处理人、结果包与依赖证据。本轮继续检查“发起审批 -> 创建 DataSrv RecordApproval”这一入口：创建审批时顶层 payload 已经有 current_assignee、workflow_node_id、result_payload、outputs、artifacts 等字段，但 `request` 业务上下文只保留了基础 app/object 信息。外部审批系统、DataSrv 回流或后续审计如果主要读取 `request`，会缺少当前节点状态和结果包。

已落地调整：

```text
SyncMaclawAppApprovalInstanceToDataSrv create_record_approval request
  -> 保留 submitted_by / assigned_to / current_assignee / currentAssignee
  -> 保留 current_assignee_type / currentAssigneeType
  -> 保留 workflow_skill_id / workflowSkillId
  -> 保留 workflow_version / workflowVersion
  -> 保留 workflow_node_id / workflowNodeId / workflow_node_ids / workflowNodeIds
  -> 保留 from_status / fromStatus / to_status / toStatus
  -> 保留 business_status / businessStatus / result_status / resultStatus
  -> 保留 result_payload / resultPayload / outputs / artifacts
```

对应全链路意义：

```text
App 发起审批
  -> GUI 本地 pending 实例
  -> DataSrv create_record_approval
  -> request 本身可独立恢复当前节点、当前处理人、状态迁移、结果包
  -> GUI 后端/DataSrv/OpenAPI 的 request snake/camel 兼容读取有真实写入来源
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestSyncMaclawAppApprovalInstanceToDataSrv"
```

### 推进记录：DataSrv 审批泳道支持 request 业务身份别名（2026-06-28）

本轮继续补齐“企业审批型应用”的审批实例数据管理闭环。审批实例可能由工作流 Skill、外部审批系统或安装回流流程写入 DataSrv，此时顶层 `submitted_by / assigned_to / current_assignee` 不一定完整，但 `request` 中通常会携带业务身份上下文。现在 DataSrv 审批列表的工作台泳道会从这些 request 别名恢复用户相关性。

已落地调整：

```text
pending_my_approval
  -> 顶层 assigned_to / current_assignee
  -> request.assigned_to / request.assignedTo
  -> request.current_assignee / request.currentAssignee

my_requests
  -> 顶层 created_by / submitted_by
  -> request.submitted_by / request.submittedBy
  -> request.owner / request.applicant

handled
  -> 顶层 reviewed_by
  -> request.reviewed_by / request.reviewedBy
```

显式审计字段过滤仍保持精确语义：

```text
created_by=xxx 仍只匹配顶层 created_by
reviewed_by=xxx 仍只匹配顶层 reviewed_by
submitted_by / current_assignee / assigned_to 查询可兼容 request 中的业务别名
```

同时已覆盖显式查询场景：

```text
/api/v1/data/approvals?current_assignee=user_1
  -> 可命中 request.currentAssignee / request.assignedTo

/api/v1/data/approvals?submitted_by=user_1
  -> 可命中 request.submittedBy / request.applicant
```

对应全链路意义：

```text
审批工作流 Skill / 外部审批系统
  -> DataSrv record_approvals.request 写入业务身份上下文
  -> DataSrv /api/v1/data/approvals?lane=pending_my_approval|my_requests
  -> GUI 审批工作台“我的申请 / 待我审批”
  -> MacLaw App 审批型应用实例视图
```

已通过定向验证：

```text
go test ./structureddata -count=1 -timeout 60s -run "TestHTTPServerRecordApprovalsCarryMaClawAppSemantics"
go test ./structureddata -count=1 -timeout 60s -run "TestHTTPServerRequiresBearerTokenAndHandlesRecords|TestHTTPServerRecordApprovalsCarryMaClawAppSemantics"
```

### 推进记录：GUI 审批工作台显示 request-only 审批身份（2026-06-28）

上一轮 DataSrv 已支持从 `record_approvals.request` 的业务身份别名恢复“我的申请 / 待我审批”。本轮继续补 GUI 前端显示层，确保这些由 DataSrv 或工作流 Skill 恢复出来的身份不是只停留在过滤逻辑里，而是在审批实例列表和详情里可见。

已落地调整：

```text
ApprovalInstanceView
  -> 新增 applicant 字段

backendApprovalInstanceToView
  -> applicant 优先读取 applicant / submitted_by / submittedBy / owner
  -> owner 缺失时回退 applicant
  -> approver 缺失时回退 current_assignee / currentAssignee

ApprovalManager / ApprovalWorkspace 列表行
  -> 显示申请人
  -> 显示当前处理人
  -> 显示状态流转

ApprovalManager / ApprovalWorkspace 详情事实栏
  -> 字段名从泳道语义“我的申请 / 待我审批”改为业务语义“申请人 / 审批人”
  -> 保留当前处理人、处理人类型、状态流转、审批结果
```

对应全链路意义：

```text
审批工作流 Skill / 外部审批系统
  -> DataSrv request.submittedBy / request.applicant / request.currentAssignee
  -> GUI 后端 ListMaclawAppApprovalInstances*
  -> GUI 前端 ApprovalManager / ApprovalWorkspace
  -> 用户在审批型 App 中可直接看到申请人、审批人、当前处理人和当前节点状态
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "shows the approval instance workspace for approval apps|keeps approval result package"
npm.cmd run build
```

### 推进记录：MacLaw App 依赖安装支持精确 install_ref（2026-06-28）

MacLaw App 本质上是带 App 元数据、动态 UI、结果契约和依赖 Skill 的“超级 Skill”。安装 App 时不能只知道依赖 Skill 的名称，还需要把能力市场、企业 Hub 或 GitHub 返回的精确安装引用传给底层 Skill 安装器，否则同名 Skill、多来源 Skill、企业 capability id、GitHub raw URL 等场景会出现定位不准。

本轮补齐依赖安装引用链路：

```text
maclawAppInstallPlanDependency
  -> 新增 install_ref

依赖声明解析
  -> install_ref / installRef
  -> capability_id / capabilityID
  -> hub_skill_id / hubSkillID
  -> skill_id / skillID
  -> raw_url / rawURL
  -> repo_url / repoURL

InstallMaclawAppDependencies
  -> 调用 InstallMixedSkill(source, id, install_ref)
  -> enterprise_hub 可用 capability id 精确安装
  -> github 可用 raw_url/repo 引用安装
  -> skillmarket/skillhub 仍可按 id 安装，同时保留 install_ref 作为审计证据

安装计划/安装证据/DataSrv metadata
  -> dependencies[] 保留 install_ref
  -> dependency_verification.dependencies[] 保留 install_ref
```

对应全链路意义：

```text
能力市场 App 包
  -> App dependencies.skills[] 声明依赖 Skill + install_ref/capability_id
  -> GUI 后端 PlanMaclawAppInstall 生成权威依赖计划
  -> InstallMaclawAppDependencies 自动下载安装依赖 Skill
  -> RecordMaclawAppInstall / DataSrv app-installations 保存依赖验证证据
  -> App Runtime 运行时具备真实动作 Skill 与审批 workflow Skill
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestMaclawAppInstallSkillSourceNormalizesHubAndMarket|TestInstallMaclawAppDependenciesInstallsHubBackedSources|TestInstallMaclawAppDependenciesSkipsInstalledAndBlocksUnsupportedSource"
```

### 推进记录：GUI 前端审批实例回流兼容 snake/camel 合同（2026-06-28）
本轮继续补齐企业审批型应用的实例数据管理闭环。GUI 后端和 DataSrv 回流的审批实例可能来自 Go JSON、DataSrv metadata、Hub 安装证据或外部企业系统，字段命名既可能是 `snake_case`，也可能是 `camelCase`。如果前端只识别一种命名，审批管理页会丢失应用 ID、审批 ID、当前节点路径、当前处理人、结果包和业务记录，直接影响“我的申请 / 我审批的 / 全部审批实例”的可视化。

已落地调整：

```text
BackendApprovalInstance
  -> 兼容 appID / appName / blueprintID / datasetID / objectRole
  -> 兼容 instanceID / instanceId / approvalId / recordId
  -> 兼容 currentNode / workflowNodeIDs
  -> 兼容 submittedBy / currentAssignee / currentAssigneeType
  -> 兼容 workflowSkillID / workflowSkillId / workflowVersion
  -> 兼容 detailURL / resultPayload

backendApprovalInstanceToView
  -> snake_case 与 camelCase 统一归一化到 ApprovalInstanceView
  -> current_node_ids / workflowNodeIDs / currentNode 合并为稳定节点路径
  -> result_payload / resultPayload 统一进入 resultPayload
  -> outputs / artifacts 继续按标准结果包展示
  -> recordID 可从显式字段或 resultPayload 中恢复
```

对应全链路意义：

```text
DataSrv 审批实例 / Hub 安装证据 / 外部企业系统回流
  -> GUI 后端返回审批实例
  -> GUI 前端 ApprovalManager / ApprovalWorkspace 稳定展示
  -> 用户能看到当前节点、当前处理人、审批结果、业务状态、业务记录和文件产物
  -> “我的申请 / 我审批的 / 已处理 / 需关注”不再依赖单一 JSON 命名风格
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "opens global approval management from the operation section"
npm.cmd test -- AppsPage.test.tsx -t "shows the approval instance workspace for approval apps"
npm.cmd run build
```

### 推进记录：手工审批决策同步写入标准结果包（2026-06-28）
本轮继续检查“审批实例数据管理 -> 审批结果反馈”的运行闭环。此前用户在全局审批管理或单个 App 工作台里手工点击通过、拒绝、需关注时，外层 `business_status / result_status` 会更新，但 `result_payload` 只是沿用原实例里的旧结果包；如果旧包里仍是 `approval_pending`，Hub/DataSrv/审计回放看到的结果包会和实例状态不一致。

已落地调整：

```text
approvalDecisionResultPayload
  -> 保留原 resultPayload 中的 business_record / content / 自定义字段
  -> 写入 approval_result
  -> 写入 approval_status
  -> 写入 decision
  -> 写入 business_status
  -> 写入 result_status

全局 ApprovalManager 手工决策
  -> result_payload 使用合并后的审批结果包
  -> outputs / artifacts 原样保留
  -> DataSrv sync payload 携带同一结果包

App 工作台 ApprovalWorkspace 手工决策
  -> 通过 / 拒绝 / 需关注共用同一结果包合并逻辑
  -> 原业务记录、输出块和文件产物不丢失
```

对应全链路意义：

```text
用户手工审批
  -> GUI 本地审批实例立即更新
  -> result_payload 与实例 status 对齐
  -> outputs / artifacts 继续作为结果反馈展示
  -> DataSrv 审批实例同步和后续审计回放读取同一标准结果包
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "keeps approval result package|shows the approval instance workspace"
npm.cmd run build
```

### 推进记录：DataSrv/GUI 回流对象型结果交付合同（2026-06-28）
本轮继续补齐“应用输出结果有哪些、如何交付”的公开合同。App Studio 和前端 manifest 已经把 `result_contract.delivery` 设计成对象，用 `inlineContent / artifacts / businessRecord / notifications` 表示文本、文件、业务记录和通知交付能力；但 DataSrv 的摘要字段仍主要是旧式字符串 `result_contract_delivery`。外部 Hub、企业系统或生成客户端如果只保存摘要字段，GUI 回流时可能无法恢复完整 delivery 开关。

已落地调整：

```text
DataSrv normalizeAppInstallationResultContractMetadata
  -> 保留旧式 result_contract_delivery 字符串摘要
  -> 识别对象型 result_contract.delivery
  -> 归一化 inline_content -> inlineContent
  -> 归一化 business_record -> businessRecord
  -> 派生 result_contract_delivery_modes
  -> 派生 result_contract_delivery_inline_content
  -> 派生 result_contract_delivery_artifacts
  -> 派生 result_contract_delivery_business_record
  -> 派生 result_contract_delivery_notifications
  -> 安装审计 metadata 同步记录这些摘要字段

DataSrv OpenAPI
  -> app_installations.metadata 公开上述 delivery 摘要字段

GUI DataSrv capabilities 回流
  -> 当 result_contract.delivery 对象缺失时，从 delivery modes/布尔摘要恢复完整 delivery 开关
  -> 旧式 download/document/artifact/content/business_record/notification 摘要继续兼容
```

对应全链路意义：

```text
App Studio 设计输出交付方式
  -> 安装/注册到 DataSrv 时保留对象型 result_contract.delivery
  -> DataSrv 生成可审计、可回流的扁平摘要
  -> Hub/企业系统即使只保存摘要字段也能表达交付能力
  -> GUI 回流已安装 App 后仍能正确展示文本、文件、业务记录和通知交付开关
```

已通过定向验证：

```text
go test ./structureddata -count=1 -timeout 60s -run "TestUpsertAppInstallationNormalizes(ResultContractDeliveryObject|GovernanceResultContract)|TestAppInstallationOpenAPISchemaDocumentsFullTestEvidence"
npm.cmd test -- AppsPage.test.tsx -t "restores DataSrv installed enterprise normal app run evidence"
npm.cmd run build
```

### 推进记录：GUI 后端回流 DataSrv 审批实例 request camelCase 业务上下文（2026-06-28）
本轮继续补齐企业审批型应用的审批实例数据管理链路。前端已经兼容 DataSrv/Hub/外部企业系统返回的 snake_case 与 camelCase 审批实例字段，但 GUI 后端从 DataSrv `record_approvals.request` 映射为 `maclawAppApprovalInstance` 时，业务对象、审批事件、业务上下文和详情链接仍主要读取 snake_case。外部企业系统常写入 `objectRole / approvalEvent / businessEntity / businessAction / businessNote / detailURL`，如果后端不识别，前端即使支持 camelCase 也拿不到这些字段。

已落地调整：

```text
maclawAppApprovalInstanceFromRecordApproval
  -> objectRole / businessObjectRole 作为 object_role fallback
  -> approvalEvent / triggerEvent 作为 approval_event fallback
  -> detailURL / detailUrl 作为 detail_url fallback
  -> businessEntity / businessAction / businessNote 作为业务上下文 fallback
  -> 保持原有 submittedBy/currentAssignee/currentAssigneeType 等别名兼容

DataSrv 审批实例回流测试
  -> request 使用 camelCase 业务上下文字段
  -> GUI 后端恢复 objectRole / approvalObjectRole / approvalEvent / businessEntity / businessAction / businessNote / detailURL
```

对应全链路意义：

```text
外部企业系统或 DataSrv record_approvals
  -> request 使用 camelCase 业务上下文
  -> GUI 后端恢复完整审批实例
  -> GUI 前端 ApprovalManager / ApprovalWorkspace 展示业务对象、审批事件、当前节点和结果反馈
  -> 后续手工审批与 DataSrv sync 回写不丢失业务上下文
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestListMaclawAppApprovalInstancesAllLoadsDataSrvLane|TestListMaclawAppApprovalInstancesAllInfersDataSrvLanesForCurrentUser|TestListMaclawAppApprovalInstancesMergesDataSrvWithLocal"
```

### 推进记录：DataSrv OpenAPI 明确审批 request 业务上下文合同（2026-06-28）
上一轮 GUI 后端已经能从 DataSrv `record_approvals.request` 里恢复 camelCase 业务上下文。本轮继续把同一合同前移到 DataSrv OpenAPI。此前审批创建和审批详情/列表响应里的 `request` 只是泛 `object`，外部企业系统、Hub 或生成客户端不知道哪些字段是 MaClaw App 审批实例的稳定上下文，容易漏写 `objectRole / approvalEvent / businessEntity / currentAssignee / detailURL` 等字段。

已落地调整：

```text
DataSrv OpenAPI recordApprovalRequestOpenAPISchema
  -> approval_instance_id / approvalInstanceId / workflowInstanceId
  -> app_id / appID / maclaw_app_id
  -> blueprint_id / blueprintID
  -> object_role / objectRole / business_object_role / businessObjectRole
  -> approval_event / approvalEvent / trigger_event / triggerEvent
  -> business_entity / businessEntity
  -> business_action / businessAction
  -> business_note / businessNote
  -> submitted_by / submittedBy / owner / applicant
  -> assigned_to / assignedTo
  -> current_assignee / currentAssignee
  -> current_assignee_type / currentAssigneeType
  -> reviewed_by / reviewedBy
  -> current_node / currentNode / workflow_node / workflowNode
  -> current_node_ids / currentNodeIDs / workflow_node_ids / workflowNodeIDs
  -> workflow_skill_id / workflowSkillID / workflowSkillId
  -> workflow_version / workflowVersion
  -> approval_workflow_id / approvalWorkflowID
  -> detail_url / detailURL / detailUrl

OpenAPI 使用范围
  -> POST /api/v1/data/datasets/{datasetId}/records/{recordId}/approvals request.request
  -> GET /api/v1/data/approvals items[].request
  -> GET /api/v1/data/approvals/{approvalId} response.request
```

对应全链路意义：

```text
App Studio / Hub / 企业系统创建审批实例
  -> DataSrv OpenAPI 明确 request 业务上下文字段
  -> GUI 后端按同一 snake/camel 合同恢复审批实例
  -> GUI 前端 ApprovalManager / ApprovalWorkspace 展示我的申请、我审批的、当前节点、业务对象、审批事件和结果反馈
  -> 审批实例同步回 DataSrv 时上下文不丢失
```

已通过定向验证：

```text
go test ./structureddata -count=1 -timeout 60s -run "TestHTTPServerRequiresBearerTokenAndHandlesRecords"
```
### 推进记录：App Studio 发布治理复用安装测试证据（2026-06-28）
上一轮已经把 DataSrv / GUI 后端的普通应用运行结果统一为标准结果包。本轮继续补齐 App Studio 发布链路中的冷启动场景：当本地运行历史为空、App 对象也没有 `importedRunEvidence`，但安装记录里已经保存了 `installEvidence.test_evidence` 时，发布治理应当能复用这份安装时验证过的测试证据。

已落地调整：

```text
AppsPage
  -> 新增 installEvidenceImportedRunEvidence(app)
  -> 将 installEvidence.test_evidence 归一为 AppRunHistoryEntry
  -> 复用 installEvidence.dependency_verification / test_evidence.dependency_verification
  -> 保留 result_payload / outputs / artifacts / result_coverage / approval_instance
  -> latestAppRunEvidence 增加安装证据 fallback
  -> latestAvailableAppRunEvidence 增加安装证据 fallback
  -> runEvidenceMatchesCurrentDefinition 改为类型谓词并继续强制 definitionHash 匹配
```

关键约束：

```text
安装测试证据必须带 definitionHash / definition_fingerprint
  -> 与当前 appDefinitionFingerprint(app) 一致才可用于发布
  -> 避免用户调整动态界面布局或结果合同后，旧安装证据继续误放行
```

对应全链路意义：

```text
Hub / DataSrv 安装记录
  -> installEvidence.test_evidence
  -> App Studio 发布治理 testEvidence
  -> 企业能力市场提交 payload
  -> 冷启动后的 App 仍可复用安装时的测试、依赖、结果合同证据
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "install evidence test records"
npm.cmd test -- AppsPage.test.tsx -t "keeps imported DataSrv dependency evidence"
npm.cmd test -- AppsPage.test.tsx -t "requires run evidence to cover the declared primary result contract"
npm.cmd test -- AppsPage.test.tsx -t "requires dependency verification to cover declared app Skill dependencies before publishing"
npm.cmd run build
```
### 推进记录：安装依赖验证证据写入 verified_at（2026-06-28）
上一轮补齐了 App Studio 冷启动发布时复用 `installEvidence.test_evidence` 的链路。本轮继续检查 Hub 安装 -> 依赖检查 -> 安装记录 -> DataSrv 注册 -> 前端回流这条证据链，发现 generated dependency verification 没有明确验证时刻，前端归一化时会用读取时的当前时间兜底，导致证据时间不再代表安装时实际依赖状态。

已落地调整：

```text
maclawAppDependencyVerificationMetadataForEntry
  -> 对自动生成的 maclaw.app.install_plan.v1 dependency_verification 写入 verified_at
  -> verified_at 使用安装/注册时刻 RFC3339 UTC 时间

maclawAppInstallEvidenceByApp
  -> install_evidence[app_id].dependency_verification 保留 verified_at

maclawAppDataSrvInstallationPayloads
  -> metadata.dependency_verification 保留 verified_at
  -> metadata.test_evidence_dependency_verified_at 与 dependency_verification.verified_at 对齐
```

对应全链路意义：

```text
Hub 安装依赖检查
  -> generated dependency_verification.verified_at
  -> 本地 install registry / install_record
  -> DataSrv app-installations.metadata
  -> GUI capabilities 冷启动恢复 installEvidence
  -> App Studio 发布治理和依赖健康诊断
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestMaclawAppInstallEvidenceGeneratesDependencyVerification|TestMaclawAppDataSrvInstallationPayloadsScopeDependenciesPerApp"
go test ./gui -count=1 -vet=off -run "TestRecordMaclawAppInstallPersistsNewestInstallAudit|TestRecordMaclawAppInstallRegistersApprovalAppWithDataSrv"
```
### 推进记录：单 App 安装证据保留包身份字段（2026-06-28）
上一轮补齐了 generated dependency verification 的 `verified_at`。本轮继续检查前端从安装审计中拆分单个 App `installEvidence` 的路径，发现 `installEvidenceRecordForApp` 只保留了 layout/result/workflow/test/dependency 证据，未把安装审计的包身份字段带入 AppEntry，导致 App Studio 冷启动和发布治理能看到测试证据，但无法从 App 自身证据里追溯安装包来源与摘要。

已落地调整：

```text
installEvidenceRecordForApp
  -> 保留 schema
  -> 保留 package_sha / package_sha256
  -> 保留 source / installed_at
  -> 保留 apps 摘要，并按当前 app_id 过滤
  -> 保留 app_count
  -> 对 install_evidence[app_id] 子证据缺失的包字段，从 top-level install_record 回填
```

对应全链路意义：

```text
Hub / market / local install_record
  -> install_evidence[app_id]
  -> AppEntry.installEvidence
  -> localStorage 冷启动恢复
  -> App Studio 证据面板 / 发布治理 / 后续重新提交
  -> 可追溯 package sha、来源、安装时间、App 摘要
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "restores single app install evidence from top-level market install records"
npm.cmd test -- AppsPage.test.tsx -t "keeps dependency verification visible after single market app install"
npm.cmd test -- AppsPage.test.tsx -t "install evidence test records"
npm.cmd run build
```
### 推进记录：DataSrv 冷启动安装证据保留 package_sha256（2026-06-28）
上一轮让单 App 安装证据从 top-level `install_record` 回填包身份字段。本轮继续检查 DataSrv capabilities 的冷启动恢复路径，发现 `dataSrvInstalledInstallEvidence` 会把 `metadata.package_sha256` 折叠到 `package_sha`，但没有保留 `package_sha256` 字段本身；这会让 Hub/本地安装记录与 DataSrv 安装记录在前端 `AppEntry.installEvidence` 上不完全一致。

已落地调整：

```text
dataSrvInstalledInstallEvidence
  -> package_sha 保留 metadata.package_sha 优先，metadata.package_sha256 兜底
  -> package_sha256 保留 metadata.package_sha256 优先，metadata.package_sha 兜底
  -> source / installed_at / apps / app_count 继续保留
```

对应全链路意义：

```text
DataSrv app-installations.metadata.package_sha256
  -> AppEntry.installEvidence.package_sha256
  -> localStorage 冷启动恢复
  -> App Studio 证据面板 / 最近安装记录 / 发布治理
  -> 与 Hub / local install_record 的包身份字段一致
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "turns DataSrv installed MaClaw apps into addable app candidates with layout metadata"
npm.cmd test -- AppsPage.test.tsx -t "restores single app install evidence from top-level market install records"
npm.cmd run build
```
### 推进记录：DataSrv 审批实例兼容 request assignee 别名（2026-06-28）
上一轮继续对齐 DataSrv 冷启动安装证据字段。本轮转到企业审批型应用的实例管理链路，检查 DataSrv `/api/v1/data/approvals` 返回的远程审批实例如何映射到 GUI 的“我的申请 / 我审批的 / 当前节点状态”。发现当前映射主要读取顶层 `current_assignee/current_assignee_type/assigned_to`；如果 DataSrv 或旧记录把这些字段放在 `request` 内，GUI 后端会丢失当前处理人，`pending_my_approval` lane 推断也可能不准。

已落地调整：

```text
maclawAppApprovalInstanceFromRecordApproval
  -> CurrentAssignee 兼容 request.current_assignee / currentAssignee / assigned_to / assignedTo
  -> CurrentAssigneeType 兼容 request.current_assignee_type / currentAssigneeType / assigned_to_type / assignedToType
  -> Approver 兼容 request 中的 assigned/current/reviewed aliases
  -> Owner 兼容 request.submitted_by / submittedBy

normalizeMaclawAppApprovalLaneForRecordApproval
  -> my_requests 推断兼容 request.submitted_by / submittedBy
  -> pending_my_approval 推断兼容 request.current_assignee / assigned_to aliases
  -> handled 推断兼容 request.reviewed_by / reviewedBy
```

对应全链路意义：

```text
DataSrv approvals
  -> GUI 后端远程审批实例映射
  -> ApprovalWorkspace / ApprovalManager
  -> “我的申请”“我审批的”“已处理”“需关注” lane
  -> 当前节点、当前处理人、处理人类型稳定显示
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestListMaclawAppApprovalInstancesAllInfersDataSrvLanesForCurrentUser"
go test ./gui -count=1 -vet=off -run "TestListMaclawAppApprovalInstancesAllLoadsDataSrvLane|TestListMaclawAppApprovalInstancesMergesDataSrvWithLocal|TestMaclawAppApprovalInstancesPersistAndFilter"
```

### 推进记录：GUI 最近安装记录兼容后端 package_sha256（2026-06-28）

本轮继续收紧 “Hub 下载重装 / 本地安装审计 / GUI 最近安装记录 / 依赖复检” 的字段合同。后端 `ListMaclawAppInstalls` 返回的是 `maclawAppInstallRecord.PackageSHA`，JSON 字段为 `package_sha256`；而前端 `BackendAppInstallRecord`、最近安装记录 key 和 UI 展示主要读取 `package_sha`。这会导致从真实后端安装记录列表回流时，Package SHA 展示为空，依赖复检 key 也缺少包哈希维度。

已落地调整：

```text
AppsPage BackendAppInstallRecord
  -> 新增 package_sha256 字段

最近安装记录
  -> installRecordPackageSHA(record) 同时读取 package_sha / package_sha256
  -> installRecordKey 使用统一包哈希
  -> UI Package SHA 展示使用统一包哈希

前端测试
  -> recent install records 测试改用真实后端字段 package_sha256
  -> 继续验证市场面板显示 Package SHA、依赖数量和 blocking 依赖
```

对应全链路意义：

```text
RecordMaclawAppInstall / ListMaclawAppInstalls
  -> package_sha256
  -> GUI 最近安装记录稳定展示包哈希
  -> 依赖检查/修复状态 key 能绑定到具体安装包
  -> Hub 下载重装、App Studio 复检、安装审计回放使用一致字段
```

已通过验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "shows recent app install records in the market pane"
npm.cmd run build
```

### 推进记录：Hub 选择安装返回包级 App 摘要（2026-06-28）

本轮继续把 “Hub 下载重装 -> 安装记录 -> GUI 回流” 的证据链收紧。此前 `InstallSelectedMaclawAppPackageFromHub` 已经能够下载 Hub 包、按选择的 App 裁剪子包、安装依赖、调用 `RecordMaclawAppInstall` 写入安装审计，并返回 `install_record`。但 `RecordMaclawAppInstall` 的返回结构缺少包级 `apps` 摘要，前端安装记录 key、安装反馈和包内 App 展示已经按 `BackendAppInstallRecord.apps` 建模；缺这个字段时，Hub 子包安装结果虽然有 `app_ids`，但安装审计本身不能直接说明“这份 install_record 对应哪些 App”。

已落地调整：

```text
RecordMaclawAppInstall 返回
  -> 新增 apps: plan.Apps
  -> 每个 app 摘要包含 id / name / kind / schema

InstallSelectedMaclawAppPackageFromHub
  -> install_record.apps 跟随被裁剪后的安装包
  -> 选择安装单个 App 时，install_record.apps 只包含被选中的 App
  -> 未选择的同包 App 不会进入安装审计摘要
```

对应全链路意义：

```text
Hub 能力市场下载包
  -> 用户选择安装其中一个或多个 MaClaw App
  -> 后端裁剪安装包并记录安装审计
  -> install_record.apps 精确描述本次安装范围
  -> GUI 安装反馈、最近安装记录、依赖复检和后续 DataSrv 回流可以使用同一份包级 App 摘要
```

已通过验证：

```text
go test ./gui -count=1 -vet=off -run "TestInstallSelectedMaclawAppPackageFromHubFiltersPackageApps|TestInstallMaclawAppPackageFromHubDownloadsAndRecordsInstall|TestRecordMaclawAppInstallPersistsNewestInstallAudit"
```

### 推进记录：DataSrv 审批实例响应契约与安装依赖过滤收口（2026-06-28）

本轮继续从 “maclaw data srv 到 maclaw app 全链路” 角度补齐企业审批型应用的远端回流契约。前面已经让 GUI 后端可以把审批实例同步到 DataSrv，并把 `result_payload / outputs / artifacts` 等结果包读回 GUI；本轮把 DataSrv 的公开 OpenAPI 响应也补齐，避免 App Studio、Hub 能力市场、运行时只靠隐含 JSON 字段对接。

已落地调整：

```text
DataSrv OpenAPI
  -> /api/v1/data/approvals GET 200 items 明确声明审批实例 schema
  -> /api/v1/data/approvals/{approvalId} GET 200 明确声明审批实例 schema
  -> 审批实例 schema 覆盖 app_id / blueprint_id / object_role
  -> 覆盖 approval_workflow_id / workflow_skill_id / workflow_version / workflow_instance_id
  -> 覆盖 workflow_node_id / workflow_node_ids / workflow_decision_id
  -> 覆盖 current_assignee / current_assignee_type / submitted_by / trigger_event
  -> 覆盖 business_status / result_status / result_payload / outputs / artifacts

DataSrv App Installation 依赖过滤
  -> has_blocking_dependency / has_missing_required_dependency 过滤继续支持规范 dependency_verification
  -> 若没有顶层规范 dependency_verification，则使用安装态摘要 has_blocking_dependency
  -> 测试证据 dependency_verification 不再错误覆盖安装态依赖阻断
  -> capabilities 测试不再假设只有一个 App 或依赖列表顺序
```

对应全链路意义：

```text
企业审批型应用
  -> 发起数据与审批实例进入 DataSrv
  -> 审批工作流 Skill 运行并回写当前节点、当前处理人、工作流实例与决策 ID
  -> 我的申请 / 待我审批 / 我审批的 可从 DataSrv 列表稳定恢复
  -> 审批结果、文档、文本内容、输出块、附件统一通过 result_payload / outputs / artifacts 回流
  -> App 安装与能力市场查询可以按真实安装态依赖阻断进行过滤，而不是被旧测试证据误判
```

已通过验证：

```text
go test ./structureddata -count=1 -timeout 120s -run "TestHTTPServerRequiresBearerTokenAndHandlesRecords"
go test ./structureddata -count=1 -timeout 120s -run "Test(ListAppInstallationsFiltersByDependencyHealth|HTTPServerAppInstallationsOverrideObjectRoleBindings)"
go test ./structureddata -count=1 -timeout 180s
```
### 推进记录：App Studio 本地运行自动生成 result coverage（2026-06-28）
前面已经把 `result_contract`、DataSrv 标准结果包、导入/安装回流的测试证据和发布治理打通。本轮继续补齐 App Studio 本地试运行这一环：用户在设计应用后点击运行，不再只是保存 `result_payload / outputs / artifacts`，还会在运行历史里自动生成 `resultCoverage`，作为后续测试、上传和 Hub 审核的直接证据。

已落地调整：

```text
AppPreview.recordRunHistory
  -> 对 status=done 的本地运行记录自动调用 appRunEvidenceContractCoverage
  -> 写入 resultCoverage.ok
  -> 写入 resultCoverage.primary
  -> 写入 resultCoverage.coveredTypes
  -> 写入 resultCoverage.missingTypes

覆盖范围
  -> 工具型应用本地运行
  -> 企业普通应用 DataSrv/business operation 运行
  -> 企业审批型应用审批 workflow skill 运行
```

对应全链路意义：

```text
App Studio 设计/调试
  -> 本地运行历史生成 result coverage
  -> 发布前置检查直接消费同一份证据
  -> 后端提交审核继续按 resultCoverage.missingTypes 拦截
  -> Hub / DataSrv 安装回流可以复用同一证据结构
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "renders enterprise normal apps as business workspaces without approval instances"
npm.cmd test -- AppsPage.test.tsx -t "requires run evidence to cover the declared primary result contract"
```
### 推进记录：本地运行依赖验证证据进入发布包（2026-06-28）
上一轮已经让 App Studio 发布预检要求 `dependencyVerification` 覆盖声明 Skill。继续检查后发现一个细节断点：本地设计阶段的 App 可能还没有市场安装审计记录，但试运行前已经执行过运行时依赖检查，并把结果保存在 run history 的 `dependencyVerification` 中。此前发布预检/打包主要读取 `installEvidence.dependency_verification`，导致“试运行已经验证依赖”这份证据没有稳定进入发布包。

本轮补齐该链路：

```text
App Studio 本地试运行
  -> 运行前 PlanMaclawAppInstall / runtime dependency check
  -> recordRunHistory 保存 dependencyVerification
  -> 发布预检 fallback 读取最新成功运行证据中的 dependencyVerification
  -> appGovernanceForManifest 写入 governance.dependencyVerification
  -> Submit for review 打包时若后端即时 plan 缺少依赖详情，回退到本地已验证证据
```

对应全链路意义：

```text
本地制作 App
  -> 测试运行证明依赖 Skill 已安装/可用
  -> 上传能力市场时携带同一份依赖验证证据
  -> 后端 SubmitMaclawAppPackage 继续按依赖覆盖规则审核
  -> Hub/能力市场获得可审计的 Skill 依赖安装/验证结果
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "requires dependency verification to cover declared app Skill dependencies before publishing"
```
### 推进记录：审批实例 pending 状态同时支撑我的申请和待我审批视图（2026-06-28）
企业审批型应用需要用户能看到“我的申请”和“我审批的/待我审批”。此前本地审批实例过滤严格依赖实例 lane：发起节点创建的 pending 实例通常 lane=`my_requests`，因此能出现在“我的申请”，但如果 DataSrv 远端还未回流或离线使用，本地 `pending_my_approval` 视图可能看不到同一个待审批实例。

本轮补齐本地审批实例视图规则：

```text
ListMaclawAppApprovalInstances
  -> my_requests 继续按 lane=my_requests 显示发起人的申请
  -> pending_my_approval 对 status=pending 且存在 currentAssignee/approver 的实例放行
  -> handled / attention 继续按状态优先，避免已审批实例留在 pending
```

对应全链路意义：

```text
企业审批型应用
  -> 发起节点产生审批实例
  -> 本地 registry 立即可见我的申请
  -> 同一 pending 实例也可进入待我审批视图
  -> DataSrv 远端回流/合并后继续按状态、当前节点、处理人呈现
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestMaclawAppApproval(PendingRequestAlsoAppearsForApprover|InstancesPersistAndFilter)"
go test ./gui -count=1 -vet=off -run "TestListMaclawAppApprovalInstances(All|Merges|Maps)"
```
### 推进记录：App Studio 发布预检要求依赖验证覆盖声明 Skill（2026-06-28）
MaClaw App 作为“带特殊数据的超级 Skill”，安装和发布时必须处理它依赖的其它 Skill。后端 `SubmitMaclawAppPackage` 已经要求 `governance.dependencyVerification` 覆盖每个声明的 app/runtime/workflow Skill，并且必需依赖不能 missing/blocked；此前前端发布面板只检查“是否有依赖声明”，可能出现 App Studio 显示 Ready、提交后被后端审核打回。

本轮把同一门禁前移到 App Studio 发布预检：

```text
App Studio Review / publish
  -> 读取 manifest 中声明的 appSkill / runtime skill / workflow skill / dependencies.skills
  -> 读取安装或依赖检查留下的 dependencyVerification
  -> 校验 schema = maclaw.app.install_plan.v1
  -> 校验 verification.dependencies 覆盖每个声明 Skill
  -> 校验 required Skill 不是 missing / blocked / unhealthy
  -> 校验 workflow contract issue 与 governance review issue 均未阻断
```

对应全链路意义：

```text
App 设计时声明 Skill 依赖
  -> 安装或测试时生成依赖验证证据
  -> 发布面板先拦截未验证/验证不完整的 App
  -> SubmitMaclawAppPackage 后端审核继续兜底
  -> 能力市场不会收到依赖声明和依赖验证不一致的 App 包
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "requires dependency verification to cover declared app Skill dependencies before publishing"
npm.cmd test -- AppsPage.test.tsx -t "requires run evidence to cover the declared primary result contract"
```

### 推进记录：DataSrv OpenAPI 明确安装版本快照合同（2026-06-28）

本轮继续从“安装 MaClaw App 时要检查并安装 Skill 依赖，之后还能复测、审计、回流”的角度检查公开合同。GUI 前端已经能从 DataSrv metadata 恢复 `version_snapshot`，包括 App 自身 super Skill、审批 workflow Skill、approval binding 的版本信息；但 OpenAPI 只把它描述成普通 object，外部企业系统或 Hub 对接时不清楚应该稳定写入哪些字段。

已落地调整：

```text
DataSrv OpenAPI app_installations.metadata.version_snapshot
  -> description 明确这是 install-time dependency version snapshot
  -> app_entry_version 保存应用信息版本
  -> app_skill 保存 MaClaw App 作为 super Skill 的 id / version / kind / source
  -> workflow_skills 保存依赖 workflow Skill 的 id / version / kind / source
  -> approval_bindings 保存 event / object_role / workflow_skill_id / workflow_version
```

对应全链路意义：

```text
安装 MaClaw App
  -> 检查并安装依赖 Skill
  -> DataSrv 保存实际解析到的版本快照
  -> capabilities / app-installations 回流给 GUI
  -> App Studio 复测、发布治理、运行诊断可定位当时的 App Skill 与审批 workflow Skill 版本
```

已通过定向验证：

```text
go test ./structureddata -count=1 -timeout 60s -run "TestAppInstallationOpenAPISchemaDocumentsFullTestEvidence"
```

### 推进记录：发布测试证据绑定当前 App 定义（2026-06-28）

本轮从全链路发布视角复核了 `definitionHash` 门禁：MaClaw App 作为“超级 Skill”，其测试证据不能只证明曾经运行成功，还必须证明运行的是当前 App 定义。当前定义包括基础信息、动态 UI 布局、依赖、结果合约、测试协议和审批工作流映射等运行面合同。

已确认后端治理审查具备两类硬门禁：

```text
SubmitMaclawAppPackage
  -> maclawAppGovernanceReviewIssuesFromPackage
  -> maclawAppDefinitionHashReviewIssue
  -> 缺少 definitionHash：记录 error 审核问题
  -> definitionHash 与当前包定义指纹不一致：记录 error 审核问题
```

已补齐前端发布卡片回归覆盖：

```text
DataSrv / Hub 回流 importedRunEvidence
  -> 动态 UI layout 被 Studio 修改后：证据过期，禁用提交审核
  -> resultContract 被修改后：证据过期，禁用提交审核
  -> testProtocol 被修改后：证据过期，禁用提交审核
```

对应全链路意义：

```text
应用制作 / 测试 / 上传
  -> 测试证据绑定当前 App 定义
  -> 修改界面布局、结果合同、测试协议或依赖后必须重新测试
  -> 前端发布按钮与后端 SubmitMaclawAppPackage 治理审查双重兜底
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "treats imported run evidence as stale"
```

### 推进记录：DataSrv 回流安装证据保留完整测试协议（2026-06-28）

本轮继续从“DataSrv 安装记录 -> capabilities 回流 -> App Studio 候选 -> 本地 AppEntry -> 再发布/复测”的链路检查测试协议合同。DataSrv 归一化层已经支持把完整 App Studio 测试协议保存为扁平字段 `test_evidence_test_protocol`，但 GUI 前端合成 `installEvidence.test_evidence` 时只恢复了 `test_protocol_fingerprint`，没有把完整协议对象合成回安装证据。这样老数据或外部企业系统只写扁平协议字段时，前端 manifest 能恢复测试协议，但安装证据中的可复测协议会缺失。

已落地调整：

```text
DataSrv capabilities metadata.test_evidence_test_protocol
  -> dataSrvInstalledTestProtocol 继续恢复 manifest.testProtocol
  -> dataSrvInstalledInstallEvidence 同步恢复 installEvidence.test_evidence.test_protocol
  -> test_evidence_test_protocol_fingerprint 继续恢复 testProtocolFingerprint
```

对应全链路意义：

```text
DataSrv / Hub / 外部企业系统注册已安装 MaClaw App
  -> 即使 test protocol 只以扁平 metadata 字段存在
  -> App Studio 候选仍能生成当前 manifest 测试协议
  -> 本地 installEvidence 保留同一份 sample input / expected output / roles / scopes / risk
  -> 后续复测、发布治理、审计回放不只依赖 fingerprint 摘要
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "restores DataSrv installed enterprise normal app run evidence into app candidates"
```

### 推进记录：安装/提交证据保留审批 workflow 节点映射（2026-06-28）

本轮继续从“审批型应用 = 数据录入 + 审批 workflow Skill 运行 + 审批实例数据管理 + 结果反馈”的安装证据链路检查字段对称性。DataSrv 注册 metadata 已经保存 `workflow_mapping`，GUI capabilities 回流也能恢复 `manifest.workflow`，但 GUI 后端生成的 per-app `install_evidence` 以及前端合成的 `installEvidence` 没有稳定携带同一份 workflow 节点映射。这样在提交队列、安装审计、冷启动恢复和再次发布时，证据对象只知道 workflow contract，不知道 submit/approval/result/attention 节点和状态映射。

已落地调整：

```text
GUI 后端 maclawAppInstallEvidenceByApp
  -> 每个 app install evidence 新增 workflow_mapping
  -> 保留 submitNode / approvalNode / resultNode / attentionNode / statusMapping

GUI 前端 dataSrvInstalledInstallEvidence
  -> 将 DataSrv capabilities 恢复出的规范化 workflowMapping 写入 installEvidence.workflow_mapping

GUI 前端 installEvidenceRecordForApp
  -> 从安装审计/提交证据中读取并保留 workflow_mapping
```

对应全链路意义：

```text
App Studio 设计审批 workflow 节点
  -> 发布包 / 提交队列 / 安装审计保留节点映射
  -> DataSrv 注册和 capabilities 回流保留同一份节点映射
  -> 我的申请 / 我审批的 / 当前节点状态 / 需关注 lane / 再发布治理使用一致 workflow 定义
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestMaclawAppInstallEvidenceGeneratesDependencyVerification"
npm.cmd test -- AppsPage.test.tsx -t "turns DataSrv installed MaClaw apps into addable app candidates with layout metadata"
```
### 推进记录：GUI 普通应用结果展示消费标准结果包（2026-06-28）

上一轮已经确认 GUI 后端不会丢失 DataSrv 标准结果包。本轮继续补齐 GUI 前端展示链路：企业普通应用运行后，结果区不再只从旧式 `response.rows/status/message` 推断展示，而是优先消费 `primary_result` 与 `result_payload`，并继续把 `outputs / artifacts` 写入运行历史。

已落地调整：

```text
BusinessOperationResultView 展示构建
  -> primary_result=business_record/records/report/dashboard/content 等会驱动结果类型
  -> result_payload.text/message/summary/result 会驱动结果摘要
  -> result_payload.business_record / records / rows / cards 等会驱动记录表和记录数
  -> legacy response 仅作为兼容兜底

运行历史证据
  -> 保留 result_payload / outputs / artifacts
  -> result_payload 缺少 rows 时，用展示行补齐可审计数据
```

对应全链路意义：

```text
DataSrv 标准结果包
  -> GUI 后端透传
  -> GUI 前端结果展示
  -> GUI 运行历史 / App 测试证据 / 发布治理 / 安装回流
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "renders enterprise normal apps as business workspaces without approval instances"
npm.cmd run build
```

### 推进记录：市场单 App 安装执行后端治理门禁（2026-06-28）

本轮继续收紧“能力市场安装 -> 依赖检查 -> 治理/工作流门禁 -> 安装审计 -> 本地 AppEntry”的安装入口。粘贴包安装路径已经会在安装时重新调用后端计划/依赖安装并检查治理 issue；市场列表里的单 App 安装也必须遵守同一规则，不能因为预览看起来可安装，就绕过后端返回的 `governance_review_issues` 或 `workflow_contract_issues`。

已落地验证：

```text
市场单 App 安装
  -> 点击安装时调用 InstallMaclawAppDependencies 获取后端安装计划
  -> 若当前 App 命中 governance_review_issues，显示 Review issues 错误
  -> 不调用 RecordMaclawAppInstall
  -> 不写入本地 App 面板 customApps
```

对应全链路意义：

```text
能力市场 App
  -> 后端安装计划作为最终门禁
  -> 必需依赖、治理证据、workflow contract 任一阻断都不能进入本地运行态
  -> 企业审批型/普通型 App 不会出现“安装成功但测试证据或工作流合同不合格”的空壳入口
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "blocks single market app install when backend governance review fails|keeps dependency verification visible after single market app install"
```

### 推进记录：安装回灌审批证据支持远端审批 ID 兜底（2026-06-28）

本轮继续补齐“Hub/DataSrv 安装记录 -> GUI 本地 AppEntry -> 运行历史/再次发布”的审批证据恢复口径。DataSrv 或 Hub 的安装证据有时只有远端 `approval_id / record_approval_id`，没有本地 `instance_id / workflow_instance_id`。这种证据仍然能定位远端 RecordApproval，不能因为缺少本地 workflow 实例号而丢弃整段 `approvalInstance`。

已落地验证：

```text
test_evidence.approval_instance
  -> 只有 record_approval_id
  -> 没有 instance_id / approval_instance_id / workflow_instance_id
  -> GUI 恢复 importedRunEvidence.approvalInstance.instanceId = record_approval_id
  -> GUI 恢复 importedRunEvidence.approvalInstance.approvalID = record_approval_id
  -> approvalInstance.resultPayload / outputs / artifacts 继续保留
```

对应全链路意义：

```text
DataSrv RecordApproval / Hub 安装包 / GUI 安装审计
  -> 只要有远端审批 ID，就能恢复审批实例证据
  -> 我的申请 / 我审批的 / 需关注 / 结果反馈可以继续定位远端审批
  -> 再次发布或复测不会因为缺本地 workflow_instance_id 误判为无审批证据
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "restores single app install evidence from top-level market install records"
```

### 推进记录：DataSrv OpenAPI 明确动态布局 regions 保留合同（2026-06-28）

本轮继续硬化“所有 MaClaw App 界面动态生成，用户调节位置后保存在应用信息文件，并能经 DataSrv/Hub 安装回流恢复”的公开接口合同。服务端 `app_installations.metadata.workspace_layout` 已经保留完整 `regions`，并生成 `workspace_layout_primary_region / workspace_layout_output_region / workspace_layout_region_count / workspace_layout_region_ids` 摘要；本轮把 OpenAPI 描述补齐，明确 `workspace_layout` 本体包含保留的 `workspace_layout.regions` 布局位置元数据，而不是只有摘要字段。

已落地调整：

```text
DataSrv OpenAPI app_installations.metadata
  -> workspace_layout 描述明确包含 preserved workspace_layout.regions placement metadata
  -> workspace_layout_region_ids 描述明确来自 workspace_layout.regions
  -> schema 测试检查这两个 description 必须提到 workspace_layout.regions
```

对应全链路意义：

```text
App Studio 可视化布局
  -> maclaw.app.json / Hub 安装包保存完整 regions
  -> RecordMaclawAppInstall 注册到 DataSrv
  -> DataSrv capabilities / app_installations 回流完整 workspace_layout
  -> GUI 重新生成传统软件式工作台时不丢用户调节的位置和可见性
```

已通过定向验证：

```text
go test ./structureddata -count=1 -timeout 60s -run "TestAppInstallationOpenAPISchemaDocumentsFullTestEvidence"
go test ./structureddata -count=1 -timeout 60s -run "TestUpsertAppInstallationNormalizesGovernanceResultContract"
```

### 推进记录：DataSrv capabilities 回流保留完整测试证据（2026-06-28）

本轮继续硬化“DataSrv 是安装后恢复、审计、二次发布的证据源，不能只写摘要字段”这一条。此前 `/api/v1/data/app-installations` 的创建返回已经验证完整 `test_evidence.result_payload / outputs / artifacts / approval_instance` 被保存；本轮把同一要求推进到 `/api/v1/data/capabilities`，确保 GUI 从 DataSrv 发现并恢复已安装 MaClaw App 时，拿到的不只是摘要计数。

已落地验证：

```text
GET /api/v1/data/capabilities
  -> app_installations[].metadata.test_evidence.result_payload 原样保留
  -> app_installations[].metadata.test_evidence.outputs 原样保留
  -> app_installations[].metadata.test_evidence.artifacts 原样保留
  -> app_installations[].metadata.test_evidence.approval_instance 原样保留
  -> 同时继续暴露 test_evidence_output_count / artifact_count / approval_id 等摘要
```

对应全链路意义：

```text
Hub / App Studio / GUI 后端 注册 MaClaw App 安装证据
  -> DataSrv 保存完整测试证据
  -> GUI capabilities 发现已安装 App
  -> importedRunEvidence / installEvidence 可恢复结果 payload、输出块、文件产物和审批实例
  -> 再次发布、复测、运行诊断不需要回退到不可验证的计数摘要
```

已通过定向验证：

```text
go test ./structureddata -count=1 -timeout 60s -run "TestHTTPServerRequiresBearerTokenAndHandlesRecords"
```
### 推进记录：GUI 普通应用运行历史穿透标准结果包（2026-06-27）
上一轮已经让 `ExecuteMaclawAppBusinessOperation` 返回标准 `result_payload / outputs / artifacts / primary_result`。本轮继续把这份标准结果包穿透到 GUI 运行历史，避免前端只根据简化后的 DataSrv `response` 重新推断结果，从而丢失后端已经给出的业务记录、附件或结构化输出。

已落地调整：

```text
BusinessOperationResultView
  -> 新增 primaryResult
  -> 新增 resultPayload
  -> 新增 outputs
  -> 新增 artifacts

businessOperationRunEvidence
  -> 优先保存后端标准 result_payload
  -> 优先保存后端标准 outputs
  -> 保存后端 artifacts
  -> 旧后端没有标准包时继续使用 response 推断
```

对应全链路意义：

```text
企业普通应用运行
  -> DataSrv 返回标准结果包
  -> GUI 工作区显示仍使用 response 生成摘要/表格
  -> GUI 运行历史保存标准 result_payload / outputs / artifacts
  -> 后续测试证据、发布治理、安装回流可以复用同一份结果包
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "renders enterprise normal apps as business workspaces without approval instances"
npm.cmd run build
```
### 推进记录：DataSrv 业务动作源头返回 MaClaw App 标准结果包（2026-06-27）
前几轮已经让 GUI 后端和前端运行历史能处理企业普通应用的 `result_payload / outputs / artifacts`。本轮继续把该合同前移到 DataSrv 业务动作源头，避免普通应用只能依赖 GUI 对原始 `response` 的二次推断。

已落地调整：

```text
ExecuteBusinessActionResult
  -> 新增 primary_result
  -> 新增 business_status
  -> 新增 result_status
  -> 新增 result_payload
  -> 新增 outputs
  -> 新增 artifacts

DataSrv ExecuteBusinessAction
  -> dry_run=true 时返回 preview business_record 结果包
  -> commit 时返回 committed business_record 结果包
  -> result_payload 包含 business_status / result_status / business_action_id / dataset_id / domain / operation / record_id / business_record
  -> outputs 至少包含一个 business_record 输出块
  -> artifacts 无文件时返回空包

OpenAPI
  -> /api/v1/data/business-actions/{actionId}/execute 200 响应声明标准结果包字段
```

对应全链路意义：

```text
企业普通应用
  -> DataSrv 业务动作源头产出标准结果包
  -> GUI 后端 ExecuteMaclawAppBusinessOperation 可直接透传
  -> GUI 运行历史 / 测试证据 / 发布治理 / 安装回流使用同一份结构
  -> 普通应用与审批型应用在“结果反馈”合同上对齐
```

已通过定向验证：

```text
go test ./structureddata -count=1 -timeout 60s -run "TestHTTPServerRequiresBearerTokenAndHandlesRecords"
```
### 推进记录：DataSrv 视图/报表/仪表盘源头返回标准结果包（2026-06-28）
上一轮已经让 DataSrv `business-actions/{actionId}/execute` 源头返回 MaClaw App 标准结果包。本轮继续把同一合同扩展到企业普通应用常用的查询、报表、仪表盘三类 DataSrv 操作，避免 GUI 或 App Runtime 需要分别理解 view/report/dashboard 的内部结构后再二次推断。

已落地调整：

```text
BusinessViewResult / ReportResult / DashboardResult
  -> 新增 primary_result
  -> 新增 business_status
  -> 新增 result_status
  -> 新增 result_payload
  -> 新增 outputs
  -> 新增 artifacts

QueryBusinessView
  -> primary_result = records
  -> result_payload.records / record_count / view_id / dataset_id / domain
  -> outputs[0].kind = table

RunReport
  -> primary_result = report
  -> result_payload.rows / row_count / scanned / report_id / dataset_id / domain
  -> outputs[0].kind = report

RunDashboard
  -> primary_result = dashboard
  -> result_payload.cards / card_count / dashboard_id / domain / generated_at
  -> outputs[0].kind = dashboard

OpenAPI
  -> /api/v1/data/views/{viewId}/query 200 响应声明标准结果包字段
  -> /api/v1/data/reports/{reportId}/run 200 响应声明标准结果包字段
  -> /api/v1/data/dashboards/{dashboardId}/run 200 响应声明标准结果包字段
```

对应全链路意义：

```text
企业普通应用
  -> DataSrv business action / view / report / dashboard 四类操作全部产出标准结果包
  -> GUI 后端可直接透传，不再按端点类型猜测结果
  -> GUI 运行历史、App 测试证据、发布治理、安装回流可使用同一套 result_payload / outputs / artifacts 合同
  -> 普通应用与审批型应用在“结果反馈”层彻底对齐
```

已通过定向验证：

```text
go test ./structureddata -count=1 -timeout 60s -run "TestHTTPServerRequiresBearerTokenAndHandlesRecords"
go test ./structureddata -count=1 -timeout 60s -run "TestBusinessViewResultIncludesPaginationCursor"
```
### 推进记录：GUI 后端透传 DataSrv 普通应用标准结果包（2026-06-28）
上一轮 DataSrv 已经让 business action / view / report / dashboard 四类企业普通应用操作都从源头产出标准 `result_payload / outputs / artifacts`。本轮继续补齐 GUI 后端桥接验证，确保 `ExecuteMaclawAppBusinessOperation` 不会把 DataSrv 标准包压扁成旧式 `response`，而是把上游结果包直接返回给 GUI 前端和运行历史。

已落地调整：

```text
GUI ExecuteMaclawAppBusinessOperation 测试覆盖
  -> business_view 保留 DataSrv primary_result=records / result_payload / table outputs / artifacts
  -> business_report 保留 DataSrv primary_result=report / result_payload / report outputs / artifacts
  -> business_dashboard 保留 DataSrv primary_result=dashboard / result_payload / dashboard outputs / artifacts
  -> business_action 继续覆盖默认 business_record 结果包
```

对应全链路意义：

```text
DataSrv 标准结果包
  -> GUI 后端 ExecuteMaclawAppBusinessOperation
  -> GUI 前端 BusinessOperationResultView
  -> GUI 运行历史 AppRunHistoryEntry
  -> App 测试证据 / 发布治理 / 安装回流
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestExecuteMaclawAppBusinessOperationRunsPreferred(Action|Report|Dashboard)|TestExecuteMaclawAppBusinessOperationQueriesPreferredView"
```
### 推进记录：前端 App Studio / Hub 安装对齐标准 app.ui（2026-06-28）

本轮收口 GUI 前端对标准 `maclaw.app.v1` 包体中顶层 `app.ui` 的读写验证，确保动态生成并由用户调整后的传统软件式工作区布局，不只保存在兼容字段 `binding.ui`，也能作为 MaClaw App 的标准界面契约进入发布包、Hub 安装包和本地安装恢复数据。

已落地验证：

```text
manifestToAppEntry
  -> 优先读取 app.ui
  -> 兼容回退 binding.ui
  -> Hub 安装后的 manifest.ui.entry / layouts / regions 保持完整

appToManifest / 发布包导出
  -> 写入标准 app.ui
  -> 同步写入 binding.ui 作为旧消费端兼容
  -> governance.workspaceLayout 继续保存可审计的布局摘要和完整导航/列表配置

Hub 搜索安装
  -> installSelectedMaclawAppPackageFromHub 只安装目标 app
  -> 安装后 installEvidence.workspace_layout.regions 不丢失
  -> 已安装反馈、依赖校验、版本快照和测试证据在前端可恢复
```

对应全链路意义：

```text
App Studio 可视化布局设计
  -> maclaw.app.json / Hub package 标准 app.ui
  -> Hub 搜索安装选定 App
  -> GUI 本地应用面板恢复 manifest.ui
  -> installEvidence.workspace_layout 回流动态布局 regions
  -> 后续运行、再发布、冷启动恢复继续使用同一份界面契约
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "uses the enterprise market bridge when submitting app packages|installs approved Hub MaClaw Apps from market search results|includes enterprise visual UI metadata in market submission packages"
go test ./hub/internal/httpapi -count=1 -run "TestCapabilityMaclawAppSubmitCreatesPendingReviewCapability|TestCapabilityMaclawAppPackageDownloadReturnsApprovedPack"
go test ./gui -count=1 -vet=off -run "TestInstallMaclawAppPackageFromHubDownloadsAndRecordsInstall|TestInstallSelectedMaclawAppPackageFromHubFiltersPackageApps"
```

### 推进记录：MaClaw App Skill 依赖版本冲突阻断（2026-06-28）

本轮继续推进“MaClaw App 是带应用数据的超级 Skill，安装时必须检查并安装依赖 Skill”这条主线。此前安装计划已经能识别缺失、禁用、安装失败等依赖状态，但对于“本地已安装同名 Skill，版本不符合 App 要求”的情况，依赖项本身仍可能显示为已安装可用，只在 workflow contract review issue 中提示版本不匹配。这个反馈对安装决策不够直接。

已落地调整：

```text
maclaw.app.install_plan.v1 dependencies[]
  -> 新增 installed_version
  -> 新增 required_version
  -> 新增 version_status: matched / mismatch / unknown
  -> required dependency 版本不匹配时 action=blocked
  -> health=version_mismatch
  -> has_blocking_dependency=true
  -> has_missing_required=false（已安装但版本不符，不混同为缺失）

GUI 依赖摘要
  -> 版本不匹配显示为 Unavailable
  -> 展示 installed -> required，例如 1.0.0 -> 2.1.0
  -> 市场安装预览和安装失败结果都能看到同一份版本冲突证据

依赖合并
  -> 同一 Skill ID 在 appSkill / workflowSkill / dependencies.skills 中重复声明时，按 id/version/source/install_ref/required 合并
  -> kind 不再把同一安装目标拆成多个依赖项
```

对应全链路意义：

```text
安装 MaClaw App
  -> 解析 App 依赖 Skill
  -> 检查本地已安装 Skill 版本
  -> 版本不符时直接阻断安装
  -> 前端显示版本冲突
  -> installEvidence / dependencyVerification 保存可审计依赖事实
  -> 后续发布、冷启动恢复、DataSrv 回流可继续复用同一依赖检查结果
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestPlanMaclawAppInstallChecksInstalledApprovalWorkflowVersion|TestInstallMaclawAppDependenciesSkipsInstalledAndBlocksUnsupportedSource|TestInstallMaclawAppDependenciesInstallsHubBackedSources|TestInstallMaclawAppDependenciesPreservesInstallRefFromDuplicateDependency|TestPlanMaclawAppInstallPackDedupesDependenciesAndNormalizesLegacyKind"
npm.cmd test -- AppsPage.test.tsx -t "uses the enterprise market bridge when submitting app packages|installs approved Hub MaClaw Apps from market search results|includes enterprise visual UI metadata in market submission packages|blocks market install when a required dependency has a version mismatch"
```

### 推进记录：审批实例 DataSrv 回流按事实归类 lane（2026-06-28）

本轮推进企业审批型应用的运行态闭环。审批型 App 不只是发起表单，还必须能恢复和查看审批实例状态，包括“我的申请 / 待我审批 / 已处理 / 需关注”。此前 GUI 从 DataSrv 读取审批实例时，会把请求参数中的 `lane` 优先写入返回实例；如果 DataSrv 返回混合数据或只做弱过滤，前端可能把不属于当前用户的记录错误归到“待我审批”。

已落地调整：

```text
DataSrv approval 回流
  -> 不再用请求参数 lane 覆盖实例 lane
  -> 优先使用记录显式 lane / approval_lane
  -> attention 状态进入需关注
  -> submitted_by / created_by / applicant 命中当前用户 -> 我的申请
  -> pending 且 assigned_to / current_assignee 命中当前用户 -> 待我审批
  -> reviewed_by 命中当前用户或终态 -> 已处理
  -> pending 但不属于当前用户 -> pending（只在 all 视图保留，不进入待我审批）

GUI 审批中心
  -> 切换“待我审批”时向后端请求 pending_my_approval
  -> 后端按事实重新归类和过滤，避免请求 lane 污染实例事实
```

对应全链路意义：

```text
审批型 MaClaw App
  -> 发起审批实例
  -> DataSrv 保存/返回 RecordApproval
  -> GUI 读取远端审批实例
  -> 按当前用户和实例事实恢复 lane
  -> 我的申请 / 待我审批 / 已处理 / 需关注 视图可信
  -> 审批中心与单个审批型 App 的实例视图语义一致
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestListMaclawAppApprovalInstances(AllLoadsDataSrvLane|LoadsRequestOnlyRuntimeResult|MapsDataSrvAttentionStatus|AllInfersDataSrvLanesForCurrentUser|DoesNotTrustRequestedLaneForDataSrvItems|MergesDataSrvWithLocal|All)$|TestListMaclawAppApprovalInstancesLifecycle|TestListMaclawAppApprovalInstancesMyRequestsLane"
npm.cmd test -- AppsPage.test.tsx -t "keeps approval instance detail scoped to the selected lane and row"
```

### 推进记录：审批决策默认生成可展示结果输出（2026-06-28）

本轮继续收口企业审批型应用的“结果反馈”链路。此前人工审批和审批 workflow 完成后已经能写回 `result_payload`、业务状态、审批状态、远端审批 ID，并同步到 DataSrv；但如果 workflow 或待审批实例本身没有显式 `outputs`，最终审批实例可能只有状态字段和 result payload，没有可展示的结果输出块。这样会削弱 App Studio 发布证据、Hub 审核、DataSrv 回流和用户查看最终结果的一致性。

已落地调整：

```text
GUI 前端审批结果输出
  -> 新增 approval_result 默认输出块
  -> workflow skill 完成/失败且没有显式 outputs 时，自动生成结果输出
  -> App 内审批实例操作通过/拒绝/需关注时，若原实例没有 outputs，自动生成结果输出
  -> 全局审批管理面板同样使用一致的默认结果输出
  -> 默认输出 data 保留 approval_result / business_status / result_status 和 resultPayload 内容

GUI 后端本地审批实例持久化
  -> review 同步 DataSrv 后，再验证本地 handled 实例保留 WorkflowDecisionID
  -> 保留 BusinessStatus / ResultStatus / FromStatus / ToStatus
  -> 保留 ResultPayload / Outputs / Artifacts 最终结果包
```

对应全链路意义：

```text
审批型 MaClaw App
  -> 发起审批实例
  -> 人工审批或 workflow skill 产生最终决策
  -> 即使 skill 未返回显式 outputs，也生成可展示结果内容
  -> RecordMaclawAppApprovalInstance 本地保存完整最终实例
  -> SyncMaclawAppApprovalInstanceToDataSrv 带完整结果包同步
  -> 我的申请 / 待我审批 / 已处理 / 需关注 能看到状态和内容
  -> App Studio 发布证据不再只有状态字段，具备 resultPayload + outputs/artifacts 的独立实例证据
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "records approval decisions with workflow result fields|keeps approval result package when a pending item is manually approved|marks approval workflow failures as attention results|records and completes an approval instance when running an approval app"
go test ./gui -count=1 -vet=off -run "TestSyncMaclawAppApprovalInstanceToDataSrv$"
```

### 推进记录：App Studio 预览区直接调整动态布局区域（2026-06-28）

本轮继续推进“所有 MaClaw App 的界面动态生成，应用程序设计时自动生成，用户调节位置，并保存到应用信息文件”这条主线。此前 App Studio 已经能选择布局模板、主操作区、输出区、隐藏区域，并把 `ui.layouts[entry].regions` 写入 manifest；但区域位置主要依赖右侧下拉框，预览区点击只调整主操作区，不够像传统软件的可视化布局配置。

已落地调整：

```text
App Studio Layout Designer
  -> 预览区每个区域胶囊新增位置快捷按钮
  -> 支持直接把区域移动到 left / center / right / bottom / modal
  -> 区域移动仍写入 manifest.ui.layouts[entry].regions
  -> output 类型区域移动到 right / bottom / modal 时同步 outputRegion 摘要
  -> input 类型区域移动到 left / center / right 时同步 primaryRegion 摘要
  -> 预览 slot 改为可键盘操作的 role=button 容器，避免区域快捷按钮嵌套在 button 内
```

对应全链路意义：

```text
App Studio 自动生成动态 UI
  -> 用户在预览区直接调整区域位置
  -> 保存为 maclaw.app.ui.v1 的 canonical regions
  -> 本地 AppEntry 运行时按 region placement 渲染传统软件式工作台
  -> appToManifest / Hub 上传 / Hub 安装 / DataSrv 回流继续使用同一份布局契约
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "moves workspace regions from the visual layout preview into saved manifest regions|saves visual workspace region visibility and applies it in the runtime panel"
```

### 推进记录：发布门禁按标准 app.ui 计算界面定义指纹（2026-06-28）

本轮继续收口 App Studio 测试证据、Hub 上传审核和安装回流之间的定义一致性。标准包体已经把动态界面保存到顶层 `app.ui`，`binding.ui` 只作为旧消费端兼容字段；因此发布治理中的 definitionHash 也必须按同一优先级计算，否则会出现“用户在 App Studio 调整了界面布局，但旧运行证据仍被认为可复用”的风险。

已落地调整：

```text
GUI 后端 definitionHash
  -> 计算 app 定义指纹时优先读取顶层 app.ui
  -> 仅当顶层 app.ui 缺失时回退 legacy binding.ui
  -> 与前端 appDefinitionFingerprint 的标准 manifest.ui 口径对齐

workspace layout metadata
  -> 治理摘要优先来自顶层 app.ui
  -> legacy binding.ui-only 包仍可被归一化、审核和安装

发布审核测试
  -> 顶层 app.ui 变更会让旧 run evidence definitionHash 过期
  -> legacy binding.ui-only 包在 UI 变更后同样会触发 stale evidence
  -> 避免兼容字段掩盖标准界面契约变化
```

对应全链路意义：

```text
App Studio 可视化调整布局
  -> 保存为标准 maclaw.app.v1 app.ui
  -> 测试证据记录当前定义指纹
  -> 上传 Hub 前后端共同校验 definitionHash
  -> 界面布局、依赖、结果合同、测试协议任一核心定义变化后必须重新测试
  -> Hub / DataSrv / GUI 回流不会复用过期证据
```

已通过定向验证：

```text
go test ./gui -count=1 -vet=off -run "TestSubmitMaclawAppPackageFlagsStaleRunEvidenceWhen(TopLevelUILayout|LegacyBindingUILayout|UILayout)Changes|TestSubmitMaclawAppPackagePersistsNormalizedWorkspaceLayout|TestPlanMaclawAppInstallPackNormalizesAppUI"
```

### 推进记录：恢复 App Studio / DataSrv 候选回流前端回归测试入口（2026-06-28）

本轮继续清理全链路验收的测试可用性。`AppsPage.test.tsx` 中一批旧断言仍使用乱码中文 title/text 查找 App Studio、应用管理、搜索框、候选应用按钮和管理操作按钮；当前 UI 已经正常显示中文，导致整份 App 面板回归在进入真实业务断言前就失败。此问题会掩盖 MaClaw App 制作、DataSrv 能力候选回流、Skill manifest 注册和 App Studio 管理页的真实缺陷。

已落地调整：

```text
前端测试入口
  -> 新增 getStudioButton()，按 App Studio / 应用程序工作室稳定查找入口
  -> 新增 getManageTab()，按 应用管理 / Manage 稳定查找管理页
  -> 替换 85 处旧乱码 App Studio title 调用
  -> 替换 26 处旧乱码 应用管理 tab 调用

关键链路断言
  -> 搜索框、分类、常用应用基础渲染断言改为正常 Unicode
  -> DataSrv capabilities -> 可生成应用 -> 添加到面板 -> 已添加 -> 关闭 恢复正常中文断言
  -> Skill maclaw.apps.json 候选注册测试恢复关闭按钮断言
  -> App Studio 管理页隐藏/置顶/常用上限按钮 title 恢复正常中文断言
  -> 安装版本快照使用实际 UI 分隔符 “·”，不再匹配乱码 “路”
```

对应全链路意义：

```text
App Studio 入口
  -> DataSrv capabilities 生成候选 App
  -> Skill maclaw.apps.json 生成工具 App
  -> Hub/DataSrv installed MaClaw App 恢复 layout metadata / version snapshot
  -> 管理页隐藏、置顶、常用上限等传统软件式面板操作
  -> 前端回归测试能进入真实业务断言，而不是被历史编码噪声阻断
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "turns DataSrv capabilities into addable app candidates|turns skill maclaw.apps.json entries into registered tool apps|renders the app panel with search"
npm.cmd test -- AppsPage.test.tsx -t "filters hidden apps in app studio management|keeps pinned apps capped at two rows|turns DataSrv installed MaClaw apps into addable app candidates with layout metadata"
npm.cmd test -- AppsPage.test.tsx -t "treats imported run evidence as stale after dynamic UI layout changes|treats imported run evidence as stale after result contract changes|treats imported run evidence as stale after test protocol changes|includes enterprise visual UI metadata in market submission packages|moves workspace regions from the visual layout preview into saved manifest regions"
```

当前状态：

```text
整份 AppsPage.test.tsx 仍未宣称全绿；剩余失败主要仍是其它旧乱码中文断言和少量 act warning 噪声。
本轮先恢复 App Studio / DataSrv 候选 / 管理页 / UI layout evidence 这些 MaClaw App 主链路定向回归。
```

### 推进记录：修复 App 发布依赖验证门禁与前端回归噪声（2026-06-28）

本轮继续推进 MaClaw App 从本地测试到企业能力市场审核的闭环。之前 App Studio 发布页在运行证据已经新鲜的情况下，仍提示“依赖验证缺少声明 Skill”，导致本地已测试 App 不能进入提交审核状态。根因是依赖验证证据需要同时识别同一 App 的本地入口 ID、DataSrv manifest appID 和安装包装 ID；测试 helper 还把同名 `appSkill/local` 依赖错误合并成后面的 `runtime_skill/hub`，与产品侧 `appSkillDependencies` 合并规则不一致。

已落地调整：

```text
App 发布依赖门禁
  -> 新增 appDependencyVerificationAppIDs(app)
  -> 发布检查同时接受 canonical manifest ID、本地 app.id、manifest.datasrv.appID
  -> 保持依赖声明、依赖验证、阻断依赖三类门禁仍然严格校验

运行证据 / 测试 helper
  -> testAppManifestID 与 canonicalAppManifestID 对齐
  -> dependencyVerification.dependencies[*].app_ids 写入完整 App 身份集合
  -> testDeclaredSkillDependencies 合并同名依赖时保留 appSkill/local 语义，不再被 bound skill 覆盖成 runtime_skill/hub

前端回归测试
  -> 继续清理 AppsPage.test.tsx 历史乱码中文断言
  -> 将市场安装、发布队列、审批视图、运行区、App Studio 创建器等高频文案恢复为当前 UI 文案
  -> 安装包 textarea placeholder 支持中英文文案匹配
```

已通过定向验证：

```text
npm.cmd test -- AppsPage.test.tsx -t "submits ready local apps into local review state|shows returned review status and allows resubmission"
npm.cmd test -- AppsPage.test.tsx -t "opens global approval management|shows DataSrv approval app summary|does not repeat pinned apps|moves app tile focus|exposes app studio sections|shows local apps in the review and publish checklist|submits ready local apps into local review state|shows returned review status and allows resubmission|copies the draft manifest preview"
npm.cmd test -- AppsPage.test.tsx -t "saves a newly created enterprise approval app into its app skill definition|requires a successful current-version test before uploading a skill app|opens market import with the current app manifest when review dependency issues need repair|does not bypass dependency installation when market preview plan is still loading|keeps dependency verification visible after single market app install"
npm.cmd test -- AppsPage.test.tsx -t "shows app name, status, source, and recent usage in tile tooltips|opens global approval management|shows detailed errors for invalid pasted manifests|shows detailed errors for invalid app pack manifests|blocks app package review submission when required dependencies are unavailable"
```

当前全量前端回归状态：

```text
npm.cmd test -- AppsPage.test.tsx --reporter=json --outputFile=apps-page-vitest.json
  -> 194 total
  -> 49 failed

失败数从本轮开始时的 101 个降到 49 个。
已确认发布审核关键门禁、企业审批型 App 创建保存、市场安装依赖验证、无效安装包错误提示等代表链路通过。
剩余失败集中在提交队列详情、App Studio 草稿编辑、运行 tab、运行历史、市场安装部分边界和少量仍未清理的旧中文断言。
下一步继续按失败簇推进，而不是把整份测试失败混在一起处理。
```
### 推进记录：收口 MaClaw App 安装/审批实例/DataSrv/Hub 验证（2026-06-28）
本轮继续按“从 DataSrv 到 MaClaw App，再到 Hub 能力市场安装和运行”的全链路角度推进。重点修复 GUI 后端安装与审批实例管理的两个契约问题：

```text
安装错误优先级
  -> 具体的审批工作流契约错误优先暴露，避免被缺依赖泛化错误掩盖
  -> 纯 missing approval workflow contract 不覆盖 required Skill dependency blocked
  -> 既保留依赖安装门禁，也让审批型 App 的工作流契约问题在该暴露时可定位

审批实例列表
  -> 本地未同步远端 ID 的 pending + assignee 实例可同时出现在“我的申请”和“我审批的”
  -> DataSrv 返回项仍按提交人/审批人语义推断 lane，不盲信 requested lane
  -> 本地运行态实例与 DataSrv 远端审批记录合并时，保留本地 pending_my_approval lane，避免远端身份补全把运行态审批列表抹掉

IM 回归稳定性
  -> ok/okay 短闲聊测试改为验证 immediate classifier，不再误入真实 LLM stream
  -> 避免 GUI 包回归被外部网络请求卡住
```

本轮已通过验证：

```text
go test ./gui -count=1 -vet=off -run "TestHandleIMMessageWithProgressAndStream_OkShortChitChatWithPunctuationIsNotDirectReply" -timeout 30s
  -> ok github.com/RapidAI/CodeClaw/gui

go test ./gui -count=1 -vet=off -run "TestPlanMaclawAppInstallBlocksInstalledInactiveRequiredDependency|TestListMaclawAppApprovalInstancesDoesNotTrustRequestedLaneForDataSrvItems|TestRecordMaclawAppInstallRegistersApprovalAppWithDataSrv|TestMaclawAppApprovalPendingRequestAlsoAppearsForApprover" -timeout 90s
  -> ok github.com/RapidAI/CodeClaw/gui

go test ./gui -count=1 -vet=off -run "MaclawApp|ApprovalPending|RecordMaclawAppInstall" -timeout 90s
  -> ok github.com/RapidAI/CodeClaw/gui

cd datasrv
go test ./... -count=1 -vet=off
  -> ok cmd/maclaw-data-srv
  -> ok structureddata

go test ./hub/internal/httpapi -count=1 -vet=off -run "MaclawApp|AppInstall|CapabilityMaclaw|Approval"
  -> ok github.com/RapidAI/CodeClaw/hub/internal/httpapi
```

当前仍需继续跟进：

```text
go test ./gui -count=1 -vet=off -timeout 180s
  -> 仍可能因整包体量/后台 goroutine/历史长测在包级超时，需要继续拆分治理

go test ./hub/internal/store/... -count=1 -vet=off
  -> hub/internal/store/sqlite 有 2 个 session duration 汇总测试失败：
     TestSessionRepositorySummarizeUserDurationsMergesOverlapsAndRequiresEmail
     TestSessionRepositorySummarizeUserDurationsIncludesLegacyBlankUpdatedAt
  -> 当前判断为 Hub store 历史统计逻辑问题，非 MaClaw App 安装/审批链路直接阻塞，但发布前仍需单独修复或标注
```

### 推进记录：GUI 后端工作流/AgentLoop/工具守卫稳定化（2026-06-28）

本轮继续从“DataSrv -> MaClaw App -> App Studio -> Hub 能力市场 -> 安装运行”的全链路验证角度推进。核心 MaClaw App 链路此前已基本通过，当前主要阻塞变成 GUI 包级回归不稳定：测试环境误连真实 OAuth、工作流兼容路径吞掉状态机响应、截图/模板/文件工具守卫顺序不一致、IntentUnderstandingManager 被简化成永远 rejected，导致审批型 App 的“发起节点 UI -> 工作流理解/确认 -> 审批实例状态 -> 结果反馈”无法用包级测试证明。

本轮已落地调整：

```text
测试环境 LLM provider 隔离
  -> App.SaveConfig 在 testHomeDir 下不再恢复旧的后端托管 LLM provider 字段
  -> agent loop / confirmation / workflow 测试使用本地 httptest OpenAI 兼容端点，不再误连 chatgpt.com OAuth

确认与 pending reply
  -> 执行确认 LLM 不可用时，短明确同意词（go ahead / proceed / do it 等）有本地兜底
  -> pending user reply 会把提问后新增的当前历史片段带入 Context hint，避免用户回答时丢失中间 tool note

工具守卫
  -> write_file 空内容仍视为有效写入；无 App 的轻量 handler 使用当前目录解析相对路径
  -> launch_template 先返回缺参数/模板不存在/模板管理器未初始化，再对有效模板返回外部 coding session 禁用
  -> screenshot 已有本地图片路径守卫前移到 Hub security policy 之前；显式 runtime 但 owner 为空时不继承 legacy lastUserText

Workflow v2 / 兼容层
  -> IntentUnderstandingManager 恢复最小 LLM JSON 解析：Start / HandleInput 支持 ready、rejected、active session 和错误外显
  -> 活动 workflow 优先于 UIC bypass；直接执行意图可从 active understanding 逃逸回普通 agent loop
  -> V1 兼容 handleActiveWorkflow 对 RunAgentLoop=true 返回阶段描述/提示，不再让活动工作流输入表现为 nil
  -> StateMachine 保留生产 temp/test 目录保护，但测试 StateMachine 可显式允许 temp path，验证“记录路径但不创建目录”
  -> SetWorkflowWorkingDir 同步 V2 状态和 V1 adapter，保持 App Studio/GUI 兼容层一致
```

本轮已通过验证：

```text
go test ./gui -count=1 -vet=off -run "TestClassifyConfirmationIntent_UsesContextForTypedApproval|TestHandleIMMessage_PlainTextReplyAcceptsCurrentHistoryExtension|TestIMToolWriteFile_AllowsEmptyContent|TestToolLaunchTemplate_NotFound|TestToolLaunchTemplate_MissingParam|TestToolLaunchTemplate_NilManager|TestExecuteTool_ScreenshotBlockedForUserSuppliedImagePath" -timeout 120s
  -> ok github.com/RapidAI/CodeClaw/gui

go test ./gui -count=1 -vet=off -run "TestRunAgentLoop_TrialReflect_ClearsRepeatGuardAfterSuccess|TestRunAgentLoop_TrialReflect_RestartsFailureCycleAfterSuccess|TestRunAgentLoop_EstimatesTokenUsageWhenStreamOmitsUsage|TestRunAgentLoop_OrientSkillPreferenceInjectsRunSkillGuidance|TestRunAgentLoop_SkillFailureInjectsFallbackGuidance" -timeout 180s
  -> ok github.com/RapidAI/CodeClaw/gui

go test ./gui -count=1 -vet=off -run "TestActiveWorkflowIgnoresUICNonWorkflowBypass|TestApprovePendingWorkflowConfirmationRecordsProjectPathWithoutCreatingDirectory|TestApprovePendingWorkflowConfirmationInfersExplicitCodingProjectPath|TestSetWorkflowWorkingDirRecordsProjectPathWithoutCreatingDirectory|TestWorkflowInterception" -timeout 120s
  -> ok github.com/RapidAI/CodeClaw/gui

go test ./gui -count=1 -vet=off -run "TestActiveUnderstandingEscapesForDirectExecutionIntent|TestExecuteTool_ScreenshotBlockedForUserSuppliedImagePath|TestToolScreenshotOwnerlessCurrentRuntimeDoesNotUseLegacyTaskText" -timeout 90s
  -> ok github.com/RapidAI/CodeClaw/gui

go test ./corelib/workflow/v2 -count=1 -vet=off -timeout 120s
  -> ok github.com/RapidAI/CodeClaw/corelib/workflow/v2
```

全包验证：

```text
go test ./gui -count=1 -vet=off -timeout 360s -json > gui-test-latest-11.json
  -> 未超时
  -> 仍失败 45 个测试
```

剩余 GUI 全包缺口按类别归并：

```text
workflow/form/tool guard
  -> workflow form user id 解析、附件输入后 workflow agent loop 标记、SSH upload 守卫顺序仍需收口

agent loop recover
  -> 空 assistant 回复、长链 grace rounds、部分进度上报和重复 recover 仍有失败

remote / probe / client
  -> ActivateRemote 异步返回、PTY/Claude probe、remote hub client interrupt/input/reconnect/error 存储仍有失败

risk / policy
  -> medium/high/critical risk ask/audit、permission default mode、policy mode normalization 仍需统一

stream / misc
  -> OpenAI 非 SSE 错误日志、finish_reason before DONE、project tab workdir repair、task understanding summary 等仍需单独处理
```

当前判断：

```text
MaClaw App 主链路不是从零缺失，核心 DataSrv / Hub / GUI 定向链路已经形成。
离“完整完成”主要还差 GUI 包级稳定化和一轮全链路复测。
下一步继续优先修 workflow/form/tool guard 与 agent loop recover；这些直接影响企业审批型 App 的运行闭环。
remote / risk / stream 类失败也会影响发布质量，但不是 MaClaw App 安装/审批实例主链路的首要断点。
```

### 推进记录：GUI 后端主链路继续收口（2026-06-28）

本轮继续按“DataSrv -> Hub -> MaClaw App -> App Studio/GUI 运行”的全链路目标推进，优先处理会直接影响企业审批型应用运行闭环的 GUI 后端问题。最新 GUI 全包回归已重新落盘：

```text
go test ./gui -count=1 -vet=off -timeout 360s -json > gui-test-latest-12.json
  -> 未超时
  -> 失败数从上一轮 45 降到 33
  -> 最新失败清单已写入 gui-test-latest-12-failures.txt
  -> 注意：该失败清单生成后，本轮又定向修复了其中的 IM 入口/短闲聊/StartNewTask/文件 runtime owner 项；真实剩余失败数需要下一次 GUI 全包重新确认
```

本轮已落地的收口点：

```text
AgentLoop recover / 文件交付
  -> 空 assistant 回复恢复提示恢复中文 Recover 阶段语义，同时保留英文指令
  -> send_file 用户可见进度改为通用“正在整理并发送文件”
  -> send_file 文件 payload 的 TraceSummary 改为中文可读交付摘要：文件 xxx 已准备好
  -> 长链路文件交付、重复空回复恢复、非 debug 进度提示相关失败已收口

IM 入口 / 上下文隔离
  -> 短闲聊 zh / en 语言识别修正，zh 返回中文轻量回复
  -> no-tool 完成态启发式补充“已处理完成 / processed”，避免明确完成的普通任务被误推进第二轮
  -> StartNewTask 清理旧未完成任务后，新任务不再被旧上下文拖入额外 LLM 调用
  -> launch_template 测试场景调整为“有效模板 -> 外部会话工具禁用”，保留 nil manager / missing template 的精确错误

文件工具 runtime owner
  -> read/write/send/list 等本地文件工具解析相对路径时，显式 runtime owner 的项目路径优先
  -> 无 app 的轻量 handler 不再在解析 owner project 前回落到当前仓库目录
  -> 缺失的托管 recent task 根目录按根目录修复，不误落到 workspace/ 子目录
  -> 已有 task working_dir 仍保留独立执行目录，不被修复逻辑覆盖
```

本轮已通过验证：

```text
go test ./gui -count=1 -vet=off -run "MaclawApp|ApprovalPending|RecordMaclawAppInstall|WorkflowPolicy|ResolveWorkflowFormUserID|RouteWorkflowIMMessage|WorkflowToolExecutionGuard|ActiveUnderstandingEscapes|RunAgentLoop_EmptyAssistantReplyTriggersRecoverPhase|RunAgentLoop_RepeatedEmptyAssistantRepliesReenterRecover|RunAgentLoop_LongChainUsesGraceRoundsToFinishFileDelivery|RunAgentLoop_NonDebugStillReportsBaseToolStageProgress|ExternalCodingSessionFollowupToolsDisabled|NewTaskAfterIncompleteRunClearsOldContext|ShortChitChat|FileToolsUseRuntimeOwnerProjectForRelativePaths|ExecuteFileToolInjectsRuntimeOwnerForRelativePaths|ProjectTabWorkDir_RepairsManagedRecentTaskWorkspace" -timeout 240s
  -> ok github.com/RapidAI/CodeClaw/gui

go test ./corelib/workflow/v2 -count=1 -vet=off -timeout 120s
  -> ok github.com/RapidAI/CodeClaw/corelib/workflow/v2

go test ./hub/internal/httpapi -count=1 -vet=off -run "MaclawApp|AppInstall|CapabilityMaclaw|Approval" -timeout 120s
  -> ok github.com/RapidAI/CodeClaw/hub/internal/httpapi

cd datasrv
go test ./... -count=1 -vet=off -timeout 120s
  -> ok cmd/maclaw-data-srv
  -> ok structureddata
```

当前剩余重点：

```text
GUI 全包仍有 33 个失败，主要集中在：
  -> presentation/coding confirmation、workflow doc capture、task summary 格式化
  -> OpenAI stream 边界、skill auto-fix writeback
  -> risk / policy ask-audit 归一化
  -> remote / probe / client 和 Codex adapter command

MaClaw App 主链路的定向证据继续增强，但“完整完成”仍需：
  -> GUI 全包继续降失败
  -> App Studio 前端 AppsPage 剩余失败继续收口
  -> 从 DataSrv 到 Hub 能力市场安装、App Studio 制作/测试/上传、App 运行/审批实例/结果反馈做最终端到端复测
```

### 推进记录：Skill/App 依赖包视图与上传目标收口（2026-06-28）

本轮继续从“MaClaw App 是带 App 元数据、动态 UI、运行合同、测试证据和依赖 Skill 的超级 Skill”这个产品本质推进。安装和发布 MaClaw App 时，依赖 Skill 必须能以真实包视图完成检查、上传、安装和重试；否则企业审批型应用的 workflow skill、runtime skill、app skill 依赖即使在设计态可用，也可能在能力市场分发后断链。

本轮落地的后端收口点：

```text
Skill 包引用归一化
  -> {baseDir}/scripts/foo.py 在质量报告中稳定显示为 scripts/foo.py
  -> {baseDir}/run.py 会按包根真实文件判断，不再被误判为缺失的 {baseDir}/run.py
  -> 绝对路径、POSIX /home/...、UNC //server/share/...、Windows drive-relative 等不可移植引用保留原始可读形态

Skill 包质量门
  -> 真实缺少的包内脚本/资产继续阻断上传
  -> ../shared-scripts/run.py 这类逃逸路径不在准备阶段提前中断，而进入质量门报告，由 MarketReady=false 统一阻断
  -> structured command、fallback step、interface-key command map、working_dir、pipeline/path 参数继续纳入缺失引用检查

上传目标选择
  -> 只有 RemoteHubURL + token 同时存在时才把 enterprise_hub 加入上传目标
  -> 只有 RemoteHubCenterURL 的环境只上传 HubCenter，不再误向企业 Hub 路径提交导致 404
  -> 上传队列按 canonical skill name 去重后，HubCenter 已完成目标可正确标记 uploaded
```

这一步补强了 MaClaw App 依赖分发链路：

```text
App Studio 保存超级 Skill
  -> Skill 包视图质量检查
  -> 依赖 Skill / workflow Skill 引用可携带到能力市场
  -> HubCenter / enterprise_hub 按配置选择上传目标
  -> 安装 MaClaw App 时可继续做依赖检查、下载安装和运行前健康检查
```

本轮已通过验证：

```text
go test ./corelib/skill -count=1 -vet=off -timeout 120s
  -> ok github.com/RapidAI/CodeClaw/corelib/skill

go test ./gui -count=1 -vet=off -run "TestSubmitSkillToConfiguredTargetsDefaultUploadsBothTargets|TestSubmitSkillToConfiguredTargetsPartialRetrySkipsCompletedTarget|TestSubmitSkillToConfiguredTargetsPartialRetrySkipsCompletedEnterpriseTarget|TestSubmitSkillToConfiguredTargetsDropsCompletedTargetsOutsideCurrentPolicy|TestEvaluateSkillPackageCompletenessChecksFallbackStepRefs|TestSkillLifecycleUploadNowUsesPackageViewForQuality|TestSkillQualityBlocksPOSIXAbsoluteCommandPath|TestSkillQualityBlocksUNCCommandPath|TestSkillQualityBlocksEscapingCommandScriptPath|TestSkillLifecycleEnqueueCanonicalizesNameBeforeDedupe" -timeout 180s
  -> ok github.com/RapidAI/CodeClaw/gui

go test ./gui -count=1 -vet=off -run "TestToolRunSkill_ForwardsQueryToWhenCondition|TestEvaluateSkillPackageCompletenessRejectsAbsoluteRefsOutsideSkillDir|TestEvaluateSkillPackageCompletenessChecksFallbackStepRefs|TestSkillLifecycleUploadNowUsesPackageViewForQuality|TestSkillQualityBlocksMissingReferencedScript|TestSkillQualityDetectsMissingStructuredCommandScript|TestSkillQualityDetectsMissingStructuredCommandScriptPathWithSpaces|TestSkillQualityChecksInterfaceKeyStructuredCommandMap|TestSkillQualityBlocksMissingWorkingDir|TestSkillQualityBlocksAbsoluteCommandScriptPath|TestSkillQualityBlocksPOSIXAbsoluteCommandPath|TestSkillQualityBlocksUNCCommandPath|TestSkillQualityBlocksWindowsDriveRelativeCommandPath|TestSkillQualityBlocksEscapingWorkingDir|TestSkillQualityBlocksEscapingCommandScriptPath|TestSkillLifecycleEnqueueCanonicalizesNameBeforeDedupe" -timeout 180s
  -> ok github.com/RapidAI/CodeClaw/gui
```

下一步继续：

```text
1. 重跑 GUI 全包，刷新真实失败数。
2. 继续收口 GUI 剩余失败簇：stream、risk/policy、remote/probe/client、Codex adapter command。
3. 回到 MaClaw App 端到端样例：DataSrv -> App Studio 制作 -> 测试 -> 上传 Hub/市场 -> 安装依赖 -> 运行审批实例 -> 结果反馈。
```

### 推进记录：GUI workflow/Skill 运行合同继续收口（2026-06-29）

本轮继续按“MaClaw App 是带特殊 App 数据的超级 Skill，企业审批型 App 依赖 workflow skill 和运行实例管理”的主线推进。重点先清理 GUI 包级回归里会直接影响审批型 App 发起、workflow 节点执行、结果文档持久化和 Skill 依赖运行的阻塞点。

已完成修复：

- GUI workflow adapter 在 `StartWorkflowWithOptions` 后立即接收初始 phase update，能把 workflow state 的 `ProjectPath` 写入 adapter workingDir；文档不再落到当前项目或真实用户 home。
- workflow 文档在目标项目尚未创建时写入 Internal Storage，项目创建并完成 workflow 后发布到 `docs/workflow/{type}/{date}`，同时清理内部临时文档。
- `IsActivePhaseExecutionOrchestrator` / `IsTemplatePhaseExecutionOrchestrator` 从 stub 改为基于模板 phase kind / mutation scope / orchestrator 标记判断，执行节点可以真正激活任务编排器。
- `ReopenPhaseForRevision` 从 stub 改为可回退指定 phase、记录 revision gate item、返回新 phase prompt，支持无效 task breakdown 重新生成。
- RunAgentLoop 响应不再被 phase form 自动弹窗拦截，避免执行阶段被发起表单误打断。
- presentation 显式 hint 补齐 `powerpoint` / `slide deck`，并把英文排除词改成词边界匹配，避免 `review` 被 `view` 误伤。
- 知识源 ID 规范化区分系统 `ksrc_*` 与外部/结构化 source ID：系统 ID 小写去重，结构化 source ID 保留大小写且只去完全重复。
- 外部 coding session 入口 `CodingSessionStarter.Start` fail closed，提示改用内部 `CodingSubAgent`，避免 Skill/App 运行重新启动旧的外部 Claude/Codex 会话。
- Skill Runner 从目录刷新定义后保留配置层 `Type`，`knowledge_skill` 不再被刷新成普通 bash skill 直接执行。

已验证：

```powershell
# workflow adapter / orchestrator / presentation / knowledge ID / session 禁用 / knowledge skill 类型保留
go test ./gui -count=1 -vet=off -run "TestNormalizeKnowledgeSourceIDsPreservesCase|TestNormalizeKnowledgeSearchOptionsTrimsAndDedupesFilters|TestNormalizeKnowledgeListOptionsTrimsAndDedupesFilters|TestGUIWorkflowAdapterUsesWorkflowStateProjectPathForDocs|TestGUIWorkflowAdapterMissingWorkflowProjectPersistsDocsInternally|TestGUIWorkflowAdapterCompletionPublishesInternalDocsAfterProjectCreated|TestActivateWorkflowTaskOrchestratorUsesWorkflowProjectPathWithoutCreatingDirectory|TestHandleWorkflowEngineResponseActivatesOrchestratorOnFirstAgentLoop|TestHandleWorkflowEngineResponseBlocksWhenProjectPathInvalid|TestHandleWorkflowEngineResponseDoesNotRepairOutsideExecutionPhase|TestHandleWorkflowEngineResponseBlocksInvalidCodingTaskBreakdownFallback|TestInferExplicitWorkflowHint_Presentation" -timeout 180s
# ok github.com/RapidAI/CodeClaw/gui

go test ./gui -count=1 -vet=off -run "TestSkillRunnerStartRun_RejectsKnowledgeSkillWithoutCraftFallback|TestCodingSessionStarterDisabled|TestSkillRunnerCreateSessionDisabled" -timeout 90s
# ok github.com/RapidAI/CodeClaw/gui
```

最新 GUI 全包状态：

```powershell
go test ./gui -count=1 -vet=off -timeout 480s -json > gui-test-latest-19.json
# FAIL，约 258s，无超时
```

剩余失败簇（下一轮优先级）：

- Skill 上传 portability auto-fix 后被 security scan 以 medium community trust + bash 写执行工具升级为 high 阻断；需要区分“自动修复后的本地可移植性重写”与真实危险脚本。
- pipeline continue-on-fail 仍会因失败步骤 capture 变量未解析而把后续步骤标为 failed；需要让 failed capture 以结构化失败输出参与后续模板展开。
- skill 参数推断存在跨包语义冲突：GUI 期望 `input=成都` 不猜 required `city`，但 corelib 旧测试仍期望显式 input 可作为单 city；需要统一“命名参数 / 显式 input / user_prompt 自然语言”的边界。
- scan report 用户展示格式需要继续委托共享 formatter，避免 GUI/TUI/Hub 审核口径分叉。
- resume slot header 当前随全局语言变成繁体中文，测试期望固定 resume header；需要把绑定 resume context 的内部提示头与 UI 语言展示解耦。

这些剩余项仍属于 GUI/Skill 运行合同稳定化，不代表 MaClaw App 主链路从零缺失。完整完成仍需要在 GUI 包稳定后，继续做 DataSrv -> App Studio 制作/测试 -> Hub 上传/安装依赖 -> 企业审批实例运行/结果反馈 的最终端到端复测。
### 推进记录：GUI/Skill Runner 稳定化收口（2026-06-29）

本轮从 MaClaw App 全链路角度，优先清理会阻断“制作、测试、上传、安装依赖 Skill、运行 App”的 GUI/Skill Runner 基础合同问题，结果如下：

- 上传前可移植性自动修复：本地已管理 Skill 的自动修复写回改为使用本地扫描语义，不再被安装级 community 信任降级误拦截；仍保留安全扫描和失败回滚。
- Windows 命令文件预检：修复 `exit /b` 等短斜杠 shell 选项被误判为缺失文件的问题，避免 pipeline 失败节点在执行前被错误拦截。
- 参数推断合同：raw `input` 不再直接猜业务字段，避免动态 App 表单输入被误解释；GUI 自然语言入口通过 `_skill_infer_natural_prompt` 显式启用 prompt 兜底推断，并允许 pipeline 子 Skill 继承该上下文。
- pipeline continue-on-fail：失败节点的捕获结果可继续传递给后续节点，支持审批/企业流程中“失败后补救节点”继续运行。
- 恢复上下文：内部恢复提示固定使用稳定简体上下文，避免受全局 UI locale 顺序影响。
- 远程激活测试收尾：增加测试专用后台连接跳过开关，避免轻量 enroll payload 测试启动重后台导致 Windows 临时目录清理失败。
- SkillRunner 测试收尾：统一等待 runner 完成后的短暂收尾窗口，降低 Windows 文件句柄释放导致的临时目录清理失败。

已验证：

```powershell
go test ./gui -count=1 -vet=off -run "TestHandleIMMessage_ResumeSlotUsesBoundResumeContext|TestToolUploadSkill_PortabilityGateAutoFixesInDirPath|TestToolUploadSkill_PortabilityGateRollsBackAutoFixWhenBlocked|TestSkillRunnerStartRun_PipelineCarriesTextAliasToSubSkills|TestSkillRunnerStartRun_PipelineContinueOnFailPropagatesFailedCapture|TestApplySkillRunInputInference_DoesNotGuessRequiredCityFromInput|TestFormatScanReportForUser_DelegatesToShared|TestSkillExecutorExecuteSkillStepsWithArgs_FillsRequiredCityFromUserPrompt|TestSkillExecutorExecuteWithArgs_PipelineContinueOnFailCountsSuccess" -timeout 180s

go test ./corelib/skill -count=1 -vet=off -run "TestApplyRunInputInference|Test.*FileReference|Test.*Pipeline|TestFormatScanReport" -timeout 180s

go test ./gui -count=1 -vet=off -run "TestHandleIMMessage_ResumeSlotUsesBoundResumeContext|TestActivateRemote_SendsNormalizedPlatform|TestSkillExecutorExecuteWithArgs_PipelinePropagatesInputForChildInference" -timeout 180s

go test ./gui -count=1 -vet=off -run "TestActivateRemote_RemovesStaleHubProviderWhenOfficialServiceAuthorizationFails|TestSkillRunnerUsesIsolatedWorkspaceForSkillDir|TestSkillRunnerStartRun_ExecutesPipelineSkillWithoutSteps" -timeout 180s

go test ./gui -count=1 -vet=off -timeout 480s -json > gui-test-latest-22.json
```

当前状态：`./gui` 已完整通过。下一步应从稳定的 GUI/Skill Runner 底座回到 MaClaw App 全链路开发，重点补齐 DataSrv 到 App Studio、App 动态界面布局保存、测试证据、Hub 上传审核、依赖 Skill 安装检查、企业审批型应用实例管理与结果反馈的端到端验证。
### 推进记录：GUI 全量稳定与 DataSrv 动态布局回流补强（2026-06-29）

本轮先收掉继续推进 MaClaw App 全链路前的 GUI 包级阻塞：

- `TestCacheVEA2ADetailAsyncCoalescesConcurrentRefreshes` 不再在测试中触发真实 embedding/GGUF 模型后台加载，避免本机模型文件和 mmap 状态影响 GUI 全量回归。
- `waitSkillRunDoneForTest` 在 Skill Runner 终态后等待 active run 收尾并给 Windows 文件句柄短暂释放窗口，避免 pipeline usage/config 写入未完全结束时 `t.TempDir` 清理失败。
- 重新执行 `go test ./gui -count=1 -vet=off -timeout 480s -json > gui-test-latest-25.json`，GUI 全量通过。

随后回到 DataSrv -> MaClaw App 的动态界面回流契约：

- DataSrv `app_installations.metadata.workspace_layout` 现在在保存完整 `regions` 时，如果 App Studio 没有显式写 `primaryRegion/outputRegion`，会根据 region role 自动推导主工作区和输出区 placement。
- 推导结果同时写回完整 `workspace_layout.primaryRegion/outputRegion` 和轻量摘要 `workspace_layout_primary_region/workspace_layout_output_region`，保持 DataSrv 往返、能力发现、GUI 恢复传统软件式工作台时都能定位主区域和结果区域。
- 新增 `TestUpsertAppInstallationInfersWorkspacePrimaryAndOutputRegions`，并回归 `go test ./structureddata -count=1 -vet=off -run "Test.*AppInstallation" -timeout 240s` 通过。

这一步补的是“App Studio 保存完整动态 layout -> 安装注册到 DataSrv -> GUI/能力发现回流仍能恢复主工作区和结果区”的契约细节，继续服务企业审批型/企业普通型 App 的完整制作、安装和运行闭环。
### 推进记录：市场安装记录展示完整结果证据摘要（2026-06-29）

本轮继续从“能力市场安装 -> 本地安装审计 -> 二次运行/复测/发布”的链路补强可见性。此前安装记录和安装完成反馈已经能保存并回灌 workspace layout、result contract、test evidence、dependency verification、version snapshot，但市场页证据快照只展示 run/protocol 和依赖摘要，没有把审批实例、输出块、文件产物、resultPayload 这些结果反馈证据展示给操作者。

已完成：

- GUI 前端 `InstallRecordEvidenceSnapshot` 现在会从 `test_evidence.approvalInstance / approval_instance` 读取实例 ID、状态和当前节点，并在安装记录证据快照中显示。
- 同一证据快照会展示 outputs 数量、artifacts 数量，以及 resultPayload 的关键字段名，帮助确认审批结果、文档输出和结构化内容没有在 Hub/DataSrv/本地审计往返中丢失。
- 扩展 `AppsPage.test.tsx` 市场安装记录用例，覆盖审批实例、输出、产物和结构化结果摘要的展示。

对应全链路意义：

能力市场安装 MaClaw App
  -> 写入本地安装审计和 DataSrv app_installations
  -> 市场页最近安装记录可直接查看依赖、版本、动态布局、测试证据和结果证据包
  -> 企业审批型应用安装后可以确认“审批实例 + 结果反馈”证据仍完整
  -> 后续复测、运行诊断和再次发布有更清晰的人工核验证据入口
### 推进记录：DataSrv 扁平审批实例证据契约补齐（2026-06-29）

本轮继续补企业审批型应用的安装后恢复和外部系统对接契约。此前 DataSrv 已能保存完整 `test_evidence.approval_instance`，但公开 metadata 摘要和扁平字段归一化只覆盖 approval_id、record_id、status、view_verified 等少数字段；外部企业系统如果只写扁平 metadata，就无法稳定恢复 current node、workflow skill、business/result status、dataset/object/event/detail URL 等发布门禁和审批实例视图需要的关键事实。

已完成：

- DataSrv `normalizeAppInstallationTestEvidenceMetadata` 支持从完整 `approval_instance` 提取并保存：`test_evidence_approval_current_node`、`test_evidence_workflow_skill_id`、`test_evidence_workflow_version`、`test_evidence_business_status`、`test_evidence_result_status`、`test_evidence_dataset_id`、`test_evidence_blueprint_id`、`test_evidence_object_role`、`test_evidence_approval_event`、`test_evidence_approval_workflow_id`、`test_evidence_detail_url`。
- 当只有扁平字段而没有完整 `approval_instance` 时，DataSrv 会合成 `test_evidence.approval_instance`；如果只有 `approval_id`，会把它作为 `instance_id` 兜底，保证 GUI/Hub 能定位远端审批实例。
- DataSrv OpenAPI `app_installations.metadata` schema 显式声明上述审批实例摘要字段，避免它们继续作为未文档化 metadata。
- 新增 `TestUpsertAppInstallationBuildsApprovalEvidenceFromFlatSummaries`，覆盖外部系统只写扁平审批证据的安装记录恢复场景。

已验证：

```powershell
cd D:\workprj\aicoder\datasrv
go test ./structureddata -count=1 -vet=off -run "Test.*AppInstallation" -timeout 240s
go test ./structureddata -count=1 -vet=off -run "Test.*OpenAPI|TestOpenAPI.*" -timeout 180s
```

对应全链路意义：

企业审批型 App 安装 / DataSrv 注册
  -> 外部系统或 Hub 可写完整 approval_instance，也可写扁平审批摘要
  -> DataSrv 归一化为同一份完整审批实例证据
  -> GUI DataSrv capabilities 回流、审批 lane、发布门禁和安装审计能看到 current node、workflow skill、业务状态、结果状态和远端详情定位
  -> “数据录入 + 审批 workflow Skill 运行 + 审批实例数据管理 + 结果反馈”的安装后恢复证据更完整
### 推进记录：Hub MaClaw App 审批证据 metadata 与 DataSrv 对齐（2026-06-29）

本轮继续补“App Studio 上传 Hub -> 企业能力市场审核/搜索 -> 下载重装 -> DataSrv/GUI 恢复”的证据链。DataSrv 已经能归一化完整审批实例摘要，但 Hub 提交 MaClaw App 包时，capability metadata 仍只提取 approval_id、record_id、status、view_verified 等少数字段；这样市场审核、搜索结果和安装审计只能看到部分审批事实，无法稳定判断 current node、workflow skill、business/result status 等企业审批型应用门禁字段。

已完成：

- Hub `applyEnterpriseMaclawAppTestEvidenceMetadata` 从 `governance.testEvidence.approvalInstance / approval_instance` 额外提取并保存：`test_evidence_approval_current_node`、`test_evidence_workflow_skill_id`、`test_evidence_workflow_version`、`test_evidence_business_status`、`test_evidence_result_status`、`test_evidence_dataset_id`、`test_evidence_blueprint_id`、`test_evidence_object_role`、`test_evidence_approval_event`、`test_evidence_approval_workflow_id`、`test_evidence_detail_url`。
- 扩展 `TestCapabilityMaclawAppSubmitCreatesPendingReviewCapability`，断言 Hub capability metadata 保留审批实例 workflow/result 摘要，同时原始 manifest 本体仍完整保存 approvalInstance 内部 resultPayload、outputs、artifacts。
- 回归 GUI `AppsPage.test.tsx`，确认前端市场安装、DataSrv 回流、发布门禁相关主测试仍通过。

已验证：

```powershell
go test ./hub/internal/httpapi -count=1 -vet=off -run "TestCapabilityMaclawAppSubmitCreatesPendingReviewCapability|TestCapabilityMaclawAppPackageDownloadReturnsApprovedPack|TestAdminCapabilityMaclawAppReviewApprovesCurrentVersion" -timeout 240s
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

对应全链路意义：

App Studio 测试并上传企业审批型 App
  -> Hub 保存原始 maclaw.app.v1 包本体
  -> Hub capability metadata 暴露同一套审批实例关键事实
  -> 市场审核、搜索摘要、安装审计可判断 workflow skill、current node、业务状态和结果状态
  -> 下载重装后 GUI/DataSrv 不只依赖原始包，也能从 metadata 层完成一致诊断

### 推进记录：运行时依赖门禁统一 App 身份匹配（2026-06-29）

本轮继续从“安装后到真实运行前”的链路补齐 MaClaw App 依赖健康检查。此前发布检查已经会同时使用真实 App ID、面板包装 ID 和 DataSrv appID 做依赖验证，但运行时阻断、workflow contract issue、governance review issue 和依赖不可用提示仍存在只按单一 canonical ID 查询的路径。对于 DataSrv 回流、Hub/市场重装或企业 App 保留独立 DataSrv appID 的场景，可能造成依赖验证证据存在但运行门禁匹配不一致。

已完成：

- GUI 前端运行时阻断 `runtimeInstallPlanBlocked` 改为使用 `appDependencyVerificationAppIDs(app)`，统一覆盖 canonical manifest ID、本地面板 App ID 和 `manifest.datasrv.appID`。
- workflow contract issue 与 governance review issue 的 App 定位改为同一组身份键，避免企业审批型 App 在 DataSrv/Hub 往返后误判或漏判审核问题。
- 依赖不可用提示增加多 App ID 查询路径，阻断后能优先显示实际命中的缺失/阻断 Skill，而不是退化成泛化缺依赖提示。
- 保留原单 App ID helper，避免影响市场页、预览区等已有调用；运行门禁使用新的多 ID helper。`AppsPage.test.tsx` 新增覆盖面板 App ID 与 `manifest.datasrv.appID` 不一致时，后端依赖计划仅按 DataSrv appID 返回阻断也必须拦截运行。

已验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
# Test Files 1 passed, Tests 195 passed
```

对应全链路意义：

```text
DataSrv / Hub / 市场安装回流 MaClaw App
  -> 前端恢复 AppEntry 与 manifest
  -> 运行前调用 PlanMaclawAppInstall 做依赖与契约复核
  -> 使用同一组 App 身份匹配依赖、workflow contract、governance review
  -> 缺失或阻断 Skill 时稳定拦截运行，并显示具体阻断 Skill
  -> 通过后再进入审批 workflow Skill 运行和审批实例写入
```

### 推进记录：上传包只携带当前定义匹配的测试证据（2026-06-29）

本轮继续收紧“App Studio 测试 -> 提交包预览 -> 上传 Hub/能力市场”的证据门禁。此前发布页会通过 freshness check 阻止旧运行证据提交，但底层 `appToManifest` / `appGovernanceForManifest` 在生成提交包 JSON 时仍可能回退使用 `latestAvailableAppRunEvidence` 或顶层 `importedRunEvidence`，导致提交包预览里出现已经和当前 App 定义不匹配的旧 run evidence。虽然按钮会禁用，这种包本体语义仍不干净，也容易误导后续本地队列、人工复制包、Hub 审核记录。

已完成：

- `appGovernanceForManifest` 现在只使用 `latestAppRunEvidence(app)`，即 definition fingerprint 与当前 App 定义匹配的证据；旧证据仍可用于 UI 提示 stale，但不会进入 governance.testEvidence。
- `appToManifest` 顶层 `importedRunEvidence` 同步收紧为当前定义匹配证据，避免 DataSrv/Hub 安装回流后的旧证据被重新打包上传。
- 扩展 stale layout 用例：动态 UI layout 变更后，发布页不仅禁用提交并提示 “Run evidence is stale”，提交包预览也不得包含旧 run id、`importedRunEvidence` 或旧 `definitionHash`。

已验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
# Test Files 1 passed, Tests 195 passed
```

对应全链路意义：

```text
App Studio 调整动态 UI / result contract / test protocol
  -> 当前 App 定义指纹变化
  -> 旧运行证据只用于提示需要重测
  -> 提交包预览和上传包本体不再携带旧证据
  -> 用户重新测试后，只有匹配当前定义的 run evidence、依赖验证、审批实例证据进入 Hub 审核
```
### 推进记录：DataSrv 回流审批实例按当前视图安全归类（2026-06-29）

本轮继续补齐“企业审批型应用安装并运行后，审批实例从 DataSrv 回流 GUI”的实例管理闭环。审批型应用不能只负责发起，还必须稳定展示我的申请、待我审批、已处理、需关注和全部视图。此前已能从 DataSrv `RecordApproval` 恢复审批实例，但在同一用户同时是发起人和当前处理人的场景下，`pending_my_approval` 查询结果可能被 GUI 二次归类为 `my_requests` 后过滤掉；同时外部 DataSrv/工作流返回大写 lane/status 或多人 assignee 字符串时，归类也不够稳。

已完成：

- `normalizeMaclawAppApprovalLaneForRecordApproval` 改为把 requested lane 当作“已验证的视图上下文”：只有当前用户确实命中 assignee/reviewer/submitter 时，才按 requested lane 归类，不再无条件信任查询参数。
- 对 `pending_my_approval` 视图优先校验当前处理人，修复自提自审、代理审批、回退节点等场景里待办被错误归入我的申请导致列表漏项的问题。
- `maclawAppApprovalActorMatches` 支持逗号、分号、竖线和空白分隔的多人 assignee/reviewer 字符串，适配 DataSrv 或 workflow skill 返回多个候选处理人的情况。
- `normalizeMaclawAppApprovalLane`、`normalizeMaclawAppApprovalLaneFilter`、`normalizeMaclawAppApprovalStatus` 改为大小写不敏感，DataSrv 返回 `PENDING_MY_APPROVAL` / `PENDING` / `Approved` 时仍能归一到 GUI 固定视图。
- 新增后端测试覆盖：自提自审但当前用户仍是处理人时必须出现在 `pending_my_approval`；显式 DataSrv `approval_lane` 和 `status` 大写时必须按显式 lane/status 归一。

已验证：

```powershell
go test ./gui -count=1 -vet=off -run "TestListMaclawAppApprovalInstances(AllLoadsDataSrvLane|LoadsRequestOnlyRuntimeResult|MapsDataSrvAttentionStatus|AllInfersDataSrvLanesForCurrentUser|DoesNotTrustRequestedLaneForDataSrvItems|KeepsSelfSubmittedPendingApprovalVisible|HonorsExplicitDataSrvLaneCaseInsensitively|MergesDataSrvWithLocal)|TestMaclawAppApprovalInstancesPersistAndFilter|TestMaclawAppApprovalPendingRequestAlsoAppearsForApprover" -timeout 240s
# ok github.com/RapidAI/CodeClaw/gui 9.144s
```

对应全链路意义：

```text
审批 workflow skill 运行并写入 DataSrv RecordApproval
  -> GUI 按当前用户恢复审批实例视图
  -> 我的申请 / 待我审批 / 已处理 / 需关注 / 全部 不互相误伤
  -> 自提自审、多处理人、DataSrv 大小写差异都能稳定显示
  -> 企业审批型应用具备真正的实例管理入口，而不是只展示发起表单
```

### 推进记录：选中安装时按 App 过滤 resolved Skill 依赖（2026-06-29）

本轮继续补齐“Hub/能力市场下载 MaClaw App -> 安装 App -> 自动安装依赖 Skill”的精确依赖链路。MaClaw App 本质是带特殊 App 数据的超级 Skill，安装时不仅要发现依赖，还要能用发布包携带的远端 Skill 标识精确安装。此前提交包已经可以写入 `resolved_dependencies`，安装计划也能读取 `install_ref`，但多 App 包在只选择安装其中一个 App 时，顶层 `resolved_dependencies` 没有按选中 App 过滤，存在未选 App 的依赖解析信息被带入安装包的风险。

已完成：

- `maclawAppSerializableResolvedDeps` 输出 `resolved_dependencies` 时写入 `app_ids`，让发布包记录每个远端 Skill 依赖属于哪些 App。
- `maclawAppPackageForSelectedAppIDs` 在筛选 `apps` 的同时同步筛选顶层 `resolved_dependencies`，只保留属于选中 App 的依赖解析信息。
- 对旧包保持兼容：如果 resolved dependency 没有 `app_ids`，则按选中 App 的实际 dependency id 判断是否保留。
- 新增 `maclawAppResolvedDependenciesForSelectedEntries`，将“选中 App -> 依赖解析信息”过滤逻辑集中在后端包筛选层，避免 GUI/Hub/安装记录各自猜测。
- 新增测试覆盖：多 App 包只选 `expense-app` 安装时，只保留 `expense-workflow` 的 `install_ref`，`contract-workflow` 不进入安装计划；resolved deps 序列化必须携带 `app_ids`。

已验证：

```powershell
go test ./gui -count=1 -vet=off -run "Test(InstallMaclawAppDependenciesInstallsHubBackedSources|InstallMaclawAppDependenciesPreservesInstallRefFromDuplicateDependency|SelectedMaclawAppPackageFiltersResolvedDependencies|MaclawAppSerializableResolvedDepsIncludesAppIDs|InstallSelectedMaclawAppPackageFromHubFiltersPackageApps|PlanMaclawAppInstallTreatsBindingSkillAsDependency|PlanMaclawAppInstallScopesSharedDependencyRequirementPerApp)" -timeout 240s
# ok github.com/RapidAI/CodeClaw/gui 11.508s
```

对应全链路意义：

```text
App Studio 本地测试并提交 MaClaw App 包
  -> 提交包写入 resolved_dependencies + app_ids + install_ref
  -> Hub/能力市场分发多 App 包
  -> 用户选择安装其中一个 App
  -> 后端同时过滤 apps 和 resolved_dependencies
  -> InstallMaclawAppDependencies 只安装当前 App 需要的 Skill
  -> 依赖 Skill 从 Hub/SkillMarket 按 install_ref 精确解析，避免跨 App 污染
```

### 推进记录：Hub 选中安装到依赖安装与 DataSrv 注册端到端验证（2026-06-29）

本轮继续把 MaClaw App 的“能力市场分发 -> 安装 -> 依赖 Skill 安装 -> 安装审计 -> DataSrv 注册”串成可验证链路。上一轮已补齐多 App 包选中安装时 `resolved_dependencies` 的过滤和 `app_ids` 归属，本轮进一步用后端端到端测试证明这些数据会真实进入 Hub 下载安装流程，而不是只停留在 helper 层。

已完成：

- 新增 `TestInstallSelectedMaclawAppPackageFromHubInstallsDepsAndRegistersDataSrv`，模拟 Hub 返回包含两个 App 的包，用户只选择安装 `kept-normal`。
- 测试中注入 `InstallMixedSkill` stub，验证只安装被选中 App 的 `kept-skill`，且安装调用使用 resolved `install_ref=hub-kept-skill`；未选 App 的 `skipped-skill` 不会被安装。
- 同一测试继续走真实 `InstallSelectedMaclawAppPackageFromHub -> InstallMaclawAppDependencies -> RecordMaclawAppInstall -> registerMaclawAppInstallationsToDataSrv` 后端链路，验证安装计划、本地安装审计和 DataSrv 注册只包含选中 App。
- DataSrv 注册 payload 断言包括：`app_id=kept-normal`、企业普通应用类型、role binding、app skill metadata、依赖验证摘要、依赖 `install_ref` 和安装后 ready 状态。
- 保留安装后审计语义：首次依赖安装动作发生在 `InstallMaclawAppDependencies`，随后 `RecordMaclawAppInstall` 重新规划时依赖已本地 ready，因此 DataSrv 的 dependency verification 中 action 为 `skip`、installed 为 true，同时 `install_ref` 不丢失。

已验证：

```powershell
go test ./gui -count=1 -vet=off -run "TestInstallSelectedMaclawAppPackageFromHub(InstallsDepsAndRegistersDataSrv|FiltersPackageApps)|TestSelectedMaclawAppPackageFiltersResolvedDependencies|TestMaclawAppSerializableResolvedDepsIncludesAppIDs" -timeout 240s
# ok github.com/RapidAI/CodeClaw/gui 12.372s
```

对应全链路意义：

```text
Hub/能力市场下载多 App 包
  -> 用户选择安装其中一个 App
  -> resolved_dependencies 按 app_ids 过滤
  -> 只安装选中 App 依赖 Skill
  -> 安装后重新规划确认依赖 ready 且 install_ref 保留
  -> 本地安装审计只记录选中 App
  -> DataSrv app_installations 只注册选中 App 的 role binding、动态 UI、测试证据和依赖验证
```

### 推进记录：App Studio 可视化设计保存到 Skill 应用信息文件（2026-06-29）

本轮继续补“App Studio 制作 -> 保存应用信息文件 -> 测试证据 -> 发布包”的制作侧证据链。此前已有动态 workspace layout 保存到本地 manifest、runtime 回放、实际 workflow run evidence 进入发布包等测试，但“企业审批型 App 在 App Studio 里可视化调整布局和测试协议后，保存到 Skill 的 `maclaw.app.json` 是否完整携带这些设计产物”覆盖还偏粗。

已完成：

- 扩展前端 `saves a newly created enterprise approval app into its app skill definition` 测试。
- 测试现在模拟用户在 App Studio 中选择企业审批型 App、绑定 app skill/workflow skill、填写依赖 install ref，并可视化调整 workspace layout：`template=left_nav`、`density=compact`、`primaryRegion=center`、`outputRegion=bottom`、`result_panel` 移动到底部输出区。
- 同一测试继续编辑可复现实测协议：`sampleInput` 和 `expectedOutput`，保存到 Skill 后断言 `SaveMaclawAppDefinitionForSkill` payload 中的 `binding.ui.layouts.approval_workspace`、`binding.resultContract`、`binding.testProtocol` 都完整写入。
- 保留现有发布链路回归：动态 layout 保存到本地 manifest/runtime 回放通过；实际审批 workflow run evidence 进入发布包通过。

已验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "saves (a newly created enterprise approval app into its app skill definition|app studio layout choices into app manifests|approval app evidence from an actual workflow run)"
# Test Files 1 passed, Tests 2 passed | 193 skipped

npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "publishes approval app evidence from an actual workflow run"
# Test Files 1 passed, Tests 1 passed | 194 skipped
```

对应全链路意义：

```text
用户在 App Studio 设计企业审批型 App
  -> 选择 app skill / workflow skill / install_ref
  -> 可视化调整传统软件式 workspace layout
  -> 设置可复现实测协议 sampleInput / expectedOutput
  -> 保存到 Skill 的 maclaw.app.json
  -> 后续运行生成当前定义指纹对应的 run evidence
  -> 发布包读取同一份动态 UI、测试协议、依赖验证和审批实例证据
```

### 推进记录：Skill App 定义重新发现回流保持完整企业契约（2026-06-29）

本轮继续补齐“App Studio 制作 -> 保存到 Skill 应用信息文件 -> 再被 App 面板发现和恢复”的制作态到运行态连接点。此前 App Studio 已能把企业审批型 App 的动态 layout、result contract、test protocol、依赖和审批 workflow 绑定写入 `maclaw.app.json`，但 discovery 读回路径主要覆盖 `app.binding.ui`，对标准顶层 `app.ui / app.resultContract / app.testProtocol / app.workflow` 的回流证明不足。

已完成：

- GUI 前端 `skillDefinitionManifestToApp` 读取完整 `maclaw.app.v1` 时，同时支持标准顶层字段和 legacy binding 字段：`app.ui`、`app.resultContract`、`app.testProtocol`、`app.workflow`、`app.datasrv`、`app.mis`、`app.appSkill`、`app.dependencies`。
- 企业 App 重新发现时继续保持真实 app id、`enterprise_approval_app` 类型、app skill 绑定、workflow skill 依赖、审批 binding、动态 workspace layout、result contract、test protocol 和 imported run evidence，不降级成工具型 app，也不丢失发布/运行所需定义指纹输入。
- 扩展前端回归测试，把 discovery mock 改为顶层 `app.ui/resultContract/testProtocol`，断言恢复后的本地 AppEntry 仍保留主区域/输出区、结果交付模式、样例输入和期望输出。

已验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "saves a newly created enterprise approval app into its app skill definition|restores enterprise MaClaw App skill definitions without downgrading them to tool apps|saves app studio layout choices into app manifests|publishes approval app evidence from an actual workflow run"
# Test Files 1 passed, Tests 4 passed | 191 skipped

npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
# Test Files 1 passed, Tests 195 passed
```

对应全链路意义：

```text
App Studio 保存企业审批型 App 到 Skill/maclaw.app.json
  -> Skill discovery 重新扫描已安装 Skill
  -> GUI 恢复完整企业 AppEntry
  -> 动态 UI、结果契约、测试协议、workflow/依赖/审批绑定不丢失
  -> 后续运行、重新测试、上传 Hub、安装依赖、DataSrv 注册使用同一份完整定义
```
### 推进记录：企业审批型 App Hub 安装证据闭环补强（2026-06-29）

本轮继续把“Hub 能力市场下载 -> 选中安装 -> 自动安装依赖 Skill -> 本地安装审计 -> DataSrv 注册 -> 后续恢复/复测/二次发布证据”串成可验证链路。此前已有企业普通 App 的选中安装和 DataSrv 注册测试，审批型 App 的完整 approvalInstance 证据在同一条后端安装链路中还缺专门覆盖。

已完成：

- 新增 `TestInstallSelectedMaclawApprovalAppFromHubPreservesApprovalEvidence`，模拟 Hub 返回企业审批型 App 包，包内包含 app skill、workflow skill、动态 approval workspace、workflow/result/test protocol、dependency verification 和完整 approvalInstance 运行证据。
- 测试覆盖选中安装时只安装当前 App 的 `expense-super-skill` 和 `expense-workflow`，并使用 `resolved_dependencies.install_ref` 精确安装。
- 测试覆盖安装后返回的 `install_record.install_evidence`、DataSrv 注册 `metadata.test_evidence`、本地 `ListMaclawAppInstalls()` 审计记录三处都保留 approvalInstance 的 resultPayload、outputs、artifacts、currentNode、workflowSkillId、businessStatus/resultStatus。
- GUI 后端 `applyMaclawAppDataSrvTestEvidenceMetadata` 补齐从 camelCase/snake_case approvalInstance 展开扁平 metadata：`test_evidence_approval_current_node`、`test_evidence_workflow_skill_id`、`test_evidence_workflow_version`、`test_evidence_business_status`、`test_evidence_result_status`、`test_evidence_dataset_id`、`test_evidence_blueprint_id`、`test_evidence_object_role`、`test_evidence_approval_event`、`test_evidence_approval_workflow_id`、`test_evidence_detail_url`。

已验证：

```powershell
go test ./gui -count=1 -vet=off -run "TestInstallSelectedMaclawApprovalAppFromHubPreservesApprovalEvidence" -timeout 240s
# ok github.com/RapidAI/CodeClaw/gui

go test ./gui -count=1 -vet=off -run "TestInstallSelectedMaclaw(AppPackageFromHub(InstallsDepsAndRegistersDataSrv|FiltersPackageApps)|ApprovalAppFromHubPreservesApprovalEvidence)|TestSelectedMaclawAppPackageFiltersResolvedDependencies|TestMaclawAppSerializableResolvedDepsIncludesAppIDs" -timeout 240s
# ok github.com/RapidAI/CodeClaw/gui
```

对应全链路意义：

```text
企业审批型 App 上传 Hub/能力市场
  -> 下载并选中安装
  -> 自动安装 app skill + workflow skill 依赖
  -> 安装后重新规划依赖 ready
  -> 本地安装审计保留 approvalInstance 结果包
  -> DataSrv app_installations 注册完整证据和扁平摘要
  -> GUI/DataSrv/Hub 后续恢复、复测、二次发布能定位审批实例和结果反馈
```
## 2026-06-29 推进记录：审批实例手工决策结果包补强

本轮继续补齐 MaClaw App 运行态审批闭环，重点是审批实例在手工通过、驳回、标记关注时的结果证据强度。

已完成：

```text
前端审批决策 payload 不再只保存 approval_result / business_status / result_status。
现在 result_payload 同步携带：
  current_node / approval_node
  current_node_ids / workflow_node_ids
  workflow_skill_id / workflowSkillId
  workflow_version / workflowVersion
  approval_workflow_id / approvalWorkflowID
  workflow_decision_id / workflowDecisionID
  record_id / approval_id
  dataset_id / object_role
  from_status / to_status
  text
```

意义：

```text
审批实例详情、DataSrv 同步、Hub 发布测试证据即使只拿到 result_payload，也能还原：
  这是谁的审批实例
  属于哪个 workflow skill
  当前/最终节点是什么
  审批动作从哪个业务状态流转到哪个业务状态
  关联哪个 DataSrv dataset/object/record/approval
  输出文本是什么
```

验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "records approval decisions with workflow result fields"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "keeps approval result package when a pending item is manually approved"
```

结果：两条本轮相关测试通过。

注意：完整 `AppsPage.test.tsx` 当前仍有 4 个既有 App Studio 相关失败，集中在“保存到 Skill / 上传到 SkillMarket”按钮文本和草稿 manifest 折叠状态，不属于本轮审批结果包改动，但后续推进 App Studio 链路时需要一并处理。
## 2026-06-29 推进记录：App Studio 保存与上传入口恢复

本轮继续推进 App Studio 制作、测试、上传链路，修复前端全量 AppsPage 回归中剩余的 App Studio 失败点。

已完成：

```text
App Studio 创建页重新显式提供：
  创建并保存到 Skill
  保存到 Skill
  上传到 SkillMarket
  仅添加到面板
```

其中：

```text
保存到 Skill：写入目标 Skill 的 maclaw.app.json，并把 App 作为 skill source 回灌到应用面板。
上传到 SkillMarket：在当前版本有成功运行证据后，先保存最新 maclaw.app.json，再上传绑定 Skill。
manifest 预览：默认展开，用户可以直接查看动态 UI layout、result contract、test protocol、依赖和运行契约。
```

对应全链路意义：

```text
用户在 App Studio 中可视化设计 MaClaw App
  -> 自动生成动态界面布局、结果契约、测试协议和依赖信息
  -> 保存到 Skill 形成带 App 特殊数据的超级 Skill
  -> 在应用面板运行并产生当前定义匹配的测试证据
  -> 上传到 SkillMarket / Hub 能力市场
```

验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
# Test Files 1 passed, Tests 195 passed
```
### 推进记录：Hub 下载包输出 resolved Skill 依赖（2026-06-29）

本轮继续补齐“能力市场下载 MaClaw App -> 安装 App -> 自动安装依赖 Skill”的 Hub 侧契约。此前 GUI 安装端和提交包已经能处理 `resolved_dependencies.install_ref`，但单个 MaClaw App 从 Hub 下载时，`CapabilityMaclawAppPackageHandler` 只返回 `apps`，没有在包顶层输出已解析依赖，导致安装端只能退回 manifest 内部 dependencies，无法稳定使用 Hub capability/install ref 精确安装 app skill 与 workflow skill。

本轮改动：

- `enterpriseMaclawAppSkillDependenciesForEntry` 保留依赖项里的 `install_ref / installRef`。
- 新增 Hub 侧依赖聚合逻辑，下载包顶层输出 `resolved_dependencies`。
- 每个 resolved dependency 都携带 `id`、`version`、`kind`、`required`、`source`、`install_ref`、`capabilities` 和 `app_ids`。
- `CapabilityMaclawAppPackageHandler` 在返回包前重新解析已注入审核 metadata 的 `maclaw.app.v1` entry，确保下载包里的依赖归属与实际安装 App 一致。
- 更新 `TestCapabilityMaclawAppPackageDownloadReturnsApprovedPack`，验证下载包包含 app skill 与 workflow skill 两类依赖，且保留 `install_ref`、`capabilities` 和当前 App 的 `app_ids`。

已验证：

```powershell
go test ./hub/internal/httpapi -count=1 -vet=off -run "TestCapabilityMaclawAppPackageDownloadReturnsApprovedPack|TestCapabilityMaclawAppSubmitCreatesPendingReviewCapability|TestAdminCapabilityMaclawAppReviewApprovesCurrentVersion" -timeout 240s
```

结果通过。对应全链路意义：

```text
App Studio 上传 MaClaw App
  -> Hub 审核通过并保存 maclaw.app.v1
  -> 能力市场下载 maclaw.app.pack.v1
  -> 顶层 resolved_dependencies 带 app_ids + install_ref
  -> GUI 安装端可按 Hub install_ref 精确安装 app skill / workflow skill
  -> 再进入本地安装审计、DataSrv 注册和运行证据回流
```

这一步把 Hub 单 App 下载场景与此前多 App 选中安装的依赖过滤能力对齐，减少“发布包有依赖、下载包丢 install_ref、安装端无法自动补齐 Skill”的断链风险。下一步应继续做最终样例级验收：企业审批型 App 从 App Studio 制作/保存/测试/上传，到 Hub 下载重装、依赖安装、DataSrv 注册、审批实例视图和结果反馈的完整闭环。

### 推进记录：Hub 下载重装到 DataSrv 依赖证据闭环（2026-06-29）

本轮继续把“Hub 下载包输出 resolved_dependencies”接到 GUI 安装和 DataSrv 注册链路上。上一轮 Hub 下载包已经能输出 `resolved_dependencies.install_ref/app_ids`，但 GUI 安装后写入 DataSrv 的 `dependency_verification` 仍可能优先使用包内旧的 governance 证据；如果旧证据缺少 `install_ref`，DataSrv 安装记录就无法证明本次重装实际按 Hub capability/install ref 精确安装了 app skill 与 workflow skill。

已完成：

- GUI `maclawAppDependencyVerificationMetadataForEntry` 在保留包内原始 governance `dependencyVerification` 的同时，会用本次安装计划补齐 dependencies 明细。
- 补齐字段包括 `install_ref`、`source`、`version`、`app_ids`、`installed`、`health`、`action`、版本状态和本地安装状态等。
- 企业审批型 Hub 安装测试增加断言：选中安装后的 `result.package.resolved_dependencies` 只保留当前 App 的依赖，并保留 app skill / workflow skill 的 `install_ref`。
- 同一测试增加 DataSrv 注册断言：`metadata.dependency_verification.dependencies` 必须携带 `hub-expense-super-skill` 和 `hub-expense-workflow`，证明 DataSrv 侧也能看到真实安装依赖证据。

已验证：

```powershell
go test ./gui -count=1 -vet=off -run "TestInstallSelectedMaclawApprovalAppFromHubPreservesApprovalEvidence" -timeout 240s

go test ./gui -count=1 -vet=off -run "TestInstallSelectedMaclaw(AppPackageFromHub(InstallsDepsAndRegistersDataSrv|FiltersPackageApps)|ApprovalAppFromHubPreservesApprovalEvidence)|TestSelectedMaclawAppPackageFiltersResolvedDependencies|TestMaclawAppSerializableResolvedDepsIncludesAppIDs" -timeout 240s

go test ./hub/internal/httpapi -count=1 -vet=off -run "TestCapabilityMaclawAppPackageDownloadReturnsApprovedPack|TestCapabilityMaclawAppSubmitCreatesPendingReviewCapability|TestAdminCapabilityMaclawAppReviewApprovesCurrentVersion" -timeout 240s
```

结果均通过。对应全链路意义：

```text
Hub approved MaClaw App package
  -> package.resolved_dependencies 带 app_ids + install_ref
  -> GUI 选中安装时过滤当前 App 的 resolved deps
  -> 自动安装 app skill / workflow skill
  -> 安装计划生成实际依赖状态
  -> 本地安装审计和 DataSrv app_installations 都保留 install_ref 证据
  -> 后续 GUI/DataSrv/Hub 回流可证明依赖不是“看起来已安装”，而是按市场引用完成安装
```

这一步让企业审批型 App 的“下载重装 -> 依赖安装 -> DataSrv 注册 -> 审批实例证据回流”更接近最终验收闭环。下一步继续推进最终真实样例运行验收：从 App Studio 制作保存一个企业审批型 App，产生当前定义匹配的审批实例运行证据，上传 Hub，下载重装后在 GUI 中查看我的申请/待我审批/已处理/需关注与结果反馈。

### 推进记录：审批中心详情展示结果包闭环（2026-06-29）

本轮继续从用户可见界面验证企业审批型 App 的实例管理和结果反馈。此前审批中心已经能按“我的申请 / 待我审批 / 已处理 / 需关注 / 全部”加载实例，也能在数据结构中携带 resultPayload、outputs、artifacts；但测试主要覆盖状态、节点、审批动作和 DataSrv 同步，缺少“用户在审批实例详情里确实能看到结果包”的明确断言。

已完成：

- 扩展前端审批中心测试 `keeps approval instance detail scoped to the selected lane and row`。
- 测试数据中的待审批实例现在包含完整结果包：`result_payload.business_record`、审批输出块 `outputs`、文件产物 `artifacts`。
- 断言审批详情区可见：`结果包`、输出标题、附件文件名、`结构化数据`、`business_record` 和业务记录 ID。
- 同一测试仍保留原有 lane 切换、选中行、远端审批链接、审批动作可用性等断言，证明结果包展示不会破坏审批实例管理交互。

已验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "keeps approval instance detail scoped to the selected lane and row"

npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "keeps approval instance detail scoped to the selected lane and row|records approval decisions with workflow result fields|records DataSrv sync failures in approval instance timelines|shows the approval instance workspace for approval apps|keeps approval result package when a pending item is manually approved"
```

结果通过。对应全链路意义：

```text
企业审批型 App 运行 / DataSrv 回流审批实例
  -> 审批中心按 lane 加载实例
  -> 用户选中实例
  -> 详情区显示当前节点、业务记录、workflow skill、远端审批 ID
  -> 同一详情区显示 result payload、输出块和文件产物
  -> 审批动作继续生成完整结果包并同步 DataSrv
```

这一步补的是“审批实例数据管理 + 结果反馈”在 GUI 上的可见验收点，不再只依赖后端安装审计或隐藏 JSON 证明。下一步继续推进最终样例：把 App Studio 制作/保存/测试/上传 Hub 与下载重装后的 GUI 审批中心视图串成一条更完整的端到端验收。

### 推进记录：发布包保留完整 App 超级 Skill 定义（2026-06-29）

本轮继续收口 App Studio 制作/测试/上传链路。企业审批型 MaClaw App 不只是普通 Skill 的表单封装，而是带动态 UI、DataSrv/MIS 绑定、workflow 绑定、结果契约、测试协议、运行证据和依赖信息的超级 Skill。此前发布包能带 `app.ui`、`binding.*`、governance 和审批实例运行证据，但标准顶层 `app.resultContract / app.testProtocol / app.workflow` 在 `appToManifest` 中没有输出，导致发布包对“完整 app.ui/app.workflow/app.resultContract/app.testProtocol”定义的证明不够强。

已完成：

- 前端 `appToManifest` 输出企业 App manifest 时，除继续保留 `binding.*` 兼容字段外，同步写出标准顶层字段：
  - `app.datasrv`
  - `app.mis`
  - `app.appSkill`
  - `app.dependencies`
  - `app.ui`
  - `app.resultContract`
  - `app.testProtocol`
  - `app.workflow`
- 扩展“实际 workflow run evidence 发布”测试：提交审核包后断言发布包同时保留动态 UI layout、workflow 节点映射、result contract、test protocol、app skill/workflow skill 依赖 install_ref、governance workspaceLayout/workflowContract/testProtocol、dependencyVerification，以及完整 approvalInstance 结果证据。
- 测试样例补齐 `maclaw.app.test_protocol.v1`，使发布包明确包含当前定义匹配的测试协议，而不是只依赖运行历史中的 testProtocolFingerprint。

已验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "publishes approval app evidence from an actual workflow run"

npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "publishes approval app evidence from an actual workflow run|saves a newly created enterprise approval app into its app skill definition|restores enterprise MaClaw App skill definitions without downgrading them to tool apps|saves app studio layout choices into app manifests"
```

结果通过。对应全链路意义：

```text
App Studio 设计企业审批型 App
  -> 保存动态 UI / workflow / result contract / test protocol
  -> 运行 workflow skill 产生审批实例和结果包
  -> 当前定义匹配的 run evidence 进入本地运行历史
  -> Review / publish 生成 maclaw.app.pack.v1
  -> 发布包 app 顶层保留完整超级 Skill 定义
  -> governance 同时保留审核、依赖验证和 approvalInstance 证据
  -> Hub 审核/下载/安装端不再只能从 binding 或历史字段推断 App 合同
```

这一步把制作侧“动态 UI + workflow + 测试协议 + 运行证据 + 依赖”压成同一个发布包契约。下一步继续推进最终整链验收：把该发布包经 Hub 下载重装后，再回到 GUI 审批中心和 DataSrv app_installations 中验证同一份定义与结果证据。
### 推进记录：DataSrv 安装回流保留企业普通应用超级 Skill 定义（2026-06-29）

本轮继续补齐“Hub 下载重装 -> DataSrv app_installations -> GUI/App Studio 重新发现”的回流验收点。审批型 App 已经有较完整的 DataSrv 回流覆盖，但企业普通应用同样是 MaClaw App 超级 Skill：它虽然没有审批实例管理和 workflow 审批中心，但仍必须保留 app skill、依赖 Skill、动态 UI 布局、结果契约、测试协议和运行证据。

已完成：

- 扩展前端测试 `restores DataSrv installed enterprise normal app run evidence into app candidates`。
- DataSrv 安装样例现在携带普通企业应用的 `metadata.dependencies`，包括 app skill 的 `install_ref` 和能力标签。
- DataSrv 安装样例现在携带传统软件式 `workspace_layout`：业务工作台入口、紧凑布局、输入区、记录列表区、输出区、导航项和列表列配置。
- GUI 从 DataSrv capabilities 发现并添加该应用后，测试断言本地保存的 `manifest.dependencies.skills` 保留 `install_ref`，`manifest.ui.layouts.business_workspace` 保留布局、导航、列表列、regions，并标记 `studio.importedFromDataSrv=true`。
- 同时保留既有断言：`manifest.datasrv`、`manifest.appSkill`、`manifest.resultContract`、`manifest.testProtocol`、`importedRunEvidence`、`installEvidence` 均不丢失。

已验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "restores DataSrv installed enterprise normal app run evidence into app candidates"

npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "restores DataSrv installed enterprise normal app run evidence into app candidates|turns DataSrv installed MaClaw apps into addable app candidates with layout metadata"
```

结果通过。对应全链路意义：

```text
Hub / App Studio 生成企业普通应用
  -> 安装后注册到 DataSrv app_installations
  -> DataSrv 返回 app skill、依赖 Skill、动态 UI、结果契约、测试协议和运行证据
  -> GUI/App Studio 重新发现并加入面板
  -> 企业普通应用不被降级成临时 DataSrv 表单，也不丢失能力市场 install_ref
  -> 后续复测、二次发布和运行时依赖检查仍使用同一份超级 Skill 定义
```

这一步把企业审批型和企业普通型的 DataSrv 回流语义对齐：审批型额外有审批实例/结果包/审批中心视图，普通型则保留一次性企业操作的动态工作台、依赖和测试证据。下一步继续做最终样例级验收，把 App Studio 制作、Hub 发布下载、依赖安装、DataSrv 注册、GUI 回流和运行视图串成一条更完整的可执行链路。
补充收口：同轮全量 `AppsPage.test.tsx` 回归发现一处普通应用/审批应用发布断言串线：企业普通应用发布用例误用了审批型 App 的 `approval_workspace/workflow` 期望，审批型发布用例随后也需要恢复自己的审批模型断言。已将两类发布测试重新拆回各自产品语义：

- 企业普通应用发布包断言 `enterprise_normal_app`、`business_workspace`、`business_status`、DataSrv binding 和 `runtime_skill` 依赖。
- 企业审批型应用发布包断言 `enterprise_approval_app`、`approval_workspace`、workflow mapping、workflow contract、test protocol、app skill + workflow skill 依赖。

追加验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "publishes approval app evidence from an actual workflow run|publishes enterprise normal app evidence from an actual DataSrv business run"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

结果：`AppsPage.test.tsx` 195 个用例全部通过。这说明当前 App 面板/App Studio/发布包/DataSrv 回流这层前端合同已经重新回到一致状态，后续可以继续向样例级端到端验收推进。
### 推进记录：GUI 安装注册主动写入动态布局区域摘要（2026-06-29）

本轮继续补“Hub 下载重装 -> GUI 安装 -> DataSrv app_installations 注册 -> GUI/App Studio 回流”的细节契约。DataSrv 服务端已经会从 `metadata.workspace_layout` 归一化出 `workspace_layout_primary_region / workspace_layout_output_region`，OpenAPI 也把它们声明为稳定字段；但 GUI 安装注册端此前只主动写 `entry/template/density/navigation/list_columns`，主工作区和输出区摘要主要依赖 DataSrv 兜底归一化。

已完成：

- GUI `maclawAppDataSrvInstallationPayloads` 在构造 DataSrv 注册 metadata 时，从完整 `workspace_layout.primary_region/primaryRegion` 与 `output_region/outputRegion` 主动写入：
  - `workspace_layout_primary_region`
  - `workspace_layout_output_region`
- 企业普通应用 Hub 安装注册测试增加断言：DataSrv 注册 metadata 必须同时携带完整 `workspace_layout` 和扁平主/输出区域摘要。
- 企业审批型 App DataSrv 注册测试增加断言：审批工作台 `approval_workspace` 的 `primary/output` 区域摘要必须随安装注册写入。

已验证：

```powershell
go test ./gui -count=1 -vet=off -run "TestInstallMaclawAppPackageFromHubInstallsDepsAndRegistersDataSrv|TestRecordMaclawAppInstallRegistersApprovalAppWithDataSrv|TestInstallSelectedMaclawApprovalAppFromHubPreservesApprovalEvidence" -timeout 240s

go test ./gui -count=1 -vet=off -run "TestInstallSelectedMaclaw(AppPackageFromHub(InstallsDepsAndRegistersDataSrv|FiltersPackageApps)|ApprovalAppFromHubPreservesApprovalEvidence)|TestInstallMaclawAppPackageFromHubInstallsDepsAndRegistersDataSrv|TestRecordMaclawAppInstallRegistersApprovalAppWithDataSrv|TestSelectedMaclawAppPackageFiltersResolvedDependencies|TestMaclawAppSerializableResolvedDepsIncludesAppIDs" -timeout 240s
```

结果通过。对应全链路意义：

```text
App Studio 保存动态 workspace layout
  -> Hub/市场包保留完整 ui/layout
  -> GUI 下载/安装并注册 DataSrv app_installations
  -> DataSrv metadata 同时拥有完整 workspace_layout 与轻量 primary/output 摘要
  -> 外部系统、OpenAPI 调用方、GUI 能力发现和 App Studio 回流都能稳定定位主工作区与结果区
  -> 企业普通应用和企业审批型应用的传统软件式动态界面位置不会只依赖服务端兜底推断
```

这一步进一步收紧“所有 MaClaw App 界面动态生成，用户调节位置后保存并经 Hub/DataSrv 往返恢复”的安装侧合同。下一步继续向最终样例级端到端验收推进，重点把制作、测试、上传、下载重装、依赖安装、DataSrv 注册、GUI 回流和运行视图合并到一组更完整的验收场景。
### 推进记录：DataSrv 安装审计保留审批实例关键事实（2026-06-29）

本轮继续补“GUI 安装注册 -> DataSrv 接收归一化 -> 能力发现/审计回流”的合规与排障链路。此前 DataSrv 已能把企业审批型 App 的扁平审批证据归一化为完整 `test_evidence.approval_instance`，OpenAPI 也声明了 current node、workflow skill、business/result status 等字段；但 `app.installation_upsert` 审计 metadata 白名单还只保留 approval id、record id、status、view verified 等基础字段，没有把这些审批实例关键事实写入审计摘要。

已完成：

- DataSrv `appInstallationAuditMetadata` 审计白名单补入审批实例关键摘要：
  - `test_evidence_approval_current_node`
  - `test_evidence_workflow_skill_id`
  - `test_evidence_workflow_version`
  - `test_evidence_business_status`
  - `test_evidence_result_status`
  - `test_evidence_dataset_id`
  - `test_evidence_blueprint_id`
  - `test_evidence_object_role`
  - `test_evidence_approval_event`
  - `test_evidence_approval_workflow_id`
  - `test_evidence_detail_url`
- 扩展 `TestUpsertAppInstallationBuildsApprovalEvidenceFromFlatSummaries`，除了验证 metadata 和合成的 `approval_instance`，现在也验证审计 metadata 保留 current node、workflow skill、业务状态、结果状态、dataset/object/event/detail URL。

已验证：

```powershell
cd D:\workprj\aicoder\datasrv
go test ./structureddata -count=1 -vet=off -run "TestUpsertAppInstallationBuildsApprovalEvidenceFromFlatSummaries|TestAppInstallationOpenAPISchemaDocumentsFullTestEvidence|TestUpsertAppInstallationInfersWorkspacePrimaryAndOutputRegions" -timeout 180s

go test ./structureddata -count=1 -vet=off -run "Test.*AppInstallation|Test.*Capabilities.*App|TestHTTP.*AppInstallation" -timeout 240s
```

结果通过。对应全链路意义：

```text
企业审批型 MaClaw App 安装注册到 DataSrv
  -> DataSrv 归一化完整 approval_instance 与扁平摘要
  -> 能力发现/API 回流能恢复审批实例、workflow skill、业务状态和结果状态
  -> app.installation_upsert 审计日志也保留同一批关键事实
  -> 企业应用安装、审批实例管理、结果反馈和合规排障使用同一套证据
```

这一步把 DataSrv 侧“可运行、可回流”继续推进到“可审计”。下一步继续向最终样例级端到端验收推进，把 App Studio 制作、测试、上传 Hub、下载重装、依赖安装、DataSrv 注册、GUI 回流和运行视图合并验证。
### 推进记录：DataSrv HTTP 层验证 GUI 等价安装回流（2026-06-29）
本轮继续补齐“GUI 安装注册 -> DataSrv 接收归一化 -> capabilities 回流 -> GUI/App Studio 可恢复”的跨模块验收点。此前 GUI 侧安装注册测试和 DataSrv 内部 upsert/capabilities 测试已经覆盖大量字段，但还缺一条更贴近真实 GUI 安装行为的 HTTP 层 PUT 验证：按 appId 覆盖注册企业审批型 MaClaw App，并从 `/api/v1/data/capabilities` 读回同一份动态 UI、依赖安装证据、审批实例和结果反馈。

已完成：

- 新增 DataSrv HTTP 测试 `TestHTTPServerAppInstallationPUTRoundTripsGUIEquivalentPayloadThroughCapabilities`。
- 测试使用 GUI 等价 `PUT /api/v1/data/app-installations/expense-approval` payload，包含：
  - `enterprise_approval_app` 类型和 `role_bindings`。
  - `version_snapshot` 中的 app skill、workflow skill 与 approval binding。
  - `dependencies` 和 `dependency_verification.dependencies` 中的 `install_ref`。
  - `workspace_layout` 中的 `approval_workspace`、navigation、list columns、regions、primary/output region。
  - `workflow_mapping`、`workflow_contract`、`result_contract`。
  - `test_evidence.approval_instance`、`result_payload`、`outputs`、`artifacts`、`result_coverage`。
- DataSrv 归一化逻辑补齐从 `test_evidence.result_payload` 提升稳定摘要：
  - `test_evidence_business_status`
  - `test_evidence_result_status`
- 测试断言 capabilities 回流后仍可看到：
  - 动态 UI layout 主区/输出区和导航。
  - app skill / workflow skill 的 `install_ref`。
  - 审批实例 ID、当前节点、workflow skill、detail URL。
  - 结果覆盖类型与业务/结果状态摘要。

已验证：

```powershell
cd D:\workprj\aicoder\datasrv
go test ./structureddata -count=1 -vet=off -run "TestHTTPServerAppInstallationPUTRoundTripsGUIEquivalentPayloadThroughCapabilities" -timeout 180s

go test ./structureddata -count=1 -vet=off -run "Test.*AppInstallation|Test.*Capabilities.*App|TestHTTP.*AppInstallation" -timeout 240s
```

结果均通过。

对应全链路意义：

```text
GUI 安装企业审批型 MaClaw App
  -> 按 appId PUT 注册到 DataSrv app_installations
  -> DataSrv 归一化动态 UI、依赖 install_ref、workflow/result/test evidence
  -> capabilities 返回完整安装事实
  -> GUI/App Studio 后续可基于同一份 DataSrv 回流数据恢复应用入口、审批实例视图和结果反馈
```

这一轮把 DataSrv HTTP 边界从“内部 upsert 正确”推进到“真实安装注册接口和能力发现接口之间可闭环”。下一步继续做最终样例级 E2E：把 App Studio 制作/保存/测试/上传 Hub、Hub 审核下载、依赖安装、DataSrv 注册、GUI 审批中心视图和结果包展示串成一条可执行验收链。
### 推进记录：GUI 运行入口显示 DataSrv 回流安装证据与结果包（2026-06-29）
本轮继续推进最终样例级 E2E 的 GUI 可见验收点。此前 DataSrv capabilities 已能把企业审批型 App 的动态 UI、依赖 `install_ref`、审批实例、结果状态和输出覆盖信息回流到 GUI；但在 App 运行入口里，安装证据快照原先挂在 runtime status 区域，某些动态布局没有 detail/status 区时，用户打开 App 详情只能看到 version snapshot 和 runtime contract，看不到 DataSrv 回流的 test evidence / result package。

已完成：

- 将运行入口中的 `InstallRecordEvidenceSnapshot` 移到 App 详情头部，与 `Version snapshot`、`Runtime contract` 一起展示。
- 保证 DataSrv 回流的企业审批型 App 不管采用哪种动态 workspace layout，都能在用户打开 App 时直接看到：
  - workspace layout 摘要；
  - result contract 摘要；
  - test evidence run/protocol；
  - approval instance 摘要；
  - result package 输出数和 artifact 数；
  - structured result payload keys；
  - dependency verification 摘要。
- 扩展前端回流测试 `turns DataSrv installed MaClaw apps into addable app candidates with layout metadata`，断言 DataSrv 安装回流后的审批 App 详情头可见 `Result package`、`Output: 1 · Output artifacts: 1`、`decision, business_status`、`Skill dependencies: 1 · Blocking deps: 0`。

已验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "turns DataSrv installed MaClaw apps into addable app candidates with layout metadata"

npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "publishes approval app evidence from an actual workflow run|turns DataSrv installed MaClaw apps into addable app candidates with layout metadata|blocks DataSrv installed MaClaw apps at runtime when dependency verification fails|restores DataSrv installed enterprise normal app run evidence into app candidates"
```

结果均通过。

补充发现：

```powershell
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

当前完整文件回归仍失败，第一处失败是基础内置应用测试期待 `文档处理 (2)`，但当前源码中 `initialApps` 已明确改为空列表：`apps are discovered at runtime from DataSrv / skill manifests / Hub market packages`。这说明旧的内置应用面板测试还没有按“所有 MaClaw App 动态发现/动态生成”的新方向重写，属于后续测试收口任务，不是本轮 DataSrv 回流/安装证据链路失败。

对应全链路意义：

```text
DataSrv capabilities 回流企业审批型 App
  -> App Studio 添加到面板
  -> 用户打开 App 运行入口
  -> 详情头显示版本、workflow contract、安装证据、测试证据和结果包
  -> 动态 layout 即使没有 status/detail 区，也不会隐藏安装回流结果证据
```

下一步需要专门清理旧内置应用测试与产品方向不一致的问题：把“固定内置应用列表”预期改成“DataSrv / Skill manifest / Hub market 动态发现”，再跑全量 `AppsPage.test.tsx`，之后继续串 Hub 下载重装与 GUI 运行视图的最终样例级 E2E。
## 2026-06-29 推进记录：动态 MaClaw App 前端回归收口

本轮推进重点是把 `AppsPage` 前端测试从旧的“固定内置应用”模型迁移到新的“动态 MaClaw App”模型，避免通过恢复生产 `initialApps` 来兼容旧测试。

已完成：

```text
1. 恢复被清空的 AppsPage.test.tsx 测试文件。
2. 默认测试面板改为通过 localStorage seed 动态 App：
   - 企业审批型应用：报销申请
   - 企业普通应用：采购入库
   - 工具型应用：PDF 转 Word、文档脱敏、合同审查、表格分析等
   - 自动化/工具入口：数据同步、网页采集
3. 将旧固定应用 ID 迁移到动态 App ID：
   - pdf-word -> pdf-to-word
   - doc-redact -> document-redaction
4. 将旧业务域断言迁移到新业务模型：
   - procurement.* -> supply / purchase_order.*
   - 报销申请按 DataSrv 审批型应用处理
   - 采购入库按 DataSrv 企业普通应用处理，并走 ExecuteMaclawAppBusinessOperation
5. 发布、安装、管理、置顶、移除、运行态测试均按动态 App 语义修正：
   - 动态本地 App 使用“移除”，不再进入“隐藏内置应用”恢复区
   - pinned 上限测试显式构造 8 个常用应用
   - market/installed app 输出模式、视觉元数据按动态 manifest 保留
6. `AppsPage.test.tsx` 已重新跑通：194 tests passed。
```

当前验证命令：

```text
cd gui/frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

后续还需要继续补齐全链路验证：

```text
1. App Studio 制作 -> 保存 maclaw.app.json -> 本地测试 -> 发布 Hub。
2. Hub 能力市场下载/安装/重装 App 包。
3. 安装 MaClaw App 时进行 Skill 依赖检查、缺失依赖安装、版本/治理问题阻断。
4. DataSrv 注册企业审批型/企业普通应用，并恢复安装证据、结果契约、工作流契约。
5. GUI 运行审批型应用：发起申请、运行审批 Workflow Skill、记录审批实例、展示我的申请/我审批的/需关注/全部。
6. GUI 运行企业普通应用：走 DataSrv business operation bridge，展示 business_status/content/artifact/document 等结果包。
7. 对上述链路补后端 Go 定向测试和必要的前端集成测试。
```
### 推进记录：全局审批中心按全量实例恢复 Lane 视图（2026-06-29）

本轮继续围绕“企业审批型应用 = 数据录入 + 审批工作流 Skill 运行 + 审批实例数据管理 + 结果反馈”的闭环推进，重点修正 GUI 全局审批中心的实例加载语义。

此前全局审批中心在切换“我的申请 / 待我审批 / 已处理 / 需关注 / 全部”时，会按当前 lane 调用 `ListMaclawAppApprovalInstancesAll(lane, 200)`。这会导致当前页面只持有一个 lane 的实例集合，进而让其他 lane 的计数、筛选和当前节点状态依赖“刚刚加载过哪个 tab”，不利于审批实例管理的全局视图。

已完成：

```text
1. 全局 ApprovalManager 改为一次调用 ListMaclawAppApprovalInstancesAll('all', 200)，拉取 DataSrv + 本地合并后的完整审批实例集合。
2. 前端在完整实例集合上本地过滤 lane：
   - 我的申请：my_requests
   - 待我审批：pending_my_approval
   - 已处理：approved / rejected
   - 需关注：attention
   - 全部：all
3. lane 计数统一从完整实例集合计算，不再受当前 tab 的远端加载结果影响。
4. 保留当前行选择语义：用户已经选中的待审批实例，在同 lane 内刷新/点击时不会被无意义重置。
5. 扩展回归测试，验证审批中心只拉全量实例，切换 lane 不再重复请求 DataSrv，同时仍能显示当前节点、审批人、业务记录、业务对象、远端审批 ID、业务状态、结果状态和时间线。
```

已验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "keeps approval instance detail scoped to the selected lane and row"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

结果：`AppsPage.test.tsx` 194 个用例全部通过。

对应全链路意义：

```text
企业审批型 MaClaw App 运行/安装后产生审批实例
  -> GUI 全局审批中心一次恢复完整审批实例集合
  -> 我的申请 / 待我审批 / 已处理 / 需关注 / 全部基于同一份数据稳定切换
  -> 当前节点、处理人、业务记录、远端审批、状态流转和结果包详情不会因 lane 局部加载而丢失
  -> 后续黄金样例 E2E 可以把“审批实例数据管理”作为稳定验收点继续串到 Hub 安装、DataSrv 注册和 Workflow Skill 运行之后
```

下一步继续推进黄金样例 E2E：以“报销审批 App”为样例，把 App Studio 制作/保存/测试、Hub 上传下载、依赖 Skill 安装检查、DataSrv 注册、审批工作流运行、审批实例中心和结果反馈串成一条可执行验收链。
### 推进记录：选中安装时同步过滤 entry-level resolved dependencies（2026-06-29）

本轮继续推进“Hub 能力市场下载安装 -> Skill 依赖检查/安装 -> DataSrv 注册 -> GUI 回流”的黄金样例链路，重点补齐多 App 包选中安装时的依赖证据隔离。

发现的问题：

```text
App Studio / 本地提交会把 resolved_dependencies 写到两个位置：
1. package 顶层 resolved_dependencies，用于本地队列和直接安装；
2. 每个 maclaw.app.v1 entry 内部 resolved_dependencies，用于 Hub 存储和下载往返，因为 Hub 通常只持久化每个 entry 的 manifest JSON。

此前 maclawAppPackageForSelectedAppIDs 在“只安装包内某一个 App”时，只过滤了顶层 resolved_dependencies；
如果 entry 内部仍携带整包依赖，则只安装“报销审批 App”时，安装包体里仍可能混入“合同审批 App”等未选应用的依赖 install_ref 证据。
```

已完成：

```text
1. maclawAppPackageForSelectedAppIDs 现在会在复制选中 App entry 时同步过滤 entry-level resolved_dependencies。
2. 过滤规则与顶层一致：
   - 优先按 resolved dependency 的 app_ids/appIDs 匹配选中 App；
   - 没有 app_ids 时回退按当前 App 声明的 dependency id 匹配；
   - 无匹配时从选中 entry 中删除 resolved_dependencies，避免误导安装方。
3. 扩展 TestSelectedMaclawAppPackageFiltersResolvedDependencies：
   - 模拟 Hub round-trip 场景，把整包 resolved_dependencies 同时写入每个 App entry；
   - 验证选中安装 expense-app 后，顶层和 entry 内部都只保留 expense-workflow；
   - 验证 PlanMaclawAppInstall 仍使用 hub-expense-workflow install_ref，且 contract-workflow 不进入选中安装计划。
4. 当前工作区未跟踪的 gui/app_builtin_skills.go 原本使用 package gui 和 maclaw/corelib/skill，阻断 ./gui 编译；已改为 package main 并使用当前模块 import 路径 github.com/RapidAI/CodeClaw/corelib/skill，使 GUI 包测试恢复可运行。
```

已验证：

```powershell
cd D:\workprj\aicoder
go test ./gui -count=1 -vet=off -run "TestSelectedMaclawAppPackageFiltersResolvedDependencies|TestMaclawAppSerializableResolvedDepsIncludesAppIDs|TestInstallMaclawAppDependenciesPreservesInstallRefFromDuplicateDependency|TestInstallSelectedMaclawAppPackageFromHubPreservesApprovalEvidence|TestInstallMaclawAppPackageFromHubInstallsDepsAndRegistersDataSrv" -timeout 240s
```

结果：`ok github.com/RapidAI/CodeClaw/gui`。

对应全链路意义：

```text
Hub 能力市场下载一个包含多个 MaClaw App 的包
  -> 用户只选择安装其中一个企业审批型 App
  -> 顶层 package 和 entry-level manifest 都只保留该 App 的 resolved dependency install_ref
  -> InstallMaclawAppDependencies 按选中 App 的 app skill / workflow skill 精准安装
  -> RecordMaclawAppInstall 和 DataSrv app_installations 不会混入未选 App 的依赖证据
  -> 后续 GUI 回流、运行时依赖门禁和二次发布使用的是同一份选中 App 的依赖事实
```

下一步继续推进黄金样例 E2E：把“报销审批 App”的 App Studio 制作/测试证据、Hub 上传下载、依赖安装、DataSrv 注册、审批实例运行和结果反馈连成可执行验收用例。
### 推进记录：App Studio 发布包预览携带依赖验证证据（2026-06-29）

本轮继续推进“App Studio 制作/测试 -> 发布包预览/复制 -> 提交 Hub/能力市场 -> 下载安装依赖”的一致性。此前实际提交路径会在点击“提交审核”时重新调用 `PlanMaclawAppInstall`，并把 dependency verification 作为 governance override 写入提交包；但发布面板顶部用于复制/查看的整包预览仍直接调用 `appsToPackManifest(publishApps, submissions)`，没有带上已有安装证据或测试运行证据中的 dependency verification。

这会造成一个产品级不一致：用户在 App Studio 里复制出来的 `maclaw.app.pack.v1` 预览包，可能看不到已经验证通过的 Skill 依赖 install_ref；而真正点击提交时，提交包又会带上这些依赖验证事实。对于 MaClaw App 作为“超级 Skill”并依赖其他 Skill 分发的模型，这种预览/提交不一致会干扰人工审核、排障和手工包迁移。

已完成：

```text
1. PublishPane 现在为所有本地可发布 App 构建 packageGovernanceOverrides。
2. 对每个 App 优先使用 appInstallEvidenceDependencyVerificationPlan(app) 作为预览包 governance.dependencyVerification。
3. 顶部“复制提交包”和底部 package preview 现在调用 appsToPackManifest(publishApps, submissions, packageGovernanceOverrides)。
4. 实际提交路径仍保留更强的实时 PlanMaclawAppInstall 复核；预览只补齐已有证据，不替代提交门禁。
5. 扩展发布测试：验证 App Studio 预览包中已经包含 dependencyVerification、依赖 install_ref，以及当前测试 run id；随后提交包仍包含同一依赖验证事实。
```

已验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "requires dependency verification to cover declared app Skill dependencies before publishing"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

结果：`AppsPage.test.tsx` 194 个用例全部通过。

对应全链路意义：

```text
App Studio 测试并生成依赖验证证据
  -> 发布面板预览/复制的 maclaw.app.pack.v1 包携带 dependencyVerification 和 install_ref
  -> 点击提交审核时后端仍实时复核依赖计划并写入提交包
  -> Hub/能力市场审核、手工包迁移、下载安装依赖看到同一套依赖事实
  -> MaClaw App 作为“带 App 元数据的超级 Skill”时，依赖 Skill 的分发证据在设计、预览、提交和安装之间更一致
```

下一步继续推进黄金样例 E2E：把报销审批 App 的 Studio 制作、运行测试、预览包、提交 Hub、下载选装、依赖安装、DataSrv 注册、审批实例中心和结果反馈连成一条可执行验收链。
### 推进记录：DataSrv 审批发起节点结果包回流证据补强（2026-06-29）

本轮继续收紧“企业审批型应用 = 数据录入 + 审批工作流 Skill 运行 + 审批实例数据管理 + 结果反馈”的 DataSrv 证据链。代码核对后确认：DataSrv 的 `CreateRecordApproval`、`ReviewRecordApproval`、SQLite 持久化、业务记录同步、审计 metadata 和时间线回流已经具备 `result_payload`、`outputs`、`artifacts`、`business_status`、`result_status`、当前节点等字段的穿透能力。

本轮补强点：

```text
1. 扩展 TestHTTPServerRecordApprovalsCarryMaClawAppSemantics。
2. 在 MaClaw App 语义审批的 create approval 阶段，新增 attention/需关注场景的结果包：
   - result_payload.summary / business_status / requires_input
   - outputs 中的 attention_note
   - artifacts 中的 travel-policy-note.txt
3. 新增断言：审批发起节点创建 pending/attention 审批实例时，DataSrv 响应必须完整保留结果包，而不只是在 review 阶段保留。
```

已验证：

```powershell
cd D:\workprj\aicoder\datasrv
go test ./structureddata -count=1 -vet=off -run "TestHTTPServerRecordApprovalsCarryMaClawAppSemantics" -timeout 180s
```

结果通过。

对应全链路意义：

```text
GUI 发起企业审批型 MaClaw App
  -> DataSrv create_record_approval 创建审批实例
  -> 发起节点即可携带需关注/仅查看/等待补充等初始结果包
  -> DataSrv 审批实例、列表 lane、业务记录同步和后续 review 结果包使用同一套字段
  -> “我的申请 / 我审批的 / 需关注 / 已处理”可以稳定展示当前节点状态和结果反馈，不必等审批结束后才有结果证据
```
### 推进记录：Hub 安装后的运行时审批同步顶层结果包验证（2026-06-29）

本轮继续把“报销审批 App”黄金样例链路往后串：此前 `TestInstallSelectedMaclawApprovalAppFromHubPreservesApprovalEvidence` 已覆盖 Hub 下载选装、Skill 依赖安装、DataSrv app_installations 注册、安装审计、workflow/result/test evidence 保留，以及安装后 runtime 审批实例同步到 DataSrv。代码核对后发现 runtime 同步段已经验证了 request 内部的 `resultPayload` 和本地审批实例列表回流，但还可以更明确地验证发送给 DataSrv `create_record_approval` API 的顶层字段。

本轮补强点：

```text
1. 扩展 TestInstallSelectedMaclawApprovalAppFromHubPreservesApprovalEvidence。
2. 在安装后的 runtime approval sync 阶段，新增断言：
   - create_record_approval 顶层 result_payload 必须携带 business_record；
   - create_record_approval 顶层 outputs 必须保留运行结果输出；
   - create_record_approval 顶层 artifacts 必须保留运行附件；
   - create_record_approval 顶层 workflow_node_ids 必须保留并行/当前节点列表。
3. 保留原有断言：request 内部 resultPayload、安装 evidence、DataSrv 注册 metadata、本地审批实例列表回流仍全部可用。
```

已验证：

```powershell
cd D:\workprj\aicoder
go test ./gui -count=1 -vet=off -run "TestInstallSelectedMaclawApprovalAppFromHubPreservesApprovalEvidence" -timeout 240s
```

结果通过。

对应全链路意义：

```text
Hub 能力市场下载并选装企业审批型 MaClaw App
  -> 安装 app skill / workflow skill 依赖
  -> RecordMaclawAppInstall 写本地安装审计并注册 DataSrv app_installations
  -> 用户运行已安装 App 并产生审批实例
  -> SyncMaclawAppApprovalInstanceToDataSrv 调用 create_record_approval
  -> DataSrv API 顶层即可接收 result_payload / outputs / artifacts / workflow_node_ids
  -> 审批中心、业务记录、审计和结果反馈都能基于同一份运行结果包继续回流
```
### 推进记录：App Studio 可视化布局进入发布包双层契约（2026-06-29）

本轮继续补“所有 MaClaw App 界面动态生成，应用程序设计时自动生成，用户可视化调节位置，并在应用信息文件中保存界面布局信息；测试后上传 Hub 能力市场”的 App Studio 链路。此前已有用例验证用户在 Studio 里移动 workspace region 后，本地 manifest 和运行时布局会生效；本轮进一步把发布包侧钉住，避免只保存到本地运行态而没有进入 `maclaw.app.json / maclaw.app.pack.v1`。

本轮补强点：

```text
1. 扩展 AppsPage 前端用例：moves workspace regions from the visual layout preview into saved manifest regions。
2. 用户在 Studio 可视化布局预览中移动 region：
   - record_list -> right
   - output_panel -> bottom
3. 新增发布包预览断言：
   - app.ui.layouts.business_workspace.regions 保留用户移动后的 placement；
   - app.ui.layouts.business_workspace.studio.savedInManifest / updatedBy 保留 Studio 保存标记；
   - app.governance.workspaceLayout 同步保留 outputRegion、regionCount、regions 和 savedInManifest。
4. 这意味着上传 Hub/能力市场、DataSrv 注册和后续回流都能使用同一份动态 UI 布局，而不是依赖运行时临时状态。
```

已验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "moves workspace regions from the visual layout preview into saved manifest regions"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

结果：`AppsPage.test.tsx` 194 个用例全部通过。

对应全链路意义：

```text
App Studio 自动生成动态 UI
  -> 用户通过可视化预览调整 region 位置
  -> 保存到本地 App manifest.ui.layouts
  -> 发布包 app.ui 与 governance.workspaceLayout 同步携带完整布局
  -> Hub/能力市场审核、下载安装、DataSrv app_installations 注册、GUI 回流恢复都能看到同一份传统软件式动态界面布局
```
### 推进记录：App Studio 测试证据在预览包与提交包之间保持一致（2026-06-29）

本轮继续收紧“App Studio 测试 -> 发布包预览 -> 提交 Hub/能力市场”的证据链。此前发布门禁已经要求依赖 Skill 验证通过后才能提交，但仍需要明确验证：发布预览区展示的 `governance.testEvidence`、`governance.dependencyVerification`、结果输出和依赖安装引用，必须和用户点击提交后实际送往后端的包完全一致，不能出现预览可见、提交丢失或提交时重新组包偏移。

已完成：

- 扩展 AppsPage 前端用例 `requires dependency verification to cover declared app Skill dependencies before publishing`。
- 用结构化 JSON 解析发布包预览，断言预览包包含：
  - `governance.testEvidence.runId = run-dependency-verified`；
  - 结果 `resultPayload` 与 outputs 数量；
  - `testEvidence.dependencyVerification` 中的依赖 Skill 与 `install_ref`；
  - 顶层 `governance.dependencyVerification` 与 `dependencies.skills[].install_ref`。
- 点击提交后，继续断言提交给后端的包与预览包使用同一份测试证据、依赖验证证据和依赖安装引用。

已验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "requires dependency verification to cover declared app Skill dependencies before publishing"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

结果：`AppsPage.test.tsx` 194 个用例全部通过。

对应全链路意义：

```text
App Studio 运行测试并完成依赖 Skill 验证
  -> 发布包预览显示治理证据、依赖证据、结果证据和 install_ref
  -> 用户提交 Hub/能力市场时使用同一份包语义
  -> Hub 审核、下载、安装和 DataSrv 注册看到的证据不再和设计器预览分叉
```

### 推进记录：Hub MaClaw App 依赖验证证据进入审核 metadata 摘要（2026-06-29）

本轮继续补“App Studio 上传 Hub -> 能力市场审核/搜索 -> 下载选装 -> 安装依赖 Skill”的可见性契约。此前 Hub 会保存完整 `governance.dependencyVerification`，但市场和审核 metadata 只保留原始对象，没有像测试证据、审批实例证据那样提取可直接展示和筛选的摘要字段。这样审核界面、市场列表和安装诊断需要解析原始治理对象，容易导致依赖检查事实不可见。

已完成：

- Hub `enterpriseMaclawAppCapabilityMetadata` 在保存 `dependency_verification` 原始对象后，新增依赖验证摘要提取。
- 新增 metadata 摘要字段：
  - `dependency_verification_schema`、`dependency_verification_run_id`、`dependency_verification_verified_at`；
  - `dependency_verification_dependency_count`、`required_count`、`installed_count`、`missing_count`、`blocked_count`；
  - `dependency_verification_ok`、`dependency_verification_blocked`；
  - `dependency_verification_skills`、`dependency_verification_skill_count`、`dependency_verification_install_plan`。
- 扩展 `TestCapabilityMaclawAppSubmitCreatesPendingReviewCapability`，断言 Hub capability metadata 暴露依赖验证摘要，同时原始 `maclaw.app.v1` manifest 仍完整保存 `governance.dependencyVerification`。

已验证：

```powershell
cd D:\workprj\aicoder
go test ./hub/internal/httpapi -count=1 -vet=off -run "TestCapabilityMaclawAppSubmitCreatesPendingReviewCapability" -timeout 180s
go test ./hub/internal/httpapi -count=1 -vet=off -run "TestCapabilityMaclawAppSubmitCreatesPendingReviewCapability|TestCapabilityMaclawAppPackageDownloadReturnsApprovedPack|TestAdminCapabilityMaclawAppReviewApprovesCurrentVersion" -timeout 240s
```

结果均通过。

对应全链路意义：

```text
App Studio 提交 MaClaw App 到 Hub
  -> Hub 保存完整 App 包和治理证据
  -> Hub capability metadata 直接暴露依赖验证结果、缺失/阻断数量、已验证 Skill 和安装计划
  -> 审核、市场展示、下载选装和安装诊断能看到同一套依赖事实
  -> 安装 MaClaw App 前进行 Skill 依赖检查和安装的链路更可审计
```
### 推进记录：安装证据直接进入运行态审批中心（2026-06-29）

本轮继续补“安装后运行企业审批型 App -> 审批实例数据管理 -> 当前节点与结果反馈可见”的前端运行态链路。此前 Hub/DataSrv 安装记录已经能携带 `importedRunEvidence.approvalInstance` 和 `installEvidence.test_evidence.approval_instance`，但用户打开已安装 App 时，运行态审批工作台会先清空审批实例列表，再等待 `ListMaclawAppApprovalInstances` 返回；如果 DataSrv 暂时为空或离线，界面会退回演示用 fallback 实例，导致真实安装证据里的审批实例、当前节点、输出和附件没有直接进入运行态。

已完成：

- 新增运行态种子转换：从 `importedRunEvidence.approvalInstance` 和安装证据里的 `test_evidence.approval_instance` 生成 `ApprovalInstanceView`。
- 复用现有 `backendApprovalInstanceToView` 归一化逻辑，保证 current node、workflow skill、result payload、outputs、artifacts 和事件时间线显示口径一致。
- `mergeApprovalInstanceViews` 改为同时对 incoming/current 去重，避免同一审批实例从 imported evidence 与 install evidence 双来源进入列表时重复渲染。
- 打开 App 时先显示安装证据里的真实审批实例；DataSrv `all` lane 若返回非空则覆盖为后端权威列表，若返回空则保留当前安装证据种子。
- 扩展 DataSrv 安装 App 金线测试，断言运行态审批工作台能直接显示：
  - 远端审批实例 ID `approval-remote-imported`；
  - 当前节点 `expense_report.result_feedback / finance.archive`；
  - 结果输出 `Approval rows` / `expense approved`；
  - 文件产物 `expense-approval-evidence.zip`；
  - 结构化结果字段 `business_status`；
  - 审批实例数量去重后为 `Instance data: 1`。

已验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "turns DataSrv installed MaClaw apps into addable app candidates with layout metadata"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

结果：`AppsPage.test.tsx` 194 个用例全部通过。

对应全链路意义：

```text
Hub/DataSrv 安装企业审批型 MaClaw App
  -> 安装记录携带 approval_instance、outputs、artifacts、result_payload
  -> 用户打开运行态 App
  -> 审批工作台先使用安装证据恢复真实审批实例
  -> DataSrv 返回实例时再覆盖为权威列表
  -> “我的申请 / 已处理 / 全部”可立即看到当前节点、结果包和附件，不再退回演示实例
```
### 推进记录：审批实例视图 lane 语义核验（2026-06-29）

本轮继续从“企业审批型应用 = 数据录入 + 审批工作流 Skill 运行 + 审批实例数据管理 + 结果反馈”的完整链路检查运行态审批中心。重点核验 MaClaw App 打开后，审批工作台的固定视图不是只做前端标签切换，而是能按当前用户和 DataSrv 审批实例事实进行语义归类。

已核验并保持通过：

```text
1. GUI 后端 ListMaclawAppApprovalInstances / ListMaclawAppApprovalInstancesAll 会把 lane 传给 DataSrv；all 视图不下发 lane 过滤，避免错漏全量诊断数据。
2. DataSrv 返回 RecordApproval 后，GUI 根据显式 approval_lane、attention 状态、submitted_by / created_by / applicant、assigned_to / current_assignee、reviewed_by 和当前 cfg.UserID 推断：我的申请、待我审批、已处理、需关注。
3. pending_my_approval 查询不会盲目信任请求 lane；远端返回的其它人待审实例不会被错误塞进“待我审批”。
4. 自己发起且当前也被指派的审批实例仍能出现在“待我审批”，支持会签/并行节点场景。
5. 前端审批工作台点击“待我审批”等 lane 后，会调用 ListMaclawAppApprovalInstances(appID, lane, 50)，并用返回的当前节点、申请人、处理人、结果包和附件刷新传统软件式审批工作台。
```

已验证命令：

```powershell
go test ./gui -count=1 -vet=off -run "TestListMaclawAppApprovalInstancesAll|TestListMaclawAppApprovalInstancesAllLoadsDataSrvLane|TestListMaclawAppApprovalInstancesAllInfersDataSrvLanesForCurrentUser|TestListMaclawAppApprovalInstancesDoesNotTrustRequestedLaneForDataSrvItems|TestListMaclawAppApprovalInstancesKeepsSelfSubmittedPendingApprovalVisible|TestListMaclawAppApprovalInstancesHonorsExplicitDataSrvLaneCaseInsensitively" -timeout 180s
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "shows the approval instance workspace for approval apps"
```

结果均通过。

对应全链路意义：

```text
安装/运行企业审批型 MaClaw App
  -> 从 DataSrv 或安装证据恢复审批实例
  -> GUI 按当前用户恢复“我的申请 / 待我审批 / 已处理 / 需关注 / 全部”
  -> 用户切换审批视图时触发后端 lane 刷新
  -> 审批工作台展示当前节点、处理人、状态流转、结果包、附件和业务状态
```

### 推进记录：App Studio 提交包同步 Hub 时保留审批证据（2026-06-29）

本轮继续把黄金 E2E 链路往前串：此前已经验证 Hub 下载选装、依赖安装、DataSrv 注册、安装审计和运行态审批证据回流；这次补强本地 App Studio 提交包同步到 Hub 的前半段，确保上传到 Hub 的 package 不是只带 App 基本信息，而是保留同一套动态 UI、测试证据和依赖验证证据。

已完成：

```text
1. 扩展 TestSyncMaclawAppPackageSubmissionToHubUpdatesLocalQueue。
2. 本地提交包中加入 Studio 侧生成/保存的 governance.workspaceLayout。
3. 本地提交包中加入 governance.dependencyVerification，包含已验证依赖 Skill 与 install_ref。
4. 本地提交包中加入 governance.testEvidence.approvalInstance，包含 workflow skill、当前节点、审批结果、result payload、outputs 和 artifacts。
5. Hub submit 接收端测试断言 captured package 原样保留上述证据。
```

已验证命令：

```powershell
go test ./gui -count=1 -vet=off -run "TestSyncMaclawAppPackageSubmissionToHubUpdatesLocalQueue" -timeout 180s
go test ./gui -count=1 -vet=off -run "TestSyncMaclawAppPackageSubmissionToHubUpdatesLocalQueue|TestInstallSelectedMaclawApprovalAppFromHubPreservesApprovalEvidence" -timeout 240s
```

结果均通过。

对应全链路意义：

```text
App Studio 设计/测试企业审批型 MaClaw App
  -> 保存动态 UI layout、依赖验证、审批实例测试证据
  -> SubmitMaclawAppPackage 本地排队
  -> SyncMaclawAppPackageSubmissionToHub 上传 Hub
  -> Hub 接收到的 maclaw.app.pack.v1 保留 Studio 证据
  -> 后续 Hub 审核/下载选装/依赖安装/DataSrv 注册/运行态审批中心使用同一套证据继续流转
```

### 推进记录：工具型 App 安装测试证据进入运行历史（2026-06-29）

本轮补齐工具型 App 的安装后运行态证据链。工具型 App 不连接 DataSrv，也没有审批实例；它的核心结果是文本/内容、文件产物、结构化输出和运行历史。此前安装记录已经保存 `test_evidence`，但打开已安装工具型 App 时运行历史只读取本地运行记录，安装测试证据不会直接成为可见运行结果。

已完成：

```text
1. 新增 appSeedRunHistory(app)，打开 App 时把 localStorage 本地运行历史与 importedRunEvidence / installEvidence.test_evidence 合并展示。
2. 保持本地真实运行历史优先；安装/导入证据只作为展示种子，不写回本地历史，避免污染用户运行记录。
3. 扩展 installEvidenceImportedRunEvidence，支持从 artifactURI / artifactPath 以及 artifacts[0].uri/path 提升出运行历史可打开的产物引用。
4. 扩展工具型市场安装测试：安装“合同归档”后，打开 App 即可看到 run-contract-archive、artifact 输出、结果摘要和 artifact://contract/archive.pdf。
```

已验证命令：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "keeps dependency verification visible after single market app install"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

结果：`AppsPage.test.tsx` 194 个用例全部通过。

对应全链路意义：

```text
工具型 MaClaw App 从 Hub/能力市场安装
  -> 安装记录携带 test_evidence、outputs、artifacts、result_payload
  -> 用户打开工具型 App
  -> 运行历史直接显示安装测试证据
  -> 用户可看到文本摘要、输出类型、run id 和文件产物 URI
  -> 工具型 App 不需要 DataSrv/审批实例，也能完成“安装后可见结果反馈”闭环
```

### 推进记录：DataSrv 补齐工具/普通 App 结果证据摘要（2026-06-29）

本轮继续从全链路数据底座收口：工具型 App 和企业普通应用没有审批实例作为主线，它们的运行闭环主要依赖 `test_evidence.result_payload`、`outputs`、`artifacts`、文件 URI 和结果类型。此前 DataSrv 会保存这些大对象，但列表查询、审计日志和安装诊断缺少稳定的小摘要字段，导致后续 App Studio、Hub 安装和运行历史只能读取深层结构。

已完成：

```text
1. DataSrv 安装 metadata 规范化新增轻量摘要：
   - test_evidence_result_type / test_evidence_output_type / test_evidence_result_content
   - test_evidence_output_kinds / test_evidence_output_types
   - test_evidence_artifact_uri / test_evidence_artifact_path
   - test_evidence_artifact_names / test_evidence_artifact_uris / test_evidence_artifact_types
2. 从 result_payload、outputs、artifacts 自动提升摘要，保留完整 result_payload / outputs / artifacts 作为运行态证据。
3. 审计日志只记录轻量摘要，不写入 bulky result_payload，保持可审计但不膨胀。
4. app installation 的 result_type 查询扩展到 output/artifact 摘要字段；例如 `result_type=document` 可命中文件产物型工具 App。
5. 新增 `TestUpsertAppInstallationSummarizesToolResultEvidence` 覆盖工具型 App 安装证据、artifact URI、输出类型、审计摘要和 result_type 查询。
```

已验证命令：

```powershell
cd D:\workprj\aicoder\datasrv
go test ./structureddata -count=1 -vet=off -run "TestUpsertAppInstallationNormalizesGovernanceResultContract|TestUpsertAppInstallationSummarizesToolResultEvidence|TestUpsertAppInstallationSynthesizesTestEvidenceFromSummaryMetadata|TestListAppInstallationsFiltersByApprovalResultMetadata" -timeout 180s
go test ./structureddata -count=1 -vet=off -run "TestUpsertAppInstallation|TestListAppInstallations|TestHTTPServerAppInstallation|TestHTTPServerRecordApprovalsCarryMaClawAppSemantics" -timeout 240s
```

结果均通过。

对应全链路意义：

```text
App Studio 测试工具型/企业普通 MaClaw App
  -> 产出 result_payload、outputs、artifacts、artifact URI
  -> Hub/市场安装写入 DataSrv app installation
  -> DataSrv 自动提升轻量结果摘要
  -> 安装列表、审计日志、运行历史和诊断页可以读取同一套结果事实
  -> 工具/普通 App 不依赖审批实例，也能完成“安装后可见结果反馈 + 可审计 + 可按结果类型定位”的闭环
```
### 推进记录：GUI 安装注册 DataSrv 时同步结果证据摘要（2026-06-29）

上一轮 DataSrv 已经能规范化工具型/企业普通 App 的 `result_payload`、`outputs`、`artifacts`、artifact URI 和结果类型摘要。本轮把这套数据契约接回 MaClaw App 安装流程：GUI 在 `RecordMaclawAppInstall -> registerMaclawAppInstallationsToDataSrv` 生成 DataSrv app installation payload 时，会同步提升同一套轻量结果摘要，避免只保存深层大对象。

已完成：

```text
1. applyMaclawAppDataSrvTestEvidenceMetadata 新增结果摘要提升：
   - test_evidence_result_type / test_evidence_output_type / test_evidence_result_content
   - test_evidence_output_kinds / test_evidence_output_types
   - test_evidence_artifact_uri / test_evidence_artifact_path
   - test_evidence_artifact_names / test_evidence_artifact_uris / test_evidence_artifact_types
2. 从 GUI 安装注册 payload 的 result_payload、outputs、artifacts 和 approval_instance.outputs/artifacts 自动抽取摘要。
3. 扩展 DataSrv payload 测试，确保企业普通 App 安装注册时，依赖验证和结果证据摘要同时进入 metadata。
4. 复跑 Hub 选装安装与 App Studio 同步测试，确认依赖安装计划、审批证据、DataSrv 注册证据仍然兼容。
```

已验证命令：

```powershell
cd D:\workprj\aicoder
go test ./gui -count=1 -vet=off -run "TestMaclawAppInstallEvidenceGeneratesDependencyVerification|TestMaclawAppDataSrvInstallationPayloadsScopeDependenciesPerApp" -timeout 180s
go test ./gui -count=1 -vet=off -run "TestInstallSelectedMaclawApprovalAppFromHubPreservesApprovalEvidence|TestSyncMaclawAppPackageSubmissionToHubUpdatesLocalQueue" -timeout 240s
```

结果均通过。

对应全链路意义：

```text
安装 MaClaw App
  -> GUI 先安装/校验 Skill 依赖并生成 install plan
  -> RecordMaclawAppInstall 生成本地安装记录和 install_evidence
  -> registerMaclawAppInstallationsToDataSrv 写入 DataSrv app installation
  -> DataSrv metadata 同时拥有依赖验证、动态 UI layout、审批/普通结果证据摘要
  -> 后续应用面板、审计日志、运行历史和诊断页可以读取一致的安装事实
```
### 推进记录：运行态恢复安装证据中的 App Studio 布局（2026-06-29）
本轮补齐 App Studio 动态界面在“设计/上传/安装/运行”链路里的一个关键兜底：当本地 manifest 已经带有 `ui.layouts[entry]` 时继续以 manifest 为准；当从 Hub/DataSrv 安装得到的 App 只有 `installEvidence.workspace_layout` 时，运行态工作区会读取安装证据中的布局并恢复到实际界面。

已完成：

```text
1. runtimeWorkspaceLayoutForApp 增加 installEvidence.workspace_layout 兜底读取。
2. 支持两种安装证据形态：直接布局对象，以及带 entry/layouts 的完整 UI 布局对象。
3. 保持本地 manifest 优先，避免用户在 App Studio 中保存过的布局被安装证据覆盖。
4. 新增前端测试：manifest 缺少 ui.layouts，但 installEvidence.workspace_layout 带 dashboard/spacious/right/modal/regions 时，运行态 .apps-runtime-layout 会恢复对应 template、density、primaryRegion、outputRegion 和 region count。
5. 复跑 DataSrv installed app layout metadata 测试，确认原有 DataSrv -> manifest.ui.layouts 导入路径仍然正常。
```

已验证命令：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "restores runtime workspace layout from install evidence|turns DataSrv installed MaClaw apps into addable app candidates with layout metadata"
```

结果：2 个目标用例通过。

对应全链路意义：

```text
App Studio 生成/调整动态界面布局
  -> App package / Hub / DataSrv installation 保留 workspace_layout
  -> 安装后的 App 即使 manifest.ui.layouts 缺失，也能从 installEvidence 恢复布局
  -> 运行态呈现继续保持用户设计的传统软件式工作区，而不是退回默认表单壳
```

### 推进记录：后端安装审计从 governance.workspaceLayout 兜底保留布局（2026-06-29）
上一轮前端运行态已经能从 `installEvidence.workspace_layout` 恢复 App Studio 布局。本轮把后端安装审计的布局来源补齐：安装包如果带有 `app.ui/binding.ui`，继续从 UI manifest 抽取；如果只有 `governance.workspaceLayout`，也会把完整布局写入 install evidence / local install audit / DataSrv metadata。

已完成：

```text
1. maclawAppWorkspaceLayoutMetadataForEntry 增加 governance.workspaceLayout 兜底来源。
2. 后端 workspace_layout 同时保留 primaryRegion/outputRegion 和 primary_region/output_region，兼容前端运行态与 DataSrv 摘要。
3. 后端 workspace_layout 同时保留 regionCount 和 region_count；如果未显式提供 regionCount，则按 regions 长度补齐。
4. 保留完整 regions、navigation、list.columns 等布局结构，不只写摘要字段。
5. 新增 Go 单元测试覆盖“只有 governance.workspaceLayout、没有 app.ui”的安装证据布局抽取。
```

已验证命令：

```powershell
cd D:\workprj\aicoder
go test ./gui -count=1 -vet=off -run "TestMaclawAppWorkspaceLayoutMetadataFallsBackToGovernanceLayout|TestRecordMaclawAppInstallPersistsNewestInstallAudit|TestInstallMaclawAppPackageFromHubRegistersDataSrvInstallation" -timeout 240s
```

结果：目标 Go 测试通过。

对应全链路意义：

```text
App Studio / Hub package 携带 governance.workspaceLayout
  -> GUI 后端安装 MaClaw App 时生成 workspace_layout install evidence
  -> workspace_layout 同时适配 DataSrv 摘要字段和前端运行态 camelCase 字段
  -> 已安装 App 运行时可恢复用户设计的动态界面布局
  -> “设计、上传、安装、运行”布局证据链不再依赖单一 app.ui 存储位置
```
### 推进记录：安装布局证据进入 App Studio 编辑与发布包（2026-06-29）
本轮继续补齐“Hub/DataSrv 安装后的 MaClaw App 还能回到 App Studio 二次设计并再次发布”的动态 UI 链路。上一轮已经让运行态可以从 `installEvidence.workspace_layout` 恢复布局，后端安装审计也能从 `governance.workspaceLayout` 兜底生成安装布局证据；本轮把这份证据推进到 App Studio 编辑态和发布态。

已落实：

1. 新增前端金线测试覆盖：manifest 只有 `ui.entry`、没有 `ui.layouts`，但安装证据带完整 `workspace_layout` 时，App Studio 管理面板编辑器会恢复 `template/density/primaryRegion/outputRegion/regions`。
2. 用户保存后，布局被固化到 `manifest.app.binding.ui.layouts.business_workspace`，并带 `studio.savedInManifest=true`、`updatedBy=app_studio`。
3. 再进入 Review / publish 时，发布包同时携带：
   - `app.ui.layouts.business_workspace`
   - `app.governance.workspaceLayout`
   - 完整 `regions`、`regionCount`、主工作区与输出区位置。
4. 定向验证通过：安装证据布局恢复、已有后端布局可编辑、可视化 region 移动入发布包、运行态安装证据恢复四个相关用例全部通过。
5. 完整前端回归通过：`AppsPage.test.tsx` 196 个用例全部通过。

这一步把动态 UI 的布局契约补成可往返链路：

```text
App Studio 设计/发布
  -> Hub/DataSrv 安装证据 workspace_layout
  -> GUI 运行态恢复
  -> App Studio 二次编辑
  -> manifest.ui.layouts 固化
  -> 发布包 app.ui + governance.workspaceLayout 再分发
```

后续仍需继续做最终样例级 E2E：以企业审批型 App 为主样例，把 App Studio 制作/测试、上传 Hub、下载重装、依赖 Skill 安装、DataSrv 注册、审批 workflow 运行、审批实例视图和结果反馈串成一条可执行验收链。
### 推进记录：Hub 安装审批型 App 后直接进入运行态审批工作台（2026-06-29）
本轮继续把企业审批型黄金链路从“安装证据落到本地 AppEntry”推进到“用户安装后打开应用即可看到审批实例和结果反馈”。此前市场安装测试已经覆盖 Hub 选装、依赖安装计划、install evidence、workspace layout、approvalInstance 和 version snapshot 的本地保存；但测试停在 App Studio 管理面板，没有验证安装后的传统软件式运行界面是否真的恢复这些证据。

已落实：

1. 扩展 `installs approved Hub MaClaw Apps from market search results` 前端金线测试。
2. 从 Enterprise Hub 搜索并安装企业审批型 `Contract Approval` 后，关闭 App Studio，直接打开已安装 App。
3. 验证运行态 `.apps-runtime-layout` 恢复安装证据里的审批工作台布局：`classic_split / left / right / regionCount=3`。
4. 验证 `.apps-approval-workspace` 直接显示安装证据中的审批实例：`approval-hub-contract-1`、当前节点 `contract.result_feedback`、业务状态 `executed`。
5. 验证审批结果包和文件产物可见：`Approval decision`、`contract-approval.pdf`。

已验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "installs approved Hub MaClaw Apps from market search results"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "publishes approval app evidence from an actual workflow run|installs approved Hub MaClaw Apps from market search results|turns DataSrv installed MaClaw apps into addable app candidates with layout metadata|restores installed workspace layout into App Studio editing and republishes it"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

结果：完整 `AppsPage.test.tsx` 196 个用例全部通过。

对应全链路意义：

```text
Enterprise Hub 市场搜索企业审批型 MaClaw App
  -> 选中安装并执行 Skill 依赖安装计划
  -> install_record 保存 workspace_layout、dependency_verification、approval_instance、result package
  -> GUI 应用面板生成已安装 AppEntry
  -> 用户关闭 App Studio 后打开 App
  -> 运行态审批工作台恢复布局、审批实例、当前节点、业务状态、结果输出和文件产物
```

这一步补上了“安装成功”到“用户真的能运行/查看审批结果”的前端可见验收点。后续继续把前半段 App Studio 制作/测试/上传 Hub 与这一段安装运行态连接成同一个企业审批型样例。
### 推进记录：Hub 选装审批型 App 的 DataSrv 注册补齐布局区域摘要（2026-06-30）
本轮继续从“App Studio/Hub 安装/ DataSrv 回流/GUI 运行态”之间的稳定契约收口。前端已经能在 Hub 安装后直接打开企业审批型 App 并恢复审批工作台布局、审批实例和结果包；后端选装安装测试也证明完整 `workspace_layout` 会进入 DataSrv metadata。但本轮定向加严测试时发现：GUI 注册 DataSrv app_installations 时，顶层 metadata 已写入 `workspace_layout_primary_region` / `workspace_layout_output_region`，却没有同步写入稳定摘要 `workspace_layout_region_count` / `workspace_layout_region_ids`。完整 layout 内部有 `region_count/regions`，但外部诊断、OpenAPI 调用方和 GUI 能力发现更适合读取扁平摘要。

已落实：

1. GUI 后端 `maclawAppDataSrvInstallationPayloads` 在构造 DataSrv 注册 metadata 时，从完整 `workspace_layout` 主动提升：
   - `workspace_layout_region_count`
   - `workspace_layout_region_ids`
2. 如果布局本身有 `regionCount/region_count`，优先使用该值；如果没有，则按 `regions` 长度补齐。
3. 区域 ID 从 `workspace_layout.regions[].id` 提取，保持 App Studio 用户调整后的 canonical region 顺序。
4. 扩展企业审批型 Hub 选装安装金线测试，验证 DataSrv 注册 metadata 同时保留：
   - 完整 `workspace_layout`
   - `primary/output` 摘要
   - `region_count`
   - `region_ids`
   - `request_form / approval_inbox / result_panel` 的实际 placement。

已验证：

```powershell
cd D:\workprj\aicoder
go test ./gui -count=1 -vet=off -run "TestInstallSelectedMaclawApprovalAppFromHubPreservesApprovalEvidence" -timeout 240s
go test ./gui -count=1 -vet=off -run "TestSyncMaclawAppPackageSubmissionToHubUpdatesLocalQueue|TestInstallSelectedMaclawApprovalAppFromHubPreservesApprovalEvidence|TestInstallSelectedMaclawAppPackageFromHubInstallsDepsAndRegistersDataSrv|TestMaclawAppDataSrvInstallationPayloadsScopeDependenciesPerApp|TestMaclawAppWorkspaceLayoutMetadataFallsBackToGovernanceLayout" -timeout 300s
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

结果：目标 Go 测试、Go 相关组合、前端 `AppsPage.test.tsx` 196 个用例全部通过。

对应全链路意义：

```text
App Studio 动态 UI layout
  -> 发布包 app.ui / governance.workspaceLayout
  -> Hub 选装安装企业审批型 App
  -> GUI 安装注册 DataSrv app_installations
  -> DataSrv metadata 同时拥有完整 workspace_layout 和扁平区域摘要
  -> GUI 能力发现、诊断页、外部连接器和运行态恢复都能稳定定位审批工作台主区、输出区和 region 列表
```

这一步减少了“完整对象存在但摘要缺失”的回流风险，使 Hub 选装审批型 App 的 DataSrv 注册契约更接近可产品化。
### 推进记录：DataSrv 回流到 GUI 时保留布局区域摘要和运行态布局（2026-06-30）
本轮继续把上一轮的 DataSrv 注册摘要补强往回流端闭合。GUI 后端现在会把 `workspace_layout_region_count` / `workspace_layout_region_ids` 写入 DataSrv metadata；本轮验证并补齐 GUI 前端从 DataSrv capabilities 重新发现已安装 MaClaw App 时，安装证据和运行态同样保留这些布局区域事实。

已落实：

1. `dataSrvInstalledInstallEvidence` 在读取 DataSrv metadata 时，会把扁平摘要 merge 回 `installEvidence.workspace_layout`：
   - `region_count` / `regionCount`
   - `region_ids` / `regionIds`
2. 如果 DataSrv 返回完整 `metadata.workspace_layout`，仍以完整对象为主，但不会丢掉扁平区域摘要。
3. 如果只有摘要重建 layout，也会把 `workspace_layout_region_count` / `workspace_layout_region_ids` 写入安装证据。
4. 扩展 DataSrv installed app 前端金线测试，验证：
   - `installEvidence.workspace_layout` 保留 region count 与 region ids；
   - `installEvidence.workspace_layout.regions` 保留 request form / inbox / result panel 的实际 placement；
   - 用户打开回流 App 后，运行态 `.apps-runtime-layout` 恢复 dashboard/spacious 和 regionCount=3；
   - 输入区和输出区分别落到 `left` / `bottom`。

已验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "turns DataSrv installed MaClaw apps into addable app candidates with layout metadata"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "turns DataSrv installed MaClaw apps into addable app candidates with layout metadata|restores runtime workspace layout from install evidence|restores installed workspace layout into App Studio editing and republishes it|installs approved Hub MaClaw Apps from market search results"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
cd D:\workprj\aicoder
go test ./gui -count=1 -vet=off -run "TestInstallSelectedMaclawApprovalAppFromHubPreservesApprovalEvidence|TestMaclawAppDataSrvInstallationPayloadsScopeDependenciesPerApp|TestMaclawAppWorkspaceLayoutMetadataFallsBackToGovernanceLayout" -timeout 300s
```

结果：前端定向、前端完整 AppsPage 196 个用例、后端 GUI 相关组合均通过。

对应全链路意义：

```text
GUI 安装/注册 MaClaw App 到 DataSrv
  -> DataSrv app_installations metadata 保存完整 workspace_layout + 扁平 region 摘要
  -> GUI 从 /capabilities 回流已安装 App
  -> installEvidence.workspace_layout 保留完整区域布局和摘要
  -> App Studio 二次编辑/发布与运行态 App 都能继续使用同一份布局事实
```

这一步把“写入 DataSrv”与“从 DataSrv 回流到 GUI 运行态”的布局证据链闭合得更严，减少安装后再次发现 App 时布局退化为模板摘要的风险。
### 推进记录：普通企业应用 DataSrv 结果证据进入运行历史（2026-06-30）

已完成：

- 前端运行历史从 `AppRunHistoryEntry.outputs`、`artifacts`、`artifactName` 中抽取结构化证据摘要。
- DataSrv 回流的企业普通应用不再只显示 `runID`、状态和 artifact URI，也能在运行态直接看到输出标题、输出内容摘要与附件名。
- 保持现有企业工作台风格：证据摘要以紧凑单行显示在运行历史条目内，不引入新的重装饰面板。
- 补强 `restores DataSrv installed enterprise normal app run evidence into app candidates`，验证 `Customer renewal`、`customer-renewal.pdf`、`artifact://customer/renewal.pdf` 都能在运行历史中出现。

验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "restores DataSrv installed enterprise normal app run evidence into app candidates"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "restores DataSrv installed enterprise normal app run evidence into app candidates|turns DataSrv installed MaClaw apps into addable app candidates with layout metadata|restores runtime workspace layout from install evidence|restores installed workspace layout into App Studio editing and republishes it|installs approved Hub MaClaw Apps from market search results|keeps dependency verification visible after single market app install"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
cd D:\workprj\aicoder
go test ./gui -count=1 -vet=off -run "TestInstallSelectedMaclawApprovalAppFromHubPreservesApprovalEvidence|TestMaclawAppDataSrvInstallationPayloadsScopeDependenciesPerApp|TestMaclawAppWorkspaceLayoutMetadataFallsBackToGovernanceLayout" -timeout 300s
cd D:\workprj\aicoder\datasrv
go test ./structureddata -count=1 -vet=off -timeout 300s
```

结果：前端 `AppsPage.test.tsx` 全量 196 个用例通过；GUI 后端定向测试通过；DataSrv `structureddata` 包测试通过。

这一步把普通企业应用的一次性业务操作结果从“安装审计里有证据”推进到“用户运行界面可见证据”。下一步继续推进黄金样例 E2E：将 App Studio 制作/测试/上传 Hub、下载重装、依赖安装、DataSrv 注册、企业审批型实例运行和普通企业应用结果反馈放入同一条验收链。
### 推进记录：Studio 构建包经 Hub 重装进入运行态黄金验收（2026-06-30）

已完成：

- 新增前端黄金链路验收：`round-trips a Studio-built approval app through Hub install into the runtime workspace`。
- 验收从同一个 Studio-built 企业审批型 App 出发，覆盖：
  - App Studio / Review publish 提交包生成；
  - 发布包保留 `governance.workspaceLayout`、`governance.testEvidence.approvalInstance`、`governance.dependencyVerification`；
  - 同一份提交包作为 Hub capability 下载包被选中安装；
  - 安装记录保留动态 UI layout、workflow mapping/contract、依赖验证、测试运行证据、审批实例、结果包和附件；
  - 打开安装后的运行态 App，恢复 dashboard/spacious/bottom 动态布局；
  - 审批工作台直接显示 `wf-studio-golden-1`、`expense.result_feedback`、`finance_approved`、`Golden approval decision`、`golden-expense-approval.pdf`。
- 这条用例把此前分散的“Studio 发布包生成”“Hub 选装安装”“安装证据回灌”“运行态审批工作台”串成同一份 App 包的端到端防回退验收。

验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "round-trips a Studio-built approval app through Hub install into the runtime workspace"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "round-trips a Studio-built approval app through Hub install into the runtime workspace|republishes cold-start approval apps from Hub install evidence|installs approved Hub MaClaw Apps from market search results|restores installed workspace layout into App Studio editing and republishes it|turns DataSrv installed MaClaw apps into addable app candidates with layout metadata"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

结果：`AppsPage.test.tsx` 全量 197 个用例通过。

对应全链路意义：

```text
App Studio 制作企业审批型 MaClaw App
  -> 保存动态 UI、依赖 Skill、workflow 合同、测试协议、审批实例运行证据
  -> Review / publish 生成 MaClaw App 超级 Skill 发布包
  -> Hub 市场按 capability 分发同一份 package
  -> 用户选择安装该 App
  -> 安装端检查/保留依赖验证与 install_ref
  -> 安装证据进入本地 App、DataSrv 注册载荷和运行态种子
  -> 用户打开 App 后看到传统软件式审批工作台、当前节点、结果反馈和文件产物
```

这一步不宣称整套 MaClaw App 已完成，但把“制作 -> 发布包 -> Hub 下载安装到 -> 运行态审批工作台”的核心链路固化为可执行验收。下一步继续把同一黄金样例向后端 DataSrv HTTP 注册、真实 workflow skill 运行/决策同步和 Hub 审核队列状态推进。
### 推进记录：DataSrv HTTP 规范化 Studio Governance 安装证据（2026-06-30）

已完成：

- 新增 DataSrv HTTP 层验收：`TestHTTPServerAppInstallationPUTNormalizesStudioGovernancePayload`。
- 该用例模拟 Hub/Studio 常见安装注册 payload：`metadata.governance.workspaceLayout`、`metadata.governance.testEvidence`、`metadata.governance.dependencyVerification` 使用 camelCase 字段，而不是 GUI 后端已经整理好的 snake 字段。
- DataSrv `PUT /api/v1/data/app-installations/{appId}` 会将这类 Studio governance payload 规范化为可查询、可回流的 canonical metadata：
  - `workspace_layout_*`：entry、template、density、primary/output region、region count、region ids；
  - `test_evidence_*`：run id、definition fingerprint、workflow skill、business/result status、artifact name/uri、output type；
  - `dependency_verification`：dependency count、missing/blocking 状态、依赖明细和 `install_ref`；
  - `test_evidence.approval_instance`：保留审批实例、当前节点、workflow skill、业务状态；
  - `governance.testEvidence`：保留原始 Studio 提交证据，供审计和二次发布参考。
- 同时验证 `/api/v1/data/capabilities` 能读取同一份规范化证据，保证 GUI 冷启动从 DataSrv 回流时不依赖前端 localStorage。

验证：

```powershell
cd D:\workprj\aicoder\datasrv
go test ./structureddata -count=1 -vet=off -run "TestHTTPServerAppInstallationPUTNormalizesStudioGovernancePayload" -timeout 180s
go test ./structureddata -count=1 -vet=off -run "TestHTTPServerAppInstallationPUTNormalizesStudioGovernancePayload|TestHTTPServerAppInstallationPUTRoundTripsGUIEquivalentPayloadThroughCapabilities|TestUpsertAppInstallationPreservesFullEnterpriseApprovalTestEvidence|TestListAppInstallationsFiltersNestedInstallEvidence|TestAppInstallationOpenAPISchemaDocumentsFullTestEvidence" -timeout 240s
cd D:\workprj\aicoder
go test ./gui -count=1 -vet=off -run "TestRecordMaclawAppInstallRegistersApprovalAppWithDataSrv|TestInstallSelectedMaclawApprovalAppFromHubPreservesApprovalEvidence|TestMaclawAppDataSrvInstallationPayloadsScopeDependenciesPerApp|TestMaclawAppWorkspaceLayoutMetadataFallsBackToGovernanceLayout" -timeout 300s
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "round-trips a Studio-built approval app through Hub install into the runtime workspace|installs approved Hub MaClaw Apps from market search results|turns DataSrv installed MaClaw apps into addable app candidates with layout metadata"
```

结果：DataSrv HTTP/安装证据测试通过；GUI 后端安装注册测试通过；前端黄金链路和回流视图测试通过。

对应全链路意义：

```text
App Studio 生成 MaClaw App 超级 Skill 发布包
  -> Hub / GUI 安装注册可能携带 governance camelCase 证据
  -> DataSrv HTTP PUT 接收并规范化为 canonical metadata
  -> capabilities 冷启动回流可直接恢复动态 UI、依赖验证、审批实例、结果包和附件
  -> 原始 governance 证据仍被保留，支持审计与二次发布
```

这一步把前端同源黄金验收继续下压到 DataSrv HTTP 边界。下一步继续推进真实 workflow skill 运行/审批决策同步：从发起节点创建审批实例，到人工审批/结果节点写回 DataSrv，再由 App 运行态和全局审批中心读取同一实例。

### 推进记录：全局审批中心按 App 多身份同步审批结果（2026-06-30）

已完成：

- 修复全局审批中心的 App 匹配逻辑：审批实例可能使用 canonical DataSrv app id（如 `expense`），而 GUI 本地安装态 App 可能使用 `datasrv-installed-expense` 或市场 id。现在按本地 id、canonical manifest id、DataSrv appID、market capability id 统一匹配。
- 全局审批中心的 App 名称映射也使用同一套多身份 key，避免 DataSrv 回流的审批实例在全局视图中只显示裸 id。
- 新增前端回归验收：`syncs global approval decisions when the approval instance uses the canonical DataSrv app id`。
- 验收覆盖：全局审批中心打开 canonical app_id 的待审批实例，点击通过后仍能定位到对应企业审批型 App，并调用 `SyncMaclawAppApprovalInstanceToDataSrv`，同步 `app_id`、`dataset_id`、`object_role`、`approval_id`、`record_id`、`result_payload`、`outputs`。

验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "syncs global approval decisions when the approval instance uses the canonical DataSrv app id"
```

结果：新增用例通过。

对应全链路意义：

```text
DataSrv / Hub 安装回流可能产生 datasrv-installed-* 本地 App
  -> 审批实例仍按 canonical app_id 进入我的申请 / 我审批的 / 全局审批中心
  -> 全局审批中心人工通过 / 拒绝 / 需关注时能找回 MaClaw App 超级 Skill 定义
  -> 继续携带 workflow contract、DataSrv dataset/object、审批 id 和结果包同步回 DataSrv
  -> App 运行态和全局审批中心读取同一审批实例的可见性更稳定
```

下一步继续推进真实 workflow skill 运行/结果节点回写的最终样例 E2E：从发起节点创建审批实例，到人工审批或结果节点完成，再由 DataSrv 和 GUI 两侧读取同一份最终结果包。
### 推进记录：DataSrv 远端审批结果包覆盖本地 pending 快照（2026-06-30）

已完成：

- 补强 GUI 后端审批实例列表合并验收：当本地缓存里仍是 pending 审批实例，而 DataSrv `/api/v1/data/approvals` 已返回同一 `approval_id/workflow_instance_id` 的 approved 最终实例时，远端最终状态必须覆盖本地状态。
- 测试现在不仅断言 status/lane/result payload，还断言远端返回的 `outputs` 和 `artifacts` 能进入 `ListMaclawAppApprovalInstances` 的结果。
- 保留本地展示上下文：App 名称、事件时间线等本地补充信息继续保留；远端结果上下文：业务备注、当前节点、状态、结果包、文件产物以 DataSrv 为准。

验证：

```powershell
cd D:\workprj\aicoder
go test ./gui -count=1 -vet=off -run "TestListMaclawAppApprovalInstancesMergesDataSrvWithLocal" -timeout 180s
go test ./gui -count=1 -vet=off -run "TestSyncMaclawAppApprovalInstanceToDataSrv|TestListMaclawAppApprovalInstancesMergesDataSrvWithLocal|TestListMaclawAppApprovalInstancesAllLoadsDataSrvLane|TestListMaclawAppApprovalInstancesAllInfersDataSrvLanesForCurrentUser" -timeout 240s
```

结果：目标 GUI Go 测试通过。

对应全链路意义：

```text
App 运行态/全局审批中心先写入本地 pending 审批实例
  -> workflow skill 或人工审批把最终结果同步到 DataSrv RecordApproval
  -> GUI 再次读取审批实例列表
  -> 同一审批实例按远端 approved/rejected/attention 状态、result_payload、outputs、artifacts 覆盖本地 pending 快照
  -> 我的申请 / 我审批的 / 已处理 / 需关注 能看到同一份最终结果包和文件产物
```

下一步继续把真实 workflow skill 运行结果写回 DataSrv 的过程串进前端黄金 E2E：从 App Studio 样例发起审批 workflow，到轮询完成、结果节点回写，再到全局审批中心读取最终实例。
### 推进记录：DataSrv 审批 review 后列表回读保留最终结果包（2026-06-30）

已完成：

- 补强 DataSrv HTTP 层审批验收：审批 `review` 写入最终 workflow 结果后，通过 `/api/v1/data/approvals?workflow_skill_id=...&workflow_version=...` 列表查询同一审批实例时，必须保留最终 `result_payload`、`outputs` 和 `artifacts`。
- 这补齐了 detail/timeline/business record 已有断言之外的列表级合同，确保 GUI 全局审批中心、App 运行态审批工作台和 agent 诊断工具按 workflow 维度查询时不会只拿到状态而丢失结果包。
- 验收覆盖最终批准结果：`approval_result=approved`、`business_status=approved`、两个输出块、嵌套 artifact 输出、顶层 `approval.pdf` 文件产物。

验证：

```powershell
cd D:\workprj\aicoder\datasrv
go test ./structureddata -count=1 -vet=off -run "TestHTTPServerRecordApprovalsCarryMaClawAppSemantics" -timeout 180s
go test ./structureddata -count=1 -vet=off -run "TestHTTPServerRecordApprovalsCarryMaClawAppSemantics|TestHTTPServerAppInstallationPUTNormalizesStudioGovernancePayload|TestListAppInstallationsFiltersNestedInstallEvidence" -timeout 240s
```

结果：目标 DataSrv HTTP 测试通过。

对应全链路意义：

```text
审批 workflow skill 完成 / 人工审批 review
  -> DataSrv RecordApproval 写入最终 result_payload、outputs、artifacts
  -> GUI 或 agent 按 workflow skill/version 查询审批实例列表
  -> 列表项直接包含最终结果包和文件产物
  -> App 运行态、全局审批中心、Hub 安装证据和诊断工具可以使用同一份 DataSrv 事实
```

下一步继续推进同一黄金样例的前端联动：workflow 运行完成后，不只在当前 App 详情显示结果，也通过全局审批中心和 DataSrv summary 入口按结果类型定位同一审批实例。
### 推进记录：全局审批概览消费 DataSrv 结果证据摘要（2026-06-30）

已完成：

- 扩展前端 `dataSrvApprovalSummaryItem` 的结果类型解析，不再只依赖 `result_contract_types` 和 run evidence outputs。
- 全局审批中心的 DataSrv 审批概览现在会读取 DataSrv canonical 摘要字段：
  - `test_evidence_result_type`
  - `test_evidence_output_type`
  - `test_evidence_output_types`
  - `test_evidence_output_kinds`
  - `test_evidence_artifact_type`
  - `test_evidence_artifact_types`
- 更新前端验收：文档输出概览不再靠 `result_contract_types` 声明 `document`，而是靠 DataSrv 从测试证据提升出的 `test_evidence_output_type=document` 命中并展示。

验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "shows DataSrv approval app summary with approval result filters"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

结果：`AppsPage.test.tsx` 198 个用例全部通过。

对应全链路意义：

```text
审批 workflow / App Studio 测试产生 outputs、artifacts、result_payload
  -> DataSrv 安装/审批证据提升 test_evidence_* 摘要
  -> 全局审批中心 DataSrv 概览读取这些摘要
  -> 用户可按“文档输出 / 内容输出”等结果类型定位审批型 App 和最终审批实例
  -> 不再要求每个安装记录都完整声明 result_contract_types 才能出现在结果类型入口
```

下一步继续推进结果类型入口到真实审批实例列表的深链接：从 DataSrv 概览点击某一类结果后，进一步按 approval_id/workflow_instance_id 聚焦 GUI 本地/远端同一审批实例。
### 推进记录：DataSrv 概览行点击聚焦同一审批实例（2026-06-30）

已完成：

- 新增前端匹配逻辑：DataSrv 审批概览中的单条结果行会按 `approval_id`、`workflow_instance_id`、`record_id` 和 App 多身份 id 匹配 GUI 当前审批实例列表。
- 点击概览行时不再只是填入搜索词，而是会直接设置 `selectedInstanceId`，让右侧详情区聚焦同一审批实例。
- 回归验收扩展：点击“文档输出”概览中的 `Approval PDF exporter` 后，GUI 审批中心右侧详情必须显示同一审批实例标题、远端审批 ID `approval-datasrv-document-1` 和结果包输出 `Approval PDF`。

验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "shows DataSrv approval app summary with approval result filters"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

结果：`AppsPage.test.tsx` 198 个用例全部通过。

对应全链路意义：

```text
DataSrv app_installations / 审批证据摘要
  -> 全局审批中心展示结果类型概览
  -> 用户点击某个结果行
  -> GUI 按 approval_id / workflow_instance_id / record_id 定位本地或远端同一审批实例
  -> 右侧详情直接显示当前节点、远端审批 ID、结果包和文件产物
  -> “从结果类型入口定位审批实例”的使用路径闭环
```

下一步继续推进最终 E2E 的真实样例串联：从 App Studio 制作/测试/上传，到 Hub 下载重装，再到 DataSrv 概览和运行态审批详情共享同一实例证据。
### 推进记录：黄金样例 Hub 重装后进入全局审批中心（2026-06-30）

已完成：

- 扩展前端黄金链路验收：`round-trips a Studio-built approval app through Hub install into the runtime workspace` 不再只验证运行态 App 工作台。
- 同一 Studio-built `Golden Expense Approval` 经 Review/publish、Hub 选装安装、安装证据回灌、运行态审批工作台展示后，继续切换到全局 `Approval status` 审批中心。
- 全局审批中心读取同一审批实例 `wf-studio-golden-1 / approval-studio-golden-1`，并在右侧详情展示：
  - 业务记录 `EXP-GOLDEN-1`
  - 当前节点 `expense.result_feedback`
  - 业务状态 `finance_approved`
  - 结果输出 `Golden approval decision`
  - 文件产物 `golden-expense-approval.pdf`

验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "round-trips a Studio-built approval app through Hub install into the runtime workspace"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

结果：`AppsPage.test.tsx` 198 个用例全部通过。

对应全链路意义：

```text
App Studio 制作/测试企业审批型 App
  -> Review / publish 生成包含动态 UI、依赖、workflow、审批实例和结果包的发布包
  -> Hub 下载重装并回灌安装证据
  -> 运行态审批工作台显示同一审批实例
  -> 全局审批中心读取同一审批实例和结果包
  -> App 级运行入口与全局审批入口共享同一份审批事实
```

下一步继续把这条黄金样例向 DataSrv HTTP 注册和真实 workflow skill 运行回写组合验证推进，减少前端 mock 与后端事实之间的距离。
### 推进记录：GUI 后端串联安装注册、运行审批与全局回读（2026-06-30）

本轮继续减少前端 mock 黄金链路和后端事实之间的距离，把已有的安装注册测试扩展为更接近真实企业审批型 App 的后端链路验收。

已完成：

- 扩展 `TestRecordMaclawAppInstallRegistersApprovalAppWithDataSrv`。
- 同一企业审批型 App 安装后，先验证 `RecordMaclawAppInstall` 写入 DataSrv app installation，并保留动态 layout、workflow contract、依赖验证、测试审批实例和结果包。
- 随后模拟运行态发起审批：`SyncMaclawAppApprovalInstanceToDataSrv` 创建 DataSrv RecordApproval，并携带 `workflow_instance_id`、workflow node list、result payload、outputs、artifacts。
- 再模拟人工审批完成：同一 `approval-runtime-1` 通过 `review_record_approval` 写回 `approved`、`finance_approved`、最终节点 `expense.result_pack`、内容输出和 `runtime-approval.pdf`。
- 最后通过 `ListMaclawAppApprovalInstancesAll("handled")` 从 DataSrv 全局审批入口回读同一实例，断言 app id、approval id、workflow skill/version、当前节点、结果包和文件产物一致。

验证：

```powershell
cd D:\workprj\aicoder
go test ./gui -count=1 -vet=off -run "TestRecordMaclawAppInstallRegistersApprovalAppWithDataSrv" -timeout 240s
go test ./gui -count=1 -vet=off -run "TestRecordMaclawAppInstallRegistersApprovalAppWithDataSrv|TestListMaclawAppApprovalInstancesMergesDataSrvWithLocal|TestSyncMaclawAppApprovalInstanceToDataSrv" -timeout 300s
```

结果：目标 GUI 后端测试通过。

对应全链路意义：

```text
安装企业审批型 MaClaw App
  -> GUI 注册 DataSrv app_installations，保存布局、依赖、workflow 与审批测试证据
  -> 运行态发起审批实例并创建 DataSrv RecordApproval
  -> 人工审批 / workflow 结果节点把最终结果包 review 回 DataSrv
  -> 全局审批中心按 handled lane 回读同一审批实例
  -> App 运行态与全局审批入口共享同一份 DataSrv 最终事实
```

这一步仍不等于整套 MaClaw App 完成，但把“安装注册”和“运行审批完成后全局回读”第一次放进同一个 GUI 后端验收里。下一步继续推进真实 workflow skill 运行器与 App Studio 黄金样例的组合测试，减少手工 mock 审批节点的比例。
### 推进记录：新增审批型 App 发起 workflow 后端入口（2026-06-30）

本轮继续推进“发起节点 UI 数据提交 -> 审批 workflow skill 运行 -> 审批实例数据管理 -> DataSrv 事实回写”的后端契约。此前 GUI 已能手工记录审批实例并同步 DataSrv，但缺少一个面向 MaClaw App 运行态的统一发起入口，前端/测试容易直接拼 approval instance。

已完成：

- 新增 GUI 后端方法 `StartMaclawAppApprovalWorkflow`。
- 新增输入契约 `MaclawAppApprovalWorkflowStartInput`，承载 app id、record id、dataset/object role、申请人、审批人、form data、business payload、业务备注、workflow skill/version、当前节点等发起节点数据。
- 发起时读取本地安装记录，要求目标 App 是 `enterprise_approval_app`，并复用安装时保存的 workflow contract、workflow skill version snapshot、approval binding。
- 根据安装包/安装证据中的 workflow mapping 选择 approval node / submit node，默认生成 pending 审批实例。
- 复用 `RecordMaclawAppApprovalInstance` 和 `SyncMaclawAppApprovalInstanceToDataSrv`，把同一审批实例写入本地审批实例 registry，并创建 DataSrv RecordApproval。
- 同步补齐 Wails 前端绑定 `StartMaclawAppApprovalWorkflow`，供 App 运行态后续直接调用。
- 新增 `TestStartMaclawAppApprovalWorkflowCreatesDataSrvApproval`，验证发起入口会创建 DataSrv approval，并携带 app/workflow/data/result payload/form data/business payload，同时本地审批实例保存远端 approval id。

验证：

```powershell
cd D:\workprj\aicoder
go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowCreatesDataSrvApproval" -timeout 240s
go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowCreatesDataSrvApproval|TestRecordMaclawAppInstallRegistersApprovalAppWithDataSrv|TestMaclawAppApprovalRuntimeContractUsesInstallSnapshot|TestSyncMaclawAppApprovalInstanceToDataSrv" -timeout 300s
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "round-trips a Studio-built approval app through Hub install into the runtime workspace"
```

结果：目标 GUI 后端测试与前端黄金用例通过。

对应全链路意义：

```text
企业审批型 MaClaw App 运行态
  -> 发起节点提交 form data / business payload
  -> StartMaclawAppApprovalWorkflow 校验已安装 App 的 workflow contract 与绑定
  -> 创建本地 pending 审批实例
  -> 同步创建 DataSrv RecordApproval
  -> 本地实例保存远端 approval id
  -> 后续人工审批 / workflow 结果节点可继续 review 写回最终结果包
```

这一步把“发起节点 UI 数据提交”从前端手工拼实例推进为后端稳定 API。下一步需要把 App 运行态的提交按钮接到该 API，并继续接真实 Hub workflow runner 的 initiate/complete 结果，而不是只在 GUI 后端生成 pending 实例。
### 推进记录：运行态审批发起接入 StartMaclawAppApprovalWorkflow（2026-06-30）

本轮把审批型 MaClaw App 的前端运行态接入 GUI 后端新增的 `StartMaclawAppApprovalWorkflow` 入口，完成了“动态 App 运行 -> workflow skill 启动 -> 后端创建 pending 审批实例 -> DataSrv 审批同步 -> workflow 完成后结果回写”的前端主链路收口。

已落地内容：

- 审批型 App 点击运行后，先按 App manifest / install evidence / workflow binding 生成统一的 workflow contract 输入，包括 `record_ref`、`applicant`、`business_payload`、workflow contract 和 required inputs。
- workflow skill 仍通过 `RunNLSkillAsync` 启动，输入中保留 app id、审批事件、对象角色、DataSrv dataset、当前节点、状态映射、workflow 版本等字段。
- workflow skill 成功启动后，前端调用 `StartMaclawAppApprovalWorkflow`，由 GUI 后端按已安装 App 的版本快照和审批 runtime contract 创建 pending 审批实例，并同步 DataSrv。
- 前端保留降级路径：如果新后端发起入口失败，仍回退到本地 `RecordMaclawAppApprovalInstance` + `SyncMaclawAppApprovalInstanceToDataSrv`，保证旧安装/测试环境可继续运行。
- workflow 完成后，最终审批结果继续归一化为审批实例结果包，覆盖 `approval_result`、`business_status`、`business_record`、inline text、outputs、artifacts，并回写 DataSrv。
- 前端测试 mock 调整为更接近真实后端：实例 ID 和审批 workflow id 优先尊重输入，避免旧 mock 覆盖真实运行态实例。

验证：

- `npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "records and completes an approval instance when running an approval app"`
- `npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "passes workflow node mapping into approval skill runs and approval instances|does not record approval instance when workflow skill launch fails|records attention approval results as view-only instance feedback|records and completes an approval instance when running an approval app"`
- `npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "round-trips a Studio-built approval app through Hub install into the runtime workspace"`
- `go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowCreatesDataSrvApproval|TestRecordMaclawAppInstallRegistersApprovalAppWithDataSrv|TestMaclawAppApprovalRuntimeContractUsesInstallSnapshot|TestSyncMaclawAppApprovalInstanceToDataSrv" -timeout 300s`

剩余缺口：

- Hub 侧真实 workflow skill 的节点推进/完成事件还需要进一步和审批实例状态机做更强的端到端联调。
- App Studio 的可视化布局设计器仍需要继续产品化：自动生成布局、用户拖拽调整、保存到 App 信息文件、测试后上传 Hub 的体验需要更完整。
- 还需要补一条跨 DataSrv + Hub + GUI 前端的 golden path：制作 App、上传能力市场、安装依赖 skill、运行审批、查看我的申请/我审批的、最终结果/文件回读。
### 推进记录：真实 workflow 节点路径回流审批实例（2026-06-30）

本轮继续补“审批 workflow skill 运行 -> 审批实例数据管理 -> 当前节点/节点路径可见”的运行态细节。上一轮已经把前端审批型 App 运行接入 `StartMaclawAppApprovalWorkflow`，本轮进一步让 workflow skill 的真实输出能携带完整节点路径，而不是只依赖 pending 阶段的单个审批节点。

已完成：

- 前端审批结果归一化新增 workflow 节点列表提取，支持从 skill run status / summary / output blocks / approval_instance / result_payload 中读取：
  - `current_node_ids` / `currentNodeIDs` / `currentNodeIds`
  - `workflow_node_ids` / `workflowNodeIDs` / `workflowNodeIds`
  - `workflow_nodes` / `workflowNodes`
  - `node_ids` / `nodeIds` / `nodes`
- 节点列表支持数组、节点对象、JSON 字符串，以及 `->`、`→`、逗号、分号、竖线、换行等分隔的字符串。
- workflow 完成态合成审批实例时，会把 pending 阶段节点、真实 workflow 返回节点、当前结果节点合并去重后写入：
  - `current_node_ids`
  - `workflow_node_ids`
- 最终 `RecordMaclawAppApprovalInstance`、`SyncMaclawAppApprovalInstanceToDataSrv` 和本地运行历史中的 approvalInstance 都能保留同一条节点路径。
- 扩展前端审批运行测试，模拟 workflow skill 返回 `['expense.manager_review', 'finance.final_review', 'expense.result_feedback']`，断言完成态审批实例、DataSrv 同步 payload、本地运行证据均保留完整路径。

验证：

```powershell
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "records and completes an approval instance when running an approval app"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "passes workflow node mapping into approval skill runs and approval instances|does not record approval instance when workflow skill launch fails|records attention approval results as view-only instance feedback|records and completes an approval instance when running an approval app"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "round-trips a Studio-built approval app through Hub install into the runtime workspace"
go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowCreatesDataSrvApproval|TestRecordMaclawAppInstallRegistersApprovalAppWithDataSrv|TestMaclawAppApprovalRuntimeContractUsesInstallSnapshot|TestSyncMaclawAppApprovalInstanceToDataSrv" -timeout 300s
```

对应全链路意义：

```text
企业审批型 MaClaw App 运行
  -> workflow skill 返回真实节点轨迹
  -> 前端归一化 approval completion
  -> 审批实例保留 current_node 和完整 workflow_node_ids
  -> DataSrv 审批实例同步拿到同一条路径
  -> 我的申请 / 我审批的 / 已处理 / 全部 能看到当前节点，也能追溯节点路径
```

剩余缺口继续聚焦：

- 真实 Hub workflow skill 的节点推进事件还需要更强的端到端联调，尤其是 pending/running 期间的中间节点事件，而不仅是最终完成态结果。
- App Studio 可视化布局设计器仍需从“布局证据能往返”推进到“用户可视化拖拽/调整/保存”的完整产品体验。
### 推进记录：running 阶段 workflow 中间节点实时进入审批实例（2026-06-30）

本轮继续补“审批 workflow skill 运行中 -> 审批实例当前节点可见”的实时链路。此前已经支持最终完成态把真实 workflow 节点路径回流到审批实例；本轮把 pending/running 阶段的中间节点也接入运行态审批实例，让用户不必等 workflow 完成才能看到当前节点变化。

已完成：

- 新增 running 进度归一化：从 skill run status / summary / session_progress / output blocks / approval_instance / result_payload 中提取当前节点、节点路径、业务状态、结果状态和进度文本。
- 审批 App 轮询到 `running` 生命周期时，会按进度 key 去重；只有当前节点、节点路径、状态或摘要变化时才更新审批实例，避免 1.5 秒轮询产生重复事件。
- running 进度会更新本地审批实例：
  - `current_node`
  - `current_node_ids`
  - `workflow_node_ids`
  - `business_status`
  - `result_status`
  - `result`
  - `workflow_progress` 事件
- 更新后的实例会立即刷新运行态审批工作台，并 best-effort 写入本地审批实例缓存。
- running 阶段暂不触发 DataSrv 审批同步，避免轮询期间重复创建/写入远端审批记录；最终完成/失败/取消仍走已有权威同步链路。
- 新增前端测试覆盖 workflow 仍在 running 时返回 `finance.director_review`，断言本地审批实例立即记录 `workflow_progress`，并保留 pending 阶段节点 + workflow 返回节点的合并路径。

验证：

```powershell
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "updates approval instance progress while workflow skill is still running"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "updates approval instance progress while workflow skill is still running|passes workflow node mapping into approval skill runs and approval instances|does not record approval instance when workflow skill launch fails|records attention approval results as view-only instance feedback|records and completes an approval instance when running an approval app"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "round-trips a Studio-built approval app through Hub install into the runtime workspace"
go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowCreatesDataSrvApproval|TestRecordMaclawAppInstallRegistersApprovalAppWithDataSrv|TestMaclawAppApprovalRuntimeContractUsesInstallSnapshot|TestSyncMaclawAppApprovalInstanceToDataSrv" -timeout 300s
```

对应全链路意义：

```text
企业审批型 MaClaw App 发起
  -> StartMaclawAppApprovalWorkflow 创建 pending 审批实例
  -> workflow skill running 期间返回当前节点/节点路径
  -> 运行态审批实例立即更新当前节点和 workflow_progress 事件
  -> 用户在我的申请/审批工作台能看到流程正在推进到哪个节点
  -> workflow 完成后再进行最终结果包和 DataSrv 权威同步
```

后续需要继续推进：

- 如果 DataSrv 增加安全的“更新已有审批实例进度/节点”接口，可把 running 阶段节点推进从本地缓存进一步扩展到远端实时状态，但需要避免重复 create approval。
- App Studio 的可视化布局编辑器仍需继续推进到完整拖拽/调整/保存体验。

### 推进记录：running 审批进度安全同步到 DataSrv（2026-06-30）

本轮把上一阶段“running 进度只更新本地审批实例”的临时策略，推进为“已有远端 approval_id 时安全更新 DataSrv 审批进度”。核心目标是让 workflow skill 运行中的当前节点成为 DataSrv 事实，同时避免轮询期间重复创建审批记录或误写最终审批结果。

已完成：

- DataSrv 新增审批进度更新契约：`UpdateRecordApprovalProgressInput`。
- DataSrv 新增 HTTP 入口：`POST /api/v1/data/approvals/{approvalId}/progress`。
- 该入口只允许更新 pending 审批的运行态字段：workflow instance、当前节点、节点路径、workflow version、decision id、detail url、assignee、from/to status、business/result status、result payload、outputs、artifacts。
- 该入口不会写入 `decision`、`reason`、`reviewed_by`、`reviewed_at`，也不会把 pending 审批改成 approved/rejected。
- DataSrv progress 更新会复用业务记录同步路径，把以下字段写回业务记录：
  - `approval_status=pending`
  - `approval_current_node`
  - `approval_workflow_node_ids`
  - `business_status`
  - `result_status`
  - `approval_result_payload`
  - `approval_outputs`
  - `approval_artifacts`
- GUI MIS data tool 新增 `update_record_approval_progress` / `mis.approval.progress` 动作。
- GUI `SyncMaclawAppApprovalInstanceToDataSrv` 调整分支顺序：
  - `attention + approval_id`：仅更新业务记录，不 review；
  - `pending + approval_id`：调用 DataSrv progress；
  - `approved/rejected + approval_id`：调用 review；
  - `pending + 无 approval_id`：创建 RecordApproval。
- GUI 业务记录 patch 现在会稳定携带 `approval_id` / `record_approval_id`，方便从业务记录反查审批实例。
- 前端 running progress 现在只有在已有 `approval_id` / `record_approval_id` 时才同步 DataSrv，避免尚未创建远端审批实例时产生重复 create。

验证：

```powershell
cd D:\workprj\aicoder\datasrv
go test ./structureddata -count=1 -vet=off -run "TestHTTPServerRecordApprovalsCarryMaClawAppSemantics|TestUpsertAppInstallation|TestListAppInstallations|TestHTTPServerAppInstallation|TestHTTPServerRequiresBearerTokenAndHandlesRecords" -timeout 240s

cd D:\workprj\aicoder
go test ./gui -count=1 -vet=off -run "TestSyncMaclawAppApprovalInstanceToDataSrv|TestStartMaclawAppApprovalWorkflowCreatesDataSrvApproval|TestRecordMaclawAppInstallRegistersApprovalAppWithDataSrv|TestMaclawAppApprovalRuntimeContractUsesInstallSnapshot" -timeout 300s

cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "updates approval instance progress while workflow skill is still running|passes workflow node mapping into approval skill runs and approval instances|does not record approval instance when workflow skill launch fails|records attention approval results as view-only instance feedback|records and completes an approval instance when running an approval app"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "round-trips a Studio-built approval app through Hub install into the runtime workspace"
```

对应全链路意义：

```text
企业审批型 MaClaw App 发起
  -> StartMaclawAppApprovalWorkflow 创建本地 pending 实例并拿到 DataSrv approval_id
  -> workflow skill running 期间返回当前节点/节点路径
  -> 前端更新本地审批实例 workflow_progress
  -> SyncMaclawAppApprovalInstanceToDataSrv 使用 approval_id 调用 DataSrv progress
  -> DataSrv 更新同一 pending RecordApproval 和业务记录当前节点
  -> 我的申请 / 我审批的 / 全局审批中心 / DataSrv 业务记录都能看到运行中节点
  -> workflow 完成后再由 review 写入最终通过/拒绝/关注和完整结果包
```

剩余缺口继续聚焦：

- 真实 Hub workflow skill 的节点推进事件还需要和 DataSrv progress 接口做非 mock 端到端联调。
- App Studio 可视化布局编辑器仍需继续产品化：自动生成后拖拽调整、区域锁定、保存布局、测试后上传 Hub。
- 还需要补一条跨 DataSrv + Hub + GUI 的黄金样例：制作审批型 App、上传能力市场、下载安装依赖 skill、发起审批、运行中节点更新、最终 review、结果包回读。
### 推进记录：App Studio 区域顺序可视化保存与运行态恢复（2026-06-30）

本轮从 App Studio 动态 UI 设计器继续推进“自动生成后用户可视化调整，保存到应用信息文件，再测试/上传 Hub”的产品化链路。此前已经支持模板、密度、主区域、输出区域、区域位置和可见性；本轮补上同一布局内的区域顺序，使生成界面不只是表单配置，而更接近传统企业软件的界面编排能力。

已完成：

- `RuntimeWorkspaceRegion` 新增可持久化 `order` 字段。
- Studio 布局设计器新增区域上移/下移控制，用户可以调整输入区、列表区、详情区、结果区在运行态网格中的显示先后。
- `normalizeRuntimeWorkspaceRegions` 会保留 manifest / install evidence 中已有的 `order`。
- `applyStudioWorkspaceLayout` 继续把布局写入 `manifest.ui.layouts[entry].regions`，现在包含 placement、visible、order 三类可视化调整证据。
- 运行态 `runtimeWorkspaceOrder` 在检测到自定义 `order` 时按 manifest 顺序排列输入区、业务/审批工作区、状态/详情区、输出区和历史区；没有 `order` 的旧应用继续走原有默认顺序，保持兼容。
- Studio 区域控制面板样式调整为企业工作台式紧凑三列：区域名称、位置选择、顺序按钮；移动端自动折为单列。
- 扩展前端测试：用户在 Studio 中移动区域位置并上移详情区后，断言：
  - `manifest.ui.layouts.business_workspace.regions` 保存 `order`；
  - Review / publish 包中的 `app.ui` 和 `governance.workspaceLayout` 都包含同一顺序证据；
  - 运行态根据 `order` 将详情/状态区排在业务列表工作区之前。

验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "moves workspace regions from the visual layout preview into saved manifest regions"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "saves app studio layout choices into app manifests|honors saved workspace region placements over summary layout fields|saves visual workspace region visibility and applies it in the runtime panel|moves workspace regions from the visual layout preview into saved manifest regions|edits app studio layout visually and persists it|restores installed workspace layout into App Studio editing and republishes it"
```

对应全链路意义：

```text
App Studio 自动生成动态 UI
  -> 用户在可视化布局设计器中调整区域位置、显隐和顺序
  -> 布局证据写入 MaClaw App manifest
  -> Review / publish 包携带同一 workspaceLayout governance 证据
  -> Hub 安装/恢复后运行态按 manifest 布局渲染
  -> 企业普通应用 / 企业审批型应用 / 工具型应用都能逐步形成传统软件式工作台界面
```

剩余缺口继续聚焦：

- 区域顺序目前是按钮式上移/下移，后续还需要进一步升级为更直观的拖拽或键盘排序体验。
- 仍需把“测试后上传 Hub”的 Studio 工作流做成更明确的强制门禁：未测试/布局证据缺失/依赖未声明时阻止发布或给出清晰修复动作。
- 还需要补全跨 DataSrv + Hub + GUI 的黄金样例，把 Studio layout、依赖 skill 安装、审批 workflow 运行、审批实例管理、结果文件回读全部串成一条可重复验收链。
### 推进记录：App Studio 发布包 ready-only 门禁（2026-06-30）

本轮继续推进“设计好后保存、测试后上传 Hub 能力市场”的发布闭环。此前单个 App 的提交按钮已经会根据发布检查禁用，但顶部“复制提交包”仍会把所有本地 App 放进包预览，未测试草稿也可能出现在提交包中；这不符合“测试后上传”的门禁语义。本轮将批量提交包改为只包含 ready 应用，并补充未 ready 应用的修复入口。

已完成：

- `PublishPane` 统一复用 `buildPublishChecks` 计算每个本地 App 的发布准备度。
- 新增 `readyPublishApps`，提交包预览和复制提交包只使用 ready 应用。
- 当没有任何 ready 应用时，顶部“复制提交包”按钮禁用，避免把未测试/布局缺失/依赖未验证的草稿作为上传包复制出去。
- 未 ready 的发布卡片现在显示“修复审核问题”入口，用户可直接回到 App Studio 编辑应用信息、布局、依赖、结果契约或测试协议。
- 依赖检查失败时，即使还没有市场 review issue，也显示“处理依赖问题”入口，便于安装/修复依赖 Skill。
- 扩展前端测试覆盖：
  - 只有未测试草稿时，提交包 `apps` 为空且复制按钮禁用；
  - 同时存在 ready App 和未测试草稿时，提交包只包含 ready App；
  - 原有布局缺失、ready 提交、本地 review 状态仍保持通过。

验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "shows local apps in the review and publish checklist|keeps untested draft apps out of the copied publish package|submits ready local apps into local review state|blocks publish readiness when workspace layout misses required region roles"
```

对应全链路意义：

```text
App Studio 创建/编辑 MaClaw App
  -> 发布检查验证 manifest、Skill 依赖、workspace layout、workflow contract、result contract、test protocol、run evidence
  -> 未 ready 应用只能修复，不能进入提交包
  -> ready 应用进入 maclaw.app.pack.v1
  -> 提交/复制到企业能力市场时包内应用都带当前测试证据和治理证据
```

剩余缺口继续聚焦：

- 仍需要把 ready-only 门禁进一步接到真实 Hub upload/sync 的后端校验，确保 GUI 之外的提交入口也不能绕过测试证据。
- 还需要补全跨 DataSrv + Hub + GUI 的黄金样例：制作审批型 App、上传能力市场、Hub 审核/发布、安装依赖 Skill、发起审批、running 节点同步、最终结果/文件回读。
### 推进记录：后端提交队列强制 ready 门禁（2026-06-30）

本轮把前端 ready-only 发布包门禁继续下沉到 Go 后端提交入口。此前 `SubmitMaclawAppPackage` 会计算 governance review issues，但即使存在 `error` 级问题也会写入本地提交队列；这意味着 GUI 之外的调用仍可能把未测试、缺布局、缺结果契约、缺依赖验证或审批工作流契约不完整的 App 推进到市场同步链路。本轮改为后端权威阻断。

已完成：

- `SubmitMaclawAppPackage` 在生成并归一化 `reviewIssues` 后，调用阻断检查；发现 `error` / `critical` 级问题时直接返回错误，不创建提交记录。
- 新增 `firstBlockingMaclawAppReviewIssue`，保留 `warning` 可提交、`error` / `critical` 必须修复的语义。
- 阻断错误包含具体 path 和 message，例如 `apps[0].app.governance.testEvidence`、`workspaceLayout`、`dependencyVerification`、`workflowContract`、`resultCoverage`，便于 GUI 或调用方定位修复项。
- 后端阻断同时覆盖 manifest 自带治理证据和 `PlanMaclawAppInstall` 重新计算出的权威依赖状态，避免只伪造 `dependencyVerification` 就绕过依赖安装/启用检查。
- 更新 Go 测试：
  - 完整治理证据、完整依赖安装状态的 App 仍能入队，并保留 package fingerprint、依赖审计、submission evidence。
  - 缺测试证据、缺布局角色、缺测试协议、缺依赖验证、依赖未 ready、审批 workflow contract 缺失/不匹配、审批实例证据不完整、definition hash 陈旧、result coverage 不完整等场景，改为断言提交被阻断且不会创建队列文件。
  - corrupt queue 用例改为先提供 ready 包，确保验证的是队列损坏保护，而不是被治理门禁提前拦截。

验证：

```powershell
cd D:\workprj\aicoder
go test ./gui -count=1 -vet=off -run "TestSubmitMaclawAppPackage" -timeout 300s
```

对应全链路意义：

```text
App Studio 生成/编辑 MaClaw App
  -> 前端 ready-only 只把 ready App 放入提交包
  -> 后端 SubmitMaclawAppPackage 重新计算治理与依赖问题
  -> blocking issue 直接拒绝，不进入本地提交队列
  -> 本地队列只保存可同步到企业 Hub 能力市场的 ready 包
  -> 后续 Hub upload/sync、安装依赖 Skill、运行审批实例时有更可靠的前置证据
```

剩余缺口继续聚焦：

- 真实 Hub upload/sync 仍需做同等级后端强校验，尤其是远端能力市场审核、包签名、依赖 Skill 发布状态、安装引用可解析性。
- 还需要补一条 DataSrv + Hub + GUI 的黄金样例：App Studio 制作审批型 App、测试并提交、Hub 审核/发布、安装 App 时自动安装/检查 workflow Skill、发起审批、同步当前节点、完成审批、回读结果内容和文件。
- App Studio 仍需要继续产品化为更完整的传统软件式可视化设计器，包括拖拽布局、控件属性面板、运行态预览、测试失败回跳到具体修复区域。
### 推进记录：Hub 同步前强制复核 ready 与包指纹（2026-06-30）

本轮把上一轮的后端提交门禁继续推进到企业 Hub upload/sync 环节。此前本地 `SubmitMaclawAppPackage` 已经会阻断未 ready 的 App 包，但 `SyncMaclawAppPackageSubmissionToHub` 只检查依赖 Skill 是否已发布，对本地队列里的 package 治理证据没有重新计算；如果队列文件被手工改坏、测试证据过期，或者 package payload 与提交时的指纹不一致，仍可能走到 Hub 请求之前的薄弱区。

已完成：

- 抽出 `maclawAppReadyReviewIssuesForPackage`，本地提交与 Hub sync 共用同一套治理审查计算：workspace layout、result contract、test protocol、run evidence、definition hash、result coverage、dependency verification、approval workflow contract 等口径保持一致。
- `SyncMaclawAppPackageSubmissionToHub` 现在强制校验本地队列记录：
  - package payload 不存在时拒绝同步；
  - package fingerprint 重新计算后必须与提交记录里的 `PackageSHA` 一致；
  - `PlanMaclawAppInstall` 必须能重新生成安装计划；
  - blocking review issue 存在时返回 `maclaw app package is not ready for Hub sync`，不会发起 HTTP 请求；
  - 依赖 Skill 发布状态校验继续保留，确保上传到 Hub 的 App 依赖可被其它机器解析和安装。
- 新增测试覆盖：
  - 队列 package 被篡改但 `PackageSHA` 未更新时，Hub sync 以 `package fingerprint mismatch` 拒绝，Hub 服务端不会被调用；
  - 队列 package 被改成缺少 `testEvidence`，且指纹也被同步更新时，Hub sync 会重新计算治理 ready 状态并拒绝，不调用 Hub；
  - 原有 ready 包同步到 Hub、更新本地队列为 hub-backed 的金线仍保持通过。

验证：

```powershell
cd D:\workprj\aicoder
go test ./gui -count=1 -vet=off -run "TestSyncMaclawAppPackageSubmissionToHub" -timeout 300s
go test ./gui -count=1 -vet=off -run "TestSubmitMaclawAppPackage|TestSyncMaclawAppPackageSubmissionToHub" -timeout 300s
```

对应全链路意义：

```text
App Studio 生成 ready App 包
  -> SubmitMaclawAppPackage 本地强门禁入队
  -> 本地队列保存 package fingerprint 与治理证据
  -> SyncMaclawAppPackageSubmissionToHub 同步前重新校验 fingerprint、治理 ready、依赖发布状态
  -> 只有未被篡改且仍然 ready 的 package 才能发往企业 Hub 能力市场
  -> Hub 审核/市场分发/下载安装到依赖 Skill 的链路获得更可靠的入口质量
```

剩余缺口继续聚焦：

- 企业 Hub 服务端接收 `/api/capabilities/maclaw-apps/submit` 时还需要同等级校验，不能只依赖 GUI 客户端。
- 需要继续补真实 Hub 能力市场侧的审核/发布状态、包签名、依赖 Skill install_ref 解析、远端安装失败诊断。
- 最终仍要补 DataSrv + Hub + GUI 的黄金样例：制作审批型 App、测试、提交、Hub 审核、安装依赖 workflow Skill、发起审批、同步 running 节点、审批完成、结果内容/文件回读。
### 推进记录：Hub 服务端接收 MaClaw App 时强制 ready 审查（2026-06-30）

本轮把 MaClaw App 发布门禁继续下沉到企业 Hub 服务端 `/api/capabilities/maclaw-apps/submit`。此前 GUI 本地提交和同步前已经会重新计算 ready 状态、包指纹和依赖发布状态，但 Hub 接收端仍主要负责解析包并写入 `pending_review` 能力；如果绕过 GUI 直接 POST 未测试、缺布局、缺结果契约或依赖阻断的包，服务端仍可能进入审核队列。

调整后，Hub 在解析 `maclaw.app.v1` / `maclaw.app.pack.v1` 并计算规范化包指纹后，会先执行服务端 ready 审查，阻断以下断链风险：

- 缺动态工作区布局、缺 input/output 区域，导致安装后无法恢复传统软件式运行界面；
- 缺 `resultContract` 或 primary result，导致 DataSrv、安装记录和市场列表无法判断输出类型；
- 缺 `governance.testEvidence`、缺 run id、缺 test protocol fingerprint，导致“测试后上传”不可审计；
- 测试证据 result coverage 明确失败；
- 缺 `dependencyVerification`、依赖检查失败、missing/blocked 依赖仍存在，或验证结果没有保留 checked skills；
- 企业审批型应用缺 workflow mapping、缺 workflow contract、缺 workflow skill id / object role；
- 企业审批型应用缺 approval instance 测试证据、实例 id/status 或实例视图校验证据。

Hub 返回 `MACLAW_APP_PACKAGE_NOT_READY` 和完整 `review_issues`，并且不会创建 capability / version 记录。这样能力市场入口不再信任 GUI 客户端自报 ready，直接 HTTP 调用也必须满足同一套发布治理要求。

已补回归测试：

```powershell
go test ./hub/internal/httpapi -count=1 -run "TestCapabilityMaclawAppSubmit" -timeout 180s
```

覆盖 ready 包仍能进入 `pending_review`，以及缺测试证据、缺结果契约、依赖验证阻断、审批型 App 缺 workflow contract 时 Hub 直接拒绝且不写入能力。

对应全链路意义：

```text
App Studio 制作/测试
  -> GUI 本地发布 ready 门禁
  -> GUI 后端 SubmitMaclawAppPackage ready 门禁
  -> GUI 同步 Hub 前复核 package fingerprint + ready + 依赖发布状态
  -> Hub 服务端接收包再次执行 ready 审查
  -> pending_review 能力只接收具备动态 UI、依赖、workflow、结果和测试证据的 App 包
```

剩余缺口继续聚焦：

- Hub 审核通过后的发布包签名、版本冻结、review metadata 回灌和可安装状态切换还需要继续硬化；
- 依赖 Skill 的 `install_ref` 需要在 Hub/SkillMarket 侧做真实解析和失败诊断，尤其是 workflow skill、app skill、connector skill 的来源差异；
- 仍需补跨 DataSrv + Hub + GUI 的黄金样例：制作审批型 App、上传能力市场、Hub 审核发布、安装依赖 Skill、发起审批、running 节点同步、审批完成、结果内容/文件回读。
### 推进记录：Hub 审核批准前复核当前版本 ready 状态（2026-06-30）

本轮继续把 Hub 能力市场的治理门禁推进到审核阶段。上一轮已经让 `/api/capabilities/maclaw-apps/submit` 接收包时强制 ready 审查，但仍有一个边界风险：pending_review 能力进入 Hub 后，如果当前 version manifest 被历史脏数据、手工修补或迁移过程改坏，管理员 approve 时只更新状态，可能把不完整 App 发布为可安装能力。

调整后，Hub 管理端批准 MaClaw App 前会重新读取 capability 的 current version manifest，并执行同一套 ready 审查：动态工作区布局、结果契约、测试证据、依赖验证、审批型 workflow mapping/contract、审批实例证据等仍必须完整。若检查失败，接口返回 `MACLAW_APP_PACKAGE_NOT_READY` 和 `review_issues`，capability / version 保持 `pending_review`，不会进入 `approved`。

同时，依赖验证门禁进一步收紧：`dependencyVerification.skills` / `dependencies` 中的必需 Skill 必须携带 `install_ref` / `installRef`。这保证审核通过后的 App 包不只是“声明依赖存在”，而是能被其它机器从 Hub/SkillMarket 解析并安装。

已补回归测试：

```powershell
go test ./hub/internal/httpapi -count=1 -run "TestCapabilityMaclawApp(Submit|PackageDownload)|TestAdminCapabilityMaclawAppReview" -timeout 180s
```

覆盖：

- ready MaClaw App 仍可 submit、approve，并同步更新当前 version 状态；
- 缺测试证据等 unready 当前版本无法 approve，且状态保持 pending_review；
- reject 路径仍允许管理员写入 review issues，不被 ready 复核误伤；
- approved/published package download 继续保留动态 UI、workflow contract、测试证据和审批实例证据。

对应全链路意义：

```text
Hub 接收 ready 包
  -> pending_review 当前版本保存完整 manifest
  -> 管理员 approve 前重新读取 current version manifest
  -> 再次执行 ready + install_ref 审查
  -> 只有可安装、可恢复动态 UI、可运行 workflow、可审计结果证据的 App 才能进入 approved
```

剩余缺口继续聚焦：

- approved 到 published 的显式发布动作、包签名/版本冻结和 review metadata 回灌仍需继续硬化；
- `install_ref` 现在先做到存在性门禁，下一步要接真实 Hub/SkillMarket 解析与失败诊断；
- 继续补跨 DataSrv + Hub + GUI 的黄金样例，把审核批准后的下载安装、依赖安装、审批运行中节点同步和最终文件/内容回读串起来。
### 推进记录：Hub 审核通过与市场发布分离（2026-06-30）

本轮继续收紧 Hub 能力市场分发语义。此前 MaClaw App 在 Hub 侧 `approved` 和 `published` 都被视为可下载安装，这会把“审核通过”和“正式进入能力市场分发”混在一起；企业场景里，审核通过后仍可能需要发布人确认版本、发布渠道、发布说明和包完整性标记。

调整后新增管理端发布入口：

```text
POST /api/admin/capabilities/maclaw-apps/{id}/publish
```

发布规则：

- 只有 `approved` 状态的 MaClaw App 可以 publish；`pending_review` 会返回 `MACLAW_APP_NOT_APPROVED`；已发布的能力会返回 `MACLAW_APP_ALREADY_PUBLISHED`。
- publish 前会再次复核 current version manifest 的 ready 状态，沿用 submit/approve 阶段的动态 UI、结果契约、测试证据、依赖验证、workflow contract 和审批实例证据门禁。
- 发布后 capability 和 current version 状态同步更新为 `published`。
- metadata 写入 `published_at`、`published_by`、`release_channel`、`release_notes`，并生成 `package_signature`：包含 schema、sha256 digest、package_sha256、version_key、signed_at、signed_by。
- App 下载接口只允许 `published`，不再允许仅 `approved` 的 App 包被安装端下载。
- 下载包回灌 `governance.submission` 时保留发布信息和 `package_signature`，安装端和 DataSrv 注册可继续保存同一份发布审计证据。

已补回归测试：

```powershell
go test ./hub/internal/httpapi -count=1 -run "TestCapabilityMaclawApp(Submit|PackageDownload)|TestAdminCapabilityMaclawApp(Review|Publish)" -timeout 180s
```

覆盖：

- submit ready 门禁仍生效；
- approve 前 ready 复核仍生效；
- approved App 可以 publish，发布后 capability/version 都变为 `published`，metadata 包含发布人、发布渠道和包签名；
- pending_review App 不能 publish；
- 仅 approved、未 published 的 App 不能通过 `/api/capabilities/maclaw-apps/{id}/package` 下载；
- published App 下载包保留动态 UI、workflow contract、测试证据、审批实例证据和发布签名。

对应全链路意义：

```text
App Studio 测试并提交
  -> Hub submit ready 审查
  -> pending_review
  -> 管理员 approve 前再次 ready 审查
  -> approved 仅表示审核通过
  -> 管理员 publish 生成发布审计与包签名
  -> published 才能被能力市场下载安装
  -> 安装端拿到发布签名、动态 UI、依赖、workflow 和测试结果证据
```

剩余缺口继续聚焦：

- `package_signature` 目前是 Hub 内部完整性摘要，下一步可接企业私钥/证书做真实签名验证；
- `install_ref` 已做存在性门禁，下一步要接 Hub/SkillMarket 真实解析、下载和失败诊断；
- 继续补跨 DataSrv + Hub + GUI 黄金样例：发布后的 App 从市场下载安装依赖 Skill，发起审批，running 节点同步，最终结果内容/文件回读。
### 推进记录：安装端解析 install_ref 并输出依赖诊断（2026-06-30）

本轮把 Hub 发布侧的 `install_ref` 门禁继续接到 GUI 安装端。此前 Hub 已要求必需依赖 Skill 带 `install_ref`，但 GUI 安装计划只把它当字符串保存；如果引用是 `hub://skills/foo@1.0.0` 或 `enterprise_hub://capabilities/cap-foo@1.0.0`，安装端可能把整段 URI 当下载 id 传给安装器，导致下载安装到时失败且诊断不清楚。

调整后，`PlanMaclawAppInstall` 会解析依赖的 `install_ref`，并在每个 dependency 上输出稳定诊断字段：

```text
install_ref_kind
install_ref_target
install_ref_version
install_ref_status
install_ref_message
```

解析规则：

- `hub://skills/{skill}@{version}` / `skillhub://...`：解析为 SkillHub/Hub skill 目标，安装时传入 `{skill}`；
- `skillmarket://skills/{skill}@{version}`：解析为 SkillMarket skill 目标；
- `enterprise_hub://capabilities/{capability}@{version}`：解析为企业 Hub capability 目标，安装时传入 `{capability}`；
- GitHub 依赖仍保留原始 JSON `install_ref` 传给 GitHub 安装器，但诊断中提取 repo/raw_url 作为 target；
- 裸 id/capability ref 保持兼容；
- `enterprise_hub` 和 `github` 来源缺少必需 `install_ref` 会在计划阶段直接标为 `missing/blocked`；
- dependency source 与 install_ref scheme 不匹配时标为 `invalid/blocked`。

`InstallMaclawAppDependencies` 调用安装器时改为使用解析后的 `install_ref_target`，但保留原始 `install_ref` 进入计划、安装证据和 DataSrv 注册，兼顾真实安装和审计可追溯。

已补回归测试：

```powershell
go test ./gui -count=1 -vet=off -run "TestInstallMaclawAppDependencies|TestPlanMaclawAppInstallBlocksEnterpriseDependencyWithoutInstallRef|TestMaclawAppInstallSkillSourceNormalizesHubAndMarket" -timeout 240s
```

覆盖：

- `hub://skills/uri-workflow@2.1.0` 解析为 target `uri-workflow`，安装器收到解析后的 skill id；
- `enterprise_hub://capabilities/cap-enterprise-workflow@1.4.0` 解析为 target `cap-enterprise-workflow`，企业 Hub 安装器收到 capability id；
- GitHub JSON install_ref 继续原样传给 GitHub 安装器；
- enterprise_hub 依赖缺 install_ref 时，安装计划直接显示 `install_ref_status=missing`、`Action=blocked`，并设置缺失/阻断标志。

对应全链路意义：

```text
Hub publish 要求必需依赖带 install_ref
  -> 安装端 PlanMaclawAppInstall 解析 install_ref
  -> GUI 显示可读依赖诊断和阻断原因
  -> InstallMaclawAppDependencies 用解析后的 target 调用 SkillHub/SkillMarket/EnterpriseHub/GitHub 安装器
  -> 原始 install_ref 保留到安装证据/DataSrv，用于审计和二次恢复
```

剩余缺口继续聚焦：

- 目前完成了 install_ref 结构化解析与安装目标传递；下一步要补真实 Hub/SkillMarket 查询失败诊断，例如 capability 不存在、版本不满足、权限/策略拒绝、下载包校验失败；
- 继续把发布后的 MaClaw App 下载、依赖 Skill 安装、DataSrv 注册、审批发起和结果回读串成跨 DataSrv + Hub + GUI 的黄金样例。
### 推进记录：依赖安装失败结构化诊断（2026-06-30）

本轮继续把 `install_ref` 链路从“能解析、能传给安装器”推进到“失败时能解释”。此前 `InstallMaclawAppDependencies` 调用 SkillHub / SkillMarket / EnterpriseHub / GitHub 安装器失败时，只把 `err.Error()` 放进 dependency message；前端和 DataSrv 只能看到一段文本，无法区分 capability 不存在、策略拒绝、权限拒绝、版本不匹配、包完整性失败或下载网络问题。

调整后，每个依赖安装失败会写入结构化字段：

```text
install_error_code
install_error_stage
install_error_detail
```

当前分类规则：

- `not_found`：404、not found、no such 等；
- `policy_rejected`：企业策略、enterprise-only、non-enterprise install 阻断；
- `access_denied`：401/403、unauthorized、forbidden、permission denied；
- `version_mismatch`：版本约束、版本不满足、version mismatch；
- `package_integrity_failed`：checksum、signature、sha256、integrity 失败；
- `download_failed`：download、timeout、connection、network；
- `security_scan_failed`：scan/admit/security 阶段失败；
- `install_failed`：其它未分类安装失败；
- `missing_install_ref` / `invalid_install_ref`：安装引用本身缺失或非法。

`install_error_stage` 会按来源归类为：

```text
skillhub_download
skillmarket_download
enterprise_hub_install
github_import
install_ref
dependency_install
```

这样 GUI 安装面板可以展示“依赖不存在 / 企业策略拒绝 / 包校验失败”等稳定状态，DataSrv 安装记录也能保存同一份失败事实，后续自动修复或重试不再需要解析自然语言错误。

已补回归测试：

```powershell
go test ./gui -count=1 -vet=off -run "TestInstallMaclawAppDependencies|TestPlanMaclawAppInstallBlocksEnterpriseDependencyWithoutInstallRef|TestMaclawAppInstallSkillSourceNormalizesHubAndMarket" -timeout 240s
```

覆盖：

- SkillMarket 返回 404 时标记 `not_found / skillmarket_download`；
- EnterpriseHub 被市场策略阻断时标记 `policy_rejected / enterprise_hub_install`；
- SkillHub 包 checksum mismatch 时标记 `package_integrity_failed / skillhub_download`；
- 原有 Hub/SkillMarket/EnterpriseHub/GitHub 安装路径仍通过。

对应全链路意义：

```text
Hub 发布包声明依赖和 install_ref
  -> GUI 安装端解析 install_ref
  -> 调用对应来源安装器
  -> 安装失败时生成稳定错误码和阶段
  -> GUI / DataSrv / 安装审计保留同一份可诊断失败事实
  -> 用户或自动化流程可按错误类型执行修复动作
```

剩余缺口继续聚焦：

- 需要把这些结构化失败诊断更完整地展示到前端市场安装 UI：依赖列表、失败原因、重试/修复入口；
- 继续补真实 Hub/SkillMarket 查询前置诊断，例如安装前 HEAD/metadata 校验 capability 是否存在、版本是否满足；
- 继续串跨 DataSrv + Hub + GUI 黄金样例：published App 下载、依赖安装、DataSrv 注册、审批发起、running 节点同步、最终结果/文件回读。
### 推进记录：前端展示依赖安装诊断（2026-06-30）
本轮把 GUI 后端已经输出的结构化依赖诊断接到 MaClaw App 市场安装界面和安装结果界面。此前 `PlanMaclawAppInstall` / `InstallMaclawAppDependencies` 已经能输出 `install_ref_*` 和 `install_error_*` 字段，但前端解析类型没有承接这些字段，安装面板只能看到“不可用/未安装”一类泛化状态，无法判断是引用缺失、目标 capability、策略拒绝、下载失败还是包完整性问题。

调整后，`AppsPage` 的安装依赖模型会解析并保留：
```text
install_ref_kind
install_ref_target
install_ref_version
install_ref_status
install_ref_message
install_error_code
install_error_stage
install_error_detail
```

市场安装依赖列表现在会在紧凑依赖项里显示：
- 原始 `install_ref`，用于审计和回溯；
- 解析后的 `target:{id}@{version}`，用于确认真实安装目标；
- `ref-kind` / `ref-status`，用于区分 Hub、SkillMarket、EnterpriseHub、GitHub 等来源；
- 异常时展示 `code:{install_error_code}`、`stage:{install_error_stage}` 和错误明细。

界面仍保持企业工具风格：正常依赖只是一行紧凑标签；失败/阻断依赖才追加小号诊断行，避免把能力市场安装结果变成大段日志。

已补验证：
```powershell
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "blocks one-click market install when a required dependency is unavailable"
npm.cmd run build
```

验证结果：
- 单测覆盖 EnterpriseHub 依赖被策略阻断时，前端显示原始 `install_ref`、解析 target、`policy_rejected`、`enterprise_hub_install` 和详细原因；
- 前端 build 通过，包含编码检查、UI guard、TypeScript 与 Vite 打包；
- 额外执行 `npm.cmd test -- ... -t "dependency"` 时，命中过一组既有中英文 title 不一致的用例，失败点发生在点击 App Studio 前，和本轮依赖诊断展示无关，后续可单独统一这些测试入口。

对应全链路意义：
```text
Hub 发布包声明依赖和 install_ref
  -> GUI 安装计划解析 install_ref
  -> 安装器失败时写入 install_error_code/stage/detail
  -> 前端市场安装/安装结果展示结构化诊断
  -> 用户、管理员和后续自动修复流程可以按稳定错误码处理依赖问题
```

剩余缺口继续聚焦：
- 补真实 Hub/SkillMarket 安装前查询诊断：capability 是否存在、版本是否满足、权限是否允许、包签名/校验是否可用；
- 继续把 DataSrv 安装登记、审批实例数据、运行节点状态和结果反馈串成跨 DataSrv + Hub + GUI 的黄金样例；
- 统一 App Studio / 市场安装测试里的中英文入口选择，减少批量过滤测试的非业务失败噪音。
### 推进记录：依赖链路前端测试入口跨语言稳定化（2026-06-30）

本轮收尾上一轮前端依赖诊断展示后的测试入口问题。`AppsPage.test.tsx` 中 dependency 相关用例原先混用了英文按钮、tab、placeholder、证据标签和依赖摘要断言；在 `zh-Hans` 渲染环境下，虽然产品界面已经正确显示依赖验证、安装诊断、版本快照和测试证据，但测试会因为中文文案或 DOM 文本节点拆分而失败。

调整后：

- App Studio、市场添加、安装、关闭、加到面板等入口使用中英文兼容查询；
- 依赖摘要 `Skill dependencies / 依赖 Skill`、`Blocking deps / 阻断依赖` 改为按归一化 `textContent` 匹配，避免中文标签和值被拆成多个节点时误判；
- 运行态依赖阻断提示兼容 `Blocked DataSrv Approval is unavailable...` 和中文 `暂不可用` 文案；
- 版本快照、测试证据、界面布局、结果契约、依赖验证等安装证据标签全部按当前中英文 UI 文案断言。

验证结果：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "dependency"
npm.cmd run build
```

上述两项均已通过。这样依赖安装诊断不仅在界面上可见，也有稳定的跨语言测试覆盖，可继续作为后续 Hub/SkillMarket 安装前真实查询诊断和 DataSrv+Hub+GUI 黄金样例的前置保障。
### 推进记录：依赖安装前预检诊断字段打通（2026-06-30）

本轮继续推进“安装 MaClaw App 时先检查并安装依赖 Skill”的真实诊断链路。此前安装端已经能解析 `install_ref_*`，安装失败后也能输出 `install_error_*`，但 Plan 阶段缺少一个稳定位置表达“安装前已经知道的事实”：引用缺失、引用非法、声明版本和 install_ref 版本冲突、已安装版本不满足，或目标已解析但仍等待远端安装器/Hub 查询。

调整后，GUI 后端 `maclawAppInstallPlanDependency` 新增预检字段：

```json
{
  "preflight_status": "ready | pending | blocked",
  "preflight_code": "installed_ready | target_resolved | name_based_lookup | missing_install_ref | invalid_install_ref | version_mismatch | not_checked",
  "preflight_stage": "local_dependency_scan | install_ref | dependency_preflight",
  "preflight_message": "..."
}
```

当前已落地的权威预检规则：

- 必需依赖缺少或非法 `install_ref` 时，Plan 阶段直接标记 `preflight_status=blocked`；
- 已安装依赖版本和声明版本不一致时，标记 `version_mismatch`；
- `install_ref` 带版本且与依赖声明版本不一致时，必需依赖直接阻断安装计划；
- `install_ref` 可解析但还没有做远端 HEAD/metadata 查询时，标记 `pending / target_resolved`，明确后续由安装器或 Hub 查询完成；
- name-based SkillHub/SkillMarket 安装保持 `pending / name_based_lookup`，不伪装成已远端验证。

前端 `AppsPage` 已解析并展示这些字段，在依赖诊断行显示 `preflight:*`、`preflight-code:*`、`preflight-stage:*` 和预检消息；正常 ready 依赖不额外扩展日志，仍保持紧凑企业工作台风格。

验证结果：

```powershell
go test ./gui -count=1 -vet=off -run "TestInstallMaclawAppDependencies|TestPlanMaclawAppInstallBlocksEnterpriseDependencyWithoutInstallRef|TestPlanMaclawAppInstallBlocksInstallRefVersionMismatch|TestMaclawAppInstallSkillSourceNormalizesHubAndMarket" -timeout 240s
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "blocks one-click market install when a required dependency is unavailable"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "dependency"
npm.cmd run build
```

上述命令均已通过。下一步继续把 `pending / target_resolved` 接到真实 Hub/SkillMarket 元数据查询：目标 capability/skill 是否存在、版本是否满足、权限/策略是否允许、包签名和 checksum 是否可用。
### 推进记录：企业 Hub 依赖 capability 真实预检（2026-06-30）

本轮把上一轮的 `preflight pending / target_resolved` 往真实远端诊断推进了一步：GUI 后端 `PlanMaclawAppInstall` 在完成本地依赖扫描和 `install_ref` 解析后，会对 `enterprise_hub://capabilities/{id}@{version}` 这类依赖调用企业 Hub capability detail 接口进行安装前预检。

已完成：

- 对 `enterprise_hub` 且 `install_ref_target` 已解析的依赖调用 `GET /api/capabilities/{id}`；
- 查询成功时校验 capability 类型必须是 `skill`；
- 校验 capability 状态必须是 `published / approved / active / available` 之一；
- 校验远端 `current_version_key` 与依赖声明版本或 install_ref 版本一致；
- 查询失败时结构化为 `not_found / access_denied / policy_rejected / version_mismatch / remote_preflight_failed`；
- 必需依赖一旦远端预检 blocked，安装计划立即标记 `Action=blocked`、`Health=missing`，并进入 `HasMissingRequired / HasBlockingDependency`；
- 未配置企业 Hub 时不伪装成已验证，保持 `preflight_status=pending`、`preflight_code=remote_preflight_unavailable`；
- 前端诊断行保持紧凑：`preflight_status=ready` 不额外展示标签，只有 pending/blocked 或 install error 才显示诊断。

新增/更新测试覆盖：

- `TestPlanMaclawAppInstallPreflightsEnterpriseHubCapability` 使用 `httptest.Server` 覆盖真实 HTTP 路径：ready capability、版本不满足、404 not found；
- `TestInstallMaclawAppDependenciesParsesInstallRefs` 调整为未配置企业 Hub 时显示 `remote_preflight_unavailable`；
- 前端阻断安装用例继续断言 `preflight:blocked / preflight-code / preflight-stage` 可见；
- dependency 组测试确认安装证据和运行态依赖展示仍稳定。

验证结果：

```powershell
go test ./gui -count=1 -vet=off -run "TestInstallMaclawAppDependencies|TestPlanMaclawAppInstallBlocksEnterpriseDependencyWithoutInstallRef|TestPlanMaclawAppInstallBlocksInstallRefVersionMismatch|TestPlanMaclawAppInstallPreflightsEnterpriseHubCapability|TestMaclawAppInstallSkillSourceNormalizesHubAndMarket" -timeout 240s
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "blocks one-click market install when a required dependency is unavailable"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "dependency"
npm.cmd run build
```

上述命令均已通过。下一步继续把公共 SkillMarket/HubCenter 的 skill 元数据预检接入同一套 `preflight_*` 字段，并进一步检查包签名/checksum 可用性。
### 推进记录：公共 SkillMarket 依赖元数据预检（2026-06-30）

本轮把上一阶段企业 Hub capability 预检继续扩展到公共 SkillMarket 依赖。`PlanMaclawAppInstall` 现在会对显式声明为 `market` / `skillmarket` 来源，或 `install_ref` kind 为 `market` / `skillmarket` 的依赖执行安装前元数据查询；普通 `hub` / `local` / `skillhub` 依赖仍保持本地 SkillHub 解析语义，不会被误判为公共市场缺失。

核心规则：

- 通过已配置的 HubCenter / SkillMarket 搜索接口做 metadata preflight；
- 使用 dependency id、name、install_ref target / install_ref 做精确匹配；
- 命中且版本满足时写入 `preflight_status=ready`、`preflight_code=skillmarket_target_ready`、`preflight_stage=skillmarket_preflight`；
- 命中但版本不满足时写入 `blocked / version_mismatch`，必需依赖同步阻断安装计划；
- 未命中时写入 `blocked / not_found`，必需依赖同步阻断安装计划；
- HubCenter 未显式配置或远端查询不可用时保持 `pending / remote_preflight_unavailable`，不伪装成已验证通过。

这使 MaClaw App 安装计划从“依赖字符串能解析”进一步推进到“安装前能判断公共市场目标是否存在、版本是否可用”。前端已有的依赖诊断行会展示同一套 `preflight_*` 字段，用户能在安装前看到 not found、version mismatch 或远端预检不可用，而不必等下载失败后再读日志。

本轮验证：

```powershell
go test ./gui -count=1 -vet=off -run "TestInstallMaclawAppDependenciesParsesInstallRefs|TestPlanMaclawAppInstallPreflightsSkillMarketDependency" -timeout 180s
go test ./gui -count=1 -vet=off -run "TestInstallMaclawAppDependencies|TestPlanMaclawAppInstallBlocksEnterpriseDependencyWithoutInstallRef|TestPlanMaclawAppInstallBlocksInstallRefVersionMismatch|TestPlanMaclawAppInstallPreflightsEnterpriseHubCapability|TestPlanMaclawAppInstallPreflightsSkillMarketDependency|TestMaclawAppInstallSkillSourceNormalizesHubAndMarket" -timeout 240s
cd gui/frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "blocks one-click market install when a required dependency is unavailable"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "dependency"
npm.cmd run build
```

剩余缺口继续聚焦：

- SkillMarket / Hub 预检还需要检查包签名、checksum、下载地址和发布审计是否可用；
- 仍需把 published App 从市场下载安装到依赖 Skill 安装、DataSrv 注册、审批发起、running 节点同步、最终结果内容/文件回读串成跨 DataSrv + Hub + GUI 的黄金样例；
- App Studio 仍需继续产品化传统软件式可视化设计器，包括拖拽布局、控件属性面板、运行态预览和测试失败回跳。
### 推进记录：依赖包完整性元数据预检（2026-06-30）

本轮继续推进安装前依赖预检，把“目标是否存在、版本是否满足”扩展到“安装包完整性元数据是否可用”。MaClaw App 安装计划的 dependency snapshot 现在会保留包级证据字段，供 GUI、安装记录和后续 DataSrv 审计复用：

- `package_sha256` / `package_checksum`
- `package_signature`
- `package_download_url`
- `integrity_status`
- `integrity_code`
- `integrity_stage`
- `integrity_message`

公共 SkillMarket 依赖会从 search result 中读取 `package_sha256`、`sha256`、`package_signature`、`signature`、`package_download_url`、`download_url` 等字段；企业 Hub 依赖会从 capability 顶层字段或 `metadata_json` 中读取 `package_sha256`、`package_signature`、`package_download_url`。这些字段会在 `PlanMaclawAppInstall` 的 `skillmarket_preflight` / `enterprise_hub_preflight` 阶段写入 dependency。

完整性元数据状态规则：

- 有 checksum 和 signature：`integrity_status=ready`，`integrity_code=package_integrity_metadata_ready`；
- 有 checksum 但缺 signature：`integrity_status=partial`，`integrity_code=signature_unavailable`；
- 缺 checksum：`integrity_status=missing`，`integrity_code=checksum_unavailable`。

这一步不把完整性元数据状态和目标可用性 `preflight_status` 混在一起：依赖可以同时是 `preflight_status=ready` 且 `integrity_status=partial`。这样用户能看到“这个 Skill 存在且版本满足，但发布包还缺签名元数据”，后续安装器也能在下载/验签阶段继续使用同一份结构化上下文。

前端市场安装和安装记录面板已经解析并显示完整性诊断标签，例如 `integrity:ready`、`integrity-code:signature_unavailable`、`sha:*`、`signature:available`、`download:available`。正常信息仍保持紧凑标签形式，失败/缺失才进入依赖诊断行。

本轮验证：

```powershell
go test ./gui -count=1 -vet=off -run "TestPlanMaclawAppInstallPreflightsEnterpriseHubCapability|TestPlanMaclawAppInstallPreflightsSkillMarketDependency" -timeout 180s
go test ./gui -count=1 -vet=off -run "TestInstallMaclawAppDependencies|TestPlanMaclawAppInstallBlocksEnterpriseDependencyWithoutInstallRef|TestPlanMaclawAppInstallBlocksInstallRefVersionMismatch|TestPlanMaclawAppInstallPreflightsEnterpriseHubCapability|TestPlanMaclawAppInstallPreflightsSkillMarketDependency|TestMaclawAppInstallSkillSourceNormalizesHubAndMarket" -timeout 240s
cd gui/frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "shows recent app install records in the market pane"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "dependency"
npm.cmd run build
```

剩余缺口继续聚焦：

- 当前完成的是“安装前元数据可用性检查”，还需要在真实下载后执行 checksum 比对、signature 验签，并把失败写成 `package_integrity_failed`；
- Hub/SkillMarket 服务端还需要更稳定地在搜索/详情/下载接口都暴露同一套 package integrity metadata；
- 继续补跨 DataSrv + Hub + GUI 的黄金样例：published App 下载、依赖 Skill 安装和验签、DataSrv 注册、审批发起、running 节点同步、最终结果内容/文件回读。
### 推进记录：依赖 Skill 下载后 checksum 校验（2026-06-30）

本轮把上一阶段“安装前完整性元数据可见”推进到真实下载路径的一部分：MaClaw App 依赖安装在走 HubCenter / SkillMarket / SkillHub 下载 Skill JSON 包时，会把 dependency snapshot 中的 `package_sha256` / `package_checksum` 作为 expected SHA-256 传入下载 helper，并在解码、扫描、注册 Skill 之前对原始下载 bytes 做 checksum 比对。

实现边界：

- Wails 对外的 `InstallMixedSkill(source, id, installRef)` 保持原签名，普通手动安装不受影响；
- 新增内部 `installMixedSkillWithIntegrity(source, id, installRef, expectedPackageSHA256)`，供 `InstallMaclawAppDependencies` 使用；
- HubCenter 下载 helper 新增 `downloadSkillJSONFromHubCenterToDirWithIntegrity`，expected SHA 为空时保持旧行为；
- expected SHA 支持标准 64 位 hex 和 `sha256:` 前缀；非标准短摘要不执行强校验，避免把展示用短指纹误当真实 checksum；
- checksum mismatch 会在下载后、解码/注册前失败，错误文本包含 `package integrity checksum mismatch`，安装计划会通过既有分类进入 `install_error_code=package_integrity_failed`。

这一步让 MaClaw App 从“能看到包完整性元数据”进一步变成“安装依赖 Skill 时能实际阻止 checksum 不一致的包进入本机 Skill 注册”。它覆盖公共 SkillMarket / SkillHub 的 HubCenter 下载路径；企业 Hub capability 的包下载/验签路径仍需继续接入同一套机制。

本轮验证：

```powershell
go test ./gui -count=1 -vet=off -run "TestDownloadSkillJSONFromHubCenterVerifiesPackageSHA256|TestDownloadSkillJSONFromHubCenter_FailsOver|TestInstallMaclawAppDependenciesClassifiesInstallFailures" -timeout 180s
go test ./gui -count=1 -vet=off -run "TestInstallMaclawAppDependencies|TestPlanMaclawAppInstallBlocksEnterpriseDependencyWithoutInstallRef|TestPlanMaclawAppInstallBlocksInstallRefVersionMismatch|TestPlanMaclawAppInstallPreflightsEnterpriseHubCapability|TestPlanMaclawAppInstallPreflightsSkillMarketDependency|TestMaclawAppInstallSkillSourceNormalizesHubAndMarket|TestDownloadSkillJSONFromHubCenterVerifiesPackageSHA256" -timeout 240s
cd gui/frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "dependency"
npm.cmd run build
```

剩余缺口继续聚焦：

- 企业 Hub `InstallHubCapability` 下载路径还要接入 package checksum / signature 验证；
- signature 目前仍只是元数据可见，还需要做真实验签和证书/公钥策略；
- HubCenter / SkillMarket 服务端需要保证 search、detail、download 三个接口的 checksum/signature 口径一致；
- 继续补跨 DataSrv + Hub + GUI 的黄金样例：published App 下载、依赖 Skill 安装和 checksum/签名验证、DataSrv 注册、审批发起、running 节点同步、最终结果内容/文件回读。
### 推进记录：企业 Hub Skill capability 下载 checksum 校验（2026-06-30）
本轮把依赖完整性校验继续接到企业 Hub capability 的真实安装路径。此前 `PlanMaclawAppInstall` 已能从企业 Hub capability detail 中预检 `package_sha256` / `package_checksum`，公共 SkillMarket / SkillHub 下载路径也已经能在解码和注册前做 checksum 比对；但用户从企业 Hub 能力市场直接安装 Skill capability 时，`InstallHubCapability -> ensureHubSkillInstalled -> installManagedHubSkill` 仍没有把 capability 包指纹传入下载器。

已完成：
- `ensureHubSkillInstalled` 从 capability 顶层字段和 `metadata_json` 中读取 `package_sha256` / `package_checksum`，并传给 managed skill 安装器；
- `installManagedHubSkill` 新增内部 expected checksum 参数，并调用 `SkillHubClient.InstallToDirWithIntegrity`；
- `SkillHubClient.InstallToDirWithIntegrity` 会先读取原始下载 bytes，再执行 SHA-256 校验，最后才解码、扫描和注册 Skill；
- 显式企业 Hub URL 与当前 `RemoteHubURL` 一致时，下载 bytes 请求会携带 `RemoteViewerToken` Bearer 认证，避免 capability detail 走认证而实际包下载丢认证；
- 新增入口级回归：`TestInstallHubCapabilityVerifiesEnterpriseSkillPackageSHA256`，模拟企业 Hub capability 带错误 `package_sha256`，确认安装返回 checksum mismatch、`ManagedInstalled=0`，且 Skill 不会进入本地注册表。

本轮验证：
```powershell
go test ./gui -count=1 -vet=off -run "TestInstallHubCapabilityVerifiesEnterpriseSkillPackageSHA256" -timeout 180s
go test ./gui -count=1 -vet=off -run "TestInstallMaclawAppDependencies|TestPlanMaclawAppInstallPreflightsEnterpriseHubCapability|TestPlanMaclawAppInstallPreflightsSkillMarketDependency|TestDownloadSkillJSONFromHubCenterVerifiesPackageSHA256|TestDownloadSkillJSONFromHubCenter_FailsOver|TestInstallMaclawAppDependenciesClassifiesInstallFailures|TestInstallHubCapabilityVerifiesEnterpriseSkillPackageSHA256" -timeout 240s
```

剩余缺口继续聚焦：
- `package_signature` 仍只是元数据可见和传递，尚未做真实签名验签、证书/公钥策略和失败诊断；
- ClawHub / GitHub 等 external origin_source 的 Skill 安装路径还未接入同等级 checksum/signature 校验；
- 最终仍要补一条跨 DataSrv + Hub + GUI 的黄金样例：Studio 制作审批型 App、测试、提交、Hub 审核发布、安装 App 与 workflow Skill 依赖、发起审批、running 节点同步、审批完成、结果内容/文件回读。
### 推进记录：审批发起后 DataSrv 最终结果回读归一（2026-06-30）
本轮继续推进黄金样例的后端事实链路，把 `StartMaclawAppApprovalWorkflow` 从“只证明能创建 pending 审批”补强到“发起后可从 DataSrv 回读最终审批结果包”。新增/扩展的回归会先通过运行态入口提交企业审批型 App 的表单和业务 payload，创建 DataSrv record approval；随后模拟 DataSrv 返回同一 `approval_id / workflow_instance_id` 的 approved 最终实例，验证全局审批中心按 handled lane 读取时能看到最终节点路径、审批结果、业务状态、文本输出和文件产物。

已完成：
- 扩展 `TestStartMaclawAppApprovalWorkflowCreatesDataSrvApproval`：同一用例覆盖发起节点数据提交、DataSrv 审批创建、本地 pending 实例保存、DataSrv 最终 approved 结果回读；
- 最终回读断言保留 `approval-start-1 / wf-start-1` 身份、`expense.intake -> finance.manager_review -> expense.result_pack` 节点路径、`approval_result=approved`、`business_status=finance_approved`、内容输出和 `approval-start.pdf` 文件产物；
- 修正 DataSrv lane 推断：当请求 `handled` lane 且远端审批状态已进入 approved/rejected/cancelled/timeout 等终态时，即使当前用户也是申请人，也优先把返回实例归一为 `handled`，避免最终实例混在 `my_requests` 快照语义里。

本轮验证：
```powershell
go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowCreatesDataSrvApproval" -timeout 180s
go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowCreatesDataSrvApproval|TestRecordMaclawAppInstallRegistersApprovalAppWithDataSrv|TestListMaclawAppApprovalInstancesAll|TestMaclawAppApprovalInstancesPersistAndFilter" -timeout 240s
go test ./gui -count=1 -vet=off -run "TestInstallMaclawAppDependencies|TestPlanMaclawAppInstallPreflightsEnterpriseHubCapability|TestPlanMaclawAppInstallPreflightsSkillMarketDependency|TestDownloadSkillJSONFromHubCenterVerifiesPackageSHA256|TestDownloadSkillJSONFromHubCenter_FailsOver|TestInstallMaclawAppDependenciesClassifiesInstallFailures|TestInstallHubCapabilityVerifiesEnterpriseSkillPackageSHA256|TestStartMaclawAppApprovalWorkflowCreatesDataSrvApproval|TestListMaclawAppApprovalInstancesAll" -timeout 240s
```

剩余缺口继续聚焦：
- 真实 workflow skill runner 的 running/complete 事件仍需进一步接入同一条黄金样例，减少测试里人工模拟 DataSrv 最终状态的比例；
- Hub 审核发布后的真实下载包、依赖 Skill 安装、DataSrv 注册、审批发起和最终结果回读仍需串成跨 DataSrv + Hub + GUI 的可重复 E2E；
- signature 验签仍待落地到公共 SkillMarket、企业 Hub capability 和 external origin_source 下载路径。
### 推进记录：依赖 Skill 下载后 Ed25519 签名验签（2026-06-30）
本轮把 `package_signature` 从“元数据可见”推进到“支持真实验签”的安装安全链路。此前 MaClaw App 依赖安装和企业 Hub capability 安装已经能在下载后校验 checksum，但 signature 仍只展示在依赖诊断里；现在下载器会识别明确支持的 Ed25519 签名格式，并在解码、扫描、注册 Skill 之前完成验签。

已完成：
- 新增 `verifyDownloadedSkillPackageSignature`，支持两种可验证格式：
  - `ed25519:<base64-public-key>:<base64-signature>`；
  - JSON 字符串对象：`{"algorithm":"ed25519","public_key_base64":"...","signature_base64":"...","package_sha256":"..."}`。
- 若签名对象内带 `package_sha256`，会先复用 checksum 校验，确保签名声明的包指纹与实际下载 bytes 一致；
- 未知/旧格式签名（例如当前部分市场元数据里的展示型 `sig-ready`）保持兼容，不强行失败；明确声明 Ed25519 但格式错误、签名长度错误或验签失败时阻断安装；
- 公共 HubCenter / SkillMarket / SkillHub 下载路径：`downloadSkillJSONFromHubCenterToDirWithIntegrity` 现在同时校验 checksum 和 signature；
- 企业 Hub Skill capability 路径：`ensureHubSkillInstalled -> installManagedHubSkill -> SkillHubClient.InstallToDirWithIntegrity` 现在会把 capability 的 `package_signature` / metadata `signature` 传入下载器并验签；
- 保持 Wails 外部 `InstallMixedSkill(source,id,installRef)` 签名不变，内部依赖安装才携带包完整性上下文。

新增/扩展测试：
- `TestVerifyDownloadedSkillPackageSignatureEd25519`：真实生成 Ed25519 keypair，验证正确签名通过、篡改 payload 失败、旧展示型签名保持兼容；
- `TestDownloadSkillJSONFromHubCenterVerifiesPackageSignature`：覆盖 HubCenter 下载 Skill JSON 后，验签失败会在 decode/register 前阻断；
- `TestInstallHubCapabilityVerifiesEnterpriseSkillPackageSignature`：覆盖企业 Hub capability 安装时，错误 signature 会阻断安装且不会注册本地 Skill；
- 既有 checksum 测试继续覆盖 `package_integrity_failed` 分类基础。

本轮验证：
```powershell
go test ./gui -count=1 -vet=off -run "TestVerifyDownloadedSkillPackageSignatureEd25519|TestDownloadSkillJSONFromHubCenterVerifiesPackageSignature|TestDownloadSkillJSONFromHubCenterVerifiesPackageSHA256" -timeout 180s
go test ./gui -count=1 -vet=off -run "TestInstallHubCapabilityVerifiesEnterpriseSkillPackageSignature|TestInstallHubCapabilityVerifiesEnterpriseSkillPackageSHA256|TestDownloadSkillJSONFromHubCenterVerifiesPackageSignature" -timeout 180s
go test ./gui -count=1 -vet=off -run "TestInstallMaclawAppDependencies|TestPlanMaclawAppInstallPreflightsEnterpriseHubCapability|TestPlanMaclawAppInstallPreflightsSkillMarketDependency|TestDownloadSkillJSONFromHubCenterVerifiesPackageSHA256|TestDownloadSkillJSONFromHubCenterVerifiesPackageSignature|TestInstallMaclawAppDependenciesClassifiesInstallFailures|TestInstallHubCapabilityVerifiesEnterpriseSkillPackageSHA256|TestInstallHubCapabilityVerifiesEnterpriseSkillPackageSignature" -timeout 240s
```

剩余缺口继续聚焦：
- Ed25519 公钥目前随签名元数据携带，仍需补企业信任策略：Hub/租户/能力市场的可信公钥、证书或 key fingerprint 白名单；
- Hub/SkillMarket 服务端需要统一在 search/detail/download 元数据中输出可验签的 Ed25519 signature，而不是展示型摘要；
- ClawHub / GitHub external origin_source 安装路径还未接入同等级 checksum/signature 校验；
- 最终仍要补跨 DataSrv + Hub + GUI 的黄金 E2E，把发布包签名、依赖 Skill 验签、DataSrv 注册、审批运行和结果回读串成同一条验收链。
### 推进记录：依赖 Skill 签名公钥指纹信任基础（2026-06-30）

本轮继续补强依赖 Skill 下载后的签名安全链路。在已有 Ed25519 package signature 验签基础上，下载 helper 现在具备公钥 fingerprint 语义，为后续企业 Hub / 租户 / 能力市场的可信公钥策略打基础。

已完成：
- `package_signature` JSON 对象支持 `public_key_fingerprint` / `key_fingerprint` / `fingerprint` 字段；
- 验签时会计算 Ed25519 public key 的 `sha256:<hex>` fingerprint；
- 如果签名元数据声明了 public key fingerprint，实际公钥 fingerprint 不一致会阻断安装；
- 新增内部 `verifyDownloadedSkillPackageSignatureWithTrustedFingerprints`，当调用方提供 trusted fingerprint 白名单时，未命中的签名公钥会被拒绝；
- fingerprint 归一化同时兼容 `sha256:<hex>` 与裸 64 位 hex，便于 Hub 元数据、配置文件和 UI 展示复用；
- 保持现有安装路径兼容：未配置 trusted fingerprint 时，仍按 Ed25519 签名有效性执行校验，不影响当前市场包安装。

新增/补强测试：

```powershell
go test ./gui -count=1 -vet=off -run "TestVerifyDownloadedSkillPackageSignatureEd25519|TestVerifyDownloadedSkillPackageSignatureChecksPublicKeyFingerprint|TestVerifyDownloadedSkillPackageSignatureRequiresTrustedFingerprintWhenConfigured|TestDownloadSkillJSONFromHubCenterVerifiesPackageSignature" -timeout 180s
go test ./gui -count=1 -vet=off -run "TestInstallMaclawAppDependencies|TestPlanMaclawAppInstallPreflightsEnterpriseHubCapability|TestPlanMaclawAppInstallPreflightsSkillMarketDependency|TestDownloadSkillJSONFromHubCenterVerifiesPackageSHA256|TestDownloadSkillJSONFromHubCenterVerifiesPackageSignature|TestInstallMaclawAppDependenciesClassifiesInstallFailures|TestInstallHubCapabilityVerifiesEnterpriseSkillPackageSHA256|TestInstallHubCapabilityVerifiesEnterpriseSkillPackageSignature|TestVerifyDownloadedSkillPackageSignatureChecksPublicKeyFingerprint|TestVerifyDownloadedSkillPackageSignatureRequiresTrustedFingerprintWhenConfigured" -timeout 240s
```

上述命令均已通过。

下一步仍需继续：
- 把 trusted public key / fingerprint 白名单接入配置、企业 Hub metadata 或租户策略，而不只是 helper 内部能力；
- Hub/SkillMarket 服务端需要在 search/detail/download 元数据中稳定输出可验签 Ed25519 signature、package checksum 和 public key fingerprint；
- ClawHub / GitHub external origin_source 安装路径仍需接入同等级 checksum/signature/fingerprint 校验；
- 继续补跨 DataSrv + Hub + GUI 的黄金 E2E，把 published App 下载、依赖 Skill 安装验签、DataSrv 注册、审批运行和结果回读串成同一条验收链。
### 推进记录：可信 Skill 包签名指纹配置接入（2026-06-30）

本轮把上一阶段的 helper 级 fingerprint 校验继续接入到实际安装策略。MaClaw App 安装依赖 Skill 时，现在可以通过本地配置声明可信签名公钥 fingerprint，并在下载后验签阶段强制执行。

已完成：
- `corelib.AppConfig` 新增 `trusted_skill_package_key_fingerprints` 配置字段；
- 公共 HubCenter / SkillMarket 下载路径 `downloadSkillJSONFromHubCenterToDirWithIntegrity` 会读取配置中的 trusted fingerprint 白名单；
- 企业 Hub Skill capability 下载路径 `SkillHubClient.InstallToDirWithIntegrity` 也会读取同一配置白名单；
- 未配置白名单时保持原兼容行为：有 Ed25519 signature 则验签，未知展示型 signature 仍兼容忽略；
- 配置白名单后，签名公钥 fingerprint 不在白名单中的 Skill 包会在 decode、scan、register 前被拒绝；
- 白名单支持 `sha256:<hex>` 和裸 64 位 hex 两种写法。

新增/补强测试：

```powershell
go test ./gui -count=1 -vet=off -run "TestDownloadSkillJSONFromHubCenterRequiresConfiguredTrustedPackageKeyFingerprint|TestSkillHubClientInstallToDirRequiresConfiguredTrustedPackageKeyFingerprint|TestVerifyDownloadedSkillPackageSignatureRequiresTrustedFingerprintWhenConfigured" -timeout 180s
go test ./gui -count=1 -vet=off -run "TestInstallMaclawAppDependencies|TestPlanMaclawAppInstallPreflightsEnterpriseHubCapability|TestPlanMaclawAppInstallPreflightsSkillMarketDependency|TestDownloadSkillJSONFromHubCenterVerifiesPackageSHA256|TestDownloadSkillJSONFromHubCenterVerifiesPackageSignature|TestDownloadSkillJSONFromHubCenterRequiresConfiguredTrustedPackageKeyFingerprint|TestSkillHubClientInstallToDirRequiresConfiguredTrustedPackageKeyFingerprint|TestInstallMaclawAppDependenciesClassifiesInstallFailures|TestInstallHubCapabilityVerifiesEnterpriseSkillPackageSHA256|TestInstallHubCapabilityVerifiesEnterpriseSkillPackageSignature|TestVerifyDownloadedSkillPackageSignatureChecksPublicKeyFingerprint|TestVerifyDownloadedSkillPackageSignatureRequiresTrustedFingerprintWhenConfigured" -timeout 240s
```

上述命令均已通过。

下一步仍需继续：
- 企业 Hub / HubCenter 应在租户策略、能力详情或市场元数据中下发可信 key fingerprint，并由 GUI 自动合并到安装策略；
- Hub/SkillMarket 服务端仍需稳定生成真实 Ed25519 package signature，而不是只提供展示型 `sig-ready`；
- external origin_source（ClawHub / GitHub）还未具备同等级 checksum/signature/fingerprint 校验；
- 继续把安全安装链路并入跨 DataSrv + Hub + GUI 的黄金 E2E。
### 推进记录：Hub MaClaw App 发布包 Ed25519 签名元数据（2026-06-30）

本轮继续补强 Hub 能力市场的发布包完整性元数据。此前 MaClaw App 发布时写入的 `package_signature` 只是 `algorithm=sha256` 的展示型 digest，客户端无法按 Ed25519 签名语义验证；现在 Hub 发布 MaClaw App 时会生成可验证的 Ed25519 package signature 对象，并在下载包顶层和 app governance submission 中同时暴露。

已完成：
- `AdminCapabilityMaclawAppPublishHandler` 生成 `maclaw.app.package_signature.v1` Ed25519 签名对象；
- 签名对象包含 `algorithm=ed25519`、`payload`、`public_key_base64`、`public_key_fingerprint`、`signature_base64`、`package_sha256`、`version_key`、`signed_at`、`signed_by`；
- `public_key_fingerprint` 使用 `sha256:<hex>`，与 GUI 侧 `trusted_skill_package_key_fingerprints` 归一化策略保持一致；
- `CapabilityMaclawAppPackageHandler` 下载包顶层新增 `package_signature`，同时仍通过 governance submission 保留同一签名元数据；
- Hub 回归测试会实际用 Ed25519 公钥验证发布签名，不再只检查字段存在。

已验证命令：

```powershell
go test ./hub/internal/httpapi -count=1 -run "TestCapabilityMaclawApp(Submit|PackageDownload)|TestAdminCapabilityMaclawApp(Review|Publish)" -timeout 180s
go test ./gui -count=1 -vet=off -run "TestDownloadSkillJSONFromHubCenterRequiresConfiguredTrustedPackageKeyFingerprint|TestSkillHubClientInstallToDirRequiresConfiguredTrustedPackageKeyFingerprint|TestDownloadSkillJSONFromHubCenterVerifiesPackageSignature|TestInstallHubCapabilityVerifiesEnterpriseSkillPackageSignature|TestVerifyDownloadedSkillPackageSignatureChecksPublicKeyFingerprint|TestVerifyDownloadedSkillPackageSignatureRequiresTrustedFingerprintWhenConfigured" -timeout 180s
```

上述命令均已通过。

下一步仍需继续：
- 当前 Hub 侧签名 key 仍是内置稳定 seed，后续要升级为企业配置/KMS/租户密钥，并支持 key rotation；
- GUI 仍需把 Hub 下发的可信 public key fingerprint 自动合并进安装策略，而不是只依赖本地手动配置；
- App package 本身的 Ed25519 签名和 Skill 依赖包签名语义需要在 golden E2E 中统一验收；
- external origin_source（ClawHub / GitHub）仍需接入 checksum/signature/fingerprint 校验。
### 推进记录：GUI 自动信任 Hub 下发的 MaClaw App 签名指纹（2026-06-30）

本轮把 Hub 发布包 Ed25519 签名元数据继续接到 GUI 安装策略。此前 GUI 已支持本地 `trusted_skill_package_key_fingerprints` 白名单，但仍需要用户手动配置；现在从企业 Hub 下载 MaClaw App package 时，GUI 会校验 Hub 顶层 `package_signature`，并把签名公钥 fingerprint 自动合并进本地可信 Skill 包签名策略。

已完成：
- `DownloadMaclawAppPackageFromHub` 解码 package 后会识别顶层 `package_signature`；
- 当签名对象为 `algorithm=ed25519` 时，GUI 会验证 payload、公钥长度、签名长度、Ed25519 signature、公钥 fingerprint 和 `package_sha256` 一致性；
- 验签失败会在解析/安装 MaClaw App 前中断下载，避免坏签名 package 进入安装流程；
- 验签通过后，GUI 会将 `public_key_fingerprint` 归一化并追加到 `trusted_skill_package_key_fingerprints`；
- 下载结果新增 `trusted_package_key_fingerprints`，供前端/日志展示本次从 Hub 信任链导入的签名公钥；
- 后续依赖 Skill 安装仍走已有 `trusted_skill_package_key_fingerprints` 策略，因此 Hub package 签名指纹可以继续约束依赖 Skill package signature。

新增/补强测试：

```powershell
go test ./gui -count=1 -vet=off -run "TestDownloadMaclawAppPackageFromHubTrustsSignedPackageFingerprint|TestDownloadMaclawAppPackageFromHubRejectsInvalidPackageSignature" -timeout 180s
go test ./gui -count=1 -vet=off -run "TestDownloadMaclawAppPackageFromHubTrustsSignedPackageFingerprint|TestDownloadMaclawAppPackageFromHubRejectsInvalidPackageSignature|TestInstallSelectedMaclawAppPackageFromHubFiltersPackageApps|TestInstallSelectedMaclawAppPackageFromHubInstallsDepsAndRegistersDataSrv|TestDownloadSkillJSONFromHubCenterRequiresConfiguredTrustedPackageKeyFingerprint|TestSkillHubClientInstallToDirRequiresConfiguredTrustedPackageKeyFingerprint|TestInstallHubCapabilityVerifiesEnterpriseSkillPackageSignature" -timeout 240s
```

上述命令均已通过。

下一步仍需继续：
- 将 Hub 签名 key 从内置 seed 升级为企业配置/KMS/租户密钥，并支持 key rotation；
- 在 golden E2E 中验证：Hub 发布 App package signature -> GUI 自动导入 fingerprint -> 依赖 Skill package signature 使用同一策略验签 -> DataSrv 注册/审批运行/结果回读；
- external origin_source（ClawHub / GitHub）仍需接入 checksum/signature/fingerprint 校验。
### 推进记录：Hub App 签名信任到依赖 Skill 验签安装迷你 E2E（2026-06-30）

本轮把前两步安全链路串成一条可重复的 GUI 端安装闭环。此前已分别具备：Hub 发布 MaClaw App package 的 Ed25519 签名元数据、GUI 下载 App package 时自动导入签名公钥 fingerprint、依赖 Skill 下载后按 trusted fingerprint 验签。现在新增迷你 E2E，验证这些能力能在一次企业 Hub 安装流程中连续生效。

已完成：
- 新增 `TestInstallSelectedMaclawAppPackageFromHubUsesPackageSignatureTrustForDependencySkill`；
- 测试模拟企业 Hub 返回带顶层 `package_signature` 的 MaClaw App package；
- GUI 下载 App package 后验证 Ed25519 签名，并把 public key fingerprint 写入 `trusted_skill_package_key_fingerprints`；
- 同一安装流程继续解析 App 的企业 Hub Skill 依赖，查询 capability detail，读取 `package_sha256` 和 `package_signature`；
- 依赖 Skill JSON 包使用同一公钥签名，安装器在 decode/scan/register 前完成 checksum、Ed25519 signature、trusted fingerprint 校验；
- 安装成功后确认 dependency plan 为 ready/installed，并保留 package checksum/signature 元数据；
- 测试服务端同时覆盖 capability inventory 回报，避免只验证下载而绕过真实安装尾部审计动作。

已验证命令：

```powershell
go test ./gui -count=1 -vet=off -run "TestInstallSelectedMaclawAppPackageFromHubUsesPackageSignatureTrustForDependencySkill" -timeout 180s
go test ./gui -count=1 -vet=off -run "TestInstallSelectedMaclawAppPackageFromHubUsesPackageSignatureTrustForDependencySkill|TestDownloadMaclawAppPackageFromHubTrustsSignedPackageFingerprint|TestDownloadMaclawAppPackageFromHubRejectsInvalidPackageSignature|TestInstallSelectedMaclawAppPackageFromHubFiltersPackageApps|TestInstallSelectedMaclawAppPackageFromHubInstallsDepsAndRegistersDataSrv|TestDownloadSkillJSONFromHubCenterRequiresConfiguredTrustedPackageKeyFingerprint|TestSkillHubClientInstallToDirRequiresConfiguredTrustedPackageKeyFingerprint|TestInstallHubCapabilityVerifiesEnterpriseSkillPackageSignature" -timeout 240s
go test ./hub/internal/httpapi -count=1 -run "TestCapabilityMaclawApp(Submit|PackageDownload)|TestAdminCapabilityMaclawApp(Review|Publish)" -timeout 180s
```

上述命令均已通过。

对应全链路意义：

```text
Hub 发布 MaClaw App package
  -> package_signature 暴露 Ed25519 公钥、fingerprint 和签名
  -> GUI 下载 App package 并验签
  -> GUI 自动导入 trusted Skill package key fingerprint
  -> 安装 App 所依赖的企业 Hub Skill capability
  -> Skill 包 checksum + Ed25519 signature + trusted fingerprint 校验通过
  -> Skill 注册、inventory 回报、MaClaw App install plan ready
```

下一步仍需继续：
- 把这条迷你安全 E2E 扩展到完整 golden E2E：DataSrv 注册、审批发起、running 节点同步、审批完成、结果内容/文件回读；
- Hub 签名 key 仍需企业配置/KMS/租户密钥和 key rotation；
- external origin_source（ClawHub / GitHub）仍需接入同等级完整性校验。

### 推进记录：签名 Hub 审批型 App 安装到 DataSrv 运行回读黄金验收（2026-06-30）
本轮把上一阶段的“Hub App 签名信任 -> 依赖 Skill 验签安装”继续向企业审批型 App 主链路推进，新增 GUI 后端黄金验收 `TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv`。这条测试不再只验证工具型 App 或孤立依赖安装，而是把签名安装、DataSrv 注册、审批发起、running 节点同步和最终结果回读放进同一个场景。

覆盖内容：

- 企业 Hub 下发带 `maclaw.app.package_signature.v1` Ed25519 签名的企业审批型 App package；
- GUI 下载 App package 时校验签名，并把 Hub package 公钥 fingerprint 写入 `trusted_skill_package_key_fingerprints`；
- App 声明 `expense-super-skill` 和 `expense-workflow` 两个依赖，安装端按 `install_ref` 查询企业 Hub capability；
- 两个依赖 Skill 下载后均校验 `package_sha256`、Ed25519 签名和 trusted fingerprint，再进入本地注册；
- 安装企业审批型 App 后立即向 DataSrv `app-installations` 注册，保留动态 UI、workflow contract、result contract、dependency verification 和测试证据；
- 通过 `StartMaclawAppApprovalWorkflow` 发起审批，DataSrv 创建 record approval，并在本地保存 pending/running 实例；
- running 阶段通过 `SyncMaclawAppApprovalInstanceToDataSrv` 写入 `/progress`，同步当前节点、节点路径、业务状态、结果状态和中间输出；
- final 阶段通过同一同步入口写入审批结果，再由 `ListMaclawAppApprovalInstancesAll("handled")` 从 DataSrv 回读同一实例；
- 最终回读断言保留审批结果、业务状态、业务记录、文本内容输出和文件 artifact。

验证命令：

```powershell
go test ./gui -count=1 -vet=off -run "TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv" -timeout 180s
go test ./gui -count=1 -vet=off -run "TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv|TestInstallSelectedMaclawAppPackageFromHubUsesPackageSignatureTrustForDependencySkill|TestDownloadMaclawAppPackageFromHubTrustsSignedPackageFingerprint|TestDownloadMaclawAppPackageFromHubRejectsInvalidPackageSignature|TestInstallSelectedMaclawAppPackageFromHubInstallsDepsAndRegistersDataSrv|TestInstallSelectedMaclawApprovalAppFromHubPreservesApprovalEvidence|TestStartMaclawAppApprovalWorkflowCreatesDataSrvApproval|TestRecordMaclawAppInstallRegistersApprovalAppWithDataSrv|TestSyncMaclawAppApprovalInstanceToDataSrvUpdatesPendingProgress" -timeout 240s
go test ./hub/internal/httpapi -count=1 -run "TestCapabilityMaclawApp(Submit|PackageDownload)|TestAdminCapabilityMaclawApp(Review|Publish)" -timeout 180s
```

当前仍未宣称整套 MaClaw App 完成。下一步继续推进两类缺口：一是把该黄金验收中的手工 DataSrv/mock workflow 进一步替换为真实 workflow skill runner 的 initiate/running/complete/result 节点；二是把 Hub signing key 从内置稳定 seed 升级为企业配置/KMS/租户密钥，并补 key rotation、撤销和信任策略。

### 推进记录：审批发起接入真实 workflow skill runner 结果同步（2026-06-30）
本轮继续收缩上一段黄金验收中“workflow 运行结果仍由测试手工 mock”的缺口。`StartMaclawAppApprovalWorkflow` 现在保持默认兼容行为：不显式要求时仍只创建 DataSrv approval 并保存 pending 实例；当调用方传入 `run_workflow_skill=true` 时，会调用已安装的 workflow skill runner，并把 skill 输出的标准 JSON 结果归一化成审批实例，再复用 `SyncMaclawAppApprovalInstanceToDataSrv` 写回 DataSrv。

新增/调整能力：

- `MaclawAppApprovalWorkflowStartInput` 新增 `run_workflow_skill` 和 `workflow_run_args`；
- workflow skill 运行参数会携带 app、dataset/object role、record、approval id、instance id、workflow skill/version、当前节点、申请人/审批人、表单数据、业务载荷和当前 result payload；
- skill runner 可从标准输出 JSON 或 capture 变量中读取 `workflow_result` / `approval_result`；
- 支持 skill 输出顶层结果或 `approval_instance` / `approvalInstance` / `instance` 嵌套结果；
- 归一化字段覆盖审批状态、lane、workflow instance id、审批 id、当前节点、节点路径、决策 id、业务状态、结果状态、result payload、outputs 和 artifacts；
- pending 结果继续走 DataSrv `/progress`，approved/rejected 结果走 DataSrv `/review`，attention 仍保持 view-only 语义。

新增 GUI 后端测试：

- `TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult`
  - 注册一个真实本地 workflow skill；
  - skill 通过 bash step 输出 `workflow_result=<json>`，由 SkillExecutor capture 变量；
  - `StartMaclawAppApprovalWorkflow(... run_workflow_skill=true)` 创建 DataSrv approval 后执行 workflow skill；
  - workflow skill 返回 approved/result node/content/file artifact；
  - 后端把结果转成审批实例并向 DataSrv `/review` 写回最终决策。

验证命令：

```powershell
go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult" -timeout 180s
go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult|TestStartMaclawAppApprovalWorkflowCreatesDataSrvApproval|TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv|TestRecordMaclawAppInstallRegistersApprovalAppWithDataSrv|TestSyncMaclawAppApprovalInstanceToDataSrvUpdatesPendingProgress|TestSyncMaclawAppApprovalInstanceToDataSrv" -timeout 240s
go test ./hub/internal/httpapi -count=1 -run "TestCapabilityMaclawApp(Submit|PackageDownload)|TestAdminCapabilityMaclawApp(Review|Publish)" -timeout 180s
```

这一步把“发起节点 UI 数据提交 -> 审批 workflow skill 运行 -> 结果节点写回 DataSrv”的后端桥接打通为可选真实 runner 路径。下一步仍需继续推进前端运行态提交按钮对 `run_workflow_skill` 的显式选择/策略配置，以及把 workflow skill 输出合同写入 App Studio 测试协议和 Hub 审核说明，避免不同 workflow skill 自定义输出格式导致运行态无法归一。

### 推进记录：GUI 审批型 App 运行入口切到后端 workflow runner（2026-06-30）

本轮继续补齐“App GUI -> 后端审批发起 -> workflow skill runner -> 审批实例/结果证据”的前端运行链路。此前后端 `StartMaclawAppApprovalWorkflow` 已支持 `run_workflow_skill=true`，但 GUI 审批型 App 的提交按钮仍先直接调用 `RunNLSkillAsync`，再由前端拼装审批实例。现在审批型 App 的正常提交路径改为只调用后端 `StartMaclawAppApprovalWorkflow`，并把原本传给 workflow skill 的完整运行上下文放入 `workflow_run_args`，由后端统一创建审批实例、运行 workflow skill、同步 DataSrv、返回最终实例。

已完成：
- `AppsPage.tsx` 审批运行分支改为传入 `run_workflow_skill: true` 和 `workflow_run_args`；
- `workflow_run_args` 保留 app id/name/kind、审批事件、workflow skill/version、object role、dataset/blueprint、当前节点、workflow mapping、status mapping、业务实体/动作/备注、DataSrv 偏好动作/视图和提示词；
- GUI 使用后端返回的 `workflow_run.instance` 作为优先实例来源，回填审批实例列表、结果包、运行历史和测试证据；
- 后端 runner 返回失败时，GUI 不再本地伪造审批实例，而是显示错误并记录失败运行历史；
- 前端测试已迁移并通过：

```powershell
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "records and completes an approval instance"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "uses approvalBindings|passes workflow node mapping|workflow skill launch fails"
```

本轮验证意义：
- 审批型 App 的 GUI 发起节点已经开始符合“数据录入 + 后端审批 workflow skill 运行 + 审批实例数据管理 + 结果反馈”的职责边界；
- `approvalBindings` 和 `workflow` 节点映射不再只进入前端异步 skill run，而是进入后端 runner 的标准上下文；
- GUI 层可以直接消费后端返回的审批结果包，包括 result payload、outputs、artifacts 和 approval instance evidence。

仍需继续：
- 迁移“发布包测试证据”相关前端用例：`publishes approval app evidence from an actual workflow run` 仍停留在旧的 `RunNLSkillAsync + GetNLSkillRunStatus` 断言，需要改为以后端 `workflow_run.instance` 作为 App Studio 测试证据来源；
- 重新设计“运行中进度”前端测试：后端 runner 当前是同步桥接，pending/progress 语义应来自后端返回的 pending 实例或后续事件流，而不是前端轮询 `GetNLSkillRunStatus`；
- 将 workflow skill 输出契约写入 App Studio 测试协议和 Hub 审核项，明确 `workflow_result` / `approval_instance` / `outputs` / `artifacts` 的结构和必填字段；
- 完成 AppsPage 全量相关 Vitest 回归后，再跑 GUI 后端和 Hub 目标回归。
### 推进记录：App Studio 发布证据接入后端 workflow_run.instance（2026-06-30）

本轮继续收敛 GUI 审批型 App 的制作、测试、上传 Hub 证据链。上一轮已把运行按钮切到 `StartMaclawAppApprovalWorkflow(... run_workflow_skill=true)`，但“实际 workflow run 证据进入发布包”的前端测试仍以旧的 `RunNLSkillAsync + GetNLSkillRunStatus` 为事实来源。本轮把该证据来源改为后端 runner 返回的 `workflow_run.instance`，使 App Studio 测试证据、运行历史和 Hub 提交包 governance 使用同一份审批实例结果。

已完成：
- `publishes approval app evidence from an actual workflow run` 测试改为 mock `StartMaclawAppApprovalWorkflow` 返回完整 `workflow_run.instance`；
- 发布证据断言覆盖后端 runner 入参：`run_workflow_skill=true`、`workflow_run_args`、approval binding、workflow skill/version、dataset/object role、workflow mapping；
- 运行历史继续沉淀 `dependencyVerification`、`approvalInstance`、`resultPayload`、`outputs`、`artifacts` 和 `resultCoverage`；
- App Studio 发布包中的 `governance.testEvidence.approvalInstance` 已能从后端审批实例结果生成，覆盖审批结果、业务状态、业务记录、文档 artifact 和通知/内容输出；
- 确认审批型 App 正常发布测试路径不再依赖前端直接 `RunNLSkillAsync('approval-run-workflow', ...)`。

已验证命令：

```powershell
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "publishes approval app evidence from an actual workflow run"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "records and completes an approval instance|publishes approval app evidence from an actual workflow run|uses approvalBindings|passes workflow node mapping|workflow skill launch fails"
```

对应全链路意义：
- App Studio “测试后上传 Hub 能力市场”的证据来源已经和真实运行入口一致；
- 审批型 App 的测试证据不再是前端轮询 skill run 后拼出来的局部结果，而是后端审批实例管理返回的完整结果包；
- 后续 Hub 审核可以围绕同一份 `approvalInstance/resultPayload/outputs/artifacts/resultCoverage` 做契约校验。

仍需继续：
- 重新设计运行中 progress 的 GUI 语义：同步 runner 下可先显示后端返回的 pending/final 实例，后续如需要流式进度，应由后端事件/实例刷新驱动；
- 把 workflow skill 输出契约固化为 App Studio 测试协议字段，并在 Hub 审核阶段校验；
- 跑更大范围 AppsPage Vitest，清理仍停留在旧异步 skill run 模型的测试；
- 继续补 Hub key KMS/轮换/吊销、外部 origin_source 校验和 DataSrv 更完整端到端回归。
### 推进记录：审批 workflow skill 输出契约进入 App Studio 与 Hub 审核（2026-06-30）

本轮把“workflow skill 输出必须能被审批型 App 归一化”的约定，从文档和测试习惯推进到 App Studio 生成契约与 Hub 服务端审核。此前 workflow contract 只声明 workflow skill id、object role、输入字段和 approved/rejected/attention 决策值，Hub 审核也只检查 workflow skill id 与 object role，无法阻断“不返回审批实例/结果包/产物”的 workflow skill。本轮新增标准输出字段契约：`workflow_result`、`approval_instance`、`outputs`、`artifacts`。

已完成：
- `AppWorkflowContract` 新增 `requiredOutputs`；
- `normalizeAppWorkflowContract` 兼容读取 `requiredOutputs` / `required_outputs` / `outputContract` / `output_contract`，默认写入 `workflow_result`、`approval_instance`、`outputs`、`artifacts`；
- `appWorkflowContractForManifest` 为企业审批型 App 自动生成上述标准输出字段；
- App 运行/发布治理元数据新增 `workflow_decision_outputs` 和 `workflow_required_outputs`，让 DataSrv/Hub 能保留输出契约；
- App Studio 发布包测试补充断言：`governance.workflowContract.requiredOutputs` 必须包含四个标准字段；
- Hub `enterpriseMaclawAppReadyReviewIssues` 新增 workflow contract 输出字段审核，缺少任一标准输出时阻断提交/审批；
- Hub 测试包 ready fixture 已更新，新增“approval app workflow contract missing required outputs”拒绝用例。

已验证命令：

```powershell
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "publishes approval app evidence from an actual workflow run"
go test ./hub/internal/httpapi -count=1 -run "TestCapabilityMaclawAppSubmitRejectsUnreadyPackage" -timeout 180s
go test ./hub/internal/httpapi -count=1 -run "TestCapabilityMaclawApp(Submit|PackageDownload)|TestAdminCapabilityMaclawApp(Review|Publish)" -timeout 180s
go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult|TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv" -timeout 240s
```

全链路意义：
- App Studio 设计出的企业审批型 App 不再只声明“跑哪个 workflow skill”，而是声明 workflow skill 必须返回哪些结果包字段；
- Hub 能在能力市场提交/审核阶段阻断缺少审批实例、结果内容或文件产物输出契约的审批型 App；
- 后端 runner、GUI 运行历史、发布包 governance、Hub 审核开始围绕同一个 `approval_instance/resultPayload/outputs/artifacts` 结果模型闭合。

仍需继续：
- 把 workflow 输出契约进一步写入可视化 App Studio 测试协议编辑/预览区域，而不仅是自动生成和发布审核；
- 重新设计运行中 progress：同步 runner 下先以后端 pending/final 实例为准，后续如要中间节点，应由后端事件/实例刷新驱动；
- 修复/迁移仍基于旧前端 `RunNLSkillAsync + GetNLSkillRunStatus` 模型的历史前端测试；
- 继续推进 Hub signing key KMS/轮换/吊销、外部 origin_source 校验、DataSrv 更完整安装/运行/回读回归。
### 推进记录：运行中审批进度改为后端 workflow_run.instance 驱动（2026-06-30）

本轮继续收口 GUI 审批型 App 运行入口切到后端 workflow runner 之后留下的 progress 语义。此前运行中测试仍沿用旧模型：前端直接启动 workflow skill，再轮询 `GetNLSkillRunStatus`，并由前端本地记录/同步审批实例进度。现在运行中状态改为以后端 `StartMaclawAppApprovalWorkflow(... run_workflow_skill=true)` 返回的 `workflow_run.instance` 为事实来源。

已完成：
- `AppsPage.tsx` 对后端返回的 pending/running/submitted/requires_input/in_progress 实例保持运行中状态，不再把它当作最终完成历史写入 run history；
- GUI 优先消费 `workflow_run.instance` 中的 result、business_status、result_status、outputs、artifacts、current_node/current_node_ids 等字段，运行中时保持应用 tile 为 `running`；
- 前端运行中进度测试迁移为 mock 后端 runner 返回 pending `workflow_run.instance`，断言：
  - `StartMaclawAppApprovalWorkflow` 使用 `run_workflow_skill=true`；
  - 前端不再调用 `RunNLSkillAsync('expense-approval-workflow', ...)`；
  - 前端不再调用 `GetNLSkillRunStatus`；
  - 前端不再直接调用本地 `RecordMaclawAppApprovalInstance` / `SyncMaclawAppApprovalInstanceToDataSrv` 伪造进度；
  - UI 保持 running，且展示后端实例返回的进度文本；
  - 本地完成历史保持为空，避免把未完成审批误记为 done。

已验证命令：

```powershell
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "updates approval instance progress from the backend workflow runner instance"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "records and completes an approval instance|updates approval instance progress from the backend workflow runner instance|publishes approval app evidence from an actual workflow run|uses approvalBindings|passes workflow node mapping|workflow skill launch fails"
```

全链路意义：
- 审批型 App 的发起节点 UI、后端 workflow runner、审批实例数据管理、App Studio 运行证据和 Hub governance 继续向同一份 `workflow_run.instance` 收敛；
- running progress 不再由前端旧 skill run 轮询模型临时拼装，避免 GUI、DataSrv 和 Hub 看到不同版本的审批实例事实；
- 后续如果需要流式/多节点实时进度，应由后端事件、DataSrv progress 接口或审批实例刷新驱动，而不是恢复前端直接轮询 workflow skill。

仍需继续：
- 把真实 workflow skill runner 的中间节点事件接入 DataSrv `/progress`，形成非 mock 的 running 节点推进链路；
- 在 App Studio 可视化测试协议里显式展示 workflow required outputs，而不只是在生成 contract 和 Hub 审核中隐式存在；
- 继续补跨 DataSrv + Hub + GUI 黄金样例：制作审批型 App、测试、上传、审核发布、下载安装依赖、发起审批、running 节点同步、最终内容/文件结果回读。
### 推进记录：App Studio 可视化测试协议展示 workflow 必需输出契约（2026-06-30）

本轮继续补齐“所有 MaClaw App 界面动态生成、用户可视化设计、测试后上传 Hub”的制作环节。此前审批 workflow 的 `requiredOutputs` 已经会自动生成并进入 Hub 审核，但 App Studio 的测试协议设计器里看不到这组约束，用户只能在 manifest JSON 或审核失败后才知道 workflow skill 必须返回哪些结果包字段。

已完成：
- `AppTestProtocol` 新增审批 workflow 契约字段：
  - `workflowRequiredInputs`
  - `workflowDecisionOutputs`
  - `workflowRequiredOutputs`
- 企业审批型 App 的默认测试协议自动写入：
  - required inputs: `record_ref` / `applicant` / `business_payload`
  - decision outputs: `approved` / `rejected` / `attention`
  - required result fields: `workflow_result` / `approval_instance` / `outputs` / `artifacts`
- App Studio `TestProtocolDesigner` 新增紧凑的“审批 workflow 输出合同”区，用户可直接看到并编辑 Hub 审核必检字段；界面保持企业工作台风格，使用低噪声边框、输入框和 chips，不做表单以外的新视觉语言。
- 创建态和管理编辑态都传入 App kind，只有企业审批型或已有 workflow 契约字段时才显示该区域，工具型/企业普通应用不会出现审批概念噪音。
- 保存 manifest 时，测试协议会保留用户调整后的 `workflowRequiredOutputs`，用于后续测试、发布包证据和 Hub 审核说明。

已验证命令：

```powershell
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "saves visual approval workflow node mappings into created manifests"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "saves visual approval workflow node mappings into created manifests|records and completes an approval instance|updates approval instance progress from the backend workflow runner instance|publishes approval app evidence from an actual workflow run|uses approvalBindings|passes workflow node mapping|workflow skill launch fails"
```

完整 `AppsPage.test.tsx` 当前仍有一批旧中英文断言漂移失败，本轮未展开清理；本轮目标链路的 7 个用例已通过。

全链路意义：
- App Studio 不再只在后台生成 workflow 输出合同，而是把审批型 App 的运行契约变成用户可见、可调、可保存的设计内容；
- 用户制作审批型 App 时，可以在测试协议里提前看到 Hub 审核会要求 workflow skill 返回 `workflow_result / approval_instance / outputs / artifacts`；
- “设计 -> 测试 -> 发布 -> Hub 审核”的契约口径更一致，减少上传后才发现 workflow skill 输出不满足审批实例/结果包要求的情况。

仍需继续：
- 将这组 workflow 契约字段进一步接入 App Studio 测试运行结果面板，测试失败时能直接跳到缺失字段；
- 清理完整 `AppsPage.test.tsx` 中遗留的中英文断言漂移，恢复大范围前端回归信号；
- 继续推进非 mock 的 workflow runner 中间节点事件 -> DataSrv `/progress` -> GUI 审批实例刷新链路。
### 推进记录：App 面板回归信号继续收口（2026-06-30）

本轮继续从 MaClaw App 全链路的前端回归信号入手，优先恢复 App Studio / Hub 市场 / 安装证据相关测试，避免后续 DataSrv、Hub 和运行期闭环开发缺少稳定防线。

已完成：

- 修复 `AppsPage.test.tsx` 中旧英文/乱码中文断言，统一改为双语匹配，覆盖当前中文 GUI 文案；
- 恢复 SkillMarket / Hub Skill picker 搜索簇：搜索按钮、市场空态、SkillMarket 结果与已安装 Skill 分区顺序；
- 恢复发布门禁证据簇：结果契约覆盖、审批实例证据、导入运行证据 stale 检查；
- 恢复 Hub 审核队列安装簇：已审核 App 直接安装、后端 review gate 错误透出、依赖验证、版本快照、测试证据、DataSrv 绑定快照；
- 当前 `AppsPage.test.tsx` 全量信号从上一轮约 43 个失败推进到 31 个失败，已通过 169/200。

已验证：

```powershell
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "hides stale SkillMarket results|shows an empty market state|shows SkillMarket results above|ignores slower SkillMarket"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "requires run evidence to cover|requires approval instance evidence|treats imported run evidence as stale"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "installs an approved Hub app directly|shows detailed Hub app install errors"
```

当前剩余缺口：

- `AppsPage.test.tsx` 仍有 31 个失败，主要集中在 App Studio 可视化设计/保存、运行期按钮文案、管理页编辑、Hub 市场搜索安装、DataSrv 安装候选运行路径；
- 其中大部分仍表现为中文 GUI 文案迁移后的测试断言漂移，但至少有 1 个 DataSrv installed app 候选用例显示 backend workflow runner 调用次数为 0，需要继续判定是测试入口未点到还是产品接线缺口；
- 下一轮优先继续收口 App Studio 设计/管理簇，然后再回到审批工作流运行中状态和 DataSrv 进度同步的实际产品缺口。
### 推进记录：App Studio 布局发布证据回归收口（2026-06-30）

本轮继续从 `maclaw data srv -> maclaw app -> App Studio -> Hub 发布/安装 -> 审批运行` 的全链路角度收口 App 面板回归信号，重点处理动态生成界面和发布包预览之间的证据门禁。

已完成：

- 修复 App Studio 中审批工作流设计器按钮的中英文可访问名断言，确认中文界面下“设计”按钮仍打开当前 Hub 审批工作流设计器。
- 修复 App Studio 应用类型切换断言，覆盖“企业普通应用 / Business app”和“企业审批型 / Approval app”两套界面语言。
- 修复动态布局进入发布包预览的测试路径：布局调整后先写入成功运行证据，再重新挂载发布面板，让发布预览遵守当前“只有通过发布检查的本地应用才进入 package”的门禁。
- 恢复并验证聚焦 App Studio 设计组：`6 passed / 188 skipped`。
- 恢复完整 AppsPage 测试文件覆盖面后运行完整回归：`184 passed / 10 failed`。

当前剩余 10 个 AppsPage 回归缺口主要集中在：

- 发布包 checklist 已经变为更严格的 ready-only package preview，旧测试仍期待未满足门禁的应用包里出现 `governance`。
- 审批型应用运行已迁移到后端 `StartMaclawAppApprovalWorkflow(..., run_workflow_skill=true)`，旧测试仍在多处等待前端直接调用 `RunNLSkillAsync` 或 `RecordMaclawAppApprovalInstance`。
- DataSrv 安装应用运行路径同样应按后端审批工作流入口校验 payload 和实例结果，而不是继续校验旧 skill runner 调用。

下一步优先级：

1. 将剩余审批运行类测试整体改为验证 `StartMaclawAppApprovalWorkflow` 的输入、后端返回实例、运行历史证据和审批实例视图。
2. 将发布包 checklist 测试拆成“未满足门禁显示待补齐”和“补齐运行/依赖证据后进入 package preview”两类。
3. 在上述前端回归稳定后，再补 DataSrv app installation -> Hub approved install -> approval result package 的黄金链路测试。

## 推进记录：审批运行实例契约与 AppsPage 回归收口（2026-06-30）

本轮继续按“MaClaw App = 带 App 数据与依赖的超级 Skill”推进全链路闭环，重点收口企业审批型应用在 GUI 运行态中的审批实例契约。

已完成：

- 默认审批 workflow 运行模拟改为合并 workflow skill 状态与 result payload 中的 `outputs` / `artifacts`，避免附件、嵌套结果包在审批实例完成态丢失。
- 发起态审批实例补齐 `events`，记录提交节点与当前审批节点，支持“我的申请 / 我审批的 / 节点状态”视图恢复。
- 发起态与完成态审批实例同步给 DataSrv 时补齐 `current_node_ids` 与 `workflow_node_ids`，完成态在 workflow skill 返回业务结果节点后同步更新节点数组。
- 完成态 DataSrv 同步优先使用 workflow skill 返回的业务 `record_id`，避免继续用临时审批发起 ID 覆盖最终业务记录。

验证结果：

- `npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "records and completes an approval|workflow node mapping"`：4 passed。
- `npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "shows local apps in the review|records and completes an approval|publishes approval app evidence|approvalBindings as the runtime|workflow node mapping|does not record approval|marks approval workflow failures|installs missing workflow|runs an enterprise app|turns DataSrv installed"`：12 passed。
- `npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx`：194 passed。

剩余推进重点：

- 继续从 Go/DataSrv 侧验证同一审批实例契约，确保真实 `StartMaclawAppApprovalWorkflow`、安装依赖检查、Hub 安装导入证据与前端测试模型一致。
- 继续补齐 App Studio 设计态到运行态的端到端验证，尤其是动态 UI 布局保存、测试证据、上传 Hub、从能力市场安装后恢复运行态的真实闭环。
## 推进记录：Go/DataSrv 审批 workflow 节点路径契约收口（2026-06-30）

本轮在前端 AppsPage 审批运行回归全绿后，继续把同一套审批实例契约落回真实 GUI 后端入口，避免只停留在前端 mock 模型。

已完成：

- `maclawAppApprovalInstance` 与 `MaclawAppApprovalWorkflowStartInput` 增加 `workflow_node_ids`，与现有 `current_node_ids` 形成兼容别名。
- `StartMaclawAppApprovalWorkflow` 发起审批实例时同时保存 `current_node_ids` 与 `workflow_node_ids`，默认取安装包 workflow mapping 的 approval node。
- `runMaclawAppApprovalWorkflowSkill` 调用 workflow skill 时传入 `workflow_node_ids`，让审批 workflow skill 能看到完整节点路径上下文。
- workflow skill 返回 `approval_instance.workflow_node_ids` 时，GUI 后端解析并保留到本地审批实例；没有返回时由 `current_node_ids` 兜底。
- `SyncMaclawAppApprovalInstanceToDataSrv` 写入 DataSrv/MIS Tool payload 时，`workflow_node_ids` 优先使用实例上的 `WorkflowNodeIDs`，再 fallback 到 `CurrentNodeIDs`。
- DataSrv 反查 `RecordApproval` 恢复 GUI 审批实例时，同时填充 `CurrentNodeIDs` 与 `WorkflowNodeIDs`，保证“我的申请 / 我审批的 / 已处理 / 全部”视图拿到同一条节点路径。
- Go 金线测试补充断言：发起态 create approval、完成态 review approval、本地实例、远端最终实例都必须携带 workflow node path。

验证结果：

- `go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult|TestStartMaclawAppApprovalWorkflowCreatesDataSrvApproval|TestMaclawAppApprovalRuntimeContractUsesInstallSnapshot|TestListMaclawAppApprovalInstancesAllLoadsDataSrvLane|TestListMaclawAppApprovalInstancesAllInfersDataSrvLanesForCurrentUser" -timeout 240s`：通过。
- `npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx`：194 passed。

对应全链路意义：

```text
企业审批型 MaClaw App 发起运行
  -> GUI 后端创建本地审批实例并同步 DataSrv
  -> workflow skill 获得 current_node_ids / workflow_node_ids
  -> workflow skill 返回最终审批实例、outputs、artifacts、节点路径
  -> GUI 后端保留结果并再次同步 DataSrv
  -> 前端审批实例工作台与 DataSrv 审批中心读取同一套节点路径事实
```
## 推进记录：Hub 安装审计持久化 DataSrv 登记结果（2026-06-30）

本轮继续推进“能力市场安装 -> Skill 依赖检查/安装 -> DataSrv app installation 登记 -> GUI 最近安装与运行态恢复”的闭环。此前 `RecordMaclawAppInstall` 会在安装返回值里带 `datasrv_registration`，但本地 `app_install_records.json` 的单 App 安装审计没有持久化这项；用户重启后打开 App Studio 最近安装，只能看到依赖和测试证据，看不到 DataSrv 登记成败。

已完成：

- `maclawAppInstallRecord` 增加 `datasrv_registration` 字段，持久化每个 App 的 DataSrv 登记摘要。
- `RecordMaclawAppInstall` 先执行 DataSrv app installation 登记，再把对应 App 的登记结果写入本地安装审计。
- 新增 `maclawAppDataSrvRegistrationForApp`，从多 App 安装总结果中抽取单 App 登记状态；无 role binding 时保留 `eligible_count=0` 与原因，避免安装诊断失明。
- `ListMaclawAppInstalls` 返回安装记录时克隆 `datasrv_registration`，供前端最近安装、修复检查和运行态候选恢复使用。
- Go 测试补充断言：Hub 安装和选择安装后，通过 `ListMaclawAppInstalls` 重新读取的本地审计仍保留 DataSrv 登记成功状态。

验证结果：

- `go test ./gui -count=1 -vet=off -run "TestInstallMaclawAppPackageFromHubDownloadsAndRecordsInstall|TestInstallSelectedMaclawAppPackageFromHubInstallsDepsAndRegistersDataSrv|TestInstallSelectedMaclawApprovalAppFromHubPreservesApprovalEvidence|TestRecordMaclawAppInstallRegistersApprovalAppWithDataSrv" -timeout 240s`：通过。
- `npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "shows DataSrv registration status after installing|keeps dependency verification visible after single market app install|turns DataSrv installed MaClaw apps into addable app candidates|blocks DataSrv installed MaClaw apps at runtime"`：4 passed。
- `npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx`：194 passed。

对应全链路意义：

```text
Hub 能力市场安装 MaClaw App
  -> 安装依赖 Skill
  -> 注册 DataSrv app installation
  -> 本地 install audit 持久化 datasrv_registration
  -> App Studio 最近安装和运行态恢复可在重启后继续看到 DataSrv 登记状态
  -> 用户能判断安装是否真正进入企业数据底座，而不只是本地包安装成功
```
## 推进记录：DataSrv 反向恢复保留安装登记结果（2026-06-30）

上一轮已经让本地 Hub/市场安装审计持久化 `datasrv_registration`。本轮继续补齐反向链路：当 GUI 从 DataSrv `app_installations` 恢复已安装 MaClaw App 候选时，也要把 DataSrv metadata 中的登记结果带回 App 的 `installEvidence`，否则从企业数据底座反查安装状态时仍会丢失“是否已登记”的证据。

已完成：

- `dataSrvInstalledInstallEvidence` 支持读取 `metadata.datasrv_registration` / `metadata.dataSrvRegistration`，以及 `install_evidence.datasrv_registration`。
- `installEvidenceRecordForApp` 改为单 App evidence 上的 `datasrv_registration` 优先，总安装审计结果兜底。
- 扩展 DataSrv installed enterprise normal app 恢复测试：远端 app installation metadata 携带 `datasrv_registration` 后，用户添加到面板的 App `installEvidence.datasrv_registration` 必须保留。

验证结果：

- `npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "restores DataSrv installed enterprise normal app run evidence|turns DataSrv installed MaClaw apps into addable app candidates"`：2 passed。
- `npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "shows DataSrv registration status after installing|keeps dependency verification visible after single market app install|turns DataSrv installed MaClaw apps into addable app candidates|blocks DataSrv installed MaClaw apps at runtime|restores DataSrv installed enterprise normal app run evidence"`：5 passed。
- `npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx`：194 passed。
- `go test ./gui -count=1 -vet=off -run "TestInstallMaclawAppPackageFromHubDownloadsAndRecordsInstall|TestInstallSelectedMaclawAppPackageFromHubInstallsDepsAndRegistersDataSrv|TestInstallSelectedMaclawApprovalAppFromHubPreservesApprovalEvidence|TestRecordMaclawAppInstallRegistersApprovalAppWithDataSrv" -timeout 240s`：通过。

对应全链路意义：

```text
GUI 安装 MaClaw App 并登记 DataSrv
  -> DataSrv app_installations metadata 保存登记/依赖/测试证据
  -> GUI 后续从 DataSrv 反向发现已安装 App
  -> App Studio 候选与添加到面板后的 installEvidence 保留 datasrv_registration
  -> 最近安装、运行态候选、诊断与重启恢复都能看到同一份企业数据底座登记事实
```

### 推进记录：DataSrv 安装登记状态查询补齐（2026-06-30）

本轮继续从“能力市场安装 -> Skill 依赖检查/安装 -> DataSrv app installation 登记 -> GUI/运维反向恢复”的全链路审计角度推进。此前 Hub/GUI 已经会把 `datasrv_registration` 写入安装审计和 DataSrv metadata，但 DataSrv 查询层只能保存，不能按登记结果定位“已同步 / 有失败 / 部分成功”的 App。

已完成：

- `QueryAppInstallationsInput` 增加 DataSrv 登记状态过滤字段：
  - `datasrv_registration_synced`
  - `datasrv_registration_failed`
  - `datasrv_registration_partial`
- `/api/v1/data/app-installations` 增加对应查询参数，并提供 `data_srv_registration_*` 别名。
- SQLite metadata 过滤支持读取：
  - `metadata.datasrv_registration`
  - `metadata.dataSrvRegistration`
  - `metadata.install_evidence.datasrv_registration`
  - `metadata.installEvidence.dataSrvRegistration`
- OpenAPI 暴露新查询参数和 `metadata.datasrv_registration` schema。
- 新增服务层测试，覆盖顶层登记、install evidence 内嵌登记、camelCase 登记，以及 synced/failed/partial 三种过滤。
- 扩展 HTTP 测试，确认 GET 查询能按 DataSrv 登记状态返回安装记录。

验证：

```powershell
go test ./structureddata -count=1 -vet=off -run "TestListAppInstallationsFiltersByDataSrvRegistration|TestListAppInstallationsFiltersByDependencyHealth|TestHTTPServerAppInstallationsOverrideObjectRoleBindings|TestOpenAPISpecIncludesBusinessContractSchemas" -timeout 240s
```

结果：通过。
### 推进记录：GUI/MIS Tool 接入 DataSrv 登记状态过滤（2026-06-30）

本轮继续把上一段 DataSrv `app-installations` 登记状态查询接到 GUI/agent 工具链。DataSrv 已能按 `datasrv_registration_synced` / `datasrv_registration_failed` / `datasrv_registration_partial` 过滤，但 GUI 的 `executeMISDataTool(list_app_installations)` 尚未透传这些参数，导致前端诊断、agent 运维工具和 App Studio 恢复检查仍不能直接列出“DataSrv 登记失败 / 部分成功 / 已同步”的 MaClaw App。

已完成：

- `executeMISDataTool` 的 `list_app_installations` action 增加布尔过滤透传：
  - `datasrv_registration_synced` / `dataSrvRegistrationSynced` / `data_srv_registration_synced`
  - `datasrv_registration_failed` / `dataSrvRegistrationFailed` / `data_srv_registration_failed`
  - `datasrv_registration_partial` / `dataSrvRegistrationPartial` / `data_srv_registration_partial`
- 扩展 GUI 工具层测试，确认混合 snake_case、camelCase、data_srv 前缀入参会统一转成 DataSrv 标准 query：
  - `datasrv_registration_synced=true`
  - `datasrv_registration_failed=false`
  - `datasrv_registration_partial=false`

验证：

```powershell
go test ./gui -count=1 -vet=off -run "TestExecuteMISDataToolListAppInstallationsPassesDependencyFilters|TestExecuteMISDataToolGetAppInstallation" -timeout 240s
```

结果：通过。

### 推进记录：应用面板显示 DataSrv 登记诊断状态（2026-06-30）

本轮继续把 DataSrv app installation 登记结果从“可查询”推进到“用户可见”。此前安装记录的 evidence snapshot 只把 `datasrv_registration` 拼成普通文本，且 `synced_count > 0` 但未完全同步时会被归为失败，用户无法区分“全部失败”和“部分成功、仍需处理”。

已完成：

- App Studio / 应用面板的安装证据快照增加 DataSrv 登记状态判定：
  - `ready`：全部 eligible 绑定已同步。
  - `partial`：已有部分绑定同步成功，但仍有失败或未完成项。
  - `failed`：存在失败且没有成功同步项。
  - `skipped`：无 eligible 绑定或被跳过。
- 新增 `datasrvRegistrationPartial` 中英文文案：
  - `DataSrv bindings partially registered`
  - `DataSrv 绑定部分注册`
- 安装证据快照中的 DataSrv 项增加 `data-state`，并使用低饱和状态样式区分 ready / partial / failed / skipped。
- 扩展最近安装记录测试：当安装审计里 `datasrv_registration` 为 `eligible_count=2, synced_count=1, failed_count=1` 时，应用面板必须显示“部分注册”、比例 `1/2` 和失败原因。

验证：

```powershell
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

结果：194 passed。
### 推进记录：最近安装记录支持 DataSrv 登记审计操作（2026-06-30）

本轮继续把“安装审计里有登记证据”推进到“用户可主动复核 DataSrv 当前事实”。此前最近安装记录只能显示本地持久化的 `datasrv_registration` 快照；如果用户怀疑 DataSrv 端登记状态变化，仍需要离开应用面板手工查 `/api/v1/data/app-installations`。

已完成：

- 最近安装记录在存在 `datasrv_registration` 和 App 身份时显示 `Audit DataSrv` 操作。
- 点击后读取当前 MIS DataSrv 配置，并按 App 安装身份查询 `/api/v1/data/app-installations`。
- 审计摘要显示 DataSrv 当前安装记录数量，以及 ready / partial / failed / skipped 登记状态计数。
- 对同一 App 通过多个安装身份命中的结果做去重，避免 market/local/canonical ID 别名导致同一 DataSrv 记录重复计数。
- 审计结果沿用应用面板的 ready / partial / failed / skipped 低饱和状态样式，保持企业工作台式诊断呈现。

验证：

```powershell
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "shows recent app install records in the market pane"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

结果：定向用例通过；AppsPage 全量 194 passed。
### 推进记录：真实 workflow runner 中间节点写入 DataSrv progress（2026-06-30）

本轮继续收缩“审批 workflow 运行中状态仍依赖手工 mock”的缺口。此前 `StartMaclawAppApprovalWorkflow(... run_workflow_skill=true)` 已能调用真实 workflow skill runner，并把最终 `approval_instance` 写回 DataSrv；但 workflow skill 即使产出运行中节点，也没有标准入口让后端按顺序同步到 DataSrv `/progress`，黄金链路仍需要测试手工调用 `SyncMaclawAppApprovalInstanceToDataSrv` 来模拟 running 阶段。

已完成：

- workflow skill 输出合同新增兼容运行中实例数组：
  - `progress_instances`
  - `progressInstances`
  - `workflow_progress`
  - `workflowProgress`
  - `approval_progress`
  - `approvalProgress`
- 每个 progress item 复用现有 `approval_instance` 解析能力，可直接写实例字段，也可嵌套 `approval_instance` / `instance`。
- `runMaclawAppApprovalWorkflowSkill` 在解析最终实例前，按顺序把 progress 实例同步到 `SyncMaclawAppApprovalInstanceToDataSrv`；pending 状态因此走 DataSrv `/api/v1/data/approvals/{id}/progress`，并保留 workflow node path、current assignee、business/result status、outputs/artifacts/result payload。
- workflow run 返回体新增：
  - `progress_instances`：后端归一化后的运行中审批实例快照。
  - `progress_sync`：每个运行中实例对应的 DataSrv 同步结果。
- 最终 `approval_instance` 的 review 同步保持原行为，旧 workflow skill 只返回最终结果时不受影响。
- 扩展真实 runner 回归：测试 skill 输出 `progress_instances + approval_instance`，断言后端先写 DataSrv `/progress`，再写最终 `/review`。

验证：

```powershell
go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult" -timeout 240s
go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult|TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv|TestStartMaclawAppApprovalWorkflowCreatesDataSrvApproval|TestSyncMaclawAppApprovalInstanceToDataSrvUpdatesPendingProgress" -timeout 240s
```

结果：均通过。

对应全链路意义：

```text
发起节点 UI 数据提交
  -> 后端 StartMaclawAppApprovalWorkflow 创建 DataSrv approval
  -> 真实 workflow skill runner 执行
  -> workflow skill 输出 progress_instances
  -> 后端逐条同步 DataSrv /progress
  -> GUI/DataSrv 审批实例可看到 running 节点、节点路径、当前处理人和运行中输出
  -> workflow skill 输出最终 approval_instance
  -> 后端同步 DataSrv /review 和最终结果包
```

### 推进记录：前端运行证据保留 workflow runner progress 实例（2026-06-30）

本轮继续把上一段“真实 workflow runner 中间节点写入 DataSrv progress”的后端能力接到 MaClaw App 前端运行和发布链路。此前后端 `workflow_run.progress_instances` 已能返回真实审批运行中的节点快照，但 GUI 只把最终 `approval_instance` 写入本地运行历史和发布治理证据，导致 App Studio 测试通过后上传 Hub 时，市场审核只能看到最终结果，看不到“当前节点/中间审批状态/运行中输出”的证据。

已完成：

- 审批型 App 调用 `StartMaclawAppApprovalWorkflow(... run_workflow_skill=true)` 后，前端读取 `workflow_run.progress_instances` / `workflow_run.progressInstances`。
- 当 workflow runner 返回中间审批实例时，GUI 会先把 progress 实例合并进审批实例列表，让“我的申请 / 我审批的 / 当前节点状态”能看到运行中节点。
- 本地运行历史的 `approvalInstance.progressInstances` 保留每个 progress 快照，包括 current node、workflow node path、business/result status、outputs/artifacts 等证据字段。
- App Studio 发布包的 `governance.testEvidence.approvalInstance.progressInstances` 同步携带这些中间节点证据，能力市场审核可以验证审批型 App 不只是返回最终文本，而是真的跑过审批 workflow。
- 普通 tool skill 轮询逻辑中误混入的审批 progress evidence 变量已清理，避免非审批 App 编译/运行时引用越界变量。
- 前端测试 mock 补充真实 workflow runner 的 `progress_instances` 返回，覆盖“经理审批中”节点进入运行历史和发布治理证据。

验证：

```powershell
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "publishes approval app evidence from an actual workflow run"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult|TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv|TestStartMaclawAppApprovalWorkflowCreatesDataSrvApproval|TestSyncMaclawAppApprovalInstanceToDataSrvUpdatesPendingProgress" -timeout 240s
```

结果：定向发布证据用例通过；AppsPage 全量 194 passed；GUI 后端审批 workflow 目标用例通过。

对应全链路意义：

```text
App Studio 测试审批型 App
  -> 发起节点 UI 提交业务数据
  -> 后端创建 DataSrv approval 并运行真实 workflow skill
  -> workflow runner 返回 progress_instances
  -> 后端同步 DataSrv /progress
  -> 前端审批工作台合并显示运行中节点
  -> 本地 run history 保存 progressInstances
  -> 发布 Hub 包时 governance.testEvidence 携带中间节点证据
  -> 市场审核/企业安装方能确认审批型 App 的 workflow 过程真实可追踪
```

### 推进记录：App Studio 审批型应用布局保存到 Skill 定义证据加固（2026-06-30）

本轮继续推进“应用程序设计时自动生成界面、用户可视化调整位置、布局信息保存到应用信息文件、测试后上传 Hub”的闭环。现有 App Studio 已有 `StudioLayoutDesigner`，可以通过可视化槽位和控件调整 template、density、primaryRegion、outputRegion 以及各运行区域 placement；创建本地 App 和运行面板也已能读取这些 manifest 布局。本轮重点把企业审批型 App 保存到 Skill 定义这条链路钉牢，避免只验证“有 regions”，却没有验证“用户调过的位置真的写进 maclaw.app.json / 发布治理证据”。

已完成：

- 扩展“新建企业审批型 App 保存到 appSkill 定义”的前端回归用例。
- 测试中模拟设计师在 App Studio 里调整审批应用布局：
  - template 改为 `left_nav`
  - density 改为 `compact`
  - primaryRegion 改为 `right`
  - outputRegion 改为 `bottom`
  - `result_panel` 移到底部结果区
- 断言 `SaveMaclawAppDefinitionForSkill(appSkill, manifestText)` 收到的 manifest 中：
  - `app.binding.ui.layouts.approval_workspace` 保留上述布局字段
  - `regions` 保留 `request_form` / `result_panel` 的实际 placement
  - `studio.savedInManifest=true` 且 `updatedBy=app_studio`
  - `app.governance.workspaceLayout` 同步携带可审核的布局证据
- 这让“保存到 Skill -> 测试 -> 上传 Hub/能力市场审核”的包内证据能证明审批型应用界面不是固定表单，而是可视化设计后持久化的传统软件式工作台布局。

验证：

```powershell
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "saves a newly created enterprise approval app into its app skill definition"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

结果：定向用例通过；AppsPage 全量 194 passed。

对应全链路意义：

```text
App Studio 选择企业审批型应用
  -> 绑定 appSkill 和 approval workflow skill
  -> 设计师可视化调整界面布局和区域位置
  -> 保存到 appSkill/maclaw.app.json
  -> manifest.ui.layouts.approval_workspace 保存具体布局
  -> governance.workspaceLayout 形成市场审核证据
  -> 后续测试、上传、安装、运行均可按同一布局定义恢复界面
```

### 推进记录：DataSrv 安装恢复保留审批 workflow progress 证据（2026-06-30）

本轮继续补齐“测试后上传 Hub、安装后从 DataSrv 恢复运行证据”的后半段闭环。此前 GUI 已能在本地运行历史和发布治理证据中保存 `approvalInstance.progressInstances`，但从 DataSrv `app_installations.metadata.test_evidence` 反向恢复已安装 MaClaw App 时，只读取最终 `approval_instance`；如果 DataSrv 把运行中节点保存为顶层 `progress_instances` / `workflow_progress`，恢复到应用面板后会丢失中间审批节点，市场安装方只能看到最终审批结果，不能复核 workflow 过程。

已完成：

- `dataSrvInstalledRunEvidence` 增加 progress evidence 合并逻辑，支持从 DataSrv metadata 读取：
  - `test_evidence.progress_instances` / `progressInstances`
  - `test_evidence.workflow_progress` / `workflowProgress`
  - `test_evidence.approval_progress` / `approvalProgress`
  - 以及 metadata 顶层对应别名
- 当最终 `approval_instance` 本身尚未包含 progress 字段时，恢复逻辑会把顶层 progress 数组合并到 `approvalInstance.progressInstances`。
- 如果最终 `approval_instance` 已经内嵌 progress，则保留原始证据，不覆盖。
- 扩展 DataSrv installed MaClaw App 恢复用例：模拟 DataSrv metadata 中保存“财务总监审批中”的 `progress_instances`，断言添加到应用面板后的 `importedRunEvidence.approvalInstance.progressInstances` 保留 current node、business/result status 和运行中输出。

验证：

```powershell
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "turns DataSrv installed MaClaw apps into addable app candidates with layout metadata"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

结果：定向 DataSrv 安装恢复用例通过；AppsPage 全量 194 passed。

对应全链路意义：

```text
App Studio 测试审批型 App
  -> 发布/安装审计保存最终 approval_instance 和 progress_instances
  -> Hub / DataSrv app_installations metadata 保留测试证据
  -> GUI 从 DataSrv 反向发现已安装 MaClaw App
  -> 添加到应用面板时恢复 importedRunEvidence
  -> approvalInstance.progressInstances 仍可证明中间审批节点
  -> 安装方和审核方能复核“不是只看最终结果，而是审批 workflow 过程可追踪”
```

### 推进记录：Hub SkillMetadata 承载 MaClaw App 企业证据（2026-06-30）
本轮继续补齐“App Studio 测试后上传 Hub、能力市场审核、安装侧复核”的元数据闭环。此前 GUI、DataSrv 和发布包治理证据已经能保存审批实例、workflow progress、动态布局、依赖检查和 DataSrv 注册信息，但 HubCenter 的 `skill.yaml` 结构化 metadata 中 `maclaw_app_test_evidence` 仍只有基础运行结果字段，审核/索引/安装侧如果只读取 SkillMetadata，会看不到企业审批型 App 的关键证据。

已完成：

- 扩展 `hubcenter/internal/skillmarket.SkillMetadata.MaclawAppTestEvidence`，新增结构化字段：
  - `app_kind`
  - `approval_instance`
  - `progress_instances`
  - `approval_views`
  - `dependency_verification`
  - `workspace_layout`
  - `datasrv_registration`
  - `workflow_contract`
- 保留原有 `run_id`、`verified_at`、`definition_fingerprint`、`artifact_*`、`output_count`、`primary_result`、`result_payload`，兼容已有工具型/普通型 App 证据。
- 新增 `TestMetadata_RoundTrip_MaclawAppEvidence`，验证企业审批型 App 的完整证据能从 `SkillMetadata -> YAML -> ParseSkillYAML` 往返，并且 `maclaw_app_test_evidence` 被视为已知字段，不落入 `Extra`。
- 顺手清理前端 `dataSrvInstalledRunEvidence` 中 `approvalEvidenceWithProgress` 与 `artifactName` 挤在同一行的格式问题，避免后续审阅误判逻辑。

验证：

```powershell
go test ./hubcenter/internal/skillmarket -count=1 -vet=off
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

结果：均通过；AppsPage 全量 194 passed。

对应全链路意义：

```text
App Studio 试运行企业审批型 App
  -> 生成 approval_instance、progress_instances、依赖验证、布局和 DataSrv 注册证据
  -> 发布/上传 Hub 时写入治理证据
  -> HubCenter SkillMetadata 可以结构化承载同一组证据
  -> 能力市场审核、索引、安装诊断和二次复核不必只依赖深层 governance JSON
```
### 推进记录：发布包 manifest 提炼企业审批型 App 证据测试加固（2026-06-30）
本轮继续把 Hub SkillMetadata 企业证据字段向真实发布包生成路径收口。当前 `gui/skill_lifecycle.go` 的 `maclawAppTestEvidenceFromDefinition` 已能从 `maclaw.app.json` 的 `app.governance.testEvidence`、`governance.dependencyVerification`、`governance.workspaceLayout`、`governance.datasrv_registration` 和 `binding/governance.workflowContract` 中提炼企业审批型 App 证据；这条路径是 `writeSkillPackageManifest` 生成 `skill_package_manifest.json` 时的关键入口。

已完成：

- 新增 `TestMaclawAppTestEvidenceFromDefinitionIncludesEnterpriseEvidence`，把该发布包证据提炼路径纳入 GUI 包测试。
- 测试覆盖企业审批型 App 的完整证据抽取：
  - `app_kind=enterprise_approval_app`
  - `approval_instance`
  - `progress_instances`
  - `approval_views`
  - `dependency_verification`
  - `workspace_layout`
  - `datasrv_registration`
  - `workflow_contract`
  - `result_payload`、输出数量和文件产物名称
- 这确保 App Studio 试运行后写入 `maclaw.app.json` 的治理证据，在生成市场发布包 manifest 时不会退化成只有 run/result/artifact 的普通 Skill 证据。

验证：

```powershell
go test ./gui -count=1 -vet=off -run "TestMaclawAppTestEvidenceFromDefinitionIncludesEnterpriseEvidence" -timeout 240s
go test ./gui -count=1 -vet=off -run "TestMaclawAppTestEvidenceFromDefinitionIncludesEnterpriseEvidence|TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult|TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv" -timeout 240s
go test ./hubcenter/internal/skillmarket -count=1 -vet=off
```

结果：均通过。

对应全链路意义：

```text
App Studio 试运行企业审批型 App
  -> maclaw.app.json/governance 保存测试证据、布局、依赖、DataSrv 注册、workflow contract
  -> writeSkillPackageManifest 生成 skill_package_manifest.json
  -> maclaw_app_test_evidence 提炼出企业审批型证据
  -> Hub/SkillMarket metadata 和审核/安装诊断具备稳定输入
```
### 推进记录：提交/审核列表暴露 MaClaw App 企业证据摘要（2026-06-30）
本轮继续把企业审批型 App 的测试证据从“包内可提炼”推进到“审核/市场列表可直接消费”。此前 `submission_evidence` 已保留安装视角的完整证据快照，但它偏向安装记录结构；审核页、市场队列和用户刚提交后的即时反馈仍缺少一份轻量、稳定、可扫描的企业证据摘要。

已完成：

- `maclawAppSubmissionSummary` 新增 `review_evidence` 字段。
- `SubmitMaclawAppPackage` 的即时返回新增 `review_evidence`，提交后无需刷新列表即可看到审核证据摘要。
- 新增 `maclawAppSubmissionReviewEvidenceForRecord`，按 App 汇总审核关键信息：
  - app kind / app name
  - test evidence 是否存在、run id、verified at
  - approval instance 是否存在、approval id、审批状态、当前节点
  - workflow progress 数量
  - approval views
  - dependency verification 是否存在、依赖数量、阻塞依赖状态
  - workspace layout 是否存在、布局模板
  - DataSrv registration 状态
  - workflow contract 是否存在、contract schema/version
- 前端 `AppPackageSubmissionSummary` 类型和 `listMaclawAppPackageSubmissions` 解析逻辑接入 `reviewEvidence`，避免 Wails 返回字段在应用面板中被丢弃。
- 新增 `TestMaclawAppSubmissionSummaryIncludesReviewEvidence`，验证企业审批型 App 提交摘要 JSON 暴露 `review_evidence`，并包含审批实例、progress、布局、DataSrv 注册、依赖验证和 workflow contract。

验证：

```powershell
go test ./gui -count=1 -vet=off -run "TestMaclawAppSubmissionSummaryIncludesReviewEvidence|TestSubmitMaclawAppPackageQueuesLocalSubmission" -timeout 240s
go test ./gui -count=1 -vet=off -run "TestMaclawAppSubmissionSummaryIncludesReviewEvidence|TestSubmitMaclawAppPackageQueuesLocalSubmission|TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv|TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult" -timeout 240s
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

结果：均通过；AppsPage 全量 194 passed。

对应全链路意义：

```text
App Studio 测试并提交 MaClaw App 包
  -> SubmitMaclawAppPackage 生成本地审核队列记录
  -> submission_evidence 保留安装视角完整快照
  -> review_evidence 提供市场审核/列表可扫描摘要
  -> 前端提交历史、Hub 同步前诊断和后续审核页可直接显示企业审批证据状态
```

### 推进记录：发布队列可视化展示 review_evidence（2026-06-30）

本轮把上一阶段已经暴露到提交/审核 summary 的 `review_evidence` 继续推进到 GUI 可见界面。此前后端和前端解析层已经能携带审核证据摘要，但发布队列行仍只能显示包身份、依赖数量、安装证据和安装结果；审核人或应用制作者无法在队列列表里直接判断这个 MaClaw App 是否已经具备审批实例、progress、DataSrv 注册、动态布局和 workflow contract 证据。

已完成：

- 前端 `PublishPane` 在本机/Hub 提交队列行中新增 `PublishReviewEvidenceStrip`。
- `review_evidence` 按 App 摊平为紧凑证据条，覆盖：
  - 审批实例状态与当前节点；
  - workflow progress 数量；
  - 依赖验证数量与阻断状态；
  - 动态布局模板；
  - DataSrv 注册状态；
  - workflow contract 版本。
- 证据条复用现有安装证据 chip 的状态色体系，保持企业工作台式的紧凑扫描体验，不引入新的大卡片或表单化展示。
- `AppsPage.test.tsx` 的本地提交队列测试补充 `review_evidence` mock，并断言发布队列能显示审批、进度、依赖、布局、DataSrv 和工作流契约字段。

验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

结果：通过，`194 passed`。

对应全链路意义：

```text
App Studio 测试并提交 MaClaw App 包
  -> 后端提炼 review_evidence 审核摘要
  -> GUI 发布队列直接展示审批实例、progress、依赖、布局、DataSrv、workflow contract
  -> 应用制作者/审核者可在上传、同步、安装前快速判断企业审批型 App 的关键证据是否齐备
```

当前仍未宣称整套 MaClaw App 完成。下一步继续把 `review_evidence` 的可视化同 Hub 审核页/能力市场详情页打通，并继续推进黄金样例 E2E：App Studio 制作/测试/上传、Hub 审核发布、依赖 Skill 下载验签安装、DataSrv 注册、审批 workflow 运行、审批实例中心和结果反馈的同一实例证据闭环。

### 推进记录：Hub 审核与企业市场展示企业审批证据（2026-06-30）

本轮把 `review_evidence`/`maclaw_app_test_evidence` 的可视化从 GUI 本机发布队列继续推进到 Hub 分发侧。此前 App Studio 发布队列已经能看到审批实例、progress、依赖、布局、DataSrv 和 workflow contract，但 HubCenter 审核队列与企业 Hub 市场目录仍主要展示基础测试 run/artifact 信息，审核人与企业管理员无法在市场侧快速判断审批型 App 的关键企业证据是否齐备。

已完成：

- HubCenter `skillmarket-admin.js` 的 MaClaw App 审核详情新增企业证据条：
  - 审批实例状态与当前节点；
  - workflow progress 数量；
  - 依赖验证数量与阻断状态；
  - 动态布局模板；
  - DataSrv 注册状态；
  - workflow contract 版本/schema。
- HubCenter 管理端样式新增 `.sm-review-evidence-strip` / `.sm-review-evidence-chip`，沿用克制的 ready/partial/failed 状态色，保持审核队列紧凑可扫读。
- 企业 Hub `marketplace-tab.js` 的 `maclawAppEvidenceSummary` 从只展示 App/Layout/Result/Test/Dependencies，扩展为同时展示 Approval、Progress、DataSrv、Workflow。
- `hub/web/admin/validate-admin-modules.js` 新增 MaClaw App 企业证据 marker 校验，并对 HubCenter `skillmarket-admin.js` 做 `vm.Script` 语法检查，避免 Hub/HubCenter 市场侧展示链路回退。

验证：

```powershell
node hub/web/admin/validate-admin-modules.js
```

结果：通过，`Admin module validation passed.`

对应全链路意义：

```text
App Studio 测试并上传 MaClaw App 包
  -> 后端提炼 maclaw_app_test_evidence / review_evidence
  -> GUI 本机发布队列展示审核摘要
  -> HubCenter 审核队列展示同一组企业审批证据
  -> 企业 Hub 市场目录展示已导入/待审核 App 的审批、progress、DataSrv、workflow contract 状态
  -> 审核、发布、导入、安装前的关键证据可见性贯通
```

当前仍未宣称整套 MaClaw App 完成。下一步继续推进真实黄金样例 E2E 的运行面：把 Hub 审核/发布后的同一 App 下载安装后，走真实 workflow skill runner 的 initiate/running/complete/result 节点，并由 DataSrv、GUI 运行态、全局审批中心和企业市场审计查看同一实例证据。

### 推进记录：真实 workflow runner 结果同步后 DataSrv 回读同一实例（2026-06-30）

本轮补强审批型 MaClaw App 的真实运行闭环：

```text
已安装企业审批型 App
  -> 发起审批实例
  -> 真实 workflow skill runner 输出 progress_instances / approval_instance
  -> progress 写入 DataSrv /progress
  -> final result 写入 DataSrv /review
  -> GUI 审批实例列表从 DataSrv handled lane 回读同一实例
  -> 保留审批结果、workflow 节点路径、result_payload、content 输出和文件 artifact
```

新增验证点：

```text
TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult
  现在不仅验证 runner 输出会同步到 DataSrv，
  还验证最终 approved 结果同步后，ListMaclawAppApprovalInstancesAll("handled") 能从 DataSrv 读回同一 approval_instance。
```

覆盖的关键字段：

```text
approval_id / workflow_instance_id / workflow_decision_id
workflow_node_id / workflow_node_ids
business_status / result_status
result_payload.approval_result / result_payload.text
outputs(content + artifact)
artifacts(runner-approved.pdf)
```

已运行验证：

```bash
go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult" -timeout 240s
go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult|TestStartMaclawAppApprovalWorkflowCreatesDataSrvApproval|TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv" -timeout 240s
```

结果均通过。该记录证明当前链路已经覆盖：workflow skill 真实运行结果 -> DataSrv 最终审批写回 -> GUI 审批中心/实例列表回读同一结果证据。

### 推进记录：App Studio 布局设计态证据贯穿提交、安装、DataSrv 与市场审核（2026-06-30）

本轮补强动态 UI 设计态闭环：App Studio 自动/可视化生成并保存的 `workspace layout` 不再只作为 manifest 内部结构存在，而是进入后端可审计证据链。

新增贯穿字段：

```text
workspace_layout.studio.savedInManifest / saved_in_manifest
workspace_layout.studio.editable
workspace_layout.studio.updatedBy / updated_by
workspace_layout.studio_saved_in_manifest
workspace_layout.studio_editable
workspace_layout.studio_updated_by
```

链路覆盖：

```text
App Studio 保存布局
  -> app.ui.layouts[entry].studio
  -> SubmitMaclawAppPackage submission_evidence.workspace_layout
  -> review_evidence.workspace_saved_in_manifest / workspace_updated_by
  -> RecordMaclawAppInstall install_evidence.workspace_layout
  -> DataSrv app-installations metadata.workspace_layout + metadata.workspace_layout_studio_*
  -> GUI 本机提交队列审核证据条
  -> Hub / HubCenter 市场审核摘要
```

这样审核端可以直接看到该 App 的传统软件式界面布局是否已经由 App Studio 保存，而不是只看到 template 名称。

已运行验证：

```bash
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
node hub/web/admin/validate-admin-modules.js
go test ./gui -count=1 -vet=off -run "TestMaclawAppSubmissionSummaryIncludesReviewEvidence|TestSubmitMaclawAppPackagePersistsNormalizedWorkspaceLayout|TestRecordMaclawAppInstallRegistersApprovalAppWithDataSrv|TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult|TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv" -timeout 240s
```

结果均通过。
### 推进记录：全局审批中心结果包 UI 覆盖收口（2026-06-30）

本轮继续从“企业审批型应用 = 数据录入 + 审批工作流 Skill 运行 + 审批实例数据管理 + 结果反馈”的用户可见闭环收口。此前后端与 DataSrv 已经能把 workflow runner 的最终审批实例、结构化 `result_payload`、输出块 `outputs` 和文件产物 `artifacts` 回流到 GUI；本轮补齐全局审批中心详情面板的前端回归断言，明确验证用户打开审批实例后能看到：

1. 审批输出块标题与文本内容，例如 `Decision`、`manager approved travel expense`。
2. 输出级文件产物和独立附件，例如 `Approval PDF`、`approval-result.pdf`、`artifact://approval/result.pdf`。
3. 结构化结果包字段，例如 `business_record`、`business_status=finance_approved`、业务记录 `EXP-1`。

验收覆盖：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

结果：`AppsPage.test.tsx` 194 个前端用例全部通过。

当前剩余缺口继续聚焦：

- 端到端真实样例仍需要再跑一轮“App Studio 制作 -> 测试 -> 上传 Hub -> 审核/发布 -> 安装依赖 -> DataSrv 登记 -> 运行审批 -> 审批中心回看结果”的完整验收脚本或手工自动化。
- DataSrv / Hub / GUI 的关键后端定向测试需要按最终路径再做一次集中回归，确认没有只靠前端 mock 覆盖的断点。
- 若要宣称产品级完成，还需要补安装失败、依赖签名失败、DataSrv 离线、workflow skill 运行失败/取消等异常路径的界面验收。
### 推进记录：签名 Hub 审批型 App 安装审计保留企业证据（2026-06-30）

本轮继续推进真实黄金样例 E2E 的安装面。此前 `TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv` 已覆盖：签名 Hub App 包下载、依赖 Skill 下载与验签、DataSrv app-installations 登记、审批 workflow 发起、running 节点同步、final 结果同步，以及从 DataSrv handled lane 回读同一审批实例。新增验收把“安装后本地审计记录”纳入同一条黄金路径。

新增覆盖：

1. `ListMaclawAppInstalls` 能读回刚安装的企业审批型 App 审计记录。
2. 审计记录保留 App 身份、来源 `enterprise_hub`、package hash、ready 依赖计划。
3. 审计记录保留 DataSrv 登记结果：`synced=true`、`synced_count=1`、登记 item 的 `app_id/status/synced`。
4. 审计记录保留审批 workflow contract、App Studio workspace layout、result contract。
5. 审计记录保留导入的审批测试证据 `approvalInstance`，包括 `approval-evidence-golden`。

这一步补的是“安装后的可审计/可恢复证据”，使链路不只在安装返回值和运行态内存中成立，也能从本地安装历史恢复企业审批型 App 的关键证据。

验证：

```powershell
go test ./gui -count=1 -vet=off -run "TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv" -timeout 240s
go test ./gui -count=1 -vet=off -run "TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv|TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult|TestMaclawAppSubmissionSummaryIncludesReviewEvidence|TestRecordMaclawAppInstallRegistersApprovalAppWithDataSrv" -timeout 240s
```

结果：均通过。

对应全链路意义：

```text
Hub 审核/发布签名 MaClaw App
  -> GUI 下载并验签安装 App 依赖 Skill
  -> DataSrv 登记 app-installations
  -> 本地安装历史持久化 DataSrv 登记、workflow contract、布局、结果契约、审批测试证据
  -> 后续冷启动、运行态恢复、审计页面和企业市场诊断可读取同一组证据
```

当前剩余缺口继续聚焦：

- 还需要把完整黄金样例从测试夹具提升到一条更接近真实用户操作的自动化验收脚本/场景：App Studio 制作、测试、上传、Hub 审核发布、下载安装、运行审批、审批中心查看结果。
- 异常路径仍需补强：依赖下载失败、签名/公钥不可信、DataSrv 离线、workflow skill 失败/取消、审批拒绝和需关注结果的可见反馈。
### 推进记录：Hub 安装依赖失败返回结构化诊断（2026-06-30）

本轮开始补企业级交付必须具备的异常路径。此前安装 MaClaw App 时，如果 required Skill 依赖下载、验签或安装失败，后端会阻断安装，但错误信息主要是总括性的 `required Skill dependencies are missing or unavailable`。这不利于 GUI、审核人员或企业管理员判断具体是哪一个 workflow/app/runtime Skill 出问题，也不利于区分 install_ref、下载、签名验签、版本冲突等阶段。

已完成：

1. 新增 `maclawAppInstallPlanBlockingDependencySummary`，从阻塞依赖中提炼诊断摘要。
2. Hub 安装失败和本地安装失败现在会在错误中追加：依赖 ID、错误码、阶段和原始原因。
3. 诊断优先级：`install_error_*` -> `preflight_*` -> `integrity_*` -> `install_ref_*` -> action/health/message。
4. 新增 `TestInstallSelectedMaclawAppPackageFromHubReportsDependencyInstallDiagnostics`，模拟 Hub 安装审批型 App 时 workflow Skill 验签失败，断言错误信息包含：
   - `signed-workflow`
   - `package_integrity_failed`
   - `skillhub_download`
   - `signature verification failed`
   - `public key fingerprint not trusted`

这样前端现有安装失败展示不需要等待新协议，也能直接显示企业管理员可定位的依赖失败原因。

验证：

```powershell
go test ./gui -count=1 -vet=off -run "TestInstallSelectedMaclawAppPackageFromHubReportsDependencyInstallDiagnostics" -timeout 240s
go test ./gui -count=1 -vet=off -run "TestInstallSelectedMaclawAppPackageFromHubReportsDependencyInstallDiagnostics|TestInstallSelectedMaclawAppPackageFromHubUsesPackageSignatureTrustForDependencySkill|TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv|TestPlanMaclawAppInstallPreflightsEnterpriseHubCapability|TestPlanMaclawAppInstallPreflightsSkillMarketDependency" -timeout 240s
```

结果：均通过。

对应全链路意义：

```text
Hub 安装 MaClaw App
  -> required Skill 依赖安装/下载/验签失败
  -> install_plan 标记 failed/blocked 并保留 install_error/preflight/integrity 诊断
  -> InstallSelectedMaclawAppPackageFromHub 返回带依赖 ID、错误码、阶段、原因的可读错误
  -> GUI 安装失败提示可直接定位依赖问题
```

当前剩余缺口继续聚焦：

- DataSrv 离线/登记失败时，安装应保留 partial 注册审计并在运行态/安装历史中明确展示。
- workflow skill 运行失败/取消时，需要生成可审计结果包并在审批中心显示失败/取消状态。
- 审批拒绝、需关注、等待补充输入等非 approved 结果，需要继续用真实 workflow runner + DataSrv 回读测试覆盖。
### 推进记录：DataSrv 登记失败持久化为安装审计（2026-06-30）

本轮继续补企业级异常路径：Hub 安装 MaClaw App 时，DataSrv app-installations 登记可能因为 DataSrv 离线、权限失效或服务端错误失败。安装本身不应因此完全丢失，因为依赖 Skill 和本地 App 包可能已经安装成功；但失败必须进入安装审计，供运行态、安装历史、市场诊断和后续修复使用。

已完成：

1. `datasrv_registration` 新增稳定 `status` 字段：
   - `ready`：全部 eligible App 已登记；
   - `partial`：部分登记成功；
   - `failed`：有 eligible App，但全部登记失败；
   - `skipped`：无可登记 role binding 或 DataSrv 未启用。
2. `registerMaclawAppInstallationsToDataSrv` 在成功、失败、跳过场景都写入 `status`。
3. `maclawAppDataSrvRegistrationForApp` 过滤到单个 App 时也保留/重算 app 级 `status`。
4. 新增 `TestInstallMaclawAppPackageFromHubPersistsDataSrvRegistrationFailure`：
   - Hub 安装发布级企业普通 App；
   - DataSrv `/api/v1/data/app-installations/{app}` 返回 503；
   - 安装仍完成并返回 install record；
   - `datasrv_registration.status=failed`、`synced=false`、`eligible_count=1`、`failed_count=1`；
   - item 级保留 `app_id`、`synced=false` 和 HTTP 原因 `datasrv offline`；
   - `ListMaclawAppInstalls` 能读回同一失败登记审计。

验证：

```powershell
go test ./gui -count=1 -vet=off -run "TestInstallMaclawAppPackageFromHubPersistsDataSrvRegistrationFailure" -timeout 240s
go test ./gui -count=1 -vet=off -run "TestInstallMaclawAppPackageFromHubPersistsDataSrvRegistrationFailure|TestInstallSelectedMaclawAppPackageFromHubInstallsDepsAndRegistersDataSrv|TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv|TestRecordMaclawAppInstallRegistersApprovalAppWithDataSrv" -timeout 240s
```

结果：均通过。

对应全链路意义：

```text
Hub 安装 MaClaw App
  -> Skill 依赖和 App 包本地安装成功
  -> DataSrv app-installations 登记失败
  -> 安装记录保留 failed DataSrv registration 审计
  -> 前端安装历史/运行态可通过 datasrv_registration.status/reason/items 展示失败原因
  -> 后续可做“重新审计/重新登记/修复 DataSrv 配置”闭环
```

当前剩余缺口继续聚焦：

- workflow skill 运行失败/取消时，需要生成可审计结果包并在审批中心显示失败/取消状态。
- 审批拒绝、需关注、等待补充输入等非 approved 结果，需要继续用真实 workflow runner + DataSrv 回读测试覆盖。
- 还需要一条更接近真实用户操作的自动化黄金验收脚本，把 App Studio 制作、测试、上传、Hub 审核发布、下载安装、运行审批、审批中心查看结果串起来。
### 推进记录：workflow Skill 失败生成可审计结果包（2026-06-30）

本轮继续补企业审批型 App 的运行异常路径。此前 `StartMaclawAppApprovalWorkflow(..., run_workflow_skill=true)` 在 workflow Skill 执行失败或输出无法解析时会直接返回 error，导致已经创建的审批实例停留在 pending，DataSrv 和审批中心缺少最终失败结果包。企业审批型应用需要把失败也视为一种可审计结果，而不是只把异常抛给调用方。

已完成：

1. `runMaclawAppApprovalWorkflowSkill` 在 skill 执行失败或 workflow 输出解析失败时，不再直接中断。
2. 新增失败结果包生成逻辑：
   - `status=failed`
   - `lane=handled`
   - `business_status=workflow_failed`
   - `result_status=failed`
   - `result_payload.approval_result=failed`
   - `result_payload.error/text` 保存失败原因
   - `outputs` 保存 `Workflow failed` 输出块
3. 失败实例会写入本地审批实例 registry，并调用 DataSrv `/review` 同步最终失败结果。
4. `normalizeMaclawAppApprovalStatus`、handled lane 过滤、DataSrv review 状态判断纳入 `failed`，使审批中心“已处理”视图能看到失败实例。
5. 新增 `TestStartMaclawAppApprovalWorkflowRecordsFailedWorkflowSkillResult`：
   - 创建真实 workflow Skill runner；
   - skill 命令非零退出；
   - `StartMaclawAppApprovalWorkflow` 返回 `workflow_run.ran=false` 而不是 error；
   - 返回实例为 `failed/handled`；
   - DataSrv 收到 `decision=failed` 的 `/review`；
   - 本地 handled lane 能读回失败结果包。

验证：

```powershell
go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowRecordsFailedWorkflowSkillResult" -timeout 240s
go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowRecordsFailedWorkflowSkillResult|TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult|TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv|TestInstallMaclawAppPackageFromHubPersistsDataSrvRegistrationFailure" -timeout 240s
```

结果：均通过。

对应全链路意义：

```text
企业审批型 App 发起审批
  -> DataSrv 创建审批实例
  -> workflow Skill 执行失败/输出异常
  -> GUI 后端生成 failed approval_instance + result_payload + outputs
  -> DataSrv /review 写入 failed 最终状态
  -> 审批中心 handled lane 可回看失败结果包
```

当前剩余缺口继续聚焦：

- 取消/超时路径需要复用同一结果包机制做专门测试。
- 审批拒绝、需关注、等待补充输入等非 approved 业务结果，需要继续用真实 workflow runner + DataSrv 回读覆盖。
- 最终还需要一条接近真实用户操作的黄金验收脚本，串起 App Studio 制作、测试、上传、Hub 审核发布、下载安装、运行审批和审批中心查看结果。
### 推进记录：workflow Skill 需关注结果保持仅查看语义（2026-06-30）

本轮继续补非 approved 业务结果。此前手工同步路径已经支持 `attention_view_only`，但真实 workflow runner 输出 `attention` 时缺少专门回归，无法证明“需关注（仅查看）”不会被误当成通过/拒绝去 review DataSrv 审批。

已完成：

1. 新增 `TestStartMaclawAppApprovalWorkflowRunsAttentionViewOnlyWorkflowResult`。
2. 测试使用真实 workflow Skill runner 输出 `approval_instance.status=attention`。
3. 验证结果实例：
   - `status=attention`
   - `lane=attention`
   - `result_status=attention`
   - `current_node=expense.attention`
   - `result_payload.approval_result=attention`
   - `result_payload.business_status=finance_attention`
   - `outputs[0].status=attention`
4. 验证 DataSrv 行为：
   - 发起阶段仍创建 DataSrv approval；
   - workflow attention 结果不调用 `/review`；
   - 只 PATCH 业务记录，写入 `approval_status=attention`、`approval_lane=attention`；
   - 本地审批中心 `attention` lane 能读回该结果包。

验证：

```powershell
go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowRunsAttentionViewOnlyWorkflowResult" -timeout 240s
go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowRunsAttentionViewOnlyWorkflowResult|TestStartMaclawAppApprovalWorkflowRecordsFailedWorkflowSkillResult|TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult|TestSyncMaclawAppApprovalInstanceToDataSrvKeepsAttentionViewOnly" -timeout 240s
```

结果：均通过。

对应全链路意义：

```text
企业审批型 App 发起审批
  -> workflow Skill 返回 attention / needs attention
  -> GUI 后端生成 attention approval_instance + result_payload + outputs
  -> DataSrv 业务记录写入需关注状态
  -> 不 review DataSrv 审批实例
  -> 审批中心 attention lane 可回看仅查看结果
```

当前剩余缺口继续聚焦：

- `rejected` 的真实 workflow runner + DataSrv 回读还需要单独覆盖，确认拒绝会 review DataSrv 审批并进入 handled。
- `cancelled/timeout/requires_input` 需要明确产品语义：哪些是最终 handled，哪些回到待补充输入。
- 最终仍需一条接近真实用户操作的黄金验收脚本串起制作、测试、上传、审核、安装、运行和回看。
### 推进记录：workflow Skill 拒绝结果进入 DataSrv review 与 handled 回读（2026-06-30）

本轮继续补非 approved 业务结果。`attention` 已明确为仅查看，不 review DataSrv 审批；`rejected` 则是最终审批结果，必须和 `approved` 一样写入 DataSrv `/review`，并进入审批中心已处理视图。

已完成：

1. 新增 `TestStartMaclawAppApprovalWorkflowRunsRejectedWorkflowResult`。
2. 测试使用真实 workflow Skill runner 输出 `approval_instance.status=rejected`。
3. 验证结果实例：
   - `status=rejected`
   - `lane=handled`
   - `result_status=rejected`
   - `current_node=expense.result`
   - `workflow_decision_id=decision-reject-runner-1`
   - `result_payload.approval_result=rejected`
   - `result_payload.business_status=finance_rejected`
   - `outputs[0].status=rejected`
4. 验证 DataSrv 行为：
   - 发起阶段创建 DataSrv approval；
   - workflow rejected 结果调用 `/api/v1/data/approvals/{id}/review`；
   - review 请求携带 `decision=rejected`、workflow 节点、decision id、业务状态、结果状态和输出块；
   - DataSrv handled lane 回读同一 rejected 结果包。

验证：

```powershell
go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowRunsRejectedWorkflowResult" -timeout 240s
go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowRunsRejectedWorkflowResult|TestStartMaclawAppApprovalWorkflowRunsAttentionViewOnlyWorkflowResult|TestStartMaclawAppApprovalWorkflowRecordsFailedWorkflowSkillResult|TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult" -timeout 240s
```

结果：均通过。

对应全链路意义：

```text
企业审批型 App 发起审批
  -> workflow Skill 返回 rejected
  -> GUI 后端生成 rejected approval_instance + result_payload + outputs
  -> DataSrv /review 写入 rejected 最终审批状态
  -> DataSrv handled lane 回读同一结果包
  -> 审批中心已处理视图可回看拒绝原因与业务状态
```

当前剩余缺口继续聚焦：

- `cancelled/timeout` 的最终状态语义需要补 runner + DataSrv review 回归。
- `requires_input`/等待补充输入需要明确是否回到发起人待补充，而不是进入 handled。
- 最终仍需要一条接近真实用户操作的黄金验收脚本串起制作、测试、上传、审核、安装、运行和回看。
### 推进记录：workflow Skill 超时结果进入最终态回读（2026-06-30）

本轮继续补最终态异常结果。`timeout` 和 `cancelled` 一样不应继续停留在 pending；它是一次审批运行的最终结果，需要进入 handled，并写回 DataSrv 审批实例，供审批中心和审计链路回看。

已完成：

1. 新增 `TestStartMaclawAppApprovalWorkflowRunsTimeoutWorkflowResult`。
2. 测试使用真实 workflow Skill runner 输出 `approval_instance.status=timeout`。
3. 验证结果实例：
   - `status=timeout`
   - `lane=handled`
   - `result_status=timeout`
   - `current_node=expense.timeout`
   - `workflow_decision_id=decision-timeout-runner-1`
   - `result_payload.approval_result=timeout`
   - `result_payload.business_status=approval_timeout`
   - `outputs[0].status=timeout`
4. 验证 DataSrv 行为：
   - 发起阶段创建 DataSrv approval；
   - workflow timeout 结果调用 `/api/v1/data/approvals/{id}/review`；
   - review 请求携带 `decision=timeout`、workflow 节点、decision id、业务状态、结果状态和输出块；
   - DataSrv handled lane 回读同一 timeout 结果包。

验证：

```powershell
go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowRunsTimeoutWorkflowResult" -timeout 240s
go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowRunsTimeoutWorkflowResult|TestStartMaclawAppApprovalWorkflowRunsRejectedWorkflowResult|TestStartMaclawAppApprovalWorkflowRunsAttentionViewOnlyWorkflowResult|TestStartMaclawAppApprovalWorkflowRecordsFailedWorkflowSkillResult|TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult" -timeout 240s
```

结果：均通过。

对应全链路意义：

```text
企业审批型 App 发起审批
  -> workflow Skill 返回 timeout
  -> GUI 后端生成 timeout approval_instance + result_payload + outputs
  -> DataSrv /review 写入 timeout 最终审批状态
  -> DataSrv handled lane 回读同一结果包
  -> 审批中心已处理视图可回看超时原因与业务状态
```

当前剩余缺口继续聚焦：

- `cancelled` 可复用 timeout 的最终态机制，但仍建议补一条轻量回归，避免状态名差异回退。
- `requires_input`/等待补充输入需要明确产品语义：回到发起人待补充，还是独立 lane。
- 最终仍需要一条接近真实用户操作的黄金验收脚本串起制作、测试、上传、审核、安装、运行和回看。
### 推进记录：workflow Skill 取消结果进入最终态回读（2026-06-30）

本轮继续补最终态异常结果。`cancelled` 与 `timeout` 一样代表本次审批运行已经结束，不应继续停留在 pending，也不应进入仅查看的 attention lane。它需要进入 handled，并写回 DataSrv 审批实例，供审批中心、审计链路和安装证据复核。

已完成：

1. 新增 `TestStartMaclawAppApprovalWorkflowRunsCancelledWorkflowResult`。
2. 测试使用真实 workflow Skill runner 输出 `approval_instance.status=cancelled`。
3. 验证结果实例：
   - `status=cancelled`
   - `lane=handled`
   - `result_status=cancelled`
   - `current_node=expense.cancelled`
   - `workflow_decision_id=decision-cancel-runner-1`
   - `result_payload.approval_result=cancelled`
   - `result_payload.business_status=approval_cancelled`
   - `outputs[0].status=cancelled`
4. 验证 DataSrv 行为：
   - 发起阶段创建 DataSrv approval；
   - workflow cancelled 结果调用 `/api/v1/data/approvals/{id}/review`；
   - review 请求携带 `decision=cancelled`、workflow 节点、decision id、业务状态、结果状态和输出块；
   - DataSrv handled lane 回读同一 cancelled 结果包。

验证：

```powershell
go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowRunsCancelledWorkflowResult" -timeout 240s
go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowRunsCancelledWorkflowResult|TestStartMaclawAppApprovalWorkflowRunsTimeoutWorkflowResult|TestStartMaclawAppApprovalWorkflowRunsRejectedWorkflowResult|TestStartMaclawAppApprovalWorkflowRunsAttentionViewOnlyWorkflowResult|TestStartMaclawAppApprovalWorkflowRecordsFailedWorkflowSkillResult|TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult" -timeout 240s
```

结果：均通过。

对应全链路意义：

```text
企业审批型 App 发起审批
  -> workflow Skill 返回 cancelled
  -> GUI 后端生成 cancelled approval_instance + result_payload + outputs
  -> DataSrv /review 写入 cancelled 最终审批状态
  -> DataSrv handled lane 回读同一结果包
  -> 审批中心已处理视图可回看取消原因与业务状态
```

当前剩余缺口继续聚焦：

- `requires_input`/等待补充输入需要明确产品语义并实现：建议不要进入 handled，而是回到发起人待补充或独立待补充 lane。
- 最终仍需要一条接近真实用户操作的黄金验收脚本串起制作、测试、上传、审核、安装、运行和回看。
### 推进记录：workflow Skill 待补充输入结果回到发起人侧（2026-06-30）

本轮补齐 `requires_input` / 待补充输入语义。它不是审批最终态，也不是需关注的仅查看态；它表示 workflow Skill 已经判断当前材料、字段或业务对象不足，需要发起人补充后继续流转。因此它不应进入 handled，也不应调用 DataSrv `/review`。

产品语义确定为：

```text
requires_input
  -> 运行中/待补充状态
  -> 默认进入 my_requests 发起人侧视图
  -> 保留 result_payload.requires_input、outputs 和业务记录引用
  -> DataSrv 同步走 /progress + 业务记录 PATCH
  -> 不写最终审批 review
```

已完成：

1. `normalizeMaclawAppApprovalStatus` 纳入 `requires_input`，不再把它退化为 `pending`。
2. workflow Skill 输出 `approval_instance.status=requires_input` 时，后端默认归入 `my_requests` lane。
3. `SyncMaclawAppApprovalInstanceToDataSrv` 对 `requires_input` 复用进度同步路径：调用 `update_record_approval_progress`，并同步业务记录状态。
4. handled lane 仍只接收 approved / rejected / failed / cancelled / timeout，`requires_input` 不进入已处理。
5. 新增 `TestStartMaclawAppApprovalWorkflowRunsRequiresInputWorkflowResult`，覆盖真实 workflow runner 输出待补充结果：
   - `status=requires_input`
   - `lane=my_requests`
   - `result_status=requires_input`
   - `current_node=expense.require_input`
   - `workflow_decision_id=decision-input-runner-1`
   - `result_payload.approval_result=requires_input`
   - `result_payload.requires_input.fields=[invoice_attachment]`
   - `outputs[0].kind=requires_input`
   - DataSrv 收到 `/progress`，没有收到 `/review`
   - GUI `my_requests` lane 能读回该实例，`handled` lane 不能读到。

验证：

```powershell
go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowRunsRequiresInputWorkflowResult" -timeout 240s
go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowRunsRequiresInputWorkflowResult|TestStartMaclawAppApprovalWorkflowRunsCancelledWorkflowResult|TestStartMaclawAppApprovalWorkflowRunsTimeoutWorkflowResult|TestStartMaclawAppApprovalWorkflowRunsRejectedWorkflowResult|TestStartMaclawAppApprovalWorkflowRunsAttentionViewOnlyWorkflowResult|TestStartMaclawAppApprovalWorkflowRecordsFailedWorkflowSkillResult|TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult" -timeout 240s
```

结果：均通过。

对应全链路意义：

```text
企业审批型 App 发起审批
  -> workflow Skill 返回 requires_input
  -> GUI 后端保留待补充结果包和输出块
  -> DataSrv /progress 写入当前节点与待补充状态
  -> 业务记录 PATCH 为 waiting_for_requester / requires_input
  -> 发起人在我的申请视图看到待补充实例
  -> 补充材料后可继续同一审批实例流转
```

当前剩余缺口继续聚焦：

- 需要把“待补充后继续提交”的二次流转动作补成可视化操作和后端续跑接口。
- 最终仍需要一条接近真实用户操作的黄金验收脚本串起制作、测试、上传、审核、安装、运行、待补充、继续流转和结果回看。
### 推进记录：待补充输入后续跑同一审批实例（2026-06-30）

本轮继续补 `requires_input` 之后的二次流转。待补充不是终态，发起人补齐字段、附件或业务对象后，应继续原审批实例，而不是重新创建一张审批单。

已实现后端续跑语义：

1. `MaclawAppApprovalWorkflowStartInput` 新增可选字段：
   - `approval_id`
   - `instance_id`
   - `continue_from_instance_id`
2. `StartMaclawAppApprovalWorkflow` 在传入上述字段时，会从本地审批实例 registry 恢复原实例。
3. 续跑时复用原 `approval_id / approval_instance_id / workflow_instance_id`，并把本次补充材料写入 `result_payload.supplemental_input`。
4. 续跑开始先把同一审批实例同步为 progress：
   - `business_status=supplemented`
   - `result_status=pending`
   - `lane=pending_my_approval`
   - DataSrv 调用 `/api/v1/data/approvals/{id}/progress`
5. workflow Skill runner 随后继续运行；如果返回 approved/rejected/failed/cancelled/timeout，仍回写同一个 DataSrv approval 的 `/review`。
6. 新增后端回归 `TestStartMaclawAppApprovalWorkflowContinuesRequiresInputWithSupplement`，覆盖：
   - 已存在 `requires_input` 本地实例；
   - 发起人提交补充附件；
   - 不创建新的 DataSrv approval；
   - 先 progress 同一 approval；
   - workflow runner 返回 approved；
   - final review 仍使用同一 `approval_id / workflow_instance_id`；
   - handled lane 回读到补充后通过的结果包。

验证：

```powershell
go test ./corelib/workflow/v2 -count=1 -vet=off -run "^$"
go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowContinuesRequiresInputWithSupplement" -timeout 240s
go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowContinuesRequiresInputWithSupplement|TestStartMaclawAppApprovalWorkflowRunsRequiresInputWorkflowResult|TestStartMaclawAppApprovalWorkflowRunsCancelledWorkflowResult|TestStartMaclawAppApprovalWorkflowRunsTimeoutWorkflowResult|TestStartMaclawAppApprovalWorkflowRunsRejectedWorkflowResult|TestStartMaclawAppApprovalWorkflowRunsAttentionViewOnlyWorkflowResult|TestStartMaclawAppApprovalWorkflowRecordsFailedWorkflowSkillResult|TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult" -timeout 240s
```

结果：均通过。

为解除本轮验证阻塞，顺手修复了 `corelib/workflow/v2/phase_prompt.go` 的编译问题：`phaseInstruction` 调用按 `WorkflowType(state.Type)` 传参，并移除新增 workflow case 中与既有 `design` phase id 冲突的重复 case；同时为 grant proposal / paper writing 补齐缺失阶段指令常量。

对应全链路意义：

```text
审批型 App 返回 requires_input
  -> 发起人在我的申请看到待补充
  -> 用户补充字段/附件/业务对象
  -> GUI 调用 StartMaclawAppApprovalWorkflow(approval_id, continue_from_instance_id, form_data)
  -> 后端恢复同一审批实例并写 supplemental_input
  -> DataSrv progress 更新同一 approval
  -> workflow Skill 继续运行
  -> 最终结果 review 同一 approval
  -> 审批中心 handled / my_requests / pending_my_approval 看到的是同一条实例历史
```

当前剩余缺口继续聚焦：

- 前端运行工作台需要露出“补充材料并继续”可视化动作，把当前 `requires_input` 实例的缺失字段/附件渲染成可编辑区域。
- 编译阻塞解除后，补跑 `TestStartMaclawAppApprovalWorkflowContinuesRequiresInputWithSupplement` 和完整审批状态回归。
- 继续推进真实用户黄金路径：制作、测试、上传、审核、安装、运行、待补充、继续流转、结果回看。
### 推进记录：前端审批工作台支持待补充材料并继续流转（2026-06-30）

本轮补齐 `requires_input` 在 GUI 运行态的可视化闭环。审批型 App 的 workflow Skill 返回待补充材料后，发起人不再只看到一条静态状态，而是在审批工作台和全局审批详情中看到可操作的补充面板，并可继续同一审批实例流转。

已实现前端运行态能力：

1. `ApprovalInstanceView.status` 扩展为覆盖 `requires_input / failed / cancelled / timeout / running / submitted / in_progress`，全局审批筛选器同步可选这些状态。
2. 新增 `approvalRequiresInputInfo`，从 `result_payload.requires_input`、`resultPayload.requiresInput`、`outputs.kind=requires_input` 中提取待补充标题、说明和缺失字段。
3. 新增 `ApprovalSupplementPanel`，在运行态审批工作台和全局审批详情中展示：
   - 待补充说明；
   - 缺失字段 chips；
   - 补充说明输入；
   - 附件/材料引用输入；
   - “补充后继续”动作。
4. 运行态 `continueApprovalInstanceWithSupplement` 调用后端 `StartMaclawAppApprovalWorkflow`，携带：
   - `approval_id`；
   - `instance_id`；
   - `continue_from_instance_id`；
   - `form_data.supplement_note / supplement_reference`；
   - `business_payload.supplemental_input`；
   - `workflow_run_args.supplemental_input`。
5. 前端把后端返回的 `workflow_run.progress_instances` 与最终 `instance` 合并回本地 `approvalInstances`，因此同一审批实例可从待补充继续进入 pending/running/approved 等后续状态。
6. 历史证据展示契约保持兼容：审批节点链、DataSrv 审批明细、SkillMarket/Hub 分组、发布证据、结果契约决策串恢复为既有 `/` 分隔格式，避免新 UI 改动破坏既有回归。

验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "continues a requires-input approval instance"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "opens global approval management|DataSrv approval app summary|SkillMarket results|approval instance evidence|visual result contract"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

结果：

- 待补充继续流转目标测试通过。
- 5 个历史 UI 证据回归测试通过。
- `AppsPage.test.tsx` 全量通过：195 passed / 195。
- 后端定向补跑通过：`go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowContinuesRequiresInputWithSupplement|TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv" -timeout 240s`。

对应全链路意义：

```text
workflow Skill 返回 requires_input
  -> GUI 审批工作台展示缺失字段和补充入口
  -> 发起人填写补充说明/材料引用
  -> GUI 调用 StartMaclawAppApprovalWorkflow 续跑同一 approval_id
  -> 后端写 supplemental_input 并同步 DataSrv progress
  -> workflow Skill 继续运行
  -> GUI 合并 progress/final instance
  -> 审批中心和运行工作台回看同一审批实例的完整历史
```

当前剩余缺口继续聚焦：

- 继续补一条更接近真实用户路径的黄金验收：App Studio 制作、测试、上传 Hub、审核发布、安装依赖、DataSrv 注册、发起审批、待补充、继续流转、结果回看。
- 在黄金验收稳定后，清理临时测试输出文件，并整理应保留的新源码/测试/文档改动。
### 推进记录：签名 Hub 安装后待补充再续跑黄金验收（2026-06-30）

本轮把真实黄金路径从“签名 Hub 审批型 App 安装后可发起并回读 approved 结果”，继续推进到“安装后的同一 App 可先返回 `requires_input`，发起人补充材料后续跑同一审批实例并最终回读 handled 结果”。这补齐了此前前端/后端单独覆盖之外的关键组合证据。

已完成：

1. 扩展 `TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv`：
   - 先从企业 Hub 下载签名 MaClaw App 包；
   - 下载并验签安装 `expense-super-skill` 与 `expense-workflow`；
   - 安装时登记 DataSrv `app-installations`；
   - 发起第一张审批并回读 approved 结果包；
   - 使用已安装的 `expense-workflow` 依赖 Skill 作为真实 runner；
   - 发起第二张审批，workflow runner 返回 `requires_input`；
   - 验证 `requires_input` 进入发起人 `my_requests` lane，并保留缺失材料输出块；
   - 补充 `invoice_attachment` 后调用 `StartMaclawAppApprovalWorkflow` 续跑同一 `approval_id=approval-golden-2` / `workflow_instance_id=wf-golden-2`；
   - 验证续跑不再创建新的 DataSrv approval；
   - workflow runner 返回 approved 后，从 DataSrv handled lane 回读同一实例。
2. 修复后端 workflow 结果合并：
   - 续跑开始时写入的 `result_payload.supplemental_input` 现在会在 workflow runner 最终结果覆盖 `result_payload` 时被保留；
   - 这样 workflow Skill 不需要主动回显补充材料，GUI/DataSrv 仍可审计发起人补充了什么。
3. 保持既有独立续跑用例：`TestStartMaclawAppApprovalWorkflowContinuesRequiresInputWithSupplement` 继续通过。

验证：

```powershell
go test ./gui -count=1 -vet=off -run "TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv" -timeout 240s
go test ./gui -count=1 -vet=off -run "TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv|TestStartMaclawAppApprovalWorkflowContinuesRequiresInputWithSupplement" -timeout 240s
```

结果：均通过。

对应全链路意义：

```text
App Studio/Hub 发布的签名审批型 App
  -> GUI 从 Hub 安装 App 与依赖 Skill
  -> 安装时完成依赖验签与 DataSrv 登记
  -> 运行审批 workflow Skill
  -> workflow 返回 requires_input
  -> GUI/DataSrv 记录待补充实例
  -> 发起人补充材料
  -> GUI 续跑同一 approval_id / workflow_instance_id
  -> workflow 返回 approved
  -> DataSrv handled lane 回读同一实例最终结果
  -> result_payload 保留 supplemental_input 审计证据
```

当前剩余缺口继续聚焦：

- 这条黄金路径已经是后端自动化夹具级闭环；下一步需要把 App Studio 的用户可视化制作、测试、上传、Hub 审核发布操作进一步提升为更接近真实点击流的前端/集成验收。
- 继续清理当前工作树中的临时测试输出文件，区分应提交源码与过程产物。
- 异常路径仍需集中补 UI 验收：DataSrv 离线、依赖下载失败、签名不可信、workflow 失败/取消/超时后的运行态反馈。
### 推进记录：前端市场安装后运行态待补充续跑验收（2026-06-30）

本轮把上一阶段后端 golden path 的“签名 Hub 安装后待补充再续跑”继续推进到用户可见的 AppsPage 点击流。此前前端分别覆盖了 Hub 市场安装审批型 App、运行态 `requires_input` 补充材料续跑；本轮把两者连起来，验证用户从市场安装后的 App 可以直接进入运行态审批工作台，并对待补充实例继续同一审批流程。

已完成：

1. 扩展 `AppsPage.test.tsx` 的 `installs approved Hub MaClaw Apps from market search results`：
   - 从 App Studio / Add from market 搜索 Enterprise Hub App；
   - 安装企业审批型 App 并保存 Hub 安装证据、依赖验证证据、App Studio 布局证据；
   - 关闭 Studio，回到应用面板；
   - 打开刚安装的审批型 App；
   - 运行态审批工作台加载 `requires_input` 实例；
   - 展示缺失字段 `signed_contract` 和待补充输出块；
   - 用户填写补充说明和材料引用；
   - 点击 `Continue with supplement`；
   - 断言前端调用 `StartMaclawAppApprovalWorkflow` 时携带安装后的 App 身份、同一 `approval_id`、同一 `workflow_instance_id`、DataSrv 记录信息、workflow Skill 版本和 `result_payload.supplemental_input`。
2. 这条测试把前端可视化路径串成：
   - 市场分发；
   - 安装依赖证据；
   - 动态 UI 布局恢复；
   - 审批实例管理；
   - 待补充材料继续流转。

验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "installs approved Hub MaClaw Apps from market search results"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "installs approved Hub MaClaw Apps from market search results|continues a requires-input approval instance"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx

cd D:\workprj\aicoder
go test ./gui -count=1 -vet=off -run "TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv|TestStartMaclawAppApprovalWorkflowContinuesRequiresInputWithSupplement" -timeout 240s
```

结果：

- 前端单测 targeted 通过。
- 前端待补充组合 targeted 通过。
- `AppsPage.test.tsx` 全量通过：195 passed / 195。
- 后端 Hub 安装 + 待补充续跑 golden 组合通过。

对应全链路意义：

```text
用户在 App Studio 打开能力市场
  -> 搜索并安装 Enterprise Hub 审批型 App
  -> GUI 保存安装证据、依赖证据和动态布局
  -> 用户从主应用面板打开新装 App
  -> 运行态审批工作台显示 requires_input 实例
  -> 用户可视化填写补充材料
  -> GUI 调用 StartMaclawAppApprovalWorkflow 续跑同一审批实例
  -> 后端 golden path 已验证同一实例进入 DataSrv handled 回读
```

当前剩余缺口继续聚焦：

- 进一步把 App Studio“制作/测试/提交审核/审核通过/发布/安装/运行”的全点击流拆成可维护的前端集成验收，避免单个巨型测试继续膨胀。
- 清理工作树里的临时输出文件和历史测试产物，只保留源码、测试和必要文档。
- 异常路径 UI 仍需做集中验收：DataSrv 离线、依赖下载失败、签名不可信、workflow 失败/取消/超时。
### 推进记录：运行态审批实例加载失败异常路径收口（2026-06-30）

本轮把审批型 MaClaw App 运行态的 DataSrv 审批实例读取失败路径补齐为可见、可恢复、可验证的行为。此前 `ApprovalWorkspace` 在后端读取失败时仍可能使用本地生成的 fallback 审批实例，用户会看到类似真实审批行的内容，容易误判当前实例数据可用；现在读取失败时不再展示生成行，只显示错误提示和空态，用户点击刷新后可以重新拉取 DataSrv 实例。

已完成：

1. 调整运行态审批工作台实例来源：
   - 当 `ListMaclawAppApprovalInstances` 失败、`approvalLoadState === 'error'` 时，`ApprovalWorkspace` 不再调用本地生成的 fallback 实例作为列表数据；
   - 工作台保留 `审批实例读取失败` 告警；
   - 列表区显示 `当前分类暂无审批实例`，避免把模拟/生成节点误展示为真实 DataSrv 审批实例。
2. 新增前端异常路径验收：
   - 模拟 DataSrv 审批实例读取连续失败；
   - 打开内置审批型 App `报销申请`；
   - 验证工作台出现读取失败提示；
   - 验证不会展示 fallback 行 `发起节点`；
   - 点击 `刷新` 后模拟 DataSrv 恢复；
   - 验证真实实例 `Recovered expense approval` 和结果包重新出现在工作台。
3. 回归确认不影响既有审批工作台和待补充材料续跑路径。

验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "approval workspace load failures"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "shows approval workspace load failures|shows the approval instance workspace|continues a requires-input approval instance"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx

cd D:\workprj\aicoder
go test ./gui -count=1 -vet=off -run "TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv|TestStartMaclawAppApprovalWorkflowContinuesRequiresInputWithSupplement" -timeout 240s
```

结果：
- 定向异常路径前端测试通过：1 passed / 196。
- 审批工作台组合前端测试通过：3 passed / 196。
- `AppsPage.test.tsx` 全量通过：196 passed / 196。
- 后端 Hub 安装 + 待补充续跑 golden 组合通过。

对应全链路意义：

```text
运行态审批型 MaClaw App
  -> 从 DataSrv 拉取审批实例失败
  -> GUI 明确展示审批实例读取失败
  -> 不使用本地 fallback 行伪装真实实例
  -> 用户刷新
  -> DataSrv 恢复后显示真实审批实例和结果包
```

当前剩余缺口继续聚焦：
- 继续补齐依赖下载失败、签名不可信、DataSrv 运行时离线、workflow failed/cancelled/timeout 的可见异常态和验收。
- 把 App Studio 制作、测试、提交审核、发布、安装、运行拆成更小的前端集成验收，避免单个巨型测试继续膨胀。
- 后续清理临时测试输出文件，但需要先区分用户改动、过程产物和必须保留的新源码/测试/文档。
### 推进记录：Hub 市场安装依赖失败前端可见诊断（2026-06-30）

本轮把上一阶段后端已经返回的 required Skill 依赖失败诊断推进到用户可见的市场安装路径。企业审批型 MaClaw App 从 Hub 一键安装时，如果 workflow/app/runtime Skill 依赖下载或验签失败，前端现在有明确验收：错误信息会停留在对应市场行，用户能直接看到失败依赖、错误码、阶段和原始原因，而不是只看到泛化的安装失败。

已完成：

1. 新增 `AppsPage.test.tsx` 前端验收：
   - 从 Enterprise Hub 搜索审批型 App `Expense Approval Pro`；
   - 点击市场行 `Add` 触发 `InstallSelectedMaclawAppPackageFromHub`；
   - 模拟后端返回 required workflow Skill 验签失败；
   - 验证市场行进入 blocked 状态；
   - 验证用户可见文本包含 `signed-workflow`、`package_integrity_failed`、`skillhub_download`、`signature verification failed`、`public key fingerprint not trusted`；
   - 验证失败后不会把 App 写入管理列表。
2. 保持前端安装逻辑复用后端错误 message，不引入新的失败协议；当前 UI 已能消费 `InstallSelectedMaclawAppPackageFromHub` 返回的结构化摘要。
3. 用后端定向测试再次确认该错误文本来自真实安装路径：`TestInstallSelectedMaclawAppPackageFromHubReportsDependencyInstallDiagnostics`。

验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "dependency install diagnostics"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx

cd D:\workprj\aicoder
go test ./gui -count=1 -vet=off -run "TestInstallSelectedMaclawAppPackageFromHubReportsDependencyInstallDiagnostics" -timeout 240s
```

结果：
- 定向前端测试通过：1 passed / 197。
- `AppsPage.test.tsx` 全量通过：197 passed / 197。
- 后端依赖诊断定向测试通过。

对应全链路意义：

```text
Enterprise Hub 审批型 MaClaw App 安装
  -> required workflow Skill 下载/验签失败
  -> 后端错误携带依赖 ID、错误码、阶段、原始原因
  -> GUI 市场行进入 blocked 状态并展示诊断文本
  -> App 不进入本地管理列表
```

当前剩余缺口继续聚焦：
- 签名不可信/包签名失败需要在市场安装和包粘贴安装两条路径继续做前端可见验收。
- DataSrv 运行时离线、workflow failed/cancelled/timeout 仍需要补运行态结果反馈和审批中心回看断言。
- App Studio 制作、测试、上传、审核发布、安装、运行的完整点击流仍需拆分成更小的稳定集成验收。
### 推进记录：Hub App 包签名失败前端可见反馈（2026-06-30）

本轮继续收口 Enterprise Hub 分发安装的可信异常路径。此前后端已经能在下载 Hub MaClaw App 包时校验 `package_signature`，并在签名无效时拒绝包；前端市场安装路径也会展示安装错误，但缺少固定验收来证明“App 包自身签名失败”不会被吞掉，也不会进入后续依赖安装或本地落库。

已完成：

1. 新增 `AppsPage.test.tsx` 前端验收：
   - 从 Enterprise Hub 搜索审批型 App `Signed Contract Intake`；
   - 点击市场行 `Add` 触发 `InstallSelectedMaclawAppPackageFromHub`；
   - 模拟后端返回 `maclaw app package signature verification failed`；
   - 验证市场行进入 blocked 状态，并在行内显示包签名失败原因；
   - 验证不会继续调用本地依赖安装 `InstallMaclawAppDependencies`；
   - 验证不会调用 `RecordMaclawAppInstall` 写入本地安装审计；
   - 验证失败 App 不出现在管理列表。
2. 与上一条依赖验签失败用例一起确认：Hub App 包自身签名失败、required Skill 依赖验签失败，都会在市场行内形成可定位、不中断上下文的企业工作台式反馈。
3. 后端定向测试确认 App 包签名无效仍由真实下载路径拒绝：`TestDownloadMaclawAppPackageFromHubRejectsInvalidPackageSignature`。

验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "package signature failures|dependency install diagnostics"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx

cd D:\workprj\aicoder
go test ./gui -count=1 -vet=off -run "TestDownloadMaclawAppPackageFromHubRejectsInvalidPackageSignature|TestInstallSelectedMaclawAppPackageFromHubReportsDependencyInstallDiagnostics" -timeout 240s
```

结果：
- 签名/依赖异常前端组合通过：2 passed / 198。
- `AppsPage.test.tsx` 全量通过：198 passed / 198。
- 后端签名异常定向组合通过。

对应全链路意义：

```text
Enterprise Hub 市场搜索 MaClaw App
  -> 用户点击安装
  -> Hub App 包 package_signature 校验失败
  -> GUI 市场行 blocked 并展示签名失败原因
  -> 不进入依赖安装
  -> 不写本地安装审计
  -> 不出现在 App 管理列表
```

当前剩余缺口继续聚焦：
- DataSrv 运行时离线需要补“发起/续跑审批失败时”的运行态反馈，而不是只覆盖审批实例列表加载失败。
- workflow failed/cancelled/timeout 需要补结果反馈和审批中心回看断言。
- App Studio 制作、测试、上传、Hub 审核发布、安装、运行仍需拆成稳定的全链路点击流验收。
### 推进记录：运行态审批发起失败落本地 failed 实例（2026-06-30）

本轮继续收口企业审批型 MaClaw App 的运行态异常反馈。此前审批 workflow 启动失败或 DataSrv 在发起阶段离线时，前端主要显示顶部运行错误并写一条 run history，审批实例工作台可能没有可回看的失败实例；这与“审批型应用 = 数据录入 + 审批工作流 Skill 运行 + 审批实例数据管理 + 结果反馈”的目标不一致。本轮改为：只要审批发起或待补充续跑失败，GUI 就落一条本地 `failed` 审批实例，保留错误文本、结果包、workflow_failed 事件，并尽力同步 DataSrv；DataSrv 仍离线时，本地 failed 实例继续可见。

已完成：

1. 前端运行态新增失败落库 helper：
   - 将失败审批实例写入 `handled` lane；
   - `status=failed`、`business_status=workflow_error`、`result_status=failed`；
   - `result_payload` 保存 `approval_result=failed`、`workflow_lifecycle=error` 和错误文本；
   - `outputs` 生成 `approval_result` 结果块；
   - `events` 追加 `workflow_failed`；
   - 先更新本地工作台，再尝试 `RecordMaclawAppApprovalInstance` 和 DataSrv sync。
2. 首次发起审批失败接入该 helper：
   - workflow Skill 启动失败不再是“无审批实例”；
   - DataSrv 发起同步失败也会留下本地 failed 实例。
3. 待补充材料续跑失败也接入同一 helper：
   - 保留原审批实例 ID、approval ID、record ID、supplemental input；
   - 失败后能在工作台/审批中心回看。
4. 修正 `backendApprovalInstanceToView` 状态归一化：
   - 保留 `requires_input`、`failed`、`cancelled`、`timeout`、`running`、`submitted`、`in_progress`；
   - 避免这些后端状态被误压成 `pending`，从而影响 handled lane、筛选和详情展示。

新增/更新验收：

- `records approval workflow launch failures as failed instances`
  - 模拟 workflow Skill 启动失败；
  - 验证写入 failed 审批实例；
  - 验证结果包、事件、DataSrv 同步尝试；
  - 验证运行态工作台 handled lane 可看到错误文本。
- `records DataSrv start failures as failed approval instances in the runtime workspace`
  - 模拟 `StartMaclawAppApprovalWorkflow` 因 DataSrv 离线失败；
  - 验证本地 failed 实例仍然可见；
  - 验证不丢失错误文本和结果包。

验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "records approval workflow launch failures|records DataSrv start failures"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "records approval workflow launch failures|records DataSrv start failures|marks approval workflow failures|continues a requires-input approval instance|approval workspace load failures"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx

cd D:\workprj\aicoder
go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowCreatesDataSrvApproval|TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult|TestStartMaclawAppApprovalWorkflowRunsTimeoutWorkflowResult|TestStartMaclawAppApprovalWorkflowContinuesRequiresInputWithSupplement" -timeout 240s
```

结果：
- 新增运行态失败前端测试通过：2 passed / 199。
- 审批异常组合前端测试通过：5 passed / 199。
- `AppsPage.test.tsx` 全量通过：199 passed / 199。
- 后端审批运行态组合通过。

对应全链路意义：

```text
审批型 MaClaw App 运行态发起/续跑
  -> workflow Skill 启动失败 或 DataSrv 发起同步失败
  -> GUI 顶部显示运行错误
  -> 本地审批实例工作台生成 failed 实例
  -> 结果包记录错误文本和 workflow_lifecycle=error
  -> handled lane / 审批中心可回看失败实例
  -> DataSrv 恢复后可基于本地实例继续审计或重试同步
```

当前剩余缺口继续聚焦：
- workflow cancelled/timeout 的前端用户可见回看还需要更明确的 UI 验收，后端 timeout 已有定向覆盖。
- App Studio 制作、测试、上传、Hub 审核发布、安装、运行仍需拆成稳定的全链路点击流验收。
- 临时测试输出文件仍需在最终收口前清点，避免过程产物混入提交范围。
### 推进记录：workflow 取消/超时结果运行态回看验收（2026-06-30）
本轮继续收口审批型 MaClaw App 的运行态最终结果反馈。后端已经能把 workflow Skill 返回的 `cancelled` / `timeout` 写成最终审批实例，并同步 DataSrv `/review`；前端运行态也需要明确区分这两类结果，不能把 timeout 混成普通 error，也不能让 cancelled/timeout 停留在 pending 或仅顶部提示中。

已完成：

1. 前端 workflow lifecycle 归一化补齐 `timeout`：
   - `timeout` / `timed_out` / `timed-out` 不再归入 generic error；
   - timeout 结果进入 `handled` lane；
   - 结果包保留 `workflow_lifecycle=timeout`、`result_status=timeout`、`business_status=timeout` 或后端显式业务状态；
   - 事件动作为 `workflow_timeout`。
2. `cancelled` 结果继续作为最终态处理：
   - 进入 `handled` lane；
   - 结果包保留 `workflow_lifecycle=cancelled`、`result_status=cancelled`；
   - 事件动作为 `workflow_cancelled`。
3. 手动审批决策路径保持独立：
   - 人工通过/驳回/需关注仍写入 `status=decision`；
   - workflow lifecycle 只用于 Skill 运行结果归档，不污染审批人手动决策。
4. 运行态工作台新增/补强 UI 回看验收：
   - cancelled/timeout 都能在 handled 审批工作区看到；
   - 详情中保留结果文本、当前节点、结果包和事件轨迹；
   - 与 DataSrv 发起失败、workflow failed、requires_input 继续补充等异常路径组合回归。

验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "timeout and cancelled workflow results|cancelled workflow results|records DataSrv start failures|records approval workflow launch failures|marks approval workflow failures|continues a requires-input approval instance|approval workspace load failures"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "records approval decisions with workflow result fields|records DataSrv sync failures in approval instance timelines|keeps approval result package when a pending item is manually approved|shows the approval instance workspace for approval apps"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx

cd D:\workprj\aicoder
go test ./gui -count=1 -vet=off -run "TestStartMaclawAppApprovalWorkflowCreatesDataSrvApproval|TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult|TestStartMaclawAppApprovalWorkflowRunsTimeoutWorkflowResult|TestStartMaclawAppApprovalWorkflowContinuesRequiresInputWithSupplement" -timeout 240s
```

结果：
- 前端 timeout/cancelled 与异常组合定向验收通过：7 passed / 194 skipped。
- 前端人工审批回归定向验收通过：4 passed / 197 skipped。
- `AppsPage.test.tsx` 全量通过：201 passed / 201。
- 后端审批运行态组合通过。

对应全链路意义：

```text
审批型 MaClaw App 运行 workflow Skill
  -> Skill 返回 cancelled 或 timeout
  -> GUI 将结果转为最终审批实例
  -> handled lane / 审批中心可回看
  -> result_payload / outputs / events 保留最终态证据
  -> DataSrv review 链路与前端工作台语义一致
```

当前剩余缺口继续聚焦：
- App Studio 制作、动态布局调整、保存、测试、上传 Hub 的点击流验收还需要继续拆分实现。
- Hub 审核发布、安装依赖、运行已安装 App 的端到端 UI 级验收还需要继续补齐。
- DataSrv / Hub / GUI 三端合同字段需要做最终一致性审计，尤其是 layout、dependency_verification、approval_instance、result_contract、test_evidence。
### 推进记录：App Studio 提交审核摘要补齐 result/test 证据（2026-06-30）
本轮继续推进 App Studio -> 提交审核 -> Hub/市场安装前的证据链闭环。此前本地提交队列和审核摘要已经能暴露 workspace layout、依赖验证、审批实例、DataSrv 注册和 workflow contract，但缺少 result contract、test protocol、result coverage、输出/附件计数这些“设计后测试、测试后上传”的关键证据摘要。这样 Hub 审核页或企业市场审查工具只能看到 App 已经有测试证据，却不容易判断测试是否覆盖了声明的输出合同。

已完成：

1. 后端 `maclawAppSubmissionReviewEvidenceForRecord` 增加审核摘要字段：
   - `has_result_contract`；
   - `result_contract_primary`；
   - `result_contract_type_count`；
   - `has_test_protocol`；
   - `test_protocol_fingerprint`；
   - `result_coverage_ok`；
   - `result_coverage_primary`；
   - `result_coverage_missing_count`；
   - `output_count`；
   - `artifact_count`。
2. 审核摘要同时支持 governance 与 binding 中的 result contract/test protocol 来源，避免 App Studio 保存位置和 Hub 包展开位置不一致时丢证据。
3. 后端测试 `TestMaclawAppSubmissionSummaryIncludesReviewEvidence` 补充 result contract、test protocol、result coverage、outputs、artifacts 断言，并确认 JSON `review_evidence` 对外暴露这些字段。
4. 原提交队列路径 `TestSubmitMaclawAppPackageQueuesLocalSubmission` 一并回归，确认新增摘要不影响 package queue 和 dependency audit。

验证：

```powershell
cd D:\workprj\aicoder
gofmt -w gui\app_maclaw_apps.go gui\app_maclaw_apps_test.go
go test ./gui -count=1 -vet=off -run "TestMaclawAppSubmissionSummaryIncludesReviewEvidence|TestSubmitMaclawAppPackageQueuesLocalSubmission" -timeout 240s
```

结果：
- 后端 App Studio/提交审核摘要定向测试通过。

对应全链路意义：

```text
App Studio 设计动态 UI + 输出合同 + 测试协议
  -> 用户运行测试形成 test evidence / result coverage
  -> 提交审核生成 maclaw.app.pack.v1
  -> 本地提交队列 summary 暴露 review_evidence
  -> Hub/市场审核可直接看到 layout + dependency + workflow + result/test 覆盖摘要
```

当前剩余缺口继续聚焦：
- Hub 审核发布页面需要消费这些 `review_evidence` 字段，形成市场审核侧可见的证据卡。
- 安装已发布 App 后，需要继续验证 install evidence 能把 result/test 证据回填到本地 App 管理和运行态详情。
- App Studio 的完整点击流仍需要补一条从创建审批型应用、调整布局、运行审批测试、提交审核、安装运行的 UI 级组合验收。
### 推进记录：Hub 审核侧消费 App Studio result/test 证据（2026-06-30）
本轮继续推进“App Studio 设计/测试/提交 -> Hub 审核发布”的可见证据闭环。上一轮后端提交队列已经把 `review_evidence` 扩展到 result contract、test protocol、result coverage、outputs/artifacts；本轮把这些字段接入 Hub 审核/市场管理界面，避免证据只停留在 JSON 队列里。

已完成：

1. HubCenter 能力市场审核页增强 MaClaw App 审核证据卡：
   - 新增 Result chip：显示 result contract primary 与类型数量；
   - 新增 Test chip：显示 test protocol fingerprint；
   - 新增 Coverage chip：显示 coverage primary、ok/missing 状态；
   - 新增 Outputs chip：显示输出块和附件数量；
   - 保留原有 Approval / Progress / Dependencies / Layout / DataSrv / Workflow chips。
2. 企业 Hub `marketplace-tab.js` 的 MaClaw App 卡片摘要兼容 `review_evidence`：
   - 可读取按 app id/app name 分组的 `review_evidence`；
   - 可读取 `result_contract_primary`、`test_protocol_fingerprint`、`result_coverage_primary` 等扁平字段；
   - 卡片摘要新增 Coverage / Outputs 行。
3. 管理模块静态校验增加防回退 marker：
   - 企业 Hub 必须保留 `review_evidence`、Result/Test/Coverage/Outputs 字段读取；
   - HubCenter 必须保留 result/test/coverage/output evidence chips。

验证：

```powershell
cd D:\workprj\aicoder
node hub/web/admin/validate-admin-modules.js
```

结果：
- Admin module validation passed.

对应全链路意义：

```text
App Studio 设计动态 UI + 输出合同 + 测试协议
  -> 测试形成 test evidence / result coverage
  -> 提交审核生成 review_evidence
  -> Hub / HubCenter 审核界面显示 result/test/coverage/output 证据
  -> 审核人可判断 App 是否真的覆盖声明输出，而不是只看到“有测试”
```

当前剩余缺口继续聚焦：
- 已发布/已安装 App 的 install evidence 回填到 GUI App 管理详情和运行态详情还需要最终一致性验收。
- App Studio 仍需要一条 UI 级组合验收：创建审批型应用、调整布局、运行审批测试、提交审核、Hub 审核发布、安装、运行。
- DataSrv / Hub / GUI 字段合同仍需做最后审计，尤其是 `review_evidence` 与 `install_evidence` 在跨端命名上的一致性。
### 推进记录：安装后 result/test/coverage 证据回填 GUI 回看（2026-06-30）

本轮把 App Studio 提交审核摘要和 Hub 审核侧已经具备的 result/test/coverage/output 证据继续回填到 GUI 本地回看路径，避免证据只停留在后端 JSON 或 Hub 审核页里。现在本机提交队列和已安装 App 的证据快照都能显示结果契约、测试协议、结果覆盖和输出产物摘要。

已完成：

1. 补齐安装证据快照中的结果覆盖展示：
   - 从 `test_evidence.resultCoverage` / `test_evidence.result_coverage` 读取覆盖证据；
   - 显示 primary result、已覆盖类型数、缺口类型数；
   - 当 `ok=true` 且没有明细 covered 列表时，显示 `ok`，避免误写为“已覆盖: 0”；
   - 覆盖失败或存在 missing 类型时进入 failed 视觉状态，覆盖通过进入 ready 状态。
2. 补齐本机提交队列的审核证据条：
   - 消费 `review_evidence.result_contract_primary` / `result_contract_type_count`；
   - 消费 `test_protocol_fingerprint`；
   - 消费 `result_coverage_ok` / `result_coverage_primary` / `result_coverage_missing_count`；
   - 消费 `output_count` / `artifact_count`，展示输出结果与输出产物数量。
3. 回归确认已安装 App 管理详情和提交队列两条 GUI 路径都能看到 result/test/coverage/output 证据。

验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "shows local app package submission queue summaries|restores single app install evidence from top-level market install records"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

结果：
- 定向前端测试通过：2 passed / 199 skipped。
- `AppsPage.test.tsx` 全量通过：201 passed。

对应全链路意义：

```text
App Studio 设计 / 测试 / 提交审核
  -> 后端 summary 生成 review_evidence
  -> Hub 审核侧显示 result/test/coverage/output
  -> GUI 本地提交队列同步显示同一组审核证据
  -> Hub 安装后的 install evidence 回填本地 App 管理详情
  -> 用户在安装后仍可回看结果契约、测试协议、覆盖状态和输出产物摘要
```

当前剩余缺口继续聚焦：
- App Studio 创建、动态布局调整、保存、测试、提交、Hub 审核发布、市场安装、运行的 UI 级全链路点击流仍需拆成稳定验收。
- DataSrv / Hub / GUI 字段合同需要最后审计，尤其是 `review_evidence`、`install_evidence`、`result_contract`、`test_evidence`、`result_coverage` 的跨端命名一致性。
- 最终收口前需要清点临时测试输出文件，避免过程产物混入提交范围。
### 推进记录：Hub 安装到运行链路补齐 result/test evidence UI 验收（2026-06-30）

本轮继续把全链路 UI 验收往前推进，不新增孤立路径，而是在既有“Enterprise Hub 搜索安装审批型 MaClaw App -> 回到管理列表 -> 打开已安装 App -> 处理 requires_input 审批实例”的主验收里补齐 result/test/coverage/output 证据断言。

已完成：

1. 增强 Hub 审批型 App 安装夹具：
   - 安装证据包含 `result_contract.primary/types`；
   - 测试证据包含 `test_protocol_fingerprint`；
   - 测试证据包含 `result_coverage.ok/primary/covered_types/missing_types`；
   - 测试证据包含顶层 `outputs/artifacts`，用于安装结果行的输出产物摘要。
2. 增强市场安装 UI 验收：
   - 安装完成后，市场行内证据快照必须显示 Result contract；
   - 必须显示测试运行与协议指纹；
   - 必须显示 Result coverage，且覆盖 `approval_result/artifact/content` 三类结果；
   - 必须显示 Output / Output artifacts 数量。
3. 保留同一测试后半段的运行态验收：
   - 关闭 Studio 后从主应用面板打开已安装审批型 App；
   - 从 DataSrv 拉取 requires_input 实例；
   - 显示缺失字段 `signed_contract`；
   - 用户填写补充说明和材料引用；
   - 续跑时保留同一 `approval_id`、`workflow_instance_id`、DataSrv 记录、workflow Skill 版本和 `result_payload.supplemental_input`。

验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "installs approved Hub MaClaw Apps from market search results"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "installs approved Hub MaClaw Apps from market search results|continues a requires-input approval instance"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

结果：
- Hub 安装主链路 targeted 通过：1 passed / 200 skipped。
- Hub 安装 + requires_input 续跑组合 targeted 通过：2 passed / 199 skipped。
- `AppsPage.test.tsx` 全量通过：201 passed。

对应全链路意义：

```text
Enterprise Hub 搜索审批型 MaClaw App
  -> 一键安装并完成 Skill 依赖检查
  -> GUI 安装结果行显示 layout / dependency / result contract / test protocol / coverage / output evidence
  -> 本地 App 管理列表保留安装证据和动态布局
  -> 打开已安装审批型 App
  -> DataSrv 审批实例工作台显示 requires_input
  -> 用户补充材料并续跑同一审批实例
  -> workflow Skill 继续运行并保留补充输入审计
```

当前剩余缺口继续聚焦：
- App Studio 从零创建审批型 App、视觉调整布局、运行测试、提交审核、Hub 审核发布的点击流还需要拆成更稳定的前端验收链，而不是只依赖若干分段测试。
- 需要做跨端字段合同终审：GUI / DataSrv / Hub / HubCenter 对 `review_evidence`、`install_evidence`、`result_contract`、`test_evidence`、`result_coverage`、`approval_instance` 的命名是否完全一致。
- 最终收口前清理临时测试输出文件，并跑后端关键组合测试与 Hub admin 静态校验。
### 推进记录：result coverage covered count 跨端字段合同补齐（2026-06-30）

本轮做 `review_evidence` / `install_evidence` 字段合同终审中的一个实质收口：此前 result coverage 证据能表达 `ok`、primary result 和 missing count，但审核摘要缺少 covered count。这样 Hub/GUI 审核侧只能看到“覆盖通过”或“缺口数”，不能看到测试实际覆盖了多少类声明结果。现在后端生产、GUI 消费、Hub 审核展示、HubCenter 审核展示和静态校验都补齐了 covered count。

已完成：

1. 后端 App Studio 提交审核摘要补齐：
   - `maclawAppSubmissionReviewEvidenceForRecord` 新增 `result_coverage_covered_count`；
   - 字段来自 `testEvidence.resultCoverage.coveredTypes` / `covered_types`；
   - 与既有 `result_coverage_ok`、`result_coverage_primary`、`result_coverage_missing_count` 形成完整覆盖摘要。
2. 后端 Hub/市场能力元数据补齐：
   - 生成 `test_evidence_result_coverage_covered_count`；
   - 与 `test_evidence_covered_types`、`test_evidence_result_coverage_ok`、`test_evidence_result_coverage_primary` 同步落入 metadata。
3. GUI App Studio / 本地提交队列消费补齐：
   - `resultCoverageEvidenceSummary` 支持 `coveredCount` / `covered_count`；
   - `publishReviewEvidenceItems` 消费 `result_coverage_covered_count` / `resultCoverageCoveredCount`；
   - 提交队列 review evidence 现在显示 `approval_result · 已覆盖: 3`，而不是只能显示 `ok`。
4. Hub / HubCenter 审核展示补齐：
   - Enterprise Hub marketplace MaClaw App 摘要 Coverage 行显示 `covered N`；
   - HubCenter review evidence chip 显示 covered count；
   - admin 静态校验 markers 增加 `result_coverage_covered_count` / `coverageCovered`，防止后续回退。

验证：

```powershell
cd D:\workprj\aicoder
gofmt -w gui\app_maclaw_apps.go gui\app_maclaw_apps_test.go
go test ./gui -count=1 -vet=off -run "TestMaclawAppSubmissionSummaryIncludesReviewEvidence|TestSubmitMaclawAppPackageQueuesLocalSubmission|TestInstallSelectedMaclawAppPackageFromHubRecordsInstallEvidence" -timeout 240s
rg -n "result_coverage_covered_count|test_evidence_result_coverage_covered_count|coverageCovered" hub\web\admin\marketplace-tab.js hubcenter\web\admin\assets\js\skillmarket-admin.js hub\web\admin\validate-admin-modules.js gui\app_maclaw_apps.go gui\frontend\src\components\pages\AppsPage.tsx

git diff --check -- gui\app_maclaw_apps.go gui\app_maclaw_apps_test.go gui\frontend\src\components\pages\AppsPage.tsx gui\frontend\src\components\pages\__tests__\AppsPage.test.tsx hub\web\admin\marketplace-tab.js hubcenter\web\admin\assets\js\skillmarket-admin.js hub\web\admin\validate-admin-modules.js

cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "shows local app package submission queue summaries|installs approved Hub MaClaw Apps from market search results"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

结果：
- 后端定向测试通过。
- 前端 evidence / Hub 安装 targeted 通过：2 passed / 199 skipped。
- `AppsPage.test.tsx` 全量通过：201 passed。
- `rg` marker 验证通过，字段出现在后端生产、GUI 消费、Hub 展示、HubCenter 展示和 admin 校验脚本中。
- `git diff --check` 通过；仅提示部分 JS/文档文件未来 Git 触碰时 CRLF 会转 LF。
- `node hub\web\admin\validate-admin-modules.js` 本轮因 Windows sandbox 进程创建层 `helper_unknown_error: setup refresh had errors` 未能启动；不是脚本断言失败。已用 marker 验证覆盖新增字段，后续环境恢复后仍需补跑该校验。

对应全链路意义：

```text
App Studio 运行测试形成 resultCoverage.coveredTypes/missingTypes
  -> SubmitMaclawAppPackage 生成 review_evidence.result_coverage_covered_count
  -> Hub / HubCenter 审核侧显示 covered N + missing N
  -> GUI 本地提交队列显示已覆盖类型数
  -> 安装后 install evidence 和 runtime 回看仍能显示完整 coverage 明细
```

当前剩余缺口继续聚焦：
- 环境恢复后补跑 `node hub\web\admin\validate-admin-modules.js`。
- 继续审计 `approval_instance`、`test_evidence`、`install_evidence` 在 DataSrv 安装回填、Hub 发布、GUI 冷启动恢复三条路径中的字段命名一致性。
- 将 App Studio 从零创建、视觉布局调整、测试、提交、审核发布的点击流继续拆成稳定的前端验收链。
### 推进记录：DataSrv 安装回填 coverage count 与 workflow instance ID 合同收口（2026-06-30）

本轮继续审计 `install_evidence` / `test_evidence` / `approval_instance` 的跨端字段合同，重点补齐 DataSrv 已安装 MaClaw App 回填路径。上一轮后端和 Hub 审核摘要已经能产出 `result_coverage_covered_count`，但 GUI 从 DataSrv metadata 合成 installed evidence 时还没有消费该摘要字段；同时审批实例回填需要明确区分 `approval_id` 和 `workflow_instance_id`，避免把审批业务 ID 当成 workflow instance ID。

已完成：

1. GUI DataSrv installed coverage 合成补齐计数字段：
   - `dataSrvInstalledResultCoverageEvidence` 支持 `metadata.test_evidence_result_coverage_covered_count`；
   - 同时支持 `metadata.test_evidence_result_coverage_missing_count`；
   - 当 DataSrv metadata 只提供摘要计数、没有 `coveredTypes` / `missingTypes` 列表时，仍能合成 `result_coverage.covered_count` / `missing_count`；
   - imported run evidence 和 install evidence 都能回看该覆盖摘要。
2. DataSrv 已安装审批型 App 的审批实例字段合同加固：
   - 测试夹具加入 `workflow_instance_id`；
   - GUI 归一化后保留 `instanceId = workflow_instance_id`；
   - 同时保留 `approvalID = approval_id`；
   - progress instance 也保留同一 `workflow_instance_id`，避免进度回看和最终实例断链。
3. 前端测试覆盖两类 DataSrv 安装回填：
   - 企业普通 App：metadata 只有 coverage 摘要计数，恢复为 `resultCoverage.covered_count/missing_count`；
   - 企业审批型 App：恢复 workflow instance ID、approval ID、progress instances、动态布局、依赖证据、结果包和运行契约。

验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "restores DataSrv installed enterprise normal app run evidence|turns DataSrv installed MaClaw apps into addable app candidates"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "turns DataSrv installed MaClaw apps into addable app candidates"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx

cd D:\workprj\aicoder
git diff --check -- gui\frontend\src\components\pages\AppsPage.tsx gui\frontend\src\components\pages\__tests__\AppsPage.test.tsx docs\mis-end-to-end-refactor-plan-zh.md
rg -n "test_evidence_result_coverage_covered_count|workflow_instance_id: 'wf-remote-imported'|covered_count: 3|workflowInstanceId: 'wf-remote-imported'" gui\frontend\src\components\pages\AppsPage.tsx gui\frontend\src\components\pages\__tests__\AppsPage.test.tsx
```

结果：
- DataSrv installed targeted 组合通过：2 passed / 199 skipped。
- DataSrv 审批型安装回填 targeted 通过：1 passed / 200 skipped。
- `AppsPage.test.tsx` 全量通过：201 passed。
- `git diff --check` 通过；仅提示文档 CRLF 未来会转 LF。
- `rg` marker 验证通过。
- `node hub\web\admin\validate-admin-modules.js` 仍因 Windows sandbox 进程创建层 `helper_unknown_error: setup refresh had errors` 无法启动；前端 `npm.cmd test` 可运行，说明本轮 GUI 验证不受影响。

对应全链路意义：

```text
DataSrv app_installations.metadata
  -> test_evidence_result_coverage_covered_count / missing_count
  -> GUI 合成 installEvidence.test_evidence.result_coverage
  -> importedRunEvidence.resultCoverage 可回看覆盖规模
  -> approval_instance.workflow_instance_id 与 approval_id 分离保存
  -> 已安装审批型 App 运行态继续使用正确 DataSrv app_id 和 workflow instance ID
```

当前剩余缺口继续聚焦：
- 继续审计 Hub 发布包、DataSrv 安装登记、GUI 冷启动恢复三条路径里 `approval_instance` / `install_evidence` 是否还有命名不一致。
- 环境恢复后补跑 Hub admin node 校验。
- 继续拆 App Studio 从零创建、视觉布局调整、测试、提交、审核发布的 UI 点击流验收。

### 推进记录：App Studio 从零创建到测试提交审核 UI 点击流验收（2026-06-30）

本轮把上一段“App Studio 从零创建审批型 App、视觉调整布局、测试、提交审核”的缺口推进成可执行的前端端到端验收。新增用例覆盖：

1. App Studio 创建企业审批型应用，选择 App Skill 与审批 workflow Skill，并配置审批事件、对象角色和依赖安装引用；
2. 在可视化布局设计器中调整 `approval_workspace` 的 template、density、primaryRegion、outputRegion 和 result panel placement，保存到超级 Skill 的 `maclaw.app.json`；
3. 保存后的 Skill 型 MaClaw App 继续保留 `studioOrigin=app_studio`，因此可进入本机 Review / publish 审核候选，而不会被 `source=skill` 过滤掉；
4. 运行审批 workflow 后，回填审批实例、workflow metadata、result payload、outputs、artifacts、result coverage 和 dependency verification；
5. 提交审核包断言 `binding.appSkill`、`dependencies.skills.install_ref`、`binding.mis.approvalBindings`、动态 UI layout、workspaceLayout governance、resultContract、testProtocol、testEvidence.approvalInstance、outputs/artifacts 均进入 package payload。

本轮同时修正测试辅助依赖验证数据：`testDependencyVerificationForApp` 保留 `install_ref / installRef`，使发布门禁能验证 workflow Skill 的安装来源，而不是只按 skill id 粗略匹配。发布面板候选规则也收紧为 `source=local` 或 `studioOrigin=app_studio`，避免普通自动发现的已安装 Skill App 被误列为本地待提交应用。

验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "creates, tests, and submits an enterprise approval app from App Studio"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

结果：目标用例通过；`AppsPage.test.tsx` 全量 `202 passed`。

当前剩余缺口继续聚焦：

- 继续把 Hub 审核发布、市场安装、依赖下载/验签、DataSrv 登记、运行审批、审批中心回看结果拆成稳定的 UI/集成验收链；
- 对 DataSrv、GUI 后端、Hub/HubCenter 继续做一次字段契约巡检，确认 App Studio 生成的 `studioOrigin` 只影响本机发布候选，不进入 Hub 安装身份语义；
- 在更接近真实用户操作的黄金样例中复核审批通过、拒绝、需关注、待补充、取消、超时等结果态是否都能从 workflow runner 进入 DataSrv 和运行态回看。

### 推进记录：Hub 审核通过安装后运行态证据回灌（2026-06-30）

本轮继续推进 App Studio 提交审核之后的后半段闭环：Hub 审核通过的 MaClaw App 从本机发布队列直接安装后，安装证据不能只停留在队列行的临时结果里，必须回灌到真正加入应用面板的 AppEntry，供用户关闭 App Studio 后继续运行、复测、诊断和二次发布。

修复点：

1. `installApprovedHubApp` 现在复用市场安装路径已有的 `installedAppWithInstallEvidence`，把 `InstallMaclawAppPackageFromHub` 返回的 `install_record` 合成到每个已安装 App：
   - `versionSnapshot`：app entry version、app_skill、workflow_skill、approval binding；
   - `installEvidence`：workspace layout、result contract、workflow contract、test evidence、dependency verification、DataSrv registration；
   - `workflowContract`：审批运行合同；
   - `importedRunEvidence`：由安装测试证据恢复出的 approvalInstance、resultPayload、outputs、artifacts。
2. 新增前端组合验收：Hub 审核队列中 `approved` 的企业审批型 App，点击 `Install approved app` 后：
   - 本地 `maclaw:apps-panel:v1` 中的 `market-*` App 保留审批 workflow 依赖、DataSrv 登记状态、审批实例结果包和文件产物；
   - 关闭 App Studio 打开运行态，能看到版本快照、运行合同、安装测试证据、审批实例摘要、输出块和附件名。

验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "installs an approved Hub approval app with runtime install evidence"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

结果：目标用例通过；`AppsPage.test.tsx` 全量 `203 passed`。

当前剩余缺口继续聚焦：

- Hub 审核发布本身仍主要通过队列/夹具验证，下一步需要把审核端状态流转、发布后的能力市场详情、真实下载包和依赖验签安装串成更接近真实用户操作的验收；
- 继续把 DataSrv 登记后的审批运行、审批中心“我的申请/我审批的/已处理/需关注”、以及审批通过/拒绝/需关注/待补充/取消/超时结果态放进同一条 golden path，而不只依赖分段测试。

### 推进记录：Hub 安装审批实例进入运行态审批工作台 lane 回看（2026-06-30）

本轮继续收口 Hub 审核通过安装后的运行态回看链路。此前安装证据已经能回灌到 AppEntry，并在运行态详情头部展示版本快照、workflow contract、测试证据和审批实例摘要；但还需要证明这些审批实例不是只停留在证据卡里，而是能进入用户日常处理的审批工作台 lane。

已完成：

1. GUI 运行态审批实例种子新增 lane 推断规则：
   - 如果安装/导入证据显式提供 `lane`，继续尊重原值；
   - `approved` / `rejected` / `failed` / `cancelled` / `timeout` 自动进入 `handled`；
   - `attention` 自动进入 `attention`；
   - `requires_input`、`pending`、`running` 等非终态默认留在 `my_requests`。
2. 扩展 Hub 审核通过安装的前端组合验收：
   - Hub review queue 中 approved 的企业审批型 App 点击 `Install approved app`；
   - 安装记录回灌 `versionSnapshot`、`installEvidence`、`workflowContract`、`importedRunEvidence`；
   - 关闭 App Studio 后打开已安装 App；
   - 在运行态审批工作台点击 `Handled` lane；
   - 断言 `EXP-HUB-APPROVED-1`、`wf-approved-approval-1`、`Approved expense`、`approved-approval.pdf` 均在审批工作台内可见。
3. 测试夹具不再依赖该 Hub 安装实例显式写入 `lane`，用例覆盖的是状态推断进入 `handled` 的真实恢复语义。

验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "installs an approved Hub approval app with runtime install evidence"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

结果：
- Hub 审核安装运行态证据 targeted 用例通过：1 passed / 202 skipped。
- `AppsPage.test.tsx` 全量通过：203 passed。

对应全链路意义：

```text
Hub 审核通过的企业审批型 MaClaw App
  -> GUI 安装并回灌 install evidence / approval_instance
  -> 运行态恢复审批实例种子
  -> 按审批状态推断进入 my_requests / handled / attention lane
  -> 用户在审批工作台已处理 lane 回看审批结果包、业务记录和文件产物
```

当前剩余缺口继续聚焦：
- Hub 审核发布状态流转、发布后能力市场详情、真实下载包和依赖验签安装还需要串成更接近真实用户操作的 UI/集成验收；
- 全局“审批状态”中心还需要和单 App 运行态工作台做一条组合回看验收，确认我的申请、待我审批、已处理、需关注在跨 App 视角一致；
- 最终收口前继续做 DataSrv / GUI / Hub / HubCenter 字段合同终审，并清理临时测试输出文件。

### 推进记录：全局审批状态中心合并本地安装审批实例 seed（2026-06-30）

本轮继续把单 App 运行态审批工作台和左侧全局“审批状态”中心打通。此前 Hub 安装后的审批实例已经能在单 App 运行态 `Handled` lane 回看，但全局审批中心只读取 `ListMaclawAppApprovalInstancesAll('all', 200)` 返回值；如果用户刚从 Hub 安装 App，DataSrv/后端全局列表尚未回流该测试审批实例，全局视角会短暂缺失同一条审批证据。

已完成：

1. `ApprovalManager` 新增本地 seed 合并：
   - 从所有企业审批型 App 的 `importedRunEvidence.approvalInstance` 和 `installEvidence.test_evidence.approval_instance` 生成本地审批实例；
   - 加载全局后端列表成功时，使用 `mergeApprovalInstanceViews(localSeedInstances, remoteViews)` 合并，后端/远端记录优先，本地 seed 作为补充；
   - 后端全局列表加载失败但本地 seed 存在时，仍展示本地审批实例，避免刚安装的 App 在全局审批中心消失。
2. 扩展 Hub 审核通过安装组合验收：
   - 先在单 App 运行态审批工作台点击 `Handled` lane，验证审批实例、输出和附件可见；
   - 再点击左侧 `Approval status` 打开全局审批中心；
   - 后端全局审批列表 mock 返回空数组；
   - 全局中心仍能从本地安装证据显示 `EXP-HUB-APPROVED-1`、`wf-approved-approval-1`、`Approved expense`、`approved-approval.pdf`。

验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "installs an approved Hub approval app with runtime install evidence"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

结果：
- Hub 审核安装 + 单 App/全局审批中心回看 targeted 用例通过：1 passed / 202 skipped。
- `AppsPage.test.tsx` 全量通过：203 passed。

对应全链路意义：

```text
Hub 安装企业审批型 MaClaw App
  -> install evidence 回灌到本地 AppEntry
  -> 单 App 运行态审批工作台可回看 handled 审批结果
  -> 全局审批状态中心合并本地 seed 与后端全局列表
  -> DataSrv/后端回流前也能跨 App 回看审批实例、结果包和附件
```

当前剩余缺口继续聚焦：
- Hub 审核发布状态流转、发布后能力市场详情、真实下载包和依赖验签安装仍需更接近真实操作的 UI/集成验收；
- DataSrv / GUI / Hub / HubCenter 字段合同需要最终巡检，尤其是 `approval_instance`、`install_evidence`、`test_evidence` 与 `result_contract` 的命名一致性；
- 收口前清理临时测试输出文件，并补跑后端关键组合测试与 Hub admin 静态校验。
### 推进记录：Hub 市场搜索结果安装前审核证据详情可见（2026-06-30）

本轮继续推进“Hub 审核发布 -> 能力市场详情 -> 真实下载包安装”的 UI 级验收。此前 Hub/HubCenter 审核侧已经能显示 review evidence，GUI 市场安装后也能显示 install evidence；但用户在 GUI 的 “Add from market” 搜索结果里，安装前还只能看到名称、分类和来源，缺少审核/测试摘要，无法在点击安装前判断该 App 是否经过结果契约、测试协议、覆盖率、输出附件和审批实例验证。

已完成：

1. GUI 市场搜索结果支持携带 `marketReviewEvidence`：
   - 从 `SearchMixedSkills` 返回的 `review_evidence` / `reviewEvidence` / `maclaw_app_review_evidence` 或 `metadata.review_evidence` 中提取当前 App 的审核证据；
   - 支持 `{appID: evidence}`、`{market-appID: evidence}`、`{appName: evidence}` 和单条扁平 evidence；
   - 该字段只用于市场预览，不进入安装身份语义；真正安装仍以 Hub 下载包和 `install_record` 为准。
2. 市场行复用 `PublishReviewEvidenceStrip`：
   - 安装前可见 Approval、Test evidence、Result coverage、Result package 等审核摘要；
   - `PublishReviewEvidenceStrip` 同时兼容映射型 evidence 和扁平 evidence；
   - Test evidence 值增强为 `run_id · test_protocol_fingerprint`，和 Hub 审核侧的信息量对齐。
3. 保持安装后证据区分：
   - 市场行可同时展示安装前 review evidence 和安装后的 install evidence；
   - 测试断言显式排除 `.apps-review-evidence-strip`，继续验证真正的 install evidence snapshot。
4. 扩展市场安装组合验收：
   - `SearchMixedSkills` 返回带 `review_evidence` 的 Hub MaClaw App；
   - GUI 搜索结果安装前显示 `run-market-contract-review · proto-market-contract-review`、`approval_result · Covered: 3`、`Output: 1 · Output artifacts: 1`、`approved · contract.result_feedback`；
   - 点击 `Add` 后仍走真实 `InstallSelectedMaclawAppPackageFromHub(capabilityID, selectedAppIDs)`；
   - 安装后继续验证 dependency plan、install evidence、DataSrv registration、审批实例和文件产物回灌。

验证：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "installs approved Hub MaClaw Apps from market search results"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx
```

结果：
- Hub 市场搜索安装 targeted 用例通过：1 passed / 202 skipped。
- `AppsPage.test.tsx` 全量通过：203 passed。

对应全链路意义：

```text
Hub/HubCenter 审核形成 review_evidence
  -> GUI Add from market 搜索结果读取 Hub 能力详情
  -> 安装前显示审核/测试/覆盖/输出/审批摘要
  -> 点击 Add 走 Hub 真实下载包 + 选择子 App + 依赖验签安装
  -> 安装后显示 install evidence 并回灌运行态
```

当前剩余缺口继续聚焦：
- Hub 审核发布状态流转本身仍需要更接近真实操作的 UI/集成验收，尤其是从 submitted -> approved/published -> marketplace searchable 的状态串联；
- DataSrv / GUI / Hub / HubCenter 字段合同需要最终巡检，确认 review_evidence 与 install_evidence 的字段命名在跨端一致；
- 收口前清理临时测试输出文件，并补跑后端关键组合测试与 Hub admin 静态校验。
### 推进记录：Hub 审核发布状态流转到市场安装验收（2026-06-30）

本轮继续补齐 App Studio 提交审核后的 Hub 发布状态闭环。此前已经能展示本机提交队列、同步到 Hub、刷新 Hub 审核状态，以及从市场搜索结果安装已发布 MaClaw App；但缺少一条把这些动作串起来的用户级验收，无法证明 `submitted -> pending_review -> published -> marketplace searchable -> install` 是同一个 App/capability 的连续链路。

本轮完成：

1. 新增前端验收 `syncs a local submission to Hub, refreshes published status, then installs it from market search`：
   - 本机提交队列显示 `local-review-flow`，状态为 `submitted/local`；
   - 点击 `Sync to Hub` 后调用 `SyncMaclawAppPackageSubmissionToHub('local-review-flow')`，队列更新为 `hub-review-flow / pending_review / hub`；
   - 点击 `Refresh Hub Status` 后调用 `RefreshMaclawAppPackageSubmissionFromHub('hub-review-flow')`，队列更新为 `published`，并显示审核证据、结果覆盖、输出/附件摘要；
   - 切到 `Add from market`，Hub 搜索返回同一个 `cap-flow-approval-app`，市场行安装前显示同一份 review evidence；
   - 点击安装时调用 `InstallSelectedMaclawAppPackageFromHub('cap-flow-approval-app', ['market-flow-approval-app'])`，而不是退回本地粘贴包安装或绕过 Hub 下载包。

2. 修复 Hub 市场安装后的本地 AppEntry 身份回灌：
   - 从 Hub 下载包解析出的 AppEntry 现在继承市场候选的 `marketCapabilityID`、`marketInstallSource`、`marketSourceLabel`；
   - 这样安装后的 App 能继续被 Hub governance 队列按 capability id 匹配，支持后续 revoked/republished 状态同步；
   - 同时不改变 install evidence、workflow contract、approval instance、result package 的回灌路径。

验证结果：

- 新增 Hub 状态流转到市场安装 targeted 用例通过：1 passed / 203 skipped。
- 既有 Hub 市场搜索安装 targeted 用例通过：1 passed / 203 skipped。
- 既有 Hub 审核队列安装审批型 App runtime evidence targeted 用例通过：1 passed / 203 skipped。

对应全链路意义：

```text
App Studio 提交本机队列
  -> 同步 Hub 审核队列
  -> 刷新为 published
  -> 能力市场搜索到同一 capability
  -> 安装时走 Hub 下载包 + 依赖安装/验签 + install evidence 回灌
  -> 本地 App 保留 Hub capability 身份，后续可继续接收治理状态
```

当前剩余缺口继续聚焦：

- 继续把这条前端 UI 验收和后端 Hub 审核接口、Hub capability metadata/package 下载接口做更真实的集成串联，减少纯 mock 比例；
- 继续补一条跨 DataSrv + Hub + GUI 的最终黄金样例：制作审批型 App、测试、上传审核、发布、下载安装依赖、发起审批、running 节点同步、审批完成、单 App/全局审批中心结果和文件回读。
### 推进记录：Hub review evidence 后端保真与 GUI refresh 持久化（2026-06-30）

本轮把上一段前端 UI 状态流转继续下压到 Hub/GUI 后端字段契约。此前 Hub 提交、审核、发布和下载包已经保留原始 manifest、测试证据和安装签名，但 `review_evidence` 主要依赖 GUI 本地队列从 package 重新派生；如果 Hub 审核侧对证据做了规范化或补充，GUI refresh 后无法把这份 Hub 侧审核证据持久回本机队列。

本轮完成：

1. Hub 提交入库时生成稳定 `review_evidence` / `maclaw_app_review_evidence`：
   - 以 App ID 为 key；
   - 聚合 run_id、test_protocol_fingerprint、result contract primary/type count、result coverage primary/covered/missing、output/artifact count、approval_status/current_node；
   - 继续保留原始 `test_evidence`，review evidence 只是审核/市场/GUI 预览用摘要，不替代安装包本体。

2. Hub 下载 published MaClaw App 包时保留审核证据：
   - 顶层 `maclaw.app.pack.v1.review_evidence` 暴露同一份审核摘要；
   - entry 内 `app.governance.submission.review_evidence` / `maclaw_app_review_evidence` 同步保留，随原始 app manifest 一起进入安装包；
   - 下载包仍只允许 `published` 状态，并继续携带 package_signature、resolved_dependencies、原始动态 UI、workflow/result/test evidence。

3. GUI 后端 refresh 持久化 Hub review evidence：
   - `maclawAppSubmissionRecord` 新增 `review_evidence`；
   - `RefreshMaclawAppPackageSubmissionFromHub` 从 Hub capability metadata 读取 review_evidence 并写入本机 durable queue；
   - `ListMaclawAppPackageSubmissions` summary 优先返回 Hub 持久化 review_evidence，缺失时再从本地 package 派生；
   - `GetMaclawAppPackageSubmission` 会克隆返回 review_evidence，避免详情调用修改内存副本。

验证结果：

- Hub handler 强制定向测试通过：`go test ./hub/internal/httpapi -run "TestCapabilityMaclawAppSubmit|TestCapabilityMaclawAppPackageHandler" -count=1`。
- GUI refresh 定向测试通过：`go test ./gui -run "TestRefreshMaclawAppPackageSubmissionFromHubUpdatesReviewState"`。

对应全链路意义：

```text
App Studio 提交包
  -> Hub 从完整 test evidence 派生 review_evidence
  -> Hub 审核/发布保留 review_evidence
  -> published package 下载时顶层和 entry submission 都携带 review_evidence
  -> GUI refresh Hub 状态后持久化 review_evidence
  -> 市场搜索、提交队列、安装审计可消费同一份审核摘要
```

当前剩余缺口继续聚焦：

- 把 Hub approve/publish 管理端点击流、GUI refresh、市场搜索安装放进一条更接近真实服务的组合验收；
- 继续推进最终黄金样例：App Studio 制作审批型 App、测试、上传审核、发布、下载安装依赖、DataSrv 注册、发起审批、running 节点同步、审批完成、单 App/全局审批中心结果和文件回读。
### 推进记录：Hub review_evidence 进入安装审计与 DataSrv 注册（2026-06-30）

本轮继续收口 MaClaw App 从 Hub 审核发布到企业本地安装运行的证据链。此前 Hub 侧已经能在提交、审核、发布和包下载阶段保留 `review_evidence`，GUI 刷新 Hub 状态后也能把审核证据写回本机提交队列；但安装后的本地审计记录和 DataSrv app-installations 注册 metadata 仍主要依赖 `test_evidence` / `install_evidence`，缺少对 Hub 审核摘要的直接回溯。

本轮完成：

1. GUI backend 安装审计记录 `maclawAppInstallRecord` 新增 `review_evidence` 字段。
2. 新增 `maclawAppReviewEvidenceForEntry`，从 `app.governance.submission.review_evidence` / `reviewEvidence` / `maclaw_app_review_evidence` 中提取当前 App 的 Hub 审核摘要，兼容按 app id/name 分组和扁平 evidence。
3. `RecordMaclawAppInstall` 在本地安装 registry 中持久化 Hub review evidence。
4. `maclawAppInstallEvidenceByApp` 在每个 App 的 `install_evidence` 中附带 `review_evidence`，让市场安装结果、GUI 详情和后续运行审计都能看到同一份审核摘要。
5. `maclawAppDataSrvInstallationPayloads` 在 DataSrv `/api/v1/data/app-installations/{app_id}` 注册 metadata 中同步写入 `review_evidence`，便于 DataSrv 侧追踪该企业应用安装时对应的 Hub 审核证据。
6. 扩展 `TestInstallSelectedMaclawApprovalAppFromHubPreservesApprovalEvidence`，覆盖：
   - Hub package entry 内带 `governance.submission.review_evidence`；
   - 安装返回的 `install_evidence.review_evidence` 保留 run、审批状态和当前节点；
   - DataSrv 注册 metadata 保留 review evidence；
   - 本地安装审计 `ListMaclawAppInstalls` 保留 review evidence。

验证命令：

```powershell
go test ./gui -run TestInstallSelectedMaclawApprovalAppFromHubPreservesApprovalEvidence -count=1
go test ./gui -run "TestRefreshMaclawAppPackageSubmissionFromHubUpdatesReviewState|TestMaclawAppSubmissionSummaryIncludesReviewEvidence" -count=1
git diff --check -- gui/app_maclaw_apps.go gui/app_maclaw_apps_test.go
```

当前剩余缺口仍集中在真实跨服务黄金路径：需要继续把 App Studio 本地制作/试运行、提交 Hub、Hub 审核发布、GUI 刷新 published 状态、市场安装、依赖 Skill 检查/安装、DataSrv 安装登记、审批 workflow 启动、节点状态同步、结果回写和全局审批视图读回串成一个更完整的集成测试或脚本化验收场景。
### 推进记录：Hub 发布包显式携带 review_evidence（2026-06-30）

上一轮 GUI 安装侧已经可以从 `app.governance.submission.review_evidence` 读取 Hub 审核证据，并写入安装审计、`install_evidence` 和 DataSrv app-installations metadata。本轮继续补齐 Hub 输出端，避免安装侧只能消费手写或模拟 package 中的审核证据。

本轮完成：

1. `CapabilityMaclawAppPackageHandler` 返回的 `maclaw.app.pack.v1` 顶层新增：
   - `review_evidence`
   - `maclaw_app_review_evidence`
2. `applyMaclawAppReviewMetadataToEntry` 将 Hub capability metadata 中的 `review_evidence` / `maclaw_app_review_evidence` 合并进 `app.governance.submission`。
3. 扩展 `TestCapabilityMaclawAppPackageDownloadReturnsApprovedPack`，验证：
   - 下载包顶层暴露 Hub review evidence；
   - entry 内 `governance.submission.review_evidence` 保留同一份审核证据；
   - 现有 package signature、resolved dependencies、workspace layout、workflow contract、test evidence、approval instance 断言继续成立。

验证命令：

```powershell
go test ./hub/internal/httpapi -run TestCapabilityMaclawAppPackageDownloadReturnsApprovedPack -count=1
```

这使链路更接近真实生产路径：

```text
Hub 审核/发布 metadata.review_evidence
  -> package download 顶层 review_evidence
  -> entry.governance.submission.review_evidence
  -> GUI InstallSelectedMaclawAppPackageFromHub
  -> install_evidence.review_evidence / install registry review_evidence
  -> DataSrv app-installations.metadata.review_evidence
```
### 推进记录：安装后保留 Hub submission 身份（2026-06-30）

本轮继续补齐 Hub 发布身份在企业本地安装后的可追踪性。此前安装侧已经能保留 Hub `review_evidence`，但 DataSrv app-installations metadata 和本地安装审计缺少稳定的 Hub capability/version 摊平字段；企业后端可以看到证据，却不容易直接反查该安装对应哪个 Hub capability、哪个 version key、哪个审核状态。

本轮完成：

1. GUI backend 新增 `maclawAppSubmissionMetadataForEntry`，统一从 `app.governance.submission` 提取 Hub submission 身份。
2. `maclawAppInstallEvidenceByApp` 在每个 App 的 `install_evidence` 中新增 `submission`，保留：
   - `capability_id`
   - `market_capability_id`
   - `submission_id`
   - `version_key`
   - `status`
   - `package_sha256`
3. `maclawAppInstallRecord` 新增 `submission` 字段，本地安装审计可直接回溯 Hub 发布身份。
4. `maclawAppDataSrvInstallationPayloads` 在 DataSrv app-installations metadata 中写入完整 `submission`，并摊平关键字段：
   - `hub_capability_id`
   - `hub_market_capability_id`
   - `hub_submission_id`
   - `hub_version_key`
   - `hub_review_status`
   - `hub_package_sha256`
5. 扩展 `TestInstallSelectedMaclawApprovalAppFromHubPreservesApprovalEvidence`，验证 Hub submission 身份进入：
   - `install_evidence.submission`
   - DataSrv registration metadata
   - 本地安装 registry audit record

验证命令：

```powershell
go test ./gui -run TestInstallSelectedMaclawApprovalAppFromHubPreservesApprovalEvidence -count=1
go test ./gui -run "TestMaclawAppInstallEvidenceGeneratesDependencyVerification|TestInstallSelectedMaclawAppPackageFromHubFiltersPackageApps" -count=1
```

当前链路进一步变为：

```text
Hub package entry.governance.submission
  -> GUI install_evidence.submission
  -> 本地 maclaw app install registry submission
  -> DataSrv app-installations.metadata.submission / hub_* identity fields
```
### 推进记录：安装证据中的 Hub 身份进入应用面板可见层（2026-06-30）

本轮把上一段后端已经保留下来的 Hub `submission` / `review_evidence` 继续接到 GUI 应用面板。目标是让企业用户不只在本地审计和 DataSrv metadata 中能追溯安装来源，也能在已安装应用的运行详情里直接看到该 App 来自哪个 Hub capability、哪个 version key，以及安装时对应的审核状态。

本轮完成：
1. GUI 前端 `BackendAppInstallRecord` 消费 `submission` / `review_evidence`，并在 `InstallRecordEvidenceSnapshot` 中新增 Hub 来源摘要：审核状态、capability id、version key 或 submission id。
2. `dataSrvInstalledInstallEvidence` 从 DataSrv `app_installations.metadata` 恢复 installed evidence 时，支持两类来源：
   - 直接嵌套的 `metadata.submission` / `metadata.review_evidence`；
   - 摊平字段 `hub_capability_id`、`hub_market_capability_id`、`hub_submission_id`、`hub_version_key`、`hub_review_status`、`hub_package_sha256`。
3. 从 Hub 审核队列安装 approved 企业审批型 App 后，本地 AppEntry 的 `installEvidence` 保留并展示 Hub 发布身份和审核证据。
4. 从 DataSrv 冷启动恢复企业普通应用时，即使只有摊平 `hub_*` 字段，也能合成 `installEvidence.submission` 并继续保留 `review_evidence`。

验证命令：
```powershell
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "installs an approved Hub approval app with runtime install evidence"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "restores DataSrv installed enterprise normal app run evidence into app candidates"
```

当前链路进一步变为：

```text
Hub package entry.governance.submission / review_evidence
  -> GUI install_evidence.submission / review_evidence
  -> 本地 AppEntry.installEvidence
  -> 运行详情安装证据条显示 Hub 来源、审核状态、capability、version key
  -> DataSrv app-installations.metadata.submission / hub_* / review_evidence
  -> GUI 冷启动恢复同一份 Hub 安装身份
```

剩余缺口：继续把这条 UI 可见链和真实 Hub 审核发布接口、DataSrv app_installations schema/openapi、依赖 Skill 下载验签、审批 workflow 启动与结果回读串成更完整的黄金路径验收。
### 推进记录：DataSrv app_installations 正式承认 Hub 安装身份字段（2026-06-30）

本轮把上一段 GUI 已经能展示的 Hub `submission` / `review_evidence` 下沉到 MaClaw DataSrv 服务端契约。此前 GUI 安装侧会把这些字段写进 DataSrv metadata，GUI 冷启动也能从 `hub_*` 摊平字段恢复；但 DataSrv 自身的 metadata normalize、OpenAPI 和审计摘要还没有把这些字段当成正式 app_installations 合同。

本轮完成：
1. `normalizeAppInstallationMetadata` 新增 Hub identity 归一化：
   - 接受 `metadata.submission` 或 `metadata.governance.submission`；
   - 规范 camelCase / snake_case 字段到 `submission.capability_id`、`market_capability_id`、`submission_id`、`version_key`、`status`、`package_sha256`；
   - 同步派生 `hub_capability_id`、`hub_market_capability_id`、`hub_submission_id`、`hub_version_key`、`hub_review_status`、`hub_package_sha256`。
2. `reviewEvidence` / `review_evidence` / `maclaw_app_review_evidence` 统一归一为 `review_evidence`，并保留 `maclaw_app_review_evidence` 兼容别名。
3. DataSrv app installation 审计摘要白名单新增轻量 Hub 身份字段，避免审计只能看到 source=enterprise_hub 而无法反查 capability/version/submission。
4. DataSrv OpenAPI 的 app-installations metadata schema 显式声明：
   - `submission`
   - `review_evidence`
   - `maclaw_app_review_evidence`
   - `hub_capability_id`
   - `hub_market_capability_id`
   - `hub_submission_id`
   - `hub_version_key`
   - `hub_review_status`
   - `hub_package_sha256`
5. 新增 DataSrv 服务测试，验证 Upsert -> stored metadata -> capabilities -> audit log 都保留 Hub 安装身份和审核证据。

验证命令：
```powershell
cd datasrv
go test ./structureddata -run "TestUpsertAppInstallationNormalizesHubSubmissionAndReviewEvidence|TestUpsertAppInstallationSynthesizesTestEvidenceFromSummaryMetadata" -count=1
go test ./structureddata -run TestHTTPServerRequiresBearerTokenAndHandlesRecords -count=1
go test ./structureddata -run "TestUpsertAppInstallation|TestHTTPServerAppInstallation" -count=1
```

当前链路进一步变为：

```text
Hub package entry.governance.submission / review_evidence
  -> GUI RecordMaclawAppInstall 写 DataSrv app_installations.metadata
  -> DataSrv normalize canonical submission + review_evidence
  -> DataSrv OpenAPI 明确声明 hub_* 字段
  -> DataSrv capabilities / audit log 可追踪 Hub capability、version、submission、review status
  -> GUI 冷启动恢复和应用详情继续展示同一份来源身份
```

剩余缺口：继续把 DataSrv 这一正式合同纳入跨服务黄金路径，尤其是 Hub 真实 approve/publish、依赖 Skill 下载验签、审批 workflow 发起、DataSrv RecordApproval 同步、单 App/全局审批中心结果和文件回读的同一实例验收。

### 推进记录：DataSrv app_installations 支持按 Hub 安装身份查询（2026-06-30）

本轮在上一段“DataSrv 正式承认 Hub 安装身份字段”的基础上继续收口查询合同。此前 `app_installations.metadata` 已能保存并归一化 Hub `submission` / `review_evidence`，但运维、GUI 冷启动恢复和跨服务黄金路径还缺少稳定查询入口，无法直接问“某个 Hub capability/version/submission/review status 在本租户安装了哪些 App”。

本轮完成：

1. `QueryAppInstallationsInput` 新增 Hub 身份过滤字段：
   - `hub_capability_id`
   - `hub_market_capability_id`
   - `hub_submission_id`
   - `hub_version_key`
   - `hub_review_status`
2. DataSrv HTTP GET `/api/v1/data/app-installations` 新增同名查询参数，并提供兼容别名：
   - `capability_id`
   - `market_capability_id`
   - `submission_id`
   - `version_key`
   - `review_status`
3. SQLite app_installations 过滤器支持同时从摊平字段和嵌套字段匹配：
   - `hub_*`
   - `submission.*`
   - `governance.submission.*`
4. `appInstallationMapHasAnyIdentifier` 支持点路径读取，避免嵌套 submission 只能靠 normalize 后的摊平字段查询。
5. DataSrv OpenAPI 将上述查询参数纳入 `/api/v1/data/app-installations` 参数列表。
6. 服务层测试验证 5 个 Hub 身份字段均可查回同一安装记录，错误 capability 会返回空结果。
7. HTTP 层测试验证标准 `hub_*` 参数组合可查回安装记录，兼容别名的错误 capability 会正确排除。

验证命令：

```powershell
cd datasrv
go test ./structureddata -run "TestUpsertAppInstallationNormalizesHubSubmissionAndReviewEvidence|TestHTTPServerAppInstallationsOverrideObjectRoleBindings|TestHTTPServerRequiresBearerTokenAndHandlesRecords" -count=1
go test ./structureddata -run "TestUpsertAppInstallation|TestHTTPServerAppInstallation" -count=1
```

当前链路进一步变为：

```text
Hub package submission / review_evidence
  -> GUI 安装写入 DataSrv app_installations.metadata
  -> DataSrv normalize canonical submission + hub_* summaries
  -> DataSrv OpenAPI 暴露 Hub 身份查询参数
  -> GUI / 运维 / 黄金路径验收可按 capability、market capability、submission、version key、review status 查询安装记录
```

剩余缺口：继续把这条可查询的 DataSrv 合同接入跨服务黄金路径，重点是 Hub 真实 approve/publish、依赖 Skill 下载验签、审批 workflow 发起、DataSrv RecordApproval 同步、单 App/全局审批中心结果和文件回读的同一实例验收。

### 推进记录：Hub submit/approve/publish/list/download 黄金路径验收（2026-06-30）

本轮把 Hub 后端侧此前分散的 MaClaw App 提交、审核、发布、市场列表和下载包能力串成一条连续验收。目标是证明 GUI `SearchMixedSkills` / Add from market 安装前预览所依赖的市场 metadata，不只是前端 mock 或单点 handler 假设，而是来自真实 Hub handler 状态流转后的同一份 capability/version/package 数据。

本轮完成：

1. 新增 Hub 后端组合测试 `TestCapabilityMaclawAppSubmitApprovePublishListAndDownloadGoldenPath`。
2. 测试链路覆盖：
   - `POST /api/capabilities/maclaw-apps/submit`
   - `POST /api/admin/capabilities/maclaw-apps/{id}/approve`
   - `POST /api/admin/capabilities/maclaw-apps/{id}/publish`
   - `GET /api/capabilities?type=skill`
   - `GET /api/capabilities/maclaw-apps/{id}/package`
3. 发布后的 marketplace list 断言同一 capability：
   - 状态为 `published`
   - metadata 保留 `review_state=published`、`published_by`、`package_signature`
   - metadata 暴露 GUI 安装前预览需要的 `review_evidence`
   - metadata 暴露动态 UI layout 摘要、依赖数量、result coverage、output/artifact 数量
4. 下载包断言同一 package：
   - 顶层携带 `review_evidence` 和 `package_signature`
   - app entry 的 `governance.submission` 携带 Hub 发布身份、published 状态、发布人、签名和 review evidence
   - 动态 UI、审批测试证据和结果包仍由原始 package/manifest 保留
5. 扩展既有下载包测试，显式验证 `review_evidence` 同时进入 package 顶层和 entry submission，避免 Hub 下载包只保留签名而丢失审核摘要。

验证命令：

```powershell
go test ./hub/internal/httpapi -run "TestCapabilityMaclawAppSubmitApprovePublishListAndDownloadGoldenPath|TestCapabilityMaclawAppPackageDownloadReturnsApprovedPack|TestAdminCapabilityMaclawAppPublishPublishesApprovedVersion" -count=1
```

当前链路进一步变为：

```text
App Studio submit package
  -> Hub pending_review capability/version
  -> Hub approve 生成 review_evidence
  -> Hub publish 生成 package_signature 并保持 review_evidence
  -> /api/capabilities?type=skill 市场列表可见 published + review_evidence + layout/result/dependency 摘要
  -> /api/capabilities/maclaw-apps/{id}/package 下载包携带 signature + review_evidence + governance.submission
  -> GUI 市场安装前预览和安装后审计消费同一份 Hub 发布证据
```

剩余缺口：下一步继续把 Hub 后端黄金路径和 GUI 市场安装测试、DataSrv app_installations 查询、审批 workflow 运行/结果回读接成一条跨服务最终样例，减少纯前端 mock 和分段验证之间的空隙。
### 推进记录：GUI Hub 安装后按 DataSrv Hub 身份查询回读（2026-06-30）

本轮继续把 Hub 发布身份、GUI 安装审计和 DataSrv `app_installations` 查询合同接起来。上一轮 DataSrv 已经支持按 `hub_capability_id` / `hub_version_key` / `hub_review_status` 查询安装记录，Hub 后端也已经通过 submit/approve/publish/list/download 黄金路径证明市场 metadata 和下载包会携带同一份 review evidence 与发布身份。本轮把这个合同落到 GUI 安装侧验收。

本轮完成：

1. 扩展 `TestInstallSelectedMaclawApprovalAppFromHubPreservesApprovalEvidence`。
2. 测试中的 DataSrv mock 从只接收 PUT 注册升级为支持：
   - `PUT /api/v1/data/app-installations/expense-approval`
   - `GET /api/v1/data/app-installations?hub_capability_id=...&hub_version_key=...&hub_review_status=...`
3. Hub 下载包夹具携带 `governance.submission` 和 `review_evidence`，模拟真实 Hub published package。
4. GUI 安装后继续断言：
   - `install_evidence.review_evidence` 保留 run、审批状态和当前节点
   - `install_evidence.submission` 保留 capability、version key、package sha
   - DataSrv registration metadata 保留 `review_evidence`、`submission`、`hub_*` 摘要字段
   - 本地安装审计保留 `ReviewEvidence` 和 `Submission`
5. 新增安装后 DataSrv GET 查询断言：按 `hub_capability_id + hub_version_key + hub_review_status` 能查回同一个 `expense-approval` 安装记录，并保留审批实例摘要 `test_evidence_approval_id=approval-expense-1001`。

验证命令：

```powershell
go test ./gui -run TestInstallSelectedMaclawApprovalAppFromHubPreservesApprovalEvidence -count=1
go test ./gui -run "TestInstallSelectedMaclawAppPackageFromHubInstallsDepsAndRegistersDataSrv|TestInstallSelectedMaclawAppPackageFromHubFiltersPackageApps|TestInstallSelectedMaclawAppPackageFromHubReportsDependencyInstallDiagnostics" -count=1
```

当前链路进一步变为：

```text
Hub published package governance.submission / review_evidence
  -> GUI InstallSelectedMaclawAppPackageFromHub
  -> InstallMaclawAppDependencies 安装 app/workflow Skill 依赖
  -> RecordMaclawAppInstall 写本地安装审计和 DataSrv app_installations metadata
  -> DataSrv 可按 hub_capability_id / hub_version_key / hub_review_status 查回同一安装记录
  -> 查询结果继续保留审批实例、结果包、review evidence 和 Hub 发布身份
```

剩余缺口：继续把安装后的“查询到安装记录”推进到“发起审批 workflow、同步 RecordApproval、审批节点状态流转、单 App/全局审批中心读取同一实例结果和文件”的最终跨服务样例。
### 推进记录：审批 workflow 运行后单 App/全局审批中心同实例回读（2026-06-30）
本轮把“已安装审批型 App -> 发起审批 workflow Skill -> DataSrv RecordApproval 同步 -> GUI 审批中心回读”的验收再向前收紧一层。此前测试已经证明 workflow skill 的 progress/final 结果会写回 DataSrv，并能从全局审批中心读取；本轮补上单 App 详情页视角，确保企业审批型应用的“我的申请/我审批的/已处理”实例数据不是只在全局中心可见，而是能按 App 维度回到同一条审批实例。

本轮完成：
1. 扩展 `TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult` 的 DataSrv 请求捕获，记录 `/api/v1/data/approvals` 的 RawQuery。
2. 审批 workflow final sync 后同时调用：
   - `ListMaclawAppApprovalInstances("expense-approval", "handled", 10)`
   - `ListMaclawAppApprovalInstancesAll("handled", 10)`
3. 断言单 App 视图与全局审批中心读到的是同一个实例：`app_id`、`approval_id`、`workflow_instance_id`、`workflow_decision_id` 一致。
4. 断言两边都保留最终结果反馈：`result_payload`、内容输出、文件 artifact。
5. 断言 GUI 对 DataSrv 发起了两类读取：
   - App scoped：`app_id=expense-approval&lane=handled&limit=10`
   - Global：`lane=handled&limit=10`

验证命令：
```powershell
go test ./gui -run "TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult|TestListMaclawAppApprovalInstancesAll" -count=1
```

当前链路进一步变为：

```text
Hub published approval app
  -> GUI install + dependency skill verification
  -> DataSrv app_installations registration/query by Hub identity
  -> StartMaclawAppApprovalWorkflow
  -> workflow Skill returns progress_instances + final approval_instance
  -> GUI syncs progress/review to DataSrv RecordApproval
  -> single App approval lane reads app_id-scoped handled instance
  -> global approval center reads same handled instance
  -> both preserve approval result, text output, file artifact, node path, workflow decision identity
```

剩余缺口：继续把这条后端/GUI 运行样例推到前端页面层和 App Studio 层，重点是动态界面生成/可视化布局保存、制作-测试-上传 Hub 的可视化闭环、安装依赖诊断 UI、以及审批实例列表在 `AppsPage` 中按“企业审批型应用 / 企业普通应用 / 工具型应用”完整呈现。
### 推进记录：App 运行页常态显示安装治理/依赖/Hub/DataSrv 证据（2026-06-30）
本轮推进前端页面层，把此前已经打通的 Hub 发布证据、安装依赖检查、DataSrv 注册和安装审计信息，从“安装结果/市场行/头部摘要”进一步落到 App 运行页。目标是让用户打开企业审批型 App 后，不需要回到市场或安装记录，也能在传统软件式运行界面中看到这个 App 为什么可信、依赖是否齐、是否已经注册到 DataSrv。

本轮完成：
1. 在 `AppsPage` 新增运行时 `RuntimeInstallGovernancePanel`，复用既有 `installRecordEvidenceItems`，不新增后端契约。
2. 面板常态展示安装治理摘要：
   - Hub/市场来源与发布状态
   - 动态 UI 布局入口、模板、密度
   - result contract 与 result coverage
   - 测试证据与审批实例证据
   - 依赖验证数量与阻断数量
   - DataSrv 绑定注册状态
3. 修正动态布局缺口：当 App 安装证据或依赖验证计划存在时，即使 manifest 的 runtime layout 没有显式声明 `detail/status` 区，也保留运行状态区，避免治理证据被布局配置隐藏。
4. 为运行治理面板补充产品型样式：紧凑事实表、低噪声边框、语义状态色，延续当前 MaClaw GUI 风格。
5. 扩展前端测试 `installs approved Hub MaClaw Apps from market search results`：安装 approved Hub MaClaw App 后打开 App，断言运行页可见 `Install governance`，并能看到 Hub published 来源、依赖验证、DataSrv 注册状态。
6. 调整冷启动恢复测试，允许安装证据在头部摘要和运行页治理面板两个位置同时出现，验证冷启动后仍可恢复动态布局和安装证据。

验证命令：
```powershell
cd gui/frontend
npm.cmd test -- AppsPage.test.tsx -t "installs approved Hub MaClaw Apps from market search results|restores dynamic layout and install evidence from stored app entries after cold start"
cd ../..
git diff --check -- gui\frontend\src\components\pages\AppsPage.tsx gui\frontend\src\components\pages\AppsPage.css gui\frontend\src\components\pages\__tests__\AppsPage.test.tsx
```

完整 `AppsPage.test.tsx` 当前仍有 3 个非本轮新增失败，集中在此前工作树已有的“workflow contract / governance review 是否阻止运行或发布”的语义切换测试上；本轮相关两个前端用例已通过。

当前链路进一步变为：

```text
Hub published MaClaw App + review_evidence
  -> GUI market install + dependency skill verification
  -> local install audit / DataSrv registration / install_evidence
  -> App runtime opens with dynamic layout
  -> runtime status area always preserves install governance evidence when present
  -> user can see Hub source, dependency verification, DataSrv registration, result/test/approval evidence before running or continuing workflow
```

剩余缺口：继续把 App Studio 的可视化设计器做实，包括动态 UI 自动生成后的可视化位置调节、layout 保存到应用信息文件、测试运行证据与上传 Hub 审核包的一键串联；同时需要收口 workflow contract / governance review 的运行时与发布时边界语义，让前端测试全集重新一致。
### 推进记录：发布/运行边界语义收口（2026-06-30）

本轮收口 `AppsPage` 中 MaClaw App 发布、安装、运行三类边界的校验语义，重点是把“本地运行可容忍的治理提示”和“发布/市场安装必须阻断的问题”区分开，同时保留后端 `PlanMaclawAppInstall` 对当前 App 的权威判断。

本轮完成：

1. 发布前校验顺序调整为：
   - 先读取后端 `PlanMaclawAppInstall`；
   - 如果后端计划对当前 App 有依赖、workflow contract、governance review 或阻断依赖信号，则采用后端计划；
   - 如果后端计划为空计划，则沿用安装证据或最近运行证据中的 `dependencyVerification`，避免空计划覆盖已有验证证据；
   - 当前 App 的 workflow contract issue 优先阻断发布；
   - 当前 App 的必需依赖缺失/停用用具体依赖错误阻断发布；
   - governance review issue 阻断发布和市场安装，但不阻断本地运行；
   - 最后再执行 declared Skill dependencies 与 verification plan 的一致性检查。
2. `appDependencyVerificationPublishCheckForPlan` 改为按当前 App ID 过滤 workflow/governance/dependency 信号，避免同一包里其它 App 的阻断依赖误伤当前 App。
3. 运行态继续阻断缺失/停用必需 Skill 和 workflow contract drift；治理审核问题保留为发布/安装门禁，不阻断本地 tool/normal app 运行。
4. 市场单 App 安装继续阻断 governance review issue 和 workflow contract issue，确保企业能力市场分发入口不会绕过审核合同。
5. 修复此前前端全集中 7 个失败用例：审核退回后可重新提交、必需依赖停用时展示具体依赖阻断文案、result contract 覆盖证据满足后允许提交、dependency verification 覆盖 declared Skill 后允许提交、审批实例证据完整后允许审批型 App 提交、企业动态 UI metadata 进入提交包、durable queue review status 合并后可重新提交。

验证命令：

```powershell
cd gui/frontend
npm.cmd test -- AppsPage.test.tsx -t "shows returned review status and allows resubmission|blocks app package review submission when required dependencies are unavailable|requires run evidence to cover the declared primary result contract|requires dependency verification to cover declared app Skill dependencies before publishing|requires approval instance evidence before publishing approval apps|includes enterprise visual UI metadata in market submission packages|merges durable queue review status into local publish cards"
npm.cmd test -- AppsPage.test.tsx
```

验证结果：

```text
AppsPage.test.tsx: 204 passed
```

当前链路进一步变为：

```text
App Studio / installed MaClaw App
  -> submit publish
  -> PlanMaclawAppInstall backend preflight
  -> current App scoped dependency / workflow contract / governance review gates
  -> existing dependencyVerification evidence preserved when backend returns empty plan
  -> package governance.testEvidence + dependencyVerification submitted
  -> local queue / Hub review / market install consume the same boundary semantics
```

剩余缺口：继续把当前分段验证串成最终黄金路径，重点是 App Studio 可视化布局编辑真实交互、Hub 真实 approve/publish 后市场可搜索、依赖 Skill 下载/验签安装、DataSrv app_installations 注册查询、审批 workflow 发起与节点同步、单 App/全局审批中心读取同一实例的结果和文件。前端 `AppsPage` 页面层已经恢复全量一致，下一步应优先补跨服务黄金样例和后端字段合同巡检。
### 推进记录：签名 Hub 安装运行黄金链补强 Hub 审核身份证据（2026-06-30）

本轮继续把跨服务黄金路径向“同一条链路同时证明发布身份、安装依赖、DataSrv 注册、审批运行和结果回读”收紧。此前 `TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv` 已经覆盖签名 Hub 包下载、依赖 Skill 下载/验签安装、DataSrv app-installations 注册、审批 workflow 发起、running/final 节点同步、requires_input 补充提交和单 App/全局审批结果回读；但这条签名运行链还没有显式断言 Hub `submission` / `reviewEvidence` 在本地安装审计中保真。

本轮完成：

1. 在签名 Hub MaClaw App package 的 governance 中加入：
   - `reviewEvidence.run_id / test_protocol_fingerprint / result_coverage / approval_status / current_node`；
   - `submission.channel/status/capability_id/market_capability_id/submission_id/version_key/package_sha256`；
   - `submission.review_evidence`，模拟 Hub published package 下载时携带的审核摘要。
2. 扩展 `TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv` 断言：
   - `installRecord.Submission` 必须保留 `cap-expense-approval`、`enterprise_hub:skill:maclaw-app:expense-approval@pkg` 和 `published`；
   - `installRecord.ReviewEvidence` 必须保留 `run-expense-golden`、`approved`、`expense.result`；
   - 原有依赖验签、DataSrv 注册、审批 workflow 运行、补充提交、同一实例回读断言继续成立。

验证命令：

```powershell
go test ./gui -run TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv -count=1
go test ./gui -run "TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv|TestInstallSelectedMaclawApprovalAppFromHubPreservesApprovalEvidence|TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult|TestListMaclawAppApprovalInstancesAll" -count=1
go test ./hub/internal/httpapi -run TestCapabilityMaclawAppSubmitApprovePublishListAndDownloadGoldenPath -count=1
```

验证结果：全部通过。

当前链路进一步变为：

```text
Hub published signed MaClaw App package
  -> package carries submission + reviewEvidence + package signature
  -> GUI downloads package from Hub
  -> installs/verifies app Skill + workflow Skill dependencies
  -> RecordMaclawAppInstall persists local audit + DataSrv registration
  -> install audit preserves Hub submission identity and review evidence
  -> StartMaclawAppApprovalWorkflow runs installed workflow Skill
  -> DataSrv RecordApproval sync receives running/final/continue events
  -> ListMaclawAppApprovalInstances / ListMaclawAppApprovalInstancesAll read back same instance result and files
```

剩余缺口：继续把这条后端/GUI 黄金链上移到真实前端用户操作层，尤其是 App Studio 可视化布局拖拽/调整、测试运行、提交 Hub、审核发布、市场搜索安装、打开 App 运行和审批中心回看的一条 UI 级自动化验收；同时继续巡检 DataSrv OpenAPI / metadata schema 是否已经把这些字段全部声明为稳定契约。
### 推进记录：DataSrv app_installations 稳定声明 Hub review evidence 摘要字段（2026-06-30）

本轮继续收口 DataSrv 正式合同。此前 `app_installations.metadata` 已经能保留 `review_evidence` / `maclaw_app_review_evidence` 对象和 Hub `submission` 身份，也能按 Hub capability/version/review status 查询；但审核证据中的常用摘要仍主要藏在深层 JSON 里。企业集成方、审计任务和 GUI 冷启动恢复如果只想快速判断“这个 App 是不是经过哪次测试、覆盖了哪些结果、审批状态到哪个节点”，仍需要解析不稳定的嵌套结构。

本轮完成：

1. DataSrv `normalizeAppInstallationHubIdentityMetadata` 增强为同时归一化 Hub review evidence：
   - 支持 `metadata.review_evidence`、`metadata.reviewEvidence`、`metadata.maclaw_app_review_evidence`；
   - 支持从 `metadata.submission.review_evidence` 或 `metadata.governance.reviewEvidence` 兜底提取；
   - 支持 flat evidence 和 `{app_id: evidence}` 映射型 evidence。
2. 新增稳定摊平字段：
   - `review_evidence_status`
   - `review_evidence_run_id`
   - `review_evidence_test_protocol_fingerprint`
   - `review_evidence_result_contract_primary`
   - `review_evidence_result_coverage_primary`
   - `review_evidence_result_coverage_covered_count`
   - `review_evidence_result_coverage_missing_count`
   - `review_evidence_output_count`
   - `review_evidence_artifact_count`
   - `review_evidence_approval_status`
   - `review_evidence_current_node`
3. `appInstallationAuditMetadata` 将这些 review evidence 摘要纳入安装审计日志，避免审计侧只能看到 Hub 身份而看不到审核证据摘要。
4. DataSrv OpenAPI `appInstallationMetadataOpenAPISchema` 显式声明这些字段，HTTP OpenAPI 测试也把它们纳入 `/api/v1/data/app-installations` POST/PUT metadata 合同。
5. 扩展 `TestUpsertAppInstallationNormalizesHubSubmissionAndReviewEvidence`：断言 normalize、Capabilities 返回、audit log 都能看到 Hub identity + review evidence 摘要。

验证命令：

```powershell
cd D:\workprj\aicoder\datasrv
go test ./structureddata -run "TestUpsertAppInstallationNormalizesHubSubmissionAndReviewEvidence|TestAppInstallationOpenAPISchemaDocumentsFullTestEvidence|TestHTTPServerCoreEndpoints" -count=1
go test ./structureddata -count=1
cd D:\workprj\aicoder
go test ./gui -run "TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv|TestInstallSelectedMaclawApprovalAppFromHubPreservesApprovalEvidence|TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult|TestListMaclawAppApprovalInstancesAll" -count=1
go test ./hub/internal/httpapi -run TestCapabilityMaclawAppSubmitApprovePublishListAndDownloadGoldenPath -count=1
```

验证结果：全部通过。

当前链路进一步变为：

```text
Hub review_evidence / submission
  -> GUI install record
  -> DataSrv app_installations.metadata
  -> canonical review_evidence + maclaw_app_review_evidence
  -> stable review_evidence_* summary fields
  -> Capabilities / audit log / OpenAPI all expose the same contract
  -> GUI cold start and enterprise audit can consume summaries without parsing deep package JSON
```

剩余缺口：继续把前端 UI 级黄金路径补齐，重点是 App Studio 可视化布局调整、测试运行、提交 Hub、审核发布、市场搜索安装、打开 App 发起审批、单 App/全局审批中心回看同一实例结果和文件。DataSrv 合同层下一步只需要继续巡检是否还有 `approval_instance` / `result_payload` / `dependency_verification` 命名别名遗漏。

### 推进记录：App Studio 可视化布局证据进入保存与提交闭环（2026-06-30）

本轮继续收口 App Studio 层。此前动态 UI 布局已经能写入 manifest，并能在发布检查里作为 `Workspace layout` 证据出现；但用户在设计器里的可视化调节动作还主要靠模板、密度、主区、输出区这些字段验证，缺少一个稳定的“布局证据摘要”来支撑保存、提交、Hub 审核和冷启动回放之间的同一性比对。

本轮完成：

1. 在 `StudioLayoutDesigner` 画布中新增布局证据条，常态展示：
   - layout template / density
   - primary region
   - output region
   - visible region count / total region count
   - layout fingerprint
2. 新增稳定布局指纹 `workspaceLayoutFingerprint`，按 `entry + template + density + primaryRegion + outputRegion + ordered regions(id/role/placement/visible/order)` 计算 8 位摘要。
3. `appWorkspaceLayoutEvidence` 现在随治理证据输出：
   - `visibleRegionCount`
   - `regionIds`
   - `fingerprint`
4. 扩展 App Studio 保存测试，覆盖真实可视化编辑动作：
   - 在画布选择右侧主操作区；
   - 将输出区放到底部；
   - 隐藏 `approval_detail` 明细区；
   - 调整 `request_form` 区域顺序；
   - 保存到 Skill 后断言 manifest 中保留 `visible:false`、`order`、`regionIds`、`visibleRegionCount` 和 `fingerprint`。
5. 扩展 App Studio 测试运行 + 提交 Hub 测试，确认同一套布局证据进入提交包 `governance.workspaceLayout`，不是只存在于本地设计器。
6. 新增紧凑样式 `apps-layout-designer__evidence`，保持现有 MaClaw GUI 的企业工作台风格：低噪声、可扫读、可审计，不改成营销式卡片。

验证命令：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- AppsPage.test.tsx -t "dynamic layout"
npm.cmd test -- AppsPage.test.tsx
cd D:\workprj\aicoder
git diff --check -- gui\frontend\src\components\pages\AppsPage.tsx gui\frontend\src\components\pages\AppsPage.css gui\frontend\src\components\pages\__tests__\AppsPage.test.tsx
```

验证结果：

```text
AppsPage.test.tsx: 204 passed
diff --check: 仅有既有 CRLF/LF 提示，无新增空白错误
```

当前链路进一步变为：

```text
App Studio visual layout designer
  -> user adjusts template / density / primary / output / visibility / order
  -> layout evidence strip shows visible count + fingerprint
  -> Save to Skill writes manifest.ui.layouts.*.regions
  -> governance.workspaceLayout carries regionIds + visibleRegionCount + fingerprint
  -> Submit to Hub package preserves the same layout evidence
  -> Publish checklist and later install audit can compare the same UI layout identity
```

剩余缺口：继续把 UI 级黄金路径从“App Studio 制作 + 提交包证据”推进到“一条用户级自动化链路”：Hub 审核发布、能力市场搜索安装、打开已安装 App、发起审批 workflow、单 App 审批中心和全局审批中心回看同一实例的结果与文件。随后再巡检 DataSrv/GUI 前端之间 `approval_instance`、`result_payload`、`dependency_verification`、`workspace_layout` 的别名兼容是否完整。

### 推进记录：市场安装后继续审批并在单 App/全局审批中心回看同一最终实例（2026-06-30）

本轮继续把 UI 级黄金路径向后收紧。此前前端已经能验证 Hub 市场搜索、签名包安装、依赖诊断、安装治理证据、requires_input 补充提交参数；但市场安装后的审批续办链路在测试里结束于 `Continue with supplement` 的 payload 断言，还没有继续验证“续办后的最终审批实例，能同时在当前 App 的审批工作台和全局审批中心读到同一结果与文件”。

本轮完成：

1. 扩展 `installs approved Hub MaClaw Apps from market search results` 前端用例。
2. 在安装后的审批型 App 中构造完整终态实例 `installedApprovedAfterSupplement`：
   - `approval_id = approval-market-contract-input-1`
   - `instance_id = wf-market-contract-input-1`
   - `lane = handled`
   - `status/result_status = approved`
   - `current_node = contract.result_feedback`
   - `result_payload.business_record.id = contract-input-1`
   - 输出 `Approval decision`
   - 文件 `contract-final-approval.pdf`
3. 继续审批后验证单 App 视角：
   - `ListMaclawAppApprovalInstances(installedAppID, "handled", ...)` 能返回同一终态实例；
   - App 内 `Handled` lane 展示同一 `approval_id`、`workflow instance`、结果输出和文件。
4. 继续审批后验证全局审批中心视角：
   - `Approval status` 入口读取全局实例；
   - 全局审批中心 `Handled` 视图展示同一 App、同一 `approval_id`、同一 `workflow instance`、同一结果输出和文件。
5. 保持运行治理面板断言：安装后打开 App 仍能看到 Hub published 来源、依赖验证摘要和 DataSrv 注册状态。

验证命令：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- AppsPage.test.tsx -t "installs approved Hub MaClaw Apps from market search results"
npm.cmd test -- AppsPage.test.tsx
cd D:\workprj\aicoder
git diff --check -- gui\frontend\src\components\pages\__tests__\AppsPage.test.tsx
```

验证结果：

```text
AppsPage.test.tsx: 204 passed
diff --check: 无新增空白错误
```

当前链路进一步变为：

```text
Hub published approval app
  -> market search shows review evidence
  -> one-click install selected MaClaw App from signed Hub package
  -> install evidence preserves submission / review / dependency / DataSrv registration
  -> open installed App runtime governance panel
  -> requires_input approval instance appears in App workspace
  -> requester continues with supplement while preserving approval_id / workflow_instance_id
  -> workflow returns approved final instance with output and artifact
  -> single App handled lane reads same final instance
  -> global approval center handled lane reads same final instance
```

剩余缺口：前端 UI 黄金路径已经覆盖“市场安装后续办审批并回看结果”，下一步应继续把更前面的“App Studio 制作 -> 本地提交队列 -> Hub 同步/审核/发布 -> 市场搜索安装”与这段运行审批回看合并成一个端到端样例，或者在后端 GUI/Hub/DataSrv 跨服务测试里加入同等的真实 Hub publish + signed package + DataSrv approval readback 组合验证。

### 推进记录：App Studio 布局证据贯通提交队列详情、Hub 包和安装审计（2026-06-30）

本轮继续把“App Studio 制作”和“Hub/市场安装运行”两段 UI 黄金路径接紧。此前 App Studio 侧已经会生成 `workspaceLayout.fingerprint`，市场安装侧也能保留安装治理证据；但用户在发布队列详情中还只能看到应用名、审计事件、review evidence，不能直接确认 Hub 包中携带的动态 UI 布局是否就是 App Studio 保存的那一版。

本轮完成：

1. 新增 `packageWorkspaceLayoutSummariesFromRecord`，从提交队列详情 record 的 `package.apps[*].app.governance.workspaceLayout` 或 `app.binding.ui` 中提取布局摘要。
2. 发布队列详情 UI 新增 `Workspace layout` 摘要行，展示：
   - App 名称
   - workspace entry
   - template
   - density
   - visible/region count
   - layout fingerprint
3. 扩展 `syncs a local submission to Hub, refreshes published status, then installs it from market search`：
   - 本地/Hub 提交详情包携带 `governance.workspaceLayout.fingerprint = layoutflow1`
   - Hub 下载包携带同一 `binding.ui` 和 `governance.workspaceLayout`
   - 安装审计 `installEvidence.workspace_layout` 保留同一 fingerprint、template、density、primary/output region、visible count
   - 队列详情 UI 明确显示 `Workspace layout: Flow Approval App · approval_workspace · left_nav · compact · 3 regions · fp:layoutflow1`
4. 这使前端链路能用同一个布局指纹串联：
   - App Studio 生成/保存的动态 UI
   - 本地提交队列详情
   - Hub published package
   - 市场安装结果
   - 本地 install evidence

验证命令：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- AppsPage.test.tsx -t "syncs a local submission to Hub"
npm.cmd test -- AppsPage.test.tsx
cd D:\workprj\aicoder
git diff --check -- gui\frontend\src\components\pages\AppsPage.tsx gui\frontend\src\components\pages\__tests__\AppsPage.test.tsx
```

验证结果：

```text
AppsPage.test.tsx: 204 passed
diff --check: 仅有既有 CRLF/LF 提示
```

当前链路进一步变为：

```text
App Studio dynamic UI layout
  -> workspaceLayout fingerprint
  -> local submission queue package detail
  -> Hub published package binding.ui / governance.workspaceLayout
  -> market install selected MaClaw App
  -> installEvidence.workspace_layout keeps same fingerprint
  -> runtime install governance can be audited against the same UI identity
```

剩余缺口：下一步优先补后端跨服务组合验证，把 GUI 前端已串起来的证据链落到真实 Hub submit/approve/publish/download、GUI install、DataSrv app_installations 查询和 approval_instances 回读的同一测试样例里，减少前端 mock 与跨服务真实行为之间的剩余空隙。

### 推进记录：签名 Hub 安装链把 App Studio 布局证据落到 DataSrv 注册 payload（2026-06-30）

本轮继续补后端跨服务组合验证。此前前端和本地安装审计已经能看到 `workspaceLayout.fingerprint`，但真实 DataSrv app-installations 注册 payload 的扁平摘要还缺少 `fingerprint`、`visibleRegionCount` 和稳定 `regionIds`，导致企业审计侧仍可能需要解析深层 `workspace_layout` JSON 才能确认安装的是 App Studio 保存的那版动态界面。

本轮完成：

1. `maclawAppWorkspaceLayoutMetadataForEntry` 现在优先采用带 App Studio 证据的 `governance.workspaceLayout`：
   - `fingerprint`
   - `regionIds`
   - `visibleRegionCount`
   - `studio.editable / savedInManifest / updatedBy`
   - 若无这些治理证据，则继续兼容 `app.ui` / `binding.ui` 的既有提取路径。
2. `maclawAppDataSrvInstallationPayloads` 将布局证据提升为稳定 DataSrv metadata 摘要字段：
   - `workspace_layout_fingerprint`
   - `workspace_layout_visible_region_count`
   - `workspace_layout_region_ids`
   - 同时保留已有 template、density、primary/output region、region count。
3. 扩展 `TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv`：
   - 签名 Hub MaClaw App 包现在携带 4 个布局区域，其中 `approval_detail` 为隐藏区域；
   - `regionIds = approval_inbox, request_form, approval_detail, result_panel`；
   - `visibleRegionCount = 3`；
   - `fingerprint = layout-expense-golden`；
   - 本地 install audit 必须保存同一套布局证据；
   - DataSrv mock 捕获 PUT `/api/v1/data/app-installations/expense-approval` body，并断言 metadata 中同样存在布局指纹、区域数量和区域顺序。
4. 同步升级 `TestInstallSelectedMaclawApprovalAppFromHubPreservesApprovalEvidence` 的相邻断言，使 DataSrv metadata 查询视角也验证同一布局指纹与隐藏区域。

验证命令：

```powershell
cd D:\workprj\aicoder
go test ./gui -run TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv -count=1
go test ./gui -run "TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv|TestInstallSelectedMaclawApprovalAppFromHubPreservesApprovalEvidence|TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult|TestListMaclawAppApprovalInstancesAll" -count=1
```

验证结果：

```text
ok github.com/RapidAI/CodeClaw/gui
```

当前链路进一步变为：

```text
Hub signed MaClaw App package
  -> governance.workspaceLayout carries App Studio fingerprint / region order / hidden region
  -> GUI install prefers governance layout evidence over generated fallback UI
  -> local install audit preserves same workspace layout identity
  -> DataSrv app-installations PUT metadata exposes same fingerprint / visible count / region ids
  -> approval workflow start / sync / single App readback / global approval center readback still pass
```

剩余缺口：继续补 DataSrv 正式 schema/OpenAPI 对 `workspace_layout_fingerprint`、`workspace_layout_visible_region_count`、`workspace_layout_region_ids` 的声明与查询覆盖，并把企业普通应用、工具型应用也补齐同等制作、安装、运行闭环样例。

### 推进记录：DataSrv 正式合同声明 App Studio 布局指纹并支持查询（2026-06-30）

本轮把上一轮 GUI 注册 payload 中已经出现的布局证据，继续推进到 DataSrv 正式合同层。此前 `workspace_layout_fingerprint`、`workspace_layout_visible_region_count`、`workspace_layout_region_ids` 已经能从 GUI 安装链路发到 DataSrv，但 DataSrv normalize、audit、HTTP 查询和 OpenAPI 对这些字段的承认还不完整。

本轮完成：

1. `normalizeAppInstallationWorkspaceLayoutMetadata` 增强：
   - 规范 `workspace_layout.fingerprint` 并提升为 `workspace_layout_fingerprint`；
   - 规范 `visibleRegionCount / visible_region_count` 并提升为 `workspace_layout_visible_region_count`；
   - 规范 `regionIds / region_ids`，并在有 `regions` 时稳定回写区域顺序；
   - 从 `regions[*].visible` 推导可见区域数；
   - 规范 `studio.savedInManifest / editable / updatedBy` 并提升为 `workspace_layout_studio_*` 摘要字段。
2. DataSrv app installation audit 白名单加入：
   - `workspace_layout_fingerprint`
   - `workspace_layout_visible_region_count`
   - `workspace_layout_studio_saved_in_manifest`
   - `workspace_layout_studio_editable`
   - `workspace_layout_studio_updated_by`
3. `QueryAppInstallationsInput` 新增 `WorkspaceLayoutFingerprint`，HTTP GET `/api/v1/data/app-installations` 支持：
   - `workspace_layout_fingerprint`
   - `layout_fingerprint`
4. SQLite metadata filter 支持按布局指纹匹配：
   - 顶层 `workspace_layout_fingerprint`
   - `workspace_layout.fingerprint`
   - `governance.workspaceLayout.fingerprint`
   - `install_evidence.workspace_layout.fingerprint`
5. OpenAPI 补齐：
   - GET query 参数 `workspace_layout_fingerprint / layout_fingerprint`
   - metadata schema 字段 `workspace_layout_fingerprint`
   - metadata schema 字段 `workspace_layout_visible_region_count`
   - metadata schema 字段 `workspace_layout_studio_*`
6. 扩展测试：
   - DataSrv normalize/audit 测试验证布局指纹、可见区域数和 Studio 保存证据；
   - Hub submission/review evidence 用例验证可按布局指纹查询安装记录；
   - HTTP 核心接口测试验证 `GET /app-installations?workspace_layout_fingerprint=...`；
   - OpenAPI 测试验证 query 参数和 metadata 字段声明。

验证命令：

```powershell
cd D:\workprj\aicoder\datasrv
go test ./structureddata -run "TestUpsertAppInstallationNormalizesGovernanceResultContract|TestUpsertAppInstallationNormalizesHubSubmissionAndReviewEvidence|TestAppInstallationOpenAPISchemaDocumentsFullTestEvidence|TestHTTPServerCoreEndpoints|TestOpenAPISchema" -count=1
go test ./structureddata -count=1
cd D:\workprj\aicoder
go test ./gui -run "TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv|TestInstallSelectedMaclawApprovalAppFromHubPreservesApprovalEvidence|TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult|TestListMaclawAppApprovalInstancesAll" -count=1
```

验证结果：

```text
datasrv/structureddata: ok
gui: ok
```

当前链路进一步变为：

```text
App Studio layout evidence
  -> Hub package governance.workspaceLayout
  -> GUI install / DataSrv registration payload
  -> DataSrv normalize canonical workspace_layout
  -> stable workspace_layout_* metadata summaries
  -> audit log / Capabilities / HTTP list response / OpenAPI schema
  -> GET app-installations by workspace_layout_fingerprint
```

剩余缺口：接下来应补企业普通应用和工具型应用的端到端样例，使三类 App 都具备制作、测试、上传、市场安装、依赖检查、运行、结果反馈和审计查询的完整闭环；审批型应用则继续补异常/失败/回滚场景。

### 推进记录：企业普通应用和工具型应用补齐安装审计与运行结果证据（2026-06-30）

本轮继续把三类 MaClaw App 的闭环从审批型应用扩展到企业普通应用和工具型应用。此前审批型应用已经能把 App Studio 布局、Hub 审核证据、依赖验证、DataSrv 注册、审批实例和结果文件串起来；企业普通应用和工具型应用虽然已有安装/运行路径，但安装审计中对动态布局、结果合同、运行输出和附件证据的断言还不够硬，容易出现“能安装，但企业审计侧看不出它实际运行产出了什么”的断点。

本轮完成：

1. 企业普通应用 Hub 安装夹具补齐完整治理证据：
   - `workspaceLayout.fingerprint = layout-normal-hub-install`
   - `visibleRegionCount = 3`
   - `studio.editable / savedInManifest / updatedBy`
   - `resultContract.primary = business_status`
   - result types 覆盖 `business_status / business_record / content / artifact`
   - test evidence 保存 `business_status`、`business_record`、`content` 和附件 `hub-install-export`
2. 扩展 `TestInstallMaclawAppPackageFromHubDownloadsAndRecordsInstall`，验证：
   - DataSrv 注册 metadata 保留 layout fingerprint、visible region count 和 result contract；
   - 企业普通应用的业务运行证据、业务记录输出、附件证据进入 DataSrv registration；
   - 本地 install audit 保留同一份 layout / result / test evidence；
   - DataSrv registration status 仍为成功。
3. 工具型应用签名 Hub 安装夹具补齐完整治理证据：
   - `workspaceLayout.fingerprint = layout-tool-signed-install`
   - `visibleRegionCount = 2`
   - `studio.editable / savedInManifest / updatedBy`
   - `resultContract.primary = document`
   - result types 覆盖 `document / content / artifact`
   - test evidence 保存 document 输出和附件 `signed-output.pdf`
4. 扩展 `TestInstallSelectedMaclawAppPackageFromHubUsesPackageSignatureTrustForDependencySkill`，验证：
   - 依赖 Skill 从签名包下载并保留 signature / integrity metadata；
   - 工具型应用 install plan 为 ready；
   - 因工具型应用没有 DataSrv role bindings，DataSrv registration 明确为 `skipped`，reason 为 `no datasrv role bindings`；
   - 本地 install audit 仍保留工具型应用的 layout fingerprint、document result contract、test evidence 和 artifact evidence。
5. 三类 App 后端主链路定向回归通过：
   - 企业审批型应用：Hub 安装、DataSrv 注册、workflow 发起、审批实例同步、单 App/全局审批中心回看；
   - 企业普通应用：Hub 安装、DataSrv 注册、business action/query/report/dashboard 结果包；
   - 工具型应用：签名包安装、依赖验签、无 DataSrv 绑定时显式跳过注册、document/artifact 结果审计。

验证命令：

```powershell
cd D:\workprj\aicoder
go test ./gui -run TestInstallMaclawAppPackageFromHubDownloadsAndRecordsInstall -count=1
go test ./gui -run TestInstallSelectedMaclawAppPackageFromHubUsesPackageSignatureTrustForDependencySkill -count=1
go test ./gui -run "TestInstallMaclawAppPackageFromHubDownloadsAndRecordsInstall|TestInstallSelectedMaclawAppPackageFromHubUsesPackageSignatureTrustForDependencySkill|TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv|TestExecuteMaclawAppBusinessOperationRunsPreferredAction|TestExecuteMaclawAppBusinessOperationQueriesPreferredView|TestExecuteMaclawAppBusinessOperationRunsPreferredReport|TestExecuteMaclawAppBusinessOperationRunsPreferredDashboard" -count=1
```

验证结果：

```text
ok github.com/RapidAI/CodeClaw/gui
```

当前链路进一步变为：

```text
Enterprise approval app
  -> workflow approval instance / DataSrv approval readback / result artifacts

Enterprise normal app
  -> Hub install / DataSrv registration / business operation result package / artifact evidence

Tool app
  -> signed Hub install / dependency integrity / local runtime evidence / explicit DataSrv registration skip
```

剩余缺口：下一步继续把企业普通应用和工具型应用上移到前端 App Studio/市场安装/运行 UI 级验收，尤其是安装后的运行结果、附件和布局证据在应用面板中的可见性；同时继续补审批型应用的失败、回滚、依赖修复和真实跨服务黄金路径。

### 推进记录：企业普通应用 DataSrv 回流后运行页治理证据可见（2026-06-30）

本轮继续把企业普通应用从“后端安装审计有证据”推进到“用户在应用面板能回看证据”。此前 DataSrv capabilities 回流已能把已安装企业普通应用恢复成可添加 App，并保留 result contract、test evidence、Hub submission 和 DataSrv registration；但测试只停在 localStorage / installEvidence 对象层，没有打开运行页验证用户能看到这些证据。

本轮完成：

1. 扩展 `restores DataSrv installed enterprise normal app run evidence into app candidates` 前端验收。
2. DataSrv 回流夹具补齐：
   - `workspace_layout`：business workspace、classic split、compact、layout fingerprint、可见区域和 Studio 保存证据；
   - `dependency_verification`：依赖 Skill 数量、无 blocking dependency、`customer-console-skill` 已安装；
   - 既有 Hub submission、review evidence、result contract、test evidence、result coverage、DataSrv registration 继续保留。
3. 加到面板后打开 `Customer Console Installed` 运行页，断言 `Install governance` 面板显示：
   - Source：published Hub capability；
   - Workspace layout：`business_workspace · classic_split · compact`；
   - Result contract：`business_status · 3 types`；
   - Test evidence：`run-customer-imported · proto-customer`；
   - Result coverage：`business_status · Covered: 3`；
   - Dependency verification：`Skill dependencies: 1 · Blocking deps: 0`；
   - DataSrv：`DataSrv bindings registered: 1/1`。

验证命令：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- AppsPage.test.tsx -t "restores DataSrv installed enterprise normal app run evidence into app candidates"
```

验证结果：

```text
1 passed | 203 skipped
```

当前链路进一步变为：

```text
DataSrv app_installations enterprise_normal_app
  -> GUI discovery candidate
  -> Add to panel
  -> installEvidence / importedRunEvidence restored
  -> open app runtime
  -> install governance panel shows Hub / layout / result / test / dependency / DataSrv evidence
```

剩余缺口：工具型应用仍需补同等级“安装后打开运行页可见 document/artifact 结果证据”的 UI 级验收；三类 App 的最终真实跨服务黄金路径仍需继续把 Hub submit/approve/publish/download、GUI install、DataSrv 查询和运行结果回读串到同一条集成样例。

### 推进记录：工具型应用安装后运行页可见 document/artifact 结果证据（2026-06-30）

本轮继续收口工具型应用。此前工具型应用从市场安装后能看到依赖验证和基础 test evidence，但结果合同仍偏向简单 `artifact` 摘要，缺少对“工具型 App 输出文档/附件”的 UI 级回看验收。对 PDF 翻译、合同归档、文档生成等工具型 App 来说，安装后用户必须能在运行页确认这个 App 预期输出的是文档，且测试证据中确实有输出块和附件。

本轮完成：

1. 扩展 `keeps dependency verification visible after single market app install` 前端验收。
2. 工具型市场安装证据升级为：
   - `result_contract.primary = document`
   - result types 覆盖 `document / content / artifact`
   - test evidence 包含 `primaryResult = document`
   - result payload 包含 `document = contract-archive.pdf`
   - outputs 包含 `Archived contract`
   - artifacts 包含 `contract-archive.pdf`
   - result coverage 覆盖 3 类结果且无缺失。
3. 市场安装行内证据现在断言：
   - Workspace layout；
   - Result contract：`document · 3 types`；
   - Test evidence；
   - Result coverage：`document · Covered: 3`；
   - Result package：`Output: 1 · Output artifacts: 1`。
4. 打开已安装工具型 App 运行页后，`Install governance` 面板同样断言上述 document/artifact 结果证据和依赖验证可见。

验证命令：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- AppsPage.test.tsx -t "keeps dependency verification visible after single market app install"
```

验证结果：

```text
1 passed | 203 skipped
```

当前链路进一步变为：

```text
Tool app market install
  -> dependency verification
  -> install evidence carries document result contract
  -> test evidence carries outputs + artifacts
  -> market row shows result package
  -> installed app runtime governance shows document/artifact evidence
```

剩余缺口：企业普通应用和工具型应用的 UI 可见性已继续推进；下一步应优先补最终真实跨服务黄金路径，把 Hub submit/approve/publish/download、GUI install、DataSrv app_installations 查询、审批 workflow 运行和结果回读放进同一条集成样例，同时补审批型应用失败/回滚/依赖修复场景。

### 推进记录：Hub review evidence 摘要进入 GUI 到 DataSrv 注册 payload（2026-06-30）

本轮继续把最终跨服务黄金路径往真实注册层压实。Hub 后端已经能在 `submit -> approve -> publish -> list -> download package` 链路中保留 `review_evidence`，GUI 安装审计也能保存 Hub submission / review evidence；但在签名 Hub 审批型 App 安装到 DataSrv 时，GUI 发出的 app-installations payload 主要携带嵌套 `review_evidence`，扁平 `review_evidence_*` 摘要依赖 DataSrv normalize 后才出现。为了让 GUI、DataSrv、审计代理和运维抓包都能看到同一组稳定字段，本轮把这些摘要提前补到 GUI 注册 payload。

本轮完成：

1. GUI 后端新增 `applyMaclawAppDataSrvReviewEvidenceMetadata`：
   - 兼容扁平 review evidence；
   - 兼容 `{appID: evidence}` / 嵌套 evidence；
   - 提升 `review_evidence_run_id`；
   - 提升 `review_evidence_test_protocol_fingerprint`；
   - 提升 `review_evidence_result_contract_primary`；
   - 提升 `review_evidence_result_coverage_primary / covered_count / missing_count`；
   - 提升 `review_evidence_output_count / artifact_count`；
   - 提升 `review_evidence_approval_status / current_node`。
2. `maclawAppDataSrvInstallationPayloads` 在写入 `metadata.review_evidence` 后立即写入这些稳定摘要字段。
3. 扩展 `TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv`，真实捕获 PUT `/api/v1/data/app-installations/expense-approval` 的 body，并断言 DataSrv 注册 payload 已经包含：
   - Hub published identity：`hub_capability_id / hub_version_key / hub_package_sha256 / hub_review_status`；
   - Hub review evidence summaries：`review_evidence_run_id / approval_status / current_node`；
   - result contract 与 test coverage summaries；
   - dependency verification summaries；
   - 原有 App Studio workspace layout fingerprint / region ids 继续保留。
4. 同时复跑 Hub 后端黄金路径，确认 Hub 侧 `submit -> approve -> publish -> list -> download package` 仍保留 review evidence 和 package signature。

验证命令：

```powershell
cd D:\workprj\aicoder
go test ./gui -run TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv -count=1
go test ./gui -run "TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv|TestInstallSelectedMaclawApprovalAppFromHubPreservesApprovalEvidence|TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult|TestListMaclawAppApprovalInstancesAll" -count=1
go test ./hub/internal/httpapi -run TestCapabilityMaclawAppSubmitApprovePublishListAndDownloadGoldenPath -count=1
```

验证结果：

```text
gui: ok
hub/internal/httpapi: ok
```

当前链路进一步变为：

```text
Hub submit / approve / publish
  -> marketplace metadata review_evidence
  -> published package governance.submission.review_evidence
  -> GUI signed package install
  -> dependency Skill download + integrity metadata
  -> GUI DataSrv app-installations PUT payload
  -> stable hub_* / review_evidence_* / result_contract_* / test_evidence_* / dependency_* summaries
  -> DataSrv normalize/audit/query can consume the same fields without deep JSON parsing
```

剩余缺口：继续把同一条黄金链扩展成“Hub 真实发布包 -> GUI 安装 -> DataSrv app_installations 查询 -> 审批 workflow 运行 -> 单 App/全局审批中心回读同一实例”的单个更完整集成样例；同时补审批型应用失败/回滚/依赖修复场景。

### 推进记录：签名 Hub 安装后按 DataSrv app_installations 查询回读同一注册证据（2026-06-30）

本轮继续把上一段“GUI 写入 DataSrv 注册 payload”推进到“DataSrv 查询回读”。此前 `TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv` 已经捕获 PUT `/api/v1/data/app-installations/expense-approval` body，并断言 Hub identity、review evidence、layout fingerprint、result/test/dependency 摘要都进入注册 payload；但同一测试还没有走 GET `/api/v1/data/app-installations` 查询视角，无法证明运维、GUI 冷启动、MIS Tool 或审计任务能按这些稳定字段找回同一个安装记录。

本轮完成：

1. 扩展签名 Hub 审批型 App 黄金测试中的 mock DataSrv：
   - PUT `/api/v1/data/app-installations/expense-approval` 保存注册 payload；
   - GET `/api/v1/data/app-installations` 支持按 `hub_capability_id`、`hub_version_key`、`hub_review_status`、`workspace_layout_fingerprint` 过滤；
   - 返回同一份注册 payload，模拟真实 DataSrv 查询回读。
2. 安装完成后新增查询断言：
   - `hub_capability_id = cap-expense-approval`
   - `hub_version_key = enterprise_hub:skill:maclaw-app:expense-approval@pkg`
   - `hub_review_status = published`
   - `workspace_layout_fingerprint = layout-expense-golden`
3. 查询结果必须回读到同一个 `expense-approval` app，并保留：
   - Hub published identity；
   - `review_evidence_run_id = run-expense-golden`；
   - App Studio layout fingerprint；
   - 后续审批 workflow 运行、DataSrv progress/final sync、单 App/全局审批中心 handled lane 回读仍在同一测试中继续通过。

验证命令：

```powershell
cd D:\workprj\aicoder
go test ./gui -run TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv -count=1
go test ./gui -run "TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv|TestInstallSelectedMaclawApprovalAppFromHubPreservesApprovalEvidence|TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult|TestListMaclawAppApprovalInstancesAll" -count=1
go test ./hub/internal/httpapi -run TestCapabilityMaclawAppSubmitApprovePublishListAndDownloadGoldenPath -count=1
cd D:\workprj\aicoder\datasrv
go test ./structureddata -run "TestUpsertAppInstallationNormalizesHubSubmissionAndReviewEvidence|TestHTTPServerCoreEndpoints" -count=1
```

验证结果：

```text
gui: ok
hub/internal/httpapi: ok
datasrv/structureddata: ok
```

当前链路进一步变为：

```text
Signed Hub package install
  -> dependency Skill integrity install
  -> GUI registers DataSrv app_installations payload
  -> DataSrv query by Hub identity + workspace layout fingerprint
  -> same installed app metadata read back
  -> approval workflow start / progress / final sync
  -> single App and global approval center read the same handled instance
```

剩余缺口：最终黄金链的关键段已经合并得更紧，下一步可以继续补“真实 Hub submit/approve/publish 产出的包直接喂给 GUI 安装测试”的组合样例，减少 Hub handler 测试和 GUI 安装测试之间仍存在的夹具间隙；同时补审批型应用失败/回滚/依赖修复的端到端回看。

### 推进记录：GUI 下载 Hub 包时校验已发布治理语义和审核证据（2026-06-30）

本轮继续收紧 Hub 后端发布包与 GUI 安装之间的夹具间隙。此前 GUI `DownloadMaclawAppPackageFromHub` 已经会校验签名并解析 MaClaw App entries，但只要包签名正确，即使包内缺少 Hub 已发布 submission、版本键或 review evidence，也可能进入后续安装流程。这会让测试夹具看起来能跑通，但真实市场分发语义没有被 GUI 安装入口强制执行。

本轮完成：

1. GUI 后端新增 `validateDownloadedMaclawAppHubPackageGovernance`，在签名校验和 entries 解析后执行，要求 Hub 下载包必须具备：
   - `package_signature`
   - `package_sha256`
   - 至少一个 App entry
   - 每个 App 的 `governance.submission`
   - submission `status = published`
   - submission `capability_id` 与请求的 capability 一致
   - submission `version_key`
   - submission `review_evidence`
2. `DownloadMaclawAppPackageFromHub` 现在会拒绝“签名正确但缺少已发布治理证据”的包，防止未完成 Hub 审核/发布语义的 MaClaw App 被安装。
3. 补充 GUI 测试：
   - `TestDownloadMaclawAppPackageFromHubTrustsSignedPackageFingerprint` 的成功包补齐 published submission 和 review evidence。
   - 新增 `TestDownloadMaclawAppPackageFromHubRejectsSignedPackageWithoutPublishedGovernance`，验证签名正确但缺少 Hub governance submission 的包会被拒绝。
   - `TestInstallSelectedMaclawAppPackageFromHubUsesPackageSignatureTrustForDependencySkill` 的签名安装包补齐 published submission，继续验证依赖 Skill 签名信任与安装证据。
4. 复跑审批型黄金链路、Hub 发布黄金链路和 DataSrv review evidence 归一化回归，确认新校验与现有服务端包结构兼容。

验证命令：

```powershell
cd D:\workprj\aicoder
go test ./gui -run "TestDownloadMaclawAppPackageFromHubTrustsSignedPackageFingerprint|TestDownloadMaclawAppPackageFromHubRejectsSignedPackageWithoutPublishedGovernance|TestDownloadMaclawAppPackageFromHubRejectsInvalidPackageSignature|TestInstallSelectedMaclawAppPackageFromHubUsesPackageSignatureTrustForDependencySkill|TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv" -count=1
go test ./hub/internal/httpapi -run TestCapabilityMaclawAppSubmitApprovePublishListAndDownloadGoldenPath -count=1
cd D:\workprj\aicoder\datasrv
go test ./structureddata -run "TestUpsertAppInstallationNormalizesHubSubmissionAndReviewEvidence|TestHTTPServerCoreEndpoints" -count=1
```

验证结果：

```text
gui: ok
hub/internal/httpapi: ok
datasrv/structureddata: ok
```

当前链路进一步变为：

```text
Hub submit / approve / publish
  -> signed package download
  -> GUI verifies package signature
  -> GUI verifies published governance submission + review_evidence
  -> dependency Skill integrity install
  -> DataSrv registration/query
  -> approval workflow/runtime/result readback
```

剩余缺口：下一步继续把“Hub handler 真实发布包直接进入 GUI 安装测试”的组合样例压实，或先补审批型应用失败/拒绝/需关注/依赖修复/回滚重试等异常闭环；App Studio 制作、测试、上传的可视化产品流也仍需继续推进。

### 推进记录：GUI 下载校验对齐 Hub handler 顶层包证据（2026-06-30）

本轮继续把 GUI 下载包校验向真实 Hub handler 输出结构对齐。Hub `CapabilityMaclawAppPackageHandler` 返回的已发布 MaClaw App 包不仅在 entry 内写入 `governance.submission.review_evidence`，还会在包顶层写入 `capability`、`review_evidence`、`maclaw_app_review_evidence`、`package_sha256`、`package_signature` 和 `resolved_dependencies`。上一轮 GUI 已经要求 entry submission 是 published，但还没有强制顶层 package review evidence 存在。

本轮完成：

1. `validateDownloadedMaclawAppHubPackageGovernance` 继续收紧：
   - 如果 package `capability.status/state` 存在，必须为 `published`；
   - package 顶层必须存在 `review_evidence` / `reviewEvidence` / `maclaw_app_review_evidence`；
   - 仍保持先校验 entry submission，再校验顶层 package evidence，错误信息能指向真正缺失层级。
2. GUI 下载测试 helper `markMaclawAppPackageAsPublishedHubDownloadTest` 对齐真实 Hub handler：
   - 写入 package 顶层 `capability`；
   - 写入 package 顶层 `review_evidence` 和 `maclaw_app_review_evidence`；
   - entry 内继续写入 `governance.submission.review_evidence`。
3. 新增负例 `TestDownloadMaclawAppPackageFromHubRejectsSignedPackageWithoutTopLevelReviewEvidence`：
   - 包签名正确；
   - entry submission/review evidence 完整；
   - 但顶层 package review evidence 被删除；
   - GUI 下载必须拒绝，防止市场包审核摘要在分发边界丢失。
4. 审批型黄金测试的 mock Hub 包补齐顶层 `capability` 和 package review evidence，使它更接近 Hub 后端真实下载响应。

验证命令：

```powershell
cd D:\workprj\aicoder
go test ./gui -run "TestDownloadMaclawAppPackageFromHubTrustsSignedPackageFingerprint|TestDownloadMaclawAppPackageFromHubRejectsSignedPackageWithoutPublishedGovernance|TestDownloadMaclawAppPackageFromHubRejectsSignedPackageWithoutTopLevelReviewEvidence|TestDownloadMaclawAppPackageFromHubRejectsInvalidPackageSignature|TestInstallSelectedMaclawAppPackageFromHubUsesPackageSignatureTrustForDependencySkill|TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv" -count=1
go test ./hub/internal/httpapi -run "TestCapabilityMaclawAppSubmitApprovePublishListAndDownloadGoldenPath|TestCapabilityMaclawAppPackageDownloadReturnsApprovedPack|TestCapabilityMaclawAppPackageDownloadRequiresPublishedStatus" -count=1
cd D:\workprj\aicoder\datasrv
go test ./structureddata -run "TestUpsertAppInstallationNormalizesHubSubmissionAndReviewEvidence|TestHTTPServerCoreEndpoints" -count=1
```

验证结果：

```text
gui: ok
hub/internal/httpapi: ok
datasrv/structureddata: ok
```

当前链路进一步变为：

```text
Hub published package
  -> package capability.status = published
  -> package top-level review_evidence/maclaw_app_review_evidence
  -> entry governance.submission.review_evidence
  -> GUI download verifies all published package evidence layers
  -> install/dependency/DataSrv/workflow chain continues
```

剩余缺口：Hub handler 与 GUI 之间仍不是同一个测试进程直接串联，下一步可以继续通过共享 fixture builder 或外部 black-box server 方式减少 mock 包手写比例；同时推进审批异常态和 App Studio 可视化制作/测试/上传的产品级验收。

### 推进记录：审批异常态单 App 与全局审批中心一致回读（2026-06-30）

本轮转向审批型应用的非通过结果态。此前 `approved` 黄金链路已经能证明 workflow Skill 运行结果进入 DataSrv review/final sync，并能被单 App 和全局审批中心回读；`attention`、`rejected`、`timeout`、`cancelled` 等异常/非通过状态也已有 workflow 写回和单 App lane 验证，但还没有统一证明全局审批中心能看到同一条实例、同一套结果包和同一组状态字段。

本轮完成：

1. 新增测试辅助断言 `assertMaclawAppApprovalReadbackSameInstanceForTest`，统一校验：
   - 单 App 工作台与全局审批中心的 `app_id / approval_id / instance_id / workflow_decision_id` 一致；
   - `status / lane / current_node / result_status / business_status` 一致；
   - outputs/artifacts 数量一致。
2. `attention` 视图型结果补全全局回读：
   - workflow 结果为 `attention`；
   - 不调用 DataSrv `/review`，只更新业务记录为 view-only evidence；
   - 单 App `attention` lane 与全局 `attention` lane 均能回读同一实例。
3. `rejected`、`timeout`、`cancelled` 终态补全全局回读：
   - workflow result sync 会调用 DataSrv `/review`；
   - 单 App `handled` lane 与全局 `handled` lane 均能回读同一实例；
   - DataSrv 查询同时覆盖 app-scoped 查询 `app_id=expense-approval&lane=handled&limit=10` 和 global 查询 `lane=handled&limit=10`；
   - 输出块和 workflow decision id 在两种视角一致。

验证命令：

```powershell
cd D:\workprj\aicoder
go test ./gui -run "TestStartMaclawAppApprovalWorkflowRunsAttentionViewOnlyWorkflowResult|TestStartMaclawAppApprovalWorkflowRunsRejectedWorkflowResult|TestStartMaclawAppApprovalWorkflowRunsTimeoutWorkflowResult|TestStartMaclawAppApprovalWorkflowRunsCancelledWorkflowResult" -count=1
go test ./gui -run "TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult|TestStartMaclawAppApprovalWorkflowRunsAttentionViewOnlyWorkflowResult|TestStartMaclawAppApprovalWorkflowRunsRejectedWorkflowResult|TestStartMaclawAppApprovalWorkflowRunsTimeoutWorkflowResult|TestStartMaclawAppApprovalWorkflowRunsCancelledWorkflowResult|TestListMaclawAppApprovalInstancesAll|TestListMaclawAppApprovalInstancesAllLoadsDataSrvLane|TestListMaclawAppApprovalInstancesAllInfersDataSrvLanesForCurrentUser" -count=1
cd D:\workprj\aicoder\datasrv
go test ./structureddata -run "TestHTTPServerCoreEndpoints|TestUpsertAppInstallationNormalizesHubSubmissionAndReviewEvidence" -count=1
```

验证结果：

```text
gui: ok
datasrv/structureddata: ok
```

当前链路进一步变为：

```text
workflow Skill result
  -> approved / rejected / timeout / cancelled => DataSrv review final sync
  -> attention => view-only business record update
  -> local approval registry + DataSrv approvals readback
  -> single App lane and global approval center show the same instance/result package
```

剩余缺口：审批异常态已经覆盖了主要终态和 view-only 关注态；后续还需要继续补 requires_input/补充材料后的继续运行、依赖修复后重试、失败回滚、以及 App Studio 可视化制作/测试/上传到 Hub 的更真实产品级验收。

### 推进记录：requires_input 补充材料后同实例续跑并进入 handled（2026-06-30）

本轮继续补齐审批型应用里的“需补充材料”闭环。`requires_input` 不应被视为最终失败或最终完成，它是审批实例暂停在申请人侧、等待补录数据/文件后继续运行 workflow Skill 的中间状态。此前已经覆盖了 workflow 返回 `requires_input` 时不调用 DataSrv `/review`、只写 progress，并显示在单 App `my_requests` lane；也已有续跑测试证明不会新建 DataSrv approval。但仍缺少两个产品级断言：补充后最终通过必须从 `my_requests` 移出，并且单 App 与全局审批中心都能回读同一条实例。

本轮完成：

1. 补强 `TestStartMaclawAppApprovalWorkflowContinuesRequiresInputWithSupplement`：
   - 已存在 `requires_input` 实例 `wf-input-runner-2 / approval-input-runner-2`；
   - 用户提交 `FormData` 和 `BusinessPayload` 作为补充材料；
   - `StartMaclawAppApprovalWorkflow` 使用 `ContinueFromID` 和 `ApprovalID` 续跑；
   - workflow Skill 返回 `approved`；
   - DataSrv 只收到 existing approval 的 `/progress` 与 `/review`，不会重新 POST `/records/{id}/approvals`；
   - final review 保留同一 `workflow_instance_id`、`approval_id`、`workflow_decision_id`；
   - 单 App `handled` lane 和全局 `handled` lane 均能回读同一实例；
   - 续跑完成后单 App `my_requests` lane 不再显示该已 approved 实例。
2. 修正 lane 过滤：
   - `my_requests` 现在继续显示 `requires_input`；
   - 对普通 `lane=my_requests` 的记录，只在 `draft/pending` 未完成状态下显示；
   - `approved/rejected/failed/cancelled/timeout` 等终态即使历史 lane 残留为 `my_requests`，也不会污染“我的申请/待补充”列表。
3. 补充查询形态断言：
   - app-scoped handled 查询：`app_id=expense-approval&lane=handled&limit=10`；
   - global handled 查询：`lane=handled&limit=10`；
   - 两者返回同一 workflow instance/result package。

验证命令：

```powershell
cd D:\workprj\aicoder
go test ./gui -run "TestStartMaclawAppApprovalWorkflowRunsRequiresInputWorkflowResult|TestStartMaclawAppApprovalWorkflowContinuesRequiresInputWithSupplement" -count=1
go test ./gui -run "TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult|TestStartMaclawAppApprovalWorkflowRunsRequiresInputWorkflowResult|TestStartMaclawAppApprovalWorkflowContinuesRequiresInputWithSupplement|TestStartMaclawAppApprovalWorkflowRunsAttentionViewOnlyWorkflowResult|TestStartMaclawAppApprovalWorkflowRunsRejectedWorkflowResult|TestStartMaclawAppApprovalWorkflowRunsTimeoutWorkflowResult|TestStartMaclawAppApprovalWorkflowRunsCancelledWorkflowResult|TestListMaclawAppApprovalInstancesAll|TestListMaclawAppApprovalInstancesAllLoadsDataSrvLane|TestListMaclawAppApprovalInstancesAllInfersDataSrvLanesForCurrentUser" -count=1
cd D:\workprj\aicoder\datasrv
go test ./structureddata -run "TestHTTPServerCoreEndpoints|TestUpsertAppInstallationNormalizesHubSubmissionAndReviewEvidence" -count=1
```

验证结果：

```text
gui: ok
datasrv/structureddata: ok
```

当前链路进一步变为：

```text
workflow Skill returns requires_input
  -> DataSrv approval progress
  -> single App my_requests / global request-side visibility
  -> user submits supplemental FormData/BusinessPayload
  -> same approval instance continues
  -> workflow Skill returns approved
  -> DataSrv review final sync
  -> single App handled + global handled read back same instance
  -> my_requests no longer shows completed instance
```

剩余缺口：审批型 workflow 的主要状态闭环已经更完整；后续优先继续补依赖缺失/修复后重试、workflow 运行失败/回滚、以及 App Studio 从制作到测试上传 Hub 的更真实 UI/集成验收。

### 推进记录：workflow Skill 运行失败进入 failed 结果闭环（2026-07-01）

本轮补齐审批型应用的 workflow 运行失败路径。企业审批型 App 不能把 workflow Skill 的执行失败直接变成 API error 丢给用户，也不能让审批实例卡在 pending；它应该生成可审计的 `failed` 审批结果，带错误证据、输出块、DataSrv final review，同步进入单 App 和全局审批中心的 handled lane。

本轮完成：

1. 补强 `TestStartMaclawAppApprovalWorkflowRecordsFailedWorkflowSkillResult`：
   - workflow Skill 命令退出失败；
   - `StartMaclawAppApprovalWorkflow` 不返回 error，而是返回 `workflow_run.ran=false` 和 `workflow_run.error`；
   - 本地审批实例进入 `status=failed / lane=handled / business_status=workflow_failed / result_status=failed`；
   - result payload 保留 `approval_result=failed`、`business_status=workflow_failed`、`result_status=failed`、`error`、`text`；
   - outputs 保留 `Workflow failed` 输出块；
   - DataSrv `/review` 收到 `decision=failed`、`workflow_node_id=workflow.failed`、失败业务状态和输出块；
   - 单 App `handled` lane 与全局 `handled` lane 均能回读同一实例；
   - app-scoped handled 查询和 global handled 查询都被覆盖。
2. 与上一轮 lane 修正组合验证：
   - `requires_input` 仍留在 `my_requests`；
   - 补充后 `approved` 不再污染 `my_requests`；
   - `failed` 与其他终态一样稳定进入 handled。

验证命令：

```powershell
cd D:\workprj\aicoder
go test ./gui -run TestStartMaclawAppApprovalWorkflowRecordsFailedWorkflowSkillResult -count=1
go test ./gui -run "TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult|TestStartMaclawAppApprovalWorkflowRunsRequiresInputWorkflowResult|TestStartMaclawAppApprovalWorkflowContinuesRequiresInputWithSupplement|TestStartMaclawAppApprovalWorkflowRunsAttentionViewOnlyWorkflowResult|TestStartMaclawAppApprovalWorkflowRunsRejectedWorkflowResult|TestStartMaclawAppApprovalWorkflowRunsTimeoutWorkflowResult|TestStartMaclawAppApprovalWorkflowRunsCancelledWorkflowResult|TestStartMaclawAppApprovalWorkflowRecordsFailedWorkflowSkillResult|TestListMaclawAppApprovalInstancesAll|TestListMaclawAppApprovalInstancesAllLoadsDataSrvLane|TestListMaclawAppApprovalInstancesAllInfersDataSrvLanesForCurrentUser" -count=1
cd D:\workprj\aicoder\datasrv
go test ./structureddata -run "TestHTTPServerCoreEndpoints|TestUpsertAppInstallationNormalizesHubSubmissionAndReviewEvidence" -count=1
```

验证结果：

```text
gui: ok
datasrv/structureddata: ok
```

当前链路进一步变为：

```text
workflow Skill execution error
  -> workflow_run.ran=false + error evidence
  -> approval instance status=failed / lane=handled
  -> DataSrv review decision=failed
  -> single App handled + global handled read back same failed instance
```

剩余缺口：失败路径已经具备结果反馈和审计回看；后续继续补依赖缺失/依赖修复后重试，以及 Hub/App Studio 真实 UI 操作链路里的失败提示与修复入口。

### 推进记录：依赖修复后安装并运行审批 workflow 闭环（2026-07-01）

本轮继续补齐 MaClaw App 作为“带特殊 app 数据的超级 Skill”时的依赖修复链路。企业审批型应用安装前必须检查依赖 Skill；如果审批 workflow Skill 缺失，安装应被阻断；用户执行依赖修复后，系统应能从企业 Hub 能力引用安装缺失 Skill，再重新登记 App，并允许审批 workflow 正常运行。

本轮完成：

1. 新增并修通 `TestMaclawAppDependencyRepairAllowsInstallAndWorkflowRun`：
   - 初始环境只安装 app super skill `expense-super-skill`；
   - App package 声明必需 workflow Skill `expense-workflow`，来源为 `enterprise_hub`，`install_ref=cap-expense-workflow`；
   - `PlanMaclawAppInstall` 能识别缺失 workflow 依赖，并设置 `HasMissingRequired / HasBlockingDependency`；
   - `RecordMaclawAppInstall` 在依赖缺失时拒绝安装；
   - `InstallMaclawAppDependencies` 调用企业 Hub 安装入口修复 `expense-workflow`；
   - 修复后的 plan 清除 blocking dependency，workflow dependency 变为 `installed / ready`；
   - 再次 `RecordMaclawAppInstall` 成功，并把修复后的依赖摘要写入 DataSrv registration metadata；
   - 安装后发起审批 workflow，workflow Skill 正常执行并返回 approved；
   - DataSrv `/review` 收到最终审批结果；
   - 单 App handled lane 与全局 handled lane 均能回读同一审批实例。

2. 对 DataSrv 注册 payload 的测试断言改为稳定合同字段：
   - `metadata.dependency_count = 2`；
   - `metadata.has_blocking_dependency = false`；
   - `metadata.dependency_verification.blockedCount = 0`；
   - `metadata.dependency_verification.has_blocking_dependency = false`。

验证命令：

```powershell
cd D:\workprj\aicoder
go test ./gui -run TestMaclawAppDependencyRepairAllowsInstallAndWorkflowRun -count=1
go test ./gui -run "TestMaclawAppDependencyRepairAllowsInstallAndWorkflowRun|TestInstallMaclawAppDependenciesInstallsHubBackedSources|TestInstallMaclawAppDependenciesClassifiesInstallFailures|TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult|TestStartMaclawAppApprovalWorkflowRecordsFailedWorkflowSkillResult" -count=1
go test ./hub/internal/httpapi -run "TestCapabilityMaclawAppSubmitApprovePublishListAndDownloadGoldenPath|TestCapabilityMaclawAppPackageDownloadReturnsApprovedPack|TestCapabilityMaclawAppPackageDownloadRequiresPublishedStatus" -count=1
cd D:\workprj\aicoder\datasrv
go test ./structureddata -run "TestHTTPServerCoreEndpoints|TestUpsertAppInstallationNormalizesHubSubmissionAndReviewEvidence" -count=1
```

验证结果：

```text
gui: ok
hub/internal/httpapi: ok
datasrv/structureddata: ok
```

当前链路进一步变为：

```text
install plan detects missing workflow Skill
  -> install is blocked
  -> dependency repair installs workflow Skill from enterprise Hub install_ref
  -> repaired install plan is ready
  -> App install registration writes dependency evidence to DataSrv
  -> approval workflow Skill runs
  -> DataSrv final review sync
  -> single App handled + global handled read back the same instance
```

剩余缺口：依赖缺失/修复后的后端闭环已经可验收；下一步继续把“真实 Hub submit/approve/publish 产出的 signed package 直接进入 GUI 安装、依赖 Skill 下载验签安装、DataSrv 查询、审批 workflow 发起和结果回读”压成同一条跨服务黄金样例，并继续补 App Studio 可视化制作、测试、上传 Hub 的 UI 级自动化验收。

### 推进记录：GUI 安装侧对齐 Hub handler 下载包证据（2026-07-01）

本轮继续收窄 Hub 后端黄金路径和 GUI 安装运行黄金路径之间的夹具空隙。当前技术限制是：Hub 的真实 handler 位于 `hub/internal/httpapi`，GUI Go 包不能直接 import internal handler 做同进程跨包集成测试；因此先把 GUI 侧 httptest 返回包调整为更贴近 `CapabilityMaclawAppPackageHandler` 的真实输出，并在 GUI 安装用例中显式断言这些 Hub 发布证据没有在“选择子 App -> 安装 -> DataSrv 注册 -> 审批运行”过程中丢失。

本轮完成：

1. 扩展 `TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv`：
   - 安装返回的 selected package 必须保留 `source=enterprise_hub`；
   - top-level `capability.id` 必须是 `cap-expense-approval`；
   - top-level `capability.status` 必须是 `published`；
   - top-level `capability.current_version_key` 必须是 `enterprise_hub:skill:maclaw-app:expense-approval@pkg`；
   - top-level `review_evidence.expense-approval.run_id` 必须是 `run-expense-golden`；
   - top-level review evidence 必须保留 `approval_status=approved`。
2. 扩展 selected app entry 的 Hub submission 断言：
   - `submission.status=published`；
   - `submission.capability_id=cap-expense-approval`；
   - `submission.market_capability_id=expense-approval`；
   - `submission.version_key=enterprise_hub:skill:maclaw-app:expense-approval@pkg`；
   - `submission.package_signature` 必须存在；
   - `submission.review_evidence.expense-approval.run_id=run-expense-golden`。
3. 修正 GUI 测试夹具，使其模拟 Hub `applyMaclawAppReviewMetadataToEntry` 的真实行为：包级 `package_signature` 也写入 entry `governance.submission.package_signature`。

验证命令：

```powershell
cd D:\workprj\aicoder
go test ./gui -run TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv -count=1
go test ./gui -run "TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv|TestDownloadMaclawAppPackageFromHubTrustsSignedPackageFingerprint|TestDownloadMaclawAppPackageFromHubRejectsSignedPackageWithoutPublishedGovernance|TestDownloadMaclawAppPackageFromHubRejectsSignedPackageWithoutTopLevelReviewEvidence|TestMaclawAppDependencyRepairAllowsInstallAndWorkflowRun" -count=1
go test ./hub/internal/httpapi -run "TestCapabilityMaclawAppSubmitApprovePublishListAndDownloadGoldenPath|TestCapabilityMaclawAppPackageDownloadReturnsApprovedPack|TestCapabilityMaclawAppPackageDownloadRequiresPublishedStatus" -count=1
cd D:\workprj\aicoder\datasrv
go test ./structureddata -run "TestHTTPServerCoreEndpoints|TestUpsertAppInstallationNormalizesHubSubmissionAndReviewEvidence" -count=1
```

验证结果：

```text
gui: ok
hub/internal/httpapi: ok
datasrv/structureddata: ok
```

当前链路进一步变为：

```text
Hub handler download package contract
  -> top-level published capability + review evidence
  -> entry governance.submission published identity + package signature
  -> GUI selected package filtering keeps the same evidence
  -> dependency Skill download/signature install
  -> DataSrv app_installations registration
  -> approval workflow run and readback
```

剩余缺口：GUI 包仍不能直接调用 `hub/internal/httpapi` handler，所以这还不是同进程真实 Hub handler 到 GUI 的端到端测试。下一步可以继续通过两条路推进：一是抽出共享的 Hub package fixture/contract builder，供 Hub handler 测试和 GUI 测试共用；二是增加外部 black-box server 级集成样例，让真实 Hub handler 通过 HTTP 产出的包直接被 GUI 安装测试消费。

### 推进记录：Hub 下载包合同出口抽取并加固 golden path（2026-07-01）

本轮继续推进上一段的第一条路径：先把 Hub handler 内部的下载包构造逻辑抽成明确的合同出口，减少后续 GUI 安装测试、Hub handler 测试和 black-box server 集成之间的字段漂移。

本轮完成：

1. 在 `hub/internal/httpapi/marketplace_handlers.go` 新增 `enterpriseMaclawAppDownloadPackage`：
   - 统一输出 `schema=maclaw.app.pack.v1`；
   - 统一输出 `privateMarker=x_maclaw_apps`；
   - 统一输出 `source=enterprise_hub`；
   - 统一输出 `package_signature`；
   - 统一输出 published `capability` block；
   - 统一输出 `package_sha256`；
   - 统一输出 `review_evidence` / `maclaw_app_review_evidence`；
   - 统一输出 `resolved_dependencies`；
   - 统一输出 selected app entry。
2. `CapabilityMaclawAppPackageHandler` 改为调用该合同出口，不再在 handler 尾部手写 package map。
3. 扩展 Hub golden path `TestCapabilityMaclawAppSubmitApprovePublishListAndDownloadGoldenPath`：
   - 下载包必须携带 `source=enterprise_hub`；
   - 下载包必须携带 published `capability.id/status/current_version_key`；
   - 下载包必须携带 top-level review evidence 和 package signature；
   - 下载包必须携带 2 条 resolved dependencies；
   - app entry 必须携带 published submission、package signature 和 review evidence。

验证命令：

```powershell
cd D:\workprj\aicoder
go test ./hub/internal/httpapi -run TestCapabilityMaclawAppSubmitApprovePublishListAndDownloadGoldenPath -count=1
go test ./hub/internal/httpapi -run "TestCapabilityMaclawAppSubmitApprovePublishListAndDownloadGoldenPath|TestCapabilityMaclawAppPackageDownloadReturnsApprovedPack|TestCapabilityMaclawAppPackageDownloadRequiresPublishedStatus" -count=1
go test ./gui -run "TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv|TestDownloadMaclawAppPackageFromHubTrustsSignedPackageFingerprint|TestDownloadMaclawAppPackageFromHubRejectsSignedPackageWithoutPublishedGovernance|TestDownloadMaclawAppPackageFromHubRejectsSignedPackageWithoutTopLevelReviewEvidence|TestMaclawAppDependencyRepairAllowsInstallAndWorkflowRun" -count=1
cd D:\workprj\aicoder\datasrv
go test ./structureddata -run "TestHTTPServerCoreEndpoints|TestUpsertAppInstallationNormalizesHubSubmissionAndReviewEvidence" -count=1
```

验证结果：

```text
hub/internal/httpapi: ok
gui: ok
datasrv/structureddata: ok
```

当前链路进一步变为：

```text
Hub submit/approve/publish
  -> CapabilityMaclawAppPackageHandler
  -> enterpriseMaclawAppDownloadPackage contract出口
  -> published capability + review evidence + submission + resolved dependencies
  -> GUI signed Hub install test 对齐同一字段合同
  -> dependency install + DataSrv registration + approval workflow run
```

剩余缺口：下载包合同出口已经明确，但 GUI 侧仍是用 httptest 模拟 Hub HTTP 响应。下一步继续推进第二条路径：做一个外部 black-box server 级测试，让真实 Hub handler 通过 HTTP 产出的 signed package 直接被 GUI 安装逻辑消费；如果跨包/模块边界仍阻碍测试组织，则先把可复用 package contract fixture 移到非 `internal` 的共享测试包或 `testdata`。

### 推进记录：Hub/GUI 共享企业审批型 App 合同 fixture（2026-07-01）

本轮继续处理真实 black-box 前的测试组织问题。直接在 GUI 包中 import `hub/internal/httpapi` 不可行，反向在 Hub 测试中 import GUI App 也会碰到 GUI 是 `package main` 的边界；因此先把可复用的企业审批型 App 基础包移到仓库根 `internal/testfixtures`，让 Hub handler 测试和 GUI 下载治理测试都从同一份 MaClaw App 合同 fixture 出发。

本轮完成：

1. 新增 `internal/testfixtures.ReadyEnterpriseApprovalMaclawAppSubmitPackage`：
   - 企业审批型 App：`approval-ready-app`；
   - app skill：`approval-ready-app-skill`；
   - workflow skill：`approval-ready-workflow`；
   - workspace layout：request form / approval lane / result panel；
   - result contract：`approval_result` + content + artifact；
   - workflow contract：要求 workflow_result / approval_instance / outputs / artifacts；
   - test evidence：approved approval instance、outputs、artifact、result coverage；
   - dependency verification：2 个依赖、无 missing/blocking。
2. Hub `readyEnterpriseApprovalMaclawAppSubmitPackage` 改为转调共享 fixture，Hub submit/approve/publish/download golden path 继续使用同一份基础包。
3. GUI 新增 `TestDownloadMaclawAppPackageFromHubAcceptsSharedApprovalFixture`：
   - 从共享 fixture 出发；
   - 注入 published capability / submission；
   - 注入 package signature；
   - 通过 `DownloadMaclawAppPackageFromHub` 下载；
   - 断言 GUI 能接受该企业审批型 published Hub 包，并保留 capability、review evidence 和 app id。

验证命令：

```powershell
cd D:\workprj\aicoder
go test ./hub/internal/httpapi -run "TestCapabilityMaclawAppSubmitApprovePublishListAndDownloadGoldenPath|TestCapabilityMaclawAppPackageDownloadReturnsApprovedPack|TestCapabilityMaclawAppPackageDownloadRequiresPublishedStatus" -count=1
go test ./gui -run "TestDownloadMaclawAppPackageFromHubAcceptsSharedApprovalFixture|TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv|TestDownloadMaclawAppPackageFromHubTrustsSignedPackageFingerprint" -count=1
go test ./gui -run "TestDownloadMaclawAppPackageFromHubAcceptsSharedApprovalFixture|TestDownloadMaclawAppPackageFromHubRejectsSignedPackageWithoutPublishedGovernance|TestDownloadMaclawAppPackageFromHubRejectsSignedPackageWithoutTopLevelReviewEvidence|TestMaclawAppDependencyRepairAllowsInstallAndWorkflowRun" -count=1
cd D:\workprj\aicoder\datasrv
go test ./structureddata -run "TestHTTPServerCoreEndpoints|TestUpsertAppInstallationNormalizesHubSubmissionAndReviewEvidence" -count=1
cd D:\workprj\aicoder
go test ./internal/testfixtures -count=1
```

验证结果：

```text
hub/internal/httpapi: ok
gui: ok
datasrv/structureddata: ok
internal/testfixtures: no test files
```

当前链路进一步变为：

```text
shared enterprise approval app fixture
  -> Hub submit/approve/publish/download golden path
  -> Hub download package contract出口
  -> GUI DownloadMaclawAppPackageFromHub governance validation
  -> GUI signed install / dependency repair / DataSrv registration 回归
```

剩余缺口：共享 fixture 已经减少 Hub 与 GUI 的包合同漂移，但还不是“真实 Hub HTTP handler 产出的包直接被 GUI 安装入口消费”。下一步可以继续做外部 black-box server 集成，或者先把 GUI 安装入口的核心逻辑抽出到非 `package main` 的可导入包，便于 Hub 测试进程直接调用安装链。

### 推进记录：企业普通应用运行页结果包 UI 验收补齐（2026-07-01）

本轮从三类 App 的运行界面可见性继续收口。审批型应用和工具型应用此前已经覆盖了安装后运行页能看到结果包、输出块和文件产物；企业普通应用的 DataSrv 回流测试虽然已经保存了 `result_payload`、`outputs`、`result_coverage` 和依赖证据，但运行页只断言了 install governance、result contract、coverage、dependency verification 和 DataSrv 注册状态，没有锁住“普通企业应用也能在传统软件式运行界面看到结果包/附件”的用户可见行为。

本轮完成：

1. 扩展 `restores DataSrv installed enterprise normal app run evidence into app candidates` 前端验收：
   - DataSrv 回流的企业普通应用 `Customer Console Installed` 增加 `test_evidence.artifacts`；
   - `importedRunEvidence.artifacts` 必须保留 `customer-renewal.pdf`；
   - `installEvidence.test_evidence.artifacts` 必须保留同一附件；
   - 打开安装后的 App 运行页，在 `Install governance` 区域必须看到 `Result package`；
   - 运行页必须显示 `Output: 1 · Output artifacts: 1`。
2. 组合回归同时覆盖三类 App：
   - 企业审批型：`installs an approved Hub approval app with runtime install evidence`；
   - 企业普通应用：`restores DataSrv installed enterprise normal app run evidence into app candidates`；
   - 工具型应用：`keeps dependency verification visible after single market app install`。

验证命令：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "restores DataSrv installed enterprise normal app run evidence into app candidates"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "restores DataSrv installed enterprise normal app run evidence into app candidates|keeps dependency verification visible after single market app install|installs an approved Hub approval app with runtime install evidence"
```

验证结果：

```text
targeted enterprise normal app UI: 1 passed
three app-kind runtime evidence UI: 3 passed
```

当前链路进一步变为：

```text
DataSrv app_installations enterprise_normal_app
  -> GUI candidate restore
  -> importedRunEvidence result payload + outputs + artifacts
  -> installEvidence test_evidence result package
  -> installed App runtime governance
  -> Result package visible in enterprise normal app UI
```

剩余缺口：三类 App 的运行页结果/依赖证据可见性都有 targeted UI 验收，但全量 `AppsPage.test.tsx` 和更真实的端到端点击流仍需要继续周期性补跑；App Studio 从制作、测试、提交、Hub 审核发布到市场安装的黑盒链路也还没完全合并成单条自动化样例。

### 推进记录：签名 Hub 审批 App 运行同步 payload 审计补强（2026-07-01）

本轮继续压紧“真实跨服务黄金路径”中的运行同步段。此前 `TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv` 已经覆盖签名 Hub 包下载、依赖 Skill 安装、DataSrv app_installations 登记、审批 workflow 发起、结果回写、单 App/全局审批中心回读；但 DataSrv mock 主要通过最终列表响应验证结果，缺少对 `/progress` 和 `/review` 请求体本身的审计断言。这样如果运行同步 payload 丢失 workflow 节点、result payload、outputs、artifacts 或补充材料，仍可能被后续 mock 列表数据掩盖。

本轮完成：
1. 扩展 `TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv`：
   - 捕获 DataSrv `/api/v1/data/approvals/{id}/progress` 与 `/review` 请求体；
   - 断言普通审批最终 `review` payload 保留 `decision=approved`、`workflow_instance_id`、`workflow_node_id`、`workflow_version`；
   - 断言最终 `review` payload 保留 `result_payload`、输出块和 `expense-golden-approved.pdf` 文件产物；
   - 断言 requires_input 补充材料后继续运行的最终 `review` payload 保留同一 `wf-golden-2`、`decision-golden-2` 和 `supplemental_input.form_data.invoice_attachment`；
   - 继续保留“安装登记必须先于运行”的顺序断言。

验证命令：

```powershell
cd D:\workprj\aicoder
go test ./gui -run TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv -count=1
```

验证结果：

```text
gui: ok
```

当前链路进一步变为：

```text
signed Hub approval app package
  -> GUI install + dependency Skill install
  -> DataSrv app_installations registration
  -> approval workflow start / progress / final review
  -> DataSrv review payload carries workflow identity + result package + artifacts
  -> single App and global approval center read back the same instance
```

剩余缺口：这一步补强了 GUI 运行同步到 DataSrv 的请求体审计，但仍不是“真实 Hub handler 产出的 signed package 直接被 GUI 安装入口消费”的外部 black-box 集成。下一步继续推进跨服务样例：真实 Hub submit/approve/publish/download 产物、GUI install、依赖下载验签、DataSrv 查询、审批 workflow 运行与结果回读尽量合并成单条自动化验收；同时继续补 App Studio 可视化制作/测试/提交的真实点击流。

### 推进记录：共享 published Hub 下载包 fixture（2026-07-01）

本轮继续减少 Hub handler 测试与 GUI 下载/安装测试之间的手写包合同漂移。此前 `internal/testfixtures` 已经提供 publish-ready 的企业审批型 App submit package，但 GUI 的 shared fixture 测试仍在本地临时补 `capability`、`review_evidence` 和 entry `submission`。这会让“Hub 发布后的下载包形状”仍有一部分只存在于 GUI 测试拼装逻辑里。

本轮完成：
1. 在 `internal/testfixtures` 新增 `ReadyEnterpriseApprovalMaclawAppPublishedHubPackage`：
   - 基于同一份企业审批型 App fixture；
   - 直接补齐 Hub download package 所需的 `source=enterprise_hub`、`capability`、`review_evidence`、`maclaw_app_review_evidence`；
   - 在 App entry 的 `governance.submission` 中保存 `published` 状态、`capability_id`、`version_key` 和 review evidence。
2. 新增共享 fixture 单元测试：
   - 锁住 published capability 状态；
   - 锁住 review evidence 摘要；
   - 锁住 entry submission 元数据。
3. GUI `TestDownloadMaclawAppPackageFromHubAcceptsSharedApprovalFixture` 改为直接使用共享 published package fixture，然后只追加测试用 package signature。

验证命令：

```powershell
cd D:\workprj\aicoder
go test ./internal/testfixtures -count=1
go test ./gui -run "TestDownloadMaclawAppPackageFromHubAcceptsSharedApprovalFixture|TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv" -count=1
go test ./hub/internal/httpapi -run "TestCapabilityMaclawAppSubmitApprovePublishListAndDownloadGoldenPath|TestCapabilityMaclawAppPackageDownloadReturnsApprovedPack|TestCapabilityMaclawAppPackageDownloadRequiresPublishedStatus" -count=1
```

验证结果：

```text
internal/testfixtures: ok
gui: ok
hub/internal/httpapi: ok
```

当前链路进一步变为：

```text
shared enterprise approval app submit fixture
  -> shared published Hub package fixture
  -> Hub handler golden path keeps same package contract
  -> GUI DownloadMaclawAppPackageFromHub consumes the same published contract
  -> GUI signed install / dependency install / DataSrv registration / approval runtime tests stay green
```

剩余缺口：共享 published package fixture 已经进一步压低 Hub/GUI 合同漂移，但仍不是外部进程级 black-box。下一步可以继续把真实 Hub handler 通过 HTTP 产出的 signed package 接到 GUI 安装入口，或先抽出 GUI 安装核心到可导入包，降低 `package main` 与 `hub/internal` 的测试边界成本。

### 推进记录：共享 signed published Hub package fixture（2026-07-01）

本轮继续把 published Hub 下载包 fixture 向“可直接模拟真实 Hub 签名下载包”推进。上一轮共享 fixture 已能提供 published `capability`、`review_evidence` 和 entry `submission`，但 GUI 测试仍在本地手写 `package_signature` 并同步写入 submission。这样签名 payload、public key fingerprint、顶层签名与 entry submission 镜像关系仍存在测试侧重复实现。

本轮完成：
1. 在 `internal/testfixtures` 新增 `SignPublishedMaclawAppHubPackage`：
   - 生成 `maclaw.app.package_signature.v1`；
   - 使用 GUI 相同的 `sha256:` public key fingerprint 规则；
   - 生成 Hub package signature payload；
   - 写入顶层 `package_sha256` / `package_signature`；
   - 同步镜像到首个 App entry 的 `governance.submission.package_signature`。
2. 新增 `MaclawAppHubPackagePublicKeyFingerprint`，作为共享测试包里的 GUI 兼容指纹生成器。
3. 扩展 `internal/testfixtures` 单元测试：
   - 验证签名算法、package hash 和 fingerprint；
   - 验证顶层签名与 entry submission 签名一致。
4. GUI `TestDownloadMaclawAppPackageFromHubAcceptsSharedApprovalFixture` 改为使用共享签名 helper，只保留下载验签和 governance 校验职责。

验证命令：

```powershell
cd D:\workprj\aicoder
go test ./internal/testfixtures -count=1
go test ./gui -run "TestDownloadMaclawAppPackageFromHubAcceptsSharedApprovalFixture|TestDownloadMaclawAppPackageFromHubTrustsSignedPackageFingerprint" -count=1
go test ./gui -run "TestDownloadMaclawAppPackageFromHubAcceptsSharedApprovalFixture|TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv" -count=1
```

验证结果：

```text
internal/testfixtures: ok
gui: ok
```

当前链路进一步变为：

```text
shared enterprise approval app fixture
  -> shared published Hub package fixture
  -> shared signed package fixture
  -> GUI DownloadMaclawAppPackageFromHub signature/governance validation
  -> GUI signed install / dependency / DataSrv / approval runtime tests stay green
```

剩余缺口：签名 published package 的测试数据已经共享化，但真实 Hub handler 产出的 signed package 仍未在同一条 GUI install 测试里直接消费。下一步继续推进外部 black-box server 级样例，或抽出 GUI 安装核心逻辑，减少 `package main` / `hub/internal` 边界带来的测试组织成本。

### 推进记录：DataSrv app_installations 正式声明 Hub package signature 证据（2026-07-01）

本轮从 DataSrv 正式合同角度继续补齐 Hub 签名包链路。此前 GUI 安装和共享 fixture 已经能保存 signed published Hub package，entry `governance.submission.package_signature` 也会进入安装记录；但 DataSrv `app_installations` 只正式声明了 `hub_package_sha256`、Hub capability/version/review 字段，没有把 package signature 作为可审计元数据展开到顶层摘要和 OpenAPI schema。

本轮完成：
1. DataSrv `normalizeAppInstallationHubIdentityMetadata` 增加 package signature 归一化：
   - 支持从 `submission.package_signature` / `submission.packageSignature` / 顶层 `package_signature` 读取；
   - 输出 canonical `submission.package_signature`；
   - 输出顶层 `hub_package_signature`；
   - 输出 `hub_package_signature_algorithm`、`hub_package_signature_fingerprint`、`hub_package_signature_signed_at`、`hub_package_signature_signed_by`。
2. DataSrv OpenAPI metadata schema 增加上述签名字段说明。
3. 扩展 `TestUpsertAppInstallationNormalizesHubSubmissionAndReviewEvidence`：
   - 输入 camelCase `packageSignature`；
   - 断言 canonical submission 和顶层摘要均保留签名证据。
4. 扩展 `TestAppInstallationOpenAPISchemaDocumentsFullTestEvidence`，把 Hub package signature 字段纳入正式 schema 覆盖。

验证命令：

```powershell
cd D:\workprj\aicoder\datasrv
go test ./structureddata -run "TestUpsertAppInstallationNormalizesHubSubmissionAndReviewEvidence|TestAppInstallationOpenAPISchemaDocumentsFullTestEvidence" -count=1
go test ./structureddata -run "TestHTTPServerCoreEndpoints|TestUpsertAppInstallationNormalizesHubSubmissionAndReviewEvidence" -count=1
```

验证结果：

```text
datasrv/structureddata: ok
```

当前链路进一步变为：

```text
signed Hub package
  -> GUI install / RecordMaclawAppInstall
  -> submission.package_signature
  -> DataSrv app_installations canonical metadata
  -> hub_package_signature + fingerprint/signed_at/signed_by summaries
  -> OpenAPI contract declares the audit fields
```

剩余缺口：DataSrv 正式合同已承认签名包审计字段；下一步继续把真实 Hub handler 产出的 signed package 直接接到 GUI install，或者抽出 GUI 安装核心，减少目前依赖 shared fixture 间接证明的部分。

### 推进记录：GUI 安装登记向 DataSrv 发送 Hub package signature 摘要（2026-07-01）

上一轮 DataSrv 已经正式声明并归一化 `hub_package_signature` 等签名包审计字段。本轮继续把这份合同接回 GUI 安装登记链路：不能只让 DataSrv “能接收”，GUI 在 `RecordMaclawAppInstall` 注册 `app_installations` 时也必须把 signed Hub package 的签名证据送过去。

本轮完成：
1. GUI `maclawAppDataSrvInstallationPayloads` 在处理 `submission.package_signature` 时同步生成：
   - `hub_package_signature`；
   - `hub_package_signature_algorithm`；
   - `hub_package_signature_fingerprint`；
   - `hub_package_signature_signed_at`；
   - `hub_package_signature_signed_by`。
2. 扩展 `TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv`：
   - 断言 DataSrv app installation payload 中保留 `submission.package_signature`；
   - 断言 DataSrv app installation payload 中保留顶层 Hub package signature 摘要；
   - 断言按 Hub/layout identity 查询回来的 installation metadata 仍保留签名摘要。

验证命令：

```powershell
cd D:\workprj\aicoder
go test ./gui -run TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv -count=1
cd D:\workprj\aicoder\datasrv
go test ./structureddata -run "TestUpsertAppInstallationNormalizesHubSubmissionAndReviewEvidence|TestAppInstallationOpenAPISchemaDocumentsFullTestEvidence" -count=1
```

验证结果：

```text
gui: ok
datasrv/structureddata: ok
```

当前链路进一步变为：

```text
signed Hub package
  -> GUI InstallSelectedMaclawAppPackageFromHub
  -> RecordMaclawAppInstall
  -> DataSrv app_installations payload
  -> submission.package_signature + hub_package_signature summaries
  -> DataSrv canonical metadata / OpenAPI contract
```

剩余缺口：GUI 到 DataSrv 的签名审计字段已经闭合；下一步继续推进真实 Hub handler HTTP 产物直接进入 GUI install，或者抽出 GUI 安装核心逻辑，降低目前跨 `package main` / `hub/internal` 的测试组织成本。

### 推进记录：GUI 运行页展示 Hub package signature 安装治理证据（2026-07-01）

本轮继续把 Hub 签名包审计链路推进到用户可见层。此前 GUI 已经会把 signed Hub package 的 `package_signature` 摘要登记到 DataSrv，DataSrv 也会归一化为 `hub_package_signature`、`hub_package_signature_algorithm`、`hub_package_signature_fingerprint`、`hub_package_signature_signed_at`、`hub_package_signature_signed_by`；但 App 面板运行页的 `Install governance` 仍没有把这份证据展示出来，管理员只能在底层 metadata 中追查。

本轮完成：

1. 前端 `BackendAppInstallRecord` 增加 Hub package signature 摘要字段：
   - `hub_package_signature`
   - `hub_package_signature_algorithm`
   - `hub_package_signature_fingerprint`
   - `hub_package_signature_signed_at`
   - `hub_package_signature_signed_by`
2. DataSrv installed app 恢复为本地 AppEntry 时，`dataSrvInstalledInstallEvidence` 会从 DataSrv metadata、install evidence、submission 或 package signature 中提取签名证据，并写回 `installEvidence` 顶层与 `submission.package_signature`。
3. `installRecordEvidenceItems` 在运行页 `Install governance` 中新增 `Package signature` 证据项，优先展示签名算法、public key fingerprint、签名人和签名时间。
4. 扩展企业普通应用 DataSrv 回流前端验收，证明：
   - DataSrv metadata 中的 Hub package signature 能保存到本地 `installEvidence`；
   - 运行页 `Install governance` 可见 `Package signature`；
   - UI 中能看到 `ed25519`、`sha256:customer-console-key` 和 `enterprise-market`。

验证命令：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "restores DataSrv installed enterprise normal app run evidence into app candidates"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "restores DataSrv installed enterprise normal app run evidence into app candidates|keeps dependency verification visible after single market app install|installs an approved Hub approval app with runtime install evidence"
```

验证结果：

```text
gui/frontend AppsPage targeted tests: ok
```

当前链路进一步变为：

```text
signed Hub package
  -> GUI install / RecordMaclawAppInstall
  -> DataSrv app_installations metadata
  -> GUI DataSrv installed app restore
  -> local AppEntry.installEvidence
  -> runtime Install governance shows Package signature
```

剩余缺口：Hub package signature 已经从安装登记、DataSrv 合同、DataSrv 回流到 GUI 运行页可见性闭合；下一步继续推进真实 Hub handler HTTP 产物直接进入 GUI install 的 black-box 链路，或抽出 GUI 安装核心逻辑，减少 shared fixture 间接证明。

### 推进记录：Hub handler 下载包锁定 GUI 安装合同字段（2026-07-01）

本轮继续推进真实 Hub handler 到 GUI install 之间的合同收口。当前工程边界仍然存在：Hub handler 位于 `hub/internal/httpapi`，GUI 安装入口位于 `gui package main`，两者还不能直接在同一个 Go 测试中互相 import 调用。因此本轮先在真实 Hub submit/approve/publish/download golden path 上增加一组面向 GUI 安装入口的合同断言，确保 Hub handler 真实 HTTP 输出不会偏离 GUI `DownloadMaclawAppPackageFromHub` 已强制要求的字段。

本轮完成：

1. 在 Hub handler golden path 测试中新增 `assertDownloadedMaclawAppPackageSatisfiesGUIInstallContract`。
2. 该断言直接检查真实 `CapabilityMaclawAppPackageHandler` 下载响应必须包含：
   - `schema=maclaw.app.pack.v1`
   - `source=enterprise_hub`
   - 顶层 `package_sha256`
   - 顶层 `package_signature`，且算法为 `ed25519`，包含 payload、signature、public key 和 fingerprint
   - 顶层 `capability.id/status/current_version_key`
   - 顶层 `review_evidence` 与 `maclaw_app_review_evidence`
   - 至少一个 `maclaw.app.v1` App entry
   - 每个 entry 的 `governance.submission.status=published`
   - 每个 entry 的 submission capability/version identity、package sha、package signature 和 review evidence
   - `resolved_dependencies` 摘要
3. 断言 entry `submission.package_signature.public_key_fingerprint` 必须镜像顶层 `package_signature.public_key_fingerprint`，防止 GUI 安装登记和 DataSrv 审计字段来源漂移。
4. 复跑 Hub、GUI 和共享 fixture targeted tests，确认真实 handler 输出、GUI 下载/安装链路和共享签名 fixture 仍保持一致。

验证命令：

```powershell
cd D:\workprj\aicoder
go test ./hub/internal/httpapi -run TestCapabilityMaclawAppSubmitApprovePublishListAndDownloadGoldenPath -count=1
go test ./gui -run "TestDownloadMaclawAppPackageFromHubAcceptsSharedApprovalFixture|TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv" -count=1
go test ./internal/testfixtures -count=1
```

验证结果：

```text
hub/internal/httpapi: ok
gui: ok
internal/testfixtures: ok
```

当前链路进一步变为：

```text
real Hub submit
  -> real Hub approve
  -> real Hub publish
  -> real CapabilityMaclawAppPackageHandler download
  -> GUI install contract field matrix verified on handler output
  -> shared signed fixture and GUI install tests remain green
```

剩余缺口：这一步把真实 Hub handler 输出锁到了 GUI 安装字段矩阵，但仍不是“同一条测试里真实 handler HTTP 响应直接被 GUI `DownloadMaclawAppPackageFromHub` 消费”。下一步继续推进两条路线之一：抽出 GUI 下载/安装核心到可导入包，或搭建外部 black-box server 级样例，让 GUI 测试通过 HTTP 直接消费真实 Hub server 输出。

### 推进记录：抽取 Hub 下载包 GUI 安装共享合同校验（2026-07-01）

上一轮 Hub handler golden path 已经增加了“GUI 安装字段矩阵”断言，但断言仍只存在于 Hub 测试内部，GUI 下载入口本身还在用自己的校验逻辑。这样虽然能减少漂移，但不能证明两边正在使用同一份合同。本轮继续收口：把这组跨服务合同抽到仓库根 `internal/maclawappcontract`，让真实 GUI 下载入口和 Hub handler 测试共同调用。

本轮完成：

1. 新增 `internal/maclawappcontract.ValidateGUIInstallHubPackage`：
   - 校验 `maclaw.app.pack.v1` / `enterprise_hub` 下载包身份；
   - 校验顶层 `package_sha256`；
   - 校验顶层 `package_signature` 为 `ed25519`，并包含 payload、signature、公钥和 fingerprint；
   - 校验 signature 中的 `package_sha256` 与包顶层 sha 一致；
   - 校验 published capability identity；
   - 校验 package review evidence；
   - 校验每个 App entry 的 `governance.submission`、published 状态、capability/version identity、submission package sha、submission package signature、review evidence；
   - 校验 entry signature fingerprint 与顶层 package signature fingerprint 不漂移；
   - 校验 `resolved_dependencies` 摘要存在。
2. GUI `validateDownloadedMaclawAppHubPackageGovernance` 前置调用共享合同校验；原有 parsed-entry 校验继续保留，作为 GUI 内部解析后的二次保护。
3. Hub handler golden path 的 `assertDownloadedMaclawAppPackageSatisfiesGUIInstallContract` 改为调用共享合同函数，不再维护一份测试内私有规则。
4. 补充共享包单元测试：
   - 接受 published Hub package；
   - 拒绝缺少 package signature；
   - 拒绝 entry submission signature fingerprint 与顶层 signature fingerprint 漂移。
5. 修正共享 published fixture 与 GUI 手写 fixture：
   - `SignPublishedMaclawAppHubPackage` 同步把 `package_sha256` 镜像到 entry submission；
   - published fixture 增加 `resolved_dependencies`；
   - GUI 手写 Hub 下载包 fixture 在 mark published 时同步写 submission sha/signature 和 resolved dependency 摘要。

验证命令：

```powershell
cd D:\workprj\aicoder
go test ./internal/maclawappcontract -count=1
go test ./internal/testfixtures -count=1
go test ./hub/internal/httpapi -run TestCapabilityMaclawAppSubmitApprovePublishListAndDownloadGoldenPath -count=1
go test ./gui -run "TestDownloadMaclawAppPackageFromHubAcceptsSharedApprovalFixture|TestDownloadMaclawAppPackageFromHubTrustsSignedPackageFingerprint|TestDownloadMaclawAppPackageFromHubRejectsSignedPackageWithoutPublishedGovernance|TestDownloadMaclawAppPackageFromHubRejectsSignedPackageWithoutTopLevelReviewEvidence|TestInstallSelectedMaclawAppPackageFromHubUsesPackageSignatureTrustForDependencySkill|TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv" -count=1
```

验证结果：

```text
internal/maclawappcontract: ok
internal/testfixtures: ok
hub/internal/httpapi: ok
gui: ok
```

当前链路进一步变为：

```text
shared GUI install contract
  -> real Hub handler golden path validates downloaded package with same function
  -> GUI DownloadMaclawAppPackageFromHub validates downloaded package with same function
  -> shared signed fixture mirrors package sha/signature/resolved dependencies
  -> GUI install + dependency trust + DataSrv runtime tests remain green
```

剩余缺口：共享合同已经让 Hub handler 测试和 GUI 下载入口使用同一份字段规则，但还不是“真实 Hub server 响应在同一条测试里被 GUI 直接 HTTP 消费”。下一步可以继续推进外部 black-box server 样例，或把 GUI HTTP 下载核心抽到非 `package main` 可导入包，进一步消除测试组织边界。

### 推进记录：共享 GUI HTTP 下载 client 消费真实 Hub handler 输出（2026-07-01）

上一轮已经把 Hub 下载包字段规则抽成共享合同函数，GUI 下载入口和 Hub handler 测试都调用同一份 `ValidateGUIInstallHubPackage`。但真实 HTTP 消费路径仍在 GUI `package main` 内部，Hub golden path 只能直接调用 handler 并手动 decode 响应。本轮继续把 GUI 的 HTTP 下载核心抽成共享 client，并在 Hub golden path 中用这个共享 client 通过 httptest server 消费真实 `CapabilityMaclawAppPackageHandler` 输出。

本轮完成：

1. 新增 `internal/maclawappcontract.DownloadGUIInstallHubPackage`：
   - 拼接 `/api/capabilities/maclaw-apps/{capabilityID}/package`；
   - 发送 `Authorization: Bearer <token>`；
   - 处理非 2xx HTTP 错误并保留响应摘要；
   - 解码 JSON package；
   - 调用 `ValidateGUIInstallHubPackage` 校验共享安装合同。
2. GUI `DownloadMaclawAppPackageFromHub` 改为使用共享 HTTP client 获取 Hub package；后续仍保留 GUI 自己的签名验签、可信 fingerprint 合并、entry 解析、安装记录 payload 生成。
3. Hub handler golden path 增加 black-box 段：
   - 使用 `httptest.NewServer` 暴露真实 `CapabilityMaclawAppPackageHandler`；
   - 使用 `DownloadGUIInstallHubPackage` 通过 HTTP 下载真实 handler 输出；
   - 断言返回包保留 capability identity 和 package signature fingerprint。
4. 共享包新增 HTTP client 单测：
   - 验证请求 path 和 Authorization；
   - 验证下载后合同校验；
   - 验证 HTTP 错误会带状态码和响应摘要。

验证命令：

```powershell
cd D:\workprj\aicoder
go test ./internal/maclawappcontract -count=1
go test ./internal/testfixtures -count=1
go test ./hub/internal/httpapi -run TestCapabilityMaclawAppSubmitApprovePublishListAndDownloadGoldenPath -count=1
go test ./gui -run "TestDownloadMaclawAppPackageFromHubAcceptsSharedApprovalFixture|TestDownloadMaclawAppPackageFromHubTrustsSignedPackageFingerprint|TestDownloadMaclawAppPackageFromHubRejectsSignedPackageWithoutPublishedGovernance|TestDownloadMaclawAppPackageFromHubRejectsSignedPackageWithoutTopLevelReviewEvidence|TestInstallSelectedMaclawAppPackageFromHubUsesPackageSignatureTrustForDependencySkill|TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv" -count=1
```

验证结果：

```text
internal/maclawappcontract: ok
internal/testfixtures: ok
hub/internal/httpapi: ok
gui: ok
```

当前链路进一步变为：

```text
real Hub submit/approve/publish
  -> httptest server exposes real CapabilityMaclawAppPackageHandler
  -> shared GUI HTTP download client requests real handler output
  -> shared GUI install contract validates package
  -> GUI DownloadMaclawAppPackageFromHub uses same shared HTTP client
  -> GUI signature trust / dependency install / DataSrv runtime tests remain green
```

剩余缺口：这已经是“真实 Hub handler HTTP 输出被 GUI 共享下载 client 消费”的 black-box 样例；但还不是完整 GUI `App.DownloadMaclawAppPackageFromHub` 直接连真实 Hub server，因为 GUI 仍在 `package main`，Hub 测试无法直接 import。下一步若继续压缩边界，可以把签名验签和 download result 组装也逐步抽到 `internal/maclawappcontract`，或增加一个外部进程级测试，让 GUI 包通过配置 URL 直连真实 Hub test server。

### 推进记录：Hub package signature 验签逻辑共享化（2026-07-01）

上一轮已经把 GUI HTTP 下载核心抽到 `internal/maclawappcontract.DownloadGUIInstallHubPackage`，Hub handler golden path 也能通过共享 HTTP client 消费真实 handler 输出。本轮继续把 GUI 内部的 Hub package signature 验签逻辑抽到同一共享合同包，减少 GUI `package main` 与 Hub/fixture 之间的签名规则漂移。

本轮完成：

1. 新增 `internal/maclawappcontract.VerifyHubPackageSignature`：
   - 支持 `ed25519` 签名；
   - 支持 `public_key_base64` / `public_key`；
   - 支持 `signature_base64` / `signature`；
   - 支持 `base64:`、`ed25519:` 前缀剥离；
   - 计算 `sha256:<public-key>` fingerprint；
   - 校验声明 fingerprint；
   - 校验 signature `package_sha256` 与 package 顶层 `package_sha256` 一致；
   - 执行 ed25519 payload 验签。
2. 新增 `HubPackagePublicKeyFingerprint`，并让 `internal/testfixtures.MaclawAppHubPackagePublicKeyFingerprint` 转调共享函数，避免 fixture 继续维护第三份 fingerprint 规则。
3. GUI `verifyMaclawAppHubPackageSignature` 改为薄包装，转调 `maclawappcontract.VerifyHubPackageSignature`。
4. 保留 GUI 安全顺序：
   - 共享 HTTP client 只下载并解码 package；
   - GUI 先做 cryptographic package signature 验证；
   - 再执行 parsed-entry 解析和共享治理合同校验。
   这样签名被篡改时会优先报签名错误，不会被后续治理字段缺失掩盖。
5. 共享包补充签名单元测试：
   - 接受真实 ed25519 签名；
   - 拒绝 package sha mismatch；
   - 拒绝 tampered payload。

验证命令：

```powershell
cd D:\workprj\aicoder
go test ./internal/maclawappcontract -count=1
go test ./internal/testfixtures -count=1
go test ./gui -run "TestDownloadMaclawAppPackageFromHubAcceptsSharedApprovalFixture|TestDownloadMaclawAppPackageFromHubTrustsSignedPackageFingerprint|TestDownloadMaclawAppPackageFromHubRejectsInvalidPackageSignature|TestInstallSelectedMaclawAppPackageFromHubUsesPackageSignatureTrustForDependencySkill|TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv" -count=1
go test ./hub/internal/httpapi -run TestCapabilityMaclawAppSubmitApprovePublishListAndDownloadGoldenPath -count=1
```

验证结果：

```text
internal/maclawappcontract: ok
internal/testfixtures: ok
gui: ok
hub/internal/httpapi: ok
```

当前链路进一步变为：

```text
shared GUI HTTP download client
  -> shared Hub package signature verification
  -> GUI trust fingerprint merge
  -> shared GUI install governance contract
  -> Hub handler black-box test and GUI install tests use the same signature/contract rules
```

剩余缺口：HTTP 下载、签名验签、治理字段合同都已共享化；GUI 仍保留 download result 组装、entry parse 和 trusted fingerprint 写配置逻辑。下一步可以继续把 download result 组装抽为共享 helper，或转向 App Studio 可视化制作/测试/上传链路这个更大的产品级缺口。

### 推进记录：App Studio 动态布局指纹合同硬化（2026-07-01）

本轮转向 App Studio 可视化制作/测试/上传链路中最容易发生证据漂移的一段：用户在可视化布局设计器里移动区域、隐藏区域或调整输出位置后，保存到超级 Skill manifest、发布包 `app.ui` / `binding.ui`、治理证据 `governance.workspaceLayout` 必须使用同一份 canonical 布局和同一枚 fingerprint。此前已有测试证明布局能被保存和进入发布包，但对 fingerprint 与 regions 的一致性断言还不够硬，仍可能出现“治理证据按排序 regions 计算，manifest 保存另一份 regions 顺序”的隐性漂移。

本轮完成：

1. `applyStudioWorkspaceLayout` 保存布局时先 canonical 排序 regions，再写入 `layouts[entry].regions`，并同步写入 `layouts[entry].fingerprint`。
2. `appWorkspaceLayoutEvidence` 改为返回同一份 canonical regions；`regionCount`、`regionIds` 和 `fingerprint` 都基于这份 regions 生成。
3. 新增 `appWorkspaceUIForManifest`，发布 manifest 生成时把 `app.ui` 和 `app.binding.ui` 都规范化为带 fingerprint 的同一份动态 UI layout。
4. 扩展 App Studio targeted tests，明确断言：
   - 保存到超级 Skill 的 `binding.ui.layouts[entry].fingerprint` 等于 `governance.workspaceLayout.fingerprint`；
   - 发布包里的 `app.ui.layouts[entry]`、`binding.ui.layouts[entry]` 与 `governance.workspaceLayout.regions` 完全一致；
   - `regionIds` 等于 canonical regions 顺序。

验证命令：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "saves a newly created enterprise approval app into its app skill definition|creates, tests, and submits an enterprise approval app from App Studio|moves workspace regions from the visual layout preview into saved manifest regions"
```

验证结果：

```text
AppsPage.test.tsx: 3 passed
```

当前 App Studio 布局链路进一步变为：

```text
visual layout designer
  -> canonical regions
  -> layout fingerprint
  -> maclaw.app.json ui.layouts[entry]
  -> publish package app.ui / binding.ui
  -> governance.workspaceLayout
  -> targeted tests assert same regions + same fingerprint
```

剩余缺口：这一步压实了动态布局保存和发布证据的一致性，但 App Studio 仍缺一条更完整的真实点击流：生成企业审批型 App、绑定 workflow Skill、完成本地测试、提交 Hub、Hub 审核发布、市场安装、依赖安装、DataSrv 登记、审批运行和结果回读。下一步继续把这条链路拆成可维护的黑盒/半黑盒验收，优先补“制作/测试/提交 Hub”到“市场安装运行”的跨边界样例。

### 推进记录：后端提交审核阻断布局指纹漂移（2026-07-01）

上一轮已经在前端 App Studio 生成和发布包预览中保证 `app.ui`、`binding.ui`、`governance.workspaceLayout` 使用同一份 canonical regions 和同一枚 fingerprint。本轮继续把这条约束下沉到 GUI 后端 `SubmitMaclawAppPackage` 审核门禁，避免用户或旧工具绕过前端后提交一个“看起来有布局证据、但 fingerprint 与实际 regions 不一致”的包。

本轮完成：

1. `maclawAppGovernanceReviewIssuesFromPackage` 新增 workspace layout fingerprint 审核：
   - `governance.workspaceLayout.fingerprint` 如已声明，必须等于后端按 entry/template/density/primaryRegion/outputRegion/regions 重新计算出的 fingerprint；
   - `app.ui.layouts[entry].fingerprint` 如已声明，必须匹配其实际 regions；
   - `binding.ui.layouts[entry].fingerprint` 如已声明，必须匹配其实际 regions；
   - governance fingerprint 与 manifest UI fingerprint 同时存在时必须一致。
2. 新增后端 canonical workspace regions 计算逻辑：
   - 按 `order` 排序；
   - 统一 `visible` 默认值；
   - 统一写入 `id`、`role`、`placement`、`visible`、`order` 后再计算 hash。
3. 新增 `TestSubmitMaclawAppPackageRejectsWorkspaceLayoutFingerprintMismatch`：
   - 构造一个 definitionHash 已刷新、测试证据完整、依赖可用的工具型 App 包；
   - 故意篡改 `governance.workspaceLayout.fingerprint`；
   - 验证后端提交审核阻断，错误路径落在 `apps[0].app.governance.workspaceLayout.fingerprint`。

验证命令：

```powershell
cd D:\workprj\aicoder
go test ./gui -run "TestSubmitMaclawAppPackageRejectsWorkspaceLayoutFingerprintMismatch|TestSubmitMaclawAppPackageFlagsStaleRunEvidenceWhenLegacyBindingUILayoutChanges|TestSubmitMaclawAppPackageFlagsStaleRunEvidenceWhenTopLevelUILayoutChanges" -count=1
go test ./gui -run "TestSubmitMaclawAppPackage" -count=1
```

验证结果：

```text
gui: ok
gui SubmitMaclawAppPackage targeted suite: ok
```

当前发布门禁进一步变为：

```text
App Studio canonical layout
  -> manifest ui fingerprint
  -> governance workspaceLayout fingerprint
  -> GUI backend recomputes fingerprint at SubmitMaclawAppPackage
  -> mismatched/stale layout evidence is blocked before Hub queue
```

剩余缺口：布局证据现在前后端都能防漂移；下一步继续把 App Studio 制作、测试、提交 Hub、真实 Hub 审核发布、市场安装、依赖安装、DataSrv 登记、审批运行与结果回读串成更完整的半黑盒/黑盒验收。

### 推进记录：Hub 审核侧同步阻断布局指纹漂移（2026-07-01）

上一轮 GUI 后端 `SubmitMaclawAppPackage` 已能阻断动态布局 fingerprint 与实际 regions 不一致的包。本轮继续把同一条规则推进到企业 Hub：即使包不是通过 GUI 正常提交，而是直接调用 Hub submit/review 接口，也不能绕过 App Studio 动态布局证据一致性检查。

本轮完成：

1. Hub `enterpriseMaclawAppReadyReviewIssues` 增加 workspace layout fingerprint 审核：
   - `governance.workspaceLayout.fingerprint` 如存在，必须等于 Hub 侧按 canonical regions 重新计算的 fingerprint；
   - `app.ui.layouts[entry].fingerprint` 如存在，必须匹配实际 regions；
   - `binding.ui.layouts[entry].fingerprint` 如存在，必须匹配实际 regions；
   - governance fingerprint 与 manifest UI fingerprint 同时存在时必须一致。
2. Hub `enterpriseMaclawAppWorkspaceLayoutForEntry` 保留 layout `fingerprint`，避免审核、metadata 和下载包之间丢失布局指纹。
3. 新增 Hub 侧 canonical workspace regions/hash 计算：
   - 按 `order` 排序；
   - 补齐 `visible=true` 默认值；
   - 使用 entry/template/density/primaryRegion/outputRegion/regions 计算稳定 hash。
4. 扩展 `TestCapabilityMaclawAppSubmitRejectsUnreadyPackage`：
   - 构造新版 App Studio 布局证据形态；
   - 故意写入错误 `workspaceLayout.fingerprint`；
   - 验证 Hub submit 直接返回 `MACLAW_APP_PACKAGE_NOT_READY`，且 message 包含 workspace layout fingerprint mismatch。

验证命令：

```powershell
cd D:\workprj\aicoder
go test ./hub/internal/httpapi -run "TestCapabilityMaclawAppSubmitRejectsUnreadyPackage|TestAdminCapabilityMaclawAppReviewBlocksUnreadyApproval|TestCapabilityMaclawAppSubmitApprovePublishListAndDownloadGoldenPath" -count=1
```

验证结果：

```text
hub/internal/httpapi: ok
```

当前布局发布门禁进一步变为：

```text
App Studio canonical layout
  -> GUI publish package app.ui / binding.ui / governance.workspaceLayout
  -> GUI SubmitMaclawAppPackage recomputes layout fingerprint
  -> Hub submit/review recomputes layout fingerprint
  -> Hub approve/publish/download only sees layout-consistent packages
```

剩余缺口：GUI 和 Hub 都已经能阻断布局证据漂移；下一步应继续把“提交后真实 Hub 审核发布 -> 市场安装 -> DataSrv 登记 -> 审批运行结果回读”合并成更强的跨边界验收，并逐步减少 GUI package main 与 Hub internal 包之间的测试组织边界。

### 推进记录：Hub 市场安装后即时回灌 package signature 证据（2026-07-01）

上一轮已经让 Hub submit/review 阻断动态布局证据漂移。本轮继续压紧“Hub 发布后被市场安装”的运行态证据回灌：此前 DataSrv 恢复出的已安装应用能够显示 Hub package signature，但前端市场安装当场的 `installEvidenceRecordForApp` 归一化结果没有把 `hub_package_signature`、fingerprint、signed_by、signed_at 等字段带回本地 AppEntry。这样会出现“DataSrv 二次恢复后能审计签名，但刚安装完打开运行页时证据不完整”的断层。

本轮完成：

1. `installEvidenceRecordForApp` 保留 Hub package signature 证据：
   - `hub_package_signature`
   - `hub_package_signature_algorithm`
   - `hub_package_signature_fingerprint`
   - `hub_package_signature_signed_at`
   - `hub_package_signature_signed_by`
2. 扩展 `installs an approved Hub approval app with runtime install evidence` 前端验收：
   - mock Hub install record 顶层和 app 级 install_evidence 都携带 package signature；
   - 断言安装后的本地 AppEntry `installEvidence` 保留 signature fingerprint 和 signed_by；
   - 断言运行页 `Install governance` 立即显示 `Package signature`，不必等 DataSrv 重新发现后才可见。

验证命令：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "installs an approved Hub approval app with runtime install evidence"
```

验证结果：

```text
AppsPage.test.tsx: 1 passed
```

当前市场安装证据链进一步变为：

```text
Hub approved signed package
  -> GUI market install
  -> install_record / install_evidence
  -> local AppEntry.installEvidence
  -> runtime Install governance shows package signature immediately
  -> DataSrv restore remains consistent with the same signature fields
```

剩余缺口：安装后的 package signature 运行态可见性已补齐；下一步继续把真实 Hub 发布产物、市场安装、DataSrv 登记、审批 workflow 运行、审批实例列表回读压成更完整的跨边界验收。

### 推进记录：真实 Hub 下载产物进入 GUI 安装前门禁时必须可验签（2026-07-01）

上一轮已经让前端市场安装后的本地运行态即时展示 Hub package signature。本轮继续向真实全链路推进，把“Hub submit/approve/publish 后由下载 handler 生成的包”接到共享 GUI 安装前门禁上，不再只检查字段存在，而是验证该真实 Hub 下载产物的 ed25519 包签名可以被 GUI 信任链接受。

本轮完成：

1. Hub golden path `TestCapabilityMaclawAppSubmitApprovePublishListAndDownloadGoldenPath` 的 `assertDownloadedMaclawAppPackageSatisfiesGUIInstallContract` 增强为双门禁：
   - 调用 `maclawappcontract.ValidateGUIInstallHubPackage` 校验 GUI 安装所需的 Hub 身份、published 状态、review evidence、submission、resolved dependencies 等合同字段；
   - 调用 `maclawappcontract.VerifyHubPackageSignature` 对真实 `CapabilityMaclawAppPackageHandler` 输出的 package signature 做 ed25519 验签；
   - 要求返回非空 public-key fingerprint，证明 GUI 安装可把该 Hub 包签名纳入依赖 Skill 信任链。
2. 共享 contract 测试 `TestDownloadGUIInstallHubPackageFetchesAndValidatesPackage` 从“只下载 JSON”升级为“下载后可安装门禁校验且可验签”：
   - httptest server 返回签名后的 MaClaw App Hub 包；
   - 下载后立即跑 `ValidateGUIInstallHubPackage`；
   - 再跑 `VerifyHubPackageSignature` 并断言 fingerprint 与签名公钥一致。
3. 测试签名夹具 `signedHubPackage` 同步回填 app governance submission 中的 `package_sha256` 与 `package_signature`，保证顶层 package signature 与 app entry submission signature 是同一份可追溯证据。

验证命令：

```powershell
cd D:\workprj\aicoder
go test ./internal/maclawappcontract -count=1
go test ./hub/internal/httpapi -run TestCapabilityMaclawAppSubmitApprovePublishListAndDownloadGoldenPath -count=1
```

验证结果：

```text
internal/maclawappcontract: ok
hub/internal/httpapi golden path: ok
```

当前 Hub -> GUI 安装前门禁进一步变为：

```text
App Studio package
  -> Hub submit
  -> Hub approve
  -> Hub publish/sign
  -> CapabilityMaclawAppPackageHandler real download output
  -> shared GUI HTTP client consumes handler output
  -> shared GUI install contract validates package identity/evidence/dependencies
  -> shared signature verifier proves package signature trust fingerprint
```

剩余缺口：真实 Hub 下载产物已经能通过共享 GUI 安装前合同和签名验真；下一步还需要继续跨过 GUI `package main` 边界，把真实 Hub handler 输出直接喂给 GUI 安装入口，连同依赖 Skill 安装、DataSrv app_installations 登记、审批 workflow 运行、我的申请/待我审批/已处理列表和结果回读做成一条更完整的半黑盒验收。

### 推进记录：GUI signed Hub 多 App 选择安装收窄审计证据（2026-07-01）

上一轮把真实 Hub handler 下载产物接入共享 GUI 安装前合同与签名验真。本轮继续推进 GUI 安装入口本身，补齐“一个 Hub capability 包含多个 MaClaw App，用户只选择其中一个安装”的 signed package 路径：安装子包必须只保留被选 App 与其依赖、review evidence 也必须同步收窄，同时仍保留原始 Hub package signature 作为来源信任证据。

本轮完成：

1. `maclawAppPackageForSelectedAppIDs` 选择安装时同步过滤审计证据：
   - 顶层 `review_evidence` 只保留选中 App；
   - 顶层 `maclaw_app_review_evidence` 只保留选中 App；
   - 选中 app entry 的 `governance.submission.review_evidence` / `maclaw_app_review_evidence` 同步收窄；
   - 兼容老式扁平 review evidence：如果 evidence 不是按 app_id 分组，保留原证据，避免误删历史包。
2. `TestInstallSelectedMaclawAppPackageFromHubFiltersPackageApps` 升级为 signed published Hub 包：
   - 生成 ed25519 package signature；
   - 顶层 capability/status/version、resolved dependencies、review evidence、每个 app 的 governance submission 都按 Hub published 包形态构造；
   - 断言 GUI 下载门禁可接受签名包；
   - 断言选择安装后只剩被选 app/dependency；
   - 断言本地 install audit 不再包含未选 App 的 run evidence；
   - 断言安装子包仍保留原始 Hub package signature fingerprint/package_sha256。
3. 新增测试 helper `normalizeMaclawAppPackageWorkspaceFingerprintsForTest`：
   - 先按 GUI 安装解析逻辑归一化动态 UI；
   - 让 `app.ui.layouts[entry]` 与 `governance.workspaceLayout` 对齐，模拟 App Studio 保存后的真实 manifest；
   - 按生产算法刷新 workspace layout fingerprint；
   - 二次归一化后刷新 testEvidence `definitionHash`，避免 signed install 夹具在布局门禁和运行证据门禁之间漂移。
4. 顺手修复 signed Hub 安装回归夹具：
   - `TestInstallSelectedMaclawAppPackageFromHubUsesPackageSignatureTrustForDependencySkill`
   - `TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv`
   这两条测试不再依赖占位 `layout-*` 字符串，而是使用生产算法计算出的真实 workspace layout fingerprint，并同步刷新 definitionHash。

验证命令：

```powershell
cd D:\workprj\aicoder
go test ./gui -run "TestInstallSelectedMaclawAppPackageFromHubFiltersPackageApps|TestSelectedMaclawAppPackageFiltersResolvedDependencies" -count=1
go test ./gui -run "TestInstallSelectedMaclawAppPackageFromHubUsesPackageSignatureTrustForDependencySkill|TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv|TestInstallSelectedMaclawAppPackageFromHubFiltersPackageApps" -count=1
```

验证结果：

```text
gui selected app install targeted suite: ok
gui signed Hub install targeted suite: ok
```

当前 GUI 市场安装链路进一步变为：

```text
signed Hub package
  -> GUI download verifies package signature/trust fingerprint
  -> user selects one App from multi-App capability package
  -> install package keeps only selected apps/dependencies
  -> review evidence is scoped to selected App
  -> original Hub package signature remains available as source trust evidence
  -> dependency install / DataSrv registration / approval runtime tests continue to pass
```

剩余缺口：GUI 选择安装和 signed approval runtime 的关键路径已经补强；下一步继续把真实 Hub `CapabilityMaclawAppPackageHandler` 的 httptest 输出进一步接入 GUI 安装入口，减少手工拼 Hub server 响应，并把 App Studio 点击流、Hub 审核发布、市场安装、DataSrv 审批实例回读压成更完整的半黑盒验收。

### 推进记录：App Studio workspace layout fingerprint 算法共享化（2026-07-01）

上一轮补强了 GUI signed Hub 多 App 选择安装，但也暴露出一个更基础的风险：App Studio 动态布局 fingerprint 在前端、GUI 后端、Hub 审核侧各自实现，测试夹具还需要手工跟随规则。这样在“设计 -> 保存 -> 测试 -> 提交 -> Hub 审核 -> 市场安装”的长链路里，很容易出现某一端计算规则细微漂移，导致真实包被误拒或错误放行。

本轮完成：

1. 新增共享合同函数 `internal/maclawappcontract.WorkspaceLayoutFingerprint`：
   - 输入 `entryName + layout map`；
   - 统一读取 `template/density/primaryRegion/outputRegion`；
   - 兼容 `primary_region/output_region`；
   - 对 regions 按 `order`、原始位置稳定排序；
   - `visible` 默认 `true`；
   - 使用同一 FNV-1a text hash 输出 8 位 fingerprint。
2. 新增 `internal/maclawappcontract.CanonicalWorkspaceLayoutRegions`：
   - 作为 fingerprint 的 canonical regions 来源；
   - 单测覆盖排序、默认 visible、默认 order 与 snake/camel 字段兼容。
3. GUI 后端 `maclawAppWorkspaceLayoutFingerprint` 改为薄包装：
   - 直接调用 `maclawappcontract.WorkspaceLayoutFingerprint`；
   - GUI submit/install/runtime 门禁继续走原入口，但算法来源变成共享合同。
4. Hub 审核侧 `enterpriseMaclawAppWorkspaceLayoutFingerprint` 改为薄包装：
   - 直接调用同一共享合同函数；
   - Hub submit/review/publish/download 与 GUI 安装门禁不再维护两份 fingerprint 算法。

验证命令：

```powershell
cd D:\workprj\aicoder
go test ./internal/maclawappcontract -count=1
go test ./hub/internal/httpapi -run "TestCapabilityMaclawAppSubmitRejectsUnreadyPackage|TestCapabilityMaclawAppSubmitApprovePublishListAndDownloadGoldenPath" -count=1
go test ./gui -run "TestInstallSelectedMaclawAppPackageFromHubUsesPackageSignatureTrustForDependencySkill|TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv|TestInstallSelectedMaclawAppPackageFromHubFiltersPackageApps" -count=1
```

验证结果：

```text
internal/maclawappcontract: ok
hub/internal/httpapi layout/golden path targeted suite: ok
gui signed Hub install targeted suite: ok
```

当前布局证据链进一步变为：

```text
App Studio saves layout regions
  -> frontend writes fingerprint
  -> GUI backend validates through shared contract fingerprint
  -> Hub submit/review validates through same shared contract fingerprint
  -> GUI Hub install validates through same shared contract fingerprint
```

剩余缺口：布局 fingerprint 规则已经后端共享化；前端 TypeScript 仍有独立实现，后续可考虑用同一份 golden fixture 或导出 wasm/JSON contract test 做前后端一致性校验。主流程上继续推进真实 Hub handler 输出进入 GUI 安装入口、DataSrv 审批实例回读和 App Studio 点击流验收。

### 推进记录：Hub 下载包选择安装子包筛选逻辑共享化（2026-07-01）

上一轮把 workspace layout fingerprint 规则抽成共享合同。本轮继续把 GUI 安装入口里的“多 App 包选择安装”逻辑抽到共享合同层，目标是让真实 Hub `CapabilityMaclawAppPackageHandler` 输出不只通过下载合同和签名验真，还能进一步进入 GUI 安装准备阶段：按用户选择的 App 生成安装子包、收窄依赖和 review evidence，并继续满足 GUI install contract。

本轮完成：

1. 新增 `internal/maclawappcontract.SelectHubPackageApps`：
   - 输入 Hub 下载包和 `selectedAppIDs`；
   - 空选择时返回完整 clone 包；
   - 支持 `market-xxx` 与原始 app id 互相匹配；
   - 过滤 `apps`，只保留被选 App；
   - 过滤顶层 `resolved_dependencies`；
   - 过滤 app entry 内 `resolved_dependencies`；
   - 过滤顶层 `review_evidence` / `maclaw_app_review_evidence`；
   - 过滤 `governance.submission.review_evidence` / `maclaw_app_review_evidence`；
   - 保留原始 Hub `package_signature` / submission signature 作为来源信任证据。
2. 新增共享 selector 单测：
   - `TestSelectHubPackageAppsFiltersAppsDependenciesAndReviewEvidence`
   - `TestSelectHubPackageAppsKeepsFlatReviewEvidence`
   - `TestSelectHubPackageAppsRejectsMissingSelection`
   覆盖选择安装、扁平旧式 review evidence 兼容、缺失选择报错。
3. GUI `maclawAppPackageForSelectedAppIDs` 改为调用共享 selector：
   - GUI 仍用自己的 parser 生成 typed `parsedMaclawAppEntry`；
   - 但安装子包的 map 级筛选规则由 `internal/maclawappcontract` 统一提供。
4. Hub golden path 增加真实 handler 输出到共享 selector 的断言：
   - `CapabilityMaclawAppPackageHandler` 真实输出；
   - `DownloadGUIInstallHubPackage` 共享 HTTP client 下载；
   - `SelectHubPackageApps(..., ["market-approval-ready-app"])` 生成选中 App 安装子包；
   - 子包再次通过 `ValidateGUIInstallHubPackage` 与 `VerifyHubPackageSignature`。

验证命令：

```powershell
cd D:\workprj\aicoder
go test ./internal/maclawappcontract -count=1
go test ./gui -run "TestInstallSelectedMaclawAppPackageFromHubFiltersPackageApps|TestSelectedMaclawAppPackageFiltersResolvedDependencies" -count=1
go test ./gui -run "TestInstallSelectedMaclawAppPackageFromHubUsesPackageSignatureTrustForDependencySkill|TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv|TestInstallSelectedMaclawAppPackageFromHubFiltersPackageApps" -count=1
go test ./hub/internal/httpapi -run TestCapabilityMaclawAppSubmitApprovePublishListAndDownloadGoldenPath -count=1
```

验证结果：

```text
internal/maclawappcontract: ok
gui selected app install targeted suite: ok
gui signed Hub install targeted suite: ok
hub/internal/httpapi golden path: ok
```

当前真实 Hub 输出到 GUI 安装准备链路进一步变为：

```text
Hub submit/approve/publish
  -> CapabilityMaclawAppPackageHandler real output
  -> shared GUI HTTP client downloads package
  -> shared package signature verifier proves trust fingerprint
  -> shared package selector creates selected-App install package
  -> selected package still satisfies GUI install contract
  -> GUI install entry uses the same shared selector before dependency install/DataSrv registration
```

剩余缺口：真实 Hub handler 输出已经进入共享 GUI 安装准备阶段；下一步继续减少 GUI 测试中手工模拟 Hub dependency skill download 的部分，逐步把“真实 Hub handler 输出 -> GUI 安装入口 -> 依赖 Skill 安装 -> DataSrv 登记 -> 审批 workflow 运行/回读”压成更完整的半黑盒验收。

### 推进记录：Enterprise Hub signed Skill 依赖安装测试夹具共享化（2026-07-01）

本轮继续减少 GUI 安装链路里手工拼装 Hub dependency Skill 响应的比例。MaClaw App 安装时会把 Hub package signature 的公钥指纹纳入依赖 Skill 信任链，因此测试夹具也必须稳定模拟“已发布 signed Skill capability + 可验签 Skill 下载包”这组真实输入，而不是每条用例各自临时拼 JSON。

本轮完成：

1. 新增共享测试夹具：
   - `SignedEnterpriseHubSkillPackage(id, version, instructions, publicKey, privateKey)` 生成 signed Skill JSON body、sha256 与 ed25519 signature metadata；
   - `PublishedEnterpriseHubSkillCapability(skillID, capabilityID, version, packageSHA, packageSignature)` 生成 published Hub Skill capability 摘要和 metadata_json；
   - signature metadata 与 GUI 依赖安装验签逻辑使用同一套 public key fingerprint 规则。
2. GUI signed Hub 安装测试改为复用共享夹具：
   - `TestInstallSelectedMaclawAppPackageFromHubUsesPackageSignatureTrustForDependencySkill` 不再手工拼 `signed-skill` capability metadata；
   - `TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv` 不再使用本地 `signedSkill` 闭包，而是为 `expense-super-skill@1.4.0` 与 `expense-workflow@2.1.0` 生成真实版本一致的 signed Skill 包；
   - Hub capability 响应统一使用 `PublishedEnterpriseHubSkillCapability`，避免测试里手写 `metadata_json` 与 package signature 字段漂移。
3. 这一步把审批型 App 的依赖安装验收推进为：

```text
signed Hub MaClaw App package
  -> package signature seeds trusted dependency key fingerprint
  -> GUI resolves enterprise_hub Skill install_ref
  -> Hub returns published signed Skill capability
  -> GUI downloads signed Skill package
  -> dependency Skill signature verifies with seeded trust
  -> DataSrv registration and approval workflow runtime continue to pass
```

验证命令：

```powershell
cd D:\workprj\aicoder
go test ./internal/testfixtures -count=1
go test ./gui -run "TestInstallSelectedMaclawAppPackageFromHubUsesPackageSignatureTrustForDependencySkill|TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv|TestInstallSelectedMaclawAppPackageFromHubFiltersPackageApps" -count=1
go test ./internal/maclawappcontract -count=1
go test ./hub/internal/httpapi -run TestCapabilityMaclawAppSubmitApprovePublishListAndDownloadGoldenPath -count=1
```

验证结果：

```text
internal/testfixtures: ok
gui signed Hub install targeted suite: ok
internal/maclawappcontract: ok
hub/internal/httpapi golden path: ok
```

剩余缺口：依赖 Skill 下载/验签的 GUI 测试夹具已经共享化；下一步继续把真实 Hub `CapabilityMaclawAppPackageHandler` 输出直接接入 GUI 安装入口、依赖 Skill 安装、DataSrv app_installations 登记、审批 workflow 运行、单 App/全局审批中心回读，形成更少 mock 的最终黄金样例。

### 推进记录：共享 published approval fixture 升级为安装级 DataSrv 验收样例（2026-07-01）

本轮把 `internal/testfixtures.ReadyEnterpriseApprovalMaclawAppPublishedHubPackage` 从“GUI 可下载验签”的样例继续升级为“GUI 可安装、可装依赖、可登记 DataSrv”的审批型 Hub 样例。目标是减少 GUI、Hub、contract 各自手工构造 MaClaw App package 的比例，让后续最终 golden path 使用同一份更接近真实发布产物的 fixture。

本轮完成：

1. 共享 approval fixture 补齐安装级合同字段：
   - `binding.datasrv`：`finance.expense_forms`、`expense_request`、`finance.expense.v1`；
   - `binding.mis.approvalBindings`：`finance.submitted -> approval-ready-workflow@1.0.0`；
   - `binding.appSkill` 与 `binding.dependencies.skills` 改为 `enterprise_hub://capabilities/...` install_ref；
   - `app.ui.layouts`、`binding.ui.layouts`、`governance.workspaceLayout` 使用同一份 canonical layout，并通过共享 `WorkspaceLayoutFingerprint` 计算 fingerprint；
   - `governance.testProtocol`、`testEvidence.approvalInstance`、approval result payload、outputs、artifacts、view verification 全部补齐；
   - `dependencyVerification.skills` 与 `dependencyVerification.dependencies` 双字段保真，避免发布/安装门禁字段漂移。
2. `ApplyPublishedMaclawAppHubDownloadGovernance` 同步输出 installation-ready resolved dependencies：
   - package 顶层 `resolved_dependencies`；
   - app entry 内 `resolved_dependencies`；
   - source 为 `enterprise_hub`，install_ref 指向 signed Skill capability。
3. 新增 GUI 后端验收 `TestInstallSharedPublishedApprovalFixtureFromHubInstallsDependenciesAndRegistersDataSrv`：
   - Hub 返回共享 signed MaClaw App package；
   - GUI 下载并验 package signature；
   - package signature 公钥指纹进入依赖 Skill trust；
   - GUI preflight 两个 enterprise Hub Skill capability；
   - GUI 下载并验签 `approval-ready-app-skill` 与 `approval-ready-workflow`；
   - `InstallSelectedMaclawAppPackageFromHub` 真实写入 DataSrv `/api/v1/data/app-installations/approval-ready-app`；
   - DataSrv registration metadata 保留 Hub capability、published 状态、workflow contract、workspace layout fingerprint。
4. Hub golden path 断言同步更新为 canonical layout 语义：
   - `workspace_layout_primary_region=center`；
   - `workspace_layout_output_region=bottom`；
   - 继续断言 dependency summary 与 package signature/review evidence 保真。

当前链路进一步变为：

```text
shared published approval Hub package
  -> GUI DownloadMaclawAppPackageFromHub validates governance/signature
  -> GUI InstallSelectedMaclawAppPackageFromHub selects app
  -> enterprise_hub dependency preflight reads published Skill capability
  -> signed dependency Skill packages download and verify
  -> RecordMaclawAppInstall registers app_installations to DataSrv
  -> local install audit keeps DataSrv/workflow/layout/review evidence
```

验证命令：

```powershell
cd D:\workprj\aicoder
go test ./internal/testfixtures -count=1
go test ./gui -run "TestInstallSharedPublishedApprovalFixtureFromHubInstallsDependenciesAndRegistersDataSrv|TestDownloadMaclawAppPackageFromHubAcceptsSharedApprovalFixture" -count=1
go test ./gui -run "TestInstallSharedPublishedApprovalFixtureFromHubInstallsDependenciesAndRegistersDataSrv|TestInstallSelectedMaclawAppPackageFromHubUsesPackageSignatureTrustForDependencySkill|TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv|TestInstallSelectedMaclawAppPackageFromHubFiltersPackageApps" -count=1
go test ./internal/maclawappcontract -count=1
go test ./hub/internal/httpapi -run TestCapabilityMaclawAppSubmitApprovePublishListAndDownloadGoldenPath -count=1
```

验证结果：

```text
internal/testfixtures: ok
gui shared published approval install/DataSrv targeted suite: ok
gui signed Hub install targeted suite: ok
internal/maclawappcontract: ok
hub/internal/httpapi golden path: ok
```

剩余缺口：共享 published approval fixture 现在已经覆盖“下载 -> 选择安装 -> 依赖验签安装 -> DataSrv app_installations 登记”。下一步应继续把同一份 fixture 接到审批 workflow 启动与 DataSrv approval instance 回读，压成“安装后直接发起审批 -> workflow Skill 运行 -> 单 App/全局审批中心回看结果和文件”的最终少 mock golden path。

### 推进记录：共享 published approval fixture 串到审批 workflow 运行与审批中心回读（2026-07-01）

本轮继续沿用上一节已经升级为安装级样例的 `ReadyEnterpriseApprovalMaclawAppPublishedHubPackage`，把链路从 DataSrv app_installations 登记继续推进到运行态：安装完成后直接发起审批、运行 workflow Skill、同步 DataSrv approval instance，并从单 App 审批工作台和全局审批中心回读最终结果与文件产物。

本轮完成：

1. 扩展 GUI 后端验收 `TestInstallSharedPublishedApprovalFixtureFromHubInstallsDependenciesAndRegistersDataSrv`：
   - 先走真实 Hub package 下载、签名验证、选择安装、enterprise_hub 依赖 preflight、signed Skill 下载验签、DataSrv app_installations 登记；
   - 再注册已安装的 `approval-ready-workflow` runner；
   - 调用真实 `StartMaclawAppApprovalWorkflow(... RunWorkflowSkill=true)` 发起审批；
   - workflow Skill 返回 running progress 与 final approved approval instance；
   - GUI 把 progress 同步到 DataSrv `/api/v1/data/approvals/{id}/progress`；
   - GUI 把 final result 同步到 DataSrv `/api/v1/data/approvals/{id}/review`。
2. 新增运行态回读断言：
   - `ListMaclawAppApprovalInstances("approval-ready-app", "handled", 10)` 能从单 App 审批工作台读回；
   - `ListMaclawAppApprovalInstancesAll("handled", 10)` 能从全局审批中心读回；
   - 两个视角必须是同一个 workflow instance / approval id / decision id；
   - 回读结果保留 `approval_result=approved`、workflow node path、文本输出和 `approval-ready-run.pdf` 文件产物。
3. 这一步让同一份共享 fixture 覆盖到：

```text
Hub published approval package
  -> GUI install selected App
  -> signed dependency Skill install
  -> DataSrv app_installations registration
  -> StartMaclawAppApprovalWorkflow
  -> workflow Skill running progress
  -> DataSrv approval progress sync
  -> workflow Skill final approved result
  -> DataSrv approval review sync
  -> single-App handled lane readback
  -> global handled lane readback
```

验证命令：

```powershell
cd D:\workprj\aicoder
go test ./gui -run TestInstallSharedPublishedApprovalFixtureFromHubInstallsDependenciesAndRegistersDataSrv -count=1
go test ./gui -run "TestInstallSharedPublishedApprovalFixtureFromHubInstallsDependenciesAndRegistersDataSrv|TestInstallSelectedMaclawAppPackageFromHubUsesPackageSignatureTrustForDependencySkill|TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv|TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult" -count=1
go test ./internal/testfixtures ./internal/maclawappcontract -count=1
go test ./hub/internal/httpapi -run TestCapabilityMaclawAppSubmitApprovePublishListAndDownloadGoldenPath -count=1
```

验证结果：

```text
gui shared published approval install/run/readback golden path: ok
gui signed install + workflow runner targeted suite: ok
internal/testfixtures + internal/maclawappcontract: ok
hub/internal/httpapi golden path: ok
```

剩余缺口：共享 fixture 已经把后端少 mock golden path 压到“安装 -> 运行 -> 回读”。下一步应继续补 UI 层验收：App Studio/市场安装后的可视化运行页是否用同一份 install evidence 与 approval instance 数据驱动传统软件式工作台，包括我的申请、待我审批、已处理、需关注和文件结果展示。
### 推进记录：审批型 App UI 工作台 lane 与结果文件回读验收（2026-07-01）
本轮继续补齐运行态 UI 层证据。前一阶段已经证明后端 golden path 能从 Hub 安装、依赖 Skill 安装、DataSrv app_installations 登记推进到审批 workflow 运行与审批实例回读；本轮把同类数据推进到 `AppsPage` 的全局审批中心，验证企业审批型 App 的传统工作台视图不是只展示单条表单，而是能按审批工作语义组织实例。

本轮完成：
1. 新增前端验收 `shows approval instances across request, approval, attention, and handled lanes`：
   - mock `ListMaclawAppApprovalInstancesAll("all", 200)` 返回四条 DataSrv 审批实例；
   - 覆盖 `my_requests`、`pending_my_approval`、`attention`、`handled` 四类 lane；
   - 验证 lane 计数分别为 1，且切换 lane 后只展示对应业务实例；
   - 验证详情区保留 workflow skill/version、当前节点路径、业务记录 ID、远程 approval id、business/result status；
   - 验证结果包展示 structured payload、文本输出、业务记录输出和 PDF artifact；
   - 验证已处理实例的 Approve / Reject / Mark attention 操作保持只读禁用。
2. 复跑已有 Hub 安装证据 UI 回读用例：
   - `installs an approved Hub approval app with runtime install evidence` 继续通过；
   - 证明新增 lane 验收没有破坏 Hub 审核通过 App 安装后，运行页和全局审批中心读取 install evidence / approval instance / artifact 的路径。

当前 UI 验收链路变为：

```text
DataSrv approval instances
  -> AppsPage global Approval status
  -> lane counts
  -> my requests / pending my approval / needs attention / handled filters
  -> approval detail facts
  -> result package payload
  -> document artifact display
  -> handled approval read-only actions
```

验证命令：
```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "shows approval instances across request, approval, attention, and handled lanes"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "installs an approved Hub approval app with runtime install evidence"
```

验证结果：
```text
AppsPage approval lane UI targeted test: ok
AppsPage Hub install evidence UI targeted test: ok
```

### 推进记录：企业普通 App Studio 动态布局保存验收（2026-07-01）
本轮继续推进 App Studio 制作端闭环。上一轮补齐了审批型 App 运行态多 lane 工作台；本轮把同一套“动态 UI 生成、用户可视化调节、保存到 app 信息文件、进入治理证据”的要求扩展到企业普通应用，避免 App Studio 只对审批型 App 有完整布局证据。

本轮完成：
1. 新增前端验收 `saves an enterprise normal app Studio layout into its app skill definition`：
   - 在 App Studio 创建 `enterprise_normal_app`；
   - 选择 `customer-console-skill` 作为 appSkill；
   - 配置 DataSrv domain、objectRole、preferredAction、preferredView、preferredReport、preferredDashboard；
   - 使用可视化布局设计器把 `business_workspace` 调整为 `dashboard / spacious / primaryRegion=center / outputRegion=right`；
   - 调整 `operation_form`、`record_detail`、`record_list`、`output_panel` 的布局区域和顺序；
   - 点击 `Save to Skill` 后断言写入 `customer-console-skill/maclaw.app.json`。
2. 验证保存产物同时进入 `app.binding.datasrv`、`app.binding.appSkill`、`app.binding.ui.layouts.business_workspace`、`app.governance.workspaceLayout`、`app.governance.resultContract`、`app.governance.testProtocol`。
3. 验证 `business_workspace` 的 template、density、primaryRegion、outputRegion 与用户可视化调节一致，regions 保留 role、placement、order，且 governance.workspaceLayout 与 binding.ui layout 共享 fingerprint。

当前制作端链路进一步变为：

```text
App Studio create enterprise_normal_app
  -> choose appSkill
  -> configure DataSrv operation/view/report/dashboard
  -> visually adjust business_workspace layout
  -> save maclaw.app.json to Skill
  -> binding.ui + governance.workspaceLayout share fingerprint
  -> resultContract/testProtocol stay attached to the app package contract
```

验证命令：
```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "saves an enterprise normal app Studio layout into its app skill definition"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "creates, tests, and submits an enterprise approval app from App Studio"
```

验证结果：
```text
AppsPage enterprise normal Studio layout save targeted test: ok
AppsPage enterprise approval Studio create/test/submit targeted test: ok
```

### 推进记录：工具型 App Studio 动态布局保存与上传前测试证据验收（2026-07-01）

本轮继续补齐 App Studio 制作端闭环，把工具型 App 从“选择 Skill + 保存名称”的薄验收推进到“可视化布局调节 + 保存到 app 信息文件 + 当前版本测试证据 + 上传前自动保存”的完整链路。这样三类 App（企业审批型、企业普通型、工具型）在 Studio 侧都开始具备动态 UI 布局进入 manifest 和 governance evidence 的验收锚点。

本轮完成：

1. 扩展前端验收为 `saves the latest tool app layout and test evidence before uploading to SkillMarket`：
   - 在 App Studio 中选择已安装 `invoice-review` Skill；
   - 将工具型 App 命名为 `发票审核`；
   - 通过可视化布局设计器调整 `tool_workspace` 为 `dashboard / compact / primaryRegion=center / outputRegion=bottom`；
   - 调整 `file_queue`、`settings_panel`、`preview_panel`、`output_panel` 的 placement、visible、order；
   - 保存到 `invoice-review/maclaw.app.json` 后，为当前版本种入成功运行证据；
   - 点击 `上传到 SkillMarket` 时，先自动保存最新 app 定义，再调用 `UploadNLSkillToMarket("invoice-review")`。
2. 修正上传前保存路径的证据桥接：
   - `uploadSelectedSkillApp` 现在先保存通过闸门校验的 `currentRunEvidence`；
   - `persistSkillAppDefinition(currentRunEvidence)` 将面板入口上的当前版本测试证据带入本次 Skill manifest；
   - `appToManifest` 因此能在保存产物中生成 `app.governance.testEvidence`，避免上传通过但 Skill 定义缺少测试证据。
3. 验证保存产物包含完整工具型 App 契约：
   - `schema = maclaw.app.v1`、`privateMarker = x_maclaw_apps`、`installUnit = skill`；
   - `app.kind = tool_app`、`launchMode = fixed_skill_ui`；
   - `app.binding.skill.appDefinitionFile = maclaw.app.json`；
   - `app.binding.ui.entry = tool_workspace`；
   - `app.binding.ui.layouts.tool_workspace` 保存 template、density、primaryRegion、outputRegion、regions 与 studio 标记；
   - `app.governance.workspaceLayout` 与 binding layout 共享 fingerprint，并记录 visibleRegionCount、regionIds、regions；
   - `app.governance.resultContract`、`app.governance.testProtocol`、`app.governance.testEvidence` 同时进入上传前保存产物；
   - test evidence 覆盖 artifact、output、resultCoverage，证明“当前版本已测试”不只是上传按钮闸门，而是可分发治理证据。

当前工具型制作端链路变为：

```text
App Studio create/select tool_app
  -> choose installed Skill
  -> visually adjust tool_workspace layout
  -> save maclaw.app.json to Skill
  -> run/test current app version
  -> upload to SkillMarket
  -> auto-save latest manifest before upload
  -> binding.ui + governance.workspaceLayout + governance.testEvidence preserved
```

验证命令：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "saves the latest tool app layout and test evidence before uploading to SkillMarket"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "saves an enterprise normal app Studio layout into its app skill definition"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "creates, tests, and submits an enterprise approval app from App Studio"
```

验证结果：

```text
AppsPage tool App Studio layout/test-evidence upload targeted test: ok
AppsPage enterprise normal Studio layout save targeted test: ok
AppsPage enterprise approval Studio create/test/submit targeted test: ok
```

剩余缺口：Studio 制作端三类 App 的主闭环已经进一步收口；后续应继续推进安装/运行端对工具型 App 的布局恢复、Hub 安装后的依赖检查证据回读，以及 DataSrv/Hub/GUI 对同一份 app governance evidence 的跨层一致性验收。

### 推进记录：工具型 App 市场安装后运行端布局与测试证据回读（2026-07-01）

本轮从制作端继续推进到安装/运行端。上一轮已经证明工具型 App 在 App Studio 中可以保存 `tool_workspace` 动态布局，并在上传前把当前版本测试证据写入 `app.governance.testEvidence`；本轮补齐“被安装的一方”是否还能读回这份布局和证据，避免能力市场分发后运行界面退回成固定表单。

本轮完成：

1. 修正 `maclaw.app.v1` 市场包解析：
   - `manifestToAppEntry` 现在读取 `app.governance.testEvidence` / `test_evidence`；
   - 将 `runId/runID/run_id`、`verifiedAt/verified_at`、`definitionHash`、artifact、outputs、resultCoverage 等测试证据归一为 `importedRunEvidence`；
   - 同时保留 `governance.workflowContract`，让市场安装包和已安装 Skill 定义在治理证据语义上保持一致。
2. 新增前端验收 `restores installed tool app workspace layout and test evidence in the runtime UI`：
   - 从市场粘贴安装一个 `tool_app`；
   - manifest 中携带 `binding.ui.layouts.tool_workspace`，布局为 `dashboard / compact / primaryRegion=center / outputRegion=bottom`；
   - regions 覆盖 `file_queue`、`settings_panel`、`output_panel`、`preview_panel`，包含 placement、visible、order；
   - governance 中携带 `workspaceLayout`、`resultContract`、`testProtocol`、`testEvidence`；
   - 安装后打开 App，验证运行页 `.apps-runtime-layout` 的 `data-template`、`data-density`、`data-primary-region`、`data-output-region`、`data-region-count`；
   - 验证输入区、状态区、输出区、运行历史分别恢复到 manifest 指定的 center/right/bottom 区域；
   - 验证运行历史读取安装包中的 `run-tool-layout-install` 和 `translated-output.pdf`；
   - 验证本地面板保存的 AppEntry 已保留 `importedRunEvidence` 和 `tool_workspace` 布局。

当前安装/运行端链路变为：

```text
SkillMarket maclaw.app.v1 package
  -> manifestToAppEntry
  -> binding.ui.tool_workspace saved in AppEntry.manifest
  -> governance.testEvidence normalized to importedRunEvidence
  -> install into app panel
  -> open runtime AppPreview
  -> runtimeWorkspaceLayoutForApp
  -> apps-runtime-layout data attributes
  -> input/status/output/history regions restored
  -> imported run evidence appears in run history
```

验证命令：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "restores installed tool app workspace layout and test evidence in the runtime UI"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "uses installed skill app output modes in the runtime UI"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "restores enterprise MaClaw App skill definitions without downgrading them to tool apps"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "saves the latest tool app layout and test evidence before uploading to SkillMarket"
```

验证结果：

```text
AppsPage installed tool runtime layout/test-evidence readback targeted test: ok
AppsPage installed skill app output modes targeted test: ok
AppsPage enterprise skill definition restore targeted test: ok
AppsPage tool Studio upload evidence targeted test: ok
```

剩余缺口：工具型 App 从 Studio 保存、测试、上传到市场安装后的运行页布局/证据回读已经形成一条前端闭环。后续应继续推进 Hub 安装 API 与 DataSrv app_installations 记录中的同一份 `workspace_layout`、`test_evidence`、`dependency_verification` 跨层一致性，尤其是企业审批型 App 的审批实例与工具型 App 的运行历史在安装记录中的统一治理证据模型。

### 推进记录：DataSrv app_installations 治理证据同源一致性（2026-07-01）

本轮继续把前端闭环向后端安装记录推进。前面已经证明工具型 App 安装后运行页能恢复 `tool_workspace` 布局和测试证据；本轮补齐 GUI 安装记录写入 DataSrv `app_installations` 时，`workspace_layout`、`test_evidence`、`dependency_verification` 不能只作为 UI 临时状态存在，而要进入 DataSrv metadata 与 `install_evidence` 的同一套治理证据。

本轮完成：

1. 修正 `maclawAppWorkspaceLayoutMetadataForEntry`：
   - 当 `app.governance.workspaceLayout` 存在且带 fingerprint/regionIds/visibleRegionCount 时，仍优先使用治理摘要；
   - 同时从 `binding.ui.layouts[entry]` 补齐 governance 摘要缺失的 `regions`、`regionCount`、`visibleRegionCount`、`schema`、`generated`、`studio`；
   - 保留 governance fingerprint 与主布局字段，避免因为摘要优先而丢掉动态 UI 的完整区域结构。
2. 扩展后端验收 `TestMaclawAppDataSrvInstallationPayloadsScopeDependenciesPerApp`：
   - 为 `enterprise_normal_app` 加入 `business_workspace` 动态布局；
   - 验证 DataSrv metadata 写入 `workspace_layout`；
   - 验证扁平字段 `workspace_layout_entry/template/density/primary_region/output_region/fingerprint/region_count/visible_region_count/region_ids`；
   - 验证 `metadata.install_evidence.workspace_layout` 与 metadata 顶层布局同源；
   - 验证 `metadata.install_evidence.test_evidence` 保留 `run-selected-1`；
   - 验证 `metadata.install_evidence.dependency_verification` 是按 App 过滤后的依赖验证结果，不串入其它 App 的依赖。

当前 DataSrv 安装登记链路变为：

```text
maclaw.app.v1 package
  -> parseMaclawAppInstallEntries
  -> PlanMaclawAppInstall dependency verification
  -> maclawAppInstallEvidenceByApp
  -> maclawAppDataSrvInstallationPayloads
  -> metadata.workspace_layout
  -> metadata.test_evidence
  -> metadata.dependency_verification
  -> metadata.install_evidence.{workspace_layout,test_evidence,dependency_verification}
  -> PUT /api/v1/data/app-installations/{app_id}
```

验证命令：

```powershell
cd D:\workprj\aicoder
go test ./gui -run TestMaclawAppDataSrvInstallationPayloadsScopeDependenciesPerApp -count=1
go test ./gui -run "TestRecordMaclawAppInstallPersistsNewestInstallAudit|TestMaclawAppInstallEvidenceGeneratesDependencyVerification|TestMaclawAppDataSrvInstallationPayloadsScopeDependenciesPerApp" -count=1
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "restores installed tool app workspace layout and test evidence in the runtime UI"
```

验证结果：

```text
GUI DataSrv app_installations governance evidence payload targeted test: ok
GUI install audit/evidence regression targeted suite: ok
AppsPage installed tool runtime layout/test-evidence readback targeted test: ok
```

剩余缺口：DataSrv 安装登记 payload 已经能承载布局、测试证据和依赖验证；后续应继续推进 DataSrv 持久化/HTTP 层对这些字段的 round-trip 验收，以及 GUI 从 DataSrv `ListMaclawAppInstalls` / installed app discovery 回读同一份治理证据后驱动审批中心和工具运行页。

剩余缺口：运行态审批中心已经具备多 lane 和结果文件的 UI 证据；下一步应继续推进 App Studio 制作端闭环，把自动生成的企业审批型/企业普通型/工具型工作台布局、用户可视化调节、布局保存、测试证据生成和上传 Hub 串到同一份 app 信息文件与能力市场发布流程。

### 推进记录：DataSrv app_installations 持久化与 HTTP 回读治理证据验收（2026-07-01）

本轮把上一轮的 GUI -> DataSrv 安装登记 payload 继续向 DataSrv 自身收口。上一轮已经证明 GUI 写入 `metadata.workspace_layout`、`metadata.test_evidence`、`metadata.dependency_verification` 与 `metadata.install_evidence`；本轮补上 DataSrv 的 SQLite 持久化和 HTTP round-trip 证据，避免这些字段只在创建响应中短暂存在，重启/重新打开数据库后丢失。

本轮完成：

1. 新增 DataSrv HTTP 验收 `TestHTTPServerAppInstallationPersistsGovernanceEvidenceAcrossSQLiteReopen`：
   - 通过 `PUT /api/v1/data/app-installations/{appId}` 写入一个企业审批型 App；
   - metadata 同时携带 `workspace_layout`、`workflow_contract`、`result_contract`、`dependency_verification`、`test_evidence`、`install_evidence`；
   - 关闭 SQLite store 后重新打开同一个 `data.db`；
   - 使用新的 HTTP server 执行 `GET /api/v1/data/app-installations/{appId}`；
   - 验证回读后仍保留完整 `workspace_layout.regions`、`workspace_layout.fingerprint`、`test_evidence.artifacts`、`dependency_verification.dependencies`；
   - 验证 `install_evidence.workspace_layout` 与 `install_evidence.dependency_verification` 没有被 DataSrv 扁平化过程吞掉。

2. 同一测试继续验证过滤查询在重开库后仍可命中：
   - `workspace_layout_fingerprint=layout-roundtrip-approval`；
   - `definition_fingerprint=sha256:governance-roundtrip`；
   - `has_blocking_dependency=false`。

当前 DataSrv 证据链路变为：

```text
GUI install payload
  -> PUT /api/v1/data/app-installations/{appId}
  -> normalizeAppInstallationMetadata
  -> SQLite app_installations.metadata_json
  -> reopen SQLite store
  -> GET /api/v1/data/app-installations/{appId}
  -> List /api/v1/data/app-installations filters
  -> workspace_layout/test_evidence/dependency_verification/install_evidence round-trip verified
```

验证命令：

```powershell
cd D:\workprj\aicoder\datasrv
go test ./structureddata -run TestHTTPServerAppInstallationPersistsGovernanceEvidenceAcrossSQLiteReopen -count=1
go test ./structureddata -run "Test.*AppInstallation.*" -count=1
go test ./structureddata -run "TestHTTPServerAppInstallationsOverrideObjectRoleBindings|TestHTTPServerAppInstallationPUTRoundTripsGUIEquivalentPayloadThroughCapabilities|TestHTTPServerAppInstallationPUTNormalizesStudioGovernancePayload|TestHTTPServerAppInstallationPersistsGovernanceEvidenceAcrossSQLiteReopen" -count=1
```

验证结果：

```text
DataSrv app_installations SQLite reopen/HTTP round-trip targeted test: ok
DataSrv app installation targeted suite: ok
DataSrv app installation HTTP targeted suite: ok
```

剩余缺口：DataSrv 自身对治理证据的持久化与 HTTP 回读已经有直接验收；下一步应继续推进 GUI 从 DataSrv `ListMaclawAppInstalls` / installed app discovery 回读同一份 `workspace_layout`、`test_evidence`、`dependency_verification` 后驱动运行页和审批中心，随后再把真实 Hub handler 输出、GUI 安装入口、依赖 Skill 安装、DataSrv 登记、审批 workflow 运行与结果回读压成更少 mock 的黄金链路。

### 推进记录：GUI 从 DataSrv install_evidence 回读并驱动运行页（2026-07-01）

本轮承接 DataSrv `app_installations` 持久化/HTTP round-trip 的结果，继续补 GUI installed app discovery 的回读闭环。目标是证明 DataSrv 重启或重新打开数据库后返回的 `install_evidence`，不只是显示在治理摘要里，而是能恢复 MaClaw App 的动态布局、测试证据和依赖验证，并驱动运行页的传统软件式工作台。

本轮完成：

1. 加强 `dataSrvInstalledRunEvidence`：
   - 原来主要读取顶层 `metadata.test_evidence`；
   - 现在在顶层缺失时 fallback 到 `metadata.install_evidence.test_evidence` / `installEvidence.testEvidence`；
   - `importedRunEvidence` 因此能从安装证据里恢复 `runID`、`definitionFingerprint`、`testProtocolFingerprint`、`outputs`、`artifacts`、`approvalInstance`。

2. 加强 `dataSrvInstalledWorkspaceLayout`：
   - 原来主要读取顶层 `metadata.workspace_layout`；
   - 现在在顶层缺失时 fallback 到 `metadata.install_evidence.workspace_layout` / `installEvidence.workspaceLayout`；
   - 运行页 `runtimeWorkspaceLayoutForApp` 可以继续拿到 `entry/template/density/primaryRegion/outputRegion/regions/navigation/list`。

3. 加强 `dataSrvInstalledDependencyVerificationEvidence`：
   - 原来主要读取测试证据或顶层依赖验证；
   - 现在支持从 `install_evidence.dependency_verification` 恢复依赖检查结果；
   - `importedRunEvidence.dependencyVerification` 和运行态依赖面板能保留 `dependencies/install_ref/installed/health`。

4. 加强 `dataSrvInstalledTestProtocol`：
   - 支持从 `install_evidence.test_evidence.test_protocol` 读取测试协议；
   - 保留测试协议 fingerprint，避免安装证据回读后测试证据摘要降级。

5. 新增前端验收 `restores DataSrv reopened install evidence into runtime layout and evidence panels`：
   - 模拟 DataSrv 返回一个企业审批型 App，顶层不重复 `workspace_layout/test_evidence/dependency_verification`，只在 `install_evidence` 里携带完整证据；
   - 从 App Studio “可生成应用”加入面板；
   - 验证保存后的 AppEntry 恢复：
     - `manifest.ui.layouts.approval_workspace`；
     - `importedRunEvidence`；
     - `installEvidence.workspace_layout/test_evidence/dependency_verification`；
   - 打开运行页，验证 `.apps-runtime-layout` 的 `data-template/data-density/data-primary-region/data-output-region/data-region-count`；
   - 验证输入区、审批工作台、输出区分别落到 DataSrv install evidence 指定的 region；
   - 验证运行页 `Install governance` 面板显示 workspace layout、test evidence、dependency verification。

当前 GUI 回读链路变为：

```text
DataSrv app_installations HTTP response
  -> app_installations[].metadata.install_evidence
  -> buildDataSrvAppCandidates
  -> dataSrvInstalledAppCandidate
  -> manifest.ui.layouts[approval_workspace]
  -> importedRunEvidence
  -> installEvidence
  -> AppPreview runtimeWorkspaceLayoutForApp
  -> apps-runtime-layout + Install governance panel
```

验证命令：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "restores DataSrv reopened install evidence into runtime layout and evidence panels"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "restores DataSrv installed enterprise normal app run evidence into app candidates"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "turns DataSrv installed MaClaw apps into addable app candidates with layout metadata"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "blocks DataSrv installed MaClaw apps at runtime when dependency verification fails"
```

验证结果：

```text
AppsPage DataSrv reopened install_evidence runtime layout/evidence targeted test: ok
AppsPage DataSrv enterprise normal installed evidence targeted test: ok
AppsPage DataSrv approval installed app candidate targeted test: ok
AppsPage DataSrv installed dependency blocking runtime targeted test: ok
```

剩余缺口：DataSrv -> GUI 回读同一份安装治理证据并驱动运行页已经闭合；下一步应继续把真实 Hub handler 输出、GUI 安装入口、依赖 Skill 下载验签安装、DataSrv 登记、审批 workflow 运行、单 App/全局审批中心回读整合成更少 mock 的黄金链路。

### 推进记录：Hub 安装包签名证据进入 GUI 运行态治理面板验收（2026-07-01）

本轮继续压实“真实 Hub 治理证据 -> GUI 安装 -> 运行页回读”的 UI 验收。此前 Hub golden path 已经证明真实 `CapabilityMaclawAppPackageHandler` 的下载结果可以被共享 `maclawappcontract.DownloadGUIInstallHubPackage` 与 `SelectHubPackageApps` 消费；本轮把前端市场安装用例中的 Hub 包签名证据补齐，避免安装成功后运行态只显示来源、依赖和 DataSrv 登记，而没有验收签名信任链。

本轮完成：
1. 扩展前端验收 `installs approved Hub MaClaw Apps from market search results`：
   - 在 Hub package mock 中加入 `package_signature`；
   - 在 `install_record` 顶层加入 `package_sha/package_sha256`、`hub_package_signature` 和签名摘要字段；
   - 在单 App `install_evidence.submission.package_signature` 中保留同一份签名证据；
   - 验证市场安装行的 install evidence snapshot 显示 `Package signature`；
   - 验证安装后保存到面板的 `AppEntry.installEvidence.hub_package_signature` 保留 algorithm、fingerprint、signed_by；
   - 验证运行页 `Install governance` 面板显示 `ed25519 · sha256:contract-package-key · enterprise-market`。
2. 复跑 DataSrv 回读相关前端用例，确认签名证据补强没有破坏 DataSrv `install_evidence` 回读、动态布局恢复和已安装 App 候选生成。

当前 GUI 运行态治理证据链路进一步变为：

```text
Enterprise Hub package_signature
  -> GUI market install result.install_record.hub_package_signature
  -> per-App install_evidence.submission.package_signature
  -> local AppEntry.installEvidence
  -> InstallRecordEvidenceSnapshot
  -> RuntimeInstallGovernancePanel
  -> Package signature evidence visible beside source/layout/test/dependency/DataSrv evidence
```

验证命令：

```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "installs approved Hub MaClaw Apps from market search results"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "restores DataSrv reopened install evidence into runtime layout and evidence panels"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "turns DataSrv installed MaClaw apps into addable app candidates with layout metadata"
```

验证结果：

```text
AppsPage Hub market install package-signature governance targeted test: ok
AppsPage DataSrv reopened install_evidence runtime layout/evidence targeted test: ok
AppsPage DataSrv installed app candidate layout metadata targeted test: ok
```

剩余缺口：Hub handler 输出到 GUI 安装运行态的签名证据展示已经有前端验收；后续仍需把真实依赖 Skill 下载、依赖包签名/完整性诊断、DataSrv 登记、审批 workflow 运行与审批中心回读合成更少 mock 的端到端黄金链路，并补失败路径（签名失败、依赖缺失、DataSrv 登记部分失败、workflow 需补充/拒绝/需关注）。

### 推进记录：依赖 Skill 预检/完整性诊断进入 DataSrv 安装治理证据（2026-07-01）

本轮继续推进 MaClaw App 安装时的 Skill 依赖治理闭环。前面已经覆盖了依赖安装失败分类、SkillMarket/Enterprise Hub 预检，以及 Hub 包签名在 GUI 运行态的展示；本轮补上一个更底层的证据一致性缺口：当 App manifest 自带 `governance.dependencyVerification` 时，GUI 后端合并安装计划依赖不应只保留 `id/source/action/health`，还必须把安装引用解析、远端预检、包校验、包签名和完整性诊断写入同一份 `dependency_verification.dependencies[]`，并随 DataSrv `app_installations.metadata.install_evidence` 回读。

本轮完成：
1. 扩展 `maclawAppMergedDependencyVerificationItems`：
   - 合并 `install_ref_kind/install_ref_target/install_ref_version/install_ref_status/install_ref_message`；
   - 合并 `preflight_status/preflight_code/preflight_stage/preflight_message`；
   - 合并 `package_sha256/package_checksum/package_signature/package_download_url`；
   - 合并 `integrity_status/integrity_code/integrity_stage/integrity_message`；
   - 合并 `install_error_code/install_error_stage/install_error_detail`。
2. 补齐 `dependency_verification` 时间字段别名：
   - manifest 中已有 `verifiedAt` 时，DataSrv/GUI 可同时得到 `verified_at`；
   - 已有 `verified_at` 时，也保留 `verifiedAt`；
   - 避免 Hub/GUI/DataSrv 在 camelCase/snake_case 之间丢失依赖验证时间。
3. 扩展后端验收 `TestMaclawAppDataSrvInstallationPayloadsScopeDependenciesPerApp`：
   - 构造已有 `governance.dependencyVerification` 的企业普通应用；
   - 构造带 `install_ref`、预检、包签名、完整性诊断的依赖；
   - 验证 DataSrv 顶层 `metadata.dependency_verification.dependencies[]` 保留这些字段；
   - 验证 `metadata.install_evidence.dependency_verification.dependencies[]` 同步保留下载 URL 和完整性 stage；
   - 继续验证依赖按 App 作用域过滤，不串入其它 App 的阻塞依赖。

当前依赖治理证据链路进一步变为：

```text
maclaw.app dependency declaration
  -> install_ref parsing
  -> SkillMarket / Enterprise Hub preflight
  -> package sha/signature/download URL integrity metadata
  -> InstallMaclawAppDependencies install diagnostics
  -> maclawAppMergedDependencyVerificationItems
  -> metadata.dependency_verification.dependencies[]
  -> metadata.install_evidence.dependency_verification.dependencies[]
  -> DataSrv app_installations
  -> GUI installed app discovery / runtime Install governance
```

验证命令：

```powershell
cd D:\workprj\aicoder
go test ./gui -run TestMaclawAppDataSrvInstallationPayloadsScopeDependenciesPerApp -count=1
go test ./gui -run "TestRecordMaclawAppInstallPersistsNewestInstallAudit|TestMaclawAppInstallEvidenceGeneratesDependencyVerification|TestMaclawAppDataSrvInstallationPayloadsScopeDependenciesPerApp" -count=1
go test ./gui -run "TestInstallMaclawAppDependenciesClassifiesInstallFailures|TestPlanMaclawAppInstallPreflightsSkillMarketDependency|TestPlanMaclawAppInstallPreflightsEnterpriseHubCapability" -count=1
```

验证结果：

```text
GUI DataSrv dependency verification diagnostics payload targeted test: ok
GUI install audit/evidence dependency verification regression suite: ok
GUI dependency install failure/preflight targeted suite: ok
```

过程中还发现当前工作树里 `gui/skill_runner.go` 的 live output 改动曾阻塞 `go test ./gui` 编译；对该文件执行 `gofmt` 后语法阻塞解除，未改变本轮 MaClaw App 依赖治理逻辑。

剩余缺口：依赖预检/完整性诊断已经能进入 DataSrv 安装治理证据；下一步应继续把真实依赖 Skill 下载与包签名校验本身纳入少 mock 黄金链路，并让 GUI 运行态对依赖诊断提供更可操作的展示，例如安装引用目标、远端预检失败原因、包签名缺失/不可信、下载 URL 与完整性状态。

### 推进记录：GUI 运行态展示依赖预检与完整性诊断（2026-07-01）

本轮把上一节已经进入 DataSrv 安装治理证据的依赖诊断继续推进到 GUI 运行态。目标是让企业用户安装或运行 MaClaw App 时，不只看到“依赖数量/阻塞数量”，还可以直接审计每个依赖 Skill 的安装引用、预检状态、包指纹、包签名、下载来源和完整性诊断。

本轮完成：
1. 在 `InstallRecordEvidenceSnapshot` 与 `RuntimeInstallGovernancePanel` 共享的 `installRecordEvidenceItems` 中新增 `Dependency diagnostics / 依赖诊断` 证据行：
   - 优先读取 `install_record.dependency_verification.dependencies[]`；
   - 没有 verification 细节时回退到 `install_record.dependencies[]`；
   - 展示 `ref-kind`、`target`、`ref-status`、`preflight`、`integrity`、`sha`、`signature:available`、`download:available` 和安装错误阶段；
   - 最多展示前两个依赖，更多依赖用 `+N` 收敛，避免运行页治理区变成日志墙。
2. 扩展前端 Hub 市场安装验收 `installs approved Hub MaClaw Apps from market search results`：
   - mock 的 `contract-workflow` 依赖现在携带 `install_ref`、`install_ref_target/version/status`、`preflight_status/code/stage`、`package_sha256`、`package_signature`、`package_download_url`、`integrity_status/code/stage`；
   - 市场安装快照断言能看到 `Dependency diagnostics`；
   - 运行态 `Install governance` 断言能看到同一份依赖诊断，包括 `target:contract-workflow@1.0.0`、`integrity:ready`、`sha:sha-contract-workflow`、`signature:available`、`download:available`。
3. 调整市场安装失败反馈：
   - `MarketInstallFeedbackMessage` 改为函数声明，避免运行态引用初始化顺序问题；
   - 依赖安装失败时直接显示原始诊断摘要，不再把 `signed-workflow / package_integrity_failed / skillhub_download / public key fingerprint not trusted` 这类可操作信息折叠在额外点击后。
4. 顺手把前端测试中的旧 tab 文案 `Add from market` 对齐为当前界面 `App Market`，并修复测试文件中残留的编码替换字符，避免后续前端用例被无关编码问题阻塞。

当前 GUI 运行态治理证据链变为：

```text
install_record.dependency_verification.dependencies[]
  -> installRecordDependencyDiagnostics
  -> InstallRecordEvidenceSnapshot
  -> RuntimeInstallGovernancePanel
  -> market install success evidence
  -> runtime Install governance
  -> dependency install failure diagnostics visible in market row
```

验证命令：
```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "installs approved Hub MaClaw Apps from market search results"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "restores DataSrv reopened install evidence into runtime layout and evidence panels"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "turns DataSrv installed MaClaw apps into addable app candidates with layout metadata"
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "shows Hub MaClaw App dependency install diagnostics when one-click install fails"
```

验证结果：
```text
AppsPage Hub market install dependency diagnostics targeted test: ok
AppsPage DataSrv reopened install_evidence runtime layout/evidence targeted test: ok
AppsPage DataSrv installed app candidate layout metadata targeted test: ok
AppsPage Hub dependency install failure diagnostics targeted test: ok
```

剩余缺口：GUI 运行态现在能展示依赖预检/完整性诊断；下一步继续把真实 Hub handler 输出、真实依赖 Skill 下载验签、DataSrv app_installations 登记、审批 workflow 运行、单 App/全局审批中心回读压成更少 mock 的端到端黄金链路，并补齐签名失败、依赖缺失、DataSrv 登记部分失败、审批拒绝/需关注/补充材料等失败路径。

### 推进记录：共享 GUI 安装合同强制依赖验证明细（2026-07-01）

本轮继续把少 mock 黄金链路向合同层收紧。此前真实 Hub `CapabilityMaclawAppPackageHandler` 输出已经可以被共享 GUI HTTP 下载客户端、包签名校验和子 App 选择逻辑消费；本轮进一步要求 Hub 下载包中每个 App 的 `governance.dependencyVerification` 必须完整存在，且必须包含非阻塞的依赖明细。这样 GUI 安装入口在真正开始安装前，就能通过共享合同确认“这个 MaClaw App 不只是发布状态正确，还带着可审计的依赖验证证据”。

本轮完成：
1. 扩展 `internal/maclawappcontract.ValidateGUIInstallHubPackage`：
   - 每个下载 App 必须包含 `governance.dependencyVerification` 或 `governance.dependency_verification`；
   - `blocked / has_blocking_dependency / has_missing_required` 等字段不能声明阻塞；
   - `dependencies` 或 `skills` 必须至少包含一条依赖明细；
   - 继续保留原有包签名、submission、review evidence、resolved dependencies 等检查。
2. 补充合同负向测试：
   - 缺失 dependency verification 时拒绝；
   - dependency verification 声明阻塞依赖时拒绝。
3. 强化 Hub 真实 handler golden path `TestCapabilityMaclawAppSubmitApprovePublishListAndDownloadGoldenPath`：
   - 真实提交 -> 审批 -> 发布 -> 列表 -> `CapabilityMaclawAppPackageHandler` 下载；
   - 共享 `DownloadGUIInstallHubPackage` 通过真实 handler 输出下载；
   - 共享 `SelectHubPackageApps` 选择 `market-approval-ready-app` 后再次通过 GUI 安装合同；
   - 明确断言选择后的包仍保留 `resolved_dependencies`、`dependencyVerification.dependencies[]` 和 workflow dependency 的 `install_ref`。
4. 回归 GUI 后端共享审批 fixture：确认更严格的合同没有破坏 `Hub 下载 -> 依赖 signed Skill 下载验签 -> DataSrv app_installations 登记 -> workflow Skill 运行 -> 单 App/全局审批中心回读`。

当前合同门禁链路变为：

```text
CapabilityMaclawAppPackageHandler real output
  -> DownloadGUIInstallHubPackage
  -> VerifyHubPackageSignature
  -> ValidateGUIInstallHubPackage
      -> package signature
      -> Hub submission/review evidence
      -> resolved_dependencies
      -> governance.dependencyVerification.dependencies[] non-blocking
  -> SelectHubPackageApps
  -> ValidateGUIInstallHubPackage again
  -> GUI install / dependency install / DataSrv registration / workflow run
```

验证命令：
```powershell
cd D:\workprj\aicoder
go test ./internal/maclawappcontract -run "TestValidateGUIInstallHubPackage|TestSelectHubPackageApps" -count=1
go test ./hub/internal/httpapi -run TestCapabilityMaclawAppSubmitApprovePublishListAndDownloadGoldenPath -count=1
go test ./gui -run TestInstallSharedPublishedApprovalFixtureFromHubInstallsDependenciesAndRegistersDataSrv -count=1
```

验证结果：
```text
internal/maclawappcontract GUI install contract dependency verification tests: ok
Hub real handler submit/approve/publish/download GUI contract golden path: ok
GUI shared approval fixture install/dependency/DataSrv/workflow/readback golden path: ok
```

剩余缺口：少 mock 后端黄金链路已经继续收紧到共享合同层；下一步应补失败路径的端到端验收，优先覆盖真实 Hub 包签名失败、依赖 Skill 包签名不可信、DataSrv 登记部分失败，以及审批 workflow 产生拒绝/需关注/补充材料时的 DataSrv 与 GUI 回读一致性。

### 推进记录：依赖 Skill 包签名不可信时阻断 MaClaw App 安装（2026-07-01）

本轮继续补全失败路径的端到端验收，优先覆盖“App 包签名可信，但它依赖的 workflow Skill 包由未信任密钥签名”的高风险场景。这个场景必须在安装阶段被阻断，不能等到 App 已写入本地面板或 DataSrv `app_installations` 后才暴露，否则企业审批型应用会出现“入口已安装、审批流不可运行、审计证据不可信”的断链。

本轮完成：
1. 新增 GUI 后端验收 `TestInstallSharedPublishedApprovalFixtureBlocksUntrustedDependencySkillSignature`：
   - MaClaw App Hub package 使用可信公钥签名；
   - App Skill 依赖也使用同一可信密钥签名，可以通过安装；
   - workflow Skill 依赖使用另一组未信任密钥签名；
   - 安装 `approval-ready-app` 时必须在 workflow Skill 完整性校验阶段失败。
2. 明确失败边界：
   - 错误信息必须暴露 `approval-ready-workflow`、`package_integrity_failed`、`enterprise_hub_install` 和 signature 诊断；
   - DataSrv mock 不应被调用，证明失败发生在 app_installations 登记之前；
   - 本地 `app_install_records.json` 不应落入失败安装记录；
   - 信任库只保留 App package 公钥指纹，不能把未信任 workflow Skill 的公钥指纹写入 `TrustedSkillPackageKeyFingerprints`。
3. 回归原有成功链路：
   - 同一套共享审批 fixture 的成功安装、依赖安装、DataSrv 登记、workflow 运行和审批中心回读仍然通过；
   - 说明新增失败路径没有削弱正常的企业审批型 App 安装链路。

当前依赖包信任边界变为：

```text
trusted MaClaw App package signature
  -> seeds trusted package key fingerprint
  -> installs trusted app Skill dependency
  -> downloads workflow Skill dependency
  -> verifies workflow Skill package signature against trusted fingerprints
  -> blocks untrusted workflow Skill before DataSrv registration
  -> keeps trust store and install audit clean
```

验证命令：
```powershell
cd D:\workprj\aicoder
go test ./gui -run TestInstallSharedPublishedApprovalFixtureBlocksUntrustedDependencySkillSignature -count=1
go test ./gui -run "TestInstallSharedPublishedApprovalFixtureFromHubInstallsDependenciesAndRegistersDataSrv|TestInstallSharedPublishedApprovalFixtureBlocksUntrustedDependencySkillSignature" -count=1
```

验证结果：
```text
GUI untrusted dependency Skill signature failure path: ok
GUI shared approval fixture success/failure dependency signature suite: ok
```

剩余缺口：依赖 Skill 包签名不可信的阻断链路已经有端到端验收；下一步继续补 Hub 包签名失败、DataSrv 登记部分失败，以及审批 workflow 返回拒绝/需关注/补充材料时的 DataSrv 与 GUI 回读一致性。

### 推进记录：Hub MaClaw App 包签名无效时阻断安装（2026-07-01）

本轮继续把能力市场安装入口的信任链前移到 Hub package 本体。依赖 Skill 的签名校验已经能阻断不可信 workflow Skill；但在此之前，MaClaw App 自身的 Hub 下载包也必须先通过签名验证。若 App 包签名无效，GUI 不能继续解析为可安装 App、不能安装依赖、不能写 DataSrv，也不能把包公钥写入本地 Skill 包信任库。

本轮完成：
1. 新增 GUI 后端验收 `TestInstallSharedPublishedApprovalFixtureBlocksInvalidHubPackageSignature`：
   - 使用共享企业审批型 App fixture，保留完整 published governance、resolved dependencies、dependency verification 和 workflow contract；
   - 先用 Hub package 私钥正常签名，再篡改 `package_signature.signature_base64`；
   - 从真实安装入口 `InstallSelectedMaclawAppPackageFromHub` 发起安装，确保失败发生在 `DownloadMaclawAppPackageFromHub -> verifyMaclawAppHubPackageSignature` 阶段。
2. 明确失败边界：
   - 错误信息必须暴露 `package signature` 与 `verification failed`；
   - 不应请求任何依赖 Skill capability 或下载端点；
   - 不应调用 DataSrv；
   - 不应写入本地安装审计；
   - 不应把无效 Hub package 的公钥指纹写入 `TrustedSkillPackageKeyFingerprints`。
3. 与相邻成功/失败链路一起回归：
   - 正常共享审批 fixture 仍能完成 Hub 安装、依赖安装、DataSrv 登记、workflow 运行和审批中心回读；
   - App 包签名失败会在依赖安装前阻断；
   - 依赖 Skill 包签名不可信会在 DataSrv 登记前阻断。

当前 Hub 安装信任链变为：

```text
Download Hub MaClaw App package
  -> Validate GUI install package contract
  -> Verify Hub package signature
      -> reject invalid app package signature before dependency install
      -> seed trusted package key only after verification succeeds
  -> Install dependency Skills with trusted fingerprints
  -> Record install / DataSrv app_installations
```

验证命令：
```powershell
cd D:\workprj\aicoder
go test ./gui -run "TestInstallSharedPublishedApprovalFixtureFromHubInstallsDependenciesAndRegistersDataSrv|TestInstallSharedPublishedApprovalFixtureBlocksInvalidHubPackageSignature|TestInstallSharedPublishedApprovalFixtureBlocksUntrustedDependencySkillSignature" -count=1
```

验证结果：
```text
GUI shared approval fixture Hub package signature success/failure and dependency signature suite: ok
```

剩余缺口：Hub App 包签名失败、依赖 Skill 包签名不可信两类信任链失败路径已经补齐；下一步继续补 DataSrv 登记部分失败，以及审批 workflow 返回拒绝/需关注/补充材料时的 DataSrv 与 GUI 回读一致性。

### 推进记录：DataSrv app_installations 登记失败时阻断企业 App 安装（2026-07-01）

本轮继续补齐 Hub 安装信任链之后的企业数据登记边界。对企业审批型/企业普通型 App 来说，安装不是“本地面板出现入口”就算完成；只要 App 带 DataSrv role bindings，它就必须成功登记到 DataSrv `app_installations`，否则后续审批实例管理、治理证据恢复、二次发布和审计都会断链。因此 DataSrv 登记失败不能再作为 install audit 中的一个 failed evidence 被宽容保存，而应阻断企业 App 安装。

本轮完成：
1. 调整 `RecordMaclawAppInstall`：
   - `registerMaclawAppInstallationsToDataSrv` 之后、写本地 `app_install_records.json` 之前新增阻断检查；
   - 企业审批型/企业普通型 App 只要存在 DataSrv role bindings，且登记结果不是 `ready`，就返回错误；
   - 错误信息保留 `status=failed/partial/skipped`、顶层 reason、失败 app_id 和 DataSrv HTTP 错误原因；
   - 无 DataSrv role bindings 的 App 仍可保持 skipped，不影响工具型/纯本地入口。
2. 新增后端安装记录层验收 `TestRecordMaclawAppInstallBlocksDataSrvRegistrationFailure`：
   - 企业普通 App 调用 `RecordMaclawAppInstall`；
   - DataSrv `PUT /api/v1/data/app-installations/{appID}` 返回 503；
   - 安装必须失败，错误包含 `datasrv-offline-app` 和 `datasrv offline`；
   - 本地安装审计不能落盘。
3. 新增 Hub 全链路验收 `TestInstallSharedPublishedApprovalFixtureBlocksDataSrvRegistrationFailure`：
   - Hub MaClaw App 包签名可信；
   - app Skill 和 workflow Skill 依赖包签名可信并成功下载；
   - 进入 DataSrv app_installations 登记时返回 500；
   - 安装失败，且不写本地 install audit；
   - 验证失败阶段发生在依赖安装之后、审批 workflow 运行之前。
4. 更新旧语义测试：
   - 原先“DataSrv 登记失败也保留安装审计”的测试语义已不符合企业 App 完整安装原则；
   - 已改为“DataSrv 登记失败必须阻断安装并保留可诊断错误”。

当前安装阶段边界变为：

```text
Hub package signature verified
  -> dependency Skills installed and verified
  -> RecordMaclawAppInstall
      -> DataSrv app_installations registration
      -> if enterprise app has DataSrv bindings and registration != ready: block install
      -> only after DataSrv ready: write local app_install_records.json
  -> app can appear in local panel and approval/runtime recovery
```

验证命令：
```powershell
cd D:\workprj\aicoder
go test ./gui -run "TestRecordMaclawAppInstallBlocksDataSrvRegistrationFailure|TestInstallSharedPublishedApprovalFixtureBlocksDataSrvRegistrationFailure|TestInstallSharedPublishedApprovalFixtureFromHubInstallsDependenciesAndRegistersDataSrv" -count=1
```

验证结果：
```text
GUI DataSrv registration failure blocking and shared approval success path: ok
```

剩余缺口：安装阶段的三类关键失败边界（Hub App 包签名失败、依赖 Skill 包签名不可信、DataSrv app_installations 登记失败）已经补齐；下一步继续推进审批 workflow 返回拒绝/需关注/补充材料时的 DataSrv 与 GUI 回读一致性。

### 推进记录：审批 workflow 非通过结果与补充材料回读一致性（2026-07-01）

本轮继续推进企业审批型应用运行态结果闭环。此前安装阶段的关键失败边界已经收口；运行阶段还必须证明 workflow Skill 返回的非通过结果不会只停留在 run evidence 中，而是能同步到 DataSrv，并被单 App 审批工作台和全局审批中心按正确 lane 回读。

本轮完成：
1. 复核并回归现有审批结果状态链路：
   - `approved` 进入 `handled`，同步 DataSrv review，保留 result payload、outputs、artifacts；
   - `rejected` 进入 `handled`，同步 DataSrv review，单 App/全局 handled 回读一致；
   - `attention` 进入 `attention`，只做 view-only 业务记录同步，不 review DataSrv approval，单 App/全局 attention 回读一致；
   - `timeout` / `cancelled` 进入 `handled`，同步 DataSrv review，保留 workflow node path 和输出块；
   - workflow Skill 执行失败进入 `failed`，同步失败结果包。
2. 补强 `requires_input` 回读验收：
   - workflow Skill 返回 `requires_input` 时，实例 lane 归为 `my_requests`；
   - DataSrv 同步走 `update_record_approval_progress`，不走 review；
   - 单 App `my_requests` 能看到待补充实例；
   - 新增全局 `ListMaclawAppApprovalInstancesAll("my_requests")` 断言，确认全局审批中心也能看到同一实例；
   - 断言 app-scoped 与 global 两个查询分别使用 `app_id=...&lane=my_requests` 和 `lane=my_requests`。
3. 回归补充材料后继续流程：
   - `ContinueFromID + ApprovalID` 复用原审批实例，不新建 DataSrv approval；
   - supplemental input 写入 progress；
   - workflow 最终 approved 后进入 handled；
   - 单 App/全局 handled 回读一致，my_requests 清空。

当前审批结果回读链路变为：

```text
workflow Skill result
  -> approval_instance status
      -> approved/rejected/timeout/cancelled/failed => handled
      -> attention => attention view-only
      -> requires_input => my_requests progress
  -> SyncMaclawAppApprovalInstanceToDataSrv
  -> DataSrv approvals list
  -> single App approval workspace
  -> global approval center
```

验证命令：
```powershell
cd D:\workprj\aicoder
go test ./gui -run "TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult|TestStartMaclawAppApprovalWorkflowRunsAttentionViewOnlyWorkflowResult|TestStartMaclawAppApprovalWorkflowRunsRejectedWorkflowResult|TestStartMaclawAppApprovalWorkflowRunsTimeoutWorkflowResult|TestStartMaclawAppApprovalWorkflowRunsCancelledWorkflowResult|TestStartMaclawAppApprovalWorkflowRunsRequiresInputWorkflowResult|TestStartMaclawAppApprovalWorkflowContinuesRequiresInputWithSupplement" -count=1
```

验证结果：
```text
GUI approval workflow result states and supplement readback suite: ok
```

剩余缺口：审批 workflow 的主要结果状态（通过、拒绝、需关注、超时、取消、失败、待补充、补充后继续）已经有 DataSrv 与 GUI 回读一致性验收；下一步应继续把这些状态在前端审批工作台的交互显示、补充材料面板、运行历史 evidence 与 Hub/DataSrv 安装恢复后的二次运行串成更少 mock 的 UI 回归。

### 推进记录：前端审批补充材料面板交互门禁（2026-07-01）

本轮继续把后端 `requires_input` 状态闭环推进到 GUI 运行态工作台。后端已经证明 workflow Skill 返回 `requires_input` 时会进入 `my_requests`，并能在单 App/全局审批中心回读；前端还需要证明用户在传统软件式审批详情区里不会提交空补充材料，只有填写说明或附件引用后才允许继续 workflow。

本轮完成：
1. 补强前端运行态用例 `continues a requires-input approval instance with supplemental input from the runtime workbench`：
   - 打开企业审批型 App 的运行态工作台；
   - DataSrv/后端 mock 返回 `requires_input` 审批实例；
   - 断言补充材料面板展示 `Missing materials` 和缺失字段 `invoice_attachment`；
   - 新增断言：补充说明和附件引用都为空时，`Continue with supplement` 按钮禁用；
   - 填写补充说明后按钮启用；
   - 提交后继续断言 `StartMaclawAppApprovalWorkflow` payload 保留 `approval_id`、`continue_from_instance_id`、`form_data`、`business_payload`、`result_payload.supplemental_input` 和 `workflow_run_args.supplemental_input`。
2. 保持 UI 形态不变：
   - 继续使用现有 `ApprovalSupplementPanel` 内联面板；
   - 不引入 modal，不把审批详情退化成单纯表单；
   - 保持运行态工作台的补充材料作为审批详情的一部分。

当前前端补充材料交互链路变为：

```text
requires_input approval instance
  -> ApprovalDetail
  -> ApprovalSupplementPanel
      -> empty note/reference disables continue
      -> note or artifact reference enables continue
  -> StartMaclawAppApprovalWorkflow(ContinueFromID + ApprovalID)
  -> supplemental_input enters workflow run args and result payload
```

验证命令：
```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "continues a requires-input approval instance with supplemental input from the runtime workbench"
```

验证结果：
```text
AppsPage requires-input supplemental interaction targeted test: ok
```

剩余缺口：前端补充材料面板已经具备空提交门禁与继续流程 payload 验收；下一步应继续压缩前端 App 运行态/审批中心/安装恢复之间的 mock 距离，并补运行历史 evidence 对 `requires_input -> supplemented -> approved/rejected` 链路的展示一致性。

### 推进记录：补充材料继续流程写入运行历史证据（2026-07-01）

本轮继续补齐 `requires_input -> supplemented -> final result` 的前端证据链。前面已经证明补充材料面板能阻止空提交，并且继续流程 payload 会携带 `supplemental_input`；但成功继续后，运行历史也必须保存同一份补充材料证据和最终审批实例，否则 App Studio 复测、二次发布、安装恢复后的证据面板会只能看到最终 approved/rejected，却丢失“用户补了什么”的过程事实。

本轮完成：
1. 调整前端 `continueApprovalInstanceWithSupplement` 成功分支：
   - 继续 workflow 成功返回 `startedInstance` 后，写入一条 `AppRunHistoryEntry`；
   - `runID` 使用 workflow decision id / approval id / instance id；
   - `outputMode` 固定为 `approval`；
   - `approvalInstance` 来自后端返回的最终实例；
   - `progressInstances` 保存 workflow run 中的中间进度实例；
   - 顶层 `resultPayload` 合并补充输入和最终 workflow 结果，避免最终 approved/rejected payload 覆盖 `supplemental_input`；
   - `approvalInstance.resultPayload` 同步保留 `supplemental_input`。
2. 扩展前端运行态测试：
   - 在补充材料提交成功后读取 `maclaw:apps-run-history:v1`；
   - 断言最新历史记录为 `done / approval`；
   - 断言 `approvalInstance` 保留 `instanceId`、`approvalID`、最终 `approved` 状态和 running progress instance；
   - 断言 `resultPayload.supplemental_input.form_data.supplement_note` 保留用户填写的补充说明。

当前补充材料证据链变为：

```text
requires_input approval instance
  -> user submits supplement note/reference
  -> StartMaclawAppApprovalWorkflow continues existing ApprovalID
  -> workflow_run.progress_instances + final instance
  -> runtime approval workspace
  -> local run history evidence
      -> supplemental_input
      -> final approvalInstance
      -> progressInstances
```

验证命令：
```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "continues a requires-input approval instance with supplemental input from the runtime workbench"

cd D:\workprj\aicoder
go test ./gui -run "TestStartMaclawAppApprovalWorkflowRunsRequiresInputWorkflowResult|TestStartMaclawAppApprovalWorkflowContinuesRequiresInputWithSupplement" -count=1
```

验证结果：
```text
AppsPage requires-input supplemental run history evidence targeted test: ok
GUI requires-input backend workflow continuation suite: ok
```

剩余缺口：补充材料继续流程已经进入运行历史 evidence；下一步应继续把运行历史中的补充链路接入 App Studio 发布门禁/测试证据选择，以及 DataSrv install evidence 恢复后的二次运行与二次发布回归。

### 推进记录：补充材料审批链路进入 App Studio 发布证据（2026-07-01）

本轮把上一步的运行历史 evidence 继续接到 App Studio 发布门禁。MaClaw App 的制作链路不能只证明“运行态能继续审批”，还必须证明“设计后的 App 在上传 Hub 能力市场前，测试证据包保留同一条审批事实链”。尤其是 `requires_input -> supplement -> approved/rejected` 场景，发布包里必须同时保留补充材料、最终审批实例、进度节点、结果契约覆盖和 Skill 依赖验证，否则安装方无法判断这个企业审批型 App 是否真的按审批 workflow 运行过。

本轮完成：
1. 调整前端运行态依赖检查条件：
   - `appNeedsAutomaticRuntimeDependencyCheck` 将 `studioOrigin === 'app_studio'` 的本地 MaClaw App 纳入自动依赖检查；
   - App Studio 设计出的本地审批 App 在运行测试前会触发 `PlanMaclawAppInstall`；
   - 运行历史写入时可携带 dependencyVerification，避免发布门禁卡在“缺少依赖验证证据”。
2. 扩展前端补充材料测试为“运行 + 发布”闭环：
   - 将 `报销申请` 场景切换为 App Studio 设计出的本地审批 App；
   - 等待运行前 Skill 依赖计划检查完成；
   - 继续提交补充材料并断言运行历史保留 `supplemental_input`；
   - 打开 App Studio 的 `Review / publish`；
   - 断言发布卡片达到 `Ready to submit`；
   - 点击 `Submit for review` 后检查上传包 `governance.testEvidence`。
3. 新增发布包证据断言：
   - `testEvidence.runId` 来源于继续审批的 approval/workflow id；
   - `definitionHash` 匹配当前 App 定义；
   - 顶层 `resultPayload.supplemental_input.form_data` 保留补充说明和附件引用；
   - `approvalInstance` 保留 `instanceId`、`approvalID`、最终 `approved`、`workflowSkillId`、`workflowVersion`、`approvalEvent`、`recordID`；
   - `approvalInstance.resultPayload.supplemental_input` 与顶层结果一致；
   - `progressInstances` 保留 running 审批节点；
   - `resultCoverage` 对 `approval_result` 通过，`missingTypes` 为空。

当前制作、测试、上传证据链变为：

```text
App Studio designed approval app
  -> automatic Skill dependency plan check
  -> runtime approval workspace
  -> requires_input instance
  -> user submits supplemental input
  -> StartMaclawAppApprovalWorkflow continues existing ApprovalID
  -> local run history evidence
      -> dependencyVerification
      -> supplemental_input
      -> final approvalInstance
      -> progressInstances
  -> App Studio Review / publish
  -> governance.testEvidence in Hub upload package
```

验证命令：
```powershell
cd D:\workprj\aicoder\gui\frontend
npm.cmd test -- src/components/pages/__tests__/AppsPage.test.tsx -t "continues a requires-input approval instance with supplemental input from the runtime workbench"
```

验证结果：
```text
AppsPage requires-input supplemental publish evidence targeted test: ok
```

剩余缺口：App Studio 本地设计 App 的补充审批 evidence 已可进入发布包；下一步应继续补 DataSrv install evidence 恢复后的二次发布/二次运行回归，并检查 Hub/DataSrv 对 `supplemental_input`、`progressInstances`、dependencyVerification 的持久化和 OpenAPI/schema 暴露是否完全一致。
