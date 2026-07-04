package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/a2a"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestSendQueuedGroupMessagesCoalescesStreamChunks(t *testing.T) {
	got := make(chan a2a.GroupDiscussionMessage, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/a2a/consultations/session-1/messages":
			var msg a2a.GroupDiscussionMessage
			if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
				t.Errorf("decode body: %v", err)
				return
			}
			got <- msg
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	dispatcher := NewGroupChatDispatcher(app)
	messages := make(chan a2a.GroupDiscussionMessage, 4)
	messages <- a2a.GroupDiscussionMessage{Kind: a2a.MessageStatement, Content: "question"}
	messages <- a2a.GroupDiscussionMessage{Kind: a2a.MessageStreamChunk, Content: "hello "}
	messages <- a2a.GroupDiscussionMessage{Kind: a2a.MessageStreamChunk, Content: "world"}
	messages <- a2a.GroupDiscussionMessage{Kind: a2a.MessageStreamEnd}
	close(messages)

	dispatcher.sendQueuedGroupMessages("session-1", messages)
	close(got)

	var sent []a2a.GroupDiscussionMessage
	for msg := range got {
		sent = append(sent, msg)
	}
	if len(sent) != 3 {
		t.Fatalf("sent messages = %d, want 3: %#v", len(sent), sent)
	}
	if sent[0].Kind != a2a.MessageStatement || sent[0].Content != "question" {
		t.Fatalf("first message = %#v", sent[0])
	}
	if sent[1].Kind != a2a.MessageStreamChunk || sent[1].Content != "hello world" {
		t.Fatalf("coalesced chunk = %#v", sent[1])
	}
	if sent[2].Kind != a2a.MessageStreamEnd {
		t.Fatalf("last message = %#v", sent[2])
	}
}

func TestForwardAgentResponseFilesLocalDispatchDoesNotWaitForHubUpload(t *testing.T) {
	releaseUpload := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ve/files/upload":
			<-releaseUpload
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "file_id": "file-1", "file_url": "/api/ve/files/file-1"})
		case "/api/a2a/consultations/session-1/messages":
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	defer close(releaseUpload)

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "report.pdf")
	if err := os.WriteFile(filePath, []byte("pdf"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineID: "machine-1", RemoteMachineToken: "token-1", GroupDiscussion: corelib.GroupDiscussionConfig{Enabled: true}}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	dispatcher := NewGroupChatDispatcher(app)
	sess := &groupExecutorSession{SessionID: "session-1"}

	done := make(chan struct{})
	go func() {
		dispatcher.forwardAgentResponseFiles(sess, &IMAgentResponse{LocalFilePath: filePath}, true)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("local dispatch waited for Hub file upload")
	}
}

func TestUniqueVEFilePathsTrimsAndDeduplicates(t *testing.T) {
	paths := uniqueVEFilePaths([]string{" ", ` C:\tmp\report.pdf `, `c:\TMP\report.pdf`, `D:\tmp\other.pdf`})
	if len(paths) != 2 {
		t.Fatalf("paths = %#v, want 2 unique paths", paths)
	}
	if paths[0] != `C:\tmp\report.pdf` || paths[1] != `D:\tmp\other.pdf` {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestGroupExecutorSenderContextGuardsVisibleReply(t *testing.T) {
	got := groupExecutorSenderContext(" user-1 ")
	if !strings.Contains(got, "from group participant user-1") {
		t.Fatalf("context = %q", got)
	}
	if !strings.Contains(got, "no hidden reasoning or meta notes") {
		t.Fatalf("context missing visible-reply guard: %q", got)
	}
	if strings.Contains(got, "\u93c9") || strings.Contains(got, "\u9225") || strings.Contains(got, "?") {
		t.Fatalf("context contains mojibake: %q", got)
	}
}

func TestBuildGroupExecutorDiscussionContextIncludesPriorGroupMessages(t *testing.T) {
	detail := a2a.HubDiscussionDetail{
		Discussion: a2a.HubDiscussionSummary{Topic: "朱禄的数字分身", Question: "介绍数字分身", ParticipantIDs: []string{"machine-1", "zhulu-machine", "local-maclaw"}},
		Session: &a2a.Session{Participants: []a2a.Participant{
			{ID: "machine-1", RoleCode: "initiator"},
			{ID: "zhulu-machine", RoleCode: "speak"},
			{ID: "local-maclaw", RoleCode: "speak"},
		}},
		Messages: []a2a.Message{
			{ID: "m1", FromID: "zhulu-machine", Kind: a2a.MessageStatement, Content: "朱禄的数字分身擅长网络流量处理、高速转发、流量特征提取和深度学习。"},
			{ID: "m2", FromID: "machine-1", Kind: a2a.MessageStatement, Content: "@本机AI 基于刚才对方介绍的资料，做个朱禄的数字分身的介绍 ppt"},
		},
	}

	got := buildGroupExecutorDiscussionContext(detail, a2a.GroupDiscussionMessage{FromID: "machine-1", Content: "@本机AI 基于刚才对方介绍的资料，做个朱禄的数字分身的介绍 ppt"}, "machine-1")
	if !strings.Contains(got, "朱禄的数字分身") || !strings.Contains(got, "高速转发") || !strings.Contains(got, "local AI") {
		t.Fatalf("context missing group facts: %q", got)
	}
	if strings.Contains(got, "@本机AI 基于刚才") {
		t.Fatalf("context should not duplicate current local-targeted input: %q", got)
	}
}

func TestBuildGroupExecutorDiscussionContextSkipsSyncedCurrentInputWithoutFromID(t *testing.T) {
	detail := a2a.HubDiscussionDetail{
		Messages: []a2a.Message{
			{ID: "m1", FromID: "zhulu-machine", Kind: a2a.MessageStatement, Content: "朱禄的数字分身擅长流量特征提取。"},
			{ID: "m2", FromID: "machine-1", Kind: a2a.MessageStatement, Content: "@本机AI 做介绍PPT"},
		},
	}

	got := buildGroupExecutorDiscussionContext(detail, a2a.GroupDiscussionMessage{Content: "@本机AI 做介绍PPT"}, "machine-1")
	if strings.Contains(got, "@本机AI 做介绍PPT") {
		t.Fatalf("context should skip current input even when current FromID is empty: %q", got)
	}
	if !strings.Contains(got, "流量特征提取") {
		t.Fatalf("context should keep prior group facts: %q", got)
	}
}

func TestBuildGroupExecutorDiscussionContextKeepsOlderRepeatedInput(t *testing.T) {
	detail := a2a.HubDiscussionDetail{
		Messages: []a2a.Message{
			{ID: "m1", FromID: "machine-1", Kind: a2a.MessageStatement, Content: "repeat request"},
			{ID: "m2", FromID: "anna", Kind: a2a.MessageStatement, Content: "answer between repeats"},
			{ID: "m3", FromID: "machine-1", Kind: a2a.MessageStatement, Content: "repeat request"},
		},
	}

	got := buildGroupExecutorDiscussionContext(detail, a2a.GroupDiscussionMessage{FromID: "machine-1", Kind: a2a.MessageStatement, Content: "repeat request"}, "machine-1")
	if strings.Count(got, "repeat request") != 1 {
		t.Fatalf("context should keep older repeated input and skip only current: %q", got)
	}
	if !strings.Contains(got, "answer between repeats") {
		t.Fatalf("context should keep intervening messages: %q", got)
	}
}

func TestGroupExecutorParticipantLinesDoesNotLabelInitiatorAsLocalAI(t *testing.T) {
	detail := a2a.HubDiscussionDetail{Session: &a2a.Session{Participants: []a2a.Participant{
		{ID: "machine-1", RoleCode: "initiator"},
		{ID: "local-maclaw", RoleCode: "speak"},
	}}}

	got := groupExecutorParticipantLines(detail, "machine-1")
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "machine-1 (local AI)") {
		t.Fatalf("initiator machine should not be labelled local AI: %q", joined)
	}
	if !strings.Contains(joined, "local-maclaw (local AI)") {
		t.Fatalf("local AI alias should be labelled: %q", joined)
	}
}

func TestBuildGroupExecutorDiscussionContextCoalescesStreamChunks(t *testing.T) {
	detail := a2a.HubDiscussionDetail{
		Messages: []a2a.Message{
			{ID: "m1", FromID: "zhulu-machine", Kind: a2a.MessageStreamChunk, Content: "朱禄擅长"},
			{ID: "m2", FromID: "zhulu-machine", Kind: a2a.MessageStreamChunk, Content: "网络流量处理"},
			{ID: "m3", FromID: "zhulu-machine", Kind: a2a.MessageStreamEnd},
			{ID: "m4", FromID: "machine-1", Kind: a2a.MessageStatement, Content: "@本机AI 做介绍PPT"},
		},
	}

	got := buildGroupExecutorDiscussionContext(detail, a2a.GroupDiscussionMessage{FromID: "machine-1", Content: "@本机AI 做介绍PPT"}, "machine-1")
	if !strings.Contains(got, "[zhulu-machine] 朱禄擅长网络流量处理") {
		t.Fatalf("context did not coalesce stream chunks: %q", got)
	}
	if strings.Count(got, "zhulu-machine") != 1 {
		t.Fatalf("stream chunks should appear as one prior message: %q", got)
	}
}

func TestBuildGroupExecutorDiscussionContextPreservesStreamChunkSpacing(t *testing.T) {
	detail := a2a.HubDiscussionDetail{
		Messages: []a2a.Message{
			{ID: "m1", FromID: "anna", Kind: a2a.MessageStreamChunk, Content: "Hello "},
			{ID: "m2", FromID: "anna", Kind: a2a.MessageStreamChunk, Content: "world"},
			{ID: "m3", FromID: "anna", Kind: a2a.MessageStreamEnd},
		},
	}

	got := buildGroupExecutorDiscussionContext(detail, a2a.GroupDiscussionMessage{}, "machine-1")
	if !strings.Contains(got, "[anna] Hello world") {
		t.Fatalf("context should preserve stream chunk spacing: %q", got)
	}
}

func TestBuildGroupExecutorDiscussionContextIncludesCompressedMemory(t *testing.T) {
	detail := a2a.HubDiscussionDetail{
		Session: &a2a.Session{ContextSummary: "[compressed shared group memory]\n- [anna] earlier important fact"},
		Messages: []a2a.Message{
			{ID: "m99", FromID: "machine-1", Kind: a2a.MessageStatement, Content: "@本机AI continue"},
		},
	}

	got := buildGroupExecutorDiscussionContext(detail, a2a.GroupDiscussionMessage{ID: "m99", FromID: "machine-1", Content: "@本机AI continue"}, "machine-1")
	if !strings.Contains(got, "Shared compressed memory") || !strings.Contains(got, "earlier important fact") {
		t.Fatalf("context should include compressed memory: %q", got)
	}
}

func TestBuildGroupExecutorDiscussionContextUsesOnlyPostSummaryRecentMessages(t *testing.T) {
	detail := a2a.HubDiscussionDetail{
		Session: &a2a.Session{ContextSummary: "[compressed shared group memory]\n- [anna] summarized old fact", SummaryUpToID: "m2"},
		Messages: []a2a.Message{
			{ID: "m1", FromID: "anna", Kind: a2a.MessageStatement, Content: "old detail one"},
			{ID: "m2", FromID: "anna", Kind: a2a.MessageStatement, Content: "old detail two"},
			{ID: "m3", FromID: "anna", Kind: a2a.MessageStatement, Content: "recent detail"},
		},
	}

	got := buildGroupExecutorDiscussionContext(detail, a2a.GroupDiscussionMessage{}, "machine-1")
	if strings.Contains(got, "old detail one") || strings.Contains(got, "old detail two") {
		t.Fatalf("context should not repeat summarized raw messages: %q", got)
	}
	if !strings.Contains(got, "summarized old fact") || !strings.Contains(got, "recent detail") {
		t.Fatalf("context should include summary and post-summary recent messages: %q", got)
	}
}

func TestSanitizeVEGroupExecutorHistoryClearsReasoningFieldsOnly(t *testing.T) {
	history := []agent.ConversationEntry{
		{Role: "user", Content: "weather"},
		{Role: "assistant", Content: "visible planning note", ReasoningContent: "hidden"},
		{Role: "assistant", Content: "assistant planning note", ToolCalls: []map[string]string{{"name": "manage_skill"}}, ReasoningContent: "hidden"},
		{Role: "tool", Content: "tool result", ToolCallID: "call-1"},
		{Role: "assistant", Content: "Sunny, 35C."},
	}

	got := sanitizeVEGroupExecutorHistory("ve-group-executor:session-1", history)
	if len(got) != 5 {
		t.Fatalf("history len = %d, want 5: %+v", len(got), got)
	}
	if got[1].Content != "visible planning note" || got[1].ReasoningContent != "" {
		t.Fatalf("assistant reasoning field not sanitized correctly: %+v", got[1])
	}
	if got[2].Content != "assistant planning note" || got[2].ReasoningContent != "" || got[2].ToolCalls == nil {
		t.Fatalf("tool-call assistant entry not structurally sanitized correctly: %+v", got[2])
	}
	if got[4].Content != "Sunny, 35C." {
		t.Fatalf("visible assistant content changed: %+v", got[4])
	}
}
