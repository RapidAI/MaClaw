# Token 银行（模型分享）功能设计

> 状态：设计稿 **v8**（2026-10-02）
> v6 改动：**积分落点修正为 hub 侧 `Grant`**——架构改为「hubcenter 记账 + hub 单向提取」，
> 转赠走同一条提取通道（§14）；hubcenter 购买钱包的账务改造确认无关并**已回滚**（§14.8）
> **v7 改动**：v6 换架构后有一批 v5 正文没跟着改（§3.3/§3.6/§5/§10/§12.1 仍在引用已回滚的
> `CreditSpendable` 与购买钱包），本次逐条对齐；新增 **§16**：新账本 `token_bank_accounts`
> 在 hubcenter 三节点下会**丢更新**（与被回滚的购买钱包是同一个病），以及
> microcredits 与浮点账本的精度自相矛盾
> 日期：2026-10-01
> 实现记录（2026-10-01）：§10 的 P0-8、P0-9、P0-10、P1 与 D7 已按 §17.11 落地。P1 第 9 条沿用 hub `Source=token_bank` 的 permanent Grant。P0-4 的界面与定向测试已有，桌面端到端点击未做。
> 实现记录（2026-10-02）：§3.6 的领取落地页 `GET /c/{code}` 与桌面深链 `maclaw://credit/<code>` 已落地。领取仍不转账；浏览器页登录后绑定，桌面打开 Token 银行并预览，由用户点领取后再提取到本机。
> 实现记录（2026-10-02）：§6.1 / §7.3 的分享卡分时已落地。`GET /api/v1/token-bank/shares?range=today|month|all`（默认 all）与模型明细按中国时区自然日汇总 `token_bank_usage` 的消耗、手续费与所得。桌面面板可切换今日 / 本月 / 累计；明细展示已结算数字，不在客户端重算单价。
> 实现记录（2026-10-02）：Token 银行积分**不能购买算力卡**，也**没有法币提现**，两项都不排期。花费只发生在提取到本机后的 hub Grant 被助手消耗时。「提取」不是提现。
> 实现记录（2026-10-02）：§9 P2 #14–#20 已落地。私有分享按 hub/租户放行；排行榜按用量净额与徽章；桌面可一键导入探测通过的模型、追加密钥并改访问范围；新模型 24 小时内约 5% 流量，不健康则继续限流；`hck_` 可创建和批量创建分享。#20 毛利接口与界面此前已有。本批没有加法币提现，也没有用积分购买算力卡。
> 涉及子系统：MaClaw GUI（Wails + React）、corelib（llmpool / config）、hubcenter（Go + SQLite + web/admin）

**版本演进**

| 版本 | 改动 |
|---|---|
| v2 | 档位改用**三个平台服务阵列**承载（§3.4）；阵列不可删除、空阵列不被回收；代码已落地（§10 P0-0） |
| v3 | 7 条决策拍板（§11） |
| v4 | ① 积分不兑换法币，Credits 是唯一计价单位，全链路不存在"元"（§5、§12）；<br>② **没有提现功能**（§12 范围外清单）；<br>③ **「取出」= 删除共享信息**，不设 `withdrawn` 软删状态（§3.5）；<br>④ **review 修正**：手续费方向更正为「从分享者所得扣 10%」（原 v3 误记为向消费者加价，§11 决策 2）；<br>⑤ **review 修正**：倒挂风险并未消除，改由 §5 ⑥ 兜底（§13）；<br>⑥ 新增**积分分享（转赠）**：单笔 ≤ 可用 50%，链接单次领取（§3.6） |
| **v5** | 逐条核对代码后的 review：**档位倍率不能再复用 `ProviderArray.CreditMultiplier`**（§3.4 致命项 A1），阵列下发会连 `TokenPricing` 一起清空（A2）；修正 §5 计价口径、§11 决策 6、§13 倒挂判断；§14 补 5 个必改点（HA 回放丢桶、`SettlePending` 无流水、debt 抵扣破坏 SUM、写入点实为 12 处、opening 种子需确定性 id）。完整证据见 **§15** |
| **v6** | 积分落点改为 **hub 侧 `Grant`**：hubcenter 只记账（`token_bank_accounts`）+ hub 单向 pull 提取（§14 重写）；§14 的购买钱包流水汇总改造**回滚**（§14.8） |
| **v7** | 对齐 v6 遗留的 v5 正文（§3.3/§3.6/§5/§10/§12.1）；新增 **§16**：新账本跨节点丢更新（E1🔴）、micro 与浮点精度矛盾（E2🔴）、`token_bank_withdrawals` 缺表（E3）、提取接口未进 API 清单（E4）、Grant 的 ServiceGroupID 概念混用（E5）、hub 多实例提取归属（E6） |
| **v8** | 2026-10-02 review（证据见 `docs/design/token-bank-design-review-v8-zh.md`）：<br>① **新增 §14.9/§18 F1（🔴 必修，待拍板）**：账本异步复制 + 提取读本地余额 ⇒ **跨节点 flush 窗口内可超发**；append-only 只保证回放幂等，不保证并发串行化；<br>② **新增 §18 HA 同步边界表**：只有 `token_bank_ledger_batch` 进 HA 同步，`price_book`（钱规则）/ `withdrawals`（重放凭据）/ `usage`（对账前提）/ `shares` 全不同步；`llm_provider` 亦不同步 ⇒ `tbk_` 成员单节点可见；§13「密钥随 HA 扩散」风险据此**改判为不成立**；<br>③ **R3/R8 补落地**：hub 侧已有 7 个文件（`hub/internal/center/token_bank.go` 等），§16.4 的"hub 侧零代码"与 §17 漏记的 11+ 文件一并补齐；吸收 `TokenBankAutoRequestID` / `TokenBankRemainingMicro` 两个比文档更严谨的实现细节；<br>④ **R4 修正**：§8 第 9 条改 claim-only 口径（原 v5 的"Debit+Credit 同事务"与 §14.7 冲突）；<br>⑤ **R5/R9/R10/R11/R12**：unfreeze 幂等约定改代码实际写法、§5 余额表改 `*_micro` 并补 `granted` 桶、舍入描述与"每条腿独立 ceil"对齐、E1 段落改已拍板语气、§10 完成度标注对齐 |

> ⚠️ **v4 有一处口径待你最终确认**，见 §5 顶部注记与 §11 决策 2：手续费到底从分享者扣还是向消费者加价。
> 目前按你 2026-10-01 的原话「从分享者所得里扣 10%」实现。

> 🔴 **v5 提出一个必须由你拍板的新问题**：档位倍率当前的实现通道会**把倍率算进消费者实付**
> （high 档消费者被收 2 倍），与「消费者无感」冲突。见 §3.4-A1 与 §11 决策 6 的修订说明。
> 三阵列本身可以保留（用于分组、调度与展示），但**倍率必须改走独立字段**。

**v2 已落地部分（代码已完成，见 §10）**：三个不可删除的 Token Bank 阵列、空阵列不被自动清理、
删除保护、自动化 API 的 `token_bank_tier` / `array_id` 字段与文档。

---

## 1. 目标与一句话定义

**Token 银行**：用户把自己已配置、已测试通过的模型服务商（URL + APIKey + 可用模型）加密托管到 hubcenter，
平台把它接入公共模型池供其他用户消费；这些调用消耗的 token 按
**「平台单价 × 档次倍率 − 10% 手续费」** 算成 **Credits（积分）** 记入分享者账户，
**积分终身有效**，可累计、可查看、可消费、**可转赠给其他人**（§3.6，单笔上限 50%）。

> **v4 口径（已拍板）：Credits 是系统里唯一的计价单位，积分的单位就是 Credits。**
> 不设"积分 → 人民币"的换算，**没有提现功能**，整条链路上不存在"元"；
> 分享者所得 = 本次消耗 Credits − 平台手续费（10%），见 §5。

三方角色：

| 角色 | 干什么 | 得到什么 |
|---|---|---|
| 分享者（Provider Owner） | 托管服务商与模型 | 终身有效的积分 |
| 消费者（Tenant） | 通过 Hub 正常调用模型，不感知来源 | 正常的模型服务 |
| 平台（hubcenter 管理员） | 设定手续费率、模型档次评级、启停与取出 | 手续费 + 生态供给 |

---

## 2. 现状盘点：能直接复用的既有能力

动手前已核实，绝大部分基础设施已存在，**不需要从零造**：

| 能力 | 既有实现 | 位置 |
|---|---|---|
| 服务商配置结构 | `corelib.MaclawLLMProvider`（含 `ConnectionTestPassed`） | `corelib/types.go:953` |
| "测试通过"判定 | 前端字段 `connection_test_passed` | `LLMConfigPanel.tsx:186` |
| 服务商 chip 渲染（按钮落点） | `llm-config-provider-chip` 循环 | `LLMConfigPanel.tsx:1122` |
| 设置 tab 配置体系 | `settingsTabs.ts` / `settingsTabConfig.ts`（有覆盖性断言测试） | `guiapp/frontend/src/config/` |
| Wails 绑定三处 | `App.js` / `App.d.ts` / `models.ts`（自动生成，只加 Go 方法） | `guiapp/frontend/wailsjs/go/main/` |
| 客户端加密上传 | RSA-OAEP + PBKDF2 + AES-256-GCM，`GET /api/v1/crypto/pubkey` | `hubcenter/internal/skillmarket/crypto.go` |
| 服务端密钥落库加密 | `sm_api_keys.encrypted_key` + `encryptKey/decryptKey` | `internal/skillmarket/apikey_service.go:52` |
| Token 计价内核 | `llmpool.TokenPricing`（Credits per 10K tokens，microcredits 精度 1e6） | `corelib/llmpool/token_pricing.go` |
| 档次倍率 | `DefaultCapabilityBillingMultiplier`：low=0.5 / mid=1 / high=2 | `corelib/llmpool/capability_billing.go:33` |
| 服务商注册表 | `llmpool.ProviderConfig`（含 `Paused`、`Models`、`TokenPricing`） | `corelib/llmpool/types.go:9` |
| 服务商暂停 | `PUT /api/admin/llm/providers/{id}/paused` | `llm_routes.go:63` |
| 消费链路 | Hub → `POST /api/llm/v1/chat/completions` 代理 | `llm_routes.go:31` |
| 用量流水 | `llm_usage_records`（含 `credits_deducted`） | `internal/store/sqlite/llm_repo.go:45` |
| 计费幂等 | `llm_proxy_billing_attempts`（request_id 主键） | `llm_repo.go:132` |
| ~~积分钱包~~ | ~~`CreditsService.Credit/Debit/GetBalance`（`sm_users`，int64）~~ ⚠️ **v6 起与 Token 银行无关**（那是 SkillMarket 购买钱包，决策 11 明确不混用） | `internal/skillmarket/credits_service.go` |
| **积分真正落点**（v6） | hub 的 `llmservice.Grant`（`CreditsTotal`/`CreditsUsed`/`Permanent`/`UsageEvents`） | `hub/internal/llmservice/registry.go:287`；创建点 `service.go:884/1663/2113/2153` |
| 用户账户 | `sm_users`（id/email/credits/settled_credits） | `internal/skillmarket/types.go:8` |
| admin 卡片列表 + 分页 | `compute-market-tab.js` / `petstore-admin.js` 样板 | `hubcenter/web/admin/assets/js/` |
| admin 新增左侧 tab | `index.html:48` nav + section + script + `admin-core.js` 四处登记 | 见 §7.3 |

**不存在、需从零建的**：服务商"归属人"概念、模型档次评级（low/mid/high）、
可配置手续费率（现为硬编码 `PetStorePlatformFeePct = 30`）、Token 银行领域表与 API。
（取出的语义很轻：就是删除共享信息，见 §3.5。）

---

## 3. 端到端流程

### 3.1 分享（客户端侧）

```
[设置 → 大模型配置] 服务商 chip（仅 connection_test_passed=true 且非 hub 内置服务）
   └─ 点「🏦 Token 银行」按钮
        └─ 确认弹窗（原文案，见 §7.1）
             └─ 分享对话框
                  ① 拉取该服务商全部模型（/models 探测）→ 列表，默认全选
                  ② 点「确认」→ 逐模型短探测（并发 N，进度条 X/N）
                     → 标出可用模型（绿）/ 不可用（灰，说明原因，默认取消勾选）
                  ③ 设置可分享 token 上限：输入上限 / 输出上限（默认 0 = 无限）
                  ④ 提交：取 hubcenter 公钥 → 本地 AES-256-GCM 加密 payload
                     → POST /api/v1/token-bank/shares（带幂等键，防重复提交）
```

几个必须写死的细节（v4 review 补）：

- **token 上限在哪执行**：`max_input_tokens_per_request` / `max_output_tokens_per_request`
  存在服务端，由 **hubcenter 在代理转发前**校验，不是客户端自觉。
  输出上限通过改写请求的 `max_tokens` 强制；输入上限超限直接拒绝（返回 400 并记 `last_error`）。
- **提交幂等**：`POST /shares` 带 `Idempotency-Key`（= `client_instance_id + ':' + key_fingerprint`），
  服务端 24h 内同键直接返回首次结果，防用户连点造成重复分享。
- **重名模型不冲突**：不同用户可能分享同名模型（都有 `gpt-4o`）。调度单位是成员 ID
  （`tbk_<share_id>__<model>`，全局唯一），同名模型只是同一服务组里的多个副本，走既有负载均衡即可。
- **客户端探测 ≠ 服务端探测**：客户端探测（§3.1 ②）只是**分享时的初筛**，结果落到
  `token_bank_models.available`。服务端还应做**定期复探**（P1 #6 健康度的数据来源），
  两者写同一组字段，以服务端结果为准。

### 3.2 入库与接入（hubcenter 侧）

```
解密 payload → 校验归属用户(session) → 生成 share_id
   → 写入 token_bank_shares / token_bank_models
   → 为**每个**勾选且探测可用的模型创建一个 registry 成员 ProviderConfig
        · ID = tbk_<share_id>__<model_urlsafe>
        · Models = [该模型]，URL/Key 复用同一份
        · ArrayID = token_bank_mid（默认落点）
   → 管理员或自动化 API 再按模型标档位，移入 low / high 阵列（见 §3.4）
   → 挂到指定（或默认）ServiceGroup，进入调度池
```

### 3.3 消费与结算

```
消费者 → Hub → POST /api/llm/v1/chat/completions
   → 调度命中某个 tbk_ 成员 → 真实上游
   → 【消费者侧】按所属服务组的既有定价扣 credits_deducted（**完全不变，消费者无感**）= charged
   → 【分享者侧】同一份 usage 再记一笔 token_bank_usage：
        tier_multiplier = token_bank_models.tier_multiplier  ← 0.5/1/2，只在这里用
        gross = Σ(tokens/10000 × price_book 单价) × tier_multiplier  ← 本次消耗 Credits
        fee   = gross × fee_rate                                     ← 10%，平台手续费
        net   = gross − fee                                           ← 入账分享者
        倒挂保护：net = max(0, min(net, charged))                     ← 平台不倒贴
   → TokenBankCredit(owner_user_id, net_micro, desc)   ← 入 hubcenter 的 `token_bank_accounts`
        （v6/v7：**不碰** hubcenter 的购买钱包，也不碰已回滚的 `CreditSpendable`。
          这笔钱要等 hub 侧提取后才变成 agent 助手能花的额度，见 §14）
   → 幂等键：request_id（复用 llm_proxy_billing_attempts 思路）
```

> **两侧定价彼此独立**，这是刻意的：消费者侧沿用服务组既有的定价（不因来源是 Token Bank 而变，
> 消费者无感）；分享者侧按 `token_bank_price_book × 档位倍率` 计算。平台毛利 = `charged − net`。
>
> **v5 更正**：A1 修订后，消费者侧的 `provider` 倍率恒为 1.0，`charged` 里**不含**档位倍率，
> 而 `gross` 含——两侧因此**真的独立**，倒挂**确实存在**（v4 §13 的结论对，但当时给的理由是错的：
> 它以为两侧独立，实际上 v2 方案下两侧同源）。倒挂仍由 §5 ⑥ 的 `min(net, charged)` 兜底。
>
> **⚠️ `charged = 0` 的情形**（免费路由 / 赠送额度 / `BillingModeFree`）会让 `net` 被压成 0：
> 分享者白干活且毫无感知。必须在结算时记 `net_clamped=1` 标记，并由 P1 #10 收益曲线暴露，
> 否则"我的模型明明被调用了却没积分"会变成高频客诉。

### 3.4 三阵列模型（v2 提出，v5 修正倍率通道）

平台启动时确保存在三个服务阵列（ProviderArray），用于**分组、调度与后台展示**：

| 阵列 id | 用途 | 档位 | 分享者侧档位倍率 `tier_multiplier` |
|---|---|---|---|
| `token_bank_low` | 低档共享模型 | low | 0.5 |
| `token_bank_mid` | 中档共享模型（分享后默认落点） | mid | 1.0 |
| `token_bank_high` | 高档共享模型 | high | 2.0 |

设计要点：

1. **档位粒度是「服务商下的某个具体模型」**。阵列按**成员（provider）**分组，而一个成员可以带多个模型，
   所以**分享时每个模型创建一个成员**（`id = tbk_<share_id>__<model>`，共享同一份 URL/Key，
   `Models` 只含该模型）。这样同一个服务商的不同模型才能分在不同档位。
2. **标档位 = 移阵列**。`PATCH /api/admin/llm/providers/{id}` 带 `{"token_bank_tier":"high"}`，
   即把该模型成员移到 `token_bank_high`，同时把档位写进 `token_bank_models.tier`。
   **阵列只负责"这个模型属于哪一组"，不再负责倍率**（原因见下面 A1）。
3. **不可删除、不会被清空回收**。阵列标 `System=true` + `Manual=true`：
   `dropEmptyProviderArrays` 不再回收它们（最后一个模型取出后阵列仍在），
   `DeleteProviderArray` 返回 `ErrArrayProtected`。（此项已落地，见 §10 P0-0。）

#### 🔴 A1（致命）：档位倍率不能复用 `ProviderArray.CreditMultiplier`

v2 的假设是"倍率由阵列下发，计费侧不用感知，走既有链路即可"。**核实代码后该假设不成立**：

```
syncProviderArrayBilling            provider_array.go:275
  └─ copyArrayBillingToProvider     provider_array.go:358
       dst.CreditMultiplier = arr.CreditMultiplier      ← 阵列倍率写进成员
proxyRequestBillingCredits          proxy.go:954
  ├─ ProviderMultiplier = ResolveCreditMultiplier(provider.BillingPolicy())   proxy.go:897
  ├─ displayMultiplier  = CombineCreditMultipliers(ProviderMultiplier, route) proxy.go:984
  └─ credits = Estimate(tokens, Pricing, displayMultiplier)                   proxy.go:991
```

`ResolveCreditMultiplier`（`corelib/llmpool/credit_multiplier.go:28`）**无条件**返回成员的
`CreditMultiplier`，并被乘进**消费者实付**。后果：

- 消费者调用 high 档共享模型，**被扣 2 倍 credits**；low 档只扣 0.5 倍；
- 管理员改一次档位 = **改一次消费者价格**，与决策 6「消费者侧定价完全不变」直接冲突；
- 反过来，这也意味着 v4 §13 说的"两侧定价独立、倒挂依然存在"**前提错了**——
  两侧同源（都含 tier 倍率），倒挂其实不会发生（除非服务组 route 倍率 < 1 或走免费路由）。

**修订方案**：三个阵列的 `CreditMultiplier` **一律保持 1.0**，只当分组容器；
档位倍率独立存两处——`token_bank_models.tier`（low/mid/high）+ `token_bank_models.tier_multiplier`
（数值快照），**只在分享者结算侧使用**，不进消费者计费链路。
结算时 `tier_multiplier` 由 tier 映射得到（0.5/1/2，与 `DefaultCapabilityBillingMultiplier` 同值），
保证「消费者无感」与「分享者按档位拿分」同时成立。

#### 🟠 A2（必须改代码）：阵列下发会清空成员的 `TokenPricing`

`copyArrayBillingToProvider`（`provider_array.go:365`）**连定价一起覆盖**：

```go
dst.TokenPricing = arr.TokenPricing.Clone()   // 无条件覆盖
```

三个 Token Bank 阵列只设了 `CreditMultiplier`，`TokenPricing` 为空。于是：
`EffectiveRouteTokenPricing`（`token_pricing.go:367`）在服务组 route 没有
`TokenPricingOverride` 时**回退到 `provider.TokenPricing`**——而它刚被清空，
`ResolveTokenPricing` 因 `!HasCreditPricing()` 返回 false（`token_pricing.go:389`），
`snapshot == nil`，消费者侧**拿不到价格**，只能走 `proxy.go:1004` 之后的兜底分支。

**修订方案**（二选一，推荐第一个）：
1. 改 `copyArrayBillingToProvider`：阵列**仅在自己确有定价时**才覆盖成员定价
   （`if arr.TokenPricing.HasCreditPricing() { dst.TokenPricing = ... }`）；
2. 或给 `token_bank_*` 三个阵列显式配 `TokenPricing`。但这样平台统一定价就进了消费者侧，
   与决策 6 冲突，不推荐。

#### 🟡 A3：`EnsureTokenBankArrays` 每次启动强制改回倍率

`token_bank_arrays.go:120-123`：只要 `arr.CreditMultiplier != spec.Multiplier` 就改回。
它会覆盖管理员的手工调整；且它**不清理 `CreditMultiplierSchedule`**——一旦有人给阵列配了分时窗，
`ResolveCreditMultiplier` 优先命中 schedule（`credit_multiplier.go:36-39`），`CreditMultiplier` 形同虚设。

**修订方案**：按 A1 把三个阵列的 `CreditMultiplier` 固定为 1.0 后，Ensure 只补缺、不再强制改已有值；
并加校验**禁止**给 `token_bank_*` 阵列配 `CreditMultiplierSchedule` / `Timezone`。

### 3.5 暂停 / 恢复 / 取出

> **v4 明确：「取出」= 删除该服务商的共享信息，没有别的语义。**
> 不留 `withdrawn` 软删状态，不做"停用但保留"的中间态——删就是删干净。

| 操作 | 动作 | 数据 |
|---|---|---|
| 暂停 | registry Provider `Paused=true`，不再调度；共享信息**仍在** | status=paused，保留全部历史与累计积分 |
| 恢复 | `Paused=false`，重新进入调度 | status=active |
| **取出** | **删除共享信息**：先结算在途用量 → 从 registry 摘除该服务商的全部模型成员 → 密钥安全擦除 → **物理删除** `token_bank_shares` / `token_bank_models` 记录 | **已赚到的积分保留在用户账户**（凭 `token_bank_usage` 流水），只是未来不再产生新积分 |

取出的完整顺序（不可颠倒）：

```
1. 停止调度（立刻 Paused=true，拒新请求）
2. 结算在途用量（可能有已发出未结算的请求，落账后再删）
3. 从 LLM Registry 摘除该 share 下的所有 tbk_<id>__<model> 成员
4. encrypted_api_key 覆盖写空 + 内存明文 secretbox 清零
5. DELETE token_bank_models WHERE share_id=?
6. DELETE token_bank_shares WHERE id=?
7. token_bank_usage 流水保留（用户的积分凭据 + 明细页数据源）
```

只有 `active` / `paused` 两个状态，`status` 列**不再设 `withdrawn` 值**，
`withdrawn_at` 列一并去掉——删掉的行不需要墓碑。

### 3.6 积分分享（转赠给其他人）

用户可以把**自己赚到的积分**分享给别人。单次分享上限 = **当前可用积分的 50%**，
生成一条链接，**只有第一个打开并领取的人能拿到**（单次消费）。

```
【发送方】GUI 设置 → Token 银行 → 顶部积分总览 → [分享积分]
    ① 输入积分数（服务端校验 ≤ floor(可用 × 50%)）
    ② 服务端：冻结该笔积分（写入 credit_share_links，status=active）
    ③ 返回链接：https://<host>/c/<code>  与深链 maclaw://credit/<code>
    ④ GUI 展示链接 + [复制链接] + 二维码

【接收方】打开链接 → 落地页显示「xxx 分享给你 N 积分」
    → 登录（需 verified）→ [领取]
    → 服务端原子 claim：条件 UPDATE 绑定 claimed_by（只有第一个人成功），**不动账**
    → 接收方提取到自己的 hub 时才实际过账（§14.7）
    → 接收方流水 description = "来自 x***@xx.com 的积分分享"

【未领取】发送方可撤销（解冻）；到期自动解冻（默认 7 天）
```

**防超发（关键）**：分享出去但还没被领走的那部分必须**冻结**，否则用户可以连开 10 个链接、
每个都填 50%，10 个人各领一次就超发了。

```
可用积分 available = token_bank_accounts: earned + received − withdrawn − granted − frozen
冻结中   frozen    = Σ credit_share_links.credits WHERE sender=? AND status IN ('active','claimed') AND 未过期
单笔上限           = floor(available × 0.5)

> **过账时机（v5 拍板）**：创建链接时只**冻结**；**实际从发送方扣除**发生在接收方把积分
> **提取到自己的 hub** 的那一刻（§14.7）。领取只做「绑定第一个人」，不动账。
```

#### 🔴 C1：提取接口看不见"冻结中"，可以提走已冻结的积分

> **v7 重写**：v5 这条针对的是 `CreditsService.Debit`（购买钱包），v6 已回滚那条路。
> 但**漏洞本身没消失，只是换了个入口**——现在是 **§14.2 的提取接口**。

提取接口的校验如果只做 `available = earned + received − withdrawn − granted` 而**忘了减 `frozen`**，攻击路径就是：

```
1. 用户账本 earned = 100，生成 10.00 的分享链接（frozen_micro = 10 × 1e6）
2. 立刻提取全部 100 → 校验通过（100 ≥ 100），hub 侧拿到 100 额度的 grant
3. 链接被人领走 → 接收方提取 10
   → 发送方账本变成 −10（负余额），接收方凭空多 10：净超发 10
```

**改法**（两条都要，缺一不可）：

1. **提取接口的 `available` 必须包含 `frozen`**（§14.3 的公式已写，但要在**代码**里落实，
   并写成单测：冻结 10 后最多只能提 90）。
2. **冻结必须是同一事务内的独立列更新**，不能只在 `credit_share_links` 里记一笔——
   否则账本和链接表会各自漂移。撤销 / 过期时必须 `frozen_micro −= N`，
   且要防负值（并发撤销 + 领取会让减两次）。

> 注意 `frozen` 的口径要包含**已领取但未提取**的链接（§3.6 公式已含 `claimed`），
> 否则"已领取还没提"的那段时间窗口就是敞口的。

#### 🟡 C2：解冻时发送方余额可能已经不够

撤销 / 过期解冻是 `UPDATE ... status='revoked'` + 释放冻结额度。若在冻结期间用户已把钱花掉，
"释放"出来的其实是**不存在的余额**。必须定义清楚：

- 撤销时校验 `credits ≥ 0`（本来就没真扣钱，冻结只是额度锁定，不算真扣）→ 直接释放即可；
- 若按 C1 方案 1 把冻结做成了真实流水，则解冻 = 反向流水，天然自洽；
- **无论哪种，都要在 admin 的积分分享审计里给出"冻结/解冻/领取"三态对账**（§6.2 已有接口，补字段即可）。

**单次领取的原子性（关键）**：

```sql
-- 单个 SQLite 事务（BeginImmediate，仿 CreditsService.Debit 的写法）
UPDATE credit_share_links
   SET status='claimed', claimed_by_user_id=?, claimed_by_email=?, claimed_at=?
 WHERE id=? AND status='active' AND (expires_at='' OR expires_at > ?)
-- RowsAffected == 1 才算抢到；== 0 则返回 ErrAlreadyClaimed / ErrLinkExpired
-- v6/v7：claim **只绑定人，不动账**。真正扣账发生在接收方提取时（§14.7 第 ③ 步）
```

**HA 三节点下的领取（不能只靠同步）**：见 §8 第 9 条——领取必须路由到创建该链接的节点裁决，
否则两个节点各自本地事务成功，同步时按 `ha_entity_versions` 覆盖，**同一笔积分会被领两次**。

---

## 4. 数据模型（hubcenter SQLite，草案）

迁移范式：在 `internal/store/sqlite/llm_repo.go` 的 `EnsureLLMTables` 里追加 `CREATE TABLE IF NOT EXISTS`；
补列走 `ensureXxxColumn` 范式。仓库**无版本表**，不要引入 migration 版本号。

```sql
-- 分享的服务商
CREATE TABLE IF NOT EXISTS token_bank_shares (
  id                    TEXT PRIMARY KEY,
  owner_user_id         TEXT NOT NULL,            -- sm_users.id
  owner_email           TEXT NOT NULL,
  client_instance_id    TEXT NOT NULL DEFAULT '', -- 来源客户端实例（可审计）
  member_id_prefix      TEXT NOT NULL DEFAULT '', -- registry 成员 ID 前缀 = tbk_<id>__，
                                                  -- 实际调度单位是「每模型一个成员」，见 token_bank_models.member_id
  display_name          TEXT NOT NULL,
  api_url               TEXT NOT NULL,
  protocol              TEXT NOT NULL DEFAULT 'openai',
  encrypted_api_key     TEXT NOT NULL,            -- 复用 apikey_service 加密方案
  key_fingerprint       TEXT NOT NULL DEFAULT '', -- 掩码展示用，不存明文
  status                TEXT NOT NULL DEFAULT 'active',   -- active|paused（v4：无 withdrawn，取出=物理删除）
  visibility            TEXT NOT NULL DEFAULT 'public',   -- public|private（P2）
  service_group_id      TEXT NOT NULL DEFAULT '',
  max_input_tokens_per_request  INTEGER NOT NULL DEFAULT 0,  -- 0 = 无限
  max_output_tokens_per_request INTEGER NOT NULL DEFAULT 0,
  daily_token_cap       INTEGER NOT NULL DEFAULT 0,          -- P1 熔断
  monthly_token_cap     INTEGER NOT NULL DEFAULT 0,          -- P1
  total_credits_micro   INTEGER NOT NULL DEFAULT 0,          -- 累计获得（终身）
  last_error            TEXT NOT NULL DEFAULT '',
  paused_reason         TEXT NOT NULL DEFAULT '',
  created_at            TEXT NOT NULL,
  updated_at            TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_tbk_shares_owner  ON token_bank_shares(owner_user_id, status);
-- 同一个用户不该把同一把 key 分享两次（换 key 走 PUT /key，不是重新分享）
-- 取出是物理删除，行没了自然不冲突，所以这里不需要 WHERE 过滤
CREATE UNIQUE INDEX IF NOT EXISTS idx_tbk_shares_dedup
  ON token_bank_shares(owner_user_id, key_fingerprint);

-- 该服务商下被分享的模型
CREATE TABLE IF NOT EXISTS token_bank_models (
  id                TEXT PRIMARY KEY,
  share_id          TEXT NOT NULL,
  model_name        TEXT NOT NULL,
  member_id         TEXT NOT NULL DEFAULT '',      -- registry 成员 id = tbk_<share_id>__<model>
  array_id          TEXT NOT NULL DEFAULT 'token_bank_mid', -- v5：只作分组/调度用，恒 CreditMultiplier=1.0
  tier              TEXT NOT NULL DEFAULT 'mid',   -- v5 新增：low|mid|high|custom，档位的真相源
  tier_multiplier   REAL NOT NULL DEFAULT 1,       -- v5 新增：档位倍率快照，只用于分享者结算
  enabled           INTEGER NOT NULL DEFAULT 1,
  available         INTEGER NOT NULL DEFAULT 0,    -- 最近一次探测结果
  last_probe_at     TEXT NOT NULL DEFAULT '',
  last_probe_error  TEXT NOT NULL DEFAULT '',
  used_input_tokens  INTEGER NOT NULL DEFAULT 0,
  used_output_tokens INTEGER NOT NULL DEFAULT 0,
  earned_credits_micro INTEGER NOT NULL DEFAULT 0,
  UNIQUE(share_id, model_name)
);
CREATE INDEX IF NOT EXISTS idx_tbk_models_share ON token_bank_models(share_id);
CREATE INDEX IF NOT EXISTS idx_tbk_models_array ON token_bank_models(array_id);

-- 每次消费的结算流水（明细页数据源）
CREATE TABLE IF NOT EXISTS token_bank_usage (
  id                    INTEGER PRIMARY KEY AUTOINCREMENT,
  request_id            TEXT NOT NULL,
  share_id              TEXT NOT NULL,
  owner_user_id         TEXT NOT NULL DEFAULT '',   -- 冗余：share 被删除后流水仍需归属到人
  share_display_name    TEXT NOT NULL DEFAULT '',   -- 快照：服务商取出后明细页仍显示得出资助方是谁
  model_name            TEXT NOT NULL,
  consumer_hub_id       TEXT NOT NULL DEFAULT '',
  consumer_tenant_id    TEXT NOT NULL DEFAULT '',  -- 只用于防滥用统计，不展示给分享者
  input_tokens          INTEGER NOT NULL DEFAULT 0,
  output_tokens         INTEGER NOT NULL DEFAULT 0,
  cached_input_tokens   INTEGER NOT NULL DEFAULT 0,
  cache_write_tokens    INTEGER NOT NULL DEFAULT 0,
  unit_in_credits_per_10k  REAL NOT NULL DEFAULT 0,
  unit_out_credits_per_10k REAL NOT NULL DEFAULT 0,
  price_book_id         TEXT NOT NULL DEFAULT '',   -- 命中的定价表条目，'' = 走 default_unit_*
  tier                  TEXT NOT NULL DEFAULT 'mid',
  tier_multiplier       REAL NOT NULL DEFAULT 1,
  fee_rate              REAL NOT NULL DEFAULT 0.1,
  gross_credits_micro   INTEGER NOT NULL DEFAULT 0,  -- 本次消耗 Credits（× 倍率后，扣手续费前）
  fee_credits_micro     INTEGER NOT NULL DEFAULT 0,  -- 平台手续费 = gross × fee_rate
  net_credits_micro     INTEGER NOT NULL DEFAULT 0,  -- 入账分享者 = gross − fee（已过倒挂保护）
  charged_credits_micro INTEGER NOT NULL DEFAULT 0,  -- 消费者实付（服务组定价，倒挂保护上界）
  formula_json          TEXT NOT NULL DEFAULT '',   -- 明细页展示计算过程
  net_clamped           INTEGER NOT NULL DEFAULT 0, -- v5 新增：1 = 被倒挂保护压过（charged 不足），运营需看得见
  created_at            TEXT NOT NULL,
  UNIQUE(request_id, share_id, model_name)
);
CREATE INDEX IF NOT EXISTS idx_tbk_usage_share_time ON token_bank_usage(share_id, created_at);
CREATE INDEX IF NOT EXISTS idx_tbk_usage_model      ON token_bank_usage(share_id, model_name);
CREATE INDEX IF NOT EXISTS idx_tbk_usage_owner_time ON token_bank_usage(owner_user_id, created_at);
```

-- 分享提交幂等（v5 新增：§3.1 ④ 说了 24h 幂等，v4 却没有表承载它）
CREATE TABLE IF NOT EXISTS token_bank_share_requests (
  idempotency_key  TEXT PRIMARY KEY,   -- client_instance_id + ':' + key_fingerprint
  owner_user_id    TEXT NOT NULL,
  share_id         TEXT NOT NULL,      -- 首次成功创建的 share，同键回放直接返回它
  response_json    TEXT NOT NULL DEFAULT '',
  created_at       TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_tbk_share_req_owner ON token_bank_share_requests(owner_user_id, created_at);

-- 积分分享链接（单次领取，转赠用）
CREATE TABLE IF NOT EXISTS credit_share_links (
  id                 TEXT PRIMARY KEY,
  code               TEXT NOT NULL,                 -- 链接里的随机码：32 字节 CSPRNG → base64url，43 字符
  sender_user_id     TEXT NOT NULL,
  sender_email       TEXT NOT NULL DEFAULT '',       -- 展示时掩码，不直接外泄完整邮箱
  credits_micro      INTEGER NOT NULL,               -- 分享的积分数（微积分精度）
  status             TEXT NOT NULL DEFAULT 'active',-- active|claimed|revoked|expired
  claimed_by_user_id TEXT NOT NULL DEFAULT '',
  claimed_by_email   TEXT NOT NULL DEFAULT '',
  origin_node_id     TEXT NOT NULL DEFAULT '',       -- 创建该链接的节点；HA 下由它裁决领取（§8 第 9 条）
  expires_at         TEXT NOT NULL DEFAULT '',       -- 空 = 不过期；默认 7 天
  created_at         TEXT NOT NULL,
  claimed_at         TEXT NOT NULL DEFAULT '',
  revoked_at         TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_credit_share_code   ON credit_share_links(code);
CREATE INDEX IF NOT EXISTS idx_credit_share_sender_status ON credit_share_links(sender_user_id, status);

> **为什么要冗余 `owner_user_id` 和 `share_display_name`**：
> 「取出」会**物理删除** `token_bank_shares` 行（§3.5），但流水要留着当积分凭据。
> 没有这两个字段，用户取出服务商后打开历史明细会看到一堆没有名字、没有归属的孤儿记录。

**系统设置**（写 `system_settings`，key = `token_bank_settings`，仿 `RegistrySettingKey` 用法）：

```json
{
  "fee_rate": 0.10,
  "fee_target": "provider",
  "default_unit_input_credits_per_10k": 3.0,
  "default_unit_output_credits_per_10k": 6.0,
  "default_unit_cached_read_credits_per_10k": 0.3,
  "default_unit_cache_write_credits_per_10k": 3.75,
  "require_review_before_online": false,
  "require_verified_identity": true,
  "auto_pause_consecutive_failures": 5,
  "max_shares_per_user": 20,
  "credit_share_max_ratio": 0.5,
  "credit_share_link_ttl_hours": 168,
  "credit_share_daily_limit": 10,
  "credit_share_min_credits": 1
}
```

字段含义（v4）：
- `fee_rate` 0.10：手续费 10%
- `fee_target` `provider`：**手续费从分享者所得里扣**（`net = gross × (1 − fee_rate)`）。
  保留这个字段只为将来切 `consumer` 时不必改代码；当前**只按 `provider` 实现**
- `default_unit_*_credits_per_10k`：**平台统一定价基准**，单位就是 Credits per 10K tokens，
  命中不到定价表条目时的兜底价。**纯积分，不与任何法币挂钩**
- `require_review_before_online` false：分享即上线，**不做服务商审核**
- `require_verified_identity` true：**用户身份必须已验证**（邮箱）才允许分享服务商 / 领取积分
- `credit_share_max_ratio` 0.5：**单笔积分分享上限 = 可用积分的 50%**（§3.6）
- `credit_share_link_ttl_hours` 168：分享链接 7 天未领取自动过期解冻
- `credit_share_daily_limit` 10：每人每天最多生成 10 条分享链接（防刷）
- `credit_share_min_credits` 1：单笔下限，避免灰尘转账

> 单价只有一处来源：下面的 `token_bank_price_book` 定价表（admin 维护）。
> 表中存的也是 Credits per 10K，没有人民币字段。官方人民币价最多作为 admin 填表时的**参考信息**，
> 不进计算链路。

**平台积分定价表**（新表，admin 在「Token 银行 → 定价基准」里维护）：

```sql
CREATE TABLE IF NOT EXISTS token_bank_price_book (
  id                     TEXT PRIMARY KEY,
  model_pattern          TEXT NOT NULL,   -- 精确模型名，或前缀通配 'gpt-4o*'
  unit_input_credits_per_10k        REAL NOT NULL DEFAULT 0,
  unit_output_credits_per_10k       REAL NOT NULL DEFAULT 0,
  unit_cached_read_credits_per_10k  REAL NOT NULL DEFAULT 0,
  unit_cache_write_credits_per_10k  REAL NOT NULL DEFAULT 0,
  updated_at             TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_token_bank_price_pattern ON token_bank_price_book(model_pattern);
```

命中顺序：**精确模型名 → 前缀通配（最长优先）→ `system_settings` 里的 `default_unit_*`**。

---

## 5. Credits 计算（唯一口径）

单位：`llmpool.MicrocreditsPerCredit = 1_000_000`，配置以十进制 Credits 录入，计算走整数微积分防漂移。

**v4 口径（2026-10-01 决策）：Credits 是唯一计价单位，没有法币换算。**
**单价取自平台积分定价表；手续费 10% 从分享者所得里扣。**

> ⚠️ **口径待确认（见 §11 决策 2）**：v3 曾把手续费写成"向消费者加价、分享者拿 100%"，
> 但你在决策 2 的原话是「**从分享者所得里扣 10%**」。本节按**你的原话**写，即 `net = gross × 90%`。
> 若你实际想要"分享者拿满额、手续费向消费者另收"，把 ④ 改成 `net = gross`、
> `charged = gross × 1.1` 即可，其余不变。两种口径的差别只在钱归谁，不影响其它设计。

```
① 单价取自平台积分定价表 token_bank_price_book
   unit_*_per_10k 的单位就是 Credits per 10K tokens（不是元，不换算）
   命中顺序：精确模型名 → 最长前缀通配 → system_settings.default_unit_*

② token 项小计
   in_cost  = ceil(input_tokens  / 10000 × unit_in_per_10k  × 1e6)
   out_cost = ceil(output_tokens / 10000 × unit_out_per_10k × 1e6)
   cr_cost  = ceil(cached_read   / 10000 × unit_cr_per_10k  × 1e6)
   cw_cost  = ceil(cache_write   / 10000 × unit_cw_per_10k  × 1e6)
   base     = in_cost + out_cost + cr_cost + cw_cost

③ 档次倍率（v5：取自 `token_bank_models.tier`，**不是**阵列的 `CreditMultiplier`）
   tier_multiplier = low 0.5 / mid 1.0 / high 2.0
   阵列的 CreditMultiplier 恒为 1.0（§3.4-A1），保证该倍率不会渗进消费者实付

④ 本次消耗 Credits（gross）
   gross = round(base × tier_multiplier)
   注意：gross 是「这次调用消耗了多少 Credits」，单位就是 Credits，不再换算成别的

   🟡 v8 修正（R10）：v7 这里写「单次只舍入一次」，是错的。
   实际是**两条腿各舍入一次**：② 的四条 token 腿各自 `ceil`（向上取整），
   ④ 再乘倍率 `round` 一次——共 5 次舍入。实现与判据见
   `TestComputeTokenBankSettlementRoundsEachLegUp`（§17.8）。
   「每条腿独立向上取整」是刻意的：先求和再取整会让小额调用长期系统性少付，
   而腿级 ceil 的误差上界是 4 micro/次，方向恒定（偏多付），可预期比可抵消更重要。

⑤ 手续费与分享者所得
   platform_fee = round(gross × fee_rate)             ← 默认 10%，从分享者所得里扣
   net          = gross − platform_fee                ← 入账分享者的积分

⑥ 消费者实付与倒挂保护
   charged = 消费者所属服务组的既有定价（**与本公式无关**，消费者侧不感知 Token Bank）
   平台毛利 = charged − net
   倒挂保护：net = max(0, min(net, charged))
     —— 若服务组卖价低于分享者应得，平台不倒贴，最多把 net 压到 charged（平台毛利 0）
```

明细页展示的计算过程（分享者视角，用户要求"显示 credits 计算过程"）：

```
gpt-4o-mini   tier=high（token_bank_high，×2.0）   定价表: 3.0 / 6.0 Credits per 10K
  input   12,340 tok ÷ 10,000 × 3.0 = 3.7020 Credits
  output   3,120 tok ÷ 10,000 × 6.0 = 1.8720 Credits
  小计 base                         = 5.5740 Credits
  × 档位倍率 2.0（high）            = 11.1480 Credits   ← 本次消耗 Credits
  手续费 −10%（平台）               = −1.1148 Credits
  ─────────────────────────────────────────────────
  您获得                            = 10.0332 Credits   ← 记入您的积分账户，终身有效
```

**账户余额的三个数**（GUI Token 银行卡片上显示，对应用户最初的要求）：

| 显示项 | 含义 | 计算 |
|---|---|---|
| 累计获得 | 所有分享的服务商累计为您赚到的积分 | `earned_micro + received_micro`（§14.3，账本 `SUM` 为准） |
| **已提取** | 已提取到 hub 变成额度的（**不是**"被助手花掉"，助手实际消耗在 hub 侧记账） | `token_bank_accounts.withdrawn_micro` |
| **已转赠** | 转赠链接已被对方提取、真正从您账上扣掉的部分 | `token_bank_accounts.granted_micro` |
| 冻结中 | 转赠链接已生成但尚未过账的部分 | `token_bank_accounts.frozen_micro` |
| 可用积分 | 还能提取多少 | `earned + received − withdrawn − granted − frozen`（§14.3），**无有效期，不会清零** |

> 🟡 **v8 修正（R9）**：v7 这张表写的是 `withdrawn_credits` / `frozen_credits`——**浮点命名且无 `_micro` 后缀**，
> 与 §14.3 最终版 `*_micro INTEGER` 不符；同时表里**漏了 `granted` 桶**，导致 §7.3 卡片上的减法
> （`总获得 − 已提取 − 冻结中 = 可用`）在用户转赠成功后会对不上。
>
> ⚠️ **术语统一**：全文档只用「**已提取**」，不再用「已消耗」。
> "已消耗"会被用户读成"已经被助手花掉了"，而它实际是"已提取到本机"——
> 助手真正花掉多少在 **hub 侧** Grant 的 `CreditsUsed` 里，hubcenter 根本不知道。
> 这两个数字分属两端，混用会直接制造"我的积分怎么少了"的客诉。

> 🔴 **v7 修正**：v5 这行写的是"取钱包 `credits` 余额（`GetBalance`）"——那是**购买钱包**，
> 而 v6 已经决定 Token 银行积分**不落购买钱包**（决策 11）。跟着 §14 改成 `token_bank_accounts` 口径。
>
> ⚠️ **UX 上必须说清楚的两个"可用"**：hubcenter 里的"可用"只是**可提取额度**，
> 和 hub 里 agent 助手真正能花的额度是**两个数字**。GUI 只显示一个"可用 88.45"会让用户以为
> 助手里已经有钱了。建议文案：「可用 88.45（提取后可在助手中使用）」+ 一键提取入口。

明细页同时给出「本次消耗 Credits」与「您获得」两个数，差额就是平台手续费——
用户要求"显示 credits 计算过程"，这两个数必须都可见，否则分享者会觉得积分被偷了。

---

## 6. API 清单（草案）

### 6.1 客户端（Bearer 用户会话）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/token-bank/pubkey` | 复用 `/api/v1/crypto/pubkey` 即可，不必新建 |
| POST | `/api/v1/token-bank/shares` | 提交加密分享包 |
| GET | `/api/v1/token-bank/shares` | 我的分享列表（含总积分/**已提取**/状态）；`?range=today｜month｜all` 控制分时统计口径 |
| GET | `/api/v1/token-bank/shares/{id}/models` | 模型明细 + 各模型 credits |
| PUT | `/api/v1/token-bank/shares/{id}/paused` | 暂停/恢复 |
| DELETE | `/api/v1/token-bank/shares/{id}` | 取出 = 删除共享信息（先结算在途 → 摘成员 → 擦密钥 → 物理删记录） |
| PUT | `/api/v1/token-bank/shares/{id}/key` | 原地换 Key（保留 id 与累计积分） |
| GET | `/api/v1/token-bank/summary` | 我的总积分 / 已提取 / 冻结中 / 可用 / 单笔可分享上限 |

**积分提取（v6 核心，v7 补进清单——§14.2 定义了它但 §6 漏了）**

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/v1/token-bank/credits/withdraw` | hub 侧调用。body `{request_id, amount, hub_id, kind, link_id}`；校验 `amount ≤ available`（**必须含 frozen**，C1）；幂等返回首次结果；成功后 hub 侧建 `Permanent` Grant |
| GET | `/api/v1/token-bank/credits/withdrawals` | 我的提取记录（含 hub_id / grant_id，供跨端对账与重放，见 E6） |

> 🟠 **v8 补漏（R8）**：上表是**用户会话**视角。实际还有几类端点，v7 清单里查不到：
>
> | 方法 | 路径 | 说明 |
> |---|---|---|
> | POST | （hub 专用提取端点） | `httpapi/token_bank_hub_withdraw.go`：独立的 hub 认证路径，body 含 `service_group_id`；**会校验服务组内确有 tbk_ 成员**，否则 400 `service_group_not_token_bank`（**E5 判据①已实现，v7 未记录**）；`kind` 仅 `self` / `gift` |
> | — | `httpapi/token_bank_claim_route.go` | §8 第 9 条：`claim` 路由到 `origin_node_id` 裁决，对端不可达时拒绝本地代领 |
> | GET | `/c/{code}` | `httpapi/token_bank_landing.go`：领取落地页（2026-10-02 已落地） |
> | GET | `/api/v1/token-bank/usage/daily`、`usage.csv` | `httpapi/token_bank_usage_handlers.go`：P1 #10 收益曲线 / #11 CSV 导出 |
> | — | `httpapi/token_bank_share_handlers.go` | 分享的 CRUD（提交 / 列表 / 暂停 / 取出 / 换 Key / 模型同步） |
>
> 深链 `maclaw://credit/<code>` 与桌面侧领取同样已落地（见文档顶部 2026-10-02 实现记录）。

**积分分享（转赠，§3.6）**

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/v1/credits/share-links` | 创建分享链接，body `{"credits": 30}`；校验 ≤ 可用×50%，返回链接与 `code` |
| GET | `/api/v1/credits/share-links` | 我发出的链接列表（含状态：待领取/已领取/已撤销/已过期） |
| POST | `/api/v1/credits/share-links/{id}/revoke` | 撤销未领取的链接（解冻积分） |
| GET | `/api/v1/credits/share-links/{code}/preview` | 公开只读预览：发送者掩码 + 数量 + 是否可领。**需限流** |
| POST | `/api/v1/credits/share-links/{code}/claim` | 领取（原子，单次）；不能领自己的；需 verified |

### 6.2 管理后台（RequireAdmin）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET/PUT | `/api/admin/token-bank/settings` | 手续费率 + 默认单价（Credits per 10K）等全局设置 |
| GET/POST/PUT/DELETE | `/api/admin/token-bank/price-book` | 平台积分定价表维护（模型名/通配 → 单价，单位 Credits per 10K） |
| GET | `/api/admin/token-bank/overview` | 整体统计（用户数/模型数/总消耗 tokens/总发放积分） |
| GET | `/api/admin/token-bank/users` | 分享用户卡片（分页：每页 20） |
| GET | `/api/admin/token-bank/shares` | 全部服务商（可按用户/状态筛选） |
| PUT | `/api/admin/token-bank/shares/{id}/paused` | 暂停/恢复 |
| DELETE | `/api/admin/token-bank/shares/{id}` | 取出（同上，删除共享信息） |
| PUT | `/api/admin/token-bank/shares/{id}/models/{model}/tier` | 模型评级 low/mid/high |
| GET | `/api/admin/token-bank/credit-shares` | 积分分享链接审计（发送方/接收方/数量/状态/时间），异常转赠排查用 |
| POST | `/api/admin/token-bank/credit-shares/{id}/revoke` | 管理员冻结可疑链接 |

### 6.3 自动化 API（hck_ 密钥，RequireLLMAdminScope）

在 `/api/admin/llm/*` 体系下加 Token 银行段，使 `hck_` 脚本化管理自己的服务商：
`GET /api/admin/llm/token-bank/shares`、`PUT .../paused`、`DELETE .../{id}`。
同时在 `llm_admin_api_doc.go` 的 Markdown / OpenAPI 文档里补章节。

**已实现（v2）**：档位标记走既有的 `PATCH /api/admin/llm/providers/{id}`（需要 write），新增两个字段：

```json
{"token_bank_tier": "high"}
{"array_id": "token_bank_high"}
```

- `token_bank_tier` 只接受 `low` / `mid` / `high`，把该模型成员移进对应的平台阵列；
  响应回显 `array_id` 与 `token_bank_tier`。非法档位返回 400。
- `array_id` 是通用写法，可移到任意已存在阵列（含非 Token Bank 阵列），阵列不存在返回 400。
- 两者不要同时发送。
- 删除 `token_bank_low` / `_mid` / `_high` 返回 400 `provider array is protected`。

文档位置：`hubcenter/internal/httpapi/llm_admin_api_doc.go`（`/api/llm/admin-api.md` 与
`/api/llm/admin-api.json` 由它生成）。

---

## 7. 前端设计

### 7.1 MaClaw GUI — 入口（设置 → 大模型配置）

落点：`guiapp/frontend/src/components/remote/LLMConfigPanel.tsx:1122` 的 `llm-config-provider-chip` 渲染循环内，
条件：`p.connection_test_passed === true && !p.is_hub_service`。

新增一个带银行图标的小按钮（建议复用现有 `PROVIDER_LOGOS` 同级的图标表，样式类 `llm-config-tokenbank-btn`）。

**确认弹窗文案**（按你给的原文，仅做排版整理）：

> **Token 银行**
> Token 银行功能将把当前服务商的访问信息保存到 MaClaw 服务器（安全传输）。
> 其他用户访问该服务商并消耗 token 后，将存到您的账户。
> 根据模型档次获得不同的积分，**积分终身有效**，今后将可随时消耗使用。
>
> [取消] [确认分享]

> **v4 补充（建议加一行小字，避免预期错位）**：
> 积分即 Credits。提取到本机后由助手消耗服务组额度，**不能购买算力卡**，不可提现、不可转让为人民币。

### 7.2 MaClaw GUI — 分享对话框（新组件 `TokenBankShareDialog.tsx`）

```
服务商：OpenAI 兼容 · https://api.xxx.com/v1        [已测试通过 ✓]

可分享模型（默认全选）                    [全选] [全不选]
 ☑ model-a    可用 ✓   (312 ms)
 ☑ model-b    可用 ✓   (188 ms)
 ☐ model-c    不可用 ✗  404 model not found
 ☑ model-d    待探测 …
 [开始探测]  ▓▓▓▓▓▓▓░░░░  3/5

可分享 token 上限（0 = 无限）
 单次输入上限 [    0 ]     单次输出上限 [    0 ]

                              [取消]  [确认分享]
```

### 7.3 MaClaw GUI — 新设置 tab「Token 银行」

改动文件（5 处，缺一会有覆盖性测试报错）：

1. `src/config/settingsTabs.ts` — `SettingsTabId` 加 `'tokenBank'`、`SETTINGS_CONTENT_TAB_IDS`、分组表、图标表、`getSettingsTabOptions`
2. `src/config/settingsTabConfig.ts` — 加入 `SETTINGS_TABS_SELF_LOADING`（面板自拉数据）
3. `src/components/settings/SettingsActiveContent.tsx:370` — `switch` 加 case
4. `src/appLazyComponents.ts` — 懒加载登记 `TokenBankPanel`
5. 新建 `src/components/settings/TokenBankPanel.tsx`

面板顶部是**积分总览条**，下面是每个分享的服务商一张卡片：

```
┌──────────────────────────────────────────────────────┐
│ 我的积分   总获得 128.45                              │
│            已提取 30.00    已转赠 0.00    冻结中 10.00 │
│            可用 88.45      单笔最多可分享 44.22        │
│                                    [分享积分]  [明细]  │
└──────────────────────────────────────────────────────┘
┌──────────────────────────────────────────────────────┐
│ 🏦 阿里云百炼   api.aliyun.com   运行中 ●             │
│ 分享模型 7 个 · 累计被消耗 1.24M tokens               │
│ 总积分 128.45      已提取 30.00      可用 88.45       │
│ 今日 +2.3   本月 +18.7   累计 +128.45  ← 分时切换     │
│        [明细]  [暂停]  [取出]                          │
└──────────────────────────────────────────────────────┘
```

**分享积分弹窗**（点顶部 `[分享积分]`）：

```
┌────────────────────────────────────────┐
│ 分享积分                                │
│ 可用 88.45，单笔最多可分享 44.22        │
│ 积分数 [  10.00  ]   [全部可分享]       │
│                                         │
│ ⚠ 链接只有第一个领取的人能拿到           │
│                        [取消] [生成链接] │
└────────────────────────────────────────┘

生成后：
┌────────────────────────────────────────┐
│ 分享 10.00 积分                         │
│ https://hub.maclaw.io/c/9Xk...（43字符）│
│ [复制链接]  [显示二维码]                │
│ 7 天内未被领取将自动退回                │
│ ── 我发出的分享 ─────────────           │
│ 10.00  待领取   7天后过期   [撤销]      │
│ 5.00   已被 x***@xx.com 领取  09-28     │
└────────────────────────────────────────┘
```

- 「明细」→ 弹窗列出该服务商每个模型的 credits，含 §5 的计算过程
- 「暂停/恢复」→ 二次确认；暂停期间卡片显示"已暂停"+ 暂停时刻
- 「取出」→ 强确认弹窗：**"取出将删除该服务商在平台的全部共享信息（访问地址、密钥、模型列表），删除后其他用户将无法再使用它。您已获得的积分不受影响，继续保留在您的账户中。此操作不可撤销。"**

### 7.4 hubcenter admin — 新左侧 tab「Token 银行」

按静态方式加（`platform` 组），8 处改动：

1. `web/admin/index.html:48` nav 加 `<button data-tab="tokenbank">`
2. `index.html` 加 `<section id="tab-tokenbank">`（含手续费率卡片 + 整体统计 + 用户卡片网格 + 分页）
3. `index.html:341-355` 加 `<script src="/admin/assets/js/token-bank-tab.js?v=1" defer>`
4. `admin-core.js:203` `tabMeta` 加 `tokenbank:['tokenBankTabTitle','tokenBankTabSubtitle']`
5. `admin-core.js:321` `openTab()` 加 `initTokenBankTab()` 钩子
6. `admin-core.js:209-215` `TAB_ICONS.tokenbank`
7. `admin-core.js:543/:590` i18n 中英双语键值
8. 改完 bump `index.html` 的 `?v=`（契约要求）

内容：

- **手续费率**：输入框，默认 10%，保存即写 `system_settings`
- **整体信息**：几个用户分享 / 分享多少个模型 / 总共被消费多少 tokens / 已发放多少积分 / **已转赠多少积分**
- **用户卡片**：每行 4 个，每页 20 个；每张卡显示——用户（邮箱掩码）、分享服务商数、模型数、被消耗 tokens、获得积分；点击可下钻到该用户的服务商列表

### 7.5 hubcenter admin — 模型接入 tab 增「Token 银行服务商」管理区

在 `web/admin/index.html` 的 `tab-llmservice`（index.html:279-332）内新增一张卡片，或独立 section，
由 `llm-service-tab.js` 渲染（该文件已 4266 行，建议**新开 `token-bank-provider-admin.js`** 避免继续膨胀）：

- 列出所有 `tbk_` 服务商，每个展开显示其模型列表
- 每个模型一行：模型名 + **档次下拉（low / mid / high）** + 本次消耗 Credits + 累计获得积分
- 改档次 = 后台调 `PATCH /api/admin/llm/providers/{id}` 带 `token_bank_tier`，
  模型成员随即移到对应阵列，倍率立即生效
- 服务商级操作：暂停/恢复、取出（强确认）
- 三个平台阵列在阵列列表里标注「平台内置，不可删除」，删除按钮置灰

---

## 8. 安全设计（不可省略）

1. **传输与存储**：客户端用 hubcenter RSA 公钥做 OAEP 加密 salt + AES-256-GCM 加密 payload；服务端解密后再用 `apikey_service` 的 `encryptKey` 落库。**任何 API 出参的 `api_key` 恒为空**（仿 `safeProvider.APIKey` 恒空的做法）。
2. **掩码展示**：admin 与 GUI 只显示 `sk-...abcd` 形式的 fingerprint。
3. **取出即删除**：取出走物理删除，`encrypted_api_key` 先覆盖写空再删行；明文在内存用 `corelib/secretbox` 清零。不保留 `withdrawn` 墓碑行，避免"已删除的密钥还躺在库里"。
4. **限额**：单次 I/O token 上限（用户设置）+ 平台侧日/月上限（P1，防单 key 被刷爆）。
5. **合规提示**：分享弹窗需加一句"请确认您有权共享该服务商密钥"；建议维护一份**禁止共享的服务商黑名单**（部分厂商 ToS 明确禁止 key 共享）。
6. **HA 一致性**：多节点部署下**只有账本**走 HA 同步，其余 Token Bank 表**一律不同步**（完整清单见 **§18**）。
   > ✅ **v8 改判（原 D4）**：v5 担心 `token_bank_shares.encrypted_api_key` 进同步表会让
   > "单节点失守"升级成"全部托管密钥失守"。**核实后这条风险不成立**——
   > `token_bank_shares` 从来没进过 HA 同步（`ha/service.go` 的 `isSupportedEntityType` 里
   > Token Bank 只有 `token_bank_ledger_batch` 一个实体），密文不会扩散。
   >
   > 🟠 **但它换来两个新问题，v8 新登记**：
   > - **可用性**：`llm_provider` 同样不在同步清单内 ⇒ `tbk_` 成员**只在创建它的那个节点上可见**。
   >   该节点宕机，这批共享模型整体下线，**没有跨节点容灾**。
   > - **操作一致性**：admin 的暂停 / 改档 / 取出经负载均衡落到别的节点时，操作的是**另一份数据**，
   >   回读又可能落到第三个节点 ⇒ "改了没生效 / 时好时坏"。这是 D27/D28 那一族的静默失配，但更上层。
   > 两条都要么接受（并在运维手册写明"Token Bank 管理操作必须打到 share 所属节点"），
   > 要么单独给 `token_bank_shares` / `token_bank_price_book` 做同步——后者见 §18 的取舍。
7. **操作审计**（v4 review 补）：档位变更（`token_bank_tier`）、手续费率变更、定价表改动、
   管理员暂停/取出，都要留「谁 / 何时 / 前后值」。否则出现"我的模型倍率怎么变了"时无法自证。
   单价被改错是 §13 列出的活跃风险，审计是它的主要缓解手段。
8. **分享者身份不外泄**：`token_bank_usage` 的 `consumer_tenant_id` 只用于防滥用统计，
   绝不出现在分享者可见的任何接口里；反过来分享者的邮箱在消费者侧也不可见。

9. **积分分享链接的 HA 原子性（§3.6，最容易埋雷的一条）**：

   hubcenter 的 HA 同步是 `ha_sync_ops` 追加 + `ha_entity_versions` 版本比较
   （`internal/store/sqlite/migrations.go:250`，**没有名为 oplog 的表**，之前文档里"ha oplog"
   的说法指的就是这套）。它对 **append-only 的流水**（如 `llm_usage_records`、`token_bank_usage`）
   完全适用，但对**有状态实体的并发修改**不适用：两个节点各自本地事务成功、
   各写一条 op，同步时按版本覆盖，**同一笔积分会被领两次**。

   因此领取必须这样实现：

   - `credit_share_links.origin_node_id` 记录创建该链接的节点；
   - `POST .../claim` 被**路由到 `origin_node_id` 所在节点**执行（本地事务 + 条件 UPDATE，天然原子）；
   - 该节点不可达时返回「链接暂时无法领取，请稍后重试」，**绝不允许代理节点本地代领**；
   - 🔴 **v8 修正（R4）**：下面这条是 v5 的残留，**与 §14.7 冲突，已作废**——
     ~~"发送方 Debit 与接收方 Credit 必须在同一个事务内完成"~~。
     现口径是 **claim 只绑定第一个人，完全不动账**；真正的扣账发生在接收方
     **提取到自己 hub** 的那一步（§14.7 第 ③ 步，`SettleClaimedGift` 一笔事务内三行）。
     理由：接收方领了却一直不提取时，在第 ② 步扣账会把发送方的积分**无限期占住**。
   - 🔴 **同一个裁决思路必须延伸到提取接口**：账本是异步复制的，提取读本地余额，
     于是存在跨节点超发窗口——**F1，见 §14.9**。§8 第 9 条解决了"链接被领两次"，
     但真正动钱的是提取，它还没有同等保护。

   > ⚪ **已失效（随 §14.8 回滚）**：v5 在这里"顺带暴露"的购买钱包风险
   > （`sm_users` 四个字段 `credits` / `settled_credits` / `pending_settlement` / `debt`
   > 跨节点 last-write-wins）**与 Token 银行无关**——Token 银行积分不落 `sm_users`。
   > 领取走 `origin_node_id` 裁决是**永久**要求，不是"§14 改造完成前"的临时措施。

---

## 9. 我还建议补上的功能（按价值排序）

### P0 — 不做会出事 / 体验残缺

| # | 功能 | 理由 |
|---|---|---|
| 1 | **Key 失效自动降级 + 提醒** ✅ §17.11 | key 过期/余额耗尽是最常见故障。连续失败 N 次自动暂停该服务商，并记一条错误，供分享者更新密钥 |
| 2 | **原地换 Key**（保留 id 与累计积分）✅ §17.11 | 换 key 走原分享 id，累计积分不断裂 |
| 3 | **取出（删除）前的在途用量结算** ✅ §17.11 | 删除瞬间可能有一批已发出未结算的请求。成员上的档位 / 倍率 / 显示名快照先落账，再允许取出 |
| 4 | **新模型上线提醒** ✅ §17.11 | 同一 API 已有分享时，探测到未加入的模型，提示「发现 N 个新模型，可以加入银行」 |
| 5 | **分享者自己的用量不计费（自用豁免）** ✅ §17.11 | 分享者自己调自己的服务商时识别 owner 并跳过结算 |

### P1 — 明显提升留存与平台健康度

| # | 功能 | 理由 |
|---|---|---|
| 6 | **健康度评分 + 调度权重** ✅ §17.11 | 样本满 5 次后，按成功率给 Token Bank 成员加权；平均延迟超过 5 秒再减半 |
| 7 | **日/月 token 熔断** ✅ §17.11 | 到达日或月 token 上限后，当笔仍入账，随后暂停分享并记下 `token cap` |
| 8 | **异常消耗告警** ✅ §17.11 | 当日 token 超过前 7 日日均的 5 倍时记 `anomaly` 并走同一条暂停。硬熔断优先于异常 |
| 9 | **积分消费闭环** ✅ 沿用既有 hub Grant（§12.1） | 没有提现（§12）。花费发生在 hub `Source=token_bank` 的 permanent Grant 被助手调用消耗时。购买钱包与 `CreditsService.Debit` 不进入这条链路（决策 11） |
| 10 | **分时/分模型收益曲线** ✅ §17.11 | `GET /api/v1/token-bank/usage/daily`：近 30 天净额与 Top 10 模型 |
| 11 | **对账导出（CSV）** ✅ §17.11 | `GET /api/v1/token-bank/usage.csv`，最多 5000 行 |
| 12 | **服务商黑名单（ToS 合规）** ✅ §17.11 | `provider_denylist` 按主机名拦截，命中返回 403 `provider_denied` |

### P2 — 锦上添花 / 生态玩法

| # | 功能 | 理由 |
|---|---|---|
| 14 | **私有分享**（只给指定 hub/租户用） ✅ 2026-10-02 | 企业内部场景刚需 |
| 15 | **分享者排行榜 + 徽章** ✅ 2026-10-02 | 按 `token_bank_usage.net_micro` 排名，不是模型上的 `earned_micro` |
| 16 | **一键批量导入** ✅ 2026-10-02 | 桌面对端点上的模型逐个探测，只提交可用的 |
| 17 | **多 Key 轮转**（同服务商挂多个 key） ✅ 2026-10-02 | 同一分享追加密钥，出站轮换，最多 8 把 |
| 18 | **新分享灰度放量** ✅ 2026-10-02 | 新模型 24h 内约 5% 流量；窗口后样本不足或健康则全量，不健康继续限流。已有成员不重新计时 |
| 19 | **CLI / 自动化 API 批量分享** ✅ 2026-10-02 | `POST /api/admin/llm/token-bank/shares` 与 `/batch`，`hck_` write。密钥入库前加密 |
| 20 | **毛利润透视图**（admin） ✅ 此前已有 | 消费者实付 vs 分享者所得 vs 平台毛利的三角视图，定价决策必需 |

> 原第 20 条「积分转赠 / 捐赠」**已转正为正式功能**（你要的积分分享），见 §3.6。
> 它与「不可提现」不冲突：转赠是站内积分转移，提现是换成法币，后者仍然不做。

---

## 10. 分期实施计划

| 阶段 | 内容 | 产出判据 |
|---|---|---|
| **P0-0 阵列底座** ✅ **已完成**（含 A1/A2/A3 三项修正，见 §17.1） | `ProviderArray.System` 字段（`corelib/llmpool/types.go`）；`hubcenter/internal/llmservice/token_bank_arrays.go` 的 `EnsureTokenBankArrays` / `MoveProviderMemberToArray` / `SetProviderMemberTokenBankTier`；`DeleteProviderArray` 的 `ErrArrayProtected` 保护；`dropEmptyProviderArrays` 跳过系统阵列；`llm_init.go` 启动种子；`PATCH` 的 `array_id` / `token_bank_tier`；文档与 OpenAPI | `go test ./hubcenter/internal/llmservice/` 全绿（含 7 个新用例） |
| **P0-0a 倍率通道修正**（🔴 v5 新增，**阻塞 P0-3**）✅ **已落地** | A1：三个阵列 `CreditMultiplier` 改恒 1.0，档位改存 `token_bank_models.tier` / `tier_multiplier`；A2：`copyArrayBillingToProvider` 改为"阵列确有定价才覆盖"；A3：`EnsureTokenBankArrays` 只补缺、禁配分时窗 | **回归用例**：同一模型标 high 档，消费者实付**不变**（与标 mid 时一致），分享者所得变为 2 倍 |
| **P0-0b 积分账本与提取**（前置，可与 P0-1 并行）✅ **已落地**（store + httpapi + **hub 侧 pull 与 Grant**，见 §17.1 与 §17 的 v8 补漏表） | §14：`token_bank_accounts` 表 → 结算入账（浮点）→ 提取接口（幂等 `request_id`）→ hub 侧 `Source='token_bank'` permanent grant → 自动/手动提取触发 → 定期对账 | 重复 `request_id` 只扣一次；hub 余额低于阈值自动提取成功；两端对账无差异 |
| **P0-1 数据底座** ✅ **已落地**（9 张表 + repo 全套） | 四张表（`shares` / `models` / `usage` / `price_book`）+ `system_settings` + `EnsureLLMTables` 追加 + store 层 repo | 表建成，单测通过 |
| **P0-2 服务端 API** ✅ **§6.1 / §6.2 / §6.3 全部落地** | §6.1/6.2 全部接口 + 加密解密 + 幂等结算 | 接口可调用，结算幂等验证；判据见 §17.6 / §17.7 |
| **P0-3 结算接入** ✅ **已落地（§17.8）** | proxy 结算钩子 + 防倒挂 + **`token_bank_accounts` 入账**（v7：不是 CreditsService） | 一次真实调用后积分正确入账；**两节点并发结算后 `earned` 不丢（E1 并发测试）** ✅ 24×4 并发重放 exactly-once |
| **P0-4 GUI 分享链路** 🟡 **组件与定向测试已有** | chip 按钮 + 确认弹窗 + 分享对话框 + 探测进度 | 桌面里走完一次真实分享未点击，故不记「端到端分享成功」。vitest 覆盖 chip、对话框、探测横幅与未验证引导 |
| **P0-5 GUI Token 银行 tab** ✅ **已落地（§17.10）** | 卡片列表 + 总/**已提取**积分 + 明细 + 暂停 + 取出 | 五处改动齐全，`tsc`/vitest 绿（数据层 15 + 面板 13 + 导航 2 用例） |
| **P0-6 admin Token 银行 tab** ✅ **已落地（§17.9）** | 手续费率 + 整体统计 + 用户卡片分页 + 共享模型评级 + 定价表 + 转赠审计 | 六个子页可看、可改、可翻页；`TestAdminPage*` 全绿 |
| **P0-7 模型接入管理** ✅ **并入 P0-6 完成（§17.9 末节）** | 无需新增 UI：P0-6「共享模型」子页（`low/mid/high` 评级）已覆盖；结算侧**实时**读 `tier_multiplier`，无缓存 | 评级改后新请求按新倍率结算 ✅ 写(`SetModelTier`)→读(`TokenBankShareForPublish`) 链路已核；倍率随 usage 快照落库，**不追溯** |
| **P0-8 身份验证门** ✅ **已落地** | 分享前校验 `sm_users.status == verified`；未验证时桌面先展示服务端原文，仅当文案是身份门时才提供邮箱验证 | `TestTokenBankSubmitShareRejectsUnverifiedAccount` 返回 403 `identity_not_verified`。确认框只在文案含 `identity_not_verified` 或 `verify your account before sharing` 时出现。拒绝验证时分享对话框保持打开 |
| **P0-9 积分分享链接** ✅ **已落地** | `credit_share_links` 表 + API + 创建时冻结 + 50% 上限。领取只绑定接收人、不转账，并路由到 `origin_node_id`。接收方提取时先 `SettleClaimedGift`，再由该 hub 写入 `Source=token_bank` 的 Grant。C2 解冻差额不为负 | `TestGiftClaimIsFirstComeOnly`、`TestGiftClaimMovesNoMoney`、`TestGiftCannotOverIssueWhenSenderWithdrawsFirst`、`TestGiftUnfreezeShortfallDoesNotDriveFrozenNegative`、`TestTokenBankClaimRoutesToOriginAndRefusesLocalFallback` |
| **P0-10 GUI 分享积分** ✅ **面板、落地页与深链已落地** | 顶部积分总览条 + 分享弹窗 + 链接/二维码 + 我发出的列表 + 撤销。领取可以粘贴 code 或 URL，也可以打开 `https://<host>/c/<code>` 或 `maclaw://credit/<code>`。code 只在创建时返回一次 | 面板测试覆盖整额链接、列表不含 code、超额拦截、撤销确认、粘贴 URL 后按该笔金额提取，以及深链预填后不自动领取。`GET /c/{code}` 只对 10 位分享码返回页面，不回显非法路径。桌面窗口仍未点击 |
| **P1** ✅ **已落地（§17.11）** | §9 的 1–8 与 10–12：连续失败自动暂停、原地换 Key、在途结算、新模型提醒、自用豁免、健康度权重、日/月熔断、异常告警、30 天收益与 Top 模型、CSV、服务商黑名单 | 定向 `go test` 与 vitest 通过。桌面 GUI 未做交互点击，也未重新打包 |
| **P1-消费闭环** ✅ **沿用既有 hub Grant，本阶段无新代码** | 花费发生在 hub `Source=token_bank` 的 permanent Grant 被助手调用消耗时（§12.1）。不做提现，不接法币，购买钱包不进入这条链路（决策 11） | 闭环 = 结算入账 → 提取 → hub Grant → 助手消耗。Token 银行积分不能购买算力卡（2026-10-02 确认） |
| **P2** ✅ **2026-10-02** | §9 #14–#20：私有分享、排行榜与徽章、一键导入、多 Key 轮转、24h 灰度、`hck_` 批量创建。毛利透视图此前已有 | 定向 `go test` 与 vitest。没有法币提现，也不能用积分购买算力卡。桌面窗口未点击，安装包未重打 |

**UI 门禁（改完必跑，见项目备忘）**：`go build ./...` + gofmt + `go test` →
`go run scripts/check_wails_bindings.go` → `tsc --noEmit` + vitest →
`assemble-app-css.mjs` + `verify-app-css-baseline.mjs --update` →
`check-main-ui-guards.mjs` / `check-agent-architecture.mjs` / `check-encoding.mjs`。

---

## 11. 决策记录（2026-10-01 全部拍板，含 v4 追加）

| # | 议题 | 结论 |
|---|---|---|
| 1 | 定价归谁定 | **平台统一定价**（admin 维护「Token 银行定价基准」`token_bank_price_book`）。**单价单位就是 Credits per 10K，不与任何法币挂钩** |
| 2 | 手续费向谁收 | **从分享者所得里扣 10%**（你 2026-10-01 的原话）。即 `net = gross × 90%`。⚠️ v3 曾误记为"向消费者加价、分享者拿 100%"，本 v4 已按你的原话更正；若你实际想要后者，见 §5 顶部注记，**只改一处公式** |
| 3 | 积分能否提现 | **没有提现功能，也不能购买算力卡。** 积分不兑换人民币。提取到 hub 之后，只能由助手按服务组额度消耗，**终身有效、无有效期**。见 §12 范围外清单 |
| 4 | 要不要审核 | 服务商**不需要审核，分享即上线**；但**用户身份必须审核**（邮箱验证）。分级由 AI 自动化 API 处理 |
| 5 | 「暂停/恢复（分时显示）」 | 确认 = **暂停/恢复按钮 + 今日 / 本月 / 累计的分时统计切换**；默认是开通状态显示「暂停」，暂停后显示「恢复」 |
| 6 | 消费者端定价 | **沿用所属服务组的既有定价，完全不变**（消费者不感知来源是 Token Bank）。分享者侧才走平台统一定价表 × 档位倍率。<br>🔴 **v5 修订**：v2 的"档位 = 阵列 `CreditMultiplier`"实现**违反本条**——该值会被乘进消费者实付（§3.4-A1）。改为：阵列倍率恒 1.0，档位倍率独立存 `token_bank_models.tier_multiplier`，只在结算侧使用 |
| 7 | HA 同步 | **走 ha oplog**，Token 银行结算与分享数据随 `llm_usage_records` 同一条路径复制 |
| 8 | **积分与 Credits 的关系**（v4 追加） | **积分的单位就是 Credits**，不做二次换算、不设汇率、不出现"元"。<br>但**入账数 ≠ 消耗数**：入账 = 本次消耗 Credits − 10% 手续费（按决策 2）。明细页两个数都展示 |
| 10 | ~~积分余额的存储方式~~（v4，**已废弃**） | ~~改为流水汇总~~ → **已回滚**。该改造针对 SkillMarket 购买钱包，与 Token 银行无关（§14.8） |
| 11 | **Token 银行积分存哪**（v5） | **不在 hubcenter 的购买钱包**。hubcenter 记账（`token_bank_accounts`，浮点），hub 消费（permanent Grant，`Source=token_bank`），两端靠**单向提取**衔接（§14） |
| 12 | **转赠的实际扣账时机**（v5） | 创建链接时**预扣冻结**；接收方**提取到自己 hub** 时才真正从发送方扣除。7 天未提取自动解冻退回（§14.7） |
| 13 | **积分精度**（v5） | 接受 `float64`（与 Grant 一致），**不做整数化**；靠定期对账 + `adjustment` 流水修正漂移（§14.6） |
| 9 | **积分能否转赠**（v4 追加） | **能**，但只能站内转赠：单笔 ≤ 可用积分 **50%**，生成链接，**只有第一个人能领**，7 天未领自动退回；发送方未领取前可撤销。仍然**不可提现、不可换法币**。见 §3.6 |

连带影响：
- §5 公式按 1/2/6/8 重写（单价取自平台积分定价表、手续费从分享者扣、消费者侧定价不变、积分单位是 Credits）
- ⚠️ **「计费倒挂」风险并没有消除**（v3 判断错了，v5 重新判断）。v2 方案下两侧其实**同源**
  （都含 tier 倍率），倒挂本不会发生；**按 A1 修订后两侧才真正独立**，倒挂才真的存在——
  服务组卖价完全可能低于分享者应得。仍由 §5 ⑥ `net = max(0, min(net, charged))` 兜底
  （平台最多毛利 0，不会倒贴），并要求把 `net_clamped=1` 记下来做运营可见性。见 §13
- §13 的「法币合规 / 提现通道」风险因决策 3 不做提现而**整体消除**（§12）
- §4 的 `system_settings` 用 `fee_target` / `default_unit_*_credits_per_10k` / `require_verified_identity`，
  **删除 `credits_per_rmb` 与 `pricing_source`**；新增 `token_bank_price_book` 表
- `fee_target` 现在恒为 `provider`（从分享者扣），保留字段只为将来切换到 `consumer` 时不改代码

---

## 12. 范围外：不做提现

> **本功能没有提现能力，也不计划有。** 2026-10-02 确认：法币提现不需要，不排期。
> Token 银行只做一件事：把分享出去的 token 换成分享者账户里的 Credits。
> 积分可以提取到本机，由助手按服务组额度消耗；这不是打到银行卡，整条链路不出现法币。

因此以下内容**全部不在范围内**，不排期、不设计、不预留接口：
短信验证码通道、支付宝/微信打款、提现工单与审核流、对账、
「积分 → 人民币」汇率与官方价数据源对接。

产品文案：**积分不可提现、不可兑换为人民币**；**允许在站内转赠给其他用户**（§3.6）。
GUI 的 Token 银行 tab 不出现任何"提现""转出到余额"入口——只有「分享积分」，那是**转赠**不是提现。

### 12.1 让积分花得出去（P1，建议尽量前置）

没有提现，积分的价值就完全取决于"能不能花"。这一项是 P1 里性价比最高的。

| 能力 | 现状 | 位置 | 缺口 |
|---|---|---|---|
| 积分**消费**（真正花钱的那一步） | ✅ 已有 | hub 的 `Grant`（`CreditsTotal` / `CreditsUsed` / `UsageEvents`） | 无——Token 银行提取过来的就是它 |
| 积分**入账** | 🟡 要建 | `token_bank_accounts`（§14.3） | 结算钩子（P0-3） |
| 提取成可用额度 | 🟡 要建 | `POST /api/v1/token-bank/credits/withdraw`（§14.2） | 幂等 + 并发（E1）+ 冻结校验（C1） |
| 算力卡下单 | ✅ 已有 | hubcenter `llm_card_orders` + `internal/cardstore` | ⚠️ 那是**购买钱包**的路子，与 Token 银行积分无关（决策 11） |
| 余额/流水查询 | ✅ 已有 | hub `ResolveStatusFromRegistryForUser` | 需把 `Source="token_bank"` 的 grant 单独展示 |

> **v7 更正**：v5 这节把消费闭环写成了"复用 `CreditsService.Debit` 抵扣算力卡"，
> 那是购买钱包的路子。v6 决策 11 之后，消费发生在 **hub 的 Grant 被 LLM 调用消耗**的时候，
> 算力卡那条路对 Token 银行积分**不适用**。真正的闭环 = 「结算入账 → 提取 → hub 建 permanent Grant → 助手调用消耗」。

结论：**消费端无需新建**（Grant 机制现成），要建的只有 hubcenter 侧记账 + 提取桥。

### 12.2 身份验证（决策 4 的门）

| 能力 | 现状 | 位置 | 缺口 |
|---|---|---|---|
| 邮箱验证 | ✅ 已有 | `sm_users.verify_method = "email"` + `internal/mail`（SMTP） | 无 |
| `verified` 状态门槛 | ✅ 已有 | `sm_users.status`，`CreditsService` 里的 `u.Status != "verified"` → `ErrUnverifiedAccount` | 复用同款判据做「分享前门槛」即可 |

分享接口只需加一句：`sm_users.status != "verified"` → 拒绝分享并引导走邮箱验证。
手机验证码通道（hubcenter 全仓 0 命中）**不需要建**——不提现就不需要它。

---

## 13. 主要风险

| 风险 | 影响 | 缓解 |
|---|---|---|
| **计费倒挂**（服务组卖价 < 分享者应得） | 平台亏钱 | **v3 曾误判为"已消除"，v5 再修正**：v2 方案下两侧同源（都含 tier 倍率），本不会倒挂；按 §3.4-A1 修订后两侧才真正独立，倒挂**才真的存在**。靠 §5 ⑥ `net = max(0, min(net, charged))` 兜底（平台最多毛利 0），并靠 P2 #20 毛利透视图持续监控 |
| **档位倍率渗进消费者实付**（v5 新增，A1） | 消费者被加价，违反决策 6 | 阵列 `CreditMultiplier` 恒 1.0；档位倍率独立存 `token_bank_models.tier_multiplier`，只在结算侧使用 |
| **阵列下发清空成员 TokenPricing**（v5 新增，A2） | 消费者侧拿不到价，走兜底分支 | 改 `copyArrayBillingToProvider`：阵列仅在自己确有定价时才覆盖成员 |
| ~~HA 回放丢 bucket~~（B1） | ⚪ **已失效** | §14 已回滚，不再给 `sm_credits_transactions` 加列（§14.8） |
| **冻结额度被 Debit 穿透**（v5 新增，C1） | 积分超发，资损 | 冻结落成真实流水（`frozen` 桶），或 12 处 Debit 路径统一改用可用余额 |
| **`charged = 0` 导致分享者颗粒无收**（v5 新增） | 客诉"模型被调用却没积分" | 结算记 `net_clamped=1`，由 P1 #10 收益曲线暴露 |
| ~~积分提现的法币合规 / 打款通道~~ | ~~支付牌照、税务、反洗钱~~ | **v4 已消除**：不做提现（§12），Credits 只在站内流转 |
| 定价表填错导致单价离谱 | 分享者收益异常、消费者投诉 | 定价表改动留审计日志；提供「按官方价试算」的**只读参考**（不进计算）；异常单价告警 |
| Key 被平台侧泄露 | 信任崩塌、法律 | 双层加密 + 明文零出参 + secretbox 清零 |
| 服务商 ToS 禁止 key 共享 | 封号/法律 | 黑名单 + 用户声明 |
| 盗刷导致分享者巨额账单 | 用户流失 | 日/月熔断 + 异常告警（P1 #7 #8） |
| 结算丢失/重复 | 对账纠纷 | request_id 唯一键幂等 |
| 分享者取出后明细变孤儿 | 用户看不到历史赚了多少 | `token_bank_usage` 冗余 `owner_user_id` + `share_display_name` 快照（§4） |
| **分享链接被领两次**（HA 并发） | 超发积分，资损 | 领取路由到 `origin_node_id` 裁决 + 条件 UPDATE `RowsAffected==1`（§8 第 9 条）。**必须写并发测试** |
| 用户连开多条链接绕过 50% | 超发 | 未领取部分计入"冻结中"，从可用额度里扣除（§3.6） |
| 链接被截获/撞库 | 积分被盗领 | code 32 字节 CSPRNG；preview/claim 接口限流；链接 7 天过期；发送方可撤销 |
| 积分余额跨节点并发更新丢更新（既有，购买钱包） | 余额错乱 | **已确认与 Token 银行无关**（积分不落 `sm_users`），已回滚；若要修按 §14.8 单独立项。Token 银行侧改由 §14.6 定期对账兜底 |
| 浮点累加漂移（v6 新增） | 长期小额累加后账面对不上 | 已拍板接受 `float64`；统一 `roundCredits`（4 位）+ 三处对账 + 差异走 `adjustment` 流水，不静默改数（§14.6） |
| `llm-service-tab.js` 已 4266 行 | 继续膨胀难维护 | Token 银行管理必须新开 JS 文件 |
| **成员被手工删除后 `member_id` 悬空**（v5 新增） | share 还在、模型不调度，无声故障 | 启动自检：registry 里找不到 `member_id` 就把该模型标 `available=0` 并告警；admin 下钻页显示"成员缺失" |
| ~~**密钥随 HA 同步扩散到全节点**~~（v5 新增，§8 第 6 条） | ⚪ **v8 改判：不成立** | `token_bank_shares` **从未进过** HA 同步（Token Bank 只有 `token_bank_ledger_batch` 一个实体，见 §18），密文不扩散。<br>**但换来两个新问题**：① `llm_provider` 也不同步 ⇒ `tbk_` 成员单节点可见，该节点宕机则这批共享模型整体下线（无容灾）；② admin 的暂停/改档/取出落到别的节点 = 操作另一份数据 ⇒ "改了没生效"。见 §18 |
| **自用豁免**（§9 #5）✅ | 分享者调用自己的服务商会产生空转积分 | 已用 `hub_user_links.email = sm_users.email` 判断（`HubBelongsToUser`）。命中则跳过结算；查询出错仍结算；缺表视为非本人 |
| ~~**`token_bank_accounts` 跨节点丢更新**~~（v7 新增，E1） | ✅ **已解决**（append-only 流水 + `SUM` 为余额），但**不等于并发安全**，见下一条 F1 | 账本 `token_bank_ledger` 已落地，`token_bank_accounts` 降级为物化缓存；`TestTokenBankConcurrentSettlementKeepsExactSum` / `TestSettleTokenBankUsageConcurrentIsExactlyOnce` 全绿（**均为单进程**） |
| ~~**账本 REAL 与 usage micro 精度不一致**~~（v7 新增，E2） | ✅ **已解决** | 账本与流水一律 `INTEGER micro`，只在生成 Grant 的最后一跳 `micro / 1e6` 转 float64 |
| **多 hub 提取归属未定义**（v7 新增，E6） | ✅ 主路径已解决，但**两条口子还在** | `withdrawals` 记 `hub_id`/`grant_id`；自动提取按 1/N 均摊；`request_id` 重放做「重新下发」。<br>🔴 **口子 1（R7）**：`manual` 由请求体自报，自托管 hub 传 `true` 即可一次提空 ⇒ 1/N 形同虚设。<br>🔴 **口子 2（§18）**：`token_bank_withdrawals` **不同步**，换节点重放查不到原记录 ⇒ 判为"没提过" ⇒ 重复扣账 + 重复建 grant |
| **Grant 的 ServiceGroupID 填错**（v7 新增，E5） | grant 建了但用不掉（被 `FindModelServiceGroup` 静默跳过） | ✅ 已落地：hub 侧 `token_bank_grant.go` 与 hubcenter 侧 `token_bank_hub_withdraw.go` **两端各校验一次**；后者在服务组内无 tbk_ 成员时返回 400 `service_group_not_token_bank` |
| 🔴 **跨节点提取超发**（v8 新增，**F1**） | 同一笔余额被两台 hub 各提走一次 ⇒ 资损，且**日常场景**（自托管多机）即可触发 | 账本异步复制（200 条/15s）+ 提取读本地余额 ⇒ flush 窗口内两节点各自放行。**待拍板**，三条改法见 §14.9；验收必须是跨节点测试 |
| 🔴 **价目表跨节点不一致**（v8 新增） | admin 改价只改了一个节点 ⇒ 同一个模型在不同节点结算出**不同的收益**，`Σ usage` 与账本无法精确对账 | `token_bank_price_book` 不在 HA 同步内。要么给它做同步（它是低频小表），要么规定"改价必须打到每个节点"，见 §18 |
| 🟡 **`RebuildAccount` 无生产调用点**（v8 新增，R6）— ✅ **已修复** | 缓存漂移后**不会自愈也不会告警**；后果是 **GUI / admin 展示错误余额**（用户看到错的可用积分），**不是资损**——因为提取决策读的是 ledger（`token_bank_withdraw.go:156`），不读缓存 | 修复：`FindDriftedAccounts`（一次 SQL 比对 cache 与 ledger，只返回不一致的账号）+ `runTokenBankCacheReconcile`（启动一遍 + 每 30 分钟一遍，逐条打日志）。漂移量上升 = 复制有问题的信号，不是对账任务忙 |
| 🟠 **`manual` 自报绕过 E6**（v8 新增，R7） | 任一自托管 hub 传 `manual=true` 一次提空 ⇒ 1/N 均摊形同虚设 | 见 §14.5 的补记。推荐：`manual` 提取要求用户会话凭据，hub 服务凭据只允许受限模式 |

---

## 14. 积分账本与提取：hubcenter 记账，hub 消费

> v5 重写（2026-10-01）。**旧版 §14「把 hubcenter 的余额改成流水汇总」已废弃并回滚**——
> 那条路线建立在错误前提上：它改的是 SkillMarket 的**购买钱包**，而 Token 银行积分根本不落那里。

### 14.1 两套 credits 必须分开（代码核实结论）

| | SkillMarket credits | Hub 服务 credits |
|---|---|---|
| 载体 | hubcenter `sm_users.credits` | hub `llmservice.Grant` |
| 来源 | **用户充值购买**（等价于钱） | 服务兑换 / 算力卡 / 邀请码 / 推荐 |
| 用途 | 买 skill / 宠物包 / AI 专家 | **agent 助手消耗 LLM** |
| 字段 | `credits` 单列 | `CreditsTotal` / `CreditsUsed` / `Permanent` / `UsageEvents` |
| Token 银行 | ❌ 无关 | ✅ **积分落这里** |

用户拍板：两者**不混用**。所以在 hubcenter 里给 Token 银行单独记账，再让 hub 单向提取。

### 14.2 架构：hubcenter 只记账，hub 单向 pull

```
[消费者调用 tbk_ 服务商]  ← 服务商共享池在 hubcenter，流量与结算都在 hubcenter
    → token_bank_usage 记一行明细（单价 × 倍率 × 手续费，formula_json 留痕）
    → token_bank_accounts.earned_credits += N
    → 积分停留在 hubcenter，此时还不是 agent 助手能花的额度

[分享者要用积分]
    → hub 本地 agent 助手余额低于阈值（或手动点「提取」）
    → hub 主动 pull：POST /api/v1/token-bank/credits/withdraw  {request_id, amount}
    → hubcenter 扣除 earned / withdrawn 记账
    → hub 生成 permanent Grant（Source="token_bank"）→ 变成可用额度
```

**为什么是单向 pull 而不是 hubcenter 推送**：hub 常在 NAT / 内网后面，推送要处理不可达重试、
幂等回执、乱序；pull 由消费端发起，天然幂等（request_id），失败重试即可。

### 14.3 hubcenter 侧账户表

> **2026-10-01 已拍板**：金额一律 `INTEGER micro`（E2）；账本真相源 = append-only 流水（E1）。
> 下面就是**最终版建表语句**，不是草案。

```sql
-- ① 真相源：append-only 流水（E1）。每行只增不改，HA 回放 INSERT OR REPLACE 天然幂等
CREATE TABLE IF NOT EXISTS token_bank_ledger (
  id             TEXT PRIMARY KEY,          -- 确定性 id（见下方幂等约定）
  user_id        TEXT NOT NULL,
  bucket         TEXT NOT NULL,             -- earned|received|withdrawn|granted|frozen
  amount_micro   INTEGER NOT NULL,          -- 带符号（E2：整数微积分，1 credit = 1e6）
  biz_key        TEXT NOT NULL DEFAULT '',  -- 业务幂等键，空表示不要求唯一
  ref_type       TEXT NOT NULL DEFAULT '',  -- usage|withdraw|share_link|adjustment
  ref_id         TEXT NOT NULL DEFAULT '',
  note           TEXT NOT NULL DEFAULT '',
  created_at     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_tbk_ledger_user_bucket ON token_bank_ledger(user_id, bucket);
CREATE UNIQUE INDEX IF NOT EXISTS idx_tbk_ledger_bizkey
  ON token_bank_ledger(biz_key) WHERE biz_key <> '';

-- ② 物化缓存：只是 SUM 的快照，错了可以 rebuild（E1）
CREATE TABLE IF NOT EXISTS token_bank_accounts (
  user_id            TEXT PRIMARY KEY,
  earned_micro       INTEGER NOT NULL DEFAULT 0,
  received_micro     INTEGER NOT NULL DEFAULT 0,
  withdrawn_micro    INTEGER NOT NULL DEFAULT 0,
  granted_micro      INTEGER NOT NULL DEFAULT 0,
  frozen_micro       INTEGER NOT NULL DEFAULT 0,
  updated_at         TEXT NOT NULL
);
-- 可用 available_micro = earned + received − withdrawn − granted − frozen

-- ③ 提取记录：幂等 + 多 hub 归属 + 重放凭据（E3/E6）
CREATE TABLE IF NOT EXISTS token_bank_withdrawals (
  id             TEXT PRIMARY KEY,
  request_id     TEXT NOT NULL,             -- hub 生成，重试必须带同一个
  user_id        TEXT NOT NULL,
  hub_id         TEXT NOT NULL DEFAULT '',  -- 哪个 hub 提走的
  amount_micro   INTEGER NOT NULL,
  grant_id       TEXT NOT NULL DEFAULT '',  -- hub 侧建出来的 grant，跨端对账与重放用
  kind           TEXT NOT NULL DEFAULT 'self', -- self|gift
  link_id        TEXT NOT NULL DEFAULT '',  -- kind=gift 时指向 credit_share_links.id
  status         TEXT NOT NULL DEFAULT 'issued', -- issued|reissued
  created_at     TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_tbk_withdraw_req     ON token_bank_withdrawals(request_id);
CREATE INDEX IF NOT EXISTS idx_tbk_withdraw_user_time      ON token_bank_withdrawals(user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_tbk_withdraw_hub            ON token_bank_withdrawals(hub_id);
```

**余额的唯一算法**（任何地方都不许自己加减）：

```sql
SELECT bucket, COALESCE(SUM(amount_micro), 0)
  FROM token_bank_ledger WHERE user_id = ? GROUP BY bucket;
```

**幂等约定（关键）**：`token_bank_ledger.id` 必须**确定性生成**，不能 `generateID()`——
否则 HA 三节点各插一条随机 id，回放后余额 ×3（与 §14.8 里 opening 种子同一个坑）。

```
结算入账   → biz_key = tokenBankUsageBizKey(request_id, share_id, model)
             id      = 由该 biz_key 哈希派生（**必须含 model**，D17）
提取       → biz_key = "withdraw:<request_id>"        id = "wd_"   + sha256(requestID)[:16]
转赠冻结   → biz_key = "freeze:<link_id>"             id = "frz_"  + sha256(linkID)[:16]
转赠解冻   → biz_key = "unfreeze:<link_id>"           id = "unfrz_"+ sha256(linkID)[:16]
转赠过账   → biz_key = "gift:<link_id>"               id = 分离的两行：
                       发送方 "grnt_"+hash（granted += N）
                       接收方 "rcv_" +hash（received += N）
```

> 🟠 **v8 修正（R5）：解冻 id 里没有 seq，也不该有。**
> v7 写的是 `unfreeze:<link_id>:<seq>`（"可能多次，不加唯一键"），**与代码不符且更弱**：
> 实际实现是 `giftLedgerID("unfrz", "unfreeze:"+linkID)`——对 link 的**纯函数**，无 seq、
> 无时间戳、无计数器。因为它是确定性的，重复解冻直接被主键 `INSERT OR IGNORE` 挡住，
> 根本不需要 seq；而"加 seq"反而会打破本节的头号约束（三节点必须算出同一个值），
> 让并发的撤销 + 过期各插一行 ⇒ `frozen` 变负 ⇒ 可用额度虚高。
> 另外 `releaseGiftFreeze` 已实现 **capped at the sender's**（解冻量不超过该发送方实际冻结量），
> C2「解冻时余额可能已不足」在实现层已有兜底，比文档描述的更强。
>
> 🔴 **D17 提醒**：`biz_key` 必须含 `model`。`token_bank_ledger` 除主键外还有
> `idx_tbk_ledger_bizkey = UNIQUE(biz_key) WHERE biz_key <> ''`，而 `INSERT OR IGNORE`
> 在有多个唯一约束时会被**任一**约束忽略——"没插进去"不能默认为"主键冲突"。
> 一次请求命中同一 share 的两个模型时，缺 model 会让第二笔被静默吞掉（少付钱且无报错）。

**rebuild**（缓存错了能自愈，这是 E1 的全部价值）：

```sql
UPDATE token_bank_accounts SET
  earned_micro    = (SELECT COALESCE(SUM(amount_micro),0) FROM token_bank_ledger WHERE user_id=? AND bucket='earned'),
  received_micro  = (SELECT COALESCE(SUM(amount_micro),0) FROM token_bank_ledger WHERE user_id=? AND bucket='received'),
  withdrawn_micro = (SELECT COALESCE(SUM(amount_micro),0) FROM token_bank_ledger WHERE user_id=? AND bucket='withdrawn'),
  granted_micro   = (SELECT COALESCE(SUM(amount_micro),0) FROM token_bank_ledger WHERE user_id=? AND bucket='granted'),
  frozen_micro    = (SELECT COALESCE(SUM(amount_micro),0) FROM token_bank_ledger WHERE user_id=? AND bucket='frozen')
WHERE user_id = ?;
```

GUI 顶部卡片显示：总获得 / 已提取 / 冻结中 / **可用**（都用 `micro / 1e6` 展示）。

#### ✅ E2 已拍板：整数微积分，只在最后一跳转 float

```
token_bank_usage（*_micro INTEGER）
      ↓ 同一笔数，不换算
token_bank_ledger / accounts（INTEGER micro）   ← hubcenter 内部全程整数，零漂移
      ↓ 只在生成 Grant 的最后一跳：float64(amount_micro) / 1e6
Grant.CreditsTotal（float64）                   ← 漂移只发生在这一跳
```

于是 `Σ usage.net_micro == SUM(ledger.earned)` 是**整数精确相等**的对账，
不再是"差值小于阈值"。决策 13「Grant 保持 float64」不被推翻。

### 14.4 hub 侧：permanent Grant

已核实 `hub/internal/llmservice/` 里 grant 创建就是往 `reg.Grants` 追加，
`Source` 现有取值 `card` / `new_user_limit_card` / `invitation_code` / `user_referral`
（service.go:884 / :1663 / :2113 / :2153）。Token 银行并列加一个即可：

```go
reg.Grants = append(reg.Grants, Grant{
    ID: NewID("grant"), UserID: owner.UserID, Email: owner.Email,
    ServiceGroupID: req.ServiceGroupID,        // 由 hub 显式传入，两端都要校验存在性
    Source:         "token_bank",
    Permanent:      true,                      // 终身有效，不设 ExpiresAt
    CreditsTotal:   float64(amountMicro) / float64(llmpool.MicrocreditsPerCredit), // 唯一的 float 转换点（E2）
    StartsAt: now, CreatedAt: now,
})
```

> **已拍板（2026-10-01）：不新建服务组。** `ServiceGroupID` 由 **hubcenter 管理员把
> `token_bank_*` 阵列分配进哪个服务组**决定——分配的结果就体现在
> `ServiceGroup.Models[].ProviderConfigs[].ProviderID` 里出现了 `tbk_` 成员。
> 核实结论：`ServiceGroup` 没有指向阵列的字段（`corelib/llmpool/types.go:120-132`），
> 关联就是靠路由表里的 provider id，所以"阵列 → 服务组"是管理员在**服务组侧**配出来的。

**提取时服务组 id 怎么定**（三重校验，缺一不可）：

```
1. hub 发起提取时显式带 service_group_id（hub 知道自己要补哪个组的额度）
2. hubcenter 校验：该服务组内确实存在 tbk_ 成员
   （即管理员已把 token bank 阵列分配进这个组）——没有则 400，不许乱填
3. hub 侧建 grant 前校验 reg.FindModelServiceGroup(sgID) != nil
   （hub/internal/llmservice/service.go:858 的模式，缺失会被静默跳过）
```

> 一个用户的多个模型可能分散在多个服务组，所以**一次提取只针对一个服务组**，
> 不做跨组分摊——分摊会让"哪笔积分进了哪组"变成对不上的账。

> ⚠️ 「服务兑换」得到的 grant 是带 `ExpiresAt` 的（GUI 里 `CreditsRemaining` 旁就显示过期时间）。
> 只有 `Permanent: true` 才是无期——Token 银行发的必须是 permanent，且 `Source` 要能一眼区分。

#### 🟠 E5（v7）：`ServiceGroupID` 不能挂 `token_bank_low/mid/high`

原注释写的 `token_bank_low/mid/high` 是 **`ProviderArray`（服务商阵列）** 的 id，
而 `Grant.ServiceGroupID` 要的是 **`ServiceGroup`（服务组）**——两者是完全不同的东西：

- 阵列 = 一组上游服务商（供给侧，`hubcenter/internal/llmservice/provider_array.go`）
- 服务组 = 用户可订阅、绑定模型与额度的计费单元（`reg.FindModelServiceGroup`）

代码里兑换卡时就**显式校验**了服务组存在性（`hub/internal/llmservice/service.go:858`：
`if reg.FindModelServiceGroup(serviceGroupID) == nil { continue }`），填一个不存在的 id，
grant 会被静默跳过或直接无效。

**已拍板（与上面一致）**：不新建服务组，由管理员把 `token_bank_*` 阵列分配进既有服务组；
提取时 hub 显式指定 `service_group_id`，两端各校验一次存在性。
档位**不通过**服务组表达（那是 §3.4-A1 已否决的老路）。

**验收判据（写成单测）**：
① hubcenter：服务组内无 `tbk_` 成员时提取返回 400；
② hub：`reg.FindModelServiceGroup(sgID) == nil` 时**报错而不是静默跳过**
（兑换逻辑 `hub/internal/llmservice/service.go:858` 的默认行为是 `continue`，
必须显式改成报错，否则"提了积分却没到账"会变成最难查的静默故障）。

### 14.5 提取的触发与幂等

| 触发 | 时机 | 备注 |
|---|---|---|
| **自动**（**默认开**） | hub 本地可用额度 < 阈值（阈值可配） | **已拍板：默认开**，但受下面的"平均分配"约束，避免一台机器抽干（E6） |
| 手动 | GUI Token 银行 tab「提取到本机」 | 用户显式操作，**可提走全部 available**，不受平均约束 |
| 转赠 | 接收方领取后首次提取 | 见 §14.7 |

**多 hub 平均分配（已拍板）**：hub 是自托管的，一个用户可能有多台。
自动提取的额度按**该用户名下的 hub 数**平均切分：

```
N         = 该用户名下已注册的 hub 数（查 hubcenter 的 hub 注册表；未知时按 1）
单次上限   = floor(available_micro / max(N, 1))
```

- 一台机器一次最多拿走 1/N，剩下的留给别的机器，**不会出现"先 pull 的抽干"**；
- 手动提取是用户显式意图，允许一次提完（不受此限，但要二次确认）；
- `N` 随 hub 注册/下线变化，不必强一致——平均只是防独占，不是精确配额。
  （`N` 依赖的 `hub_user_links` **在** HA 同步内，所以各节点算出的 N 一致。）
- 🟡 **几何衰减，不是真配额**（D8，§17.5）：公式每次都拿**当时剩余**去除，
  于是"先问的拿一半，后问的拿剩下的一半"。当前按拍板公式实现，留作观察项。

#### 🟠 v8 补记：hub 侧的实现比本文档更严谨，这里吸收进来（R3）

| 本文档的说法 | 实际实现 | 评价 |
|---|---|---|
| 「`request_id` 由 hub 生成，重试必须带同一个」 | `TokenBankAutoRequestID(hubID, email, groupID, seq)` —— **确定性**：由 hub id + 用户 + 服务组 + 单调序号（`TokenBankAutoSeq` / `AdvanceTokenBankAutoSeq`）算出 | 比文档强。它顺带解决了"超时重试到底该不该换 request_id"：正常重试带同 seq（幂等命中），**新周期**才换 seq（允许再提一次）。文档应改为这个口径 |
| 「hub 本地可用额度低于阈值」 | `TokenBankRemainingMicro(reg, email, groupID, now)` —— 按**服务组**算剩余额度，再与 `cfg.ThresholdMicro` 比 | 比文档强。§14.4 已拍板"一次提取只针对一个服务组"，阈值判断理应同粒度；文档原话没说是哪个余额 |
| 手动 / 自动的区分 | `RunTokenBankAutoOnce` 默认开的后台路径；GUI 走 `manual=true` | 见下方 R7 |

**🟠 R7（待拍板）：`manual` 由调用方自报 ⇒ E6 的 1/N 可以被一句话绕过。**
落地实现里 `Manual` 是从请求体读的布尔（`token_bank_handlers.go:170`、
`token_bank_hub_withdraw.go:127` 都是 `Manual: req.Manual`）。而 §17.6-D12 自己已经点破：
「HubCenter 无法从请求本身区分'人在点按钮'和'cron 在跑'」。
既然无法区分，而 hub 是自托管的，那么任一 hub 只要传 `manual=true` 就能一次提走全部积分——
**正是 E6 要消灭的"一台机器抽干"**。

建议（择一，推荐第 1 条）：
1. **`manual` 提取要求用户会话凭据**；hub 服务凭据只允许受限模式（自动路径本来就是机器在调，不需要 manual）；
2. hub 专用端点**强制** `Manual=false`；
3. 给 manual 提取加日累计上限（如单日 ≤ `available/N × 2`）。

幂等靠 `request_id`：`token_bank_withdrawals.request_id` 唯一键 + `token_bank_ledger` 的
`biz_key` 唯一键，双保险；重复提交返回首次结果，绝不重复扣账。

**重新下发（E6）**：hub 重装 / 迁移后 grant 丢失，可凭 `token_bank_withdrawals` 里
`status='issued'` 且 hub 侧确认不存在的记录，用**同一个 `request_id`** 重新请求，
服务端回放首次结果（含 `amount_micro`），hub 重建 grant 后回报 `grant_id`，
记录置 `status='reissued'`。**不产生第二笔扣账**。

### 14.6 精度与对账（已拍板：整数 micro + 单跳浮点）

hubcenter 侧**全程整数**（E2），只有生成 Grant 的最后一跳转 `float64`（§14.4）。
Grant 本身仍是 `float64`，所以跨端对账仍需要，但阈值是**微积分级别**的：

- hubcenter 内部：`Σ token_bank_usage.net_micro` vs `SUM(ledger WHERE bucket='earned')`
  → **整数精确相等**，不为 0 就是 bug
- hubcenter 缓存：`SUM(ledger)` vs `token_bank_accounts.*` → 不等就触发 rebuild（§14.3）
- 跨端：`hubcenter.withdrawn_micro` vs `hub 已建 grant 的 CreditsTotal × 1e6`
  → 允许 **≤ 1 micro** 的舍入差（单跳 float 转换），超过就告警
- 差异修正走一条 `bucket` 相同的 `adjustment` 流水，**不静默改数**

展示统一 `micro / 1e6` 后按 `roundCredits`（4 位）渲染，尾差不进账本。

#### ✅ E1（v7 提出，v8 标记已拍板并落地）：账本丢更新

> 🟡 **v8 结构说明（R11）**：这一条**不属于**「精度与对账」，它是并发一致性问题，
> 只是 v7 写在了 §14.6 下面。内容保留在此，但请注意它与本节标题无关。

这是 §14.8 里那个"购买钱包 last-write-wins"的**同款病**，换个表又得了一遍——
而且这次更严重，因为**结算入账是每调用一次写一次**，不是偶发写入。

```
节点 A：结算 → 读 accounts(earned=100) → 写 earned=100.5
节点 B：结算 → 读 accounts(earned=100) → 写 earned=100.7      ← 并发，各自本地成功
HA 同步：两边各写一条 op，按 ha_entity_versions 版本比较 → 后到的覆盖先到的
结果：earned = 100.7，A 那 0.5 **凭空消失**
```

对账救不了这个——它只能事后发现"少了"，而丢失是持续发生的。

**✅ 已拍板并落地（§16.5 第 1 条 / §17.1）：采用第 1 条。** 其余两条仅作背景保留：

1. ✅ **账本改 append-only 流水**（`token_bank_ledger`，每行带 `biz_key` 唯一），
   余额 = `SUM`，与 `token_bank_usage` 同性质，HA 回放 `INSERT OR IGNORE` 天然幂等。
   `token_bank_accounts` 降级为物化缓存，可用 `RebuildAccount` 重建。
2. **最低限度**（未采用）：所有入账改成 SQL 原子增量 `SET earned_credits = earned_credits + ?`，
   杜绝"读-改-写"。能消除单节点内丢失，但 HA 同步仍是整行覆盖，**跨节点仍会丢**。
3. **收敛写入点**（未采用）：结算只允许路由到"该 share 的归属节点"执行（与 §3.6 领取走
   `origin_node_id` 同一思路）。改动小，但把调度灵活性也一起锁死了。

> **并发测试是硬门槛**（已达成）：`TestTokenBankConcurrentSettlementKeepsExactSum`、
> `TestSettleTokenBankUsageConcurrentIsExactlyOnce`（24 调用 × 4 并发 → 恰好 24 usage / 24 ledger）。
>
> 🔴 **但这两个测试都是单进程的**（同一 SQLite、同一事务管理器）。它们证明的是
> "唯一约束与事务能挡住重放"，**证明不了跨节点下的并发安全**——
> 账本是**异步**复制的（满 200 条或 15s），两节点在窗口内各自放行一笔**不同的**写入，
> 仍然会超发。这是 v8 新发现的 **F1，见 §14.9**。
> 一句话区分：**E1 的解法保证"同一行重放不重复"（幂等），不保证"两个节点各自放行不冲突"（串行化）。**

### 14.7 转赠（积分分享）也走提取通道

用户拍板：**分享的积分同样从 hubcenter 提取，再进入目标用户的 hub 账户**。
过账时机是这一节的关键：

```
① 发送方创建链接
   → hubcenter: frozen_micro += N        ← 预扣（冻结），earned 不动

② 接收方打开链接 → 「xxx 分享给你 N 积分」→ [领取]
   → 原子 claim：条件 UPDATE 绑定 claimed_by，只有第一个人成功
   → 此时仍然只是冻结，发送方没有被实际扣减

③ 接收方把积分提取到自己的 hub            ← 实际过账在这一步
   → hubcenter: frozen_micro −= N; granted_micro += N（发送方）
                received_micro += N（接收方）
   → hub: 生成 permanent Grant(Source="token_bank")

④ 未提取：发送方可撤销；7 天到期自动解冻退回
```

**为什么不在第 ② 步就扣**：接收方领了却一直不提取，发送方的积分就被永久占住。
放到第 ③ 步扣，配合 7 天超时退回，占住时间有上界。
同时也消灭了「hubcenter 扣了但 hub 没收到」的悬空状态——扣与建 grant 在同一次提取事务里。

### 14.8 已回滚的工作（留档，别重复踩）

hubcenter `internal/skillmarket` 的「余额改流水汇总」改造（`bucket` / `biz_key` /
opening 种子 / `RebuildUserBalance` / `CreditSpendable`）已于 2026-10-01 **全部回滚**，
代码回到 HEAD。原因是它与 Token 银行无关——那是购买钱包的账务加固。

若将来要修（它确实是既有风险：跨节点并发改余额会 last-write-wins），单独立项，注意：

- 真实写入点是 **12 处**直接 UPDATE 余额列 + `Credit`/`Withdraw`/`SettlePending`/`RecordPlatformFee`
  四个方法的桶语义，不是文档早期写的 8 处
- `ha_sync.go:325` 的回放 SQL **硬编码 9 列**，加列必须同批次改，否则回放丢列
- `SettlePending` 不写流水、`Credit` 的 debt 抵扣无对应流水，改造后 `SUM ≠ 余额`
- opening 种子若用随机 id，会在三节点各插一份 → 余额 ×3（必须用确定性 id）

### 14.9 🔴 F1（v8 新发现，待拍板）：异步复制窗口内跨节点提取可超发

> 这是 v8 review 里**唯一一条"设计已定稿、代码已落地、钱仍然会算错"的问题**。
> 前 7 版都没有识别它，因为 E1 的解决过程把"幂等"当成了"并发安全"。

#### 现象

账本的 HA 复制是**异步批量**的：`tokenBankLedgerSyncBuffer` 满 200 条或 15s 才 flush（§17.3）；
而提取的额度校验是在事务内 `ledgerBalance(ctx, tx, userID)` —— **本地账本的 `SUM`**。
于是存在一个最长约 15s（或 199 条积压）的窗口，两个节点各自看到"余额还是满的"：

> 🔍 **v8 精确化（重要，别修错地方）**：提取读的**已经是权威源**——
> `AvailableMicro()` 与 `Withdraw` 都走 `ledgerBalance`（`token_bank_withdraw.go:156/414`），
> **从不读** `token_bank_accounts` 缓存（该缓存只服务于 GUI / admin 展示）。
> 所以滞后**不是**"读了脏缓存"造成的，**改成读 ledger 也解决不了**——
> 本地 ledger 本身就是一份滞后至多 15s 的副本。
> 这条排除了一个很自然的错误修法（"把 Available 改成读 SUM 就行"），它已经在读 SUM 了。

```
节点 A：hub1 提取全部 100 → 本地账本 withdrawn +100 → 进缓冲，尚未 flush
（3 秒后）
节点 B：hub2 提取全部 100 → 本地还没有那一行 → available 仍是 100 → 放行，withdrawn +100
同步完成后：两笔都成立 → available = −100 → 超发 100
```

E6（多 hub 平均分配）**防不住**：它只约束"顺序提取"，不约束"跨节点窗口"。
而 hub 是**自托管**的（家里 / 公司 / 重装各一台，`hub/README.md`），
这不是理论攻击，是日常场景。转赠过账（§14.7 第 ③ 步）也走同一条提取通道，同样敞口。

#### 为什么现有测试没抓到

- `TestSettleTokenBankUsageConcurrentIsExactlyOnce`（24 × 4 并发）是**单进程**并发，
  覆盖 SQLite 事务与唯一约束，**不涉及跨节点**；
- `TestTokenBankConcurrentSettlementKeepsExactSum` 同理；
- §17.3 只论证了"同一行被两路各发一次会不会重复入账"（不会，靠行级 `INSERT OR IGNORE`），
  没有论证"两个节点各自**放行**一笔不同的写入"。

#### 关键区分（写在这里防止再犯）

| | 保证什么 | 靠什么 |
|---|---|---|
| **幂等**（E1 已解决） | 同一行重放不重复入账 | 确定性主键 + `INSERT OR IGNORE` |
| **串行化**（F1 未解决） | 两个节点不会各自放行冲突的写入 | ❌ 当前**没有任何机制** |

append-only 流水天然给出前者，**不给出后者**。

#### 三条改法（推荐第 1 条）

1. **提取也路由到裁决节点**（推荐）。仿 §8 第 9 条给 claim 的做法：为账本/用户定一个
   归属节点，**所有写账本的操作**（结算除外——结算天然发生在 share 所在节点）
   路由到该节点本地事务执行；不可达时明确失败，**不允许代理节点本地代提**。
   语义最干净，与既有 claim 实现同构。
2. **乐观锁前置校验**。提取事务里带上"我看到的 ledger 最大 id / 行数"，
   对端若已领先则拒绝，让调用方重读重试。改动小，但要在事务里引入跨节点读取。
3. **接受窗口 + 强制对账止血**。保留现状，把 §14.6 的对账做成定时且强制告警，
   并在提取侧加"单用户单日提取总额上限"。最弱，仅作过渡。

> ⚠️ 无论选哪条，**验收判据必须是跨节点测试**：两节点、人为拉长 flush 间隔、
> 并发提取同一用户，断言 `withdrawn ≤ earned + received − granted − frozen`。
> 现有的单进程并发测试**不足以**证明它。

**状态**：🔴 **待拍板**。它决定要不要改提取的执行位置，越晚改动面越大。

---

## 15. v5 代码核实清单（review 证据）

2026-10-01 逐条读代码核对 v4 正文的产出。**核实为真**的引用不再列举，下面只列**与文档不符**或
**文档没写但必须做**的项。文件路径省略前缀 `hubcenter/` 与 `corelib/`。

### 15.1 已核实为真的引用 ✅

| 文档说法 | 证据 |
|---|---|
| P0-0 阵列底座已落地 | `llmservice/token_bank_arrays.go` 全文；`provider_array.go:144 dropEmptyProviderArrays`、`:728 DeleteProviderArray` + `:740 ErrArrayProtected`；`token_bank_arrays_test.go:110` 有删除保护用例 |
| `DropEmpty` 跳过系统阵列 | `provider_array.go:220` |
| 硬编码手续费 `PetStorePlatformFeePct = 30` | `skillmarket/credits_service.go:25` |
| `MicrocreditsPerCredit = 1_000_000` | `llmpool/token_pricing.go:27` |
| 档位倍率 0.5 / 1 / 2 | `llmpool/capability_billing.go:37`（`CanonicalClientModel` 能正确吃掉 `official-low` 等常量，传 tier 常量安全） |
| `sm_credits_transactions` 在 HA 同步表内 | `skillmarket/ha_sync.go:207-209` |
| `EnsureLLMTables` 存在 | `store/sqlite/llm_repo.go:21` |

### 15.2 与文档不符 / 文档遗漏 ❌

| # | 级别 | 结论 | 证据 | 落到 |
|---|---|---|---|---|
| A1 | 🔴 致命 | 档位倍率会被乘进**消费者实付**，推翻决策 6 | `provider_array.go:358 → proxy.go:897/984/991`、`llmpool/credit_multiplier.go:28` | §3.4-A1、§11 决策 6 |
| A2 | 🟠 高 | 阵列下发无条件覆盖成员 `TokenPricing`，消费者侧拿不到价 | `provider_array.go:365`、`llmpool/token_pricing.go:367/389` | §3.4-A2 |
| A3 | 🟡 中 | `EnsureTokenBankArrays` 每次启动强制改回倍率，且不清理分时窗 | `token_bank_arrays.go:120-123`、`credit_multiplier.go:36-39` | §3.4-A3 |
| B1 | ⚪ 已失效 | ~~HA 回放 SQL 硬编码 9 列，加 `bucket` 丢桶~~ → §14 已回滚，不再加列 | `skillmarket/ha_sync.go:325` | §14.8 |
| B2 | ⚪ 已失效 | ~~`SettlePending` 不写流水~~ → 属于购买钱包改造，随 §14 回滚 | `credits_service.go:376-402` | §14.8 |
| B3 | ⚪ 已失效 | ~~`Credit` 的 debt 抵扣无对应流水~~ → 同上，随 §14 回滚 | `credits_service.go:152-180` | §14.8 |
| B4 | ⚪ 已失效 | ~~直接 UPDATE 余额列实为 12 处~~ → 随 §14 回滚，计数留档备用 | `credits_service.go` 等 | §14.8 |
| B5 | ⚪ 已失效 | ~~opening 种子随机 id 三节点各插一份~~ → 随 §14 回滚 | `ha_sync.go:325` | §14.8 |
| C1 | 🔴 高 | 冻结额度会被**扣账入口**穿透（v5 是 `Debit`，v6 换成**提取接口**，漏洞换个入口还在） | `credits_service.go:120`；§14.2 提取接口 | §3.6-C1（v7 已按新架构重写） |
| C2 | 🟡 中 | 解冻时发送方余额可能已不足，行为未定义 | — | §3.6-C2 |
| D1 | 🟡 中 | §6.3 允许移到任意阵列，非 TB 阵列时 `tier` 无定义 | — | §4（`tier='custom'`） |
| D2 | 🟡 中 | registry 成员被手工删除 → `member_id` 悬空，无声故障 | — | §13 |
| D3 | 🟡 中 | §3.1 的 24h 幂等键**没有表承载** | — | §4 `token_bank_share_requests` |
| D4 | 🟠 高 | §8 说 TB 数据走 HA 同步 = 把 `encrypted_api_key` 复制到所有节点 | `ha_sync.go:207` | §8、§13 |
| D5 | 🟡 中 | P0 #5 自用豁免缺 `consumer → owner` 归属映射，技术上无法识别 | — | §13 |
| ~~D6~~ | ⚪ 已失效 | ~~「可用 = 总积分 − 已消耗」在钱包混入 TopUp/退款后算错~~ → 购买钱包口径，随 §14.8 回滚；Token 银行可用额走 §14.3 五桶公式 | `credits_service.go:328`、`refund_service.go` | §5 |

### 15.3 下一步要先做的三件事

1. **拍板 A1**：确认档位倍率改走独立字段（阵列倍率恒 1.0）。这不只影响 Token Bank，
   还要求改 `copyArrayBillingToProvider` 的覆盖语义（A2）——建议与 P0-0 的后续改动合并提交。
2. ~~**§14 账务改造单独立项**~~ → **已决定不做**（与 Token 银行无关，代码已回滚）。
   购买钱包的并发丢更新是既有风险，若要修按 §14.8 单独立项。
3. **§14 新架构先落地账本与提取**：`token_bank_accounts` 表 → 结算入账 → 提取接口
   （幂等 `request_id`）→ hub 侧 `Source='token_bank'` permanent grant → 自动提取触发。
4. **§3.6 冻结方案先定再写码**：C1（Debit 看不见冻结）在**新账本**里同样成立——
   提取接口必须校验 `available`（含 frozen），不能只读 `earned`。
   （v7 已把 §3.6-C1 正文从 `Debit` 改写为**提取接口**口径。）

---

## 16. v7 review：v6 换架构后的遗留与新账本问题

v6 把积分落点从 hubcenter 改成 hub 的 Grant，方向正确（两套 credits 确实不该混）。
但换架构时留下一批 v5 正文没跟着改，新账本本身也引入两个新问题。

### 16.1 v5 遗留、v7 已对齐

| 位置 | v6 时的状态 | v7 处理 |
|---|---|---|
| §3.3 结算 | 还写 `CreditSpendable(...)` 入「买家钱包 credits，见 §14.7 方案 A」——该方法已回滚，§14.7 也换了内容 | 改为入 `token_bank_accounts` |
| §5 账户余额表 | 还写"可用 = 取钱包 `credits` 余额（`GetBalance`）" | 改为 `token_bank_accounts` 口径，并加了"两个可用"的 UX 提示 |
| §3.6 claim | SQL 注释还写"发送方 Debit + 接收方 Credit" | 改为"claim 只绑人、不动账"，与 §14.7 第 ③ 步一致 |
| §3.6-C1 | 整段还在讲 `CreditsService.Debit` 穿透冻结 | 重写为**提取接口**穿透冻结 |
| §10 P0-3 | 写"CreditsService 入账" | 改为 `token_bank_accounts` 入账 + E1 并发判据 |
| §12.1 | 消费闭环写成"复用 Debit 抵扣算力卡"（购买钱包） | 改为 hub Grant 消费口径 |

### 16.2 v6 新引入 / v7 新发现

| # | 级别 | 结论 | 证据 / 理由 | 落到 |
|---|---|---|---|---|
| **E1** | 🔴 致命 | `token_bank_accounts` 在 hubcenter 三节点下**丢更新**，与被回滚的购买钱包是同款病，且**频率高得多**（每次调用都入账） | HA 同步按 `ha_entity_versions` 整行覆盖；账户余额是读-改-写 | §14.6-E1 |
| **E2** | 🔴 高 | 账本用 `REAL` 与 §5 的 microcredits 整数口径**自相矛盾**，漂移重新回到高频累加路径 | `token_bank_usage` 全是 `*_micro INTEGER` | §14.3-E2 |
| **E3** | 🟠 中 | `token_bank_withdrawals` 表 §14.5 依赖它做幂等，却**没有定义** | — | §14.3 已补 |
| **E4** | 🟠 中 | 提取接口 `POST .../credits/withdraw` 只在 §14.2 出现，**§6 API 清单漏了** | — | §6.1 已补 |
| **E5** | 🟠 中 | Grant 的 `ServiceGroupID` 注释成"可挂 `token_bank_low/mid/high`"——那是 ProviderArray，不是 ServiceGroup | `hub/.../service.go:858` 兑换卡时 `reg.FindModelServiceGroup(...) == nil` 会被跳过 | §14.4-E5 |
| **E6** | 🟠 中 | hub 是**自托管**的，一个用户可能有多台 hub；自动提取会让先 pull 的那台抽干额度，重装后 grant 丢失 | `hub/README.md`：self-hosted；§14.2 自动触发没限定 hub | §16.3 |

**实现期新发现的缺口（2026-10-01 开工后补入）**

| # | 级别 | 问题 | 证据 | 处置 |
|---|---|---|---|---|
| **D7** | 🟠 中 → ✅ 已修 | 连接级 `PRAGMA` 原先只打在池里的一个连接上 | `pragmaConnector`（`hubcenter/internal/store/sqlite/provider.go`）在每个新连接的 `Connect` 上执行 `foreign_keys` / `busy_timeout` / `synchronous` / `temp_store` / `cache_size` / `wal_autocheckpoint`。库级 `auto_vacuum` 先于 `journal_mode=WAL`，只在写库上执行一次。DSN 保持原样。生产 `MaxWriteOpenConns` 仍为 1（`config.go` 默认与 `cmd/hubcenter/main.go`） | §17.4；`TestConnectionPragmasApplyToEveryPooledConnection` 同时持有两个写连接，两边 `busy_timeout=4321` 且 `foreign_keys=1` |
| **D8** | 🟡 低 | §14.5 的平均分配是"剩余额度的 1/N"几何衰减，不是"每机预留 1/N"，晚问的机器拿得少 | `AutoWithdrawLimitMicro` 每次都拿当时 available 去除 | §17.5（按拍板公式实现，留作观察项） |
| **D9** | 🔴 高 → ✅ 已修 | `AppendLedger` 只写本地，**没有**生产者把新行推给 HA 对端；复制链路（实体/应用/分类）建好了却收不到东西 | `token_bank_ledger_sync.go` 只有 `InsertLedgerBatch`（收），没有 append-and-publish 的调用点 | §17.3（已补 `app/token_bank_ledger_sync.go` 生产者 + `SetSyncSink` 钩子，8 类账本写入全部覆盖） |
| **D10** | 🔴 高 → ✅ 已修 | `TokenBankWithdrawRequest` 没有 `AmountMicro`，store 自定金额；HTTP 传来的 `amount` 被忽略，hub 想提 2 分实际被提走全部 10 分 | §6.1 定义了 `{amount}` 但 store 层无法表达"部分提取" | §17.6（补字段；语义钉为"cap 是上限、按请求额精确执行、超 cap 报错不截断"，新增 `ErrTokenBankInsufficient`） |
| **D11** | 🔴 高 → ✅ 已修 | `amount_micro == 0` 被判非法，而 0 正是"给我允许的全部"——**自动补充（E6）的唯一正常请求**，该主路径在 HTTP 层走不到 | handler 里 `if amountMicro <= 0 { 400 }` | §17.6（0 合法，仅负数报错） |
| **D12** | 🔴 高 → ✅ 已修 | handler 硬编码 `Manual: true`，E6 的 1/N 上限**永不生效**，任一 hub 可一次提走用户全部积分 | `Manual` 未从请求体读取 | §17.6（改为 body 的 `manual`，**默认 false = 受限**） |
| **D13** | 🔴 高 → ✅ 已修 | `CreateShare` 的幂等早返回使模型列表变更**永远到不了库**：用户在上游启用新模型后重新分享，走的是幂等命中，新模型静默进不了池子；模型 upsert 的 `ON CONFLICT DO UPDATE` 是死代码 | 幂等检查在事务里最先执行，命中即 return | §17.7（新增 `SyncShareModels`：事务内校验归属 → upsert 新列表 → 不在列表的模型**禁用而非删除**） |
| **D14** | 🟠 中 → ✅ 已修 | 模型 upsert 的 `DO UPDATE` 漏了 `array_id` / `available` / `last_probe_error`。§3.4 明写「标档位 = 移阵列」，漏 `array_id` 意味着改档后调度仍投旧分组；漏 `available` 意味着已挂的模型永远显示绿色 | `ON CONFLICT DO UPDATE SET` 只列了 member_id/tier/tier_multiplier/enabled | §17.7（补齐三列；**用量计数器刻意不更新**，已在注释说明理由） |
| **D15** | 🟠 中 → ✅ 已修 | `ListGiftLinks` / `RevokeGiftLink` 把 `sender_user_id` 当必填。传空得到 `= ''`，**永远 0 行**——管理员看到的转赠审计是"从来没人转过"，空结果被读成真相；撤销同理，管理员无法冻结可疑链接 | SQL 与前置校验都假定 sender 非空 | §17.7（sender 改为可选：空 = 全体 / 管理员操作；解冻**永远回到行上记录的 sender**，传错 id 只会被拒） |
| **D16** | 🟡 低 → ✅ 已修 | 价目表的 `?` 通配符 handler 层没拦（store 的 `tokenBankWildcardPrefix` 明确拒绝 `?`），`gpt-4o?` 能入库但永远不匹配——一条看着生效、实际什么都不定价的规则 | handler 只校验了 `*` | §17.7（补 `ContainsAny(pattern, "?")` 拒绝分支） |

### 16.3 E6：多 hub 场景下"提取到哪台机器"必须定义

hub 是自托管服务（`hub/README.md`：*the self-hosted remote control service for MaClaw Desktop*），
同一个用户完全可能有多台（家里 / 公司 / 重装）。而 §14.2 的自动触发是
"hub 本地余额低于阈值 → pull"，于是：

- 账本有 100，A 机器自动提走 100 → B 机器永远拉不到；
- B 机器重装后 registry 重建，**之前提取过的 grant 消失**，hubcenter 侧已记 `withdrawn`，
  积分凭空没了。

**已拍板（2026-10-01）**：

1. `token_bank_withdrawals` 记 `hub_id` + `grant_id`（已加列），作为跨端对账与重放的凭据；
2. **自动提取默认开**，但多 hub 时按**平均方式**分配额度：
   单次上限 = `floor(available_micro / N)`，N = 该用户名下已注册的 hub 数（§14.5）。
   这样"先 pull 的那台抽干额度"不会发生；手动提取允许提完（用户显式意图优先）。
3. 提供「重新下发」：凭 `token_bank_withdrawals` 里已扣账但未生效的记录，
   用同一个 `request_id` 重放，为指定 hub 重建 grant，避免重装丢分（§14.5）。

### 16.4 v6 已核实为真的引用 ✅

| 文档说法 | 证据 |
|---|---|
| hub 的 Grant 结构（`CreditsTotal`/`CreditsUsed`/`Permanent`/`UsageEvents`） | `hub/internal/llmservice/registry.go:287-315` |
| `Source` 取值 card / new_user_limit_card / invitation_code / user_referral | `hub/internal/llmservice/service.go:884 / :1663 / :2113 / :2153` |
| grant 创建就是 `append(reg.Grants, ...)`、`NewID("grant")`、`roundCredits` | 同上；`:2153` 有 `CreditsTotal: roundCredits(credits)` |
| ~~hub 侧暂无任何 token_bank 代码~~ → 🔴 **v8 更正（R3）：已有 7 个文件** | `hub/internal/center/token_bank.go`（`WithdrawTokenBank` / `FinishTokenBankGrant` / `TokenBankHubWithdrawnMicro` / `RunTokenBankAutoOnce`）、`hub/internal/llmservice/token_bank_grant.go`（grant 创建 + `FindModelServiceGroup` 校验）、`hub/internal/httpapi/token_bank_handlers.go` + `router.go`，各带测试 |
| hub ↔ hubcenter 已有成熟通信通道 | `hub/internal/center/service.go`，≥12 处 `orderedCenterBaseURLs`（注册 / 心跳 / 凭据） |

> 💡 **顺带建议**：§14.2 的 hub→hubcenter pull **不要另起一套认证**，直接复用
> `hub/internal/center` 已有的 base URL 管理 + 凭据体系，省掉大半工作量。

### 16.5 v7 之后仍需拍板 → **2026-10-01 已全部拍板（采用推荐项）**

| # | 议题 | 结论 |
|---|---|---|
| 1 | **E1** 账本并发 | **append-only 流水账本**：真相源 = `token_bank_ledger`（`SUM` 即余额），`token_bank_accounts` 降级为**可 rebuild 的物化缓存** |
| 2 | **E2** 金额精度 | **账本与流水一律 `INTEGER micro`**（1e6）；只在生成 Grant 的最后一跳 `micro / 1e6` 转 float64 |
| 3 | **E6** 多 hub | 三条全做：`withdrawals` 记 `hub_id` + `grant_id`；**自动提取默认开**，多 hub 时按**平均方式**分配（单次上限 `floor(available_micro / N)`，N = 该用户名下已注册 hub 数）；手动提取可提完；提供凭 `request_id` 的「重新下发」 |
| 4 | **E5** ServiceGroupID | **不新建服务组**：由 hubcenter 管理员把 `token_bank_*` 阵列**分配进既有服务组**（结果体现在 `ServiceGroup.Models[].ProviderConfigs[].ProviderID` 里出现 `tbk_` 成员）；提取时 hub 显式传 `service_group_id`，两端各校验一次存在性 |
| 5 | **A1** 档位倍率 | 阵列 `CreditMultiplier` **恒 1.0 并强制锁定**；档位存 `token_bank_models.tier` / `tier_multiplier`，只在结算侧用 |
| 6 | **A3** Ensure 语义 | 采取**更强的一档**：不只补缺，而是每次启动**强制锁定**倍率 1.0 + 清空 `CreditMultiplierSchedule` / `Timezone`。理由见 §3.4-A3 的实现注记 |

> 说明：第 6 条比 v7 正文写的"只补缺"更严格。因为"只补缺"意味着管理员仍能把阵列倍率改成 2，
> 而那个值**会直接打到消费者实付**——正是 A1 要消灭的东西。既然这三个阵列是平台内置的分组容器，
> 倍率就不该是可改项，强制锁定才是防护。

---

## 17. 落地进度与实现期新发现（2026-10-01 开工后）

### 17.1 已落地代码

| 文件 | 内容 |
|---|---|
| `hubcenter/internal/llmservice/token_bank_arrays.go` | A1：三阵列 `CreditMultiplier` 恒 1.0；新增 `TokenBankTierMultiplier(tier)` / `TokenBankTierMultiplierOfArray(arrayID)` 只走结算侧；A3：`EnsureTokenBankArrays` 强制锁定并清空 `CreditMultiplierSchedule` / `Timezone` |
| `hubcenter/internal/llmservice/provider_array.go`（`copyArrayBillingToProvider`） | A2：`if arr.TokenPricing.HasCreditPricing()` 才覆盖 `dst.TokenPricing`，成员自带定价不再被阵列空定价冲掉 |
| `hubcenter/internal/store/sqlite/llm_repo.go` | `EnsureLLMTables` 追加 8 张表：`token_bank_shares` / `_models` / `_usage` / `_price_book` / `_share_requests` / `_ledger` / `_accounts` / `_withdrawals` + `credit_share_links`，金额列全 `INTEGER micro` |
| `hubcenter/internal/store/sqlite/token_bank_repo.go` | E1 账本：五个 bucket、`AvailableMicro()`（扣 frozen，C1）、`AppendLedger`（`INSERT OR IGNORE` + 事务内 SQL 原子增量，重放返回 `applied=false`）、`Balance`（`SUM` 为准）、`RebuildAccount` |
| `hubcenter/internal/store/sqlite/token_bank_withdraw.go` | 提取：`Withdraw`（`request_id` 幂等 + 事务内先查重放）、`AutoWithdrawLimitMicro`（E6 平均）、`countUserHubs`（`sm_users.id → email → hub_user_links` 三条 join）、`BindGrantID` / `MarkReissued` / `ListWithdrawals` |
| `hubcenter/internal/store/sqlite/token_bank_gift.go` | §3.6 转赠：`CreateGiftLink`（校验 50% 上限 + 写 `frozen` 冻结流水）、`ClaimGiftLink`（单个条件 UPDATE 原子抢，只绑人不动账）、`SettleClaimedGift`（**一笔事务内**解冻+granted+received 三行，幂等）、`RevokeGiftLink` / `ExpireGiftLinks` / `ListGiftLinks` |
| `hubcenter/internal/store/sqlite/token_bank_ledger_sync.go` | E1 的复制侧：`TokenBankLedgerBatch` + `InsertLedgerBatch`（批次级去重 + 行级 `INSERT OR IGNORE`）、`ListLedgerAfter`（补种分页）、`ledgerBatchID`（内容确定性 id） |
| `hubcenter/internal/ha/token_bank_sync.go` | 新增实体 `token_bank_ledger_batch`：`AttachTokenBankLedger` / `AppendTokenBankLedgerBatch` / `applyTokenBankLedgerBatchOp`；已进 `isSupportedEntityType` 与 admin 同步分类 `token_bank` |
| `hubcenter/internal/app/llm_init.go` | 接线：`haSvc.AttachTokenBankLedger(sqlite.NewTokenBankRepo(provider))`、`tokenBankRepo.SetSyncSink(ledgerBuffer.Add)` |
| `hubcenter/internal/app/token_bank_ledger_sync.go` | 复制**生产者**：`tokenBankLedgerSyncBuffer`（Add/Flush/Run，200 条或 15s）+ `seedTokenBankLedgerHAOps`（按启动时刻 cutoff 补种，确定性 batch id 防重发） |

> 🟠 **v8 补漏（R3 / R8）：v7 这张表漏记了至少 11 个文件，其中整个 `hub/` 目录完全没有出现。**
> 按文档判断进度会得出"hub 侧闭环还没接通"的错误结论，**实际上它已经通了**。
>
> **hub 侧（v7 记为"grep 零命中"，实际已有）**
>
> | 文件 | 内容 |
> |---|---|
> | `hub/internal/center/token_bank.go` | `WithdrawTokenBank`（pull 提取）、`FinishTokenBankGrant`（回写 `grant_id` / 重发）、`TokenBankHubWithdrawnMicro`、`RunTokenBankAutoOnce`（§14.5 自动提取后台路径） |
> | `hub/internal/llmservice/token_bank_grant.go` | grant 创建（含 `reg.FindModelServiceGroup` 校验，E5 判据②）、按服务组的剩余额度 `TokenBankRemainingMicro`、自动提取的确定性 `request_id`（`TokenBankAutoRequestID` / `TokenBankAutoSeq`） |
> | `hub/internal/httpapi/token_bank_handlers.go` + `router.go` | hub 本地端点与路由 |
> | `hub/internal/{center,llmservice}/token_bank*_test.go` | 上述行为的定向测试 |
>
> **hubcenter 侧漏记**
>
> | 文件 | 内容 |
> |---|---|
> | `httpapi/token_bank_hub_withdraw.go` | hub 专用提取端点（§6.1 补表），含服务组校验 |
> | `httpapi/token_bank_claim_route.go` | claim 路由到 `origin_node_id`（§8 第 9 条） |
> | `httpapi/token_bank_landing.go` | `GET /c/{code}` 领取落地页 |
> | `httpapi/token_bank_usage_handlers.go` | P1 #10 收益曲线 / #11 CSV 导出 |
> | `httpapi/token_bank_share_handlers.go` | 分享 CRUD（含 `SyncShareModels`，D13） |
> | `llmservice/token_bank_publish.go` | 成员发布（owner / 档位 / 倍率 / 显示名快照） |
> | `llmservice/token_bank_autopause.go` | P1 #1 连续失败自动暂停 |
> | `llmservice/token_bank_caps.go` | P1 #7 日/月熔断 |
> | `llmservice/token_bank_group.go` | 服务组与 tbk_ 成员归属（E5 校验基础） |
> | `store/sqlite/token_bank_usage_query.go` | 分时统计 / Top 模型 / CSV 查询 |

**单测全绿**（`go test ./hubcenter/internal/store/sqlite/ -run 'TokenBank|Gift|LedgerBatch|ListLedger'`，21 个用例）。
已固化为用例的判据：

| 判据 | 用例 |
|---|---|
| E1：并发结算后 `earned` **整数精确相等** | `TestTokenBankConcurrentSettlementKeepsExactSum` |
| E1：整批重放不重复入账（含换 batch id 的补种） | `TestTokenBankReplicatedRowsNeverDoubleCredit`、`TestTokenBankLedgerBatchIsIdempotent` |
| E2：`AvailableMicro` 必扣 `frozen` | `TestTokenBankAvailableMicroSubtractsFrozen` |
| C1：**冻结 5 后最多只能提 5，全链路不超发** | `TestGiftCannotOverIssueWhenSenderWithdrawsFirst`、`TestTokenBankWithdrawNeverOverdraftsFrozenCredits` |
| §3.6 两人并发领同一链接只有一人成功 | `TestGiftClaimIsFirstComeOnly` |
| §3.6 领取只绑人不动账，过账在提取时 | `TestGiftClaimMovesNoMoney` |
| E6 平均分配按剩余额度 1/N | `TestTokenBankHubCountDrivesAutoLimit`、`TestTokenBankAutoWithdrawSplitsAcrossHubs` |
| 提取 `request_id` 幂等（含重装重放） | `TestTokenBankWithdrawIsIdempotentOnRequestID`、`TestTokenBankWithdrawBindsGrantAndLists` |

### 17.2 ✅ 实现期修掉的三个自诊断问题（review 产物）

写完后自查发现并已修复，记在这里是因为它们都是"不修就会静默出错"的类型：

1. **提取重放返回 `ErrGiftLinkNotClaimed` 而不是"已结算"**（`SettleClaimedGift`）：
   结算后链接状态变 `settled`，再调用会落到 default 分支报错。正确语义是**返回 `applied=false`**——
   调用方问的是"请结算"，而结算已经在那儿了；报错会诱导调用方去做补偿写入。
2. **`MarkReissued` 匹配 0 行也返回 nil**：`request_id` 打错时会"成功"，运维据此以为
   丢失的 grant 已重发。已改为 `RowsAffected == 0` 报错。
3. **主键由明文拼接**（`"wd_" + requestID`）：`request_id` 由 hub 生成、长度不可控，
   拼接值可能超过索引/主键的合理长度且两表 id 有撞车风险。已改为
   `wd_` / `wdl_` / `frz_` / `unfrz_` / `grnt_` / `rcv_` + `sha256(requestID)[:16]` 十六进制，
   仍是 `request_id` 的**纯函数**（三节点必须算出同一个值），但长度恒定、两张表不会撞。

### 17.3 ✅ `token_bank_ledger` 的复制已接通

设计说"HA 回放 `INSERT OR REPLACE` 天然幂等"，但**只有接通才有复制**——
`llm_usage_records` 有 `llm_usage_sync.go` 这条链路，账本原本什么都没有。已按同款实现：

- 实体 `token_bank_ledger_batch`，进 `isSupportedEntityType` + 新增 admin 同步分类 `token_bank`；
- 批次级去重表 `token_bank_ledger_batches`，行级仍靠确定性主键 `INSERT OR IGNORE`；
- **空 origin 拒绝**（否则本节点的补种会把别人的行当成自己的再广播回去）；
- 对端 `InsertLedgerBatch` **只对本节点真正插入的行 bump 缓存**——
  从别处收到的行那边已经 bump 过，这里再 bump 就是重复计数。

> ✅ **复制链路已完整接通（2026-10-01 当日补齐）**。生产者在这一侧：
> `hubcenter/internal/app/token_bank_ledger_sync.go` 的 `tokenBankLedgerSyncBuffer`
> （批量 200 / 15s 或满批立即 flush，失败回灌队首）+ 启动补种 `seedTokenBankLedgerHAOps`
> （以**启动时刻**为 cutoff，之前的走补种、之后的走实时缓冲，同一条不会两路都发）。
> 写入侧通过 `tokenBankRepo.SetSyncSink` 在**事务提交之后**把每一行交给缓冲——
> **所有**写账本的地方都接了：`AppendLedger`、提取、冻结、解冻、grant、receive、撤销、过期。
> 判据：`TestTokenBankEveryLedgerWriteIsPublished`（每一类行都必须被发布）、
> `TestTokenBankPublishHappensAfterCommit`（发布时该行已可见，即已提交）、
> `TestTokenBankNilSinkIsSafe`（单机部署不设 sink 也能跑）。
>
> ⚠️ 仍需注意：`usage:<request_id>:<share_id>` 的 `share_id` 由**消费者侧**的请求决定，
> 但补种是按 `created_at` 时间序分页的。若两个节点的时钟偏差超过 flush 间隔，
> 实时缓冲和补种理论上可能对同一行各发一次——两路的 **batch id 不同但 row id 相同**，
> 接收端行级 `INSERT OR IGNORE` 兜住了，所以**不会重复入账**，只是多一次网络往返。
> 这条依赖"row id 是内容的纯函数"，不能改成随机 id。

### 17.4 ✅ D7：每个池连接都带上连接级 `PRAGMA`

原先 `sqlite.NewProvider` 用 `db.Exec("PRAGMA ...")` 下参数，`database/sql` 只会在池中的一个连接上执行它。modernc.org/sqlite v1.46.1 上，第二个写连接因此拿不到 `busy_timeout` 与 `foreign_keys`。

落地方式是 `pragmaConnector`（`hubcenter/internal/store/sqlite/provider.go`）：`Connect` 打开驱动连接后，立刻在该连接上执行连接级 pragma：

- `foreign_keys=ON`
- `busy_timeout`（取配置）
- `synchronous=NORMAL`
- `temp_store=MEMORY`
- `cache_size`（`CacheSizeKB > 0` 时）
- `wal_autocheckpoint=2000`（开启 WAL 时）

库级 pragma 只在写库上执行一次，顺序是 `auto_vacuum`（仅 `incremental` / `full` / `none`；空和 `off` 不动），然后 `journal_mode=WAL`。`mmap_size` 失败忽略。读库复用同一组连接级 pragma，不再单独下库级 pragma。DSN 保持原样，也不加 `cache=shared`。`:memory:` 的写库与读库仍是两个库，这是原行为。

生产 `MaxWriteOpenConns` 保持 **1**（`hubcenter/internal/config/config.go` 的默认值，以及 `hubcenter/cmd/hubcenter/main.go`）。连接器让以后单独决定放开写池时，新连接也会带上这些 pragma。这次没有改这个池大小。

判据：`TestConnectionPragmasApplyToEveryPooledConnection` 用文件库、WAL、`MaxWriteOpenConns=2`，同时持有两个写连接，两边 `PRAGMA busy_timeout` 都是 4321，`PRAGMA foreign_keys` 都是 1。

### 17.5 🟡 新发现 D8：平均分配的口径是"剩余额度的 1/N"，不是"每机预留 1/N"

§14.5 的公式 `floor(available_micro / N)` 每次都拿**当时剩余**去除，于是：

```
9 分、2 台机器：A 提 4.5 → 剩 4.5；B 再提 2.25 → 剩 2.25 …
```

即"先问的拿一半，后问的拿剩下的一半"，几何衰减，而不是"每台各 4.5"。
好处是积分不会因为某台机器从不来取而**滞留在 hubcenter**；
代价是晚问的机器拿得少（它可以过一会儿再问一次，账上还有余额）。

若要改成真"每机配额 1/N"，公式应为
`floor((earned+received-frozen-granted)/N) - 已提给本机的额度`，
但那会带来"配额被预留却无人领取"的新问题。**当前实现按拍板公式走，此项留作观察项。**

### 17.6 ✅ P0-2 服务端 API（§6.1 客户端段已落地）

| 文件 | 内容 |
|---|---|
| `hubcenter/internal/httpapi/token_bank_handlers.go` | §6.1 全部客户端接口：`GET /token-bank/summary`、`POST /token-bank/credits/withdraw`、`GET /token-bank/credits/withdrawals`、`POST/GET /credits/share-links`、`POST /credits/share-links/{id}/revoke`、`GET /credits/share-links/{code}/preview`、`POST /credits/share-links/{code}/claim`。另有 `tokenBankRepoView`（窄接口，便于测试与 nil 安全）、`creditsToMicro`（溢出守卫）、`newGiftLinkCode`、`maskEmail` |
| `hubcenter/internal/httpapi/skillmarket_handlers.go` | 新增 `tokenBank` / `nodeID` 字段 + `SkillMarketConfig.TokenBank/NodeID` + `SetTokenBankRepo`（setter 而非构造参数，因为 repo 由 LLM 模块创建，而 SkillMarket 必须先于 LLM 持久化失败仍可用） |
| `hubcenter/internal/httpapi/router.go` | 8 条路由注册，位于 smHandlers 段 |
| `hubcenter/internal/app/bootstrap.go` | `smHandlers.SetTokenBankRepo(llmModule.TokenBank, nodeID)` |
| `hubcenter/internal/app/llm_init.go` | `LLMModule.TokenBank` 透出 repo；`tokenBankRepo` → `TokenBankRepo` 导出 |
| `hubcenter/internal/store/sqlite/token_bank_{withdraw,gift}.go` | 新增 `AmountMicro` 字段与 `ErrTokenBankInsufficient`；`CountUserHubs` / `GiftLinkByCode` 读侧导出方法 |

**实现期新发现并修掉的三个问题（都是"不修就静默算错钱"的类型）：**

1. **`Withdraw` 根本没有"按指定金额提取"的能力（D10）。**
   `TokenBankWithdrawRequest` 原先没有 `AmountMicro` 字段，store 自己决定金额
   （manual 取全部 / 自动取 1/N）。而 §6.1 的 body 里明写 `{amount}`，
   于是 HTTP 层传进来的金额**被完全忽略**：hub 想提 2 分、账上有 10 分，
   实际被提走 10 分。已补 `AmountMicro`，并把语义钉死为
   "**cap 是上限，请求额精确执行，超 cap 报错而不是截断**"。
   截断是错的：hub 以为提了 9 实际只提了 3，而它重试时 `request_id` 相同会被
   幂等去重，于是**永远收敛不到真相**。对应的 store 层新增 `ErrTokenBankInsufficient`
   （与 `ErrTokenBankNothingToWithdraw` 分开，因为"你没钱了"是终态、
   "你要多了"是可以换个更小数字重试的）。

2. **`amount_micro == 0` 被当成非法输入（D11）。**
   而 0 正是"给我允许的全部"——也就是**自动补充（E6）的唯一正常请求**。
   结果是自动补充这条主路径**在 HTTP 层根本走不到**。
   已改为：0 合法（取当前模式允许的全部），只有负数报错。

3. **`Manual` 被硬编码为 `true`（D12）。**
   handler 里写死 `Manual: true`，于是 E6 的 1/N 上限**永远不会生效**，
   任何 hub 都能一次提走用户全部积分——正是一台机器独吞的失效模式。
   已改为从 body 读，**默认 false（即受限模式）**：老客户端不知道这个字段，
   缺省必须是安全的那一侧；而且 HubCenter 无法从请求本身区分"人在点按钮"
   和"cron 在跑"。

另修：`credits * 1_000_000` 的**整数溢出无守卫**（用户传超大 `credits`
会让 int64 回绕成负数或错误的正常数），已抽成 `creditsToMicro` 并加边界用例；
以及两个"同时传两种单位"的歧义请求（`amount`+`amount_micro`、
`credits`+`credits_micro`）现在一律 400，而不是静默取其一。

**判据用例（27 个，`go test ./hubcenter/internal/httpapi/ -run 'TestTokenBank|TestCreditsToMicro'` 全绿）：**

| 判据 | 用例 |
|---|---|
| 未登录不能看余额 | `TestTokenBankSummaryRequiresSession` |
| `available = earned + received − withdrawn − granted − **frozen**` | `TestTokenBankSummaryExcludesFrozenFromAvailable`、`TestTokenBankWithdrawRefusesFrozenCredits` |
| 转赠后立刻冻结、可用余额同步变小、再转赠受 50% 上限约束 | `TestTokenBankGiftCreateFreezesAndShrinksAvailable`、`TestTokenBankGiftRejectsOverCap` |
| 预览页脱敏发送者、不泄露领取人与发送者余额 | `TestTokenBankGiftPreviewMasksSenderAndHidesCodeOwner` |
| 自己不能领自己的；两人抢只有一人成功 | `TestTokenBankGiftClaimIsSingleWinnerAndForbidsOwnLink` |
| 领取需 verified 账号 | `TestTokenBankGiftClaimRequiresVerifiedAccount` |
| 撤销解冻、重复撤销不重复解冻、他人不能撤销 | `TestTokenBankGiftRevokeReturnsFrozenCredits`、`TestTokenBankGiftRevokeRejectsAnotherUsersLink` |
| 提取 `request_id` 幂等 + 重放返回同一记录 | `TestTokenBankWithdrawRequiresRequestIDAndIsIdempotent` |
| D10：按指定金额提取（不是提走全部） | `TestTokenBankWithdrawAcceptsWholeCreditAmount`、`TestTokenBankWithdrawCannotExceedAvailable` |
| D11：`amount_micro=0` 取"允许的全部" | `TestTokenBankWithdrawZeroAmountTakesTheWholeManualBalance` |
| D12：默认受限模式，超 cap 报错 | `TestTokenBankWithdrawDefaultsToCappedMode` |
| 溢出守卫 | `TestCreditsToMicroRefusesOverflow`、`TestTokenBankGiftRejectsBothUnitsAndOverflow` |
| 每用户只能看到自己的流水/链接 | `TestTokenBankWithdrawalsAreListedForTheCallerOnly`、`TestTokenBankGiftListShowsOwnLinksOnly` |
| 过期链接 `claimable=false` 且领取返回 410 | `TestTokenBankGiftExpiryIsReportedNotClaimed` |

**尚未落地（P0-2 剩余部分）**：无。§6.1 / §6.2 / §6.3 全部落地，见 §17.7。

---

### 17.7 ✅ P0-2 服务端 API 收尾（§6.2 管理后台 + §6.3 自动化）

| 文件 | 内容 |
|---|---|
| `hubcenter/internal/store/sqlite/token_bank_share.go` | 新建（约 25 KB）：`TokenBankShare/ShareModel/PriceRule/Settings/OwnerSummary/Overview`；`CreateShare`（幂等 on owner+fingerprint、cap 校验、模型同事务 upsert）、`SyncShareModels`、`ListShares`、`ListShareOwners`、`Overview`、`SetSharePaused`、`TakeOutShare`、`RenameShareKey`、`LoadShare`、`ListModels`、`SetModelTier`、`CountShareModels`、`ListPriceRules`、`UpsertPriceRule`、`DeletePriceRule`、`ResolvePrice`、`DefaultTokenBankSettings`、`ParseTokenBankSettings` |
| `hubcenter/internal/store/sqlite/token_bank_share_test.go` | 新建：40 个用例（见下表） |
| `hubcenter/internal/httpapi/token_bank_admin_handlers.go` | 新建（约 27 KB）：§6.2 全部 14 条接口 + `tokenBankAdminRepoView` 窄接口 + `validateTokenBankSettings` + `tokenBankAdminPage` |
| `hubcenter/internal/httpapi/token_bank_admin_handlers_test.go` | 新建：20 个用例，含真实 admin 登录（`RequireAdmin` 走真 token 校验） |
| `hubcenter/internal/httpapi/llm_routes.go` | §6.3 三条路由，`RequireLLMAdminScope` 分别 read/write/delete；`RegisterLLMRoutes` 增加 `smHandlers` 参数 |
| `hubcenter/internal/httpapi/router.go` | §6.2 十四条路由（`RequireAdmin`）；`llmRouteHook` 签名加上 `*SkillMarketHandlers` |
| `hubcenter/internal/app/llm_init.go` | hook 闭包透传 `smHandlers` |
| `hubcenter/internal/httpapi/llm_admin_api_doc.go` | Markdown 加「Token Bank 分享管理」章节 + 3 条 curl；OpenAPI 加 3 个 path + `TokenBankPaused` schema |
| `hubcenter/internal/store/sqlite/token_bank_gift.go` | `ListGiftLinks` / `RevokeGiftLink` 的 sender 参数改为**可选**（空 = 全体 / 管理员操作） |

**实现期新发现并修掉的四个问题（D13～D16）：**

4. **`CreateShare` 的幂等早返回让「模型列表变更」永远到不了库（D13）。**
   幂等检查（同 owner + 同 key fingerprint）在事务里**最先**执行，命中就直接
   `return existing, false`。于是 `upsertTokenBankModelTx` 里那段
   `ON CONFLICT(share_id, model_name) DO UPDATE` 是**死代码**：
   它只可能在一个 share 行已插入、模型却没写的情况下触发，而这两件事在同一事务里，
   不可能只成功一半。
   实际后果：用户在上游启用了新模型，重新点「分享」——因为 key 没变，走的是幂等命中，
   **新模型永远进不了池子**，而且没有任何报错。
   已新增 `SyncShareModels(ctx, shareID, scopeOwner, models, now)`：
   在的事务内先校验归属，再 upsert 新列表、把不在列表里的模型 `enabled = 0`（**禁用而非删除**，
   因为行上挂着累计用量与收益，且可能有在途结算），返回被退休的条数。
   同时把 `CreateShare` 里那段注释改成实话（它只服务首次插入）。

5. **`array_id` 不在 `ON CONFLICT DO UPDATE` 里（D14）。**
   §3.4 明写「**标档位 = 移阵列**」——阵列**就是**档位，所以改档必须跟着移阵列，
   否则调度仍把成员投到旧分组。
   已把 `array_id`（以及同样漏掉的 `available` / `last_probe_error`）加入更新列。
   `available` / `last_probe_error` 属于同一类漏：§3.1 说服务端定期复探的结果要覆盖客户端初筛，
   漏掉它们意味着一个已经挂了的模型会**永远显示绿色**。
   **用量计数器（`used_input_tokens` / `used_output_tokens` / `earned_micro`）刻意不在更新列里**——
   重新探测不是抹掉历史收益的理由，这一点已在代码注释里写明。

6. **`ListGiftLinks` 把 `sender_user_id` 当成必填（D15）。**
   原来的 SQL 是 `WHERE sender_user_id = ?`，传空字符串得到的是
   `sender_user_id = ''`——**永远返回 0 行**。管理员打开 §6.2 的转赠审计，
   看到的会是「从来没有人转赠过」，而不是他要的审计表。这种"空结果被读成真相"
   比报错更难发现。已改为可选谓词（空 = 全体），与 `ListShares` 的 owner 过滤口径一致。
   同一处修正也施加到 `RevokeGiftLink`：管理员需要能冻结可疑链接（§6.2），
   而它原先要求 sender 非空且必须匹配。改为**空 = 管理员操作、跳过归属校验**，
   且解冻**永远回到行上记录的 sender**（不是入参），所以传错 id 只会被拒绝，
   不可能把退款改道。

7. **价目表的 `?` 通配符在 handler 层没被拦住（D16）。**
   store 的 `tokenBankWildcardPrefix` 只认结尾 `*`、并显式拒绝 `?`，
   但 handler 的入参校验只数了 `*`。于是 `gpt-4o?` 会被**存进价目表**，
   然后在解析时永远不匹配——一条看起来生效、实际什么都不定价的规则。
   已在 handler 补 `strings.ContainsAny(pattern, "?")` 的拒绝分支。

**判据用例（§6.2 20 个 + store 40 个；`go test ./hubcenter/internal/httpapi/ -run 'TestTokenBankAdmin|TestTokenBankSettingsValidation'`、
`go test ./hubcenter/internal/store/sqlite/ -run 'TestTokenBank'` 均全绿）：**

| 判据 | 用例 |
|---|---|
| §6.2 全部接口无 token 一律 401 | `TestTokenBankAdminEndpointsRequireAdmin` |
| 设置：未保存时返回文档默认值，PUT 后落库可回读 | `TestTokenBankAdminSettingsServeDefaultsThenPersist` |
| 设置：费率 >1 / 负数、ratio >1、ttl=0、max_shares=0、负单价一律 400 | `TestTokenBankAdminSettingsRejectInvalidValues` |
| 设置：非法 JSON 400；文档默认值本身能过校验器 | `TestTokenBankAdminSettingsRejectMalformedBody`、`TestTokenBankSettingsValidationAcceptsTheDefaults` |
| 总览：统计卡 + 回显实时费率 | `TestTokenBankAdminOverviewAndUsers` |
| 分享列表：**密钥恒为空串**、邮箱掩码、`has_key` 正确 | `TestTokenBankAdminSharesMasksEmailAndNeverReturnsTheKey` |
| 分享列表：按 user_id / status 筛选 | `TestTokenBankAdminSharesFilterByUserAndStatus` |
| 暂停：**空请求体默认按暂停处理**，恢复必须显式 false | `TestTokenBankAdminPausedDefaultsToPausing` |
| 暂停/取出未知 id → 404 | `TestTokenBankAdminPauseUnknownShareIs404`、`TestTokenBankAdminTakeOutShareRemovesIt` |
| 档位：canonical 三元组（high → ×2.0 + token_bank_high） | `TestTokenBankAdminSetModelTierUsesCanonicalPair` |
| 档位：非法档位 / custom 缺倍率 / 负倍率一律 400 | `TestTokenBankAdminSetModelTierRejectsBadTier` |
| 档位：custom 用显式倍率 | `TestTokenBankAdminSetModelTierCustomTakesTheExplicitPair` |
| 档位：未知 share/model → 404 | `TestTokenBankAdminSetModelTierUnknownTargetsAre404` |
| 价目表 CRUD + 按 id 删 + 重复删 404 | `TestTokenBankAdminPriceBookCRUD` |
| D16：`*` 中缀 / `?` 通配一律 400，尾 `*` 放行 | `TestTokenBankAdminPriceBookRejectsMidPatternWildcard` |
| 价目表：负价 / 缺 pattern → 400 | `TestTokenBankAdminPriceBookRejectsNegativeAndMissingPattern` |
| D15：审计两侧都掩码、已领取不可撤销 | `TestTokenBankAdminCreditSharesAuditMasksBothSides` |
| D15：**空 sender 过滤 = 全体**（两个发送方都出现） | `TestTokenBankAdminAuditListsEverySender` |
| D15：管理员撤销后冻结回到发送方、可用额恢复到 100 | `TestTokenBankAdminRevokeCreditShareReturnsFreezeToSender` |
| D15：重复撤销 → 409；未知 → 404 | `TestTokenBankAdminRevokeCreditShareReportsNotActive` |
| 分页上限 200、默认 20、负 offset 归零 | `TestTokenBankAdminPageIsClamped` |
| 未接入 LLM 模块时 503 而非 panic | `TestTokenBankAdminEndpointsReportUnavailableWithoutRepo` |
| D13：给已有 share 补新模型（幂等命中后仍能加） | `TestTokenBankSyncShareModelsAddsNewModelsToALiveShare` |
| D13/D14：同步保留用量与收益、刷新 array/available | `TestTokenBankSyncShareModelsPreservesEarnings` |
| D13：被移除的模型是**禁用**而非删除，再加回是同一行 | `TestTokenBankSyncShareModelsDisablesRetiredModels` |
| D13：同步的归属校验与去重校验 | `TestTokenBankSyncShareModelsScopesToOwnerAndValidates` |
| 首次分享：模型与 share 同事务、默认值正确 | `TestTokenBankCreateShareWritesModelsInOneTransaction` |
| 幂等：同 owner 同 fingerprint 复用原行（改了 id 也复用） | `TestTokenBankCreateShareIsIdempotentOnOwnerAndFingerprint` |
| 不同 fingerprint 才建第二条 | `TestTokenBankCreateShareDistinctFingerprintCreatesSecondShare` |
| cap：达到上限拒绝、**幂等命中优先于 cap** | `TestTokenBankCreateShareEnforcesPerUserCap` |
| cap：取出后**槽位归还** | `TestTokenBankTakeOutShareFreesASlotUnderTheCap` |
| 必填字段与非法模型导致**整体回滚** | `TestTokenBankCreateShareRequiresMandatoryFields`、`TestTokenBankCreateShareRollsBackWhenAModelIsInvalid` |
| 取出：**用量历史存活**、同一 key 可重新分享 | `TestTokenBankTakeOutShareKeepsUsageHistoryAndFreesTheKey` |
| 换 Key：保住 id 与累计收益；撞到别的 share 报 `ErrTokenBankDuplicateKey` | `TestTokenBankRenameShareKeyKeepsShareIdentityAndEarnings`、`TestTokenBankRenameShareKeyRejectsRotationOntoAnotherShare` |
| 列表分页/排序、owner 聚合无 N+1 | `TestTokenBankListSharesHonoursLimitAndOffset`、`TestTokenBankListShareOwnersAggregatesWithoutNPlusOne` |
| 总览排除已撤销的 share 与其模型 | `TestTokenBankOverviewCountsSharesModelsAndCredits` |
| 价目解析：精确胜前缀、最长前缀胜、无匹配报 false | `TestTokenBankResolvePriceExactBeatsPrefix`、`TestTokenBankResolvePriceLongestPrefixWins`、`TestTokenBankResolvePriceReportsNoMatch` |
| 通配前缀只认结尾 `*` | `TestTokenBankWildcardPrefixOnlyHonoursTrailingStar` |
| 设置解析：默认值、损坏 blob 回默认、前向兼容修复、（费率 0 有意义不算未设置） | `TestTokenBankSettingsDefaultsAndForwardCompatibility` |

**§17.6 / §17.7 之后的收尾说明**：`hubcenter/internal/httpapi` 与
`hubcenter/internal/store/sqlite` 两个包在全量 `-count=1` 下通过；
`gofmt` 与 `go vet` 干净。§6.3 的文档同时落在 `llm_admin_api_doc.go` 的
Markdown 与 OpenAPI 两处，并由 `TestLLMAdminAPIDocsArePublic` 断言
（含"OpenAPI 必须是合法 JSON"这一条，因为一个语法错误的文档比没有文档更糟）。


### 17.8 ✅ P0-3 结算接入（store 结算 + proxy 钩子）

§5 的钱规则只实现在一处：`store/sqlite/token_bank_settle.go` 的 `SettleTokenBankUsage`。
它在一个事务里写 `token_bank_usage` 与账本、并 bump `token_bank_accounts`，提交后才 publish（HA）。
proxy 侧是一个薄钩子，三处 `RecordUsage` 站点（缓存命中 / 缓冲响应 / 流式）共用一个入口
`settleTokenBankUsageForProvider`，差异在调用方解决，钱规则只此一份。

| 落点 | 文件 | 内容 |
| --- | --- | --- |
| 结算（§5） | `store/sqlite/token_bank_settle.go` | `SettleTokenBankUsage`、`ComputeTokenBankSettlement`、`ListUsage`、`SumUsageMicro` |
| 钩子 | `llmservice/token_bank_settle.go` | `TokenBankSettler` 接口、`settleTokenBankUsageForProvider`（统一入口） |
| 装配 | `app/token_bank_proxy_settler.go` | 把 `*sqlite.TokenBankRepo` 绑到接口；`bootstrap.go` 装入 `ProxyCfg` |

**接口而非直接依赖**：`llmservice` 不能 import `store/sqlite`——proxy 会被链进不开库的二进制。
装配放在 `app`，因为只有它同时认识两侧。

**owner / 档位 / 费率 / 单价一律由 settler 现读**，代理不传：调档要能反映、取出要能变 no-op。
把这几项当入参会同时引入两类错误——用错的 owner 付错人，用旧倍率付错钱。

#### 两个实现期发现的缺陷

**D17（🔴 高 → ✅ 已修）：第二个模型的分账被账本唯一索引静默吞掉。**
`token_bank_ledger` 除主键外还有 `idx_tbk_ledger_bizkey = UNIQUE(biz_key) WHERE biz_key <> ''`。
最初 `biz_key` 写成 `usage:<request>:<share>`（**不含 model**），而账本行 id 含 model。
于是一次请求命中同一 share 的两个模型时，第二笔 `INSERT OR IGNORE` 被 **biz_key 唯一索引**挡下：
usage 行写了、`Applied=true`、**账本少一条、少付一份钱，且无任何报错**。
修法：抽出 `tokenBankUsageBizKey(request, share, model)` 作为唯一真源，账本 id 由其 hash 派生，
写入与 publish 共用。
**教训**：`INSERT OR IGNORE` 在存在多个唯一约束时，会被**任一**约束忽略；
"没插进去"不能默认为"主键冲突"，必须知道另一条唯一索引的存在。

**D18（🔴 高 → ✅ 已修）：分享者单价一度取用消费者的路由定价，抹平了两侧独立性。**
§5 明确两侧**刻意独立**：消费者沿用服务组既有定价（无感），分享者按
`token_bank_price_book × 档位倍率`。第一版把消费者的定价快照单价传给了结算，
等于让 `gross` 与 `charged` 同源——**倒挂会几乎永不触发，`net_clamped` 永远为 0，
真实倒挂被掩盖**。修法：`ChargedMicro` 是代理唯一的输入，单价由 settler 从
`ResolvePrice(model)` 解析（无命中回落 settings 默认单价）。

#### 测试

| 保证 | 用例 |
| --- | --- |
| §5 每条腿独立向上取整；零 token 为 0 | `TestComputeTokenBankSettlementRoundsEachLegUp` |
| 缓存两条腿独立计价 | `TestComputeTokenBankSettlementCacheLegsAreSeparate` |
| 费率越界被夹取（不产生负 net） | `TestComputeTokenBankSettlementClampsFeeRate` |
| 入账金额 = gross − fee，且进 `earned` 桶 | `TestSettleTokenBankUsageCreditsOwner` |
| 重放只入账一次，且**读回首次金额**（调档后重放不报新数） | `TestSettleTokenBankUsageIsIdempotent`、`TestSettleTokenBankUsageReplayReturnsCanonicalAmounts` |
| 同 request 不同 model 各记一笔（D17 回归） | `TestSettleTokenBankUsageDistinctModelsSettleSeparately` |
| 防倒挂：压到 charged 并标 `net_clamped` | `TestSettleTokenBankUsageClampsToCharged` |
| `charged=0`：net=0 但 usage 行仍写（"被调用却没积分"可解释） | `TestSettleTokenBankUsageZeroChargedPaysNothing` |
| owner 缺失时不是一个错误，只是不结算 | `TestSettleTokenBankUsageMissingOwnerIsNotApplied` |
| **E1 并发 exactly-once**：24 个调用 × 4 并发重放 → 恰好 24 usage / 24 ledger | `TestSettleTokenBankUsageConcurrentIsExactlyOnce` |
| 非 Token Bank provider 零额外开销（不查库） | `TestSettleTokenBankUsageIgnoresNonMemberProvider` |
| 结算用的是成员 id 里的模型，不是 logical 模型名 | `TestSettleTokenBankUsageUsesModelFromMemberID` |
| 已取出的 share 不结算、不报错 | `TestSettleTokenBankUsageSkipsWithdrawnShare` |
| 未装配 settler → no-op 而非 panic | `TestSettleTokenBankUsageNilSettlerIsNoOp` |
| 调用方传入的单价**不能**覆盖价目表（D18 回归） | `TestSettleTokenBankUsageOwnerUnitsNotConsumerPricing` |
| 适配器：真库端到端入账、大小写不敏感、未知 share/model 回落、价目表/默认单价、坏 blob 回落 | `TestTokenBankProxySettler*`、`TestNewTokenBankProxySettlerNilRepoIsNil` |
| `CreditsToMicrocredits` 往返不漂移 + 溢出饱和不回绕 | `TestCreditsToMicrocreditsRoundTripsWithInverse` |

---

### 17.9 ✅ P0-6 admin Token 银行 tab

**纯前端阶段**：§6.2 的管理端接口在 P0-2 已全部落地，本阶段只新增页面，未改任何 Go 业务代码。

#### 新增文件

`hubcenter/web/admin/assets/js/token-bank-tab.js`（IIFE，约 880 行）。

设计约束（刻意为之）：

1. **不在这里算钱。** 费率、价目表、档位倍率都是钱，钱的唯一实现只在 Go store。UI 只负责收值、发出去、**再把服务器接受后的结果读回来**，所以被夹取或被修正的设置显示的是服务器认可的版本，而不是操作者输入的那一份。
2. **只发可编辑字段。** `PUT settings` 的 blob 里还有 `fee_target` / `require_review_before_online` / `require_verified_identity` 等本表单不编辑的标志位；回填一个猜测值等于悄悄改写没人动过的设置，因此这些字段原样透传。
3. **不引入内联样式。** 所有版式走 `admin-shell.css` 已有类；`TestAdminPageKeepsStyleAndScriptSplit` 会因此变绿（见下方 D19）。

#### 六个子页

| 子页 | 数据源 | 操作 |
| --- | --- | --- |
| 总览 | `GET /api/admin/token-bank/overview` | 只读指标卡（分享用户/有效分享/共享模型/已服务 tokens/已赚/已转赠/已提取/冻结）+ 费率与转赠上限 |
| 设置 | `GET/PUT /api/admin/token-bank/settings` | 费率、四条默认单价、单用户分享上限、转赠比例/有效期/日限/最小值、连续失败自动暂停 |
| 分享用户 | `GET /api/admin/token-bank/users` | `limit`/`offset` 分页 |
| 共享模型 | `GET /api/admin/token-bank/shares` + 状态/拥有者筛选 | `low/mid/high` 评级、暂停/恢复、取出 |
| 定价表 | `GET/POST/DELETE /api/admin/token-bank/price-book` | 增改删规则 |
| 积分转赠 | `GET /api/admin/token-bank/credit-shares` | 冻结（退回发送方） |

#### 接线（四处，缺一不可）

1. `index.html` — `platform` 导航组内 `llmservice` 之后新增 `<button data-tab="tokenbank">`。
2. `index.html` — `<section id="tab-tokenbank">` 面板：子页 tab 条（`tbkSubTab{Overview,Settings,Users,Shares,Price,Creditshares}`）+ 六个视图容器。
3. `index.html` — `<script src=".../token-bank-tab.js?v=..." defer>` 紧邻 `llm-service-tab.js`。
4. `admin-core.js` — 四处：`tabMeta.tokenbank`、`TAB_ICONS.tokenbank`、i18n `navTokenBank`/`navTokenBankDesc`（en + zh 两表）、`openTab` 内的 `initTokenBankTab()` 分发。

> `token-bank-tab.js` 自己也**包装了 `window.openTab`** 作为兜底，但真正的接线在 `admin-core.js` —— 依赖包装会让 `tabMeta` 与图标回落到 overview，`TestAdminPageStaticNavHasPageChrome` 会当场报出来（D20）。

#### 实现期新发现

| 编号 | 级别 | 问题 | 修复 |
| --- | --- | --- | --- |
| **D19** | 🟠 MED | `TestAdminPageKeepsStyleAndScriptSplit` 失败：`llm-service-tab.js:2246` 的 `llmPrvWorkBuddy` 用 `style="grid-column:1 / -1"` 内联版式。测试要求 bundle 里不得出现内联样式 | 改用 CSS 里**已有**的 `.grid-span-all{grid-column:1/-1}` |
| **D20** | 🟠 MED | 只靠 JS 包装 `openTab` 注册 tab 不够：`tabMeta` 缺失会让标题/副标题回落 overview，`TAB_ICONS` 缺失会让页面图标回落 | 老老实实在 `admin-core.js` 补 `tabMeta` + `TAB_ICONS` + i18n + 分发四处 |
| **D21** | 🟡 LOW | `saveProvider` 丢字段：帐单编辑器（时区/倍率/分时窗）在 provider 保存路径上被 `existing.*` 覆盖回旧值，UI 显示的是编辑器里的新值而落库的是旧值 | `saveProvider` 现在以 `readProviderBilling()` 为准写回 `payload.timezone` / `credit_multiplier` / `credit_multiplier_schedule`，并在窗口非法时按 `billingDroppedWindows` 拦下保存 |
| **D22** | 🟡 LOW | `loadProviderTraffic` 只处理 `data.traffic` 的对象形状 | 兼容数组形状 |
| **D23** | 🟠 MED | **wire tag 混用**：`shares` / `price-book` / `overview` / `users` 直接 marshal store 结构体（**无 json tag → PascalCase**），而 `credit-shares` 用专门的 wire 结构体（**snake_case**）。UI 一开始统一按 PascalCase 读，导致转赠审计页**全部单元格空白** | 转赠页改读 snake_case（`credits_micro` / `sender_email_masked` / `claimed_by_email_masked` / `revocable`），并只在 `revocable` 为真时渲染「冻结」按钮 |
| **D24** | 🟡 LOW | 共享模型筛选传的是 `?owner=`，而服务端读的是 `?user_id=` —— **筛选被静默忽略，显示全部** | 改用 `user_id` |
| **D25** | 🟡 LOW | 定价表表单只提交输入/输出两条腿，缓存读/写腿无处可填（后端支持四条腿） | 表单补 `tbkPriceCacheRead` / `tbkPriceCacheWrite` |
| **D26** | 🟡 LOW | 设置表单的兜底值与 `DefaultTokenBankSettings()` 不一致（单价填 0 而服务端是 3.0/6.0/0.3/3.75；`max_shares_per_user` 填 5 而服务端是 20） | 抽出 `TBK_SETTINGS_DEFAULTS` 与 Go 侧逐字段对齐；`pick()` 只把 `null/undefined` 当未设置，**0 是合法值**（免费模型、零费率），用 `\|\|` 会把真实的 0 换成默认值 |
| **D27** | 🟠 MED | **状态字面量与 store 不一致 + 共享模型列表无分页**。下拉框给的是 `available`/`paused`/`revoked`，而 store 是 `TokenBankShareStatusActive = "active"`。于是 `Status === 'available'` **恒为 false**：(a)「生效中」筛选**永远返回 0 行**；(b) 每一行都渲染成「恢复」按钮（连生效中的也是）。同时列表**没有分页器**、`offset` 从不发送 —— 服务端 `limit` 默认 20、上限 200，装了 50 个分享的实例会**永远只显示前 20 行**，看不出来 | 见下方「D27 修复明细」 |
| **D28** | 🟠 MED | **`tabMeta` 引用了未定义的 i18n 键**。`tabMeta.tokenbank = ['tbkTitle','tbkDesc']`，但两张 i18n 表都没定义这两个键。`tr()` 的兜底是 `\|\| key`，所以页面标题渲染成字面量 **`tbkTitle`**、副标题渲染成 **`tbkDesc`**。tab 能打开、面板能渲染，没有任何测试会发现 | i18n 表（en + zh）补 `tbkTitle` / `tbkDesc`；新增 `TestAdminPageTabMetaKeysAreDefined` 把整类问题变成编译期约束 |

##### D27 修复明细

1. **状态常量单一来源**：新增 `SHARE_STATUSES = ['active','paused','revoked']` 与 `shareStatus(s)` / `shareIsLive(s)`，筛选下拉、徽章、按钮三处共用；状态文案走 i18n `tbkStatus_{active,paused,revoked}`。
2. **`active` 筛选恢复可用**：筛选把 `[''].concat(SHARE_STATUSES)` 作为选项来源，`status=` 参数直接传字面量，与服务端 `ListShares(ctx, owner, status, ...)` 的期望一致。
3. **按钮语义修正**：`live` 为真才是「暂停」，否则「恢复」；`revoked` 的分享**不渲染**切换按钮（已取出，点必然失败）。这不是禁用，是不渲染。
4. **补分页器**：`sharesPager(loaded)` / `tokenBankSharesPage(delta)`，`loadTokenBankShares()` 发送 `offset = page × size`；`atEnd` 判据为 `loaded < state.shares.size`。切筛选时把 `page` 归零（否则停在第 3 页看一个新的空集，像是「筛选没匹配到」）。
5. **`rawNum()` 收口**：删除死代码 `credits()`；`num()` 用 `toLocaleString`，会插入千分位分隔符，而分隔符在 `<input type=number>` 里**非法**（浏览器渲染成空）。回填控件的值一律走 `rawNum()`。

> D27 与 D28 属同一类：**UI 与服务端契约的静默失配**。共同点是「什么都不报错」—— 不抛异常、不打印、页面照常渲染，只是数据不对或标题是变量名。这类缺陷靠人眼 review 很难稳定抓到，所以 D28 直接补了静态测试；D27 由 `TestTokenBankAdminSharesFilterByUserAndStatus` 在服务端侧兜住筛选语义。

#### 测试

| 保证 | 用例 |
| --- | --- |
| 新脚本按既定顺序挂载、且 `defer` | `TestAdminPageSplitScriptOrder`、`TestAdminPageSplitScriptsAreDeferred` |
| `tokenbank` 有 `tabMeta` 与 `TAB_ICONS`，页面图标不回落 | `TestAdminPageStaticNavHasPageChrome` |
| bundle 内无内联样式（D19 回归） | `TestAdminPageKeepsStyleAndScriptSplit` |
| 帐单编辑器字段真落库（D21 回归） | `TestAdminPageLLMProviderBillingEditor` |
| 服务商流量卡片按数组解包（D22 回归） | `TestAdminPageLLMProviderTrafficCards` |
| **`tabMeta` 引用的 i18n 键必须存在（D28 回归）** | `TestAdminPageTabMetaKeysAreDefined` |
| 共享模型筛选按 `user_id` + `status` 生效（D24/D27 服务端侧） | `TestTokenBankAdminSharesFilterByUserAndStatus` |
| 定价表四条腿 CRUD（D25 服务端侧） | `TestTokenBankAdminPriceBookCRUD` |

`TestAdminPageTabMetaKeysAreDefined` 扫描**整个 bundle**（不只是 `admin-core.js`），因为自带 i18n 表的 tab（`user-rankings-tab.js`、`token-bank-tab.js`）在各自文件里定义标题键。第一版只扫 `admin-core.js`，当场把 `userRankingsTabTitle` 误报成悬空键 —— 这个误报本身就是「扫描范围必须等于真实定义范围」的证据。

#### 未覆盖 / 已知限制

- 转赠审计页按 `revocable` 决定是否渲染「冻结」按钮；已领取/已冻结的链接不显示按钮（不是禁用，是不渲染 —— 避免让操作者以为点得动）。
- `token_bank_settings` 里的 `require_review_before_online` / `require_verified_identity` 两个开关**本表单不编辑**（属 P0-8 的范围），只做原样透传。其中 `require_verified_identity` 的透传兜底为 `true`，与 `DefaultTokenBankSettings()` 一致 —— 若不知道就按「更严」处理，而不是悄悄放宽。
- `TokenBankShareModel.Enabled`（模型级启用/停用）目前只由 publish 链路同步，管理端**没有**单独开关。设计文档未把「管理员手动停用单个模型」列为验收项，故不补；操作者的等价手段是暂停整个分享或取出。
- 本阶段未跑 `tsc`/vitest（那是 `guiapp/frontend` 的门禁）；admin 页面是纯静态 JS，门禁为 `go test ./hubcenter/internal/httpapi/` 的 `TestAdminPage*`。

#### P0-7 结论：并入本阶段完成

P0-7「模型接入管理」的验收判据是**「评级改后新请求按新倍率结算」**，已由 P0-6「共享模型」子页 + P0-3 结算钩子共同满足，无需新增 UI。证据链：

1. **写**：`PUT /api/admin/token-bank/shares/{id}/models/{model}/tier`（`tokenBankTierRequest`）→ `repo.SetModelTier(...)` 落库 `token_bank_share_models.tier / tier_multiplier`。评级只接受 `low/mid/high/custom`；`low/mid/high` 走 `tokenBankTierDefaults` 的**规范对**（0.5/`token_bank_low`、1.0/`token_bank_mid`、2.0/`token_bank_high`），调用方**不能**传 `tier=high` 配 1.0 倍率。
2. **读**：`tokenBankProxySettler.TokenBankShareForPublish` 每次结算都 `repo.LoadShare` + `repo.ListModels`，**实时**取 `matched.Tier / matched.TierMultiplier`。**无进程内缓存**，所以改评级对下一个请求立即生效。
3. **不追溯**：倍率随当次 usage 行快照落库（`TokenBankShareSettlementView.TierMultiplier` 的注释即此约定），之后改评级**不会**重写已结算记录。
4. **UI**：P0-6「共享模型」子页已提供 low/mid/high 三个评级按钮（`gradeTokenBankModel`），当前评级高亮为 `btn-secondary`。

不在 P0-7 范围的两项（故不补）：provider chip 上的 Token Bank 标记属 §7.1 GUI 分享链路（P0-4）；`Enabled` 独立开关见「已知限制」。

---

### 17.10 ✅ P0-5 GUI Token 银行 tab

对比 §6.1 契约，客户端只读**自己**的分享/提取/汇总，全部经 Wails Go 方法转发（webview 拿不到 session token，不能自己 `fetch` HubCenter）。

#### 五处改动

| 层 | 文件 | 内容 |
| --- | --- | --- |
| Go 绑定 | `guiapp/token_bank_client.go` | 统一 `tokenBankClientDo(method, path, body)`：读 `LoadConfig()` → 校验 `RemoteHubCenterURL` / `SkillMarketSessionToken` → 附 `Authorization: Bearer` → 解 JSON；7 个方法（summary / shares / share models / pause / take out / withdrawals / withdraw）。 |
| 生成绑定 | `guiapp/frontend/wailsjs/go/main/App.{js,d.ts}` | 按既有手写模式补 7 条，`check_wails_bindings.go` 校验通过（18 个动态引用 + 1310 条生成绑定全部有原生方法）。 |
| 数据层 | `src/utils/hubcenterTokenBank.ts` | 类型 + 归一化（snake_case 优先、PascalCase 兜底）+ `formatCredits*` / `creditsToMicro` / `newWithdrawRequestID` / `isShareLive`。 |
| 面板 | `src/components/TokenBankPanel.tsx` | 汇总卡 → 分享卡（懒展开模型、暂停、取出）→ 提取记录；串行 `refresh()` 使登录错误只弹一次。 |
| 导航 | `SidebarNavRail(.Pieces).tsx` + `SidebarNavIcons.tsx` + `appLazyComponents.ts` + `App.tsx` | 扩展菜单加 `tokenbank` 项（三语文案）；`navTab === 'tokenbank'` 渲染面板。 |

#### 与契约对齐的要点

- **状态枚举（D27 同源）**：`isShareLive()` 只认 `'active'`，`'available'` 视为非活跃；面板在 `revoked` 时**不渲染**暂停开关。
- **§12 无提现**：面板只有「提取到本机」（手动 `manual=true`，可取**全部**可用余额）与分享卡上的操作，**没有**任何「转出到余额/提现」入口。
- **E6**：自动提取按 1/N 均摊，面板用 `describeAutoWithdraw()` 明示「自动提取按 N 个节点均摊，手动提取可取全部」，避免用户误以为自动提取被截断。

#### 样式归属（本次修正）

面板样式**不**放在组件级 `TokenBankPanel.css`（仓库无此先例），而是落到 manifest partial `src/styles/partials/61-token-bank.css`，由 `assemble-app-css.mjs` 组装进 `App.css`。块名按 BEM（`tbk-panel__head`、`tbk-pill--on`），颜色全部取自 `00-tokens.css`，**不留硬编码 fallback**（tokens 恒有定义，fallback 只会掩盖笔误）。

#### 测试与门禁

| 保证 | 用例 |
| --- | --- |
| 数据层归一化 / 格式化 / `isShareLive('available') === false`（D27 回归） | `src/utils/__tests__/hubcenterTokenBank.test.ts`（15） |
| 面板渲染、暂停、取出二次确认、手动取全部余额 | `src/components/__tests__/TokenBankPanel.test.tsx`（13） |
| 扩展菜单含 Token Bank、三语 label、点击切到 `tokenbank` | `src/components/layout/__tests__/SidebarNavRail.test.tsx`（+2） |
| 导航项在扩展菜单（非系统菜单）、图标/懒加载/渲染分支存在 | `scripts/check-main-ui-guards.mjs` |
| 面板 partial 已登记进 manifest、无组件级 CSS 导入、引用的 `--theme-*` 均已定义 | 同上（新增块） |

`check-main-ui-guards.mjs` 的 `--theme-*` 校验是本次新增的**反向断言**：把 `var(--theme-surface-muted)` 改成不存在的名字，门禁立即报 `uses undefined theme variables`。此前这类笔误的表现是「颜色静默回落」，与 D27/D28 同属静默失配，故补静态检查而非依赖肉眼。

#### 已知限制

- 面板**只读**自己的数据；`TokenBankWithdraw` 之外的写操作（改价、改费率）全在 admin 侧（P0-6）。
- 无分页：分享与提取记录一次拉全量。按 §6.1 当前契约服务端亦未提供这两个列表的 `offset`；若单账号分享数超过百级需补分页（与 P0-6 的 shares 分页器同法）。
- 「分享」入口（chip 按钮 + 分享对话框 + 探测进度）在本阶段未做；本 tab 当时只呈现已存在的分享。后续入口见 §17.11。

---

### 17.11 ✅ P1 与 D7（2026-10-01）

§9 的 1–8、10–12 与 D7 已落地。第 9 条沿用 P0-0b 的 hub Grant，本阶段没有新的扣款或购买钱包代码。

| # | 行为 | 判据 |
|---|---|---|
| 1 | 连续失败达到阈值后暂停一次，并调用 `SetSharePaused`、`SetTokenBankSharePaused`、`NoteShareModelError`。阈值 0 关闭。成功清零。计入传输失败、401/402/403/408/429/5xx 与非空错误；200–399 且无错误、以及 400/404/422 不计 | `TestTokenBankAutoPauseFiresOncePerStreak`、`TestTokenBankAutoPauseDisabledAtZero` |
| 2 | 桌面 `TokenBankRotateShareKey(shareID, apiURL, apiKey, protocol, keyFingerprint)` 走已有 `PUT /api/v1/token-bank/shares/{id}/key`。取消或空密钥不发请求。已撤销的分享不显示「更新密钥」。`TokenBankWithdraw` 仍是 3 个参数 | 面板 vitest：活分享带修剪后的密钥与非空指纹；取消不调用 |
| 3 | 发布时把 owner / 档位 / 倍率 / 显示名记在成员上。分享行已经不在时，用这份快照和 `TokenBankFallbackPrice` 结算。价格失败则不入账。分享行还在时仍以数据库为准 | `TestSettleTokenBankUsageUsesInFlightSnapshotWhenShareIsGone`、`TestSettleTokenBankUsageDropsInFlightWhenFallbackPriceFails` |
| 4 | 同一 API 已有未撤销分享时，探测横幅给出未加入的模型数。第一次分享（已分享列表为空）不显示。列表加载失败按空列表处理 | `modelsMissingFromShare` 单测；对话框与 chip vitest |
| 5 | `HubBelongsToUser` 用邮箱把 hub 连到 `sm_users`。命中则跳过结算；查询出错仍结算；缺表视为非本人 | `TestSettleTokenBankUsageSkipsSelfUse`、`TestSettleTokenBankUsagePaysWhenSelfUseLookupFails`、`TestHubBelongsToUser` |
| 6 | 只对 `tbk_` 成员、且窗口样本不少于 5 时，把权重乘上 `round(100 * 成功率)`，下限 1。平均延迟超过 5000ms 时，这个百分数再减半。冷成员保持原权重。`DispatchWeight > 1` 仍直接走加权，原有 WRR 用例保持原语义 | `TestEffectiveDispatchWeightScalesTokenBankMembersOnly`、`TestOrderArrayMembersUsesWeight` |
| 7 | 用量插入成功后累加已用 token，并按上海时区的当日 / 当月合计比较上限。0 表示不限。当笔仍然入账。重放不报 `CapHit`、也不再暂停。日上限优先于月上限。暂停发生在事务外，原因是 `token cap: daily` 或 `token cap: monthly` | `TestSettleTokenBankUsageDailyCapCreditsThenReportsHit`、`TestSettleTokenBankUsageMonthlyCap`、`TestSettleTokenBankUsageDailyCapWinsOverAnomaly`、`TestPauseTokenBankShareRecordsTheCap`、`TestSettleTokenBankUsagePausesOnCapHitOnce` |
| 8 | 前 7 日（不含今天）合计大于 0，且 `today * 7 > prev * 5` 时记 `anomaly`，走同一条暂停。硬熔断优先 | `TestSettleTokenBankUsageAnomaly`。日界用上海零点；昨天取 `dayStart` 往前一小时的 UTC |
| 9 | 助手消耗的是 hub 上 `Source=token_bank` 的 permanent Grant。结算入账、提取、Grant、调用消耗这条链路在 P0-0b 已经接通 | 本阶段无新增代码，也未接 `CreditsService.Debit` |
| 10 | `GET /api/v1/token-bank/usage/daily?days=`。缺省与非法为 30，大于 366 钳到 366。按所有者隔离，按 UTC 日期分组，Top 模型按净额取 10 条 | `TestTokenBankUsageDailyIsOwnerScoped`、`TestTokenBankUsageDailyAndCSV` |
| 11 | `GET /api/v1/token-bank/usage.csv`，`text/csv`，列 `created_at,request_id,model,input_tokens,output_tokens,gross_micro,fee_micro,net_micro,charged_micro`，最多 5000 行。导出行类型是 `TokenBankUsageExportRow` | `TestTokenBankUsageExportCapsAt5000`、`TestTokenBankUsageDailyAndCSV` |
| 12 | `provider_denylist` 按主机名精确匹配或子域后缀匹配。空主机与无法解析的 URL 不拦截。`notopenai.com` 不命中 `openai.com`。未列入名单的主机可以创建分享。校验发生在解密之前，坏密文也返回 403 `provider_denied`。保存设置时去掉空白项 | `TestTokenBankProviderDeniedMatchesHostAndSubdomain`、`TestTokenBankCreateShareRejectsDenylistedProvider`、`TestValidateTokenBankSettingsDropsBlankDenylistEntries` |
| D7 | 见 §17.4。生产写池仍是 1 | `TestConnectionPragmasApplyToEveryPooledConnection` |

本轮还跑过的回归：`go test ./hubcenter/internal/store/sqlite/ -run "TestSettleTokenBankUsage|TestTokenBank"`、`go test ./hubcenter/internal/httpapi/ -run "TestTokenBank|TestCreditsToMicro|TestAdminPage"`、`go test ./hubcenter/internal/llmservice/ -run "TestTokenBankAutoPause|TestEffectiveDispatchWeight|TestSettleTokenBankUsage|TestOrderArrayMembersUsesWeight"`、`go test ./hubcenter/internal/app/ -run "TestPauseTokenBankShare|TestNewTokenBankProxySettler|TestTokenBankProxySettler"`。前端 vitest 四个文件 66 项通过（数据层 28、面板 22、对话框 14、chip 2）。`check_wails_bindings.go` 通过（1318 条生成绑定）。`61-token-bank.css` 已组装进 `App.css`，基线已更新，CSS 纪律检查通过。

桌面窗口没有点过。GUI 二进制没有重新打包。`tsc --noEmit` 在 `codePreviewPresentationPreservation.test.ts` 上报了 3 个既有的 `taskResult` 类型错误，Token Bank 文件没有类型错误。`check-main-ui-guards.mjs` 因 `AIAssistantPanel.tsx` 超过 6800 行失败，该文件不在本次改动里。更广的 proxy 流式套件没有重跑；`recordProxyStreamUsage` 的已知调用点传入的是 nil。

---

## 18. v8 新增：HA 同步边界表（哪些状态跨节点共享）

> 这一节是 v8 review（R2）的产物。起因是 §8 第 6 条笼统写着"Token 银行数据要走 ha oplog 同步"，
> 而实现只同步了账本——**文档与实现已经不是一回事**，下一个接手的人会按文档去做然后踩空。

### 18.1 权威查法（先看这个，别信任何文档章节）

`hubcenter/internal/ha/service.go` 的 **`isSupportedEntityType`** 就是"哪些状态跨节点共享"的
唯一权威清单（20 行 switch）。任何涉及跨节点的设计问题，**先查这张表**。
Token Bank 在里面只有一行：

```go
EntityTokenBankLedgerBatch = "token_bank_ledger_batch"   // service.go:50
EntityTokenBankPriceBook   = "token_bank_price_book"     // service.go:60（v8 新增）
                                                          // :2022 isSupportedEntityType
                                                          // :967 admin 同步分类 "token_bank"
```

> ⚠️ **这张表只覆盖"独立实体"。** 大量状态是**搭 `system_setting` 的车**同步的
> （`EntitySystemSetting` 一行，key/value 全量复制）——最典型的就是 LLM registry（§18.3）。
> 判断"某状态是否跨节点共享"时，**不能只看这张表里有没有它的名字**，还要看它是不是存在
> `system_settings` 里。`grep -rn "该实体的表名" hubcenter/internal/ha/` 零命中 ≠ 不同步。

### 18.2 逐表边界

| 表 / 状态 | 同步 | 后果与兜底 |
|---|---|---|
| `token_bank_ledger`（经 `token_bank_ledger_batch`） | ✅ 异步批量（200 条 / 15s） | 余额能到各节点；**但有 F1 超发窗口（§14.9）** |
| `system_settings`（`token_bank_settings`：费率、上限、denylist） | ✅ `EntitySystemSetting` | 全局钱规则一致 ✓ |
| `hub_user_links`（E6 的 N 依赖它） | ✅ `EntityHubUserLink` | 各节点算出的 N 一致 ✓ |
| `llm_usage_records`（经 `llm_usage_batch`） | ✅ | 消费者侧流水可汇总 ✓ |
| **`token_bank_price_book`** | ✅ **已补**（`EntityTokenBankPriceBook`，v8） | ✅ 钱规则跨节点一致 ✓ |
| **`token_bank_withdrawals`** | ❌ | 🔴 幂等/重放凭据是**本地**的：hub 换节点重试用同一 `request_id`（§14.5 重新下发）在新节点查不到 ⇒ 判为"没提过" ⇒ 重复扣账 + 重复建 grant。**已由 clearing node 吸收**（只在一处写，见 §18.4 R2-b） |
| **`token_bank_usage`** | ❌ | 🟠 明细与 CSV 在不同节点看到不同数据；§14.6 第一条对账（`Σ usage == SUM(ledger.earned)`）**只能单节点成立** |
| **`token_bank_shares` / `_models`** | ❌ | 🟠 见 §18.3。附带正效应：密钥不扩散，§13 原「密钥扩散」风险**改判为不成立** |
| **`token_bank_share_requests`**（24h 幂等键） | ❌ | 🟡 幂等键只在创建节点有效；重复提交打到另一节点会建出第二个 share（`idx_tbk_shares_dedup` 也是本地唯一键，挡不住跨节点） |
| **`credit_share_links`** | ❌ | ✅ **刻意不同步**，由 §8 第 9 条的 `origin_node_id` 裁决兜住，设计自洽 |
| **`llm_service_registry`**（providers / arrays / service groups，含全部 `tbk_` 共享池成员） | ✅ **搭 `EntitySystemSetting` 的车**（单键整 blob） | ✅ **共享池不是单节点的**，任何节点都能调度，见 §18.3。⚠️ 但整 blob 覆盖 ⇒ 跨节点并发写会丢更新，见 §18.4 **R2-d** |

### 18.3 ✅ R2-c 已查实：registry **是同步的**，共享池不是"单节点"的

v8 写这一节时的推论是错的（"没有 llm_provider 实体 ⇒ 共享池只在创建节点存在"）。
**追完装配路径后推翻**：`llm_provider` 确实不在 HA 实体清单里，但它**根本不是独立表**——
整个 registry 是 `system_settings` 里的一个 key，搭 `EntitySystemSetting` 的车同步。

**完整链路（逐跳核实，不是推测）**：

| # | 位置 | 做了什么 |
|---|---|---|
| 1 | `llmservice/registry.go:21` | `RegistrySettingKey = "llm_service_registry"`；整份 `Registry`（providers + arrays + service groups）序列化成**一个 JSON** 存这一个 key |
| 2 | `app/bootstrap.go:103` | `systemSettings = &haSystemSettings{inner: st.System, sync: haSvc}` —— settings 仓库被 HA 包装 |
| 3 | `app/ha_wrappers.go:30` | `haSystemSettings.Set` = 先落本地，再 `haSvc.AppendSystemSetting(key, value)` |
| 4 | `ha/service.go:3000` | `AppendSystemSetting` → `AppendUpsert(EntitySystemSetting, …)`。**无 key 排除列表**，registry 与其它设置一视同仁 |
| 5 | `ha/service.go:2537` | 对端 pull → `applySystemSettingOp` → 写本地 settings |
| 6 | `ha/service.go:2571` | 且 `payload.Key == llmservice.RegistrySettingKey` 时调 `llmRegistryCacheInvalidator` |
| 7 | `app/llm_init.go:91` | 该 invalidator = `llmSvc.InvalidateCache`，**绕过 `LoadRegistry` 的 30s TTL** 强制重读 |

**结论**：任何节点上新建的 `tbk_` 共享池成员，都会随 registry blob 到达其它节点并被立即看到。
调度侧再往下走一步也成立——

- 共享池成员的**凭据就在 registry 里**（`APIKey` + `TokenBankExtraKeys`，`token_bank_publish.go:449-470`），
  不依赖未同步的 `token_bank_shares` 表 ⇒ 别的节点拿到 blob 就有完整的出网能力；
- `token_bank_publish.go` 全文**没有设置 `AllowedNodeIDs`** ⇒ `ProviderAllowedOnNode`（空名单 = 全节点允许）为真
  ⇒ 走 `upstream_hop.go:97/111` 的**本地直连**分支，不会去 hop。
  （`upstream_hop` 只对管理员**显式配了节点白名单**的 provider 生效。）

所以：**一个在 hc-1 上发布的共享池，hc-2 / hc-3 都能调度，都能正确结算**（owner 信息挂在 provider 上一起同步）。
§3.4「三阵列」与 §8「HA 一致性」**不需要重写**，v8 里那句"该节点宕机 ⇒ 这批共享模型整体下线"是错的——
registry 是全量的，宕机只影响该节点持有的 `token_bank_shares` 行（见下）。

**剩下为真的部分**：`token_bank_shares` / `_models` 行**仍然不同步**，于是"记录"和"可调度性"分居两地：

- 成员（可调度性）在所有节点；share 行（owner 的列表视图、可见性、备用密钥）只在创建节点；
- ⇒ 「我的共享」列表在别的节点是空的 —— 与 G5/G6 同一族，但**共享池这条目前没接路由**，
  因为共享池的创建/撤下不是"只有一次"的钱操作（不写账本）。要不要一起收敛，见 R2-d。

**方法论（这次踩的坑，写下来防止再犯）**：
"HA 实体清单里没有 X" **推不出** "X 不同步"。X 可能是某个已同步实体的**一部分**
（system_settings 的 key、快照的字段）。判同步与否要追**它最终落在哪个存储上**，
而不是在实体常量表里搜它的名字。

### 18.4 待办登记

| # | 事项 | 类型 | 状态 |
|---|---|---|---|
| F1 | 跨节点提取超发窗口 | 设计 + 代码 | ✅ **已落地**：所有"只有一次"的钱操作（建链接 / 提取 / 转赠结算）路由到配置的 **clearing node**（`token_bank_clearing_route.go`）。见下面 F1-解 |
| R2-a | `token_bank_price_book` 是否补同步 | 设计 | ✅ **已落地**：新增 HA 实体 `token_bank_price_book`，store 层 sink → `AppendTokenBankPriceRule` / `...Delete`，admin 改价自动扩散。见下面 F1-解 |
| R2-b | `token_bank_withdrawals` 是否补同步 | 设计 | ✅ **不再需要**：提取已收敛到 clearing node，该表只在一处写，重放也路由到同一处。**不需要补同步** |
| R2-c | 确认 provider 是否真不同步；若是，补"共享池容灾 / admin 操作路由"结论 | 核实 + 设计 | ✅ **已查实：registry 经 `system_setting` 同步，共享池全节点可调度**（§18.3）。**不需要**补共享池容灾或 admin 操作路由 |
| R6 | `RebuildAccount` 补启动 + 定时校验 | 代码 | ✅ **已落地**（`FindDriftedAccounts` + `runTokenBankCacheReconcile`，一次 SQL 找出漂移账号，启动一遍 + 每 30 分钟一遍，逐条打日志） |
| R7 | `manual` 凭据收敛 | 代码 | ✅ **已落地**：hub 专用端点**不再接受** `manual`（结构体字段已删，硬编码 false）；manual=true 只能来自带用户会话的端点 |
| F2 | `SettleClaimedGift` 幂等判定只看 receiver 那一行，而 `credit_share_links` 不在同步表内 ⇒ 复制来的 `rcv_` 行先到的节点会 return "已结算"，本节点的 link status 却永远停在 `claimed` | 设计 + 代码 | ✅ **已落地**：建链接也路由到 clearing node ⇒ 链接只存在于一处 ⇒ 结算必在该处执行。**不需要补同步，也不需要在别处读 link**。且核实发现更严重的一面：接收方在非 origin 节点提取时 `GiftLinkByID` 直接 not found，转赠积分**根本提不出来** |
| F3 | token bank 全部写路径用 deferred `BeginTx`，而 skillmarket 的钱路径一律 `BeginImmediate` | 代码 | ✅ **已落地（2026-10-02）**：`config.Validate` **拒绝任何 ≠1 的 `max_write_open_conns`**（fail-fast，不是静默钳制）——该值调大即"两笔交错事务都通过同一余额校验后各扣一次"，是 F1 超发的**单节点孪生**。现有部署配置全部已是 1，无破坏。注释写明：要扩大写池，先把 token bank 迁到 `BeginImmediate` |
| — | `countHubsForUser` 吞掉所有错误返回 1 | 代码 | ✅ **已修**：只有"缺表"才降级为 1，其余错误上抛（N 是 `AutoWithdrawLimitMicro` 的除数，降级为 1 = 自动提取可抽干全部余额） |
| 🔴 **G1** | **hub 回写 grant 绑定失败**（v8 复查新发现） | 代码 | ✅ **已修**：`POST /api/hubs/{id}/token-bank/grants`（`BindGrantID` / `ReissueGrantID`）已接路由（认证之后、写库之前）
| 🔴 **G2** | **领取落地页 404** | 代码 | ✅ **已修**：`GET /c/{code}` 在 code 格式校验之后接路由。反向验证复现的症状就是「分享链接不存在或已失效」
| 🔴 **G3** | **预览接口 404** | 代码 | ✅ **已修**：`GET /api/v1/credits/share-links/{code}/preview`（桌面深链预览走它）已接路由 |
| 🟠 **G4** | **撤销不可用（用户 + admin）** | 代码 | ✅ **已修**：用户 `POST /api/v1/credits/share-links/{id}/revoke`（会话认证之后）与 admin `POST /api/admin/token-bank/credit-shares/{id}/revoke` 都已接路由 |
| 🟠 **G5** | **我的分享列表空** | 代码 | ✅ **已修**：`GET /api/v1/credits/share-links` 已接路由（反向验证复现：`200 {"share_links":[]}`） |
| 🟠 **G6** | **我的提取记录空** | 代码 | ✅ **已修**：`GET /api/v1/token-bank/credits/withdrawals` 已接路由（反向验证复现：`200 {"withdrawals":[]}`） |
| 🟠 **G7** | **hub 对账返回 0** | 代码 | ✅ **已修**：`POST /api/hubs/{id}/token-bank/reconcile` → `SumWithdrawalMicro` 已接路由 |
| 🟠 **G8** | **admin 审计列表空** | 代码 | ✅ **已修**：`GET /api/admin/token-bank/credit-shares` 已接路由 |
| 🟡 **G9** | **存量链接迁移缺口** | 设计 | 🟠 **待办（上线动作，不是代码）**：配置 `clearing_node_id` **之前**建的链接仍留在原节点。配置之后，它们的撤销/结算被路由到 clearing ⇒ not found ⇒ 老链接既不能撤销也不能结算，积分冻结在原处。**已决定不做代码迁移**，改为上线前人工核查——步骤见 **§18.8** |
| 🟡 **G10** | **转发丢失响应头** | 代码 | ✅ **已修**：`tokenBankProxyToPeer` 现在透传对端全部响应头（只跳过 `Content-Length` / `Transfer-Encoding` / `Connection` 等 hop-by-hop 与分帧头）。落地页的 CSP / `no-store` / `nosniff` 不再丢失 |
| 🟡 **R2-c** | `llm_provider` 不在 HA 实体清单 | 核实 | ✅ **已追完**：清单里确实没有，但 registry 整份存在 `system_settings["llm_service_registry"]` 里，搭 `EntitySystemSetting` 同步 + apply 时 `InvalidateCache` ⇒ **共享池任何节点都能调度**（§18.3）。v8 的"单节点"结论作废 |
| 🔴 **R2-d**（R2-c 追出来的新缺口） | `llm_service_registry` 是**单键整 blob 覆盖**，跨节点并发写会丢更新 | 设计 | 🟡 **部分落地（第 1 条已做）**：apply 侧已加 `UpdatedAt` fence（`ha/llm_registry_fence.go`），滞后副本不再回滚更新的本地 registry，真并发仍丢一半。反向验证过：停用 fence 后「滞后 op 覆盖 fresher 本地值」精确复现。**写侧进程内锁已全量审计**（见 §18.9 末尾）。详见 §18.9 |
| 🔴 **R2-e**（写侧审计新发现） | autopause 触发条件是 `failures == threshold` **精确等于**且 pause 回调 `_ =` 吞错 ⇒ 瞬时写库失败后计数只涨不归零，**该 share 的自动暂停永久失效**（坏 share 永远留在调度池里持续烧钱） | 代码 | ✅ **已修（2026-10-02）**：改为 `failures >= threshold` 触发——继续失败的唯一现实原因就是 pause 没落库，重试即恢复路径；pause 落地后成员停派发，notes 自然停止。回调吞错改为 `log.Printf` 可见。反向验证过 |
| 🟡 **T1**（publish 测试盲区） | httpapi 层 publish 路径无法替 stub，"改 Key / 改可见性后注册表是否收到新值"无测试覆盖 | 测试 | ✅ **已闭环（2026-10-02）**：publisher 已是接口 `tokenBankPublishView`（可 stub）；换 Key 已有 lifecycle 测试断言 `provider.APIKey` 到达 registry；**可见性→registry 原本零覆盖**，已补 `TestTokenBankSetShareVisibilityReachesRegistry`（private+audience / 回 public 清 audiences / 无受众的 private 拒绝，全走真 registry），反向验证过（停掉 specs 的 visibility 透传 ⇒ 测试红，失败即"registry 保留旧 scope"） |

### 18.6 F1/R2-b/F2 的解：clearing node（已落地）

三条看起来不同的问题，根因是同一个：**"只有一次"语义的操作被允许在多个节点各自执行**。
解法不是给每张表补同步，而是**让这些操作只在一处发生**。

**配置**：`token_bank_settings.clearing_node_id`（在共享 settings blob 里，本身跨节点同步）。

**路由到该节点的操作**（`token_bank_clearing_route.go`）：

| 操作 | 为什么必须在一处 |
|---|---|
| 建分享链接 | 它写 `credit_share_links`（不同步）+ 冻结账本。放一处 ⇒ 链接只存在一处 ⇒ 后面的结算一定能读到它 |
| 提取（self） | 额度校验读本地账本，`tokenBankLedgerSyncBuffer` 200 条/15s 才 flush ⇒ 两节点可各放行一次全额 |
| 转赠结算 | 由接收方提取触发（`settleGiftForWithdraw`），必须和链接在同一处 |
| 领取 claim | **本来就**路由到 `origin_node_id`；链接现在建在 clearing node ⇒ origin == clearing，天然一致，无需改动 |

**为什么不选别的**：

- **按 user_id 哈希**：节点增减会漂移，而 `token_bank_withdrawals` 不同步 ⇒ 漂移后重放找不到归属，又回到 R2-b。要它就得再补一个同步，两个改动换一个。
- **"写完账本后等复制确认"**：只能把窗口从 15s 缩到毫秒，**不解决并发**——两个不同 request_id 各写各的账本行，`INSERT OR IGNORE` 都成功。没有 leader/共识时，串行化只能靠单点。
- **补 `credit_share_links` 同步**：它和 `token_bank_ledger` 是两种复制语义（有状态行 vs append-only），为一张低频表引入前者不划算。

**两条纪律**：

1. **默认关闭**（配置为空 = 不路由）。单节点无需串行化；未配置的集群保持现状，不会因为运维没填这项就把每次提取都打失败。
2. **clearing node 不可达 ⇒ 拒绝（503），绝不本地放行**。本地放行就是超发本身，只是披着"高可用"的外衣。测试
   `TestTokenBankWithdrawRefusesWhenClearingUnreachable` 同时断言了 503 **和余额未被扣**。

**滚动升级**：转发用的 header 值**保留旧名**（`X-Token-Bank-Claim-Hop` 等）。三节点逐个升级期间新旧版本并存，
改名会被旧节点静默忽略，它会因此本地执行——正好是路由要防的那件事。

### 18.7 🔴 v8 复查：写收敛了，读没有（G1–G10）

上面这个方案有个**只在事后才看得见的漏洞**：它把**写**收敛到了一处，却没有把**读**一起收敛，
而底下的表（`credit_share_links`、`token_bank_withdrawals`）**并不参与 HA 同步**。
于是一旦真的配置了 `clearing_node_id`，真实效果是：

```
建链接   → 路由到 clearing ⇒ link 行只写在 hc-1
看列表   → 落到 hc-2 ⇒ hc-2 上没有这张表的数据 ⇒ 空
开落地页 → 落到 hc-3 ⇒ not found ⇒ 显示「链接不存在」
hub 回绑 grant → 落到 hc-2 ⇒ hc-2 上没有这条 withdrawal ⇒ 404，grant_id 永远记不上
```

**这不是理论风险，是配置即触发的功能倒退**，而且因为它只在"已配置 clearing node"时出现，
单节点开发环境和默认集群都测不出来——测试里三个入口全绿，正是因为它们断言的就是"有没有转发"。

**根因一句话**：把"只有一次"的语义收敛到单点，同时要求**所有依赖该数据的操作也去那个点**，
否则单点写 + 分布式读 = 读到的永远是空的那一半。

| ~~A / B~~ | **已选定 A** | — | ✅ **已落地**：G1–G8 八个入口全部接读/写路由（认证之后、动库之前），G10 响应头透传已修。代价已接受：clearing node 现在是**整个分享功能的单点**——它不可达时，不只是提取，连「看我的列表」「打开落地页」都会返回 503。**B（补同步）降级为可选增强**：若将来 clearing 成为瓶颈或该单点不可接受，再补 `credit_share_links` / `token_bank_withdrawals` 同步让读回到本地（安全性论证见下） |

**B 是否安全**——这里有个前提变了，值得写下来：§18 反复警告"有状态行整行覆盖会丢更新"，
那个警告的前提是**多个节点都能写同一行**。而 clearing node 落地后，**这两张表只有一个写者**（写全被路由过去了），
其余节点只持有只读副本。所以整行覆盖同步在这里**不会**丢更新——警告依然成立，只是它的前提不再满足。

**已选定 A**（读写全路由），八个入口全部接上，G10 已修。选它的理由：改动局部、可立即验证，
且与已定的"钱操作正确性优先于可用性"是同一条原则。`B` 降级为可选增强，论证保留在下面。

**反向验证顺带成了证据**。停掉路由后跑新测试，失败信息逐条就是上面预测的线上症状：

```
落地页    404  「分享链接不存在或已失效」
预览/撤销 404  {"code":"not_found"}
我的列表  200  {"share_links":[]}      ← 最阴的一种：成功状态码 + 空数据
提取记录  200  {"withdrawals":[]}      ← 同上
hub 回绑  404  {"code":"withdrawal_not_found"}
```

注意中间两条：**它们返回 200**。监控系统看状态码不会报警，只有用户会发现"我刚建的东西不见了"。
这类"写收敛了但读没有"的缺口不会以报错的形式出现，所以也等不到告警。

**已知代价（接受）**：clearing node 不可达时，不只是提取失败，连查看列表和打开落地页都会 503。
这与 §18.6 第 2 条纪律一致——本地兜底就是错的那一半，只是换了个接口。若将来这个单点不可接受，
再走 `B`：新增两个 HA 实体让读回到本地。**B 是安全的**（单写者前提下整行覆盖不会丢更新，见下），
只是工作量约为 A 的两倍。

### 18.8 🟡 G9：启用 clearing node 前的存量核查（上线动作，不可跳过）

已确认：**G9 不做代码迁移**，改为上线前人工核查。理由是当前 `clearing_node_id` 还没配，
存量链接大概率为零 —— 但"大概率"不是"确认过"，这一步不能省。

**在填 `clearing_node_id` 之前，三台节点上各跑一次**：

```sql
-- 1. 存量转赠链接（会直接踩 G9）
SELECT status, COUNT(*) FROM credit_share_links GROUP BY status;
-- 2. 存量提取单（重放/对账会踩）
SELECT status, COUNT(*) FROM token_bank_withdrawals GROUP BY status;
-- 3. 存量共享池（这台不同步，每台都要单独看）
SELECT status, COUNT(*) FROM token_bank_shares GROUP BY status;
```

- 三台全为 0（或只剩已终态的行）⇒ 直接启用，G9 不复现。
- 有任何非终态行 ⇒ **先处理再启用**。否则这些链接被路由到 clearing 判 not found，
  表现就是 §18.4 G9 原文：既不能撤销也不能结算，积分冻结在原处。

处理方式（按成本排序）：

1. 让持有方在启用前撤销 / 结算掉 —— 最干净，但要联系用户；
2. 把 `clearing_node_id` **设为持有这些行的那台节点** —— 存量天然落在正确的地方，零迁移；
3. 启用后做一次数据迁移（把行搬进 clearing 节点的库）—— 当初判定"不划算"的那条路，
   只有存量多到 1 / 2 都不可行时才走。

> 第 3 张表（共享池）与 clearing **目前无关**：共享池的创建/撤下没接路由（§18.3），
> 所以它的存量只会造成"我的共享列表在别的节点看不到"，不碰钱。列在这里是为了**一次查完**，别事后补查。

### 18.9 🔴 R2-d：registry 是单键整 blob，跨节点并发写会丢更新

追 R2-c 时顺带挖出来的，与 R2-c 是两件事：R2-c 问"能不能调度"（答：能），
R2-d 问"写会不会丢"（答：会）。

**为什么丢**：

- 整份 `Registry` 序列化成**一个 JSON** 存在 `system_settings["llm_service_registry"]`；
  `MutateRegistry`（`registry.go:155`）= load → clone → 改 → **全量 persist**。没有字段级合并。
- 串行化只有两把**进程内**锁：`Service.writeMu`（`registry.go:75`）与 `tokenBankRouteMu`
  （`token_bank_publish.go:22`）。跨节点没有任何协调。
- `applySystemSettingOp`（`service.go:2537`）只对三类 key 做了"新者胜" fence：
  monitor lease、official class head、member health 前缀。**registry key 没有 fence**
  ⇒ 谁的 op 后 apply 谁赢，另一个节点的变更被静默丢弃。

**谁在写（分布在所有节点）**：

| 写入点 | 发生节点 |
|---|---|
| `TokenBankCreateShare` / `TakeOutShare` / `SetShareVisibility` / `Add` `RemoveShareKey` | 任意节点（**未**路由到 clearing） |
| autopause → `SetTokenBankSharePaused`（`bootstrap.go:311-336`） | **每个节点**（本地代理连续失败就触发） |
| 管理员 provider CRUD / array 编辑 / 默认服务组切换 / 暂停 | 任意节点 |

**最咬人的场景**：

1. hc-1 发布共享池 ⇒ blob A（含新 member）；
2. 同时 hc-2 因本地调用失败 autopause 了另一个 member ⇒ blob B（基于**同一份旧 blob**，不含新 member）；
3. 互相 apply ⇒ 后到者胜 ⇒ 若 B 后到，**新发布的 member 从所有节点的 registry 里消失**；
4. **不会自愈**：`token_bank_shares` 行仍在 hc-1（该表不同步），hc-1 认为"已发布"；
   且**没有启动期全量重发布** —— `republishTokenBankShare` 只在显式操作时触发
   （创建 / 改 key / 改可见性 / 取出）；
5. 用户侧症状：**「我的共享显示已发布，但谁都调不到」**，直到他手动改一次才被 republish 救回。
   和 G5/G6 一样是 **200 + 空/错数据**，监控看不出来。

**与已定的 A 的关系**：A 只收敛了**钱**的写（账本、链接、提取、结算）。registry 的写没收敛，
clearing node 目前完全不管它。

**三条改法**：

1. **最小止血：给 registry key 加 `UpdatedAt` fence**。`persistRegistry`（`registry.go:137`）已经写了
   `reg.UpdatedAt`，apply 侧解析 payload 比对即可 —— 照抄 `officialClassHeadFence` 的手法，
   滞后副本不覆盖 fresher 本地值。挡掉"乱序滞后副本"，**不解决真并发**（两边都新时仍丢一半）。
2. **真修 A′：把 registry 的写也收敛到 clearing**（复用 `token_bank_clearing_route.go`）。
   代价是共享池的创建/撤下/暂停也变成 clearing 单点，**放大** §18.7 已接受的单点问题 ——
   在 clearing 的可用性还没被实际运维验证之前，不建议做。
3. **真修 B′：字段级合并**（按 provider id / group id 逐实体 last-writer-wins，而不是整 blob 覆盖）。
   正确性最好，工作量最大，且要把 registry 从 `system_setting` 里拎出来单独立一个 HA 实体。

**建议顺序**：先做 1（十几行 + 一个测试），把窗口从"任意乱序"缩到"真正并发的那一瞬"；
2 / 3 是否做，等 clearing node 上线跑一段再说 —— 如果 A 的单点代价被证明可接受，
把 registry 也收敛过去是同一条原则的自然延伸。

**✅ 第 1 条已落地（2026-10-02）**：`ha/llm_registry_fence.go` 新增 `llmRegistryFence`，
`applySystemSettingOp` 在写 registry key 前先比对 `updated_at`——incoming 严格更旧 ⇒ 跳过
（且**不**触发 cache invalidation，因为本地什么都没变）。语义上刻意收窄，照抄
`llmProviderMonitorLeaseFence` 的纪律：

| 情形 | 行为 |
|---|---|
| incoming 严格更旧 | **fence**：保留本地，cursor 照常推进（返回 nil，不会重试打转） |
| incoming 更新 / 本地为空 / 本地无时间戳（legacy） | 应用 |
| incoming 无法解析（损坏） | **照常应用**——损坏的 registry 不能阻塞恢复 |
| 时间戳相等 | 应用（同一次写的回声，幂等） |

两个刻意的取舍，写下来防止将来当 bug 修掉：

1. **比时间不比内容**：fence 只拒绝"严格更旧"，同刻不同内容照常应用。真实并发（两边都新）仍丢一半
   ——这是 fence 的定义边界，不是缺陷；修它要走上面的第 2/3 条。
2. **时钟偏斜换确定性**：节点间比墙钟，偏斜节点的"真·更晚"写可能输给时间戳更早的本地值。
   不 fence 时赢家由 apply 顺序决定，并不更好；fence 只是让赢家变成确定的。

测试 5 个（`system_setting_apply_test.go`）：fence 生效（且**不断言** invalidation，应为 0）、
更新应用（invalidation = 1）、损坏照常应用、legacy 无时间戳收敛、**亚秒精度**（`…00.5Z` 与 `…00Z`
按时间序不按字典序——time.Time 解析，不能用 class head 那种字符串比较）。
反向验证：临时停用 fence ⇒ stale 测试红，失败信息即 R2-d 的原始症状
（"stale registry op rolled the fresher local registry backwards"）。

**写侧进程内锁全量审计（2026-10-02，fence 的另一半）**：fence 只保 apply 侧；写入侧若不持锁，
本节点的并发写自己就先丢。逐点核实结论：**全部持锁，无缺口**——

- `registry.go` 内 15 处绕过 `MutateRegistry` 的直写 `persistRegistry`（Agent/Provider/ServiceGroup
  CRUD、`SaveRegistry`）全部在函数头 `defer s.lockRegistryWrite()()` 保护下；
- `provider_array.go`（`DeleteProviderArray`）与 `provider_array_import.go`（`importProviderArrays`）
  两处直写同样持锁；
- `MutateRegistry` 本身 load→clone→mutate→persist 整段持 `writeMu`，clone 在 mutator 之前，
  cached 指针不会被 mutator 摸到。

审计顺带修的两处（见 §18.4 R2-e 与下述）：

1. **autopause 一次性窗口**（真 bug）：触发条件 `failures != threshold` 精确等于 +
   pause 回调 `_ =` 吞错 ⇒ 瞬时写库失败后该 share 永远不会被自动暂停。
   已改为 `failures >= threshold` 重试语义 + 回调错误打日志。
   注意旧测试 `FiresOncePerStreak` 断言的"超过阈值不再触发"恰恰是 bug 的镜像——
   持续失败只可能发生在 pause 没落库时，此时重试才是对的；"只触发一次"由
   pause 落地后停止派发保证，不需要精确匹配保证。
2. **`GetProvider` / `FindServiceGroupForModel` 返回缓存内切片的浅拷贝**：`ProviderReferences`
   的注释明确"返回深拷贝防缓存污染"，这两个没做到——调用方改返回值的 slice/map
   会直接腐蚀缓存（下一次 persist 还会把腐蚀写回盘）。已改深拷贝（`registry_deep_copy_test.go`
   用 ModelMap/ProviderIDs 变异探针钉住）。

审计过但干净的：`token_bank_group.go` / `token_bank_p2.go`（canary、audience、key 轮换都
遵守"不共享 registry 切片"约定）/ `token_bank_caps.go` / `token_bank_arrays.go`。

### 18.5 一条方法论（写在这里防止再犯）

**"HA 同步是幂等的"不等于"HA 下是并发安全的"。**
append-only 流水天然给出前者（确定性主键 + `INSERT OR IGNORE`），
**不给出后者**（两个节点各自放行一笔不同的写入）。
凡在文档里见到"天然幂等""回放不会重复"这类论断，
都要追问一句：**复制是同步的还是异步的？窗口有多长？窗口内会不会各自放行？**
本次 F1 正是 §17.3 只回答了前者、没问后者而漏掉的。

