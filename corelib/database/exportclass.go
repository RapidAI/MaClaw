package database

import (
	"path/filepath"
	"strings"
	"sync"
)

var classifiedExports sync.Map // abs path -> classification

// NoteClassifiedExport records that path was produced by export_excel from a
// classified profile. send_file / send_to_im consult this before shipping
// the file off-box.
func NoteClassifiedExport(path, class string) {
	path = strings.TrimSpace(path)
	class = strings.ToLower(strings.TrimSpace(class))
	if path == "" || class == "" {
		return
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	classifiedExports.Store(path, class)
}

// ClassifiedExport returns the classification of a previously exported file.
func ClassifiedExport(path string) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	value, ok := classifiedExports.Load(path)
	if !ok {
		return "", false
	}
	class, _ := value.(string)
	return class, class != ""
}

// BlocksExternalDelivery reports whether a classified export may not be
// uploaded to IM or the public network.
func BlocksExternalDelivery(class string) bool {
	switch strings.ToLower(strings.TrimSpace(class)) {
	case "confidential", "restricted":
		return true
	default:
		return false
	}
}
