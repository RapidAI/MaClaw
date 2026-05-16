# Technical Design: Capability Market Upgrade

## Overview

本设计覆盖三个核心变更：
1. **命名统一**：从"技能市场/Skill Market/Marketplace"统一为"能力市场/Capability Market"
2. **HubCenter 管理员搜索导入**：HubCenter 管理员从 ClawHub/GitHub 搜索 Skill 和 MCP Server 并导入为免费能力包
3. **MCP 验证能力**：连通性、工具可用性、Schema 正确性、运行时健康检查

## Architecture

### 系统层次关系

```
┌─────────────────────────────────────────────────────────────┐
│  Maclaw Client (GUI / TUI)                                  │
│  - SkillsManagementPanel → CapabilityMarketPanel            │
│  - tui/commands/skillmarket → capabilitymarket              │
│  - corelib/remote/skillmarket_auth → capability_market_auth │
└──────────────────────────┬──────────────────────────────────┘
                           │
              ┌────────────┴────────────┐
              ▼                         ▼
┌─────────────────────────┐  ┌─────────────────────────────────┐
│  Enterprise Hub         │  │  HubCenter                      │
│  /api/capabilities      │  │  /api/v1/skills (SkillHub)      │
│  /api/admin/capabilities│  │  /api/v1/skillmarket            │
│  /marketplace           │  │  /api/capability-market/mcp     │
│                         │  │  /api/admin/capability-market    │
│  Admin: search from     │  │                                 │
│  HubCenter+ClawHub+GH   │  │  Admin: search from             │
│                         │  │  ClawHub+GitHub only             │
└─────────────────────────┘  └─────────────────────────────────┘
              │                         │
              └────────────┬────────────┘
                           ▼
              ┌─────────────────────────┐
              │  External Sources       │
              │  - ClawHub (mirror)     │
              │  - GitHub (repo search) │
              └─────────────────────────┘
```

### 数据模型

#### Capability（已有，Hub 侧）

```sql
-- hub/internal/capability/service.go 已有
capabilities (
  id, capability_type, publisher, capability_id, display_name,
  description, source, managed_by, status, relation_to_origin,
  global_key, current_version_key, origin_key, metadata_json
)
```

#### MCP Catalog Entry（已有，HubCenter 侧）

```go
// hubcenter/internal/httpapi/capability_market_handlers.go 已有
type CapabilityMarketMCPEntry struct {
    ID             string `json:"id"`
    CapabilityID   string `json:"capability_id"`
    CapabilityType string `json:"capability_type"` // "mcp"
    DisplayName    string `json:"display_name"`
    Description    string `json:"description"`
    Version        string `json:"version"`
    Source         string `json:"source"`
    Pricing        string `json:"pricing"`
    MCP            MCPConfig `json:"mcp"`
    // ...
}
```

#### MCP Validation Result（新增）

```go
// corelib/mcp/validation_report.go (新文件)
type ValidationReport struct {
    Connectivity    *ConnectivityResult    `json:"connectivity"`
    ToolAvailability *ToolAvailabilityResult `json:"tool_availability,omitempty"`
    SchemaCorrectness *SchemaCorrectnessResult `json:"schema_correctness,omitempty"`
    RuntimeHealth   *RuntimeHealthResult   `json:"runtime_health,omitempty"`
    OverallStatus   string                 `json:"overall_status"` // "pass" | "warn" | "fail"
    Duration        time.Duration          `json:"duration_ms"`
    Timestamp       time.Time              `json:"timestamp"`
}

type ConnectivityResult struct {
    Connected bool   `json:"connected"`
    LatencyMs int64  `json:"latency_ms,omitempty"`
    Error     string `json:"error,omitempty"`
}

type ToolAvailabilityResult struct {
    Available bool     `json:"available"`
    Tools     []string `json:"tools,omitempty"`
    Warning   string   `json:"warning,omitempty"`
    Error     string   `json:"error,omitempty"`
}

type SchemaCorrectnessResult struct {
    Valid  bool           `json:"valid"`
    Errors []SchemaError  `json:"errors,omitempty"`
}

type SchemaError struct {
    ToolName string `json:"tool_name"`
    Message  string `json:"message"`
}

type RuntimeHealthResult struct {
    Healthy    *bool  `json:"healthy"` // nil = skipped
    ResponseMs int64  `json:"response_ms,omitempty"`
    ToolUsed   string `json:"tool_used,omitempty"`
    Error      string `json:"error,omitempty"`
    Note       string `json:"note,omitempty"`
}
```

## Components and Interfaces

### MCP Validator Interface

```go
// corelib/mcp/validator.go
type MCPValidator interface {
    Validate(ctx context.Context, config MCPServerConfig) (*ValidationReport, error)
    CheckConnectivity(ctx context.Context, config MCPServerConfig) *ConnectivityResult
    CheckToolAvailability(ctx context.Context, config MCPServerConfig) *ToolAvailabilityResult
    CheckSchemaCorrectness(ctx context.Context, tools []ToolEntry) *SchemaCorrectnessResult
    CheckRuntimeHealth(ctx context.Context, config MCPServerConfig, tools []ToolEntry) *RuntimeHealthResult
}
```

### HubCenter Admin Import Interface

```go
// hubcenter/internal/httpapi/capability_market_handlers.go
type CapabilityImporter interface {
    ImportSkill(ctx context.Context, source, installRef string) (*ImportResult, error)
    ImportMCP(ctx context.Context, source, installRef string, runValidation bool) (*ImportResult, error)
}

type ImportResult struct {
    CapabilityID   string            `json:"capability_id"`
    CapabilityType string            `json:"capability_type"`
    DisplayName    string            `json:"display_name"`
    Source         string            `json:"source"`
    Pricing        string            `json:"pricing"`
    ValidationReport *mcp.ValidationReport `json:"validation_report,omitempty"`
}
```

### MCP Search Interface

```go
// corelib/skill/hub_search_mcp.go
type MCPSearcher interface {
    SearchMCPClawHub(ctx context.Context, query string) ([]HubSearchResult, error)
    SearchMCPGitHub(ctx context.Context, query string) ([]HubSearchResult, error)
}
```

## Data Models

### ValidationReport

```go
type ValidationReport struct {
    Connectivity      *ConnectivityResult      `json:"connectivity"`
    ToolAvailability  *ToolAvailabilityResult  `json:"tool_availability,omitempty"`
    SchemaCorrectness *SchemaCorrectnessResult `json:"schema_correctness,omitempty"`
    RuntimeHealth     *RuntimeHealthResult     `json:"runtime_health,omitempty"`
    OverallStatus     string                   `json:"overall_status"` // "pass" | "warn" | "fail"
    DurationMs        int64                    `json:"duration_ms"`
    Timestamp         time.Time                `json:"timestamp"`
}

type ConnectivityResult struct {
    Connected bool   `json:"connected"`
    LatencyMs int64  `json:"latency_ms,omitempty"`
    Error     string `json:"error,omitempty"`
}

type ToolAvailabilityResult struct {
    Available bool     `json:"available"`
    Tools     []string `json:"tools,omitempty"`
    Warning   string   `json:"warning,omitempty"`
    Error     string   `json:"error,omitempty"`
}

type SchemaCorrectnessResult struct {
    Valid  bool          `json:"valid"`
    Errors []SchemaError `json:"errors,omitempty"`
}

type SchemaError struct {
    ToolName string `json:"tool_name"`
    Message  string `json:"message"`
}

type RuntimeHealthResult struct {
    Healthy    *bool  `json:"healthy"` // nil = skipped
    ResponseMs int64  `json:"response_ms,omitempty"`
    ToolUsed   string `json:"tool_used,omitempty"`
    Error      string `json:"error,omitempty"`
    Note       string `json:"note,omitempty"`
}
```

### MCPServerConfig

```go
type MCPServerConfig struct {
    EndpointURL string            `json:"endpoint_url"`
    Transport   string            `json:"transport"` // "sse" | "streamable-http"
    Headers     map[string]string `json:"headers,omitempty"`
    APIKey      string            `json:"api_key,omitempty"`
}
```

## Components

### Component 1: 命名统一（Rename Layer）

**策略**：渐进式迁移，保留旧名称作为别名。

#### 1.1 前端文案替换

| 文件 | 变更 |
|------|------|
| `gui/frontend/src/components/remote/SkillsManagementPanel.tsx` | 4 处 `localizeText` 调用：`"Skill Market"→"Capability Market"`, `"技能市场"→"能力市场"`, `"技能市場"→"能力市場"` |
| `iWorkerCenter/frontend/src/pages/CloudRegistrationPage.tsx` | `skill_market` → `capability_market` |
| `iWorkerCloud/web/admin/src/pages/SkillMarketPage.tsx` | 重命名为 `CapabilityMarketPage.tsx`，更新内部文案 |

#### 1.2 TUI 命令别名

```go
// tui/commands/skillmarket.go
// 保留 RunSkillMarket 作为入口，新增 RunCapabilityMarket 别名
func RunCapabilityMarket(args []string) error { return RunSkillMarket(args) }

// tui/app.go 或 main.go 中注册两个命令名
case "capabilitymarket", "skillmarket":
    return commands.RunCapabilityMarket(args[1:])
```

#### 1.3 共享库类型别名

```go
// corelib/remote/skillmarket_auth.go → 新增 capability_market_auth.go
type CapabilityMarketAuthClient = SkillMarketAuthClient  // 类型别名
func NewCapabilityMarketAuthClient() *CapabilityMarketAuthClient {
    return NewSkillMarketAuthClient()
}
```

#### 1.4 HubCenter API 路由别名

```go
// hubcenter/internal/httpapi/router.go
// 保留旧路由，新增别名
registerStaticRoutes(mux, "./web/skillmarket", "/capabilitymarket")
registerStaticRoutes(mux, "./web/skillmarket", "/marketplace") // 已有
```

### Component 2: HubCenter Admin 搜索导入 MCP

**当前状态**：`AdminCapabilityMarketExternalSearchHandler()` 在 HubCenter 侧已存在，但只支持 `capability_type=skill`。需要扩展支持 `type=mcp`。

#### 2.1 扩展 HubCenter External Search Handler

```go
// hubcenter/internal/httpapi/capability_market_handlers.go
// AdminCapabilityMarketExternalSearchHandler 修改
func AdminCapabilityMarketExternalSearchHandler() http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // ... 已有的 source 验证逻辑 ...
        capabilityType := corelib.NormalizeCapabilityType(r.URL.Query().Get("type"))
        if capabilityType == "" {
            capabilityType = corelib.CapabilityTypeSkill
        }

        switch capabilityType {
        case corelib.CapabilityTypeSkill:
            // 已有逻辑：SearchAllFiltered
        case corelib.CapabilityTypeMCP:
            // 新增：搜索 ClawHub/GitHub 上的 MCP 配置
            items := searchExternalMCPConfigs(r.Context(), source, query, allowedSources)
            writeJSON(w, http.StatusOK, map[string]any{
                "allowed_sources": allowedSources,
                "items": items,
            })
        }
    }
}
```

#### 2.2 MCP 搜索源实现

```go
// corelib/skill/hub_search_mcp.go (新文件)
func (c *HubClient) SearchMCPClawHub(ctx context.Context, query string) ([]HubSearchResult, error)
func (c *HubClient) SearchMCPGitHub(ctx context.Context, query string) ([]HubSearchResult, error)
```

**ClawHub MCP 搜索**：查询 ClawHub API 的 `type=mcp` 过滤参数（需要 ClawHub 支持 MCP 类型标记）。

**GitHub MCP 搜索**：Repository Search API `topic:mcp-server` + query，或 Code Search `filename:mcp.json` + query。

#### 2.3 HubCenter Admin Import Handler

```go
// hubcenter/internal/httpapi/capability_market_handlers.go
// 新增 AdminCapabilityMarketImportHandler
func AdminCapabilityMarketImportHandler(settings store.SystemSettingsRepository) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        var req struct {
            CapabilityType string `json:"capability_type"` // "skill" | "mcp"
            Source         string `json:"source"`          // "clawhub" | "github"
            InstallRef     string `json:"install_ref"`     // 安装引用（JSON 序列化的候选信息）
            RunValidation  bool   `json:"run_validation"`  // 是否运行 MCP 验证
        }
        // 1. 下载能力包
        // 2. 如果是 MCP 且 RunValidation=true，执行验证
        // 3. 注册到 HubCenter（skill → SkillStore, mcp → MCP Catalog）
        // 4. 标记 pricing=free, source=clawhub/github
    }
}
```

**路由注册**：
```go
mux.HandleFunc("POST /api/admin/capability-market/import",
    RequireAdmin(adminService, AdminCapabilityMarketImportHandler(systemSettings)))
```

### Component 3: MCP Validator

#### 3.1 核心验证器

```go
// corelib/mcp/validator.go (新文件)
type Validator struct {
    Timeout time.Duration // 总超时，默认 30s
}

func NewValidator() *Validator {
    return &Validator{Timeout: 30 * time.Second}
}

func (v *Validator) Validate(ctx context.Context, endpoint string, config MCPServerConfig) (*ValidationReport, error)
func (v *Validator) CheckConnectivity(ctx context.Context, endpoint string) *ConnectivityResult
func (v *Validator) CheckToolAvailability(ctx context.Context, endpoint string) *ToolAvailabilityResult
func (v *Validator) CheckSchemaCorrectness(ctx context.Context, tools []ToolEntry) *SchemaCorrectnessResult
func (v *Validator) CheckRuntimeHealth(ctx context.Context, endpoint string, tools []ToolEntry) *RuntimeHealthResult
```

#### 3.2 连通性验证

```go
func (v *Validator) CheckConnectivity(ctx context.Context, endpoint string) *ConnectivityResult {
    ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
    defer cancel()

    start := time.Now()
    // 发送 MCP initialize 请求
    resp, err := sendMCPRequest(ctx, endpoint, "initialize", map[string]any{
        "protocolVersion": "2024-11-05",
        "capabilities":    map[string]any{},
        "clientInfo":      map[string]any{"name": "maclaw-validator", "version": "1.0"},
    })
    latency := time.Since(start).Milliseconds()

    if err != nil {
        return &ConnectivityResult{Connected: false, Error: err.Error()}
    }
    return &ConnectivityResult{Connected: true, LatencyMs: latency}
}
```

#### 3.3 工具可用性验证

```go
func (v *Validator) CheckToolAvailability(ctx context.Context, endpoint string) *ToolAvailabilityResult {
    ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
    defer cancel()

    resp, err := sendMCPRequest(ctx, endpoint, "tools/list", nil)
    if err != nil {
        return &ToolAvailabilityResult{Available: false, Error: err.Error()}
    }
    // 解析 tools 列表
    tools := parseMCPToolsList(resp)
    if len(tools) == 0 {
        return &ToolAvailabilityResult{Available: true, Warning: "no tools exposed"}
    }
    names := make([]string, len(tools))
    for i, t := range tools { names[i] = t.Name }
    return &ToolAvailabilityResult{Available: true, Tools: names}
}
```

#### 3.4 Schema 正确性验证

```go
func (v *Validator) CheckSchemaCorrectness(ctx context.Context, tools []ToolEntry) *SchemaCorrectnessResult {
    var errors []SchemaError
    for _, tool := range tools {
        schema := tool.InputSchema
        if schema == nil { continue }

        // 1. 检查 required 字段引用的属性是否存在
        errs := ValidateSchemaStructure(schema)
        for _, e := range errs {
            errors = append(errors, SchemaError{ToolName: tool.Name, Message: e})
        }

        // 2. 构造样本参数并用 ValidateArgs 验证 round-trip
        sampleArgs := constructSampleArgs(schema)
        if valErrs := ValidateArgs(schema, sampleArgs); len(valErrs) > 0 {
            for _, ve := range valErrs {
                errors = append(errors, SchemaError{
                    ToolName: tool.Name,
                    Message:  fmt.Sprintf("round-trip validation failed: %s", ve.Message),
                })
            }
        }
    }
    return &SchemaCorrectnessResult{Valid: len(errors) == 0, Errors: errors}
}
```

#### 3.5 运行时健康检查

```go
func (v *Validator) CheckRuntimeHealth(ctx context.Context, endpoint string, tools []ToolEntry) *RuntimeHealthResult {
    // 选择安全工具：优先无 required 参数 → 仅 string 参数 → 第一个
    tool := selectSafeHealthCheckTool(tools)
    if tool == nil {
        return &RuntimeHealthResult{Healthy: nil, Note: "no safe tool found for testing"}
    }

    ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
    defer cancel()

    args := constructMinimalArgs(tool.InputSchema)
    start := time.Now()
    _, err := sendMCPRequest(ctx, endpoint, "tools/call", map[string]any{
        "name":      tool.Name,
        "arguments": args,
    })
    responseMs := time.Since(start).Milliseconds()

    if err != nil {
        healthy := false
        return &RuntimeHealthResult{Healthy: &healthy, Error: err.Error(), ToolUsed: tool.Name}
    }
    healthy := true
    return &RuntimeHealthResult{Healthy: &healthy, ResponseMs: responseMs, ToolUsed: tool.Name}
}
```

#### 3.6 MCP 通信层

```go
// corelib/mcp/client.go (新文件)
type MCPServerConfig struct {
    EndpointURL string            `json:"endpoint_url"`
    Transport   string            `json:"transport"` // "stdio" | "sse" | "streamable-http"
    Command     string            `json:"command,omitempty"`
    Args        []string          `json:"args,omitempty"`
    Env         map[string]string `json:"env,omitempty"`
    Headers     map[string]string `json:"headers,omitempty"`
}

// sendMCPRequest 根据 transport 类型选择通信方式
func sendMCPRequest(ctx context.Context, config MCPServerConfig, method string, params any) (json.RawMessage, error) {
    switch config.Transport {
    case "stdio":
        return sendMCPStdio(ctx, config, method, params)
    case "sse", "streamable-http", "":
        return sendMCPHTTP(ctx, config, method, params)
    default:
        return nil, fmt.Errorf("unsupported MCP transport: %s", config.Transport)
    }
}
```

### Component 4: Hub Admin External Search 扩展（MCP 支持）

Hub 侧的 `AdminCapabilityExternalSearchHandler` 已支持 `type=mcp` 搜索 HubCenter。需要扩展支持 ClawHub 和 GitHub 源的 MCP 搜索。

```go
// hub/internal/httpapi/marketplace_handlers.go
// 已有的 AdminCapabilityExternalSearchHandler 中 type=mcp 分支扩展
case corelib.CapabilityTypeMCP:
    if source == corelib.CapabilitySourceHubCenter {
        items, err = searchHubCenterMCPMarketplace(r.Context(), centerStatus, query)
    } else {
        // 新增：ClawHub/GitHub MCP 搜索
        items = searchExternalMCPMarketplace(r.Context(), source, query, allowedSources)
    }
```

### Component 5: Hub Admin MCP Validation API

```go
// hub/internal/httpapi/marketplace_handlers.go
// 新增验证端点
func AdminMCPValidateHandler() http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        var req struct {
            EndpointURL string            `json:"endpoint_url"`
            Transport   string            `json:"transport"`
            Command     string            `json:"command,omitempty"`
            Args        []string          `json:"args,omitempty"`
            Env         map[string]string `json:"env,omitempty"`
            Headers     map[string]string `json:"headers,omitempty"`
            Checks      []string          `json:"checks"` // ["connectivity","tools","schema","health"] or ["all"]
        }
        // ...
        validator := mcp.NewValidator()
        report, err := validator.Validate(r.Context(), req.EndpointURL, mcpConfig)
        writeJSON(w, http.StatusOK, report)
    }
}
```

**路由注册**（Hub）：
```go
mux.HandleFunc("POST /api/admin/capabilities/mcp/validate",
    RequireAdmin(admins, AdminMCPValidateHandler()))
```

**路由注册**（HubCenter）：
```go
mux.HandleFunc("POST /api/admin/capability-market/mcp/validate",
    RequireAdmin(adminService, AdminMCPValidateHandler(systemSettings)))
```

## Key Design Decisions

### 1. 命名迁移策略：别名而非重命名

**决策**：保留旧名称作为别名（类型别名、路由别名、命令别名），不做破坏性重命名。

**原因**：
- 避免所有调用方同时修改
- 已部署的客户端仍使用旧 API 路径
- 渐进式迁移，降低风险

### 2. MCP 验证的 Transport 支持

**决策**：首期只支持 HTTP-based transport（SSE / Streamable HTTP），不支持 stdio。

**原因**：
- Hub/HubCenter 是服务端，无法在服务端启动 stdio 子进程
- stdio transport 的 MCP Server 需要在客户端本地运行
- HTTP-based MCP Server 可以远程验证

### 3. HubCenter MCP 搜索源

**决策**：HubCenter 管理员只能从 ClawHub 和 GitHub 搜索（不从 HubCenter 自身搜索）。

**原因**：
- HubCenter 就是自己，搜索自己没有意义
- 与 `AdminMarketplaceSearchSources(CapabilityMarketplaceHostHubCenter)` 已有逻辑一致

### 4. 验证失败不阻止导入

**决策**：MCP 验证失败时仍允许导入，但标记 `validation_status: failed`。

**原因**：
- MCP Server 可能暂时不可达但配置正确
- 管理员可能知道 Server 将在稍后上线
- 提供信息而非阻止操作

## API Endpoints Summary

### 新增端点

| Method | Path | Host | 说明 |
|--------|------|------|------|
| POST | `/api/admin/capability-market/import` | HubCenter | 管理员导入能力包 |
| POST | `/api/admin/capability-market/mcp/validate` | HubCenter | MCP 验证 |
| POST | `/api/admin/capabilities/mcp/validate` | Hub | MCP 验证 |

### 修改端点

| Method | Path | Host | 变更 |
|--------|------|------|------|
| GET | `/api/admin/capability-market/external-search` | HubCenter | 新增 `type=mcp` 支持 |
| GET | `/api/admin/capabilities/external-search` | Hub | 新增 ClawHub/GitHub MCP 搜索 |

### 新增路由别名

| 旧路径 | 新路径 | Host |
|--------|--------|------|
| `/skillmarket` | `/capabilitymarket` | HubCenter |
| `maclaw-tui skillmarket` | `maclaw-tui capabilitymarket` | Client |

## File Changes Summary

### 新增文件

| 文件 | 说明 |
|------|------|
| `corelib/mcp/validator.go` | MCP 验证器核心实现 |
| `corelib/mcp/validation_report.go` | 验证报告数据类型 |
| `corelib/mcp/client.go` | MCP JSON-RPC 客户端（HTTP transport） |
| `corelib/skill/hub_search_mcp.go` | ClawHub/GitHub MCP 搜索 |
| `corelib/remote/capability_market_auth.go` | 新名称类型别名 |

### 修改文件

| 文件 | 变更 |
|------|------|
| `gui/frontend/src/components/remote/SkillsManagementPanel.tsx` | 文案替换 |
| `iWorkerCloud/web/admin/src/pages/SkillMarketPage.tsx` | 重命名 + 文案 |
| `iWorkerCenter/frontend/src/pages/CloudRegistrationPage.tsx` | module key |
| `tui/commands/skillmarket.go` | 帮助文本 + 命令别名 |
| `hubcenter/internal/httpapi/capability_market_handlers.go` | MCP 搜索 + 导入 + 验证 |
| `hubcenter/internal/httpapi/router.go` | 新路由注册 |
| `hub/internal/httpapi/marketplace_handlers.go` | MCP 搜索扩展 + 验证 |
| `hub/internal/httpapi/router.go` | 新路由注册 |
| `corelib/capability_market.go` | 可能的辅助函数 |

## Testing Strategy

## Correctness Properties

### Property 1: Naming Backward Compatibility

All legacy API paths (`/skillmarket/`, `/marketplace`) SHALL remain accessible in the new version and return identical results to the new paths (`/capabilitymarket/`).

**Validates: Requirements 2.1, 4.2, 5.3**

### Property 2: Type Alias Equivalence

`CapabilityMarketAuthClient` and `SkillMarketAuthClient` SHALL be the same type (Go type alias), usable interchangeably in all call sites.

**Validates: Requirements 3.5**

### Property 3: Validation Idempotency

For the same MCP Server with unchanged state, two consecutive calls to `Validate()` SHALL return the same `overall_status` value.

**Validates: Requirements 13.1**

### Property 4: Schema Round-Trip

For any valid tool schema, `constructSampleArgs(schema)` SHALL produce arguments that pass `ValidateArgs(schema, args)` with zero validation errors.

**Validates: Requirements 14.3**

### Property 5: Search Source Isolation

HubCenter admin search SHALL never return results with `source=hubcenter`. Hub admin search MAY return results with `source=hubcenter`.

**Validates: Requirements 8.5, 7.2**

### Property 6: Import Pricing Invariant

All capabilities imported via HubCenter admin import SHALL have `pricing` field set to `"free"` regardless of the source capability's original pricing.

**Validates: Requirements 8.6**

## Error Handling

### MCP 验证错误处理

| 错误场景 | 处理方式 |
|---------|---------|
| 连通性超时（10s） | `connectivity.connected=false`, 跳过后续检查, `overall_status=fail` |
| URL 格式错误 | `connectivity.connected=false`, `error="invalid URL"`, `overall_status=fail` |
| `tools/list` 失败 | `tool_availability.available=false`, 继续 schema 检查（使用空列表）, `overall_status=fail` |
| Schema 无效 | `schema_correctness.valid=false`, 列出具体错误, `overall_status=warn` |
| 健康检查无安全工具 | `runtime_health.healthy=null`, `note="no safe tool"`, 不影响 `overall_status` |
| 健康检查超时（15s） | `runtime_health.healthy=false`, `error="timeout"`, `overall_status=warn` |
| 总超时（30s） | 中止剩余检查, 返回已收集结果, `overall_status=fail` |

### 搜索错误处理

| 错误场景 | 处理方式 |
|---------|---------|
| ClawHub 不可达 | 跳过 ClawHub 结果, 返回其他源结果, 不报错 |
| GitHub API 限流 | 跳过 GitHub 结果, 返回其他源结果, 不报错 |
| 所有源都失败 | 返回空结果列表 + `errors` 字段列出失败原因 |

### 导入错误处理

| 错误场景 | 处理方式 |
|---------|---------|
| 下载失败 | 返回 HTTP 502 + 错误详情 |
| MCP 验证失败 | 仍然导入, 标记 `validation_status=failed`, 返回 200 + 验证报告 |
| 重复导入 | 更新已有条目（upsert 语义）, 返回 200 |

## Testing Strategy

### 单元测试

- `corelib/mcp/validator_test.go`：Mock HTTP server 测试四种验证
- `corelib/mcp/validation_report_test.go`：报告生成和 overall_status 计算
- `corelib/skill/hub_search_mcp_test.go`：MCP 搜索结果解析

### 集成测试

- HubCenter admin external search with `type=mcp`
- HubCenter admin import flow（skill + mcp）
- Hub admin MCP validate endpoint

### 向后兼容测试

- 旧 API 路径仍可访问
- 旧 TUI 命令名仍可用
- 旧类型名仍可编译
