# Requirements Document

## Introduction

The Coding Tool Gate currently uses hardcoded keyword lists (`codingKeywords`, `bugFixKeywords`, `creationCodingKeywords`, `nonCodingKeywords`, etc.) in `gui/coding_tool_gate.go` and `gui/im_intent_classifier.go` to decide whether to activate the three-phase coding workflow. This approach is fragile and inaccurate: it misclassifies bug-fix tasks as new-project tasks, fails on novel phrasings, and requires manual keyword additions for every new edge case.

This feature replaces the keyword-based gate decision with a semantic/LLM-driven approach, leveraging the existing `corelib/tool/IntentClassifier` (embedding-based, three-layer classification) and `classifyTaskIntentWithLLM()` infrastructure. The gate must accurately distinguish five task categories: new project creation, bug fix/debugging, code maintenance/refactoring, non-coding tasks, and continuation of existing work.

## Glossary

- **Gate**: The `codingToolGateConfig` decision mechanism that determines whether the three-phase coding workflow (requirements → design → task breakdown) is activated for a given user message. Once computed, the gate decision is immutable for the duration of the agent loop.
- **Three_Phase_Workflow**: The mandatory coding workflow consisting of requirements document → technical design → task breakdown, enforced by the Gate when activated.
- **Semantic_Classifier**: The upgraded classification component that uses vector embeddings and/or LLM calls instead of keyword matching to determine task intent for the Gate decision.
- **IntentClassifier**: The existing `corelib/tool/IntentClassifier` struct that performs three-layer classification: Layer 1 (regex/rules), Layer 2 (embedding cosine similarity against anchor texts), Layer 3 (optional LLM callback).
- **Gate_Intent**: The five-category classification result specific to the Gate decision: `new_project`, `bug_fix`, `maintenance`, `non_coding`, `continuation`.
- **Anchor_Text**: Pre-computed example sentences for each Gate_Intent category, used as reference points for embedding cosine similarity scoring.
- **Confidence_Score**: A float64 value in [0, 1] representing the classifier's certainty in its classification result.
- **Keyword_Fallback**: The existing keyword-based classification logic retained as a fast-path and safety net when the Semantic_Classifier is unavailable or returns low confidence.
- **Conversation_Context**: The recent conversation history (up to 10 messages) used to disambiguate short continuation phrases like "开工" or "继续".
- **LLM_Refinement**: An optional LLM call used to resolve ambiguous classifications when embedding similarity falls in the uncertain zone.
- **classifyTaskIntent**: The existing function in `gui/im_intent_classifier.go` that performs keyword-based intent classification into `coding`/`ssh`/`non_coding`/`ambiguous`/`unknown`.
- **classifyTaskIntentWithLLM**: The existing function that calls an LLM for intent classification with structured JSON output, used as a fallback when keyword classification is ambiguous.

## Requirements

### Requirement 1: Five-Category Gate Intent Classification

**User Story:** As a user, I want the Gate to accurately classify my message into one of five task categories, so that new project tasks go through the three-phase workflow while bug fixes, maintenance, and non-coding tasks execute directly.

#### Acceptance Criteria

1. WHEN a user message describes creating a new application, game, tool, or system (e.g. "开发一个贪吃蛇游戏", "写一个爬虫", "build a REST API"), THE Semantic_Classifier SHALL return Gate_Intent `new_project` with Confidence_Score ≥ 0.70.
2. WHEN a user message describes fixing a bug, debugging, or troubleshooting (e.g. "有bug，一直显示加载中", "修复崩溃", "debug this crash"), THE Semantic_Classifier SHALL return Gate_Intent `bug_fix` with Confidence_Score ≥ 0.70.
3. WHEN a user message describes refactoring, optimization, or code maintenance (e.g. "重构这个函数", "优化性能", "clean up this module"), THE Semantic_Classifier SHALL return Gate_Intent `maintenance` with Confidence_Score ≥ 0.70.
4. WHEN a user message describes a non-coding task (e.g. "翻译文档", "搜索论文", "总结这篇文章"), THE Semantic_Classifier SHALL return Gate_Intent `non_coding` with Confidence_Score ≥ 0.70.
5. WHEN a user message is a short continuation phrase (e.g. "继续", "开工", "go ahead") AND Conversation_Context contains coding-related messages, THE Semantic_Classifier SHALL return Gate_Intent `continuation` with Confidence_Score ≥ 0.60.
6. WHEN a user message is a short continuation phrase AND Conversation_Context does not contain coding-related messages, THE Semantic_Classifier SHALL return Gate_Intent with Confidence_Score < 0.50, indicating ambiguity.

### Requirement 2: Three-Layer Classification Architecture

**User Story:** As a developer, I want the Semantic_Classifier to use a layered approach (rules → embeddings → LLM) so that classification is fast for common cases and accurate for edge cases.

#### Acceptance Criteria

1. THE Semantic_Classifier SHALL first attempt classification using keyword rules (Layer 1) and return immediately when a high-confidence match is found (Confidence_Score ≥ 0.90).
2. WHEN Layer 1 returns Confidence_Score < 0.90, THE Semantic_Classifier SHALL compute embedding cosine similarity against Anchor_Text sets for each Gate_Intent category (Layer 2).
3. WHEN Layer 2 returns Confidence_Score ≥ 0.78 with a gap ≥ 0.10 between the top two categories, THE Semantic_Classifier SHALL return the top category without invoking LLM_Refinement.
4. WHEN Layer 2 returns Confidence_Score in the range [0.55, 0.78) or gap < 0.10, THE Semantic_Classifier SHALL invoke LLM_Refinement (Layer 3) to resolve the ambiguity.
5. WHEN Layer 2 returns Confidence_Score < 0.55, THE Semantic_Classifier SHALL invoke LLM_Refinement (Layer 3) to attempt classification.
6. IF LLM_Refinement is unavailable or times out, THEN THE Semantic_Classifier SHALL fall back to the Layer 2 result if available, or to Keyword_Fallback.

### Requirement 3: Gate-Specific Anchor Texts

**User Story:** As a developer, I want dedicated anchor text sets for the five Gate_Intent categories, so that embedding similarity accurately distinguishes between new project creation and bug fix/maintenance tasks.

#### Acceptance Criteria

1. THE Semantic_Classifier SHALL maintain separate Anchor_Text sets for each of the five Gate_Intent categories: `new_project`, `bug_fix`, `maintenance`, `non_coding`, `continuation`.
2. THE `new_project` Anchor_Text set SHALL contain at least 10 example sentences covering Chinese and English, including phrases like "开发一个贪吃蛇游戏", "写一个爬虫", "build a web application", "create a CLI tool".
3. THE `bug_fix` Anchor_Text set SHALL contain at least 10 example sentences covering Chinese and English, including phrases like "有bug，一直显示加载中", "修复崩溃", "fix the loading issue", "debug this crash".
4. THE `maintenance` Anchor_Text set SHALL contain at least 8 example sentences covering Chinese and English, including phrases like "重构这个函数", "优化性能", "refactor the auth module", "clean up dead code".
5. THE `non_coding` Anchor_Text set SHALL contain at least 8 example sentences covering Chinese and English, including phrases like "翻译文档", "搜索论文", "summarize this article", "帮我整理资料".
6. THE `continuation` Anchor_Text set SHALL contain at least 6 example sentences covering Chinese and English, including phrases like "继续", "开工", "开干", "let's go", "start working".
7. THE Semantic_Classifier SHALL pre-compute and cache Anchor_Text embeddings at initialization time in a background goroutine.

### Requirement 4: Latency Budget Compliance

**User Story:** As a user, I want the Gate decision to be made quickly so that I don't experience noticeable delays before the assistant starts responding.

#### Acceptance Criteria

1. WHEN Layer 1 (keyword rules) produces a high-confidence result, THE Semantic_Classifier SHALL return within 1 millisecond.
2. WHEN Layer 2 (embedding similarity) is required, THE Semantic_Classifier SHALL return within 500 milliseconds, including embedding computation for the user message.
3. WHEN Layer 3 (LLM_Refinement) is required, THE Semantic_Classifier SHALL enforce a timeout of 3 seconds on the LLM call.
4. IF the LLM call exceeds the 3-second timeout, THEN THE Semantic_Classifier SHALL return the best available result from Layer 1 or Layer 2 without waiting for the LLM response.
5. THE Semantic_Classifier SHALL use the existing `QueryEmbeddingCache` to avoid redundant embedding computations for repeated or similar queries.

### Requirement 5: Gate Decision Integration

**User Story:** As a developer, I want the Semantic_Classifier to integrate seamlessly with the existing `newCodingToolGateConfigWithClassifier()` function, so that the Gate decision uses semantic classification while preserving all existing bypass mechanisms (skip signals, background loop, etc.).

#### Acceptance Criteria

1. THE `newCodingToolGateConfigWithClassifier()` function SHALL use the Semantic_Classifier as the primary classification source when the IntentClassifier is available and ready.
2. WHEN the Semantic_Classifier returns Gate_Intent `new_project` with Confidence_Score ≥ 0.70, THE Gate SHALL activate (active=true).
3. WHEN the Semantic_Classifier returns Gate_Intent `bug_fix`, THE Gate SHALL not activate (active=false) and SHALL set bugFix=true.
4. WHEN the Semantic_Classifier returns Gate_Intent `maintenance`, THE Gate SHALL not activate (active=false).
5. WHEN the Semantic_Classifier returns Gate_Intent `non_coding`, THE Gate SHALL not activate (active=false).
6. WHEN the Semantic_Classifier returns Gate_Intent `continuation`, THE Gate SHALL not activate (active=false).
7. THE Gate SHALL continue to respect existing skip signals ("直接做", "不用问了", "just do it", etc.) regardless of the Semantic_Classifier result.
8. THE Gate SHALL continue to return active=false for LoopKindBackground regardless of the Semantic_Classifier result.
9. WHEN the Semantic_Classifier is unavailable (IntentClassifier is nil or not ready), THE Gate SHALL fall back to the existing keyword-based `classifyTaskIntent()` + `isBugFixOnly()` logic.

### Requirement 6: Conversation Context for Continuation Detection

**User Story:** As a user, I want the system to understand that "开工" means "start the previously discussed coding task" when I've been discussing a coding project, so that I don't get asked to rephrase my request.

#### Acceptance Criteria

1. WHEN the Semantic_Classifier receives a short message (≤ 4 Chinese characters or ≤ 10 English characters), THE Semantic_Classifier SHALL examine Conversation_Context to determine intent.
2. THE Semantic_Classifier SHALL scan the most recent 10 messages in Conversation_Context for coding-related signals (coding keywords, tool calls to create_session/bash/write_file, or previous Gate_Intent `new_project` results).
3. WHEN coding-related signals are found in Conversation_Context AND the short message matches known continuation phrases, THE Semantic_Classifier SHALL return Gate_Intent `continuation` with Confidence_Score ≥ 0.60.
4. WHEN no coding-related signals are found in Conversation_Context, THE Semantic_Classifier SHALL not classify the short message as `continuation` and SHALL defer to embedding/LLM layers.

### Requirement 7: LLM-Based Gate Refinement

**User Story:** As a developer, I want an LLM fallback for ambiguous cases so that the Gate makes accurate decisions even for novel phrasings that don't match any anchor texts.

#### Acceptance Criteria

1. THE LLM_Refinement SHALL use a structured JSON output format with fields: `gate_intent` (string), `confidence` (number), `reason` (string).
2. THE LLM_Refinement system prompt SHALL instruct the LLM to classify into exactly five categories: `new_project`, `bug_fix`, `maintenance`, `non_coding`, `continuation`.
3. THE LLM_Refinement SHALL use the existing `classifyTaskIntentWithLLM()` infrastructure (HTTP client, model config, protocol detection) with a modified system prompt specific to Gate classification.
4. WHEN LLM_Refinement returns confidence < 0.60, THE Semantic_Classifier SHALL treat the result as ambiguous and fall back to the Keyword_Fallback result.
5. IF LLM_Refinement returns an error or invalid JSON, THEN THE Semantic_Classifier SHALL fall back to the best available Layer 1/Layer 2 result.

### Requirement 8: Backward Compatibility and Gradual Rollout

**User Story:** As a developer, I want the semantic classification to be additive and non-breaking, so that existing keyword-based behavior is preserved as a fallback and the system degrades gracefully.

#### Acceptance Criteria

1. THE Semantic_Classifier SHALL not modify or remove any existing keyword lists (`codingKeywords`, `bugFixKeywords`, `creationCodingKeywords`, `nonCodingKeywords`).
2. WHEN the embedding model is not loaded or the IntentClassifier is not ready, THE Gate SHALL produce identical results to the current keyword-based implementation.
3. THE Semantic_Classifier SHALL log its classification decision (layer used, confidence, gate_intent) at debug level for observability.
4. THE Semantic_Classifier SHALL expose a `Ready()` method that returns true only after all Anchor_Text embeddings have been pre-computed.
5. WHEN the Semantic_Classifier overrides a keyword-based classification, THE Gate SHALL log both the keyword result and the semantic result for debugging.

### Requirement 9: Session Task Guard Integration

**User Story:** As a developer, I want the `checkSessionTaskGuard()` function to also benefit from semantic classification, so that session creation decisions are consistent with Gate decisions.

#### Acceptance Criteria

1. WHEN `classifyTaskIntent()` returns `intentAmbiguous` or `intentUnknown`, THE `checkSessionTaskGuard()` function SHALL consult the Semantic_Classifier before blocking session creation.
2. WHEN the Semantic_Classifier returns Gate_Intent `new_project` or `bug_fix` or `maintenance`, THE `checkSessionTaskGuard()` SHALL allow session creation (return empty string).
3. WHEN the Semantic_Classifier returns Gate_Intent `non_coding`, THE `checkSessionTaskGuard()` SHALL block session creation with an appropriate hint message.
4. THE `checkSessionTaskGuard()` SHALL continue to use the existing `hasCodingActionPhrase()` + `conversationHasCodingContext()` logic as an additional signal alongside the Semantic_Classifier.

### Requirement 10: Mixed-Intent Message Handling

**User Story:** As a user, I want the system to correctly handle messages that contain both creation and bug-fix signals, so that "开发一个bug追踪系统" activates the three-phase workflow while "修复这个bug然后重构代码" does not.

#### Acceptance Criteria

1. WHEN a user message contains both creation signals and bug-fix signals (e.g. "开发一个bug追踪系统"), THE Semantic_Classifier SHALL classify as `new_project` because the primary intent is creation.
2. WHEN a user message contains bug-fix signals combined with maintenance signals (e.g. "修复这个bug然后重构代码"), THE Semantic_Classifier SHALL classify as `bug_fix` because the primary action is fixing.
3. WHEN a user message contains coding signals combined with non-coding signals (e.g. "翻译这段代码的注释"), THE Semantic_Classifier SHALL classify as `non_coding` because the primary action is translation.
4. THE Semantic_Classifier SHALL use the embedding similarity scores across all five categories to determine the dominant intent, rather than relying on keyword presence/absence alone.

### Requirement 11: Observability and Diagnostics

**User Story:** As a developer, I want detailed logging and diagnostics for the Semantic_Classifier, so that I can debug misclassifications and tune anchor texts.

#### Acceptance Criteria

1. THE Semantic_Classifier SHALL log the classification path (Layer 1/2/3), the top-2 Gate_Intent categories with their Confidence_Scores, and the final decision for every classification call.
2. THE Semantic_Classifier SHALL record classification results in the existing `UsageTracker` infrastructure for offline analysis.
3. WHEN the Semantic_Classifier overrides the Keyword_Fallback result, THE log entry SHALL include both the keyword result and the semantic result with their respective confidence scores.
4. THE Semantic_Classifier SHALL expose a diagnostic method that returns the full scoring breakdown (all five categories with scores) for a given input text, usable in tests and debugging tools.
