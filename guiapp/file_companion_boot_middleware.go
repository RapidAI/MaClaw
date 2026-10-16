package guiapp

import (
	"bytes"
	"net/http"
)

// fileCompanionBootMiddleware injects the companion boot flag into the shell
// HTML. Other responses, and every response in the main process, pass through.
func fileCompanionBootMiddleware(app *App, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if app == nil || !app.isFileCompanionProcess() || !fileCompanionBootPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		cap := &fileCompanionHTMLCapture{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(cap, r)
		body := injectFileCompanionBootFlag(cap.buf.Bytes())
		header := w.Header()
		header.Del("Content-Length")
		w.WriteHeader(cap.code)
		_, _ = w.Write(body)
	})
}

func fileCompanionBootPath(path string) bool {
	switch path {
	case "/", "/index.html":
		return true
	default:
		return false
	}
}

type fileCompanionHTMLCapture struct {
	http.ResponseWriter
	buf  bytes.Buffer
	code int
}

func (c *fileCompanionHTMLCapture) WriteHeader(status int) {
	c.code = status
}

func (c *fileCompanionHTMLCapture) Write(p []byte) (int, error) {
	return c.buf.Write(p)
}

func fileCompanionAssetMiddleware(app *App, next http.Handler) http.Handler {
	return fileCompanionBootMiddleware(app, recordAudioAssetMiddleware(app, noStoreAssetMiddleware(next)))
}
