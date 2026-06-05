# MaClawDataSrv Implementation

`datasrv` is an independently buildable Go module:

```powershell
cd datasrv
go test ./...
go build ./cmd/maclaw-data-srv
```

The module path is `github.com/RapidAI/CodeClaw/datasrv`. During in-repo
development, `datasrv/go.mod` uses `replace github.com/RapidAI/CodeClaw => ..`
so the implementation compiles against the local `corelib/structureddata`
contract package.

Package boundary reference: [`docs/datasrv-structureddata-boundary.md`](../docs/datasrv-structureddata-boundary.md).
Production operations guide: [`docs/datasrv-production-ops-guide.md`](../docs/datasrv-production-ops-guide.md).

`datasrv/structureddata` owns the concrete structured data service implementation:

- SQLite schema, migrations, and store methods.
- Service orchestration and governance workflows.
- HTTP API, OpenAPI document, and embedded Web Console.
- Implementation tests for cursor pagination, records, connectors, imports, backups, and governance flows.

Shared request/response DTOs and caller-facing contracts live in `corelib/structureddata`.
Implementation files in this directory should import or alias those contract types instead of
adding new shared DTO definitions here.

Executable entry points, including `cmd/maclaw-data-srv`, should import this implementation
package directly when constructing the service, store, or HTTP server.

The exported implementation surface is intentionally narrow:

- `NewSQLiteStore`
- `NewService`
- `NewHTTPServer`
- `NewHTTPServerWithAPIKeys`
- `ParseAPIKeyPolicies`

All other exported request/response shapes should be defined in `corelib/structureddata` and
aliased into `datasrv/structureddata` through `*_alias.go` files.

## Run and Admin Recovery

Start the HTTP service:

```powershell
$env:MACLAW_DATA_SQLITE_PATH = "D:\data\maclaw\data.db"
go run ./cmd/maclaw-data-srv
```

By default, DataSrv listens on `127.0.0.1:18180`. Override the loopback listen
address with `MACLAW_DATA_HTTP_ADDR`, for example `127.0.0.1:18182` during
local side-by-side testing. Plain HTTP startup rejects non-loopback addresses.

On Windows, the same `maclaw-data-srv.exe` can run either as a normal command
line process or as an NT service. When the binary is launched by the Windows
Service Control Manager, stop and shutdown controls are mapped to the same
graceful HTTP shutdown path used by Ctrl+C:

```powershell
sc.exe create MaClawDataSrv binPath= "C:\MaClaw\maclaw-data-srv.exe" start= auto
sc.exe start MaClawDataSrv
sc.exe stop MaClawDataSrv
```

The standalone Windows installer registers `MaClawDataSrv` as an automatic
startup service and starts it after installation.

`MACLAW_DATA_TOKEN` is optional. When set, it acts as a service bearer token and
must be at least 24 characters. Without it, use the first-time administrator
setup and login flow to obtain a temporary bearer token.
`MACLAW_DATA_ADMIN_PASSWORD_MIN_LENGTH` optionally controls the local
administrator password minimum length. The default is 8, and accepted values are
clamped to the 8-128 range.
`MACLAW_DATA_ADMIN_LOGIN_MAX_FAILURES` optionally enables a local failed-login
lockout threshold. The default is 0, which disables lockout. When enabled,
`MACLAW_DATA_ADMIN_LOGIN_LOCKOUT_MINUTES` controls the lockout window and
defaults to 15 minutes. Failed-login counters and active lockout deadlines are
stored in SQLite, so restarting the service does not clear an active lockout.
`GET /api/v1/setup/status` returns a `password_policy` object with the active
minimum length, lockout settings, and offline reset availability so the Web
Console and automation agents can discover the current administrator password
rules before setup or recovery.

The Web Console is served from `/` and `/ui`. First-time administrator setup is
available through the Web Console and these HTTP endpoints:

- `GET /api/v1/setup/status`
- `POST /api/v1/setup/admin`
- `POST /api/v1/login`

Phase 2 administrator management APIs are available after signing in with a
`data_admin` bearer token:

- `GET /api/v1/data/admin/accounts`
- `POST /api/v1/data/admin/accounts`
- `PATCH /api/v1/data/admin/accounts/{username}`
- `GET /api/v1/data/admin/sessions`
- `PATCH /api/v1/data/admin/sessions/{sessionId}`
- `DELETE /api/v1/data/admin/sessions/{sessionId}`
- `GET /api/v1/data/admin/tenants`
- `POST /api/v1/data/admin/tenants/sync`
- `GET /api/v1/data/admin/hub-registration`
- `POST /api/v1/data/admin/hub-registration`
- `POST /api/v1/data/admin/hub-registration/register`
- `POST /api/v1/data/admin/hub-registration/sync-tenants`

`POST /api/v1/data/admin/accounts` creates additional local administrator
accounts. `PATCH` can update display name, role, and enabled state. Disabling an
administrator revokes that account's active sessions, and the service refuses to
disable the last enabled administrator. Session APIs list active local
administrator sessions, mark the current bearer-token session, adjust session
expiry up to 168 hours, and revoke one session at a time.

Administrator accounts have an `admin_scope` of `global` or `tenant`. The first
administrator created by setup is global. Global administrators can register
DataSrv with Hub, pull Hub tenants, create tenant administrators, and list all
administrator accounts/sessions by default. Tenant administrators are limited to
their own tenant and cannot create or promote global administrators.

Hub registration stores the Hub base URL, platform identity, generated RSA key
pair, callback secret, and optional virtual mail domain. After a global
administrator saves the settings and calls the register endpoint, DataSrv signs
platform requests to Hub and can pull the Hub tenant registry. The login screen
can refresh tenant choices through `POST /api/v1/setup/tenants/sync`; that
public endpoint is rate-limited and only works after Hub registration is active.

Offline administrator recovery commands do not require `MACLAW_DATA_TOKEN` and
do not start the HTTP service. The target SQLite database file must already
exist, so a wrong `-db` path fails instead of creating a new empty database.

```powershell
maclaw-data-srv admin list -db D:\data\maclaw\data.db
maclaw-data-srv admin list -db D:\data\maclaw\data.db -tenant all -json
maclaw-data-srv admin reset-password -db D:\data\maclaw\data.db -username admin
maclaw-data-srv admin reset-password -db D:\data\maclaw\data.db -username admin -json
```

When `reset-password` omits `-password`, the command generates a temporary
password and prints it once. When `-password` is provided, the password is
hashed with bcrypt and is not echoed to stdout. Password reset revokes existing
sessions for that administrator. After reset, sign in with the new password,
issue fresh API keys if needed, and retire any copied temporary passwords.

## Phase 2 Backlog

The structured data storage engine phase 1 is complete. Phase 2 has started.
Current progress:

- Backend multi-administrator management API: create, list, display-name/role
  update, enable/disable, last-enabled-admin protection, and session revocation
  on disable.
- Web Console multi-administrator management, including create, disable,
  display-name update, and role review.
- Session management APIs and UI for listing active administrator sessions,
  marking the current session, updating session expiry, and revoking sessions.
- Audit hardening for administrator setup, login, password reset, account
  changes, and session revoke.
- Optional local administrator failed-login lockout policy, controlled by
  environment variables.
- Administrator password policy discovery through `GET /api/v1/setup/status`,
  the Web Console setup panel, OpenAPI, and CLI help text.
- Production operations guide covering service environment variables, database
  location, reverse proxy/TLS deployment, backup verification, restore
  checklist, maintenance, and offline admin commands.
- SQLite performance guardrails and benchmarks for record query/index paths,
  FTS/tag queries, audit cursor pagination, and API key/session authorization
  lookups.
- Governance evidence performance benchmark with an operational fixture covering
  managed API keys, connector health, backups, and audit history.
- Import/export job history performance benchmarks for indexed dataset/status
  pagination under larger job volumes.
- Audit review polish for API key rotation, backup download/restore
  operations, and governance evidence exports.
- HTTP end-to-end coverage for first-time administrator setup, administrator
  login, session-token data browsing, Chinese evidence summary output, and
  password reset recovery handoff.
- SQLite restore drill coverage for the pre-restore rollback snapshot, plus
  operational rollback steps in the production guide.
- HTTP concurrent query benchmark covering full routing, bearer authentication,
  JSON handling, and SQLite-backed record search.
- HTTP write/export benchmarks covering concurrent batch import and JSONL export
  through the full API stack.
- Web Console interaction wiring tests for administrator initialization,
  administrator login, token persistence, password-field cleanup, language
  switching, and localized governance evidence refresh.
- Web Console responsive layout and first-screen contract guards for desktop
  grid structure, mobile collapse behavior, administrator setup, language
  switch, operational health, and governance evidence anchors.
- Browser-driven Web Console screenshot regression using real Chromium rendering
  across desktop and mobile viewport sizes, with PNG pixel checks for nonblank,
  non-monochrome, navigable console output.
- HTTP recovery and long-running job workflow benchmark covering backup create,
  maintenance, async JSONL import job polling, async JSONL export job polling,
  export download, and backup download through the full authenticated API stack.

Remaining work:

- None tracked for phase 2 in this module. Future work should be opened as a
  new phase with an explicit scope.
