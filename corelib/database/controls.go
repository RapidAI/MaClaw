package database

import (
	"fmt"
	"strings"
	"sync/atomic"
)

// CanonicalToolSchemaHash is the locked snapshot of ToolParameters(). Hosts
// disable the database tool when ToolSchemaHash() differs so mixed GUI/TUI/srv
// binaries cannot execute the same call with different fields.
//
// Update this constant (and this comment) in the same change that edits the
// model-facing schema.
const CanonicalToolSchemaHash = "sha256:336788cd04deee8b49739c535f3de0ec5d7b5a34a22a789ae169ef26cdf4c7a7"

// SchemaContractLocked reports whether this binary's tool schema matches the
// locked contract. A mismatch is a release-engineering error, not a runtime
// configuration problem.
func SchemaContractLocked() bool {
	return ToolSchemaHash() == CanonicalToolSchemaHash
}

// IssueGate is consulted by IssueApproval before a token is minted. Hosts
// bind it to PolicyEngine.Evaluate so a deny rule cannot be bypassed by the
// approval UI. Returning a non-nil error fail-closes issuance.
type IssueGate func(req ApprovalRequest) error

// MetricsSnapshot is the process-local, non-sensitive view of database tool
// activity used by doctor and admin diagnostics.
type MetricsSnapshot struct {
	ConnectAttempts int64 `json:"connect_attempts"`
	ConnectSuccess  int64 `json:"connect_success"`
	QueryCount      int64 `json:"query_count"`
	Truncations     int64 `json:"truncations"`
	Denials         int64 `json:"denials"`
	Timeouts        int64 `json:"timeouts"`
	ApprovalsIssued int64 `json:"approvals_issued"`
	ApprovalsDenied int64 `json:"approvals_denied"`
}

type runtimeMetrics struct {
	connectAttempts atomic.Int64
	connectSuccess  atomic.Int64
	queryCount      atomic.Int64
	truncations     atomic.Int64
	denials         atomic.Int64
	timeouts        atomic.Int64
	approvalsIssued atomic.Int64
	approvalsDenied atomic.Int64
}

func (m *runtimeMetrics) snapshot() MetricsSnapshot {
	if m == nil {
		return MetricsSnapshot{}
	}
	return MetricsSnapshot{
		ConnectAttempts: m.connectAttempts.Load(),
		ConnectSuccess:  m.connectSuccess.Load(),
		QueryCount:      m.queryCount.Load(),
		Truncations:     m.truncations.Load(),
		Denials:         m.denials.Load(),
		Timeouts:        m.timeouts.Load(),
		ApprovalsIssued: m.approvalsIssued.Load(),
		ApprovalsDenied: m.approvalsDenied.Load(),
	}
}

// SetEnabled is the process kill switch. Disabling immediately closes live
// connections and rejects new requests; the manager itself stays open so a
// later enable can reuse the same profile set.
func (m *Manager) SetEnabled(enabled bool) {
	if m == nil {
		return
	}
	var closeAdapters []Adapter
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.enabled = enabled
	if !enabled {
		closeAdapters = m.takeLiveAdaptersLocked()
	}
	m.mu.Unlock()
	for _, adapter := range closeAdapters {
		_ = adapter.Close()
	}
}

func (m *Manager) takeLiveAdaptersLocked() []Adapter {
	items := make([]Adapter, 0, len(m.items))
	for id, adapter := range m.items {
		items = append(items, adapter)
		delete(m.items, id)
		delete(m.lastUsed, id)
		delete(m.bindings, id)
		delete(m.profileIDs, id)
	}
	for token := range m.results {
		delete(m.results, token)
	}
	return items
}

// ToolEnabled reports whether the manager will accept new requests. A schema
// lock mismatch or kill switch disables the tool without closing the manager.
func (m *Manager) ToolEnabled() bool {
	if m == nil || m.IsClosed() {
		return false
	}
	if !SchemaContractLocked() {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enabled && m.migrationError == ""
}

func (m *Manager) disabledReason() string {
	if m == nil {
		return "数据库连接工具未初始化。请先配置数据源 profile。"
	}
	if !SchemaContractLocked() {
		return "database tool disabled: schema hash mismatch; upgrade every host to the same contract"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.migrationError != "" {
		return "database tool disabled: " + m.migrationError
	}
	if !m.enabled {
		return "database tool disabled: database_tool_enabled is off"
	}
	return ""
}

// SetIssueGate installs the PolicyEngine-backed issuance gate. Passing nil
// restores issuance without an extra policy check (fingerprint binding remains).
func (m *Manager) SetIssueGate(gate IssueGate) {
	if m == nil {
		return
	}
	m.mu.Lock()
	if !m.closed {
		m.issueGate = gate
	}
	m.mu.Unlock()
}

// SnapshotMetrics returns a copy of the current counters.
func (m *Manager) SnapshotMetrics() MetricsSnapshot {
	if m == nil {
		return MetricsSnapshot{}
	}
	return m.metrics.snapshot()
}

func (m *Manager) noteDenial() {
	if m != nil {
		m.metrics.denials.Add(1)
	}
}

func (m *Manager) noteTimeout() {
	if m != nil {
		m.metrics.timeouts.Add(1)
	}
}

func disableError(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "database tool is disabled"
	}
	return fmt.Sprintf("permission: %s", reason)
}
