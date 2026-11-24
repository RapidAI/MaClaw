package httpapi

import (
	_ "embed"
	"net/http"
)

//go:embed botnode.md
var botnodeDoc []byte

//go:embed maclawsrv_only.md
var maclawsrvOnlyDoc []byte

// serveBotnodeDoc is the intranet node runbook. It is public so an agent on
// that machine can read it. It contains no secrets.
func serveBotnodeDoc(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(botnodeDoc)
}

// serveMaclawsrvOnlyDoc is the 私有云端服务 runbook: MaClawSrv + botnode
// tunnel only, no desktopd. Same public runbook rules as botnode.
func serveMaclawsrvOnlyDoc(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(maclawsrvOnlyDoc)
}

func registerBotnodeDoc(mux *http.ServeMux) {
	mux.HandleFunc("GET /docs/botnode", serveBotnodeDoc)
	mux.HandleFunc("GET /docs/botnode.md", serveBotnodeDoc)
	mux.HandleFunc("GET /docs/maclawsrv_only", serveMaclawsrvOnlyDoc)
	mux.HandleFunc("GET /docs/maclawsrv_only.md", serveMaclawsrvOnlyDoc)
}
