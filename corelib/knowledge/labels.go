package knowledge

import (
	"path/filepath"
	"strings"
)

func ingestLabelsForSource(source Source, manual []string, auto bool) []string {
	labels := append([]string{}, manual...)
	if !auto {
		return normalizeSourceLabels(labels)
	}
	if source.Kind != "" {
		labels = append(labels, "kind:"+source.Kind)
	}
	if source.ProjectPath != "" {
		labels = append(labels, "scope:project")
	} else {
		labels = append(labels, "scope:personal")
	}
	if domain := normalizeURLPolicyDomain(firstNonEmpty(source.SiteName, source.CanonicalURI, source.URI)); domain != "" && source.Kind == SourceKindURL {
		labels = append(labels, "domain:"+domain)
	}
	if folder := sourceFolderLabel(source.RelativePath); folder != "" {
		labels = append(labels, "folder:"+folder)
	}
	return normalizeSourceLabels(labels)
}

func sourceFolderLabel(relativePath string) string {
	relativePath = strings.TrimSpace(relativePath)
	if relativePath == "" {
		return ""
	}
	dir := filepath.Dir(relativePath)
	if dir == "." || dir == "" {
		return ""
	}
	parts := strings.FieldsFunc(filepath.ToSlash(dir), func(r rune) bool {
		return r == '/'
	})
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[0])
}
