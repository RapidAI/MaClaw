# 新任务引导页整合设计（轻量版）

整合目标：在"今天要完成什么？"页（`AssistantWelcomeView`）自然引入**任务类型、AI 专家、工作流类型、工作空间**四项配置，**默认不指定**（专家=通用专家、工作流=无），不打扰快速输入的主路径。主界面顶部「新建任务」按钮直达引导页，用户输入第一条命令发送时才创建任务。

## 1. 设计原则

- **输入框永远是主角**：四项配置是输入框底部工具行（📎/🎤 同行右侧）的一枚安静配置条，默认折叠为一枚"⚙ 默认"chip，不改变现有页面布局和使用习惯。
- **默认即智能**：不指定专家 = 通用专家（系统通用能力处理）；不指定工作流 = **无（不执行工作流）**；「自动判断」为显式可选项（发送后由系统语义拦截匹配工作流）；不设类型 = 会话；不设目录 = 默认工作目录。零配置回车 = 今天的旧行为，完全兼容。
- **选择即显式**：用户一旦手动指定某项，该项生效并取代自动行为（显式指定工作流时创建后直接启动该工作流，绕过语义拦截）。
- **不建向导**：不用分步流程，全部用行内 chip + 弹层选择器完成，弹层关闭即回到输入框，Enter 随时可发。
- **一个入口、一份配置**：引导页产出一个 `TaskDraft`（见 §6），所有创建路径（回车、快捷卡、模板卡）都收敛到它。顶部「新建任务」按钮 = 打开全新引导页（`EVENT_OPEN_NEW_TASK_WIZARD` + `newTaskWizard` 页签标记），首条消息发送即建任务（零配置 = 普通会话任务）；普通空会话页的零配置发送仍不建任务（现状兼容）。

## 2. 页面结构

```
              今天要完成什么？

  ┌───────────────────────────────────────────┐
  │ 设计一页管理看板，突出核心指标…（输入框）     │
  │ 📎 🎤 [⚙ 默认 ▾]            Enter 发送 · ↵ │
  └───────────────────────────────────────────┘
  展开后（点击 ⚙ 默认 或任何已选 chip；配置条与 📎/🎤 同一行，窄窗口自动换行、不挤压发送按钮）：
  ┌───────────────────────────────────────────┐
  │ 📎 🎤 类型[会话▾] 专家[通用专家▾] 工作流[无▾] │
  │ 工作空间[本地▾]              Enter 发送 · ↵ │
  └───────────────────────────────────────────┘

  快捷任务  [整理会议纪要] [回复客户邮件] …
  分类标签  经营分析 运营排障 …
  模板卡片  …
```

- **工作空间（Workspace）= 任务的运行位置**，三种类型：本地文件夹 / 云端工作区 / 远程服务器目录。引导页配置条与左侧任务列表使用同一概念、同一枚类型徽标（📁 本地 / ☁️ 云端 / 🖧 远程）。
- **TaskConfigBar**：输入框底部工具行（📎/🎤 之后、Enter 提示与发送按钮之前）的一行安静配置条，默认折叠为一枚 `⚙ 默认 ▾`（hover 提示"默认 — 会话 · 通用专家 · 无工作流 · 本地"）；有任何手动项时展开为四个 chip，未指定的维度仍显示默认值（类型=会话、专家=通用专家、工作流=无、工作空间=默认）。行内 flex 布局并允许换行，窄窗口下 chip 折到下一行，Enter 提示与发送按钮始终右置可点。
- 每个 chip 点击打开**向下弹层**（复用 portal + `useSafeBackdropDismiss`，z-index 约定同 `WelcomePromptParamDialog`），弹层内单选列表，选中即回填、自动关闭、焦点回到输入框。支持 Esc 关闭、Enter 确认。
- 模板卡片/快捷任务点击时，若该卡带来源信息（专家卡、工作流模板卡），自动点亮对应 chip——用户看得见"这张卡帮我配了什么"，可再改。

## 3. 四个选择器

| Chip | 默认 | 选项来源 | 选中后的行为 |
|---|---|---|---|
| 类型 | 会话 | 会话 / 编程 两类（云端工作区与远程归入"工作空间位置"，不再是独立类型） | 决定工作空间可选位置：会话=本地/云端/远程，编程=本地/远程（云端置灰） |
| 专家 | 通用专家 | 本地专家 `ListExperts()` + 已安装行业专家 `ListManagedIndustryExperts()`（仅取 `installed` 且带本地专家 id 的条目；目录占位/安装中项不可选）+ 专家市场入口；顶部固定「🤖 通用专家」（默认，不指定领域专家） | 指定具体专家后，工作流 chip 锁定为「由专家决定」，工作流弹层置灰（互斥，见 §7） |
| 工作流 | 无（不执行工作流） | `ListWorkflowTemplateSummaries()`（新增绑定，源自 `corelib/workflow/v2` 模板注册表，约 35 个）；顶部固定「🚫 无（默认，不执行工作流）」与「✨ 自动判断」 | 「自动判断」= 发送后语义拦截匹配工作流；指定模板后直接启动并绕过语义拦截（v1 参数槽弹窗未接入，缺槽位由首条消息时的 AG UI 表单补录，见 §6.4）；工作流选择 ≠ 无（含「自动判断」）即锁定专家，专家强制回到「通用专家」，专家弹层非通用项置灰（互斥，见 §7） |
| 工作空间 | 本地 | 位置三选一：本地文件夹 / 云端工作区 / 远程服务器目录；弹层为位置菜单 + 子面板（本地目录列表 / 云端工作区列表 / SSH 表单），主面板行内显示当前各位置的选择摘要 | 「编程」类型下云端位置置灰；选远程时弹出 SSH 表单（主机/端口/用户名/密码/工作目录 + 测试连接，复用 `TestRemoteSSHConnection`） |

弹层统一布局：顶部搜索框（专家/工作流选项多）+ 单选列表（图标 + 名称 + 一行描述）。默认项固定置顶（专家弹层置顶「通用专家」，工作流弹层置顶「无」与「自动判断」），选中置顶默认项即恢复自动——没有单独的"恢复自动"底部项。

## 4. 后端改动（最小收口）

新增两个 Wails 绑定，旧绑定全部保留：

```go
// guiapp/app_task_create.go
type TaskCreateOptions struct {
    Name               string            // 任务描述
    Mode               string            // chat | coding_dev | remote_coding_dev | cloud（引导页"类型×位置"映射：会话×本地→chat、会话×云端→cloud、编程×本地→coding_dev、编程×远程→remote_coding_dev）
    WorkingDir         string
    Remote             *RemoteTarget     // 复用现有结构：host/port/user/password/workDir/safety
    CloudWorkspaceID   string
    ExpertID           string            // 空 = 通用专家（默认，不指定领域专家）
    WorkflowTemplateID string            // 空 = 无（不执行工作流）或「自动判断」（前端把 'auto' 归约为空后传入，经消息层语义拦截）；模板 id = 直传。后端实际只会收到 空 | 模板 id 两种值
    Params             map[string]string // 工作流模板参数槽
}

func (a *App) CreateTaskUnified(opts TaskCreateOptions) (UnifiedTaskCreateResult, error)
func (a *App) ListWorkflowTemplateSummaries() []WorkflowTemplateSummary
// WorkflowTemplateSummary: {ID, Title, Category, PhaseCount, RequiresWorkingDir, HasParamSlots}
// UnifiedTaskCreateResult: {ProjectPath, Warning} —— Warning 非空 = 任务已建、可打开，
//   但次级步骤失败（工作流启动 / 远程环境武装）且已降级。warning 必须走结果字段而不是
//   error 返回：Wails JS 绑定在 error 非 nil 时会 reject 整个 promise 并丢弃 path。
```

`CreateTaskUnified` 分发（内部复用现有实现）：
- `ExpertID != ""` → `CreateExpertTask`，name 作为首条消息；
- `WorkflowTemplateID != ""` → 先校验模板（未知模板不留孤儿记录），建任务后直接经 V2 StateMachine 启动（`startUnifiedWorkflow`，**跳过语义拦截**）；启动失败时记录已存在，返回 `{path, Warning}` 降级为普通会话任务，GUI 照常打开页签；
- `Mode = remote_coding_dev` → 建远程任务记录后用一次性密码武装 SSH 环境；武装失败同样返回 `{path, Warning}`，GUI 打开页签并置 `remoteNeedsReconnect`，首条消息延迟到重连成功再发；
- 否则按 `Mode` 走现有 `CreateTask / CreateTaskWithMode / CreateTaskWithCloudWorkspace`。

全零值 = 旧 `CreateTask` 行为，回归无风险。

> **空值语义变更（工作流默认 = 无）**：`WorkflowTemplateID` 空以前表示"自动语义拦截"，现在表示**无（不执行工作流）**。创建层本身对两种空值都不启动工作流，差异在**消息层**：「无」的首条消息带 `no_workflow_interception` 透传键（`agent.UserMessage.NoWorkflowInterception`，`routeWithWorkflowV2` 入口直接放行），显式跳过工作流语义拦截/启动；`'auto'` 或缺省（零配置旧路径）则保持现状语义拦截行为。该校验/分发逻辑不需要改动（空 = 不启工作流两种语义一致），仅注释与消息层契约变化。

## 5. 左侧任务列表同步改造

概念统一：见 §3"工作空间"定义。`SidebarTaskManagement.tsx` 任务列表中"目录"字段改名为"工作空间"：

- **每行显示**：类型徽标（📁 本地 / ☁️ 云端 / 🖧 远程）+ 对应值——本地=路径，云端=工作区名，远程=`host:workDir`。
- **类型推导**：remote 任务读远程 meta（`UpdateRemoteCodingTaskMeta` 已存 host/workDir）；cloud 任务读 `cloudWorkspaceId`（tags 已带）；其余为本地。
- **筛选**：列表筛选行增加"工作空间"筛选——全部 / 本地 / 云端 / 远程。
- **联动**：点击任务的"工作空间"徽标可直接修改（编辑入口复用现有编辑远程 meta / 任务信息对话框）；与引导页配置条共用 `WorkspacePickerPopover` 组件与同一枚徽标样式。

## 6. 前端改动

### 6.1 `taskDraft.ts`（新）

welcome 页所有点击路径共享一份 `TaskDraft`：

```
{
  name: string;                              // 输入框原文（发送时取 trim 后文本，name 仅作初值）
  taskType: 'chat' | 'coding';               // 任务类型（§7.2 类型×位置联动）
  expertId: string | null;                   // null = 通用专家（默认）
  expertName: string | null;                 // chip 显示回退
  workflowTemplateId: string | null;         // null = 无；'auto' = 自动判断；其余 = 模板 id
  workspace: { kind: 'local' | 'cloud' | 'remote',
               localPath?: string,           // 空/未设置 = 默认工作目录
               cloudWorkspaceId?: string, cloudName?: string,
               remote?: { host, port, user, password, workDir } };
  workflowParams: Record<string, string>;    // 工作流模板参数槽
}
```

互斥 / 联动（§7）全部是这里的纯函数：`withExpert` 指定专家即把工作流打回「无」；`withWorkflow` 选任何 ≠ 无 的工作流（含「自动判断」）即把专家打回「通用专家」；`withTaskType` 编程类型下云端位置退回本地。

### 6.2 `TaskConfigBar.tsx`（新）

四枚 chip + 弹层容器，纯样式组件，Theme token + `.mc-*` 约定。

### 6.3 弹层（新）

`ExpertPickerPopover / WorkflowPickerPopover / WorkspacePickerPopover（本地/云端/远程三态）/ RemoteServerPopover（SSH 表单）` 行内弹层；工作目录弹层逻辑从 `SidebarTaskManagement.tsx` 抽取复用。SSH 表单复用 `TestRemoteSSHConnection`，测试连接失败不阻断「确定」（发送链路的武装失败走 `{path, Warning}` 降级契约）。

### 6.4 接线

`AssistantWelcomeView` 回车/快捷卡 → `runTaskConfigSend`（`task-config/taskConfigSend.ts` 纯逻辑）→ `CreateTaskUnified`；成功后非专家分支派发 `EVENT_OPEN_TASK_LAUNCH`（App 监听并打开任务页签 autoSend 首条消息），专家分支走 `openExpertConversation`（`pendingExpertOpen.initialMessage` → 专家页签打开后经专家会话发送，页签已存在同样补发）。全默认 draft 绝不进拦截（零配置回车 = 旧行为）；例外：向导页签（`newTaskWizard` 标记，由 `EVENT_OPEN_NEW_TASK_WIZARD` 设置）首条发送强制 `force` 建任务。`EVENT_OPEN_CREATE_CODING_TASK` 保持原语义不变、未扩展为完整 draft。

> v1 已知裁剪：工作流参数槽弹窗未接入——选中带参数槽的模板直接带空参数创建，缺槽位由后端 `SubmitForm` 失败兜底，首个用户消息到达时展示 AG UI 表单补录（`startUnifiedWorkflow` 注释）。`RequiresWorkingDir` 目前只在列表项上展示「· 需要工作目录」提示，不强制阻断发送。

### 6.5 分类标签

分类标签数据源后续迭代对齐模板目录（本版保留硬编码，不改用户体验）。

## 7. 约束规则

1. **专家 × 工作流互斥**：专家默认「通用专家」（= 未指定领域专家）；指定具体专家后工作流锁定为「由专家决定」，弹层内置灰不可选；反之工作流选择 ≠ 无（**含「自动判断」——'auto' 也算指定了工作流行为**）即锁定专家，专家强制回到「通用专家」，专家弹层内非通用项置灰。`SemanticOnly` 模板不出现在显式列表。
2. **类型 × 工作空间**：任务类型收敛为「会话 / 编程」两类；工作空间是位置选择（本地文件夹 / 云端工作区 / 远程服务器目录）。会话：三者皆可（对话任务可指定云端工作区或本地文件夹作为运行位置）；编程：本地 / 远程（云端置灰），本地文件夹必填、远程弹出 SSH 表单（host/port/user/password/workDir + 测试连接，复用 `TestRemoteSSHConnection`）。会话×远程走远程 SSH 会话/诊断链路。
3. **工作流模板元数据**：注册表补 `RequiresWorkingDir` 字段（缺省 true）。v1 仅用于列表项提示「· 需要工作目录」，不强制补目录（见 §6.4 裁剪说明）；后续版本再决定是否升级为发送前强制校验。
4. **"自动"语义**：任何 chip 显示"自动"时，该维度行为与今天完全一致——这是兼容性的兜底保证。
5. **专家 × 工作空间**：专家任务不携带工作空间（`CreateExpertTask` 签名冻结、无工作空间参数），因此指定专家时工作空间弹层中云端/远程两行置灰（hover 说明「专家任务不携带工作空间 / Expert tasks do not carry a workspace」），工作空间 chip 本身保持可点（可查看/改选本地目录），已选的云端/远程自动归约为本地默认；切回通用专家恢复三种位置完全可选。与专家×工作流互斥对称，保证"不可能的组合不可选"，而不是选了被静默忽略。

## 8. 迁移步骤

1. 后端：`TaskCreateOptions` / `CreateTaskUnified` / `ListWorkflowTemplateSummaries` + 模板 `RequiresWorkingDir` 元数据。
2. 前端：`taskDraft` → `TaskConfigBar` → 三个 popover → 接线回车与卡片路径；左侧任务列表"目录"改"工作空间" + 类型徽标 + 筛选。
3. 验证：六条创建路径（chat/coding/remote/cloud/expert/workflow）+ 默认零配置路径（必须等价旧行为）。

## 9. 开放问题

- ~~专家任务当前不带工作目录（`CreateExpertTask` 无此参数），显式选专家时目录 chip 是否隐藏还是允许附加？~~ **已决（§7.5）**：chip 保留可点（本地目录仍可查看/改选），云端/远程置灰，已选云端/远程归约为本地默认。
- 工作流模板约 35 个，弹层是否需要"常用 Top 8 + 全部"两层？
