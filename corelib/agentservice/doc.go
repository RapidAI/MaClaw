// Package agentservice provides a multi-tenant service layer for exposing
// Maclaw agent capabilities to external programs.
//
// Design rules:
//  1. All core business logic stays in corelib.
//  2. Transport adapters (REST, gRPC, IM, etc.) remain thin wrappers.
//  3. Data is isolated by tenant, user, and instance.
//  4. Each instance maps to a full Maclaw runtime with its own DataDir.
package agentservice
