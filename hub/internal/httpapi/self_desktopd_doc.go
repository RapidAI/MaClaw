package httpapi

import (
	_ "embed"
	"net/http"
)

//go:embed self_desktopd.md
var selfDesktopdDoc []byte

// serveSelfDesktopdDoc is the desktop client's one-click install runbook.
// It is public so the installing agent can read it. It contains no secrets.
func serveSelfDesktopdDoc(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(selfDesktopdDoc)
}

func registerSelfDesktopdDoc(mux *http.ServeMux) {
	mux.HandleFunc("GET /docs/self-desktopd", serveSelfDesktopdDoc)
	mux.HandleFunc("GET /docs/self-desktopd.md", serveSelfDesktopdDoc)
}
