# Implementation Plan: Skill OpenAI Proxy

## Overview

Implement a lightweight local HTTP proxy server in `corelib/openai_proxy.go` that provides an OpenAI-compatible `/v1/chat/completions` endpoint during Skill execution. The proxy forwards requests to the currently configured LLM provider with automatic protocol conversion (OpenAI direct, Anthropic → OpenAI, Responses API → OpenAI). Integrate the proxy lifecycle into both GUI `SkillRunner` and TUI `toolRunSkill` paths.

## Tasks

- [x] 1. Implement core proxy server and helper function
  - [x] 1.1 Create `corelib/openai_proxy.go` with `OpenAIProxyConfig`, `OpenAIProxy` struct, `NewOpenAIProxy`, `Start`, `Stop`, `Port` methods
    - `OpenAIProxyConfig` holds URL, Key, Model, Protocol, WireAPI fields
    - `OpenAIProxy` manages `http.Server`, `net.Listener`, port, and `http.Client` (120s timeout)
    - `Start()` binds to `127.0.0.1:0` (random port), registers HTTP handler via `http.ServeMux`, starts `server.Serve` in goroutine, returns allocated port
    - `Stop()` calls `server.Shutdown` with 5-second context deadline
    - _Requirements: 1.1, 1.2, 1.3, 1.5, 1.6_

  - [x] 1.2 Implement `NeedsOpenAIProxy` helper function in `corelib/openai_proxy.go`
    - Returns `true` if `requiredEnv` contains `OPENAI_API_KEY` AND `extraEnv` does not contain `OPENAI_API_KEY` or `OPENAI_BASE_URL`
    - _Requirements: 2.4, 2.5, 2.6, 7.1, 7.2_

  - [x] 1.3 Implement `routeProtocol` method and `handleChatCompletions` HTTP handler
    - `routeProtocol()` returns `"anthropic"`, `"responses"`, or `"openai"` based on config Protocol/WireAPI
    - `handleChatCompletions` validates: path must be `/v1/chat/completions` (else 404), method must be POST (else 405), body must be valid JSON (else 400)
    - Routes to `forwardOpenAI`, `forwardAnthropic`, or `forwardResponses` based on `routeProtocol()`
    - Returns OpenAI-format JSON error bodies for all error cases
    - _Requirements: 6.1, 6.2, 6.3, 6.7_

- [x] 2. Implement protocol conversion functions
  - [x] 2.1 Implement OpenAI direct forward path (`forwardOpenAI`)
    - Replace `model` field with config Model value
    - Force `stream: false`
    - Set `Authorization: Bearer {key}` header
    - Construct upstream URL as `{config.URL}/v1/chat/completions`
    - Forward upstream response body and status code as-is
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 6.6_

  - [x] 2.2 Implement Anthropic conversion functions (`openaiToAnthropic`, `anthropicToOpenAI`, `forwardAnthropic`)
    - `openaiToAnthropic`: extract system messages into `system` field, map user/assistant to `messages`, set model, set `max_tokens` to 4096 if absent, force `stream: false`
    - `forwardAnthropic`: set `x-api-key`, `Authorization: Bearer`, `anthropic-version: 2023-06-01` headers; use `AnthropicMessagesEndpoint` for URL construction
    - `anthropicToOpenAI`: map `content[].text` to `choices[0].message.content` (concatenate multiple blocks), map `stop_reason` to `finish_reason` (`end_turn`→`stop`, `max_tokens`→`length`), map usage fields, set `object: "chat.completion"`
    - _Requirements: 4.1–4.11, 8.1, 8.3_

  - [x] 2.3 Implement Responses API conversion functions (`openaiToResponses`, `responsesToOpenAI`, `forwardResponses`)
    - `openaiToResponses`: map `messages` to `input` field, set model, force `stream: false`
    - `forwardResponses`: set `Authorization: Bearer` header; construct URL as `{config.URL}/v1/responses`
    - `responsesToOpenAI`: extract text from `output[].content[].text` where type is `output_text` (concatenate multiple items), map usage fields, set `object: "chat.completion"`
    - _Requirements: 5.1–5.8, 8.2, 8.4_

  - [x] 2.4 Implement upstream error handling in all forward functions
    - Connection refused / DNS failure / timeout → HTTP 502 with `{"error": {"message": "upstream provider unreachable: {detail}", "type": "server_error"}}`
    - Upstream 4xx/5xx → forward status code and body as-is (OpenAI path) or wrap in OpenAI error format (Anthropic/Responses paths)
    - _Requirements: 6.4, 6.5, 3.5_

- [x] 3. Checkpoint — Ensure all proxy core tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 4. Integrate proxy lifecycle into GUI SkillRunner
  - [x] 4.1 Modify `gui/skill_runner.go` `executeAsync` to start/stop proxy
    - Before the step execution loop: check `NeedsOpenAIProxy(skill.RequiredEnv, run.extraEnv)`
    - If true: build `OpenAIProxyConfig` from current `MaclawLLMProvider` (accessed via `r.executor.app`), call `proxy.Start()`
    - On start failure: log error, continue without proxy (Req 1.4)
    - On success: inject `OPENAI_API_KEY=sk-maclaw-local-proxy`, `OPENAI_BASE_URL=http://127.0.0.1:{port}/v1`, `OPENAI_MODEL={model}` into `run.extraEnv`
    - `defer proxy.Stop()` for cleanup on completion, failure, or cancellation
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 2.1, 2.2, 2.3, 2.7_

  - [ ]* 4.2 Write unit tests for GUI proxy integration
    - Test that `NeedsOpenAIProxy` correctly gates proxy startup
    - Test that env vars are injected when proxy starts
    - Test that user-provided env vars bypass proxy
    - _Requirements: 2.4, 2.5, 2.6, 7.1, 7.2_

- [x] 5. Integrate proxy lifecycle into TUI toolRunSkill
  - [x] 5.1 Modify `tui/agent_tools.go` `toolRunSkill` to start/stop proxy
    - Before the step execution loop: check `NeedsOpenAIProxy(skill.RequiredEnv, extraEnvMap)`
    - If true: build `OpenAIProxyConfig` from current LLM config, call `proxy.Start()`
    - On start failure: log error, continue without proxy
    - On success: inject env vars into each step's subprocess environment (append to `cmd.Env`)
    - `defer proxy.Stop()` for cleanup
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 2.1, 2.2, 2.3, 2.7_

  - [ ]* 5.2 Write unit tests for TUI proxy integration
    - Test that proxy is started only when needed
    - Test that env vars are correctly injected into subprocess
    - Test user override bypass
    - _Requirements: 2.4, 2.5, 2.6, 7.1, 7.2_

- [x] 6. Checkpoint — Ensure all integration tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 7. Write property-based tests
  - [ ]* 7.1 Write property test for `NeedsOpenAIProxy` bypass logic
    - **Property 1: User-provided env vars bypass proxy**
    - **Validates: Requirements 2.4, 2.5, 2.6**
    - Use `rapid` to generate arbitrary `requiredEnv` slices and `extraEnv` maps; verify that when `OPENAI_API_KEY` or `OPENAI_BASE_URL` is present in extraEnv, `NeedsOpenAIProxy` returns false

  - [ ]* 7.2 Write property test for OpenAI direct forward
    - **Property 2: OpenAI direct forward preserves request structure**
    - **Validates: Requirements 3.1, 3.2, 6.6**
    - Generate arbitrary valid OpenAI request bodies; verify forwarded body is identical except `model` replaced and `stream` set to `false`

  - [ ]* 7.3 Write property test for Anthropic round-trip structural completeness
    - **Property 3: Anthropic conversion round-trip structural completeness**
    - **Validates: Requirements 8.1, 4.1, 4.8**
    - Generate requests with system/user/assistant messages; convert to Anthropic, simulate valid Anthropic response, convert back; verify all required OpenAI fields present (`id`, `object`, `model`, `choices`, `usage`)

  - [ ]* 7.4 Write property test for Responses API round-trip structural completeness
    - **Property 4: Responses API conversion round-trip structural completeness**
    - **Validates: Requirements 8.2, 5.1, 5.6**
    - Generate requests; convert to Responses API, simulate valid response, convert back; verify all required OpenAI fields present

  - [ ]* 7.5 Write property test for multiple content blocks concatenation
    - **Property 5: Multiple content blocks concatenation**
    - **Validates: Requirements 8.3, 8.4**
    - Generate Anthropic responses with N≥1 text blocks; verify concatenation. Generate Responses API responses with M≥1 message items; verify concatenation

  - [ ]* 7.6 Write property test for stream field forced to false
    - **Property 6: Stream field forced to false**
    - **Validates: Requirements 6.6**
    - Generate requests with `stream` set to `true`, `false`, or absent; verify forwarded request always has `stream: false`

  - [ ]* 7.7 Write property test for invalid JSON returns 400
    - **Property 7: Invalid JSON returns 400**
    - **Validates: Requirements 6.3**
    - Generate arbitrary non-JSON byte sequences; send to proxy handler; verify HTTP 400 with error body containing `message` and `type` fields

  - [ ]* 7.8 Write property test for non-OPENAI skills unaffected
    - **Property 8: Non-OPENAI skills unaffected**
    - **Validates: Requirements 7.1, 7.2**
    - Generate `requiredEnv` slices that do NOT contain `OPENAI_API_KEY`; verify `NeedsOpenAIProxy` returns false for all

- [ ] 8. Write integration tests with mock upstream servers
  - [ ]* 8.1 Create `corelib/openai_proxy_integration_test.go` with mock HTTP servers
    - Test full round-trip for OpenAI direct forward path: start proxy, send request, verify response from mock upstream
    - Test full round-trip for Anthropic conversion path: mock returns Anthropic response, verify OpenAI-format response to client
    - Test full round-trip for Responses API conversion path: mock returns Responses API response, verify OpenAI-format response to client
    - Test error scenarios: mock returns 500, mock unreachable (connection refused), request timeout
    - Test concurrent requests to same proxy instance
    - _Requirements: 1.6, 3.1–3.5, 4.1–4.11, 5.1–5.8, 6.1–6.5, 8.1–8.4_

  - [ ]* 8.2 Write unit tests for error response format and edge cases
    - Test 404 for unknown paths, 405 for non-POST methods
    - Test `stop_reason` → `finish_reason` mapping (`end_turn`→`stop`, `max_tokens`→`length`, unknown→`stop`)
    - Test `anthropic-version` header value
    - Test endpoint URL construction for each protocol path
    - Test `NeedsOpenAIProxy` with various input combinations (empty requiredEnv, OPENAI_API_KEY only, OPENAI_BASE_URL only, both, neither)
    - _Requirements: 6.1, 6.2, 4.7, 4.10_

- [x] 9. Final checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- The design uses Go with `pgregory.net/rapid` (already in go.mod) for property-based tests
- Proxy logic lives in `corelib/` so both GUI and TUI share the same implementation
- Checkpoints ensure incremental validation after core implementation and after integration
- Property tests validate universal correctness properties from the design document
- Unit tests validate specific examples and edge cases
