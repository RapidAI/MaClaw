# HubCenter 接入优化方案

## 已实施的优化

### P0: 搜索与更新检查解耦 + 更新缓存 ✅

**文件**: `gui/hub_update_cache.go`（新文件）, `gui/skill_searcher.go`, `gui/app_wails_bindings.go`

**改动**:
1. `enrichInstalledState()` 不再调用 `checkHubSkillUpdatesSafe()`，改为从 `hubUpdateCache` 读取（零 HTTP 请求）
2. `CheckHubSkillUpdates()` 增加 10 分钟 TTL 缓存，缓存命中直接返回
3. 缓存过期时，更新检查从串行 N+1 改为并发（最多 3 并发）
4. Install/Update/Delete 操作后自动 invalidate 缓存

### P1: HubCenter URL 解析结果缓存 ✅

**文件**: `gui/hub_update_cache.go`, `gui/hubcenter_client_helper.go`

**改动**:
1. `resolvedHubCenterCache` 缓存已解析的 base URL，TTL 60 秒
2. `resolveHubCenterCandidates()` 改为调用 `resolveHubCenterBaseURLCached()`
3. 60 秒内的所有 HubCenter API 请求共享同一个已解析的 base URL

### P2: rememberHubCenterSelection 写盘节流 ✅

**文件**: `gui/hub_update_cache.go`, `gui/hubcenter_client_helper.go`

**改动**:
1. `rememberHubCenterSelection()` 改为调用 `rememberHubCenterSelectionThrottled()`
2. 只在 base URL 实际变化时写 config.json
3. nil cache 时（测试场景）回退到始终写盘（向后兼容）

### P2: RefreshRecommendations 启动异步化 ✅

**文件**: `gui/app.go`

**改动**: `RefreshRecommendations` 改为 goroutine + 8 秒超时，不阻塞 `ensureSkillHubClient()`

### P3: HubClient 连接复用单例 ✅

**文件**: `corelib/skill/hub_search.go`, 所有消费方

**改动**:
1. 新增 `DefaultHubClient()` 包级别单例，复用 HTTP Transport（TCP 连接池）
2. 所有消费方从 `NewHubClient()` 改为 `DefaultHubClient()`
3. `NewHubClient()` 保留用于测试场景

**消费方更新**: `gui/skill_searcher.go`, `gui/im_message_handler.go`, `tui/tool_manage_skill.go`, `tui/app.go`, `tui/commands/skill_search_api.go`, `tui/commands/skillhub.go`

## 问题量化

### 用户搜索一次 Skill 的实际请求链路

```
前端: SearchMixedSkills("pdf")
  → SkillSearcher.SearchAll()
    ├─ [1] SkillSearcher.Search() — SkillMarket API
    │   └─ getHubCenterJSON("/api/v1/skillmarket/search")
    │       └─ resolveHubCenterCandidates()
    │           └─ resolveHubCenterBaseURL()
    │               ├─ [probe] SelectBestCenter() — 并发探测 N 个节点
    │               │   └─ N × GET /api/client/quality  (每个 3s 超时)
    │               └─ [discover] FetchHubCenterDiscovery()
    │                   └─ GET /api/client/hubcenters  (4s 超时)
    │       └─ GET /api/v1/skillmarket/search?q=pdf
    │
    ├─ [2] HubClient.SearchClawHub() — 独立 HTTP
    │   └─ GET cn.clawhub-mirror.com/api/v1/search?q=pdf
    │
    ├─ [3] HubClient.SearchGitHub() — 独立 HTTP
    │   └─ GET api.github.com/search/repositories?q=pdf+topic:skill
    │
    └─ [4] enrichInstalledState()
        └─ checkHubSkillUpdatesSafe()
            └─ CheckHubSkillUpdates()
                └─ 遍历 K 个已安装 hub skill:
                    └─ K × SkillHubClient.CheckUpdate()
                        └─ K × getHubCenterJSON("/api/v1/skills/{id}")
                            └─ K × resolveHubCenterCandidates()  ← 每次都走！
```

**实际请求数**（假设 3 个 HubCenter 节点、5 个已安装 hub skill）：
- 探测请求: 3（SelectBestCenter，15s 缓存内复用）
- 发现请求: 1（FetchHubCenterDiscovery）
- 搜索请求: 1（SkillMarket）+ 1（ClawHub）+ 1（GitHub）= 3
- 更新检查: 5（每个 skill 一次）
- **总计: 12 次 HTTP 请求，其中 5 次是 N+1 更新检查**

### 前端切换到 Hub Tab 的请求

```
useEffect → checkUpdates()
  └─ CheckHubSkillUpdates()
      └─ 5 × getHubCenterJSON  ← 又是 5 次
```

**用户每次切 tab 都触发 5 次请求，无缓存。**

---

## 优化方案（按优先级排序）

### P0: 更新检查从搜索路径中剥离 + 客户端缓存

**问题**: `enrichInstalledState()` 在每次搜索时同步调用 `checkHubSkillUpdatesSafe()`，产生 K 次串行 HTTP 请求。搜索延迟 = 搜索本身 + K × 更新检查。

**修复**:

#### 1. `enrichInstalledState` 不再调用 `checkHubSkillUpdatesSafe`

搜索结果的"已安装"标记只需要本地数据（`loadSkills()` 比对 name/ID），不需要远程更新检查。更新标记（`HasUpdate`）改为读取缓存。

```go
// skill_searcher.go
func (s *SkillSearcher) enrichInstalledState(results []MixedSkillSearchResult) {
    skills := s.app.skillExecutor.loadSkills()
    // 从缓存读取更新信息，不发 HTTP 请求
    updatesByHubID := s.app.getCachedHubUpdates()
    for i := range results {
        for _, skill := range skills {
            if mixedResultMatchesSkill(results[i], skill) {
                results[i].Installed = true
                results[i].InstalledName = skill.Name
                if skill.Source == "hub" && skill.HubSkillID != "" {
                    results[i].CanUpdate = true
                    results[i].HasUpdate = updatesByHubID[skill.HubSkillID]
                }
                break
            }
        }
    }
}
```

#### 2. 更新检查结果缓存 + 后台刷新

```go
// app_wails_bindings.go 或新文件 hub_update_cache.go
type hubUpdateCache struct {
    mu        sync.RWMutex
    updates   []HubSkillUpdateInfo
    byHubID   map[string]bool
    fetchedAt time.Time
    ttl       time.Duration // 10 分钟
}

func (a *App) getCachedHubUpdates() map[string]bool {
    a.hubUpdateCache.mu.RLock()
    defer a.hubUpdateCache.mu.RUnlock()
    if a.hubUpdateCache.byHubID == nil {
        return nil
    }
    return a.hubUpdateCache.byHubID
}

// CheckHubSkillUpdates 改为：缓存未过期直接返回，过期则刷新
func (a *App) CheckHubSkillUpdates() ([]HubSkillUpdateInfo, error) {
    a.hubUpdateCache.mu.RLock()
    if time.Since(a.hubUpdateCache.fetchedAt) < a.hubUpdateCache.ttl {
        result := a.hubUpdateCache.updates
        a.hubUpdateCache.mu.RUnlock()
        return result, nil
    }
    a.hubUpdateCache.mu.RUnlock()
    
    // 实际请求
    updates, err := a.fetchHubSkillUpdatesUncached()
    if err != nil {
        return nil, err
    }
    
    // 更新缓存
    a.hubUpdateCache.mu.Lock()
    a.hubUpdateCache.updates = updates
    a.hubUpdateCache.byHubID = make(map[string]bool)
    for _, u := range updates {
        // 需要通过 skill name 找到 hub ID
        for _, s := range a.skillExecutor.loadSkills() {
            if s.Name == u.SkillName && s.HubSkillID != "" {
                a.hubUpdateCache.byHubID[s.HubSkillID] = true
            }
        }
    }
    a.hubUpdateCache.fetchedAt = time.Now()
    a.hubUpdateCache.mu.Unlock()
    
    return updates, nil
}
```

**效果**: 搜索请求从 12 次降到 7 次（去掉 5 次更新检查）。Tab 切换时命中缓存，0 次请求。

---

### P1: HubCenter URL 解析结果缓存（应用级）

**问题**: `getHubCenterJSON` 每次调用都走 `resolveHubCenterCandidates` → `resolveHubCenterBaseURL`。虽然 `SelectBestCenter` 有 15s 缓存，但 `resolveHubCenterBaseURL` 还要做 config 读取、URL 合并、`FetchHubCenterDiscovery` 等操作。

**修复**: 在 `App` 级别缓存已解析的 HubCenter base URL，TTL 60 秒。

```go
// hubcenter_client_helper.go
type resolvedHubCenter struct {
    mu        sync.RWMutex
    base      string
    all       []string
    resolvedAt time.Time
    ttl       time.Duration // 60 秒
}

func (a *App) resolveHubCenterBaseURLCached(ctx context.Context, client *http.Client) (string, []string, error) {
    a.resolvedHub.mu.RLock()
    if a.resolvedHub.base != "" && time.Since(a.resolvedHub.resolvedAt) < a.resolvedHub.ttl {
        base, all := a.resolvedHub.base, a.resolvedHub.all
        a.resolvedHub.mu.RUnlock()
        return base, all, nil
    }
    a.resolvedHub.mu.RUnlock()
    
    base, all, err := a.resolveHubCenterBaseURL(ctx, client)
    if err != nil {
        return "", nil, err
    }
    
    a.resolvedHub.mu.Lock()
    a.resolvedHub.base = base
    a.resolvedHub.all = all
    a.resolvedHub.resolvedAt = time.Now()
    a.resolvedHub.mu.Unlock()
    
    return base, all, nil
}
```

然后 `resolveHubCenterCandidates` 改为调用 `resolveHubCenterBaseURLCached`。

**效果**: 60 秒内的所有 HubCenter API 请求共享同一个已解析的 base URL，省掉重复的探测和发现请求。`CheckHubSkillUpdates` 的 5 次请求不再各自触发探测。

---

### P1: N+1 更新检查改为批量 API

**问题**: 5 个已安装 skill → 5 次串行 `GET /api/v1/skills/{id}`。

**修复**: 新增批量检查 API。

#### 服务端（如果可控）

```
POST /api/v1/skills/check-updates
Body: [{"id": "xxx", "version": "1.0"}, ...]
Response: [{"id": "xxx", "latest_version": "1.1"}, ...]
```

#### 客户端

```go
func (a *App) fetchHubSkillUpdatesUncached() ([]HubSkillUpdateInfo, error) {
    skills := a.skillExecutor.loadSkills()
    var checks []map[string]string
    for _, s := range skills {
        if s.Source == "hub" && s.HubSkillID != "" {
            checks = append(checks, map[string]string{
                "id":      s.HubSkillID,
                "version": s.HubVersion,
            })
        }
    }
    if len(checks) == 0 {
        return nil, nil
    }
    
    // 单次批量请求
    body, _ := json.Marshal(checks)
    // POST /api/v1/skills/check-updates
    var results []struct {
        ID            string `json:"id"`
        LatestVersion string `json:"latest_version"`
    }
    _, _, err := a.postHubCenterJSON(ctx, path, body, &results)
    // ... 转换为 HubSkillUpdateInfo
}
```

**如果服务端不可控**（短期方案）: 并发请求 + 限制并发数。

```go
func (a *App) fetchHubSkillUpdatesUncached() ([]HubSkillUpdateInfo, error) {
    skills := a.skillExecutor.loadSkills()
    var hubSkills []corelib.NLSkillEntry
    for _, s := range skills {
        if s.Source == "hub" && s.HubSkillID != "" {
            hubSkills = append(hubSkills, s)
        }
    }
    
    sem := make(chan struct{}, 3) // 最多 3 并发
    var mu sync.Mutex
    var updates []HubSkillUpdateInfo
    var wg sync.WaitGroup
    
    for _, s := range hubSkills {
        wg.Add(1)
        go func(skill corelib.NLSkillEntry) {
            defer wg.Done()
            sem <- struct{}{}
            defer func() { <-sem }()
            
            meta, err := a.skillHubClient.CheckUpdate(ctx, skill.HubSkillID, skill.HubVersion)
            if err != nil || meta == nil {
                return
            }
            mu.Lock()
            updates = append(updates, HubSkillUpdateInfo{...})
            mu.Unlock()
        }(s)
    }
    wg.Wait()
    return updates, nil
}
```

**效果**: 批量 API → 1 次请求。并发方案 → 延迟从 5×RTT 降到 2×RTT（3 并发）。

---

### P2: RefreshRecommendations 启动异步化

**问题**: `app.go` 初始化链路中 `RefreshRecommendations(context.Background())` 同步阻塞。HubCenter 不可达时阻塞 10 秒。

**修复**:

```go
// app.go
a.skillHubClient = NewSkillHubClient(a)
// 异步刷新，不阻塞启动
go func(client *SkillHubClient) {
    ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
    defer cancel()
    _ = client.RefreshRecommendations(ctx)
}(a.skillHubClient)
```

**效果**: 启动不再被 HubCenter 可达性阻塞。

---

### P2: rememberHubCenterSelection 写盘节流

**问题**: 每次成功的 `getHubCenterJSON` 都调用 `rememberHubCenterSelection` → `updatePreferredHubCenterURLs` → `LoadConfig` + `SaveConfig`。搜索一次可能触发 6+ 次写盘。

**修复**: 只在 base URL 实际变化时写盘。

```go
// hubcenter_client_helper.go
func (a *App) rememberHubCenterSelection(base string, discovered []string) {
    if a == nil {
        return
    }
    // 只在 base 变化时写盘
    a.resolvedHub.mu.RLock()
    lastBase := a.resolvedHub.base
    a.resolvedHub.mu.RUnlock()
    
    if base == lastBase {
        return // base 没变，不写盘
    }
    a.updatePreferredHubCenterURLs(base, discovered)
}
```

**效果**: 正常使用中写盘频率从"每次请求"降到"URL 切换时"（极少发生）。

---

### P2: TUI 搜索接入多节点 failover

**问题**: TUI 的 `skillSearch` 直接用 `app.appConfig.SkillHubBaseURL()` 单个 URL，不走发现和 failover。

**修复**: `HubClient` 增加可选的 URL 解析回调，或者 TUI 在调用前先解析 URL。

短期方案——TUI 在搜索前尝试 ping 主 URL，失败则用 fallback：

```go
func skillSearch(app *TUIApp, args map[string]interface{}) string {
    // ...
    hubURL := app.appConfig.SkillHubBaseURL(remote.DefaultRemoteHubCenterURL)
    
    // 简单 failover: 主 URL 不可达时用 fallback 列表
    urls := app.appConfig.HubCenterBaseURLs(
        remote.DefaultRemoteHubCenterURL, 
        remote.DefaultRemoteHubCenterURLs,
    )
    ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
    defer cancel()
    
    ordered := remote.SelectBestCenter(ctx, &http.Client{Timeout: 3*time.Second}, urls, hubURL)
    if len(ordered) > 0 {
        hubURL = ordered[0]
    }
    
    client := skill.NewHubClient()
    results := client.SearchAll(ctx, hubURL, query)
    // ...
}
```

**效果**: TUI 搜索获得与 GUI 相同的 failover 能力。

---

### P3: HubClient 连接复用

**问题**: 每次 `NewHubClient()` 创建新的 `http.Client`，不复用 TCP 连接。

**修复**: 包级别单例。

```go
// corelib/skill/hub_search.go
var defaultHubClient = &HubClient{
    httpClient:  &http.Client{
        Timeout: 15 * time.Second,
        Transport: &http.Transport{
            MaxIdleConns:        10,
            MaxIdleConnsPerHost: 5,
            IdleConnTimeout:     90 * time.Second,
        },
    },
    userAgent:   "MaClaw/1.0",
    githubToken: "", // 延迟初始化
}

var hubClientOnce sync.Once

func DefaultHubClient() *HubClient {
    hubClientOnce.Do(func() {
        defaultHubClient.githubToken = ResolveGitHubToken()
    })
    return defaultHubClient
}
```

消费方从 `skill.NewHubClient()` 改为 `skill.DefaultHubClient()`。

**效果**: TCP 连接复用，减少 TLS 握手开销。对 ClawHub 和 GitHub 的请求受益最大（跨搜索复用连接）。

---

### P3: 前端 Hub Tab 推荐列表

**问题**: `RefreshRecommendations` 的数据只服务于 tool routing，前端 Hub Tab 只有搜索框，没有"热门推荐"。用户打开 Hub Tab 看到空白，必须主动搜索。

**修复**: 前端 Hub Tab 初始状态显示推荐列表。

```tsx
// SkillsManagementPanel.tsx
const [hubRecommendations, setHubRecommendations] = useState<MixedSkillSearchResult[]>([]);

useEffect(() => {
    if (activeTab === "hub" && !hubSearched) {
        // 加载推荐列表
        GetHubRecommendations().then(recs => {
            setHubRecommendations(Array.isArray(recs) ? recs : []);
        }).catch(() => {});
    }
}, [activeTab, hubSearched]);
```

后端新增 Wails binding:

```go
func (a *App) GetHubRecommendations() ([]MixedSkillSearchResult, error) {
    a.ensureSkillHubClient()
    recs := a.skillHubClient.GetRecommendations()
    results := make([]MixedSkillSearchResult, len(recs))
    for i, r := range recs {
        results[i] = MixedSkillSearchResult{
            ID: r.ID, Name: r.Name, Description: r.Description,
            Source: "skillhub", SourceLabel: "SkillHub",
            // ...
        }
    }
    // enrichInstalledState
    return results, nil
}
```

**效果**: 用户打开 Hub Tab 立即看到热门 skill，不需要先搜索。推荐数据来自内存缓存，零延迟。

---

## 优化效果汇总

| 场景 | 优化前 HTTP 请求数 | 优化后 | 延迟改善 |
|------|-------------------|--------|---------|
| 搜索一次 (5 个已安装 hub skill) | 12 | 7 (P0) → 4 (P1 批量) | -60% |
| 切换 Hub Tab | 5 | 0 (缓存命中) | -100% |
| 连续搜索 3 次 (60s 内) | 36 | 12 (URL 缓存) → 9 (批量) | -75% |
| 应用启动 | 阻塞 0-10s | 异步，0s | -100% |
| 写盘次数 (单次搜索) | 6+ | 0-1 | -90% |

## 实施顺序

1. **P0**: `enrichInstalledState` 剥离更新检查 + 更新缓存（改 2 个文件，风险低）
2. **P1**: URL 解析缓存（改 1 个文件，风险低）
3. **P1**: 更新检查并发化（改 1 个文件，风险低；批量 API 需要服务端配合）
4. **P2**: 启动异步化（改 1 行，风险极低）
5. **P2**: 写盘节流（改 1 个函数，风险低）
6. **P2**: TUI failover（改 1 个函数，风险低）
7. **P3**: HubClient 单例（改 1 个文件 + 消费方，风险低）
8. **P3**: 前端推荐列表（新增 binding + 前端组件，风险低）
