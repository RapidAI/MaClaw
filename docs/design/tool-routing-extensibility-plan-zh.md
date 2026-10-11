# 工具语义路由：skill / MCP 无限安装场景的扩展性问题与改进计划

> 目标：把"语义路由的受管上层建筑"从**有界集合设计**改造成**可随安装数伸缩**的设计。
> 范围：仅 skill / MCP 这类可无限安装的动态面（轨道 B / 受管语义面）；内置工具（轨道 A 的 legacy gateway）不在本次改动重点，因为它本来就是有界且已评审的。
> 所有代码位置均已在 `D:\workprj\aicoder` 仓库核对（2026-10-11）。

---

## 0. 根因（一句话）

受管语义面的四个核心部件——**能力本体、评审表、发现预算、治理面**——全部是写死在 Go 代码里的常数级结构，而**安装数是用户侧的变量**。当 N（已安装 server / skill 数量）增长时，这套常数结构会出现"装了也用不上""装多了召回崩""一个挂了整族不可行"三类结构性故障。

更反直觉的是：**托管度越高，可扩展性越差**——legacy 轨道 A 至少有 BM25 宽面 + 18 个动态工具名额，装了就能被检索到；受管轨道 B 反而装了也用不上。

### 上一轮一处说法修正

上一轮我说 `bestProvider` "排序键里没有 provider kind 的信任分级"。**这个说法不精确，修正如下**：

- `bestProvider`（`corelib/tool/semantic_planner.go:1531`）在初始化时 `sort.Slice(candidates, ... StableID())`，即候选先按 `Binding.StableID()` 排序；当 `provision.Quality` **相等**时，保留字典序更靠前的那个（"tie keeps the lower StableID"）。
- 而 `StableID` 以 provider **kind 字符串**开头（`builtin` < `host` < `mcp` < `skill`），所以 kind **确实**通过"字典序副作用"参与了 tie-break（`builtin` 会排在 `mcp` 前面）。
- 但它**不是安全语义**：只要质量严格更高（`provision.Quality > selectedQuality`），kind 就被直接压过；且 kind 的先后只是 `StableID` 前缀恰好按字符串排，没有任何"信任等级"概念。所以准确表述应为：**kind 仅作为相等质量下的字典序副作用，没有显式的信任分级；质量严格更高时任何 kind 都能压过 builtin，且不存在 per-tenant / per-user 覆盖位。**

---

## 1. 已核实的事实清单

| 部件 | 位置 | 现状 |
|---|---|---|
| 能力本体 | `corelib/tool/capability_ontology.go` `BuiltinCapabilityOntology()` | **44 条**，owner 全是 `core`，加新词必须改代码发版 |
| 按名评审（server-agnostic） | `reviewed_dynamic_bindings.go:56` `ReviewedMCPCapabilityContracts()` | `{work_log_query: read, work_log_update: update}`，**只看工具名**，任意后装 server 命中同名即继承声明 |
| 开放族 | `reviewed_dynamic_bindings.go:84` `ReviewedMCPOpenLookupContract()` | `CapabilityInformationLookup`，scope=reference，Quality=1，EffectReadOnly；**任意 MCP 自动获得** |
| 身份精确匹配 | `reviewed_dynamic_bindings.go:188` `ReviewedMCPCapabilityBindings()` | 仅 1 条：`global_key=enterprise_hub:mcp:hubcenter:web-search-prime` + tool=`web_search_prime`，Quality=3 |
| skill 通道 | `reviewed_dynamic_bindings.go:302` `ReviewedSkillCapabilityContracts()` | **空 map**，skill 侧目前零通道 |
| 选优 | `semantic_planner.go:1531` `bestProvider` | 只比 `Quality` 单标量；tie-break = StableID 字典序；**一条 need 只返回一个 provider** |
| 生命周期门禁 | `semantic_planner.go:1592` `providerLifecycleEligible` | kind 级 coverage 状态：非只读 provider 在 stale/incomplete 覆盖率下直接不可用 |
| 发布校验 | `semantic_catalog.go:510` `Publish` | 重复 adapter 报错、binding 必含 SchemaDigest、ParameterAuthorization 必填；每次发布 `Generation+1` |
| 网关剔除 | `semantic_legacy_gateway.go:26` | 受管面**没有** `discover_tool` / `call_mcp_tool` 这类网关（被明令剔除） |
| grant 身份 | `dynamic_capability_publisher.go` `ObservedBindingDigest` | provider 身份钉在 **schema digest** 上，vendor 改一个参数描述即废掉在途 grant |
| 发现预算 | `tools_search` 七态 + petition | 每轮固定 4 次搜索 + 每类 1 次 petition，**不随 N 伸缩** |

---

## 2. 五大结构性缺陷

### 2.1 安装是 O(1) 用户动作，可见性是 O(发版) 工程动作
用户装 50 个 MCP，能进受管语义面的只有"能挂到 `information.lookup` 的只读检索"，其余一律不可见。且**这个不可见是静默的**：受管面没有 `discover_tool` / `call_mcp_tool` 网关，`tools_search` 的七态里也没有"已安装但未评审"这一态——模型和用户都拿不到"你需要先去评审它"的信号。

### 2.2 按名授权是直接的提权面
`ReviewedMCPCapabilityContracts` 按 tool name 全局匹配，注释自己写着 *"a server installed later publishes through this row without a code change"*。任意后装 server 只要把工具命名为 `work_log_query`，就继承了被评审过的 `record.read.worklog`；命名为 `work_log_update` 则继承 `EffectExternalEffect`（外部系统写操作）。同文件里第二条通道是按 marketplace `global_key` 精确匹配，两条通道安全级别不同却并存——用错弱的那条就是权限提升。

### 2.3 选优只有单标量 + 无信任分级（已修正表述见 §0）
装一个被评审过但质量很差的 MCP，会在全局永久压过内置实现（`reviewedMCPWebSearchPrimeQuality=3` 压过 desktop 的 2 和 host 的 1），用户没有任何"用回内置"的开关。同时 `bestProvider` 一条 need 只返回一个 provider，装 5 个都能做 `information.lookup` 的 MCP 在语义层只剩 Quality 最高的那一个——生态多样性在语义层被压成由常量表决定的点。

### 2.4 预算恒定 vs N 单调上升（最硬的一条）
发现/补救预算（`tools_search` 4 次/轮 + petition 每类 1 次/轮）是**恒定**的，而召回压力随 N 上升。装得越多，未被评审的 provider 越多，但补救容量不动。

### 2.5 生命周期以 kind 为粒度 fail-closed
`providerLifecycleEligible` 下，一个 kind 的 coverage 不完整（`Incomplete`/`Stale`）就整族不可用；非只读 provider 在 stale 覆盖率直接不可用。N 个 server ⇒ "至少一个 kind 处于 incomplete" 的概率随 N 上升，而 planner 一有 `Unmet` 就整轮 fail-closed 或退轨道 A。`ObservedBindingDigest` 又把这个脆弱性放大：vendor 改一个参数描述，所有在途 grant 失效，N 越大"被别人的发布会打断"的概率越高。

> 附带：开放族 `information.lookup` 的边界是 **effect 声明而非行为证明**——一个声明 `EffectReadOnly` 的 MCP 照样能外发数据，而它正因为"只读"才不进 external-effect receipt 路径，等于它干了什么反而更不进账本。这是开放授权面必须配套的审计缺口。

---

## 3. 改进计划（按 ROI 排序，分阶段）

### Phase 0 — 撤销：tengyun 实例死绑定被否定，恢复开放按名（任意 server）✅ 已回退 (2026-10-11)
**问题**：§2.2 提权面——但用户的核心诉求是「随意安装 MCP 并调用，不要有死的绑定」。
**方向纠正（2026-10-11）**：原计划把 work_log 收窄到 `tengyun` server 实例，等价于用一个新的死绑定（硬编码 server ID）去堵旧的死绑定，与"随意安装、随意调用"目标相反，**已被否决并完全回退**：
- 删除 `ReviewedMCPCapabilityServerScope` 与 `mcpServerScopedAllowed`，`mcpContractsForObservation` 恢复单参 `identity` 签名；调用点改回 `mcpContractsForObservation(identity)`。
- 删除负向测试 `TestReviewedMCPWorklogNotGrantedToNonScopedServer`，并恢复 `TestReviewedMCPCapabilityBindingJoinsIdentityNotToolName` 中两条"任意 server 按名拿 work_log"断言。
- `work_log_query` / `work_log_update` 恢复为**对任意已安装 server 按名开放**（即"装任意 worklog MCP 即生效"，无需改代码、无需钉死 server ID）。
**为什么开放按名即可，不必 per-server 钉死**：`record.read/update.worklog` 是一等公民 capability——`intent_capability_rules.go` 主动产出这两个 need、`semantic_capability_policy.go` 做策略门禁、guiapp 有整套 worklog 路由测试。按名映射是"任意 MCP 工具 → 一等 worklog capability"的唯一桥接，删掉会让日常 worklog 功能整体失效。开放按名 ≠ 静默提权：`EffectExternalEffect` 本就走 external-effect receipt 审计路径，unreviewed（mcp）层不会静默提权；恶意同名 server 至多获得"被识别为 worklog 写入方"的能力，而这就是用户装 worklog MCP 时期望的行为。
**真正的硬化（留给 P1/P2，不是用死绑定去堵）**：把"按名授权"改为"按工具的 effect 声明 + 提供方信任等级（builtin>host>reviewed>unreviewed）推导"——声明只读的工具走只读、声明会写的工具走 `EffectExternalEffect` 且落在 unreviewed 层被审计/确认；审阅过的 server 才拿到信任加分。这样既不要求写死任何 server/工具名，又能把未审阅工具的 effect 正确入账（顺带修掉 §0 末尾"开放族把所有工具错标只读、绕过账本"的审计缺口）。

### Phase 0 硬化已落地：开放族按 MCP annotations 逐工具分类 effect（2026-10-11 完成）✅
> 这是 Phase 0 末尾"真正硬化"的最小可行实现：**满足"随意安装 MCP 并调用、不要死绑定"，且未评审工具也能写，但写走 receipt 审计而非静默提权。**
**改动**：
- 开放族 `information.lookup` 的 capability 信封（descriptor）由 `EffectReadOnly` 放宽为 `[]EffectReadOnly, EffectExternalEffect`（位置：`reviewed_dynamic_capabilities.go:328` `NewReviewedDynamicCapabilityRegistry` 与 `guiapp/semantic_tool_routing.go:418` `newIMSemanticCapabilityRegistry`——**两处必须同步**，否则 IM 发布校验器 `validateContract` 会 reject external_effect 合同；后一处注释明确要求与其保持一致）。
- 逐工具分类 `openLookupEffectsForTool(tool)`（`reviewed_dynamic_bindings.go`）：读 MCP `tools/list` 的 vendor `annotations`——`readOnlyHint==true` → `[EffectReadOnly]`（静默读路径）；其余（写/未注解）→ `[EffectExternalEffect]`（receipt 边界确认，fail-safe 不静默）。**信封放宽但逐工具收窄**，避免把纯读检索误判为写、也不破坏静默环境检索。
- 发布侧 `dynamic_capability_publisher.go` `publishReviewedMCPObservationForCapabilityWithOpenLookup` 按注解套用逐工具 effect（未评审名不进 reviewed 评审表，故不会拿到 `record.update.worklog` 等一等 capability）。
- 收据边界：`dynamicSelectionRequiresReceipt` 对含 `EffectExternalEffect`/`EffectSensitive` 的 selection 返回 true → 走 `executeDynamicExternalEffect`（awaiting_receipt 直到可信回执）。selection 的 Effects 来自 contract（非 descriptor），所以未注解工具的写 selection 必然 receipt 门控。
- 重放防误判：`CapabilityNeedHasSideEffect`（`corelib/tool/semantic_effects.go:128`）对 `information.lookup` 特例返回 false——放宽后的信封不会把"已成功的 lookup"错判为"未完成的 mutation"重放（否则每个纯读都会被塞进 receipt 边界、破坏静默检索）。
**验收（已跑）**：
- `guiapp` 4 个开放族/评审测试全绿：`TestReviewedMCPPublicationQuarantinesUnlistedTools`（未评审 `work_log_delete` 仍属开放族 ExternalEffect、但拿不到 reviewed `record.update.worklog` 能力）、`TestArbitraryMCPToolIsCallableThroughOpenLookup`（未注解工具 `Effects[0]==ExternalEffect` 且 `SelectionRequiresExternalReceipt` 为真）、`TestArbitraryMCPReachesTheModelSurface`（`selection.Effects[0]==ExternalEffect` 且落在 rendered 面）、`TestReviewedMCPCapabilityIdentityPublishesInstalledSearchTool`。
- `corelib/agentservice` 全绿；`corelib/tool/semantic_effects_test.go:51` 断言 `information.lookup` 在无 registry 下仍判 read-only（false）。
- `corelib/tool` 包仅有 `Test*RouteRevisionDoesNotProjectArtifactToDifferentProducer`（Memory/SQLite 两例）失败，**与本次改动无关**：`semantic_route_state_store.go` 实现未改，stash 掉 `semantic_effects.go` 后两测试依旧 fail，属 RouteArtifactRef 投影的 pre-existing 缺陷，不在本任务范围。
**未覆盖（已知边界，非回归）**：MaClawSrv / hub 的 HTTP 动态能力发布端点仍只验证 read_only 开放合同（子集合法、测试通过）；若要从服务端 API 也放开未评审写，需在这些端点的校验 registry 同步放宽信封——桌面 guiapp 路径已完整打通。

### Phase 1 — 本体受控扩展点
**问题**：§2.1 静默不可见 + 本体封闭。
**改动**：
- `capability_ontology.go` 增加外部命名空间约定：`ExternalCapabilityPrefix = "x:"`，并增加运行时注册器 `RegisterExternalCapability(ns string, def CapabilityDescriptor)`。
- Publish 校验：外部 capability 必须 `owner=vendor`、带 `ns` 前缀、且其 effect 集合**不超出该 namespace 声明的 effect 上限**（默认白名单 + 每 namespace 覆盖）。
- catalog 把外部 capability 与内置 44 条分开存储/展示，避免污染内置本体与评审表。
- 同步给 `tools_search` 增加状态 `installed-not-reviewed`：未在本体注册、也未在评审表的动态 provider，进入该状态并可被列出、可触发 petition。
**代码位置**：`capability_ontology.go`、`semantic_catalog.go:510` 的 Publish 校验、`dynamic_semantic_catalog.go`（状态枚举）。
**验收**：
- 装一个声明 `x:acme:translate` 的 MCP，断言进入面且被 effect 上限约束；越界 effect 被 Publish 拒绝。
- 断言 `tools_search` 对该 provider 返回 `installed-not-reviewed` 而非静默隐藏。

#### Phase 1 已落地（2026-10-11）：外部命名空间 + 运行时注册 + effect 上限校验 ✅
**落地点**（均在 `corelib/tool`，同包无需跨包 import）：
- `capability_ontology.go`：`ExternalCapabilityPrefix CapabilityID = "x:"` + `DefaultExternalCapabilityEffectCeiling = [read_only, external_effect]`（**默认排除 sensitive / local_mutation**：任意 vendor 能力可读、可对外部系统动手，但未经 host 显式放宽不得声明宿主侧敏感权或本地状态变更）。
- `semantic_catalog.go` `CapabilityRegistry`：
  - 新增字段 `externalEffectCeilings map[string][]EffectClass`（按 namespace 段覆盖默认上限）。
  - `Register` 加守卫：任何带 `x:` 前缀的 ID 必须走 `RegisterExternalCapability`，否则拒（内置/reviewed 能力不得占用外部前缀）。
  - 新增 `RegisterExternalCapability(ns, def)`：**允许在已 Seal 的 registry 上注册**（密封只锁内置词汇；后装的 MCP/Skill 合法引入新 x: 能力让 planner 能命名）；强制前缀、`owner=vendor`、namespace 归属、effect ⊆ 该 namespace 上限、`Consumes/Produces/Qualifiers` 合法；同 ID 幂等（相同描述 no-op）、冲突描述（版本/owner/effects 等不一致）拒绝。
  - 新增 `SetExternalCapabilityCeiling(ns, effects)`：host 显式放宽/收窄某 namespace 上限（reviewed 操作）。
  - `PublishWithCoverage` 的 provision 循环新增防御：外部 capability 的 provider `Effects` 必须 ⊆ 其**注册描述**的 effect 集合（即注册时已被 namespace 上限约束的那个集合），越界即拒——安装方无法静默扩大已评审范围。
**验收（已跑，`semantic_catalog_external_test.go` 全绿）**：
- `TestBuiltinRegisterRejectsExternalPrefix`：内置 `Register` 拒 `x:` 前缀。
- `TestRegisterExternalCapability*`：happy path；拒非 vendor owner；拒超默认上限的 effect（sensitive）；拒 namespace 不符；拒缺前缀；同 ID 幂等 + 冲突描述拒。
- `TestRegisterExternalCapabilityOnSealedRegistry`：密封 registry 上内置 `Register` 失败、外部注册仍成功（关键：后装能力不依赖重新发版）。
- `TestSetExternalCapabilityCeilingWidensRegistration`：host 放宽 namespace 上限后，vendor 能力可声明 sensitive。
- `TestPublishAcceptsRegisteredExternalCapability`：已注册外部能力 + 合规 effect 的 provider 发布成功。
- `TestPublishRejectsUnregisteredExternalCapability`：未注册的 `x:` 能力发布被拒（"unknown or deprecated"）。
- `TestPublishRejectsExternalEffectBeyondDescriptor`：provider 声明超出注册描述的 effect 被拒（"not permitted by its registered descriptor"）。
**依赖顺序 / 下一步（未做，独立增量）**：
- `tools_search` 的 `installed-not-reviewed` 状态**本轮 deferred**（见 Task #9）。它需要先解决「vendor MCP 如何声明自身 capability」这一输入契约——当前发布链路只把 MCP 映射到 builtin 能力或开放族 `information.lookup`，尚无 vendor 自声明 `x:` 能力的路径。该输入接通后，再在 discovery/planner 面加状态枚举，使「已装但未映射」的 provider 被列出而非静默隐藏。届时 Phase 1 全部验收点才闭合。
- 三包（`corelib/tool`、`corelib/agentservice`、`guiapp`）`go build` 均 EXIT 0。

#### Phase 1 ① 已落地（2026-10-11）：vendor 自声明能力 + installed-not-reviewed 可见性 ✅
> 这是 Phase 1 deferred 的 ①：让「装一个声明 x:acme:translate 的 MCP」真正进入面且被 effect 上限约束，并通过可见性枚举闭合 §2.1「装了但不可见」缺口。
**改动**：
- 数据透传 `DeclaredCapabilities []coretool.CapabilityID`：
  - `corelib/agentservice` `ObservedMCPTool`（发布输入）+ `MCPToolView`（tools/list 视图），在 `mcp.go` 的 remote `checkRemote` 与 local `DiscoverTools` 两处解析自定义 wire 扩展字段 `x_capabilities`（非标准 MCP；标准 MCP 缺省则该工具仅走开放族）。
  - `guiapp` `MCPToolView` + `mcpWireToolView`（`app_nl_mcp.go`，`toView` 映射）+ `local_mcp_client.go` `discoverToolsContext` 解析 `x_capabilities`；`reviewed_dynamic_publication.go` 转发到 `ObservedMCPTool`。
- 发布合并（关键：contract 按 `server+tool` 覆盖，不能二次 publish，否则冲掉开放族合同）：`publishReviewedMCPObservationForCapability` 对每个工具，把其声明的外部能力**合并进同一份 contract**——先 `registry.RegisterExternalCapability(ns, desc)`（desc.Effects = 该工具按注解收窄后的 effect 集合），再把 `x:` 能力作为 provision 追加；注册越界（effect 超出 namespace 上限）即 reject 并 fail-closed（该 vendor cap 不发布、不入可见性列表）。
- `installed-not-reviewed` 可见性枚举：`DynamicCapabilityContractPublisher` 加进程内 `declaredExternal` 映射（发布前清、逐工具记录、Revoke 时删），对外暴露 `ListInstalledNotReviewedProviders() []InstalledNotReviewedProvider`（ServerID / DeclaredCapabilities / Namespace / CeilingReviewed）。`CeilingReviewed` 由 `registry.ExternalCeilingIsDefault(ns)` 判定——host 未放宽 namespace 上限即 `false`（即「已装但 host 未评审」）。这是 `tools_search` / petition 面直接消费的查询 API。
- 注册器补 `ExternalCeilingIsDefault(ns)`（`semantic_catalog.go`）：比较生效上限与 `DefaultExternalCapabilityEffectCeiling`，供可见性枚举区分「默认上限」vs「host 已放宽」。
**验收（已跑，`dynamic_capability_publisher_external_test.go` 全绿）**：
- `TestPublishRegistersVendorDeclaredCapability`：声明 `x:acme:translate` 的只读 MCP → 能力被注册进宿主 registry，且该工具 contract 含 `x:acme:translate` provision（按名进入面，非仅开放族）。
- `TestPublishVendorDeclaredCapabilityListedNotReviewed`：发布后 `ListInstalledNotReviewedProviders` 返回该 provider，`CeilingReviewed=false`，declared caps 正确。
- `TestPublishVendorDeclaredCapabilityEffectCeilingEnforced`：host 把 `acme` namespace 上限收窄到 `read_only` → 写工具（external_effect）声明该 cap 被 reject、不入 registry、不入可见性列表（fail-closed）；但同 namespace 下只读工具仍正常发布——即「越界 effect 被拒、合规 effect 进入面」。
- `TestRevokeClearsInstalledNotReviewed`：Revoke 后该 provider 从可见性列表移除。
- `guiapp` 4 个开放族/评审测试仍全绿；`corelib/agentservice` 全包绿；`corelib/tool` 仅 2 个 pre-existing artifact 投影失败（无关）。
**已知边界（非回归，明确 deferred）**：
- `ListInstalledNotReviewedProviders` 是**消费 API**，本轮回合未改 `tools_search` 调度本身（避免在这棵共享脏树上大改 discovery 面）；Phase 3 预算伸缩会直接调用它。进程重启后该列表在下次 observation 自动重建。
- planner 路由目前仍把 vendor 自声明 `x:` 能力与开放族 `information.lookup` **并列**在同一 contract（quality 均为 1）；need 精确匹配 `x:acme:translate` 时优先命中该 provision。是否要让「vendor 声明了具体 cap 的工具」在 need 模糊时压过纯开放族，属 Phase 2 排序范畴，未在此做。
- 三包 `go build` 均 EXIT 0。

### Phase 2 — 两级排序 + 显式信任分级 + per-tenant 覆盖 ✅ 已落地（2026-10-11）
> 原 § 曾判定"当前数据模型下排序为 no-op，需先满足前置依赖"。本轮已补齐前置依赖（`TrustTier` 字段贯穿 contract→descriptor→ProviderSpec），并把分级逻辑落到 `bestProvider`。原判定中"单一 need 内 approved/unreviewed 永不竞争"仍是生产事实，故本 Phase 价值是**显式策略 + 防御未来 regression**，而非修当前行为；验收用"构造矛盾场景"证明分级真实生效。

**问题**：§2.3 + §0 修正点。
**改动**：
- 引入 `ProviderTrustTier`（`corelib/tool/semantic_catalog.go`）：`approved (builtin + reviewed) > unreviewed`。**zero 值即 `approved`**——50+ 处内置/host `ProviderSpec` 字面量无需改动，缺失字段不会降级可信 provider；只有开放族 `information.lookup` 显式标 `unreviewed`。`Rank()` 方法给出选择优先级。
- 字段贯穿：`DynamicCapabilityContract.TrustTier`（agentservice）→ `DynamicProviderDescriptor.TrustTier` → `ProviderSpec.TrustTier`；publisher 在 reviewed 契约（`work_log_read`/`work_log_update`/`web_search_prime`）标 `approved`、`ReviewedMCPOpenLookupContract`（`information.lookup`）标 `unreviewed`，经 `ProjectMCPDynamicProvider`/`ProjectSkillDynamicProvider` 投影。
- `bestProvider` 排序键改为三级：① trust tier（显式，approved 恒压 unreviewed）② `Quality`（仅同级内比较）③ `StableID` 字典序（确定性 tie-break）。
- per-tenant / per-user 覆盖位复用既有 `PinForCapability`（未新增代码），与 §7 一致。
- 一条 need 仍只选一个 provider（保持确定性）。tier 刻意**不进** `Digest`/`StableID`，tier 变更不旋转在途 grant。
**代码位置**：`semantic_catalog.go`（`ProviderTrustTier`/`ProviderSpec.TrustTier`）、`semantic_dynamic_provider.go`（`DynamicProviderDescriptor.TrustTier`+`Project`）、`dynamic_capability_contract.go`、`reviewed_dynamic_bindings.go`、`dynamic_semantic_catalog.go`、`semantic_planner.go:1531` `bestProvider`。
**验收（已跑）**：
- `corelib/tool` `TestToolPlannerTrustTierPrecedenceKeepsApprovedOverUnreviewed`：构造 builtin(approved, Quality 1) 与 mcp(unreviewed, Quality 9) 同提供 `visual.capture.desktop`，断言选 approved（反向验证：撤掉 tier 比较则 unreviewed 胜出、测试 fail）。
- `TestToolPlannerUnreviewedProviderStillSelectableWhenAlone`：单 unreviewed provider 无 approved 对手时仍可选（tier 不整体隐藏 unreviewed，避免破坏"装了就能调"）。
- `corelib/agentservice` `TestProjectMCPDynamicProviderProjectsTrustTier`：contract `TrustTier=unreviewed/approved` 经投影正确落到 `ProviderSpec.TrustTier`。
- planner 既有套件（含 Phase 4 的 `TestToolPlannerPhase4*`、Quality tie-break）全绿；agent 三包 `go build` 均 EXIT 0。
- per-tenant 覆盖：`PinForCapability` 仍为最高优先（pin 在 `bestProvider` 入口即排除非匹配实现，tier/Quality 不参与），既有 `semantic_provider_pin_test.go` 覆盖，未回归。

### Phase 3 — 发现预算随 N 伸缩
**问题**：§2.4。
**改动**：
- `tools_search` 的 petition 名额与搜索次数改为**至少按未评审 provider 数量线性增长**（例如 `base + k*unreviewedCount`，封顶防止滥用）。
- 配合 Phase 1 的 `installed-not-reviewed` 状态，让补救预算真正能触达新增安装。
**代码位置**：`semantic_legacy_gateway.go` / `dynamic_capability_publisher.go` 中 tools_search 调度逻辑、petition 配额常量。
**验收**：装 50 个未评审 MCP，断言 `tools_search` 能列出它们并标注 `not-reviewed`，且补救预算足以覆盖（不再出现"装太多召回不到"）。

#### Phase 3 已落地（2026-10-11）：发现预算随 N 伸缩 ✅
**实现**：`dynamic_capability_publisher.go` 新增纯函数 `RecommendedDiscoveryBudget(unreviewedCount int) DiscoveryBudget`——线性 `base(8) + per(2)*N`、封顶 `cap=128`、全程单调不降——与便捷方法 `(*Publisher) RecommendedDiscoveryBudgetForInstalled()`，直接消费 `ListInstalledNotReviewedProviders()` 的真实安装数。`DiscoveryBudget{PetitionSlots, SearchPasses}` 即本 Phase 的「petition 名额 + 搜索次数」。
**验收（已跑，`dynamic_capability_publisher_external_test.go` 全绿）**：
- `TestRecommendedDiscoveryBudgetScalesWithN`：N=0→8、N=10→28、N=50→108、N=200→封顶 128，且 0..200 全程单调不降。
- `TestRecommendedDiscoveryBudgetForInstalledTracksInstalledCount`：装 50 个未评审 MCP，断言 `ListInstalledNotReviewedProviders` 返回 50 个且全 `CeilingReviewed=false`（= not-reviewed），`RecommendedDiscoveryBudgetForInstalled()` 返回 108（=8+50×2，足以覆盖 50 个）。
**已知边界（deferred，非回归）**：`tools_search` 调度/UI 的**实际调用点**在当前代码不存在（grep 确认 `semantic_legacy_gateway.go` 仅含 legacy 网关名 + light-prompt 过滤，无 discovery/petition 调度；`petition` 概念全仓无现成落点）。本 Phase 交付"预算随 N 伸缩"的计算 API + 可见性枚举（Phase 1 ①），待上层 tools_search/UI 接入时调用 `RecommendedDiscoveryBudgetForInstalled()` 即闭合"调度消费"。未造新调度层，避免共享脏树大改引发回归。

### Phase 4 — 生命周期解耦（按 provider 降级而非按 kind fail-closed）
**问题**：§2.5。
**改动**：
- `providerLifecycleEligible` 改为以**单个 provider** 的 `Ready` / `ReadyUntil` 为准，移除"kind coverage incomplete ⇒ 整族不可用"的 fail-closed。
- 仅当**该 need 的所有候选 provider 都不可用**时才判 `Unmet`；单个 provider 探测失败只让它从候选集退出，不影响同 capability 的 builtin / 其他 provider。
- mutating（非只读）provider 仍需完整 fresh 覆盖率，保持既有安全语义。
**代码位置**：`semantic_planner.go:1592` `providerLifecycleEligible`、`:1622` `matchingUnreadyProvider`、planner 的 `Unmet` 判定分支。
**验收**：故意让一个 MCP server 探测失败（返回 negative observation），断言其余 provider 与该 capability 的 builtin 实现仍可被选中、整轮不 fail-closed。

**状态（已落地，2026-10-11）**：`providerLifecycleEligible` 的 `Incomplete` 分支新增按单 provider 降级——只读且 `Ready` 的 provider 在 family coverage incomplete 下仍 eligible，mutating（非只读）provider 仍 `return false` 保持 fail-closed。新增 `TestToolPlannerPhase4NegativeObservationKeepsSiblingSelectable` 与 `TestToolPlannerPhase4MutatingProviderStaysFailClosedOnIncomplete` 两个验收测试；放宽既有 `TestToolPlannerUsesFamilyCoverageWithoutHidingReadySibling` 断言（Incomplete 下 mcp 只读 sibling 也可选，不再必须选 complete 的 skill）；更新数据集 `coverage_unmet.json` 的 `generate-catalog-incomplete` 期望（只读 `search` 现可选，仅无 provider 的 `generate` 仍 unmet），移除"kind incomplete ⇒ 整族 unmet"旧假设。`guiapp` 开放族测试全绿。
**已知边界**：`corelib/tool` 全包当前另有 `TestDynamicToolBuilder_*` 4 例失败与 `Test*RouteRevisionDoesNotProjectArtifactToDifferentProducer` 2 例失败，均与本任务无代码交叉（前者在 `router_skill.go` 的 skill gateway 暴露逻辑、后者在 `RouteArtifactRef` 投影），属 pre-existing/并发会话 dirty，本任务未触碰、未修。

### Phase 5 — grant 身份放宽（可选，配套 Phase 1）✅ 已落地（2026-10-11）
**问题**：§2.5 中 `ObservedBindingDigest` 把身份钉死在 schema digest，vendor 改一个参数描述 → StableID/Digest 变 → 所有在途 grant 硬失效。
**改动**：
- `DynamicCapabilityContract` 新增 `StableBindingKey`（grant-stable 身份）：MCP = `global_key:capability:tool_name`，Skill = `stable_id:version:content_digest`。**刻意不含 observed schema digest**。
- `Digest()` 的 binding 组件由 `ObservedBindingDigest`（含 schema）改为 `StableBindingKey`。`ContractDigest` 与 `MCPToolBinding.StableID()` 因此不再随参数描述微调旋转。
- publisher 4 个构造点（MCP reviewed/open-lookup、Skill reviewed）填 `StableBindingKey`。
- `dynamicMCPContractMatchesEntry` 改为按稳定身份匹配；schema drift（observed digest 变但稳定身份不变）**不再 quarantine**，仅 `log` 提示"re-review suggested"。
- `ObservedBindingDigest` 字段保留（审计 + drift 检测），仅退出 grant 身份计算。
**验收（已跑）**：
- `TestPublishMCPStableIdentitySurvivesSchemaDrift`：vendor 改参数 description → 两次发布后 `StableBindingKey`/`Digest()` 不变、`ObservedBindingDigest` 旋转（drift 仍可被检测）。
- `TestPublishMCPDigestRotatesOnEffectChange`（反向验证）：effect 由 read-only 变 external_effect → `Digest()` 旋转、`StableBindingKey` 不变（同 tool/capability）——**实质性变更仍失效在途 grant**。
- `TestBindMCPToolKeepsGrantAcrossSchemaDrift`：schema drift 下 `StableID()` 保持（旧 `...RejectSchemaDrift` 已反转语义），`CallBoundTool` 不再 `mcp_binding_stale`。
- `corelib/agentservice` 全包绿（54s）；`guiapp` 4 个开放族测试绿。
**安全边界（非回归）**：mutating/effect 变更、tool 改名、capability 变更仍旋转 Digest → grant 正确失效（fail-closed 仅对"纯描述微调"放宽）；Skill 因 `StableBindingKey` 含 `content_digest`，content/version 变更仍失效（既有语义保持）。

---

## 4. 风险与回滚

| 改动 | 风险 | 回滚 |
|---|---|---|
| Phase 0 撤销 tengyun 死绑定 | 已回退为开放按名（任意 server）；残留风险=恶意同名 server 可继承 worklog capability | 接受为期望行为；真正硬化由 P1/P2 的「effect 声明 + 信任分级」承接，不依赖任何死绑定 |
| Phase 1 外部命名空间 | 外部 capability 可能声明过宽 effect | Publish 强制 effect 上限白名单；越界即拒 |
| Phase 2 两级排序 | 影响既有选优结果（如 web_search_prime 压 host） | tier 默认值保持 `reviewed > host`，过渡期等价旧行为；per-tenant 覆盖可逐个收回 |
| Phase 3 预算伸缩 | 预算过大可能被滥用/拖慢 | 设硬封顶常量 + 监控指标 |
| Phase 4 生命周期解耦 | mutating provider 误判可用 | 仅非只读 provider 要求 fresh 完整覆盖，保持 fail-closed |

**通用回滚**：每个 Phase 独立 PR、独立 feature flag（如 `semanticExt.ExternalOntology`、`semanticExt.TierSort`），可在不改代码路径的情况下逐相回退。Phase 0 必须先于 Phase 1 上线（安全前置）。

---

## 5. 测试矩阵（必带反向验证）

1. **安全负向**（Phase 0）：同名后装 server 不继承 reviewed effect —— 撤掉 identity 校验后应 fail。
2. **本体扩展**（Phase 1）：`x:` 命名空间注册 + effect 上限拒越界；非 `x:` 前缀外部 capability 被拒。
3. **排序**（Phase 2）：tier 优先于 Quality；per-tenant 覆盖；**撤修复再跑应 fail**（反向验证）。
4. **预算**（Phase 3）：N=50 未评审时 `tools_search` 全列出 + 配额够用。
5. **生命周期**（Phase 4）：单 provider negative observation 不触发整族 fail-closed。
6. **回归**：现有 `routingeval/data/*.json` 用例 + `TestTokenBankBucketsAreKnown` 风格的守卫测试在每相后全绿。

---

## 6. 可观测性补充

- 暴露指标：`unreviewed_provider_count`、`not_reviewed_listed_count`、`bestprovider_tier_fallback_count`（unreviewed 压过 builtin 的次数，应为 0）、`kind_coverage_incomplete_events`。
- `tools_search` 返回里区分 `reviewed` / `not-reviewed` / `pinned`，便于排查"装了用不上"类工单（与今早 `web_search_prime` 找不到的排查同源）。

---

## 7. 与既有工作的关系

- 上一轮讨论的 **provider pinning**（用户点名寻址）解决"能不能指定实现"；本计划 Phase 0/2/4 解决"规模伸缩与默认安全"。两者互补，pinning 对应的 `PinForCapability` 已是 `bestProvider` 的一等公民，Phase 2 的 per-tenant 覆盖复用同一机制。
- legacy 轨道 A 的 BM25 + 18 动态名额**保留为逃生阀**：当受管面因预算/评审来不及覆盖时，动态检索仍能兜底，不应在本案中被削弱。
