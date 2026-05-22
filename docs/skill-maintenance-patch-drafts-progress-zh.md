# Skill 维护 Patch Draft 补充说明

日期：2026-05-22

## 本轮补充

`execute_maintenance_plan` 在 file-backed skill 遇到 `improve_contract` 时返回 `patch_draft`，不直接改写 `skill.yaml`。

原因：file-backed skill 的 source of truth 是磁盘定义文件。只改 config overlay 会造成运行时契约和文件定义不一致。

当前覆盖范围：

- 空契约：没有 `params` / `required_args`，但 step 模板中出现 `{{input}}` 等占位符。
- 半截契约：已有部分 `params`，但模板中还有未声明参数，例如已有 `input`、缺 `output`。
- 旧契约：只有 `required_args`，没有完整 `params` schema。

## patch_draft 内容

返回字段挂在单条执行结果上：

- `kind`: `improve_contract`
- `skill`
- `skill_dir`
- `target_file`: `skill.yaml`
- `required_args`
- `params`
- `suggested_yaml`
- `recommended_action`

该 draft 是只读建议，不写文件、不安装、不执行。后续可由人工审阅后用 `manage_skill(action="patch")` 或直接编辑 `skill.yaml` 应用。

## 当前边界

- config-backed skill：可审批后直接补齐 `params/required_args`。
- file-backed skill：只生成 patch draft。
- `attempt_repair`：仍走 SelfRepair。
- `merge_duplicate`：仍需独立合并审阅流。
