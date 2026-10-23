package mailbox

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
)

const pop3MsgA = "From: Alice <alice@example.com>\r\nTo: me@example.com\r\nSubject: Hello POP\r\nDate: Mon, 19 Oct 2026 10:00:00 +0800\r\nMessage-ID: <a@x>\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nfirst body\r\n.leading dot line\r\n"
const pop3MsgB = "From: Bob <bob@example.com>\r\nTo: me@example.com\r\nSubject: =?UTF-8?B?5Y+R56Wo?=\r\nDate: Tue, 20 Oct 2026 10:00:00 +0800\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=XX\r\n\r\n--XX\r\nContent-Type: text/plain\r\n\r\ninvoice attached\r\n--XX\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=inv.pdf\r\nContent-Transfer-Encoding: base64\r\n\r\nJVBERi0=\r\n--XX--\r\n"

func stuff(msg string) string {
	var b strings.Builder
	for _, l := range strings.Split(strings.TrimSuffix(msg, "\r\n"), "\r\n") {
		if strings.HasPrefix(l, ".") {
			l = "." + l
		}
		b.WriteString(l + "\r\n")
	}
	return b.String()
}

func startPOP3(t *testing.T, pass string) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	msgs := []string{pop3MsgA, pop3MsgB}
	uidls := []string{"uid-a", "uid-b"}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				fmt.Fprint(c, "+OK ready\r\n")
				authed := false
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					f := strings.Fields(line)
					if len(f) == 0 {
						continue
					}
					verb := strings.ToUpper(f[0])
					switch verb {
					case "USER":
						fmt.Fprint(c, "+OK\r\n")
					case "PASS":
						if len(f) > 1 && f[1] == pass {
							authed = true
							fmt.Fprint(c, "+OK\r\n")
						} else {
							fmt.Fprint(c, "-ERR bad\r\n")
						}
					case "UIDL":
						if !authed {
							fmt.Fprint(c, "-ERR\r\n")
							continue
						}
						fmt.Fprint(c, "+OK\r\n")
						for i, u := range uidls {
							fmt.Fprintf(c, "%d %s\r\n", i+1, u)
						}
						fmt.Fprint(c, ".\r\n")
					case "TOP", "RETR":
						var n int
						fmt.Sscanf(f[1], "%d", &n)
						fmt.Fprint(c, "+OK\r\n"+stuff(msgs[n-1])+".\r\n")
					case "STAT":
						fmt.Fprint(c, "+OK 2 100\r\n")
					case "QUIT":
						fmt.Fprint(c, "+OK bye\r\n")
						return
					default:
						fmt.Fprint(c, "-ERR\r\n")
					}
				}
			}(c)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

func pop3Account(port int) Account {
	return Account{Name: "p", Address: "me@example.com", Inbound: &Endpoint{Host: "127.0.0.1", Port: port, Security: SecurityNone, Protocol: ProtocolPOP3, Username: "me"}}
}

func TestPOP3ListSearchReadAttachment(t *testing.T) {
	port := startPOP3(t, "s3cret")
	d := NewNetDialer()
	acc := pop3Account(port)
	ctx := context.Background()
	list, err := d.List(ctx, acc, "s3cret", "", 10)
	if err != nil || len(list) != 2 {
		t.Fatalf("list %v %v", list, err)
	}
	if list[0].Subject != "发票" || list[1].Subject != "Hello POP" || list[0].UID != pop3UID("uid-b") {
		t.Fatalf("order/subject %+v", list)
	}
	found, err := d.Search(ctx, acc, "s3cret", "INBOX", "alice", 10)
	if err != nil || len(found) != 1 || found[0].Subject != "Hello POP" {
		t.Fatalf("search %v %v", found, err)
	}
	body, err := d.Read(ctx, acc, "s3cret", "", pop3UID("uid-a"), 0)
	if err != nil || !strings.Contains(body.Text, "first body") || !strings.Contains(body.Text, "\n.leading dot") {
		t.Fatalf("read %q %v", body.Text, err)
	}
	b2, err := d.Read(ctx, acc, "s3cret", "", pop3UID("uid-b"), 0)
	if err != nil || len(b2.Attachments) != 1 || b2.Attachments[0].Name != "inv.pdf" {
		t.Fatalf("read b %+v %v", b2.Attachments, err)
	}
	att, err := d.Attachment(ctx, acc, "s3cret", "", pop3UID("uid-b"), 0)
	if err != nil || string(att.Bytes) != "%PDF-" {
		t.Fatalf("attachment %q %v", att.Bytes, err)
	}
	if _, err := d.Read(ctx, acc, "s3cret", "", 12345, 0); err == nil {
		t.Fatal("unknown uid must fail")
	}
}

func TestPOP3AuthAndUnsupported(t *testing.T) {
	port := startPOP3(t, "right")
	d := NewNetDialer()
	acc := pop3Account(port)
	if _, err := d.List(context.Background(), acc, "wrong", "", 5); err != ErrAuth {
		t.Fatalf("want ErrAuth, got %v", err)
	}
	if _, err := d.List(context.Background(), acc, "right", "Sent", 5); err == nil || !strings.Contains(err.Error(), "POP3") {
		t.Fatalf("folder on POP3: %v", err)
	}
	if _, err := d.PreviewDelete(context.Background(), acc, "right", DeleteRequest{}); err == nil {
		t.Fatal("delete on POP3 must be refused")
	}
	if f, _ := d.Folders(context.Background(), acc, "right"); len(f) != 1 || f[0].Name != "INBOX" {
		t.Fatalf("folders %v", f)
	}
}

func TestPOP3ProbeAndNormalize(t *testing.T) {
	port := startPOP3(t, "pw")
	s, err := NewNetDialer().DialInbound(context.Background(), Endpoint{Host: "127.0.0.1", Port: port, Security: SecurityNone, Protocol: ProtocolPOP3})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Login("me", "pw"); err != nil {
		t.Fatal(err)
	}
	if err := s.Select("INBOX"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	ep, err := normalizeEndpoint(&Endpoint{Host: "pop.163.com", Protocol: "POP3"}, true)
	if err != nil || ep.Protocol != ProtocolPOP3 || ep.Port != 995 {
		t.Fatalf("normalize %+v %v", ep, err)
	}
	if _, err := normalizeEndpoint(&Endpoint{Host: "x", Protocol: "exchange"}, true); err == nil {
		t.Fatal("unknown protocol accepted")
	}
	ep, _ = normalizeEndpoint(&Endpoint{Host: "imap.x"}, true)
	if ep.Protocol != "" || ep.Port != 993 {
		t.Fatalf("imap default changed %+v", ep)
	}
}
