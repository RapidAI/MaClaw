# 干净任务环境与按需检索（中文摘要）

状态：**可以开工（rev 3 冻结）**。不重新打开挂载政策，除非有新的命名目标。本修订只补实施细则。
权威文本：[clean-working-set-on-demand-retrieval.md](clean-working-set-on-demand-retrieval.md)。

## 0. 冻结要点（rev 3.1）

1. 可选检索必须离开 required **wave**。`MaxSelections=1` 不能把意图一起打成 `planning_budget_exceeded`。
2. 可选预算溢出是 `Omitted`（`optional_budget_omitted`），不是 `Unmet`。GUI HostReject 只看 `len(plan.Unmet) > 0`。
3. Phase 2 只改 GUI 的 primary 政策。Core/Hub 先不要 Append（大量 `len(Needs)==1` 测试）。
4. 共享 `PromptKnowledgeBaseRules` 跟着 TUI 停 dump 一起改，不要提前改。

## 1. 决策

仓库正文用工具拉取，不进首轮 system prompt。

- 新任务 working set 为空（只有目录 + 拉取提示）。
- Host 不在 iteration 0 BM25 注入。
- 目录是指针，不是事实。
- 写入（ingest / knowledge admin）永远不是 ambient。

已落地：IM/Core catalog-only、extractor `OpNoop`、GUI 仅 coding 族 Append、非托管 name-pin。
未落地：required-only wave 的 planner，以及按 `result.Primary` 挂载。

## 2. WantsAmbientRetrieval(primary)

只用 `result.Primary`。`Labels()` 含 secondary；天气+PDF 是 `live_data` + `document_generate`。

返回 false：

- `IsNonCapabilityLabel`：`non_coding` / `continuation` / `ambiguous` / `unknown`
- 闭环：`audio_*` / `screenshot` / `computer_use` / `current_time` / delivery / `document_open` / `document_generate` / `app_launch` / `file_download` / `schedule_*` / config|session|template manage / `knowledge_write` / `knowledge_admin`
- 查找（已有检索工具）：`search` / `live_data` / `web_fetch`

| Primary | Secondary | Append |
| --- | --- | --- |
| `coding` / `document_read` / `file_read` | | yes |
| `live_data` | `document_generate` | no |
| `knowledge_read` | `document_generate` | yes |
| `non_coding` | | no（靠 pin） |

Append 仍加 knowledge + memory。light close 再丢掉 memory。不要在 resolve 时判断 light。
已实现的 Append 会按 NeedID 排序；`need:~ambient:*` 排在字母 ID **后面**。Phase 1/2 不要改这个排序。`Selections[0]` 由 planner 的 required-first 保证。

群组拒绝 knowledge：可选 need 走已有的 `policy_denied` → `Omitted`，不要加新分支。`knowledge_read` 作为 **required** 被拒仍是 `Unmet`（原有 fail-closed）。

## 3. Phase 1 算法（`applyPlanningBudget`）

签名改为同时传入 `req.Needs`。`PlannedSelection` 不加 `Required` 字段。

```
按 NeedID 查 Required（缺失则当 required）
required / optional 分区
先对 required 跑现有 wave 限额
  丢掉的 required -> Unmet
再用剩余名额/token 填 optional
  装不下或父 required 已丢 -> Omitted (optional_budget_omitted)
plan.Selections = required 前缀 + optional 填充
```

空 `Requires` 的 optional **不得**进入 required 第一波。这就是 `document_open` + ambient 被整波打死的根因。

`routingeval` 预算夹具没有 optional，预期不用改。`business_data` 紧预算应保住 MIS、省略可选 read。

默认 GUI `MaxSelections=0`（不限额）。Phase 1 在桌面 IM 上几乎看不见，直到测试设紧预算。

## 4. 阶段

1. Phase 1：planner required-only wave。不改挂载政策，不删 pin。
2. Phase 2：只改 GUI `classificationWantsAmbientRetrieval` → `WantsAmbientRetrieval(Primary)`。保留 pin。测试优先断言“有没有某 capability”，不要死磕 `len(defs)`。
3. Phase 3：TUI/Core 停 dump，再改共享提示词，再给 Core/Hub 做 Append。测试用 `dropAmbientRetrievalNeeds` 忽略 `need:~ambient:*`。
4. Phase 4：删 name-pin；VE / `/btw`；ReadOnly `memory.recall`。

## 5. 不做

- BM25 静默注入
- `AmbientRetrieval: true`
- 任意 label 黑名单
- 查找类“先仓库后网页”（南京天气会打进 KB）
- optional 留在 required wave
- Phase 2 同时给 Core Append
- 把 Append 缩成只加 knowledge
- Phase 1/2 删 pin
- 再开一轮挂载政策讨论

## 6. 残留风险（不为这些改政策）

- VE / `/btw` 仍 dump，直到 Phase 4
- TUI / Core 仍注入，直到 Phase 3
- 非托管 pin 留到 Phase 4
- Phase 2 会给 `ssh` / `browser` / `office` / `file_read` 加上仓库工具；用 capability 断言，不要用精确 grant 数
- light close 仍在 plan 之后丢掉 memory
