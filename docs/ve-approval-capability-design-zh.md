# 数字员工审批能力——"启用审批"功能设计方案

## 一、产品定位

"启用审批"让本机的数字员工（VE）成为企业审批工作流中的**自动审批节点**。

**关键联动**：只有本机启用了审批开关，该 VE 才会在 Hub 审批工作流设计器的"审批角色"面板中作为可选的数字分身（Digital Twin）出现。开关关闭 = 审批角色中看不到自己的数字分身。

---

## 二、开关的两个作用

### 作用 1：声明审批能力——让自己在 Hub 审批角色中可见

```
桌面端: 启用审批 = ON
    │
    ▼ 上报 Hub（VE status=active + capability=approval）
Hub: Approver Directory API
    │
    ▼
审批工作流设计器 → 审批节点 → "选择审批人" 面板
    └── 能看到"张三的数字分身"作为可选审批角色
```

Hub 的 `/api/v1/workflow-directory/approvers` 返回审批人目录时：
- 只返回 `status == "active"` 的 VE
- 前端按 owner_email 分为两类：
  - **digital_twin**（数字分身）：有 owner 的 VE，显示在 owner 所在部门下
  - **department_digital_employee**（部门数字员工）：无 owner 的 VE

### 作用 2：运行时处理审批请求

开关打开后，本机 VE 的消息处理器接受 `approval_request`，走 4 步管线：
1. ACL 准入 → 2. 队列容量 → 3. 规则引擎三路决策 → 4. 返回结果

开关关闭时，所有审批请求直接返回 `reject: capability disabled`。

---

## 三、Hub 审批角色系统

### 3.1 什么是审批角色

审批角色是**抽象层**，解耦"工作流设计"和"具体审批人分配"：

```
工作流设计器中：
  审批节点.审批人 = "财务审批角色"（role:function:finance:finance_approver）
        │
        ▼ 运行时解析
审批角色表：
  finance_approver.assignees = [
    { type: "user", id: "cfo@company.com" },
    { type: "digital_twin", id: "machine-id-of-cfo-ve" }  ← 数字分身
  ]
        │
        ▼ 解析为
实际审批人 machine_id = "machine-id-of-cfo-ve"
```

### 3.2 数字分身在审批角色中的出现条件

VE 要出现在审批角色的可选列表中，需同时满足：
1. VE 已在 Hub 注册且 `status = "active"`（管理员已批准）
2. VE 在桌面端**启用了审批开关**（本次功能的核心）

### 3.3 审批角色配置示例

```json
{
  "id": "role:organization:tech_dept:dept_manager",
  "scopeName": "技术部",
  "roleName": "部门经理",
  "executionMode": "manual",
  "assignees": [
    { "subjectType": "user", "subjectId": "manager@company.com", "displayName": "王经理" },
    { "subjectType": "digital_twin", "subjectId": "machine-xyz", "displayName": "王经理的数字分身" }
  ]
}
```

### 3.4 角色运行时解析（`workflow_approval_role_resolver.go`）

工作流实例到达审批节点时：
- `role:xxx` 前缀 → 查角色表 → 展开 assignees
- `subjectType=user` → 查该用户的在线机器 → machine_id
- `subjectType=digital_twin` → 直接用 subjectId 作为 machine_id

---

## 四、完整端到端流程

```
1. 张三在桌面端「设置→数字员工」启用审批
2. Hub Approver Directory 返回"张三的数字分身"
3. 管理员在 Hub 工作流设计器配置审批角色：
     finance_approver.assignees += 张三的数字分身
4. 员工提交报销单 ¥300
5. Hub 工作流实例到达审批节点
6. 解析角色 → 张三的 machine_id
7. Hub 通过 WebSocket 发送 approval_request 给张三桌面端
8. 桌面端管线处理：
   - enabled=true ✓
   - ACL 通过 ✓
   - 队列未满 ✓
   - 规则: amount(300) ≤ 500 → auto_approve
9. 返回 approval_response: approve
10. Hub 记录审计 → 推进工作流
```

---

## 五、启用后可配置的子功能

| 子功能 | 说明 | 默认值 |
|--------|------|--------|
| 访问控制 (ACL) | 白名单/黑名单，按部门/角色/技能/ID 过滤谁能提交请求 | 白名单，空列表 |
| 最大队列 | 同时待处理的审批请求数上限 | 50 |
| 每日配额 | 每天处理的审批总量上限（自动重置） | 100 |
| 超时时间 | 超时后转给备用审批人 | 24 小时 |
| 备用审批人 | 队列满/超时/不可用时的兜底 | 空 |
| 自动拒绝规则 | 命中条件立即拒绝（附原因） | 空，最多50条 |
| 自动通过规则 | 命中条件立即通过 | 空，最多50条 |
| 需人工规则 | 命中条件转人类审批 | 空，最多50条 |

规则条件支持的运算符：等于、不等于、大于、小于、包含、在列表中、不在列表中、为空、不为空。
字段路径支持点号嵌套（最多3层），如 `details.amount`、`details.department.name`。

---

## 六、设置界面结构

```
设置 → 数字员工
  └── 审批能力
        ├── [开关] 启用审批
        │     开启后：本机 VE 在 Hub 审批角色中可被选为审批人
        │
        │   ── 以下仅在开关开启后显示 ──
        ├── 访问控制列表 (白名单/黑名单 + 部门/角色/技能/ID)
        ├── 运行限制 (队列/配额/超时)
        ├── 备用审批人 (VE 或用户 ID)
        ├── 三路路由规则 (拒绝/通过/人工)
        ├── [按钮] 保存审批设置
        └── [按钮] 打开 Hub 审批工作流设计器
```

---

## 七、关键代码模块

| 层 | 文件 | 职责 |
|----|------|------|
| 桌面端 | `gui/ve_approval_handler.go` | 审批处理管线主逻辑 |
| 桌面端 | `gui/ve_approval_rules.go` | 三路规则引擎 |
| 桌面端 | `gui/ve_approval_queue.go` | 队列/配额管理 |
| 桌面端 | `gui/ve_approval_capability_check.go` | 能力验证 API |
| 桌面端 | `gui/app_ve_handler.go` | 消息路由分发 |
| Hub | `hub/.../workflow_directory_handler.go` | 审批人目录 API |
| Hub | `hub/.../approval_roles_handler.go` | 审批角色 CRUD |
| Hub | `hub/.../workflow_approval_role_resolver.go` | 角色→机器ID 解析 |
| Hub | `hub/.../workflow/executor.go` | 工作流执行引擎 |
| Hub | `hub/web/approval_workflow/workflow-editor.js` | 设计器前端 |
