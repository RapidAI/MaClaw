package guiapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func resetCompanionShareAnnouncements() {
	companionShareAnnounce.mu.Lock()
	companionShareAnnounce.seen = map[string]time.Time{}
	companionShareAnnounce.mu.Unlock()
}

func TestAnnounceCompanionShareNotificationOnce(t *testing.T) {
	originalShow, originalFlash := ShowNotification, FlashAndBeep
	t.Cleanup(func() { ShowNotification, FlashAndBeep = originalShow, originalFlash })
	resetCompanionShareAnnouncements()
	shown := make([]string, 0, 1)
	ShowNotification = func(title, message string, _ uint32) {
		shown = append(shown, title+"|"+message)
	}
	FlashAndBeep = func() {}
	app := &App{}
	if !app.announceCompanionShareNotification("notice-1", "分享文档", "有人分享了论文.pdf") {
		t.Fatal("first announcement was skipped")
	}
	if app.announceCompanionShareNotification("notice-1", "分享文档", "有人分享了论文.pdf") {
		t.Fatal("the same notice was announced twice")
	}
	if len(shown) != 1 || !strings.Contains(shown[0], "论文.pdf") {
		t.Fatalf("shown = %v", shown)
	}
}

func TestAnnounceCompanionShareNoticesOnlyUnreadShares(t *testing.T) {
	originalShow, originalFlash := ShowNotification, FlashAndBeep
	t.Cleanup(func() { ShowNotification, FlashAndBeep = originalShow, originalFlash })
	resetCompanionShareAnnouncements()
	shown := make([]string, 0, 2)
	ShowNotification = func(title, message string, _ uint32) {
		shown = append(shown, title+"|"+message)
	}
	FlashAndBeep = func() {}
	app := &App{}
	app.announceCompanionShareNotices([]ClientNotification{
		{ID: "share-unread", Title: "分享文档", Content: "论文 A", Source: companionShareNotificationSource},
		{ID: "share-read", Title: "分享文档", Content: "论文 B", Source: companionShareNotificationSource, IsRead: true},
		{ID: "other", Title: "公告", Content: "普通通知", Source: "hub"},
	})
	if len(shown) != 1 || !strings.Contains(shown[0], "论文 A") {
		t.Fatalf("shown = %v", shown)
	}
}

func TestOpenFileCompanionShareFromNotificationUsesLocalCopy(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	file := filepath.Join(t.TempDir(), "shared.pdf")
	if err := os.WriteFile(file, []byte("%PDF-shared"), 0o644); err != nil {
		t.Fatal(err)
	}
	const token = "cd_55555555555555555555555555555555"
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "shared.pdf", Path: file,
		Source: "cloud", Mine: false, Visibility: "group", Revision: 1,
	}); err != nil {
		t.Fatal(err)
	}
	originalLaunch := fileCompanionNotificationLaunch
	t.Cleanup(func() { fileCompanionNotificationLaunch = originalLaunch })
	var launched []string
	fileCompanionNotificationLaunch = func(_ *App, paths []string) error {
		launched = append(launched, paths...)
		return nil
	}
	path, err := app.OpenFileCompanionShareFromNotification(token)
	if err != nil {
		t.Fatal(err)
	}
	if path != file || len(launched) != 1 || launched[0] != file {
		t.Fatalf("path=%q launched=%v", path, launched)
	}
}

func TestOpenFileCompanionShareFromNotificationRejectsABadToken(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if _, err := app.OpenFileCompanionShareFromNotification("not-a-share"); err == nil || !strings.Contains(err.Error(), "分享链接不存在") {
		t.Fatalf("err = %v", err)
	}
}

func TestAnnounceCompanionHostTransferNotices(t *testing.T) {
	originalShow, originalFlash := ShowNotification, FlashAndBeep
	t.Cleanup(func() { ShowNotification, FlashAndBeep = originalShow, originalFlash })
	resetCompanionShareAnnouncements()
	shown := make([]string, 0, 1)
	ShowNotification = func(title, message string, _ uint32) {
		shown = append(shown, title+"|"+message)
	}
	FlashAndBeep = func() {}
	app := &App{}
	app.announceCompanionHostTransferNotices([]ClientNotification{
		{ID: "transfer-1", Title: "主持人转移", Content: "请你接任主持人", Source: companionHostTransferSource},
		{ID: "transfer-2", Title: "主持人变更", Content: "已提名", Source: companionHostTransferNoticeSource, IsRead: true},
		{ID: "share-1", Title: "分享文档", Content: "论文", Source: companionShareNotificationSource},
	})
	if len(shown) != 1 || !strings.HasPrefix(shown[0], "主持人转移|") {
		t.Fatalf("shown = %v", shown)
	}
}
