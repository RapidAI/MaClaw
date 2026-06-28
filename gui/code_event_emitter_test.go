package main

import (
	"testing"
)

// ---------------------------------------------------------------------------
// detectLanguageFromExt tests
// ---------------------------------------------------------------------------

func TestDetectLanguageFromExt_KnownExtensions(t *testing.T) {
	cases := []struct {
		fileName string
		want     string
	}{
		{"main.go", "go"},
		{"app.ts", "typescript"},
		{"component.tsx", "typescript"},
		{"index.js", "javascript"},
		{"app.jsx", "javascript"},
		{"script.py", "python"},
		{"lib.rs", "rust"},
		{"Main.java", "java"},
		{"util.c", "c"},
		{"util.h", "c"},
		{"engine.cpp", "cpp"},
		{"engine.cc", "cpp"},
		{"engine.hpp", "cpp"},
		{"page.html", "html"},
		{"page.htm", "html"},
		{"style.css", "css"},
		{"data.json", "json"},
		{"config.yaml", "yaml"},
		{"config.yml", "yaml"},
		{"README.md", "markdown"},
		{"deploy.sh", "shell"},
		{"init.bash", "shell"},
	}
	for _, tc := range cases {
		t.Run(tc.fileName, func(t *testing.T) {
			got := detectLanguageFromExt(tc.fileName)
			if got != tc.want {
				t.Errorf("detectLanguageFromExt(%q) = %q, want %q", tc.fileName, got, tc.want)
			}
		})
	}
}

func TestDetectLanguageFromExt_UnknownExtension(t *testing.T) {
	cases := []string{"notes.txt", "data.csv", "archive.tar.gz", "noext"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			got := detectLanguageFromExt(name)
			if got != "plaintext" {
				t.Errorf("detectLanguageFromExt(%q) = %q, want %q", name, got, "plaintext")
			}
		})
	}
}

func TestDetectLanguageFromExt_CaseInsensitive(t *testing.T) {
	cases := []struct {
		fileName string
		want     string
	}{
		{"Main.GO", "go"},
		{"App.TS", "typescript"},
		{"Index.JS", "javascript"},
		{"Script.PY", "python"},
		{"Config.YAML", "yaml"},
	}
	for _, tc := range cases {
		t.Run(tc.fileName, func(t *testing.T) {
			got := detectLanguageFromExt(tc.fileName)
			if got != tc.want {
				t.Errorf("detectLanguageFromExt(%q) = %q, want %q", tc.fileName, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Nil context safety tests
// ---------------------------------------------------------------------------

func TestCodeEventEmitter_NilContext_EmitCodeFileEvent(t *testing.T) {
	app := &App{} // ctx is nil by default
	emitter := NewCodeEventEmitter(app)

	// Should not panic when ctx is nil
	emitter.EmitCodeFileEvent(CodeFileEvent{
		SessionID: "test-session",
		FilePath:  "/project/main.go",
		FileName:  "main.go",
		Content:   "package main",
		OpType:    "create",
		Language:  "go",
	})
}

func TestCodeEventEmitter_NilContext_EmitSessionStart(t *testing.T) {
	app := &App{} // ctx is nil by default
	emitter := NewCodeEventEmitter(app)

	// Should not panic when ctx is nil
	emitter.EmitSessionStart("test-session")
}

func TestCodeEventEmitter_NilContext_EmitSessionEnd(t *testing.T) {
	app := &App{} // ctx is nil by default
	emitter := NewCodeEventEmitter(app)

	// Should not panic when ctx is nil
	emitter.EmitSessionEnd("test-session")
}

// ---------------------------------------------------------------------------
// Constructor and struct tests
// ---------------------------------------------------------------------------

func TestNewCodeEventEmitter(t *testing.T) {
	app := &App{}
	emitter := NewCodeEventEmitter(app)
	if emitter == nil {
		t.Fatal("NewCodeEventEmitter returned nil")
	}
	if emitter.app != app {
		t.Error("NewCodeEventEmitter did not set app field correctly")
	}
}

func TestCodeFileEvent_Fields(t *testing.T) {
	evt := CodeFileEvent{
		SessionID: "session-123",
		FilePath:  "/project/src/main.go",
		FileName:  "main.go",
		Content:   "package main\n\nfunc main() {}",
		Original:  "package main",
		OpType:    "modify",
		Language:  "go",
		ForceOpen: true,
	}
	if evt.SessionID != "session-123" {
		t.Errorf("SessionID = %q, want %q", evt.SessionID, "session-123")
	}
	if evt.FilePath != "/project/src/main.go" {
		t.Errorf("FilePath = %q, want %q", evt.FilePath, "/project/src/main.go")
	}
	if evt.FileName != "main.go" {
		t.Errorf("FileName = %q, want %q", evt.FileName, "main.go")
	}
	if evt.Content != "package main\n\nfunc main() {}" {
		t.Errorf("Content mismatch")
	}
	if evt.Original != "package main" {
		t.Errorf("Original = %q, want %q", evt.Original, "package main")
	}
	if evt.OpType != "modify" {
		t.Errorf("OpType = %q, want %q", evt.OpType, "modify")
	}
	if evt.Language != "go" {
		t.Errorf("Language = %q, want %q", evt.Language, "go")
	}
	if !evt.ForceOpen {
		t.Errorf("ForceOpen = false, want true")
	}
}
