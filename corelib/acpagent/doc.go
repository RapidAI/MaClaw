// Package acpagent implements a minimal Agent Client Protocol (ACP) Agent
// surface for MaClaw, focused on the VS Code ↔ GUI bridge path.
//
// Naming: this is the industry Agent Client Protocol (agentclientprotocol.com),
// NOT the proprietary iFlow ACP WebSocket protocol used in gui/remote_execution_iflow.go.
//
// Transport: JSON-RPC 2.0 over NDJSON (one message per line on stdio).
// Bridge backend (v1): MaClaw GUI third-party IM Gateway (loopback HTTP).
package acpagent
