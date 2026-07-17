package main

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/lansenger"
	"github.com/RapidAI/CodeClaw/corelib/lansengergroupsummary"
)

func TestLansengerGroupSummaryIsCommandAndFilter(t *testing.T) {
	if !lansengergroupsummary.IsSummaryCommand("/summary") {
		t.Fatal("expected /summary")
	}
	if lansengergroupsummary.IsSummaryCommand("/summary please") {
		t.Fatal("extra args should not match bare command")
	}
}

func TestLansengerGroupSummaryRecordAndGenerateEmpty(t *testing.T) {
	app := &App{}
	// Point base dir at temp so store is isolated.
	// getMaclawBaseDir may fall back to home; override via store on service.
	m := newLansengerGatewayManager(app)
	svc := m.groupSummaryService()
	if svc == nil || svc.store == nil {
		t.Fatal("service nil")
	}
	// Replace store root with temp.
	tmp := t.TempDir()
	svc.store = lansengergroupsummary.NewStore(tmp)

	// No messages yet.
	body := svc.generateSummary("g-test", "测试群")
	if !strings.Contains(body, "暂无可摘要") && !strings.Contains(body, "未配置 LLM") {
		// Without LLM configured we still short-circuit after empty check... actually
		// empty check is after LLM check. So without LLM we get config error.
		if !strings.Contains(body, "未配置 LLM") {
			t.Fatalf("unexpected empty response: %q", body)
		}
	}

	// Seed messages directly.
	for i, text := range []string{"Alice: 先讨论方案 A", "Bob: 我倾向方案 B", "/summary"} {
		_, err := svc.store.Append("g-test", "测试群", "", "u"+string(rune('1'+i)), "User", text, time.Now().Add(time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
	}
	msgs, _, err := svc.store.LoadNew("g-test")
	if err != nil {
		t.Fatal(err)
	}
	filtered := lansengergroupsummary.FilterSummaryCommands(msgs)
	if len(filtered) != 2 {
		t.Fatalf("filtered=%d want 2", len(filtered))
	}
	chunks := lansengergroupsummary.BuildChunks(filtered, 6000, 5500, 800)
	if len(chunks) != 1 {
		t.Fatalf("chunks=%d", len(chunks))
	}
}

func TestTryHandleGroupSummaryCommandIgnoresNonCommand(t *testing.T) {
	app := &App{}
	m := newLansengerGatewayManager(app)
	msg := lansenger.IncomingMessage{
		ChatType:   "group",
		GroupID:    "g1",
		FromUserID: "u1",
		Text:       "@Bot hello",
		IsAtMe:     true,
	}
	if m.tryHandleGroupSummaryCommand(msg) {
		t.Fatal("non-command should not be handled")
	}
}

func TestRecordGroupMessageBuffersText(t *testing.T) {
	app := &App{}
	m := newLansengerGatewayManager(app)
	svc := m.groupSummaryService()
	svc.store = lansengergroupsummary.NewStore(t.TempDir())

	m.recordGroupMessage(lansenger.IncomingMessage{
		ChatType:   "group",
		GroupID:    "g1",
		GroupName:  "Team",
		FromUserID: "u1",
		SenderName: "Alice",
		Text:       "讨论一下发布计划",
		MessageID:  "m1",
	})
	msgs, _, err := svc.store.LoadNew("g1")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Text != "讨论一下发布计划" {
		t.Fatalf("msgs=%#v", msgs)
	}

	// Redelivery with same MessageID is ignored.
	m.recordGroupMessage(lansenger.IncomingMessage{
		ChatType:   "group",
		GroupID:    "g1",
		FromUserID: "u1",
		SenderName: "Alice",
		Text:       "讨论一下发布计划",
		MessageID:  "m1",
	})
	msgs, _, _ = svc.store.LoadNew("g1")
	if len(msgs) != 1 {
		t.Fatalf("dedup failed, got %d", len(msgs))
	}

	// p2p ignored
	m.recordGroupMessage(lansenger.IncomingMessage{
		ChatType:   "p2p",
		FromUserID: "u1",
		Text:       "private",
	})
	msgs, _, _ = svc.store.LoadNew("g1")
	if len(msgs) != 1 {
		t.Fatalf("p2p should not append, got %d", len(msgs))
	}
}

func TestGroupSummaryInFlightBusy(t *testing.T) {
	app := &App{}
	m := newLansengerGatewayManager(app)
	svc := m.groupSummaryService()
	if !svc.tryBegin("g1") {
		t.Fatal("first begin should succeed")
	}
	if svc.tryBegin("g1") {
		t.Fatal("second begin should be busy")
	}
	svc.end("g1")
	if !svc.tryBegin("g1") {
		t.Fatal("after end should succeed")
	}
	svc.end("g1")
}

func TestExtendMarkPastSummaryCommandsContiguousOnly(t *testing.T) {
	raw := []lansengergroupsummary.Message{
		{Seq: 10, Text: "real A"},
		{Seq: 11, Text: "real B"},
		{Seq: 12, Text: "/summary"},
		{Seq: 13, Text: "/摘要"},
	}
	// Covered through 11: absorb trailing commands 12,13.
	if got := extendMarkPastSummaryCommands(raw, 11); got != 13 {
		t.Fatalf("after 11 mark=%d want 13", got)
	}
	// Covered through 10: next is real B (11), must NOT jump to /summary.
	if got := extendMarkPastSummaryCommands(raw, 10); got != 10 {
		t.Fatalf("after 10 mark=%d want 10 (must not skip real B)", got)
	}
	// Only commands after empty coverage.
	raw2 := []lansengergroupsummary.Message{
		{Seq: 1, Text: "/summary"},
		{Seq: 2, Text: "/summary"},
	}
	if got := extendMarkPastSummaryCommands(raw2, 0); got != 2 {
		t.Fatalf("commands only mark=%d want 2", got)
	}
}

func TestGroupSummaryServiceAtomicInit(t *testing.T) {
	app := &App{}
	m := newLansengerGatewayManager(app)
	a := m.groupSummaryService()
	b := m.groupSummaryService()
	if a == nil || a != b {
		t.Fatal("expected stable singleton service")
	}
	if m.groupSummaryAtomic.Load() != a {
		t.Fatal("atomic pointer not set")
	}
}
