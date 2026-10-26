package remote

import (
	"bufio"
	"fmt"
	"net"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// startTestTypedShellSSHServer accepts one managed PTY session and reports every
// line the client types into the remote shell. The shell stays open until the
// client closes stdin, so a one-shot command that ends early is observable.
func startTestTypedShellSSHServer(t *testing.T, signer ssh.Signer) (address string, typed <-chan string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	lines := make(chan string, 8)
	serverConfig := &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if conn.User() == "deploy" && string(password) == "test-password" {
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
			go func() {
				serverConn, channels, requests, handshakeErr := ssh.NewServerConn(conn, serverConfig)
				if handshakeErr != nil {
					return
				}
				go ssh.DiscardRequests(requests)
				for channel := range channels {
					if channel.ChannelType() != "session" {
						_ = channel.Reject(ssh.UnknownChannelType, "only session channels")
						continue
					}
					ch, chRequests, chErr := channel.Accept()
					if chErr != nil {
						continue
					}
					go func() {
						for req := range chRequests {
							switch req.Type {
							case "pty-req":
								_ = req.Reply(true, nil)
							case "shell":
								_ = req.Reply(true, nil)
								go func() {
									scanner := bufio.NewScanner(ch)
									for scanner.Scan() {
										line := scanner.Text()
										lines <- line
										_, _ = fmt.Fprintf(ch, "OUT:%s\n", line)
									}
									_ = ch.Close()
								}()
								return
							default:
								_ = req.Reply(false, nil)
							}
						}
						_ = ch.Close()
					}()
				}
				_ = serverConn.Close()
			}()
		}
	}()
	t.Cleanup(func() { _ = listener.Close() })
	return listener.Addr().String(), lines
}

func TestSSHPTYSessionInitialCommandRunsInsideTheShell(t *testing.T) {
	signer := testSSHSigner(t)
	address, typed := startTestTypedShellSSHServer(t, signer)

	client, err := ssh.Dial("tcp", address, &ssh.ClientConfig{
		User:            "deploy",
		Auth:            []ssh.AuthMethod{ssh.Password("test-password")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	session := NewSSHPTYSession(client, "deploy@test:22")
	if err := session.Start(SSHSessionSpec{
		HostConfig:     SSHHostConfig{User: "deploy", Host: "test", Port: 22, KeepaliveInterval: 10 * time.Second},
		InitialCommand: "uname -a",
	}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer session.Close()

	waitForTypedLine(t, typed, "uname -a")

	// The initial command is a line typed into the interactive shell, not the
	// session's whole process. The same channel must still accept the next
	// exec; before this fix the remote process had already exited here.
	if err := session.Write([]byte("uname -r\n")); err != nil {
		t.Fatalf("Write() after the initial command = %v", err)
	}
	waitForTypedLine(t, typed, "uname -r")
	if !session.IsAlive() {
		t.Fatal("the managed PTY session must stay alive after the initial command")
	}
}

func waitForTypedLine(t *testing.T, typed <-chan string, want string) {
	t.Helper()
	select {
	case got := <-typed:
		if got != want {
			t.Fatalf("remote shell received %q, want %q", got, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("remote shell did not receive %q", want)
	}
}
