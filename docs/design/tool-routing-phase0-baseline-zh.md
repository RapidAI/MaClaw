# 工具路由 Phase 0 基线：路由路径现状图 + TUI 持久化降级设计

- 状态：实施中（Phase 0 交付物 3/4，对应 docs/design/tool-routing-improvement-plan-zh.md §6 Phase 0 第 3、4 项）
- 本文档是改进计划的基线输入：现状图用于 Phase 3 迁移范围界定，降级设计是 Phase 3 TUI 迁移的前置门槛
- 事实截至：2026-09 代码快照（所有引用经源码核实）

## 1. 路由路径现状图

### 1.1 五条路径总览

| 路径 | 工具面来源 | 过滤层 | 执行分发 | 持久化 |
|---|---|---|---|---|
| TUI 主会话 | 静态 `agent.NewCoreToolRegistry()`（每 TUIApp 一个，tui/app.go:231） | 只读子代理过滤（app.go:3583）+ 可选 spawn 工具 + 轻量 profile 过滤（app.go:3590-3593）+ 每调用工作流策略（app.go:3657/3667） | `CoreToolRegistry.ExecuteCtx`（app.go:3621） | 仅会话历史 JSON；无 grant/面状态 |
| GUI IM | `tool.Router.RouteWithOptions`（guiapp/tool_router.go:62-67，UIC 经 `SetUnifiedClassifier` 注入） | UIC 条件激活 + BM25 预算裁剪；managed turn 硬阻断 legacy 路由（im_handler_wiring.go:1193）；router-nil 时剥离条件工具 fail-closed | `executeAgentLoopToolCall`（im_tool_execution.go:66） | guiapp SQLite 协调器（`semantic-routing/semantic-execution.db`） |
| agentservice Core Agent | 静态 specs（core_agent_executor.go:1799）+ managed semantic 面（`ClosedManagedDefinitionsForProfile`，1831-1847） | 能力投影 + 只读子代理过滤 + 远程绑定 ssh-only + 轻量 profile | CoreAgentExecutor 回调 | agentservice 侧 `OpenDynamicSemanticRoutingResources` |
| Coding 子代理 | 内存面 `codingDynamicSurface` 别名表 / durable 路径 ToolPlanner + SQLite 协调器 + CatalogRenderer | 角色 allowlist + 写集冲突 fail-closed | `ExecuteToolCallWithContext`（coding_bound_dynamic_request_adapter.go:593） | 同 guiapp SQLite 协调器 |
| TUI 变体（5 个） | 静态 registry（4 个）/ 硬编码清单（1 个） | 见 §1.2 | `toolRegistry.ExecuteCtx` / `Execute` | 无 |

### 1.2 TUI 变体差异（Phase 3 迁移的完整清单）

| 回调 | 位置 | 面来源 | 与主 TUI 的差异 |
|---|---|---|---|
| `tuiCallbacks` | tui/app.go:3581 | `toolRegistry.BuildDefinitions()` | 基准（有轻量过滤） |
| `tuiBtwCallbacks` | tui/app.go:4038 | `buildTuiBtwToolDefinitions`（请求级最小集） | memory 工具 recall-only 在 Execute 强制（app.go:4057-4062） |
| `tuiLoopCycleCallbacks` | tui/loop_command.go:286 | 硬编码 5 工具清单（`tooldef.BuildToolDef` 内联构建） | **不经注册表**，迁移时需先迁入注册表 |
| `pipeCallbacks` | tui/pipe_mode.go:335 | `toolRegistry.BuildDefinitions()` | 轻量过滤已补：仅在 `MACLAW_PROMPT_PROFILE` 全局强制 light 时生效（默认状态可证明为 no-op）；无只读子代理状态，该过滤结构性缺席（有注释） |
| `rpcCallbacks` | tui/rpc_mode.go:398 | 同上 | 同上；`IsToolAllowed` 恒 true（信任调用方，rpc_mode.go:421）——注意强制 light 部署会收窄 RPC 客户端可见面，属预期策略语义 |
| `tuiWeixinCallbacks` | tui/weixin_gateway.go:456 | 同上 | 同上（轻量过滤仅 env 强制时生效；只读子代理过滤结构性缺席） |
| `tuiSchedulerCallbacks` | tui/agent_tools_schedule.go:372 | `toolRegistry.BuildDefinitions()`（2026-09-19 评估入表） | 与 pipe/rpc/weixin 相同：轻量过滤仅 `MACLAW_PROMPT_PROFILE` 全局强制时生效（后台定时任务走真实 RunLoop 回合，系统提示同样吃 env 覆盖）；无只读子代理状态，该过滤结构性缺席 |

### 1.3 关键发现

1. **TUI 全族不走 `tool.Router`**：tui/ 目录零 `RouteWithOptions`/`NewRouter` 引用。TUI 的"路由"= 静态全量注册表 + 轻量过滤，正是改进计划 Phase 3 要迁移的对象。
2. **变体间过滤不一致（已收口，2026-09-19）**：pipe/rpc/weixin/scheduler 四个变体已补上与主 TUI 对齐的轻量过滤，但**仅在 `MACLAW_PROMPT_PROFILE` 全局强制 light 时生效**——这些变体不做文本分类，默认状态下过滤可证明为 no-op；强制 light 下四者现在渲染 light 面（与其 light 系统提示一致；scheduler 的后台定时任务走真实 RunLoop 回合，系统提示同样吃 env 覆盖），RPC 模式因 `IsToolAllowed` 恒 true 会随强制 light 收窄可见面，属预期策略语义。只读子代理过滤四变体结构性缺席（无 `runtimeReadOnlyChild` 状态，代码注释已记录）。`/btw` 的最小集与 `/loop` 的固定 full-profile 周期为刻意保留的例外（各有代码注释）。
3. **`loop_command` 是孤岛**：硬编码 5 工具清单不经过 `CoreToolRegistry`，Phase 2 收敛 ToolDispatcher 时它需要一个显式注册步骤。
4. **Router 实例化**：guiapp 为每进程一个共享 Router（app.go:748-749），会话 pin 刻意 no-op；TUI/agentservice 无 Router 实例。
5. **持久化现状**：TUI 会话重启后工具面从静态注册表全新重建，grant/别名等面状态随进程死亡——今天语义栈的 SQLite 协调器只存在于 guiapp 与 agentservice。

## 2. TUI 持久化降级设计（Phase 3 前置门槛）

### 2.1 需求

语义栈要求生产宿主提供 durable、共享的 grant store（`corelib/tool/semantic_invocation.go:182-198`，memory store 仅限测试）。TUI 独立运行（无 agentservice、无 guiapp 的 SQLite 协调器）时，Phase 3 迁移 `MaterializeReadySurface` 必须回答：grants 存哪、重启后谁恢复、退化到何种有界行为。

### 2.2 设计：文件级 grant journal + 重启降级语义

**存储**：TUI 进程内嵌一个文件后端 grant store，位置 `<maclawpath.DataDir()>/semantic-routing/tui-grants.jsonl`（与 guiapp 的 `semantic-execution.db` 同目录不同文件，避免与多宿主共享库争用）。每行一条 JSON 记录：grant 摘要（渲染名哈希、selection 引用、签发时间、计划代际），**不含参数载荷与签名原文**（签名原文留在内存，重启即失效）。

**重启恢复语义（有界降级）**：
1. TUI 重启后 journal 仅用于**归因与诊断**（上一会话签过哪些 capability、哪一代计划），**不恢复活 grant**——活 grant 依赖进程内签名密钥与面代际，跨进程恢复会违反一次性语义（R2：渲染名 = 活 grant 载体，载体随进程死亡）。
2. 面状态按计划 DAG 从 ToolCatalog 重建（语义栈本身支持重放），重建后由 planner 重新签发新 grant——即"计划可恢复、授权不可恢复、授权随首用重签"。
3. 降级边界：journal 文件损坏/不可读 → 忽略并继续（当前行为），仅记一条日志；不 fail-closed 阻断 TUI 启动，因为 TUI 面本来就是静态重建的。

**与 guiapp 路径的关系**：TUI 变体中 `rpcCallbacks` 是 RPC 服务的宿主（可能被多客户端连接），若未来 rpc 模式需要多进程共享 grant，应复用 guiapp 的 SQLite 协调器而非扩展 JSONL——本文档范围只覆盖独立 TUI 单进程场景，该判断留给 Phase 3 实施期复核。

### 2.3 验收要点（Phase 3 实施时核查）

- 重启后会话可继续，面按 plan 重建，新 grant 正常签发；
- 上一会话的 grant 不可重放（用尽/过期的拒绝语义与冻结契约一致）；
- journal 不泄漏参数载荷/签名原文（文件权限 0600，目录 0700）；
- `loop_command` 硬编码清单已迁入注册表或显式声明为例外。

## 3. 基线度量现状（Phase 0 第 1 项产出已落地）

| 设施 | 位置 | 内容 |
|---|---|---|
| `corelib/tool/routing_stats.go` | 新增 | legacy 路由调用/选中面大小、条件激活、未渲染拒绝、已耗 grant 拒绝、petition 救援、discover_tool 命中/未命中；防抖持久化到 `~/.maclaw/data/stats/routing.json`；`FormatRoutingLine()` 已挂接 TUI /doctor、maclaw-cli shared-loop、doctor 报告 |
| `corelib/agent/prompt_profile_stats.go` | 既有 | 轻量 turn 占比、轻量拒绝数（按工具细分）、light→full 升级数 |
| `corelib/tool/surfaceeval/` | 新增 | 离线表面选择评估器：数据集 JSON + 全量面基线对照，输出召回率/面大小/token 估计；种子集 `data/baseline-v1.json`（12 样本）。首跑基线：10 个召回样本 meanRecall=0.700，均面 10.8 vs 基线 25，token 节省 54.8%；失败样本恰好暴露三个真实缺口——无 UIC 时 ssh fail-closed、`glob` 小写拼写无 legacy provision（provision 表只认 `Glob`/`ripgrep`）、中英混合查询下 web_search BM25 得 0。三个缺口均已关闭：UIC 缓存桩（样本声明 `simulated_intent`，harness 挂接 NoopEmbedder + 预置缓存的 UIC 桩，无 LLM/embedding 调用）；`legacy_adapter_catalog.go` 查询边界新增受审别名 `glob`→Glob、`grep`→ripgrep（仅已审核的替代表记，不授予新能力）；`BuiltinEnrichments` 为 web_search/web_fetch/knowledge_search/glob/grep 增补受控中英别名。当前种子基线：meanRecall=1.000，失败样本 0；代价是均面升至 13.00（小写 glob/grep 进入候选池后对泛查询有少量 n-gram 命中），token 节省 46.11% |
| `corelib/tool/routing_dual_eval_stats.go` | 新增 | 双跑分歧计数：按 gate 聚合（16 槽 + overflow 桶）、最近分歧摘要（无用户文本）、防抖持久化到 `~/.maclaw/data/stats/permission_dual_eval.json`；已接入全部六个默认 hook（loop 对拍/专家/凭证/出域/ACP/planner 表面），`FormatPermissionDualEvalLine()` 挂接三处状态展示——这是翻转切片"对拍零差异跑稳 2 周"的度量基座 |
| `corelib/tool/taskeval/` | 新增 | 任务成功率评估骨架（LLM-in-the-loop 组件的离线替身）：录制转录回放——每轮携带 user_text + 模型实际调用的工具，harness 跑与 surfaceeval 相同的路由管线（`RouteWithOptions` + 可选 `simulated_intent` UIC 缓存桩）并评分 `surface_adequacy`（全部实际调用工具均被渲染的轮次 / 有资格轮次），外加与 surfaceeval 相同的面大小/token 基线对照；`TurnSelector` 接口为 provider 接缝（离线实现 `RouterReplaySelector`，live 实现留待未来接入真实模型）。不在目录中的被调工具计为模型错误（计入 `ModelErrorCalls`、剔除 adequacy 分母）。种子转录 3 条（`data/`：全充足 / 一轮工具未渲染 / 条件工具 + simulated_intent），指标确定性可复现。**转录捕获管线已落地两个数据源**（映射规则一致：user 轮开启新 Turn、assistant tool_calls 去重保序累积、目录外调用剔除并计数、不产出 simulated_intent、零轮会话报错、文件 0600、写出前校验）：①`export.go`：corelib/agent ConversationMemory 快照——TUI `~/.maclaw/data/tui_conversation.json` 等共享同一格式——转 Dataset（`ExportFromConversationStore`/`ExportFile`）；②guiapp IM 会话搜索库——`<dataDir>/session_search.db`（SQLite FTS5，`sessions(session_id, timestamp, platform, topic, full_text)`，IM 每轮 post-conversation 由 `persistSessionTranscriptAsync` 写入，`full_text` 为 `session.Serialize` 格式、含 `[tool_call:ID name:NAME]` 块）——经 `session.Deserialize` 走同一映射（`guiapp/taskeval_export.go` `ExportFromIMSessionStore`/`ExportIMFile`；因 guiapp 依赖 corelib/tool，转换器驻留 guiapp 复用 taskeval 公共类型，有等价性测试保证两源统计口径一致）；回放与 provider/live 模式状态同下 |
| 缺口 | — | taskeval 的 provider/live 模式（真实模型调用，`TurnSelector` 接缝已预留）；surfaceeval 与 taskeval 的 harness 验收独立评审（改进计划要求的评审门槛，尚未进行）~~UIC 接入 harness~~（已关闭：样本声明 `simulated_intent` 时 harness 挂接 UIC 缓存桩，见 §3 surfaceeval 行） |
