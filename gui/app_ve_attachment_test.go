package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
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
		{"letter.docx", "document"},
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

func TestBase64EncodeFile(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	content := "Hello, World!"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	encoded, err := base64EncodeFile(testFile)
	if err != nil {
		t.Fatalf("base64EncodeFile failed: %v", err)
	}
	if encoded != "SGVsbG8sIFdvcmxkIQ==" {
		t.Errorf("unexpected encoding: %s", encoded)
	}

	// Nonexistent file.
	_, err = base64EncodeFile(filepath.Join(tmpDir, "nope.txt"))
	if err == nil {
		t.Error("expected error for nonexistent file")
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
		{"file.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
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
