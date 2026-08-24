# 虚拟动态服务组技术方案

状态：**设计已改口**。分类要分对（全局头），分到哪儿仍是各组路由。V1 / V1.1 与按组一头代码仍在仓库；全局头 + 独立管理页未落地。
配套：[`llm-provider-multiplier-lb-zh.md`](llm-provider-multiplier-lb-zh.md)（L3 服务商 WRR）、[`../cost-route-phase1.md`](../cost-route-phase1.md)（桌面 C0–C3，不得双调度）。

## 1. 摘要

服务组已经能在同一逻辑模型下对多个服务商做倍率带 WRR。缺的是：用户只面对一个服务组，系统按业务类型换逻辑模型，压成本。

对外一个 `auto`，对内按 WorkloadClass 路由。路由表就是允许集合，不得逃出名单。组内换的是服务商和模型，不是嵌套组。

| 决策 | 定义 |
| --- | --- |
| 组与商 | 服务组是扣费单元，服务商是提供者。组内只能加服务商。`maclaw_official` 是服务商。 |
| 组形态 | `kind=dynamic` + `routes[]`。禁止 `members[]`。 |
| 词表 | `plan` / `design` / `review` / `doc_write` / `code` / `ops` / `chat` / `classify`。`balanced` 是默认档，不是第 9 类。 |
| 代码档位 | 写代码中档；规划 / 设计高质量。 |
| 扣费 | 按命中的服务组、解析后逻辑模型的 `credit_multiplier`。与选了哪家服务商无关。 |
| 无 hint | 走 `routes.balanced`，不升 `plan`。 |
| L1 | 命中哪一层动态组，谁分类。禁止双边各跑一次。chat 与 responses 同一套。 |
| 官方入租户组 | 管理员把官方服务商挂进 `provider_configs`，`model` 只许三档名。平台不往每个租户组塞官方。 |
| 选中官方后 | 转发 `official-high` / `official-mid` / `official-low`，禁止再传 `auto`。必须带 `X-MaClaw-Workload-Class` 供 HubCenter 记账，HC 不得因此再 L1。 |
| 官方 Auto | 升级现有 HubCenter 官方组，不新开 SKU。 |
| Hub 官方门面 | `maclaw_official_group` 保持静态，透传 `auto`。 |
| 流量 | 每个服务组按 class 看请求数、入 / 出 / 总 tokens。 |
| 分类器 | 词表与 V1 规则全局同一套。V2 头：HC 一份、每个 Hub 租户一份。拟合只吃金标。头只改写 P3/P4。训练写 candidate；采用才进 serving（`previous ←` 旧 serving）。热路径只读 serving。须先采用再 `shadow` 旁路（规则仍路由，头只打分），禁止 `off` 直跳 `canary`/`on`。训练页展示旁路效果，一致率不作门禁。后两者还要分发齐。转正看 serving 门禁，采用看 candidate 门禁。准确率只看 P3/P4 人工金标。无独立页不准 `on`。 |
| Hub 训练 | 显式指定一个 trainer（未指定则单进程自任）。语料落**同一份**库，其它 L1 投递进去；换 trainer 只换进程，不换库。trainer 上同时只一条拟合（跨租户串行）。只吃该租户金标，写**该租户 candidate**。禁止跨租户混头。官方门面不进租户语料。 |
| 转正 | 采用才下发 serving。`shadow`/`canary`/`on` 必须已有 serving；后两者还要分发齐。HubCenter / Hub 副本 ACK；单进程本机切正。 |
| 头 UI | **独立页**（模型接入「分类头」）：训练、采用、**旁路观察**、转正、只读该次训练快照、滚动池补标。组上只看分类流量、只应用分类结果。 |

## 2. 目标、范围与非目标

### 2.1 目标

1. 一张卡既能规划 / 设计，也能写文档 / 写代码，按业务换模型。
2. **分对**（全局分类），才能把轻活卸到低成本模型。分到哪一档由该组路由表决定。
3. 不改 L3 WRR / 满载跳过 / sequence failover。
4. 用户只按命中的服务组扣费。官方出算力时，HubCenter 按现有规则扣该租户官方额度。
5. 调度可审计；每组每类流量可看。
6. `/v1/models` 只暴露 `auto`；知道内部名的调用方可钉死。
7. 独立分类头页可训练、采用、看旁路效果、转正、只读该次训练快照、给滚动池补标。无此页不准 `on`。转正前必须先 `shadow` 旁路。

### 2.2 V1

- `kind=dynamic` + `routes[]` + 校验
- `corelib/llmpool/workload.go` 规则分类（只在 L1 主人上跑）
- Hub 租户动态组：L1 + L3，可挂本地和 `maclaw_official`
- Hub provider 补 `model`
- 现有官方组升级为动态 + 三档模板
- 扣费打到命中的动态组 ID，费率用逻辑模型倍率
- chat 与 responses 都走 L1
- usage 增加 `service_group_id` + `workload_class`；Admin 每组每类流量表
- 路由表编辑、规则归因看板 + 试跑（组页）
- 桌面主从边界（带 hint 是 V1.1）

### 2.3 非目标

- 组内加组；新开官方 Auto SKU
- 逃出路由表；每请求 LLM 分类；请求内 SGD
- 改 L3 WRR；`TaskType` 当路由键
- 平台默认往租户组塞官方；`code` 因缺能力升到 `plan`
- 租户样本 / embedding 上报 HubCenter
- 按组各训一头；跨租户混头；HC 与 Hub 租户语料互训
- 自动把旧按组头 / 语料混成全局头
- 用本次拟合样本回算门禁准确率；用自动 P0/P1 或 P0 人工行刷准确率
- 把旁路与规则一致率当成转正门禁
- 训练热替换 serving；未采用就进 `shadow`/`canary`/`on`；头覆盖 P0/P1/P2
- `pipeline=off` 或 P0/P1/P2 时热路径跑 embedding
- 换 trainer 另起一份空语料库
- 分发未齐时再采用 / 回滚 / 升 `canary`/`on`
- 降级还过门禁
- 采用 / 回滚改 `trained_at`；拉官方沿用对侧时间当本侧前向钟
- 热路径读 candidate；preview / TrainRun 原文写进会 HA 复制的制品 JSON
- 只有打到 trainer 的请求才进语料（其它 L1 必须能投递进同一份库）
- 每节点一份本地语料当全集（现码 `llm_usage_records` 就是这种，不能当样板）
- 用 `llm_usage_records` 当滚动语料，或按 1500 裁 usage
- 读不到语料时用空门禁放行；把空报告写成已通过（`PROMOTE` 只绕过检查，不涂绿）
- 同一 trainer 上并行开多租户训练
- `off` 直跳 `canary`/`on`（须先 `shadow`）
- 采用时不把旧 serving 写入 previous
- 组页上训练 / 转正（组内只应用头，不拥有头）

## 3. 术语与三层

| 术语 | 定义 | 不是 |
| --- | --- | --- |
| 服务组 | 授权与扣费单元 | 服务商 / 组内成员 |
| 服务商 | `provider_configs` 提供者 | 扣费单元 |
| 官方三档 | `official-high` / `mid` / `low` | WorkloadClass |
| WorkloadClass | L1 路由键 | TaskType / C0–C3 |
| `balanced` | 分类失败默认档 | 业务类 |
| 全局头 | HC 一份，或每个 Hub 租户一份 | 每组一头 |
| 组应用 | 用全局 class 查本组 `routes[]` | 组内再分类 / 再训头 |
| 分类流量 | 该组记账成功的 class × tokens（含 cache hit） | 训练语料 |
| 滚动语料 | L1 主人成交后投到 trainer 的样本池 | 按组流量表 / 各节点 usage |
| 该次训练快照 | 某 `version` 实际拟合用到的金标样本 | 滚动池的实时窗口 |
| 前向门禁集 | 评估对象重打分；准确率只看 P3/P4 人工金标 | 自动金标、P0 行充准确率、本次 `sample_ids` |
| serving | `shadow`/`canary`/`on` 热路径用的权重 | 刚训完的 candidate |
| candidate | 最近一次训练产物，须「采用」才进 serving | 自动覆盖线上 |
| 采用 | `previous ←` 旧 serving；`serving ← candidate` 并下发，清空 candidate | 改 pipeline |
| serving 门禁 | 升 `canary`/`on` 用 | 采用新 candidate |
| candidate 门禁 | `canary`/`on` 下采用用 | 转正 |
| 自动金标 | P0/P1，只进拟合 | 门禁复核 |
| 人工金标 | 独立页补标 | 快照上改标 |
| 旁路观察 | `shadow`：规则路由，serving 对 P3/P4 打分不改写；训练页展示效果 | 门禁；热路径改写 |

```text
ChatCompletions / Responses
  -> L1 WorkloadClass
    -> L2 logical model
      -> L3 provider WRR
        -> local  or  official-high|mid|low
```

动态组走 `routes[]`。静态组 `auto` 仍走 `SelectBestModelForRequest`。Hub L3 次序用 provider 列表顺序。

## 4. L1 所有权

| 用户走的组 | L1 | 官方服务商 | HubCenter |
| --- | --- | --- | --- |
| 租户动态组 → 本地 | Hub | 不经过 | 无 |
| 租户动态组 → 官方 | Hub | 改写三档名 + **必带 class 头**；`X-MaClaw-Service-Group-ID` 选算力池 | 只 L3，用 class 头记账 |
| Hub 官方门面 | 无 | 透传 `auto` | 官方组 L1 + L3 |
| 直打 HubCenter | 无 | 无 | 官方组 L1 + L3 |

禁止：L1 之后再传 `auto`；组套组；双边 L1；缺能力就把 `code` 改成 `plan`。

## 5. 分类法

P0 hint → P1 workflow/phase → P2 TaskType → P3 启发式 → P4 `routes.balanced`。

V2 头只接在 P3/P4：`canary`/`on` 且规则来源是 `heuristic`/`fallback` 时才可改写。P0/P1/P2 永远规则赢。现码 `ApplyHeadPipeline` 在 `on` 时会盖掉 hint，且 `on` + 空预测会回 `balanced`，须改：**先**看 `ruleSource`，属 `hint`/`workflow`/`task_type` 则原样返回，再走 canary / 空预测回 `balanced`。

P0：`X-MaClaw-Workload-Class` 或 body `maclaw_workload_class`，非法则忽略。header 优先。

P1 PhaseKind：`document_planning`/`code_planning`→`plan`；`artifact_generation`→`doc_write`；`execution`→`code`；`review`→`review`；`ops_execution`→`ops`；`ops_risk_policy`→`review`；`intake`→`classify`。

P1 仅 WorkflowType：申报/立项/`business_plan`/`project_proposal`/`grant_proposal`/`event_planning`/`gaokao_application`/`nsfc_*`/`changjiang_scholar`→`plan`；`product_design`/`innovation`/`due_diligence`/`competitive_analysis`/`experiment_design`→`design`；`paper_writing`/`presentation_design`/`bid_response`/`research_report`/`literature_review`/`patent_application`/`us_patent_application`→`doc_write`；`coding`/`testing`/`paper_reproduction`/`maintenance`→`code`；`bid_review`/`contract_review`/`compliance_audit`/`patent_analysis`/`changjiang_scholar_review`→`review`；`ops_maintenance`→`ops`。

P2：`fast`/C0→`chat`；`intent`/`summary`/C1→`classify`；`reasoning`+tools/编码→`code`；C3 无工作流不得升 `plan`。

P3 只看请求体。接受「写一个商业计划」被判成 `doc_write`。

必填路由：`plan`/`design`/`doc_write`/`code`/`balanced`。缺 `review`/`ops`/`chat`/`classify` 回落 `balanced`。能力升级只在同一质量档换模型，禁止跨 class。

`vision` 是能力约束。`plan`/`design` 禁止 `quality=low`。编码工作流：`requirements`/`design`/`tasks`→`plan`，`implementation`→`code`，`verification`→`review`。

## 6. Schema

缺省 `kind=static`。不设 `default_class`，P4 走 `balanced`。

租户组示例：`plan`/`design`/`review` → `opus-class`（官方 `official-high`）；其余 → `coder-class`（本地优先，官方 `official-mid` 保底）。`quality_floor` 只约束未标明质量的路由和 `balanced`。

校验：五条必填路由；`routes[].model` 非 `auto`；官方 `model` 只能是三档名（保存时枚举校验，不要求在线拉 HC 目录）；禁止 `members[]`。官方组若还没有某档，运行时 404，L3 可落到下一家。三档名若与旧模型重名，由平台管理员改绑，不另起一套词。

`/v1/models` 动态组只返回 `auto`。

### 6.1 官方 Auto

升级现有 HubCenter 官方组（如 `maclaw-official`）：加 `kind=dynamic`、`auto`、三档逻辑名。旧具体模型名保留，带名请求跳过 L1。

| class | 档 | 路由 |
| --- | --- | --- |
| `plan` / `design` / `review` | high | `official-high` |
| `doc_write` / `code` / `ops` / `balanced` | medium | `official-mid` |
| `chat` / `classify` | low | `official-low` |

Hub 内置 `maclaw_official_group` 保持静态门面。

## 7. 请求路径

```text
租户动态组
  Hub 授权并按该组逻辑模型倍率扣费
    L1 -> L3
      本地 -> 用户 endpoint
      官方 -> official-* + Workload-Class 头 -> HC 只 L3
      官方 402/403 -> 同模型下一家服务商

官方门面 / 直打 HC
  auto -> HC L1 + L3
```

插入点：`proxy.go`（官方组且 `auto` 才 L1）；Hub chat + responses（租户动态组且 `auto`）；`maclaw_builtin.go`（租户动态组改写三档并带 class 头）。

`chargedServiceGroupIDs` 固定为该动态组。缓存 key 用 resolved 模型，不用 `auto`。

## 8. 计费与每组每类流量

| 路径 | 用户侧 | 服务商侧 |
| --- | --- | --- |
| 租户 auto → 本地 | 该组 × 逻辑模型倍率 | 无 |
| 租户 auto → 官方 | 同上 | HC 扣该租户官方额度 |
| 官方门面 / 直打 HC | 官方组 | 同上 |

每个服务组 Admin 按 class 看：**请求数、入 tokens、出 tokens、总 tokens**。时间窗 24h / 7d / 30d。行：8 类 + `balanced` + 组合计。只计成功完成的请求（含 cache hit）。静态组一行 `unclassified`。

聚合键：`service_group_id` + `workload_class`（**实际路由 class**）。Hub 记在扣费组。HubCenter：自己 L1 用该类；租户已分类转发则用 class 头（Hub 必带）。`UsageRecord` / 查询补这两列，索引 `(service_group_id, workload_class, created_at)`。

归因字段：`requested_group`、`requested_model`、`workload_class`、`class_source`、`resolved_model`、`resolved_provider`、`upstream_model`、`official_provider_pool`、`selection_reason`。响应头：`X-MaClaw-Workload-Class`（实际路由 class）、`X-MaClaw-Resolved-Model`。

## 9. 规则看板（V1）与分类头（V2）

核心：分类要**分对**，才能把轻活卸到低成本模型。分到哪一档是该组路由表，不是分类器的事。组内只**应用**全局头给出的 class。

```text
成交请求
  -> L1（全局规则，或已转正的全局头）
    -> 该组 routes[] （用户选档）
      -> L3 WRR
  -> 记账成功（含 cache hit；见 9.1）
    -> 该组分类流量（按组看；含 cache hit）
    -> preview + 规则字段入本侧滚动语料（投到 trainer 那一份）
```

### 9.1 组被命中后记什么

只由 **L1 主人**在动态组记账成功钩子里落盘：HC 须 **`RecordUsage` 返回成功**；Hub 与现码同一记账钩子。含 cache hit（可以不扣积分）。Hub 已分类再转发官方时，HC 不再 L1，也不把这条写入 HC 语料；HC 自己的 usage 行照记，只是不进 HC 滚动语料。preview 为空则不进语料（现码已跳过）。语料投递失败不挡响应、不回滚 usage。

记样本**不为落盘单独 embed**。成交后写 preview（截断 400）、rule class / source、当时头预测、`group_id`。L1 若已经为打分 embed 过，把该向量一并写入语料，拟合可少补一遍；未打分则只写 preview。拟合时若缺向量，按已存 preview 补（与现码 `TrainOfficialClassHeadNow` 一致）。禁止回读请求原文。同一请求禁止打分、记样本各 embed 一次。

L1 打分：本节点有可用 serving（或未 ACK 时的 previous）、`pipeline≠off`、且规则来源是 `heuristic`/`fallback` 时，才对 preview embed 一次并写 `HeadClass`。`off`、P0/P1/P2、或本节点无 serving/previous，不 embed（现码 `ClassifyAndRouteWithHead` 在非 `off` 时一律 Predict，须改）。

同一条成交请求：

1. **该组分类流量**（第 8 节）：按**实际路由 class**（头改写后）。含 cache hit。静态组不跑 L1，一行 `unclassified`。
2. **滚动语料**：`id` = `sha1(已截断 preview)` 的**完整 hex**（不带 class / 时间，不截 8 字节）。现码 `hash(preview|class|时间)` 再截 8 字节，每次新 id，须改。人工金标（GoldClass / `gold_source=human` / `labeled_at`）不被成交 upsert 改写；已有自动金标不被后来的 P2/P3 抹掉；embedding 只补缺不覆盖。`last_seen` / `routed_class` / `HeadClass` 可更新为本次。P0/P1 标 `gold_source=auto`；**P2 不当自动金标**；P3 与头自预测不当金标。人工补标 / 改标刷新 `labeled_at`；撤标清空金标并立刻离开门禁。cache hit 计入流量，只更新同一 preview。裁剪只在 trainer 落库时做。

不记：passthrough、未成交、官方门面（`maclaw_official_group`）进租户语料。

HC 现码在分类当下就记样本，须改到记账成功之后（见上）。滚动池有上限（现码约 1500）：先丢无金标，再丢最旧自动金标；**人工金标最后才丢**。只裁语料表，不裁 usage。

**语料与制品拆开。** 现码一份 JSON 既当语料又当制品，HA 会复制 preview，保存时又剥掉 preview。改为：

- **语料**（preview / embedding / 金标）：本侧**一份**库，按 `id=sha1(已截断 preview)` 完整 hex upsert。**不是** `llm_usage_records`，也**不要学 usage**：现码 usage 是各节点本地 SQLite、不进 HA，写成那样会把全集打散。单进程就地写。多副本默认：**成交节点把样本投递到指定 trainer，trainer 落入同一份库**；独立页 / 训练 / 门禁读这同一份。投递失败只丢这条样本。**换 trainer 只换谁拟合 / 谁收投递，不换库路径。** 新节点必须先能打开这份库，否则拒绝更换；有进行中作业时禁止换。换完仍读不到则训练失败、门禁未就绪，禁止对着空表开训或放行。禁止把 preview 并进制品 gossip / HA JSON。Hub 按 tenant 隔离，语料不出 Hub 安装（副本之间投递可以，禁止上传 HC）。
- **制品**（serving / previous / candidate 权重、pipeline、ACK、`TrainRun` 元数据）：可 HA。L1 只读 serving（未 ACK 回退 previous），**不读 candidate**。ACK 只对 serving。

独立页默认打到能读语料的节点（转发 trainer）。转发失败时：快照卡只显示制品里的 gold / group，补标 / 门禁显示未就绪，**禁止**在本地空表上补标。Hub 语料不出网。

### 9.2 V1 规则看板（组页）

挂在**每个动态组**上，与第 8 节流量表在一起。只读归因，不训、无转正开关。

看：每类流量、`class_source` 占比、无 hint 截断样本、规则试跑。用来判断 P3 烂不烂、该不该去独立页转正。组页本身不转正。

列表按钮为「流量」。静态组不出现。

### 9.3 V2 头与训练（全局）

`logits = W e + b`，`W` 为 8x256。`max(softmax)<tau` 则回 `balanced`。`balanced` 不进 `W` 的行。

| 范围 | 几份头 | 语料 | 运行时 |
| --- | --- | --- | --- |
| HubCenter | **一份** | HC 自己做 L1 且记账成功的动态组成交 | HC 当 L1 主人时用 |
| Hub 租户 | **每个租户一份** | 该租户各组成交（官方门面除外） | 该租户动态组当 L1 主人时用 |

禁止：按组各存一头、跨租户混头、HC 与 Hub 租户语料互训。Hub 可只读拉已发布官方头作**冷启动权重**（见 9.6），禁止反向上传样本。样本：HC embedding+标签不进 HA 原文；Hub 不出网。

存储：制品键仍是 HC `llm_official_class_head_v1` / Hub 租户 `llm_class_head_v1`。语料另库（trainer 可读的那份，见 9.1），不进该 JSON。旧的 `llm_class_head_v1:{group}` 不再读写，不做自动混批迁移（切日见 9.6）。

**拟合只吃金标**（自动 + 人工）。无金标留在滚动池供补标，不进这次拟合。无金标则训练失败并写清原因。8 类未齐仍允许训练，但门禁「覆盖」项不会绿。

**训练只写 candidate + `TrainRun`，不得改 serving。** 现码 `TrainOfficialClassHeadNow` 立刻把 `Current` 换成新权重；`canary`/`on` 会吃到未验证头，须改。同一 trainer 进程同时只跑一条拟合（HC 本就一条；Hub **跨租户也串行**，队列按 tenant 隔离语料 / 制品，不并行开训）。进行中再点返回已排队。作业开始时冻结本次 `sample_ids`；训练中新成交仍 upsert 滚动池，不进这一次拟合。再训覆盖未采用的 candidate，不把旧 candidate 推进 previous（previous 只留给 serving）。训练失败则 candidate 与 `TrainRun` 都不动。

**采用 = 换 serving，并留下一档可回滚。** `previous ← 旧 serving`（没有旧 serving 则 previous 仍空）；`serving ← candidate` 并下发；candidate 与其 `TrainRun` 清空（快照跟着权重走：旧 serving 的 TrainRun 进 previous，candidate 的 TrainRun 进 serving）。更旧的 previous 丢掉，只留一档。采用 / 回滚 / 改 pipeline **都不改**各版自带的 `trained_at`。

无 candidate、训练进行中失败。**已有 serving 且分发未齐**也失败（不要在上一次下发未完成时再换头）。尚无 serving 的首次采用不等 ACK。已在 `canary`/`on` 时采用，看 candidate 门禁。细节以表为准。

**热路径只读 serving**（未 ACK 回退 previous）。candidate 权重可以放在制品里供独立页重打分，L1 禁止读。

| 动作 | 条件 | 结果 |
| --- | --- | --- |
| 训练 | 有金标；trainer 上无进行中拟合 | 只写 candidate；`trained_at`=本侧写入时刻 |
| 拉官方 | 未在训练 | 覆盖未采用 candidate，不碰 serving；`trained_at`=本侧写入时刻；`TrainRun.sample_ids` 留空（对侧语料不在本侧，禁止抄官方 id）。训练中则失败 |
| 采用（`off`/`shadow`） | 有 candidate；未在训练；尚无 serving，或已有 serving 且分发已齐 | `previous ←` 旧 serving；`serving ← candidate` 并下发；pipeline 不变；不改 `trained_at` |
| `off→shadow` | **已有 serving**（首次采用后立刻可切，不必等 ACK） | pipeline=`shadow`；打分用 serving |
| 升 `shadow→canary` / `shadow→on` / `canary→on` | **已有 serving**；**分发已齐**；**serving 门禁**全绿或 `PROMOTE` | 只改 pipeline。允许 `shadow` 一步到 `on`，不强制先金丝雀。**禁止 `off→canary` / `off→on`** |
| 采用（`canary`/`on`） | 有 candidate；未在训练；分发已齐；**candidate 门禁**或 `PROMOTE` | 同上换 serving（写入 previous）并下发；不改该版 `trained_at` |
| 降级 `on→canary/shadow/off`、`canary→shadow/off` | 无 | 随时；不过门禁。现码 `on→canary` 还走门禁，须改 |
| 回滚 | 有 previous；分发已齐 | serving 与 previous 对调并下发；pipeline 不变；candidate 不动；各版 `trained_at` 随权重走。否则失败 |

**HubCenter**：指定 trainer 节点训 candidate；采用后下发各 HC 节点。

**Hub**：与 HC 一样**显式指定**一个 trainer 副本（未指定则单进程自任）+ 一份冻结 Gemma。队列按 **tenant** 隔离，但 trainer 上同一时刻只跑一条拟合。一次作业只读该租户滚动语料里的金标、只写该租户 candidate。其余副本只 ACK **serving**，不得各训一份。

训练异步，不挡 chat 热路径。类不均衡用现有损失类别权重，不另开 V4。V3 同安装多副本：trainer + ACK 矩阵 + 未 ACK 副本用上一版 serving。

Pipeline 默认 `off`：`off` 规则、不 embed。P0/P1/P2 即使在 `shadow`/`canary`/`on` 也不 embed。管理 API：`shadow`/`canary`/`on` 必须制品里已有 serving；升 `canary`/`on` 还要集群分发齐。运行时本节点：有 serving 用 serving；未 ACK 且有 previous 用 previous；两者皆无则当 `off`（不 embed）。

**转正前旁路 = `shadow`。** 路由仍走规则，不改 `routed_class`、不改扣费。serving 只对 P3/P4 embed 一次，把 `HeadClass` / `HeadMaxP` 写入语料，供训练页判断「若转正会怎么判」。未开影子则无在线旁路（`off` 不 embed）。`canary` 的 user 用扣费主体（Hub `user_id` / HC auth）；空则当未命中，不随机。`canary` 按 `hash(user) % 100 < 5` 且 `max(p)>=tau`，且仅当规则来源是 `heuristic`/`fallback` 才走头；`on` 同样只改写 P3/P4，低置信回 `balanced`。阈值与金丝雀比例沿用现码，不改。

### 9.4 独立分类头管理界面

**必须有独立页**，挂在模型接入第 4 个子 Tab「分类头」。不是顶栏新业务，不挂组上。无此页不准 `on`。

权限：HubCenter 仅平台超管；Hub 仅该租户管理员。训练、采用、转正、回滚、`PROMOTE` 同权。

一页五块：

1. **训练**：状态、serving / candidate 版本、用本侧金标训练、采用、上次失败、serving 分发 ACK。
2. **旁路观察**：转正前给管理员看「头会怎么判」，**不作门禁**。跟当前选中的 serving / candidate 走。
3. **转正**：规则 -> 采用 -> 影子旁路 -> 金丝雀 / 转正；两行门禁各 x/5；未绿则 `PROMOTE`+原因。无 serving 时 `shadow`/`canary`/`on` 都不可用（先采用）。`off` 不能点到金丝雀 / 转正。
4. **该次训练样本**：只读 `TrainRun` 快照（见 9.5）。不是第 8 节按组流量表，也不可在快照上改金标。
5. **滚动池补标**：默认队列为 `heuristic`/`fallback` 且尚未人工标的样本，按类补齐覆盖。可按 `group_id` 筛选。补标 / 撤标只写滚动池；撤标后该行立刻离开门禁。

旁路卡两行，都只计 P3/P4（头真正会碰的请求），读不到语料则未就绪：

| 行 | 何时有数 | 怎么算 | 看什么 |
| --- | --- | --- | --- |
| 在线旁路 | `shadow`/`canary`/`on` 且选中 **serving** | 语料里 `last_seen >= 该版 trained_at`、已有 `HeadClass` 的成交（热路径写下的） | 真实旁路 |
| 回放旁路 | 选中 serving 或 candidate | 该版 `W` 对池内 P3/P4 preview **重打分**；`id` 不在该版 `sample_ids`（避免训练集自嗨）；缺 embedding 当场补 | 采用后立刻能看；看 candidate 只能走这一行 |

展示：旁路条数；与规则一致率；会改写占比（`HeadClass ≠ rule_class` 且 `max_p >= tau`）；低置信回 `balanced` 占比；规则 class × 头预测交叉（8 类 + `balanced`）；最近若干条截断 preview / rule / head / `max_p` / 会否改写。回放不回写语料 `HeadClass`。未开 `shadow` 时在线行为空，回放仍可用。与规则一致率**不作门禁**。

页头：serving 版本、candidate 版本、状态徽章、**两行门禁**（serving / candidate，没有的灰掉）、旁路摘要（条数 / 会改写占比）、建议句。五卡跟当前选中的那行走。门禁阈值沿用现码：人工复核>=200 且 8 类有覆盖；准确率>=0.85；plan/design 召回>=0.80；连续两窗口。制品就绪分两行：serving = 权重 Ready 且分发齐；candidate = 权重 Ready（不必已下发）。哪几行进哪一卡门禁见 9.6。

状态：未启用 / 训练中 / 影子观察 / 金丝雀 / 已转正 / 门禁未过 / 分发中 / 已回滚。

`off->shadow` 须已有 serving（先采用；不必等首次 ACK）。**禁止 `off` 直跳 `canary`/`on`**。升 `canary`/`on` 只能从 `shadow`/`canary` 出发，且 serving 在、分发齐、serving 门禁绿（或 `PROMOTE`）。降级随时。回滚须有 previous 且分发齐。训练中不可采用、不可拉官方。训练 / 门禁 / 旁路 / 补标 API 在读不到语料的节点上须转到 trainer，或返回未就绪；空表只能报未就绪，不得写成已通过。`PROMOTE` 只绕过门禁检查去改 pipeline，不把空报告涂绿。可撤人工标。

| | HubCenter 全局头 | Hub 租户头 |
| --- | --- | --- |
| 训练 | 指定 trainer；混合 HC L1 成交金标 → candidate | 单一 trainer，跨租户串行 → 该租户 candidate |
| 采用 / 转正 | **下发 serving 到各 HC 节点**，矩阵 ACK 才完成 | **下发 serving 到各 Hub 副本**，ACK 才完成；单进程立刻本机 ACK |
| 未齐 | 「分发中」，未拉到的节点用 previous serving | 「分发中」，未 ACK 副本用 previous serving |
| 回滚 | serving 与 previous 对调并下发；candidate 不动 | 同左 |
| 样本 | 落 trainer 语料库；embedding+标签不进制品 HA 原文 | 落 trainer 库；不出 Hub 安装 |

### 9.5 该次训练快照

滚动语料会继续进新成交，不能拿「此刻缓冲」冒充「这一次训练」。

每次训练成功必须写下 `TrainRun`，并挂到 **candidate**（不改 serving）：

- `version`
- `trained_at`
- `sample_ids[]`（本次拟合实际用到的金标）
- 当时每条的 `gold_class` / `gold_source` / `group_id`（副本，供独立页只读）
- 拟合用的评估摘要（可选；不作转正门禁）

制品里的 `TrainRun` **不含 preview**。preview 只在语料库；独立页经 trainer 按 `sample_ids` 回填。转发失败则只显示制品里的 gold / group。

独立页「该次训练样本」默认展示 **candidate** 快照；可切 serving / previous。采用后：candidate 快照成为 serving 快照，旧 serving 快照成为 previous，candidate 清空。回滚后展示回滚到的 serving 快照。补标只写滚动池，不改已冻结快照。

下发 ACK 只针对 **serving** 权重。制品里可以带 candidate，L1 不读。不推滚动池。副本热路径只读当前 serving 版本号。

### 9.6 切日、门禁与冷启动

**切日（按组一头 -> 全局头）**

- L1 立刻只读全局 store。旧 `llm_class_head_v1:{group}` 不再读。
- **不**把任一旧组头提升为全局 serving 或 candidate。pipeline 强制回 `off`。serving / candidate 皆空。
- 语料不自动导入。管理员可在独立页一键「导入旧组金标」到本侧滚动池（同 HC 或同租户、只金标、不导权重；`id` 重算为 `sha1(已截断 preview)` 完整 hex；**preview 已被剥空的行跳过**——HC 现码 `SaveStripsPreview` 后往往只剩标签，无法拟合也无法前向打分；导入标 `gold_source=human` 仅当原记录是人工复核；`labeled_at`=导入时刻，不当历史回放）。默认不做。先导入再训练时，这些行 `labeled_at < trained_at`，**不算进该版门禁**（须训练后再补标）。
- 已转正的按组头全部失效；未 ACK 的按组下发作废。

**门禁必须前向，且按行分流**

两份报告分开算，页上两行都展示：

- **serving 门禁**：升 `canary`/`on` 用。评估 serving 权重。制品 = serving Ready 且分发齐。
- **candidate 门禁**：`canary`/`on` 下采用用。评估 candidate 权重。制品 = candidate Ready。`off`/`shadow` 下采用不过此门。

一律用该版权重对语料里的 preview **重打分**，不用落盘 `HeadClass`。缺 embedding 则当场补；embedder 不可用则**整份**门禁未就绪，不准把补失败的行当没看见。无 preview 的行跳过，不充正确。读不到语料则两行都未就绪，0 行不得写成已通过。`PROMOTE` 只绕过检查，不涂绿。

行要满足：`gold_source=human`，`labeled_at >= 该版 trained_at`，`id` 不在该版 `sample_ids`。

| 门禁项 | 计哪些行 |
| --- | --- |
| 复核>=200、8 类覆盖 | 上面全部人工行（含人工确认过的 P0/P1/P2，用来凑稀有类） |
| 准确率、plan/design 召回、两窗口 | 仅规则来源 `heuristic`/`fallback` 的人工行（头真正会改写的请求） |

自动金标只进拟合。两窗口 = 近 7 日 + 再往前 7 日，且都落在该版 `trained_at` 之后。

`trained_at` 只在本侧**训练成功**或**拉官方写入 candidate**时打成「此刻」。采用、回滚、升/降 pipeline **都不改**时间戳。换评估对象只是换哪一版的 `W` / `trained_at` / `sample_ids`。采用前在 `off` 打下的人工标，只要 `labeled_at >= 该版 trained_at` 且不在 `sample_ids`，采用后同一钟算进 serving 门禁；采用后影子期打的标同样算（影子打的是 serving，不是 candidate）。

**冷启动**

Hub 可只读拉已发布官方头写入 **candidate**（覆盖未采用 candidate，不碰 serving）。`trained_at`=本侧写入时刻，不沿用官方制品上的时间（否则对侧旧钟会把本地历史人工标算进前向窗口）。`sample_ids` 留空。不算 serving，不算已转正，pipeline 仍为 `off`。要进 `shadow`/`canary`/`on` 必须先采用。禁止把官方样本写入租户池。

### 9.7 施工要点（相对现码）

- L1 只读全局 store，不再 `HeadRuntimeForGroup(groupID)`。HC 用 `OfficialHeadRuntime`；Hub 用 `classHeadRuntime(tenant, "")`。热路径只读 serving（未 ACK 回退 previous）。管理 API：无 serving 拒绝 `shadow`/`canary`/`on`；本节点两者皆无则当 `off`。
- `ApplyHeadPipeline`：**先**看 `ruleSource`，`hint`/`workflow`/`task_type` 原样返回；然后再走 canary / `on` 空预测回 `balanced`。`ClassifyAndRouteWithHead`：`off` 或 P0/P1/P2 不调用 Predict/embed。
- `AllowPipelineChange`：只拦升级；降级（含 `on→canary`）不过门禁。拒绝 `off→canary`/`off→on`。升 `canary`/`on` 另要求 serving 在且分发齐。`PROMOTE` 不把空门禁涂绿。
- 训练写入 `candidate`，禁止再把 `Current` 当 serving 热替换。再训覆盖未采用 candidate。增加「采用」API：`previous ←` 旧 serving，`serving ← candidate`。已有 serving 且分发未齐、无 candidate 或训练中时采用失败。采用不改 `trained_at`。
- 制品与语料拆库：语料不是 `llm_usage_records`，也不按 usage 做成各节点本地表。多副本由 L1 投递到 trainer 落入**同一份库**；换 trainer 不换库，有作业时禁止换。单进程就地写。裁 1500 只裁语料表。preview 不进制品 HA JSON。制品可带 candidate 权重，L1 不读。现码 SaveStripsPreview 不再当「整店剥 preview」。
- 门禁 API 同时返回 serving 与 candidate 两份 `GateReport`。必须读 trainer 那份语料才能算；读不到则转发或未就绪，空窗口不准放行。
- 拉官方只写 candidate，`trained_at` 用本侧此刻，`sample_ids` 留空；训练中拉官方失败。
- HC 记样本从 `applyProxyWorkloadRouting` 挪到 `RecordUsage` 成功之后（含 cache hit）。空 preview 不写。L1 已打分则顺手写入 embedding。
- 滚动池 `id`=`sha1(已截断 preview)` 完整 hex，现码带 class/时间戳且截 8 字节的 id 须改。upsert 不换 id。P2 不当自动金标。补标刷新 `labeled_at`。门禁准确率只累加 P3/P4 人工行。分类流量用 `routed_class`。训练开始时冻结 `sample_ids`；失败不替换 `TrainRun`。
- 训练队列按 tenant 隔离语料，不再 `tenant|group`。trainer 进程同时只一条拟合（Hub 跨租户串行）。
- store 增加 `TrainRun`（至少保留 serving + previous；candidate 采用后可只留快照引用）。
- 组列表「流量/训练」改为「流量」；组对话框删除头 dashboard。
- 模型接入增加「分类头」子 Tab（含采用、serving/candidate、旁路观察、默认 P3 补标队列）。旁路一致率不作门禁。
- `shadow` 对 P3/P4 写 `HeadClass`/`HeadMaxP`；训练页同时给在线旁路与回放旁路。回放不回写语料。
- 测试：同租户多组合入一份 store；官方门面不进租户语料；Hub 转发官方不进 HC 语料；非 trainer 节点成交经投递进入 trainer 语料，本地 usage 表不是语料；空 preview 不进语料；cache hit 写入语料但不裁 usage；人工金标不被后来 P3 upsert 抹掉；改标后 `labeled_at` 更新；导入跳过无 preview 行；先导入再训练则导入行不进该版门禁；训练中新成交不进本次 `sample_ids`；训练失败后 candidate 与 `TrainRun` 不变；Hub 两租户不能同时开训；有作业时拒绝换 trainer；训练后 `on` 仍用旧 serving 且 L1 不读 candidate；无 serving 不能 `shadow`/`canary`；`off` 不能直跳 `canary`/`on`；分发未齐不能 `canary`、不能再次采用；首次采用后可立刻 `off→shadow`；`shadow` 不改 `routed_class` 但 P3 写入 `HeadClass`；`off` 时在线旁路为空、回放仍可用；回放不含该版 `sample_ids` 且不回写 `HeadClass`；旁路一致率绿也不能代替 serving 门禁；`shadow→on` 一步须 serving 门禁；`on→canary` 不过门禁；采用后 previous 是旧 serving，无旧 serving 则回滚失败；P0 在 `on` 下不被头覆盖，也不 embed，空预测也不把 P0 改成 `balanced`；P2 行不是自动金标；准确率窗口不含 `hint` 行；upsert 后 `sample_ids` 仍能对上；采用后 `trained_at` 不变；拉官方后 `trained_at` 是本侧时刻且 `sample_ids` 为空；训练中拉官方失败；HA 制品与 `TrainRun` 都不含 preview；本节点无 serving/previous 时按 `off`；读不到语料时门禁未就绪且 `PROMOTE` 不涂绿；embedder 宕机时整份门禁未就绪。

## 10. 桌面

直连第三方：`model_routes`。走租户动态组：Hub L1，桌面只传 hint。走官方门面：HC L1。禁止桌面换 URL 后再按 `auto` 重选。V1.1 带 hint。

## 11. 节奏

| 阶段 | 内容 | 状态 |
| --- | --- | --- |
| 设计 | 本文（全局头改口） | 已定稿；代码未切 |
| V1 | schema、规则 L1、三档、按组扣费、每组每类流量、组页规则看板 | 已落地 |
| V1.1 | 桌面 / 工作流 hint | 已落地 |
| V2 按组一头 | 每动态组一头、组页转正 | **代码仍在**；口径已废 |
| V2 全局头 | 热路径只读 serving；语料与制品拆开；采用≠转正；两行门禁 | **未落地** |
| V3 | 多副本下发 ACK | 代码按组一头；须随全局头改成按 HC / 按租户下发 |

施工：第 2.2、4、5、6、7、8、9.2（组页流量）、10 节已落地。第 9.1 / 9.3–9.7 未落地。默认 `pipeline=off`。现码训练热替换 `Current`、`on` 覆盖 hint、`on→canary` 还过门禁、无 serving 也可切 pipeline、`off` 可直跳 `canary`/`on`，与本文不符。

## 12. 实施状态

| 项 | 结论 |
| --- | --- |
| 组 / 商 | 组扣费，组内只加商 |
| 官方 Auto | 升级现有组 + 三档 |
| 转发官方 | 三档名 + 必带 class 头，HC 不二次 L1 |
| 流量 | 组 × class：请求 / 入 / 出 / 总 tokens |
| Hub 训练（设计） | 显式指定 trainer，语料同一份库，跨租户串行；热路径不读 candidate |
| 转正（设计） | 先采用再下发（`previous ←` 旧 serving）；须先 `shadow`，禁止 `off` 直跳；`canary`/`on` 还要分发齐 |
| 头 UI（设计） | 模型接入独立「分类头」页：训练、采用、旁路观察、转正、只读快照、滚动池补标。组上只留流量 |
| V1 代码 | 已落地 |
| V1.1 桌面 / 工作流 hint | 已落地 |
| V2 分类头代码 | 仍按组一头；默认 `pipeline=off`；组页转正。**与本文不一致，待切** |
| V3 Hub 多副本 | 代码已落地但是按组 ACK；待改成按租户头 ACK |
| 全局头 + 独立页 | **未落地** |
