# 任务列表：编程会话启动时服务商选择

## 任务 1: 添加 isValidProvider 和 validProviders 工具函数
- [x] 在 `remote_tool_catalog.go` 中添加 `isValidProvider(m ModelConfig) bool` 函数
- [x] 在 `remote_tool_catalog.go` 中添加 `validProviders(tc ToolConfig) []ModelConfig` 函数
- [x] 判定逻辑：`strings.EqualFold(m.ModelName, "original")` 或 `strings.TrimSpace(m.ApiKey) != ""`
- [x] 添加单元测试 `TestIsValidProvider` 覆盖：Original（有效）、有 ApiKey（有效）、空 ApiKey 非 Original（无效）、空 ModelName（无效）
- [x] 添加单元测试 `TestValidProviders` 验证过滤结果不含无效服务商

**需求映射**: 需求 1（有效服务商统一判定）

## 任务 2: 修改 buildRemoteLaunchSpec 支持 providerOverride
- [x] 在 `app.go` 中修改 `buildRemoteLaunchSpec` 签名，末尾增加 `providerOverride string` 参数
- [x] 实现逻辑：`providerOverride` 非空时替代 `toolCfg.CurrentModel` 查找 ModelConfig
- [x] 查找到 ModelConfig 后调用 `isValidProvider` 校验，无效则返回 `"provider %q has no API key configured"` 错误
- [x] `providerOverride` 指定的名称在 Models 中未找到时返回 `"provider %q not found for tool %s"` 错误
- [x] 对默认路径（`providerOverride` 为空）也增加 `isValidProvider` 校验
- [x] 修改 `buildClaudeLaunchSpec` 调用，末尾传 `""`
- [x] 添加单元测试：providerOverride 为空时使用 CurrentModel、providerOverride 有效时覆盖、providerOverride 无效时报错、providerOverride 不存在时报错

**需求映射**: 需求 4（核心构建函数支持服务商覆盖）、需求 7（启动时服务商有效性校验）

## 任务 3: 适配所有 buildRemoteLaunchSpec 调用方
- [x] `remote_status.go` — `StartRemoteSession`: 末尾加 `""` 参数
- [x] `remote_status.go` — `StartRemoteHandoffSession`: 末尾加 `""` 参数
- [x] `remote_mobile_launch.go` — `StartRemoteSessionForProject`: 传 `req.Provider`（任务 4 中新增的字段）
- [x] `app.go:2437` — 桌面端 launch 调用: 末尾加 `""` 参数
- [x] `remote_diagnostics.go` — readiness 和 smoke 调用（2处）: 末尾加 `""` 参数
- [x] `remote_status_test.go` — 所有测试中的调用: 末尾加 `""` 参数
- [x] `remote_activation_test.go` — `buildClaudeLaunchSpec` 测试调用不受影响（通过 wrapper）
- [x] 确认编译通过，所有现有测试仍然 pass

**需求映射**: 需求 4（核心构建函数支持服务商覆盖）

## 任务 4: RemoteStartSessionRequest 增加 Provider 字段
- [x] 在 `remote_mobile_launch.go` 的 `RemoteStartSessionRequest` 结构体中增加 `Provider string \`json:"provider,omitempty"\``
- [x] 在 `StartRemoteSessionForProject` 中将 `req.Provider` 传递给 `buildRemoteLaunchSpec` 的 `providerOverride` 参数
- [x] 添加单元测试：Provider 为空时使用默认、Provider 有效时覆盖、Provider 无效时返回错误

**需求映射**: 需求 3（PWA/移动端启动会话时选择服务商）

## 任务 5: create_session 工具增加 provider 参数
- [x] 在 `im_message_handler.go` 的 `buildToolDefinitions` 中，`create_session` 工具定义增加 `provider` 参数（可选，description 说明用途和示例值）
- [x] 在 `toolCreateSession` 处理器中提取 `provider` 参数：`provider, _ := args["provider"].(string)`
- [x] 将 `provider` 传入 `RemoteStartSessionRequest{..., Provider: provider}`
- [x] 当 provider 指定了无效服务商时，在错误信息中附加可用服务商列表（调用 `validProviders`）
- [x] 添加单元测试：不传 provider 时行为不变、传有效 provider 时覆盖、传无效 provider 时报错

**需求映射**: 需求 2（IM Agent 端创建会话时选择服务商）

## 任务 6: 新增 list_providers Agent 工具
- [x] 在 `im_message_handler.go` 的 `buildToolDefinitions` 中添加 `list_providers` 工具定义
- [x] 在 `executeTool` switch 中添加 `case "list_providers"` 路由
- [x] 实现 `toolListProviders` 处理器：加载配置、获取 ToolConfig、调用 `validProviders` 过滤、格式化输出（名称、model_id 脱敏、是否默认）
- [x] 无有效服务商时返回提示信息
- [x] 添加单元测试

**需求映射**: 需求 5（列出可用服务商工具）

## 任务 7: 新增 ListValidProviders Wails 绑定
- [x] 在 `remote_status.go` 中添加 `ProviderView` 结构体（Name、ModelID、IsDefault）
- [x] 在 `remote_status.go` 中添加 `func (a *App) ListValidProviders(toolName string) ([]ProviderView, error)` 方法
- [x] 内部调用 `remoteToolConfig` + `validProviders` 过滤
- [x] 添加单元测试

**需求映射**: 需求 6（PWA 前端服务商选择界面 — 后端支持）

## 任务 8: 修改 StartRemoteSession Wails 绑定支持 provider 参数
- [x] 修改 `remote_status.go` 中 `StartRemoteSession` 签名：增加 `provider string` 参数
- [x] 将 `provider` 传递给 `buildRemoteLaunchSpec` 的 `providerOverride`
- [x] 同步修改 `StartRemoteHandoffSession` 签名增加 `provider string` 参数
- [x] 修改 `StartRemoteClaudeSession` 调用适配（传 `""`）
- [x] 更新 `frontend/wailsjs/go/main/App.d.ts` 类型声明（Wails 自动生成或手动更新）

**需求映射**: 需求 6（PWA 前端服务商选择界面 — 后端支持）

## 任务 9: PWA 前端服务商选择器
- [x] 在 `useRemotePanel.ts` 中新增 state：`providers: ProviderView[]`、`selectedProvider: string`
- [x] 工具切换时（`selectedRemoteTool` 变化）调用 `ListValidProviders(tool)` 刷新 providers 列表
- [x] `selectedProvider` 默认值为 providers 中 `isDefault=true` 的项
- [x] 修改 `startRemoteSession` 调用：`StartRemoteSession(selectedRemoteTool, projectDir, getUseProxy(), selectedProvider)`
- [x] 修改 `quickStartRemoteSession` 调用：同上传递 `selectedProvider`
- [x] 在 `RemoteRoutingCard.tsx` 启动按钮区域增加服务商下拉选择器（`<select>`），仅展示有效服务商
- [x] 下拉选择器中当前默认服务商标注 "(默认)" 后缀
- [x] 添加国际化文本：`remoteProviderLabel`（"服务商"/"Provider"）

**需求映射**: 需求 6（PWA 前端服务商选择界面）
