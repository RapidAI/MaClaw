# 需求文档：算力管理模块

## 简介

算力管理模块在 iWorkerCloud（中心化管理平台）与 iWorkerCenter（边缘节点）之间建立统一的 LLM 算力配置与分发机制。iWorkerCloud 作为算力配置的中心来源和 LLM 请求的转发网关，管理多个 LLM 服务商的接入信息；iWorkerCenter 的 LLM 请求默认通过 iWorkerCloud 转发到上游 LLM 服务商，iWorkerCloud 在转发过程中自动记录 token 用量。如果 iWorkerCloud 对某个 iWorkerCenter 授予算力自管理权限，该 iWorkerCenter 可以自行配置 LLM 服务商并直连。所有不同协议的 LLM 服务（Anthropic、Gemini 等）统一转换为 OpenAI API 协议，对 DiWorker 客户端提供一致的 LLM 服务接口。

## 术语表

- **iWorkerCloud**: 中心化管理平台，管理多个 iWorkerCenter 实例，负责 LLM 服务商配置的集中管理与分发
- **iWorkerCenter**: 边缘节点，运行纯 Linux 服务程序（Go HTTP 服务 + 内嵌 Web 管理面板），默认从 iWorkerCloud 获取算力配置，对 DiWorker 提供 LLM 代理服务
- **DiWorker**: 数字员工客户端，通过 iWorkerCenter 的 `/v1/chat/completions` 端点消费 LLM 服务
- **LLM_Provider**: 一个 LLM 服务商的接入配置记录，包含 URL、API Key、协议类型、User-Agent、算力类型等字段
- **Protocol_Adapter**: 协议转换组件，将非 OpenAI 协议（Anthropic、Gemini 等）的请求和响应转换为 OpenAI API 格式
- **Compute_Permission**: iWorkerCloud 对某个 iWorkerCenter 授予的算力模块自管理权限
- **Compute_Source**: iWorkerCenter 的算力来源模式，取值为 `cloud`（从 iWorkerCloud 同步）或 `local`（本地自行管理）
- **Provider_Sync**: iWorkerCenter 从 iWorkerCloud 拉取最新 LLM_Provider 配置列表的过程
- **Compute_Type**: 算力的用途分类标签，例如 `general`（通用）、`coding`（编程）、`document`（文档）、`analysis`（分析）
- **Token_Usage_Record**: 单次 LLM 请求的 token 用量记录，包含 input_tokens、output_tokens、total_tokens、关联的 LLM_Provider、请求时间戳、所属 DiWorker 标识
- **MToken_Price**: 每百万 token（MToken）的单价配置，每个 LLM_Provider 可分别配置 input_price_per_mtoken（输入单价）和 output_price_per_mtoken（输出单价），单位为货币金额
- **Cost_Summary**: 基于 Token_Usage_Record 和 MToken_Price 计算得出的费用汇总，支持按日、按月维度聚合
- **Center_Cost_Report**: iWorkerCloud 层面按 iWorkerCenter 维度聚合的费用统计报表，用于精算和成本分摊
- **DiWorker_Cost_Report**: iWorkerCenter 层面按 DiWorker 维度聚合的 token 用量和费用统计报表，用于计算单个数字员工的投入产出

## 需求

### 需求 1：iWorkerCloud LLM 服务商管理

**用户故事：** 作为 iWorkerCloud 管理员，我希望在 Cloud 平台上集中添加和管理多个 LLM 服务商配置，以便统一分发给各个 iWorkerCenter。

#### 验收标准

1. THE iWorkerCloud SHALL provide a CRUD API for LLM_Provider records, each containing: name (display name), base_url (API endpoint), api_key (authentication credential), protocol (one of `openai`, `anthropic`, `gemini`), user_agent (request User-Agent string, e.g. `openclaw` or `claude-code/2.0.0`), compute_type (one of `general`, `coding`, `document`, `analysis`), model (default model identifier), enabled (boolean), priority (integer for ranking), and description (free-text note)
2. WHEN an administrator creates or updates an LLM_Provider, THE iWorkerCloud SHALL validate that base_url is a valid HTTPS URL and that protocol is one of the supported values (`openai`, `anthropic`, `gemini`)
3. THE iWorkerCloud SHALL store LLM_Provider records in the SQLite database with api_key encrypted at rest
4. WHEN an administrator requests the LLM_Provider list via the admin API, THE iWorkerCloud SHALL return all fields except api_key, replacing api_key with a boolean `has_api_key` flag
5. THE iWorkerCloud SHALL provide a test endpoint that sends a simple prompt to the specified LLM_Provider and returns success/failure status with latency measurement
6. THE iWorkerCloud admin web panel SHALL include a "算力管理" (Compute Power) page for managing LLM_Provider records with add, edit, delete, enable/disable, and test connectivity actions

### 需求 2：iWorkerCloud 算力配置分发 API

**用户故事：** 作为 iWorkerCenter，我希望通过 API 从 iWorkerCloud 拉取分配给我的 LLM 服务商配置，以便自动获取算力接入信息。

#### 验收标准

1. THE iWorkerCloud SHALL provide a `GET /api/centers/{id}/compute-providers` endpoint that returns the list of enabled LLM_Provider records assigned to the requesting iWorkerCenter
2. WHEN an iWorkerCenter requests compute providers, THE iWorkerCloud SHALL authenticate the request using the center's secret (same mechanism as heartbeat authentication)
3. THE iWorkerCloud SHALL include the full api_key in the response to authenticated iWorkerCenter requests (unlike the admin API which masks it)
4. WHEN an iWorkerCenter with status `disabled` requests compute providers, THE iWorkerCloud SHALL return HTTP 403 with error code `CENTER_DISABLED`
5. THE iWorkerCloud SHALL support assigning specific LLM_Provider records to specific iWorkerCenter instances; when no specific assignment exists, all enabled providers are returned

### 需求 3：iWorkerCloud 算力权限授予

**用户故事：** 作为 iWorkerCloud 管理员，我希望能够授权特定的 iWorkerCenter 自行管理本地算力配置，以便满足有特殊需求的边缘节点。

#### 验收标准

1. THE iWorkerCloud SHALL maintain a Compute_Permission flag per iWorkerCenter, stored in the center record, defaulting to `false`
2. WHEN an administrator enables Compute_Permission for an iWorkerCenter, THE iWorkerCloud SHALL include `compute_permission: true` in the compute providers API response for that center
3. WHEN an administrator disables Compute_Permission for an iWorkerCenter, THE iWorkerCloud SHALL include `compute_permission: false`, signaling the center to revert to cloud-sourced providers
4. THE iWorkerCloud admin panel SHALL provide a toggle control on each center's detail page to grant or revoke Compute_Permission
5. WHEN Compute_Permission is revoked for an iWorkerCenter that was using local providers, THE iWorkerCloud SHALL include a `force_sync: true` flag in the next compute providers response, instructing the center to discard local overrides

### 需求 4：iWorkerCenter 算力来源切换

**用户故事：** 作为 iWorkerCenter，我希望支持两种算力来源模式——默认从 iWorkerCloud 同步，或在获得授权后使用本地配置——以便灵活适应不同部署场景。

#### 验收标准

1. THE iWorkerCenter SHALL maintain a Compute_Source setting with two possible values: `cloud` (default) and `local`
2. WHILE Compute_Source is `cloud`, THE iWorkerCenter SHALL periodically (every 5 minutes) pull LLM_Provider configurations from iWorkerCloud via the compute providers API and use them as the active provider list for DiWorker requests
3. WHILE Compute_Source is `cloud`, THE iWorkerCenter SHALL disable the local LLM_Provider editing UI and display a read-only view of the cloud-synced providers
4. WHEN the iWorkerCenter receives `compute_permission: true` from iWorkerCloud, THE iWorkerCenter SHALL enable the option to switch Compute_Source to `local`
5. WHILE Compute_Source is `local`, THE iWorkerCenter SHALL use locally configured LLM_Provider records and stop syncing from iWorkerCloud
6. WHEN the iWorkerCenter receives `force_sync: true` from iWorkerCloud, THE iWorkerCenter SHALL switch Compute_Source back to `cloud`, discard local provider overrides, and apply the cloud-provided configuration
7. IF the iWorkerCenter cannot reach iWorkerCloud during a Provider_Sync attempt, THEN THE iWorkerCenter SHALL retain the last successfully synced provider list and retry on the next sync interval

### 需求 5：iWorkerCenter 本地 LLM 服务商管理

**用户故事：** 作为 iWorkerCenter 管理员，我希望在获得授权后能够在本地添加和管理 LLM 服务商配置，以便使用特定的模型服务。

#### 验收标准

1. WHILE Compute_Source is `local` and Compute_Permission is `true`, THE iWorkerCenter SHALL provide a CRUD interface for local LLM_Provider records with the same fields as the iWorkerCloud provider schema (name, base_url, api_key, protocol, user_agent, compute_type, model, enabled, priority, description)
2. THE iWorkerCenter SHALL store local LLM_Provider records in the local configuration file (`/etc/iworkercenter/settings.json` or path specified by `--config` flag), extending the existing `providers` array with the new fields (user_agent, compute_type)
3. THE iWorkerCenter SHALL provide a test connectivity function for local providers that sends a simple prompt and reports success/failure with latency
4. WHEN Compute_Source is `cloud`, THE iWorkerCenter SHALL display the cloud-synced providers in read-only mode with a label indicating "来自 iWorkerCloud"

### 需求 6：协议转换层

**用户故事：** 作为 iWorkerCenter，我希望将所有不同协议的 LLM 服务统一转换为 OpenAI API 协议，以便 DiWorker 客户端只需对接一种 API 格式。

#### 验收标准

1. THE Protocol_Adapter SHALL convert incoming OpenAI-format requests to the target provider's native protocol before forwarding, supporting at minimum: OpenAI (passthrough), Anthropic (Messages API), and Gemini (generateContent API)
2. THE Protocol_Adapter SHALL convert the target provider's native response back to OpenAI chat completion format before returning to DiWorker
3. WHEN forwarding a request to an Anthropic-protocol provider, THE Protocol_Adapter SHALL extract system messages into the Anthropic `system` parameter, set `anthropic-version` header, and use both `x-api-key` and `Authorization: Bearer` headers
4. WHEN forwarding a request to a Gemini-protocol provider, THE Protocol_Adapter SHALL convert the messages array to Gemini `contents` format, map `system` role to `systemInstruction`, and append the API key as a query parameter
5. THE Protocol_Adapter SHALL set the User-Agent header on outgoing requests to the value specified in the LLM_Provider's user_agent field
6. IF a provider returns a non-200 status code, THEN THE Protocol_Adapter SHALL convert the error response to OpenAI error format and propagate the HTTP status code
7. FOR ALL supported protocols, parsing a provider response then formatting it as OpenAI format then parsing the OpenAI format SHALL produce content equivalent to the original response (round-trip property)

### 需求 7：iWorkerCenter 算力管理 UI

**用户故事：** 作为 iWorkerCenter 管理员，我希望在 Web 管理面板的导航栏中看到一个"算力管理"标签页，以便快速访问算力配置界面。

#### 验收标准

1. THE iWorkerCenter admin web panel SHALL add a "算力管理" tab in the navigation, positioned after "模型调度" tab
2. WHEN the "算力管理" tab is selected, THE iWorkerCenter SHALL display the current Compute_Source mode (`cloud` or `local`) with a visual indicator
3. WHILE Compute_Source is `cloud`, THE iWorkerCenter SHALL display the synced provider list in read-only mode, the last sync timestamp, a manual "立即同步" (Sync Now) button, and the sync status (success/failure/pending)
4. WHILE Compute_Source is `local` and Compute_Permission is `true`, THE iWorkerCenter SHALL display an editable provider list with add, edit, delete, enable/disable, and test connectivity actions
5. WHEN Compute_Permission is `false` and the user attempts to switch to `local` mode, THE iWorkerCenter SHALL display a message: "需要 iWorkerCloud 管理员授予算力自管理权限"
6. THE iWorkerCenter SHALL display each provider's protocol type, compute_type, user_agent, enabled status, and last test result in the provider list

### 需求 8：iWorkerCloud 管理面板算力页面

**用户故事：** 作为 iWorkerCloud 管理员，我希望在 Cloud 管理面板中有一个专门的算力管理页面，以便集中管理所有 LLM 服务商和各 Center 的算力权限。

#### 验收标准

1. THE iWorkerCloud admin panel SHALL include a "算力管理" navigation entry in the sidebar
2. THE iWorkerCloud admin panel SHALL display a provider management section with a table listing all LLM_Provider records, showing name, protocol, compute_type, user_agent, enabled status, and action buttons (edit, delete, test, toggle)
3. THE iWorkerCloud admin panel SHALL display a center permissions section listing all registered iWorkerCenter instances with their current Compute_Permission status and a toggle to grant/revoke permission
4. WHEN an administrator clicks "测试连通性" (Test Connectivity) for a provider, THE iWorkerCloud admin panel SHALL call the test endpoint and display the result (success/failure, latency, error message if any)
5. THE iWorkerCloud admin panel SHALL provide a form for adding/editing LLM_Provider records with fields for: name, base_url, api_key (password input), protocol (dropdown), user_agent (text with presets: `openclaw`, `claude-code/2.0.0`), compute_type (dropdown), model, priority, description, and enabled toggle

### 需求 9：Token 用量记录（双端记录）

**用户故事：** 作为系统，我希望 iWorkerCloud 和 iWorkerCenter 都在各自的转发层自动记录 token 用量数据，Cloud 用于全局精算，Center 用于按 DiWorker 的实时统计。

#### 验收标准

1. WHEN iWorkerCloud forwards a request to an upstream LLM provider and receives a response, THE iWorkerCloud SHALL create a Token_Usage_Record containing: input_tokens, output_tokens, total_tokens, provider_name, model, center_id, diworker_id, and timestamp (UTC)
2. WHEN iWorkerCenter forwards a DiWorker request (via iWorkerCloud or directly to a local provider) and receives a response, THE iWorkerCenter SHALL create a local Token_Usage_Record containing: input_tokens, output_tokens, total_tokens, provider_name, model, diworker_id, and timestamp (UTC)
3. BOTH iWorkerCloud and iWorkerCenter SHALL extract token counts from the provider's response: for OpenAI-protocol from the `usage` object, for Anthropic-protocol from the `usage` object, and for Gemini-protocol from the `usageMetadata` object
4. IF the provider response does not include token usage information, THEN the recording side SHALL estimate token counts using a character-based approximation and mark the record with `estimated: true`
5. THE iWorkerCloud SHALL store Token_Usage_Record entries in SQLite with indexes on center_id, diworker_id, and timestamp
6. THE iWorkerCenter SHALL store Token_Usage_Record entries in local SQLite with indexes on diworker_id and timestamp
7. THE iWorkerCenter SHALL use its own locally recorded data as the primary source for daily DiWorker-level usage statistics and real-time display

### 需求 10：MToken 单价配置

**用户故事：** 作为 iWorkerCloud 管理员，我希望为每个 LLM 服务商配置输入和输出的每百万 token 单价，以便系统自动计算费用。

#### 验收标准

1. THE iWorkerCloud SHALL extend the LLM_Provider record with two additional fields: input_price_per_mtoken (decimal, currency amount per million input tokens) and output_price_per_mtoken (decimal, currency amount per million output tokens)
2. WHEN an administrator creates or updates an LLM_Provider, THE iWorkerCloud SHALL validate that input_price_per_mtoken and output_price_per_mtoken are non-negative decimal values
3. THE iWorkerCloud SHALL include input_price_per_mtoken and output_price_per_mtoken in the compute providers API response to iWorkerCenter, so that iWorkerCenter can perform local cost calculations
4. THE iWorkerCloud admin panel SHALL display input_price_per_mtoken and output_price_per_mtoken fields in the LLM_Provider add/edit form, with labels "输入单价 (每MToken)" and "输出单价 (每MToken)"
5. WHEN MToken_Price values are updated for an LLM_Provider, THE iWorkerCloud SHALL apply the new prices to future cost calculations only; historical Cost_Summary records SHALL retain the prices effective at the time of calculation

### 需求 11：iWorkerCloud 按 Center 费用统计

**用户故事：** 作为 iWorkerCloud 管理员，我希望按每个 iWorkerCenter 查看每日和每月的 token 用量和费用统计，以便进行精算和成本分摊。

#### 验收标准

1. THE iWorkerCloud SHALL compute Center_Cost_Report by aggregating its locally stored Token_Usage_Record data per center_id (since all LLM requests are forwarded through iWorkerCloud, usage data is recorded directly at the Cloud layer without requiring Center to report)
2. THE iWorkerCloud SHALL calculate cost as: `input_cost = input_tokens × input_price_per_mtoken / 1,000,000` and `output_cost = output_tokens × output_price_per_mtoken / 1,000,000`
3. THE iWorkerCloud SHALL support querying Center_Cost_Report with daily and monthly granularity via `GET /api/stats/center-costs?center_id={id}&period=daily|monthly&start={date}&end={date}`
4. WHEN an administrator queries Center_Cost_Report without specifying a center_id, THE iWorkerCloud SHALL return aggregated statistics for all centers with per-center breakdown
5. THE iWorkerCloud SHALL generate daily Center_Cost_Report summaries at 00:05 UTC each day for the previous day, and monthly summaries on the 1st of each month for the previous month

### 需求 12：iWorkerCenter 按 DiWorker 费用统计

**用户故事：** 作为 iWorkerCenter 管理员，我希望按每个 DiWorker 查看 token 用量和费用统计，以便计算每个数字员工的投入产出。

#### 验收标准

1. THE iWorkerCenter SHALL compute DiWorker_Cost_Report by aggregating its own locally recorded Token_Usage_Record data per diworker_id, calculating cost using the same formula as Center_Cost_Report
2. THE iWorkerCenter SHALL support querying DiWorker_Cost_Report with daily and monthly granularity, filterable by diworker_id and date range
3. THE iWorkerCenter SHALL display each DiWorker_Cost_Report entry with: diworker_id, diworker_name, total_input_tokens, total_output_tokens, total_tokens, input_cost, output_cost, total_cost, and request_count
4. THE iWorkerCenter SHALL generate daily DiWorker_Cost_Report summaries at 00:05 local time each day for the previous day, and monthly summaries on the 1st of each month for the previous month
5. THE iWorkerCenter SHALL include per-provider breakdown in DiWorker_Cost_Report, showing token usage and cost split by each LLM_Provider used
6. WHEN generating monthly summaries, THE iWorkerCenter SHALL pull the corresponding monthly aggregated data from iWorkerCloud via `GET /api/centers/{id}/monthly-usage?month={YYYY-MM}` and compare it against the local monthly totals, displaying any discrepancy as a reconciliation indicator (e.g. "本地: 1,234,567 tokens / Cloud: 1,234,890 tokens")

### 需求 13：Token 用量与费用统计 UI

**用户故事：** 作为管理员，我希望在 iWorkerCloud 和 iWorkerCenter 的管理面板中直观地查看 token 用量和费用统计数据，以便快速了解算力消耗情况。

#### 验收标准

1. THE iWorkerCloud admin panel SHALL add a "用量统计" (Usage Statistics) sub-tab under the "算力管理" page, displaying Center_Cost_Report data in a table with columns: center_name, total_input_tokens, total_output_tokens, total_tokens, input_cost, output_cost, total_cost
2. THE iWorkerCloud admin panel SHALL provide date range picker and period selector (daily/monthly) controls for filtering Center_Cost_Report data
3. THE iWorkerCloud admin panel SHALL display a summary row at the top showing the total cost across all centers for the selected period
4. THE iWorkerCenter admin web panel SHALL add a "用量统计" (Usage Statistics) sub-tab under the "算力管理" page, displaying DiWorker_Cost_Report data in a table with columns: diworker_name, total_input_tokens, total_output_tokens, total_tokens, input_cost, output_cost, total_cost, request_count
5. THE iWorkerCenter admin web panel SHALL provide date range picker and period selector (daily/monthly) controls for filtering DiWorker_Cost_Report data
6. WHEN an administrator clicks on a specific DiWorker row in the iWorkerCenter usage table, THE iWorkerCenter SHALL display a detail view showing per-provider token usage and cost breakdown
7. WHEN an administrator clicks on a specific center row in the iWorkerCloud usage table, THE iWorkerCloud admin panel SHALL display a detail view showing per-provider token usage and cost breakdown for that center
8. THE iWorkerCloud admin panel SHALL display a line chart (trend curve) showing daily token usage (total_tokens) and daily cost (total_cost) over the selected date range, with the ability to toggle between per-center lines and an aggregate total line
9. WHEN the period selector is set to "monthly", THE iWorkerCloud admin panel SHALL display a bar chart showing monthly token usage and cost for each center, enabling month-over-month comparison
10. THE iWorkerCenter admin web panel SHALL display a line chart (trend curve) showing daily token usage and daily cost over the selected date range, with the ability to toggle between per-DiWorker lines and an aggregate total line
11. WHEN the period selector is set to "monthly", THE iWorkerCenter admin web panel SHALL display a bar chart showing monthly token usage and cost for each DiWorker, enabling month-over-month comparison
12. THE trend charts on both iWorkerCloud and iWorkerCenter SHALL support hovering to display tooltip with exact values (date, token count, cost) for each data point
