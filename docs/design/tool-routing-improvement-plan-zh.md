# 工具路由架构对比与改进计划

- 状态：草案 v4（已过四轮评审：事实核对、对抗性设计评审、引用准确性 + 内部一致性评审、上位设计对齐 + 量化事实核验，意见已全部吸收，见第 9 节变更记录）
- 范围：corelib/agent、corelib/tool、corelib/agentservice、corelib/intent、guiapp、tui
- 参考对象：xai-org/grok-build（开源提交 a28ee2b，2026-09-17）
- 上位设计：`docs/design/semantic-tool-routing-design-zh.md`（本文是其对比补全 + 分阶段落地计划，不替代该设计）
- 强制门禁：`docs/design/managed-surface-migration-checklist-zh.md`（2026-09-18 ssh 事故链后设立的迁移闸门）——**凡模型可见面变更（含改名、可见集变化）一律逐能力过闸，不设"纯重构豁免"**（v1 的错误已修正，见第 9 节）

## 1. 背景

对 xAI 开源的 Grok Build（SpaceXAI 的终端 AI 编码 agent，Rust）做了源码级分析，重点是其工具路由策略，并与本程序现有实现逐层对比。Grok Build 的核心哲学是**纯模型驱动路由**：模型按名字选工具，系统只提供确定性门控；本程序走的是**语义路由**路线（上位设计见 semantic-tool-routing-design-zh.md）：系统先用检索/分类/能力规划决定"给模型看哪些工具"。

两条路线各有优势。本程序在语义规划、授权令牌、面完整性校验、写集冲突检测上领先；但在架构收口、权限策略统一、命名空间、自愈引导上存在短板。本文记录对比结论并给出分阶段改进计划。

**适用范围声明**：grok-build 是单一厂商的第一方工具注册表，其不少机制（明文 `server__tool` 命名、全量工具面、集中式规则求值）建立在"工具来源可信"的前提下。本程序接了任意的第三方 MCP server，工具名是**服务端控制的不可信字符串**，因此第 5、6 节中凡涉及借鉴 grok-build 的地方都做了适配或明确拒绝借鉴，并给出理由。

## 2. Grok Build 路由机制摘要

### 2.1 分层结构

| 层 | 位置 | 职责 |
|---|---|---|
| `FinalizedToolset` | `xai-grok-tools/src/registry/types.rs` | 会话级注册表，映射模型可见名 → 工具元数据 + registry_id |
| `LocalRegistry` | `xai-computer-hub-sdk/src/harness.rs` | 进程级，映射 canonical `ToolId`（`namespace:name`）→ 可执行句柄 |
| computer-hub registry | `xai-computer-hub-core/src/registry.rs` | 远端工具按 `(SessionId, ToolId)` 路由，与本地平面组合 |

### 2.2 关键机制

- **分发**：模型调用携带 client-facing 名 → `prepare_dispatch` 线性扫描匹配 → 反查 canonical `ToolId` → `LocalRegistry` 执行。纯名字精确匹配，无模糊/语义匹配。未知名返回 "Tool not found"，错误信息包含合法名字列表，形成模型自愈循环。
- **命名空间**：内置工具暴露裸名（取冒号后一段）；MCP 工具用双下划线 `server__tool`（非 Claude 的 `mcp__`），解析器 overlap-aware；`register_alias` 支持前缀回退。**注意：此机制仅适用于 grok-build 的第一方工具集；本程序不照搬（见 §5 安全决策 R1）**。
- **可见性三层过滤**：
  1. 会话构建期：`CapabilityMode` 按 `ToolKind` 声明式过滤（ReadOnly 剔除 Edit/Write/Execute；meta-tools 恒允许）；
  2. 每轮：`should_list(&ListToolsContext)` 谓词 + plan 模式过滤 + 后端 search 激活时剔除本地 `web_search`；
  3. MCP 准入：`meta.ui.visibility` 标记 `["app"]` 的工具只给 UI。
- **权限**：deny > ask > allow 规则合并（requirements.toml → managed settings → 项目配置 → `.claude/settings.json` 回退）；不信任目录不贡献规则；hub 场景审批策略在 `session.bind` 时盖章；唯一 LLM 侧分类器在 auto 权限模式里，分类的是权限而非工具。
- **子代理路由**：模型在 `task` 工具里自己选 `subagent_type`；工具描述构建时从活体子代理列表动态生成（名字 + 描述 + 工具集）；系统侧只做解析（project → builtin → user → plugin）+ 门控（toggle 禁用表、父 allow-list、child ⊆ parent 能力约束）；选错返回合法列表让模型纠正。
- **元工具**：`search_tool` 发现 MCP 工具，`use_tool` 同轮二次分发执行（`InnerDispatch`），`call_raw` 跳过 reminders 保证后处理只跑一次。
- **一致性原则**：本地/远端/子代理调用全部收敛到同一个 `FinalizedToolset` 分发路径。

## 3. 本程序现状

本程序存在**两套并存的路由架构**：

1. **Legacy 文本检索路由器**（`corelib/tool/router.go`，TUI/GUI 在用）：
   - UIC 统一意图分类（L2 embedding ∥ L3 LLM 树，融合截止线）→ BM25（可选 embedding 混合、reranker）→ 条件工具 keep/filter 规则 → 预算裁剪（`MaxToolBudget=28`）；
   - 敏感工具 fail-closed（ssh/browser 等需分类器激活）；`discover_tool` 作为漏召回恢复路径（4 次/turn 上限）。
2. **Managed Semantic 能力规划器**（`corelib/tool/semantic_planner.go`，agentservice/IM 治理轮在用）：
   - 能力需求解析 → 能力本体匹配 → 不可变 ToolPlan DAG → `MaterializeReadySurface` 渲染闭包；
   - 动态 MCP/skill 工具以不透明适配器名（`invoke_mcp_<base64>`）或 HMAC 一次性授权令牌（`invoke_<sha256>`）暴露，provider 身份不进 prompt；
   - 工具面字节级收据在 HTTP 边界校验（`tool_surface_receipt.go`）；SQLite 协调器幂等账本防重放。

分发链路：渲染面精确名围栏（`corelib/agent/loop.go:2695`）→ 轻量 profile 授权 → 参数校验 → host 执行器。host 侧 name-switch 分发在 `corelib/agentservice/core_agent_executor.go`、`guiapp/im_tool_execution.go`、`guiapp/coding_subagent.go` 多处重复实现。

权限门约 6 层独立 fail-closed：工作流相位策略、专家白名单、ACP 权限注册表、凭证闸（密钥扫描）、出域访问审批、轻量 profile 硬编码清单——**无统一配置层，已核实不存在 `corelib/permission` 或等价的规则合并设施**。注意这 6 层有重要的结构性质（v2 补充）：

- **相互独立、纵深防御**：一层出 bug 或配置错误，其余五层仍然关闭；
- **分层时机不同**：面渲染期（专家过滤 `filterToolsForExpert`、轻量 profile 过滤）vs 分发期（专家执行拒绝、凭证闸）——决定模型是"根本看不见该工具"还是"调用中途被拒"，两者自愈语义不同；
- **参数条件化**：`lightDeniedDatabaseWrite`（同名工具按 action 参数拒）、`expertToolCallRejectionWithDef`（`manage_skill` 按 action+name 拒）、凭证闸（扫描参数载荷中的密钥材质）——三者都是**按调用参数**决策，不是按工具名静态分类。

迁移追踪：`corelib/tool/routingarch/` 是 AST 扫描器 + 双向校验的评审清单（`baseline.go`），每个遗留点位带 Reason 和删除条件，新违规和过时条目都会 fail CI。评估：`corelib/tool/routingeval/` 是**规划器正确性**样本测试集（40 个 JSON 场景，断言 needs/selections/artifact 边），**无命中率/误选率/上下文成本等运行时指标**。

## 4. 对比结论

### 4.1 本程序领先项

1. **语义规划器**：能力需求 → 本体 → 不可变 plan DAG，Grok Build 无此层（全量工具进上下文，靠描述引导）。
2. **一次性授权令牌 + 幂等账本**：动态工具防重放，grant 消费/失效语义明确；Grok Build 动态工具为持久名。
3. **工具面 wire 级完整性收据**：HTTP 边界字节校验；Grok Build 仅名字匹配。
4. **并行写集冲突检测**（`corelib/codingruntime/write_set.go`）：子代理并行隔离要求写集不相交，fail-closed；Grok Build 只有并发数上限（32）。
5. **意图分类双通道融合**：embedding ∥ LLM 树 + 融合截止线，比纯 prompt 引导精细。

### 4.2 本程序短板（v2 修订）

| # | 短板 | 对照 | v2 处置 |
|---|---|---|---|
| 1 | 双轨架构未收口，TUI 不走 Router，`routingarch/baseline.go` 在维护遗留点位评审清单 | Grok Build 单一注册表、全路径收敛 | Phase 3 处理 |
| 2 | 第一方工具缺少**显式的 `namespace:id` 注册约定**——现有 `semanticModelFunctionNames`（`semantic_model_name.go:79-113`）只是渲染名重映射表，不是贯穿注册/分发/授权的 ID 体系（第三方 MCP 的不透明名是正确设计，见 R1） | Grok Build `namespace:id` 全局一致 | Phase 2 在 remap 表基础上升级为 ID 体系 |
| 3 | 无配置化权限策略层，策略散落 env/专家定义/工作流模板 | Grok Build deny > ask > allow 统一规则合并 | Phase 1 处理（按 R3 改造为"共享快照 + 独立消费"） |
| 4 | `PetitionToolCall` 只能下轮补授权且仅限已编目名字 | Grok Build `use_tool` 同轮二次分发 | Phase 2 处理（仅限不改变冻结拒绝文案的前提） |
| 5 | 工具描述/子代理清单的活体引导覆盖窄，delegate 是拒绝式而非引导式 | Grok Build 构建时从活体工具集/子代理列表生成描述 | Phase 3 处理 |
| 6 | 轻量 profile 过滤在过滤结果为空时 **fail-open**（`light_tools.go:96-99`），且枚举清单无任何分类学背书 | Grok Build 声明式 `ToolKind` 过滤 | 改为：枚举清单保留为轻量面定义，AccessKind 只派生**测试断言**（R4） |
| 7 | 子代理能力过滤多处特例实现，无统一能力模型背书 | Grok Build child ⊆ parent 统一约束 + kind 回填 | Phase 3 处理（在 Phase 1 规则之上叠加，不重复迁移） |

## 5. 与既有设计文档的关系 + 安全决策记录

本文不重复以下文档已冻结/已评审的内容，相关阶段直接引用：

- `semantic-tool-routing-design-zh.md`：上位设计（ToolPlan、能力本体、五阶段演进），routingarch 扫描器实现其 §9 phase C；
- `semantic-tool-routing-determinism-and-flexibility-zh.md`：已落地的确定性/灵活性事故修复（tools_search 元工具、petition 扩展、one-shot grant、参数消毒）。**冻结不变量包括：模型可见名即一次性 grant 的载体（同名不 rebound）；并发批内一次性工具仅首次成功；拒绝文案为 "Do not retry…continue with the tools rendered in this request"（列表即真相）**；
- `semantic-routing-miss-fallback-zh.md`：planner 漏召回有界回退的冻结契约；
- `semantic-tool-routing-coding-subagent-remediation-zh.md`：CodingSubAgent 静态工具带并入语义 plan 链的整改（Phase 3 前置）；
- `semantic-tool-routing-continuity-and-surface-reform-zh.md`：双可见性模型冲突的整改（Phase 3 连续性依据）；
- `managed-surface-migration-checklist-zh.md`：每个 legacy 能力迁移前的四问闸门 + 不变量测试范式。**v1 曾宣称 Phase 2 是"纯架构重构、不触发闸门"，评审判定错误：模型可见面变更一律过闸（Q④ 预算/耗尽语义即被改名影响），已修正。**

### 与上位设计的术语映射与对齐声明（v4 新增，对齐审计产出）

术语映射（本文 → `semantic-tool-routing-design-zh.md`）：

| 本文 | 上位设计 |
|---|---|
| CapabilityMode | `ExposureContract.RiskClass` / §6 风险类别（上位无 mode 组合概念，本文为新增） |
| 工具面 / surface | 暴露闭包（materialized exposure surface，§5.2.1/§8.1） |
| grant | invocation grant / selection token（§4.3/§5.2） |
| petition | 上位正文无此概念（活在 principles §7 与 determinism 文档中）；最接近 `capability.discovery` need |
| plan DAG | ToolPlan 有向执行图（§5.2.1） |
| MaterializeReadySurface | materializer + CatalogRenderer 生成最终工具定义（§5/§4.4） |
| ToolDispatcher | PlanExecutor（§5.3）——相邻概念，收敛对象不同（见下） |
| AccessKind | EffectClass / RiskClass |
| `namespace:id` ToolID | capability ID（§4.2，不同概念）/ ProviderBinding（§4.3） |

对齐结论（经上位设计全文审计）：

1. **六项方案均为新增，无重复建设**：上位设计未定义 `corelib/permission` 规则层、第一方 ToolID 体系、CapabilityMode、host name-switch 收敛、活体子代理名册描述（§4.4 规定描述仅来自受治理 capability 文案——Phase 3 第 4 条引入活体名册时须同步修订该条）；TUI 迁移被上位 §9 阶段 A 第 6 条"为所有入口建立 RouteRequest adapter"宽泛覆盖，本文为其实施细化。
2. **顺序无硬冲突**：上位路线为 A（目录+planner 内核）→ B（统一事实/约束 + PlanExecutor）→ C（切换并删除旧路由）；本文 Phase 1 权限统一是把上位阶段 B 第 1 条（专家/群权限/workflow 作为约束 producer）**前置**为"先建配置层、再映射为 planner 约束"，方向兼容。
3. **终态承诺**：上位不变量 #1（单一决策点）与 §8.1 迁移表要求按名条件表最终删除、专家 allow-list 迁移为 planner 约束。本文 R3 保留六门独立消费是**过渡态**而非终态——Phase 3 完成后，六个消费者应按上位阶段 C 继续收敛为 planner 约束，快照层退化为规则解析/合并设施。本文不得被引用为"按名门永存"的依据。
4. routingeval 场景数以实测为准（40，上位 §11.97 同）；v3 的"46"已修正。

### 安全决策记录（v2 新增，对抗性评审的直接产出）

- **R1：第三方/动态 MCP 工具保留不透明适配器名，不引入 `server__tool` 明文命名。** 理由：MCP 工具名是服务端控制的不可信字符串（`mcp_integration.go:72-76` 明确"no lookup-by-provider API"是刻意边界；`semantic_model_name.go:26-34` 明确不透明名防 provider 身份泄漏）。明文命名会把攻击者影响的文本放进模型可见命名空间（`ignore_previous_instructions__x`、与宿主名邻近混淆、overlap-aware 解析器解析对抗输入）。grok-build 的该机制建立在第一方注册表前提上，不可移植。面收据校验保护的是渲染名的**完整性**，不是其内容的**可信性**——v1 风险表的对策不成立，已删除。
- **R2：一次性 grant 语义仍以"渲染名 = 活 grant 载体"实现，不给稳定名。** `semantic_surface_host.go:139-152` 的活 grant 表以渲染名为键并拒绝同名 rebind；若名字稳定化，第二次调用只能（a）同名 rebind（违反碰撞防护）或（b）把令牌作为模型参数（违反 `mcp_integration.go:58-60` "业务参数 only" 与 `semantic_invocation.go:212-217` "签名载荷永不暴露为模型可控参数"）。结论：**第一方工具**通过 `semanticModelFunctionNames` 稳定名重映射获得可读名（无一次性语义需求）；**动态第三方工具**维持 grant-token 名不变。Phase 2 改名范围据此收窄。
- **R3：权限层采用"共享规则快照 + 六个独立消费门"，不做集中求值。** 集中求值会让一个 bug 同时禁用全部策略（单点旁路），且面渲染期/分发期两个时机在集中模型里无法表达。`corelib/permission` 只做：规则解析、 deny>ask>allow 合并、快照发布；各门保持独立实现，改为读取同一快照。规则源不可读时快照本身 fail-closed。
- **R4：轻量 profile 的枚举清单是定义而非负担，AccessKind 只派生测试。** `light_tools.go:11-13` 的固定清单与轻量 system prompt 一一对应，"加工具要改代码"正是保持轻量面最小化的人工评审闸；且现有过滤在结果为空时 fail-open（`light_tools.go:96-99`），改用宽泛分类派生会把分类器错误直接引入轻量路径并可能放大面。处置：枚举清单保留；新增 AccessKind 测试断言"清单内每个工具的分类符合预期"；**顺带修复 fail-open（空列表时应回退到核心只读集而非全量面）**。
- **R5：参数条件化策略用 args 谓词规则表达，不扁平化为静态 kind。** 凭证闸等按参数决策的门，规则模型须支持 `when args match predicate` 形式；凭证闸的 2 分钟超时默认拒绝语义（`im_credential_gate.go:36-38`）在迁移时显式钉死为规则属性，防止未确认凭证卡变成外泄路径。

## 6. 改进计划

### Phase 0：度量基线（2 周）

1. 在 `routingarch/baseline.go` 清单基础上给每个 legacy 调用点加埋点：路由命中率、petition 触发率、未渲染拒绝率、工具面 token 占用。
2. **新建运行时评估能力**（`corelib/tool/routingeval/` 现有的是规划器正确性测试，不含运行时指标，本项是新建）：任务成功率/误选率/上下文成本对照"全量面基线"。**验收门槛（v2 强化）：评估集规模、五条路径覆盖率、统计功效须独立评审通过**——M3/M4 的全部验收都建立在这套指标上，弱指标会橡皮图章化后续阶段。
3. 产出《路由路径现状图》：TUI / GUI / IM / agentservice / coding 五条路径的注册表、过滤、分发归属；**枚举全部 6 个 TUI `BuildTools` 实现**——`tuiCallbacks`（`tui/app.go:3581`）、`tuiBtwCallbacks`（`tui/app.go:4038`）、`tuiLoopCycleCallbacks`（`tui/loop_command.go:286`）、`pipeCallbacks`（`tui/pipe_mode.go:335`）、`rpcCallbacks`（`tui/rpc_mode.go:398`）、`tuiWeixinCallbacks`（`tui/weixin_gateway.go:456`），v1 只提了一处。
4. **TUI 持久化设计（v2 新增）**：语义栈要求生产宿主提供持久化共享 grant store（`semantic_invocation.go:182-198`，SQLite 协调器）。须给出 TUI 独立运行（无 agentservice SQLite 协调器）时的降级设计：grants 存哪、进程重启后 plan 状态谁恢复、退化到何种有界行为。这是 Phase 3 TUI 迁移的前置。

**验收**：评审通过的评估脚本 + 基线报告 + TUI 持久化降级设计。

### Phase 1：权限策略层统一（3 周）

1. 新增 `corelib/permission` 包（按 R3、R5 设计）：
   - `AccessKind` 三态分类（Read / Mutate / Execute）+ **args 谓词规则形式**（`when args match <predicate>`），覆盖 `lightDeniedDatabaseWrite` / 专家 `manage_skill` / 凭证闸三类参数条件化策略；
   - 规则来源（遵循既有配置约定 `~/.maclaw/config.json`，不新增 TOML 约定）：managed settings → `~/.maclaw/config.json` 的 `permission` 段 → 项目级 `.maclaw/permission.json` → `.claude/settings.json` 兼容回退；可选新增 managed 端 `requirements.json` 作为最高优先级来源（JSON 与现状一致）；
   - 求值固定 **deny > ask > allow**，与文件顺序无关；不信任目录不贡献规则；managed yolo pin 可剔除 catch-all allow；**规则源不可读 → 快照 fail-closed**；
   - ask 规则的默认语义显式化：交互宿主超时/无响应 = 拒绝（钉死凭证闸现行语义）。
2. 现有门改造为**同一快照的独立消费者**（不是集中求值器）：
   - 工作流相位策略 → deny 规则消费者；
   - 专家白名单 → per-expert allow 集消费者；
   - 凭证闸 → ask 规则消费者（密钥扫描命中；超时即拒）；
   - 出域审批 → ask 规则的 UI 实现；
   - 轻量 profile（按 R4）：枚举清单保留，AccessKind 仅派生测试断言；**修复 `light_tools.go:96-99` 的 fail-open**。
3. 与 planner 侧既有设施的衔接：planner 已有 `CapabilityPolicyRule`/`RoutingConstraint{Effect:"deny"}`/`require_confirmation`（`corelib/agentservice/`），`corelib/permission` 的输出**映射为** planner 约束的输入，避免双 Deny 真源；单一求值入口 + 对拍覆盖。
4. 每层迁移配"新旧决策对拍"测试，零差异跑稳 2 周后移除旧逻辑。**保留回滚开关**（v2 新增）：旧逻辑按门封存而非删除，一个配置位可整体回退。
5. **数据兼容（v2 新增，v4 补精确路径）**：存量专家定义（`~/.maclaw/experts/experts.json`，JSON 文件含 tombstone/Hub 同步字段，非数据库存储——v4 核实）的工具白名单、工作流模板、`~/.maclaw/config.json` 须给出默认映射，使无 `permission` 段的旧配置行为与现状逐字节一致。

**验收**：单一配置可表达现有全部门控行为；对拍零差异；fail-open 修复带回归测试；回滚开关演练通过。

### Phase 2：命名空间与分发收敛（3~4 周，范围已按 R1/R2 收窄）

1. **命名（收窄后）**：仅为**第一方工具**引入显式 `ToolID = {namespace}:{name}` 并通过 `semanticModelFunctionNames` 重映射获得稳定可读名；**第三方/动态 MCP 与 skill 适配器维持不透明名 + grant-token 名不变**（R1/R2）。不再追求"全仓无随机名"。
2. **分发收敛**：host 侧收敛为单一 `ToolDispatcher` 接口，host 只注册 handler，不做名字判断；消除 `coding_subagent.go` / `im_tool_execution.go` / `core_agent_executor.go` 三处重复 name-switch。**此为本阶段主体工作量。**
3. **Petition 升级（收窄后）**：`PetitionToolCall` 升级为同轮二次分发。
   - **前置阅读**：`semantic-tool-routing-determinism-and-flexibility-zh.md`；必须保持 grant 消费一次、后处理只跑一次的不变量（对齐 grok-build `call_raw` 跳过 reminders）。
   - **删除 v1 的"未知工具错误回显合法名字列表"条目**：该文案与冻结拒绝文案"Do not retry…continue with the tools rendered in this request"（列表即真相原则）直接冲突；合法名发现是 `tools_search`/petition 的职责，已由冻结契约覆盖，不重复建设。
4. **闸门适用性（v2 修正）**：第一方工具改名是模型可见面变更，**逐能力过四问闸门**；可用性矩阵按清单范式编写（范式锚定见 `managed-surface-migration-checklist-zh.md:40-49` 对 `guiapp/semantic_ssh_connect_test.go` 的引用），具名断言随改名同步更新 `semantic_model_name_test.go` 与相关 pin 测试。

**验收**：第一方工具稳定名重映射完成且过闸；host 分发只剩注册表；petition 同轮分发保持全部冻结不变量（pin 测试全绿）；模型自愈率较基线提升。

### Phase 3：语义面全端默认（4~6 周）

1. TUI（**全部 6 个** `BuildTools` 站点，逐个迁移）与 GUI legacy 路径迁移到 `MaterializeReadySurface`；`Router.RouteWithOptions` 降级为 capability need 推导器的一个输入。
   - 每个能力的迁移必须通过 `managed-surface-migration-checklist-zh.md` 四问闸门，并附四场景可用性矩阵等不变量测试；分类器降级路径（floor exemption / leftoverKeeps / petition）必须书面回答。
   - 遵守 `semantic-routing-miss-fallback-zh.md` 的漏召回有界回退冻结契约。
   - TUI 迁移落地前，Phase 0 的持久化降级设计必须先实现并评审通过。
2. `routingarch/baseline.go` 的删除条件：**清单内每个点位的删除条件全部满足**（双向校验清零），而非按时间删除；清单清零后 scanner 降级为纯违规检测。
3. 定义声明式 `CapabilityMode`（kind 过滤 + child ⊆ parent 约束）。**与 Phase 1 的关系（v2 澄清，消除原双重迁移矛盾）**：专家白名单、workflow 策略的规则迁移在 Phase 1 已完成且仅此一次；Phase 3 的 CapabilityMode 是在 Phase 1 规则**之上叠加** kind 组合层（AccessKind 分类动词 × 模式组合策略），统一的是 read-only 角色等纯可见性过滤，不重复迁移规则。
4. `task`/`delegate_task` 描述构建时从活体子代理列表生成（角色 + 能力 + 工具集），拒绝式 admission 保留为兜底；CodingSubAgent 侧先完成 `semantic-tool-routing-coding-subagent-remediation-zh.md` 的整改。**（v4 标注：此条与上位 §4.4"描述仅来自受治理 capability 文案，renderer 不得注入动态元数据"冲突，实施时须同步修订上位该条，并给出治理等价性论证——名册内容须来自受治理的子代理注册表而非运行时随意枚举。该冲突在本切片未解决，保持原标注。）**

   - **进展（2026-09-19）**：整改文档 §9.12 门禁 #3 切片 1 已落地——动态 factory 消费单一不可变生产组合输入（plan + admitted bindings + publish fn + coordinator route，缺一即拒、无 by-name 回退），`codingDynamicAliasesMayMaterialize` 仍为 false；切片 2（callback scope/admission 接线 + E5 原子 cutover）未开始。同日落地切片 5 的 shadow-only 部分（整改文档 §9.19）：远程只读 provider specs（`fs.read.remote`/`repo.inspect.remote`，已验证 SSH 会话绑定，本地/远程绑定不可互换）+ 远程 S0 观察从 `not_prepared` 升级为真实 shadow plan 对账；仍无 cutover，远程静态带照旧服务。绑定的 transport conformance 证据与显式接受缺口记录于 §9.20（opaque 句柄/跨句柄失效/互换拒绝已 hermetic 证明；拨号唯一性、重连 pin 仍为未证缺口；exec 取消栅栏已在 §9.20 修正条中退役——assembler 修复为取消/超时零结果 + corelib/guiapp 双侧 conformance 钉死）。切片 9 骨架已启动（整改文档 slice 9）：`routingeval` 新增 `coding_family` 类目（category_id 41，居于 10.1 的 1-40 之上，harness 允许 40 以上的整改切片类目），9 个 planner 样本覆盖只读姿态约束、required/optional（新增 `omitted` 断言通道）、repeat sibling、schema token 预算（closed-wave 前缀）、no-fallback、远程读绑定与缺席降级、本地/远程不可互换；§9.4 的 catalog_incomplete/policy_denied 聚合指标在切片 2 产出 live 数据前保持 pending，不伪造指标。

**验收**：评审通过的运行时评估中，semantic 面在全端任务集成功率不低于 legacy、上下文成本下降；`baseline.go` 评审清单清零。

### Phase 4：精细化（2 周，**排在 Phase 3 验收之后**，v2 修正）

- `discover_tool` 与 `semantic_tools_search` 合并为一个元工具：与冻结契约冲突点（tools_search 4/turn 预算、petition 准入规则，determinism 文档 §4.8）须附契约差异附录并修订冻结文档后再动手；
- 写集冲突检测泛化为通用 mutation scope，供权限层引用（此项可与 Phase 3 并行）；
- 埋点数据回写 `UsageTracker`，形成路由质量闭环（此项可与 Phase 3 并行）。

## 7. 关键风险与对策（v2 修订）

| 风险 | 对策 |
|---|---|
| 权限层对拍期间新旧逻辑漂移 | Phase 1 强制对拍测试，不一致 fail CI；回滚开关可整体回退 |
| 第三方工具明文命名的 prompt 注入面（v1 对策经评审不成立，已放弃该路线，见 R1） | 第三方工具名维持不透明；第一方工具名来自受信注册表，Receipt 校验照旧 |
| 一次性 grant 语义在改名后失锚（v1 未解决） | 按 R2：动态工具名不变，grant 仍以渲染名为载体；第一方稳定名无一次性语义需求 |
| TUI 迁移破坏轻量端体验 | 语义面支持 `PlanningBudget`，TUI 用更紧的 token 预算渲染；独立运行时按 Phase 0 降级设计退化 |
| 迁移期分类器降级导致能力不可用（2026-09-18 ssh 事故链先例） | 所有面变更逐能力过四问闸门 + 可用性矩阵测试；floor exemption 显式书面化 |
| `coding_subagent.go`（16k 行）收敛引入回归 | 先抽接口再加测试（spawn/orchestrator/admission 已有单测基础），小步合并 |
| Phase 1 与 planner 侧既有 Deny 规则形成双真源 | permission 快照为唯一规则真源，planner 约束由快照映射生成（映射本身纳入对拍覆盖）；六个消费门各自独立求值（R3） |
| 集中化权限求值变成单点旁路 | 按 R3 保持六门独立消费共享快照；快照不可读 fail-closed |
| 轻量 profile 分类派生引入 fail-open 放大 | 按 R4 不派生运行时面，仅派生测试；并修复现有 fail-open |
| 凭证闸迁移后超时语义漂移成外泄路径 | 按 R5 超时即拒钉死为规则属性，迁移测试覆盖 |
| 新建运行时评估的样本偏差 | 评估集从真实会话日志采样，覆盖五条路径 + 6 个 TUI 站点，统计功效独立评审 |
| 数据兼容：旧配置/旧专家定义行为漂移 | Phase 1 默认映射逐字节复现现状；迁移 appendix 给出对照表 |

## 8. 里程碑与总周期（v2 更新）

- M1（第 2 周）：基线报告 + 评审通过的运行时评估能力 + TUI 持久化降级设计
- M2（第 5~6 周）：`corelib/permission` 上线（共享快照 + 独立消费门），对拍零差异，回滚开关演练通过
- M3（第 9~10 周）：第一方命名空间 + host 分发统一 + petition 同轮分发，全部过闸，pin 测试绿
- M4（第 14~16 周）：semantic 面全端默认（含 6 个 TUI 站点），`baseline.go` 评审清单清零
- M5（第 17~18 周，紧随 M4）：Phase 4 收尾——元工具合并（附契约差异附录）、mutation scope 泛化、UsageTracker 闭环上线

总周期约 12~16 周。Phase 0→1 可随时启动；Phase 2/3 依赖基线数据；Phase 3 速度受闸门评审节奏约束，不得跳闸；Phase 4 在 Phase 3 验收后排期。

## 9. 变更记录

**v1 → v2（对抗性设计评审吸收）**：

1. 删除"第三方 MCP 工具改用 `server__tool` 明文命名"（评审 blocker #1）：不透明名是刻意的不可信输入隔离边界，改为仅第一方工具引入命名空间（R1）。
2. 补 grant 载体设计（评审 blocker #2）：明确一次性语义仍以渲染名为载体，不给动态工具稳定名（R2）。
3. 删除 Phase 2 的"不触发迁移闸门"豁免和"错误回显合法名列表"条目（评审 blocker #3）：前者与文档自身承诺矛盾且 Q④ 直接相关；后者与冻结拒绝文案冲突，tools_search 已覆盖该职责。
4. Phase 1 增加 args 谓词规则与凭证闸超时语义钉死（评审 major #4/#5）。
5. 权限层从"集中求值"改为"共享快照 + 六独立消费门"，保留纵深防御与面渲染/分发双时机（评审 major #5，R3）。
6. 消除 Phase 1 与 Phase 3 对专家/工作流门的双重迁移矛盾（评审 major #6）。
7. 轻量 profile 从"AccessKind 派生运行时面"改为"枚举清单保留 + 分类派生测试 + 修复 fail-open"（评审 major #7，R4）。
8. TUI 面从 1 个 `BuildTools` 站点扩为全量 6 个，新增 TUI 无 SQLite 协调器时的持久化降级设计为 Phase 0 交付（评审 major #8）。
9. Phase 0 从 1 周扩为 2 周，评估 harness 验收须独立评审（评审 minor #9）。
10. 新增回滚开关与存量配置/专家定义兼容 appendix 要求（评审 minor #10）。
11. Phase 4 从"可并行"改为排在 Phase 3 验收后，元工具合并须附契约差异附录（评审 minor #11）。

**v2 → v3（引用准确性 + 内部一致性评审吸收）**：

12. 修正 §4.2 短板 #2 的事实表述：第一方并非"无稳定命名空间"，`semanticModelFunctionNames` 重映射表已存在；短板重新定义为"缺显式 `namespace:id` 注册约定，remap 表不是贯穿注册/分发/授权的 ID 体系"，消除与 Phase 2 方案的自我矛盾（v2 遗留）。
13. 修正 Phase 2 对迁移清单 `:40-49` 的引用：该处锚定的是四场景可用性矩阵的**测试范式**（`guiapp/semantic_ssh_connect_test.go`），不钉具体渲染名；改名影响的具名断言在 `semantic_model_name_test.go` 等 pin 测试（引用核对 finding #10）。
14. TUI 6 个 `BuildTools` 站点改为列出实际回调名（`tuiCallbacks`/`tuiBtwCallbacks`/`tuiLoopCycleCallbacks`/`pipeCallbacks`/`rpcCallbacks`/`tuiWeixinCallbacks`），便于实施期检索（引用核对 finding #8）。
15. 清理 §7 风险表的删除线表述；并将"单一求值入口"改为"快照唯一真源 + 六门独立求值"，消除与 R3 的措辞张力。
16. 里程碑补充 M5（Phase 4 收尾），总周期明确为 12~18 周。
17. 引用核对结论：v2 其余 10 项代码引用全部准确（fail-open、opaque 边界注释、grant 表键、持久化 store 要求、凭证闸超时即拒、围栏与拒绝文案、6 站点行号、`semantic_model_name_test.go`、面收据文件），无需修改。

## 10. 实施进度（v4 起记录）

**评审轮次 R2（实施中评审第二期，覆盖最近 ~10 个切片）**：确认五宿主 dispatcher 试点的等价性主张全部成立（kill switch 默认 off、无 name-keyed 注册、ID 解析不可被影子化、handler 穿过完整 legacy 入口含全部围栏）；UsageTracker 双线记录排查结论为**无重复**（遗留 IM 与 shared IM 的写入路径不相交）；coding remediation 新增件（组合输入/remote 影子 catalog/conformance/exec fence）无越界。修复 6 项：中低 1（`RecordRoutingStats` 门控 6 个记录函数中 4 个未遵守文档契约——已统一）、低 2（`Decision.Rule` 浅拷贝与文档不可变声明不符——已深拷贝 Kind/When/切片）、低 3（Prefix 谓词边界语义入文档+钉原始行为）、信息 3（database/database_query 双语别名消重避免敏感/只读并列 tie、taskeval `MinSet` 消除 0 歧义、导出多模态拼接加分隔符）。两项值得试点扩面前关注：Prefix 作者须自带尾分隔符（文档已警）、`legacy_adapter_catalog.go` init 两处检查恒真（只有别名目标检查有意义，fail-fast 语义可接受，vet nit 记待办）。

**评审轮次 R1（实施中评审，2026-09-18）**：两个并行评审代理对全部新增代码做了 27 项核查，确认 15 项修复并全部落地——高危 2 项（routing_stats 测试污染用户真实数据文件并删除；surfaceeval 离线 harness 污染线上 telemetry 与持久化——已加 `RecordRoutingStats` 开关）；中危 4 项（热路径每调用全量 JSON 解析——已加 `Snapshot.HasArgsRules` 快路径；专家翻译 store/builtin 并集产生幻影 allow——已改 store-first 去重对齐 legacy 解析；用户拒绝/超时误记为 ask——已改 deny；瞬时加载错误永久 DenyAll+每调用刷日志——三宿主改为 nil+单次日志）；低危 9 项（空配置文件误 fail-closed、死谓词规则、非标量 `%v` 匹配、Decision.Rule 可变指针、dispatcher 错误丢弃、持久化无限重试、日志形状漂移、scope 竞态、复制语义注释）。确认不修并已记录：dispatcher 命中跳过前置守卫（试点纪律）、跨门重复打点（观察期已知噪音）、v2 工作流门（Phase 3 决策）、技能级白名单不可表达（翻转切片前置条件，代码 NOTE 已锚定）。

| 切片 | 状态 | 交付物 |
|---|---|---|
| R4 轻量 profile fail-open 修复 | ✅ | `corelib/agent/light_tools.go` 核心只读回退集 + 回归测试 |
| Phase 0-1 路由埋点 | ✅ | `corelib/tool/routing_stats.go`（8 项指标，防抖持久化），hook 于 router/loop/discover_tool；`FormatRoutingLine()` 挂接 TUI/doctor/CLI 三处状态展示 |
| Phase 0-2 离线评估 harness | ✅（离线双评估 + 采集管线；live/评审 pending） | `corelib/tool/surfaceeval/`（召回/面大小/token vs 全量基线，种子集 12 样本）；种子基线三个缺口（ssh 无 UIC fail-closed、小写 glob 无 provision、web_search 跨语言 BM25 零分）已全部关闭，meanRecall 0.700→1.000（均面 10.8→13.0，token 节省 54.8%→46.1%）；**LLM-in-the-loop 成功率组件骨架已启动**：`corelib/tool/taskeval/`（录制转录回放模式，`TurnSelector` provider 接缝，种子转录 3 条，指标确定性）；**转录捕获管线已落地 2 个数据源**（sources: 2 of N：`export.go` corelib/agent ConversationMemory 快照 → Dataset；`guiapp/taskeval_export.go` guiapp IM 会话搜索库 `session_search.db` → 同一 Dataset 映射，等价性测试锁定两源统计口径一致；agentservice 会话库等其余数据源待接）；provider/live 模式未开始；harness 独立评审未进行 |
| Phase 0-3 路由路径现状图 | ✅ | `docs/design/tool-routing-phase0-baseline-zh.md` §1（五路径 + TUI 6 变体全表） |
| Phase 0-4 TUI 持久化降级设计 | ✅ | 同文档 §2（"计划可恢复、授权不可恢复、授权随首用重签"；JSONL journal；rpc 多客户端场景标注复核） |
| Phase 1 引擎 | ✅ | `corelib/permission`：deny>ask>allow 多源合并、args 谓词（R5）、Subject 作用域、fail-closed、yolo pin、`.claude/settings.json` 回退 |
| Phase 1 loop 对拍 | ✅ | `corelib/agent/permission_dual_eval.go`：`authorizeLoopTool` 四决策点双评，仅具体规则分歧打点 |
| Phase 1 宿主接线 | ✅（3/3 宿主族） | guiapp IM（`App.permissionSnapshot`）、agentservice CoreAgent（`CoreAgentExecutor` 级）、TUI 全部 7 个回调（进程级 holder，package main）；guiapp 其余 5 个回调（ve/btw/coding/remote/loop-command）待接线 |
| Phase 1 门迁移（双跑阶段） | 🟡（5/6 + 1 决策推迟） | 专家白名单、凭证闸、出域审批（含 `ArgsPredicate.Prefix` 引擎扩展）、ACP 权限注册表（allow/deny 映射，ask 在客户端轮询不可见——已注释）全部双跑；loop `authorizeLoopTool` 对拍 + planner 表面桥接覆盖其余两席。**工作流相位门决策推迟到 Phase 3**（每相位动态策略不适合 once-built 快照，按上位设计直接收敛为 planner 约束）。**翻转前技术债**：非白名单工具的 legacy 拒绝对快照不可见（需门侧补集或引擎默认效果）；sync.Once 快照时效性；凭证闸超时即拒须编码为 ask 规则属性；ACP/出域的"规则只收窄不放宽已协商授权"原则已在代码锚定 |
| Phase 1 配置兼容 + 回滚开关 | 🟡（兼容映射 ✅ / 开关 ⬜） | 附录 A 已交付（存量表面→规则表示对照表 + 单一事实源）；database 写动作清单单一事实源已建（`database.WriteActions()` + `permission.DatabaseWritePredicate()`，含 `InFold` 大小写不敏感匹配的防漂移对拍）；回滚开关约定已入附录，按门随翻转切片交付 |
| Phase 1 其余门/对拍全量/planner 映射/配置兼容/回滚开关 | ⬜ | 凭证闸、出域审批、工作流相位、ACP 注册表、轻量 profile 测试断言 |
| Phase 2 命名空间与分发收敛 | 🟡（五宿主试点，50 名，注册债全清） | `corelib/toolid` ✅；双注册表 ID 化；试点：**agentservice 20、IM 11、TUI 9、coding 8、remote coding 5**；parity 真实写标准；`loop_command` 孤岛治理 ✅（含 edit_file 出厂 bug）；petition 同轮升级拒绝实现（冻结契约阻断）；注册债 **`ripgrep` 与 `coding_knowledge_search` 均清偿** ✅（后者抽取共享执行器 `runCodingKnowledgeSearch`，顺带修正 `noteRecalledExperiences` 记录合并后命中列表——已披露的有据改进）；两债清偿均记录爆炸半径（IM/桌面表面新增渲染两工具，零既有断言破裂）；`current_datetime` 因时钟非确定性排除；待运行期：开闸观察 |
| Phase 3 语义面全端默认 | 🟡（整改关键路径启动） | CodingSubAgent remediation 工单分解完成（10 切片，关键路径：切片0→1→2→…）；**切片 1（不可变生产组合输入）✅**——四组件组合 + digest 漂移拒绝 + 无 by-name 恢复，E3/E4/E5 全套绿；**切片 0（源码唯一来源）调查完成**：旧注两半均过时（`gui/` 为 2026-09-09 退役并 gitignore 的旧单体；`guiapp` 3,981 文件受跟踪即唯一事实源，remediation 文档已更正），**遗留动作 = 18 个未跟踪证据 .go 文件随切片 2 提交入 Git，需用户授权 git 提交**；设计钉冲突（Phase-3 第 4 条 vs 上位 §4.4）仍未解决，阻塞该项排期 |
| Phase 4 精细化 | 🟡（闭环项已落地，其余排期不变） | 埋点回写 UsageTracker（§6 第 3 条，原定与 Phase 3 并行）：**✅ 2026-09-19 落地**——调查发现消费端（`Router.scoreEligible` 经 `ExperienceScore`/`ContextOutcomeScore`/`RoutingHintAdjustment`，`router.go:1800-1814`）此前仅由 legacy IM 执行层（`im_agent_loop_tool_exec.go:535`）与 skill 运行器喂真实结果，全部 `agent.RunLoop` 宿主（shared IM 回路、coding/remote coding、`/loop`、TUI 6+1 站点）静默；现 core 回路新增可选 `agent.UsageTrackerProvider` 接口（`corelib/agent/loop.go`），仅在真实 dispatcher 执行后（policy 拒绝/参数拒绝/replan 跳过均不喂）以 BM25 top-5 用户 token + 成败 outcome 异步回写，失败为有限轻负权（usageOutcomeWeight -0.3）且不触发连续失败抑制（抑制需显式 retry/abandon，core 层不断言）；宿主接线：guiapp sharedAgentLoopCallbacks/loopCycleCallbacks/codingSubAgentCallbacks/remoteCodingCallbacks → `IMMessageHandler.usageTracker`，TUI 经 `usageTrackerFeed` mixin 懒加载默认路径（打开失败即 no-op）；无 tracker 宿主（agentservice/maclaw-cli）天然 no-op；测试 `corelib/agent/loop_usage_tracker_test.go`（成功/失败/nil no-op/policy 拒绝不喂 + OutcomeScore 可见变化）；元工具合并与 mutation scope 泛化仍按原计划排在 Phase 3 验收后 |

### 附录 A：存量配置兼容映射（Phase 1 第 5 项交付）

原则：无 `permission` 段的旧配置必须逐字节复现现状（双跑阶段此原则由"行为不翻转"天然满足；翻转切片以本表为对拍依据）。

| 存量表面 | 规则表示 | 单一事实源 |
|---|---|---|
| `~/.maclaw/config.json` 的 `permission.rules` | 原生规则（用户源，低于 managed） | 引擎 `Load` |
| `~/.maclaw/experts/experts.json` 工具白名单 | per-expert allow 规则（`Subject=expertID`）+ 空白名单不贡献规则 | `guiapp/expert_permission_rules.go` |
| `.claude/settings.json` 的 `permissions.deny/ask/allow` | 无谓词规则（provenance `claude-fallback`） | 引擎 `loadClaudeRules` |
| database 写动作门（轻量 profile / `database_query` 只读面 / 工作流相位共用） | `Rule{Tool: "database"/"database_query", Effect: deny, When: permission.DatabaseWritePredicate()}` | `corelib/database.WriteActions()`（新提取的唯一清单；`permission` 经 `InFold` 大小写不敏感匹配与 `IsWriteAction` 逐动作对拍，见 `compat_test.go`） |
| 轻量允许清单 | **不迁移**（R4：枚举清单即轻量面定义），仅分类学测试断言背书 | `light_tools_classification_test.go` |
| 凭证闸超时即拒 | 翻转时编码为 ask 规则属性（本附录增补：建议规则字段 `on_timeout: "deny"`，引擎扩展留待翻转切片） | 注释锚定于 `im_credential_gate.go` |
| 工作流相位 ToolPolicy | 不映射为规则（推迟 Phase 3，直接收敛为 planner 约束） | 进度表决策记录 |
| 出域审批 / ACP 协商授权 | 规则**只收窄不放宽**已协商授权（代码注释锚定） | `coding_subagent_scope_approval.go` / `acp_permission.go` |

回滚开关：翻转切片交付。约定——每个门翻转时保留 legacy 实现于配置位之后（env `MACLAW_PERMISSION_GATE_<name>=legacy`），对拍零差异运行 2 周后方可删除；双跑阶段的 killswitch `MACLAW_PERMISSION_DUAL_EVAL=off` 已全线生效。

**v3 → v4（上位设计对齐审计 + 量化事实核验吸收）**：

18. 修正 routingeval 场景数：46 → **40**（实测 `corelib/tool/routingeval/data/` 为 40 个 JSON，上位设计 §11.97 同；v2 引用核对漏项）。
19. 新增 §5"术语映射与对齐声明"：本文六项方案经上位设计全文审计均为新增、无重复建设；明确 Phase 1 是上位阶段 B 的前置改写而非平行分支；**承诺六门独立消费是过渡态**，Phase 3 后须按上位阶段 C 继续收敛为 planner 约束，防止本文被引用为"按名门永存"的依据。
20. 数据兼容条目补专家定义实际存储路径（`~/.maclaw/experts/experts.json`，JSON 非 DB）。
21. 标注 Phase 3 第 4 条（活体子代理名册描述）与上位 §4.4"描述仅来自受治理 capability 文案"的冲突，实施时须同步修订该条。
