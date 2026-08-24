# 语义工具路由设计复审与修正方案

> 状态：建议采纳，作为 `semantic-tool-routing-continuity-and-surface-reform-zh.md` 的实施门禁补充。  
> 结论：主方案的方向正确；但 CodingSubAgent 的动态 Skill/MCP 目前仍是“选择器收口”，尚不是由计划、grant 与 journal 共同约束的语义执行闭环。不得将其标记为已迁移。

## 1. 复审结论

“多轮后工具不完整”的根因判断是成立的：历史工具名被当作连续性状态并参与本轮工具集合，最终与固定预算竞争，导致静默裁剪。正确的原则是：**任务连续性保存事实与工件，不保存工具名；每个模型请求从当前计划完整替换工具面。**

不过，设计落地时必须再守住一个边界：

```text
候选召回 / BM25 / matched set  --只能提供证据-->  ToolPlan
ToolPlan + 当前状态           --唯一授权来源-->  Grant + Materialization
Grant + 已发送请求             --唯一呈现来源-->  RequestSurface + Alias
可信响应 + ToolCallID          --唯一执行入口-->  Journal + Admit + 固定 dispatcher
```

若任一箭头被绕过，系统仍会出现“工具定义看起来受控、实际执行却由旧 map/name 决定”的双重授权。当前 `codingDynamicSurface.byName` 与 `executeBoundCodingSkill/MCP` 正是这个剩余缺口：它们已不暴露 provider selector，但仍将 match 直接变成内存中的执行权。

因此本次修正的验收目标不是“alias 更随机”，而是：

1. 任何模型可见 alias 都能反查到 current `PlanID + SelectionID + GrantFingerprint`。
2. 任何执行都先经过同一 coordinator 的 current-lineage 检查、host-call journal 和 grant consume。
3. 重试、取消、替换、重启和并发 executor 下，内存中的 match/alias 永远不能恢复执行权。

## 2. 必须先修正的设计缺口

| 优先级 | 缺口 | 风险 | 规范性修正 |
| --- | --- | --- | --- |
| P0 | Coding 动态 binding 仍由 `matchedSkills/matchedMCPTools` 直接授权 | BM25 候选重新获得执行权；重启后无法审计或恢复 | match 只能成为 planner evidence。无 capability、effect、repeat、参数授权和 binding digest 的 contract 时返回 `catalog_incomplete`。 |
| P0 | 未建立可信 `TenantID/PrincipalID/SessionID/RootTaskID/TurnID` ingress | 用 user ID、loop ID 或路径补 scope 会串联并发任务 | 为 local、remote、nested 构造链显式传入已验证 identity；缺任一字段不 materialize 动态工具。若 `PrincipalID` 非全局唯一，还必须将 tenant 纳入 invocation scope/route key，而非只放在连续性投影。 |
| P0 | `ToolCallExecutionContext` 只有 epoch/response ID，缺 protocol 与 host connection identity | alias 无法组成稳定的 `HostCallIdentity`；不同请求可能碰撞 | Context 至少携带 `SurfaceEpoch`、`Protocol`、`ConnectionID`、provider `ResponseID`；后三者只能由 request adapter/host 写入。 |
| P0 | request surface 的“发送前发布”与“发送成功后写入”表述矛盾 | 要先有 alias 才能发请求；若误把 prepared record 当已发送请求，会留下孤儿 surface | 明确状态机：`prepared -> active(sent) -> bound(response) -> finished/superseded/cancelled`。prepared 可渲染但**不可解析/执行**；请求未真正启动即原子 abandon。response ID 只可把 active surface bind 为 response-bound。 |
| P1 | retry/fallback 的 retire + publish 是两次操作 | A、B request 可短暂同时 active；晚到 A 可能消费本应给 B 的未消费 grant | coordinator 增加原子 `ReplaceModelRequestSurface`：在同一事务中 retire predecessor，再校验 current/exposed grant 并创建 successor。普通 retry 不重签 grant。 |
| P1 | route cancel 没有统一的 request-surface/materialization/grant 原子撤销入口 | GUI 若分别 retire/revoke 会产生竞争窗口 | coordinator 增加 `CancelRouteSurface(scope, reason)`：同一事务 retire 所有 active request surfaces、retire materializations、revoke issued grants，并写审计原因。child revision 发布复用相同内部事务。 |
| P1 | 执行仍直调 bound Skill/MCP | 没有 journal replay/conflict、grant consume 或 current fencing | 抽取固定 bridge，统一使用 `canonicalize -> issuer.Validate -> coordinator.Admit -> ExecuteSelectionWithEffects -> Complete/Reject`。模型永远不能选择 bridge 的 provider。 |
| P2 | provider correlation 是“解析到响应后才赋值”的隐式步骤 | 绑定失败时容易错误执行或只靠名称继续 | core loop 需要显式 request lifecycle hook：请求渲染/发送后登记 surface；取得 `ResponseID` 后先 `BindModelRequestResponse`，成功才派发 tool calls。缺 ID、缺 call ID、bind 失败均返回 `stale_surface`。 |
| P2 | 外部副作用与 completion 未收敛 | crash/retry 可能重复执行或在 child revision 写旧结果 | 有 effect 的 dynamic selection 必须走 existing external-effect/receipt settlement；未具备 receipt contract 的动态 provider 只能只读或 `catalog_incomplete`。 |

## 3. 目标模型：授权、呈现、执行三层分离

### 3.1 不可合并的生命周期

`InvocationGrant` 不是 function alias；`ModelRequestSurface` 也不是新的 grant。三层分别解决不同问题：

| 对象 | 谁创建 | 生命周期 | 能否单独授权执行 |
| --- | --- | --- | --- |
| `PlannedSelection` | ToolPlanner | immutable revision 内 | 否；仅声明被批准的能力与固定 binding。 |
| `InvocationGrant` / `RouteMaterialization` | coordinator publish/materialize | `issued -> consumed/revoked/expired` | 否；还需 request alias 与 host call admission。 |
| `ModelRequestSurface/Alias` | coordinator request lifecycle | `prepared -> active(response-bound) -> terminal` | 否；只证明 alias 确实出现在一个已发送、并已和可信 response 关联的请求中。 |
| `HostCallRecord` / execution | coordinator admit/complete | journal/execution lifecycle | 是唯一可进入 dispatcher 的路径。 |

推荐请求时序如下。`Replace` 与 `Cancel` 都必须在 coordinator 内原子执行，不能由 GUI 依次调用多个 store。

```text
planner -> PublishSurface -> materialization + issued grant
renderer -> Prepare request surface (opaque aliases)
transport -> HTTP request starts
provider -> response metadata(ResponseID)
adapter -> BindModelRequestResponse (prepared -> active)
tool call -> ResolveAlias -> Validate -> Admit -> fixed bridge -> Complete/Reject

retry/fallback: Replace(A, B), reuse A 的 issued grant
revision replacement/cancel: CancelRouteSurface(A), revoke A 的 issued grant
```

“request has started”是 transport 的可信事件，不是模型文本，也不是 loop iteration；它允许保留 `prepared` 记录以便诊断和原子替换，但不使 alias 可解析。唯一可执行状态是 `active(response-bound)`：`BindModelRequestResponse` 成功后才进入该状态。对于无法提供可验证 response correlation 或 tool-call ID 的协议，动态 Skill/MCP 必须不渲染；静态、非动态工具可走其既有兼容策略，但不能降级为 name-based 动态 dispatch。

### 3.2 Coding capability contract

每个候选 Skill/MCP 在进入 planner 前必须被转换为主机签发的 contract。以下字段不可由模型、Skill 描述或 MCP 返回结果临时补全：

```go
type CodingDynamicCapabilityContract struct {
    CapabilityID            tool.CapabilityID
    AdapterName             string
    ProviderBindingDigest   string // skill: stableID/name/version/content/contract；MCP: server/tool/schema/contract
    ParameterAuthorization  tool.ParameterAuthorization
    Effect                  tool.EffectClass
    RepeatPolicy            tool.RepeatPolicy
    ReceiptPolicy           tool.ReceiptPolicy
    TrustedSchemaDigest     string
}
```

`SkillMatch` / `MCPToolMatch` 只能贡献候选证据和绑定快照，不能替代 `CapabilityID` 或把任意命令、目标、路径、server ID 偷渡进参数。contract 不完整、schema 漂移、inventory 不可读或 provider 健康状态不满足时，结果是 `catalog_incomplete` 或同 need 的 replan，不是 generic gateway。

### 3.3 唯一执行桥

dynamic dispatcher 必须只接收已经解析好的 `PlannedSelection` 与 host-owned binding。推荐抽象：

```go
ExecuteBoundSelection(ctx, selection, canonicalArgs) (result string, effects []tool.Effect, err error)
```

别把 `serverID`、`skillName`、`runID`、项目路径或 provider descriptor 再作为该函数的模型输入。bridge 可在执行前重新读取 principal-scoped inventory，核对 binding/schema/contract digest；不一致时用 `Reject` 写出 `skill_binding_stale` 或 `mcp_binding_stale`，并触发同一 need 的 replan。

## 4. 推荐实施顺序与停止条件

### P0：先建立可拒绝的边界

交付内容：

1. `TrustedCodingInvocationIdentity` 的 verified ingress 与 local/remote/nested 全链路传递；禁止从 `LoopContext.ID`、`Runtime.RequestID`、`codeSessionID`、user ID、project path 或文本生成 RootTaskID。
2. provider capability matrix：是否有 response ID、tool-call ID、请求 connection identity、取消语义。矩阵不满足的 provider 不 materialize Coding dynamic surface。
3. `ToolCallExecutionContext` 完整 correlation carrier，以及 prepared/active/bound/terminal request-surface 状态机。
4. dynamic Skill/MCP capability contract 与 fail-closed catalog builder。

停止条件：没有可信 identity、coordinator、contract 或 correlation 的任一路径，工具列表中不得出现 `skill_*` 或 `mcp_*`，执行也不得偷偷调用旧 handler。

### P1：计划化与持久发布

交付内容：把 match 变为 candidate evidence；由现有 `ToolPlanner + PublishSurface/MaterializeReadySurface` 生成 selection、grant 和 materialization；renderer 仅从 exposed materialization 读取 grant 并生成 alias。实现原子 `ReplaceModelRequestSurface` 与 `CancelRouteSurface`。

停止条件：任意 Coding alias 可通过 durable records 反查当前 route、selection 与 grant；清空进程内 `byName` 后不能靠缓存执行，但可按 durable state 恢复当前呈现。

### P2：admission、journal 与效果提交

交付内容：local 和 remote 共用 fixed bridge；工具调用执行完整 `ResolveAlias -> canonicalize -> Validate -> Admit -> dispatch -> Complete/Reject`。由真实 `{Protocol, ConnectionID, ResponseID, ToolCallID, SurfaceEpoch}` 建立 journal identity；response bind 必须早于解析 tool call。

停止条件：同一 identity 重传返回首次结果，不重做 I/O；同 identity 不同参数返回 conflict；无 correlation、过期 epoch 或旧 revision 一律 `stale_surface`。

### P3：取消、崩溃与并发恢复

交付内容：steering replacement、timeout、transport failure、nested worker exit 统一调用 coordinator lifecycle API；带副作用的 provider 接入 receipt/effect settlement；补双 executor 与重启恢复测试。

停止条件：cancel/child revision 与旧 call 并发时，旧 revision 不能新建 journal、consume grant 或写 completion；不确定 receipt 不自动重放外部效果。

### P4：灰度与删除

交付内容：按 verified tenant + task handle 固定分桶，采集 `catalog_incomplete`、`stale_surface`、binding drift、journal replay/conflict、cancel fencing 和 recovery 指标。仅在 P0–P3 回归稳定后删除模型可达的 `executeBoundCodingSkill/MCP` 直调与全部 generic selector fallback。

停止条件：异常只能降级到澄清/重新规划/受限静态面；任何开关都不得恢复 session pin 或 `manage_skill`/`call_mcp_tool`。

## 5. 必须新增的回归用例

1. 同 principal 的两个 session 使用同名 Skill/MCP：route、grant、alias、journal 和 completion 完全隔离。
2. A request 的 alias 在 retry B、child revision、cancel、nested exit 后迟到：均为 `stale_surface`；只有 retry B 可复用尚未消费的同一 grant。
3. request 只完成 prepared、未真实发送，或响应缺少/不匹配 ResponseID：alias 不可 resolve，grant 未消费。
4. 同一 `ToolCallID + SurfaceEpoch + ResponseID` 重传：返回原 journal 结果；参数 digest 改变：`host_call_conflict`；不同 call ID 仍受 selection 的 repeat/effect policy 限制。
5. skill content/version/contract 或 MCP schema/server health 在 request 后变化：不执行，写 binding-stale rejection，且不按名称匹配其它候选。
6. child revision publish 与旧 tool call 并发：至多 current revision 的 admission 成功；旧 invocation 不产生外部 effect、artifact 或连续性事实。
7. 进程重启、两个 executor 恢复：只用 durable current materialization/request surface；删空内存 match map 不改变安全结论。
8. 缺任一 trusted identity 字段、coordinator、call ID 或 provider correlation：模型看不到动态 alias，返回 `catalog_incomplete`/`clarification_required`，不使用补造 ID。

## 6. 对主设计文档的编辑建议

主文档的 §8.3.17 可以保留为“第一阶段已完成”的历史记录，但必须显式标注为**非授权闭环、不可扩面、不可作为完成依据**。§8.3.18–§8.3.19 应补入以下约束：

1. request surface 的 `prepared/active/bound` 状态与发送证明，消除“发送前建 alias”与“发送成功后写 surface”的时序矛盾；
2. `ReplaceModelRequestSurface`、`CancelRouteSurface` 两个 coordinator 原子操作；
3. `ToolCallExecutionContext` 的 protocol/connection/response 三种独立可信字段；
4. tenant 与 invocation route key 的隔离决策；
5. Coding contract、effect/receipt policy 作为 P1 前置，而不是实现细节；
6. “不能恢复执行权”的 restart 测试作为 P1/P2 的硬门槛。

完成定义也应增加一条：**不存在任何从 `matchedSkills`、`matchedMCPTools`、内存 alias map、项目路径或 function name 直达 Skill/MCP 执行的模型可达路径。**

## 7. 最终决策

批准继续推进“计划驱动、全量 replacement”的主方向；但 CodingSubAgent 必须以 P0–P4 分阶段迁移，不应以当前随机 alias 实现替代 durable authorization。真正解决多轮工具缺失的标准不是持续保留更多工具，而是每个请求都能在当前任务计划内得到一个完整、可审计、可撤销且不会被旧对话污染的工具闭包。

补充：普通 IM 的 request/loop ID 也不得被误当作 semantic root、turn 或 session。没有 verified task relation 的 IM 只使用宿主私有的一次性 identity 进行本轮计划，不提供“根据同一 owner/request/文本继续”的授权；复用 loop context 收到新 request 时必须旋转该 identity。历史工具名只能用于展示/协议兼容，不能映射为当前唯一 grant。对已发布 managed surface 的取消，必须经 coordinator `CancelRouteSurface` 原子撤销 aliases/materializations/grants，而非只清内存工具列表。

## 8. 本轮复审修正：先完成 G1 身份锚点，禁止以 Runtime 任务替代语义任务

### 8.1 现状判定

当前实现已经完成一个重要但较窄的安全收口：CodingSubAgent 与 RemoteCodingSubAgent 在缺少可信 dynamic-catalog 闭环时，既不向模型渲染 `skill_*` / `mcp_*`，也拒绝旧 alias、`manage_skill` 和 `call_mcp_tool` 的模型调用。这个状态应称为 **P0 fail-closed 边界已建立**，而不是“Coding 动态能力已迁移”。

下一项阻塞不是 alias 格式、BM25 质量或再次放开 gateway，而是 G1：当前 local/remote runtime 的 `onStart(codingruntime.ExecutionRequest)` 只能得到 ledger 的 `TaskID`/`AttemptID`。它们可作为宿主映射键和审计字段，但不是已经证明的 semantic `RootTaskID`；SSH session、`LoopContext.ID`、`Runtime.RequestID`、`codeSessionID`、用户 ID、项目路径和文本 hash 同样都不是替代品。

因此，在可信锚点完整接入前，G2–G5 不得通过“先把 alias 接回去、以后再补身份”的方式并行推进。否则会把一个可审计的 fail-closed 降级成跨窗口/跨任务共享授权的风险。

### 8.2 必须新增的宿主身份入口

应在 GUI 宿主层新增一个仅由认证与任务服务构造的 `TrustedCodingInvocationIdentity` ingress。它不是模型可写 DTO，也不应由 `CodingSubAgent` 自行拼装：

```go
type TrustedCodingInvocationIdentity struct {
    TenantID    string
    PrincipalID string
    SessionID   string
    RootTaskID  string
    TurnID      string
}
```

推荐的职责边界如下：

| 情形 | 唯一可接受的 RootTaskID 来源 | runtime TaskID 的角色 | TurnID |
| --- | --- | --- | --- |
| 用户显式新建 coding task | 认证宿主创建 semantic root，并在创建 runtime task 时登记受限映射 | 只作该映射的 opaque key | 宿主为本次请求新签发 |
| 显式续接/调整任务 | 服务端校验 continuation/task handle、scope、expiry、current revision/fencing 后读取 anchor | 仅用于定位本次 ledger attempt | 宿主为新请求新签发 |
| local / remote 执行开始 | `onStart` 经 anchor resolver 校验 task/attempt 与已登记 mapping 后取得 | 只能作为 resolver 输入，不能直接赋值给 RootTaskID | 沿用已经验证的本次 turn |
| nested child | 可信父执行器显式继承同一 root 与 tenant/principal/session；child 另获新 turn | 记录 child runtime lineage，不能创建另一 root | 宿主为 child 签发新值 |

`SessionID` 与 `PrincipalID` 必须来自不同的可信来源；即使单用户桌面部署暂时相同，也不能以 `UserID` 同时填充两者。resolver 任一字段为空、session/principal 不匹配、runtime mapping 缺失或 child lineage 不能证明时，只返回不可用结果；动态 Skill/MCP 继续 `catalog_incomplete`，静态 coding 工具不受影响。

### 8.3 G1 实施切片与原子性要求

1. **定义 anchor 服务与不可伪造输入。** 输入只接受认证后的 tenant/principal/session、明确的新建/续接操作及服务端 handle；输出才是 identity。不得公开接受 `RootTaskID`、revision、fencing、task digest 或 runtime task ID 的“可信”构造函数。
2. **原子登记 runtime-to-root anchor。** 新建 root 或已验证续接启动 runtime task 时，将 runtime task/attempt lineage、identity scope、状态和过期信息以同一服务事务或可重放 outbox 登记。不能依赖 GUI 内存 map；进程重启后 resolver 必须仍能验证映射。
3. **local、remote 同步接入。** 两个 runtime `onStart` 都只调用 resolver 并写入 agent 的只读 identity；无法解析时保留 dynamic surface 为空。不得从 `ExecutionRequest` 之外的诊断字段补齐。
4. **nested 显式继承。** child 构造器只接收已验证的 parent identity/capability，而非原始 root 字符串；父取消、supersede 或 child lineage 不匹配时拒绝继承。remote SSH connection 只可作为 transport audit。
5. **再解锁 G2。** 只有 G1 的 durable resolver、跨重启验证和 local/remote/nested 回归通过，才把 inventory contract 转为 planner candidate evidence；此时仍不恢复任何 legacy gateway。

这条顺序避免了两个常见误修：一是把 `codingruntime.TaskID` 直接改名为 `RootTaskID`，二是为了桌面单用户兼容把 `UserID` 同时塞入 principal/session。两者都会把原本独立的 route lineage、grant、journal 与连续性域错误合并。

### 8.4 修订后的验收清单

在 G1 合并前，至少应有以下负向与恢复测试：

1. 同一 principal 的两个可信 session，即使同一 runtime task 文本、Skill 名和项目路径相同，也无法互相解析 anchor、grant 或 journal。
2. 伪造、缺失或过期的 runtime-to-root mapping；以及将 `TaskID`、SSH session、loop/request ID 代作 root 的输入，全部不渲染动态 alias 且无 handler I/O。
3. 新建、续接、refine、cancel、supersede、nested spawn/exit 各自验证 root/turn 的生成、继承或撤销规则；续接 handle 重放不能产生第二个有效 identity。
4. 进程重启后仅持久 anchor 可被 resolver 恢复；清空 CodingSubAgent 内存状态不得改变拒绝结论。
5. local 与 remote 的 `onStart` 行为一致；remote SSH 连接更换不会改变 semantic session/root，且不能借旧连接恢复动态授权。

G1 完成的证据应是：每一次未来的 Coding dynamic request 都能审计到 `tenant/principal/session/root/turn` 的独立来源及其 verified anchor，而不是仅看到一串 runtime 或 loop 标识。之后才可依次交付 G2（contract-backed inventory）、G3（plan/publish/durable alias）、G4（admit/journal/fixed bridge）与 G5（cancel/recovery/effect）。

## 9. 三次复审结论：把“已收口”与“已迁移”分开管理

本轮根据当前实现重新核对后，结论需要更精确：主问题的方向正确，且 P0 与 G1 的拒绝边界已经落地；但 Coding dynamic Skill/MCP **仍不是已迁移能力**。它现在处于安全地不可用，而不是功能上完整可用的状态。把这两种状态混写，会诱使后续维护者为了“恢复功能”重新打开按名称执行的旁路。

| 范围 | 当前状态 | 已经成立的保证 | 仍缺少的闭环 | 发布结论 |
| --- | --- | --- | --- | --- |
| 通用语义 surface | 已有 durable route/materialization/request-surface 原语 | surface replacement、epoch/response 关联、replace/cancel 原子 API | 各未迁移宿主的接线与全包并发验证 | 可继续作为唯一复用底座 |
| 旧 session pin 路由 | 已从最终工具面移除 | 历史工具名不再自动挤占预算 | legacy adapter 完全计划化 | 不得恢复 |
| Coding P0 | 已 fail-closed | `manage_skill`、`call_mcp_tool`、内存 `byName` 和 name fallback 不可由模型调用 | 真正的动态执行路径 | 不得以兼容名义放开 |
| G1 durable anchor | Desktop Wails 的受限 R1a--R1c 已接入；非 desktop 未接入 | runtime task/attempt 不能冒充 semantic root；desktop local/remote/nested 只能按 durable anchor 解析 | desktop relation 与 runtime anchor 的崩溃可恢复原子性；non-desktop 的认证 task/continuation 宿主 | 动态 alias 继续为空 |
| G2 catalog adapter | planner preparation 完成，生产 ingress 未完成 | catalog 只从 identity + inventory + contract 构建；match 未获授权；可生成 immutable `ToolPlan` | 从认证 ingress 提供真实 needs/facts，并接入 request lifecycle | 未完成 |
| G3 durable surface | 底座完成，未接入 CodingSubAgent | 已复用通用 coordinator 发布 plan/grant/materialization 与 durable request alias；prepared 不可解析，bind/replace/cancel 受原子 API 约束 | 真实 transport 的 request/response correlation，以及 local/remote 模型面接线 | 不得渲染或灰度动态 alias |
| G4 fixed bridge | 基础完成，未接入 Coding callback | durable alias 先 resolve，再 schema/contract canonicalize、issuer validate、coordinator admit/reject、固定 bound binding 和 complete；无 model selector | local/remote callback 的真实 provider correlation 接线，以及生产 inventory/receipt 验证 | 不得打开动态 alias |
| G5 lifecycle | 未开始接线 | G3 helper 已能调用 cancel/replace 原子 API | timeout/steering/nested exit/restart 宿主接线与 effect recovery | 不得连接真实动态 I/O |

这张表是发布门禁，不是进度展示：某行未完成时只能返回 `catalog_incomplete`、澄清或同 need 的重新规划；不能以“功能回退”解释为重新暴露 generic gateway。

### 9.1 不可变职责边界

后续实现必须明确只存在以下四种单向数据流；任一反向流都属于设计缺陷：

```text
认证 task/continuation 服务
  -> TrustedCodingInvocationIdentity -> durable SemanticTaskAnchor
宿主 inventory + host-signed contract
  -> ToolCatalog -> candidate evidence -> ToolPlanner
current ToolPlan + PlanState
  -> coordinator publish/materialize -> request-specific alias surface
可信 provider callback
  -> ResolveAlias -> Validate -> Admit -> fixed bridge -> Complete/Reject
```

特别是：

1. runtime `TaskID`、attempt、SSH 连接、`LoopContext` 只可向下作为 audit/mapping key，绝不可逆推 semantic identity。
2. BM25/matched set 只能向下提供“为什么考虑该 capability”的 evidence；它不能产生 `SelectionID`、grant、alias 或 provider binding。
3. prompt、definition 和 callback 均不得反向选择 Skill/MCP provider；模型只选择已渲染 alias，并只提交该 alias 的受限参数。
4. `ContinuityState` 只保存可验证的任务事实/工件投影，不能保存 dynamic adapter、函数名、match 或历史 alias。

### 9.2 以可交付物组织的修正方案

**R1：收紧并扩展 G1 的生产身份入口（先做）。** Desktop Wails 已有受限的 task relation、local/remote `onStart` binding 与 nested child lineage；下一步应先补齐其“显式新任务”语义和 relation/anchor 的崩溃可恢复交接，再扩展到其他经认证宿主。在认证后的“新建 coding task / 已验证 continuation”边界签发 identity，并在创建 runtime attempt 的同一事务或可重放 outbox 中登记 anchor。`setVerifiedCodingInvocationIdentity` 只能由这个宿主调用；agent、remote transport、nested constructor 都不能自行构造 identity。nested child 通过已验证 parent capability 派生同 root、独立 turn 的 child anchor。任何入口未到位的范围，禁止开始动态 alias 灰度。

**R2：把 G2 接到通用 planner，而不是接回 Coding map。** 为 contract-backed Skill/MCP inventory 实现 `ToolCatalog` provider；其输出必须包含 capability、binding/schema/content digest、parameter authorization、repeat/effect/receipt policy 和 health snapshot。BM25 只补充 candidate evidence。catalog、contract、health 或 digest 任一缺失时，planner 输出 `catalog_incomplete`；不能输出一个“稍后由 bridge 查名称”的 selection。

R2 的 demand-side 同样必须固定在宿主：Coding 只可从 reviewed `semanticCodingCapabilityRule` 展开 `CapabilityNeed` 及其 sibling budget，稳定记录 policy evidence；不得把 task 文本、BM25、`matchedSkills`、`matchedMCPTools` 或 provider metadata 作为 need/binding 的来源。所有 required policy need 必须同时满足；否则 `Plan.Unmet` 使整个 dynamic surface 返回 `catalog_incomplete`，不得选择性渲染“目前恰好可用”的 provider。

**R3：完成 G3 的发布链。** 使用现有 `ToolPlanner` 的 immutable selection 和现有 `SQLiteSemanticExecutionCoordinator` 的 `PublishSurface/MaterializeReadySurface`，从 exposed materialization 渲染 request alias；请求开始后 bind response，之后才允许 `ResolveAlias`。不得新增 Coding 专用 grant store、journal 或 alias map。为 retry/fallback 只调用原子 `ReplaceModelRequestSurface`，普通 retry 复用未消费 grant；revision/cancel/nested exit 只调用 `CancelRouteSurface`。

R3 的另一个硬规则是：**plan 不是“尽可能多渲染”的候选集。** 任一 required need 落入 `Plan.Unmet`，或 ready closure 为空时，整个动态 surface 返回 `catalog_incomplete`/replan，不能只发布恰好可用的 selection。否则模型会误以为当前 alias 覆盖了该任务，并把 planner 的不完整结果变成静默能力缺失。

**R4：完成 G4 的唯一执行桥。** local 与 remote 共用 `ExecuteBoundSelection(ctx, selection, canonicalArgs)`；bridge 读取的是 coordinator 已解析的 selection 和 host-owned binding，不接受模型传来的 Skill 名、MCP server/tool、run ID、path 或 selector。每次 callback 必须携带 `{Protocol, ConnectionID, ResponseID, SurfaceEpoch, ToolCallID}`，按 `ResolveAlias -> canonicalize -> Validate -> Admit -> bridge -> Complete/Reject` 处理；binding/schema drift 写 rejection 并触发同 need replan。

**R5：完成 G5，再考虑灰度。** 所有 steering、timeout、provider failure、child exit 和进程恢复必须统一走 coordinator lifecycle；有副作用的 provider 必须接 receipt/effect settlement。灰度键固定为 verified tenant + trusted task handle，不得按 user、文本或项目路径分桶。出现异常只可退至静态工具面与可解释的拒绝，不可恢复旧 gateway。

### 9.3 每个交付物的硬验收与责任人

| 交付物 | 主要改造面 | 必须通过的证据 | 不满足时的动作 |
| --- | --- | --- | --- |
| R1 / G1 ingress | 认证 task/continuation 宿主、runtime 创建链 | 新建、续接、remote、nested、重启后均能由独立来源审计五元 identity；跨 session/turn 不能解析 | 动态面为空 |
| R2 / G2 planner evidence | lifecycle inventory、contract registry、catalog adapter | 删除 matched map 后 planner 仍能生成/拒绝同样的 selection；contract 或 health 漂移失败闭合 | `catalog_incomplete` 或 replan |
| R3 / G3 durable surface | 通用 planner、coordinator、LLM request adapter | alias 可反查 current route、selection、grant、binding digest；prepared alias 不可解析 | 不发送动态 definitions |
| R4 / G4 admission bridge | local/remote callback、fixed bridge、journal | 重传无重复 I/O；参数冲突、缺 correlation、旧 epoch 均拒绝 | `host_call_conflict` / `stale_surface` |
| R5 / G5 lifecycle | cancellation/recovery/effect settlement | cancel/replacement/child exit 与旧 call 并发时旧 call 不能 consume 或完成；不确定 effect 不重放 | cancel + recovery 仅审计/澄清 |

### 9.4 合并与发布规则

1. 每个 R1--R5 只能在对应表格的负向测试和恢复测试全绿后合并；不得用人工试跑替代。
2. 在 R3 完成前，不允许把 `codingDynamicAliasesMayMaterialize()` 改为 `true`。
3. 在 R4 完成前，即使 R3 已能渲染 alias，也不得把 alias 连接到真实 Skill/MCP I/O。
4. 在 R5 完成前，不允许面向真实带副作用 provider 灰度；只读 provider 也必须满足 R3/R4。
5. 只有 R1--R5 全部完成后，才能删除/隔离历史 `executeBoundCodingSkill/MCP` 实现；删除前先用静态搜索和回归证明不存在模型可达引用。

最终评审判断：应批准按 R1→R5 继续改造；不批准任何以名称、历史工具面、runtime ID 或内存 match 恢复动态功能的“短期修复”。这既避免多轮工具面再次被污染，也避免从表面闭合退化为执行旁路。

### 9.7 IM 受管路径的 journal identity 复审（2026-08）

IM managed semantic surface 不属于 Coding dynamic transport，因此不能伪称拥有 provider-issued `ConnectionID`。但它仍需要 journal 防止同一 host callback 内的重试重复执行。正确做法是为**每一次 materialized surface**签发 host-private、随机的 journal partition nonce，并连同该 surface 的 epoch 与 provider tool-call ID 形成 host-call key；replacement、child revision 或 cancel 后均不得重用。

禁止项同样明确：不得把 `Runtime.RequestID`、`LoopContext.ID`、checkpoint run ID、semantic turn、文本或用户 ID 填入 `ConnectionID`。这些字段是运行/诊断标签，不是 transport identity；一旦作为 journal 键，就会把不同请求的重试语义错误合并。若 surface 没有该 host-private nonce，admission 必须拒绝，不存在兼容 fallback。

这只是 IM 的局部 journal 分区收口，不能被解读成 provider correlation 已完成：Coding dynamic 仍必须取得真实 `{Protocol, ConnectionID, ResponseID, ToolCallID}`，并完成 durable request-surface bind 后才可 materialize alias。

### 9.5 G3 基础实现复核（2026-08）

本轮已完成 G3 的**可验证底座**，但它仍是未接线的 host helper，不能改变当前产品的 fail-closed 状态：

1. `prepareAndPublishCodingDurableDynamicSurface` 严格沿用 `ToolCatalog → ToolPlanner → SQLiteSemanticExecutionCoordinator.PublishSurface → CatalogRenderer → PublishModelRequestSurface`；没有 Coding 专用 grant、journal 或 `byName` 执行权。
2. alias 使用每次请求独立的随机 `coding_dynamic_*` 值，仅保存在 durable `ModelRequestSurface`；`prepared` 状态及错误 ResponseID 都不能 resolve。
3. `BindResponse` 后才允许 resolve；retry 通过 `ReplaceModelRequestSurface` 原子淘汰 predecessor，并仅复用未消费 grant；`Cancel` 通过 `CancelRouteSurface` 在同一 coordinator transaction 内退休请求面、materialization 与 issued grant。
4. 发布前拒绝 identity/catalog/transport correlation 不完整、空 ready closure，及含 `Plan.Unmet` 的 partial plan。这里不能为了“部分可用”暴露 alias。
5. 该 helper 尚未接到 `CodingSubAgent` / `RemoteCodingSubAgent` 的模型请求回调；虽然已有 G4 fixed bridge host helper，`codingDynamicAliasesMayMaterialize()` 仍必须继续为 `false`，且动态 definitions 不能进入真实模型请求。

现有定向回归覆盖 prepared、错误 response、retry/supersede、cancel、缺 correlation 和 partial plan；下一步只有在 R1 认证 ingress 与真实 provider correlation adapter 就绪后，才能把此 G3 底座接到 local/remote request lifecycle。

### 9.6 G4 fixed bridge 基础实现复核（2026-08）

本轮增加了未接线的 `codingDurableDynamicSurface.ExecuteBoundSelection`。它的输入是 trusted `{Protocol, ConnectionID, ResponseID, ToolCallID}`、当前 surface epoch、opaque alias 与业务参数；没有 Skill 名、MCP server/tool、项目路径、run ID 或 provider selector。执行顺序固定为：

```text
ResolveAlias -> issuer.Validate -> canonicalize authorized args
  -> coordinator Admit / Reject -> refresh inventory + bound catalog execute
  -> Complete / receipt settlement
```

其中 canonicalization 通过 `DynamicSemanticCatalog.CanonicalizeSelectionArguments` 导出为受限 API，避免 GUI 读取 catalog 私有 schema/binding 或复制 name fallback。对 malformed arguments 仍走 `Reject`，因此同一 host-call identity 的重传返回 durable journal 结果，而参数摘要改变返回 `host_call_conflict`；缺 response bind、tool-call ID 或 transport identity 一律 `stale_surface`。执行前会重新观察 contract-backed inventory，binding 漂移或目录不可用只能完成为 rejection/unknown，绝不按 alias 或候选名称查找替代 provider。

该 bridge 尚未接到 local/remote callback，尚未让模型看到 alias；它只完成 G4 的可验证闭环底座，不能作为 R4 release gate 的完成证明。

## 10. 第四次复审：R1 不是“补一个字段”，而是补齐受认证的任务关系

### 10.1 本次核对结果

R1 的描述方向正确，但目前还缺少可以直接交付给实现者的**宿主边界契约**。现有 `RuntimeContext` 不能充当该契约：它由 `IMUserMessage` 组装，`ConversationID` 当前取自 `UserID`，`SessionKey` 由 channel/provider/user/actor 拼接，`Actor` 多数情况下是 `main-ai`。这些字段可用于 UI 路由、审计或兼容锁键，却不能证明“哪个已认证主体在某个独立会话中创建/续接了哪项 coding task”。

同样，`codingruntime.TaskID/AttemptID`、`LoopContext.ID`、SSH session、工作目录和请求文本都只能证明一次运行或传输存在，不能证明它属于某个语义任务。因此，不能在 `RunTaskWithSubAgent`、`RemoteCodingSubAgent.ExecuteTask` 或 `onStart` 中把现有字符串重新命名后填入五元 identity；那会把并发窗口、重试和远端连接错误合并为同一授权域。

结论：当前 P0 fail-closed 是正确状态；R1 的首个代码交付物应是“受认证的 task relation 服务”，而不是为 `setVerifiedCodingInvocationIdentity` 再增加一个公开调用点。

### 10.1.1 复审更新：Desktop Wails 已形成首个受限生产 ingress

此处的“缺少宿主契约”已对 **Wails desktop pure Coding workbench** 完成首个受限实现：app-owned SQLite `codingTaskRelationService` 持久化 opaque `VerifiedCodingTaskHandle`，desktop host 用固定 tenant/principal 与随机、独立的 host session 创建 subject；UI owner/path 只用来选择本地 host 保存的 session record，绝不写入 identity 的 tenant/principal/session/root 字段。`runAIAssistantMessageAsyncForUser` 仅在创建/恢复后已被识别为 pure coding workbench 的请求边界签发 in-process、`json:"-"` 的短时 token，agent 只能拿 `{token, owner}` 向 App 换取 relation；token 在换取前原子消费，避免一个请求因 local/remote 分支并发而签发两个 turn。

这使 desktop 的首轮在 `PrepareLocalCodingEnvironment`/remote prepare 已 arm，或可从持久 coding task tag 恢复时，能够进入 R1 binding；同 owner 的后续 desktop workbench 请求通过 verified continuation 保持 root、取得新 turn。取消/clear 撤销 active descendants；进程重启不以路径恢复 memory-only desktop session，而是重新认证、重新建立 root。非 desktop 的 IM、workflow、ACP、远端 transport 本身仍没有独立认证 session/task handle，继续 fail-closed。该事实只完成 R1 production ingress，**不**改变 R2--R5、transport-correlation 前置或动态 alias 的总开关。

复审还补齐了 cancellation 闭环：project close/hide/delete/archive 即使没有活跃 loop，也必须先撤销 task relation；clear/cancel 要先解析实际 desktop owner，再撤销其 relation，不能以空的 legacy session key 调用 revoke。`ClearAIAssistantHistoryForSession` 即使随后为清理会初始化 IM runtime，撤销仍必须发生在该初始化之前；否则未启动 handler 的旧 relation 会漏过 fencing。该规则避免“取消只是内存 signal、迟到 attempt 仍能 bind”的 TOCTOU。

### 10.2 建议的最小宿主契约

认证/任务服务应拥有以下不可由模型、agent 或 transport 构造的操作。接口名称仅示意；关键是调用方与持久化边界，不是具体 Go 类型：

```go
// 仅认证 task/continuation 宿主可调用。
CreateCodingTask(authenticatedSubject, conversationSession, request) -> VerifiedCodingTaskHandle
VerifyCodingContinuation(authenticatedSubject, continuationHandle) -> VerifiedCodingTaskHandle

// 仅 runtime 启动适配器可调用；一次 attempt 至多绑定一个已验证 handle。
BindCodingAttempt(VerifiedCodingTaskHandle, RuntimeTaskID, RuntimeAttemptID) -> SemanticTaskAnchor

// 仅 child admission 使用；继承 scope/root，签发新的 turn，并记录 parent-child lineage。
IssueChildCodingTurn(VerifiedParentCapability, ChildAdmission) -> VerifiedCodingTaskHandle
```

`VerifiedCodingTaskHandle` 必须是服务端持久记录或可验证、可撤销的签名 capability；其载荷至少绑定：`TenantID`、全局 `PrincipalID`、服务端会话 `SessionID`、服务端创建的 `RootTaskID`、本次唯一 `TurnID`、状态/过期时间，以及 continuation 或 parent-child lineage。它不是允许前端/agent 直接提交五个字符串的 DTO。

`BindCodingAttempt` 应使用唯一约束或条件写入保证：一个 `{RuntimeTaskID, RuntimeAttemptID}` 只能绑定一个完整 identity；同一 runtime task 的 attempts 不能跨 tenant/principal/session/root；已取消、过期、superseded 或已消费的 handle 不可重新绑定。绑定成功后只允许 runtime 从 durable anchor 回读 identity；agent 不保留可跨运行复用的 claim。

### 10.3 接线顺序与权限边界

```text
认证主体 + 独立服务端 session
  -> 创建 task / 校验 continuation
  -> VerifiedCodingTaskHandle（持久化、可撤销）
  -> runtime 创建 fresh attempt
  -> BindCodingAttempt（条件写入 SemanticTaskAnchor）
  -> onStart 仅 resolve anchor
  -> catalog / plan / durable request surface
```

local 和 remote 只在最后两步不同；SSH session 是 remote transport audit，不能进入 handle scope。nested child 必须先经过 runtime child admission，再由 parent capability 签发新 `TurnID`；不能复制 parent 的 dynamic identity、request surface、grant 或 alias。

如果上游尚无独立的认证 session 或 coding-task handle，R1 必须先扩展上游 ingress/持久 task relation。此时的可用性降级是保持动态面为空，而不是以 `UserID`、`RuntimeContext.SessionKey` 或 runtime task ID 伪造兼容模式。

### 10.4 R3/R4 的第二个硬前置：真实 transport correlation

现有 agent loop 已提供 epoch、response bind 和 tool-call execution context 的扩展点，但 Coding callback 尚未提供由 provider/transport 生成的完整 `{Protocol, ConnectionID, ResponseID, ToolCallID}`。特别是 `ConnectionID` 不能由 `LoopContext.ID`、request ID 或模型参数派生；`ResponseID` 也不能从模型输出文本猜测。

因此在 R1 之后仍应先交付一个 provider-adapter capability matrix：每个可灰度 provider 明确声明能否稳定提供 request connection、response ID、tool call ID、stream cancel 及重传语义。任意字段或取消语义不满足的 provider，不渲染 Coding dynamic aliases；不能因其有 function call 格式就进入 R3/R4。此矩阵应成为灰度 allowlist 的输入，而不是运行时 best-effort fallback。

**本轮实现复核（2026-08）：** 已将该矩阵收敛为 host-owned `codingDynamicProviderCorrelationForConfig`，并明确当前所有 Coding loop adapter 都是**不合格**行：OpenAI chat HTTP/SSE、Anthropic HTTP/SSE 与 Responses HTTP/SSE 的解析器可以在 wire payload 带值时读取 `ResponseID` 和 `ToolCallID`，但用户可配置的兼容 endpoint 并不保证每个响应均含 provider-issued ID（legacy/content fallback 甚至可没有 call ID），也没有把稳定、transport-owned `ConnectionID` 交给 Coding callback；配置成 `responses-ws` 也仍经过当前 Responses HTTP 请求路径，不能把 WireAPI 标签当作已建立 WebSocket connection 的证明。因此它们不会 materialize 动态 alias。测试还确认 URL、model、provider display/ID 等描述性配置不能伪造合格 correlation。将来新增真实 adapter 时，必须同时验证并提供该连接标识、response/tool-call ID、cancel fence 与 replay semantics，并完成 `ToolSurfaceExecutionContextProvider + ToolSurfaceResponseBinder + durable surface/bridge` 的端到端接线；仅把 matrix 行改为 eligible 不构成授权。

### 10.4.1 本轮 P0 清理：prompt 也不能保留旧 gateway 的“幽灵能力”

模型面除了定义与 callback，还有系统 prompt。即使 `BuildTools` 不再发送 `manage_skill` / `call_mcp_tool`，若 full-workbench 或 nested preamble 继续宣称它们可用，模型仍会不断生成无法执行的旧调用；这不是安全旁路，却会把 fail-closed 误表现为工具故障并污染多轮重试。

现已移除这两个名字在 Coding preamble 中的可调用性声明，统一改为“**仅可调用本轮 tools 中实际出现的受管扩展函数/受限别名**”。同时删除已经无调用方的 `codingDynamicSurface -> executeBoundCodingSkill/MCP` compatibility alias dispatcher；旧 name selector 只在明确 host-maintenance helper 中保留，local/remote 的普通和 context-aware model callback 均先拒绝。回归要求：动态 alias 为空时，prompt、definitions、callback 三处都不可声称或执行旧 gateway；清空 `byName` 或删除该 helper 不应改变任何模型调用结果。

### 10.5 建议拆分为可合并的小切片

| 切片 | 允许改动 | 必须测试 | 明确禁止 |
| --- | --- | --- | --- |
| R1a task relation | 认证主体、独立 session、new/continuation handle、撤销/过期和审计表 | 跨 session、handle 重放、续接越权、重启 | 用 `UserID`/文本/路径构造 root 或 session |
| R1b attempt binding | runtime 创建链显式接收 verified handle，并条件写 anchor | local/remote 新 attempt、重复/冲突 bind、重启 read-back | 在 `onStart` 从 runtime 字段补 identity |
| R1c child lineage | child admission 后签发新 turn 并绑定 child attempt | parent cancel/supersede、child exit、跨 root child | 复制 parent alias/grant/identity 指针 |
| R2 catalog | contract inventory 进入通用 planner | contract/health/schema drift、删除 matched map | 用 BM25/name 作 binding fallback |
| R3/R4 adapter | 仅 correlation matrix 合格的 provider 接入 surface 和 bridge | prepared/response bind、重传/冲突、late call | 伪造 connection/response/tool-call identity |

Desktop 的 R1a--R1c 已可作为受限生产入口，但其跨库交接与显式新任务边界尚未收口，仍不应开始“动态工具恢复率”类指标或灰度；non-desktop 仍须先完成 R1a--R1c。当前唯一有意义的指标是拒绝原因、伪造尝试、anchor 冲突和未绑定 relation 的恢复率。R3/R4 后再观察 `catalog_incomplete`、`stale_surface`、binding drift、journal replay/conflict 与 cancel fencing，且按 verified tenant + task handle 固定分桶。

### 10.6 更新后的批准结论

批准继续沿 R2 → R3/R4 → R5 实施，并将 non-desktop ingress 保留在 R1 扩展队列。当前不批准把动态 alias 接回 Coding callback，也不批准任何“单用户桌面可放宽”的身份降级；单用户仅影响认证宿主如何签发独立 principal/session，不改变其必须独立存在的安全边界。

### 10.6.1 本轮 R1 边界补强：显式“新任务”必须撤销 relation 与未消费 ingress（2026-08）

复核 Desktop Wails 的首个 R1 production ingress 后，发现它已能为普通连续请求生成新 `TurnID`，也会在 cancel/clear 时撤销 relation；但 `StartNewTask` 原先只参与未完成会话/UI 历史分流，并没有成为 Coding relation 的 root 边界。结果是：用户明确开始新任务后，下一次 coding runtime 仍可把新请求作为旧 handle 的 verified continuation，继承旧 `RootTaskID`。这不是 alias 层的问题，而是认证宿主把“会话文字上的新任务”遗漏在 task relation 生命周期之外。

同时，旧 request 已签发但尚未兑换的 in-process ingress token 不含 generation。若 root fence 仅删除 session mapping 而不删除这些 token，迟到旧请求仍可能在 fence 后兑换一个新 relation，制造顺序倒置的 root。更隐蔽的是 token 已从 map 取出、正在等待 runtime binding 的 consume-to-bind 窗口：单纯删除 map entry 已来不及阻止该请求。不能用 RequestID、project path 或模型文本给 token 加隐式连续性；它们都不是 task handle。

修复在 Wails request boundary 增加 `beginDesktopCodingTaskIngressForRequest(owner, startNewTask)`：当且仅当宿主显式传入 `StartNewTask` 时，先撤销该 owner 的 active relation 与所有未消费 token，再签发本请求的 one-shot token。额外的 host-private ingress generation 随每个 fence 增长；token 消费时和 relation lock 内都会校验 generation，因此 consume-to-bind 窗口中的旧 token 也无法跨过 fence。新的兑换于是创建独立 `tenant/principal/session/root/turn`；普通请求仍只经已验证 continuation 取得同 root 的新 turn。`revokeDesktopCodingTaskRelation` 也统一推进 generation 并清理该 owner 的 pending token，使 cancel、clear、project close 与 explicit new task 使用同一 fence。

回归覆盖“先签发旧 token、再显式新任务、随后旧 token 迟到”的顺序，以及 token 已消费但仍未 binding 时发生 fence 的竞态：旧 token 均不得再兑换 relation；新 token 必须产生不同 root 和 session，旧 handle 的 continuation 必须返回 revoked。此修复只完成 Desktop R1 的生命周期完整性，**不**降低 R2--R5 门禁：当前 provider correlation 仍不合格，动态 alias 继续不 materialize。

### 10.7 复审补强：非受管 legacy 刷新也必须保持全量 replacement（2026-08）

复审发现，初始 legacy 路由虽然已可经过 `LegacyAdapterPlan`，但 injection 与 recovery 仍可能在工作流/渠道/轻量 profile 过滤之后直接使用原始 definitions；当其中出现未审查 host 名称时，旧逻辑会降级为 snapshot admission。这是“当前列表”重新成为授权来源的另一种形式，也会让恢复阶段与初始阶段拥有不同的工具面规则。

修正后，initial route、injection replacement 和 skill-failure recovery 都统一走 `renderClosedLegacyReplacementSurface`：先过滤上一请求遗留的 client definition，再要求全部 host definition 具有 live reviewed provision，并从空列表渲染 immutable `LegacyAdapterPlan`；任一 host definition 无 provision 或 renderer 出错时返回空静态面/可观测 `catalog_incomplete`，绝不回退原始列表。client definitions 仅在 host plan 渲染完成后根据本请求 `ClientToolContext` 重新 materialize，并保留其独立参数契约；同名 host 工具优先。

本轮也把实际由 ambient/group policy 注入的 `knowledge_search` 与 `current_datetime` 补入 reviewed legacy catalog，避免将真实、固定的 host tool 错误归类为动态豁免。该项只收紧 legacy replacement；不改变 managed semantic surface 的 grant 规则，也不为 Coding Skill/MCP 打开任何动态 alias。

### 10.8 复审补强：fresh ingress replacement 不是 identity reset，必须是 durable authority fence（2026-08）

复审确认了一个真实的并发缺口：`prepareIMLoopContext` 对 fresh ingress 只清空 private semantic identity 时，旧 shared-loop callback 仍可持有已 materialize 的 `semanticCallSurface`。`CancelC` 不可用于解决该问题，因为 LoopContext 的复用语义要求它对 replacement 保持可用；原来的 cancel hook 也只覆盖显式 loop cancellation。因此，“新 turn 不再能找到旧 surface”不等于“旧 provider response 不能再执行旧 surface”。

修正方案是将 turn replacement 建模为独立、host-private generation：surface 必须从同一 loop lock 原子采样 `{identity, generation}`，避免 identity 与 generation 被一次并发 replacement 混配；入口分类、catalog/planner 一律经 generation-bound `SemanticTurnContext`，并在发布 managed surface 后立即把 `CancelRouteSurface` 注册到该 generation；fresh ingress 先原子推进 generation、取消上一代尚未发布的分类/规划、脱离并执行上一代 fence，才重置 runtime/identity。发布和注册之间发生 race 时，注册发现 generation 已变，立即撤销刚发布 surface 并 fail-closed，绝不返回 definitions。`CancelRouteSurface` 继续是唯一 durable primitive，原子退休 request surface、materialization 与 issued grants；callback epoch 再提供 in-memory 的早期拒绝。

新增端到端回归在同一 LoopContext 上发布第一轮截图 grant，再以第二个 RequestID replacement：第一轮 grant 必须被 durable revoke，第二轮独立 surface 仍可发行/消费 grant。该方案不从 RequestID、文本、LoopID 或 runtime ID 派生授权，也不复用 CancelC；它只补齐“旧 authority 在新 ingress 开始前失效”的生命周期不变量。

### 10.9 复审补强：CodingSubAgent 的静态工具带仍是 C-2 旁路（2026-08）

本轮继续核对 `CodingSubAgent` 与 `RemoteCodingSubAgent` 后确认：它们的动态 Skill/MCP 已安全地 fail-closed，但静态工具面仍由各自的 `BuildTools`、task-kind/name filter、词法 research helper 和 name dispatcher 决定。尤其 local callback 会缓存已渲染的 `cachedTools`，remote callback 则独立拼接 SSH 工具；两者都没有以当前 immutable `ToolPlan` 作为每次模型请求唯一的 selection/materialization 来源。任务文字还能经 `codingTaskNeedsLocalization` / `codingTaskNeedsExternalResearch` 改变 web research definition，构成文本直接扩展工具面的遗留授权路径。

这条问题不应通过增大缓存、保存更多历史工具名或放开 Coding dynamic alias 修复。改进方案已单列在 `semantic-tool-routing-coding-subagent-remediation-zh.md`：先冻结静态 bypass，并把 local static coding capability 迁入通用 catalog/planner/surface/admission 链；remote workspace 作为独立 binding 分阶段迁移；control-plane 工具单独定义 revision/replay 语义；只有真实 transport correlation 和统一 lifecycle 完成后才宣称 Coding static surface 已迁移。R1--R5 未完成前，`codingDynamicAliasesMayMaterialize()` 继续为 false。

该方案的首个 S0 收口已落地：lean local CodingSubAgent 不再由 `codingTaskNeedsLocalization` / `codingTaskNeedsExternalResearch` 的词法结果追加 `web_search` / `web_fetch`。这些函数保留为 localization 的质量证据判断，不再作为工具 materialization 的授权；同一 lean posture 的普通本地问题与 version/SDK/third-party 措辞现获得相同静态面。local `cachedTools` 的跨请求 rendered-definition 缓存也已删除，`BuildTools` / `BuildToolsForModelRequest` 每次重建完整 surface；回归验证 host posture 改变后旧请求的写入 definitions 不会留在下一请求。full-environment、nested、remote 及 static provider 的 catalog/planner/admission 迁移仍未完成，不能把这一收口描述为 C-2 已完成。

### 10.10 复审修订：Coding static 迁移必须先有 project adapter，后有可执行 surface（2026-08）

对 C-2 方案作第二次复核后，补上一个不能以“复用已有 capability”一笔带过的硬前提：通用 IM 的 `semanticCallSurface` executor 使用 IM 主体/工作区解析，而 CodingSubAgent 的 local project 是独立的 host binding。若直接将 Coding selection 接到 `readTrustedFile`、`writeTrustedFile`、`inspectTrustedRepo` 或 `runTrustedBuildVerify`，即使 capability 名称相同，也可能把选择执行在错误 workspace；这会把安全迁移变成 cross-project confusion。

因此本轮批准的路径是三段式：先用 `CodingExecutionEnvelope` 做 catalog shadow plan；再提供 project-scoped local adapter 并测试 A/B workspace 不可兑换；最后仅在真实模型 transport 能提供可信 `{Protocol, ConnectionID, ResponseID, ToolCallID}` 时切入 `PublishSurface → Grant → Admit → Journal`。shadow plan 不可渲染 alias 或改变现有静态 dispatcher 的执行权；缺 correlation 的静态带只能明确标为 fenced compatibility，不得被称为 migrated。

第一批只允许 local read-only inspection family。`bash`/`ssh_bash` 是 generic command gateway，必须先拆成 reviewed verify contract；web research 需要独立网络/effect/artifact policy；`todo`、`goal`、`spawn`、`report_localization` 等属 control-plane，必须等待 S3 的 revision/replay/lineage 契约。每个 family cutover 时必须同时删掉 model definition、task-kind name filter 与 name dispatcher 三个 legacy 授权点，禁止出现“planner 已加、switch 还在”的双执行桥。完整的 inventory、阶段门禁与指标已更新到 `semantic-tool-routing-coding-subagent-remediation-zh.md` §9。

### 10.11 S1-A 已落地，但不得误报为 static cutover（2026-08）

local Desktop Coding ingress 现已能在认证 host boundary 签发一次性 opaque workspace handle，并随同一次性 Coding ingress token 原子消费。runtime attempt 从 durable task relation 恢复 verified identity 后，`CodingSubAgent` 以 identity、handle、posture 与 role 构造 local read-only shadow catalog/plan。handle 与其实际目录的解析留在 App 私有表中；workspace A/B 的 provider binding 不同，且不能跨 owner 解析。路径、RequestID、runtime task/attempt、LoopContext ID 和模型文本均没有成为 identity/binding 输入。

该切片只生成 `fs.read.local`、`repo.inspect.vcs` 的 `ToolPlan` 及 explain/unmet 结果：它不 materialize definitions/aliases，不签发 grant，不接 admission/journal，也不改动 static `BuildTools`/dispatcher。未验证 workspace 会得到 `catalog_incomplete`，不会把剩余 legacy definitions 当成成功。随后补入的 S1-B adapter 仅用于验证固定 workspace binding、canonical schema/参数与目录重验，仍不从模型 callback 可达；new-task/cancel 会撤销旧 workspace handle。故当前状态应标为 **S1-A shadow-planned + S1-B adapter-tested + compatibility belt fenced**；S1-C correlation-bound cutover 仍是明确前置条件。

### 10.12 第五次复审：S1-C 的 blocker 是 adapter 证明，不是 core loop 缺少 hook（2026-08）

复核 `RunLoop` 的真实链路后，结论更明确：core loop 已在每次模型请求边界重新渲染 tools、创建 `SurfaceEpoch`，并在解析 response 后将 provider `ResponseID` 交给 `ToolSurfaceResponseBinder`，随后把 provider tool-call ID 与执行上下文传给 `ExecuteToolCallWithContext`。`codingDurableDynamicSurface` 也已覆盖 publish、response bind、alias resolve、admission、journal、replace 与 cancel 的 durable primitives。因此 S1-C 不能以“给 Coding callbacks 补几个接口”为交付目标；它需要一个**实际 transport 生命周期由宿主拥有且可证明**的 adapter。

当前 OpenAI chat / Responses HTTP(SSE)、Anthropic HTTP(SSE) 仍全部不合格：兼容 endpoint 可能省略 response 或 call ID，HTTP/SSE adapter 没有向 Coding callback 交付稳定 transport-owned `ConnectionID`，且 transparent retry/fallback/reconnect 与 late-response 的可重放语义没有形成一个可审核的实例生命周期。`responses-ws` 配置标签仍走当前 HTTP path，不能被视为已有 WebSocket session。故没有证据支持把 current matrix 的任一行改成 eligible，也不应把 `SurfaceEpoch`、request ID 或 host 随机数偷换为 provider correlation。

批准的后续方案是：新增 future adapter 时先让它在发送前 reserve 实际 channel，得到运行中 transport session 产生的 `{Protocol, ConnectionID}`；随后才 publish request surface 并由同一 channel 发送。响应解析必须在第一条工具调用前提供 provider `{ResponseID, ToolCallID}` 并完成 durable bind；cancel/fresh replacement 必须先 durable retire surface/grant、再 cancel channel；retry/fallback/reconnect 必须创建新 channel/new epoch，未知效果只能进入 `Unknown`。adapter capability 必须由 reviewed adapter build 发布，而不是从用户可编辑 config 推导。

该 adapter 先只 cut over local read-only family，并通过 response bind、ID 缺失、alias/parameter drift、retry/fallback、cancel race、late response、A/B workspace 和 replay/unknown 的 conformance suite。通过后，同一切片删除该 family 的 definition、name filter、name dispatcher 三处 legacy 权限点；不合格 provider 继续使用 fenced compatibility belt。详见 `semantic-tool-routing-coding-subagent-remediation-zh.md` §9.6。

### 10.13 S0 compatibility belt 的执行面复审收口（2026-08）

复审确认“每轮重新 BuildTools”本身不足以保证工具面完整：如果 callback 的 execution switch 不验证调用名确实属于**当前** rendered list，迟到模型响应或模型臆造名称仍可能击中 legacy dispatcher。这会重新让 dispatcher 成为一个比 model surface 更大的隐式授权面。

已补入 local/remote 分离的 static compatibility inventory 和 request-local rendered-name fence。inventory 为当前每个可见 static 工具记录 capability、effect、workspace/transport 或 control-plane scope；surface 先经该闭合 inventory 过滤，再在 request renderer 处记录完整 name set。普通与 context-aware callback 均要求模型 name **精确**存在于该 set，不能把未 render 的历史别名/大小写变体先 canonicalize 成已 render 工具；因 posture replacement 撤掉的 write/SSH 名称、local/remote 跨 host 名称和 invented name 统一 fail closed 为 `static_surface_unavailable`。LongHorizon GUI/browser host tools 保持其单独的 frozen episode-policy owner，但也只能执行本次实际 render 的 definitions，不能从 policy 名称表直接跳入 dispatcher。这解决的是 legacy compatibility belt 内“呈现与执行不一致”的旁路，而不是 grant/admission 迁移；没有真实 provider correlation 时仍不签发静态 alias，S1-A/S1-B 的状态也不变。

### 10.14 第六次复审：S0 围栏无法撤销同名的迟到静态调用（2026-08）

复审发现文档必须补足一项关键限制：request-local rendered-name set 不是 request-bound executable authority。它只保存当前 callback 的名称集合；当前 Coding HTTP/SSE adapter 尚未向 callback 提供可证明的 `{Protocol, ConnectionID, ResponseID, ToolCallID}`。因此，若旧 response 与新 response 都有 `read_file`，旧 response 即使在 replacement 后迟到，仍能通过 name-set fence 并落入 legacy dispatcher。该围栏只能阻断已撤下名称、跨 host 名称和编造名称，**不能**阻断同名的 stale response，也不能提供幂等、journal 或 unknown-effect 语义。

这确认了 S1-C 的前置条件不是可选的“增强项”：不得以 `SurfaceEpoch`、iteration、request ID、runtime attempt、LoopContext ID 或 host 随机 call ID 代替 provider-issued correlation。这样的代换至多制造一个本地标签，无法把收包、response 解析、工具调用、cancel、retry/fallback/reconnect 绑定为同一可审计 transport 生命周期。

修正后的方案是分层承诺：S0 仅承诺 exposure/execution name parity；S1-A/B 仅承诺 shadow planning 与 fixed workspace adapter；只有 S1-C 的真实 adapter 在 response bind 前取得 provider IDs 后，才承诺 stale revoke、`ResolveAlias → Admit → Journal`、重放处理和 unknown outcome。S1-C 之前，任何 effectful static family 的风险收敛只能通过减少其兼容面、串行化/停用 replacement-retry，或暂时不暴露该 family 完成；不得将 S0 数据结构扩张为伪 grant store。

### 10.15 S0 补强：利用既有 SurfaceEpoch，但不误标为 correlation（2026-08）

`RunLoop` 已经会在每次模型请求前调用 `BeginToolSurfaceEpoch`，并把该值原样带到 `ExecuteToolCallWithContext`。此前 Coding callbacks 返回空字符串，白白留下了同一 live callback 内“surface 已替换、旧 execution context 后到”的窗口。现已为 local/remote compatibility callback 增加仅内存的 request-instance epoch：每次完整 render 后才能签发；render replacement 时先清除上一值；context-aware dispatch 只接受当前值。测试确认同名的旧 local `read_file` 与 remote `ssh_read_file` 不能到达 legacy dispatcher。

这个改动不改变 §10.14 的结论。epoch 是 host-local cancellation/replacement fence，不是 transport-owned identity：它不关联 provider response、不能防止 adapter 将旧 wire response 误贴为当前 context，也没有 durable replay/unknown 语义。因此不得据此解锁动态 alias、S1-C static cutover 或 effectful family；它只是把现有 core-loop 的本地 replacement 信号用作 compatibility defense in depth。

### 10.16 S0 可观测性收口：审计实际 rendered surface，而非缓存候选列表（2026-08）

设计要求的基线应记录“本次实际给模型的完整 surface”，不是 registry 候选、task 文本推断的工具集合或 cache 命中。现已在 local/remote `BuildToolsForModelRequest` 的最终 replacement 边界记录 `coding_static_surface` 审计项：host kind、单调 static revision、posture、排序后的 rendered tool names，以及 local S1-A shadow plan 已存在时的 opaque plan/catalog/reason 摘要。计划、遗漏和 unmet 原因均来自 `ToolPlan`，没有另行按名字再计算。

审计 payload 严禁包含 workspace handle/path、任务文本、参数、grant/alias、provider secret；回归同时验证 local implementation→inquiry replacement 的 write 名称消失，及 shadow plan audit 可关联但不泄漏 binding。这些记录是迁移对账与问题定位证据，不是授权缓存：写审计失败不会放宽、收缩或重算模型 surface，remote S2 尚未实现时明确以 `not_prepared` 报告，避免把 local shadow plan 错投到 SSH workspace。

### 10.17 复审修正：`not_prepared` 不能吞掉 catalog-incomplete（2026-08）

S1-A 的 planner 已规定：verified identity 存在但 workspace binding 缺失时，仍发布 incomplete catalog 并产生明确的 `catalog_incomplete` unmet need；这与“identity 尚未可验证、根本不能准备 shadow plan”的 `not_prepared` 是不同状态。复审发现 runtime caller 多加了 `binding.complete()` 前置判断，导致真实 workspace 失效被吞成后者，削弱迁移对账与故障定位。

已移除该 caller-side gate，并以一个只接受 host-populated subagent state 的 runtime bridge 收口：identity 是唯一启动前提；binding 不完整时被原样传给 planner。测试确认该路径产生零 selections、两个 `catalog_incomplete` unmet，且不会从 project/runtime/task 字段补造 binding；identity 缺失仍不得启动 planner。由此 audit 中 `not_prepared` 仅表示没有可验证 identity/尚未进入 S1-A，`catalog_incomplete` 才表示已进入 S1-A 但 catalog closure 不完整。

### 10.18 S0 对账补强：按 capability 比较 legacy 与 shadow，而非按工具名（2026-08）

仅记录 rendered names 还不足以解释迁移差异：legacy belt 可以用多个函数名表达同一能力，shadow plan 则用 selection 表达 provider/capability 决策。现将 observation 扩展为 capability-class set diff：`legacy_only_capabilities` 表示实际 compatibility surface 有、shadow 当前未选的能力；`shadow_only_capabilities` 表示 shadow 已选、实际 legacy surface 未呈现的能力。集合来自 closed inventory 和 `ToolPlan.Selection.FitProof.MatchedCapability`，不从模型文本、参数或名称猜测。

这让 `Glob`/`ripgrep`/`read_file` 对一个 `fs.read.local` selection 的多对一关系不产生伪差异，而静态 `write_file` 相对当前只读 shadow family 的超额暴露会被准确报告。diff 仍是只读审计证据，禁止接回 authorization、surface replacement 或 fallback；remote 尚无 S2 plan 时也不生成伪对账结果。

### 10.19 第七次复审：control-plane 的版本围栏不能借用 transport 身份（2026-08）

继续审计 Coding 的 `report_localization`、`todo_write`、goal 与 `spawn_coding_agent` 后，确认它们不属于可以直接迁入 `fs.read.local` 一类 provider catalog 的 I/O capability。它们修改或授权的是 agent/task 控制状态；若继续只由 callback-local map 和函数名 dispatcher 驱动，surface replacement 后的旧 localization evidence、迟到的 todo merge 或 child 回传就可能影响新一代状态。

修正采用两层、不可互换的边界：

1. **S3 compatibility control-plane revision。** 每个 live callback 在完整 static surface replacement 时推进本地 revision；`report_localization` evidence 记录该 revision，编辑 gate 只读取当前 revision evidence。`todo`/可模型写的 goal 使用 `{expected_revision, expected_version}` compare-and-apply，失配稳定拒绝为 `control_plane_stale`。无 surface 的 direct-host/test 路径可保留既有业务校验，但其 state 不自动成为首次模型 surface 的授权依据。
2. **后续 durable owner/journal。** 该 revision 既不是 `ConnectionID`、`ResponseID`、`ToolCallID`，也不是跨进程任务版本。当前 HTTP/SSE adapter 仍无真实 correlation 时，不承诺 tool-call replay 幂等；跨重启 todo/goal 和可审计重放必须由 task owner 的 durable CAS 与合格 adapter 的 HostCall journal 另行实现。

spawn 的最小谱系合同也随之明确：只有 verified、仍在运行的 parent attempt 才可申请 child attempt/turn；child 必须重新解析自身 trusted identity、重新渲染自身 surface，绝不复制 parent epoch、surface、grant、alias、todo 或 localization state。worker 的 worktree/write-set 与只读 child 的 ledger admission 继续是独立执行约束；child 只返回 bounded report，不能直接回写已结束的 parent callback。

这一切片的验收包括 local/remote 的 localization replacement、todo 迟到 replace/merge/clear、direct-host 隔离、spawn no-inheritance 与 parent-ended rejection。完成时仅可标为 **S3 callback-local revision/CAS compatibility fence**；不得以此宣称 control-plane 已 semantic cutover，亦不得放宽 dynamic Skill/MCP 的 fail-closed 状态。详细状态模型、API schema 和测试矩阵见 `semantic-tool-routing-coding-subagent-remediation-zh.md` §9.8。

本轮已实现该 fence 的第一段：local/remote rendered static surface 推进 callback-local control-plane revision；localization evidence 仅在同 revision 的 existing-file edit gate 有效；todo definition 给出本轮 `{expected_revision, expected_version}`，执行采用 compare-and-apply，replacement 使旧 token 返回 `control_plane_stale`。由于 goal durable owner/CAS 未完成，已从 local/remote Coding model surface 移除，保留 host/orchestrator API。回归覆盖 local/remote stale replacement 与 evidence invalidation，以及 child callback 从空 state 开始。它不提供跨重启 CAS、tool-call replay 或 wire-level stale-response 防护；transport-correlated journal 与未来 goal 的 durable CAS 仍未完成。

### 10.20 第八次复审：spawn 谱系不能通过共享 `LoopContext` 实现（2026-08）

复核 `newReadOnlyNestedCodingAgent`、`newReadOnlyNestedRemoteCodingAgent` 以及同步 `runNested*` 路径后，发现 §10.19 的“child 不复制 parent surface/control-plane state”只在 callback 对象层得到部分落实：构造器仍将 `parent.loopCtx` 传入 child。该对象携带的不只是取消通知，还包括 parent loop/request trace、一次性 coding ingress、attachments、workflow/preview 生命周期以及可用于查询粘性权限的 UI owner。因而这是一条更底层的 parent-state alias，能够绕开“未复制 epoch/todo/localization”的窄测试。

设计修正是把 child 的四类输入解耦：

1. **runtime cancellation**：detached child 只接受 runtime Attempt context；同步 child 获得 host 建立的单向 cancel bridge。两者均不保存 parent `LoopContext`。
2. **diagnostic trace**：child 使用 fresh host-private diagnostic value；parent link 只能作为审计父边，不能复用 parent loop/request ID，也不能参与 semantic identity、provider binding、grant、scheduler owner 或 correlation。
3. **authenticated lineage**：child 在 admission 后由 verified relation 获得新 turn；嵌套路径绝不读取/消费 parent `CodingTaskIngressToken`，也不复制 parent verified/dynamic identity。
4. **UI/approval**：progress 是显式非授权 callback；effectful child 的 approval 由 host 重新签发并限制到 isolate/write-set，不能借 copied `UserID`、scope/high-risk state 或 preview/workflow context 继承。

这要求在 S3 的后续切片中引入 restricted child execution envelope/factory，并同时改 local、remote、同步、detached 四条 constructor/execution 路径。验收除了既有 child callback 零状态外，还必须证明 parent cancel 对同步/脱离 child 的不同语义、root ingress 不可由 child 读取、child trace 不携带 parent request/loop ID、以及 child 不复用粘性审批。该改动仍只是谱系与取消边界收口；没有真实 transport-owned correlation 时，不得把 fresh child diagnostic ID 或 callback revision 宣称为 replay/journal 防护，也不得放开动态 Skill/MCP。

首个实现切片已将 local/remote read-only 与同步 nested 构造器改为 fresh restricted child context，移除 parent `LoopContext`、local scope approval 与 remote high-risk approval 的直接复用；同步执行使用可释放的单向 cancel bridge，detached child 保持 runtime Attempt-owned cancellation。远程 root ingress 的读取现限制为 `nestDepth == 0`，防止 nested child 再次消费 `CodingTaskIngressToken`。定向回归覆盖 context 字段、审批隔离与两类取消语义。child-scoped effect approval、durable journal 和真实 correlation 仍未完成，不能将本项标为 S3 semantic cutover。

为防止“去掉继承”退化为 worker 无边界写入，同步 worker 已各自获得 fresh approval state，并只预批准 host 创建的 local/remote isolate root；parent 的 full-access、目录批准与高风险决定均不复制。非 isolate 路径和高风险命令继续 fail-closed，等待后续 durable child-scoped approval preflight。

read-only `ExecuteReadOnlyChild` 的 shallow-copy 路径也已收紧：在 admission helper 内先清空 parent dynamic/verified identity、relation handle 与 local shadow binding，仅用 fresh child Attempt 再签发/解析 child turn。若无法形成 child mapping，不提供 dynamic surface；这避免“构造器干净、适配器复制时又恢复 parent state”的旁路。

fresh local child identity 允许重新做只读 shadow-plan 对账，但不允许继承 workspace binding：缺 binding 必须显式为 `catalog_incomplete`，绝不可从 parent workspace、项目路径或 runtime identifier 补造。该状态不打开 alias/grant/static dispatch。

### 10.21 第九次复审：无 correlation 的 static compatibility 必须有“模糊投递即终止”策略（2026-08）

本轮沿 `corelib/agent.RunLoop` 的实际时序复核后，确认剩余问题不只是“epoch 还不够强”，而是当前兼容路径把一次 HTTP/SSE 调用的**不确定投递**当成了可立即替换的普通失败：每个请求在发送前重建 definitions 并创建 `SurfaceEpoch`；fallback callback 与 retry 都会再次重建 definitions/epoch；响应返回后才把 `ResponseID` 交给 binder。对于合格的受管 adapter，这一时序可由 channel、response bind 和 journal 闭合；对于当前 Coding HTTP/SSE compatibility belt，缺少 transport-owned connection/response/call identity，因而不能证明先前请求没有在超时、取消、断流或 fallback 后仍产生迟到 tool call。

`SurfaceEpoch` 仍应保留：它能拒绝 core loop **确实带着旧本地 context** 的调用。但它不能证明 adapter 没有把旧 wire response 归入新的 request context；同名 `read_file`、`write_file`、`ssh_read_file` 或 `ssh_bash` 于是仍可能命中当前 name-set + legacy dispatcher。把 epoch、iteration、runtime attempt、request ID 或新随机数升级为 correlation，会掩盖而非解决这个事实。

因此在 S1-C 之前应新增一个独立的 **S0.5 ambiguous-delivery containment** 切片。它不是 semantic migration，也不尝试伪造 journal；目标仅是让“已经离开进程、但结果无法被可信归属”的模型请求不能无声地产生一个可执行 successor surface。

| 事件 | 无 correlation compatibility 的强制处置 | 明确禁止 |
| --- | --- | --- |
| 请求正常完成且 callback 已按同一内存 context 消费响应 | 可继续现有 S0 name-set/epoch compatibility；仍只声明 local fence | 将其标为 response-bound 或 journal-safe |
| 请求开始后发生 timeout、cancel、stream decode failure、connection reset、fallback 前失败，或 host 无法证明“请求从未送达” | 标记本 callback 的本轮 static surface 为 `ambiguous_delivery`；原子撤下 name set/epoch，停止该 Coding turn 的 tool-enabled retry/fallback，并向模型/用户返回可重试的受限失败 | 重新 render 同名静态工具后继续发送；把失败当作“无请求发生” |
| 仅在发送前、本地明确拒绝且可证明字节未离开 host 的失败 | 可 abandon prepared local surface 后重试 | 用 HTTP 状态猜测未送达，或由 provider 文本自行证明 |
| 新任务/steering/cancel 与旧请求竞争 | 先进入/保持 `ambiguous_delivery` 或完成受管 cancel；旧 compatibility callback 不得借 successor surface 继续执行 | `retire -> render successor` 的两次本地操作替代 durable lifecycle |

这里的“停止”特指**带 static compatibility definitions 的当前 Coding turn**，不是删除 host-maintenance/test 的显式 direct-host 入口，也不是把任意文本回答都视为副作用。若产品必须保持可用性，安全替代是结束该 agent turn 并由用户/任务 owner 发起新的、边界清晰的尝试；不能让同一回调在不确定旧请求之后自动 retry、fallback 或 light-upgrade 成一个新的可执行静态面。effectful family（写入、命令、远程执行、spawn）应优先从该 compatibility 模式收缩；read-only family 即使暂时保留，也只能得到这一较弱的 containment，不获得 S1-C 的 stale/replay 承诺。

为避免把这一策略停留在调用惯例，core loop 需要一个仅反映**本地观测到的 transport attempt 状态**的可选 lifecycle hook（名称示意）：

```go
type ToolSurfaceAttemptObserver interface {
    OnToolSurfaceAttemptStarted(execution ToolCallExecutionContext)
    OnToolSurfaceAttemptFinished(execution ToolCallExecutionContext, delivery ToolSurfaceDeliveryState)
}

type ToolSurfaceDeliveryState string
const (
    ToolSurfaceNotSent           ToolSurfaceDeliveryState = "not_sent"
    ToolSurfaceResponseConsumed  ToolSurfaceDeliveryState = "response_consumed"
    ToolSurfaceAmbiguousDelivery ToolSurfaceDeliveryState = "ambiguous_delivery"
)
```

hook 必须由实际请求发送位置调用：`Started` 在即将使用本次 definitions 发出字节时记录；仅当 response 已被本轮正常消费才报告 `response_consumed`；任何已开始后的 error/cancel/retry/fallback/reconnect 都默认 `ambiguous_delivery`，除非 adapter 给出可审计的 `not_sent` 证明。它不接受模型数据填充，也不产生 `ConnectionID`、`ResponseID`、`ToolCallID`、grant 或 journal key。后续 S1-C adapter 应直接以其真实 lifecycle 取代这个 containment hook，而非把其状态升级成 durable correlation。

新增验收必须覆盖 local/remote parity：已开始请求后的 timeout/cancel/fallback/retry 不得生成 second executable compatibility surface；同名旧调用在 quarantine 后不能到 dispatcher；发送前失败可安全重试；effectful inventory 在 ambiguous 模式下零暴露；以及 observer 的 `not_sent` 不能由 URL、HTTP 文本、loop/request ID 或模型响应伪造。只有这组测试通过，S0 的发布说明才能从“仅有 name parity”提升为“包含不确定投递的 fail-closed containment”；仍不得称为 static semantic cutover。

首个实现切片现已接入 shared loop：`ToolSurfaceAttemptObserver` 仅观测 `started`、`response_consumed` 与 `ambiguous_delivery`；Coding local/remote callback 选择 containment 后，stream fallback、outer retry 以及 started request 的异常都会先清空当前 epoch/name set，并拒绝在同一 callback render successor static surface。定向回归覆盖成功消费记录、ambiguous HTTP failure 无第二请求、以及 local/remote quarantine 后同名 read tool 无法进入 legacy dispatcher。`not_sent` 的真实 transport 证明、effectful family 在正常 compatibility 模式的进一步下线、以及 cancel/steer 的端到端 fault injection 仍未完成；本项只能标记为 **S0.5 的首个 lifecycle containment 切片**。

随后已将“正常（未出错）S0.5 static belt 仍暴露 effectful 工具”的缺口收紧：rendered local surface 不再包含 `edit_*`、`write_file`、`bash`、`download_file`；remote surface 不再包含 `ssh_write_file`、`ssh_edit_file`、`ssh_bash`、`download_file`。实现以 closed inventory 的 effect class 为唯一策略输入：非 control-plane 且非 `read_only` 的条目在模型 request render 前移除；LongHorizon `computer_*`/`browser_*` 保持由其 frozen episode policy 独立管理。测试同时断言被移除名称不能命中 dispatcher。S3 control-plane 仍仅是 callback-local fence，并未因为本次筛选而获得 transport journal 语义。

复审还识别出 control-plane 中不能一概保留的例外：`spawn_coding_agent` 虽标记为 parent-lineage control plane，却会创建 child runtime attempt、占用 isolate/approval 容量，并可能启动 effectful worker；它不是 `todo_write` 那类可由 revision/version CAS 拒绝迟到写入的 callback-local mutation。因此也已从无 correlation 的 local/remote request surface 移除，直接模型调用同样在 dispatcher 前拒绝。只有 `report_localization` / `todo_write` 等已有各自 S3 revision/CAS 与 lineage 约束的 control-plane 状态继续保留；这不是对 spawn 的授权降级，而是等待将来的 response-correlated child admission + journal cutover。

最后的 S3 audit 发现 `report_localization` 也不满足该保留条件：它只在写入时读取当前 callback revision，却不要求模型带回 render 时的 `{expected_revision, expected_version}`；若同一 surface 中出现迟到的重复报告，仍可覆盖较新的 evidence。故该工具也已从无 correlation static surface 移除，保留 host/internal 调用与既有 replacement-invalidation 测试，但不再可被模型通过 legacy dispatcher 触达。当前该 belt 中唯一保留的 mutation 是 `todo_write`：其 schema 明示 expected revision/version，执行路径 compare-and-apply，第二次或过期 payload 返回 `control_plane_stale`。这依然不等于 durable replay，但至少不会把同一 callback 的 stale payload 静默写为当前状态。

### 10.22 第十次复审：prompt 与轨迹元数据必须和实际 request surface 同构（2026-08）

收缩 `BuildToolsForModelRequest` 后仍存在一个会反复制造“工具不完整”表象的根本缺口：`BuildSystemPrompt`、task user message 和 subagent trajectory 的启动快照仍从较宽的 direct-host `BuildTools`/full-workbench 模板取材。于是同一轮模型虽然只收到 read-only + `todo_write` 的无 correlation surface，却被 prompt 要求调用 `write_file`/`edit_file`/`bash`、`ssh_*` 写入或命令、`report_localization`、`spawn_coding_agent`。这不是授权绕过，但会稳定诱导不可达调用、错误恢复重试和训练/审计中的假能力记录。

本轮将当前 HTTP/SSE compatibility 模式显式建模为**transport posture**，而非由 task 文本、项目路径、callback ID 或模型名推导：local 非 Horizon 与全部 remote Coding callback 均使用受限 prompt；Horizon 继续由其 frozen episode owner 独立控制。受限 prompt 和对应 task checklist 只指导本轮已经可见的只读工具；实现/运行请求明确转为“收集证据 + 说明 safe-mode blocker + 输出供 correlation-bound workflow 执行的最小下一步”。只有 definition 中实际列出时才可使用 `todo_write`，且必须原样携带 `expected_revision` / `expected_version`；陈旧 payload 必须重新读取状态，不能原样重试。

trajectory 启动快照也改用同一 effect filter，但**不得**为记录而调用 `BuildToolsForModelRequest`：后者推进 callback-local revision/rendered-name fence，只有真实 provider request 边界可拥有该副作用。快照仅作审计 metadata，复用 `BuildTools` 后施加无 correlation filter，不创建 epoch、grant 或 correlation。新增 local/remote negative regression 同时断言 prompt 与 snapshot 不含 write/command/localization/spawn 的幽灵名称，并保留可用 read-only/`todo_write` 指引。

这项一致性修复不恢复任何 capability，也不把 prompt/trajectory snapshot 变成 authorization；可执行权仍只来自真实 request renderer 的 definitions、rendered-name fence，以及未来 S1-C 的 response-bound `Publish → Bind → Admit → Journal`。

### 10.23 第十一次复审：遥测也必须逐请求绑定实际 request surface（2026-08）

复审发现仍有一条容易被忽略的 ghost-surface：`LoopInputBreakdown` 最初在 request renderer 之前读取 `BuildTools` 的宽 compatibility inventory。模型实际收到的是 `BuildToolsForModelRequest` 刚刚生成、可能已删去 legacy 条目或已换成 request-local alias 的 definitions，因此 token/cost telemetry 与模型可见工具面不一致。该偏差会污染容量评估、工具面收敛指标和事故审计；它不是授权漏洞，却会让“工具是否完整、是否被裁剪”的诊断依据失真。

修正原则是：每一次**真实离开 host 的模型请求**各自记录一次，并且记录的 conversation 与 definitions 必须就是该次 transport 即将发送的完整 request surface。初始 streaming request、streaming failure 后实际发出的 non-stream fallback、以及 outer transient retry 都必须在其 request-local render 之后、发送之前分别调用 breakdown observer。特别是 MoA fan-out 后，聚合请求使用的是注入 private advice 的 `reqConversation`，遥测也必须读取这一最终消息快照，不能回退原始 `conversation`。不得为遥测额外调用 renderer 或缓存其结果：renderer 可推进 revision、name-set 或 epoch，观测只能消费已经为该请求生成的 definitions。没有实际发送的 backoff、被 containment 阻断的 fallback/retry、以及 trajectory snapshot 都不得计入 request telemetry。

新增回归应断言：（1）宽 `BuildTools` 与窄 renderer surface 时，工具 token 只按窄面统计；（2）stream + fallback 产生两个请求、两个 breakdown，且两者均对齐各自 sent definitions；（3）retry 亦满足同一规则；（4）任何 observer 失败或记录缺失都不能改变执行授权、delivery state、grant 或 correlation。该项的完成状态仅为 **request-local observability parity**，不补齐 S1-C correlation，也不能用于放宽 dynamic Skill/MCP 的 fail-closed 策略。

### 10.24 第十二次复审：Responses SSE 必须保留 provider response ID（2026-08）

继续审计 S1-C 前置条件时发现：non-stream Responses body 已将 top-level `id` 写入 `llm.Response.ResponseID`，但 streaming Responses parser 在 `response.created` / `response.completed` 的 `response.id` 中收到同样的 provider-issued ID 后却丢弃了它。结果是 shared loop 虽然会把 `resp.ResponseID` 交给 `ToolSurfaceResponseBinder`，Responses SSE 路径却永远传入空值；这会把“缺少 correlation”与“解析层丢失已有 provider evidence”混为一谈，使后续合格 adapter 无法正确 fail-closed 或验收。

现已在 Responses SSE 聚合器中提取 event envelope 的 `response.id`，写回最终（以及带可见 partial 的）`llm.Response`；若同一 SSE stream 出现两个不同的非空 provider response ID，则直接报错，禁止把一个 stream 的 tool calls 绑定到不确定 response。此处不接受 payload 中的 request/loop/工具参数字段，也不生成 local replacement ID。回归覆盖 `response.completed` 的 ID 保留与冲突 ID 拒绝。

该补洞只修复 provider evidence 的完整传递：它**不**证明现有 HTTP/SSE adapter 拥有 stable `ConnectionID`、不会消除 transparent retry/reconnect/late-response 问题，也不让 Coding dynamic aliases 或 static effectful families 变得 eligible。S1-C 仍需要真实 adapter 将 `{Protocol, ConnectionID, ResponseID, ToolCallID}` 作为一个 transport-owned lifecycle 验证、绑定、admit 和 journal；在此前 dynamic Skill/MCP 继续 fail-closed。

### 10.25 第十三次复审：禁止 SDK 隐式重试绕过 request surface lifecycle（2026-08）

继续从“每条 telemetry/attempt 必须对应一条实际模型请求”反查后发现一个更深的旁路：`RunLoop` 已为 streaming fallback 与 outer retry 重新 render tools、记录 breakdown、签发 epoch 并触发 delivery observer，但 OpenAI-compatible SDK 内部仍可能在同一个 `DoOpenAIRequest*` 调用里因为 400 自动发起 `max_tokens`、tool-less 或 compact-message repair request。第二、第三个 HTTP 请求没有经过 loop 的 renderer、attempt observer、quarantine 与 telemetry，因此可能使用一张未重新授权的工具面；对无 correlation 的 Coding compatibility belt 尤其危险。

修正为 request owner 明示接管：新增 context-scoped `WithTransparentRequestRetriesDisabled`。Coding local/remote 的 `codingLoopLLMRequestContext` 启用该标记；OpenAI stream 与 non-stream SDK 路径遇到该标记时只发送当前完整 request 一次，并返回原始 provider failure，由 `RunLoop` 以已有的 ambiguous-delivery containment 决定是否结束或显式 retry。标记不由 URL/model/task/loop/request ID 推导，也不提供 `not_sent` 证明，更不把 400 当成可安全重放的依据；它只禁止隐藏 successor request。

同一 request-owner policy 还必须覆盖 HTTP 客户端的自动重定向：默认 `net/http` 在收到 307/308 时可以重放 POST body 到新 URL，这同样是绕过 renderer、attempt observer 和 telemetry 的隐式 successor。带禁用标记的请求使用 client 的副本并返回首个 redirect response（不跟随）；普通调用维持其既有 redirect 策略。该拦截只阻止本库代为创建第二条 HTTP 请求，不把 3xx 解释为未投递，也不把 redirect URL、request ID 或 trace 字段提升为 connection/response correlation。

普通非 Coding 调用不带此标记，兼容性 repair 与 redirect 行为保持不变。新增 regression 对比启用/未启用两种上下文：禁用时 OpenAI stream/non-stream 与 Responses stream 都不会产生 tool-less/compact/max-token 或 redirect successor；Coding context 必须携带该标记。此项收口保证 S0.5 的 request surface、observer 和 token telemetry 不会被 SDK 内部重试或 HTTP redirect 悄悄旁路，但不替代 provider correlation、connection lifecycle 或 durable journal。

同轮 SSE 还不得在 parser 内静默换绑 provider response ID。Responses parser 已拒绝不同的非空 `response.id`；现在 OpenAI chat SSE（包括 stream=true 但实际返回 SSE 的兼容非流式 parser）与 Anthropic message stream 同样首次看到非空 provider ID 后冻结，后续不同 ID 立即失败。这样不会把一个混合流中前半段的 tool call 绑定给后半段的 response；空 ID 仍是缺 correlation，不能由 parser 补造。本项只是 parser evidence 的 fail-closed 一致性，不能把 HTTP connection、callback epoch 或 3xx/timeout 变成 S1-C correlation。

### 10.26 第十四次复审：G3 durable surface 必须能在重启后仅凭持久化证据恢复（2026-08）

此前 `codingDurableDynamicSurface` 虽将 request alias、grant、route 和 host-call journal 写入同一个 coordinator，但 GUI helper 仍把 `plan`、`scope`、`protocol`、`connection`、`epoch`、alias map 和 rendered definitions 留在进程内。进程在 `response` 已可信绑定、tool call 尚未抵达 fixed bridge 的窗口崩溃后，新进程只能依赖旧对象才能继续处理。这会诱导实现者以 `matchedSkills`、`matchedMCPTools`、动态 `byName` map、项目路径或 runtime/loop ID 重建执行权；这些都是 discovery/cache 证据，不能替代 grant、route 或 transport correlation。

修正是让 coordinator 提供窄的 `RecoverBoundModelRequestSurface`：输入仅为认证 host 的 `TenantID` 与 transport-owned `{Protocol, ConnectionID, Epoch}`；它只接受同 tenant、`active`、已保存非空 `ResponseID`、且 route 仍 current 的 surface，并从 durable alias record 读取不可变 grant/scope。`prepared`（崩溃时未证明 request 已进入 response domain）、`finished`、`superseded`、`cancelled`、租户/route 不匹配均统一返回 `stale_surface`。恢复后每个 tool call 仍必须以 provider `ResponseID + ToolCallID` 经过 `ResolveModelRequestAlias → Validate → Admit → Journal`；恢复 API 不消费 grant，也不重发模型请求或 provider I/O。

GUI 的对应 G3 helper 只从该 durable record 重建最小 holder，并重新从 coordinator 读取 published plan；它刻意不恢复 rendered definitions、动态 catalog 或 in-memory alias dispatcher。动态 catalog 必须在 fixed bridge 的 provider I/O 前由 lifecycle-owned inventory 重新观察，缺失/漂移仍为 `catalog_incomplete`。回归覆盖 coordinator 关闭并重新打开后的 active surface 解析、错误 tenant、terminal surface，以及 prepared/superseded/cancelled surface 的拒绝。

这补的是 **G3 durability/recovery helper**，不是 S1-C：现有 Coding HTTP/SSE 仍没有真实 request lifecycle adapter 来生成并维护 `{Protocol, ConnectionID, ResponseID, ToolCallID}`，也没有将此 helper 接入 production Coding callbacks。因此 Skill/MCP dynamic alias 仍保持零 materialization / fail-closed；不得将持久化记录、epoch 或 parser ID 透传表述为 static semantic cutover 或动态能力已开放。

### 10.27 第十五次复审：将 S1-C 的 channel 生命周期写入 shared loop，而非留作回调约定（2026-08）

继续审计发现，`ToolSurfaceExecutionContextProvider` 只能在 callback 层填入 metadata；它没有约束 metadata 对应的 transport 是否真的发送了本次 definitions。即使 callback 未来能拿到 WebSocket/HTTP session，也仍可能出现“先 render/publish，再由别的 helper 或自动 fallback 发送”的分叉。这样又会把 `ConnectionID` 降格为一个看似可信的字符串。

现已在 shared `RunLoop` 增加可选的 `ToolSurfaceRequestChannelProvider` 与 `BoundModelRequestToolSurfaceRenderer`。合格 host 必须先 reserve 单次、transport-owned channel，RunLoop 验证其非空 `{Protocol, ConnectionID}` 与新 epoch，再把该完整 context 交给 bound renderer；随后**只**通过同一个 channel 发送一次请求。channel 明确禁止 transparent retry、redirect、reconnect、fallback；失败返回 loop 后也不在同一 channel 上建立 successor。下一请求必须重新 reserve channel、重新 render/publish surface。普通 HTTP/SSE compatibility host 不实现该接口，继续走原有 S0.5 containment，不会被伪装为 qualified adapter。

新增 loop conformance 覆盖证明同一个 reservation 的 connection/epoch 贯穿 render、response binder 和 tool execution，response ID 在 binder 前写入；第二轮得到新 connection/new epoch；channel 失败时零 fallback、零 outer retry、零 successor render。此 seam 是把 §9.6 所需的时序变为 core loop 可强制的结构性约束，仍未接入现有 Coding callbacks，也不自动证明任一 transport 合格。

同时补齐 `responses-ws` parser：从 `response.created` / `response.completed` envelope 保留 provider `response.id` 并拒绝同一 WebSocket stream 的冲突 ID。该值依旧只是 parser evidence；当前 websocket helper 每请求拨号且尚未作为 `ToolSurfaceRequestChannel` 接入 callback，故它不能单独成为 stable `ConnectionID`、取消/replay semantics 或 S1-C enablement 的依据。

随后已将 Responses WebSocket 实现抽成一个真实的单次 socket channel：连接 ID 只在 `DialContext` 成功后由 host 生成并与该 live socket 一同持有，任何 URL、model、provider ID/name、request/loop ID 都不能参与生成；`Do` 第二次调用稳定拒绝。当前它仅作为 reviewed transport primitive，Coding callback capability matrix 仍标为 `responses-ws-channel-available-not-wired`，因为尚未把 verified ingress、`PublishSurface/ModelRequestSurface`、response binder 和 fixed executor 全部接到该 channel。故此项仍不是 S1-C enablement，也不会打开 dynamic aliases。

### 10.28 第十六次复审：S1-C 必须以单一 adapter holder 闭合，而非分别“接几个接口”（2026-08）

`ToolSurfaceRequestChannel`、durable surface、response binder 与 fixed bridge 已分别具备后，新的主要风险不再是缺少接口，而是由 callback 在不同对象之间手工拼接它们。那会重新允许“channel A render、helper B send、binder C bind、旧 dispatcher D execute”的分叉；即使每个部件独立正确，组合仍无法证明同一调用链。

因此将 S1-C 下一步固定为一个仅代表一次 live reservation 的 adapter holder：它同时拥有 verified identity、channel 的 `{Protocol, ConnectionID}`、fresh epoch、durable surface、binder 和 context executor；所有 terminal event 都经 coordinator 原子退休 surface/materialization/grant。该 holder 首先只能作为 test-only fixture，不能直接使 local/remote callback 或 dynamic alias 开关 eligible。完整状态机、回归矩阵和 production release gate 见 [S1-C 生产就绪性复审与改进方案](semantic-tool-routing-s1c-production-readiness-review-zh.md)。

首个 `test-only` 实现现已落为 `codingBoundDynamicRequestAdapter`：它只接收已验证 identity、complete immutable plan/catalog 与 shared loop 传入的 reservation tuple；publisher 使用该 tuple 和 loop epoch 写 prepared surface，response binder 成功前没有 alias resolution，context executor 对 protocol/connection/epoch/response/tool-call ID 任一错配均在 catalog/provider I/O 前返回 `stale_surface`。空 response ID/bind 失败、显式 close 都经 `CancelRouteSurface` 退休 durable state；回归还锁定相同无效 call 的 journal replay。该对象尚未接到任何 production callback，故 `responses-ws-channel-available-not-wired` 和 dynamic alias 的 fail-closed release gate 不变。

随后 holder 已在测试中直接包装 live `responsesWSRequestChannel`：它以 channel 提供的 protocol/connection 作为发布的唯一允许 tuple，自身持有唯一 `Do` 入口，故 renderer 不可能在 socket A 上发布后经 helper B 发送。WS response 的 provider ID 被 bind 后才会进入 G4；同一 reservation 的第二次 exchange 被底层单次 channel 拒绝，close/cancel 后 coordinator 退休 predecessor surface 且 alias 无法解析。此处仍只是 C 阶段的 test-only transport conformance，不是 callback production wiring；matrix 行和 alias 开关均不变。

另新增独立于 transport matrix 的 production qualification registry。当前所有行（包括 `responses-ws`）固定为 `Wired=false`、`Enabled=false`；future factory 只有在这两个 host-owned 状态均为真时才会 reserve live channel，且它在该检查前不会读 catalog、生成 plan 或发布 surface。这样配置、测试 fixture 或调用者无法通过传入“qualified=true”把 test-only holder 升格为生产能力。local/remote callback 仍未注册 provider，动态 alias 继续零 materialization。

取消语义也已先在 holder 收口：`Close(non-nil cause)` 先取消 bridge context、再原子 `CancelRouteSurface`，并关闭 live channel；fixed bridge 在 catalog re-observe 前再次检查 context。此顺序避免 close 与刚获 admission 的执行并发时继续新建 provider I/O。当前覆盖的是 test-only holder；steering、timeout、nested exit 与 runtime completion 何时调用该入口仍是 production wiring 的前置，不得因这个局部闭环修改 matrix/alias gate。

为避免 future callback 分别发明 cancellation 行为，holder 还暴露封闭的 `CloseForLifecycle(steered | nested_exit | runtime_terminal)`。三类 host lifecycle 与未知值都只会收敛到同一 `Close → CancelRouteSurface` 路径，且不接受 task/transport/model/runtime 字符串作为 reason 来源。该 API 现仅有定向测试覆盖，local/remote callbacks 尚未接入，仍不可据此开放 alias。

已再抽取一个 test-only callback lifecycle relay，它一体实现 future 的 channel provider、bound renderer、binder 和 context executor，确保四个 shared-loop 扩展读取同一 active holder。relay 只接受 factory 交出的 live channel tuple；factory 若没有 transport-owned protocol/connection，relay 会关闭它且不留下 active state。由于 production qualification 仍固定 disabled，现有 callbacks 不创建 relay、不 reserve socket、不发布 alias；本项是接线形状约束，不是 enablement。

### 10.29 第十七次复审：request channel 的 socket close 不能代替 semantic surface disposition（2026-08）

复核 shared `RunLoop` 与 test-only holder 的组合后，发现一个尚未满足 production gate 的生命周期缺口：channel 在 `Do` 返回后可以立即 `Close(nil)` 来释放 socket，但此时 response 还可能在 binder 前被 steering、空 choices、解析失败、early return 或最终文本路径丢弃。socket 已关闭并不能说明 durable `ModelRequestSurface` 已完成 bind、工具 batch 已结算，或未消费 grant 已撤销。若将现有 relay 直接接进 callback，prepared/bound alias 会在这些分支中失去唯一终止者，正是多轮 successor 与迟到调用可能重新交叠的窗口。

修正已作为 shared-loop 的 `test-only` conformance seam 落地：host-owned、一次性的 `ToolSurfaceDispositionObserver` 接收 loop 已持有的 execution context，并只允许封闭状态：`response_abandoned`、`response_settled`、`tool_batch_settled`、`steered`、`runtime_terminal` 和 `transport_failure`。任一未 bind/未接受 response 必须 `response_abandoned`；无工具的最终 response 与已提交的工具 batch分别结算；steering、timeout/cancel、nested exit 和 runtime terminal 必须退休仍 active 的 holder。relay 是该 callback 的唯一 owner，并映射到同一 `CloseForLifecycle → CancelRouteSurface` 事务；不能让各个 local/remote return 分支自行清 map 或只关 channel。loop regressions 覆盖 empty response abandon、finalization-steer 的 predecessor/successor、tool batch settle 与 transport failure；relay regression 覆盖 exact tuple、wrong tuple 和重复通知。

同时明确 binder-failure sequencing：binder 可以先把 durable holder 退休，loop 仍必须以同一 reservation 发送 `response_abandoned`。relay 的 ownership clear 只验证 protocol/connection/epoch，不把“non-terminal”误作清理条件；随后 close 是幂等的。该特例只消除 terminal holder 阻塞 successor 的内存泄漏，绝不令已退休 holder 恢复 render、bind 或 dispatch。

对 loop 的所有 reservation 后 early-return/recovery 分支再做穷举，已补齐 truncation、invalid-tool-arguments 与空响应 iteration-limit 的 `response_abandoned`。这三个分支虽不执行 tool，但会保留 assistant/recovery context；因此不能把它们误归为 final settle。每个 reservation 仍只发送一次 disposition。

这一修正不改变生产行为：qualification 继续 `Wired=false/Enabled=false`，relay/holder 均保持 test-only，`codingDynamicAliasesMayMaterialize()==false`。只有将 disposition seam 接到真实 verified Coding callbacks，并证明每个 reservation 的 production early return、steer/cancel、nested exit 与 runtime terminal 都恰好结算一次，才可讨论 callback 注册。

为防止未来把 D/E 降格成两个 boolean，production qualification 已收敛为必需的 host-owned release-evidence object：除 correlation capability、`Wired` 与 `Enabled` 外，必须存在 reviewed adapter version、verified ingress 范围、disposition conformance version、catalog/receipt policy coverage、opaque fixed cohort 和 kill-switch proof。factory 与 alias gate 使用同一个 `eligible()`；当前 registry 不提供任何这些 evidence，因此即使 future caller 能取得 verified identity 或选择 Responses WS 配置，也不能 reserve、publish 或 materialize。

### 10.30 第十八次复审：callback 装配字段不等于 callback 接线（2026-08）

local `codingSubAgentCallbacks` 与 remote `remoteCodingCallbacks` 现都保留 `dynamicLifecycleRelay`，并在 callback 建立时调用同一个 `tryAttachQualifiedDynamicLifecycleRelay()`。此函数只使用已由认证 ingress 绑定的 trusted identity 与 app-owned qualification；当前 qualification disabled，故稳定返回 `nil`，不会 dial WS、读取 catalog、发布 surface 或生成 alias。

这是 `implemented-not-wired` 的装配边界，不是 production callback registration：后续不得分别打开这些接口或先启用 qualification；必须以单一 composition adapter 原子委托 request-channel provider、bound renderer、response binder、context executor 与 disposition observer，并在真实 verified ingress 的 cancellation、nested exit、steering 和 batch terminal 路径上证明每个 reservation 只结算一次。D1 已以 test-only shape 满足原子委托；完整 D0--D3 门禁见 [S1-C 生产就绪性复审与改进方案](semantic-tool-routing-s1c-production-readiness-review-zh.md#10-本次复审补强d-阶段必须原子切换-callback-composition)。

该项的 D1 **test-only composition shape** 已随即落地：两类 callback 现在同时实现并由同一 `dynamicLifecycleRelay` 委托这五个接口。relay 缺失时整套返回 inert 值，令 RunLoop 保持 S0.5；relay 存在时 context executor 也不再进入 static/name dispatcher。编译期 interface assertions 和 local/remote fake-channel 回归锁定这条 all-or-nothing 规则。

D2 同步补入了 execution-scoped lifecycle owner（仍为 disabled-path 的 test-only 形状）：它只引用已安装 relay，并把 `LoopContext` cancellation、detached runtime execution-context cancellation、同步 nested handoff 和 callback terminal defer 收敛到 `CloseForLifecycle`。owner 使用 exact relay ownership，因此旧 callback 的 terminal return 无法关闭一个 successor relay；它也不接收或推导 task/path/runtime/config/provider 的身份字段。定向回归覆盖上述 cancellation/handoff 和 predecessor/successor 隔离。由于 qualification 仍为 disabled，真实 callback 只能取得 nil relay，这仍不是 production enablement；真实 approved ingress 的 end-to-end exactly-once lifecycle evidence 仍是 D2 blocker。

### 10.31 第二十次复审：pure Coding steer 必须走 RunLoop 的 revision 协议（2026-08-23）

复审时发现 local/remote pure Coding callback 虽有 `codingSubAgentHooks.TransformConversation` 注入 pending guide，却没有实现 `LLMReplanAware` 或 `LLMFinalizationGuard`，hooks 也不保存 `LoopContext.ReplanRevision()` 的消费水位。这使 `RequestReplan()` 无法可靠取消当前 Coding request、触发 shared loop 的 `ToolSurfaceSteered`，或阻止 stale final text 与已接受 steer 并发提交；未来 relay 非 nil 时会进一步令已 reserve surface 缺失唯一终态。

修正已落地：callback 保存原子 observed revision；`TransformConversation` 在 drain 前快照并只提交该快照，避免吞掉 drain 期间新到的 revision（空 payload 的 accepted steer 也在此边界消费）；`LLMRequestContext` 在 scheduler 等待前安装可取消 operation；`LLMReplanRequested` 只调用 `ReplanRequestedSince`；`TryFinalizeLLMResponse` 只调用 `TrySealReplans`。holder 不得从这些 callback 直接 close，而必须由 `RunLoop` 产生唯一 `steered` disposition 并由 relay retire。真实 `codingagent.Run` 已覆盖 local/remote live steer cancellation、successor conversation、finalization 拒绝和 watermark。

这完成 D2a，且 D2b 已补首个 real-holder callback conformance：local/remote 均以 exact `{Protocol, ConnectionID, SurfaceEpoch}` 将 `steered` disposition 转交同一 relay，reservation ledger 证明 predecessor 只有一个 terminal，alias 被退休为 `stale_surface`，且迟到/重复 predecessor disposition 不会触及 successor。该测试仍是 qualification-disabled 的 test-only fixture，不覆盖 tool-batch durability、runtime/nested/route terminal 的全组合；D2c 的完成也不等于 D3 的 production cohort。详细验收见 [S1-C 生产就绪性复审与改进方案](semantic-tool-routing-s1c-production-readiness-review-zh.md#12-本轮改进方案以-reservation-ledger-完成-d2bd2c)。

### 10.32 第二十一次复审：工具面完整性是 request ownership 性质（2026-08-23）

“多轮后工具缺失”此前容易被描述成 cache invalidation 问题，但这种归因不足。缓存最多是泄漏的载体；真正必须保持的是：每一次实际发送的模型 request 都拥有一个完整 replacement surface，且该 surface 只能由同一 live reservation render、bind、execute、settle/retire。只要 callback 能从上一轮 definitions、alias/name map、任务文本或 SDK 私有 successor 拼接下一轮工具面，就仍会在 replan、retry、cancel 与 batch failure 下出现缺失、幽灵名称或跨轮执行。

故 architecture contract 收敛为：

1. `ToolPlan` 是唯一的 capability selection 来源；continuity 只持久化事实/工件/已结算结果，不持久化 executable definition、alias、grant 或 transport tuple。
2. `RunLoop` 在每个真实 request 前完成 reservation、fresh epoch 与 complete render；同一 channel 禁止 retry/redirect/fallback，任何 successor 重新 reserve/re-render。
3. binder、context executor 与 disposition observer 必须由一个 relay/holder 原子委托；context executor 不得回落 static/name dispatcher。
4. 响应含工具时，只有 complete paired batch 经 durability commit 才可产生 `tool_batch_settled`；任何 starter/committer failure、steer、cancel、nested/route terminal 都必须走唯一非 settled disposition。

这项定义将“工具完整性”从 best-effort UI/缓存效果提升为可用 reservation ledger 证明的安全性质。下一项实现应优先补 D2c 的 local/remote real-holder batch/terminal matrix，而不是增加匹配器、扩大 compatibility inventory 或提前开放 dynamic alias。具体矩阵与 D3 边界见 [S1-C 生产就绪性复审与改进方案](semantic-tool-routing-s1c-production-readiness-review-zh.md#13-第二十一次复审把多轮工具不完整归因到-surface-ownership而非缓存)。

首批 D2c 回归现已接通 actual local/remote callback composition 与 `RunLoop`：starter failure、committer failure、complete commit 和 request 已发送后的 runtime terminal 都以同一 holder/relay 结算；nested exit / route supersede 也已验证 exact replacement isolation，且同 coordinator/tenant 上的双 executor route 不会相互取消。commit 后、普通 settled 前到达的 steer 也稳定结算为 `steered` 而非 `tool_batch_settled`。restart 仅能恢复 durable bound authority，不能复活 definitions/alias map/catalog，且 terminal route 拒绝恢复。verified runtime ingress 到 actual `codingagent.Run` 的完整 batch 回归也已证明 handler/identity/relay/terminal 不会在不同对象间分叉。它仍仅为 qualification-disabled test fixture，尚未完成 verified-ingress 下全部 cancellation、binder failure、early return 与 child handoff 的组合审计，不能据此宣称 D2/D3 或动态 alias 已完成。

### 10.33 第二十二次复审：必须证明实际 wire payload 是完整 replacement（2026-08-23）

10.32 将完整性正确地归于 request ownership，但仅规定“从 plan render”仍不足以排除 renderer/SDK 的 append、缓存合并、字段省略或序列化子集。故新增 `SurfaceManifest` / `SurfaceReceipt`：host 将当前 immutable plan 的完整 selected definitions、明确 omission 与 `Replace` 语义 canonicalize 并摘要；sender 在 `Do` 前对最终 wire payload 复算 hash，只有两者严格相等才允许发送和 bind。该 digest 不是 identity 或授权输入，执行权仍由 verified identity 与 live tuple/bound response 证明。

该收敛必须先覆盖当前生产中的 S0.5 静态兼容面，再复用于未来动态 alias。D2.5 因而成为 D3 硬门禁：local/remote 的 repeated turns、plan/budget change、empty surface、steer/retry/cancel/redirect 以及 SDK payload mutation 都必须证明每次 request 采用新的 receipt 和显式 replacement；任何 mismatch/implicit merge 均 fail-closed。D2 仍只指 test-only lifecycle proof；D3 才是 fixed cohort production conformance。完整方案见[完整工具面与请求所有权复审](semantic-tool-routing-surface-ownership-review-zh.md)。

首批代码已将 receipt 放在最晚可控点：HTTP compatibility path 在 `RoundTrip` 前核验最终 JSON body；Responses WS 在序列化 `response.create` 后、`WriteMessage` 前核验最终 frame。两条路径共享 canonical tool-contract projection，从而允许协议 envelope/安全 schema normalization，但拒绝 definition 集合或调用参数契约变化。静态路径的多轮 request、drop/append 与 empty surface 测试，以及 dynamic holder 的 WS-frame drop 回归均已通过；尚未把 receipt 列表接入 verified-ingress ledger，故 D2.5/D3 仍未完成。
