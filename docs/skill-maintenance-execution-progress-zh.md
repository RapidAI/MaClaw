# Skill 维护执行器补充说明

日期：2026-05-22

## 本轮完成

新增 `manage_skill(action="execute_maintenance_plan")`，用于把 `maintenance_plan` 的一部分低风险建议变成审批式执行。

安全边界：默认 `dry_run=true`；当 `dry_run=false` 时必须同时传 `confirm=true` 和非空 `approved_actions`，执行器只处理批准列表里的动作，避免一次确认误执行整份计划。

当前可执行动作：

- `mark_needs_review`：把目标 skill 的 `Status` 设为 `needs_review`。
- `refresh_lifecycle`：清理已验证修复后的 `RepairAttemptCount` 和 `LastError`。
- `refresh_index`：返回 `requires_index_refresh=true`，GUI 侧刷新 skill cache、ToolRouter index 和 prompt skill summary。
- `archive_stale`：只对 `learned/crafted` skill 做元数据归档，把 `Status` 设为 `disabled`，不删除文件。
- `improve_contract`：对非 file-backed skill 从 step 模板补齐 `params/required_args`，覆盖空 schema、partial schema、仅 `required_args` 的旧 schema；file-backed skill 仍要求走 patch draft flow。

仍只规划、不自动改写的动作：

- `attempt_repair`：需要走现有 SelfRepair 流。
- `merge_duplicate`：需要人工确认主 skill 和合并策略。
- 文件/目录级归档：需要备份、恢复路径和归档位置；当前 `archive_stale` 只做 learned/crafted 元数据软禁用。

## 接入点

- 核心纯函数：`corelib/skill.ExecuteSkillMaintenancePlan`。
- GUI：`manage_skill(action="execute_maintenance_plan")`，非 dry-run 后保存配置并按需刷新索引。
- TUI：同名 action，默认 dry-run，可预览执行结果；扫描时会按 SkillDir 优先把 file-backed overlay 叠回 YAML 定义，写回时只保存运行态 overlay，避免把 YAML 定义体污染进 config。
- CoreAgent：当前 schema 暴露 action，但 provider 暂不支持执行，避免无持久化接口时误写。

## 验证

已补测试覆盖 dry-run 不变更、不确认拦截、缺少 `approved_actions` 拦截、批准后标记待审、生命周期清理、风险动作跳过、索引刷新信号，以及 GUI/TUI dispatcher 接入。
