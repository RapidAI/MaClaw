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

## 操作入口与自动进化控制

自动自修复 / 优化 / 发现（执行后喂入 `EvolutionPipeline`）受三层控制；**手动** `trigger_repair` / `trigger_optimize` 与详情/列表按钮不因配置关闭而禁用。

| 层级 | 机制 | 入口 |
| --- | --- | --- |
| Env | `MACLAW_DISABLE_SKILL_EVOLUTION=1\|true\|yes\|on` | 进程环境，优先强制关自动路径 |
| Config | `skill_evolution_enabled`（默认 true） | 通用设置勾选；技能 → 设置 → 进化；`manage_skill set_evolution_enabled`；`nlskill evolution enable\|disable --persist` |
| Session | 进程内 flag | TUI/CLI：`nlskill evolution enable\|disable`（无 `--persist`） |

相关 `manage_skill` 动作：

| action | 作用 |
| --- | --- |
| `evolution_status` | 只读管道状态（含 `env_disabled` / `config_enabled` / `disabled`） |
| `evolution_audit` | 只读持久审计（`limit`、可选 `name` 过滤；JSONL 路径见上） |
| `maintenance_drafts` | 只读收集 patch_draft / merge_draft / 排队自修复（dry-run） |
| `set_evolution_enabled` | `enabled=true\|false` 写入配置；TUI 在 enable 时清除 session 禁用 |
| `trigger_repair` | 强制/可选同步自修复 |
| `trigger_optimize` | 强制跳过门槛与 24h 节流的一次优化 |

GUI 技能页：详情「立即修复/立即优化」；列表快捷操作；进化页「运维速查」、**待处理**候选（批量修复/优化+进度+可取消）、**草案人审**（修复审阅包 / 契约补丁 apply+备份 / merge 两次确认退役）、**已退役/归档批量恢复**、**待审核批量通过**、**最近活动**与 **审计历史**（kind/时间/搜索/仅聚焦技能、导出另存为、加载更多、点技能名联动高亮，YAML 恢复类可回滚）。配置变更经 `config-changed` / `maclaw-config-changed` 实时同步。

## 操作手册（面向运维 / 日常使用）

本节是产品化后的**短流程**，与上面设计原则一致：只读可自动，写盘必须确认，坏了可回滚。

### 0. 数据落盘位置

| 用途 | 路径 |
| --- | --- |
| 进化审计（JSONL，最新在上） | `~/.maclaw/skill_evolution/audit.jsonl` |
| 文件型技能 YAML 版本备份 | `<skill_dir>/skill.yaml.vN` |
| 自动进化开关 | 配置 `skill_evolution_enabled`；环境 `MACLAW_DISABLE_SKILL_EVOLUTION` |

Windows 下 `~` 对应用户 profile / Maclaw 数据根目录（与 `corelib.MaclawBaseDir()` 一致）。

### 1. 打开 / 关闭自动进化

**目标**：停掉执行后自动 repair/optimize/发现；**不影响**详情页「立即修复/立即优化」和 `trigger_*`。

| 场景 | GUI | TUI / CLI | Agent 工具 |
| --- | --- | --- | --- |
| 看状态 | 技能 → 设置 / 进化页；通用设置勾选 | `nlskill evolution status` | `manage_skill action=evolution_status` |
| 本进程临时关 | — | `nlskill evolution disable` | — |
| 持久关 | 取消「技能自进化」勾选 | `nlskill evolution disable --persist` | `manage_skill action=set_evolution_enabled enabled=false` |
| 再打开 | 勾选开启 | `nlskill evolution enable --persist` | `enabled=true`（并清 session 禁用） |
| 强制全关 | 设环境变量后重启 | `MACLAW_DISABLE_SKILL_EVOLUTION=1` | 同上，env 优先 |

优先级：**Env 强制关 > Config / Session**。手动 `trigger_repair` / `trigger_optimize` 始终可用。

### 2. 日常巡检（只读）

1. **健康标签**：`manage_skill action=list` 看 `[healthy]` / `[needs_review]` / `[missing_contract]`。
2. **治理计划**：`manage_skill action=maintenance_plan`（不改任何技能）。
3. **人审草案**：GUI「草案人审」或 `manage_skill action=maintenance_drafts`  
   - `patch_drafts`：文件型契约补全建议（含 `suggested_yaml`）  
   - `merge_drafts`：重复技能合并建议  
   - `queued_repair`：可排队自修复项  
4. **审计**：GUI「审计历史」或 `manage_skill action=evolution_audit`（可选 `name`、`limit`）。

### 3. 人审：应用契约补丁（写盘）

**前提**：文件型技能，草案里已有 `improve_contract` / patch。

1. 在 GUI 草案卡中打开 YAML 预览 / 复制审阅包，确认无误。  
2. 点 **应用**（内部 `ApplySkillMaintenanceAction(kind=improve_contract, confirm=true)`）。  
3. 成功后：  
   - 当前 `skill.yaml` 已写入补丁  
   - 自动备份为 `skill.yaml.vN`  
   - 审计写入 `skill:maintenance_apply`  
   - 索引刷新（`requires_index_refresh`）  
4. 若结果不对 → 见 §5 回滚。

CLI/Agent 等价路径：先 `maintenance_drafts` 确认草案，再走 GUI 应用或专用绑定（勿直接手改 YAML 绕过备份，除非你自己做版本管理）。

### 4. 人审：合并重复并退役

1. 查看 merge 草案中的 **保留 / 退役** 建议与证据。  
2. GUI 两次确认后应用；真实执行必须：  
   - `confirm=true`  
   - `allow_duplicate_retire=true`  
   - 退役目标一般为 learned/crafted  
3. 退役后状态为 `disabled`，`last_error` 形如 `retired_by_maintenance_duplicate: kept <name> ...`。  
4. **恢复**：技能列表将状态改回 `active`（GUI 一键恢复 / `SetNLSkillStatus`）。  
   - 维护类 `retired_by_maintenance*` / `archived_by_maintenance*` 的 `last_error` 会清理  
   - **安全扫描**类 `last_error` 会保留作证据

### 5. 回滚 YAML（文件型）

1. GUI 技能详情 / 审计里对「YAML 恢复类」点回滚，或调用  
   `RestoreSkillYAMLBackup(skillName, version, confirm=true)`。  
2. `version<=0` 表示恢复**最新**备份。  
3. 恢复前会**再备份当前**文件（pre-backup），避免二次误操作无法挽回。  
4. 审计事件：`skill:yaml_restore`。

磁盘层也可手动：对比 `skill.yaml` 与 `skill.yaml.vN`，但推荐走 Versioner 绑定以保持审计一致。

### 6. 手动修复 / 优化（跳过自动门槛）

| 动作 | manage_skill | GUI |
| --- | --- | --- |
| 立即修复 | `action=trigger_repair name=<技能>` | 详情 / 列表「立即修复」 |
| 立即优化 | `action=trigger_optimize name=<技能>` | 详情 / 列表「立即优化」 |

用于自动管道关闭、或节流/门槛挡住但仍需处理的场景。非文件型可走 SelfRepair；文件型可修复失败仍以 review/patch 为主。

### 7. 批量维护执行（审批门）

`manage_skill action=execute_maintenance_plan`：

| 模式 | 参数 | 效果 |
| --- | --- | --- |
| 预演（默认） | `dry_run=true`（默认） | 只报告将做什么 |
| 真实执行 | `dry_run=false` **且** `confirm=true` **且** `approved_actions` 非空 | 仅执行已批准的低风险元数据动作 |

不会静默改 `skill.yaml`；文件改写必须走 §3 草案应用。

### 8. 故障速查

| 现象 | 排查 |
| --- | --- |
| 自动进化不跑 | `evolution_status`：`env_disabled` / `config_enabled` / session；是否从未跑过技能（管道懒启动） |
| 应用契约失败 | 是否 file-backed（有 `skill_dir`）；磁盘写权限；`confirm` 是否 true |
| 合并不生效 | 是否缺 `related_skill` 或 `allow_duplicate_retire` |
| 回滚失败 | 技能是否有 `skill_dir`；是否存在 `skill.yaml.vN` |
| 审计为空 | 是否发生过 repair/optimize/apply/restore；路径是否在当前用户 Maclaw 数据目录 |
| 恢复后仍带 last_error | 若是安全扫描证据则**有意保留**；维护退役/归档标记应已清除 |

### 9. GUI 进化页运维速查（产品化入口）

技能 → **进化** 页顶部「运维速查」可展开：

| 区域 | 常用动作 |
| --- | --- |
| 草案人审 | 修复审阅包 / 契约 apply+备份 / merge 退役；点技能名可聚焦审计 |
| 审计历史 | kind · 时间(1h/24h/7d/30d) · 搜索 · 仅聚焦技能 · 导出(确认行数+另存为) · 加载更多 |
| 待处理 | 全部立即修复/优化；进度条；**取消批量**跳过剩余 |
| 队列 | 退役批量恢复；待审核批量通过 |

### 10. 关键路径回归（开发）

推荐一键脚本（仓库根目录）：

```powershell
# Windows PowerShell
./scripts/test-skill-evolution.ps1
```

```bash
# Git Bash / *nix
bash scripts/test-skill-evolution.sh
```

等价手写命令：

```bash
go test ./corelib/skill/ -count=1 -timeout 90s \
  -run "TestApply|TestVersioner_|TestEvolutionAudit|TestCollectMaintenance|TestNormalizeManageSkillActionEvolution|TestOpsCritical|TestCollectHighValue|TestFileBacked|TestIsHighValue|TestBuildHighValue|TestBuildMaintenanceExperience|TestIngestHighValue|TestKindFromEventName"
go test ./gui/ -count=1 -timeout 120s \
  -run "TestSetNLSkillStatus|TestGetSkillEvolutionStatus|TestListSkillEvolution|TestPatchConfigFieldsSkillEvolution|TestBatch|TestBuildExperienceLearningSnapshotIncludesSkillMaintenance"
go test ./tui/ -count=1 -timeout 90s \
  -run "TestManageSkillHandler_AllCanonical|TestManageSkillHandler_Evolution|TestManageSkillHandler_SetEvolution"
```

覆盖：契约补丁+备份、Versioner 回滚、审计读写、草案收集、进化开关、退役恢复、批量状态/修复/优化、高价值 experience hints、manage_skill 别名与守卫。

### 11. 端到端 Manual QA 清单（发版前）

在 GUI 技能 → **进化** 页与技能详情完成下列检查。每条记 Pass / Fail / N/A。

#### A. 开关与只读
- [ ] 取消「技能自进化」后，执行技能不再触发自动 repair/optimize（`evolution_status` 显示 config 关）
- [ ] 环境变量 `MACLAW_DISABLE_SKILL_EVOLUTION=1` 重启后 banner 提示 env 关闭；手动「立即修复/优化」仍可用
- [ ] 运维速查可展开，文案三语可读

#### B. 草案人审
- [ ] 文件型可修复失败技能出现在 **修复草案**（含 error_class / action_hint）
- [ ] 修复草案：打开目录、复制审阅包、标记待审（审计出现 `mark_needs_review`）
- [ ] **契约补丁**：应用后生成 `skill.yaml.vN`，params 写入 YAML；可版本下拉回滚
- [ ] **合并草案**：两次确认后仅 metadata 退役；退役队列可恢复 active 并清维护 last_error

#### C. 审计历史
- [ ] kind / 时间 / 搜索 / 仅聚焦技能 过滤生效；计数显示匹配/总数
- [ ] 点修复草案技能名 → 审计聚焦；点审计技能名 → 打开详情并高亮草案
- [ ] 导出 JSON/CSV：先确认行数，再系统另存为；取消对话框无异常
- [ ] 长列表：加载更多 / 显示全部

#### D. 待处理与批量
- [ ] 修复候选「全部立即修复」：进度条前进，可 **取消批量**，剩余跳过
- [ ] 优化候选「全部立即优化」：同上
- [ ] 待审核「全部通过」、退役「全部重新启用」成功

#### E. 安全与边界
- [ ] 文件型技能不会被自动 SelfRepair 改盘；只出 draft
- [ ] 安全扫描类 last_error 在 approve 后仍保留；维护退役标记在 re-enable 时清除
- [ ] 关闭自动进化不影响手动 trigger

#### F. 回归命令
- [ ] `./scripts/test-skill-evolution.ps1`（或 `.sh`）全部 PASS

### 12. 功能冻结说明

自进化产品主线（控制面 → 人审 → 审计 → 经验回流 → 运维 GUI）视为 **feature complete**。后续默认只做：

1. bugfix / 文案校对  
2. 回归脚本维护  
3. 性能与可访问小幅 polish  

新能力（如云同步审计、跨设备草案）另开设计，不在本闭环默认范围内。

### 13. 交付验证记录

| 项 | 状态 |
| --- | --- |
| 一键回归 `scripts/test-skill-evolution.ps1` | PASS（corelib/skill · gui · tui） |
| Manual QA §11 | 待操作者在 GUI 勾选完成 |
| 阻塞编译修复 | `tui` `resolvePromptProfile` 双返回值；`RecordLightToolDeny` 与 stats 实现对齐（无重复声明） |

最后验证命令：

```powershell
./scripts/test-skill-evolution.ps1
```

### 14. 交付结论（handoff）

**状态：可交付（feature complete + 回归绿）**

闭环能力摘要：

| 层 | 能力 |
| --- | --- |
| 核心 | Curator 计划、审批执行、file-backed repair/contract draft、Versioner 备份回滚、高价值 experience 回流 |
| 工具 | `manage_skill`：plan/drafts/execute/evolution_status/audit/set_evolution/trigger_repair|optimize |
| GUI | 进化页：运维速查、草案人审、审计筛选/导出/聚焦、批量修复优化（可取消）、队列批量恢复/通过 |
| 质量 | Manual QA §11；`scripts/test-skill-evolution.ps1` / `.sh` |

**操作者待办（代码外）**

1. 在 GUI 按 §11 做一次人工冒烟并勾清单  
2. 合并/发版说明可引用本节与 §12 冻结范围  

**停止自动堆功能**：后续无明确 bug / 新设计需求时，不再扩展运维 UI 小功能。

## 验收标准

- Curator 可在无 LLM、无网络环境运行。
- `maintenance_plan` 不修改输入技能列表。
- 每条建议包含 `action`、`skill`、`risk`、`reason`、`evidence`、`recommended_action`。
- active 且成功率高的技能不产生噪声建议。
- 0% 成功率、重复 crafted/learned、长期未使用 learned、参数契约不完整的技能能稳定产出建议。
- `execute_maintenance_plan` 在真实执行时必须检查 `confirm` 与 `approved_actions`。
- 文件型技能契约修复只返回草案，不直接改 `skill.yaml`（须人审 apply + 备份）。
- 执行后需要刷新索引的动作必须设置 `requires_index_refresh=true`。
- 写盘类动作写入进化审计；YAML 回滚可恢复到备份版本。
- 自动进化可被 env/config/session 关闭，且手动 trigger 仍可用。

## 后续优化

1. ~~给 mutating 动作补审计事件和回滚路径。~~ **已完成**（apply/restore 审计 + Versioner）。
2. ~~将 `maintenance_plan` 的高价值建议写入 experience learning，供下一轮对话主动提示。~~ **已完成**（高价值过滤 → memory 任务产物 + UsageTracker 软信号 + 系统提示「技能治理提示」+ GovernanceDraftProvider）。
3. ~~在 UI 层增加 review queue，把 `patch_draft` / `merge_draft` 做成人审卡片。~~ **已完成**（GUI 草案人审 / 待审核队列 / 审计历史）。
4. ~~为 file-backed 失败补独立 repair patch draft，让可修复错误也能进入人审修复卡片。~~ **已完成**（plan 产出 `attempt_repair` + dry-run `patch_draft` kind=attempt_repair，含 error_class/action_hint/suggested review YAML）。
5. ~~自动进化 env/config/session 三层开关、GUI/TUI/manage_skill 入口、手动 repair/optimize、配置变更实时同步。~~ **已完成**。
6. ~~产品化：草案 apply、YAML 回滚、退役恢复、操作手册与关键路径回归。~~ **已完成**。
7. ~~GUI repair draft 专用卡片 + experience snapshot `skill_maintenance_hints`。~~ **已完成**。
8. ~~审计历史 kind 筛选 + 修复草案一键 mark needs_review（写审计）。~~ **已完成**。
9. ~~审计导出 JSON/CSV、批量标记/恢复、修复草案<->审计双向聚焦高亮。~~ **已完成**。
10. ~~审计技能名搜索、系统另存为导出、待处理候选批量立即修复。~~ **已完成**。
11. ~~优化候选批量立即优化、审计时间范围筛选、导出文件名含聚焦技能。~~ **已完成**。
12. ~~审计「仅聚焦技能」开关、批量修复/优化进度条、导出前行数确认。~~ **已完成**。
13. ~~批量可取消、审计分页加载更多、GUI「运维速查」帮助。~~ **已完成**。
14. ~~Manual QA 清单 + 一键回归脚本（`scripts/test-skill-evolution.*`）+ 功能冻结说明。~~ **已完成**。
