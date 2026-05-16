# Requirements Document

## Introduction

MaClaw currently uses the OpenAI Chat Completions API (`POST /v1/chat/completions`) for all LLM calls. When users authenticate via OAuth with their ChatGPT Plus/Pro subscription, the token exchange produces an API key tied to the API billing account, not the ChatGPT subscription. This causes `insufficient_quota` errors because users haven't funded their API account separately.

The OpenAI Responses API (`POST /v1/responses`) uses ChatGPT subscription quota instead of API billing quota. This feature adds Responses API support so OAuth-authenticated users can use their ChatGPT subscription quota, while non-OAuth users continue using Chat Completions API unchanged.

## Glossary

- **MaClaw**: The desktop AI agent application (GUI mode)
- **Chat_Completions_API**: OpenAI's `POST /v1/chat/completions` endpoint, requires API billing quota
- **Responses_API**: OpenAI's `POST /v1/responses` endpoint, uses ChatGPT subscription quota
- **OAuth_Provider**: A MaclawLLMProvider with `AuthType == "oauth"`, authenticated via OpenAI OAuth PKCE flow
- **API_Key_Provider**: A MaclawLLMProvider with `AuthType != "oauth"`, authenticated via manually entered API key
- **Wire_API**: A configuration field indicating which API wire format to use (`"chat"` or `"responses"`)
- **SSE**: Server-Sent Events, the streaming protocol used by both APIs but with different event type schemas
- **LLM_Request_Builder**: The `corelib/llm` package functions that construct HTTP requests for LLM calls
- **Tool_Definition**: The JSON schema describing a callable tool, formatted differently between Chat Completions and Responses APIs
- **MaclawLLMConfig**: The runtime LLM configuration struct passed through the call chain
- **MaclawLLMProvider**: The persisted provider configuration struct including auth_type, URL, key, and model

## Requirements

### Requirement 1: Automatic API Selection Based on Auth Type

**User Story:** As a ChatGPT Plus/Pro subscriber, I want MaClaw to automatically use the Responses API when I authenticate via OAuth, so that my LLM calls use my ChatGPT subscription quota instead of requiring separate API billing.

#### Acceptance Criteria

1. WHEN a MaclawLLMProvider has AuthType equal to "oauth", THE LLM_Request_Builder SHALL construct requests targeting the Responses_API endpoint (`/v1/responses`)
2. WHEN a MaclawLLMProvider has AuthType not equal to "oauth", THE LLM_Request_Builder SHALL construct requests targeting the Chat_Completions_API endpoint (`/v1/chat/completions`)
3. THE MaclawLLMConfig SHALL include a WireAPI field that indicates which API format to use
4. WHEN the WireAPI field is "responses", THE LLM_Request_Builder SHALL use the Responses_API request format
5. WHEN the WireAPI field is empty or "chat", THE LLM_Request_Builder SHALL use the Chat_Completions_API request format

### Requirement 2: Responses API Request Construction

**User Story:** As a developer, I want the LLM request builder to correctly construct Responses API requests, so that OAuth-authenticated calls succeed against the `/v1/responses` endpoint.

#### Acceptance Criteria

1. THE LLM_Request_Builder SHALL construct Responses_API request bodies with an `input` field containing the conversation messages instead of a `messages` field
2. THE LLM_Request_Builder SHALL include the `model` field in Responses_API request bodies
3. WHEN tools are provided, THE LLM_Request_Builder SHALL format tool definitions using the Responses_API tool schema (with `type: "function"` wrapper containing `name`, `description`, `parameters`)
4. WHEN streaming is requested, THE LLM_Request_Builder SHALL set `stream: true` in the Responses_API request body
5. THE LLM_Request_Builder SHALL send Responses_API requests to the endpoint constructed as `{base_url}/responses` (not `/v1/responses` appended, since base_url already contains `/v1`)
6. THE LLM_Request_Builder SHALL set the `Authorization: Bearer {key}` header on Responses_API requests

### Requirement 3: Responses API Streaming Response Parsing

**User Story:** As a user, I want streaming responses from the Responses API to be parsed correctly, so that I see real-time token output during LLM generation.

#### Acceptance Criteria

1. WHEN a streaming Responses_API response is received, THE MaClaw SHALL parse SSE events with `response.output_item.added`, `response.content_part.added`, `response.output_text.delta`, and `response.completed` event types
2. WHEN a `response.output_text.delta` event is received, THE MaClaw SHALL extract the text delta from the `delta` field and pass it to the token callback
3. WHEN a `response.function_call_arguments.delta` event is received, THE MaClaw SHALL accumulate the function call argument fragments
4. WHEN a `response.output_item.done` event is received with a `function_call` type item, THE MaClaw SHALL construct a complete tool call from the accumulated function name, call_id, and arguments
5. WHEN the `response.completed` event is received, THE MaClaw SHALL extract usage statistics from the `response.usage` field
6. IF the SSE stream produces no data within the idle timeout period, THEN THE MaClaw SHALL close the connection and return a retryable error

### Requirement 4: Responses API Non-Streaming Response Parsing

**User Story:** As a developer, I want non-streaming Responses API responses to be parsed correctly, so that test/probe calls and fallback paths work with the Responses API.

#### Acceptance Criteria

1. WHEN a non-streaming Responses_API response is received, THE MaClaw SHALL extract text content from the `output` array items with `type: "message"` containing `content` parts of `type: "output_text"`
2. WHEN a non-streaming Responses_API response contains tool calls, THE MaClaw SHALL extract them from `output` array items with `type: "function_call"` containing `name`, `call_id`, and `arguments`
3. WHEN a non-streaming Responses_API response contains a `usage` field, THE MaClaw SHALL extract `input_tokens`, `output_tokens`, and `total_tokens`

### Requirement 5: Tool Call Format Conversion

**User Story:** As a user, I want tool calling to work seamlessly with the Responses API, so that MaClaw's agent capabilities function correctly when using my ChatGPT subscription.

#### Acceptance Criteria

1. WHEN sending tool definitions via the Responses_API, THE LLM_Request_Builder SHALL convert each tool from the Chat Completions format (`{type: "function", function: {name, description, parameters}}`) to the Responses API format (`{type: "function", name, description, parameters}`)
2. WHEN a tool call result needs to be sent back via the Responses_API, THE LLM_Request_Builder SHALL format it as an input item with `type: "function_call_output"`, `call_id`, and `output` fields
3. WHEN the Responses_API returns a function_call output item, THE MaClaw SHALL map it to the internal `llmToolCall` structure with `ID` set to `call_id`, `Function.Name` set to `name`, and `Function.Arguments` set to `arguments`

### Requirement 6: Provider Configuration and WireAPI Propagation

**User Story:** As a user, I want the correct API format to be automatically determined from my provider configuration, so that I don't need to manually configure wire_api settings.

#### Acceptance Criteria

1. WHEN the OpenAI OAuth provider is selected, THE MaClaw SHALL set WireAPI to "responses" in the MaclawLLMConfig passed to the LLM call chain
2. WHEN a non-OAuth provider is selected, THE MaClaw SHALL set WireAPI to empty string (defaulting to Chat Completions) in the MaclawLLMConfig
3. THE MaclawLLMProvider struct SHALL include a WireAPI field that is persisted in the configuration
4. WHEN writing Codex CLI config via WriteCodexConfig, THE MaClaw SHALL propagate the wire_api value to the `[model_providers.xxx]` section

### Requirement 7: LLM Connection Test with Responses API

**User Story:** As a user, I want the LLM connection test to work correctly when using the Responses API, so that I can verify my OAuth setup is working.

#### Acceptance Criteria

1. WHEN testing an OAuth provider's LLM connection, THE MaClaw SHALL send the test request using the Responses_API format
2. WHEN the Responses_API test request succeeds, THE MaClaw SHALL report the connection as online
3. IF the Responses_API test request fails with an authentication error, THEN THE MaClaw SHALL report a clear error message indicating the OAuth token may need refreshing
4. WHEN probing for vision support on an OAuth provider, THE MaClaw SHALL use the Responses_API format for the probe request

### Requirement 8: Error Handling for Responses API

**User Story:** As a user, I want clear error messages when Responses API calls fail, so that I can understand and resolve issues with my ChatGPT subscription usage.

#### Acceptance Criteria

1. IF the Responses_API returns HTTP 401, THEN THE MaClaw SHALL display a message indicating the OAuth token has expired and suggest re-authenticating
2. IF the Responses_API returns HTTP 429, THEN THE MaClaw SHALL display a message indicating the ChatGPT subscription rate limit has been reached
3. IF the Responses_API returns HTTP 403 with an `insufficient_quota` error, THEN THE MaClaw SHALL display a message suggesting the user verify their ChatGPT subscription status
4. IF the Responses_API returns an unrecognized error, THEN THE MaClaw SHALL include the endpoint URL and model name in the error message for debugging

### Requirement 9: Backward Compatibility

**User Story:** As an existing API key user, I want my current setup to continue working without any changes, so that the Responses API addition does not disrupt my workflow.

#### Acceptance Criteria

1. THE MaClaw SHALL continue using the Chat_Completions_API for all providers where AuthType is not "oauth"
2. THE MaClaw SHALL not modify the request format, endpoint URL, or tool definition format for non-OAuth providers
3. WHEN upgrading from a previous version, THE MaClaw SHALL preserve existing provider configurations without requiring manual migration
4. THE MaClaw SHALL support both Chat Completions and Responses API formats concurrently for different providers in the same session

### Requirement 10: Conversation State Management for Responses API

**User Story:** As a user, I want multi-turn conversations to work correctly with the Responses API, so that the agent maintains context across tool calls and follow-up messages.

#### Acceptance Criteria

1. WHEN sending a multi-turn conversation via the Responses_API, THE LLM_Request_Builder SHALL convert the messages array into the Responses API `input` format, mapping `role: "user"` messages to input items with `type: "message"` and `role: "user"`, and `role: "assistant"` messages to items with `type: "message"` and `role: "assistant"`
2. WHEN a system message is present, THE LLM_Request_Builder SHALL include it as the `instructions` field in the Responses_API request body (not as an input item)
3. WHEN tool call results are present in the conversation history, THE LLM_Request_Builder SHALL convert them to `type: "function_call_output"` input items with the corresponding `call_id` and `output`
4. WHEN assistant messages with tool calls are present in the conversation history, THE LLM_Request_Builder SHALL convert them to `type: "function_call"` input items with `name`, `call_id`, and `arguments`
