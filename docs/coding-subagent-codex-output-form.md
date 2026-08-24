# CodingSubAgent / RemoteCodingSubAgent 输出形态改进（对齐 Codex）

用户可见输出的落地设计。机制（compact / verify loop / rollout）见 `docs/coding-subagent-codex-improvements.md`。

## 0. 本版相对上一版改了什么

上一版标题清单和「P0+P1 一起合」是对的，但数据面仍会踩坑：

1. **成功任务的下游上下文不该吃 `QualitySummary`。** `compactSubAgentPreviousResultSummary` 若优先返回质量字段，下一任务会再次看见审计腔。通过的前置任务只应传用户短文；失败重试才用 `ErrorSummary`（必要时加 `QualitySummary`）。
2. **远端无改动验收绑在 `Summary` 字符串上。** `verifiedGUIRemoteNoChangeResult` 靠 `strings.Contains(Summary, "[verified no-change acceptance]")`。只从用户正文拆掉标记会让 runtime 不再承认 verified no-change。必须改成结构化旗标。本地已经用字段判定（`verifiedGUINoChangeResult`），远端要对齐。
3. **`RemoteCodingSubAgentResult` 比本地瘦。** 没有 `QualityStatus` / `ExplorationSummary` / `VerificationSummary`。远端 P0 不能只改 `TaskItem`，要先补结果结构。
4. **停 `ExecuteTask` 的 `append*` 不够。** `SubAgentTaskRunner.runTaskHandle` 会再 `appendSubAgentQualityReportSummary` 一次；`coding_runtime_adapter` 还会把 `Final diff/merge gate failed` 拼进 `Summary`。
5. **不要另造一套 digest 公式。** adapter 已按 `result_summary` / `file_activity` / `verification` / `verified_no_change` 分条 hash。第一批只把远端 no-change 从「摘要含标记」迁到旗标；接受 `result_summary` digest 随短文变短而变一次。不要改成 `Status+Files+Commands` 的新拼法。
6. **AC 没有现成的字段判定。** 现网 AC 门是「摘要是否提到验收句」。删复述门等于第一批不再硬卡 AC 措辞。AC 仍写入任务描述约束模型；不要假装已有结构化 AC 检查器。
7. **用户看见 `Summary` 的入口不止聊天。** `im_tool_execution.go`、`semantic_delegate.go` 把 `result.Summary` 当回复正文；GUI 工作台 `onToken` 流模型字，收尾再用 `Summary` 落正式答案。所以拆的是这条正式答案，不是只改前端卡片。

## 1. 决策摘要

用户觉得本地 / 远端 coding subagent 不像编程 agent，是因为正式答案是验收报告，过程是任务看板。

三层根因：

1. `Summary` 同时当用户回复、`TaskItem.ResultSummary`、经验提取语料、部分 runtime 证据（远端 no-change 标记）。
2. 系统提示 +「摘要必须复述证据」门把模型写成审计员。
3. GUI 活动流是工具名 + QA 汇总，还只留最近 3 行。

目标：GUI = Codex 式 Read / Edit / `$ cmd` + 工程师短文；IM = 只有短文。工具门禁不削弱。

    第一批（必须一起合）：数据面拆分 + 停复述门 + 改回复合同
    第二批：GUI 活动流
    第三批：diff 卡片；中途短句可缓

## 2. 范围、非目标、不变量

### 2.1 范围

- 本地 `ExecuteTask` 收尾与远端 `applyRemoteVerificationOutcome` 对 `Summary` 的拼装。
- `RemoteCodingSubAgentResult` 补齐与判定相关的字段。
- `TaskItem.ResultSummary` 写入、`runTaskHandle` 二次拼装、`compactSubAgentPreviousResultSummary` / `currentTaskRetryOutputs`。
- `coding_runtime_adapter.go` 的 no-change 判定与「Final diff/merge gate」对 `Summary` 的改写。
- 用户入口：工作台正式答案、`im_tool_execution`、`semantic_delegate`。
- 提示合同：`buildCodingSubAgentSystemPrompt`、`buildRemoteCodingSystemPrompt`、inquiry / operational。
- GUI 事件与活动流（第二批）。

### 2.2 非目标

- 不重做 compact / verify loop / rollout / prompt cache。
- 不兼容 `codex exec` JSONL / App Server。
- 不削弱探索前置、新鲜验证、`git_diff`、破坏性命令、定位硬门。
- 不删 `CodingSubAgentResult` 上已有审计字段。
- 不给 IM 做假轨迹。
- 第一批不做结构化 AC 判定器。

### 2.3 不变量

- 门禁读工具审计，不读用户短文措辞。
- 用户可见正文不再出现第 4.2 节标题、方括号尾巴、`Final diff/merge gate failed` 段落。
- 本地 / 远端对用户同一套收尾；GUI 轨迹不暴露 `ssh_`。
- 失败重试仍有 `ErrorSummary`（及质量短字段）。
- 远端 verified no-change 在拆标记后仍然成立（改旗标，不删能力）。
- 本地 `verifiedGUINoChangeResult` 继续只读字段，不读 `Summary`。

## 3. 通道

| 通道 | 现在 | 目标 |
|---|---|---|
| GUI 工作台 | `onToken` 流模型字，正式答案 = 带标题的 `Summary`；活动流近 3 行 QA | 正式答案 = 短文；轨迹 = Read/Edit/`$` |
| IM / delegate_task / semantic_delegate | 直接发 `result.Summary` | 只发短文 |
| 编排器：通过的前置任务 | 从 `ResultSummary` 抽 `## 质量审计` | 只传用户短文 |
| 编排器：失败重试 | 同上 + `ErrorSummary` | `ErrorSummary`，必要时 `QualitySummary` |
| 经验提取 | 截 `Summary` 前 800 字 | 短文 + 已有 `CommandsRun` / `Error` |
| runtime ledger | `result_summary` hash 整段 `Summary`；远端 no-change 靠标记 | hash 变短后的短文（一次性变化）；no-change 改旗标 |

## 4. 现状

### 4.1 Codex

见 `docs/protocol.md` 第 2 节。用户 item 是 `assistant_message` / `command_execution` / `file_change` / `turn_diff`。

### 4.2 `Summary` 全部挂载点（用户能看见的）

本地 `ExecuteTask`：

    模型正文
    ## 文件变更 / ## 安全边界 / ## 探索状态 / ## 命令验证
    ## 动态工具 / ## 验证状态 / ## Diff 自检 / ## 质量审计 / ## 步骤清单

另外：

- `failedCodingSubAgentStartResult` 往 `Summary` 挂 `## 质量审计`
- `runTaskHandle` 在写入 `ResultSummary` 前再挂一次 `## 质量审计`
- `coding_runtime_adapter` 失败时追加 `Final diff/merge gate failed: ...`
- 本地 / 远端 attempt 尾巴 `Execution attempt:`

远端 `applyRemoteVerificationOutcome`：

    ## 探索状态 / ## 确认状态 / ## 验证状态 / ## 远程 Diff 自检
    ## 无改动证据 / ## 命令状态
    [repository inquiry] / [operational request] / [verified no-change acceptance]

`compactSubAgentPreviousResultSummary` 只认四个本地标题，认不出 `## 远程 Diff 自检`。

### 4.3 复述门（读措辞）与工具门（读审计）

复述门，第一批停用：

- `summarizeSubAgentChangedFileSummaryEvidence`
- `summarizeSubAgentRiskSummaryEvidence`
- `summarizeSubAgentAcceptanceCriteriaEvidence`
- `summarizeSubAgentVerificationCommandSummaryEvidence`
- `summarizeSubAgentClaimedVerificationEvidence`（要求摘要点名命令）
- `summarizeSubAgentScopeEvidence` 里「必须在摘要解释扩范围」的一半（文件是否越出 `task.Files` 仍可按路径集合判定）

工具门，保留：

- 未探索改已有文件、改后无新鲜验证、破坏性命令、定位硬门
- `summarizeSubAgentClaimedVerificationFailureEvidence`（声称通过但命令失败）
- 路径是否越出计划文件集（不依赖措辞）

### 4.4 活动流

`tool_started` / `tool_finished` 的 `detail` 是工具名；`command` 只给 bash；聊天 `slice(-3)`；收尾刷 8 种 `*_summary`。

### 4.5 两种结果结构不对称

`CodingSubAgentResult` 已有探索 / 验证 / 质量字段，本地 no-change 已读这些字段。

`RemoteCodingSubAgentResult` 只有 `Summary` / `Error` / 文件 / `CommandsRun`。审计被写进 `Summary`，runtime 再从正文里找标记。远端第一批的核心是补字段，不是只改 `TaskItem`。

`TaskItem` 仅内存结构（无 JSON 标签），加两个短字段成本低，但仍要把「通过 / 失败」两条消费路径分开。

## 5. 目标形态

GUI：

    . Read  gui/coding_subagent.go
    v Edit  gui/coding_subagent.go
    $ go test ./gui -run TestFoo   2.1s

    门禁仍在代码里跑，不再写进正式答案。
    `go test ./gui -run TestQualityReportHidden` passed.

IM：只有短文。

## 6. 数据面（按此实现，不要并列方案）

### 6.1 用户短文

    CodingSubAgentResult.Summary
    RemoteCodingSubAgentResult.Summary
        = compactSubAgentModelSummary(模型正文) 或极短 fallback

任何 `appendSubAgent*` / `appendRemote*` / inquiry 尾巴 / attempt 尾巴 / Final-diff 段落都不得再写入这两段。`append*` 函数可留作测试或调试辅助，第一批只断调用。

失败详情进 `Error` / `ErrorSummary`，不要回写用户短文。

### 6.2 远端结果补字段

`RemoteCodingSubAgentResult` 至少增加：

    QualityStatus / QualitySummary
    ExplorationSummary / VerificationSummary   // 与本地 no-change 判定对齐
    VerifiedNoChange bool                      // 取代摘要里的标记

`verifiedGUIRemoteNoChangeResult` 改为读 `VerifiedNoChange`（或与本地一样：质量通过且探索/验证摘要非空）。现有测例从「Summary 含标记」改为断言旗标。

### 6.3 TaskItem 与重试

    ResultSummary     = 用户短文（给「上一任务做了什么」）
    ErrorSummary      = 失败原因（已有）
    QualityStatus     = 新增
    QualitySummary    = 新增，只给失败重试

消费规则：

    通过的前置任务 -> ResultSummary（短文），禁止用 QualitySummary
    失败重试       -> ErrorSummary；若为空再用 QualitySummary

`runTaskHandle` 不再调用 `appendSubAgentQualityReportSummary`。`updateTaskResultSummary` / `RecordTaskResultSummaryForRun` 同时写短文与质量短字段。

### 6.4 digest 与 runtime

- 保留现有多条 evidence 类型。
- `result_summary` / child `EvidenceDigest` 继续 hash `Summary`（变短后 digest 变一次，可接受）。
- 远端 `verified_no_change` 改为 hash 结构化字段（与本地 `ExplorationSummary+VerificationSummary+QualitySummary` 对齐），判定走旗标。
- `Final diff/merge gate` 只改 `Status` / `Error`，不改用户 `Summary`。
- 不要引入第三种 digest 拼法。

### 6.5 Todo 与经验

`## 步骤清单` 不进 `Summary`。清单走已有 todo / step UI。

经验提取继续读变短的 `Summary` + `CommandsRun` + `Error`。

### 6.6 AC

第一批：AC 留在任务描述里约束模型；删除「摘要必须提到 AC」硬门。不在本设计实现 AC 字段匹配器。

## 7. 分阶段

### 第一批：数据面 + 复述门 + 回复合同

同一 PR（或两个连续 PR，第二个不得晚于第一个进主分支）。

P0 文件（断挂载 + 补字段 + 改判定）：

- `gui/coding_subagent.go`（finish、start-fail、attempt 尾巴）
- `gui/remote_coding_subagent.go`（`applyRemoteVerificationOutcome`、结果结构、inquiry/operational 尾巴）
- `gui/coding_subagent_orchestrator.go`（`runTaskHandle`、`compactSubAgentPreviousResultSummary`、TaskItem）
- `gui/task_execution_orchestrator.go`（写入 API）
- `gui/coding_runtime_adapter.go`（远端 no-change、Final-diff 不写 Summary）
- 用户入口无需改调用方式：它们已经读 `Summary`，短文变干净即生效

P1：

- 重写本地 / 远端完成标准：先结果；路径写正文；验证写事实；禁止审计标题；无真实风险不写 remaining risk
- 停用第 4.3 节复述门；保留工具门与反谎言

验收：

- 上述入口的成功正文不含 4.2 标题 / 标记 / Final-diff 段落
- 未验证改文件、未探索改已有文件、破坏性命令仍失败
- 不写 remaining risk 不再质量失败
- 通过任务的 `prevOutputs` 不含 `## 质量审计` / `PASSED:`
- 失败重试仍能看到上次错误
- `verifiedGUIRemoteNoChangeResult` 在无标记字符串时仍为真（旗标）
- `go test ./gui` 相关断言迁到字段

### 第二批：GUI 活动流

后端给 `tool_started` / `tool_finished` 填 `files` / `command`（及 exit）。前端把工具名映射为 `Read` / `Edit` / `Write` / `Search` / `$` / `Diff`，不把英文标签写进事件名。远端不暴露 `ssh_`。

聊天默认不渲染 quality / exploration / verification / guardrail / file_activity 汇总。取消 `slice(-3)`：本回合可滚动，或最近 20 行 + 展开。

验收：典型改文件能看出 Read -> Edit -> `$ test`；本地远端标签一致。

### 第三批：P3 / P4

P3：`Edited foo.go (+12 -3)` 进现有 preview。  
P4：中途短 `assistant_note`；最终正文仍以回合结束文本为准。

## 8. 已拍板

| 问题 | 决定 |
|---|---|
| 第二份 AuditReport Markdown | 不要 |
| 成功任务的下游上下文 | 只用用户短文 |
| 远端 no-change | 结构化旗标，不靠 Summary |
| digest | 不改公式；只迁判定输入 |
| P0 / P1 | 同一批；双向依赖 |
| AC 硬门 | 第一批去掉措辞门，不做新判定器 |
| `append*` 函数 | 先断调用，不急删 |
| 轨迹标签 | 前端映射英文 Read/Edit/`$` |
| IM 假轨迹 | 不做 |
| 步骤清单进回复 | 不进 |
| 反谎言（声称通过但失败） | 保留 |

## 9. 顺序与风险

    1. 第一批
    2. P2
    3. P3，P4 可选

风险：

- `runTaskHandle` 漏改会让编排器路径标题复发。
- 远端只拆标记不补旗标，会打破 no-change ledger。
- 复述门与挂载必须同批，否则模型或 `append*` 会把清单写回来。
- 测试面大：`coding_subagent_test.go`、`coding_subagent_orchestrator_test.go`、`coding_runtime_adapter_test.go`、remote / todo 测试。
- P2 取消 3 行上限后进度管道变胖，按回合折叠，不要每条都当永久聊天气泡。

## 10. 测试迁移

用户 `Summary` / 正式答案 / 通过任务的 `ResultSummary` 不得再含：

    ## 质量审计 / ## 验证状态 / ## 探索状态 / ## Diff 自检 / ## 文件变更
    ## 安全边界 / ## 命令验证 / ## 动态工具 / ## 步骤清单
    ## 远程 Diff 自检 / ## 确认状态 / ## 命令状态 / ## 无改动证据
    [repository inquiry] / [operational request] / [verified no-change acceptance]
    Final diff/merge gate failed

改断言到：`Error`、`QualityStatus`、`VerifiedNoChange`、`CommandsRun`、`FilesModified`。

保留工具门测试。删除「摘要没写 remaining risk / 没点名文件 / 没映射 AC -> 失败」。

第二批改前端活动流测试，并补本地 / 远端标签一致性。

## 11. 相关文档

- `docs/coding-subagent-codex-improvements.md`：机制。
- `docs/protocol.md` 第 2 节：Codex 事件。
- `docs/coding-subagent-architecture-design.md`：纯净上下文。
- `docs/coding-subagent-bug-localization.md`：定位硬门保留，不进用户标题。
