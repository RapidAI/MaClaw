package mailbox

import "github.com/emersion/go-sasl"

// xoauth2Client is the SASL XOAUTH2 mechanism used by Gmail and Hotmail.
// The initial response is: user=<email>\x01auth=Bearer <token>\x01\x01
type xoauth2Client struct {
	username string
	token    string
}

func newXOAuth2Client(username, token string) sasl.Client {
	return &xoauth2Client{username: username, token: token}
}

func (c *xoauth2Client) Start() (string, []byte, error) {
	if c == nil || c.username == "" || c.token == "" {
		return "", nil, ErrAuth
	}
	raw := "user=" + c.username + "\x01auth=Bearer " + c.token + "\x01\x01"
	return "XOAUTH2", []byte(raw), nil
}

func (c *xoauth2Client) Next(challenge []byte) ([]byte, error) {
	// Gmail and Outlook send a challenge only when the token was rejected, and
	// that payload can quote the token. An empty reply lets the server finish
	// with NO or 535. Cancelling with "*" leaves the connection mid-command.
	return []byte{}, nil
}
