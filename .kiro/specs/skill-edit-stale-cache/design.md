# Skill Edit Stale Cache Bugfix Design

## Overview

Two bugs affect the "编辑 Skill" (Edit Skill) dialog. Bug A: `shouldHydrateSkillFromFile()` in `gui/app_nl_skills.go` incorrectly returns `false` when a config-based skill already has steps and its source is not `"hub"`, preventing on-disk `skill.yaml` changes from being reflected in the edit dialog. Bug B: the modal backdrop in `SkillsManagementPanel.tsx` uses a simple `onClick={closeForm}` handler that fires when a text selection drag starts inside an input but the mouseup lands on the backdrop, causing the dialog to disappear unexpectedly.

The fix for Bug A removes the overly restrictive source check so that any file-based skill with valid steps always hydrates from disk. The fix for Bug B adopts the existing `onMouseDown` + `onClick` guard pattern (already used in `CustomDialog` and `MCPManagementPanel`) to distinguish intentional backdrop clicks from accidental drag-to-backdrop events.

## Glossary

- **Bug_Condition (C)**: The condition that triggers each bug — (A) `shouldHydrateSkillFromFile` returns `false` for non-hub skills with existing steps, (B) click event fires on backdrop when mousedown originated inside modal content
- **Property (P)**: The desired behavior — (A) disk-based steps always override config steps when valid, (B) dialog stays open when click did not originate on backdrop
- **Preservation**: Existing behaviors that must remain unchanged — (A) guard conditions for empty steps, name mismatch, config-only skills; (B) direct backdrop clicks and close/cancel buttons still close the dialog
- **shouldHydrateSkillFromFile**: Function in `gui/app_nl_skills.go` that decides whether a config-based skill's steps should be replaced with the on-disk file version during `loadSkills()` merge
- **loadSkills()**: Function in `gui/app_nl_skills.go` that reads skills from config.json, scans on-disk YAML files, and merges them — config skills are hydrated from file when `shouldHydrateSkillFromFile` returns true
- **makeBackdropProps**: Helper function in `MCPManagementPanel.tsx` that returns `onMouseDown` + `onClick` props implementing the guard pattern — only fires close when mousedown started on the backdrop itself
- **primaryDir**: The canonical skills directory (`~/.maclaw/data/skills`) used by hub-sourced skill hydration logic

## Bug Details

### Bug Condition A — Stale Cache

The bug manifests when a user registers a skill (which stores steps in `config.json`), then modifies the skill's `skill.yaml` on disk. The `shouldHydrateSkillFromFile()` function prevents hydration because the config skill already has steps (`len(configSkill.Steps) > 0`) and its source is not `"hub"`. The early `return false` at line ~126 blocks all non-hub skills from being refreshed from disk.

**Formal Specification:**
```
FUNCTION isBugConditionA(input)
  INPUT: input of type SkillMergePair (configSkill, fileSkill, primaryDir)
  OUTPUT: boolean

  RETURN input.fileSkill.Name != ""
     AND input.configSkill.Name == input.fileSkill.Name
     AND len(input.fileSkill.Steps) > 0
     AND len(input.configSkill.Steps) > 0
     AND input.configSkill.Source != "hub"
END FUNCTION
```

### Bug Condition B — Dialog Disappears on Select-All

The bug manifests when a user starts a text selection inside an input/textarea in the edit dialog (mousedown inside modal-content) and the mouse drag extends beyond the modal-content boundary so that mouseup/click lands on the modal-backdrop. The simple `onClick={closeForm}` on the backdrop fires unconditionally.

**Formal Specification:**
```
FUNCTION isBugConditionB(event)
  INPUT: event of type UserInteraction (mousedownTarget, clickTarget)
  OUTPUT: boolean

  RETURN event.mousedownTarget IS INSIDE modal-content
     AND event.clickTarget IS modal-backdrop
END FUNCTION
```

### Examples

- **Bug A Example 1**: User registers skill "my-deploy" (source="manual", steps=[bash: deploy.sh]). Later edits `skill.yaml` to add a new step. Opens edit dialog → sees old single-step definition, not the updated two-step version.
- **Bug A Example 2**: User registers skill "data-pipeline" (source="", steps=[bash: run.py]). Modifies description in `skill.yaml`. Opens edit dialog → sees old description from config.json.
- **Bug A Example 3**: Hub skill "hub-tool" (source="hub") with on-disk updates → correctly hydrated (existing behavior, not a bug).
- **Bug A Edge Case**: Config skill with steps but file skill has empty steps → should NOT hydrate (guard preserved).
- **Bug B Example 1**: User triple-clicks to select all text in the skill name input. Mouse drag extends to backdrop → dialog closes, losing edits.
- **Bug B Example 2**: User does Ctrl+A in description textarea, then clicks to reposition cursor but click lands on backdrop → dialog closes.
- **Bug B Example 3**: User clicks directly on backdrop (mousedown and click both on backdrop) → dialog closes (correct, preserved).

## Expected Behavior

### Preservation Requirements

**Unchanged Behaviors:**
- Skills that exist only in config.json (no on-disk file) continue to use config.json steps, triggers, and description as-is
- Skills that exist only on disk (not in config.json) continue to be added from the file-based scan
- File-based skills with empty steps (`len(fileSkill.Steps) == 0`) are never used to hydrate config skills
- Name mismatch guard (`fileSkill.Name == "" || configSkill.Name != fileSkill.Name`) continues to prevent hydration
- Hub-sourced skills in the primary skills directory continue to be hydrated from disk (existing logic preserved)
- `saveSkills()` continues to exclude file-based skills (source == "file") from config.json
- `openEditForm()` continues to call `loadData()` to refresh skills before displaying the edit dialog
- Direct backdrop clicks (mousedown on backdrop, then click on backdrop) continue to close all modal dialogs
- Close button (×) and Cancel button continue to close the edit dialog normally

**Scope:**
All inputs that do NOT involve the bug conditions should be completely unaffected by this fix. This includes:
- Config-only skills (no on-disk counterpart)
- Disk-only skills (no config counterpart)
- Hub-sourced skills (already hydrated correctly)
- Mouse clicks on buttons, inputs, and other interactive elements within the dialog
- Keyboard interactions that don't result in backdrop clicks

## Hypothesized Root Cause

Based on the bug description, the most likely issues are:

1. **Bug A — Overly Restrictive Source Check**: The `shouldHydrateSkillFromFile` function has a guard `if configSkill.Source != "hub" { return false }` at line ~126 that blocks hydration for ALL non-hub skills when the config skill already has steps. This was likely intended to prevent overwriting manually-entered steps, but it also blocks legitimate on-disk updates for skills registered via "manual" source or empty source. The fix should always return `true` when the file skill has valid steps, regardless of the config skill's source — the on-disk `skill.yaml` is the source of truth for steps.

2. **Bug A — Hub-Specific Path Check Becomes Dead Code**: After the source check, there's a `primaryDir` path comparison that only applies to hub skills. With the fix, this path check becomes unnecessary for the basic hydration decision, but the hub-specific directory validation may still be relevant for hub skills that need stricter path matching.

3. **Bug B — Missing mousedown Guard**: The modal backdrop uses `onClick={closeForm}` which fires on any click event, regardless of where the mousedown originated. The browser fires a click event on the element where the mouseup occurs if the mousedown was on the same element OR a descendant. When a drag starts inside modal-content and ends on the backdrop, the click event fires on the backdrop because `stopPropagation()` on modal-content only prevents bubbling, not the click synthesis from the drag.

4. **Bug B — Existing Pattern Not Applied**: The `CustomDialog` component and `MCPManagementPanel` already implement the correct `onMouseDown` + `onClick` guard pattern via `backdropMouseDownRef` and `makeBackdropProps()` respectively. The `SkillsManagementPanel` simply wasn't updated to use this pattern.

## Correctness Properties

Property 1: Bug Condition A — Non-hub skills with on-disk updates get hydrated

_For any_ SkillMergePair input where the bug condition A holds (configSkill and fileSkill have matching names, fileSkill has valid steps, configSkill has existing steps, and configSkill.Source is not "hub"), the fixed `shouldHydrateSkillFromFile'` function SHALL return `true`, allowing the on-disk steps to override the stale config steps.

**Validates: Requirements 2.1, 2.2, 2.3**

Property 2: Preservation A — Non-buggy inputs behave identically

_For any_ SkillMergePair input where the bug condition A does NOT hold (name mismatch, empty file steps, empty config steps, or hub source), the fixed `shouldHydrateSkillFromFile'` function SHALL produce the same result as the original function, preserving all existing guard conditions and hub hydration logic.

**Validates: Requirements 3.1, 3.3, 3.4, 3.5**

Property 3: Bug Condition B — Drag-to-backdrop does not close dialog

_For any_ user interaction where mousedown originated inside modal-content and the click/mouseup lands on the modal-backdrop, the fixed dialog SHALL remain open, preventing accidental dismissal during text selection.

**Validates: Requirements 2.4**

Property 4: Preservation B — Direct backdrop clicks still close dialog

_For any_ user interaction where both mousedown and click originate on the modal-backdrop (intentional dismiss), the fixed dialog SHALL close normally, preserving the existing dismiss behavior.

**Validates: Requirements 2.5, 3.8, 3.9**

## Fix Implementation

### Changes Required

Assuming our root cause analysis is correct:

**File**: `gui/app_nl_skills.go`

**Function**: `shouldHydrateSkillFromFile`

**Specific Changes**:
1. **Remove the source-based early return**: Delete the `if configSkill.Source != "hub" { return false }` guard. When the config skill has steps AND the file skill has valid steps, always return `true` — the on-disk file is the source of truth for steps regardless of source.
2. **Simplify the function**: After removing the source check, the hub-specific `primaryDir` path comparison becomes unnecessary for the basic hydration decision. The function simplifies to: return `true` when names match and file skill has valid steps (the `len(configSkill.Steps) == 0` early return is subsumed by the general `true` case).
3. **Preserve guard conditions**: Keep the initial guards: `fileSkill.Name == ""`, `configSkill.Name != fileSkill.Name`, and `len(fileSkill.Steps) == 0` all still return `false`.

**Simplified function after fix**:
```go
func shouldHydrateSkillFromFile(configSkill, fileSkill NLSkillEntry, primaryDir string) bool {
    if fileSkill.Name == "" || configSkill.Name != fileSkill.Name || len(fileSkill.Steps) == 0 {
        return false
    }
    return true
}
```

**File**: `gui/frontend/src/components/remote/SkillsManagementPanel.tsx`

**Function**: Edit/Create form dialog backdrop

**Specific Changes**:
4. **Add a `useRef` for backdrop mousedown tracking**: Add `const backdropMouseDownRef = useRef(false)` (or reuse the existing `makeBackdropProps` pattern from `MCPManagementPanel.tsx`).
5. **Replace `onClick={closeForm}` with mousedown+click guard**: On the edit/create form's `modal-backdrop` div, replace the simple `onClick={closeForm}` with `onMouseDown` + `onClick` handlers that only fire `closeForm` when `mousedown` started on the backdrop itself.
6. **Apply same pattern to detail dialog backdrop**: The detail skill dialog (`detailSkill && ...`) also uses `onClick={() => setDetailSkill(null)}` on its backdrop — apply the same guard pattern for consistency.

## Testing Strategy

### Validation Approach

The testing strategy follows a two-phase approach: first, surface counterexamples that demonstrate the bug on unfixed code, then verify the fix works correctly and preserves existing behavior.

### Exploratory Bug Condition Checking

**Goal**: Surface counterexamples that demonstrate the bug BEFORE implementing the fix. Confirm or refute the root cause analysis. If we refute, we will need to re-hypothesize.

**Test Plan**: Write unit tests for `shouldHydrateSkillFromFile` that pass in SkillMergePair inputs matching the bug condition (non-hub source, both have steps, matching names, valid file steps). Run these tests on the UNFIXED code to observe `false` returns. For Bug B, write React component tests that simulate mousedown inside an input and click on the backdrop.

**Test Cases**:
1. **Manual Source Stale Test**: `shouldHydrateSkillFromFile(configSkill{Source:"manual", Steps:[step1]}, fileSkill{Steps:[step1,step2]}, primaryDir)` → returns `false` on unfixed code (will fail assertion of `true`)
2. **Empty Source Stale Test**: `shouldHydrateSkillFromFile(configSkill{Source:"", Steps:[step1]}, fileSkill{Steps:[step1,step2]}, primaryDir)` → returns `false` on unfixed code
3. **File Source Stale Test**: `shouldHydrateSkillFromFile(configSkill{Source:"file", Steps:[step1]}, fileSkill{Steps:[step1,step2]}, primaryDir)` → returns `false` on unfixed code
4. **Drag-to-Backdrop Test**: Simulate mousedown on input inside modal-content, then click on modal-backdrop → dialog closes on unfixed code (will fail assertion of dialog remaining open)

**Expected Counterexamples**:
- `shouldHydrateSkillFromFile` returns `false` for all non-hub sources when config has steps
- Possible causes: the `if configSkill.Source != "hub" { return false }` guard at line ~126

### Fix Checking

**Goal**: Verify that for all inputs where the bug condition holds, the fixed function produces the expected behavior.

**Pseudocode:**
```
FOR ALL input WHERE isBugConditionA(input) DO
  result := shouldHydrateSkillFromFile'(input.configSkill, input.fileSkill, input.primaryDir)
  ASSERT result = true
END FOR
```

### Preservation Checking

**Goal**: Verify that for all inputs where the bug condition does NOT hold, the fixed function produces the same result as the original function.

**Pseudocode:**
```
FOR ALL input WHERE NOT isBugConditionA(input) DO
  ASSERT shouldHydrateSkillFromFile(input.configSkill, input.fileSkill, input.primaryDir)
       = shouldHydrateSkillFromFile'(input.configSkill, input.fileSkill, input.primaryDir)
END FOR
```

**Testing Approach**: Property-based testing is recommended for preservation checking because:
- It generates many test cases automatically across the input domain (varying source values, step counts, name combinations, primaryDir values)
- It catches edge cases that manual unit tests might miss (e.g., whitespace-only primaryDir, empty string source)
- It provides strong guarantees that behavior is unchanged for all non-buggy inputs

**Test Plan**: Observe behavior on UNFIXED code first for non-bug-condition inputs (hub source, empty file steps, name mismatches), then write property-based tests capturing that behavior.

**Test Cases**:
1. **Hub Source Preservation**: Verify hub-sourced skills with matching primaryDir continue to hydrate correctly
2. **Empty File Steps Preservation**: Verify file skills with no steps never trigger hydration
3. **Name Mismatch Preservation**: Verify mismatched names never trigger hydration
4. **Config-Only Preservation**: Verify skills without on-disk counterparts are unaffected
5. **Backdrop Direct Click Preservation**: Verify clicking directly on backdrop (mousedown+click both on backdrop) still closes dialog

### Unit Tests

- Test `shouldHydrateSkillFromFile` with all source values: "manual", "file", "", "hub", "agent_skill", "github", "clawhub"
- Test with various step count combinations: (0,0), (0,N), (N,0), (N,M)
- Test name matching: exact match, mismatch, empty names
- Test hub-specific path: matching primaryDir, non-matching, empty primaryDir
- Test backdrop mousedown+click guard: mousedown inside → click backdrop (no close), mousedown backdrop → click backdrop (close), mousedown backdrop → click inside (no close)

### Property-Based Tests

- Generate random `NLSkillEntry` pairs with random Source, Steps, Name, SkillDir values and verify:
  - When isBugConditionA holds → fixed function returns `true`
  - When isBugConditionA does NOT hold → fixed function returns same as original
- Generate random UI interaction sequences (mousedown target, click target) and verify:
  - When isBugConditionB holds → dialog stays open
  - When direct backdrop click → dialog closes

### Integration Tests

- Test full `loadSkills()` flow: register a skill, modify on-disk YAML, call `loadSkills()`, verify merged result has updated steps
- Test `openEditForm()` → `loadData()` → verify edit dialog shows fresh on-disk data
- Test edit dialog text selection: select text in input, drag to backdrop, verify dialog stays open
- Test edit dialog intentional dismiss: click backdrop, verify dialog closes
