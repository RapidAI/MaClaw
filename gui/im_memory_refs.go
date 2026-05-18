package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const memoryRefPreviewRunes = 800

var memoryRefUnsafeNameChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func writeMemoryRefFile(memoryStorePath, userID, kind, content string, now time.Time) (string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", fmt.Errorf("memory ref content is empty")
	}
	baseDir := strings.TrimSpace(memoryStorePath)
	if baseDir == "" {
		return "", fmt.Errorf("memory store path is empty")
	}
	baseDir = filepath.Dir(baseDir)
	kind = safeMemoryRefPathPart(kind)
	if kind == "" {
		kind = "generic"
	}
	owner := safeMemoryRefPathPart(userID)
	if owner == "" {
		owner = "default"
	}
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()

	dir := filepath.Join(baseDir, "memory_refs", kind, owner, now.Format("2006-01"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	sum := sha256.Sum256([]byte(content))
	name := fmt.Sprintf("%s-%s.md", now.Format("20060102T150405.000000000Z"), hex.EncodeToString(sum[:])[:12])
	path := filepath.Join(dir, name)
	body := fmt.Sprintf(`---
kind: %s
owner: %s
created_at: %s
sha256: %s
---

%s
`, kind, owner, now.Format(time.RFC3339Nano), hex.EncodeToString(sum[:]), content)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func safeMemoryRefPathPart(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = memoryRefUnsafeNameChars.ReplaceAllString(s, "_")
	s = strings.Trim(s, "._-")
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

func memoryRefPreview(content string) string {
	content = strings.TrimSpace(content)
	return truncateRunes(content, memoryRefPreviewRunes)
}
