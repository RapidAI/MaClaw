package guiapp

import (
	"encoding/base64"
	"errors"
	"path/filepath"
	"strings"
	"sync"
)

// fileTypeIconCache stores one PNG data URL per file type. The shell icon is
// the association for the extension, so every .pdf shares a single lookup.
// Executables, shortcuts, and icon files keep their own icon, keyed by path.
var fileTypeIconCache sync.Map

// fileTypeIconFlight collapses concurrent lookups of the same key. The owner
// publishes url/err before closing done.
var fileTypeIconFlight sync.Map

type fileTypeIconCall struct {
	done chan struct{}
	url  string
	err  error
}

func fileTypeIconCacheKey(path string) string {
	path = strings.TrimSpace(path)
	if fileTypeIconUsesOwnIcon(path) {
		return strings.ToLower(path)
	}
	return strings.ToLower(filepath.Ext(path))
}

// fileTypeIconUsesOwnIcon reports paths whose shell icon is the file's own
// icon rather than the type association. Documents stay on the association so
// the shell does not open them.
func fileTypeIconUsesOwnIcon(path string) bool {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(path))) {
	case "", ".exe", ".lnk", ".ico", ".url", ".scr":
		return true
	default:
		return false
	}
}

// AIAssistantFileTypeIconDataURL returns a PNG data URL of the operating
// system's associated icon for the attachment's file type. The WebView never
// receives a file URL. A missing file still resolves from its extension.
func (a *App) AIAssistantFileTypeIconDataURL(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("attachment path is required")
	}
	key := fileTypeIconCacheKey(path)
	if url, ok := cachedFileTypeIcon(key); ok {
		return url, nil
	}

	call := &fileTypeIconCall{done: make(chan struct{})}
	if existing, loaded := fileTypeIconFlight.LoadOrStore(key, call); loaded {
		wait := existing.(*fileTypeIconCall)
		<-wait.done
		return wait.url, wait.err
	}
	defer func() {
		close(call.done)
		fileTypeIconFlight.Delete(key)
	}()

	// A lookup that finished while this call was being published.
	if url, ok := cachedFileTypeIcon(key); ok {
		call.url = url
		return url, nil
	}
	pngBytes, err := shellFileTypeIconPNG(path)
	if err != nil {
		call.err = err
		return "", err
	}
	call.url = "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes)
	fileTypeIconCache.Store(key, call.url)
	return call.url, nil
}

func cachedFileTypeIcon(key string) (string, bool) {
	cached, ok := fileTypeIconCache.Load(key)
	if !ok {
		return "", false
	}
	url, ok := cached.(string)
	return url, ok && url != ""
}
