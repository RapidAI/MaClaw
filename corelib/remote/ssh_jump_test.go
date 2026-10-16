package remote

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestDialSSHProxyJumpReachesTarget(t *testing.T) {
	signer := testSSHSigner(t)
	targetAddr := startTestExecSSHServer(t, signer, "deploy", "correct-password")
	jumpAddr := startTestJumpSSHServer(t, signer, "deploy", "correct-password")
	targetHost, targetPort := testSSHHostPort(t, targetAddr)
	jumpHost, jumpPort := testSSHHostPort(t, jumpAddr)

	cfg := SSHHostConfig{
		Host:           targetHost,
		User:           "deploy",
		Port:           targetPort,
		Password:       "correct-password",
		AuthMethod:     "password",
		ConnectTimeout: 2 * time.Second,
		JumpHosts: []SSHHostConfig{{
			Host:           jumpHost,
			User:           "deploy",
			Port:           jumpPort,
			Password:       "correct-password",
			AuthMethod:     "password",
			ConnectTimeout: 2 * time.Second,
		}},
	}
	client, err := dialSSH(cfg)
	if err != nil {
		t.Fatalf("dialSSH() error = %v", err)
	}
	defer client.Close()

	result, err := RunSSHCommand(context.Background(), client, "echo hello", 5*time.Second)
	if err != nil {
		t.Fatalf("RunSSHCommand() error = %v", err)
	}
	if result.ExitCode != 0 || result.Stdout != "OUT:echo hello\n" {
		t.Fatalf("result = %+v", result)
	}
}

func TestDialSSHProxyJumpTimesOut(t *testing.T) {
	signer := testSSHSigner(t)
	jumpAddr := startTestHangingJumpSSHServer(t, signer, "deploy", "correct-password")
	jumpHost, jumpPort := testSSHHostPort(t, jumpAddr)

	start := time.Now()
	_, err := dialSSH(SSHHostConfig{
		Host:           "127.0.0.1",
		User:           "deploy",
		Port:           1,
		Password:       "correct-password",
		AuthMethod:     "password",
		ConnectTimeout: 200 * time.Millisecond,
		JumpHosts: []SSHHostConfig{{
			Host:           jumpHost,
			User:           "deploy",
			Port:           jumpPort,
			Password:       "correct-password",
			AuthMethod:     "password",
			ConnectTimeout: 2 * time.Second,
		}},
	})
	elapsed := time.Since(start)
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("error = %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("jump dial hung for %s", elapsed)
	}
}

func startTestHangingJumpSSHServer(t *testing.T, signer ssh.Signer, user, password string) string {
	t.Helper()
	return startTestJumpSSHServerWith(t, signer, user, password, serveTestHangingJumpConn)
}

func startTestJumpSSHServer(t *testing.T, signer ssh.Signer, user, password string) string {
	t.Helper()
	return startTestJumpSSHServerWith(t, signer, user, password, serveTestJumpConn)
}

func startTestJumpSSHServerWith(t *testing.T, signer ssh.Signer, user, password string, serve func(net.Conn, *ssh.ServerConfig)) string {
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
			return nil, fmt.Errorf("invalid test credentials")
		},
	}
	serverConfig.AddHostKey(signer)
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go serve(conn, serverConfig)
		}
	}()
	t.Cleanup(func() { _ = listener.Close() })
	return listener.Addr().String()
}

func serveTestJumpConn(conn net.Conn, serverConfig *ssh.ServerConfig) {
	serverConn, channels, requests, err := ssh.NewServerConn(conn, serverConfig)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(requests)
	for newChannel := range channels {
		if newChannel.ChannelType() != "direct-tcpip" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "only direct-tcpip")
			continue
		}
		go serveTestDirectTCPIP(newChannel)
	}
	_ = serverConn.Close()
}

func serveTestHangingJumpConn(conn net.Conn, serverConfig *ssh.ServerConfig) {
	_, channels, requests, err := ssh.NewServerConn(conn, serverConfig)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(requests)
	for newChannel := range channels {
		if newChannel.ChannelType() != "direct-tcpip" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "only direct-tcpip")
		}
	}
}

func serveTestDirectTCPIP(newChannel ssh.NewChannel) {
	var msg struct {
		DestAddr string
		DestPort uint32
		OrigAddr string
		OrigPort uint32
	}
	if err := ssh.Unmarshal(newChannel.ExtraData(), &msg); err != nil {
		_ = newChannel.Reject(ssh.ConnectionFailed, "bad direct-tcpip payload")
		return
	}
	target, err := net.Dial("tcp", net.JoinHostPort(msg.DestAddr, strconv.Itoa(int(msg.DestPort))))
	if err != nil {
		_ = newChannel.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	ch, reqs, err := newChannel.Accept()
	if err != nil {
		_ = target.Close()
		return
	}
	go ssh.DiscardRequests(reqs)
	go func() {
		_, _ = io.Copy(ch, target)
		_ = ch.CloseWrite()
	}()
	go func() {
		_, _ = io.Copy(target, ch)
		_ = target.Close()
	}()
}
