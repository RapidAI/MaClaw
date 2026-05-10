package main

import "strings"

type agentNetServiceBillingKind string

const (
	agentNetServiceBillingUnknown agentNetServiceBillingKind = ""
	agentNetServiceBillingFree    agentNetServiceBillingKind = "free"
	agentNetServiceBillingPerCall agentNetServiceBillingKind = "per_call"
	agentNetServiceBillingPerKB   agentNetServiceBillingKind = "per_kb"
)

func normalizeAgentNetServiceBillingKind(kind string) agentNetServiceBillingKind {
	switch agentNetServiceBillingKind(strings.ToLower(strings.TrimSpace(kind))) {
	case agentNetServiceBillingFree:
		return agentNetServiceBillingFree
	case agentNetServiceBillingPerCall:
		return agentNetServiceBillingPerCall
	case agentNetServiceBillingPerKB:
		return agentNetServiceBillingPerKB
	default:
		return agentNetServiceBillingUnknown
	}
}

func (kind agentNetServiceBillingKind) String() string {
	return string(kind)
}

func (kind agentNetServiceBillingKind) IsPaid() bool {
	return kind != agentNetServiceBillingUnknown && kind != agentNetServiceBillingFree
}

type agentNetServiceModeKind string

const (
	agentNetServiceModeUnknown      agentNetServiceModeKind = ""
	agentNetServiceModeRR           agentNetServiceModeKind = "rr"
	agentNetServiceModeServerStream agentNetServiceModeKind = "server-stream"
	agentNetServiceModeBidi         agentNetServiceModeKind = "bidi"
)

func normalizeAgentNetServiceModeKind(kind string) agentNetServiceModeKind {
	switch agentNetServiceModeKind(strings.ToLower(strings.TrimSpace(kind))) {
	case agentNetServiceModeRR:
		return agentNetServiceModeRR
	case agentNetServiceModeServerStream:
		return agentNetServiceModeServerStream
	case agentNetServiceModeBidi:
		return agentNetServiceModeBidi
	default:
		return agentNetServiceModeUnknown
	}
}

func (kind agentNetServiceModeKind) String() string {
	return string(kind)
}

func isAgentNetLocalHTTPServiceURLScheme(scheme string) bool {
	return strings.EqualFold(strings.TrimSpace(scheme), "http")
}

func isAgentNetLoopbackServiceHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}
