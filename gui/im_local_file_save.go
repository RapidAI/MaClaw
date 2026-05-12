package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// saveScreenshotToFile saves base64-encoded PNG data to a local file under
// ~/.maclaw/data/screenshots/ and returns the absolute file path.
func (h *IMMessageHandler) saveScreenshotToFile(base64Data string) (string, error) {
	dir := filepath.Join(corelib.MaclawBaseDir(), "data", "screenshots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("cannot create screenshots directory: %w", err)
	}
	fileName := fmt.Sprintf("screenshot_%s_%d.png", time.Now().Format("20060102_150405"), time.Now().UnixMilli()%1000)
	filePath := filepath.Join(dir, fileName)
	decoded, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return "", fmt.Errorf("base64 decode failed: %w", err)
	}
	if err := os.WriteFile(filePath, decoded, 0o644); err != nil {
		return "", fmt.Errorf("write file failed: %w", err)
	}
	return filePath, nil
}

// saveFileDataToLocal saves base64-encoded file data to ~/.maclaw/data/files/
// and returns the absolute file path.
func (h *IMMessageHandler) saveFileDataToLocal(name, base64Data string) (string, error) {
	dir := filepath.Join(corelib.MaclawBaseDir(), "data", "files")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("cannot create files directory: %w", err)
	}
	if name == "" {
		name = fmt.Sprintf("file_%s_%d", time.Now().Format("20060102_150405"), time.Now().UnixMilli()%1000)
	}
	name = filepath.Base(name)
	if name == "." || name == ".." || name == string(filepath.Separator) {
		name = fmt.Sprintf("file_%s_%d", time.Now().Format("20060102_150405"), time.Now().UnixMilli()%1000)
	}
	filePath := filepath.Join(dir, name)
	decoded, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return "", fmt.Errorf("base64 decode failed: %w", err)
	}
	if err := os.WriteFile(filePath, decoded, 0o644); err != nil {
		return "", fmt.Errorf("write file failed: %w", err)
	}
	return filePath, nil
}
