# 审批发版当日检查清单（约 15 分钟）

**完整手册**：[approval-e2e-verification-zh.md](./approval-e2e-verification-zh.md)  
**一键自动**：`scripts\run-approval-e2e-checks.cmd`

---

## A. 机器侧（约 3 分钟）

在仓库根目录：

```bat
scripts\run-approval-e2e-checks.cmd
```

或确认 GitHub Actions **Approval E2E Contract**（`approval-e2e.yml`）在目标 commit 上已绿。

- [ ] 全部步骤 `OK` / `passed`（本地或 CI）
- [ ] 失败则 **不要发版**，先修再重跑

---

## B. 实机最小集（约 10–12 分钟）

### B1. 主路径冒烟（约 4 分钟）

| 步骤 | 期望 | 勾选 |
| --- | --- | --- |
| 桌面登录 Hub，VE「启用审批」 | 能力上报正常 | [ ] |
| App 提交一条 `approval_engine=hub` 实例 | registry 有 `hub_instance_id` | [ ] |
| 审批人通过或驳回 | Hub 推进；操作台状态更新 | [ ] |
| 操作台打开该实例详情 | 无异常 `hub_sync_error` | [ ] |

### B2. 双机 attention（#1，约 4 分钟）— **发版必做**

| 步骤 | 期望 | 勾选 |
| --- | --- | --- |
| 同一账号两台桌面在线 | 两端均连上 Hub | [ ] |
| 触发 block 或升级失败（超时无 fallback / max-retries） | 两端操作台进入 **attention** | [ ] |
| 看列表徽章 | urgency 与/或「升级重投」可见 | [ ] |
| （可选）一端日志 | 含 `[workflow-notifier] delivered` 或 `[maclaw-approval]` | [ ] |

### B3. 二选一（#4 或 #5，约 3 分钟）— **发版至少一项**

**选项 A — 空角色设计器（#4）**

| 步骤 | 期望 | 勾选 |
| --- | --- | --- |
| Admin 清空审批角色 assignee | 保存成功 | [ ] |
| 打开工作流设计器选审批人 | empty-state 引导，**无** localStorage 旧人 | [ ] |

**选项 B — Hub 抖动（#5）**

| 步骤 | 期望 | 勾选 |
| --- | --- | --- |
| 已有 hub 绑定本地实例时停 Hub / 断网 | 刷新操作台 | [ ] |
| 观察本地实例 | **不**仅因不可达被标 missing | [ ] |
| 恢复 Hub 再刷新 | 与 directory 对齐 | [ ] |

---

## C. 建议加做（有余力，+5–10 分钟）

| # | 场景 | 一眼期望 |
| --- | --- | --- |
| 7 | any-2-of-3 一人永久离线 | 不 block；琥珀色「离线耗尽」；另两人可过 |
| 8 | 升级重投中重启 Hub | 日志 `reconcile restored`；attempts 不回 1 |
| 9 | 会签 A 离线 B 已送达后 timeout | 不会永远 defer 整节点 |

---

## D. 签字

| 项 | 填写 |
| --- | --- |
| 日期 / 版本 | |
| 执行人 | |
| A 自动 | 通过 / 失败 |
| B1 主路径 | 通过 / 失败 |
| B2 双机 | 通过 / 失败 |
| B3 选项 | A 空角色 / B 抖动 / 跳过（原因：） |
| 是否放行发版 | 是 / 否 |

---

## E. 失败时优先查

```text
[workflow-notifier] delivered status | no recipient machines
[workflow-escalation] reconcile: restored=
[maclaw-approval] event=
escalation_failed / dispatch_partial_failure
```

更细步骤与架构表见完整手册 §2–§4。
