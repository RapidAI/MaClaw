package mailbox

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-smtp"
)

func TestSMTPLoginAuthWhenPlainIsRejected(t *testing.T) {
	user, pass := exchangeSMTP(t, "PLAIN LOGIN", true)
	if user != "me@qq.com" || pass != "授权码" {
		t.Fatalf("user=%q pass=%q", user, pass)
	}
}

func TestSMTPLoginOnlyDoesNotTryPlain(t *testing.T) {
	user, pass := exchangeSMTP(t, "LOGIN", false)
	if user != "me@qq.com" || pass != "授权码" {
		t.Fatalf("user=%q pass=%q", user, pass)
	}
}

func TestSMTPPlainAuthWhenAdvertised(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	errCh := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		rw := bufio.NewReader(conn)
		fmt.Fprintf(conn, "220 localhost\r\n")
		if _, err := rw.ReadString('\n'); err != nil {
			errCh <- err
			return
		}
		fmt.Fprintf(conn, "250-localhost\r\n250-AUTH PLAIN\r\n250 OK\r\n")
		line, err := rw.ReadString('\n')
		if err != nil {
			errCh <- err
			return
		}
		if !strings.Contains(strings.ToUpper(line), "AUTH PLAIN") {
			errCh <- fmt.Errorf("auth=%q", line)
			return
		}
		fmt.Fprintf(conn, "235 ok\r\n")
		errCh <- nil
	}()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	client := smtp.NewClient(conn)
	client.CommandTimeout = 3 * time.Second
	if err := authSMTP(client, "me@qq.com", "secret"); err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

func TestSMTPOauthUsesXOAUTH2(t *testing.T) {
	secret, err := encodeOAuthSecret(oauthSecret{
		Kind: AuthOAuth, Provider: ProviderGmail, AccessToken: "ya29.test", RefreshToken: "r",
		Expiry: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	errCh := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		rw := bufio.NewReader(conn)
		fmt.Fprintf(conn, "220 localhost\r\n")
		if _, err := rw.ReadString('\n'); err != nil {
			errCh <- err
			return
		}
		fmt.Fprintf(conn, "250-localhost\r\n250-AUTH XOAUTH2\r\n250 OK\r\n")
		line, err := rw.ReadString('\n')
		if err != nil {
			errCh <- err
			return
		}
		parts := strings.Fields(strings.TrimSpace(line))
		if len(parts) < 3 || parts[1] != "XOAUTH2" {
			errCh <- fmt.Errorf("auth=%q", line)
			return
		}
		raw, err := base64.StdEncoding.DecodeString(parts[2])
		if err != nil {
			errCh <- err
			return
		}
		if string(raw) != "user=a@gmail.com\x01auth=Bearer ya29.test\x01\x01" {
			errCh <- fmt.Errorf("payload=%q", raw)
			return
		}
		fmt.Fprintf(conn, "235 ok\r\n")
		errCh <- nil
	}()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	client := smtp.NewClient(conn)
	client.CommandTimeout = 3 * time.Second
	if err := authSMTP(client, "a@gmail.com", secret); err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

func TestDeadlineConnRejectsAClearedOrExtendedDeadline(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	stamp := &stampConn{Conn: left}
	capAt := time.Now().Add(time.Minute)
	wrapped := &deadlineConn{Conn: stamp, deadline: capAt}
	if err := wrapped.SetDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := wrapped.SetDeadline(time.Now().Add(5 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(stamp.deadlines) != 2 {
		t.Fatalf("deadlines=%d", len(stamp.deadlines))
	}
	for _, got := range stamp.deadlines {
		if got.IsZero() || got.After(capAt.Add(time.Second)) {
			t.Fatalf("deadline %v escaped cap %v", got, capAt)
		}
	}
}

func TestHungIMAPLoginStopsAtTheContextDeadline(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		fmt.Fprintf(conn, "* OK IMAP4rev1 ready\r\n")
		buf := make([]byte, 64)
		_, _ = conn.Read(buf)
		time.Sleep(4 * time.Second)
	}()
	port := listenPort(t, ln)
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	session, err := NewNetDialer().DialInbound(ctx, Endpoint{Host: "127.0.0.1", Port: port, Security: SecurityNone})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	started := time.Now()
	err = session.Login("me@qq.com", "secret")
	if err == nil || time.Since(started) > 2*time.Second || !errors.Is(err, ErrConnect) {
		t.Fatalf("err=%v waited=%s", err, time.Since(started))
	}
}

func TestHungSMTPAuthStopsAtTheContextDeadline(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		fmt.Fprintf(conn, "220 localhost\r\n")
		rw := bufio.NewReader(conn)
		if _, err := rw.ReadString('\n'); err != nil {
			return
		}
		fmt.Fprintf(conn, "250-localhost\r\n250-AUTH PLAIN\r\n250 OK\r\n")
		if _, err := rw.ReadString('\n'); err != nil {
			return
		}
		time.Sleep(4 * time.Second)
	}()
	port := listenPort(t, ln)
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	session, err := NewNetDialer().DialOutbound(ctx, Endpoint{Host: "127.0.0.1", Port: port, Security: SecurityNone})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Quit()
	started := time.Now()
	err = session.Auth("me@qq.com", "secret")
	if err == nil || time.Since(started) > 2*time.Second || !errors.Is(err, ErrConnect) {
		t.Fatalf("err=%v waited=%s", err, time.Since(started))
	}
}

type stampConn struct {
	net.Conn
	deadlines []time.Time
}

func (s *stampConn) SetDeadline(t time.Time) error {
	s.deadlines = append(s.deadlines, t)
	return s.Conn.SetDeadline(t)
}

func listenPort(t *testing.T, ln net.Listener) int {
	t.Helper()
	_, portText, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func exchangeSMTP(t *testing.T, authLine string, rejectPlain bool) (string, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	errCh := make(chan error, 1)
	credCh := make(chan [2]string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		rw := bufio.NewReader(conn)
		if _, err := fmt.Fprintf(conn, "220 localhost\r\n"); err != nil {
			errCh <- err
			return
		}
		if _, err := rw.ReadString('\n'); err != nil {
			errCh <- err
			return
		}
		if _, err := fmt.Fprintf(conn, "250-localhost\r\n250-AUTH %s\r\n250 OK\r\n", authLine); err != nil {
			errCh <- err
			return
		}
		line, err := rw.ReadString('\n')
		if err != nil {
			errCh <- err
			return
		}
		if rejectPlain {
			if !strings.Contains(strings.ToUpper(line), "AUTH PLAIN") {
				errCh <- fmt.Errorf("expected PLAIN, got %q", line)
				return
			}
			fmt.Fprintf(conn, "535 denied\r\n")
			if _, err := rw.ReadString('\n'); err != nil {
				errCh <- err
				return
			}
			fmt.Fprintf(conn, "501 cancel\r\n")
			line, err = rw.ReadString('\n')
			if err != nil {
				errCh <- err
				return
			}
		}
		if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(line)), "AUTH LOGIN") {
			errCh <- fmt.Errorf("expected LOGIN, got %q", line)
			return
		}
		fmt.Fprintf(conn, "334 VXNlcm5hbWU6\r\n")
		userLine, err := rw.ReadString('\n')
		if err != nil {
			errCh <- err
			return
		}
		fmt.Fprintf(conn, "334 UGFzc3dvcmQ6\r\n")
		passLine, err := rw.ReadString('\n')
		if err != nil {
			errCh <- err
			return
		}
		fmt.Fprintf(conn, "235 ok\r\n")
		user, err1 := base64.StdEncoding.DecodeString(strings.TrimSpace(userLine))
		pass, err2 := base64.StdEncoding.DecodeString(strings.TrimSpace(passLine))
		if err1 != nil || err2 != nil {
			errCh <- fmt.Errorf("decode user %v pass %v", err1, err2)
			return
		}
		credCh <- [2]string{string(user), string(pass)}
		errCh <- nil
	}()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	client := smtp.NewClient(conn)
	client.CommandTimeout = 3 * time.Second
	if err := authSMTP(client, "me@qq.com", "授权码"); err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	cred := <-credCh
	return cred[0], cred[1]
}
