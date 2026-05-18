# TencentDB Agent Memory 借鉴方案

本文记录从 Tencent/TencentDB-Agent-Memory 中可借鉴到 MaClaw 的设计点，以及适合本仓库的落地顺序。参考仓库版本：`5736acc8711b843756a2c0f8d86e939b28c5e02d`。

## 结论

不建议整套迁移。TencentDB-Agent-Memory 的 OpenClaw/Hermes 插件形态、运行时 patch、TypeScript 存储实现都和 MaClaw 不同；真正值得吸收的是三类工程思想：

1. 短期上下文做成“原文外置 + 符号索引”。
2. 长期记忆做成 L0/L1/L2/L3 分层，而不是平铺向量堆。
3. 召回结果必须可追溯到原始证据，压缩不应切断回查路径。

## 可借鉴点

### 1. 原文外置，召回只注入索引

TencentDB-Agent-Memory 的 context offload 会把大段工具结果写入 `refs/`，再把任务进展压缩成 Mermaid/MMD 文件放在上下文里。上下文中的节点携带 `node_id` 或文件名，LLM 需要细节时再回查原始文件。

MaClaw 当前已有 `task_artifact` 和 `SourceURL`，但普通 `memory(action=recall)` 输出还没有稳定地提示“这条记忆的完整证据在哪”。第一步应让召回输出显示来源路径和回查方式，形成最小闭环：摘要负责轻量注入，原文负责事实核验。

### 2. 稳定上下文与动态召回分离

TencentDB-Agent-Memory 将 Persona、Scene Navigation、Memory Tool Guide 放到稳定系统上下文，把每轮 L1 相关记忆放到动态用户上下文前缀。这样稳定内容不随每轮查询变动，有利于 prompt cache，也减少系统提示被动态召回污染。

MaClaw GUI 已经有 frozen memory snapshot 和 per-message proactive recall，可以继续沿这个方向收敛：

- 静态区：用户画像、记忆索引、长期工具说明。
- 动态区：本轮 `RecallDynamic` 结果、知识库 auto recall、derived facts。

### 3. L0/L1/L2/L3 分层

TencentDB-Agent-Memory 的长期记忆链路是：

- L0 Conversation：原始对话。
- L1 Atom：原子事实。
- L2 Scene：场景块 Markdown。
- L3 Persona：用户画像。

MaClaw 已有 `conversation_summary`、`task_artifact`、主题层、Profile/Temporal Tree 等机制，但命名和回查路径还不够统一。下一步不必重写存储，可以先在文档和调试输出中统一分层语义。

### 4. 事件驱动的后台管线

参考仓库不是单纯固定周期跑批，而是结合 warm-up、idle debounce、checkpoint recovery、串行队列和后台任务 drain。MaClaw 当前已有大量后台维护能力，适合借鉴其触发策略，减少“写入后要等长周期才变好”的延迟。

### 5. 中文检索的工程细节

参考仓库在 SQLite FTS 查询侧和写入侧都做了中文分词，并用 RRF 合并 FTS 与向量结果。MaClaw 的召回融合已经较完整，因此这里优先借鉴“查询清洗、降级可观测、来源输出”，而不是替换 RecallDynamic。

## 落地计划

### Phase A：可追溯召回输出

状态：已实施第一版。

目标：`memory(action=recall)` 返回每条记忆时，附带 `source_type`、`source_url`，对 `task_artifact` 文件路径给出 `read_file` 回查提示。

收益：不增加召回 token 注入量，却让 LLM 能从摘要 drill down 到完整产物或证据。

已落地：

- 显式记忆工具召回使用 `FormatRecallEntryForTool` 输出来源和回查提示。
- 自动 prompt 召回使用 `FormatRecallEntryForPrompt` 输出短来源提示，避免把全文塞进系统提示。

### Phase B：短期上下文外置层

状态：已实现三条主路径，覆盖自动裁剪的长 assistant 产物、`compress_context` 主动压缩快照，以及 workflow 阶段产物。

目标：为长工具结果/阶段产物建立类似 `refs/` 的轻量文件层，记忆条目只保留摘要、来源路径、trace id。

已落地：

- `gui/im_conversation_trim.go` 不再在调用 memory sink 前截断长 assistant 文本，保证外置层拿到完整原文。
- `gui/im_history_persistence.go` 将被裁剪的长文本写入 `memory_refs/conversation_trim/<user>/<yyyy-mm>/...md`，记忆条目只保存 800 rune 预览。
- 自动保存的 `task_artifact` 设置 `SourceType=conversation_trim_ref` 和 `SourceURL=<ref file>`，可被 Phase A 的 recall 格式提示 drill down。
- `gui/im_tool_compress_context.go` 将 `compress_context` 的工作状态快照写入 `memory_refs/context_checkpoint/<user>/<yyyy-mm>/...md`，记忆条目设置 `SourceType=context_checkpoint_ref`。

后续接入点：
- 可继续把更多后台维护点迁到 Phase D 的 idle debounce 触发器。

### Phase C：场景导航层

状态：已实现第一版确定性 scene index。

目标：在现有主题层/项目上下文之上生成一个人可读的 scene index，用于提示“有哪些项目/任务/决策块可回查”。

已落地：

- `corelib/memory/scene_index.go` 基于现有记忆条目按项目路径聚合 scene，不调用 LLM、不改写状态。
- 每个 scene 暴露项目路径、workflow 类型、最近产物、SourceURL 列表、最近预览，保持“导航轻量、证据可回查”。
- `memory(action=scenes)` 可直接返回 scene/task index，便于 agent 在长任务中先看导航图，再按 SourceURL drill down。
- 系统 prompt 的记忆区会注入一个很短的 `[Scene Index]` 导航块，展示最近项目和阶段产物来源，避免每轮只靠关键词召回碰运气。

约束：先用确定性规则聚合，不让 LLM 直接写核心状态文件。

### Phase D：管线触发优化

状态：已实现第一刀，给后台记忆管线增加事件驱动的 idle debounce 触发。

目标：把部分 6 小时后台维护改成“前几轮快速 warm-up + 空闲 flush + 后台串行任务”。

已落地：

- `corelib/memory/pipeline.go` 增加 `TriggerSoon(delay)`，重复触发会重置 timer，把一串记忆写入合并成一次后台维护。
- `RunOnce` 增加串行保护，避免定时维护和事件触发维护重叠执行。
- GUI 的 ProjectIndex 变更回调在刷新任务侧栏事件之外，会以 2 分钟 idle debounce 触发一次 memory pipeline，缩短“写入后要等 6 小时才整理”的延迟。
- 工作流阶段产物、自动历史裁剪外置引用、`compress_context` 工作状态快照、手动创建任务、任务沉淀、任务切换归档摘要、`memory(action=save)` 写入成功后，也会以 45 秒 idle debounce 触发同一条 memory pipeline，让新证据更快进入 scene/persona/压缩维护链路。

注意：保持现有存储和索引更新契约，不引入运行时 patch。

### Phase E：任务侧栏证据导航

状态：已实现第一版，把 scene index 从 agent 内部导航接到 GUI 任务搜索结果。

目标：任务列表不只显示项目名和摘要，还能暴露最近阶段产物与 SourceURL，让用户从侧栏就知道“这个任务的证据在哪”。

已落地：

- `gui/app_project_search.go` 的 `ProjectSearchResult` 增加 `source_urls` 与 `recent_artifacts`，由 `memory.Store.SceneIndex` 聚合补齐。
- 新增 `GetProjectScene(projectPath)` Wails 绑定，返回单个项目的 scene 详情，供后续任务详情/证据面板直接消费。
- `gui/frontend/src/components/ai/ProjectSceneDetailPanel.tsx` 提供任务搜索内联证据详情面板，可从任务行查看最近产物并打开来源。
- `gui/project_context.go` 的 `LoadProjectContext` 同步输出最近产物来源，让新打开的 Project Tab 初始上下文也能指向外置证据，并对本地 SourceURL 标注 `full: read_file`。
- `gui/app_project_search.go` 的 `buildProjectTabContextMessage` 也会写入“最近产物来源”，覆盖从任务搜索直接创建/恢复 Project Tab 的后端上下文路径，并带结构化 `source_hint=full: read_file` / 文本 `read_file` 回查提示。
- `gui/frontend/src/components/ai/ProjectSearchPanel.tsx` 在任务行显示最近产物摘要，把完整 SourceURL 放进 tooltip，并提供打开来源的小按钮，避免把长文档塞进任务列表。
- `gui/frontend/src/components/ai/useProjectContextLoader.ts` 在 Project Tab 系统上下文中优先使用后端 `source_hint`，对本地来源追加 `full: read_file`，让模型知道下一步怎么回查全文。
- `gui/app_project_search_scene_test.go` 覆盖 memory_refs 来源、项目路径 tag 和任务搜索结果之间的连接。
- `gui/frontend/src/components/ai/ProjectSearchIcon.tsx` 将任务搜索行的证据详情、继续任务、打开来源入口收敛为 SVG 图标按钮，避免文本按钮挤占任务列表空间。
- `gui/frontend/src/components/ai/__tests__/ProjectSearchPanel.test.tsx` 增加 scene 详情加载与打开产物来源的前端回归测试。
- `gui/frontend/src/components/layout/SidebarRecentTasks.tsx` 与 `SidebarTaskEvidencePanel.tsx` 将同一份 `GetProjectScene` 证据导航扩展到主侧栏最近任务列表，可直接查看并打开最近产物来源。

### Phase F：中文 lexical fallback

状态：已实现第一刀，保留现有 gse 中文分词，同时在 BM25 tokenization 层追加轻量 CJK 2-3 字 ngram fallback。

目标：不引入新的重依赖、不改存储格式，让中文短语在分词词典切分不理想时仍能通过 BM25/关键词召回命中。

已落地：

- `corelib/bm25/bm25.go` 的 `Tokenize` 会先使用 gse 分词，再对连续 CJK 字符串追加去重后的 bigram/trigram fallback token。
- fallback 仅对 3-24 字的连续 CJK run 生效，避免长文整段生成过多碎片。
- `corelib/bm25/bm25_test.go` 覆盖中文短语 fallback token 与 substring BM25 命中。
- `corelib/memory/recall_trace.go` 暴露 `LastRecallTrace()`，记录最近一次 `RecallDynamic` 的 query、实体、语义 token、BM25 token、BM25/vector/semantic 命中数、结果 ID 与 SourceType 分布，方便后续评测中文召回路径。
- `memory(action=recall, debug=true)` 和 `memory(action=trace)` 已接入 recall trace 输出，能直接查看中文 BM25 fallback token、各召回通道命中数、候选数、结果 ID 和 SourceType 分布。

约束：这只服务 BM25/关键词召回，不改变 `ExpandQuery` 的语义 token 规则；`ExpandQuery.QueryTokens` 仍保持“人类可读的语义单位”，避免把碎片 tag 注入 prompt 或项目导航。

### Phase G：SQLite 持久化

状态：已接入第一版，GUI 默认使用 SQLite WAL 作为长期记忆事实存储，并保留内存 BM25/vector/scene/theme 索引用于高速召回。
目标：把 JSON/partition 文件从事实存储路径迁移到 SQLite，降低频繁写入时的整文件重写成本，并为后续跨实例 sync、增量诊断和 FTS 预筛选打基础。
已落地：

- GUI 初始化记忆时使用 `NewStoreWithMode(baseDir, StoreModeSQLite)`，首次启动会把 legacy `memories.json` 或 partition 文件迁移到 `memory.db`；SQLite 打开失败时回退到 JSON mode。
- `Store` 的普通 `Save/Update/Delete/TouchAccess` 路径已接入 backend 持久化，删除和 LRU 淘汰会同步软删除 SQLite 行。
- SQLite store 仍启动 Store 级 debounce flush，用于批量维护任务修改多条 entry 后统一落库；GUI ProjectIndex 变更会统一触发前端刷新并通过 `TriggerSoon` 做 idle debounce memory maintenance；召回热路径继续走内存索引，不把 BM25/vector 查询直接搬进 SQL。
- 新增 SQLite store 级 save/update/delete reload 回归测试，确认普通写入不再只停留在内存层；GUI `ensureMemoryStore` 也有回归测试确认默认会创建 `memory.db`。
- SQLite backend 增加 FTS5/trigram 文本预筛选辅助接口 `SearchTextIDs`，并提供 LIKE fallback；当前先作为后续大库预筛选的地基，不直接替换内存 BM25 排序。
- 新增 owner/category/project/time 过滤下推 benchmark 与 `SearchTextIDsFiltered` 实验接口；当前强过滤场景依然是内存 BM25 + 本地过滤更快：2k 下约 `1.04ms/op` vs SQLite 过滤预筛 `4.06ms/op`，10k 下约 `6.51ms/op` vs `18.25ms/op`，50k 下约 `38.28ms/op` vs `115.98ms/op`。
- 新增 BM25 `ScoreSubset` 与 SQLite FTS 预筛 + BM25 子集重排 benchmark；BM25 层缓存 document frequency，避免每次查询重复扫描 DF。文本-only 当前基准：2k 记忆下纯内存 BM25 约 `449µs/op`、SQLite FTS 预筛 + 子集重排约 `1.34ms/op`；10k 下约 `2.05ms/op` vs `9.51ms/op`；50k 下约 `12.06ms/op` vs `54.64ms/op`。因此暂不默认接入 RecallDynamic 热路径。

约束：当前 SQLite 负责事实存储与增量版本，不替代内存召回索引；2k/10k/50k 的文本-only 与 owner/category/project/time 过滤下推基准均显示内存 BM25 更快，因此不把 SQL 预筛选接入 RecallDynamic 候选集。后续若要重评，应先改变数据分布或引入真正选择性很强的结构化字段索引。

### Phase H：经验学习审计入记忆
状态：已接入第一刀。远程工具 session 的 experience extractor 在完成或失败后，不只写入进程内 audit trail，还会把 extraction audit 作为 `project_knowledge` 记忆保存。
目标：让“学到了什么、为什么没学成、哪些 skill 被注册/更新/跳过”进入长期记忆与 governance 视图，而不是停留在临时日志里。
已落地：

- `ExperienceExtractor` 绑定 GUI memory store，`recordAudit/recordAuditError` 会同步保存 `experience-audit-{sessionID}` 记忆。
- 记忆内容保留 session、tool、project、status、候选/注册/更新/跳过计数、upserted skill 名称、错误与前 8 条 decision，并显式写入 safety boundary。
- 记忆带 `tool_memory`、`experience_extraction`、`status:*`、`tool:*` 与项目路径标签，`source_url=experience://extraction/{sessionID}`，可进入 experience governance / trace detail 视图。
- 增加回归测试确认 extraction audit 会落入长期记忆，并在 `buildExperienceLearningSnapshot` 中作为 tool-memory trace 聚合。
约束：这仍是“审计证据入记忆”，不是自动安装技能、自动改路由或自动执行 follow-up；后续把经验提炼成可行动作，仍走现有 review / draft / record_draft_review 闭环。
### Phase I：失败复盘/自适应重试入记忆

状态：已接入第一版。自适应重试模块在记录失败分类与恢复决策时，会把“哪类工具/LLM 调用失败、累计失败次数、当轮采取 retry/fix/skip/disable 中的哪一种策略”写入长期记忆。
目标：让失败恢复经验不只停留在单次 trajectory log 中，而是能进入后续 recall 与 experience governance：模型可以看到过去哪些工具参数容易错、哪些错误应先修正参数、哪些 transient/network 错误只适合有限重试。
已落地：

- `AdaptiveRetry` 可绑定 GUI memory store，`RecordFailure` 会保存或更新 `adaptive-retry-{tool}-{category}` 记忆。
- 记忆使用 `project_knowledge` 分类，带 `tool_recovery_pattern`、`adaptive_retry`、`tool:*`、`category:*`、`action:*` 标签，`source_type=tool_usage`，`source_url=experience://adaptive_retry/{tool}/{category}`。
- 内容保留 tool、failure category、failure count、首次/最近观测时间、decision、attempt、delay/error context、disabled 状态，并显式写入 safety boundary：这只是失败恢复证据，不授权自动执行、改路由、改凭据或安装技能。首次观测时间会在后续更新中保留，用来区分短时抖动和跨会话长期不可用。
- `SetMemoryStore` / `SetAdaptiveRetry` / agent loop recorder bundle 已补齐 wiring，确保 GUI 主记忆库和自适应重试实例保持同一存储入口。
- Agent loop 的工具执行失败路径已接入 adaptive retry：参数解析/缺参/校验失败归为 `args`，审批/防火墙/工作流策略拒绝归为 `permission`，未知工具/截断阻断归为 `logic`，其他 handler-reported 失败继续按错误文本分类。
- 同一 tool/category 的连续失败达到 `adaptiveRetryReviewThreshold=3`、工具被禁用或决策为 `disable` 时，会给该失败复盘记忆打上 `review_required` 和 `failure_count:*`，在 experience governance 中作为 `tool_recovery_pattern` review signal 优先展示；不同 category 的失败会打断当前连续计数，且 transient/network 在仍处于 `retry` 决策时不会触发 review，只有进入 `skip`/`disable` 等终态或工具被禁用才进入审核队列，避免 transient/network 抖动和参数错误相互污染。审核后会记录 `reviewed_failure_count:*`，后续同类连续失败数未再增加一个阈值窗口前保持已审核状态，避免同一噪声反复打扰。
- `ReviewExperienceTrace` 已支持 `tool_recovery` 审核种类，人工 approve/reject/defer 只写入审核审计和状态标签，不会自动改路由、授权工具、安装技能或执行 follow-up；后续同一失败记忆被新证据更新时会保留既有 `Experience review record` 审计段，避免“已审核为什么”的上下文被覆盖。
- `GetExperienceLearningSnapshot` / `governance_summary` 现在会聚合 `tool_recovery_summaries` / `tool_recovery_governance`，按 tool/category 暴露 failure count、review_required、disabled、首次/最近观测时间，并新增 `experience_learning(action=tool_recovery)` 只读诊断入口，同时接受 `inspect_tool_recovery_governance` / `tool_recovery_governance` / `recovery_governance` 别名，支持 `tool`、`category`、`provider`、`model`、`wire_api`、`review_only`、`limit` 过滤。
- `experience_learning` 已补入内置工具注册表，工具说明明确 `snapshot -> governance_summary`、`routing_self_evolution -> routing_signals/tool_recovery_governance`、`trace_details` 的只读边界，并在 schema 中暴露 `tool`、`category`、`provider`、`model`、`wire_api`、`review_only`、`limit` 等恢复治理过滤参数。
- LLM 请求失败的 adaptive retry 证据现在会附带 provider/model/wire_api 元数据；`tool_recovery_summaries`、`tool_recovery_governance` 和 `experience_learning(action=tool_recovery)` 会暴露 provider/model/wire_api 计数，并支持按 `provider`、`model`、`wire_api` 过滤，方便把失败窗口从 tool/category 扩展到 provider/tool/category/wire_api 维度。
- 增加回归测试确认 adaptive retry 记忆会创建、同一 tool/category 会更新而不是重复创建、agent-loop 工具失败会落入同一记忆入口、重复失败会进入 review 队列，并会在 `buildExperienceLearningSnapshot` 中作为 tool-memory / tool-recovery trace 聚合。

约束：这一步只保存“失败复盘证据”，不把失败恢复规则直接升级为自动策略。后续如果要把它变成可执行建议，应继续走 review / draft / record_draft_review 闭环，避免历史失败样本直接改变执行权限。

## 当前验证

本轮继续补齐了防退化验证，重点覆盖 SQLite 默认存储、ProjectIndex 事件触发的 idle debounce 记忆维护、工具恢复治理、前端经验学习视图以及 AI 助手拆分约束。已通过的验证包括：

- `go test ./gui -run "Test(ExperienceLearningToolRecoveryGovernanceSummarizesAdaptiveRetry|ExperienceGovernanceSummaryRecommendsToolRecoveryInspection|EnsureMemoryStoreDefaultsToSQLite|ProjectIndexChangeTriggersDebouncedMemoryPipeline|SearchProjectsIncludesSceneSourceArtifacts|BuildProjectTabContextMessageIncludesSceneSources)" -count=1`。
- `go test ./corelib/memory -run "TestPipelineTriggerSoonDebouncesBursts|TestNewStoreWithMode" -count=1`。
- `go test ./corelib/security -run "TestFirewall_StandardMode_AllowsRmRfWithoutConfirmationChannel|TestFirewall_DeveloperMode_BypassesCheck|TestPolicyEngine" -count=1`。
- `node scripts/check-encoding.mjs corelib gui docs/tencentdb-agent-memory-inspired-improvements.md --strict-mojibake`。
- 前端 `npx vitest --run src/components/ai/__tests__/useAIAssistant.test.ts src/components/ai/__tests__/aiAssistantProjectTabState.test.ts src/components/remote/__tests__/MemoryExperienceLearningPanel.test.tsx`，3 个文件 83 个测试通过。
- 前端 `npx tsc --noEmit` 与 `npm run build` 通过；构建仅保留既有的大 chunk 与动态/静态 import 共存警告。
- UI 拆分 guard 通过：`AIAssistantPanel.tsx` 790 行，`aiAssistantPanelTypes.ts` 119 行，且 `AIAssistantPanel.tsx` 保持从 `./aiAssistantPanelTypes` 导入类型。

## 不采用的部分

- 不采用 `postinstall` patch 宿主运行时的方式。
- 不让 LLM 直接拥有任意核心状态文件写权限。
- 不把 Go 端现有 RecallDynamic 替换成 TypeScript SQLite 实现。
- 不在当前阶段引入远端 Tencent VectorDB 作为强依赖。

## 当前进展

Phase A 已完成召回来源提示；Phase B 已完成三类外置引用；Phase C 已接入 scene/task index；Phase D 已有 idle debounce 事件触发；Phase E 已把 scene 证据导航接入 GUI 任务搜索结果、任务搜索内联详情面板、Project Tab 前端加载上下文、后端创建上下文和独立 `GetProjectScene` 详情接口，并给最近产物来源加了打开入口；前端任务搜索面板已补齐图标化入口和 scene 详情回归测试；Phase D 已继续把工作流产物、历史裁剪引用、压缩快照、手动任务、任务沉淀、任务归档摘要和显式 memory save 接到同一 idle debounce 触发器。主侧栏最近任务列表也已接入 `GetProjectScene` 内联证据面板；Phase F 已给 BM25 增加轻量 CJK ngram fallback，并把 `LastRecallTrace()` 接到 `memory(action=recall, debug=true)` 与 `memory(action=trace)` 召回诊断，能看到中文 query 的 BM25 token、命中通道和来源分布；Phase G 已将 GUI 长期记忆默认切到 SQLite WAL 事实存储，并保留内存索引负责高速召回；本轮补了 GUI 默认 `memory.db` 创建回归、ProjectIndex -> `TriggerSoon` 维护触发说明和严格编码检查，避免 SQLite/事件触发链路后续退化；Phase H 已把远程 session experience extraction 的审计结果写入长期记忆，让“学到了什么/为什么没学成”可被后续 governance 和 recall 看见。Phase I 已继续把自适应重试的失败分类与恢复决策写入长期记忆，作为 `tool_recovery_pattern` / `tool_usage` 证据进入 governance 与后续 recall。本轮已把 agent-loop 工具执行失败路径补进 `RecordFailure` 写入点，给重复失败复盘增加了 review_required 阈值与 `tool_recovery` 人工审核闭环，并加入按 tool/category 连续失败计数推进的审核冷却窗口；后续更新同一失败记忆时会保留首次观测时间和审核审计，便于区分短时服务抖动、跨会话长期不可用以及“已审过但又复发”的噪声。本轮已继续把 provider/model/wire_api 信息接入失败窗口，让治理汇总能从 tool/category 扩展到 provider/tool/category/wire_api，并补齐 `experience_learning(action=tool_recovery, provider, model, wire_api)` 及 `inspect_tool_recovery_governance` / `tool_recovery_governance` / `recovery_governance` 别名的只读查询与回归测试；当治理汇总只发现恢复证据但暂无审核队列时，会推荐 `inspect_tool_recovery_governance` / `experience_learning(action=tool_recovery)`，且沿用包含“不得重试执行、不得改凭据”的恢复治理边界；推荐调用的 `recommended_focus_context` 也会带上 provider/model/wire_api/category/tool 计数和优先 trace，便于从治理摘要直接定位失败窗口；前端经验学习面板也会在治理摘要中展示失败窗口/待审/禁用计数，并把 `inspect_tool_recovery_governance` 的 Focus 操作落到工具证据视图和优先 trace。
