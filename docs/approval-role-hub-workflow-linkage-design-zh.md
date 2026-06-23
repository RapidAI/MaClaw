# Hub 审批角色与工作流设计器联动设计

## 背景

当前审批工作流设计器的审批节点通过“选择审批人”选择具体用户，但企业中心已经存在组织机构、数字员工、用户范围和能力包等治理能力。后续审批体系需要支持：

- 部门经理审批下属请假等部门内审批。
- 财务、采购、法务等跨部门职能审批。
- 数字员工参与审批建议、初审、自动处理。
- 物理员工拥有多个数字分身，数字分身可代表本人参与流程。

因此，审批节点不应只选择邮箱或用户账号，而应引用 Hub 中配置好的“审批角色”。

## 设计目标

- 在 Hub 上新增独立功能 Tab：`审批角色`。
- 组织架构视图沿用企业管理中的组织机构图，但扩展为多主体展示。
- 工作流设计器的审批人选择器复用 Hub 的组织视图和职能视图。
- 流程配置人可以直接选择角色，不需要理解复杂路由规则。
- 工作流保存角色引用，运行时再解析为具体审批主体。
- 审计记录必须能区分物理员工、部门数字员工、个人数字分身，以及其责任归属。

## 核心概念

### 审批主体

审批主体是可以参与审批流程的对象，分为三类：

| 类型 | 说明 | 例子 |
| --- | --- | --- |
| 物理员工 | 真实员工，是最终责任主体 | 张三、李四 |
| 部门数字员工 | 归属于某个部门或职能的数字岗位 | 发票审核助手、合同审查助手 |
| 个人数字分身 | 归属于某个物理员工的数字代理 | 张三-合同初审分身 |

### 审批角色

审批角色是流程可引用的职责位置，不等同于某个人。

示例：

- 部门经理
- 直属上级
- 财务审批员
- 采购审批员
- 合同审批员
- 请假初审

审批角色绑定一个或多个审批主体。

### 审批范围

审批范围表示角色在哪个组织或职能上下文中生效。

示例：

- 全局
- 销售部
- 财务部
- 采购职能
- 申请人所在部门

## Hub 侧设计

### 信息架构

企业中心增加或明确 `审批角色` Tab：

```text
企业中心
├─ 组织机构
├─ 数字员工
├─ 审批角色
└─ 权限/策略
```

`审批角色` Tab 用于配置“某个范围下，某个审批角色由哪些主体承担”。

### 组织架构视图

组织架构视图沿用企业管理中的组织机构图，但显示多主体关系：

```text
销售部
├─ 物理员工
│  ├─ 张三
│  │  ├─ 张三-合同初审分身
│  │  └─ 张三-客户跟进分身
│  └─ 李四
├─ 部门数字员工
│  └─ 销售线索助手
└─ 审批角色
   ├─ 部门经理
   ├─ 合同审批员
   └─ 请假初审
```

规则：

- 部门数字员工可归入某个部门。
- 个人数字分身归入对应物理员工下。
- 数字员工可以算组织成员，但必须与物理员工区分类型。
- 组织树不是单一员工树，而是多主体组织架构。

### 职能视图

职能视图用于配置跨部门审批角色。Hub 预置财务、采购、法务、IT、HR、行政、销售、运营、客户成功、安全、风控合规、数据等常见职能；租户管理员可以按公司实际情况自由新增或删除职能，删除职能时同步移除该职能下的审批角色，工作流设计器只引用保存后的角色结果。

```text
职能视图
├─ 财务
│  ├─ 财务审批员
│  ├─ 发票审核
│  └─ 付款复核
├─ 采购
│  ├─ 采购审批员
│  └─ 供应商准入
├─ 法务
│  └─ 合同审批员
└─ IT
   └─ 权限审批员
```

职能视图不需要引入复杂路由规则。配置人只需要在工作流中选择“哪个职能下的哪个角色”。

### 审批角色配置页

推荐布局：

```text
[组织视图] [职能视图]

左侧：组织树或职能目录
右侧：当前范围下的审批角色

角色              物理员工        数字员工/数字分身        执行方式
部门经理           张三            -                       人工审批
合同审批员         李四            张三-合同初审分身         数字初审 + 人工确认
财务审批员         王五            发票审核助手              数字建议 + 人工确认
```

角色字段建议：

| 字段 | 说明 |
| --- | --- |
| roleCode | 稳定编码，如 `finance_approver` |
| roleName | 展示名称，如“财务审批员” |
| scopeType | `global`、`department`、`function`、`user` |
| scopeId | 部门、职能或用户 ID |
| assignees | 绑定的审批主体列表 |
| executionMode | 人工审批、数字建议、数字初审、自动审批 |
| fallbackAssignees | 兜底审批主体 |
| workflowEnabled | 是否允许工作流引用 |

### 接口落地

首版由 Hub 维护租户级审批角色配置，工作流设计器只读引用：

```text
GET /api/admin/security/approval-roles
PUT /api/admin/security/approval-roles
GET /api/v1/workflow-directory/approvers
```

管理端接口使用租户管理员权限，保存到租户级 system settings：`approval_roles_v1`。工作流目录接口在原有组织树、成员、机器、数字员工基础上增加 `approval_roles` 和 `function_scopes` 字段，供审批节点选择器按组织视图和职能视图展示；当某个职能已存在但尚未配置审批角色时，设计器在职能视图中显示为不可选提示行，避免配置人误判为职能不存在。

推荐响应结构：

```json
{
  "roles": [
    {
      "id": "role:function:finance:finance_approver",
      "view": "function",
      "scopeType": "function",
      "scopeId": "finance",
      "scopeName": "财务",
      "roleCode": "finance_approver",
      "roleName": "财务审批员",
      "executionMode": "digital_review",
      "assignees": [
        {
          "subjectType": "user",
          "subjectId": "finance@example.com",
          "displayName": "财务王"
        }
      ]
    }
  ],
  "updated_at": "2026-06-23T00:00:00Z"
}
```

## 工作流设计器联动

### 审批节点配置入口

审批节点右侧面板保留 `选择审批人`，但点击后打开“审批角色选择器”，而不是简单用户列表。

```text
审批人
[选择审批人]
```

### 审批角色选择器

选择器包含三个 Tab：

```text
选择审批人
[按组织选择] [按职能选择] [指定成员]
```

#### 按组织选择

适用于：

- 申请人所在部门的部门经理。
- 指定部门的审批员。
- 某个员工的数字分身。
- 部门内数字员工。

选择方式：

```text
范围
- 申请人所在部门
- 指定部门/组织
- 全局

角色
- 部门经理
- 直属上级
- 请假初审
- 合同审批员
```

示例：

```text
请假审批
范围：申请人所在部门
角色：部门经理
```

#### 按职能选择

适用于财务、采购、法务、IT 等跨部门审批。

选择方式：

```text
职能
- 财务
- 采购
- 法务
- IT

角色
- 财务审批员
- 采购审批员
- 合同审批员
- 权限审批员
```

示例：

```text
报销审批
职能：财务
角色：财务审批员
```

#### 指定成员

作为兜底能力保留，可直接选择：

- 物理员工
- 部门数字员工
- 个人数字分身

但产品上应弱化直接指定成员，优先推荐选择审批角色。直接指定成员适合临时流程、小团队流程或一次性流程。

### 设计器展示摘要

选择完成后，右侧配置面板显示可读摘要。

部门角色：

```text
审批人
申请人所在部门 / 部门经理
运行时自动解析
```

职能角色：

```text
审批人
财务 / 财务审批员
王五 + 发票审核助手
```

指定成员：

```text
审批人
李四、张三-合同初审分身
```

## 保存模型

工作流设计器保存角色引用，不保存具体用户邮箱。

### 申请人所在部门角色

```json
{
  "approverSource": "approval_role",
  "selectionView": "organization",
  "scopeMode": "applicant_department",
  "roleCode": "department_manager"
}
```

### 指定组织角色

```json
{
  "approverSource": "approval_role",
  "selectionView": "organization",
  "scopeMode": "fixed_scope",
  "scopeType": "department",
  "scopeId": "dept_finance",
  "roleCode": "finance_approver"
}
```

### 职能角色

```json
{
  "approverSource": "approval_role",
  "selectionView": "function",
  "scopeMode": "fixed_scope",
  "scopeType": "function",
  "scopeId": "finance",
  "roleCode": "finance_approver"
}
```

### 指定成员

```json
{
  "approverSource": "direct_subject",
  "subjects": [
    {
      "subjectType": "user",
      "subjectId": "u_lisi"
    },
    {
      "subjectType": "digital_twin",
      "subjectId": "dt_zhangsan_contract",
      "ownerUserId": "u_zhangsan"
    }
  ]
}
```

## 运行时解析

流程运行时根据保存的引用查询 Hub。

示例 1：请假审批

```text
配置：申请人所在部门 / 部门经理
申请人：小刘
申请人部门：销售部
Hub 查询：销售部 / 部门经理
解析结果：张三
```

示例 2：报销审批

```text
配置：财务 / 财务审批员
Hub 查询：财务职能 / 财务审批员
解析结果：王五 + 发票审核助手
```

示例 3：合同审批

```text
配置：销售部 / 合同审批员
Hub 查询：销售部 / 合同审批员
解析结果：李四 + 张三-合同初审分身
```

## 执行方式

审批角色可配置执行方式：

| 执行方式 | 说明 |
| --- | --- |
| 人工审批 | 只由物理员工处理 |
| 数字建议 + 人工确认 | 数字员工给出意见，人工最终审批 |
| 数字初审 + 人工确认 | 数字员工先完成规则校验或初审，再交人工确认 |
| 自动审批 | 数字员工可直接审批，必须受权限、金额、场景限制 |

初期建议优先支持前三种。自动审批需要更严格的授权和审计，不作为首版默认能力。

## 审计模型

审批记录必须同时记录“执行主体”和“责任归属”。

```json
{
  "workflowInstanceId": "wf_001",
  "nodeId": "node_approval_1",
  "roleCode": "finance_approver",
  "scopeType": "function",
  "scopeId": "finance",
  "action": "approve",
  "executor": {
    "subjectType": "digital_employee",
    "subjectId": "bot_invoice_checker",
    "displayName": "发票审核助手"
  },
  "responsibleParty": {
    "subjectType": "department",
    "subjectId": "dept_finance",
    "displayName": "财务部"
  },
  "finalHumanReviewer": {
    "subjectType": "user",
    "subjectId": "u_wangwu",
    "displayName": "王五"
  }
}
```

个人数字分身场景：

```json
{
  "executor": {
    "subjectType": "digital_twin",
    "subjectId": "dt_zhangsan_contract",
    "displayName": "张三-合同初审分身"
  },
  "actsOnBehalfOf": {
    "subjectType": "user",
    "subjectId": "u_zhangsan",
    "displayName": "张三"
  }
}
```

## 空状态与校验

### Hub 侧

- 当前范围无审批角色：提示“当前范围尚未配置审批角色”，提供“新建审批角色”。
- 角色无成员：提示“该角色尚未绑定审批主体，工作流引用后运行时会失败”。
- 数字员工无审批权限：允许显示，但不可选，提示“未启用审批参与权限”。
- 个人数字分身无代表人：不可保存。

### 工作流设计器侧

- 已选角色不存在：显示“该审批角色已被删除或停用”，要求重新选择。
- 已选范围下角色无成员：显示“该范围下尚未配置可用审批主体”，提供跳转 Hub 的入口。`function_scopes` 中存在但无审批角色的职能在选择器中显示为“未配置审批角色”的不可选行。
- 角色包含数字员工：显示执行方式，避免配置人误以为只有人工审批。
- 指定成员为数字分身：显示“代表：张三”。

## 权限边界

- Hub 管理员可以配置审批角色。
- 工作流设计器配置人只能引用自己有权限查看的角色。
- 工作流设计器不直接修改 Hub 角色绑定关系。
- 如果工作流中发现角色缺失，允许跳转 Hub，但修改权限由 Hub 控制。

## 联动逻辑 Review

### 1. Hub 配角色，工作流引角色，运行时解角色

逻辑成立。

Hub 是组织、数字员工、审批角色的治理中心；工作流设计器只引用角色；运行时再解析到具体主体。这样人员变动或数字员工调整只需改 Hub，不需要逐个修改流程。

### 2. 组织视图和职能视图都需要

逻辑成立。

组织视图解决部门经理、直属上级、部门内审批、个人数字分身等场景。职能视图解决财务、采购、法务、IT 等跨部门审批场景。两者不是重复，而是两种找人的心智入口。

### 3. 数字员工进入组织架构，但必须分类型

逻辑成立。

如果完全不进入组织架构，审批角色配置时不好找数字员工。如果和物理员工混成同级员工，又会污染组织关系。因此采用多主体组织架构：

- 部门数字员工归入部门。
- 个人数字分身归入物理员工。
- 审批角色选择器显示主体类型和代表关系。

### 4. 不引入复杂路由规则

逻辑成立。

当前目标是方便易用。流程配置人直接选择“范围 + 角色”即可。复杂路由可以后续作为高级能力，但首版不应阻塞核心体验。

### 5. 审批角色不是审批人

逻辑成立。

审批角色是稳定引用，审批人是运行时解析结果。这个分离能解决人员变更、数字员工替换、部门调整、审计责任等问题。

### 6. 指定成员仍需保留

逻辑成立。

虽然推荐使用审批角色，但指定成员对临时流程、测试流程、一次性审批仍然有价值。它应作为兜底入口，而不是主路径。

## 首版范围建议

首版建议实现：

- Hub `审批角色` Tab。
- 组织视图：部门、物理员工、部门数字员工、个人数字分身。
- 职能视图：预置财务、采购、法务、IT、HR、行政、销售、运营、客户成功、安全、风控合规、数据等常见职能，并允许租户自由新增/删除职能。
- 审批角色绑定物理员工、部门数字员工、个人数字分身。
- 工作流设计器审批人选择器支持“按组织选择 / 按职能选择 / 指定成员”。
- 工作流保存角色引用。
- 运行时按角色引用解析审批主体。
- 基础审计记录区分执行主体、代表人和最终确认人。

暂不建议首版实现：

- 复杂路由规则引擎。
- 多条件动态匹配。
- 无人工确认的高风险自动审批。
- 与薪酬、成本中心、项目制组织的深度规则联动。

## 最终结论

审批体系的核心链路应确定为：

```text
Hub 配置审批角色
-> 工作流设计器选择审批角色
-> 流程运行时解析审批主体
-> 审计记录执行主体和责任归属
```

产品命名上，Hub 独立功能 Tab 使用 `审批角色`。工作流设计器右侧仍显示 `审批人`，但点击后进入支持组织视图、职能视图和指定成员的审批角色选择器。

这样既保留了“用户是在选审批人”的直觉，又把底层模型升级为“选择可治理、可变更、可审计的审批角色”。

## 实现闭环补充

本轮实现后，联动链路需要按以下闭环验收：

- Hub `审批角色` 保存租户级角色配置，角色 ID 使用 `role:{scopeType}:{scopeId}:{roleCode}`，工作流设计器只保存该稳定引用。
- 工作流目录接口返回 `approval_roles` 和 `function_scopes`，审批节点选择器优先使用 Hub 配置；接口不可用时只允许使用本地缓存/默认角色作为兜底展示。
- 组织视图中，部门数字员工按 `visible_group_ids` 归入部门；个人数字分身按 `owner_email` 归入对应物理员工；未配置部门归属的部门数字员工只在根节点兜底展示。
- 运行时进入审批节点时，将 `role:*` 引用解析为具体审批机器，并把解析后的 `approver_ids` 与原始 `original_approvers` 写入节点执行结果，供待办过滤和审计排查使用。
- `我的待审批` 以运行时解析后的 `approver_ids` 过滤，历史运行中没有该元数据的审批节点继续按旧逻辑展示，避免旧数据突然消失。
- 提交审批决定的 HTTP API 必须复用同一套角色解析逻辑后再授权，否则会出现“待办可见，但同意/拒绝被 403 拦截”的断链。

## 当前落地验收矩阵

| 链路 | 落地要求 | 代码/测试锚点 |
| --- | --- | --- |
| Hub 独立入口 | `审批角色` 必须是 Hub 左侧独立 Tab，不嵌在组织机构子页里。 | `hub/web/admin/index.html`、`hub/web/admin/admin.js`、`hub/web/admin/validate-admin-modules.js` |
| Hub 角色配置 | 支持组织视图和职能视图；组织视图复用企业组织树；职能视图覆盖财务、采购、法务、IT 等跨部门角色。 | `hub/web/admin/security-tab.js`、`hub/web/admin/security-tab.test.js` |
| 审批主体选择 | 可从同一范围内选择物理员工、部门数字员工、个人数字分身；个人数字分身按 `owner_email` 归入物理员工，部门数字员工按 `visible_group_ids` 归入部门。 | `security-tab.test.js` 中 `subject picker writes scoped organization assignees before save` |
| 保存模型 | Hub 保存租户级 `approval_roles_v1`，角色 ID 为 `role:{scopeType}:{scopeId}:{roleCode}`，并规范化/去重非法输入。 | `approval_roles_handler.go`、`approval_roles_handler_test.go` |
| 设计器目录 | `/api/v1/workflow-directory/approvers` 返回组织树、部门成员、用户、机器、数字员工和 `approval_roles`。 | `workflow_directory_handler.go`、`workflow_directory_handler_test.go` |
| 设计器选择器 | 审批节点选择审批人时提供组织视图、职能视图和直接成员视图；组织/职能视图能显示 Hub 审批角色及已配置主体摘要；职能已存在但未配置角色时显示不可选提示。 | `workflow-editor.js`、`workflow-editor.test.js` |
| 运行时解析 | 工作流保存稳定 `role:*` 引用；进入审批节点或提交审批决定时解析为具体机器/审批主体。 | `workflow_approval_role_resolver.go`、`workflow_approval_role_resolver_test.go`、`api_decision_test.go` |
| 授权闭环 | 提交审批决定前必须解析角色后再校验调用者；解析为空或解析失败时不允许误放行。 | `TestDecisionAPI_UnresolvedApprovalRoleIsForbidden`、`TestDecisionAPI_ApprovalRoleResolveErrorFailsClosed` |

验收原则：Hub 是审批角色事实源；工作流设计器只引用稳定角色或直接主体；运行时负责把角色展开为具体审批人。首版不引入复杂路由规则，由流程设置人直接选择组织/职能范围和角色，保证配置入口清晰、可解释、可审计。

### 后端兜底校验补充

前端设计器在保存和提交前已校验审批节点必须选择至少一个审批人或审批角色；后端 `ValidateGraphStructure` 与 `/api/v1/workflows/{id}/versions/{vid}/validate` 也需要执行同一条规则。这样即使通过 API 或旧页面绕过前端，空审批节点也不能进入待审核/发布链路。

当前落地锚点：

- `ErrApprovalApproverRequired`：审批节点缺少 `approver_ids` 时返回可识别的图校验错误。
- `ValidateGraphStructure`：提交审核前拒绝空审批节点，HTTP 层返回 `400 VALIDATION_FAILED`。
- `ValidateWorkflowGraphDetailed`：设计器校验接口返回节点级错误，便于界面定位到具体审批节点。
- 测试覆盖：`TestValidateGraphStructure_RejectsApprovalWithoutApprover`、`TestValidateWorkflowGraphDetailed_ApprovalWithoutApprover`、`TestSubmitForReview_RejectsApprovalWithoutApprover`。
