# MaClawSrv

`MaClawSrv` is the REST entrypoint for exposing Maclaw agent capability to other programs.

## Design

- Core business logic lives in `corelib/agentservice`.
- `MaClawSrv` only handles HTTP routing, auth headers, JSON encode/decode, and status mapping.
- Agent execution is now delegated to `corelib/agent` via a service-specific host adapter, so the REST service reuses the same shared loop and prompt foundation as GUI/TUI.
- The service root data directory is configured with `MACLAW_DATA_ROOT`. When unset, MaClawSrv defaults to `~/.maclaw_srv`.
- Data isolation is strict at `tenant -> user` level.
- Every Maclaw user owns one shared data root, so memory, config, knowledge, and history are reused across all instances of that user.
- Each `instance` is only a logical agent runtime entrypoint. It has its own private runtime directory, but it points to the same user `DataDir`.
- `MaClawSrv` is a pure agent system. It does not expose programming tools, code workspace management, or coding-session orchestration APIs.
- Remote SSH tools are available through the shared agent runtime; host-local bash execution is security-sensitive and is disabled by default.
- User parameters are exposed through REST, so callers can discover required fields, update config, and validate readiness before starting instances.
- Shared user config is also synchronized to `user/data/config.json`, which matches existing Maclaw runtime conventions.
- Tenant, user, credential, instance, session, message, and run metadata are persisted under `MACLAW_DATA_ROOT/state/store.json` by the default file store.
- Each instance exposes a bootstrap description so a future runner can start the real runtime without guessing any paths.

## Security Notes

- `MACLAW_ADMIN_SECRET` must be explicitly configured and should be high-entropy. The service no longer falls back to a default admin secret.
- `MACLAW_TOKEN_SECRET` must be explicitly configured and should be high-entropy. The service no longer falls back to a default token secret.
- Optional TLS can be enabled with `MACLAW_TLS_CERT_FILE` and `MACLAW_TLS_KEY_FILE`.
- Runtime config files that contain user LLM credentials are now written with owner-only permissions.
- Credential secrets are stored as salted `scrypt` digests, and API keys are persisted as `hash + prefix` instead of plaintext.
- Local host bash execution must be explicitly enabled with `MACLAW_ENABLE_LOCAL_BASH=true`; leave it disabled for multi-tenant deployments unless an external sandbox exists.
- `GET /health` now returns only service status and does not expose filesystem paths.

## Directory layout

```text
MACLAW_DATA_ROOT/
  state/
    store.json
  tenants/
    tenant_xxx/
      users/
        user_xxx/
          data/
            config.json
          config/
            app_config.json
          instances/
            inst_xxx/
              bootstrap.json
              workspace/
```

`workspace/` is currently only an instance-private scratch directory in the scaffold. It is not exposed as a coding workspace capability.

## Integration Docs

- See `QUICKSTART.md` for the shortest English integration path.
- See `QUICKSTART.zh-CN.md` for the shortest Chinese integration path.
- See `API_MANUAL.md` for the detailed English client integration guide aimed at AI coding tools, desktop apps, automation services, and external control planes.
- See `API_MANUAL.zh-CN.md` for the detailed Chinese integration manual.

## REST API

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
- `POST /api/v1/auth/token`
- `GET /api/v1/me`
- `GET /api/v1/config/schema`
- `GET /api/v1/config`
- `PUT /api/v1/config`
- `POST /api/v1/config/validate`
- `POST /api/v1/config/test`
- `GET /api/v1/usage/summary`
- `GET /api/v1/mcp/servers`
- `POST /api/v1/mcp/servers`
- `GET /api/v1/mcp/servers/{serverId}`
- `PATCH /api/v1/mcp/servers/{serverId}`
- `DELETE /api/v1/mcp/servers/{serverId}`
- `POST /api/v1/mcp/servers/{serverId}/start`
- `POST /api/v1/mcp/servers/{serverId}/stop`
- `POST /api/v1/mcp/servers/{serverId}/health-check`
- `GET /api/v1/mcp/servers/{serverId}/tools`
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
- `GET /api/v1/instances`
- `POST /api/v1/instances`
- `GET /api/v1/instances/{instanceId}`
- `PATCH /api/v1/instances/{instanceId}`
- `DELETE /api/v1/instances/{instanceId}`
- `GET /api/v1/instances/{instanceId}/capabilities`
- `POST /api/v1/instances/{instanceId}/stop`
- `POST /api/v1/instances/{instanceId}/resume`
- `POST /api/v1/instances/{instanceId}/refresh-readiness`
- `GET /api/v1/instances/{instanceId}/summary`
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

## Tenant and user status

- Admin callers can update tenant and user status with `PATCH` and `status` set to `active` or `disabled`.
- Disabled tenants and disabled users cannot receive new tokens.
- Existing bearer tokens are also rejected after the owning tenant or user is disabled.
- Admin callers can list user credentials and revoke a credential with `DELETE`; revoked credentials cannot receive new tokens.
- Newly issued bearer tokens are bound to the issuing credential, so revoking that credential invalidates future authenticated requests made with those tokens.

## Audit events

- `GET /api/v1/admin/audit-events` returns control-plane and user action audit events.
- Supports optional filters: `tenant_id`, `user_id`, `action`, `resource_type`.
- Supports the same `limit` and `before` cursor pagination used by other list endpoints.
- Current audited actions include tenant, user, credential, config, instance, session, message, run, and token issuance events.

## Config preflight

- `POST /api/v1/config/validate` with empty body validates the saved user config.
- `POST /api/v1/config/test` with empty body tests the saved user config against the real LLM endpoint.
- Both endpoints also accept either `{"app_config": {...}}` or a raw `AppConfig` JSON body as a candidate config for dry-run validation/testing before saving.

## Pagination

The list endpoints support cursor pagination with query parameters:

- `limit`: positive integer, default `100`, maximum `500`.
- `before`: RFC3339/RFC3339Nano timestamp. When provided, only records created before this timestamp are returned.

Supported endpoints:

- `GET /api/v1/instances`
- `GET /api/v1/instances/{instanceId}/sessions`
- `GET /api/v1/instances/{instanceId}/sessions/{sessionId}/messages`
- `GET /api/v1/instances/{instanceId}/runs`
- `GET /api/v1/admin/audit-events`

Responses keep the backward-compatible `items` field and add `limit`, `has_more`, and optional `next_before`. Items are returned in chronological order inside the selected page, so callers can request older pages with `?before=<next_before>`.

Archived sessions are hidden by default in `GET /api/v1/instances/{instanceId}/sessions`; add `?include_archived=true` when a control plane needs to show archived history.

## Instance readiness

- `PATCH /api/v1/instances/{instanceId}` updates mutable instance fields such as `name`, `description`, and `metadata` without changing lifecycle state.
- `GET /api/v1/instances/{instanceId}/summary` returns an aggregated instance view including session, message, run, waiting, and archival counters.
- Instance responses now include `ready`, `ready_reason`, and `readiness`.
- `readiness` currently reflects whether the instance runtime directory exists, whether the shared user data directory exists, whether the saved LLM config is valid, and whether the instance status is `ready`.
- `GET /api/v1/instances` can be used as the main list view for control planes.
- `GET /api/v1/instances/{instanceId}` can be used when the caller wants a single authoritative readiness snapshot.
- `POST /api/v1/instances/{instanceId}/stop` marks the logical agent instance as stopped; stopped instances reject new messages.
- `POST /api/v1/instances/{instanceId}/resume` re-validates the saved user config and marks the instance ready only when config is valid.
- `POST /api/v1/instances/{instanceId}/refresh-readiness` recalculates readiness without changing the intended lifecycle state.

## Session lifecycle

- `PATCH /api/v1/instances/{instanceId}/sessions/{sessionId}` updates mutable session fields such as `title` and `metadata`.
- `POST /api/v1/instances/{instanceId}/sessions/{sessionId}/archive` performs a soft archive and preserves all messages and runs.
- Archived sessions are excluded from the default list response, but can still be fetched directly by ID.
- Archived sessions reject new message sends with `409 Conflict`.
- `POST /api/v1/instances/{instanceId}/sessions/{sessionId}/restore` re-enables the session for normal message flow.

## Messaging shortcuts

- `POST /api/v1/instances/{instanceId}/messages` is the one-step send endpoint.
- If `session_id` is provided, the message is appended to that session.
- If `session_id` is omitted, MaClawSrv creates a new session using optional `agent_id`, `title`, and `session_metadata`.
- The response includes `session`, `run`, and assistant `message`.
- When the shared agent loop triggers `ask_user`, the session enters a waiting state and the next user message is treated as the answer to that structured follow-up question.

## Run Query

- `GET /api/v1/instances/{instanceId}/runs` returns runs for the authenticated user and instance.
- Supports optional filters: `status`, `session_id`, `response_source`, and `waiting_for_user`.
- Uses `started_at` for `before` cursor pagination.

## Message Query

- `GET /api/v1/instances/{instanceId}/sessions/{sessionId}/messages` supports optional filters: `role`, `since`, and `until`.
- `since` and `until` use RFC3339 timestamps and are applied before pagination.

## Usage Summary

- `GET /api/v1/usage/summary` returns the authenticated user's aggregate service usage.
- The summary includes instance, session, message, run, run-status, and last-activity counters.
- The endpoint is scoped to the current bearer token, so tenants and users remain isolated.

## Start checks

- `POST /api/v1/instances` validates the saved user config before creating the instance.
- If required parameters are missing or invalid, the API returns an error together with `config_validation.issues` so the caller can guide the user to complete configuration.
- The current required configuration is the selected LLM endpoint, credential, and model. No programming-tool or workspace parameters are required by `MaClawSrv`.

## Notes

- The current service executor reuses `corelib/agent.RunLoop`, shared system prompt construction, session task tracking, and persistent per-user memory store initialization.
- `MaClawSrv` still deliberately avoids GUI/TUI-specific coding session orchestration and only exposes the pure service-hosted agent capability set.


