package mailbox

import (
	"bufio"
	"net"
	"strings"
	"sync"
	"testing"

	imapclient "github.com/emersion/go-imap/client"
)

// scriptIMAP is a tiny line server: it answers LOGIN, CAPABILITY and ID and
// records every command verb it saw.
func scriptIMAP(t *testing.T, caps string) (*imapclient.Client, func() []string) {
	t.Helper()
	srv, cli := net.Pipe()
	var mu sync.Mutex
	var seen []string
	go func() {
		w := bufio.NewWriter(srv)
		r := bufio.NewReader(srv)
		w.WriteString("* OK [CAPABILITY " + caps + "] ready\r\n")
		w.Flush()
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			f := strings.Fields(line)
			if len(f) < 2 {
				continue
			}
			tag, verb := f[0], strings.ToUpper(f[1])
			mu.Lock()
			seen = append(seen, verb+" "+strings.TrimSpace(strings.Join(f[2:], " ")))
			mu.Unlock()
			switch verb {
			case "CAPABILITY":
				w.WriteString("* CAPABILITY " + caps + "\r\n")
			case "ID":
				w.WriteString("* ID NIL\r\n")
			case "LOGOUT":
				w.WriteString("* BYE\r\n")
			}
			w.WriteString(tag + " OK done\r\n")
			w.Flush()
		}
	}()
	c, err := imapclient.New(cli)
	if err != nil {
		t.Fatal(err)
	}
	return c, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), seen...) }
}

func TestIMAPLoginSendsIDWhenAdvertised(t *testing.T) {
	c, seen := scriptIMAP(t, "IMAP4rev1 ID AUTH=PLAIN")
	defer c.Terminate()
	if err := imapLogin(c, "u@163.com", "code"); err != nil {
		t.Fatal(err)
	}
	var id string
	for _, s := range seen() {
		if strings.HasPrefix(s, "ID ") {
			id = s
		}
	}
	if id == "" || !strings.Contains(id, `"MaClaw"`) {
		t.Fatalf("no ID command after login: %v", seen())
	}
}

func TestIMAPLoginSkipsIDWhenNotAdvertised(t *testing.T) {
	c, seen := scriptIMAP(t, "IMAP4rev1 AUTH=PLAIN")
	defer c.Terminate()
	if err := imapLogin(c, "u@example.com", "pw"); err != nil {
		t.Fatal(err)
	}
	for _, s := range seen() {
		if strings.HasPrefix(s, "ID ") {
			t.Fatalf("ID sent to a server without ID: %v", seen())
		}
	}
}
