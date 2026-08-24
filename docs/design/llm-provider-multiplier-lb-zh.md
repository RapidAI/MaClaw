# 同倍率服务商 Load Balance

> 状态：设计定稿，待实现
> 范围：Hub + HubCenter
> 日期：2026-08-18
> 二次评审：2026-08-18

## 1. 决策摘要

同成本档的服务商不再按卡片顺序吃满 failover，而是自动组成一个 LB 池。

- **不同倍率**：便宜档先整档，贵档只做 failover。
- **相同倍率**：档内用平滑加权轮询分流；忙、暂停、熔断的成员跳过，再试同组下一个。
- **卡片不相邻不影响成组。** 分组键是当前生效倍率，不是 sequence 是否连续。
- **sequence 只保留两件事**：管理页展示序，以及同组内失败后的尝试次序。
- Hub 与 HubCenter 共用 `corelib/llmpool` 里的分组和选路，不各写一套。

算法结论：档内主算法用 **平滑加权 round-robin（nginx smooth WRR）**，不用 least-in-flight。后者在低 QPS（对话请求的常态）下会退化成永远打 sequence 最小的那家。

## 2. 现状

| 位置 | 现在怎么选 | 同倍率时的结果 |
|---|---|---|
| HubCenter proxy | `OrderProviderRoutes` 之后再按 `sequence` 重排；忙了 `TryAcquire` 失败就跳下一家 | 序号小的吃满，另一家闲着 |
| Hub `/v1` | `OrderProvidersForRequest` ：能力 / 解析档 / 更便宜倍率 / priority / 配置顺序；忙了 `Acquire` 排队等待 | 列表靠前的那家固定先打；若它满了会排队而不是分流 |

卡片目前不展示倍率，也没有同组标志。

## 3. 分组

```
group_key = FormatCreditMultiplierHeader(ResolveCreditMultiplier(provider.billing, now))
例：x0.5 / x1 / x1.5 / x2
```

1. 用**当前生效**的服务商计费倍率（默认值+分时窗口），不用默认倍率。
2. 卡片只看服务商级 vendor 倍率。
3. 运行时若路由加价不同，有效成本是 `vendor x route`，加价不同则不进同一池。卡片组标仍按 vendor 档，可能和单次请求的运行时分档不一致。
4. 浮点用格式化字符串做 key。
5. 一组至少 2 个成员才叫 LB 组。
6. 已暂停的仍显示组标，但不进运行时池。
7. **位置不连续不影响成组。**

## 4. 档内算法

### 4.1 不用 least-in-flight 做主选

1. **低 QPS 塌缩**。对话请求通常一条结束才来下一条。 `in_flight=0` 时只能靠 sequence 破平，每一枪都打序号最小的那家。
2. **`max_concurrency=0` 不记账**。 Hub / HubCenter 不限并发时都不增加 active。
3. **Hub `Acquire` 会排队**。 A 满了会在 A 上等到超时，而不是交给同组空闲的 B。

in-flight 只用来判断满员跳过，不当主选。

### 4.2 主选：平滑加权 WRR

```
weight = max_concurrency          // > 0
weight = max(group_max_finite, 1) // max_concurrency <= 0, never full
```

WRR 成员键是 **providerID**，不是 `(provider, model)`。同一服务商多条路由共享一份权重。

用 nginx smooth WRR（进程内、按 `pool + band` 记 `current_weight`，加锁）。HubCenter 的 pool 是 `服务组ID + 逻辑模型`，Hub 的 pool 是 `模型名`。不能只用全局分档键，否则不同模型会互相重置 WRR：



```
for member in group:
    current_weight[member] += weight[member]
    pick max current_weight (tie: smaller sequence)
pick.current_weight -= sum(weights)
return pick
```

成员集合或权重变化时，按 `providerIDs + weights` 指纹重置该组。多实例各自记账。

- 低 QPS 也会轮转，两家 `conc=10` 接近 1:1。
- `conc=100` + `conc=10` + `conc=10` 约 10:1:1。
- 等权时**第一枪仍是 sequence 最小者**。旧测试 `TestHandleProxyRequestUsesProviderSequenceOrder` 只打一枪，不能证明永远先打序号小的；分流要连续两枪。

### 4.3 尝试顺序

```
显式指定服务商 (X-LLM-Provider / ?provider=) -> 不走 LB
过滤: 不存在/暂停/熔断/协议或模型不匹配
打分保持现状: capability / resolution_tier / priority
按有效倍率分档, 便宜 -> 贵
档内再按打分带 (score, resolution_tier) 切开:
  能力分不同的不互为 LB, 只做 failover
  同带且同倍率才 WRR
档内次序:
  1. 第一家 = WRR 胜者
  2. 其余按 sequence 从小到大 (不含胜者), 不从胜者序号往后切，也不绕环
  3. 忙/失败/可重试 5xx -> 同带同组下一个
  4. 同带耗尽 -> 同倍率下一打分带 -> 下一档
HubCenter extraLiveServiceGroupFailoverRoutes 仍挂在主池之后，不进主池 WRR
```

例： sequence 为 A=1, B=2, C=4。 WRR 选中 C 后， failover 是 A -> B, 不是 C -> (空)。

### 4.4 满员与排队

| | 同组还有空位 | 同组都满 |
|---|---|---|
| HubCenter | TryAcquire 失败即跳过 | 进入下一档 |
| Hub | 满了跳过，禁止在有空闲兄弟时对满员成员 Acquire 排队 | 才允许对 WRR 胜者排队 |

### 4.5 不在范围内

- 一条流式请求不拆到多家。
- 缓存命中仍回原 provider。
- 不引入手动 LB 组配置。
- 不按组重排卡片。
- 本轮不合并 Hub `OrderProvidersForRequest` 与 `llmpool.OrderProviders`； Balance 作为第二趟。

## 5. 卡片

列表接口增加 `current_multiplier` / `lb_group` / `lb_group_size` / `lb_eligible`。
- `lb_group_size >= 2` 才显示 `LB · x1`。
- 暂停仍保留组标。
- 分时窗口显示当前值。
- HubCenter: `renderProviders()`; Hub: endpoint 卡片标题旁

## 6. 落地

```
corelib/llmpool
  ProviderLBGroups / WRRScheduler / BalanceProviderRoutes
hubcenter
  proxy.go drop whole-list sequence sort; extras stay last
  list API + llm-service-tab.js badge
hub
  OrderProvidersForRequest then Balance
  skip full sibling instead of queue; list API + llm-provider-tab.js badge
```

## 7. 验收

1. 两家同 x1 连续空闲请求两边都要打到；第一枪允许打序号小的。
2. conc=100 + 10 + 10 约 10:1:1。
3. 改成 x2 则退出 x1 组。
4. 分时窗口改档后卡片与选路一起变。
5. 暂停成员有组标但不进池。
6. Hub 同组一家打满时新请求打另一家，不排队。
7. 中间夹着不同倍率不影响同组成组。
8. 显式指定服务商绕过 LB。
9. tools 请求只在有 tools 能力的同倍率成员间 WRR。
10. 等权两家只打一枪仍打 sequence 较小者。

## 8. 算法评审记录

| 候选 | 结论 |
|---|---|
| sequence failover | 现状。否决。 |
| least-in-flight | 低 QPS 塌缩。可作满员跳过，不可作主选。 |
| idx % sum(w) | 会连打大容量节点。否决。 |
| smooth WRR + skip full + sequence failover | 采用。 |
| equal RR | 会浪费 conc=100 节点。不采用。 |

## 9. 二次评审补丁

1. **「从起点起」有歧义，已改成「胜者第一，其余按 sequence」。**
2. **WRR 必须落在同一打分带里。** 只按倍率分组会把 tools 请求均衡到不会 tools 的兄弟。
3. **卡片组 != 每次请求的运行时组。** 卡片用 vendor；运行时用 vendor x route。
4. **HubCenter extras 不进主池。**
5. **WRR 按 providerID 计权重。**
6. **重置指纹含权重。**
7. **旧 sequence 单请求测试会继续绿。** 分流验收必须打两枪以上。

## 10. 落地后复查补丁

1. **WRR 状态按 pool 隔离。** 键是 `pool + band`，不是裸分档键 `x1|s0|t1`。不同模型即使同档也不互相重置。
2. **同一 pool 内成员或权重变化仍重置。** 熔断把成员踢出 WRR 后，冷却结束重新进入会换指纹，第一枪回到 sequence 最小者以便探测。单成员早退也必须忘掉旧状态，否则缩成 1 个再恢复会接着上次轮转。
3. **暂停 / 熔断不占 WRR 名额。** `SkipWRR`：暂停卡仍留在故障转移名单（全暂停仍报 paused）。Hub 熔断打开同样不进 WRR，半开探测仍走 BeforeAttempt。HubCenter 熔断打开的卡继续从主池省略。
4. **进程内 WRR 状态上限 512**，按 LRU 淘汰，避免模型变多后 map 无限涨。
