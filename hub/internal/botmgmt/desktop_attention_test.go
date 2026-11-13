package botmgmt

import "testing"

func TestDesktopAttentionIDStaysWithTheSameAsk(t *testing.T) {
	svc := NewService(nil)
	svc.noteDesktopAttention("tenant-a", "alice", "bot_a", "请登录")
	id := svc.DesktopAttentionID("tenant-a", "alice", "bot_a")
	if id == "" || svc.DesktopAttention("tenant-a", "alice", "bot_a") != "请登录" {
		t.Fatalf("id %q reason %q", id, svc.DesktopAttention("tenant-a", "alice", "bot_a"))
	}
	svc.noteDesktopAttention("tenant-a", "alice", "bot_a", "请登录")
	if svc.DesktopAttentionID("tenant-a", "alice", "bot_a") != id {
		t.Fatal("the same ask was given a new id")
	}
	svc.noteDesktopAttentionFor("tenant-a", "alice", "bot_a", "run_9", "请登录")
	if svc.DesktopAttentionID("tenant-a", "alice", "bot_a") != "run_9" {
		t.Fatalf("run id %q", svc.DesktopAttentionID("tenant-a", "alice", "bot_a"))
	}
	svc.forgetDesktopAttention("tenant-a", "alice", "bot_a")
	if svc.DesktopAttention("tenant-a", "alice", "bot_a") != "" || svc.DesktopAttentionID("tenant-a", "alice", "bot_a") != "" {
		t.Fatal("a forgotten ask is still current")
	}
	if svc.DesktopAttention("tenant-a", "alice", "bot_b") != "" {
		t.Fatal("another bot was touched")
	}
}
