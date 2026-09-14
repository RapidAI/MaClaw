package guiapp

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestSessionFactsPersistAcrossContinueAndClearOnNewTask(t *testing.T) {
	h := &IMMessageHandler{}
	const userID = "desktop-user:D:/proj"
	h.admitSessionFact(userID, nil, agent.SessionFact{
		Entity: "ip:10.0.0.9",
		Claim:  "10.0.0.9 当前不可达",
	})
	if h.loadSessionFacts(userID) == nil || h.loadSessionFacts(userID).Len() != 1 {
		t.Fatal("overlay should persist on the handler")
	}

	var b strings.Builder
	h.appendSessionFactsSection(&b, userID, nil)
	if !strings.Contains(b.String(), agent.SessionFactsMarker) || !strings.Contains(b.String(), "10.0.0.9 当前不可达") {
		t.Fatalf("prompt section missing overlay: %q", b.String())
	}

	h.clearPerUserSessionState(userID)
	if h.loadSessionFacts(userID) != nil {
		t.Fatal("new task / session reset must drop the overlay")
	}
}

func TestDeleteMemoryRetractsLiveAgentHistory(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Stop() })
	if err := store.Save(memory.Entry{Content: "服务器 10.0.0.8 当前可达，SSH 端口 22", Category: memory.CategoryProjectKnowledge}); err != nil {
		t.Fatal(err)
	}
	entry := store.List("", "")[0]
	cm := agent.NewConversationMemory()
	t.Cleanup(cm.Stop)
	cm.Save("desktop-user:D:/proj", []agent.ConversationEntry{
		{Role: "user", Content: "连那台机器"},
		{Role: "tool", ToolName: "memory", Content: "Recalled 1 relevant memories\n- [project_knowledge] 服务器 10.0.0.8 当前可达，SSH 端口 22\n"},
	})
	handler := &IMMessageHandler{memoryStore: store, memory: cm}
	handler.admitSessionFact("desktop-user:D:/proj", nil, agent.SessionFact{
		Entity: "ip:10.0.0.8",
		Claim:  "10.0.0.8 当前可达",
	})
	app := &App{memoryStore: store, imHandler: handler, testHomeDir: t.TempDir()}
	if err := app.DeleteMemory(entry.ID); err != nil {
		t.Fatalf("DeleteMemory: %v", err)
	}
	history := cm.Load("desktop-user:D:/proj")
	if len(history) < 2 {
		t.Fatalf("history missing: %#v", history)
	}
	toolText, _ := history[1].Content.(string)
	if strings.Contains(toolText, "当前可达") {
		t.Fatalf("deleted fact still in live history: %q", toolText)
	}
	if handler.loadSessionFacts("desktop-user:D:/proj") != nil {
		t.Fatal("session overlay still holds the deleted fact")
	}
	rendered := agent.RenderMemoryRetraction(handler.LoadMemoryRetractions())
	if !strings.Contains(rendered, agent.MemoryRetractionMarker) || !strings.Contains(rendered, "已删除") {
		t.Fatalf("missing retraction prompt: %q", rendered)
	}
	if strings.Contains(rendered, "SSH 端口 22") {
		t.Fatalf("deleted warehouse body must not be re-injected: %q", rendered)
	}
}

func TestSyncVerifiedFactUpdatesMemoryWarehouse(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Stop() })
	if err := store.Save(memory.Entry{
		Content:  "跳板机 10.9.8.7 当前可达",
		Category: memory.CategoryProjectKnowledge,
		Status:   memory.StatusActive,
		OwnerID:  "desktop-user:proj",
	}); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{memoryStore: store}
	h.syncVerifiedFactToStores("desktop-user:proj", agent.SessionFact{
		Entity:   "ip:10.9.8.7",
		Claim:    "10.9.8.7 当前不可达",
		Evidence: "bash: 100% packet loss",
	})
	for _, e := range store.RecallDynamicForTool("10.9.8.7", memory.CategoryProjectKnowledge, "", "desktop-user:proj") {
		if e.IsActive() && strings.Contains(e.Content, "可达") && !strings.Contains(e.Content, "不可达") {
			t.Fatalf("stale reachable memory still recalled: %s", e.Content)
		}
	}
}

func TestAdmitSessionFactFromMemorySave(t *testing.T) {
	h := &IMMessageHandler{}
	h.admitSessionFactFromMemorySave("desktop-user", "跳板机 8.8.8.8 已经不通了")
	got := h.loadSessionFacts("desktop-user")
	if got == nil || got.Len() != 1 {
		t.Fatal("expected overlay fact from memory save")
	}
	fact, ok := agent.LookupSessionFact(got, "ip:8.8.8.8")
	if !ok || !strings.Contains(fact.Claim, "不可达") {
		t.Fatalf("fact=%#v", fact)
	}
}
