# 网页搜索策略与多引擎配置设计

## 1. 目标

把当前“选择一个 provider；失败后按固定顺序兜底”的网页搜索，升级为用户可理解、可调整、在中国大陆网络也能正常工作的搜索策略。

本设计覆盖桌面端 AI Assistant 调用的 `web_search` 工具，以及配置页中的搜索设置。它不改变 `web_fetch` 的基本语义；仅在 TinyFish 被选中且具备 Key 时，继续复用其增强抓取能力。

### 目标

- 未配置任何 API Key 时，用户仍然可以使用免费搜索。
- Google 是一等搜索源：用户可启用、禁用、调整到任意优先级（包括第一位）。
- 用户可调整搜索源顺序，并能一键恢复为指定预设的默认顺序。
- 失败、超时、验证页或结果不足时，系统快速切换下一搜索源。
- 浏览器搜索保留为受控能力：可作为“Google 网页版”等配置项的执行方式，也可作为所有 HTTP 搜索失败后的最终兜底。
- 搜索执行不抢占用户正在使用的页面，不默认携带登录 Cookie。
- 保持旧配置可读取，升级后不需要用户重新填写已有 API Key。

### 非目标

- 第一阶段不做基于 LLM 的结果判断、摘要质量打分或个性化推荐。
- 第一阶段不承诺每一个公共搜索站点都有稳定的 HTTP 抓取实现；对 Google 等容易触发验证的网站，优先经隔离浏览器执行。
- 不通过 IP 地址强制判定用户所在地区；地区只影响首次默认预设，用户随时可切换。

## 2. 当前实现与问题

当前入口在 `gui/im_tools_misc.go` 的 `toolWebSearch`：它从配置取出一个 `WebSearchCurrentProvider`，只调用该 provider。

核心实现位于 `corelib/websearch/search.go`：

1. 已选择 API provider（Brave、Serper、TinyFish、Tavily）有 Key 时，调用一次 API；DuckDuckGo 使用 HTML 页面。
2. provider 缺少 Key、失败或返回空结果时，进入固定 HTTP 兜底链：`Bing CN -> Baidu -> DuckDuckGo HTML`。
3. 直连链拿到第一批非空结果立即返回；每项最多 5 秒。
4. 进程内只记录“上次成功的直连端点”，下一次将它提到首位。该状态不持久化，也不按网络、语言或查询类型区分。
5. 所有 HTTP 路径失败后，才可调用 GUI 注册的浏览器搜索 hook。

这导致以下问题：配置 UI 是单选语义；用户不能定义免费引擎顺序；Google 不是可选搜索源；搜索结果不跨引擎聚合、去重或解释降级原因；单个被墙或触发验证的引擎会消耗过多等待时间。

## 3. 设计原则

1. **配置优先。** 用户排序是最高优先级；自动健康度只在同一优先级的候选选择和降级决策中发挥作用，不能悄悄改写用户顺序。
2. **免费可用。** 不把 API Key 作为能否搜索的前提。
3. **快速失败。** 对已知容易验证或不可达的公共搜索页使用较短超时，尽早切下一个。
4. **浏览器隔离。** 浏览器搜索默认使用后台隔离上下文，不激活前台标签，不读取现有登录态。用户可显式开启人工辅助验证；遇到验证码或滑块时可将验证页交给用户手动完成，但系统不得自动破解、代做或绕过验证。
5. **可观测。** 每次搜索记录所选源、执行方式、耗时、结果数和降级原因，但不记录 API Key、Cookie 或完整网页正文。
6. **渐进交付。** 先实现可靠的“排序 + 顺序降级”；再加入并发补发与聚合重排，避免一次性更改过大。

## 4. 概念模型

### 4.1 搜索源（Search Engine）与执行方式（Transport）

搜索源是用户理解的引擎，例如 Bing CN、百度、Google、DuckDuckGo、Brave。执行方式是实际请求通道：

- `api`：调用厂商或代理搜索 API，需要 Key；例如 Brave、Serper、TinyFish、Tavily。
- `http_html`：以 HTTP 请求读取公开结果页；例如 Bing CN、百度、DuckDuckGo。
- `browser`：由受管浏览器在隔离后台页执行；Google 免费网页版应使用这一方式。

一个搜索源可以在未来支持多个执行方式，但一个已配置条目在第一阶段只选择一个 `transport`，使行为和排障结果明确。

`Serper` 是 Google 结果的 API 服务；`Google` 则代表免费网页搜索。二者必须是不同条目，避免“选了 Google 却要求填 Serper Key”的歧义。

### 4.2 内置搜索源

内置条目固定 `id`，用户只能启用、排序及填写允许的配置字段，不能修改 `id`。首期内置如下：

| id | 名称 | 默认执行方式 | 是否需 Key | 说明 |
| --- | --- | --- | --- | --- |
| `bing_cn` | Bing | `http_html` | 否 | 中国大陆优先预设的首选免费源。 |
| `baidu` | 百度 | `http_html` | 否 | 中文、本地内容补充。 |
| `google` | Google | `browser` | 否 | 可排第一；若网络不可达或验证，快速降级。 |
| `duckduckgo` | DuckDuckGo | `http_html` | 否 | 免费国际搜索。 |
| `brave` | Brave Search API | `api` | 是 | 稳定 API 搜索。 |
| `serper` | Serper（Google API） | `api` | 是 | Google 结果的 API 通道。 |
| `tinyfish` | TinyFish | `api` | 是 | 搜索及可选增强抓取。 |
| `tavily` | Tavily | `api` | 是 | 面向检索/Agent 的 API。 |

后续如果开放自定义源，必须单独设计 SSRF、认证字段加密、解析器和审核策略；不纳入本次范围。

## 5. 配置模型与兼容迁移

### 5.1 新配置

在 `corelib.AppConfig` 增加 `WebSearchStrategy`，保留原字段以读取历史配置。建议 Go 模型如下：

```go
type WebSearchStrategy struct {
    Version                 int                      `json:"version"`
    Preset                  string                   `json:"preset"` // mainland, international, custom
    Mode                    string                   `json:"mode"`   // priority, smart, aggregate
    Engines                 []WebSearchEngineConfig  `json:"engines"`
    BrowserFallbackEnabled  bool                     `json:"browser_fallback_enabled"`
    BrowserFallbackEngineID string                   `json:"browser_fallback_engine_id"`
    HedgingDelayMS          int                      `json:"hedging_delay_ms"`
    MinResultsBeforeHedge   int                      `json:"min_results_before_hedge"`
}

type WebSearchEngineConfig struct {
    ID       string `json:"id"`
    Enabled  bool   `json:"enabled"`
    Priority int    `json:"priority"`  // 数值越小越靠前，保存时重新编号
    Transport string `json:"transport"` // api, http_html, browser
    APIKey   string `json:"api_key,omitempty"`
    BaseURL  string `json:"base_url,omitempty"`
}
```

API Key 在现有安全配置存储机制可用时应写入密钥存储，`AppConfig` 只保存引用；在迁移期可继续兼容既有 `WebSearchProvider.Key`，但日志和 Wails 返回值必须遮蔽 Key。

### 5.2 预设与默认顺序

新安装默认使用 `mainland`，不依据 IP 强制切换。默认启用的免费链如下：

| 预设 | 默认顺序 |
| --- | --- |
| `mainland`（中国大陆优先） | Bing → 百度 → DuckDuckGo → Google |
| `international`（国际网络优先） | Google → DuckDuckGo → Bing → 百度 |

默认不启用尚未配置 Key 的 API 源。用户填入 Key 并通过测试后，可以启用并拖到任意位置。Google 即使在 `mainland` 默认靠后，也可被用户移动到第一位。

点击“重置默认顺序”时：

- 恢复当前选中预设的引擎启用状态与顺序；
- 保留所有已填写的 API Key 和自定义 Base URL；
- 不自动启用没有 Key 的 API 源；
- 将 `Preset` 设置为当前预设，不覆盖用户的浏览器兜底开关。

只要用户拖动顺序、开关引擎或改模式，`Preset` 变为 `custom`；用户也可显式选择预设后再次重置。

### 5.3 旧配置迁移

旧字段：`WebSearchProviders` 和 `WebSearchCurrentProvider`。

迁移规则：

1. 若 `WebSearchStrategy.Version >= 1`，直接使用新配置。
2. 否则创建 `mainland` 默认策略，并把旧 providers 的 Key、BaseURL 覆盖到同类型 API 引擎。
3. 旧 `CurrentProvider` 若匹配一个内置 API/免费源，则将它移到策略第一位并启用；若为空，保持预设顺序。
4. 成功保存新策略后仍保留旧字段一个发布周期，便于旧版本回滚；新代码只写新字段。
5. 下一个大版本移除旧字段前，提供一次配置导出和迁移测试。

## 6. 搜索调度

### 6.1 对外接口

将现有单一 provider 接口扩展为：

```go
func SearchWithStrategyCtx(
    ctx context.Context,
    query string,
    maxResults int,
    strategy corelib.WebSearchStrategy,
) (SearchResponse, error)

type SearchResponse struct {
    Results      []SearchResult       `json:"results"`
    Diagnostics  []SearchAttempt      `json:"diagnostics,omitempty"`
    Degraded     bool                 `json:"degraded"`
}

type SearchAttempt struct {
    EngineID     string        `json:"engine_id"`
    Transport    string        `json:"transport"`
    DurationMS   int64         `json:"duration_ms"`
    ResultCount  int           `json:"result_count"`
    Outcome      string        `json:"outcome"` // success, empty, timeout, blocked, error, skipped
    FallbackNote string        `json:"fallback_note,omitempty"`
}
```

`toolWebSearch` 仍输出兼容的文本结果；诊断信息只写日志和后续可选的调试 UI，不直接暴露给模型。这样不会改变现有 Agent 工具协议。

### 6.2 第一阶段：优先级顺序降级

第一阶段的执行算法：

```text
规范化 query 和 max_results
读取启用的引擎，按 Priority 升序排列
跳过缺少 Key 的 API 引擎并记为 skipped

依次执行引擎：
  为该引擎创建受控超时上下文
  执行 API / HTTP / browser transport
  若被识别为验证码、反爬页、空结果、超时或错误：记录失败并尝试下一个
  若结果数 >= min(max_results, 3)：规范化、去重并立即返回
  若仅 1-2 条结果：暂存为候选，继续尝试下一引擎

若已有候选结果：去重后返回，并标记 Degraded=true
若全部启用引擎失败且 BrowserFallbackEnabled：
  用 BrowserFallbackEngineID 在隔离浏览器中执行一次最终兜底
若仍失败：返回包含分源诊断的错误
```

当前超时与重试预算：普通搜索总预算 30 秒；HTTP/API 单次 6 秒；浏览器条目单次 6 秒。HTTP/API 遇到超时、DNS、TLS、连接中断或 HTTP 5xx 等适合立即恢复的瞬态错误时，等待约 200 毫秒后最多重试一次；认证失败、HTTP 429 限流、验证码/拦截和浏览器路径不自动重试。429 只有在后续实现并遵守 `Retry-After` 后才应进入自动重试，避免立即重试再次消耗配额。设置页的单引擎测试使用独立冷启动预算（HTTP/API 12 秒、浏览器 30 秒），避免把首次 DNS、代理连接或 TLS 握手误判为不可用。所有尝试仍必须尊重父级总预算。

第一阶段只合并“结果不足时”的少量候选；正常情况下保留用户首选源的原始排序，结果更可预测。

### 6.3 第二阶段：智能与聚合模式

在第一阶段稳定并拥有遥测数据后启用两个可选模式：

- `smart`：仍尊重用户优先级，但结合最近 15 分钟的成功率、P95 延迟和空结果率跳过临时熔断的引擎；不自动把低优先级引擎提升到高优先级之前。
- `aggregate`：先启动第一引擎；在 `HedgingDelayMS`（默认 500ms）后，如果尚无至少 `MinResultsBeforeHedge`（默认 3）条可用结果，启动下一引擎。最多同时 2 个请求、最多查询 3 个引擎，达到足够候选或总预算耗尽即取消剩余请求。

聚合结果处理：

1. 规范化 URL：小写 host、去除 fragment、删除常见追踪参数、规范化尾部 `/`。
2. 相同规范化 URL 去重；保留优先级更高的来源，补充较好的标题/摘要。
3. 同一 host 最多保留 2 条结果，避免单域名垄断。
4. 稳定排序：优先级桶、源内排名、是否有摘要、来源多样性。第一版不引入不可解释的语义相关性分数。
5. 取前 `maxResults` 条。

## 7. 浏览器搜索与 Google

### 7.1 Google 作为可排序条目

Google 的内置条目为 `{ id: "google", transport: "browser" }`。它与其他搜索源一样参与排序：用户把它置顶时，调度器第一步就执行 Google 浏览器搜索；失败后继续下一项。

浏览器适配器必须明确接受 `engineID`，不能再由底层隐式地在 Bing/Google 之间决定：

```go
type BrowserSearchProvider func(
    ctx context.Context,
    engineID string,
    query string,
    maxResults int,
    opts BrowserSearchOptions,
) ([]BrowserSearchHit, error)
```

首期浏览器适配器支持 `google` 和 `bing_cn`；不支持的 `engineID` 必须返回结构化错误而非私自换站点。

### 7.2 隐私、安全与交互约束

- 默认使用后台、无痕/隔离上下文；不切换用户当前活动标签页。
- 默认不注入现有浏览器 Cookie、账号或扩展状态。
- 只有 `web_fetch(use_browser_cookies=true)` 这类既有显式授权路径才能使用登录态；`web_search` 不继承该授权。
- 遇到验证码或人工验证，立即判为 `blocked`，不尝试自动绕过。开启“人工辅助验证”时，激活隔离验证页并等待用户手动完成，完成后自动继续提取结果；关闭时直接继续下一搜索源。验证成功或等待超时后必须清理该隔离页。
- 浏览器不可用时，将该引擎记为失败并继续，不使整次搜索失败。
- 搜索配置页以文字说明：Google 网页版可能受网络、验证页或地区限制影响；排第一表示优先尝试，不表示必然可访问。

## 8. 配置 UI 设计

修改现有 `gui/frontend/src/components/remote/WebSearchConfigPanel.tsx`，从单选 provider 卡片升级为“搜索策略”页面。

### 8.1 基础区

- **预设**：中国大陆优先、国际网络优先、自定义。
- **搜索模式**：优先级（首期上线）、智能、聚合；后两项在尚未实现时隐藏或禁用，并标注“即将推出”。
- **重置默认顺序**：弹出确认，说明会恢复当前预设顺序和启用状态、保留 Key。
- **允许浏览器最终兜底**：开关，默认开启；可选下拉框指定 Bing 或 Google 作为最终兜底源。
- **允许人工辅助验证**：默认关闭。用户明确开启后，遇到验证码或滑块才允许将隔离验证页带到前台并延长等待时间。

### 8.2 引擎排序列表

每一行包含：拖拽手柄、启用开关、名称、执行方式徽标、状态、配置入口。

```text
搜索引擎（拖动调整尝试顺序）
  ☰  [开]  1  Google             浏览器   可用性取决于网络      [测试] [设置]
  ☰  [开]  2  Bing               网页直连 已启用                [测试]
  ☰  [开]  3  百度               网页直连 已启用                [测试]
  ☰  [关]  4  Brave Search API   API      需要 API Key           [设置]
```

- API 引擎无 Key 时可保留在列表中，但启用开关旁显示“需要 Key”；保存时不允许把它保持为启用状态，或自动转为关闭并给出明确反馈。推荐前者，防止“看似已启用、实际永远跳过”。
- Google、Bing、百度、DuckDuckGo 均无需 Key，因此永远可供用户开启。
- 移动端采用上移/下移按钮替代拖拽，键盘操作提供“上移/下移”按钮和 aria-live 顺序提示。

### 8.3 测试与状态

- “测试”只测试该行的实际 transport；不能用 HTTP 测试代替 Google 浏览器测试。
- 测试结果展示引擎、执行方式、耗时、结果数和可读错误原因；不展示密钥或完整响应。
- 近期健康状态为只读信息：最近一次成功时间、最近 20 次成功率、P50 延迟。未启用 telemetry 时不展示虚构统计。

### 8.4 保存规则

保存时前端提交完整策略，后端再次校验：内置 ID、唯一性、transport 与 ID 是否兼容、排序连续性、API Key 必填性、超时范围。后端负责最终归一化，不信任前端。

## 9. Wails 接口与后端边界

现有接口保留一个发布周期，新增：

```go
func (a *App) GetWebSearchStrategy() WebSearchStrategyView
func (a *App) SaveWebSearchStrategy(req SaveWebSearchStrategyRequest) error
func (a *App) ResetWebSearchStrategy(preset string) WebSearchStrategyView
func (a *App) TestWebSearchEngine(engine corelib.WebSearchEngineConfig) WebSearchEngineTestResult
func (a *App) GetWebSearchHealth() []WebSearchEngineHealth // 后续阶段
```

`WebSearchStrategyView` 不返回明文 Key，只返回 `has_api_key`。保存请求可单独携带用户刚输入的 Key；后端写入安全存储后从响应中抹除。

代码职责：

| 层 | 职责 |
| --- | --- |
| `corelib` | 策略数据模型、校验、调度、去重、结果和尝试诊断。 |
| `corelib/websearch` | 各 API/HTTP transport、浏览器 adapter 接口、错误分类。 |
| `gui` | 配置迁移/持久化、浏览器实现注入、Wails API、日志与指标。 |
| `gui/frontend` | 排序/启用/预设/测试交互，不决定真实调度。 |
| `gui/im_tools_misc.go` | 读取策略并调用 `SearchWithStrategyCtx`，保持工具文本协议。 |

## 10. 观测与错误分类

每次尝试输出结构化日志字段：`query_hash`、`engine_id`、`transport`、`elapsed_ms`、`result_count`、`retry_count`、`outcome`、`fallback_from`。`query_hash` 使用带本机随机 salt 的哈希，避免在普通日志保留明文搜索词。

统一错误分类：

- `timeout`
- `network_unreachable`
- `rate_limited`
- `blocked`（验证码、反爬挑战、疑似验证页）
- `invalid_key`
- `no_results`
- `parse_error`
- `browser_unavailable`
- `unsupported_engine`

聚合模式和健康评分只消费这些分类，不解析任意错误文本作业务决策。

## 11. 测试计划

### 单元测试

- 默认预设顺序、重置逻辑、用户将 Google 移至第一位。
- 旧 `WebSearchProviders` / `WebSearchCurrentProvider` 的迁移，Key 与 BaseURL 保留。
- 策略校验：重复 ID、未知 ID、非法 transport、API 源缺 Key、无启用免费源。
- 调度：成功即停、空结果继续、超时继续、验证码继续、总预算生效、浏览器最终兜底开关。
- Google 浏览器条目第一位时，先调用 `browser(engineID="google")`，不得改为 Bing。
- URL 规范化、去重、同域名上限和稳定排序。
- 现有单 provider `SearchWithProvider` 回归测试保持通过。

### 集成测试

- Wails 获取、保存、重置、遮蔽 Key 的契约测试。
- `toolWebSearch` 在无 API Key 的全免费策略下仍返回结果或分源失败信息。
- 浏览器 adapter 不可用、超时、返回验证码时不影响后续 HTTP/API 引擎。
- 配置页保存后重启应用，顺序和开关不丢失。

### 手工验收

1. 新安装、零 Key：大陆预设可直接搜索。
2. 将 Google 拖到第一：日志显示 Google 先被尝试，失败后转到下一项。
3. 关闭浏览器兜底：所有 HTTP/API 失败时不启动浏览器。
4. 重置默认：恢复预设顺序，已填 API Key 仍在。
5. 用户前台浏览网页时执行搜索：不切换、不关闭、不污染当前标签页。
6. 开启人工辅助后触发验证码或滑块：验证页被带到前台；用户完成后搜索自动继续，应用不执行任何代答、拖拽或绕过操作。

## 12. 分期实施

### Phase 1：策略配置与顺序降级

- 新策略模型、旧配置迁移、预设与重置。
- 免费引擎条目和 Google 浏览器条目。
- 配置 UI：启用、排序、预设、重置、测试、浏览器最终兜底开关。
- 顺序调度、错误分类、基础日志、URL 去重。
- 完成单元/集成/手工验收。

### Phase 2：聚合和健康度

- 受总预算限制的并发补发。
- 去重后稳定重排、域名多样性。
- 短期健康度、熔断和配置页状态展示。
- `smart` 与 `aggregate` 模式开放。

### Phase 3：质量与运营

- 可选的结果质量反馈与匿名聚合指标。
- 基于查询语言的默认预设建议（只建议，不强制切换）。
- 评估新增公开引擎或自定义源的安全设计。

## 13. 验收标准

- 用户不填任何 API Key，也能在配置 UI 中选择、排序并使用免费搜索源。
- Google 可启用并排在任意位置；排第一时真实执行顺序为 Google 优先。
- 在大陆网络中 Google 失败不会阻断后续 Bing/百度等引擎；浏览器条目最多使用 6 秒的普通搜索预算，随后继续下一项。
- 用户可以一键恢复大陆或国际预设；重置不清除已保存 API Key。
- 浏览器仅在用户启用的浏览器条目或明确允许的最终兜底路径中运行，且默认隔离、不抢前台、不使用登录态。
- 旧用户升级后，既有 provider Key 与当前选项可迁移，现有 `web_search` 工具调用不破坏。

