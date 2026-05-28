# HubCenter Hub/Tenant Registration Policy

## Goal

HubCenter owns global registration routing. Hub owners and tenant admins may operate daily invites, but HubCenter keeps the routing boundary, fallback policy, and quota guardrails.

## Concepts

- Hub origin is Hub-level identity: `official` or `self_hosted`.
- Signup policy is Hub+Tenant level. Hub-level values are defaults only.
- Public fallback is tenant-level. Official Hub can host both public fallback tenant and domain-restricted tenants.
- Self-hosted enterprise Hub cannot receive scattered public signup.
- Enterprise temporary external users use tenant invites, not public signup.

## Policy Model

`hub_instances` keeps Hub-level identity and defaults:

```text
hub_origin: official | self_hosted
default_signup_scope: public | domain_restricted | invite_only
```

`hub_tenant_registration_policies` stores tenant-level routing policy:

```text
hub_id
tenant_id
tenant_name
signup_scope: inherit | public | domain_restricted | invite_only
is_public_fallback
invite_enabled
max_active_invites
monthly_invite_quota
per_invite_max_uses_default
per_invite_max_uses_max
status: active | disabled | incomplete
```

Email domains stay in `hub_domain_routes`:

```text
domain -> hub_id + tenant_id + priority
```

## Defaults

Official Hub public tenant:

```text
hub_origin=official
signup_scope=public
is_public_fallback=true
```

Official Hub enterprise tenant:

```text
signup_scope=domain_restricted
is_public_fallback=false
allowed domains required for email-domain registration
```

Self-hosted enterprise Hub tenant:

```text
hub_origin=self_hosted
signup_scope=domain_restricted by default
is_public_fallback=false always
```

Enterprise tenant without domains:

```text
Not eligible for email-domain registration.
Can still use invite_only if invite_enabled=true.
```

Global invite defaults:

```text
invite_enabled=true
max_active_invites=100
monthly_invite_quota=500
per_invite_max_uses_default=1
per_invite_max_uses_max=20
```

## Routing Rules

1. If email plus invite code is provided, HubCenter resolves invite first.
2. Valid invite maps directly to `hub_id + tenant_id`.
3. Invite target must be active and invite-enabled.
4. If no invite, HubCenter matches email domain routes.
5. If no domain route, HubCenter uses exactly one active public fallback tenant.
6. Self-hosted Hub/Tenant cannot be public fallback.
7. Duplicate domains are allowed only with priority; lowest priority wins. Equal-priority duplicates are treated as conflict and should require admin fix or user choice.

## Invite Routing

Tenant or Hub admin generates invite locally. Hub registers invite route with HubCenter:

```text
code_hash -> hub_id + tenant_id
```

HubCenter stores hash only. Hub may register invites only for its own tenants. Duplicate code hash for same Hub/Tenant is idempotent; duplicate hash for another Hub/Tenant is rejected.

Invite usage consumes quota on successful registration, not creation. Creation is guarded by `max_active_invites`.

## Admin Rules

- `official` can only be set by HubCenter admin.
- `is_public_fallback=true` can only be set on official Hub tenant.
- `domain_restricted` without domains is saved as incomplete for routing, not fatal.
- `invite_only` without invite quota uses global defaults unless explicitly overridden.
- Every policy change should be auditable.

## Compatibility

Existing `accept_public_signup=true` maps to a default tenant public policy until explicit tenant policies are created. Existing `corporate_email_domain` and `hub_domain_routes` continue to drive domain routing.
