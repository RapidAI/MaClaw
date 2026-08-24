# LLM 服务商模型 Token Credits 定价与扣费设计

> 状态：核心计费链路、响应丢失后的官方服务商对账与后台补偿已实现；持久化 outbox、每日跨层对账与历史数据迁移仍按第 11 节后续推进  
> 范围：HubCenter 算力商店、Hub 模型接入服务商、Hub 用户额度与用量统计  
> 日期：2026-08-23  
> 关联设计：[同倍率服务商 Load Balance](llm-provider-multiplier-lb-zh.md)

## 1. 决策摘要

LLM 的价格配置放在**模型接入的服务商路由**上，即“服务商 + 上游模型”的组合；在现有模型中应落在 `ModelProviderConfig`，或由独立的 `ProviderModelPricing` 表以该组合为唯一键承载。它不放在服务组、充值卡或用户上。

- 计费唯一货币是 **Credits**；每条路由分别设置输入、输出的 `Credits / 10,000 Token`。
- 人民币是相同路由上的**参考展示价格**，不参与授权、扣款、余额判断和限额判断。
- 服务组决定用户是否有权使用、从哪个服务组对应的额度扣款，以及该服务组对价格的倍率；它不保存输入/输出基础单价。
- 一次请求先按服务商所在时区和请求开始时间，从实际命中的服务商路由解析该时段的输入/输出价格；再乘以实际扣款服务组的倍率。
- 每次成功调用必须落一条不可变的计费明细；余额、用户统计、服务商统计均从该明细汇总，避免“统计一套、扣款另一套”。
- Credits 在服务端以整数最小单位计算和存储；JSON 中的十进制价格仅是配置输入形式，禁止用二进制浮点数直接记账。

本设计取代“`1 Credit = 10,000 Total Tokens`”的单一规则。兼容期仍允许将输入、输出价格都初始化为 `1 Credit / 万 Token`。

### 1.1 当前实现状态（2026-08-23）

已落地的核心路径如下：

- 模型接入服务商路由支持输入、输出分别按每万 Token 配置 Credits 价格和仅展示的人民币参考价，并支持按服务商时区解析分时价格。
- Hub 在请求开始时冻结本地服务商价格；官方服务商先向 HubCenter 取得短期 Quote，锁定实际服务商路由和基础价格，再以 Hub 服务组倍率预留用户 Credits。
- 请求结束时按实际输入、输出用量和冻结价格结算；`billing_group_multiplier` 仅由 Hub 应用一次。`maclaw_official` 加入 Hub 服务组后同样适用该规则。
- 请求级预留会占用可用余额；成功结算在同一 Registry 保存中释放预留并写入幂等账本，未发送/失败路径释放预留。账本以请求 ID 去重，SQLite 保存审计镜像。
- HubCenter Quote/Proxy 对官方路径锁定路由；流式请求从 Trailer、非流式请求从 Header 获取最终价格与 usage 快照。
- Hub 对已发送但未收到响应/Trailer 的官方请求，先在该用户下次请求时尝试对账；同时按租户启动后立即执行、随后每分钟执行后台对账。HubCenter 只返回按 Hub、租户和请求 ID 限域的最终 usage/价格快照；未知、未完成或不兼容的结果保留预留，不会因 Quote TTL 自动释放。

当前仍在演进的边界：旧 HubCenter 不支持 Quote 时会为滚动发布走兼容转发路径；billing-attempt 当前是 HubCenter 进程内短期保留（30 分钟），因此超过其保留窗口的未知请求仍需持久化 outbox、长周期异常队列和每日跨层对账处理。

## 2. 边界与术语

| 术语 | 定义 |
|---|---|
| 服务商 | 一个可转发请求的上游接入，例如 MaClaw 官方、OpenAI、DeepSeek 或租户配置的外部 API。 |
| 服务商路由 | 在一个定价作用域内由不可变 `provider_route_id` 标识；其业务唯一键为 `scope_id + provider_id + upstream_model`。配置应属于模型接入服务商（`ModelProviderConfig`）或等价独立价格表。当同一服务商接多个模型时，每个模型可有独立价格。 |
| 逻辑模型 | Hub / HubCenter 对用户暴露的模型名；可路由到一个或多个服务商路由。 |
| 服务组 | 权限、额度归属和价格倍率单位，决定“谁能用、从哪组 Credits 扣、按几倍价格扣”。 |
| Credits | 唯一可扣减的内部额度。 |
| 计费模式 | 路由的显式 `billing_mode`：`paid` 表示按本设计扣 Credits；`free` 表示免费且不创建用户预授权。不能仅以价格为 0 推断免费。 |
| 参考人民币 | 运营或用户查看成本时的展示值，不是结算依据。 |
| 原始用量 | 上游返回的输入、输出及缓存等 Token；不乘倍率。 |
| 计费快照 | 请求开始确定、随账单永久保存的路由、单价、倍率和规则版本。 |
| 计费报价（quote） | 请求执行前冻结的基础价格、路由和最大可结算额度，用于预授权；官方服务商由 HubCenter 签发。 |
| 计费尝试（attempt） | 同一用户 `request_id` 的一次上游发送。一个请求可有多个尝试，但最终用户账单最多一笔；每次发送使用独立 `attempt_id`。 |
| 计费策略版本 | 解析服务组、路由、价格、倍率、免费模式和规则开关的不可变版本；`billing_policy_version` 必须随报价和账本快照保存。 |

**不在本期范围内：**图像、音频、请求次数、工具调用、联网检索等非 Token 计费。它们未来可作为独立 `billing_unit` 扩展，不能伪装为 Token。

## 3. 定价配置

### 3.1 路由价格结构

在每个模型接入服务商路由上配置默认价格和可选分时价格表。HubCenter 管理官方服务商；Hub 管理租户外部服务商；二者使用同一模型和校验规则。分时价格归属服务商路由，服务组不维护时段价格。

```json
{
  "provider_id": "maclaw-official",
  "upstream_model": "opencode-1",
  "billing_mode": "paid",
  "token_pricing": {
    "input_credits_per_10k": 1.0,
    "output_credits_per_10k": 4.0,
    "input_rmb_per_10k": 0.02,
    "output_rmb_per_10k": 0.08,
    "minimum_request_credits": 0.1,
    "timezone": "Asia/Shanghai",
    "price_schedule": [
      {
        "id": "weekday-night",
        "days": [1, 2, 3, 4, 5],
        "start": "00:00",
        "end": "08:00",
        "input_credits_per_10k": 0.5,
        "output_credits_per_10k": 2.0,
        "input_rmb_per_10k": 0.01,
        "output_rmb_per_10k": 0.04
      }
    ],
    "version": "2026-08-23-v1"
  }
}
```

| 字段 | 含义 | 是否用于扣款 |
|---|---|---|
| `input_credits_per_10k` | 输入 Token 每一万的 Credits 价格 | 是 |
| `output_credits_per_10k` | 输出 Token 每一万的 Credits 价格 | 是 |
| `input_rmb_per_10k` | 输入 Token 每一万的参考人民币 | 否，仅展示 |
| `output_rmb_per_10k` | 输出 Token 每一万的参考人民币 | 否，仅展示 |
| `minimum_request_credits` | 成功请求的基础最低消费（未乘服务组倍率） | 是 |
| `timezone` | 分时价格表适用的 IANA 时区，默认 `Asia/Shanghai` | 是，决定命中窗口 |
| `price_schedule` | 按星期和本地时段覆盖默认输入/输出价格的列表 | 是 |
| `version` | 价格规则版本 | 用于审计 |
| `billing_mode` | `paid` 或 `free` | `paid` 才允许创建用户收费账单；`free` 不扣用户 Credits |

### 3.2 配置原则

1. `billing_mode` 必须显式配置。`paid` 路由的 Credits 单价必须是非负有限数，且输入、输出和最低消费不能同时为零；`free` 路由不创建用户收费账单。零价不得隐式表示免费。
2. 人民币字段可为空或为零，不能作为 Credits 的回退来源，也不能反向换算 Credits。
3. 同一 `scope_id + provider_id + upstream_model` 在同一时刻只能有一个启用的价格版本。价格版本为不可变修订，必须有单调递增的 `version` 和明确的 `effective_at`；修改通过创建新版本完成，不能原地覆盖。关联该路由的服务组、倍率、免费/收费模式及规则开关也必须形成可引用的 `billing_policy_version`。
4. 更新价格只影响 `effective_at` 之后开始的请求；历史账单保存快照，绝不能重算。
5. UI 以“输入 Credits / 万”“输出 Credits / 万”为主字段；参考人民币明确标注“仅供参考，不参与扣费”。
6. 分时表以服务商 `timezone` 的请求开始时间匹配；`days` 使用 `0=周日 … 6=周六`，`end` 为排他边界，跨夜窗口按开始日延续规则处理。每个窗口必须有稳定 `id`。账单同时保存请求开始 UTC 时间、时区和解析后的 UTC offset，以消除夏令时回拨时的歧义；春季跳过的本地时间不能配置为窗口边界。
7. 同一星期的分时窗口不得重叠；保存时直接拒绝重叠配置，未命中窗口时才回退到路由默认价格。若将来确需覆盖语义，必须引入显式 `priority` 和配置审计；第一版不得依赖列表顺序。

### 3.3 服务商分时价格

服务商可能在不同时间段提供不同的输入、输出成本，因此价格不是一个全天固定值。每个 `price_schedule` 窗口可同时覆盖输入/输出 Credits 和人民币参考价格；未填写的字段沿用路由默认值。

```text
请求开始时间 → 转换为服务商时区 → 选中 price_schedule 窗口
             → 得到该请求的输入/输出基础价格
             → 乘实际扣款服务组倍率 → 最终扣款价格
```

请求跨越时段边界时，整笔请求固定使用**开始时命中的服务商分时价格**，不拆分 Token 重算。该规则与服务商分时成本的选路规则保持一致，也避免流式请求跨时段产生不可复核的账单。

### 3.4 计费报价：预授权前先冻结官方价格

Hub 不能在不知道官方路由基础价格的情况下可靠预授权。因此，官方请求必须先取得 HubCenter 的**计费报价**，而不是等上游调用完成后才发现价格：

```text
Hub 生成 request_id、解析逻辑模型和实际扣款服务组
  → HubCenter Quote：选定路由、冻结分时基础价格、计算可结算上限
  → Hub 以报价 + 服务组倍率预授权用户 Credits
  → HubCenter Proxy：携带 quote_id / quote_token 执行已报价的路由
  → 完成后以 usage + 同一报价快照最终结算
```

- `quote` 绑定 `request_id`、Hub/租户、已认证用户主体、逻辑模型、实际 `provider_route_id`、价格版本、`billing_policy_version`、服务组 ID 与倍率快照、请求开始时间、输入 Token 上限、输出 `max_tokens` 上限和过期时间；不得被另一用户、模型、服务组或请求复用。报价的输入上限必须由实际路由使用的 Tokenizer 计算，或由 HubCenter 以该路由的受控最大输入上限给出；不能用字符数或 Hub 的不同 Tokenizer 直接替代。
- HubCenter 为报价返回短期有效、可验证且防篡改的 `quote_token`；Hub 将其只作为内部认证字段回传。签名、密钥标识和过期时间由 HubCenter 验证，Hub 不自行伪造或修改报价。它是 bearer 凭证：不得写入访问日志、账本、埋点、错误消息或返回给终端用户；账本仅保存 `quote_id` 与凭证指纹/密钥版本。
- Proxy 必须执行已报价的路由和价格版本。若该路由不可用，不得静默切换到不同价格路由：应返回可重报价的**未发送**错误；Hub 释放旧预授权后，可在同一 `request_id` 下创建新 quote 和新的 `attempt_id` 再预授权。若已无法确认旧尝试是否发送至上游，则不得自动重试或故障切换，必须先按 `attempt_id` 对账。
- 报价过期、上下文不匹配或价格版本失效时，Proxy 必须拒绝执行；Hub 释放原预授权后重新报价。报价仅冻结价格，不保证上游实际可用性。服务组授权、倍率或成员关系在报价后发生变化时，不得改变已预授权请求：已签发 quote 依旧按其 `billing_policy_version` 结算；新请求一律按新策略重新授权与报价。
- 外部服务商无需远端 Quote：Hub 在本地选定路由时，以同一 `PricingQuote` 数据结构冻结基础价格，再执行本地预授权。这样两条路径共享结算模型。
- 所有收费上游发送均携带 `request_id` 与 `attempt_id`；HubCenter 必须在自身边界按二者去重。只有在确认请求尚未离开 HubCenter 时才能重试同一尝试；状态未知、已发送、已开始流式输出以及对冲（hedge）发送默认禁止自动重试，避免同一用户请求被上游重复消费。

## 4. 计费规则

### 4.1 基础公式

设：

- `I` = 上游实际输入 Token；
- `O` = 上游实际输出 Token；
- `Pi` = 服务商路由在请求开始时命中的输入 Credits / 10,000 Token；
- `Po` = 服务商路由在请求开始时命中的输出 Credits / 10,000 Token；
- `Mg` = 最终实际扣款服务组的倍率；未配置时为 `1`。

则：

```text
input_credits  = I / 10,000 × Pi × Mg
output_credits = O / 10,000 × Po × Mg
requested_credits = input_credits + output_credits
final_minimum_credits = provider_minimum_request_credits × Mg
deducted_credits = max(requested_credits, final_minimum_credits)
```

最终精度、舍入模式与存储单位见第 4.3 节；不得先分别按 Token 类型或每个流片段舍入，以免累积偏差。

### 4.2 示例

服务商路由设置：输入 `1 Credit / 万`、输出 `4 Credits / 万`；实际扣款服务组的倍率为 `×2`。请求输入 20,000、输出 5,000 Token：

```text
输入 = 20,000 / 10,000 × 1 × 2 = 4 Credits
输出 =  5,000 / 10,000 × 4 × 2 = 4 Credits
实际扣除 = 8 Credits
```

同一请求展示参考人民币时，只按服务商路由的 RMB 单价计算，且**不乘服务组倍率**：

```text
reference_rmb = I / 10,000 × provider_input_rmb_per_10k
              + O / 10,000 × provider_output_rmb_per_10k
```

它是上游参考成本/标价，不是 Credits 的换算值，也不能影响上式。若要展示用户侧倍率后的 Credits 价格，应明确命名为“最终 Credits 价格”，不得称为人民币成本。

### 4.3 精度、舍入与 Token 分类

Credits 账本采用 `microcredit` 整数作为最小单位，建议 `1 Credit = 1,000,000 microcredits`。价格保存时将十进制 `Credits / 10,000 Token` 无损转换为该单位；计算使用整数或定点十进制：

```text
raw_microcredits = tokens × price_microcredits_per_10k × billing_group_multiplier / 10,000
```

每笔请求仅在 `max(raw_total, final_minimum)` 后，按 **half-up** 规则舍入至 `1,000 microcredits`（即 0.001 Credit）。同一个 `request_id` 的重放必须得到完全相同的结果；禁止使用 `float64`、逐流片舍入或数据库的隐式精度转换。

- `input_tokens` 是上游 usage 的 prompt/input token；`output_tokens` 是 completion/output token。供应商将 reasoning token 单列时，本期必须明确归入 output 并保留 `reasoning_tokens` 原值，不能遗漏或重复扣费。
- `cached_input_tokens`、`cache_write_tokens` 是输入的统计子集，本期不另行计价，不能再并入 `input_tokens` 二次扣费。
- 未识别的新 usage 类别先持久化原始字段但不自动计费；只有新增明确单价、快照字段和验收用例后才可成为新计费项目。

### 4.4 倍率规则

倍率由**最终实际扣款的服务组**决定，不由服务商、模型或路由决定。服务商负责在请求开始时按分时表提供输入/输出基础价格；倍率是对该时段基础价格的修正：

```text
effective_input_price  = input_credits_per_10k × M
effective_output_price = output_credits_per_10k × M
```

- 服务组倍率字段命名为 `billing_group_multiplier`，默认 `1`；本设计中服务组倍率不分时，服务商路由价格才分时。
- 一次请求只能使用一个最终扣款服务组；多个候选组仅用于路由/权限，确定扣款组后才读取该组倍率。
- 本地 Hub 外部服务商：Hub 读取本地服务商路由价格和本地实际扣款服务组倍率。
- MaClaw 官方服务商：HubCenter 返回命中服务商路由的基础价格快照；Hub 只应用本地实际扣款服务组倍率，不能接收或重复应用官方服务商倍率。
- 配置保存时直接拒绝非正数、NaN、Inf 倍率；运行时若已冻结的收费快照出现无效倍率或无效基础价格，必须停止发送/结算并进入 `pricing_unresolved`，不得静默按 `1` 继续收费。仅显式迁移生成的 `legacy-v1` 快照可使用倍率 `1`。

### 4.5 调度成本与用户计费必须分离

当前服务商/模型上的 `CreditMultiplier` 还可能用于同倍率服务商负载均衡、成本档位或选路。它与最终用户扣费不是同一概念，必须拆分语义：

| 字段 | 配置位置 | 用途 | 是否进入用户账单 |
|---|---|---|---|
| `dispatch_cost_multiplier` | 服务商/模型路由 | 调度、负载均衡、成本排序；兼容期承接旧 `CreditMultiplier` | 否 |
| `billing_group_multiplier` | Hub 实际扣款服务组 | 最终用户 Credits 定价倍率 | 是 |

新账单公式只能读取 `billing_group_multiplier`。在调度改为直接比较服务商分时基础价格前，旧 `CreditMultiplier` 可双读为 `dispatch_cost_multiplier`，但绝不能再隐式进入用户账单公式。

## 5. 缓存和异常请求

### 5.1 本地响应缓存

Hub 或 HubCenter 的本地完整响应缓存命中时未请求上游：

- 不扣 Credits；
- 记录 `local_cache_hit=true`、原始 Token 为 `0` 或明确标记为“未调用上游”；
- 可展示命中次数，但不能把它与上游 Prompt Cache 混合。

### 5.2 上游 Prompt Cache

本期的输入单价已覆盖上游报告的总输入 Token；`cached_input_tokens` 是输入 Token 的子集，仅作统计，不单独加减。`cache_write_tokens` 同理，仅作统计。

未来需要差异化缓存价格时，新增 `cache_read_credits_per_10k` / `cache_write_credits_per_10k` 及对应 RMB 字段，并以新 `pricing.version` 启用；不得修改旧账单的含义。

### 5.3 成功、失败和流式中断

| 情况 | 是否扣 Credits | 规则 |
|---|---|---|
| 上游成功 | 是 | 依据最终 usage 与计费快照。 |
| 流式已输出业务内容后中断 | 是 | 有可信 usage 时按实际结算；usage 缺失时保留预授权并标记 `usage_unresolved`，由对账任务补齐。只有明确启用的兜底策略才可按最低消费结算，并标注 `usage_source=fallback`。 |
| 上游失败且未输出业务内容 | 否 | 只记失败访问日志。 |
| 本地缓存命中 | 否 | 见 5.1。 |

## 6. 账本、扣款和统计

### 6.1 不可变计费明细

新增 `llm_billing_ledger`（名称可按现有存储规范调整）。每个可收费请求一条记录，至少包含：

```text
request_id, tenant_id, user_id, email
service_group_id, logical_model, provider_id, upstream_model
provider_route_id, pricing_quote_id, upstream_attempt_id
billing_policy_version, billing_mode_snapshot
input_tokens, output_tokens, cached_input_tokens, cache_write_tokens
provider_pricing_timezone, provider_pricing_window_snapshot
input_credits_per_10k_snapshot, output_credits_per_10k_snapshot
input_rmb_per_10k_snapshot, output_rmb_per_10k_snapshot
service_group_multiplier_snapshot
provider_minimum_credits_snapshot, final_minimum_credits_snapshot
input_credits, output_credits, minimum_adjustment_credits, deducted_microcredits
reference_rmb, pricing_version, usage_source, local_cache_hit
state, reserved_microcredits, settled_at, created_at
```

`deducted_microcredits` 是用户报表和余额扣减的唯一依据；界面将其按固定比例展示为 Credits。每条路由即使关联多个候选服务组，也只记录最终实际扣款的服务组，避免一笔消费在多组中重复统计。

账本应采用“请求主记录 + 尝试明细”的关系：`llm_billing_ledger` 以 `(tenant_id, request_id)` 唯一，保存最终用户结算；`llm_billing_attempts` 以 `(tenant_id, request_id, attempt_id)` 唯一，保存每次上游发送、路由、usage、HubCenter 供给授权结果和状态。尝试记录不是自动加总的用户账单；只有被标记为 `settlement_attempt` 的一次成功尝试可驱动该请求的最终扣费。

### 6.2 预授权与最终结算

仅在请求完成后扣款会造成“上游已经产生费用、Hub 用户余额却不足”的不可逆窗口。计费型服务组采用预授权 + 最终结算；这是官方与外部服务商共用的状态机：

```text
请求开始
  → Hub 确定实际扣款服务组与 request_id
  → 按已冻结报价、实际输入和最大输出预算预留 Credits（至少预留最终最低消费）
  → 调用上游 / HubCenter
  → 收到 usage 与基础价格快照
  → 按最终价格原子结算，释放未使用预留或补扣差额
  → 账本状态 finalized
```

- `request_id` 由 Hub 在转发前生成并贯穿全链路；预授权、HubCenter 供给授权和 Hub 用户账本均以它作为幂等关联键。一个 `request_id` 只能对应一个已冻结的收费服务组、价格报价和终端账单；服务组或倍率变更不追溯影响已创建的预授权。
- 预留的上限使用实际/可验证的输入 Token 与请求声明的 `max_tokens`，按已冻结报价的基础价格和服务组倍率计算；未指定上限时服务端必须采用该模型的受控最大输出预算，不能按无限额度放行。
- 因分时价格已在请求开始冻结，HubCenter 回传的快照可校验且可用于最终结算；不因请求完成时跨过时段而变价。
- 最终费用高于预留时，先在同一用户 + 服务组余额域内原子补扣；若仍不足，账本必须进入显式 `debt` / `overdraft` 状态，并触发后续限制或追缴策略，不能静默少扣。
- 用户主动取消、上游失败且没有业务输出时，释放全部预留；有业务输出或获得可用 usage 时按第 5.3 节结算。Hub 进程重启或网络断连留下的 `reserved` 状态必须由对账任务按 `request_id` 查询上游尝试结果后结算或释放；不得因 TTL 到期直接当作未消费。

### 6.3 原子账本与余额变更

```text
最终 usage 与价格快照可用
  → 生成/补全不可变计费明细
  → 在同一事务/串行临界区内最终结算对应用户-服务组 Credits
  → 标记账本为 finalized（或 debt / failed）
  → 异步汇总用户、服务商和运营报表
```

- 账本写入与余额变更必须具备同一 `request_id` 的幂等保护。
- 并发扣费以用户 + 服务组为粒度原子执行，不能出现并发超用。
- 余额可用量应扣除未结算预留；不具备足够预授权额度的请求在调用上游前阻断。
- 每次状态变迁都必须带乐观锁版本或条件更新（例如 `reserved → executing → finalized`）；完成回调、Trailer、对账任务和用户取消可并发到达，只有一个执行者能完成结算。状态机必须拒绝从 `finalized` 回退或再次扣款。
- 预留不是收入：运营收入、用户报表和排行榜只能汇总 `finalized` 的 `deducted_microcredits`；`reserved`、`pricing_unresolved`、`usage_unresolved` 和 `debt` 必须分别展示，不能混入“已消费”。
- 取消的边界以 HubCenter 尝试状态为准：Hub 在尚未发送前取消则释放预留；请求已发送时只转发取消信号，不能立即退款；最终按实际 usage 或可信的“未产生计费 usage”证明结算。客户端断开连接不等于上游未消费。

### 6.4 统计展示

所有界面把“使用量”和“扣费”分开：

| 页面 | 必须展示 |
|---|---|
| 用户用量统计 | 输入、输出、缓存读写、原始总 Token、实际扣除 Credits、余额。 |
| 用户排行榜 | 默认按实际扣除 Credits 排名；可切换为原始总 Token，标题必须指明维度。 |
| 服务商统计 | 输入/输出 Token、各自对应 Credits 收入、命中的服务商价格时段、实际扣款服务组倍率、参考人民币。 |
| 单请求访问日志 | 输入、输出、命中的服务商价格时段、基础价格、服务组倍率、最终扣除 Credits、`pricing_version`。 |

不得使用 `total_tokens / 10,000` 作为“积分”展示；该值在输入和输出价格不同后没有财务意义。

### 6.5 事件、审计与对账

记账事务提交后，以 **outbox** 写入 `BillingFinalized`、`BillingDebtRaised`、`ReservationReleased` 等事件；异步报表、通知、HubCenter 对账只能消费 outbox，不得通过读取 HTTP 响应或重复调用计费函数生成统计。消费者按 `(tenant_id, request_id, event_type)` 幂等。

- 价格配置的创建、启用、停用、分时窗口变化、服务组倍率变化、免费/收费切换均需记录操作者、前后值、原因、审批/工单号和生效时间。任一已产生用户消费的版本不可删除。
- 每日对账以 `request_id + attempt_id` 比对 Hub 用户最终账本、HubCenter 供给授权尝试和上游 usage；差异进入显式 `reconciliation_exception` 队列，不通过直接改余额消除。对账拉取使用 HubCenter 内部认证接口，并按 Hub ID、租户、请求和尝试四元组限域，不能以任意 `attempt_id` 跨租户查询。
- 补偿必须追加反向账本分录（例如 `adjustment_refund`、`adjustment_debt`），引用原账本和原因；禁止 UPDATE 已结算的 `deducted_microcredits`，保证审计可追溯。

### 6.6 保留、隐私与访问控制

计费审计所需数据与模型请求内容必须分离。账本和尝试明细只保存路由、价格快照、Token 数、请求/尝试 ID、服务组、结算状态和不可逆的主体引用；**不得**保存 API Key、`quote_token`、Prompt、Completion、工具参数或原始上游响应。需要关联访问日志时，以受权限保护的 `request_id` 关联。

- 用户、服务组、服务商和财务角色只可读取其权限范围内的汇总与明细；跨租户的 `request_id` 不可被枚举或作为查询唯一条件。
- 审计与账本使用各自明确的保留期；到期删除可识别主体映射或访问日志时，账本保留不可逆主体引用、金额和配置快照，以维持财务可核查性。
- 配置、报价、快照和账本接口都必须做租户边界校验；不能仅信任客户端传来的 `tenant_id`、`service_group_id` 或 Header。

## 7. 从服务商源头到 Hub 用户扣费的完整链路

### 7.1 两层账本，禁止混账或重复扣用户

官方服务商经过 HubCenter 转发时，存在两个不同主体的账务层，必须明确分离：

| 账务层 | 扣款对象 | 目的 | 是否等同于 Hub 用户 Credits |
|---|---|---|---|
| HubCenter 上游结算 / 供给授权层 | Hub / 租户在 HubCenter 的官方算力供给授权额度 | 控制官方算力供给、租户授权和 HubCenter 自身成本 | 否 |
| Hub 用户消费层 | Hub 中调用该服务的最终用户、其实际扣款服务组 | 对终端用户计费并扣减其充值/授予的 Credits | 是 |

HubCenter 不能把“供给授权已扣”误当作 Hub 用户已扣；Hub 也不能用 HubCenter 的授权余额替代用户服务组余额。一个官方请求可以在两个层级各有一条不同主体的账务记录，但**Hub 用户消费层只能扣一次**。两层均以同一 `request_id` 作为幂等键；HubCenter 重试不得导致 Hub 用户重复扣款，Hub 重试也不得重复消耗 HubCenter 供给额度。

### 7.2 官方服务商加入 Hub 服务组后的端到端流程

以下流程适用于管理员将内置 `maclaw-official` 服务商加入某个 Hub 服务组（例如“MaClaw 官方服务组”，服务组倍率 `×2`）的场景：

```text
1. Hub 管理员配置服务组
   服务组 G：可用模型 + provider_id=maclaw-official + billing_group_multiplier=2
   G 负责权限、用户余额与倍率；不保存基础输入/输出价格。

2. 用户调用 Hub /v1 接口并取得计费报价
   Hub 认证用户 → 解析逻辑模型 → 选定实际扣款服务组 G
   → 生成 request_id，向 HubCenter Quote 请求冻结实际官方路由与分时基础价格
   → Hub 以报价、G 的倍率和 max_tokens 预授权用户 Credits
   → 请求携带 tenant、service_group_id、request_id、quote_token、请求开始时间转发给 HubCenter。

3. HubCenter 使用已报价的官方真实服务商路由
   验证 quote_token、上下文和过期时间 → 使用报价已冻结的“服务商 + 上游模型”及分时基础价格
   → 生成 attempt_id，调用上游 → 获取 input_tokens / output_tokens
   → 可在 HubCenter 写入上游结算 / 授权层记录。

4. HubCenter 回传结果和基础价格快照
   返回实际 provider、upstream_model、输入/输出 Token、命中的分时基础价格、价格版本。
   不返回面向终端用户的最终 Credits，也不替 Hub 应用服务组倍率。

5. Hub 最终结算终端用户账单
   从快照读取 Pi / Po，读取实际扣款服务组 G 的 Mg=2
   → 计算最终 `deducted_microcredits`
   → 同一原子操作写入 Hub 用户账本、消耗预授权并释放未使用额度或补扣差额。

6. Hub 汇总展示
   用户统计、用户排行榜与 Hub 服务组余额均读取 Hub 用户账本；
   原始 Token 保持原值，实际扣除显示倍率后的 Credits。
```

示例：HubCenter 命中官方服务商 `opencode-1` 的夜间分时价格（输入 `0.5`、输出 `2` Credits / 万）；Hub 服务组 `G` 的倍率为 `×2`。用户输入 20k、输出 5k：

```text
HubCenter 基础价格：输入 0.5，输出 2
Hub 服务组最终价格：输入 1，输出 4
Hub 用户扣款：20k / 10k × 1 + 5k / 10k × 4 = 4 Credits
```

这里 `×2` 只由 Hub 的服务组 `G` 应用一次；HubCenter 不得再次把 `×2` 编入基础价格。

若官方调用发生超时或 Hub 与 HubCenter 断连，Hub 不能根据“未收到响应”断定未消费。它先把请求保持为 `executing` / `usage_unresolved`，按 `request_id + attempt_id` 查询 HubCenter 尝试状态：确认未发送才释放预留；确认已完成则按最终 usage 结算；状态未知则保留预留并限制同一用户的重复提交，直到超出对账窗口后进入人工/自动补偿流程。

### 7.3 官方价格快照协议

官方协议由两个固定接口组成：`Quote` 用于执行前冻结价格，`Proxy` 用于回传最终 usage 与可审计快照。HubCenter 的 Proxy 响应使用 `X-MaClaw-Pricing-Snapshot`：非流式响应在 Header 返回 **Base64URL 编码的 UTF-8 JSON**；流式响应在结束 Trailer 使用同名 Header 返回。不得向 OpenAI 兼容响应 body 注入私有元数据。快照须设置严格大小上限（建议 4 KiB），仅包含**已报价且实际执行的上游服务商路由的基础价格事实**：

HTTP Header / Trailer 在链路中可能被代理剥离，且 Header 在响应开始后不能改写。因此，`X-MaClaw-Pricing-Snapshot` 仅作为**在线快速结算通道**，不能作为唯一事实来源。HubCenter 必须同时提供受 Hub-to-HubCenter 内部认证保护的 `GET /internal/llm/billing-attempts/{attempt_id}`（或等价接口）；它按 `Hub ID + tenant_id + request_id + attempt_id` 返回同一不可变快照与最终 usage。Hub 未收到或校验失败 Trailer 时进入 `usage_unresolved` / `pricing_unresolved`，通过该接口对账，绝不信任客户端回传的 Header。

```json
{
  "request_id": "req_...",
  "quote_id": "quote_...",
  "attempt_id": "attempt_...",
  "provider_route_id": "route_...",
  "billing_policy_version": "policy_...",
  "usage": {
    "input_tokens": 20000,
    "output_tokens": 5000,
    "cached_input_tokens": 0,
    "cache_write_tokens": 0,
    "source": "provider_final"
  },
  "provider_id": "official-provider-a",
  "upstream_model": "opencode-1",
  "pricing_timezone": "Asia/Shanghai",
  "pricing_window_id": "weekday-night",
  "pricing_started_at": "2026-08-23T01:30:00+08:00",
  "input_credits_per_10k": 0.5,
  "output_credits_per_10k": 2.0,
  "input_rmb_per_10k": 0.01,
  "output_rmb_per_10k": 0.04,
  "minimum_request_credits": 0.1,
  "pricing_version": "2026-08-23-v1"
}
```

协议要求：

1. `request_id` 必须由 Hub 在请求前生成并随 `X-MaClaw-Request-ID` 转发；快照的 `quote_id`、`provider_route_id`、价格版本、`billing_policy_version` 和请求开始时间必须与预授权时的报价一致。Hub 不接受不匹配、缺失、超限或不可解析的快照。
2. 非流式响应在 Header 返回快照；流式响应在结束 Trailer 返回最终实际路由和快照。Hub 必须在读取完流后再结算，并在预授权有效期内等待 Trailer。
3. 快照只含基础价格，**不含服务组倍率、不含最终 `deducted_microcredits`**。
4. 快照必须带标准化的最终 usage，或与同一 `request_id + attempt_id` 的可认证 usage 记录关联；非流式可从响应 body 交叉校验，流式可从 usage chunk 或专用 Trailer 取得。没有 usage 时不能把预授权上限当成实际用量；进入 `usage_unresolved` 并由对账任务补齐，只有明确的产品策略才可按最低消费结算。
5. Hub 在账本中保存原始快照或规范化字段；不能在账单生成时向 HubCenter 重新查询当前价格。
6. 快照属于 HubCenter 与 Hub 的内部认证链路数据。Hub 只接受来自已认证官方代理连接的快照；不得接受客户端透传、伪造或由外部服务商提供的同名 Header。HubCenter 还必须验证 Proxy 请求的 `quote_token`，使快照与实际执行的已报价路由不可替换。
7. 官方调用缺少有效快照时，Hub 必须按明确的故障策略处理：推荐拒绝新计费型请求；已发生的请求保留预授权并进入 `pricing_unresolved`，由受控补偿任务或已配置的兼容价格策略结算。仅在后者下继续时，账本必须标记 `pricing_source=legacy_fallback`，绝不能悄然回退到旧单倍率。

### 7.4 Hub 外部服务商的同构路径

外部服务商不经过 HubCenter，但计费产物完全相同：Hub 在选定 `provider_id + upstream_model` 后，按其本地分时价表生成同样的基础价格快照；再应用实际扣款服务组倍率，写入同一 Hub 用户账本。这样官方与外部服务商的用户余额、报表和审计接口无需分叉。

## 8. HubCenter 与 Hub 的职责

| 层级 | 服务商类型 | 基础价格配置者 | 服务组倍率配置者 | 用户扣款执行者 |
|---|---|---|---|---|
| HubCenter 算力商店 | MaClaw 官方服务商 | HubCenter 管理员 | Hub 管理员（本地服务组） | Hub（基于价格快照） |
| Hub 模型接入 | 租户外部服务商 | Hub 管理员 | Hub 管理员（本地服务组） | Hub |

HubCenter 对官方调用必须返回第 7.3 节定义的结构化**基础价格**快照。Hub 取得快照后，以最终实际扣款服务组倍率计算最终价格和 Credits。现有 `X-MaClaw-Credit-Multiplier` 不再作为扣款依据；过渡期仅可用于排障兼容。

## 9. 数据迁移与兼容

1. 旧服务商/模型没有价格时，自动生成兼容价格：输入 `1`、输出 `1 Credit / 万 Token`，并标记 `pricing.version=legacy-v1`。
2. 现存服务商/模型 `credit_multiplier` 先兼容映射为 `dispatch_cost_multiplier`，继续服务于已有同倍率负载均衡/选路；不得直接迁移或删除。新增服务组 `billing_group_multiplier`，默认 `1`，作为新账单的唯一倍率来源。
3. 历史 `llm_usage_records.credits_deducted` 与余额数据保持不动；不从历史 Token 反推新账单。
4. 新账本启用后，用户统计优先读取账本；旧累计报告显示为“历史原始用量”，避免与新 Credits 并列混算。
5. 迁移期双读：旧 `credit_multiplier` 仅可供调度使用，任何新用户账单均不得读取它；待调度改为直接比较路由分时基础价格后再单独废弃该兼容字段。
6. 新账本、预授权、最终结算启用后，再逐步废弃以 `usage.TotalTokens` 直接调用 `EstimateCreditsWithFloor` 的单价扣费路径。

## 10. 接口与界面验收

### 10.1 配置验收

1. HubCenter 与 Hub 的服务商模型编辑器都能独立设置输入、输出 Credits / 万 Token。
2. RMB 字段在 UI 上清晰标注“参考展示，不参与扣费”。
3. 缺少 Credits 定价的收费路由无法保存或启用；免费路由必须显式标为免费。
4. 修改价格后，新的请求使用新版本，旧账单不变。
5. 价格、最低消费均为零但 `billing_mode=paid` 的路由无法启用；`billing_mode=free` 的路由不创建用户预授权或收费账单。
6. 非正数、NaN 或 Inf 的服务组倍率和基础价格无法保存；运行时收到无效已冻结快照时请求进入 `pricing_unresolved`，不按默认倍率继续扣费。
7. 修改服务组成员、授权或倍率后，已签发 Quote 的 `billing_policy_version` 和预授权金额保持不变；后续请求使用新策略重新授权。

### 10.2 计费验收

1. 输入 20k、输出 5k；输入价 1、输出价 4、倍率 ×2，扣除正好 8 Credits。
2. 相同总 Token、不同输入输出比例，扣除 Credits 应不同。
3. 服务商分时窗口在请求开始时正确选择输入/输出基础价格；跨时段的流式请求不重新计价。
4. 官方路由收到 HubCenter 分时基础价格快照后，只应用最终实际扣款服务组倍率一次。
5. 本地缓存命中扣 0；上游 prompt cache 命中仍按本期总输入规则计费。
6. 最低消费只对成功请求生效；失败请求不扣。
7. 两个并发请求不会超过同一用户服务组的可用 Credits。
8. 相同 usage、价格、倍率和 `request_id` 重放时，`deducted_microcredits` 必须完全一致；0.0005 Credit 边界按 half-up 规则得到确定结果。
9. reasoning token 归入输出且仅扣一次；`cached_input_tokens` 是统计子集，不得令输入 Credits 增加两次。
10. 价格修改生成新版本；在报价后、执行前修改价格时，该请求仍使用报价中的旧快照，之后的新报价才使用新版本。
11. 用于预授权的输入 Token 必须由实际路由的 Tokenizer 或 HubCenter 的受控上限产生；Hub 使用不同 tokenizer 的估计不可造成未授权的上游发送。
12. 客户端取消或断开时，Hub 仅在确认尝试未发送后释放预留；已发送请求以最终 usage 或可信零用量证明结算。
13. Quote、快照或访问日志中不得出现 `quote_token`、API Key、Prompt 或 Completion；跨租户调用同一类接口不能通过伪造的 tenant/service-group 字段读取或结算他人账单。

### 10.3 报表验收

1. 用户报表的 `actual_credits` 等于同一时间范围账本 `deducted_microcredits` 按固定比例换算后的总和。
2. 服务商统计的输入/输出 Credits 之和等于账本对应分项之和。
3. 排行榜切换“Credits / 原始 Token”后顺序可不同，且两种维度标签明确。
4. 任意一笔扣款可用 `request_id` 回溯到完整单价、倍率、Token 和版本快照。

### 10.4 官方服务商端到端验收

1. 将 `maclaw-official` 加入 Hub 服务组 `G`，配置 `G.billing_group_multiplier=2`，为测试用户发放 `G` 的 Credits。
2. HubCenter 命中官方服务商的一个已知分时窗口，响应返回该窗口的基础价格快照。
3. Hub 接收快照、以 `G` 的倍率计算最终 Credits，并只扣该用户在 `G` 的余额一次。
4. 修改 `G` 的倍率后，相同 HubCenter 基础快照得到不同 Hub 用户扣款；修改 HubCenter 分时价格后，新的请求基础价格改变而 `G` 倍率不变。
5. HubCenter 上游结算/授权记录与 Hub 用户账本可通过 `request_id` 关联，但二者余额变动不相互替代。
6. 流式官方请求从 Trailer 取得最终快照后正确扣款；Trailer 缺失时按故障策略处理，不能悄然按旧单倍率扣费。
7. HubCenter 在预授权前返回 `quote_id` / `quote_token`；Hub 依据报价预留额度。过期、篡改、服务组或模型不匹配的 quote_token 必须在上游调用前被拒绝。
8. 报价后的官方路由不可用时不得静默切换到不同基础价格的路由；旧预授权被释放，新报价和新预授权成功后才可重试。
9. Hub 重试响应读取或 HubCenter 重试代理时，Hub 用户账本与 HubCenter 供给授权账均按 `request_id` / `attempt_id` 幂等，不会重复扣款。
10. Hub 在代理超时后不能直接释放预留或发起第二次上游发送；须经 `request_id + attempt_id` 对账确认未发送。已发送或状态未知的尝试保留预留，直至对账完成。
11. Header / Trailer 被代理剥离或流式连接中断时，Hub 通过内部 `billing-attempts` 对账接口取回同一不可变 usage 与快照；该接口拒绝 Hub ID、租户、request_id 或 attempt_id 任一不匹配的读取。

## 11. 实施顺序

1. 在共享模型中增加版本化的服务商路由 `token_pricing`、稳定 `provider_route_id`、分时校验和定点 `microcredit` 单位；完成 HubCenter、Hub API 和管理端编辑器。
2. 实现 `PricingQuote`：HubCenter Quote 签发并校验官方 `quote_token`，Hub 外部服务商本地生成同构报价；加入报价过期、路由不可用和故障切换策略。
3. 建立价格快照与对账协议：HubCenter 官方代理在 Header / Trailer 返回认证的 Base64URL 快照，并提供按 Hub/租户/请求/尝试限域的内部对账查询；两者均强制与 `quote_id`、路由和版本校验一致。
4. 新增不可变计费账本、幂等预授权、原子最终结算、租户边界访问控制和遗留预授权对账任务，再把余额扣减改为从账本使用 `deducted_microcredits`。
5. 在 Hub 加入“官方服务商加入服务组”的结算支路：冻结实际扣款服务组、根据报价预授权、读取快照、仅应用 `billing_group_multiplier` 一次。
6. 改造用量统计、用户排行榜、服务商访问日志，分离原始 Token 与实际 Credits。
7. 落地 outbox、配置审计、每日跨层对账、追加式补偿分录及账本/请求内容分离的保留策略。
8. 迁移旧配置为 `legacy-v1`，灰度启用新价格；核对账本、余额和报表一致性后停止旧计算路径。
