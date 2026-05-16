# Requirements Document

## Introduction

This feature reclassifies how skills are categorized between the "Installed Skills" (已安装技能) and "Learned Skills" (自学习技能) tabs in the Skills Management UI. Currently, the split is based solely on the `Source` field value: "learned"/"crafted" go to the Learned tab, everything else goes to Installed. The problem is that skills auto-installed by Maclaw via capability gap detection (source "hub"/"github"/"clawhub") land in the Installed tab alongside user-manually-installed skills, even though Maclaw autonomously found and installed them. The user expects these auto-acquired skills to appear in the Learned tab, since Maclaw "learned" to use them on the user's behalf. Only skills the user explicitly installed should remain in the Installed tab.

## Glossary

- **Skill**: An `NLSkillEntry` representing a natural language skill with steps, triggers, and metadata.
- **Source**: The `NLSkillEntry.Source` field indicating how a skill was created. Current values: `"manual"`, `"learned"`, `"hub"`, `"crafted"`, `"file"`, `"zip_import"`, `"github"`, `"clawhub"`.
- **CapabilityGapDetector**: The component that detects when the AI assistant lacks a capability and autonomously searches SkillHub/GitHub/ClawHub for a matching skill to install and execute.
- **Auto_Install**: A skill installation triggered by the CapabilityGapDetector or the `search_and_install_skill` tool during conversation, without the user explicitly choosing to install from the Hub UI.
- **Manual_Install**: A skill installation initiated by the user through the Skills Management Panel UI (Hub tab browse/install button) or TUI `skillhub install` command.
- **Installed_Tab**: The "已安装技能" tab in SkillsManagementPanel showing user-managed skills.
- **Learned_Tab**: The "自学习技能" tab in SkillsManagementPanel showing skills Maclaw acquired autonomously.
- **SkillsManagementPanel**: The React component (`SkillsManagementPanel.tsx`) that renders the skill management UI with Installed and Learned tabs.
- **SkillExecutor**: The backend component that manages skill registration, execution, and persistence.
- **ExportLearnedSkillsZip**: The function that exports learned/crafted skills to a zip archive for backup.
- **CleanupStaleSkills**: The function that auto-disables unused learned/crafted skills after 30 days.

## Requirements

### Requirement 1: New Source Value for Auto-Installed Skills

**User Story:** As a user, I want skills that Maclaw automatically found and installed to be distinguishable from skills I manually installed, so that I can see which skills Maclaw acquired on my behalf.

#### Acceptance Criteria

1. WHEN the CapabilityGapDetector installs a skill from SkillHub, THE CapabilityGapDetector SHALL set the skill Source field to `"auto_hub"` instead of `"hub"`.
2. WHEN the CapabilityGapDetector installs a skill from GitHub, THE CapabilityGapDetector SHALL set the skill Source field to `"auto_github"` instead of `"github"`.
3. WHEN the CapabilityGapDetector installs a skill from ClawHub, THE CapabilityGapDetector SHALL set the skill Source field to `"auto_clawhub"` instead of `"clawhub"`.
4. WHEN the `toolSearchAndInstallSkill` handler installs a skill via the `search_and_install_skill` tool call, THE handler SHALL set the skill Source field to `"auto_hub"`, `"auto_github"`, or `"auto_clawhub"` corresponding to the original source.
5. WHEN a user manually installs a skill through the SkillHubClient.Install method (Hub UI), THE SkillHubClient SHALL continue to set the Source field to `"hub"`.
6. WHEN a user manually installs a skill through the TUI `skillhub install` command, THE TUI handler SHALL continue to set the Source field to `"hub"`.
7. THE NLSkillEntry.Source field comment in `corelib/types.go` SHALL be updated to include `"auto_hub"`, `"auto_github"`, and `"auto_clawhub"` as valid values.

### Requirement 2: Frontend Tab Classification Update

**User Story:** As a user, I want skills that Maclaw auto-installed to appear in the "Learned Skills" tab alongside learned and crafted skills, so that I can see all skills Maclaw acquired autonomously in one place.

#### Acceptance Criteria

1. THE SkillsManagementPanel SHALL classify skills with Source `"learned"`, `"crafted"`, `"auto_hub"`, `"auto_github"`, or `"auto_clawhub"` into the Learned_Tab.
2. THE SkillsManagementPanel SHALL classify skills with Source `"manual"`, `"hub"`, `"file"`, `"zip_import"`, `"github"`, or `"clawhub"` into the Installed_Tab.
3. WHEN the Learned_Tab contains auto-installed skills, THE SkillsManagementPanel SHALL display a source icon distinguishing auto-installed skills (e.g., `🤖` for auto-installed) from experience-learned skills (`📖`) and crafted skills (`🔧`).
4. WHEN a user hovers over the source icon of an auto-installed skill, THE SkillsManagementPanel SHALL display a tooltip indicating the original source (e.g., "Auto-installed from SkillHub", "自动安装自 SkillHub").

### Requirement 3: Backend Learned Skill Filters Update

**User Story:** As a user, I want auto-installed skills to be included in learned skill operations (export, cleanup), so that they are managed consistently with other Maclaw-acquired skills.

#### Acceptance Criteria

1. THE ExportLearnedSkillsZip function SHALL include skills with Source `"auto_hub"`, `"auto_github"`, or `"auto_clawhub"` as eligible for export, in addition to `"learned"` and `"crafted"`.
2. THE CleanupStaleSkills function SHALL include skills with Source `"auto_hub"`, `"auto_github"`, or `"auto_clawhub"` as eligible for stale cleanup, in addition to `"learned"` and `"crafted"`.
3. THE learned skill selection cleanup logic in SkillsManagementPanel (the `learnedNames` filter on data load) SHALL include skills with Source `"auto_hub"`, `"auto_github"`, or `"auto_clawhub"`.

### Requirement 4: Helper Predicate for Learned Classification

**User Story:** As a developer, I want a single reusable predicate function to determine if a skill belongs to the "learned" category, so that classification logic is consistent across all call sites and easy to maintain.

#### Acceptance Criteria

1. THE codebase SHALL provide a Go helper function `IsLearnedSource(source string) bool` that returns true for Source values `"learned"`, `"crafted"`, `"auto_hub"`, `"auto_github"`, and `"auto_clawhub"`.
2. THE codebase SHALL provide a TypeScript helper function `isLearnedSource(source: string): boolean` with the same logic for frontend use.
3. WHEN a new auto-install source value is added in the future, THE developer SHALL only need to update the helper functions to include the new value.
4. THE ExportLearnedSkillsZip function SHALL use the Go `IsLearnedSource` helper instead of inline `Source == "learned" || Source == "crafted"` checks.
5. THE CleanupStaleSkills function SHALL use the Go `IsLearnedSource` helper instead of inline source checks.
6. THE SkillsManagementPanel SHALL use the TypeScript `isLearnedSource` helper for all tab filtering and selection logic.

### Requirement 5: Backward Compatibility for Existing Skills

**User Story:** As a user, I want my existing skills to continue working correctly after the update, so that the reclassification does not break my current skill library.

#### Acceptance Criteria

1. WHEN the application starts with existing skills that have Source `"hub"`, `"github"`, or `"clawhub"` (installed before this change), THE system SHALL continue to classify those skills in the Installed_Tab.
2. THE system SHALL NOT retroactively change the Source field of existing skills.
3. WHEN a skill with Source `"hub"` is executed, THE SkillExecutor SHALL handle the skill identically to a skill with Source `"auto_hub"` in terms of execution behavior.
4. IF a skill has an unrecognized Source value, THEN THE SkillsManagementPanel SHALL classify the skill in the Installed_Tab as a safe default.
