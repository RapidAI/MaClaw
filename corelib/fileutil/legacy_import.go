package fileutil

import (
	"os"
	"strings"
)

// LegacyImportPending reports whether path still holds a document that a later
// open can import. accept reports whether the bytes unmarshal into that store's
// document. A nil accept treats any non-empty readable file as pending.
// A missing file, an empty file, a directory, and bytes accept rejects are not
// pending, so the next save may record the import and remove the file.
// Any other read error is pending: a locked file stays until a later open can
// copy it.
func LegacyImportPending(path string, accept func([]byte) bool) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return !os.IsNotExist(err)
	}
	if info.IsDir() {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return !os.IsNotExist(err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return false
	}
	if accept == nil {
		return true
	}
	return accept(data)
}
