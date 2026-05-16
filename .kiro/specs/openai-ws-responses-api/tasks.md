# Implementation Plan: OpenAI Responses API WebSocket Transport

## Overview

Add WebSocket as an alternative streaming transport for the OpenAI Responses API in MaClaw. The WebSocket path connects to `wss://api.openai.com/v1/responses`, sends a `response.create` JSON frame, and reads response frames with the same event types as the HTTP+SSE path. Selected via `WireAPI = "responses-ws"` on the provider config. Non-streaming requests fall back to the HTTP Responses API. Uses `gorilla/websocket` (already in go.mod).

## Tasks

- [x] 1. Data model changes — IsResponsesWebSocket helper and IsResponsesAPI update
  - [x] 1.1 Add `IsResponsesWebSocket()` method on `MaclawLLMConfig` in `corelib/types.go`
    - Returns true when `WireAPI` equals `"responses-ws"` (case-insensitive, trimmed)
    - _Requirements: 1.4_

  - [x] 1.2 Update `IsResponsesAPI()` in `corelib/types.go` to match both `"responses"` and `"responses-ws"`
    - This ensures non-streaming fallback paths (doLLMRequest, TestMaclawLLM, vision probe) work for WebSocket providers
    - _Requirements: 12.1, 12.2, 12.3_

  - [ ]* 1.3 Write property test for WireAPI classification
    - **Property 1: WireAPI classification**
    - Generate random strings; verify `IsResponsesWebSocket()` returns true iff trimmed lowercase equals `"responses-ws"`, and `IsResponsesAPI()` returns true iff trimmed lowercase equals `"responses"` or `"responses-ws"`
    - **Validates: Requirements 1.1, 1.2, 1.3, 1.4, 11.1, 11.2, 12.1**

- [x] 2. WebSocket URL construction and frame builder
  - [x] 2.1 Create `gui/llm_stream_responses_ws.go` with `responsesWSEndpoint` function
    - Replace `https://` with `wss://`, `http://` with `ws://`, trim trailing slashes, append `/responses`
    - _Requirements: 2.1_

  - [x] 2.2 Implement `buildResponsesWSFrame` in `gui/llm_stream_responses_ws.go`
    - Build JSON frame with `type: "response.create"`, `model`, `input`, `store: false`, and optionally `instructions` and `tools`
    - Reuse `llm.ConvertToResponsesInput` and `llm.ConvertToResponsesTools` from `corelib/llm/responses_convert.go`
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 9.1, 10.1, 10.2, 10.3, 10.4_

  - [ ]* 2.3 Write property test for WebSocket URL construction
    - **Property 2: WebSocket URL construction**
    - Generate random base URL strings; verify `responsesWSEndpoint` produces correct `wss://` URL ending in `/responses`
    - **Validates: Requirements 2.1**

  - [ ]* 2.4 Write property test for frame construction
    - **Property 3: WebSocket request frame structure**
    - Generate random messages and tools; build frame and verify JSON structure matches expected format
    - **Validates: Requirements 3.1, 3.2, 3.3, 3.4, 9.1, 9.2, 10.1, 10.2, 10.3, 10.4**

- [x] 3. Checkpoint — Ensure URL and frame builder tests pass
  - Run tests, ask the user if questions arise.

- [x] 4. WebSocket streaming implementation
  - [x] 4.1 Implement `doResponsesWSLLMRequestStream` in `gui/llm_stream_responses_ws.go`
    - Construct WebSocket URL via `responsesWSEndpoint`
    - Dial using `gorilla/websocket.Dialer` with `HandshakeTimeout` from `cfg.EffectiveTimeoutSec()`, `Authorization: Bearer {key}` header
    - Send `response.create` frame via `buildResponsesWSFrame`
    - Read JSON frames in a loop, dispatch on `type` field
    - Handle: `response.output_item.added`, `response.output_text.delta`, `response.function_call_arguments.delta`, `response.function_call_arguments.done`, `response.output_item.done`, `response.completed`, `response.failed`, `response.incomplete`, `error`
    - Apply token filter chain (think filter → func call filter → tool call filter)
    - Enforce `guiMaxToolArgumentsBytes` (180KB) limit
    - Implement 90s idle timeout watchdog with `time.Timer`
    - Send periodic ping frames (30s interval) via background goroutine
    - Set pong handler to reset idle timer
    - Deferred connection close for cleanup
    - Populate `llmStreamMetrics` (FirstTokenAt, FirstSSEWaitNanos, IdleTimeoutCount)
    - Handle handshake errors via `classifyResponsesAPIHTTPError`
    - Handle unexpected close codes with descriptive error messages
    - Return partial content on connection drop (same behavior as HTTP+SSE path)
    - _Requirements: 2.2, 2.3, 2.4, 2.5, 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7, 5.1, 5.2, 5.3, 5.4, 6.1, 6.2, 6.3, 6.4, 7.1, 7.2, 7.3, 8.1, 8.2, 8.3, 8.4, 8.5, 8.6, 9.3, 13.2, 13.3, 13.4_

  - [ ]* 4.2 Write property test for WebSocket frame parsing
    - **Property 4: WebSocket frame parsing produces correct llmResponse**
    - Generate random frame sequences (text deltas, function call argument deltas, completed events); parse and verify assembled llmResponse
    - **Validates: Requirements 4.1, 4.2, 4.3, 4.4, 4.5, 9.3, 13.4**

  - [ ]* 4.3 Write property test for transport equivalence
    - **Property 5: Transport equivalence**
    - Generate random event sequences; format as both WebSocket JSON frames and SSE event:/data: lines; parse via both paths and verify identical llmResponse output
    - **Validates: Requirements 9.4**

  - [ ]* 4.4 Write property test for partial content recovery
    - **Property 6: Partial content recovery on connection drop**
    - Generate random frame sequences, truncate after at least one content frame; verify parser returns partial content
    - **Validates: Requirements 5.3, 7.2**

  - [ ]* 4.5 Write unit tests for handshake error classification
    - Test HTTP 401, 429, 403+insufficient_quota handshake responses
    - Verify error messages match Chinese-language patterns from `classifyResponsesAPIHTTPError`
    - _Requirements: 8.1, 8.2, 8.3, 8.6_

  - [ ]* 4.6 Write unit tests for in-stream error handling
    - Test `response.failed` frame, `error` frame (previous_response_not_found, websocket_connection_limit_reached), unexpected close codes
    - _Requirements: 4.6, 8.4, 8.5_

- [x] 5. Checkpoint — Ensure WebSocket streaming tests pass
  - Run tests, ask the user if questions arise.

- [x] 6. Routing integration
  - [x] 6.1 Add `IsResponsesWebSocket()` check in `gui/llm_stream.go` `doLLMRequestStream`
    - Insert before the existing `IsResponsesAPI()` check
    - Route to `doResponsesWSLLMRequestStream` when `IsResponsesWebSocket()` returns true
    - _Requirements: 1.5, 13.1_

  - [x] 6.2 Verify non-streaming fallback works in `gui/im_llm_client.go` `doLLMRequest`
    - No code changes needed — `IsResponsesAPI()` now matches `"responses-ws"`, so `doResponsesAPILLMRequest` is used for non-streaming calls
    - _Requirements: 12.1_

  - [x] 6.3 Verify `TestMaclawLLM` and vision probe work for WebSocket providers
    - No code changes needed — `IsResponsesAPI()` now matches `"responses-ws"`, so HTTP Responses API test/probe paths are used
    - _Requirements: 12.2, 12.3_

- [x] 7. Backward compatibility verification
  - [x] 7.1 Verify existing Chat Completions and Anthropic tests still pass
    - Run existing `gui/llm_stream_test.go` and `gui/app_maclaw_llm_test.go` tests
    - Confirm no regressions in non-WebSocket provider paths
    - _Requirements: 11.1, 11.2, 11.3, 11.4_

  - [ ]* 7.2 Write integration test for end-to-end WebSocket streaming
    - Mock WebSocket server returning realistic frame sequence (text + tool calls + usage)
    - Verify full pipeline from `doResponsesWSLLMRequestStream` through filters to `llmResponse`
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5, 9.3, 9.4_

- [x] 8. Final checkpoint — Ensure all tests pass
  - Run full test suite, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional property/unit tests that can be skipped for faster MVP
- The implementation uses `gorilla/websocket` (already in go.mod), no new dependencies needed
- Each streaming request opens a fresh WebSocket connection (no connection pooling in v1)
- The OpenAI WebSocket mode supports `previous_response_id` for incremental turns — this is a future optimization not included in this implementation
- Connection lifetime is limited to 60 minutes by OpenAI; per-request connections avoid this limit
- Property tests use Go's `testing/quick` or `pgregory.net/rapid` with minimum 100 iterations per property
- Tag format: `// Feature: openai-ws-responses-api, Property {N}: {title}`
