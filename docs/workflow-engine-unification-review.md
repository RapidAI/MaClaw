# 工作流流程引擎统一推进能力 Review 与改进方案

## 背景

本轮问题暴露的不是单个 `coding.task_breakdown` 配置错误，而是流程引擎的阶段语义没有被统一建模。

当前模板已经能声明 `NeedsConfirm`、`ToolPolicy`、`InputSchema`、`RequiresInput`、`CanSkip`、`DisableOrchestrator` 等字段，但这些字段既承担展示语义，又承担工具权限，又承担状态推进，又承担执行编排开关。结果是每次新增流程模板或新增阶段能力时，都容易在 GUI、TUI、agentservice、前端元数据、提示词、工具过滤里各补一刀。

正确方向：流程引擎统一拥有阶段契约与推进规则；各端只消费同一个派生结果。

## 当前逻辑梳理

核心代码入口：

| 模块 | 当前职责 |
| --- | --- |
| `corelib/workflow/types.go` | 定义工作流、阶段模板、工具策略、输入结构 |
| `corelib/workflow/registry.go` | 注册模板前执行阶段契约准入校验，拒绝冲突模板 |
| `corelib/workflow/templates.go` | 内置流程模板和阶段列表 |
| `corelib/workflow/engine.go` | 状态机：启动、输入、表单、审核、推进、保存阶段输出 |
| `corelib/workflow/phase_metadata.go` | 派生前端/TUI 阶段展示元数据 |
| `corelib/workflow/execution_phase.go` | 判断是否进入执行编排阶段 |
| `corelib/workflow/tool_policy.go` | 根据工具策略过滤 LLM 可见工具 |
| `corelib/workflow/ops_command_policy.go` | 对具体工具调用做参数级安全校验 |
| `corelib/workflow/prompt_builder.go` | 根据阶段模板生成阶段系统提示词 |
| `gui/im_agent_loop_tools.go` | GUI agent loop 暴露工具前应用工作流工具过滤 |
| `gui/workflow_orchestrator_activation.go` | GUI 进入执行编排时启动 orchestrator |
| `tui/workflow_integration.go` | TUI 接入同一套 engine 与 phase metadata |
| `corelib/agentservice/service.go` | 从消息/会话 metadata 传递 `tool_policy` |
| `corelib/agentservice/core_agent_executor.go` | agentservice 执行侧应用同一套工具策略校验 |

当前核心派生规则：

| 规则 | 位置 | 语义 |
| --- | --- | --- |
| `NeedsConfirm == true` | `engine.go` | 阶段输出保存后进入 review state，等待确认/修改/跳过 |
| `PhaseExpectsDocument()` | `phase_metadata.go` | 判断阶段是否产出预览文档 |
| `ToolPolicy` | `tool_policy.go`、`ops_command_policy.go` | 决定工具曝光和执行校验 |
| `ToolFilterFull && !NeedsConfirm && !DisableOrchestrator` | `execution_phase.go` | 判断是否为执行编排阶段 |
| `InputSchema != nil` | `engine.go` | 阶段先展示结构化表单，再运行 agent loop |
| `RequiresInput != nil` | `engine.go`、`prompt_builder.go` | 流程启动后先等待外部材料 |

这些规则本身合理，但目前还不是一个显式的“阶段契约”。调用方需要记住多个字段的组合含义。

## 根因模式

### 1. 工具策略曾被当成阶段类型

`doc_only` 原本表示“文档阶段，不允许执行/变更”。但 coding 的任务拆分阶段需要读取仓库、运行只读发现命令，否则无法拆出可执行任务。

之前为了防止主 agent 在技术设计后直接写代码，给规划阶段收工具，实际是 workaround：

- 优点：阻止主 agent 提前改文件。
- 问题：规划阶段也失去读仓库能力，导致“没工具，不能写文件/不能查项目”。
- 更深问题：把“阶段是否产出文档”和“阶段能否读仓库”混在一个 `doc_only` 策略里。

当前已引入 `planning` 策略，这是正确方向：

- 允许只读仓库检查：`bash`、`read_file`、`list_directory` 等。
- 禁止项目变更：`write_file`、`edit_file`、`edit_lines`、`task`、`delegate_task`、`ssh` 等。
- 系统保存阶段文档；agent 不负责用写文件工具落文档。

### 2. 文档落地和项目变更没有清晰分层

用户关心点是对的：规划阶段也要“生成文档落地成文件”。但这里有两种写：

| 写入类型 | 谁执行 | 是否应受规划阶段工具限制 |
| --- | --- | --- |
| 工作流阶段文档保存 | workflow engine / persistence / frontend doc event | 不应被 LLM 工具策略阻断 |
| 项目/代码/构建文件变更 | implementation/orchestrator/coding subagent | 必须等确认后进入执行阶段 |

也就是说，规划阶段不是“不能写任何东西”，而是“LLM 不能直接改项目文件”。阶段产物由流程系统持久化，这是引擎职责。

### 3. 状态推进散落在多个边界

当前 engine 是权威状态机，但 GUI/TUI/agentservice 仍各有工具曝光、执行校验、orchestrator 激活点。虽然它们已经逐步使用 core workflow 函数，但新增阶段能力时仍有漏接风险。

根因不是某个调用点写错，而是缺少一个所有边缘层都能直接消费的 `PhaseContract`。

## 目标模型

引入统一阶段契约：模板声明阶段意图，核心派生阶段能力，边缘层只消费能力。

这里要拆成两层，避免把“模板静态能力”和“当前是否被用户输入/审核阻塞”混在一个结构里：

| 层 | 输入 | 输出 | 用途 |
| --- | --- | --- | --- |
| 静态阶段契约 | `WorkflowTemplate` + `PhaseTemplate` | `PhaseContract` | UI 元数据、工具策略、执行编排能力 |
| 运行时阶段 gate | `WorkflowState` + 静态契约 | `PhaseRuntimeGate` | 当前是否等待输入、等待表单、等待审核、能否运行 agent loop |

建议新增：

```go
type MutationScope string

const (
    MutationScopeNone        MutationScope = "none"         // 不改项目，不生成外部 artifact
    MutationScopeWorkflowDoc MutationScope = "workflow_doc" // 仅由 workflow system 保存阶段文档
    MutationScopeArtifact    MutationScope = "artifact"     // 生成 PPT/DOCX/PDF 等交付物
    MutationScopeProject     MutationScope = "project"      // 修改代码、目录、构建文件、测试文件
    MutationScopeOps         MutationScope = "ops"          // 修改远端/运维对象，必须受风险策略控制
)

type PhaseContract struct {
    PhaseID                  string
    Kind                     PhaseKind
    ToolPolicy               ToolFilterPolicy
    ExpectsDocument          bool
    RequiresReview           bool
    RequiresStructuredForm   bool
    MutationScope            MutationScope
    AllowsRepoInspection     bool
    AllowsProjectMutation    bool
    AllowsDelegation         bool
    UsesSystemDocPersistence bool
    ActivatesOrchestrator    bool
}

type PhaseRuntimeGate struct {
    Contract                PhaseContract
    WaitingForWorkflowInput bool
    WaitingForPhaseForm     bool
    AwaitingReview          bool
    BlocksAgentLoop         bool
}
```

统一派生入口：

```go
func DerivePhaseContract(tmpl *WorkflowTemplate, phase PhaseTemplate) PhaseContract
func DerivePhaseRuntimeGate(tmpl *WorkflowTemplate, state *WorkflowState) PhaseRuntimeGate
```

调用方原则：

- `engine.go` 决定状态推进、review gate、表单 gate、输入 gate。
- `tool_policy.go` 和 `ops_command_policy.go` 决定工具曝光和具体调用校验。
- `phase_metadata.go` 从 `PhaseContract` 派生 UI 元数据。
- GUI/TUI/agentservice 不再重推阶段语义，只读取 contract。

关键不变量：

- `AllowsProjectMutation` 只能由 `MutationScopeProject` 推导，不能仅由 `ToolPolicy=full` 推导。
- `ActivatesOrchestrator` 只能由 `Kind=execution && MutationScopeProject && ToolPolicy=full && !RequiresReview && !DisableOrchestrator` 推导。
- `BlocksAgentLoop` 是运行时状态，只能出现在 `PhaseRuntimeGate`，不能写进静态模板契约。
- `UsesSystemDocPersistence` 表示 workflow system 保存阶段产物，不等于 LLM 可用写文件工具。

## 阶段类型建议

`PhaseKind` 不必替代现有字段，可以先作为派生字段落地，减少迁移成本。

| Kind | 典型阶段 | 能力 |
| --- | --- | --- |
| `intake` | 输入材料收集、结构化表单 | 等待用户输入，不运行执行工具 |
| `document_planning` | PRD、大纲、策略、研究计划 | 产出文档，需确认，不改项目 |
| `code_planning` | coding 任务拆分、测试计划拆分 | 可只读检查仓库，需确认，不改项目，不委托 subagent |
| `artifact_generation` | PPT/DOCX/PDF 等最终文件生成 | 生成交付物，可写 artifact，不改项目源码，不应被 coding orchestrator 接管 |
| `execution` | coding implementation、testing execution | 可改项目，可委托 coding subagent/orchestrator |
| `ops_risk_policy` | 运维风险策略审批 | 产出命令清单和审批策略 |
| `ops_execution` | 运维受控执行 | 只执行已审批命令，禁止高风险命令 |
| `review` | 代码审查、缺陷报告、总结 | 读为主，产出报告，通常需确认 |

## 工具策略矩阵

| Policy | 用途 | 允许 | 禁止 |
| --- | --- | --- | --- |
| `none` | 非工作流或无约束路径 | 保持原有工具路由 | 无工作流限制 |
| `doc_only` | 普通文档/分析阶段 | 读文件、列目录、记忆、web、阶段文档导出/发送 | shell、项目写入、委托、SSH |
| `planning` | 需要仓库上下文的可审阅规划阶段 | 只读 shell、读文件、列目录、记忆、web、发送阶段文档 | 写文件、编辑、创建目录、委托 subagent、SSH、破坏性命令 |
| `full` | 实现/生成阶段 | 完整工具集的阶段契约 | 由执行 profile、沙箱、运行期校验和 `MutationScope` 限制；coding implementation 主循环还叠加 CodingSubAgent handoff 策略 |
| `ops_controlled` | 运维受控执行 | bash/ssh/读文件/任务检查 | 未审批变更、高风险命令、泛化委托 |

关键原则：

- `NeedsConfirm` 表示 review gate，不表示无工具。
- `ToolPolicy` 表示 LLM 工具边界，不表示文档是否保存。
- `ExpectsDocument` 从 `NeedsConfirm` 优先派生；reviewable 阶段必有可预览阶段产物。
- `planning` 阶段可读仓库，但所有项目变更必须变成任务列表，等确认后进入 `execution`。
- `full` 不是“所有 full 阶段都能改项目”。是否能改项目由 `MutationScopeProject` 决定。
- `doc_only` 即使允许导出/发送阶段文档，也不代表允许生成任意交付物；正式 artifact 生成要进入 `MutationScopeArtifact`。

## 变更范围矩阵

单靠 `ToolPolicy` 仍太粗。`presentation_design` 的 PPT 生成、`business_plan` 的 DOCX 生成、`coding` 的源码修改都可能需要 `full`，但语义完全不同。

应把“能调用哪些工具”和“允许改什么状态”拆开：

| MutationScope | 允许变更 | 禁止变更 | 典型阶段 |
| --- | --- | --- | --- |
| `none` | 无外部写入 | 工作流文档外的所有变更 | 纯分析、纯 review |
| `workflow_doc` | engine 保存阶段文档、doc preview、gate result | LLM 直接写项目文件 | 需求、设计、任务拆分 |
| `artifact` | 生成用户交付物，如 PPT/DOCX/PDF/报告附件 | 源码、构建文件、项目目录结构 | PPT 生成、商业计划书生成 |
| `project` | 修改 workspace 项目文件、测试、构建配置 | 未经确认的规划期变更 | coding implementation、testing execution |
| `ops` | 已审批远端/运维对象变更 | 未审批命令、高风险命令 | ops controlled execution |

这样可以解决一个隐性问题：未来如果某个 artifact generation 阶段用了 `ToolPolicy=full`，不会被误判成 coding orchestrator，也不会默认拥有项目修改语义。

## 兼容派生规则

迁移期不能只看 `ToolPolicy`。尤其 `ToolPolicy=full` 同时覆盖“改源码”和“生成交付物”，必须结合 workflow type、phase ID、`NeedsConfirm`、`DisableOrchestrator` 派生。

建议兼容派生顺序：

1. 如果模板显式声明 `Kind` / `MutationScope`，先使用显式声明，再做冲突校验。
2. 如果 `ToolPolicy=planning && NeedsConfirm=true`，派生为 `Kind=code_planning`、`MutationScope=workflow_doc`。
3. 如果 `ToolPolicy=ops_controlled`，派生为 `Kind=ops_execution`、`MutationScope=ops`、`ActivatesOrchestrator=false`。
4. 如果 `ToolPolicy=full && workflow=coding && phase=implementation`，派生为 `Kind=execution`、`MutationScope=project`。
5. 如果 `ToolPolicy=full && workflow=testing && phase` 表示测试执行，派生为 `Kind=execution`、`MutationScope=project`。
6. 如果 `ToolPolicy=full && workflow` 属于 PPT/DOCX/PDF/报告生成类，派生为 `Kind=artifact_generation`、`MutationScope=artifact`。
7. 如果 `ToolPolicy=full && DisableOrchestrator=true`，默认不激活 coding orchestrator，并要求模板后续显式声明 `Kind`。
8. 如果 `NeedsConfirm=true` 且不满足以上规则，派生为 `Kind=document_planning`、`MutationScope=workflow_doc`。
9. 未识别的 `full` 阶段应 fail closed：不自动激活 orchestrator，测试提示补充 `Kind` / `MutationScope`。

这条规则很关键：新 contract 不能在兼容层重现旧问题。所有“能改项目”的结论必须能追溯到显式 `MutationScopeProject` 或已知安全映射。

## 状态机推进规则

统一状态推进应只发生在 engine：

1. `StartWorkflowWithOptions` 创建 `WorkflowState`，注入 intent/source material。
2. `DerivePhaseRuntimeGate` 计算当前 gate。
3. 若模板 `RequiresInput` 未满足，进入输入等待，不运行 agent loop。
4. 若当前阶段 `InputSchema != nil` 且未提交，返回 `ShowForm`，不运行 agent loop。
5. 表单提交后，构造 phase prompt，运行 agent loop。
6. `SavePhaseOutputAndMaybeAdvance` 保存阶段产物。
7. 若 `RequiresReview`，进入 awaiting review，不自动推进。
8. 用户 confirm/skip/revise 后，`ApplyReviewIntent` 推进或重跑当前阶段。
9. 若进入 `ActivatesOrchestrator` 阶段，GUI/agentservice 调用执行编排。
10. 非 review 执行阶段完成后由 engine 推进到下一阶段或完成流程。

边缘层禁止自己根据文本猜测推进阶段。

## 统一接入边界

### GUI

GUI 应只做四件事：

- 用 `PhaseContract.ToolPolicy` 过滤 LLM 可见工具。
- 用 `PhaseContract.ActivatesOrchestrator` 启动 coding orchestrator。
- 用 `PhaseContract.ExpectsDocument` 和 `PhaseMeta` 展示文档预览。
- 用 `PhaseRuntimeGate.BlocksAgentLoop` 判断是否应暂停 agent loop。

不应在 GUI 里重复写“Full 且非 NeedsConfirm 才执行”的新变体判断。该判断应由 core contract 给出。

### TUI

TUI 应继续复用同一套 registry、engine、metadata/contract 派生。TUI 没有预览面板，但仍应记录同一份阶段能力，保证行为一致。

### agentservice

agentservice 需要接收并执行 `tool_policy`，但不应理解模板细节。它只负责：

- 从 message/session metadata 读取 `tool_policy`。
- 在工具曝光和具体执行前调用 core workflow 校验。
- 对 ops 执行使用已审批命令 manifest。

### Frontend

前端不应维护独立阶段规则。现有 fallback map 只能作为降级路径。目标是所有流程阶段展示来自后端生成的 `PhaseMeta` 或未来 `PhaseContractMeta`，并用 contract test 防漂移。

## 模板编写规则

未来新增/修改流程模板时按以下规则：

| 场景 | 应使用 |
| --- | --- |
| 只产出文档，不需要查仓库 | `NeedsConfirm=true` + `ToolPolicy=doc_only` + `MutationScope=workflow_doc` |
| 产出可审阅计划，需要查仓库 | `NeedsConfirm=true` + `ToolPolicy=planning` + `MutationScope=workflow_doc` |
| 要改项目文件、创建目录、写 CMake/source/package | `NeedsConfirm=false` + `ToolPolicy=full` + `Kind=execution` + `MutationScope=project` |
| 要生成 PPT/DOCX/PDF 等 artifact | 独立 generation phase，`Kind=artifact_generation` + `MutationScope=artifact` |
| 运维风险策略 | `Kind=ops_risk_policy` + reviewable policy phase，产出 allowed_commands manifest |
| 运维执行 | `Kind=ops_execution` + `ToolPolicy=ops_controlled` + `MutationScope=ops` + `DisableOrchestrator=true` |

反规则：

- 不要用 `doc_only` 阻止 coding 提前执行，同时又期待它能查仓库。
- 不要在 reviewable 阶段给 `full`，否则主 agent 可能抢跑实现。
- 不要让 agent 自己写阶段文档到项目目录；阶段文档由 workflow engine 保存。
- 不要在模板 prompt 里单独发明推进规则；推进规则属于 engine/contract。
- 不要让 artifact generation 复用 coding execution 语义；artifact 写入和项目源码修改必须分开。

模板注册时必须做冲突校验；`WorkflowRegistry.Register` 返回错误并拒绝冲突模板，`MustRegister` 用于内置模板，失败时立即 panic：

| 冲突 | 处理 |
| --- | --- |
| `NeedsConfirm=true` + `MutationScope=project` | 拒绝或要求拆成 planning + execution 两阶段 |
| `NeedsConfirm=true` + `ToolPolicy=full` | 拒绝 |
| `ToolPolicy=doc_only` + `MutationScope=project` | 拒绝 |
| `ToolPolicy=planning` + `MutationScope=project/artifact/ops` | 拒绝 |
| `ToolPolicy=full` + 未知 `Kind` / 非 `project|artifact` scope | 拒绝 |
| `Kind=artifact_generation` + `ActivatesOrchestrator=true` | 拒绝 |
| `Kind=ops_execution` + `ToolPolicy!=ops_controlled` | 拒绝 |
| `Kind=execution` + `MutationScope!=project` | 要求显式说明，否则拒绝 |
| `Kind=execution` + `ToolPolicy!=full` | 拒绝 |

## 迁移方案

### Phase 1：固化当前修复

已完成或应保持：

- `planning` 工具策略作为 reviewable coding planning 的标准边界。
- `PhaseExpectsDocument` 先看 `NeedsConfirm`，再看工具策略。
- 具体工具调用统一走 `ValidateToolCallByPolicyWithApproval`。
- direct CodingSubAgent 只允许 execution orchestrator 阶段触发。
- 确认任务拆分后再进入 `implementation`，此时开放 execution/orchestrator；主 agent 不直接获得本地项目变更工具，只能委派内部 CodingSubAgent。

### Phase 2：新增 `PhaseContract` 派生器

新增核心函数：

- `DerivePhaseContract`
- `DeriveWorkflowContracts`
- `DerivePhaseRuntimeGate`
- `PhaseContractMetadata`

先不改模板结构，只从现有字段派生，保持兼容。

### Phase 3：边缘层改为只消费 contract

逐步替换：

- `PhaseMetadata` 内部改用 `DeriveWorkflowContracts`。
- `IsExecutionOrchestratorPhase` 改为 contract 派生结果。
- GUI/TUI phase update 附带 contract 摘要。
- agentservice metadata 使用 contract 里的 `ToolPolicy`。
- GUI/TUI 的 agent-loop pause 逻辑使用 `PhaseRuntimeGate.BlocksAgentLoop`。

### Phase 4：模板声明升级

可选引入显式字段：

```go
Kind PhaseKind `json:"kind,omitempty"`
MutationScope MutationScope `json:"mutation_scope,omitempty"`
```

迁移原则：

- 老模板无 `Kind` 时按现有字段派生。
- 新模板优先声明 `Kind` 和 `MutationScope`，字段冲突时测试失败。
- `Kind` 只表达意图，实际能力仍由 `DerivePhaseContract` 统一裁决。
- `MutationScope` 表达允许变更的对象范围，不直接决定工具曝光。

## 测试策略

核心测试应覆盖三层：模板契约、工具边界、推进行为。

### 模板契约测试

- 所有 `NeedsConfirm=true` 阶段必须 `ExpectsDocument=true`。
- 所有 `code_planning` 阶段必须允许 repo inspection 且禁止 project mutation。
- 所有 `execution + MutationScopeProject` 阶段必须 `ActivatesOrchestrator=true`，除非显式禁用。
- 所有 `artifact_generation` 阶段必须 `ActivatesOrchestrator=false`。
- 所有 `ops_execution` 阶段必须 `DisableOrchestrator=true` 且 `MutationScope=ops`。

### 工具策略测试

- `planning` 允许只读命令：`git status`、`rg -n`、`go list`。
- `planning` 禁止写命令：`mkdir`、`touch`、`tee`、`sed -i`、`go mod tidy`、`npm install`。
- `planning` 禁止写工具和委托工具：`write_file`、`edit_file`、`edit_lines`、`task`、`delegate_task`。
- `ops_controlled` 要求 approved manifest。
- `full + MutationScopeArtifact` 不应触发 coding orchestrator。
- `full + MutationScopeProject` 才允许 coding subagent/orchestrator；coding workflow implementation 的主 agent 只暴露 `delegate_task(agent="coding_workflow")` 和读/交付工具，项目变更由 CodingSubAgent 执行。
- coding workflow implementation 暴露给 LLM 的 `delegate_task` schema 必须收窄为 `agent=coding_workflow`，避免工具描述继续提示 `help` 或其他委派路径。
- `delegate_task` 是 coding implementation 的 essential handoff tool；参数截断时只能注入压缩请求提示，不能加入临时 blocked tools，否则主 agent 会再次落入“无可用编程工具”状态。
- `Register` 拒绝冲突模板，且不能覆盖已有有效模板。

### 推进测试

- reviewable 阶段保存输出后不自动进入下一阶段。
- confirm 后才进入下一阶段。
- revise 后重跑当前阶段。
- skip 只对 `CanSkip=true` 生效。
- execution 阶段完成后由 engine 推进。

### 集成契约测试

- GUI、TUI、agentservice 对同一阶段得到同一 `ToolPolicy`。
- 前端 phase metadata 与后端生成结果保持一致。
- direct CodingSubAgent 在非 execution 阶段被拒绝；在 coding implementation 阶段，主 agent 的 `bash`/`write_file`/`edit_file`/`craft_tool`/`task` 也必须被工具列表和执行 gate 双重拒绝。
- coding task breakdown 的工具列表不为空，且没有写工具。
- artifact generation phase 即使使用 `full`，也不被 GUI/TUI/agentservice 当成 coding execution。

## 风险与注意事项

| 风险 | 处理 |
| --- | --- |
| 旧模板未声明新字段 | 先派生兼容，不强制迁移 |
| 前端 fallback 规则漂移 | contract test 覆盖，逐步弱化 fallback |
| `full` artifact generation 被误判为 coding orchestrator | 用 `Kind=artifact_generation` 或 `DisableOrchestrator=true` 明确 |
| planning 只读命令识别漏判 | 继续保守 deny-by-default，扩展测试用例 |
| 文档保存被误认为工具写入 | 文档明确区分 system doc persistence 与 project mutation |
| 运行时 gate 被写进静态模板 | 拆分 `PhaseContract` 和 `PhaseRuntimeGate` |

## 推荐落地顺序

1. 保持当前 `planning` 修复，不回退到 `doc_only` workaround。
2. 新增 `PhaseContract`、`PhaseRuntimeGate` 和派生测试，先不动模板。
3. 新增 `MutationScope` 派生，先从 `Kind/ToolPolicy/NeedsConfirm/DisableOrchestrator` 兼容推导。
4. 将 `PhaseMetadata`、`IsExecutionOrchestratorPhase`、工具策略 metadata 都改为从 contract 派生。
5. 更新 GUI/TUI/agentservice：只消费 contract/runtime gate，不重复组合判断。
6. 增加模板 authoring checklist 和所有内置模板 contract 快照测试。
7. 最后再考虑给模板显式加 `Kind` 和 `MutationScope`，避免一次性大迁移。

## 结论

根因修复不是继续调某一个阶段的 `ToolPolicy`，而是把“阶段意图 -> 阶段能力 -> 工具边界 -> 状态推进 -> UI 展示”收束成一个统一 contract。

`planning` 策略解决了当前 coding 任务拆分无工具的问题；`PhaseContract` 则解决后续所有流程模板都要一个个补的问题。

## Prompt Contract 补充：提示词语义也必须受阶段契约约束

还有一层根因在提示词语义。coding implementation 模板仍然把主 workflow agent 描述成“实现者”，但运行期工具策略已经要求实现必须通过内部 CodingSubAgent 完成。只靠工具闸门，会导致模型先选择旧的主 agent 直写路径，然后撞到受限工具集，最后报告“没有工具”。

现在 `BuildPhaseSystemPrompt` 会给 `coding/implementation` 追加统一 handoff contract：

- 主 workflow agent 是协调者，不是代码修改者。
- 实现必须通过 `delegate_task(agent="coding_workflow")` 委派。
- 主 workflow agent 不能直接调用 `bash`、`write_file`、`edit_file`、`craft_tool`、`task`、外部 session、`ssh` 等本地项目变更工具。
- `read_file` 和 `list_directory` 只用于构造简洁委派请求。
- 如果 `delegate_task` 缺失，必须报告 workflow tooling error，不能声称无法编程，也不能绕过流程自己实现。
- no-tool/stall 恢复提示也必须走同一契约：coding implementation 阶段只能提醒 `delegate_task(agent="coding_workflow")`，不能回退到通用“选择文件/编辑工具”提示。

这样提示层和 contract/tool 层一致，模型会先走正确路径，而不是等工具调用被拦后才暴露问题。
