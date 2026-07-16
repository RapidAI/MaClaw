package main

import (
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
)

// recordAudioHTTPPath is the AssetServer route for true binary PCM append
// (no base64). Frontend: POST body = raw bytes, query session_id=.
const recordAudioHTTPPath = "/maclaw-record/v1/append"

// recordAudioAssetMiddleware intercepts binary record append POSTs, then
// falls through to the next asset handler (frontend SPA / static files).
func recordAudioAssetMiddleware(app *App, next http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if app != nil && req != nil && req.Method == http.MethodPost &&
			strings.TrimSuffix(req.URL.Path, "/") == recordAudioHTTPPath {
			handleRecordAudioAppendHTTP(app, rw, req)
			return
		}
		// Chain existing no-store headers for static assets.
		if req != nil {
			rw.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
			rw.Header().Set("Pragma", "no-cache")
			rw.Header().Set("Expires", "0")
		}
		next.ServeHTTP(rw, req)
	})
}

func handleRecordAudioAppendHTTP(app *App, rw http.ResponseWriter, req *http.Request) {
	// Bound body to chunk limit (+ small slack) before full read.
	req.Body = http.MaxBytesReader(rw, req.Body, int64(maxRecordedAudioChunkBytes)+1024)
	sessionID := strings.TrimSpace(req.URL.Query().Get("session_id"))
	if sessionID == "" {
		sessionID = strings.TrimSpace(req.Header.Get("X-Record-Session-Id"))
	}
	if sessionID == "" {
		http.Error(rw, "session_id required", http.StatusBadRequest)
		return
	}
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) ||
			strings.Contains(err.Error(), "request body too large") ||
			strings.Contains(err.Error(), "http: request body too large") {
			http.Error(rw, "chunk too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(rw, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := app.appendRecordedAudioRaw(sessionID, raw); err != nil {
		status := http.StatusBadRequest
		low := strings.ToLower(err.Error())
		switch {
		case strings.Contains(low, "not found"):
			status = http.StatusNotFound
		case strings.Contains(low, "chunk too large"), strings.Contains(low, "too large"):
			status = http.StatusRequestEntityTooLarge
		case strings.Contains(low, "closed"):
			status = http.StatusConflict
		}
		if recordDetailEnabled() {
			log.Printf("[record-audio] http append fail session=%s status=%d err=%v", sessionID, status, err)
		}
		http.Error(rw, err.Error(), status)
		return
	}
	rw.WriteHeader(http.StatusNoContent)
}
