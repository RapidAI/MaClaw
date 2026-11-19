package httpapi

import (
	_ "embed"
	"net/http"
)

//go:embed hub-install.md
var hubInstallDoc []byte

// serveHubInstallDoc is the Hub install and Hub Center registration runbook.
// It is public so an agent on the target machine can read it. It contains no secrets.
func serveHubInstallDoc(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(hubInstallDoc)
}

func registerHubInstallDoc(mux *http.ServeMux) {
	mux.HandleFunc("GET /docs/hub-install", serveHubInstallDoc)
	mux.HandleFunc("GET /docs/hub-install.md", serveHubInstallDoc)
}
