---
title: 工具路由全托管迁移计划
doc_id: tool-routing-full-migration-plan-zh
status: 草稿
created: 2026-09-26
depends_on:
  - docs/design/tool-routing-explained-current-zh.md
---

# 工具路由：从双轨并存到唯一语义路径

本计划是 `tool-routing-explained-current-zh.md` §3.8 的执行版本。**所有结论均已回源码核实并标注行号**；未核实项显式标记。

---

## 0. 结论先行

**能做到，且门槛比原先估计的低。** 但代价不在代码量，而在两处：

| 维度 | 结论 |
|---|---|
| missing 可观测/可恢复 | ✅ **已建成**，不是前置工作（见 §1，推翻 §3.8 原判断） |
| 规则表覆盖 | ✅ **已闭合**：`workflow_task` 是**owner 登记的永久豁免**，不是缺口（见 §1.2，本轮最终确认） |
| 宿主接线 | ⚠️ 仅剩 TUI 一家未接（7 个回调站点） |
| 魔法时刻 | 迁移靠**下调 CI 配额**驱动，不靠一次性删除 |

---

## 1. 现状盘点（本轮核实）

### 1.1 missing 机制已经完整 ✅

追查 `plan.Unmet` / `plan.Omitted` 的全部消费点，五道保障均在位：

| 保障 | 位置 | 行为 |
|---|---|---|
| 结构化分类 | `corelib/tool/semantic_planner.go:588-594` | `record()` 按 `need.Required \|\| unmetIsAuthoringFault(code)`（`:381-387`）分流 Unmet / Omitted |
| fail-closed 回退 | `corelib/agentservice/dynamic_semantic_routing.go:1389`、`:1558` | `if err != nil \|\| len(plan.Unmet) > 0 { return true }` |
| 受控请愿恢复 | `corelib/agentservice/semantic_petition.go:81` | `ValidatePetitionExpansion`：child 有 Unmet 直接拒绝；要求严格超集（父 selection 原样存活、新增只来自该 label 的规则模板） |
| HTTP 边界签名 | `corelib/agent/tool_surface_receipt.go:1067-1096` | `Omitted` 进面收据并规范化去重，与 digest 同受保护 |
| 评测与快照 | `corelib/tool/routingeval/runner.go:477-482`、`corelib/agentservice/semantic_behavior_snapshot.go:110-115` | Unmet 是一等断言对象；Unmet/Omitted 落行为快照 |

> **因此 §3.8 里"missing 要先建起来"的判断作废。** 真正的门槛只有下面两项。

### 1.2 规则表覆盖：只剩一个，但性质特殊

`intent_capability_rules.go:182-184` 显示 `coding` / `bug_fix` / `maintenance` 已迁移。**`workflow_task` 无规则，且这是 owner 登记的永久豁免，不是欠账：**

**⭐ 最终确认（2026-09-26 第 3 轮）**：豁免不是口头决定，是一套**四件套**，全部落在代码里——

1. **inventory 测试登记**：`guiapp/semantic_intent_rule_inventory_test.go:22-25` 的 `unmigratedSemanticIntentLabels` 写明理由——*"multi-turn workflow loop owns the route; a single-turn capability plan is the wrong unit for it"*。测试还会反向守门：谁给 workflow_task 加了规则，`TestSemanticIntentRuleCoverageInventory` 直接报错。
2. **专属用户文案**：普通聊天回合被分类为 workflow_task 时，`semantic_tool_routing.go:3000-3006` 返回 *"这类多阶段任务要从工作流发起……用 /workflow 或界面上的工作流面板开始"*（`semantic_workflow_entry_required`）——**产品语义是拒绝并指引，不是给一张工具面**。
3. **阶段回合标签剥离**：`semantic_workflow_phase_route.go:30-57` —— 工作流回合里的 workflow_task 是分类器在复述本轮已走的路由，主动剥离；纯 workflow_task 阶段 fallthrough 到 legacy pipeline（与 document_generate 阶段同路）。
4. **独立回路**：阶段执行走 `WorkflowAgentLoop`（`workflow_v2_integration.go:823`），工具面由 phase `ToolPolicy` 在 name 层 + `semanticCapabilityPolicyConstraints` 在能力层管理（见 P2 已实施部分）。

**给规则表加宽泛 Need 的方案被正式否定**（原 P2 想法）：它会把"拒绝并指引工作流入口"的产品语义变成"给一张搜索+文档面"；`TestSemanticIntentRuleCoverageInventory` 也会 fail（`mapped && declared → error`）。多轮工作流的正确单元是工作流回路本身，不是单轮 capability plan。

### 1.3 宿主接线：只剩 TUI

| 宿主 | 状态 | 证据 |
|---|---|---|
| GUI IM 托管回合 | ✅ | `semanticManaged` 闸门 |
| CoreAgent | ✅ | 静态 specs + 受管语义面 |
| Coding 子代理 | ✅ | 自有 catalog + 角色 allowlist |
| maclaw-cli / ACP | ✅ 继承 | 在 GUI IM 下游 |
| **TUI 全族（7 站）** | ❌ | `tui/needle_logging.go:67` → `app.toolRegistry.BuildDefinitions()`，静态全量，无选择 |

### 1.4 ⭐ 现成的迁移引擎：CI 配额

`corelib/tool/routingarch/routingarch_test.go:87` `TestLegacySurfaceDoesNotGrow` 钉了四条天花板：

| 遗留原因 | 当前 | 天花板 | 余量 |
|---|---|---|---|
| `ReasonLegacyNameRouter` | 11 | 13 | 2 |
| `ReasonLegacyPolicyFilter` | **28** | 28 | **顶格** |
| `ReasonProviderNameCallLegacy` | 9 | 10 | 1 |
| `ReasonInstalledDefinitionStep` | **4** | 4 | **顶格** |

**这就是迁移的进度条。** 天花板是"只许降不许升"的单向阀，每迁走一处就把对应数字下调一格，CI 自动禁止回潮——把"应该做什么"变成"只能做什么"。

历史触顶记录也证明它有效（测试注释）：

> "maxLegacyPolicyFilter read 25 until the 2026-09-09 `gui/` → `guiapp/` monolith move."

---

## 2. 阶段计划

### P0 — 认知校准 ✅ 已完成（本会话）

| 项 | 内容 |
|---|---|
| 更正 1 | §3.8 "missing 要先建" → 已建成，附五条消费点出处 |
| 更正 2 | §3.7 workflow_task 的缺口性质 → 结构性（V2 工作流），非遗漏 |
| 澄清 | `isV2WorkflowLoop`（`im_system_prompt.go:128`）**不是第三套路由**，只决定是否追加 prompt 片段 |
| 代码 | 补 `TestValidatePetitionExpansionRejectsUnmetChild`（见 P1.0） |

### P1 — 补齐 missing 地基的最后一块测试保护

**P1.0 ✅ 已完成**：`semantic_petition_test.go` 新增 `TestValidatePetitionExpansionRejectsUnmetChild`

- 覆盖此前三条测试均未触碰的 `child.Unmet > 0` 拒绝分支（`semantic_petition.go:81`）
- 同时钉住反向语义：`Omitted`（策略性丢弃）**不应**触发 Unmet 门禁
- 验收：`go test ./corelib/agentservice/ -run TestValidatePetitionExpansion` → **4/4 PASS 已验证**

### P2 — workflow_task 的能力发布（需产品决策）

**目标**：让 V2 工作流运行时把当前阶段的需求发布成 `CapabilityNeed`，而非静态表映射。

#### ⭐ 关键修正：工作量被我上一轮严重高估

原以为要"给 20 种 WorkflowType 各写一套规则"。实际结构是**收敛的**：

```go
// corelib/workflow/v2/state.go:51-57（注释 2026-09-26 已更正）
type ToolPolicy string
const (
    ToolPolicyNone          ToolPolicy = "none"           // sentinel: no workflow decision available
    ToolPolicyDocOnly       ToolPolicy = "doc_only"       // read/search/memory only
    ToolPolicyPlanning      ToolPolicy = "planning"       // repository inspection
    ToolPolicyFull          ToolPolicy = "full"           // all tools (execution phase)
    ToolPolicyOpsControlled ToolPolicy = "ops_controlled" // controlled operational tools
)
```

`intent_definitions.go:521-525` 的 20 种 WorkflowType（`product_design` / `business_plan` / `bid_response` / `grant_proposal` …）**共享这同一套 5 档**。而且 `Phase` 结构已经带了 `ToolPolicy` 字段（`state.go:64`）。

它也是工具面的**实际载体**，不是装饰：`agentservice/shared_tool_surface.go:337`（`ToolPolicy: c.toolPolicy`）、`core_agent_executor.go:662`、`:2355`（判 `ops_controlled`）、`types.go:1082-1085`（注释明写 "constrains tool exposure and execution"）。另见 `v2/types.go:83` 的类型别名 `ToolFilterPolicy = ToolPolicy`。

⇒ **A 的真实工作量 = 5 条枚举→CapabilityNeed 的映射表。**

#### ⭐ 更重要的：这笔账其实是反的

`ToolPolicyFull = "full" // all tools (execution phase)`——**工作流的执行阶段当前拿的是全量工具面**：无 invocation grant、无面收据、工具名明文。20 种 type × 多个 phase 都在上面跑。

所以 **B 不是"省下 A 的工作量"，B 是保留这个洞。** 就算按 C-2 删完 legacy，workflow 执行阶段仍在全量面上，全托管收益被腰斩。

#### 方案对比

| | A（动态 Need 发布） | B（登记豁免） |
|---|---|---|
| 改动量 | 5 条映射 + 面适配 | routingarch 一行豁免 |
| `full = all tools` 的洞 | **堵上** | 保留 |
| workflow 面是否进 grant/收据体系 | 是 | 否 |
| 能否下调 `ReasonLegacyNameRouter` 配额 | 能 | 不能（永久占位） |
| 风险 | 触碰 V2 执行路径 | 几乎为零 |
| 回滚 | 映射是纯数据 | 删豁免即可 |

#### ⭐⭐ 决定性发现：A 的基础设施已经建成并通电

上一轮我标注"工具 policy 与 capability 引擎的交接方式未核实"。追完之后——**桥早就建好了**，`ToolPolicy → CapabilityNeed` 的映射表已经存在于生产代码里（`guiapp/semantic_capability_policy.go:136-160`）：

```go
return agentservice.StaticCapabilityPolicyAdapter{Rules: []agentservice.CapabilityPolicyRule{
    deny(string(v2.ToolPolicyDocOnly), tool.CapabilityBusinessDataMIS, tool.CapabilityKnowledgeIngestLocal,
        tool.CapabilityFSWriteLocal, tool.CapabilityAudioCaptureMicrophone, /* …共 17 项 */),
    deny(string(v2.ToolPolicyPlanning), all...),
    deny(string(v2.ToolPolicyOpsControlled), all...),
    deny(imSemanticPolicyStateBlocked, all...),
}}
```

完整链路（每一跳均已核实）：

| # | 环节 | 位置 |
|---|---|---|
| 1 | `Phase.ToolPolicy` → policy state 字符串 | `guiapp/semantic_capability_policy.go:171` `imSemanticWorkflowPolicyState` |
| 2 | state → 能力级路由约束 | `:61` `imSemanticCapabilityPolicyAdapter` + 上述 deny 规则 |
| 3 | 汇总成本轮约束 | `:153` `semanticCapabilityPolicyConstraints` |
| 4 | **注入 Planner** | `guiapp/semantic_tool_routing.go:2787` → `:2794` `RouteRequest{Constraints: policyConstraints}` |
| 5 | Planner 消费 | `corelib/tool/semantic_planner.go:605` `constraintDeniesNeed(req.Constraints, need, req.Now)` |

#### ⭐ 这也解决了"同一 policy 对象"的关注点

`codingagent.go:45-54` 说的"definition 过滤与执行期门用同一对象"，在这里用的是一个**更强**的不变式——注释原文（`semantic_capability_policy.go:188-190`）：

> "The execution-time legacy **workflow gate still applies unchanged** underneath, so a planning-context approximation can only **tighten a turn, never widen it**."

即：不要求两侧实现逐字节一致，只要求能力侧永远是 name 侧的子集。**这比"同一对象"更稳**，因为它不依赖两份实现同步演化。执行期保底仍在：`v2/types.go:213` `IsToolAllowedByPolicy`、`:242` `ValidateToolCallByPolicy`。

#### ⚠️ 已被第 3 轮复审推翻：宽泛 Need 不是正确做法

我在 §1.2 初版说过"给它一组宽泛的 Need，再由当前 phase 的 `ToolPolicy` 自动收紧，两层叠加 = 阶段相关的工具面"——**机制分析本身没错，但方案选错了对象**：

- **阶段回合根本不看规则表**。阶段执行走 `WorkflowAgentLoop` 独立回路（`workflow_v2_integration.go:823`），其分类里的 workflow_task 标签被 `semanticClassificationForWorkflowLoop`（`semantic_workflow_phase_route.go:30`）主动剥离——**规则表条目对阶段回合没有任何作用**。
- **普通聊天回合不该给面**。被分类为 workflow_task 的聊天回合，产品语义是 HostReject + 指引工作流入口（`semantic_tool_routing.go:3000`），不是服务一张工具面。
- **owner 已用 inventory 测试登记了豁免**（`semantic_intent_rule_inventory_test.go:22`），加规则会直接 fail CI。

⇒ **workflow_task 的"迁移"以豁免形态完成**：它不是单轮 capability plan 能服务的家族，它的路由由多轮工作流回路拥有。这一结论已写进 §1.2 的四件套清单。

#### ⭐⭐ 更正：所谓"待拍板的 ToolPolicyNone"是我的一次误判

上一轮的结论——"`ToolPolicyNone`（`"none"` = 无工具限制）与全托管理念冲突，只能废弃或定义明确上界"——**建立在错误前提上**。逐处核实后的事实：

**没有任何 phase 主动选择 `none`。** `templates.go` 的 153 个 Phase 字面量**全部**显式设置了 ToolPolicy（`grep` 计数：DocOnly 134 / Full 36 / Planning 1 / OpsControlled 1，未设置 **0**）。`ToolPolicyNone` **一次都没有出现在赋值位置**。

它出现的全部位置都是**函数返回值**，承担四种互不相同的语义：

| 身份 | 出处 | 真实语义 | 全托管后的行为 |
|---|---|---|---|
| ① 注释名义（"无限制档位"） | `state.go:52`（更正前 `:38` 的 `// no tool restrictions`） | **不存在**，无 phase 使用 | — |
| ② sentinel：不适用 / 拿不到 | `v2/engine.go:1290,1293,1297` | 无工作流 / 待审核中 / 模板匹配失败 | `apply=false` → 不施加约束 ✔ |
| ③ 默认值：metadata 未声明 | `service_messaging.go:791` | 两侧 metadata 都无 `tool_policy` | headless 链路，等价 full，不进能力层 |
| ④ 信号量：阶段执行被封 | `im_agent_loop_tools.go:781`（唯一 `apply=true` 分支） | 卡在执行阻断上 | `:133` → `Blocked` → **deny all（fail-closed）✔** |

⇒ **`ToolPolicyNone` 不需要废弃，也不需要 owner 拍板。** 四种身份里三种的行为在今天都是正确的；① 只是注释没跟上代码。**它是一个 sentinel，不是一个逃逸口。**

#### ⭐ 因此真正的口是 `ToolPolicyFull`，不是 `None`

真正的 `all tools` 来自 `ToolPolicyFull`（`"full" // all tools (execution phase)`）——它是 **36 个 phase 的主动选择**，而 `v2/types.go:236` 把 `None` 和 `Full` 一并原样放行：

```go
if policy == ToolPolicyNone || policy == ToolPolicyFull || len(tools) == 0 {
    return tools
}
```

**名字是会撒谎的：这一行里 `None` 是被 `Full` 顺带放行的陪衬，主角是 `Full`。**

#### ✅ P2 进度（2026-09-26 已实施两项）

**已落地：**

| # | 事项 | 位置 | 依据 |
|---|---|---|---|
| 1 | `full` phase 不再返回空约束，改为投影到新状态 `imSemanticPolicyStateExecution` | `guiapp/semantic_capability_policy.go:179` | 消除 `""` 的二义（"无工作流适用" vs "全部允许"） |
| 2 | 为该状态加宽上界规则：deny 三个**接管型**家族 | `semantic_capability_policy.go:158-159` | `deny(execution, ShellExecuteRemoteHost, BrowserControlWeb, ComputerControlDesktop)` |
| 3 | 更正 `state.go:37-47` 注释：`None` 是 sentinel 而非"无限制档位" | `corelib/workflow/v2/state.go:52` | 153 个 phase 无一使用 `None` |
| 4 | 新增测试 `TestIMSemanticPolicyStateExecutionCeiling` | `semantic_capability_families_test.go` | 4/4 PASS 已验证 |

**⭐ 上界的出处（不是我拍脑袋定的）**：`v2.RequiredToolNamesForPolicy(ToolPolicyFull)`（`v2/types.go:290`，`case ToolPolicyFull:` 在 `:292`）声明了 full 阶段期望的最小工具集——

```go
case ToolPolicyFull:
    return []string{"bash", "read_file", "list_directory", "write_file", "edit_file"}
```

**全是本地 shell 与本地文件系统，没有任何一项触及另一台主机、另一个会话或另一台设备。** 所以只有"接管型"三族（远程 SSH / 浏览器接管 / 桌面接管）被 deny，其余一律保留。`ops_controlled` 那条已经用 `bash/ssh` 覆盖了远程主机场景，full 阶段不需要它。

这也解释了为什么这次改动是**安全的**：`semantic_capability_policy.go:188-190` 保证能力侧只能收紧不能放寛，而收紧的方向这里是有最小集实证的，不会误伤工作流真正需要的能力。

> ⚠️ **复审补充（同日）**：`semantic_capability_policy.go` 的行号在本次插入新规则后整体下移约 15 行。此前本节引用的 `:108-126`（映射表）、`:133`（投影函数）、`:147-152`（收紧不变式）为**改动前**的行号；改动后分别对应 `:136-160`（deny 规则表）、`:171`（`imSemanticWorkflowPolicyState`）、`:188-190`（不变式注释）。**引用本文档行号时以本次修订为准。**

**剩余（未在本次范围内）：**

1. ~~给 `workflow_task` 加一组宽泛 Need~~ **已撤销**：owner 豁免四件套已落地（§1.2），加规则破坏"拒绝并指引"产品语义且 fail inventory 测试。**规则表覆盖维度就此闭合。**
2. 两项纯清理（**已实施**，2026-09-26）：
   - ✅ `toolPolicyFromMetadata`（`service_messaging.go:783`，默认 return `:791`）默认值 `None`→`Full`：等价性已复核——metadata 路径的三个消费点（`FilterToolDefinitions` `types.go:245`、`IsToolAllowedByPolicy` default 分支 `types.go:220`、database 写检查 `types.go:270`）对 None/Full 行为一致；GUI 侧所有 `== ToolFilterNone` 比较（blocked 语义，`im_tool_execution.go:603/:631`、`im_subagent_route.go:82` 等）全部读自 workflow engine 路径，不经 metadata；传输面 `AgentCapabilities.Metadata["tool_policy"]` 虽从 `none` 变 `full` 但全仓零消费方。"未声明"不再借用 sentinel。
   - ✅ `FilterToolDefinitions`（`types.go:235-243`）加注释：说明 None/Full 的 name 层放行语义及能力层取代计划，禁止新增依赖它做限制的消费者。
   - ✅ 新增测试 `TestToolPolicyFromMetadata`（`corelib/agentservice/service_messaging_test.go`）：钉住默认值 = Full、message 优先于 session、非法值逐级 fallthrough——此前该函数零测试覆盖。

### P3 — TUI 接入 SemanticSurfaceHost（最大一块，建议独立会话）

**目标**：`tui/needle_logging.go:67` 的静态全量面替换为受管语义面。

- **改动清单**：TUI 7 个回调站点、`SemanticSurfaceHost` 接入、catalog snapshot publisher
- **备选**：在 `routingarch` baseline 显式登记"TUI 保留静态全量面"为**永久例外**（避免为一个 seldom-used 路径付出整个 reactive rewriting 的成本）
- **风险**：高（TUI 是生产交互路径）
- **前置**：P2 完成，TUI 测试套件全绿

#### ⭐ 侦察补充（2026-09-26 第 3 轮）：两套工具目录是平行的，接线 = 目录重建

`BuildDefinitions()` 的消费点共 4 处：`needle_logging.go:67`（遥测记录）、`app.go:3595`、`app.go:4227`、`loop_command.go:319`；执行点 `ExecuteCtx` 3 处（`app.go:3634`、`app.go:4092`、`loop_command.go:335`）。

关键事实：**TUI 的 `agent.CoreToolRegistry`（`app.go:232`，`RegisterCoreTools` 注册）与 IM 能力目录的 provider（约 40 个 `corelib/agentservice/dynamic_host_*.go` 宿主服务，reviewed adapter 名如 `host_document_generate_file`）是两套平行体系**。capability 本体（`corelib/tool/capability_ontology.go`）是共享的，但 provider 适配器按宿主各发各的——IM 的 dynamic_host 服务绑定 GUI 宿主依赖，TUI 无法直接复用。

⇒ 接线的真实成本不是"把 `BuildDefinitions()` 换成 plan 调用"，而是：

1. **为 TUI 发布自己的 provider 集**：把 CoreToolRegistry 的每个工具映射到共享本体的 capability（复用本体，新建 TUI 侧 provider 绑定）
2. **per-turn planning**：7 个回调站点改为 plan → render → grant
3. **执行侧围栏**：3 处 `ExecuteCtx` 加渲染名围栏校验（对齐 `loop.go:3014` 的语义）
4. **catalog snapshot publisher**：TUI 宿主的目录快照发布

这是**一整层建设**，不是接线。对比豁免成本（workflow_task 模式：inventory 登记 + 理由），"TUI 永久例外"的性价比取决于 TUI 的实际使用占比——**这个数据当前没有**（needledata 有 `EventToolRouting` 遥测，`needle_logging.go:67` 就在记录 TUI 每轮的 available tools，可以从遥测数据估算 TUI 流量占比后再决策）。

**建议**：先跑遥测数据估算 TUI 流量占比；占比可观 → 立项做目录重建；占比可忽略 → 按 workflow_task 模式登记永久例外。

#### ⭐ 切片 1 已落地（2026-09-26 第 4 轮）：`tui/semantic_static_catalog.go`

用户拍板继续接线后，按 coding-static S1-A 模式完成了 TUI 侧目录地基（**零运行时行为变化**，当前唯一消费者是测试）：

| 件 | 内容 |
|---|---|
| `tuiSemanticCapabilityRegistry()` | builtin 本体注册 + Seal（镜像 `guiapp/semantic_tool_routing.go:401` 的用法） |
| `tuiStaticReadOnlyCapabilityNeeds()` | 2 条评审 needs：`fs.read.local` / `information.fetch.web`，evidence 只允许静态目录声明 |
| `tuiStaticPostureConstraints()` | 显式 deny 六个变异/接管家族（FSWrite / ShellLocal / BuildVerify / ShellRemote / Browser / Desktop） |
| `tuiSemanticReadOnlyProviders()` | `read_file → fs.read.local`、`web_fetch → information.fetch.web`，adapter 名 = 注册表工具名（分发直通 legacy `ExecuteCtx` 链） |
| `tuiStaticCatalogSnapshot()` | `PublishWithCoverage(Complete)` — 静态清单的 Complete 是合法的（provider 全集构建期已知） |

**实施中核实的关键事实**：

1. `BuildDefinitions()` 消费点实测 **8 处**（上表 4 处 + `pipe_mode.go:338`、`rpc_mode.go:404`、`weixin_gateway.go:459`、`app.go:4227` btw 面）；主插槽是 **`tuiCallbacks.BuildTools`（`app.go:3594`）**——它已有 readonly-child 过滤与 light-profile 过滤，语义面就是第三层过滤。
2. **TUI 零 UIC 通路**（`corelib/intent` 无 import）——切片 2 必须先接 `ClassifyContext`。
3. `information.search.web` 不在 builtin 本体（只有 `information.fetch.web`），SearchWeb 能力归 IM/agentservice 域——TUI 目录不认领。
4. **注册表 schema 类型陷阱**：`CoreToolRegistry` 的 `Properties` 值是 `map[string]string`，进程内无 JSON 往返，直接喂 `NewParameterAuthorization` 会 `parameter_schema_invalid`（authorizer 断言 `map[string]interface{}`）。已用一次 JSON marshal/unmarshal 规范化解决（schema 本就是 JSON，规范化同时让 digest 稳定）。
5. **一个 capability 一个 provider**（切片 1 纪律）：`list_directory`/`ripgrep` 同属 fs.read.local 但未投影——多 provider 的 planner 交互是切片 2 的决策，不是数据追加。

**切片路线**：切片 2 = UIC 接线 + `IntentRuleCoverageFromClassification` 需求推导 + `ToolPlanner.Plan` + kill switch（默认 OFF）进 `BuildTools` 第三层过滤；切片 3 = `MaterializeReadySurface` grant/收据 + 8 处消费点收口 + 3 处 `ExecuteCtx` 围栏。遥测估算仍建议在切片 2 前完成。

### P4 — 逐步删 legacy（配额驱动）

**顺序原则：先降配额，再删代码。**

1. 每完成一处迁移 → 把 `TestLegacySurfaceDoesNotGrow` 对应天花板**下调一格** → CI 封住回潮
2. 天花板降到 0 后，删除 `Router.RouteWithOptions` 及调用点
3. 清理 `routingarch/baseline.go` 的 101 条点位

- **验收**：四项配额归零 → baseline 清空 → `RouteWithOptions` 无引用
- **风险**：不可逆
- **回滚**：依赖 git（建议在独立分支逐步推进，每步一个 commit）

---

## 3. 门禁清单

| 门禁 | 位置 | 作用 |
|---|---|---|
| 增长上限 | `routingarch_test.go:87` `TestLegacySurfaceDoesNotGrow` | 四项配额只降不升 |
| 新增点位 | `routingarch_test.go:40` | 新点位未在 baseline → fail |
| 陈旧点位 | `routingarch_test.go:58` | baseline 有但扫不到 → 必须删 |
| 检测器失明 | `routingarch_test.go:163`、`:195` | 守卫检测器本身不失明 |
| 请愿严格超集 | `semantic_petition_test.go` (4 例) | 防止请愿成为权限提升通道 |

---

## 4. 本次会话交付

- 更正文档 §3.7、§3.8 两处判断错误 + 一处过度推测
- 新增本计划文档
- 新增测试 `TestValidatePetitionExpansionRejectsUnmetChild`（4/4 PASS）

**未执行**：P2 / P3 / P4。三者均需 owner 决策或独立会话，不在一次改动中完成。
