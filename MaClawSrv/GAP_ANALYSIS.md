# MaClawSrv Gap Analysis





This document summarizes what `MaClawSrv` already covers well, what is still missing, and where interface consistency should be improved before treating it as a fully polished external control plane.





## Current State





`MaClawSrv` is already broadly usable as a multi-tenant Maclaw agent service.





Strong areas today:





- Admin lifecycle for tenant, user, and credential provisioning


- Admin delete coverage for tenant and user resources


- Tenant and user list filtering for core operator workflows


- Credential lifecycle coverage for create, list, get, update, rotate-secret, and revoke


- User authentication via API key and secret to bearer token exchange


- Shared per-user config lifecycle with schema, get, update, validate, and test APIs


- Runtime lifecycle for instances, sessions, messages, and runs


- Run event streaming through SSE


- Async job resources for heavier skill and MCP operations


- Skill management through REST


- MCP server management through REST


- Usage, audit, alerts, dashboard, overview, and tenant-summary views


- OpenAPI exposure through `/openapi.json`


- Basic ops probes through `/health`, `/livez`, `/readyz`, `/version`, and `/metrics`





That means the product is no longer in a scaffold state. The remaining work is mainly about completeness, consistency, and operability.





## Missing Functional Coverage





### 1. Admin lifecycle maturity





Base delete coverage is now present, and explicit delete protection is available, but lifecycle policy is still simple overall.





Still missing or worth improving:





- Soft-delete or recycle-bin semantics


- Delete protection for managed tenants and users is now available


- Export-before-delete workflows


- More explicit retirement policy for tenant/user offboarding





### 2. Credential lifecycle maturity





Current coverage now includes list, create, get, update, rotate-secret, and revoke.





Still missing pieces:





- Auto-rotation policy beyond current expires_at support and expiry alerts


- Server-generated secret issuance and one-time reveal workflows are now available





So credential management is much closer to a full control-plane API, but not fully finished yet.





### 3. Backup, export, and migration





The basic REST surface now covers:





- Exporting service state via `GET /api/v1/admin/export`


- Importing service state via `POST /api/v1/admin/import`





Still missing:





- Snapshotting tenant data


- Migrating tenants or users between environments


- Higher-assurance restore validation and rollback





This is still a meaningful operations gap for enterprise adoption, but the baseline export/import path now exists.





### 4. Service-level events and webhooks





Current eventing is limited to run SSE and user-polled async jobs.





Still missing:





- Admin event subscriptions


- Tenant/user lifecycle webhooks


- Run completion webhook delivery


- Skill/MCP operation completion hooks





If external systems need to automate around `MaClawSrv`, this is a likely next gap.





### 5. Deployment and ops signals





The service now exposes `GET /health`, `GET /livez`, `GET /readyz`, `GET /version`, and `GET /metrics` for baseline production integration, and `GET /api/v1/admin/system/readiness` for detailed admin-only readiness diagnostics.





Still worth improving:





- Optional downstream provider checks





### 6. Higher-level analytics





Existing overview/dashboard data is useful, but still fairly basic.





Likely next analytics surfaces:





- Hot tenants


- Over-quota tenants


- Inactive tenants/users


- Error-rate trends


- Skill usage analytics


- MCP usage analytics





## Interface Consistency Gaps





### 1. Action vs resource conventions should stay explicit





The current API style is internally consistent, but external users still need the pattern spelled out:





- Plain resource lifecycle uses `GET`, `POST`, `PATCH`, `PUT`, `DELETE`


- State transition actions use explicit action routes such as `/stop`, `/resume`, `/archive`, `/restore`, `/health-check`





This is fine. It just needs to stay clearly documented so integrators do not guess wrong.





### 2. Pagination documentation must track real route coverage





Actual pagination support includes:





- admin tenants


- admin users


- admin credentials


- admin audit-events


- jobs


- MCP servers


- skills


- instances


- sessions


- messages


- runs





Any docs that omit jobs, MCP, or skill pagination become misleading for client SDK authors.





### 3. OpenAPI should remain the source of truth





`openapi.go` is much cleaner than prose docs and should be treated as the canonical route inventory.





Recommended discipline:





- add every route in `http.go`


- mirror it in `openapi.go`


- update prose docs last





### 4. Ops probes should keep gaining signal





The probes now exist and `/readyz` confirms the configured data root is reachable, but production users will still expect deeper readiness semantics over time.





## Suggested Next Priorities





### Priority 1





- Keep README, manuals, and OpenAPI aligned with actual routes


- Add richer restore policies, validation, and snapshot endpoints


- Add stronger lifecycle policy for delete/retire operations





### Priority 2





- Add richer credential lifecycle semantics


- Add webhook/event subscription model


- Add richer admin analytics





### Priority 3





- Add cross-tenant operator search and broader analytics slices


- Add stronger readiness and dependency health signals





## Recommendation





If the goal is to make `MaClawSrv` feel complete for external platform integration, the shortest path is:





1. Keep docs and OpenAPI exact.


2. Strengthen restore, retire, and credential lifecycle policies.


3. Add webhooks and broader analytics.


4. Keep improving readiness and operability signals.





That sequence improves both actual capability and integration confidence without forcing a large rewrite.

## Recent completion: delete protection

- Tenant and user delete protection is now available.
- Admin flows can mark critical resources as protected, inspect the flag through `delete-check`, and receive `409` on final delete attempts until protection is cleared.
- This closes one of the lifecycle-safety gaps around accidental destructive operations.

## Recent completion: server-generated credentials

- Server-generated credential issuance is now available.
- Admin callers can omit `api_key` and/or `api_secret` during credential creation.
- Generated secrets are one-time reveal values and are not stored or returned by later read APIs.

## Recent completion: credential expiry alerts

- Credential expiry alerts are now available through admin alerts.
- Operators can filter `kind=credential_expiring` or `kind=credential_expired` and tune the lookahead window with `credential_expiry_window_days`.
- Alert credential payloads remain sanitized.

## Recent completion: server-generated credential rotation

- Server-generated credential rotation is now available.
- Admin callers can omit `api_secret` on secret rotation or `api_key` on key rotation.
- Generated plaintext values are one-time reveal values returned only in the rotation response.

## Recent completion: credential overview counters

- Admin overview now includes credential lifecycle counters.
- Dashboard consumers can show total, active, suspended, revoked, expired, and expiring credential counts without separately scanning all users.

## Recent completion: tenant summary credential counters

- Tenant summary now exposes credential lifecycle counters at the tenant level and per-user level.
- Admin consoles can display total, active, suspended, revoked, expired, and expiring credential counts without separately listing credentials for every user.

## Recent completion: credential metrics

- `/metrics` now exposes credential total, status, expired, and expiring gauges.
- Operators can alert on credential lifecycle risk from Prometheus without polling admin JSON endpoints.

## Recent completion: usage summary credential counters

- Authenticated users can now see their own aggregate credential lifecycle counters in `/api/v1/usage/summary`.
- The endpoint remains safe-by-default because it exposes counts only, not credential material.

## Recent completion: credential create expiry

- Credential creation now accepts `expires_at` directly.
- Admin callers can create generated credentials with expiry in one request instead of issuing an unrestricted credential and patching it afterward.
