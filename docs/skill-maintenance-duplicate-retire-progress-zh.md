# Skill 重复项退役执行补充说明

日期：2026-05-22

## 本轮补充

`execute_maintenance_plan` 现在支持在明确授权后退役重复 skill。

默认行为仍是只返回 `merge_draft`，不写入。只有同时满足以下条件才会执行：

- `dry_run=false`
- `confirm=true`
- `approved_actions` 非空且包含 `merge_duplicate`
- `allow_duplicate_retire=true`

## 执行动作

执行时只做元数据退役：

- 根据 merge draft 的 `recommended_retire` 找到要退役的 skill。
- 仅允许退役 `learned/crafted` 来源。
- 将 `Status` 设为 `disabled`。
- 在 `LastError` 写入 `retired_by_maintenance_duplicate` 标记和保留项名称。
- 不删除文件，不合并 steps，不改写保留项。
- 返回 `requires_index_refresh=true`。

## 安全边界

外部来源 skill，例如 hub/file/manual，不会被这个动作退役。它们仍只能通过人工审阅、patch 或卸载流程处理。
