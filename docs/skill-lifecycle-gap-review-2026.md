# Skill 全链路闭环审查（2026-07）

## 闭环现状

```
发现 SearchAll (Hub/ClawHub/GitHub + 本地 rerank)
  → 下载 DownloadSkillHub / GitHub
  → 安装 CommitStaging + 安全扫描 + 依赖安装
  → 调用 SkillRunner / TUI skillRun
  → 错误 ClassifyStepError → LastError [class:] + experience
  → 自修复 EvolutionPipeline + maybeRepairSkill + RepairGate
  → 自改进 Optimizer / NudgePromoter / craft PersistCraftedSkill
```

## 文档 vs 代码（纠偏）

| 旧文档声称的 P0/P1 | 当前代码 | 状态 |
| --- | --- | --- |
| GUI 未接 self-repair | `skill_runner` + `EvolutionPipeline.RepairHook` | **已修复** |
| 错误分类三套分裂 | `corelib/skill/error_classifier.go` 统一 | **已修复** |
| craft 不落盘 | `tool_craft.go` → `PersistCraftedSkill` | **已修复** |
| 参数契约缺失 | `param_bind` / craft / repair / **info+JSON Schema** | **已加强** |
| file-backed 不自动修 | 有意：走 maintenance draft / patch 审核流 | **设计如此** |

## 已修缺陷

1. **进化通知用了运行时 skill 指针**：`UsageCount` / `LastError` 可能仍是旧值，导致 optimizer / repair 门槛误判。  
   → `updateUsageStats` 始终返回落盘后的 deep copy；`NotifySkillExecution` 使用该 copy。

2. **`OutputQuality` 恒为 `"basic"`**：成功跑完全部步骤也不标 good，弱化质量反馈。  
   → `skillRunOutputQuality`：全步骤成功 → `good`，失败 → `basic`。

3. **参数契约未进入 repair（P1）**  
   → `RepairContext` 增加 DeclaredParams / MissingRequired / UnknownArgs / ResolvedByAlias  
   → `NewRepairContext` + `EnrichRepairParamContract`（declared 来自 Params 或 SynthesizeParams）  
   → repair system prompt 区分「参数名错」vs「skill 逻辑错」  
   → GUI / TUI / EvolutionPipeline 统一走 `NewRepairContext`

## 已修：发现 degraded（P1）

- `HubClient.SearchAllFilteredReport`：每源 `Queried/OK/Count/Error/Skipped`
- 空结果 + 源失败 → 明确「搜索不完整」，避免误判「无 skill」
- 有结果 + 源失败 → GUI `manage_skill`/TUI 搜索结果带 ⚠️ 警告
- GUI `SearchDegradedError` 区分硬错误与部分失败

## 已修：session_not_found 经统一分类器（P2）

审计结论：GUI/TUI 失败路径已统一调用 `ClassifyStepError`，但 **`session_not_found` 从未进入 rules 表**（旧 TUI 独立分类遗留名），浏览器/remote 的 `session not found` / `session expired` 会落成 `unknown` 并误触发 skill self-repair。

- 新增 `ErrSessionNotFound` + `hasSkillSessionNotFoundMarker`
- 匹配：`session_not_found` / `session not found` / `session expired` / `invalid session` / `no such session`
- `Repairable=false`（改 skill 步无法恢复会话）、`Retryable=true`（重建 session 后可重试）
- `SkillExecutionFollowUp` → `retry`；hint → `reestablish_session`
- 旧文档 `skill-lifecycle-mechanism-analysis.md`、`memento-skills-inspired-improvements.md` 顶部标 **OBSOLETE**

## 已修：参数 schema 可观察 + JSON Schema（P3）

缺口：bind/repair 已有契约，但 agent **list 看不见 args**、无 inspect 动作、缺参错误不含完整 schema；`input_schema` 的 `type` 未进入 `NLSkillParam`。

- `NLSkillParam.Type` / `SkillYAMLParam.Type` 保留 JSON Schema type
- `ParamSchemaJSONObject` / `FormatParamSchemaJSON`：声明式 object schema 导出
- `FormatCompactParamTags`：`list` 行内 `params: input*, format`
- `manage_skill action=info`（别名 inspect/show/describe/get/schema/params）→ `FormatSkillInspectReport`
- 缺参消息（PrepareRunner + GUI precheck）附带 Parameter contract + JSON Schema
- Agent View 字段类型优先用声明 type

## 已修：从 SKILL.md 补参数描述（P3b）

合成/`params` 缺 description 时，agent 只看到裸参数名。

- `ExtractParamHintsFromDoc`：解析 Parameters/Arguments/参数 章节（bullet / table / CLI / typed）
- `EnrichParamsFromDoc` / `EnrichParamsFromSkillDir`：只填空字段，不覆盖显式声明
- `CompleteParamsForSkill`：Complete + 文档 enrich 的统一入口
- 接入：`info` inspect、repair 契约、taxonomy 上下文、缺参诊断、GUI agent-view / system prompt schema

## 已修：常见参数名 fallback 描述（P3c）

无参数章节 / 无 SKILL.md 时，合成参数仍会缺 description。

- `commonParamDescriptions` / `commonParamTypes`：input/output/format/limit/url/… 等常用名
- 复合启发式：`*_file` / `*_path` / `*_dir` / `*_url` / `*_id` / `is_*`
- `ApplyCommonParamDescriptionFallbacks`：只填空字段
- 优先级：**显式 YAML > SKILL.md > 常见名 fallback**（`enrichParamsForDiagnostics` 末尾应用）

## 已修：quota_exceeded + rate_limit 漏检（P3d / 观察项收口）

证据：`docs/llm-429-retry-and-recovery-design.md` 将 quota 列为 transient；OpenAI 等常返回 `insufficient_quota` / `rate_limit_exceeded` **无** 数字 429。旧 `hasSkillRateLimitMarker` 强制 `429 && phrase`，漏检；quota 落 `unknown` 会误触发 self-repair。

- `ErrQuotaExceeded`：`Repairable=false`，`Retryable=false`，FollowUp=`abandon`，hint=`inform_user`
- 匹配：`insufficient_quota` / `quota exceeded` / `exceeded your current quota` / credits exhausted 等
- **排除** `disk quota` / filesystem quota（避免与磁盘配额混淆）
- `rate_limit`：短语优先（含 `rate_limit_exceeded`），不再强依赖 429

## 已修：skill 生成即上传市场 → 成功运行阈值门禁（2026-07）

旧行为：生成管线 quality gate 用模拟成功执行打分 → 必 approved → `EnqueueUpload("generated_upload", proof=false, now=true)` 立即上传，零散 skill 涌入市场；旧的 3 次运行阈值（`AutoUploadTrigger.ShouldUpload`）已是死代码。

新行为：

- 生成只注册、不上传；唯一自动上传时机是运行成功后 `SkillRunner.tryAutoUpload`：`skill_auto_upload_enabled`（默认 true）且 `SuccessCount >= skill_auto_upload_min_successes`（默认 3，含本次，取自 `updateUsageStats` 落账副本）才 `EnqueueUpload`（质量门 + 内容 hash 去重 `findUploadedQueueItem`）
- 两个新配置均可经 `PatchConfigFields` 写入；`min_successes ≤0` 回退默认；自进化路径（`EvolutionPipeline.UploadTrigger`）同受总开关控制
- 存量兼容：上传 worker 启动时 `PurgeLegacyGeneratedUploads` 清除旧版 `generated_upload` pending/failed 队列条目（blocked/uploaded 保留；达标 skill 下次成功运行重新入队）
- write-only 的 `AutoUploadTrigger` 子系统（tracker/ShouldUpload/RecordExecution/MarkUploadedHash 及 manager 接口层 accessor）整体移除
- 服务端（hubcenter）：`POST /api/v1/skills` 直发口加 session token 鉴权（此前匿名可发）；上传者自报 `trusted/builtin/official` 一律降级 `community`（此前强制 trusted），HA 集群同步的污染面关闭

已知未决：`LoadConfig` 热路径不返回 NLSkills（`config_txn.go loadConfigSnapshot` 有根因注释），热路径补挂会改变 maclaw app install 的 provenance 判定，待 install 模块负责人定向。

## 后续改进计划（未做）

| 优先级 | 项 | 说明 |
| --- | --- | --- |
| **冻结** | skill 生命周期主链路 | 发现→安装→调用→分类→修复→契约→描述 enrich；无新证据不再扩 |
| 换轨 | 其它产品轨道 | adaptive/cost-route 已 closed；新目标需显式命名 |

## 建议冻结点

skill 生命周期（含参数契约与错误分类观察项）**已冻结**。默认「继续」不应再在本轨道叠加改动。

**已换轨（2026-07）**：AppView Phase 0 — AgentView `viewRevision` 过期提交保护，见 [appview-phase0-revision-guard.md](./appview-phase0-revision-guard.md)。
