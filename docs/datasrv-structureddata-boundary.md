# MaClawDataSrv Package Boundary

This document records the current package split for MaClawDataSrv.

## Packages

### `corelib/structureddata`

Contract-only package for callers.

Allowed contents:

- Exported request and response DTOs.
- Query input structs.
- Response envelope structs.
- Struct-only access contracts with explicit JSON field tags.

Disallowed contents:

- Service constructors.
- Store implementations.
- HTTP server implementation.
- OpenAPI or Web Console implementation.
- SQLite migrations or database logic.
- Exported functions, variables, or constants.
- Behavioral interfaces or implementation aliases.

The package must not import `datasrv/structureddata`.

### `datasrv/structureddata`

Concrete MaClawDataSrv implementation package.

The parent `datasrv/` directory is an independently buildable Go module with
module path `github.com/RapidAI/CodeClaw/datasrv`. It owns its own `go.mod`,
`go.sum`, and `cmd/maclaw-data-srv` executable entry point. In-repo development
uses `replace github.com/RapidAI/CodeClaw => ..` so this module compiles against
the local `corelib/structureddata` contracts.

Owned contents:

- `Store`, `Service`, `SQLiteStore`, and `HTTPServer`.
- SQLite schema, migrations, and query implementation.
- Service orchestration and governance workflows.
- HTTP handlers, OpenAPI, and Web Console assets.
- Implementation tests.

Exported construction surface:

- `NewSQLiteStore`
- `NewService`
- `NewHTTPServer`
- `NewHTTPServerWithAPIKeys`
- `ParseAPIKeyPolicies`

Exported package state is limited to sentinel error values such as
`ErrUnauthorized`, `ErrForbidden`, and `ErrInvalidInput`. Every exported sentinel
error must be handled by `httpStatusForError`.

All exported DTOs should be defined in `corelib/structureddata` and aliased through
`datasrv/structureddata/*_alias.go` files. Implementation files should use those local
aliases instead of importing `corelib/structureddata` directly.

### `cmd/maclaw-data-srv`

Executable entry point.

Responsibilities:

- Read process configuration and environment variables.
- Construct `SQLiteStore`, `Service`, and `HTTPServer` from `datasrv/structureddata`.
- Own process lifecycle and graceful shutdown.

It must import `datasrv/structureddata` directly for service construction and must not
construct the service through `corelib/structureddata`.

## Guardrails

The boundary is enforced by architecture tests in:

- `corelib/structureddata/architecture_test.go`
- `datasrv/structureddata/architecture_test.go`
- `cmd/maclaw-data-srv/architecture_test.go`

Additional implementation guardrails:

- Every HTTP API route registered in `datasrv/structureddata/http.go` must be
  documented by `datasrv/structureddata/openapi.go`.
- Every OpenAPI path/method entry must map back to a registered HTTP API route.
- Every `/api/v1/data/...` HTTP route must be registered through
  `HTTPServer.withAuth`.
- Every `/api/v1/data/...` OpenAPI operation must document `401` and `403`
  JSON error responses.
- `401` responses should include `WWW-Authenticate: Bearer realm="MaClawDataSrv"`
  and the OpenAPI schema should document that header.
- JSON API responses should include `X-Content-Type-Options: nosniff`; OpenAPI
  error response schemas should document that header.
- Download responses should set `Content-Type`, `Content-Disposition`, and
  `X-Content-Type-Options: nosniff` through the shared `writeDownloadHeaders`
  helper.
- Download operations in OpenAPI should document the `Content-Disposition` and
  `X-Content-Type-Options` success response headers.
- Download operations in OpenAPI should document their success response media
  types through the shared `downloadOpenAPIMetadataByRoute` table.
- Backup download operations should document the `X-MaClaw-Backup-SHA256`
  success response header for byte integrity checks.
- Cursor-paginated data responses that expose `next_before` should also expose
  `next_before_id`; Web Console load-more actions should require both cursor
  parts before requesting the next page.
- `docs/README.md` links to this boundary record so package split decisions are
  discoverable from the documentation index.
