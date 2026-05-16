# Requirements Document

## Introduction

Maclaw 执行 Skill 时，部分 Skill 声明了 `required_env: ["OPENAI_API_KEY"]`，需要 OpenAI 兼容的 API 端点。当前用户必须手动配置 OpenAI API Key，但 Maclaw 本身已有完整的 LLM 配置（`MaclawLLMConfig`），包含 URL、Key、Model 等信息。本功能在 Maclaw 内嵌一个轻量级本地 HTTP 代理，在 Skill 执行期间提供 OpenAI 兼容的 `/v1/chat/completions` 端点，自动将请求转发到当前配置的 LLM 服务商，并处理协议转换（Anthropic → OpenAI、Responses API → OpenAI）。代理按需启动，Skill 执行完毕后关闭，不常驻。

## Glossary

- **Proxy**: 本地 HTTP 代理服务器，监听 `localhost:{random_port}`，提供 OpenAI 兼容的 `/v1/chat/completions` 端点
- **Skill_Runner**: Maclaw 中负责执行 Skill 步骤的模块，包括 GUI 侧的 `SkillRunner` 和 TUI 侧的 `toolRunSkill`
- **MaclawLLMConfig**: 当前活跃的 LLM 服务商配置，包含 URL、Key、Model、Protocol、WireAPI 等字段
- **OpenAI_Protocol**: 标准 OpenAI Chat Completions API 协议（`POST /v1/chat/completions`），请求和响应格式遵循 OpenAI 规范
- **Anthropic_Protocol**: Anthropic Messages API 协议（`POST /v1/messages`），使用 `x-api-key` 认证和 Anthropic 特有的消息格式
- **Responses_API**: OpenAI Responses API 协议（`POST /v1/responses`），使用不同于 Chat Completions 的请求/响应结构
- **Upstream_Provider**: 当前 `MaclawLLMConfig` 指向的实际 LLM 服务商（如智谱、OpenAI、MiniMax 等）
- **Dummy_Key**: 固定的本地 API Key（`sk-maclaw-local-proxy`），用于 Skill 脚本连接本地 Proxy，无真实认证作用
- **Extra_Env**: Skill 执行时通过 `run_skill` 工具的 `env` 参数注入的环境变量

## Requirements

### Requirement 1: 本地代理生命周期管理

**User Story:** As a Skill 开发者, I want Maclaw 在执行需要 OpenAI API 的 Skill 时自动启动本地代理, so that Skill 脚本无需用户手动配置即可调用 LLM。

#### Acceptance Criteria

1. WHEN a Skill with `required_env` containing `OPENAI_API_KEY` is about to execute, THE Skill_Runner SHALL start the Proxy on `localhost` with a random available port before executing the first step
2. WHEN the Skill execution completes (success or failure), THE Skill_Runner SHALL stop the Proxy and release the port within 5 seconds
3. WHEN the Skill execution is cancelled via context cancellation, THE Skill_Runner SHALL stop the Proxy and release the port within 5 seconds
4. WHEN the Proxy fails to start (port binding failure or other error), THE Skill_Runner SHALL log the error and continue Skill execution without the Proxy, allowing the Skill to fail naturally if it requires the API
5. THE Proxy SHALL listen only on `127.0.0.1` (localhost), rejecting connections from non-loopback addresses
6. WHILE the Proxy is running, THE Proxy SHALL serve requests on the path `/v1/chat/completions` using HTTP POST method

### Requirement 2: 环境变量注入

**User Story:** As a Skill 开发者, I want Maclaw 自动注入 OpenAI 相关环境变量, so that Skill 脚本可以直接使用标准 OpenAI SDK 而无需额外配置。

#### Acceptance Criteria

1. WHEN the Proxy is started for a Skill execution, THE Skill_Runner SHALL inject `OPENAI_API_KEY` with the value `sk-maclaw-local-proxy` into the Skill step environment
2. WHEN the Proxy is started for a Skill execution, THE Skill_Runner SHALL inject `OPENAI_BASE_URL` with the value `http://127.0.0.1:{proxy_port}/v1` into the Skill step environment
3. WHEN the Proxy is started for a Skill execution, THE Skill_Runner SHALL inject `OPENAI_MODEL` with the Model value from the current MaclawLLMConfig into the Skill step environment
4. WHEN the user provides `OPENAI_API_KEY` via `run_skill` extra_env parameter, THE Skill_Runner SHALL use the user-provided value and SHALL NOT start the Proxy
5. WHEN the user provides `OPENAI_BASE_URL` via `run_skill` extra_env parameter, THE Skill_Runner SHALL use the user-provided value and SHALL NOT start the Proxy
6. WHEN the user provides both `OPENAI_API_KEY` and `OPENAI_BASE_URL` via extra_env, THE Skill_Runner SHALL use the user-provided values and SHALL NOT start the Proxy
7. THE Skill_Runner SHALL apply the same environment variable injection logic in both GUI (`SkillRunner`) and TUI (`toolRunSkill`) execution paths

### Requirement 3: OpenAI 协议直通转发

**User Story:** As a Skill 开发者, I want 当 LLM 服务商使用 OpenAI 协议时请求被直接转发, so that 标准 OpenAI 兼容服务商无需额外转换。

#### Acceptance Criteria

1. WHILE the current MaclawLLMConfig has Protocol `""` or `"openai"` and WireAPI `""` or `"chat"`, THE Proxy SHALL forward the incoming OpenAI Chat Completions request body to the Upstream_Provider's `/v1/chat/completions` endpoint without structural modification
2. WHEN forwarding to an OpenAI-protocol Upstream_Provider, THE Proxy SHALL replace the `model` field in the request body with the Model value from MaclawLLMConfig
3. WHEN forwarding to an OpenAI-protocol Upstream_Provider, THE Proxy SHALL set the `Authorization: Bearer {key}` header using the Key from MaclawLLMConfig
4. WHEN the Upstream_Provider returns a successful response, THE Proxy SHALL forward the response body back to the Skill script without structural modification
5. WHEN the Upstream_Provider returns an error response (4xx or 5xx), THE Proxy SHALL forward the HTTP status code and error body back to the Skill script

### Requirement 4: Anthropic 协议转换

**User Story:** As a Skill 开发者, I want 当 LLM 服务商使用 Anthropic 协议时请求被自动转换, so that Skill 脚本始终使用标准 OpenAI 格式而无需关心后端协议差异。

#### Acceptance Criteria

1. WHILE the current MaclawLLMConfig has Protocol `"anthropic"`, THE Proxy SHALL convert the incoming OpenAI Chat Completions request into an Anthropic Messages API request
2. WHEN converting OpenAI request to Anthropic format, THE Proxy SHALL extract `system` role messages and place them in the Anthropic `system` field
3. WHEN converting OpenAI request to Anthropic format, THE Proxy SHALL map `user` and `assistant` role messages to the Anthropic `messages` array
4. WHEN converting OpenAI request to Anthropic format, THE Proxy SHALL set the `model` field to the Model value from MaclawLLMConfig
5. WHEN converting OpenAI request to Anthropic format, THE Proxy SHALL set the `max_tokens` field to 4096 if not specified in the original request
6. WHEN sending to an Anthropic-protocol Upstream_Provider, THE Proxy SHALL set both `x-api-key` and `Authorization: Bearer {key}` headers using the Key from MaclawLLMConfig
7. WHEN sending to an Anthropic-protocol Upstream_Provider, THE Proxy SHALL set the `anthropic-version` header to `2023-06-01`
8. WHEN the Anthropic Upstream_Provider returns a successful response, THE Proxy SHALL convert the Anthropic response into OpenAI Chat Completions response format
9. WHEN converting Anthropic response to OpenAI format, THE Proxy SHALL map the Anthropic `content[].text` to OpenAI `choices[0].message.content`
10. WHEN converting Anthropic response to OpenAI format, THE Proxy SHALL map the Anthropic `stop_reason` to the OpenAI `finish_reason` field (`end_turn` → `stop`, `max_tokens` → `length`)
11. WHEN converting Anthropic response to OpenAI format, THE Proxy SHALL map the Anthropic `usage.input_tokens` and `usage.output_tokens` to the OpenAI `usage` object

### Requirement 5: Responses API 协议转换

**User Story:** As a Skill 开发者, I want 当 LLM 服务商使用 Responses API 时请求被自动转换, so that Skill 脚本始终使用标准 OpenAI Chat Completions 格式。

#### Acceptance Criteria

1. WHILE the current MaclawLLMConfig has WireAPI `"responses"` or `"responses-ws"`, THE Proxy SHALL convert the incoming OpenAI Chat Completions request into a Responses API request
2. WHEN converting OpenAI Chat Completions request to Responses API format, THE Proxy SHALL map the `messages` array to the Responses API `input` field
3. WHEN converting OpenAI Chat Completions request to Responses API format, THE Proxy SHALL set the `model` field to the Model value from MaclawLLMConfig
4. WHEN sending to a Responses API Upstream_Provider, THE Proxy SHALL use the URL from MaclawLLMConfig with the `/v1/responses` endpoint path
5. WHEN sending to a Responses API Upstream_Provider, THE Proxy SHALL set the `Authorization: Bearer {key}` header using the Key from MaclawLLMConfig
6. WHEN the Responses API Upstream_Provider returns a successful response, THE Proxy SHALL convert the Responses API response into OpenAI Chat Completions response format
7. WHEN converting Responses API response to OpenAI format, THE Proxy SHALL extract text content from the `output` array items of type `message` and map to `choices[0].message.content`
8. WHEN converting Responses API response to OpenAI format, THE Proxy SHALL map the `usage.input_tokens` and `usage.output_tokens` to the OpenAI `usage` object

### Requirement 6: 请求处理与错误处理

**User Story:** As a Skill 开发者, I want 代理提供清晰的错误信息, so that 调试 Skill 时能快速定位问题。

#### Acceptance Criteria

1. WHEN the Proxy receives a request on a path other than `/v1/chat/completions`, THE Proxy SHALL return HTTP 404 with a JSON error body `{"error": {"message": "Not Found", "type": "invalid_request_error"}}`
2. WHEN the Proxy receives a non-POST request on `/v1/chat/completions`, THE Proxy SHALL return HTTP 405 with a JSON error body `{"error": {"message": "Method Not Allowed", "type": "invalid_request_error"}}`
3. WHEN the Proxy receives a request with an invalid or unparseable JSON body, THE Proxy SHALL return HTTP 400 with a JSON error body describing the parse error
4. WHEN the Upstream_Provider is unreachable (connection refused, DNS failure, timeout), THE Proxy SHALL return HTTP 502 with a JSON error body `{"error": {"message": "upstream provider unreachable: {detail}", "type": "server_error"}}`
5. WHEN the Upstream_Provider request times out, THE Proxy SHALL use a timeout of 120 seconds for the upstream HTTP request
6. THE Proxy SHALL set `stream: false` in all forwarded requests to the Upstream_Provider, regardless of the incoming request's `stream` field
7. THE Proxy SHALL log each proxied request with the upstream protocol type, upstream URL, model name, and response status code at debug level

### Requirement 7: 非 OpenAI API Skill 不受影响

**User Story:** As a Skill 开发者, I want Skill 不声明 OPENAI_API_KEY 时代理不启动, so that 不需要 OpenAI API 的 Skill 执行不受任何影响。

#### Acceptance Criteria

1. WHEN a Skill does not include `OPENAI_API_KEY` in its `required_env` field, THE Skill_Runner SHALL NOT start the Proxy
2. WHEN a Skill does not include `OPENAI_API_KEY` in its `required_env` field, THE Skill_Runner SHALL NOT inject `OPENAI_API_KEY`, `OPENAI_BASE_URL`, or `OPENAI_MODEL` environment variables
3. THE Proxy startup and shutdown process SHALL NOT add more than 200ms of latency to the Skill execution start time

### Requirement 8: 协议转换往返一致性

**User Story:** As a 开发者, I want 协议转换保持语义一致, so that Skill 脚本收到的响应与直接调用 OpenAI API 的格式一致。

#### Acceptance Criteria

1. FOR ALL valid OpenAI Chat Completions requests containing `messages` with `system`, `user`, and `assistant` roles, converting to Anthropic format and then converting the Anthropic response back to OpenAI format SHALL produce a response with the same structural fields as a native OpenAI response (`id`, `object`, `model`, `choices`, `usage`)
2. FOR ALL valid OpenAI Chat Completions requests, converting to Responses API format and then converting the Responses API response back to OpenAI format SHALL produce a response with the same structural fields as a native OpenAI response (`id`, `object`, `model`, `choices`, `usage`)
3. WHEN the Anthropic response contains multiple `content` blocks of type `text`, THE Proxy SHALL concatenate them into a single `content` string in the OpenAI response
4. WHEN the Responses API response contains multiple `message` output items, THE Proxy SHALL concatenate their text content into a single `content` string in the OpenAI response
