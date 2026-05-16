# Tasks: Skill Source Reclassification

## Task 1: Add IsLearnedSource helper and update Source field comment
- [x] 1.1 Add `learnedSources` map and `IsLearnedSource(source string) bool` function to `corelib/types.go`
- [x] 1.2 Update the `Source` field JSON comment in `NLSkillEntry` to include `"auto_hub"`, `"auto_github"`, `"auto_clawhub"`
- [x] 1.3 Write unit test for `IsLearnedSource` covering all known source values and unknown/empty values

## Task 2: Set auto_ prefix in CapabilityGapDetector
- [x] 2.1 In `gui/capability_gap_detector.go` `Resolve()`, set `skill.Source = "auto_hub"` before `Register()` in the Hub install path (Step 6)
- [x] 2.2 In `gui/capability_gap_detector.go` `Resolve()`, set `imported.Source = "auto_github"` before `Register()` in the GitHub fallback path
- [x] 2.3 Update existing `CapabilityGapDetector` tests to verify auto_ source values

## Task 3: Set auto_ prefix in toolSearchAndInstallSkill path
- [x] 3.1 In `gui/im_message_handler.go` `installAndExecuteSkill()`, change the GitHub path to pass `"auto_github"` to `registerAndExecuteSkill` and set `imported.Source = "auto_github"`
- [x] 3.2 In `gui/im_message_handler.go` `installAndExecuteSkill()`, change the ClawHub path to pass `"auto_clawhub"` and set `skill.Source = "auto_clawhub"`
- [x] 3.3 In `gui/im_message_handler.go` `installAndExecuteSkill()`, change the SkillMarket path to pass `"auto_hub"` and set `skill.Source = "auto_hub"`

## Task 4: Update backend learned skill filters
- [x] 4.1 In `gui/skill_backup.go` `ExportLearnedSkillsZip()`, replace inline `Source == "learned" || Source == "crafted"` check with `IsLearnedSource(s.Source)`
- [x] 4.2 In `gui/app_nl_skills.go` `CleanupStaleSkills()`, replace inline `Source != "learned" && Source != "crafted"` check with `!IsLearnedSource(s.Source)`

## Task 5: Update frontend tab classification and UI
- [x] 5.1 Add `isLearnedSource()` helper function and `LEARNED_SOURCES` set to `SkillsManagementPanel.tsx`
- [x] 5.2 Update `installedSkills` and `learnedSkills` `useMemo` filters to use `isLearnedSource()`
- [x] 5.3 Update the `learnedNames` selection cleanup filter on data load to use `isLearnedSource()`
- [x] 5.4 Add `learnedSourceIcon()` and `learnedSourceTooltip()` helper functions for source-specific icons (🤖 for auto_, 📖 for learned, 🔧 for crafted) and localized tooltips
- [x] 5.5 Update the source icon `<span>` in the Learned tab table to use the new helper functions

## Task 6: Verify build and existing tests pass
- [x] 6.1 Run Go build to verify no compilation errors
- [x] 6.2 Run existing Go tests related to skill management (`gui/capability_gap_detector_test.go`, `gui/app_nl_skills_market_test.go`) to verify no regressions
