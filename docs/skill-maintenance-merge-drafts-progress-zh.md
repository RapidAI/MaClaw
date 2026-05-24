# Skill 重复合并 Draft 补充说明

日期：2026-05-22

## 本轮补充

`execute_maintenance_plan` 现在对 `merge_duplicate` 返回 `merge_draft`，默认不删除、禁用或改写任何 skill。

## merge_draft 内容

- `primary_skill`
- `duplicate_skill`
- `recommended_keep`
- `recommended_retire`
- `reasons`
- `primary_summary`
- `duplicate_summary`
- `recommended_action`

`recommended_keep` 通过本地可解释分数选择：成功次数、使用次数、失败次数、active 状态、是否已有参数契约、描述和目录证据。这个结果只作为审阅建议，不作为自动合并依据。

## 安全边界

- 不删除 skill。
- 默认不禁用 duplicate。
- 不合并 steps/docs。
- 不写 `skill.yaml` 或 config。

后续若要落地合并，应新增独立审批流：先展示 merge draft，再由用户指定保留项和退休项，最后走备份、patch、验证、索引刷新。
