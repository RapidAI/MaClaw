# MaClawSrv API 对接手册（中文）

本文面向需要直接调用 `MaClawSrv` 的 AI 工具、自动化代理、桌面客户端、控制面和其他外部程序。目标是让你不用反复翻源码，就能快速完成集成。

## 1. 服务定位

`MaClawSrv` 是 Maclaw 的 REST 服务入口，具备以下特性：

- 多租户、多用户隔离，隔离层级是 `tenant -> user`。
- 同一个 user 下可以同时运行多个 instance。
- 同一 user 下的 instance 共享同一份配置、记忆、skill、历史数据。
- core 能力复用 `corelib/agentservice` 和共享 agent 运行时。
- 这是纯 agent 服务，不是 coding session 编排服务。

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

默认情况下，非 loopback 场景应优先配置 TLS。

## 3. 重要环境变量

必填安全变量：

- `MACLAW_ADMIN_SECRET`：管理接口密钥，至少 24 位
- `MACLAW_TOKEN_SECRET`：Bearer token 签名密钥，至少 32 位

常用运行变量：

- `MACLAW_HTTP_ADDR`
- `MACLAW_DATA_ROOT`：默认 `~/.maclaw_srv`
- `MACLAW_TLS_CERT_FILE`
- `MACLAW_TLS_KEY_FILE`
- `MACLAW_ALLOW_INSECURE_HTTP`
- `MACLAW_CREDENTIAL_PEPPER`

与本地 bash 相关的变量：

- `MACLAW_ENABLE_LOCAL_BASH`
- `MACLAW_LOCAL_BASH_TRUSTED_SINGLE_USER`
- `MACLAW_LOCAL_BASH_TENANT_ID`
- `MACLAW_LOCAL_BASH_USER_ID`

多租户部署下一般不建议开启本地 bash。

## 4. 鉴权模型

### 4.1 管理接口

管理接口使用固定 Header：

```http
X-MaClaw-Admin-Secret: <admin-secret>
```

主要用于租户、用户、凭证、审计日志等控制面操作。

### 4.2 用户接口

用户接口使用 Bearer Token。

标准流程：

1. admin 先创建 tenant / user / credential
2. 客户端用 `api_key + api_secret` 换 token
3. 后续调用带 `Authorization: Bearer <token>`

换 token 接口：

```http
POST /api/v1/auth/token
Content-Type: application/json
```

```json
{
  "api_key": "ak_xxx",
  "api_secret": "secret_xxx"
}
```

查当前用户：

```http
GET /api/v1/me
Authorization: Bearer <token>
```

## 5. 推荐对接顺序

对 AI 工具来说，推荐按以下顺序接入：

1. `POST /api/v1/auth/token`
2. `GET /api/v1/me`
3. `GET /api/v1/config/schema`
4. `GET /api/v1/config`
5. `POST /api/v1/config/validate`
6. 必要时 `PUT /api/v1/config`
7. 需要真实连通性检查时，`POST /api/v1/config/test`
8. `POST /api/v1/instances`
9. `POST /api/v1/instances/{instanceId}/messages`
10. `GET /api/v1/instances/{instanceId}/runs/{runId}` 或查会话消息

这样接可以把“配置缺失”和“运行时失败”明确分开。

## 6. 配置接口

每个 user 只有一份共享配置，该 user 下所有 instance 共用。

### 6.1 查 schema

```http
GET /api/v1/config/schema
```

### 6.2 查当前配置

```http
GET /api/v1/config
```

### 6.3 更新配置

```http
PUT /api/v1/config
```

可以直接传 `AppConfig` JSON，例如：

```json
{
  "maclaw_llm_url": "https://api.openai.com/v1",
  "maclaw_llm_key": "sk-...",
  "maclaw_llm_model": "gpt-5.4"
}
```

### 6.4 校验配置

```http
POST /api/v1/config/validate
```

- 空 body：校验已保存配置
- 传 candidate config：只做干跑校验

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

### 6.5 实测配置

```http
POST /api/v1/config/test
```

这个接口会去真正测 LLM endpoint 连通性。

## 7. 实例接口

### 7.1 查实例列表

```http
GET /api/v1/instances
```

### 7.2 创建实例

```http
POST /api/v1/instances
```

```json
{
  "name": "primary-agent",
  "description": "default assistant entrypoint"
}
```

如果配置不完整，这里会返回 `config_validation` 问题。

### 7.3 查实例详情

```http
GET /api/v1/instances/{instanceId}
```

重点看以下字段：

- `status`
- `ready`
- `ready_reason`
- `readiness`
- `config_validation`

### 7.4 查实例能力

```http
GET /api/v1/instances/{instanceId}/capabilities
```

建议 AI 工具必接这个接口，用来动态判断：

- 是否支持 sessions
- 是否支持 ask_user
- 是否暴露 SSH
- 是否暴露本地 bash
- 当前可用工具及参数 schema

### 7.5 停止 / 恢复 / 刷新就绪状态

```http
POST /api/v1/instances/{instanceId}/stop
POST /api/v1/instances/{instanceId}/resume
POST /api/v1/instances/{instanceId}/refresh-readiness
```

## 8. 会话与消息接口

### 8.1 推荐方式：一步发消息

```http
POST /api/v1/instances/{instanceId}/messages
```

请求体常用字段：

- `session_id`：可选，继续旧会话
- `title`：可选，新会话标题
- `content`：必填
- `input_type`：可选
- `client_message_id`：可选，客户端关联 id

示例：

```json
{
  "title": "Demo session",
  "content": "Please summarize current project status.",
  "client_message_id": "msg_local_001"
}
```

### 8.2 显式创建会话

```http
POST /api/v1/instances/{instanceId}/sessions
POST /api/v1/instances/{instanceId}/sessions/{sessionId}/messages
```

### 8.3 查会话状态与运行结果

```http
GET /api/v1/instances/{instanceId}/sessions
GET /api/v1/instances/{instanceId}/sessions/{sessionId}
GET /api/v1/instances/{instanceId}/sessions/{sessionId}/messages
GET /api/v1/instances/{instanceId}/runs
GET /api/v1/instances/{instanceId}/runs/{runId}
```

建议重点关注：

- `waiting_for_user`
- `pending_ask`
- `status`
- `assistant_message_id`

## 9. Skill 接口

Skill 是 user 级共享资源。

```http
GET /api/v1/skills
POST /api/v1/skills/search
POST /api/v1/skills/install
POST /api/v1/skills/import
GET /api/v1/skills/{skillName}
DELETE /api/v1/skills/{skillName}
GET /api/v1/skills/{skillName}/export
POST /api/v1/skills/{skillName}/validate
POST /api/v1/skills/{skillName}/improve
POST /api/v1/skills/{skillName}/upload
GET /api/v1/skill-uploads/{submissionId}
GET /api/v1/skill-market/account
```

推荐理解方式：

- `list`：查当前用户已安装 skill
- `search`：查 GitHub / SkillMarket / SkillHub
- `install` / `import`：安装或导入 skill
- `validate` / `improve`：做可移植性检查和修复
- `upload`：发布到 market

## 10. MCP 接口

MCP server 同样是 user 级共享资源。

```http
GET /api/v1/mcp/servers
POST /api/v1/mcp/servers
GET /api/v1/mcp/servers/{serverId}
PATCH /api/v1/mcp/servers/{serverId}
DELETE /api/v1/mcp/servers/{serverId}
POST /api/v1/mcp/servers/{serverId}/start
POST /api/v1/mcp/servers/{serverId}/stop
POST /api/v1/mcp/servers/{serverId}/health-check
GET /api/v1/mcp/servers/{serverId}/tools
```

支持两种类型：

- `remote`：连接远程 MCP
- `local`：启动本地 stdio MCP 进程

客户端一般这样用：

1. `POST /api/v1/mcp/servers` 创建
2. `POST /api/v1/mcp/servers/{serverId}/health-check` 做检查
3. `GET /api/v1/mcp/servers/{serverId}/tools` 拉取工具列表
4. 需要时 `start` / `stop`

## 11. 常见错误与处理建议

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
    "issues": []
  }
}
```

推荐对状态码的处理：

- `400`：请求错或配置不完整
- `401`：鉴权失败
- `404`：资源不存在
- `429`：被限流，登录流程适合重试
- `500`：服务端异常

## 12. AI 客户端集成建议

- 登录后立即调 `GET /api/v1/me`，把本地状态绑定到 `tenant_id + user_id`
- 不要把 config 当成 instance 级配置
- 不要把 skill 和 MCP 当成 instance 私有资源
- 优先使用 `POST /instances/{id}/messages` 而不是先手动创 session
- 用 `GET /instances/{id}/capabilities` 动态决定 UI
- 把 `config_validation.issues` 直接展示给用户
- 不要假定本地 bash 一定可用

## 13. 接口总表

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

用户接口：

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
- `GET /api/v1/instances/{instanceId}/capabilities`
- `POST /api/v1/instances/{instanceId}/stop`
- `POST /api/v1/instances/{instanceId}/resume`
- `POST /api/v1/instances/{instanceId}/refresh-readiness`
- `GET /api/v1/instances/{instanceId}/bootstrap`
- `POST /api/v1/instances/{instanceId}/messages`
- `GET /api/v1/instances/{instanceId}/sessions`
- `POST /api/v1/instances/{instanceId}/sessions`
- `GET /api/v1/instances/{instanceId}/sessions/{sessionId}`
- `GET /api/v1/instances/{instanceId}/sessions/{sessionId}/messages`
- `POST /api/v1/instances/{instanceId}/sessions/{sessionId}/messages`
- `GET /api/v1/instances/{instanceId}/runs`
- `GET /api/v1/instances/{instanceId}/runs/{runId}`

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
