package main

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// saveScreenshotToFile saves base64-encoded PNG data to a local file and
// returns the absolute file path. A profile-owned Lansenger handler uses its
// private bot data directory; all other handlers retain the legacy shared
// ~/.maclaw/data/screenshots/ location.
func (h *IMMessageHandler) saveScreenshotToFile(base64Data string) (string, error) {
	dir := h.localArtifactDir("screenshots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("cannot create screenshots directory: %w", err)
	}
	fileName := fmt.Sprintf("screenshot_%s_%d.png", time.Now().Format("20060102_150405"), time.Now().UnixMilli()%1000)
	filePath := uniqueLocalDeliveryPath(dir, fileName)
	decoded, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return "", fmt.Errorf("base64 decode failed: %w", err)
	}
	if err := os.WriteFile(filePath, decoded, 0o644); err != nil {
		return "", fmt.Errorf("write file failed: %w", err)
	}
	return filePath, nil
}

// saveFileDataToLocal saves base64-encoded file data locally and returns the
// absolute file path. A profile-owned Lansenger handler writes under its own
// bot data directory; all other handlers retain the legacy
// ~/.maclaw/data/files/ location. If the display name already exists, a unique
// suffix is added so concurrent/repeated deliveries do not overwrite.
func (h *IMMessageHandler) saveFileDataToLocal(name, base64Data string) (string, error) {
	dir := h.localArtifactDir("files")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("cannot create files directory: %w", err)
	}
	return saveBase64FileToDir(dir, name, base64Data)
}

// saveFileDataForLocalDelivery picks the landing directory for a current-
// channel delivery on a local chat (desktop/TUI). The channel IS the user's
// local conversation, so the user-facing copy belongs in the bound workspace
// next to the files the producer tools wrote there — a delivery buried in
// the host artifact store (~/.maclaw/data/files) reads as "not delivered"
// (2026-08-27 weather-PDF turns: the reply showed a store path the user
// never looks at, while the PPT written into the workspace was found at
// once). The artifact-store record stays the audit copy; this is only the
// user-facing materialization. Falls back to the artifact dir when no
// workspace is bound (bot profiles, detached runs).
func (h *IMMessageHandler) saveFileDataForLocalDelivery(ownerID, name, base64Data string) (string, error) {
	if workspace := trustedPrincipalBoundWorkspace(h, ownerID); workspace != "" {
		// The producer tool (office write, file write) usually already landed
		// these exact bytes in the workspace under its real name. Delivering a
		// second, synthetically named copy next to it shows the user two
		// "文件已保存" lines for one file (2026-08-28 birthday-deck turn:
		// 布偶宝宝5岁生日.pptx and attachment_022044_572.pptx, same content).
		// Delivery's endpoint is the user (§4.19), and the bytes are already
		// at a user-visible location — reuse that path instead.
		if existing := findIdenticalWorkspaceFile(workspace, base64Data); existing != "" {
			return existing, nil
		}
		if path, err := saveBase64FileToDir(workspace, name, base64Data); err == nil {
			return path, nil
		}
	}
	return h.saveFileDataToLocal(name, base64Data)
}

// findIdenticalWorkspaceFile returns the path of an existing workspace-root
// file whose content is byte-identical to the decoded payload, or "" when
// none matches. Comparison is size-gated then SHA-256, and root-level only:
// producer tools write deliverables at the workspace root, and the bound
// keeps the scan cheap on large trees.
func findIdenticalWorkspaceFile(workspace, base64Data string) string {
	decoded, err := decodeToolPayloadBase64(base64Data)
	if err != nil || len(decoded) == 0 {
		return ""
	}
	entries, err := os.ReadDir(workspace)
	if err != nil {
		return ""
	}
	want := sha256.Sum256(decoded)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.Size() != int64(len(decoded)) {
			continue
		}
		candidate := filepath.Join(workspace, entry.Name())
		body, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}
		if sha256.Sum256(body) == want {
			return candidate
		}
	}
	return ""
}

func saveBase64FileToDir(dir, name, base64Data string) (string, error) {
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
	filePath := uniqueLocalDeliveryPath(dir, name)
	decoded, err := decodeToolPayloadBase64(base64Data)
	if err != nil {
		return "", fmt.Errorf("base64 decode failed: %w", err)
	}
	if err := os.WriteFile(filePath, decoded, 0o644); err != nil {
		return "", fmt.Errorf("write file failed: %w", err)
	}
	return filePath, nil
}

// localArtifactDir selects a trusted, handler-owned output directory. The
// profile identity is installed when its runtime is created and is never taken
// from tool arguments or model output, so a profile cannot choose another
// bot's artifact directory.
func (h *IMMessageHandler) localArtifactDir(kind string) string {
	if h != nil && h.app != nil {
		if strings.TrimSpace(h.lansengerBotProfileID) != "" {
			if base := lansengerBotDataDir(h.app, h.lansengerBotProfileID); base != "" {
				return filepath.Join(base, "artifacts", kind)
			}
		}
		if base := strings.TrimSpace(h.app.GetDataDir()); base != "" {
			return filepath.Join(base, kind)
		}
	}
	return filepath.Join(corelib.MaclawDataDir(), kind)
}

// uniqueLocalDeliveryPath returns dir/name, or a suffixed path if name already exists.
func uniqueLocalDeliveryPath(dir, name string) string {
	filePath := filepath.Join(dir, name)
	if _, err := os.Stat(filePath); err != nil {
		return filePath
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	if base == "" {
		base = "file"
	}
	for i := 0; i < 1000; i++ {
		candidate := fmt.Sprintf("%s_%s_%03d%s", base, time.Now().Format("150405"), (time.Now().UnixMilli()+int64(i))%1000, ext)
		filePath = filepath.Join(dir, candidate)
		if _, err := os.Stat(filePath); err != nil {
			return filePath
		}
	}
	return filepath.Join(dir, fmt.Sprintf("%s_%d%s", base, time.Now().UnixNano(), ext))
}
