# Implementation Plan: Unified Intent Classifier (UIC)

## Overview

Build a new `corelib/intent/` package implementing a three-layer classification pipeline (keywords → embedding cosine → LLM), then migrate 6 consumer modules to use it. Each task builds incrementally — core types first, then layers, then adapters, then consumer wiring.

## Tasks

- [x] 1. Create core types and package structure
  - [x] 1.1 Create `corelib/intent/types.go` with all type definitions
    - Define `IntentLabel` constants (12 labels), `KeywordStrength`, `KeywordEntry`, `ClassificationResult`, `MessageContext`, `LLMClassifyFunc`
    - Implement `AllLabels()`, `IntentLabel.IsValid()`
    - _Requirements: 1.5, 2.1, 2.2_

  - [ ]* 1.2 Write property tests for intent label taxonomy validity
    - **Property 3: Intent label taxonomy validity**
    - **Validates: Requirements 2.1, 2.3**

  - [x] 1.3 Create `corelib/intent/tool_affinity.go` with ToolAffinityRegistry
    - Implement `NewToolAffinityRegistry()` with default mappings (ssh→ssh, search→web_search, document_delivery→send_file/open/craft_tool, browser→browser_*/gui_record_*, office→office, coding/maintenance→generate_pdf/office)
    - Implement `ToolsFor(label)` and `Resolve(primary, secondary)`
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8_

  - [ ]* 1.4 Write property test for tool affinity mapping correctness
    - **Property 4: Tool affinity mapping correctness**
    - **Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7**

- [x] 2. Implement keyword registry and Layer 1
  - [x] 2.1 Create `corelib/intent/keyword_registry.go` with consolidated keyword data
    - Define `var defaultKeywords = []KeywordEntry{...}` consolidating all keywords from router.go, im_intent_classifier.go, im_tools_session.go, coding_tool_gate.go, gate_intent_classifier.go
    - Implement `KeywordRegistry` struct with `entries`, `byLabel`, `strongIndex`, `weakByLabel`
    - Implement `NewKeywordRegistry()` with conflict resolution priority (ssh > browser(strong) > coding > non_coding > ambiguous)
    - Implement `Match(text)` returning `[]KeywordMatch`
    - _Requirements: 5.1, 5.2, 18.1, 18.2, 18.4_

  - [x] 2.2 Create `corelib/intent/layer1.go` with keyword classification logic
    - Implement `classifyByKeywords(msg MessageContext) (ClassificationResult, bool)`
    - Group matches by label, count strong/weak hits
    - Apply mixed-intent dominance rules (creation > bug-fix > maintenance; non-coding primary action > coding context)
    - Browser two-tier detection (strong → 0.92, weak combo → 0.55)
    - Continuation detection for short messages (≤10 runes) with conversation context
    - Return `(result, true)` when confidence ≥ 0.90, else `(result, false)` to escalate
    - _Requirements: 4.1, 4.2, 5.2, 5.3, 5.4, 18.3_

  - [ ]* 2.3 Write property test for keyword priority and dominance
    - **Property 6: Keyword classification with priority and dominance**
    - **Validates: Requirements 5.2, 5.3, 5.4, 18.3**

- [x] 3. Checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 4. Implement Layer 2 (embedding cosine) and Layer 3 (LLM)
  - [x] 4.1 Create `corelib/intent/layer2.go` with embedding cosine classification
    - Implement `classifyByEmbedding(text string) (ClassificationResult, bool)`
    - Define `intentAnchor` struct and anchor texts for all 10 non-ambiguous/unknown labels
    - Reuse `QueryEmbeddingCache` for query embeddings
    - Compute max cosine similarity per anchor set, find top-1 and top-2
    - Confident if `top1Score >= 0.78 && gap >= 0.10`, else escalate
    - Implement background anchor warmup goroutine
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5_

  - [x] 4.2 Create `corelib/intent/layer3.go` with LLM refinement
    - Implement `classifyByLLM(msg MessageContext) (ClassificationResult, error)`
    - Build unified system prompt covering all 12 intent labels with disambiguation rules
    - Request structured JSON output: `{"intent", "confidence", "reason", "secondary"}`
    - Enforce configurable timeout (default 8s)
    - Confidence < 0.60 → treat as ambiguous
    - Handle timeout/failure by returning error (caller falls back to lower layer)
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5, 4.6_

- [x] 5. Implement main classifier with cache and pipeline orchestration
  - [x] 5.1 Create `corelib/intent/classifier.go` with UnifiedIntentClassifier
    - Implement `Config` struct and `New(cfg Config)` constructor
    - Implement `Classify(msg MessageContext) ClassificationResult` with three-layer pipeline orchestration
    - Implement per-message cache (`sync.Map` keyed by message text)
    - Implement `InvalidateCache()`, `Ready()`, `SetLLMFunc()`, `DiagnoseScores()`
    - Handle graceful degradation: skip Layer 2 when NoopEmbedder, skip Layer 3 when LLM nil
    - Log all decisions with `[UnifiedIntentClassifier]` prefix
    - Log layer escalations and Layer 3 overrides
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 4.2, 4.3, 4.4, 4.5, 4.7, 4.8, 14.1, 14.2, 14.3, 14.4, 14.5, 15.5, 17.1, 17.2, 17.3, 17.4, 17.5_

  - [ ]* 5.2 Write property test for classification cache idempotence
    - **Property 1: Classification cache idempotence**
    - **Validates: Requirements 1.2, 1.3, 1.4**

  - [ ]* 5.3 Write property test for classification result structural validity
    - **Property 2: Classification result structural validity**
    - **Validates: Requirements 1.5, 2.2, 4.8**

  - [ ]* 5.4 Write property test for pipeline layer escalation correctness
    - **Property 5: Pipeline layer escalation correctness**
    - **Validates: Requirements 4.2, 4.3, 4.4, 4.5, 4.7, 7.3**

  - [ ]* 5.5 Write property test for graceful degradation
    - **Property 7: Graceful degradation skips unavailable layers**
    - **Validates: Requirements 6.4, 14.1, 14.2, 14.3, 14.4**

- [x] 6. Checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 7. Implement consumer adapters
  - [x] 7.1 Create `corelib/intent/adapters.go` with adapter methods
    - Implement `ToTaskIntent()` mapping: coding/bug_fix/maintenance→"coding", ssh→"ssh", non_coding/browser/search/document_delivery/office→"non_coding", ambiguous/unknown→"ambiguous"
    - Implement `ToGateIntent()` mapping: coding(creation)→"new_project", bug_fix→"bug_fix", maintenance→"maintenance", non_coding/search/document_delivery/office/browser→"non_coding", continuation→"continuation"
    - Implement `IsCodingLike()` and `IsNonCodingLike()` helpers
    - _Requirements: 9.2, 9.3, 9.4, 9.5, 9.6, 12.1, 12.2, 12.3_

  - [ ]* 7.2 Write property test for IMIntentClassifier adapter mapping
    - **Property 8: IMIntentClassifier adapter mapping**
    - **Validates: Requirements 9.3, 9.4, 9.5, 9.6**

  - [ ]* 7.3 Write property test for session guard allow/block decisions
    - **Property 9: Session guard allow/block decisions**
    - **Validates: Requirements 10.2, 10.3**

  - [ ]* 7.4 Write property test for coding tool gate activation decisions
    - **Property 10: Coding tool gate activation decisions**
    - **Validates: Requirements 11.2, 11.4**

  - [ ]* 7.5 Write property test for GateIntentClassifier adapter mapping
    - **Property 11: GateIntentClassifier adapter mapping**
    - **Validates: Requirements 12.1, 12.2, 12.3**

- [x] 8. Migrate Router to consume UIC
  - [x] 8.1 Modify `corelib/tool/router.go` to use UIC
    - Add `SetUnifiedClassifier(uic *UnifiedIntentClassifier)` setter
    - In `Route()`, when UIC is available, obtain `ClassificationResult` and use `ToolNames` from Tool Affinity instead of evaluating `conditionalKeepRules` keyword matches
    - Preserve `needsSemanticConfirm` mechanism for browser weak signals by checking UIC confidence and layer
    - Preserve Eager Pin for matched conditional tools using UIC result
    - Fall back to existing `conditionalKeepRules` when UIC is nil
    - _Requirements: 8.1, 8.2, 8.3, 8.4, 8.5, 16.1, 16.3_

  - [ ]* 8.2 Write unit tests for Router UIC integration
    - Test Router uses UIC Tool Affinity when available
    - Test Router falls back to conditionalKeepRules when UIC is nil
    - _Requirements: 8.1, 8.5_

- [ ] 9. Migrate IMIntentClassifier and SessionGuard
  - [x] 9.1 Modify `gui/im_intent_classifier.go` to delegate to UIC
    - In `classifyTaskIntent()`, when UIC is available, call `Classify()` then `ToTaskIntent()` to map to existing `taskIntentResult` structure
    - Preserve all `taskIntentResult` fields: Intent, Matched, Evidence, Reason, Confidence, Source
    - Fall back to existing keyword logic when UIC is nil
    - _Requirements: 9.1, 9.2, 9.3, 9.4, 9.5, 9.6, 16.1, 16.3_

  - [x] 9.2 Modify `gui/im_tools_session.go` to delegate to UIC
    - In `checkSessionTaskGuard()`, when UIC is available, use `ClassificationResult` instead of calling `classifyTaskIntent()` with local keyword lists
    - Allow session creation for coding/bug_fix/maintenance, block for non_coding/search/document_delivery/office
    - Preserve context-aware continuation logic for continuation/ambiguous with coding history
    - Fall back to existing logic when UIC is nil
    - _Requirements: 10.1, 10.2, 10.3, 10.4, 16.1, 16.3_

- [x] 10. Migrate CodingToolGate and GateIntentClassifier
  - [x] 10.1 Modify `gui/coding_tool_gate.go` to use UIC
    - When UIC is available, use `ClassificationResult` for bug-fix detection (`isBugFixOnly`) and creation-oriented signal detection
    - Bypass three-phase workflow when UIC primary is `bug_fix` or `continuation` with high confidence
    - Activate three-phase workflow when UIC primary is `coding` with creation signals
    - Preserve skip-signal detection as separate fast-path check independent of UIC
    - Fall back to existing logic when UIC is nil
    - _Requirements: 11.1, 11.2, 11.3, 11.4, 11.5, 16.1_

  - [x] 10.2 Modify `gui/gate_intent_classifier.go` to delegate to UIC
    - In `Classify()`, when UIC is available, call UIC `Classify()` then `ToGateIntent()` to map to `GateIntentResult`
    - Preserve `GateIntentResult` fields: Intent, Confidence, Gap, Layer, Reason, AllScores
    - Reuse existing gate anchor texts as part of UIC Layer 2 Anchor Sets
    - Fall back to existing three-layer pipeline when UIC is nil
    - _Requirements: 12.1, 12.2, 12.3, 12.4, 16.1_

- [x] 11. Migrate CapabilityGapDetector and wire UIC into App
  - [x] 11.1 Modify `gui/capability_gap_detector.go` to use UIC signals
    - When UIC is available, check whether the original user message was classified as non-coding/ambiguous before applying gap detection
    - Remove `gapKeywords` fallback when UIC is available; retain `gapKeywords` as last resort when no LLM is configured
    - Retain LLM-based gap detection logic (response analysis, not user intent)
    - _Requirements: 13.1, 13.2, 13.3, 13.4_

  - [x] 11.2 Wire UIC into App initialization (`gui/app.go` or `gui/app_embedding.go`)
    - Create UIC in `activateEmbedderAsync()` after existing IntentClassifier creation
    - Inject into Router via `SetUnifiedClassifier()`
    - Inject into GateIntentClassifier via `SetUnifiedClassifier()`
    - IMMessageHandler, SessionGuard, CodingToolGate access UIC via App reference
    - Add `SetUnifiedClassifier()` setter to App struct
    - Log available layers at startup
    - _Requirements: 16.2, 16.4, 14.5_

- [x] 12. Checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 13. Final integration and backward compatibility verification
  - [x] 13.1 Add cache invalidation call in message processing cycle
    - Call `InvalidateCache()` after each message processing cycle completes (in IMMessageHandler)
    - _Requirements: 1.3_

  - [ ]* 13.2 Write integration tests for backward compatibility
    - Test nil UIC → all consumers fall back to existing behavior
    - Test public API signatures unchanged: `classifyTaskIntent()`, `checkSessionTaskGuard()`, `GateIntentClassifier.Classify()`, `Router.Route()`
    - _Requirements: 16.1, 16.2, 16.3_

- [x] 14. Final checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties from the design document
- Unit tests validate specific examples and edge cases
- All new code goes in `corelib/intent/` package; consumer modifications are minimal adapter wiring
- Every consumer preserves nil-UIC fallback to existing logic for backward compatibility (Requirement 16)
