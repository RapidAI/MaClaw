# Coding 语义工具路由：S1-C 生产就绪性复审与改进方案

> 状态：**设计门禁，尚未批准生产切换**（2026-08-23）
>
> 适用范围：`CodingSubAgent`、`RemoteCodingSubAgent` 的动态 Skill/MCP。
> 本文是 S1-C 接线的单一验收清单；它不改变现有“动态 alias 不 materialize”的默认策略。

## 1. 复审结论

原始问题的根因不是“工具数量不够”，而是把历史工具名当作会话连续性，并让它参与下一轮工具面的拼接和预算竞争。正确的主线仍然是：**连续性只保存任务事实、工件和已结算结果；每次模型请求只根据当前计划完整替换工具面。**

当前实现已经消除了大量旁路：静态兼容面按请求重建、ambiguous delivery 后隔离、隐式 SDK successor request 被禁止、动态匹配不再直接成为模型可达执行器，且 G3/G4、response-ID parser、single-use request-channel 均已有基础设施。

但“有 request channel”和“可发送动态 alias”之间仍有一个不可跳过的闭环。`responses-ws-channel-available-not-wired` 只能说明**已具备经审查的 transport primitive**；它不等价于 S1-C adapter，更不等价于生产可用。若此时只把 capability matrix 改成 eligible，模型仍可能绕开 durable surface、在 bind 失败或 channel 关闭后遗留可执行 alias，或把上一 channel 的迟到调用误交给下一 channel。

因此本次决定是：**不直接接通 local/remote production callback；先实现一个受测的 S1-C adapter holder，并将其作为唯一可被生产接线复用的对象。** 在全部 release gate 通过前，`codingDynamicAliasesMayMaterialize()` 必须仍为 `false`。

## 2. 当前事实与不得误报的边界

| 项目 | 已有事实 | 不能据此推导 |
| --- | --- | --- |
| 可信身份 | Desktop coding workbench 可解析受限的 verified identity；缺失继续 fail-closed | 任意 GUI/IM/remote request 都已具备 semantic identity |
| G2 catalog / planner | contract-backed catalog 和 immutable plan preparation 已存在 | matched Skill/MCP 已获得执行权 |
| G3 | durable plan、grant、materialization、prepared/response-bound surface 和 restart recovery 已存在 | callback 已从真实 transport 发布/绑定 alias |
| G4 | `ExecuteBoundSelection` 可以走 canonicalize、validate、admit、journal、complete/reject | local/remote 模型调用已经只经该 bridge 执行 |
| provider evidence | SSE/WS parser 保留 provider `ResponseID` 并拒绝同流冲突 | parser ID 本身证明了连接、投递或重传关系 |
| request channel | Responses WS 已有 live dial 后生成的 opaque connection ID，且 `Do` 单次执行 | 该 channel 已承载 Coding callback 的 definitions、bind 和 dispatch |

任何设计或提交说明若把上表右列的结论写成已完成，均应视为阻塞性文档缺陷。

## 3. 必须保持的授权链

```text
可信 identity + 审查过的 catalog + host needs
              -> immutable ToolPlan
              -> PublishSurface / issued grant / materialization
live request channel + fresh epoch
              -> prepared ModelRequestSurface + opaque alias
provider ResponseID
              -> BindModelRequestResponse (唯一可解析状态)
{protocol, connection, response, epoch, toolCallID} + alias
              -> Resolve -> canonicalize -> validate -> Admit/journal
              -> fixed ExecuteBoundSelection -> Complete / Reject / settlement
```

以下任何输入都只能作为候选或诊断信息，绝不能恢复或选择执行权：任务文本、路径、`LoopContext.ID`、runtime request/task/attempt ID、用户 ID、工具名、provider/model/URL、`matchedSkills`、`matchedMCPTools` 或 `byName` 缓存。

## 4. 新增的 S1-C adapter holder 设计

### 4.1 单一对象、单一所有权

新增一个仅面向 S1-C 的 holder（名称可为 `codingBoundDynamicRequestAdapter`），一份实例只代表一次已 reserve 的 request channel。它必须同时持有：

- 已验证的 `TrustedCodingInvocationIdentity`；
- live `ToolSurfaceRequestChannel` 交出的 `{Protocol, ConnectionID}`；
- fresh `SurfaceEpoch`；
- `codingDurableDynamicSurface` 和 coordinator；
- response binder；
- `ToolCallContextExecutor` 的固定 bridge；
- 生命周期状态与一次性 close/cancel 函数。

它不能持有 `matchedSkills`、`matchedMCPTools`、`codingDynamicSurface.byName` 或可按名称恢复 provider 的 map。definitions 和 alias 只能由 `BuildToolsForBoundModelRequest` 从 coordinator 已发布的 materialization 导出。

### 4.2 状态机和线性化点

```text
reserved
  -> prepared                 // renderer 成功 publish；alias 仍不可解析
  -> sent                     // channel.Do 开始；仅作诊断，不授权
  -> response_bound           // Bind(ResponseID) 成功；唯一可 Admit 的状态
  -> terminal

任意失败： reserved/prepared/sent/response_bound -> cancelled | failed
```

这里还必须区分“transport 已返回 response”和“该 response 已被 loop 接受并完成 bind”。前者不是可保留 surface 的终态：若 live steering、取消、空/非法响应、binder 前的 loop 退出或 response discard 发生在二者之间，holder 必须以 host-owned `response_abandoned` 原因终止，不能把 prepared surface 留给下一轮，更不能只关闭 socket。

规则如下：

1. renderer 只能接收该 channel 的 `{Protocol, ConnectionID, Epoch}`；缺一个字段即不 publish。
2. `ResponseID` 必须由同一 channel 的 parser 产出，并在任何 tool call 之前 bind。空、冲突或 bind 失败时，所有 call 返回 `stale_surface`。
3. dispatch 必须精确比较 protocol、connection、epoch、response ID 和非空 tool-call ID；任一不符不得查询 provider。
4. 执行前必须重新观察 lifecycle-owned catalog，并验证 immutable binding/schema/contract digest。catalog 不完整为 `catalog_incomplete`；binding 漂移为 binding-stale rejection + 同 need replan。
5. `Close`、channel error、context cancellation、binder failure、steering、nested exit 以及 route supersede 均调用同一个 coordinator 生命周期入口，退休 surface/materialization 并撤销尚未消费的 grant。不能只清 holder 字段或 alias map。
6. 一个 terminal holder 绝不重启 `Do`、重新 bind、重新 render 或接受 successor 的 call。successor 必须 reserve 新 channel、生成新 epoch 并重新 publish。
7. shared loop 必须拥有**显式的 surface disposition hook**，并在每一个 response 路径恰好结算一次：`response_abandoned`（response 返回但未 bind/未接受）、`response_settled`（无 tool call 的最终回复）、`tool_batch_settled`（工具 batch 已结算）、`steered`、`runtime_terminal`、`nested_exit` 与 transport error。该 hook 只接收 loop 已持有的 execution tuple 和封闭 disposition；不得以 task text、配置、runtime ID 或工具名补造生命周期证据。`channel.Close(nil)` 仅释放 transport，绝不能被解释为 durable surface 已退休。

### 4.3 伪代码约束

```go
func (a *codingBoundDynamicRequestAdapter) BuildToolsForBoundModelRequest(
    userText string, iteration int, exec agent.ToolCallExecutionContext,
) []map[string]any {
    requireExactTransport(exec, a.channel.ExecutionContext())
    requireVerifiedIdentity(a.identity)
    return a.surface.PublishAndRender(exec) // prepared only
}

func (a *codingBoundDynamicRequestAdapter) BindToolSurfaceResponse(
    exec agent.ToolCallExecutionContext,
) error {
    requireExactTransport(exec, a.channel.ExecutionContext())
    requireNonEmptyProviderResponseID(exec.ResponseID)
    return a.surface.BindResponse(exec) // failure terminally retires the route surface
}

func (a *codingBoundDynamicRequestAdapter) ExecuteToolCallWithContext(
    alias, args, toolCallID string, exec agent.ToolCallExecutionContext,
) agent.ToolExecutionResult {
    requireExactBoundCall(exec, toolCallID)
    return a.surface.ExecuteBoundSelection(alias, args, toolCallID, exec)
}
```

这里的 `require*` 必须 fail-closed；它们不是从字符串推导 identity 的辅助函数。

## 5. 分阶段改进计划

| 阶段 | 交付物 | 通过条件 | 明确禁止 |
| --- | --- | --- | --- |
| A：文档与接口收敛 | 本文、holder interface、单一 adapter qualification registry | matrix 的状态与真实接线一致；未接线状态不可被 feature flag 覆盖 | 将 `responses-ws` 标为 eligible |
| B：受测 holder | 无 production callback 调用的 adapter fixture，连通 channel → publish/render → bind → fixed bridge | 正、负向 lifecycle test 全通过；所有真实 provider I/O 使用假桥 | 修改 `codingDynamicAliasesMayMaterialize()` |
| C：transport conformance | 将 live Responses WS channel 接入 holder，验证 close/cancel/error 统一撤销 | 一个 channel 一次 `Do`；无 fallback/retry；所有 terminal path 无可 resolve alias | 以 HTTP/SSE parser 或配置标签冒充 channel |
| D：production wiring | 仅在 verified desktop Coding ingress 下由 callbacks 实现 channel provider、bound renderer、binder、context executor 和 loop disposition owner | shared loop 对每个 reservation 都给出唯一 terminal disposition；灰度关闭时行为与今天相同；灰度开启也仅允许受认证固定分桶 | 让 local/remote 旧 name dispatcher 兜底 |
| E：效果与灰度 | effect/receipt policy、metrics、回滚演练、双 executor/restart 验收 | fail-closed 指标与人工处置路径可用；带副作用 provider 有 settlement contract | 在缺 receipt contract 时开放 effectful MCP/Skill |

阶段 B 已完成为 `test-only`：`codingBoundDynamicRequestAdapter` 只在定向测试中组合 durable publisher、response binder 与 G4 fixed bridge，未被任何 local/remote callback 构造或注册。它验证 channel tuple/epoch 贯穿 publish、bind 和 dispatch，并在 bind failure 或 close 时经 coordinator 退休 surface。阶段 C 的首个 test-only 接合也已完成：holder 可包装一个 live Responses WS reservation，自身成为唯一的 `Do` 所有者，定义只会在同一 socket 的 tuple 上发布；回归验证 provider response ID 回到 binder、同一 socket 的第二次 exchange 被拒、close 后 predecessor alias 不可解析。

为避免未来“测试通过后顺手打开开关”，现已增加 app-owned `codingDynamicProductionAdapterQualification`：它与 transport capability matrix 分离，当前固定为 `Wired=false`、`Enabled=false`、`coding_dynamic_production_wiring_disabled`。唯一 future factory 先读取该内部 qualification，未通过即不 reserve socket、不读 catalog、不发布 surface；它不接受调用者传入的 boolean、URL/model/provider 描述字段或 task 文本作为资格证据。它仍没有 callback provider 注册或灰度资格；D/E 必须以单独设计评审批准，不能随同基础设施改动自动发生。

holder 还补齐了取消先于 durable retirement 的 bridge context：host-owned `Close(cause)` 先取消传入 `ExecuteBoundSelection` 的 context，再调用 `CancelRouteSurface`。因此一个恰好已经通过 alias admission 的 call，在 catalog fresh-observe 或 provider I/O 前看到 cancellation 时会完成为 `stale_surface`，而不能趁本地 close 尚未完成重新开始 I/O；回归同时验证 context 被取消且 predecessor alias 已不可 resolve。这是 test-only G5 failure closure，不意味着 steering/nested exit 已接入 production callback。

为 production lifecycle 接线预先收敛了唯一 terminal API：`CloseForLifecycle(steered | nested_exit | runtime_terminal)`，未知 reason 也 fail-closed 地退休同一 holder。它不接受任务文本、transport 配置、runtime ID 或工具名作为 reason；定向回归证明三种已命名终态和未知输入都会取消 bridge context 并使 bound alias 不可解析。当前 callback 仍未调用该 API，所以这只是可测试的装配契约，不改变资格门禁。

进一步抽出 callback-facing lifecycle relay：它是 callback 的 request-channel provider、bound renderer、response binder、context executor 与 disposition observer 的共同 owner。relay 只有 factory 返回**含 live protocol/connection 的 holder**时才保存 active state；缺 connection 的 fixture/adapter 会立即 close 并拒绝，绝不用 config 或 callback epoch 补齐。local/remote callback 已以 D1 的 `test-only` composition shape 原子实现并委托这五个接口；它只读取已验证 identity 和 app-owned qualification，当前 qualification disabled 时 relay 必定为 `nil`，五个入口整体 inert，故不会 reserve、publish、bind、dispatch 或 materialize alias。此处仍是装配/回归形状，不是 production enablement。

该 lifecycle 缺口现已在 shared loop 补为 `test-only` conformance seam：每个 non-nil request channel 可向 `ToolSurfaceDispositionObserver` 收到一次且仅一次的 `transport_failure`、`response_abandoned`、`response_settled`、`tool_batch_settled`、`steered` 或 `runtime_terminal`。loop 在 response-ID 可得时先写回 disposition context；socket 的 `Close(nil)` 仍仅释放 transport。relay 对 tuple 精确匹配后清空 active holder，再将 disposition 映射到同一个 `CloseForLifecycle → CancelRouteSurface` 路径；错误 tuple 和重复 disposition 不得影响 active/successor holder。定向回归覆盖空 choices abandon、finalization-steer 后 successor settle、tool-batch settle、transport failure 和 relay 的匹配/重复拒绝。

对所有 reservation 后 `return/continue` 的再次审计补齐了 recovery-only 分支：截断 tool call、非法 tool arguments、空响应达到 iteration 上限均为 `response_abandoned`，而不是在追加 recovery prompt 后把 predecessor 留为 active。无效参数路径的回归断言 binder 已收到同一 provider response ID、没有任何 tool execute、且只发出一次 abandon disposition。

随后复审 binder failure 的交接顺序：`BindToolSurfaceResponse` 本身会先退休 holder，而 loop 随后仍必须 emit `response_abandoned`。relay 因而以 exact reservation tuple（不要求 holder 仍 non-terminal）清除 active 指针，再幂等调用 close；否则已退休 holder 会永久占住 relay，阻止 successor reservation。这一规则不放宽任何 execution 校验；dispatch 仍要求 holder non-terminal、已 bound 的完整 tuple。新增 regression 锁定“bind failure → abandoned disposition → active cleared”。

这仍不是 D 阶段：当前 callback 的装配尝试是 `implemented-not-wired`，而非接口注册；qualification 仍 disabled，动态 aliases 仍零 materialization。production cutover 前还须把真实 host 的 steering、nested exit、runtime terminal 与 batch durability owner 注册到这一 seam，并证明每个 reservation 的全路径 exactly-once disposition。

## 6. 必须新增的回归矩阵

### 正向闭环

1. reserve 的 protocol/connection/epoch 原样贯穿 render、`Do`、response bind 和 dispatch。
2. 只有 provider 返回非空 `ResponseID` 后，动态 alias 才能解析并进入 journal。
3. 相同完整 host-call identity 重传返回首次 journal 结果；参数 digest 不同返回 conflict，不再次 I/O。
4. 下一轮重新 reserve 后使用不同 epoch/connection；前一轮 alias 不能在新 holder 上解析。

### 必须拒绝

1. 空/冲突 `ResponseID`、空 tool-call ID、错误 protocol/connection/epoch/response ID、未知 alias、未 bind surface。
2. renderer/publish、bind、canonicalize、catalog fresh-observe 或 bridge 任一步失败。
3. channel `Do` error、close、context cancel、steer、route cancel/supersede、nested exit、process recovery 后的迟到调用。
4. 清空所有内存 match/alias/catalog 缓存后，不得借 provider 名称、路径或历史工具名执行；恢复只可读取 durable current surface，且 execution 前仍重新观察 catalog。
5. 两个 tenant/session、两个 executor 和 predecessor/successor 并发时，不能共享 grant、alias、journal 或 completion。

每个负向测试还应断言三件事：无 provider I/O、无 grant consume、无新的 executable successor surface。

## 7. 发布门禁与可观测性

发布判断不得由一个布尔开关单独承担。应在 `codingDynamicAliasesMayMaterialize()` 之前收敛为不可伪造的 qualification result，至少包含：

- adapter 类型和版本；
- verified ingress 范围；
- transport conformance 版本与测试证据；
- dynamic catalog/receipt policy 覆盖；
- 固定 tenant + trusted task-handle 的灰度分桶；
- kill switch，以及关闭后返回的 `catalog_incomplete` / `stale_surface` 行为。

上述要求现已被类型化为 qualification 的必要字段，而非仅保留在文档：`AdapterVersion`、`VerifiedIngress`、`LifecycleDispositionVersion`、`CatalogReceiptPolicyCovered`、opaque `FixedCohort`、`KillSwitchInstalled`、`Wired` 与 `Enabled` 必须连同 correlation capability 全部为真，`eligible()` 才成立。future factory 与 alias gate 只读取这个完整结果；配置的 URL、model、provider/name、任务文本、runtime ID 或调用者布尔值均无入口可填写这些字段。当前 app-owned registry 故意返回全部 release evidence 为空/false，继续 fail-closed。

必须分别采集 `catalog_incomplete`、response bind failure、`stale_surface`、journal replay/conflict、binding drift、channel terminal reason、cancel fencing 与 effect settlement。告警和回滚只能关闭动态 surface，不能回退到 `manage_skill`、`call_mcp_tool`、`byName` 或静态 name dispatch。

## 8. 对现有文档的维护规则

1. `semantic-tool-routing-design-review-zh.md` 记录架构决策与审查结论；`semantic-tool-routing-coding-subagent-remediation-zh.md` 记录 Coding 实施切片；本文维护 S1-C 的当前生产门禁。
2. 每项实现记录必须有明确状态：`implemented-not-wired`、`test-only`、`production-wired-disabled`、`production-enabled`。禁止用“已完成”省略作用域。
3. release note 只有在本文第 5 节 D/E 的条件满足后，才能使用“动态能力可用/已迁移”；此前只能称为“基础设施已实现，动态 aliases 仍 fail-closed”。

## 9. 最终建议

继续推进“计划驱动、完整 replacement、durable grant/journal”的方向，但将下一次改动严格限定为**受测 adapter holder**与其 failure-closure 测试。不要为了恢复便利性先放开动态别名；那会重新引入按名称和内存匹配执行的根本缺陷，且恰好会在多轮、重试、取消和重启时再次表现为工具面不完整或错误执行。

## 10. 本次复审补强：D 阶段必须原子切换 callback composition

当前的装配字段消除了一个未来实现细节上的歧义，但也暴露出新的发布风险：若先把 qualification 改为 eligible，再逐个给 callback 增加 interface，便会出现“holder 已可被创建、但 Render / Bind / Execute / disposition 只接了其中一部分”的半接线状态。该状态不能被视为安全灰度，因为任何一条旧 name dispatcher 或未结算 return 分支都会重新成为授权旁路。

因此将 D 阶段进一步拆成如下不可跨越的门禁；只有 D3 通过后才允许 registry 发布完整 qualification：

| 切片 | 允许改动 | 必须证明 | 禁止项 |
| --- | --- | --- | --- |
| D0（已完成） | callback 仅保留 `dynamicLifecycleRelay` 装配边界 | identity 缺失或 qualification disabled 时 relay 为 `nil`，无 socket/catalog/alias 副作用 | 把字段存在当作 production wiring |
| D1 | 为 local 与 remote 各创建一个**完整 composition adapter**，一次性委托 request channel、bound renderer、binder、context executor、disposition observer 五个接口 | 同一 callback 实例的五个接口只读取同一个 relay；任一接口或 relay 缺失时整套退回 S0.5，不做部分代理 | 逐个 interface 开关、直接调用 holder、旧 `manage_skill`/`call_mcp_tool` 兜底 |
| D2 | 将 verified ingress bind、runtime cancel、nested exit、route supersede 和 batch settlement 统一登记到这个 composition owner | 每一 live reservation 在真实 callback 路径上有且仅有一个 disposition；identity bind 发生在 composition 构造之前 | 由 task/path/runtime/LoopContext/provider 配置补造 identity 或 lifecycle tuple |
| D3 | 以 host-owned fixed cohort 做只读/无副作用 catalog 的 production conformance 与 kill-switch 演练 | 开关关闭立即停止 reserve/publish/materialize；已绑定 surface 仍只会 `stale_surface`，绝不回落到按名执行 | 仅凭 unit test 或 WS 可连接性把 `Wired/Enabled` 置真 |

`codingDynamicAliasesMayMaterialize()` 在 D3 前应继续无条件返回 `false`；即使未来 qualification 数据结构的字段被填满，也不能把这一函数改成“只要 `eligible()` 就为真”。它必须同时确认 callback 的完整 composition 已为**本次实际请求**取得 live holder，并且渲染、bind、dispatch 都将经过该 holder。这样 qualification 是发布许可，而不是从配置推导出的执行权。

建议的实施顺序是先增加 D1 的编译期 interface 断言与 fake-channel 集成测试，再接 D2 的真实 ingress lifecycle；最后才以单个不可由 user/task/config 推导的 cohort 开 D3。每一步都保留当前 fail-closed 默认，任何缺证据的路径只返回 `catalog_incomplete` 或 `stale_surface`。

**当前实施状态：D1 composition shape 已以 `test-only` 完成；D2 的 host-terminal bridge 已开始，但 production lifecycle conformance 尚未完成。** local 与 remote callback 现都原子实现上述五个 loop extension，但它们只快照已装配的 relay：relay 为 `nil` 时 request-channel provider 返回 `nil,nil`，bound renderer 返回空、binder/disposition 无副作用，保留既有 S0.5 static path；relay 存在时五个入口全部委托同一 relay，`ExecuteToolCallWithContext` 不再回退到 static/name dispatcher。定向 fake-channel 回归已覆盖 local/remote 的 reserve → render → bind failure → disposition cleanup 以及 late static-name `stale_surface` 拒绝。production qualification 仍 disabled，因而真实 callback 当前只能走前一种 nil-relay 行为。

为 D2 增加了 execution-scoped lifecycle owner：它只持有已安装 relay，并监听 host-owned `LoopContext` cancellation 或 detached child 的 runtime execution context；二者任一终止即经 `CloseForLifecycle(runtime_terminal)` 取消 bridge context、退休 durable surface 并清除 active holder。local/remote 的同步 nested handoff 也在 child 获得 fresh Attempt 前以 `nested_exit` 关闭 parent owner；owner 以 exact relay ownership 清理，旧任务 return 不可关闭 successor relay。此处不从 task/path/runtime ID/config 补造 identity 或 tuple。回归覆盖两种 cancellation source、nested handoff 和 replacement-relay isolation。仍待 D2 完成项是：在实际 approved cohort 中端到端验证 verified ingress bind、steering/supersede、batch durability 与每个 reservation 的 exactly-once disposition；在这些证据齐备前不得改 qualification 或 alias gate。

## 11. 第二十次复审：pure Coding steering 的首个 D2 子门禁已关闭，D2 尚未完成（2026-08-23）

### 11.1 阻塞性发现

`RunLoop` 只会在 callback 实现 `agent.LLMReplanAware` 时，把 `LoopContext.RequestReplan()` 变成当前 request 的取消、`ToolSurfaceSteered` disposition 和不消耗 iteration 的 successor；无 tool-call 的最终文本还要求 `agent.LLMFinalizationGuard` 与同一 revision 进行原子裁决。共享 IM callback 已实现这两个协议：它在 `TransformConversation` 前快照 revision、注入 steering 后提交已消费 revision，并用 `TrySealReplans` 阻止“最终文本刚提交、用户引导刚被接受”的竞态。

复审时 pure local/remote Coding callback 只通过 `codingSubAgentHooks.TransformConversation` 消费 pending injection；hooks 没有 `LoopContext` revision 的 observed state，`codingSubAgentCallbacks` / `remoteCodingCallbacks` 也没有实现上述两个接口。因此 steering 可能只在下一轮文本拼接时出现，不能保证当前 request 被取消、不能保证其已 reserve 的 surface 收到唯一 `steered` disposition，也不能与 final response commit 原子竞争。这一缺口在 relay 为 `nil` 的 S0.5 路径同样会造成响应陈旧；在未来 D2 relay 非 nil 时则会直接破坏 predecessor surface 的 retirement 证明。

**结论：这是 D2 的 release blocker。** 不得以 `LoopContext.ID`、runtime ID、task/path、provider 配置或待注入文本补造 tuple/identity；也不得由 callback 直接 close relay 来替代 shared loop 的唯一 disposition。

### 11.2 已完成的首个子门禁

1. local 与 remote callback 各保存 request-local、原子读取的 `observedReplanRevision`；只从已有 host-owned `LoopContext.ReplanRevision()` 读取，不能从模型文本或 transport metadata 推导。
2. `TransformConversation` 在 drain 前快照 revision，且只提交该**预先快照**。drain 期间新到的更高 revision 保持可见；accepted steer 即使可见 payload 为空，也在该 transform 边界消费。
3. `LLMRequestContext` 在 scheduler 等待前安装该轮可取消 operation；因此 transform 后、实际请求创建前到达的 steer 会取消 operation，而不会被新 watermark 吞掉。
4. callback 的 `LLMReplanRequested()` 只调用 `ReplanRequestedSince(observedRevision)`；`TryFinalizeLLMResponse()` 只调用 `TrySealReplans(observedRevision)`。两者都只报告/裁决 revision，绝不自行退休 holder。
5. `RunLoop` 仍是唯一发送 `ToolSurfaceSteered` 的 owner；future relay 只按 exact reservation tuple 接收 disposition 并退休 holder。

### 11.3 D2 回归状态与剩余退出条件

| 场景 | 当前状态 | 必须断言 |
| --- | --- | --- |
| request 进行中收到 steer（pure Coding） | 已覆盖 | 当前 operation 被取消；successor 使用 injected conversation，旧文本不提交 |
| steer 落在 transform 与 request 创建之间 | 已覆盖 watermark 基础语义 | revision 不丢失；旧 request 不启动或被立即作为 steer replacement 处理 |
| steer 落在 response 返回与 final text commit 之间 | 已覆盖 | `TryFinalizeLLMResponse` 拒绝旧文本；只有 successor 可以结算 |
| hook 已注入 steer | 已覆盖 | observed revision 前移；successor 不反复 self-steer，且注入内容仍在 conversation 中 |
| relay 持有真实 reservation 时的 steer | 已覆盖 callback/holder conformance | `RunLoop` 的 exact tuple 只由 callback 转交给 relay 一次；predecessor alias 为 `stale_surface`，successor 使用新 tuple，重复/迟到 disposition 不影响 successor；零 static/name fallback |
| tool batch 与 steering/terminal 竞争 | **待完成** | 仅 batch 已 durable commit 才 `tool_batch_settled`；否则正好一次 `steered` / `runtime_terminal` / `response_abandoned`，且无 grant 残留 |
| cancellation 与 steer 竞争 | **待完成** | 取消胜出时不接受 steer；不会生成 successor surface 或第二次 disposition |

第一批 callback/loop conformance 已落地：local/remote callback 实现 `LLMReplanAware` 与 `LLMFinalizationGuard`，`codingSubAgentHooks` 以 transform 前 revision 更新 callback watermark，且 Coding request context 在 scheduler 等待前安装 replan-aware operation。真实 `codingagent.Run` 回归覆盖两端的 live steer cancellation、successor injected conversation、finalization 拒绝，以及已消费/后续 steering watermark。

这只关闭 **D2a（pure Coding 的 replan 协议）**，并不证明任何 alias 已被 reserve、bind、dispatch 或 retirement。当前测试走 nil-relay/S0.5；因此不能从“收到了 steer”推导出“holder 已收到 `steered` disposition”。D2b/D2c 必须在仍为 test-only 的 verified-ingress 集成夹具中使用真实 holder、coordinator、channel tuple 与 batch durability hook 完成证明；这不是 D3 的 production cohort，也不能改变 release registry。

## 12. 本轮改进方案：以 reservation ledger 完成 D2b/D2c

### 12.1 先建立可审计的测试证据，而不是提前打开开关

为每个 test-only live holder 建立仅测试可见的 reservation ledger。ledger 的 key 必须是 `RunLoop` 已持有的 `{Protocol, ConnectionID, SurfaceEpoch}`，response bind 后仅附加 provider `ResponseID`；它不接收 task text、路径、runtime/loop ID、provider 配置或工具名。记录的事件只有 `reserved`、`prepared`、`bound`、`tool_batch_started`、`tool_batch_committed` 与唯一 terminal disposition。

ledger 不是新的授权来源、生产 journal 或 identity 映射；它只把“每个实际 reservation 恰好一个 disposition”的 D2 断言变成可检查证据。一个 iteration 在 reserve 前被 steer 不产生 ledger entry；一旦 `ReserveToolSurfaceRequestChannel` 返回非 nil，则必须恰好有一个 terminal entry。

### 12.2 D2b：steering 与真实 holder 的端到端夹具

local 与 remote 的第一条同构 callback/holder conformance 已落地：test-only relay 用 actual `codingBoundDynamicRequestAdapter`、single-use fake channel、durable coordinator 和固定无副作用 catalog 创建 holder。测试以 `{Protocol, ConnectionID, SurfaceEpoch}` ledger 记录 reserve → prepared → response-bound → terminal，并用 `ToolSurfaceSteered` 的 exact tuple 证明 relay 仅退休当前 holder；duplicate/late predecessor disposition 不会关闭 successor，predecessor alias 解析和执行都为 `stale_surface`。production qualification 仍为 disabled；该测试只通过显式 test-only factory 安装 relay，不修改 `codingDynamicAliasesMayMaterialize()`。

至少在以下线性化点注入 steer：`Reserve` 后、bound render 后、`Do` 进行中、response 返回但 binder 前、binder 后但 tool batch commit 前、final text seal 前。每个用例必须断言：

- predecessor tuple 的 ledger 只有一个 `steered`（已覆盖）；
- predecessor alias 解析/执行均为 `stale_surface`，且没有 static/name dispatcher fallback；
- successor 使用不同 connection/epoch，且只在 predecessor terminal 后 reserve；
- provider I/O、grant consume、journal completion 和 catalog observation 的次数符合该状态；被 steer 的阶段不得新增 effect；
- local 与 remote 产生相同终态序列（steering path 已覆盖）。

### 12.3 D2c：宿主终态与 batch durability 的闭环

将现有 execution-scoped owner、nested handoff 和 `RunLoop` disposition seam 放进同一 real-holder fixture，分别注入 runtime cancel、nested exit、route supersede、bind failure、tool-batch starter/committer failure 和正常 batch commit。验收规则是：所有未提交 batch 都不是 `tool_batch_settled`；每个 reservation 仅有一个 terminal disposition；terminal 后迟到 call 的结果为 `stale_surface` 并且不发生 provider I/O。binder failure 仍允许先使 holder terminal，但随后 `response_abandoned` 必须清除 relay ownership，保证 successor 不会被旧指针阻塞。

### 12.4 D2 的完成定义与 D3 边界

D2 完成的证据是 D2a--D2c 在 verified ingress 的 test-only 集成夹具中均通过，并覆盖 local/remote parity、restart/recovery 及两 executor 隔离。它**不**授权 production materialization。D3 才负责 host-owned fixed cohort、只读无副作用 catalog、kill switch 和真实 production conformance；在 D3 评审签字前，`Wired=false`、`Enabled=false`、`codingDynamicAliasesMayMaterialize()==false` 是必须保持的安全状态。

## 13. 第二十一次复审：把“多轮工具不完整”归因到 surface ownership，而非缓存（2026-08-23）

> 后续复审发现，本节的“完整 replacement”还必须在最终 wire payload 上留下可验证 receipt，且应同时覆盖仍在生产使用的静态 S0.5 surface。详见[完整工具面与请求所有权复审](semantic-tool-routing-surface-ownership-review-zh.md)。该要求新增 D2.5，属于 D3 的硬前置，并不改变本文件既有 fail-closed gate。

### 13.1 根因与判定

“几次对话后工具不完整”的表象可能来自 definition cache、按 task 文本补工具、上一轮 alias 残留、SDK 隐式 successor，或终态未清理；但它们共享同一个根因：**模型可见工具面不是一次真实模型请求的完整、可替换、可结算的 host-owned snapshot。**

因此不能以“刷新工具列表”“提高匹配召回”“保留上一轮工具名”修复。这些修补会把历史名称重新变成授权与连续性来源，并在 steer、retry、batch 失败、nested exit 或重连后扩大交叉请求执行面。连续性只能保存任务事实、工件、已提交结果和未知 effect 标记；不能保存 definition、alias、grant、connection、response 或 tool-call 的可执行资格。

每一模型请求必须满足如下不变量：

```text
current immutable plan + current policy + verified host binding
  -> one complete rendered surface
  -> one reserved transport channel + one fresh epoch
  -> one provider-bound response (if accepted)
  -> one terminal disposition
```

其中任一箭头缺失时，该 request 只能 fail-closed，不能从 task/path/model/tool name、旧 cache 或 runtime/LoopContext ID 补全。

### 13.2 必须一起修正的四个层次

| 层次 | 正确职责 | 必须禁止的旧式做法 |
| --- | --- | --- |
| 计划层 | 将 capability、binding、effect、repeat budget、当前 revision 固化为 immutable `ToolPlan`；每轮从当前 plan 全量 render | 根据任务措辞、BM25/match cache 或历史名称增删模型工具 |
| 请求层 | 在发送前 reserve 唯一 channel，使用该 channel tuple + fresh epoch publish/render；successor 必须重新 reserve/re-render | renderer、sender、binder 分属不同 helper；SDK 内部 retry/redirect 复用旧 surface |
| 执行层 | 只以 `{protocol, connection, epoch, responseID, toolCallID}` + opaque alias 解析 immutable selection，经 admission/journal/fixed bridge 执行 | `byName`、静态 switch、provider/tool name 或 path 作为 fallback dispatcher |
| 生命周期/持久化层 | `RunLoop` 为每个 reservation 精确发出一次 disposition；batch 仅在完整 paired delta durable commit 后 settled | socket close 当作完成；各 callback return 分支自行清 alias；未提交 batch 被写成 settled |

四层必须原子组合。只优化其中一层（例如 request-local render）不能证明执行层不会回落按名 dispatch；只补 lifecycle close 也不能证明 sender 实际发送了 render 的 definitions。

### 13.3 D2c 的可执行验收矩阵

在 real-holder + relay + `RunLoop` 的 test-only verified-ingress fixture 中，local/remote 必须跑相同矩阵。ledger 仍只按 `{Protocol, ConnectionID, SurfaceEpoch}` 归属，response ID 仅作为 bind 后证据：

| 注入点 | 唯一允许 terminal | 额外断言 |
| --- | --- | --- |
| batch starter 返回错误 | `response_abandoned` | 零 tool/provider I/O、零 grant consume、relay ownership 清除 |
| 任一 tool 后 cancel / hard terminal | `runtime_terminal` | 剩余 sibling 不执行；不得出现 `tool_batch_settled` |
| steer 在 batch commit 前获胜 | `steered` | 不提交 batch、不生成 successor 前遗留 alias |
| batch committer 返回错误 | `response_abandoned` | 已有结果只能作为 uncertain diagnostic，不能被重放或标为 settled |
| 完整 paired batch + committer 成功 | `tool_batch_settled` | exactly once commit，随后 alias/holder 均 terminal |
| bind failure / route supersede / nested exit | `response_abandoned` 或相应 lifecycle terminal | 即使 holder 已先关闭，disposition 仍只清理同 tuple 的 owner，不触及 successor |

每格都必须进一步验证 terminal 后的旧 alias 为 `stale_surface`，且没有 catalog/provider I/O、grant consume、journal completion 或 static/name fallback。取消与 steer 并发时以 host terminal fence 为胜者；不得产生第二个 disposition 或 successor reservation。

### 13.4 D3 前的发布决策

上述 D2c 通过只证明测试夹具中的生命周期闭环；它不代表动态 Skill/MCP 可以进入真实会话。D3 还必须由 host-owned fixed cohort 在只读、无副作用 catalog 上验证 ingress、kill switch、审计和恢复边界，并进行关闭演练：关闭后禁止新的 reserve/publish/materialize，已绑定 surface 只能 `stale_surface`，绝不降级到 `manage_skill`、`call_mcp_tool`、`byName` 或静态 name dispatcher。

在这些证据和独立评审到位前，生产结论保持不变：`Wired=false`、`Enabled=false`，且 `codingDynamicAliasesMayMaterialize()==false`。

### 13.5 D2c 首批实现证据（test-only，2026-08-23）

首个 D2c real-holder conformance 已补入 local/remote 同构 `RunLoop` 夹具。该夹具使用实际 callback 的五接口组合、actual relay/holder、single-use channel 和 fixed G4 bridge；仅用测试 wrapper 提供 fresh epoch、记录 reservation ledger，并注入可选 batch durability hook。它没有更改 production qualification、registry、factory 或 alias gate。

已验证的 matrix 为：

- starter 失败：`response_abandoned`，context executor 零次进入、committer 零次进入；
- committer 失败：完整 paired batch 后唯一 `response_abandoned`；
- 正常完整 batch：唯一 `tool_batch_settled`，starter/committer/context executor 各恰好一次；
- request 已发送后 runtime terminal：唯一 `runtime_terminal`，零 executor、零 commit；
- durability commit 后、普通 batch settled 前收到 steer：唯一 `steered`，绝不改写为 `tool_batch_settled`；
- nested exit 与 route supersede：两者都以封闭的 host lifecycle reason 退休当前 reservation；旧 response 的迟到 disposition 不会关闭新 reservation；
- 双 executor：共享同一 coordinator/tenant 的两个不同 root/turn route 保持隔离；一方 supersede 不取消另一方已 bound alias；
- restart/recovery：仅凭 verified identity + durable tuple 恢复 response-bound surface；不恢复 definitions/alias map/catalog，恢复 bridge 在 catalog 缺失时 `catalog_incomplete`，terminal route 不能恢复；
- 上述每格 local/remote 对称，ledger 对相同 `{Protocol, ConnectionID, SurfaceEpoch}` 均只含 `reserved → prepared → bound(responseID) → terminal`；终态 alias 均为 `stale_surface`。

该证据关闭 D2c 的 batch/terminal/recovery 矩阵，且已覆盖 nested exit / route supersede 的 replacement isolation、双 executor route isolation、restart/recovery，以及 steer 恰落在 batch commit 后的终态裁决。另已增加 verified runtime ingress → actual Coding callback → `codingagent.Run` → actual relay/holder 的完整 batch lifecycle 回归，证明 identity 先于 relay bind，且 `RunLoop` 的 commit terminal 会清除同一 holder。**D2 仍未完成**：仍须针对 verified-ingress 路径逐一审计 cancellation、binder failure、early return 与 child handoff 的完整矩阵；在此之前不得将 D2 整体或 D3 标为完成。

随后已落地 D2.5 的首个 wire-proof：静态 S0.5 HTTP 路径在最终 `RoundTrip` 前对每个真实 request 建立并验证 `ToolSurfaceReceipt`，空面也强制编码成 `tools: []`；Responses WS 在 `response.create` frame 序列化后、实际 `WriteMessage` 前执行同一 canonical verification。定义减少、追加或 payload 与 manifest 不符都以 `surface_integrity_failure` fail-closed，且不会发送字节。该 WS 证据目前仍是 holder/transport 的 test-only fixture；它不代表 verified-ingress D2 已完成，也不改变 qualification 或 alias gate。
