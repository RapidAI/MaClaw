# Implementation Plan: preview-panel-tab-binding

## Overview

This implementation plan fixes the preview panel tab binding bugs where the AI assistant panel's right-side preview area uses global singleton state not bound to individual tabs. The fix introduces per-tab preview state storage, routes backend events by `project_path`, extends the tab switch effect to save/restore preview state, and removes guards that prematurely hide or discard documents.

## Tasks

- [x] 1. Write bug condition exploration test
  - **Property 1: Bug Condition** - Preview State Isolation Per Tab
  - **CRITICAL**: This test MUST FAIL on unfixed code - failure confirms the bug exists
  - **DO NOT attempt to fix the test or the code when it fails**
  - **NOTE**: This test encodes the expected behavior - it will validate the fix when it passes after implementation
  - **GOAL**: Surface counterexamples that demonstrate preview state is NOT isolated per tab
  - **Scoped PBT Approach**: For deterministic bugs, scope the property to concrete failing cases:
    - Tab switch with active workflow preview: switching from Tab A (with documents) to Tab B should NOT show Tab A's documents
    - Workflow completion: `workflow:phase_update(completed)` should NOT hide the preview pane (`splitMode` should remain true)
    - Late doc_update: `workflow:doc_update` arriving after `workflow:phase_update(completed)` should NOT be discarded
    - Cross-tab code file update: `code:file_update` with `project_path=TabA` while Tab B is active should NOT affect Tab B's state
  - Test that for any tab switch interaction where multipleTabsExist(), the preview state of Tab A is saved and Tab B's state is restored (from Bug Condition `isBugCondition` in design)
  - Test that workflow completion does not set `splitMode=false` (from Bug Condition: `previewPaneHasDocuments()` case)
  - Test that `workflowActiveRef` guard does not discard late doc_update events (from Bug Condition: `workflowJustCompleted()` case)
  - Test that `code:file_update` events route to the correct tab based on `project_path` (from Bug Condition: `eventTargetTab != activeTab` case)
  - Run test on UNFIXED code
  - **EXPECTED OUTCOME**: Test FAILS (this is correct - it proves the bug exists)
  - Document counterexamples found:
    - Tab switch shows wrong tab's documents or empty panel
    - Workflow completion immediately hides documents via `setWorkflowSplitMode(false)`
    - Late doc_update silently discarded due to `workflowActiveRef.current` guard
    - Code file update applied to wrong tab's global state
  - Mark task complete when test is written, run, and failure is documented
  - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5_

- [x] 2. Write preservation property tests (BEFORE implementing fix)
  - **Property 2: Preservation** - Single Tab Backward Compatibility
  - **IMPORTANT**: Follow observation-first methodology
  - **IMPORTANT**: Write these tests BEFORE implementing the fix
  - Observe behavior on UNFIXED code for non-buggy inputs (single tab only, no multi-tab interaction):
    - Observe: with only the "local" tab, all workflow events apply to the global state exactly as before
    - Observe: `userClosed=true` suppresses auto-open within the same tab
    - Observe: `code:session_start` resets files map and closes panel
    - Observe: split ratio drag-handle changes persist within the same tab
    - Observe: events without `project_path` route to active tab (current behavior)
    - Observe: `workflow:suggest_maximize` shows fullscreen suggestion banner
  - Write property-based test: for all inputs where only the single "local" tab exists (no project tabs), the system produces the same observable behavior as the original system with no save/restore overhead
  - Property-based testing generates many test cases for stronger preservation guarantees across the input domain
  - Verify tests PASS on UNFIXED code
  - **EXPECTED OUTCOME**: Tests PASS (this confirms baseline behavior to preserve)
  - Mark task complete when tests are written, run, and passing on unfixed code
  - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6_

- [x] 3. Fix for preview panel tab binding bugs

  - [x] 3.1 Remove premature split mode closure in useWorkflowState.ts
    - Delete `setWorkflowSplitMode(false)` call inside the `if (!isActive)` branch of the `workflow:phase_update` handler (~line 227)
    - Keep documents visible after workflow completion; only clear `splitMode` on explicit user action (close button), new workflow start (new workflow ID detected), or full workflow reset (null state)
    - _Bug_Condition: isBugCondition(input) where input.type == "workflow_completion" AND previewPaneHasDocuments()_
    - _Expected_Behavior: splitMode remains true after workflow completion, documents remain accessible_
    - _Preservation: Single tab behavior unchanged — splitMode logic still applies for explicit close/reset_
    - _Requirements: 2.2_

  - [x] 3.2 Remove workflowActiveRef guard on doc_update handler in useWorkflowState.ts
    - Delete the `if (!workflowActiveRef.current) return;` guard (~line 237) in the `workflow:doc_update` handler
    - Accept late document updates regardless of workflow active status — the document is the final output and should always be displayed
    - _Bug_Condition: isBugCondition(input) where input.type == "doc_update" AND workflowJustCompleted()_
    - _Expected_Behavior: doc_update events are accepted and displayed regardless of workflowActiveRef state_
    - _Preservation: Non-buggy doc_update handling (during active workflow) unchanged_
    - _Requirements: 2.3_

  - [x] 3.3 Add project_path event routing in useWorkflowState.ts
    - In `workflow:phase_update` and `workflow:doc_update` handlers, extract `data.project_path` from event payload
    - If `project_path` is present and does not match the currently active tab's `projectPath`, store the update in the per-tab state map for the matching tab (or orphan map if no tab matches) instead of applying to current state
    - If `project_path` is absent, route to currently active tab (backward compatible fallback)
    - _Bug_Condition: isBugCondition(input) where input.type == "doc_update" AND eventTargetTab != activeTab_
    - _Expected_Behavior: events routed to correct tab's state based on project_path_
    - _Preservation: Events without project_path route to active tab (same as current behavior)_
    - _Requirements: 2.4, 3.2_

  - [x] 3.4 Add project_path event routing in useCodePreviewState.ts
    - In `code:file_update` handler, extract `data.project_path`
    - If present and non-matching to the active tab, store the update in the corresponding tab's state in the per-tab map
    - If absent, apply to current active tab's state (backward compatible fallback)
    - _Bug_Condition: isBugCondition(input) where input.type == "code_file_update" AND eventTargetTab != activeTab_
    - _Expected_Behavior: code file updates routed to correct tab based on project_path_
    - _Preservation: Events without project_path apply to active tab (same as current behavior)_
    - _Requirements: 2.4, 3.2_

  - [x] 3.5 Introduce per-tab preview state map in AIAssistantPanel.tsx
    - Add `const previewStateMapRef = useRef<Map<string, { workflow: WorkflowUIState; code: CodePreviewUIState; previewMode: PreviewPaneMode }>>(new Map());`
    - This map stores preview state keyed by tab ID for save/restore on tab switch
    - _Bug_Condition: isBugCondition(input) where input.type == "tab_switch" AND multipleTabsExist()_
    - _Expected_Behavior: each tab has its own isolated preview state in the map_
    - _Preservation: When only single "local" tab exists, map remains empty — zero overhead_
    - _Requirements: 2.1, 2.5, 3.1_

  - [x] 3.6 Extend tab switch effect to save/restore preview state in AIAssistantPanel.tsx
    - In the existing `prevActiveTabIdRef` effect (lines 174-226), before restoring the new tab:
      - Save the previous tab's `workflowState`, `codePreviewState`, and current `activePreviewMode` to the previewStateMapRef
    - After activating the new tab:
      - Restore its preview state from the map (or use initial/empty state if not found)
    - Add orphan state adoption: when a new project tab is created with a `projectPath`, check orphan map for matching state and adopt it
    - _Bug_Condition: isBugCondition(input) where input.type == "tab_switch" AND multipleTabsExist()_
    - _Expected_Behavior: Tab A's state is saved, Tab B's state is restored; preview pane displays Tab B's state immediately_
    - _Preservation: Single tab — no save/restore executes, zero overhead_
    - _Requirements: 2.1, 2.5, 3.1_

  - [x] 3.7 Add AgentView per-tab scoping in AIAssistantPanel.tsx
    - Store `agentView` ownership (tab ID) and only display it when the owning tab is active
    - Hide AgentView when switching away from the owning tab
    - _Bug_Condition: isBugCondition(input) where input.type == "agent_view_display" AND agentViewOwnerTab != activeTab_
    - _Expected_Behavior: AgentView only visible when owning tab is active_
    - _Preservation: Single tab — agentView always visible (same as current)_
    - _Requirements: 2.6_

  - [x] 3.8 Add project_path to workflow:doc_update event payload in Go backend
    - In `gui/workflow_adapter.go`, `EmitDocUpdate` function: include `a.workingDir` (via `a.GetWorkingDir()`) as `"project_path"` in the event map payload
    - Note: `workflow:phase_update` already includes project_path through `normalizeWorkflowStateForFrontendWithRegistry`
    - _Bug_Condition: Frontend cannot route doc_update events without project_path field_
    - _Expected_Behavior: workflow:doc_update payload includes project_path for frontend routing_
    - _Preservation: Existing phase_update behavior unchanged_
    - _Requirements: 2.4_

  - [x] 3.9 Add project_path to code:file_update event payload in Go backend
    - In `gui/im_message_handler.go` (or equivalent code:file_update emission point): include the current working directory / project path in the `code:file_update` event payload
    - _Bug_Condition: Frontend cannot route code:file_update events without project_path field_
    - _Expected_Behavior: code:file_update payload includes project_path for frontend routing_
    - _Preservation: Existing code:file_update consumers that ignore project_path are unaffected_
    - _Requirements: 2.4_

  - [x] 3.10 Verify bug condition exploration test now passes
    - **Property 1: Expected Behavior** - Preview State Isolation Per Tab
    - **IMPORTANT**: Re-run the SAME test from task 1 - do NOT write a new test
    - The test from task 1 encodes the expected behavior
    - When this test passes, it confirms the expected behavior is satisfied:
      - Tab switch correctly saves/restores preview state
      - Workflow completion keeps documents visible
      - Late doc_update events are accepted
      - Code file updates route to correct tab
    - Run bug condition exploration test from step 1
    - **EXPECTED OUTCOME**: Test PASSES (confirms bug is fixed)
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5_

  - [x] 3.11 Verify preservation tests still pass
    - **Property 2: Preservation** - Single Tab Backward Compatibility
    - **IMPORTANT**: Re-run the SAME tests from task 2 - do NOT write new tests
    - Run preservation property tests from step 2
    - **EXPECTED OUTCOME**: Tests PASS (confirms no regressions)
    - Confirm all tests still pass after fix:
      - Single tab behavior identical to pre-fix
      - Events without project_path route to active tab
      - userClosed suppression works per-tab
      - code:session_start reset works per-tab
      - Split ratio persists per-tab
      - workflow:suggest_maximize shows banner

- [x] 4. Checkpoint - Ensure all tests pass
  - Run full test suite to confirm no regressions
  - Verify Property 1 (Bug Condition) test passes on fixed code
  - Verify Property 2 (Preservation) tests pass on fixed code
  - Verify any existing unit tests in the modified files still pass
  - Ensure all tests pass, ask the user if questions arise.

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1", "2"] },
    { "id": 1, "tasks": ["3.1", "3.2", "3.5", "3.8", "3.9"] },
    { "id": 2, "tasks": ["3.3", "3.4", "3.6", "3.7"] },
    { "id": 3, "tasks": ["3.10", "3.11"] },
    { "id": 4, "tasks": ["4"] }
  ]
}
```

## Notes

- Tasks 1 and 2 (exploration and preservation tests) can run in parallel since they are independent
- Task 3 (fix implementation) depends on both tasks 1 and 2 being complete
- Within task 3, sub-tasks 3.1 and 3.2 are independent and can start immediately
- Sub-task 3.5 (per-tab map introduction) is the foundation for 3.3, 3.4, 3.6, and 3.7
- Backend changes (3.8, 3.9) are prerequisites for frontend event routing (3.3, 3.4)
- Verification tasks (3.10, 3.11) depend on all implementation sub-tasks being complete
- Task 4 (checkpoint) depends on all of task 3 including verification
