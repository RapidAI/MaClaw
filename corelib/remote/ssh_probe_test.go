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
			ok, finished, _ := awaitSSHProbe(client, 200*time.Millisecond)
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
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, loaded := sshProbes.Load(client); !loaded {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("shared probe was not removed after the connection closed")
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
