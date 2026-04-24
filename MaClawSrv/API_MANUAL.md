# MaClawSrv API Manual

This document is a practical integration manual for AI coding tools, automation agents, desktop clients, control planes, and external programs that need to call `MaClawSrv` directly. The goal is to let integrators work efficiently without repeatedly reading the source, while making each endpoint's purpose, inputs, outputs, and usage patterns explicit.

## 1. What MaClawSrv provides

`MaClawSrv` exposes Maclaw as a multi-tenant, multi-user REST service.

Key characteristics:

- Data isolation is `tenant -> user`.
- Multiple logical instances can run under the same user at the same time.
- All instances of the same user reuse the same config, memory, skill set, and long-term data.
- Core runtime behavior is reused from `corelib/agentservice` and shared agent infrastructure.
- `MaClawSrv` is a pure agent service. It does not expose coding-session orchestration APIs.
- Skill management and MCP management are also available through REST.

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

Transport guidance:

- By default, plain HTTP should only be used on loopback.
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
- `MACLAW_ALLOW_INSECURE_HTTP`: allows non-loopback HTTP when set to `true`.
- `MACLAW_CREDENTIAL_PEPPER`: optional extra pepper for credential hashing.

Host-local bash related variables:

- `MACLAW_ENABLE_LOCAL_BASH`
- `MACLAW_LOCAL_BASH_TRUSTED_SINGLE_USER`
- `MACLAW_LOCAL_BASH_TENANT_ID`
- `MACLAW_LOCAL_BASH_USER_ID`

Important:

- Local bash is security-sensitive and should stay disabled in multi-tenant deployments unless an outer OS/container sandbox exists.

## 4. Authentication model

`MaClawSrv` has two authentication layers: admin APIs and user runtime APIs.

### 4.1 Admin APIs

Admin APIs use a static header:

```http
X-MaClaw-Admin-Secret: <admin-secret>
```

These are used for tenants, users, credentials, audit events, and other control-plane operations.

### 4.2 User APIs

User APIs use a bearer token.

Standard flow:

1. The admin side creates a tenant, user, and credential.
2. The client exchanges `api_key + api_secret` for a token.
3. The client calls user-scoped APIs with `Authorization: Bearer <token>`.

Token endpoint:

```http
POST /api/v1/auth/token
Content-Type: application/json
```

Request body:

```json
{
  "api_key": "ak_xxx",
  "api_secret": "secret_xxx"
}
```

Field notes:

- `api_key`: public credential identifier.
- `api_secret`: secret value stored only by the client; the server does not return it later in plaintext.

Response example:

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

Response fields:

- `access_token`: bearer token for subsequent requests.
- `token_type`: always `Bearer`.
- `expires_at`: expiration timestamp in RFC3339 format.
- `principal.tenant_id`: current tenant.
- `principal.user_id`: current user.

Current-user endpoint:

```http
GET /api/v1/me
Authorization: Bearer <token>
```

This returns a `User` object. Typical fields include:

- `id`
- `tenant_id`
- `name`
- `email`
- `status`
- `created_at`
- `updated_at`

## 5. Recommended integration flow for AI tools

For AI tools and desktop clients, use this sequence:

1. `POST /api/v1/auth/token`
2. `GET /api/v1/me`
3. `GET /api/v1/config/schema`
4. `GET /api/v1/config`
5. `POST /api/v1/config/validate`
6. `PUT /api/v1/config` if missing fields must be filled
7. `POST /api/v1/config/test` if the client wants a real connectivity check
8. `POST /api/v1/instances`
9. `GET /api/v1/instances/{instanceId}/capabilities`
10. `POST /api/v1/instances/{instanceId}/messages`
11. Poll `GET /api/v1/instances/{instanceId}/runs/{runId}` or read message history

This separates configuration problems from runtime failures and makes it easier to surface setup issues in UI.

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

Config-related failures may include extra details:

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

Recommended handling:

- `400`: invalid request fields, incomplete config, or unmet business preconditions.
- `401`: authentication failure.
- `404`: missing tenant, user, instance, session, run, skill, or MCP server.
- `409`: state conflict, for example a cancelled run or writing to an archived session.
- `429`: throttled token/login flow; retry is appropriate.
- `502`: downstream executor or external dependency failure; the response may still include a created `run`.
- `500`: server-side failure.

## 8. Pagination

Several list endpoints support cursor pagination.

Query parameters:

- `limit`: default `100`, max `500`.
- `before`: RFC3339 or RFC3339Nano timestamp.

Typical response shape:

```json
{
  "items": [],
  "limit": 100,
  "has_more": true,
  "next_before": "2026-04-23T18:00:00Z"
}
```

Field meanings:

- `items`: current page items.
- `limit`: effective page size.
- `has_more`: whether another page exists.
- `next_before`: the `before` value to use for the next page.

Supported on:

- `GET /api/v1/instances`
- `GET /api/v1/instances/{instanceId}/sessions`
- `GET /api/v1/instances/{instanceId}/sessions/{sessionId}/messages`
- `GET /api/v1/instances/{instanceId}/runs`
- `GET /api/v1/admin/audit-events`

## 9. Admin API

These APIs are typically used by control planes, installers, provisioning tools, or admin backends.

### 9.1 Create tenant

```http
POST /api/v1/admin/tenants
```

Request body:

```json
{
  "name": "Acme"
}
```

Field notes:

- `name`: tenant display name.

Response: `Tenant` object.

Main fields:

- `id`
- `name`
- `status`: `active` or `disabled`
- `created_at`
- `updated_at`

### 9.2 List tenants

```http
GET /api/v1/admin/tenants
```

Response:

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

### 9.3 Get or update a tenant

```http
GET /api/v1/admin/tenants/{tenantId}
PATCH /api/v1/admin/tenants/{tenantId}
```

Patch fields may include:

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

Request body:

```json
{
  "name": "Alice",
  "email": "alice@example.com"
}
```

Field notes:

- `name`: user name.
- `email`: optional contact or login mapping information.

Response: `User` object.

Main fields:

- `id`
- `tenant_id`
- `name`
- `email`
- `status`
- `created_at`
- `updated_at`

### 9.5 List, get, or update users

```http
GET /api/v1/admin/tenants/{tenantId}/users
GET /api/v1/admin/tenants/{tenantId}/users/{userId}
PATCH /api/v1/admin/tenants/{tenantId}/users/{userId}
```

Patch fields may include:

- `name`
- `email`
- `status`: `active` or `disabled`

### 9.6 Create credential

```http
POST /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials
```

Request body:

```json
{
  "name": "default-client",
  "api_key": "ak_demo_client",
  "api_secret": "super-secret"
}
```

Field notes:

- `name`: credential label for admin-side identification.
- `api_key`: public identifier.
- `api_secret`: secret value stored by the client.

Response: `Credential` object.

Main fields:

- `id`
- `tenant_id`
- `user_id`
- `name`
- `api_key`
- `api_key_prefix`
- `status`
- `created_at`
- `updated_at`

Notes:

- `api_secret` is only known to the client at creation time.
- Bearer tokens are associated with the credential that issued them.

### 9.7 List or revoke credentials

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

Response items are `AuditEvent` objects. Main fields include:

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

## 10. User config API

Each user has one shared config used by all instances under that user.

### 10.1 Get config schema

```http
GET /api/v1/config/schema
```

Response:

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

Field meanings:

- `key`: config field name.
- `title`: display label.
- `description`: field explanation.
- `required`: whether the field is required.
- `secret`: whether the field is sensitive.
- `type`: field type, for example `string`.
- `example`: suggested example value.

Use this endpoint to build dynamic settings UI instead of hardcoding config fields.

### 10.2 Get current config

```http
GET /api/v1/config
```

Response example:

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

Field meanings:

- `tenant_id`, `user_id`: owner of the config.
- `app_config`: effective Maclaw config.
- `updated_at`: last update time.

Notes:

- Sensitive fields are sanitized in responses.
- If no config has been saved yet, the server returns an empty `app_config`.

### 10.3 Update config

```http
PUT /api/v1/config
```

Request body accepts raw `AppConfig` JSON:

```json
{
  "maclaw_llm_url": "https://api.openai.com/v1",
  "maclaw_llm_key": "sk-...",
  "maclaw_llm_model": "gpt-5.4"
}
```

Notes:

- `PUT /config` accepts the raw config object, not an `app_config` wrapper.
- Existing secret fields are preserved/merged server-side when appropriate.

Response: updated `UserConfig`.

### 10.4 Validate config without saving

```http
POST /api/v1/config/validate
```

Supported request shapes:

- Empty body: validate the saved config.
- Raw `AppConfig`: validate a candidate config.
- `{"app_config": {...}}`: also validates a candidate config.

Response example:

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

Field meanings:

- `valid`: whether validation passed.
- `issues[].key`: invalid config key.
- `issues[].message`: user-facing explanation.

### 10.5 Test config against a real provider

```http
POST /api/v1/config/test
```

Request body format matches `validate`.

Response example:

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

Field meanings:

- `success`: whether connectivity succeeded.
- `message`: success message.
- `error`: failure message.
- `latency_ms`: request latency.
- `endpoint`: tested provider endpoint.
- `provider_name`: detected provider.
- `model`: tested model.
- `protocol`, `wire_api`: lower-level protocol identification.
- `validation`: optional validation result if config issues also exist.

## 11. Instance API

An instance is a logical runtime entrypoint. It does not own separate long-term memory or config; it reuses shared user-level state.

### 11.1 List instances

```http
GET /api/v1/instances
```

Supports pagination via `limit` and `before`.

Response items are `Instance` objects. Main fields include:

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

Important semantics:

- `status`: lifecycle state, typically `ready` or `stopped`.
- `ready`: whether the instance is currently usable.
- `ready_reason`: direct reason when not usable.
- `readiness.ready`: structured readiness state.
- `readiness.config_valid`: whether config passed validation.
- `readiness.has_llm_config`: whether minimum LLM config exists.
- `config_validation.issues`: user-displayable config errors.

### 11.2 Create instance

```http
POST /api/v1/instances
```

Request body:

```json
{
  "name": "primary-agent",
  "description": "default assistant entrypoint",
  "metadata": {
    "channel": "wails-demo"
  }
}
```

Field notes:

- `name`: instance name.
- `description`: instance description.
- `metadata`: application-defined labels preserved as-is.

Response: created `Instance`.

If config is incomplete, the endpoint may return `400` with `config_validation` details.

### 11.3 Get a single instance

```http
GET /api/v1/instances/{instanceId}
```

Fields typically worth surfacing prominently:

- `status`
- `ready`
- `ready_reason`
- `readiness`
- `config_validation`
- `data_dir`
- `runtime_dir`
- `workspace_dir`

### 11.4 Update instance

```http
PATCH /api/v1/instances/{instanceId}
```

Patch fields may include:

- `name`
- `description`
- `metadata`

This updates mutable instance metadata only; it does not change lifecycle state.

### 11.5 Delete instance

```http
DELETE /api/v1/instances/{instanceId}
```

Response:

```json
{
  "status": "deleted"
}
```

### 11.6 Get instance capabilities

```http
GET /api/v1/instances/{instanceId}/capabilities
```

This is a key endpoint for AI clients and should usually be called immediately after selecting an instance.

Main fields:

- `executor`
- `supports_sessions`
- `supports_ask_user`
- `supports_ssh`
- `supports_local_bash`
- `tools`
- `metadata`

`tools[]` fields:

- `name`
- `description`
- `enabled`
- `disabled_reason`
- `parameters`

`parameters` can be used directly to drive UI forms for tool invocation or capability display.

### 11.7 Stop, resume, or refresh readiness

```http
POST /api/v1/instances/{instanceId}/stop
POST /api/v1/instances/{instanceId}/resume
POST /api/v1/instances/{instanceId}/refresh-readiness
```

Response: latest `Instance`.

### 11.8 Get bootstrap descriptor

```http
GET /api/v1/instances/{instanceId}/bootstrap
```

Returns `InstanceBootstrap` with fields such as:

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

## 12. Session, message, and run API

### 12.1 Recommended one-step send

```http
POST /api/v1/instances/{instanceId}/messages
```

Request fields:

- `session_id`: optional, continue an existing session.
- `agent_id`: optional, choose an agent ID when creating a session.
- `title`: optional, title for a new session.
- `content`: required user input.
- `input_type`: optional, for example `text`.
- `metadata`: optional message metadata.
- `session_metadata`: optional metadata for a new session.
- `client_session_key`: optional client-side idempotency/helper key.
- `client_message_id`: optional client-side message correlation ID.

Example:

```json
{
  "title": "Demo session",
  "content": "Please summarize current project status and next steps.",
  "client_message_id": "msg_local_001"
}
```

Success response example:

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

### 12.2 Explicit session creation

```http
POST /api/v1/instances/{instanceId}/sessions
```

Request body:

```json
{
  "agent_id": "default",
  "title": "Planning",
  "metadata": {
    "source": "external-tool"
  }
}
```

### 12.3 List sessions

```http
GET /api/v1/instances/{instanceId}/sessions
```

Supported query parameters:

- `include_archived`
- `limit`
- `before`

Response items are `Session` objects. Main fields include:

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

Important semantics:

- `waiting_for_user`: whether the agent is waiting for a reply.
- `pending_ask.question`: prompt to present to the user.
- `pending_ask.input_type`: answer type.
- `pending_ask.options`: explicit answer choices when available.

### 12.4 Get, update, or delete a session

```http
GET /api/v1/instances/{instanceId}/sessions/{sessionId}
PATCH /api/v1/instances/{instanceId}/sessions/{sessionId}
DELETE /api/v1/instances/{instanceId}/sessions/{sessionId}
```

Patch fields may include:

- `title`
- `metadata`

Deleting a session typically also removes associated messages and runs, so clients should usually confirm before calling it.

### 12.5 Archive or restore a session

```http
POST /api/v1/instances/{instanceId}/sessions/{sessionId}/archive
POST /api/v1/instances/{instanceId}/sessions/{sessionId}/restore
```

Behavior notes:

- Archived sessions are hidden from default listings.
- Posting into an archived session usually returns `409`.
- Use `include_archived=true` when showing historical conversations.

### 12.6 List messages

```http
GET /api/v1/instances/{instanceId}/sessions/{sessionId}/messages
```

Supports pagination via `limit` and `before`.

Response items are `Message` objects. Main fields include:

- `id`
- `session_id`
- `tenant_id`
- `user_id`
- `instance_id`
- `role`: `user`, `assistant`, or `system`
- `input_type`
- `output_type`
- `content`
- `metadata`
- `created_at`

For transcript rendering, this endpoint should be treated as the source of truth.

### 12.7 Post into a specific session

```http
POST /api/v1/instances/{instanceId}/sessions/{sessionId}/messages
```

Request body:

```json
{
  "content": "Continue from the previous result.",
  "input_type": "text",
  "metadata": {
    "source": "external-tool"
  }
}
```

Success response:

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

### 12.8 List runs

```http
GET /api/v1/instances/{instanceId}/runs
```

Supported query parameters:

- `status`: `running`, `succeeded`, `failed`, `cancelled`
- `session_id`
- `limit`
- `before`

Response items are `Run` objects. Main fields include:

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

### 12.9 Get a single run

```http
GET /api/v1/instances/{instanceId}/runs/{runId}
```

This is the simplest polling endpoint for run status. When `assistant_message_id` appears, clients can fetch the message list for the full assistant content.

### 12.10 Subscribe to run events

```http
GET /api/v1/instances/{instanceId}/runs/{runId}/events
```

Content type: `text/event-stream`.

Event types:

- `snapshot`
- `done`
- `error`

Payload example:

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

Use SSE when the client wants more real-time updates than polling.

### 12.11 Cancel a run

```http
POST /api/v1/instances/{instanceId}/runs/{runId}/cancel
```

Response: cancelled `Run` object. The run `status` should become `cancelled`.

## 13. Usage summary API

```http
GET /api/v1/usage/summary
```

Useful for dashboards and account-overview pages.

Response fields:

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

## 14. Skill API

Skills are user-scoped shared resources. All instances of the same user share the same installed skills.

### 14.1 List installed skills

```http
GET /api/v1/skills
```

### 14.2 Search remote skill sources

```http
POST /api/v1/skills/search
```

Request body:

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

### 14.3 Install a skill

```http
POST /api/v1/skills/install
```

GitHub example:

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

SkillHub example:

```json
{
  "source": "skillhub",
  "skill_id": "skill_xxx",
  "skill_hub_url": "https://skillhub.example.com"
}
```

Zip example:

```json
{
  "source": "zip",
  "zip_base64": "<base64-zip>",
  "overwrite": true
}
```

### 14.4 Import a skill archive directly

```http
POST /api/v1/skills/import
```

Request body:

```json
{
  "zip_base64": "<base64-zip>",
  "overwrite": true,
  "archive_name": "demo-skill.zip"
}
```

### 14.5 Get, delete, or export a skill

```http
GET /api/v1/skills/{skillName}
DELETE /api/v1/skills/{skillName}
GET /api/v1/skills/{skillName}/export
```

### 14.6 Validate or improve a skill

```http
POST /api/v1/skills/{skillName}/validate
POST /api/v1/skills/{skillName}/improve
```

`improve` request body:

```json
{
  "auto_fix": true
}
```

### 14.7 Upload a skill to market

```http
POST /api/v1/skills/{skillName}/upload
```

Request body:

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

MCP servers are also user-scoped shared resources. All instances of the same user share them.

Two kinds exist:

- `remote`
- `local`

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
    "D:\\workprj\\aicoder"
  ],
  "env": {
    "NODE_ENV": "production"
  },
  "auto_start": true,
  "disabled": false
}
```

### 15.3 Get, update, or delete MCP server

```http
GET /api/v1/mcp/servers/{serverId}
PATCH /api/v1/mcp/servers/{serverId}
DELETE /api/v1/mcp/servers/{serverId}
```

### 15.4 Start, stop, health-check, or list tools

```http
POST /api/v1/mcp/servers/{serverId}/start
POST /api/v1/mcp/servers/{serverId}/stop
POST /api/v1/mcp/servers/{serverId}/health-check
GET /api/v1/mcp/servers/{serverId}/tools
```

## 16. End-to-end quickstart

### 16.1 Admin bootstrap

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

### 16.2 Login

```bash
curl -s http://127.0.0.1:18080/api/v1/auth/token \
  -H 'Content-Type: application/json' \
  -d '{"api_key":"ak_demo","api_secret":"demo-secret"}'
```

### 16.3 Save config

```bash
curl -s http://127.0.0.1:18080/api/v1/config \
  -X PUT \
  -H 'Authorization: Bearer <token>' \
  -H 'Content-Type: application/json' \
  -d '{"maclaw_llm_url":"https://api.openai.com/v1","maclaw_llm_key":"sk-xxx","maclaw_llm_model":"gpt-5.4"}'
```

### 16.4 Create instance

```bash
curl -s http://127.0.0.1:18080/api/v1/instances \
  -H 'Authorization: Bearer <token>' \
  -H 'Content-Type: application/json' \
  -d '{"name":"primary-agent","description":"demo"}'
```

### 16.5 Send message

```bash
curl -s http://127.0.0.1:18080/api/v1/instances/inst_xxx/messages \
  -H 'Authorization: Bearer <token>' \
  -H 'Content-Type: application/json' \
  -d '{"title":"Demo","content":"Please introduce your capabilities."}'
```

## 17. Quick paths by role

### 17.1 AI conversation client

Applies to desktop AI assistants, chat frontends, IDE plugins, and agent shells.

Recommended call chain:

1. `POST /api/v1/auth/token`
2. `GET /api/v1/me`
3. `GET /api/v1/config`
4. `POST /api/v1/config/validate`
5. `POST /api/v1/instances`
6. `GET /api/v1/instances/{instanceId}/capabilities`
7. `POST /api/v1/instances/{instanceId}/messages`
8. `GET /api/v1/instances/{instanceId}/runs/{runId}` or `GET /api/v1/instances/{instanceId}/runs/{runId}/events`
9. `GET /api/v1/instances/{instanceId}/sessions/{sessionId}/messages`

Implementation focus:

- Bind local state to `tenant_id + user_id` immediately after login.
- Treat the `messages` list as the source of truth for transcript rendering, not only the `run` response.
- If `waiting_for_user=true`, read `session.pending_ask` before deciding the next UI step.
- Decide whether to show tools, ask-user, bash, or SSH features based on `capabilities`.

### 17.2 Control plane or admin console

Applies to SaaS consoles, provisioning backends, tenant admin pages, and ops panels.

Recommended call chain:

1. `POST /api/v1/admin/tenants`
2. `POST /api/v1/admin/tenants/{tenantId}/users`
3. `POST /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials`
4. `GET /api/v1/admin/tenants`
5. `GET /api/v1/admin/tenants/{tenantId}/users`
6. `GET /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials`
7. `GET /api/v1/admin/audit-events`

Implementation focus:

- Admin consoles should store tenant/user/credential metadata, not end-user bearer tokens.
- `audit-events` fits audit views, event timelines, and issue tracing screens.
- Tenant disable, user disable, and credential revoke should usually be presented as distinct actions in UI.

### 17.3 Settings page or config wizard

Applies to first-run setup, settings pages, and config diagnostics.

Recommended call chain:

1. `GET /api/v1/config/schema`
2. `GET /api/v1/config`
3. `POST /api/v1/config/validate`
4. `POST /api/v1/config/test`
5. `PUT /api/v1/config`
6. `POST /api/v1/instances/{instanceId}/refresh-readiness`

Implementation focus:

- Build fields from `config/schema` rather than hardcoding them in frontend.
- Use `validate` for inline form validation and `test` for a dedicated connectivity-check button.
- After saving config, refresh instance readiness instead of forcing users to recreate the instance.

### 17.4 Skill or MCP management page

Applies to skill marketplaces, extension pages, and MCP management UI.

Recommended call chain:

1. `GET /api/v1/skills`
2. `POST /api/v1/skills/search`
3. `POST /api/v1/skills/install` or `POST /api/v1/skills/import`
4. `POST /api/v1/skills/{skillName}/validate`
5. `POST /api/v1/skills/{skillName}/upload`
6. `GET /api/v1/mcp/servers`
7. `POST /api/v1/mcp/servers`
8. `POST /api/v1/mcp/servers/{serverId}/health-check`
9. `GET /api/v1/mcp/servers/{serverId}/tools`

Implementation focus:

- Skills and MCP servers are user-scoped shared resources, not instance-private resources.
- For MCP, run `health-check` before surfacing `tools` to users.
- Skill import, export, and upload may involve large payloads, so UI should handle progress and failure states.

## 18. Common call scenarios

### 18.1 First successful path

Shortest useful path:

1. Admin creates tenant, user, and credential.
2. Client exchanges token.
3. Client fills config.
4. Client creates an instance.
5. Client reads capabilities.
6. Client sends the first message.

### 18.2 Config error recovery path

When `create instance` or `send message` returns a config error, the recommended recovery flow is:

1. Read `config_validation.issues` from the response.
2. Open the settings page or config panel.
3. Call `GET /api/v1/config/schema` to re-render field guidance.
4. Let the user edit, then call `POST /api/v1/config/validate`.
5. If needed, call `POST /api/v1/config/test`.
6. Save and refresh instance readiness.

### 18.3 Real-time conversation path

For a more real-time chat experience:

1. `POST /api/v1/instances/{instanceId}/messages`
2. Read `run.id`
3. Open `GET /api/v1/instances/{instanceId}/runs/{runId}/events` as SSE
4. After `done`, fetch message history once for final consistency

### 18.4 History view path

For products with a conversation history page:

1. `GET /api/v1/instances/{instanceId}/sessions`
2. When a user opens a session, call `GET /messages`
3. When a user archives a session, call `POST /archive`
4. Use `include_archived=true` in the archive/history view
5. Call `POST /restore` when restoring a session

## 19. Integration recommendations for AI clients

- Always call `GET /api/v1/me` after login and bind local client state to `tenant_id + user_id`.
- Treat config as user-shared state, not instance-local state.
- Treat skills as user-shared state.
- Treat MCP servers as user-shared state.
- Prefer `POST /instances/{id}/messages` unless explicit session lifecycle control is required.
- Use `GET /instances/{id}/capabilities` to adapt UI based on tool exposure and policy.
- Use `waiting_for_user` and `pending_ask` to support structured follow-up interactions.
- Surface `config_validation.issues` directly to the user.
- Prefer run SSE when near-real-time status is needed; otherwise poll run status.
- Do not assume coding-session orchestration APIs exist in `MaClawSrv`.

## 20. Minimal client checklist

A production-grade caller should support at least:

- admin bootstrap flow or external provisioning flow
- login and token refresh/re-login
- config load, validate, update, and test
- instance create, list, get, and state refresh
- message send
- session and run polling or SSE subscription
- capability inspection
- skill list/search/install/import/export/upload when relevant
- MCP create/update/start/check/tools when relevant
- structured error rendering

## 21. API inventory

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
