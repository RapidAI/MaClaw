package database

import (
	"context"
	"fmt"
	"net"
	"strings"
)

// TunnelDialer dials a TCP address through an already-approved SSH session.
// Hosts inject this so the database package never sees SSH credentials or
// opens its own SSH connections. sessionID is the host-side profile binding.
type TunnelDialer func(ctx context.Context, sessionID, network, address string) (net.Conn, error)

// SetTunnelDialer installs the host SSH-session dialer. Passing nil disables
// tunneled profiles (connect then fail-closes with permission).
func (m *Manager) SetTunnelDialer(dial TunnelDialer) {
	if m == nil {
		return
	}
	m.mu.Lock()
	if !m.closed {
		m.tunnel = dial
	}
	m.mu.Unlock()
}

func (m *Manager) tunnelDialer() TunnelDialer {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tunnel
}

func profileUsesSSHTunnel(p Profile) bool {
	return strings.TrimSpace(p.SSHSessionID) != ""
}

func requireTunnelDialer(p Profile, dial TunnelDialer) error {
	if !profileUsesSSHTunnel(p) {
		return nil
	}
	switch p.Type {
	case SourceMySQL, SourcePostgres, SourceSQLServer:
	default:
		return fmt.Errorf("unsupported_capability: ssh tunnel is only supported for mysql, postgres and sqlserver")
	}
	if dial == nil {
		return fmt.Errorf("permission: ssh tunnel is not configured")
	}
	return nil
}
