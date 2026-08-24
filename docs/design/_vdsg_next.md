# 虚拟动态服务组技术方案

状态：设计已冻结，V1 未开工。  
配套：[`llm-provider-multiplier-lb-zh.md`](llm-provider-multiplier-lb-zh.md)（L3 供应商 WRR，语义不变）、[`../cost-route-phase1.md`](../cost-route-phase1.md)（桌面 C0–C3，不得与本层双调度）。

相对讨论稿的修订：拆清 Hub 租户动态组与 HubCenter 官方动态组是两套 SKU；`maclaw_official` 由租户管理员配置入组，不是平台默认保底；补上 L1 所有权、Hub 上游模型改写、双记账、以及 V2 分类头 / 训练门禁。

## 1. 摘要

服务组已经能在同一逻辑模型下对多个供应商做倍率带 WRR 与 `sequence` 故障转移。缺的是：用户只面对一个服务组（一张卡 / 一个授权 SKU），系统按业务类型把请求落到不同逻辑模型，以压成本。

产品形态接近 Amazon Kiro Auto：对外一个 `auto`，对内按业务分类。与 Kiro 的关键差异：动态组路由表就是允许集合，不得逃出管理员放入的模型名单。

本期冻结决策：

| 决策 | 定义 |
| --- | --- |
| 组形态 | 方案 A：`kind=dynamic`，组内 `routes[]` 把 WorkloadClass 映射到逻辑模型。嵌套成员组推迟。 |
| 词表 | 8 类：`plan` / `design` / `review` / `doc_write` / `code` / `ops` / `chat` / `classify`。`balanced` 只是默认档，不是第 9 类。 |
| 代码档位 | 写代码 = 中档便宜；规划 / 设计走高质量。不按「IDE 写代码用最强模型」回改。 |
| 售卖 | 动态组是独立 SKU，卡密与 grant 绑该组自己的 ID。 |
| 无 hint | 默认 `balanced`，不升到 `plan`。 |
| 分类器 V1 | 只做规则。分类头与在线训练是 V2。 |
| 两套动态组 | Hub 租户动态组与 HubCenter 官方动态组是两份配置、两套 SKU，**不要求同一组 ID**。 |
| 官方进租户组 | `maclaw_official` 是普通供应商，由该 Hub **租户管理员**决定挂到哪个逻辑模型、是否当 L3 保底。平台不默认塞进每个 auto 组。 |
| L1 所有权 | 命中租户动态组 → Hub 做 L1；只走内置官方 SKU → Hub 透传 `auto`，HubCenter 做 L1。禁止双边各跑一次。 |
| 选中官方后 | 必须转发**已解析的上游模型名**，禁止再传 `model=auto`。 |
| Embedding / 头 | 进程内共享一份冻结 Gemma；Hub 按租户（或动态组）各一份 8×256 头；HubCenter 官方一份头，由管理员指定的 trainer 节点训练下发。 |
| 实施 | 先停在设计。显式批准后再改代码。 |

「虚拟」指用户看见一个组、内部模型动态选择，不是组嵌套。

## 2. 目标、范围与非目标

### 2.1 目标

1. 同一张卡既能跑规划 / 设计，也能跑写文档 / 写代码，按业务换模型。
2. 不改 L3 WRR / 满载跳过 / `sequence` failover 语义。
3. 租户动态组与官方动态组各自按自己的 `service_group_id` 授权和扣费；官方被选中时允许**双记账**（Hub 租户组 + HubCenter 官方组），不发明第三层 alias。
4. 每次调度可审计。
5. 用户或管理员可钉死具体逻辑模型，跳过 L1。

### 2.2 V1 范围（批准后才编码）

- `kind=dynamic` + `routes[]` + 组校验
- 共享规则分类器（`corelib/llmpool`，两边调用同一套，但只在自己的 L1 落点跑）
- Hub 租户动态组：L1 + L3（L3 可含本地供应商和 / 或 `maclaw_official`）
- HubCenter 官方动态组：直打 proxy，或经内置官方 SKU 透传 `auto` 后的 L1 + L3
- Hub `ModelServiceProviderConfig` **补齐上游 `model` 改写**（与 `llmpool.ModelProviderConfig.model` 对齐），否则官方无法按业务落到指定官方模型
- 归因字段、Admin 路由表与「把官方挂到某模型」的配置
- 桌面 cost-route 主从边界

### 2.3 非目标

- 嵌套成员组、跨组扣费委托
- Auto 逃出路由表允许集合
- 每请求 LLM 分类器；请求内在线 SGD
- 改 L3 WRR / circuit breaker
- 用桌面 `TaskType` 当服务组路由键
- 平台把官方默认塞进每个租户 auto 组
- Hub 把租户对话原文或 embedding 报到 HubCenter 当训练集
- 未批准就改 registry / proxy / Admin UI

## 3. 术语与三层调度

| 术语 | 定义 | 不能代表什么 |
| --- | --- | --- |
| 服务组 | 权益表面，卡密 / grant / 官方授权的绑定单位 | 不是供应商 |
| 逻辑模型 | 组内名称，可对应多个供应商路由 | 不是上游 model 字符串 |
| 租户动态组 | Hub 上某租户的 `kind=dynamic` 组 | 不是 HubCenter 官方组 |
| 官方动态组 | HubCenter 上的 `kind=dynamic` 组 | 不是 Hub 租户组的镜像 |
| WorkloadClass | L1 路由键 | 不是 `TaskType`，不是 C0–C3 |
| `balanced` | 分类失败时的默认档 | 不是业务类 |
| 分类头 | 冻结 embedding 上的 8×256 线性层 | 不是第二套 Gemma |
| L3 WRR | 同一逻辑模型内的供应商均衡 | 不按业务换模型 |

```text
ChatCompletions
  -> L1 WorkloadClass
    -> L2 logical model (routes[] or pinned name)
      -> L3 provider WRR (unchanged)
        -> local endpoint  or  HubCenter (resolved upstream model)
```

| 层 | 今天 | 本设计 |
| --- | --- | --- |
| L3 | [`balance.go`](../../corelib/llmpool/balance.go) 倍率带 WRR + sequence failover | 不改语义。`maclaw_official` 只是其中一个 provider |
| L2 | 静态组的 `auto` 走 [`SelectBestModelForRequest`](../../hub/internal/llmservice/service.go)（capability / tier / 倍率） | 动态组改走 `routes[]`；静态组保持旧 `auto` |
| L1 | 只在桌面 / 工作流 | 进入服务组，但**只有一个主人**（见第 4 节） |

## 4. L1 所有权与官方供应商

这是讨论稿里最容易写拧的部分。以请求命中的**第一层动态组**为准。

| 用户实际走的组 | 谁跑 L1 | Hub 对官方做什么 | HubCenter 做什么 |
| --- | --- | --- | --- |
| Hub 租户动态组，L3 选中本地供应商 | Hub | 不经过官方 | 无 |
| Hub 租户动态组，L3 选中 `maclaw_official` | Hub | 转发**已解析上游模型** + 官方侧 `X-MaClaw-Service-Group-ID`（官方 provider 上配置的 HC 组，**不是**租户动态组 ID） | 只做该具体模型的 L3，**不再 L1** |
| 内置官方 SKU / 用户没绑租户动态组 | 无（Hub 不分类） | `model=auto` 原样透传 | 官方动态组 L1 + L3 |
| 客户端直打 HubCenter | 无 Hub | 无 | 官方动态组 L1 + L3 |

租户管理员对 `maclaw_official` 的三种合法配法（平台不预置）：

1. **某 class 的模型源**：`plan` → `opus-class`，该模型的 `provider_configs` 只有官方，并写上游模型名。
2. **某模型的 L3 保底**：本地 `sequence` 在前，官方靠后。
3. **不挂官方**：组内只有自定义服务商。

禁止：

- 平台自动给每个 auto 组追加官方保底。
- 租户组已做完 L1 后，仍把 `model=auto` 交给 HubCenter。
- 用租户动态组 ID 去 HubCenter 做授权（HC 只认官方组 ID）。

Hub 今天的 [`ModelServiceProviderConfig`](../../hub/internal/llmservice/registry.go) **没有**上游 `model` 字段，官方路径无法按业务改写。V1 必须补上，与 [`llmpool.ModelProviderConfig.Model`](../../corelib/llmpool/types.go) 对齐。

## 5. 业界对照

| 产品 | 采用 | 不采用 |
| --- | --- | --- |
| Kiro Auto | 一个 SKU、质量下限、可关掉 | 逃出批准名单 |
| Cursor Auto | `model=具体名` 跳过 L1 | — |
| Claude Code / 桌面 TaskType | 只作 P2 hint | 当服务组主键 |
| Continue / Aider 槽位 | — | 覆盖不了开放 API |

## 6. 分类法

路由表只认 WorkloadClass，不认桌面 `TaskType`。

### 6.1 冻结的 8 类

| class | 含义 | 质量 | 典型来源 |
| --- | --- | --- | --- |
| `plan` | 业务规划、项目设计、方案 / 架构取舍 | 高 | `document_planning` / `code_planning` / `business_plan` |
| `design` | 产品设计、创新、标书策略、尽调框架 | 高 | `product_design` / `innovation` / `due_diligence` |
| `review` | 评审、合规、标书评阅 | 中高 | `PhaseKindReview` / `bid_review` |
| `doc_write` | 写文档、报告、PPT、填标书 | 中 | `artifact_generation` / `paper_writing` |
| `code` | 写代码、改代码、补测试 | 中 | `PhaseKindExecution` |
| `ops` | 运维受控执行 | 中 | `ops_execution` |
| `chat` | 闲聊、简单问答 | 低 | `TaskFast` |
| `classify` | 意图、摘要压缩 | 最低 | `TaskIntent` / `TaskSummary` |

约束：V1 不得加类；`vision` 是能力约束不是 class；`plan` / `design` 禁止路由到 `quality=low`；`balanced` 只用于失败回落。

同一编码工作流：`requirements` / `design` / `tasks` → `plan`，`implementation` → `code`，`verification` → `review`。

### 6.2 信号优先级

```text
P0 hint  ->  P1 workflow+phase  ->  P2 TaskType  ->  P3 启发式  ->  P4 balanced
```

| 级 | 信号 | 规则 |
| --- | --- | --- |
| P0 | `X-MaClaw-Workload-Class` 或 body `maclaw_workload_class` | 须为 8 类之一且在该组 `routes[]`；非法则忽略并审计 |
| P1 | `X-MaClaw-Workflow-Type` + `X-MaClaw-Phase-Kind` | 固定表，phase 优先 |
| P2 | `X-MaClaw-Task-Type` | 只映射 `chat` / `classify` / `code`，不能单独判 `plan` |
| P3 | 请求体启发式 | 仅无 hint 兜底，`class_source=heuristic` |
| P4 | 默认 | `balanced` |

P0 header 优先于 body。Body 不得覆盖已校验的 P0。

P1 PhaseKind：`document_planning` / `code_planning` → `plan`；`artifact_generation` → `doc_write`；`execution` → `code`；`review` → `review`；`ops_execution` → `ops`；`ops_risk_policy` → `review`；`intake` → `classify`。

无 phase 时按 WorkflowType：申报 / 立项 / `business_plan` / `project_proposal` → `plan`；`product_design` / `innovation` / `due_diligence` → `design`；`paper_writing` / `presentation_design` / `bid_response` → `doc_write`；`coding` / `testing` / `maintenance` → `code`；`bid_review` / `contract_review` / `compliance_audit` → `review`；`ops_maintenance` → `ops`。

P2：`fast`/C0 → `chat`；`intent`/`summary`/C1 → `classify`；`reasoning` 且工具 / 编码上下文 → `code`；`vision` 只加能力约束；C3 无工作流不得升 `plan`。

P3 只看请求体：规划关键词 → `plan`/`design`；写文档 / PPT → `doc_write`；实现 / 补测试 → `code`；评审 → `review`；短闲聊 → `chat`。V1 接受「写一个商业计划」被判成 `doc_write`，用日志度量。

### 6.3 能力硬约束

L2 之后、L3 之前：缺 `tools` / `vision` / 窗口不够则在组内允许集合上升级一档；再不行明确报错，不静默乱落到无关模型。

## 7. Schema（方案 A）

缺省 `kind` = `static`，行为与今天一致。

Hub 与 HubCenter 动态组字段对齐。Hub 的每条 provider 路由必须能写上游模型（V1 补字段）：

```json
{
  "id": "tenant-auto",
  "kind": "dynamic",
  "access_policy": "grant_required",
  "exposed_models": ["auto"],
  "default_class": "balanced",
  "quality_floor": "medium",
  "routes": [
    {"class": "plan", "model": "opus-class", "quality": "high"},
    {"class": "code", "model": "coder-class", "quality": "medium"},
    {"class": "balanced", "model": "coder-class", "quality": "medium"}
  ],
  "models": [
    {"name": "auto"},
    {
      "name": "opus-class",
      "capability_tags": ["tools", "reasoning", "document"],
      "resolution_tier": 3,
      "credit_multiplier": 2.0,
      "provider_configs": [
        {
          "provider_id": "maclaw_official",
          "model": "claude-opus",
          "official_service_group_id": "hc-official-pro"
        }
      ]
    },
    {
      "name": "coder-class",
      "resolution_tier": 1,
      "credit_multiplier": 1.0,
      "provider_configs": [
        {"provider_id": "local-deepseek", "model": "deepseek-chat", "sequence": 1},
        {
          "provider_id": "maclaw_official",
          "model": "qwen-plus",
          "official_service_group_id": "hc-official-std",
          "sequence": 20
        }
      ]
    }
  ]
}
```

上例只表达管理员**可以**这样配（规划只用官方，写代码本地优先、官方保底），不是平台默认模板。

校验：

1. `kind=dynamic` 必须有 `routes`，且至少包含 `plan` / `design` / `doc_write` / `code` / `balanced`。
2. `routes[].model` 必须在同组 `models[]` 且不是 `auto`。
3. 请求 `auto`/`default` 才走 L1；请求组内其它逻辑名则只走 L3。
4. `plan`/`design` 禁止 `quality=low`。
5. 挂 `maclaw_official` 的路由必须有非空上游 `model`，以及要扣费的官方 `official_service_group_id`（或等价的已有 official 绑定字段）。
6. 旧组无 `kind` 按 `static` 读，保存不必回写。

方案 B（`members[]`）不进 V1。

## 8. 请求路径

```text
命中租户动态组
  Hub 授权(租户组 ID) -> Hub L1 -> Hub L3
    本地供应商 -> 用户 endpoint
    official -> 改写 model + X-MaClaw-Service-Group-ID=官方组
             -> HubCenter 只 L3
  Hub 扣租户组；若打到官方，HC 再扣官方组

只走内置官方 SKU
  Hub 透传 auto + 官方组 ID -> HubCenter L1 + L3 -> 只扣官方组

直打 HubCenter
  HubCenter L1 + L3
```

共享代码放 `corelib/llmpool/workload.go`。插入点：

1. HubCenter [`proxy.go`](../../hubcenter/internal/llmservice/proxy.go)：仅当请求模型是 `auto`/`default` 且命中官方动态组时跑 L1。具体模型名 → 只 L3。
2. Hub [`llm_provider_handlers.go`](../../hub/internal/httpapi/llm_provider_handlers.go)：仅租户 `kind=dynamic` 且 `auto`/`default` 时跑 Hub L1。
3. [`maclaw_builtin.go`](../../hub/internal/llmservice/maclaw_builtin.go)：来自租户动态组时转发已改写的 body.model；来自内置官方 SKU 时保持 `auto`。客户端已带的 hint 头原样透传，供官方 L1 或审计，**不得在 Hub 上生成 class 再让 HC 重判具体模型**。

缓存 key 必须含 resolved 逻辑模型与上游 model，禁止用对外名 `auto`。

## 9. 计费、治理、可观测

| 路径 | Hub 扣谁 | HubCenter 扣谁 |
| --- | --- | --- |
| 租户动态组 → 本地 | 租户动态组 grant | 不扣 |
| 租户动态组 → 官方 | 租户动态组 grant（按该逻辑模型倍率） | 官方组授权（按官方倍率） |
| 内置官方 SKU | 若 Hub 侧有官方绑定则按其现有规则 | 官方组 |

这是两条已有账本，不是新 alias。Admin UI 应让管理员看见「这条业务可能双记账」。

治理：`routes[]` + `models[]` = 允许集合。覆盖：请求具体逻辑名则跳过 L1。

归因字段（扩展 `ModelSelectionDebug`）：`requested_group`、`requested_model`、`workload_class`、`class_source`（`hint` / `workflow` / `task` / `heuristic` / `default` / `override` / `head`）、`resolved_model`、`resolved_provider`、`upstream_model`、`official_service_group_id`、`selection_reason`。

响应头：`X-MaClaw-Workload-Class`、`X-MaClaw-Resolved-Model`，与已有 `X-Provider-ID` / `X-MaClaw-Credit-Multiplier` 并列。

## 10. 分类头与训练（V2，V1 不做）

V1 生产只用规则。V2 用已有 Gemma embedding（256 维，进程内一份）加一个 8 路线性头，缓解无 hint 时 P3 不准，且避免每请求打 LLM。

```text
e = Embed(text)          # 冻结，多租户 / 多 HC 节点共享内存权重
logits = W e + b         # W 为 8x256
若 max(softmax) < τ  -> balanced
否则 argmax -> 8 类之一
```

`balanced` 用阈值实现，不占第 9 行。`τ` 可配，建议先 0.45–0.55。

### 10.1 谁持有哪一份 W

| 位置 | Embedding | 分类头 |
| --- | --- | --- |
| HubCenter 各节点 | 每节点加载同一份 Gemma | **一份官方头** |
| Hub 进程 | 一份 Gemma，所有租户共享 | **每租户一份**（样本极少时可只读加载官方头）；业务差很多再按动态组拆 |

Hub 租户样本不出网。`maclaw_official` 上发生的分类：若请求属于租户动态组，标签记在 **Hub 该租户**（Hub 做的 L1）；若属于内置官方 SKU，标签记在 **HubCenter**。

### 10.2 训练机制（不是请求内在线学习）

不要在热路径上 `W ← W − η∇L`。要的是：采集 → 影子打分 → 异步重训 → 复核门禁 → 版本发布。

标签：

| 来源 | 能否训练 |
| --- | --- |
| P0 / P1 | 能，主集 |
| 人工复核 | 能，门禁集 |
| 规则与头一致且高置信 | 能，银标 |
| P3 启发式、模型自预测 | 不能当主监督 |

HubCenter：管理员指定 **trainer 节点**（不要用 HA 选主）。各节点上报紧凑样本（优先 256 维向量 + 标签，少传原文）。trainer 验证通过后发布 `official-head@vN`，其它节点只加载。trainer 宕机则继续用上一版。

Hub：该进程自己训本租户头，制品不出网。可选用官方已发布头初始化。

门禁建议：复核集约 200 条且 8 类有覆盖；总体准确率 ≥ 0.85；`plan`/`design` 召回 ≥ 0.80；连续两个窗口过线再切正。低置信与超时（如 20ms）仍回 `balanced`。

## 11. 与桌面 cost-route 的边界

| 场景 | 谁做主 |
| --- | --- |
| 桌面直连第三方 | 继续 `model_routes` / C0–C3 |
| 桌面走租户动态组 | Hub L1；桌面只传 hint，不换 URL/Key |
| 桌面只走内置官方 SKU | HubCenter L1；Hub 透传 `auto` |
| 同时开 | 桌面只分类传 hint；选模只发生在第 4 节的 L1 主人上 |

## 12. 落地节奏

| 阶段 | 内容 | 状态 |
| --- | --- | --- |
| 0 | 本文 | 已完成 |
| V1 | 动态组 schema、规则 L1、Hub 上游 `model` 改写、官方按第 4 节转发、归因、Admin 路由表 | 未开工 |
| V1.1 | 桌面 / 工作流带 hint | 依赖 V1 |
| V2 | 影子头、trainer、租户头、门禁切正 | 可选 |
| 不做 | 嵌套组、逃出名单、请求内 LLM / SGD、平台默认塞官方保底 | — |

V1 改动面：`corelib/llmpool` 类型与 `workload.go`；Hub `registry.go` 补 `model`；`proxy.go`；`llm_provider_handlers.go`；`maclaw_builtin.go` 按来源决定是否改写 model；两边 Admin UI；usage 字段。

## 13. 开工评审

值得做：同一张卡既跑规划又跑写文档 / 写代码；管理员还想把官方指定给贵业务或当保底。

可继续不做：流量已被桌面 `model_routes` 分流，也没有「一张卡打天下」的售卖。

V1 技术风险仍是无 hint 的 P3 误判；用日志度量，不把分类头当第一依赖。

## 14. 实施状态

| 项 | 结论 |
| --- | --- |
| 方案 A | 冻结；B 不进 V1 |
| 8 类 + `balanced` | 冻结 |
| 写代码中档 | 产品原则 |
| 两套 SKU | Hub 租户组 ≠ HC 官方组 |
| 官方入组 | 管理员配置；选中后转发已解析模型 |
| 分类头 | V2；共享 embedding，头按 HC 官方 / Hub 租户 |
| 训练 | 异步 + 门禁；HC 指定 trainer；Hub 本地训 |
| V1 代码 | 不开工，待显式批准 |

批准开工时以第 2.2、第 4、第 7、第 8、第 12 节为施工范围。
