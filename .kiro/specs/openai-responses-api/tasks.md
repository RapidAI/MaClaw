# Implementation Plan: OpenAI Responses API Support

## Overview

Add OpenAI Responses API (`POST /v1/responses`) support to MaClaw so OAuth-authenticated users consume their ChatGPT subscription quota instead of API billing quota. Implementation follows the existing multi-protocol pattern: new wire format branch in request builder, message converter, streaming/non-streaming parsers, and routing — selected by a `WireAPI` field on config structs.

## Tasks

- [x] 1. Data model changes and WireAPI field
  - [x] 1.1 Add WireAPI field to MaclawLLMProvider and MaclawLLMConfig in `corelib/types.go`
    - Add `WireAPI string \`json:"wire_api,omitempty"\`` to both structs
    - Add `IsResponsesAPI()` helper method on `MaclawLLMConfig` that returns true when `WireAPI` equals `"responses"` (case-insensitive, trimmed)
    - _Requirements: 1.3, 1.4, 1.5, 6.3_

  - [ ]* 1.2 Write property test for WireAPI propagation from provider AuthType
    - **Property 8: WireAPI propagation from provider AuthType**
    - Generate random `MaclawLLMProvider` configs with varying `AuthType` and `WireAPI`; verify derived `MaclawLLMConfig.WireAPI` follows the oauth→responses rule
    - **Validates: Requirements 6.1, 6.2**

- [x] 2. Message and tool format conversion (`corelib/llm/responses_convert.go`)
  - [x] 2.1 Create `corelib/llm/responses_convert.go` with `ConvertToResponsesInput` and `ConvertToResponsesTools`
    - `ConvertToResponsesInput`: convert OpenAI Chat Completions messages to Responses API `input` array + `instructions` string, following the conversion table in design §3
    - `ConvertToResponsesTools`: flatten `{type:"function", function:{name,description,parameters}}` to `{type:"function", name, description, parameters}`
    - _Requirements: 2.1, 2.3, 5.1, 5.2, 10.1, 10.2, 10.3, 10.4_

  - [ ]* 2.2 Write property test for tool definition format conversion
    - **Property 4: Tool definition format conversion preserves semantics**
    - Generate random Chat Completions tool definitions; convert and verify `name`, `description`, `parameters` are identical
    - **Validates: Requirements 2.3, 5.1**

  - [ ]* 2.3 Write property test for message conversion
    - **Property 5: Message conversion preserves all roles and content**
    - Generate random multi-turn conversation sequences with system/user/assistant/tool roles; convert and verify each item maps correctly per design §3 table
    - **Validates: Requirements 5.2, 10.1, 10.2, 10.3, 10.4**

- [x] 3. Checkpoint — Ensure conversion tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 4. Request builder (`corelib/llm/responses.go`)
  - [x] 4.1 Create `corelib/llm/responses.go` with `BuildResponsesAPIRequestData` and `NewResponsesAPIRequest`
    - `BuildResponsesAPIRequestData`: construct endpoint URL as `TrimRight(baseURL, "/") + "/responses"`, build JSON body with `model`, `input`, `instructions`, `stream`, `tools` fields using `ConvertToResponsesInput` and `ConvertToResponsesTools`
    - `NewResponsesAPIRequest`: create `*http.Request` with `Authorization: Bearer {key}` header, Content-Type, and the built body
    - Define `ResponsesAPIRequestOptions` struct with `Stream`, `Tools`, `ExtraBody` fields
    - _Requirements: 2.1, 2.2, 2.4, 2.5, 2.6_

  - [ ]* 4.2 Write property test for request body structure
    - **Property 2: Responses API request body structure**
    - Generate random message arrays and model names; build request body and verify it contains `model`, `input`, `stream` keys, does NOT contain `messages` key, and `model` value matches config
    - **Validates: Requirements 2.1, 2.2, 2.4**

  - [ ]* 4.3 Write property test for endpoint URL construction
    - **Property 3: Responses API endpoint URL construction**
    - Generate random base URL strings (with/without trailing slashes); verify endpoint equals `TrimRight(baseURL, "/") + "/responses"`
    - **Validates: Requirements 2.5**

  - [ ]* 4.4 Write property test for WireAPI-based format selection
    - **Property 1: WireAPI-based format selection**
    - Generate random `MaclawLLMConfig` with varying `WireAPI`; build request data via both builders and verify the body contains `input` key for `"responses"` and `messages` key for empty/`"chat"`
    - **Validates: Requirements 1.4, 1.5, 9.1, 9.2**

- [x] 5. Non-streaming response parser (`corelib/llm/responses_parse.go`)
  - [x] 5.1 Create `corelib/llm/responses_parse.go` with `ParseNonStreamResponsesAPIResponse` and `ParseNonStreamResponsesAPIBody`
    - Parse `output` array: extract text from `type:"message"` items with `output_text` content parts, extract tool calls from `type:"function_call"` items
    - Map to internal `llm.Response` type: `Message.Content`, `ToolCalls[]`, `Usage`
    - _Requirements: 4.1, 4.2, 4.3, 5.3_

  - [ ]* 5.2 Write property test for non-streaming response parsing
    - **Property 6: Non-streaming response parsing extracts all content**
    - Generate random Responses API JSON response bodies with varying message/function_call output items and usage; parse and verify extraction matches source values
    - **Validates: Requirements 4.1, 4.2, 4.3, 5.3**

- [x] 6. Checkpoint — Ensure corelib/llm tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 7. Streaming SSE parser (`gui/llm_stream_responses.go`)
  - [x] 7.1 Create `gui/llm_stream_responses.go` with `doResponsesAPILLMRequestStream` method on `IMMessageHandler`
    - Build HTTP request via `NewResponsesAPIRequest` with `Stream: true`
    - Parse named SSE events (`event:` + `data:` pairs) from response body
    - Handle `response.output_item.added` (init per-item accumulator), `response.output_text.delta` (token callback), `response.function_call_arguments.delta` (accumulate args), `response.output_item.done` (build `llmToolCall`), `response.completed` (extract usage)
    - Handle `response.failed` (return error) and `response.incomplete` (return partial with finish reason `"length"`)
    - Implement 90s idle timeout watchdog (same `guiSSEIdleTimeout` pattern)
    - Apply `guiMaxToolArgumentsBytes` (180KB) limit on function call arguments
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6_

  - [x] 7.2 Implement `classifyResponsesAPIHTTPError` in `gui/llm_stream_responses.go`
    - HTTP 401 → "OAuth token 已过期，请重新登录 OpenAI 账号 (HTTP 401)"
    - HTTP 429 → "ChatGPT 订阅额度已达上限，请稍后再试 (HTTP 429)"
    - HTTP 403 + insufficient_quota → "ChatGPT 订阅状态异常，请检查订阅是否有效 (HTTP 403)"
    - Other → include endpoint URL and model in message
    - _Requirements: 8.1, 8.2, 8.3, 8.4_

  - [x] 7.3 Implement `doResponsesAPILLMRequest` (non-streaming path) in `gui/llm_stream_responses.go` or `gui/im_llm_client.go`
    - Build HTTP request via `NewResponsesAPIRequest` with `Stream: false`
    - Parse response via `ParseNonStreamResponsesAPIResponse`
    - Use `classifyResponsesAPIHTTPError` for error responses
    - _Requirements: 4.1, 4.2, 4.3_

  - [ ]* 7.4 Write property test for streaming SSE parse
    - **Property 7: Streaming SSE parse produces correct llmResponse**
    - Generate random SSE event sequences (text deltas, function call argument deltas, completed events); parse and verify concatenated deltas equal final content, argument deltas equal final arguments, usage preserved
    - **Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5**

  - [ ]* 7.5 Write unit tests for error classification
    - Test each HTTP status code (401, 429, 403+insufficient_quota, 500) with specific response bodies
    - Verify error messages match design §8 table
    - _Requirements: 8.1, 8.2, 8.3, 8.4_

- [x] 8. Checkpoint — Ensure streaming parser tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 9. Routing and config propagation
  - [x] 9.1 Add WireAPI routing in `gui/llm_stream.go` `doLLMRequestStream`
    - Add `cfg.IsResponsesAPI()` check before existing `Protocol == "anthropic"` check
    - Route to `doResponsesAPILLMRequestStream` when WireAPI is "responses"
    - _Requirements: 1.4, 9.1, 9.4_

  - [x] 9.2 Add WireAPI routing in `gui/im_llm_client.go` `doLLMRequest`
    - Add `cfg.IsResponsesAPI()` check before existing protocol check
    - Route to `doResponsesAPILLMRequest` when WireAPI is "responses"
    - _Requirements: 1.4, 9.1, 9.4_

  - [x] 9.3 Modify `GetMaclawLLMConfig` in `gui/app_maclaw_llm.go` to propagate WireAPI
    - Read `WireAPI` from selected provider
    - Auto-set to `"responses"` when `AuthType == "oauth"` and `WireAPI` is empty
    - Pass `WireAPI` into the returned `MaclawLLMConfig`
    - _Requirements: 6.1, 6.2_

  - [x] 9.4 Modify `TestMaclawLLM` in `gui/app_maclaw_llm.go` to route Responses API test
    - Add `cfg.IsResponsesAPI()` check to route to `testResponsesAPILLM`
    - Implement `testResponsesAPILLM`: send minimal Responses API request (`model` + single user message), parse response, report online/offline
    - Handle vision probe routing for OAuth providers
    - _Requirements: 7.1, 7.2, 7.3, 7.4_

  - [x] 9.5 Propagate WireAPI in Codex config write path
    - Ensure `WriteCodexConfig` caller in `gui/app_maclaw_llm.go` passes provider's `WireAPI` value
    - _Requirements: 6.4_

  - [ ]* 9.6 Write property test for Codex config wire_api propagation
    - **Property 9: Codex config wire_api propagation**
    - Generate random wire_api strings; call `WriteCodexConfig` and verify TOML output contains `wire_api = "{value}"` in provider section
    - **Validates: Requirements 6.4**

- [x] 10. Backward compatibility verification
  - [x] 10.1 Verify existing Chat Completions and Anthropic tests still pass
    - Run existing `gui/llm_stream_test.go` and `gui/app_maclaw_llm_test.go` tests
    - Confirm no regressions in non-OAuth provider paths
    - _Requirements: 9.1, 9.2, 9.3, 9.4_

  - [ ]* 10.2 Write integration test for end-to-end streaming
    - Mock HTTP server returning realistic Responses API SSE stream (text + tool calls + usage)
    - Verify full pipeline from `doLLMRequestStream` through to `llmResponse`
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5_

- [x] 11. Final checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirement acceptance criteria for traceability
- The implementation language is Go, matching the existing codebase
- Property tests use Go's `testing/quick` or `pgregory.net/rapid` with minimum 100 iterations per property
- Checkpoints ensure incremental validation at key integration points
- The design specifies 9 correctness properties; all are covered as property test sub-tasks
