# MacLaw 技能自进化系统开发技术文档

## 背景

参考 Hermes 的技能进化闭环，MacLaw 已具备技能执行、使用统计、失败分类、自修复、临时脚本持久化、能力记忆注入等基础能力。当前缺口不是“有没有技能系统”，而是缺少稳定闭环：执行结果如何反向影响技能选择、技能修复、技能沉淀和定期治理。

目标：形成低风险、可审计、可逐步自动化的技能进化系统。

```text
用户对话 -> 技能选择/执行 -> 使用追踪 -> 失败分类 -> 自修复/降权/沉淀 -> 定期治理 -> 下一轮加载最新能力记忆
```

## 现有能力映射

| Hermes 模块 | MacLaw 现有模块 | 状态 |
| --- | --- | --- |
| 主对话循环 | GUI/TUI agent loop + manage_skill | 已有 |
| 记忆系统 | `corelib/memory` + `UsagePatternBridge` | 已有 |
| 技能系统 | `corelib/skill` + Hub/market | 已有 |
| 使用追踪 | `corelib/tool/usage_tracker.go` | 已有 |
| 后台审核代理 | `corelib/skill/self_repair.go` + experience learning | 部分已有 |
| 技能沉淀 | `corelib/skill/craft_to_skill.go` | 已有 |
| 策展/维护者 | `SkillMaintenancePlan` + approval-gated executor | 已接入 |

## 设计原则

1. 先计划，后执行：Curator 默认只生成维护计划，不直接删除、合并或改写技能。
2. 使用结果回流：成功率、失败率、修复次数、最后使用时间进入排序、记忆和治理。
3. 失败先分类：只有可修复错误进入自修复；限流、网络等外部错误只提示重试或降权。
4. 技能即资产：临时成功脚本应持久化；低质量技能应标记待审；重复技能进入合并候选。
5. 可审计：所有自动建议必须带 `reason`、`evidence`、`risk`、`recommended_action`。
6. 文件型技能只出草案：`skill.yaml` / `SKILL.md` 改写必须走 patch draft + 人审流程。

## 核心数据流

```text
Skill execution
  -> UsageTracker.RecordExperience
  -> ContextOutcomeScore / DistillRoutingHints
  -> SkillMemory prompt injection
  -> SelfRepair on non-file-backed repairable failure
  -> Curator maintenance plan
  -> human-approved execution later
```

## Curator 维护计划

`BuildSkillMaintenancePlan` 读取当前技能列表，输出只读 `SkillMaintenancePlan`。

- `mark_needs_review`：连续失败或成功率为 0 的技能。文件型技能的可修复失败也进入此治理流，避免后台改写磁盘定义。
- `attempt_repair`：非文件型技能最近失败可归类为可修复错误，且自修复次数未耗尽。
- `merge_duplicate`：名称/描述高度相似的 learned/crafted 技能。
- `archive_stale`：长期未使用、非 Hub 核心、低质量 learned/crafted 技能。
- `refresh_lifecycle`：修复后已有成功证据，建议清理修复计数。
- `refresh_index`：文件型技能修复或更新后，要求刷新 scan cache、router index、prompt summary。
- `improve_contract`：可执行技能有 steps，但参数契约不完整，包括空契约、partial `params`、仅 `required_args` 的旧契约。

只读计划示例：

```json
{
  "summary": "3 actions, highest risk medium",
  "actions": [
    {
      "action": "mark_needs_review",
      "skill": "pdf-converter",
      "risk": "medium",
      "reason": "0/4 successful runs",
      "evidence": ["usage_count=4", "success_count=0", "failure_count=4"]
    }
  ]
}
```

## 已实现闭环

### 1. 只读治理入口

`manage_skill(action=maintenance_plan)` 已接入 GUI/TUI/CoreAgent，返回本地、无 LLM、无网络依赖的 Curator JSON。

```json
{
  "ok": true,
  "non_executing": true,
  "boundary": "read-only skill maintenance plan; no skill was modified, archived, merged, deleted, installed, or executed",
  "plan": {
    "summary": "4 actions, highest risk medium",
    "actions": []
  }
}
```

工具 schema 已同步暴露 `max_actions`、`stale_after_days`、`min_failure_runs`、`duplicate_similarity` 等参数，避免 GUI/CoreAgent/TUI 能力说明漂移。

### 2. 审批式执行入口

`manage_skill(action=execute_maintenance_plan)` 默认 `dry_run=true`。真实执行必须同时满足：

- `dry_run=false`
- `confirm=true`
- `approved_actions` 非空

执行器只处理低风险元数据动作：标记待审、清理生命周期、刷新索引、learned/crafted 软归档、显式允许时软退休重复项。文件改写、目录移动、真实合并仍只返回草案或交给专用流程。

### 3. 技能列表健康标签

`manage_skill(action=list)` 已追加轻量健康标签：

- `[healthy]`：使用量足够、成功率高、最近无错误。
- `[needs_review]`：多次运行但成功数为 0。
- `[missing_contract]`：可执行技能存在 steps，但参数契约不完整；覆盖缺 `params`、partial `params`、仅 `required_args` 的旧格式。

列表只显示标签，不生成完整治理计划，避免列表操作过重。

### 4. 技能执行信号回流

GUI runner 与 TUI `manage_skill run` 在更新技能统计后，会把 `skill:<name>` 写入 `UsageTracker.RecordExperience`，包含：

- `success`
- `follow_up`
- `task_type=skill_execution`
- `error_class`
- `final_outcome`

这让技能执行结果进入现有经验学习、能力记忆和路由提示体系。

### 5. SelfRepair 与 Curator 协调

Curator 会优先为非文件型可修复失败建议 `attempt_repair`。缺少可修复证据、修复次数耗尽、错误不可修复，或技能为 file-backed 时，进入待审/降权/patch 治理。

```text
非文件型 + 可修复错误 + 未超过修复次数 -> SelfRepair
文件型 + 可修复错误 -> review/patch flow，不排后台 repair
SelfRepair 成功 -> repair_history，后续成功后 verified
SelfRepair 不适用/失败 -> Curator 建议 mark_needs_review
外部错误(rate_limit/network) -> 不修 skill，只记录 retryable warning
```

### 6. Patch Draft 与 Merge Draft

文件型技能的 `improve_contract` 不直接写文件，而是返回 `patch_draft`：

- `required_args`
- `params`
- `suggested_yaml`
- `recommended_action`

重复技能的 `merge_duplicate` 默认返回 `merge_draft`，包含保留/退休建议、双方摘要和评分理由。只有显式 `allow_duplicate_retire=true`，且退休目标为 learned/crafted，才会执行软退休。

## 安全边界

| 层级 | 动作 | 默认权限 |
| --- | --- | --- |
| Read-only | `maintenance_plan`、健康标签、失败摘要 | 允许自动执行 |
| Draft-only | `build_skill_draft`、`patch_draft`、`merge_draft` | 允许自动生成，不写文件 |
| Mutating | `mark_needs_review`、软归档、软退休、生命周期清理 | 必须用户确认 + 显式批准动作 |
| High-risk | 文件改写、目录移动、安装/卸载、真实合并 | 必须专用流程、备份、审计、可恢复路径 |

## 验收标准

- Curator 可在无 LLM、无网络环境运行。
- `maintenance_plan` 不修改输入技能列表。
- 每条建议包含 `action`、`skill`、`risk`、`reason`、`evidence`、`recommended_action`。
- active 且成功率高的技能不产生噪声建议。
- 0% 成功率、重复 crafted/learned、长期未使用 learned、参数契约不完整的技能能稳定产出建议。
- `execute_maintenance_plan` 在真实执行时必须检查 `confirm` 与 `approved_actions`。
- 文件型技能契约修复只返回草案，不直接改 `skill.yaml`。
- 执行后需要刷新索引的动作必须设置 `requires_index_refresh=true`。

## 后续优化

1. 给 mutating 动作补审计事件和回滚路径。
2. 将 `maintenance_plan` 的高价值建议写入 experience learning，供下一轮对话主动提示。
3. 在 UI 层增加 review queue，把 `patch_draft` / `merge_draft` 做成人审卡片。
4. 为 file-backed 失败补独立 repair patch draft，让可修复错误也能进入人审修复卡片。
