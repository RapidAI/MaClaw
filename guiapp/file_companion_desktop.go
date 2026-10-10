package guiapp

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/brand"
)

// fileCompanionMimeTypeLine is the Linux desktop MimeType value. It is the
// preview-type list from the design, including application/octet-stream, and
// it is not a default-handler assignment.
const fileCompanionMimeTypeLine = "text/markdown;text/plain;text/html;text/x-tex;application/pdf;application/rtf;application/msword;application/vnd.openxmlformats-officedocument.wordprocessingml.document;application/vnd.openxmlformats-officedocument.wordprocessingml.template;application/vnd.ms-word.document.macroEnabled.12;application/vnd.oasis.opendocument.text;application/wps;application/wpt;application/vnd.ms-excel;application/vnd.openxmlformats-officedocument.spreadsheetml.sheet;application/vnd.ms-excel.sheet.macroEnabled.12;application/vnd.ms-excel.sheet.binary.macroEnabled.12;application/vnd.oasis.opendocument.spreadsheet;application/et;application/ett;application/vnd.ms-powerpoint;application/vnd.openxmlformats-officedocument.presentationml.presentation;application/vnd.openxmlformats-officedocument.presentationml.slideshow;application/vnd.ms-powerpoint.presentation.macroEnabled.12;application/vnd.oasis.opendocument.presentation;application/dps;application/dpt;image/png;image/jpeg;image/gif;image/webp;image/bmp;image/svg+xml;image/vnd.microsoft.icon;image/tiff;image/heic;video/mp4;video/webm;video/quicktime;video/x-msvideo;video/x-matroska;video/x-m4v;audio/mpeg;audio/wav;audio/ogg;audio/mp4;audio/aac;audio/flac;application/octet-stream;"

// fileCompanionDesktopEntry is the user-level desktop file body. Identical
// content is what the writer compares so a second start does not touch mtime.
func fileCompanionDesktopEntry(exe, displayName string) string {
	exe = filepath.Clean(strings.TrimSpace(exe))
	displayName = strings.TrimSpace(displayName)
	return strings.Join([]string{
		"[Desktop Entry]",
		"Type=Application",
		"Name=用 " + displayName + " 打开",
		"Exec=" + exe + " open-file %F",
		"TryExec=" + exe,
		"MimeType=" + fileCompanionMimeTypeLine,
		"NoDisplay=false",
		"Terminal=false",
		"StartupNotify=true",
		"",
	}, "\n")
}

func fileCompanionDesktopFilePath(dir, brandID string) string {
	return filepath.Join(dir, strings.TrimSpace(brandID)+"-file-companion.desktop")
}

// writeFileCompanionDesktopFile writes one brand's desktop file. The same
// bytes leave the existing mtime alone. It does not write mimeapps.list.
func writeFileCompanionDesktopFile(dir, brandID, content string) (bool, error) {
	path := fileCompanionDesktopFilePath(dir, brandID)
	existing, err := os.ReadFile(path)
	if err == nil && string(existing) == content {
		return false, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// removeFileCompanionDesktopFile deletes only this brand's desktop file.
func removeFileCompanionDesktopFile(dir, brandID string) error {
	err := os.Remove(fileCompanionDesktopFilePath(dir, brandID))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func fileCompanionApplicationsDir() string {
	dir := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return ""
		}
		dir = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dir, "applications")
}

func fileCompanionDesktopBrand() (id, displayName string) {
	current := brand.Current()
	id = strings.TrimSpace(current.ID)
	if id == "" {
		id = "maclaw"
	}
	displayName = strings.TrimSpace(current.DisplayName)
	if displayName == "" {
		displayName = "MaClaw"
	}
	return id, displayName
}
