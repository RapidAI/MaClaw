# MaClaw GUI 语义工具路由改进方案

> 现状校正（2026-09-06）：本文描述目标设计和迁移方向，不代表所有链路已经上线闭环。基于日志的当前状态、阻断项和实施顺序以 [《语义工具路由日志复审与改进计划》](D:/workprj/aicoder/docs/maclaw-semantic-tool-routing-review-2026-09.md) 为准。

## 1. 问题定义

当前路由通过关键词和请求长度推断是否需要工具，例如根据 `implement`、`write`、`fix` 等词决定是否进入完整执行路径。这种方式存在三个问题：

- 关键词无法可靠表达任务范围，否定句、引用内容、示例代码和中文同义表达容易误分类。
- 工具召回依赖命中词，导致任务需要的工具未进入候选集，出现“工具不完整”或“工具消失”。
- 工具集合在多轮对话、重试和能力变化时不稳定，模型可能重复调用、改用错误工具或无法解释工具不可用原因。

目标是将工具召回从“关键词命中”改为“任务范围驱动”，并保证一个任务生命周期内工具集合稳定、可解释、可回退。

## 2. 设计目标

1. 先识别任务范围，再召回工具；关键词只能作为辅助特征，不能直接决定工具。
2. 同一任务使用稳定的工具快照，避免多轮过程中工具随机出现或消失。
3. 工具不完整时自动补齐；工具不可用时保留工具声明并返回结构化能力错误。
4. 高风险工具必须经过权限、能力和状态校验，语义路由不能直接授予执行权。
5. 每次路由都可从日志还原：任务范围、候选工具、选择原因、版本和回退过程。

## 3. 总体架构

```text
用户请求
   |
   v
任务范围解析器（Scope Resolver）
   |
   v
任务范围对象（Task Scope）
   |
   +--> 能力目录（Capability Catalog）
   |       - 工具元数据
   |       - 输入输出契约
   |       - 风险等级
   |       - 前置条件
   |
   +--> 工具计划器（Tool Planner）
           - 召回完整工具集
           - 依赖补齐
           - 稳定排序
           - 生成回退路径
                   |
                   v
             执行前策略检查
                   |
                   v
                工具执行
```

路由器只负责生成计划，执行器负责能力、权限、连接状态、审批和幂等校验。两者不得通过字符串约定隐式传递授权。

## 4. 任务范围模型

任务范围由用户目标和上下文共同确定，建议使用以下结构：

```json
{
  "scope_id": "scope-uuid",
  "scope_version": 3,
  "intent": "database_inspection",
  "objective": "inspect_schema_and_explain_query_error",
  "resources": [
    {"kind": "mysql_profile", "id": "mysql-192-168-1-242"}
  ],
  "allowed_actions": ["connect", "read_schema", "query"],
  "forbidden_actions": ["write", "delete", "execute_mutation"],
  "required_outputs": ["schema_summary", "error_explanation"],
  "risk_ceiling": "read_only",
  "conversation_id": "...",
  "routing_policy_version": "scope-router-v1"
}
```

### 4.1 范围解析规则

- `intent` 表示任务类型，不由单个关键词决定，而由目标、资源、期望输出和上下文联合推断。
- `allowed_actions` 明确本轮允许的动作集合。
- `forbidden_actions` 明确禁止的动作，覆盖模型或工具计划器的默认倾向。
- `risk_ceiling` 限制工具风险上限；只读任务不得因为后续文本出现“执行”一词而自动升级。
- 用户明确提出的新目标必须生成新的 `scope_version`；普通追问复用原范围。
- 范围不确定时返回澄清状态，不以猜测补齐高风险权限。

## 5. 基于任务范围的工具召回

### 5.1 工具元数据

每个工具必须登记到统一能力目录，至少包括：

```json
{
  "name": "db.query",
  "domain": "database",
  "capabilities": ["read_schema", "read_data"],
  "input_contract": "...",
  "output_contract": "...",
  "risk_level": "read_only",
  "preconditions": ["active_connection"],
  "alternatives": ["db.schema_snapshot"],
  "stable_surface": true,
  "availability_probe": "connection_state",
  "version": "2"
}
```

### 5.2 召回流程

1. 根据 `intent`、资源类型、允许动作和所需输出筛选工具域。
2. 根据工具依赖图补齐前置工具。例如 `db.query` 自动补齐 `db.connect` 和 `db.select_connection`，而不是等待模型猜测。
3. 根据 `risk_ceiling` 删除越权工具；越权工具不应只在提示词中隐藏。
4. 保留稳定表面工具。工具暂时不可用时仍返回工具声明，并标记 `enabled=false` 和不可用原因。
5. 按固定规则排序：任务必要性、依赖顺序、风险等级、工具版本、名称。禁止按召回文本顺序随机排列。
6. 将最终集合写入 `tool_snapshot_id`，本轮后续调用只使用该快照。

### 5.3 召回结果

```json
{
  "tool_snapshot_id": "toolsnap-uuid",
  "scope_id": "scope-uuid",
  "tools": [
    {"name": "db.connect", "enabled": true, "reason": "required_precondition"},
    {"name": "db.query", "enabled": true, "reason": "required_output"},
    {"name": "db.execute", "enabled": false, "reason": "risk_ceiling_read_only"}
  ],
  "fallbacks": [
    {"from": "db.query", "to": "db.schema_snapshot", "when": "query_timeout"}
  ]
}
```

## 6. 防止工具不完整或消失

### 6.1 任务级工具快照

- 在任务开始时生成工具快照，并绑定 `scope_id + scope_version`。
- 同一任务的多轮对话、重试和审批回调必须引用同一快照。
- 只有以下情况允许重新计算：用户明确改变目标、能力状态发生变化、工具版本升级。重新计算必须增加 `scope_version` 并记录原因。

### 6.2 稳定表面与不可用工具

对 `screenshot`、`open` 等宿主工具采用稳定表面策略：

- 始终出现在能力目录和工具快照中。
- 当前主机不支持时返回结构化 `capability_unavailable`。
- 返回 `retryable`、`alternatives` 和 `user_action`，避免模型盲目重试。

### 6.3 依赖闭包

工具计划必须是依赖闭包。任何工具只要出现在计划中，其所有必需前置工具都必须同时存在，或者计划必须显式标记为不可执行。禁止出现“只有 execute 没有连接或查询确认工具”的残缺计划。

### 6.4 快照失效

实现层提供任务级快照失效接口。只有以下宿主状态变化才允许失效并重建工具集合：

- 能力连接状态变化，例如 MCP server 健康状态或数据库连接状态改变；
- Skill/MCP 注册表发生增删、版本或契约摘要变化；
- 路由策略版本或任务权限范围发生变化。

用户改写请求、补充上下文、普通多轮追问和迭代次数变化不得触发失效。失效时必须清理旧集合和旧 `tool_snapshot_id`，下一次请求重新生成完整集合并记录失效原因。

## 7. 执行与安全边界

- 工具召回不等于工具授权。
- 写操作必须绑定 `operation_id`、目标资源、参数摘要和审批 token。
- 查询失败、连接状态过期或审批被拒绝时，禁止自动进入写执行。
- 同一 `operation_id` 幂等；审批拒绝后只能由用户新建操作。
- 连接建立采用单飞机制，同一资源同时只有一个真实连接动作，其余请求复用 pending 结果。
- 工具失败由错误类别驱动回退：参数错误进入参数修正，网络错误进入有限重试，能力不可用进入替代工具或用户提示。

## 8. 路由日志

每次任务至少记录：

```json
{
  "event": "tool_route",
  "timestamp": "...",
  "conversation_id": "...",
  "scope_id": "...",
  "scope_version": 3,
  "routing_policy_version": "scope-router-v1",
  "tool_snapshot_id": "toolsnap-uuid",
  "intent": "database_inspection",
  "candidate_tools": ["db.connect", "db.query", "db.execute"],
  "selected_tools": ["db.connect", "db.query"],
  "excluded_tools": [{"name": "db.execute", "reason": "risk_ceiling_read_only"}],
  "fallbacks": [],
  "confidence": 0.93,
  "result_class": "planned"
}
```

工具执行日志还应包含 `operation_id`、`attempt`、`parent_action_id`、`approval_id` 和 `query_fingerprint`，确保审批、查询和执行可以关联。

## 9. 迁移计划

### P0：一致性与安全

- 实现 `scope_id`、`tool_snapshot_id`、`operation_id`。
- 为数据库工具增加依赖闭包和查询成功前置条件。
- 阻止审批拒绝后的同操作重试。
- 为连接建立增加按资源的单飞与幂等。

### P1：范围路由器

- 建立能力目录和工具元数据格式。
- 实现任务范围解析器和工具计划器。
- 将 `acpPromptMayMutateWorkspace`、`acpPreferLightProfile` 降级为辅助特征，移除其直接路由职责。
- 增加范围不确定时的澄清状态。

### P2：稳定性与评估

- 实现稳定表面工具和结构化能力错误。
- 增加工具快照回放、路由版本和离线评估集。
- 建立误召回率、漏召回率、工具消失率、重复调用率和高风险越权率监控。

## 10. 验收标准

- 任务工具集合由范围和能力目录决定，单个关键词变化不会改变工具计划。
- 同一任务的多轮和重试使用同一 `tool_snapshot_id`。
- 任一计划都满足依赖闭包，不存在残缺工具链。
- 工具不可用时不会从模型表面消失，且不会触发无限重试。
- 查询失败后不会自动执行写操作。
- 审批拒绝后同一 `operation_id` 不会再次执行。
- 日志可以完整还原“为什么召回、为什么选择、为什么回退”。

## 11. 当前代码落地映射

当前工作树已经落地工具召回和快照链的若干基础部件，但动态 Skill/MCP 仍保持 fail-closed：

- `guiapp/coding_subagent.go` 与 `gui/coding_subagent.go`：回调默认采用任务范围模式；缺少可信 planner/admission 时不从 registry 或任务文字补齐动态工具。
- `coding_subagent_skills.go` / `coding_subagent_mcp.go`：范围模式只接受 host admission 与 planner selection 的交集；关键词/BM25 仅保留在未治理的兼容入口。
- `coding_tool_scope_snapshot.go`：生成稳定快照并在宿主能力变化时设置失效围栏；失效的旧 plan 不会被重新采纳。
- `semantic_tools_search.go`：`tools_search` 绑定当前 `scope_id + catalog_digest`，分页 token 还绑定 capability filter 和目录摘要；query 文字只用于解释，不参与集合选择。静态目录行是可用性说明，不是执行授权。
- `corelib/tool.InvocationScope`：`ToolSnapshotID` 与 `CatalogDigest` 分开校验；旧持久化行只可由可信 route/model-surface reader 通过 `CanonicalInvocationScope` 补齐空快照，直接 `Validate` 仍严格拒绝缺失或冲突快照。

Responses-WS 的 E1--E4 qualification 证据和 hermetic 测试可以证明 transport/lifecycle 原语，但不能单独打开生产动态 alias。当前 `newQualifiedCodingBoundDynamicRequestLifecycleRelay` 只有测试 override 才构造 rehearsal relay；生产 callback 仍返回 nil，直到同一回调接收到完整 host scope plan、动态 binding admission、durable surface 和 coordinator 绑定。这样可避免 transport qualification 绕过任务范围准入。

后续接入 semantic planner 时，必须把 `tool_snapshot_id` 绑定到 planner 的 Catalog Snapshot 和持久化 Invocation Scope；不得重新从用户文本调用旧的 `selectRelevant*ForTask` 作为第二套召回入口。只有完成真实 scope/admission 接线、恢复交叉校验和旧路由删除门槛后，才能宣称 Catalog、Plan、Scope 和 Grant 形成不可漂移的授权链。

### 12. 持久化恢复与幂等收口（2026-09-07）

本轮补齐了三条容易让工具面在重启或重试时“消失”或错配的边界：

- 物化记录写入前从可信 route scope 与已签名 grant 计算 binding scope；旧 route 行会在首次遇到具体快照时绑定 `ToolSnapshotID`，但不会改写旧 grant 的签名字段。
- 模型请求 surface 发布、解析和恢复均以 route row 为快照权威。旧 surface 列为空时只从 route 或签名 grant canonicalize，具体冲突直接返回 `stale_surface`。
- `PrepareDeliveryAndComplete` 先校验 host-call、请求摘要、结果摘要和 delivery operation，再接受 `running/admitted` 或 `awaiting_receipt/completed` 的幂等重试；当前或已完成 delivery 的重复提交不会重复产生外部发送，结果不一致会拒绝。

这些补丁仍遵守“计划决定范围、目录决定候选、授权决定执行”的链路，不能作为重新打开动态 Skill/MCP alias 的依据。

### 13. tools_search 别名状态收口（2026-09-07）

复核发现按 capability 标记 planned 会把未选中的同能力别名显示为“前置步骤完成后自动出现”，实际 surface 却不会渲染该别名。已将状态判断收紧为当前 ToolPlan.Selections 的精确 AdapterName，并在 gui/guiapp 增加回归测试，避免目录发现再次制造“工具会出现但实际消失”的错误预期。

### 14. host-call 幂等键必须同时绑定参数摘要（2026-09-07）

协调器的 repeat sibling 兼容逻辑不能只依据 grant fingerprint。对已存在的 host-call 记录，只有 `grant_fingerprint` 与 `request_digest` 同时匹配，或新 grant 属于历史兼容 fingerprint 且 `request_digest` 仍完全相同，才允许 replay/in-progress/unknown 状态复用；同一 transport call ID 携带不同 canonical 参数必须返回 `host_call_conflict`。该约束已落实在 `Admit`、`Reject`，并由 Coding durable dynamic surface 回归测试钉死。
### 15. tools_search 必须使用渲染名而非内部 adapter 名（2026-09-07）

目录条目的名称必须来自实际模型面：稳定 host adapter 经过 `SemanticModelFunctionName`，动态 MCP/Skill 只允许使用 `surface.grants`/`retiredGrants` 的真实 opaque key。`surface.schemas` 的 key 是 source adapter，只能补充描述，不能直接作为模型可调用名；未 materialize 的动态 selection 不得进入目录。这样搜索结果与 renderer、authorizer、执行入口共享同一名称闭包，避免模型获得一个永远不会出现的内部名称。
### 16. 动态范围校验使用可信 adapter key（2026-09-07）

动态 provider 的 definition 会使用固定 `dynamic_provider` 占位函数名，避免把 MCP/Skill 身份带入模型面。scope router 不得从 `function.name` 推导动态 resolver 身份；应使用 host-owned definitions map 的 key（即 planner 的 `AdapterName`）验证允许集合、依赖闭包和缺失诊断。新增 `RouteForScopePlanByAdapter`，校验时临时建立 adapter-keyed identity，返回时保留原始 definition，防止已绑定的动态工具被错误判为 `scope_plan_incomplete` 或在搜索/发布阶段消失。