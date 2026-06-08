# Preview Panel Tab Binding Bugfix Design

## Overview

The AI assistant panel's right-side preview area (workflow progress board, document preview, source code preview) uses global singleton state (`useWorkflowState()` and `useCodePreviewState()`) that is not bound to individual tabs. This causes preview content to persist/disappear incorrectly on tab switch, documents to vanish on workflow completion, and late `workflow:doc_update` events to be discarded due to a race condition with the `workflowActiveRef` guard. The fix introduces per-tab preview state storage with an in-memory `Map<tabId, PreviewState>`, routes backend events by `project_path`, extends the tab switch effect to save/restore preview state, and removes the guards that prematurely hide or discard documents.

## Glossary

- **Bug_Condition (C)**: The condition that triggers the bug — switching tabs causes preview state mismatch, workflow completion hides documents, late doc_update events are discarded, or AgentView bleeds across tabs
- **Property (P)**: The desired behavior — each tab owns its own preview state (workflow + code + preview mode), events are routed to the correct tab's state, and documents persist until user action
- **Preservation**: Existing single-tab behavior (local tab only), backward-compatible event handling without `project_path`, `userClosed` suppression, `code:session_start` reset, and split ratio persistence must remain unchanged
- **PreviewState**: A composite of `WorkflowUIState`, `CodePreviewUIState`, and `PreviewPaneMode` for a single tab
- **project_path**: A field in backend events identifying which project/tab the event belongs to
- **orphanPreviewStates**: Preview states received for a `project_path` that has no matching tab yet

## Bug Details

### Bug Condition

The bug manifests when the user interacts with multiple tabs in the AI assistant panel. The global singleton state hooks (`useWorkflowState`, `useCodePreviewState`) do not track which tab's data they hold. Events from the backend update a single global state regardless of which tab is active, and tab switches do not save/restore the preview panel's state.

**Formal Specification:**
```
FUNCTION isBugCondition(input)
  INPUT: input of type UserInteraction (tab switch, workflow event, or tab creation)
  OUTPUT: boolean

  RETURN (input.type == "tab_switch" AND multipleTabsExist())
         OR (input.type == "workflow_completion" AND previewPaneHasDocuments())
         OR (input.type == "doc_update" AND workflowJustCompleted())
         OR (input.type == "code_file_update" AND eventTargetTab != activeTab)
         OR (input.type == "agent_view_display" AND agentViewOwnerTab != activeTab)
END FUNCTION
```

### Examples

- User has Tab A with an active coding workflow showing design docs, switches to Tab B → preview pane still shows Tab A's docs or disappears entirely
- Workflow completes (status → "completed") → `setSplitMode(false)` immediately hides the preview pane, user cannot review final documents
- Backend emits `workflow:doc_update` 200ms after `workflow:phase_update(completed)` → `workflowActiveRef.current` is already false → document update discarded
- Tab A receives `code:file_update` while Tab B is active → global code preview state shows Tab A's file on Tab B's panel
- AgentView form shown for Tab A's task remains visible when user switches to Tab B

## Expected Behavior

### Preservation Requirements

**Unchanged Behaviors:**
- When only the single "local" tab exists, behavior is identical to current implementation (global singleton state, no save/restore overhead)
- Backend events without a `project_path` field are routed to the currently active tab's state (backward compatible fallback)
- `userClosed` flag continues to suppress auto-open for that specific tab until user explicitly reopens
- `workflow:suggest_maximize` continues to show the fullscreen suggestion banner
- `code:session_start` continues to reset the code preview files map and close the panel until first file update
- Split ratio drag-handle adjustments persist and apply consistently within the same tab

**Scope:**
All inputs that do NOT involve multi-tab interaction, workflow completion document hiding, or cross-tab event routing should be completely unaffected by this fix. This includes:
- Single-tab usage (local tab only)
- Chat message send/receive
- Tab creation/deletion mechanics
- Backend agent loop architecture

## Hypothesized Root Cause

Based on the bug description and code analysis, the most likely issues are:

1. **Global singleton state architecture**: `useWorkflowState()` and `useCodePreviewState()` are called once at the `AIAssistantPanel` top level (line ~710), returning a single shared state object. There is no mechanism to maintain separate state per tab.

2. **Premature split mode closure**: `useWorkflowState.ts` line ~227 (`if (!isActive) { setWorkflowSplitMode(false); }`) immediately hides the preview pane when workflow status transitions away from "active", preventing document review.

3. **Race condition guard on doc_update**: `useWorkflowState.ts` line ~237 (`if (!workflowActiveRef.current) return;`) discards `workflow:doc_update` events that arrive after the `workflow:phase_update(completed)` event, which is common due to async event ordering.

4. **Missing event routing by project_path**: Backend events carry data but the frontend state layer does not inspect any `project_path` field to determine which tab should receive the update. `EmitDocUpdate` in `workflow_adapter.go` does not include `project_path` in its payload.

5. **Tab switch effect gap**: The existing tab switch effect (AIAssistantPanel lines 174-226) saves/restores chat messages and scroll position but does not save/restore `workflowState`, `codePreviewState`, or `activePreviewMode`.

6. **AgentView not scoped to tab**: The `agentView` prop is evaluated globally (`const showAgentView = !!agentView;`) without checking if the owning tab is active.

## Correctness Properties

Property 1: Bug Condition - Preview State Isolation Per Tab

_For any_ tab switch interaction where the user switches from Tab A to Tab B, the fixed system SHALL save Tab A's complete preview state (workflowState + codePreviewState + activePreviewMode) to an in-memory map keyed by Tab A's ID, and restore Tab B's previously saved preview state from the same map. The preview pane SHALL display Tab B's state immediately after the switch.

**Validates: Requirements 2.1, 2.5**

Property 2: Preservation - Single Tab Backward Compatibility

_For any_ interaction where only the single "local" tab exists (no project tabs), the fixed system SHALL produce the same observable behavior as the original system, with no save/restore overhead and all events routing to the single global state instance.

**Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5, 3.6**

## Fix Implementation

### Changes Required

Assuming our root cause analysis is correct:

**File**: `gui/frontend/src/components/ai/useWorkflowState.ts`

**Function**: `useWorkflowState` (major rewrite → per-tab aware)

**Specific Changes**:
1. **Remove premature split mode closure**: Delete the `setWorkflowSplitMode(false)` call inside the `if (!isActive)` branch of the `workflow:phase_update` handler. Instead, keep documents visible. Only clear `splitMode` on explicit user action (close button), new workflow start (new workflow ID detected), or full workflow reset (null state).
2. **Remove workflowActiveRef guard on doc_update**: Delete the `if (!workflowActiveRef.current) return;` guard at line ~237 in the `workflow:doc_update` handler. Accept late document updates regardless of workflow active status — the document is the final output and should always be displayed.
3. **Add project_path event routing**: In both `workflow:phase_update` and `workflow:doc_update` event handlers, extract `data.project_path` from the event payload. If the field is present and does not match the currently active tab's `projectPath`, store the update in the per-tab state map for the matching tab (or orphan map if no tab matches) instead of applying it to current state.

**File**: `gui/frontend/src/components/ai/useCodePreviewState.ts`

**Function**: `useCodePreviewState` (event routing enhancement)

**Specific Changes**:
1. **Add project_path event routing**: In the `code:file_update` handler, extract `data.project_path`. If present and non-matching to the active tab, store the update in the corresponding tab's state in the per-tab map.

**File**: `gui/frontend/src/components/ai/AIAssistantPanel.tsx`

**Function**: Tab switch effect (lines 174-226) + state initialization

**Specific Changes**:
1. **Introduce per-tab preview state map**: Add `const previewStateMapRef = useRef<Map<string, { workflow: WorkflowUIState; code: CodePreviewUIState; previewMode: PreviewPaneMode }>>(new Map());`
2. **Save on tab switch**: In the existing `prevActiveTabIdRef` effect, before restoring the new tab, save the previous tab's `workflowState`, `codePreviewState`, and current `activePreviewMode` to the map.
3. **Restore on tab switch**: After activating the new tab, restore its preview state from the map (or use initial/empty state if not found).
4. **Orphan state adoption**: When a new project tab is created with a `projectPath`, check `orphanPreviewStates` map for matching state and adopt it.
5. **AgentView per-tab scoping**: Store `agentView` ownership (tab ID) and only display it when the owning tab is active. Hide when switching away.

**File**: `gui/frontend/src/components/ai/AssistantPreviewPane.tsx`

**Function**: `AssistantPreviewPane` (receive per-tab state)

**Specific Changes**:
1. **No structural changes needed**: This component already receives `workflowState` and `codePreviewState` as props. The per-tab binding happens upstream in `AIAssistantPanel` which will pass the correct tab's state.
2. **Preview mode persistence**: The `activeMode` state within `AssistantPreviewPane` needs to be lifted to the parent or stored per-tab. Currently it's local state that resets on prop changes. The per-tab map in `AIAssistantPanel` will store and restore this value.

**File**: `gui/workflow_adapter.go`

**Function**: `EmitDocUpdate`

**Specific Changes**:
1. **Add project_path to event payload**: Include `a.workingDir` (already available via `a.GetWorkingDir()`) as `"project_path"` in the `workflow:doc_update` event map payload. The `workflow:phase_update` already includes project_path through `normalizeWorkflowStateForFrontendWithRegistry` which copies `state.ProjectPath`.

**File**: `gui/im_message_handler.go` (or equivalent code:file_update emission point)

**Function**: `code:file_update` event emission

**Specific Changes**:
1. **Add project_path to code:file_update payload**: When emitting `code:file_update` events, include the current working directory / project path so the frontend can route the event to the correct tab.

## Testing Strategy

### Validation Approach

The testing strategy follows a two-phase approach: first, surface counterexamples that demonstrate the bug on unfixed code, then verify the fix works correctly and preserves existing behavior.

### Exploratory Bug Condition Checking

**Goal**: Surface counterexamples that demonstrate the bug BEFORE implementing the fix. Confirm or refute the root cause analysis. If we refute, we will need to re-hypothesize.

**Test Plan**: Write unit tests that simulate multi-tab scenarios with workflow events and assert that preview state is correctly isolated. Run these tests on the UNFIXED code to observe failures.

**Test Cases**:
1. **Tab Switch Stale Preview**: Simulate switching from Tab A (active workflow with documents) to Tab B → assert Tab B does not show Tab A's documents (will fail on unfixed code)
2. **Workflow Completion Document Loss**: Simulate `workflow:phase_update(completed)` → assert `splitMode` remains true and documents are still accessible (will fail on unfixed code)
3. **Late Doc Update Discard**: Simulate `workflow:phase_update(completed)` followed by `workflow:doc_update` 100ms later → assert document is accepted (will fail on unfixed code)
4. **Cross-Tab Code File Update**: Simulate `code:file_update` with `project_path=TabA` while Tab B is active → assert Tab B's code preview is not affected (will fail on unfixed code)

**Expected Counterexamples**:
- Tab switch shows wrong tab's documents or empty panel
- Workflow completion immediately hides documents
- Late doc_update silently discarded due to `workflowActiveRef` guard
- Possible causes: global singleton state, premature splitMode=false, workflowActiveRef guard

### Fix Checking

**Goal**: Verify that for all inputs where the bug condition holds, the fixed function produces the expected behavior.

**Pseudocode:**
```
FOR ALL input WHERE isBugCondition(input) DO
  result := handleInteraction_fixed(input)
  ASSERT expectedBehavior(result)
END FOR
```

### Preservation Checking

**Goal**: Verify that for all inputs where the bug condition does NOT hold, the fixed function produces the same result as the original function.

**Pseudocode:**
```
FOR ALL input WHERE NOT isBugCondition(input) DO
  ASSERT handleInteraction_original(input) = handleInteraction_fixed(input)
END FOR
```

**Testing Approach**: Property-based testing is recommended for preservation checking because:
- It generates many test cases automatically across the input domain
- It catches edge cases that manual unit tests might miss
- It provides strong guarantees that behavior is unchanged for all non-buggy inputs

**Test Plan**: Observe behavior on UNFIXED code first for single-tab scenarios and non-multi-tab events, then write property-based tests capturing that behavior.

**Test Cases**:
1. **Single Tab Preservation**: Verify that with only the "local" tab, all workflow events apply to the global state exactly as before
2. **UserClosed Preservation**: Verify that `userClosed=true` suppresses auto-open within the same tab after fix
3. **Code Session Start Preservation**: Verify `code:session_start` resets files map and closes panel for the affected tab
4. **Split Ratio Preservation**: Verify drag-handle split ratio changes persist within the same tab
5. **Backward-Compatible Event Routing**: Verify events without `project_path` route to active tab (same as current behavior)

### Unit Tests

- Test per-tab state map save/restore on tab switch
- Test event routing by `project_path` to correct tab
- Test orphan state adoption when new tab matches
- Test workflow completion keeps documents visible
- Test late `workflow:doc_update` accepted after completion
- Test AgentView only visible on owning tab
- Test single-tab mode has zero overhead

### Property-Based Tests

- Generate random tab configurations (1-5 tabs) and random event sequences → verify each tab's state is isolated
- Generate random workflow lifecycle events → verify documents are never prematurely hidden
- Generate random `code:file_update` events with various `project_path` values → verify routing correctness
- Generate random tab switch sequences → verify state save/restore round-trips correctly

### Integration Tests

- Test full flow: create project tab → start workflow → receive events → switch tabs → switch back → verify state restored
- Test flow: workflow completes → verify documents visible → user sends new message → verify eventual clear
- Test flow: multiple project tabs receiving concurrent events → verify isolation
