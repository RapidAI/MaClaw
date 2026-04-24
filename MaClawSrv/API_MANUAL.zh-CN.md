# MaClawSrv API 对接手册（中文）

本文面向需要直接调用 `MaClawSrv` 的 AI 工具、自动化代理、桌面客户端、控制面和其他外部程序。目标是让你不用反复翻源码，就能快速完成集成，并明确知道每个接口的用途、输入、输出和推荐调用方式。

## 1. 服务定位

`MaClawSrv` 是 Maclaw 的 REST 服务入口，具备以下特性：

- 多租户、多用户隔离，隔离层级是 `tenant -> user`。
- 同一个 user 下可以同时运行多个 instance。
- 同一 user 下的 instance 共享同一份配置、记忆、skill、历史数据。
- core 能力复用 `corelib/agentservice` 和共享 agent 运行时。
- 这是纯 agent 服务，不是 coding session 编排服务。
- skill 管理和 MCP 管理也通过 REST 暴露。

## 2. 基础地址与健康检查

默认监听地址：

```text
http://127.0.0.1:18080
```

健康检查：

```http
GET /health
```

返回：

```json
{
  "status": "ok"
}
```

传输建议：

- 默认情况下，明文 HTTP 仅建议用于 loopback。
- 远程部署请配置 `MACLAW_TLS_CERT_FILE` 和 `MACLAW_TLS_KEY_FILE`。
- 不要通过不安全远程 HTTP 传输管理密钥、API secret 或 Bearer token。

## 3. 重要环境变量

必填安全变量：

- `MACLAW_ADMIN_SECRET`：管理接口密钥，至少 24 位。
- `MACLAW_TOKEN_SECRET`：Bearer token 签名密钥，至少 32 位。

常用运行变量：

- `MACLAW_HTTP_ADDR`：监听地址，默认 `:18080`。
- `MACLAW_DATA_ROOT`：数据根目录，默认 `~/.maclaw_srv`。
- `MACLAW_TLS_CERT_FILE`：TLS 证书路径。
- `MACLAW_TLS_KEY_FILE`：TLS 私钥路径。
- `MACLAW_ALLOW_INSECURE_HTTP`：设置为 `true` 时允许非 loopback 明文 HTTP。
- `MACLAW_CREDENTIAL_PEPPER`：可选，凭证摘要额外 pepper。

与本地 bash 相关的变量：

- `MACLAW_ENABLE_LOCAL_BASH`
- `MACLAW_LOCAL_BASH_TRUSTED_SINGLE_USER`
- `MACLAW_LOCAL_BASH_TENANT_ID`
- `MACLAW_LOCAL_BASH_USER_ID`

说明：

- 本地 bash 是高敏感能力，多租户部署一般不要开启，除非外层已有 OS 级或容器级隔离。

## 4. 鉴权模型

MaClawSrv 有两层鉴权：管理面接口和用户运行时接口。

### 4.1 管理接口

管理接口使用固定 Header：

```http
X-MaClaw-Admin-Secret: <admin-secret>
```

主要用于租户、用户、凭证、审计日志等控制面操作。

### 4.2 用户接口

用户接口使用 Bearer Token。

标准流程：

1. 管理端先创建 tenant / user / credential。
2. 客户端用 `api_key + api_secret` 交换 token。
3. 后续所有用户态接口携带 `Authorization: Bearer <token>`。

换 token：

```http
POST /api/v1/auth/token
Content-Type: application/json
```

请求体：

```json
{
  "api_key": "ak_xxx",
  "api_secret": "secret_xxx"
}
```

字段说明：

- `api_key`：凭证公钥。
- `api_secret`：凭证密钥，只在客户端保存，服务端不会再次明文返回。

返回示例：

```json
{
  "access_token": "...",
  "token_type": "Bearer",
  "expires_at": "2026-04-23T20:30:00Z",
  "principal": {
    "tenant_id": "tenant_xxx",
    "user_id": "user_xxx"
  }
}
```

返回字段说明：

- `access_token`：后续 Bearer token。
- `token_type`：固定为 `Bearer`。
- `expires_at`：过期时间，RFC3339。
- `principal.tenant_id`：当前租户。
- `principal.user_id`：当前用户。

查当前用户：

```http
GET /api/v1/me
Authorization: Bearer <token>
```

返回的是 `User` 对象，典型字段：

- `id`
- `tenant_id`
- `name`
- `email`
- `status`
- `created_at`
- `updated_at`

## 5. 推荐对接顺序

对 AI 工具和桌面客户端，推荐按以下顺序接入：

1. `POST /api/v1/auth/token`
2. `GET /api/v1/me`
3. `GET /api/v1/config/schema`
4. `GET /api/v1/config`
5. `POST /api/v1/config/validate`
6. 必要时 `PUT /api/v1/config`
7. 需要真实连通性检查时，`POST /api/v1/config/test`
8. `POST /api/v1/instances`
9. `GET /api/v1/instances/{instanceId}/capabilities`
10. `POST /api/v1/instances/{instanceId}/messages`
11. 轮询 `GET /api/v1/instances/{instanceId}/runs/{runId}` 或读取消息列表

这样接入可以把“配置缺失”和“运行期失败”清晰拆开，也便于 UI 先把配置问题暴露给用户。

## 6. 通用 Header

用户接口：

```http
Authorization: Bearer <token>
Accept: application/json
Content-Type: application/json
```

管理接口：

```http
X-MaClaw-Admin-Secret: <admin-secret>
Accept: application/json
Content-Type: application/json
```

## 7. 错误模型

常见错误格式：

```json
{
  "error": "..."
}
```

配置问题引发的错误可能附带：

```json
{
  "error": "invalid config",
  "config_validation": {
    "valid": false,
    "issues": [
      {
        "key": "maclaw_llm_key",
        "message": "is required"
      }
    ]
  }
}
```

推荐按状态码处理：

- `400`：请求字段错误、配置不完整或业务前置条件不满足。
- `401`：鉴权失败。
- `404`：tenant、user、instance、session、run、skill、MCP server 不存在。
- `409`：状态冲突，例如运行已取消、已归档会话不可继续写入。
- `429`：登录或 token 交换被限流，建议重试。
- `502`：下游执行器或外部依赖失败，接口通常会带上已创建的 `run` 信息。
- `500`：服务端异常。

## 8. 分页模型

多个列表接口支持游标分页。

查询参数：

- `limit`：默认 `100`，最大 `500`。
- `before`：RFC3339 或 RFC3339Nano 时间戳。

典型返回：

```json
{
  "items": [],
  "limit": 100,
  "has_more": true,
  "next_before": "2026-04-23T18:00:00Z"
}
```

字段说明：

- `items`：当前页数据。
- `limit`：本次实际采用的分页大小。
- `has_more`：是否还有下一页。
- `next_before`：下一次请求应携带的 `before` 值。

支持分页的接口：

- `GET /api/v1/instances`
- `GET /api/v1/instances/{instanceId}/sessions`
- `GET /api/v1/instances/{instanceId}/sessions/{sessionId}/messages`
- `GET /api/v1/instances/{instanceId}/runs`
- `GET /api/v1/admin/audit-events`

## 9. 管理接口

这些接口通常由控制面、安装器、开通工具或租户管理后台调用。

### 9.1 创建租户

```http
POST /api/v1/admin/tenants
```

请求体：

```json
{
  "name": "Acme"
}
```

字段说明：

- `name`：租户展示名。

返回：`Tenant` 对象。

主要字段：

- `id`：租户 ID。
- `name`：租户名称。
- `status`：`active` 或 `disabled`。
- `created_at`、`updated_at`：时间戳。

### 9.2 查询租户列表

```http
GET /api/v1/admin/tenants
```

返回：

```json
{
  "items": [
    {
      "id": "tenant_xxx",
      "name": "Acme",
      "status": "active"
    }
  ]
}
```

### 9.3 查询 / 更新单个租户

```http
GET /api/v1/admin/tenants/{tenantId}
PATCH /api/v1/admin/tenants/{tenantId}
```

更新请求体可包含：

- `name`：更新名称。
- `status`：`active` 或 `disabled`。

示例：

```json
{
  "status": "disabled"
}
```

### 9.4 创建用户

```http
POST /api/v1/admin/tenants/{tenantId}/users
```

请求体：

```json
{
  "name": "Alice",
  "email": "alice@example.com"
}
```

字段说明：

- `name`：用户名。
- `email`：可选，联系邮箱或登录映射信息。

返回：`User` 对象。

主要字段：

- `id`
- `tenant_id`
- `name`
- `email`
- `status`
- `created_at`
- `updated_at`

### 9.5 查询 / 更新用户

```http
GET /api/v1/admin/tenants/{tenantId}/users
GET /api/v1/admin/tenants/{tenantId}/users/{userId}
PATCH /api/v1/admin/tenants/{tenantId}/users/{userId}
```

更新请求体可包含：

- `name`
- `email`
- `status`：`active` 或 `disabled`

### 9.6 创建凭证

```http
POST /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials
```

请求体：

```json
{
  "name": "default-client",
  "api_key": "ak_demo_client",
  "api_secret": "super-secret"
}
```

字段说明：

- `name`：凭证用途说明，便于后台识别。
- `api_key`：公开标识。
- `api_secret`：客户端保存的私密值。

返回：`Credential` 对象。

主要字段：

- `id`
- `tenant_id`
- `user_id`
- `name`
- `api_key`
- `api_key_prefix`
- `status`
- `created_at`
- `updated_at`

注意：

- `api_secret` 只在创建时由客户端自己掌握，后续接口不会明文返回。
- Bearer token 和创建它的 credential 绑定。

### 9.7 查询 / 撤销凭证

```http
GET /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials
DELETE /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials/{credentialId}
```

### 9.8 审计事件

```http
GET /api/v1/admin/audit-events
```

可选查询参数：

- `tenant_id`
- `user_id`
- `action`
- `resource_type`
- `limit`
- `before`

返回项为 `AuditEvent`，主要字段：

- `id`
- `tenant_id`
- `user_id`
- `actor_type`
- `actor_tenant_id`
- `actor_user_id`
- `action`
- `resource_type`
- `resource_id`
- `metadata`
- `created_at`

## 10. 用户配置接口

每个 user 只有一份共享配置，该 user 下所有 instance 共用。

### 10.1 查询配置 schema

```http
GET /api/v1/config/schema
```

返回：

```json
{
  "items": [
    {
      "key": "maclaw_llm_url",
      "title": "LLM URL",
      "description": "...",
      "required": true,
      "secret": false,
      "type": "string",
      "example": "https://api.openai.com/v1"
    }
  ]
}
```

字段说明：

- `key`：配置字段名。
- `title`：展示标题。
- `description`：字段用途说明。
- `required`：是否必填。
- `secret`：是否敏感字段。
- `type`：字段类型，例如 `string`。
- `example`：建议示例值。

用途：

- 如果你要做动态设置页，优先依赖这个接口生成表单，而不是把配置字段写死在客户端。

### 10.2 查询当前配置

```http
GET /api/v1/config
```

返回示例：

```json
{
  "tenant_id": "tenant_xxx",
  "user_id": "user_xxx",
  "app_config": {
    "maclaw_llm_url": "https://api.openai.com/v1",
    "maclaw_llm_model": "gpt-5.4"
  },
  "updated_at": "2026-04-23T18:00:00Z"
}
```

字段说明：

- `tenant_id`、`user_id`：当前配置所属用户。
- `app_config`：实际生效的 Maclaw 配置。
- `updated_at`：最后更新时间。

说明：

- 返回时敏感字段会被脱敏。
- 如果用户还没有保存配置，服务会返回空的 `app_config`。

### 10.3 更新配置

```http
PUT /api/v1/config
```

请求体直接传 `AppConfig` JSON：

```json
{
  "maclaw_llm_url": "https://api.openai.com/v1",
  "maclaw_llm_key": "sk-...",
  "maclaw_llm_model": "gpt-5.4"
}
```

说明：

- `PUT /config` 只接受原始配置对象，不需要再包 `app_config` 外层。
- 现有 secret 字段会按服务端规则保留合并，不要求每次更新都把旧 secret 原样带回。

返回：更新后的 `UserConfig`。

### 10.4 校验配置但不保存

```http
POST /api/v1/config/validate
```

支持三种调用方式：

- 空 body：校验当前已保存配置。
- 直接传 `AppConfig`：校验候选配置。
- 传 `{"app_config": {...}}`：同样是校验候选配置。

返回示例：

```json
{
  "valid": false,
  "issues": [
    {
      "key": "maclaw_llm_model",
      "message": "is required"
    }
  ]
}
```

字段说明：

- `valid`：是否通过。
- `issues[].key`：出问题的配置键。
- `issues[].message`：可直接展示给用户的错误说明。

### 10.5 测试配置真实连通性

```http
POST /api/v1/config/test
```

请求体格式与 `validate` 相同。

返回示例：

```json
{
  "success": true,
  "message": "ok",
  "latency_ms": 812,
  "endpoint": "https://api.openai.com/v1",
  "provider_name": "openai",
  "model": "gpt-5.4",
  "protocol": "openai",
  "wire_api": "chat_completions"
}
```

字段说明：

- `success`：是否连通成功。
- `message`：成功提示。
- `error`：失败原因。
- `latency_ms`：请求时延。
- `endpoint`：实际测试的服务地址。
- `provider_name`：识别出的 provider。
- `model`：参与测试的模型。
- `protocol`、`wire_api`：底层协议识别信息。
- `validation`：如果同时存在配置问题，会附带校验结果。

## 11. 实例接口

instance 是一个逻辑运行入口，不单独拥有配置和长期记忆，而是复用 user 级共享数据。

### 11.1 查询实例列表

```http
GET /api/v1/instances
```

支持分页参数：`limit`、`before`。

返回项为 `Instance`，主要字段：

- `id`
- `tenant_id`
- `user_id`
- `name`
- `data_dir`
- `runtime_dir`
- `workspace_dir`
- `status`
- `ready`
- `ready_reason`
- `readiness`
- `description`
- `metadata`
- `config_validation`
- `created_at`
- `updated_at`

关键语义：

- `status`：当前生命周期状态，常见值是 `ready` 或 `stopped`。
- `ready`：当前是否可用。
- `ready_reason`：不可用时的直接原因。
- `readiness.ready`：更结构化的就绪状态。
- `readiness.config_valid`：配置是否通过校验。
- `readiness.has_llm_config`：是否具备最小 LLM 配置。
- `config_validation.issues`：可直接展示给用户的配置问题列表。

### 11.2 创建实例

```http
POST /api/v1/instances
```

请求体：

```json
{
  "name": "primary-agent",
  "description": "default assistant entrypoint",
  "metadata": {
    "channel": "wails-demo"
  }
}
```

字段说明：

- `name`：实例名称。
- `description`：实例说明。
- `metadata`：业务自定义标签，原样透传保存。

返回：创建后的 `Instance`。

说明：

- 如果配置不完整，接口可能返回 `400`，并附带 `config_validation`，适合直接展示到设置页。

### 11.3 查询单个实例

```http
GET /api/v1/instances/{instanceId}
```

适合在实例详情页重点展示：

- `status`
- `ready`
- `ready_reason`
- `readiness`
- `config_validation`
- `data_dir`
- `runtime_dir`
- `workspace_dir`

### 11.4 更新实例

```http
PATCH /api/v1/instances/{instanceId}
```

请求体可包含：

- `name`
- `description`
- `metadata`

说明：

- 这个接口只修改实例可变元数据，不改变运行状态。

### 11.5 删除实例

```http
DELETE /api/v1/instances/{instanceId}
```

返回：

```json
{
  "status": "deleted"
}
```

### 11.6 查询实例能力

```http
GET /api/v1/instances/{instanceId}/capabilities
```

这是 AI 客户端非常重要的一个接口，推荐在实例选中后立即调用。

返回主要字段：

- `executor`：底层执行器类型。
- `supports_sessions`：是否支持会话模式。
- `supports_ask_user`：是否支持结构化追问。
- `supports_ssh`：是否暴露 SSH。
- `supports_local_bash`：是否暴露本地 bash。
- `tools`：当前可用工具列表。
- `metadata`：能力相关附加信息。

`tools[]` 字段说明：

- `name`：工具名。
- `description`：工具说明。
- `enabled`：当前是否启用。
- `disabled_reason`：禁用原因。
- `parameters`：参数 schema，可直接驱动 UI 表单。

### 11.7 停止 / 恢复 / 刷新就绪状态

```http
POST /api/v1/instances/{instanceId}/stop
POST /api/v1/instances/{instanceId}/resume
POST /api/v1/instances/{instanceId}/refresh-readiness
```

返回：最新的 `Instance`。

### 11.8 查询 bootstrap 描述

```http
GET /api/v1/instances/{instanceId}/bootstrap
```

返回 `InstanceBootstrap`，主要字段：

- `instance_id`
- `tenant_id`
- `user_id`
- `data_dir`
- `runtime_dir`
- `workspace_dir`
- `config_path`
- `conversation_store_path`
- `confirmation_store_path`
- `metadata`
- `generated_at`

## 12. 会话、消息与运行接口

### 12.1 推荐方式：一步发消息

```http
POST /api/v1/instances/{instanceId}/messages
```

请求体字段：

- `session_id`：可选，继续旧会话。
- `agent_id`：可选，新建会话时指定 agent ID。
- `title`：可选，新会话标题。
- `content`：必填，用户输入正文。
- `input_type`：可选，例如 `text`。
- `metadata`：可选，消息级元数据。
- `session_metadata`：可选，新会话的元数据。
- `client_session_key`：可选，客户端幂等辅助键。
- `client_message_id`：可选，客户端消息关联 ID。

示例：

```json
{
  "title": "Demo session",
  "content": "Please summarize current project status and next steps.",
  "client_message_id": "msg_local_001"
}
```

成功返回示例：

```json
{
  "session": {
    "id": "sess_xxx"
  },
  "run": {
    "id": "run_xxx",
    "status": "succeeded"
  },
  "message": {
    "id": "msg_xxx",
    "role": "assistant",
    "content": "..."
  }
}
```

### 12.2 显式创建会话

```http
POST /api/v1/instances/{instanceId}/sessions
```

请求体：

```json
{
  "agent_id": "default",
  "title": "Planning",
  "metadata": {
    "source": "external-tool"
  }
}
```

### 12.3 查询会话列表

```http
GET /api/v1/instances/{instanceId}/sessions
```

支持查询参数：

- `include_archived`
- `limit`
- `before`

返回项为 `Session`，主要字段：

- `id`
- `tenant_id`
- `user_id`
- `instance_id`
- `agent_id`
- `title`
- `metadata`
- `archived`
- `archived_at`
- `waiting_for_user`
- `pending_ask`
- `last_message_at`
- `created_at`
- `updated_at`

### 12.4 查询 / 更新 / 删除单个会话

```http
GET /api/v1/instances/{instanceId}/sessions/{sessionId}
PATCH /api/v1/instances/{instanceId}/sessions/{sessionId}
DELETE /api/v1/instances/{instanceId}/sessions/{sessionId}
```

更新请求体可包含：

- `title`
- `metadata`

### 12.5 归档 / 恢复会话

```http
POST /api/v1/instances/{instanceId}/sessions/{sessionId}/archive
POST /api/v1/instances/{instanceId}/sessions/{sessionId}/restore
```

说明：

- 已归档会话默认不会出现在列表里。
- 归档会话不能继续 `POST message`，否则通常返回 `409`。
- 需要显示“历史对话”时，查询列表请加 `include_archived=true`。

### 12.6 查询消息列表

```http
GET /api/v1/instances/{instanceId}/sessions/{sessionId}/messages
```

支持分页参数：`limit`、`before`。

返回项为 `Message`，主要字段：

- `id`
- `session_id`
- `tenant_id`
- `user_id`
- `instance_id`
- `role`
- `input_type`
- `output_type`
- `content`
- `metadata`
- `created_at`

### 12.7 在指定会话中发消息

```http
POST /api/v1/instances/{instanceId}/sessions/{sessionId}/messages
```

请求体：

```json
{
  "content": "Continue from the previous result.",
  "input_type": "text",
  "metadata": {
    "source": "external-tool"
  }
}
```

成功返回：

```json
{
  "run": {
    "id": "run_xxx"
  },
  "message": {
    "id": "msg_xxx",
    "role": "assistant",
    "content": "..."
  }
}
```

### 12.8 查询运行列表

```http
GET /api/v1/instances/{instanceId}/runs
```

支持查询参数：

- `status`
- `session_id`
- `limit`
- `before`

返回项为 `Run`，主要字段：

- `id`
- `tenant_id`
- `user_id`
- `instance_id`
- `session_id`
- `user_message_id`
- `assistant_message_id`
- `status`
- `error`
- `response_source`
- `waiting_for_user`
- `duration_ms`
- `started_at`
- `completed_at`
- `metadata`

### 12.9 查询单个运行

```http
GET /api/v1/instances/{instanceId}/runs/{runId}
```

### 12.10 订阅运行事件流

```http
GET /api/v1/instances/{instanceId}/runs/{runId}/events
```

返回类型：`text/event-stream`。

事件类型：

- `snapshot`
- `done`
- `error`

数据体示例：

```json
{
  "type": "snapshot",
  "snapshot": {
    "run": {
      "id": "run_xxx",
      "status": "running"
    },
    "session": {
      "id": "sess_xxx"
    },
    "assistant_message": {
      "id": "msg_xxx",
      "content": "partial or final content"
    }
  }
}
```

### 12.11 取消运行

```http
POST /api/v1/instances/{instanceId}/runs/{runId}/cancel
```

返回：取消后的 `Run` 对象。

## 13. 使用量汇总接口

```http
GET /api/v1/usage/summary
```

返回字段：

- `tenant_id`
- `user_id`
- `data_dir`
- `instances`
- `ready_instances`
- `stopped_instances`
- `sessions`
- `messages`
- `user_messages`
- `assistant_messages`
- `runs`
- `runs_by_status`
- `last_activity_at`

## 14. Skill 接口

skill 是 user 级共享资源；同一用户下所有 instance 共用同一套 skill。

### 14.1 查询已安装 skill

```http
GET /api/v1/skills
```

### 14.2 搜索远程 skill 源

```http
POST /api/v1/skills/search
```

请求体：

```json
{
  "query": "ssh deploy",
  "sources": ["github", "skillmarket"],
  "top_n": 10,
  "include_installed": true,
  "skill_hub_url": "https://skillhub.example.com",
  "skill_market_url": "https://market.example.com",
  "github_token": "ghp_xxx"
}
```

### 14.3 安装 skill

```http
POST /api/v1/skills/install
```

GitHub 示例：

```json
{
  "source": "github",
  "repo_full_name": "owner/repo",
  "file_path": "skills/my-skill/SKILL.md",
  "branch": "main",
  "overwrite": true,
  "github_token": "ghp_xxx"
}
```

SkillHub 示例：

```json
{
  "source": "skillhub",
  "skill_id": "skill_xxx",
  "skill_hub_url": "https://skillhub.example.com"
}
```

Zip 示例：

```json
{
  "source": "zip",
  "zip_base64": "<base64-zip>",
  "overwrite": true
}
```

### 14.4 导入 skill 压缩包

```http
POST /api/v1/skills/import
```

请求体：

```json
{
  "zip_base64": "<base64-zip>",
  "overwrite": true,
  "archive_name": "demo-skill.zip"
}
```

### 14.5 查询 / 删除 / 导出 skill

```http
GET /api/v1/skills/{skillName}
DELETE /api/v1/skills/{skillName}
GET /api/v1/skills/{skillName}/export
```

### 14.6 校验与改进 skill

```http
POST /api/v1/skills/{skillName}/validate
POST /api/v1/skills/{skillName}/improve
```

`improve` 请求体：

```json
{
  "auto_fix": true
}
```

### 14.7 发布 skill 到 market

```http
POST /api/v1/skills/{skillName}/upload
```

请求体：

```json
{
  "skill_market_url": "https://market.example.com",
  "email": "author@example.com"
}
```

查询异步上传状态：

```http
GET /api/v1/skill-uploads/{submissionId}
```

查询 market 账户资料：

```http
GET /api/v1/skill-market/account?email=author@example.com&base_url=https://market.example.com
```

## 15. MCP 接口

MCP server 也是 user 级共享资源；同一用户下所有 instance 共用。

支持两种类型：

- `remote`
- `local`

### 15.1 查询 MCP server 列表

```http
GET /api/v1/mcp/servers
```

### 15.2 创建 MCP server

```http
POST /api/v1/mcp/servers
```

远程 MCP 示例：

```json
{
  "kind": "remote",
  "name": "Docs MCP",
  "endpoint_url": "https://mcp.example.com",
  "auth_type": "bearer",
  "auth_secret": "token_xxx",
  "headers": {
    "X-Client": "maclawsrv"
  }
}
```

本地 MCP 示例：

```json
{
  "kind": "local",
  "name": "Filesystem MCP",
  "command": "npx",
  "args": [
    "-y",
    "@modelcontextprotocol/server-filesystem",
    "D:\\workprj\\aicoder"
  ],
  "env": {
    "NODE_ENV": "production"
  },
  "auto_start": true,
  "disabled": false
}
```

### 15.3 查询 / 更新 / 删除 MCP server

```http
GET /api/v1/mcp/servers/{serverId}
PATCH /api/v1/mcp/servers/{serverId}
DELETE /api/v1/mcp/servers/{serverId}
```

### 15.4 启动 / 停止 / 健康检查 / 查询工具

```http
POST /api/v1/mcp/servers/{serverId}/start
POST /api/v1/mcp/servers/{serverId}/stop
POST /api/v1/mcp/servers/{serverId}/health-check
GET /api/v1/mcp/servers/{serverId}/tools
```

## 16. 端到端快速示例

### 16.1 管理侧初始化

```bash
curl -s http://127.0.0.1:18080/api/v1/admin/tenants \
  -H 'X-MaClaw-Admin-Secret: your-admin-secret' \
  -H 'Content-Type: application/json' \
  -d '{"name":"Demo Tenant"}'
```

```bash
curl -s http://127.0.0.1:18080/api/v1/admin/tenants/tenant_xxx/users \
  -H 'X-MaClaw-Admin-Secret: your-admin-secret' \
  -H 'Content-Type: application/json' \
  -d '{"name":"Demo User","email":"demo@example.com"}'
```

```bash
curl -s http://127.0.0.1:18080/api/v1/admin/tenants/tenant_xxx/users/user_xxx/credentials \
  -H 'X-MaClaw-Admin-Secret: your-admin-secret' \
  -H 'Content-Type: application/json' \
  -d '{"name":"demo-client","api_key":"ak_demo","api_secret":"demo-secret"}'
```

### 16.2 登录换 token

```bash
curl -s http://127.0.0.1:18080/api/v1/auth/token \
  -H 'Content-Type: application/json' \
  -d '{"api_key":"ak_demo","api_secret":"demo-secret"}'
```

### 16.3 保存配置

```bash
curl -s http://127.0.0.1:18080/api/v1/config \
  -X PUT \
  -H 'Authorization: Bearer <token>' \
  -H 'Content-Type: application/json' \
  -d '{"maclaw_llm_url":"https://api.openai.com/v1","maclaw_llm_key":"sk-xxx","maclaw_llm_model":"gpt-5.4"}'
```

### 16.4 创建实例

```bash
curl -s http://127.0.0.1:18080/api/v1/instances \
  -H 'Authorization: Bearer <token>' \
  -H 'Content-Type: application/json' \
  -d '{"name":"primary-agent","description":"demo"}'
```

### 16.5 发消息

```bash
curl -s http://127.0.0.1:18080/api/v1/instances/inst_xxx/messages \
  -H 'Authorization: Bearer <token>' \
  -H 'Content-Type: application/json' \
  -d '{"title":"Demo","content":"Please introduce your capabilities."}'
```

## 17. 按角色快速接入

### 17.1 AI 对话客户端

适用对象：桌面 AI 助手、聊天前端、IDE 插件、Agent 壳层。

推荐调用链：

1. `POST /api/v1/auth/token`
2. `GET /api/v1/me`
3. `GET /api/v1/config`
4. `POST /api/v1/config/validate`
5. `POST /api/v1/instances`
6. `GET /api/v1/instances/{instanceId}/capabilities`
7. `POST /api/v1/instances/{instanceId}/messages`
8. `GET /api/v1/instances/{instanceId}/runs/{runId}` 或 `GET /api/v1/instances/{instanceId}/runs/{runId}/events`
9. `GET /api/v1/instances/{instanceId}/sessions/{sessionId}/messages`

实现重点：

- 首次登录后立即绑定 `tenant_id + user_id`，避免把不同用户状态混用。
- 聊天 UI 以 `messages` 列表为准，不要只依赖 `run` 返回体。
- 如果 `waiting_for_user=true`，要继续读 `session.pending_ask` 决定下一步交互。
- UI 中的工具入口、是否显示 ask-user / bash / SSH，都应由 `capabilities` 决定。

### 17.2 控制面 / 管理后台

适用对象：SaaS 控制台、开通后台、租户管理页面、运维面板。

推荐调用链：

1. `POST /api/v1/admin/tenants`
2. `POST /api/v1/admin/tenants/{tenantId}/users`
3. `POST /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials`
4. `GET /api/v1/admin/tenants`
5. `GET /api/v1/admin/tenants/{tenantId}/users`
6. `GET /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials`
7. `GET /api/v1/admin/audit-events`

实现重点：

- 管理后台不应直接保存 bearer token，而应保存 tenant/user/credential 基础信息。
- `audit-events` 适合做审计查询页、时间线页和问题追踪页。
- 用户禁用、租户禁用、凭证撤销这三种控制动作建议在 UI 上分开表达。

### 17.3 设置页 / 配置向导

适用对象：首次启动向导、设置页、配置诊断页。

推荐调用链：

1. `GET /api/v1/config/schema`
2. `GET /api/v1/config`
3. `POST /api/v1/config/validate`
4. `POST /api/v1/config/test`
5. `PUT /api/v1/config`
6. `POST /api/v1/instances/{instanceId}/refresh-readiness`

实现重点：

- 表单项优先由 `config/schema` 驱动，不要把字段名写死在前端。
- `validate` 用于表单内联校验，`test` 用于“连通性检测”按钮。
- 保存配置后，如果实例已存在，记得刷新 readiness，而不是要求用户重建实例。

### 17.4 Skill / MCP 管理页

适用对象：技能市场页、能力扩展页、MCP 管理页。

推荐调用链：

1. `GET /api/v1/skills`
2. `POST /api/v1/skills/search`
3. `POST /api/v1/skills/install` 或 `POST /api/v1/skills/import`
4. `POST /api/v1/skills/{skillName}/validate`
5. `POST /api/v1/skills/{skillName}/upload`
6. `GET /api/v1/mcp/servers`
7. `POST /api/v1/mcp/servers`
8. `POST /api/v1/mcp/servers/{serverId}/health-check`
9. `GET /api/v1/mcp/servers/{serverId}/tools`

实现重点：

- skill 和 MCP 都是 user 级共享资源，不要绑定到某个 instance 页面里做私有化假设。
- MCP 新建后先做 `health-check`，再把 `tools` 展示给用户，体验会更稳定。
- skill 导入、导出、发布都可能涉及大包体，前端要处理进度和失败提示。

## 18. 常见调用场景

### 18.1 首次可用链路

最短链路：

1. 管理端创建 tenant / user / credential。
2. 客户端换 token。
3. 客户端补全 config。
4. 创建 instance。
5. 查询 capabilities。
6. 发第一条消息。

### 18.2 配置报错链路

当 `create instance` 或 `send message` 返回配置错误时，推荐这样处理：

1. 读取返回体中的 `config_validation.issues`。
2. 跳转设置页或弹出配置面板。
3. 调 `GET /api/v1/config/schema` 重新渲染字段说明。
4. 用户修改后先 `POST /api/v1/config/validate`。
5. 必要时 `POST /api/v1/config/test`。
6. 保存后刷新实例 readiness。

### 18.3 实时对话链路

如果希望更像聊天产品的实时体验：

1. `POST /api/v1/instances/{instanceId}/messages`
2. 拿到 `run.id`
3. 建立 `GET /api/v1/instances/{instanceId}/runs/{runId}/events` SSE
4. 收到 `done` 后再拉一次消息列表作为最终一致结果

### 18.4 历史会话链路

如果产品有“历史会话”页：

1. `GET /api/v1/instances/{instanceId}/sessions`
2. 用户进入会话后调 `GET /messages`
3. 用户归档会话时调 `POST /archive`
4. 历史会话页使用 `include_archived=true`
5. 恢复会话时调 `POST /restore`

## 19. AI 客户端集成建议

- 登录后立即调 `GET /api/v1/me`，把本地状态绑定到 `tenant_id + user_id`。
- 把 config 视为 user 级共享状态，不要误当成 instance 私有配置。
- skill 和 MCP server 也都是 user 级共享资源。
- 优先使用 `POST /instances/{id}/messages`，除非你真的需要显式管理 session 生命周期。
- 用 `GET /instances/{id}/capabilities` 动态决定 UI，而不是假设本地 bash、SSH 或 ask_user 一定存在。
- 用 `waiting_for_user` 和 `pending_ask` 支持结构化追问交互。
- 把 `config_validation.issues` 直接展示给用户，不要吞掉。
- 需要更实时状态时优先用 run SSE；不方便处理流时再用轮询。
- 不要假定 `MaClawSrv` 会提供 coding-session 编排类接口，它定位是 agent 运行服务。

## 20. 最小客户端能力清单

一个可用的生产级调用方，至少应支持：

- 管理侧初始化流程，或对接外部开通系统。
- 登录、token 续期或重新登录。
- 配置读取、校验、保存、连通性测试。
- instance 创建、查询、状态刷新。
- 消息发送。
- session 与 run 轮询或 SSE 订阅。
- capability 探测。
- skill 的查询 / 搜索 / 安装 / 导入 / 导出 / 发布（按需）。
- MCP server 的创建 / 更新 / 启停 / 健康检查 / tools 拉取（按需）。
- 结构化错误渲染。

## 21. 接口总表

管理接口：

- `GET /health`
- `GET /api/v1/admin/audit-events`
- `POST /api/v1/admin/tenants`
- `GET /api/v1/admin/tenants`
- `GET /api/v1/admin/tenants/{tenantId}`
- `PATCH /api/v1/admin/tenants/{tenantId}`
- `POST /api/v1/admin/tenants/{tenantId}/users`
- `GET /api/v1/admin/tenants/{tenantId}/users`
- `GET /api/v1/admin/tenants/{tenantId}/users/{userId}`
- `PATCH /api/v1/admin/tenants/{tenantId}/users/{userId}`
- `GET /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials`
- `POST /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials`
- `DELETE /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials/{credentialId}`

鉴权与用户运行时接口：

- `POST /api/v1/auth/token`
- `GET /api/v1/me`
- `GET /api/v1/config/schema`
- `GET /api/v1/config`
- `PUT /api/v1/config`
- `POST /api/v1/config/validate`
- `POST /api/v1/config/test`
- `GET /api/v1/usage/summary`
- `GET /api/v1/instances`
- `POST /api/v1/instances`
- `GET /api/v1/instances/{instanceId}`
- `PATCH /api/v1/instances/{instanceId}`
- `DELETE /api/v1/instances/{instanceId}`
- `GET /api/v1/instances/{instanceId}/capabilities`
- `POST /api/v1/instances/{instanceId}/stop`
- `POST /api/v1/instances/{instanceId}/resume`
- `POST /api/v1/instances/{instanceId}/refresh-readiness`
- `GET /api/v1/instances/{instanceId}/bootstrap`
- `POST /api/v1/instances/{instanceId}/messages`
- `GET /api/v1/instances/{instanceId}/sessions`
- `POST /api/v1/instances/{instanceId}/sessions`
- `GET /api/v1/instances/{instanceId}/sessions/{sessionId}`
- `PATCH /api/v1/instances/{instanceId}/sessions/{sessionId}`
- `DELETE /api/v1/instances/{instanceId}/sessions/{sessionId}`
- `POST /api/v1/instances/{instanceId}/sessions/{sessionId}/archive`
- `POST /api/v1/instances/{instanceId}/sessions/{sessionId}/restore`
- `GET /api/v1/instances/{instanceId}/sessions/{sessionId}/messages`
- `POST /api/v1/instances/{instanceId}/sessions/{sessionId}/messages`
- `GET /api/v1/instances/{instanceId}/runs`
- `GET /api/v1/instances/{instanceId}/runs/{runId}`
- `GET /api/v1/instances/{instanceId}/runs/{runId}/events`
- `POST /api/v1/instances/{instanceId}/runs/{runId}/cancel`

skill 接口：

- `GET /api/v1/skills`
- `POST /api/v1/skills/search`
- `POST /api/v1/skills/install`
- `POST /api/v1/skills/import`
- `GET /api/v1/skill-uploads/{submissionId}`
- `GET /api/v1/skill-market/account`
- `GET /api/v1/skills/{skillName}`
- `DELETE /api/v1/skills/{skillName}`
- `GET /api/v1/skills/{skillName}/export`
- `POST /api/v1/skills/{skillName}/validate`
- `POST /api/v1/skills/{skillName}/improve`
- `POST /api/v1/skills/{skillName}/upload`

MCP 接口：

- `GET /api/v1/mcp/servers`
- `POST /api/v1/mcp/servers`
- `GET /api/v1/mcp/servers/{serverId}`
- `PATCH /api/v1/mcp/servers/{serverId}`
- `DELETE /api/v1/mcp/servers/{serverId}`
- `POST /api/v1/mcp/servers/{serverId}/start`
- `POST /api/v1/mcp/servers/{serverId}/stop`
- `POST /api/v1/mcp/servers/{serverId}/health-check`
- `GET /api/v1/mcp/servers/{serverId}/tools`
