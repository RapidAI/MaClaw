# 技术设计：快速启动会话 (Quick Launch Session)

## 概述

在现有 `toolCreateSession` → `buildRemoteLaunchSpec` 流程中引入 `ProviderResolver`，实现服务商自动降级回退。同时增强 `create_session` 工具参数以支持 `project_id` 项目选择模式。

## 架构变更

### 新增文件

- `provider_resolver.go` — ProviderResolver 核心逻辑
- `provider_resolver_test.go` — 单元测试

### 修改文件

- `im_message_handler.go` — toolCreateSession 集成 ProviderResolver
- `app.go` — buildRemoteLaunchSpec 支持接收已解析的 ModelConfig
- `tool_registry_builtin.go` — create_session 工具定义增加 project_id 参数

## 详细设计

### 1. ProviderResolver（新增）

```go
// provider_resolver.go
package main

// ProviderResolveResult 服务商解析结果
type ProviderResolveResult struct {
    Provider    ModelConfig // 最终选中的服务商
    Fallback    bool        // 是否发生了降级
    OriginalName string     // 原始目标服务商名称（降级时有值）
    Reason      string      // 选择原因描述
    Tried       []string    // 已尝试的服务商名称列表
    Errors      []string    // 各服务商的失败原因
}

// ProviderResolver 服务商解析器
type ProviderResolver struct{}

// Resolve 解析服务商，支持三种模式：
// 1. providerOverride 非空 → 直接使用指定服务商（不降级）
// 2. providerOverride 为空 → 使用 CurrentModel 默认服务商
// 3. 默认服务商不可用 → 按 Models 列表顺序降级
func (r *ProviderResolver) Resolve(
    toolCfg ToolConfig,
    providerOverride string,
) (ProviderResolveResult, error)
```

解析逻辑：
- 用户指定 provider → 精确匹配（大小写不敏感），找不到或无效直接报错
- 未指定 provider → 先尝试 CurrentModel，失败则遍历 Models 列表中其他 `isValidProvider` 的服务商
- 全部失败 → 返回 error，附带所有已尝试的服务商和失败原因

### 2. toolCreateSession 改造

当前流程：
```
toolCreateSession → StartRemoteSessionForProject → buildRemoteLaunchSpec（内部解析 provider）
```

改造后流程：
```
toolCreateSession
  ├─ 解析 tool（已有 contextResolver）
  ├─ 解析 project（已有 contextResolver + 新增 project_id 支持）
  ├─ 预检（已有 sessionPrecheck）
  ├─ ProviderResolver.Resolve(toolCfg, provider) ← 新增
  │   ├─ 成功 → 带已解析的 provider 调用 StartRemoteSessionForProject
  │   └─ 降级 → hints 中注明降级信息
  └─ 返回结果（含服务商信息）
```

### 3. create_session 工具参数扩展

```go
// tool_registry_builtin.go — create_session 参数增加 project_id
"project_id": map[string]string{
    "type": "string",
    "description": "预设项目 ID（可选，与 project_path 二选一）",
}
```

toolCreateSession 中增加 project_id 解析：优先使用 project_id 从配置中查找项目，其次使用 project_path。

### 4. buildRemoteLaunchSpec 适配

当前 `buildRemoteLaunchSpec` 内部做 provider 解析。改造后：
- toolCreateSession 通过 ProviderResolver 预先解析好 provider
- 将解析结果（provider name）传入 `providerOverride` 参数
- buildRemoteLaunchSpec 逻辑不变，只是调用方保证传入的 provider 一定有效

这样改动最小，不破坏桌面端直接调用 buildRemoteLaunchSpec 的路径。

### 5. 反馈消息格式

成功（无降级）：
```
✅ 会话已创建 [session_id]
🔧 工具: claude | 📦 服务商: DeepSeek | 📁 项目: /path/to/project
```

成功（有降级）：
```
✅ 会话已创建 [session_id]
🔧 工具: claude | 📁 项目: /path/to/project
⚡ 服务商已降级: Original → DeepSeek（Original 未配置 API Key）
```

失败（所有服务商不可用）：
```
❌ 无法创建会话：所有服务商均不可用
- Original: 未配置 API Key
- DeepSeek: 未配置 API Key
请在桌面端为 Claude 配置至少一个有效的服务商。
```
