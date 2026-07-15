# 审批工作流 × MaClaw App 端到端改进计划

**状态**：Phase 0–18 完成 + 升级栈硬化（CAS / 重启 reconcile / any-N / peer timeout / attempts UI）  
**日期**：2026-07-15  
**验证手册**：[approval-e2e-verification-zh.md](./approval-e2e-verification-zh.md)（含实机 #1–#10）  
**发版短清单**：[approval-release-day-checklist-zh.md](./approval-release-day-checklist-zh.md)（约 15 分钟）  
**一键检查**：`scripts\run-approval-e2e-checks.cmd`  
**关联文档**：

- [maclaw-app-technical-guide.md](./maclaw-app-technical-guide.md)
- [ve-approval-capability-design-zh.md](./ve-approval-capability-design-zh.md)
- [approval-role-hub-workflow-linkage-design-zh.md](./approval-role-hub-workflow-linkage-design-zh.md)
- [app-panel-approval-ops-redesign-zh.md](./app-panel-approval-ops-redesign-zh.md)
- [appview-phase3-multi-region-approval.md](./appview-phase3-multi-region-approval.md)

---

## 1. 目标原则

> **业务审批只认 Hub 实例；MaClaw App 是入口与投影；VE 是可配置的自动审批节点。**

| 层 | 职责 | 权威性 |
| --- | --- | --- |
| Hub WorkflowExecutor | 图推进、会签/或签、超时升级、审计 | **Source of truth** |
| MaClaw App 本地 registry | 桌面队列、AppView、离线缓存 | Projection / cache |
| DataSrv | 业务记录 + 审批摘要字段 | Business data plane |
| VE 审批能力 | ACL/规则自动决策，回写 Hub | Executor node |

旁路审批（工具安全确认、入驻 approval、工作流版本 publish review）产品词表隔离，不混入本计划。

---

## 2. 现状与断层

### 2.1 已具备

- Hub 图执行、`ResumeInstance` CAS、多审批模式
- Decision API：`POST /api/v1/instances/{id}/nodes/{nodeID}/decision`
- Trigger API：`POST /api/v1/workflows/{id}/trigger`
- 真分发：`HubApprovalDispatcher` → `ve:approval_request`
- 角色表 CRUD + `ResolveApproverIDs` 骨架
- App 本地实例、全局列表、AppView 工作区、DataSrv 同步
- VE 规则管线 + `approval_capability_enabled` 上报

### 2.2 关键断层

1. **App 面板决策不接 Hub**：`DecideMaclawAppApprovalInstance` 只写本地 + DataSrv。
2. **启动路径可选 Hub**：`StartMaclawAppApprovalWorkflow` 以本地/skill 为主，缺稳定 `hub_instance_id` 契约。
3. **VE 响应通道脆弱**：discussion content 塞 JSON，未强制 DecisionAPI。
4. **角色动态解析未满**：`applicant_department` / `executionMode` 设计领先实现。
5. **状态模型分裂**：Hub / App / DataSrv / VE 四套字段无投影规则。

---

## 3. 统一 ID 契约（Phase 0）

`maclawAppApprovalInstance` 增加（或规范化）字段：

| 字段 | JSON | 含义 |
| --- | --- | --- |
| `ApprovalEngine` | `approval_engine` | `hub` \| `local` |
| `HubWorkflowID` | `hub_workflow_id` | Hub 已发布工作流 ID |
| `HubInstanceID` | `hub_instance_id` | Hub 运行实例 ID |
| `HubNodeID` | `hub_node_id` | 当前阻塞审批节点 ID |
| `HubSyncError` | `hub_sync_error` | 最近一次 Hub 写失败摘要 |

兼容：

- `approval_workflow_id` 在 engine=hub 时可作为 `hub_workflow_id` 回退来源。
- `workflow_skill_id` 仍表示本地/技能执行面，可与 Hub 工作流并存。
- skill 结果 JSON 可携带 `hub_instance_id` / `hub_node_id` / `hub_workflow_id`。

决策语义：

| 场景 | 行为 |
| --- | --- |
| `approval_engine=hub` 且具备 instance+node | **先** Hub decision，成功后再投影本地 + DataSrv |
| Hub 失败 | 不落「已通过/已驳回」终态；标 `attention` + `hub_sync_error` |
| `approval_engine=local` 或无 Hub 绑定 | 保持本地决策（兼容演示/单机） |

---

## 4. 全流程目标态

```text
[设计] Hub 审批角色 + 工作流设计器 → 发布
[安装] enterprise_approval_app pack（DataSrv + workflow 绑定）
[启用] 桌面 VE「启用审批」→ Hub 目录可见数字分身

[提交]
  App 填表
    → StartMaclawAppApprovalWorkflow
         → 本地 registry (pending, my_requests)
         → DataSrv sync
         → POST /api/v1/workflows/{hub_workflow_id}/trigger  (engine=hub)
         → 回写 hub_instance_id / hub_node_id
    → OpenMaclawAppApprovalWorkspace (AppView)

[审批节点]
  Hub ResolveApproverIDs → Dispatch ve:approval_request
    → VE: ACL/队列/规则 → auto 或 require_human
    → 人工: App 操作台 / AppView / Hub API

[决策]
  POST .../instances/{id}/nodes/{nodeID}/decision
    → ResumeInstance
    → 投影本地 + DataSrv + 通知
```

---

## 5. 分阶段计划

### Phase 0 — 契约与真相源

- [x] 本计划文档
- [x] 实例字段：`approval_engine` / `hub_*`
- [x] normalize / merge / payload 解析兼容
- [x] 契约单测样例固化（`gui/app_maclaw_app_approval_hub_test.go`）

### Phase 1 — App ↔ Hub 主链路（当前开工）

- [x] Start：可选/自动触发 Hub workflow，回写 instance/node
- [x] Decide：engine=hub 时调用 Decision API
- [x] Hub 失败 → attention，避免假成功
- [x] mock Hub 集成单测（trigger / decision / failure attention）

### Phase 2 — VE 分发硬化（本轮已落地）

- [x] 桌面接收 `ve:approval_request` → VE 规则管线（此前未路由，已修复）
- [x] 自动决策经 Decision API → `ResumeInstance`（`approve`/`reject` wire 格式；不再塞 discussion content）
- [x] details 中 requester 部门/角色/技能注入 ACL
- [x] require_human → 本地 registry `pending_my_approval` + `ve:approval-require-human` 事件
- [x] Hub 决策失败 → 本地 `attention` + `hub_sync_error`
- [x] Approver Directory 仅返回 `active` 且 `approval_capability_enabled` 的 VE

### Phase 3 — 审批角色运行时（本轮已落地）

- [x] `role:dynamic:applicant_department:<roleCode>` → 申请人部门 → `role:department:<deptID>:<roleCode>`
- [x] 固定部门 / 职能角色原有展开保留
- [x] executionMode 编排：manual 人先 / digital_* 机先 / auto 仅数字
- [x] 解析上下文：`ApprovalResolveContext`（instance data → applicant / department）
- [x] 无人可解析：节点 FallbackApprover → 通知发起人并 fail
- [x] 去掉设计器 localStorage 生产路径（Phase 7：Hub-only 角色源）

### Phase 4 — 操作台体验（本轮已落地聚合）

- [x] 操作区「审批状态 / 运行记录」已有 UI（`AppsPage` ops）
- [x] `ListMaclawAppApprovalInstancesAll` 聚合 **本地 + DataSrv + Hub directory**
  - pending → `/api/v1/directory/pending-action`
  - my_requests → `/api/v1/directory/initiated`
  - handled → `/api/v1/directory/completed`
- [x] Hub 项映射为 `app_id=hub-workflow`、`approval_engine=hub`、带 `hub_instance_id/node_id`
- [x] 操作台通过/驳回对 Hub 绑定实例走 `DecideMaclawAppApprovalInstance`
- [x] 列表展示 Hub 标记与 hub instance id
- [x] technical-guide 审批章节对齐当前投影/操作台（非 tool_app 全文重写）

### Phase 5 — 观测与对账（本轮已落地）

- [x] 统一 trace：`[maclaw-approval] event=… app_id=… hub_instance_id=…`
- [x] `ReconcileMaclawAppApprovalProjections`：Hub directory SoT 对齐本地
  - Hub 已完成 → 本地标 handled
  - Hub 可达但目录无此实例 → 本地 attention（missing）
  - Hub 不可达 → **不改**本地（防误伤）
  - Hub pending 本地缺失 → upsert 缓存
  - Hub initiated 侧 attention/escalation → 亦 upsert 离线缓存
- [x] `ListMaclawAppApprovalInstancesAll` 刷新前 soft-reconcile
- [x] 操作台刷新显式调用 Reconcile + List
- [x] EscalationManager → directory/push/reconcile → attention + 升级重投徽章

---

## 6. MVP 验收（Phase 1）

1. 启动审批应用后，本地实例含 `hub_instance_id`（Hub 可用时）。
2. AppView / 操作台「通过」调用 Hub decision，Hub 节点推进。
3. Hub 不可达时本地不标最终 approved，而标 attention。
4. 无 Hub 绑定的 local 引擎仍可本地决策（兼容）。
5. DataSrv 在 Hub 成功后同步业务状态。

---

## 7. 代码地图

| 区域 | 路径 |
| --- | --- |
| App 审批实例 | `gui/app_maclaw_app_approval.go` |
| App 类型 | `gui/app_maclaw_app_types.go` |
| AppView 决策 | `gui/agent_view_maclaw_app.go` |
| Hub 触发/决策客户端 | `gui/app_maclaw_app_approval_hub.go`（本轮新增） |
| Hub 执行器 | `hub/internal/workflow/executor.go` |
| Decision API | `hub/internal/workflow/api_decision.go` |
| Trigger API | `hub/internal/workflow/api_instance.go` |
| 分发 | `hub/internal/httpapi/workflow_runtime_deps.go` |
| VE 处理 | `gui/ve_approval_handler.go`, `gui/app_ve_handler.go` |

---

## 8. 风险与缓解

| 风险 | 缓解 |
| --- | --- |
| 双写无事务 | Hub 成功为准；本地乐观后失败回滚到 attention |
| Machine token vs 审批人身份 | 使用现有 Hub Bearer 鉴权；Decision 403 时明确错误与 attention |
| 仅 skill 无 Hub | `approval_engine=local` 显式标记，企业包后续强制 hub 绑定 |
| 名词过载 | 业务审批 / 工具确认 / 入驻 / 发布审核 分词 |

---

## 9. 交付记录

### Phase 0/1（已完成）

1. 文档与 ID 契约字段  
2. Hub trigger / decision HTTP 客户端  
3. Start / Decide 接线  
4. mock Hub 单测  

### Phase 2（已完成）

| 项 | 路径 |
| --- | --- |
| 入站路由 | `gui/remote_hub_ve_events.go` → `ve:approval_request` |
| VE 决策回写 | `gui/app_ve_handler.go` → Decision API |
| require_human | 同上 + 本地 `hub-workflow` 投影 |
| 目录过滤 | `hub/internal/httpapi/workflow_directory_handler.go` |
| 单测 | `gui/app_ve_approval_response_test.go`、directory handler 测试 |

### Phase 3 交付（已完成）

| 项 | 路径 |
| --- | --- |
| 解析上下文 | `hub/internal/workflow/approval_resolve_context.go` |
| 动态部门角色 | `hub/internal/httpapi/workflow_approval_role_resolver.go` |
| 申请人部门查询 | `security.SecurityService.GetUserGroupID` |
| 执行注入上下文 | `executor.executeApprovalNode` / `ResumeInstance` / Decision API |
| 单测 | `workflow_approval_role_resolver_test.go`、`approval_resolve_context_test.go` |

### Phase 4 交付（已完成）

| 项 | 路径 |
| --- | --- |
| Hub directory 拉取 | `listMaclawAppApprovalInstancesFromHub` |
| 列表合并 | `listMaclawAppApprovalInstances` |
| 操作台决策 | `AppsPage` → `DecideMaclawAppApprovalInstance` |
| 单测 | `TestListMaclawAppApprovalInstancesAllMergesHubPending` |

### Phase 5 交付（已完成）

| 项 | 路径 |
| --- | --- |
| 对账 | `ReconcileMaclawAppApprovalProjections` |
| 可达探测 | `probeMaclawAppHubDirectoryReachable` |
| Trace | `maclawAppApprovalTrace` |
| 列表/刷新 | `List…All` + AppsPage `loadInstances` |
| 单测 | `TestReconcileMaclawAppApprovalProjectionsAlignsHandled`、`TestReconcileSkipsWhenHubUnreachable` |

### Phase 6 交付（本轮）

| 项 | 说明 |
| --- | --- |
| executionMode 两阶段 | `digital_suggest` 永不终裁；`digital_review` 仅允许自动拒绝终裁 |
| 派发形态 | 单节点 + 多解析人 + 数字模式 → 提升为 sequential（数字先于人） |
| 节点元数据 | `ApprovalNodeConfig.ExecutionMode` 写入 request details |
| 角色推导 | `ResolveExecutionMode` 从 role refs 取最谨慎 mode |
| 阻塞投影 | `markNodeBlocked` 写入 `blocked_reason` 到 instance data |
| 超时压力 | Hub urgency=overdue → 本地 attention |
| 文档 | `maclaw-app-technical-guide.md` 审批章节已对齐当前链路 |

### Phase 7 交付（已完成）

| 项 | 说明 |
| --- | --- |
| 设计器角色来源 | 仅 Hub `approval_roles` / directory；**不再**读 localStorage 作生产数据 |
| Admin 角色 Tab | Hub API 失败时空列表 + toast，不回退 localStorage assignees |
| Blocked 推送 | `HubWorkflowParticipantNotifier` → `ve:workflow_status` |
| 桌面处理 | `handleVEWorkflowStatus` → 本地 attention 投影 + 前端事件 |
| 接线 | `WithNotifier(participantNotifier)` 挂到 WorkflowExecutor |

### Phase 8 交付（已完成）

| 项 | 说明 |
| --- | --- |
| 设计器 empty-state | 无 Hub 角色 / 有角色无 assignee → 引导去 Admin「审批角色」配置；i18n EN/CN |
| 空态样式 | `approver-picker-empty--guide` 虚线卡片 + 操作提示 |
| 多机广播 | `machinesForIdentity` 向用户**全部在线机**推送；全离线时回退全部已知机 |
| 直接 machine 字段 | `requester_machine_id` + `initiator_machine_id` 等并集去重广播 |
| 单测 | notifier multi-machine；workflow-editor empty catalog；i18n keys |

### Phase 9 交付（已完成）

| 项 | 说明 |
| --- | --- |
| 事件分类 | `classifyWorkflowStatusEvent`：timeout/overdue → `event=escalation` + `urgency=overdue`；unavailable → `urgency=critical` |
| 桌面 urgency | `handleVEWorkflowStatus` / `applyHubWorkflowStatusAttention` 写入 `ResultPayload.urgency` |
| 操作台视觉 | `AppsPage` 行 `data-urgency` + 徽章（超时/紧急/关注）与 CSS 分层 |
| 单测 | classify + timeout escalation payload；attention urgency 投影 |

### Phase 10 交付（本轮）

| 项 | 说明 |
| --- | --- |
| Escalation 失败推送 | `EscalationManager.SetNotifier` → `markEscalationFailed` 调用 `NotifyInitiator` |
| Router 接线 | 与 `participantNotifier` 共用，max-retries 后推 `ve:workflow_status` |
| technical-guide | 审批执行链路补齐推送/urgency/多机/角色 SoT |
| 单测 | max-retries 后 `NotifyInitiator` 调用 1 次 |

### 生产联调清单

| # | 场景 | 期望 |
| --- | --- | --- |
| 1 | 同用户双机在线，节点 blocked | 两台桌面均收到 `ve:workflow_status`，操作台 attention + urgency |
| 2 | 审批超时 fallback 失败 | `event=escalation` / `urgency=overdue`，本地 `ResultPayload.urgency=overdue` |
| 3 | Escalation 队列 5 次失败 | audit `escalation_failed` + initiator 推送 attention |
| 4 | Hub Admin 清空角色后开设计器 | empty-state 引导配置，不读 localStorage |
| 5 | Hub 短暂不可达 | reconcile 不误标 missing；恢复后对齐 |

### Phase 11 交付（本轮）

| 项 | 说明 |
| --- | --- |
| Executor 接入 | `WithEscalationManager`：fallback `DispatchFallback` 失败 → 入队重试，不立刻 block |
| 超时护栏 | `fallbackAlreadyDispatched` 且仍有 pending escalation → 不抢先 `markNodeBlocked` |
| 最终失败 | `SetFailedHook` → `onEscalationFailed` → `markNodeBlocked(escalation_failed)` |
| 首次入队推送 | `escalation pending` 通知发起人（attention，非立即 overdue 终态） |
| Router | 先建 `escalationMgr` 再挂 executor，再 `Start()` |
| 单测 | `TestHandleTimeout_CascadingFailure_QueuesEscalationWhenManagerWired` |

### Phase 12 交付（已完成）

| 项 | 说明 |
| --- | --- |
| 主路径失败 | `ModeSingle` / `ModeSequential` 首次 `Dispatch` 失败 → `handlePrimaryDispatchFailure` |
| 优先 fallback | 配置了 FallbackApprover 时先走 `handleFallbackRouting` |
| 无 fallback | 有 EscalationManager → 主审批人入队；无 manager → 仍 hard-fail 返回 error |
| 公共入队 | `enqueueEscalationOrBlock`（fallback 与 primary 共用） |
| 护栏 | timeout / fallback 路由在 `HasPendingForInstance` 时不抢先 block |
| 单测 | `TestStartInstance_PrimaryDispatchFailure_QueuesEscalation` / `_RoutesToFallback` |

### Phase 13 交付（本轮）

| 项 | 说明 |
| --- | --- |
| 会签 / N 票 | `handlePartialMultiDispatchFailure` |
| 有 EscalationManager | 失败人入队，**继续** fan-out 其余审批人，实例保持 running |
| 无 manager | 兼容旧 soft-block（实例 blocked，停止 fan-out） |
| 单测 | Countersign 部分失败继续派发；AnyNofM 无 manager soft-block |

### Phase 14 交付（本轮）

| 项 | 说明 |
| --- | --- |
| 多失败人列表 | instance data `escalation_approvers[]`（保留标量 `escalation_approver` 兼容） |
| 入队去重 | `appendUniqueStringList`；Escalate 立即重投成功则不标 pending |
| 送达清理 | `SetDeliveredHook` → `onEscalationDelivered` 从列表移除；全清则去 pending |
| 终态清理 | `markNodeBlocked` 清除 pending/approvers/reason |
| 联调清单单测 | `approval_e2e_checklist_test.go` 覆盖超时 block、升级耗尽、多失败列表、marker 清理 |

### Phase 15 交付（本轮）

| 项 | 说明 |
| --- | --- |
| Directory API | `DirectoryItem.escalation_pending` / `escalation_approvers`（pending-action + SQLite directory JSON） |
| 推送 payload | `ve:workflow_status` 携带 `escalation_approvers` |
| 桌面投影 | directory 映射 + `applyHubWorkflowStatusAttention` extras → `ResultPayload` |
| 操作台 UI | 列表徽章「升级重投: id1, id2」+ `data-escalation=pending` 样式 |
| 单测 | directory 映射、push 投影、既有 checklist |

### Phase 16 交付（本轮）

| 项 | 说明 |
| --- | --- |
| Directory 全路径 | `ApplyEscalationFieldsToDirectoryItem`：SQLite + PG initiated/pending/confirm/completed |
| 验证手册 | `docs/approval-e2e-verification-zh.md`（自动化对照 + 实机步骤 + 日志关键字） |
| 一键脚本 | `scripts/run-approval-e2e-checks.ps1` |
| Checklist 单测 | empty approvers fail-closed；directory escalation markers |

### Phase 17 交付（本轮）

| 项 | 说明 |
| --- | --- |
| Reconcile 同步升级标记 | Hub directory 的 `escalation_pending/approvers` 合并进本地 ResultPayload |
| attention 抬升 | Hub 报告升级重投且本地仍 pending → 操作台 attention |
| 联调脚本 | 兼容 Windows PowerShell 5.1（`powershell -File …`） |
| 单测 | `TestReconcileMergesEscalationApproversFromHub` |

### Phase 18 交付（本轮）

| 项 | 说明 |
| --- | --- |
| Reconcile upsert 扩展 | 发起人 `initiated` 目录中的 attention/escalation 亦写入本地离线缓存 |
| technical-guide | 操作台投影字段表 + 验证入口 |
| 计划勾选 | Phase 4/5 残留文档与 Escalation 联动项全部勾完 |
| 单测 | `TestReconcileUpsertsInitiatorEscalationFromDirectory` |

### 代码侧闭环结论

Hub 权威审批 E2E **实现与自动化已齐**。一键：`scripts\run-approval-e2e-checks.cmd`。

**仅剩实机验收**（见验证手册 §2）：

| # | 场景 |
| --- | --- |
| 1 | 双机桌面 WS blocked 推送 |
| 4 | Admin 清空角色后设计器 empty-state |
| 5 | Hub 抖动（自动化已覆盖不可达不误伤） |

---

## 10. Phase 2 端到端路径（当前）

```text
Hub executeApprovalNode
  → WithApprovalResolveContext(instanceData)
  → ResolveApproverIDs
       ├─ role:function:… / role:department:… 展开 assignees
       ├─ role:dynamic:applicant_department:X
       │     → applicant 部门 → role:department:<dept>:X
       └─ executionMode 排序（digital_first / human_first / auto digital-only）
  → HubApprovalDispatcher SendToMachine type=ve:approval_request
  → Desktop handleVEApprovalRequest → VEMessageHandler
  → ACL + 规则引擎
       ├─ auto_approve/reject → POST /api/v1/instances/{id}/nodes/{node}/decision
       │                         → ResumeInstance
       └─ require_human → RecordMaclawAppApprovalInstance (lane=pending_my_approval)
                          → 人工在 App 面板 Decide → 同上 Decision API
```
