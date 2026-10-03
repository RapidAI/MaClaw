# Token 银行设计文档 v7 —— review 报告（2026-10-02）

> 被审对象：`docs/design/token-bank-design-zh.md`（v7，1972 行）
> 方法：通读全文 + 抽查代码引用（`hubcenter/`、`hub/`、`corelib/`）
> 结论：**设计主线正确，账本改造（E1/E2）与钱规则收敛是对的**；但文档已明显落后于代码，
> 且 HA（三节点）下有一处**尚未被任何一版识别的超发窗口**。
> 分 4 档：🔴 必修 / 🟠 应修 / 🟡 一致性 / ✅ 核实为真

---

## 0. 一句话摘要

| 类别 | 条数 | 代表问题 |
|---|---|---|
| 🔴 必修 | 4 | 跨节点提取可超发（R1）、价目表与提取凭据不同步（R2）、hub 侧已实现但文档仍写"零代码"（R3）、§8 仍在讲 Debit/Credit（R4） |
| 🟠 应修 | 4 | unfreeze 幂等约定过时（R5）、rebuild 无调用点（R6）、`manual` 可自报抽干（R7）、API/文件清单漏项（R8） |
| 🟡 一致性 | 4 | 命名口径（R9）、舍入描述自相矛盾（R10）、E1 段落错位（R11）、§10 完成度标注参差（R12） |

**最需要你先看的一条是 R1**：它是唯一一条"设计已定稿、代码已落地、但钱仍然会算错"的问题。

---

## 🔴 R1（必修）：异步复制窗口内，跨节点提取可以超发

### 现象

§16.5 拍板用 append-only 流水账本解决 E1（并发丢更新）。这个解法**只保证"回放幂等"，
不保证"并发串行化"**，而落地实现恰好是异步复制：

- 账本走 `tokenBankLedgerSyncBuffer`（**满 200 条或 15s** 才 flush，§17.3）；
- 提取的额度校验是事务内 `ledgerBalance(ctx, tx, userID)` —— **本地账本的 SUM**
  （`token_bank_withdraw.go:156/414`）。

> 🔍 **精确化（很重要，它排除了一个很自然的错误修法）**：
> 提取读的**已经是权威源**——`AvailableMicro` 与 `Withdraw` 都走 `ledgerBalance`，
> **从不读** `token_bank_accounts` 缓存（缓存只服务于 GUI / admin 展示）。
> 所以滞后**不是**"读了脏缓存"，**改成读 SUM 也解决不了**——本地 ledger 本身就是滞后的副本。

于是存在一个最长约 15s（或 199 条积压）的窗口，两节点各自看到"余额还是满的"：

```
节点 A：hub1 提取全部 100 → 本地账本 withdrawn +100 → 进入缓冲，尚未 flush
（3 秒后）
节点 B：hub2 提取全部 100 → 本地账本里那行还没到 → available 仍是 100 → 放行
同步后：两笔 withdrawn 都成立 → available = −100，超发 100
```

E6（多 hub 平均分配）**防不住这个**：它只约束"顺序提取"，不约束"跨节点窗口"。
§14.5 的自动提取默认开、且 hub 是**自托管**的（家里 / 公司 / 重装各一台），
这条路径不是理论攻击，是日常场景。

### 为什么文档没抓到

- §17.8 的 `TestSettleTokenBankUsageConcurrentIsExactlyOnce`（24 调用 × 4 并发）是**单进程**并发，
  覆盖的是 SQLite 事务与唯一约束，**不是跨节点**。
- §17.3 只讨论了"同一行被两路各发一次会不会重复入账"（答案是不会，靠行级 `INSERT OR IGNORE`），
  没有讨论"两个节点各自**放行**一笔不同的写入"。
- §8 第 9 条给 `credit_share_links` 设计了 `origin_node_id` 裁决（正确），
  但**提取接口没有同样处理**——而提取才是真正动钱的那一步（转赠的过账也走它，§14.7）。

### 建议（三选一，推荐第 1 条）

1. **提取也路由到裁决节点**：仿 §8 第 9 条，给账本/user 定一个 `origin_node_id`
   （或"账本归属节点"），所有写账本的操作（结算除外——结算天然发生在 share 所在节点）
   路由到该节点本地事务执行；不可达时明确失败，不允许代理节点本地代提。
2. **写入前做跨节点版本前置校验**：提取事务里带上"我看到的 ledger 最大 id/count"，
   对端若已领先则拒绝并让调用方重读重试（乐观锁）。改动小，但要引入跨节点读取。
3. **接受窗口 + 事后对账**：保留现状，但把 §14.6 的对账做成**定时且强制告警**，
   并在提取侧加"单用户单日提取总额上限"作为兜底止血。这是最弱的一档，只建议作为过渡。

> 无论选哪条，**判据要写成跨节点测试**（两节点、人为拉长 flush 间隔、并发提取，
> 最终 `withdrawn ≤ earned + received − granted − frozen`）。现有的单进程并发测试不足以证明它。

---

## 🔴 R2（必修）：只有账本进了 HA 同步，其余 token_bank 表全都不进

### 核实结果

`hubcenter/internal/ha/service.go` 的 `isSupportedEntityType` 里，Token Bank **只有一个**实体：

```go
EntityTokenBankLedgerBatch = "token_bank_ledger_batch"   // service.go:50
```

其余全部不在同步范围内（grep `token_bank_shares` / `price_book` / `withdrawals` 在 `ha/` 与
`app/*sync*.go` 下**零命中**）：

| 表 | 同步 | 后果 |
|---|---|---|
| `token_bank_ledger` | ✅ | 余额能到各节点（但有 R1 的窗口） |
| **`token_bank_price_book`** | ❌ | **钱规则不一致**：admin 在节点 1 改价，节点 2/3 仍按旧价结算，同一个模型收益取决于流量落到哪台 |
| **`token_bank_withdrawals`** | ❌ | 幂等/重放凭据是**本地**的。hub 换节点重试用同一 `request_id`（§14.5 重新下发）在新节点上查不到 → 判为"没提过" → 重复扣账并重复建 grant |
| `token_bank_shares` / `_models` | ❌ | 见下方"附带正效应" |
| `token_bank_usage` | ❌ | 明细与 CSV 导出在不同节点看到**不同的数据**，`Σ usage` 无法跨节点对账（§14.6 第一条对账的前提就不成立） |
| `token_bank_share_requests` | ❌ | 幂等键只在创建节点有效，重复提交打到另一节点会建出第二个 share |
| `system_settings`（`token_bank_settings`） | ✅ | 费率/上限能同步 ✓ |

### 两个连带结论

**附带正效应**：§13 里的 🟠 风险「密钥随 HA 同步扩散到全节点」（D4/§8 第 6 条）
**实际上不成立**——`token_bank_shares` 压根不同步，密文不会扩散。
这条应当改口径为"已因不同步而规避"，而不是继续挂在高风险位。

**附带新风险（文档完全没讨论）**：provider 配置同样不在 HA 实体清单里，
意味着 `tbk_` 成员**只在创建它的那个节点上存在**。于是：

- Token Bank 共享池没有跨节点容灾：该节点宕机，这批共享模型整体下线；
- admin 的暂停 / 改档 / 取出经负载均衡落到别的节点时，是对**另一份数据**操作，
  回读时又可能落到第三个节点 → "改了没生效 / 时好时坏"，属于 D27/D28 那一族的静默失配，但更上层。

> ⚠️ 这一条里有我的推断成分：**"provider 不同步 ⇒ tbk_ 成员单节点可见"** 我是从
> "HA 实体清单里没有 llm_provider"反推的，没有追到 provider registry 的完整装配路径。
> 请你或我下一步确认：provider 是否有 HA 之外的同步机制。如果确认是单节点，
> §3.4「三阵列」与 §8「HA 一致性」两节都要重写。

### 建议

文档需要新增一节 **「HA 同步边界表」**，逐表列出「同步 / 不同步 / 理由 / 不同步的后果与兜底」。
现在的状态是 §8 笼统地说"走 ha oplog"，而实现只同步了账本——**文档描述与实现已经不是一回事**，
下一个接手的人会按文档去做，然后踩空。

---

## 🔴 R3（必修）：hub 侧已有完整实现，文档仍写"零代码"

§16.4「v6 已核实为真的引用」里赫然写着 **"hub 侧暂无任何 token_bank 代码（`hub/` 目录 grep `token_bank` 零命中）"**。
现在实际有 7 个文件：

```
hub/internal/center/token_bank.go              WithdrawTokenBank / FinishTokenBankGrant
                                               TokenBankHubWithdrawnMicro
                                               RunTokenBankAutoOnce  ← §14.5 的自动提取
hub/internal/center/token_bank_test.go
hub/internal/httpapi/token_bank_handlers.go
hub/internal/httpapi/router.go
hub/internal/llmservice/token_bank_grant.go    grant 创建 + FindModelServiceGroup 校验
hub/internal/llmservice/token_bank_grant_test.go
```

而且实现比文档**更严谨**，这些设计文档还没吸收：

- 自动提取的 `request_id` 不是"hub 随便生成"，而是
  `TokenBankAutoRequestID(hubID, email, groupID, seq)` —— **确定性 + 带序号**，
  天然解决"重试要不要换 request_id"（§14.5 只写了"hub 生成，重试必须带同一个"）；
- `TokenBankRemainingMicro(reg, email, groupID, now)` —— 阈值判断是**按服务组**算剩余额度的，
  比 §14.5 的"hub 本地余额低于阈值"更精确（后者没说余额指哪个组）。

**后果**：§14.2 是整个 v6 架构的核心（hubcenter 记账 + hub 单向 pull），
而文档的落地进度（§17）里 hub 侧仍是一片空白 → 任何人按文档判断进度，都会得出
"闭环还没接通"的错误结论。

---

## 🔴 R4（必修）：§8 第 9 条仍是 v5 口径，与 §14.7 直接冲突

§8 第 844-845 行（"积分分享链接的 HA 原子性"）写着：

> 发送方 Debit 与接收方 Credit 必须在**同一个事务**内完成，最后只 `emitSync` 一次
> （仿 `CreditsService.Debit` 的 `BeginImmediate` + `GetUserByIDForUpdate` 写法）。

但 §14.7 / §16.1 已经明确 claim **只绑人、不动账**，过账发生在接收方提取时。
§16.1 声称"v5 遗留、v7 已对齐"并逐条列了 6 处，**漏了这一处**。

这条的危害在于它是 §8「安全设计（不可省略）」里"最容易埋雷的一条"，
新读者会照着它实现一个根本不存在的事务。

同样性质的还有 §8 第 846-849 行的"顺带暴露的既有风险"整段——它还在讨论
`sm_users` 四字段（购买钱包），而 §14.8 已确认与 Token 银行无关并回滚。

---

## 🟠 R5（应修）：unfreeze 的幂等约定已过时，文档比实现"弱"

§14.3 的幂等约定表写：

```
转赠解冻 → id = "unfreeze:<link_id>:<seq>"   （可能多次，不加唯一键）
```

代码（`token_bank_gift.go:356 / :495`）实际是：

```go
giftLedgerID("unfrz", "unfreeze:"+linkID)   // 无 seq，对 link 纯函数 → 确定性
```

而且 `releaseGiftFreeze` 的注释写明 **"capped at the sender's"** —— 已经做了防负值上限。

也就是说：**C2（解冻时发送方余额可能已不足）的实现比文档强**。
文档留着 `:<seq>` 这个写法，会让维护者以为解冻可以重复发生、进而自己再去造防重逻辑，
甚至有人会真的去实现一个非确定性的 `seq`，那才会把 §14.3 强调的"三节点必须算出同一个值"打破。

---

## 🟠 R6（应修）：`RebuildAccount` 没有生产调用点，"自愈"是纸面的

§14.3：「rebuild（缓存错了能自愈，**这是 E1 的全部价值**）」
§14.6：「hubcenter 缓存：`SUM(ledger)` vs `token_bank_accounts.*` → **不等就触发 rebuild**」

grep 全仓（排除测试）只有定义，没有调用：

```
token_bank_repo.go:255  // RebuildAccount recomputes the cached balance...
token_bank_repo.go:258  func (r *TokenBankRepo) RebuildAccount(...)
token_bank_ledger_sync.go:38  // RebuildAccount is the repair tool, not the steady state.
```

两条承诺都没落地：既没有定时对账，也没有不等时自动 rebuild。

**严重性修正（复查后下调 🟠 → 🟡）**：缓存漂移的后果是 **GUI / admin 显示错误余额**，
**不是资损**——因为提取决策读的是 ledger（`token_bank_withdraw.go:156`），不读缓存。
所以它是"用户看到错的可用积分"的体验/信任问题，不是钱的问题。仍建议修，但不必排在 F1 之前。

建议：至少补一个启动时 + 定时的 `SUM vs cache` 校验，不等则 rebuild 并打点告警；
否则 §14.3 那句"E1 的全部价值"是空的。

---

## 🟠 R7（应修）：`manual=true` 由调用方自报，E6 的 1/N 约束可以被一句话绕过

§14.5 拍板：自动提取按 1/N 均摊，手动提取"用户显式意图"可取全部。
但落地上 `Manual` 是从请求体读的布尔（`token_bank_handlers.go:170`、
`token_bank_hub_withdraw.go:127` 都是 `Manual: req.Manual`）。

§17.6-D12 自己已经点破：

> HubCenter 无法从请求本身区分"人在点按钮"和"cron 在跑"。

既然无法区分，而 hub 是**自托管**的（`hub/README.md`），那么任一 hub 只要传
`manual=true` 就能一次提走用户全部积分——**正是 E6 要消灭的"一台机器抽干"**。
E6 的 1/N 于是只保护了"老老实实不传 manual 的客户端"。

建议（择一）：
- `manual` 提取要求**用户会话凭据**，hub 服务凭据只允许受限模式；
- 或 hub 专用端点强制 `Manual=false`（它本就是机器在调）；
- 或给 manual 提取加日累计上限（比如单日不超过 available 的 1/N × 2）。

---

## 🟠 R8（应修）：API 清单与文件清单均漏项

§6.1 只列了 9 条客户端接口，实际代码里还有（文档里找不到）：

| 实际存在 | 文件 |
|---|---|
| hub 专用提取端点（独立认证路径） | `httpapi/token_bank_hub_withdraw.go` |
| 领取路由到 origin 节点 | `httpapi/token_bank_claim_route.go` |
| `GET /c/{code}` 领取落地页 | `httpapi/token_bank_landing.go` |
| 用量 / 收益接口（P1 #10 #11） | `httpapi/token_bank_usage_handlers.go` |
| 分享 CRUD | `httpapi/token_bank_share_handlers.go` |

§17 的文件清单同样漏了至少 11 个文件（`token_bank_publish.go`、`_autopause.go`、
`_caps.go`、`_group.go`、`_usage_query.go` 等），以及整个 `hub/` 目录。

文档头部那三条"实现记录（2026-10-02）"补了领取落地页、分时统计、不能购买算力卡，
但正文与清单没跟着动——**这是文档维护方式的系统性缺口**：新内容堆在顶部注记里，
正文不同步。建议要么正文跟着改，要么在顶部明确写"以下新增内容尚未并入正文"。

---

## 🟡 R9：余额口径的命名与术语不一致

- §5「账户余额的三个数」表写的是 `token_bank_accounts.withdrawn_credits` / `frozen_credits`
  ——**浮点命名、无 `_micro` 后缀**，与 §14.3 最终版 `withdrawn_micro` / `frozen_micro INTEGER` 不符；
- 表头写"已提取"，§7.3 的 GUI 示意图写"已消耗 30.00"。**"已消耗"会被用户读成
  "被助手花掉了"**，而它其实是"已提取到本机"。§5 自己也在 §5-v7 注记里强调要区分两个"可用"，
  却在这张表里用了最容易混淆的那个词；
- 卡片示例 `128.45 − 30.00 − 10.00 = 88.45` **漏了 `granted` 桶**（转赠过账后的支出），
  一旦用户转赠成功，卡片上的减法会对不上。

---

## 🟡 R10：舍入描述自相矛盾

§5 ④：`gross = round(base × tier_multiplier) ← **单次只舍入一次**`
§17.8 判据：`TestComputeTokenBankSettlementRoundsEachLegUp`（**每条腿独立向上取整**）

实际是四条腿各 `ceil` 一次、再乘倍率 `round` 一次，**至少两次**。
"单次只舍入一次"这句是错的，且它恰好是 §5 里唯一一条关于精度的声明。

---

## 🟡 R11：E1 段落位置错、语气未更新

E1（跨节点丢更新）被放在 **§14.6「精度与对账」** 下面，但它跟精度无关，是并发一致性问题。
而且正文仍以未决语气写"三条改法（推荐第 1 条）"，而 §16.5 已拍板采用第 1 条。
§13 风险表同样把 E1/E2 写成"账本改 append-only 流水"这种**待办**措辞。

---

## 🟡 R12：§10 完成度标注参差

- P0-0 仍标"v5 需追加修正"（§17.1 已落地）；
- P0-0b / P0-1 标"🟡 store 层已落地"，实际 store + httpapi + admin UI + GUI 面板都完成了；
- P0-4 的"桌面端到端点击未做"在 §17.11 末尾又重复了一遍，与 §17.10 的"已知限制"重叠。

---

## ✅ 核实为真（抽查 6 项）

| 文档引用 | 核实结果 |
|---|---|
| §14.4/E5：`hub/internal/llmservice/service.go:858` 兑换卡时 `FindModelServiceGroup(...) == nil { continue }` | ✅ 行号准确，上下文一致 |
| §14.4 判据②：服务组内无 tbk_ 成员时报错 | ✅ 已实现：`token_bank_hub_withdraw.go` 返回 400 `service_group_not_token_bank`（**但 §17 没记录这条判据**） |
| §17.3：账本复制实体只有 `token_bank_ledger_batch` | ✅ 准确，且这正是 R2 的出发点 |
| §17.1：`token_bank_repo.go` 的五个 bucket、`AvailableMicro` 扣 frozen | ✅ 与代码一致 |
| §16.5 拍板 5（阵列倍率恒 1.0 锁定） | ✅ `token_bank_arrays.go` 已实现 |
| §3.6 / §14.7：claim 只绑人不动账 | ✅ `ClaimGiftLink` 单条条件 UPDATE，`SettleClaimedGift` 独立 |

---

## 建议的处置顺序

1. **先拍板 R1**（跨节点提取超发）——它决定要不要动提取的执行位置，越晚改动越大。
2. **确认 R2 里的 provider 同步事实**，据此重写 §3.4 / §8 的 HA 部分，并新增「HA 同步边界表」。
3. **R3 / R4 / R5 / R8 是纯文档修正**，可以直接并进 v8：把 hub 侧实现补进 §17、
   把 §8 第 9 条改成 claim-only 口径、把 unfreeze 幂等约定改成代码实际写法、补全文件与 API 清单。
4. **R6 / R7 是代码缺口**，建议各开一条待办（rebuild 调度、manual 凭据收敛）。
5. **R9–R12 随 v8 一并清理**。

> R1 与 R2 都需要你先拍板方向，其余我可以按本文档直接改 v8 正文。
