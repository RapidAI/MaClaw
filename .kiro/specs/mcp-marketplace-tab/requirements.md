# MCP 能力市场独立 Tab + 安装分流 + 强制下发保护

## 概述

将 MCP 能力市场搜索从嵌入在"远程 (HTTP)"tab 内部的位置，提升为独立的顶级 Tab（与"本地 (Stdio)"和"远程 (HTTP)"并列）。安装后的 MCP 按实际类型（Stdio/HTTP）分流到对应的本地/远程列表中。强制下发的 MCP 不能删除，只能查看。

## 需求

### 需求 1: 能力市场作为独立顶级 Tab

**用户故事**: 作为用户，我希望能力市场搜索是一个独立的 Tab，与本地和远程并列，而不是藏在远程 Tab 内部。

**验收标准**:
- MCP 管理面板顶部 Tab 栏显示三个 Tab：`本地 (Stdio)` | `远程 (HTTP)` | `能力市场`
- 能力市场 Tab 包含：搜索框 + 搜索结果列表 + 推荐能力 + 同步下发按钮
- 从 `RemoteMCPPanel` 中移除 `MCPMarketplacePanel` 的嵌入
- 远程 Tab 只显示已注册的远程 MCP 服务器列表和管理操作

### 需求 2: 安装后按类型分流到本地/远程列表

**用户故事**: 作为用户，我从能力市场安装一个 MCP 后，它应该出现在正确的列表中——Stdio 类型出现在本地列表，HTTP 类型出现在远程列表。

**验收标准**:
- 能力市场安装的 MCP 根据其 `capability_type` 决定归属：
  - `capability_type == "mcp_stdio"` → 安装到本地 MCP 列表
  - `capability_type == "mcp_http"` 或 `capability_type == "mcp"` → 安装到远程 MCP 列表
- 安装完成后，对应列表自动刷新显示新安装的 MCP
- 能力市场 Tab 中已安装的能力显示"已安装"标记（不可重复安装）

### 需求 3: 强制下发的 MCP 不能删除

**用户故事**: 作为用户，企业管理员强制下发的 MCP 我不能删除，只能查看其配置和工具列表。

**验收标准**:
- 强制下发的 MCP（`Source == "marketplace"` 且对应的 `HubCapabilityDeployment.ReinstallIfRemoved == true`）在列表中不显示删除按钮
- 强制下发的 MCP 显示锁定图标（）和"企业下发"标签
- 强制下发的 MCP 仍可查看工具列表、健康状态、配置密钥
- 强制下发的 MCP 仍可编辑密钥配置（用户需要填入自己的 API Key）
- 非强制的市场安装 MCP（用户主动搜索安装的）可以正常删除

### 需求 4: 前端感知 MCP 来源

**用户故事**: 作为开发者，前端需要知道每个 MCP 的来源（手动注册/市场安装/强制下发），以决定 UI 行为。

**验收标准**:
- 前端 `MCPServerView` 接口新增 `source` 字段（`"manual"` | `"mdns"` | `"project"` | `"marketplace"`）
- 前端 `MCPServerView` 接口新增 `managed` 字段（`boolean`，标识是否为强制下发不可删除）
- 后端 `ListMCPServers` 返回的数据包含 `source` 和 `managed` 字段
- 本地 MCP 列表中，市场安装的也显示来源标签

### 需求 5: 后端删除操作检查强制下发保护

**用户故事**: 作为系统，当用户尝试删除强制下发的 MCP 时，后端应拒绝操作并返回明确错误。

**验收标准**:
- `UnregisterMCPServer` 在删除前检查 `Source == "marketplace"` 且 `managed == true`
- 满足条件时返回错误："此 MCP 为企业强制下发，不可删除"
- 非强制的市场安装 MCP 可正常删除
- 本地 MCP 的 `UnregisterLocalMCPServer` 同样检查强制下发保护

## 约束

- 能力市场 Tab 的搜索/安装逻辑复用现有 `MCPMarketplacePanel` 组件，只改变其挂载位置
- 后端 `MCPServerEntry` 已有 `Source` 字段，只需确保前端正确消费
- 强制下发判定逻辑：`Source == "marketplace"` + 对应 deployment 的 `ReinstallIfRemoved == true`
- 向后兼容：旧版本安装的 MCP（`Source` 为空）视为 `"manual"`，不受强制下发保护

## 非功能需求

- Tab 切换延迟 < 50ms（纯前端状态切换）
- 能力市场搜索延迟不变（已有机制）
- 强制下发判定在 `ListMCPServers` 时一次性计算，不增加额外 API 调用
