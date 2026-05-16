# Implementation Plan: SkillRouter Body-Aware Retrieval

## Overview

基于阿里 SkillRouter 论文，对 maclaw 工具检索管线进行四层递进改进：Body 数据层 → Body 采集层 → Enrichment/Embedding 分叉 → LLM Listwise 重排序。所有变更在 `corelib/tool/` 包内完成，保持向后兼容。

## Tasks

- [x] 1. RegisteredTool Body 字段扩展与截断函数
  - [x] 1.1 Add Body and BodySummary fields to RegisteredTool struct
    - In `corelib/tool/types.go`, add `Body string` and `BodySummary string` fields with `json:"body,omitempty"` and `json:"body_summary,omitempty"` tags
    - _Requirements: 1.1, 1.2_

  - [x] 1.2 Implement TruncateBody function in new file `corelib/tool/truncate.go`
    - Create `TruncateBody(body string, maxChars int) string` with markdown-aware truncation
    - Define `const DefaultBodyMaxChars = 1500`
    - Truncation strategy: preserve complete lines, prioritize headings (`#`), list items (`-`/`*`), code block boundaries (`` ``` ``), append `\n...` when truncated
    - Handle edge cases: empty body returns empty, maxChars ≤ 0 returns empty, single overlong line truncates at char boundary
    - _Requirements: 1.3, 1.4, 1.5, 1.6, 11.1, 11.2, 11.3, 11.4, 11.5_

  - [x]* 1.3 Write property tests for TruncateBody in `corelib/tool/truncate_property_test.go`
    - **Property 1: TruncateBody 短文本恒等** — For any body with rune length ≤ maxChars, output equals input
    - **Validates: Requirements 1.4, 11.4**
    - **Property 2: TruncateBody 输出不变量** — For any body and maxChars > 0: (a) output rune length ≤ maxChars + len("..."), (b) every line in output is an exact copy of an input line, (c) output ends with `\n...` iff truncation occurred
    - **Validates: Requirements 1.3, 1.6, 11.2, 11.3, 11.5**

  - [x]* 1.4 Write unit tests for TruncateBody in `corelib/tool/truncate_test.go`
    - TestTruncateBody_Empty, TestTruncateBody_ExactLimit, TestTruncateBody_PreservesHeadings, TestTruncateBody_CodeBlockBoundary
    - _Requirements: 1.3, 1.4, 1.5, 1.6, 11.1, 11.2, 11.3, 11.4, 11.5_

- [x] 2. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 3. Body 采集层：Builtin, MCP, NL Skill
  - [x] 3.1 Add BuiltinBodies map to `corelib/tool/enrichment.go`
    - Define `var BuiltinBodies = map[string]string{...}` with parameter schema descriptions for core builtin tools (bash, read_file, write_file, list_directory, memory, web_search, web_fetch, screenshot, send_and_observe, create_session, call_mcp_tool, browser_connect, browser_navigate, browser_click, etc.)
    - _Requirements: 4.1, 4.4_

  - [x] 3.2 Modify Registry.Register() to auto-populate Body from BuiltinBodies
    - In `corelib/tool/registry.go`, after setting defaults, if `tool.Body == ""` and `BuiltinBodies[tool.Name]` exists, set `tool.Body = BuiltinBodies[tool.Name]` and `tool.BodySummary = TruncateBody(tool.Body, DefaultBodyMaxChars)`
    - _Requirements: 4.2, 4.3_

  - [x]* 3.3 Write property test for Builtin Body auto-population in `corelib/tool/registry_body_property_test.go`
    - **Property 7: Builtin Body 自动填充** — For any tool name in BuiltinBodies, registering with empty Body results in Body == BuiltinBodies[name] and BodySummary == TruncateBody(BuiltinBodies[name], DefaultBodyMaxChars)
    - **Validates: Requirements 4.2, 4.3**

  - [x] 3.4 Implement BuildMCPToolBody function in `corelib/tool/reranker.go` (new file, will also hold Reranker interface later)
    - `func BuildMCPToolBody(schema map[string]interface{}) string` — serialize inputSchema into readable "Parameters:\n- name (type): description" format
    - Return empty string for nil or empty schema
    - _Requirements: 3.1, 3.2, 3.3_

  - [x]* 3.5 Write property test for BuildMCPToolBody in `corelib/tool/reranker_property_test.go`
    - **Property 5: MCP Body 构建包含 Schema 信息** — For any inputSchema with ≥1 property, output contains each property's name and type. Empty/nil schema returns ""
    - **Validates: Requirements 3.1, 3.3**

  - [x]* 3.6 Write unit tests for BuildMCPToolBody in `corelib/tool/reranker_test.go`
    - TestBuildMCPToolBody_EmptySchema, TestBuildMCPToolBody_NestedSchema, TestBuildMCPToolBody_MultipleParams
    - _Requirements: 3.1, 3.2, 3.3_

  - [x] 3.7 Add NL Skill body population in skill registration flow
    - In `gui/app_nl_skills.go`, when registering an NL Skill, read SKILL.md content into Body field, set BodySummary via TruncateBody
    - When SKILL.md cannot be read, log warning and leave Body empty
    - When importing from remote with AgentSkillMD, use that as Body
    - _Requirements: 2.1, 2.2, 2.3_

- [x] 4. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.


- [x] 5. Enrichment Prompt 改进
  - [x] 5.1 Update GenerateEnrichmentPrompt to accept bodySummary parameter
    - In `corelib/tool/enrichment.go`, change signature to `GenerateEnrichmentPrompt(toolName, description, bodySummary string) (system, user string)`
    - When bodySummary is non-empty, include it in the user prompt and update system prompt to instruct LLM to generate implementation-detail-aware and distinguishing queries
    - When bodySummary is empty, fall back to name + description only (backward compatible)
    - _Requirements: 5.1, 5.2, 5.3, 5.4_

  - [x] 5.2 Update all callers of GenerateEnrichmentPrompt to pass bodySummary
    - Search for all call sites and add the third argument (pass empty string where BodySummary is unavailable)
    - _Requirements: 5.1_

  - [x]* 5.3 Write property test for enrichment prompt in `corelib/tool/enrichment_body_property_test.go`
    - **Property 6: Enrichment Prompt 包含 Body Summary** — For any non-empty bodySummary, user prompt contains bodySummary. For empty bodySummary, user prompt does not contain body-related content
    - **Validates: Requirements 5.1, 5.2**

  - [x]* 5.4 Write unit tests for enrichment prompt in `corelib/tool/enrichment_test.go`
    - TestGenerateEnrichmentPrompt_WithBody, TestGenerateEnrichmentPrompt_EmptyBody
    - _Requirements: 5.1, 5.2, 5.3, 5.4_

- [x] 6. Embedding 文本分叉（BM25 不变，Embedding 含 BodySummary）
  - [x] 6.1 Add buildEmbeddingText() method to Router in `corelib/tool/router.go`
    - `func (r *Router) buildEmbeddingText(name, description string) string` — returns name + description + BodySummary (from registry lookup)
    - When BodySummary is empty, returns name + description only
    - _Requirements: 7.1, 7.2_

  - [x] 6.2 Update Router.Route() to pass embedding text to HybridRetriever
    - Build separate `embeddingTexts` map using `buildEmbeddingText()` for each candidate
    - Pass `embeddingTexts` (instead of `candidateTexts`) to `r.hybrid.FuseScores()`
    - Keep BM25 indexing using `buildSearchText()` unchanged
    - _Requirements: 6.1, 6.2, 7.3_

  - [x] 6.3 Add buildEmbeddingText() to DynamicToolBuilder in `corelib/tool/builder.go`
    - Same logic as Router's buildEmbeddingText()
    - Update Build() to pass embedding texts to hybrid.FuseScores() separately from BM25 texts
    - _Requirements: 6.3, 7.4_

  - [x]* 6.4 Write property tests for text construction in `corelib/tool/router_body_property_test.go`
    - **Property 3: BM25 文本不包含 Body** — For any RegisteredTool with Body length > 50, buildSearchText() output does not contain Body substring
    - **Validates: Requirements 6.1, 6.2**
    - **Property 4: Embedding 文本包含 BodySummary** — For any RegisteredTool with non-empty BodySummary, buildEmbeddingText() output contains BodySummary. When BodySummary is empty, output contains only name + description
    - **Validates: Requirements 7.1, 7.3**

- [x] 7. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 8. Reranker 接口与 LLM Listwise 重排序
  - [x] 8.1 Define Reranker interface and CandidateSummary type in `corelib/tool/reranker.go`
    - `type CandidateSummary struct { Name, Description, BodySummary string }`
    - `type Reranker interface { Rerank(userMessage string, candidates []CandidateSummary, topK int) ([]string, error) }`
    - _Requirements: 9.1, 8.7_

  - [x] 8.2 Add SetReranker() to Router and integrate reranking into Route()
    - Add `reranker Reranker` field and `SetReranker(rr Reranker)` method to Router
    - In Route(), after three-signal scoring: if reranker != nil and candidates > MaxToolBudget, take top-20, build CandidateSummary list, call Rerank(userMessage, candidates, 5)
    - Promote reranked results to front of candidate list
    - On error or empty result: fall back to fused score ordering, log warning
    - On < 5 results: supplement from fused score list
    - _Requirements: 8.1, 8.2, 8.3, 8.5, 8.6, 9.2, 9.3_

  - [x] 8.3 Add SetReranker() to DynamicToolBuilder and integrate reranking into Build()
    - Mirror Router's reranker integration in DynamicToolBuilder.Build()
    - _Requirements: 9.4_

  - [x]* 8.4 Write property tests for reranker integration in `corelib/tool/router_body_property_test.go`
    - **Property 8: Reranker 调用契约** — When reranker is configured and candidates > MaxToolBudget, Rerank is called with ≤ 20 candidates and topK=5. When not configured, no Rerank call
    - **Validates: Requirements 8.1, 8.2, 8.4, 9.3**
    - **Property 9: Reranker 失败优雅回退** — With an always-error reranker, Route() output matches no-reranker output. With < 5 results, Router supplements from fused scores
    - **Validates: Requirements 8.5, 8.6**

  - [x]* 8.5 Write unit tests for reranker integration in `corelib/tool/router_body_test.go`
    - TestRouter_Reranker_NotConfigured, TestRouter_Reranker_Error, TestRouter_Reranker_PartialResults
    - Use mockReranker with configurable return values and error
    - _Requirements: 8.1, 8.4, 8.5, 8.6_

- [x] 9. 向后兼容性验证
  - [x]* 9.1 Write property test for backward compatibility in `corelib/tool/router_body_property_test.go`
    - **Property 10: 空 Body 向后兼容** — With all tools having empty Body/BodySummary, NoopEmbedder, and nil Reranker, Router.Route() and DynamicToolBuilder.Build() produce identical output to current implementation
    - **Validates: Requirements 10.1, 10.2, 10.3, 10.4**

  - [x]* 9.2 Write property test for Router/Builder consistency in `corelib/tool/router_body_property_test.go`
    - **Property 11: Router 与 Builder 行为一致性** — For same tool set and user message, Router and DynamicToolBuilder produce identical buildSearchText() and buildEmbeddingText() output for each tool
    - **Validates: Requirements 6.3, 7.4, 10.5**

- [x] 10. 可观测性与日志扩展
  - [x] 10.1 Extend writeRouteLog to include body_aware field and reranker logging
    - Add `bodyAware bool` parameter to writeRouteLog, log it as `Body-aware: true/false`
    - When reranker is invoked, log candidate count and reranked order
    - When reranker fails, log error reason and fallback action
    - Update Route() call to writeRouteLog to pass bodyAware flag (true when any candidate has non-empty BodySummary and hybrid is active)
    - _Requirements: 12.1, 12.2, 12.3, 12.4_

  - [x]* 10.2 Write unit test for body_aware log field in `corelib/tool/router_body_test.go`
    - TestRouter_BodyAware_LogField — verify writeRouteLog includes body_aware field
    - _Requirements: 12.4_

- [x] 11. Integration wiring and caller updates
  - [x] 11.1 Update gui/tool_router.go to pass through SetReranker
    - Wire SetReranker from application layer to Router
    - _Requirements: 9.2_

  - [x] 11.2 Update gui/tool_builder.go to pass through SetReranker
    - Wire SetReranker from application layer to DynamicToolBuilder
    - _Requirements: 9.4_

- [x] 12. Final checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests use `pgregory.net/rapid` library with ≥100 iterations per property
- All code is in Go, targeting the `corelib/tool/` package primarily
- The design uses Go throughout — no language selection was needed
