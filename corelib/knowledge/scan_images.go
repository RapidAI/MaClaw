package knowledge

import (
	"path/filepath"
	"regexp"
	"strings"
)

// ImageReference records where a text document references an image file.
// Used for the two-pass scan: first pass builds references, second pass
// uses them to provide context for standalone images.
type ImageReference struct {
	SourceID      string // Source ID of the referencing document
	NodeID        string // DocumentNode ID where the reference was found
	AltText       string // alt text from the reference syntax
	ContextBefore string // text before the reference (truncated)
	ContextAfter  string // text after the reference (truncated)
	SectionTitle  string // section/heading the reference is under
}

// imageRefPatterns are regexes for detecting image references in text.
var imageRefPatterns = []*regexp.Regexp{
	// Markdown: ![alt](path)
	regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+\.(?:png|jpg|jpeg|gif|webp|bmp|svg))\)`),
	// HTML: <img src="path" alt="...">
	regexp.MustCompile(`<img[^>]+src=["']([^"']+\.(?:png|jpg|jpeg|gif|webp|bmp|svg))["'][^>]*>`),
	// HTML alt extraction helper
	regexp.MustCompile(`alt=["']([^"']*)["']`),
}

// BuildImageReferenceMap scans text DocumentNodes for image file references.
// Returns a map from relative image path → list of references.
// This is the "first pass" of the two-pass import strategy.
func BuildImageReferenceMap(sources []Source, nodesBySource map[string][]DocumentNode, rootPath string) map[string][]ImageReference {
	refs := make(map[string][]ImageReference)

	for _, source := range sources {
		nodes := nodesBySource[source.ID]
		// Determine the directory containing this source file.
		// For imported files, URI is the absolute file path.
		// For URLs/text, URI may be a URL — in that case fall back to rootPath.
		sourceDir := ""
		if source.URI != "" && !strings.Contains(source.URI, "://") {
			sourceDir = filepath.Dir(source.URI)
		}
		if sourceDir == "" || sourceDir == "." {
			sourceDir = rootPath
		}

		var currentSection string
		for _, node := range nodes {
			if node.Type == NodeTypeImage {
				continue
			}

			// Track section titles (heading nodes)
			if node.Level > 0 && node.Title != "" {
				currentSection = node.Title
			}

			text := node.Text
			if text == "" {
				continue
			}

			// Find Markdown image references: ![alt](path)
			for _, match := range imageRefPatterns[0].FindAllStringSubmatch(text, -1) {
				if len(match) >= 3 {
					alt := match[1]
					imgPath := match[2]
					resolvedPath := resolveImageRefPath(imgPath, sourceDir, rootPath)
					if resolvedPath != "" {
						contextBefore, contextAfter := extractRefContext(text, match[0])
						refs[resolvedPath] = append(refs[resolvedPath], ImageReference{
							SourceID:      source.ID,
							NodeID:        node.ID,
							AltText:       alt,
							ContextBefore: contextBefore,
							ContextAfter:  contextAfter,
							SectionTitle:  currentSection,
						})
					}
				}
			}

			// Find HTML image references: <img src="path">
			for _, match := range imageRefPatterns[1].FindAllStringSubmatch(text, -1) {
				if len(match) >= 2 {
					imgPath := match[1]
					resolvedPath := resolveImageRefPath(imgPath, sourceDir, rootPath)
					if resolvedPath == "" {
						continue
					}
					// Try to extract alt text
					var alt string
					if altMatch := imageRefPatterns[2].FindStringSubmatch(match[0]); len(altMatch) >= 2 {
						alt = altMatch[1]
					}
					contextBefore, contextAfter := extractRefContext(text, match[0])
					refs[resolvedPath] = append(refs[resolvedPath], ImageReference{
						SourceID:      source.ID,
						NodeID:        node.ID,
						AltText:       alt,
						ContextBefore: contextBefore,
						ContextAfter:  contextAfter,
						SectionTitle:  currentSection,
					})
				}
			}
		}
	}

	return refs
}

// resolveImageRefPath resolves a relative image reference path to an absolute path.
// Returns empty string if resolution fails or path doesn't look like an image.
func resolveImageRefPath(refPath, sourceDir, rootPath string) string {
	refPath = strings.TrimSpace(refPath)
	if refPath == "" {
		return ""
	}
	// Skip URLs
	if strings.HasPrefix(refPath, "http://") || strings.HasPrefix(refPath, "https://") {
		return ""
	}
	// Skip data URIs
	if strings.HasPrefix(refPath, "data:") {
		return ""
	}

	// Resolve relative to source directory
	var absPath string
	if filepath.IsAbs(refPath) {
		absPath = refPath
	} else {
		absPath = filepath.Join(sourceDir, refPath)
	}

	// Clean and normalize
	absPath = filepath.Clean(absPath)

	// Also try resolving relative to root
	if rootPath != "" {
		altPath := filepath.Clean(filepath.Join(rootPath, refPath))
		// Prefer the path that's within rootPath
		if strings.HasPrefix(altPath, rootPath) {
			absPath = altPath
		}
	}

	return absPath
}

// extractRefContext gets text before and after an image reference in a paragraph.
func extractRefContext(fullText, matchText string) (before, after string) {
	idx := strings.Index(fullText, matchText)
	if idx < 0 {
		return "", ""
	}

	beforeText := fullText[:idx]
	afterText := fullText[idx+len(matchText):]

	before = truncateRunes(strings.TrimSpace(beforeText), 200)
	after = truncateRunes(strings.TrimSpace(afterText), 200)
	return before, after
}

// ClassifyImageKind returns SourceKindImage if the file extension is a supported image.
// Empty string if not an image extension.
func ClassifyImageKind(ext string) string {
	if IsImageExt(ext) {
		return SourceKindImage
	}
	return ""
}
