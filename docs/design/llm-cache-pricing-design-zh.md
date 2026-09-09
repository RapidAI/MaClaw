# Hub/HubCenter LLM 缓存独立计价设计

## 1. 背景与目标

当前 LLM 计费只区分输入 Token 和输出 Token。上游服务商返回的 Prompt Cache 用量（Cache Read、Cache Write）虽然已经被记录，但仍按普通输入价格处理，无法体现缓存命中的实际成本差异。

本设计的目标：

1. 为 Cache Read 和 Cache Write 增加独立的 Credits 与人民币参考价格。
2. 默认 Cache Read 价格为普通输入价格的 1/10。
3. Hub 直连的第三方服务商、HubCenter 官方服务商使用统一计价模型。
4. Hub 使用统计同时按缓存价格计算 Credits 和人民币参考成本。
5. 价格在请求开始时冻结，历史账单不受后续配置修改影响。
6. 保持旧版价格快照和旧账单的含义不变。

本设计明确区分“上游成本价格”和“服务组售卖倍率”：缓存单价是服务商/模型的基础价格，服务组只负责对整次请求加倍率。不得在多个位置同时维护一份没有优先级定义的缓存价格。

## 2. 术语与边界

### 2.1 Prompt Cache

Prompt Cache 是上游模型对输入上下文的缓存。它只作用于输入侧，不缓存输出 Token。

- **Cache Read**：本次请求复用了上游已有的输入缓存。
- **Cache Write**：本次请求向上游写入新的输入缓存。
- `cached_input_tokens` 和 `cache_write_tokens` 都是输入 Token 的子集，不能与普通输入重复计算。

### 2.2 本地完整响应缓存

Hub 或 HubCenter 命中本地完整响应缓存时，不会调用上游服务商，也没有真实的上游 Token 用量：

- 不扣 Credits；
- 不产生输入、输出或 Cache Read/Write 费用；
- 使用统计标记 `local_cache_hit=true`，并与 Prompt Cache 分开统计。

## 3. 价格配置模型

### 3.1 新增字段

价格单位为每 10,000 Token。

```json
{
  "input_credits_per_10k": 1.0,
  "output_credits_per_10k": 4.0,
  "cache_read_credits_per_10k": 0.1,
  "cache_write_credits_per_10k": 1.0,
  "input_rmb_per_10k": 0.02,
  "output_rmb_per_10k": 0.08,
  "cache_read_rmb_per_10k": 0.002,
  "cache_write_rmb_per_10k": 0.02,
  "minimum_request_credits": 0.1,
  "version": "cache-v1"
}
```

新增字段：

- `cache_read_credits_per_10k`
- `cache_write_credits_per_10k`
- `cache_read_rmb_per_10k`
- `cache_write_rmb_per_10k`

人民币字段仅用于参考成本和报表展示，不参与 Credits 扣款。

### 3.2 默认值

当新版本价格配置没有显式填写缓存价格时：

```text
cache_read_credits_per_10k  = input_credits_per_10k × 0.1
cache_write_credits_per_10k = input_credits_per_10k × 1.0
cache_read_rmb_per_10k      = input_rmb_per_10k × 0.1
cache_write_rmb_per_10k     = input_rmb_per_10k × 1.0
```

Cache Read 的默认十分之一是本设计的核心默认规则。Cache Write 默认按普通输入价计算，也可以由管理员单独设置。

新字段必须支持“未设置”和“显式设置为 0”的区分。不能用普通数值字段的 `0` 同时表示这两种状态；实现应使用可选指针字段、字段存在位或等价的 presence 机制。只有“未设置”才触发上述默认值。

如果业务最终决定 Cache Write 也默认为输入价的十分之一，只需替换默认策略，不应改变字段和快照结构。

### 3.3 分时价格窗口

如果服务商使用分时价格，`price_schedule` 中也允许覆盖四个缓存价格字段。请求开始时解析一次时间窗口，并将最终价格写入不可变快照；流式请求跨越时间窗口时不得重新计价。

## 4. 配置入口与归属

### 4.1 Hub 直连第三方服务商

配置入口：

```text
Hub 管理后台
  → LLM Endpoint/服务商
  → 编辑服务商
  → Token Pricing
```

这里维护第三方服务商及其模型的基础输入、输出、Cache Read、Cache Write 价格，以及人民币参考成本。

服务组中的“模型路由 → 添加/编辑服务商”默认只配置路由、计费模式和 `credit_multiplier`。如确实需要不同服务组采用不同售卖单价，应显式打开“覆盖基础价格”，并保存为服务组路由价格快照；未打开时必须继承服务商价格。

价格来源必须显式记录，优先级为：

```text
服务组路由显式覆盖
    > 服务商/模型基础价格
    > 旧版兼容默认规则
```

服务商价格和服务组覆盖不能静默合并。每个请求快照保存 `pricing_source`（`provider` 或 `service_group_override`），以便审计。

### 4.2 HubCenter 官方 MaClaw 服务商

官方服务商的基础价格由 HubCenter 维护并随请求发送价格快照。Hub 只应用请求所属服务组的计费倍率，不应再次替换 HubCenter 的基础价格。

Hub 不允许通过本地服务组覆盖修改官方服务商的基础价格；如需调整用户售价，应使用服务组倍率，由 HubCenter 的价格快照负责记录上游基础价格。

### 4.3 服务组倍率

服务组或模型路由中的 `credit_multiplier` 是整条请求的倍率，不是缓存折扣。最终倍率为：

```text
provider_time_multiplier × service_group_multiplier
```

该倍率同时作用于普通输入、Cache Read、Cache Write 和输出费用。

### 4.4 GUI 本地缓存开关

MaClaw GUI 中的 `llm_prompt_cache` 只负责启用/关闭本地缓存及其 TTL、容量等运行参数，不负责设置价格。

## 5. 上游用量归一化

Hub 和 HubCenter 应将不同第三方协议转换为统一字段：

| 服务商字段 | 统一字段 |
|---|---|
| OpenAI `usage.prompt_tokens_details.cached_tokens` | `cached_input_tokens` |
| Anthropic `cache_read_input_tokens` | `cached_input_tokens` |
| Anthropic `cache_creation_input_tokens` | `cache_write_tokens` |
| 其他协议的等价字段 | 对应统一字段 |

如果第三方只返回总输入 Token，没有缓存明细：

- 不得猜测缓存命中量；
- `cached_input_tokens=0`、`cache_write_tokens=0`；
- 全部按普通输入价格计费；
- 使用统计标记缓存明细不可用。

## 6. Credits 计费公式

设：

```text
I  = input_tokens
R  = cached_input_tokens       (Cache Read)
W  = cache_write_tokens        (Cache Write)
O  = output_tokens
```

归一化层必须先确认三类输入用量是否属于同一个 `input_tokens` 总数。当前支持的 OpenAI/Anthropic 字段均按“包含在总输入中”处理，并要求：

```text
0 <= R <= I
0 <= W <= I
R + W <= I
```

输入 Token 再拆分：

```text
普通输入 = max(0, I - R - W)
```

请求未乘倍率前的 Credits：

```text
base_credits =
    普通输入 × input_credits_per_10k / 10000
  + R × cache_read_credits_per_10k / 10000
  + W × cache_write_credits_per_10k / 10000
  + O × output_credits_per_10k / 10000
```

最终扣款：

```text
credits = base_credits × provider_time_multiplier × service_group_multiplier
credits = max(credits, minimum_request_credits × 最终倍率)
```

如果上游数据违反约束，不能静默制造费用。应按以下顺序处理：保留原始 usage、将计费使用量截断到输入总数范围、记录 `usage_anomaly=cache_tokens_exceed_input`，并在报表中显示异常请求数。截断策略必须在 Hub 和 HubCenter 共用，保证重试和对账结果一致。

客户端提交的 `cached_input_tokens` 或 `cache_write_tokens` 不得直接作为计费依据；计费只接受上游响应解析结果或受信任的 HubCenter 快照。

## 7. 人民币参考成本

人民币成本使用完全相同的 Token 拆分和倍率，但读取 RMB 价格字段：

```text
rmb_cost =
    普通输入 × input_rmb_per_10k / 10000
  + R × cache_read_rmb_per_10k / 10000
  + W × cache_write_rmb_per_10k / 10000
  + O × output_rmb_per_10k / 10000
```

人民币成本只用于展示、报表和运营分析，不得反向推导 Credits 扣款。

## 8. 预扣款与结算

请求发送前通常不知道真实 Cache Read/Write 数量，因此预扣款必须覆盖最坏情况。

建议预估输入价格使用：

```text
max(input_credits_per_10k,
    cache_read_credits_per_10k,
    cache_write_credits_per_10k)
```

同时仍需计入输出上限：

```text
预扣上限 = 预估输入 Token × 上述最大输入单价
         + output_token_limit × output_credits_per_10k
```

最后再乘 Provider 分时倍率、服务组倍率并应用最低消费。预扣款只能增加安全上限，不能把缓存折扣当作已知事实。

请求完成后根据真实 usage 重新计算：

- 扣除最终应付 Credits；
- 释放预扣款与最终扣款之间的差额；
- 如果 usage 缺失，保留 `usage_unresolved` 状态，不能擅自按缓存折扣结算。

## 9. 价格快照与历史账单

每次可计费请求必须冻结以下信息：

```text
pricing.version
input/output/cache_read/cache_write Credits 价格
input/output/cache_read/cache_write RMB 价格
provider_time_multiplier
service_group_multiplier
minimum_request_credits
pricing_source
```

账单明细还必须保存本次实际用量及已计算的方向性金额（普通输入、Cache Read、Cache Write、输出分别保存 Credits 与 RMB）。只保存当前价格再在报表阶段重算是不允许的，因为会破坏历史不可变性。

重试场景以最终成功且实际发送上游的 attempt 为准；同一逻辑请求不得因多个上游尝试重复扣除缓存 Token。未发送上游的失败 attempt 只能记录访问日志。

历史账单、Usage Stats 和对账任务都使用该快照，不能在报表生成时重新读取当前配置。

### 9.1 旧快照兼容

旧版快照没有缓存价格字段，或 `pricing.version` 为 `legacy-v1` 时：

- Cache Read 和 Cache Write 继续按旧版普通输入价格处理；
- 不回溯修改历史账单；
- 新请求生成 `cache-v1` 快照后才使用独立缓存价格。

### 9.2 接口与存储迁移

新增字段必须同时出现在以下边界：

1. HubCenter → Hub 的认证价格快照；
2. Hub/HubCenter 内部 usage 结构和流式 usage 结构；
3. 账单明细、访问日志和日报/趋势聚合表；
4. 管理后台读写 API 及导入导出 JSON。

数据库迁移只允许追加 nullable/带默认值的列。旧记录的缓存方向性金额保持为空或按旧账单规则展示，不得使用新默认价格回填历史记录。

## 10. Hub 使用统计

### 10.1 后端统计字段

Usage Stats 的请求、汇总和趋势接口应至少提供：

```text
input_tokens
cached_input_tokens
cache_write_tokens
output_tokens

normal_input_credits
cache_read_credits
cache_write_credits
output_credits
total_credits

normal_input_cost_rmb
cache_read_cost_rmb
cache_write_cost_rmb
output_cost_rmb
total_cost_rmb
cache_usage_source
usage_anomaly_count
```

`normal_input_credits` 等方向性金额应直接来自账单明细的冻结结果；汇总接口不得读取当前服务商价格重新计算。`cache_usage_source` 至少包括 `provider_reported`、`unavailable`、`local_cache`。

### 10.2 前端展示

统计页面应分别展示：

- 普通输入、Cache Read、Cache Write、输出 Token；
- 各方向 Credits 费用及总 Credits；
- 各方向人民币参考成本及总成本；
- 缓存命中率和缓存复用率；
- 无缓存明细的请求数量及覆盖率。

“Input” 汇总可以继续保留，但必须明确它是普通输入与缓存输入的合计，不能把 Cache Read 再重复加到输入费用中。

## 11. 第三方服务商适配要求

每个协议适配器必须：

1. 解析供应商返回的 Cache Read/Write 用量；
2. 明确区分“字段不存在”和“字段值为零”；
3. 将归一化后的用量写入请求 usage、访问日志和账单快照关联记录；
4. 对流式响应在收到 usage 后更新最终结算；
5. 对不支持缓存明细的服务商使用普通输入价格，不做推断。

适配器还必须保留原始字段是否存在的信息，避免把“服务商明确返回 0”和“服务商不支持该字段”混为一谈。对于不可信或客户端伪造的缓存字段必须丢弃，并记录诊断原因。

## 12. 验收测试

### 12.1 计费计算

输入价 1、Cache Read 价 0.1、Cache Write 价 1、输出价 4：

```text
input=10000, cached=8000, write=0, output=5000
费用 = 2000×1/10000 + 8000×0.1/10000 + 5000×4/10000
     = 2.28 Credits（未乘倍率）
```

应覆盖：

- 只有 Cache Read；
- 只有 Cache Write；
- Cache Read 与 Cache Write 同时存在；
- 缓存 Token 为零；
- 缓存 Token 超过输入 Token；
- 最低消费和最终倍率；
- 价格小数、舍入和重放一致性。

还应验证“未设置”和“显式零价”的区别，以及服务商价格、服务组覆盖和旧版兼容价格三种来源的选择结果。

### 12.2 第三方协议

- OpenAI cached tokens 正确归一化；
- Anthropic cache read/write 正确归一化；
- 无缓存字段时按普通输入计费；
- 流式 usage 与非流式 usage 结果一致。

### 12.3 历史与报表

- 修改价格后，旧账单金额不变；
- Hub Usage Stats 的 Credits 与人民币成本和账单一致；
- 本地完整响应缓存命中扣费为零；
- 缓存明细覆盖率统计正确。

## 13. 发布步骤

1. 增加价格字段、快照字段和 usage-aware 计费函数。
2. 增加 HubCenter 与 Hub 管理界面字段，默认填充 Cache Read 为输入价的 1/10。
3. 增加第三方协议用量归一化和报表字段。
4. 先以 `cache-v1` 生成新请求快照，保留 `legacy-v1` 兼容读取。
5. 完成历史账单、预扣款、流式请求和第三方协议回归测试后启用。
