package desktoppool

import (
	"testing"
	"time"
)

func TestBotGUIUseRecordsKeepsPageAndForgets(t *testing.T) {
	now := time.Date(2026, 11, 10, 7, 30, 0, 0, time.UTC)
	p := &Pool{Now: func() time.Time { return now }}
	if at, page := p.BotGUIUse("tenant-a", "alice"); !at.IsZero() || page != "" {
		t.Fatalf("fresh pool reported use %v %q", at, page)
	}
	p.NoteBotGUIUse("tenant-a", "alice", "http://d:6081/vnc.html")
	now = now.Add(30 * time.Second)
	p.NoteBotGUIUse("tenant-a", "alice", "") // a screenshot keeps the session's page
	at, page := p.BotGUIUse("tenant-a", "alice")
	if !at.Equal(now) || page != "http://d:6081/vnc.html" {
		t.Fatalf("use=%v page=%q", at, page)
	}
	if at, _ := p.BotGUIUse("tenant-a", "bob"); !at.IsZero() {
		t.Fatal("another user's desktop shares the record")
	}
	p.ForgetBotGUIUse("tenant-a", "alice")
	if at, page := p.BotGUIUse("tenant-a", "alice"); !at.IsZero() || page != "" {
		t.Fatalf("stop left the record %v %q", at, page)
	}
	var nilPool *Pool
	nilPool.NoteBotGUIUse("t", "u", "x")
	if at, _ := nilPool.BotGUIUse("t", "u"); !at.IsZero() {
		t.Fatal("nil pool reported use")
	}
}
