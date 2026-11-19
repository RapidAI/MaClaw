package remote

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestSSHPoolConcurrentAcquireSharesDialAndReferences(t *testing.T) {
	signer := testSSHSigner(t)
	var dials int32
	address := startTestPasswordSSHServerCounted(t, signer, "deploy", "correct-password", &dials)
	host, port := testSSHHostPort(t, address)
	cfg := SSHHostConfig{
		Host: host, User: "deploy", Port: port, Password: "correct-password",
		AuthMethod: "password", ConnectTimeout: 2 * time.Second,
	}
	pool := NewSSHPool()
	t.Cleanup(pool.CloseAll)

	const callers = 16
	start := make(chan struct{})
	clients := make(chan *ssh.Client, callers)
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			client, err := pool.Acquire(cfg)
			if err != nil {
				errs <- err
				return
			}
			clients <- client
		}()
	}
	close(start)
	wg.Wait()
	close(clients)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := atomic.LoadInt32(&dials); got != 1 {
		t.Fatalf("concurrent acquire dialed %d connections, want 1", got)
	}
	pool.mu.Lock()
	entry := pool.conns[sshPoolKey(cfg)]
	pool.mu.Unlock()
	if entry == nil || entry.refCount != callers {
		t.Fatalf("pool entry = %#v, want %d references", entry, callers)
	}
	var first *ssh.Client
	for client := range clients {
		if first == nil {
			first = client
		}
		if client != first {
			t.Fatal("concurrent acquire returned different clients")
		}
		pool.Release(cfg, client)
	}
	if got := pool.Stats(); len(got) != 0 {
		t.Fatalf("pool retained entries after every release: %#v", got)
	}
}
