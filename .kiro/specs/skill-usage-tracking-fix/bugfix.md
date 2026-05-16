# Bugfix Requirements Document

## Introduction

After a skill is successfully invoked via the agent (LLM calls `run_skill` tool), the skill management UI continues to display "未使用" (never used) with `usage_count` remaining at 0. The backend correctly updates usage statistics (`UsageCount`, `SuccessCount`, `LastUsedAt`) and persists them to disk, but the frontend is never notified to refresh its cached skill list. This affects both the GUI (Wails desktop app) and has a secondary data staleness issue in the TUI.

## Bug Analysis

### Current Behavior (Defect)

1.1 WHEN a skill is successfully invoked via the GUI agent (SkillRunner async path) and `updateUsageStats()` persists updated stats to disk THEN the system does not emit any Wails event to notify the frontend, causing the SkillsManagementPanel to continue displaying stale cached data with `usage_count = 0` and "未使用"

1.2 WHEN a skill is successfully invoked via the GUI synchronous path (`SkillExecutor.Execute()`) and usage stats are persisted to disk THEN the system does not emit any event to notify the frontend, causing the same stale display issue

1.3 WHEN the user opens or is already viewing the SkillsManagementPanel after a skill execution completes THEN the system shows "未使用" because `loadData()` is only called once on component mount via `useEffect` and there is no mechanism to trigger a refresh after async skill execution

1.4 WHEN a skill is invoked via the TUI agent (`toolRunSkill`) and stats are saved via `store.SaveConfig(cfg)` THEN subsequent operations that reload config may see stale in-memory data if the config is cached and not re-read from disk

### Expected Behavior (Correct)

2.1 WHEN a skill is successfully invoked via the GUI agent (SkillRunner async path) and `updateUsageStats()` persists updated stats to disk THEN the system SHALL emit a Wails event (e.g., `skill:usage_updated`) so the frontend can refresh the skill list

2.2 WHEN a skill is successfully invoked via the GUI synchronous path (`SkillExecutor.Execute()`) and usage stats are persisted to disk THEN the system SHALL emit a Wails event (e.g., `skill:usage_updated`) so the frontend can refresh the skill list

2.3 WHEN the SkillsManagementPanel is mounted and a `skill:usage_updated` event is received THEN the system SHALL automatically call `loadData()` to refresh the skill list, displaying the updated `usage_count`, `success_rate`, and `last_used_at` values

2.4 WHEN a skill is invoked via the TUI agent and stats are saved to disk THEN subsequent operations SHALL read fresh data from disk rather than relying on potentially stale in-memory config

### Unchanged Behavior (Regression Prevention)

3.1 WHEN a skill with `source == "file"` is invoked THEN the system SHALL CONTINUE TO skip usage stats updates (file-based skills cannot persist stats back to YAML)

3.2 WHEN the SkillsManagementPanel is first mounted THEN the system SHALL CONTINUE TO load the full skill list via `loadData()` on initial render

3.3 WHEN `updateUsageStats()` is called with a successful execution (`execErr == nil`) THEN the system SHALL CONTINUE TO increment `UsageCount` and `SuccessCount`, set `LastUsedAt`, and clear `LastError`

3.4 WHEN `updateUsageStats()` is called with a failed execution (`execErr != nil`) THEN the system SHALL CONTINUE TO increment `UsageCount`, set `LastUsedAt`, and record the error in `LastError`

3.5 WHEN the `SkillExecutor.List()` method is called THEN the system SHALL CONTINUE TO compute `SuccessRate` as `SuccessCount / UsageCount` and return the full `NLSkillDefinition` list

3.6 WHEN the user manually navigates away from and back to the SkillsManagementPanel THEN the system SHALL CONTINUE TO reload skills via the existing `loadData()` mechanism on remount
