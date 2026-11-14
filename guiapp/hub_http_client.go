package guiapp

import (
	"net/http"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// Hub HTTP traffic shares a verified-TLS connection pool. Self-signed private
// hubs use MACLAW_HUB_CA_FILE; verification is never disabled implicitly.
var hubHTTPClient = func() *http.Client {
	client := remote.NewHubHTTPClient()
	client.Timeout = 30 * time.Second
	return client
}()
