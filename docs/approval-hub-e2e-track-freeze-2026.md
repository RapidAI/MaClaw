# Hub 审批 × MaClaw App E2E — track freeze

**Status: closed / frozen.**  
默认「继续」**不应**再扩展本专题，除非：回归修复、实机验收失败、或明确新命名目标。

**日期**：2026-07-15  
**主分支**：`origin/main`（含 CI 门禁 `approval-e2e.yml`）

---

## What shipped

| 层 | 能力 |
| --- | --- |
| SoT | Hub `WorkflowExecutor` 为审批推进真相源；App registry / DataSrv 为投影 |
| 升级队列 | `EscalationManager`：去重、opportunistic redeliver、progress/fail/deliver hooks |
| 持久 markers | `escalation_pending` / `approvers` / `attempts` / `exhausted_approvers` |
| 重启 | `ReconcileEscalations` 从 markers 重建队列并续计 attempts |
| 并发 | CAS 写 `instance_data`；终态 `CancelForInstance`；deliver 串行防双投 |
| 策略 | any-N 耗尽可继续；peer-aware timeout defer；countersign 单 peer 不拖死全节点 |
| 推送 | `ve:workflow_status` 含 pending / attempts / exhausted |
| Directory | `EscalationPending` / `Approvers` / `Exhausted` / `Attempts` |
| 桌面 UI | 升级重投（`id×N`）、离线耗尽徽章 + 详情字段；`\u` 安全文案 |
| 自动化 | `scripts/run-approval-e2e-checks.{cmd,ps1}` + Vitest helpers |
| CI | `.github/workflows/approval-e2e.yml`（path filter PR/main） |
| 文档 | 完整手册、15 分钟发版清单、改进计划、本文 freeze |

---

## Operator entry points

```bat
scripts\run-approval-e2e-checks.cmd
```

| 文档 | 用途 |
| --- | --- |
| [approval-release-day-checklist-zh.md](./approval-release-day-checklist-zh.md) | 发版当日 ~15 min（B2 双机必做） |
| [approval-e2e-verification-zh.md](./approval-e2e-verification-zh.md) | 全矩阵 #1–#10 + 日志关键字 |
| [approval-maclaw-app-e2e-improvement-plan-zh.md](./approval-maclaw-app-e2e-improvement-plan-zh.md) | 阶段历史与架构原则 |

CI：GitHub Actions → **Approval E2E Contract**。

---

## Explicitly out of scope (not regressions)

| 项 | 说明 |
| --- | --- |
| 实机 #1 双桌面 WS | 需两台真机同账号 |
| 实机 #4 空角色 / #5 Hub 抖动 | 需 Admin / 断网环境 |
| SSH 密码 vault 明文 localStorage | 独立安全专题 |
| 完整 durable escalation 表 | 当前用 instance_data markers + 内存队列，足够重启恢复 |

---

## When to reopen this track

1. CI **Approval E2E Contract** 红且确认与本栈相关。  
2. 实机发版清单失败（双机无 push、重启丢重试、any-N 误 block 等）。  
3. 产品新目标（例如：escalation 独立表、加密 vault、mobile 审批对等）。

否则：请开**新命名目标**，不要用「继续」无范围推进本专题。

---

## Freeze evidence (local)

- 一键脚本：`scripts/run-approval-e2e-checks.ps1` 在 freeze 提交前应全绿。  
- 关键提交索引见 `approval-e2e-verification-zh.md` §6。
