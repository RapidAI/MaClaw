package steering

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// frontMatter represents the YAML front-matter of a steering file.
type frontMatter struct {
	Inclusion       string   `yaml:"inclusion"`
	FileMatchPattern string  `yaml:"fileMatchPattern"`
	ContextKeywords []string `yaml:"contextKeywords"`
	Priority        *int     `yaml:"priority"`
	Overridable     *bool    `yaml:"overridable"`
}

// ParseFile reads and parses a steering file from disk.
// Returns nil if the file exceeds MaxFileBytes or cannot be parsed.
func ParseFile(path string, scope Scope) (*File, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	if info.Size() > MaxFileBytes {
		return nil, fmt.Errorf("file %s exceeds size limit (%d > %d bytes)", path, info.Size(), MaxFileBytes)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	content := string(data)
	fm, body := splitFrontMatter(content)

	body = strings.TrimSpace(body)
	if body == "" {
		return nil, fmt.Errorf("file %s has no content after front-matter", path)
	}

	f := &File{
		Name:        filepath.Base(path),
		Scope:       scope,
		Inclusion:   InclusionAlways, // default
		Priority:    100,             // default
		Overridable: true,            // default
		Content:     body,
		SourcePath:  path,
		ModTime:     info.ModTime(),
	}

	if fm != nil {
		if fm.Inclusion != "" {
			mode := InclusionMode(fm.Inclusion)
			switch mode {
			case InclusionAlways, InclusionFileMatch, InclusionContextMatch, InclusionManual:
				f.Inclusion = mode
			default:
				// Unknown inclusion mode: fall back to always (safe default).
				f.Inclusion = InclusionAlways
			}
		}
		f.FileMatchPattern = fm.FileMatchPattern
		f.ContextKeywords = fm.ContextKeywords
		if fm.Priority != nil {
			f.Priority = *fm.Priority
		}
		if fm.Overridable != nil {
			f.Overridable = *fm.Overridable
		}
	}

	return f, nil
}

// splitFrontMatter separates YAML front-matter (delimited by ---) from the
// Markdown body. Returns nil frontMatter if no valid front-matter is found.
func splitFrontMatter(content string) (*frontMatter, string) {
	// Front-matter must start at the very beginning of the file.
	trimmed := strings.TrimLeft(content, "\xef\xbb\xbf") // strip BOM
	if !strings.HasPrefix(trimmed, "---") {
		return nil, content
	}

	// Find the closing --- delimiter.
	rest := trimmed[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return nil, content
	}

	fmText := rest[:idx]
	body := rest[idx+4:] // skip "\n---"
	// Strip trailing \r from front-matter (Windows CRLF).
	fmText = strings.TrimRight(fmText, "\r")
	// Strip leading \r\n from body.
	body = strings.TrimLeft(body, "\r\n")

	var fm frontMatter
	if err := yaml.Unmarshal([]byte(fmText), &fm); err != nil {
		// Parse error: treat entire content as body, no front-matter.
		return nil, content
	}

	return &fm, body
}
