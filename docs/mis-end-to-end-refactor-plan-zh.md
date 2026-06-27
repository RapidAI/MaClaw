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
