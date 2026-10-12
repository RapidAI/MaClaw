# 任意 MCP「安装即可运行」改进方案

日期：2026-10-12　仓库：`D:\workprj\aicoder`
前置调查：`.workbuddy/outputs/mcp-install-exec-path-2026-10-12.md`

---

## 0. 一句话结论

**现状做不到「任意 MCP 装完即可运行」，根因不是哪一个闸门写错了，而是供需匹配架构：需求侧（need）只能来自 host 发布的静态规则表，供给侧（MCP 工具）是动态的，两者只能靠 capability ID 相遇。** 因此方案不是"打开一个开放网关"（那条路被刻意封死且正确），而是分三步：先给每个已安装工具**发一个稳定的能力身份**，再开一条**用户点名的显式调用通道**，最后把**评审做成提权阶梯**而不是准入开关。

---

## 0.5 Review 修订记录（实施前复审，2026-10-12）

初版方案写完后逐条查证，发现三处会把它做错或做漏的地方，已据此修订。**P0 的 P0-1b 被撤销**，P1-1 的能力描述符设计被推翻重写。

### 修订 1（撤销 P0-1b）：AutoStart 默认 true 是错的方向

初版把「新装默认 `AutoStart=true`」当成 P0-1。查证后发现**本会话可运行性根本不靠它**：

- `clientForOwner`（`guiapp/local_mcp_manager.go:434`）**本来就会按需启动**，并且完整记录失败/就绪（`:470-521`）。
- 但 `CallToolForOwner` 的**共享路径**（无 ownerID）在 `m.clients[serverID]` 缺失时直接返回 `"local MCP server %s not running"`（`:427`）。

也就是说：装了但没被 sync 过的本地 MCP，owner 路径能拉起、共享路径拉不起——**同一个 server 两条路径行为不一致**。这既是「装完不可用」的真正原因，也说明 AutoStart 只是启动延迟优化，不是正确性问题。

**修订**：撤销 P0-1b（不再默认改 AutoStart，理由有三：① 按需启动已足够；② 应用启动时拉起 N 个第三方进程是无谓的攻击面变化；③ `LocalMCPServerEntry.AutoStart` 是 `bool` + `omitempty`，**无法区分"未设置"与"用户显式关闭"**，贸然置 true 会让 `UpdateLocalMCPServer` 复活用户关掉的 server——正是 Hub 策略"缺键≠显式 false"的同款陷阱）。改为 **P0-1a：共享路径补齐按需启动**，与 owner 路径共用同一套 durable 记账。

### 修订 2（P1-1 重写）：自动生成的能力描述符必须与 schema 解耦

初版只写了 `id := x:mcp:<serverID>:<tool>` 和 Summary 不含身份，**漏了 `Version` 和幂等语义**。查证 `RegisterExternalCapability`（`corelib/tool/semantic_catalog.go:362`）发现：

- 它**要求** `Owner == "vendor"` 且 `Version` 非空，否则直接报错（`:378-386`）；
- 相同 ID 重复注册**只有 descriptor 完全相等才幂等**，否则返回 `conflicts with an already registered descriptor`（`:417-423`），**不会更新**；
- 相等性比较覆盖 `Version / Owner / Summary / Effects / Consumes / Produces / Qualifiers`（`externalDescriptorEqual:434`）。

后果：若按初版设 `Version = tool schema digest`、`Summary = tool.description`，则 **MCP 作者每次改 schema 或改工具描述，都会让重新发布硬报错**——observation 循环直接被毒死，比"静默不可见"更糟。

**修订**：capability 描述符必须**完全稳定、与 schema 无关**：
- `Version` = 固定常量（如 `"1"`），**不是** schema digest；
- `Summary` = 确定性模板（namespace + tool slug），**不取** tool.description；
- schema 漂移只反映在 **provider binding 的 `SchemaDigest`** 上（该机制已存在：`BindMCPTool`/`ObservedBindingDigest`），不碰 descriptor；
- `Effects` 逐工具由 `openLookupEffectsForTool` 决定，仍受 `ExternalEffectCeiling` 约束。

### 修订 3（P0-2 的隐藏依赖）：放行 pending 必须同时补上失败记账

初版只改闸门，没说谁计数。查证发现 `recordRuntimeSyncFailure` / `recordRuntimeSyncReady`（`local_mcp_manager.go:325/343`）**只对 market 受管 server 生效**（`:331-339` 的显式保留），且远程路径原本**根本不记账**。

后果：一旦把 pending 放行，远程 managed server 的 `ensureSession` 失败不会计数，`Attempts` 永不增长，`needs_review` 永远升不上去——**闸门被放宽成永久放行**，正是本项目最忌讳的 fail-open 形态。

**修订**：P0-2 必须与 P0-1a 同批交付，且远程路径要补对称的 `recordRemoteRuntimeSyncFailure`（同样只对 managed 生效）。本次实施已一并完成。

### 修订 4（P0-1a 的守卫）：按需启动必须复用同一个运行时闸门

初版没提按需启动与 `blocked` 集合的关系。查证 `syncFromConfig`（`local_mcp_manager.go:150-160`）发现 `mcpRuntimeSyncAllowsAutomaticStartForManager` 会把 needs_review / backoff 的 server 标成 `blocked` 并拒绝启动。

后果：若按需启动不看这个闸门，`needs_review` 的 server 虽然被调用闸门拦住，却仍可能被另一条路径拉起进程——**终态被绕开**。

**修订**：`startSharedClient` 在 fork 前必须调 `mcpRuntimeSyncAllowsAutomaticStartForManager`，与 sync 路径同一判据。已实施并配测试。

---

## 0.6 实施状态（2026-10-12）

| 项 | 状态 |
|---|---|
| **P0-1a 共享路径按需启动** | ✅ 已实施（`guiapp/local_mcp_manager.go` 新增 `startSharedClient`）+ 5 个测试 |
| **P0-2 三态闸门 + 远程失败/成功记账** | ✅ 已实施（`mcpRuntimeSyncGate`、`recordRemoteRuntimeSyncFailure`、`mcpRuntimeSyncShouldRecordReady`）+ 11 个测试 |
| **P0-2b 崩溃进程替换** | ✅ 已实施（`watchProcess` 只清 running 不摘 clients）+ 1 个测试 |
| **P0-2c MaClawSrv transport 同缺陷修复** | ✅ 已实施（`corelib/agentservice/mcp_integration.go` 复用 `rt.startLocal`）+ 1 个测试 |
| P0-1b AutoStart 默认 true | ❌ 撤销（见修订 1） |
| **P0-5 需求侧：已安装 MCP 不受 label 清单限制** | ✅ **已实施（本轮）**，见 §3.5 —— 这是「阻塞 B」的核心治理 |
| **P0-6 contract-only required need 降级** | ✅ **已实施（本轮）**，见 §3.6 |
| **P0-7 unmet 拒绝时记录已安装侧（可观测性）** | ✅ **已实施（本轮）**，见 §3.7 |
| P0-3 `mcpOpenLookupReady` TTL + 后台刷新 | ⏸ 未实施（可见性问题，不阻塞可运行性） |
| P0-4 未评审清单接 UI | ⏸ 未实施（需动 Wails 绑定三处 + 前端） |
| P1-1 自动生成 per-tool capability | ⏸ 未实施，设计已按修订 2 重写 |
| P1-2 用户点名 pin 通道 | ⏸ 未实施 |
| P2 评审闭环 + require_confirmation | ⏸ 未实施 |

**终态验证（第二轮复审后）**

| 项 | 结果 |
|---|---|
| `go build ./...` 全仓库 | ✅ 通过 |
| `go vet ./guiapp/ ./corelib/agentservice/` | ✅ 干净 |
| `gofmt` | ✅ 本次 7 个文件干净（`app_nl_mcp.go` 的差异在 23-58 行，是仓库既有漂移，本次改动在 811-906 行，不重叠） |
| `go test ./guiapp/` MCP 全集（39 例） | ✅ **ok 118.2s** |
| `go test ./corelib/agentservice/` MCP 全集 | ✅ **ok 9.6s** |
| `node scripts/check-agent-architecture.mjs` | ✅ `Agent architecture guard passed.` |

既有失败 3 例已判定与本次改动无关，明细与依据见 §6.2。

---

## 1. 四个阻塞（按性质分类）



### 阻塞 A — 能力身份缺失（架构性，P1 解决）

无评审名的工具只得到 `ReviewedMCPOpenLookupContract()`（`corelib/agentservice/reviewed_dynamic_bindings.go:89`），它 provision 的 capability 恒为 `information.lookup`。一个"发 Jira 单"的 MCP 工具和一个"查天气"的 MCP 工具，在语义层是**同一个 capability**。

后果：模型这轮的 need 若是 `information.lookup` 才可能命中；其他 need 永远匹配不上 → 工具不可选。

### 阻塞 B — 需求侧是静态规则表（架构性，P1/P2 解决）

`needs` 的唯一生产路径：

```
resolveIntentLabelCapabilityNeeds(registry, rules, minConfidence, classification)
  corelib/agentservice/dynamic_semantic_routing.go:424
→ rules: map[intent.IntentLabel][]IntentCapabilityNeedTemplate   (:206)
→ needs 喂给 ToolPlanner.Plan                                    (:1518)
→ bestProvider 按 Capability 匹配 provider                        (semantic_planner.go:1531)
```

`rules` 是 host/owner 发布的表。**已安装的 MCP 不会自动往这张表里加行**，所以装再多也只是供给侧变多，需求侧不变。

配套地，开放网关 `call_mcp_tool` / `discover_tool` / `manage_skill` 被 `legacyDynamicGatewayNames`（`corelib/tool/semantic_legacy_gateway.go:26`）在 `ClosedManagedDefinitions:62` 硬剔除，并有 `scripts/check-agent-architecture.mjs:385-389` 门禁。**这条封锁是对的，本方案不解除它。**

### 阻塞 C — 运行时闸门（工程性，P0 解决，最容易修）

| 闸门 | 位置 | 现状 |
|---|---|---|
| 新装 stdio 默认 `AutoStart=false` → 重启不拉起 | `guiapp/app.go:1973` | 配置还在，进程没了 |
| market 受管**缺状态即 pending** → 拦启动 + 拦调用 | `guiapp/mcp_runtime_sync_state.go:118,133-139` | 注释明写"光有配置不算就绪证据" |
| 3 次失败 → `needs_review` 终止态 | 同上 `:269`，上限 `:34` | 只能 `RetryMCPRuntimeSync(id, true)` |
| `mcpOpenLookupReady` 依赖内存态观测 | `guiapp/reviewed_dynamic_publication.go:80` | 远程 health 是内存态，重启即失；本地靠 `IsRunning` |

### 阻塞 D — 「已装未评审」没有出口（纯接线缺失，P0 解决）

`ListInstalledNotReviewedProviders`（`corelib/agentservice/dynamic_capability_publisher.go:666`）与 `RecommendedDiscoveryBudget`（`:726`）**零生产调用方**（全仓 grep 确认，只有测试）。设计文档 `docs/design/tool-routing-extensibility-plan-zh.md:45-46` 已记此缺陷：不可见是**静默**的，用户和模型都拿不到"需要先评审"的信号。

---

## 2. 设计原则（四条，含不可越界清单）

**原则 1 — 解耦「能否运行」与「授予多少权限」。**
安装 ⇒ 可被点名调用（`read_only` 直接跑；写操作走 receipt 账本）。评审 ⇒ 提权（更高 TrustTier / Quality、更宽 effect、进入自动路由）。**评审是提权，不是准入。**

**原则 2 — 默认仍然 fail-closed。**
自动生成的能力 ceiling 沿用 `DefaultExternalCapabilityEffectCeiling`（`corelib/tool/capability_ontology.go:25`）= `[read_only, external_effect]`，**刻意不含 `sensitive` 与 `local_mutation`**。这条不许改。

**原则 3 — need 只能由 host 产生，永不由模型产生。**
与 `RouteRequest.Pins` 的既有约定一致（`semantic_planner.go:269-274` 注释）。本方案新增的 need 来源只有两处：静态规则表、**用户/host 的显式动作**。

**原则 4 — 不解除的既有封锁（越界清单）：**

- `legacyDynamicGatewayNames`（开放网关黑名单）— 不解
- `IsDisabledExternalCodingSessionTool`（`create_session`/`send_and_observe`/`control_session` 永久黑名单）— 不解
- "a pinned provider that is not Ready is not selectable"（`semantic_planner.go:272-274`）— 不绕过
- `ClosedManagedDefinitions` 的空 grant 表 = 空面（fail closed）— 不改
- `needs_review` 作为合规终态 — 保留，但必须有 UI 出口

---

## 3. 目标形态：三级阶梯

| 级别 | 触发 | 效果 | 是否需评审 |
|---|---|---|---|
| **L0 已安装** | 装完 | UI 可见、可手动试调用、状态透明（不再静默） | 否 |
| **L1 可点名** | 用户在 UI/IM 点名该 MCP 工具 | host 发 need + pin，模型本轮可调用；写操作走 receipt | 否 |
| **L2 可自动路由** | 用户点「批准」完成评审 | 写入 namespace ceiling + need 规则，语义面自动命中 | 是 |

「只要能安装上，就可以运行」= **L0 + L1 无条件成立，L2 需要一次点击。**

---

## 3.5 P0-5：需求侧解耦——已安装 MCP 不再受 label 清单限制（已实施）

对应「阻塞 B」的根治理。用户目标原话：**任何已经安装的 mcp 工具都可以被调用**。

### 为什么不是「把 worklog 从排除清单里删掉」

初版方案倾向直接把 `LabelWorklogRead/LabelWorklogUpdate` 从 `WantsAmbientRetrieval` 的排除列表移除。这是**错误的修法**：清单式放行意味着每接一个新企业 MCP，都要有人想起回来改这个文件；漏改的失败形态与本次故障**完全一样且同样静默**。

根因是**两个正交决策被合并成一个函数**：

| 决策 | 含义 | 谁负责 |
|---|---|---|
| `WantsAmbientRetrieval` | 是否挂 **host 自己的**知识库/记忆伴侣 | host，保守允许 |
| `CallerWantsDynamicLookup`（新增） | 这一轮**能否调用用户已安装的 MCP** | 用户安装行为的后果 |

worklog 不该长 host 知识库伴侣（**这一点仍然成立，`WantsAmbientRetrieval` 一行未改**），但它必须能被用户装的那个 MCP 服务。合并二者使后者被前者的保守默认值连坐。

### 改点

1. `corelib/intent/ambient_retrieval.go` 新增 `CallerWantsDynamicLookup(primary IntentLabel) bool`，与 `WantsAmbientRetrieval` 完全独立。
   - 按**什么让开放族不安全**写，而非枚举"今天碰巧能用的 label"：仅投递 / 启动 / 删除 / 配置 / 会话 / 管理员 / 时钟 / 音频 / 截图 / 桌面控制返回 `false`——这些**结果形态不可能由「调某个 vendor 工具」产出**，且它们的 reviewed *host* provider 必须赢。
   - 其余一律 `true`。**安全不来自这张清单**：unreviewed 工具运行在按其自身 vendor annotation 收窄的 effect 包络内，非显式 read-only 的一律走 external-effect receipt 边界。
2. `guiapp/semantic_tool_routing.go` 挂载点改为 `semanticPermitsDynamicLookup(ctx, primary, slimOffice)`，只保留两类真正该拦的理由：结果形态（上表）+ 会话延续（petition 重规划 / slim 继续）。
   - ⚠️ `slimOffice` **作为参数传入而非在 helper 内重算**——重算会与调用点漂移，对"这是否为延续轮"给出不同答案。

### 兼容既有原则

- 原则 3（need 只能由 host 产生）：满足，新增 need 仍由 host 代码产生。
- 原则 4 越界清单：**一条未触碰**。`legacyDynamicGatewayNames` 开放网关黑名单照旧封死。

## 3.6 P0-6：contract-only required need 的降级 graft（已实施）

即使 P0-5 生效，仍有一类故障**逃过去**：某个 required need 的 capability **只能**由安装的后端提供，而这轮没有 provider ⇒ 直接 `no_feasible_provider` 整轮拒绝。`record.read.worklog` 正是此类（`reviewedContractOnlyCapabilities` 已声明它无 host attach site）。

新增 `semanticNeedsWithContractOnlyGraft(needs, providers, pins)`，在 `Plan` 前、provider 列表已完整时执行。**它做两件事，缺一不可**：

1. **把原来那个 required need 降级为 optional**（`out[index].Required = false`）；
2. 追加一个 `Required:false` 的 `information.lookup` need。

> ⚠️ **第 1 步是必需的，不是可选优化**（第三轮 review 才发现漏了它）。初版只做了第 2 步，结果 **graft 完全没有作用**：`semantic_planner.go:609` 的 `record()` 以 `need.Required` 决定把无 provider 的 need 记进 `plan.Unmet` 还是 `plan.Omitted`，required 的仍进 Unmet → 整轮照旧被拒。当时所有单测都只断言"needs 列表多了一条"，**没有一条端到端跑 planner 断言 plan 成功**，所以缺陷被完全掩盖。修好后补了端到端守卫 `TestZZGraftActuallyRescuesThroughPlanner`（跑真实 `ToolPlanner.Plan`，断言 `plan.Unmet` 为空且有 selection）。

**四道收敛，缺一即 fail-open**：

1. 仅 **reviewed contract-only** capability 参与（无 host attach site，缺少安装后端时无任何替代路径）。
2. 仅当该 capability 在本轮 provider 列表中**确无 provider**（这就是原本会拒绝的那一轮）。
3. 仅降级到 `information.lookup`——语义即"调用已安装的那个工具"，以 `TrustTierUnreviewed` 发布，effect 包络由该工具自身 annotation 收窄。
4. **用户显式 pin 过该 capability 时绝不降级**：用户点名了实现，换一个实现是答非所问。
5. **安装的替代工具必须真能承载该 need 的 effect**（`EffectsWithinCeiling(needEffects, providerEffects)`）。**写操作只能由"自身 annotation 承认了 external effect"的工具承载**；若装的全是只读工具，required 的写**保持 required、继续 fail-closed**。
   - 理由：降级一个 required **写**却无工具能写，会让 turn 照常进行而**写入静默不发生**——用户拿不到任何提示，对工时这类 system of record，丢一次写是不可见的。这比它替代掉的那句明确拒绝更糟。
   - 注意 `openLookupEffectsForTool` 对单个工具只返回 `read_only` **或** `external_effect`，不会两者都有，所以"读+写两条腿"的场景需要两个 provider 才能都降级（守卫测试即按此构造）。

附加约束：graft 目标自身必须已发布且 Ready，否则只是把失败推后一步且解释更差。**什么都没装时仍然干净失败**（不掩盖真实缺失，守卫 `TestZZGraftStaysClosedWithNothingInstalled`），真实 reviewed provider 凭更高 Quality/TrustTier 照样优先（有 provider 时根本不进此路径）。

**同一 capability 只有一条 need**：ID 由 capability 派生，故按 capability 分组、每个只 graft 一次，并对 capability 排序消除 map 遍历顺序。否则同一 capability 的两条 required need（read+update 双腿、repeat sibling）会生成**重复 need ID**，而 `semantic_planner.go:587` 对重复 ID 直接 `return error` —— 会把可救的轮次变成硬规划失败（比原 bug 更糟）。守卫 `TestContractOnlyGraftMintsOneNeedPerCapability` / `...IsDeterministicAcrossCapabilityOrder`。

**"可用 provider" 只有一个定义**：`providerProvisionList` 统一过滤 `!Ready`，ambient 挂载路径与 graft 共用它。过滤 down provider 是必要的——provider 存在但观测转负时 planner 走的是 `not_ready`（有专属文案），不应抑制 graft 去用另一个健康工具；反之若把 down 计入"已安装可服务"，唯一 work-log MCP 坏掉的用户会重新撞上原始故障。守卫 `TestContractOnlyGraftIgnoresADownProvider` / `TestDynamicLookupNeedIgnoresADownProvider`。

导出 `ReviewedContractOnlyCapabilityIDs()` / `IsReviewedContractOnlyCapability()`，使 wiring 测试消费的字符串清单与 capability ID 清单由同一来源导出——两份清单漂移是静默的，只加一边会导致该族永久不可 graft 或永久不可接线。

## 3.7 P0-7：unmet 拒绝时记录「已安装侧」（已实施）

本次排查耗时最久的直接原因：`no_feasible_provider` 在以下两种情形**输出完全一致**：

- A. 什么都没装。
- B. 用户装的 MCP 健康、tools/list 成功、契约已发布，**只是没有 need 点名它**。

两者修法完全不同，而日志无从区分。新增 `logSemanticUnmetWithInventory`，在 unmet 拒绝时打出：

```
[semantic-routing] unserved needs unmet=[...] installed_mcp=[serverID(工具数) ...] contract_only_published=[...]
```

只读已发布契约，**不可能放宽任何面**。以后此类故障应可分钟级定位。

## 3.8 本轮验证

| 项 | 结果 |
|---|---|
| `go build ./corelib/... ./guiapp/` | ✅ 通过 |
| `go vet ./guiapp/` | ✅ 干净 |
| `gofmt` 本次改动文件 | ✅ 干净 |
| `node scripts/check-agent-architecture.mjs` | ✅ `Agent architecture guard passed.` |
| 新增回归测试 7 例（`guiapp/zz_installed_mcp_reachability_test.go`） | ✅ 全 PASS |
| **反向验证** | ✅ 把 `LabelWorklog*` 塞回新函数排除清单 → 2 例立刻 FAIL，证明测试在守性质而非空转 |

既有失败 4 例已用 stash 基线条条实证与本次无关（`TestZipDirectoryBytesRejectsSymlinkRootAndSpecialFiles`、`TestSQLiteJobRepositoryAdmissionIsUniqueAcrossHandles`、`TestMCPRegistryWarmServerToolsTimesOutWithoutBlocking`、`TestRegisterMCPServerImmediatelySyncsTools`）。

> ⚠️ **用 stash 做基线验证的陷阱**：新增测试文件是 untracked 且引用改动内的新符号，只 stash 源文件会 `[build failed]`，易被误读为"改动破坏编译"。**须先把 untracked 新测试移出，再 stash 源文件。**

## 4. 分阶段方案

### P0 — 工程解除（不改安全模型，风险最低，建议先做）

> ⚠️ 本节 P0-1/P0-2 已按 §0.5 修订 1/3/4 重写并实施；P0-1b 撤销。

**P0-1a 共享路径补齐按需启动（已实施）**
- 问题：`clientForOwner`（`local_mcp_manager.go:434`）本就按需启动并记账，但无 ownerID 的共享路径在 `:427` 直接返回 `not running`
- 改点：`guiapp/local_mcp_manager.go` 新增 `startSharedClient(serverID)`，与 owner 路径共用同一套 start → DiscoverTools → durable 记账 → `publishReviewedServerTools`
- 守卫（缺一即 fail-open）：
  1. `entry.Disabled` 仍拒绝
  2. fork 前调 `mcpRuntimeSyncAllowsAutomaticStartForManager`，与 `syncFromConfig` 的 `blocked` 同判据，防止 `needs_review`/backoff 被按需启动绕开
  3. `recordRuntimeSyncReady` 写失败 → 停进程并 fail closed（与 `clientForOwner:515-521` 一致）
- **撤销的 P0-1b**（新装默认 `AutoStart=true`）：见修订 1，三条理由（按需启动已足够 / 启动期拉起 N 个第三方进程 / `AutoStart` 是 bool+omitempty 无法区分未设置与显式关闭）

**P0-2 三态闸门（已实施）**
- 改点：`guiapp/mcp_runtime_sync_state.go` 新增 `mcpRuntimeSyncGate(serverID) (blocked bool, reason string)`，替换两个**调用**路径的布尔判断（`local_mcp_manager.go:409`、`app_nl_mcp.go:823`）
- 新语义：
  - `needs_review` → 拦（保留，终态）
  - `ready` → 放
  - `pending` 且 `Attempts >= 3` → 拦（防无限重试）
  - `pending` 且 `NextRetryAt` 未到 → 拦（**保留退避**，防打爆）
  - `pending` 且预算未用尽 / 无 durable 状态 → **放行，调用本身即探测**
- 必须同批交付：远程 `ensureSession` 失败调 `recordRemoteRuntimeSyncFailure`（仅 managed，与本地同保留），否则失败不计数、`needs_review` 永不升级 = 闸门被放宽成永久放行
- **未改**：清单/发现路径仍用 `mcpRuntimeSyncPending`（本轮只放开"可运行"， blast radius 最小）

**P0-3 `mcpOpenLookupReady` 加 TTL + 后台刷新**
- 改点：`guiapp/reviewed_dynamic_publication.go:80`
- 现在依赖内存态 `remoteHealthObservation`。改为把观测结果（含时间戳）持久化，TTL 内视为 ready，过期后台异步刷新而不阻塞可见性
- **防退化**：从未成功观测过 → 仍 false（不许把"没观测过"当"观测成功"）

**P0-4 「已装未评审」接进 UI**
- 改点：新增 Wails 绑定暴露 `ListInstalledNotReviewedProviders`；前端 `guiapp/frontend/src/components/remote/MCPManagementPanel.tsx` 加"已安装 · 未评审"徽标与去评审入口
- 纯可见性改动，不动任何权限判定

### P1 — 能力身份 + 点名通道（核心）

**P1-1 为每个已安装工具自动生成稳定 capability**

在 `corelib/agentservice/dynamic_capability_publisher.go:225 publishReviewedMCPObservationForCapability` 的逐工具循环里，对**每个** observed tool（不再等 `openReady`）：

```
ns := "mcp:" + serverID
id := ExternalCapabilityPrefix + ns + ":" + slug(toolName)     // x:mcp:<serverID>:<tool>
```

注册要点（逐条对应已修过的历史 bug）：

1. `Effects` 用 `openLookupEffectsForTool(tool)`（`reviewed_dynamic_bindings.go:111`）：显式 `readOnlyHint==true` → `read_only`，否则 → `external_effect`
2. **必须显式调用 `EffectsWithinCeiling(effects, registry.ExternalEffectCeiling(ns))`**。理由：`RegisterExternalCapability`（`semantic_catalog.go:362`）内部的 ceiling 校验在 `desc.Effects` 等于 ceiling 时**恒真**（`semantic_catalog.go:206-220` 注释明确要求调用方另行校验）—— 这是最可能做成 fail-open 的一处
3. `Summary` 用 tool 自带的 description，**不含 serverID / toolName**。此处与 §8.2 的跨 server 冲突场景不同：ID 已含 serverID 做 namespace 隔离，天然不会跨 server 撞车；但如果图省事把身份写进 Summary，会重现已修的 descriptor 冲突 bug
4. namespace = `mcp:<serverID>`（每 server 一个），便于按 server 粒度评审 ceiling
5. 保留原有 `information.lookup` provision，不破坏现有匹配
6. 加数量上限（每 server 工具数、全局总数），防 catalog 随 N 膨胀（对应设计文档 §2.4）

**为什么 unreviewed 不会抢位**：`bestProvider` 排序是 `TrustTier.Rank() → Quality → StableID`（`semantic_planner.go:1565-1576`），`TrustTierUnreviewed` 恒低于 approved。**这条排序不许动**，并用测试钉死。

**P1-2 用户点名的显式调用通道（pin）**

- 复用既有 `coretool.ProviderPin{Capability, Kind:"mcp", ProviderID}`（`corelib/tool/semantic_provider_pin.go:41`）
- 改点：`corelib/agentservice/dynamic_semantic_routing.go:416 finishIntentNeedResolution` 追加一路：**由用户显式选择**（UI 勾选 / IM 点名）产生 need + pin
- 约束：
  - need 由 host 从用户动作产生，**不是模型产生**（遵守原则 3）
  - 仍走 `providerLifecycleEligible`（`semantic_planner.go:1602`）—— 不 Ready 的 provider 即使被 pin 也不可选（现状注释已承诺，不许绕）
  - 写操作仍走 receipt 账本（`dynamic_semantic_execution.go:350`）

### P2 — 评审提权闭环

- UI「批准」→ `registry.SetExternalCapabilityCeiling(ns, effects)`（`semantic_catalog.go:331`）+ 把该 capability 写入 per-install 的 need 规则（持久化）→ 进入 L2 自动路由
- **建议加一条**：未评审 namespace 上的 `external_effect` 调用，注入 `require_confirmation` 约束。机制已存在（`semantic_planner.go:1727 constraintRequiresConfirm`，要求 `RoutingConstraint{Effect:"require_confirmation", Authority ∈ policy|runtime|channel}`）。**这是"装完即可写外部系统"唯一的安全缺口，必须补**，否则 L1 等于"任意 MCP 装完即可对外部系统产生副作用且无确认"
- 保留 `needs_review` 终态，但 UI 出口（`RetryMCPRuntimeSync(id, true)`）要在面板上显性可达

### P3 — 规模化（另立一轮）

1. `bestProvider` 一条 need 只返回一个 provider（§2.3）→ 支持多 provider
2. `RecommendedDiscoveryBudget`（`dynamic_capability_publisher.go:726`）接线到实际调度
3. 受控发现网关（需单独拍板，**不做成 open gateway**）

---

## 5. 风险登记册（按历史踩坑模式整理）

| # | 陷阱 | 后果 | 防线 |
|---|---|---|---|
| R1 | `RegisterExternalCapability` 的 ceiling 校验在 `Effects == ceiling` 时恒真 | 静默越权 | 显式 `EffectsWithinCeiling` 校验 + 测试 |
| R2 | 自动生成时把 serverID/toolName 写进 `Summary`，或 `Summary`/`Version` 随 schema 漂移 | 重现 §8.2 descriptor 冲突；更糟的是 schema 一变就硬报错、毒死 observation 循环 | ID 自带 ns 隔离；`Version` 用固定常量；schema 只进 provider binding digest（修订 2） |
| R3 | 为"能跑"放宽 `providerLifecycleEligible` | 不 Ready 的 provider 被选中，状态变更无收据 | pin 不豁免 Ready |
| R4 | 改 `DefaultExternalCapabilityEffectCeiling` | 任意 MCP 自动获得 `sensitive`/`local_mutation` | 常量不许改，测试钉住 |
| R5 | P0-2 放宽 pending 时顺手删掉 needs_review | 合规终态失效 | 只改 pending 分支，needs_review 分支不动 |
| R6 | P0-2 放行后**没有配套失败记账** | `Attempts` 永不增长，`needs_review` 永不升级 = 闸门被放宽成永久放行 | 远程 `ensureSession` 失败必须 `recordRemoteRuntimeSyncFailure`（仅 managed）；测试钉住升级路径 |
| R7 | P0-1a 按需启动不看 `mcpRuntimeSyncAllowsAutomaticStartForManager` | `needs_review`/backoff 的 server 仍被另一条路径拉起进程，终态被绕开 | fork 前同一判据；测试断言 needs_review 不被启动 |
| R8 | P0-3 把"从未观测"当"观测成功" | 不可用的 server 显示为可用 | TTL 只在"曾成功"时起效 |
| R9 | `bestProvider` 排序被改（unreviewed 压过 approved） | 装个差 MCP 永久抢位 | 排序不许动 + 测试 |
| R10 | 为省事给新装 server 默认 `AutoStart=true` | ① 应用启动拉起 N 个第三方进程；② bool+omitempty 无法区分"未设置"与"显式关闭"，更新路径复活用户关掉的 server | P0-1b 已撤销；可运行性由按需启动保证 |

---

## 6. 测试计划

### 6.1 已实施（P0，17 例新增，全部通过）

> 第二轮复审（§6.2）后计数更新：12 → 17 例。

`guiapp/mcp_runtime_sync_state_test.go` 追加 7 例闸门状态机：
- `TestMCPRuntimeSyncGateAllowsFirstCallWithoutDurableState` —— 无 durable 状态 = 首次探测窗口，放行
- `TestMCPRuntimeSyncGateAllowsPendingWithRemainingBudget` —— pending 且有预算，放行（**本次放宽的行为核心**）
- `TestMCPRuntimeSyncGateBlocksNeedsReview` —— 终态仍拦，且 reason 点名 review
- `TestMCPRuntimeSyncGateBlocksExhaustedPending` —— 预算耗尽仍拦
- `TestMCPRuntimeSyncGateBlocksRetryBackoff` —— 退避窗口仍拦（防放宽把 cooldown 一起拆掉）
- `TestMCPRuntimeSyncGateBlocksWhenPersistenceFailed` —— 持久化写失败仍拦
- `TestMCPRuntimeSyncGateAllowsReady`

`guiapp/local_mcp_manager_test.go` 追加 5 例 + `newOnDemandManager` 夹具（复用既有 `newHelperLocalMCPServerEntry`）：
- `TestLocalMCPManagerCallToolStartsSharedServerOnDemand` —— 从未 sync 过，调用即启动；二次调用复用同一进程
- `TestLocalMCPManagerCallToolRestartsCrashedServer` —— 进程崩溃后句柄仍在 map 里，调用必须换上新进程（**实施中新发现的第二条"装完不可用"路径**）
- `TestLocalMCPManagerCallToolRefusesDisabledServerOnDemand` —— disabled 在新路径上仍权威
- `TestLocalMCPManagerCallToolAdmitsManagedPendingServer` —— managed + pending 有预算 → 可调用，且成功后落 `ready`
- `TestLocalMCPManagerCallToolRefusesManagedNeedsReview` —— 终态既拦调用也**不启动进程**（守住修订 4 的守卫）

> 夹具注意：`markMCPRuntimeSyncReady` 会经 `mcpRuntimeSyncRequired` 读 `a.mcpRegistry`，生产环境总是挂着的；夹具必须同样挂上，否则就绪永不落账。

#### 终态验证（第四轮复审后）

- `go build ./...` 全仓 ✅　`go vet ./guiapp/ ./corelib/agentservice/` ✅　`node scripts/check-agent-architecture.mjs` ✅ passed
- 累计新增 **18 例测试**：第一轮 12（guiapp）+ 第二轮 3（guiapp 2 / corelib 1）+ 第三轮 2（corelib）+ 第四轮 1（corelib）
- D1/D2 的 `LastError` 断言、D5/D6/D7 三例**均做过反向验证**：撤销对应修复，测试必须变红（实测均变红）
- 改动面：源文件 6 个 +359 行，测试文件 4 个 +699 行
- `go test ./guiapp/ -run '<10 组 MCP 模式>'` → **ok, 57.9s**（含 12 例第一轮 + 3 例第二轮 + 既有护栏）
- `go test ./corelib/agentservice/ -run 'TestMCPToolBridge|TestMCPReadiness|TestMCPEndpoint|TestMCPSpawn'` → **ok, 7.6s**（含第二轮 1 例 + 第三轮 2 例）
- `gofmt -l` 仅报 `guiapp/app_nl_mcp.go`：差异在第 23-58 行（结构体 tag 对齐），本次改动在 811-906 行，**不重叠，属仓库既有漂移**，未顺手格式化。

> 注意：上述范围为避开 3 条既有失败（见上表）所收敛的范围，并非全包套件。
> 首轮 19/19 与第二轮均在此范围内全绿。

### 6.2 复审修订（第二轮 review/fix/optimize）

对已实施的 P0 代码做复审，发现 4 个真实缺陷，主题一致：**闸门放行了 pending，但失败记账与就绪收敛各自只覆盖了第一条路径**。

| # | 缺陷 | 位置 | 后果 | 修法 |
|---|---|---|---|---|
| D1 | 远程路径只给**握手失败**记账；握手成功后 `doMCPRoundTrip` 的失败只调 `recordFailure`（健康计数，不进状态机） | `guiapp/app_nl_mcp.go` `CallToolForOwner` | 会话已缓存的 server 一旦后端死亡，每次调用都被放行、`Attempts` 永不增长、`needs_review` 永不升级 → **放宽后的闸门被永久绕开** | 往返失败同样调 `recordRemoteRuntimeSyncFailure`（仅 managed） |
| D2 | 远程**成功**调用从不落 `ready` | 同上 | 只被调用、从不被 health check 的 managed server 永远停在 pending，被 `im_tools_misc.go:71` 等清单闸门持续隐藏 = 另一种"装完不可用" | 成功后按需 `markMCPRuntimeSyncReady` |
| D3 | `startSharedClient` 的 spawn 闸门只在**锁外**查一次 | `guiapp/local_mcp_manager.go` | 等锁期间后台探测可能已把状态翻成 `needs_review`，排队中的启动照样 fork 进程（TOCTOU） | 锁内复检；并给 `clientForOwner` 补同款防线（此前它只靠调用方那道闸） |
| D4 | `corelib/agentservice` 里**同一缺陷的第二份拷贝** | `corelib/agentservice/mcp_integration.go` `CallTool` | MaClawSrv（`build_win.bat` 双架构真实构建目标）走这条 transport，装完同样回 `local MCP server %q is not running` | 复用既有 `rt.startLocal`（幂等）按需启动；失败即 `stopLocal` 清理半初始化进程 |

| D5 | corelib 按需启动**绕过 `validateLocalMCPSpawnTarget`** | `corelib/agentservice/mcp_integration.go` `CallTool` | 该闸门原只在 `StartMCPServer` 一处（`startLocal` 自身也无防线），我新开的调用点直接进 `startLocal`，可 spawn 位于 `os.TempDir` 等不可信目录的二进制（2026-09-08 review 的 `/tmp/evil` 同源缺口） | 调用点重复该闸门 |
| D6 | 子进程**绑到请求级 ctx** | 同上 | `startLocal` 把 ctx 交给 `exec.CommandContext`，请求返回/取消即树杀刚起来的长驻进程。guiapp 侧用 manager 级 `m.ctx` 规避（`StopAll` 中 `cancel()`），corelib 侧我原先直接传了请求 ctx | 改用 `context.WithoutCancel(ctx)`——保留 request 派生值（tracing），只脱取消；与 hubcenter `ha_wrappers.go:289` 惯例一致 |

配套新增 2 例测试，且**两例均做过反向验证**（撤销修复测试必变红）：
- `TestMCPToolBridge_CallTool_RejectsUntrustedSpawnTarget`（D5）—— 命令指向 `os.TempDir()`，必须被拦且不落 client 到 runtime；撤销闸门后报 `executable file not found` 而非 `rejected`
- `TestMCPToolBridge_CallTool_SurvivesCanceledCallerContext`（D6）—— 传已取消的 ctx，错误必须**不含** `canceled`；撤销 `WithoutCancel` 后实得 `start local MCP server: context canceled`
  - ⚠️ 夹具教训：命令必须能在 PATH 上解析（用 `go` + 假子命令）。用 `nonexistent-command-for-test` 会先在 spawn 闸门失败，**根本走不到 `CommandContext`**，测试会假通过。

| D7 | **进程无回收**（我上一轮的注释是臆断，现已坐实） | `corelib/agentservice` 全局 | 上一轮注释称"由 service shutdown 负责"，但 `Service.Close` grep `mcp\|MCP\|Shutdown` **零命中**、`MCPReadinessManager` 无 Close、`globalMCPRuntimes` 只有 Load/LoadOrStore。**我的改动还扩大了发生面**：从「仅 AutoStart 的 server」变成「任何被调用的 server」，且脱离 ctx 后请求结束也不死 | 新增 `mcpServiceRuntime.stopAllLocal` / `userMCPRuntime.stopAllLocal`，接入 `Service.Close`（在关闭 executor 之前）；同时修正那句不实注释 |

**D7 的性质要说清楚**：进程无回收**不是我引入的**——`EnsureReady` 起的 AutoStart 进程从一开始就没人回收。但我的按需启动让"任何被调用过的 server 都会留下进程"，把它从小概率变成了常态，所以必须在本轮补掉。

配套新增 1 例测试：`TestServiceCloseStopsDetachedLocalMCPProcesses` —— 断言 Close 后 `runtime.local` 清空且 client 停止。
- ⚠️ 该测试**必须先保证 Close 前 runtime 非空**，否则"从未启动任何东西"也会 PASS（假通过）。已在 Close 前显式断言并按需 seed。
- 反向验证通过：撤销 `stopAllLocal` 调用后实得 `runtime.local after Close() has 1 entries; want 0`。

**顺带一处优化**：改用 Load-only 的 `mcpRuntimeForService(s)` 而非 `runtimeForService(s)`——后者是 `LoadOrStore`，会给每个从未碰过 MCP 的 Service 都留一个永不删除的条目（该 map 无任何 Delete 调用）。

#### 一个刻意不做的事

corelib 的 `startLocal` 无 guiapp 那样的 `startMaxRetries` 退避重试，也无 owner 隔离。这是两侧设计差异，不是缺陷：corelib runtime 是 per-user 共享、由 readiness manager 统一 reconcile。本轮不引入重试——在共享 runtime 上加重试会放大 fork 风暴风险，留待 P2。

#### 方法论：验证自己写下的注释

本轮唯一的新缺陷（D7）源于**上一轮注释里一句未经核实的断言**。当时我写下"由 runtime.stopLocal / service shutdown 负责"，却从未打开 `Service.Close` 确认它是否真的触及 MCP——它没有。

**凡是注释里写明"由 X 负责释放/回收/关闭"的，必须在写之前就把 X 的代码路径打开看一遍。** 注释中的所有权声明不是文档，是一份尚未履行的契约。

> 才捎带发现上面那处 `LoadOrStore` 泄漏优化。

- `TestMCPRegistryCallToolManagedPendingMarksReadyOnSuccess`（D2）—— pending 的 managed server 调用成功 → 落 `ready` 且 Attempts 归零
- `TestMCPRegistryCallToolManagedCountsRoundTripFailure`（D1）—— 握手成功但 `tools/call` 返 500 → Attempts 递增；`LastError` 必须是 `runtime HTTP request failed` 类（**握手失败会归类成 `runtime initialize failed`，靠这个分类差异区分两条记账路径**，否则测试会被既有的握手 recorder 蒙混过关）；打满预算后升级 `needs_review`，随即被闸门以 review 理由拦下
  - ⚠️ 坑：`summarizeMCPRuntimeSyncError`（`mcp_runtime_sync_state.go:435`）把原始错误归并成**分类字符串**再入库，所以断言不能写 `LastError` 含 `"MCP HTTP 500"`——原文早已被丢掉。改用 `"runtime HTTP request failed"` 反而**更能**区分两条记账路径（握手失败会归类成 `runtime initialize failed`）。写状态/摘要类断言前先看 `summarize*` 的归类分支。
  - ⚠️ 坑 2：**别假设"连打两次调用就能累加 Attempts"**。第一次失败后记录的 `NextRetryAt`（Attempts×1 分钟）会让第二次调用被退避拦下，**根本到不了端点**。这正是修正后行为正确的证据（修正前此处什么都不记录、无冷却、永不升级）。要耗尽预算得先把 `NextRetryAt` 改写成过去时间再调用。
- `TestMCPRuntimeSyncShouldRecordReady`（D2 的写抑制规则）—— 判据必须 entry 驱动（热路径不读 config）、非 managed 不得建 marker、已 `ready` 不得重复写盘
- `TestMCPToolBridge_CallTool_StartsLocalServerOnDemand`（D4）—— `AutoStart:false` 仍能按需启动（证明可运行性不依赖该开关），且启动失败不在 runtime 里留半初始化 client

#### 一个刻意的"不修"

调用前 `IsRunning()` 与 `client.CallTool` 之间存在窗口：进程可能在这两步之间死掉。**不做"失败即重试一次"**——请求可能已经送达，重试会让有副作用的 MCP 工具执行两次。现状是本次调用报错、下一次调用换上新进程，可恢复且安全。

#### 测试归属判定（本轮踩到两个坑）

**坑 1：不要用 `git stash` 做基线对照。** 本环境有会话并行编辑本仓，stash 期间 `corelib/agent/prompt_blocks.go` 被人改到语法错误（`Unexpected name string`），且**改动在 stash 里有丢失风险**。要用就 `cp` 到临时目录备份再 `cp` 回来。

**坑 2：HEAD 版替换法也会失效。** 把源文件还原成 HEAD 版后，留在树上的**新增测试文件**引用了新符号，必然 build failed。别把它误读成"基线也坏"。

**可靠的判定顺序**（本轮据此定案）：
1. `grep` 确认失败点与改动的代码路径是否有交集——本轮两个失败走 `register → warmServerTools → HealthCheck`，不经过我改的 `CallToolForOwner`；
2. `git diff --stat <被测文件及其依赖>` 确认未改动、也无他人并行改动；
3. 隔离单跑 + 重复运行，确认稳定复现而非偶发；
4. 读 fixture 与被测代码语义是否自洽。

**本轮据此判定为既有三条（与本次改动无关，勿去修）**：

| 测试 | 失败形态 | 定案依据 |
|---|---|---|
| `TestDiscoverToolFindsKnowledgeDirectoryAndURLWritersForChineseNeed` | 中文语义未匹配到 `knowledge_import_directory` | 走 `tool_discover.go` 中文发现链路，与 MCP 运行时零交集；4 个相关文件均与 HEAD 一致 |
| `TestRegisterMCPServerImmediatelySyncsTools` | `TempDir RemoveAll: .maclaw\data directory is not empty` | **断言本身全部通过**，仅 cleanup 失败——Windows 上句柄未释放的典型症状；我的 diff 无任何 `MkdirAll`/`WriteFile` |
| `TestMCPRegistryWarmServerToolsTimesOutWithoutBlocking` | `tools/list response is missing tools` | fixture 返回 `{"result":{}}` 空 result，而 `HealthCheck` 要求必带 `tools` —— **fixture 与被测语义不自洽**，且该health 代码在 HEAD 上即如此（`7cf6e307` 之后） |

> 补充：两侧全量运行时请在确认其他人并行编辑停歇后进行。本轮先后撞上 3 次他人的中间态
> （`corelib/docgen` `ValidateContent` 未定义、`corelib/swarm` `"unicode" imported and not used`、
> `corelib/agent/prompt_blocks.go` 语法错误），**全部自愈**——判归属一律先 `git status --porcelain <路径>`。

### 6.4 待实施（P1/P2）

- `corelib/agentservice/dynamic_capability_publisher_autoidentity_test.go`
  - 任意工具（无评审名、无 `x_capabilities`）也获得 `x:mcp:<serverID>:<tool>`
  - `readOnlyHint=true` → `read_only`；否则 → `external_effect`
  - `Effects` 超出 ceiling 时被拒（**反向验证：撤销 `EffectsWithinCeiling` 调用，测试必须变红**）
  - **schema 漂移不改变 descriptor**（`Version` 固定常量），只改 provider binding digest —— 修订 2 的回归护栏
  - 两 server 同名工具互不冲突（ns 隔离）
  - unreviewed capability 不抢 approved 的位（`bestProvider` 排序）
- `corelib/agentservice/dynamic_semantic_pin_invoke_test.go`
  - 用户点名 → plan 命中被 pin 的 MCP provider
  - provider 不 Ready → pin 无效（不静默替换）
  - 无用户点名 → 不产生 need（模型侧不得自行解锁）
- `corelib/agentservice/dynamic_semantic_unreviewed_confirm_test.go`
  - 未评审 namespace 的 `external_effect` selection 带 `ConfirmationID`
- `guiapp/reviewed_dynamic_publication_test.go` 增补（P0-3）
  - 从未观测 → `mcpOpenLookupReady == false`
  - 观测成功且未过 TTL → true；过 TTL → 后台刷新

既有护栏（不许变红）：`dynamic_capability_publisher_regression_test.go`、`dynamic_semantic_routing_test.go:93/151`、`mcp_integration_test.go:124/333`、`guiapp/app_nl_mcp_test.go:256/262/308`、`guiapp/mcp_runtime_sync_state_test.go:102/155`、`guiapp/local_mcp_manager_test.go:16/77/105`、`scripts/check-agent-architecture.mjs`。

---

## 7. 验收标准

1. 装一个没有任何评审名、没有 `x_capabilities` 的 MCP（stdio + remote 各一），UI 里立刻可见且状态明确（不再静默）
2. 装完在 UI 点「试调用」能成功返回（L0）
3. 用户点名该 MCP 的某个工具后，模型本轮能调用到它（L1）
4. 不点名时，模型**不会**获得该工具的调用能力
5. 未评审 MCP 的写类工具被调用时，走 receipt 账本 + 确认（P2 后）
6. 已评审能力仍优先于未评审（`bestProvider` 排序不变）
7. 重启应用后，新装 stdio server 自动拉起（P0-1）
8. `needs_review` 仍拦调用，且 UI 有恢复入口

---

## 8. 待拍板（三个决策点）

1. **P1-1 的能力 ID 形态**：`x:mcp:<serverID>:<tool>`（serverID 隔离，简单但 serverID 变了身份就变）vs marketplace `global_key` 隔离（稳定但要求市场来源）。建议前者 + 迁移映射。
2. **P2 的确认强度**：未评审 MCP 的写操作是「每次确认」还是「每 server 每会话确认一次」。建议前者，代价是吵。
3. **P3 是否做受控发现网关**：不做则「任意 MCP 装完即可运行」止于 L1（需用户点名）；做了才可能在无点名时自动命中，但形状上接近被封禁的 `discover_tool`。**建议本轮不做**，先把 L0/L1 做扎实。

---

## 附：与本方案直接冲突的既有设计（改动前必读）

- `docs/design/tool-routing-extensibility-plan-zh.md` §2.1（静默不可见）、§2.2（按名授权=提权面）、§2.3（单标量选优）、§2.4（预算恒定）、§2.5（kind 粒度 fail-closed）、§8.1/§8.2/§8.5（registry 实例、Summary 身份、Effects 身份——三个已修 bug）
- `docs/design/semantic-tool-routing-design-zh.md:2123/2311` —— `call_mcp_tool`/`manage_skill` 现存的都是未迁移路径，删除会功能回归，故"按族解锁、目前无族可解"
