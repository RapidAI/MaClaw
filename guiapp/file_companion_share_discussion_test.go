package guiapp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func resetFileCompanionShareDiscussionLimits(token string) {
	delete(fileCompanionShareDiscussionReplies, token)
	for key := range fileCompanionShareDiscussionReady {
		if strings.HasPrefix(key, token+"|") {
			delete(fileCompanionShareDiscussionReady, key)
		}
	}
	for key := range fileCompanionShareDiscussionFailures {
		if strings.HasPrefix(key, token+"|") {
			delete(fileCompanionShareDiscussionFailures, key)
		}
	}
}

func TestFileCompanionPendingShareNotesKeepsOnlyUnansweredReaderNotes(t *testing.T) {
	since := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	notes := []FileCompanionShareNote{
		{ID: "cn_old", Author: "reader", Text: "旧批注", CreatedAt: "2026-10-27T09:00:00Z"},
		{ID: "cn_new", Author: "reader", Text: "新批注", CreatedAt: "2026-10-27T10:01:00Z"},
		{ID: "cn_held", Author: "reader", Text: "待审核", CreatedAt: "2026-10-27T10:01:30Z", Decision: "pending"},
		{ID: "cn_skip", Author: "reader", Text: "已忽略", CreatedAt: "2026-10-27T10:01:40Z", Decision: "ignored"},
		{ID: "cn_host", Author: "host", Text: "主持人的话", CreatedAt: "2026-10-27T10:02:00Z", Host: true},
		{ID: "cn_answered", Author: "reader", Text: "已经被回答", CreatedAt: "2026-10-27T10:03:00Z"},
		{ID: "cn_reply", Author: "host", Text: "回答", CreatedAt: "2026-10-27T10:04:00Z", Host: true, ReplyTo: "cn_answered"},
		{ID: "cn_notime", Author: "reader", Text: "没有时间", CreatedAt: ""},
	}
	got := fileCompanionPendingShareNotes(notes, since)
	if len(got) != 1 || got[0].ID != "cn_new" {
		t.Fatalf("pending notes = %+v", got)
	}
}

func TestFileCompanionApprovedShareNotesWaitsForHostReview(t *testing.T) {
	notes := []FileCompanionShareNote{
		{ID: "cn_pending", Author: "reader", Text: "待审核", CreatedAt: "2026-10-27T10:00:00Z", Decision: "pending"},
		{ID: "cn_ignored", Author: "reader", Text: "已忽略", CreatedAt: "2026-10-27T10:01:00Z", Decision: "ignored"},
		{ID: "cn_ok", Author: "reader", Text: "已通过", CreatedAt: "2026-10-27T10:02:00Z", Decision: "approved"},
		{ID: "cn_answered", Author: "reader", Text: "已经回答", CreatedAt: "2026-10-27T10:03:00Z", Decision: "approved"},
		{ID: "cn_reply", Author: "host", Text: "回答", CreatedAt: "2026-10-27T10:04:00Z", Host: true, ReplyTo: "cn_answered"},
	}
	got := fileCompanionApprovedShareNotes(notes)
	if len(got) != 1 || got[0].ID != "cn_ok" {
		t.Fatalf("approved notes = %+v", got)
	}
}

func TestFileCompanionReviewVisibleNotesKeepsOnlyPassedLines(t *testing.T) {
	notes := []FileCompanionShareNote{
		{ID: "cn_pending", Author: "reader", Text: "待审核", Decision: "pending"},
		{ID: "cn_ignored", Author: "reader", Text: "已忽略", Decision: "ignored"},
		{ID: "cn_approved", Author: "reader", Text: "已通过", Decision: "approved"},
		{ID: "cn_legacy", Author: "reader", Text: "旧批注"},
		{ID: "cn_host", Author: "host", Text: "主持人回答", Host: true},
	}
	got := fileCompanionReviewVisibleNotes(notes)
	if len(got) != 2 || got[0].ID != "cn_approved" || got[1].ID != "cn_host" {
		t.Fatalf("visible notes = %+v", got)
	}
}

func TestPollFileCompanionShareDiscussionsWaitsForHostApproval(t *testing.T) {
	const token = "cd_cccccccccccccccccccccccccccccccc"
	resetFileCompanionShareDiscussionLimits(token)
	now := time.Now().UTC()
	var mu sync.Mutex
	notes := []map[string]any{
		{"id": "cn_pending", "author": "reader@example.com", "text": "待审核的问题", "created_at": now.Add(-10 * time.Minute).Format(time.RFC3339), "decision": "pending"},
		{"id": "cn_ignored", "author": "reader@example.com", "text": "已忽略的问题", "created_at": now.Add(-8 * time.Minute).Format(time.RFC3339), "decision": "ignored"},
		{"id": "cn_approved", "author": "reader@example.com", "text": "已通过的问题", "created_at": now.Add(-5 * time.Minute).Format(time.RFC3339), "decision": "approved"},
	}
	revision := 2
	var posts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/mobile/documents/shares":
			mu.Lock()
			payload := map[string]any{
				"notes_reply":  true,
				"notes_review": true,
				"mine": []map[string]any{{
					"token": token, "name": "a.pdf", "owner": "owner@example.com", "visibility": "group",
					"permanent": true, "revision": 1, "notes": notes, "notes_revision": revision,
				}},
				"received": []any{},
			}
			body, _ := json.Marshal(payload)
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
		case r.Method == http.MethodPost && r.URL.Path == "/api/mobile/documents/shares/"+token+"/notes":
			var post struct {
				Text    string `json:"text"`
				ReplyTo string `json:"reply_to"`
			}
			_ = json.NewDecoder(r.Body).Decode(&post)
			mu.Lock()
			posts = append(posts, post.ReplyTo)
			revision++
			notes = append(notes, map[string]any{
				"id": "cn_host", "author": "owner@example.com", "text": post.Text,
				"created_at": time.Now().UTC().Format(time.RFC3339), "host": true, "reply_to": post.ReplyTo,
			})
			body, _ := json.Marshal(map[string]any{"notes": notes, "notes_revision": revision})
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	source := filepath.Join(t.TempDir(), "a.pdf")
	if err := os.WriteFile(source, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "a.pdf", Path: source, Source: "cloud", Mine: true, Revision: 1,
	}); err != nil {
		t.Fatal(err)
	}

	oldHook := fileCompanionShareDiscussionModelHook
	t.Cleanup(func() { fileCompanionShareDiscussionModelHook = oldHook })
	questions := 0
	fileCompanionShareDiscussionModelHook = func(msg IMUserMessage) (*IMAgentResponse, error) {
		questions++
		if !strings.Contains(msg.Text, "已通过的问题") || strings.Contains(msg.Text, "待审核的问题") || strings.Contains(msg.Text, "已忽略的问题") {
			t.Fatalf("agent saw the wrong note: %q", msg.Text)
		}
		return &IMAgentResponse{Text: "回答已通过的问题。"}, nil
	}

	if _, err := app.pollFileCompanionShareDiscussions(); err != nil {
		t.Fatal(err)
	}
	if questions != 1 || len(posts) != 1 || posts[0] != "cn_approved" {
		t.Fatalf("questions=%d posts=%+v", questions, posts)
	}
}

func TestPollFileCompanionShareDiscussionsSkipsANoteIgnoredBeforeTheTurn(t *testing.T) {
	const token = "cd_eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	resetFileCompanionShareDiscussionLimits(token)
	t.Cleanup(func() { resetFileCompanionShareDiscussionLimits(token) })
	now := time.Now().UTC()
	var mu sync.Mutex
	gets := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/mobile/documents/shares" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		gets++
		decision := "approved"
		if gets > 1 {
			decision = "ignored"
		}
		payload := map[string]any{
			"notes_reply":  true,
			"notes_review": true,
			"mine": []map[string]any{{
				"token": token, "name": "a.pdf", "owner": "o", "visibility": "group",
				"permanent": true, "revision": 1, "notes_revision": gets,
				"notes": []map[string]any{{
					"id": "cn_late", "author": "reader@example.com", "text": "不该进入模型",
					"created_at": now.Add(-time.Minute).Format(time.RFC3339), "decision": decision,
				}},
			}},
			"received": []any{},
		}
		body, _ := json.Marshal(payload)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer server.Close()
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache:      corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"},
	}
	source := filepath.Join(t.TempDir(), "a.pdf")
	if err := os.WriteFile(source, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "a.pdf", Path: source, Source: "cloud", Mine: true, Revision: 1,
	}); err != nil {
		t.Fatal(err)
	}
	oldHook := fileCompanionShareDiscussionModelHook
	t.Cleanup(func() { fileCompanionShareDiscussionModelHook = oldHook })
	questions := 0
	fileCompanionShareDiscussionModelHook = func(msg IMUserMessage) (*IMAgentResponse, error) {
		questions++
		return &IMAgentResponse{Text: "不该出现的回答"}, nil
	}
	if _, err := app.pollFileCompanionShareDiscussions(); err != nil {
		t.Fatal(err)
	}
	if questions != 0 {
		t.Fatalf("ignored note reached the agent: questions=%d", questions)
	}
}

func TestPollFileCompanionShareDiscussionsSkipsAnIgnoreWhenTheReviewFlagIsMissing(t *testing.T) {
	const token = "cd_dddddddddddddddddddddddddddddddd"
	resetFileCompanionShareDiscussionLimits(token)
	t.Cleanup(func() { resetFileCompanionShareDiscussionLimits(token) })
	now := time.Now().UTC()
	var mu sync.Mutex
	gets := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/mobile/documents/shares" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		gets++
		decision := "approved"
		if gets > 1 {
			decision = "ignored"
		}
		payload := map[string]any{
			"notes_reply": true,
			"mine": []map[string]any{{
				"token": token, "name": "a.pdf", "owner": "o", "visibility": "group",
				"permanent": true, "revision": 1, "notes_revision": gets,
				"notes": []map[string]any{{
					"id": "cn_late", "author": "reader@example.com", "text": "不该进入模型",
					"created_at": now.Add(-time.Minute).Format(time.RFC3339), "decision": decision,
				}},
			}},
			"received": []any{},
		}
		body, _ := json.Marshal(payload)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer server.Close()
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache:      corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"},
	}
	source := filepath.Join(t.TempDir(), "a.pdf")
	if err := os.WriteFile(source, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "a.pdf", Path: source, Source: "cloud", Mine: true, Revision: 1,
	}); err != nil {
		t.Fatal(err)
	}
	oldHook := fileCompanionShareDiscussionModelHook
	t.Cleanup(func() { fileCompanionShareDiscussionModelHook = oldHook })
	questions := 0
	fileCompanionShareDiscussionModelHook = func(msg IMUserMessage) (*IMAgentResponse, error) {
		questions++
		return &IMAgentResponse{Text: "不该出现的回答"}, nil
	}
	if _, err := app.pollFileCompanionShareDiscussions(); err != nil {
		t.Fatal(err)
	}
	if questions != 0 {
		t.Fatalf("ignored note reached the agent without notes_review: questions=%d", questions)
	}
}

func TestPollFileCompanionShareDiscussionsHoldsPendingNotesWhenTheReviewFlagIsMissing(t *testing.T) {
	const token = "cd_abababababababababababababababab"
	resetFileCompanionShareDiscussionLimits(token)
	t.Cleanup(func() { resetFileCompanionShareDiscussionLimits(token) })
	now := time.Now().UTC()
	var posts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/mobile/documents/shares":
			body, _ := json.Marshal(map[string]any{
				"notes_reply": true,
				"mine": []map[string]any{{
					"token": token, "name": "a.pdf", "owner": "o", "visibility": "group",
					"permanent": true, "revision": 1, "notes_revision": 1,
					"notes": []map[string]any{
						{"id": "cn_wait", "author": "phone:17090134628", "text": "作者是谁？", "created_at": now.Add(-2 * time.Minute).Format(time.RFC3339), "decision": "pending"},
						{"id": "cn_ok", "author": "phone:17090134628", "text": "已通过的问题", "created_at": now.Add(-time.Minute).Format(time.RFC3339), "decision": "approved"},
					},
				}},
				"received": []any{},
			})
			_, _ = w.Write(body)
		case r.Method == http.MethodPost && r.URL.Path == "/api/mobile/documents/shares/"+token+"/notes":
			var post struct {
				ReplyTo string `json:"reply_to"`
			}
			_ = json.NewDecoder(r.Body).Decode(&post)
			posts = append(posts, post.ReplyTo)
			_, _ = w.Write([]byte(`{"notes":[],"notes_revision":2}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache:      corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"},
	}
	source := filepath.Join(t.TempDir(), "a.pdf")
	if err := os.WriteFile(source, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "a.pdf", Path: source, Source: "cloud", Mine: true, Revision: 1,
	}); err != nil {
		t.Fatal(err)
	}
	oldHook := fileCompanionShareDiscussionModelHook
	t.Cleanup(func() { fileCompanionShareDiscussionModelHook = oldHook })
	questions := 0
	fileCompanionShareDiscussionModelHook = func(msg IMUserMessage) (*IMAgentResponse, error) {
		questions++
		if strings.Contains(msg.Text, "作者是谁") {
			t.Fatalf("pending note reached the agent without approval: %q", msg.Text)
		}
		return &IMAgentResponse{Text: "回答已通过的问题"}, nil
	}
	if _, err := app.pollFileCompanionShareDiscussions(); err != nil {
		t.Fatal(err)
	}
	if questions != 1 || len(posts) != 1 || posts[0] != "cn_ok" {
		t.Fatalf("questions=%d posts=%v", questions, posts)
	}
}

func TestPollFileCompanionShareDiscussionsDropsAReplyTheHubRefusedAsIgnored(t *testing.T) {
	const token = "cd_ffffffffffffffffffffffffffffffff"
	resetFileCompanionShareDiscussionLimits(token)
	t.Cleanup(func() { resetFileCompanionShareDiscussionLimits(token) })
	now := time.Now().UTC()
	var mu sync.Mutex
	decision := "approved"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/mobile/documents/shares":
			body, _ := json.Marshal(map[string]any{
				"notes_reply":  true,
				"notes_review": true,
				"mine": []map[string]any{{
					"token": token, "name": "a.pdf", "owner": "o", "visibility": "group",
					"permanent": true, "revision": 1, "notes_revision": 1,
					"notes": []map[string]any{{
						"id": "cn_flip", "author": "reader@example.com", "text": "通过后被忽略",
						"created_at": now.Add(-time.Minute).Format(time.RFC3339), "decision": decision,
					}},
				}},
				"received": []any{},
			})
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
		case r.Method == http.MethodPost && r.URL.Path == "/api/mobile/documents/shares/"+token+"/notes":
			decision = "ignored"
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"批注已被忽略，不能回复"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache:      corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"},
	}
	source := filepath.Join(t.TempDir(), "a.pdf")
	if err := os.WriteFile(source, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "a.pdf", Path: source, Source: "cloud", Mine: true, Revision: 1,
	}); err != nil {
		t.Fatal(err)
	}
	oldHook := fileCompanionShareDiscussionModelHook
	t.Cleanup(func() { fileCompanionShareDiscussionModelHook = oldHook })
	questions := 0
	fileCompanionShareDiscussionModelHook = func(msg IMUserMessage) (*IMAgentResponse, error) {
		questions++
		if strings.Contains(msg.Text, "不该进入") {
			t.Fatalf("agent saw a dropped note: %q", msg.Text)
		}
		return &IMAgentResponse{Text: "这份回答不该被缓存"}, nil
	}
	if _, err := app.pollFileCompanionShareDiscussions(); err != nil {
		t.Fatal(err)
	}
	if questions != 1 {
		t.Fatalf("questions=%d, want the turn that the Hub then refused", questions)
	}
	if _, err := app.pollFileCompanionShareDiscussions(); err != nil {
		t.Fatal(err)
	}
	if questions != 1 {
		t.Fatalf("ignored note was answered again: questions=%d", questions)
	}
	mu.Lock()
	decision = "approved"
	mu.Unlock()
	if _, err := app.pollFileCompanionShareDiscussions(); err != nil {
		t.Fatal(err)
	}
	if questions != 2 {
		t.Fatalf("re-approval did not start a fresh turn: questions=%d", questions)
	}
}

func TestFileCompanionShareDiscussionRepliesToApprovedNoteOlderThanWatermark(t *testing.T) {
	const token = "cd_dddddddddddddddddddddddddddddddd"
	resetFileCompanionShareDiscussionLimits(token)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"notes_reply":true,"notes_review":true,"server_time":"2026-10-27T12:00:00Z","mine":[{"token":"`+token+`","name":"a.pdf","owner":"o","visibility":"group","permanent":true,"revision":1,"notes":[{"id":"cn_old","author":"r","text":"很久以前的批注","created_at":"2026-09-01T00:00:00Z","decision":"approved"}],"notes_revision":1}],"received":[]}`)
	}))
	defer server.Close()
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	source := filepath.Join(t.TempDir(), "a.pdf")
	if err := os.WriteFile(source, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "a.pdf", Path: source, Source: "cloud", Mine: true, Revision: 1,
	}); err != nil {
		t.Fatal(err)
	}

	oldHook := fileCompanionShareDiscussionModelHook
	t.Cleanup(func() { fileCompanionShareDiscussionModelHook = oldHook })
	questions := 0
	fileCompanionShareDiscussionModelHook = func(msg IMUserMessage) (*IMAgentResponse, error) {
		questions++
		return &IMAgentResponse{Text: "回答很久以前的批注。"}, nil
	}
	// A Hub call would try to post the reply; this test only needs the fact
	// that the approved note was picked up, so let the POST fail and inspect
	// the model call count before the failure is recorded.
	if _, err := app.pollFileCompanionShareDiscussions(); err != nil {
		t.Fatal(err)
	}
	if questions != 1 {
		t.Fatalf("approved note older than the watermark was skipped: questions=%d", questions)
	}
}

func TestFileCompanionShareDiscussionReplyIsCapped(t *testing.T) {
	long := strings.Repeat("长", fileCompanionShareDiscussionReplyRunes+20)
	got := fileCompanionShareDiscussionReply(long)
	runes := []rune(got)
	if len(runes) != fileCompanionShareDiscussionReplyRunes+1 || !strings.HasSuffix(got, "…") {
		t.Fatalf("reply cap = %d runes", len(runes))
	}
}

func TestPollFileCompanionShareDiscussionsAnswersOneReaderNote(t *testing.T) {
	const token = "cd_0123456789abcdef0123456789abcdef"
	resetFileCompanionShareDiscussionLimits(token)
	now := time.Now().UTC()
	readerNote := map[string]any{
		"id":         "cn_reader",
		"author":     "reader@example.com",
		"text":       "这个方法能复现吗？",
		"created_at": now.Add(-30 * time.Minute).Format(time.RFC3339),
		"host":       false,
	}
	var mu sync.Mutex
	notes := []map[string]any{readerNote}
	revision := 1
	type notePost struct {
		Text    string `json:"text"`
		ReplyTo string `json:"reply_to"`
	}
	var posts []notePost
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/mobile/documents/shares":
			mu.Lock()
			payload := map[string]any{
				"notes_reply": true,
				"mine": []map[string]any{{
					"token":          token,
					"name":           "论文.pdf",
					"owner":          "owner@example.com",
					"visibility":     "group",
					"permanent":      true,
					"revision":       3,
					"notes":          notes,
					"notes_revision": revision,
				}},
				"received": []any{},
			}
			body, _ := json.Marshal(payload)
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
		case r.Method == http.MethodPost && r.URL.Path == "/api/mobile/documents/shares/"+token+"/notes":
			var post notePost
			_ = json.NewDecoder(r.Body).Decode(&post)
			mu.Lock()
			posts = append(posts, post)
			revision++
			notes = append(notes, map[string]any{
				"id":         "cn_host",
				"author":     "owner@example.com",
				"text":       post.Text,
				"created_at": time.Now().UTC().Format(time.RFC3339),
				"host":       true,
				"reply_to":   post.ReplyTo,
			})
			body, _ := json.Marshal(map[string]any{"notes": notes, "notes_revision": revision})
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	source := filepath.Join(t.TempDir(), "论文.pdf")
	if err := os.WriteFile(source, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "论文.pdf", Path: source, Source: "cloud", Mine: true, Revision: 3,
	}); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(app.GetDataDir(), "file-companion", fileCompanionShareDiscussionDirName)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	since, _ := json.Marshal(map[string]any{"since": map[string]string{token: now.Add(-time.Hour).Format(time.RFC3339)}})
	if err := os.WriteFile(filepath.Join(stateDir, fileCompanionShareDiscussionStateName), since, 0o600); err != nil {
		t.Fatal(err)
	}

	oldHook := fileCompanionShareDiscussionModelHook
	t.Cleanup(func() { fileCompanionShareDiscussionModelHook = oldHook })
	questions := 0
	var question string
	fileCompanionShareDiscussionModelHook = func(msg IMUserMessage) (*IMAgentResponse, error) {
		questions++
		question = msg.Text
		return &IMAgentResponse{Text: "可以复现，补充材料里有完整配置。"}, nil
	}

	count, err := app.pollFileCompanionShareDiscussions()
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || questions != 1 {
		t.Fatalf("poll count=%d questions=%d", count, questions)
	}
	if !strings.Contains(question, "这个方法能复现吗") {
		t.Fatalf("question = %q", question)
	}
	if len(posts) != 1 || posts[0].ReplyTo != "cn_reader" || !strings.Contains(posts[0].Text, "补充材料") {
		t.Fatalf("posts = %+v", posts)
	}
	// The Hub now carries the host reply, so the note is not answered twice.
	if _, err := app.pollFileCompanionShareDiscussions(); err != nil {
		t.Fatal(err)
	}
	if questions != 1 || len(posts) != 1 {
		t.Fatalf("answered the same note twice: questions=%d posts=%d", questions, len(posts))
	}
}

func TestPollFileCompanionShareDiscussionsSkipsOldHub(t *testing.T) {
	const token = "cd_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	resetFileCompanionShareDiscussionLimits(token)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// An older Hub omits notes_reply and has no host/reply_to fields.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"mine":[{"token":"` + token + `","name":"a.pdf","owner":"o","visibility":"group","permanent":true,"revision":1,"notes":[{"id":"cn_1","author":"r","text":"问题","created_at":"2026-10-27T10:00:00Z"}],"notes_revision":1}],"received":[]}`))
	}))
	defer server.Close()
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	oldHook := fileCompanionShareDiscussionModelHook
	t.Cleanup(func() { fileCompanionShareDiscussionModelHook = oldHook })
	fileCompanionShareDiscussionModelHook = func(msg IMUserMessage) (*IMAgentResponse, error) {
		t.Fatal("an old Hub must not trigger the host agent")
		return nil, nil
	}
	count, err := app.pollFileCompanionShareDiscussions()
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("old hub count = %d", count)
	}
}

func TestFileCompanionShareDiscussionFirstPollSkipsHistory(t *testing.T) {
	const token = "cd_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	resetFileCompanionShareDiscussionLimits(token)
	now := time.Now().UTC()
	var mu sync.Mutex
	notes := []map[string]any{{
		"id":         "cn_history",
		"author":     "reader@example.com",
		"text":       "功能上线前就有的批注",
		"created_at": now.Add(-30 * time.Minute).Format(time.RFC3339),
	}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		payload := map[string]any{
			"notes_reply": true,
			"mine": []map[string]any{{
				"token": token, "name": "a.pdf", "owner": "o", "visibility": "group",
				"permanent": true, "revision": 1, "notes": notes, "notes_revision": len(notes),
			}},
			"received": []any{},
		}
		body, _ := json.Marshal(payload)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer server.Close()
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	source := filepath.Join(t.TempDir(), "a.pdf")
	if err := os.WriteFile(source, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "a.pdf", Path: source, Source: "cloud", Mine: true,
	}); err != nil {
		t.Fatal(err)
	}
	oldHook := fileCompanionShareDiscussionModelHook
	t.Cleanup(func() { fileCompanionShareDiscussionModelHook = oldHook })
	questions := 0
	fileCompanionShareDiscussionModelHook = func(msg IMUserMessage) (*IMAgentResponse, error) {
		questions++
		return &IMAgentResponse{Text: "回答"}, nil
	}
	if _, err := app.pollFileCompanionShareDiscussions(); err != nil {
		t.Fatal(err)
	}
	if questions != 0 {
		t.Fatal("the first poll answered notes that predate the feature")
	}
	mu.Lock()
	notes = append(notes, map[string]any{
		"id":         "cn_live",
		"author":     "reader@example.com",
		"text":       "上线后的新批注",
		"created_at": now.Add(2 * time.Second).Format(time.RFC3339),
	})
	mu.Unlock()
	if _, err := app.pollFileCompanionShareDiscussions(); err != nil {
		t.Fatal(err)
	}
	if questions != 1 {
		t.Fatalf("a note after the baseline was not answered: questions=%d", questions)
	}
}

func TestFileCompanionShareDiscussionClaimIsExclusive(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	release, ok := app.claimFileCompanionShareDiscussionNote("cd_cccccccccccccccccccccccccccccccc", "cn_1")
	if !ok {
		t.Fatal("first claim was refused")
	}
	if _, second := app.claimFileCompanionShareDiscussionNote("cd_cccccccccccccccccccccccccccccccc", "cn_1"); second {
		t.Fatal("a claimed note was claimed twice")
	}
	release()
	if _, again := app.claimFileCompanionShareDiscussionNote("cd_cccccccccccccccccccccccccccccccc", "cn_1"); !again {
		t.Fatal("a released claim stayed held")
	}
}

func TestFileCompanionShareDiscussionHandlerUsesTheMainProcessHandler(t *testing.T) {
	app := &App{testHomeDir: t.TempDir(), disableBackgroundEmbeddingForTest: true}
	manager := NewRemoteSessionManager(app)
	app.remoteSessions = manager
	client := NewRemoteHubClient(app, manager)
	manager.SetHubClient(client)
	handler, err := app.fileCompanionShareDiscussionHandler("fcs_test")
	if err != nil {
		t.Fatal(err)
	}
	if handler == nil || handler.memory == nil || app.imHandler != handler {
		t.Fatalf("discussion handler did not reuse the main handler: %#v", handler)
	}
	dir := filepath.Join(app.GetDataDir(), "file-companion", fileCompanionShareDiscussionDirName, "sessions")
	if info, statErr := os.Stat(dir); statErr != nil || !info.IsDir() {
		t.Fatalf("discussion session directory: %v", statErr)
	}
}

func TestListFileCompanionLibraryCarriesHostReplyCapability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/mobile/documents/shares" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"notes_reply":true,"mine":[],"received":[]}`))
	}))
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	listed, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	if !listed.NotesReply {
		t.Fatal("a Hub with host replies was reported as an old Hub")
	}
	// A later network failure keeps the last answer: falling back to false
	// would make a new reader turn answer privately on a Hub that supports the
	// shared discussion.
	server.Close()
	listed, err = app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	if !listed.NotesReply {
		t.Fatal("a failed refresh lost the host reply capability")
	}
}

func TestFileCompanionShareDiscussionSessionFindsItsDocument(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	path := filepath.Join(t.TempDir(), "paper.pdf")
	if err := os.WriteFile(path, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	sessionID := fileCompanionShareDiscussionSessionID("cd_ffffffffffffffffffffffffffffffff")
	canonical, err := app.ensureFileCompanionShareDiscussionDocument(sessionID, path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := app.fileCompanionOpenDocument("file-companion:" + sessionID)
	if err != nil || got != canonical {
		t.Fatalf("discussion document = %q, %v; want %q", got, err, canonical)
	}
	handler := &IMMessageHandler{app: app}
	toolName := func(def map[string]interface{}) string {
		fn, _ := def["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		return name
	}
	tools := handler.fileCompanionClosedSurfaceTools("file-companion:" + sessionID)
	if len(tools) == 0 {
		t.Fatal("the discussion session did not get the PDF tools")
	}
	names := map[string]bool{}
	for _, def := range tools {
		names[toolName(def)] = true
	}
	if !names[fileCompanionToolReadPages] {
		t.Fatal("the discussion session lost the read tool")
	}
	if names[fileCompanionToolRestore] || names[fileCompanionToolGenerate] {
		t.Fatalf("the discussion session can write files: %v", names)
	}
	if text, ok := handler.executeFileCompanionHostTool("file-companion:"+sessionID, fileCompanionToolGenerate, "{}"); ok || !strings.Contains(text, "只能阅读") {
		t.Fatalf("a discussion turn called pdf_generate: ok=%v text=%q", ok, text)
	}
	// A normal open tab keeps the whole surface.
	normal := handler.fileCompanionClosedSurfaceTools("file-companion:" + fileCompanionSessionID(canonical))
	normalNames := map[string]bool{}
	for _, def := range normal {
		normalNames[toolName(def)] = true
	}
	if !normalNames[fileCompanionToolGenerate] {
		t.Fatal("a normal companion turn lost pdf_generate")
	}

	// A tab that already has the file open keeps its grant, including the
	// permission to write; the background session only adds a read-only one
	// when nothing is open.
	st := app.companionRuntime()
	st.mu.Lock()
	st.grants[canonical] = &fileCompanionGrant{UserPath: path, Canonical: canonical, Editable: true, SelectionStart: -1}
	st.mu.Unlock()
	if _, err := app.ensureFileCompanionShareDiscussionDocument(sessionID, path); err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	editable := st.grants[canonical].Editable
	st.mu.Unlock()
	if !editable {
		t.Fatal("the background session replaced an open tab's grant")
	}
}

func TestFileCompanionShareDiscussionUsesShareCreationAfterInstall(t *testing.T) {
	const (
		liveToken = "cd_dddddddddddddddddddddddddddddddd"
		oldToken  = "cd_eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	)
	resetFileCompanionShareDiscussionLimits(liveToken)
	resetFileCompanionShareDiscussionLimits(oldToken)
	t.Cleanup(func() {
		resetFileCompanionShareDiscussionLimits(liveToken)
		resetFileCompanionShareDiscussionLimits(oldToken)
	})
	now := time.Now().UTC()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]any{
			"notes_reply": true,
			"mine": []map[string]any{
				{
					"token": liveToken, "name": "live.pdf", "owner": "o", "visibility": "group",
					"permanent": true, "revision": 1, "notes_revision": 1,
					"created_at": now.Add(-10 * time.Minute).Format(time.RFC3339),
					"notes": []map[string]any{{
						"id": "cn_live", "author": "r", "text": "上线后的新批注",
						"created_at": now.Add(-5 * time.Minute).Format(time.RFC3339),
					}},
				},
				{
					"token": oldToken, "name": "old.pdf", "owner": "o", "visibility": "group",
					"permanent": true, "revision": 1, "notes_revision": 1,
					"created_at": now.Add(-2 * time.Hour).Format(time.RFC3339),
					"notes": []map[string]any{{
						"id": "cn_old", "author": "r", "text": "安装前的旧批注",
						"created_at": now.Add(-90 * time.Minute).Format(time.RFC3339),
					}},
				},
			},
			"received": []any{},
		}
		body, _ := json.Marshal(payload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer server.Close()
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache:      corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"},
	}
	for _, token := range []string{liveToken, oldToken} {
		path := filepath.Join(t.TempDir(), token+".pdf")
		if err := os.WriteFile(path, []byte("%PDF-"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
			ID: token, Token: token, Name: token + ".pdf", Path: path, Source: "cloud", Mine: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	state := map[string]any{"installed_at": now.Add(-time.Hour).Format(time.RFC3339)}
	body, _ := json.Marshal(state)
	statePath := app.fileCompanionShareDiscussionStatePath()
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	oldHook := fileCompanionShareDiscussionModelHook
	t.Cleanup(func() { fileCompanionShareDiscussionModelHook = oldHook })
	var questions []string
	fileCompanionShareDiscussionModelHook = func(msg IMUserMessage) (*IMAgentResponse, error) {
		questions = append(questions, msg.Text)
		return &IMAgentResponse{Text: "回答"}, nil
	}
	if _, err := app.pollFileCompanionShareDiscussions(); err != nil {
		t.Fatal(err)
	}
	if len(questions) != 1 || !strings.Contains(questions[0], "上线后的新批注") {
		t.Fatalf("questions = %q", questions)
	}
	if strings.Contains(questions[0], "旧批注") {
		t.Fatalf("a pre-install note was answered: %q", questions[0])
	}
}

func TestShareDiscussionReusesTheAnswerWhenTheHubRefusesThePost(t *testing.T) {
	const token = "cd_11111111111111111111111111111111"
	resetFileCompanionShareDiscussionLimits(token)
	t.Cleanup(func() { resetFileCompanionShareDiscussionLimits(token) })
	now := time.Now().UTC()
	readerNote := map[string]any{
		"id": "cn_reader", "author": "r", "text": "能复现吗？",
		"created_at": now.Add(-30 * time.Minute).Format(time.RFC3339),
	}
	var mu sync.Mutex
	notes := []map[string]any{readerNote}
	postAttempts := 0
	var posted []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/mobile/documents/shares":
			mu.Lock()
			payload := map[string]any{
				"notes_reply": true,
				"mine": []map[string]any{{
					"token": token, "name": "a.pdf", "owner": "o", "visibility": "group",
					"permanent": true, "revision": 1, "notes": notes, "notes_revision": len(notes),
				}},
				"received": []any{},
			}
			body, _ := json.Marshal(payload)
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
		case r.Method == http.MethodPost && r.URL.Path == "/api/mobile/documents/shares/"+token+"/notes":
			mu.Lock()
			postAttempts++
			attempt := postAttempts
			mu.Unlock()
			if attempt == 1 {
				http.Error(w, `{"message":"暂时无法保存"}`, http.StatusInternalServerError)
				return
			}
			var post struct {
				Text string `json:"text"`
			}
			_ = json.NewDecoder(r.Body).Decode(&post)
			mu.Lock()
			posted = append(posted, post.Text)
			body, _ := json.Marshal(map[string]any{"notes": notes, "notes_revision": len(notes)})
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache:      corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"},
	}
	path := filepath.Join(t.TempDir(), "a.pdf")
	if err := os.WriteFile(path, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "a.pdf", Path: path, Source: "cloud", Mine: true,
	}); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{"since": map[string]string{token: now.Add(-time.Hour).Format(time.RFC3339)}}
	body, _ := json.Marshal(state)
	statePath := app.fileCompanionShareDiscussionStatePath()
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	oldHook := fileCompanionShareDiscussionModelHook
	t.Cleanup(func() { fileCompanionShareDiscussionModelHook = oldHook })
	questions := 0
	fileCompanionShareDiscussionModelHook = func(msg IMUserMessage) (*IMAgentResponse, error) {
		questions++
		return &IMAgentResponse{Text: "可以复现，配置在附录。"}, nil
	}
	if _, err := app.pollFileCompanionShareDiscussions(); err != nil {
		t.Fatal(err)
	}
	if questions != 1 || postAttempts != 1 {
		t.Fatalf("first poll questions=%d posts=%d", questions, postAttempts)
	}
	// The retry interval is a real delay; clear it so the cached answer is
	// posted now instead of waiting five minutes.
	delete(fileCompanionShareDiscussionFailures, token+"|cn_reader")
	if _, err := app.pollFileCompanionShareDiscussions(); err != nil {
		t.Fatal(err)
	}
	if questions != 1 {
		t.Fatalf("the model ran again for a cached answer: questions=%d", questions)
	}
	if postAttempts != 2 || len(posted) != 1 || !strings.Contains(posted[0], "配置在附录") {
		t.Fatalf("retry posts=%d body=%v", postAttempts, posted)
	}
}

func TestShareDiscussionPromptCarriesNoHostAssistantContext(t *testing.T) {
	handler := &IMMessageHandler{}
	const discussionPrompt = "主持人讨论提示"
	got := handler.buildIMEntrySystemPrompt(IMUserMessage{
		UserID: "file-companion:" + fileCompanionShareDiscussionSessionID("cd_33333333333333333333333333333333"),
		Text:   "读者问题",
		AssistantBinding: &agent.AssistantBinding{
			Mode:          "file-companion",
			InitialPrompt: discussionPrompt,
		},
	}, nil, nil, false, "", "", "", "")
	if got != discussionPrompt {
		t.Fatalf("discussion prompt leaked host context: %q", got)
	}
}

func TestShareDiscussionReplyRedactsHostPaths(t *testing.T) {
	canonical := filepath.Join(t.TempDir(), "paper.pdf")
	dataDir := filepath.Join(t.TempDir(), "data")
	reply := "文件在 " + canonical + "，目录 " + filepath.Dir(canonical) + "，结果 " + filepath.Join(dataDir, "file-companion", "results", "x.pdf")
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		reply += "，主目录 " + home
	}
	got := fileCompanionShareDiscussionRedact(reply, canonical, dataDir)
	if strings.Contains(got, canonical) || strings.Contains(got, filepath.Dir(canonical)) || strings.Contains(got, dataDir) {
		t.Fatalf("redaction left a host path: %q", got)
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" && strings.Contains(got, home) {
		t.Fatalf("redaction left the home directory: %q", got)
	}
	if !strings.Contains(got, "本机路径已省略") {
		t.Fatalf("redaction placeholder missing: %q", got)
	}
}

func TestShareDiscussionWatermarkUsesTheHubClock(t *testing.T) {
	const token = "cd_44444444444444444444444444444444"
	resetFileCompanionShareDiscussionLimits(token)
	t.Cleanup(func() { resetFileCompanionShareDiscussionLimits(token) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/mobile/documents/shares" {
			http.NotFound(w, r)
			return
		}
		// The Host's own clock is not part of this test. The Hub clock says the
		// feature was installed at 10:00 and the share was made at 10:05, so
		// the 10:06 note is live even if this machine's clock is hours ahead.
		body, _ := json.Marshal(map[string]any{
			"notes_reply": true,
			"server_time": "2026-10-27T10:00:00Z",
			"mine": []map[string]any{{
				"token": token, "name": "a.pdf", "owner": "o", "visibility": "group",
				"permanent": true, "revision": 1, "notes_revision": 1,
				"created_at": "2026-10-27T10:05:00Z",
				"notes": []map[string]any{{
					"id": "cn_live", "author": "r", "text": "Hub 时钟下的新批注",
					"created_at": "2026-10-27T10:06:00Z",
				}},
			}},
			"received": []any{},
		})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer server.Close()
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache:      corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"},
	}
	path := filepath.Join(t.TempDir(), "a.pdf")
	if err := os.WriteFile(path, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "a.pdf", Path: path, Source: "cloud", Mine: true,
	}); err != nil {
		t.Fatal(err)
	}
	oldHook := fileCompanionShareDiscussionModelHook
	t.Cleanup(func() { fileCompanionShareDiscussionModelHook = oldHook })
	questions := 0
	fileCompanionShareDiscussionModelHook = func(msg IMUserMessage) (*IMAgentResponse, error) {
		questions++
		return &IMAgentResponse{Text: "回答"}, nil
	}
	if _, err := app.pollFileCompanionShareDiscussions(); err != nil {
		t.Fatal(err)
	}
	if questions != 1 {
		t.Fatalf("a note after the Hub-clock watermark was not answered: questions=%d", questions)
	}
}

func TestShareDiscussionClampsAHostClockWatermark(t *testing.T) {
	const token = "cd_55555555555555555555555555555555"
	resetFileCompanionShareDiscussionLimits(token)
	t.Cleanup(func() { resetFileCompanionShareDiscussionLimits(token) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := json.Marshal(map[string]any{
			"notes_reply": true,
			"server_time": "2026-10-27T10:00:00Z",
			"mine": []map[string]any{{
				"token": token, "name": "a.pdf", "owner": "o", "visibility": "group",
				"permanent": true, "revision": 1, "notes_revision": 1,
				"created_at": "2026-10-27T10:05:00Z",
				"notes": []map[string]any{{
					"id": "cn_live", "author": "r", "text": "本机时钟跑快后的现场批注",
					"created_at": "2026-10-27T10:06:00Z",
				}},
			}},
			"received": []any{},
		})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer server.Close()
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache:      corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"},
	}
	path := filepath.Join(t.TempDir(), "a.pdf")
	if err := os.WriteFile(path, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "a.pdf", Path: path, Source: "cloud", Mine: true,
	}); err != nil {
		t.Fatal(err)
	}
	statePath := app.fileCompanionShareDiscussionStatePath()
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte(`{"installed_at":"2026-10-27T10:10:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	oldHook := fileCompanionShareDiscussionModelHook
	t.Cleanup(func() { fileCompanionShareDiscussionModelHook = oldHook })
	questions := 0
	fileCompanionShareDiscussionModelHook = func(msg IMUserMessage) (*IMAgentResponse, error) {
		questions++
		return &IMAgentResponse{Text: "回答"}, nil
	}
	if _, err := app.pollFileCompanionShareDiscussions(); err != nil {
		t.Fatal(err)
	}
	if questions != 1 {
		t.Fatalf("a watermark ahead of the Hub clock skipped a live note: questions=%d", questions)
	}
}

// After a host transfer the old admin is a participant: the new host uploads
// the project and the admin's machine must download it. Only the host seat
// syncs, so the two machines cannot overwrite each other.
func TestFileCompanionHostTransferMovesSyncAndDiscussion(t *testing.T) {
	const token = "cd_10101010101010101010101010101010"
	var app *App
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/mobile/documents/shares":
			_, _ = io.WriteString(w, `{"notes_reply":true,"server_time":"`+time.Now().UTC().Format(time.RFC3339)+`","mine":[{"token":"`+token+`","name":"a.pdf","owner":"old@example.com","host_label":"new@example.com","host":false,"admin":true,"visibility":"group","permanent":true,"revision":4}],"received":[]}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/import"):
			w.Header().Set("X-Companion-Document-Name", "a.pdf")
			w.Header().Set("X-Companion-Document-Visibility", "group")
			w.Header().Set("X-Companion-Document-Revision", "4")
			_, _ = w.Write([]byte("%PDF-from-new-host"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	app = &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache:      corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"},
	}
	source := filepath.Join(t.TempDir(), "a.pdf")
	if err := os.WriteFile(source, []byte("%PDF-old-admin"), 0o644); err != nil {
		t.Fatal(err)
	}
	opened, err := app.FileCompanionOpen(source)
	if err != nil {
		t.Fatal(err)
	}
	// The admin's own row: created it, but the seat moved away.
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "a.pdf", Path: opened.Path,
		Source: "cloud", Mine: true, Admin: true, Host: false, HostKnown: true,
		Visibility: "group", Revision: 3,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.SyncFileCompanionShare(opened.Path, "[]"); err == nil {
		t.Fatal("the former admin uploaded a project the new host owns")
	}
	// It is a participant now, so a newer host revision is downloaded.
	listed, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Updates) != 1 || listed.Updates[0].Token != token {
		t.Fatalf("updates = %+v", listed.Updates)
	}
	if listed.Updates[0].Revision != 4 {
		t.Fatalf("pulled revision = %d", listed.Updates[0].Revision)
	}
}

// A machine that accepts the host seat was a participant a moment ago, so its
// copy can be one revision behind the host who handed it over. It downloads
// the live project once before it starts uploading, or its first sync would
// publish the older revision and drop what the previous host already shared.
func TestFileCompanionHostSeatGainPullsTheLiveRevision(t *testing.T) {
	const token = "cd_20202020202020202020202020202020"
	var app *App
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/mobile/documents/shares":
			_, _ = io.WriteString(w, `{"notes_reply":false,"server_time":"`+time.Now().UTC().Format(time.RFC3339)+`","mine":[{"token":"`+token+`","name":"a.pdf","owner":"old@example.com","host_label":"me@example.com","host":true,"admin":false,"visibility":"group","permanent":true,"revision":4}],"received":[]}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/import"):
			w.Header().Set("X-Companion-Document-Name", "a.pdf")
			w.Header().Set("X-Companion-Document-Visibility", "group")
			w.Header().Set("X-Companion-Document-Revision", "4")
			_, _ = w.Write([]byte("%PDF-latest"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	app = &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache:      corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"},
	}
	source := filepath.Join(t.TempDir(), "a.pdf")
	if err := os.WriteFile(source, []byte("%PDF-participant"), 0o644); err != nil {
		t.Fatal(err)
	}
	opened, err := app.FileCompanionOpen(source)
	if err != nil {
		t.Fatal(err)
	}
	// The row this machine had while it was only a participant: same share, one
	// revision behind, and the seat was somebody else's.
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "a.pdf", Path: opened.Path,
		Source: "cloud", Mine: false, Admin: false, Host: false, HostKnown: true,
		Visibility: "group", Revision: 3,
	}); err != nil {
		t.Fatal(err)
	}
	listed, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Updates) != 1 || listed.Updates[0].Token != token {
		t.Fatalf("the new host did not pull the live revision: updates=%+v", listed.Updates)
	}
	if listed.Updates[0].Revision != 4 {
		t.Fatalf("pulled revision = %d", listed.Updates[0].Revision)
	}
	var stored FileCompanionLibraryItem
	var found bool
	for _, item := range listed.Items {
		if item.Token == token {
			stored, found = item, true
			break
		}
	}
	if !found {
		t.Fatalf("the share row disappeared from the library: %+v", listed.Items)
	}
	if !fileCompanionItemIsHost(stored) {
		t.Fatalf("the row did not keep the host seat: %+v", stored)
	}
	if stored.Revision != 4 {
		t.Fatalf("stored revision = %d, the next sync would publish the older project", stored.Revision)
	}
	// The seat answer has to survive a restart: a row re-read from disk is what
	// decides who may upload, and a dropped answer hands the seat back to the
	// share's creator.
	fileCompanionLibraryMu.Lock()
	reread, ok := app.readFileCompanionLibraryLocked()
	fileCompanionLibraryMu.Unlock()
	if !ok {
		t.Fatal("the library cannot be read back")
	}
	for _, item := range reread {
		if item.Token != token {
			continue
		}
		if !item.HostKnown {
			t.Fatalf("the stored row forgot the host answer: %+v", item)
		}
		if !fileCompanionItemIsHost(item) {
			t.Fatalf("a restarted row handed the host seat back: %+v", item)
		}
	}
}
