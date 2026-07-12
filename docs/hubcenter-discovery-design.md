# HubCenter 自动发现与最优选择 + 邮箱关联 Hub 高效查找

> **实施状态**：Phase 1 已实现 | Phase 2 已实现 | Phase 3 待实施

## 现状分析

### 当前架构

```
MacLaw 客户端
    ↓ (1) POST /api/entry/resolve {email}
HubCenter (单一 URL: hubs.mypapers.top:9388)
    ↓ (2) 返回 hub 列表
MacLaw 选择 hub → WebSocket 连接
```

### 三台 HubCenter 服务器

| 节点 | URL | 角色 |
|------|-----|------|
| 节点 1 | `hubs.mypapers.top` | 默认（硬编码在 `corelib/remote/defaults.go`） |
| 节点 2 | `hubs.maclaw.top` | HA 对等节点 |
| 节点 3 | `hubs2.maclaw.top` | HA 对等节点 |

### 现有机制

**Hub 服务器侧**（`hub/internal/center/service.go`）已有完整的多 HubCenter 质量探测：
- `orderedCenterBaseURLs()` 并发探测所有 HubCenter 节点
- `probeCenterQuality()` 调用 `GET /api/client/quality` 获取质量分数
- 按 Reachable → Routable → QualityScore 排序选择最优节点
- 注册和心跳自动 fallback 到次优节点

**MacLaw 客户端侧**（`gui/remote_activation.go`）**缺失此机制**：
- `resolveRemoteHubCenter()` 只用单一 `centerURL`，无 fallback
- `defaultRemoteHubCenterURL` 硬编码单一 URL
- 节点 1 不可达时，整个 hub 发现流程失败

### 问题本质

Hub 服务器和 MacLaw 客户端面对同一个问题（"从多台 HubCenter 中选最优"），但 Hub 服务器有完整解决方案，MacLaw 客户端没有。这是**同一机制的两个消费方，只实现了一个**。

---

## 问题 1：MacLaw 客户端缺少多 HubCenter 质量探测

### 根因

`resolveRemoteHubCenter()` 接受单一 `centerURL`，直接 POST 请求。无 fallback、无质量探测、无缓存。

### 修复：复用 Hub 服务器侧的质量探测模式

#### 1.1 默认 HubCenter URL 列表（`corelib/remote/defaults.go`）

从单一 URL 升级为有序列表：

```go
// DefaultRemoteHubCenterURLs is the ordered list of hub center URLs.
// MacLaw client and hub server both use this list for discovery.
var DefaultRemoteHubCenterURLs = []string{
    "https://hubs.mypapers.top",
    "https://hubs.maclaw.top",
    "https://hubs2.maclaw.top",
}

// DefaultRemoteHubCenterURL is kept for backward compatibility.
// New code should use DefaultRemoteHubCenterURLs.
var DefaultRemoteHubCenterURL = DefaultRemoteHubCenterURLs[0]
```

#### 1.2 共享质量探测器（`corelib/remote/hubcenter_probe.go`，新文件）

提取 Hub 服务器侧的 `probeCenterQuality` + `orderedCenterBaseURLs` 到 corelib 共享层：

```go
package remote

// CenterQuality 是 /api/client/quality 的响应子集。
type CenterQuality struct {
    Reachable    bool
    Routable     bool
    QualityScore int
    CanResolve   bool
    RTTMs        int64
}

// ProbeCenterQuality 探测单个 HubCenter 节点的质量。
// 超时 2 秒，与 hub/internal/center/service.go 的 probeCenterQuality 行为一致。
func ProbeCenterQuality(ctx context.Context, client *http.Client, baseURL string) CenterQuality

// SelectBestCenter 并发探测所有 HubCenter 节点，返回按质量排序的 URL 列表。
// preferred 是上次成功使用的 URL（加 5 分偏好）。
// 与 hub/internal/center/service.go 的 orderedCenterBaseURLs 排序逻辑一致。
func SelectBestCenter(ctx context.Context, client *http.Client, urls []string, preferred string) []string
```

**并发探测**：对所有 URL 并发发送 `GET /api/client/quality`，2 秒超时。排序规则与 Hub 服务器侧完全一致：
1. Reachable（可达优先）
2. Routable（可路由优先）
3. QualityScore + Preferred 偏好（上次成功的 URL +5 分）
4. 字母序兜底

#### 1.3 `resolveRemoteHubCenter` 改造（`gui/remote_activation.go`）

```go
func (a *App) resolveRemoteHubCenter(centerURL string, email string, cfg corelib.AppConfig) (hubCenterResolveResult, error) {
    // 构建候选 URL 列表
    urls := buildCenterURLList(centerURL, cfg)

    // 质量探测 + 排序
    ordered := remote.SelectBestCenter(context.Background(), hubHTTPClient, urls, cfg.RemoteHubCenterURL)
    if len(ordered) == 0 {
        return hubCenterResolveResult{}, fmt.Errorf("no reachable hub center")
    }

    // 按质量顺序尝试 resolve
    payload, _ := json.Marshal(map[string]string{"email": email})
    var lastErr error
    for _, url := range ordered {
        resp, err := hubHTTPClient.Post(url+"/api/entry/resolve", "application/json", bytes.NewReader(payload))
        if err != nil {
            lastErr = err
            continue
        }
        defer resp.Body.Close()
        var result hubCenterResolveResult
        if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
            lastErr = err
            continue
        }
        if resp.StatusCode >= 300 {
            lastErr = fmt.Errorf("%s", result.Message)
            continue
        }
        // 成功——记住这个 URL 供下次偏好
        if url != cfg.RemoteHubCenterURL {
            a.updateHubCenterURL(url)
        }
        return result, nil
    }
    return hubCenterResolveResult{}, fmt.Errorf("all hub centers failed: %w", lastErr)
}

func buildCenterURLList(explicit string, cfg corelib.AppConfig) []string {
    seen := make(map[string]bool)
    var urls []string
    add := func(u string) {
        u = strings.TrimRight(strings.TrimSpace(u), "/")
        if u != "" && !seen[u] {
            seen[u] = true
            urls = append(urls, u)
        }
    }
    add(explicit)
    add(cfg.RemoteHubCenterURL)
    for _, u := range remote.DefaultRemoteHubCenterURLs {
        add(u)
    }
    return urls
}
```

#### 1.4 质量缓存（避免每次 resolve 都探测）

`SelectBestCenter` 内部维护进程级缓存（`sync.Map`），TTL 15 秒（与 `/api/client/quality` 返回的 `ttl_seconds` 一致）。15 秒内重复调用直接返回缓存结果。

```go
var centerQualityCache struct {
    mu      sync.Mutex
    results []rankedCenter
    ts      time.Time
    ttl     time.Duration
}
```

---

## 问题 2：多台 Hub 注册在 HubCenter 时，高效查找邮箱关联的 Hub

### 现状

当前 `/api/entry/resolve` 的流程：

```
1. 查 blocked_emails → 是否被封禁
2. 查 hub_user_links → 获取用户绑定的 hub_id 列表
3. 查 hubs（全量 ListAll）→ 获取所有 hub 实例
4. 过滤 public/shared + online + !disabled
5. 排序：default_hub > shared > public > 字母序
6. 返回完整 hub 列表
```

**问题**：`ListAll()` 查询所有 hub 实例，当 HubCenter 注册了大量 hub（100+）时，每次 resolve 都全量扫描。而且 `ListByEmail` 的结果被丢弃了（第 2 步查了但没用），排序只用了 `defaultLinkHubID`。

### 协议改进方案

#### 方案 A：HubCenter 侧优化（推荐）

**不改协议**，优化 `ResolveByEmailFromIP` 的查询逻辑：

```go
func (s *Service) ResolveByEmailFromIP(ctx context.Context, email string, clientIP string) (*ResolveResult, error) {
    // ... IP/email 校验不变 ...

    // 第一优先级：用户直接绑定的 hub（通过 hub_user_links）
    boundHubs, err := s.hubs.ListByEmail(ctx, email)  // 已有方法，之前结果被丢弃
    if err != nil {
        return nil, err
    }

    defaultLinkHubID := ""
    if link, err := s.links.GetDefaultByEmail(ctx, email); err == nil && link != nil {
        defaultLinkHubID = link.HubID
    }

    // 构建结果：绑定的 hub 优先，然后是公开可发现的 hub
    byID := map[string]*store.HubInstance{}
    for _, hub := range boundHubs {
        if hub != nil && !hub.IsDisabled && hub.Status == "online" {
            byID[hub.ID] = hub
        }
    }

    // 补充公开 hub（用户未绑定但可发现的）
    allHubs, err := s.hubs.ListAll(ctx)
    if err != nil {
        return nil, err
    }
    for _, hub := range allHubs {
        if hub == nil || byID[hub.ID] != nil {
            continue
        }
        if isPubliclyDiscoverable(hub) && !hub.IsDisabled && hub.Status == "online" {
            byID[hub.ID] = hub
        }
    }

    // ... 排序和返回不变 ...
}
```

这个优化不改协议，但让绑定的 hub 不受 visibility 过滤影响——用户绑定的 private hub 也能被发现。

#### 方案 B：新增 `/api/entry/resolve-fast` 端点（可选，大规模场景）

当 HubCenter 注册了 1000+ hub 时，`ListAll` 的全量扫描成为瓶颈。新增轻量端点：

```
POST /api/entry/resolve-fast
Request:  {"email": "user@example.com"}
Response: {
    "email": "user@example.com",
    "mode": "bound",
    "default_hub": {
        "hub_id": "xxx",
        "name": "My Hub",
        "base_url": "https://hub.example.com",
        "status": "online"
    },
    "bound_hubs": [...],     // 仅用户绑定的 hub（通常 1-3 个）
    "public_count": 42       // 公开 hub 数量（不返回详情）
}
```

**区别**：
- `resolve`：返回所有可发现的 hub（绑定 + 公开），适合 UI 展示完整列表
- `resolve-fast`：只返回用户绑定的 hub + 公开 hub 数量，适合客户端自动连接

MacLaw 客户端自动连接场景只需要用户绑定的 hub（通常 1-3 个），不需要遍历所有公开 hub。

#### 方案 C：`/api/client/endpoints` 端点复用（已有）

HubCenter 已有 `GET /api/client/endpoints` 端点，返回所有 HA 节点信息。MacLaw 客户端可以用它做 HubCenter 节点发现：

```
GET /api/client/endpoints
Response: {
    "ok": true,
    "nodes": [
        {"node_id": "n1", "base_url": "https://hubs.mypapers.top", "quality_score": 98, "routable": true},
        {"node_id": "n2", "base_url": "https://hubs.maclaw.top", "quality_score": 95, "routable": true},
        {"node_id": "n3", "base_url": "https://hubs2.maclaw.top", "quality_score": 92, "routable": true}
    ],
    "ttl_seconds": 60
}
```

MacLaw 客户端可以先调用任一节点的 `/api/client/endpoints` 获取完整节点列表，然后对所有节点做质量探测。这比硬编码 URL 列表更灵活——新增 HubCenter 节点时客户端自动发现。

---

## 推荐实施方案

### Phase 1：MacLaw 客户端多 HubCenter 质量探测（核心）

**修改文件**：

| 文件 | 修改 |
|------|------|
| `corelib/remote/defaults.go` | 新增 `DefaultRemoteHubCenterURLs` 列表 |
| `corelib/remote/hubcenter_probe.go` | 新文件：`ProbeCenterQuality` + `SelectBestCenter`（从 hub 侧提取共享） |
| `gui/remote_activation.go` | `resolveRemoteHubCenter` 改为多 URL fallback + 质量排序 |
| `gui/remote_defaults.go` | `defaultRemoteHubCenterURL` 保留兼容，新增 `defaultRemoteHubCenterURLs` |
| `corelib/remote/hubcenter_probe_test.go` | 新文件：8 个测试 |
| `gui/remote_activation_test.go` | 更新现有 3 个测试 + 新增 2 个多 URL fallback 测试 |

**数据流（改造后）**：

```
MacLaw 客户端
    ↓ (1) 并发 GET /api/client/quality → 3 台 HubCenter
    ↓ (2) 按质量排序：[hubs.maclaw.top(98), hubs.mypapers.top(95), hubs2.maclaw.top(92)]
    ↓ (3) POST /api/entry/resolve {email} → hubs.maclaw.top（最优）
    ↓ (4) 失败 → fallback → hubs.mypapers.top
    ↓ (5) 返回 hub 列表
MacLaw 选择 hub → WebSocket 连接
```

### Phase 2：HubCenter 侧 resolve 优化

**修改文件**：

| 文件 | 修改 |
|------|------|
| `hubcenter/internal/entry/service.go` | `ResolveByEmailFromIP` 优先使用 `ListByEmail` 结果，private hub 对绑定用户可见 |

### Phase 3：动态节点发现（可选）

**修改文件**：

| 文件 | 修改 |
|------|------|
| `corelib/remote/hubcenter_probe.go` | `SelectBestCenter` 先调用 `/api/client/endpoints` 获取动态节点列表，与硬编码列表合并 |

---

## 回答你的问题

> 在 hubcenter 上注册有多台 hub 服务器的情况下，是不是要改进协议，才能高效的找到自己注册邮箱所关联的 hub 服务器？

**不需要改协议**。现有的 `/api/entry/resolve` 协议已经支持返回用户绑定的 hub 列表。需要改的是两个层面：

### 层面 1：MacLaw 客户端找不到最优 HubCenter（P0）

这是当前的**硬伤**——客户端只用单一 HubCenter URL，节点不可达就完全失败。Hub 服务器侧已有完整的多节点质量探测机制（`orderedCenterBaseURLs` + `probeCenterQuality`），客户端侧缺失。

**修复**：将 Hub 服务器侧的质量探测逻辑提取到 `corelib/remote/` 共享层，MacLaw 客户端复用。

### 层面 2：HubCenter 的 resolve 查询效率（P1）

当前 `ResolveByEmailFromIP` 调用了 `ListByEmail`（获取用户绑定的 hub）但**丢弃了结果**，然后 `ListAll` 全量扫描。这在 hub 数量少时无感，hub 数量多时（100+）是浪费。

**修复**：优先使用 `ListByEmail` 结果（用户绑定的 hub），补充公开 hub。不改协议，只改查询逻辑。

### 层面 3：大规模场景的协议优化（P2，可选）

如果未来 HubCenter 注册了 1000+ hub，可以新增 `/api/entry/resolve-fast` 端点，只返回用户绑定的 hub（通常 1-3 个），不遍历所有公开 hub。但当前规模不需要。

---

## 实施优先级

1. **Phase 1**（P0）：MacLaw 客户端多 HubCenter 质量探测 — 解决单点故障
2. **Phase 2**（P1）：HubCenter resolve 查询优化 — 解决 `ListByEmail` 结果被丢弃的问题
3. **Phase 3**（P2）：动态节点发现 — 新增 HubCenter 节点时客户端自动发现

Phase 1 是必须做的（当前是硬伤），Phase 2 是顺手优化，Phase 3 是未来扩展。
