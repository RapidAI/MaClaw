# 审批工作流 × MaClaw E2E 验证手册

**关联**：[approval-maclaw-app-e2e-improvement-plan-zh.md](./approval-maclaw-app-e2e-improvement-plan-zh.md)  
**原则**：Hub WorkflowExecutor 为 SoT；桌面 registry / DataSrv 为投影。

---

## 1. 一键自动化（CI / 本地）

在仓库根目录执行（Windows PowerShell 或 bash）：

```bat
REM 推荐（不受 PowerShell ExecutionPolicy 限制）
scripts\run-approval-e2e-checks.cmd
```

```powershell
# 若本机允许脚本执行：
powershell -ExecutionPolicy Bypass -File scripts/run-approval-e2e-checks.ps1
```

等价手工命令：

```powershell
go test ./hub/internal/workflow/ -count=1 -timeout 180s -run "TestChecklist_|TestEscalation_|TestHandleTimeout|TestStartInstance_"
go test ./hub/internal/httpapi/ -count=1 -timeout 120s -run "TestHubWorkflowParticipantNotifier|TestClassifyWorkflowStatusEvent"
go test ./gui/ -count=1 -timeout 180s -run "TestApplyHubWorkflowStatusAttention|TestMaclawAppApprovalInstanceFromHubDirectoryItem|TestReconcile"
node hub/web/approval_workflow/workflow-editor.test.js
node hub/web/approval_workflow/i18n.test.js
```

### 清单与自动化对照

| # | 场景 | 自动化覆盖 | 实机必做 |
| --- | --- | --- | --- |
| 1 | 同用户双机 blocked 推送 | `TestHubWorkflowParticipantNotifier_BroadcastsMultipleMachines`（Hub 侧多 machine 投递） | 两台桌面同时登录同一账号，触发 block，确认两侧操作台 attention |
| 2 | 超时 fallback → overdue | `TestChecklist_TimeoutFallback*` | 可选：缩短 timeout 节点做实机 |
| 3 | Escalation max-retries | `TestChecklist_EscalationMaxRetries_*` / `TestEscalation_MaxRetriesExhausted` | 可选 |
| 4 | 无角色 empty-state | `workflow-editor.test.js` empty catalog；`TestChecklist_EmptyApprovalRoles_*` | Admin 清空角色后打开设计器，确认引导文案，不读 localStorage |
| 5 | Hub 不可达 reconcile | `TestReconcileSkipsWhenHubUnreachable` | 停 Hub 后刷新操作台，本地实例不误标 missing；恢复后对齐 |

---

## 2. 实机步骤（最小集）

### 2.1 预置

1. Hub 已发布一条审批工作流（含 FallbackApprover 更佳）。  
2. 租户已配置 `approval_roles` 并绑定 assignee（至少 1 人 + 可选数字员工）。  
3. 桌面登录 Hub，VE「启用审批」已开。  
4. enterprise_approval_app 已安装并绑定 `hub_workflow_id`。

### 2.2 主路径冒烟

1. App 填表提交 → 本地 registry 有 `hub_instance_id`。  
2. 审批人机器收到 `ve:approval_request`（或人工在操作台 pending）。  
3. 通过 / 驳回 → Hub 节点推进；DataSrv 业务状态同步。  
4. 操作台「审批状态」能看到 Hub 标记与 urgency 徽章。

### 2.3 清单 #1 双机

1. 同一用户两台在线桌面。  
2. 人为触发 block（超时无 fallback，或升级失败）。  
3. **期望**：两台均收到 `ve:workflow_status`，操作台 attention + urgency；有升级重投人时显示「升级重投: …」。

### 2.4 清单 #4 设计器

1. Hub Admin → 安全 → 审批角色：清空或全部无 assignee。  
2. 打开工作流设计器审批人选择。  
3. **期望**：empty-state 引导去 Admin；刷新后仍不出现浏览器 localStorage 旧人。

### 2.5 清单 #5 Hub 抖动

1. 存在若干本地 hub 绑定实例。  
2. 停止 Hub 或断网，操作台刷新。  
3. **期望**：不把本地实例误标为 missing/attention（仅因不可达）。  
4. 恢复 Hub 后再刷新：与 directory 对齐。

---

## 3. 日志关键字

```text
[maclaw-approval] event=
[workflow-notifier] delivered status
[workflow-notifier] no recipient machines
[hub-client] handleVEWorkflowStatus
escalation_failed
dispatch_partial_failure
```

---

## 4. 通过标准（发布前）

- [x] 一键自动化全部绿（`scripts\run-approval-e2e-checks.cmd`）  
- [ ] 实机 #1 双机至少验证一次  
- [ ] 实机 #4 或 #5 至少验证一项（角色 empty / Hub 抖动）  
- [ ] 无回归：本地 `approval_engine=local` 演示路径仍可决策  

## 5. 发布状态（仓库）

| 项 | 状态 |
| --- | --- |
| 本地提交 | `14eb9550 feat: close Hub-authoritative approval E2E path` |
| 分支 | `main`（相对 `origin/main` **ahead 4**） |
| 同栈其它本地提交 | mobile QR / version bump / app workflows+SSH（非本专题） |
| 推送 | **已推送** `origin/main`（`5d72ed9c..e7c0b58d`，含审批 E2E 整栈 5 提交） |

> 远程：https://github.com/RapidAI/MaClaw/commit/14eb9550 （审批主提交）  
