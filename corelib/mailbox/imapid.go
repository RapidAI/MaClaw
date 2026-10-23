package mailbox

import (
	"github.com/emersion/go-imap"
	imapclient "github.com/emersion/go-imap/client"
)

// IMAP ID (RFC 2971). NetEase (163/126/yeah) answers SELECT with "Unsafe
// Login" until the client identifies itself, so the ID goes out right after
// login on every server that advertises it. A server that refuses it still
// gets the session; ID is best-effort.
var imapIDFields = []interface{}{
	"name", "MaClaw",
	"version", "1.0",
	"vendor", "RapidAI",
	"support-email", "support@maclaw.top",
}

type imapIDCommand struct{}

func (imapIDCommand) Command() *imap.Command {
	return &imap.Command{Name: "ID", Arguments: []interface{}{imapIDFields}}
}

func sendIMAPID(c *imapclient.Client) {
	if c == nil {
		return
	}
	if ok, err := c.Support("ID"); err != nil || !ok {
		return
	}
	_, _ = c.Execute(imapIDCommand{}, nil)
}
