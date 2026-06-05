# MaClawDataSrv Production Operations Guide

This guide records the operator workflow for running MaClawDataSrv in a
production-like environment.

## Service Configuration

Required decisions before starting the service:

- `MACLAW_DATA_SQLITE_PATH`: explicit SQLite database file path. Prefer a
  durable volume such as `D:\data\maclaw\data.db`.
- `MACLAW_DATA_ROOT`: fallback data directory when `MACLAW_DATA_SQLITE_PATH` is
  not set. The service stores `data.db` under this root.
- `MACLAW_DATA_HTTP_ADDR`: listen address. Default is `127.0.0.1:18180`. Keep
  plain HTTP on loopback and place TLS at a reverse proxy.
- `MACLAW_DATA_TOKEN`: optional static service bearer token. When set, it must
  be at least 24 characters. Local administrator login can be used without it.
- `MACLAW_DATA_API_KEYS`: optional JSON array of static scoped API key policies.
- `MACLAW_DATA_ADMIN_PASSWORD_MIN_LENGTH`: local administrator password minimum
  length. Default is 8, clamped to 8-128.
- `MACLAW_DATA_ADMIN_LOGIN_MAX_FAILURES`: optional failed-login lockout
  threshold. Default is 0, which disables lockout.
- `MACLAW_DATA_ADMIN_LOGIN_LOCKOUT_MINUTES`: lockout duration when the failed
  login threshold is enabled. Default is 15 minutes, clamped to 1-1440.
  Failed-login counters and active lockout deadlines are persisted in SQLite, so
  service restarts do not clear an active lockout.

Example PowerShell startup:

```powershell
$env:MACLAW_DATA_SQLITE_PATH = "D:\data\maclaw\data.db"
$env:MACLAW_DATA_HTTP_ADDR = "127.0.0.1:18180"
$env:MACLAW_DATA_ADMIN_PASSWORD_MIN_LENGTH = "12"
$env:MACLAW_DATA_ADMIN_LOGIN_MAX_FAILURES = "5"
$env:MACLAW_DATA_ADMIN_LOGIN_LOCKOUT_MINUTES = "15"
.\maclaw-data-srv.exe
```

## Network Deployment

Run the Go service on loopback by default:

```powershell
$env:MACLAW_DATA_HTTP_ADDR = "127.0.0.1:18180"
```

Terminate TLS and expose public traffic through a reverse proxy such as nginx,
Caddy, IIS ARR, or a platform gateway. The proxy should forward:

- `/` and `/ui` for the Web Console.
- `/api/v1/*` for JSON APIs.
- `Authorization`, `X-MaClaw-Tenant-ID`, `X-MaClaw-User-ID`, and
  `X-MaClaw-Role` headers when trusted upstream service tokens are used.
- `X-MaClaw-Admin-Scope` only for trusted static service-token calls that need
  administrator scope. Use `global` for cross-tenant operations such as Hub
  registration, otherwise omit it or use `tenant`.

Recommended proxy behavior:

- Enforce HTTPS.
- Limit request body size to the service default or lower when possible.
- Preserve `X-Content-Type-Options` and download headers.
- Restrict direct access to `127.0.0.1:18180` from outside the host.

## First Administrator

Initialize the first administrator from the Web Console or API:

```powershell
Invoke-RestMethod `
  -Method Post `
  -Uri http://127.0.0.1:18180/api/v1/setup/admin `
  -ContentType application/json `
  -Body '{"username":"admin","password":"change-me-strong","display_name":"Primary Administrator"}'
```

Check the active password policy before setup or recovery:

```powershell
Invoke-RestMethod http://127.0.0.1:18180/api/v1/setup/status
```

The response includes `password_policy.min_length`, lockout settings, and
`offline_reset_available`. The Web Console displays the same policy in the
first-time administrator panel.

If the password is forgotten, use the offline command against the existing
database file. The command does not require `MACLAW_DATA_TOKEN` and refuses to
create a missing database.

```powershell
.\maclaw-data-srv.exe admin list -db D:\data\maclaw\data.db
.\maclaw-data-srv.exe admin reset-password -db D:\data\maclaw\data.db -username admin
```

If `reset-password` omits `-password`, it generates a temporary password and
prints it once. A provided `-password` is hashed with bcrypt and is not echoed.
Password reset revokes active sessions for that administrator. After reset,
sign in with the new password, issue fresh API keys if needed, and retire any
copied temporary passwords.

## Hub Registration And Tenants

The first administrator created by setup is a global administrator. Global
administrators can register DataSrv with Hub, pull the Hub tenant registry, and
create tenant administrators. Tenant administrators are limited to their own
tenant and cannot save Hub registration settings or promote accounts to global
scope.

Register DataSrv with Hub from the Web Console access area, or through the API:

```powershell
$token = "<global administrator bearer token>"
$headers = @{ Authorization = "Bearer $token" }

Invoke-RestMethod `
  -Method Post `
  -Uri http://127.0.0.1:18180/api/v1/data/admin/hub-registration `
  -Headers $headers `
  -ContentType application/json `
  -Body '{"hub_base_url":"http://127.0.0.1:18181","platform_id":"datasrv","platform_name":"MaClawDataSrv","callback_base_url":"http://127.0.0.1:18180","virtual_mail_domain":"datasrv.local"}'

Invoke-RestMethod `
  -Method Post `
  -Uri http://127.0.0.1:18180/api/v1/data/admin/hub-registration/register `
  -Headers $headers `
  -ContentType application/json `
  -Body '{}'
```

After registration, pull tenants from Hub:

```powershell
Invoke-RestMethod `
  -Method Post `
  -Uri http://127.0.0.1:18180/api/v1/data/admin/hub-registration/sync-tenants `
  -Headers $headers `
  -ContentType application/json `
  -Body '{}'
```

The login screen can refresh tenant choices with
`POST /api/v1/setup/tenants/sync`. That endpoint is public so the login UI can
work before a user has a token, but it is rate-limited and only succeeds after
Hub registration is active. The request to Hub is still signed by DataSrv using
the registered platform key.

Operational checks:

- Keep Hub and DataSrv base URLs on loopback or behind trusted TLS gateways.
- Record `platform_id`, Hub URL, callback base URL, and virtual mail domain in
  deployment notes.
- Review `/api/v1/setup/status` after registration. It should include
  `hub_registration.registered=true` and synced tenant entries.
- Create tenant administrators only after the tenant appears in
  `/api/v1/data/admin/tenants`.
- Use `GET /api/v1/data/admin/accounts?tenant=all` and
  `GET /api/v1/data/admin/sessions?tenant=all` only from global administrator
  sessions during audits.

## Backup Checklist

Use the built-in backup API before risky imports, schema changes, bulk updates,
bulk deletes, or restore operations.

Create and download a backup:

```powershell
$token = "<administrator bearer token>"
$headers = @{ Authorization = "Bearer $token" }
$backup = Invoke-RestMethod `
  -Method Post `
  -Uri http://127.0.0.1:18180/api/v1/data/backups `
  -Headers $headers `
  -ContentType application/json `
  -Body '{"name":"pre-change checkpoint","note":"before production change"}'

Invoke-WebRequest `
  -Uri "http://127.0.0.1:18180$($backup.download_url)" `
  -Headers $headers `
  -OutFile D:\data\maclaw\backups\$($backup.id).sqlite
```

Verify the downloaded file hash before archiving:

```powershell
(Get-FileHash D:\data\maclaw\backups\$($backup.id).sqlite -Algorithm SHA256).Hash.ToLowerInvariant()
$backup.sha256
```

Operator checklist:

- Confirm `backup.id`, `backup.size_bytes`, and `backup.sha256` are present.
- Confirm the downloaded SHA-256 equals `backup.sha256`.
- Store a copy outside the active database directory.
- Record the change reason and backup ID in the deployment notes.
- Run `POST /api/v1/data/maintenance/run` with `integrity_check` after major
  changes.

## Restore Checklist

Restore is intentionally explicit and requires `confirm=true`.

Before restore:

- Stop write-heavy clients and scheduled connector syncs.
- Create or retain a fresh backup of the current state.
- Verify the backup metadata and downloaded SHA-256.
- Confirm the restore target `backup_id`.
- Record the operator, reason, expected data impact, and rollback plan.

Restore through the API:

```powershell
Invoke-RestMethod `
  -Method Post `
  -Uri "http://127.0.0.1:18180/api/v1/data/backups/$backupId/restore" `
  -Headers $headers `
  -ContentType application/json `
  -Body '{"confirm":true,"reason":"restore verified checkpoint"}'
```

After restore:

- Run maintenance with `integrity_check`.
- Check `/readyz` and `/api/v1/data/stats`.
- Review recent audit logs with `/api/v1/data/audit`.
- Re-run any connector sync or import jobs only after confirming the restored
  state.

Rollback drill:

- The SQLite restore path renames the pre-restore database to
  `<database>.before-restore-<timestamp>` before copying the selected backup into
  place.
- During a quarterly recovery drill, record the rollback snapshot path, verify
  the file is non-empty, and keep it until the restored state is accepted.
- If the selected backup is wrong, stop the service, move the active database to
  a quarantine path, move the `.before-restore-*` snapshot back to the configured
  database path, start the service, and run `integrity_check`.
- Archive the restore audit row and the rollback snapshot decision together.

## Maintenance

Run maintenance from a `data_admin` bearer token:

```powershell
Invoke-RestMethod `
  -Method Post `
  -Uri http://127.0.0.1:18180/api/v1/data/maintenance/run `
  -Headers $headers `
  -ContentType application/json `
  -Body '{"tasks":["integrity_check","optimize"]}'
```

Use `vacuum` only during a maintenance window:

```powershell
Invoke-RestMethod `
  -Method Post `
  -Uri http://127.0.0.1:18180/api/v1/data/maintenance/run `
  -Headers $headers `
  -ContentType application/json `
  -Body '{"tasks":["integrity_check","vacuum","optimize"]}'
```

## Evidence and Audit Handoff

For release or incident evidence:

- Export governance evidence with `/api/v1/data/governance/evidence-pack`.
- Download `/api/v1/data/governance/evidence-summary.txt` for the rollout note.
- Export filtered audit CSV with `/api/v1/data/audit/export.csv`.
- Include backup IDs, SHA-256 hashes, operator usernames, and session revocation
  notes in the handoff.
- For API key changes, include the audit `key_prefix`, role, enabled state,
  permission flags, and allowed scope counts. Never paste the full secret into
  incident notes or release records.
- For backup restore evidence, include the audit `backup_id`, `sha256`,
  `size_bytes`, `reason`, `status`, `restored_by`, and `restored_at` fields.
- For governance evidence exports, archive the audit `evidence_id`,
  `evidence_sha256`, `status`, `risk_level`, `failed_sections`, and
  `recommendation_count` fields with the exported pack.
