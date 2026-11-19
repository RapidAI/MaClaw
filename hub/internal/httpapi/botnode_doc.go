package httpapi

import (
	_ "embed"
	"net/http"
)

//go:embed botnode.md
var botnodeDoc []byte

// serveBotnodeDoc is the intranet node runbook. It is public so an agent on
// that machine can read it. It contains no secrets.
func serveBotnodeDoc(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(botnodeDoc)
}

func registerBotnodeDoc(mux *http.ServeMux) {
	mux.HandleFunc("GET /docs/botnode", serveBotnodeDoc)
	mux.HandleFunc("GET /docs/botnode.md", serveBotnodeDoc)
}
