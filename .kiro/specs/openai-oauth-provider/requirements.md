# Requirements Document

## Introduction

在 MaClaw 的 onboarding 流程中新增 OpenAI OAuth 登录选项（方案 A：复用 Codex CLI 的 OAuth client_id）。用户选择 OpenAI 后，通过浏览器完成 OAuth PKCE 认证，自动获取 access_token 和 refresh_token，无需手动输入 API Key。token 过期时自动刷新，用户无感。同时支持 GUI 桌面端和 TUI 终端两个入口。GUI 设置页面和 TUI 终端均提供 OpenAI 套餐用量/剩余额度查询功能，帮助用户诊断 token 耗尽等问题。Onboarding 向导明确将 OpenAI 作为首选入口展示。

## Glossary

- **OAuth_Module**: 负责 OpenAI OAuth PKCE 流程的核心模块，包含授权 URL 构建、本地回调服务器、code 换 token、token 刷新等功能，位于 corelib 层
- **Callback_Server**: OAuth 流程中在本地启动的 HTTP 服务器，监听 `http://127.0.0.1:<port>/auth/callback`，接收 OpenAI 授权回调并提取 authorization code
- **Token_Manager**: 负责 token 生命周期管理的组件，包括 token 存储、过期检测、自动刷新
- **MaclawLLMProvider**: corelib/types.go 中定义的 LLM 提供商配置结构体，需扩展 OAuth 相关字段
- **OnboardingWizard**: GUI 前端的 onboarding 向导组件，位于 gui/frontend/src/components/remote/OnboardingWizard.tsx
- **TUI_LLM_Command**: TUI 端的 llm 子命令模块，位于 tui/commands/llm.go
- **PKCE**: Proof Key for Code Exchange，OAuth 2.0 扩展，使用 code_verifier 和 code_challenge（S256）防止授权码拦截攻击
- **OpenAI_Auth_Endpoint**: OpenAI 的 OAuth 授权端点 `https://auth.openai.com/oauth/authorize`
- **OpenAI_Token_Endpoint**: OpenAI 的 OAuth token 端点 `https://auth.openai.com/oauth/token`
- **Usage_Display**: GUI 设置页面中 LLM Config 区域展示 OpenAI 账户剩余用量/额度的 UI 组件
- **OpenAI_Billing_API**: OpenAI 的计费/用量查询 API（如 `https://api.openai.com/dashboard/billing/credit_grants` 或 `/v1/dashboard/billing/usage` 等端点），用于获取账户剩余额度和用量统计信息
- **Usage_Query_Service**: 位于 corelib 层的用量查询服务，封装 OpenAI_Billing_API 调用逻辑，供 GUI 和 TUI 共同使用

## Requirements

### Requirement 1: MaclawLLMProvider 结构体扩展

**User Story:** 作为开发者，我希望 MaclawLLMProvider 结构体支持 OAuth 认证类型的字段，以便区分 API Key 认证和 OAuth 认证的 provider，并存储 OAuth token 相关信息。

#### Acceptance Criteria

1. THE MaclawLLMProvider SHALL 包含 `AuthType` 字段，取值为 `"api_key"`（默认）或 `"oauth"`
2. THE MaclawLLMProvider SHALL 包含 `RefreshToken` 字段，用于存储 OAuth refresh_token
3. THE MaclawLLMProvider SHALL 包含 `TokenExpiresAt` 字段（Unix 时间戳），用于记录 access_token 的过期时间
4. WHEN `AuthType` 字段为空或未设置时，THE Token_Manager SHALL 将其视为 `"api_key"` 类型，保持向后兼容
5. WHEN MaclawLLMProvider 被序列化为 JSON 时，THE MaclawLLMProvider SHALL 使用 `omitempty` 标签确保新增字段在未设置时不出现在 JSON 输出中

### Requirement 2: 预置 OpenAI Provider

**User Story:** 作为用户，我希望在 MaClaw LLM provider 列表中看到一个预置的 "OpenAI" 选项，以便快速通过 OAuth 登录使用 OpenAI 服务。

#### Acceptance Criteria

1. THE defaultMaclawLLMProviders 函数 SHALL 在 provider 列表中包含一个 Name 为 `"OpenAI"` 的预置条目
2. THE OpenAI 预置条目 SHALL 设置 URL 为 `"https://api.openai.com/v1"`、Model 为 `"gpt-4o"`、AuthType 为 `"oauth"`、ContextLength 为 `128000`
3. THE OpenAI 预置条目 SHALL 排列在 provider 列表的第一个位置
4. WHEN 用户已有保存的 provider 列表时，THE GetMaclawLLMProviders 函数 SHALL 保留用户已有的配置不被覆盖

### Requirement 3: OAuth PKCE 流程实现

**User Story:** 作为用户，我希望选择 OpenAI 后能通过浏览器完成 OAuth 认证，无需手动复制粘贴 API Key。

#### Acceptance Criteria

1. WHEN 用户触发 OpenAI OAuth 登录时，THE OAuth_Module SHALL 生成符合 RFC 7636 的 code_verifier（至少 43 字符的随机字符串）和 code_challenge（SHA256 + Base64URL 编码）
2. WHEN OAuth 流程启动时，THE OAuth_Module SHALL 构建包含 `client_id`、`redirect_uri`、`response_type=code`、`code_challenge`、`code_challenge_method=S256`、`scope` 参数的授权 URL，指向 OpenAI_Auth_Endpoint
3. WHEN 授权 URL 构建完成后，THE OAuth_Module SHALL 调用系统默认浏览器打开该 URL
4. WHEN OAuth 流程启动时，THE Callback_Server SHALL 在 `127.0.0.1` 上启动 HTTP 服务器监听回调请求
5. IF Callback_Server 的默认端口被占用，THEN THE Callback_Server SHALL 自动尝试其他可用端口
6. WHEN Callback_Server 收到包含 `code` 参数的回调请求时，THE OAuth_Module SHALL 使用该 code 和 code_verifier 向 OpenAI_Token_Endpoint 发送 POST 请求换取 token
7. WHEN token 交换成功时，THE OAuth_Module SHALL 返回 access_token、refresh_token 和 expires_in
8. IF 回调请求包含 `error` 参数，THEN THE OAuth_Module SHALL 返回包含 error 和 error_description 的错误信息
9. IF 用户在 120 秒内未完成浏览器授权，THEN THE OAuth_Module SHALL 超时并关闭 Callback_Server，返回超时错误
10. WHEN OAuth 流程完成（成功或失败）后，THE Callback_Server SHALL 停止监听并释放端口

### Requirement 4: Token 存储与 Provider 配置更新

**User Story:** 作为用户，我希望 OAuth 认证成功后 token 自动保存到 provider 配置中，下次启动无需重新登录。

#### Acceptance Criteria

1. WHEN OAuth 认证成功时，THE Token_Manager SHALL 将 access_token 写入 MaclawLLMProvider 的 `Key` 字段
2. WHEN OAuth 认证成功时，THE Token_Manager SHALL 将 refresh_token 写入 MaclawLLMProvider 的 `RefreshToken` 字段
3. WHEN OAuth 认证成功时，THE Token_Manager SHALL 根据 `expires_in` 计算过期时间戳并写入 MaclawLLMProvider 的 `TokenExpiresAt` 字段
4. WHEN token 信息更新后，THE Token_Manager SHALL 调用 SaveMaclawLLMProviders 持久化配置
5. WHEN provider 配置被持久化时，THE SaveMaclawLLMProviders 函数 SHALL 同步更新 legacy 字段（MaclawLLMUrl、MaclawLLMKey 等）以保持向后兼容

### Requirement 5: Token 自动刷新

**User Story:** 作为用户，我希望 access_token 过期时系统自动刷新，不中断我的使用体验。

#### Acceptance Criteria

1. WHEN DoSimpleLLMRequest 被调用且当前 provider 的 AuthType 为 `"oauth"` 时，THE Token_Manager SHALL 在发送请求前检查 access_token 是否即将过期（过期前 5 分钟视为即将过期）
2. WHEN access_token 即将过期且 refresh_token 存在时，THE Token_Manager SHALL 向 OpenAI_Token_Endpoint 发送 refresh_token grant 请求获取新的 access_token
3. WHEN token 刷新成功时，THE Token_Manager SHALL 更新 MaclawLLMProvider 的 Key、RefreshToken（如果返回了新的）和 TokenExpiresAt 字段，并持久化配置
4. IF token 刷新失败，THEN THE Token_Manager SHALL 返回包含刷新失败原因的错误信息，提示用户重新登录
5. IF refresh_token 不存在或为空，THEN THE Token_Manager SHALL 返回错误信息，提示用户重新进行 OAuth 登录

### Requirement 6: GUI OnboardingWizard 集成

**User Story:** 作为 GUI 桌面端用户，我希望在 onboarding 向导中看到 "OpenAI" 按钮，点击后通过浏览器完成 OAuth 登录。

#### Acceptance Criteria

1. THE OnboardingWizard SHALL 在 LLM provider 按钮列表中显示 "OpenAI" 选项
2. WHEN 用户选择 AuthType 为 `"oauth"` 的 provider 时，THE OnboardingWizard SHALL 显示 "使用 OpenAI 账号登录" 按钮，替代 API Key 输入表单
3. WHEN 用户点击 "使用 OpenAI 账号登录" 按钮时，THE OnboardingWizard SHALL 调用后端 OAuth 流程并显示 "等待浏览器授权..." 的加载状态
4. WHEN OAuth 认证成功时，THE OnboardingWizard SHALL 显示成功提示并将 LLM 步骤标记为已完成
5. IF OAuth 认证失败或超时，THEN THE OnboardingWizard SHALL 显示错误信息并允许用户重试
6. WHEN 用户选择 AuthType 为 `"api_key"` 或 `IsCustom` 为 true 的 provider 时，THE OnboardingWizard SHALL 保持现有的 API Key 输入表单不变

### Requirement 7: TUI OAuth 登录命令

**User Story:** 作为 TUI 终端用户，我希望通过命令行完成 OpenAI OAuth 登录，获得与 GUI 相同的体验。

#### Acceptance Criteria

1. THE TUI_LLM_Command SHALL 支持 `maclaw-tui llm login openai` 子命令
2. WHEN `llm login openai` 命令执行时，THE TUI_LLM_Command SHALL 在终端输出 "正在打开浏览器进行 OpenAI 授权..." 提示
3. WHEN `llm login openai` 命令执行时，THE TUI_LLM_Command SHALL 调用 OAuth_Module 启动 PKCE 流程
4. WHEN OAuth 认证成功时，THE TUI_LLM_Command SHALL 输出成功信息并将 OpenAI 设置为当前 provider
5. IF OAuth 认证失败或超时，THEN THE TUI_LLM_Command SHALL 输出错误信息并返回非零退出码
6. WHEN `llm providers` 命令执行时，THE TUI_LLM_Command SHALL 在 provider 列表中显示 OpenAI 条目及其 OAuth 认证状态

### Requirement 8: Callback 页面用户反馈

**User Story:** 作为用户，我希望在浏览器完成 OAuth 授权后看到清晰的反馈页面，知道可以关闭浏览器回到应用。

#### Acceptance Criteria

1. WHEN Callback_Server 成功接收到授权码时，THE Callback_Server SHALL 返回一个 HTML 页面，显示 "授权成功，请返回 MaClaw" 的提示
2. WHEN Callback_Server 接收到错误回调时，THE Callback_Server SHALL 返回一个 HTML 页面，显示错误原因和重试建议
3. THE Callback_Server 返回的 HTML 页面 SHALL 在 3 秒后自动尝试关闭浏览器标签页

### Requirement 9: OpenAI 套餐用量查询

**User Story:** 作为用户，我希望在 GUI 设置页面和 TUI 终端中查询 OpenAI 账户的套餐用量信息（剩余额度、使用统计），以便及时了解账户状态并诊断 token 耗尽等问题。

#### Acceptance Criteria

1. THE Usage_Query_Service SHALL 封装 OpenAI_Billing_API 的调用逻辑，使用 OAuth access_token 作为 Bearer token 发起请求
2. WHEN Usage_Query_Service 查询成功时，THE Usage_Query_Service SHALL 返回包含剩余额度（美元单位）、已用额度和总额度的结构化数据
3. IF access_token 已过期，THEN THE Usage_Query_Service SHALL 先尝试通过 Token_Manager 刷新 token，再发起用量查询
4. IF access_token 刷新失败，THEN THE Usage_Query_Service SHALL 返回错误信息，提示用户重新登录 OpenAI
5. IF OpenAI_Billing_API 请求失败（网络错误或非 2xx 响应），THEN THE Usage_Query_Service SHALL 返回包含错误原因的错误信息
6. WHEN 当前选中的 provider 为 OpenAI（AuthType 为 `"oauth"`）时，THE Usage_Display SHALL 在 GUI 设置 → LLM Config 页面中显示剩余额度信息
7. WHEN 用户打开 LLM Config 页面且当前 provider 为 OpenAI 时，THE Usage_Display SHALL 调用 Usage_Query_Service 查询并展示剩余额度数值和用量摘要
8. IF Usage_Query_Service 返回错误，THEN THE Usage_Display SHALL 显示 "无法获取用量信息" 的提示及错误原因
9. WHEN 当前选中的 provider 不是 OAuth 类型时，THE Usage_Display SHALL 隐藏剩余额度区域，不影响现有 UI
10. THE TUI_LLM_Command SHALL 支持 `maclaw-tui llm usage` 子命令，用于查询 OpenAI 套餐用量
11. WHEN `llm usage` 命令执行且当前 provider 为 OpenAI 时，THE TUI_LLM_Command SHALL 调用 Usage_Query_Service 查询并在终端输出剩余额度、已用额度和总额度信息
12. IF `llm usage` 命令执行时当前 provider 不是 OAuth 类型，THEN THE TUI_LLM_Command SHALL 输出 "当前 provider 不支持用量查询" 的提示

### Requirement 10: Onboarding 流程 OpenAI 入口

**User Story:** 作为首次启动 MaClaw 的用户，我希望在 onboarding 向导中明确看到 OpenAI 作为可选的 LLM provider，以便直接进入 OAuth 登录流程。

#### Acceptance Criteria

1. WHEN 用户首次启动 MaClaw 进入 onboarding 向导时，THE OnboardingWizard SHALL 在 provider 选择步骤中显示 "OpenAI" 作为独立的可选项
2. THE OnboardingWizard SHALL 将 "OpenAI" 选项置于 provider 列表的首位，并附带 "使用 OpenAI 账号一键登录" 的描述文字
3. WHEN 用户在 onboarding 中选择 "OpenAI" 选项时，THE OnboardingWizard SHALL 直接进入 OAuth 登录流程，跳过 API Key 输入步骤
4. WHEN OAuth 登录成功后，THE OnboardingWizard SHALL 自动将 OpenAI 设置为当前 provider 并继续 onboarding 的下一步骤
5. IF 用户在 onboarding 中选择其他 provider（非 OAuth 类型），THEN THE OnboardingWizard SHALL 进入传统的 API Key 配置流程
