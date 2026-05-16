# 实施任务

## Task 1: MaclawLLMProvider 结构体扩展

- [x] 1.1 在 `corelib/types.go` 的 `MaclawLLMProvider` 结构体中新增 `AuthType string json:"auth_type,omitempty"`、`RefreshToken string json:"refresh_token,omitempty"`、`TokenExpiresAt int64 json:"token_expires_at,omitempty"` 三个字段
- [x] 1.2 在 `gui/corelib_aliases.go` 中确认 `MaclawLLMProvider` 类型别名已覆盖新字段（Go type alias 自动继承，无需修改，仅需验证）
- [x] 1.3 在 `gui/frontend/src/components/remote/OnboardingWizard.tsx` 的 `LLMProvider` 接口中新增 `auth_type?: string` 字段
- [x] 1.4 编写属性测试：MaclawLLMProvider JSON 序列化往返（Property 1）— 验证新增字段的 omitempty 行为和往返一致性

## Task 2: 预置 OpenAI Provider

- [x] 2.1 修改 `gui/app_maclaw_llm.go` 的 `defaultMaclawLLMProviders()` 函数，在列表首位添加 `{Name: "OpenAI", URL: "https://api.openai.com/v1", Model: "gpt-4o", AuthType: "oauth", ContextLength: 128000}`
- [x] 2.2 编写单元测试验证 `defaultMaclawLLMProviders()` 返回值：OpenAI 在首位、字段值正确
- [x] 2.3 编写属性测试：已保存的 provider 列表不被默认值覆盖（Property 8）

## Task 3: OAuth PKCE 核心模块

- [x] 3.1 创建 `corelib/oauth/oauth.go`，实现 `Config` 结构体、`GenerateCodeVerifier()`、`GenerateCodeChallenge()`、`BuildAuthURL()` 函数
- [x] 3.2 创建 `corelib/oauth/callback.go`，实现 `CallbackServer`（Start/Port/WaitForCode/Stop），成功和失败的 HTML 响应页面（含 3 秒自动关闭脚本）
- [x] 3.3 实现 `RunOAuthFlow()` 函数：生成 PKCE 参数 → 启动 Callback Server → 打开浏览器 → 等待回调 → 用 code 换 token → 返回 TokenResult
- [x] 3.4 实现 `ExchangeCode()` 函数：向 OpenAI Token Endpoint 发送 POST 请求，解析返回的 access_token/refresh_token/expires_in
- [x] 3.5 编写属性测试：code_verifier 符合 RFC 7636（Property 4）
- [x] 3.6 编写属性测试：code_challenge SHA256 Base64URL 确定性（Property 5）
- [x] 3.7 编写属性测试：授权 URL 包含所有必要参数（Property 6）
- [x] 3.8 编写单元测试：Callback Server 启动/停止/端口释放、HTML 响应内容验证

## Task 4: Token Manager

- [x] 4.1 创建 `corelib/oauth/token.go`，实现 `NeedsRefresh()`、`RefreshAccessToken()`、`ApplyTokenResult()`、`EnsureValidToken()` 函数
- [x] 4.2 编写属性测试：AuthType 空值向后兼容（Property 2）
- [x] 4.3 编写属性测试：NeedsRefresh 过期检测（Property 3）
- [x] 4.4 编写属性测试：TokenResult 正确应用到 Provider（Property 7）

## Task 5: Token 刷新集成到 LLM 请求

- [x] 5.1 在 `gui/app_maclaw_llm.go` 中新增 `ensureOAuthToken()` 辅助方法，在 `TestMaclawLLM` 和 `PingMaclawLLM` 调用前检查并刷新 OAuth token
- [x] 5.2 在 `tui/commands/llm.go` 的 `llmTest` 和 `llmPing` 中，调用 `oauth.EnsureValidToken` 在请求前刷新 token（当 provider 为 oauth 类型时）

## Task 6: GUI 后端 OAuth 方法

- [x] 6.1 在 `gui/app_maclaw_llm.go` 中实现 `StartOpenAIOAuth()` 方法：调用 `oauth.RunOAuthFlow`，成功后更新 provider 配置并持久化
- [x] 6.2 在 `gui/app_maclaw_llm.go` 中实现 `GetOpenAIUsage()` 方法：调用 `oauth.QueryUsage`，需要时先刷新 token

## Task 7: GUI 前端 OnboardingWizard 改造

- [x] 7.1 修改 OnboardingWizard.tsx：当 `selectedProvider.auth_type === "oauth"` 时，显示 "使用 OpenAI 账号登录" 按钮替代 API Key 输入表单
- [x] 7.2 实现 OAuth 登录按钮点击逻辑：调用 `StartOpenAIOAuth()` Wails binding，显示 "等待浏览器授权..." 加载状态，处理成功/失败
- [x] 7.3 将 "OpenAI" 选项在 provider 按钮列表中置于首位，附带 "使用 OpenAI 账号一键登录" 描述

## Task 8: GUI UsageDisplay 组件

- [x] 8.1 创建 `gui/frontend/src/components/remote/UsageDisplay.tsx` 组件：当前 provider 为 OAuth 类型时显示剩余额度信息
- [x] 8.2 在 LLM Config 设置区域集成 UsageDisplay 组件，调用 `GetOpenAIUsage()` 获取数据

## Task 9: TUI 命令扩展

- [x] 9.1 在 `tui/commands/llm.go` 中新增 `llm login openai` 子命令：调用 `oauth.RunOAuthFlow`，成功后更新 provider 配置
- [x] 9.2 在 `tui/commands/llm.go` 中新增 `llm usage` 子命令：调用 `oauth.QueryUsage`，输出用量信息
- [x] 9.3 修改 `llmProviders` 函数，在 provider 列表中显示 OAuth 认证状态（已认证/未认证/已过期）

## Task 10: Usage Query Service

- [x] 10.1 创建 `corelib/oauth/usage.go`，实现 `UsageInfo` 结构体和 `QueryUsage()` 函数：调用 OpenAI Billing API，解析返回数据
- [x] 10.2 编写单元测试：Usage API 响应解析（mock HTTP 响应）
