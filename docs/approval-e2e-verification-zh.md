# 审批工作流 × MaClaw E2E 验证手册

**关联**：[approval-maclaw-app-e2e-improvement-plan-zh.md](./approval-maclaw-app-e2e-improvement-plan-zh.md)  
**原则**：Hub WorkflowExecutor 为 SoT；桌面 registry / DataSrv 为投影。  
**一键脚本**：`scripts\run-approval-e2e-checks.cmd`（或 `.ps1`）

---

## 1. 一键自动化（CI / 本地）

在仓库根目录：

```bat
scripts\run-approval-e2e-checks.cmd
```

```powershell
powershell -ExecutionPolicy Bypass -File scripts/run-approval-e2e-checks.ps1
```

等价手工命令：

```powershell
go test ./hub/internal/workflow/ -count=1 -timeout 180s -run "TestChecklist_|TestEscalation_|TestHandleTimeout|TestOnEscalation|TestReconcileEscalations|TestShouldDefer|TestPersistEscalation|TestStartInstance_"
go test ./hub/internal/httpapi/ -count=1 -timeout 120s -run "TestHubWorkflowParticipantNotifier|TestClassifyWorkflowStatusEvent"
go test ./gui/ -count=1 -timeout 180s -run "TestApplyHubWorkflowStatusAttention|TestMaclawAppApprovalInstanceFromHubDirectoryItem|TestReconcile|TestMergeMaclawAppApprovalEscalation"
node hub/web/approval_workflow/workflow-editor.test.js
node hub/web/approval_workflow/i18n.test.js
```

### 清单与自动化对照

| # | 场景 | 自动化覆盖 | 实机必做 |
| --- | --- | --- | --- |
| 1 | 同用户双机 blocked / attention 推送 | `TestHubWorkflowParticipantNotifier_*`（多 machine + escalation extras） | 两台桌面同账号，触发 block/升级，两侧操作台 attention |
| 2 | 超时 fallback → overdue | `TestChecklist_TimeoutFallback*` | 可选：缩短 timeout 节点做实机 |
| 3 | Escalation max-retries | `TestChecklist_EscalationMaxRetries_*` / `TestEscalation_MaxRetriesExhausted` | 可选 |
| 4 | 无角色 empty-state | `workflow-editor.test.js`；`TestChecklist_EmptyApprovalRoles_*` | Admin 清空角色后打开设计器 |
| 5 | Hub 不可达 reconcile | `TestReconcileSkipsWhenHubUnreachable` | 停 Hub 后刷新操作台 |
| 6 | 会签多 peer 离线列表 | `TestChecklist_CountersignMultiOffline_*` / `TestStartInstance_Countersign*` | 可选 |
| 7 | any-N peer 耗尽仍可完成 | `TestChecklist_AnyNofM_*` / `TestOnEscalationFailed_AnyNofM_*` | 可选：any-2-of-3，一人永久离线 |
| 8 | Hub 重启恢复重试队列 | `TestReconcileEscalations_*` / attempts 续计 | **建议**：离线审批中重启 Hub，确认仍重投且不重置 attempts |
| 9 | peer-aware timeout | `TestHandleTimeout_DoesNotDeferWhenPeerAlreadyDelivered` / `TestShouldDefer*` | 可选：会签一人离线、一人已送达，超时仍应推进/block |
| 10 | UI 升级重投 / 离线耗尽 | 无前端 Vitest 契约（文案用 `\u` 转义） | 操作台列表徽章 + 详情 attempts（`a×3`） |

---

## 2. 架构速记（排障用）

| 概念 | 权威位置 |
| --- | --- |
| 谁还在重试 | 内存 `EscalationManager` 队列；持久 markers：`escalation_approvers` |
| 重试次数 | `instance_data.escalation_attempts`（map）；重启 `ReconcileEscalations` 续计 |
| 永久失败 peer | `escalation_exhausted_approvers`；any-N 可达则不 block |
| 超时是否 defer | **仅当**所有仍需未决 peer 都在队列中（`shouldDeferForEscalationRetries`） |
| 终态 | complete / fail / withdraw / block → `CancelForInstance` + 清 markers |
| 桌面投影 | `ve:workflow_status` extras + directory 字段 + App registry |

---

## 3. 实机步骤

### 3.1 预置

1. Hub 已发布审批工作流（建议配 FallbackApprover）。  
2. 租户 `approval_roles` 已绑定 assignee。  
3. 桌面登录 Hub，VE「启用审批」已开。  
4. enterprise_approval_app 已绑定 `hub_workflow_id`。

### 3.2 主路径冒烟

1. App 填表提交 → registry 有 `hub_instance_id`。  
2. 审批人收到 `ve:approval_request`（或操作台 pending）。  
3. 通过 / 驳回 → Hub 推进；DataSrv 同步。  
4. 操作台可见 Hub 标记与 urgency。

### 3.3 清单 #1 双机

1. 同一用户两台在线桌面。  
2. 触发 block 或升级失败通知。  
3. **期望**：两侧 `ve:workflow_status`；attention + urgency；有离线 peer 时列表出现紫色「升级重投」徽章（可带 `id×N`）。

### 3.4 清单 #4 设计器 empty roles

1. Admin → 审批角色清空 assignee。  
2. 设计器选审批人。  
3. **期望**：empty-state 引导；不读 localStorage 旧人。

### 3.5 清单 #5 Hub 抖动

1. 存在 hub 绑定本地实例。  
2. 停 Hub / 断网后刷新操作台。  
3. **期望**：不因不可达误标 missing。  
4. 恢复后对齐 directory。

### 3.6 清单 #7–#8（升级健壮性，建议发版前做一轮）

**any-N 耗尽**

1. 工作流 any-2-of-3。  
2. 使 1 个审批人永久不可达直至 max retries。  
3. **期望**：实例 **不** block；`escalation_exhausted_approvers` 含该人；另 2 人仍可凑满 2 票。  
4. UI：琥珀色「离线耗尽」徽章。

**Hub 重启续重试**

1. 会签 / 单人审批，审批人离线，出现「升级重投」。  
2. 重启 Hub 进程（不撤实例）。  
3. **期望**：日志含 `escalation reconcile restored`；attempts **不回 1**；人上线后仍能 redeliver。

### 3.7 清单 #9 peer-aware timeout

1. 会签两人：A 离线（escalation 队列），B 已收到请求未决。  
2. 等到节点 timeout。  
3. **期望**：**不会**因 A 在队列里永远 defer；节点按 timeout/fallback/block 规则处理（B 不被无限挂起）。

---

## 4. 日志关键字

```text
[maclaw-approval] event=
[workflow-notifier] delivered status
[workflow-notifier] no recipient machines
[workflow-escalation] reconcile: restored=
[hub-router] escalation reconcile
[hub-client] handleVEWorkflowStatus
escalation_failed
escalation_delivered
dispatch_partial_failure
critical write dropped
```

---

## 5. 通过标准（发布前）

- [x] 一键自动化全部绿（`scripts\run-approval-e2e-checks.cmd`）  
- [ ] 实机 #1 双机至少验证一次  
- [ ] 实机 #4 或 #5 至少验证一项  
- [ ] 建议：#7 any-N 或 #8 Hub 重启至少一项  
- [ ] 无回归：`approval_engine=local` 演示路径仍可决策  

---

## 6. 发布状态（仓库）

| 项 | 状态 |
| --- | --- |
| 审批 E2E 主栈 | 已合入 `origin/main` |
| 近期相关提交（节选） | `c75cb8c2` UI 文案/exhausted 样式；`0d4dfad4` attempts/exhausted 推送与展示；`ce41a713` attempts 持久化；`e65086af` any-N + peer timeout；`f67e8b9c` 重启 reconcile；`8076b1d9` CAS + 终态清队列 |
| 分支 | `main` 与 `origin/main` 对齐（本手册更新后以 push 为准） |
| 远程 | https://github.com/RapidAI/MaClaw |

> 手工清单 #1/#4/#5/#7/#8 为产品验收项，代码侧已有对应自动化契约，不阻塞合并，但发版前应勾选。
