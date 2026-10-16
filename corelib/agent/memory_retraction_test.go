package agent

import (
	"strings"
	"testing"
	"time"
)

func TestRedactMemoryNeedlesInText(t *testing.T) {
	got, changed := RedactMemoryNeedlesInText("Recalled 1 relevant memories\n- [project_knowledge] 服务器 10.0.0.8 当前可达\n", []string{"服务器 10.0.0.8 当前可达"})
	if !changed {
		t.Fatal("expected redaction")
	}
	if strings.Contains(got, "当前可达") {
		t.Fatalf("deleted fact still present: %q", got)
	}
	if !strings.Contains(got, "已从记忆仓库删除") {
		t.Fatalf("missing retraction marker: %q", got)
	}
}

func TestRenderMemoryRetractionOverridesHistory(t *testing.T) {
	r := AdmitMemoryRetraction(nil, MemoryRetractionItem{Kind: "deleted", Content: "服务器 10.0.0.8 当前可达"})
	got := RenderMemoryRetraction(r)
	if !strings.Contains(got, MemoryRetractionMarker) || !strings.Contains(got, "不得当作事实使用") {
		t.Fatalf("render=%q", got)
	}
	if strings.Contains(got, "服务器 10.0.0.8 当前可达") {
		t.Fatalf("deleted body must not be re-injected into the prompt: %q", got)
	}
	if !strings.Contains(got, "已删除 1 条") {
		t.Fatalf("missing delete count: %q", got)
	}
}

func TestApplyPromptOverlaysKeepsAllTails(t *testing.T) {
	ws := &WorkingState{Goal: "继续任务", Next: "下一步"}
	facts := NewSessionFactOverlay()
	AdmitSessionFact(facts, SessionFact{Entity: "ip:10.0.0.8", Claim: "10.0.0.8 当前不可达"})
	retractions := AdmitMemoryRetraction(nil, MemoryRetractionItem{Kind: "deleted", Content: "服务器 10.0.0.8 当前可达"})
	conv := []interface{}{map[string]string{"role": "system", "content": "policy\n\n" + WorkingStateMarker + "\n目标: old\n\n" + SessionFactsMarker + "\n- old"}}
	got := applyPromptOverlays(conv, ws, true, facts, retractions)
	_, content, _ := systemPromptContent(got[0])
	if strings.Count(content, WorkingStateMarker) != 1 || strings.Count(content, SessionFactsMarker) != 1 || strings.Count(content, MemoryRetractionMarker) != 1 {
		t.Fatalf("tail markers broken: %q", content)
	}
	if !strings.Contains(content, "policy") || !strings.Contains(content, "10.0.0.8 当前不可达") || !strings.Contains(content, "已删除") {
		t.Fatalf("missing sections: %q", content)
	}
	if strings.Contains(content, "服务器 10.0.0.8 当前可达") {
		t.Fatalf("deleted warehouse body leaked into prompt: %q", content)
	}
	idxWS := strings.Index(content, WorkingStateMarker)
	idxFacts := strings.Index(content, SessionFactsMarker)
	idxRet := strings.Index(content, MemoryRetractionMarker)
	if !(idxWS < idxFacts && idxFacts < idxRet) {
		t.Fatalf("overlay order = ws:%d facts:%d ret:%d\n%s", idxWS, idxFacts, idxRet, content)
	}
}

func TestDropSessionFactsMatchingDeletedContent(t *testing.T) {
	overlay := NewSessionFactOverlay()
	AdmitSessionFact(overlay, SessionFact{Entity: "ip:10.0.0.8", Claim: "10.0.0.8 当前可达"})
	DropSessionFactsMatching(overlay, []string{"跳板机 10.0.0.8 当前可达，SSH 22"})
	if overlay.Len() != 0 {
		t.Fatalf("overlay still has %d facts", overlay.Len())
	}
}

func TestDropSessionFactsMatchingKeepsContradictoryLiveUpdate(t *testing.T) {
	overlay := NewSessionFactOverlay()
	AdmitSessionFact(overlay, SessionFact{Entity: "ip:10.0.0.8", Claim: "10.0.0.8 当前不可达"})
	DropSessionFactsMatching(overlay, []string{"服务器 10.0.0.8 当前可达，SSH 端口 22"})
	if overlay.Len() != 1 {
		t.Fatalf("live unreachable overlay must survive deleting the old reachable warehouse row, len=%d", overlay.Len())
	}
}

func TestRedactConversationEntriesSkipsUserTurns(t *testing.T) {
	entries := []ConversationEntry{
		{Role: "user", Content: "服务器 10.0.0.8 当前可达，SSH 端口 22"},
		{Role: "tool", ToolName: "memory", Content: "Recalled 1 relevant memories\n- [project_knowledge] 服务器 10.0.0.8 当前可达，SSH 端口 22\n"},
	}
	got, changed := RedactConversationEntries(entries, []string{"服务器 10.0.0.8 当前可达，SSH 端口 22"})
	if !changed {
		t.Fatal("expected tool result redaction")
	}
	if user, _ := got[0].Content.(string); user != "服务器 10.0.0.8 当前可达，SSH 端口 22" {
		t.Fatalf("user turn rewritten: %q", user)
	}
	if tool, _ := got[1].Content.(string); strings.Contains(tool, "当前可达") {
		t.Fatalf("tool turn not redacted: %q", tool)
	}
}

func TestMemoryRetractionExpires(t *testing.T) {
	r := AdmitMemoryRetraction(nil, MemoryRetractionItem{
		Kind:       "deleted",
		Content:    "服务器 10.0.0.8 当前可达，SSH 端口 22",
		AdmittedAt: time.Now().Add(-2 * time.Hour),
	})
	if got := MemoryRetractionNeedles(r); len(got) != 0 {
		t.Fatalf("expired retraction still has needles: %v", got)
	}
	if got := RenderMemoryRetraction(r); got != "" {
		t.Fatalf("expired retraction still rendered: %q", got)
	}
}

func TestDropSessionFactsMatchingRequiresWarehouseToContainClaim(t *testing.T) {
	overlay := NewSessionFactOverlay()
	AdmitSessionFact(overlay, SessionFact{Entity: "ip:10.0.0.81", Claim: "10.0.0.81 当前可达"})
	DropSessionFactsMatching(overlay, []string{"10.0.0.8 当前可达，SSH 22"})
	if overlay.Len() != 1 {
		t.Fatalf("more specific overlay must not drop on a shorter IP prefix, len=%d", overlay.Len())
	}
}

func TestRedactMemoryNeedlesPrefersLongerMatch(t *testing.T) {
	got, changed := RedactMemoryNeedlesInText(
		"note: 服务器 10.0.0.8 当前可达，SSH 端口 22",
		[]string{"10.0.0.8 当前可达", "服务器 10.0.0.8 当前可达，SSH 端口 22"},
	)
	if !changed {
		t.Fatal("expected redaction")
	}
	if strings.Count(got, "已从记忆仓库删除") != 1 {
		t.Fatalf("expected a single longest replacement, got %q", got)
	}
}

func TestExtractSessionFactIgnoresHTTPCurl(t *testing.T) {
	if _, ok := ExtractSessionFactFromTool(
		"bash",
		`{"command":"curl -I http://10.1.2.3/"}`,
		"HTTP/1.1 200 OK\nCache-Control: public, max-age=60\nAge: 0\n",
		ToolExecutionOutcomeOK,
	); ok {
		t.Fatal("HTTP curl output must not become a reachability fact")
	}
}

func TestExtractSessionFactIgnoresMappingFilename(t *testing.T) {
	if _, ok := ExtractSessionFactFromTool(
		"bash",
		`{"command":"cat mapping.json"}`,
		"10.1.2.3 connection refused",
		ToolExecutionOutcomeOK,
	); ok {
		t.Fatal("the word ping inside mapping must not trigger a reachability fact")
	}
}

func TestExtractSessionFactAcceptsWindowsPing(t *testing.T) {
	fact, ok := ExtractSessionFactFromTool(
		"bash",
		`{"command":"ping 10.1.2.3"}`,
		"Reply from 10.1.2.3: bytes=32 time=1ms TTL=64",
		ToolExecutionOutcomeOK,
	)
	if !ok {
		t.Fatal("expected reachable fact from Windows ping")
	}
	if !strings.Contains(fact.Claim, "可达") || strings.Contains(fact.Claim, "不可达") {
		t.Fatalf("claim=%q", fact.Claim)
	}
}

func TestExtractSessionFactIgnoresUnrelatedLogDump(t *testing.T) {
	if _, ok := ExtractSessionFactFromTool(
		"bash",
		`{"command":"cat /var/log/syslog"}`,
		"kernel: 10.1.2.3 connection refused",
		ToolExecutionOutcomeOK,
	); ok {
		t.Fatal("reading a log must not become a session reachability fact")
	}
}

func TestExtractSessionFactIgnoresTimeoutInCommandArgs(t *testing.T) {
	fact, ok := ExtractSessionFactFromTool(
		"bash",
		`{"command":"timeout 5 ping -c 1 10.1.2.3"}`,
		"64 bytes from 10.1.2.3: icmp_seq=1 ttl=64",
		ToolExecutionOutcomeOK,
	)
	if !ok {
		t.Fatal("expected reachable fact from ping result")
	}
	if !strings.Contains(fact.Claim, "可达") || strings.Contains(fact.Claim, "不可达") {
		t.Fatalf("command-line timeout must not flip polarity: %q", fact.Claim)
	}
}
