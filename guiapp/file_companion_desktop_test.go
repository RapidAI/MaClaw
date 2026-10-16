package guiapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFileCompanionDesktopEntryIsBrandScopedAndNotDefault(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "MaClaw")
	body := fileCompanionDesktopEntry(exe, "MaClaw")
	if strings.Contains(body, "[Default Applications]") {
		t.Fatal("desktop file writes a default handler")
	}
	if !strings.Contains(body, "Exec="+exe+" open-file %F") {
		t.Fatalf("exec line missing: %s", body)
	}
	if !strings.Contains(body, "TryExec="+exe) {
		t.Fatal("missing TryExec")
	}
	if !strings.Contains(body, "MimeType="+fileCompanionMimeTypeLine) {
		t.Fatal("mime line drifted")
	}
	if !strings.Contains(body, "application/octet-stream;") {
		t.Fatal("missing octet-stream")
	}
	if strings.Contains(body, "text/x-go") {
		t.Fatal("desktop mime list includes a code type")
	}
	for _, key := range []string{"NoDisplay=false", "Terminal=false", "StartupNotify=true", "Name=用 MaClaw 打开"} {
		if !strings.Contains(body, key) {
			t.Fatalf("missing %s", key)
		}
	}

	dir := t.TempDir()
	changed, err := writeFileCompanionDesktopFile(dir, "maclaw", body)
	if err != nil || !changed {
		t.Fatalf("first write changed=%v err=%v", changed, err)
	}
	info, err := os.Stat(fileCompanionDesktopFilePath(dir, "maclaw"))
	if err != nil {
		t.Fatal(err)
	}
	past := info.ModTime().Add(-2 * time.Hour)
	if err := os.Chtimes(fileCompanionDesktopFilePath(dir, "maclaw"), past, past); err != nil {
		t.Fatal(err)
	}
	changed, err = writeFileCompanionDesktopFile(dir, "maclaw", body)
	if err != nil || changed {
		t.Fatalf("identical write changed=%v err=%v", changed, err)
	}
	again, err := os.Stat(fileCompanionDesktopFilePath(dir, "maclaw"))
	if err != nil {
		t.Fatal(err)
	}
	if !again.ModTime().Equal(past) {
		t.Fatalf("identical content touched mtime: %s", again.ModTime())
	}

	other := fileCompanionDesktopEntry(exe, "TigerClaw")
	if _, err := writeFileCompanionDesktopFile(dir, "qianxin", other); err != nil {
		t.Fatal(err)
	}
	if err := removeFileCompanionDesktopFile(dir, "maclaw"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fileCompanionDesktopFilePath(dir, "maclaw")); !os.IsNotExist(err) {
		t.Fatal("brand file was not removed")
	}
	if _, err := os.Stat(fileCompanionDesktopFilePath(dir, "qianxin")); err != nil {
		t.Fatal("unregister removed another brand")
	}
}
