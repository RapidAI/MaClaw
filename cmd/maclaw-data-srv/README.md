# maclaw-data-srv

`cmd/maclaw-data-srv` is the executable entry point for MaClawDataSrv.

It owns process concerns only:

- Environment variable parsing.
- Loopback listen address validation.
- Store, service, and HTTP server construction through `datasrv/structureddata`.
- Graceful shutdown.

Concrete structured data implementation belongs in `datasrv/structureddata`.
Shared request and response contracts belong in `corelib/structureddata`.

Package boundary reference: [`docs/datasrv-structureddata-boundary.md`](../../docs/datasrv-structureddata-boundary.md).

## Configuration

The command reads these environment variables:

- `MACLAW_DATA_TOKEN`: optional service bearer token. When set, it must be at least 24 characters. Without it, use first-time administrator setup and login to obtain a temporary bearer token.
- `MACLAW_DATA_HTTP_ADDR`: optional loopback listen address, default `127.0.0.1:18180`.
- `MACLAW_DATA_SQLITE_PATH`: optional explicit SQLite database path.
- `MACLAW_DATA_ROOT`: optional data root used when `MACLAW_DATA_SQLITE_PATH` is not set.
- `MACLAW_DATA_API_KEYS`: optional managed API key policy definitions parsed by `datasrv/structureddata`.

`MACLAW_DATA_API_KEYS` is a JSON array. Each entry with a non-empty `key` becomes a
scoped bearer credential:

```json
[
  {
    "id": "agent-sales-read",
    "key": "replace-with-at-least-24-characters",
    "tenant_id": "tenant_1",
    "user_id": "agent_sales",
    "role": "data_auditor",
    "allowed_domains": ["sales"],
    "allowed_views": ["sales.pipeline"],
    "allowed_reports": ["sales.forecast"]
  }
]
```

Plain HTTP is intentionally loopback-only. Put TLS, public ingress, and external network
policy in a reverse proxy or service mesh in front of this process.

First-time administrator setup is available from the embedded Web Console at `/`
or `/ui`, and through the setup/login HTTP endpoints implemented by
`datasrv/structureddata`.

Run `maclaw-data-srv --help` to print service usage without starting the
process. Offline administrator recovery commands live in the independent
`datasrv` module:

```powershell
cd datasrv
go run ./cmd/maclaw-data-srv admin --help
```
