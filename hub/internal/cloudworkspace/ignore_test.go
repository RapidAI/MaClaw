package cloudworkspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/cloudworkspaceignore"
)

func TestShouldIgnoreWrapsCorelib(t *testing.T) {
	if IgnoreFileName != cloudworkspaceignore.FileName {
		t.Fatalf("IgnoreFileName=%q", IgnoreFileName)
	}
	if !ShouldIgnore("node_modules/x", false, "") {
		t.Fatal("builtin node_modules")
	}
	if ShouldIgnore("src/main.go", false, "") {
		t.Fatal("source should sync")
	}
	if !ShouldIgnore(".maclaw/x", false, "!.maclaw/\n!.maclaw/**\n") {
		t.Fatal("product dir is force-skipped")
	}
	if ShouldIgnore("app.exe", false, "") {
		t.Fatal("*.exe is not ignored")
	}
}

func TestReadCloudignoreWrapper(t *testing.T) {
	root := t.TempDir()
	body := "*.log\n"
	if err := os.WriteFile(filepath.Join(root, IgnoreFileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadCloudignore(root)
	if err != nil || got != body {
		t.Fatalf("got %q err=%v", got, err)
	}
}
