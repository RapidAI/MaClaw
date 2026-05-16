# Requirements Document

## Introduction

The codebase currently has 6+ files maintaining independent keyword lists for user intent classification (coding/ssh/non-coding/bug-fix/continuation/browser/search/document-delivery/excel/pptx). Each module duplicates keywords, applies inconsistent classification logic, and produces conflicting results for the same input. This architecture has caused bugs #34, #36, #42, #44 and creates an ever-growing maintenance burden where every bug fix adds more keywords to scattered lists.

This feature replaces all user-intent keyword classification with a Unified Intent Classifier (UIC) that computes a single, structured classification result per user message and shares it across all consumers. The existing `GateIntentClassifier` three-layer pipeline (keywords → embedding cosine → LLM) is generalized into a shared service. Technical signal detection (error patterns, security commands, tool name lists) is explicitly out of scope.

## Glossary

- **UIC**: Unified Intent Classifier — the single entry point for all user-intent classification in the system
- **Intent_Label**: A structured classification tag representing user intent (e.g., `coding`, `ssh`, `non_coding`, `browser`, `search`, `document_delivery`, `bug_fix`, `continuation`, `maintenance`, `office`)
- **Tool_Affinity**: A mapping from an Intent_Label to a set of tool names that should be activated for that intent
- **Classification_Result**: The structured output of the UIC containing primary intent, confidence score, secondary intents, tool affinities, and classification layer
- **Layer_1**: Keyword-based fast-path classification (<1ms)
- **Layer_2**: Embedding cosine similarity classification against anchor vectors (~5ms)
- **Layer_3**: LLM-based refinement for ambiguous cases (1-3s, with timeout)
- **Consumer**: Any module that needs intent classification results (Router, IMIntentClassifier, SessionGuard, CodingToolGate, CapabilityGapDetector, GateIntentClassifier)
- **Message_Context**: The combination of current user message text, conversation history, and attachment metadata used as input to the UIC
- **Anchor_Set**: Pre-computed embedding vectors for representative sentences of each Intent_Label, used in Layer 2
- **Confidence_Threshold**: The minimum confidence score required for a classification layer to produce a definitive result without escalating to the next layer
- **Eager_Pin**: The mechanism that activates conditional tools in the Router session upon first intent match, persisting them for subsequent messages

## Requirements

### Requirement 1: Single Classification Entry Point

**User Story:** As a developer, I want all intent classification to go through one entry point, so that classification logic is consistent and maintainable.

#### Acceptance Criteria

1. THE UIC SHALL expose a `Classify(message Message_Context) Classification_Result` method as the sole entry point for user-intent classification
2. WHEN a user message is received, THE UIC SHALL compute the Classification_Result exactly once per message
3. THE UIC SHALL cache the Classification_Result for the duration of the current message processing cycle so that all Consumers receive the same result
4. IF a Consumer requests classification for a message that has already been classified, THEN THE UIC SHALL return the cached Classification_Result without recomputation
5. THE Classification_Result SHALL contain: primary Intent_Label, confidence score [0,1], secondary Intent_Label list, Tool_Affinity set, classification layer (1/2/3), and human-readable reason string

### Requirement 2: Intent Label Taxonomy

**User Story:** As a developer, I want a unified set of intent labels that covers all current classification needs, so that every consumer can map UIC output to its specific decision.

#### Acceptance Criteria

1. THE UIC SHALL support the following Intent_Labels: `coding`, `ssh`, `non_coding`, `browser`, `search`, `document_delivery`, `bug_fix`, `continuation`, `maintenance`, `office`, `ambiguous`, `unknown`
2. THE UIC SHALL assign exactly one primary Intent_Label per Classification_Result
3. THE UIC SHALL assign zero or more secondary Intent_Labels when the message contains mixed-intent signals
4. WHEN a new Intent_Label is added to the taxonomy, THE UIC SHALL require only a configuration change (anchor texts, tool mappings) without modifying classification pipeline code

### Requirement 3: Tool Affinity Mapping

**User Story:** As a developer, I want the classifier to output which tools should be activated for a given intent, so that the Router no longer needs its own keyword-to-tool mapping.

#### Acceptance Criteria

1. THE UIC SHALL maintain a declarative Tool_Affinity registry mapping each Intent_Label to a set of tool names
2. WHEN the primary Intent_Label is `ssh`, THE Tool_Affinity SHALL include `ssh`
3. WHEN the primary Intent_Label is `search`, THE Tool_Affinity SHALL include `web_search`
4. WHEN the primary Intent_Label is `document_delivery`, THE Tool_Affinity SHALL include `send_file`, `open`, `craft_tool`
5. WHEN the primary Intent_Label is `browser`, THE Tool_Affinity SHALL include all browser automation tool names
6. WHEN the primary Intent_Label is `office`, THE Tool_Affinity SHALL include `office`
7. WHEN the primary Intent_Label is `coding` or `maintenance`, THE Tool_Affinity SHALL include `generate_pdf`, `office`
8. THE Tool_Affinity registry SHALL be editable without modifying classification pipeline code

### Requirement 4: Three-Layer Classification Pipeline

**User Story:** As a developer, I want the classifier to use a fast-path for obvious intents and escalate to semantic/LLM layers only when needed, so that latency stays low for common cases.

#### Acceptance Criteria

1. THE UIC SHALL execute Layer_1 (keyword rules) first for every classification request
2. WHEN Layer_1 produces a Classification_Result with confidence ≥ 0.90, THE UIC SHALL return the result without escalating to Layer_2
3. WHEN Layer_1 confidence is below 0.90, THE UIC SHALL escalate to Layer_2 (embedding cosine similarity)
4. WHEN Layer_2 produces a Classification_Result with confidence ≥ 0.78 and score gap ≥ 0.10 between top-1 and top-2 categories, THE UIC SHALL return the result without escalating to Layer_3
5. WHEN Layer_2 is ambiguous (confidence < 0.78 or gap < 0.10), THE UIC SHALL escalate to Layer_3 (LLM refinement)
6. THE Layer_3 LLM call SHALL enforce a configurable timeout, defaulting to 8 seconds (third-party API providers like 智谱 GLM often have higher latency than Anthropic; 3 seconds is too aggressive and causes frequent timeouts)
7. IF the Layer_3 LLM call times out or fails, THEN THE UIC SHALL fall back to the best available Layer_2 result, or Layer_1 result if Layer_2 is unavailable
8. THE Classification_Result SHALL record which layer produced the final answer

### Requirement 5: Layer 1 — Unified Keyword Rules

**User Story:** As a developer, I want a single consolidated keyword rule set that replaces all 6+ scattered keyword lists, so that keyword conflicts and duplications are eliminated.

#### Acceptance Criteria

1. THE UIC Layer_1 SHALL consolidate keywords from `router.go` (sshIntentKeywords, searchIntentKeywords, documentDeliveryKeywords, browserIntentKeywords, browserPageKeywords, browserActionKeywords, excelKeywords, pptxReadKeywords, codingWorkflowDocKeywords), `im_intent_classifier.go` (sshKeywords, ambiguousKeywords, codingKeywords, nonCodingKeywords), `im_tools_session.go` (nonCodingKeywords, codingKeywords, codingActionPhrases), `coding_tool_gate.go` (skipSignals, bugFixKeywords, creationCodingKeywords), `capability_gap_detector.go` (gapKeywords), and `gate_intent_classifier.go` (gateContPhrases, gateMaintenanceKeywords) into a single keyword registry
2. WHEN the same keyword appears in multiple legacy lists with different intent mappings, THE UIC SHALL resolve the conflict using a priority order: ssh > browser (strong) > coding > non_coding > ambiguous
3. THE UIC Layer_1 SHALL apply mixed-intent dominance rules: creation keywords dominate bug-fix keywords; bug-fix keywords dominate maintenance keywords; non-coding primary action dominates coding context words
4. THE UIC Layer_1 SHALL detect browser intent using two tiers: strong keywords (浏览器/browser/chrome/playwright) produce high confidence; weak keyword combinations (page + action words) produce low confidence requiring Layer_2 confirmation

### Requirement 6: Layer 2 — Embedding Cosine Similarity

**User Story:** As a developer, I want the classifier to use the already-loaded embedding model for semantic similarity, so that ambiguous messages are classified without LLM latency.

#### Acceptance Criteria

1. THE UIC Layer_2 SHALL use the existing local embedding model (loaded at startup) to compute query embeddings
2. THE UIC Layer_2 SHALL maintain pre-computed Anchor_Sets for each Intent_Label, warmed up at startup via background goroutine
3. THE UIC Layer_2 SHALL use the existing QueryEmbeddingCache to avoid redundant embedding computations
4. WHEN the embedding model is not available (NoopEmbedder), THE UIC SHALL skip Layer_2 and escalate directly from Layer_1 to Layer_3
5. THE UIC Layer_2 SHALL compute max cosine similarity between the query embedding and each Anchor_Set, producing a score per Intent_Label

### Requirement 7: Layer 3 — LLM Refinement

**User Story:** As a developer, I want an LLM fallback for truly ambiguous messages, so that edge cases are handled correctly without growing keyword lists.

#### Acceptance Criteria

1. THE UIC Layer_3 SHALL send the user message to the configured LLM with a unified system prompt covering all Intent_Labels
2. THE UIC Layer_3 SHALL request structured JSON output containing intent, confidence, reason, and evidence fields
3. WHEN the LLM returns confidence < 0.60, THE UIC SHALL treat the result as `ambiguous`
4. IF the LLM is not configured (empty URL or model), THEN THE UIC SHALL skip Layer_3 and use the best available lower-layer result
5. THE UIC Layer_3 system prompt SHALL include disambiguation rules for known confusing cases (e.g., "更新" as software update vs document update, "页面"+"打开" as browser vs game description)

### Requirement 8: Consumer Migration — Router (corelib/tool/router.go)

**User Story:** As a developer, I want the Router to consume UIC results instead of maintaining its own keyword lists, so that tool selection is driven by unified classification.

#### Acceptance Criteria

1. WHEN the Router performs tool selection, THE Router SHALL obtain the Classification_Result from the UIC instead of evaluating `conditionalKeepRules` keyword matches
2. THE Router SHALL use the Tool_Affinity set from the Classification_Result to determine which conditional tools to include
3. THE Router SHALL preserve the `needsSemanticConfirm` mechanism for browser weak signals by checking the UIC confidence and layer
4. THE Router SHALL continue to perform Eager_Pin for matched conditional tools using the UIC result
5. WHEN the UIC is not available (nil), THE Router SHALL fall back to the existing `conditionalKeepRules` keyword matching

### Requirement 9: Consumer Migration — IMIntentClassifier (gui/im_intent_classifier.go)

**User Story:** As a developer, I want `classifyTaskIntent()` to delegate to the UIC, so that the duplicated sshKeywords/codingKeywords/nonCodingKeywords lists are eliminated.

#### Acceptance Criteria

1. WHEN `classifyTaskIntent()` is called, THE IMIntentClassifier SHALL map the UIC Classification_Result to the existing `taskIntentResult` structure (intentCoding/intentSSH/intentNonCoding/intentAmbiguous)
2. THE mapping SHALL preserve the existing `taskIntentResult` fields: Intent, Matched, Evidence, Reason, Confidence, Source
3. WHEN the UIC primary Intent_Label is `coding`, `bug_fix`, or `maintenance`, THE IMIntentClassifier SHALL map to `intentCoding`
4. WHEN the UIC primary Intent_Label is `ssh`, THE IMIntentClassifier SHALL map to `intentSSH`
5. WHEN the UIC primary Intent_Label is `non_coding`, `browser`, `search`, `document_delivery`, or `office`, THE IMIntentClassifier SHALL map to `intentNonCoding`
6. WHEN the UIC primary Intent_Label is `ambiguous` or `unknown`, THE IMIntentClassifier SHALL map to `intentAmbiguous`

### Requirement 10: Consumer Migration — Session Guard (gui/im_tools_session.go)

**User Story:** As a developer, I want the session creation guard to use UIC results, so that the duplicated nonCodingKeywords/codingKeywords lists are eliminated.

#### Acceptance Criteria

1. WHEN `checkSessionTaskGuard()` evaluates whether to allow session creation, THE SessionGuard SHALL use the UIC Classification_Result instead of calling `classifyTaskIntent()` with local keyword lists
2. THE SessionGuard SHALL allow session creation when the UIC primary Intent_Label is `coding`, `bug_fix`, or `maintenance`
3. THE SessionGuard SHALL block session creation when the UIC primary Intent_Label is `non_coding`, `search`, `document_delivery`, or `office`
4. THE SessionGuard SHALL preserve the context-aware continuation logic: when the UIC returns `continuation` or `ambiguous` and conversation history contains coding context, session creation SHALL be allowed

### Requirement 11: Consumer Migration — Coding Tool Gate (gui/coding_tool_gate.go)

**User Story:** As a developer, I want the Coding Tool Gate to use UIC results for bug-fix detection and skip-signal detection, so that the duplicated bugFixKeywords/creationCodingKeywords/skipSignals lists are eliminated.

#### Acceptance Criteria

1. WHEN the Coding Tool Gate evaluates whether to activate the three-phase workflow, THE Gate SHALL use the UIC Classification_Result
2. WHEN the UIC primary Intent_Label is `bug_fix`, THE Gate SHALL not activate (bypass three-phase workflow)
3. WHEN the UIC primary Intent_Label is `continuation` with high confidence, THE Gate SHALL not activate
4. WHEN the UIC primary Intent_Label is `coding` and the UIC detects creation-oriented signals, THE Gate SHALL activate the three-phase workflow
5. THE Gate SHALL preserve skip-signal detection (直接做/不用问了/开工 etc.) as a separate fast-path check independent of the UIC

### Requirement 12: Consumer Migration — GateIntentClassifier (gui/gate_intent_classifier.go)

**User Story:** As a developer, I want the GateIntentClassifier to be replaced by the UIC, so that the five-category gate classification uses the unified pipeline instead of a separate three-layer pipeline.

#### Acceptance Criteria

1. THE UIC Intent_Label taxonomy SHALL be a superset of the GateIntentClassifier categories (new_project, bug_fix, maintenance, non_coding, continuation)
2. WHEN the GateIntentClassifier `Classify()` method is called, THE GateIntentClassifier SHALL delegate to the UIC and map the Classification_Result to `GateIntentResult`
3. THE mapping SHALL preserve the `GateIntentResult` fields: Intent, Confidence, Gap, Layer, Reason, AllScores
4. THE UIC SHALL reuse the existing gate anchor texts as part of its Layer_2 Anchor_Sets for the corresponding Intent_Labels

### Requirement 13: Consumer Migration — CapabilityGapDetector (gui/capability_gap_detector.go)

**User Story:** As a developer, I want the capability gap detector to use UIC signals instead of its own keyword list, so that the gapKeywords list is eliminated.

#### Acceptance Criteria

1. WHEN the CapabilityGapDetector evaluates LLM response text, THE Detector SHALL check whether the UIC Classification_Result (computed for the original user message) indicated a non-coding or ambiguous intent before applying gap detection
2. THE CapabilityGapDetector SHALL retain its own LLM-based gap detection logic (this is response analysis, not user intent classification)
3. THE CapabilityGapDetector SHALL remove the `gapKeywords` fallback and rely solely on LLM-based detection when an LLM is configured
4. IF no LLM is configured, THEN THE CapabilityGapDetector SHALL fall back to the existing `gapKeywords` as a last resort

### Requirement 14: Graceful Degradation

**User Story:** As a developer, I want the system to continue working when the embedding model or LLM is unavailable, so that the classifier never blocks user interaction.

#### Acceptance Criteria

1. IF the embedding model is a NoopEmbedder, THEN THE UIC SHALL operate with Layer_1 only (keyword rules) and Layer_3 (LLM) when available
2. IF the LLM is not configured, THEN THE UIC SHALL operate with Layer_1 and Layer_2 only
3. IF both the embedding model and LLM are unavailable, THEN THE UIC SHALL operate with Layer_1 only
4. WHEN operating in degraded mode, THE Classification_Result SHALL indicate the highest available layer in its `Layer` field
5. THE UIC SHALL log a warning at startup indicating which layers are available

### Requirement 15: Performance Constraints

**User Story:** As a developer, I want the classifier to meet strict latency requirements, so that it does not slow down message processing.

#### Acceptance Criteria

1. THE UIC Layer_1 classification SHALL complete within 1 millisecond
2. THE UIC Layer_2 classification SHALL complete within 10 milliseconds (including cache lookup)
3. THE UIC Layer_3 classification SHALL enforce a configurable hard timeout, defaulting to 8 seconds (accommodates third-party API providers with higher latency)
4. THE UIC total classification time (all layers combined) SHALL not exceed 9 seconds in the worst case
5. THE UIC Anchor_Set warmup SHALL complete in a background goroutine without blocking application startup

### Requirement 16: Backward Compatibility

**User Story:** As a developer, I want the migration to be incremental and reversible, so that existing behavior is preserved during the transition.

#### Acceptance Criteria

1. WHEN the UIC is not initialized (nil), each Consumer SHALL fall back to its existing keyword-based classification logic
2. THE UIC SHALL produce classification results that are compatible with the existing `taskIntentResult`, `GateIntentResult`, and `conditionalKeepRules` output formats through adapter methods
3. THE migration SHALL not change the public API signatures of `classifyTaskIntent()`, `checkSessionTaskGuard()`, `GateIntentClassifier.Classify()`, or `Router.Route()`
4. THE UIC SHALL be injectable into each Consumer via a setter method (e.g., `SetUnifiedClassifier(uic *UnifiedIntentClassifier)`)

### Requirement 17: Observability and Diagnostics

**User Story:** As a developer, I want to see classification decisions and layer escalation in logs, so that I can debug misclassification issues.

#### Acceptance Criteria

1. THE UIC SHALL log each classification decision with: user text (truncated to 30 characters), final Intent_Label, confidence, layer, and reason
2. WHEN Layer escalation occurs (Layer_1 → Layer_2 or Layer_2 → Layer_3), THE UIC SHALL log the escalation with the lower layer's result
3. WHEN Layer_3 overrides a Layer_2 result, THE UIC SHALL log both results for comparison
4. THE UIC SHALL expose a `DiagnoseScores(text string) map[IntentLabel]float64` method for debugging that returns all Layer_2 scores without side effects
5. THE UIC SHALL use the log prefix `[UnifiedIntentClassifier]` for all log messages

### Requirement 18: Keyword Registry as Data, Not Code

**User Story:** As a developer, I want keywords to be declared as structured data rather than scattered across code files, so that adding or modifying keywords does not require understanding classification logic.

#### Acceptance Criteria

1. THE UIC SHALL define all keywords in a single registry data structure, organized by Intent_Label
2. Each keyword entry in the registry SHALL specify: the keyword string, the associated Intent_Label, and a strength indicator (strong/weak)
3. WHEN a keyword is marked as `weak`, THE UIC Layer_1 SHALL require additional signals (another weak keyword from the same Intent_Label, or Layer_2 confirmation) before producing a high-confidence result
4. THE keyword registry SHALL be defined in a single Go source file separate from the classification pipeline logic
