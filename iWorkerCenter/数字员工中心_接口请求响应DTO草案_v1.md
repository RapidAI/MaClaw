# 数字员工中心：接口请求响应 DTO 草案 v1

## 1. 目标

本文件用于为数字员工中心 V1 的关键接口补齐请求与响应 DTO 草案，作为后续：
- Go 请求结构体定义
- 前端类型定义
- OpenAPI 草稿整理
- handler 参数校验
- 联调 mock 数据

的统一基础。

本文件优先覆盖 V1 最小闭环接口，不追求一次性把所有次要接口完全展开。

---

## 2. 通用约定

## 2.1 基础响应结构

建议所有接口统一采用：

```json
{
  "code": 0,
  "message": "ok",
  "data": {}
}
```

字段建议：
- `code`: 业务状态码，`0` 表示成功
- `message`: 可读消息
- `data`: 业务数据
- 可选：`request_id`

## 2.2 分页结构

列表接口建议返回：

```json
{
  "items": [],
  "page": 1,
  "page_size": 20,
  "total": 100
}
```

## 2.3 时间字段

统一建议：
- 使用 ISO8601 字符串
- 例如：`2026-04-05T10:30:00Z`

## 2.4 ID 字段

统一使用字符串表达：
- `id`
- `colleague_id`
- `workflow_instance_id`
- `config_bundle_version_id`

这样前后端都更稳定，不提前绑定数据库自增整型语义。

## 2.5 JSON 扩展字段

以下类型建议直接保留对象/数组结构，不要在 DTO 层透出 `_json` 后缀：
- `strengths`
- `job_labels`
- `work_preference_profile`
- `collaboration_preference_profile`
- `allowed_task_types`
- `allowed_message_types`
- `attachment_refs`
- `scope`

---

## 3. 通用复用 DTO

## 3.1 `StatusChangeRequest`

```json
{
  "status": "active",
  "reason": "manual_enable"
}
```

字段：
- `status`: 目标状态
- `reason`: 可选，状态变更原因

## 3.2 `ReviewRequest`

```json
{
  "review_status": "approved",
  "review_comment": "安全审核通过"
}
```

## 3.3 `ResultPayload`

```json
{
  "result_summary": "已完成初步分析",
  "result_payload_ref": "obj://results/abc123"
}
```

## 3.4 `RejectRequest`

```json
{
  "reason": "缺少必要上下文",
  "detail": "请补充原始附件",
  "reject_action": "terminate_workflow"
}
```

说明：
- 协作拒绝时可不传 `reject_action`
- 工作流步骤拒绝时可带 `reject_action`

## 3.5 `PageResponse<T>`

```json
{
  "items": [],
  "page": 1,
  "page_size": 20,
  "total": 0
}
```

---

## 4. 数字员工 DTO

## 4.1 `GET /admin/colleagues`

### Query
- `page`
- `page_size`
- `status`
- `role_type`
- `keyword`

### Item DTO

```json
{
  "id": "col_001",
  "name": "采购助理",
  "avatar_url": "https://example/avatar.png",
  "summary": "擅长采购申请与供应商沟通",
  "role_type": "office",
  "strengths": ["采购流程", "表单整理"],
  "job_labels": ["采购申请", "供应商问询"],
  "capability_count": 3,
  "default_model_policy_id": "mrp_001",
  "status": "active",
  "updated_at": "2026-04-05T10:30:00Z"
}
```

### Response

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "items": [],
    "page": 1,
    "page_size": 20,
    "total": 1
  }
}
```

## 4.2 `GET /admin/colleagues/{id}`

### Response DTO

```json
{
  "id": "col_001",
  "name": "采购助理",
  "avatar_url": "https://example/avatar.png",
  "summary": "擅长采购申请与供应商沟通",
  "role_type": "office",
  "role_prompt_ref": "prompt://colleague/col_001",
  "strengths": ["采购流程", "表单整理"],
  "job_labels": ["采购申请", "供应商问询"],
  "work_preference_profile": {
    "preferred_task_types": ["approval", "office_request"],
    "preferred_output_formats": ["table", "summary"]
  },
  "collaboration_preference_profile": {
    "preferred_delegate_role_types": ["data", "office"],
    "preferred_receive_task_types": ["approval", "request"]
  },
  "default_model_policy_id": "mrp_001",
  "shared_memory_scope": {
    "type": "role",
    "values": ["office"]
  },
  "private_config_version": "v1",
  "status": "active",
  "created_at": "2026-04-05T10:00:00Z",
  "updated_at": "2026-04-05T10:30:00Z",
  "created_by": "admin_001",
  "updated_by": "admin_001"
}
```

## 4.3 `POST /admin/colleagues`

### Request DTO

```json
{
  "name": "采购助理",
  "avatar_url": "https://example/avatar.png",
  "summary": "擅长采购申请与供应商沟通",
  "role_type": "office",
  "role_prompt_ref": "prompt://colleague/col_001",
  "strengths": ["采购流程", "表单整理"],
  "job_labels": ["采购申请", "供应商问询"],
  "work_preference_profile": {
    "preferred_task_types": ["approval", "office_request"]
  },
  "collaboration_preference_profile": {
    "preferred_delegate_role_types": ["data", "office"]
  },
  "default_model_policy_id": "mrp_001",
  "shared_memory_scope": {
    "type": "role",
    "values": ["office"]
  },
  "status": "active"
}
```

### Response DTO

```json
{
  "id": "col_001"
}
```

## 4.4 `PUT /admin/colleagues/{id}`

### Request DTO
与创建基本一致，但全部字段可按更新语义处理。

## 4.5 `PUT /admin/colleagues/{id}/avatar`

### Request DTO

```json
{
  "avatar_url": "https://example/new-avatar.png"
}
```

## 4.6 `POST /admin/colleagues/{id}/status`

### Request DTO

```json
{
  "status": "disabled",
  "reason": "manual_disable"
}
```

## 4.7 `PUT /admin/colleagues/{id}/preferences`

### Request DTO

```json
{
  "work_preference_profile": {
    "preferred_task_types": ["approval"]
  },
  "collaboration_preference_profile": {
    "preferred_delegate_role_types": ["data"]
  }
}
```

## 4.8 `PUT /admin/colleagues/{id}/collaboration-scope`

### Request DTO

```json
{
  "shared_memory_scope": {
    "type": "role",
    "values": ["office", "data"]
  }
}
```

## 4.9 `GET /client/colleagues`

### Item DTO

```json
{
  "id": "col_001",
  "name": "采购助理",
  "avatar_url": "https://example/avatar.png",
  "summary": "擅长采购申请与供应商沟通",
  "role_type": "office",
  "strengths": ["采购流程", "表单整理"],
  "job_labels": ["采购申请", "供应商问询"]
}
```

## 4.10 `GET /client/colleagues/{id}/jobs`

### Response DTO

```json
{
  "colleague_id": "col_001",
  "jobs": [
    {
      "label": "采购申请",
      "summary": "可协助处理采购申请材料整理"
    }
  ]
}
```

---

## 5. 能力包 DTO

## 5.1 `GET /admin/capabilities`

### Query
- `page`
- `page_size`
- `status`
- `review_status`
- `source_type`
- `keyword`

### Item DTO

```json
{
  "id": "cap_001",
  "name": "采购申请助手",
  "summary": "处理采购申请单整理与校验",
  "source_type": "hubcenter",
  "version": "1.0.0",
  "author": "hubcenter",
  "risk_level": "medium",
  "supported_roles": ["office"],
  "job_labels": ["采购申请"],
  "status": "active",
  "review_status": "approved",
  "installed_at": "2026-04-05T10:00:00Z",
  "updated_at": "2026-04-05T10:30:00Z"
}
```

## 5.2 `GET /admin/capabilities/{id}`

### Response DTO

```json
{
  "id": "cap_001",
  "name": "采购申请助手",
  "summary": "处理采购申请单整理与校验",
  "source_type": "hubcenter",
  "source_ref": "hub://capabilities/123",
  "version": "1.0.0",
  "author": "hubcenter",
  "risk_level": "medium",
  "supported_roles": ["office"],
  "job_labels": ["采购申请"],
  "status": "active",
  "review_status": "approved",
  "review_comment": "审核通过",
  "manifest_ref": "obj://capability/manifests/cap_001.json",
  "installed_at": "2026-04-05T10:00:00Z",
  "created_at": "2026-04-05T10:00:00Z",
  "updated_at": "2026-04-05T10:30:00Z"
}
```

## 5.3 `POST /admin/capabilities/import`

### Request DTO

```json
{
  "source_type": "hubcenter",
  "source_ref": "hub://capabilities/123",
  "version": "1.0.0"
}
```

### Response DTO

```json
{
  "id": "cap_001"
}
```

## 5.4 `POST /admin/capabilities/{id}/review`

### Request DTO

```json
{
  "review_status": "approved",
  "review_comment": "安全审核通过"
}
```

## 5.5 `POST /admin/capabilities/{id}/bind`

### Request DTO

```json
{
  "colleague_id": "col_001",
  "priority": 100,
  "source_type": "manual",
  "scope": {
    "task_types": ["approval"]
  }
}
```

## 5.6 `POST /admin/capabilities/{id}/unbind`

### Request DTO

```json
{
  "colleague_id": "col_001"
}
```

## 5.7 `POST /admin/capabilities/{id}/status`

### Request DTO

```json
{
  "status": "disabled",
  "reason": "manual_disable"
}
```

## 5.8 `POST /admin/capabilities/{id}/upgrade`

### Request DTO

```json
{
  "target_version": "1.1.0",
  "source_ref": "hub://capabilities/123"
}
```

## 5.9 `GET /runtime/capabilities`

### Item DTO

```json
{
  "id": "cap_001",
  "name": "采购申请助手",
  "summary": "处理采购申请单整理与校验",
  "job_labels": ["采购申请"],
  "version": "1.0.0"
}
```

---

## 6. 协作委托 DTO

## 6.1 `POST /runtime/collaboration/requests`

### Request DTO

```json
{
  "task_id": "task_001",
  "from_colleague_id": "col_001",
  "to_colleague_id": "col_002",
  "delegated_by_colleague_id": "col_001",
  "message_type": "assist_request",
  "subject": "请协助分析采购异常",
  "content": "请基于附件完成初步分析",
  "context_summary": "采购申请异常，需快速定位原因",
  "attachment_refs": [
    "obj://attachments/file1"
  ]
}
```

### Response DTO

```json
{
  "id": "msg_001",
  "status": "standby"
}
```

## 6.2 `GET /runtime/collaboration/outgoing`

### Item DTO

```json
{
  "id": "msg_001",
  "task_id": "task_001",
  "from_colleague_id": "col_001",
  "to_colleague_id": "col_002",
  "message_type": "assist_request",
  "subject": "请协助分析采购异常",
  "status": "processing",
  "created_at": "2026-04-05T10:00:00Z",
  "completed_at": null
}
```

## 6.3 `GET /runtime/collaboration/incoming`

### Item DTO
与 outgoing 基本一致，但可补充：
- `delegated_by_colleague_id`
- `accepted_at`

## 6.4 `GET /runtime/collaboration/requests/{id}`

### Response DTO

```json
{
  "id": "msg_001",
  "task_id": "task_001",
  "workflow_instance_id": "wfins_001",
  "workflow_step_instance_id": "wfstep_001",
  "from_colleague_id": "col_001",
  "to_colleague_id": "col_002",
  "delegated_by_colleague_id": "col_001",
  "message_type": "assist_request",
  "subject": "请协助分析采购异常",
  "content": "请基于附件完成初步分析",
  "context_summary": "采购申请异常，需快速定位原因",
  "attachment_refs": ["obj://attachments/file1"],
  "status": "processing",
  "security_check_result": {
    "result": "allowed",
    "hit_policy_ids": []
  },
  "result_summary": "已完成初步分析",
  "result_payload_ref": "obj://results/res_001",
  "accepted_at": "2026-04-05T10:05:00Z",
  "completed_at": null,
  "created_at": "2026-04-05T10:00:00Z",
  "updated_at": "2026-04-05T10:10:00Z"
}
```

## 6.5 `POST /runtime/collaboration/requests/{id}/accept`

### Request DTO
可为空对象：

```json
{}
```

### Response DTO

```json
{
  "id": "msg_001",
  "status": "accepted"
}
```

## 6.6 `POST /runtime/collaboration/requests/{id}/start`

### Request DTO

```json
{}
```

### Response DTO

```json
{
  "id": "msg_001",
  "status": "processing"
}
```

## 6.7 `POST /runtime/collaboration/requests/{id}/complete`

### Request DTO

```json
{
  "result_summary": "已完成初步分析",
  "result_payload_ref": "obj://results/res_001"
}
```

### Response DTO

```json
{
  "id": "msg_001",
  "status": "completed"
}
```

## 6.8 `POST /runtime/collaboration/requests/{id}/reject`

### Request DTO

```json
{
  "reason": "缺少必要上下文",
  "detail": "请补充原始采购附件"
}
```

### Response DTO

```json
{
  "id": "msg_001",
  "status": "rejected"
}
```

## 6.9 `GET /runtime/tasks/inbox`

### Query
- `page`
- `page_size`
- `task_kind`
- `status`
- `timeout_only`

### Item DTO

```json
{
  "task_kind": "workflow",
  "task_id": "msg_001",
  "workflow_instance_id": "wfins_001",
  "workflow_step_instance_id": "wfstep_001",
  "from_colleague_id": "col_001",
  "delegated_by_colleague_id": "col_001",
  "subject": "请协助分析采购异常",
  "status": "standby",
  "created_at": "2026-04-05T10:00:00Z",
  "timeout_at": null
}
```

---

## 7. 协作权限 DTO

## 7.1 `GET /admin/collaboration-permissions`

### Item DTO

```json
{
  "id": "cp_001",
  "from_colleague_id": "col_001",
  "to_colleague_id": "col_002",
  "allowed_task_types": ["approval", "request"],
  "allowed_message_types": ["assist_request", "analysis_request"],
  "approval_required": false,
  "priority": 100,
  "status": "active",
  "updated_at": "2026-04-05T10:00:00Z"
}
```

## 7.2 `POST /admin/collaboration-permissions`

### Request DTO

```json
{
  "from_colleague_id": "col_001",
  "to_colleague_id": "col_002",
  "allowed_task_types": ["approval", "request"],
  "allowed_message_types": ["assist_request", "analysis_request"],
  "approval_required": false,
  "priority": 100,
  "status": "active"
}
```

## 7.3 `PUT /admin/collaboration-permissions/{id}`

### Request DTO
与创建一致。

## 7.4 `POST /admin/collaboration-permissions/{id}/status`

### Request DTO

```json
{
  "status": "disabled",
  "reason": "manual_disable"
}
```

## 7.5 `GET /runtime/colleagues/{id}/permissions/delegation-targets`

### Response DTO

```json
{
  "colleague_id": "col_001",
  "targets": [
    {
      "colleague_id": "col_002",
      "allowed_task_types": ["approval"],
      "allowed_message_types": ["assist_request"],
      "approval_required": false,
      "priority": 100
    }
  ]
}
```

---

## 8. 工作流 DTO

## 8.1 `GET /admin/workflows`

### Query
- `page`
- `page_size`
- `status`
- `category`
- `keyword`

### Item DTO

```json
{
  "id": "wfd_001",
  "name": "采购审批流程",
  "summary": "采购申请按固定步骤审批",
  "category": "approval",
  "applicable_task_types": ["purchase_request"],
  "status": "active",
  "version": "1",
  "published_at": "2026-04-05T10:00:00Z",
  "updated_at": "2026-04-05T10:30:00Z"
}
```

## 8.2 `GET /admin/workflows/{id}`

### Response DTO

```json
{
  "id": "wfd_001",
  "name": "采购审批流程",
  "summary": "采购申请按固定步骤审批",
  "category": "approval",
  "applicable_task_types": ["purchase_request"],
  "status": "active",
  "version": "1",
  "published_at": "2026-04-05T10:00:00Z",
  "created_at": "2026-04-05T09:00:00Z",
  "updated_at": "2026-04-05T10:30:00Z",
  "steps": [
    {
      "id": "wfsd_001",
      "step_code": "submit_review",
      "step_name": "提交审核",
      "step_type": "approval",
      "assignee_mode": "fixed_colleague",
      "assignee_colleague_id": "col_002",
      "assignee_role_type": "office",
      "timeout_rule": {
        "timeout_minutes": 60
      },
      "reject_rule": {
        "action": "terminate_workflow"
      },
      "sort_order": 1
    }
  ]
}
```

## 8.3 `POST /admin/workflows`

### Request DTO

```json
{
  "name": "采购审批流程",
  "summary": "采购申请按固定步骤审批",
  "category": "approval",
  "applicable_task_types": ["purchase_request"],
  "status": "draft"
}
```

### Response DTO

```json
{
  "id": "wfd_001"
}
```

## 8.4 `PUT /admin/workflows/{id}`

### Request DTO
与创建基本一致。

## 8.5 `POST /admin/workflows/{id}/copy`

### Request DTO

```json
{
  "name": "采购审批流程-副本"
}
```

### Response DTO

```json
{
  "id": "wfd_002"
}
```

## 8.6 `POST /admin/workflows/{id}/status`

### Request DTO

```json
{
  "status": "active",
  "reason": "manual_enable"
}
```

## 8.7 `POST /admin/workflows/{id}/publish`

### Request DTO

```json
{
  "version": "2",
  "change_summary": "新增复核步骤"
}
```

## 8.8 `GET /admin/workflows/{id}/steps`

### Item DTO

```json
{
  "id": "wfsd_001",
  "step_code": "submit_review",
  "step_name": "提交审核",
  "step_type": "approval",
  "assignee_mode": "fixed_colleague",
  "assignee_colleague_id": "col_002",
  "assignee_role_type": null,
  "timeout_rule": {
    "timeout_minutes": 60
  },
  "reject_rule": {
    "action": "terminate_workflow"
  },
  "sort_order": 1
}
```

## 8.9 `POST /admin/workflows/{id}/steps`

### Request DTO

```json
{
  "step_code": "submit_review",
  "step_name": "提交审核",
  "step_type": "approval",
  "assignee_mode": "fixed_colleague",
  "assignee_colleague_id": "col_002",
  "assignee_role_type": null,
  "timeout_rule": {
    "timeout_minutes": 60
  },
  "reject_rule": {
    "action": "terminate_workflow"
  },
  "sort_order": 1,
  "ext": {}
}
```

## 8.10 `PUT /admin/workflows/{id}/steps/{stepId}`

### Request DTO
与新增步骤一致。

## 8.11 `POST /admin/workflows/{id}/steps/reorder`

### Request DTO

```json
{
  "items": [
    {
      "step_id": "wfsd_001",
      "sort_order": 1
    },
    {
      "step_id": "wfsd_002",
      "sort_order": 2
    }
  ]
}
```

## 8.12 `POST /runtime/workflows/instances`

### Request DTO

```json
{
  "workflow_definition_id": "wfd_001",
  "biz_object_type": "purchase_request",
  "biz_object_id": "biz_001",
  "title": "采购申请 PR-20260405-001",
  "initiator_id": "col_001",
  "context_summary": "采购申请进入审批流程"
}
```

### Response DTO

```json
{
  "id": "wfins_001",
  "current_step_id": "wfstep_001",
  "status": "running"
}
```

## 8.13 `GET /runtime/workflows/instances`

### Item DTO

```json
{
  "id": "wfins_001",
  "workflow_definition_id": "wfd_001",
  "title": "采购申请 PR-20260405-001",
  "biz_object_type": "purchase_request",
  "biz_object_id": "biz_001",
  "initiator_id": "col_001",
  "current_step_id": "wfstep_001",
  "status": "running",
  "started_at": "2026-04-05T10:00:00Z",
  "completed_at": null
}
```

## 8.14 `GET /runtime/workflows/instances/{id}`

### Response DTO

```json
{
  "id": "wfins_001",
  "workflow_definition_id": "wfd_001",
  "title": "采购申请 PR-20260405-001",
  "biz_object_type": "purchase_request",
  "biz_object_id": "biz_001",
  "initiator_id": "col_001",
  "current_step_id": "wfstep_001",
  "status": "running",
  "started_at": "2026-04-05T10:00:00Z",
  "completed_at": null,
  "steps": [
    {
      "id": "wfstep_001",
      "step_definition_id": "wfsd_001",
      "assigned_colleague_id": "col_002",
      "delegation_message_id": "msg_001",
      "status": "processing",
      "decision": null,
      "result_summary": null,
      "started_at": "2026-04-05T10:00:00Z",
      "accepted_at": "2026-04-05T10:05:00Z",
      "completed_at": null,
      "rejected_at": null
    }
  ]
}
```

## 8.15 `GET /runtime/workflows/instances/{id}/steps`

### Item DTO
与实例详情中的 `steps` 项一致。

## 8.16 `POST /runtime/workflows/instances/{id}/pause`

### Request DTO

```json
{
  "reason": "manual_pause"
}
```

### Response DTO

```json
{
  "id": "wfins_001",
  "status": "paused"
}
```

## 8.17 `POST /runtime/workflows/instances/{id}/resume`

### Request DTO

```json
{
  "reason": "manual_resume"
}
```

### Response DTO

```json
{
  "id": "wfins_001",
  "status": "running"
}
```

## 8.18 `POST /runtime/workflows/instances/{id}/cancel`

### Request DTO

```json
{
  "reason": "manual_cancel"
}
```

### Response DTO

```json
{
  "id": "wfins_001",
  "status": "cancelled"
}
```

## 8.19 `POST /runtime/workflows/steps/{id}/accept`

### Request DTO

```json
{}
```

### Response DTO

```json
{
  "id": "wfstep_001",
  "status": "accepted"
}
```

## 8.20 `POST /runtime/workflows/steps/{id}/start`

### Request DTO

```json
{}
```

### Response DTO

```json
{
  "id": "wfstep_001",
  "status": "processing"
}
```

## 8.21 `POST /runtime/workflows/steps/{id}/complete`

### Request DTO

```json
{
  "result_summary": "审批通过",
  "result_payload_ref": "obj://workflow-results/wfstep_001",
  "decision": "approved"
}
```

### Response DTO

```json
{
  "id": "wfstep_001",
  "status": "completed",
  "workflow_instance_id": "wfins_001",
  "workflow_status": "running",
  "next_step_id": "wfstep_002"
}
```

## 8.22 `POST /runtime/workflows/steps/{id}/reject`

### Request DTO

```json
{
  "reason": "资料不完整",
  "detail": "缺少采购金额说明",
  "reject_action": "terminate_workflow"
}
```

### Response DTO

```json
{
  "id": "wfstep_001",
  "status": "rejected",
  "workflow_instance_id": "wfins_001",
  "workflow_status": "rejected"
}
```

## 8.23 `POST /admin/workflows/steps/{id}/reassign`

### Request DTO

```json
{
  "target_colleague_id": "col_003",
  "reason": "原处理人不可用"
}
```

## 8.24 `POST /admin/workflows/steps/{id}/timeout-handle`

### Request DTO

```json
{
  "action": "manual_reassign",
  "target_colleague_id": "col_003",
  "reason": "步骤超时处理"
}
```

---

## 9. 模型路由与代理 DTO

## 9.1 `GET /admin/model-routing/models`

### Item DTO

```json
{
  "id": "me_001",
  "name": "Claude Sonnet",
  "provider": "anthropic",
  "api_base_url": "https://api.anthropic.com",
  "auth_type": "api_key",
  "protocol_type": "anthropic",
  "capabilities": ["chat", "tool_use"],
  "status": "active",
  "cost_level": "medium",
  "updated_at": "2026-04-05T10:00:00Z"
}
```

## 9.2 `POST /admin/model-routing/models`

### Request DTO

```json
{
  "name": "Claude Sonnet",
  "provider": "anthropic",
  "api_base_url": "https://api.anthropic.com",
  "auth_type": "api_key",
  "protocol_type": "anthropic",
  "capabilities": ["chat", "tool_use"],
  "status": "active",
  "cost_level": "medium"
}
```

## 9.3 `GET /admin/model-routing/policies`

### Item DTO

```json
{
  "id": "mrp_001",
  "name": "办公默认策略",
  "summary": "办公类任务默认模型策略",
  "applicable_role_types": ["office"],
  "applicable_task_types": ["approval", "request"],
  "primary_model_ref": "me_001",
  "fallback_model_refs": ["me_002"],
  "proxy_mode": "llm_only",
  "status": "active",
  "version": "1"
}
```

## 9.4 `POST /runtime/model-routing/recommend`

### Request DTO

```json
{
  "colleague_id": "col_001",
  "task_type": "approval",
  "request_type": "llm"
}
```

### Response DTO

```json
{
  "routing_policy_id": "mrp_001",
  "primary_model_ref": "me_001",
  "fallback_model_refs": ["me_002"],
  "proxy_mode": "llm_only"
}
```

## 9.5 `POST /proxy/llm`

### Request DTO

```json
{
  "source_client_id": "client_001",
  "source_colleague_id": "col_001",
  "task_id": "task_001",
  "workflow_instance_id": "wfins_001",
  "routing_policy_id": "mrp_001",
  "request_summary": "采购审批文本分析",
  "payload": {
    "model": "claude-sonnet-4-6",
    "messages": []
  }
}
```

### Response DTO

```json
{
  "request_id": "proxy_001",
  "status": "success",
  "provider": "anthropic",
  "data": {}
}
```

## 9.6 `POST /proxy/api`

### Request DTO

```json
{
  "source_client_id": "client_001",
  "source_colleague_id": "col_001",
  "task_id": "task_001",
  "routing_policy_id": "mrp_001",
  "request_summary": "查询供应商接口",
  "payload": {
    "method": "POST",
    "url": "https://internal.example/api/query",
    "headers": {},
    "body": {}
  }
}
```

---

## 10. 安全规则 DTO

## 10.1 `GET /admin/security-policies`

### Item DTO

```json
{
  "id": "sp_001",
  "name": "跨角色协作限制",
  "policy_type": "collaboration",
  "summary": "限制敏感任务跨角色委托",
  "scope_type": "role",
  "scope_values": ["office"],
  "status": "active",
  "version": "1",
  "priority": 100,
  "published_at": "2026-04-05T10:00:00Z",
  "updated_at": "2026-04-05T10:10:00Z"
}
```

## 10.2 `POST /admin/security-policies`

### Request DTO

```json
{
  "name": "跨角色协作限制",
  "policy_type": "collaboration",
  "summary": "限制敏感任务跨角色委托",
  "content": {
    "rules": []
  },
  "scope_type": "role",
  "scope_values": ["office"],
  "status": "draft",
  "version": "1",
  "priority": 100
}
```

## 10.3 `PUT /admin/security-policies/{id}`

### Request DTO
与创建基本一致。

## 10.4 `POST /admin/security-policies/{id}/publish`

### Request DTO

```json
{
  "reason": "manual_publish"
}
```

## 10.5 `POST /admin/security-policies/{id}/disable`

### Request DTO

```json
{
  "reason": "manual_disable"
}
```

## 10.6 `POST /admin/security-policies/{id}/rollback`

### Request DTO

```json
{
  "target_version": "1",
  "reason": "manual_rollback"
}
```

## 10.7 `POST /runtime/security/check`

### Request DTO

```json
{
  "check_type": "collaboration",
  "source_colleague_id": "col_001",
  "target_colleague_id": "col_002",
  "task_type": "approval",
  "message_type": "assist_request",
  "context": {
    "biz_object_type": "purchase_request"
  }
}
```

### Response DTO

```json
{
  "result": "allowed",
  "hit_policy_ids": [],
  "reason": "passed"
}
```

---

## 11. 配置下发 DTO

## 11.1 `GET /admin/config-bundles`

### Item DTO

```json
{
  "id": "cb_001",
  "bundle_type": "colleague_profile",
  "version": "2026.04.05.1",
  "change_summary": "更新采购助理配置",
  "status": "published",
  "published_at": "2026-04-05T10:00:00Z",
  "created_at": "2026-04-05T09:50:00Z"
}
```

## 11.2 `POST /admin/config-bundles`

### Request DTO

```json
{
  "bundle_type": "colleague_profile",
  "version": "2026.04.05.1",
  "change_summary": "更新采购助理配置",
  "payload_ref": "obj://config-bundles/cb_001.json",
  "target_scope": {
    "type": "all_clients",
    "values": []
  }
}
```

### Response DTO

```json
{
  "id": "cb_001",
  "status": "draft"
}
```

## 11.3 `POST /admin/config-bundles/{id}/publish`

### Request DTO

```json
{
  "reason": "manual_publish"
}
```

### Response DTO

```json
{
  "id": "cb_001",
  "status": "published"
}
```

## 11.4 `POST /admin/config-bundles/{id}/rollback`

### Request DTO

```json
{
  "target_version": "2026.04.04.1",
  "reason": "manual_rollback"
}
```

## 11.5 `GET /client/config/version`

### Response DTO

```json
{
  "latest_version": "2026.04.05.1",
  "bundle_types": [
    {
      "bundle_type": "colleague_profile",
      "version": "2026.04.05.1"
    },
    {
      "bundle_type": "security_policy",
      "version": "2026.04.05.1"
    }
  ]
}
```

## 11.6 `GET /client/config/bundles`

### Item DTO

```json
{
  "id": "cb_001",
  "bundle_type": "colleague_profile",
  "version": "2026.04.05.1",
  "change_summary": "更新采购助理配置",
  "payload_ref": "obj://config-bundles/cb_001.json",
  "published_at": "2026-04-05T10:00:00Z"
}
```

## 11.7 `GET /client/config/bundles/{id}`

### Response DTO

```json
{
  "id": "cb_001",
  "bundle_type": "colleague_profile",
  "version": "2026.04.05.1",
  "change_summary": "更新采购助理配置",
  "payload": {
    "colleagues": []
  }
}
```

## 11.8 `POST /client/config/apply-result`

### Request DTO

```json
{
  "config_bundle_version_id": "cb_001",
  "client_id": "client_001",
  "client_version": "1.0.0",
  "apply_status": "success",
  "failure_reason": "",
  "detail": {
    "applied_items": 10
  },
  "applied_at": "2026-04-05T10:05:00Z"
}
```

### Response DTO

```json
{
  "accepted": true
}
```

---

## 12. 错误响应建议

建议统一错误结构：

```json
{
  "code": 40001,
  "message": "invalid status transition",
  "data": {
    "field": "status",
    "current_status": "completed"
  }
}
```

建议至少统一以下错误类型：
- 参数错误
- 资源不存在
- 状态流转非法
- 权限不允许
- 安全规则拦截
- 版本冲突
- 外部依赖失败

---

## 13. V1 最小必做 DTO 集

建议第一批先落这些 request/response 结构：

### 13.1 数字员工
- `CreateColleagueRequest`
- `UpdateColleagueRequest`
- `ColleagueDetailResponse`
- `ClientColleagueListItem`

### 13.2 能力包
- `ImportCapabilityRequest`
- `ReviewCapabilityRequest`
- `BindCapabilityRequest`
- `CapabilityDetailResponse`

### 13.3 协作委托
- `CreateCollaborationRequest`
- `RejectCollaborationRequest`
- `CompleteCollaborationRequest`
- `CollaborationDetailResponse`
- `InboxItemResponse`

### 13.4 工作流
- `CreateWorkflowDefinitionRequest`
- `CreateWorkflowStepDefinitionRequest`
- `CreateWorkflowInstanceRequest`
- `CompleteWorkflowStepRequest`
- `RejectWorkflowStepRequest`
- `WorkflowInstanceDetailResponse`

### 13.5 模型与代理
- `CreateModelEndpointRequest`
- `CreateModelRoutingPolicyRequest`
- `ProxyLLMRequest`
- `ProxyAPIRequest`

### 13.6 安全与下发
- `CreateSecurityPolicyRequest`
- `SecurityCheckRequest`
- `CreateConfigBundleRequest`
- `ConfigApplyResultRequest`

---

## 14. 一句话总结

**V1 的 DTO 设计应优先保证“接口语义直接、字段与实体一致、前后端可共享、状态流转可表达”，先把最小闭环接口的 request/response 定稳，再补充次级查询与统计接口。**
