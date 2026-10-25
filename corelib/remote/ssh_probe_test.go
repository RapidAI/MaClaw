package remote

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestTransportFailedRejectsAnUnstartedSession(t *testing.T) {
	session := NewSSHPTYSession(nil, "host")
	if !session.TransportFailed() {
		t.Fatal("an unstarted session has no transport")
	}
	if session.IsAlive() {
		t.Fatal("an unstarted session is not alive")
	}
}

func TestTransportFailedAgreesWithAFinishedKeepalive(t *testing.T) {
	client := dialSilentSSHClient(t)
	session := NewSSHPTYSession(client, "deploy@127.0.0.1:22")
	session.started = true
	t.Cleanup(func() { sshProbes.Delete(client) })

	storeSSHProbe(client, &sharedSSHProbe{started: time.Now(), done: closedProbe(), ok: true})
	if session.TransportFailed() {
		t.Fatal("a finished keepalive is a live transport")
	}
	if !session.IsAlive() {
		t.Fatal("a finished keepalive is alive")
	}

	storeSSHProbe(client, &sharedSSHProbe{started: time.Now(), done: closedProbe(), ok: false})
	if !session.TransportFailed() {
		t.Fatal("a finished transport error drops the session")
	}
	if session.IsAlive() {
		t.Fatal("a finished transport error is not alive")
	}
}

func TestAwaitSSHProbeReturnsAStuckRequestWithoutAnotherWait(t *testing.T) {
	client := dialSilentSSHClient(t)
	stuck := &sharedSSHProbe{started: time.Now().Add(-sshProbeStuckAfter), done: make(chan struct{})}
	storeSSHProbe(client, stuck)
	t.Cleanup(func() { sshProbes.Delete(client) })

	start := time.Now()
	ok, finished, running, _ := awaitSSHProbe(client, sshProbeWait)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("stuck probe waited %s", elapsed)
	}
	if ok || finished || running < sshProbeStuckAfter {
		t.Fatalf("ok=%v finished=%v running=%s", ok, finished, running)
	}

	session := NewSSHPTYSession(client, "deploy@127.0.0.1:22")
	session.started = true
	if session.TransportFailed() {
		t.Fatal("a stuck keepalive has not finished, so the open session stays bound")
	}
	if session.IsAlive() {
		t.Fatal("a stuck keepalive is not alive for reuse")
	}
}

func TestTransportFailedKeepsASessionWhoseKeepaliveHasNotFinished(t *testing.T) {
	client := dialSilentSSHClient(t)
	session := NewSSHPTYSession(client, "deploy@127.0.0.1:22")
	session.started = true

	start := time.Now()
	var failed, alive bool
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		failed = session.TransportFailed()
	}()
	go func() {
		defer wg.Done()
		alive = session.IsAlive()
	}()
	wg.Wait()
	if elapsed := time.Since(start); elapsed < 4*time.Second {
		t.Fatalf("probe returned in %s, want the short wait to finish first", elapsed)
	}
	if failed {
		t.Fatal("an unfinished keepalive is not a failed transport")
	}
	if !alive {
		t.Fatal("an unfinished keepalive still belongs to this session")
	}
}

func storeSSHProbe(client *ssh.Client, probe *sharedSSHProbe) {
	sshProbes.Store(client, probe)
}

func closedProbe() chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}

func dialSilentSSHClient(t *testing.T) *ssh.Client {
	t.Helper()
	signer := testSSHSigner(t)
	var requests atomic.Int32
	address := startSilentGlobalRequestSSHServer(t, signer, "deploy", "correct-password", &requests)
	host, port := testSSHHostPort(t, address)
	client, err := dialSSH(SSHHostConfig{
		Host:           host,
		User:           "deploy",
		Port:           port,
		Password:       "correct-password",
		AuthMethod:     "password",
		ConnectTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("dialSSH() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestSSHProbeCountsAsFailureOnlyAfterTheRequestEndsOrSticks(t *testing.T) {
	if sshProbeCountsAsFailure(true, true, 0) {
		t.Fatal("a successful probe is not a dead connection")
	}
	if !sshProbeCountsAsFailure(true, false, time.Second) {
		t.Fatal("a finished transport error is a failure")
	}
	if sshProbeCountsAsFailure(false, false, sshProbeWait) {
		t.Fatal("an unfinished probe is the same request and is not another failure")
	}
	if !sshProbeCountsAsFailure(false, false, sshProbeStuckAfter) {
		t.Fatal("one request with no answer for the stuck interval is a dead connection")
	}
}

func TestAwaitSSHProbeUsesOneRequestForConcurrentCallers(t *testing.T) {
	signer := testSSHSigner(t)
	var requests atomic.Int32
	address := startSilentGlobalRequestSSHServer(t, signer, "deploy", "correct-password", &requests)
	host, port := testSSHHostPort(t, address)
	client, err := dialSSH(SSHHostConfig{
		Host:           host,
		User:           "deploy",
		Port:           port,
		Password:       "correct-password",
		AuthMethod:     "password",
		ConnectTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("dialSSH() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ok, finished, _, _ := awaitSSHProbe(client, 200*time.Millisecond)
			if finished || ok {
				t.Errorf("silent server probe finished=%v ok=%v", finished, ok)
			}
		}()
	}
	close(start)
	wg.Wait()
	time.Sleep(100 * time.Millisecond)
	if got := requests.Load(); got != 1 {
		t.Fatalf("global keepalive requests = %d, want 1", got)
	}

	_ = client.Close()
	deadline := time.Now().Add(sshProbeResultKeep + 2*time.Second)
	for time.Now().Before(deadline) {
		if _, loaded := sshProbes.Load(client); !loaded {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("shared probe was not removed after the connection closed")
}

func TestTransportFailureSticksToTheHandle(t *testing.T) {
	client := dialSilentSSHClient(t)
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	session := NewSSHPTYSession(client, "deploy@127.0.0.1:22")
	session.started = true
	if !session.TransportFailed() {
		t.Fatal("a closed connection finishes the keepalive with a transport error")
	}
	// A later successful keepalive on the same client must not revive this PTY.
	storeSSHProbe(client, &sharedSSHProbe{started: time.Now(), done: closedProbe(), ok: true})
	t.Cleanup(func() { sshProbes.Delete(client) })
	start := time.Now()
	if session.IsAlive() || !session.TransportFailed() || !session.ObservedTransportFailure() {
		t.Fatal("a finished transport error stays on this handle")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("sticky failure waited %s", elapsed)
	}
}

func TestSessionKeepaliveRecordsAStuckProbeWhenItFails(t *testing.T) {
	client := &ssh.Client{}
	probe := &sharedSSHProbe{
		started: time.Now().Add(-sshProbeStuckAfter),
		done:    make(chan struct{}),
	}
	storeSSHProbe(client, probe)
	t.Cleanup(func() { sshProbes.Delete(client) })

	session := NewSSHPTYSession(client, "deploy@127.0.0.1:22")
	session.started = true
	exited := make(chan struct{})
	go func() {
		session.sessionKeepalive(20 * time.Millisecond)
		close(exited)
	}()

	time.Sleep(200 * time.Millisecond)
	if session.ObservedTransportFailure() {
		t.Fatal("a stuck keepalive is not a finished transport error yet")
	}
	probe.ok = false
	close(probe.done)

	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("keepalive did not stop after the stuck probe failed")
	}
	sshProbes.Delete(client)
	if !session.ObservedTransportFailure() {
		t.Fatal("a stuck keepalive that ends in a transport error stays on the handle")
	}
	if session.IsAlive() || !session.TransportFailed() {
		t.Fatal("the recorded transport error is still a dead session")
	}
}

func TestSessionKeepaliveLeavesAStuckProbeUsableWhenItSucceeds(t *testing.T) {
	client := &ssh.Client{}
	probe := &sharedSSHProbe{
		started: time.Now().Add(-sshProbeStuckAfter),
		done:    make(chan struct{}),
	}
	storeSSHProbe(client, probe)
	t.Cleanup(func() { sshProbes.Delete(client) })

	session := NewSSHPTYSession(client, "deploy@127.0.0.1:22")
	session.started = true
	exited := make(chan struct{})
	go func() {
		session.sessionKeepalive(20 * time.Millisecond)
		close(exited)
	}()

	time.Sleep(200 * time.Millisecond)
	probe.ok = true
	close(probe.done)

	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("keepalive did not stop after the stuck probe succeeded")
	}
	sshProbes.Delete(client)
	if session.ObservedTransportFailure() {
		t.Fatal("a late keepalive success is not a transport failure")
	}
}

func TestSessionKeepaliveStopUnblocksAStuckProbe(t *testing.T) {
	client := &ssh.Client{}
	probe := &sharedSSHProbe{
		started: time.Now().Add(-sshProbeStuckAfter),
		done:    make(chan struct{}),
	}
	storeSSHProbe(client, probe)
	t.Cleanup(func() { sshProbes.Delete(client) })

	session := NewSSHPTYSession(client, "deploy@127.0.0.1:22")
	session.started = true
	exited := make(chan struct{})
	go func() {
		session.sessionKeepalive(20 * time.Millisecond)
		close(exited)
	}()

	time.Sleep(200 * time.Millisecond)
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("closing the session left the keepalive waiting on the stuck probe")
	}
	if session.ObservedTransportFailure() {
		t.Fatal("closing the session is not a finished transport error")
	}
}

func TestSessionKeepaliveRecordsAFinishedTransportError(t *testing.T) {
	client := dialSilentSSHClient(t)
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	session := NewSSHPTYSession(client, "deploy@127.0.0.1:22")
	session.started = true
	done := make(chan struct{})
	go func() {
		session.sessionKeepalive(20 * time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("keepalive did not stop after the transport error")
	}
	sshProbes.Delete(client)
	if session.IsAlive() || !session.ObservedTransportFailure() || !session.TransportFailed() {
		t.Fatal("keepalive left the handle bound after a transport error")
	}
}

func TestFinishedTransportErrorStaysVisibleToTheNextRead(t *testing.T) {
	client := dialSilentSSHClient(t)
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	session := NewSSHPTYSession(client, "deploy@127.0.0.1:22")
	session.started = true
	if !session.TransportFailed() {
		t.Fatal("a closed connection finishes the keepalive with a transport error")
	}
	if !session.ObservedTransportFailure() {
		t.Fatal("the finished error stays visible without starting another probe")
	}
	start := time.Now()
	if !session.TransportFailed() || session.IsAlive() {
		t.Fatal("a retained transport error is still a dead session")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("retained failure waited %s", elapsed)
	}
}

func startSilentGlobalRequestSSHServer(t *testing.T, signer ssh.Signer, user, password string, requests *atomic.Int32) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serverConfig := &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, gotPassword []byte) (*ssh.Permissions, error) {
			if conn.User() == user && string(gotPassword) == password {
				return nil, nil
			}
			return nil, errors.New("invalid test credentials")
		},
	}
	serverConfig.AddHostKey(signer)
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func() {
				serverConn, channels, reqs, handshakeErr := ssh.NewServerConn(conn, serverConfig)
				if handshakeErr != nil {
					return
				}
				go func() {
					for range reqs {
						// Count the request and do not reply. The client probe stays in flight.
						requests.Add(1)
					}
				}()
				for channel := range channels {
					_ = channel.Reject(ssh.UnknownChannelType, "test server accepts no channels")
				}
				_ = serverConn.Close()
			}()
		}
	}()
	t.Cleanup(func() { _ = listener.Close() })
	return listener.Addr().String()
}
