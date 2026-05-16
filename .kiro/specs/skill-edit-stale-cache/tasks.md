# Tasks

## Task 1: Fix `shouldHydrateSkillFromFile` to always hydrate from disk when file has valid steps

- [x] 1.1 Simplify `shouldHydrateSkillFromFile` in `gui/app_nl_skills.go`: remove the `if configSkill.Source != "hub" { return false }` guard and the subsequent hub-specific `primaryDir` path comparison. The function should return `true` whenever names match and file skill has valid steps (`len(fileSkill.Steps) > 0`), regardless of config skill's source or existing steps count.
- [x] 1.2 Verify that `loadSkills()` merge logic in `gui/app_nl_skills.go` correctly uses the simplified `shouldHydrateSkillFromFile` — no changes needed to `loadSkills()` itself, but confirm the hydration block (`configSkill.Steps = fs.Steps`, etc.) still applies correctly.

## Task 2: Fix modal backdrop in `SkillsManagementPanel.tsx` to use mousedown+click guard

- [x] 2.1 Add a `useRef<boolean>` for backdrop mousedown tracking in `SkillsManagementPanel.tsx` (similar to `backdropMouseDownRef` in `CustomDialog.tsx`). Either import/reuse the `makeBackdropProps` helper from `MCPManagementPanel.tsx` or implement the same pattern inline.
- [x] 2.2 Replace `onClick={closeForm}` on the edit/create form's `modal-backdrop` div with the `onMouseDown` + `onClick` guard pattern that only fires `closeForm` when mousedown started on the backdrop itself.
- [x] 2.3 Apply the same mousedown+click guard pattern to the detail skill dialog's `modal-backdrop` (`onClick={() => setDetailSkill(null)}`) for consistency.

## Task 3: Write property-based tests for Bug A fix

- [x] 3.1 [PBT-exploration] Write a property-based test that generates random `SkillMergePair` inputs satisfying `isBugConditionA` (non-hub source, both have steps, matching names, valid file steps) and asserts `shouldHydrateSkillFromFile` returns `true`. Run on UNFIXED code to confirm the bug (expect failure — function returns `false`).
- [x] 3.2 [PBT-fix] After applying the fix from Task 1, re-run the exploration test from 3.1 to verify it passes — all bug-condition inputs now return `true`.
- [x] 3.3 [PBT-preservation] Write a property-based test that generates random `SkillMergePair` inputs where `isBugConditionA` does NOT hold (name mismatch, empty file steps, empty config steps, or hub source with matching primaryDir) and asserts the fixed function returns the same result as the original function. This ensures no regression for non-buggy inputs.

## Task 4: Write unit tests for Bug B fix

- [x] 4.1 Write a unit test that simulates mousedown inside modal-content (e.g., an input element) followed by click on modal-backdrop, and asserts the dialog remains open (closeForm not called).
- [x] 4.2 Write a unit test that simulates mousedown on modal-backdrop followed by click on modal-backdrop, and asserts the dialog closes (closeForm called) — preservation of intentional dismiss.
- [x] 4.3 Write a unit test that verifies the close button (×) and Cancel button still close the dialog normally.

## Task 5: Integration verification

- [x] 5.1 Verify the Go code compiles successfully after the `shouldHydrateSkillFromFile` change by running `go build ./gui/...` or equivalent.
- [x] 5.2 Verify the TypeScript code compiles successfully after the `SkillsManagementPanel.tsx` change by running the frontend build/typecheck.
