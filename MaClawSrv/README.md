# MaClawSrv









`MaClawSrv` is the REST service entrypoint for exposing Maclaw agent capability to external programs.









## Positioning









- Core business logic lives in `corelib/agentservice`.




- Shared agent execution is reused from `corelib/agent` through the service adapter layer.




- `MaClawSrv` focuses on HTTP routing, authentication, JSON mapping, and control-plane orchestration.




- Data isolation is `tenant -> user`.




- Each user has one shared data root, so config, memory, skill state, MCP state, and long-term history are reused across that user's instances.




- Multiple logical instances can run at the same time under one user.




- `MaClawSrv` is a pure agent service. It does not expose coding-session orchestration or local programming workspace management APIs.




- Host-local bash is security-sensitive and disabled by default.




- SSH capability is exposed only when direct SSH is enabled or SSH host labels are configured for the user.




- When `MACLAW_DATA_ROOT` is unset, the default root is `~/.maclaw_srv`.









## Security









- `MACLAW_ADMIN_SECRET` is required and must be explicitly configured.




- `MACLAW_TOKEN_SECRET` is required and must be explicitly configured.




- Default listen address is `127.0.0.1:18080`; wildcard addresses such as `:18080` require TLS or `MACLAW_ALLOW_INSECURE_HTTP=true`.




- Optional TLS is supported with `MACLAW_TLS_CERT_FILE` and `MACLAW_TLS_KEY_FILE`.




- Credential secrets are stored as salted `scrypt` digests.




- API keys are persisted as `hash + prefix`, not plaintext.




- Runtime config files containing user secrets are written with owner-only permissions.




- Local bash requires `MACLAW_ENABLE_LOCAL_BASH=true` and should remain disabled in multi-tenant deployments unless an external OS/container sandbox exists.




- `GET /health` only returns service status and does not expose filesystem paths.




- `GET /readyz` verifies the configured data root exists, is a directory, and is writable before returning `ready`.




- `GET /api/v1/admin/system/readiness` returns admin-only detailed readiness checks, including writable state-path diagnostics.









## Data Layout









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









## API Groups









System:









- `GET /health`




- `GET /livez`




- `GET /readyz`




- `GET /version`




- `GET /openapi.json`




- `GET /api/v1/openapi.json`









Admin:









- `GET /api/v1/admin/system/readiness`




- `GET /api/v1/admin/overview`




- `GET /api/v1/admin/dashboard`




- `GET /api/v1/admin/alerts`




- `GET /api/v1/admin/audit-events`




- `GET /api/v1/admin/export` with `tenant_id`, `user_id`, and include flags for backup/export flows




- `POST /api/v1/admin/import` for restore/import flows with optional `overwrite=true` or `dry_run=true`, returning `conflicts`, `warnings`, and `plan` in precheck mode




- `GET /api/v1/admin/tenants` with `status` and `name` filters




- `POST /api/v1/admin/tenants`




- `GET /api/v1/admin/tenants/{tenantId}`




- `GET /api/v1/admin/tenants/{tenantId}/summary`




- `PATCH /api/v1/admin/tenants/{tenantId}`




- `DELETE /api/v1/admin/tenants/{tenantId}`




- `GET /api/v1/admin/users` with cross-tenant `tenant_id`, `status`, `name`, and `email` filters




- `GET /api/v1/admin/tenants/{tenantId}/users` with `status`, `name`, and `email` filters




- `POST /api/v1/admin/tenants/{tenantId}/users`




- `GET /api/v1/admin/tenants/{tenantId}/users/{userId}`




- `PATCH /api/v1/admin/tenants/{tenantId}/users/{userId}`




- `DELETE /api/v1/admin/tenants/{tenantId}/users/{userId}`




- `GET /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials`




- `POST /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials`




- `GET /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials/{credentialId}`




- `PATCH /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials/{credentialId}`




- `POST /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials/{credentialId}/rotate-secret`




- `POST /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials/{credentialId}/rotate-key`




- `DELETE /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials/{credentialId}`









Auth and user config:









- `POST /api/v1/auth/token`




- `GET /api/v1/me`




- `GET /api/v1/config/schema`




- `GET /api/v1/config`




- `PUT /api/v1/config`




- `POST /api/v1/config/validate`




- `POST /api/v1/config/test`




- `GET /api/v1/usage/summary`









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









Skills:









- `GET /api/v1/skills`




- `POST /api/v1/skills/search`




- `POST /api/v1/skills/install`




- `POST /api/v1/skills/import`




- `GET /api/v1/jobs` with optional `kind`, `status`, `limit`, and `before` filters




- `DELETE /api/v1/jobs` with `kind`, `status`, `before`, or `all=true` for bulk terminal-job cleanup




- `GET /api/v1/jobs/{jobId}`




- `POST /api/v1/jobs/{jobId}/cancel`




- `DELETE /api/v1/jobs/{jobId}`




- `GET /api/v1/skill-uploads/{submissionId}`




- `GET /api/v1/skill-market/account`




- `GET /api/v1/skills/{skillName}`




- `DELETE /api/v1/skills/{skillName}`




- `GET /api/v1/skills/{skillName}/export`




- `POST /api/v1/skills/{skillName}/validate`




- `POST /api/v1/skills/{skillName}/improve`




- `POST /api/v1/skills/{skillName}/upload`









Runtime:









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









## Interface Conventions









- Resource reads use `GET`.




- Resource creation uses `POST`.




- Mutable resource updates use `PATCH` or `PUT`.




- Resource deletion uses `DELETE`.




- State-changing actions that are not plain CRUD use explicit action routes such as `/stop`, `/resume`, `/archive`, `/restore`, `/health-check`, and `/refresh-readiness`.




- Admin APIs use `X-MaClaw-Admin-Secret`.




- User APIs use `Authorization: Bearer <token>`.




- Machine-readable source of truth is `openapi.go`, exposed at `/openapi.json`.









## Pagination









Cursor pagination is currently supported by these list endpoints:









- `GET /api/v1/admin/tenants` with `status` and `name` filters




- `GET /api/v1/admin/users` with cross-tenant `tenant_id`, `status`, `name`, and `email` filters




- `GET /api/v1/admin/tenants/{tenantId}/users` with `status`, `name`, and `email` filters




- `GET /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials`




- `GET /api/v1/admin/audit-events`




- `GET /api/v1/admin/export` with `tenant_id`, `user_id`, and include flags for backup/export flows




- `POST /api/v1/admin/import` for restore/import flows with optional `overwrite=true` or `dry_run=true`, returning `conflicts`, `warnings`, and `plan` in precheck mode




- `GET /api/v1/mcp/servers`




- `GET /api/v1/skills`




- `GET /api/v1/instances`




- `GET /api/v1/instances/{instanceId}/sessions`




- `GET /api/v1/instances/{instanceId}/sessions/{sessionId}/messages`




- `GET /api/v1/instances/{instanceId}/runs`









Query parameters:









- `limit`: positive integer, default `100`, max `500`




- `before`: cursor value




- session list: `include_archived` must be `true` or `false`




- message list: `role` accepts `user`, `assistant`, or `system`




- run list: `status` accepts `running`, `succeeded`, `failed`, or `cancelled`




- run list: `response_source` currently accepts `ask_user`




- run list: `waiting_for_user` must be `true` or `false`









Notes:









- Most list endpoints use RFC3339 or RFC3339Nano timestamps as the cursor.




- `GET /api/v1/skills` uses case-insensitive skill name ordering and returns the next name in `next_before`.




- Responses keep `items` and add `limit`, `has_more`, and optional `next_before`.














## Async Jobs









- `POST /api/v1/skills/install?async=true` returns `202 Accepted` and a user-scoped job resource.




- `POST /api/v1/skills/import?async=true` returns `202 Accepted` and a user-scoped job resource.




- `POST /api/v1/skills/{skillName}/upload?async=true` returns `202 Accepted` and a user-scoped job resource.




- `POST /api/v1/mcp/servers?async=true` returns `202 Accepted` and a user-scoped job resource.




- `PATCH /api/v1/mcp/servers/{serverId}?async=true` returns `202 Accepted` and a user-scoped job resource.




- `POST /api/v1/mcp/servers/{serverId}/start?async=true` returns `202 Accepted` and a user-scoped job resource.




- `POST /api/v1/mcp/servers/{serverId}/stop?async=true` returns `202 Accepted` and a user-scoped job resource.




- `POST /api/v1/mcp/servers/{serverId}/health-check?async=true` returns `202 Accepted` and a user-scoped job resource.




- Poll with `GET /api/v1/jobs/{jobId}` until `status` becomes `succeeded`, `failed`, or `canceled`.




- Use `GET /api/v1/jobs` to list recent jobs, `DELETE /api/v1/jobs?status=succeeded&before=<timestamp>` or `DELETE /api/v1/jobs?all=true` to bulk-remove finished jobs, `POST /api/v1/jobs/{jobId}/cancel` to request cancellation, and `DELETE /api/v1/jobs/{jobId}` to remove a single finished job.




- Async job payloads are isolated by tenant/user and return the same result shape as the synchronous API when finished.









Already in place:









- Multi-tenant admin control plane for tenants, users, and credentials




- Token issuance and bearer authentication, with immediate old-token invalidation after credential secret rotation or revoke




- Shared per-user config lifecycle with schema, validate, test, and update APIs




- Runtime lifecycle for instances, sessions, messages, runs, and run SSE events




- Skill lifecycle coverage for search, install, import, export, validate, improve, upload, delete, and async job polling for heavy skill operations




- MCP lifecycle coverage for create, update, delete, start, stop, health-check, and tool listing




- Usage, audit, overview, dashboard, tenant summary, alerts, metrics, and admin export/import endpoints









## Main Gaps









The service is broadly usable, but it is not yet a fully complete control plane. Current gaps include:









- Credential lifecycle has secret rotation and status control, but still lacks full API key reissue/expiry policy flows




- Admin list filtering is now available for tenants and users, but deeper fuzzy search is still thin




- Service export/import is now available, but snapshot orchestration and stronger restore policy are still missing




- No service-level webhook/event subscription model beyond run SSE




- Metrics are available at `/metrics`, including service totals, unready-instance gauges, token auth failure and rate-limit counters, waiting/failed run gauges, run succeeded/failed event counters, and async job status gauges, but richer deployment dashboards and alert integrations are still thin




- No stronger analytics surfaces such as hot tenants, over-quota tenants, inactive users, or MCP/skill usage trends









## Docs









- English detailed guide: `API_MANUAL.md`




- Chinese detailed guide: `API_MANUAL.zh-CN.md`




- English quickstart: `QUICKSTART.md`




- Chinese quickstart: `QUICKSTART.zh-CN.md`




- Gap analysis: `GAP_ANALYSIS.md`




- Chinese gap analysis: `GAP_ANALYSIS.zh-CN.md`








































## Delete safety

- Tenant and user destructive deletes now require `?confirm=true` on the delete request.
- Recommended admin flow: call `delete-check` or `retire-plan` first, confirm the impact and blockers, then issue the final delete with explicit confirmation.
- A request without `confirm=true` returns `400` and does not remove the tenant or user.

## Delete protection

- Delete protection can be configured on both tenants and users through the normal create and update admin payloads.
- Protected resources appear as `delete_protected=true` in delete-check responses, and the blockers list includes `kind=delete_protected`.
- Even with `?confirm=true`, protected tenants or users still return `409` until the protection flag is cleared.
