package guiapp

import (
	"bytes"
	"encoding/base64"
	"errors"
	"path/filepath"
	"strings"
	"sync"
)

// ErrFileTypeIconUnsupported reports a platform with no shell icon mechanism.
// It is definitive, not a transient fault: callers fall back to their own glyph
// and stop asking.
var ErrFileTypeIconUnsupported = errors.New("file type icons are provided by the host operating system")

// fileTypeIconCache stores one icon per file type. The shell icon is the
// association for the extension, so every .pdf shares a single lookup.
// Executables, shortcuts, and icon files keep their own icon, keyed by path.
var fileTypeIconCache sync.Map

// fileTypeIconFlight collapses concurrent lookups of the same key. The owner
// publishes url/err before closing done.
var fileTypeIconFlight sync.Map

type fileTypeIconCall struct {
	done  chan struct{}
	entry fileTypeIconEntry
	err   error
}

// fileTypeIconEntry keeps the shell's answer together with whether it actually
// describes the type. A suffix no application claims resolves to the same blank
// "unknown file" sheet as every other unclaimed suffix; the bytes are what
// identifies that case, so the verdict is computed once per key and reused.
type fileTypeIconEntry struct {
	url     string
	generic bool
}

// genericFileTypeIconPNG is the shell's answer for an unclaimed suffix. It is
// resolved once: the probe name carries a suffix no association can claim.
var (
	genericFileTypeIconOnce sync.Once
	genericFileTypeIconPNG  []byte
	genericFileTypeIconErr  error
)

const genericFileTypeIconProbeName = "file.maclaw-no-association"

func loadGenericFileTypeIconPNG() ([]byte, error) {
	genericFileTypeIconOnce.Do(func() {
		genericFileTypeIconPNG, genericFileTypeIconErr = shellFileTypeIconPNG(genericFileTypeIconProbeName)
	})
	return genericFileTypeIconPNG, genericFileTypeIconErr
}

// fileTypeIconIsGeneric reports whether these bytes are the shell's placeholder
// for an unassociated suffix rather than an icon for this type. A probe that
// failed reports false: without a known placeholder nothing can match it.
func fileTypeIconIsGeneric(pngBytes []byte) bool {
	generic, err := loadGenericFileTypeIconPNG()
	if err != nil || len(generic) == 0 || len(generic) != len(pngBytes) {
		return false
	}
	return bytes.Equal(generic, pngBytes)
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
	entry, err := a.fileTypeIcon(path)
	if err != nil {
		return "", err
	}
	return entry.url, nil
}

// AIAssistantFileTypeIconDataURLIfAssociated is the cloud drive's variant. It
// answers "" for a suffix no application claims, because that suffix resolves
// to the shell's blank sheet, which says nothing about the type. The caller
// draws its own per-type glyph instead. A platform with no icon mechanism
// answers the same way, so callers stop asking instead of retrying a lookup
// that can never succeed.
func (a *App) AIAssistantFileTypeIconDataURLIfAssociated(path string) (string, error) {
	entry, err := a.fileTypeIcon(path)
	if errors.Is(err, ErrFileTypeIconUnsupported) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if entry.generic {
		return "", nil
	}
	return entry.url, nil
}

func (a *App) fileTypeIcon(path string) (fileTypeIconEntry, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return fileTypeIconEntry{}, errors.New("attachment path is required")
	}
	key := fileTypeIconCacheKey(path)
	if entry, ok := cachedFileTypeIcon(key); ok {
		return entry, nil
	}

	call := &fileTypeIconCall{done: make(chan struct{})}
	if existing, loaded := fileTypeIconFlight.LoadOrStore(key, call); loaded {
		wait := existing.(*fileTypeIconCall)
		<-wait.done
		return wait.entry, wait.err
	}
	defer func() {
		close(call.done)
		fileTypeIconFlight.Delete(key)
	}()

	// A lookup that finished while this call was being published.
	if entry, ok := cachedFileTypeIcon(key); ok {
		call.entry = entry
		return entry, nil
	}
	pngBytes, err := shellFileTypeIconPNG(path)
	if err != nil {
		call.err = err
		return fileTypeIconEntry{}, err
	}
	entry := fileTypeIconEntry{
		url:     "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes),
		generic: fileTypeIconIsGeneric(pngBytes),
	}
	fileTypeIconCache.Store(key, entry)
	call.entry = entry
	return entry, nil
}

func cachedFileTypeIcon(key string) (fileTypeIconEntry, bool) {
	cached, ok := fileTypeIconCache.Load(key)
	if !ok {
		return fileTypeIconEntry{}, false
	}
	entry, ok := cached.(fileTypeIconEntry)
	return entry, ok && entry.url != ""
}
