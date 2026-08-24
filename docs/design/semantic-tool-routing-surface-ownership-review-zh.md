# 语义工具路由：完整工具面与请求所有权复审

> 状态：**设计修订建议，尚未批准生产切换**（2026-08-23）
>
> 适用范围：所有进入模型的 Coding 工具定义；包括当前 S0.5 静态兼容工具面，以及未来受限的动态 Skill/MCP alias。
>
> 关联文档：[S1-C 生产就绪性复审与改进方案](semantic-tool-routing-s1c-production-readiness-review-zh.md)、[语义工具路由设计复审](semantic-tool-routing-design-review-zh.md)、[Coding 子代理修复方案](semantic-tool-routing-coding-subagent-remediation-zh.md)。若三者与本文冲突，以本文的“完整性证明”和“阶段边界”约束为准。

## 1. 结论

现有方案已经正确识别出主要授权风险：历史工具名、alias map、task/path、LoopContext/runtime ID 和 SDK 私有 successor 都不能成为下一轮执行权来源；每次真实模型请求都必须由新的 reservation 负责 render、bind、execute 与 terminal disposition。

但它仍有一个 **P0 根本缺口**：文档把“从当前 plan 全量 render”作为规则，却没有把它变成可在真实 outbound request 上验证的证据。于是下列实现即使都“遵守接口”，仍可能重现多轮工具不完整：

1. planner 已选出完整集合，但 renderer 因缓存、预算、序列化失败或 SDK 合并行为只发送子集；
2. caller 以为 `tools` 是 replacement，provider/SDK 却按 append 或保留上次 request 的定义处理；
3. 静态 S0.5 和未来动态 surface 分别 render，某一侧没有进入 wire payload；
4. retry、redirect 或 successor 复用了旧 definitions；
5. budget 截断了候选，却没有把“有意省略”写进不可变 plan，因此无法区分正确的最小面和意外缺失。

因此，根本修正不是再加缓存失效、召回或 fallback，而是把“完整工具面”升级为一个由 host 生成、随请求发送并可审计的 **Surface Manifest / Receipt 合约**。没有完整性 receipt 的模型请求不得发送；没有 matching receipt 的 response 不得 bind/dispatch。该合约同时覆盖静态兼容面和未来动态面，不能只给目前关闭的 alias 路径增加证明。

但应严格区分三件事，不能把它们都称作“receipt 已发送”：

1. `payload_verified`：在最后可控的序列化边界，实际 tools payload 与 manifest 相等；这是**允许尝试发送**的前提。
2. `transport_handoff_started`：HTTP `RoundTrip` 或 WebSocket `WriteMessage` 已被调用；它只能说明进入了 transport，失败时仍可能是 `delivery_ambiguous`。
3. `provider_replace_conformant`：该 provider 的 API 合同已明确该请求的 tools 是 replacement，而非服务端会话中的 append/retain；它不能由本地 hash 单独证明。

`payload_verified` 证明 host 没有静默丢工具；它既不证明字节已经到达服务端，也不证明服务端会按 replacement 解释。后两项必须由 transport 生命周期和 provider conformance 分别证明。

## 2. 不变量：完整不等于“所有已知工具”

“完整”是相对于本次 immutable `ToolPlan` 的 complete replacement，而不是把全局 catalog、历史工具或匹配器召回的所有名称全部塞入请求。计划必须显式记录：

- 被选择的 capability / binding / schema / effect policy / repeat budget；
- 被省略的 capability 及确定性 `OmissionReason`（policy、budget、host need、unsupported transport 等）；
- 静态兼容工具与动态 alias 的规范化 definition，以及会改变该次可调用集合的 invocation policy（`tool_choice`、`parallel_tool_calls`、provider envelope）；
- 本次 plan revision、catalog/policy snapshot digest 与 canonical surface digest。

这使“工具少了”可被精确区分为两类：

| 情形 | 结果 |
| --- | --- |
| capability 不在计划中，且有合法 omission | 本次最小工具面完整；不得从历史补回 |
| capability 在计划中，但不在 outbound surface receipt | `surface_integrity_failure`，请求不得发送 |
| wire payload 与 receipt 不同 | `surface_integrity_failure`，response 不得 bind |
| 上一轮存在某名称、本轮 plan 未选择 | 正常 replacement；该名称不得可见或可执行 |

digest 只证明“host 所选的 canonical surface 被完整发送”，**不承担 identity、授权或跨请求关联职责**。授权仍依赖 verified identity、immutable plan、live `{protocol, connection, epoch}`、provider response ID 和 tool-call ID 的已有链路。

## 3. 统一 Surface Contract

每个真实模型请求在发送前必须构造如下只读记录；其字段均由 host-owned plan、renderer 和 live reservation 产生，禁止由 task/path/name/provider URL/runtime ID 补造。

```go
type SurfaceManifest struct {
    // Audit 证据。静态 S0.5 若没有可关联的 ToolPlan，必须显式标记
    // PlanEvidence=unavailable，而不是臆造 ID、omission 或 catalog digest。
    PlanEvidence        PlanEvidence
    Omitted             []OmissionRecord

    // 下列字段是本次请求的 replacement payload 投影。
    SurfaceMode         SurfaceReplacementMode // 必须为 Replace
    StaticDefinitions   []CanonicalToolDefinition
    DynamicDefinitions  []CanonicalToolDefinition // 当前 production 应为空
    InvocationPolicy    CanonicalInvocationPolicy
    PayloadDigest       string // definitions + ordering + mode + policy

    // 仅供审计；包含 PlanEvidence/Omitted。它不能与 wire payload hash 比较。
    AuditDigest         string
}

type SurfaceReceipt struct {
    PayloadDigest   string // manifest 的可发送 projection
    WirePayloadHash string // 实际交给 transport/SDK 的完整 tool payload projection
    AuditDigest     string // 只关联本次审计 manifest，不参加 wire 相等比较
    Verification    SurfaceVerificationState // payload_verified / failure
    Handoff         SurfaceHandoffState      // not_started / started / ambiguous
}
```

`{Protocol, ConnectionID, SurfaceEpoch}` 不是 receipt digest 的组成部分，也不能由 receipt 反向推导；它们仍是 live reservation 的身份。对于 correlation-bound channel，holder 在内存中把 receipt evidence 与**同一** live tuple 关联，并在 terminal 时一同销毁。S0.5 的 HTTP compatibility path 没有该资格，最多保存 request-local 的审计 receipt，不能伪造 connection/epoch 进入动态 lifecycle ledger。

### 3.1 `PayloadDigest` 与 `AuditDigest` 的双投影

这里不能继续使用一个笼统的 `ManifestDigest`。它会诱导实现者把未进入 wire 的计划元数据与实际发送字段混在同一个 hash 中，既无法验证 provider payload，又会迫使没有 `ToolPlan` 的 S0.5 路径伪造 omission。

`CanonicalInvocationPolicy` 只表达本次最终 wire 中、会改变模型可调用面的字段：

```go
type CanonicalInvocationPolicy struct {
    Envelope          ToolProviderEnvelope // openai-chat / responses / anthropic
    ToolChoice        CanonicalToolChoice  // explicit auto/required/specific，或 explicit provider-default
    ParallelToolCalls OptionalBool          // absent/default 与 false 必须可区分
}

type PlanEvidence struct {
    State             PlanEvidenceState // available / unavailable
    PlanID            string            // available 时才允许存在
    PlanSnapshotDigest string
    CatalogPolicyDigest string
}
```

- `PayloadDigest = canonical(definitions, replacement_mode, invocation_policy)`；它是唯一可与最终 HTTP body / WS frame 比较的 digest。
- `AuditDigest = canonical(PlanEvidence, Omitted, PayloadDigest)`；它只用于解释选择和预算，**不能**充当 wire hash，也不能成为 identity、grant 或 successor 输入。
- `PlanEvidence.State=unavailable` 是 S0.5 静态路径的诚实状态：该路径仍可验证完整 replacement 和 invocation policy，却不得杜撰 `PlanID`、`Omitted` 或 catalog snapshot。只有 renderer 获得同一次 immutable `ToolPlan` 时才可写入 `available`。
- omission 记录必须按稳定 `NeedID + ReasonCode` 排序、去重并仅存摘要。它说明“为何未选”，不授权任何本轮或下一轮工具。

provider-native 表示只有在下列受版本控制的投影白名单内才可视为同一 policy；任何额外改写均为 `surface_integrity_failure`：

| 逻辑 policy | OpenAI Chat wire | Responses wire | Anthropic wire |
| --- | --- | --- | --- |
| 指定函数 | `{type:"function", function:{name}}` | `{type:"function", name}` | provider 证书定义的等价形状；未证书即不合格 |
| 自动 / provider 默认 | 显式 `auto` 与字段缺失必须区分 | 显式 `auto` 与字段缺失必须区分 | 同左；不得从缺失反推意图 |
| 并行调用 | `parallel_tool_calls` 的 present/value | `parallel_tool_calls` 的 present/value | 若协议无此字段，projection 必须写成 `unsupported`，不能静默丢弃 |

这张表不是把所有 provider 强行抽象成同一 API；它只约束「同一个明确 host policy」经 provider 转换后是否仍保持同一 callable surface。若当前 builder 根本未提供字段，manifest 必须记为 `provider-default` / `absent`，而不是猜测为 `auto` 或 `false`。

要求如下：

1. `SurfaceMode` 只能是明确的 `Replace`；缺失、`Append`、merge 或“让 SDK 自行决定”均为失败。空工具面也必须以显式 replacement 发送，而不是省略字段后继承旧面。
2. canonicalization 必须在 hash 前完成：definition 顺序、JSON schema、required/default、tool choice、parallel policy 与 alias 表示均要确定化；不得用 map 迭代顺序或 provider 返回顺序计算 digest。`PayloadDigest` 只覆盖最终可发送 projection；`AuditDigest` 才覆盖 plan/omission。二者不得混用。
3. renderer 只能从 manifest 导出 wire definitions 与 invocation policy。sender 在真正发起 `Do` 前，重新计算实际 payload projection hash，必须与 `PayloadDigest` 相同；不一致时不发送、不创建 successor，并以单一 terminal disposition 退休 reservation。
4. receipt 必须作为本次 `Do` 的不可替换输出交给 `RunLoop`；不得仅在 transport 内部 log 一次后继续返回 response。`RunLoop` 在调用 binder 前验证 `Verified=true`、manifest/wire digest 相等、replacement mode 为 `replace`，并确认它属于当前 reservation。
5. transport/SDK 若无法暴露“最终 wire payload”，或 provider 没有经审查的 per-request replacement 合同，则该 adapter 不满足资格；禁止用“API 一般会覆盖”作为证据。遇到 HTTP redirect 时，tool-bearing request 必须拒绝，而不是让 client 自动跟随并形成第二次未重新 reserve/render 的请求。
6. `ResponseID` 只能在 matching receipt 的 channel 上 bind。receipt 不匹配、channel 关闭、response 迟到或 terminal 后的任何 call 都是 `stale_surface` / `surface_integrity_failure`，不得回落 by-name 或静态 dispatcher。
7. receipt 与 manifest 仅保存可审计 digest、计数、omission reason 和 terminal reason；不持久化 executable definitions、alias、grant 或 transport tuple 以供 successor 恢复。restart recovery 仍只能恢复已经 durable-bound 的 authority，并须重新观察 catalog。

## 4. 发送与生命周期的线性化

```text
immutable ToolPlan
  -> SurfaceManifest (complete selection + explicit omissions)
  -> reserve live channel + fresh epoch
  -> Render(Replace) -> payload_verified receipt
  -> transport handoff starts (or delivery_ambiguous)
  -> channel.Do once, returns response + immutable receipt evidence
  -> RunLoop checks matching receipt, then binds provider ResponseID
  -> fixed context executor / batch durability
  -> exactly one terminal disposition
```

- `payload_verified` 是发送前的线性化点；只有它存在，reservation 才允许进入 `ready_to_send`。真正的 `sent` 只能由 `transport_handoff_started` 标记，且 write/round-trip error 要按 `delivery_ambiguous` 而不是“未发送”处理。
- `channel.Do` 返回并不代表 receipt 已结算。共享 `RunLoop` 仍是唯一的 terminal disposition owner。
- `response_abandoned`、`transport_failure`、`steered`、`runtime_terminal`、`nested_exit`、`route_superseded` 与未 durable commit 的 batch 都必须退休 receipt 对应的同一 reservation。
- `tool_batch_settled` 只允许在完整 paired batch durable commit 后出现；它不表示下一 request 可重用本 receipt。
- 任何 successor 都必须重做 plan → manifest → new reservation → new receipt；不得从 predecessor 复制 definitions、digest、alias 或 omissions。

## 5. 对现有阶段计划的修正

当前 D2/D3 的边界在个别表述中混用了“verified-ingress test-only fixture”和“approved cohort”。这会让 D2 看似既是测试阶段又需要真实生产审批，形成误开 gate 的风险。统一为：

| 阶段 | 唯一目标 | 可运行范围 | 退出证据 |
| --- | --- | --- | --- |
| D2 | 证明 holder/relay/RunLoop 生命周期正确 | verified-ingress 的 test-only fixture；不得 materialize production alias | local/remote 对称的 terminal、batch、recovery、child handoff、binder failure、early return 与 cancellation 矩阵；每个 reservation 恰好一个 disposition |
| D2.5（新增） | 证明完整 replacement 真正到达 wire payload | test-only 可观测 transport；覆盖静态 S0.5 与 dynamic fixture | `PayloadDigest = final payload projection hash`；显式 Replace；有 plan 时 budget omission 可由 `AuditDigest` 审计；successor 不能复用 predecessor receipt |
| D3 | 小范围 production conformance | host-owned fixed cohort、只读且无副作用 catalog | verified ingress、D2/D2.5 evidence、审计、kill switch 演练全部通过；仍默认关闭动态 alias |
| D4 | 有副作用 capability 的逐项开放 | 经单独审批的 receipt-capable provider | effect idempotency/unknown-effect/compensation 策略和回滚演练通过 |

`Wired`、`Enabled`、`codingDynamicAliasesMayMaterialize()` 必须继续保持 false，直到 D3 经独立评审批准；D2/D2.5 测试通过不改变任何生产路径。

## 6. kill switch 的语义需要收紧

“关闭后已绑定 surface 只能 `stale_surface`”只适用于尚未通过 admission、尚未开始外部 I/O 的 call。对于已开始的副作用请求，kill switch 不能诚实地承诺它从未发生。

因此关闭操作应原子地：停止新的 plan/reserve/publish/materialize；取消未 admission 的 bridge context；退休未执行的 bound surface；并把已 admission/已发出 effect 的项标成 `effect_unknown` 或按 provider receipt 结算。它不得自动重试，更不得把 unknown effect 当作未执行。D3 只允许只读、无副作用 catalog；D4 才能以 provider receipt、idempotency key 和人工/补偿流程处理 effectful capability。

## 7. 必须落地的验收与观测

### P0：当前静态路径先覆盖

当前动态 alias 仍 fail-closed，而用户可见的“多轮工具不完整”首先可能发生在 S0.5 静态兼容路径。因此先为 local 与 remote 的**每一个真实 outbound request**记录只含 digest/计数的 manifest receipt，并测试：

- 连续多轮、plan 不变：每轮都发送同一个 canonical complete set，但 receipt / reservation 不复用；
- plan 增删、预算变更、显式空面：payload 与 manifest 精确相等，omission 可解释；
- steer、retry、redirect、cancel、response discard：predecessor receipt 不能出现在 successor wire payload；
- SDK mock/real channel：append、字段省略、payload mutation、serializer error 均在发送前失败；
- local/remote parity：相同 plan 得到相同 canonical digest，connection/epoch 不同；
- 终态与 restart：旧 alias/receipt 无法被解析或用于执行，且无 name fallback。

### P1：未来动态路径接入同一合约

把 `codingBoundDynamicRequestAdapter.BuildToolsForBoundModelRequest` 的输出限定为 manifest 的 `DynamicDefinitions`，并在 `Do` 前校验 combined static + dynamic payload digest。D2 尚余的 verified-ingress cancellation、binder failure、early return、child handoff 应同时断言 receipt 被同一 exact `{protocol, connection, epoch}` reservation 正确退休。

### P0：本轮复审新增的结构性门禁

现有首批实现把 HTTP/WS 的 final payload 校验放在正确的最后可控点，但尚不能据此宣称完成上述完整 contract。以下缺口必须按顺序关闭：

| 缺口 | 为什么是根本问题 | 必须的修正 | 验收证据 |
| --- | --- | --- | --- |
| channel receipt 未作为 `Do` 结果返回 | channel 可在内部验证后仍返回 response；`RunLoop` 仍可能在不知 receipt 的情况下 bind | 为 correlation-bound channel 增加一次性 `DoVerified` 结果，原子携带 `response + receipt + handoff state`；普通 `Do` 不得作为 qualified dynamic 路径 | 注入“验证成功但返回伪造/缺失 receipt”的 channel，binder 必须零调用且 reservation 只得到 `surface_integrity_failure` |
| receipt 的审计与 live tuple 未显式关联 | 仅凭 digest 无法证明 lifecycle ledger 中的 receipt 属于同一个 reservation；把 task/URL/name 拿来补会重新引入错误身份 | 由 holder 以当前 `{protocol, connection, epoch}` 暂存并消费 receipt；receipt 本身不含、也不生成 tuple | local/remote verified-ingress ledger 均出现 `reserved → prepared → receipt → bound → terminal`，且 key 仅为 exact tuple |
| `verified` 与真正 transport handoff 混淆 | `RoundTrip` 前校验或 `WriteMessage` 前校验后仍可能未写出，错误处理会误判为安全未发送 | receipt/evidence 记录 `payload_verified` 与 `handoff` 两个独立状态；失败后按 `not_started` 或 `delivery_ambiguous` 退休，绝不自动复用 surface | serializer failure 为 `not_started`；write failure 为 `delivery_ambiguous`；均无 successor receipt 复用 |
| 本地 payload 相等不能证明 provider replacement | 有状态 provider 仍可能把相等 payload 与旧工具面合并 | 为每个候选 provider 建立版本化 `ReplacementSemantics` 证书：API 依据、显式空面行为、redirect 策略、集成/契约测试；缺任一项不可 qualified | append/retain 模拟器和真实兼容端契约测试；HTTP 3xx 必须在首次 request 失败而非自动跟随 |
| manifest 仍未承载计划省略和 invocation policy | 只 hash definitions 无法判断预算裁剪是否有意，也漏掉 `tool_choice` 等会改变可调用性的 wire 字段 | 引入 `PayloadDigest`/`AuditDigest` 双投影；在有 `ToolPlan` 的 renderer 前冻结 `PlanEvidence/OmissionRecord`，将 provider-normalized `tool_choice`、parallel-call policy 纳入 `PayloadDigest`；无 plan 的 S0.5 显式标为 evidence unavailable；仅允许版本化白名单 schema/policy rewrite | plan/budget/empty 三组测试均能解释 omission；`auto→required`、指定函数变更、parallel 的 absent/false/true 变更均失败；未白名单 schema 或 policy 改写一律 `surface_integrity_failure` |

推荐接口形状（示意，名称可调整）如下；它刻意让 receipt 是同一次发送的返回值，避免 observer、全局 map 或事后查询造成 TOCTOU：

```go
type VerifiedToolSurfaceDispatch struct {
    Response *llm.Response
    Receipt  ToolSurfaceReceipt // immutable; includes verification + handoff state
}

type VerifiedToolSurfaceRequestChannel interface {
    ToolSurfaceRequestChannel
    DoVerified(ctx context.Context, messages []interface{}, tools []map[string]interface{}, onToken llm.TokenCallback, stream bool) (VerifiedToolSurfaceDispatch, error)
}
```

`RunLoop` 仅在当前 reservation 使用该接口、receipt 为 `payload_verified`、handoff 已开始且 digest/mode 匹配时调用 binder。`DoVerified` 返回 error 时，若 handoff 已开始，统一进入 ambiguous-delivery terminal；若未开始，则以 `surface_integrity_failure` 退休。接口中的 receipt 仍只作完整性证据，绝不成为 alias、grant、identity 或 successor 输入。

### 指标与阻断条件

至少采集：`surface_manifest_created`、`surface_payload_verified`、`surface_integrity_failure`、`surface_replace_unsupported`、`surface_omission_reason`、`surface_terminal_reason`、`effect_unknown`。任一 `surface_integrity_failure`、receipt-mismatch 或 append/implicit-merge 观察值都阻断 D3；指标只存 digest/计数，避免记录敏感 schema/arguments。

## 8. 最终建议

保留当前 fail-closed 动态 alias 策略，不通过“保留上一轮工具”“提高匹配召回”或恢复 name dispatcher 来缓解表象。下一项实现优先级应是：

1. 为静态 S0.5 出站请求实现并验证 `SurfaceManifest` / `SurfaceReceipt`；
2. 以同一结构补齐 D2 的 verified-ingress 未覆盖矩阵；
3. 将 D2.5 作为 D3 的硬前置；
4. 仅在只读 fixed cohort 的真实 wire evidence 后讨论生产接线。

这样“工具面完整”才是可证明、可回归、可审计的请求所有权性质，而不是依赖缓存行为的经验结果。

## 9. 实施状态（2026-08-23）

已完成 D2.5 的第一批**payload verification**证据，且没有改变动态 alias 的 production gate：

- `RunLoop` 对当前 S0.5 静态 HTTP 出站请求创建 request-local `ToolSurfaceReceipt`，在 `RoundTrip` 前核验最终 payload；缺失、减少或追加 definition 均不发送；空面写成显式 `tools: []`；fallback/retry 重建 receipt。
- Responses WebSocket transport 在 `response.create` frame 序列化后、`WriteMessage` 前用同一 canonical contract 核验扁平化后的 tools；test-only `codingBoundDynamicRequestAdapter` 已覆盖“render → WS frame → receipt”与删掉 definition 的 fail-closed 情形。
- receipt 只记录 digest、数量、replacement mode 与 failure，不成为 identity、grant、alias 或 successor 恢复输入。动态 production qualification 仍为 `Wired=false`、`Enabled=false`，`codingDynamicAliasesMayMaterialize()==false`。

本轮已关闭其中两项结构性缺口，仍未关闭 D2.5：

- correlation-bound channel 已新增一次性 `DoVerified` dispatch；`RunLoop` 仅在 receipt 与当前 rendered surface 相等、明确 `replace` 且 handoff 已开始时才调用 binder。缺失、伪造或不匹配 receipt 直接产生 `surface_integrity_failure`，binder 零调用；local/remote verified-ingress test ledger 已记录 `reserved → prepared → receipt → bound → terminal`。
- Responses WS 的单次 channel 已返回同一 socket dispatch 的 receipt，并区分 serializer 前失败与 `WriteMessage` 后的 `ambiguous` handoff；holder 仍在 production qualification-disabled 的 test-only 路径中。
- 静态 HTTP receipt client 已强制拒绝 tool-bearing 301/302/303/307/308 自动跳转；redirect 是第二次模型请求，不能继承首次 request 的 manifest/receipt。回归以真实 HTTP server 断言只收到首个请求、目标端零请求。
- production qualification 新增 `ReceiptDispatchVersion` 与 `ReplacementSemanticsVersion` 两个 host-owned gate；即使未来有人设置 `Wired/Enabled`，也不能仅凭 transport primitive 或配置字段越过“dispatch receipt / provider replacement”审查。

仍必须完成 provider replacement semantics 证书、真实 SDK serializer mutation、provider-specific wire conversion、bind/early-return/child-handoff、D3 fixed cohort 和 kill-switch 演练。HTTP tool-bearing redirect 拒绝已完成并有回归；它不替代 provider conformance。任何其余项缺失均不得提升 production qualification。

补充校正：当前静态 S0.5 HTTP client 的 receipt observer 在 `RoundTrip` 返回后才收到 handoff evidence；正常返回为 `started`，transport error 为 `ambiguous`。它仍是静态 compatibility 诊断，不能升级为 dynamic bind authority；dynamic path 只能使用 `DoVerified` 的原子 dispatch。

### 9.1 本次复审的修订结论与实施顺序

前述第一批实现解决了“definition 被删、加、改或空面被省略”这一层，但当前 `ToolSurfaceReceipt` 的 digest 只覆盖 definitions。因而它还不能检测 `tool_choice` 或 `parallel_tool_calls` 在最终 builder / SDK / WS frame 中被改写、删除或跨 provider 错投影；也不能把动态 `prepared.Plan.Omitted` 安全地带进审计。该问题是 D2.5 的剩余 P0，不是 telemetry 缺项。

以下改动必须作为**一个 fail-closed 切片**合入；任一 transport 未接入时，动态 production qualification 与 alias gate 继续关闭：

1. 在 `corelib/agent` 增加不可变的 invocation-policy / plan-evidence value object，并将 receipt 的 definitions-only digest 替换为 `PayloadDigest`。保留兼容字段只能作为短期迁移镜像，`RunLoop` 的 verified dispatch 校验必须最终只接受完整 payload projection。
2. 让 HTTP wrapper 从最终 JSON body、Responses WS 从序列化后的 `response.create` frame 读取 `tools`、`tool_choice` 与 `parallel_tool_calls`，调用同一个 canonical verifier。缺字段、类型错误、未声明转换或 `absent ↔ false` 漂移均在 handoff 前失败。
3. 对 Chat / Responses builder 明确传入 policy，而不能从 `ExtraBody` 或 map 遍历后猜测：Chat 的 typed `ToolChoice` 与 Responses 的 flat shape 先归一化到同一个 logical policy，再按 envelope 投影；Anthropic 在有经审查 certificate 前只记录自身原生 projection，不能假装拥有 Chat/Responses 的字段。
4. 仅在 `codingDynamicPlanPreparation` 已随 request-local renderer 进入同一 request 时，构造 `PlanEvidence=available`、稳定的 `Omitted` 摘要与 `AuditDigest`。当前静态 S0.5 `RunLoop` 只收到 definitions，必须使用 `PlanEvidence=unavailable`；不得为了报表把 static inventory、task/path、runtime ID 或旧 receipt 拼成 plan。
5. 为每一个候选 provider 加 `ReplacementSemantics` certificate：协议/版本、显式 `tools:[]` 行为、payload policy 映射、redirect 策略、append/retain 负例和实际 contract test。qualification 中的 `ReplacementSemanticsVersion` 只能引用已验证 certificate；字符串非空不是证据。
6. 最后补齐 D2 lifecycle 矩阵（binder failure、early return、child handoff、cancel/steer）和 D3 fixed cohort / kill switch。任何 `surface_integrity_failure`、policy projection mismatch、append/retain 或 certificate 缺失都阻断升级。

最小验收矩阵如下。每项都要同时断言 binder 零调用（若 handoff 前失败）、无 name fallback、旧 receipt/plan 不进入 successor，且 local/remote fixture 一致。

| 情形 | 最终 wire policy | 预期 |
| --- | --- | --- |
| `auto → required` | `tool_choice` 值变化 | handoff 前 `surface_integrity_failure` |
| 指定 `a → b` | provider-native specific-function 投影变化 | handoff 前失败 |
| `parallel_tool_calls`: absent → false / false → true | presence 或值变化 | handoff 前失败 |
| Chat nested choice ↔ Responses flat choice | 同一 logical specified-function policy | 仅在版本化白名单投影下通过 |
| dynamic budget omission | `PlanEvidence=available`、排序后的 Omitted 摘要 | `AuditDigest` 可审计；不改变 `PayloadDigest` 以外的 wire 校验 |
| 静态 S0.5 无 plan | `PlanEvidence=unavailable` | 仍校验 policy/definitions；不得声称 Omitted evidence |
| 3xx / append-retain mock | redirect 或服务端保留旧面 | 首次请求终止 / certificate 失败，绝不自动跟随或补回历史工具 |

### 9.2 对设计文档本身的评审意见

方案的主线是正确的：将 selection、render、wire、bind 和 terminal 线性化，并把 digest 与 identity 分离，能够切掉导致“聊几轮就残缺”的历史 surface 泄漏。但是在正式批准前，应修正三处表述/边界：

- 原型中单一 `SurfaceDigest` 容易误导为「计划省略也必须与 wire 相等」。现已改为 `PayloadDigest` / `AuditDigest`；这是可验证性边界，不是命名优化。
- “HTTP tool-bearing redirect 已拒绝”已经落地，因此不应继续列为未完成项；未完成的是 provider replacement certificate 与真实 provider contract。下方状态项据此更正。
- `HandoffStarted` 只表示本地调用过 `RoundTrip` / `WriteMessage`；即使随后遇到 3xx、读错误或超时，也不能作为 provider 已按 replacement 接受请求的证明。外部效果仍须按 `ambiguous` 或 provider receipt 处理。

因此 D2.5 目前结论为：**definitions wire verification、verified dispatch、完整 payload policy 与 plan omission audit 已完成；provider replacement conformance 未完成。** D3 仍明确 blocked，动态 alias 仍为零 materialization。

### 9.3 invocation-policy payload projection 已接入（本次实现）

本轮已把 `tool_choice`、`parallel_tool_calls` 和 envelope 纳入 receipt 的**绑定判定**，不再接受 definitions-only digest 作为 `RunLoop` bind authority：

- `ToolSurfaceReceipt` 现有 `PayloadDigest` / `WirePayloadHash`；它们由 canonical `(definitions, replace, invocation policy)` 生成。旧 `ManifestDigest` / `WirePayloadDigest` 仅为兼容诊断字段，不能通过 `RunLoop` 校验。
- 静态 HTTP wrapper 在最终 JSON body、Responses WS 在已序列化的 `response.create` frame 调用同一个 payload verifier。`tool_choice` 变化、specific-function 变化以及 `parallel_tool_calls` 的 absent/false/true 漂移，均在 transport handoff 前以 `surface_integrity_failure` 拒绝。
- `RunLoop` 依据实际选择的 request envelope（chat / responses / anthropic）重算 payload projection；correlation-bound channel 返回不同 envelope 或 policy 的 receipt 时，binder 零调用。当前生产动态 alias 仍关闭。
- 空面必须显式包含 `tools: []`。HTTP compatibility wrapper 会在最终 body 注入它；WS frame builder 已显式发送它。字段缺失不能被解释成 replacement。

### 9.4 plan omission audit 已接入（仍为 D2.5 / test-only）

本轮将 `AuditDigest` 接入同一 request-owned dispatch，而没有把审计事实升级为授权事实：

- `AuditDigest = PayloadDigest + PlanEvidence`。`PayloadDigest` 仍只覆盖最终 wire 的 definitions、replace 与 invocation policy；plan ID、snapshot、catalog generation 与排序去重后的 `{NeedID, ReasonCode}` omission 只影响 audit digest，绝不改变 wire proof、alias、grant、identity 或 successor authority。
- 静态 S0.5 明确使用 `PlanEvidence=unavailable`，并拒绝带字段的“不可用”记录；不会从 static inventory、task/path、runtime ID 或旧 receipt 伪造 omission。动态 test-only holder 只在已 render 且 exact `{protocol, connection, epoch}` reservation 匹配时提供当前 `prepared.Plan` 的证据。
- `RunLoop` 在 dispatch 前把 evidence 交给同一个一次性 correlation-bound channel；channel/WS 在最终 payload/frame verifier 中生成 receipt。缺少 setter、伪造 `AuditDigest` 或 evidence 与 holder 当前 plan 不符均为 `surface_integrity_failure`，binder 零调用且同一 reservation 退休。
- omission 的输入顺序和重复项不影响 digest；改变 omission reason 会改变 `AuditDigest`，但不改变相同 wire payload 的 `PayloadDigest`。回归已覆盖此性质、静态 unavailable 以及 local/remote forged-audit receipt。

这解决的是“预算/策略裁剪能否被诚实审计”的 D2.5 缺口；它**不**证明 provider replacement semantics，亦不打开动态 alias。下一硬前置仍是版本化 `ReplacementSemantics` certificate（含显式空面、append/retain、redirect 与真实 provider contract），随后才是 D2 lifecycle 矩阵和 D3 fixed cohort。

### 9.5 replacement certificate 已成为结构化资格门禁（尚未签发任何生产证书）

为避免 `ReplacementSemanticsVersion` 退化为“填一个非空字符串即可开门”，qualification 现要求一个与 live channel protocol 精确匹配的结构化 certificate，且其版本必须与兼容字段相等。该 certificate 必须同时声明并经过独立契约回归：

- 本协议 `tools: []` 是显式清空而非字段省略或历史 retain；
- tool-bearing redirect 被拒绝，不能形成未经新 reservation 的第二次发送；
- `tool_choice` / `parallel_tool_calls` 的 provider-native policy projection 有版本；
- append 与 retain 两类 stateful-provider 负例都已测试。

certificate 缺失、版本不一致、protocol 不匹配或任一条款为 false 时，即便其它 lifecycle、ingress、cohort 和 kill-switch 字段都已填写，`eligible()` 仍为 false。当前没有任何生产 certificate，`codingDynamicProductionAdapterForConfig` 继续返回 `Wired=false`、`Enabled=false`；该结构化门禁只是防止未来接线以字符串绕过 conformance 审核，不能替代真实 provider contract 或批准 D3。

### 9.6 D2 early-return 生命周期矩阵补齐（test-only）

已将 interactive early-return 加入 local/remote 对称的 verified holder 回归：`ask_user` 与 `record_audio` 在 response bind 后均必须产生一次且仅一次 `response_abandoned` disposition，并按同一 exact `{protocol, connection, epoch}` ledger 记录 `reserved → prepared → receipt → bound → terminal`。断言同时覆盖 holder/relay 退休、late alias 为 `stale_surface`，避免 interactive pause 将已经 response-bound 的 surface 留给下次继续或 successor。

已覆盖的 D2 test-only terminal 情形包括：receipt integrity failure、transport failure、empty/invalid response、binder failure、batch starter/committer failure、runtime cancel、steer、interactive pause、nested exit、route supersede 与 post-commit steer；每种均保持 production alias gate 关闭。nested handoff 已进一步覆盖真实 `runNestedCodingAgent` 的调用顺序：即使 child 在 isolate 校验阶段立即失败，父 holder 也已先按 `nested_exit` 退休；local/remote holder 的已绑定 alias 均不可再 resolve/execute。剩余 D2/D3 工作是将这些 fixture 证据映射到真实 provider contract 和独立批准的 fixed cohort，不能据测试通过自动开放生产路径。

### 9.7 builder 级显式空 replacement（本次实现）

Chat Completions 与 Responses 的请求构建器现在都支持显式 replacement 意图：调用方设置 `ExplicitToolReplacement` 后，即使输入为空、或所有 definition 在兼容性清洗后被剔除，最终 JSON 仍为 `"tools":[]`。普通无工具请求不隐式获得该字段，避免把 provider 的兼容性无工具重试误判为“清空一份既有 tool surface”。RunLoop 的 receipt wrapper 仍会在其拥有 immutable manifest 时强制补齐空 replacement；两类 builder 均有序列化回归覆盖。

这只是 host builder 的确定性保证，仍不能证明 SDK 之后或真实 provider 会按 replacement 语义处理该字段；最终 wire verifier、结构化 replacement certificate 与 D3 独立审批仍是必要前置。动态 alias 继续零 materialization。

### 9.8 RunLoop 统一禁止 SDK 内部 successor（本次实现）

此前只有特定 Coding host 主动把 `WithTransparentRequestRetriesDisabled` 放入 request context，普通 `RunLoop` callback 仍可能触发 SDK 内部的“去工具 / 压缩 / 参数降级”请求。这些请求绕开了新的 render、receipt、attempt telemetry 与 delivery disposition，违反“一次真实发送对应一次 owner”的边界。

现在 `RunLoop` 对所有由其启动的请求统一添加该 context 标记，同时保留 host 提供的 deadline、cancellation 与 tracing value。失败会返回给 RunLoop；其已有 fallback/outer retry 才能新建 deadline、重新 render tools、创建新的 receipt client，并记录独立 attempt。stream → non-stream fallback 同时替换 HTTP client，保证 successor final payload 由 successor manifest 验证，而不是沿用 predecessor receipt。此约束不改变 loop 外普通 SDK 调用的兼容性行为。

### 9.9 MoA reference 也纳入空面 receipt（本次实现）

MoA 的 reference/advisor 虽不允许工具调用，仍是独立的真实 model request。此前该分支直接发送 `nil` tools，不能证明它确实发出了显式空 replacement。现在每个 reference request 都建立独立 HTTP receipt client，在最终 wire body 上验证 `"tools":[]` 与该请求的 provider policy；reference 与 aggregator 都会产出各自 receipt。这样 advisory fan-out 不会成为“绕开完整工具面证明”的旁路。

### 9.10 MoA reference 禁止 SDK 隐藏 successor（本次实现）

advisor 请求不经过主轮 `llmRequestContextForLoop`，因此曾遗漏统一的 SDK-local retry 禁令：在兼容 provider 返回 4xx 时，reference 可能自行发出 tool-less、compact 或参数降级 successor，而没有新的 request owner、receipt 或 delivery record。现在 `CallRef` 在建立 reference timeout 后同样应用 `WithTransparentRequestRetriesDisabled`；SDK 必须把原始错误交还给 fan-out runner，不能在该闭包内再发送请求。回归使用会触发 OpenAI compatibility repair 的配置，断言 advisor 只发生一次 owner-visible HTTP 请求；reference 的显式空 surface 与 receipt 仍保留。此项同样不为动态 alias 提供任何 production qualification。

### 9.11 并发 MoA receipt observer 隔离（本次实现）

MoA advisor 的 fan-out 最多并发三个 HTTP request。每个 request 的 manifest、receipt client 和 payload proof 已是私有的，但它们此前可同时调用同一个 host `ToolSurfaceReceiptObserver`；普通 host 常以 append-only slice 实现审计 sink，会造成数据竞争或丢失 receipt。现在仅对同一 `RunLoop` 的 advisor observer 加入互斥串行化，主 aggregator 仍在 fan-out 完成后调用原 observer。该锁不参与 transport、identity、grant、alias、plan 或 successor 决策，也不为 receipt 赋予授权含义；它只保证每一已生成的诊断 receipt 完整送达 host。`-race` 回归覆盖三 advisor 并发与重复运行，确认 observer 最大并发为一且所有 advisor/aggregator receipt 均保留。

### 9.12 Anthropic 也显式表达 owner-owned 空 replacement（本次实现）

Chat 与 Responses builder 已支持在 owner 明确要求时输出 `tools: []`，但 Anthropic builder 原先只在非空转换结果时写入 `tools`。这会让 MoA Anthropic advisor 的 `nil` tools 依赖字段省略，和 receipt 的“显式 replacement”声明不一致。`AnthropicMessagesRequestOptions` 现增加 `ExplicitToolReplacement`：只有 caller 明确拥有 replacement 语义时，空/转换后为空的输入才写出 `tools: []`；普通无工具 SDK 调用保持原兼容性，不会被误标为清空。RunLoop 的 Anthropic 主请求、stream→non-stream fallback 和 MoA reference 都传入该意图，因此最终 wire verifier 能证明空面。回归覆盖 builder 的三 envelope 空面，以及 Anthropic MoA reference 的实际 wire body 与 verified receipt。此为 builder/wire 一致性修复，不构成 provider replacement certificate，也不改变 D3 或动态 alias gate。

### 9.13 WS 预发送完整性失败也终止 reservation（本次实现）

Responses WS channel 已在 `WriteMessage` 前验证 final frame 和 audit evidence；但若 audit evidence 缺失，调用会在尚未写 socket 时返回。若调用方随后补设 evidence，理论上可复用同一 live socket 形成一个未被首次 dispatch 记录的逻辑 successor。这不是 provider retry，却同样破坏“一次 reservation 只对应一次 owner-owned dispatch”的线性化约束。

现在 channel 在开始 `DoVerified` 时即原子标记 `dispatchAttempted`。缺 audit evidence、错误的 non-stream 调用、frame 校验失败及后续发送/读取错误都会使该 reservation 不可再次发送；之后设置 audit evidence 会被拒绝，第二次 `DoVerified` 也只返回 already-used。回归通过真实 WS server 断言：缺 audit evidence 或 non-stream 调用时 server 收不到 `response.create` frame，且同一 socket 不能在事后补证据或改为 stream 后再使用。该修正只收紧 test-only transport primitive 的 fail-closed 行为；不生成 dynamic alias，也不构成 D3 或 provider replacement certificate 的证据。

### 9.14 reservation 的 pre-dispatch failure 也有唯一 terminal disposition（本次实现）

`RunLoop` 过去只在 dispatch 区段创建 `disposeSurface`。因此已经拿到 non-nil channel、却在 epoch 缺失、transport correlation 缺失、bound renderer 缺失，或 audit-evidence setter 不可用/拒绝时提前返回的分支，只会调用 `Close`，不会把同一 reservation 的 semantic terminal fact 交给 holder/relay。这既违反“每个 reservation 恰好一个 disposition”，也可能让 relay 保留一个已关闭的 active holder，阻止之后的干净 reservation。

现在 disposition closure 在 channel reservation 后立即建立；所有上述 pre-dispatch integrity failure 都先发送一次 `surface_integrity_failure`，再关闭 channel 并返回。发送路径原有的 once guard 保持不变，所以后续错误或外层清理不会重复 terminal。`corelib/agent` 回归覆盖 missing epoch、missing correlation、missing renderer，以及 missing audit setter：均断言零发送、一次 Close、一次 `surface_integrity_failure` disposition。该修复只完善 D2 生命周期闭包；不改变 provider replacement 结论，动态 alias gate 仍关闭。

### 9.15 correlation-bound surface 不得退化为 unavailable audit evidence（本次实现）

此前 `RunLoop` 仅在 callback *恰好*实现 `ToolSurfaceAuditEvidenceProvider` 时才尝试写 evidence。一个已经 reserve channel 并完成 bound render 的 callback 若遗漏这个可选接口，仍可能以 `PlanEvidence=unavailable` 继续 dispatch；这把 S0.5 static HTTP 的诚实状态错误地借给了 request-local dynamic surface，使预算/策略 omission 无法审计。

现在 non-nil correlation-bound channel 强制要求三项同时成立：callback 提供 audit-evidence provider、该 exact reservation 返回 `Available=true` 的 immutable evidence、channel 接受同一 record。任一缺失都在发送前以 `surface_integrity_failure` disposition 退休，零 `DoVerified`/零 binder。静态 S0.5 只有在**未 reserve channel**时才能使用 `Available=false`。回归覆盖 provider 缺失与 provider 返回 unavailable evidence，并保留 missing setter 的测试；每项均断言零发送、一次 Close、一次 terminal disposition。holder 直调的第二次 `DoVerified` 会识别 stricter channel 的“after dispatch attempt”状态并继续交由 channel 返回 canonical `already used`，而不会把既有 evidence 重设错误伪装成新 owner。此为 D2/D2.5 的边界收紧，不签发 provider certificate，动态 alias 仍为零 materialization。

### 9.16 pre-dispatch integrity failure 也输出一次失败 receipt（本次实现）

此前 channel 在 `DoVerified` 后会把 dispatch receipt 送给 observer；但 epoch、correlation、renderer 或 audit-evidence 检查失败发生在 `DoVerified` 之前，只留下 terminal disposition，没有与该 reservation 对应的 receipt 审计项。这会使 audit sink 无法区分“尚未 reserve”与“已 reserve、明确未发送且被 fail-closed 退休”。

现在已 reserve channel 建立 request-local、once-only receipt reporter。任何 pre-dispatch integrity failure 都发送一个不可验证的 diagnostic receipt：`ReplacementMode=replace`、`Handoff=not_started`、带 `surface_integrity_failure` failure 原因；随后按既有路径发送唯一 terminal disposition 并关闭 channel。正常 dispatch 仍只报告 channel 返回的 receipt，不会双写。回归断言 missing epoch / unavailable audit evidence 均产生一次 failed receipt、零发送、一次 Close、一次 terminal；receipt 是诊断证据，绝不成为 alias、identity、grant 或 successor 输入。

### 9.17 已发布的 bound surface 不得被 authorizer 静默裁剪（本次实现）

此前 correlation-bound 路径在 `BuildToolsForBoundModelRequest` 发布 durable surface 后，仍复用了 S0.5 static compatibility 的 `FilterToolDefinitionsByAuthorizer`。若 authorizer 的可见性判断与 holder 已发布的 alias 集不一致，loop 会把一个较小的 definitions 子集发到 wire；receipt 仍可对这个子集验证通过，却无法证明 provider 所见面与 durable holder、plan 和后续 bind/execute 的面相同。这正是“每轮局部正确、跨轮工具面变残缺”的隐藏所有权绕行。

现在 bound renderer 的返回值保持原样，不能在 publish 后被二次 filter。若存在 `ToolAuthorizer` 且它拒绝任一已发布 definition，RunLoop 在发送前以 `surface_integrity_failure` 退休同一 reservation：零 `DoVerified`、零 binder、一次 failed receipt、一次 terminal disposition；不会发送“恰好剩余可用”的子集。静态 S0.5 路径仍保留原有 filter，因为它没有 correlation-bound durable surface，且该兼容行为不在本次扩张范围。回归构造两个已发布 alias、authorizer 仅允许其中一个，断言请求完全不发送。该门禁只保证已发布 surface 与 wire surface 同一所有者，不生成 name-based authority，也不改变 D3、provider replacement certificate 或动态 alias 的关闭状态。

### 9.18 PayloadDigest 覆盖模型可见的工具描述（本次实现）

此前 canonical definition 只散列 alias 与参数 schema。虽然这能发现删工具、换 alias 与 schema 漂移，却不能发现 wire builder / SDK 将某 alias 的 `description` 改为另一能力的路由指令；模型仍会看见相同名称和 schema，receipt 却会错误通过。这不是纯展示字段：description 直接参与语义工具选择，故属于完整 callable surface。

现在 canonical definition 将非空 description 纳入 `PayloadDigest`。省略与显式空字符串仍等价，以兼容各 builder 对空描述的确定性省略；非字符串 description 直接视为 integrity failure。HTTP 回归将 `read_file` 的 wire 描述改为“运行任意 shell 命令”，断言 transport 零调用并产生失败 receipt。该改动不把 description 用作 identity、grant、alias、plan 或 successor 输入；它只收紧本次请求的 model-visible payload proof，动态 alias production gate 继续关闭。

### 9.19 function surface 拒绝 provider-visible type 漂移（本次实现）

canonical definition 先前从 `function` 内取得 alias、description 与 schema，却没有校验外层 `type`。因此 payload 可把同一个 alias/schema 从 `function` 改成 provider 的其它 tool primitive，仍通过 definitions digest。该 primitive 的模型行为、执行方与 replacement 语义都不再等价，不能借由同一 alias 混入 function surface。

现在此 contract 只接受 function type：OpenAI Chat/Responses 的显式 `type` 必须为 `function`，非字符串、空值以外的其它 type 全部在 handoff 前 `surface_integrity_failure`；Anthropic 原生转换因不携带 type 仍允许省略。HTTP 回归将 `read_file` 伪装为 `web_search_preview`，断言 transport 零调用与失败 receipt。它不扩张可支持的 provider tool 类别；若未来要接入其它 type，必须有单独的 canonical projection、wire verifier 与 replacement certificate，不能复用当前动态 alias authority。

### 9.20 correlation-bound renderer 以显式 publication proof 区分空面与 publish 失败（本次实现）

显式 `tools: []` 对 unbound S0.5 compatibility request 是合法 replacement；但对已经 reserve live channel 的 bound dynamic path，renderer 的职责是把已发布 durable surface 投影到同一 reservation。此前若 publisher/relay/holder 失败并返回 nil，RunLoop 会把它当作空面继续发送，receipt 会诚实验证空 payload，却不能证明“动态 surface 已发布且 provider 看到了它”。这把 publication failure 静默降级成 tool-less request，仍会造成多轮工具面消失。

不能用 `len(definitions)` 推断 publication 成功：`nil` 和 `[]` 在 Go 中都可能表示 host 明确发布的空 replacement；而 lifecycle fixture 也需要这个合法状态。现在新增 `PublishedBoundModelRequestToolSurfaceRenderer`，返回 `{Definitions, Published, Failure}`。`Published=true` 才表示 renderer 已将该 reservation 的 surface 显式发布，definitions 即使为空也按明确 replacement 处理；`Published=false` 则在 handoff 前以 `surface_integrity_failure` 退休 reservation：零 `DoVerified`、零 binder、一次 failed receipt、一次 terminal disposition。`Failure` 仅供诊断，不可参与 identity、grant、alias、capability 或 successor 决策。

Coding bound adapter/relay 实现该 proof，并将 definition clone 从“静默跳过损坏项”改为显式错误：发布、复制或 relay holder 任一失败均不能退化为 tool-less request。遗留 bound renderer 仍保留兼容接口，因此其明确空面不被误杀；它们不能冒充 Coding dynamic publication。回归覆盖 `Published=false` 且验证无请求字节离开 host；动态 alias/D3/production certificate 继续关闭。

### 9.21 动态 request presentation 失败必须回滚已发布 route authority（本次实现）

此前 `publishCodingDurableDynamicSurfaceForEpoch` 先通过 coordinator 原子提交 `PublishSurface`（route revision、初始 grants 与 materialization），再进行 renderer、alias copy 和 `PublishModelRequestSurface`。后半段任一失败会直接返回 error；虽然调用方不会得到 definitions，但已提交的 current route 及 exposed grant 仍存活。这不是可执行 alias 的“安全空面”：之后的重试可能误把该半成品 route 当作可复用的 current revision，且旧 reservation 的 prepared surface 也缺少统一退休。

现在 `PublishSurface` 成功后建立唯一失败补偿：renderer、随机 alias、alias clone、epoch 或 request-surface publish 任一失败，均调用 `CancelRouteSurface(scope)`。该 coordinator 事务会一并退休 prepared/active request surface、materialization 与尚未消费的 grants，并把该 revision 置为 cancelled；只有完整 request surface publication 成功才返回 holder。若取消本身失败，原始 publication error 与 retirement error 一并上报，绝不假称已清理。回归以重复 epoch 强制 request-surface publish 在 route/grant 已提交后失败，断言 route 不再 current，late predecessor response 也无法 bind。此修复只收紧 D2 test-only 生命周期，不开放 D3/production alias。

### 9.22 qualification certificate 必须与 live channel 相互校验（本次实现）

`ReplacementSemantics` certificate 原先只在 production qualification 的结构体内核验 protocol/version。即使未来由独立审批把 `Wired/Enabled` 打开，factory 仍可能按 config 选择一个 channel，而没有再次证明该**实际 reservation**正是 certificate 所覆盖的 transport；configuration 的 `WireAPI` 不能替代 live channel 的 protocol/connection，也不能证明 channel 返回 verified dispatch。

现在 `reserveCodingBoundDynamicRequestAdapter` 在打开 socket 后、读取 catalog 或发布 plan 前，校验 live channel：必须实现 `VerifiedToolSurfaceRequestChannel` 与 audit-evidence setter、实际 `{Protocol, ConnectionID}` 完整、channel protocol 与 certificate 精确相同，且固定 Responses WS factory 只接受 `Envelope=responses` 和 `cfg.IsResponsesWebSocket()`。任何不符都会先关闭 reservation 再返回；不会产生 plan/grant/alias。回归覆盖 live protocol、certificate envelope、configured envelope 和 connection 缺失四种 mismatch。该校验不能把测试中构造的 certificate 变成生产批准；当前 `codingDynamicProductionAdapterForConfig` 仍返回 `Wired=false`、`Enabled=false`，D3 继续关闭。

### 9.23 durable dynamic reservation 强制要求 publication proof（本次实现）

`PublishedBoundModelRequestToolSurfaceRenderer` 是可选扩展；若只靠 callback 是否恰好实现它来决定，未来一个 durable dynamic holder 可能被接到仅实现旧 `BuildToolsForBoundModelRequest` 的 callback。这样 renderer 失败仍可被 `[]definitions` 伪装，重新引入“已 reserve 但工具面悄然消失”。同时不能把规则扩展为所有 bound channel 必须非空或必须使用新接口：普通 bound lifecycle fixture 的显式空 replacement 是合法状态。

现在由**live holder/channel**显式实现 `ToolSurfacePublicationProofRequirement`。`codingBoundDynamicRequestAdapter` 标记其 reservation 为 durable dynamic；RunLoop 在该 marker 为真时，若 callback 不提供 proof-carrying renderer，立即 `surface_integrity_failure`、零 render/Do/bind、一次 failed receipt/disposition。未标记的 compatibility channel 保留旧 renderer contract 与合法空 replacement。marker 不由 config、URL、task、alias 或 definition 数量推断，也不提供 identity/grant/successor authority。回归覆盖“proof-required channel + legacy renderer”并断言其完全不发送；D3 和生产 alias gate 不变。

### 9.24 生命周期指标成为统一、脱敏的事件流（本次实现）

此前 receipt、attempt 与 terminal disposition 已分别存在，但没有满足第 7 节验收条件的统一指标出口；生产审计方容易只能拼接日志，或把 alias、连接 tuple、plan ID 等不应持久化的信息当作关联键。现在 `corelib/agent` 新增可选的 `ToolSurfaceEventObserver`，统一输出以下稳定事件：

- `surface_manifest_created`：每次 owner 已创建请求 surface；
- `surface_payload_verified`：最终 HTTP body/WS frame 的 receipt 已验证；
- `surface_integrity_failure`：final-payload、receipt 或 pre-dispatch 完整性门禁失败；
- `surface_replace_unsupported`：为未来经认证的 provider replacement 拒绝保留独立事件位，不能通过 string parsing 临时伪造；
- `surface_omission_reason`：仅在 correlation-bound、`PlanEvidence.Available=true` 的不可变证据中输出归一化 reason code；
- `surface_terminal_reason`：每一个 reserve channel 的唯一 terminal disposition。

事件只携带 payload/audit digest、工具计数、replacement/handoff/terminal enum、failure kind 与 omission reason code；**不**携带 definitions、schema、arguments、alias、grant、NeedID、plan ID、response ID、protocol/connection tuple 或任意原始错误文本。每个 manifest 已成功创建的 `surface_terminal_reason` 均复用**同一 immutable manifest**的 digest/计数投影，因此审计端可在不引入 response ID、alias 或连接 tuple 的前提下与同一 request surface 对账；它不能在 terminal 时重新 render 或从可变 inputs 重算。static HTTP/SSE receipt client 与 terminal event 也必须从同一次 manifest construction 接收这个投影，不能先 emit metric、之后各自再次 hash 同一 slice，否则 observer 的同步副作用可令两者分叉。发生在 manifest 前的 reservation 失败则明确没有 surface digest，并以 `failure_kind=surface_integrity_failure` 标注，绝不借用历史或部分 surface 的 digest。HTTP static S0.5、bound channel 和 MoA advisor 都接到同一事件出口；MoA fan-out 对 receipt observer 和 lifecycle event observer 分别串行化，避免 append-only 审计 sink 的竞态。事件是诊断输出，不能参与 render、authorization、bind、retry 或 successor 选择。

外部副作用的 `effect_unknown` 则属于另一条边界：`LedgerDynamicExternalEffectCoordinator` 新增独立可选 `DynamicEffectEventObserver`，只在已 admission 的 dispatch 因不确定传输错误落入 unknown 后发送脱敏 `effect_unknown` 事件。它不包含 operation key、selection/binding、arguments 或 provider receipt，且不会驱动自动重试或把 unknown 变成未执行。

回归覆盖静态多轮 surface 的 manifest/verified 事件、bound reservation 的 manifest/verified/terminal 对称性、MoA 三 advisor 并发下 receipt 与 lifecycle event observer 的单线程投递，以及 external-effect unknown 的单次事件。指标能成为 D3 的硬阻断输入：任一 integrity failure、receipt mismatch 或未来 provider 的 append/implicit merge observation 均阻断 qualification；但指标存在本身不构成 replacement certificate，更不会打开 D3 或 production dynamic alias gate。

### 9.25 S0.5 static attempt 也有逐次 terminal metric（本次实现）

第 9.24 节最初只为 reservation-bound channel 提供了 `surface_terminal_reason`。静态 HTTP/SSE 没有 durable reservation holder，但同样可能发生一轮 streaming request 后由 RunLoop 创建 non-stream fallback，或一轮临时失败后由 RunLoop 创建 outer retry。若只在整个 iteration 收敛时输出 terminal，前驱 receipt 就没有独立终态，指标无法证明“每个真实 outbound request 都已退休”。

现在 RunLoop 为每个 static owner-visible attempt 保留一次性 terminal metric：初始 stream、stream fallback 和 outer retry 都各自重新创建 manifest/receipt，并各自只输出一次 `surface_terminal_reason`。stream predecessor 在 fallback 前以 `transport_failure` 退休；fallback/retry 成功则独立 `response_settled`。此行为仅补齐诊断线性化，不让 static S0.5 获得 dynamic reservation、provider correlation、alias 或重放权限。回归验证 stream→fallback 及 stream→outer-retry 均严格输出两个 terminal events，与两次真实 wire attempt 一一对应。

为保证 failed attempt 不会在指标中看起来“从未创建 surface”，bound path 的 `surface_manifest_created` 已移动到已完成 durable publication、取得 immutable available plan evidence 后、但在 channel 接受 evidence 之前。于是 audit-evidence setter 缺失/拒绝的路径也具有完整的 `manifest_created → integrity_failure → terminal` 证据链，且仍是零发送、零 binder；manifest metric 不被 setter 当作授权输入。

### 9.26 MoA advisor static attempt 也独立结算 terminal metric（本次实现）

第 9.25 节覆盖了聚合器的普通 static HTTP/SSE 尝试，但 MoA advisor 是由 `moa.Runner` 并发发出的独立 owner-visible 请求；如果只记录它们的 manifest/receipt，而把最终 aggregator 的 terminal 当作整个 fan-out 的终态，就会遗漏每个 advisor surface 的 retirement。这会让指标无法逐请求判断 advisor 的空响应、失败或成功是否已闭合。

现在 `RunLoop` 的 `CallRef` 在每个 advisor request 返回后立即输出一个独立的 `surface_terminal_reason`：transport error 为 `transport_failure`，nil/无 choice/无 content 且无 reasoning 的 response 为 `response_abandoned`，其余被 `moa.Runner` 接受为 advice 的 response 为 `response_settled`。terminal event 始终携带该 advisor 的同一 empty-replacement payload/audit digest 与工具计数；若无法重建此确定性投影，则只输出 `surface_integrity_failure`，不发出无法对账的成功终态。该映射仅反映本地可见的 request outcome；它不声称 provider acknowledgement，不携带 connection/response identity，也不为 advisor response 赋予 bind、alias 或 successor 权限。MoA 的 receipt 与 lifecycle event observers 仍分别串行化，因此并发 fan-out 只能改变到达顺序，不能竞争 append-only 审计 sink。回归覆盖三个并发 advisor 加 aggregator 的四个 terminal events，以及 empty advisor response 的 `response_abandoned → aggregator response_settled` 序列。

### 9.27 manifest 构建失败也必须闭合 static lifecycle（本次实现）

static HTTP 与 MoA advisor 在创建 receipt client 前都先构建 immutable manifest。此前这一步若因非法 definition、policy 或 evidence 失败，只会输出 `surface_integrity_failure`，却没有对应的 terminal event；下游按 terminal 统计时会把这类 owner-visible surface 创建失败误判为悬挂请求，或错误地由后续 request 的终态“补齐”。

现在 `NewToolSurfaceReceiptHTTPClientWithLifecycleEvents` 在 manifest 构建失败时固定输出 `surface_integrity_failure → surface_terminal_reason(surface_integrity_failure)`。由于 manifest 不存在，两个 event 均不带 payload/audit digest、工具计数或 replacement projection；terminal 只带 bounded failure kind。这与 reservation 在 manifest 前失败的规则一致：不能为了可关联性拼接旧 digest、部分 schema 或 runtime identity。回归以非法 model-visible description 验证零 client/零 transport、两条脱敏事件和唯一 terminal。

### 9.28 manifest 后的 definitions 必须是 request-owned snapshot（本次实现）

manifest digest 不可变并不自动令调用方传入的 `[]map[string]interface{}` 不可变。此前 static HTTP lifecycle 在创建 manifest、发出 `surface_manifest_created` 后仍把同一 callback-owned slice 交给后续 builder；bound channel 也可能在 event observer 返回后继续持有 renderer 返回的 map。任一同步 observer、callback 或并发路径若改变 description/schema，就会形成“manifest/terminal 仍是旧 digest，而实际 wire payload 已是新面”的 TOCTOU。最终 receipt 会拒绝一部分 static 情况，但 bound channel 仍可能将改变后的定义交给自己的 `DoVerified`；正确性不能依赖 observer 恰好不修改输入。

现在在 lifecycle manifest 创建前将 JSON-shaped definitions 做一次深拷贝，不能序列化的值直接 fail-closed；static initial request、stream fallback 与 outer retry 均以同一 frozen snapshot 完成 JSON 序列化。冻结发生在 input-breakdown、manifest、receipt 等所有诊断 observer **之前**，所以任何同步计量回调都不能在 telemetry 与 wire 之间改变 callback-owned definitions。尤其 outer retry 是新的 outbound attempt：它先冻结本次重新 render 的定义、再做 input accounting、再创建 manifest/receipt，绝不复用 predecessor 的 slice 或让 retry observer 改写 baseline。correlation-bound renderer 在发出 manifest event 前同样冻结其 model-facing presentation；传入 `DoVerified` 的又是该 baseline 的独立副本，因此 channel/SDK 即使错误地改写自己的输入，也不能让 RunLoop 随后按被改写的 map 重新计算 receipt。`nil`/空 slice 继续统一为显式 `tools: []` replacement，不把空面当作发布失败。回归让 input-breakdown 和 `surface_manifest_created` observer 分别故意修改 callback 的 description：static transport 必须仍收到原 description；outer retry 的两个独立 render 分别仍发送各自的原 description；bound dispatch 必须验证、bind 并以原 manifest digest 结算，不能把 observer 修改后的定义送出；另一个回归让 channel 修改其传入 slice，receipt 与原 baseline 不一致时 binder 必须零调用。该修复只冻结一次请求的 presentation，不给 digest、observer 或 snapshot 增加 identity/grant/successor 语义，也不改变 D3 的 provider conformance 与动态 alias 关闭状态。

retry 的 freeze 失败发生在 successor manifest 之前，因此不应借用 predecessor digest，也不得在 common error path 再写一条伪造的 `transport_failure` terminal。现在 retry 在准备 successor 时清除 predecessor manifest projection；freeze 或 lifecycle construction 失败只输出一次无 digest 的 `surface_integrity_failure` terminal，并标记该静态 slot 已结算。回归以不可 JSON 序列化的 retry-only schema 验证：stream/fallback 两个已发送 predecessor 各自保持自己的 `transport_failure`，失败的 retry 只产生一条未关联 digest 的 integrity terminal，零第三次 wire request。

### 9.29 static lifecycle 的 AuditDigest 必须贯穿最终 wire receipt（本次实现）

此前 static lifecycle 已从同一次 immutable manifest 产生 `surface_manifest_created` 与 terminal，但 lifecycle client 在 `RoundTrip` 时默认用空的 audit evidence 重算 receipt。这使一个调用方合法提供 `PlanEvidence.Available=true` 时，manifest/omission event 的 `AuditDigest` 会和同一 HTTP request 的 verified receipt 分叉；虽然 payload proof 仍正确，却破坏了审计双投影的“同一请求同一事实”约束。

现在 lifecycle 构建只保存归一化后的 request-owned audit evidence，并将其传给最终 HTTP wire verifier；该 evidence 仍不会参与 definitions、invocation policy、authorization、bind、alias、identity 或 successor 决策。兼容的非-lifecycle receipt client 继续使用明确的 unavailable evidence。回归以含 omission 的 available evidence 断言 manifest、omission event 与最终 verified receipt 的 `AuditDigest` 完全相等，并额外断言 wire surface 被拒绝时的 failed receipt 仍使用同一审计摘要；因此无论静态调用方未来何时取得合法 plan evidence，审计链都不会因 final-boundary 重算而分叉。动态 production gate、D3 provider conformance 和 alias fail-closed 状态均不变。

### 9.30 AuditEvidence 的 receiver 必须保存 canonical snapshot（本次实现）

仅在入口做 validation 不足以形成 immutable evidence：`ToolSurfacePlanEvidence.Omitted` 是 slice。若 WS channel 或 holder 直接保存 caller 传入的结构，调用者可在 `SetToolSurfaceAuditEvidence` 返回后改写 omission；同一 reservation 最终产生的 receipt 就会与已发布 manifest 的审计摘要不同。这是 audit-only 字段的 TOCTOU，不能用“它不影响授权”来忽略，因为它会破坏对预算/策略省略事实的追溯。

现在 corelib 暴露 `NormalizeToolSurfacePlanEvidence`，一次完成校验、排序、去重和 omission slice 的 copy；保存 evidence 的 Responses WS channel、Coding bound adapter 及测试 transport 都只保留这个 canonical snapshot。RunLoop 还会在 manifest observer 之前将 correlation-bound provider 的 evidence 归一化，并用同一 snapshot 创建 manifest、交给 channel、复验 dispatch receipt，避免 observer 改写 provider-owned slice。WS 回归在 setter 返回后改写调用方的 omission reason，bound 回归在 manifest event observer 中改写 provider-owned omission reason，最终 verified receipt 都必须等于改写前的审计摘要。WS final-payload parser 在缺 tools、tools 类型非法或 invocation policy 无法解析时，也会返回带同一 `PayloadDigest`/`AuditDigest` 的 failed receipt，而不是用 payload digest 伪装 audit digest。该更新不让 audit evidence 成为 route key、identity、grant、绑定或 successor 输入；动态 alias production gate 继续关闭。

### 9.31 retry backoff 被取消时不得伪造或复用 successor surface（本次实现）

streaming predecessor 发生可重试错误后，RunLoop 会先以 `transport_failure` 退休该 predecessor；若在 retry backoff 期间收到 stop，系统没有创建 successor manifest、receipt 或 transport handoff。这个分支必须直接结束 request context，不能再调用 `disposeSurface`：后者会把已经退休的 predecessor 再次结算，或让审计端误以为存在一个未渲染 successor。

现在该分支显式保持 predecessor 的单次 terminal，不创建第二次 disposition、manifest 或 wire attempt。回归模拟首个 request 返回 503、随后 host stop，断言仅有一次真实 request、一次 attempt start/ambiguous finish；因此取消不会触发隐藏 retry，也不会借用旧 surface 生成新生命周期。此修改同样不开放 dynamic alias 或改变 D3 的外部 provider conformance 门禁。

### 9.32 WS frame 无法解析时也必须保留 request-owned receipt projection（本次实现）

Responses WS 的 final verifier 过去在 `json.Unmarshal(response.create frame)` 失败时返回只有 failure 文本的空 receipt。对于已存在 immutable plan evidence 的 correlation-bound request，这会导致 channel 返回的 failed receipt 缺少 `PayloadDigest`、`AuditDigest`、工具计数与 replacement mode；manifest 已创建但失败事件无法对账，且诊断层容易将它误认成未创建 surface 的另一类错误。

现在 WS verifier 在 frame JSON 无法解析时仍复用 corelib 的 request-payload failure path，依据已冻结的 rendered definitions、policy 与 evidence 生成失败 receipt。回归以 malformed frame 断言它不通过、不写 transport，同时保留与同一 rendered manifest 完全一致的 payload/audit digest、工具计数与 replace mode。它不将 receipt 变成绑定授权；RunLoop 仍要求 verified + matching receipt 后才会 bind，D3/provider conformance/dynamic alias gate 都不变。

### 9.33 标准库 transport 的隐式重放不得越过 owner（本次实现）

最终 HTTP receipt verifier 在把 body 交给 concrete `RoundTripper` 前，主动清除 `request.GetBody`。这不是普通请求构造层的兼容性选择，而是 owner 边界：Go 的 HTTP/1 transport 会在复用连接失败时对具备 `GetBody` 的可重放请求发起透明重试；若保留它，同一 manifest/receipt/terminal 可对应两次真实 payload send，而 RunLoop 无法为第二次 send 创建 successor surface。

清除 `GetBody` 后，HTTP/1 无法重绕已消费的 tool-bearing body；HTTP/2 对 body 已进入发送路径后的 retry 亦会因无 `GetBody` 返回错误。HTTP/2 仍允许在连接于**读取 body 之前**已不可用时复用同一 request，这不构成第二次 payload handoff，因而不需要也不能伪造 successor surface。任何已可能发送 bytes 的 transport error 都维持 `handoff=ambiguous` 并返回 owner；只有 RunLoop 的 outer retry 才能冻结新的 definitions、创建新的 manifest/receipt，并输出新的 terminal。redirect 则在 receipt wrapper 的第一跳直接拒绝，避免 `http.Client` 把 redirect 变成未经 reservation 的第二次请求。回归断言 final concrete transport 看不到 `GetBody`，并以真实 redirect server 断言目标地址零请求。该约束不关闭 HTTP/2、不中断普通无工具 client，也不改变 D3、provider conformance certificate 或 production dynamic alias gate。

### 9.34 SDK raw-body 包装层不得重新打开 transport replay（本次实现）

OpenAI SDK 的 raw-body adapter 为了让 SDK 按 host 已构建的 JSON 发送，会在其 `RoundTrip` 中替换 request body。它此前同时重建了 `GetBody`；即使外层 receipt verifier 已清空该字段，SDK adapter 仍可能在它之后把请求重新标记为可重放，令标准库 transport 在复用连接错误时发出 owner 看不见的重复 payload。

现在 raw-body adapter 仅在 request-owner context 禁止透明重试时保持 `GetBody=nil`；普通 SDK 调用仍保留原有 307/308 兼容语义。最终 receipt wrapper 无论 context 都会在它的 final wire boundary 清除该字段，因此 adapter 即使位于 receipt wrapper 外层，也无法重新标记 owner-bound payload 为可重放。回归先构造带 caller `GetBody` 的 request，再经过带 owner context 的 raw-body adapter，断言其 concrete transport 只看到精确 host body 且 `GetBody` 为空。SDK 的 API-level retries 仍由 request context 的透明重试禁令和 RunLoop owner lifecycle 控制；本条只封闭标准库 transport replay 的下层旁路，不构成 provider replacement certificate，也不改变 D3/dynamic alias gate。

### 9.35 dormant SDK stream path 也必须继承 owner transport policy（本次实现）

`openAISDKChatStreamUnused` 虽未被当前生产分派调用，但它仍会构造 SDK client 与 raw-body transport。此前该函数没有在构造 client 前调用 `HTTPClientForRequestContext`；将来若被重新接线，owner context 即使已经声明禁用透明重试，底层 `http.Client` 仍可能按自己的 redirect 策略把 307/308 变成第二次请求。

现在该路径在所有 SDK 包装之前先取得 request-scoped client，因此 owner-bound context 会拒绝 redirect；raw-body adapter 同时保持 `GetBody=nil`。回归通过真实 307 server 触发这个 dormant stream path，并断言 redirect 目标收到零请求。该修复只是封闭未来启用时的 transport 旁路，不等同于 provider replacement semantics certificate，也不改变 D3、production dynamic alias 或 kill-switch 的未完成状态。

### 9.36 host-owned invocation policy 已贯穿 Chat / Responses 请求构建（本次实现）

此前 `RunLoop` 仅根据 config 生成 `provider-default` manifest policy；虽然 final receipt 会检查实际 wire，但 builder 没有由同一 owner 接收 `tool_choice` 与 `parallel_tool_calls`。这意味着 host 若需要 `required`、指定函数或明确禁止 parallel call，就只能寄希望于 `ExtraBody`/pass-through map，既不能作为 immutable manifest 的事实来源，也可能在 provider sanitization 前后漂移。

现在新增可选 `ToolSurfaceInvocationPolicyProvider`：host 在每个真实 outbound request 前返回完整 policy，RunLoop 先校验其 envelope 与本次 transport 一致，再把同一值用于 manifest、HTTP receipt、verified channel receipt 复验、stream fallback 与 outer retry。Chat / Responses builders 均以 typed options 接收 choice 与 optional parallel 值；`ExtraBody` 仍不能覆盖这两个 reserved key。`nil` 和 `false` 保持不同，specific choice 分别投影到 Chat nested function 与 Responses flat function shape。回归覆盖 Chat wire 上的指定函数 + `parallel_tool_calls:false`，以及 envelope 不匹配时零 request 的 fail-closed 行为。

Anthropic 尚无经证书审查的同等 policy projection；如果 host 为其提供非 provider-default/absent policy，RunLoop 在发送前拒绝，不能把 manifest 中的限定静默丢弃到 wire。该切片消除本地 policy 所有权旁路，但并不产生真实 provider `ReplacementSemantics` certificate，动态 alias production gate 与 D3 仍保持关闭。

### 9.37 Responses WebSocket 也必须接收同一份 host-owned policy（本次实现）

9.36 之后仍发现一个根本性旁路：`RunLoop` 可以为关联绑定的 request 创建带 host policy 的 manifest，但 Responses WebSocket channel 的最终 `response.create` frame 仍在内部固定采用 provider-default。这会让 manifest / receipt 复验与真实 WS wire 不一致；更糟的是，若两者都默认，问题会被普通回归掩盖，直到多轮 host 改为 `required`、指定函数或 `parallel_tool_calls:false`。

现在关联绑定 channel 必须实现 `ToolSurfaceInvocationPolicyRequestChannel`。RunLoop 只在 immutable manifest 已创建后交付已校验的 policy；不支持该交付或试图在 dispatch 后修改 policy 均以 `surface_integrity_failure` 关闭 reservation。Responses WS channel 将 policy canonical snapshot 与 audit evidence 分别冻结，并在 `response.create` 最终 JSON 上按 Responses flat specific-function 形状投影 `tool_choice` 与 optional `parallel_tool_calls`，再用**同一 policy**生成 receipt 后才 `WriteMessage`。holder 也只能透传，不能从 cfg、URL、模型名或 provider map 推导默认值。

新增回归覆盖 Responses HTTP wire、Responses WS final frame 和 Anthropic 非默认 policy 的发送前拒绝；同时保留 direct transport fixture 显式安装 provider-default policy，避免测试把“未声明 policy”误当作合法默认。该修正只是补齐 local policy ownership；真实 provider 的 replacement certificate、fixed cohort、kill-switch 演练以及 dynamic alias production qualification 仍未完成，`codingDynamicAliasesMayMaterialize()` 继续为 `false`。

### 9.38 dynamic production qualification 必须验证 policy-handoff 能力（本次实现）

仅让 future dynamic factory 检查 live channel 的 verified dispatch、audit evidence、protocol 和 connection ID 仍不充分。若该 channel 不具备 policy handoff，factory 即使拿着带 `PolicyProjectionVersion` 的 replacement certificate，也可能认证一个最终固定 provider-default 的 serializer；这会使 certificate/manifest 所说的 `required`、specific function 或 explicit parallel 值根本无法抵达 wire。

现在 `validateCodingDynamicQualifiedRequestChannel` 将 `ToolSurfaceInvocationPolicyRequestChannel` 列为与 verified dispatch、audit setter 并列的硬门槛。新的负例 fixture 刻意实现 correlation、`DoVerified` 和 audit setter，却省略 policy setter，验证其被拒绝。该结构性检查不构成真实 provider certificate，不开启 production wiring，也不改变 `Wired=false`、`Enabled=false` 与 alias zero-materialization；它只防止未来误把本地 primitive 当作完整 dynamic adapter。

### 9.39 holder 直调也不得绕过 policy snapshot（本次实现）

factory 与 RunLoop 的 handoff 都已收紧后，test-only holder 的直接 `DoVerified` 仍是一条独立入口：它会自行补齐 audit evidence，却未证明 policy 已由 manifest owner 交付。未来若有人在复用 holder 的测试/接线中直接调用该入口，底层 channel 就可能退回 provider-default，重新形成“manifest 预期与最终 wire 不同”的旁路。

现在 holder 自身保存 normalized 的 invocation-policy snapshot，并在任何 direct dispatch 前要求它已设置；缺失 policy 会在零 transport send 时以 `surface_integrity_failure` 关闭 reservation，之后也不能补设 policy 复用该 socket。重复设置只能接受相同 canonical value，dispatch 开始后任何变更均被拒绝。回归验证未设 policy 的 direct holder dispatch 零调用底层 transport、并且 terminal reservation 拒绝 late policy。此机制只维护 request-local wire ownership；不提供 provider conformance、identity、grant 或 successor authority，production dynamic alias 仍关闭。

### 9.40 audit evidence 与 invocation policy 必须原子交给关联 channel（本次实现）

先前 RunLoop 依次调用 policy setter、audit setter。两者虽都在 write 前完成，但这是隐含的调用顺序约定：未来 transport 若在第一个 setter 后意外启动，或 setter 有副作用，reservation 可能以半配置状态被观察，造成 audit/policy 与 manifest 的组合不再是同一个 immutable request record。

现在引入 `ToolSurfaceDispatchPreparation`，包含 normalized audit evidence 与 invocation policy；correlation-bound RunLoop channel 和 qualified factory 必须支持一次性 `SetToolSurfaceDispatchPreparation`。Responses WS channel 在同一锁内验证、比较、保存整个 pair；bound holder 也仅原子转交同一个 reservation 已验证的 pair。旧单字段 setter保留为 direct fixture/compatibility seam，但不再满足 RunLoop 的关联 dispatch 或 future qualified factory。回归覆盖 changed preparation 被拒绝、随后 reservation 仍不会发送。该改动收紧 host 内部线性化，不为 provider 的 stateful replacement 行为提供证书，也不改变 dynamic alias production gate。

### 9.41 移除单字段 preparation API，禁止 compatibility 回退（本次实现）

9.40 若仍保留 audit-only / policy-only setter，即便 RunLoop 不再调用，未来代码仍可通过 type assertion 回退到两次 setup；这会把“必须原子”的约束重新降格为约定，并让测试无意中验证一条 production 不允许的路径。

现在已删除单字段 preparation interface 及 Responses WS、bound holder、test transport 的对应方法；相关 direct transport fixtures 也改为显式构造完整 `ToolSurfaceDispatchPreparation`。因此 correlation-bound dispatch 的唯一配置形态就是同一 immutable pair，不能再从 audit/policy 任一半边开始。该 API 收束不影响 static HTTP 的 unavailable audit state，也不开放 dynamic alias、provider certificate 或 D3。

### 9.42 specific tool choice 必须属于同一 immutable definition surface（本次实现）

此前 canonical policy 仅校验 `specific` 带有非空函数名。若 host 或 builder 指定一个当前 `tools` 中不存在的名字，definitions 与 policy 各自都可能形状正确并生成一致 digest，但 provider 实际看到的是一个自相矛盾的 callable surface：指定函数没有在 replacement 中被声明。这种悬空 selector 在多轮裁剪/重渲染后尤其容易造成“工具面不完整”的隐蔽语义漂移。

现在 manifest construction 在 definitions canonicalization 后验证：任何 `ToolChoice=specific` 的 name 必须出现在同一 immutable replacement definitions 集合中；否则 manifest/final verifier 在 handoff 前生成失败 receipt 并拒绝发送。回归覆盖 Responses flat specific choice 指向未渲染函数时的拒绝，同时保留已渲染 native projection 的通过路径。该约束只校验同一次 model-visible surface 的自洽性，不以 alias/name 作为跨轮 authority，也不代替 provider replacement certificate。

### 9.43 required policy 不得与空 replacement surface 并存（本次实现）

即使 specific selector 已验证属于 definitions，空 `tools:[]` 配合 `tool_choice:"required"` 仍是另一个不可满足的 policy：它要求模型调用工具，却不给任何可调用 function。若让这类组合通过，provider 的兼容层可能自行放宽 required、保留历史工具或返回无法解释的错误，都会让本轮 manifest 无法代表真实 callable surface。

现在 manifest construction 也拒绝 `required + empty tools`；final HTTP/WS verifier 因此在发送前给出失败 receipt。`auto`、`none` 与 provider-default 的显式空 replacement 仍保持合法且可被审计。回归覆盖 Responses 空面 required 的拒绝。此修正只排除 host 已知不满足的组合，不推断 provider 的默认行为，也不替代 replacement certificate 或 D3 gate。

### 9.44 Chat streaming fallback 必须重投影 successor invocation policy（本次实现）

streaming Chat request 失败后发出的 non-stream fallback 是一个新的真实 outbound request；它既不能复用 predecessor 的 definitions/manifest/receipt，也不能复用 predecessor 已投影的 `tool_choice` 或 `parallel_tool_calls`。此前 `doLLMRequestWithToolsStreamWithBeforeFallback` 虽然从 callback 取得了 successor policy，却在闭包外沿用了初始 stream 计算出的 Chat options。于是 successor manifest/receipt 可以属于新 policy，而 fallback wire 仍携带旧 policy；final receipt 通常会拒绝该请求，但正确性不应依赖最后一道探测性拒绝。

现在 Chat fallback 在 `beforeFallback` 成功返回 successor surface、HTTP client 与 policy 后，重新通过 `toolSurfacePolicyRequestOptions` 投影 `tool_choice` 和 `parallel_tool_calls`，再构造 non-stream request。Chat 与 Responses 回归都让 predecessor 使用 `tool_choice:auto`、`parallel_tool_calls:true`，让 successor 改为指定 `only_tool` 且 `parallel_tool_calls:false`，并断言第二个真实 HTTP body 分别携带各自 native projection（Chat nested function，Responses flat function）。Anthropic 仍只接受 provider-default/absent policy，故不存在未经审查的本地投影。此修复不开放 legacy dynamic by-name/name fallback、`manage_skill` 或 `call_mcp_tool`，也不改变 dynamic alias production gate、真实 provider certificate 或 D3 qualification 状态。

### 9.45 fallback callback 必须原子返回完整 successor preparation（本次实现）

9.44 修正了 Chat 分支的旧 policy closure capture，但原 callback 仍以四个平行返回值交付 `definitions`、HTTP receipt client、policy 和 allow flag。该形式无法在类型层面说明三项是同一次 successor request 的不可分记录；未来任一调用点都可能只替换其中一项，重新把 predecessor client、manifest 或 policy 混入 fallback。

现在 callback 返回 `toolSurfaceFallbackPreparation` 与结果状态；preparation 是一个完整 successor surface 的最小闭包：定义、为该定义创建的 receipt HTTP client、以及同一次 host-owned invocation policy 必须一并交付；三种 envelope 仅在 preparation 成功后从同一对象更新局部请求状态。它不承载 digest、identity、alias、grant、plan 或 response binding；这些仍由 request-local lifecycle / receipt 和外层 RunLoop 负责。该接口收束仅防止内部 partial-update 漂移，不签发 provider replacement certificate，也不改变动态 alias 的 fail-closed production gate。

### 9.46 fallback successor preparation 失败也必须独立闭合（本次实现）

fallback 的 predecessor streaming attempt 已在构造 successor 前结算。如果 successor 的 policy 取得失败，或其 definitions 无法形成 lifecycle manifest，旧 callback 的 `allow=false` 只会返回原始 stream error；这样外层既无法区分“无 successor”与“successor integrity failure”，也可能把已经退休的 predecessor manifest 当成 common error path 的终态。这会让审计流缺少一轮 prospective outbound request 的失败闭包。

现在 fallback callback 返回 `(preparation, error)`：明确的 ambiguous-delivery containment 使用私有 `fallback suppressed` sentinel，保留原始 transport error 且不虚构 successor；所有实际 preparation 失败则返回 `surface_integrity_failure`。在开始构造 successor 前，RunLoop 清除 predecessor manifest projection；policy 失败显式发出无 digest 的 `integrity_failure → terminal(surface_integrity_failure)`，lifecycle manifest 失败复用 lifecycle 自己的同一闭包，二者都不会随后被外层重标为 `transport_failure`。回归分别覆盖 successor policy envelope 不匹配和非法 successor description，均断言零第二次 HTTP send、先前 streaming manifest 的一次 `transport_failure`，以及独立、脱敏的 successor integrity terminal。该收紧只维护 request 生命周期，不提供 alias、grant、identity、provider replacement certificate 或 D3 资格。

### 9.47 首次请求的 host policy 预验证失败也必须闭合 static lifecycle（本次实现）

`ToolSurfaceInvocationPolicyProvider` 在第一次 renderer、manifest 和 receipt 之前运行。此前它返回错误（例如 host policy 的 envelope 与当前请求不一致）会正确做到零 HTTP send，却直接从 RunLoop 返回；生命周期观测看不到该次 request-surface creation 已被 host 拒绝，和 manifest 构建失败、retry/fallback successor preflight failure 的闭合规则不一致。

现在 RunLoop 在请求 timeout/context 建立后立即取得 event observer；任何初始 policy 预验证失败均发出无 payload/audit digest 的 `surface_integrity_failure → surface_terminal_reason(surface_integrity_failure)`，随后结束 host request context。它不会渲染 definitions、创建 manifest、调用 transport、预留 channel 或借用先前 request 的 digest。回归以 Responses policy 投影用于 Chat request 的 envelope mismatch 验证零服务器请求和恰好两条脱敏事件。此改动只让 pre-manifest failure 可审计，不将 telemetry 用作授权，也不改变 dynamic alias、provider certificate 或 D3 gate。

### 9.48 live reservation 获取失败也必须闭合 pre-manifest lifecycle（本次实现）

correlation-bound host 的 `ReserveToolSurfaceRequestChannel` 可能在获得 live socket/channel 前失败。此前 RunLoop 直接返回 `LLM request channel failed`；没有 channel tuple、manifest 或 transport send 是正确的，但观测侧同样无法区分“本轮没有尝试”与“尝试创建受所有权约束的 surface 时失败”。这与文档规定的 pre-manifest reservation failure 必须有唯一终态不一致。

现在 reserve error 在结束 host request context 前输出无 digest、无工具计数、无 replace mode 的 `surface_integrity_failure → surface_terminal_reason(surface_integrity_failure)`，并向调用者明确标注 `surface_integrity_failure`。由于 reservation 未取得，不调用 channel close/disposition observer，也不生成或补造 protocol、connection、epoch、plan、receipt 或 alias。回归让 reserve provider 直接报错，断言一次 reserve、零 HTTP transport send 和恰好两条脱敏事件。该修复只是诚实地闭合请求创建失败，不能作为 live channel、provider replacement semantics 或 D3 evidence。

### 9.49 host request context 创建失败也必须闭合 pre-manifest lifecycle（本次实现）

`LLMRequestContextProvider` 是 RunLoop 进入每轮模型请求的最外层 host scheduling/cancellation boundary。此前其返回 error 时会在 policy、reservation、manifest 和 transport 之前直接退出；这保持了零发送，却使生命周期流遗漏一次 host 已拒绝的 request creation，和其它 pre-manifest failure 的统一审计规则不一致。

现在 context provider failure 输出无 payload/audit digest、工具计数和 replace mode 的 `surface_integrity_failure → surface_terminal_reason(surface_integrity_failure)`，并以 `surface_integrity_failure` 返回给调用者。该路径不调用 provider 给出的完成回调（该回调不存在）、不构造 fallback/successor，也不会生成 context、policy、channel、manifest、receipt、tuple、plan 或 alias。回归让 context provider 直接失败，验证只调用一次 provider、零 HTTP send 与恰好两条脱敏事件。它只闭合 host 侧 request-creation audit，不把 context 失败转换成 provider conformance 或 D3 evidence。

### 9.50 verified dispatch 声称已 handoff 时必须提供 response 或 error（本次实现）

correlation-bound channel 的 `DoVerified` 是 response bind 前唯一可信 dispatch 边界。receipt 即使已验证且 `handoff=started`，若 channel 返回 `Response=nil, error=nil`，RunLoop 既没有可消费的 provider response，也没有能说明读取/transport 失败的 error；此前会继续离开 dispatch 分支并在访问 choices 时产生 nil dereference，或将 channel contract 破坏误解为正常空响应。

现在 RunLoop 在 matching receipt 和 `handoff=started` 之后，强制 `Response != nil || error != nil`。`nil,nil` 以 `surface_integrity_failure` 退休同一 reservation、关闭 channel，并且零 binder/零 executor；started + real error 仍按已有 transport failure 处理，ambiguous handoff 仍不允许 bind。回归构造一个产生有效 receipt、started handoff 却返回 nil response 的 channel，断言一次 dispatch、一次 close、唯一 integrity disposition 和零 binder。该检查只完整化本地 dispatch contract，不生成 provider acknowledgement、replacement certificate、alias 或 D3 evidence。

### 9.51 static HTTP/SSE adapter 也必须满足 response-or-error 契约（本次实现）

9.50 只覆盖了 correlation-bound `DoVerified`。静态 HTTP/SSE/stream-fallback 路径虽由当前 parser 通常保证返回 response 或 error，但 `RunLoop` 仍在统一消费点直接读取 `resp.Usage` 和 `resp.Choices`；任何未来 adapter 若错误地返回 `(nil, nil)`，会绕过正常空响应语义并造成空指针，而不是为已经发起的同一请求写入一个明确终态。

现在所有 request path 在 transport error 已结算、任何 response 字段被读取之前共同检查 `resp != nil`。缺少 response 且没有 error 被归类为 `surface_integrity_failure`，并先记录 `ambiguous_delivery`，再以当前 request 的 manifest/receipt（若已创建）闭合；它不会绑定 response、调用 executor，或被解释为 `choices:[]`。这条防线不改变现有 parser 的正常空 choices 行为；`choices:[]` 仍是 provider 返回的 `response_abandoned`。标准 Go HTTP client 会把低层 RoundTripper 的 `(nil, nil)` 转成 error，因此该通用 guard 主要封闭 future SDK/protocol adapter 的契约旁路；它不提供 provider replacement certificate、alias、grant 或 D3 qualification，production dynamic alias gate 继续关闭。

### 9.52 MoA advisor 不能把缺失 response 降级为空回复（本次实现）

MoA reference fan-out 虽然不进入主聚合器的 response bind，却是独立的真实 outbound request，并且拥有自己的静态 explicit-empty manifest、receipt 和 terminal event。9.51 的主循环 guard 不能自动覆盖其 `CallRef` 闭包；此前某个 future advisor adapter 若返回 `(nil, nil)`，runner 会将它写成 `empty response`，生命周期也误标为 `response_abandoned`，从而掩盖已发送请求没有得到可消费结果的 adapter 合约破坏。

现在 advisor `CallRef` 在完成 non-stream dispatch 后复用同一 response-or-error guard。`nil,nil` 返回明确的 `surface_integrity_failure` 给 fan-out，并为该 advisor 自己的 manifest 发出 `surface_integrity_failure` terminal；真正的 `choices:[]` 仍保持 `response_abandoned`。这不把 MoA response 当作 correlation-bound identity，也不向 advisor 授予工具执行权；它只保证每个 owner-visible request 对同一种 contract violation 有一致、不可降级的终态。

### 9.53 已消费的空 choices 不能被留作未结算 attempt（本次实现）

`choices:[]` 与 `(nil,nil)` 不同：前者已经从当前 request 得到了可解析的 provider response，只是没有可供 loop 使用的 choice。此前 RunLoop 会正确将 surface 终结为 `response_abandoned`，但在 `finishAttempt(response_consumed)` 之前提前返回；带 attempt observer 的 host 因而留下“started 无 finish”的记录，后续审计可能把它误读为 ambiguous delivery，破坏“每个真实 outbound request 恰有一个 delivery state”的观测不变量。

现在空 choices 在写入 response ID、退休 `response_abandoned` 之前先记录 `response_consumed`。这不把空回复接受为最终回答，也不允许 retry/redirect/successor 复用其 receipt；它仅精确区分“已收到但不可消费的 response”与“请求结果未知”的 delivery 状态。回归同时断言一个 start、一个 `response_consumed` finish 与原有的 no-choices 错误。

### 9.54 空 assistant turn 必须在 recovery/backoff 前退休当前 surface（本次实现）

`choices` 非空但 assistant content、reasoning 和 tool calls 全为空时，loop 会进入空回复 recovery：可能等待、接受 live steering，或注入 recovery prompt 发起下一轮 request。此前当前 surface 只有在 hard-exit 或最后迭代分支才调用 `response_abandoned`；正常 recovery 的 `continue` 会让它保持未终结，下一轮 manifest 从而在同一逻辑回合并存，违背每个 response 都必须有唯一 terminal 的所有权规则。

现在一旦识别为无可用 assistant turn，RunLoop 立即以 `response_abandoned` 退休当前 surface，再进行 backoff、steer 检查和 recovery prompt 构造。后续所有分支都只是在已退休 surface 后决定是否创建新的 request，不会再次结算同一 terminal。回归覆盖 `maxIter=1` 的空 assistant turn，断言唯一 `response_abandoned` terminal；该规则同样适用于多轮 recovery，且不把空 turn 变成已结算的最终回答。

### 9.55 response binding 失败必须在 executor 之前拒绝整份 response（本次实现）

此前 binder 失败仅把错误写入 `ToolCallExecutionContext` 后继续处理 response，并依赖动态 executor 自己拒绝。这不是可靠的 request ownership 边界：任意实现了 binder、却仍使用旧 `ExecuteTool` / `StructuredToolExecutor` 的兼容 host 并不会读取该 metadata，因而可能在 `response_abandoned` 已退休同一 surface 后执行模型调用。此路径直接违反“没有 matching response binding 不得 dispatch”。

现在 `BindToolSurfaceResponse` 返回错误时，RunLoop 立即以 `response_abandoned` 退休当前 surface，并返回 `surface_integrity_failure`；不会写入 assistant history、进入 batch checkpoint 或调用任何 executor。回归构造 receipt 已验证但 binder 失败且 response 含 tool call 的 bound channel，断言 binder 只调用一次、terminal 仍为唯一 abandoned，且所有 tool executor 调用为零。此改变不把 binder failure 伪装成 provider transport failure，也不允许旧 alias/name/compatibility dispatcher 绕过 binding。

### 9.56 语义 dependant release 必须晚于 batch 的 durable commit（本次实现）

本轮继续审计已绑定 response 进入 tool batch 后的 early-return 与 durability 边界时，发现 shared IM callback 曾在 `OnToolBatchCommitted` 一进入时、以及 `OnToolBatchAbandoned` 时释放 `semanticHoldDependantIssue`。前者会在 `persistRecoveryCheckpoint` 失败时，把由本批 search 等结果派生的 dependant grant 暴露出来，而 durable recovery 仍只保留 pre-tool 的 `external_uncertain` checkpoint；后者还会把取消、超大参数、hard-stop 等未完成批次误当成可发布 successor 的来源。这是“surface 已结束”与“结果已 durable commit”混淆，可能使下一轮工具面包含无法由恢复记录证明的 capability。

现在 release 被线性化到真实 durable 边界：完整 batch 只有在 `persistRecoveryCheckpoint` 成功且 `hasPendingToolBatch=false` 后才释放；`ask_user` / `record_audio` 只在 paired interactive history 与同一 run 的 checkpoint marker 原子落盘后释放；abandoned、starter failure、committer failure、cancel 和 hard-stop 一律保留 hold/need 状态以及原 recovery marker。新增回归模拟 run-scoped checkpoint conflict，断言 failed commit 和 abandoned batch 都不会释放 dependant issue；已有成功 batch 与 interactive 持久化路径仍在成功落盘后正常释放。此修正不把 hold 当作 alias 授权，不开放 production dynamic alias，也不构成 provider replacement certificate 或 D3 evidence。
### 9.57 终态附件投影不得越过未提交 batch 的 durable 边界（本次实现）

`attachSharedLoopArtifacts` 先前会在终态响应构造时调用 host-owned 的 PDF 生成、图片渲染和当前通道文件交付。即使 `releaseSemanticDependantIssue()` 保持 dependant hold，这些 fallback 仍可能根据内存中的 assistant report 直接 `refreshSemanticCallSurface` 并消费 successor grant。于是，尚只保存 pre-tool `external_uncertain` checkpoint 的 batch 可以在终态投影中产生新的外部可见效果。

现在附件投影在 `hasPendingToolBatch` 为真时整体 fail-closed：不 release、不 materialize successor grant，也不执行 host-owned 生成或交付。只有完整 tool batch 已由 `OnToolBatchCommitted` 持久化，或 interactive pause 已原子持久化 paired history 并清除 pending 标记后，终态投影才能继续。新增回归覆盖真实 search→PDF dependency，证明 pending 状态下没有 PDF、没有 generate grant，且 hold/need 均保留。

### 9.58 checkpoint 写入失败也必须关闭 callback-local successor（本次实现）

`hasPendingToolBatch` 仅在 **pre-tool checkpoint 成功**后才会置位；因此它不能代表 pre-tool checkpoint 本身写入失败的情况。此时 core loop 会停止且没有工具执行，但 callback 仍可能保留上一批已经 issued 的 grant 或本地 dependant 状态。若终态投影只检查 `hasPendingToolBatch`，它仍可能从这些内存状态消费 grant 或刷新 successor，等于把一次明确的 durability failure 当成了可继续发布 authority 的边界。

### 9.60 settled request surface 与 cancelled route authority 必须分离（本次实现）

审计 D2 holder/relay 的终态路径发现，`response_settled` 与 `tool_batch_settled` 曾和 steer、transport failure、runtime cancel 共用 `Close(non-nil)`；后者最终调用 `CancelRouteSurface`。这会把已被 `OnToolBatchCommitted` 持久化的完成事实所在 revision 写成 `cancelled`，既阻断该 revision 上根据已完成 DAG 节点产生的 successor surface，也把“本 request 已结束”错误升级为“整个 route 已撤销”。

现在 coordinator 提供 `FinishModelRequestSurface(epoch)`，只将该具体 **active** presentation 标记为 `finished`，让旧 response 的 alias 不可恢复、不可 resolve，同时不撤销 current route、materialization 或仍有效的 grant。该转换仅对已 `finished` 的同一 epoch 幂等；prepared、cancelled、superseded 或不存在的 epoch 都明确拒绝，避免并发取消被静默记录成 settled。通用 `RetireModelRequestSurface` 也不再接受 `finished` state，消除 future caller 以 prepared surface 伪造成功结算的旁路。Coding bound holder 对 `response_settled` / `tool_batch_settled` 先终结 bridge context，再走该完成路径；写入失败时降级为完整 route cancellation，避免 loop 已报告 settled 而重启后仍恢复 active alias。abandoned、steered、runtime、transport、supersede 与未知 reason 仍通过 `CancelRouteSurface` fail-closed。回归覆盖两种 settled disposition：predecessor alias 均为 `stale_surface`，但 route 仍 current；core coordinator 回归还验证 finished presentation 不会撤销可复用 grant、也不会接受其他终态冒充 settled。动态 production alias gate、provider replacement certificate、D3 cohort 与 kill-switch 演练均未因此完成或打开。

### 9.61 durable terminal failure 不能被内存终态吞掉（本次实现）

request holder 之前在 `CancelRouteSurface` 失败时仍将自身设为 terminal 并静默忽略错误。虽然这会阻止当前进程继续执行，但生命周期 owner、channel close 与健康诊断会把这轮看成正常结束；若数据库不可写或已关闭，重启恢复又可能看到仍 active 的 durable request surface，造成“内存已退休、持久层未退休”的不可解释分叉。

现在 `retireLocked`、normal lifecycle close 与 settled finish 都保留 `terminalDurabilityErr`。holder 仍立即取消 bridge context、拒绝任何 late call，并向 channel 传递失败 cause；relay 在清除 active reservation 后保留同一 diagnostic error，不能以 successor、alias、grant 或 retry authority 使用它。response settle 的 `Finish` 失败仍尝试完整 route cancellation；如果 fallback 也失败，组合错误保持可见。回归通过关闭 coordinator 强制 finish/cancel 均失败，验证 holder/relay 仍 fail-closed，且 close/disposition 不会伪装为干净终态。该诊断传播不构成 provider receipt、replacement certificate 或 D3 evidence，生产 dynamic alias gate 仍关闭。

现在 callback 单独记录 `semanticDurabilityBlocked`：pre-tool checkpoint、complete batch checkpoint、`ask_user` 或 `record_audio` 的 paired-state 写入任一失败都会永久关闭本 callback 的 dependant release 与 host-owned artifact projection。这个标记不伪造 recovery marker、也不试图恢复或重放；它只保证失败后的进程内状态不能再成为下一份 grant、PDF、图片或交付的来源。回归覆盖 failed batch commit 的标记，以及已有 PDF grant 在 durability failure 后仍不能被终态 fallback 消费。

### 9.59 `DoVerified` 缺失 receipt 也必须投影为一次失败证据（本次实现）

correlation-bound channel 可能违反接口约定而返回 `response/error` 却留下零值 `ToolSurfaceReceipt`。此前 RunLoop 会先把该零值交给 observer，再由后续 verifier 拒绝；虽然 binder 不会执行，但审计流无法区分“channel 已尝试 dispatch 却没有 receipt”与“根本没有 receipt observer”。

现在 RunLoop 检测零值 dispatch receipt，并在同一 request-owner scope 生成一次 `handoff=ambiguous`、无 payload/audit digest 的 `surface_integrity_failure` receipt；缺少 receipt 时不能诚实断言 bytes 未写出，必须保守为可能已经 handoff。随后仍以原零值执行 verifier 并终结同一 reservation。非零 receipt 保留为诊断输入，但永远必须通过完整 payload/audit verifier 才可 bind。回归覆盖带 response 却省略 receipt 的 channel：发送一次、binder 零调用、仅一个 integrity disposition 与一个显式无 digest failure receipt。

### 9.62 relay 终态持久化必须与 successor admission 线性化（本次实现）

9.61 虽已让 holder/relay 保留 durable terminal failure，但 relay 曾在调用 `CloseForLifecycle` 前先清空 `active`。由于 durable SQLite 写入在 relay 锁外执行，另一个 goroutine 可以在这段窗口中 reserve successor；若 predecessor 的 finish/cancel 最终失败，重启恢复仍会看见 predecessor active，而内存中已经发布第二个 request surface。这不是普通 stale callback 问题，而是两个真实 outbound request 的 authority 同时存在，违背一请求一 immutable manifest/terminal 的根不变量。

现在 relay 使用 `terminating` 作为终态写入期间的 admission fence：开始关闭时保留 exact active holder 并标记 transition；所有 reserve、render、bind 与 dispatch 在 transition 中 fail-closed。durable close 返回后才清除同一 exact holder并解除 transition；若写入失败，`terminalErr` 被永久 latch，后续 reserve 以 `surface_integrity_failure` 拒绝，且不会调用 successor factory。成功终态仍允许创建一个新 holder，迟到 predecessor disposition 仍只按完整 execution tuple 忽略，不能扰动 successor。新增回归分别验证 transition 中零 factory 调用，以及 coordinator 已关闭导致 durable terminal failure 后零 successor admission。该闭合不产生 retry、route、alias、grant 或 provider certificate authority；production dynamic alias gate 继续关闭。

### 9.63 已发布 surface 的 presentation clone 失败不得伪装成干净 stale（本次实现）

`publishCodingDurableDynamicSurfaceForEpoch` 成功后，holder 还需要把 definitions 深拷贝给 RunLoop。此前这一步 clone 失败时，holder 以 best-effort `Cancel` 后直接标为 terminal；如果 coordinator 写入同时失败，RunLoop 会收到 pre-dispatch integrity failure，但 relay 看到的只是一个普通 terminal holder，随后可以 reserve successor。重启恢复则可能仍看到旧 prepared surface，重新出现 9.62 所防止的双 presentation 分叉。

现在 clone 失败将 route cancellation 作为终态的一部分；取消失败会记录为 `terminalDurabilityErr`。发布函数本身返回错误同样被视为 persistence-uncertain：它可能已经经过 `PublishSurface` 的首次 durable commit，不能仅凭 error 文本推断“无状态写入”。holder 因此 latch 该错误；RunLoop 的唯一 integrity disposition 传给 relay 后，relay 关闭 successor admission，零 successor factory 调用。成功 cancel 仍是标准的 terminal stale surface，不额外把 route cancellation error 当作 authority。回归覆盖关闭 coordinator 后的 publication 失败，以及 relay 接收其 integrity disposition 后拒绝 successor。该规则不把 local error 变成恢复、重试、grant、alias 或 provider certificate authority；production dynamic alias gate 保持关闭。

### 9.64 lifecycle cancellation 必须进入已开始的 transport dispatch（本次实现）

此前 holder 的 `CloseForLifecycle` 会先取消 fixed G4 bridge context，再写 durable terminal；但 `DoVerified` 把 RunLoop 传入的 request context 原样交给 channel。若终态恰好发生在 holder 完成 policy/audit handoff、底层 transport 正在等待响应时，route 虽已取消，那个 transport 仍可能继续执行到 caller 自己的 timeout；这使“只保留一个 terminal presentation”的 durable 结论与活跃网络请求脱节，也为随后成功 response 的 late bind 留下不必要的竞争窗口。

现在 holder 将 caller request context 与自身 execution lifecycle context 合并：任一方取消都会取消传给 verified channel 的 dispatch context，且不新增 deadline、retry 或 provider authority。`CloseForLifecycle` / `Close(non-nil)` 仍先取消 execution context，因而已进入 `DoVerified` 的 channel 立即观察 cancellation，再由既有 disposition/terminal 路径完成同一 durable closure。确定性回归使用一个阻塞 channel：dispatch 真实进入后触发 runtime close，验证它返回 `context.Canceled` 而不会继续等待；定向 lifecycle suite 与 race 检查仍通过。该变化只收紧 request ownership，不把 context cancellation 解释为 transport 未发送、provider acknowledgement、retry 权限或 dynamic alias production qualification；production gate 继续关闭。

### 9.65 channel cleanup 不能抢先于 semantic terminal（本次实现）

correlation-bound RunLoop 在 `DoVerified` 返回后会按 transport lifetime 调用 `requestChannel.Close(err)`；当 `err=nil` 时，这只表示 response 已交回 loop，绝不表示该 response 已 bind、batch 已 durable commit，或 route 可被取消。holder 过去把 `Close(nil)` 直接转发到 live channel：它会与 response binder、late lifecycle close 和 relay terminal write 并发，让 socket cleanup 成为一个未线性化的 terminal side channel。

现在 holder 的 `Close(nil)` 是明确 no-op；只有 `OnToolSurfaceDisposition` 经 relay 选择 settled finish 或 fail-closed cancellation 后，holder 才关闭底层 channel。非 nil `Close` 仍统一走 `closeAndRetire`，先取消 request lifecycle context，再进行 durable cancellation；新增阻塞 dispatch 回归验证这一 generic close 同样取消 in-flight transport。另一个回归验证成功 transport cleanup 不会抢先关闭 channel，随后 runtime terminal 才执行唯一 close。为了避免 renderer/pre-dispatch 已经 latch 的 persistence failure 被随后幂等 `CloseForLifecycle` 吞掉，relay terminal helper 现在回读 holder 的 latched error 并继续拒绝 successor admission。这个收束不把 channel close 变成成功 receipt、retry、grant 或 alias authority；dynamic production gate 仍关闭。

### 9.66 dispatch preparation 完成不等于获得 transport handoff 权限（本次实现）

`SetToolSurfaceDispatchPreparation` 必须在 holder 锁外调用下层 channel，才能避免把可能阻塞的 transport 实现放进 lifecycle mutex。此前在该外部 setter 返回后，`DoVerified` 直接把 `dispatchAttempted` 标记为真并调用下层 `DoVerified`；若 runtime/steer terminal 恰好在 setter 执行期间完成，route 已经 durable retire、request context 已取消，但一个没有主动检查 context 的 future channel 仍可能被调用。即使它最终失败，这也是在已关闭 presentation 上开始 outbound transport 的所有权越界。

现在 holder 把“冻结完整 frame”和“开始 handoff”明确分为两个阶段：setter 返回后在最后一个本地线性化点重新检查 `terminal`、surface 与 lifecycle context；任一项已失效就清除 `dispatchPreparing` 并返回 `stale_surface`，不会调用 channel。随后还检查 merged request context，避免已观察到的取消跨越进入 transport。已经跨过该点的并发 terminal 仍首先取消同一 context，且 qualified channel 仍须在产生外部副作用前遵守该 context；本修改不把 context cancellation 解释为未发送、provider receipt、retry 或 replacement authority。确定性回归让下层 preparation 成功后同步触发 runtime terminal，断言 transport 调用数为零。production dynamic alias gate、provider replacement certificate、D3 cohort 与 kill-switch 演练仍未完成或打开。

### 9.67 多个终态观察者不得重复释放同一 transport（本次实现）

bound holder 的 dispatch error 会先在 `DoVerified` 内部调用 `Close(err)`；随后 shared RunLoop 仍会执行其兼容的 `requestChannel.Close(err)` 清理，最终还会发出一次 semantic disposition 交给 relay。此前每个非 nil `Close` 都会继续调用底层 `channel.Close`。实际 Responses WS 恰好借助底层 `sync.Once` 避免重复断开，但这把 request-owner 的一次性资源所有权错误地下推给某一个 transport 实现；未来 channel 或测试/诊断包装器可能重复写 close frame、重复统计或产生不可预测副作用。

现在 holder 保存 `channelClosed` resource fence，并只在选定 semantic terminal 的第一个路径中取得底层 channel；后续 dispatch cleanup 和 lifecycle disposition 仍可验证/传播 durable terminal error，却不会再次调用 `Close`。`Close(nil)` 仍不具备 terminal authority。回归按 dispatch-error、generic cleanup、lifecycle terminal 三种顺序叠加，断言底层 Close 恰好一次。该资源去重不放宽任何 route、alias、grant、receipt 或 successor 权限；dynamic production alias gate 继续关闭。

### 9.68 relay terminal fence 必须覆盖 publish/bind 的整个 durable 操作（本次实现）

relay 先读取 `active`/`terminating` 再释放 mutex、随后调用 holder 的 publish 或 response bind，会留下一个线性化歧义：terminal goroutine 可以在该间隙先将 relay 标记为 `terminating`，但已经拿到旧 holder 指针的 goroutine 仍可能继续写入新的 durable surface 或 response binding。即使后续 cancellation 最终会处理它，`terminating` 也不再是“terminal 开始后零 publication/zero bind”的真实 admission fence。

现在 relay 在调用 holder 的 `RenderPublishedBoundToolSurface` 和 `BindToolSurfaceResponse` 时持有 relay mutex。故两个操作只能线性化为二者之一：publish/bind 整体完成后 terminal 才能设置 `terminating`，或者 terminal 先设置 fence，后续 publish/bind 被显式拒绝。durable operation 仍在 holder 内部完成，且 terminal 自己不会持 relay mutex 等待该 operation，因此不形成 relay→holder→relay 的锁环。执行调用不持有 relay mutex（它可包含较长的 catalog/provider 工作）；其既有 lifecycle-context cancellation fence 保持负责 in-flight interrupt。该修正不开放 production aliases、provider replacement certificate、D3 cohort 或 kill-switch。

### 9.69 bind 与 terminal 的竞争必须有可重复的线性化回归（本次实现）

9.68 的锁范围若只靠单线程 success/failure 测试，仍无法证明 future refactor 没有退回“先读取 holder 指针、释放 relay lock、再 bind”的模式。该模式最危险的地方不是最终能否 cancellation，而是 terminal 开始之后是否仍写入 response binding。现在新增确定性并发回归：测试主动占有 relay mutex，同时排队一个 response bind 和一个 runtime terminal；释放 mutex 后二者只能以完整操作的顺序执行，terminal 收敛后断言 exact holder 已 terminal、relay 不再保留 active、execution context 已取消。该测试与 race 多次运行，覆盖 terminal fence 的 actual bind 入口而非只直接改写 `terminating` 字段。

它验证的是 local request-surface ownership，不把并发排序误作 provider acknowledgement、replacement certificate、D3 conformance 或 production alias qualification；这些 gate 仍保持关闭。

### 9.70 execution 快路径不能在 terminal 开始后借旧 holder 指针进入 bridge（本次实现）

relay 的 execute 入口原先同样是“读取 `active`/`terminating` 后解锁，再调用 holder”。因此 terminal 可以在读取之后先设置 `terminating`，而执行 goroutine 仍拿着 predecessor holder 继续 alias resolution、grant admission 或 provider I/O。holder 内的 context 检查会挡住一部分时序，但 relay 自己的 terminal fence并未真正覆盖 execution admission，和 9.68 的 publish/bind 闭合不一致。

现在 relay 持有 lifecycle mutex 调用 holder 的轻量 `beginBoundToolCall`，在同一个线性化点冻结 surface、correlation tuple 和 lifecycle context；只要 terminal 先进入，execute 直接拒绝。拿到 ticket 后立刻释放 relay lock，真正的 bridge 工作不在 relay lock 下运行；terminal 会先取消同一 `executionCtx`，所以已签发但尚未开始 bridge 的 ticket 也只返回 `stale_surface`，不会 alias resolve、Admit 或触发 provider I/O。确定性回归先签发 ticket、再终止 request、最后执行该 ticket，断言 bridge 零进入。该 ticket 不是 grant、alias、retry 或新 authority，动态 production alias、provider certificate、D3 cohort 与 kill-switch 仍未开启。

### 9.71 audit evidence 读取也必须属于 relay 的 terminal fence（本次实现）

`ToolSurfaceAuditEvidence` 本身不执行 alias 或 provider，但它会马上参与下层 channel 的完整 dispatch frame（audit evidence + invocation policy）。此前 relay 读取 `active`/`terminating` 后释放 mutex，再向 holder 读取证据；终态可以在这段间隙开始，造成“terminal 已开始、旧 holder 仍被用于构造下一步 immutable handoff 输入”的不清晰边界。虽然后续 `SetToolSurfaceDispatchPreparation` 与 `DoVerified` 都会再次 fail-closed，但读取入口不应依赖后续调用碰巧补救。

现在 evidence 读取和 publish/bind/execution admission 一样在 relay mutex 下完成。它只读取 holder 内存中的 immutable plan facts，不进入 coordinator 或 transport I/O，因此不会把 durable terminal 写入塞进 relay critical section。新增并发回归让 evidence 与 runtime terminal 同时排队在 relay mutex，验证二者只能完整地线性化在 fence 的一侧，terminal 后 holder 仍不可用。该收紧不把 evidence 变成 receipt、grant、alias 或 provider authority；production dynamic alias gate 继续关闭。

### 9.72 dispatch preparation 必须是单一 in-flight handoff（本次实现）

holder 的 `SetToolSurfaceDispatchPreparation` 会在 mutex 外调用下层 channel，避免可能阻塞的 transport setup 占有 lifecycle mutex；但此前两个并发 caller 可以同时观察到 `invocationPolicySet=false`，并各自调用一次下层 setter。即使两个输入相同，这仍把“每个 request 只有一个 immutable handoff”错误地下推给具体 transport 的幂等实现；而不同 channel 对第二次 setter 的副作用并无统一保证。

现在 holder 引入独立的 `preparationInFlight` fence。第一个 caller 在释放 holder mutex 前取得该 fence；并发 caller 立即以 `surface_integrity_failure` 拒绝。外部 setter 返回时无论成功、失败、缺失 capability 或 terminal 竞争，fence 都会被清理；成功后只保留一次已冻结的 policy，重复的相同调用不再触及下层 channel。确定性回归阻塞首个 lower setter，并断言第二个 setup 没有获准，首个完成后 holder 仅有一次 settled preparation。这并不打开 production dynamic aliases，也不构成 provider replacement certificate、D3 fixed cohort 或 kill-switch 演练。

### 9.73 catalog refresh 到 provider bridge 的最后窗口必须检查 request cancellation（本次实现）

9.70 的 execution ticket 与 `codingDurableDynamicSurface.ExecuteBoundSelection` 已把 terminal context 带到 dynamic catalog；但 catalog 在取得 fresh binding 后，普通 read-only / local binding 以及 external-effect coordinator 的 dispatch closure 仍可能直接进入 MCP、Skill 或 host bridge。若 lifecycle cancellation 刚好发生在一次 catalog-level entry check 之后、provider callback 之前，这个狭窄窗口会让已经 terminal 的 response surface 发起新的 provider I/O。对外部 effect 而言，错误地标成 unknown 还会把一个可证明“尚未调用 provider”的本地取消写成不必要的恢复不确定性。

现在 catalog 在 entry 和每个实际 dispatch closure 都检查 context；已取消时返回固定的 `dynamic_execution_cancelled`，不读取 binding bridge。external-effect 的统一与独立 ledger coordinator 都将该错误作为确定性 pre-I/O failed，而不是 unknown；`executeDynamicExternalEffect` 也保持同一 reason，不让 test/different coordinator 的 callback error 抹掉该事实。Coding durable bridge 再把这个内部 reason 收束为 request-surface 语义的 `stale_surface`，并完成既有 host-call journal，使相同 tool-call ID 不能在 terminal 后重入。这不把 context cancellation 当作 provider receipt、transport 未发送证明、retry 或新 alias authority；production dynamic alias gate、replacement certificate、D3 fixed cohort 与 kill-switch 演练仍关闭。

### 9.74 dispatch preparation 报错后不得重试同一 request holder（本次实现）

`SetToolSurfaceDispatchPreparation` 的下层 setter 是 immutable audit evidence 与 invocation policy 一起跨越 transport boundary 的唯一入口。此前它若返回 error，holder 只会清除 in-flight 标记并把错误交回 caller；同一个 request 仍可再次 setter 或 `DoVerified`。但下层错误不是“未改变任何内部状态”的可靠证明：transport 可能已冻结其中一半 frame、占用 one-shot reservation，或写入无法由上层观察的 local state。继续使用该 holder 会破坏“一次 outbound request 只有一个完整 manifest/handoff”的基础不变量。

现在任何缺失 atomic setter 或 lower setter failure 都立即走 holder-owned `closeAndRetire`：先取消 lifecycle context，再完成 durable route cancellation 并仅一次关闭 channel。若 durable retire 失败，错误被组合并继续成为 relay 的 terminal durability fence；若成功，旧 holder 也仍为 terminal，不能重试 setter 或进入 `DoVerified`。新增回归让 lower setup 返回“outcome ambiguous”，验证零 transport dispatch、恰好一次 close、terminal context 已取消且 retry 被拒绝。该闭合不将错误解释为 provider receipt、bytes 未发送证明、retry、grant 或 alias authority；production dynamic alias gate 保持关闭。

### 9.75 external-effect coordinator 的返回错误不能抹掉可证明的本地取消（本次实现）

`executeDynamicExternalEffect` 先前把 coordinator 的任何返回 error 一律映射成 `dynamic_effect_execution_unknown`。这混淆了两个完全不同的事实：若 coordinator 在没有调用 dispatch callback 前观察到 request context 已取消，则没有 provider bridge 被触及，结果是确定性的本地终止；若 callback 已启动或 durable coordinator 在 I/O / 持久化后失败，才必须保守地维持 unknown。

现在 guarded callback 记录“已调用 / 已完成”，并在 coordinator 返回后立即撤销 callback 的调用权。若 callback 已完整返回一个已证明的 pre-I/O rejection（包括 `dynamic_execution_cancelled`），即使 coordinator 直接把该 error 返回，catalog 仍保留该确定性结果；若 callback 从未调用且 context 已取消，同样返回 `dynamic_execution_cancelled`。其余 coordinator error、以及不合约的异步 callback 已开始但 coordinator 已返回的情况仍 fail closed 为 unknown。接口契约也明确 callback 必须在 coordinator 返回前同步完成，防止被保存后穿越 request terminal fence。新增回归覆盖“coordinator 在 dispatch 前取消并返回 error”；既有 callback-window 回归改为不吞掉 dispatch cancellation，验证该路径不再被误判 unknown。该修复不把 cancellation 解释为 receipt、未发送证明、可重试权或新的 alias authority；production dynamic alias gate、replacement certificate、D3 fixed cohort 与 kill-switch 演练继续关闭。

### 9.76 render 阶段 terminal 必须先撤销 execution context（本次实现）

`RenderPublishedBoundToolSurface` 的 publication error 与 presentation clone error 会立即把 holder 标记为 terminal；但此前它们依赖 RunLoop 稍后送达的 integrity disposition 才取消 `executionCtx`。这与其他 terminal 路径的顺序不一致：已经持有同一 request context 的 bridge ticket 在 render failure 已经对外可见、而 disposition 尚未线性化的窗口内，仍可能错误地认为 request 可继续执行。

现在两条 render-failure 分支都在设置 holder terminal、或开始 published surface 的 durable `Cancel` 之前撤销 `executionCtx`。RunLoop 的唯一 disposition 仍负责 relay cleanup、channel release 和 terminal durability error 的传播；它不是取消已经 terminal request 的前提。回归在关闭 coordinator 制造 publication failure 后，直接在 disposition 之前断言 execution context 已取消。此处 cancellation 仅收紧本地 request ownership，不证明 provider 未发送、receipt、replacement conformance、retry、alias 或 D3 qualification；production dynamic alias gate 继续关闭。

### 9.77 bind failure 也必须在 relay disposition 前撤销 execution context（本次实现）

response bind 的 tuple mismatch、缺失 `ResponseID` 或 durable `BindResponse` failure 都会让 holder 立即退休 route surface。此前这三条分支只调用 `retireLocked`，而把 `executionCtx` 的取消推迟到 RunLoop 随后发送 `response_abandoned` / integrity disposition。因而一个已在 bind 前取得的 execution ticket 可能在 holder 已 terminal、但其 context 尚未取消的窗口中继续执行；这与 9.70 的 ticket fence 和“terminal 先撤销 route authority”的顺序冲突。

现在这些 bind-failure 分支通过同一 holder-local helper，先撤销 execution context、再写 durable route cancellation。disposition 仍是唯一的 relay ownership cleanup 及 channel release 信号，不能再作为终止 ticket 的先决条件。回归覆盖缺失 response ID 的 bind failure，并在 disposition 前断言 context 已取消。该收紧不把 bind failure 解释为 provider receipt、未发送证明、retry、alias 或 D3 qualification；production dynamic alias gate 继续关闭。

### 9.78 cancellation-style retirement 的 context fence 必须由公共收敛点保证（本次实现）

9.76/9.77 为 render 与 bind 的具体失败分支补上了提前取消，但 `retireLocked` 仍依赖每个调用者在进入前记得调用 `cancelExecution`。这会把“durable route cancellation 前必须先撤销已签发 ticket”的不变量变成约定：未来新增一个 holder-local stale / integrity branch，极易只调用 `retireLocked` 并重新制造相同窗口。

现在 `retireLocked` 自身在设置 terminal 和进行 durable `Cancel` 前幂等地撤销 `executionCtx`。`closeAndRetire` 仍在取得 holder mutex 前先取消，以便已进入 transport 的合并 request context 尽早被中断；两层调用均为幂等，公共收敛点则保证所有当前及未来 cancellation-style retirement 不会遗漏 lifecycle fence。settled request 仍走独立的 `Finish` 路径并维持同样的提前取消。该防线只维持 request ownership，不能证明 provider receipt、未发送、replacement conformance、retry、alias 或 D3 qualification；production dynamic alias gate 继续关闭。

### 9.79 S0.5 compatibility path 也必须为每个 outbound request 重建工具面（本次实现）

`RunLoop` 已为 request-local renderer 在每轮、fallback 和 outer retry 调用 `BuildToolsForModelRequest`；但没有实现该新接口的静态 compatibility callback 原先收到的是 loop 启动时缓存的 `tools` slice。虽然 receipt 能验证这份旧 slice 完整到达 wire，却无法证明它仍是当前 host policy / inventory 下的完整 replacement；多轮期间 callback 的静态 allowlist、feature state 或 configuration 改变时，旧工具会被无意继承。

现在 `buildToolsForModelRequest` 对没有 request-local renderer 的 S0.5 callback 同样在每一个 request boundary 调用 `BuildTools` 并重新经 authorizer 过滤，彻底不把前一请求的内存 slice 当作 successor authority。新增两轮回归让 legacy callback 每次 `BuildTools` 返回不同 surface：启动期 bootstrap definition 不得发送，第一、第二个真实 wire request 必须分别携带各自当轮完整 replacement。该修复不把 static surface 变成 dynamic alias、grant、receipt identity 或 provider replacement certificate；D3 和 production dynamic alias gate 继续关闭。

### 9.80 不得在首个 request 之前构造无 manifest/receipt owner 的静态 surface（本次实现）

9.79 虽让后续 request 重新调用 `BuildTools`，但 `RunLoop` 初始化仍会先构造一次静态 tools slice，再在第一轮请求边界覆盖它。这个启动期 slice 不会发送，却会触发 host inventory/feature-state 读取；它既不属于真实 outbound request，也没有 manifest、receipt 或 terminal disposition。更严重的是，任何带副作用或不稳定的 callback 实现都可能因这次无主读取造成首轮与后续轮不一致，令“每个真实 request 恰有一个 surface”退化为“另有一份幽灵 surface”。

现在 loop 初始化不再调用 `BuildTools`；静态与 request-local renderer 都只在实际 request、fallback 或 outer retry 的 boundary 构造 surface。两轮 legacy S0.5 回归相应断言 `BuildTools` 调用数恰好等于两个真实 wire request，且每轮分别发送当轮 replacement。局部 `tools` 变量只在某一已创建 request 的 response-recovery 文本和同轮 tool-batch 收敛中保存快照，不能成为下一 outbound request 的 authority。该修复不赋予静态 surface 动态 alias、grant、receipt identity 或 provider replacement certificate，D3 与 production dynamic alias gate 保持关闭。

### 9.81 静态 request-surface helper 不得保留 predecessor fallback 参数（本次实现）

9.79 将无 renderer 的静态 callback 改为在 boundary 调用 `BuildTools`，但 helper 仍接收一个 predecessor `fallback []definitions` 参数，再在实现中忽略它。该签名保留了错误的能力形状：未来调用者可能把它重新用作 `BuildTools` 失败时的兜底，从而让已 terminal 的 predecessor surface 回到 successor wire。

现在 helper 移除了 fallback 参数，所有正常轮、stream fallback 和 outer retry 都只能请求当前 callback 的新 surface。局部上一轮 definitions 仍可用于已经收到的 response 的 truncation/argument recovery 文本，却在类型层面不能流入下一 outbound request 的构造入口。该收紧不引入 fallback tool、alias、grant、receipt identity 或 provider replacement certificate；D3 与 production dynamic alias gate 继续关闭。

### 9.82 shared IM compatibility callback 也必须在真实 request boundary 重建并重绑执行面（本次实现）

9.79 修正了 `RunLoop` 的通用静态 fallback，却遗漏了 `sharedAgentLoopCallbacks`：它把 `prepareAgentLoopStartState` 预先计算的 `startState.Tools` 原样作为 `BuildTools` 返回。于是通用 loop 虽然在每个 request boundary 调用了 callback，shared IM 的第二轮、stream fallback 或 outer retry 仍可能重新发送进入 loop 前的同一 slice；更严重的是，`legacySurface` 也仍指向这份旧定义，最终 wire receipt 与执行 admission 都无法证明对应当前 request 的 replacement。

现在 shared callback 实现 `BuildToolsForModelRequest`。在 `RunLoop` 创建该 request 的 epoch 后、最终 wire receipt 之前：非 managed turn 用同一 inbound `agentLoopPhase` 和当前 host registry 重新执行 `prepareAgentLoopTools`，将完整结果替换为 callback 的 `tools` 与 `legacySurface`；managed semantic turn 只从现有 durable plan/grant 的可见闭包重投影，绝不在 renderer 中发行、刷新或复活 grant。`legacyToolSurface.replaceDefinitions` 保留同一逻辑 request stream 的 epoch holder，但替换 names/schema/client binding；因此刚生成并发送的 request 仍可使用该 epoch 执行，而下一个 request 的新 epoch 会令前一 response 失效。回归断言 replacement 不合并旧工具、保留 current epoch、且 successor epoch 会拒绝 predecessor。该修正不把 IM legacy route 变成动态 alias、grant、provider receipt 或 replacement certificate；D3 与 production dynamic alias gate 继续关闭。

### 9.83 refresh 分支也必须保持 inbound phase，并以真实 request renderer 验证 replacement（本次实现）

9.82 之后仍有一个细小但会破坏“同一 turn 固定 policy posture”的分叉：`RefreshAfterToolExecution` 的 legacy 分支使用零值 `agentLoopPhase{}` 重建 callback-local snapshot，而 request-bound renderer 使用保存的 inbound `phase`。当该 turn 带有强制 skill preference 等 phase 约束时，refresh 后、下一次真正 render 前，callback 内部的 `tools` / `legacySurface` 会短暂代表不同 policy；尽管 boundary renderer 会最终覆盖它，这种不一致会使工具批次后的本地 admission 与请求准备难以审计。

现在 refresh 也统一传入 `c.phase`，所有 legacy snapshot rebuild 都使用同一不可变 inbound posture。另新增 request-renderer 回归：两次真实 request 之间替换 generator/registry inventory，断言第二个 rendered surface 仅含 successor definition、旧 epoch 的 tool call 在执行入口被拒绝，而新 epoch 仅允许 successor tool。该回归覆盖的是 shared callback 的真实 renderer，而不只是独立 `legacyToolSurface` 值对象；并已通过定向 GUI suite 与 race 多次执行。此处不开放 dynamic alias、`manage_skill` / `call_mcp_tool` gateway fallback、provider replacement certificate、D3 cohort 或 kill-switch 演练；production gate 继续关闭。

### 9.84 Coding S0.5 renderer 不得在当前 request render 时撤销已签发 epoch（本次实现）

继续审计发现 local / remote Coding compatibility callback 仍存在相同的顺序错误：`RunLoop` 先调用 `BeginToolSurfaceEpoch`，随后调用 `BuildToolsForModelRequest`；但 `setStaticCompatibilitySurface` 在后者内部清空 `staticCompatibilityEpoch`。因此工具定义和 receipt 对应当前 request，但携带该 request epoch 的正常模型 response 到达执行入口时，会被静态执行栅栏误判为 `static_surface_unavailable`。这不是安全收紧，而是将 request-bound replacement 的合法 response 在 render 后立即撤销，表现为“工具面存在但几轮后无法执行”。

现在 local 与 remote `setStaticCompatibilitySurface` 只替换 definitions / revision，保留已经为当前 request 签发的 epoch。后继真实 request 仍先生成新的 epoch，再渲染完整 replacement，因此 predecessor epoch 在 successor render 前已失效，且不会留下无 epoch 的 admission 窗口。新增 local 与 remote 回归严格按 RunLoop 顺序执行“epoch → renderer → execute”：当前 epoch 必须可执行已渲染工具；successor epoch 则拒绝 predecessor response。定向 static compatibility suite、race 多次执行以及 core RunLoop request-rebuild suite 均通过。此修复不升级 S0.5 为 provider correlation，不开放任何 dynamic alias、legacy dynamic gateway、`Wired` / `Enabled`、replacement certificate、D3 cohort 或 kill-switch。

### 9.85 所有 outbound successor 都必须先推进 epoch，再渲染 replacement（本次实现）

初始 request 一直遵守 `BeginToolSurfaceEpoch → BuildToolsForModelRequest`，但 stream fallback 与 outer retry 曾存在另一种顺序：先 render successor definitions，再创建 successor epoch。只要 renderer 会原子替换 callback 的静态 compatibility surface，这一瞬间就会出现不一致：执行入口已经看见 successor definitions，却仍允许 delayed predecessor response 使用旧 epoch 进入。它既不是正确的 replacement，也不是可审计的 request ownership 边界；在多轮、重试或 fallback 后会表现为工具面错配、遗漏或偶发拒绝。

现在所有真实 outbound request——初始发送、stream 失败后的非流式 fallback、outer retry，以及 live steering/replan 生成的 replacement request——统一遵守以下不可拆分顺序：

`advance epoch → render complete replacement → freeze/verify receipt → start transport handoff`。

因此 successor render 之前 predecessor admission 已失效；renderer 安装的新 definitions 与其对应的执行 epoch 同时成为当前 request 的唯一候选面。不得将顺序改回 `render → advance epoch`，也不得以 predecessor definitions、policy、receipt 或 execution context 作为 successor 的兜底输入。新增三条 core regression：outer retry、stream fallback 与 live replan 均断言 renderer 依次观察到 `epoch-0-1`、`epoch-0-2`，且两个 wire request 分别只携带 predecessor / successor 的完整 replacement；replan 还断言 predecessor response 被丢弃、replacement request 才能提交最终文本。此修正只闭合本地 request-surface ownership；不证明 provider replacement conformance、transport delivery、动态 alias 资格或 D3 生产就绪性，production dynamic gate 仍保持关闭。

### 9.86 首次 S0.5 request 也必须在 render 前获得 execution epoch（本次实现）

9.85 收紧了 successor 的顺序后，local / remote Coding static belt 仍残留一个对首次请求的错误前提：`beginStaticCompatibilitySurfaceEpoch` 要求已经存在 `staticCompatibilitySurface`。但 `RunLoop` 的真实顺序是先 `BeginToolSurfaceEpoch`、后 `BuildToolsForModelRequest`；因此首次 request 尚未 render 时该字段必为 nil，epoch 会返回空值。空 epoch 又被 compatibility execution fence 视为 direct-host compatibility 调用，导致首轮模型 response 缺少与同一 request surface 的本地关联，和后续轮的 admission 语义不一致。

现在 local 与 remote callback 仅在 static belt 已 quarantine 时拒绝签发 epoch；首次 request 无需 predecessor definitions 也会获得新的 opaque in-process epoch。renderer 随后只替换本 request 的 definitions、不会清空该 epoch；successor 仍先推进其 epoch 再 render，从而拒绝 predecessor response。新增 local/remote 对称回归覆盖首次 `epoch → render → execute`、successor 拒绝，以及“首轮 render 前已签发、随后 ambiguous delivery”必须清空 epoch、禁止再 render 或执行；另以真实 `RunLoop` 的 tool round 验证 local `read_file` 和 remote `ssh_read_file` 在首轮带有 epoch 的 response 均能穿过静态 execution fence，并在下一模型请求重新 render request-bound surface。该 epoch 仍不是 provider response/connection ID、grant 或 durable replay authority；dynamic alias production gate 与 D3 条件均未改变。

### 9.87 禁止模型 dispatch 回落到 epoch-less compatibility API（本次实现）

审计发现 local / remote Coding 的 `ExecuteToolStructured` 与 `ExecuteTool` 仍是无 epoch 的 direct-host 兼容入口。它们是 post-loop verification、host maintenance 和测试所需的受限路径，不能简单删除；不过旧注释把它表述成“常规模型分派入口”，而 context-aware model boundary 在完成 epoch admission 后还曾间接重入该公开 API。这会混淆 authority provenance，并增加未来重构把模型 response 错接到无 epoch 入口的风险。

现在 `RunLoop` 在 callback 同时实现两种 executor 时始终优先调用 `ExecuteToolCallWithContext`；local / remote context boundary 在验证 rendered name 与 current epoch 后直接调用私有 canonical executor，不再回跳 `ExecuteToolStructured`。公开的无 epoch 方法明确仅用于 host-owned maintenance / verification / test，仍保留其 canonical-name 与 posture 栅栏，不能被当作 provider response dispatch。新增 core regression 让 callback 同时提供 context 与 structured executor：一轮真实模型 tool call 必须只到达 context executor，并携带 request epoch；structured fallback 调用数必须为零。此次调整不扩大 S0.5 authority，也不改变 dynamic alias、`manage_skill` / `call_mcp_tool` gateway、`Wired` / `Enabled`、provider correlation 或 D3 production gate。

### 9.88 RunLoop 必须先验证模型调用属于该请求实际 rendered surface（本次实现）

此前 Coding static belt 与 shared IM 各自检查 model tool name 是否位于当前 surface，但该性质没有由通用 `RunLoop` 保证。普通 callback 或未来宿主只要拥有一个宽松的 name dispatcher，模型就能返回未出现在本次 wire payload 中的 registry name，并在 authorizer 许可时执行。这使“definitions 已全量 replacement 且 receipt 已校验”仍可被 response-side name fallback 绕过，是工具面不完整问题在执行边界的根本残口。

现在 RunLoop 在参数校验、authorizer、context executor 和所有旧 structured/plain fallback **之前**，以精确字符串比较检查每一个 provider tool call 是否存在于本次冻结并发送的 rendered definitions。不存在时只写配对的 tool-error history，不调用任何 host dispatcher；不做大小写、alias 或 canonical-name 归一化。新增真实两轮 HTTP 回归：首轮只发送 `read_file`、模型却调用 `bash`，循环必须返回该调用的错误结果并继续到第二轮最终文本，且 callback dispatcher 调用数为零。该检查不把 S0.5 epoch 冒充 provider correlation，也不将 definitions 变成动态 grant；它只把“模型响应只能调用本请求实际暴露的函数”收敛为所有宿主共享的最小执行不变量。动态 alias、`manage_skill` / `call_mcp_tool` gateway、`Wired` / `Enabled`、provider certificate 与 D3 production gate 均不变。

### 9.89 light→full 恢复不得回写已发送 request 的工具面（本次实现）

9.88 审计还发现一个通用旁路：light profile 的 tool-deny recovery 会在收到本 request response 后立即调用 `BuildTools`，把较宽 surface 写入当前 `tools` slice，再重新 authorize 同一个 provider tool call。即使该名字并未出现在已发送 payload，这也会把“下一 request 的候选面”错误地追溯授予当前 response，破坏完整 replacement 的 request ownership。

现在 light→full recovery 只更新后续 prompt posture 与路由状态；它不重建或改写当前 `tools`，也不重新解释当前 provider tool call。当前 call 仍只能命中已冻结 surface；下一真实 outbound request 依旧经 `advance epoch → render complete replacement → freeze/verify → handoff` 生成新面。回归将旧断言反转为：升级后当前 surface 仍仅有首轮 `web_search`，不得出现 `bash`；随后由标准 request renderer 决定 successor 是否暴露 `bash`。该收紧不阻止 host-owned `ask_user` 或已渲染的 control-plane tool；它仅禁止以 light fallback 把未发送定义变成当前响应的执行权。动态 alias、gateway、qualification 与 D3 gate 均不改变。

### 9.90 tool-batch 后的 refresh 不得构造无主工具面（本次实现）

`RefreshAfterToolExecution` 位于已收到并正在提交某个 request 的 tool batch 之后、下一次 outbound request 之前。此前它在 refresh 返回 true 时直接调用 `BuildTools` 并把结果写入 loop-local `tools`。这份 definitions 既不会作为当前已发送 request 的 receipt surface，也还未具备 successor 的 epoch、manifest、receipt、handoff 或 terminal disposition；随后下一 request boundary 又会重新 render 一次。因此它是第 9.80 条所禁止的 ghost surface：除导致不稳定 inventory / feature read 被额外触发外，也为未来代码误将候选 definitions 当作当前 response 或 successor 执行权留下旁路。

现在 refresh 只让 host 更新其自身的 policy / grant / route 状态，并更新 conversation 的非执行性 system prompt，使下一 request 能看到新的 policy posture；它不调用 `BuildTools`，也不改写当前已冻结的 definitions。下一次真实 request 仍是唯一 renderer，严格执行 `advance epoch → render complete replacement → freeze/verify receipt → handoff`。回归以一次 tool request 加一次 successor request 断言 `BuildTools` 恰好调用两次：任何 refresh 期间的第三次调用都会失败。该改动不把 system prompt 视为工具授权，也不开放动态 alias、provider replacement certificate、D3 cohort 或 production dynamic gate。

### 9.91 shared IM callback 的 profile upgrade / refresh 也不得预渲染 successor surface（本次实现）

通用 `RunLoop` 不再在 batch refresh 调用 `BuildTools` 后，shared IM callback 仍在两处自行调用 `prepareAgentLoopTools`：`UpgradeLightPromptToFull` 与 legacy `RefreshAfterToolExecution`。这两处同样发生在当前 response 已经拥有其 request surface、而 successor 尚未获得 epoch 的阶段；它们会替换 `c.tools` / `legacySurface`，形成无 manifest、receipt、handoff 的 callback-local ghost surface。更危险的是，`legacySurface` 持有 epoch holder；虽然 replacement 保留 active epoch，这会让旧 response 在新的 definitions 上继续完成本应属于 predecessor 的 dispatch，违背 response 与 frozen surface 一一对应。

现在 shared callback 在 upgrade/refresh 中只更新非执行性的 system prompt、execution posture 和必要的 Computer Use sticky 状态；不再构造 definitions，也不替换 legacy surface。`BuildToolsForModelRequest` 成为唯一 legacy renderer：在 RunLoop 已推进 successor epoch 后重新计算 inbound phase，按更新后的 profile 生成完整 replacement，并原子替换 callback execution snapshot。新增回归先为 `read_file` surface 签发 epoch、触发 refresh，再断言当前 epoch 和 definitions 保持不变；只有 successor `epoch → renderer` 才允许变为仅 `bash`。附件从 light 升级的回归同时验证 Computer Use sticky state 仍被清除，而这项路由副作用不再依赖预渲染工具面。此变更不赋予 prompt 或 execution profile 任何 dispatch authority，也不开放 dynamic alias、gateway、provider certificate、D3 或 production dynamic gate。

### 9.92 /btw compatibility callback 不得缓存 predecessor definitions（本次实现）

GUI 与 TUI 的 `/btw` callback 曾将第一次 `BuildTools` 的 slice 缓存在 callback 实例内。虽然通用 RunLoop 在每个真实 request boundary 都调用了该方法，但 callback 实际返回的仍是前一 request 的同一 map slice；registry 更新、definition mutation、fallback 或 retry 因而可以让 successor 的 receipt 验证一份不再是当下完整 replacement 的 surface。这违背 S0.5 的每个 outbound request 都必须重建静态工具面的原则。

现在两端 `/btw` 每次 `BuildTools` 都重新构建最小只读 definitions，并仍交由 RunLoop request boundary 冻结与 receipt 校验。回归故意污染首个返回 definition，再取得 successor definitions，断言污染不会泄漏，证明没有共享 predecessor slice。该修复不扩大 `/btw` 工具 allowlist，也不将静态 definitions 升格为 dynamic alias、grant、provider correlation 或 replay authority。

### 9.93 IM host inventory cache 必须与 request surface 隔离（本次实现）

`IMMessageHandler.getTools` 为降低 registry / SkillHub 枚举成本保留五秒 inventory cache；但此前 cache hit 将同一 `[]map[string]interface{}` 直接交给路由与 legacy renderer。路由阶段会产生派生 slice，通常不改 map；但任一新增过滤器、schema 标注、测试 observer 或 provider adapter 只要改写嵌套 map，就会污染 cache。后继 request 即使完成了自己的 `freeze/receipt`，冻结的也可能是 predecessor 已修改的 inventory，而不是当前 host inventory 的独立 replacement。

现在 cache 只保存 handler 私有 inventory snapshot，`getTools` 在所有返回路径均深拷贝 JSON-shaped definition tree。这样既保留 cache 的枚举收益，也保证每个 request renderer 在进入 route/filter/legacy plan 前获得独立 definitions；随后 RunLoop 的 freeze 仍负责把最终 wire payload 与 transport mutation 隔离。新增回归同时污染首个返回值的顶层与嵌套 `function` map，断言第二次 cache-hit 及 handler cache 均未受影响。该修复不改变 routing selection、allowlist、dynamic alias、grant、provider receipt 或 D3 / production gate 状态。

### 9.94 长生命周期 schema registry 也不得借新 slice 复用嵌套 map（本次实现）

审计发现 TUI 的 `CoreToolRegistry.BuildDefinitions` 每轮都会新建顶层 definition，但 `ToolDef` 直接把 `ToolEntry.Properties` 和 `Required` 放进新 definition；`coreAgentCallbacks.BuildTools` 的 `functionToolDefinition` 对 `coreToolSpec.Parameters` 有相同问题。表面上每一轮的 `[]definition` 都是新的，嵌套 schema 却仍指向 registry/spec 持有的 map。任何 request-side 过滤、provider projection 或 observer 的就地修改都会写回长期 inventory，令后续请求得到已污染工具面。

现在 `agent.ToolDef` 深拷贝 properties tree 和 required list，并公开最小的 `CloneToolDefinitionMap` 供 core-agent 的 function renderer 复制完整 parameters schema。克隆保留现有非 JSON 值，使既有最终 wire-freeze 的 fail-closed 行为不被掩盖；JSON-shaped map、slice 和字符串列表则完全 request-local。Core registry 与 core-agent 回归分别污染首轮 definition 的嵌套 schema / required 字段，断言 successor 和长期 registry/spec 都保持原样。该修复只隔离 presentation data，不扩大任何 tool 的执行权，不改变 semantic grant、dynamic alias、provider correlation 或 D2/D3 production gate。

### 9.95 GUI registry 的登记、读取与 definition metadata 必须使用同一 snapshot 纪律（本次实现）

GUI `ToolRegistry` 先前只复制 semantic catalog 元数据；`InputSchema`、`Required`、`ExecutionContract` 和 tags 仍可在 `Register` 后被调用方改写，`Get/List/ListAvailable` 也会把内部 map 再次交给 renderer。尤其 `registeredToolToDef` 将 `ExecutionContract` 原样写入 definition，之后 `attachExecutionContracts`、profile filter 或测试 hook 的修改可跨过 handler inventory 影响下一次 render。

现在 registry 在登记时取得 schema/required/contract/tags 的私有快照，在全部读取 API 返回独立副本，且 renderer 将 execution-contract metadata 再复制进 definition。新增回归从登记调用方、首次 `Get` 返回值和最终 rendered definition 三个方向注入 mutation，确认后续 snapshot 都不受影响。这是对 inventory ownership 的收紧；不改变 capability catalog 分类、路由选择、tool allowlist、动态 alias materialization 或生产 gate。

### 9.96 provider envelope 转换不得借用冻结 surface 的嵌套 schema（本次实现）

OpenAI Chat definition 转到 Responses / Anthropic wire envelope 时，转换器会创建新的顶层 map，但此前分别把原 definition 的 `parameters` 直接赋给 `parameters` / `input_schema`。这会让 provider SDK、兼容层 normalizer 或 transport observer 对最终请求体的就地改写反向污染 RunLoop 仍用于 receipt、response bind 与后继 request 的冻结工具面；问题不依赖 registry，任何 request-local surface 都可能受影响。

现在两个转换器都会在 envelope 边界递归复制 schema map、slice 与字符串列表，同时保留未知的非 JSON 值，以避免把既有 wire-freeze 的 fail-closed 行为误转为静默可序列化。新增 Responses / Anthropic 对称回归：篡改 converted payload 的顶层与深层 schema 后，原 request surface 必须保持不变。此改动只隔离 provider projection，不能证明 SDK 最终 serializer、provider replacement semantics 或 D3 production qualification；动态 alias 和 production gate 均保持关闭。

### 9.97 named JSON collection 也不得绕过 schema snapshot（本次实现）

此前 registry 与 provider envelope 的 clone helper 覆盖 `map[string]interface{}`、`[]interface{}` 等常见具体类型，但 Go schema producer 可以合法使用命名的 `type Properties map[string]interface{}`、`type Enum []string` 或它们的嵌套组合。它们仍是可变、可 JSON 序列化的 schema 数据；仅因运行时具体类型不同而原样返回，会使首次请求的 filter / provider projection 修改回写长期 inventory 或 frozen request surface。

现在 clone helper 会递归复制所有 string-keyed 的 map、slice 与 array（含命名类型），同时继续原样保留非 string-keyed map、pointer、struct 等非 JSON-shaped opaque 值，让最终 wire-freeze 维持既有 fail-closed 语义。Core registry、Responses、Anthropic 以及 Coding static compatibility clone 的回归均以命名 map/slice 类型篡改 converted/request-local schema，确认 successor 和 source 均未受影响；GUI device runtime、workflow specialization 与 client-tool schema clone 也统一复用同一 helper，避免各自的 partial type switch 再次分叉。本条继续只处理内存所有权；不替代 final serializer 验证、provider replacement certificate、D3 cohort 或 production alias gate。
