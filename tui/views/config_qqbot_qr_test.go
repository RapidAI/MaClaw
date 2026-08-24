package views

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	tea "github.com/charmbracelet/bubbletea"
)

func TestConfigQQBotScanEnterStartsOverlay(t *testing.T) {
	m := NewConfigModel("en")
	m.width = 96
	m.height = 32
	m.activeTab = CfgTabIM
	m.LoadFromAppConfig(corelib.AppConfig{QQBotEnabled: true, QQBotAppID: "1020", QQBotAppSecret: "secret"})
	moveConfigCursorToKey(t, &m, "qqbot_qr_login")

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.qqBotQROverlayActive() || !m.IsEditing() {
		t.Fatalf("overlay should start, overlay=%v editing=%v", m.qqBotQROverlayActive(), m.IsEditing())
	}
	if cmd == nil {
		t.Fatal("expected scan command")
	}
	var sawScan bool
	collectTeaMsgs(cmd, func(msg tea.Msg) {
		if _, ok := msg.(ConfigQQBotScanMsg); ok {
			sawScan = true
		}
	})
	if !sawScan {
		t.Fatal("enter should emit ConfigQQBotScanMsg")
	}
	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "QQ Bot scan bind") || !strings.Contains(view, "Requesting QR code") {
		t.Fatalf("overlay view missing scan copy:\n%s", view)
	}
}

func TestConfigQQBotQROverlayPollAndCancel(t *testing.T) {
	m := NewConfigModel("zh")
	m.width = 96
	m.height = 32
	m, cmd := m.startQQBotQROverlay(true)
	if cmd == nil {
		t.Fatal("start overlay should emit commands")
	}

	m, cmd = m.Update(ConfigQQBotQRMsg{Success: true, QR: "https://q.qq.com/qqbot/openclaw/connect.html?task_id=t1", Token: "t1"})
	if cmd == nil {
		t.Fatal("QR success should emit poll")
	}
	if m.qqbotToken != "t1" || m.qqbotQR == "" {
		t.Fatalf("qr state token=%q qr=%q", m.qqbotToken, m.qqbotQR)
	}
	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "QQ 扫码绑定") {
		t.Fatalf("zh overlay missing title:\n%s", view)
	}

	m, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.qqBotQROverlayActive() {
		t.Fatal("esc should close overlay")
	}
	if cmd == nil {
		t.Fatal("esc should cancel token")
	}
	if msg, ok := cmd().(ConfigQQBotCancelMsg); !ok || msg.Token != "t1" {
		t.Fatalf("cancel msg = %#v", cmd())
	}
}

func TestConfigQQBotLateQRMsgCancelsToken(t *testing.T) {
	m := NewConfigModel("en")
	m.width = 80
	m.height = 24
	m, _ = m.startQQBotQROverlay(true)
	m.clearQQBotQROverlay()
	m, cmd := m.Update(ConfigQQBotQRMsg{Success: true, QR: "https://q.qq.com/qqbot/openclaw/connect.html?task_id=late", Token: "late-task"})
	if m.qqBotQROverlayActive() {
		t.Fatal("late QR should not reopen overlay")
	}
	if cmd == nil {
		t.Fatal("late QR should cancel the unused bind session")
	}
	if msg, ok := cmd().(ConfigQQBotCancelMsg); !ok || msg.Token != "late-task" {
		t.Fatalf("late cancel msg = %#v", cmd())
	}
}

func TestConfigQQBotQROverlayConfirmedClearsAndExpiredRefreshes(t *testing.T) {
	m := NewConfigModel("en")
	m.width = 80
	m.height = 24
	m, _ = m.startQQBotQROverlay(true)
	m, _ = m.Update(ConfigQQBotQRMsg{Success: true, QR: "https://example.invalid/qr", Token: "task-a"})
	m, cmd := m.Update(ConfigQQBotPollResultMsg{Token: "task-a", Status: "confirmed", Success: true, Completed: true, AppID: "102088"})
	if cmd != nil {
		t.Fatalf("confirmed overlay should not keep polling, cmd=%T", cmd())
	}
	if m.qqBotQROverlayActive() {
		t.Fatal("confirmed should close overlay")
	}

	m, _ = m.startQQBotQROverlay(true)
	m, _ = m.Update(ConfigQQBotQRMsg{Success: true, QR: "https://example.invalid/qr", Token: "task-b"})
	m, cmd = m.Update(ConfigQQBotPollResultMsg{Token: "task-b", Status: "expired", Completed: true})
	if !m.qqBotQROverlayActive() || !m.qqbotBusy {
		t.Fatalf("expired should refresh overlay, overlay=%v busy=%v", m.qqBotQROverlayActive(), m.qqbotBusy)
	}
	var sawScan bool
	collectTeaMsgs(cmd, func(msg tea.Msg) {
		if _, ok := msg.(ConfigQQBotScanMsg); ok {
			sawScan = true
		}
	})
	if !sawScan {
		t.Fatal("expired should request a new QR")
	}
}

func TestConfigQQBotEmptyQRCancelsToken(t *testing.T) {
	m := NewConfigModel("en")
	m.width = 80
	m.height = 24
	m, _ = m.startQQBotQROverlay(true)
	m, cmd := m.Update(ConfigQQBotQRMsg{Success: true, Token: "empty-url-task"})
	if m.qqbotToken != "" || m.qqbotQR != "" {
		t.Fatalf("empty QR should not start poll, token=%q qr=%q", m.qqbotToken, m.qqbotQR)
	}
	if cmd == nil {
		t.Fatal("empty QR with token should cancel the bind session")
	}
	if msg, ok := cmd().(ConfigQQBotCancelMsg); !ok || msg.Token != "empty-url-task" {
		t.Fatalf("cancel msg = %#v", cmd())
	}

	m, _ = m.startQQBotQROverlay(true)
	m, cmd = m.Update(ConfigQQBotQRMsg{Success: false, Token: "failed-task", Message: "boom"})
	if m.qqbotQR != "" || m.qqbotToken != "" {
		t.Fatalf("failed start should not keep QR, token=%q qr=%q", m.qqbotToken, m.qqbotQR)
	}
	if cmd == nil {
		t.Fatal("failed start with token should cancel the bind session")
	}
	if msg, ok := cmd().(ConfigQQBotCancelMsg); !ok || msg.Token != "failed-task" {
		t.Fatalf("failed cancel msg = %#v", cmd())
	}
}

func TestConfigQQBotScanRequestedWhileBusy(t *testing.T) {
	m := NewConfigModel("en")
	if m.QQBotQRScanRequested() {
		t.Fatal("idle overlay should not request a scan")
	}
	m, _ = m.startQQBotQROverlay(true)
	if !m.QQBotQRScanRequested() {
		t.Fatal("busy overlay should request a scan")
	}
	if m.QQBotQRPollTokenMatches("t1") {
		t.Fatal("poll token should not match before QR arrives")
	}
	m, _ = m.Update(ConfigQQBotQRMsg{Success: true, QR: "https://q.qq.com/qqbot/openclaw/connect.html?task_id=t1", Token: "t1"})
	if m.QQBotQRScanRequested() {
		t.Fatal("waiting overlay should not request another scan")
	}
	if !m.QQBotQRPollTokenMatches("t1") {
		t.Fatal("poll token should match the active QR")
	}
	m.clearQQBotQROverlay()
	if m.QQBotQRScanRequested() || m.QQBotQRPollTokenMatches("t1") {
		t.Fatal("cleared overlay should drop scan and poll matching")
	}
}

func TestConfigFields_IMTabShowsQQBotQRLoginForQQProfile(t *testing.T) {
	m := NewConfigModel("zh")
	m.activeTab = CfgTabIM
	m.LoadFromAppConfig(corelib.AppConfig{QQBotEnabled: true})
	for _, visible := range []string{"im_channel_profile", "qqbot_enabled", "qqbot_qr_login", "qqbot_app_id", "qqbot_app_secret"} {
		if !visibleConfigEntryExists(m.currentEntries(), visible) {
			t.Fatalf("%s should be visible for qq IM profile", visible)
		}
	}
	_, _, entry := findConfigEntryForTest(t, m, "qqbot_qr_login")
	if !entry.ReadOnly {
		t.Fatal("qqbot_qr_login should be read-only")
	}
}

func collectTeaMsgs(cmd tea.Cmd, visit func(tea.Msg)) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, next := range batch {
			collectTeaMsgs(next, visit)
		}
		return
	}
	visit(msg)
}
