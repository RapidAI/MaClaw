# Skill SelfRepair 主动触发补充说明

日期：2026-05-22

## 本轮补充

`execute_maintenance_plan` 可处理 `attempt_repair`。核心执行器不直接调用 LLM，也不改写 skill；它只返回 `queued`，表示该 skill 已通过本地资格检查，应交给现有 SelfRepair 流程处理。

GUI/TUI 接入层在满足 `dry_run=false`、`confirm=true`、action 已批准时，才会把 `queued` skill 交给现有 SelfRepair：

- GUI：调用 `SkillRunner.maybeRepairSkill`，沿用安全扫描、持久化、索引刷新和 `skill:repaired` 事件。
- TUI：调用 `commands.MaybeRepairSkillTUI`，沿用 TUI LLM repairer。

## 安全边界

- dry-run 只展示会进入队列的动作，不触发 LLM。
- 核心执行器只做资格判断和排队信号，不依赖 provider。
- 真正修复仍由已有 SelfRepair 负责，包括 LLM、`ApplyRepair`、repair history 和持久化。
- 如果 SelfRepair LLM 未配置，GUI/TUI repairer 会跳过，不破坏 skill。
- file-backed skill 不进入后台自修复；修复必须走 reviewed patch flow，避免把 `skill.yaml` 管理的 steps 写进 config overlay。

## 结果字段

`execute_maintenance_plan` 返回：

- `queued_count`
- action `status: "queued"`
- `self_repair_triggers_started`

TUI 的 `self_repair_triggers_started` 只统计实际启动的后台 repair；LLM 未配置、skill 不符合条件或 file-backed patch-only 情况不计入。

