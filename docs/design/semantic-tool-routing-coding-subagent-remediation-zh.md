# CodingSubAgent 静态工具带收口：复审结论与改进方案

> 状态：提案，作为 `semantic-tool-routing-design-zh.md` §11.57 / §11.104 与 `semantic-tool-routing-design-review-zh.md` 的后续实施切片。  
> 决策：不恢复 Coding Skill/MCP 动态 alias；先把本地与远程 CodingSubAgent 的**静态**工具选择、渲染、调用和生命周期迁入同一条语义计划链。

## 1. 复审结论

通用 IM 共享回合已经具备“每个模型请求由当前 immutable plan 完整替换工具面”的底座，也已经补齐 fresh ingress 对旧 surface/grant 的 durable fence。CodingSubAgent 与 RemoteCodingSubAgent 没有继承这条性质：它们仍以本地 `BuildTools` 组装静态工具数组、按工具名过滤和按名字分派执行。

这不是只剩动态 Skill/MCP 未完成的小缺口，而是 C-2 的独立旁路。它会使 Coding 路径继续拥有第二个 planner，并且在多轮工作时把早期任务文字推导出的工具面缓存为后续模型请求的事实来源。

当前已核实的边界如下。

| 位置 | 现状 | 为什么违背统一路由 |
| --- | --- | --- |
| `codingSubAgentCallbacks.BuildTools` | 从 registry + 固定工具表拼接，再缓存 `cachedTools` | cache 保存的是已渲染 definitions，而不是可复核的 catalog/plan；之后每轮只复制该数组 |
| `remoteCodingCallbacks.BuildTools` | 单独构造 SSH/static 工具表，再做 inquiry/operational 过滤 | local 与 remote 的能力选择规则分叉，缺少共同的 `ToolPlan`、selection 与 explain trace |
| `codingTaskNeedsLocalization` / `codingTaskNeedsExternalResearch` | 从任务文本作词法判断，决定是否暴露 web research helpers | 文本直接改变工具可见性；同一执行 posture 可因措辞不同得到不同网络能力 |
| `filter*InquiryTools` / `filter*OperationalTools` | 以工具名 allow-list 裁剪 | task kind 与工具名在 callback 中重新做授权决定，而非作为 planner facts/constraints |
| `ExecuteToolStructured` / `executeRemoteTool` | 模型给出的名称进入 switch/dispatcher | definition 名称仍是执行权入口；没有 selection/grant/host-call journal 绑定 |
| `codingDynamicAliasesMayMaterialize() == false` | Skill/MCP alias fail-closed | 这是正确的 P0 保护，不能为修复静态工具带而放开 |

因此“多轮后工具不完整”在 Coding 路径上有两个根因：

1. **缓存单位错误。** 缓存了请求无关的渲染结果，未缓存可重规划的 catalog snapshot、事实和 plan revision；工具面不能随 phase、已完成事实、预算 sibling 或环境变化被完整替换。
2. **授权位置错误。** task wording、tool-name filter、role filter 和 dispatcher 分别决定一部分工具面；它们绕开了 `ToolCatalog -> ToolPlanner -> materialization -> grant -> admit` 的唯一授权链。

这不意味着每个 Coding 辅助函数都应立刻变成通用 provider。`todo`、进度上报、代码定位证据和 child orchestration 是 agent-local control-plane 状态，必须先定义其 scope/审计语义；把它们不加区分地塞入 catalog 同样会扩大暴露面。

## 2. 不变约束

改造必须同时满足以下约束。

1. Coding Skill/MCP 动态 alias 继续 fail-closed。R1--R5（durable anchor、contract catalog、真实 response correlation、fixed bridge、lifecycle/effect recovery）未完成前，`codingDynamicAliasesMayMaterialize()` 不得返回 `true`。
2. 不得把 `matchedSkills`、`matchedMCPTools`、BM25、任务文本、项目路径、runtime task ID、LoopContext ID 或工具名当作 provider binding、grant 或 semantic identity 的来源。
3. 同一 `ToolPlan` 的 repeat budget 仍展开为不可变 sibling selection；不得将 “可再调用几次” 退化为 callback 内计数器。
4. local 与 remote 必须是不同的 host-owned provider binding。SSH transport、remote workdir 和本地 workspace 不可互相降级或借名复用。
5. 任一 required need 的 catalog/health/contract/ready closure 不完整时，返回可解释的 `catalog_incomplete` 或 replan；不得只渲染碰巧可用的静态工具，更不得回退自由 bash、generic gateway 或 name-based provider lookup。
6. 每次模型请求的 surface 都由当前 plan revision 完整渲染。允许缓存 immutable catalog metadata，禁止缓存跨请求可执行 definition/grant/alias。
7. replacement、cancel、nested child exit、timeout 和 provider failure 要撤销同一 revision 的未消费授权；旧 response/tool call 不得在新 revision 后消费旧 surface。

## 3. 目标架构

```text
认证 Coding ingress / 已验证 continuation
  -> CodingExecutionEnvelope
       {identity, workspace binding, local|remote transport, role, posture,
        policy facts, cancellation/revision fence}
  -> host-reviewed Coding capability rules
  -> ToolCatalog (static builtins first; dynamic contract providers later)
  -> ToolPlanner (needs + constraints + completed facts + repeat siblings)
  -> PublishSurface / MaterializeReadySurface
  -> request-bound static aliases + response/tool-call correlation
  -> ResolveAlias -> canonicalize -> parameter authorizer -> Admit
  -> fixed local/remote binding -> journal -> Complete / Unknown / Reject
```

`CodingExecutionEnvelope` 是 host-owned 输入，不从 task wording 推导。它至少区分：

- execution location：local workspace、remote workspace、或无可执行 workspace；
- posture：inquiry、operational、implementation（由认证入口的既有分类结果传入）；
- nested role：worker、explorer、reviewer，以及 parent lineage；
- workspace/transport capability、网络 policy、approval、已完成 selection/artifact facts；
- 当前 semantic identity、revision 与 lifecycle fence。

模型文本只能作为需求理解的证据，不能直接决定网络、写入、SSH 或 provider 的暴露。若产品希望“版本敏感问题可检索网络”，应由受审 semantic classifier 产生 `information.search.web` need 和 freshness/effect constraint；不能继续由 `codingTaskNeedsExternalResearch(text)` 在 callback 内添加 definitions。

## 4. 分阶段实施方案

### S0：冻结旁路并建立可观测基线

目的：先让遗留面无法继续悄悄扩大，再开始迁移。

- 给 local/remote static definitions、name filters 和 dispatch switch 建立 inventory；每项标注 capability、effect、workspace/transport scope、参数契约、重复预算和是否属于 control-plane。
- 新增测试：任务文字只改变同义表述时，不得单独增加 `web_search` / `web_fetch` / `download_file` 等网络 definition；现有 lexical helper 可暂保留为诊断/quality evidence，但不得参与 tool materialization。
- 记录每次 static surface 的 `{root, turn, revision, phase, rendered tool names, omitted/unmet reason}`，以便量化迁移前后工具完整性；日志不得记录 grant/alias secret。
- 冻结规则：新增 Coding 模型可见工具必须先注册 inventory；不得再向 `codingSubAgentToolOrder`、`remoteCodingToolDefinitions` 或动态 name map 直接追加。

验收：可在测试中列出全部模型可达 Coding 工具，且每项恰有一个 inventory owner；改变任务措辞不能越权增加网络/写入/远程执行面。

### S1：先迁移静态、低耦合 capability family

目标：消除最核心的本地 coding static planner，不触及动态 provider。

1. 将已有受管能力接入 Coding envelope：`fs.read.local`、`fs.write.local`、`repo.inspect.vcs`、`build.verify.local`。它们已有 capability contract、参数边界与 sibling budget，优先复用，不复制 Coding 版 schema。
2. local `BuildToolsForModelRequest` 改为只读取当前 phase 的 `MaterializeReadySurface`；`cachedTools` 仅可缓存 catalog-definition template，不可缓存 rendered aliases/definitions。
3. callback 不再从名称直接调用本地文件/git/build switch。它先用 request/response/tool-call context 解析 alias，再经 coordinator admission 取得 host-bound selection；执行器只能接收已解析的 selection 与 canonical args。
4. inquiry/operational/implementation 变为 planner constraints：例如 inquiry 不允许 `fs.write.local` 与 sensitive build，operational 只允许其 reviewed run/verify contract；删除 callback 中“工具名是否属于该 kind”的授权判定，保留执行层的防御性 scope validation。

验收：

- 同一任务的连续模型请求均由当前 plan revision 完整重渲染；已消费 read sibling 后只能出现下一个已规划 sibling，不能因 `cachedTools` 缺失或重放旧 definition 而失败。
- 名称伪造、旧 revision alias、跨 turn alias、未 ready selection、参数越界均拒绝。
- no workspace、no git repository、contract unavailable 等环境事实产生 `Omitted`/`Unmet` 的 explain trace，不回退 legacy static array。

### S2：远程 Coding 作为独立 binding 迁移

目标：避免将远程 SSH 工具伪装成本地文件能力或让 SSH command 重新成为逃生门。

- 定义 remote workspace catalog provider：每个 selection 绑定已验证的 remote task/workspace/connection scope 和 effect/receipt policy；不能只凭 `ssh_*` 名称或 working directory 执行。
- 先迁移远程只读 inspection（read/list/search/status），再迁移受限 remote write，最后才评审 remote build/verify。每一步均定义 remote cancellation、unknown outcome 和 reconnect/replay 语义。
- `ssh_bash` 不作为通用 capability 进入任一 coding plan。可验证构建使用经审查的 remote build contract；无法表达的命令保持不可用并给出能力缺口，不回退 shell。
- local extension / knowledge search 仍按独立 capability 与 policy 处理；不能把 local MCP/Skill selector 注入 remote tool surface。

验收：local alias 不能执行 remote binding，remote alias 不能定位本地文件；连接重建、迟到响应和 uncertain SSH result 不重复外部 I/O。

### S3：处理 control-plane 工具，而非把它们伪装成普通 I/O

目标：收口 `report_localization`、`todo`、spawn、goal、code navigation 等仍由名字直接调度的面。

- 对每项写出状态所有者、作用域、effect、replay 语义与是否允许模型调用。
- `report_localization` 应成为当前 Coding revision 的受限 evidence submission，且与编辑 gate 使用同一 revision；新 revision 不得读取旧未验证报告。
- task/todo 与 goal 只可更新 host-owned task state，并携带 expected revision/version，避免迟到模型调用覆盖新任务；若不需要模型自由调用，改为 host workflow step 而非 tool definition。
- nested spawn 使用 verified parent capability 签发 child turn，禁止复制 parent surface/grant/alias；explorer/reviewer 的只读限制以 planner constraint 和 execution scope 双层执行。

验收：control-plane 调用可 journal、可拒绝重放、可被 parent cancellation fence 撤销；不再依赖某个工具名是否出现在静态 allow-list。

### S4：接入真实 request lifecycle，并统一所有 Coding static surface

目标：让 Coding static tool 与通用 semantic surface 具有相同 replacement/cancel 语义。

- 仅在 provider adapter 能提供可信 `{Protocol, ConnectionID, ResponseID, ToolCallID}`、stream cancel 与 replay identity 时，给该 Coding request bind surface；不合格 provider 保持兼容静态模式，但不得假称已迁移。
- static surface 同样使用 `PublishSurface`、`ReplaceModelRequestSurface`、`CancelRouteSurface` 与 journal；不新增 Coding 私有 grant store 或 alias map。
- 将 start-new-task、continuation、timeout、steering、nested exit、project close 和 process recovery 接到同一 lifecycle coordinator。完成事实可按 root/revision scope 复用，未开始 selection、grant、adapter 与 host-call 不可跨 revision 复用。

验收：并发的 fresh task/replacement 后，旧 response 无法解析或消费旧 static alias；retry 只在可信 transport correlation 下重用既有 journal 记录，未知 effect 不重放。

### S5：最后才恢复动态 Skill/MCP（现阶段禁止）

S5 不是 S1--S4 的替代方案。只有 R1--R5、Coding catalog contract、真实 transport correlation 与 fixed bridge 全部完成后，Skill/MCP 才能作为另外的 catalog provider 参与规划。动态 alias 仍只能由 current plan 的 immutable selection materialize，绝不恢复 `manage_skill`、`call_mcp_tool`、`byName` 或任意 provider selector。

## 5. 迁移顺序与禁止项

建议合并顺序：S0 → S1 local read/write/inspect/verify → S2 remote read-only → S3 control-plane → S2 remote write/verify → S4 lifecycle → S5 dynamic provider。

以下做法明确禁止：

- 将 `cachedTools` 的 TTL 调短、增大工具数上限或把历史工具名重新 pin 回 prompt；这只能掩盖缓存单位错误。
- 以关键词/正则决定“加一个 web tool”或“加一个 SSH tool”。
- 为了兼容 remote/local，向模型暴露带 `server`、`skill`、`tool`、`command`、`workspace` 等 selector 的泛网关。
- 从 task text、RequestID、loop ID、runtime task ID 或项目路径派生 identity、binding、grant 或 alias。
- required need 不完整时只显示剩余工具，或失败后回退 `bash`/legacy router。
- 在没有真实 correlation 的 provider 上把 callback epoch 当作 durable response identity。

## 6. 测试矩阵

| 类别 | 必测反例 |
| --- | --- |
| surface replacement | 同一 Coding task 两轮请求：第一轮消耗 read sibling，第二轮只获得下一个计划 sibling；旧 alias 不可重放 |
| wording independence | 同一 verified posture 的同义任务文本不能单独增加 web/SSH/write definition；需求变化必须来自受审语义事实 |
| posture/role | inquiry、operational、implementation、explorer、reviewer 的 selection 是 planner trace 的结果；执行层再次拒绝越界参数/效果 |
| local/remote isolation | 相同 capability name 无法跨 workspace/transport 兑换 binding；remote reconnect 后旧 alias 失效 |
| lifecycle | start new task、cancel、timeout、child exit、fresh replacement 与迟到 tool call 并发时，旧 selection 均不可 admit |
| failure | catalog/health/schema/receipt 缺失为 `catalog_incomplete`、`provider_not_ready`、`unknown` 或 replan；无 legacy/gateway fallback |
| control plane | localization/todo/goal/spawn 的迟到、重放、跨 revision/parent 调用均被拒绝或幂等 journal 化 |

除单元测试外，S1 完成后应为 Coding capability family 增加 `routingeval` 样本，覆盖 posture constraints、required/optional provider、repeat sibling、budget exceeded 和 no-fallback。真实模型端到端完成率与预算标定继续单列为线上评测，不用离线 planner 测试冒充。

## 7. 已落地的首个 S0 收口（2026-08）

本提案的第一条可独立验证改动已经完成：lean local `CodingSubAgent` 不再依据 `codingTaskNeedsLocalization` / `codingTaskNeedsExternalResearch` 的任务文本词法命中，把 `web_search`、`web_fetch` 动态拼入静态定义。二者继续只服务于 localization 证据质量门，不再是模型工具面的授权来源。

新增回归以同一 lean posture 比较普通本地 bug 与“unknown vendor SDK / upgrade / version”措辞：两者得到完全相同的静态工具面，且后者不得出现 web search、fetch 或 download。full-environment 与 nested 的既有静态研究族没有在本切片中迁移，仍须在 S1/S2 用 host policy/catalog 取代各自的静态组装；本改动没有也不能打开 Coding Skill/MCP alias。

同一切片还移除了 local `cachedTools` 的跨请求定义缓存。`BuildTools` 现在在 loop 初始化与每个 `BuildToolsForModelRequest` 调用时都重新生成完整 replacement list，并始终返回 defensive copy；测试人为在两次请求之间切换 host posture，确认第二次 request surface 不会保留第一轮的写入 definition。该修复只消除了“旧 rendered definition 充当下一请求授权”的缓存错误；当前 static selection 仍来自 legacy registry/filter，尚未达到 S1 所要求的 catalog/planner/admission 迁移。

## 8. 当前发布结论

当前可安全发布的结论是：通用 IM managed surface 与其 replacement fence 已具备复用价值；Coding 动态 Skill/MCP 已正确 fail-closed；但 CodingSubAgent/RemoteCodingSubAgent 静态工具带尚未迁移，不能宣称“所有 Coding 工具已由语义路由统一治理”。

优先级应为 S0/S1，而非提升动态 alias 可用率。这样既直接修复多轮工具面由旧 definition cache 和词法补工具造成的不完整/不一致，也不会用一个新的动态旁路换取表面功能恢复。

## 9. 第二次复审修订：把“有计划”与“可执行迁移”严格分开

上一版的 S1 方向正确，但有三个容易导致错误实现的表述缺口：

1. 通用 IM 的 `semanticCallSurface` 适配器按 IM principal 解析 workspace；Coding 的 project workspace 是另一个 host binding。直接复用该适配器会让“复用 capability contract”退化为把 Coding 调用投到错误 workspace。
2. 当前 Coding transport 尚不能可靠提供 `{Protocol, ConnectionID, ResponseID, ToolCallID}`。因此不能把 callback epoch 或一次 `BuildTools` 调用伪装成 durable request identity，更不能提前签发可执行 alias/grant。
3. `bash`、`ssh_bash`、`spawn`、`todo` 等工具的 effect/replay 模型与简单文件读写不同。把它们一起塞进第一批 catalog，会把一个静态旁路改造成一个更大的通用网关。

据此，S1 需要拆成以下**不可跳过**的三个交付物；只有 S1-C 完成后，才可对该 capability 宣称“已迁移”。

| 阶段 | 可以做什么 | 明确不能做什么 | 退出条件 |
| --- | --- | --- | --- |
| S1-A：catalog shadow plan | 由 host-owned envelope 发布静态 provider inventory，产生 `ToolPlan`、`Omitted/Unmet` 和 explain trace；与现有静态带逐请求对照 | 不渲染 semantic alias，不消费 grant，不改变静态 name dispatcher 的执行权 | 每个候选工具均有 capability/effect/schema/binding owner；缺 workspace/contract/health 的 required need 稳定报 `catalog_incomplete` |
| S1-B：Coding workspace adapter | 为 local project 建立独立的 read/write/repo/verify adapter；selection 的 binding 固定到 envelope 的 workspace fact，而不是 IM principal 或路径字符串 | 不调用现有 IM `readTrustedFile`/`writeTrustedFile` 作为“看起来相同”的快捷路径；不把项目路径 hash 成 identity | workspace A/B、同一 principal、同一 capability 的 binding 相互不可兑换；adapter 在执行前再次验证 project scope |
| S1-C：受相关性保护的 cutover | 在真实模型 adapter 提供可信 request/response/tool-call correlation 后，接 `PublishSurface → Materialize → Grant → Admit → Journal`，移除该 family 的 name-based dispatcher | 不以 epoch、loop ID、runtime attempt、RequestID 或 model function name 替代 response/tool-call identity | 旧 revision/跨 turn/伪造 alias/参数漂移/迟到 response 全部拒绝，且 replacement/cancel/retry 走同一 durable lifecycle |

S1-A 的 shadow plan 是为迁移发现差异的控制面，不是新的授权面。它可以发现“legacy 静态带额外暴露了某个工具”或“计划需要但 catalog 不完整”，但在 S1-C 前不得把计划结果反向补到模型 definitions。这样既不会因计划尚不完整而造成工具突然消失，也不会把不可信 lifecycle 误标为安全。

### 9.1 首批 family 的精确边界

首批只能处理可表达为 project-scoped、参数受限的 local I/O；以**能力族**而非当前函数名为迁移单位。下表是冻结后的初始 inventory 分类，新增模型可见工具必须先扩充此表并通过同等级复审。

| 现有静态入口 | 初始归属 | S1 状态 | 决策 |
| --- | --- | --- | --- |
| `Glob`、`ripgrep`、`read_file`、`list_directory`、`git_diff` | local inspection / repo inspect | 候选 | 先收敛为 project-scoped 只读 contract；分页、glob、pattern 和 git ref 都进入参数授权 |
| `write_file`、`edit_file`、`edit_lines` | local file write | 候选，但晚于只读 | 保留“先读后改”“localization evidence”等业务 gate；它们应成为 revision facts/constraints，而非 callback 内工具名授权 |
| `bash` | generic shell | 非 S1 候选 | 先拆成 reviewed build/verify contracts；不能把 command/working_dir 作为模型可选 selector 放进 catalog |
| `web_search`、`web_fetch`、`download_file` | external research | 非 S1 候选 | full-environment 的既有静态可用性仍是 legacy 兼容面；后续须有独立 freshness、network policy、receipt 和 artifact contract |
| `code_navigation`、`report_localization`、`todo`、`goal`、`spawn_*` | control plane | S3 | 必须定义状态 owner、expected revision、parent lineage、重放和 cancellation；不能伪装为 file/provider capability |
| `ssh_*`、`ssh_bash` | remote transport | S2 | remote session/workdir 是独立 binding；`ssh_bash` 不得迁入或替代 local verify capability |

`codingSubAgentToolOrder`、`remoteCodingToolDefinitions` 和 dispatcher switch 在 S1-C 前仍是被隔离的 legacy compatibility belt，不能被 shadow plan 的成功测试掩盖。每迁完一族，必须从这三个位置**同时**移除该族的模型可达定义、name filter 和 name dispatcher；只删 definitions 或只增加 planner 都不算迁移完成。

### 9.2 CodingExecutionEnvelope 的最小契约

`CodingExecutionEnvelope` 应由认证 Coding ingress 及工作台宿主构造，并在每次规划/执行取同一不可变副本。它不是 LLM DTO，也不是从 `TaskItem`、项目路径或运行时字符串补造的结构。最小字段为：

```text
Semantic invocation identity: verified tenant/principal/session/root/turn
Host binding:                local|remote, verified workspace handle, adapter binding ID
Posture and role:            inquiry|operational|implementation, worker|explorer|reviewer
Revision facts:              completed selections/artifacts, localization evidence, policy/approval version
Lifecycle:                   host-private revision generation, cancellation fence, catalog snapshot generation
```

其中 workspace handle 必须来自 host 在任务启动时验证的工作区记录；它可被 adapter 解析为实际目录，但目录本身不是 grant、identity 或 provider binding 的输入。remote envelope 另须携带已验证连接/session binding，并在重连后强制生成新 binding/replan。posture/role 只生成 planner constraints；执行器仍要验证 selection effect、workspace scope 和 canonical parameters，防止错误的 planner 或迟到调用越界。

### 9.3 必须写成自动门禁的完成定义

以下条件全部满足前，PR 描述只能使用“shadow-planned”或“compatibility belt fenced”，不得使用“semantic migrated”。

1. `ToolCatalog` 中的每个 provider 都有固定 `ProviderBinding`、schema digest、ready/coverage 和 project/transport scope；catalog snapshot 不能从已有 definitions 反推。
2. 每一 required need 在 coverage incomplete、provider not ready、schema drift、workspace 不存在时都产生显式 `Unmet`/replan；测试断言不出现 legacy fallback 或部分静态面冒充成功。
3. 每个模型可达 family 只存在一条执行桥：`ResolveAlias → Canonicalize → Admit → fixed adapter`。删除该 family 对工具名的 switch 授权和 task-kind allow-list 授权。
4. surface replacement 的原子测试至少包含：A/B workspace 隔离、同 root 下一 revision、fresh task、cancel、迟到 response，以及 read repeat sibling 耗尽；它们都要验证旧 grant 已撤销而新 revision 可独立计划。
5. 只有 transport 提供可信 correlation 时才允许 response-bound admission/journal；否则该 family 留在明确标记的 compatibility belt，不能以“静态工具不需要 alias”为由绕过上述四项。

### 9.4 落地顺序与观测指标修订

建议的实际合并序列变更为：`S0 → S1-A local read-only shadow plan → S1-B local workspace adapter → S1-C local read-only cutover → local write cutover → S2 remote read-only → S3 control plane → reviewed local/remote verify → S4 lifecycle consolidation → S5 dynamic provider`。这比原先“直接迁移 read/write/inspect/verify”的表述多了 adapter 与 correlation 两道门，避免为了缩短路径而误接 IM adapter 或保留双授权。

每一阶段应至少上报以下不含 secret 的计数：`catalog_incomplete`、`policy_denied`、planned/omitted/unmet capability、legacy-vs-shadow 差异、stale/revoked surface、binding/workspace mismatch、admission rejection、unknown outcome。以 capability family + host kind + posture 聚合，禁止按任务文本、项目路径或模型生成名称聚合。只有连续灰度中“shadow 与受管选择差异已解释、无越权 fallback、拒绝原因可观测”才进入下一阶段。

### 9.5 已落地：S1-A local read-only shadow plan（2026-08）

本切片已实现但**未 cutover**。Desktop local Coding ingress 现在在同一 host request 内先签发一次性 workspace handle，再把该 handle 随一次性 Coding ingress token 一起消费；运行时绑定完成并恢复 verified semantic identity 后，`CodingSubAgent` 仅以 `{identity, workspace handle, posture, role}` 构造 `codingStaticExecutionEnvelope`，生成 read-only shadow `ToolCatalog → ToolPlan`。workspace 的真实目录仍留在 App 的 host-private handle 表内，S1-A 不会解析它、更不会把项目路径、runtime ID、LoopContext ID 或任务文字当 binding。

首批 inventory 只包含 `fs.read.local` 与 `repo.inspect.vcs`，分别使用独立 Coding adapter/binding identity 和通用受管 schema；`fs.write.local`、`build.verify.local`、`shell.execute.local`、web、SSH 和 control-plane 均未加入。shadow plan 不向模型渲染 definitions、不创建 alias/grant、不改变 `BuildTools` 或 static name dispatcher；缺 workspace 时以 complete identity + incomplete catalog 生成两个 `catalog_incomplete` unmet need，而不是回退或拼接部分工具面。

回归已覆盖：

- 两个 verified local workspace 的 provider binding 不同，且 handle 不能跨 desktop owner 解析；
- 未验证/不存在目录不能签发 workspace binding；
- inquiry、operational、implementation 三种 posture 的 shadow selection 都不含 write/build/shell；
- 计划 catalog 中不包含 `skill_*` / `mcp_*`，因此不会改变既有动态 fail-closed 结论。

S1-B 的最小 adapter seam 也已落地，但仍未接模型 dispatch：它只接收已规划的 `PlannedSelection`、canonical args 和 host-issued handle；每次执行重新以 owner + handle 解析并检查目录，验证 `ProviderBinding`、capability 与 schema authorization 后才调用 project-scoped read/repo helper。它不能接受 project path、provider name、tool name 或任意 selector，跨 owner、workspace 删除、路径逃逸和 reserved `provider` 参数都会拒绝；new-task/cancel fence 也会同时删除尚可解析的旧 workspace handle。由于尚无 response/tool-call correlation，这个 adapter 只能作为 S1-B 的受测固定桥，绝不能从 `ExecuteToolStructured` 或模型 surface 触达。

下一步是 S1-C：在真实 transport 提供可信 correlation 后，才允许以该 adapter 接 `PublishSurface → Materialize → Grant → Admit → Journal`，并同步删除 read-only family 的 legacy definitions、name filters 和 name dispatcher。当前 static belt 仍是 compatibility belt，不能从 S1-A/S1-B 的落地推导出“Coding static 已迁移”。

### 9.6 第三次复审修订：S1-C 必须以 adapter 生命周期契约为入口（2026-08）

现有 shared `RunLoop` 已在真实模型请求边界依次调用 `BuildToolsForModelRequest`、创建 surface epoch、从 provider response 读取 `ResponseID`、绑定 response，并把 provider tool-call ID 连同 `ToolCallExecutionContext` 交给执行器；`codingDurableDynamicSurface` 也已有 publish / bind / resolve / admit / journal 的受测基础设施。缺的不是又一个 callback 开关，而是 **Coding transport 在发送请求前和收到响应后能否对同一条请求作出可信承诺**。

因此不批准以下捷径：

- 依据 `WireAPI`、URL、模型名、配置中的 `responses-ws` 标签或“响应里偶尔有 id”把 adapter 标为 eligible；这些只是描述或偶发 payload，不是宿主已建立的 transport capability；
- 用 `LoopContext.ID`、request/runtime ID、project path、epoch、函数名或本地生成的 call ID 填充 `ConnectionID` / `ResponseID` / `ToolCallID`；epoch 只用于拒绝陈旧 surface，不能成为 provider response 身份；
- 在 HTTP/SSE 的透明 retry、fallback 或 reconnect 后复用上一请求的 prepared surface；每次实际发送都是新的 request attempt，前一 surface 必须先被 durable retire；
- 仅新增 `ToolSurfaceExecutionContextProvider`，却没有在 response 到达、tool call 分派和 cancellation 三处完成同一个 adapter 的闭环。

#### 9.6.1 future eligible adapter 的最小接口

将来引入真正可接线的 Coding adapter 时，应由 adapter 实现以下**宿主私有**能力（名称示意，不能暴露给模型或由配置直接构造）：

```go
// 在 render/publish 之前取得；返回的 connection 由实际已建立的 transport
// session/stream 所有，且 adapter 保证后续 Send/Cancel 使用的就是该会话。
ReserveCodingToolCallChannel(ctx) (CodingToolCallChannel, error)

type CodingToolCallChannel struct {
    Protocol     string
    ConnectionID string
    Send(ctx context.Context, request ProviderRequest) (ProviderResponse, error)
    Cancel(cause error) error
    // Capability 是 adapter 编译/部署时的 reviewed declaration，不是 cfg 推导值。
    Capability() CodingProviderCorrelationCapability
}

type ProviderResponse struct {
    ResponseID string              // provider-issued；空值必须使受管面 fail closed
    ToolCalls  []ProviderToolCall  // 每项有 provider-issued ID
}
```

`CodingProviderCorrelationCapability` 至少证明：稳定 transport-owned connection identity、每个可执行 response 的 provider response ID、每个 tool call 的 provider call ID、取消后迟到 response 的拒绝语义，以及 retry/reconnect 后的 replay identity 语义。它由受审 adapter 的代码与部署配置共同发布；`codingDynamicProviderCorrelationForConfig` 只可作为“当前 adapter 未实现”的 deny matrix，绝不可成为把任意用户 endpoint 升格为合格 adapter 的 allowlist。

HTTP request / SSE 响应解析器即使能解析部分 ID，也不自动满足此接口：若连接复用、断线重连、兼容 endpoint 的 ID 缺省或 fallback 行为不能由 adapter 明确约束，就仍是不合格行。未来若使用 WebSocket，也必须由运行中的 socket/session 实例给出 opaque `ConnectionID`；不能从 `responses-ws` 配置字面值推断。

#### 9.6.2 不可交换的 S1-C 时序

```text
adapter ReserveChannel（真实 Protocol + ConnectionID）
  -> host 完整计划并 PublishSurface / PublishModelRequestSurface(epoch)
  -> 使用同一 channel 发送带该完整 definitions 的请求
  -> adapter 解析 provider ResponseID + 每个 ToolCallID
  -> BindModelRequestResponse(epoch, protocol, connection, responseID)
  -> ResolveAlias(responseID, alias)
  -> Canonicalize -> grant/selection validate -> Admit(journal)
  -> fixed Coding workspace executor -> Complete / Unknown / Reject
```

`BindModelRequestResponse` 必须发生在该 response 任一工具调用执行之前。若 response ID 缺失、任一受管 call ID 缺失、binding 冲突、response 属于旧 epoch，adapter/response binder 必须让所有受管 alias 以 `stale_surface` 或 `host_call_identity_required` 被拒绝；不得将名字交回 legacy dispatcher。普通尚未迁移的 static belt 可保持原兼容行为，但它不能和已切换 family 共用同一个 name dispatcher。

retry、模型 fallback、steering replacement 与 cancel 的顺序也必须固定：先用 coordinator durable retire/cancel 当前 request surface（fresh task/cancel 同时撤销仍未消费 grants），再取消 transport；下一次发送必须 reserve 新 channel 并 publish 新 epoch。允许重用已完成 journal 记录的唯一条件，是同一 durable HostCallIdentity 的 adapter 明确定义 provider 重传；不确定外部效果写入 `Unknown`，绝不自动重新执行。该顺序把“迟到 response 到达”从竞态变成可验证的拒绝路径。

#### 9.6.3 合格性测试、灰度与删除门槛

S1-C 的首个切片仍只允许 local `fs.read.local` / `repo.inspect.vcs`。在一个真实合格 adapter 上，必须通过以下 adapter conformance suite，才可打开该 adapter + capability family 的灰度：

| 场景 | 必须断言 |
| --- | --- |
| request prepare / response bind | surface 使用 reserve 返回的 protocol/connection；response 在第一条 tool call 前绑定；缺任一 provider ID 时不执行 fixed adapter |
| alias 与参数 | 伪造 alias、旧 epoch alias、跨 response alias、同 call ID 不同参数、reserved provider selector 均不触达 workspace |
| attempt 边界 | retry、fallback、reconnect 各自使用新 epoch；旧 response 不能 bind 或 consume successor grant |
| lifecycle race | cancel、fresh task、steering、project close 与 response bind/dispatch 并发时，durable fence 胜出，迟到调用不执行 |
| replay / uncertainty | 仅 adapter 宣称同一 provider 重传身份时重放 journal 结果；连接丢失或 effect 结果未知时记录 `Unknown`，不重复 I/O |
| binding isolation | workspace A/B、principal/turn/role、local/remote connection 不可交换；fixed executor 仍重验 handle、binding 与 canonical schema |

灰度键必须是 `{reviewed adapter build, capability family, verified host kind}`，不能是 URL、模型名、项目路径或任务文本。开启一个 family 时，同一 PR 必须删除该 family 的 legacy model definition、task-kind/name filter 和 name dispatcher，并加一条“旧 switch 已不可达”的回归；否则是双执行桥，不是 cutover。任何不合格 adapter 继续走明确标识的 compatibility belt，不能因为受管 family 的 shadow plan 成功就删除其现有工具或把部分静态定义误报为 semantic migration。

### 9.7 已落地：S0 inventory 与 request-surface compatibility fence（2026-08）

在不能进行 S1-C cutover 前，本轮先把剩余 static belt 的一个实际漏洞收紧：此前 local/remote callback 虽每请求重建 definitions，但执行时仍只看 name dispatcher 与 posture/role 判定；模型若发送上一轮已被移除的名字，或编造一个 dispatcher 恰好支持、但本轮从未 render 的名字，仍可能走到 legacy 执行路径。

现已为 local 与 remote 建立闭合的 S0 compatibility inventory。每个当前可见 static 名称都有 capability、effect、binding scope 与 control-plane 标注；渲染后再经过 inventory allow gate，未知 definition 不能悄悄进入 surface。每个 `BuildToolsForModelRequest` 还保存**本次完整** rendered static name set，`ExecuteToolStructured` 与 `ExecuteToolCallWithContext` 在进入 legacy dispatcher 前必须命中该 set；上一 request 已撤下的 write/SSH name、跨 host 的 local/remote 名称、以及 invented name 都返回 `static_surface_unavailable`，不会落到 dispatcher。LongHorizon 的 GUI/browser host tool 不属于 Coding static inventory：它们继续由 frozen host episode policy render，但同样只允许本次实际 rendered definitions 的 name set，不能因 policy 中存在而越过缺失 registry definition。

该 fence 不签发 alias/grant，也不把 name set 误作为 semantic authorization：它只是 compatibility belt 的 request-local exposure/execution 一致性检查。模型可见的 function name 还必须**精确**命中 rendered name；不得先把 `grep_search` / `search_files` / 大小写变体归一为 `ripgrep` / `Glob` / `read_file` 再验证，否则旧兼容别名又会扩张本轮 surface。直接 host-maintenance/test 调用在尚未有 model request surface 时仍保持既有 guarded 行为；真实 `RunLoop` 的模型调用始终先建 request surface。回归覆盖 inventory 的完整/唯一 owner、全部 local/remote posture/role surface、inquiry replacement 后 stale write、跨 host 名称、invented name 和未 render alias。S1-C 仍须在合格 adapter 出现后删除被迁移 family 的这层 compatibility definition/filter/dispatcher，而不是把此 fence 宣称为迁移完成。

#### 9.7.1 复审限制：name-set fence 不能识别“同名的迟到响应”

本轮再次核对 callback 与 `RunLoop` 后，必须明确一个不能由 S0 修补的边界：`staticCompatibilitySurface` 只是 callback 上的**当前名称集合**，没有可靠的 `{Protocol, ConnectionID, ResponseID, ToolCallID}` 绑定。因而它能拒绝“上一轮已撤下的 `write_file`/`ssh_bash`”和未呈现名称，却不能区分“旧响应调用 `read_file`”与“当前响应调用同名 `read_file`”；若该名称同时存在于两个 surface，迟到调用仍可能进入 compatibility dispatcher。

这不是应以 callback epoch、iteration、request ID 或本地生成 call ID 修复的问题：它们都不是 transport-owned response identity。当前 fence 的承诺必须严格限定为**名称暴露与 legacy 执行的一致性**，不能宣称具备 stale-response revoke、幂等 admission、replay protection 或 effect journal。对可能并发/重试/重连的 provider，这个残余风险只能由 §9.6 的真实 correlation adapter、`BindModelRequestResponse` 和 durable `Admit → Journal` 消除。

因此 S1-C 前的发布与指标也应调整：仅统计 `static_surface_unavailable`、inventory drift 和 legacy-vs-shadow difference；不得把“旧同名调用被拒绝率”列为安全指标，更不得让有外部效果的 family 因 S0 fence 获得更高信任等级。若产品必须在 S1-C 前降低这类风险，只能以 host policy 缩小 compatibility surface、禁止并发 replacement/retry 或暂不暴露相应 effectful static family；不能伪造 correlation 或把 name set 升级为 grant。

#### 9.7.2 已落地：callback-local replacement epoch（2026-08）

在不伪造 provider correlation 的前提下，compatibility belt 仍可修复一个更窄的本地竞态：`RunLoop` 已有 request-bound `SurfaceEpoch` 的扩展点，但 Coding callback 以前返回空值，故同一 callback 在第二次 `BuildToolsForModelRequest` 后仍会接受由**本进程旧 request execution context**携带的同名调用。

现改为：每次真正的 static request surface render 后，local/remote callback 分别签发一个仅内存有效的随机 `coding-static:*` / `remote-coding-static:*` epoch；下一次 replacement 立即退休前值，context-aware callback 只接受当前 epoch。回归覆盖 local `read_file` 与 remote `ssh_read_file`：同名旧 epoch 均在 dispatcher 前返回 `static_surface_unavailable`，当前 epoch 正常通过。空 execution context 仍只服务既有 direct host-maintenance/test compatibility path；真实 `RunLoop` 会携带该 epoch。

该机制的安全声明严格受限：epoch 不进入模型 definitions，不充当 `ConnectionID`、`ResponseID` 或 `ToolCallID`，不写 journal，不能跨重启，也不能证明网络上某条响应属于该 request。它只减少同一 live callback 的 replacement-after-dispatch 窗口；真正的 response correlation、replay 和 uncertain-effect 仍由 S1-C 负责。

#### 9.7.3 已落地：静态 belt 的逐请求可观测基线（2026-08）

S0 的 inventory 现在不再只是代码常量：每次 `BuildToolsForModelRequest` 完成完整 replacement surface 后，local/remote callback 都形成一条脱敏 `coding_static_surface` audit observation。记录内容固定为 host kind、callback-local static revision、已验证 posture、排序后的实际 rendered names，以及 local 已有 shadow plan 时的 opaque plan ID/catalog generation/omitted/unmet reason。它不记录任务文本、项目路径、workspace handle、参数、grant、alias 或 provider secret。

local observation 会关联已经由 host-owned envelope 产生的 S1-A plan；没有 verified ingress 或没有 plan 时明确标为 `not_prepared`，不会临时从 callback/task 文本补出 identity。remote 当前还未进入 S2 catalog，故同样明确记作 `not_prepared`，不能伪造 local shadow plan。回归验证 replacement 的 revision 与实际 surface 等价、从 implementation 切到 inquiry 后 write 名称不再出现，并验证 audit 中可关联 shadow plan 但不泄漏 workspace binding/path。

这提供了 §9.4 所需的 S0 迁移基线（rendered names、posture、shadow/legacy 差异所需的 decision references），但 observation 本身不参与 dispatch/admission；审计不可用也不能改变工具面。下一步可依据这些记录补充受控的 legacy-vs-shadow 差异聚合，仍不得以 observation 作新的授权来源。

#### 9.7.4 修正：已验证 identity 但 workspace binding 缺失时也必须形成 shadow plan（2026-08）

复审运行时启动链路发现一个实施偏差：`prepareCodingStaticShadowPlan` 本身已能把不完整 workspace envelope 表示为两个 `catalog_incomplete` unmet needs，但 `CodingSubAgent` 原先额外要求 `staticWorkspaceBinding.complete()` 才调用它。结果是“已验证 identity、但 workspace 不存在/已撤销”的真实 ingress 会被记成 `not_prepared`，丢失设计要求的 catalog coverage 诊断。

现已将 runtime bridge 收敛为唯一 helper：只要 durable verified identity 存在就准备 S1-A shadow plan；workspace binding 原样传入，绝不从 project path、task 文本、runtime ID 或 loop 值补造。完整 binding 仍产生两个 project-scoped read-only selections；缺 binding 则稳定产生 `CatalogCoverageIncomplete` 和两个 `catalog_incomplete` unmet needs，且零 selection。新增回归覆盖这条 runtime bridge、identity 缺失拒绝与“不制造 workspace binding”。该改动仍不 materialize definition/grant/alias，也不影响 legacy dispatcher。

#### 9.7.5 已落地：legacy-vs-shadow capability 对账（2026-08）

逐请求 observation 现增加 capability-class 差异摘要：`legacy_only_capabilities` 与 `shadow_only_capabilities`。比较单位是 inventory/plan 中的 capability ID，而非模型函数名；例如 `Glob`、`ripgrep`、`read_file` 三个 legacy 名称共同映射为一个 `fs.read.local`，与 shadow 的单个 read selection 不应误报差异。反之，legacy `write_file` 在当前 read-only shadow plan 中会明确成为 `legacy_only`。

差异只在已有 local shadow plan 时计算；remote S2 仍为 `not_prepared`，不伪造 planner 结果。摘要随同脱敏 audit record 持久化，用于识别迁移前 legacy 暴露比候选 selection 更宽或更窄的情形；它不反馈到 renderer、不会撤销/补充 definitions、更不能作为 fallback 或授权输入。回归覆盖多名称同 capability 的零差异以及 write capability 的显式 legacy-only 差异。

### 9.8 第四次复审：S3 control-plane 必须采用 revision fence，而不是把 callback state 当授权（2026-08）

#### 9.8.1 现状与结论

对 local/remote callback 的复核确认，`report_localization`、`todo_write` 与 `spawn_coding_agent` 虽均有各自的业务校验，但仍是 callback-local state 加名字 dispatcher 的控制面：

| 面 | 当前已有保护 | 尚未定义的语义缺口 |
| --- | --- | --- |
| `report_localization` | evidence/research 校验；编辑前读取同一 callback 的 localization state | evidence 没有显式归属到本次 static revision；replacement 后旧证据理论上可授权同一 callback 上的新 revision 编辑 |
| `todo_write` | mutex、参数规范化、merge/clear 规则与 UI emit | 没有 expected revision/state version；迟到 replace/merge 可能覆盖较新的 checklist |
| `goal` | host task/编排器更新 | 未声明是模型可写 control state 还是 host workflow step，也没有 revision/CAS/replay 合约 |
| `spawn_coding_agent` | role、depth、runtime attempt running、worker isolate/read-only ledger admission | 子 turn 的创建虽应重新 resolve anchor，但协议未把“不可继承 parent surface/grant/alias”写成可验收的 control-plane contract |

这里的 `staticCompatibilityEpoch` 只能作为**进程内 compatibility fence**：它能让携带旧本地 execution context 的调用在进入 legacy dispatcher 前失败，但既不是 response/tool-call identity，也不是可持久的 task revision。故 S3 不得把它写入 grant、journal、continuity record 或作为重启后的依据；也不得因此开放动态 Skill/MCP。

#### 9.8.2 最小状态模型

每个 callback 维护一个仅供 compatibility belt 使用、随完整 static surface replacement 单调推进的 `ControlPlaneRevision`。它应与 rendered static revision 同步采样，但使用单独名称以避免把它误解为 provider correlation：

```go
type CodingControlPlaneRevision uint64 // callback-local compatibility generation

type RevisionBoundLocalization struct {
    Revision CodingControlPlaneRevision
    Evidence CodingSubAgentLocalizationEvidence
}

type RevisionedTodoState struct {
    Revision CodingControlPlaneRevision
    Version  uint64 // 每次成功 compare-and-apply 后递增
    Items    []codingAgentTodoItem
}
```

不变量如下：

1. 每次成功安装新的完整 static surface 时，原子推进 `ControlPlaneRevision`；先前 revision 的 localization evidence 对新 revision 不可读、不可授权编辑。
2. `report_localization` 的写入必须在验证、research evidence 快照与 revision 检查同一临界区完成；返回值只可显示当前 revision 的 evidence。
3. `todo_write` 需携带由 definition 固定 schema 声明的 `expected_revision` 和 `expected_version`。成功更新须满足两者相等；replace、merge、clear 都是 compare-and-apply，成功后 version 加一。任何失配返回稳定的 `control_plane_stale`，不得重新解释旧 payload。
4. `goal` 若继续暴露为模型工具，也必须采用 owner-owned `expected_revision/version`；若只为编排器推进外层任务，则应移出模型 surface，成为 host workflow step。
5. control-plane revision 只隔离同一 live callback 的 replacement；其内容不跨 callback、child、process restart 或 provider retry 继承。需要跨进程恢复的 todo/goal 必须由 runtime/task owner 建立独立 durable CAS record，而不能序列化该 revision。

为兼容既有 host-maintenance 与单元测试的**无模型请求直调**，在尚未安装任何 request surface 时允许显式 `direct-host` 分支：它可维持原有验证逻辑，但不得把结果标为模型请求产生的证据，也不得在之后的第一次 surface render 自动转化为 revision-bound 授权。真实 `RunLoop` 调用始终安装 surface，因而必须携带/使用当前 control-plane revision。

#### 9.8.3 定位证据与编辑 gate

推荐执行顺序：

```text
render static surface R
  -> report_localization(R, evidence)
  -> validate evidence + research snapshot
  -> atomically store {R, evidence}
  -> write/edit gate reads only {R, evidence}

replacement R -> R+1
  -> invalidate evidence from R for authorization
  -> old report/write with R: control_plane_stale / static_surface_unavailable
  -> R+1 requires a new report before editing existing bug target
```

编辑 gate 不可仅比较文件路径：旧 evidence 即使恰好指向同一 `read_file`/`ssh_read_file` 目标，也不能跨 revision 复用。research searches 仍可以保留为诊断事实，但其“足以授权编辑”的结论必须与 evidence 同属当前 revision。new-file 豁免、role/write-scope、read-before-write 等现有业务 gate 保持独立，不能被 localization state 绕过。

这是一条 compatibility safety improvement，不是 durable evidence journal：由于当前 HTTP/SSE adapter 尚无可信 response correlation，它只能拒绝被 core loop 明确标记为旧的本地上下文，不能声明为对 wire-level late response 的完整防护。

#### 9.8.4 Todo/goal 的 compare-and-apply 与重放策略

`todo_write` 的模型 definition 应显式要求：

```json
{
  "expected_revision": 7,
  "expected_version": 12,
  "merge": true,
  "todos": [{"id":"inspect", "content":"...", "status":"completed"}]
}
```

`expected_*` 由上一次成功结果或本轮 host-rendered state 提供，不能由任务文本、路径、runtime/loop/request ID 推导。成功结果必须返回新的 `{revision, version, checklist}`，让下一次调用具有可验证的 optimistic-concurrency token。重传策略在 S3 compatibility 阶段保持保守：没有 provider `ToolCallID` journal 时，**不承诺模型重传幂等**；重复到达只有在 expected version 仍相等时才会成功，第二次通常为 `control_plane_stale`，而不会静默重复 emit/UI mutation。未来接入合格 adapter 后，再以 durable HostCallIdentity 将相同调用映射为已完成结果。

对于 host 编排器已有的外层计划，`todo_write` 只写当前子步骤的 agent-local checklist，不能更新其它 `Tn`，也不能以 merge 方式覆盖 scheduler 的 task status。对 remote skill runner 的同一 helper，必须传入其自身 owner revision/version；不得把 local callback state、SSH session、skill run ID 或 child result 当作版本来源。

#### 9.8.5 Spawn 的谱系与授权边界

`spawn_coding_agent` 的 child 不是 parent request 的第二个 consumer。其最小合同应为：

```text
verified, running parent attempt + admitted spawn spec
  -> host/runtime signs child attempt/turn lineage
  -> child resolves its own trusted identity and renders its own surface
  -> parent receives bounded report only
```

禁止项：

1. 不复制 parent 的 static epoch/control-plane revision、request surface、alias、grant、matched provider 或 localization/todo state 到 child；child 即使执行相同任务文本也必须从自身 admission 开始。
2. worker 只接受 host 创建的 isolated worktree 与 declared write-set；explorer/reviewer 只通过 read-only runtime admission，且 role policy 同时在 renderer 和 executor 生效。
3. parent cancel/supersede、attempt 非 running、lease/lineage 校验失败时，拒绝新 child；已 detached 的 read-only child 只能向后续明确 parent attempt 交付 bounded report，不能回写已结束 parent callback state。
4. child 的任何 control-plane completion 不反向提升 parent revision，也不自动使 parent 的 todo/localization 成立；需要合并时由 host 读取 bounded result 并创建新的 parent-owned revision。

当前 local/remote 构造器已经朝“child 在自己的 runtime Attempt 重新 resolve anchor”收口；S3 的实现和测试必须把该限制固定下来，而非依赖注释或调用惯例。

#### 9.8.6 实施切片、验收与状态声明

建议按以下顺序交付，任何一步均不改变 S1-C/S5 的 correlation 前置：

1. 为 local/remote callback 引入 revision-bound control-plane state，并在 surface replacement 时原子撤销旧 evidence；补 current/old revision localization 回归。
2. 将 todo helper 扩展为 expected revision/version 的 compare-and-apply；先接 local、remote、remote skill runner，再移除允许无版本模型 payload 的渲染路径。
3. 审计 goal owner；不能提供 host-owned CAS 的 goal 从模型 definitions 移除。
4. 为 local/remote spawn 增加 parent/child lineage、no-surface-inheritance 和 parent-ended rejection 的回归；读写隔离仍由 runtime/worktree executor 重验。
5. 仅在具备真实 transport correlation 后，把 control-plane call 纳入 durable journal/replay；届时 revision fence 继续作为业务并发控制，不能被 journal 取代。

| 回归 | 必须断言 |
| --- | --- |
| localization replacement | R 的 report 允许 R 的 edit；安装 R+1 后 R 的同名 edit 不得被旧 evidence 授权；R+1 重新 report 才可继续 |
| todo stale merge | R/version N 成功；R/version N+1 成功后，迟到的 N replace/merge/clear 返回 `control_plane_stale`，items 与 UI state 不变 |
| direct-host compatibility | 未渲染 surface 的显式 host 调用保留既有校验；首次 model surface 后该旧 state 不自动获得模型编辑授权 |
| spawn lineage | child 没有 parent surface/epoch/todo/localization；parent attempt ended/cancelled 时拒绝 spawn；child completion 只能生成 bounded report |
| remote parity | remote localization/todo/spawn 使用同样 revision/lineage 规则，且不把 SSH session 视为 semantic identity |

完成状态只能称为 **S3 callback-local revision/CAS compatibility fence**。在 adapter lifecycle、response correlation、durable journal 和 task-owner CAS 未完成前，不能称为 control-plane semantic cutover，也不能以 S3 为理由放宽 `codingDynamicAliasesMayMaterialize()`。

#### 9.8.7 已落地：local/remote localization revision fence 与 todo CAS（2026-08）

首个 S3 compatibility 切片现已实现，范围刻意保持在 live callback 内：local 与 remote 的每次 `BuildToolsForModelRequest` 完整 replacement 都推进同一个 callback-local control-plane revision；`report_localization` 写入时记录该 revision，local/remote existing-file edit gate 只读取当前 revision 的 evidence。故 R 的 report 可以授权 R 的 edit，但 R+1 render 后同一 evidence（即使路径相同）不再授权，必须重新报告。

`todo_write` 也已接入 revision/version CAS。真实 rendered definition 对 `expected_revision` / `expected_version` 明示本次值；callback 只在两者与当前 state 相等时 apply，成功后返回新 version。surface replacement 会推进 revision 并使旧 version 失效，迟到的 replace/merge/clear 返回 `control_plane_stale`，不会 emit UI mutation 或覆盖 items。未进入模型 request renderer 的 direct-host / SkillRunner workflow helper 仍走显式 revision-zero 兼容分支；其 state 不会自动变成首次模型 request 的 localization/edit authority。

新增 local/remote stale-CAS、local/remote localization replacement、rendered token 与 child fresh-state 回归；现有 worker isolate、runtime parent-running 与 remote read-only admission 仍是 spawn 的执行门。由于 goal 尚无 host-owned durable CAS，本轮已将它从 local/remote Coding 的模型-visible legacy surface 移除；既有 host/orchestrator goal API 不受影响。该实现没有把 revision 写入 grant/journal，也没有把它作为 `ConnectionID`、`ResponseID` 或 `ToolCallID`；dynamic Skill/MCP 继续 fail-closed。provider-correlated control-plane journal 与未来若重新开放 goal 所需的 durable owner/CAS 仍是后续切片，不能把本项误报为 S3 semantic cutover。

#### 9.8.8 第五次复审：child 不得复用 parent `LoopContext`（2026-08）

前述“child callback 从空 control-plane state 开始”的回归还不足以证明谱系隔离。代码调用链显示，local/remote 的 `newReadOnlyNested*` 与同步 `runNested*` 构造器仍把 `parent.loopCtx` 原样传给 child。即使 detached read-only child 已持有独立 `executionCtx`，它仍可经该指针读取或间接使用 parent 的 `ID`、`Runtime.RequestID/PolicyOwnerID`、`CodingTaskIngressToken`、attachments、取消信号、workflow/source-preview 字段以及基于 `UserID` 的粘性审批状态。

这有三个不同层面的缺陷，不能用“`executionCtx` 优先”一笔带过：

1. detached child 的 `ShouldStop`、LLM trace、tool context 与审批回调仍可能回落到 parent `LoopContext`；parent 正常 handoff 后的 cancel 会影响本应 ledger-owned 的 child，或把 parent request trace 误记为 child trace。
2. remote child 的公开 `ExecuteTask` 会检查 `loopCtx.CodingTaskIngressToken`。若 child 继续持有 parent context，就存在再次读取/消费 root ingress 的路径；即便一次性 token 常已消费，也不能把该偶然性当作授权边界。
3. `UserID`、`codeSessionID` 和 shared approval state 在现有实现中不全是纯展示字段：它们可定位粘性权限、源预览或交互通道。复制整块 context 因而会把 UI 便利性和执行授权混在一起。

结论：`LoopContext` 不能作为 child bootstrap object，更不能作为 lineage、binding、grant、correlation 或 transport identity 的载体。child 的语义根仍只能在 runtime admission 后由已验证 relation 签发；随机 child loop ID 仅可用于进程内诊断，不能参与上述任何授权判断。

**修正架构：显式 child execution envelope。** 新增 host-private `CodingChildExecutionEnvelope`（名称可调整），将当前隐式继承拆开：

```text
parent running attempt + admitted spec
  -> host creates ChildExecutionEnvelope
       {mode: synchronous | detached,
        cancellation: child-owned context,
        diagnostic trace: fresh child-local value,
        optional UI progress sink: explicit and non-authorizing,
        workspace/transport: already policy-checked}
  -> child constructor receives the envelope, never parent LoopContext
  -> detached child binds only runtime Attempt context
  -> child admission issues/recovers its own trusted turn
  -> child renders fresh surface and returns bounded result
```

最小字段与禁止复制规则：

| 类别 | 允许来源/处理 | 明确禁止 |
| --- | --- | --- |
| cancellation | detached：`StartReadOnlyChild` 的 child runtime context；synchronous：host 显式建立、只向下传播 parent cancel 的 child context | 保存或读取 parent `CancelC`/`LoopContext`；让 detached child 因 parent normal handoff 停止 |
| trace/本地 loop bookkeeping | fresh host-random diagnostic ID；可另存 parent diagnostic link，且仅供观测 | parent `LoopContext.ID`、`Runtime.RequestID`、task/path/模型工具名作为 child identity、binding、grant 或 correlation |
| semantic lineage | admission 后 `IssueChildCodingTurn`/durable mapping | 复制 `dynamicInvocationIdentity`、verified handle/subject、surface、epoch、alias、grant、localization 或 todo |
| ingress/attachments/workflow | child 默认空；如产品另有需求，必须经新的认证 ingress 或显式 artifact handoff | `CodingTaskIngressToken`、`CodingAttachments`、`Runtime`、source-preview/workflow 生命周期的整块复制 |
| UI/progress/approval | progress callback 可显式传入；effectful worker 的 child-scoped approval 必须重新由 host preflight/签发 | 以 copied `UserID` 查询/继承 sticky scope 或 high-risk approval；把 UI owner 当作 semantic owner |

同步与 detached 不能共用取消策略：同步 worker/inspection 在 parent 仍运行期间可以有一条显式、单向的 parent-cancel → child-context bridge，但 bridge 不暴露 parent fields，且完成时注销；detached explorer/reviewer 只能由其 runtime Attempt、lease/recovery 或显式 child cancellation 终止。两类 child 均不得把 parent `LoopContext` 留在 agent struct 内。

实现顺序修正如下：

1. 增加 fresh/restricted child context/envelope factory；local 与 remote 两组 nested constructor 都改用它，禁止传递 `parent.loopCtx`。
2. 将 `ShouldStop`、`LLMRequestContext`、tool context、trajectory trace 与 approval callback 改为读取 envelope 的 child cancellation/diagnostic inputs。child trace 不得带 parent request/loop ID。
3. 将 root ingress relation 的消费限制在 root authenticated ingress；嵌套 remote `ExecuteTask` 不得读取任何 `LoopContext.CodingTaskIngressToken`。read-only child 只在 `ExecuteReadOnlyChild` 的 runtime admission 后签发 child turn。
4. 去掉 local `scopeApproval` 与 remote `highRiskApproval` 的隐式继承。read-only child 不应请求 effect approval；worker 必须对 isolate/write-set 建立 child-scoped、可撤销的 host approval。
5. 最后删除“detached child 保留 parent `LoopContext` 仅作 metadata”的注释和兼容路径；否则未来字段继续加入 `LoopContext` 时会重新扩大继承面。

新增验收必须覆盖 local/remote 两侧：

| 回归 | 必须断言 |
| --- | --- |
| fresh context | `child.loopCtx != parent.loopCtx`（最好 child 不持有该字段）；child 不含 parent ID、runtime/request、ingress token、attachments、workflow/preview state |
| cancellation | parent cancel 能终止 synchronous child 的显式 bridge；detached child 在 parent handoff/cancel 后仅按其 runtime context 决定是否停止 |
| ingress/identity | 嵌套 child 无法读取或消费 root ingress；入场前没有 dynamic/verified identity，admission 后才获得 fresh child turn |
| trace | child LLM trace 不含 parent `RequestID`/`LoopID`；任何 parent diagnostic link 不参与 scheduler owner、binding、grant 或 audit correlation |
| approvals | child 不复用 parent sticky/full/high-risk decision；worker approval 被限定到 child isolate/write-set，read-only child 不可借机升级 |
| control plane | child 的 revision/epoch/todo/localization 仍为零值，child completion 只产生 bounded result，不能回写 parent callback |

本节是 S3 谱系修正，不改变 S1-C 的真实 correlation 前提：fresh child diagnostic ID 和 child control-plane revision 都不是 `ConnectionID`、`ResponseID`、`ToolCallID` 或 durable journal key；dynamic Skill/MCP 继续 fail-closed。

#### 9.8.9 已落地：受限 child context 与 root-ingress 隔离（2026-08）

本节的第一段已实现。local/remote 的 read-only detached child 及同步 nested child 现在都创建 fresh restricted `LoopContext`，构造器不再把 `parent.loopCtx` 传给 child。该 context 仅提供随机 diagnostic loop ID、空的 user/runtime/ingress/attachment/workflow/preview 字段与本地取消通道；它不构成 semantic identity、binding、grant 或 correlation。同步 child 建立并在退出时释放单向 parent-cancel bridge；detached child 不建立该 bridge，仍由 `StartReadOnlyChild` 传入的 runtime Attempt context 驱动。

同时移除了 local `scopeApproval` 与 remote `highRiskApproval` 的 parent-state 引用继承；child 的 progress callback 仍显式保留但不授予 scope。远程 `ExecuteTask` 只允许 root（`nestDepth == 0`）读取 desktop ingress relation，嵌套 child 不会再读取 `CodingTaskIngressToken`；read-only child 仍只在 runtime admission 后 `IssueChildCodingTurn`。回归覆盖 local/remote fresh context、parent ID/request/ingress/attachments/workflow/preview 字段不泄漏、审批 state 不复用，以及 parent cancellation 对 synchronous 与 detached child 的不同语义。

同步 worker 现创建自己的 approval state：local 仅预批准当前 host-created worktree root，remote 仅预批准当前 remote isolate root；两者均不携带 parent 的 full-access、approved directories 或 high-risk decision。额外路径和高风险 command 在现阶段保持 fail-closed（直到 child-scoped host preflight 有可持久的审批合同）。

补充审计发现 read-only adapter 曾使用 `child := *parent`；即使构造器已不复制 surface，浅拷贝仍会暂时携带 parent 的 dynamic/verified identity、relation handle 与 local shadow binding。现已将其收口为 admission-only helper：先捕获 relation 输入，立即清空所有 parent semantic carriers，再仅以 fresh child Attempt 签发/resolve child turn。运行中的 child 不保留 parent verified handle、workspace binding、shadow plan 或 verified identity；没有可验证 child mapping 时仍保持 dynamic surface fail-closed。

local child 在得到 fresh attempt identity 后可重新产生 **自己的** read-only S1-A shadow plan，但 workspace binding 仍保持空值：它会稳定报告 `catalog_incomplete`，而不是沿用 parent workspace 或从 child project path 推导 binding。该 plan 仅用于对账/诊断，不 materialize alias、grant 或静态执行权；child workspace binding 必须等待后续 admission contract 明确签发。

仍未完成的是 child-scoped effect approval 的 durable host preflight、provider-correlated journal/replay 与完整 static semantic cutover。尤其不得把 fresh diagnostic ID、child callback revision 或 runtime context 误作 wire-level response/tool-call correlation；dynamic Skill/MCP 继续 fail-closed。

### 9.9 第六次复审修订：S0.5 对无 correlation 的 compatibility belt 先做 ambiguous-delivery containment（2026-08）

#### 9.9.1 审计发现与范围

`RunLoop` 虽已在模型请求边界重建 static surface、创建 callback-local `SurfaceEpoch`，并在 response 解析后、工具分派前调用 `ToolSurfaceResponseBinder`，但 Coding 当前 HTTP/SSE transport 没有可验证的 `{Protocol, ConnectionID, ResponseID, ToolCallID}` 生命周期。尤其 fallback 与 transient retry 都会重新 render definitions/epoch；如果前一次已开始的请求在 timeout、取消、断流或连接错误后仍可能在网络侧完成，host 无法仅靠 name set 或 epoch 证明一条迟到的同名静态调用不属于 successor surface。

这不改变现有 S0 fence 的价值：它仍能拒绝未 render 名称、跨 host 名称和 core loop 明确携带旧 epoch 的调用。缺口是 adapter 将旧 wire response 与新本地 context 错配时，`read_file` / `ssh_read_file` 等同名调用无法仅凭 callback state 区分。当前 `codingDynamicProviderCorrelationForConfig` 已明确不把这类 transport 认定为合格；本节也不为 static belt 创建例外。

因此新增 S0.5，定位为 **fail-closed containment**：一旦 host 看到“请求可能已投递、却无法可靠归属结果”，就禁止在同一 Coding turn 自动制造下一张可执行的 static compatibility surface。它减少 retry/fallback 的误执行窗口，但不提供 response bind、alias resolve、grant consume、journal replay、unknown-effect settlement 或跨重启恢复。

#### 9.9.2 最小本地 attempt 状态机

该状态机只记录 host 对发送过程的观察，绝不是 provider correlation：

```text
prepared
  -> not_sent                 (本地明确拒绝，未有任何字节离开 host；可 abandon/retry)
  -> started
started
  -> response_consumed        (本轮正常解析并消费；维持 S0 compatibility 语义)
  -> ambiguous_delivery       (timeout/cancel/decode/stream/connection/fallback/retry/未知错误)
ambiguous_delivery
  -> quarantined_turn         (撤下 static compatibility surface，结束本轮 tool-enabled Coding turn)
```

不变量：

1. `started` 之后除非实际 adapter 能证明 `not_sent`，任何错误一律进入 `ambiguous_delivery`；HTTP code、URL、provider文案、request/loop/runtime ID 或模型文本都不是该证明。
2. `ambiguous_delivery` 后，先清空当前 callback 的 rendered-name set 与 epoch，再禁止本 turn 的 automatic retry、fallback、steer replacement、light-upgrade 产生新的 static compatibility request。不得先 render successor 再“希望旧请求不会回来”。
3. quarantine 只约束模型可达的 Coding static belt。direct-host/test compatibility 路径必须显式调用、单独审计，不能借空 execution context 回流成模型 dispatch。
4. 新用户任务可以由 task owner 启动新的尝试；这不是同一 callback 对旧 response 的授权继承。若未来合格 adapter 存在，则使用 S1-C durable retire/cancel/channel lifecycle，而不是 S0.5 的本地状态机。

实现应以 core 中可选的 attempt observer 为接线点，而非在 local/remote callback 各自猜测 HTTP 行为：请求字节即将发送时通知 `started`；response 已被本轮消费时通知 `response_consumed`；任何开始后的 retry/fallback/cancel/error 默认通知 `ambiguous_delivery`。observer 的输入只可来自实际 transport 调用点。它不签发 `ConnectionID`，不把 `SurfaceEpoch` 写成 `ResponseID`，也不允许 callbacks 因为看到 callID/name 而自行宣布请求已安全结束。

#### 9.9.3 兼容面收缩规则

| family | 正常 S0 compatibility | `ambiguous_delivery` 后 | S1-C 前的声明 |
| --- | --- | --- | --- |
| `fs.read.local` / `repo.inspect.vcs` | 可保留 name-set + epoch fence | 本 turn 零 static definitions，结束或请求任务 owner 重试 | 仅 compatibility containment；非 replay-safe |
| write、patch、命令、build、SSH command | 不应因 S0 fence 获得更高信任；应优先迁出/收缩 | 零暴露、零 dispatch；不得自动 retry | effectful legacy，非 semantic migration |
| `report_localization` / `todo_write` / `spawn` | 继续各自的 S3 revision/CAS/lineage gate | 不接受迟到 model call；callback state 不得由 successor 消费 | S3 local fence，不是 journal |
| Skill/MCP dynamic | 继续零 materialization / fail-closed | 不变 | S5 前禁止 |

对 effectful family 的“优先收缩”不是授权恢复；在没有可接受产品替代时，应明确让模型输出受限失败，而不是让 `write_file`、`bash`、`ssh_bash` 或 `spawn_coding_agent` 跨一次 ambiguous request 自动继续。只读 family 的保留也不能被解释为保密性、参数新鲜度或 wire-level stale response 已得到完整保证。

#### 9.9.4 实施、回归与完成声明

建议按以下小切片实施：

1. 在 core request send/retry/fallback/cancel 边界加 host-private observer；先补测试证明 fallback/retry 都报告 predecessor 的 terminal state，再构建 successor。
2. local/remote callbacks 接入 shared quarantine state；`ambiguous_delivery` 原子撤下 static epoch/name set，拒绝 `BuildToolsForModelRequest` 返回模型可执行的 static belt，并返回稳定、可恢复的用户提示。
3. 先将 effectful static inventory family 放入 quarantine deny；审计记录只含 host kind、revision、delivery state 和原因码，禁止保存 task/path/args/IDs。
4. 写 local/remote 的 negative conformance：started 后 timeout、cancel、stream error、fallback、retry、同名旧调用、steer race；另写 `not_sent` 的正向重试。断言无第二个 executable surface、无 legacy dispatcher 命中、无 UI/control-plane mutation。
5. 只有合格 adapter 的 S1-C conformance（§9.6）通过后，才把同一 family 从 S0.5 quarantine 迁入真正的 `Publish -> Bind -> Admit -> Journal`；不得把 S0.5 作为长期替代。

完成状态只能称为 **S0.5 ambiguous-delivery containment**。它比“只比较当前名称集合”更能避免自动 retry/fallback 在不确定旧请求之后扩大执行面，但无法识别 wire-level response、不能安全重放，更不能解锁 static semantic cutover 或 dynamic Skill/MCP。

#### 9.9.5 已落地：shared-loop observer 与 local/remote quarantine（2026-08）

首个实现已把 S0.5 的最小 observer 放在 shared `RunLoop` 的实际 request/fallback/retry 边界：请求开始时通知 `started`；正常返回并即将消费 response 时通知 `response_consumed`；开始后的 error 或 fallback 前失败通知 `ambiguous_delivery`。observer 不接受模型参数，也没有生成/填充任何 connection、response 或 tool-call ID。

local/remote Coding callbacks 选择 containment 后，收到 `ambiguous_delivery` 会原子清除当前 static rendered-name set 和 epoch，并置本 callback terminal quarantine；随后 `BuildToolsForModelRequest` 返回空 surface，任何同名 legacy call 都在 dispatcher 前得到 `static_surface_unavailable`。shared loop 同时禁止该 callback 在同一 turn 自动走 streaming fallback 或 outer transient retry，因此不会先 render 一张 successor static surface 再期待旧请求消失。回归覆盖正常消费、ambiguous failure 不产生第二请求，以及 local/remote quarantine 的零 successor surface / 零 dispatcher 命中。

这仍是本地 containment，不是对 HTTP 投递的证明：目前没有 adapter 能报告可信的 `not_sent`，也尚未完成 cancel/steer/connection-loss fault injection、effectful family 的正常模式收缩或 S1-C durable lifecycle。故不要将“quarantine 已接线”表述为 correlation、journal 或 static semantic cutover；dynamic Skill/MCP 继续 fail-closed。

#### 9.9.6 已落地：normal compatibility surface 的 effectful-family 收缩（2026-08）

S0.5 不能只在失败后 quarantine：若正常 HTTP/SSE request 仍能把 `write_file`、`bash` 或 `ssh_bash` 交给 legacy dispatcher，same-name late response 的风险仍会落在 effectful I/O 上。现已在实际 `BuildToolsForModelRequest` 的最后 render 边界接入 closed static inventory policy：除已有独立 S3 状态机的 control-plane 外，只有 `EffectReadOnly` family 可进入无 correlation 的 Coding static surface。

local 因而不再 render `edit_file`、`edit_lines`、`write_file`、`bash` 或 `download_file`；remote 不再 render `ssh_write_file`、`ssh_edit_file`、`ssh_bash` 或 `download_file`。同一策略也在 execution fence 上自然生效，因为被移除名称不在 rendered name set。LongHorizon 的 `computer_*` / `browser_*` 不属于 Coding static inventory，继续受 frozen episode policy + actual registry render 双重约束，未被本策略伪装为 read-only Coding capability。

回归覆盖 local/remote effectful name 零暴露、直接调用 `static_surface_unavailable`、所有 posture/role inventory owner、既有 epoch/quarantine fence 与 Horizon surface。此处保留 S3 `todo_write` / localization / spawn 的既有专门 gate，不将它们误归类为 workspace/provider I/O；它们仍没有 response-correlated replay 或 durable owner journal。下一步仍是合格 S1-C adapter 和对应 family 的真正 fixed-executor cutover，而不是重新开放 legacy writes/commands。

复审后修订最后一句中的 spawn：`spawn_coding_agent` 不能与 todo/localization 一样继续留在 normal S0.5 surface。它创建 child runtime attempt、可能申请 isolate/worktree/approval 并启动 worker，因而即使其 inventory scope 是 `parent-lineage`，也属于无法由 callback-local revision CAS 收敛的 effectful lifecycle。现已将它从 local/remote `BuildToolsForModelRequest` 移除；name-set execution fence 使其直接模型调用稳定返回 `static_surface_unavailable`。child lineage、approval isolation 和 parent-running 检查继续保留在 host/runtime admission 层，但这些检查不再被误当作无 correlation transport 上的 replay/admission 证明。

因此当前 uncorrelated static compatibility 仅保留 read-only family，以及 `report_localization` / `todo_write` 等具有明确 S3 callback-local state contract 的少数 control-plane 操作。future S1-C 必须为 spawn 提供 response-bound admission、durable child-attempt journal 和 unknown/cancel settlement，才可讨论重新 materialize；dynamic Skill/MCP 仍维持 fail-closed。

对 retained control plane 的随后审计修正了 `report_localization`：其 stored evidence 虽带当前 revision，但 definition/arguments 没有 `expected_revision` / `expected_version` compare-and-apply token；同一 rendered surface 的迟到/重复 call 仍可能覆盖更晚的 evidence。因此它不能与 `todo_write` 并列为 S0.5 可保留操作，现已从 local/remote model surface 移除，任何模型可达 name dispatch 均返回 `static_surface_unavailable`。host/internal 仍可调用该 helper，且旧 revision evidence 仍被 edit gate 拒绝；这两点不是 transport replay 安全证明。

当前无 correlation belt 因而只保留 inventory `read_only` family 和 `todo_write`：后者的 rendered schema 必须包含 expected revision/version，执行采用 compare-and-apply，重复或过期 payload 返回 `control_plane_stale` 而非覆盖 state。该范围是刻意最小化的 compatibility residue；`report_localization`、spawn、writes/commands 与 dynamic Skill/MCP 都等待 future S1-C 的真实 response-bound lifecycle、admission 和 journal。

#### 9.9.7 已落地：prompt / task message / trajectory snapshot 的 request-surface parity（2026-08）

`BuildTools` 仍保留 direct-host、旧 workflow 与单元测试所需的完整静态定义；它不是当前 HTTP/SSE 模型请求的 authority。此前 `BuildSystemPrompt`、implementation preflight、full/nested prompt 和 `startSubAgentTrajectory` 却仍按这一较宽表面描述能力，导致 model-visible definitions 已删除 `write_file`、`edit_*`、`bash`、`ssh_write_file`、`ssh_edit_file`、`ssh_bash`、`report_localization`、`spawn_coding_agent` 后，prompt 或轨迹仍把它们说成可调用的幽灵能力。

现行无 correlation callback 在 render 前统一进入 compatibility prompt：仅指导实际 read-only family、CodeGraph 优先和（worker definition 出现时的）`todo_write` CAS。实现/运行类请求不再要求模型尝试不可用的写入/命令/派生/定位提交，而是要求先形成可核查 evidence，说明临时 safe-mode blocker，并输出交给 future correlation-bound execution workflow 的最小步骤。Horizon 保持独立 frozen episode prompt；该选择只根据 adapter 的 transport contract，不从 task、path、LoopContext、runtime ID 或模型工具名推导。

trajectory 仅记录同一受限定义的 audit snapshot：调用 direct-host `BuildTools` 后施加 `filterUncorrelatedCodingStaticCompatibilityEffects`，但绝不调用 `BuildToolsForModelRequest`，以免记录动作推进真实 callback revision、name set 或 epoch。新增 local/remote regression 断言 prompt 和 snapshot 均不宣传上述不可用名称，仍保留 `read_file`/`ssh_read_file`、`code_navigation` 和 `todo_write` 指引。

该项只是 surface-parity 修复：prompt、task message 与 trajectory snapshot 都不签发 executor、binding、grant、correlation 或 journal；真正恢复 effectful family 仍必须经过合格 S1-C adapter 的 response-bound publication、admission、unknown-effect settlement 与 durable journal。

#### 9.9.8 已落地：request-local telemetry parity（2026-08）

prompt、task message 与 trajectory snapshot 已与受限 compatibility surface 对齐后，仍必须避免 observability 从宽 `BuildTools` inventory 重新引入幽灵能力。`LoopInputBreakdown` 用于 token/cost 归因与 routing audit；若在 `BuildToolsForModelRequest` 前采样，它会报告 direct-host/workflow 所需但当前 HTTP/SSE 模型从未见到的 definitions，进而把容量、收敛和事故诊断导向错误的工具面。

shared `RunLoop` 现仅在已有的真实 request-local conversation 与 definitions 上记录 breakdown：初始 streaming request 在 render 后、发送前采样；实际进入 non-stream fallback 时，fallback 的重新 render surface 单独采样；outer transient retry 也以其新 render surface 单独采样。MoA fan-out 注入 advice 后，聚合调用和 telemetry 共同使用最终的 `reqConversation`。每条 telemetry 记录因此一一对应一次实际模型请求；被 S0.5 containment 阻断、处于 backoff、或仅为 trajectory snapshot 调用的路径不产生记录。测试覆盖宽 inventory/窄 request surface，以及 streaming failure 后 fallback 的两次 request、两条对齐 breakdown。

这项修正只改善可观测性和审计证据：breakdown 不会调用 renderer、推进 callback revision、创建 epoch、授权 name dispatcher，或填充任何 transport ID。它不构成 `ConnectionID`、`ResponseID`、`ToolCallID` correlation，也不改变 S0.5/S1-A/B/S1-C 的状态；dynamic Skill/MCP 继续 fail-closed。

#### 9.9.9 已落地：Responses SSE provider response-ID 透传（2026-08）

S1-C 审计还发现 Responses 的 stream/non-stream parser 不对称：non-stream response 会保留 provider `id`，stream 却未把 `response.created` 或 `response.completed` envelope 中的 `response.id` 传回 shared loop。该丢失不能以本地 `SurfaceEpoch`、iteration、request/LoopContext/runtime ID 或 call name 补齐；它只是 parser 侧遗失了本已由 provider 发出的事实。

已将稳定的 event-envelope `response.id` 透传至 `llm.Response.ResponseID`，并拒绝同一 SSE stream 内互相冲突的非空 ID。完成后的 `RunLoop` 才能把 Responses SSE 的 provider response ID 交给已有 `ToolSurfaceResponseBinder`；缺 ID 仍为缺 ID，binder/dynamic admission 必须保持拒绝。回归覆盖 completed ID 保留与冲突拒绝。

这不是 S1-C adapter：当前 HTTP/SSE transport 仍没有可证明的 connection lifecycle，不能把 parsed response ID 与 request send、transparent retry/fallback/reconnect、late response 及 tool execution journal 作为一个唯一 lifecycle 验证；Coding callbacks 也没有因此开启 dynamic aliases。它只消除了一个会掩盖真实 blocker 的 parser 缺口，dynamic Skill/MCP 继续 fail-closed。

#### 9.9.10 已落地：Coding request owner 禁止 SDK 隐式 successor request（2026-08）

S0.5 shared loop 已经为 outer retry 与 streaming fallback 建立了 request-local render、attempt observer、quarantine 与 breakdown；但 OpenAI-compatible SDK 仍可能在同一次 helper 调用内为兼容 400 自动重发 tool-less、compact-message 或较小 token 的请求。这个 successor request 未经过 `BuildToolsForModelRequest`，也没有新的 static revision/name-set/epoch 或 telemetry，等价于绕开 model request surface owner。

local/remote 共用的 `codingLoopLLMRequestContext` 现使用 context-scoped `WithTransparentRequestRetriesDisabled`。对带此标记的 OpenAI stream/non-stream helper，第一次实际请求失败后不再私自进行 compatibility repair；原错误回到 `RunLoop`，由 S0.5 ambiguous-delivery containment 原子清空 static belt、阻止自动 successor surface。该 policy 同时关闭 `net/http` 的 307/308 自动 POST redirect：首个 3xx 交还 loop，而不是让 client 绕过 request renderer 把同一 body 发送到新 URL。Responses stream 同样使用该受限 client。普通非 Coding 调用不受影响，仍保留原有 provider-compat repair 与 redirect 行为。回归覆盖 stream/non-stream 单 attempt、Responses redirect 以及 Coding context 标记存在。

该机制既不判定请求“未发送”，也不将 3xx/400/timeout/network failure 变成 replay-safe，更不会以 context 标记伪造 `ConnectionID`、`ResponseID` 或 `ToolCallID`。它只让每个实际 Coding HTTP attempt 都重新回到 shared loop 的可观测、quarantine 与 surface ownership 边界；S1-C correlation/admission/journal 仍未完成，dynamic Skill/MCP 继续 fail-closed。

随后的 parser 审计补齐了 response-ID 的 fail-closed 一致性：Responses SSE 已拒绝同流不同的非空 `response.id`；OpenAI chat SSE（含 stream 参数被兼容端点忽略后的 SSE parser）与 Anthropic message SSE 现在也会冻结第一个 provider response ID，后续 ID 变更立即报错。不得把这项解析层校验误作 connection lifecycle：空 ID、无可信 connection、缺 tool-call ID 时依旧不能 materialize 或执行 dynamic alias。

#### 9.9.11 已落地：G3 response-bound surface 的 durable recovery helper（2026-08）

`ModelRequestSurface` 已把 alias/grant、route、epoch、protocol、connection 和 response binding 持久化，但早期 `codingDurableDynamicSurface` 仍需要进程内 `plan/scope/aliases/definitions` 才能继续。若在 response 已 bind、tool call 尚未固定桥接时重启，绝不能从 `matchedSkills`、`matchedMCPTools`、`dynamicSurface.byName`、项目路径、LoopContext/runtime/request ID 或 provider/tool 名补回执行权。

现已在 coordinator 增加 `RecoverBoundModelRequestSurface`。它只接受 authenticated tenant 与 transport-owned `{Protocol, ConnectionID, Epoch}`，且只恢复同 tenant、`active`、已绑定非空 provider `ResponseID`、route 仍 current 的 durable surface；`prepared`、`finished`、`superseded`、`cancelled` 和 scope/tenant 不匹配统一为 `stale_surface`。返回的 alias grant 仍只能经 `ResolveAlias → issuer.Validate → coordinator.Admit → HostCall journal` 使用；恢复动作不消费 grant、不会发起 provider I/O、更不能创建名称 dispatcher。

GUI 的 G3 wrapper 仅从该 durable record 和 published plan 重建最小 correlation holder，明确留下 definitions、in-memory alias map 与 dynamic catalog 为空；fixed bridge 仍要在 provider I/O 前重新读取 lifecycle-owned catalog，失败仍 `catalog_incomplete`。回归覆盖 coordinator restart 后 active surface 的 durable resolve、错误 tenant、finished surface，以及 prepared/superseded/cancelled surface拒绝。

此项仅关闭“response 已绑定后 host 重启”的 G3 数据面断裂，**没有**接线到 CodingSubAgent/RemoteCodingSubAgent 的 production callback，也没有让 HTTP/SSE parser 成为 transport lifecycle adapter。故仍不能宣称 S1-C、static semantic cutover 或 dynamic Skill/MCP materialization；legacy dynamic dispatch 继续拒绝 `catalog_incomplete`。

#### 9.9.12 已落地：shared loop 的单次 request-channel S1-C seam（2026-08）

此前 `ToolSurfaceExecutionContextProvider` 只能为 callback 提供 metadata，不能证明该 `{Protocol, ConnectionID}` 属于实际发送本次 definitions 的 transport；renderer、SDK fallback 或 helper send 若分属不同对象，仍可能产生“记录的是一个连接、发出的是另一个请求”的伪 correlation。

shared `RunLoop` 现提供可选 `ToolSurfaceRequestChannelProvider` + `BoundModelRequestToolSurfaceRenderer`。未来合格 adapter 必须先 reserve 一个单次 channel，从 live transport 给出非空 protocol/connection；RunLoop 生成新 epoch 后，才把完整 context 交给 bound renderer 以 publish durable surface，随后仅通过该 channel 发送一次请求。该 channel 不允许透明 retry、redirect、reconnect 或 fallback；任一 successor 必须重新 reserve 新 channel、重 render/publish 新 surface。response 回来后，parser 的 provider `ResponseID` 仍由 RunLoop 先写入 binder context，tool call 再走 fixed bridge。

core conformance 已覆盖同一 reservation 的 protocol/connection/epoch 贯穿 render、bind 和 dispatch，successor 使用新 connection/new epoch，以及 channel failure 不产生 fallback/retry/successor render。该 seam 仅提供 S1-C 的可实施生命周期骨架；现有 local/remote Coding callback 尚未实现 provider、HTTP/SSE 仍在 S0.5 compatibility belt，dynamic aliases 继续 fail-closed。

`responses-ws` 的 parser 同步保留 lifecycle envelope 中 provider `response.id` 并拒绝同一 stream ID 冲突，以免前后 response 混合。当前 WS helper 每请求拨号且没有接入上述 request channel；这仍只是 parser evidence，不能由 `WireAPI=responses-ws` 或 socket URL 升格为 adapter correlation proof。

其后已将 WS helper 的拨号/单次 exchange 抽成实际 `responsesWSRequestChannel`：`DialContext` 成功后才创建 host-owned opaque connection ID，并将它与这一个 live socket 共同保留；同一 channel 第二次 `Do` 一律拒绝。测试断言 connection ID 不含 URL、model、provider ID/name，且只能执行一条 `response.create`。不过 current Coding callback 还未将 verified identity、durable surface publish/bind、fixed executor 接入该 primitive，因此 matrix 明确标识为 `responses-ws-channel-available-not-wired`；不因 socket 能建立就打开任何 dynamic alias。

#### 9.9.13 下一切片门禁：以 test-only bound adapter holder 连接已有原语（2026-08）

下一切片不是将 `responses-ws` matrix 改为 eligible，也不是让 local/remote callback 先实现 channel provider，而是先以一个单次 reservation 的 test-only holder 闭合 `{verified identity, channel protocol/connection, epoch, durable surface, response binder, fixed context executor}`。renderer 只能在该 channel 上 publish；binder 只能接收该 channel parser 给出的 provider `ResponseID`；dispatch 必须精确校验 protocol、connection、epoch、response、非空 tool-call ID，并且只能调用 `ExecuteBoundSelection`。执行前重新观察 lifecycle-owned catalog，绝不从 match/by-name map 恢复 binding。

channel close/error、bind failure、cancel、steering、nested exit 与 supersede 必须经过同一 coordinator 生命周期入口撤销 aliases/materializations/未消费 grants；每个拒绝回归均断言零 provider I/O、零 grant consume、零 successor executable surface。该 holder 完整通过前，`codingDynamicAliasesMayMaterialize()` 保持 `false`，所有 production Coding callbacks 仍走当前 S0.5 static compatibility belt。详见 [S1-C 生产就绪性复审与改进方案](semantic-tool-routing-s1c-production-readiness-review-zh.md)。

首个 holder 已实现为 `codingBoundDynamicRequestAdapter`，并且仅由 focused GUI tests 构造：它没有 callback 注册、没有 capability matrix 资格提升、没有按名称的 provider map。测试覆盖 publish → response bind → fixed bridge 的 tuple/epoch 一致性、未 bind/错误 protocol/connection/epoch/response/空 call ID 的 `stale_surface` 拒绝、无效参数的 durable journal replay，以及 bind failure/close 后不能 render 或消费 predecessor alias。live WS channel 与该 holder 的 production callback 接合仍未开始；故 release gate 不变。

现已补一条 live WS 的 test-only 组合回归：holder 包装 reserve 后的真实 socket channel，以其 `ExecutionContext()` 的 tuple 发布 definitions，并从同一 holder 发出唯一 `response.create`。provider `ResponseID` 回填 binder 后才进入执行域；第二次 `Do` 必须由该 reservation 拒绝；cancel 后 holder 和 durable surface 都拒绝 predecessor alias。该测试不让 CodingSubAgent/RemoteCodingSubAgent 实现 channel provider，也不改变 matrix 或 `codingDynamicAliasesMayMaterialize()==false`。

为使后续 production 接线不能只改 matrix，一并加入 `codingDynamicProductionAdapterQualification` 和唯一 future factory。registry 与 `codingDynamicProviderCorrelationCapability` 分离：前者表达 callback 是否已实际装配并被发布批准，当前固定 `Wired=false`、`Enabled=false`；factory 在此 gate 前直接返回，故不会 dial WS、读取 catalog 或发布 alias。调用方没有可传入的“资格”参数；local/remote callback 的五接口 composition 仅为 test-only 形状，仍没有 production approved-ingress registration。定向回归锁定该 fail-closed 状态。

holder 的 `Close(non-nil cause)` 现先取消专属 bridge context，再退休 durable route 和关闭 channel；G4 在 fresh catalog observation 前检查该 context。这样 admission 与 close 的竞态不会令已经 admitted 的 call 在取消后重新进入 provider I/O。测试验证 cancellation context、`stale_surface` 拒绝及 alias durable resolve 均同步失效。该项仅为 test-only lifecycle closure；实际 local/remote steering、timeout、nested exit、runtime terminal hook 接线仍未开始。

下一层接线也已定义为封闭 API：`CloseForLifecycle(steered | nested_exit | runtime_terminal)`。无论已命名 lifecycle 或未知输入，holder 都 fail-closed 到同一 cancellation/route-retirement 操作；不得把 task text、SSH/request/loop ID、工具名或 provider config 作为 lifecycle reason。回归覆盖每一个 reason 后 context cancelled、holder terminal、predecessor alias 不可 resolve；production callbacks 尚未调用它。

现已增加 `codingBoundDynamicRequestLifecycleRelay`，以一个对象实现 future callback 的 request-channel provider、bound renderer、response binder 与 context executor。relay 只能持有 factory 返回的同一 live holder；缺 transport-owned protocol/connection 时关闭 holder并拒绝，不把 config、static epoch 或 runtime ID 伪造为 connection。qualification disabled 时构造 helper 返回 nil，故 local/remote callbacks 仍没有该 relay，也不发布/执行 alias。

#### 9.9.14 复审新增 blocker：channel close 不等于 semantic surface 已结算（2026-08）

进一步核对 shared `RunLoop` 可知：request channel 的 `Do` 返回后会立即 `Close(nil)`，而 provider response 的 response-ID bind、空 choices 判定、steering discard、最终文本提交和工具 batch 提交发生在后续分支。`Close(nil)` 只释放 live transport，不能被当作 prepared/bound durable surface 的 completion 或 cancellation 证据。若 future callback 现在注册 relay，response 在 binder 前被 steering/return，或无工具 response 结束时，active holder 没有统一的 semantic terminal owner；这会留下跨下一轮的 alias/grant 生命周期空窗。

该前置已以 test-only seam 落地：shared loop 提供一次性、host-owned `ToolSurfaceDispositionObserver`，且每个 reservation 仅能收到一个封闭 outcome：`response_abandoned`、`response_settled`、`tool_batch_settled`、`steered`、`runtime_terminal` 或 `transport_failure`。relay 是这个 hook 的唯一接收者，并把所有 outcome 收敛到 `CloseForLifecycle → CancelRouteSurface`；不得在 local/remote callback 的各个 return 分支自行清 alias，也不得由 task text、config、runtime ID 或工具名称推断 outcome。定向回归已断言 empty-choice abandon、finalization steer 后 successor settle、tool batch settle、transport failure，以及 relay 的 wrong tuple / duplicate 通知都不能触碰 active/successor holder。

binder failure 也属于同一 lifecycle：binder 可以先退休 durable holder，随后的 `response_abandoned` 仍必须按 exact reservation tuple 清除 relay 的 active ownership，不能因为 holder 已 terminal 就留下阻塞 successor 的指针。此清理不改变 executor 的 terminal rejection；回归锁定该 fail-closed sequencing。

继续穷举 reservation 后的 recovery branches 后，截断 tool call、参数 JSON 非法、以及空响应已到 iteration limit 都显式发出 `response_abandoned`。它们不会执行模型宣告的 tool，也不会将当前 durable request surface 结算成 final/tool-batch；不能仅因为 loop 继续下一轮就遗留 alias。

这仍不是 production 行为改变：local/remote callbacks 尚未注册 relay，production qualification 仍 disabled，dynamic aliases 继续零 materialization。下一个切片只能是 verified desktop ingress 的真实 lifecycle registration 与全路径 conformance，不能通过提升 matrix 或 feature flag 跳过。

#### 9.9.15 修正方案复审：以完整 replacement surface 治理多轮连续性（2026-08-23）

本轮复审确认，Coding 工具“越聊越不完整”不能靠增加 `cachedTools` 生命周期、按 task keyword 补工具或恢复旧 `manage_skill`/`call_mcp_tool` 解决。它们都会让历史工具名参与下一次 capability selection，直接违背 S0/S1 的计划驱动边界。

应将 Coding 的连续性拆开处理：

- 可连续的：任务目标、已验证工件/evidence、已 durable settle 的 tool result、未知 effect 标记和当前 plan revision；
- 不可连续的：rendered definition、opaque alias、未消费 grant、connection/response/tool-call tuple、未提交 batch checkpoint 和 callback 私有 name map。

每一轮必须从当前 immutable plan 重新输出完整 static compatibility surface；未来 qualified dynamic surface 也必须从同一 plan 经 channel reservation + epoch publish 重新 materialize。上一轮的 executable surface 无论是成功、失败、steer、cancel、route supersede、nested exit 还是 batch commit failure，均不得为下一轮提供补全或 fallback。

实施顺序保持如下：先完成 D2c 的 real-holder batch durability / terminal conformance，再以 D3 fixed cohort 验证真实 verified ingress 与 kill switch，最后才逐 family 删除 compatibility name dispatcher。D2c 的最低验收是 local/remote 对称地证明：starter 失败零 I/O、committer 失败非 settled、cancel/steer 不会提交 batch、正常完整 batch 恰好一次 settled，且每条 terminal 后旧 alias 均为 `stale_surface`、零 provider I/O/零 grant consume/零 static-name fallback。未达到该矩阵前，不得改变 `codingDynamicAliasesMayMaterialize()==false` 或 production qualification。

完整的状态机、ledger key 与发布门禁以 [S1-C 生产就绪性复审与改进方案](semantic-tool-routing-s1c-production-readiness-review-zh.md#13-第二十一次复审把多轮工具不完整归因到-surface-ownership而非缓存) 为准；本文件不另建 Coding 私有 alias store、batch journal 或 identity 推导路径。

首批实现已将 local/remote actual callback composition、real holder/relay 与 shared `RunLoop` 放进同一 qualification-disabled 测试夹具：starter failure、committer failure、complete paired commit 与 request 已发送后的 runtime terminal 均按 exact reservation tuple 得到唯一 disposition，终态 alias 不能回落 static/name dispatch；nested exit / route supersede 同样不会让迟到 predecessor terminal 触及 successor，两个 root/turn route 共用 coordinator 时也不会互相取消，commit 后的 steer 也不能错误结算为 batch settled。restart 只恢复 durable bound authority，绝不恢复 process-local definitions/alias map/catalog，terminal route 也不可恢复。verified runtime ingress 到 actual `codingagent.Run` 的完整 batch 回归现已证明同一 identity/relay/holder 的闭环；但 cancellation、binder failure、early return 与 child handoff 的 verified-ingress 组合审计仍待完成，因而不得提升 production qualification 或 dynamic alias gate。

production cutover 的 registry 也不再只是 `Wired/Enabled`：它要求 correlation capability 与 adapter version、verified ingress、disposition conformance version、catalog/receipt coverage、opaque fixed cohort、kill-switch proof 同时齐备。factory 和 `codingDynamicAliasesMayMaterialize()` 共用该 predicate；任何 LLM 配置、task/runtime ID、用户字段或 caller flag 都无法填补 release evidence。当前 registry 返回空 evidence，故 local/remote 的现状不变。

#### 9.9.15a 复审补充：definition render 不能替代 outbound payload 证明（2026-08-23）

本文件原有“每轮从 immutable plan 重新输出完整 surface”的约束仍不够：若 SDK 把 `tools` 视为 append、缓存合并或最终 serializer 丢失一个 definition，callback 即使调用了正确 renderer，模型仍会看到不完整面。修复必须增加全局统一的 `SurfaceManifest` / `SurfaceReceipt`：manifest 明确本轮 selected static + dynamic definitions、合法 omission 与 `Replace` 语义；sender 在真实 `Do` 前验证 canonical manifest digest 等于最终 wire payload hash。没有 matching receipt 的 response 不得 bind/dispatch，任何 mismatch 必须 fail-closed，绝不按 name fallback。

该合约先覆盖当前 S0.5 静态路径，未来 holder 的 dynamic definitions 只是同一 manifest 的一个分量；它不保存或恢复 alias/grant/tuple，也不以 digest 充当 identity。D2.5 应在 D3 前完成 local/remote repeated-turn、plan/budget change、empty surface、steer/retry/cancel/redirect 和 payload mutation 回归。完整不变量、kill-switch 对 in-flight effect 的边界及修订阶段表见[完整工具面与请求所有权复审](semantic-tool-routing-surface-ownership-review-zh.md)。

实现已先落到两处最终发送边界：static compatibility 的 HTTP `RoundTrip`，以及 test-only holder 可复用的 Responses WS `response.create` frame → `WriteMessage` 之间。两者均强制 explicit empty replacement，并对 drop/append/mutation 失败关闭；这一项不会开放 `manage_skill`、`call_mcp_tool` 或任何 dynamic alias。verified-ingress ledger 和 production cohort 证据仍待完成。

#### 9.9.15 复审补强：D 阶段按完整 callback composition 原子切换（2026-08）

为避免 future 实现在 local/remote 两端逐个注册 request channel、renderer、binder、executor 而形成半接线，两个 callback 现仅增加 inert `dynamicLifecycleRelay` 装配字段。它只在 verified identity 已安装后尝试读取 app-owned qualification；当前 gate disabled 时为 `nil`，不会产生任何 transport、catalog 或 alias 副作用。此状态必须标记为 `implemented-not-wired`，不能称为 dynamic lifecycle 已接入。

真正 D 阶段的最小单元不是“打开一个 feature flag”，而是一个一次性实现并委托五个接口的 composition adapter：request-channel provider、bound renderer、response binder、context executor 与 disposition observer 必须共享同一 relay；任何一个未接通就整体保持 S0.5 compatibility。随后才把 verified ingress bind、cancel、steering、nested exit、runtime terminal 与 tool-batch settlement 汇入同一 owner，并以固定、不可由 user/task/config 推导的 cohort 做 kill-switch 演练。完成前 `codingDynamicAliasesMayMaterialize()` 必须维持 `false`，旧 name dispatcher 不得成为兜底。

该 composition 的 `test-only` 形状现已落地：local/remote callback 同时实现五个接口，relay 为 nil 时整体 inert，relay 存在时所有入口使用同一个 holder owner，且 context executor 对迟到的静态名称同样只返回 `stale_surface`。当前 qualification disabled 使生产运行始终落在 nil-relay/S0.5 分支。

D2 的宿主终态桥也已开始：每个 SubAgent 有 execution-scoped owner，只保留已安装 relay；LoopContext cancel、detached runtime context cancel、callback terminal defer 和同步 nested handoff 都走同一 `CloseForLifecycle`，并按 exact relay ownership 清理，防止旧 task terminal 误关 successor。该 bridge 没有也不得从 task/SSH/path/runtime ID/provider config 造 semantic identity 或 channel tuple。现阶段仍缺 approved ingress 的真实端到端 disposition/settlement proof，故 production qualification 和 alias materialization 继续关闭。

#### 9.9.16 D2a 已关闭：pure Coding 以 RunLoop revision 协议处理 steering（2026-08-23）

此前 `codingSubAgentHooks.TransformConversation` 可以把 pending guide 拼入 local/remote Coding conversation，却没有与 `LoopContext.RequestReplan()` 的 revision 协议连接；两种 callback 也没有实现 `agent.LLMReplanAware` 与 `agent.LLMFinalizationGuard`。这会使 shared `RunLoop` 无法可靠取消进行中的 Coding request、在 response 丢弃/工具批次之前发送 `ToolSurfaceSteered`，或以原子方式拒绝 final text 与 late steer 的竞争。若 relay 取得 live holder，该缺口会使 predecessor surface 的 disposition 依赖偶然 return path，违反 D2 的 exactly-once 要求。

改造不允许 callback 直接把 steer 翻译为 `CloseForLifecycle`：正确 owner 是 `RunLoop`。local/remote callback 共同维护仅来自 host `LoopContext` 的 observed revision；hooks 必须在 drain 前快照、完成 transform 后只提交该快照（空可见 payload 的 accepted steer 也在该边界消费，drain 期间新到 revision 保持可见）；`LLMRequestContext` 在 scheduler 等待前安装可取消 operation；`LLMReplanRequested` 用 `ReplanRequestedSince` 检查；`TryFinalizeLLMResponse` 用 `TrySealReplans` 关闭 final-response race。`RunLoop` 因而是唯一发出 `steered` disposition 的位置，relay 只按 exact reservation tuple 退休 holder。

D2a 的 local/remote 对称回归已通过真实 `codingagent.Run` 覆盖 request context 的 live steer cancellation、successor 注入内容、finalization 拒绝，以及已消费/后续 steering watermark。D2b 的首个 callback/holder conformance 也已通过：qualification-disabled 的 test-only relay 使用 real holder、channel tuple 和 durable coordinator；仅测试可见的 `{Protocol, ConnectionID, SurfaceEpoch}` reservation ledger 证明 predecessor 恰有一次 `steered`，其 alias 为 `stale_surface`，而重复/迟到 predecessor disposition 不会关闭新 reservation。D2 仍未完成：必须补 tool-batch durability、request 中 steer、transform/request race、response/finalization race、cancel/steer race、runtime/nested/route terminal 的全组合。每项负向断言均包含零静态 name-dispatch fallback、零额外 provider I/O/alias materialization。即使 D2 完成，仍须 D3 的 fixed cohort 与 kill-switch production conformance；此前 dynamic Skill/MCP alias 继续 zero materialization。

### 9.10 第七次复审修订：本地桌面已验证 ingress 的 correlation-gated 静态面恢复（2026-08-25）

**背景事故**：S0.5 的 `filterUncorrelatedCodingStaticCompatibilityEffects` 对所有非 horizon 请求无条件生效，把"桌面 in-process 全功能编程工作台"（Wails 签发 verified task relation + host workspace binding + scope approval + audit）与真正无 correlation 的远程 HTTP/SSE 适配器一刀切。生产症状：编程子代理只拿到只读工具，模型按兼容提示词输出"请启动后续执行工作流"的计划文本，宿主却因"有检索证据"判 passed——零产出绿勾，且空跑被技能自学习沉淀为"只写 todo 就停"的坏配方。

**关键事实变化**：S0.5 收口的前提"Coding 拿不到 request/response 相关性"对本地路径已不成立。shared `RunLoop` 在每个真实出站请求边界 mint surface epoch（`BeginToolSurfaceEpoch`），并在派发任何工具调用前把 provider wire 的 `ResponseID` 写入 `ToolCallExecutionContext`（解析器对同流冲突 ID fail-closed）。本地 in-process 回环由此具备请求级相关性：迟到/重放 response 因 epoch 不匹配被拒，ambiguous delivery 触发 quarantine，名称围栏只认当次渲染面。

**决议（经产品 owner 评审）**：新增 host 血缘事实 `correlatedLocalExecution`——仅当 root attempt 从 verified desktop ingress 解析出 durable identity 且持有 host 签发的 local workspace binding 时置位；嵌套 child 只经宿主 spawn 路径继承该事实，绝不复制 parent 的 identity/handle/surface/grant，也绝不从任务文本、配置、路径或 runtime ID 推导。该事实为 true 时，本地静态面恢复渲染 effectful family（write/edit/bash/download/spawn），否则维持 S0.5 只读兼容面。

**配套门禁与诚实性约束**（同一切片落地，缺一不可）：

1. 派发双校验：inventory 中所有非 `codingStaticCompatibilityItemAllowedWithoutTransportCorrelation` 的 family，在 `ExecuteToolCallWithContext` 必须同时携带非空 `SurfaceEpoch` 与 `ResponseID`，缺一即 `static_response_correlation_missing` fail-closed；该校验在所有面上生效，含已恢复面。
2. 兼容面诚实判败：uncorrelated 面上的 implementation/operational 任务，无文件且无命令证据时不得判 passed（该面提示词本就指示模型只出计划，而产品里并不存在所谓"后续执行工作流"）。
3. 技能自学习守卫：零生效证据（无写/无命令/无 spawn）的 coding 会话不进入技能沉淀，防止把"检索后停止"固化为配方。

**明确不解锁**：dynamic Skill/MCP alias 维持 zero materialization（S1-C qualification 仍 disabled）；remote Coding 维持完整 S0.5 只读收口；epoch-less 的宿主维护直调不得携带 effectful 调用（本切片前 `executeLoopTool` 已无生产调用者）。S1 静态面语义化迁移（planner/materialization 全链）仍是终态；本节是把本地桌面路径从"误伤的 containment"恢复到"有 correlation 证据的受控执行"，不是放弃迁移。

**回归证据**：`gui/coding_correlated_runloop_test.go` 以真实 `agent.RunLoop` + 带/不带 wire `id` 的假 provider 验证写盘成功/拦截；`gui/coding_correlated_local_surface_test.go` 覆盖渲染面、门禁、诚实判败与技能守卫；既有 uncorrelated local/remote containment 测试不变且全绿。
