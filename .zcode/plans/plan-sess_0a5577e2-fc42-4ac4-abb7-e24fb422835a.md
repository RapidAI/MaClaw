# 编程知识总结与利用机制升级

## Context

现状：读取链路（任务开始自动召回 + `coding_knowledge_search`）已实现，但写入链路只覆盖远程 CodingSubAgent 和极少数本地路径；默认 `observe + on_retry_success`、全部落 candidate、人工确认是唯一晋升通道，导致本机 `coding_knowledge.db` 14 条全部是 candidate、`recall_count` 全为 0。另外设置页的 `coding_knowledge_auto_save_mode / save_strategy / max_per_project / max_total` 在后端 `PatchConfigFields` 没有 case，实际保存会失败。

目标：把“任务完成 → 提取/合并经验 → 多次独立观测 → 自动或人工晋升 → 下次任务召回 → 回收结果反写置信度”做成真正闭环；默认改为“成功即提取候选”（observe + on_success），用户选择 auto 时 2 次独立观测后自动转 active。

## Implementation

### 1. Store 层：观测累积 + 证据晋升 + 召回兜底

文件：`corelib/knowledge/coding_store.go`

- 给 `CodingExperience` / `CodingExperienceMetadata` 增加：
  - `ObservationCount int`
  - `ObservationTaskIDs []string`（有界，最多 16 个，用于审计与去重）
- 运行时经验首次保存时初始化 `ObservationCount=1`、`ObservationTaskIDs=[SourceRuntimeTaskID]`（`saveExperience` 内）。
- 新增 `CodingExperienceObservation{RuntimeTaskID, RuntimeAttemptID, EvidenceDigest, FailedAttempts, Labels, SourceTaskTitle}`。
- 新增 `RecordCandidateObservation(ctx, id, obs, verify)`：
  - 只允许 runtime-derived 的 candidate；新观测必须带完整 provenance 且通过 host verifier。
  - 同一 taskID 不重复计数；不同 task 才 `ObservationCount++` 并追加 taskID。
  - 合并新的 `FailedAttempts`/`Labels`（去重、限长、限数量），追加 `observation_recorded` lifecycle event。
- 新增 `PromoteCandidateFromObservations(ctx, id, minObservations, budget, verify)`：
  - 仅 candidate、仅 `CreatedBy=="runtime"`、`minObservations>=2`。
  - 要求 `ObservationCount >= minObservations`、当前 provenance 校验通过、reviewed budget 未超限。
  - 转 `active`，写 `LastReviewedAt`，追加 `candidate_auto_promoted` lifecycle event。
- 编辑/导入/导出/修订的保护：
  - `preserveExperienceLifecycleOnUpdate`（或新增 helper）禁止编辑器伪造观测字段。
  - `SaveImportedExperience`、`SanitizeExperienceForExport`、`CreateRevisionCandidate` 清空观测字段。
  - 旧记录 decode 时若 runtime-derived 且 count=0，补 1。
- 召回质量：`SearchExperiences` 在严格 FTS（Latin 多词是 AND）过滤后结果为空且 query 有多个有效词时，用 `buildFTSQuerySegmented` 对每个词生成表达式并以 `OR` 重试（复用 `quoteFTSTerm`）。仍只返回 active/verified，scope/project 过滤不变。
- `ContextPackForTask` 的 item 文本追加精简的“避免: …”/“不适用: …”（来自 FailedAttempts/Contraindications，限 2-3 条、限长），纳入现有 token 预算。

### 2. 写入层：默认策略、去重合并、auto 晋升

文件：`guiapp/coding_subagent_experience.go`、`guiapp/coding_knowledge_evolution.go`

- 默认 strategy 改为 `ExperienceStrategyOnSuccess`（Go 默认与前端默认同步），mode 保持 `observe`。
- `isDuplicateExperience` 改为返回命中的 `CodingExperience`，并按 `Scope` / `ProjectPath` / `Language` 过滤，避免跨项目、跨语言误合并。
- 重复命中时：
  - 仅当 existing 是 runtime-derived candidate 才 `RecordCandidateObservation`；manual/import/revision/active/verified 保持跳过。
  - mode=`auto` 时，用有效阈值（默认 2，新增配置 `coding_knowledge_auto_promote_observations`，0 表示默认 2）尝试 `PromoteCandidateFromObservations`；预算用 `codingKnowledgeReviewedProjectBudget(cfg)`，verifier 用 `app.verifyRuntimeCodingExperience`。
  - 新经验保存后 observation=1，不立即晋升。
- 提取 prompt 增加 `result.Localization` 的 root cause / error strings / rejected hypotheses（限长），提升 bug 修复类经验质量。

### 3. 本地覆盖：统一任务完成出口

文件：`guiapp/coding_subagent.go`、`guiapp/coding_subagent_orchestrator.go`

- 给 `CodingSubAgentResult` 增加包内 `knowledgeFinalized bool`。
- 新增 `finalizeLocalCodingKnowledge(handler, cfg, httpClient, task, result, projectPath)`：
  - 前置条件：result 非空、`RuntimeTaskID != ""`、非 `HorizonOwned`、状态归一化后为 passed/failed/skipped。
  - 用归一化后的浅拷贝调用 `recordRecalledExperienceOutcomes`（仅 passed/failed）和 `extractAndSaveExperience`；`wasRetry` 复用 `task.RetryCount > 0` / `codingResultLooksLikeRetrySuccess`。
  - 成功后设置 `result.knowledgeFinalized = true`。
- 在 `runTaskWithSubAgentRuntimeOptions` 末尾（`persistLocalizationExperience` 之后、`return result` 之前）调用该 finalizer。
- `SubAgentTaskRunner.runTaskHandle` 的三处既有调用（passed `:158-159`、failed `:168-169`、default `:210`）先检查 `result.knowledgeFinalized`，避免编排器路径双跑 LLM/回写。测试里 fake runner 返回的 result 没有 marker，行为不变。
- 嵌套 child 仍不写入（没有 ledger provenance），保持现状。

### 4. Horizon 死信修复

文件：`guiapp/app_coding_knowledge.go`（可新增 `app_coding_knowledge_horizon.go`）

- `verifyRuntimeCodingExperience` 对 `horizon:<taskID>` 前缀分派到新 verifier：
  - 解析 `horizon:` 前缀、`round-N`、`EvidenceDigest`。
  - `longhorizon.LoadTaskState(a.getMaclawBaseDir(), taskID)`，校验 `state.TaskID`、对应 round 的 `Audit.Digest`，可选重跑 `ExperienceEligible`。
  - 仅允许人工确认（`CodingKnowledgeConfirm`），不参与自动晋升。
- 其余 provenance 仍走 codingruntime ledger。

### 5. 配置持久化与 UI

文件：`corelib/app_config.go`、`guiapp/app.go`、`guiapp/config_settings_tab.go`、`guiapp/frontend/src/components/settings/CodingKnowledgeSection.tsx`、`guiapp/frontend/wailsjs/go/models.ts`

- 新增 `CodingKnowledgeAutoPromoteObservations int`（JSON: `coding_knowledge_auto_promote_observations`）。
- `PatchConfigFields` 补齐 case：
  - `coding_knowledge_auto_save_mode`（observe/auto/off）
  - `coding_knowledge_save_strategy`（always/on_success/on_retry_success/off）
  - `coding_knowledge_max_per_project` / `coding_knowledge_max_total`（正整数）
  - 新 auto-promote 阈值（0-20，0=默认 2）
  - 这修复了当前这四个设置保存失败的问题。
- `settingsTabFieldKeys["programmingTools"]` 加新 key。
- UI：默认 strategy 显示 on_success；auto 模式旁增加“自动晋升观测次数”数字输入（默认 2）和提示；observe/auto 文案改为准确语义。
- `models.ts` 手工补齐新字段和已缺失的 `max_reviewed_per_project` / `max_reviewed_tokens_per_project`（该文件是 Wails 生成但已签入；有条件时跑 `wails generate module` 重新生成）。
- 更新 `docs/coding-subagent-knowledge-accumulation-design.md` 的默认策略与 auto 语义。

## Verification

- `go test ./corelib/knowledge/... -count=1`
- `go test ./corelib/codingruntime/... -count=1`
- `go test ./guiapp -run 'TestCodingKnowledge|TestCodingExperience|TestRemoteCodingKnowledgePrompt|TestSettingsTabFieldKeys|TestPatchConfigFields|TestSubAgentTaskRunner' -count=1`
- `cd guiapp/frontend && npm test -- --run CodingKnowledgeSection`，并跑 `npx tsc --noEmit`（或 `npm run build`）。
- 新增测试覆盖：
  - 观测计数只认不同 runtime task；同 task 重复不增；verifier 失败保持 candidate。
  - 2 次观测后自动晋升；observe 不晋升；budget 超限不晋升；编辑器不能伪造观测字段。
  - 跨项目/跨语言重复不被误合并。
  - 英文多词 query 在严格 AND 无结果时 OR 兜底能召回 active 经验，candidate 仍被排除。
  - 本地 finalizer 对 passed/failed/skipped 的 gate；orchestrator 对 marker 的跳过。
  - horizon `task_state.json` 匹配可确认，篡改 digest 失败。
  - 设置项保存/重载、枚举与边界校验。
- 手动端到端：在临时项目连续跑两个相似的成功编码任务（auto 模式），确认第二次后 candidate 变 active；第三个任务的 system prompt 出现“相关编码经验”；任务结束后 `recall_count` 增加；观察 `coding_knowledge.db` 的 status/observation_count。

## Risks / Notes

- 默认 `on_success` 会让每个成功编码任务多一次轻量 LLM 提取调用（15s 超时、TaskSummary 路由）；这是用户选择的“平衡”档。
- 自动晋升只发生在用户显式选择 `auto` 时，且要求 2 个不同 runtime task 的观测 + provenance 校验 + reviewed budget；单次 LLM 产物不会直接进入 prompt。
- `ObservationCount`/`ObservationTaskIDs` 是新增 metadata 字段，旧记录 decode 需要默认值，避免破坏历史数据。
- `models.ts` 是生成文件；若 `wails generate` 不可用则手工补齐并注明。
- Horizon 校验依赖 `task_state.json`（与现有 horizon 持久化一致）；data_dir 迁移后旧 horizon 记录可能无法再确认，这是可接受的审计边界。