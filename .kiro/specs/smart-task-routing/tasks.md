# Implementation Plan: Smart Task Routing

## Overview

Incrementally build the TaskClassifier and routing rules into iWorkerCenter's existing `handleChatCompletions` path. Each task adds a discrete piece of functionality, wires it into the previous step, and includes property/unit tests to catch regressions early. All code lives in `iWorkerCenter/` (package main), alongside `server.go`.

## Tasks

- [x] 1. Define data types and constants
  - [x] 1.1 Create `iWorkerCenter/task_classifier.go` with Work Type constants, Cost Tier constants, `ClassifyInput`, and `ClassificationResult` structs
    - Define `WorkTypeDocumentWriting`, `WorkTypeDataAnalysis`, `WorkTypeQualityReport`, `WorkTypeProductionReport`, `WorkTypeTableFormatting`, `WorkTypeLongTextSummary`, `WorkTypeSimpleQA`
    - Define `CostTierHigh`, `CostTierMedium`, `CostTierLow`
    - Define `ClassifyInput` struct with `TaskType`, `MessageContent`, `ColleagueName` fields
    - Define `ClassificationResult` struct with `WorkType`, `CostTier`, `Latency`, `Method` fields
    - _Requirements: 1.1, 1.4, 2.4_

  - [x] 1.2 Create `iWorkerCenter/routing_rules.go` with `RoutingRules` struct, `DefaultRoutingRules()`, `MergeWithDefaults()`, and `LookupTier()`
    - Define `RoutingRules` struct with `WorkTypeKeywords`, `WorkTypeTier`, `RoleProviderBoost`, `DefaultWorkType`, `DefaultCostTier`
    - Implement `DefaultRoutingRules()` returning all 7 built-in work types with their keyword lists and tier mappings
    - Implement `MergeWithDefaults()` to fill nil/empty maps from defaults
    - Implement `LookupTier(workType)` returning the tier or `"medium"` fallback
    - _Requirements: 2.1, 2.2, 2.3, 6.3_

  - [ ]* 1.3 Write property tests for RoutingRules
    - **Property 5: LookupTier always returns a valid tier with medium as default**
    - **Property 11: MergeWithDefaults fills missing routing rule fields**
    - **Validates: Requirements 2.1, 2.3, 6.3**
    - Place in `iWorkerCenter/routing_rules_property_test.go` using `pgregory.net/rapid`

  - [ ]* 1.4 Write unit tests for DefaultRoutingRules
    - Verify all 7 work types present in `WorkTypeKeywords`
    - Verify default tier mappings match design table
    - Place in `iWorkerCenter/routing_rules_test.go`
    - _Requirements: 1.4, 2.2_

- [x] 2. Implement classification logic
  - [x] 2.1 Implement `Classify()`, `classifyByTaskType()`, and `classifyByKeywords()` in `task_classifier.go`
    - `classifyByTaskType`: check if `TaskType` matches any keyword in `WorkTypeKeywords`, return matched work type
    - `classifyByKeywords`: scan `MessageContent` against all keyword lists, return work type with most hits
    - `Classify`: orchestrate the flow — try task_type match first, then keyword match, then default to `simple_qa`; record `Latency` and `Method`
    - _Requirements: 1.1, 1.2, 1.3, 1.5_

  - [ ]* 2.2 Write property test for classification invariant
    - **Property 1: Classification always returns a valid Work Type**
    - **Validates: Requirements 1.1**
    - Place in `iWorkerCenter/task_classifier_property_test.go`

  - [ ]* 2.3 Write property test for task_type match
    - **Property 2: Explicit task_type match selects the correct Work Type**
    - **Validates: Requirements 1.2**

  - [ ]* 2.4 Write property test for keyword match path
    - **Property 3: Absent task_type triggers keyword-based classification**
    - **Validates: Requirements 1.3**

  - [ ]* 2.5 Write property test for default fallback
    - **Property 4: No keyword match defaults to simple_qa**
    - **Validates: Requirements 1.5**

  - [ ]* 2.6 Write unit tests for Classify function
    - Test explicit task_type matching with known keywords
    - Test keyword-based classification with sample messages
    - Test default fallback with unrelated content
    - Test empty input handling
    - Place in `iWorkerCenter/task_classifier_test.go`
    - _Requirements: 1.2, 1.3, 1.5, 1.6_

- [x] 3. Checkpoint
  - Ensure all tests pass, ask the user if questions arise.

- [x] 4. Extend provider model and settings with CostTier
  - [x] 4.1 Add `CostTier` field to `CenterProvider` and `centerProviderFile` structs in `server.go`
    - Add `CostTier string` to `CenterProvider`
    - Add `CostTier string \`json:"cost_tier"\`` to `centerProviderFile`
    - Update `normalizeCenterProviders` to copy `CostTier` and default to `"medium"` when empty
    - Update `normalizeCenterProviderFiles` to default empty `CostTier` to `"medium"`
    - Update `defaultCenterProviders` and `defaultCenterProviderFiles` to include `CostTier`
    - _Requirements: 2.4, 6.5_

  - [x] 4.2 Extend `centerSettingsFile` with routing rule fields
    - Add `WorkTypeKeywords map[string][]string \`json:"work_type_keywords,omitempty"\``
    - Add `WorkTypeTier map[string]string \`json:"work_type_tier,omitempty"\``
    - Add `RoleProviderBoost map[string][]string \`json:"role_provider_boost,omitempty"\``
    - Update `readCenterSettings` to parse new fields
    - Update `loadCenterProviders` to also return parsed `RoutingRules`
    - _Requirements: 6.1, 6.2, 6.4_

  - [ ]* 4.3 Write property test for provider cost_tier normalization
    - **Property 12: Provider cost_tier normalization defaults to medium**
    - **Validates: Requirements 6.5**
    - Place in `iWorkerCenter/server_property_test.go`

- [x] 5. Implement tier-aware provider ranking
  - [x] 5.1 Implement `rankProvidersWithTier()` method on `centerServer`
    - Filter enabled providers by matching `CostTier`
    - Apply role boost: if `roleBoost[roleCode]` contains provider ID, add bonus to score
    - Sort by score descending (Priority + feature match + role boost)
    - If no providers match tier, fall back to existing `rankProviders` result
    - _Requirements: 2.5, 3.1, 3.2, 4.2, 4.3_

  - [ ]* 5.2 Write property test for tier filtering correctness
    - **Property 6: Tier filtering returns correctly filtered and priority-sorted providers**
    - **Validates: Requirements 2.5, 3.1**

  - [ ]* 5.3 Write property test for tier fallback
    - **Property 7: Fallback to all providers when no tier match exists**
    - **Validates: Requirements 3.2**

  - [ ]* 5.4 Write property test for role boost tiebreaker
    - **Property 8: Role boost affects ranking within the same Cost Tier**
    - **Validates: Requirements 4.2**

  - [ ]* 5.5 Write property test for tier > role precedence
    - **Property 9: Cost Tier takes precedence over Role preference**
    - **Validates: Requirements 4.3**

  - [ ]* 5.6 Write unit tests for rankProvidersWithTier
    - Test tier filtering with mixed-tier providers
    - Test fallback when no tier matches
    - Test explicit provider ID bypass via `model` field
    - Test role boost ordering within same tier and priority
    - _Requirements: 3.1, 3.2, 3.4, 4.2_

- [x] 6. Checkpoint
  - Ensure all tests pass, ask the user if questions arise.

- [x] 7. Implement audit logging and integrate into handleChatCompletions
  - [x] 7.1 Add audit log formatting function in `task_classifier.go`
    - Implement `FormatTaskRouteLog(result ClassificationResult, reqID string, providerID string, summary string) string`
    - Format: `[TaskRoute] ts=... req_id=... work_type=... cost_tier=... provider=... latency_ms=... method=... summary="..."`
    - Truncate summary to first 200 characters
    - _Requirements: 5.1, 5.2_

  - [ ]* 7.2 Write property test for audit log format
    - **Property 10: Audit log format contains all required fields**
    - **Validates: Requirements 5.1, 5.2**

  - [x] 7.3 Integrate classification into `handleChatCompletions`
    - After request parsing, build `ClassifyInput` from request body (extract `task_type` field and message content)
    - Load `RoutingRules` from settings (via `readCenterSettings` + `MergeWithDefaults`)
    - Call `Classify(input, rules)` with a 10ms timeout guard
    - Log `[TaskRoute]` audit entry via `log.Printf`
    - Replace `s.rankProviders(req)` call with `s.rankProvidersWithTier(req, result.CostTier, rules.RoleProviderBoost, roleCode)`
    - Preserve existing fallback: if classification aborts (timeout), use `rankProviders`
    - _Requirements: 1.1, 1.6, 3.4, 5.1, 5.2, 5.3, 7.1, 7.2, 7.3_

  - [ ]* 7.4 Write unit tests for handleChatCompletions integration
    - Test that classification result affects provider selection
    - Test that explicit `model` field bypasses classification
    - Test that malformed body returns 400 without classification
    - Test that classification timeout falls back to rankProviders
    - _Requirements: 1.6, 3.4, 7.3_

- [x] 8. Final checkpoint
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- All code is in `iWorkerCenter/` directory, package `main`
- Property tests use `pgregory.net/rapid` library
- Each property test references a specific property from the design document
- Checkpoints ensure incremental validation
- The existing `rankProviders` method is preserved for backward compatibility and fallback
