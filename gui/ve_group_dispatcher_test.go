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

	done := make(chan struct{})
	go func() {
		dispatcher.forwardAgentResponseFiles("session-1", &IMAgentResponse{LocalFilePath: filePath}, true)
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
