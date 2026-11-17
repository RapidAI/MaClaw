package inputcorrect

import (
	"path/filepath"
	"strings"
)

func filepathDir(path string) string { return filepath.Dir(path) }

func stringTrim(s string) string { return strings.TrimSpace(s) }
