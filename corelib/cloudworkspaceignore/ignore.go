// Package cloudworkspaceignore is the source of truth for which workspace
// paths are skipped during cloud-workspace sync.
//
// Hub and clients must use this package (or a thin wrapper) so a user
// .maclaw-cloudignore and the built-in table stay identical on both sides.
package cloudworkspaceignore

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// FileName is the workspace-root ignore file (gitignore subset).
const FileName = ".maclaw-cloudignore"

// ForcedNames are always skipped, even if the user file un-ignores them.
var ForcedNames = []string{".maclaw", ".maclaw-cloud"}

// BuiltinText is the built-in skip table as a gitignore-subset document.
const BuiltinText = `# VCS
.git/
.hg/
.svn/

# deps / build
node_modules/
vendor/
.venv/
venv/
__pycache__/
.pytest_cache/
.mypy_cache/
dist/
build/
target/
out/
bin/
obj/
.next/
.turbo/
coverage/
tmp/
temp/
.cache/

# IDE
.idea/
.vscode/

# product (also force-skipped)
.maclaw/
.maclaw-cloud/

# secrets
.env
.env.*
!.env.example
*.pem
*.key
id_rsa
id_dsa
id_ecdsa
id_ed25519
credentials.json

# volume images
*.iso
*.dmg
`

var builtinPatterns []pattern

func init() {
	builtinPatterns = parseIgnore(BuiltinText)
}

// Matcher applies the built-in table plus a root .maclaw-cloudignore.
type Matcher struct {
	patterns []pattern
}

// NewMatcher compiles built-in rules followed by the workspace-root file.
func NewMatcher(cloudignore string) *Matcher {
	user := parseIgnore(cloudignore)
	patterns := make([]pattern, 0, len(builtinPatterns)+len(user))
	patterns = append(patterns, builtinPatterns...)
	patterns = append(patterns, user...)
	return &Matcher{patterns: patterns}
}

// ShouldIgnore reports whether relPath is excluded from cloud-workspace sync.
// relPath is relative to the workspace root (slash or OS separators).
func ShouldIgnore(relPath string, isDir bool, cloudignore string) bool {
	return NewMatcher(cloudignore).ShouldIgnore(relPath, isDir)
}

// ShouldIgnore reports whether relPath is excluded from cloud-workspace sync.
func (m *Matcher) ShouldIgnore(relPath string, isDir bool) bool {
	if m == nil {
		return NewMatcher("").ShouldIgnore(relPath, isDir)
	}
	rel, ok := normalizeRel(relPath)
	if !ok {
		return true
	}
	if rel == "" {
		return false
	}
	if forced(rel) {
		return true
	}
	if m.decision(rel, isDir) == decIgnore {
		return true
	}
	for {
		slash := strings.LastIndex(rel, "/")
		if slash < 0 {
			return false
		}
		rel = rel[:slash]
		if rel == "" {
			return false
		}
		if forced(rel) {
			return true
		}
		if m.decision(rel, true) == decIgnore {
			return true
		}
	}
}

// ReadCloudignore returns the workspace-root .maclaw-cloudignore contents.
// A missing file is not an error; the result is empty.
func ReadCloudignore(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", nil
	}
	data, err := os.ReadFile(filepath.Join(root, FileName))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

func forced(rel string) bool {
	for _, seg := range strings.Split(rel, "/") {
		for _, name := range ForcedNames {
			// Windows can surface .Maclaw-cloud; force-skip must not depend on case.
			if strings.EqualFold(seg, name) {
				return true
			}
		}
	}
	return false
}

func normalizeRel(p string) (string, bool) {
	p = strings.ReplaceAll(strings.TrimSpace(p), `\`, "/")
	if p == "" {
		return "", true
	}
	p = path.Clean(p)
	p = strings.TrimPrefix(p, "./")
	p = strings.Trim(p, "/")
	if p == "." {
		return "", true
	}
	if p == ".." || strings.HasPrefix(p, "../") {
		return "", false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", false
		}
	}
	return p, true
}
