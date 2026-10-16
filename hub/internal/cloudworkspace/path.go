package cloudworkspace

import (
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const maxManifestPathBytes = 1024

// ValidateManifestPath requires a relative, slash-separated path with no "..".
func ValidateManifestPath(p string) (string, error) {
	if p == "" || strings.TrimSpace(p) != p || len(p) > maxManifestPathBytes || !utf8.ValidString(p) {
		return "", ErrInvalidPath
	}
	if strings.ContainsAny(p, `\:`) || strings.ContainsRune(p, 0) {
		return "", ErrInvalidPath
	}
	for _, r := range p {
		if r < 0x20 {
			return "", ErrInvalidPath
		}
	}
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) {
		return "", ErrInvalidPath
	}
	cleaned := path.Clean("/" + p)
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "" || cleaned == "." || cleaned == ".." {
		return "", ErrInvalidPath
	}
	if cleaned != p {
		return "", ErrInvalidPath
	}
	for _, seg := range strings.Split(cleaned, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", ErrInvalidPath
		}
		// A workspace can be handed off to Windows even when it was first
		// written on Linux/macOS. Reject names that Windows cannot materialize
		// instead of allowing a later Pull to overwrite a different file.
		if !portableManifestSegment(seg) {
			return "", ErrInvalidPath
		}
	}
	if ShouldIgnore(cleaned, false, "") {
		return "", ErrInvalidPath
	}
	return cleaned, nil
}

// portableManifestSegment rejects Windows device names and names whose
// trailing dot/space would be silently stripped by a case-insensitive file
// system. It intentionally does not force lower-case paths; the original
// spelling remains user-visible while normalizeEntries detects collisions.
func portableManifestSegment(seg string) bool {
	if seg == "" || strings.HasSuffix(seg, ".") || strings.HasSuffix(seg, " ") {
		return false
	}
	trimmed := strings.TrimRight(seg, " .")
	base := trimmed
	if dot := strings.IndexByte(base, '.'); dot >= 0 {
		base = base[:dot]
	}
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return false
	default:
		return true
	}
}

// manifestPathKey is the cross-platform identity used only for collision
// detection. NFC makes canonically equivalent Unicode spellings compare equal;
// lower-casing matches the case-insensitive behavior of Windows/macOS default
// volumes. The manifest still stores the original normalized spelling.
func manifestPathKey(p string) string {
	return strings.ToLower(norm.NFC.String(p))
}
