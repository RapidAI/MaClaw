package database

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestConnectSSHTunnelRequiresDialer(t *testing.T) {
	m := NewManager([]Profile{{
		ID:           "crm",
		Type:         SourcePostgres,
		Host:         "127.0.0.1",
		Port:         5432,
		Database:     "crm",
		SSHSessionID: "ssh-1",
	}}, nil)
	defer m.Close()
	_, _, err := m.Connect(context.Background(), "crm")
	if err == nil || !strings.Contains(err.Error(), "ssh tunnel is not configured") {
		t.Fatalf("err = %v", err)
	}
}

func TestConnectSSHTunnelRejectedForExcel(t *testing.T) {
	err := ValidateProfile(Profile{ID: "x", Type: SourceExcel, FilePath: "a.xlsx", SSHSessionID: "ssh-1"})
	if err == nil || !strings.Contains(err.Error(), "unsupported_capability") {
		t.Fatalf("err = %v", err)
	}
}

func TestAuthorizeProfileEndpointSkipsLocalDNSForTunnel(t *testing.T) {
	original := lookupIP
	t.Cleanup(func() { lookupIP = original })
	lookupIP = func(host string) ([]net.IP, error) {
		t.Fatalf("tunneled profile must not resolve %s locally", host)
		return nil, nil
	}
	p := Profile{Type: SourceMySQL, Host: "db.internal", Port: 3306, SSHSessionID: "ssh-1"}
	if err := authorizeProfileEndpoint(p); err != nil {
		t.Fatal(err)
	}
}

func TestConnectSSHTunnelUsesInjectedDialer(t *testing.T) {
	var gotSession, gotNetwork, gotAddr string
	m := NewManager([]Profile{{
		ID:           "crm",
		Type:         SourceMySQL,
		Host:         "10.0.0.9",
		Port:         3306,
		Database:     "crm",
		SSHSessionID: "ssh-live",
	}}, nil)
	defer m.Close()
	m.SetTunnelDialer(func(ctx context.Context, sessionID, network, address string) (net.Conn, error) {
		gotSession, gotNetwork, gotAddr = sessionID, network, address
		return nil, context.Canceled
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _, err := m.Connect(ctx, "crm")
	if err == nil {
		t.Fatal("expected tunnel dial to fail closed")
	}
	if gotSession != "ssh-live" || gotNetwork != "tcp" || !strings.Contains(gotAddr, "10.0.0.9") {
		t.Fatalf("dial args session=%s net=%s addr=%s", gotSession, gotNetwork, gotAddr)
	}
}

func TestHandleToolRejectsSSHSessionIDArgument(t *testing.T) {
	m := NewManager(nil, nil)
	got := HandleTool(context.Background(), m, map[string]interface{}{
		"action": "connect", "profile_id": "crm", "ssh_session_id": "ssh-1",
	})
	if !strings.Contains(got, "ssh_session_id must be configured on the profile") {
		t.Fatalf("got %s", got)
	}
}
