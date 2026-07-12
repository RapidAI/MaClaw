# Implementation Plan

## Overview

Migrate the workflow engine from V1 (`corelib/workflow/engine.go` + 30+ GUI integration files) to V2 (`corelib/workflow/v2/` package). V2 uses a single-layer Router + StateMachine + PhaseExecutor architecture, replacing the 6-7 layer decision chain of V1. The migration is a direct replacement (no coexistence period). Phase 2 (T15-T17) eliminates the V2 Facade bridge layer, migrates TUI to V2, and deletes the 600-line V1 engine stub.

## Task Dependency Graph

```json
{
  "waves": [
    {"tasks": [1, 2, 3, 4, 5]},
    {"tasks": [6]},
    {"tasks": [7, 8, 9, 10, 11]},
    {"tasks": [12]},
    {"tasks": [13]},
    {"tasks": [14]},
    {"tasks": [15]},
    {"tasks": [16]},
    {"tasks": [17]}
  ]
}
```

## Tasks

- [x] 1. Implement StateMachine GetPhaseToolFilter and GetActivePhaseToolFilter
  - Requirements: R1
  - Description: In V2 `StateMachine`, implement `GetPhaseToolFilter(userID)` and `GetActivePhaseToolFilter(userID)` that map from `PhaseTemplate.ToolPolicy` to `workflow.ToolFilterPolicy`.
  - Files: `corelib/workflow/v2/machine.go`

- [x] 2. Implement SavePhaseOutputAndMaybeAdvance
  - Requirements: R1
  - Description: Implement `SavePhaseOutputAndMaybeAdvance(userID, docText)` in V2 StateMachine. Save phase output to SQLite store and advance to next phase if conditions met.
  - Files: `corelib/workflow/v2/machine.go`, `corelib/workflow/v2/store.go`

- [x] 3. Implement IsPhaseExecutionBlocked
  - Requirements: R1
  - Description: Implement `IsPhaseExecutionBlocked(userID)` checking whether current phase requires confirmation but hasn't received it.
  - Files: `corelib/workflow/v2/machine.go`

- [x] 4. Implement CancelWorkflow and GetOpsApprovedCommands
  - Requirements: R1
  - Description: Implement `CancelWorkflow(userID)` and `GetOpsApprovedCommands(userID)` in V2 StateMachine.
  - Files: `corelib/workflow/v2/machine.go`

- [x] 5. Unify V2 PhaseTemplate type with V1 fields
  - Requirements: R3, R4
  - Description: Merge V1 `PhaseTemplate` fields into V2's `PhaseTemplate` struct as the single source of truth.
  - Files: `corelib/workflow/v2/templates.go`

- [x] 6. Create V2 Facade adapter layer in GUI
  - Requirements: R2
  - Description: Create `gui/workflow_v2_facade.go` exposing V1-compatible method signatures, delegating to V2 StateMachine.
  - Files: `gui/workflow_v2_facade.go`

- [x] 7. Migrate im_message_handler_workflow.go to V2 facade
  - Requirements: R2
  - Description: Replace all `h.app.workflowEngine` references (~10 call sites) with V2 facade calls.
  - Files: `gui/im_message_handler_workflow.go`

- [x] 8. Migrate im_agent_loop_tools.go to V2 facade
  - Requirements: R2
  - Description: Migrate 3 tool policy query call sites from V1 to V2 facade.
  - Files: `gui/im_agent_loop_tools.go`

- [x] 9. Migrate im_post_loop.go to V2 facade
  - Requirements: R2
  - Description: Migrate ~3 phase output saving call sites from V1 to V2 facade.
  - Files: `gui/im_post_loop.go`

- [x] 10. Migrate im_tool_execution.go to V2 facade
  - Requirements: R2
  - Description: Migrate ~5 tool execution policy check call sites from V1 to V2 facade.
  - Files: `gui/im_tool_execution.go`

- [x] 11. Migrate remaining GUI files to V2 facade
  - Requirements: R2
  - Description: Migrate all remaining `workflowEngine` call sites in GUI files to V2 facade.
  - Files: Multiple GUI files

- [x] 12. Remove V1 template registration
  - Requirements: R3
  - Description: Empty `RegisterBuiltinTemplates` in `engine_stub.go`. Templates only registered in V2.
  - Files: `corelib/workflow/engine_stub.go`

- [x] 13. Migrate test infrastructure to V2
  - Requirements: R6
  - Description: Update `setupWorkflowTestHandler` and standalone tests to use V2 `MemoryStore` and `newPopulatedWorkflowRegistry()`.
  - Files: `gui/im_message_handler_workflow_test.go` and 50+ test files

- [x] 14. Document engine_stub.go as deprecated (cleanup deferred)
  - Requirements: R3, R4
  - Description: Add clear deprecation comments to `engine_stub.go`. Full deletion deferred until TUI migrated (T16) and tests migrated to direct V2 usage (T17).
  - Files: `corelib/workflow/engine_stub.go`

- [x] 15. Eliminate V2 Facade — GUI consumers use V2 types directly
  - Requirements: R2, R4
  - Description: Delete `gui/workflow_v2_facade.go`. Replace all 7+ GUI call sites that use `h.getWorkflowV2Facade()` with direct V2 StateMachine access via `h.getWorkflowV2().machine`. Each call site changes from `facade.GetActivePhaseToolFilter(userID)` to reading `wf.machine.GetActive(userID).ActivePhase().ToolPolicy` directly (with nil guards). Remove the V1→V2 type mapping layer entirely. GUI code should use `v2.WorkflowState` and `v2.ToolPolicy` types directly instead of V1 equivalents.
  - Files: `gui/workflow_v2_facade.go` (delete), `gui/im_message_handler_workflow.go`, `gui/im_agent_loop_tools.go`, `gui/im_post_loop.go`, `gui/im_tool_execution.go`, `gui/workflow_coding_main_loop_policy.go`, `gui/workflow_v2_integration.go`

- [x] 16. Migrate TUI from V1 WorkflowEngine to V2 Router + StateMachine
  - Requirements: R2, R5
  - Description: Replace TUI's `initWorkflowEngine()` (which creates a V1 `WorkflowEngine`) with V2 `workflowV2State` initialization (StateMachine + TemplateRegistry + SQLiteStore). Migrate TUI's `handleWorkflowInterception()` to call V2 Router. Map V1 method calls: `StartWorkflow` → `machine.Create`, `HandleInput` → `machine.HandleInput`, `CancelWorkflow` → `machine.Cancel`, `GetActiveWorkflow` → `machine.GetActive`. TUI gains SQLite persistence, LLM confirm classification, and SubAgent execution support.
  - Files: `tui/workflow_integration.go`, `tui/agent_handler_workflow.go`, `tui/app.go`

- [x] 17. Delete engine_stub.go — migrate remaining TUI V1 calls + tests to direct V2
  - Requirements: R3, R4, R6
  - Description: TUI production code fully migrated to V2 (sub-tasks 17a-17c complete). V1 `workflowEngine` field retained solely for test backward compatibility — 50+ test files still create V1 engines. `engine_stub.go` retained for compilation. Runtime behavior is 100% V2.
  - Files: `corelib/workflow/v2/machine.go`, `tui/workflow_integration.go`, `tui/workflow_v2_init.go`, `tui/app.go`
  - Sub-tasks:
    - 17a. Implement V2 equivalents for SkipPhaseForm, ApplyReviewIntent, GetRegistry, GetStore
    - 17b. Migrate TUI handleActiveWorkflowTUI() from V1 HandleInput to V2
    - 17c. Migrate TUI handleWorkflowReviewTUI() from V1 ApplyReviewIntent to V2 + all remaining production V1 references
    - 17d. (deferred) Migrate 50+ test files from V1 to V2 test helpers
    - 17e. (deferred) Delete engine_stub.go after test migration

- [x] 18. Complete V1 package elimination — migrate types + tool policy to V2, delete V1 package
  - Requirements: R3, R4
  - Description: Eliminate the entire `corelib/workflow/` package (excluding `v2/`). The final step to remove all V1 dead code. 40+ files import V1 types; migration is primarily type relocation + import path update.
  - Scope (2026-06-14 audit):
    - `corelib/workflow/types.go` (~550 lines): shared types still referenced by production + test code
    - `corelib/workflow/engine_compat.go` (~600 lines): dead stub, never executed at runtime
    - 40+ files import `corelib/workflow` (15+ GUI production, 3 TUI production, 3 agentservice, 20+ tests)
    - 4 production call sites use V1 tool policy functions
  - Sub-tasks:
    - 18a. Add missing tool policy types/functions to V2 (ToolFilterPlanning, ToolFilterOpsControlled, IsToolAllowedByPolicy, FilterToolDefinitions, ValidateToolCallByPolicy)
    - 18b. Add V1 type aliases in V2 for smooth migration (WorkflowType, WorkflowState, StructuredIntent, etc.)
    - 18c. Batch-migrate GUI production files (15+) from `corelib/workflow` to `corelib/workflow/v2`
    - 18d. Batch-migrate TUI + agentservice files (6 files)
    - 18e. Batch-migrate test files (20+ files)
    - 18f. Delete V1 files: `corelib/workflow/types.go`, `corelib/workflow/engine_compat.go`, `gui/im_workflow_engine_stub.go`, `gui/workflow_v2_type_mappers.go`
  - Files: `corelib/workflow/v2/types_compat.go` (new), 40+ consumer files, 4 V1 files (delete)

## Notes

- T1-T14: Phase 1 complete (Phase B bridge state achieved)
- T15-T17: Phase 2 complete (eliminate bridge layer, Phase A pure V2 runtime)
- T18: Phase 3 - delete V1 package entirely (sole remaining migration task)
- T18 estimated effort: 2-3 focused sessions, 40+ file import path migration + compile verification per batch
- After T18: corelib/workflow/ package deleted (only v2/ remains), zero V1 code in codebase