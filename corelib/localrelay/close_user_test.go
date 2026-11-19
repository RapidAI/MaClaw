package localrelay

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestBrokerCloseUserRevokesLinkAndGrants(t *testing.T) {
	broker := &Broker{}
	server, client := net.Pipe()
	defer client.Close()
	done := make(chan error, 1)
	key := UserKey("tenant-a", "user-1")
	go func() { done <- broker.Attach(context.Background(), key, "office", server) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		online, _, _ := broker.Online(key)
		if online {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("relay link did not attach")
		}
		time.Sleep(5 * time.Millisecond)
	}
	grant := broker.Mint(Grant{Key: key, Kind: KindSocks})

	broker.CloseUser(key)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("relay link remained open after user revocation")
	}
	if online, _, _ := broker.Online(key); online {
		t.Fatal("relay link remained online after user revocation")
	}
	if _, err := broker.use(grant.Token); CodeOf(err) != CodeExpired {
		t.Fatalf("revoked grant error = %v, want CodeExpired", err)
	}
}

func TestBrokerCloseTenantRevokesGrants(t *testing.T) {
	broker := &Broker{}
	key := UserKey("tenant-a", "user-1")
	grant := broker.Mint(Grant{Key: key, Kind: KindSocks})
	broker.CloseTenant("tenant-a")
	if _, err := broker.use(grant.Token); CodeOf(err) != CodeExpired {
		t.Fatalf("tenant revoked grant error = %v, want CodeExpired", err)
	}
}
