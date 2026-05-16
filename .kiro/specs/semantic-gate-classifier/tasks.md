# Implementation Plan: Semantic Gate Classifier

## Overview

Replace the keyword-based gate decision in `gui/coding_tool_gate.go` with a semantic, multi-layer classification approach. The `GateIntentClassifier` wraps a three-layer pipeline (keyword rules → embedding cosine similarity → LLM refinement) to classify user messages into five gate-specific categories: `new_project`, `bug_fix`, `maintenance`, `non_coding`, and `continuation`. Implementation proceeds incrementally: types and interfaces first, then each classification layer, then integration with existing gate and session guard functions.

## Tasks

- [x] 1. Define GateIntent types, result struct, and classifier skeleton
  - [x] 1.1 Create `gui/gate_intent_classifier.go` with GateIntent constants, GateIntentResult struct, ConversationContextProvider interface, GateIntentClassifier struct, and gateAnchor struct
    - Define `GateIntent` type and constants: `new_project`, `bug_fix`, `maintenance`, `non_coding`, `continuation`, `unknown`
    - Define `GateIntentResult` with fields: Intent, Confidence, Gap, Layer, Reason, AllScores
    - Define `ConversationContextProvider` interface with `RecentMessages(userID string, n int) []string`
    - Define `GateIntentClassifier` struct with embedder, anchors, queryCache, ctxProvider, llmConfig, httpClient, ready, mu fields
    - Define `gateAnchor` struct with Intent, Texts, Vecs fields
    - Implement stub `NewGateIntentClassifier(emb embedding.Embedder) *GateIntentClassifier` that stores anchors and creates QueryEmbeddingCache
    - Implement stub `Classify(text string, userID string) GateIntentResult` returning unknown
    - Implement `SetContextProvider`, `SetLLMConfig`, `Ready`, `DiagnoseScores` methods
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 8.4_

  - [ ]* 1.2 Write unit tests for GateIntent types and classifier initialization
    - Verify all five GateIntent constants are distinct
    - Verify `NewGateIntentClassifier(nil)` returns a classifier with `Ready()=false`
    - Verify `SetContextProvider` and `SetLLMConfig` store values correctly
    - _Requirements: 8.4, 3.7_

- [x] 2. Implement gate-specific anchor texts and background warmup
  - [x] 2.1 Implement `gateAnchors()` function returning anchor text sets for all five categories
    - `new_project`: ≥12 Chinese+English sentences (creation-oriented)
    - `bug_fix`: ≥12 Chinese+English sentences (fix/debug-oriented)
    - `maintenance`: ≥10 Chinese+English sentences (refactor/optimize)
    - `non_coding`: ≥10 Chinese+English sentences (translate/summarize)
    - `continuation`: ≥8 Chinese+English sentences (short action phrases)
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6_

  - [x] 2.2 Implement background anchor embedding warmup in `NewGateIntentClassifier`
    - Launch goroutine to embed all anchor texts using the embedder
    - Store resulting vectors in `gateAnchor.Vecs`
    - Set `ready=true` under write lock after all embeddings complete
    - Handle nil/NoopEmbedder gracefully (stay not ready)
    - _Requirements: 3.7, 4.5_

  - [ ]* 2.3 Write unit tests for anchor text completeness and warmup lifecycle
    - Verify each category has the required minimum number of anchors
    - Verify `Ready()` returns false before warmup, true after (with mock embedder)
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7_

- [x] 3. Implement Layer 1: Keyword rules classification
  - [x] 3.1 Implement keyword-based classification in `Classify` method
    - Reuse existing `codingKeywords`, `bugFixKeywords`, `creationCodingKeywords`, `nonCodingKeywords` for fast-path matching
    - Map keyword matches to GateIntent categories: coding+creation → `new_project`, bugfix-only → `bug_fix`, non-coding → `non_coding`
    - Return confidence ≥ 0.90 for strong keyword matches, set Layer=1
    - Handle mixed-intent: creation dominates bug-fix → `new_project`; bug-fix dominates maintenance → `bug_fix`; non-coding dominates coding when primary action is non-coding → `non_coding`
    - _Requirements: 2.1, 10.1, 10.2, 10.3_

  - [ ]* 3.2 Write property test for Category Classification Accuracy (Property 1)
    - **Property 1: Category Classification Accuracy**
    - Generate random messages from category-specific templates for all five categories
    - Assert `Classify()` returns the correct GateIntent with Confidence ≥ 0.70
    - Use `rapid` library with minimum 100 iterations
    - Tag: `Feature: semantic-gate-classifier, Property 1: Category Classification Accuracy`
    - **Validates: Requirements 1.1, 1.2, 1.3, 1.4**

  - [ ]* 3.3 Write property test for Mixed-Intent Dominant Classification (Property 9)
    - **Property 9: Mixed-Intent Dominant Classification**
    - Generate random messages containing signals from multiple categories
    - Assert creation signals dominate bug-fix → `new_project`, bug-fix dominates maintenance → `bug_fix`, non-coding dominates coding → `non_coding`
    - Use `rapid` library with minimum 100 iterations
    - Tag: `Feature: semantic-gate-classifier, Property 9: Mixed-Intent Dominant Classification`
    - **Validates: Requirements 10.1, 10.2, 10.3**

- [x] 4. Implement continuation detection with conversation context
  - [x] 4.1 Implement short-message continuation detection in `Classify`
    - Detect short messages (≤4 Chinese chars or ≤10 English chars)
    - When short + ConversationContextProvider available, scan recent 10 messages for coding signals
    - If coding context found + message matches continuation phrases → return `continuation` with confidence ≥ 0.60, Layer=1
    - If no coding context → return low confidence (<0.50), fall through to Layer 2
    - _Requirements: 1.5, 1.6, 6.1, 6.2, 6.3, 6.4_

  - [ ]* 4.2 Write property test for Continuation Detection with Coding Context (Property 2)
    - **Property 2: Continuation Detection with Coding Context**
    - Generate random short continuation phrases with mock ConversationContextProvider returning coding-related messages
    - Assert `Classify()` returns `GateIntentContinuation` with Confidence ≥ 0.60
    - Use `rapid` library with minimum 100 iterations
    - Tag: `Feature: semantic-gate-classifier, Property 2: Continuation Detection with Coding Context`
    - **Validates: Requirements 1.5, 6.3**

  - [ ]* 4.3 Write property test for Continuation Ambiguity without Coding Context (Property 3)
    - **Property 3: Continuation Ambiguity without Coding Context**
    - Generate random short continuation phrases with mock ConversationContextProvider returning non-coding messages
    - Assert `Classify()` returns Confidence < 0.50
    - Use `rapid` library with minimum 100 iterations
    - Tag: `Feature: semantic-gate-classifier, Property 3: Continuation Ambiguity without Coding Context`
    - **Validates: Requirements 1.6, 6.4**

- [x] 5. Checkpoint - Verify Layer 1 and continuation detection
  - Ensure all tests pass (`go test ./gui/ -run TestGateIntent`), ask the user if questions arise.

- [x] 6. Implement Layer 2: Embedding cosine similarity
  - [x] 6.1 Implement embedding-based classification in `Classify` method
    - When Layer 1 confidence < 0.90 and `ready=true`, compute user message embedding via QueryEmbeddingCache
    - Calculate cosine similarity against all anchor vectors for each category
    - Select top category; compute gap between top-1 and top-2 scores
    - If confidence ≥ 0.78 and gap ≥ 0.10 → return result with Layer=2
    - If confidence in [0.55, 0.78) or gap < 0.10 → fall through to Layer 3
    - If confidence < 0.55 → fall through to Layer 3
    - Populate AllScores in result for diagnostics
    - _Requirements: 2.2, 2.3, 2.4, 2.5, 4.2, 4.5, 10.4_

  - [ ]* 6.2 Write property test for Layer Escalation Correctness (Property 10)
    - **Property 10: Layer Escalation Correctness**
    - Generate random classification scenarios with mock embedder returning controlled similarity scores
    - Assert Layer field is the earliest layer that produced sufficient confidence
    - Use `rapid` library with minimum 100 iterations
    - Tag: `Feature: semantic-gate-classifier, Property 10: Layer Escalation Correctness`
    - **Validates: Requirements 2.1, 2.3**

- [x] 7. Implement Layer 3: LLM refinement
  - [x] 7.1 Implement LLM-based gate classification
    - Define `gateClassifierSystemPrompt` constant and `gateClassifierJSONSchema`
    - Implement `classifyGateIntentWithLLM()` using existing LLM infrastructure (HTTP client, model config, protocol detection)
    - Enforce 3-second timeout via `context.WithTimeout`
    - Implement `parseGateLLMResponse()` to parse JSON response into GateIntentResult
    - On success with confidence ≥ 0.60 → return result with Layer=3
    - On failure/timeout/low confidence → fall back to Layer 2 result or Layer 1 result
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5, 4.3, 4.4_

  - [ ]* 7.2 Write property test for LLM Fallback on Unreliable Results (Property 11)
    - **Property 11: LLM Fallback on Unreliable Results**
    - Generate random LLM results with confidence < 0.60 or simulated errors
    - Assert classifier falls back to Layer 2/1 result rather than returning unreliable LLM result
    - Use `rapid` library with minimum 100 iterations
    - Tag: `Feature: semantic-gate-classifier, Property 11: LLM Fallback on Unreliable Results`
    - **Validates: Requirements 7.4, 7.5, 2.6**

  - [ ]* 7.3 Write property test for LLM Response JSON Round-Trip Parsing (Property 12)
    - **Property 12: LLM Response JSON Round-Trip Parsing**
    - Generate random valid JSON strings with gate_intent, confidence, reason fields → assert correct parsing
    - Generate random invalid JSON or missing fields → assert error returned
    - Use `rapid` library with minimum 100 iterations
    - Tag: `Feature: semantic-gate-classifier, Property 12: LLM Response JSON Round-Trip Parsing`
    - **Validates: Requirements 7.1**

- [x] 8. Checkpoint - Verify all three classification layers
  - Ensure all tests pass (`go test ./gui/ -run TestGateIntent`), ask the user if questions arise.

- [x] 9. Implement gate decision integration with `mapGateIntentToConfig`
  - [x] 9.1 Implement `mapGateIntentToConfig()` function and update `newCodingToolGateConfigWithClassifier()`
    - Implement `mapGateIntentToConfig(result GateIntentResult, skip bool) codingToolGateConfig` mapping:
      - `new_project` + confidence ≥ 0.70 → active=true
      - `bug_fix` → active=false, bugFix=true
      - `maintenance` → active=false
      - `non_coding` → active=false
      - `continuation` → active=false
      - `unknown` or low confidence → active=false
    - Update `newCodingToolGateConfigWithClassifier()` to accept `*GateIntentClassifier` and `userID` parameters
    - Try semantic classification first when classifier is available and ready
    - Fall back to keyword-based `classifyTaskIntent()` + `isBugFixOnly()` when classifier unavailable
    - Preserve skip signal and background loop bypass logic unchanged
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5, 5.6, 5.7, 5.8, 5.9_

  - [ ]* 9.2 Write property test for Gate Activation Correctness (Property 4)
    - **Property 4: Gate Activation Correctness**
    - Generate random GateIntentResult structs with varying intents and confidence values
    - Assert `mapGateIntentToConfig()` produces correct active/bugFix fields per the mapping rules
    - Use `rapid` library with minimum 100 iterations
    - Tag: `Feature: semantic-gate-classifier, Property 4: Gate Activation Correctness`
    - **Validates: Requirements 5.2, 5.3, 5.4, 5.5, 5.6**

  - [ ]* 9.3 Write property test for Skip Signal Always Bypasses Gate (Property 5)
    - **Property 5: Skip Signal Always Bypasses Gate**
    - Generate random user messages with skip signals appended from `skipSignalsChinese`/`skipSignalsEnglish`
    - Assert gate returns active=false regardless of classifier result
    - Use `rapid` library with minimum 100 iterations
    - Tag: `Feature: semantic-gate-classifier, Property 5: Skip Signal Always Bypasses Gate`
    - **Validates: Requirements 5.7**

  - [ ]* 9.4 Write property test for Background Loop Always Bypasses Gate (Property 6)
    - **Property 6: Background Loop Always Bypasses Gate**
    - Generate random user messages processed with LoopKindBackground
    - Assert gate returns active=false regardless of classifier result
    - Use `rapid` library with minimum 100 iterations
    - Tag: `Feature: semantic-gate-classifier, Property 6: Background Loop Always Bypasses Gate`
    - **Validates: Requirements 5.8**

  - [ ]* 9.5 Write property test for Keyword Fallback When Classifier Unavailable (Property 7)
    - **Property 7: Keyword Fallback When Classifier Unavailable**
    - Generate random user messages, call gate with nil classifier and with not-ready classifier
    - Assert gate produces identical active/bugFix fields to keyword-based implementation
    - Use `rapid` library with minimum 100 iterations
    - Tag: `Feature: semantic-gate-classifier, Property 7: Keyword Fallback When Classifier Unavailable`
    - **Validates: Requirements 5.9, 8.2**

- [x] 10. Implement session guard integration
  - [x] 10.1 Update `checkSessionTaskGuard()` in `gui/im_tools_session.go` to consult GateIntentClassifier
    - Add `getGateIntentClassifier()` method to `IMMessageHandler`
    - When keyword classification returns ambiguous/unknown, consult GateIntentClassifier
    - `new_project`/`bug_fix`/`maintenance`/`continuation` → allow session (return "")
    - `non_coding` → block session with hint message
    - Preserve existing `hasCodingActionPhrase()` + `conversationHasCodingContext()` logic
    - _Requirements: 9.1, 9.2, 9.3, 9.4_

  - [ ]* 10.2 Write property test for Session Guard Correctness (Property 8)
    - **Property 8: Session Guard Correctness**
    - Generate random GateIntentResult structs with intents in {new_project, bug_fix, maintenance, continuation, non_coding}
    - Assert coding-related intents return empty string (allow), non_coding returns non-empty hint
    - Use `rapid` library with minimum 100 iterations
    - Tag: `Feature: semantic-gate-classifier, Property 8: Session Guard Correctness`
    - **Validates: Requirements 9.2, 9.3**

- [x] 11. Implement observability, logging, and diagnostics
  - [x] 11.1 Add debug logging to `Classify` method
    - Log classification path (Layer 1/2/3), top-2 categories with confidence scores, and final decision
    - Log when semantic result overrides keyword result (include both results)
    - Use existing `log` package at debug level
    - _Requirements: 8.3, 8.5, 11.1, 11.3_

  - [x] 11.2 Implement `DiagnoseScores()` to return full scoring breakdown
    - Return `map[GateIntent]float64` with scores for all five categories
    - Usable in tests and debugging tools
    - _Requirements: 11.4_

  - [ ]* 11.3 Write unit tests for logging and DiagnoseScores output
    - Verify DiagnoseScores returns scores for all five categories
    - Verify log output contains expected fields (layer, confidence, intent)
    - _Requirements: 8.3, 11.1, 11.4_

- [x] 12. Wire up initialization in `gui/app.go` and `gui/im_message_handler.go`
  - [x] 12.1 Initialize GateIntentClassifier in App startup
    - Create `GateIntentClassifier` in `App` struct initialization using the existing embedder
    - Call `SetContextProvider` with appropriate provider
    - Call `SetLLMConfig` with lazy config accessor and HTTP client
    - Pass classifier to `newCodingToolGateConfigWithClassifier()` in agent loop gate config creation
    - _Requirements: 5.1, 8.4_

  - [ ]* 12.2 Write integration tests verifying end-to-end gate decision pipeline
    - Test full pipeline: user message → `newCodingToolGateConfigWithClassifier()` → `codingToolGateConfig` with mock classifier
    - Verify existing `coding_tool_gate_test.go` tests still pass unchanged
    - _Requirements: 8.2_

- [x] 13. Final checkpoint - Full test suite verification
  - Ensure all tests pass (`go test ./gui/`), including existing `coding_tool_gate_test.go` tests. Ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation after each major layer
- Property tests validate universal correctness properties from the design document using the `rapid` library
- Unit tests validate specific examples, edge cases, and lifecycle behavior
- All existing tests in `gui/coding_tool_gate_test.go` must continue to pass unchanged throughout implementation
- The project builds with `go build ./gui/` and tests with `go test ./gui/`
