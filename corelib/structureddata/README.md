# Structured Data Contracts

Package boundary reference: [`docs/datasrv-structureddata-boundary.md`](../../docs/datasrv-structureddata-boundary.md).

`corelib/structureddata` is the caller-facing contract package for MaClawDataSrv.

This package should contain shared struct DTOs, query inputs, response envelopes, and other
JSON-tagged access surface definitions that application code can import without pulling in
the data service implementation.

Keep behavioral interfaces, concrete service, store, HTTP server, OpenAPI, Web Console, and
migration code in `datasrv/structureddata`. The architecture test in this package guards that
dependency direction.

Command packages and service hosts that need to construct MaClawDataSrv, including
`cmd/maclaw-data-srv`, should import `datasrv/structureddata` directly. This package is for
the shared access surface only.
