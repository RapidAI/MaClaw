# 技术设计：MCP 能力市场独立 Tab + 安装分流 + 强制下发保护

## 机制性问题分析

### 问题 1: MCPMarketplacePanel 嵌入位置错误

**当前**：`MCPMarketplacePanel` 作为 `RemoteMCPPanel` 的子组件渲染（第 1061 行），与远程 MCP 服务器列表耦合。

**根因**：最初设计时能力市场只安装 HTTP 类型的 MCP，所以放在远程 Tab 内。但现在市场能力包含 Stdio 类型（本地进程），嵌入位置不再正确。

**修复**：`MCPMarketplacePanel` 提升为 `MCPManagementPanel` 的直接子组件，作为第三个 Tab 渲染。

### 问题 2: 前端 MCPServerView 丢失 source 字段

**当前**：后端 `MCPServerView` 有 `Source corelib.MCPServerSource` 字段，但前端 TypeScript interface 没有声明。JSON 序列化时 `source` 字段被发送到前端，但 TypeScript 不消费它。

**根因**：前端 interface 是手动维护的（非自动生成），添加 `Source` 字段时遗漏了前端同步。

**修复**：前端 `MCPServerView` interface 新增 `source` 和 `managed` 字段。

### 问题 3: 删除操作不检查来源

**当前**：`UnregisterMCPServer` 直接调用 `registry.Unregister(serverID)`，不检查任何条件。

**根因**：强制下发保护逻辑只在安装侧（`ReinstallIfRemoved`——删除后下次 sync 会重新安装），没有在删除侧做前置拦截。用户删除后下次 sync 又装回来，体验差。

**修复**：`UnregisterMCPServer` 在删除前检查 `managed` 标记，拒绝删除强制下发的 MCP。

## 架构变更

### Tab 结构变更

```
修复前:
┌─────────────────────────────────────────┐
│  [本地 (Stdio)]  [远程 (HTTP)]           │
├─────────────────────────────────────────┤
│  远程 Tab 内容:                          │
│  ┌─ 能力市场 (MCPMarketplacePanel) ─┐   │
│  │  搜索框 + 推荐 + 同步下发         │   │
│  └──────────────────────────────────┘   │
│  ┌─ 已注册服务器列表 ───────────────┐   │
│  │  server1 / server2 / ...         │   │
│  └──────────────────────────────────┘   │
└─────────────────────────────────────────┘

修复后:
┌─────────────────────────────────────────────────┐
│  [本地 (Stdio)]  [远程 (HTTP)]  [能力市场]       │
├─────────────────────────────────────────────────┤
│  能力市场 Tab 内容:                              │
│  ┌─ MCPMarketplacePanel ────────────────────┐   │
│  │  搜索框 + 推荐 + 同步下发 + 搜索结果      │   │
│  │  已安装标记 / 安装按钮                    │   │
│  └──────────────────────────────────────────┘   │
└─────────────────────────────────────────────────┘
```

### 数据模型变更

#### 前端 MCPServerView 扩展

```typescript
interface MCPServerView {
    id: string;
    name: string;
    endpoint_url: string;
    auth_type: "none" | "api_key" | "bearer";
    auth_secret: string;
    headers?: Record<string, string>;
    capability?: MCPServerCapabilityRef;
    tools: MCPToolView[];
    health_status: "healthy" | "slow" | "unavailable" | "unknown" | "checking";
    fail_count: number;
    last_check_at: string;
    created_at: string;
    // --- 新增 ---
    source: "manual" | "mdns" | "project" | "marketplace";  // 来源
    managed: boolean;  // 是否为强制下发（不可删除）
}
```

#### 前端 LocalMCPServer 扩展

```typescript
interface LocalMCPServer {
    id: string;
    name: string;
    command: string;
    args: string[];
    env: Record<string, string>;
    disabled: boolean;
    auto_start?: boolean;
    created_at: string;
    // --- 新增 ---
    source?: "manual" | "marketplace";  // 来源
    managed?: boolean;  // 是否为强制下发
}
```

#### 后端 MCPServerView 扩展

```go
type MCPServerView struct {
    // ... 现有字段 ...
    Source  corelib.MCPServerSource `json:"source"`   // 已有，确保序列化
    Managed bool                   `json:"managed"`  // 新增：是否为强制下发
}
```

### managed 判定机制

**单一数据源**：`managed` 标记在 `ListMCPServers` 构建 `MCPServerView` 时一次性计算，不需要额外 API 调用。

判定逻辑：
```go
func (r *MCPRegistry) isManagedCapability(entry *corelib.MCPServerEntry) bool {
    if entry.Source != corelib.MCPSourceMarket || entry.Capability == nil {
        return false
    }
    // 检查是否有对应的 managed deployment（ReinstallIfRemoved=true）
    return r.app.isCapabilityManagedDeployment(entry.Capability.CapabilityID)
}
```

`isCapabilityManagedDeployment` 从上次 sync 缓存的 `managedDeployments` 列表中查找，不发起网络请求。

### 安装分流机制

能力市场安装时，后端根据 `capability_type` 决定安装目标：

```go
func (a *App) installCapabilityByType(item HubCapabilitySummary, ...) {
    switch item.CapabilityType {
    case "mcp_stdio":
        // 安装为本地 MCP（写入 local_mcp_servers.json）
        a.installCapabilityAsLocalMCP(item, ...)
    case "mcp_http", "mcp":
        // 安装为远程 MCP（写入 mcp_servers.json）
        a.installCapabilityAsRemoteMCP(item, ...)
    case "skill":
        // 安装为 Skill（现有逻辑）
        a.installCapabilityAsSkill(item, ...)
    }
}
```

### 删除保护机制

```go
func (a *App) UnregisterMCPServer(serverID string) error {
    if a.mcpRegistry == nil {
        return fmt.Errorf("MCP registry not initialized")
    }
    // 新增：检查强制下发保护
    if entry := a.mcpRegistry.FindByID(serverID); entry != nil {
        if a.mcpRegistry.isManagedCapability(entry) {
            return fmt.Errorf("此 MCP 为企业强制下发，不可删除")
        }
    }
    return a.mcpRegistry.Unregister(serverID)
}
```

## 修改文件清单

### 前端

| 文件 | 变更 |
|------|------|
| `MCPManagementPanel.tsx` | `MCPTab` 类型新增 `"marketplace"`；Tab 栏新增第三个按钮；`activeTab === "marketplace"` 渲染 `MCPMarketplacePanel`；从 `RemoteMCPPanel` 中移除 `MCPMarketplacePanel` |
| `MCPRemoteServerRow.tsx` | `MCPServerView` interface 新增 `source`/`managed` 字段；`managed=true` 时隐藏删除按钮，显示 标签 |
| `MCPMarketplacePanel.tsx` | `onChanged` 回调新增安装类型信息，通知父组件刷新对应列表 |
| `appTranslations.ts` | 新增 `mcpTabMarketplace`、`mcpManagedLabel`、`mcpCannotDeleteManaged` 翻译 key |

### 后端

| 文件 | 变更 |
|------|------|
| `gui/app_nl_mcp.go` | `MCPServerView` 新增 `Managed bool` 字段；`ListMCPServers` 构建 view 时计算 `managed`；`UnregisterMCPServer` 新增强制下发保护检查 |
| `gui/capability_market_sync.go` | 缓存 `managedDeployments` 列表供 `isManagedCapability` 查询；新增 `isCapabilityManagedDeployment(capabilityID)` 方法 |
| `gui/local_mcp_manager.go` | 本地 MCP 列表返回时包含 `source`/`managed` 信息（如果是市场安装的 Stdio 类型） |

## 向后兼容

- `MCPTab` 类型从 `"local" | "remote"` 扩展为 `"local" | "remote" | "marketplace"`，默认值改为 `"marketplace"`（与截图中当前默认选中远程 Tab 的行为对齐——用户最常用的是搜索安装）
- 旧版本安装的 MCP（`source` 为空字符串）在前端视为 `"manual"`，`managed` 为 `false`
- `MCPMarketplacePanel` 的 props 接口不变（`translate`/`onChanged`/`installedCapabilities`），只改变挂载位置
- `onChanged` 回调现在需要同时刷新本地和远程列表（之前只刷新远程）

## 关键设计决策

### 决策 1: managed 判定时机

**方案 A**（选定）：在 `ListMCPServers` 构建 view 时一次性计算，从内存缓存的 `managedDeployments` 查找。
- 优点：零额外网络请求，前端拿到的数据已包含 managed 标记
- 缺点：依赖 sync 缓存的时效性（最多 1 次 sync 周期的延迟）

**方案 B**（废弃）：前端单独调用 API 查询每个 server 是否 managed。
- 缺点：N 个 server 需要 N 次查询，延迟高

### 决策 2: 能力市场 Tab 的 installedCapabilities 数据源

能力市场 Tab 需要知道哪些能力已安装（显示"已安装"标记）。之前从 `RemoteMCPPanel` 的 `servers` state 获取。

**修复后**：`MCPManagementPanel` 维护一个 `installedCapabilityIDs` state，在 mount 时从 `ListMCPServers` + `ListLocalMCPServers` 合并计算。`MCPMarketplacePanel.onChanged` 触发时重新计算。

### 决策 3: 本地 MCP 的 source/managed 字段

本地 MCP 使用不同的存储格式（`local_mcp_servers.json`），当前 `LocalMCPServer` struct 没有 `Source` 字段。

**修复**：`LocalMCPServer` 新增 `Source` 和 `Managed` 字段。市场安装的 Stdio 类型 MCP 在安装时设置 `Source: "marketplace"`。`managed` 判定逻辑与远程 MCP 相同。
