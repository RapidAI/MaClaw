// Package structureddata contains the MaClawDataSrv structured storage engine
// implementation.
//
// This package owns the concrete SQLite store, service orchestration, HTTP API,
// OpenAPI document, and Web Console assets. Shared request and response DTOs
// live in github.com/RapidAI/CodeClaw/corelib/structureddata and are aliased
// into this package for implementation-local ergonomics.
//
// New service hosts should construct MaClawDataSrv through this package's
// narrow exported surface: NewSQLiteStore, NewService, NewHTTPServer,
// NewHTTPServerWithAPIKeys, and ParseAPIKeyPolicies.
package structureddata
