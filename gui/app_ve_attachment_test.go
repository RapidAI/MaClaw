package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/a2a"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestClassifyFileType(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		// Text files
		{"readme.txt", "text"},
		{"doc.md", "text"},
		{"data.csv", "text"},
		{"config.json", "text"},
		{"schema.xml", "text"},
		{"config.yaml", "text"},
		{"config.yml", "text"},
		{"app.log", "text"},
		{"main.go", "text"},
		{"script.py", "text"},
		{"index.js", "text"},
		{"app.ts", "text"},
		{"page.html", "text"},
		{"style.css", "text"},
		// Image files
		{"photo.png", "image"},
		{"photo.jpg", "image"},
		{"photo.jpeg", "image"},
		{"anim.gif", "image"},
		{"modern.webp", "image"},
		{"old.bmp", "image"},
		// Document files
		{"report.pdf", "document"},
		{"letter.doc", "document"},
		{"letter.docx", "document"},
		{"budget.xls", "document"},
		{"budget.xlsx", "document"},
		{"deck.ppt", "document"},
		{"deck.pptx", "document"},
		// Unsupported
		{"archive.zip", ""},
		{"binary.exe", ""},
		{"video.mp4", ""},
		{"noext", ""},
		// Case insensitive
		{"README.TXT", "text"},
		{"Photo.PNG", "image"},
		{"Report.PDF", "document"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := classifyFileType(tt.path)
			if result != tt.expected {
				t.Errorf("classifyFileType(%q) = %q, want %q", tt.path, result, tt.expected)
			}
		})
	}
}

func TestPrepareVEAttachmentMessageSupportsAllOfficeFormats(t *testing.T) {
	app := &App{configCacheValid: true, configCache: corelib.AppConfig{}}
	for _, ext := range []string{".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx"} {
		t.Run(ext, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "attachment"+ext)
			if err := os.WriteFile(path, []byte("office attachment"), 0o600); err != nil {
				t.Fatal(err)
			}

			msg, err := app.prepareVEAttachmentMessage("session-1", "review this", []string{path}, false)
			if err != nil {
				t.Fatalf("prepareVEAttachmentMessage(%s): %v", ext, err)
			}
			if len(msg.FileAttachments) != 1 || msg.FileAttachments[0].LocalPath != path || msg.FileAttachments[0].MimeType != mimeTypeForFile(path) {
				t.Fatalf("unexpected Office attachment message: %#v", msg)
			}
		})
	}
}

func TestSendVEGroupMessageWithAttachmentsLocalMentionAvoidsFileRelayUpload(t *testing.T) {
	var uploadCalls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ve/files/upload":
			atomic.AddInt32(&uploadCalls, 1)
			t.Errorf("unexpected file relay upload")
			http.NotFound(w, r)
		case "/api/a2a/consultations/session-1/messages":
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	imagePath := filepath.Join(tmpDir, "evidence.png")
	if err := os.WriteFile(imagePath, []byte("png-data"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	app.remoteSessions = NewRemoteSessionManager(app)
	client := NewRemoteHubClient(app, app.remoteSessions)
	app.remoteSessions.SetHubClient(client)
	dispatcher := client.groupChatDispatcher()
	dispatcher.RegisterSession("session-1")
	t.Cleanup(func() {
		if !dispatcher.UnregisterSessionAndWait("session-1", 5*time.Second) {
			t.Errorf("timed out waiting for local group executor cleanup")
		}
		app.resetPathBoundStateForDataDirChange()
	})

	if err := app.SendVEGroupMessageWithAttachments("session-1", "@本地AI 看附件", []string{"本地AI"}, []string{imagePath}); err != nil {
		t.Fatalf("SendVEGroupMessageWithAttachments: %v", err)
	}
	if got := atomic.LoadInt32(&uploadCalls); got != 0 {
		t.Fatalf("file relay uploads = %d, want 0", got)
	}
}

func TestSendVEGroupMessageWithAttachmentsMixedMentionsSendsRemoteAttachment(t *testing.T) {
	var uploadCalls int32
	remoteMessage := make(chan a2a.GroupDiscussionMessage, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ve/discoverable":
			_ = json.NewEncoder(w).Encode(map[string]any{"employees": []map[string]string{{"id": "remote-ve", "machine_id": "machine-ve", "name": "Remote VE"}}})
		case "/api/ve/files/upload":
			atomic.AddInt32(&uploadCalls, 1)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "file_id": "file-1", "file_url": "/api/ve/files/file-1"})
		case "/api/a2a/consultations/session-1/messages":
			var body a2a.GroupDiscussionMessage
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
				return
			}
			if len(body.ToIDs) == 1 && body.ToIDs[0] == "machine-ve" {
				remoteMessage <- body
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/api/a2a/consultations/session-1/detail":
			_ = json.NewEncoder(w).Encode(map[string]any{"discussion": map[string]any{"id": "session-1"}})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	imagePath := filepath.Join(tmpDir, "evidence.png")
	if err := os.WriteFile(imagePath, []byte("png-data"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineID: "machine-1", RemoteMachineToken: "token-1", GroupDiscussion: corelib.GroupDiscussionConfig{Enabled: true}}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	app.remoteSessions = NewRemoteSessionManager(app)
	client := NewRemoteHubClient(app, app.remoteSessions)
	app.remoteSessions.SetHubClient(client)
	dispatcher := client.groupChatDispatcher()
	dispatcher.RegisterSession("session-1")
	t.Cleanup(func() {
		if !dispatcher.UnregisterSessionAndWait("session-1", 5*time.Second) {
			t.Errorf("timed out waiting for local group executor cleanup")
		}
		app.resetPathBoundStateForDataDirChange()
	})

	if err := app.SendVEGroupMessageWithAttachments("session-1", "@local-maclaw @remote-ve see file", []string{"local-maclaw", "remote-ve"}, []string{imagePath}); err != nil {
		t.Fatalf("SendVEGroupMessageWithAttachments: %v", err)
	}

	select {
	case msg := <-remoteMessage:
		if len(msg.ImageAttachments) != 1 || msg.ImageAttachments[0].FileURL == "" || msg.ImageAttachments[0].LocalPath != "" {
			t.Fatalf("remote image attachment = %+v", msg.ImageAttachments)
		}
		if got := atomic.LoadInt32(&uploadCalls); got != 1 {
			t.Fatalf("file relay uploads = %d, want 1", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for remote attachment message")
	}
}

func TestSendVEGroupMessageWithAttachmentsMixedMentionsUploadsBeforeLocalDispatch(t *testing.T) {
	var messageCalls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ve/discoverable":
			_ = json.NewEncoder(w).Encode(map[string]any{"employees": []map[string]string{{"id": "remote-ve", "machine_id": "machine-ve", "name": "Remote VE"}}})
		case "/api/ve/files/upload":
			http.Error(w, "upload failed", http.StatusBadGateway)
		case "/api/a2a/consultations/session-1/detail":
			_ = json.NewEncoder(w).Encode(map[string]any{"discussion": map[string]any{"id": "session-1"}})
		case "/api/a2a/consultations/session-1/messages":
			atomic.AddInt32(&messageCalls, 1)
			t.Errorf("unexpected Hub message after upload failure")
			http.NotFound(w, r)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	imagePath := filepath.Join(tmpDir, "evidence.png")
	if err := os.WriteFile(imagePath, []byte("png-data"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineID: "machine-1", RemoteMachineToken: "token-1", GroupDiscussion: corelib.GroupDiscussionConfig{Enabled: true}}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	if err := app.SendVEGroupMessageWithAttachments("session-1", "@local-maclaw @remote-ve see file", []string{"local-maclaw", "remote-ve"}, []string{imagePath}); err == nil {
		t.Fatal("expected upload failure")
	}
	if got := atomic.LoadInt32(&messageCalls); got != 0 {
		t.Fatalf("Hub messages = %d, want 0", got)
	}
}

func TestPrepareVEAttachmentMessageDoesNotLeakLocalPathsForRemoteUpload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ve/files/upload" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "file_id": "file-1", "file_url": "/api/ve/files/file-1"})
	}))
	defer server.Close()

	oldClient := veFileRelayHTTPClient
	veFileRelayHTTPClient = server.Client()
	defer func() { veFileRelayHTTPClient = oldClient }()

	tmpDir := t.TempDir()
	textPath := filepath.Join(tmpDir, "note.txt")
	imagePath := filepath.Join(tmpDir, "image.png")
	docPath := filepath.Join(tmpDir, "report.pdf")
	for path, data := range map[string][]byte{textPath: []byte("note"), imagePath: []byte("png"), docPath: []byte("pdf")} {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	app := &App{configCacheValid: true, configCache: corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineToken: "token-1"}}
	msg, err := app.prepareVEAttachmentMessage("session-1", "files", []string{textPath, imagePath, docPath}, true)
	if err != nil {
		t.Fatalf("prepareVEAttachmentMessage remote: %v", err)
	}
	if len(msg.TextAttachments) != 0 {
		t.Fatalf("remote text files should be sent through relay file attachments: %+v", msg)
	}
	if len(msg.FileAttachments) != 2 {
		t.Fatalf("remote text and document files should both be relay file attachments: %+v", msg)
	}
	if msg.ImageAttachments[0].LocalPath != "" || msg.FileAttachments[0].LocalPath != "" || msg.FileAttachments[1].LocalPath != "" {
		t.Fatalf("remote attachment message leaked local paths: %+v", msg)
	}
	if msg.ImageAttachments[0].FileURL == "" || msg.FileAttachments[0].FileURL == "" || msg.FileAttachments[1].FileURL == "" {
		t.Fatalf("remote file attachments must have file URLs: %+v", msg)
	}

	localMsg, err := app.prepareVEAttachmentMessage("session-1", "files", []string{textPath, imagePath, docPath}, false)
	if err != nil {
		t.Fatalf("prepareVEAttachmentMessage local: %v", err)
	}
	if len(localMsg.TextAttachments) != 0 {
		t.Fatalf("local text files should be passed as local-path file attachments: %+v", localMsg)
	}
	if len(localMsg.FileAttachments) != 2 {
		t.Fatalf("local text and document files should both be file attachments: %+v", localMsg)
	}
	if localMsg.FileAttachments[0].LocalPath != textPath || localMsg.ImageAttachments[0].LocalPath != imagePath || localMsg.FileAttachments[1].LocalPath != docPath {
		t.Fatalf("local attachment message missing local paths: %+v", localMsg)
	}
	if localMsg.ImageAttachments[0].FileURL != "" || localMsg.FileAttachments[0].FileURL != "" || localMsg.FileAttachments[1].FileURL != "" {
		t.Fatalf("local-only attachment message should not upload files: %+v", localMsg)
	}
}

func TestVEFileAttachmentMaxBytesForUsesSharedDocumentLimit(t *testing.T) {
	for _, test := range []struct {
		path string
		want int64
	}{
		{"report.pdf", agent.MaxOfficeReadFileBytes},
		{"report.doc", agent.MaxOfficeReadFileBytes},
		{"report.docx", agent.MaxOfficeReadFileBytes},
		{"report.xls", agent.MaxOfficeReadFileBytes},
		{"report.xlsx", agent.MaxOfficeReadFileBytes},
		{"report.ppt", agent.MaxOfficeReadFileBytes},
		{"report.pptx", agent.MaxOfficeReadFileBytes},
		{"photo.png", veFileAttachmentMaxSize},
	} {
		t.Run(test.path, func(t *testing.T) {
			if got := veFileAttachmentMaxBytesFor(test.path); got != test.want {
				t.Fatalf("limit(%q) = %d, want %d", test.path, got, test.want)
			}
		})
	}
}

func TestBuildLocalVEFileAttachmentMessageRejectsOversizedOfficeDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.docx")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(agent.MaxOfficeReadFileBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := buildLocalVEFileAttachmentMessage(path, "", ""); err == nil || !strings.Contains(err.Error(), "32 MiB") {
		t.Fatalf("oversized Office attachment error = %v", err)
	}
}
func TestValidateFileSize(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a small text file (100 bytes).
	smallFile := filepath.Join(tmpDir, "small.txt")
	if err := os.WriteFile(smallFile, make([]byte, 100), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a file exceeding text limit (600KB).
	bigTextFile := filepath.Join(tmpDir, "big.txt")
	if err := os.WriteFile(bigTextFile, make([]byte, 600*1024), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		path     string
		category string
		wantErr  bool
	}{
		{"small text ok", smallFile, "text", false},
		{"big text exceeds", bigTextFile, "text", true},
		{"small as image ok", smallFile, "image", false},
		{"small as document ok", smallFile, "document", false},
		{"nonexistent file", filepath.Join(tmpDir, "nope.txt"), "text", true},
		{"unknown category", smallFile, "unknown", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFileSize(tt.path, tt.category)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateFileSize(%q, %q) error = %v, wantErr %v", tt.path, tt.category, err, tt.wantErr)
			}
		})
	}
}

func TestPrepareVEAttachmentMessageAllowsLargeTextAsFileAttachment(t *testing.T) {
	tmpDir := t.TempDir()
	largeText := filepath.Join(tmpDir, "large.log")
	if err := os.WriteFile(largeText, []byte(strings.Repeat("x", maxTextFileSize+1)), 0o644); err != nil {
		t.Fatal(err)
	}

	app := &App{configCacheValid: true, configCache: corelib.AppConfig{}}
	msg, err := app.prepareVEAttachmentMessage("session-1", "large text", []string{largeText}, false)
	if err != nil {
		t.Fatalf("prepareVEAttachmentMessage large text: %v", err)
	}
	if len(msg.TextAttachments) != 0 || len(msg.FileAttachments) != 1 {
		t.Fatalf("large text should be carried as file attachment: %+v", msg)
	}
	if msg.FileAttachments[0].LocalPath != largeText {
		t.Fatalf("large text local path = %q, want %q", msg.FileAttachments[0].LocalPath, largeText)
	}
}

func TestMimeTypeForFile(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"file.txt", "text/plain"},
		{"file.md", "text/markdown"},
		{"file.csv", "text/csv"},
		{"file.json", "application/json"},
		{"file.xml", "application/xml"},
		{"file.yaml", "application/x-yaml"},
		{"file.yml", "application/x-yaml"},
		{"file.go", "text/x-go"},
		{"file.py", "text/x-python"},
		{"file.js", "text/javascript"},
		{"file.ts", "text/typescript"},
		{"file.html", "text/html"},
		{"file.css", "text/css"},
		{"file.png", "image/png"},
		{"file.jpg", "image/jpeg"},
		{"file.jpeg", "image/jpeg"},
		{"file.gif", "image/gif"},
		{"file.webp", "image/webp"},
		{"file.bmp", "image/bmp"},
		{"file.pdf", "application/pdf"},
		{"file.doc", "application/msword"},
		{"file.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		{"file.xls", "application/vnd.ms-excel"},
		{"file.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
		{"file.ppt", "application/vnd.ms-powerpoint"},
		{"file.pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation"},
		{"file.unknown", "application/octet-stream"},
		{"file.log", "text/plain"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := mimeTypeForFile(tt.path)
			if result != tt.expected {
				t.Errorf("mimeTypeForFile(%q) = %q, want %q", tt.path, result, tt.expected)
			}
		})
	}
}

func TestUploadToFileRelayUsesRemoteClientIDFallback(t *testing.T) {
	var gotParticipantID string
	var gotMachineID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ve/files/upload" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		gotMachineID = r.Header.Get("X-Machine-ID")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		gotParticipantID = r.FormValue("participant_id")
		_, _, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("missing uploaded file: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "file_id": "file-1", "file_url": "/api/ve/files/file-1"})
	}))
	defer server.Close()

	oldClient := veFileRelayHTTPClient
	veFileRelayHTTPClient = server.Client()
	defer func() { veFileRelayHTTPClient = oldClient }()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "image.png")
	if err := os.WriteFile(filePath, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &App{configCacheValid: true, configCache: corelib.AppConfig{RemoteClientID: "client-fallback"}}
	fileURL, err := app.uploadToFileRelay(server.URL, "token-1", filePath, "session-1")
	if err != nil {
		t.Fatalf("uploadToFileRelay: %v", err)
	}
	if fileURL != server.URL+"/api/ve/files/file-1" {
		t.Fatalf("fileURL = %q", fileURL)
	}
	if gotParticipantID != "client-fallback" {
		t.Fatalf("participant_id = %q, want client-fallback", gotParticipantID)
	}
	if gotMachineID != "client-fallback" {
		t.Fatalf("X-Machine-ID = %q, want client-fallback", gotMachineID)
	}
}

func TestUploadToFileRelayRejectsExternalFileURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ve/files/upload" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "file_id": "file-1", "file_url": "https://evil.example/api/ve/files/file-1"})
	}))
	defer server.Close()

	oldClient := veFileRelayHTTPClient
	veFileRelayHTTPClient = server.Client()
	defer func() { veFileRelayHTTPClient = oldClient }()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "image.png")
	if err := os.WriteFile(filePath, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &App{configCacheValid: true, configCache: corelib.AppConfig{RemoteMachineID: "machine-1"}}
	if _, err := app.uploadToFileRelay(server.URL, "token-1", filePath, "session-1"); err == nil {
		t.Fatal("expected external upload file_url to be rejected")
	}
}

func TestBuildVEFileAttachmentMessageSanitizesDisplayName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ve/files/upload" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "file_id": "file-1", "file_url": "/api/ve/files/file-1"})
	}))
	defer server.Close()

	oldClient := veFileRelayHTTPClient
	veFileRelayHTTPClient = server.Client()
	defer func() { veFileRelayHTTPClient = oldClient }()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "report.pdf")
	if err := os.WriteFile(filePath, []byte("pdf"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &App{configCacheValid: true, configCache: corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineToken: "token-1"}}

	msg, err := app.buildVEFileAttachmentMessage("session-1", filePath, "..\\unsafe.pdf", "")
	if err != nil {
		t.Fatalf("buildVEFileAttachmentMessage: %v", err)
	}
	if len(msg.FileAttachments) != 1 {
		t.Fatalf("FileAttachments len = %d, want 1", len(msg.FileAttachments))
	}
	if got := msg.FileAttachments[0].Filename; got != "unsafe.pdf" {
		t.Fatalf("Filename = %q, want unsafe.pdf", got)
	}
	if msg.FileAttachments[0].LocalPath != "" || msg.FileAttachments[0].FileURL == "" {
		t.Fatalf("remote attachment path/url = local %q url %q", msg.FileAttachments[0].LocalPath, msg.FileAttachments[0].FileURL)
	}
}

func TestCleanVEAttachmentDisplayNameHandlesBothPathSeparators(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
	}{
		{name: `..\unsafe.pdf`, want: "unsafe.pdf"},
		{name: "../unsafe.pdf", want: "unsafe.pdf"},
		{name: `C:\tmp\report.pdf`, want: "report.pdf"},
		{name: "/tmp/report.pdf", want: "report.pdf"},
	} {
		if got := cleanVEAttachmentDisplayName(tc.name); got != tc.want {
			t.Fatalf("cleanVEAttachmentDisplayName(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestBuildLocalVEFileAttachmentMessageDoesNotRequireHub(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "report.pdf")
	if err := os.WriteFile(filePath, []byte("pdf"), 0o644); err != nil {
		t.Fatal(err)
	}

	msg, _, err := buildLocalVEFileAttachmentMessage(filePath, "", "")
	if err != nil {
		t.Fatalf("buildLocalVEFileAttachmentMessage: %v", err)
	}
	if len(msg.FileAttachments) != 1 {
		t.Fatalf("FileAttachments len = %d, want 1", len(msg.FileAttachments))
	}
	if msg.FileAttachments[0].LocalPath != filePath || msg.FileAttachments[0].FileURL != "" {
		t.Fatalf("local attachment path/url = local %q url %q", msg.FileAttachments[0].LocalPath, msg.FileAttachments[0].FileURL)
	}
}
