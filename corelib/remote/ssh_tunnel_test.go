package remote

import (
	"context"
	"strings"
	"testing"
)

func TestDialThroughSSHSessionRequiresLiveSession(t *testing.T) {
	_, err := DialThroughSSHSession(context.Background(), nil, "ssh-1", "tcp", "127.0.0.1:3306")
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("nil lookup err = %v", err)
	}
	mgr := NewSSHSessionManager(nil)
	_, err = DialThroughSSHSession(context.Background(), func() *SSHSessionManager { return mgr }, "missing", "tcp", "127.0.0.1:3306")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing session err = %v", err)
	}
}
