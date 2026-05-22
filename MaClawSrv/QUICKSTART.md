# MaClawSrv 5-Minute Quickstart

This document is for first-time integrators of `MaClawSrv`: frontend teams, third-party tools, and external services. It keeps only the shortest useful path. For full field-level details, see `API_MANUAL.md`.

## 1. Minimum concepts

- `MaClawSrv` is the REST entrypoint for Maclaw.
- Auth has two layers: admin APIs use `X-MaClaw-Admin-Secret`, user APIs use Bearer tokens.
- The Admin Web supports two admin sign-in modes: account login with the bootstrapped owner/operator username and password, or direct Admin Secret login with `MACLAW_ADMIN_SECRET`. `MACLAW_ADMIN_SETUP_TOKEN` is only for first-run owner creation.
- Config, skills, and MCP servers are user-scoped shared resources, not instance-private resources.
- For actual chat/message execution, prefer `POST /api/v1/instances/{instanceId}/messages`.

## 2. Shortest useful path

### Step 1: Admin creates tenant, user, and credential

```http
POST /api/v1/admin/tenants
POST /api/v1/admin/tenants/{tenantId}/users
POST /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials
```

### Step 2: Client exchanges token

```http
POST /api/v1/auth/token
```

Request body:

```json
{
  "api_key": "ak_demo",
  "api_secret": "demo-secret"
}
```

### Step 3: Read and fill config

```http
GET /api/v1/config/schema
GET /api/v1/config
POST /api/v1/config/validate
PUT /api/v1/config
```

Minimum example:

```json
{
  "maclaw_llm_url": "https://api.openai.com/v1",
  "maclaw_llm_key": "sk-xxx",
  "maclaw_llm_model": "gpt-5.4"
}
```

### Step 4: Create an instance

```http
POST /api/v1/instances
```

Request body:

```json
{
  "name": "primary-agent",
  "description": "demo"
}
```

### Step 5: Read capabilities

```http
GET /api/v1/instances/{instanceId}/capabilities
```

This determines:

- whether sessions are supported
- whether ask-user is supported
- whether local bash / SSH are exposed
- which tools can be shown in UI

### Step 6: Send the first message

```http
POST /api/v1/instances/{instanceId}/messages
```

Request body:

```json
{
  "title": "Demo",
  "content": "Please introduce your capabilities."
}
```

Successful responses typically include:

- `session`
- `run`
- `message`

## 3. Two ways to read results

### Option A: Polling

```http
GET /api/v1/instances/{instanceId}/runs/{runId}
GET /api/v1/instances/{instanceId}/sessions/{sessionId}/messages
```

Best for standard web clients and the simplest to integrate.

### Option B: SSE

```http
GET /api/v1/instances/{instanceId}/runs/{runId}/events
```

Best for a more real-time chat experience. After `done`, fetch `messages` once for final consistency.

## 4. Most common mistakes

- Do not treat config as instance-level state.
- Do not treat skills or MCP servers as instance-private resources.
- Do not assume local bash is available; check `capabilities`.
- Do not rely only on the `run` response body; use `messages` as transcript source of truth.
- If `config_validation.issues` is returned, surface it directly to the user.

## 5. APIs you will use most often

- `POST /api/v1/auth/token`
- `GET /api/v1/me`
- `GET /api/v1/config/schema`
- `GET /api/v1/config`
- `POST /api/v1/config/validate`
- `PUT /api/v1/config`
- `POST /api/v1/instances`
- `GET /api/v1/instances/{instanceId}/capabilities`
- `POST /api/v1/instances/{instanceId}/messages`
- `GET /api/v1/instances/{instanceId}/runs/{runId}`
- `GET /api/v1/instances/{instanceId}/runs/{runId}/events`
- `GET /api/v1/instances/{instanceId}/sessions/{sessionId}/messages`

## 6. What to read next

- Need field-level definitions: `API_MANUAL.md`
- Need control-plane/admin integration: the admin API section in `API_MANUAL.md`
- Need skill or MCP integration: the skill and MCP sections in `API_MANUAL.md`
