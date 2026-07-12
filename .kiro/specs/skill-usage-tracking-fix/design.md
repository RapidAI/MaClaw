# Skill Usage Tracking Fix — Bugfix Design

## Overview

After a skill is successfully invoked via the agent, the skill management UI continues to display "未使用" (never used) because the backend persists updated usage statistics to disk but never notifies the frontend to refresh its cached skill list. The fix adds a Wails event emission after stats are persisted in both GUI execution paths (SkillRunner async and SkillExecutor sync), and subscribes the frontend `SkillsManagementPanel` to that event so it automatically reloads. The TUI path is confirmed to already read fresh data from disk on each `LoadConfig()` call, so no TUI change is needed.

## Glossary

- **Bug_Condition (C)**: A skill execution completes (success or failure) via the GUI agent, usage stats are persisted to disk, but no Wails event is emitted to notify the frontend
- **Property (P)**: After usage stats are persisted, a `skill:usage_updated` Wails event is emitted, and the frontend refreshes its skill list upon receiving the event
- **Preservation**: Existing stats update logic (increment counts, set timestamps, skip file-based skills), initial `loadData()` on mount, `List()` computed fields, and manual refresh behavior must remain unchanged
- **`updateUsageStats()`**: Method on `SkillRunner` in `gui/skill_runner.go` that updates usage stats for the async execution path
- **`SkillExecutor.Execute()`**: Method in `gui/app_nl_skills.go` that runs skills synchronously and updates usage stats in a write lock block
- **`SkillsManagementPanel`**: React component in `gui/frontend/src/components/remote/SkillsManagementPanel.tsx` that displays the skill list, currently only loading data on mount
- **`FileConfigStore`**: TUI config store in `tui/commands/file_config_store.go` that reads/writes config JSON from disk with no in-memory caching

## Bug Details

### Bug Condition

The bug manifests when a skill is invoked via either GUI execution path (SkillRunner async or SkillExecutor sync), usage stats are successfully persisted to disk via `saveSkills()`, but the frontend `SkillsManagementPanel` continues to display stale cached data because no event triggers a refresh.

**Formal Specification:**
```
FUNCTION isBugCondition(input)
  INPUT: input of type SkillExecution
  OUTPUT: boolean

  RETURN input.executionPath IN ["SkillRunner.updateUsageStats", "SkillExecutor.Execute"]
         AND input.skill.Source != "file"
         AND saveSkills(updatedSkills) succeeded
         AND NOT wailsEventEmitted("skill:usage_updated")
END FUNCTION
```

### Examples

- **Async path**: User invokes skill "pdf-converter" via agent chat → SkillRunner executes steps → `updateUsageStats()` increments `UsageCount` to 1 and calls `saveSkills()` → config.json updated on disk → SkillsManagementPanel still shows `usage_count: 0` and "未使用"
- **Sync path**: User invokes skill "code-formatter" via `SkillExecutor.Execute()` → steps complete → write lock block increments stats and calls `saveSkills()` → config.json updated → panel still shows stale data
- **Panel already open**: User has SkillsManagementPanel open, invokes a skill from the chat → skill completes → panel shows "未使用" until user manually navigates away and back
- **File-based skill (non-bug)**: User invokes a file-based skill → `updateUsageStats()` returns early due to `source == "file"` → no stats update, no event needed → correct behavior

## Expected Behavior

### Preservation Requirements

**Unchanged Behaviors:**
- File-based skills (`source == "file"`) must continue to skip usage stats updates entirely
- `SkillsManagementPanel` must continue to call `loadData()` on initial mount via `useEffect`
- `updateUsageStats()` must continue to increment `UsageCount`, set `LastUsedAt`, increment `SuccessCount` on success, and record `LastError` on failure
- `SkillExecutor.Execute()` write lock block must continue the same stats update logic
- `SkillExecutor.List()` must continue to compute `SuccessRate` as `SuccessCount / UsageCount`
- Manual navigation away from and back to the panel must continue to reload skills via `loadData()` on remount
- The existing manual "Refresh" button must continue to work

**Scope:**
All inputs that do NOT involve skill execution completing with stats persistence should be completely unaffected by this fix. This includes:
- Skill creation, update, deletion operations
- Hub search and install operations
- Panel tab switching
- External directory management

## Hypothesized Root Cause

Based on the bug description and code analysis, the root cause is straightforward:

1. **Missing Event Emission in SkillRunner**: `updateUsageStats()` in `gui/skill_runner.go` calls `r.executor.saveSkills(skills)` but does not call `app.emitEvent()` afterward. The SkillRunner has access to `r.executor.app` which provides the `emitEvent()` helper (handles nil context guard internally).

2. **Missing Event Emission in SkillExecutor**: `Execute()` in `gui/app_nl_skills.go` calls `e.saveSkills(skills)` in the write lock block but does not emit an event. The SkillExecutor has access to `e.app` which provides the `emitEvent()` helper. The event should be emitted outside the write lock to minimize lock hold time.

3. **No Event Subscription in Frontend**: `SkillsManagementPanel` only calls `loadData()` once on mount via `useEffect(() => { loadData(); }, [loadData])`. There is no `EventsOn("skill:usage_updated", ...)` listener to trigger a refresh when stats change.

4. **TUI Non-Issue**: `FileConfigStore.LoadConfig()` reads from disk on every call (`os.ReadFile(s.path)`). There is no in-memory cache. Subsequent operations after `SaveConfig()` will read fresh data. No fix needed.

## Correctness Properties

Property 1: Bug Condition - Event Emission After Stats Persistence

_For any_ skill execution where the skill's source is not "file" and `saveSkills()` succeeds in either `SkillRunner.updateUsageStats()` or `SkillExecutor.Execute()`, the system SHALL emit a `skill:usage_updated` Wails event via `runtime.EventsEmit(ctx, "skill:usage_updated")`.

**Validates: Requirements 2.1, 2.2**

Property 2: Preservation - File-Based Skill Skip and Existing Stats Logic

_For any_ skill execution where the skill's source is "file", the system SHALL NOT emit a `skill:usage_updated` event and SHALL NOT update usage stats, preserving the existing skip behavior. For all non-file skills, the stats update logic (UsageCount increment, LastUsedAt, SuccessCount/LastError) SHALL remain identical to the original implementation.

**Validates: Requirements 3.1, 3.3, 3.4, 3.5**

## Fix Implementation

### Changes Required

Assuming our root cause analysis is correct:

**File**: `gui/skill_runner.go`

**Function**: `updateUsageStats`

**Specific Changes**:
1. **Emit event after saveSkills**: After `_ = r.executor.saveSkills(skills)` succeeds and before the function returns, use the existing `App.emitEvent()` helper (which handles nil context guard internally):
   ```go
   r.executor.app.emitEvent("skill:usage_updated")
   ```

---

**File**: `gui/app_nl_skills.go`

**Function**: `Execute`

**Specific Changes**:
2. **Emit event after saveSkills, outside the write lock**: After the `e.mu.Unlock()` call (not inside the lock block), emit the event to minimize lock hold time. Use a flag to track whether the event should fire:
   ```go
   // Inside the lock block, after saveSkills:
   shouldEmit = true
   // ...
   e.mu.Unlock()
   if shouldEmit {
       e.app.emitEvent("skill:usage_updated")
   }
   ```
   Note: `App.emitEvent()` handles nil context guard internally, so no manual nil check is needed.

---

**File**: `gui/frontend/src/components/remote/SkillsManagementPanel.tsx`

**Function**: Component body (useEffect hooks)

**Specific Changes**:
3. **Import EventsOn/EventsOff**: Add import from `../../../wailsjs/runtime` (consistent with sibling components `ScheduledTasksPanel.tsx`, `RemoteSessionList.tsx`, `EmbeddingConfigPanel.tsx`):
   ```typescript
   import { EventsOn, EventsOff } from "../../../wailsjs/runtime";
   ```

4. **Add event listener useEffect**: Add a new `useEffect` that subscribes to `skill:usage_updated` and calls `loadData()`:
   ```typescript
   useEffect(() => {
       EventsOn("skill:usage_updated", () => {
           loadData();
       });
       return () => {
           EventsOff("skill:usage_updated");
       };
   }, [loadData]);
   ```

5. **No debounce needed**: The event fires once per skill execution (not in a tight loop), so a simple direct `loadData()` call is sufficient. If rapid successive executions become a concern in the future, a debounce can be added, but it's unnecessary for the current usage pattern.

## Testing Strategy

### Validation Approach

The testing strategy follows a two-phase approach: first, surface counterexamples that demonstrate the bug on unfixed code, then verify the fix works correctly and preserves existing behavior.

### Exploratory Bug Condition Checking

**Goal**: Surface counterexamples that demonstrate the bug BEFORE implementing the fix. Confirm or refute the root cause analysis. If we refute, we will need to re-hypothesize.

**Test Plan**: Write tests that invoke skill execution through both paths and assert that a Wails event is emitted after stats persistence. Run these tests on the UNFIXED code to observe failures and confirm the root cause.

**Test Cases**:
1. **SkillRunner Async Path Test**: Call `updateUsageStats()` with a non-file skill and successful execution → assert `runtime.EventsEmit` was called with `"skill:usage_updated"` (will fail on unfixed code)
2. **SkillExecutor Sync Path Test**: Call `Execute()` with a non-file skill → assert event emission after stats persistence (will fail on unfixed code)
3. **Frontend Event Subscription Test**: Mount `SkillsManagementPanel`, emit `skill:usage_updated` event → assert `ListNLSkills` binding is called again (will fail on unfixed code — no listener exists)

**Expected Counterexamples**:
- No `runtime.EventsEmit` call found after `saveSkills()` in either path
- No `EventsOn("skill:usage_updated", ...)` found in SkillsManagementPanel
- Possible causes: simple omission — the event notification pattern exists elsewhere in the codebase (e.g., `background-loops-changed`, `scheduled-tasks-changed`) but was never added for skill usage updates

### Fix Checking

**Goal**: Verify that for all inputs where the bug condition holds, the fixed function produces the expected behavior.

**Pseudocode:**
```
FOR ALL input WHERE isBugCondition(input) DO
  result := executeSkill_fixed(input)
  ASSERT wailsEventEmitted("skill:usage_updated")
  ASSERT frontendRefreshed() IF panelMounted
END FOR
```

### Preservation Checking

**Goal**: Verify that for all inputs where the bug condition does NOT hold, the fixed function produces the same result as the original function.

**Pseudocode:**
```
FOR ALL input WHERE NOT isBugCondition(input) DO
  ASSERT executeSkill_original(input) = executeSkill_fixed(input)
END FOR
```

**Testing Approach**: Property-based testing is recommended for preservation checking because:
- It generates many test cases automatically across the input domain (various skill sources, success/failure combinations)
- It catches edge cases that manual unit tests might miss (e.g., nil app context, concurrent executions)
- It provides strong guarantees that behavior is unchanged for all non-buggy inputs

**Test Plan**: Observe behavior on UNFIXED code first for file-based skills and stats update logic, then write property-based tests capturing that behavior.

**Test Cases**:
1. **File-Based Skill Preservation**: Verify that file-based skills continue to skip stats updates and no event is emitted — observe on unfixed code, then verify after fix
2. **Stats Update Logic Preservation**: Verify that UsageCount, SuccessCount, LastUsedAt, LastError are updated identically for success and failure cases
3. **Initial Load Preservation**: Verify that SkillsManagementPanel still calls loadData() on mount regardless of event subscription
4. **List() Computation Preservation**: Verify that SuccessRate computation remains unchanged

### Unit Tests

- Test `updateUsageStats()` emits event for non-file skills after successful save
- Test `updateUsageStats()` does NOT emit event for file-based skills
- Test `Execute()` emits event after stats persistence in write lock block
- Test `Execute()` does NOT emit event for file-based skills
- Test nil context guard (no panic when `app.ctx` is nil)
- Test frontend `useEffect` cleanup properly calls `EventsOff`

### Property-Based Tests

- Generate random skill configurations (varying source, status, step counts) and verify event emission follows the rule: emitted if and only if source != "file" and saveSkills succeeds
- Generate random success/failure execution results and verify stats update logic is identical before and after the fix
- Generate random sequences of skill executions and verify the frontend receives the correct number of refresh triggers

### Integration Tests

- Test full flow: invoke skill via agent → verify stats persisted → verify event emitted → verify panel refreshes
- Test concurrent skill executions → verify no race conditions in event emission
- Test panel mount/unmount lifecycle → verify event listener cleanup prevents memory leaks
