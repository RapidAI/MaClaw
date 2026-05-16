# Skill 运行次数/成功率统计增强与修复

## 问题概述

Skill 的运行次数（UsageCount）、成功次数（SuccessCount）、失败次数（FailureCount）统计存在多个问题：双重计数、字段缺失、路径不一致。需要统一修复，确保 skill runner 和 agent 运行 skill 两条路径都能正确、完整地统计。

## Bug 分析

### 问题 1 (P0): GUI 路径双重计数——失败场景 UsageCount 被加两次

**根因**：GUI agent 通过 `run_skill` 工具调用 skill 时，存在两条独立的统计路径：

1. **SkillRunner 内部统计**：`executeAsync()` 完成后调用 `updateUsageStats(skill, execErr)`，无论成功失败都 `UsageCount++`，失败时 `FailureCount++`
2. **Agent loop 外部统计**：`extractFailedSkillInfo()` 检测到 skill 失败后调用 `RecordSkillOutcome(sn, "failure", se)`，再次 `UsageCount++` 和 `FailureCount++`

**结果**：一次失败的 skill 执行，`UsageCount` 被加 2，`FailureCount` 被加 2。

**更严重的 workaround 场景**：skill 失败后 LLM 用其他工具完成任务，agent loop 先调用 `RecordSkillOutcome("failure")`，然后在 loop 结束时调用 `RecordSkillOutcome("workaround")`。加上 `updateUsageStats` 的一次，同一次执行 `UsageCount` 被加 3。

### 问题 2 (P0): TUI 路径双重计数——同样的问题

**根因**：TUI agent 通过 `run_skill` 工具调用 skill 时：

1. **toolRunSkill 内部统计**：执行完成后直接 `skill.UsageCount++` + `skill.SuccessCount++`（或设置 `LastError`）
2. **Agent loop 外部统计**：`extractFailedSkillInfoTUI()` 检测到失败后调用 `recordSkillOutcome(sn, "failure", se)`，再次 `UsageCount++` 和 `FailureCount++`

**结果**：与 GUI 相同的双重计数。

### 问题 3 (P1): SkillExecutor.Execute() 同步路径不记录 FailureCount

**根因**：`gui/app_nl_skills.go` 的 `Execute()` 方法在 `execErr != nil` 时只设置 `LastError`，不递增 `FailureCount`。

### 问题 4 (P1): TUI toolRunSkill 不记录 FailureCount

**根因**：`tui/agent_tools.go` 的 `toolRunSkill` 在失败时只设置 `LastError`，不递增 `FailureCount`。

## 修复方案

### 核心原则：统计职责归属单一化

让 SkillRunner / toolRunSkill 内部统计作为唯一的统计来源（single source of truth），移除 agent loop 中的重复统计。Workaround 检测保留在 agent loop 中，但改为只更新 `WorkaroundCount`，不重复增加 `UsageCount`。

### 修改清单

| 文件 | 修改 |
|------|------|
| `gui/app_nl_skills.go` | `Execute()` 失败分支补充 `FailureCount++` |
| `tui/agent_tools.go` | `toolRunSkill` 失败分支补充 `FailureCount++` |
| `gui/im_message_handler.go` | 移除 `RecordSkillOutcome("failure")` 调用；workaround 改为 `RecordWorkaround()` |
| `tui/agent_handler.go` | 移除 `recordSkillOutcome("failure")` 调用；workaround 改为 `recordSkillWorkaround()` |
| `gui/skill_runner.go` | 新增 `RecordWorkaround()` 方法（只增 WorkaroundCount，不增 UsageCount） |
| `tui/agent_handler_outcome.go` | 新增 `recordSkillWorkaround()` 方法（同上） |

## 验收标准

- 单次成功执行：UsageCount +1, SuccessCount +1, FailureCount 不变
- 单次失败执行：UsageCount +1, FailureCount +1, SuccessCount 不变
- 失败后 workaround：UsageCount +1, FailureCount +1, WorkaroundCount +1
- SkillExecutor.Execute() 同步路径失败：FailureCount +1（之前缺失）
- file-based skill：所有计数不变
- 成功率 = SuccessCount / UsageCount 准确反映实际成功率
