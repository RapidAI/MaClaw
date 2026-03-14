# MaClaw Hub Architecture

## 1. Goal
MaClaw Hub is the live work hub for remote sessions. It receives compressed state from Desktop and exposes the mobile-facing control plane.

## 2. Responsibilities

Hub is responsible for:

- self-managed identity for that hub
- machine registration
- session cache
- PWA hosting
- REST and WebSocket APIs
- admin backend

Hub is not responsible for executing Claude Code itself.

## 3. Project Layout

Hub lives under [hub](/D:/workprj/aicoder/hub).

Important directories:

- [cmd/hub](/D:/workprj/aicoder/hub/cmd/hub)
- [internal/auth](/D:/workprj/aicoder/hub/internal/auth)
- [internal/device](/D:/workprj/aicoder/hub/internal/device)
- [internal/session](/D:/workprj/aicoder/hub/internal/session)
- [internal/ws](/D:/workprj/aicoder/hub/internal/ws)
- [internal/httpapi](/D:/workprj/aicoder/hub/internal/httpapi)
- [internal/store/sqlite](/D:/workprj/aicoder/hub/internal/store/sqlite)
- [configs/config.example.yaml](/D:/workprj/aicoder/hub/configs/config.example.yaml)

## 4. Session Data Model

Hub stores compressed session state:

- summary
- preview
- recent important events

It does not store full transcript by default.

## 5. WebSocket Flow

Phase-one Desktop-to-Hub messages:

- `auth.machine`
- `machine.hello`
- `machine.heartbeat`
- `session.created`
- `session.summary`
- `session.preview_delta`
- `session.important_event`
- `session.closed`

## 6. Session Cache

Hub keeps a runtime cache containing:

- latest summary
- preview window
- recent events
- host online state

This cache is used by:

- debug APIs
- later PWA APIs
- later viewer WebSocket subscriptions

## 7. Identity

Hub is its own identity authority. It manages:

- user email
- user SN
- machine token
- viewer token

Desktop enrollment and PWA email-confirm login both terminate at the Hub.

## 8. Admin Backend

Hub includes an admin backend with:

- first-time setup
- admin login
- enrollment mode management
- blocklist
- manual bind
- later invites and approval flow

## 9. Storage

Hub uses SQLite with:

- WAL
- read/write split
- repository abstraction

Key files:

- [provider.go](/D:/workprj/aicoder/hub/internal/store/sqlite/provider.go)
- [migrations.go](/D:/workprj/aicoder/hub/internal/store/sqlite/migrations.go)

## 10. Phase-One Success

Hub phase one is complete when:

- Desktop can authenticate as a machine
- Desktop can create a Claude session
- Hub can store summary/event/preview
- debug routes can inspect the current session
