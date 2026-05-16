# Implementation Plan

## Overview

将 MCP 能力市场搜索提升为独立顶级 Tab，安装后按类型分流到本地/远程列表，强制下发的 MCP 不可删除。

## Tasks

- [x] 1. 前端 MCPTab 类型扩展 + Tab 栏新增能力市场按钮
  - `MCPTab` 类型从 `"local" | "remote"` 扩展为 `"local" | "remote" | "marketplace"`
  - `MCPManagementPanel` Tab 栏新增第三个按钮（能力市场）
  - `activeTab` 默认值改为 `"remote"`（保持现有行为）
  - `activeTab === "marketplace"` 时渲染 `MCPMarketplacePanel`
  - `appTranslations.ts` 新增 `mcpTabMarketplace` key（EN: "Marketplace", ZH: "能力市场"）
  - Files: `gui/frontend/src/components/remote/MCPManagementPanel.tsx`, `gui/frontend/src/i18n/appTranslations.ts`
  - _Requirements: 1_

- [x] 2. 从 RemoteMCPPanel 中移除 MCPMarketplacePanel 嵌入
  - 删除 `RemoteMCPPanel` 中 `<MCPMarketplacePanel ... />` 的渲染
  - 删除 `handleMarketplaceChanged` 回调（移到父组件）
  - 远程 Tab 只保留：服务器计数 + 导入 JSON + 注册按钮 + 服务器列表
  - Files: `gui/frontend/src/components/remote/MCPManagementPanel.tsx`
  - _Requirements: 1_

- [x] 3. MCPManagementPanel 维护 installedCapabilityIDs 统一状态
  - `MCPManagementPanel` 新增 `installedCapabilityIDs` state
  - mount 时从 `ListMCPServers()` + `ListLocalMCPServers()` 合并计算已安装 capability IDs
  - 传递给 `MCPMarketplacePanel` 的 `installedCapabilities` prop
  - `MCPMarketplacePanel.onChanged` 触发时重新计算
  - `onChanged` 同时通知 `LocalMCPPanel` 和 `RemoteMCPPanel` 刷新列表
  - Files: `gui/frontend/src/components/remote/MCPManagementPanel.tsx`
  - _Requirements: 1, 2_

- [x] 4. 前端 MCPServerView + LocalMCPServer 新增 source/managed 字段
  - `MCPServerView` interface 新增 `source: string` 和 `managed: boolean`
  - `LocalMCPServer` interface 新增 `source?: string` 和 `managed?: boolean`
  - `MCPRemoteServerRow.tsx` 中的 `MCPServerView` 同步更新
  - Files: `gui/frontend/src/components/remote/MCPManagementPanel.tsx`, `gui/frontend/src/components/remote/MCPRemoteServerRow.tsx`
  - _Requirements: 4_

- [x] 5. MCPRemoteServerRow 强制下发 UI 保护
  - `managed === true` 时：隐藏删除按钮，显示 🔒 图标 + "企业下发" 标签
  - `managed === true` 时：编辑按钮保留（用户可配置密钥）
  - `source === "marketplace"` 且 `managed === false` 时：正常显示删除按钮（用户主动安装的可删除）
  - `appTranslations.ts` 新增 `mcpManagedLabel`（EN: "Managed", ZH: "企业下发"）和 `mcpCannotDeleteManaged`
  - Files: `gui/frontend/src/components/remote/MCPRemoteServerRow.tsx`, `gui/frontend/src/i18n/appTranslations.ts`
  - _Requirements: 3_

- [x] 6. 本地 MCP 列表强制下发 UI 保护
  - `LocalMCPPanel` 中 `managed === true` 的本地 MCP：隐藏删除按钮，显示 🔒 标签
  - 来源为 `"marketplace"` 的本地 MCP 显示来源标签
  - Files: `gui/frontend/src/components/remote/MCPManagementPanel.tsx`
  - _Requirements: 3, 4_

- [x] 7. 后端 MCPServerView 新增 Managed 字段 + ListMCPServers 计算
  - `MCPServerView` struct 新增 `Managed bool` JSON 字段
  - `ListMCPServers` 构建 view 时调用 `isManagedCapability(entry)` 计算 managed
  - 新增 `isManagedCapability(entry)` 方法：检查 `Source == marketplace` + `isCapabilityManagedDeployment(capabilityID)`
  - Files: `gui/app_nl_mcp.go`
  - _Requirements: 4, 5_

- [x] 8. 后端缓存 managedDeployments + isCapabilityManagedDeployment 查询
  - `capability_market_sync.go` 在 `syncHubManagedCapabilities` 成功后缓存 `managedDeployments` 列表到 App 字段
  - 新增 `isCapabilityManagedDeployment(capabilityID string) bool` 方法：从缓存中查找匹配的 deployment 且 `ReinstallIfRemoved == true`
  - 缓存为 `sync.Map` 或 `map[string]bool`（capability_ref → managed），受 mutex 保护
  - Files: `gui/capability_market_sync.go`
  - _Requirements: 5_

- [x] 9. 后端 UnregisterMCPServer 强制下发保护
  - `UnregisterMCPServer` 在调用 `registry.Unregister` 前检查 `isManagedCapability`
  - 满足条件时返回 error："此 MCP 为企业强制下发，不可删除"
  - `UnregisterLocalMCPServer` 同步添加相同检查
  - Files: `gui/app_nl_mcp.go`
  - _Requirements: 5_

- [x] 10. 后端本地 MCP 的 source/managed 字段支持
  - `LocalMCPServerConfig`（或等效 struct）新增 `Source` 字段
  - 市场安装 Stdio 类型 MCP 时设置 `Source: "marketplace"`
  - `ListLocalMCPServers` 返回时包含 `source` 和 `managed` 信息
  - `GetLocalMCPServerStatuses` 或等效 API 传递 source/managed
  - Files: `gui/local_mcp_manager.go`, `gui/app_nl_mcp.go`
  - _Requirements: 2, 4_

- [x] 11. 能力市场安装分流——Stdio 类型安装到本地列表
  - `installCapabilityByType` 根据 `capability_type` 分流：
    - `"mcp_stdio"` → 调用 `installCapabilityAsLocalMCP`（写入 local_mcp_servers.json）
    - `"mcp_http"` / `"mcp"` → 现有逻辑（写入 mcp_servers.json）
  - 新增 `installCapabilityAsLocalMCP` 方法：从 capability metadata 提取 command/args/env，创建本地 MCP 配置
  - Files: `gui/capability_market_sync.go`, `gui/local_mcp_manager.go`
  - _Requirements: 2_

- [x] 12. MCPMarketplacePanel onChanged 回调增强
  - `onChanged` 回调参数新增 `installed_type?: "local" | "remote"` 字段
  - 父组件根据 `installed_type` 决定刷新哪个列表（或两个都刷新）
  - 同步下发完成后也触发 `onChanged`（可能同时安装了本地和远程类型）
  - Files: `gui/frontend/src/components/remote/MCPMarketplacePanel.tsx`, `gui/frontend/src/components/remote/MCPManagementPanel.tsx`
  - _Requirements: 2_

## Task Dependency Graph

```
1 --> 2
1 --> 3
2 --> 3
4 --> 5
4 --> 6
7 --> 9
8 --> 7
8 --> 9
10 --> 11
3 --> 12
11 --> 12
```

## Priority Order

P0（核心体验）: 1, 2, 3, 4, 5
P1（后端保护）: 7, 8, 9
P2（分流 + 本地支持）: 6, 10, 11, 12

## Notes

- Task 1-3 是纯前端改动，可以先行实现并验证 UI 效果
- Task 7-9 是后端保护机制，需要 managed deployment 缓存作为前提
- Task 10-11 涉及安装分流，需要后端 capability_type 字段支持
- 现有 `MCPMarketplacePanel` 组件的内部逻辑不需要改动，只改变挂载位置和 props 传递方式
