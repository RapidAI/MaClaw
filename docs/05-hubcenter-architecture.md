# MaClaw Hub Center Architecture

## 1. Goal
MaClaw Hub Center is the directory and entry service for multiple hubs.

Its default official address is:

- `https://hubs.mypapers.top`

## 2. Responsibilities

Hub Center is responsible for:

- hub registration
- hub heartbeat tracking
- email-based entry resolution
- platform governance

Hub Center is not responsible for:

- user login
- SN issuance
- live session relay
- viewer token issuance

## 3. Core Principle

Center answers:

- where should this user go?

Hub answers:

- who is this user?

## 4. Project Layout

Hub Center lives under [hubcenter](/D:/workprj/aicoder/hubcenter).

Important directories:

- [cmd/hubcenter](/D:/workprj/aicoder/hubcenter/cmd/hubcenter)
- [internal/hubs](/D:/workprj/aicoder/hubcenter/internal/hubs)
- [internal/entry](/D:/workprj/aicoder/hubcenter/internal/entry)
- [internal/httpapi](/D:/workprj/aicoder/hubcenter/internal/httpapi)
- [internal/store/sqlite](/D:/workprj/aicoder/hubcenter/internal/store/sqlite)

## 5. Main APIs

### Hub Registration
- `POST /api/hubs/register`
- `POST /api/hubs/{id}/heartbeat`

### Entry Resolution
- `POST /api/entry/resolve`

## 6. Data Model

Key tables:

- `hub_instances`
- `hub_user_links`
- `blocked_emails`
- `blocked_ips`
- `admin_users`
- `system_settings`

## 7. Entry Resolution

Entry resolution should:

- accept email
- filter blocked users
- find matching hubs
- pick a default PWA URL
- return `single`, `multiple`, or `none`

## 8. Governance

Hub Center admin backend should support:

- block email
- block IP
- disable hub
- inspect registered hubs

## 9. Phase-One Boundary

Phase one includes:

- hub registration
- heartbeat
- entry resolution
- basic platform governance skeleton
