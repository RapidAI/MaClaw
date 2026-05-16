# Requirements Document

## Introduction

MaClaw currently supports the OpenAI Responses API (`POST /v1/responses`) over HTTP with Server-Sent Events (SSE) for streaming. This feature adds WebSocket as an alternative streaming transport for the Responses API, connecting to the OpenAI Realtime/WebSocket endpoint (e.g. `wss://api.openai.com/v1/realtime`). WebSocket transport provides lower latency (no HTTP request/response overhead per turn), bidirectional communication (server can push events while client sends), and persistent connections that can be reused across multiple conversation turns.

The existing HTTP+SSE Responses API implementation is fully functional and will remain the default. WebSocket transport is an opt-in alternative selected via a new transport option on the provider configuration. The WebSocket path reuses the existing message conversion (`ConvertToResponsesInput`, `ConvertToResponsesTools` in `corelib/llm/responses_convert.go`) and internal `llmResponse`/`llmToolCall` types — only the transport and frame parsing layers differ.

## Glossary

- **MaClaw**: The desktop AI agent application (GUI mode)
- **Responses_API**: OpenAI's Responses API, supporting both HTTP+SSE and WebSocket transports
- **HTTP_SSE_Transport**: The existing HTTP POST + Server-Sent Events streaming transport for the Responses API
- **WebSocket_Transport**: The new WebSocket-based streaming transport for the Responses API, using persistent bidirectional connections
- **WireAPI**: A configuration field indicating which API wire format to use (`"chat"`, `"responses"`, or `"responses-ws"`)
- **WebSocket_Connection**: A persistent, full-duplex TCP connection using the WebSocket protocol (RFC 6455)
- **WebSocket_Frame**: A single JSON message sent or received over the WebSocket connection, equivalent to an SSE event in the HTTP transport
- **Connection_Lifecycle**: The sequence of connect → authenticate → exchange messages → close for a WebSocket session
- **Heartbeat**: Periodic ping/pong frames exchanged to detect dead connections and keep the connection alive through proxies
- **Reconnection**: The process of re-establishing a dropped WebSocket connection with exponential backoff
- **MaclawLLMConfig**: The runtime LLM configuration struct passed through the call chain
- **MaclawLLMProvider**: The persisted provider configuration struct including auth_type, URL, key, model, and wire_api
- **LLM_Request_Builder**: The `corelib/llm` package functions that construct requests for LLM calls
- **Token_Callback**: The function called with each text delta during streaming (`TokenCallback`)
- **Idle_Timeout**: The maximum duration (90s) without receiving data before the connection is considered stalled

## Requirements

### Requirement 1: WebSocket Transport Selection via WireAPI

**User Story:** As a user, I want to opt into WebSocket-based streaming for the Responses API, so that I can benefit from lower latency and persistent connections.

#### Acceptance Criteria

1. WHEN a MaclawLLMProvider has WireAPI equal to "responses-ws", THE MaClaw SHALL use the WebSocket_Transport for Responses API streaming requests
2. WHEN a MaclawLLMProvider has WireAPI equal to "responses", THE MaClaw SHALL continue using the HTTP_SSE_Transport for Responses API streaming requests
3. WHEN a MaclawLLMProvider has WireAPI empty or equal to "chat", THE MaClaw SHALL continue using the Chat Completions API with HTTP+SSE
4. THE MaclawLLMConfig SHALL include an `IsResponsesWebSocket()` helper method that returns true when WireAPI equals "responses-ws"
5. THE `doLLMRequestStream` dispatch function SHALL route to the WebSocket streaming path when `IsResponsesWebSocket()` returns true

### Requirement 2: WebSocket Connection Establishment

**User Story:** As a user, I want MaClaw to establish a WebSocket connection to the OpenAI Realtime endpoint, so that streaming responses are delivered over a persistent connection.

#### Acceptance Criteria

1. WHEN initiating a WebSocket Responses API request, THE MaClaw SHALL construct the WebSocket URL by replacing the `https://` scheme with `wss://` in the base URL and appending the appropriate realtime path
2. THE MaClaw SHALL include the `Authorization: Bearer {key}` credential in the WebSocket handshake request headers
3. THE MaClaw SHALL include the model name in the WebSocket connection parameters (either as a query parameter or in the initial configuration message, per the OpenAI WebSocket protocol)
4. WHEN the WebSocket handshake fails, THE MaClaw SHALL return a descriptive error including the endpoint URL and HTTP status code from the handshake response
5. THE MaClaw SHALL set a connection timeout for the WebSocket handshake that matches the configured `EffectiveTimeoutSec` value

### Requirement 3: WebSocket Message Framing and Sending

**User Story:** As a developer, I want the WebSocket transport to send Responses API requests as JSON frames, so that the server receives properly formatted input.

#### Acceptance Criteria

1. WHEN sending a conversation turn over WebSocket, THE MaClaw SHALL serialize the request as a JSON message frame containing `type`, `input`, `model`, and optionally `instructions` and `tools` fields
2. THE MaClaw SHALL reuse the existing `ConvertToResponsesInput` function from `corelib/llm/responses_convert.go` to convert messages into the Responses API input format
3. THE MaClaw SHALL reuse the existing `ConvertToResponsesTools` function from `corelib/llm/responses_convert.go` to convert tool definitions into the Responses API tool format
4. WHEN tools are provided, THE MaClaw SHALL include the converted tool definitions in the WebSocket request frame
5. THE MaClaw SHALL send the request frame as a single WebSocket text message (not fragmented across multiple frames)

### Requirement 4: WebSocket Response Frame Parsing

**User Story:** As a user, I want streaming responses received over WebSocket to be parsed correctly, so that I see real-time token output identical to the HTTP+SSE path.

#### Acceptance Criteria

1. WHEN a WebSocket text frame is received, THE MaClaw SHALL parse it as a JSON object and dispatch based on the `type` field
2. WHEN a frame with type corresponding to a text delta event is received, THE MaClaw SHALL extract the text delta and pass it to the Token_Callback
3. WHEN a frame with type corresponding to a function call arguments delta is received, THE MaClaw SHALL accumulate the argument fragments into a per-item buffer
4. WHEN a frame with type corresponding to an output item completion is received for a `function_call` type item, THE MaClaw SHALL construct a complete `llmToolCall` from the accumulated call_id, name, and arguments
5. WHEN a frame with type corresponding to a response completion event is received, THE MaClaw SHALL extract usage statistics from the response data
6. WHEN a frame with type corresponding to a response failure event is received, THE MaClaw SHALL extract the error message and return a non-retryable error
7. THE MaClaw SHALL enforce the same `guiMaxToolArgumentsBytes` (180KB) limit on accumulated function call arguments as the HTTP+SSE path

### Requirement 5: WebSocket Connection Lifecycle Management

**User Story:** As a user, I want WebSocket connections to be properly managed throughout their lifecycle, so that resources are cleaned up and connections do not leak.

#### Acceptance Criteria

1. WHEN a streaming request completes (response.completed or response.failed received), THE MaClaw SHALL send a close frame and close the WebSocket connection
2. WHEN the request context is cancelled, THE MaClaw SHALL send a close frame and close the WebSocket connection within 5 seconds
3. IF the WebSocket connection is closed unexpectedly by the server before a completion event, THEN THE MaClaw SHALL return any partial content accumulated so far (same behavior as the HTTP+SSE path for interrupted streams)
4. THE MaClaw SHALL close the WebSocket connection in a deferred cleanup block to prevent connection leaks on any code path (success, error, or panic)

### Requirement 6: WebSocket Heartbeat and Idle Detection

**User Story:** As a user, I want MaClaw to detect stalled WebSocket connections, so that I am not left waiting indefinitely when the server stops responding.

#### Acceptance Criteria

1. THE MaClaw SHALL send WebSocket ping frames at a regular interval to keep the connection alive and detect dead connections
2. WHEN the server responds to a ping with a pong, THE MaClaw SHALL reset the idle timer
3. IF no data frame or pong frame is received within the Idle_Timeout period (90 seconds), THEN THE MaClaw SHALL close the connection and return a retryable error
4. THE MaClaw SHALL use the same idle timeout duration (`guiSSEIdleTimeout` = 90 seconds) as the HTTP+SSE streaming paths for consistency

### Requirement 7: WebSocket Reconnection on Transient Failures

**User Story:** As a user, I want MaClaw to handle transient WebSocket disconnections gracefully, so that temporary network issues do not cause permanent failures.

#### Acceptance Criteria

1. IF the WebSocket connection fails during the handshake with a retryable error (network timeout, DNS resolution failure, or HTTP 502/503/504), THEN THE MaClaw SHALL return a retryable error so the agent loop can re-attempt the request
2. IF the WebSocket connection drops after partial content has been received, THEN THE MaClaw SHALL return the partial content rather than retrying (same behavior as the HTTP+SSE path)
3. THE MaClaw SHALL NOT automatically reconnect and resume a partially completed response within the WebSocket streaming function itself (reconnection is handled by the agent loop's existing retry logic)

### Requirement 8: WebSocket Error Handling

**User Story:** As a user, I want clear error messages when WebSocket Responses API calls fail, so that I can understand and resolve connection issues.

#### Acceptance Criteria

1. IF the WebSocket handshake returns HTTP 401, THEN THE MaClaw SHALL display a message indicating the OAuth token has expired and suggest re-authenticating
2. IF the WebSocket handshake returns HTTP 429, THEN THE MaClaw SHALL display a message indicating the rate limit has been reached
3. IF the WebSocket handshake returns HTTP 403 with an `insufficient_quota` indicator, THEN THE MaClaw SHALL display a message suggesting the user verify their subscription status
4. IF a `response.failed` frame is received over WebSocket, THEN THE MaClaw SHALL extract the error details and display them to the user
5. IF the WebSocket connection is rejected with an unexpected close code, THEN THE MaClaw SHALL include the close code and reason in the error message for debugging
6. THE MaClaw SHALL reuse the same error message patterns (Chinese language) as the existing HTTP+SSE Responses API error handler for consistency

### Requirement 9: Tool Calling Over WebSocket Transport

**User Story:** As a user, I want tool calling to work seamlessly over the WebSocket transport, so that MaClaw's agent capabilities function correctly with the WebSocket path.

#### Acceptance Criteria

1. WHEN sending tool definitions over the WebSocket_Transport, THE MaClaw SHALL include them in the request frame using the same Responses API tool format as the HTTP+SSE path (flat `{type: "function", name, description, parameters}`)
2. WHEN a tool call result needs to be sent back over the WebSocket_Transport, THE MaClaw SHALL format it as a `function_call_output` input item with `call_id` and `output` fields, consistent with the HTTP+SSE path
3. WHEN the WebSocket response contains function_call output items, THE MaClaw SHALL map them to the internal `llmToolCall` structure with `ID` set to `call_id`, `Function.Name` set to `name`, and `Function.Arguments` set to `arguments`
4. THE MaClaw SHALL produce an identical `llmResponse` structure from WebSocket frames as from HTTP+SSE events for the same logical response, ensuring the agent loop processes both transports uniformly

### Requirement 10: Multi-Turn Conversation Over WebSocket

**User Story:** As a user, I want multi-turn conversations to work correctly over the WebSocket transport, so that the agent maintains context across tool calls and follow-up messages.

#### Acceptance Criteria

1. WHEN sending a multi-turn conversation over the WebSocket_Transport, THE MaClaw SHALL convert the full message history into the Responses API `input` format using the existing `ConvertToResponsesInput` function
2. WHEN a system message is present, THE MaClaw SHALL include it as the `instructions` field in the WebSocket request frame
3. WHEN tool call results are present in the conversation history, THE MaClaw SHALL include them as `function_call_output` input items in the WebSocket request frame
4. WHEN assistant messages with tool calls are present in the conversation history, THE MaClaw SHALL include them as `function_call` input items in the WebSocket request frame

### Requirement 11: Backward Compatibility with Existing Transports

**User Story:** As an existing user, I want my current HTTP+SSE setup to continue working without any changes, so that the WebSocket transport addition does not disrupt my workflow.

#### Acceptance Criteria

1. THE MaClaw SHALL continue using the HTTP_SSE_Transport for all providers where WireAPI is "responses" (not "responses-ws")
2. THE MaClaw SHALL continue using the Chat Completions API for all providers where WireAPI is empty or "chat"
3. THE MaClaw SHALL not modify the behavior of existing `doResponsesAPILLMRequestStream`, `doOpenAILLMRequestStream`, or `doAnthropicLLMRequestStream` functions
4. WHEN upgrading from a previous version, THE MaClaw SHALL preserve existing provider configurations without requiring manual migration

### Requirement 12: Non-Streaming Fallback for WebSocket Providers

**User Story:** As a developer, I want non-streaming requests (connection tests, vision probes) to work for WebSocket-configured providers, so that diagnostic functions operate correctly.

#### Acceptance Criteria

1. WHEN a non-streaming LLM request is made with WireAPI equal to "responses-ws", THE MaClaw SHALL fall back to using the HTTP+SSE Responses API path for the non-streaming request (WebSocket is streaming-only)
2. WHEN testing an LLM connection for a WebSocket-configured provider, THE MaClaw SHALL use the HTTP Responses API format for the test request
3. WHEN probing for vision support on a WebSocket-configured provider, THE MaClaw SHALL use the HTTP Responses API format for the probe request

### Requirement 13: Streaming Dispatch Integration

**User Story:** As a developer, I want the WebSocket streaming path to integrate cleanly with the existing `doLLMRequestStream` dispatch, so that the routing logic remains maintainable.

#### Acceptance Criteria

1. THE `doLLMRequestStream` function SHALL check `IsResponsesWebSocket()` before the existing `IsResponsesAPI()` check, routing to the WebSocket streaming function when true
2. THE WebSocket streaming function SHALL accept the same parameters as the existing streaming functions (`reqCtx`, `cfg`, `messages`, `tools`, `httpClient`, `onToken`, `metrics`)
3. THE WebSocket streaming function SHALL populate the same `llmStreamMetrics` fields (FirstTokenAt, FirstSSEWaitNanos, IdleTimeoutCount) as the HTTP+SSE path for observability parity
4. THE WebSocket streaming function SHALL apply the same token filtering pipeline (think filter, func call filter, tool call filter) as the HTTP+SSE path

