package remote

import (
	"context"
	"fmt"
	"net"
	"strings"
)

// SSHSessionLookup returns the host SSH session manager. It may be called
// from a database tunnel dial at connect time, not during manager setup.
type SSHSessionLookup func() *SSHSessionManager

// SSHTunnelDialer adapts a live, already-approved SSH session into the
// database package's TunnelDialer. The database tool never opens SSH
// connections or sees SSH credentials.
func SSHTunnelDialer(lookup SSHSessionLookup) func(ctx context.Context, sessionID, network, address string) (net.Conn, error) {
	return func(ctx context.Context, sessionID, network, address string) (net.Conn, error) {
		return DialThroughSSHSession(ctx, lookup, sessionID, network, address)
	}
}

// DialThroughSSHSession opens a TCP connection via an existing SSH session's
// client. The session must already be running; this path does not create,
// approve, or authenticate SSH.
func DialThroughSSHSession(ctx context.Context, lookup SSHSessionLookup, sessionID, network, address string) (net.Conn, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("permission: ssh_session_id is required")
	}
	if lookup == nil {
		return nil, fmt.Errorf("permission: ssh tunnel is not configured")
	}
	mgr := lookup()
	if mgr == nil {
		return nil, fmt.Errorf("permission: ssh tunnel is not configured")
	}
	sess, ok := mgr.Get(sessionID)
	if !ok || sess == nil {
		return nil, fmt.Errorf("connection: ssh session not found")
	}
	if sess.Status == SessionExited || sess.Status == SessionError {
		return nil, fmt.Errorf("permission: ssh session is not running")
	}
	if sess.Handle == nil {
		return nil, fmt.Errorf("connection: ssh session has no client")
	}
	client := sess.Handle.Client()
	if client == nil {
		return nil, fmt.Errorf("connection: ssh session has no client")
	}
	if network == "" {
		network = "tcp"
	}
	if ctx == nil {
		ctx = context.Background()
	}
	conn, err := client.DialContext(ctx, network, address)
	if err != nil {
		return nil, fmt.Errorf("connection: ssh tunnel dial: %w", err)
	}
	return conn, nil
}
