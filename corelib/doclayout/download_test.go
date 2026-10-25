package doclayout

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

func TestVerifyFilesRejectsShortWeights(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, modelFile)
	if err := os.WriteFile(path, []byte("short"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".data", []byte("short"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := verifyFiles(path)
	if !errors.Is(err, embedding.ErrModelCorrupt) {
		t.Fatalf("err = %v", err)
	}
}
