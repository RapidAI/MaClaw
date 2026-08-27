package cloudworkspace

import (
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const maxManifestPathBytes = 1024

// ValidateManifestPath requires a relative, slash-separated path with no "..".
func ValidateManifestPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" || len(p) > maxManifestPathBytes || !utf8.ValidString(p) {
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
	}
	if ShouldIgnore(cleaned, false, "") {
		return "", ErrInvalidPath
	}
	return cleaned, nil
}
