# MaClawSrv API Manual

This document is a practical integration manual for AI coding tools, automation agents, desktop clients, and service gateways that need to call `MaClawSrv` directly.

## 1. What MaClawSrv provides

`MaClawSrv` exposes Maclaw as a multi-tenant, multi-user REST service.

Key characteristics:

- Data isolation is `tenant -> user`.
- One user owns one shared Maclaw data root.
- Multiple logical instances can run under the same user at the same time.
- All instances of the same user reuse the same config, memory, skill set, and long-term data.
- Core runtime behavior is reused from `corelib/agentservice` and shared agent infrastructure.
- `MaClawSrv` is a pure agent service. It does not expose coding-session orchestration APIs.
- Skill management and MCP management are available through REST.

## 2. Base URL and transport

Default listen address:

```text
http://127.0.0.1:18080
```

Health endpoint:

```http
GET /health
```

Response:

```json
{
  "status": "ok"
}
```

Transport rules:

- By default, plain HTTP is only allowed on loopback.
- For remote deployment, configure TLS with `MACLAW_TLS_CERT_FILE` and `MACLAW_TLS_KEY_FILE`.
- Do not send admin secrets, API secrets, or bearer tokens over insecure remote HTTP.

## 3. Server environment variables

Required security variables:

- `MACLAW_ADMIN_SECRET`: admin control-plane secret, at least 24 chars.
- `MACLAW_TOKEN_SECRET`: bearer token signing secret, at least 32 chars.

Common runtime variables:

- `MACLAW_HTTP_ADDR`: listen address. Default `:18080`.
- `MACLAW_DATA_ROOT`: root data directory. Default `~/.maclaw_srv`.
- `MACLAW_TLS_CERT_FILE`: TLS cert path.
- `MACLAW_TLS_KEY_FILE`: TLS key path.
- `MACLAW_ALLOW_INSECURE_HTTP`: allows non-loopback HTTP if set to `true`.
- `MACLAW_CREDENTIAL_PEPPER`: optional secret pepper for credential hashing.

Host-local bash related variables:

- `MACLAW_ENABLE_LOCAL_BASH`
- `MACLAW_LOCAL_BASH_TRUSTED_SINGLE_USER`
- `MACLAW_LOCAL_BASH_TENANT_ID`
- `MACLAW_LOCAL_BASH_USER_ID`

Important:

- Local bash is security-sensitive and should stay disabled for multi-tenant deployments unless an external OS/container sandbox exists.

## 4. Authentication model

There are two authentication layers.

### 4.1 Admin APIs

Admin APIs use a static header:

```http
X-MaClaw-Admin-Secret: <admin-secret>
```

This is only for tenant/user/credential provisioning and audit-style control-plane calls.

### 4.2 User APIs

User APIs use a bearer token.

Flow:

1. Admin creates tenant, user, and credential.
2. Client exchanges `api_key + api_secret` for a bearer token.
3. Client calls user-scoped APIs with `Authorization: Bearer <token>`.

Token endpoint:

```http
POST /api/v1/auth/token
Content-Type: application/json
```

Request:

```json
{
  "api_key": "ak_xxx",
  "api_secret": "secret_xxx"
}
```

Response:

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

Current-user endpoint:

```http
GET /api/v1/me
Authorization: Bearer <token>
```

## 5. Recommended integration flow for AI tools

For most AI clients, use this sequence:

1. `POST /api/v1/auth/token`
2. `GET /api/v1/me`
3. `GET /api/v1/config/schema`
4. `GET /api/v1/config`
5. `POST /api/v1/config/validate`
6. `PUT /api/v1/config` if missing fields must be filled
7. `POST /api/v1/config/test` if the client wants a real connectivity check
8. `POST /api/v1/instances`
9. `POST /api/v1/instances/{instanceId}/messages`
10. Poll `GET /api/v1/instances/{instanceId}/runs/{runId}` and/or read session messages

This makes the client robust against incomplete user config.

## 6. Common headers

User-scoped request:

```http
Authorization: Bearer <token>
Accept: application/json
Content-Type: application/json
```

Admin-scoped request:

```http
X-MaClaw-Admin-Secret: <admin-secret>
Accept: application/json
Content-Type: application/json
```

## 7. Error model

Typical errors are JSON:

```json
{
  "error": "..."
}
```

Config-related start failures may also return extra detail:

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

Recommended client behavior:

- Treat `400` as request/config/user-fixable.
- Treat `401` as auth failure.
- Treat `404` as missing tenant/user/instance/session/run/skill/MCP object.
- Treat `429` as retryable throttling on login/token flows.
- Treat `500` as server-side failure.

## 8. Pagination

Several list endpoints support cursor pagination.

Query params:

- `limit`: default `100`, max `500`
- `before`: RFC3339 or RFC3339Nano timestamp

Typical response shape:

```json
{
  "items": [],
  "limit": 100,
  "has_more": true,
  "next_before": "2026-04-23T18:00:00Z"
}
```

Supported on:

- `GET /api/v1/instances`
- `GET /api/v1/instances/{instanceId}/sessions`
- `GET /api/v1/instances/{instanceId}/sessions/{sessionId}/messages`
- `GET /api/v1/instances/{instanceId}/runs`
- `GET /api/v1/admin/audit-events`

## 9. Admin API

These APIs are typically used by a control plane, installer, or bootstrap tool.

### 9.1 Create tenant

```http
POST /api/v1/admin/tenants
X-MaClaw-Admin-Secret: <admin-secret>
```

Request:

```json
{
  "name": "Acme"
}
```

### 9.2 List tenants

```http
GET /api/v1/admin/tenants
```

### 9.3 Get/update tenant

```http
GET /api/v1/admin/tenants/{tenantId}
PATCH /api/v1/admin/tenants/{tenantId}
```

Update request fields:

- `name`
- `status`: `active` or `disabled`

Example:

```json
{
  "status": "disabled"
}
```

### 9.4 Create user

```http
POST /api/v1/admin/tenants/{tenantId}/users
```

Request:

```json
{
  "name": "Alice",
  "email": "alice@example.com"
}
```

### 9.5 List/get/update users

```http
GET /api/v1/admin/tenants/{tenantId}/users
GET /api/v1/admin/tenants/{tenantId}/users/{userId}
PATCH /api/v1/admin/tenants/{tenantId}/users/{userId}
```

Update fields:

- `name`
- `email`
- `status`: `active` or `disabled`

### 9.6 Create credential

```http
POST /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials
```

Request:

```json
{
  "name": "default-client",
  "api_key": "ak_demo_client",
  "api_secret": "super-secret"
}
```

Notes:

- `api_secret` is only shown at creation time on the client side; store it immediately.
- Credentials can be revoked later.
- Bearer tokens are bound to the issuing credential.

### 9.7 List/revoke credentials

```http
GET /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials
DELETE /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials/{credentialId}
```

### 9.8 Audit events

```http
GET /api/v1/admin/audit-events
```

Optional filters:

- `tenant_id`
- `user_id`
- `action`
- `resource_type`
- `limit`
- `before`

## 10. User config API

Each Maclaw user has one shared config used by all instances under that user.

### 10.1 Get schema

```http
GET /api/v1/config/schema
```

Use this to render a settings form dynamically if desired.

### 10.2 Get current config

```http
GET /api/v1/config
```

Response shape:

```json
{
  "tenant_id": "tenant_xxx",
  "user_id": "user_xxx",
  "app_config": {
    "maclaw_llm_url": "https://...",
    "maclaw_llm_model": "gpt-5.4"
  },
  "updated_at": "2026-04-23T18:00:00Z"
}
```

Sensitive fields are sanitized when returned.

### 10.3 Update config

```http
PUT /api/v1/config
```

Two request body formats are accepted by validate/test, and raw config is accepted by update:

Format A:

```json
{
  "maclaw_llm_url": "https://api.openai.com/v1",
  "maclaw_llm_key": "sk-...",
  "maclaw_llm_model": "gpt-5.4"
}
```

Format B:

```json
{
  "app_config": {
    "maclaw_llm_url": "https://api.openai.com/v1",
    "maclaw_llm_key": "sk-...",
    "maclaw_llm_model": "gpt-5.4"
  }
}
```

### 10.4 Validate config without saving

```http
POST /api/v1/config/validate
```

Behavior:

- Empty body: validate saved config
- Raw `AppConfig`: validate candidate config
- `{"app_config": {...}}`: validate candidate config

Response:

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

### 10.5 Test config against real provider

```http
POST /api/v1/config/test
```

Response example:

```json
{
  "success": true,
  "message": "ok",
  "latency_ms": 812,
  "endpoint": "https://api.openai.com/v1",
  "model": "gpt-5.4"
}
```

## 11. Instance API

An instance is a logical runtime entrypoint for a user. It does not own separate memory. It reuses the shared user-level Maclaw data.

### 11.1 List instances

```http
GET /api/v1/instances
```

### 11.2 Create instance

```http
POST /api/v1/instances
```

Request:

```json
{
  "name": "primary-agent",
  "description": "default assistant entrypoint",
  "metadata": {
    "channel": "wails-demo"
  }
}
```

If config is incomplete, this may fail with `config_validation` detail.

### 11.3 Get instance

```http
GET /api/v1/instances/{instanceId}
```

Important fields:

- `status`
- `ready`
- `ready_reason`
- `readiness`
- `config_validation`
- `data_dir`
- `runtime_dir`
- `workspace_dir`

### 11.4 Get instance capabilities

```http
GET /api/v1/instances/{instanceId}/capabilities
```

Use this to discover:

- executor type
- whether session mode is supported
- whether structured ask-user is supported
- whether SSH is exposed
- whether local bash is exposed
- visible tool list and parameter schemas

This endpoint is the recommended way for AI tools to adapt their UI and behavior to the deployment.

### 11.5 Stop/resume/refresh readiness

```http
POST /api/v1/instances/{instanceId}/stop
POST /api/v1/instances/{instanceId}/resume
POST /api/v1/instances/{instanceId}/refresh-readiness
```

### 11.6 Bootstrap descriptor

```http
GET /api/v1/instances/{instanceId}/bootstrap
```

This returns paths and metadata needed by a future runner or external supervisor.

## 12. Session and messaging API

There are two ways to send messages.

### 12.1 Recommended one-step send

```http
POST /api/v1/instances/{instanceId}/messages
```

Request fields:

- `session_id`: optional, continue existing session
- `agent_id`: optional, create session with a specific agent ID
- `title`: optional, used when creating a new session
- `content`: required
- `input_type`: optional
- `metadata`: optional message metadata
- `session_metadata`: optional metadata for new session
- `client_session_key`: optional client-side idempotency/helper key
- `client_message_id`: optional client-side message correlation id

Example request:

```json
{
  "title": "Demo session",
  "content": "Please summarize current project status and next steps.",
  "client_message_id": "msg_local_001"
}
```

Example response shape:

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

### 12.2 Explicit session creation + post message

Create session:

```http
POST /api/v1/instances/{instanceId}/sessions
```

Request:

```json
{
  "agent_id": "default",
  "title": "Planning",
  "metadata": {
    "source": "external-tool"
  }
}
```

Post into a session:

```http
POST /api/v1/instances/{instanceId}/sessions/{sessionId}/messages
```

Request:

```json
{
  "content": "Continue from the previous result.",
  "input_type": "text"
}
```

### 12.3 Read conversation state

```http
GET /api/v1/instances/{instanceId}/sessions
GET /api/v1/instances/{instanceId}/sessions/{sessionId}
GET /api/v1/instances/{instanceId}/sessions/{sessionId}/messages
GET /api/v1/instances/{instanceId}/runs
GET /api/v1/instances/{instanceId}/runs/{runId}
```

Useful patterns:

- Use `GET session` to see whether `waiting_for_user` is true.
- Use `pending_ask` to render a structured follow-up question in your UI.
- Use `GET runs/{runId}` to poll background completion.
- Use `GET messages` as the source of truth for chat transcript rendering.

## 13. Usage summary API

```http
GET /api/v1/usage/summary
```

This is useful for dashboards and account overview pages.

Example fields:

- `instances`
- `ready_instances`
- `stopped_instances`
- `sessions`
- `messages`
- `runs`
- `runs_by_status`
- `last_activity_at`

## 14. Skill API

Skills are user-scoped. All instances of the same user share the same installed skills.

### 14.1 List installed skills

```http
GET /api/v1/skills
```

### 14.2 Search remote skill sources

```http
POST /api/v1/skills/search
```

Request:

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

### 14.3 Install a skill from remote source or packaged input

```http
POST /api/v1/skills/install
```

Typical request variants:

GitHub repo/raw install:

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

Skill hub install:

```json
{
  "source": "skillhub",
  "skill_id": "skill_xxx",
  "skill_hub_url": "https://skillhub.example.com"
}
```

Zip payload install:

```json
{
  "source": "zip",
  "zip_base64": "<base64-zip>",
  "overwrite": true
}
```

### 14.4 Import zipped skill archive directly

```http
POST /api/v1/skills/import
```

Request:

```json
{
  "zip_base64": "<base64-zip>",
  "overwrite": true,
  "archive_name": "demo-skill.zip"
}
```

### 14.5 Get/delete/export skill

```http
GET /api/v1/skills/{skillName}
DELETE /api/v1/skills/{skillName}
GET /api/v1/skills/{skillName}/export
```

Export returns a base64 archive payload for backup or transfer.

### 14.6 Validate and improve skill

```http
POST /api/v1/skills/{skillName}/validate
POST /api/v1/skills/{skillName}/improve
```

Improve request:

```json
{
  "auto_fix": true
}
```

### 14.7 Upload skill to market

```http
POST /api/v1/skills/{skillName}/upload
```

Request:

```json
{
  "skill_market_url": "https://market.example.com",
  "email": "author@example.com"
}
```

Check async upload status:

```http
GET /api/v1/skill-uploads/{submissionId}
```

Get market account profile:

```http
GET /api/v1/skill-market/account?email=author@example.com&base_url=https://market.example.com
```

## 15. MCP API

MCP servers are also user-scoped and shared by all instances of the same user.

Two kinds exist:

- `remote`: MaClawSrv connects to an existing remote MCP endpoint
- `local`: MaClawSrv launches and manages a local stdio MCP server process

### 15.1 List MCP servers

```http
GET /api/v1/mcp/servers
```

### 15.2 Create MCP server

```http
POST /api/v1/mcp/servers
```

Remote example:

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

Local example:

```json
{
  "kind": "local",
  "name": "Filesystem MCP",
  "command": "npx",
  "args": [
    "-y",
    "@modelcontextprotocol/server-filesystem",
    "D:\workprj\aicoder"
  ],
  "env": {
    "NODE_ENV": "production"
  },
  "auto_start": true,
  "disabled": false
}
```

Rules:

- Remote requires `endpoint_url`.
- Local requires `command`.
- `kind` must be `remote` or `local`.
- Remote `auth_type` is normalized by the server.

### 15.3 Get/update/delete MCP server

```http
GET /api/v1/mcp/servers/{serverId}
PATCH /api/v1/mcp/servers/{serverId}
DELETE /api/v1/mcp/servers/{serverId}
```

Patch request may include:

- `name`
- `endpoint_url`
- `auth_type`
- `auth_secret`
- `headers`
- `command`
- `args`
- `env`
- `disabled`
- `auto_start`

### 15.4 Start/stop/check tools

```http
POST /api/v1/mcp/servers/{serverId}/start
POST /api/v1/mcp/servers/{serverId}/stop
POST /api/v1/mcp/servers/{serverId}/health-check
GET /api/v1/mcp/servers/{serverId}/tools
```

Client guidance:

- For local MCP, call `start` if `auto_start` is false.
- Call `health-check` after create/update for remote MCP.
- Call `tools` to populate tool picker or tool-catalog UI.

Returned MCP view fields commonly include:

- `id`
- `kind`
- `name`
- `endpoint_url`
- `auth_type`
- `header_names`
- `command`
- `args`
- `env_keys`
- `disabled`
- `auto_start`
- `running`
- `health_status`
- `fail_count`
- `last_check_at`
- `tools`

## 16. End-to-end quickstart example

### 16.1 Admin bootstrap

```bash
curl -s http://127.0.0.1:18080/api/v1/admin/tenants   -H 'X-MaClaw-Admin-Secret: your-admin-secret'   -H 'Content-Type: application/json'   -d '{"name":"Demo Tenant"}'
```

```bash
curl -s http://127.0.0.1:18080/api/v1/admin/tenants/tenant_xxx/users   -H 'X-MaClaw-Admin-Secret: your-admin-secret'   -H 'Content-Type: application/json'   -d '{"name":"Demo User","email":"demo@example.com"}'
```

```bash
curl -s http://127.0.0.1:18080/api/v1/admin/tenants/tenant_xxx/users/user_xxx/credentials   -H 'X-MaClaw-Admin-Secret: your-admin-secret'   -H 'Content-Type: application/json'   -d '{"name":"demo-client","api_key":"ak_demo","api_secret":"demo-secret"}'
```

### 16.2 Login

```bash
curl -s http://127.0.0.1:18080/api/v1/auth/token   -H 'Content-Type: application/json'   -d '{"api_key":"ak_demo","api_secret":"demo-secret"}'
```

### 16.3 Save config

```bash
curl -s http://127.0.0.1:18080/api/v1/config   -X PUT   -H 'Authorization: Bearer <token>'   -H 'Content-Type: application/json'   -d '{"maclaw_llm_url":"https://api.openai.com/v1","maclaw_llm_key":"sk-xxx","maclaw_llm_model":"gpt-5.4"}'
```

### 16.4 Create instance

```bash
curl -s http://127.0.0.1:18080/api/v1/instances   -H 'Authorization: Bearer <token>'   -H 'Content-Type: application/json'   -d '{"name":"primary-agent","description":"demo"}'
```

### 16.5 Send message

```bash
curl -s http://127.0.0.1:18080/api/v1/instances/inst_xxx/messages   -H 'Authorization: Bearer <token>'   -H 'Content-Type: application/json'   -d '{"title":"Demo","content":"Please introduce your capabilities."}'
```

## 17. Integration recommendations for AI clients

- Always call `GET /api/v1/me` after login and bind local client state to `tenant_id + user_id`.
- Treat config as user-shared state, not instance-local state.
- Treat skills as user-shared state.
- Treat MCP servers as user-shared state.
- Prefer `POST /instances/{id}/messages` unless you explicitly need separate session creation.
- Use `GET /instances/{id}/capabilities` to adapt UI based on tool exposure and policy.
- Use `waiting_for_user` and `pending_ask` to support structured follow-up interactions.
- Surface `config_validation.issues` directly to the user instead of hiding them.
- Avoid assuming host-local bash is available.
- Avoid assuming programming-tool or coding-session APIs exist in `MaClawSrv`.

## 18. Minimal client checklist

A production-grade caller should support at least:

- admin bootstrap flow or external provisioning flow
- login and token refresh/re-login
- config load, validate, update, test
- instance create/list/get
- message send
- session and run polling
- capability inspection
- skill list/search/install/import/export/upload when relevant
- MCP create/update/start/check/tools when relevant
- structured error rendering

## 19. API inventory

Admin:

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

Auth and user runtime:

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

Skills:

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

MCP:

- `GET /api/v1/mcp/servers`
- `POST /api/v1/mcp/servers`
- `GET /api/v1/mcp/servers/{serverId}`
- `PATCH /api/v1/mcp/servers/{serverId}`
- `DELETE /api/v1/mcp/servers/{serverId}`
- `POST /api/v1/mcp/servers/{serverId}/start`
- `POST /api/v1/mcp/servers/{serverId}/stop`
- `POST /api/v1/mcp/servers/{serverId}/health-check`
- `GET /api/v1/mcp/servers/{serverId}/tools`
