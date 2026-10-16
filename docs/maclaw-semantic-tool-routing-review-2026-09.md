# MaClaw GUI 语义工具路由：日志复审与改进计划

> 复审日期：2026-09-06（Asia/Shanghai）  
> 范围：MaClaw GUI 的任务范围解析、工具目录、语义计划、动态和静态工具面、执行准入与恢复链路。  
> 数据目录：C:\Users\ma139\.maclaw（只读检查）  
> 目标：工具召回由任务范围决定，避免依赖关键词；同一任务的工具集合可解释、可恢复，不会无故不完整或消失。

本文把日志直接观察到的事实和代码审计得到的根因分开。日志中可能包含凭据或用户内容，本文不复制任何秘密、SQL、令牌或完整提示词。

## 结论

当前代码已经有 semantic planner、Catalog 类型、RouteForScope 和快照校验等部件，但它们还没有成为 GUI 的唯一运行时权威。实际链路仍存在四个独立来源：旧文本路由、关键词驱动的 Skill 和工具发现、动态回调集合、semantic planner。配置项也没有把 Hub 消息智能路由与本地任务范围路由区分开。

这会产生两类用户可见问题：

1. 工具被错误召回。南京天气命中北京、东莞天气 Skill，通用的“了解当前任务”命中 Hello World 和复制路径 Skill。
2. 工具在请求之间不稳定。目录扫描未就绪、能力目录不完整、循环发现重新注入工具、租户字段丢失或快照身份缺失时，模型看到的工具面可能少于计划，或者在恢复时无法证明它来自哪个目录版本。

应先解决单一权威、身份和准入，再调排序或 embedding。只改相似度模型无法修复这些结构性问题。

## 日志和数据证据

### 配置快照

复审时的 C:\Users\ma139\.maclaw\config.json 显示：

| 配置 | 当前值 | 观察 |
|---|---:|---|
| smart_route_enabled | false | 代码注释和安全策略字段将它定义为 Hub smart routing；静态检索没有发现它控制本地 semantic tool-scope 路由。GUI 标签却只写“智能路由”。 |
| skill_evolution_enabled | false | Skill 自动演化关闭，但已存在的本地 Skill 仍参与列表和过滤逻辑，缺少统一的 effective policy 说明。 |
| skill_auto_upload_enabled | false | 上传关闭；目录来源和运行时可见目录没有同一份审计快照。 |
| nl_skills | 1 | 仅看到一个 active 的空 Skill 定义，不能代表完整可用目录。 |
| mcp_servers / local_mcp_servers | 0 / 0 | 当前配置没有声明 MCP；运行时仍保留本地和远程 MCP 分支，缺少“未配置”和“扫描未完成”的区分。 |
| security_policy_mode / sandbox_mode / network_level | relaxed / os / allowlist | 这些策略没有和工具范围计划生成一个不可变的 effective policy 摘要。 |
| knowledge_skill_token_budget | 0 | 零值的含义（禁用、无限或沿用默认值）没有在路由事件中明确记录。 |

C:\Users\ma139\.maclaw\data\tool_version_cache.json 显示 claude、codex 为 installed=true，codebuddy、iflow、kilo、opencode 为 installed=false；配置中的 show_* 选项同时为 true。界面可见偏好与可执行能力因此不一致。

### 路由和循环日志

截至检查时：

| 文件或模式 | 观察 |
|---|---|
| logs\tool_route.log | 48 个 route block。形状为 151/7/127/true 的 26 个、151/7/126/true 的 15 个、151/8/125/true 的 2 个、143/20/108/true 的 2 个、143/21/109/true 的 1 个、83/8/47/false 的 2 个。45 个 block 声明选择 28 个工具，2 个选择 11 个，1 个选择 29 个。 |
| logs\maclaw.log 的 semantic managed turn bypasses legacy strangler | 7 次，说明语义管理路径和旧 strangler 路径在同一版本共存。 |
| logs\maclaw.log 的 exec-router | 10 次，其中多条 full 路由的 tool_budget=0、iteration_budget=0；零预算仍被记录为可执行路由。 |
| logs\maclaw.log 的 skill-domain | 44 次 hid 2/37，Skill 域过滤在目录选择之后发生。 |
| logs\maclaw.log 的 skill-cache | 2 次 scan_not_ready using_config_skills_only external_dirs=0；扫描未就绪时只返回配置 Skill。 |
| logs\maclaw.log 的 tool-surface | 13 次 inject reason=loop-discovery tools=office；循环发现仍能改变当前工具面。 |
| logs\maclaw.log 的 ToolEmbeddingCache | 9 次维度不匹配；出现 768↔256，随后 GetBatch 出现 total=108/126、cached=0、missing=全部的批次。 |

tool_route.log 的例子具有明确的错配信号：10:30:45 起的“南京天气” block 仍带有北京、东莞和通用天气 PDF Skill；10:38 左右的“了解当前任务” block 命中 craft_generate_hello_world_cpp 与 craft_copy_path_to_workspace，且 Skill match score=1.0000。由于旧日志没有 root_task_id、scope_id、plan_id 和 catalog digest，不能仅凭日志断言某个 block 与某次执行一一对应；但它足以证明关键词和触发词路径仍在生产数据中活跃。

### 静态兼容面审计

C:\Users\ma139\.maclaw\data\audit 下 25 个 JSONL 文件共 4615 条记录，解析错误为 0。coding_static_surface 有 308 条：

| shadow_state | 数量 |
|---|---:|
| prepared | 73 |
| not_prepared | 232 |
| catalog_incomplete | 3 |

232 条 not_prepared 记录同时缺少 root_task_id、plan_id、catalog_generation；308 条全部缺少 surface_epoch。catalog_incomplete 的 3 条虽然带有 plan、root、generation，但仍标记 unmet_reasons=catalog_incomplete，并暴露 legacy-only capability。discover_tool 共 299 条，在 2026-08-29、08-30、08-31 分别出现 36、105、108 条，显示发现调用有明显批量重试或补救特征。

### 持久化执行状态

C:\Users\ma139\.maclaw\semantic-routing\semantic-execution.db 的只读统计：

| 表 | 统计 |
|---|---|
| invocation_grants | 1383；consumed 871、issued 395、revoked 117。检查时 395 条 issued 的 expires_at 均已过去，最新约为 2026-09-05T03:04Z。 |
| semantic_host_calls | 871；completed 869、unknown 2；176 条 surface_epoch 为空。 |
| semantic_plan_executions | 871；succeeded 602、awaiting_receipt 147、failed 120、unknown 2。 |
| semantic_route_states | 290；其中 79 条 tenant_id 为空。 |
| semantic_delivery_preparations | 147 条，全部为 prepared；时间范围从 2026-08-19 到 2026-09-05。 |
| semantic_continuity_projection_outbox | 721 条，全部 pending。 |
| semantic_continuity_states | 0 条。 |

这些状态表明计划生成与收据、连续性、过期授权收敛之间存在积压。恢复链路不能只比较 plan digest。

### 数据库操作审计

C:\Users\ma139\.maclaw\database_audit.jsonl 有 72 条记录：

| 动作或结果 | 数量 |
|---|---:|
| query / ok | 17 |
| query / query_error | 9 |
| connect / ok | 6 |
| connect / connection | 5 |
| connect / profile_not_found | 2 |
| execute / ok | 2 |
| execute / approval_rejected | 3 |
| write_table / approval_rejected | 1 |
| propose_profile / needs_secret | 7 |

多组 query_error 后 60 秒内出现 execute 或 write_table，例如审计行 61→64 间隔 5.49 秒、70→71 间隔 5.38 秒；同一连接还出现随后 approval_rejected。当前字段没有 operation_id、attempt、approval_id、parent_action_id，无法证明执行一定经过新的查询和审批。

### Coding runtime 准入

C:\Users\ma139\.maclaw\data\coding_runtime.db 有 29 个 attempt，其中 completed 9、failed 17、blocked 3；workspace_before_probe_failed 事件为 29 次，workspace_after_probe_failed 为 12 次。样本 policy 中 ReadOnly=false、WriteSet.Unknown=true、Claims=null、WorkspaceIsolated=false、FinalDiffGateRequired=false，但仍有完成记录。写能力在基线未知时没有形成硬准入门槛。

## 缺陷清单和修复方向

优先级含义：P0 为在关闭前不能作为受治理执行面使用的缺陷；P1 为会造成漏召回、漂移或不可恢复的缺陷；P2 为性能和体验问题。

| ID | 优先级 | 设置或实现缺陷 | 证据 | 影响 | 修复方向 |
|---|---|---|---|---|---|
| CFG-01 | P0 | smart_route_enabled 语义混用 | config=false；corelib/app_config.go 将其注释为 Hub smart routing；本地路由没有同名读取点 | 用户以为关闭了智能路由，但本地语义路由仍可能运行；无法做可预测的回滚 | 新增 semantic_tool_scope_routing，包含 enabled、mode、legacy_text_route、coverage 要求和预算；现有字段保留为 Hub 消息路由并改名显示。由一个 EffectiveRoutingPolicyResolver 在入口解析。 |
| ROUTE-01 | P0 | semantic planner 与旧 RouteWithOptions/RouteForSession 并存 | im_agent_loop_start.go 在 semanticHandled=false 时进入 prepareAgentLoopTools；im_handler_wiring.go 仍调用 routeSessionToolsWithRanking；旧调用计数存在 | 同一任务可能看到两套工具面，工具重复、消失和排序无法解释 | 每个受治理请求只允许一个 RouteAuthority；旧路由只能 shadow 记录，不得追加或发布工具定义；生产调用计数归零后删除分支。 |
| ROUTE-02 | P0 | 关键词发现仍能影响可见能力 | semantic_tools_search.go 有约 42 项手写 inventory、strings.Contains 和 top-8 截断；Skill 偏好、Skill 文档注入和“继续”粘性也使用触发词 | 同义表达、否定句、城市替换和通用追问会误召回；目录外工具无法完整发现 | 关键词仅作为解释和评估特征；工具查询改为当前 Catalog + Scope 的分页查询，返回 exact id、状态、遗漏原因和 continuation token。 |
| CAT-01 | P0 | 目录未就绪时静默使用部分目录 | scan_not_ready 时 using_config_skills_only；静态面 232 not_prepared、3 catalog_incomplete | 工具从模型面消失，或模型在不知道缺口的情况下重试 | 目录状态必须是 ready、pending、degraded、failed；pending/degraded 只能为明确允许的只读面服务，写和外部效果面必须阻断并返回结构化原因。 |
| CAT-02 | P0 | Catalog digest 与 Plan digest 混为一谈 | ToolCatalogSnapshot 没有专用 digest；ToolPlan.SnapshotDigest 覆盖完整 planner 输入；coding_tool_scope_snapshot.go 的 finalize 已是 no-op，当前静态检索未发现 adoptCodingToolSnapshotDigest 的生产接线 | 无法区分目录变化和任务需求/预算变化；恢复时可能误用旧授权 | 发布器生成 CatalogSnapshot.Digest；planner 另外生成 Plan.Digest；Scope、Surface、Grant、Revision、Audit 同时持有两者和 generation。恢复必须重新加载并比较当前目录。 |
| STATE-01 | P0 | route state 的租户写入和读取不一致 | semantic_route_state_store.go 的 INSERT 和 SELECT 路径省略 tenant_id；coordinator 路径会写入；数据库 290 条中 79 条为空 | 跨租户查找、幂等和失效过滤存在隔离风险 | 统一一个 writer/reader；所有查询强制 tenant 条件；迁移历史空租户记录到 quarantine，禁止新记录使用空值。 |
| EXEC-01 | P0 | workspace 基线失败仍可进入写执行 | 29 次 workspace_before_probe_failed；9 个 attempt 完成；policy 显示 WriteSet.Unknown=true、WorkspaceIsolated=false | 无法证明写操作的目标和差异，审批边界失效 | 写能力 admission 要求非空 baseline、写集合、claims、隔离状态和最终 diff gate；失败即 blocked。只读降级必须显式标记。 |
| SEC-01 | P0 | 审计日志存在明文凭据风险 | data/audit/audit-2026-09-05.jsonl 的 bash 参数含数据库密码（本文不复述） | 路由审计本身泄露凭据，且日志不能安全用于回放或共享 | 立即轮换暴露凭据；统一结构化参数脱敏、命令参数分级、日志访问权限和 secret scanner；审计只保留 fingerprint。 |
| EXEC-03 | P1（恢复要求高时升 P0） | 连续性 outbox 没有持续的生产消费者 | 目前只有按需 Load 时最多 drain 32 的调用，没有服务启动或后台循环；服务只启动 receipt worker；continuity_projection_outbox 721 条全部 pending、continuity_states 为 0 | 普通 route 完成事实不能及时投影，重启或 continuation 可能长期 awaiting_receipt、重复规划或重复副作用 | 将 continuity projector 纳入服务启动和恢复流程；按租户/路线顺序消费，记录 applied_at、失败重试和告警；为 prepared/channel-delivery 明确定义 dispatch 或 terminal 状态，并监控 backlog SLA。 |
| STATE-02 | P1 | 动态能力状态可被半更新读取 | callback 的 matchedSkills、matchedMCPTools、选择标志和 dynamicSelectionText 不是同一不可变对象；部分读写未由同一锁覆盖 | 并发失效时提示、日志和执行可能使用不同集合 | 引入不可变 ToolScopeState 指针，包含 sets、catalog digest、coverage、dependency set、epoch 和 stale reason；单次原子替换，in-flight 请求固定旧状态。 |
| STATE-03 | P1 | 失效事件过滤仍不完整且生产未接线 | 已有 tenant/workspace/MCP server 过滤，但 NotifyCodingCapabilityChanged* 当前只有测试调用；MCP 依赖通过当前 matchedMCPTools 反查；通用入口仍可全局广播 | 能力变更不一定触发 stale/replan；无关任务重规划，真正受影响的任务反而可能漏失效 | planner 声明资源依赖；事件总线按租户、主体、workspace、server、resource version 过滤；所有 task owner 统一注册、注销和恢复。 |
| SURF-01 | P1 | loop-discovery 能改变当前 surface | maclaw.log 13 次 tools=office 注入；round prep 注释允许 re-route/augment legacy | 同一个请求中工具集合发生突变，旧 grant 与新定义不一致 | 当前 surface 发布后只读；发现产生 replan_requested(scope, resource, epoch)，新计划和新 surface 才能加入工具。 |
| API-01 | P1 | RouteForScope 仍是名称数组 API | corelib/tool/router.go 的 allowedNames []string 会静默忽略 unknown name，没有版本、依赖闭包、disabled reason 和 coverage 诊断 | 调用方传入不完整名称时无法发现缺口，工具会无声消失 | 参数改为 ToolScopePlan/CatalogSnapshot + CapabilityNeeds；返回 selected、omitted、missing、dependency closure 和 coverage。名称数组只用于最终渲染。 |
| OBS-01 | P1 | 路由日志缺少可关联身份 | tool_route.log 只有 message、总数、候选/选择和分数；static_surface 中 308 条 surface_epoch 为空 | 无法把误召回与具体计划、租户、执行和恢复关联 | 统一 route_decision 事件，强制 root_task_id、session_id、turn_id、scope_id、plan_id、plan_digest、catalog_digest、generation、surface_epoch、attempt。 |
| EXEC-02 | P0 | 数据库动作没有严格状态机 | 2026-09-05 审计中 query_error 后 5 秒左右出现同 fingerprint 的 execute / ok，随后还出现 approval_rejected；另一组也出现 query_error→execute / ok | 查询失败后仍可能沿用旧上下文写入，审批无法证明对应本次查询 | operation 进入 query_failed、approval_rejected 或 expired 即终止；写执行必须有新 plan、新 approval 和唯一 attempt；同 operation 幂等。 |
| OPS-01 | P1 | 收据、连续性和授权清理积压 | 147 awaiting_receipt、721 pending outbox、0 continuity state、395 issued grant 已过期 | 重启或重试后可能重复投递、悬挂授权或无法继续任务 | 增加 receipt/outbox watchdog、continuity projector、grant expiry sweeper、补偿状态和告警；host call 必须绑定 surface_epoch。 |
| CFG-02 | P1 | 显示偏好与可执行能力脱节 | show_* 全部开启，但工具缓存只有 claude/codex installed=true | 模型或用户看到不存在的工具，产生无效请求 | 将 UI preference 与 catalog admission 分离；未通过版本、契约和健康探针的工具只能显示 unavailable，不能进入 executable set。 |
| ROUTE-03 | P1 | 语义分类器外部依赖抖动会降级成空或极窄路由 | maclaw.log 记录 9 次 classifier failure、15 次 trace_gap、3 次 empty response；10:32:58 的 HTTP 503 后只给出 light 路由，tool_budget=1、iteration_budget=3，连续三轮 empty response | 分类器故障改变工具面并增加无效模型请求；调用者无法判断是分类失败还是工具缺失 | 使用本地确定性 scope fallback（资源、动作、风险和上下文）；分类器只提供 advisory evidence；超时一次即返回结构化 degraded，不重复发空模型请求。 |
| PERF-01 | P2 | embedding 缓存没有稳定代际身份 | 768↔256 维度冲突，多批次全部 miss | 启动后反复重建、延迟升高且排序波动；不能作为路由权威 | cache key 加 model、dimension、schema version、build fingerprint；双代重建和原子切换；目录和范围计划不依赖 embedding 成功。 |

## 目标架构

~~~text
Ingress
  -> Identity + EffectiveRoutingPolicy
  -> Catalog Publisher (immutable snapshot, digest, generation, coverage)
  -> Scope Resolver (objective, resources, actions, risk, outputs)
  -> Tool Planner (dependency closure, deterministic order, Plan.Digest)
  -> Surface Publisher (scope + catalog digest + plan digest + epoch)
  -> Admission / Grant / Approval
  -> Host Execution + Receipt

Capability or policy change
  -> resource-scoped invalidation
  -> mark old state stale
  -> publish a new catalog/plan/surface epoch
~~~

### 配置契约

保留 smart_route_enabled 的 Hub 语义，新增独立配置。示例：

~~~json
{
  "smart_route_enabled": false,
  "semantic_tool_scope_routing": {
    "enabled": true,
    "mode": "scope_only",
    "legacy_text_route": "shadow",
    "require_catalog_coverage": true,
    "allow_degraded_read_only": true,
    "max_selections": 32,
    "max_schema_tokens": 24000,
    "max_iterations": 18
  }
}
~~~

规则：

- mode 取 scope_only、shadow 或 off。受治理的写和外部效果任务不允许由 shadow 产出可执行工具。
- 配置缺失时，受治理任务采用 fail-closed；未治理的兼容入口单独记录 legacy。
- 每次请求把解析后的 policy_version、policy_digest 和来源写入 route_decision；不让下游重复解释原始布尔值。

### 目录和计划契约

目录身份和计划身份必须分开：

~~~json
{
  "catalog_snapshot": {
    "digest": "catalog:sha256:...",
    "generation": 42,
    "registry_version": "registry-v7",
    "coverage": "complete",
    "created_at": "..."
  },
  "scope": {
    "scope_id": "scope:...",
    "scope_version": 3,
    "intent": "database_inspection",
    "resources": ["profile:mysql-..."],
    "allowed_actions": ["connect", "read_schema", "query"],
    "risk_ceiling": "read_only"
  },
  "plan": {
    "plan_id": "plan:...",
    "digest": "plan:sha256:...",
    "catalog_digest": "catalog:sha256:...",
    "selected": ["db.connect", "db.query"],
    "omitted": [
      {"id": "db.execute", "reason": "risk_ceiling_read_only"}
    ],
    "missing": [],
    "dependency_closed": true
  },
  "surface": {
    "surface_epoch": "surface:...",
    "state": "published"
  }
}
~~~

目录快照只由 Catalog Publisher 生成。GUI 回调不得根据 Skill/MCP 数组自行生成 toolsnap；尚未获得目录 digest 时只能返回 pending，不能伪造可恢复的快照身份。

### 工具查询契约

tools_search 以及任何“发现工具”入口都改为目录查询，不再是第二个召回器。输入携带 scope_id、catalog_digest、needs 和 page_token；输出包含：

- exact capability/tool id、版本、契约 digest、当前 surface 状态；
- planned、listed、petitionable、unavailable、omitted 等互斥状态；
- 缺少的依赖、不可用原因、替代能力和下一页 token；
- 查询本身的 decision_id，便于回放。

用户文字可用于展示解释和离线评估，但不能直接改变 selected 集合。

## 按顺序实施

以下顺序以先关闭根本性风险、再扩大覆盖为准。每阶段都要有可回滚开关和结构化指标。

### 第 0 阶段：建立安全边界（P0，1～2 个工作日）

1. 增加 semantic_tool_scope_routing 和 EffectiveRoutingPolicyResolver；把 GUI 标签改成“Hub 消息智能路由”，新增“任务范围工具路由”。
2. 对受治理任务强制要求 root_task_id、tenant_id、principal_id、session_id；缺失即拒绝发布 surface。
3. workspace baseline 失败或 WriteSet.Unknown 时阻断写和外部效果工具；保留显式只读 degraded 分支。
4. 统一日志脱敏、轮换已暴露凭据，补充 policy_digest、decision_id 和拒绝原因。
5. 把零 tool/iteration budget 定义为 invalid configuration，禁止当作 full 可执行路由。

退出条件：新请求中没有未解释的 policy 来源；写任务的未知基线拒绝率为 100%；新审计记录不含秘密。

### 第 1 阶段：目录、身份和恢复链路（P0，2～4 个工作日）

1. 在 ToolCatalog 发布时计算不可变 CatalogSnapshot.Digest；为 coverage 建立 ready、pending、degraded、failed 状态和 last-known-good 指针。
2. 扩展 Scope、Plan、RouteRevision、InvocationScope、Grant、HostCall、Audit 的字段，至少加入 catalog_digest、plan_digest、catalog_generation 和 surface_epoch。
3. 修复 semantic_route_states 的 tenant 写入和读取，迁移空 tenant 记录到 quarantine；新建非空约束和按租户索引。
4. 让 recovery 重新加载当前目录并同时比较 policy、catalog、plan、scope 和 surface epoch；任何漂移都返回 stale/replan。
5. 在所有生产 planner 路径接入 adopt 和验证；没有 digest 不得进入 publish。

退出条件：新产生的 route state 没有空 tenant；新 host call 没有空 surface_epoch；模拟目录漂移时旧 grant 100% 被拒绝。

### 第 2 阶段：单一路由权威和无关键词召回（P0，2～4 个工作日）

1. 建立 RouteAuthority 接口，GUI 受治理请求只调用一次 Scope Resolver → Planner → Surface Publisher。
2. 旧 Route、RouteWithOptions、RouteForSession 仅保留 shadow 统计，不得把定义追加到模型请求；在日志中记录 caller 和 decision_id。
3. 将 semantic_tools_search 改为 Catalog 查询；移除手写 inventory 的召回职责、Contains/top-8 截断和未绑定 scope 的 petition。
4. 将 Skill preference、Skill 文档注入、continuation sticky 改为 planner 的 advisory evidence；继续使用必须有 root、session、phase、expiry、digest 绑定的 ContinuationHandle。
5. 循环发现只发 replan_requested，不修改已发布 surface。

退出条件：受治理请求的 legacy RouteWithOptions 计数为 0；shadow 不发布工具定义；同一 scope 中改变关键词不会改变 plan digest。

### 第 3 阶段：不可变状态和资源失效（P1，2～3 个工作日）

1. 用不可变 ToolScopeState 替换 callback 中分散的数组、标志和缓存。
2. planner 输出依赖资源集合；事件总线按租户、主体、workspace、server、resource version 过滤。
3. 将注册和注销放到统一 task owner，覆盖本地、远程、取消、异常、进程关闭和恢复。
4. 正在执行的请求固定旧 state；能力变化只影响下一轮新 epoch，不撤销正在进行但已准入的只读调用，写调用按 grant 规则处理。

退出条件：竞态测试和 race detector 通过；无关任务不会 stale；受影响任务都有可追踪的 replan 原因。

### 第 4 阶段：执行状态、审批和积压治理（P1，2～3 个工作日）

1. 为 connect、query、execute、write 建立 operation state machine；query_error、approval_rejected、expired grant 会终止当前 operation。
2. 为每次 attempt 生成唯一 operation_id、attempt、approval_id 和 parent_action_id；查询失败后的写执行必须为零。
3. 在 MaClawSrv 启动路径加入 continuity projector；部署 receipt/outbox watchdog、grant expiry sweeper、补偿状态和告警。prepared/channel-delivery 记录必须有明确 dispatch 或 terminal 状态。
4. host call 必须携带 surface_epoch 和 grant fingerprint，重复调用按幂等键返回已有结果。

退出条件：pending outbox 持续下降；过期 issued grant 清零；query_error→execute/write 为零；每次写执行都能关联有效审批；awaiting_receipt 有 SLA 和终止态。

### 第 5 阶段：能力可用性、预算和性能（P1/P2，2～3 个工作日）

1. show_* 只表达 UI 偏好；Catalog admission 通过安装状态、版本、契约和健康探针决定 executable 或 unavailable。
2. 为每个 scope 设置明确的 selections、schema tokens、iterations、wall time 和 effect budget；超限返回结构化 blocked。
3. embedding cache 按 model、dimension、schema、build 分代，采用双代重建；planner 不依赖 embedding 才能给出完整闭包。
4. 所有工具固定按 dependency order、capability id、version 排序，避免候选顺序造成 surface digest 波动。

退出条件：预算缺省或为零的可执行请求为零；不可用工具不会出现在 executable set；缓存代际切换不改变目录身份。

### 第 6 阶段：回放、灰度和删除旧路径（P1，3～5 个工作日）

建立脱敏 replay 集，至少覆盖：

- 北京、南京和其他城市天气；
- “了解当前任务”“继续”等通用追问；
- PPT 白底修改、生成 PDF、文件发送；
- configure nginx on production server；
- 目录扫描未就绪、MCP 健康变化、Skill 版本升级；
- 多租户同名工具、workspace baseline 失败；
- query_error、approval_rejected、重启恢复和收据超时。

先在 shadow 比较 plan digest、selected、omitted 和执行结果，再按租户灰度 scope_only。连续两个发布窗口满足验收指标后，删除旧文本路由和未绑定 scope 的工具发现入口。

## 验收指标

| 指标 | 目标 |
|---|---:|
| 受治理请求的 legacy RouteWithOptions/RouteForSession 执行调用 | 0 |
| shadow 路径发布或追加模型工具定义 | 0 |
| 新 route decision 具备 root_task、scope、plan、plan digest、catalog digest、generation、surface epoch | 100% |
| 新 surface 的 coverage 状态 | 全部为 complete，或明确 pending/degraded 并遵守只读门禁 |
| 目录漂移后旧 grant 被拒绝 | 100% |
| 新 semantic_route_states 的空 tenant_id | 0 |
| 新 semantic_host_calls 的空 surface_epoch | 0 |
| query_error 后同 operation 的 execute/write | 0 |
| 写任务在 baseline 或 WriteSet 未知时进入执行 | 0 |
| 过期 issued grant（清理任务运行后） | 0 |
| continuity/outbox pending | 持续下降并有 SLA 告警 |
| 单任务重复 discover_tool | 同一 needs 最多 1 次分页查询，后续复用 decision |
| 关键词替换但 scope、资源、策略不变时的 plan digest 变化 | 0 |
| 工具依赖闭包不完整的计划 | 0 |

## 验证命令和审计注意事项

以下检查可在脱敏副本上重复，不应把原始命令参数、令牌或凭据输出到终端：

~~~text
rg -n "SmartRouteEnabled|RouteWithOptions|RouteForScope|surface_epoch|tenant_id" corelib guiapp tui
rg -n "\[exec-router\]|\[tool-surface\]|\[skill-cache\]|\[ToolEmbeddingCache\]" C:\Users\ma139\.maclaw\logs
rg -n "coding_static_surface|discover_tool" C:\Users\ma139\.maclaw\data\audit
~~~

旧 tool_route.log 缺少关联身份，因此在补齐结构化事件前，只能用于发现共存、错配和重试模式，不能用于计算单请求的精确漏召回率。静态审计记录的是兼容面观察，也不能代替每次模型请求的 surface 事件。

## 当前代码状态判定（2026-09-07 复审）

本节按当前工作树的实际代码和定向测试更新。此前把所有部件列为“仍未闭环”的描述已经过时；同时，定向测试通过不等于生产链路已经切换。

已落地并有定向回归的部件：

- `semantic_tool_scope_routing` 配置、effective policy 解析和 GUI 配置映射；
- `ToolScopePlan`/`RouteForScopePlan` 的依赖闭包、缺失依赖诊断、稳定排序和预算门禁；
- `CatalogSnapshot.Digest` 与 `ToolPlan` digest 分离，发布时校验目录 digest；
- `InvocationScope.ToolSnapshotID`、旧 payload 的 HMAC 验证，以及只由可信 route/model-surface reader 调用的 `CanonicalInvocationScope`/`ValidateWithCanonicalScope` 迁移入口；
- `tools_search` 的 scope/catalog identity 校验、查询文字不参与集合选择、绑定分页 token、严格参数拒绝；静态 inventory 仍只是可用性说明，不能授予执行权；
- Coding 静态 shadow plan、快照失效围栏、local/remote scope admission 入口、continuity projector、数据库 query/execute 状态门禁和 workspace 最终写入门禁。

仍有生产风险或需要收口的部件：

- GUI 受治理入口尚未完全收敛为 `RouteAuthority`；旧兼容路由仍存在，不能据此宣称 legacy 调用为零；
- `gui` 与 `guiapp` 的动态生命周期 gate 已统一为 fail-closed：`codingDynamicAliasesMayMaterialize()` 恒为 false，生产 `newQualifiedCodingBoundDynamicRequestLifecycleRelay` 返回 nil；`guiapp` 的 qualification override 只用于 hermetic E3/E4 rehearsal，不能构成发布接线。动态 Skill/MCP 仍需等真实 host scope plan、binding admission、durable surface 和 coordinator 绑定完成后才可开启。
- `ToolScopeState` 的不可变快照、完整资源级失效、surface/receipt/outbox/grant 清理和结构化 route telemetry 尚未达到验收指标；
- 旧 route/artifact 行的空 `ToolSnapshotID` 只在可信参考 scope 可用时补齐。当前已补上物化记录从签名 grant 绑定 route snapshot、模型请求 surface 从 route canonicalize snapshot，以及 `PrepareDeliveryAndComplete` 的结果/状态幂等重试；但 route artifact 的 parent source snapshot 仍受旧 schema 限制，多租户 route key 和所有恢复入口还需要继续做交叉验证。

因此当前版本应标记为“范围路由和迁移兼容部件已实现，生产 scope-only 执行面仍未完成切换”。在解决上述唯一权威入口、状态失效和积压治理前，不应扩大受治理语义路由覆盖面。

### 12.1 tools_search 状态误判（2026-09-07）

复审发现，目录状态函数曾把“同一 capability 的任意别名”标成“已列入本轮计划”。例如计划选择 read_file 时，list_directory 也会被标成等待前置步骤；但该别名并不会进入当前 surface，模型会继续等待一个永远不会出现的工具，形成日志中“工具消失/反复发现”的表象。修复后只有 selection.AdapterName 与目录项完全相同才可标记为 planned；同 capability 的其他别名保持“不可用”或“可请愿”，不会被误导为自动出现。GUI 与 GUIApp 均已同步，并加入回归测试。

### 12.2 host-call 重放条件过宽（2026-09-07）

复审动态持久化桥接时发现，协调器在发现同一 `protocol + connection + call + surface_epoch` 已有记录后，只要新 grant 被判定为 repeat sibling，就会沿用旧 `grant_fingerprint` 并返回旧状态；此前没有同时要求 `request_digest` 相同。结果是同一可信 host-call ID 携带另一组参数时可能被误判为原调用重放，返回第一次的 `parameter_schema_invalid`，而不是拒绝 `host_call_conflict`。这会掩盖调用方协议错误，也可能让上层把不同参数当作已处理。

修复已收紧 `Admit` 与 `Reject`：历史 grant fingerprint 仅在 canonical request digest 也完全相同的情况下允许兼容重放；digest 不同即保持冲突，不能借 repeat sibling 绕过 host-call 幂等键。新增并通过动态固定桥接回归测试，覆盖“相同参数可重放、不同参数复用 call ID 必须冲突”。
### 12.3 动态 adapter 名与渲染名错位（2026-09-07）

跨层复核发现，动态 MCP/Skill selection 的 `AdapterName` 是内部绑定身份，而 renderer 对动态 provider 暴露的是 opaque grant token（通常为 `invoke_*`）。`tools_search` 原先把 `surface.plan.Selections[].AdapterName` 直接放进目录，模型看到的名称因此无法在当前工具面调用；重启或重试时还会表现为工具消失。修复后静态 adapter 通过 `SemanticModelFunctionName`，动态条目只从当前/已退休 grant 的真实渲染键加入目录；没有 materialized grant 的内部动态 adapter 不再伪装成可发现工具。GUI 与 GUIApp 已同步，并加入动态 grant 展示回归测试。
### 12.4 动态 scope 校验必须使用 adapter-keyed catalog（2026-09-07）

动态 Skill/MCP 的 trusted definition 为了避免泄露 provider 身份，`function.name` 固定为 `dynamic_provider`；真正的 resolver 身份在 `ProviderSpec.AdapterName` 和 definitions map key 中。范围校验如果把 definition 的 `function.name` 当作 adapter，就会把已选的 `dynamic_mcp_*`/`dynamic_skill_*` 判为缺失，产生 `scope_plan_incomplete`，从而在工具已绑定时错误地清空整面。新增 `RouteForScopePlanByAdapter`，以 host-owned map key 做闭包校验，同时保留原始 definition 供后续 renderer 生成 opaque grant 名；GUI/GUIApp 的 publication validation 已切换到该入口，并加入 placeholder definition 回归测试。