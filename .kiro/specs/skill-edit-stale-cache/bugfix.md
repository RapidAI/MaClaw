# Bugfix Requirements Document

## Introduction

Two bugs affect the "编辑 Skill" (Edit Skill) dialog in maclaw:

**Bug A — Stale YAML Steps**: When a user registers a skill and later modifies the skill's source files on disk (e.g., `skill.yaml`), the edit dialog still displays the old/stale YAML steps content from `config.json` instead of the updated content from disk. The root cause is in `shouldHydrateSkillFromFile()` in `gui/app_nl_skills.go`, which prevents hydration from disk when the config-based skill already has steps stored and its source is not `"hub"`. For file-based skills, the on-disk `skill.yaml` should always be the source of truth for steps, triggers, and description.

**Bug B — Dialog Disappears on Select-All**: When a user selects all content in an input field within the edit dialog (e.g., via mouse drag or Ctrl+A with mouse interaction), the entire dialog disappears. The root cause is in `SkillsManagementPanel.tsx`: the modal backdrop uses a simple `onClick={closeForm}` handler. When a text selection drag starts inside an input but the mouseup lands on the backdrop area, the click event fires on the backdrop and closes the dialog. The `CustomDialog` component already solves this with a `onMouseDown` + `onClick` guard pattern that the skills panel should adopt.

## Bug Analysis

### Current Behavior (Defect)

1.1 WHEN a skill exists in both config.json (with non-empty steps) and on disk (with updated steps), AND the skill's source is not "hub" (e.g., "manual", "file", or empty) THEN the system returns `false` from `shouldHydrateSkillFromFile()`, ignoring the on-disk changes and keeping the stale config.json steps

1.2 WHEN a user modifies `skill.yaml` on disk (changing steps, description, or triggers) after the skill was registered, AND then opens the "编辑 Skill" dialog THEN the system displays the old steps/description/triggers from config.json, not the updated values from the on-disk `skill.yaml`

1.3 WHEN `loadSkills()` merges config-based skills with file-based skills for a skill that exists in both, AND `shouldHydrateSkillFromFile()` returns `false` due to the config skill having non-empty steps with a non-hub source THEN the system does not update `configSkill.Steps`, `configSkill.Triggers`, or `configSkill.Description` from the file-based version, even though the file version is newer

1.4 WHEN a user starts a text selection inside an input/textarea in the edit skill dialog (mousedown inside the input) AND the mouse drag extends beyond the modal-content boundary so that mouseup lands on the modal-backdrop THEN the system fires a click event on the backdrop which triggers `closeForm()`, causing the entire dialog to disappear and losing the user's unsaved edits

1.5 WHEN a user performs Ctrl+A in an input field and the browser's selection visually extends to the backdrop area, AND any subsequent mouse interaction (click/mouseup) lands on the backdrop THEN the system closes the dialog unexpectedly

### Expected Behavior (Correct)

2.1 WHEN a skill exists in both config.json (with non-empty steps) and on disk (with updated steps), AND the skill has a corresponding on-disk `skill.yaml` file THEN the system SHALL hydrate the config skill's steps from the on-disk file version, treating the disk as the source of truth for steps

2.2 WHEN a user modifies `skill.yaml` on disk and then opens the "编辑 Skill" dialog THEN the system SHALL display the latest steps, description, and triggers from the on-disk `skill.yaml` file

2.3 WHEN `loadSkills()` merges config-based skills with file-based skills for a skill that has a valid on-disk definition with non-empty steps THEN the system SHALL always update `configSkill.Steps` from the file-based version, and SHALL update `configSkill.Triggers` and `configSkill.Description` from the file-based version if the config versions are empty

2.4 WHEN a user starts a text selection inside an input/textarea in the edit skill dialog AND the mouseup lands on the modal-backdrop THEN the system SHALL NOT close the dialog, because the click did not originate on the backdrop (the mousedown was inside the modal content)

2.5 WHEN a user clicks directly on the modal-backdrop (both mousedown and click on the backdrop) THEN the system SHALL close the dialog as before (intentional dismiss behavior preserved)

### Unchanged Behavior (Regression Prevention)

3.1 WHEN a skill exists only in config.json and has no corresponding on-disk file THEN the system SHALL CONTINUE TO use the config.json steps, triggers, and description as-is

3.2 WHEN a skill exists only on disk (not in config.json) THEN the system SHALL CONTINUE TO add it to the skills list from the file-based scan

3.3 WHEN the file-based skill has empty steps (len(fileSkill.Steps) == 0) THEN the system SHALL CONTINUE TO not hydrate the config skill from the file version (no valid steps to hydrate with)

3.4 WHEN the file-based skill's name is empty or does not match the config skill's name THEN the system SHALL CONTINUE TO not hydrate (name mismatch guard)

3.5 WHEN a hub-sourced skill exists in both config.json and on disk in the primary skills directory THEN the system SHALL CONTINUE TO hydrate from the file version (existing hub hydration logic preserved)

3.6 WHEN `saveSkills()` persists skills to config.json THEN the system SHALL CONTINUE TO exclude file-based skills (source == "file") from config.json to avoid polluting the config

3.7 WHEN the frontend `openEditForm()` calls `loadData()` to refresh skills before displaying the edit dialog THEN the system SHALL CONTINUE TO re-fetch the full skill list from the backend, which now returns fresh on-disk data via the fixed `loadSkills()`

3.8 WHEN a user clicks directly on the modal-backdrop area (mousedown on backdrop, then click on backdrop) THEN the system SHALL CONTINUE TO close the edit dialog (intentional dismiss)

3.9 WHEN a user clicks on the close button (×) or the Cancel button in the edit dialog THEN the system SHALL CONTINUE TO close the dialog normally

---

### Bug Condition A — Stale Cache (Formal)

```pascal
FUNCTION isBugConditionA(X)
  INPUT: X of type SkillMergePair (configSkill, fileSkill, primaryDir)
  OUTPUT: boolean

  // The bug triggers when:
  // 1. Both config and file versions exist with matching names
  // 2. File version has valid steps (len > 0)
  // 3. Config version already has steps (len > 0)
  // 4. Config skill source is NOT "hub"
  // In this case, shouldHydrateSkillFromFile returns false,
  // preventing the disk version from overriding stale config steps.
  RETURN X.fileSkill.Name != ""
     AND X.configSkill.Name == X.fileSkill.Name
     AND len(X.fileSkill.Steps) > 0
     AND len(X.configSkill.Steps) > 0
     AND X.configSkill.Source != "hub"
END FUNCTION
```

### Fix Checking Property A

```pascal
// Property: Fix Checking — Non-hub skills with on-disk updates get hydrated
FOR ALL X WHERE isBugConditionA(X) DO
  result ← shouldHydrateSkillFromFile'(X.configSkill, X.fileSkill, X.primaryDir)
  ASSERT result = true
  // After loadSkills' merges, the config skill's steps reflect the disk version
  merged ← loadSkills'()
  skill ← merged.find(s => s.Name == X.configSkill.Name)
  ASSERT skill.Steps == X.fileSkill.Steps
END FOR
```

### Preservation Checking Property A

```pascal
// Property: Preservation Checking — Non-buggy inputs behave identically
FOR ALL X WHERE NOT isBugConditionA(X) DO
  ASSERT shouldHydrateSkillFromFile(X.configSkill, X.fileSkill, X.primaryDir)
       = shouldHydrateSkillFromFile'(X.configSkill, X.fileSkill, X.primaryDir)
END FOR
```

### Bug Condition B — Dialog Disappears (Formal)

```pascal
FUNCTION isBugConditionB(E)
  INPUT: E of type ClickEvent (mousedownTarget, clickTarget)
  OUTPUT: boolean

  // The bug triggers when:
  // 1. mousedown originated inside modal-content (e.g., an input field)
  // 2. mouseup/click lands on modal-backdrop
  // The simple onClick={closeForm} on backdrop fires, closing the dialog
  RETURN E.mousedownTarget IS INSIDE modal-content
     AND E.clickTarget IS modal-backdrop
END FUNCTION
```

### Fix Checking Property B

```pascal
// Property: Fix Checking — Drag-to-backdrop does not close dialog
FOR ALL E WHERE isBugConditionB(E) DO
  ASSERT dialog_remains_open(E)
END FOR
```

### Preservation Checking Property B

```pascal
// Property: Preservation Checking — Direct backdrop clicks still close dialog
FOR ALL E WHERE NOT isBugConditionB(E) AND E.clickTarget IS modal-backdrop DO
  ASSERT dialog_closes(E)
END FOR
```
