# Implementation Plan: TUI Knowledge Base Integration

## Overview

This plan implements knowledge base capabilities for the TUI version of MaClaw. The implementation reuses the existing `corelib/knowledge.SQLiteStore` and `corelib/agentservice/knowledge_integration.go` components, wiring them into the TUI's agent loop callbacks and CLI command infrastructure. The work is organized into: (1) store initialization and auto-recall, (2) knowledge tool registration, (3) CLI command group, and (4) system prompt integration.

## Tasks

- [x] 1. Knowledge Store Initialization and Auto-Recall
  - [x] 1.1 Add knowledgeStore field to TUIApp and implement initKnowledgeStore
    - Add `knowledgeStore *knowledge.SQLiteStore` field to `TUIApp` struct in `tui/app.go`
    - Implement `initKnowledgeStore(dataDir string)` method that checks for `~/.maclaw/knowledge.db` existence, opens the store if present, logs warning on failure, and leaves nil if DB doesn't exist
    - Call `initKnowledgeStore` during `runTUI` startup after dataDir is resolved
    - _Requirements: 1.5, 1.6, 1.7_

  - [x] 1.2 Implement appendKnowledgeAutoRecall method on TUIApp
    - Implement `appendKnowledgeAutoRecall(b *strings.Builder, userMsg string)` method on TUIApp
    - Truncate user message to 200 runes for the FTS query
    - Execute search with 3-second context timeout
    - Filter results by score threshold 0.3
    - Inject up to 3 snippets under "知识库参考（自动检索）" section header
    - Skip entirely when knowledgeStore is nil
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.7_

  - [x] 1.3 Wire auto-recall into buildSystemPromptDeps
    - In `buildSystemPromptDeps()`, set `HasKnowledgeBase: app.knowledgeStore != nil` in `SystemPromptDeps.Config`
    - When knowledgeStore is non-nil, set `KnowledgeAutoRecall` callback to invoke `appendKnowledgeAutoRecall`
    - _Requirements: 1.1, 7.1_

  - [x]* 1.4 Write property tests for auto-recall (Properties 1, 2, 3)
    - **Property 1: Auto-recall injection respects threshold and count limit**
    - **Validates: Requirements 1.1, 1.4**
    - **Property 2: Query truncation preserves short messages and caps long ones**
    - **Validates: Requirements 1.3**
    - **Property 3: HasKnowledgeBase reflects store initialization state**
    - **Validates: Requirements 1.5, 7.1**
    - Place tests in `tui/knowledge_autorecall_test.go`
    - Use `testing/quick` with minimum 100 iterations per property

- [x] 2. Knowledge Tools Registration
  - [x] 2.1 Implement knowledge tool handler methods on TUIApp
    - Implement `toolKnowledgeSearch(args map[string]interface{}) (string, error)` — FTS search against knowledge store
    - Implement `toolKnowledgeContextPack(args map[string]interface{}) (string, error)` — build citation-backed context bundle
    - Implement `toolKnowledgeSaveText(args map[string]interface{}) (string, error)` — persist text as new source
    - Implement `toolKnowledgeSaveURL(args map[string]interface{}) (string, error)` — fetch URL and persist as source
    - All handlers return descriptive error when knowledgeStore is nil
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6_

  - [x] 2.2 Register knowledge tools via CoreToolDeps.ExtraHandlers
    - Add `knowledge_search`, `knowledge_context_pack`, `knowledge_save_text`, `knowledge_save_url` to the ExtraHandlers map in tool setup
    - Register corresponding ToolEntry definitions with proper descriptions and parameter schemas
    - _Requirements: 2.1_

  - [x]* 2.3 Write unit tests for knowledge tool handlers
    - Test each handler with nil store returns descriptive error
    - Test knowledge_save_text with empty text returns error
    - Test knowledge_search delegates to store correctly
    - Place tests in `tui/knowledge_tools_test.go`
    - _Requirements: 2.2, 2.3, 2.4, 2.5, 2.6_

- [x] 3. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 4. Knowledge CLI Command Group — Core Structure
  - [x] 4.1 Create tui/commands/knowledge.go with RunKnowledge dispatcher
    - Implement `RunKnowledge(args []string, dataDir string) error` function
    - Dispatch to subcommands: import, list, search, status, delete, clear
    - Return usage error for unknown commands or empty args
    - _Requirements: 3.1, 4.1, 6.1, 6.2, 6.3, 6.4, 6.6_

  - [x] 4.2 Implement knowledgeImport for file and directory paths
    - Parse flags: `--project`, `--labels`, `--scope`, `--include-exts`, `--dry-run`, `--json`
    - Detect file vs directory for each path argument
    - For files: call `store.ImportFiles` with options
    - For directories: call `store.ImportDirectory` with `DirectoryImportRequest`
    - Process multiple paths sequentially, report per-file results
    - Print summary on success (file name, nodes created, source ID)
    - Handle errors: file not found (exit 1), unsupported format (exit 1 with supported list)
    - Skip duplicates via content hash deduplication
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 5.1, 5.2, 5.3, 5.4, 5.5_

  - [x] 4.3 Implement knowledgeList, knowledgeSearch, knowledgeStatus
    - `knowledgeList`: print table with columns ID, Kind, Title/Path, Status, Nodes, Cards, Updated
    - `knowledgeSearch`: perform FTS search, print ranked results with source, snippet, score
    - `knowledgeStatus`: print stats — total sources, nodes, cards, facts, database size
    - _Requirements: 6.1, 6.2, 6.3_

  - [x] 4.4 Implement knowledgeDelete and knowledgeClear
    - `knowledgeDelete`: remove source and all its nodes/cards/facts; prompt for confirmation without `--force`
    - `knowledgeClear`: remove all sources with `--force` flag required
    - _Requirements: 6.4, 6.5, 6.6_

  - [x] 4.5 Implement --json output mode for import commands
    - Define `knowledgeImportResult` and `knowledgeImportSummary` structs
    - When `--json` flag is active, output valid JSON instead of human-readable text
    - _Requirements: 5.6_

  - [x]* 4.6 Write property tests for CLI formatters (Properties 4-9)
    - **Property 4: File import success summary contains all required fields**
    - **Validates: Requirements 3.3**
    - **Property 5: Directory import summary contains all statistics**
    - **Validates: Requirements 4.3**
    - **Property 6: Multiple file import processes all paths sequentially**
    - **Validates: Requirements 3.1, 3.6**
    - **Property 7: JSON output mode produces valid structured JSON**
    - **Validates: Requirements 5.6**
    - **Property 8: CLI list output contains all required columns per source**
    - **Validates: Requirements 6.1**
    - **Property 9: CLI search output contains score, source, and snippet per result**
    - **Validates: Requirements 6.2**
    - Place tests in `tui/commands/knowledge_test.go`
    - Use `testing/quick` with minimum 100 iterations per property

- [x] 5. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 6. System Prompt Knowledge Base Rules and Wiring
  - [x] 6.1 Update TUI system prompt builder for knowledge base rules
    - When `HasKnowledgeBase: true`, include rules instructing LLM to use `knowledge_search` or `knowledge_context_pack` for deeper retrieval
    - Include rules instructing LLM to use `knowledge_save_text` or `knowledge_save_url` only when user explicitly asks
    - _Requirements: 7.1, 7.2, 7.3_

  - [x] 6.2 Wire knowledge CLI into TUI main command dispatcher
    - Register `knowledge` subcommand in the TUI's main command routing (similar to `memory` subcommand)
    - Ensure `maclaw-tui knowledge <subcommand>` is accessible from the terminal
    - _Requirements: 3.1, 4.1, 6.1_

  - [x]* 6.3 Write integration tests
    - End-to-end: import a real .txt file → search for content → verify result
    - End-to-end: import directory with mixed formats → verify summary counts
    - System prompt with HasKnowledgeBase=true contains knowledge tool guidance
    - _Requirements: 1.1, 3.1, 7.1_

- [x] 7. Final checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties from the design document
- Unit tests validate specific examples and edge cases
- The implementation reuses existing `corelib/knowledge` and `corelib/agentservice` components — no new knowledge logic is implemented in TUI
- The shared `~/.maclaw/knowledge.db` database path is consistent with the GUI

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "4.1"] },
    { "id": 1, "tasks": ["1.2", "2.1", "4.2"] },
    { "id": 2, "tasks": ["1.3", "2.2", "4.3", "4.4", "4.5"] },
    { "id": 3, "tasks": ["1.4", "2.3", "4.6"] },
    { "id": 4, "tasks": ["6.1", "6.2"] },
    { "id": 5, "tasks": ["6.3"] }
  ]
}
```
