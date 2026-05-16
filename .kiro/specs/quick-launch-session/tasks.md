# 实现任务：快速启动会话 (Quick Launch Session)

## 任务 1: 创建 ProviderResolver 核心模块
- [x] 创建 `provider_resolver.go`，定义 `ProviderResolveResult` 结构体和 `ProviderResolver` 类型
- [ ] 实现 `Resolve(toolCfg ToolConfig, providerOverride string) (ProviderResolveResult, error)` 方法：
  - 当 `providerOverride` 非空时，在 `toolCfg.Models` 中大小写不敏感匹配，找到则检查 `isValidProvider`，未找到或无效返回错误并附带可用服务商列表
  - 当 `providerOverride` 为空时，先尝试 `toolCfg.CurrentModel` 对应的服务商，若不可用则按 `Models` 列表顺序遍历其他 `isValidProvider` 的服务商
  - 降级时记录 `Fallback=true`、`OriginalName`、`Tried` 列表和 `Errors` 列表
  - 所有服务商均不可用时返回 error，包含所有尝试过的服务商及失败原因
- [ ] 创建 `provider_resolver_test.go`，覆盖以下场景：
  - 用户指定有效 provider → 直接使用
  - 用户指定不存在的 provider → 返回错误含可用列表
  - 用户指定存在但无 API Key 的 provider → 返回错误
  - 未指定 provider，默认服务商可用 → 使用默认
  - 未指定 provider，默认不可用，第二个可用 → 降级到第二个
  - 未指定 provider，所有服务商不可用 → 返回错误含全部失败原因
  - 服务商名称大小写不敏感匹配
  - 空 Models 列表 → 返回错误
  - 幂等性：相同输入两次调用结果一致

> 需求覆盖：需求 1（AC 1,2,4）、需求 2（AC 1,2,4）、需求 3（AC 1,2,3）、需求 7（AC 1,2,3,4）

## 任务 2: 在 toolCreateSession 中集成 ProviderResolver
- [ ] 在 `im_message_handler.go` 的 `toolCreateSession` 方法中，在预检之后、调用 `StartRemoteSessionForProject` 之前，加入 ProviderResolver 调用：
  - 通过 `remoteToolConfig(cfg, tool)` 获取 `ToolConfig`
  - 调用 `ProviderResolver.Resolve(toolCfg, provider)` 获取解析结果
  - 解析失败 → 返回错误消息（含已尝试的服务商和失败原因）
  - 解析成功且发生降级 → 在 hints 中添加降级说明（如 `⚡ 服务商已降级: Original → DeepSeek`）
  - 将解析出的 `Provider.ModelName` 作为 `Provider` 字段传入 `RemoteStartSessionRequest`
- [x] 修改成功返回消息，包含实际使用的服务商名称
- [ ] 在 `im_message_handler_tools_test.go` 中添加测试：
  - 未指定 provider 时使用默认服务商
  - 默认不可用时自动降级并在 hints 中体现
  - 用户指定 provider 时直接使用

> 需求覆盖：需求 1（AC 2,3）、需求 2（AC 3,5）、需求 3（AC 4）、需求 6（AC 1,2,3）

## 任务 3: 扩展 create_session 工具定义支持 project_id 参数
- [x] 在 `tool_registry_builtin.go` 的 `create_session` 工具定义中添加 `project_id` 参数：`"project_id": map[string]string{"type": "string", "description": "预设项目 ID（可选，与 project_path 二选一）"}`
- [ ] 在 `im_message_handler.go` 的 `toolCreateSession` 中解析 `project_id` 参数：
  - 当 `project_id` 非空时，从 `cfg.Projects` 中按 ID 查找项目，找到则使用其 `Path` 作为 `projectPath`
  - 当 `project_id` 和 `project_path` 都为空时，保持现有的 `contextResolver.ResolveProject()` 自动推断逻辑
  - 当 `project_id` 找不到对应项目时，返回错误消息并附带可用项目列表
- [ ] 在 `im_message_handler_tools_test.go` 中添加测试：
  - 通过 project_id 成功解析到项目
  - project_id 不存在时返回错误含可用项目列表
  - project_id 和 project_path 同时提供时 project_id 优先

> 需求覆盖：需求 4（AC 2,3,4）、需求 5（AC 1,2,3）

## 任务 4: 增加 list_projects 工具
- [x] 在 `tool_registry_builtin.go` 中注册 `list_projects` 工具，参数为空，返回已配置项目列表
- [ ] 在 `im_message_handler.go` 中实现 `toolListProjects()` 方法：
  - 调用 `app.LoadConfig()` 获取项目列表
  - 返回格式化的项目列表，包含项目 ID、名称、路径、是否为当前项目
- [ ] 在 `im_message_handler_tools_test.go` 中添加测试：
  - 有项目时返回格式化列表
  - 无项目时返回提示消息

> 需求覆盖：需求 4（AC 1）

## 任务 5: 增强启动反馈消息格式
- [x] 在 `toolCreateSession` 的成功返回消息中统一包含：会话 ID、工具名称、服务商名称、项目路径
- [x] 当发生降级时，额外包含降级说明行：`⚡ 服务商已降级: {原始} → {实际}（{原因}）`
- [x] 当启动失败时，返回包含失败原因和可操作修复建议的消息
- [x] 在 `im_message_handler.go` 的 `buildSystemPrompt` 中更新工具使用说明，告知 LLM 可以使用 `list_projects` 和 `project_id` 参数

> 需求覆盖：需求 6（AC 1,2,3,4）

## 任务 6: 端到端集成验证
- [x] 运行 `go test ./... -count=1` 确保所有现有测试通过
- [x] 验证 provider_resolver_test.go 全部通过
- [x] 验证 im_message_handler_tools_test.go 中新增的测试全部通过
- [x] 检查 `go vet ./...` 无警告

> 需求覆盖：全部需求的集成验证
