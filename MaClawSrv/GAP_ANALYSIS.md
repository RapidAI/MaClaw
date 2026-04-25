# MaClawSrv Gap Analysis

This document summarizes what `MaClawSrv` already covers well, what is still missing, and where interface consistency should be improved before treating it as a fully polished external control plane.

## Current State

`MaClawSrv` is already broadly usable as a multi-tenant Maclaw agent service.

Strong areas today:

- Admin lifecycle for tenant, user, and credential provisioning
- Admin delete coverage for tenant and user resources
- Credential lifecycle coverage for create, list, get, update, rotate-secret, and revoke
- User authentication via API key and secret to bearer token exchange
- Shared per-user config lifecycle with schema, get, update, validate, and test APIs
- Runtime lifecycle for instances, sessions, messages, and runs
- Run event streaming through SSE
- Skill management through REST
- MCP server management through REST
- Usage, audit, alerts, dashboard, overview, and tenant-summary views
- OpenAPI exposure through `/openapi.json`
- Basic ops probes through `/health`, `/livez`, `/readyz`, and `/version`

That means the product is no longer in a scaffold state. The remaining work is mainly about completeness, consistency, and operability.

## Missing Functional Coverage

### 1. Admin lifecycle maturity

Base delete coverage is now present, but lifecycle policy is still simple.

Still missing or worth improving:

- Soft-delete or recycle-bin semantics
- Delete protection for managed tenants
- Export-before-delete workflows
- More explicit retirement policy for tenant/user offboarding

### 2. Credential lifecycle maturity

Current coverage now includes list, create, get, update, rotate-secret, and revoke.

Still missing pieces:

- API key rotation while preserving safe lookup semantics
- Separate suspend/expire lifecycle beyond simple active/revoked states
- More explicit secret generation and one-time reveal workflows

So credential management is much closer to a full control-plane API, but not fully finished yet.

### 3. Admin search and filtering

Tenant and user list APIs are paginated, but still weak for operator workflows.

Useful missing filters:

- Tenant `status`
- Tenant `name`
- User `status`
- User `name`
- User `email`
- Cross-tenant user search

These become important once the system has enough tenants and users that cursor-only browsing is no longer practical.

### 4. Backup, export, and migration

The basic REST surface now covers:

- Exporting service state via `GET /api/v1/admin/export`
- Importing service state via `POST /api/v1/admin/import`

Still missing:

- Snapshotting tenant data
- Migrating tenants or users between environments
- Higher-assurance restore validation and rollback

This is still a meaningful operations gap for enterprise adoption, but the baseline export/import path now exists.

### 5. Async job model

Some operations are already heavier than normal CRUD, including:

- Skill install
- Skill import
- Skill upload
- MCP start
- MCP health-check

Today they are modeled as direct request/response calls. A more production-grade design would introduce a job resource like:

- `POST /api/v1/jobs`
- `GET /api/v1/jobs/{jobId}`
- `GET /api/v1/jobs/{jobId}/events`
- `POST /api/v1/jobs/{jobId}/cancel`

That would give better retry, progress, history, and timeout handling.

### 6. Service-level events and webhooks

Current eventing is limited to run SSE.

Still missing:

- Admin event subscriptions
- Tenant/user lifecycle webhooks
- Run completion webhook delivery
- Skill/MCP operation completion hooks

If external systems need to automate around `MaClawSrv`, this is a likely next gap.

### 7. Deployment and ops endpoints

The service now exposes `GET /health`, `GET /livez`, `GET /readyz`, `GET /version`, and `GET /metrics` for baseline production integration.

### 8. Higher-level analytics

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
- MCP servers
- skills
- instances
- sessions
- messages
- runs

Any docs that omit MCP or skill pagination become misleading for client SDK authors.

### 3. OpenAPI should remain the source of truth

`openapi.go` is much cleaner than prose docs and should be treated as the canonical route inventory.

Recommended discipline:

- add every route in `http.go`
- mirror it in `openapi.go`
- update prose docs last

### 4. Heavier operations still look synchronous at the HTTP boundary

This is less a bug and more an interface maturity issue. Operations such as skill install or MCP health-check feel like future job resources, but today they still look like immediate synchronous actions.

## Suggested Next Priorities

### Priority 1

- Keep README and manuals aligned with actual routes
- Add tenant/user filtering and search
- Add metrics exposure

### Priority 2

- Add richer restore policies, validation, and snapshot endpoints
- Add stronger lifecycle policy for delete/retire operations
- Add richer credential lifecycle semantics

### Priority 3

- Add async job model
- Add webhook/event subscription model
- Add richer admin analytics

## Recommendation

If the goal is to make `MaClawSrv` feel complete for external platform integration, the shortest path is:

1. Normalize docs and OpenAPI consistency.
2. Add search, filtering, and metrics.
3. Add richer restore policy and snapshot orchestration.
4. Introduce async jobs and webhook-style integration.

That sequence improves both actual capability and integration confidence without forcing a large rewrite.





